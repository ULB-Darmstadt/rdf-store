package shacl

import (
	"strings"
	"testing"

	"github.com/deiu/rdf2go"
)

func TestParseKeepsPropertyAlternativesSeparateFromConjunctiveNodeShapes(t *testing.T) {
	graph := rdf2go.NewGraph("")
	data := `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix ex: <http://example.org/> .
ex:Root a sh:NodeShape ;
  sh:property [ sh:path ex:item ; sh:or ( [ sh:node ex:A ] [ sh:node ex:B ] ) ] .
ex:A a sh:NodeShape .
ex:B a sh:NodeShape .
`
	if err := graph.Parse(strings.NewReader(data), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	shape, err := new(NodeShape).Parse(rdf2go.NewResource("http://example.org/Root"), nil)
	if err == nil || shape != nil {
		// Parse without RDF or a graph must fail; set the already parsed graph next.
		t.Fatal("expected missing graph input to fail")
	}
	shape = &NodeShape{Graph: graph}
	shape, err = shape.Parse(rdf2go.NewResource("http://example.org/Root"), nil)
	if err != nil {
		t.Fatal(err)
	}
	properties := shape.Properties["http://example.org/item"]
	if len(properties) != 1 {
		t.Fatalf("expected one property, got %d", len(properties))
	}
	property := properties[0]
	if len(property.NodeShapes) != 0 {
		t.Fatalf("alternative shapes leaked into conjunctive node shapes: %#v", property.NodeShapes)
	}
	if !property.AlternativeNodeShapes["http://example.org/A"] || !property.AlternativeNodeShapes["http://example.org/B"] {
		t.Fatalf("missing parsed alternatives: %#v", property.AlternativeNodeShapes)
	}
}
