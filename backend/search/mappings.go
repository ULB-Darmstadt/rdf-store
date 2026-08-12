package search

import (
	"fmt"
	"rdf-store-backend/base"

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

// createCollectionSchema defines the Solr schema fields for the collection.
// It returns the ordered slice of Solr field definitions.
func createCollectionSchema() (fields []solr.Field) {
	fields = append(fields, solr.Field{Name: "resourceId", Type: "string", Indexed: true, Stored: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "subject", Type: "string", Indexed: true, Stored: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "docType", Type: "string", Indexed: true, Stored: true, DocValues: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "label", Type: "string", Indexed: true, Stored: true, MultiValued: true})
	fields = append(fields, solr.Field{Name: "shape", Type: "string", Indexed: true, Stored: true, MultiValued: true})
	fields = append(fields, solr.Field{Name: "creator", Type: "string", Indexed: true, Stored: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "lastModified", Type: "pdate", Indexed: true, Stored: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "path", Type: "string", Indexed: true, Stored: false, DocValues: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "valueString", Type: "string", Indexed: true, Stored: false, DocValues: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "valueText", Type: "text_general", Indexed: true, Stored: false, MultiValued: false})
	fields = append(fields, solr.Field{Name: "valueNumber", Type: "pdouble", Indexed: true, Stored: false, DocValues: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "valueDate", Type: "pdate", Indexed: true, Stored: false, DocValues: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "valueBoolean", Type: "boolean", Indexed: true, Stored: false, DocValues: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "valueGeo", Type: "location_rpt", Indexed: true, Stored: false, MultiValued: false})
	fields = append(fields, solr.Field{Name: "datatype", Type: "string", Indexed: true, Stored: false, DocValues: true, MultiValued: false})
	fields = append(fields, solr.Field{Name: "language", Type: "string", Indexed: true, Stored: false, DocValues: true, MultiValued: false})
	return fields
}
