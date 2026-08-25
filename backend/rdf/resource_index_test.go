package rdf

import (
	"testing"

	"github.com/deiu/rdf2go"
)

func TestAppendNQuadsToGraph(t *testing.T) {
	graph := rdf2go.NewGraph("")
	root := rdf2go.NewResource("http://example.org/root")
	link := rdf2go.NewResource("http://example.org/link")
	predicate := rdf2go.NewResource("http://example.org/links")
	graph.AddTriple(root, predicate, link)

	data := []byte(`<http://example.org/link> <http://example.org/name> "Linked value" <http://example.org/graph> .`)
	if err := appendNQuadsToGraph(graph, data); err != nil {
		t.Fatal(err)
	}
	name := rdf2go.NewResource("http://example.org/name")
	triple := graph.One(link, name, nil)
	if triple == nil || triple.Object.RawValue() != "Linked value" {
		t.Fatalf("expected linked N-Quad in graph, got %#v", triple)
	}
	if graph.One(root, predicate, link) == nil {
		t.Fatal("expected original graph content to remain")
	}
}
