import { Store, DataFactory, NamedNode, Quad, StreamParser } from 'n3'
import { RdfXmlParser } from 'rdfxml-streaming-parser'
import jsonld from 'jsonld'
// @ts-ignore
import { Validator } from 'shacl-engine'

const proxy = process.env.PROXY
const loadOwlImports = process.env.IGNORE_OWL_IMPORTS !== 'false'
const owlPredicateImports = DataFactory.namedNode('http://www.w3.org/2002/07/owl#imports')
const shapesGraphName = DataFactory.namedNode('shapes')
const dataGraphName = DataFactory.namedNode('data')
let cache: Record<string, Promise<Quad[]>> = {}
let prefixes: Record<string, string> = {}

export async function validate(shapesGraph: string, shapeID: string, dataGraph: string, dataID: string, clearCache?: string) {
    if (clearCache) {
        cache = {}
        prefixes = {}
    }
    const dataset = new Store()
    const importedUrls: string[] = []

    await importRDF(parseRDF(shapesGraph, shapesGraphName), dataset, importedUrls)
    await importRDF(parseRDF(dataGraph, dataGraphName), dataset, importedUrls)

    const validator = new Validator(dataset, { factory: DataFactory })
    const report = await validator.validate({ dataset: dataset, terms: [ DataFactory.namedNode(dataID) ] }, [{ terms: [ DataFactory.namedNode(shapeID) ] }])
    return report.conforms
}

async function importRDF(rdf: Promise<Quad[]>, store: Store, importedUrls: string[]) {
    const quads = await rdf
    const dependencies: Promise<void>[] = []
    for (const quad of quads) {
        store.add(new Quad(quad.subject, quad.predicate, quad.object, quad.graph))
        // check if this is an owl:imports predicate and try to load the url
        if (owlPredicateImports.equals(quad.predicate) && loadOwlImports) {
            const url = toURL(quad.object.value, prefixes)
            // import url only once
            if (url && importedUrls.indexOf(url) < 0) {
                importedUrls.push(url)
                dependencies.push(importRDF(fetchRDF(url), store, importedUrls))
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
            resolve(await parseRDF(response, DataFactory.namedNode(url)))
        } catch(e) {
            reject(e)
        }
    })
    return cache[url]
}

async function parseRDF(rdf: string, graph: NamedNode): Promise<Quad[]> {
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
            quads.push(new Quad(quad.subject, quad.predicate, quad.object, graph))
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
