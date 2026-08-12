import { describe, expect, it } from 'vitest'
import { Parser } from 'n3'
import { collectNodeIds, mergeQuads, serializeNQuads } from './graph-model'

const parse = (value: string) => new Parser({ format: 'N-Quads' }).parse(value)

describe('graph model', () => {
    it('creates lightweight object nodes and deduplicates repeated quads', () => {
        const graph = new Map()
        const quads = parse('<https://example.org/a> <https://example.org/links> <https://example.org/b> <https://example.org/g> .')
        mergeQuads(graph, quads)
        mergeQuads(graph, quads)

        expect(graph.size).toBe(1)
        expect(collectNodeIds(graph.values())).toEqual(new Set(['https://example.org/a', 'https://example.org/b']))
    })

    it('merges cyclic expansions without duplicating links', () => {
        const graph = new Map()
        mergeQuads(graph, parse(`
            <https://example.org/a> <https://example.org/links> <https://example.org/b> <https://example.org/g> .
            <https://example.org/b> <https://example.org/links> <https://example.org/a> <https://example.org/g> .
        `))
        mergeQuads(graph, parse('<https://example.org/a> <https://example.org/links> <https://example.org/b> <https://example.org/g> .'))
        expect(graph.size).toBe(2)
    })

    it('scopes blank nodes by named graph', () => {
        const quads = parse(`
            <https://example.org/a> <https://example.org/part> _:b <https://example.org/g1> .
            <https://example.org/c> <https://example.org/part> _:b <https://example.org/g2> .
        `)
        const ids = collectNodeIds(quads)
        expect(ids).toContain('https://example.org/a')
        expect(ids).toContain('https://example.org/c')
        expect(Array.from(ids).some(id => id.startsWith('_:https://example.org/g1:'))).toBe(true)
        expect(Array.from(ids).some(id => id.startsWith('_:https://example.org/g2:'))).toBe(true)
    })

    it('exports the complete deduplicated graph as N-Quads', () => {
        const graph = new Map()
        const initial = parse('<https://example.org/a> <https://example.org/links> <https://example.org/b> <https://example.org/g1> .')
        mergeQuads(graph, initial)
        mergeQuads(graph, initial)
        mergeQuads(graph, parse('<https://example.org/b> <https://example.org/name> "B" <https://example.org/g2> .'))

        const exported = parse(serializeNQuads(graph.values()))
        expect(exported).toHaveLength(2)
        expect(new Set(exported.map(quad => quad.graph.value))).toEqual(new Set(['https://example.org/g1', 'https://example.org/g2']))
    })
})
