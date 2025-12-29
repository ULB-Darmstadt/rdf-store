import { DataFactory } from "n3"

export const PREFIX_SHACL = 'http://www.w3.org/ns/shacl#'
export const PREFIX_RDF = 'http://www.w3.org/1999/02/22-rdf-syntax-ns#'
export const PREFIX_RDFS = 'http://www.w3.org/2000/01/rdf-schema#'
export const PREFIX_SKOS = 'http://www.w3.org/2004/02/skos/core#'
export const PREFIX_DCTERMS = 'http://purl.org/dc/terms/'
export const PREFIX_FOAF = 'http://xmlns.com/foaf/0.1/'

export const APP_PATH = import.meta.env.BASE_URL ?? '/'
export const BACKEND_URL = `${process.env.NODE_ENV === 'production' ? APP_PATH : 'http://localhost:3000/'}api/v1`

export const RDF_TYPE = DataFactory.namedNode(PREFIX_RDF+'type')
export const SHACL_NODE_SHAPE = DataFactory.namedNode(PREFIX_SHACL+'NodeShape')