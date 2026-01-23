package rdf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"rdf-store-backend/shacl"
	"slices"
	"strings"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

var ErrResourceLinked = errors.New("resource is linked by other resources")

// GetResource fetches an RDF resource graph and optional metadata.
func GetResource(id string, includeLinked bool) (resource []byte, metadata *ResourceMetadata, err error) {
	if includeLinked {
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
	subjects, err := getGraphSubjects(id)
	if err != nil {
		return err
	}
	if !slices.Contains(subjects, id) && isValidIRI(id) {
		subjects = append(subjects, id)
	}
	for _, subject := range subjects {
		linked, err := hasIncomingLinks(subject, id)
		if err != nil {
			return err
		}
		if linked {
			return ErrResourceLinked
		}
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

func getGraphSubjects(id string) ([]string, error) {
	if !isValidIRI(id) {
		return nil, fmt.Errorf("invalid id IRI: %v", id)
	}
	bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT DISTINCT ?s WHERE { GRAPH <%s> { ?s ?p ?o } }`, id))
	if err != nil {
		return nil, err
	}
	res, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, err
	}
	subjects := make([]string, 0, len(res.Solutions()))
	seen := make(map[string]struct{})
	for _, row := range res.Solutions() {
		subject, ok := row["s"].(rdf.Subject)
		if !ok {
			return nil, fmt.Errorf("invalid binding: %v", row)
		}
		subjectID := subject.String()
		if !isValidIRI(subjectID) {
			continue
		}
		if _, ok := seen[subjectID]; ok {
			continue
		}
		seen[subjectID] = struct{}{}
		subjects = append(subjects, subjectID)
	}
	return subjects, nil
}

func hasIncomingLinks(id string, excludeGraph string) (bool, error) {
	if !isValidIRI(id) {
		return false, fmt.Errorf("invalid id IRI: %v", id)
	}
	if !isValidIRI(excludeGraph) {
		return false, fmt.Errorf("invalid exclude graph IRI: %v", excludeGraph)
	}
	bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`ASK WHERE { GRAPH ?g { ?s ?p <%s> } FILTER (?g != <%s>) }`, id, excludeGraph))
	if err != nil {
		return false, err
	}
	var response map[string]any
	if err := json.Unmarshal(bindings, &response); err != nil {
		return false, err
	}
	val, ok := response["boolean"]
	if !ok {
		return false, fmt.Errorf("got non boolean response")
	}
	linked, ok := val.(bool)
	if !ok {
		return false, fmt.Errorf("got non boolean response")
	}
	return linked, nil
}

// GetClassInstances retrieves all instances of a given RDF class.
func GetClassInstances(class string) ([]byte, error) {
	// prevent SPARQL injection
	if !isValidIRI(class) {
		return nil, fmt.Errorf("invalid class IRI: %v", class)
	}
	bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT DISTINCT ?s ?p ?o ?g WHERE  { GRAPH ?g { ?instance a <%s> . ?instance (<>|!<>)* ?s . ?s ?p ?o }}`, class))
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
