package rdf

import (
	"bytes"
	"fmt"
	"io"
	"rdf-store-backend/base"
	"strings"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
)

func resolveLinks(graph *rdf2go.Graph, resource []byte) ([]byte, []string, error) {
	var linkedResources []string
	visited := make(map[string]struct{})

	var walkLink func(string) error
	walkLink = func(link string) error {
		if !strings.HasPrefix(link, base.Configuration.RdfNamespace) {
			return nil
		}
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
				obj := o.String()
				if strings.HasPrefix(obj, base.Configuration.RdfNamespace) {
					candidates[obj] = struct{}{}
				}
			}
		}
		for candidate := range candidates {
			if _, hasSubject := subjects[candidate]; hasSubject {
				continue
			}
			if err := walkLink(candidate); err != nil {
				return err
			}
		}
		return nil
	}

	walkGraph := func(g *rdf2go.Graph) error {
		for t := range g.IterTriples() {
			linkCandidate, ok := t.Object.(*rdf2go.Resource)
			if !ok {
				continue
			}
			link := linkCandidate.RawValue()
			if !strings.HasPrefix(link, base.Configuration.RdfNamespace) {
				continue
			}
			if _, seen := visited[link]; seen {
				continue
			}
			if g.One(linkCandidate, nil, nil) != nil {
				continue
			}
			if err := walkLink(link); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walkGraph(graph); err != nil {
		return nil, nil, err
	}
	return resource, linkedResources, nil
}
