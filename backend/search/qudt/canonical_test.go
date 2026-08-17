package qudt

import "testing"

func TestBuildCatalogPrefersExactSIMatchAndPrunesOtherUnits(t *testing.T) {
	const quantityKind = "http://example.com/frequency"
	source := map[string]*CatalogSourceUnit{
		"exact": {
			Multiplier:        1,
			QuantityKinds:     []string{quantityKind},
			DimensionVector:   "T-1",
			SIDerivedCoherent: true,
			SIExactMatch:      true,
		},
		"equivalent": {
			Multiplier:        1,
			QuantityKinds:     []string{quantityKind},
			DimensionVector:   "T-1",
			SIDerivedCoherent: true,
		},
		"scaled": {
			Multiplier:      1000,
			QuantityKinds:   []string{quantityKind},
			DimensionVector: "T-1",
		},
		"compatible-through-parent": {
			Multiplier:      60,
			QuantityKinds:   []string{"http://example.com/specialized-frequency"},
			DimensionVector: "T-1",
		},
		"unrelated": {
			Multiplier:      1,
			QuantityKinds:   []string{"http://example.com/currency"},
			DimensionVector: "currency",
		},
	}

	catalog := BuildCatalog(source, map[string][]string{
		"http://example.com/specialized-frequency": {quantityKind},
	})
	if got := catalog.CanonicalUnits[quantityKind]; got != "exact" {
		t.Fatalf("canonical unit = %q, want exact", got)
	}
	if got := catalog.CanonicalUnits["http://example.com/specialized-frequency"]; got != "exact" {
		t.Fatalf("inherited canonical unit = %q, want exact", got)
	}
	for _, uri := range []string{"exact", "equivalent", "scaled", "compatible-through-parent"} {
		if catalog.Units[uri] == nil {
			t.Errorf("convertible unit %q was pruned", uri)
		}
	}
	if catalog.Units["unrelated"] != nil {
		t.Error("unit without a canonical target was retained")
	}
}

func TestBuildCatalogRejectsAmbiguousCandidates(t *testing.T) {
	const quantityKind = "http://example.com/quantity-kind"
	catalog := BuildCatalog(map[string]*CatalogSourceUnit{
		"a": {
			Multiplier:        1,
			QuantityKinds:     []string{quantityKind},
			DimensionVector:   "L1",
			SIDerivedCoherent: true,
		},
		"b": {
			Multiplier:        1,
			QuantityKinds:     []string{quantityKind},
			DimensionVector:   "L1",
			SIDerivedCoherent: true,
		},
	}, nil)
	if canonical := catalog.CanonicalUnits[quantityKind]; canonical != "" {
		t.Errorf("ambiguous canonical unit = %q, want empty", canonical)
	}
	if len(catalog.Units) != 0 {
		t.Errorf("retained %d units without a canonical target", len(catalog.Units))
	}
}

func TestBuildCatalogRejectsMissingConversionMetadata(t *testing.T) {
	const quantityKind = "http://example.com/length"
	catalog := BuildCatalog(map[string]*CatalogSourceUnit{
		"target": {
			Multiplier:      1,
			QuantityKinds:   []string{quantityKind},
			SIExactMatch:    true,
			DimensionVector: "",
		},
	}, nil)
	if len(catalog.CanonicalUnits) != 0 || len(catalog.Units) != 0 {
		t.Error("unit with a missing dimension vector was retained")
	}
}

func TestBuildCatalogRejectsAmbiguousInheritedTarget(t *testing.T) {
	const child = "child"
	catalog := BuildCatalog(map[string]*CatalogSourceUnit{
		"target-a": {
			Multiplier:      1,
			QuantityKinds:   []string{"parent-a"},
			DimensionVector: "L1",
			SIExactMatch:    true,
		},
		"target-b": {
			Multiplier:      1,
			QuantityKinds:   []string{"parent-b"},
			DimensionVector: "L1",
			SIExactMatch:    true,
		},
		"child-unit": {
			Multiplier:      2,
			QuantityKinds:   []string{child},
			DimensionVector: "L1",
		},
	}, map[string][]string{
		child: {"parent-a", "parent-b"},
	})
	if canonical := catalog.CanonicalUnits[child]; canonical != "" {
		t.Errorf("ambiguous inherited canonical unit = %q, want empty", canonical)
	}
	if catalog.Units["child-unit"] != nil {
		t.Error("unit with ambiguous inherited targets was retained")
	}
}
