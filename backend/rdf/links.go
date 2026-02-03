package rdf

import (
	"fmt"
	"rdf-store-backend/base"
	"strings"

	"github.com/deiu/rdf2go"
)

func resolveLinks(graph *rdf2go.Graph, resource []byte, linkedResources []string) ([]byte, []string, error) {
	for t := range graph.IterTriples() {
		if linkCandidate, ok := t.Object.(*rdf2go.Resource); ok {
			if strings.HasPrefix(linkCandidate.RawValue(), base.Configuration.RdfNamespace) && graph.One(linkCandidate, nil, nil) == nil {
				bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE { GRAPH ?g { <%s> (<>|!<>)* ?s . GRAPH ?g { ?s ?p ?o } } }`, linkCandidate.RawValue()))
				if err != nil {
					return nil, nil, err
				}
				linkedResourceGraph, err := sparqlResultToNQuads(bindings)
				if err != nil {
					return nil, nil, err
				}
				linkedResources = append(linkedResources, linkCandidate.RawValue())
				resource = append(resource, linkedResourceGraph...)
			}
		}
	}
	return resource, linkedResources, nil
}
