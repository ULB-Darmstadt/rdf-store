import { Quad, Writer } from 'n3'

export type GraphPoint = { x: number, y: number }
export type GraphLayoutNode = { id: string, label?: string }
export type PositionedNode = GraphLayoutNode & GraphPoint
export type GraphLayoutEdge = { id: string, source: string, target: string, label?: string }
export type EdgeRoute = { bend: number, loopRadius?: number, loopSide?: -1 | 1, srcTangentAngle?: number, tgtTangentAngle?: number }

export type LayoutPosition = GraphPoint

const FONT_SIZE = 12
const CHAR_WIDTH = 7
const LABEL_PADDING_X = 9
const LABEL_PADDING_Y = 2
const MIN_LABEL_HEIGHT = 14

export function estimateLabelSize(label: string | undefined): { width: number, height: number } {
    const visible = label?.replace(/<tspan class="type[^"]*">[^<]*<\/tspan>/g, '').replace(/<[^>]*>/g, '').trim() ?? ''
    const width = visible.length * CHAR_WIDTH + LABEL_PADDING_X
    const height = Math.max(MIN_LABEL_HEIGHT, FONT_SIZE + LABEL_PADDING_Y)
    return { width, height }
}

export type ForceConfig = {
    linkDistance: (source: { id: string }, target: { id: string }) => number
    linkStrength: number
    chargeStrength: number
    collideRadius: (node: { id: string }) => number
    collideIterations: number
    radialForce: ((node: { id: string }) => number) | null
    radialStrength: (node: { id: string }) => number
    alpha: number
    alphaMin: number
    alphaDecay: number
    velocityDecay: number
}

export type LayoutResult<P extends LayoutPosition = LayoutPosition> = {
    positions: Map<string, P>
    force?: ForceConfig
}

export interface LayoutEngine {
    compute(nodes: GraphLayoutNode[], edges: GraphLayoutEdge[], root: string, seed: number): LayoutResult
}

const engines: Record<string, () => LayoutEngine> = {}

export function registerLayoutEngine(type: string, factory: () => LayoutEngine) {
    engines[type] = factory
}

export function computeHierarchyScore(
    nodeIds: string[],
    edges: Pick<GraphLayoutEdge, 'source' | 'target'>[],
    root: string
): number {
    const parents = new Map<string, string[]>()
    for (const id of nodeIds) {
        parents.set(id, [])
    }
    for (const edge of edges) {
        if (edge.source === edge.target) {
            continue
        }
        parents.get(edge.target)?.push(edge.source)
    }

    const layers = new Map<string, number>()
    for (const id of nodeIds) {
        layers.set(id, 0)
    }
    layers.set(root, 0)

    let changed = true
    while (changed) {
        changed = false
        for (const id of nodeIds) {
            if (id === root) {
                continue
            }
            const parentLayers = (parents.get(id) ?? []).map(p => layers.get(p) ?? 0)
            const best = parentLayers.length > 0 ? Math.max(...parentLayers) + 1 : 0
            if (best > (layers.get(id) ?? 0)) {
                layers.set(id, best)
                changed = true
            }
        }
    }

    let downward = 0
    let total = 0
    for (const edge of edges) {
        if (edge.source === edge.target) {
            continue
        }
        const srcLayer = layers.get(edge.source) ?? 0
        const tgtLayer = layers.get(edge.target) ?? 0
        if (srcLayer < tgtLayer) {
            downward++
        }
        total++
    }

    return total > 0 ? downward / total : 0
}

// @ts-ignore
export function selectLayoutEngine(nodes: GraphLayoutNode[], edges: GraphLayoutEdge[], root: string): LayoutEngine {
    // select only radial for now
    return engines['radial']()
    /*
    const nodeCount = nodes.length
    if (nodeCount < 20) {
        return engines['radial']()
    }
    const nodeIds = nodes.map(n => n.id)
    const nonSelfEdges = edges.filter(e => e.source !== e.target)
    const hierarchyScore = computeHierarchyScore(nodeIds, nonSelfEdges, root)
    if (hierarchyScore > 0.6 && engines['hierarchical']) {
        return engines['hierarchical']()
    }
    const { core } = computeKCore(nodeIds, nonSelfEdges)
    const coreRatio = core.size / nodeCount
    if (coreRatio > 0.3 && engines['hybrid']) {
        return engines['hybrid']()
    }
    return engines['radial']()
    */
}

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

export function collectGraphNodeIds(quads: Iterable<Quad>, isNodeObject: (quad: Quad) => boolean) {
    const ids = new Set<string>()
    for (const quad of quads) {
        ids.add(nodeId(quad.subject, quad.graph.value))
        if (isNodeObject(quad)) {
            ids.add(nodeId(quad.object, quad.graph.value))
        }
    }
    return ids
}

export function mergeQuads(target: Map<string, Quad>, quads: Quad[], collect = collectNodeIds) {
    const previousNodeIds = collect(target.values())
    for (const quad of quads) {
        target.set(quadKey(quad), quad)
    }
    const currentNodeIds = collect(target.values())
    return new Set(Array.from(currentNodeIds).filter(id => !previousNodeIds.has(id)))
}

export function reserveRequestWave<T>(queue: T[], capacity: number, pageSize: number, waveSize: number) {
    const requests: Array<{ item: T, limit: number }> = []
    let remaining = Math.max(0, capacity)
    while (requests.length < waveSize && requests.length < queue.length && remaining > 0) {
        const limit = Math.min(pageSize, remaining)
        requests.push({ item: queue[requests.length], limit })
        remaining -= limit
    }
    return requests
}

export function computeKCore(nodeIds: string[], edges: Pick<GraphLayoutEdge, 'source' | 'target'>[]): { core: Set<string>, k: number } {
    const degree = new Map<string, number>()
    const adjacency = new Map<string, Set<string>>()
    for (const id of nodeIds) {
        degree.set(id, 0)
        adjacency.set(id, new Set())
    }
    for (const edge of edges) {
        if (edge.source === edge.target) {
            continue
        }
        if (!adjacency.has(edge.source)) {
            adjacency.set(edge.source, new Set())
        }
        if (!adjacency.has(edge.target)) {
            adjacency.set(edge.target, new Set())
        }
        adjacency.get(edge.source)!.add(edge.target)
        adjacency.get(edge.target)!.add(edge.source)
        degree.set(edge.source, (degree.get(edge.source) ?? 0) + 1)
        degree.set(edge.target, (degree.get(edge.target) ?? 0) + 1)
    }
    const k = Math.max(2, Math.floor(edges.length / Math.max(1, nodeIds.length)))
    const removed = new Set<string>()
    const queue: string[] = []
    for (const [id, deg] of degree) {
        if (deg < k) {
            queue.push(id)
        }
    }
    while (queue.length > 0) {
        const current = queue.pop()!
        if (removed.has(current)) {
            continue
        }
        removed.add(current)
        for (const neighbor of adjacency.get(current) ?? []) {
            if (removed.has(neighbor)) {
                continue
            }
            const newDeg = (degree.get(neighbor) ?? 1) - 1
            degree.set(neighbor, newDeg)
            if (newDeg < k) {
                queue.push(neighbor)
            }
        }
    }
    const core = new Set(nodeIds.filter(id => !removed.has(id)))
    return { core, k }
}

export function graphDepths(nodeIds: Iterable<string>, edges: Iterable<Pick<GraphLayoutEdge, 'source' | 'target'>>, root: string) {
    const neighbors = new Map<string, Set<string>>()
    for (const id of nodeIds) {
        neighbors.set(id, new Set())
    }
    for (const edge of edges) {
        if (edge.source === edge.target) {
            continue
        }
        if (!neighbors.has(edge.source)) {
            neighbors.set(edge.source, new Set())
        }
        if (!neighbors.has(edge.target)) {
            neighbors.set(edge.target, new Set())
        }
        neighbors.get(edge.source)!.add(edge.target)
        neighbors.get(edge.target)!.add(edge.source)
    }
    const depths = new Map<string, number>()
    if (!neighbors.has(root)) {
        return depths
    }
    depths.set(root, 0)
    const queue = [root]
    for (let offset = 0; offset < queue.length; offset++) {
        const current = queue[offset]
        for (const neighbor of Array.from(neighbors.get(current) ?? []).sort()) {
            if (!depths.has(neighbor)) {
                depths.set(neighbor, depths.get(current)! + 1)
                queue.push(neighbor)
            }
        }
    }
    const fallbackDepth = Math.max(0, ...depths.values()) + 1
    for (const id of Array.from(neighbors.keys()).sort()) {
        if (!depths.has(id)) {
            depths.set(id, fallbackDepth)
        }
    }
    return depths
}

export function radialRadii(depths: Iterable<number>, layerGap = 85, nodeSpacing = 38) {
    const counts = new Map<number, number>()
    for (const depth of depths) {
        counts.set(depth, (counts.get(depth) ?? 0) + 1)
    }
    const radii = new Map<number, number>([[0, 0]])
    let previousDepth = 0
    let previousRadius = 0
    for (const depth of Array.from(counts.keys()).filter(depth => depth > 0).sort((left, right) => left - right)) {
        const populationRadius = (counts.get(depth) ?? 1) * nodeSpacing / (Math.PI * 2)
        const radius = Math.max(depth * layerGap, previousRadius + (depth - previousDepth) * layerGap, populationRadius)
        radii.set(depth, radius)
        previousDepth = depth
        previousRadius = radius
    }
    return radii
}

export function stableGraphSeed(root: string, nodeIds: Iterable<string>, edgeIds: Iterable<string>) {
    const value = [root, ...Array.from(nodeIds).sort(), ...Array.from(edgeIds).sort()].join('\u0000')
    let hash = 2166136261
    for (let index = 0; index < value.length; index++) {
        hash ^= value.charCodeAt(index)
        hash = Math.imul(hash, 16777619)
    }
    return (hash >>> 0) / 4294967296
}


export function routeGraphEdges(nodes: Iterable<PositionedNode>, edges: GraphLayoutEdge[]) {
    const positions = new Map(Array.from(nodes, node => [node.id, node]))
    const routes = new Map<string, EdgeRoute>()
    const sortedEdges = [...edges].sort((left, right) => edgePairKey(left).localeCompare(edgePairKey(right)) || left.id.localeCompare(right.id))
    const pairSizes = new Map<string, number>()
    for (const edge of sortedEdges) {
        const key = edgePairKey(edge)
        pairSizes.set(key, (pairSizes.get(key) ?? 0) + 1)
    }
    const straightEdges = new Set<string>()
    const outDegree = new Map<string, number>()
    for (const edge of sortedEdges) {
        if (edge.source === edge.target) {
            continue
        }
        outDegree.set(edge.source, (outDegree.get(edge.source) ?? 0) + 1)
    }
    for (const edge of sortedEdges) {
        if (edge.source !== edge.target && (outDegree.get(edge.source) ?? 0) === 1 && (outDegree.get(edge.target) ?? 0) > 0) {
            straightEdges.add(edge.id)
        }
    }
    const loopIndexes = new Map<string, number>()
    for (const edge of sortedEdges) {
        if (edge.source === edge.target) {
            const index = loopIndexes.get(edge.source) ?? 0
            routes.set(edge.id, {
                bend: 0,
                loopRadius: 22 + Math.floor(index / 2) * 14,
                loopSide: index % 2 === 0 ? -1 : 1
            })
            loopIndexes.set(edge.source, index + 1)
            continue
        }
        const candidates = routeCandidates(edge, positions, pairSizes.get(edgePairKey(edge)) ?? 1)
        routes.set(edge.id, bestRoute(edge, candidates, positions, sortedEdges, routes, straightEdges))
    }

    // Five deterministic improvement passes reduce the ordering bias of the
    // initial greedy assignment without introducing an expensive global solver.
    for (let pass = 0; pass < 5; pass++) {
        for (const edge of sortedEdges) {
            if (edge.source === edge.target) {
                continue
            }
            const candidates = routeCandidates(edge, positions, pairSizes.get(edgePairKey(edge)) ?? 1)
            routes.delete(edge.id)
            routes.set(edge.id, bestRoute(edge, candidates, positions, sortedEdges, routes, straightEdges))
        }
    }
    smoothTangentAngles(sortedEdges, routes, positions)
    return routes
}

function smoothTangentAngles(edges: GraphLayoutEdge[], routes: Map<string, EdgeRoute>, positions: Map<string, GraphPoint>) {
    const incoming = new Map<string, { edgeId: string, angle: number }[]>()
    const outgoing = new Map<string, { edgeId: string, angle: number }[]>()
    for (const edge of edges) {
        if (edge.source === edge.target) {
            continue
        }
        const route = routes.get(edge.id)
        if (!route || Math.abs(route.bend) < 0.001) {
            continue
        }
        const source = positions.get(edge.source)
        const target = positions.get(edge.target)
        if (!source || !target) {
            continue
        }
        const dx = target.x - source.x
        const dy = target.y - source.y
        const len = Math.hypot(dx, dy)
        if (len < 1) {
            continue
        }
        const px = dx / len
        const py = -dy / len
        const srcAngle = route.srcTangentAngle ?? Math.atan2(dy / 2 + px * route.bend, dx / 2 + py * route.bend)
        const tgtAngle = route.tgtTangentAngle ?? Math.atan2(-dy / 2 + px * route.bend, -dx / 2 + py * route.bend)
        if (!outgoing.has(edge.source)) {
            outgoing.set(edge.source, [])
        }
        outgoing.get(edge.source)!.push({ edgeId: edge.id, angle: srcAngle })
        if (!incoming.has(edge.target)) {
            incoming.set(edge.target, [])
        }
        incoming.get(edge.target)!.push({ edgeId: edge.id, angle: tgtAngle })
    }
    for (const [nodeId, inEdges] of incoming) {
        const outEdges = outgoing.get(nodeId)
        if (!outEdges || inEdges.length !== 1 || outEdges.length !== 1) {
            continue
        }
        const inGeom = inEdges[0].angle + Math.PI
        const outGeom = outEdges[0].angle
        const diff = Math.atan2(Math.sin(outGeom - inGeom), Math.cos(outGeom - inGeom))
        if (Math.abs(diff) > Math.PI / 2) {
            continue
        }
        const avg = inGeom + diff / 2
        const pull = 0.3
        const inRoute = routes.get(inEdges[0].edgeId)
        const outRoute = routes.get(outEdges[0].edgeId)
        const pullIn = Math.atan2(Math.sin(avg - inGeom), Math.cos(avg - inGeom)) * pull
        const pullOut = Math.atan2(Math.sin(avg - outGeom), Math.cos(avg - outGeom)) * pull
        if (inRoute) {
            inRoute.tgtTangentAngle = inGeom + pullIn - Math.PI
        }
        if (outRoute) {
            outRoute.srcTangentAngle = outGeom + pullOut
        }
    }
}

export function edgePath(edge: GraphLayoutEdge, route: EdgeRoute, nodes: Map<string, GraphPoint>, forLabel = false) {
    const source = nodes.get(edge.source)
    const target = nodes.get(edge.target)
    if (!source || !target) {
        return ''
    }
    if (edge.source === edge.target) {
        const radius = route.loopRadius ?? 22
        const side = route.loopSide ?? -1
        const nodeRadius = 7
        return `M${source.x - nodeRadius},${source.y} C${source.x - radius},${source.y + side * radius} ${source.x + radius},${source.y + side * radius} ${source.x + nodeRadius},${source.y}`
    }
    const reverse = forLabel && target.x < source.x
    const start = reverse ? target : source
    const end = reverse ? source : target
    if (Math.abs(route.bend) < 0.001) {
        return `M${start.x},${start.y} L${end.x},${end.y}`
    }
    const dx = target.x - source.x
    const dy = target.y - source.y
    const dist = Math.hypot(dx, dy)
    const scale = dist / 3
    const srcAngle = route.srcTangentAngle ?? Math.atan2(dy / 2 + (dx / dist) * route.bend, dx / 2 + (-dy / dist) * route.bend)
    const tgtAngle = route.tgtTangentAngle ?? Math.atan2(-dy / 2 + (dx / dist) * route.bend, -dx / 2 + (-dy / dist) * route.bend)
    const srcCx = source.x + Math.cos(srcAngle) * scale
    const srcCy = source.y + Math.sin(srcAngle) * scale
    const tgtCx = target.x + Math.cos(tgtAngle) * scale
    const tgtCy = target.y + Math.sin(tgtAngle) * scale
    const cp1 = reverse ? { x: tgtCx, y: tgtCy } : { x: srcCx, y: srcCy }
    const cp2 = reverse ? { x: srcCx, y: srcCy } : { x: tgtCx, y: tgtCy }
    return `M${start.x},${start.y} C${cp1.x},${cp1.y} ${cp2.x},${cp2.y} ${end.x},${end.y}`
}

export function countRouteCrossings(nodes: Iterable<PositionedNode>, edges: GraphLayoutEdge[], routes: Map<string, EdgeRoute>) {
    const positions = new Map(Array.from(nodes, node => [node.id, node]))
    let crossings = 0
    for (let leftIndex = 0; leftIndex < edges.length; leftIndex++) {
        const left = edges[leftIndex]
        const leftRoute = routes.get(left.id)
        if (!leftRoute) {
            continue
        }
        for (let rightIndex = leftIndex + 1; rightIndex < edges.length; rightIndex++) {
            const right = edges[rightIndex]
            const rightRoute = routes.get(right.id)
            if (!rightRoute) {
                continue
            }
            if (polylinesCross(routePoints(left, leftRoute, positions), routePoints(right, rightRoute, positions))) {
                crossings++
            }
        }
    }
    return crossings
}

function bestRoute(edge: GraphLayoutEdge, candidates: EdgeRoute[], positions: Map<string, GraphPoint>, edges: GraphLayoutEdge[], routes: Map<string, EdgeRoute>, straightEdges: Set<string>) {
    let selected = candidates[0]
    let selectedScore = Number.POSITIVE_INFINITY
    const source = positions.get(edge.source)
    const target = positions.get(edge.target)
    const distance = source && target ? pointDistance(source, target) : 100
    const preferPositive = source && target ? preferredBendSign(source, target) : undefined
    const preferStraight = straightEdges.has(edge.id)
    for (const candidate of candidates) {
        const points = routePoints(edge, candidate, positions)
        const relativeBend = Math.abs(candidate.bend) / Math.max(1, distance)
        let score = Math.abs(candidate.bend) * 0.05 + relativeBend * relativeBend * 20
        if (Math.abs(candidate.bend) < 0.001) {
            score += preferStraight ? -10 : Math.max(2, distance * 0.03)
        }
        if (preferPositive !== undefined && Math.abs(candidate.bend) > 0.001) {
            const wrongDirection = preferPositive ? candidate.bend < 0 : candidate.bend > 0
            if (wrongDirection) {
                score += 0.8
            }
        }
        for (const [nodeId, node] of positions) {
            if (nodeId === edge.source || nodeId === edge.target) {
                continue
            }
            const dist = distanceToPolyline(node, points)
            if (dist < 8) {
                score += 1000 + (8 - dist) * 200
            }
        }
        for (const other of edges) {
            const otherRoute = routes.get(other.id)
            if (!otherRoute || other.id === edge.id) {
                continue
            }
            if (edgePairKey(edge) === edgePairKey(other) && Math.abs(candidate.bend - otherRoute.bend) < 0.001) {
                score += 100000
            }
            if (polylinesCross(points, routePoints(other, otherRoute, positions))) {
                score += 10000
            }
        }
        if (score < selectedScore) {
            selected = candidate
            selectedScore = score
        }
    }
    return selected
}

function routeCandidates(edge: GraphLayoutEdge, positions: Map<string, GraphPoint>, pairSize: number) {
    const source = positions.get(edge.source)
    const target = positions.get(edge.target)
    if (!source || !target) {
        return [{ bend: 0 }]
    }
    const distance = pointDistance(source, target)
    const step = Math.max(16, Math.min(56, distance * 0.22))
    const candidates: EdgeRoute[] = [{ bend: 0 }]
    const laneCount = Math.max(3, Math.ceil(pairSize / 2) + 1)
    const preferPositive = preferredBendSign(source, target)
    for (let lane = 1; lane <= laneCount; lane++) {
        if (preferPositive === undefined) {
            candidates.push({ bend: step * lane }, { bend: -step * lane })
        } else if (preferPositive) {
            candidates.push({ bend: step * lane }, { bend: -step * lane })
        } else {
            candidates.push({ bend: -step * lane }, { bend: step * lane })
        }
    }
    return candidates
}

function preferredBendSign(source: GraphPoint, target: GraphPoint): boolean | undefined {
    const dx = target.x - source.x
    const dy = target.y - source.y
    const len = Math.hypot(dx, dy)
    if (len < 1) {
        return undefined
    }
    const distFromOrigin = Math.hypot(source.x, source.y)
    if (distFromOrigin < 10) {
        const angleFromHorizontal = Math.abs(Math.atan2(Math.abs(dy), Math.abs(dx)))
        if (angleFromHorizontal < 35 * Math.PI / 180) {
            return false
        }
        return true
    }
    const cross = dx * (-source.y) - dy * (-source.x)
    if (Math.abs(cross) < 0.5) {
        return undefined
    }
    return cross < 0
}

function edgePairKey(edge: Pick<GraphLayoutEdge, 'source' | 'target'>) {
    return edge.source < edge.target ? `${edge.source}\u0000${edge.target}` : `${edge.target}\u0000${edge.source}`
}

function routePoints(edge: GraphLayoutEdge, route: EdgeRoute, positions: Map<string, GraphPoint>) {
    const source = positions.get(edge.source)
    const target = positions.get(edge.target)
    if (!source || !target) {
        return []
    }
    if (edge.source === edge.target) {
        const radius = route.loopRadius ?? 22
        const side = route.loopSide ?? -1
        const start = { x: source.x - 7, y: source.y }
        const control1 = { x: source.x - radius, y: source.y + side * radius }
        const control2 = { x: source.x + radius, y: source.y + side * radius }
        const end = { x: source.x + 7, y: source.y }
        return Array.from({ length: 25 }, (_, index) => cubicPoint(start, control1, control2, end, index / 24))
    }
    if (Math.abs(route.bend) > 0.001) {
        const dx = target.x - source.x
        const dy = target.y - source.y
        const dist = Math.hypot(dx, dy)
        const scale = dist / 3
        const srcAngle = route.srcTangentAngle ?? Math.atan2(dy / 2 + (dx / dist) * route.bend, dx / 2 + (-dy / dist) * route.bend)
        const tgtAngle = route.tgtTangentAngle ?? Math.atan2(-dy / 2 + (dx / dist) * route.bend, -dx / 2 + (-dy / dist) * route.bend)
        const cp1 = { x: source.x + Math.cos(srcAngle) * scale, y: source.y + Math.sin(srcAngle) * scale }
        const cp2 = { x: target.x + Math.cos(tgtAngle) * scale, y: target.y + Math.sin(tgtAngle) * scale }
        return Array.from({ length: 25 }, (_, index) => cubicPoint(source, cp1, cp2, target, index / 24))
    }
    return Array.from({ length: 25 }, (_, index) => quadraticPoint(source, { x: (source.x + target.x) / 2, y: (source.y + target.y) / 2 }, target, index / 24))
}

function quadraticPoint(start: GraphPoint, control: GraphPoint, end: GraphPoint, time: number) {
    const inverse = 1 - time
    return {
        x: inverse * inverse * start.x + 2 * inverse * time * control.x + time * time * end.x,
        y: inverse * inverse * start.y + 2 * inverse * time * control.y + time * time * end.y
    }
}

function cubicPoint(start: GraphPoint, control1: GraphPoint, control2: GraphPoint, end: GraphPoint, time: number) {
    const inverse = 1 - time
    return {
        x: inverse ** 3 * start.x + 3 * inverse * inverse * time * control1.x + 3 * inverse * time * time * control2.x + time ** 3 * end.x,
        y: inverse ** 3 * start.y + 3 * inverse * inverse * time * control1.y + 3 * inverse * time * time * control2.y + time ** 3 * end.y
    }
}

function distanceToPolyline(point: GraphPoint, polyline: GraphPoint[]) {
    let distance = Number.POSITIVE_INFINITY
    for (let index = 1; index < polyline.length; index++) {
        distance = Math.min(distance, distanceToSegment(point, polyline[index - 1], polyline[index]))
    }
    return distance
}

function distanceToSegment(point: GraphPoint, start: GraphPoint, end: GraphPoint) {
    const dx = end.x - start.x
    const dy = end.y - start.y
    const lengthSquared = dx * dx + dy * dy
    const time = lengthSquared === 0 ? 0 : Math.max(0, Math.min(1, ((point.x - start.x) * dx + (point.y - start.y) * dy) / lengthSquared))
    return Math.hypot(point.x - (start.x + time * dx), point.y - (start.y + time * dy))
}

function pointDistance(left: GraphPoint, right: GraphPoint) {
    return Math.hypot(right.x - left.x, right.y - left.y)
}

export function computeFlowDirections(positions: Map<string, GraphPoint>, edges: GraphLayoutEdge[]): Map<string, GraphPoint> {
    const neighborSums = new Map<string, { x: number, y: number }>()
    for (const edge of edges) {
        if (edge.source === edge.target) {
            continue
        }
        const src = positions.get(edge.source)
        const tgt = positions.get(edge.target)
        if (!src || !tgt) {
            continue
        }
        const srcDx = tgt.x - src.x
        const srcDy = tgt.y - src.y
        const srcLen = Math.hypot(srcDx, srcDy) || 1
        const srcSum = neighborSums.get(edge.source) ?? { x: 0, y: 0 }
        srcSum.x += srcDx / srcLen
        srcSum.y += srcDy / srcLen
        neighborSums.set(edge.source, srcSum)
        const tgtDx = src.x - tgt.x
        const tgtDy = src.y - tgt.y
        const tgtLen = Math.hypot(tgtDx, tgtDy) || 1
        const tgtSum = neighborSums.get(edge.target) ?? { x: 0, y: 0 }
        tgtSum.x += tgtDx / tgtLen
        tgtSum.y += tgtDy / tgtLen
        neighborSums.set(edge.target, tgtSum)
    }
    const flows = new Map<string, GraphPoint>()
    for (const [id, sum] of neighborSums) {
        const len = Math.hypot(sum.x, sum.y) || 1
        flows.set(id, { x: sum.x / len, y: sum.y / len })
    }
    return flows
}

function polylinesCross(left: GraphPoint[], right: GraphPoint[]) {
    for (let leftIndex = 1; leftIndex < left.length; leftIndex++) {
        for (let rightIndex = 1; rightIndex < right.length; rightIndex++) {
            if (segmentsCross(left[leftIndex - 1], left[leftIndex], right[rightIndex - 1], right[rightIndex])) {
                return true
            }
        }
    }
    return false
}

function segmentsCross(a: GraphPoint, b: GraphPoint, c: GraphPoint, d: GraphPoint) {
    const orientation = (first: GraphPoint, second: GraphPoint, third: GraphPoint) =>
        (second.x - first.x) * (third.y - first.y) - (second.y - first.y) * (third.x - first.x)
    const abC = orientation(a, b, c)
    const abD = orientation(a, b, d)
    const cdA = orientation(c, d, a)
    const cdB = orientation(c, d, b)
    return abC * abD < -0.000001 && cdA * cdB < -0.000001
}

export function serializeNQuads(quads: Iterable<Quad>) {
    return new Writer({ format: 'N-Quads' }).quadsToString(Array.from(quads))
}
