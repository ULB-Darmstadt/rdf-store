import { RokitSnackbar, showSnackbarMessage } from '@ro-kit/ui-widgets'
import { css, html, LitElement, type PropertyValues } from 'lit'
import { customElement, property, query, state } from 'lit/decorators.js'
import  * as d3 from 'd3'
import { type D3DragEvent, type Simulation, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3'
import { BACKEND_URL, RDF_TYPE } from './constants'
import { fetchLabels, i18n } from './i18n'

type Node = SimulationNodeDatum & {
    id: string
    label?: string
    type?: string
    properties: Record<string, string>
} 

type Edge = SimulationLinkDatum<Node> & {
    type: string
    label?: string
}

interface Quad {
    s: { value: string, type: string }
    p: { value: string, type: string }
    o: { value: string, type: string }
    g: { value: string, type: string }
}

const width = 400
const height = 400

@customElement('rdf-graph')
export class RdfGraph extends LitElement {
    static styles = css`
        svg { display: block; font-size: 12px; width: 100%; user-select: none; }
        .wrapper { position: relative; height: 100%; }
        .info-pane {
            position: fixed;
            bottom: 10px; right: 10px;
            background-color: #000C;
            border: none;
            border-radius: 5px;
            padding: 15px;
            min-width: 400px;
            text-align: left;
            color: white;
            pointer-events: none;
        }
        .type { font-size: 0.7em; color: #888; }
        .node.root circle { fill: black; }
        .node.highlight:not(:hover) circle { fill: #FAA; stroke: #A33; stroke-width: 1; stroke-dasharray: 2.512; animation: strokes 1s linear infinite; }
        .node:hover circle { stroke-width: 7; stroke: color-mix(in srgb, currentColor 20%, transparent); }
        .link-labels, .node-type { visibility: hidden; }
        svg:hover { .link-labels, .node-type { visibility: visible; } }
        @keyframes strokes { 100%  { stroke-dashoffset: 5.024; }}
    `

    @property()
    rdfSubject = ''

    @property()
    highlightSubject = ''

    @state()
    graph?: SVGSVGElement

    @query('.wrapper')
    wrapper!: HTMLElement

    updated(pv: PropertyValues) {
        if (pv.has('rdfSubject') || pv.has('highlightSubject')) {
            this.executeQuery()
        }
        if (pv.has('graph') && this.graph) {
            this.updateComplete.then(() => {
                requestAnimationFrame(() => {
                    if (this.graph) {
                        fitToView(this.graph)
                    }
                })
            })
        }
    }

    async executeQuery() {
        if (this.rdfSubject) {
            try {
                const url = `${BACKEND_URL}/sparql/query`
                const body = new URLSearchParams()
                body.append('query', `SELECT * WHERE { VALUES ?g { <${this.rdfSubject}> } GRAPH ?g { ?s ?p ?o }}`)
                const resp = await fetch(url, {
                    method: 'POST',
                    headers: { 'Accept': 'application/sparql-results+json' },
                    body: body
                })
                if (resp.status !== 200) {
                    throw new Error('server returned status ' + resp.status)
                }
                const data = await resp.json()
                if (data?.results?.bindings?.length) {
                    this.graph = await this.buildGraph(data.results.bindings)
                } else {
                    throw new Error('no results')
                }
            } catch (e) {
                showSnackbarMessage({ message: '' + e, ttl: 0, cssClass: 'error' }, this.shadowRoot!.querySelector<RokitSnackbar>('rokit-snackbar') || undefined)
            }
        }
    }

    async buildGraph(quads: Quad[]) {
        const labelsToFetch = new Set<string>()
        const nodes: Record<string, Node> = {}
        const links: Edge[] = []
        const subjects = new Set(quads.map(q => q.s.value))
        for (const q of quads) {
            labelsToFetch.add(q.s.value)
            labelsToFetch.add(q.p.value)
            // if object is not a literal, try to fetch label for it
            if (q.o.type === 'uri') {
                labelsToFetch.add(q.o.value)
            }
            let node = nodes[q.s.value]
            if (!node) {
                node = { id: q.s.value, properties: {} }
                nodes[q.s.value] = node
            }
            // check if quad object is also a subject and if so, create a link
            if (subjects.has(q.o.value)) {
                links.push({ 'source': q.s.value, 'target': q.o.value, 'type': q.p.value })
            } else {
                if (RDF_TYPE.value === q.p.value) {
                    node.type = q.o.value
                    labelsToFetch.add(q.o.value)
                } else {
                    node.properties[q.p.value] = q.o.value
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
            .force("charge", d3.forceManyBody().strength(-1500))
            .force("x", d3.forceX())
            .force("y", d3.forceY())
            .alpha(1)
            .alphaMin(0.02)
            .alphaDecay(0.08) 
            .velocityDecay(0.6)
        
        const svg = d3.create("svg").attr("viewBox", `${-width/2} ${-height/2} ${width} ${height}`)
        const scene = svg.append("g").attr("id", "scene")
        const zoom = d3.zoom<SVGSVGElement, unknown>().scaleExtent([0.05, 8]).on("zoom", event => scene.attr("transform", event.transform))
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
            // .attr("stroke-linecap", "round")
            // .attr("stroke-linejoin", "round")
            .selectAll("g")
            .data(nodeArray)
            .join("g")
            .attr("class", d => `node${d.id === this.rdfSubject ? ' root' : ''}${d.id === this.highlightSubject ? ' highlight' : ''}`)
            .call(drag(simulation) as any, undefined)

        node.append("circle")
            .attr("stroke", "var(--background-color, white)")
            .attr("stroke-width", 0.5)
            .attr("r", 4)

        // labels
        node.append("text")
            .attr("x", 8)
            .attr("y", "0.31em")
            .html(d => d.label ?? d.id)
            .clone(true).lower()
            .attr("fill", "none")
            .attr("stroke", "var(--background-color, white)")
            .attr("stroke-width", 1)

        const infoPane = d3.select(this.wrapper).append("div").style("opacity", 0).attr("class", "info-pane")

        node.on('mouseover', (_, d: Node) => {
            let content = `<table><tr class="type"><td>ID</td><td>${d.id}</td></tr>`
            for (const [key, value] of Object.entries(d.properties)) {
                content += `<tr><td><b>${i18n[key] || key}:</b></td><td>${i18n[value] || value}</td></tr>`
            }
            content += '</table>'
            infoPane.html(content).transition().duration(50).style("opacity", 1)
        })
        node.on('mouseleave', () => { infoPane.transition().duration(50).style("opacity", 0) })

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
        <div class="wrapper">
            ${this.graph}
        </div>`
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
        const scale = Math.min((width - 2 * pad) / bbox.width, (height - 2 * pad) / bbox.height)
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
    function dragstarted(event: D3DragEvent<Element, undefined, undefined>, d: Node) {
        if (!event.active) simulation.alphaTarget(0.3).restart()
        d.fx = d.x
        d.fy = d.y
    }

    function dragged(event: D3DragEvent<Element, undefined, undefined>, d: Node) {
        d.fx = event.x
        d.fy = event.y
    }

    function dragended(event: D3DragEvent<Element, undefined, undefined>, d: Node) {
        if (!event.active) simulation.alphaTarget(0)
        d.fx = null
        d.fy = null
    }

    return d3.drag().on("start", dragstarted as any).on("drag", dragged as any).on("end", dragended as any)
}
