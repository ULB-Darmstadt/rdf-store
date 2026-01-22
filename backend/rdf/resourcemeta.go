package rdf

import (
	"bytes"
	"errors"
	"fmt"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

// ResourceMetadata represents derived metadata about a stored RDF resource.
type ResourceMetadata struct {
	// Id is the resource identifier that metadata applies to.
	Id rdf2go.Term
	// Creator is the user name or identifier supplied when the resource was created.
	Creator string
	// LastModified is the timestamp recorded for the latest update.
	LastModified time.Time
	// Conformance maps resource identifiers to their conforming SHACL shape identifiers.
	Conformance map[string]string
}

// metadataUpdateTemplate renders the RDF triples persisted to the metadata dataset.
var metadataUpdateTemplate = template.Must(template.New("").Funcs(template.FuncMap{
	"FormatTime": func(t time.Time) string {
		return t.UTC().Format(time.RFC3339)
	},
}).Parse(`
	{{.Id}} <` + shacl.DCTERMS_MODIFIED.RawValue() + `> "{{FormatTime .LastModified}}"^^<http://www.w3.org/2001/XMLSchema#dateTime>  .
	{{if gt (len (.Creator)) 0}}
	{{.Id}} <` + shacl.DCTERMS_CREATOR.RawValue() + `> "{{.Creator}}" .
	{{- end}}
	{{range $key, $value := .Conformance}}
   	<{{$key}}> <` + shacl.DCTERMS_CONFORMS_TO.RawValue() + `> <{{$value}}> .
	{{- end }}
`))

// loadResourceMetadata reads resource metadata triples.
func loadResourceMetadata(id string) (metadata *ResourceMetadata, err error) {
	metadata = newResourceMetadata(rdf2go.NewResource(id), "")
	bindings, err := queryDataset(resourceMetaDataset, fmt.Sprintf(`SELECT * WHERE { GRAPH <%s> { ?s ?p ?o } }`, id))
	if err != nil {
		return
	}
	res, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, err
	}
	for _, row := range res.Solutions() {
		s, okS := row["s"].(rdf.Subject)
		p, okP := row["p"].(rdf.Predicate)
		o, okO := row["o"].(rdf.Object)
		if !okS || !okP || !okO {
			return nil, fmt.Errorf("invalid binding: %v", row)
		}
		switch p.String() {
		case shacl.DCTERMS_CREATOR.RawValue():
			if s.String() == id {
				metadata.Creator = o.String()
			}
		case shacl.DCTERMS_MODIFIED.RawValue():
			if s.String() == id {
				if date, err := time.Parse(time.RFC3339, o.String()); err == nil {
					metadata.LastModified = date
				}
			}
		case shacl.DCTERMS_CONFORMS_TO.RawValue():
			metadata.Conformance[s.String()] = o.String()
		}
	}
	return
}

// updateResourceMetadata writes updated creator and modified timestamp triples.
func updateResourceMetadata(id rdf2go.Term, resource []byte, creator string) (metadata *ResourceMetadata, graph *rdf2go.Graph, err error) {
	metadata, graph, err = buildResourceMetadata(id, resource, creator)
	if err != nil {
		return
	}
	if id != nil {
		if err = deleteResourceMetadata(id.RawValue()); err != nil {
			return
		}
	}
	var buf bytes.Buffer
	if err = metadataUpdateTemplate.Execute(&buf, metadata); err != nil {
		return
	}
	err = uploadGraph(resourceMetaDataset, metadata.Id.RawValue(), buf.Bytes(), nil)
	return
}

// deleteResourceMetadata removes the named graph of the resource metadata.
func deleteResourceMetadata(id string) error {
	return deleteGraph(resourceMetaDataset, id)
}

// buildResourceMetadata validates the resource, build a shape conformance map for contained sub-resources and returns metadata plus its parsed graph.
func buildResourceMetadata(id rdf2go.Term, resource []byte, creator string) (metadata *ResourceMetadata, graph *rdf2go.Graph, err error) {
	graph, err = base.ParseGraph(bytes.NewReader(resource))
	if err != nil {
		return
	}
	validID, profile, err := findResourceProfile(graph, id)
	if err != nil {
		return
	}
	if id != nil && !id.Equal(validID) {
		err = fmt.Errorf("%s", "id mismatch. given: "+id.RawValue()+", found: "+validID.RawValue())
		return
	}
	shapesGraph, err := GetProfile(profile.Id.RawValue())
	if err != nil {
		return
	}
	// resolve linked resources since they are needed for validation
	var linkedResources []string
	for t := range graph.IterTriples() {
		if linkCandidate, ok := t.Object.(*rdf2go.Resource); ok {
			if strings.HasPrefix(linkCandidate.RawValue(), base.Configuration.RdfNamespace) && graph.One(linkCandidate, nil, nil) == nil {
				bindings, innerErr := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE { GRAPH ?g { <%s> (<>|!<>)* ?s . GRAPH ?g { ?s ?p ?o } } }`, linkCandidate.RawValue()))
				if innerErr != nil {
					err = innerErr
					return
				}
				linkedResourceGraph, innerErr := sparqlResultToNQuads(bindings)
				if innerErr != nil {
					err = innerErr
					return
				}
				linkedResources = append(linkedResources, linkCandidate.RawValue())
				resource = append(resource, linkedResourceGraph...)
			}
		}
	}
	var conformance map[string]string
	if conformance, err = shacl.Validate(string(shapesGraph), profile.Id.RawValue(), string(resource), validID.RawValue()); err != nil {
		return
	}
	// check if conformance map contains the expected SHACL profile for the main resource
	if rootShape, ok := conformance[validID.RawValue()]; !ok || rootShape != profile.Id.RawValue() {
		err = fmt.Errorf("resource does not conform to expected shape %s", profile.Id.RawValue())
		return
	}
	// filter out shape conformance for linked resources.
	// we assume that if an ID is not a subject in the original resource graph, then it is a linked resource that has been pulled in by the SPARQL query above.
	for resourceID := range conformance {
		if slices.Contains(linkedResources, resourceID) || graph.One(rdf2go.NewResource(resourceID), nil, nil) == nil {
			fmt.Println("--- delete", resourceID)
			delete(conformance, resourceID)
		}
	}
	metadata = newResourceMetadata(validID, creator)
	metadata.Conformance = conformance
	return
}

// FindResourceProfile identifies the profile matching a resource graph.
func findResourceProfile(graph *rdf2go.Graph, id rdf2go.Term) (resourceID rdf2go.Term, profile *shacl.NodeShape, err error) {
	var refs []*rdf2go.Triple
	if id == nil {
		refs = graph.All(nil, shacl.DCTERMS_CONFORMS_TO, nil)
		refs = append(refs, graph.All(nil, shacl.RDF_TYPE, nil)...)
	} else {
		refs = graph.All(id, shacl.DCTERMS_CONFORMS_TO, nil)
		refs = append(refs, graph.All(id, shacl.RDF_TYPE, nil)...)
	}
	if len(refs) == 0 {
		return nil, nil, errors.New("resource graph has no relation " + shacl.DCTERMS_CONFORMS_TO.String() + " or " + shacl.RDF_TYPE.String() + " to a known SHACL profile")
	}
	for _, triple := range refs {
		if profileRef, ok := Profiles[triple.Object.RawValue()]; ok {
			if resourceID != nil {
				return nil, nil, errors.New("resource graph has multiple relations " + shacl.DCTERMS_CONFORMS_TO.String() + " or " + shacl.RDF_TYPE.String() + " to a known SHACL profile")
			}
			resourceID = triple.Subject
			profile = profileRef
		}
	}
	if profile == nil {
		return nil, nil, errors.New("no relation to an existing SHACL shape found in resource graph")
	}
	if resourceID == nil {
		return nil, nil, errors.New("resource graph has no relation " + shacl.DCTERMS_CONFORMS_TO.String() + " or " + shacl.RDF_TYPE.String() + " to a known SHACL profile")
	}
	return
}

// newResourceMetadata initializes a ResourceMetadata instance with defaults.
func newResourceMetadata(id rdf2go.Term, creator string) *ResourceMetadata {
	return &ResourceMetadata{
		Id:           id,
		Creator:      creator,
		LastModified: time.Now().UTC(),
		Conformance:  make(map[string]string),
	}
}
