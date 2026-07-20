package search

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	_, profile, err := rdf.FindResourceProfile(resource, metadata.Id)
	if err != nil {
		slog.Warn("not indexing because resource misses conformance entry", "resource", metadata.Id.RawValue(), "creator", metadata.Creator)
		return nil
	}
	rootProfileId := profile.Id.RawValue()
	if !conforms(metadata.Id.RawValue(), rootProfileId, metadata) {
		slog.Warn("not indexing because root profile conformance is missing", "resource", metadata.Id.RawValue(), "creator", metadata.Creator, "rootProfile", rootProfileId)
		return nil
	}

	slog.Debug("indexing", "resource", metadata.Id.RawValue(), "creator", metadata.Creator)

	doc := &document{
		"id":           metadata.Id.RawValue(),
		"rootShape":    rootProfileId,
		"creator":      metadata.Creator,
		"lastModified": metadata.LastModified,
		"label":        findLabels(metadata.Id, resource),
	}
	buildDoc(metadata.Id, profile, profile.Id.RawValue(), resource, metadata, doc)
	buildQueryDoc(metadata.Id, profile, rootProfileId, nil, nil, resource, metadata, doc, newQueryTraversalState())
	return updateDoc(doc)
}

// DeindexResource removes documents associated with a resource ID.
// It returns an error if the deletion request fails.
func DeindexResource(id string) error {
	return deleteDoc(id)
}

// buildDoc recursively constructs Solr documents from RDF graph data.
func buildDoc(subject rdf2go.Term, profile *shacl.NodeShape, profileId string, resource *rdf2go.Graph, metadata *rdf.ResourceMetadata, current *document) {
	buildDocRecursive(subject, profile, profileId, resource, metadata, current, make(map[string]bool))
}

func buildDocRecursive(subject rdf2go.Term, profile *shacl.NodeShape, profileId string, resource *rdf2go.Graph, metadata *rdf.ResourceMetadata, current *document, active map[string]bool) {
	visitKey := subject.RawValue() + "\x00" + profile.Id.RawValue()
	if active[visitKey] {
		slog.Warn("skipping recursive index shape", "subject", subject.RawValue(), "shape", profile.Id.RawValue())
		return
	}
	active[visitKey] = true
	defer delete(active, visitKey)

	slog.Debug("build doc", "subject", subject.RawValue(), "profile", profile.Id.RawValue(), "current", (*current)["id"])
	// append shape conformance
	current.appendValue("shape", profile.Id.RawValue())

	for parentId := range profile.Parents {
		parent, ok := rdf.Profiles[parentId]
		if !ok {
			slog.Warn("profile not found", "id", parentId)
			continue
		}
		buildDocRecursive(subject, parent, parent.Id.RawValue(), resource, metadata, current, active)
	}
	for alternativeId := range profile.Alternatives {
		if !conforms(subject.RawValue(), alternativeId, metadata) {
			continue
		}
		alternative, ok := rdf.Profiles[alternativeId]
		if !ok {
			slog.Warn("profile not found", "id", alternativeId)
			continue
		}
		buildDocRecursive(subject, alternative, alternative.Id.RawValue(), resource, metadata, current, active)
	}

	// append property values to document
	for path, properties := range profile.Properties {
		pathTerm := rdf2go.NewResource(path)
		for _, property := range properties {
			ft := fieldType(property)
			for _, value := range resource.All(subject, pathTerm, nil) {
				if property.QualifiedValueShapeDenormalized != nil && conforms(value.Object.RawValue(), property.QualifiedValueShape, metadata) {
					current.appendValue("_text_", findLabels(value.Object, resource))
					buildDocRecursive(value.Object, property.QualifiedValueShapeDenormalized, property.QualifiedValueShapeDenormalized.Id.RawValue(), resource, metadata, current, active)
				} else if len(property.NodeShapes) > 0 || len(property.AlternativeNodeShapes) > 0 {
					childShapes := make(map[string]bool, len(property.NodeShapes)+len(property.AlternativeNodeShapes))
					for shape := range property.NodeShapes {
						childShapes[shape] = true
					}
					for shape := range property.AlternativeNodeShapes {
						childShapes[shape] = true
					}
					for shape := range childShapes {
						if conforms(value.Object.RawValue(), shape, metadata) {
							profile, ok := rdf.Profiles[shape]
							if !ok {
								slog.Error("profile not found", "id", shape)
							} else {
								current.appendValue("_text_", findLabels(value.Object, resource))
								buildDocRecursive(value.Object, profile, shape, resource, metadata, current, active)
							}
						}
					}
				} else {
					if ft == "t" {
						current.appendValue(fieldName(profileId, property.Id.RawValue(), "txt"), value.Object.RawValue())
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
							} else if !strings.HasSuffix(val, "Z") && !hasTimezoneOffset(val) {
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

// buildQueryDoc adds root-aware fields matching shacl-form's RDF predicate
// paths. Values remain flattened into the root document by design.
type queryTraversalState struct {
	active  map[string]bool
	visited map[string]bool
}

func newQueryTraversalState() *queryTraversalState {
	return &queryTraversalState{
		active:  make(map[string]bool),
		visited: make(map[string]bool),
	}
}

func buildQueryDoc(subject rdf2go.Term, profile *shacl.NodeShape, rootShape string, propertyPath, shapePath []string, resource *rdf2go.Graph, metadata *rdf.ResourceMetadata, current *document, traversal *queryTraversalState) {
	activeKey := subject.RawValue() + "\x00" + profile.Id.RawValue()
	if traversal.active[activeKey] {
		slog.Warn("skipping recursive query-index shape", "subject", subject.RawValue(), "shape", profile.Id.RawValue())
		return
	}
	visitKey := activeKey + "\x00" + strings.Join(propertyPath, "\x00") + "\x00" + strings.Join(shapePath, "\x00")
	if traversal.visited[visitKey] {
		return
	}
	traversal.visited[visitKey] = true
	traversal.active[activeKey] = true
	defer delete(traversal.active, activeKey)

	for parentId := range profile.Parents {
		if parent, ok := rdf.Profiles[parentId]; ok {
			buildQueryDoc(subject, parent, rootShape, propertyPath, shapePath, resource, metadata, current, traversal)
		}
	}
	for alternativeId := range profile.Alternatives {
		if alternative, ok := rdf.Profiles[alternativeId]; ok && conforms(subject.RawValue(), alternativeId, metadata) {
			buildQueryDoc(subject, alternative, rootShape, propertyPath, shapePath, resource, metadata, current, traversal)
		}
	}

	for path, properties := range profile.Properties {
		pathTerm := rdf2go.NewResource(path)
		for _, property := range properties {
			nextPropertyPath := appendPath(propertyPath, path)
			shapePathSegment := path
			if property.QualifiedValueShape != "" {
				shapePathSegment = property.Id.RawValue()
			}
			nextShapePath := appendPath(shapePath, shapePathSegment)
			for _, value := range resource.All(subject, pathTerm, nil) {
				childShapes := make(map[string]bool, len(property.NodeShapes)+len(property.AlternativeNodeShapes)+1)
				for shape := range property.NodeShapes {
					childShapes[shape] = true
				}
				for shape := range property.AlternativeNodeShapes {
					childShapes[shape] = true
				}
				if property.QualifiedValueShape != "" {
					childShapes[property.QualifiedValueShape] = true
				}

				recursed := false
				if _, literal := value.Object.(*rdf2go.Literal); !literal {
					for shape := range childShapes {
						child, ok := rdf.Profiles[shape]
						if ok && conforms(value.Object.RawValue(), shape, metadata) {
							buildQueryDoc(value.Object, child, rootShape, nextPropertyPath, nextShapePath, resource, metadata, current, traversal)
							recursed = true
						}
					}
				}
				if !recursed && len(childShapes) == 0 {
					appendQueryValue(current, rootShape, nextShapePath, value.Object)
				}
			}
		}
	}
}

func appendPath(path []string, value string) []string {
	result := make([]string, len(path), len(path)+1)
	copy(result, path)
	return append(result, value)
}

func queryFieldName(rootShape string, shapePath []string, fieldType string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(rootShape))
	for _, shape := range shapePath {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(shape))
	}
	digest := hash.Sum(nil)
	return "_query_." + hex.EncodeToString(digest[:16]) + "_" + fieldType
}

func appendQueryValue(current *document, rootShape string, shapePath []string, value rdf2go.Term) {
	if literal, ok := value.(*rdf2go.Literal); ok {
		datatype := ""
		if literal.Datatype != nil {
			datatype = literal.Datatype.RawValue()
		}
		suffix := datatypeMappings[datatype]
		switch suffix {
		case "ds", "bs", "srpt":
			current.appendValue(queryFieldName(rootShape, shapePath, suffix), literal.RawValue())
		case "dts":
			date := literal.RawValue()
			if len(date) == 10 {
				date += "T00:00:00Z"
			} else if !strings.HasSuffix(date, "Z") && !hasTimezoneOffset(date) {
				date += "Z"
			}
			current.appendValue(queryFieldName(rootShape, shapePath, suffix), date)
		default:
			current.appendValue(queryFieldName(rootShape, shapePath, "txt"), literal.RawValue())
			current.appendValue(queryFieldName(rootShape, shapePath, "ss"), literal.RawValue())
		}
		return
	}
	current.appendValue(queryFieldName(rootShape, shapePath, "ss"), value.String())
}

func hasTimezoneOffset(value string) bool {
	if len(value) < 6 {
		return false
	}
	suffix := value[len(value)-6:]
	return (suffix[0] == '+' || suffix[0] == '-') && suffix[3] == ':'
}

func conforms(id string, shape string, metadata *rdf.ResourceMetadata) bool {
	if len(id) == 0 || len(shape) == 0 {
		return false
	}
	values, ok := metadata.Conformance[id]
	if !ok {
		return false
	}
	for _, value := range values {
		if value == shape {
			return true
		}
	}
	return false
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
