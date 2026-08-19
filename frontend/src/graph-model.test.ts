import { describe, expect, it } from 'vitest'
import { Parser } from 'n3'
import {
    collectGraphNodeIds, collectNodeIds, countRouteCrossings, edgePath, graphDepths, mergeQuads,
    radialRadii, reserveRequestWave, routeGraphEdges, serializeNQuads, stableGraphSeed, type EdgeRoute,
    type GraphLayoutEdge, type GraphLayoutNode
} from './graph-model'

const parse = (value: string) => new Parser({ format: 'N-Quads' }).parse(value)

describe('graph model', () => {
    it('assigns shortest-path radial depths from the focused node', () => {
        const depths = graphDepths(['root', 'a', 'b', 'c', 'isolated'], [
            { source: 'root', target: 'a' },
            { source: 'a', target: 'b' },
            { source: 'root', target: 'c' }
        ], 'root')
        expect(Object.fromEntries(depths)).toEqual({ root: 0, a: 1, c: 1, b: 2, isolated: 3 })
    })

    it('derives a stable layout seed independently of input order', () => {
        expect(stableGraphSeed('root', ['b', 'a'], ['edge-2', 'edge-1']))
            .toBe(stableGraphSeed('root', ['a', 'b'], ['edge-1', 'edge-2']))
    })

    it('expands populated rings while retaining the minimum layer gap', () => {
        const radii = radialRadii([0, ...Array(30).fill(1), 2])
        expect(radii.get(1)).toBeGreaterThan(170)
        expect(radii.get(2)! - radii.get(1)!).toBeGreaterThanOrEqual(85)
    })

    it('keeps an unobstructed single relationship slightly bent', () => {
        const nodes: GraphLayoutNode[] = [{ id: 'a', x: 0, y: 0 }, { id: 'b', x: 100, y: 0 }]
        const edges: GraphLayoutEdge[] = [{ id: 'ab', source: 'a', target: 'b', label: 'links' }]
        const route = routeGraphEdges(nodes, edges).get('ab')!
        expect(Math.abs(route.bend)).toBeGreaterThan(0)
        expect(Math.abs(route.bend)).toBeLessThan(30)
    })

    it('chooses either bend direction to avoid nodes', () => {
        const edge: GraphLayoutEdge = { id: 'ab', source: 'a', target: 'b' }
        const above = routeGraphEdges([
            { id: 'a', x: 0, y: 0 }, { id: 'b', x: 100, y: 0 }, { id: 'obstacle', x: 50, y: 6 }
        ], [edge]).get('ab')!
        const below = routeGraphEdges([
            { id: 'a', x: 0, y: 0 }, { id: 'b', x: 100, y: 0 }, { id: 'obstacle', x: 50, y: -6 }
        ], [edge]).get('ab')!
        expect(above.bend).toBeLessThan(0)
        expect(below.bend).toBeGreaterThan(0)
    })

    it('assigns distinct lanes to parallel and reciprocal relationships', () => {
        const nodes: GraphLayoutNode[] = [{ id: 'a', x: 0, y: 0 }, { id: 'b', x: 100, y: 0 }]
        const edges: GraphLayoutEdge[] = [
            { id: 'one', source: 'a', target: 'b' },
            { id: 'two', source: 'a', target: 'b' },
            { id: 'reverse', source: 'b', target: 'a' }
        ]
        const bends = edges.map(edge => routeGraphEdges(nodes, edges).get(edge.id)!.bend)
        expect(new Set(bends).size).toBe(3)
    })

    it('creates separate self-loop routes', () => {
        const nodes: GraphLayoutNode[] = [{ id: 'a', x: 20, y: 30 }]
        const edges: GraphLayoutEdge[] = [
            { id: 'one', source: 'a', target: 'a' },
            { id: 'two', source: 'a', target: 'a' },
            { id: 'three', source: 'a', target: 'a' }
        ]
        const routes = routeGraphEdges(nodes, edges)
        expect(routes.get('one')).not.toEqual(routes.get('two'))
        const radii = edges.map(edge => routes.get(edge.id)!.loopRadius!)
        expect(Math.max(...radii)).toBeGreaterThan(Math.min(...radii))
        expect(edgePath(edges[0], routes.get('one')!, new Map(nodes.map(node => [node.id, node])))).toContain(' C')
    })

    it('reverses only the label path to keep text upright', () => {
        const nodes = new Map([['left', { x: 0, y: 0 }], ['right', { x: 100, y: 20 }]])
        const edge: GraphLayoutEdge = { id: 'edge', source: 'right', target: 'left' }
        const route: EdgeRoute = { bend: 24 }
        const visible = edgePath(edge, route, nodes)
        const label = edgePath(edge, route, nodes, true)
        expect(visible).toMatch(/^M100,20 Q/)
        expect(label).toMatch(/^M0,0 Q/)
        expect(visible.match(/Q([^ ]+)/)?.[1]).toBe(label.match(/Q([^ ]+)/)?.[1])
    })

    it('keeps visible and label paths aligned after node movement', () => {
        const nodes = new Map([['source', { x: 0, y: 0 }], ['target', { x: 100, y: 0 }]])
        const edge: GraphLayoutEdge = { id: 'edge', source: 'source', target: 'target' }
        const route: EdgeRoute = { bend: -30 }
        nodes.set('target', { x: -40, y: 80 })
        const visible = edgePath(edge, route, nodes)
        const label = edgePath(edge, route, nodes, true)
        expect(visible).toMatch(/^M0,0 Q/)
        expect(label).toMatch(/^M-40,80 Q/)
        expect(visible.match(/Q([^ ]+)/)?.[1]).toBe(label.match(/Q([^ ]+)/)?.[1])
    })

    it('reduces crossings in a fixed crossing fixture', () => {
        const nodes: GraphLayoutNode[] = [
            { id: 'nw', x: -60, y: -60 }, { id: 'ne', x: 60, y: -60 },
            { id: 'sw', x: -60, y: 60 }, { id: 'se', x: 60, y: 60 }
        ]
        const edges: GraphLayoutEdge[] = [
            { id: 'descending', source: 'nw', target: 'se', label: 'author' },
            { id: 'ascending', source: 'sw', target: 'ne', label: 'affiliation' }
        ]
        const baseline = new Map(edges.map(edge => [edge.id, { bend: 24 }]))
        const optimized = routeGraphEdges(nodes, edges)
        expect(countRouteCrossings(nodes, edges, optimized)).toBeLessThan(countRouteCrossings(nodes, edges, baseline))
    })

    it('prefers shortest-arc bend direction in a radial tree', () => {
        const nodes: GraphLayoutNode[] = [
            { id: 'root', x: 0, y: 0 },
            { id: 'a', x: 85, y: 0 },
            { id: 'b', x: -85, y: 0 },
            { id: 'a1', x: 150, y: -20 },
            { id: 'a2', x: 150, y: 20 },
            { id: 'b1', x: -150, y: -20 },
            { id: 'b2', x: -150, y: 20 },
        ]
        const edges: GraphLayoutEdge[] = [
            { id: 'ra', source: 'root', target: 'a' },
            { id: 'rb', source: 'root', target: 'b' },
            { id: 'aa1', source: 'a', target: 'a1' },
            { id: 'aa2', source: 'a', target: 'a2' },
            { id: 'bb1', source: 'b', target: 'b1' },
            { id: 'bb2', source: 'b', target: 'b2' },
            { id: 'a1b1', source: 'a1', target: 'b1' },
            { id: 'a2b2', source: 'a2', target: 'b2' },
        ]
        const routes = routeGraphEdges(nodes, edges)
        expect(countRouteCrossings(nodes, edges, routes)).toBe(0)
    })

    it('eliminates crossings in a star-to-ring pattern', () => {
        const n = 8
        const nodes: GraphLayoutNode[] = [{ id: 'center', x: 0, y: 0 }]
        const edges: GraphLayoutEdge[] = []
        for (let i = 0; i < n; i++) {
            const angle = (i / n) * Math.PI * 2
            nodes.push({ id: `ring${i}`, x: 120 * Math.cos(angle), y: 120 * Math.sin(angle) })
            edges.push({ id: `c${i}`, source: 'center', target: `ring${i}` })
        }
        for (let i = 0; i < n; i++) {
            edges.push({ id: `r${i}`, source: `ring${i}`, target: `ring${(i + 1) % n}` })
        }
        const routes = routeGraphEdges(nodes, edges)
        expect(countRouteCrossings(nodes, edges, routes)).toBe(0)
    })

    it('reserves fair bounded request waves without consuming the queue', () => {
        const queue = ['a-out', 'a-in', 'b-out', 'b-in', 'c-out']
        expect(reserveRequestWave(queue, 100, 10, 4)).toEqual([
            { item: 'a-out', limit: 10 },
            { item: 'a-in', limit: 10 },
            { item: 'b-out', limit: 10 },
            { item: 'b-in', limit: 10 }
        ])
        expect(queue).toEqual(['a-out', 'a-in', 'b-out', 'b-in', 'c-out'])
    })

    it('shrinks the last automatic request to the reserved capacity', () => {
        expect(reserveRequestWave(['outgoing', 'incoming'], 13, 10, 4)).toEqual([
            { item: 'outgoing', limit: 10 },
            { item: 'incoming', limit: 3 }
        ])
        expect(reserveRequestWave(['outgoing'], 0, 10, 4)).toEqual([])
    })

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

    it('counts only named objects classified as graph nodes', () => {
        const quads = parse(`
            <https://one.example/root> <https://example.org/local> <urn:local:item> <https://example.org/g> .
            <https://one.example/root> <https://example.org/remote> <https://one.example/not-local> <https://example.org/g> .
            <https://one.example/root> <https://example.org/value> "literal" <https://example.org/g> .
        `)
        const ids = collectGraphNodeIds(quads, quad => quad.object.termType === 'NamedNode' && quad.object.value === 'urn:local:item')
        expect(ids).toEqual(new Set(['https://one.example/root', 'urn:local:item']))
    })

})
