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
	"sort"
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
// Every entity conforming to a SHACL shape becomes its own search document.
// It returns an error when indexing or deindexing fails.
func IndexResource(resource *rdf2go.Graph, metadata *rdf.ResourceMetadata) error {
	if err := DeindexResource(metadata.Id.RawValue()); err != nil {
		return err
	}
	docs, err := buildResourceDocuments(resource, metadata)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return nil
	}
	return updateDocs(docs)
}

// buildResourceDocuments creates one Solr document for every entity in the
// resource that conforms to a SHACL shape. The resource root is indexed like
// any other entity. Documents link back to their owning resource via the
// resourceId field and carry their RDF subject for highlighting.
func buildResourceDocuments(resource *rdf2go.Graph, metadata *rdf.ResourceMetadata) ([]*document, error) {
	_, profile, err := rdf.FindResourceProfile(resource, metadata.Id)
	if err != nil {
		slog.Warn("not indexing because resource misses conformance entry", "resource", metadata.Id.RawValue(), "creator", metadata.Creator)
		return nil, nil
	}
	rootProfileId := profile.Id.RawValue()
	if !conforms(metadata.Id.RawValue(), rootProfileId, metadata) {
		slog.Warn("not indexing because root profile conformance is missing", "resource", metadata.Id.RawValue(), "creator", metadata.Creator, "rootProfile", rootProfileId)
		return nil, nil
	}

	slog.Debug("indexing", "resource", metadata.Id.RawValue(), "creator", metadata.Creator)

	docsBySubject := make(map[string]*document, len(metadata.Conformance))
	docs := make([]*document, 0, len(metadata.Conformance))
	for subjectID, shapeIDs := range metadata.Conformance {
		shapeIDs = orderShapesBySpecificity(shapeIDs)
		subject := rdf2go.NewResource(subjectID)
		doc := &document{
			"id":           metadata.Id.RawValue() + "|" + subjectID,
			"resourceId":   metadata.Id.RawValue(),
			"subject":      subjectID,
			"creator":      metadata.Creator,
			"lastModified": metadata.LastModified,
			"label":        findLabels(subject, resource),
		}
		for _, shapeID := range shapeIDs {
			doc.appendValue("shape", shapeID)
			shapeProfile, ok := rdf.Profiles[shapeID]
			if !ok {
				slog.Warn("profile not found", "id", shapeID)
				continue
			}
			buildDocRecursive(subject, shapeProfile, shapeID, resource, metadata, doc, make(map[string]bool))
		}
		docsBySubject[subjectID] = doc
		docs = append(docs, doc)
	}

	rootShapes := collectRootShapes(profile)
	rootDoc := docsBySubject[metadata.Id.RawValue()]
	for _, rootShape := range rootShapes {
		for _, doc := range docs {
			doc.appendValue("rootShape", rootShape)
		}
	}
	// Query fields are only consumed by the SHACL query UI, so skip them
	// unless that mode is enabled to keep the indexed field count small.
	if base.Configuration.ShaclQueryMode {
		for _, rootShape := range rootShapes {
			traversal := &queryTraversalState{
				active:   make(map[string]bool),
				visited:  make(map[string]bool),
				entities: docsBySubject,
			}
			buildQueryDoc(metadata.Id, profile, rootShape, nil, nil, resource, metadata, rootDoc, rootDoc, traversal)
		}
		// Every entity is additionally traversed from its own most specific
		// shape so its leaf values are also indexed under paths relative to the
		// shapes it conforms to. Without this, values of embedded entities (like
		// the water bath inside a cooling process) are only indexed under paths
		// relative to the containing resource root, so facets for the embedded
		// entity's own shape miss them.
		for subjectID, entityDoc := range docsBySubject {
			shapeIDs := orderShapesBySpecificity(metadata.Conformance[subjectID])
			if len(shapeIDs) == 0 {
				continue
			}
			topShapeID := shapeIDs[0]
			topShape, ok := rdf.Profiles[topShapeID]
			if !ok {
				slog.Warn("profile not found", "id", topShapeID)
				continue
			}
			traversal := &queryTraversalState{
				active:   make(map[string]bool),
				visited:  make(map[string]bool),
				entities: docsBySubject,
			}
			buildQueryDoc(rdf2go.NewResource(subjectID), topShape, topShapeID, nil, nil, resource, metadata, entityDoc, entityDoc, traversal)
		}
	}
	return docs, nil
}

// collectRootShapes returns the concrete root shape ID and every shape ID that
// it inherits. The frontend can therefore select a base shape and use the query
// fields derived from that base shape to find specialized entities as well.
func collectRootShapes(profile *shacl.NodeShape) []string {
	visited := make(map[string]bool)
	var shapes []string
	var visit func(*shacl.NodeShape)
	visit = func(current *shacl.NodeShape) {
		profileID := current.Id.RawValue()
		if visited[profileID] {
			return
		}
		visited[profileID] = true
		shapes = append(shapes, profileID)
		for parentID := range current.Parents {
			if parent, ok := rdf.Profiles[parentID]; ok {
				visit(parent)
			} else {
				slog.Warn("profile not found", "id", parentID)
			}
		}
	}
	visit(profile)
	return shapes
}

// DeindexResource removes all search documents associated with a resource ID.
// It returns an error if the deletion request fails.
func DeindexResource(id string) error {
	return deleteByResourceId(id)
}

// orderShapesBySpecificity reorders a shape conformance list so that the most
// specific named profile comes first, followed by the remaining named profiles,
// and finally anonymous urn: shapes. The shape field order determines which
// profile is displayed for an entity, so the most derived conforming profile is
// placed first.
func orderShapesBySpecificity(shapeIDs []string) []string {
	var named, others []string
	for _, id := range shapeIDs {
		if strings.HasPrefix(id, rdf.BlankNodeReplacement) {
			others = append(others, id)
		} else {
			named = append(named, id)
		}
	}
	if len(named) > 1 {
		ancestors := make(map[string]map[string]bool, len(named))
		for _, id := range named {
			ancestors[id] = collectAncestors(id)
		}
		sort.SliceStable(named, func(i, j int) bool {
			a, b := named[i], named[j]
			aCount, bCount := ancestorCountWithin(ancestors[a], named), ancestorCountWithin(ancestors[b], named)
			if aCount != bCount {
				return aCount > bCount
			}
			if len(ancestors[a]) != len(ancestors[b]) {
				return len(ancestors[a]) > len(ancestors[b])
			}
			return a < b
		})
	}
	return append(named, others...)
}

// collectAncestors returns the transitive set of ancestor profile IDs of a shape.
func collectAncestors(shapeID string) map[string]bool {
	ancestors := make(map[string]bool)
	var visit func(id string)
	visit = func(id string) {
		if ancestors[id] {
			return
		}
		ancestors[id] = true
		profile, ok := rdf.Profiles[id]
		if !ok {
			return
		}
		for parentID := range profile.Parents {
			visit(parentID)
		}
	}
	if profile, ok := rdf.Profiles[shapeID]; ok {
		for parentID := range profile.Parents {
			visit(parentID)
		}
	}
	return ancestors
}

// ancestorCountWithin returns how many of the given shapes are ancestors of the shape.
func ancestorCountWithin(ancestors map[string]bool, shapes []string) int {
	if len(ancestors) == 0 {
		return 0
	}
	count := 0
	for _, id := range shapes {
		if ancestors[id] {
			count++
		}
	}
	return count
}

// buildDocRecursive recursively constructs Solr documents from RDF graph data.
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

// buildQueryDoc adds query fields matching shacl-form's RDF predicate paths.
// Leaf values are written to the document of the entity that owns them, so that
// contained entities are search hits on their own. Field names are derived from
// the shape-relative path alone, so the same field is shared by every root
// shape that reaches a path and by every entity traversed from its own shape.
// Values of nested entities are additionally written to the nearest entity
// document that conforms to the root shape, so a criterion on a nested property
// (e.g. owner.firstName) matches the conforming ancestor rather than only the
// nested entity itself.
type queryTraversalState struct {
	active   map[string]bool
	visited  map[string]bool
	entities map[string]*document
}

func newQueryTraversalState() *queryTraversalState {
	return &queryTraversalState{
		active:  make(map[string]bool),
		visited: make(map[string]bool),
	}
}

func buildQueryDoc(subject rdf2go.Term, profile *shacl.NodeShape, rootShape string, propertyPath, shapePath []string, resource *rdf2go.Graph, metadata *rdf.ResourceMetadata, owner *document, conforming *document, traversal *queryTraversalState) {
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
			buildQueryDoc(subject, parent, rootShape, propertyPath, shapePath, resource, metadata, owner, conforming, traversal)
		}
	}
	for alternativeId := range profile.Alternatives {
		if alternative, ok := rdf.Profiles[alternativeId]; ok && conforms(subject.RawValue(), alternativeId, metadata) {
			buildQueryDoc(subject, alternative, rootShape, propertyPath, shapePath, resource, metadata, owner, conforming, traversal)
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
				childDoc := traversal.entities[value.Object.RawValue()]
				if _, literal := value.Object.(*rdf2go.Literal); !literal {
					// A query field is only reachable by the SHACL query UI when the
					// document that carries it belongs to a resource whose root shape
					// chain includes rootShape. Keep a reference to the nearest entity
					// document that conforms to rootShape so leaf values of nested
					// entities are also stored there. Otherwise a criterion on a nested
					// property (e.g. owner.firstName) would only match the nested entity
					// document, which does not conform to the selected shape itself.
					nextConforming := conforming
					if childDoc != nil && conforms(value.Object.RawValue(), rootShape, metadata) {
						nextConforming = childDoc
					}
					if structuredPropertyIsFacet(property, childShapes) {
						// dash:facet explicitly marks a structured property as a facet
						// over its referenced resources. Index the resource ID at the
						// relationship path instead of recursing into the nested shape.
						for shape := range childShapes {
							if !conforms(value.Object.RawValue(), shape, metadata) {
								continue
							}
							if owner != nil {
								appendQueryValue(owner, nextShapePath, value.Object)
							}
							if conforming != nil && conforming != owner {
								appendQueryValue(conforming, nextShapePath, value.Object)
							}
							break
						}
						recursed = true
					} else {
						for shape := range childShapes {
							child, ok := rdf.Profiles[shape]
							if ok && conforms(value.Object.RawValue(), shape, metadata) {
								buildQueryDoc(value.Object, child, rootShape, nextPropertyPath, nextShapePath, resource, metadata, childDoc, nextConforming, traversal)
								recursed = true
							}
						}
					}
				}
				if !recursed && len(childShapes) == 0 {
					if owner != nil {
						appendQueryValue(owner, nextShapePath, value.Object)
					}
					if conforming != nil && conforming != owner {
						appendQueryValue(conforming, nextShapePath, value.Object)
					}
				}
			}
		}
	}
}

func structuredPropertyIsFacet(property *shacl.Property, childShapes map[string]bool) bool {
	if property.Facet != nil {
		return *property.Facet
	}
	for shape := range childShapes {
		if profile, ok := rdf.Profiles[shape]; ok && profile.Facet != nil && *profile.Facet {
			return true
		}
	}
	return false
}

func appendPath(path []string, value string) []string {
	result := make([]string, len(path), len(path)+1)
	copy(result, path)
	return append(result, value)
}

func queryFieldName(shapePath []string, fieldType string) string {
	hash := sha256.New()
	for i, shape := range shapePath {
		if i > 0 {
			_, _ = hash.Write([]byte{0})
		}
		_, _ = hash.Write([]byte(shape))
	}
	digest := hash.Sum(nil)
	return "_query_." + hex.EncodeToString(digest[:16]) + "_" + fieldType
}

func appendQueryValue(current *document, shapePath []string, value rdf2go.Term) {
	slog.Debug("append query value", "path", shapePath, "value", value.String(), "doc", (*current)["id"])
	if literal, ok := value.(*rdf2go.Literal); ok {
		datatype := ""
		if literal.Datatype != nil {
			datatype = literal.Datatype.RawValue()
		}
		suffix := datatypeMappings[datatype]
		switch suffix {
		case "ds", "bs", "srpt", "ss":
			current.appendValue(queryFieldName(shapePath, suffix), literal.RawValue())
		case "dts":
			date := literal.RawValue()
			if len(date) == 10 {
				date += "T00:00:00Z"
			} else if !strings.HasSuffix(date, "Z") && !hasTimezoneOffset(date) {
				date += "Z"
			}
			current.appendValue(queryFieldName(shapePath, suffix), date)
		default:
			current.appendValue(queryFieldName(shapePath, "txt"), literal.RawValue())
		}
		return
	}
	current.appendValue(queryFieldName(shapePath, "ss"), value.String())
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
