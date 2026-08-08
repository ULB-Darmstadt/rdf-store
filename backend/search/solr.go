package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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
	if err = createCollectionWithConfigset(); err != nil {
		return
	}
	if err = ensureNoBlockJoinFields(); err != nil {
		return
	}
	if err = ensureSchemaFields(); err != nil {
		return
	}
	if err = ensureCopyField(); err != nil {
		return
	}
	if err = ensureLocationRptFieldType(); err != nil {
		return
	}
	return
}

// createCollectionWithConfigset creates the index collection from the
// project-specific configset (shipped in the Solr image under
// server/solr/configsets/<index>). It raises the schemaless field limit
// (maxFields) beyond the default 1000, which the dataset's hashed query fields
// (_query_.*) require. solr-go cannot pass a configName, so this posts the
// create directly.
func createCollectionWithConfigset() error {
	query := url.Values{}
	query.Add("action", "CREATE")
	query.Add("name", base.SolrIndex)
	query.Add("collection.configName", base.SolrIndex)
	query.Add("numShards", fmt.Sprintf("%d", numShards))
	query.Add("wt", "json")
	resp, err := http.Post(fmt.Sprintf("%s/solr/admin/collections?%s", Endpoint, query.Encode()), "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed creating solr collection. status was %s: %s", resp.Status, body)
	}
	return nil
}

// postSchema sends a single schema-change request to Solr and treats any
// response whose body contains one of the tolerated fragments as success. The
// collection config doubles as the configset in ZooKeeper, so schema changes
// made by earlier index generations persist and must be applied idempotently.
func postSchema(operation string, payload any, tolerated ...string) error {
	body := map[string]any{operation: payload}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := http.Post(fmt.Sprintf("%s/solr/%s/schema", Endpoint, base.SolrIndex), "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		for _, fragment := range tolerated {
			if bytes.Contains(raw, []byte(fragment)) {
				slog.Debug("schema entry already in desired state", "operation", operation)
				return nil
			}
		}
		return fmt.Errorf("failed updating solr schema. status was %d: %s", resp.StatusCode, raw)
	}
	return nil
}

// ensureSchemaFields adds the static document fields, skipping any that
// already exist in the (possibly previously populated) collection schema.
func ensureSchemaFields() error {
	for _, field := range createCollectionSchema() {
		if err := postSchema("add-field", field, "already exists"); err != nil {
			return err
		}
	}
	return nil
}

// ensureNoBlockJoinFields drops the nested-document fields of the default
// configset. Without them, Solr treats every flat entity document as a block
// child and auto-creates a phantom parent document per add (doubling the index
// with bare-UUID docs that lack a resourceId). Fresh collections always carry
// the fields because the managed schema persists in the shared configset.
func ensureNoBlockJoinFields() error {
	for _, field := range []string{"_root_", "_nest_path_"} {
		if err := postSchema("delete-field", map[string]any{"name": field}, "does not exist", "not present"); err != nil {
			return err
		}
	}
	return nil
}

// ensureCopyField adds the catch-all copyField that feeds the _text_ field.
func ensureCopyField() error {
	return postSchema("add-copy-field", solr.CopyField{Source: "*", Dest: "_text_"}, "already exists")
}

// ensureLocationRptFieldType enables spatial WKT indexing for the location
// field. This requires the "jts-core" package installed in the docker image.
// See https://solr.apache.org/guide/solr/latest/query-guide/spatial-search.html#jts-and-polygons-flat
func ensureLocationRptFieldType() error {
	return postSchema("replace-field-type", map[string]any{
		"name":                  "location_rpt",
		"class":                 "solr.SpatialRecursivePrefixTreeFieldType",
		"spatialContextFactory": "JTS",
		"autoIndex":             "true",
		"validationRule":        "repairBuffer0",
		"distErrPct":            "0.025",
		"maxDistErr":            "0.001",
		"distanceUnits":         "kilometers",
	})
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
	data, err := json.Marshal(map[string]any{"add": commands})
	if err != nil {
		return err
	}
	resp, err := client.Update(context.Background(), base.SolrIndex, solr.JSON, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return errors.New(resp.Error.Msg)
	}
	if err := client.Commit(context.Background(), base.SolrIndex); err != nil {
		return err
	}
	return nil
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
	data, err := json.Marshal(map[string]any{"delete": map[string]any{"query": fmt.Sprintf("id:%s OR resourceId:%s", escaped, escaped)}})
	if err != nil {
		return err
	}
	resp, err := client.Update(context.Background(), base.SolrIndex, solr.JSON, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return errors.New(resp.Error.Msg)
	}
	if err := client.Commit(context.Background(), base.SolrIndex); err != nil {
		return err
	}
	return nil
}
