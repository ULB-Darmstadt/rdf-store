package api

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestApiSpecValidates(t *testing.T) {
	data, err := json.Marshal(apispec)
	if err != nil {
		t.Fatal(err)
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/quantities", "/config", "/labels", "/resource", "/resource/{id}", "/profiles", "/profile/{id}", "/class-instances", "/conforming-resources", "/graph/neighborhood", "/sparql/query", "/rdfproxy", "/solr/{collection}/schema", "/solr/{collection}/select", "/solr/{collection}/query"} {
		if doc.Paths.Find(path) == nil {
			t.Errorf("missing path %s", path)
		}
	}
}
