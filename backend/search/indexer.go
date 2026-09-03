package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"rdf-store-backend/base"
	"rdf-store-backend/rdf"
	"rdf-store-backend/search/qudt"
	"rdf-store-backend/shacl"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/deiu/rdf2go"
)

var defaultConversionPredicates = qudt.NewPredicateConfig(
	base.Configuration.ConversionUnit,
	base.Configuration.ConversionQuantity,
	base.Configuration.ConversionValue,
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
func Reindex() error {
	slog.Info("reindexing...")
	start := time.Now()
	resourceIds, err := rdf.GetAllResourceIds()
	if err != nil {
		return fmt.Errorf("listing resources for reindex: %w", err)
	}
	loader := rdf.NewResourceIndexLoader()
	var documents []*document
	resourceCount := 0
	for _, id := range resourceIds {
		docs, err := buildStoredResourceDocuments(id, loader)
		if err != nil {
			return fmt.Errorf("preparing resource %s before replacing collection: %w", id, err)
		}
		documents = append(documents, docs...)
		resourceCount = resourceCount + 1
	}
	if err := Init(true); err != nil {
		return fmt.Errorf("recreating search collection: %w", err)
	}
	if len(documents) > 0 {
		if err := updateDocsInBatches(documents, 100); err != nil {
			return fmt.Errorf("submitting reindexed documents: %w", err)
		}
	}
	slog.Info("reindexing finished", "resources", resourceCount, "duration", time.Since(start))
	return nil
}

func updateDocsInBatches(documents []*document, batchSize int) error {
	if batchSize <= 0 {
		return fmt.Errorf("invalid document batch size %d", batchSize)
	}
	for start := 0; start < len(documents); start += batchSize {
		end := min(start+batchSize, len(documents))
		if err := updateDocsWithCommit(documents[start:end], end == len(documents)); err != nil {
			return err
		}
	}
	return nil
}

// IndexStoredResource expands recursively linked local resources and indexes
// their nested query values onto the requested resource's search documents.
func IndexStoredResource(id string) error {
	return IndexStoredResources([]string{id})
}

// IndexStoredResources rebuilds several resources with a shared linked-graph
// cache and a single final Solr commit.
func IndexStoredResources(ids []string) error {
	ids = sortedUniqueStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	loader := rdf.NewResourceIndexLoader()
	var documents []*document
	for _, id := range ids {
		docs, err := buildStoredResourceDocuments(id, loader)
		if err != nil {
			return err
		}
		documents = append(documents, docs...)
	}
	if err := deleteByResourceIds(ids, len(documents) == 0); err != nil {
		return err
	}
	if len(documents) == 0 {
		return nil
	}
	return updateDocsInBatches(documents, 100)
}

func sortedUniqueStrings(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func buildStoredResourceDocuments(id string, loader *rdf.ResourceIndexLoader) ([]*document, error) {
	resource, metadata, queryConformance, err := loader.Get(id)
	if err != nil {
		return nil, err
	}
	return buildDocuments(resource, metadata, queryConformance)
}

func buildDocuments(resource *rdf2go.Graph, metadata *rdf.ResourceMetadata, queryConformance map[string][]string) ([]*document, error) {
	labelIDs := make([]string, 0, len(metadata.Conformance))
	for subjectID := range metadata.Conformance {
		labelIDs = append(labelIDs, rdf2go.NewResource(subjectID).String())
	}
	labels, err := rdf.GetDefaultLabels(labelIDs)
	if err != nil {
		return nil, fmt.Errorf("loading extracted resource labels: %w", err)
	}
	docs, err := buildResourceDocuments(resource, metadata, resourceIndexOptions{
		conversionPredicates: defaultConversionPredicates,
		extractedLabels:      labels,
		queryConformance:     queryConformance,
	})
	if err != nil {
		return nil, err
	}
	return docs, nil
}

type resourceIndexOptions struct {
	conversionPredicates qudt.PredicateConfig
	extractedLabels      map[string]string
	queryConformance     map[string][]string
}

// buildResourceDocuments creates one Solr document for every entity in the
// resource that conforms to a SHACL shape. The resource root is indexed like
// any other entity. Documents link back to their owning resource via the
// resourceId field and carry their RDF subject for highlighting.
func buildResourceDocuments(resource *rdf2go.Graph, metadata *rdf.ResourceMetadata, options resourceIndexOptions) ([]*document, error) {
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
		labels := rdf.FindLabels(subject, resource)
		if extracted := options.extractedLabels[subject.String()]; extracted != "" {
			labels = []string{extracted}
		}
		doc := &document{
			"id":           metadata.Id.RawValue() + "|" + subjectID,
			"docType":      "entity",
			"resourceId":   metadata.Id.RawValue(),
			"subject":      subjectID,
			"creator":      metadata.Creator,
			"lastModified": metadata.LastModified,
			"label":        labels,
		}
		conformingShapes := make(map[string]bool)
		for _, shapeID := range shapeIDs {
			shapeProfile, ok := rdf.Profiles[shapeID]
			if !ok {
				slog.Warn("profile not found", "id", shapeID)
				continue
			}
			appendConformingShapes(subject, shapeProfile, metadata, doc, conformingShapes)
			buildDocRecursive(subject, shapeProfile, resource, metadata, doc, make(map[string]bool))
		}
		docsBySubject[subjectID] = doc
		docs = append(docs, doc)
	}

	queryMetadata := metadata
	if options.queryConformance != nil {
		copy := *metadata
		copy.Conformance = options.queryConformance
		queryMetadata = &copy
	}
	rootProfileChain := collectRootProfileChain(profile)
	rootDoc := docsBySubject[metadata.Id.RawValue()]
	valueDocuments := make(map[string]map[string]*document, len(docsBySubject))
	for _, targetShape := range rootProfileChain {
		traversal := &queryTraversalState{
			active:         make(map[string]bool),
			visited:        make(map[string]bool),
			entities:       docsBySubject,
			valueDocuments: valueDocuments,
		}
		newQueryIndexer(resource, queryMetadata, targetShape, traversal, options.conversionPredicates).index(metadata.Id, profile, rootDoc)
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
			active:         make(map[string]bool),
			visited:        make(map[string]bool),
			entities:       docsBySubject,
			valueDocuments: valueDocuments,
		}
		newQueryIndexer(resource, metadata, topShapeID, traversal, options.conversionPredicates).index(rdf2go.NewResource(subjectID), topShape, entityDoc)
	}
	return docs, nil
}

// appendConformingShapes records only shapes that the document subject itself
// conforms to. Property traversal must not add the shapes of referenced
// entities to the current document.
func appendConformingShapes(subject rdf2go.Term, profile *shacl.NodeShape, metadata *rdf.ResourceMetadata, current *document, visited map[string]bool) {
	profileID := profile.Id.RawValue()
	if visited[profileID] {
		return
	}
	visited[profileID] = true
	current.appendValue("shape", profileID)

	for parentID := range profile.Parents {
		parent, ok := rdf.Profiles[parentID]
		if !ok {
			slog.Warn("profile not found", "id", parentID)
			continue
		}
		appendConformingShapes(subject, parent, metadata, current, visited)
	}
	for alternativeID := range profile.Alternatives {
		if !conforms(subject.RawValue(), alternativeID, metadata) {
			continue
		}
		alternative, ok := rdf.Profiles[alternativeID]
		if !ok {
			slog.Warn("profile not found", "id", alternativeID)
			continue
		}
		appendConformingShapes(subject, alternative, metadata, current, visited)
	}
}

// collectRootProfileChain returns the concrete root shape ID and every shape ID
// that it inherits. Query fields are built for each shape in this chain so the
// frontend can query specialized entities through a selected base shape.
func collectRootProfileChain(profile *shacl.NodeShape) []string {
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
func buildDocRecursive(subject rdf2go.Term, profile *shacl.NodeShape, resource *rdf2go.Graph, metadata *rdf.ResourceMetadata, current *document, active map[string]bool) {
	visitKey := subject.RawValue() + "\x00" + profile.Id.RawValue()
	if active[visitKey] {
		slog.Warn("skipping recursive index shape", "subject", subject.RawValue(), "shape", profile.Id.RawValue())
		return
	}
	active[visitKey] = true
	defer delete(active, visitKey)

	slog.Debug("build doc", "subject", subject.RawValue(), "profile", profile.Id.RawValue(), "current", (*current)["id"])

	for parentId := range profile.Parents {
		parent, ok := rdf.Profiles[parentId]
		if !ok {
			slog.Warn("profile not found", "id", parentId)
			continue
		}
		buildDocRecursive(subject, parent, resource, metadata, current, active)
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
		buildDocRecursive(subject, alternative, resource, metadata, current, active)
	}

	// append property values to document
	for path, properties := range profile.Properties {
		pathTerm := rdf2go.NewResource(path)
		for _, property := range properties {
			for _, value := range resource.All(subject, pathTerm, nil) {
				if property.IsRdfCollection {
					for _, item := range shacl.WalkRdfCollection(value.Object, resource) {
						current.appendValue("_text_", item.RawValue())
						if literal, ok := item.(*rdf2go.Literal); ok {
							datatype := ""
							if literal.Datatype != nil {
								datatype = literal.Datatype.RawValue()
							}
							field := "valueText"
							storedValue := literal.RawValue()
							if suffix := datatypeMappings[datatype]; suffix != "" {
								switch suffix {
								case "ds":
									field = "valueNumber"
								case "bs":
									field = "valueBoolean"
								case "srpt":
									field = "valueGeo"
								case "dts":
									field = "valueDate"
									if len(storedValue) == 10 {
										storedValue += "T00:00:00Z"
									} else if !strings.HasSuffix(storedValue, "Z") && !hasTimezoneOffset(storedValue) {
										storedValue += "Z"
									}
								default:
									field = "valueText"
								}
							}
							current.appendValue(field, storedValue)
						} else {
							current.appendValue("valueString", item.RawValue())
						}
					}
				} else if property.QualifiedValueShapeDenormalized != nil && conforms(value.Object.RawValue(), property.QualifiedValueShape, metadata) {
					current.appendValue("_text_", rdf.FindLabels(value.Object, resource))
					buildDocRecursive(value.Object, property.QualifiedValueShapeDenormalized, resource, metadata, current, active)
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
								current.appendValue("_text_", rdf.FindLabels(value.Object, resource))
								buildDocRecursive(value.Object, profile, resource, metadata, current, active)
							}
						}
					}
				} else {
					current.appendValue("_text_", value.Object.RawValue())
				}
			}
		}
	}
}

// buildQueryDoc adds nested value documents matching shacl-form's RDF paths.
// Leaf values are written to the document of the entity that owns them, so that
// contained entities are search hits on their own. Path IDs are derived from the
// shape-relative path alone, so the same ID is shared by every root shape that
// reaches a path and by every entity traversed from its own shape.
// Values of nested entities are additionally written to the nearest entity
// parent that conforms to the root shape, so a criterion on a nested property
// (e.g. owner.firstName) matches the conforming ancestor rather than only the
// nested entity itself.
type queryTraversalState struct {
	active         map[string]bool
	visited        map[string]bool
	entities       map[string]*document
	valueDocuments map[string]map[string]*document
}

// queryIndexer owns the dependencies and mutable state shared by one query
// document traversal. Recursive calls only need to carry the node-specific
// state below.
type queryIndexer struct {
	resource             *rdf2go.Graph
	metadata             *rdf.ResourceMetadata
	targetShape          string
	traversal            *queryTraversalState
	conversionPredicates qudt.PredicateConfig
	conversionContexts   map[string]*qudt.QuantityContext
}

type queryIndexNode struct {
	subject      rdf2go.Term
	profile      *shacl.NodeShape
	propertyPath []string
	shapePath    []string
	owner        *document
	conforming   *document
}

type queryIndexValue struct {
	document     *document
	shapePath    []string
	predicateURI string
	term         rdf2go.Term
	quantity     *qudt.QuantityContext
}

func newQueryTraversalState() *queryTraversalState {
	return &queryTraversalState{
		active:         make(map[string]bool),
		visited:        make(map[string]bool),
		valueDocuments: make(map[string]map[string]*document),
	}
}

func newQueryIndexer(resource *rdf2go.Graph, metadata *rdf.ResourceMetadata, targetShape string, traversal *queryTraversalState, quantityPredicates qudt.PredicateConfig) *queryIndexer {
	return &queryIndexer{
		resource:             resource,
		metadata:             metadata,
		targetShape:          targetShape,
		traversal:            traversal,
		conversionPredicates: quantityPredicates,
		conversionContexts:   make(map[string]*qudt.QuantityContext),
	}
}

func (indexer *queryIndexer) index(subject rdf2go.Term, profile *shacl.NodeShape, current *document) {
	indexer.walk(queryIndexNode{
		subject:    subject,
		profile:    profile,
		owner:      current,
		conforming: current,
	})
}

func (indexer *queryIndexer) conversionContext(subject rdf2go.Term) *qudt.QuantityContext {
	key := subject.String()
	if context, scanned := indexer.conversionContexts[key]; scanned {
		return context
	}
	context := indexer.conversionPredicates.ScanConversionContext(subject, indexer.resource)
	indexer.conversionContexts[key] = context
	return context
}

func (indexer *queryIndexer) walk(node queryIndexNode) {
	quantity := indexer.conversionContext(node.subject)
	activeKey := node.subject.RawValue() + "\x00" + node.profile.Id.RawValue()
	if indexer.traversal.active[activeKey] {
		slog.Warn("skipping recursive query-index shape", "subject", node.subject.RawValue(), "shape", node.profile.Id.RawValue())
		return
	}
	visitKey := activeKey + "\x00" + strings.Join(node.propertyPath, "\x00") + "\x00" + strings.Join(node.shapePath, "\x00")
	if indexer.traversal.visited[visitKey] {
		return
	}
	indexer.traversal.visited[visitKey] = true
	indexer.traversal.active[activeKey] = true
	defer delete(indexer.traversal.active, activeKey)

	for parentId := range node.profile.Parents {
		if parent, ok := rdf.Profiles[parentId]; ok {
			parentNode := node
			parentNode.profile = parent
			indexer.walk(parentNode)
		}
	}
	for alternativeId := range node.profile.Alternatives {
		if alternative, ok := rdf.Profiles[alternativeId]; ok && conforms(node.subject.RawValue(), alternativeId, indexer.metadata) {
			alternativeNode := node
			alternativeNode.profile = alternative
			indexer.walk(alternativeNode)
		}
	}

	for path, properties := range node.profile.Properties {
		pathTerm := rdf2go.NewResource(path)
		for _, property := range properties {
			nextPropertyPath := appendPath(node.propertyPath, path)
			shapePathSegment := path
			if property.QualifiedValueShape != "" {
				shapePathSegment = property.Id.RawValue()
			}
			nextShapePath := appendPath(node.shapePath, shapePathSegment)
			for _, value := range indexer.resource.All(node.subject, pathTerm, nil) {
				appendTo := func(document *document, quantity *qudt.QuantityContext) {
					indexer.appendValue(queryIndexValue{
						document:     document,
						shapePath:    nextShapePath,
						predicateURI: path,
						term:         value.Object,
						quantity:     quantity,
					})
				}
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

				if property.IsRdfCollection {
					// List members are the rdf:first values inside the list node
					// shape. shacl-form's query fields address them as
					// "<property> rdf:first", so the query path must include the
					// rdf:first segment for facets to match the indexed values.
					itemShapePath := appendPath(nextShapePath, shacl.RDF_LIST_FIRST.RawValue())
					for _, item := range shacl.WalkRdfCollection(value.Object, indexer.resource) {
						if node.owner != nil {
							indexer.appendValue(queryIndexValue{
								document:     node.owner,
								shapePath:    itemShapePath,
								predicateURI: path,
								term:         item,
								quantity:     quantity,
							})
						}
						if node.conforming != nil && node.conforming != node.owner {
							indexer.appendValue(queryIndexValue{
								document:     node.conforming,
								shapePath:    itemShapePath,
								predicateURI: path,
								term:         item,
								quantity:     quantity,
							})
						}
					}
					continue
				}

				recursed := false
				childDoc := indexer.traversal.entities[value.Object.RawValue()]
				if _, literal := value.Object.(*rdf2go.Literal); !literal {
					// A query field is only reachable by the SHACL query UI when the
					// document that carries it belongs to a resource whose root profile
					// chain includes targetShape. Keep a reference to the nearest entity
					// document that conforms to targetShape so leaf values of nested
					// entities are also stored there. Otherwise a criterion on a nested
					// property (e.g. owner.firstName) would only match the nested entity
					// document, which does not conform to the selected shape itself.
					nextConforming := node.conforming
					if childDoc != nil && conforms(value.Object.RawValue(), indexer.targetShape, indexer.metadata) {
						nextConforming = childDoc
					}
					if structuredPropertyIsFacet(property, childShapes) {
						// dash:facet explicitly marks a structured property as a facet
						// over its referenced resources. Index the resource ID at the
						// relationship path instead of recursing into the nested shape.
						for shape := range childShapes {
							if !conforms(value.Object.RawValue(), shape, indexer.metadata) {
								continue
							}
							if node.owner != nil {
								appendTo(node.owner, nil)
							}
							if node.conforming != nil && node.conforming != node.owner {
								appendTo(node.conforming, nil)
							}
							break
						}
						recursed = true
					} else {
						for shape := range childShapes {
							child, ok := rdf.Profiles[shape]
							if ok && conforms(value.Object.RawValue(), shape, indexer.metadata) {
								indexer.walk(queryIndexNode{
									subject:      value.Object,
									profile:      child,
									propertyPath: nextPropertyPath,
									shapePath:    nextShapePath,
									owner:        childDoc,
									conforming:   nextConforming,
								})
								recursed = true
							}
						}
					}
				}
				if !recursed && len(childShapes) == 0 {
					if node.owner != nil {
						appendTo(node.owner, quantity)
					}
					if node.conforming != nil && node.conforming != node.owner {
						appendTo(node.conforming, quantity)
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

func queryPathID(shapePath []string) string {
	hash := sha256.New()
	for i, shape := range shapePath {
		if i > 0 {
			_, _ = hash.Write([]byte{0})
		}
		_, _ = hash.Write([]byte(shape))
	}
	digest := hash.Sum(nil)
	return hex.EncodeToString(digest[:16])
}

func (indexer *queryIndexer) appendValue(value queryIndexValue) {
	slog.Debug("append query value", "path", value.shapePath, "value", value.term.String(), "doc", (*value.document)["id"])
	path := queryPathID(value.shapePath)
	field := "valueString"
	storedValue := value.term.String()
	var datatype string
	var language string
	if literal, ok := value.term.(*rdf2go.Literal); ok {
		if literal.Datatype != nil {
			datatype = literal.Datatype.RawValue()
		}
		if literal.Language != "" {
			language = literal.Language
		}
		suffix := datatypeMappings[datatype]
		switch suffix {
		case "ds":
			field = "valueNumber"
			storedValue = literal.RawValue()
			// Convert to canonical SI unit when quantity context is available.
			if value.quantity.ConvertsNumericPredicate(value.predicateURI) {
				if num, err := strconv.ParseFloat(literal.RawValue(), 64); err == nil {
					if converted, ok := qudt.Convert(num, value.quantity.UnitURI, value.quantity.QuantityKindURI, value.quantity.IsDelta); ok {
						storedValue = strconv.FormatFloat(converted, 'f', -1, 64)
						slog.Debug("converted quantity value", "original", literal.RawValue(), "unit", value.quantity.UnitURI, "canonical", storedValue)
					}
				}
			}
		case "bs":
			field = "valueBoolean"
			storedValue = literal.RawValue()
		case "srpt":
			field = "valueGeo"
			storedValue = literal.RawValue()
		case "ss":
			storedValue = literal.RawValue()
		case "dts":
			field = "valueDate"
			date := literal.RawValue()
			if len(date) == 10 {
				date += "T00:00:00Z"
			} else if !strings.HasSuffix(date, "Z") && !hasTimezoneOffset(date) {
				date += "Z"
			}
			storedValue = date
		default:
			field = "valueText"
			storedValue = literal.RawValue()
		}
	}
	parentID := fmt.Sprint((*value.document)["id"])
	ownerKey := parentID
	if ownerKey == "<nil>" || ownerKey == "" {
		ownerKey = fmt.Sprintf("%p", value.document)
	}
	parentDocuments := indexer.traversal.valueDocuments[ownerKey]
	if parentDocuments == nil {
		parentDocuments = make(map[string]*document)
		indexer.traversal.valueDocuments[ownerKey] = parentDocuments
	}
	child := parentDocuments[path]
	if child == nil {
		digest := sha256.Sum256([]byte(parentID + "\x00" + path))
		valueDocument := document{
			"id":         parentID + "|value|" + hex.EncodeToString(digest[:16]),
			"docType":    "value",
			"resourceId": (*value.document)["resourceId"],
			"path":       path,
		}
		child = &valueDocument
		parentDocuments[path] = child
		children, _ := (*value.document)["_childDocuments_"].([]any)
		(*value.document)["_childDocuments_"] = append(children, valueDocument)
	}
	child.appendValue(field, storedValue)
	if datatype != "" {
		child.appendValue("datatype", datatype)
	}
	if language != "" {
		child.appendValue("language", language)
	}
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
	return slices.Contains(values, shape)
}
