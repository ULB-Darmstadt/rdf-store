package shacl

import (
	"fmt"

	"github.com/deiu/rdf2go"
)

var prefixRDF = "http://www.w3.org/1999/02/22-rdf-syntax-ns#%s"
var prefixRDFS = "http://www.w3.org/2000/01/rdf-schema#%s"
var prefixSHACL = "http://www.w3.org/ns/shacl#%s"
var prefixSKOS = "http://www.w3.org/2004/02/skos/core#%s"
var prefixOWL = "http://www.w3.org/2002/07/owl#%s"
var prefixDCTerms = "http://purl.org/dc/terms/%s"
var prefixDASH = "http://datashapes.org/dash#%s"
var prefixFOAF = "http://xmlns.com/foaf/0.1/%s"

var RDF_TYPE = rdf2go.NewResource(fmt.Sprintf(prefixRDF, "type"))
var RDFS_LABEL = rdf2go.NewResource(fmt.Sprintf(prefixRDFS, "label"))
var FOAF_NAME = rdf2go.NewResource(fmt.Sprintf(prefixFOAF, "name"))
var DCTERMS_CONFORMS_TO = rdf2go.NewResource(fmt.Sprintf(prefixDCTerms, "conformsTo"))
var DCTERMS_TITLE = rdf2go.NewResource(fmt.Sprintf(prefixDCTerms, "title"))
var OWL_IMPORTS = rdf2go.NewResource(fmt.Sprintf(prefixOWL, "imports"))
var SKOS_PREF_LABEL = rdf2go.NewResource(fmt.Sprintf(prefixSKOS, "prefLabel"))

var SHACL_NODE_SHAPE = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "NodeShape"))
var SHACL_NODE = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "node"))
var SHACL_AND = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "and"))
var SHACL_IN = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "in"))
var SHACL_NAME = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "name"))
var SHACL_PROPERTY = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "property"))
var SHACL_DATATYPE = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "datatype"))
var SHACL_PATH = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "path"))
var SHACL_CLASS = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "class"))
var SHACL_NODE_KIND = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "nodeKind"))
var SHACL_IRI = rdf2go.NewResource(fmt.Sprintf(prefixSHACL, "IRI"))

var DASH_FACET = rdf2go.NewResource(fmt.Sprintf(prefixDASH, "facet"))

var RDF_LIST_FIRST = rdf2go.NewResource(fmt.Sprintf(prefixRDF, "first"))
var RDF_LIST_REST = rdf2go.NewResource(fmt.Sprintf(prefixRDF, "rest"))
