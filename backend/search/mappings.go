package search

import (
	"fmt"
	"rdf-store-backend/base"
	"rdf-store-backend/shacl"
	"rdf-store-backend/sparql"

	"github.com/stevenferrer/solr-go"
)

var prefixXSD = "http://www.w3.org/2001/XMLSchema#%s"

var datatypeMappings = map[string]string{
	fmt.Sprintf(prefixXSD, "string"):        "text_general",
	fmt.Sprintf(prefixXSD, "integer"):       "pint",
	fmt.Sprintf(prefixXSD, "int"):           "pint",
	fmt.Sprintf(prefixXSD, "short"):         "pint",
	fmt.Sprintf(prefixXSD, "long"):          "plong",
	fmt.Sprintf(prefixXSD, "byte"):          "pint",
	fmt.Sprintf(prefixXSD, "unsignedLong"):  "plong",
	fmt.Sprintf(prefixXSD, "unsignedInt"):   "pint",
	fmt.Sprintf(prefixXSD, "unsignedShort"): "pint",
	fmt.Sprintf(prefixXSD, "unsignedByte"):  "pint",
	fmt.Sprintf(prefixXSD, "float"):         "pfloat",
	fmt.Sprintf(prefixXSD, "double"):        "pdouble",
	fmt.Sprintf(prefixXSD, "decimal"):       "pdouble",
	fmt.Sprintf(prefixXSD, "date"):          "pdate",
	fmt.Sprintf(prefixXSD, "dateTime"):      "pdate",
	fmt.Sprintf(prefixXSD, "boolean"):       "pboolean",
	base.Configuration.GeoDataType:          "location_rpt",
}

func createCollectionSchema() (fields []solr.Field, copyFields []solr.CopyField) {
	fields = append(fields, solr.Field{Name: "_rdf", Type: "string", Indexed: false, Stored: true})
	fields = append(fields, solr.Field{Name: "_shape", Type: "string", Indexed: true, Stored: false, MultiValued: true})
	fields = append(fields, solr.Field{Name: "_creator", Type: "string", Indexed: false, Stored: false})
	fields = append(fields, solr.Field{Name: "_lastModified", Type: "pdate", Indexed: false, Stored: false})

	fieldMap := make(map[string]solr.Field)
	for _, profile := range sparql.Profiles {
		for _, property := range profile.Properties {
			if !property.Ignore {
				if property.Facet == nil || *property.Facet {
					if property.Class != nil || property.In || (property.NodeKind != nil && shacl.SHACL_IRI.Equal(property.NodeKind)) || (property.Facet != nil && *property.Facet) {
						// these are supposed to be facets
						fieldMap[property.FieldName] = solr.Field{Name: property.FieldName, Type: "string", MultiValued: true}
					} else {
						var datatype string
						if property.Datatype != nil {
							if value, ok := datatypeMappings[property.Datatype.RawValue()]; ok {
								datatype = value
							}
						}
						if datatype == "" {
							// fall back to text if no specific datatype is known
							datatype = "text_general"
						}
						fieldMap[property.FieldName] = solr.Field{Name: property.FieldName, Type: datatype, MultiValued: true}
					}
				}
			}
		}
	}

	for _, field := range fieldMap {
		fields = append(fields, field)
	}
	copyFields = []solr.CopyField{{Source: "*", Dest: "_text_"}}
	return fields, copyFields
}
