import * as d3 from 'd3'
import { type SimulationLinkDatum, type SimulationNodeDatum } from 'd3'
import {
    registerLayoutEngine, type ForceConfig, type GraphLayoutEdge, type GraphLayoutNode,
    type LayoutEngine, type LayoutResult
} from './graph-layout'

type SimNode = SimulationNodeDatum & { id: string }
type SimLink = SimulationLinkDatum<SimNode> & { id: string }

const PRE_TICKS = 50

const force: ForceConfig = {
    linkDistance: () => 30,
    chargeStrength: -1200,
    collideRadius: () => 18,
    collideIterations: 2,
    radialForce: null,
    radialStrength: () => 0,
    centerStrength: 0.1,
    alpha: 1.8,
    alphaMin: 0.2,
    alphaDecay: 0.08,
    velocityDecay: 0.6
}

export class ForceLayoutEngine implements LayoutEngine {
    compute(nodes: GraphLayoutNode[], edges: GraphLayoutEdge[], _root: string, seed: number): LayoutResult {
        const simNodes: SimNode[] = nodes.map(node => ({ id: node.id }))
        const simLinks: SimLink[] = edges.map(edge => ({ id: edge.id, source: edge.source, target: edge.target }))
        const simulation = d3.forceSimulation<SimNode, SimLink>(simNodes)
            .randomSource(d3.randomLcg(seed))
            .force('link', d3.forceLink<SimNode, SimLink>(simLinks).id(node => node.id))
            .force('charge', d3.forceManyBody().strength(force.chargeStrength))
            .force('collide', d3.forceCollide<SimNode>().radius(force.collideRadius).iterations(force.collideIterations))
            .force('x', d3.forceX<SimNode>().strength(force.centerStrength!))
            .force('y', d3.forceY<SimNode>().strength(force.centerStrength!))
            .alpha(force.alpha).alphaMin(force.alphaMin).alphaDecay(force.alphaDecay).velocityDecay(force.velocityDecay)
        simulation.stop()
        for (let index = 0; index < PRE_TICKS; index++) {
            simulation.tick()
        }
        const positions = new Map(simNodes.map(node => [node.id, { x: node.x ?? 0, y: node.y ?? 0 }]))
        return { positions, force }
    }
}

registerLayoutEngine('force', () => new ForceLayoutEngine())
