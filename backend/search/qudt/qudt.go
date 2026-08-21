package qudt

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"math"
	"strings"

	"github.com/deiu/rdf2go"
)

//go:embed units.json
var unitsJSON []byte

var (
	units                   map[string]*UnitInfo
	canonicalByQuantityKind map[string]string
)

// PredicateConfig identifies the RDF properties that make up a quantity
// value. A zero value is disabled.
type PredicateConfig struct {
	hasUnit           string
	hasKindOfQuantity string
	hasNumericalValue string
	enabled           bool
}

// QuantityContext carries unit information for a measurement node so that
// appendQueryValue can convert the numeric value to its canonical SI unit.
type QuantityContext struct {
	UnitURI               string
	QuantityKindURI       string
	IsDelta               bool
	numericalPredicateURI string
}

// NFDI4ING metadata4ing predicates for quantity values.
var (
	qudtIsDeltaQuantity = rdf2go.NewResource("http://qudt.org/schema/qudt#isDeltaQuantity")
)

func init() {
	// Always load the embedded unit data.
	var catalog Catalog
	if err := json.Unmarshal(unitsJSON, &catalog); err != nil {
		slog.Warn("qudt: error parsing units.json", "err", err)
		return
	}
	units = catalog.Units
	canonicalByQuantityKind = catalog.CanonicalUnits

	slog.Info("loaded QUDT units", "units", len(units), "canonicalQuantityKinds", len(canonicalByQuantityKind))
}

// NewPredicateConfig creates an immutable quantity predicate configuration.
// If any predicate is empty, quantity conversion is disabled.
func NewPredicateConfig(hasUnit, hasKindOfQuantity, hasNumericalValue string) PredicateConfig {
	config := PredicateConfig{
		hasUnit:           strings.TrimSpace(hasUnit),
		hasKindOfQuantity: strings.TrimSpace(hasKindOfQuantity),
		hasNumericalValue: strings.TrimSpace(hasNumericalValue),
	}
	config.enabled = config.hasUnit != "" && config.hasKindOfQuantity != "" && config.hasNumericalValue != ""
	return config
}

// ScanConversionContext inspects a measurement node for quantity
// properties (hasUnit, hasKindOfQuantity) and the QUDT isDeltaQuantity flag.
// It returns a context suitable for unit conversion, or nil if the node is
// not a quantity measurement.
func (config PredicateConfig) ScanConversionContext(node rdf2go.Term, resource *rdf2go.Graph) *QuantityContext {
	if !config.enabled {
		return nil
	}
	hasUnitPredicate := rdf2go.NewResource(config.hasUnit)
	hasKindOfQuantityPredicate := rdf2go.NewResource(config.hasKindOfQuantity)
	var unitURI string
	for _, t := range resource.All(node, hasUnitPredicate, nil) {
		if res, ok := t.Object.(*rdf2go.Resource); ok {
			unitURI = res.RawValue()
			break
		}
	}
	if unitURI == "" {
		return nil
	}
	var quantityKindURI string
	for _, t := range resource.All(node, hasKindOfQuantityPredicate, nil) {
		if res, ok := t.Object.(*rdf2go.Resource); ok {
			quantityKindURI = res.RawValue()
			break
		}
	}
	if quantityKindURI == "" {
		return nil
	}
	// Check for qudt:isDeltaQuantity true on the measurement node.
	isDelta := false
	for _, t := range resource.All(node, qudtIsDeltaQuantity, nil) {
		if lit, ok := t.Object.(*rdf2go.Literal); ok {
			if lit.RawValue() == "true" || lit.RawValue() == "1" {
				isDelta = true
				break
			}
		}
	}
	return &QuantityContext{
		UnitURI:               unitURI,
		QuantityKindURI:       quantityKindURI,
		IsDelta:               isDelta,
		numericalPredicateURI: config.hasNumericalValue,
	}
}

// ConvertsNumericPredicate reports whether values of predicateURI are the
// numerical values governed by this quantity context.
func (q *QuantityContext) ConvertsNumericPredicate(predicateURI string) bool {
	return q != nil && predicateURI == q.numericalPredicateURI
}

// Unit returns the UnitInfo for the given unit URI, or nil if unknown.
func Unit(unitURI string) *UnitInfo {
	return units[unitURI]
}

// CanonicalUnitURI returns the preferred coherent unit URI for the given
// quantity kind, provided it is dimensionally compatible with the source unit.
// It returns an empty string if no compatible target is known.
func CanonicalUnitURI(unitURI, quantityKindURI string) string {
	info, ok := units[unitURI]
	if !ok {
		return ""
	}
	canonicalURI := canonicalByQuantityKind[normalizeQuantityKindURI(quantityKindURI)]
	canonicalInfo := units[canonicalURI]
	if canonicalInfo == nil || info.DimensionVector == "" || canonicalInfo.DimensionVector != info.DimensionVector {
		return ""
	}
	return canonicalURI
}

// CanonicalUnit returns the UnitInfo for the preferred coherent unit of the
// given quantity kind, or nil if no compatible target is known.
func CanonicalUnit(unitURI, quantityKindURI string) *UnitInfo {
	cuURI := CanonicalUnitURI(unitURI, quantityKindURI)
	if cuURI == "" {
		return nil
	}
	return units[cuURI]
}

// Convert converts a numeric value from srcUnit to the preferred coherent unit
// for the supplied quantity kind. It returns the converted value and true on
// success, or (0, false) if the source or target unit is unknown.
//
// The QUDT conversion formula from any unit to the coherent SI unit is:
//
//	value_SI = (value + conversionOffset) × conversionMultiplier
//
// To convert between two arbitrary units:
//
//	value_B = ((value_A + offsetA) × multiplierA) / multiplierB − offsetB
//
// When isDelta is true, offsets are skipped (for temperature differences etc.).
func Convert(value float64, srcUnitURI, quantityKindURI string, isDelta bool) (float64, bool) {
	src, ok := units[srcUnitURI]
	if !ok {
		return 0, false
	}
	tgtURI := CanonicalUnitURI(srcUnitURI, quantityKindURI)
	if tgtURI == "" {
		return 0, false
	}
	// skip conversion when unit already is canonical
	if srcUnitURI == tgtURI {
		return 0, false
	}
	tgt := units[tgtURI]
	if tgt == nil {
		return 0, false
	}
	return convertWith(value, src, tgt, isDelta), true
}

func normalizeQuantityKindURI(quantityKind string) string {
	if strings.Contains(quantityKind, "://") {
		return quantityKind
	}
	return "http://qudt.org/vocab/quantitykind/" + quantityKind
}

// ConvertTo converts a numeric value from srcUnit to tgtUnit.
// It returns the converted value and true on success, or (0, false) if
// either unit is unknown or they have different dimension vectors.
func ConvertTo(value float64, srcUnitURI, tgtUnitURI string, isDelta bool) (float64, bool) {
	if srcUnitURI == tgtUnitURI {
		return 0, false
	}
	src, ok := units[srcUnitURI]
	if !ok {
		return 0, false
	}
	tgt, ok := units[tgtUnitURI]
	if !ok {
		return 0, false
	}
	if src.DimensionVector != tgt.DimensionVector {
		return 0, false
	}
	return convertWith(value, src, tgt, isDelta), true
}

func convertWith(value float64, src, tgt *UnitInfo, isDelta bool) float64 {
	srcOff := src.Offset
	tgtOff := tgt.Offset
	if isDelta {
		srcOff = 0
		tgtOff = 0
	}
	return ((value+srcOff)*src.Multiplier)/tgt.Multiplier - tgtOff
}

// ApproxEquals reports whether a and b are equal within the given tolerance.
func ApproxEquals(a, b, tolerance float64) bool {
	diff := math.Abs(a - b)
	return diff <= tolerance
}
