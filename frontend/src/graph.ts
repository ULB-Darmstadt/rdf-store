import { css, html, LitElement, type PropertyValues } from 'lit'
import { customElement, property, query } from 'lit/decorators.js'
import  * as d3 from 'd3'
import { type D3DragEvent, type Simulation, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3'
import { RDF_TYPE } from './constants'
import { fetchLabels, i18n } from './i18n'
import { Parser, Quad } from 'n3'

type Node = SimulationNodeDatum & {
    id: string
    label?: string
    type?: string
    properties: Record<string, string[]>
} 

type Edge = SimulationLinkDatum<Node> & {
    type: string
    label?: string
}

const width = 400
const height = 400

@customElement('rdf-graph')
export class RdfGraph extends LitElement {
    static styles = css`
        :host { position: relative; }
        svg { display: block; font-size: 12px; width: 100%; user-select: none; }
        .type { font-size: 0.7em; color: #888; }
        .node.root circle { fill: black; }
        .node .pulse-ring { fill: none; stroke: currentColor; stroke-width: 6; opacity: 0; pointer-events: none; }
        .node:hover circle { stroke-width: 7; stroke: color-mix(in srgb, currentColor 20%, transparent); }
        .node.highlight .pulse-ring { animation: pulseRing 1.5s ease-out 2 forwards; }
        .link-labels, .node-type { visibility: hidden; }
        svg:hover { .link-labels, .node-type { visibility: visible; } }

        @keyframes pulseRing {
            0%   { transform: scale(1);   opacity: 0.9; }
            70%  { transform: scale(2.0); opacity: 0.0; }
            100% { transform: scale(2.0); opacity: 0.0; }
        }

        #info-pane {
            position: absolute;
            right: 8px;
            top: 8px;
            width: 320px;
            max-height: 60%;
            overflow: auto;
            background-color: white;
            border-radius: 3px;
            padding: 14px;
            box-shadow: 0 10px 20px #0007;
            opacity: 0;
            h4 { margin: 0 0 8px; font-size: 13px; }
            dt { font-size: 12px; color: #888; margin-top: 6px; }
            dd { margin: 0; font-size: 12px; }
        }
        #info-pane:not(.pinned) { pointer-events: none; }
    `

    @property()
    rdfSubject = ''
    @property()
    highlightSubject = ''
    @property()
    rdf = ''

    quads: Quad[] = []

    @query('#info-pane')
    infopane!: HTMLElement

    showInfoPane = (node: Node, pinned: boolean) => {
        const pane = this.infopane
        pane.replaceChildren()

        const title = document.createElement('h4')
        title.textContent = i18n[node.id] || node.id
        pane.appendChild(title)

        const list = document.createElement('dl')
        if (i18n[node.id]) {
            const dt = document.createElement('dt')
            dt.textContent = 'ID'
            const dd = document.createElement('dd')
            dd.textContent = node.id
            list.append(dt, dd)
        }
        for (const [key, values] of Object.entries(node.properties)) {
            for (const value of values) {
            const dt = document.createElement('dt')
            dt.textContent = i18n[key] || key
            const dd = document.createElement('dd')
            dd.textContent = i18n[value] || value
            list.append(dt, dd)
            }
        }
        pane.appendChild(list)

        d3.select(pane).transition().style('opacity', 1)
        this.infopane.classList.toggle('pinned', pinned)
    }

    hideInfoPane = (_?: Event, force: boolean = true) => {
        if (force || !this.infopane.classList.contains('pinned')) {
            d3.select(this.infopane).transition().style('opacity', 0)
            this.infopane.classList.remove('pinned')
        }
    }
    
    keyListener = (event: KeyboardEvent) => { if (event.key === 'Escape') { this.hideInfoPane(undefined) }}

    updated(pv: PropertyValues) {
        if (pv.has('rdfSubject') || pv.has('highlightSubject') || pv.has('rdf')) {
            if (pv.has('rdf')) {
                this.quads = new Parser().parse(this.rdf)
            }
            this.hideInfoPane(undefined);
            (async () => {
                const graph = await this.buildGraph()
                this.shadowRoot!.querySelector('#mount')?.replaceChildren(graph)
                requestAnimationFrame(() => {
                    fitToView(graph)
                })
            })()
        }
    }

    firstUpdated() {
        window.addEventListener('keydown', this.keyListener)
        this.addEventListener('click', this.hideInfoPane)
        this.infopane.addEventListener('click', event => { event.stopPropagation() })
    }

    disconnectedCallback() {
        super.disconnectedCallback()
        window.removeEventListener('keydown', this.keyListener)
        this.removeEventListener('click', this.hideInfoPane)
    }

    async buildGraph() {
        const labelsToFetch = new Set<string>()
        const nodes: Record<string, Node> = {}
        const links: Edge[] = []
        const subjects = new Set(this.quads.map(q => q.subject.value))
        for (const q of this.quads) {
            labelsToFetch.add(q.subject.value)
            labelsToFetch.add(q.predicate.value)
            // if object is not a literal, try to fetch label for it
            if (q.object.termType === 'NamedNode') {
                labelsToFetch.add(q.object.value)
            }
            let node = nodes[q.subject.value]
            if (!node) {
                node = { id: q.subject.value, properties: {} }
                nodes[q.subject.value] = node
            }
            // check if quad object is also a subject and if so, create a link
            if (subjects.has(q.object.value)) {
                links.push({ 'source': q.subject.value, 'target': q.object.value, 'type': q.predicate.value })
            } else {
                if (RDF_TYPE.value === q.predicate.value) {
                    node.type = q.object.value
                    labelsToFetch.add(q.object.value)
                } else {
                    (node.properties[q.predicate.value] ??= []).push(q.object.value)
                }
            }
        }
        await fetchLabels(Array.from(labelsToFetch), true)

        const nodeArray = Object.values(nodes)
        for (const node of nodeArray) {
            node.label = i18n[node.id]
            if (node.type) {
                let typeLabel = i18n[node.type] || node.type
                if (node.label) {
                    node.label += ` <tspan class="type node-type">&lt;${typeLabel}&gt;</tspan>`
                } else {
                    node.label = `<tspan class="type">&lt;${typeLabel}&gt;</tspan>`
                }
            }
        }
        for (const link of links) {
            link.label = i18n[link.type]
        }

        const types = Array.from(new Set(links.map(d => d.type)))
        const color = d3.scaleOrdinal(types, d3.schemeCategory10)
        const linkId = (_: Edge, i: number) => `link-path-${i}`

        const simulation = d3.forceSimulation<Node, Edge>(nodeArray)
            .force("link", d3.forceLink<Node, Edge>(links).id(d => d.id))
            .force("charge", d3.forceManyBody().strength(-1200))
            .force("collide", d3.forceCollide<Node>().radius(18).iterations(2))
            .force("x", d3.forceX())
            .force("y", d3.forceY())
            .alpha(1)
            .alphaMin(0.02)
            .alphaDecay(0.08) 
            .velocityDecay(0.6)
        
        const svg = d3.create("svg").attr("viewBox", `${-width/2} ${-height/2} ${width} ${height}`)
        const scene = svg.append("g").attr("id", "scene")
        const zoom = d3.zoom<SVGSVGElement, unknown>().scaleExtent([0.3, 1.3]).on("zoom", event => scene.attr("transform", event.transform))
        svg.call(zoom as any)

        // arrow heads
        svg.append("defs").selectAll("marker")
            .data(types)
            .join("marker")
            .attr("id", d => `arrow-${d}`)
            // .attr("id", d => markerId(d))
            .attr("viewBox", "0 -5 10 10")
            .attr("refX", 11)
            .attr("refY", -1)
            .attr("markerWidth", 6)
            .attr("markerHeight", 6)
            .attr("orient", "auto")
            .attr("stroke", "var(--background-color, white)")
            .attr("stroke-width", 2)
            .append("path")
            .attr("fill", color)
            .attr("d", "M0,-5L10,0L0,5")

        // links
        const link = scene.append("g")
            .attr("fill", "none")
            .attr("stroke-width", 2)
            .attr("class", "links")
            .selectAll("path")
            .data(links)
            .join("path")
            .attr("id", linkId)
            .attr("stroke", d => color(d.type))
            .attr("marker-end", d => `url(${new URL(`#arrow-${d.type}`, location.toString())})`)

        // link labels
        scene.append("g")
            .attr("class", "link-labels")
            .selectAll("text")
            .data(links)
            .join("text")
            .attr("font-size", 7)
            .attr("dy", "-0.3em")
            .append("textPath")
            .attr("fill", d => color(d.type))
            .attr("href", (_, i) => `#${linkId(_, i)}`)
            .attr("startOffset", "45%")
            .attr("text-anchor", "middle")
            .text(d => d.label || d.type)

        // nodes
        const node = scene.append("g")
            .attr("fill", "#888")
            .selectAll("g")
            .data(nodeArray)
            .join("g")
            .attr("class", d => `node${d.id === this.rdfSubject ? ' root' : ''}${d.id === this.highlightSubject ? ' highlight' : ''}`)
            .call(drag(simulation) as any, undefined)

        // pulse ring for highlighing animation
        node.append("circle")
            .attr("class", "pulse-ring")
            .attr("r", 4)

        node.append("circle")
            .attr("stroke", "var(--background-color, white)")
            .attr("stroke-width", 0.5)
            .attr("r", d => d.id === this.rdfSubject ? 7 : 4) // make root node larger

        // labels
        node.append("text")
            .attr("x", 8)
            .attr("y", "0.31em")
            .html(d => d.label ?? d.id)
            .clone(true).lower()
            .attr("fill", "none")
            .attr("stroke", "var(--background-color, white)")
            .attr("stroke-width", 1)

        node.on("mouseenter", (_, d: Node) => {
            if (!this.infopane.classList.contains('pinned')) {
                this.showInfoPane(d, false)
            }
        })
        node.on("mouseleave", () => {
            this.hideInfoPane(undefined, false)
        })
        node.on("click", (e, d: Node) => {
            e.stopPropagation()
            this.showInfoPane(d, true)
        })
        node.on("pointerdown", (e) => e.stopPropagation())

        simulation.on("tick", () => {
            link.attr("d", linkArc)
            node.attr("transform", d => `translate(${d.x},${d.y})`)
        })

        // pre-tick to advance simulation to stable state
        for (let i = 0; i < 200; i++) {
            simulation.tick()
        }
        
        return Object.assign(svg.node()!, { scales: { color }, zoomBehaviour: zoom })
    }

    render() {
        return html`
            <div id="mount"></div>
            <div id="info-pane"></div>
        `
    }
}


function fitToView(svg: SVGSVGElement) {
    const scene = svg.querySelector<SVGGElement>('#scene')
    const zoom = (svg as any)['zoomBehaviour'] as d3.ZoomBehavior<SVGSVGElement, unknown>
    if (scene && zoom) {
        const bbox = scene.getBBox()
        if (!bbox.width || !bbox.height) {
            return
        }
        const pad = 20
        const unclampedScale = Math.min((width - 2 * pad) / bbox.width, (height - 2 * pad) / bbox.height)
        const scale = Math.min(unclampedScale, 1)
        const transform = d3.zoomIdentity
            .translate(0, pad - height / 2)
            .scale(scale)
            .translate(-(bbox.x + bbox.width / 2), -bbox.y)

        d3.select(svg).call(zoom.transform as any, transform)
    }
}

function linkArc(d: Edge) {
    const s = d.source as Node
    const t = d.target as Node
    const r = Math.hypot(t.x! - s.x!, t.y! - s.y!)
    return `
    M${s.x!},${s.y!}
    A${r},${r} 0 0,1 ${t.x!},${t.y!}
  `
}

const drag = (simulation: Simulation<Node, Edge>) => {
    let startX = 0, startY = 0;

    function dragstarted(event: D3DragEvent<Element, undefined, undefined>, d: Node) {
        const se = event.sourceEvent as PointerEvent | MouseEvent | undefined;
        startX = se?.clientX ?? 0;
        startY = se?.clientY ?? 0;
        d.fx = d.x;
        d.fy = d.y;
    }

    function dragged(event: D3DragEvent<Element, undefined, undefined>, d: Node) {
        const se = event.sourceEvent as PointerEvent | MouseEvent | undefined;
        const dx = (se?.clientX ?? 0) - startX;
        const dy = (se?.clientY ?? 0) - startY;

        // only “wake” simulation after a tiny move
        if (Math.hypot(dx, dy) > 2) {
            simulation.alphaTarget(0.3).restart();
        }

        d.fx = event.x;
        d.fy = event.y;
    }

    function dragended(event: D3DragEvent<Element, undefined, undefined>, d: Node) {
        if (!event.active) simulation.alphaTarget(0)
        simulation.stop()
        d.fx = null
        d.fy = null
    }

    return d3.drag().on("start", dragstarted as any).on("drag", dragged as any).on("end", dragended as any)
}
