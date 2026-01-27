package search

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"rdf-store-backend/base"
	"rdf-store-backend/rdf"
	"rdf-store-backend/shacl"
	"reflect"
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
		slog.Warn("not indexing because root profile not found", "resource", metadata.Id.RawValue(), "creator", metadata.Creator)
		return nil
	}
	slog.Debug("indexing", "resource", metadata.Id.RawValue(), "creator", metadata.Creator)

	mainDocument := document{
		"id":           metadata.Id.RawValue(),
		"creator":      metadata.Creator,
		"lastModified": metadata.LastModified,
		"label":        findLabels(metadata.Id, resource),
		"docType":      "main",
	}
	containerDocument := document{
		"id":           "container_" + metadata.Id.RawValue(),
		"creator":      metadata.Creator,
		"lastModified": metadata.LastModified,
		"docType":      "container",
		"_children_":   []*document{&mainDocument},
	}
	profile, ok := rdf.Profiles[rootProfileId]
	if !ok {
		slog.Warn("profile not found", "id", rootProfileId)
		return fmt.Errorf("profile not found %v", rootProfileId)
	}
	buildDoc(metadata.Id, profile, false, resource, metadata, &mainDocument, &mainDocument)
	return updateDoc(&containerDocument)
}

// DeindexResource removes documents associated with a resource ID.
// It returns an error if the deletion request fails.
func DeindexResource(id string) error {
	return deleteDocs(id)
}

// buildDoc recursively constructs Solr documents from RDF graph data.
func buildDoc(subject rdf2go.Term, profile *shacl.NodeShape, isProperty bool, resource *rdf2go.Graph, metadata *rdf.ResourceMetadata, current *document, parent *document) {
	slog.Debug("build doc", "subject", subject.RawValue(), "profile", profile.Id.RawValue(), "current", (*current)["id"])
	// append shape conformance
	appendMultiValue("shape", profile.Id.RawValue(), current)

	for parentId := range profile.Parents {
		profile, ok := rdf.Profiles[parentId]
		if !ok {
			slog.Warn("profile not found", "id", parentId)
			return
		}
		buildDoc(subject, profile, isProperty, resource, metadata, current, parent)
	}

	// append property values to document
	for path, properties := range profile.Properties {
		pathTerm := rdf2go.NewResource(path)
		for _, property := range properties {
			ft := fieldType(property)
			for _, value := range resource.All(subject, pathTerm, nil) {
				if len(property.NodeShapes) > 0 {
					for shape := range property.NodeShapes {
						if conformingShape, ok := metadata.Conformance[value.Object.RawValue()]; ok && conformingShape == shape {
							profile, ok := rdf.Profiles[shape]
							if !ok {
								slog.Warn("profile not found", "id", shape)
							} else {
								childDocument := document{
									"id":    value.Object.RawValue(),
									"label": findLabels(value.Object, resource),
								}
								appendMultiValue("_children_", &childDocument, current)
								appendMultiValue("ref_shapes", profile.Id.RawValue(), current)
								appendMultiValue("ref_shapes", profile.Id.RawValue(), parent)
								appendMultiValue("ref_shapes", (*parent)["shape"], &childDocument)
								appendMultiValue("ref_shapes", (*current)["shape"], &childDocument)
								buildDoc(value.Object, profile, true, resource, metadata, &childDocument, &childDocument)
							}
						}
					}
				} else {
					var val string
					if literial, ok := value.Object.(*rdf2go.Literal); ok {
						val = literial.RawValue()
					} else {
						val = value.Object.String()
					}
					if ft == "t" {
						appendMultiValue("_text_", val, current)
					} else {
						if ft == "dts" {
							// convert date to solr format
							if len(val) == 10 {
								val = val + "T00:00:00Z"
							} else if len(val) == 19 {
								val = val + "Z"
							}
						}
						var targetProfile string
						if isProperty {
							targetProfile = docMainShape(parent)
						} else {
							targetProfile = profile.Id.RawValue()
						}
						field := base.CleanStringForSolr(targetProfile) + "." + base.CleanStringForSolr(property.Id.RawValue()) + "_" + ft
						appendMultiValue(field, val, current)
					}
				}
			}
		}

	}
}

// appendMultiValue appends values to a multi-value Solr field.
func appendMultiValue(field string, value any, doc *document) {
	if value == nil {
		return
	}
	// fmt.Println("--- append", field, value)
	existing, ok := (*doc)[field].([]any)
	if !ok {
		existing = make([]any, 0)
	}
	valueRef := reflect.ValueOf(value)
	if valueRef.Kind() == reflect.Slice {
		for i := 0; i < valueRef.Len(); i++ {
			existing = append(existing, valueRef.Index(i).Interface())
		}
	} else {
		existing = append(existing, value)
	}
	(*doc)[field] = existing
}

func docMainShape(doc *document) string {
	if shapes, ok := (*doc)["shape"].([]any); ok && len(shapes) > 0 {
		if id, ok := shapes[0].(string); ok {
			return id
		}
	}
	return ""
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
