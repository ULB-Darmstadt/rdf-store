package rdf

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildOutgoingNeighborhoodQuery(t *testing.T) {
	query, countQuery, err := buildNeighborhoodQueries("https://example.org/entity/1", NeighborhoodOutgoing, 0, 25)
	if err != nil {
		t.Fatal(err)
	}
	if countQuery != "" {
		t.Fatalf("outgoing query unexpectedly has count query: %s", countQuery)
	}
	if !strings.Contains(query, "BIND(<https://example.org/entity/1> AS ?s) ?s ?p ?o") {
		t.Fatalf("query does not select direct statements for the entity: %s", query)
	}
	if strings.Contains(query, "*") || strings.Contains(query, "+") {
		t.Fatalf("outgoing query must not recurse: %s", query)
	}
}

func TestBuildIncomingNeighborhoodQueryPaginatesDistinctIRISources(t *testing.T) {
	query, countQuery, err := buildNeighborhoodQueries("https://example.org/entity/1", NeighborhoodIncoming, 50, 25)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"SELECT DISTINCT ?source", "FILTER(isIRI(?source))", "LIMIT 25", "OFFSET 50", "SELECT (?source AS ?s)", "(<https://example.org/entity/1> AS ?o)", "GRAPH ?g { ?source ?linkPredicate <https://example.org/entity/1> }"} {
		if !strings.Contains(query, expected) {
			t.Fatalf("incoming query is missing %q: %s", expected, query)
		}
	}
	if strings.Contains(query, "BIND(?source AS ?s)") {
		t.Fatalf("incoming query must constrain the graph pattern directly instead of binding after matching: %s", query)
	}
	if strings.Contains(query, "?source ?p ?o") {
		t.Fatalf("incoming query must not implicitly expand each source's outgoing neighborhood: %s", query)
	}
	if !strings.Contains(countQuery, "COUNT(DISTINCT ?source)") {
		t.Fatalf("incoming count query does not count distinct sources: %s", countQuery)
	}
}

func TestBuildNeighborhoodQueryValidation(t *testing.T) {
	tests := []struct {
		subject   string
		direction string
		offset    int
		limit     int
	}{
		{"not an iri", NeighborhoodOutgoing, 0, 25},
		{"https://example.org/entity", "sideways", 0, 25},
		{"https://example.org/entity", NeighborhoodIncoming, -1, 25},
		{"https://example.org/entity", NeighborhoodIncoming, 0, 0},
		{"https://example.org/entity", NeighborhoodIncoming, 0, 101},
	}
	for _, test := range tests {
		_, _, err := buildNeighborhoodQueries(test.subject, test.direction, test.offset, test.limit)
		if !errors.Is(err, ErrInvalidNeighborhoodRequest) {
			t.Errorf("expected validation error for %#v, got %v", test, err)
		}
	}
}

func TestParseNeighborhoodCount(t *testing.T) {
	bindings := []byte(`{"head":{"vars":["total"]},"results":{"bindings":[{"total":{"type":"literal","datatype":"http://www.w3.org/2001/XMLSchema#integer","value":"42"}}]}}`)
	total, err := parseNeighborhoodCount(bindings)
	if err != nil {
		t.Fatal(err)
	}
	if total != 42 {
		t.Fatalf("expected 42, got %d", total)
	}
}
