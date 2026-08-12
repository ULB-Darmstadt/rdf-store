package search

import (
	"testing"

	"github.com/deiu/rdf2go"

	"rdf-store-backend/rdf"
	"rdf-store-backend/shacl"
)

func TestQueryPathIDIsStable(t *testing.T) {
	name := queryPathID([]string{
		"http://example.org/child",
		"http://example.org/score",
	})
	const expected = "5f508e2ada4ba4124cc00d65f039588f"
	if name != expected {
		t.Fatalf("expected query field %q, got %q", expected, name)
	}
}

func valueChildren(doc document, path []string, field string) []any {
	pathID := queryPathID(path)
	children, _ := doc["_childDocuments_"].([]any)
	values := []any{}
	for _, raw := range children {
		child, ok := raw.(document)
		if !ok || child["path"] != pathID {
			continue
		}
		if value, ok := child[field]; ok {
			values = append(values, value)
		}
	}
	return values
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

	path := []string{childPath, scorePath}
	if values := valueChildren(childDoc, path, "valueNumber"); len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected nested numeric value document, got %#v", values)
	}
	// The child does not conform to the root shape, so the value is mirrored
	// onto the root document (the nearest conforming ancestor) to make nested
	// criteria match a conforming hit.
	if values := valueChildren(rootDoc, path, "valueNumber"); len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected root document to carry deep value document, got %#v", values)
	}
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
	shapes, ok := doc["shape"].([]any)
	if !ok || len(shapes) != 3 {
		t.Fatalf("expected concrete and inherited conforming shapes, got %#v", doc["shape"])
	}
	for _, shapeID := range []string{derivedID, middleID, baseID} {
		found := false
		for _, indexedShapeID := range shapes {
			if indexedShapeID == shapeID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected conforming shape %q in %#v", shapeID, shapes)
		}
	}

	if values := valueChildren(doc, []string{namePath}, "valueText"); len(values) != 1 || values[0] != "Inherited result" {
		t.Fatalf("expected inherited root value document, got %#v", values)
	}
}

func TestBuildResourceDocumentsCreatesEntityDocuments(t *testing.T) {
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
	if shapes, ok := (*rootDoc)["shape"].([]any); !ok || len(shapes) != 1 || shapes[0] != fx.rootID {
		t.Fatalf("expected root document to contain only its own shape %q, got %#v", fx.rootID, (*rootDoc)["shape"])
	}
	deepPath := []string{fx.childPath, fx.valuePath}
	// The measurement does not conform to the root shape, so its value is also
	// stored on the resource document (the conforming ancestor) to keep nested
	// criteria matchable against a conforming hit.
	if values := valueChildren(*rootDoc, deepPath, "valueNumber"); len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected deep value document on resource document, got %#v", values)
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
	if values := valueChildren(*childDoc, deepPath, "valueNumber"); len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected deep value document on entity document, got %#v", values)
	}
}

func TestBuildResourceDocumentsIndexesEmbeddedEntityQueryFields(t *testing.T) {
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
	if values := valueChildren(*childDoc, []string{fx.valuePath}, "valueNumber"); len(values) != 1 || values[0] != "12.5" {
		t.Fatalf("expected embedded entity value document, got %#v", values)
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

	temperaturePath := []string{temperatureProp, quantityKindPath}
	timePath := []string{timeProp, quantityKindPath}
	if values := valueChildren(temperatureDoc, temperaturePath, "valueString"); len(values) != 1 || values[0] != temperatureKind.String() {
		t.Fatalf("expected only temperature, got %#v", values)
	}
	if values := valueChildren(timeDoc, timePath, "valueString"); len(values) != 1 || values[0] != timeKind.String() {
		t.Fatalf("expected only time, got %#v", values)
	}
	if values := valueChildren(temperatureDoc, timePath, "valueString"); len(values) != 0 {
		t.Fatalf("expected temperature document not to carry time, got %#v", values)
	}
	if values := valueChildren(timeDoc, temperaturePath, "valueString"); len(values) != 0 {
		t.Fatalf("expected time document not to carry temperature, got %#v", values)
	}
}

func TestAppendQueryValueDeduplicatesValues(t *testing.T) {
	doc := document{}
	path := []string{"http://example.org/name"}
	value := rdf2go.NewLiteral("duplicate")
	keys := map[string]map[string]bool{}

	appendQueryValue(&doc, path, value, keys)
	appendQueryValue(&doc, path, value, keys)

	values := valueChildren(doc, path, "valueText")
	if len(values) != 1 || values[0] != "duplicate" {
		t.Fatalf("expected one unique value document, got %#v", values)
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

func TestBuildQueryDocIndexesStructuredDashFacetAsLeaf(t *testing.T) {
	const (
		rootID    = "http://example.org/Hardware"
		personID  = "http://example.org/Person"
		ownerPath = "http://dbpedia.org/ontology/owner"
		firstName = "http://xmlns.com/foaf/0.1/firstName"
	)
	facet := true

	root := &shacl.NodeShape{
		Id: rdf2go.NewResource(rootID), Parents: map[string]bool{}, Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{ownerPath: {{
			Id:         rdf2go.NewResource("urn:property:owner"),
			NodeShapes: map[string]bool{personID: true},
			Facet:      &facet,
		}}},
	}
	person := &shacl.NodeShape{
		Id: rdf2go.NewResource(personID), Parents: map[string]bool{}, Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{firstName: {{
			Id:       rdf2go.NewResource("urn:property:firstName"),
			Datatype: "http://www.w3.org/2001/XMLSchema#string",
		}}},
	}

	previousProfiles := rdf.Profiles
	rdf.Profiles = map[string]*shacl.NodeShape{rootID: root, personID: person}
	t.Cleanup(func() { rdf.Profiles = previousProfiles })

	hardware := rdf2go.NewResource("http://example.org/resource/1")
	owner := rdf2go.NewResource("http://example.org/person/1")
	graph := rdf2go.NewGraph("")
	graph.AddTriple(hardware, rdf2go.NewResource(ownerPath), owner)
	graph.AddTriple(owner, rdf2go.NewResource(firstName), rdf2go.NewLiteral("Leonard"))
	metadata := &rdf.ResourceMetadata{Conformance: map[string][]string{
		hardware.RawValue(): {rootID},
		owner.RawValue():    {personID},
	}}

	hardwareDoc := document{}
	ownerDoc := document{}
	traversal := newQueryTraversalState()
	traversal.entities = map[string]*document{
		hardware.RawValue(): &hardwareDoc,
		owner.RawValue():    &ownerDoc,
	}

	buildQueryDoc(hardware, root, rootID, nil, nil, graph, metadata, &hardwareDoc, &hardwareDoc, traversal)

	// dash:facet marks the structured owner relationship as a leaf facet, so the
	// owner resource ID is indexed at the [owner] path.
	if values := valueChildren(hardwareDoc, []string{ownerPath}, "valueString"); len(values) != 1 || values[0] != owner.String() {
		t.Fatalf("expected owner reference, got %#v", values)
	}
	// The nested person properties are no longer recursed into.
	if values := valueChildren(hardwareDoc, []string{ownerPath, firstName}, "valueText"); len(values) != 0 {
		t.Fatalf("expected no nested person value, got %#v", values)
	}
}

func TestBuildQueryDocUsesNodeShapeDashFacetAsDefault(t *testing.T) {
	const (
		rootID    = "http://example.org/Hardware"
		personID  = "http://example.org/Person"
		ownerPath = "http://example.org/owner"
	)
	facet := true
	root := &shacl.NodeShape{
		Id: rdf2go.NewResource(rootID), Parents: map[string]bool{}, Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{ownerPath: {{
			Id: rdf2go.NewResource("urn:property:owner"), NodeShapes: map[string]bool{personID: true},
		}}},
	}
	person := &shacl.NodeShape{Id: rdf2go.NewResource(personID), Parents: map[string]bool{}, Alternatives: map[string]bool{}, Properties: map[string][]*shacl.Property{}, Facet: &facet}
	previousProfiles := rdf.Profiles
	rdf.Profiles = map[string]*shacl.NodeShape{rootID: root, personID: person}
	t.Cleanup(func() { rdf.Profiles = previousProfiles })

	hardware := rdf2go.NewResource("http://example.org/hardware")
	owner := rdf2go.NewResource("http://example.org/person")
	graph := rdf2go.NewGraph("")
	graph.AddTriple(hardware, rdf2go.NewResource(ownerPath), owner)
	metadata := &rdf.ResourceMetadata{Conformance: map[string][]string{hardware.RawValue(): {rootID}, owner.RawValue(): {personID}}}
	doc := document{}
	ownerDoc := document{}
	traversal := newQueryTraversalState()
	traversal.entities = map[string]*document{hardware.RawValue(): &doc, owner.RawValue(): &ownerDoc}

	buildQueryDoc(hardware, root, rootID, nil, nil, graph, metadata, &doc, &doc, traversal)
	if values := valueChildren(doc, []string{ownerPath}, "valueString"); len(values) != 1 || values[0] != owner.String() {
		t.Fatalf("expected node-shape facet value, got %#v", values)
	}
}

func TestStructuredPropertyFacetOverridesNodeShapeDefault(t *testing.T) {
	shapeFacet := true
	propertyFacet := false
	const personID = "http://example.org/Person"
	previousProfiles := rdf.Profiles
	rdf.Profiles = map[string]*shacl.NodeShape{personID: {Facet: &shapeFacet}}
	t.Cleanup(func() { rdf.Profiles = previousProfiles })
	property := &shacl.Property{Facet: &propertyFacet}
	if structuredPropertyIsFacet(property, map[string]bool{personID: true}) {
		t.Fatal("dash:facet false on the property must override the node-shape default")
	}
}

func TestBuildQueryDocRecursesUnannotatedStructuredProperty(t *testing.T) {
	const (
		rootID     = "http://example.org/Hardware"
		partID     = "http://example.org/Part"
		partPath   = "http://example.org/hasPart"
		modelPath  = "http://example.org/model"
		modelValue = "M-42"
		stringType = "http://www.w3.org/2001/XMLSchema#string"
	)

	root := &shacl.NodeShape{
		Id: rdf2go.NewResource(rootID), Parents: map[string]bool{}, Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{partPath: {{
			Id:         rdf2go.NewResource("urn:property:part"),
			NodeShapes: map[string]bool{partID: true},
		}}},
	}
	// Without dash:facet, the structured property keeps recursing into its shape.
	part := &shacl.NodeShape{
		Id: rdf2go.NewResource(partID), Parents: map[string]bool{}, Alternatives: map[string]bool{},
		Properties: map[string][]*shacl.Property{modelPath: {{
			Id:         rdf2go.NewResource("urn:property:model"),
			Datatype:   stringType,
			NodeShapes: map[string]bool{},
		}}},
	}

	previousProfiles := rdf.Profiles
	rdf.Profiles = map[string]*shacl.NodeShape{rootID: root, partID: part}
	t.Cleanup(func() { rdf.Profiles = previousProfiles })

	hardware := rdf2go.NewResource("http://example.org/resource/2")
	partSubject := rdf2go.NewResource("http://example.org/part/2")
	graph := rdf2go.NewGraph("")
	graph.AddTriple(hardware, rdf2go.NewResource(partPath), partSubject)
	graph.AddTriple(partSubject, rdf2go.NewResource(modelPath), rdf2go.NewLiteral(modelValue))
	metadata := &rdf.ResourceMetadata{Conformance: map[string][]string{
		hardware.RawValue():    {rootID},
		partSubject.RawValue(): {partID},
	}}

	hardwareDoc := document{}
	partDoc := document{}
	traversal := newQueryTraversalState()
	traversal.entities = map[string]*document{
		hardware.RawValue():    &hardwareDoc,
		partSubject.RawValue(): &partDoc,
	}

	buildQueryDoc(hardware, root, rootID, nil, nil, graph, metadata, &hardwareDoc, &hardwareDoc, traversal)

	if values := valueChildren(partDoc, []string{partPath, modelPath}, "valueText"); len(values) != 1 || values[0] != modelValue {
		t.Fatalf("expected nested part value, got %#v", values)
	}
	// The part is not collapsed into a leaf reference.
	if values := valueChildren(hardwareDoc, []string{partPath}, "valueString"); len(values) != 0 {
		t.Fatalf("expected no leaf part value, got %#v", values)
	}
}
