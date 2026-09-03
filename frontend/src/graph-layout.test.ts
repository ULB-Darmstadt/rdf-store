import { describe, expect, it } from 'vitest'
import { Parser } from 'n3'
import {
    collectGraphNodeIds, collectNodeIds, computeHierarchyScore, computeKCore, countRouteCrossings,
    edgePath, estimateLabelSize, flattenLiteralCollections, graphDepths, mergeQuads, radialRadii, reserveRequestWave,
    routeGraphEdges, serializeNQuads, stableGraphSeed, type EdgeRoute, type GraphLayoutEdge,
    type LayoutEngine, type PositionedNode
} from './graph-layout'
import { RadialLayoutEngine } from './graph-layout-radial'
import { HybridLayoutEngine } from './graph-layout-hybrid'
import { HierarchicalLayoutEngine } from './graph-layout-hierarchical'

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
        const nodes: PositionedNode[] = [{ id: 'a', x: 0, y: 0 }, { id: 'b', x: 100, y: 0 }]
        const edges: GraphLayoutEdge[] = [{ id: 'ab', source: 'a', target: 'b', label: 'links' }]
        const route = routeGraphEdges(nodes, edges).get('ab')!
        expect(Math.abs(route.bend)).toBeGreaterThan(0)
        expect(Math.abs(route.bend)).toBeLessThan(30)
    })

    it('chooses either bend direction to avoid nodes', () => {
        const edge: GraphLayoutEdge = { id: 'ab', source: 'a', target: 'b' }
        const above = routeGraphEdges([
            { id: 'a', x: 0, y: 0 }, { id: 'b', x: 100, y: 0 }, { id: 'obstacle', x: 50, y: 15 }
        ], [edge]).get('ab')!
        const below = routeGraphEdges([
            { id: 'a', x: 0, y: 0 }, { id: 'b', x: 100, y: 0 }, { id: 'obstacle', x: 50, y: -15 }
        ], [edge]).get('ab')!
        expect(above.bend).toBeLessThan(0)
        expect(below.bend).toBeGreaterThan(0)
    })

    it('assigns distinct lanes to parallel and reciprocal relationships', () => {
        const nodes: PositionedNode[] = [{ id: 'a', x: 0, y: 0 }, { id: 'b', x: 100, y: 0 }]
        const edges: GraphLayoutEdge[] = [
            { id: 'one', source: 'a', target: 'b' },
            { id: 'two', source: 'a', target: 'b' },
            { id: 'reverse', source: 'b', target: 'a' }
        ]
        const bends = edges.map(edge => routeGraphEdges(nodes, edges).get(edge.id)!.bend)
        expect(new Set(bends).size).toBe(3)
    })

    it('creates separate self-loop routes', () => {
        const nodes: PositionedNode[] = [{ id: 'a', x: 20, y: 30 }]
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
        expect(visible).toMatch(/^M100,20 C/)
        expect(label).toMatch(/^M0,0 C/)
        const visiblePts = visible.match(/C([^ ]+) ([^ ]+)/)
        const labelPts = label.match(/C([^ ]+) ([^ ]+)/)
        expect(visiblePts?.[1]).toBe(labelPts?.[2])
        expect(visiblePts?.[2]).toBe(labelPts?.[1])
    })

    it('keeps visible and label paths aligned after node movement', () => {
        const nodes = new Map([['source', { x: 0, y: 0 }], ['target', { x: 100, y: 0 }]])
        const edge: GraphLayoutEdge = { id: 'edge', source: 'source', target: 'target' }
        const route: EdgeRoute = { bend: -30 }
        nodes.set('target', { x: -40, y: 80 })
        const visible = edgePath(edge, route, nodes)
        const label = edgePath(edge, route, nodes, true)
        expect(visible).toMatch(/^M0,0 C/)
        expect(label).toMatch(/^M-40,80 C/)
        const visiblePts = visible.match(/C([^ ]+) ([^ ]+)/)
        const labelPts = label.match(/C([^ ]+) ([^ ]+)/)
        expect(visiblePts?.[1]).toBe(labelPts?.[2])
        expect(visiblePts?.[2]).toBe(labelPts?.[1])
    })

    it('reduces crossings in a fixed crossing fixture', () => {
        const nodes: PositionedNode[] = [
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
        const nodes: PositionedNode[] = [
            { id: 'root', x: 0, y: 0 },
            { id: 'a', x: 85, y: 0 },
            { id: 'b', x: -85, y: 0 },
            { id: 'a1', x: 150, y: -20 },
            { id: 'a2', x: 150, y: 20 },
            { id: 'b1', x: -150, y: -20 },
            { id: 'b2', x: -150, y: 20 }
        ]
        const edges: GraphLayoutEdge[] = [
            { id: 'ra', source: 'root', target: 'a' },
            { id: 'rb', source: 'root', target: 'b' },
            { id: 'aa1', source: 'a', target: 'a1' },
            { id: 'aa2', source: 'a', target: 'a2' },
            { id: 'bb1', source: 'b', target: 'b1' },
            { id: 'bb2', source: 'b', target: 'b2' },
            { id: 'a1b1', source: 'a1', target: 'b1' },
            { id: 'a2b2', source: 'a2', target: 'b2' }
        ]
        const routes = routeGraphEdges(nodes, edges)
        expect(countRouteCrossings(nodes, edges, routes)).toBe(0)
    })

    it('eliminates crossings in a star-to-ring pattern', () => {
        const n = 8
        const nodes: PositionedNode[] = [{ id: 'center', x: 0, y: 0 }]
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

    it('straightens single-outgoing edges when target is not a leaf', () => {
        const nodes: PositionedNode[] = [
            { id: 'a', x: 0, y: 0 },
            { id: 'b', x: 100, y: 0 },
            { id: 'c', x: 200, y: 0 },
            { id: 'd', x: 250, y: -60 }
        ]
        const edges: GraphLayoutEdge[] = [
            { id: 'a1', source: 'a', target: 'b' },
            { id: 'b1', source: 'b', target: 'c' },
            { id: 'c1', source: 'c', target: 'd' }
        ]
        const routes = routeGraphEdges(nodes, edges)
        expect(routes.get('a1')!.bend).toBe(0)
        expect(routes.get('b1')!.bend).toBe(0)
    })

    it('bends single-outgoing edges when target is a leaf', () => {
        const nodes: PositionedNode[] = [
            { id: 'a', x: 0, y: 0 },
            { id: 'b', x: 100, y: 0 }
        ]
        const edges: GraphLayoutEdge[] = [
            { id: 'e', source: 'a', target: 'b' }
        ]
        const routes = routeGraphEdges(nodes, edges)
        expect(routes.get('e')!.bend).not.toBe(0)
    })

    it('bends single-outgoing edges to a leaf with multiple incoming edges', () => {
        const nodes: PositionedNode[] = [
            { id: 'a', x: -60, y: 0 },
            { id: 'b', x: 60, y: 0 },
            { id: 'leaf', x: 0, y: 120 }
        ]
        const edges: GraphLayoutEdge[] = [
            { id: 'a1', source: 'a', target: 'leaf' },
            { id: 'b1', source: 'b', target: 'leaf' }
        ]
        const routes = routeGraphEdges(nodes, edges)
        expect(routes.get('a1')!.bend).not.toBe(0)
        expect(routes.get('b1')!.bend).not.toBe(0)
    })

    it('bends edges from nodes with multiple outgoing edges', () => {
        const nodes: PositionedNode[] = [
            { id: 'root', x: 0, y: 0 },
            { id: 'a', x: 100, y: -50 },
            { id: 'b', x: 100, y: 50 }
        ]
        const edges: GraphLayoutEdge[] = [
            { id: 'ra', source: 'root', target: 'a' },
            { id: 'rb', source: 'root', target: 'b' }
        ]
        const routes = routeGraphEdges(nodes, edges)
        expect(routes.get('ra')!.bend).not.toBe(0)
        expect(routes.get('rb')!.bend).not.toBe(0)
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

    it('flattens anonymous literal collections onto their owning property', () => {
        const quads = flattenLiteralCollections(parse(`
            <https://example.org/root> <https://example.org/values> _:head <https://example.org/g> .
            _:head <http://www.w3.org/1999/02/22-rdf-syntax-ns#first> "one" <https://example.org/g> .
            _:head <http://www.w3.org/1999/02/22-rdf-syntax-ns#rest> _:tail <https://example.org/g> .
            _:tail <http://www.w3.org/1999/02/22-rdf-syntax-ns#first> "two" <https://example.org/g> .
            _:tail <http://www.w3.org/1999/02/22-rdf-syntax-ns#rest> <http://www.w3.org/1999/02/22-rdf-syntax-ns#nil> <https://example.org/g> .
        `))
        expect(quads).toHaveLength(2)
        expect(quads.map(quad => [quad.subject.value, quad.predicate.value, quad.object.value])).toEqual([
            ['https://example.org/root', 'https://example.org/values', 'one'],
            ['https://example.org/root', 'https://example.org/values', 'two']
        ])
    })

    it('preserves named, resource-valued, and annotated collections', () => {
        const cases = [
            `
                <https://example.org/root> <https://example.org/values> <https://example.org/list> <https://example.org/g> .
                <https://example.org/list> <http://www.w3.org/1999/02/22-rdf-syntax-ns#first> "one" <https://example.org/g> .
                <https://example.org/list> <http://www.w3.org/1999/02/22-rdf-syntax-ns#rest> <http://www.w3.org/1999/02/22-rdf-syntax-ns#nil> <https://example.org/g> .
            `,
            `
                <https://example.org/root> <https://example.org/values> _:head <https://example.org/g> .
                _:head <http://www.w3.org/1999/02/22-rdf-syntax-ns#first> <https://example.org/item> <https://example.org/g> .
                _:head <http://www.w3.org/1999/02/22-rdf-syntax-ns#rest> <http://www.w3.org/1999/02/22-rdf-syntax-ns#nil> <https://example.org/g> .
            `,
            `
                <https://example.org/root> <https://example.org/values> _:head <https://example.org/g> .
                _:head <http://www.w3.org/1999/02/22-rdf-syntax-ns#first> "one" <https://example.org/g> .
                _:head <http://www.w3.org/1999/02/22-rdf-syntax-ns#rest> <http://www.w3.org/1999/02/22-rdf-syntax-ns#nil> <https://example.org/g> .
                _:head <https://example.org/note> "documented" <https://example.org/g> .
            `
        ]
        for (const value of cases) {
            const quads = parse(value)
            expect(flattenLiteralCollections(quads)).toEqual(quads)
        }
    })

})

describe('RadialLayoutEngine', () => {
    const engine = new RadialLayoutEngine()

    it('assigns depth 0 to root and depth 1 to direct neighbors', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' }
        ]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        expect(result.depths.get('root')).toBe(0)
        expect(result.depths.get('a')).toBe(1)
        expect(result.depths.get('b')).toBe(1)
        expect(result.depths.get('c')).toBe(2)
    })

    it('places root at the origin', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }]
        const edges: GraphLayoutEdge[] = [{ id: 'e1', source: 'root', target: 'a' }]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        const rootPos = result.positions.get('root')!
        expect(rootPos.x).toBe(0)
        expect(rootPos.y).toBe(0)
    })

    it('places non-root nodes at their ring radius', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }]
        const edges: GraphLayoutEdge[] = [{ id: 'e1', source: 'root', target: 'a' }]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        const pos = result.positions.get('a')!
        expect(pos.radialRadius).toBeGreaterThan(0)
        const distance = Math.hypot(pos.x, pos.y)
        expect(distance).toBeCloseTo(pos.radialRadius, 0)
    })

    it('produces deterministic output for the same input', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' },
            { id: 'e3', source: 'a', target: 'c' }
        ]
        const first = engine.compute(nodes, edges, 'root', 0.42)
        const second = engine.compute(nodes, edges, 'root', 0.42)
        expect(first.positions.get('a')?.x).toBe(second.positions.get('a')?.x)
        expect(first.positions.get('c')?.x).toBe(second.positions.get('c')?.x)
    })

    it('produces different rotations for different seeds', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' }
        ]
        const first = engine.compute(nodes, edges, 'root', 0.1)
        const second = engine.compute(nodes, edges, 'root', 0.9)
        expect(first.positions.get('a')?.x).not.toBe(second.positions.get('a')?.x)
    })

    it('assigns isolated nodes to a fallback depth beyond connected nodes', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'isolated' }]
        const edges: GraphLayoutEdge[] = [{ id: 'e1', source: 'root', target: 'a' }]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        expect(result.depths.get('isolated')).toBeGreaterThan(result.depths.get('a')!)
    })

    it('computes ring radii that increase with depth', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'a', target: 'b' },
            { id: 'e3', source: 'b', target: 'c' }
        ]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        expect(result.radii.get(1)!).toBeGreaterThan(result.radii.get(0)!)
        expect(result.radii.get(2)!).toBeGreaterThan(result.radii.get(1)!)
    })

    it('implements the LayoutEngine interface', () => {
        const engine: LayoutEngine = new RadialLayoutEngine()
        const result = engine.compute([{ id: 'x' }], [], 'x', 0)
        expect(result.positions.has('x')).toBe(true)
    })

    it('returns larger collision radius for longer labels', () => {
        const engine = new RadialLayoutEngine()
        const short = engine.compute([{ id: 'root' }, { id: 'a' }], [{ id: 'e', source: 'root', target: 'a' }], 'root', 0.5)
        const long = engine.compute([{ id: 'root' }, { id: 'a', label: 'a very long label text' }], [{ id: 'e', source: 'root', target: 'a' }], 'root', 0.5)
        const shortR = short.force.collideRadius({ id: 'a' })
        const longR = long.force.collideRadius({ id: 'a' })
        expect(longR).toBeGreaterThan(shortR)
    })

    it('scales ring radius with wider labels', () => {
        const engine = new RadialLayoutEngine()
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' },
            { id: 'e3', source: 'root', target: 'c' }
        ]
        const narrow = engine.compute([{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' }], edges, 'root', 0.5)
        const wide = engine.compute([{ id: 'root' }, { id: 'a', label: 'alpha beta gamma delta' }, { id: 'b', label: 'alpha beta gamma delta' }, { id: 'c', label: 'alpha beta gamma delta' }], edges, 'root', 0.5)
        expect(wide.radii.get(1)!).toBeGreaterThan(narrow.radii.get(1)!)
    })
})

describe('estimateLabelSize', () => {
    it('returns minimum dimensions for undefined label', () => {
        const size = estimateLabelSize(undefined)
        expect(size.width).toBeGreaterThan(0)
        expect(size.height).toBeGreaterThan(0)
    })

    it('scales width with label length', () => {
        const short = estimateLabelSize('ab')
        const long = estimateLabelSize('a much longer label')
        expect(long.width).toBeGreaterThan(short.width)
    })

    it('strips hidden type spans when measuring', () => {
        const withType = estimateLabelSize('hello <tspan class="type node-type">&lt;Type&gt;</tspan>')
        const plain = estimateLabelSize('hello')
        expect(withType.width).toBe(plain.width)
    })

    it('strips remaining HTML tags when measuring', () => {
        const html = estimateLabelSize('<b>bold</b> text')
        const plain = estimateLabelSize('bold text')
        expect(html.width).toBe(plain.width)
    })
})

describe('computeKCore', () => {
    it('identifies core nodes in a dense graph', () => {
        const nodes = ['a', 'b', 'c', 'd', 'e', 'leaf1', 'leaf2']
        const edges = [
            { source: 'a', target: 'b' },
            { source: 'a', target: 'c' },
            { source: 'a', target: 'd' },
            { source: 'b', target: 'c' },
            { source: 'b', target: 'd' },
            { source: 'c', target: 'd' },
            { source: 'd', target: 'e' },
            { source: 'a', target: 'leaf1' },
            { source: 'b', target: 'leaf2' }
        ]
        const { core } = computeKCore(nodes, edges)
        expect(core.has('a')).toBe(true)
        expect(core.has('b')).toBe(true)
        expect(core.has('c')).toBe(true)
        expect(core.has('d')).toBe(true)
    })

    it('computes k from density', () => {
        const nodes = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j']
        const edges = [
            { source: 'a', target: 'b' },
            { source: 'a', target: 'c' },
            { source: 'b', target: 'c' }
        ]
        const { k } = computeKCore(nodes, edges)
        expect(k).toBeGreaterThanOrEqual(2)
    })
})

describe('HybridLayoutEngine', () => {
    const engine = new HybridLayoutEngine()

    it('produces positions for all nodes', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' },
            { id: 'e3', source: 'a', target: 'c' }
        ]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        expect(result.positions.has('root')).toBe(true)
        expect(result.positions.has('a')).toBe(true)
        expect(result.positions.has('b')).toBe(true)
        expect(result.positions.has('c')).toBe(true)
    })

    it('marks core and leaf nodes correctly', () => {
        const nodes = [
            { id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' },
            { id: 'd' }, { id: 'e' }, { id: 'f' }
        ]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' },
            { id: 'e3', source: 'root', target: 'c' },
            { id: 'e4', source: 'a', target: 'b' },
            { id: 'e5', source: 'a', target: 'c' },
            { id: 'e6', source: 'b', target: 'c' },
            { id: 'e7', source: 'root', target: 'd' },
            { id: 'e8', source: 'root', target: 'e' },
            { id: 'e9', source: 'root', target: 'f' }
        ]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        const rootPos = result.positions.get('root')!
        expect(rootPos.isCore).toBe(true)
        const dPos = result.positions.get('d')!
        expect(dPos.isCore).toBe(false)
    })

    it('returns a valid ForceConfig', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' }
        ]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        expect(result.force).toBeDefined()
        expect(typeof result.force.linkDistance).toBe('function')
        expect(typeof result.force.collideRadius).toBe('function')
        expect(typeof result.force.radialStrength).toBe('function')
    })

    it('implements the LayoutEngine interface', () => {
        const engine: LayoutEngine = new HybridLayoutEngine()
        const result = engine.compute([{ id: 'x' }], [], 'x', 0)
        expect(result.positions.has('x')).toBe(true)
    })

    it('produces deterministic output', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' },
            { id: 'e3', source: 'a', target: 'c' }
        ]
        const first = engine.compute(nodes, edges, 'root', 0.42)
        const second = engine.compute(nodes, edges, 'root', 0.42)
        expect(first.positions.get('a')?.x).toBe(second.positions.get('a')?.x)
        expect(first.positions.get('c')?.x).toBe(second.positions.get('c')?.x)
    })
})

describe('computeHierarchyScore', () => {
    it('returns high score for hierarchical graph', () => {
        const nodes = ['root', 'a', 'b', 'c', 'd']
        const edges = [
            { source: 'root', target: 'a' },
            { source: 'root', target: 'b' },
            { source: 'a', target: 'c' },
            { source: 'b', target: 'd' }
        ]
        const score = computeHierarchyScore(nodes, edges, 'root')
        expect(score).toBeGreaterThan(0.8)
    })

    it('returns low score for cyclic graph', () => {
        const nodes = ['a', 'b', 'c']
        const edges = [
            { source: 'a', target: 'b' },
            { source: 'b', target: 'c' },
            { source: 'c', target: 'a' }
        ]
        const score = computeHierarchyScore(nodes, edges, 'a')
        expect(score).toBeLessThan(0.7)
    })

    it('returns 1.0 for pure tree', () => {
        const nodes = ['root', 'a', 'b', 'c', 'd', 'e']
        const edges = [
            { source: 'root', target: 'a' },
            { source: 'root', target: 'b' },
            { source: 'a', target: 'c' },
            { source: 'a', target: 'd' },
            { source: 'b', target: 'e' }
        ]
        const score = computeHierarchyScore(nodes, edges, 'root')
        expect(score).toBe(1.0)
    })
})

describe('HierarchicalLayoutEngine', () => {
    const engine = new HierarchicalLayoutEngine()

    it('produces positions for all nodes', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' },
            { id: 'e3', source: 'a', target: 'c' }
        ]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        expect(result.positions.has('root')).toBe(true)
        expect(result.positions.has('a')).toBe(true)
        expect(result.positions.has('b')).toBe(true)
        expect(result.positions.has('c')).toBe(true)
    })

    it('assigns increasing layers from root', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' },
            { id: 'e3', source: 'a', target: 'c' }
        ]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        expect(result.positions.get('root')?.layer).toBe(0)
        expect(result.positions.get('a')?.layer).toBe(1)
        expect(result.positions.get('b')?.layer).toBe(1)
        expect(result.positions.get('c')?.layer).toBe(2)
    })

    it('places root at the origin', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }]
        const edges: GraphLayoutEdge[] = [{ id: 'e1', source: 'root', target: 'a' }]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        const rootPos = result.positions.get('root')!
        expect(rootPos.x).toBe(0)
        expect(rootPos.y).toBe(0)
    })

    it('returns a valid ForceConfig', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' }
        ]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        expect(result.force).toBeDefined()
        expect(typeof result.force.linkDistance).toBe('function')
        expect(typeof result.force.collideRadius).toBe('function')
        expect(typeof result.force.radialStrength).toBe('function')
    })

    it('implements the LayoutEngine interface', () => {
        const engine: LayoutEngine = new HierarchicalLayoutEngine()
        const result = engine.compute([{ id: 'x' }], [], 'x', 0)
        expect(result.positions.has('x')).toBe(true)
    })

    it('produces deterministic output', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' },
            { id: 'e3', source: 'a', target: 'c' }
        ]
        const first = engine.compute(nodes, edges, 'root', 0.42)
        const second = engine.compute(nodes, edges, 'root', 0.42)
        expect(first.positions.get('a')?.x).toBe(second.positions.get('a')?.x)
        expect(first.positions.get('c')?.x).toBe(second.positions.get('c')?.x)
    })

    it('assigns y-coordinates increasing with layer depth', () => {
        const nodes = [{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'c' }]
        const edges: GraphLayoutEdge[] = [
            { id: 'e1', source: 'root', target: 'a' },
            { id: 'e2', source: 'root', target: 'b' },
            { id: 'e3', source: 'a', target: 'c' }
        ]
        const result = engine.compute(nodes, edges, 'root', 0.5)
        const rootY = result.positions.get('root')!.y
        const aY = result.positions.get('a')!.y
        const cY = result.positions.get('c')!.y
        expect(aY).toBeGreaterThan(rootY)
        expect(cY).toBeGreaterThan(aY)
    })
})
