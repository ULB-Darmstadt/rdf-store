package rdf

import (
	"bytes"
	"errors"
	"fmt"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"
	"text/template"
	"time"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

type ResourceMetadata struct {
	Id           rdf2go.Term
	Creator      string
	LastModified time.Time
	Conformance  map[string]string
}

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
	fmt.Printf("--- loaded metadata: %+v\n", metadata)
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
	var conformance map[string]string
	if conformance, err = shacl.Validate(string(shapesGraph), profile.Id.RawValue(), string(resource), validID.RawValue()); err != nil {
		err = fmt.Errorf("resource graph does not conform to shape %s", profile.Id.RawValue())
		return
	}
	if rootShape, ok := conformance[validID.RawValue()]; !ok || rootShape != profile.Id.RawValue() {
		err = fmt.Errorf("%s", "resource does not conform to expected shape"+profile.Id.RawValue())
		return
	}
	metadata = newResourceMetadata(validID, creator)
	metadata.Conformance = conformance
	buildConformance(validID, metadata, graph)
	return
}

func buildConformance(id rdf2go.Term, metadata *ResourceMetadata, graph *rdf2go.Graph) {

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
				return nil, nil, errors.New("graph has multiple relations " + shacl.DCTERMS_CONFORMS_TO.String() + " or " + shacl.RDF_TYPE.String() + " to a known SHACL profile")
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

func newResourceMetadata(id rdf2go.Term, creator string) *ResourceMetadata {
	return &ResourceMetadata{
		Id:           id,
		Creator:      creator,
		LastModified: time.Now().UTC(),
		Conformance:  make(map[string]string),
	}
}
