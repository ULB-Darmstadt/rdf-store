package search

import (
	"bytes"
	"log/slog"
	"rdf-store-backend/base"
	"rdf-store-backend/rdf"
	"reflect"
	"time"

	"github.com/deiu/rdf2go"
)

// Init prepares the Solr collection and schema.
func Init(forceRecreate bool) error {
	if forceRecreate || !checkCollectionExists() {
		if err := recreateCollection(); err != nil {
			return err
		}
	}
	return nil
}

// Reindex rebuilds the Solr index from all resources.
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
func IndexResource(resource *rdf2go.Graph, metadata *rdf.ResourceMetadata) error {
	if err := DeindexResource(metadata.Id.RawValue()); err != nil {
		return err
	}

	rootProfile, ok := metadata.Conformance[metadata.Id.RawValue()]
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

	buildDoc(metadata.Id, rootProfile, resource, &mainDocument)
	for resourceId, profileId := range metadata.Conformance {
		if resourceId != metadata.Id.RawValue() {
			resourceIdTerm := rdf2go.NewResource(resourceId)
			childDocument := document{
				"id":    resourceId,
				"label": findLabels(resourceIdTerm, resource),
			}
			buildDoc(resourceIdTerm, profileId, resource, &childDocument)
			appendMultiValue("_children_", &childDocument, &mainDocument)
		}
	}
	return updateDoc(&containerDocument)
}

// DeindexResource removes documents associated with a resource.
func DeindexResource(id string) error {
	return deleteDocs(id)
}

// buildDoc recursively constructs Solr documents from RDF graph data.
func buildDoc(subject rdf2go.Term, profileId string, resource *rdf2go.Graph, current *document) {
	slog.Debug("build doc", "subject", subject.RawValue(), "profile", profileId, "current", (*current)["id"])
	profile, ok := rdf.Profiles[profileId]
	if !ok {
		slog.Warn("profile not found", "id", profileId)
		return
	}
	// append shape conformance
	appendMultiValue("shape", profileId, current)

	// append property values to document
	for pathId, properties := range profile.Properties {
		path := rdf2go.NewResource(pathId)
		for _, property := range properties {
			ft := fieldType(property)
			for _, value := range resource.All(subject, path, nil) {
				raw := value.Object.RawValue()
				if ft == "t" {
					appendMultiValue("_text_", raw, current)
				} else {
					if ft == "dts" && len(raw) == 10 {
						raw = raw + "T00:00:00Z"
					}
					field := base.CleanStringForSolr(profile.Id.RawValue()) + "." + base.CleanStringForSolr(property.Id.RawValue()) + "_" + ft
					appendMultiValue(field, raw, current)
				}
			}
		}
	}

	for parentId := range profile.Parents {
		// append properties of all parent shapes to same document
		buildDoc(subject, parentId, resource, current)
	}
}

/*
func buildDoc2(subject rdf2go.Term, profileId rdf2go.Term, profile *shacl.NodeShape, data *rdf2go.Graph, current *document, denormalized bool) {
	slog.Debug("build doc", "subject", subject.RawValue(), "profile", profile.Id.RawValue(), "current", (*current)["id"])
	if !denormalized {
		appendMultiValue("shape", profileId.RawValue(), current)
	}

	for pathId, properties := range profile.Properties {
		path := rdf2go.NewResource(pathId)
		for _, property := range properties {
			values := data.All(subject, path, nil)
			ft := fieldType(property)
			if len(values) > 0 {
				array := make([]any, 0)
				for _, value := range values {
					if len(property.QualifiedValueShape) > 0 && property.QualifiedMinCount > 0 && property.QualifiedValueShapeDenormalized != nil {
						valid, err := shacl.Validate(string(*profile.RDF), property.QualifiedValueShape, *dataTurtle, value.Object.RawValue())
						if err != nil {
							slog.Warn("error indexing resource because validation failed", "error", err)
						} else if valid {
							// RDF graph conforms to this shape
							nested := document{
								"id":         value.Object.RawValue(),
								"shape":      property.QualifiedValueShapeDenormalized.ParentList(),
								"label":      findLabels(value.Object, data),
								"ref_shapes": make([]any, 0),
							}
							appendMultiValue("ref_shapes", property.QualifiedValueShape, current)
							data.Remove(value)
							appendMultiValue("_children_", &nested, current)
							buildDoc(value.Object, rdf2go.NewResource(property.QualifiedValueShape), property.QualifiedValueShapeDenormalized, data, &nested, true)
						}
					}
					for nodeProfileId := range property.Or {
						// validate value according to sh:or
						if nodeProfile, ok := rdf.Profiles[nodeProfileId]; ok {
							valid, err := shacl.Validate(string(*nodeProfile.RDF), nodeProfileId, *dataTurtle, value.Object.RawValue())
							if err != nil {
								slog.Warn("error indexing resource because validation failed", "error", err)
							} else if valid {
								// RDF graph conforms to this shape
								nested := document{
									"id":         value.Object.RawValue(),
									"shape":      make([]any, 0),
									"label":      findLabels(value.Object, data),
									"ref_shapes": make([]any, 0),
								}
								data.Remove(value)
								appendMultiValue("_children_", &nested, current)
								buildDoc(value.Object, nodeProfile.Id, nodeProfile, data, &nested, false)
							}
						} else {
							slog.Warn("property's node shape not found", "id", nodeProfileId, "path", property.Path)
						}
					}

					if len(property.NodeShapes) > 0 {
						for nodeProfileId := range property.NodeShapes {
							if nodeProfile, ok := rdf.Profiles[nodeProfileId]; ok {
								nested := document{
									"id":         value.Object.RawValue(),
									"shape":      make([]any, 0),
									"label":      findLabels(value.Object, data),
									"ref_shapes": make([]any, 0),
								}
								data.Remove(value)
								appendMultiValue("_children_", &nested, current)
								buildDoc(value.Object, nodeProfile.Id, nodeProfile, data, &nested, false)
							} else {
								slog.Warn("property's node shape not found", "id", nodeProfileId, "path", property.Path)
							}
						}
					}
					if _, ok := value.Object.(*rdf2go.Resource); ok {
						if data.One(value.Object, nil, nil) == nil {
							array = append(array, value.Object.String())
							data.Remove(value)
						}
					} else if _, ok := value.Object.(*rdf2go.Literal); ok {
						val := value.Object.RawValue()
						if ft == "dts" && len(val) == 10 {
							val = val + "T00:00:00Z"
						}
						array = append(array, val)
						data.Remove(value)
					}
				}
				if len(array) > 0 {
					if ft == "t" {
						appendMultiValue("_text_", array, current)
					} else {
						field := base.CleanStringForSolr(profile.Id.RawValue()) + "." + base.CleanStringForSolr(property.Id.RawValue()) + "_" + ft
						appendMultiValue(field, array, current)
					}
				}
			}
		}
	}

	if !denormalized {
		for parentId := range profile.Parents {
			if parentProfile, ok := rdf.Profiles[parentId]; ok {
				buildDoc(subject, parentProfile.Id, parentProfile, data, current, false)
			} else {
				slog.Warn("profile parent not found.", "id", parentId)
			}
		}
	}
}
*/
// appendMultiValue appends values to a multi-value Solr field.
func appendMultiValue(field string, value any, doc *document) {
	if _, ok := (*doc)[field]; !ok {
		(*doc)[field] = make([]any, 0)
	}
	if reflect.TypeOf(value).Kind() == reflect.Slice {
		(*doc)[field] = append((*doc)[field].([]any), value.([]any)...)
	} else {
		(*doc)[field] = append((*doc)[field].([]any), value)
	}
}

// findLabels collects literal labels for a subject in the graph.
func findLabels(subject rdf2go.Term, data *rdf2go.Graph) (labels []string) {
	for _, triple := range data.All(subject, nil, nil) {
		if _, ok := rdf.LabelPredicates[triple.Predicate.RawValue()]; ok {
			labels = append(labels, triple.Object.RawValue())
		}
	}
	return labels
}
