import { Quad, Writer } from 'n3'

export function termKey(term: Quad['subject'] | Quad['predicate'] | Quad['object'] | Quad['graph']) {
    const language = term.termType === 'Literal' ? term.language : ''
    const datatype = term.termType === 'Literal' ? term.datatype.value : ''
    return `${term.termType}\u0000${term.value}\u0000${language}\u0000${datatype}`
}

export function quadKey(quad: Quad) {
    return [termKey(quad.subject), termKey(quad.predicate), termKey(quad.object), termKey(quad.graph)].join('\u0001')
}

export function nodeId(term: Quad['subject'] | Quad['object'], graph: string) {
    return term.termType === 'BlankNode' ? `_:${graph}:${term.value}` : term.value
}

export function collectNodeIds(quads: Iterable<Quad>) {
    const ids = new Set<string>()
    for (const quad of quads) {
        ids.add(nodeId(quad.subject, quad.graph.value))
        if (quad.object.termType === 'NamedNode' || quad.object.termType === 'BlankNode') {
            ids.add(nodeId(quad.object, quad.graph.value))
        }
    }
    return ids
}

export function mergeQuads(target: Map<string, Quad>, quads: Quad[]) {
    const previousNodeIds = collectNodeIds(target.values())
    for (const quad of quads) {
        target.set(quadKey(quad), quad)
    }
    const currentNodeIds = collectNodeIds(target.values())
    return new Set(Array.from(currentNodeIds).filter(id => !previousNodeIds.has(id)))
}

export function serializeNQuads(quads: Iterable<Quad>) {
    return new Writer({ format: 'N-Quads' }).quadsToString(Array.from(quads))
}
