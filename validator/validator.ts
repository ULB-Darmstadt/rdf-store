import { Store, DataFactory, NamedNode, Quad, StreamParser } from 'n3'
import { RdfXmlParser } from 'rdfxml-streaming-parser'
import jsonld from 'jsonld'
import { Validator } from 'shacl-engine'

const proxy = process.env.PROXY
const loadOwlImports = process.env.IGNORE_OWL_IMPORTS !== 'false'
const owlPredicateImports = DataFactory.namedNode('http://www.w3.org/2002/07/owl#imports')
const shapesGraphName = DataFactory.namedNode('shapes')
const dataGraphName = DataFactory.namedNode('data')

export async function validate(shapesGraph: string, shapeID: string, dataGraph: string, dataID: string) {
    const dataset = new Store()
    const loadedUrls: string[] = []
    const prefixes: Record<string, string> = {}

    await importRDF(shapesGraph, dataset, loadedUrls, prefixes, shapesGraphName)
    await importRDF(dataGraph, dataset, loadedUrls, prefixes, dataGraphName)

    const validator = new Validator(dataset, { factory: DataFactory })
    const report = await validator.validate({ dataset: dataset, terms: [ DataFactory.namedNode(dataID) ] }, [{ terms: [ DataFactory.namedNode(shapeID) ] }])
    return [report.conforms, report.results]
}

async function importRDF(input: string | Promise<string>, store: Store, loadedUrls: string[], prefixes: Record<string, string>, graph?: NamedNode) {
    const parse = async (input: string) => {
        const dependencies: Promise<void>[] = []
        await new Promise((resolve, reject) => {
            const parser = guessContentType(input) === 'xml' ? new RdfXmlParser() : new StreamParser()
            parser.on('data', (quad: Quad) => {
                store.add(new Quad(quad.subject, quad.predicate, quad.object, graph))
                // check if this is an owl:imports predicate and try to load the url
                if (loadOwlImports && owlPredicateImports.equals(quad.predicate)) {
                    const url = toURL(quad.object.value, prefixes)
                    // import url only once
                    if (url && loadedUrls.indexOf(url) < 0) {
                        loadedUrls.push(url)
                        // import into separate graph
                        dependencies.push(importRDF(fetchRDF(url), store, loadedUrls, prefixes, DataFactory.namedNode(url)))
                    }
                }
            })
            .on('error', (error) => {
                console.warn('failed parsing RDF', graph?.id, ', reason:' ,error.message)
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
            parser.write(input)
            parser.end()
        })
        try {
            await Promise.allSettled(dependencies)
        } catch (e) {
            console.warn(e)
        }
    }

    if (input instanceof Promise) {
        input = await input
    }
    if (input) {
        if (guessContentType(input) === 'json') {
            // convert json to n-quads
            try {
                input = await jsonld.toRDF(JSON.parse(input), { format: 'application/n-quads' }) as string
            } catch(e) {
                console.error(e)
            }
        }
        await parse(input)
    }
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

async function fetchRDF(url: string): Promise<string> {
    let proxiedURL = url
    // if we have a proxy configured, then load url via proxy
    if (proxy) {
        proxiedURL = proxy + encodeURIComponent(url)
    }
    const promise = fetch(proxiedURL, {
        headers: {
            'Accept': 'text/turtle, application/trig, application/n-triples, application/n-quads, text/n3, application/ld+json'
        },
    }).then(resp => resp.text())
    return promise
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
