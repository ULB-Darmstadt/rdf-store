import {
    estimateLabelSize, registerLayoutEngine, type ForceConfig,
    type GraphLayoutEdge, type GraphLayoutNode, type LayoutEngine, type LayoutResult,
    type LayoutPosition
} from './graph-layout'

export type HierarchicalPosition = LayoutPosition & { layer: number, index: number }

export type HierarchicalResult = LayoutResult<HierarchicalPosition> & {
    force: ForceConfig
    layers: Map<number, string[]>
}

export function computeLayers(
    nodeIds: string[],
    edges: Pick<GraphLayoutEdge, 'source' | 'target'>[],
    root: string
): Map<string, number> {
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

    return layers
}

export function minimizeCrossings(
    layers: Map<number, string[]>,
    edges: Pick<GraphLayoutEdge, 'source' | 'target'>[],
    iterations = 8
): Map<number, string[]> {
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

    const result = new Map<number, string[]>()
    for (const [layer, nodes] of layers) {
        result.set(layer, [...nodes])
    }

    const sortedLayers = Array.from(result.keys()).sort((a, b) => a - b)

    for (let iter = 0; iter < iterations; iter++) {
        for (let i = 1; i < sortedLayers.length; i++) {
            const layer = sortedLayers[i]
            const prevLayer = sortedLayers[i - 1]
            const prevOrder = new Map(result.get(prevLayer)!.map((id, idx) => [id, idx]))
            const nodes = result.get(layer)!
            const targets = nodes.map(id => {
                const neighbors = (adjacency.get(id) ?? []).filter(n => prevOrder.has(n))
                if (neighbors.length === 0) {
                    return { id, target: Infinity }
                }
                const avg = neighbors.reduce((sum, n) => sum + (prevOrder.get(n) ?? 0), 0) / neighbors.length
                return { id, target: avg }
            })
            targets.sort((a, b) => {
                if (a.target === Infinity && b.target === Infinity) {
                    return a.id.localeCompare(b.id)
                }
                if (a.target === Infinity) {
                    return 1
                }
                if (b.target === Infinity) {
                    return -1
                }
                return a.target - b.target
            })
            result.set(layer, targets.map(t => t.id))
        }

        for (let i = sortedLayers.length - 2; i >= 0; i--) {
            const layer = sortedLayers[i]
            const nextLayer = sortedLayers[i + 1]
            const nextOrder = new Map(result.get(nextLayer)!.map((id, idx) => [id, idx]))
            const nodes = result.get(layer)!
            const targets = nodes.map(id => {
                const neighbors = (adjacency.get(id) ?? []).filter(n => nextOrder.has(n))
                if (neighbors.length === 0) {
                    return { id, target: Infinity }
                }
                const avg = neighbors.reduce((sum, n) => sum + (nextOrder.get(n) ?? 0), 0) / neighbors.length
                return { id, target: avg }
            })
            targets.sort((a, b) => {
                if (a.target === Infinity && b.target === Infinity) {
                    return a.id.localeCompare(b.id)
                }
                if (a.target === Infinity) {
                    return 1
                }
                if (b.target === Infinity) {
                    return -1
                }
                return a.target - b.target
            })
            result.set(layer, targets.map(t => t.id))
        }
    }

    return result
}

export class HierarchicalLayoutEngine implements LayoutEngine {
    compute(nodes: GraphLayoutNode[], edges: GraphLayoutEdge[], root: string, _seed: number): HierarchicalResult {
        console.log('--- hierarchical')
        const nodeIds = nodes.map(n => n.id)
        const nonSelfEdges = edges.filter(e => e.source !== e.target)

        const nodeLayer = computeLayers(nodeIds, nonSelfEdges, root)
        const layerGroups = new Map<number, string[]>()
        for (const [id, layer] of nodeLayer) {
            if (!layerGroups.has(layer)) {
                layerGroups.set(layer, [])
            }
            layerGroups.get(layer)!.push(id)
        }

        const orderedLayers = minimizeCrossings(layerGroups, nonSelfEdges)

        const nodeLabelSize = new Map<string, { width: number, height: number }>()
        for (const node of nodes) {
            nodeLabelSize.set(node.id, estimateLabelSize(node.label))
        }

        const LAYER_GAP = 120
        const positions = new Map<string, HierarchicalPosition>()

        const sortedLayerKeys = Array.from(orderedLayers.keys()).sort((a, b) => a - b)

        for (const layer of sortedLayerKeys) {
            const nodeIds = orderedLayers.get(layer)!
            const totalWidth = nodeIds.reduce((sum, id) => {
                const size = nodeLabelSize.get(id)
                return sum + (size?.width ?? 50) + 16
            }, 0)

            let x = -totalWidth / 2
            const y = layer * LAYER_GAP

            for (let i = 0; i < nodeIds.length; i++) {
                const id = nodeIds[i]
                const size = nodeLabelSize.get(id)
                const nodeWidth = (size?.width ?? 50) + 16
                positions.set(id, {
                    x: x + nodeWidth / 2,
                    y,
                    layer,
                    index: i
                })
                x += nodeWidth
            }
        }

        return {
            positions,
            layers: orderedLayers,
            force: this.buildForceConfig(nodes, nonSelfEdges, nodeLayer, positions, nodeLabelSize, root)
        }
    }

    private buildForceConfig(
        _nodes: GraphLayoutNode[],
        _edges: GraphLayoutEdge[],
        nodeLayer: Map<string, number>,
        positions: Map<string, HierarchicalPosition>,
        nodeLabelSize: Map<string, { width: number, height: number }>,
        root: string
    ): ForceConfig {
        const LAYER_GAP = 120

        return {
            linkDistance: (source, target) => {
                const srcLayer = nodeLayer.get(source.id) ?? 0
                const tgtLayer = nodeLayer.get(target.id) ?? 0
                const layerDiff = Math.abs(tgtLayer - srcLayer)
                return Math.max(LAYER_GAP * 0.8, layerDiff * LAYER_GAP)
            },
            linkStrength: 0.6,
            chargeStrength: -150,
            collideRadius: (node) => {
                const size = nodeLabelSize.get(node.id)
                const nodeRadius = node.id === root ? 7 : 4
                const labelH = size?.height ?? 14
                const labelW = size?.width ?? 50
                return nodeRadius + labelH + Math.sqrt(labelW) + 4
            },
            collideIterations: 2,
            radialForce: (node) => {
                const pos = positions.get(node.id)
                return pos ? Math.abs(pos.y) : 0
            },
            radialStrength: (node) => {
                if (node.id === root) {
                    return 1
                }
                return 0.9
            },
            alpha: 1.2,
            alphaMin: 0.03,
            alphaDecay: 0.055,
            velocityDecay: 0.5
        }
    }
}

registerLayoutEngine('hierarchical', () => new HierarchicalLayoutEngine())
