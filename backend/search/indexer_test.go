package search

import (
	"testing"

	"github.com/deiu/rdf2go"

	"rdf-store-backend/rdf"
	"rdf-store-backend/shacl"
)

func TestQueryFieldNameIsStable(t *testing.T) {
	name := queryFieldName("http://example.org/Root", []string{
		"http://example.org/child",
		"http://example.org/score",
	}, "ds")
	const expected = "_query_.6dfec1bf76dd54a9f64c4acf0d8a8044_ds"
	if name != expected {
		t.Fatalf("expected query field %q, got %q", expected, name)
	}
}

func TestDocumentAppendValueDeduplicatesValues(t *testing.T) {
	doc := document{}
	doc.appendValue("field", []string{"first", "first", "second"})
	doc.appendValue("field", "second")

	values, ok := doc["field"].([]any)
	if !ok || len(values) != 2 || values[0] != "first" || values[1] != "second" {
		t.Fatalf("expected unique field values, got %#v", doc["field"])
	}
}

func TestRootShapeSchemaIsMultiValued(t *testing.T) {
	for _, field := range createCollectionSchema() {
		if field.Name == "rootShape" {
			if !field.MultiValued {
				t.Fatal("expected rootShape to accept inherited shape IDs")
			}
			return
		}
	}
	t.Fatal("rootShape field is missing from the collection schema")
}

func TestBuildQueryDocIndexesNestedInheritedNumericValue(t *testing.T) {
	rootID := "http://example.org/Root"
	derivedID := "http://example.org/Derived"
	baseID := "http://example.org/Base"
	rootPropertyID := "urn:property:child"
	scorePropertyID := "urn:property:score"
	childPath := "http://example.org/child"
	scorePath := "http://example.org/score"

	root := &shacl.NodeShape{
		Id:           rdf2go.NewResource(rootID),
		Parents:      map[string]bool{},
		Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{
			childPath: {
				{
					Id:         rdf2go.NewResource(rootPropertyID),
					NodeShapes: map[string]bool{derivedID: true},
				},
				{
					Id:         rdf2go.NewResource(rootPropertyID + ":duplicate"),
					NodeShapes: map[string]bool{derivedID: true},
				},
			},
		},
	}
	derived := &shacl.NodeShape{
		Id:           rdf2go.NewResource(derivedID),
		Parents:      map[string]bool{baseID: true},
		Alternatives: map[string]bool{},
		Properties:   map[string][]*shacl.Property{},
	}
	base := &shacl.NodeShape{
		Id:           rdf2go.NewResource(baseID),
		Parents:      map[string]bool{},
		Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{
			scorePath: {{
				Id:       rdf2go.NewResource(scorePropertyID),
				Datatype: "http://www.w3.org/2001/XMLSchema#decimal",
			}},
		},
	}

	previousProfiles := rdf.Profiles
	rdf.Profiles = map[string]*shacl.NodeShape{rootID: root, derivedID: derived, baseID: base}
	t.Cleanup(func() { rdf.Profiles = previousProfiles })

	rootSubject := rdf2go.NewResource("http://example.org/resource")
	childSubject := rdf2go.NewResource("http://example.org/child/1")
	graph := rdf2go.NewGraph("")
	graph.AddTriple(rootSubject, rdf2go.NewResource(childPath), childSubject)
	graph.AddTriple(childSubject, rdf2go.NewResource(scorePath), rdf2go.NewLiteralWithDatatype("12.5", rdf2go.NewResource("http://www.w3.org/2001/XMLSchema#decimal")))
	metadata := &rdf.ResourceMetadata{Conformance: map[string][]string{
		rootSubject.RawValue():  []string{rootID},
		childSubject.RawValue(): []string{derivedID},
	}}
	doc := document{}

	buildQueryDoc(rootSubject, root, rootID, nil, nil, graph, metadata, &doc, newQueryTraversalState())

	field := queryFieldName(rootID, []string{childPath, scorePath}, "ds")
	values, ok := doc[field].([]any)
	if !ok || len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected nested numeric query field %q, got %#v", field, doc[field])
	}
}

func TestBuildRootQueryDocsIndexesInheritedShapeContexts(t *testing.T) {
	const (
		derivedID = "http://example.org/Derived"
		middleID  = "http://example.org/Middle"
		baseID    = "http://example.org/Base"
		namePath  = "http://example.org/name"
	)

	derived := &shacl.NodeShape{
		Id:           rdf2go.NewResource(derivedID),
		Parents:      map[string]bool{middleID: true},
		Alternatives: map[string]bool{},
		Properties:   map[string][]*shacl.Property{},
	}
	middle := &shacl.NodeShape{
		Id:           rdf2go.NewResource(middleID),
		Parents:      map[string]bool{baseID: true},
		Alternatives: map[string]bool{},
		Properties:   map[string][]*shacl.Property{},
	}
	base := &shacl.NodeShape{
		Id:           rdf2go.NewResource(baseID),
		Parents:      map[string]bool{},
		Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{
			namePath: {{
				Id:       rdf2go.NewResource("urn:property:name"),
				Datatype: "http://www.w3.org/2001/XMLSchema#string",
			}},
		},
	}

	previousProfiles := rdf.Profiles
	rdf.Profiles = map[string]*shacl.NodeShape{derivedID: derived, middleID: middle, baseID: base}
	t.Cleanup(func() { rdf.Profiles = previousProfiles })

	subject := rdf2go.NewResource("http://example.org/resource")
	graph := rdf2go.NewGraph("")
	graph.AddTriple(subject, rdf2go.NewResource(namePath), rdf2go.NewLiteral("Inherited result"))
	metadata := &rdf.ResourceMetadata{Conformance: map[string][]string{
		subject.RawValue(): {derivedID},
	}}
	doc := document{}

	buildRootQueryDocs(subject, derived, graph, metadata, &doc)

	rootShapes, ok := doc["rootShape"].([]any)
	if !ok || len(rootShapes) != 3 {
		t.Fatalf("expected concrete and inherited root shapes, got %#v", doc["rootShape"])
	}
	for _, shapeID := range []string{derivedID, middleID, baseID} {
		found := false
		for _, indexedShapeID := range rootShapes {
			if indexedShapeID == shapeID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected root shape %q in %#v", shapeID, rootShapes)
		}
	}

	baseField := queryFieldName(baseID, []string{namePath}, "txt")
	if values, ok := doc[baseField].([]any); !ok || len(values) != 1 || values[0] != "Inherited result" {
		t.Fatalf("expected inherited root query field %q, got %#v", baseField, doc[baseField])
	}
}

func TestBuildQueryDocSeparatesQualifiedBranchesWithSameRdfPath(t *testing.T) {
	const (
		rootID              = "http://example.org/Root"
		temperatureID       = "urn:shape:temperature"
		timeID              = "urn:shape:time"
		partPath            = "http://example.org/part"
		quantityKindPath    = "http://example.org/quantityKind"
		temperatureProp     = "urn:property:target-temperature"
		timeProp            = "urn:property:duration"
		temperatureKindProp = "urn:property:temperature-kind"
		timeKindProp        = "urn:property:time-kind"
	)

	root := &shacl.NodeShape{
		Id: rdf2go.NewResource(rootID), Parents: map[string]bool{}, Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{partPath: {
			{Id: rdf2go.NewResource(temperatureProp), QualifiedValueShape: temperatureID, NodeShapes: map[string]bool{}, AlternativeNodeShapes: map[string]bool{}},
			{Id: rdf2go.NewResource(timeProp), QualifiedValueShape: timeID, NodeShapes: map[string]bool{}, AlternativeNodeShapes: map[string]bool{}},
		}},
	}
	temperature := &shacl.NodeShape{
		Id: rdf2go.NewResource(temperatureID), Parents: map[string]bool{}, Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{quantityKindPath: {{Id: rdf2go.NewResource(temperatureKindProp), NodeShapes: map[string]bool{}, AlternativeNodeShapes: map[string]bool{}}}},
	}
	time := &shacl.NodeShape{
		Id: rdf2go.NewResource(timeID), Parents: map[string]bool{}, Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{quantityKindPath: {{Id: rdf2go.NewResource(timeKindProp), NodeShapes: map[string]bool{}, AlternativeNodeShapes: map[string]bool{}}}},
	}

	previousProfiles := rdf.Profiles
	rdf.Profiles = map[string]*shacl.NodeShape{rootID: root, temperatureID: temperature, timeID: time}
	t.Cleanup(func() { rdf.Profiles = previousProfiles })

	rootSubject := rdf2go.NewResource("http://example.org/sample-preparation")
	temperatureSubject := rdf2go.NewResource("http://example.org/target-temperature")
	timeSubject := rdf2go.NewResource("http://example.org/heating-duration")
	temperatureKind := rdf2go.NewResource("http://qudt.org/vocab/quantitykind/Temperature")
	timeKind := rdf2go.NewResource("http://qudt.org/vocab/quantitykind/Time")
	graph := rdf2go.NewGraph("")
	graph.AddTriple(rootSubject, rdf2go.NewResource(partPath), temperatureSubject)
	graph.AddTriple(rootSubject, rdf2go.NewResource(partPath), timeSubject)
	graph.AddTriple(temperatureSubject, rdf2go.NewResource(quantityKindPath), temperatureKind)
	graph.AddTriple(timeSubject, rdf2go.NewResource(quantityKindPath), timeKind)
	metadata := &rdf.ResourceMetadata{Conformance: map[string][]string{
		rootSubject.RawValue():        {rootID},
		temperatureSubject.RawValue(): {temperatureID},
		timeSubject.RawValue():        {timeID},
	}}
	doc := document{}

	buildQueryDoc(rootSubject, root, rootID, nil, nil, graph, metadata, &doc, newQueryTraversalState())

	temperatureField := queryFieldName(rootID, []string{temperatureProp, quantityKindPath}, "ss")
	timeField := queryFieldName(rootID, []string{timeProp, quantityKindPath}, "ss")
	if values := doc[temperatureField]; len(values.([]any)) != 1 || values.([]any)[0] != temperatureKind.String() {
		t.Fatalf("expected only temperature in %q, got %#v", temperatureField, values)
	}
	if values := doc[timeField]; len(values.([]any)) != 1 || values.([]any)[0] != timeKind.String() {
		t.Fatalf("expected only time in %q, got %#v", timeField, values)
	}
}

func TestAppendQueryValueDeduplicatesValues(t *testing.T) {
	doc := document{}
	path := []string{"http://example.org/name"}
	value := rdf2go.NewLiteral("duplicate")

	appendQueryValue(&doc, "http://example.org/Root", path, value)
	appendQueryValue(&doc, "http://example.org/Root", path, value)

	for _, suffix := range []string{"ss", "txt"} {
		field := queryFieldName("http://example.org/Root", path, suffix)
		values, ok := doc[field].([]any)
		if !ok || len(values) != 1 || values[0] != "duplicate" {
			t.Fatalf("expected one unique value in %q, got %#v", field, doc[field])
		}
	}
}

func TestConformsAcceptsMultipleShapes(t *testing.T) {
	metadata := &rdf.ResourceMetadata{Conformance: map[string][]string{
		"resource": []string{"shape-a", "shape-b"},
	}}
	if !conforms("resource", "shape-b", metadata) {
		t.Fatal("expected second recorded shape to conform")
	}
}
