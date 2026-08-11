package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"rdf-store-backend/base"
	"reflect"
	"regexp"
	"slices"

	"github.com/stevenferrer/solr-go"
)

var Endpoint = base.EnvVar("SOLR_ENDPOINT", "http://localhost:8983")
var numShards = base.EnvVarAsInt("SOLR_NUM_SHARDS", 1)
var client = solr.NewJSONClient(Endpoint)

type document map[string]any

// appendValue appends values that are not already present in a multi-value
// Solr field. Duplicate values do not carry additional search information.
func (d *document) appendValue(field string, value any) {
	if value == nil {
		return
	}
	valueRef := reflect.ValueOf(value)
	if valueRef.Kind() == reflect.Slice {
		for i := 0; i < valueRef.Len(); i++ {
			d.appendValue(field, valueRef.Index(i).Interface())
		}
		return
	}
	existing, ok := (*d)[field].([]any)
	if ok {
		for _, current := range existing {
			if reflect.DeepEqual(current, value) {
				return
			}
		}
	} else {
		existing = make([]any, 0)
	}
	(*d)[field] = append(existing, value)
}

// checkCollectionExists determines whether the Solr collection is reachable and present.
// It returns a boolean indicating existence along with any request error.
func checkCollectionExists(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/solr/admin/collections?action=LIST&wt=json", Endpoint), nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected solr status: %s", resp.Status)
	}
	var payload struct {
		Collections []string `json:"collections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false, err
	}
	return slices.Contains(payload.Collections, base.SolrIndex), nil
}

// recreateCollection drops and rebuilds the Solr collection and schema.
// It returns an error if any Solr operation fails.
func recreateCollection() (err error) {
	slog.Debug("recreating solr collection", "endpoint", Endpoint, "collection", base.SolrIndex)
	if err := client.DeleteCollection(context.Background(), solr.NewCollectionParams().Name(base.SolrIndex)); err != nil {
		slog.Warn("collection couldn't be deleted", "error", err)
	}
	if err = client.CreateCollection(context.Background(), solr.NewCollectionParams().Name(base.SolrIndex).NumShards(numShards)); err != nil {
		return
	}
	if err = client.AddFields(context.Background(), base.SolrIndex, createCollectionSchema()...); err != nil {
		return
	}
	if err = client.AddCopyFields(context.Background(), base.SolrIndex, solr.CopyField{Source: "*", Dest: "_text_"}); err != nil {
		return
	}
	if err = patchLocationField(); err != nil {
		return
	}
	return
}

// This enables WKT polygon indexing. Note that we have installed "jts-core" in our docker image.
// See https://solr.apache.org/guide/solr/latest/query-guide/spatial-search.html#jts-and-polygons-flat
// patchLocationField enables spatial WKT indexing for the location field.
// It returns an error if the Solr schema patch fails.
func patchLocationField() error {
	body := map[string]any{
		"replace-field-type": map[string]any{
			"name":                  "location_rpt",
			"class":                 "solr.SpatialRecursivePrefixTreeFieldType",
			"spatialContextFactory": "JTS",
			"autoIndex":             "true",
			"validationRule":        "repairBuffer0",
			"distErrPct":            "0.025",
			"maxDistErr":            "0.001",
			"distanceUnits":         "kilometers",
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	// since solr-go doesn't support this we'll simply post directly to solr
	resp, err := http.Post(fmt.Sprintf("%s/solr/%s/schema", Endpoint, base.SolrIndex), "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed patching solr schema. status was %d", resp.StatusCode)
	}
	return nil
}

// updateDocs submits document updates and commits them in Solr.
// It returns an error if the update or commit fails.
func updateDocs(docs []*document) error {
	commands := make([]any, 0, len(docs))
	for _, doc := range docs {
		// Documents are sent unwrapped. The {"doc": {...}} element form is
		// interpreted as a document with a map-valued "doc" field, which Solr
		// treats as an atomic update (or as a nested child when the schema
		// still defines the block-join _root_/_nest_path_ fields).
		commands = append(commands, doc)
	}
	return solrUpdateBody(map[string]any{"add": commands}, true)
}

var luceneSpecialCharacters = regexp.MustCompile(`[+\-&|!(){}\[\]^"~*?:\\/]`)

// escapeQueryValue quotes and escapes a value for safe use in a Solr query.
// It returns the quoted, escaped query string.
func escapeQueryValue(value string) string {
	return `"` + luceneSpecialCharacters.ReplaceAllStringFunc(value, func(match string) string {
		return `\` + match
	}) + `"`
}

// deleteByResourceId deletes all search documents belonging to a resource.
// The id clause keeps compatibility with documents indexed by older versions.
// It returns an error if the delete or commit fails.
func deleteByResourceId(resourceId string) error {
	escaped := escapeQueryValue(resourceId)
	return solrUpdateBody(map[string]any{"delete": map[string]any{"query": fmt.Sprintf("id:%s OR resourceId:%s", escaped, escaped)}}, true)
}

// solrUpdateBody posts an update payload to the collection's /update handler
// and commits if requested. Solr error responses carry an "error.metadata"
// member that may be an array or an object; the solr-go client decodes it as
// []string and aborts response parsing, hiding the real message, so update
// responses are handled directly.
func solrUpdateBody(payload any, commit bool) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	urlStr := fmt.Sprintf("%s/solr/%s/update", Endpoint, base.SolrIndex)
	if commit {
		urlStr += "?commit=true"
	}
	resp, err := http.Post(urlStr, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("solr update failed: %s", extractSolrError(body))
	}
	return nil
}

// extractSolrError returns the message of a Solr error response body, falling
// back to the raw body when it cannot be parsed.
func extractSolrError(body []byte) string {
	var payload struct {
		Error struct {
			Msg string `json:"msg"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error.Msg != "" {
		return payload.Error.Msg
	}
	return string(body)
}
