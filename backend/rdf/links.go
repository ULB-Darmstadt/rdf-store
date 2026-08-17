package rdf

import (
	"bytes"
	"fmt"
	"io"
	"sort"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
)

func resolveLinks(graph *rdf2go.Graph, resource []byte) ([]byte, []string, error) {
	var linkedResources []string
	visited := make(map[string]struct{})

	var walkLink func(string) error
	var walkLocalCandidates func(map[string]struct{}) error
	walkLocalCandidates = func(candidates map[string]struct{}) error {
		return walkLocalLinks(candidates, visited, GetLocalSubjects, walkLink)
	}
	walkLink = func(link string) error {
		if _, seen := visited[link]; seen {
			return nil
		}
		visited[link] = struct{}{}

		bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE { GRAPH ?g { <%s> (<>|!<>)* ?s . GRAPH ?g { ?s ?p ?o } } }`, link))
		if err != nil {
			return err
		}
		linkedResourceGraph, err := sparqlResultToNQuads(bindings)
		if err != nil {
			return err
		}
		linkedResources = append(linkedResources, link)
		resource = append(resource, linkedResourceGraph...)
		if len(linkedResourceGraph) == 0 {
			return nil
		}

		dec := rdf.NewQuadDecoder(bytes.NewReader(linkedResourceGraph), rdf.NQuads)
		subjects := make(map[string]struct{})
		candidates := make(map[string]struct{})
		for {
			quad, err := dec.Decode()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if s, ok := quad.Subj.(rdf.IRI); ok {
				subjects[s.String()] = struct{}{}
			}
			if o, ok := quad.Obj.(rdf.IRI); ok {
				candidates[o.String()] = struct{}{}
			}
		}
		for candidate := range subjects {
			delete(candidates, candidate)
		}
		return walkLocalCandidates(candidates)
	}

	walkGraph := func(g *rdf2go.Graph) error {
		candidates := make(map[string]struct{})
		for t := range g.IterTriples() {
			linkCandidate, ok := t.Object.(*rdf2go.Resource)
			if !ok {
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
