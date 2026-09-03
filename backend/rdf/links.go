package rdf

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"rdf-store-backend/shacl"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

func resolveLinks(graph *rdf2go.Graph, resource []byte) ([]byte, []string, error) {
	return newLinkResolver().resolve(graph, resource)
}

type linkedResourceEntry struct {
	data       []byte
	candidates map[string]struct{}
}

type linkResolver struct {
	entries            map[string]*linkedResourceEntry
	localSubjectKnown  map[string]struct{}
	localSubjectExists map[string]bool
	queryLink          func(string) ([]byte, error)
	lookupLocal        func([]string) ([]string, error)
}

func newLinkResolver() *linkResolver {
	return &linkResolver{
		entries:            make(map[string]*linkedResourceEntry),
		localSubjectKnown:  make(map[string]struct{}),
		localSubjectExists: make(map[string]bool),
		queryLink: func(link string) ([]byte, error) {
			bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE { GRAPH ?g { <%s> (<>|!<>)* ?s . GRAPH ?g { ?s ?p ?o } } }`, link))
			if err != nil {
				return nil, err
			}
			return sparqlResultToNQuads(bindings)
		},
		lookupLocal: GetLocalSubjects,
	}
}

func (resolver *linkResolver) resolve(graph *rdf2go.Graph, resource []byte) ([]byte, []string, error) {
	var linkedResources []string
	visited := make(map[string]struct{})

	var walkLink func(string) error
	var walkLocalCandidates func(map[string]struct{}) error
	walkLocalCandidates = func(candidates map[string]struct{}) error {
		return walkLocalLinks(candidates, visited, resolver.getLocalSubjects, walkLink)
	}
	walkLink = func(link string) error {
		if _, seen := visited[link]; seen {
			return nil
		}
		visited[link] = struct{}{}

		entry, err := resolver.load(link)
		if err != nil {
			return err
		}
		linkedResources = append(linkedResources, link)
		resource = append(resource, entry.data...)
		return walkLocalCandidates(entry.candidates)
	}

	walkGraph := func(g *rdf2go.Graph) error {
		candidates := make(map[string]struct{})
		for t := range g.IterTriples() {
			linkCandidate, ok := t.Object.(*rdf2go.Resource)
			if !ok {
				continue
			}
			// Skip rdf:first/rdf:rest objects: they are part of RDF list
			// structure and must not be resolved as linked resources.
			if t.Predicate.Equal(shacl.RDF_LIST_FIRST) || t.Predicate.Equal(shacl.RDF_LIST_REST) {
				continue
			}
			link := linkCandidate.RawValue()
			if _, seen := visited[link]; seen {
				continue
			}
			if g.One(linkCandidate, nil, nil) != nil {
				continue
			}
			candidates[link] = struct{}{}
		}
		return walkLocalCandidates(candidates)
	}

	if err := walkGraph(graph); err != nil {
		return nil, nil, err
	}
	return resource, linkedResources, nil
}

func (resolver *linkResolver) load(link string) (*linkedResourceEntry, error) {
	if entry, ok := resolver.entries[link]; ok {
		return entry, nil
	}
	data, err := resolver.queryLink(link)
	if err != nil {
		return nil, err
	}
	entry := &linkedResourceEntry{data: data, candidates: make(map[string]struct{})}
	if len(data) > 0 {
		decoder := rdf.NewQuadDecoder(bytes.NewReader(data), rdf.NQuads)
		subjects := make(map[string]struct{})
		for {
			quad, err := decoder.Decode()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if subject, ok := quad.Subj.(rdf.IRI); ok {
				subjects[subject.String()] = struct{}{}
			}
			if object, ok := quad.Obj.(rdf.IRI); ok {
				entry.candidates[object.String()] = struct{}{}
			}
		}
		for subject := range subjects {
			delete(entry.candidates, subject)
		}
	}
	resolver.entries[link] = entry
	return entry, nil
}

func (resolver *linkResolver) getLocalSubjects(ids []string) ([]string, error) {
	unknown := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, known := resolver.localSubjectKnown[id]; !known {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		local, err := resolver.lookupLocal(unknown)
		if err != nil {
			return nil, err
		}
		for _, id := range unknown {
			resolver.localSubjectKnown[id] = struct{}{}
		}
		for _, id := range local {
			resolver.localSubjectExists[id] = true
		}
	}
	local := make([]string, 0, len(ids))
	for _, id := range ids {
		if resolver.localSubjectExists[id] {
			local = append(local, id)
		}
	}
	return local, nil
}

func walkLocalLinks(candidates, visited map[string]struct{}, lookup func([]string) ([]string, error), walk func(string) error) error {
	links := make([]string, 0, len(candidates))
	for link := range candidates {
		if _, seen := visited[link]; !seen {
			links = append(links, link)
		}
	}
	sort.Strings(links)
	for start := 0; start < len(links); start += MaxLocalSubjectCandidates {
		end := min(start+MaxLocalSubjectCandidates, len(links))
		localLinks, err := lookup(links[start:end])
		if err != nil {
			return err
		}
		for _, link := range localLinks {
			if err := walk(link); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetReferringResourceIds returns every resource graph that directly or
// transitively links to the requested resource or one of its subjects.
func GetReferringResourceIds(id string) ([]string, error) {
	return findReferringResourceIds(id, getGraphSubjects, getIncomingGraphIds)
}

func findReferringResourceIds(id string, subjects func(string) ([]string, error), incoming func([]string) ([]string, error)) ([]string, error) {
	if !isValidIRI(id) {
		return nil, fmt.Errorf("invalid id IRI: %v", id)
	}
	visited := map[string]struct{}{id: {}}
	queue := []string{id}
	var referring []string
	for len(queue) > 0 {
		graphID := queue[0]
		queue = queue[1:]
		targets, err := subjects(graphID)
		if err != nil {
			return nil, err
		}
		targets = append(targets, graphID)
		for start := 0; start < len(targets); start += MaxLocalSubjectCandidates {
			end := min(start+MaxLocalSubjectCandidates, len(targets))
			graphIDs, err := incoming(targets[start:end])
			if err != nil {
				return nil, err
			}
			for _, referringID := range graphIDs {
				if _, seen := visited[referringID]; seen {
					continue
				}
				visited[referringID] = struct{}{}
				referring = append(referring, referringID)
				queue = append(queue, referringID)
			}
		}
	}
	sort.Strings(referring)
	return referring, nil
}

func getIncomingGraphIds(ids []string) ([]string, error) {
	query, err := buildIncomingGraphQuery(ids)
	if err != nil || query == "" {
		return []string{}, err
	}
	bindings, err := queryDataset(ResourceDataset, query)
	if err != nil {
		return nil, err
	}
	result, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, err
	}
	graphIDs := make([]string, 0, len(result.Solutions()))
	for _, row := range result.Solutions() {
		graphID, ok := row["g"].(rdf.Context)
		if !ok {
			return nil, fmt.Errorf("invalid incoming graph binding: %v", row)
		}
		graphIDs = append(graphIDs, graphID.String())
	}
	return graphIDs, nil
}

func buildIncomingGraphQuery(ids []string) (string, error) {
	if len(ids) > MaxLocalSubjectCandidates {
		return "", fmt.Errorf("at most %d ids are allowed", MaxLocalSubjectCandidates)
	}
	unique := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !isValidIRI(id) || strings.ContainsAny(id, "<>\"{}|^`\\\\ \t\r\n") {
			return "", fmt.Errorf("invalid id IRI: %v", id)
		}
		unique[id] = struct{}{}
	}
	if len(unique) == 0 {
		return "", nil
	}
	values := make([]string, 0, len(unique))
	for id := range unique {
		values = append(values, id)
	}
	sort.Strings(values)
	return fmt.Sprintf(`SELECT DISTINCT ?g WHERE {
  VALUES ?target { %s }
  GRAPH ?g { ?s ?p ?target }
} ORDER BY ?g`, arrayToSparqlValues(values)), nil
}
