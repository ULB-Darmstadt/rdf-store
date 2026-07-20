package shacl

import (
	"bytes"
	"testing"

	"github.com/deiu/rdf2go"
)

func TestSerializeProfileClosureIncludesTransitiveImportsAndStopsCycles(t *testing.T) {
	root := parseTestProfile(t, "http://example.org/root/", `
@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix sh: <http://www.w3.org/ns/shacl#> .
<http://example.org/root/> a sh:NodeShape ; owl:imports <http://example.org/middle/> .
`)
	middle := parseTestProfile(t, "http://example.org/middle/", `
@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix sh: <http://www.w3.org/ns/shacl#> .
<http://example.org/middle/> a sh:NodeShape ; owl:imports <http://example.org/leaf/> ; sh:or _:shared .
_:shared rdf:first <http://example.org/MiddleChoice> ; rdf:rest () .
`)
	leaf := parseTestProfile(t, "http://example.org/leaf/", `
@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
<http://example.org/leaf/> a sh:NodeShape ;
  owl:imports <http://example.org/root/> ;
  sh:or _:shared ;
  sh:property [ sh:path <http://example.org/value> ; sh:datatype xsd:double ] .
_:shared rdf:first <http://example.org/LeafChoice> ; rdf:rest rdf:nil .
`)

	serialized, err := SerializeProfileClosure(root, map[string]*NodeShape{
		root.Id.RawValue():   root,
		middle.Id.RawValue(): middle,
		leaf.Id.RawValue():   leaf,
	})
	if err != nil {
		t.Fatal(err)
	}
	graph := rdf2go.NewGraph("")
	if err := graph.Parse(bytes.NewReader(serialized), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	if graph.One(rdf2go.NewResource("http://example.org/leaf/"), SHACL_PROPERTY, nil) == nil {
		t.Fatal("transitively imported leaf shape is missing")
	}
	if imports := graph.All(nil, OWL_IMPORTS, nil); len(imports) != 0 {
		t.Fatalf("locally resolved import directives remain: %v", imports)
	}
	lists := graph.All(nil, RDF_LIST_FIRST, nil)
	if len(lists) != 2 || lists[0].Subject.Equal(lists[1].Subject) {
		t.Fatalf("lists from separate profiles were not scoped: %v", lists)
	}
	for _, triple := range graph.All(nil, RDF_LIST_REST, nil) {
		if _, blank := triple.Object.(*rdf2go.BlankNode); blank && graph.One(triple.Object, RDF_LIST_FIRST, nil) == nil {
			t.Fatalf("empty list tail was left as an orphan blank node: %v", triple)
		}
	}
}

func parseTestProfile(t *testing.T, id, data string) *NodeShape {
	t.Helper()
	bytes := []byte(data)
	shape, err := new(NodeShape).Parse(rdf2go.NewResource(id), &bytes)
	if err != nil {
		t.Fatal(err)
	}
	return shape
}
