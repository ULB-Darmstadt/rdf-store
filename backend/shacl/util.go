package shacl

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/deiu/rdf2go"
)

func FindResourceProfile(graph *rdf2go.Graph, id *rdf2go.Term, profiles map[string]*NodeShape) (resourceID rdf2go.Term, profile *NodeShape, err error) {
	var refs []*rdf2go.Triple
	if id == nil {
		refs = graph.All(nil, DCTERMS_CONFORMS_TO, nil)
		refs = append(refs, graph.All(nil, RDF_TYPE, nil)...)
	} else {
		refs = graph.All(*id, DCTERMS_CONFORMS_TO, nil)
		refs = append(refs, graph.All(*id, RDF_TYPE, nil)...)
	}
	for _, triple := range refs {
		if profileRef, ok := profiles[triple.Object.RawValue()]; ok {
			if resourceID != nil {
				return nil, nil, errors.New("graph has multiple relations " + DCTERMS_CONFORMS_TO.String() + " or " + RDF_TYPE.String() + " to a known SHACL profile")
			}
			resourceID = triple.Subject
			profile = profileRef
		}
	}
	if resourceID == nil {
		return nil, nil, errors.New("graph has no relation " + DCTERMS_CONFORMS_TO.String() + " or " + RDF_TYPE.String() + " to a known SHACL profile")
	}
	return
}

func (node *NodeShape) DenormalizeQualifiedValueShapes(shapes map[string]*NodeShape) {
	for _, prop := range node.findPropertiesWithQualifiedValueShape(1) {
		denormalizedShape := new(NodeShape)
		denormalizedShape.Id = prop.Id
		denormalizedShape.Parents = make(map[string]bool)
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

func (node *NodeShape) DenormalizePropertyNodeShapes(shapes map[string]*NodeShape) {
	for _, props := range node.Properties {
		for _, prop := range props {
			if len(prop.QualifiedValueShape) > 0 {
				if shapeToDenormalizeFrom, ok := shapes[prop.QualifiedValueShape]; ok {
					denormalizedQualifiedValueShape := new(NodeShape)
					denormalizedQualifiedValueShape.Id = prop.Id
					denormalizedQualifiedValueShape.Parents = make(map[string]bool)
					denormalizedQualifiedValueShape.Properties = make(map[string][]*Property)
					denormalizedQualifiedValueShape.RDF = node.RDF
					denormalizeShape(shapeToDenormalizeFrom, denormalizedQualifiedValueShape, shapes)
					prop.QualifiedValueShapeDenormalized = denormalizedQualifiedValueShape
					if prop.QualifiedValueShape == "https://w3id.org/nfdi4ing/profiles/cbff1db0-8764-4617-b221-0752ca640258/" {
						fmt.Println("--- denormalized temp ----------------")
						denormalizedQualifiedValueShape.Print()
					}
					// shapes[denormalizedQualifiedValueShape.Id.RawValue()] = denormalizedQualifiedValueShape
				} else {
					slog.Warn("property's qualifiedValueShape not found", "id", prop.QualifiedValueShape, "path", prop.Path)
				}
			}
		}
	}
}

func denormalizeShape(current *NodeShape, target *NodeShape, shapes map[string]*NodeShape) {
	target.Parents[current.Id.RawValue()] = true
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
