package rdf

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"
	"slices"
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
	// Created is the timestamp recorded for the first insert.
	Created time.Time
	// LastModified is the timestamp recorded for the latest update.
	LastModified time.Time
	// Conformance maps resource identifiers to their conforming SHACL shape identifiers.
	Conformance map[string][]string
	// QueryConformance additionally includes linked subjects in the validation
	// context of this resource. It is used to index nested linked query paths.
	QueryConformance map[string][]string
}

const queryConformsTo = "urn:rdf-store:queryConformsTo"

// RebuildResourceConformance rebuilds metadata for a resource.
// It returns the updated metadata and any error encountered.
func RebuildResourceConformance(id string) (metadata *ResourceMetadata, err error) {
	resource, metadata, err := GetResource(id, false)
	if err != nil {
		return nil, err
	}
	metadata, _, err = updateResourceMetadata(rdf2go.NewResource(id), resource, true)
	return metadata, err
}

// RebuildAllResourceConformance refreshes metadata for every stored resource.
// It attempts every resource and joins any errors so callers can fail reliably.
func RebuildAllResourceConformance() error {
	resourceIDs, err := GetAllResourceIds()
	if err != nil {
		return err
	}
	return rebuildResourceConformanceSet(resourceIDs, func(resourceID string) error {
		_, err := RebuildResourceConformance(resourceID)
		return err
	})
}

func rebuildResourceConformanceSet(resourceIDs []string, rebuild func(string) error) error {
	var rebuildErrors []error
	for _, resourceID := range resourceIDs {
		slog.Info("rebuilding resource metadata", "resource", resourceID)
		if err := rebuild(resourceID); err != nil {
			rebuildErrors = append(rebuildErrors, fmt.Errorf("rebuilding metadata for %s: %w", resourceID, err))
		}
	}
	return errors.Join(rebuildErrors...)
}

// metadataUpdateTemplate renders the RDF triples persisted to the metadata dataset.
var metadataUpdateTemplate = template.Must(template.New("").Funcs(template.FuncMap{
	"FormatTime": func(t time.Time) string {
		return t.UTC().Format(time.RFC3339)
	},
}).Parse(`
	{{.Id}} <` + shacl.DCTERMS_MODIFIED.RawValue() + `> "{{FormatTime .LastModified}}"^^<http://www.w3.org/2001/XMLSchema#dateTime> .
	{{.Id}} <` + shacl.DCTERMS_CREATED.RawValue() + `> "{{FormatTime .Created}}"^^<http://www.w3.org/2001/XMLSchema#dateTime> .
	{{if gt (len (.Creator)) 0}}
	{{.Id}} <` + shacl.DCTERMS_CREATOR.RawValue() + `> "{{.Creator}}" .
	{{- end}}
	{{range $key, $values := .Conformance}}
	{{- range $values}}
	<{{$key}}> <` + shacl.DCTERMS_CONFORMS_TO.RawValue() + `> <{{.}}> .
	{{- end}}
	{{- end}}
	{{range $key, $values := .QueryConformance}}
	{{- range $values}}
	<{{$key}}> <` + queryConformsTo + `> <{{.}}> .
	{{- end}}
	{{- end}}
`))

// loadResourceMetadata reads resource metadata triples.
// It returns the parsed metadata and any error encountered.
func loadResourceMetadata(id string) (metadata *ResourceMetadata, err error) {
	metadata = &ResourceMetadata{
		Id:          rdf2go.NewResource(id),
		Conformance: make(map[string][]string),
	}
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
		case shacl.DCTERMS_CREATED.RawValue():
			if s.String() == id {
				if date, err := time.Parse(time.RFC3339, o.String()); err == nil {
					metadata.Created = date
				}
			}
		case shacl.DCTERMS_MODIFIED.RawValue():
			if s.String() == id {
				if date, err := time.Parse(time.RFC3339, o.String()); err == nil {
					metadata.LastModified = date
				}
			}
		case shacl.DCTERMS_CONFORMS_TO.RawValue():
			metadata.Conformance[s.String()] = append(metadata.Conformance[s.String()], o.String())
		case queryConformsTo:
			if metadata.QueryConformance == nil {
				metadata.QueryConformance = make(map[string][]string)
			}
			metadata.QueryConformance[s.String()] = append(metadata.QueryConformance[s.String()], o.String())
		}
	}
	return
}

func createResourceMetadata(resource []byte, creator string) (metadata *ResourceMetadata, graph *rdf2go.Graph, err error) {
	metadata, graph, err = buildResourceConformance(nil, resource)
	if err != nil {
		return
	}
	if exists, err := checkGraphExists(resourceMetaDataset, metadata.Id.RawValue()); exists || err != nil {
		return nil, nil, ErrExists
	}
	metadata.Creator = creator
	metadata.Created = time.Now().UTC()
	metadata.LastModified = metadata.Created
	err = storeResourceMetadata(metadata)
	return
}

// updateResourceMetadata writes creator, modified timestamp, and shape conformance triples.
// It returns the updated metadata, parsed graph, and any error encountered.
func updateResourceMetadata(id rdf2go.Term, resource []byte, preserveLastModified bool) (metadata *ResourceMetadata, graph *rdf2go.Graph, err error) {
	if metadata, err = loadResourceMetadata(id.RawValue()); err != nil {
		return
	}
	// check if exists
	if metadata.LastModified.IsZero() {
		return nil, nil, ErrNotFound
	}
	var updatedMetadata *ResourceMetadata
	updatedMetadata, graph, err = buildResourceConformance(id, resource)
	if err != nil {
		return
	}
	if !metadata.Id.Equal(updatedMetadata.Id) {
		return nil, nil, fmt.Errorf("id mismatch")
	}
	if !preserveLastModified {
		metadata.LastModified = time.Now().UTC()
	}
	metadata.Conformance = updatedMetadata.Conformance
	metadata.QueryConformance = updatedMetadata.QueryConformance
	err = storeResourceMetadata(metadata)
	return
}

// RestoreResourceMetadata replaces persisted metadata with a prior snapshot.
func RestoreResourceMetadata(metadata *ResourceMetadata) error {
	if metadata == nil || metadata.Id == nil || !isValidIRI(metadata.Id.RawValue()) {
		return fmt.Errorf("invalid resource metadata snapshot")
	}
	return storeResourceMetadata(metadata)
}

func storeResourceMetadata(metadata *ResourceMetadata) error {
	var buf bytes.Buffer
	if err := metadataUpdateTemplate.Execute(&buf, metadata); err != nil {
		return err
	}
	return uploadGraph(resourceMetaDataset, metadata.Id.RawValue(), buf.Bytes(), nil)
}

// deleteResourceMetadata removes the named graph of the resource metadata.
// It returns an error if the deletion fails.
func deleteResourceMetadata(id string) error {
	return deleteGraph(resourceMetaDataset, id)
}

// buildResourceConformance validates the resource and builds a shape conformance map for contained sub-resources.
// It returns the metadata, parsed graph, and any error encountered.
func buildResourceConformance(id rdf2go.Term, resource []byte) (metadata *ResourceMetadata, graph *rdf2go.Graph, err error) {
	graph, err = base.ParseGraph(bytes.NewReader(resource))
	if err != nil {
		return
	}
	validID, profile, err := FindResourceProfile(graph, id)
	if err != nil {
		return
	}
	if id != nil && !id.Equal(validID) {
		err = fmt.Errorf("%s", "id mismatch. given: "+id.RawValue()+", found: "+validID.RawValue())
		return
	}
	shapesGraph, ok := Profiles[profile.Id.RawValue()]
	if !ok {
		err = ErrNotFound
		return
	}
	validationShapes, err := shacl.SerializeProfileClosure(shapesGraph, Profiles)
	if err != nil {
		return nil, nil, err
	}

	// resolve linked resources since they are needed for validation
	var linkedResources []string
	resource, linkedResources, err = resolveLinks(graph, resource)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving linked resources: %w", err)
	}
	strictConformance, err := shacl.Validate(string(*shapesGraph.RDF), profile.Id.RawValue(), string(resource), validID.RawValue())
	if err != nil {
		return
	}
	// check if conformance map contains the expected SHACL profile for the main resource
	if rootShapes, ok := strictConformance[validID.RawValue()]; !ok || !slices.Contains(rootShapes, profile.Id.RawValue()) {
		err = fmt.Errorf("resource does not conform to expected shape %s", profile.Id.RawValue())
		return
	}
	conformance, err := shacl.Validate(string(validationShapes), profile.Id.RawValue(), string(resource), validID.RawValue())
	if err != nil {
		return nil, nil, err
	}
	queryConformance := cloneConformance(conformance)
	// filter out shape conformance for linked resources.
	// we assume that if an ID is not a subject in the original resource graph, then it is a linked resource that has been pulled in by the SPARQL query above.
	for resourceID := range conformance {
		if slices.Contains(linkedResources, resourceID) || graph.One(rdf2go.NewResource(resourceID), nil, nil) == nil {
			delete(conformance, resourceID)
		}
	}
	metadata = &ResourceMetadata{
		Id:               validID,
		Conformance:      conformance,
		QueryConformance: queryConformance,
	}
	return
}

func cloneConformance(source map[string][]string) map[string][]string {
	clone := make(map[string][]string, len(source))
	for subject, shapes := range source {
		clone[subject] = append([]string(nil), shapes...)
	}
	return clone
}

// FindResourceProfile identifies the profile matching a resource graph.
// It returns the resource ID, matched profile, and any error encountered.
func FindResourceProfile(graph *rdf2go.Graph, id rdf2go.Term) (resourceID rdf2go.Term, profile *shacl.NodeShape, err error) {
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
