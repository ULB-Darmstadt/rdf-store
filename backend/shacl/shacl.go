package shacl

import (
	"fmt"
	"rdf-store-backend/base"
	"strconv"

	"github.com/deiu/rdf2go"
)

type NodeShape struct {
	Id         rdf2go.Term
	Parents    map[rdf2go.Term]bool
	Properties []*Property
}

func (node *NodeShape) Parse(id rdf2go.Term, graph *rdf2go.Graph) *NodeShape {
	node.Id = id
	node.Parents = make(map[rdf2go.Term]bool)
	// there can be multiple properties with same path (e.g. for qualified value shapes), so deduplicate here
	propertyMap := make(map[string]*Property) // path -> property
	for triple := range graph.IterTriples() {
		if triple.Subject.Equal(id) {
			if triple.Predicate.Equal(SHACL_NODE) {
				node.Parents[triple.Object] = true
			} else if triple.Predicate.Equal(SHACL_AND) {
				for _, parent := range parseList(triple.Object, graph) {
					node.Parents[parent] = true
				}
			} else if triple.Predicate.Equal(SHACL_PROPERTY) {
				property := new(Property).Parse(triple.Object, node, graph)
				// ignore properties without path
				if property.Path != nil {
					// merge if property already exists
					if existingProperty, ok := propertyMap[property.Path.RawValue()]; ok {
						existingProperty.Merge(property)
					} else {
						propertyMap[property.Path.RawValue()] = property
					}
				}
			}
		}
	}
	for _, prop := range propertyMap {
		node.Properties = append(node.Properties, prop)
	}
	return node
}

type Property struct {
	Id         rdf2go.Term
	FieldName  string
	Path       *rdf2go.Resource
	Datatype   *rdf2go.Resource
	In         bool
	NodeShapes []*rdf2go.Term // this is a slice because properties may override, so that multiple sh:node must be aggregated
	Class      *rdf2go.Resource
	NodeKind   *rdf2go.Resource
	Facet      *bool
	Ignore     bool // indicates that this property overrides a property in the parent chain so that it should be ignored for indexing puposes
}

func (prop *Property) Parse(id rdf2go.Term, parent *NodeShape, graph *rdf2go.Graph) *Property {
	prop.Id = id
	if datatype := graph.One(id, SHACL_DATATYPE, nil); datatype != nil {
		if spec, ok := datatype.Object.(*rdf2go.Resource); ok {
			prop.Datatype = spec
		} else {
			panic(fmt.Errorf("property's sh:datatype is not a named node: %v", datatype.Object))
		}
	}
	if path := graph.One(id, SHACL_PATH, nil); path != nil {
		if spec, ok := path.Object.(*rdf2go.Resource); ok {
			prop.Path = spec
			nodeName := base.CleanStringForSolr(parent.Id.RawValue())
			prop.FieldName = fmt.Sprintf("%s.%s", nodeName, base.CleanStringForSolr(prop.Path.RawValue()))
		}
	}
	if node := graph.One(id, SHACL_NODE, nil); node != nil {
		prop.NodeShapes = append(prop.NodeShapes, &node.Object)
	}
	if class := graph.One(id, SHACL_CLASS, nil); class != nil {
		if spec, ok := class.Object.(*rdf2go.Resource); ok {
			prop.Class = spec
		} else {
			panic(fmt.Errorf("property's sh:class is not a named node: %v", class.Object))
		}
	}
	if in := graph.One(id, SHACL_IN, nil); in != nil {
		prop.In = true
	}
	if nodeKind := graph.One(id, SHACL_NODE_KIND, nil); nodeKind != nil {
		if spec, ok := nodeKind.Object.(*rdf2go.Resource); ok {
			prop.NodeKind = spec
		}
	}
	if facet := graph.One(id, DASH_FACET, nil); facet != nil {
		if spec, ok := facet.Object.(*rdf2go.Literal); ok {
			boolValue, err := strconv.ParseBool(spec.RawValue())
			if err != nil {
				panic(fmt.Errorf("property's dash:facet is not a boolean: %v", spec.RawValue()))
			}

			prop.Facet = &boolValue
		}
	}
	return prop
}

func (prop *Property) Merge(other *Property) {
	if other.Datatype != nil {
		prop.Datatype = other.Datatype
	}
	if other.In {
		prop.In = other.In
	}
	if other.Class != nil {
		prop.Class = other.Class
	}
	if other.NodeKind != nil {
		prop.NodeKind = other.NodeKind
	}
	prop.NodeShapes = append(prop.NodeShapes, other.NodeShapes...)
}

func parseList(head rdf2go.Term, graph *rdf2go.Graph) []rdf2go.Term {
	result := make([]rdf2go.Term, 0)
	first := graph.One(head, RDF_LIST_FIRST, nil)
	rest := graph.One(head, RDF_LIST_REST, nil)
	for first != nil && rest != nil {
		result = append(result, first.Object)
		first = graph.One(rest.Object, RDF_LIST_FIRST, nil)
		rest = graph.One(rest.Object, RDF_LIST_REST, nil)
	}
	return result
}
