package rdf

import (
	"fmt"
	"log/slog"
)

var prefixDcTerms = "http://purl.org/dc/terms/%s"
var dcTermsCreator = fmt.Sprintf(prefixDcTerms, "creator")
var dcTermsModified = fmt.Sprintf(prefixDcTerms, "modified")

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
func CreateResource(id string, resource []byte, creator string) (*ResourceMetadata, error) {
	if err := createGraph(ResourceDataset, id, resource); err != nil {
		return nil, err
	}
	return updateResourceMetadata(id, creator)
}

// UpdateResource validates permissions, updates the graph, and metadata.
func UpdateResource(id string, resource []byte, creator string) (*ResourceMetadata, error) {
	if err := validateCreator(id, creator); err != nil {
		return nil, err
	}
	if err := uploadGraph(ResourceDataset, id, resource, nil); err != nil {
		return nil, err
	}
	return updateResourceMetadata(id, creator)
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
	bindings, err := queryDataset(ResourceDataset, fmt.Sprintf(`SELECT ?s ?p ?o ?g WHERE  { GRAPH ?g { ?c a <%s> . ?c (<>|!<>)* ?s . ?s ?p ?o }}`, class))
	if err != nil {
		return nil, err
	}
	return sparqlResultToNQuads(bindings)
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
