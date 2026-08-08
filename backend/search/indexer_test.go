package search

import (
	"strings"
	"testing"

	"github.com/deiu/rdf2go"

	"rdf-store-backend/base"
	"rdf-store-backend/rdf"
	"rdf-store-backend/shacl"
)

func TestQueryFieldNameIsStable(t *testing.T) {
	name := queryFieldName([]string{
		"http://example.org/child",
		"http://example.org/score",
	}, "ds")
	const expected = "_query_.5f508e2ada4ba4124cc00d65f039588f_ds"
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

func TestOrderShapesBySpecificity(t *testing.T) {
	baseID := "http://example.org/Base"
	middleID := "http://example.org/Middle"
	specificID := "http://example.org/Specific"
	siblingID := "http://example.org/Sibling"
	urnID := "urn:24580123-e801-451a-9bcf-2406c6a73b86"

	previousProfiles := rdf.Profiles
	rdf.Profiles = map[string]*shacl.NodeShape{
		baseID:     {Id: rdf2go.NewResource(baseID), Parents: map[string]bool{}},
		middleID:   {Id: rdf2go.NewResource(middleID), Parents: map[string]bool{baseID: true}},
		specificID: {Id: rdf2go.NewResource(specificID), Parents: map[string]bool{middleID: true}},
		siblingID:  {Id: rdf2go.NewResource(siblingID), Parents: map[string]bool{baseID: true}},
	}
	t.Cleanup(func() { rdf.Profiles = previousProfiles })

	ordered := orderShapesBySpecificity([]string{urnID, middleID, baseID, siblingID, specificID})
	expected := []string{specificID, middleID, siblingID, baseID, urnID}
	for i := range expected {
		if ordered[i] != expected[i] {
			t.Fatalf("expected shape order %v, got %v", expected, ordered)
		}
	}
}

func TestOrderShapesBySpecificityKeepsSingleNamedProfileFirst(t *testing.T) {
	baseID := "http://example.org/Base"
	urnID := "urn:24580123-e801-451a-9bcf-2406c6a73b86"

	ordered := orderShapesBySpecificity([]string{urnID, baseID})
	if ordered[0] != baseID || ordered[1] != urnID {
		t.Fatalf("expected named profile first, got %v", ordered)
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
	rootDoc := document{}
	childDoc := document{}
	traversal := newQueryTraversalState()
	traversal.entities = map[string]*document{
		rootSubject.RawValue():  &rootDoc,
		childSubject.RawValue(): &childDoc,
	}

	buildQueryDoc(rootSubject, root, rootID, nil, nil, graph, metadata, &rootDoc, &rootDoc, traversal)

	field := queryFieldName([]string{childPath, scorePath}, "ds")
	if values, ok := childDoc[field].([]any); !ok || len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected nested numeric query field %q, got %#v", field, childDoc[field])
	}
	// The child does not conform to the root shape, so the value is mirrored
	// onto the root document (the nearest conforming ancestor) to make nested
	// criteria match a conforming hit.
	if values, ok := rootDoc[field].([]any); !ok || len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected root document to carry deep query field %q, got %#v", field, rootDoc[field])
	}
}

// setShaclQueryMode pins the feature flag for the duration of a test.
func setShaclQueryMode(t *testing.T, enabled bool) {
	t.Helper()
	previous := base.Configuration.ShaclQueryMode
	base.Configuration.ShaclQueryMode = enabled
	t.Cleanup(func() { base.Configuration.ShaclQueryMode = previous })
}

type measurementFixture struct {
	graph      *rdf2go.Graph
	metadata   *rdf.ResourceMetadata
	resourceID string
	childID    string
	rootID     string
	childPath  string
	valuePath  string
}

// newMeasurementFixture builds a graph of a root resource with a nested
// measurement entity and the shapes both conform to.
func newMeasurementFixture(t *testing.T) *measurementFixture {
	t.Helper()
	const (
		rootID      = "http://example.org/Root"
		measurement = "http://example.org/Measurement"
		valuePath   = "http://example.org/value"
		childPath   = "http://example.org/hasMeasurement"
	)

	root := &shacl.NodeShape{
		Id:           rdf2go.NewResource(rootID),
		Parents:      map[string]bool{},
		Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{
			childPath: {{
				Id:         rdf2go.NewResource("urn:property:hasMeasurement"),
				NodeShapes: map[string]bool{measurement: true},
			}},
		},
	}
	measurementShape := &shacl.NodeShape{
		Id:           rdf2go.NewResource(measurement),
		Parents:      map[string]bool{},
		Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{
			valuePath: {{
				Id:       rdf2go.NewResource("urn:property:value"),
				Datatype: "http://www.w3.org/2001/XMLSchema#decimal",
			}},
		},
	}

	previousProfiles := rdf.Profiles
	rdf.Profiles = map[string]*shacl.NodeShape{rootID: root, measurement: measurementShape}
	t.Cleanup(func() { rdf.Profiles = previousProfiles })

	resourceID := "http://example.org/resource/1"
	rootSubject := rdf2go.NewResource(resourceID)
	childSubject := rdf2go.NewResource("http://example.org/resource/1/measurement/1")
	graph := rdf2go.NewGraph("")
	graph.AddTriple(rootSubject, rdf2go.NewResource(childPath), childSubject)
	graph.AddTriple(childSubject, rdf2go.NewResource(valuePath), rdf2go.NewLiteralWithDatatype("12.5", rdf2go.NewResource("http://www.w3.org/2001/XMLSchema#decimal")))
	graph.AddTriple(rootSubject, shacl.DCTERMS_CONFORMS_TO, rdf2go.NewResource(rootID))
	metadata := &rdf.ResourceMetadata{
		Id:      rootSubject,
		Creator: "tester",
		Conformance: map[string][]string{
			resourceID:              {rootID},
			childSubject.RawValue(): {measurement},
		},
	}
	return &measurementFixture{
		graph:      graph,
		metadata:   metadata,
		resourceID: resourceID,
		childID:    childSubject.RawValue(),
		rootID:     rootID,
		childPath:  childPath,
		valuePath:  valuePath,
	}
}

func TestBuildResourceDocumentsIndexesInheritedShapeContexts(t *testing.T) {
	setShaclQueryMode(t, true)
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
	graph.AddTriple(subject, shacl.DCTERMS_CONFORMS_TO, rdf2go.NewResource(derivedID))
	metadata := &rdf.ResourceMetadata{
		Id: subject,
		Conformance: map[string][]string{
			subject.RawValue(): {derivedID},
		},
	}

	docs, err := buildResourceDocuments(graph, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected one document, got %d", len(docs))
	}
	doc := *docs[0]

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

	baseField := queryFieldName([]string{namePath}, "txt")
	if values, ok := doc[baseField].([]any); !ok || len(values) != 1 || values[0] != "Inherited result" {
		t.Fatalf("expected inherited root query field %q, got %#v", baseField, doc[baseField])
	}
}

func TestBuildResourceDocumentsCreatesEntityDocuments(t *testing.T) {
	setShaclQueryMode(t, true)
	fx := newMeasurementFixture(t)

	docs, err := buildResourceDocuments(fx.graph, fx.metadata)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected two entity documents, got %d", len(docs))
	}
	var childDoc *document
	for _, doc := range docs {
		if (*doc)["subject"] == fx.childID {
			childDoc = doc
		}
	}
	if childDoc == nil {
		t.Fatal("expected an entity document for the measurement")
	}
	var rootDoc *document
	for _, doc := range docs {
		if (*doc)["subject"] == fx.resourceID {
			rootDoc = doc
		}
	}
	if rootDoc == nil {
		t.Fatal("expected a document for the resource")
	}
	deepField := queryFieldName([]string{fx.childPath, fx.valuePath}, "ds")
	// The measurement does not conform to the root shape, so its value is also
	// stored on the resource document (the conforming ancestor) to keep nested
	// criteria matchable against a conforming hit.
	if values, ok := (*rootDoc)[deepField].([]any); !ok || len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected deep query field %q on resource document, got %#v", deepField, (*rootDoc)[deepField])
	}
	if (*childDoc)["id"] != fx.resourceID+"|"+fx.childID {
		t.Fatalf("unexpected entity document id %#v", (*childDoc)["id"])
	}
	if (*childDoc)["resourceId"] != fx.resourceID {
		t.Fatalf("expected resourceId %q, got %#v", fx.resourceID, (*childDoc)["resourceId"])
	}
	if shapes, ok := (*childDoc)["shape"].([]any); !ok || len(shapes) != 1 || shapes[0] != "http://example.org/Measurement" {
		t.Fatalf("expected shape %q on entity document, got %#v", "http://example.org/Measurement", (*childDoc)["shape"])
	}
	if roots, ok := (*childDoc)["rootShape"].([]any); !ok || len(roots) != 1 || roots[0] != fx.rootID {
		t.Fatalf("expected rootShape %q on entity document, got %#v", fx.rootID, (*childDoc)["rootShape"])
	}
	if values, ok := (*childDoc)[deepField].([]any); !ok || len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected deep query field %q on entity document, got %#v", deepField, (*childDoc)[deepField])
	}
}

func TestBuildResourceDocumentsIndexesEmbeddedEntityQueryFields(t *testing.T) {
	setShaclQueryMode(t, true)
	fx := newMeasurementFixture(t)

	docs, err := buildResourceDocuments(fx.graph, fx.metadata)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected two entity documents, got %d", len(docs))
	}
	var childDoc *document
	for _, doc := range docs {
		if (*doc)["subject"] == fx.childID {
			childDoc = doc
		}
	}
	if childDoc == nil {
		t.Fatal("expected an entity document for the measurement")
	}
	// The embedded measurement is traversed from its own shape, so its value is
	// indexed under the measurement-relative path. Without this, facets scoped
	// to the measurement shape would miss values of embedded measurements.
	ownField := queryFieldName([]string{fx.valuePath}, "ds")
	if values, ok := (*childDoc)[ownField].([]any); !ok || len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected embedded entity query field %q, got %#v", ownField, (*childDoc)[ownField])
	}
}

func TestBuildResourceDocumentsSkipsQueryFieldsWithoutQueryMode(t *testing.T) {
	setShaclQueryMode(t, false)
	fx := newMeasurementFixture(t)

	docs, err := buildResourceDocuments(fx.graph, fx.metadata)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected two entity documents, got %d", len(docs))
	}
	for _, doc := range docs {
		for field := range *doc {
			if strings.HasPrefix(field, "_query_.") {
				t.Errorf("expected no query fields without SHACL_QUERY_MODE, got %q", field)
			}
		}
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
	rootDoc := document{}
	temperatureDoc := document{}
	timeDoc := document{}
	traversal := newQueryTraversalState()
	traversal.entities = map[string]*document{
		rootSubject.RawValue():        &rootDoc,
		temperatureSubject.RawValue(): &temperatureDoc,
		timeSubject.RawValue():        &timeDoc,
	}

	buildQueryDoc(rootSubject, root, rootID, nil, nil, graph, metadata, &rootDoc, &rootDoc, traversal)

	temperatureField := queryFieldName([]string{temperatureProp, quantityKindPath}, "ss")
	timeField := queryFieldName([]string{timeProp, quantityKindPath}, "ss")
	if values := temperatureDoc[temperatureField]; len(values.([]any)) != 1 || values.([]any)[0] != temperatureKind.String() {
		t.Fatalf("expected only temperature in %q, got %#v", temperatureField, temperatureDoc[temperatureField])
	}
	if values := timeDoc[timeField]; len(values.([]any)) != 1 || values.([]any)[0] != timeKind.String() {
		t.Fatalf("expected only time in %q, got %#v", timeField, timeDoc[timeField])
	}
	if _, ok := temperatureDoc[timeField]; ok {
		t.Fatalf("expected temperature document not to carry %q, got %#v", timeField, temperatureDoc[timeField])
	}
	if _, ok := timeDoc[temperatureField]; ok {
		t.Fatalf("expected time document not to carry %q, got %#v", temperatureField, timeDoc[temperatureField])
	}
}

func TestAppendQueryValueDeduplicatesValues(t *testing.T) {
	doc := document{}
	path := []string{"http://example.org/name"}
	value := rdf2go.NewLiteral("duplicate")

	appendQueryValue(&doc, path, value)
	appendQueryValue(&doc, path, value)

	for _, suffix := range []string{"ss", "txt"} {
		field := queryFieldName(path, suffix)
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
