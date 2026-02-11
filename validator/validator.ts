import { Store, DataFactory, NamedNode, Quad, StreamParser, Literal } from 'n3'
import { RdfXmlParser } from 'rdfxml-streaming-parser'
import jsonld from 'jsonld'
// @ts-expect-error shacl-engine has no type definitions
import { Validator } from 'shacl-engine'
import type { Term } from '@rdfjs/types'

const proxy = process.env.PROXY
const loadOwlImports = process.env.IGNORE_OWL_IMPORTS !== 'false'
const owlPredicateImports = DataFactory.namedNode('http://www.w3.org/2002/07/owl#imports')
export const shapesGraphName = DataFactory.namedNode('shapes')
export const dataGraphName = DataFactory.namedNode('data')
let cache: Record<string, Promise<Quad[]>> = {}
let prefixes: Record<string, string> = {}
type RdfListIndex = ReturnType<Store['extractLists']>

const prefixSHACL = 'http://www.w3.org/ns/shacl#'
const shaclNode = prefixSHACL + 'node'
const shaclProperty = prefixSHACL + 'property'
const shaclPath = prefixSHACL + 'path'
const shaclAnd = prefixSHACL + 'and'
const shaclOr = prefixSHACL + 'or'
const shaclXone = prefixSHACL + 'xone'
const shaclQualifiedValueShape = prefixSHACL + 'qualifiedValueShape'

export async function validate(shapesGraph: string, rootShaclShapeID: string, dataGraph: string, resourceID: string, clearCache?: string) {
    if (clearCache) {
        cache = {}
        prefixes = {}
    }
    const dataset = new Store()
    const importedUrls: string[] = []

    await importRDF(parseRDF(shapesGraph), shapesGraphName, dataset, importedUrls)
    await importRDF(parseRDF(dataGraph), dataGraphName, dataset, importedUrls)

    const validator = new Validator(dataset, { factory: DataFactory, details: false, debug: false })
    const lists = dataset.extractLists()
    const subjectToShapeConformance: Record<string, string> = {} // RDF subjects conforming to SHACL shape IDs
    await validateShape(DataFactory.namedNode(resourceID), DataFactory.namedNode(rootShaclShapeID), subjectToShapeConformance, dataset, validator, lists)
    return subjectToShapeConformance
}

async function validateShape(resourceID: Term, shapeID: Term, subjectToShapeConformance: Record<string, string>, dataset: Store, validator: Validator, lists: RdfListIndex, visited: Set<string> = new Set()) {
    const visitKey = `${resourceID.termType}:${resourceID.value}|${shapeID.termType}:${shapeID.value}`
    if (visited.has(visitKey) || subjectToShapeConformance[resourceID.value]) {
        return
    }
    visited.add(visitKey)
    const conforms = await registerConformance(resourceID, shapeID, subjectToShapeConformance, dataset, validator)
    if (conforms) {
        // resource validates, so dive into all sh:property's in this node shape's inheritance tree
        const nodeShapes = [shapeID, ...getValueNodeShapes(shapeID, true, dataset, lists)]
        for (const shape of nodeShapes) {
            const properties = dataset.getObjects(shape, shaclProperty, shapesGraphName)
            for (const property of properties) {
                const paths = dataset.getObjects(property, shaclPath, shapesGraphName)
                const propertyShapes = getValueNodeShapes(property, false, dataset, lists)
                if (paths.length === 0 || propertyShapes.length === 0) {
                    continue
                }
                for (const path of paths) {
                    if (path.termType !== 'NamedNode') {
                        continue
                    }
                    const values = dataset.getObjects(resourceID, path, dataGraphName)
                    for (const value of values) {
                        if (value.termType === 'Literal') {
                            continue
                        }
                        for (const propertyShape of propertyShapes) {
                            await validateShape(value, propertyShape, subjectToShapeConformance, dataset, validator, lists, visited)
                        }
                    }
                }
            }
        }
    }
    return conforms
}

async function registerConformance(resourceID: Term, shapeID: Term, subjectToShapeConformance: Record<string, string>, dataset: Store, validator: Validator) {
    const report = await validator.validate({ dataset: dataset, terms: [ resourceID ] }, [{ terms: [ shapeID ] }])
    if (report.conforms) {
        subjectToShapeConformance[resourceID.value] = shapeID.value
        return true
    }
    return false
}

async function importRDF(rdf: Promise<Quad[]>, graph: NamedNode, store: Store, importedUrls: string[]) {
    const quads = await rdf
    const dependencies: Promise<void>[] = []
    for (const quad of quads) {
        store.add(new Quad(quad.subject, quad.predicate, quad.object, graph))
        // check if this is an owl:imports predicate and try to load the url
        if (owlPredicateImports.equals(quad.predicate) && loadOwlImports) {
            const url = toURL(quad.object.value, prefixes)
            // import url only once
            if (url && importedUrls.indexOf(url) < 0) {
                importedUrls.push(url)
                dependencies.push(importRDF(fetchRDF(url), graph, store, importedUrls))
            }
        }
    }
    await Promise.allSettled(dependencies)
}

async function fetchRDF(url: string): Promise<Quad[]> {
    // try to load from cache first
    if (url in cache) {
        return cache[url]
    }
    cache[url] = new Promise<Quad[]>(async (resolve, reject) => {
        try {
            let proxiedURL = url
            // if we have a proxy configured, then load url via proxy
            if (proxy) {
                proxiedURL = proxy + encodeURIComponent(url)
            }
            const response = await fetch(proxiedURL, {
                headers: {
                    'Accept': 'text/turtle, application/trig, application/n-triples, application/n-quads, text/n3, application/ld+json'
                },
            }).then(resp => resp.text())
            resolve(await parseRDF(response))
        } catch(e) {
            reject(e)
        }
    })
    return cache[url]
}

async function parseRDF(rdf: string): Promise<Quad[]> {
    if (guessContentType(rdf) === 'json') {
        // convert json to n-quads
        try {
            rdf = await jsonld.toRDF(JSON.parse(rdf), { format: 'application/n-quads' }) as string
        } catch(e) {
            console.error(e)
        }
    }
    const quads: Quad[] = []
    await new Promise((resolve, reject) => {
        const parser = guessContentType(rdf) === 'xml' ? new RdfXmlParser() : new StreamParser()
        parser.on('data', (quad: Quad) => {
            quads.push(quad)
        })
        .on('error', (error) => {
            reject(error)
        })
        .on('prefix', (prefix, iri) => {
            // ignore empty (default) namespace
            if (prefix) {
                prefixes[prefix] = iri
            }
        })
        .on('end', () => {
            resolve(null)
        })
        parser.write(rdf)
        parser.end()
    })
    return quads
}

function isURL(input: string): boolean {
    let url: URL
    try {
        url = new URL(input)
    } catch (_) {
        return false
    }
    return url.protocol === 'http:' || url.protocol === 'https:'
}

function toURL(id: string, prefixes: Record<string, string>): string | null {
    if (isURL(id)) {
        return id
    }
    const splitted = id.split(':')
    if (splitted.length === 2) {
        const prefix = prefixes[splitted[0]]
        if (prefix) {
            // need to ignore type check. 'prefix' is a string and not a NamedNode<string> (seems to be a bug in n3 typings)
            // @ts-ignore
            id = id.replace(`${splitted[0]}:`, prefix)
            if (isURL(id)) {
                return id
            }
        }
    }
    return null
}

/* Can't rely on HTTP content-type header, since many resources are delivered with text/plain */
function guessContentType(input: string) {
    if (/^\s*\{/.test(input)) {
        return 'json'
    } else if (/^\s*<\?xml/.test(input)) {
        return 'xml'
    } 
    return 'ttl'
}

function getValueNodeShapes(subject: Term, withIneritance: boolean, dataset: Store, lists: RdfListIndex, visited: Set<string> = new Set()) {
    const visitKey = `${subject.termType}:${subject.value}`
    if (visited.has(visitKey)) {
        return []
    }
    visited.add(visitKey)
    const extendedShapes: Term[] = []
    const shapesToVisit: Term[] = []

    // sh:qualifiedValueShape
    shapesToVisit.push(...dataset.getObjects(subject, shaclQualifiedValueShape, shapesGraphName))
    // sh:node
    shapesToVisit.push(...dataset.getObjects(subject, shaclNode, shapesGraphName))
    // sh:and
    const andLists = dataset.getQuads(subject, shaclAnd, null, shapesGraphName)
    // sh:or
    const orLists = dataset.getQuads(subject, shaclOr, null, shapesGraphName)
    // sh:xone
    const xoneLists = dataset.getQuads(subject, shaclXone, null, shapesGraphName)
    if (andLists.length > 0) {
        for (const andList of andLists) {
            const terms = lists[andList.object.value] ?? []
            for (const term of terms) {
                shapesToVisit.push(...getValueNodeShapes(term, withIneritance, dataset, lists, visited))
            }
        }
    }
    if (orLists.length > 0) {
        for (const orList of orLists) {
            const terms = lists[orList.object.value] ?? []
            for (const term of terms) {
                shapesToVisit.push(...getValueNodeShapes(term, withIneritance, dataset, lists, visited))
            }
        }
    }
    if (xoneLists.length > 0) {
        for (const xoneList of xoneLists) {
            const terms = lists[xoneList.object.value] ?? []
            for (const term of terms) {
                shapesToVisit.push(...getValueNodeShapes(term, withIneritance, dataset, lists, visited))
            }
        }
    }
    for (const shape of shapesToVisit) {
        extendedShapes.push(shape)
        if (withIneritance) {
            // recurse up
            extendedShapes.push(...getValueNodeShapes(shape, withIneritance, dataset, lists, visited))
        }
    }
    return extendedShapes
}
