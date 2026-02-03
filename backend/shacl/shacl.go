package shacl

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/deiu/rdf2go"
)

type NodeShape struct {
	Id         rdf2go.Term
	Parents    map[string]bool
	Properties map[string][]*Property // path -> property
	RDF        *[]byte
	Graph      *rdf2go.Graph
}

// Parse loads a NodeShape from RDF data into the NodeShape struct.
// It returns the populated NodeShape and any error encountered.
func (node *NodeShape) Parse(id rdf2go.Term, rdf *[]byte) (*NodeShape, error) {
	node.Id = id
	node.Parents = make(map[string]bool)
	node.Properties = make(map[string][]*Property)
	node.RDF = rdf
	if node.Graph == nil {
		if rdf == nil {
			return nil, fmt.Errorf("missing rdf parameter")
		}
		node.Graph = rdf2go.NewGraph("")
		if err := node.Graph.Parse(bytes.NewReader(*rdf), "text/turtle"); err != nil {
			return nil, err
		}
	}
	for _, triple := range node.Graph.All(id, nil, nil) {
		if triple.Predicate.Equal(SHACL_NODE) {
			node.Parents[triple.Object.RawValue()] = true
		} else if triple.Predicate.Equal(SHACL_AND) {
			for _, parent := range parseList(triple.Object, node.Graph) {
				node.Parents[parent.RawValue()] = true
			}
		} else if triple.Predicate.Equal(SHACL_PROPERTY) {
			property, err := new(Property).Parse(triple.Object, node, node.Graph)
			if err != nil {
				return nil, err
			}
			node.AddProperty(property)
		}
	}
	return node, nil
}

// AddProperty registers a property, merging where appropriate.
func (node *NodeShape) AddProperty(property *Property) {
	if len(property.Path) > 0 {
		// there can be multiple properties with same path. we merge them into one property (except qualifiedValueShapes)
		merged := false
		if len(property.QualifiedValueShape) == 0 {
			existingProperty := node.findPropertyWithoutQualifiedValueShape(property.Path)
			if existingProperty != nil {
				merged = true
				existingProperty.Merge(property)
			}
		}
		if !merged {
			node.Properties[property.Path] = append(node.Properties[property.Path], property)
		}
	}
}

// ParentList returns the IDs of parent node shapes.
// It returns the collected parent IDs as a slice.
func (node *NodeShape) ParentList() (list []string) {
	// list = append(list, node.Id.RawValue())
	for parent := range node.Parents {
		list = append(list, parent)
	}
	return
}

// findPropertyWithoutQualifiedValueShape finds a property without a qualified shape.
// It returns the matching property or nil if none exists.
func (node *NodeShape) findPropertyWithoutQualifiedValueShape(path string) *Property {
	if props, ok := node.Properties[path]; ok {
		for _, prop := range props {
			if len(prop.QualifiedValueShape) == 0 {
				return prop
			}
		}
	}
	return nil
}

// findPropertiesWithQualifiedValueShape filters properties by qualifiedValueShape.
// It returns the slice of matching properties.
func (node *NodeShape) findPropertiesWithQualifiedValueShape(qualifiedMinCount int) []*Property {
	result := make([]*Property, 0)
	for _, props := range node.Properties {
		for _, prop := range props {
			if len(prop.QualifiedValueShape) > 0 && prop.QualifiedMinCount >= qualifiedMinCount {
				result = append(result, prop)
			}
		}
	}
	return result
}

// Print logs a human-readable representation of the node shape.
func (node *NodeShape) Print() {
	fmt.Println("Id: " + node.Id.RawValue())
	fmt.Println("Parents:")
	for p := range node.Parents {
		fmt.Println("\t" + p)
	}
	fmt.Println("Properties:")
	for _, ps := range node.Properties {
		for _, p := range ps {
			p.Print()
			fmt.Println("------------------------")
		}
	}
}

type Property struct {
	Id                              rdf2go.Term
	Parent                          *NodeShape
	Path                            string
	Datatype                        string
	In                              bool
	Class                           bool
	HasValue                        bool
	QualifiedValueShape             string
	QualifiedValueShapeDenormalized *NodeShape
	NodeShapesDenormalized          *NodeShape
	QualifiedMinCount               int
	MaxCount                        int
	NodeShapes                      map[string]bool
	Or                              map[string]bool
	NodeKind                        string
	Facet                           *bool
}

// Print logs a human-readable representation of the property.
func (prop *Property) Print() {
	fmt.Printf("\tPath: %v\n", prop.Path)
	fmt.Printf("\tDatatype: %v\n", prop.Datatype)
	fmt.Printf("\tIn: %v\n", prop.In)
	fmt.Printf("\tMaxCount: %v\n", prop.MaxCount)
	fmt.Printf("\tQualifiedValueShape: %v\n", prop.QualifiedValueShape)
	fmt.Printf("\tClass: %v\n", prop.Class)
	fmt.Printf("\tHasValue: %v\n", prop.HasValue)
	fmt.Println("\tShapes:")
	for s := range prop.NodeShapes {
		fmt.Println("\t\t" + s)
	}
}

// Parse loads property constraints from a SHACL graph.
// It returns the populated property and any error encountered.
func (prop *Property) Parse(id rdf2go.Term, parent *NodeShape, graph *rdf2go.Graph) (*Property, error) {
	prop.Id = id
	prop.Parent = parent
	prop.NodeShapes = make(map[string]bool)
	prop.Or = make(map[string]bool)

	for _, triple := range graph.All(id, nil, nil) {
		if triple.Predicate.Equal(SHACL_DATATYPE) {
			if spec, ok := triple.Object.(*rdf2go.Resource); ok {
				prop.Datatype = spec.RawValue()
			} else {
				return nil, fmt.Errorf("property's sh:datatype is not a named node: %v", triple.Object)
			}
		} else if triple.Predicate.Equal(SHACL_PATH) {
			if spec, ok := triple.Object.(*rdf2go.Resource); ok {
				prop.Path = spec.RawValue()
			}
		} else if triple.Predicate.Equal(SHACL_NODE) {
			prop.NodeShapes[triple.Object.RawValue()] = true
		} else if triple.Predicate.Equal(SHACL_AND) {
			for _, shape := range parseList(triple.Object, graph) {
				prop.NodeShapes[shape.RawValue()] = true
			}
		} else if triple.Predicate.Equal(SHACL_OR) {
			for _, shape := range parseList(triple.Object, graph) {
				prop.NodeShapes[shape.RawValue()] = true
			}
		} else if triple.Predicate.Equal(SHACL_QUALIFIED_VALUE_SHAPE) {
			// prop.NodeShapes[triple.Object.RawValue()] = true
			prop.QualifiedValueShape = triple.Object.RawValue()
		} else if triple.Predicate.Equal(SHACL_QUALIFIED_MIN_COUNT) {
			if i, err := strconv.Atoi(triple.Object.RawValue()); err == nil {
				prop.QualifiedMinCount = i
			}
		} else if triple.Predicate.Equal(SHACL_MAX_COUNT) {
			if i, err := strconv.Atoi(triple.Object.RawValue()); err == nil {
				prop.MaxCount = i
			}
		} else if triple.Predicate.Equal(SHACL_CLASS) {
			prop.Class = true
		} else if triple.Predicate.Equal(SHACL_IN) {
			prop.In = true
		} else if triple.Predicate.Equal(SHACL_HAS_VALUE) {
			prop.HasValue = true
		} else if triple.Predicate.Equal(SHACL_NODE_KIND) {
			if spec, ok := triple.Object.(*rdf2go.Resource); ok {
				prop.NodeKind = spec.RawValue()
			}
		} else if triple.Predicate.Equal(DASH_FACET) {
			boolValue, err := strconv.ParseBool(triple.Object.RawValue())
			if err != nil {
				return nil, fmt.Errorf("property's dash:facet is not a boolean: %v", triple.Object.RawValue())
			}
			prop.Facet = &boolValue
		} else if triple.Predicate.Equal(SHACL_OR) || triple.Predicate.Equal(SHACL_XONE) {
			for _, option := range parseList(triple.Object, graph) {
				prop.Or[option.RawValue()] = true
			}
		}
	}
	return prop, nil
}

// Merge combines constraint settings from another property definition.
func (prop *Property) Merge(other *Property) {
	prop.In = prop.In || other.In
	prop.Class = prop.Class || other.Class
	prop.HasValue = prop.HasValue || other.HasValue
	prop.QualifiedMinCount = max(prop.QualifiedMinCount, other.QualifiedMinCount)
	if other.MaxCount > 0 && prop.MaxCount > 0 {
		prop.MaxCount = min(prop.MaxCount, other.MaxCount)
	} else {
		prop.MaxCount = max(prop.MaxCount, other.MaxCount)
	}
	if len(other.NodeKind) > 0 {
		prop.NodeKind = other.NodeKind
	}
	if len(other.Datatype) > 0 {
		prop.Datatype = other.Datatype
	}
	if other.Facet != nil {
		prop.Facet = other.Facet
	}
	for k := range other.NodeShapes {
		prop.NodeShapes[k] = true
	}
}

// parseList traverses an RDF list into a slice of terms.
// It returns the ordered list of RDF terms.
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
