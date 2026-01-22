package rdf

import (
	"bytes"
	"fmt"
	"log/slog"
	"rdf-store-backend/shacl"
	"strings"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

// GetResource fetches an RDF resource graph and optional metadata.
func GetResource(id string, union bool) (resource []byte, metadata *ResourceMetadata, err error) {
	if union {
		exists, err2 := checkGraphExists(ResourceDataset, id)
		if err2 != nil {
			return nil, nil, err2
		}
		if !exists {
			err = fmt.Errorf("graph %s not found in dataset %s", id, ResourceDataset)
			return nil, nil, err
		}
		var bindings []byte
		if bindings, err = queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE { GRAPH <%s> { ?s (<>|!<>)* ?s . GRAPH ?g { ?s ?p ?o } } }`, id)); err != nil {
			return
		}
		resource, err = sparqlResultToNQuads(bindings)
	} else {
		resource, err = loadGraph(ResourceDataset, id)
	}
	if err != nil {
		return
	}
	metadata, err = loadResourceMetadata(id)
	return
}

// CreateResource stores a new resource and updates metadata.
func CreateResource(resource []byte, creator string) (graph *rdf2go.Graph, metadata *ResourceMetadata, err error) {
	metadata, graph, err = updateResourceMetadata(nil, resource, creator)
	if err != nil {
		return
	}
	if err = createGraph(ResourceDataset, metadata.Id.RawValue(), resource); err != nil {
		deleteResourceMetadata(metadata.Id.RawValue())
		return
	}
	return
}

// UpdateResource validates permissions, updates the graph, and metadata.
func UpdateResource(id string, resource []byte, creator string) (graph *rdf2go.Graph, metadata *ResourceMetadata, err error) {
	if err = validateCreator(id, creator); err != nil {
		return
	}
	metadata, graph, err = updateResourceMetadata(rdf2go.NewResource(id), resource, creator)
	if err != nil {
		return
	}
	if err = uploadGraph(ResourceDataset, id, resource, nil); err != nil {
		deleteResourceMetadata(id)
	}
	return
}

// DeleteResource removes a resource graph and metadata.
func DeleteResource(id string, creator string) error {
	if err := validateCreator(id, creator); err != nil {
		return err
	}
	if err := deleteGraph(ResourceDataset, id); err != nil {
		return err
	}
	return deleteResourceMetadata(id)
}

// GetAllResourceIds lists all resource graph IDs.
func GetAllResourceIds() ([]string, error) {
	return getAllGraphIds(ResourceDataset)
}

// GetClassInstances retrieves all instances of a given RDF class.
func GetClassInstances(class string) ([]byte, error) {
	// prevent SPARQL injection
	if !isValidIRI(class) {
		return nil, fmt.Errorf("invalid class IRI: %v", class)
	}
	bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE  { GRAPH ?g { ?instance a <%s> . ?instance (<>|!<>)* ?s . ?s ?p ?o }}`, class))
	if err != nil {
		return nil, err
	}
	return sparqlResultToNQuads(bindings)
}

// GetShapeInstances retrieves all instances that conform to a given SHACL shape.
func GetShapeInstances(shape string) ([]byte, error) {
	// prevent SPARQL injection
	if !isValidIRI(shape) {
		return nil, fmt.Errorf("invalid shape IRI: %v", shape)
	}
	bindings, err := queryDataset(resourceMetaDataset, fmt.Sprintf(`SELECT DISTINCT ?resource WHERE { GRAPH ?g { ?resource <%s> <%s> } }`, shacl.DCTERMS_CONFORMS_TO.RawValue(), shape))
	if err != nil {
		return nil, err
	}
	res, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, err
	}
	var resources []string
	for _, row := range res.Solutions() {
		resource, ok := row["resource"].(rdf.Subject)
		if !ok {
			return nil, fmt.Errorf("invalid binding: %v", row)
		}
		resources = append(resources, resource.String())
	}
	if len(resources) == 0 {
		return []byte{}, nil
	}
	var b strings.Builder
	for _, resource := range resources {
		b.WriteString("<")
		b.WriteString(resource)
		b.WriteString("> ")
	}
	dataBindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE { GRAPH ?g { VALUES ?s { %s } ?s ?p ?o } }`, b.String()))
	if err != nil {
		return nil, err
	}
	return sparqlResultToNQuads(dataBindings)
}

// validateCreator ensures the requester matches stored creator metadata.
func validateCreator(id string, user string) error {
	if user == "" {
		return nil
	}
	metadata, err := loadResourceMetadata(id)
	if err != nil {
		return err
	}
	if metadata.Creator == user {
		return nil
	}
	slog.Warn("resource creator mismatch", "creator", metadata.Creator, "requesting user", user)
	return fmt.Errorf("creator mismatch")
}
