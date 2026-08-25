package rdf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"
	"slices"

	"github.com/deiu/rdf2go"
	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

var ErrResourceLinked = errors.New("resource is linked by other resources")

// GetResource fetches an RDF resource graph with optional linked graph expansion.
// It returns the resource bytes, metadata, and any error encountered.
func GetResource(id string, includeLinked bool) (resource []byte, metadata *ResourceMetadata, err error) {
	resource, err = loadGraph(ResourceDataset, id)
	if err != nil {
		return
	}
	metadata, err = loadResourceMetadata(id)
	if err != nil {
		return
	}
	if includeLinked {
		graph, innerErr := base.ParseGraph(bytes.NewReader(resource))
		if innerErr != nil {
			err = innerErr
			return
		}
		resource, _, err = resolveLinks(graph, resource)
	}
	return
}

// ResourceIndexLoader caches linked graph lookups while loading multiple
// resources from an unchanged dataset, such as during a full reindex.
type ResourceIndexLoader struct {
	links *linkResolver
}

// NewResourceIndexLoader creates a loader scoped to one indexing operation.
func NewResourceIndexLoader() *ResourceIndexLoader {
	return &ResourceIndexLoader{
		links: newLinkResolver(),
	}
}

// Get loads one resource with recursively linked data and contextual
// conformance, reusing linked graph and local-subject lookups.
func (loader *ResourceIndexLoader) Get(id string) (graph *rdf2go.Graph, metadata *ResourceMetadata, queryConformance map[string][]string, err error) {
	resource, metadata, err := GetResource(id, false)
	if err != nil {
		return nil, nil, nil, err
	}
	graph, err = base.ParseGraph(bytes.NewReader(resource))
	if err != nil {
		return nil, nil, nil, err
	}
	expanded, linkedResources, err := loader.links.resolve(graph, resource)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(linkedResources) == 0 {
		return graph, metadata, metadata.Conformance, nil
	}
	if err = appendNQuadsToGraph(graph, expanded[len(resource):]); err != nil {
		return nil, nil, nil, fmt.Errorf("parsing linked resources for indexing: %w", err)
	}
	if metadata.QueryConformance == nil {
		return nil, nil, nil, fmt.Errorf("resource metadata for %s has no linked query conformance; run rdf-store-cli rebuild", metadata.Id.RawValue())
	}
	return graph, metadata, metadata.QueryConformance, nil
}

func appendNQuadsToGraph(graph *rdf2go.Graph, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	decoder := rdf.NewQuadDecoder(bytes.NewReader(data), rdf.NQuads)
	var triples bytes.Buffer
	encoder := rdf.NewTripleEncoder(&triples, rdf.NTriples)
	for {
		quad, err := decoder.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := encoder.Encode(quad.Triple); err != nil {
			return err
		}
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	return graph.Parse(bytes.NewReader(triples.Bytes()), "text/turtle")
}

// CreateResource stores a new resource graph and updates its metadata record.
// It returns the metadata and any error encountered.
func CreateResource(resource []byte, creator string) (metadata *ResourceMetadata, err error) {
	metadata, _, err = createResourceMetadata(resource, creator)
	if err != nil {
		return
	}
	if err = createGraph(ResourceDataset, metadata.Id.RawValue(), resource); err != nil {
		deleteResourceMetadata(metadata.Id.RawValue())
		return
	}
	return
}

// UpdateResource validates permissions, updates the graph, and refreshes metadata.
// It returns the metadata and any error encountered.
func UpdateResource(id string, resource []byte, creator string) (metadata *ResourceMetadata, err error) {
	if err = validateCreator(id, creator); err != nil {
		return
	}
	previousResource, previousMetadata, err := GetResource(id, false)
	if err != nil {
		return nil, err
	}
	metadata, _, err = updateResourceMetadata(rdf2go.NewResource(id), resource, false)
	if err != nil {
		return
	}
	if err = uploadGraph(ResourceDataset, id, resource, nil); err != nil {
		if restoreErr := RestoreResourceSnapshot(previousResource, previousMetadata); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restoring resource after failed update: %w", restoreErr))
		}
	}
	return
}

// RestoreResourceSnapshot replaces a resource graph and its metadata with a
// previously loaded snapshot. It is used to compensate failed linked updates.
func RestoreResourceSnapshot(resource []byte, metadata *ResourceMetadata) error {
	if metadata == nil || metadata.Id == nil || !isValidIRI(metadata.Id.RawValue()) {
		return fmt.Errorf("invalid resource snapshot")
	}
	graph, err := base.ParseGraph(bytes.NewReader(resource))
	if err != nil {
		return err
	}
	resourceID, _, err := FindResourceProfile(graph, metadata.Id)
	if err != nil {
		return err
	}
	if !resourceID.Equal(metadata.Id) {
		return fmt.Errorf("resource snapshot id mismatch")
	}
	if err := uploadGraph(ResourceDataset, metadata.Id.RawValue(), resource, graph); err != nil {
		return err
	}
	return RestoreResourceMetadata(metadata)
}

// DeleteResource removes a resource graph and its metadata after checking for incoming links.
// It returns an error if the deletion fails or the resource is still linked.
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

// GetAllResourceIds lists all resource graph IDs in the dataset.
// It returns the slice of resource IDs and any error encountered.
func GetAllResourceIds() ([]string, error) {
	return getAllGraphIds(ResourceDataset)
}

// getGraphSubjects retrieves distinct subject IRIs from a resource graph.
// It returns the subject list or an error when the ID is invalid or the query fails.
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

// hasIncomingLinks checks whether any graph links to the given subject.
// It returns a boolean indicating linkage and an error for invalid input or query failures.
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

// GetClassInstances retrieves all instances of a given RDF class across graphs.
// It returns the instances as N-Quads bytes and any error encountered.
func GetClassInstances(classes []string) ([]byte, error) {
	// prevent SPARQL injection
	for _, class := range classes {
		if !isValidIRI(class) {
			return nil, fmt.Errorf("invalid class IRI: %v", class)
		}
	}
	bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT DISTINCT ?s ?p ?o ?g WHERE  { GRAPH ?g { VALUES ?class { %s } ?instance a ?class . ?instance (<>|!<>)* ?s . ?s ?p ?o }}`, arrayToSparqlValues(classes)))
	if err != nil {
		return nil, err
	}
	return sparqlResultToNQuads(bindings)
}

// ListConformingResources retrieves all instances that conform to a given SHACL shape.
// It returns the map mapping from instance ID to its N-Quads bytes and any error encountered.
func ListConformingResources(shape string) ([]string, error) {
	// prevent SPARQL injection
	if !isValidIRI(shape) {
		return nil, fmt.Errorf("invalid shape IRI: %v", shape)
	}
	bindings, err := queryDataset(resourceMetaDataset, fmt.Sprintf(`SELECT DISTINCT ?g WHERE { GRAPH ?g { ?g <%s> <%s> } }`, shacl.DCTERMS_CONFORMS_TO.RawValue(), shape))
	if err != nil {
		return nil, err
	}
	res, err := sparql.ParseJSON(bytes.NewReader(bindings))
	if err != nil {
		return nil, err
	}
	result := make([]string, 0)
	for _, row := range res.Solutions() {
		resource, ok := row["g"].(rdf.Subject)
		if !ok {
			return nil, fmt.Errorf("invalid binding: %v", row)
		}
		result = append(result, resource.String())
	}
	return result, nil
}

// validateCreator ensures the requester matches stored creator metadata.
// It returns nil when allowed or an error when the creator does not match.
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
