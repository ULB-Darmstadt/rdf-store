import { Quad, Writer } from 'n3'

export type GraphPoint = { x: number, y: number }
export type GraphLayoutNode = GraphPoint & { id: string }
export type GraphLayoutEdge = { id: string, source: string, target: string, label?: string }
export type EdgeRoute = { bend: number, loopRadius?: number, loopSide?: -1 | 1 }

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

export function routeGraphEdges(nodes: Iterable<GraphLayoutNode>, edges: GraphLayoutEdge[]) {
    const positions = new Map(Array.from(nodes, node => [node.id, node]))
    const routes = new Map<string, EdgeRoute>()
    const sortedEdges = [...edges].sort((left, right) => edgePairKey(left).localeCompare(edgePairKey(right)) || left.id.localeCompare(right.id))
    const pairSizes = new Map<string, number>()
    for (const edge of sortedEdges) {
        const key = edgePairKey(edge)
        pairSizes.set(key, (pairSizes.get(key) ?? 0) + 1)
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
        routes.set(edge.id, bestRoute(edge, candidates, positions, sortedEdges, routes))
    }

    // Two deterministic improvement passes reduce the ordering bias of the
    // initial greedy assignment without introducing an expensive global solver.
    for (let pass = 0; pass < 2; pass++) {
        for (const edge of sortedEdges) {
            if (edge.source === edge.target) {
                continue
            }
            const candidates = routeCandidates(edge, positions, pairSizes.get(edgePairKey(edge)) ?? 1)
            routes.delete(edge.id)
            routes.set(edge.id, bestRoute(edge, candidates, positions, sortedEdges, routes))
        }
    }
    return routes
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
    const control = edgeControlPoint(source, target, route.bend)
    const reverse = forLabel && target.x < source.x
    const start = reverse ? target : source
    const end = reverse ? source : target
    if (Math.abs(route.bend) < 0.001) {
        return `M${start.x},${start.y} L${end.x},${end.y}`
    }
    return `M${start.x},${start.y} Q${control.x},${control.y} ${end.x},${end.y}`
}

export function countRouteCrossings(nodes: Iterable<GraphLayoutNode>, edges: GraphLayoutEdge[], routes: Map<string, EdgeRoute>) {
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
            if (!rightRoute || edgesShareEndpoint(left, right)) {
                continue
            }
            if (polylinesCross(routePoints(left, leftRoute, positions), routePoints(right, rightRoute, positions))) {
                crossings++
            }
        }
    }
    return crossings
}

function bestRoute(edge: GraphLayoutEdge, candidates: EdgeRoute[], positions: Map<string, GraphPoint>, edges: GraphLayoutEdge[], routes: Map<string, EdgeRoute>) {
    let selected = candidates[0]
    let selectedScore = Number.POSITIVE_INFINITY
    for (const candidate of candidates) {
        const points = routePoints(edge, candidate, positions)
        let score = Math.abs(candidate.bend) * 0.05
        for (const [nodeId, node] of positions) {
            if (nodeId === edge.source || nodeId === edge.target) {
                continue
            }
            const distance = distanceToPolyline(node, points)
            if (distance < 14) {
                score += 1000 + (14 - distance) * 200
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
            if (!edgesShareEndpoint(edge, other) && polylinesCross(points, routePoints(other, otherRoute, positions))) {
                score += 10000
            }
            const labelDistance = pointDistance(midpoint(points), midpoint(routePoints(other, otherRoute, positions)))
            const labelClearance = Math.min(120, ((edge.label?.length ?? 8) + (other.label?.length ?? 8)) * 1.8)
            if (labelDistance < labelClearance) {
                score += (labelClearance - labelDistance) * 10
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
    for (let lane = 1; lane <= laneCount; lane++) {
        candidates.push({ bend: step * lane }, { bend: -step * lane })
    }
    return candidates
}

function edgePairKey(edge: Pick<GraphLayoutEdge, 'source' | 'target'>) {
    return edge.source < edge.target ? `${edge.source}\u0000${edge.target}` : `${edge.target}\u0000${edge.source}`
}

function edgesShareEndpoint(left: GraphLayoutEdge, right: GraphLayoutEdge) {
    return left.source === right.source || left.source === right.target || left.target === right.source || left.target === right.target
}

function edgeControlPoint(source: GraphPoint, target: GraphPoint, bend: number) {
    const dx = target.x - source.x
    const dy = target.y - source.y
    const length = Math.max(Math.hypot(dx, dy), 1)
    return {
        x: (source.x + target.x) / 2 - dy / length * bend,
        y: (source.y + target.y) / 2 + dx / length * bend
    }
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
        return Array.from({ length: 13 }, (_, index) => cubicPoint(start, control1, control2, end, index / 12))
    }
    const control = edgeControlPoint(source, target, route.bend)
    return Array.from({ length: 13 }, (_, index) => quadraticPoint(source, control, target, index / 12))
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

function midpoint(points: GraphPoint[]) {
    return points[Math.floor(points.length / 2)] ?? { x: 0, y: 0 }
}

function pointDistance(left: GraphPoint, right: GraphPoint) {
    return Math.hypot(right.x - left.x, right.y - left.y)
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
