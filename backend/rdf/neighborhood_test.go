package rdf

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestBuildOutgoingNeighborhoodQuery(t *testing.T) {
	query, err := buildNeighborhoodQuery("https://example.org/entity/1", NeighborhoodOutgoing, 7, 25)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "BIND(<https://example.org/entity/1> AS ?s) ?s ?p ?o") {
		t.Fatalf("query does not select direct statements for the entity: %s", query)
	}
	for _, expected := range []string{"ORDER BY ?g ?s ?p ?o", "LIMIT 26", "OFFSET 7"} {
		if !strings.Contains(query, expected) {
			t.Fatalf("outgoing query is missing %q: %s", expected, query)
		}
	}
	if strings.Contains(query, "*") || strings.Contains(query, "+") {
		t.Fatalf("outgoing query must not recurse: %s", query)
	}
}

func TestBuildIncomingNeighborhoodQueryPaginatesStatements(t *testing.T) {
	query, err := buildNeighborhoodQuery("https://example.org/entity/1", NeighborhoodIncoming, 50, 25)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"FILTER(isIRI(?s))", "LIMIT 26", "OFFSET 50", "(<https://example.org/entity/1> AS ?o)", "GRAPH ?g { ?s ?p <https://example.org/entity/1> }", "ORDER BY ?g ?s ?p ?o"} {
		if !strings.Contains(query, expected) {
			t.Fatalf("incoming query is missing %q: %s", expected, query)
		}
	}
	if strings.Contains(query, "COUNT(") || strings.Contains(query, "SELECT DISTINCT ?source") {
		t.Fatalf("incoming query must page statements without a count query: %s", query)
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
		_, err := buildNeighborhoodQuery(test.subject, test.direction, test.offset, test.limit)
		if !errors.Is(err, ErrInvalidNeighborhoodRequest) {
			t.Errorf("expected validation error for %#v, got %v", test, err)
		}
	}
}

func TestParseNeighborhoodPageUsesLookaheadAndClassifiesTerms(t *testing.T) {
	bindings := []byte(`{"head":{"vars":["s","p","o","g"]},"results":{"bindings":[
        {"s":{"type":"uri","value":"https://one.example/id"},"p":{"type":"uri","value":"https://example.org/p"},"o":{"type":"uri","value":"https://two.example/id"},"g":{"type":"uri","value":"https://example.org/g"}},
        {"s":{"type":"uri","value":"urn:three:id"},"p":{"type":"uri","value":"https://example.org/p"},"o":{"type":"literal","value":"value"},"g":{"type":"uri","value":"https://example.org/g"}},
        {"s":{"type":"uri","value":"https://lookahead.example/id"},"p":{"type":"uri","value":"https://example.org/p"},"o":{"type":"uri","value":"https://discarded.example/id"},"g":{"type":"uri","value":"https://example.org/g"}}
    ]}}`)
	quads, subjects, adjacent, returned, hasMore, err := parseNeighborhoodPage(bindings, 2)
	if err != nil {
		t.Fatal(err)
	}
	if returned != 2 || !hasMore {
		t.Fatalf("expected two returned rows with continuation, got %d, %v", returned, hasMore)
	}
	if len(subjects) != 2 || subjects[0] != "https://one.example/id" || subjects[1] != "urn:three:id" {
		t.Fatalf("unexpected page subjects: %#v", subjects)
	}
	if len(adjacent) != 1 || adjacent[0] != "https://two.example/id" {
		t.Fatalf("unexpected adjacent IRIs: %#v", adjacent)
	}
	if strings.Contains(string(quads), "lookahead") || strings.Count(strings.TrimSpace(string(quads)), "\n") != 1 {
		t.Fatalf("lookahead row leaked into N-Quads: %s", quads)
	}
}

func TestParseNeighborhoodPageWithoutLookaheadIsComplete(t *testing.T) {
	bindings := []byte(`{"head":{"vars":["s","p","o","g"]},"results":{"bindings":[
        {"s":{"type":"uri","value":"https://example.org/id"},"p":{"type":"uri","value":"https://example.org/p"},"o":{"type":"literal","value":"value"},"g":{"type":"uri","value":"https://example.org/g"}}
    ]}}`)
	_, _, _, returned, hasMore, err := parseNeighborhoodPage(bindings, 1)
	if err != nil {
		t.Fatal(err)
	}
	if returned != 1 || hasMore {
		t.Fatalf("expected a complete one-row page, got returned=%d hasMore=%v", returned, hasMore)
	}
}

func TestBuildLocalSubjectsQuerySupportsMixedNamespaces(t *testing.T) {
	query, err := buildLocalSubjectsQuery([]string{
		"https://example.org/resource/1",
		"urn:example:nested",
		"https://other.example/item",
		"urn:example:nested",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"VALUES ?id", "<https://example.org/resource/1>", "<urn:example:nested>",
		"<https://other.example/item>", "GRAPH ?g { ?id ?p ?o }", "GRAPH ?id { ?s ?gp ?go }",
	} {
		if !strings.Contains(query, expected) {
			t.Fatalf("local-subject query is missing %q: %s", expected, query)
		}
	}
	if strings.Count(query, "<urn:example:nested>") != 1 {
		t.Fatalf("local-subject query must deduplicate ids: %s", query)
	}
}

func TestBuildLocalSubjectsQueryValidation(t *testing.T) {
	if query, err := buildLocalSubjectsQuery(nil); err != nil || query != "" {
		t.Fatalf("empty request should not query the dataset, got %q, %v", query, err)
	}
	if _, err := buildLocalSubjectsQuery([]string{"not an iri"}); !errors.Is(err, ErrInvalidNeighborhoodRequest) {
		t.Fatalf("expected invalid IRI error, got %v", err)
	}
	tooMany := make([]string, MaxLocalSubjectCandidates+1)
	for index := range tooMany {
		tooMany[index] = "https://example.org/" + strconv.Itoa(index)
	}
	if _, err := buildLocalSubjectsQuery(tooMany); !errors.Is(err, ErrInvalidNeighborhoodRequest) {
		t.Fatalf("expected request limit error, got %v", err)
	}
}

func TestParseLocalSubjects(t *testing.T) {
	bindings := []byte(`{"head":{"vars":["id"]},"results":{"bindings":[{"id":{"type":"uri","value":"https://one.example/id"}},{"id":{"type":"uri","value":"urn:two:id"}}]}}`)
	ids, err := parseLocalSubjects(bindings)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "https://one.example/id" || ids[1] != "urn:two:id" {
		t.Fatalf("unexpected local subjects: %#v", ids)
	}
}
