package search

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"rdf-store-backend/base"
	"rdf-store-backend/rdf"
	"rdf-store-backend/shacl"
	"strings"
	"time"

	"github.com/deiu/rdf2go"
)

// Init prepares the Solr collection and schema for indexing.
// It returns an error if Solr cannot be reached or initialized.
func Init(forceRecreate bool) error {
	if forceRecreate {
		return recreateCollection()
	}
	const maxAttempts = 30
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		exists, err := checkCollectionExists(context.Background())
		if err != nil {
			slog.Warn("solr not ready yet", "attempt", attempt, "max_attempts", maxAttempts, "error", err)
		} else if exists {
			return nil
		} else {
			return recreateCollection()
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("solr not ready after %d attempts", maxAttempts)
}

// Reindex rebuilds the Solr index from all known resources.
func Reindex() {
	slog.Info("reindexing...")
	start := time.Now()
	if err := Init(true); err != nil {
		slog.Error("reindexing failed.", "error", err)
		return
	}
	resourceIds, err := rdf.GetAllResourceIds()
	if err != nil {
		slog.Error("reindexing failed.", "error", err)
		return
	}
	resourceCount := 0
	for _, id := range resourceIds {
		data, metadata, err := rdf.GetResource(id, false)
		if err != nil {
			slog.Error("failed loading resource", "id", id, "error", err)
		} else {
			graph, err := base.ParseGraph(bytes.NewReader(data))
			if err != nil {
				slog.Error(err.Error())
			} else {
				if err = IndexResource(graph, metadata); err != nil {
					slog.Error("failed indexing resource", "id", id, "error", err)
				} else {
					resourceCount = resourceCount + 1
				}
			}
		}
	}
	slog.Info("reindexing finished", "resources", resourceCount, "duration", time.Since(start))
}

// IndexResource builds and submits search documents for a resource.
// It returns an error when indexing or deindexing fails.
func IndexResource(resource *rdf2go.Graph, metadata *rdf.ResourceMetadata) error {
	if err := DeindexResource(metadata.Id.RawValue()); err != nil {
		return err
	}

	rootProfileId, ok := metadata.Conformance[metadata.Id.RawValue()]
	if !ok {
		slog.Warn("not indexing because resource misses conformance entry", "resource", metadata.Id.RawValue(), "creator", metadata.Creator)
		return nil
	}
	profile, ok := rdf.Profiles[rootProfileId]
	if !ok {
		slog.Warn("not indexing because root profile not found", "resource", metadata.Id.RawValue(), "creator", metadata.Creator, "rootProfile", rootProfileId)
		return nil
	}

	slog.Debug("indexing", "resource", metadata.Id.RawValue(), "creator", metadata.Creator)

	doc := &document{
		"id":           metadata.Id.RawValue(),
		"creator":      metadata.Creator,
		"lastModified": metadata.LastModified,
		"label":        findLabels(metadata.Id, resource),
	}
	buildDoc(metadata.Id, profile, profile.Id.RawValue(), resource, metadata, doc)
	return updateDoc(doc)
}

// DeindexResource removes documents associated with a resource ID.
// It returns an error if the deletion request fails.
func DeindexResource(id string) error {
	return deleteDoc(id)
}

// buildDoc recursively constructs Solr documents from RDF graph data.
func buildDoc(subject rdf2go.Term, profile *shacl.NodeShape, profileId string, resource *rdf2go.Graph, metadata *rdf.ResourceMetadata, current *document) {
	slog.Debug("build doc", "subject", subject.RawValue(), "profile", profile.Id.RawValue(), "current", (*current)["id"])
	// append shape conformance
	current.appendValue("shape", profile.Id.RawValue())

	for parentId := range profile.Parents {
		profile, ok := rdf.Profiles[parentId]
		if !ok {
			slog.Warn("profile not found", "id", parentId)
			return
		}
		buildDoc(subject, profile, profile.Id.RawValue(), resource, metadata, current)
	}

	// append property values to document
	for path, properties := range profile.Properties {
		pathTerm := rdf2go.NewResource(path)
		for _, property := range properties {
			ft := fieldType(property)
			for _, value := range resource.All(subject, pathTerm, nil) {
				if property.QualifiedValueShapeDenormalized != nil && conforms(value.Object.RawValue(), property.QualifiedValueShape, metadata) {
					current.appendValue("_text_", findLabels(value.Object, resource))
					buildDoc(value.Object, property.QualifiedValueShapeDenormalized, property.QualifiedValueShapeDenormalized.Id.RawValue(), resource, metadata, current)
				} else if len(property.NodeShapes) > 0 {
					for shape := range property.NodeShapes {
						if conforms(value.Object.RawValue(), shape, metadata) {
							profile, ok := rdf.Profiles[shape]
							if !ok {
								slog.Error("profile not found", "id", shape)
							} else {
								current.appendValue("_text_", findLabels(value.Object, resource))
								buildDoc(value.Object, profile, shape, resource, metadata, current)
							}
						}
					}
				} else {
					if ft == "t" {
						current.appendValue("_text_", value.Object.RawValue())
					} else {
						var val string
						if literial, ok := value.Object.(*rdf2go.Literal); ok {
							val = literial.RawValue()
						} else {
							val = value.Object.String()
						}
						if ft == "dts" {
							// convert date to solr format
							if len(val) == 10 {
								val = val + "T00:00:00Z"
							} else if !strings.HasSuffix(val, "Z") {
								val = val + "Z"
							}
						}
						current.appendValue(fieldName(profileId, property.Id.RawValue(), ft), val)
					}
				}
			}
		}
	}
}

func conforms(id string, shape string, metadata *rdf.ResourceMetadata) bool {
	if len(id) == 0 || len(shape) == 0 {
		return false
	}
	val, ok := metadata.Conformance[id]
	return ok && val == shape
}

func fieldName(shape string, property string, fieldType string) string {
	return base.CleanStringForSolr(shape) + "." + base.CleanStringForSolr(property) + "_" + fieldType
}

// findLabels collects literal labels for a subject in the graph.
// It returns the collected label strings.
func findLabels(subject rdf2go.Term, data *rdf2go.Graph) (labels []string) {
	for _, triple := range data.All(subject, nil, nil) {
		if _, ok := rdf.LabelPredicates[triple.Predicate.RawValue()]; ok {
			labels = append(labels, triple.Object.RawValue())
		}
	}
	return labels
}
