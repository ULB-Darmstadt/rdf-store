package shacl

import (
	"log/slog"
)

// DenormalizeQualifiedValueShapes expands qualified value shapes into properties.
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

// DenormalizePropertyNodeShapes expands node shapes for properties.
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
					// shapes[denormalizedQualifiedValueShape.Id.RawValue()] = denormalizedQualifiedValueShape
				} else {
					slog.Warn("property's qualifiedValueShape not found", "id", prop.QualifiedValueShape, "path", prop.Path)
				}
			}
		}
	}
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
