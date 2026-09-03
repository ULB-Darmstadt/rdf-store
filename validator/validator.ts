import { Store, DataFactory, NamedNode, Quad, StreamParser } from 'n3'
import { RdfXmlParser } from 'rdfxml-streaming-parser'
import jsonld from 'jsonld'
import { Validator } from 'shacl-engine'
import type { Term } from '@rdfjs/types'

const proxy = process.env.PROXY
const loadOwlImports = process.env.IGNORE_OWL_IMPORTS !== 'false'
const owlPredicateImports = DataFactory.namedNode('http://www.w3.org/2002/07/owl#imports')
export const shapesGraphName = DataFactory.namedNode('shapes')
export const dataGraphName = DataFactory.namedNode('data')
let cache: Record<string, Promise<Quad[]>> = {}
let prefixes: Record<string, string> = {}

const prefixSHACL = 'http://www.w3.org/ns/shacl#'
const prefixRDF = 'http://www.w3.org/1999/02/22-rdf-syntax-ns#'
const shaclNode = prefixSHACL + 'node'
const shaclProperty = prefixSHACL + 'property'
const shaclPath = prefixSHACL + 'path'
const shaclAnd = prefixSHACL + 'and'
const shaclOr = prefixSHACL + 'or'
const shaclXone = prefixSHACL + 'xone'
const shaclQualifiedValueShape = prefixSHACL + 'qualifiedValueShape'
const shaclHasValue = prefixSHACL + 'hasValue'
const rdfFirst = prefixRDF + 'first'
const rdfRest = prefixRDF + 'rest'
const rdfNil = DataFactory.namedNode(prefixRDF + 'nil')

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
    const subjectToShapeConformance: Record<string, string[]> = {} // RDF subjects conforming to SHACL shape IDs
    await validateShape(DataFactory.namedNode(resourceID), DataFactory.namedNode(rootShaclShapeID), subjectToShapeConformance, dataset, validator)
    return subjectToShapeConformance
}

async function validateShape(resourceID: Term, shapeID: Term, subjectToShapeConformance: Record<string, string[]>, dataset: Store, validator: Validator, visited: Set<string> = new Set()) {
    const visitKey = `${resourceID.termType}:${resourceID.value}|${shapeID.termType}:${shapeID.value}`
    if (visited.has(visitKey)) {
        return
    }
    visited.add(visitKey)
    const accepted = await registerConformance(resourceID, shapeID, subjectToShapeConformance, dataset, validator)
    if (accepted) {
        for (const extendedShape of getValueNodeShapes(shapeID, dataset)) {
            await validateShape(resourceID, extendedShape, subjectToShapeConformance, dataset, validator, visited)
        }

        // The recursive rdf:rest constraint is already validated by
        // shacl-engine. Do not follow it into rdf:nil, but still traverse
        // rdf:first so conformance of node-shaped list members is recorded.
        const rdfListShape = isRdfListShape(shapeID, dataset)
        for (const property of dataset.getObjects(shapeID, shaclProperty, shapesGraphName)) {
            if (rdfListShape && dataset.getObjects(property, shaclPath, shapesGraphName).some(path => path.value === rdfRest)) {
                continue
            }
            const paths = dataset.getObjects(property, shaclPath, shapesGraphName)
            const propertyShapes = getValueNodeShapes(property, dataset)
            if (paths.length === 0 || propertyShapes.length === 0) {
                continue
            }
            for (const path of paths) {
                if (path.termType !== 'NamedNode') {
                    continue
                }
                const values = dataset.getObjects(resourceID, path, dataGraphName)
                for (const value of values) {
                    for (const propertyShape of propertyShapes) {
                        await validateShape(value, propertyShape, subjectToShapeConformance, dataset, validator, visited)
                    }
                }
            }
        }
    }
    return accepted
}

function addConformance(resourceID: Term, shapeID: Term, subjectToShapeConformance: Record<string, string[]>) {
    const shapes = subjectToShapeConformance[resourceID.value] ?? []
    if (!shapes.includes(shapeID.value)) {
        shapes.push(shapeID.value)
    }
    subjectToShapeConformance[resourceID.value] = shapes
    return true
}

async function registerConformance(resourceID: Term, shapeID: Term, subjectToShapeConformance: Record<string, string[]>, dataset: Store, validator: Validator) {
    const report = await validator.validate({ dataset: dataset, terms: [ resourceID ] }, [{ terms: [ shapeID ] }])
    if (report.conforms) {
        return addConformance(resourceID, shapeID, subjectToShapeConformance)
    }
    logValidationFailure(resourceID, shapeID, report.results)
    return false
}

function logValidationFailure(resourceID: Term, shapeID: Term, results: any[] = []) {
    const violations = flattenValidationResults(results).map((result) => formatValidationResult(resourceID, shapeID, result))
    console.warn(`SHACL validation failed for ${resourceID.value} against ${shapeID.value}${violations.length > 0 ? `:\n- ${violations.join('\n- ')}` : ''}`)
}

function flattenValidationResults(results: any[]): any[] {
    const flattened: any[] = []
    for (const result of results) {
        flattened.push(result)
        if (result.results?.length) {
            flattened.push(...flattenValidationResults(result.results))
        }
    }
    return flattened
}

function formatValidationResult(resourceID: Term, shapeID: Term, result: any) {
    const details = [
        `constraint=${result.constraintComponent?.value ?? 'unknown'}`,
        `message=${getValidationMessage(result) || 'unknown violation'}`
    ]
    const path = formatValidationPath(result.path)
    const focusNode = firstTermValue(result.focusNode?.terms)
    const value = firstTermValue(result.value?.terms)
    const sourceShape = firstTermValue(result.shape?.ptr?.terms)
    if (path) {
        details.push(`path=${path}`)
    }
    if (focusNode && focusNode !== resourceID.value) {
        details.push(`focusNode=${focusNode}`)
    }
    if (value) {
        details.push(`value=${value}`)
    }
    if (sourceShape && sourceShape !== shapeID.value) {
        details.push(`sourceShape=${sourceShape}`)
    }
    return details.join(', ')
}

function getValidationMessage(result: any) {
    try {
        const message = formatValidationMessages(result.message)
        if (message) {
            return message
        }
    } catch {
    }
    return formatValidationMessages(result.shape?.message)
}

function formatValidationMessages(messages: Array<{ value?: string }> = []) {
    if (!Array.isArray(messages)) {
        return ''
    }
    return messages.map((message) => message.value).filter(Boolean).join(' | ')
}

function formatValidationPath(path: any[] = []) {
    if (!Array.isArray(path)) {
        return ''
    }
    return path.map((step) => {
        const predicates = step.predicates?.map((predicate: Term) => predicate.value).join('|') ?? ''
        const prefix = step.start === 'object' ? '^' : ''
        if (step.quantifier === 'zeroOrMore') {
            return `${prefix}${predicates}*`
        }
        if (step.quantifier === 'oneOrMore') {
            return `${prefix}${predicates}+`
        }
        if (step.quantifier === 'zeroOrOne') {
            return `${prefix}${predicates}?`
        }
        return `${prefix}${predicates}`
    }).join(' / ')
}

function firstTermValue(terms?: Term[]) {
    return terms?.[0]?.value
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

function getValueNodeShapes(subject: Term, dataset: Store) {
    const shapes: Term[] = [
        ...dataset.getObjects(subject, shaclQualifiedValueShape, shapesGraphName),
        ...dataset.getObjects(subject, shaclNode, shapesGraphName),
    ]
    for (const predicate of [shaclAnd, shaclOr, shaclXone]) {
        for (const list of dataset.getQuads(subject, predicate, null, shapesGraphName)) {
            shapes.push(...readShapeList(list.object, dataset))
        }
    }
    return shapes
}

function isRdfListShape(shapeID: Term, dataset: Store): boolean {
    const properties = dataset.getObjects(shapeID, shaclProperty, shapesGraphName)
    if (properties.length !== 2) {
        return false
    }
    let firstProp: Term | undefined
    let restProp: Term | undefined
    for (const prop of properties) {
        const paths = dataset.getObjects(prop, shaclPath, shapesGraphName)
        if (paths.length !== 1) {
            return false
        }
        if (paths[0].value === rdfFirst) {
            firstProp = prop
        } else if (paths[0].value === rdfRest) {
            restProp = prop
        }
    }
    if (!firstProp || !restProp) {
        return false
    }
    const restOrLists = dataset.getQuads(restProp, shaclOr, null, shapesGraphName)
    if (restOrLists.length !== 1) {
        return false
    }
    const branches = readShapeList(restOrLists[0].object, dataset)
    if (branches.length !== 2) {
        return false
    }
    let hasNilBranch = false
    let hasRecursiveBranch = false
    for (const branch of branches) {
        const hasValue = dataset.getObjects(branch, shaclHasValue, shapesGraphName)
        const nodeShapes = dataset.getObjects(branch, shaclNode, shapesGraphName)
        if (hasValue.length === 1 && hasValue[0].equals(rdfNil) && nodeShapes.length === 0) {
            hasNilBranch = true
        } else if (hasValue.length === 0 && nodeShapes.length === 1 && nodeShapes[0].equals(shapeID)) {
            hasRecursiveBranch = true
        }
    }
    return hasNilBranch && hasRecursiveBranch
}

function readShapeList(head: Term, dataset: Store): Term[] {
    const items: Term[] = []
    const visited = new Set<string>()
    let current = head
    while (!current.equals(rdfNil)) {
        const key = `${current.termType}:${current.value}`
        if (visited.has(key)) {
            throw new Error(`cyclic SHACL RDF list at ${current.value}`)
        }
        visited.add(key)
        const first = dataset.getObjects(current, rdfFirst, shapesGraphName)
        const rest = dataset.getObjects(current, rdfRest, shapesGraphName)
        if (first.length !== 1 || rest.length !== 1) {
            throw new Error(`malformed SHACL RDF list at ${current.value}`)
        }
        items.push(first[0])
        current = rest[0]
    }
    return items
}
