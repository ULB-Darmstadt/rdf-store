import {
    computeKCore, estimateLabelSize, registerLayoutEngine, type ForceConfig,
    type GraphLayoutEdge, type GraphLayoutNode, type LayoutEngine, type LayoutResult,
    type LayoutPosition
} from './graph-layout'
import * as d3force from 'd3-force'

type HybridPosition = LayoutPosition & { depth: number, isCore: boolean }

type HybridResult = LayoutResult<HybridPosition> & {
    force: ForceConfig
}

interface CoreNode extends d3force.SimulationNodeDatum {
    id: string
}

interface CoreLink extends d3force.SimulationLinkDatum<CoreNode> {
    id: string
}

export class HybridLayoutEngine implements LayoutEngine {
    compute(nodes: GraphLayoutNode[], edges: GraphLayoutEdge[], root: string, _seed: number): HybridResult {
        console.log('--- hybrid')
        const nodeLabelSize = new Map<string, { width: number, height: number }>()
        for (const node of nodes) {
            nodeLabelSize.set(node.id, estimateLabelSize(node.label))
        }
        const nonSelfEdges = edges.filter(e => e.source !== e.target)
        const { core } = computeKCore(nodes.map(n => n.id), nonSelfEdges)
        const coreSet = core
        const leafNodes = nodes.filter(n => !coreSet.has(n.id))
        const coreNodes = nodes.filter(n => coreSet.has(n.id))

        const positions = new Map<string, HybridPosition>()

        if (coreNodes.length === 0) {
            for (const node of nodes) {
                positions.set(node.id, { x: 0, y: 0, depth: 0, isCore: false })
            }
        } else {
            this.layoutCore(coreNodes, nonSelfEdges, positions)
            this.layoutLeaves(leafNodes, nonSelfEdges, coreSet, positions, root)
        }

        return {
            positions,
            force: this.buildForceConfig(nodes, nonSelfEdges, coreSet, positions, nodeLabelSize, root)
        }
    }

    private layoutCore(
        coreNodes: GraphLayoutNode[],
        edges: GraphLayoutEdge[],
        positions: Map<string, HybridPosition>
    ) {
        const simNodes: CoreNode[] = coreNodes.map(n => ({ id: n.id }))
        const simLinks: CoreLink[] = edges
            .filter(e => coreNodes.some(c => c.id === e.source) && coreNodes.some(c => c.id === e.target))
            .map(e => ({ id: e.id, source: e.source, target: e.target }))

        const simulation = d3force.forceSimulation<CoreNode, CoreLink>(simNodes)
            .force('center', d3force.forceCenter(0, 0))
            .force('charge', d3force.forceManyBody().strength(-200))
            .force('link', d3force.forceLink<CoreNode, CoreLink>(simLinks)
                .id(n => n.id)
                .distance(80)
                .strength(0.5))
            .force('collide', d3force.forceCollide<CoreNode>()
                .radius(() => 7 + 14 + Math.sqrt(50) + 4)
                .iterations(2))
            .alpha(1.2)
            .alphaMin(0.01)
            .alphaDecay(0.03)
            .velocityDecay(0.5)
            .stop()

        for (let i = 0; i < 600; i++) {
            simulation.tick()
        }

        for (const simNode of simNodes) {
            positions.set(simNode.id, {
                x: simNode.x ?? 0,
                y: simNode.y ?? 0,
                depth: 0,
                isCore: true
            })
        }
    }

    private layoutLeaves(
        leafNodes: GraphLayoutNode[],
        edges: GraphLayoutEdge[],
        coreSet: Set<string>,
        positions: Map<string, HybridPosition>,
        root: string
    ) {
        const adjacency = new Map<string, string[]>()
        for (const edge of edges) {
            if (edge.source === edge.target) {
                continue
            }
            if (!adjacency.has(edge.source)) {
                adjacency.set(edge.source, [])
            }
            if (!adjacency.has(edge.target)) {
                adjacency.set(edge.target, [])
            }
            adjacency.get(edge.source)!.push(edge.target)
            adjacency.get(edge.target)!.push(edge.source)
        }

        const coreAttachments = new Map<string, string[]>()
        for (const leaf of leafNodes) {
            const neighbors = adjacency.get(leaf.id) ?? []
            const coreNeighbors = neighbors.filter(n => coreSet.has(n))
            if (coreNeighbors.length > 0) {
                coreAttachments.set(leaf.id, coreNeighbors)
            } else {
                coreAttachments.set(leaf.id, [root])
            }
        }

        const coreEdgeAngles = new Map<string, number[]>()
        for (const edge of edges) {
            if (edge.source === edge.target) {
                continue
            }
            if (coreSet.has(edge.source) && coreSet.has(edge.target)) {
                const srcPos = positions.get(edge.source)
                const tgtPos = positions.get(edge.target)
                if (srcPos && tgtPos) {
                    const angle = Math.atan2(tgtPos.y - srcPos.y, tgtPos.x - srcPos.x)
                    if (!coreEdgeAngles.has(edge.source)) {
                        coreEdgeAngles.set(edge.source, [])
                    }
                    if (!coreEdgeAngles.has(edge.target)) {
                        coreEdgeAngles.set(edge.target, [])
                    }
                    coreEdgeAngles.get(edge.source)!.push(angle)
                    coreEdgeAngles.get(edge.target)!.push(angle + Math.PI)
                }
            }
        }

        const attachmentCounts = new Map<string, number>()

        for (const leaf of leafNodes) {
            const coreNeighbors = coreAttachments.get(leaf.id) ?? [root]
            let bestCore = coreNeighbors[0]
            let bestCount = Infinity
            for (const cn of coreNeighbors) {
                const count = attachmentCounts.get(cn) ?? 0
                if (count < bestCount) {
                    bestCore = cn
                    bestCount = count
                }
            }

            const corePos = positions.get(bestCore)
            if (!corePos) {
                positions.set(leaf.id, { x: 0, y: 0, depth: 1, isCore: false })
                continue
            }

            const count = attachmentCounts.get(bestCore) ?? 0
            const coreAngleFromOrigin = Math.atan2(corePos.y, corePos.x)
            const existingAngles = coreEdgeAngles.get(bestCore) ?? []

            let angle: number
            if (existingAngles.length === 0) {
                angle = coreAngleFromOrigin + count * 0.5
            } else {
                const gaps: Array<{ start: number, end: number, size: number }> = []
                const sorted = [...existingAngles].sort((a, b) => a - b)
                for (let i = 0; i < sorted.length; i++) {
                    const next = i + 1 < sorted.length ? sorted[i + 1] : sorted[0] + Math.PI * 2
                    gaps.push({ start: sorted[i], end: next, size: next - sorted[i] })
                }
                gaps.sort((a, b) => b.size - a.size)
                const gap = gaps[Math.min(count, gaps.length - 1)]
                angle = gap.start + gap.size * 0.5
            }

            const dist = 85
            positions.set(leaf.id, {
                x: corePos.x + Math.cos(angle) * dist,
                y: corePos.y + Math.sin(angle) * dist,
                depth: 1,
                isCore: false
            })

            attachmentCounts.set(bestCore, count + 1)
        }
    }

    private buildForceConfig(
        _nodes: GraphLayoutNode[],
        edges: GraphLayoutEdge[],
        coreSet: Set<string>,
        positions: Map<string, HybridPosition>,
        nodeLabelSize: Map<string, { width: number, height: number }>,
        root: string
    ): ForceConfig {
        const primaryCore = new Map<string, string>()
        for (const edge of edges) {
            if (edge.source === edge.target) {
                continue
            }
            if (coreSet.has(edge.source) && !coreSet.has(edge.target)) {
                if (!primaryCore.has(edge.target)) {
                    primaryCore.set(edge.target, edge.source)
                }
            }
            if (coreSet.has(edge.target) && !coreSet.has(edge.source)) {
                if (!primaryCore.has(edge.source)) {
                    primaryCore.set(edge.source, edge.target)
                }
            }
        }

        const hasLeafEdges = edges.some(e => !coreSet.has(e.source) || !coreSet.has(e.target))

        return {
            linkDistance: (source, target) => {
                const sourceCore = coreSet.has(source.id)
                const targetCore = coreSet.has(target.id)
                if (sourceCore && targetCore) {
                    return 60
                }
                if (sourceCore || targetCore) {
                    return 85
                }
                return 120
            },
            linkStrength: 0.4,
            chargeStrength: -80,
            collideRadius: (node) => {
                const size = nodeLabelSize.get(node.id)
                const nodeRadius = node.id === root ? 7 : 4
                const labelH = size?.height ?? 14
                const labelW = size?.width ?? 50
                return nodeRadius + labelH + Math.sqrt(labelW) + 4
            },
            collideIterations: 2,
            radialForce: hasLeafEdges ? (node) => {
                if (coreSet.has(node.id)) {
                    const pos = positions.get(node.id)
                    return pos ? Math.hypot(pos.x, pos.y) : 0
                }
                const coreId = primaryCore.get(node.id)
                if (coreId) {
                    const corePos = positions.get(coreId)
                    return corePos ? Math.hypot(corePos.x, corePos.y) + 85 : 85
                }
                return 85
            } : null,
            radialStrength: (node) => {
                if (node.id === root) {
                    return 1
                }
                if (coreSet.has(node.id)) {
                    return 0
                }
                return 0.8
            },
            alpha: 1.2,
            alphaMin: 0.03,
            alphaDecay: 0.055,
            velocityDecay: 0.5
        }
    }
}

registerLayoutEngine('hybrid', () => new HybridLayoutEngine())
