package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"rdf-store-backend/base"
	"slices"

	"github.com/stevenferrer/solr-go"
)

var Endpoint = base.EnvVar("SOLR_ENDPOINT", "http://localhost:8983")
var numShards = base.EnvVarAsInt("SOLR_NUM_SHARDS", 1)
var client = solr.NewJSONClient(Endpoint)

type document map[string]any

// checkCollectionExists determines whether the Solr collection is reachable.
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

// updateDoc submits a document update and commits it in Solr.
func updateDoc(doc *document) error {
	data, err := json.MarshalIndent(map[string]any{"add": map[string]any{"doc": doc}}, "", "   ")
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

// deleteDocs deletes all documents beneath a root container id.
func deleteDocs(root string) error {
	data, err := json.Marshal(map[string]any{"delete": map[string]any{"query": "_root_:\"container_" + root + "\""}})
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
