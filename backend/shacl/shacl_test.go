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

func TestParseNodeShapeFacet(t *testing.T) {
	graph := rdf2go.NewGraph("")
	data := `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix dash: <http://datashapes.org/dash#> .
@prefix ex: <http://example.org/> .
ex:Person a sh:NodeShape ; dash:facet true .
`
	if err := graph.Parse(strings.NewReader(data), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	shape, err := (&NodeShape{Graph: graph}).Parse(rdf2go.NewResource("http://example.org/Person"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if shape.Facet == nil || !*shape.Facet {
		t.Fatalf("expected dash:facet true, got %#v", shape.Facet)
	}
}

func TestDetectRdfCollectionsMarksRecursiveListPattern(t *testing.T) {
	turtle := `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix ex: <http://example.org/> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .

ex:Root a sh:NodeShape ;
  sh:property [
    sh:path ex:hasValues ;
    sh:maxCount 1 ;
    sh:node ex:ListShape ;
    sh:nodeKind sh:BlankNodeOrIRI ;
    sh:class ex:List
  ] .

ex:ListShape a sh:NodeShape ;
  sh:property [
    sh:path rdf:first ;
    sh:minCount 1 ;
    sh:maxCount 1 ;
    sh:datatype xsd:decimal
  ] ;
  sh:property [
    sh:path rdf:rest ;
    sh:minCount 1 ;
    sh:maxCount 1 ;
    sh:or (
      [ sh:hasValue rdf:nil ]
      [ sh:node ex:ListShape ]
    )
  ] .
`
	parseShape := func(id string) *NodeShape {
		graph := rdf2go.NewGraph("")
		if err := graph.Parse(strings.NewReader(turtle), "text/turtle"); err != nil {
			t.Fatal(err)
		}
		shape, err := (&NodeShape{Graph: graph}).Parse(rdf2go.NewResource(id), nil)
		if err != nil {
			t.Fatal(err)
		}
		return shape
	}

	root := parseShape("http://example.org/Root")
	listShape := parseShape("http://example.org/ListShape")
	shapes := map[string]*NodeShape{
		"http://example.org/Root":      root,
		"http://example.org/ListShape": listShape,
	}

	root.DenormalizePropertyNodeShapes(shapes)
	DetectRdfCollections(shapes)

	props := root.Properties["http://example.org/hasValues"]
	if len(props) != 1 {
		t.Fatalf("expected one property, got %d", len(props))
	}
	if !props[0].IsRdfCollection {
		t.Fatal("expected property to be detected as RDF collection")
	}
}

func TestDetectRdfCollectionsAcceptsEmptyListShorthand(t *testing.T) {
	turtle := `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix ex: <http://example.org/> .

ex:Root a sh:NodeShape ;
  sh:property [ sh:path ex:hasValues ; sh:maxCount 1 ; sh:node ex:ListShape ] .
ex:ListShape a sh:NodeShape ;
  sh:property [ sh:path rdf:first ; sh:minCount 1 ; sh:maxCount 1 ] ;
  sh:property [ sh:path rdf:rest ; sh:minCount 1 ; sh:maxCount 1 ;
    sh:or ( [ sh:hasValue () ] [ sh:node ex:ListShape ] )
  ] .
`
	parse := func(id string) *NodeShape {
		graph := rdf2go.NewGraph("")
		if err := graph.Parse(strings.NewReader(turtle), "text/turtle"); err != nil {
			t.Fatal(err)
		}
		shape, err := (&NodeShape{Graph: graph}).Parse(rdf2go.NewResource(id), nil)
		if err != nil {
			t.Fatal(err)
		}
		return shape
	}
	root := parse("http://example.org/Root")
	shapes := map[string]*NodeShape{
		"http://example.org/Root":      root,
		"http://example.org/ListShape": parse("http://example.org/ListShape"),
	}
	DetectRdfCollections(shapes)
	if !root.Properties["http://example.org/hasValues"][0].IsRdfCollection {
		t.Fatal("expected () terminator to be detected as rdf:nil")
	}
}

func TestDetectRdfCollectionsRequiresSupportedPattern(t *testing.T) {
	valid := `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix ex: <http://example.org/> .
ex:Root a sh:NodeShape ; sh:property [
  sh:path ex:hasValues ; sh:maxCount 1 ; sh:node ex:ListShape
] .
ex:ListShape a sh:NodeShape ;
  sh:property [ sh:path rdf:first ; sh:minCount 1 ; sh:maxCount 1 ] ;
  sh:property [ sh:path rdf:rest ; sh:minCount 1 ; sh:maxCount 1 ;
    sh:or ( [ sh:hasValue rdf:nil ] [ sh:node ex:ListShape ] )
  ] .
`
	tests := map[string]string{
		"outer maxCount":      strings.Replace(valid, "sh:maxCount 1 ; sh:node ex:ListShape", "sh:node ex:ListShape", 1),
		"cell cardinality":    strings.Replace(valid, "sh:path rdf:first ; sh:minCount 1 ; sh:maxCount 1", "sh:path rdf:first", 1),
		"rdf:nil terminator":  strings.Replace(valid, "sh:hasValue rdf:nil", "sh:hasValue ex:notNil", 1),
		"supported node kind": strings.Replace(valid, "sh:node ex:ListShape", "sh:node ex:ListShape ; sh:nodeKind sh:Literal", 1),
	}
	for name, turtle := range tests {
		t.Run(name, func(t *testing.T) {
			parse := func(id string) *NodeShape {
				graph := rdf2go.NewGraph("")
				if err := graph.Parse(strings.NewReader(turtle), "text/turtle"); err != nil {
					t.Fatal(err)
				}
				shape, err := (&NodeShape{Graph: graph}).Parse(rdf2go.NewResource(id), nil)
				if err != nil {
					t.Fatal(err)
				}
				return shape
			}
			root := parse("http://example.org/Root")
			shapes := map[string]*NodeShape{
				"http://example.org/Root":      root,
				"http://example.org/ListShape": parse("http://example.org/ListShape"),
			}
			DetectRdfCollections(shapes)
			if root.Properties["http://example.org/hasValues"][0].IsRdfCollection {
				t.Fatalf("unsupported pattern was detected as an RDF collection")
			}
		})
	}
}

func TestDetectRdfCollectionsIgnoresNonCollectionPattern(t *testing.T) {
	graph := rdf2go.NewGraph("")
	data := `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix ex: <http://example.org/> .

ex:Root a sh:NodeShape ;
  sh:property [
    sh:path ex:hasPart ;
    sh:node ex:PartShape
  ] .

ex:PartShape a sh:NodeShape ;
  sh:property [
    sh:path ex:name ;
    sh:datatype <http://www.w3.org/2001/XMLSchema#string>
  ] .
`
	if err := graph.Parse(strings.NewReader(data), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	root := &NodeShape{Graph: graph}
	root, err := root.Parse(rdf2go.NewResource("http://example.org/Root"), nil)
	if err != nil {
		t.Fatal(err)
	}
	partShape := &NodeShape{Graph: graph}
	partShape, err = partShape.Parse(rdf2go.NewResource("http://example.org/PartShape"), nil)
	if err != nil {
		t.Fatal(err)
	}
	shapes := map[string]*NodeShape{
		"http://example.org/Root":      root,
		"http://example.org/PartShape": partShape,
	}

	root.DenormalizePropertyNodeShapes(shapes)
	DetectRdfCollections(shapes)

	props := root.Properties["http://example.org/hasPart"]
	if len(props) != 1 {
		t.Fatalf("expected one property, got %d", len(props))
	}
	if props[0].IsRdfCollection {
		t.Fatal("expected property NOT to be detected as RDF collection")
	}
}

func TestWalkRdfCollectionFollowsFirstRestChain(t *testing.T) {
	graph := rdf2go.NewGraph("")
	data := `
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix ex: <http://example.org/> .
ex:head rdf:first 1.5 ; rdf:rest _:tail .
_:tail rdf:first 2.75 ; rdf:rest rdf:nil .
`
	if err := graph.Parse(strings.NewReader(data), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	items := WalkRdfCollection(rdf2go.NewResource("http://example.org/head"), graph)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].RawValue() != "1.5" {
		t.Fatalf("expected first item 1.5, got %v", items[0].RawValue())
	}
	if items[1].RawValue() != "2.75" {
		t.Fatalf("expected second item 2.75, got %v", items[1].RawValue())
	}
}

func TestWalkRdfCollectionReturnsNilForRdfNil(t *testing.T) {
	items := WalkRdfCollection(RDF_LIST_NIL, rdf2go.NewGraph(""))
	if items != nil {
		t.Fatalf("expected nil for rdf:nil, got %v", items)
	}
}
