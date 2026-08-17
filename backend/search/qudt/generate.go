//go:build ignore

// generate downloads the pinned QUDT unit, system-of-units, and quantity-kind
// vocabularies and writes the pruned runtime conversion catalog to units.json.
//
// Run with:
//
//	go run generate.go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"rdf-store-backend/search/qudt"
	"sort"
	"strconv"
	"strings"

	"github.com/deiu/rdf2go"
)

const (
	qudtVersion = "3.5.0"
	unitTTLURL  = "https://qudt.org/" + qudtVersion + "/vocab/unit"
	souTTLURL   = "https://qudt.org/" + qudtVersion + "/vocab/sou"
	qkTTLURL    = "https://qudt.org/" + qudtVersion + "/vocab/quantitykind"
	siSystemURI = "http://qudt.org/vocab/sou/SI"
)

var (
	qudtMultiplier      = rdf2go.NewResource("http://qudt.org/schema/qudt/conversionMultiplier")
	qudtOffset          = rdf2go.NewResource("http://qudt.org/schema/qudt/conversionOffset")
	qudtDV              = rdf2go.NewResource("http://qudt.org/schema/qudt/hasDimensionVector")
	qudtQK              = rdf2go.NewResource("http://qudt.org/schema/qudt/hasQuantityKind")
	qudtApplicable      = rdf2go.NewResource("http://qudt.org/schema/qudt/applicableSystem")
	qudtDerivedCoherent = rdf2go.NewResource("http://qudt.org/schema/qudt/derivedCoherentUnitOfSystem")
	qudtScalingOf       = rdf2go.NewResource("http://qudt.org/schema/qudt/scalingOf")
	qudtHasBaseUnit     = rdf2go.NewResource("http://qudt.org/schema/qudt/hasBaseUnit")
	qudtSIExactMatch    = rdf2go.NewResource("http://qudt.org/schema/qudt/siExactMatch")
	qudtApplicableUnit  = rdf2go.NewResource("http://qudt.org/schema/qudt/applicableUnit")
	qudtSpecialization  = rdf2go.NewResource("http://qudt.org/schema/qudt/specializationOf")
	skosBroader         = rdf2go.NewResource("http://www.w3.org/2004/02/skos/core#broader")
	siSystem            = rdf2go.NewResource(siSystemURI)
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	g := rdf2go.NewGraph("http://qudt.org/vocab/unit")
	for _, source := range []struct {
		name string
		url  string
	}{
		{name: "unit", url: unitTTLURL},
		{name: "system-of-units", url: souTTLURL},
		{name: "quantity-kind", url: qkTTLURL},
	} {
		fmt.Printf("downloading QUDT %s vocabulary %s...\n", source.name, qudtVersion)
		if err := downloadInto(g, source.url); err != nil {
			return fmt.Errorf("loading %s vocabulary: %w", source.name, err)
		}
	}
	type entry struct {
		multiplier float64
		offset     float64
		dv         string
		qkSet      map[string]bool
		siBase     bool
		siCoherent bool
		siAllowed  bool
		scaled     bool
		siExact    bool
	}
	entries := make(map[string]*entry)

	// Find all subjects with a conversionMultiplier — these are the units.
	for _, s := range g.All(nil, qudtMultiplier, nil) {
		subj := termURI(s.Subject)
		e, ok := entries[subj]
		if !ok {
			e = &entry{qkSet: make(map[string]bool)}
			entries[subj] = e
		}
		if lit, ok := s.Object.(*rdf2go.Literal); ok {
			if f, err := strconv.ParseFloat(lit.Value, 64); err == nil {
				e.multiplier = f
			}
		}
	}
	fmt.Printf("found %d units with conversionMultiplier\n", len(entries))

	// Collect dimension vectors (predicate-specific query required by rdf2go).
	for _, s := range g.All(nil, qudtDV, nil) {
		subj := termURI(s.Subject)
		e, ok := entries[subj]
		if !ok {
			continue
		}
		if res, ok := s.Object.(*rdf2go.Resource); ok {
			uri := res.URI
			if i := strings.LastIndex(uri, "/"); i >= 0 {
				e.dv = uri[i+1:]
			}
		}
	}

	// Collect offsets.
	for _, s := range g.All(nil, qudtOffset, nil) {
		subj := termURI(s.Subject)
		e, ok := entries[subj]
		if !ok {
			continue
		}
		if lit, ok := s.Object.(*rdf2go.Literal); ok {
			if f, err := strconv.ParseFloat(lit.Value, 64); err == nil {
				e.offset = f
			}
		}
	}

	// Collect quantity kinds.
	for _, s := range g.All(nil, qudtQK, nil) {
		subj := termURI(s.Subject)
		e, ok := entries[subj]
		if !ok {
			continue
		}
		if res, ok := s.Object.(*rdf2go.Resource); ok {
			e.qkSet[res.URI] = true
		}
	}
	// Quantity kinds also declare their applicable units in the quantity-kind
	// vocabulary. Merge those assertions to cover units whose unit record is
	// less specific.
	for _, s := range g.All(nil, qudtApplicableUnit, nil) {
		if e := entries[termURI(s.Object)]; e != nil {
			e.qkSet[termURI(s.Subject)] = true
		}
	}

	quantityKindParents := make(map[string][]string)
	for _, predicate := range []rdf2go.Term{qudtSpecialization, skosBroader} {
		for _, s := range g.All(nil, predicate, nil) {
			child := termURI(s.Subject)
			parent := termURI(s.Object)
			if child != "" && parent != "" {
				quantityKindParents[child] = appendUnique(quantityKindParents[child], parent)
			}
		}
	}

	// Collect explicit SI metadata. Base and named coherent units have the
	// strongest signal. SI-applicable, unscaled units provide a fallback for
	// coherent compound units such as cubic metre, which QUDT does not mark with
	// derivedCoherentUnitOfSystem.
	for _, s := range g.All(nil, qudtApplicable, siSystem) {
		if e := entries[termURI(s.Subject)]; e != nil {
			e.siAllowed = true
		}
	}
	for _, s := range g.All(nil, qudtDerivedCoherent, siSystem) {
		if e := entries[termURI(s.Subject)]; e != nil {
			e.siCoherent = true
		}
	}
	for _, s := range g.All(nil, qudtScalingOf, nil) {
		if e := entries[termURI(s.Subject)]; e != nil {
			e.scaled = true
		}
	}
	for _, s := range g.All(siSystem, qudtHasBaseUnit, nil) {
		if e := entries[termURI(s.Object)]; e != nil {
			e.siBase = true
		}
	}
	for _, s := range g.All(nil, qudtSIExactMatch, nil) {
		if e := entries[termURI(s.Subject)]; e != nil {
			e.siExact = true
		}
	}

	allUnits := make(map[string]*qudt.CatalogSourceUnit, len(entries))
	for uri, e := range entries {
		qks := make([]string, 0, len(e.qkSet))
		for qk := range e.qkSet {
			qks = append(qks, qk)
		}
		sort.Strings(qks)
		allUnits[uri] = &qudt.CatalogSourceUnit{
			Multiplier:        e.multiplier,
			Offset:            e.offset,
			QuantityKinds:     qks,
			DimensionVector:   e.dv,
			SIBase:            e.siBase,
			SIDerivedCoherent: e.siCoherent,
			SIApplicable:      e.siAllowed,
			Scaled:            e.scaled,
			SIExactMatch:      e.siExact,
		}
	}
	catalog := qudt.BuildCatalog(allUnits, quantityKindParents)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(catalog); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}

	if err := os.WriteFile("units.json", buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing units.json: %w", err)
	}
	fmt.Printf(
		"wrote units.json (%d convertible units, %d canonical quantity kinds)\n",
		len(catalog.Units),
		len(catalog.CanonicalUnits),
	)
	return nil
}

func downloadInto(g *rdf2go.Graph, url string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/turtle")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}
	if err := g.Parse(resp.Body, "text/turtle"); err != nil {
		return fmt.Errorf("parsing turtle: %w", err)
	}
	return nil
}

func termURI(t rdf2go.Term) string {
	if r, ok := t.(*rdf2go.Resource); ok {
		return r.URI
	}
	return t.String()
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
