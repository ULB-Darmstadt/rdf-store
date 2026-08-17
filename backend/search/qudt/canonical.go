package qudt

// Unit contains the QUDT metadata needed while generating the conversion
// catalog. Selection metadata is intentionally not written to the runtime
// catalog after the canonical targets have been resolved.
type CatalogSourceUnit struct {
	Multiplier        float64
	Offset            float64
	QuantityKinds     []string
	DimensionVector   string
	SIBase            bool
	SIDerivedCoherent bool
	SIApplicable      bool
	Scaled            bool
	SIExactMatch      bool
}

// Conversion contains the data needed to convert a unit at runtime.
type UnitInfo struct {
	Multiplier      float64 `json:"multiplier"`
	Offset          float64 `json:"offset"`
	DimensionVector string  `json:"dimensionVector"`
}

// Catalog is the generated, index-focused representation embedded by the
// runtime package.
type Catalog struct {
	Units          map[string]*UnitInfo `json:"units"`
	CanonicalUnits map[string]string    `json:"canonicalUnits"`
}

// BuildCatalog resolves canonical SI units and retains only units whose
// quantity kind, or an ancestor quantity kind, has a dimensionally compatible
// canonical target.
func BuildCatalog(source map[string]*CatalogSourceUnit, parents map[string][]string) Catalog {
	canonicalUnits := inheritCanonicalUnits(selectCanonicalUnits(source), source, parents)
	for quantityKind, uri := range canonicalUnits {
		target := source[uri]
		if target == nil || target.Multiplier == 0 || target.DimensionVector == "" {
			delete(canonicalUnits, quantityKind)
		}
	}

	units := make(map[string]*UnitInfo)
	for uri, unit := range source {
		if unit.Multiplier == 0 || unit.DimensionVector == "" || !hasCanonicalTarget(unit, source, canonicalUnits) {
			continue
		}
		units[uri] = &UnitInfo{
			Multiplier:      unit.Multiplier,
			Offset:          unit.Offset,
			DimensionVector: unit.DimensionVector,
		}
	}

	return Catalog{Units: units, CanonicalUnits: canonicalUnits}
}

// inheritCanonicalUnits resolves a quantity kind through the nearest ancestor
// with a direct canonical unit. Multiple different targets at the same nearest
// level are considered ambiguous and are not inherited.
func inheritCanonicalUnits(direct map[string]string, source map[string]*CatalogSourceUnit, parents map[string][]string) map[string]string {
	resolved := make(map[string]string, len(direct))
	for quantityKind, uri := range direct {
		resolved[quantityKind] = uri
	}

	quantityKinds := make(map[string]bool)
	for quantityKind := range parents {
		quantityKinds[quantityKind] = true
	}
	for _, unit := range source {
		for _, quantityKind := range unit.QuantityKinds {
			quantityKinds[quantityKind] = true
		}
	}

	for quantityKind := range quantityKinds {
		if resolved[quantityKind] != "" {
			continue
		}
		if uri := nearestCanonicalAncestor(quantityKind, direct, parents); uri != "" {
			resolved[quantityKind] = uri
		}
	}
	return resolved
}

func nearestCanonicalAncestor(quantityKind string, direct map[string]string, parents map[string][]string) string {
	visited := map[string]bool{quantityKind: true}
	level := append([]string(nil), parents[quantityKind]...)
	for len(level) > 0 {
		targets := make(map[string]bool)
		next := make([]string, 0)
		for _, ancestor := range level {
			if visited[ancestor] {
				continue
			}
			visited[ancestor] = true
			if uri := direct[ancestor]; uri != "" {
				targets[uri] = true
			}
			next = append(next, parents[ancestor]...)
		}
		if len(targets) == 1 {
			for uri := range targets {
				return uri
			}
		}
		if len(targets) > 1 {
			return ""
		}
		level = next
	}
	return ""
}

func hasCanonicalTarget(unit *CatalogSourceUnit, source map[string]*CatalogSourceUnit, canonicalUnits map[string]string) bool {
	for _, quantityKind := range unit.QuantityKinds {
		if target := source[canonicalUnits[quantityKind]]; target != nil && target.DimensionVector == unit.DimensionVector {
			return true
		}
	}
	return false
}

// selectCanonicalUnits selects one canonical SI unit per quantity kind. If
// equally authoritative candidates exist, it leaves that quantity kind
// unresolved rather than choosing based on a label or URI.
func selectCanonicalUnits(source map[string]*CatalogSourceUnit) map[string]string {
	type candidate struct {
		uri       string
		priority  int
		ambiguous bool
	}
	candidates := make(map[string]candidate)
	for uri, unit := range source {
		priority := priority(unit)
		if priority == 0 {
			continue
		}
		for _, quantityKind := range unit.QuantityKinds {
			existing, exists := candidates[quantityKind]
			switch {
			case !exists || priority > existing.priority:
				candidates[quantityKind] = candidate{uri: uri, priority: priority}
			case priority == existing.priority && uri != existing.uri:
				existing.ambiguous = true
				candidates[quantityKind] = existing
			}
		}
	}

	canonicalUnits := make(map[string]string, len(candidates))
	for quantityKind, candidate := range candidates {
		if !candidate.ambiguous {
			canonicalUnits[quantityKind] = candidate.uri
		}
	}
	return canonicalUnits
}

// priority ranks only coherent SI candidates. An exact match in the SI
// vocabulary is strongest, followed by explicit SI base or derived-coherent
// declarations. An unscaled, SI-applicable unit is the conservative fallback
// used for compound units such as cubic metre.
func priority(unit *CatalogSourceUnit) int {
	if unit.Multiplier != 1 || unit.Offset != 0 {
		return 0
	}
	if unit.SIExactMatch {
		return 3
	}
	if unit.SIBase || unit.SIDerivedCoherent {
		return 2
	}
	if unit.SIApplicable && !unit.Scaled {
		return 1
	}
	return 0
}
