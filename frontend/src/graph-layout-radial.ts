import {
    estimateLabelSize, graphDepths, registerLayoutEngine, type ForceConfig,
    type GraphLayoutEdge, type GraphLayoutNode, type LayoutEngine, type LayoutResult,
    type LayoutPosition
} from './graph-layout'

export type RadialPosition = LayoutPosition & { depth: number, radialRadius: number }

export type RadialResult = LayoutResult<RadialPosition> & {
    depths: Map<string, number>
    radii: Map<number, number>
    force: ForceConfig
}

export function barycentricAngle(
    neighbors: string[] | undefined,
    nodeAngle: Map<string, number>,
    nodeDepth: Map<string, number>,
    weight: (neighborDepth: number) => number
): number {
    if (!neighbors || neighbors.length === 0) {
        return Infinity
    }
    let sumSin = 0
    let sumCos = 0
    let totalWeight = 0
    for (const id of neighbors) {
        const angle = nodeAngle.get(id)
        if (angle === undefined) {
            continue
        }
        const neighborDepth = nodeDepth.get(id) ?? 0
        const w = weight(neighborDepth)
        sumSin += Math.sin(angle) * w
        sumCos += Math.cos(angle) * w
        totalWeight += w
    }
    if (totalWeight === 0) {
        return Infinity
    }
    return Math.atan2(sumSin / totalWeight, sumCos / totalWeight)
}

export function subtreeAngle(id: string, neighborMap: Map<string, string[]>, nodeAngle: Map<string, number>, nodeDepth: Map<string, number>): number {
    const neighbors = neighborMap.get(id)
    if (!neighbors || neighbors.length === 0) {
        return nodeAngle.get(id) ?? 0
    }
    const selfDepth = nodeDepth.get(id) ?? 0
    let sumSin = 0
    let sumCos = 0
    let counted = 0
    for (const neighborId of neighbors) {
        const angle = nodeAngle.get(neighborId)
        const neighborDepth = nodeDepth.get(neighborId)
        if (angle === undefined || neighborDepth === undefined) {
            continue
        }
        if (neighborDepth > selfDepth) {
            sumSin += Math.sin(angle)
            sumCos += Math.cos(angle)
            counted++
        }
    }
    if (counted === 0) {
        return nodeAngle.get(id) ?? 0
    }
    return Math.atan2(sumSin / counted, sumCos / counted)
}

export function buildNeighborMap(edges: GraphLayoutEdge[]): Map<string, string[]> {
    const neighbors = new Map<string, string[]>()
    for (const edge of edges) {
        if (edge.source === edge.target) {
            continue
        }
        let list = neighbors.get(edge.source)
        if (!list) {
            neighbors.set(edge.source, list = [])
        }
        list.push(edge.target)
        let revList = neighbors.get(edge.target)
        if (!revList) {
            neighbors.set(edge.target, revList = [])
        }
        revList.push(edge.source)
    }
    return neighbors
}

export class RadialLayoutEngine implements LayoutEngine {
    compute(nodes: GraphLayoutNode[], edges: GraphLayoutEdge[], root: string, seed: number): RadialResult {
        const nodeIds = nodes.map(node => node.id)
        const depths = graphDepths(nodeIds, edges, root)
        const phase = seed * Math.PI * 2

        const rings = new Map<number, GraphLayoutNode[]>()
        const nodeLabelSize = new Map<string, { width: number, height: number }>()
        for (const node of nodes) {
            const size = estimateLabelSize(node.label)
            nodeLabelSize.set(node.id, size)
            const depth = depths.get(node.id) ?? 1
            const ring = rings.get(depth) ?? []
            ring.push(node)
            rings.set(depth, ring)
        }

        const radii = new Map<number, number>([[0, 0]])
        {
            let previousDepth = 0
            let previousRadius = 0
            const sortedDepthsInit = Array.from(rings.keys()).filter(d => d > 0).sort((a, b) => a - b)
            for (const depth of sortedDepthsInit) {
                const ring = rings.get(depth)!
                const avgLabelWidth = ring.reduce((sum, n) => sum + (nodeLabelSize.get(n.id)?.width ?? 50), 0) / ring.length
                const nodeSpacing = Math.max(38, avgLabelWidth + 16)
                const populationRadius = ring.length * nodeSpacing / (Math.PI * 2)
                const radius = Math.max(depth * 85, previousRadius + (depth - previousDepth) * 85, populationRadius)
                radii.set(depth, radius)
                previousDepth = depth
                previousRadius = radius
            }
        }
        const neighborMap = buildNeighborMap(edges)
        const sortedDepths = Array.from(rings.keys()).sort((a, b) => a - b)

        const nodeAngle = new Map<string, number>()

        for (const depth of sortedDepths) {
            const ring = rings.get(depth)!
            for (let index = 0; index < ring.length; index++) {
                nodeAngle.set(ring[index].id, index / ring.length * Math.PI * 2)
            }
        }

        const MIN_GAP = 0.18
        const depthWeight = (neighborDepth: number) => neighborDepth + 1
        const reorderRing = (depth: number) => {
            if (depth === 0) {
                return
            }
            const ring = rings.get(depth)!
            const targets = ring.map(node => ({
                node,
                target: barycentricAngle(neighborMap.get(node.id), nodeAngle, depths, depthWeight)
            }))
            targets.sort((a, b) => {
                if (a.target === Infinity && b.target === Infinity) {
                    return a.node.id.localeCompare(b.node.id)
                }
                if (a.target === Infinity) {
                    return 1
                }
                if (b.target === Infinity) {
                    return -1
                }
                if (Math.abs(a.target - b.target) > 0.001) {
                    return a.target - b.target
                }
                const lSub = subtreeAngle(a.node.id, neighborMap, nodeAngle, depths)
                const rSub = subtreeAngle(b.node.id, neighborMap, nodeAngle, depths)
                if (Math.abs(lSub - rSub) > 0.001) {
                    return lSub - rSub
                }
                return a.node.id.localeCompare(b.node.id)
            })
            let lastAngle = targets[0]?.target === Infinity ? -MIN_GAP : (targets[0]?.target ?? 0) - MIN_GAP
            for (const { node, target } of targets) {
                const ideal = target === Infinity ? lastAngle + MIN_GAP : target
                const angle = Math.max(ideal, lastAngle + MIN_GAP)
                nodeAngle.set(node.id, angle)
                lastAngle = angle
            }
        }

        for (let iteration = 0; iteration < 4; iteration++) {
            for (let i = sortedDepths.length - 1; i >= 0; i--) {
                reorderRing(sortedDepths[i])
            }
            for (const depth of sortedDepths) {
                reorderRing(depth)
            }
        }

        // Assign final positions
        const positions = new Map<string, RadialPosition>()
        for (const depth of sortedDepths) {
            const ring = rings.get(depth)!
            const radius = radii.get(depth) ?? depth * 85
            for (const node of ring) {
                if (node.id === root) {
                    positions.set(node.id, { x: 0, y: 0, depth, radialRadius: radius })
                } else {
                    const angle = phase + (nodeAngle.get(node.id) ?? 0)
                    positions.set(node.id, {
                        x: Math.cos(angle) * radius,
                        y: Math.sin(angle) * radius,
                        depth,
                        radialRadius: radius
                    })
                }
            }
        }

        return {
            positions, depths, radii,
            force: {
                linkDistance: (source, target) => {
                    const sourceRadius = positions.get(source.id)?.radialRadius ?? 0
                    const targetRadius = positions.get(target.id)?.radialRadius ?? 85
                    return Math.abs(targetRadius - sourceRadius) || targetRadius
                },
                linkStrength: 0.5,
                chargeStrength: -80,
                collideRadius: node => {
                    const size = nodeLabelSize.get(node.id)
                    const nodeRadius = node.id === root ? 7 : 4
                    const labelH = size?.height ?? 14
                    const labelW = size?.width ?? 50
                    return nodeRadius + labelH + Math.sqrt(labelW) + 4
                },
                collideIterations: 2,
                radialForce: node => positions.get(node.id)?.radialRadius ?? 85,
                radialStrength: node => node.id === root ? 1 : 0.9,
                alpha: 1.2,
                alphaMin: 0.03,
                alphaDecay: 0.055,
                velocityDecay: 0.5
            }
        }
    }
}

registerLayoutEngine('radial', () => new RadialLayoutEngine())
