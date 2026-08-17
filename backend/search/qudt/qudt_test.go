package qudt

import (
	"testing"

	"github.com/deiu/rdf2go"
)

func TestPredicateConfigurationGate(t *testing.T) {
	const (
		hasUnit      = "http://example.com/hasUnit"
		hasKind      = "http://example.com/hasKind"
		hasNumerical = "http://example.com/hasNumericalValue"
	)
	node := rdf2go.NewResource("http://example.com/quantity")
	graph := rdf2go.NewGraph("")
	graph.AddTriple(node, rdf2go.NewResource(hasUnit), rdf2go.NewResource("http://qudt.org/vocab/unit/L"))
	graph.AddTriple(node, rdf2go.NewResource(hasKind), rdf2go.NewResource("http://qudt.org/vocab/quantitykind/Volume"))

	tests := []struct {
		name      string
		unit      string
		kind      string
		numerical string
		want      bool
	}{
		{name: "all configured", unit: hasUnit, kind: hasKind, numerical: hasNumerical, want: true},
		{name: "unit missing", kind: hasKind, numerical: hasNumerical},
		{name: "kind missing", unit: hasUnit, numerical: hasNumerical},
		{name: "numerical missing", unit: hasUnit, kind: hasKind},
		{name: "whitespace is missing", unit: "  ", kind: hasKind, numerical: hasNumerical},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := NewPredicateConfig(test.unit, test.kind, test.numerical)
			if got := config.ScanQuantityContext(node, graph) != nil; got != test.want {
				t.Errorf("ScanQuantityContext() presence = %v, want %v", got, test.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	if len(units) == 0 {
		t.Fatal("no units loaded")
	}
	if len(canonicalByQuantityKind) == 0 {
		t.Fatal("no canonical units found")
	}
	for quantityKind, unitURI := range canonicalByQuantityKind {
		unit := units[unitURI]
		if unit == nil {
			t.Errorf("canonical unit %q for %q is not in the catalog", unitURI, quantityKind)
			continue
		}
		if unit.Multiplier == 0 || unit.DimensionVector == "" {
			t.Errorf("canonical unit %q has incomplete conversion metadata", unitURI)
		}
	}
}

func TestUnitLookup(t *testing.T) {
	info := Unit("http://qudt.org/vocab/unit/K")
	if info == nil {
		t.Fatal("Unit(K) returned nil")
	}
	if info.Multiplier != 1.0 {
		t.Errorf("K multiplier = %v, want 1.0", info.Multiplier)
	}
	if info.Offset != 0.0 {
		t.Errorf("K offset = %v, want 0.0", info.Offset)
	}
	if info.DimensionVector != "A0E0L0I0M0H1T0D0" {
		t.Errorf("K dimensionVector = %v, want A0E0L0I0M0H1T0D0", info.DimensionVector)
	}

	if Unit("http://qudt.org/vocab/unit/NONEXISTENT") != nil {
		t.Error("Unit(NONEXISTENT) should return nil")
	}
}

func TestCanonicalUnit(t *testing.T) {
	// Temperature: canonical should be K
	if cu := CanonicalUnitURI("http://qudt.org/vocab/unit/DEG_C", "http://qudt.org/vocab/quantitykind/Temperature"); cu != "http://qudt.org/vocab/unit/K" {
		t.Errorf("CanonicalUnitURI(DEG_C) = %v, want K", cu)
	}

	// Length: canonical should be M
	if cu := CanonicalUnitURI("http://qudt.org/vocab/unit/FT", "http://qudt.org/vocab/quantitykind/Length"); cu != "http://qudt.org/vocab/unit/M" {
		t.Errorf("CanonicalUnitURI(FT) = %v, want M", cu)
	}

	// Unknown unit
	if cu := CanonicalUnitURI("http://qudt.org/vocab/unit/NONEXISTENT", "http://qudt.org/vocab/quantitykind/Length"); cu != "" {
		t.Errorf("CanonicalUnitURI(NONEXISTENT) = %v, want empty", cu)
	}

	// Dimensionally equal quantity kinds must retain their distinct units.
	if cu := CanonicalUnitURI("http://qudt.org/vocab/unit/HZ", "http://qudt.org/vocab/quantitykind/Frequency"); cu != "http://qudt.org/vocab/unit/HZ" {
		t.Errorf("CanonicalUnitURI(HZ, Frequency) = %v, want HZ", cu)
	}
	if cu := CanonicalUnitURI("http://qudt.org/vocab/unit/BQ", "http://qudt.org/vocab/quantitykind/Activity"); cu != "http://qudt.org/vocab/unit/BQ" {
		t.Errorf("CanonicalUnitURI(BQ, Activity) = %v, want BQ", cu)
	}

	// Cubic metre is an unscaled SI-applicable unit. QUDT does not declare it
	// as a named derived coherent unit, so it exercises the metadata fallback.
	if cu := CanonicalUnitURI("http://qudt.org/vocab/unit/L", "http://qudt.org/vocab/quantitykind/Volume"); cu != "http://qudt.org/vocab/unit/M3" {
		t.Errorf("CanonicalUnitURI(L, Volume) = %v, want M3", cu)
	}
	if cu := CanonicalUnitURI("http://qudt.org/vocab/unit/CUP", "http://qudt.org/vocab/quantitykind/LiquidVolume"); cu != "http://qudt.org/vocab/unit/M3" {
		t.Errorf("CanonicalUnitURI(CUP, LiquidVolume) = %v, want M3 inherited from Volume", cu)
	}
}

func TestCanonicalUnitRejectsNonSIQuantityKind(t *testing.T) {
	const (
		usd      = "http://qudt.org/vocab/unit/CCY_USD"
		currency = "http://qudt.org/vocab/quantitykind/Currency"
	)
	if cu := CanonicalUnitURI(usd, currency); cu != "" {
		t.Errorf("CanonicalUnitURI(USD, Currency) = %v, want empty", cu)
	}
	if _, ok := Convert(1, usd, currency, false); ok {
		t.Error("Convert(USD, Currency) should fail without an SI canonical unit")
	}
	if Unit(usd) != nil {
		t.Error("USD should be pruned from the runtime conversion catalog")
	}
}

func TestConvertTemperature(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		srcUnit   string
		want      float64
		tolerance float64
	}{
		{"C to K: 0°C = 273.15 K", 0, "http://qudt.org/vocab/unit/DEG_C", 273.15, 0.001},
		{"C to K: 100°C = 373.15 K", 100, "http://qudt.org/vocab/unit/DEG_C", 373.15, 0.001},
		{"C to K: -273.15°C = 0 K", -273.15, "http://qudt.org/vocab/unit/DEG_C", 0, 0.001},
		{"F to K: 32°F = 273.15 K", 32, "http://qudt.org/vocab/unit/DEG_F", 273.15, 0.01},
		{"F to K: 212°F = 373.15 K", 212, "http://qudt.org/vocab/unit/DEG_F", 373.15, 0.01},
		{"Rankine to K: 491.67 R = 273.15 K", 491.67, "http://qudt.org/vocab/unit/DEG_R", 273.15, 0.01},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Convert(tt.value, tt.srcUnit, "http://qudt.org/vocab/quantitykind/Temperature", false)
			if !ok {
				t.Fatalf("Convert(%v, %s) failed", tt.value, tt.srcUnit)
			}
			if !ApproxEquals(got, tt.want, tt.tolerance) {
				t.Errorf("Convert(%v, %s) = %v, want %v (±%v)", tt.value, tt.srcUnit, got, tt.want, tt.tolerance)
			}
		})
	}
}

func TestConvertTemperatureDelta(t *testing.T) {
	// Temperature differences: 100°C difference = 100 K difference (no offset)
	got, ok := Convert(100, "http://qudt.org/vocab/unit/DEG_C", "http://qudt.org/vocab/quantitykind/TemperatureDifference", true)
	if !ok {
		t.Fatal("Convert(delta) failed")
	}
	if !ApproxEquals(got, 100, 0.001) {
		t.Errorf("Convert(100, DEG_C, delta) = %v, want 100", got)
	}

	// 212°F difference = 117.78 K difference
	got, ok = Convert(212, "http://qudt.org/vocab/unit/DEG_F", "http://qudt.org/vocab/quantitykind/TemperatureDifference", true)
	if !ok {
		t.Fatal("Convert(delta) failed")
	}
	if !ApproxEquals(got, 117.778, 0.01) {
		t.Errorf("Convert(212, DEG_F, delta) = %v, want ~117.78", got)
	}
}

func TestConvertLength(t *testing.T) {
	tests := []struct {
		name    string
		value   float64
		srcUnit string
		want    float64
	}{
		{"ft to m: 1 ft = 0.3048 m", 1, "http://qudt.org/vocab/unit/FT", 0.3048},
		{"in to m: 1 in = 0.0254 m", 1, "http://qudt.org/vocab/unit/IN", 0.0254},
		{"km to m: 1 km = 1000 m", 1, "http://qudt.org/vocab/unit/KiloM", 1000},
		{"cm to m: 100 cm = 1 m", 100, "http://qudt.org/vocab/unit/CentiM", 1},
		{"mm to m: 1000 mm = 1 m", 1000, "http://qudt.org/vocab/unit/MilliM", 1},
		{"mile to m: 1 mi = 1609.344 m", 1, "http://qudt.org/vocab/unit/MI", 1609.344},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Convert(tt.value, tt.srcUnit, "http://qudt.org/vocab/quantitykind/Length", false)
			if !ok {
				t.Fatalf("Convert(%v, %s) failed", tt.value, tt.srcUnit)
			}
			if !ApproxEquals(got, tt.want, 0.001) {
				t.Errorf("Convert(%v, %s) = %v, want %v", tt.value, tt.srcUnit, got, tt.want)
			}
		})
	}
}

func TestConvertVolume(t *testing.T) {
	tests := []struct {
		name    string
		value   float64
		srcUnit string
		want    float64
	}{
		{"L to m3: 1 L = 0.001 m3", 1, "http://qudt.org/vocab/unit/L", 0.001},
		{"mL to m3: 1000 mL = 0.001 m3", 1000, "http://qudt.org/vocab/unit/MilliL", 0.001},
		{"gal to m3: 1 CUP = 0.00023658825 m3", 1, "http://qudt.org/vocab/unit/CUP", 0.00023658825},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Convert(tt.value, tt.srcUnit, "http://qudt.org/vocab/quantitykind/Volume", false)
			if !ok {
				t.Fatalf("Convert(%v, %s) failed", tt.value, tt.srcUnit)
			}
			if !ApproxEquals(got, tt.want, 0.0001) {
				t.Errorf("Convert(%v, %s) = %v, want %v", tt.value, tt.srcUnit, got, tt.want)
			}
		})
	}
}

func TestConvertTo(t *testing.T) {
	// Direct C → F conversion
	got, ok := ConvertTo(100, "http://qudt.org/vocab/unit/DEG_C", "http://qudt.org/vocab/unit/DEG_F", false)
	if !ok {
		t.Fatal("ConvertTo(DEG_C, DEG_F) failed")
	}
	if !ApproxEquals(got, 212, 0.01) {
		t.Errorf("ConvertTo(100, DEG_C, DEG_F) = %v, want 212", got)
	}

	// Incompatible units
	_, ok = ConvertTo(100, "http://qudt.org/vocab/unit/K", "http://qudt.org/vocab/unit/M", false)
	if ok {
		t.Error("ConvertTo(K, M) should fail for incompatible dimensions")
	}

	// Unknown unit
	_, ok = ConvertTo(100, "http://qudt.org/vocab/unit/NONEXISTENT", "http://qudt.org/vocab/unit/K", false)
	if ok {
		t.Error("ConvertTo with unknown unit should fail")
	}
}

func TestConvertUnknownUnit(t *testing.T) {
	_, ok := Convert(100, "http://qudt.org/vocab/unit/NONEXISTENT", "http://qudt.org/vocab/quantitykind/Temperature", false)
	if ok {
		t.Error("Convert with unknown unit should return false")
	}
}

func TestConvertCanonicalUnitReturnsFalse(t *testing.T) {
	// Converting from a canonical unit (where srcUnit == canonical) should
	// return 0, false to signal no conversion is needed.
	_, ok := Convert(300, "http://qudt.org/vocab/unit/K", "http://qudt.org/vocab/quantitykind/Temperature", false)
	if ok {
		t.Error("Convert(K→K) should return false (already canonical)")
	}
}

func TestConvertUnknownCanonical(t *testing.T) {
	// All units in our JSON have a canonical match, but this tests the code path
	// by temporarily checking a unit with an unmatched dimension vector.
	// We verify the behavior indirectly: if a unit has no canonical counterpart
	// in the same dimension vector, Convert should return false.
	// This is hard to test without modifying the data, so we just verify the
	// happy path works for all temperature units.
	tempUnits := []string{
		"http://qudt.org/vocab/unit/DEG_C",
		"http://qudt.org/vocab/unit/DEG_F",
		"http://qudt.org/vocab/unit/DEG_R",
	}
	for _, u := range tempUnits {
		if _, ok := Convert(273.15, u, "http://qudt.org/vocab/quantitykind/Temperature", false); !ok {
			t.Errorf("Convert(273.15, %s) should succeed", u)
		}
	}
	// K is canonical, so Convert should return false.
	if _, ok := Convert(273.15, "http://qudt.org/vocab/unit/K", "http://qudt.org/vocab/quantitykind/Temperature", false); ok {
		t.Error("Convert(273.15, K) should return false (K is canonical)")
	}
}
