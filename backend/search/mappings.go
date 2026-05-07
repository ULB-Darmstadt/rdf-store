package search

import (
	"fmt"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"

	"github.com/stevenferrer/solr-go"
)

var prefixXSD = "http://www.w3.org/2001/XMLSchema#%s"

var datatypeMappings = map[string]string{
	fmt.Sprintf(prefixXSD, "string"):        "t",
	fmt.Sprintf(prefixXSD, "integer"):       "ds",
	fmt.Sprintf(prefixXSD, "int"):           "ds",
	fmt.Sprintf(prefixXSD, "short"):         "ds",
	fmt.Sprintf(prefixXSD, "byte"):          "ds",
	fmt.Sprintf(prefixXSD, "unsignedInt"):   "ds",
	fmt.Sprintf(prefixXSD, "unsignedShort"): "ds",
	fmt.Sprintf(prefixXSD, "unsignedByte"):  "ds",
	fmt.Sprintf(prefixXSD, "long"):          "ds",
	fmt.Sprintf(prefixXSD, "unsignedLong"):  "ds",
	fmt.Sprintf(prefixXSD, "float"):         "ds",
	fmt.Sprintf(prefixXSD, "double"):        "ds",
	fmt.Sprintf(prefixXSD, "decimal"):       "ds",
	fmt.Sprintf(prefixXSD, "date"):          "dts",
	fmt.Sprintf(prefixXSD, "dateTime"):      "dts",
	fmt.Sprintf(prefixXSD, "boolean"):       "bs",
	base.Configuration.GeoDataType:          "srpt",
}

// fieldType maps SHACL property settings to a Solr field suffix.
// It returns the Solr field type suffix to use for the property.
func fieldType(property *shacl.Property) string {
	if property.HasValue && property.MaxCount == 1 {
		// ignore fixed value properties
		return "t"
	}
	if property.Class ||
		property.In ||
		(property.Facet != nil && *property.Facet) ||
		(shacl.SHACL_IRI.RawValue() == property.NodeKind) ||
		property.QualifiedValueShapeDenormalized != nil && property.QualifiedValueShapeDenormalized.Class {
		// these are supposed to be facets
		return "ss"
	}
	if len(property.Datatype) > 0 {
		if value, ok := datatypeMappings[property.Datatype]; ok {
			// depending on datatype, these are supposed to be facets too
			return value
		}
	}
	if len(property.Or) > 0 {
		// check if sh:or/sh:xone options resolve to a distinct facet
		var uniqueType string
		for option := range property.Or {
			ft := fieldType(option)
			if uniqueType == "" {
				uniqueType = ft
			} else {
				if ft != uniqueType {
					uniqueType = ""
					break
				}
			}
		}
		if uniqueType != "" {
			return uniqueType
		}
	}
	return "t"
}

// createCollectionSchema defines the Solr schema fields for the collection.
// It returns the ordered slice of Solr field definitions.
func createCollectionSchema() (fields []solr.Field) {
	fields = append(fields, solr.Field{Name: "label", Type: "string", Indexed: true, Stored: true, MultiValued: true})
	fields = append(fields, solr.Field{Name: "shape", Type: "string", Indexed: true, Stored: true, MultiValued: true})
	fields = append(fields, solr.Field{Name: "creator", Type: "string", Indexed: false, Stored: true})
	fields = append(fields, solr.Field{Name: "lastModified", Type: "pdate", Indexed: false, Stored: false})
	return fields
}
