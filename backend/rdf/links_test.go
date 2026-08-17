package rdf

import (
	"reflect"
	"testing"
)

func TestWalkLocalLinksUsesLocalSubjectLookupAcrossNamespaces(t *testing.T) {
	const local = "https://w3id.org/nfdi4ing/resources/local"
	candidates := map[string]struct{}{
		"https://tueditions.ulb.tu-darmstadt.de/v/local": {},
		local:                          {},
		"https://example.org/external": {},
	}
	var lookedUp []string
	lookup := func(ids []string) ([]string, error) {
		lookedUp = append(lookedUp, ids...)
		return []string{local}, nil
	}
	var walked []string
	walk := func(id string) error {
		walked = append(walked, id)
		return nil
	}

	if err := walkLocalLinks(candidates, nil, lookup, walk); err != nil {
		t.Fatal(err)
	}
	if len(lookedUp) != len(candidates) {
		t.Fatalf("expected every IRI to be checked for local availability, got %#v", lookedUp)
	}
	if !reflect.DeepEqual(walked, []string{local}) {
		t.Fatalf("expected only the locally available foreign-namespace IRI to be resolved, got %#v", walked)
	}
}
