package search

import "testing"

func TestCollectionSchemaUsesFixedValueFields(t *testing.T) {
	want := map[string]bool{
		"resourceId": false, "subject": false, "docType": false,
		"label": false, "shape": false, "creator": false, "lastModified": false,
		"path": false, "valueString": false, "valueText": false,
		"valueNumber": false, "valueDate": false, "valueBoolean": false,
		"valueGeo": false, "datatype": false, "language": false,
	}
	fields := createCollectionSchema()
	if len(fields) != len(want) {
		t.Fatalf("expected %d fixed fields, got %d", len(want), len(fields))
	}
	for _, field := range fields {
		if _, ok := want[field.Name]; !ok {
			t.Errorf("unexpected schema field %q", field.Name)
		} else if want[field.Name] {
			t.Errorf("duplicate schema field %q", field.Name)
		}
		want[field.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing schema field %q", name)
		}
	}
}

func TestCollectionSchemaMakesGroupedValueFieldsMultiValued(t *testing.T) {
	want := map[string]bool{
		"valueString": false, "valueText": false, "valueNumber": false,
		"valueDate": false, "valueBoolean": false, "valueGeo": false,
		"datatype": false, "language": false,
	}
	for _, field := range createCollectionSchema() {
		if _, ok := want[field.Name]; !ok {
			continue
		}
		want[field.Name] = true
		if !field.MultiValued {
			t.Errorf("expected grouped field %q to be multivalued", field.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing grouped field %q", name)
		}
	}
}
