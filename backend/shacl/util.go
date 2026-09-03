package shacl

import (
	"fmt"
	"log/slog"

	"github.com/deiu/rdf2go"
)

// DenormalizeQualifiedValueShapes expands qualified value shapes into properties.
func (node *NodeShape) DenormalizeQualifiedValueShapes(shapes map[string]*NodeShape) {
	for _, prop := range node.findPropertiesWithQualifiedValueShape(1) {
		denormalizedShape := new(NodeShape)
		denormalizedShape.Id = prop.Id
		denormalizedShape.Parents = make(map[string]bool)
		denormalizedShape.Alternatives = make(map[string]bool)
		denormalizedShape.Properties = make(map[string][]*Property)
		denormalizedShape.RDF = node.RDF

		if shapeToDenormalizeFrom, ok := shapes[prop.QualifiedValueShape]; ok {
			denormalizeShape(shapeToDenormalizeFrom, denormalizedShape, shapes)
			prop.QualifiedValueShapeDenormalized = denormalizedShape
		} else {
			slog.Warn("property's node shape not found", "id", prop.QualifiedValueShape, "path", prop.Path)
		}
	}
}

// DenormalizePropertyNodeShapes expands node shapes for properties.
func (node *NodeShape) DenormalizePropertyNodeShapes(shapes map[string]*NodeShape) {
	for _, props := range node.Properties {
		for _, prop := range props {
			if len(prop.QualifiedValueShape) > 0 {
				if shapeToDenormalizeFrom, ok := shapes[prop.QualifiedValueShape]; ok {
					denormalizedQualifiedValueShape := new(NodeShape)
					denormalizedQualifiedValueShape.Id = prop.Id
					denormalizedQualifiedValueShape.Parents = make(map[string]bool)
					denormalizedQualifiedValueShape.Alternatives = make(map[string]bool)
					denormalizedQualifiedValueShape.Properties = make(map[string][]*Property)
					denormalizedQualifiedValueShape.RDF = node.RDF
					denormalizedQualifiedValueShape.Class = shapeToDenormalizeFrom.Class
					denormalizeShape(shapeToDenormalizeFrom, denormalizedQualifiedValueShape, shapes)
					prop.QualifiedValueShapeDenormalized = denormalizedQualifiedValueShape
					// shapes[denormalizedQualifiedValueShape.Id.RawValue()] = denormalizedQualifiedValueShape
				} else {
					slog.Warn("property's qualifiedValueShape not found", "id", prop.QualifiedValueShape, "path", prop.Path)
				}
			}
		}
	}
}

// ReferencedNodeShapes returns node-shape terms referenced directly or through
// SHACL composition lists. The result may contain untyped blank-node shapes.
func ReferencedNodeShapes(graph *rdf2go.Graph) []rdf2go.Term {
	seen := make(map[string]bool)
	result := make([]rdf2go.Term, 0)
	add := func(term rdf2go.Term) {
		if term != nil && !seen[term.RawValue()] {
			seen[term.RawValue()] = true
			result = append(result, term)
		}
	}
	for _, predicate := range []rdf2go.Term{SHACL_NODE, SHACL_QUALIFIED_VALUE_SHAPE} {
		for _, triple := range graph.All(nil, predicate, nil) {
			add(triple.Object)
		}
	}
	for _, predicate := range []rdf2go.Term{SHACL_AND, SHACL_OR, SHACL_XONE} {
		for _, triple := range graph.All(nil, predicate, nil) {
			for _, term := range parseList(triple.Object, graph) {
				add(term)
			}
		}
	}
	return result
}

// DetectRdfCollections marks properties whose node shapes follow the recursive
// rdf:first/rdf:rest list pattern. The detection requires the property's sh:or
// alternatives to have already been parsed (i.e. after DenormalizePropertyNodeShapes).
func DetectRdfCollections(shapes map[string]*NodeShape) {
	for _, shape := range shapes {
		for _, props := range shape.Properties {
			for _, prop := range props {
				if isRdfCollectionProperty(prop, shapes) {
					prop.IsRdfCollection = true
				}
			}
		}
	}
}

// isRdfCollectionProperty checks whether a property matches the RDF collection
// heuristic: it references a single node shape containing exactly rdf:first and
// rdf:rest properties, where rdf:rest uses sh:or to allow rdf:nil or a
// recursive reference back to the same shape.
func isRdfCollectionProperty(prop *Property, shapes map[string]*NodeShape) bool {
	if prop.Parent == nil || prop.Parent.Graph == nil || prop.MaxCount != 1 || len(prop.NodeShapes) != 1 || len(prop.Or) != 0 || !isSupportedRdfListNodeKind(prop.NodeKind) ||
		!hasOnlySupportedShapePredicates(prop.Parent.Graph, prop.Id, SHACL_PATH, SHACL_MIN_COUNT, SHACL_MAX_COUNT, SHACL_NODE, SHACL_NODE_KIND, SHACL_CLASS) {
		return false
	}
	var listShapeID string
	for shapeID := range prop.NodeShapes {
		listShapeID = shapeID
	}
	listShape, ok := shapes[listShapeID]
	if !ok || listShape.Graph == nil {
		return false
	}
	nodeReferences := prop.Parent.Graph.All(prop.Id, SHACL_NODE, nil)
	if len(nodeReferences) != 1 || nodeReferences[0].Object.RawValue() != listShapeID {
		return false
	}
	firstProps, hasFirst := listShape.Properties[RDF_LIST_FIRST.RawValue()]
	restProps, hasRest := listShape.Properties[RDF_LIST_REST.RawValue()]
	if !hasFirst || !hasRest || len(firstProps) != 1 || len(restProps) != 1 {
		return false
	}
	if len(listShape.Graph.All(listShape.Id, SHACL_PROPERTY, nil)) != 2 || len(listShape.Properties) != 2 || len(listShape.Parents) != 0 || len(listShape.Alternatives) != 0 ||
		!hasOnlySupportedShapePredicates(listShape.Graph, listShape.Id, SHACL_PROPERTY) {
		return false
	}
	first := firstProps[0]
	rest := restProps[0]
	if first.MinCount != 1 || first.MaxCount != 1 || rest.MinCount != 1 || rest.MaxCount != 1 || first.HasValue || len(first.Or) != 0 || len(rest.Or) != 2 ||
		!hasOnlySupportedShapePredicates(listShape.Graph, rest.Id, SHACL_PATH, SHACL_MIN_COUNT, SHACL_MAX_COUNT, SHACL_OR) {
		return false
	}
	var hasNilBranch, hasRecursiveBranch bool
	for branch := range rest.Or {
		if branch.HasValue && branch.HasValueTerm != nil && branch.HasValueTerm.Equal(RDF_LIST_NIL) && len(branch.NodeShapes) == 0 && len(branch.Or) == 0 &&
			hasOnlySupportedShapePredicates(listShape.Graph, branch.Id, SHACL_HAS_VALUE) {
			hasNilBranch = true
		} else if !branch.HasValue && len(branch.NodeShapes) == 1 && len(branch.Or) == 0 &&
			hasOnlySupportedShapePredicates(listShape.Graph, branch.Id, SHACL_NODE) {
			for shapeID := range branch.NodeShapes {
				if shapeID == listShapeID {
					hasRecursiveBranch = true
				}
			}
		}
	}
	return hasNilBranch && hasRecursiveBranch
}

func isSupportedRdfListNodeKind(nodeKind string) bool {
	return nodeKind == "" || nodeKind == fmt.Sprintf(prefixSHACL, "IRI") || nodeKind == fmt.Sprintf(prefixSHACL, "BlankNode") || nodeKind == fmt.Sprintf(prefixSHACL, "BlankNodeOrIRI")
}

func hasOnlySupportedShapePredicates(graph *rdf2go.Graph, subject rdf2go.Term, supported ...rdf2go.Term) bool {
	allowed := make(map[string]bool, len(supported))
	for _, predicate := range supported {
		allowed[predicate.RawValue()] = true
	}
	metadata := map[string]bool{
		fmt.Sprintf(prefixSHACL, "name"):        true,
		fmt.Sprintf(prefixSHACL, "description"): true,
		fmt.Sprintf(prefixSHACL, "order"):       true,
		fmt.Sprintf(prefixSHACL, "group"):       true,
		fmt.Sprintf(prefixSHACL, "message"):     true,
		fmt.Sprintf(prefixSHACL, "severity"):    true,
	}
	shaclPrefix := fmt.Sprintf(prefixSHACL, "")
	for _, triple := range graph.All(subject, nil, nil) {
		predicate := triple.Predicate.RawValue()
		if predicate == RDF_TYPE.RawValue() || allowed[predicate] || metadata[predicate] {
			continue
		}
		if len(predicate) >= len(shaclPrefix) && predicate[:len(shaclPrefix)] == shaclPrefix {
			return false
		}
	}
	return true
}

// WalkRdfCollection follows rdf:first/rdf:rest links from a list head and
// collects each rdf:first value. It returns the collected items or nil when the
// head is nil or rdf:nil.
func WalkRdfCollection(head rdf2go.Term, graph *rdf2go.Graph) []rdf2go.Term {
	if head == nil {
		return nil
	}
	if head.Equal(RDF_LIST_NIL) {
		return nil
	}
	var items []rdf2go.Term
	visited := make(map[string]bool)
	current := head
	for current != nil && !current.Equal(RDF_LIST_NIL) {
		key := current.RawValue()
		if visited[key] {
			break
		}
		visited[key] = true
		first := graph.One(current, RDF_LIST_FIRST, nil)
		if first == nil {
			break
		}
		items = append(items, first.Object)
		rest := graph.One(current, RDF_LIST_REST, nil)
		if rest == nil {
			break
		}
		current = rest.Object
	}
	return items
}

// denormalizeShape flattens parent properties into the target shape.
func denormalizeShape(current *NodeShape, target *NodeShape, shapes map[string]*NodeShape) {
	// target.Parents[current.Id.RawValue()] = true
	for _, props := range current.Properties {
		for _, prop := range props {
			target.AddProperty(prop)
		}
	}
	for parent := range current.Parents {
		if parentShape, ok := shapes[parent]; ok {
			denormalizeShape(parentShape, target, shapes)
		} else {
			slog.Warn("parent shape not found", "id", parent)
		}
	}
}
