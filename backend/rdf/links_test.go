package rdf

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/deiu/rdf2go"
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

func TestFindReferringResourceIdsTraversesIncomingGraphs(t *testing.T) {
	subjectsByGraph := map[string][]string{
		"https://example.org/leaf":   {"https://example.org/leaf/part"},
		"https://example.org/parent": {"https://example.org/parent/part"},
		"https://example.org/root":   {"https://example.org/root/part"},
	}
	incomingByTarget := map[string][]string{
		"https://example.org/leaf/part":   {"https://example.org/parent"},
		"https://example.org/parent/part": {"https://example.org/root"},
		// A cycle must not revisit the leaf or loop forever.
		"https://example.org/root/part": {"https://example.org/leaf"},
	}
	result, err := findReferringResourceIds(
		"https://example.org/leaf",
		func(id string) ([]string, error) { return subjectsByGraph[id], nil },
		func(ids []string) ([]string, error) {
			var result []string
			for _, id := range ids {
				result = append(result, incomingByTarget[id]...)
			}
			return result, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"https://example.org/parent", "https://example.org/root"}
	if !reflect.DeepEqual(result, expected) {
		t.Fatalf("expected transitive referrers %#v, got %#v", expected, result)
	}
}

func TestBuildIncomingGraphQueryIncludesGraphAndNestedSubjectTargets(t *testing.T) {
	query, err := buildIncomingGraphQuery([]string{
		"https://example.org/resource",
		"https://example.org/resource/part",
		"https://example.org/resource/part",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"VALUES ?target", "<https://example.org/resource>",
		"<https://example.org/resource/part>", "GRAPH ?g { ?s ?p ?target }",
	} {
		if !strings.Contains(query, expected) {
			t.Fatalf("incoming graph query is missing %q: %s", expected, query)
		}
	}
	if strings.Count(query, "<https://example.org/resource/part>") != 1 {
		t.Fatalf("incoming graph query must deduplicate targets: %s", query)
	}
}

func TestLinkResolverCachesGraphAndLocalSubjectLookups(t *testing.T) {
	const linkedID = "https://example.org/linked"
	resource := []byte(`<https://example.org/root> <https://example.org/hasPart> <https://example.org/linked> .`)
	graph := rdf2go.NewGraph("")
	if err := graph.Parse(bytes.NewReader(resource), "text/turtle"); err != nil {
		t.Fatal(err)
	}
	resolver := newLinkResolver()
	queryCalls := 0
	resolver.queryLink = func(id string) ([]byte, error) {
		queryCalls++
		if id != linkedID {
			t.Fatalf("unexpected linked ID %q", id)
		}
		return []byte(`<https://example.org/linked> <https://example.org/label> "cached" <https://example.org/linked> .`), nil
	}
	lookupCalls := 0
	resolver.lookupLocal = func(ids []string) ([]string, error) {
		lookupCalls++
		return []string{linkedID}, nil
	}

	for range 2 {
		expanded, links, err := resolver.resolve(graph, append([]byte(nil), resource...))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(expanded, []byte(`"cached"`)) || !reflect.DeepEqual(links, []string{linkedID}) {
			t.Fatalf("linked graph was not expanded: links=%#v data=%s", links, expanded)
		}
	}
	if queryCalls != 1 || lookupCalls != 1 {
		t.Fatalf("expected one graph query and one locality lookup, got query=%d lookup=%d", queryCalls, lookupCalls)
	}
}
