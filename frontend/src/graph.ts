import { css, html, LitElement, nothing, type PropertyValues } from 'lit'
import { customElement, property, query, state } from 'lit/decorators.js'
import * as d3 from 'd3'
import { type D3DragEvent, type Simulation, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3'
import { Parser, Quad } from 'n3'
import { BACKEND_URL, RDF_TYPE } from './constants'
import { fetchLabels, i18n } from './i18n'
import { mergeQuads, nodeId, serializeNQuads } from './graph-model'
import { globalStyles } from './styles'

type Node = SimulationNodeDatum & {
    id: string
    label?: string
    type?: string
    navigable: boolean
    properties: Record<string, string[]>
}

type Edge = SimulationLinkDatum<Node> & {
    type: string
    label?: string
}

type IncomingProgress = { offset: number, total: number }

const width = 400
const height = 400
const incomingBatchSize = 25

@customElement('rdf-graph')
export class RdfGraph extends LitElement {
    static styles = [globalStyles, css`
        :host { position: relative; display: block; min-height: 0; overflow: hidden; }
        #mount { position: absolute; inset: 0; }
        svg { display: block; font-size: 12px; width: 100%; height: 100%; user-select: none; }
        .node { outline: none; cursor: pointer; }
        .node.stub circle { fill: #fff; stroke: #222; stroke-dasharray: 2 2; }
        .node.root circle.node-circle { fill: #222; }
        .node circle.node-circle { transition: r 120ms ease, stroke-width 120ms ease; }
        .node:hover circle.node-circle, .node:focus circle.node-circle { stroke-width: 7; stroke: color-mix(in srgb, currentColor 20%, transparent); }
        .node .new-ring { fill: none; stroke: var(--rokit-primary-color, #008877); stroke-width: 3; opacity: 0; pointer-events: none; }
        .node.new .new-ring { animation: pulseRing 1.2s ease-out 2 forwards; }
        .link-labels, .node-type { visibility: hidden; }
        svg:hover { .link-labels, .node-type { visibility: visible; } }

        @keyframes pulseRing {
            0% { transform: scale(1); opacity: 0.9; }
            75%, 100% { transform: scale(2.1); opacity: 0; }
        }

        .toolbar {
            position: absolute; z-index: 3; top: 10px; left: 10px;
            display: flex; align-items: center; gap: 6px; padding: 5px;
            background: color-mix(in srgb, var(--background-color, white) 94%, transparent);
            border: 1px solid #0002; border-radius: 10px; box-shadow: 0 4px 14px #0002;
        }
        .toolbar button, .radial-menu button {
            border: 0; border-radius: 8px; background: transparent; color: inherit; cursor: pointer;
            display: inline-flex; align-items: center; justify-content: center;
        }
        .toolbar button { width: 34px; height: 34px; }
        .toolbar button:hover, .toolbar button:focus-visible, .radial-menu button:hover, .radial-menu button:focus-visible {
            background: color-mix(in srgb, var(--rokit-primary-color, #008877) 14%, transparent);
            outline: 2px solid color-mix(in srgb, var(--rokit-primary-color, #008877) 45%, transparent);
        }
        .toolbar .counts { font-size: 11px; color: #666; white-space: nowrap; padding-right: 5px; }
        .material-icons { font-size: 20px; }

        .status {
            position: absolute; z-index: 3; left: 50%; bottom: 12px; transform: translateX(-50%);
            max-width: min(540px, calc(100% - 24px)); padding: 8px 12px; border-radius: 8px;
            background: #fff; box-shadow: 0 4px 16px #0003; font-size: 12px;
        }
        .status.error { color: #a40000; background: #fff2f2; }

        .radial-menu {
            position: absolute; z-index: 5; width: 190px; height: 190px;
            transform: translate(-50%, -50%); pointer-events: none;
        }
        .radial-menu button {
            position: absolute; pointer-events: auto; width: 74px; min-height: 54px; padding: 5px;
            flex-direction: column; gap: 2px; background: #fff; box-shadow: 0 4px 16px #0004;
            font: inherit; font-size: 10px; text-align: center;
        }
        .radial-menu button[disabled] { opacity: 0.5; cursor: default; }
        .radial-menu .incoming { left: 0; top: 20px; }
        .radial-menu .outgoing { right: 0; top: 20px; }
        .radial-menu .focus { left: 58px; bottom: 0; }
        .radial-menu .center {
            position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%);
            width: 54px; height: 54px; border-radius: 50%; background: #222; color: white;
            display: flex; align-items: center; justify-content: center; text-align: center;
            font-size: 9px; padding: 4px; box-sizing: border-box; overflow: hidden;
        }

        #info-pane {
            position: absolute; z-index: 2; right: 8px; top: 60px; width: min(320px, calc(100% - 32px));
            max-height: 55%; overflow: auto; background-color: white; border-radius: 8px; padding: 14px;
            box-shadow: 0 10px 20px #0004; opacity: 0; pointer-events: none;
        }
        #info-pane.pinned { pointer-events: auto; }
        #info-pane h4 { margin: 0 0 8px; font-size: 13px; }
        #info-pane dt { font-size: 12px; color: #888; margin-top: 6px; }
        #info-pane dd { margin: 0; font-size: 12px; overflow-wrap: anywhere; }
    `]

    @property() rdfSubject = ''
    @property() highlightSubject = ''

    @state() private loading = false
    @state() private error = ''
    @state() private menuSubject = ''
    @state() private menuX = 0
    @state() private menuY = 0
    @state() private nodeCount = 0
    @state() private edgeCount = 0
    @state() private actionLoading = new Set<string>()

    private menuPinned = false
    private menuCloseTimer?: number
    private menuOpenTimer?: number
    private suppressFit = false

    @query('#info-pane') private infopane!: HTMLElement
    @query('#mount') private mount!: HTMLElement

    private initialSubject = ''
    private activeSubject = ''
    private quads = new Map<string, Quad>()
    private outgoingLoaded = new Set<string>()
    private incomingProgress = new Map<string, IncomingProgress>()
    private positions = new Map<string, { x: number, y: number }>()
    private newNodes = new Set<string>()
    private requestEpoch = 0
    private drawVersion = 0
    private currentSvg?: SVGSVGElement

    updated(changed: PropertyValues) {
        if (changed.has('rdfSubject') || changed.has('highlightSubject')) {
            const subject = this.highlightSubject || this.rdfSubject
            if (subject && subject !== this.initialSubject) {
                this.initialSubject = subject
                void this.focusEntity(subject)
            } else if (!subject) {
                this.clearGraph()
            }
        }
    }

    firstUpdated() {
        this.addEventListener('click', this.onBackgroundClick)
        window.addEventListener('keydown', this.keyListener)
    }

    disconnectedCallback() {
        super.disconnectedCallback()
        this.removeEventListener('click', this.onBackgroundClick)
        window.removeEventListener('keydown', this.keyListener)
        this.requestEpoch++
    }

    exportNQuads() {
        return serializeNQuads(this.quads.values())
    }

    hasExportData() {
        return !this.loading && this.quads.size > 0
    }

    private notifyState() {
        this.dispatchEvent(new CustomEvent('graph-state-change', {
            detail: { loading: this.loading, hasData: this.quads.size > 0 },
            bubbles: true,
            composed: true
        }))
    }

    private clearGraph() {
        this.requestEpoch++
        this.drawVersion++
        this.activeSubject = ''
        this.quads.clear()
        this.outgoingLoaded.clear()
        this.incomingProgress.clear()
        this.positions.clear()
        this.closeMenu()
        this.clearMenuOpenTimer()
        this.loading = false
        this.error = ''
        this.nodeCount = 0
        this.edgeCount = 0
        this.mount?.replaceChildren()
        this.notifyState()
    }

    private async focusEntity(subject: string) {
        const epoch = ++this.requestEpoch
        this.drawVersion++
        this.activeSubject = subject
        this.quads.clear()
        this.outgoingLoaded.clear()
        this.incomingProgress.clear()
        this.positions.clear()
        this.newNodes.clear()
        this.closeMenu()
        this.clearMenuOpenTimer()
        this.error = ''
        this.loading = true
        this.nodeCount = 0
        this.edgeCount = 0
        this.mount?.replaceChildren()
        this.notifyState()
        await this.loadNeighborhood(subject, 'outgoing', 0, epoch)
        if (epoch === this.requestEpoch) {
            this.loading = false
            this.notifyState()
        }
    }

    private async reset() {
        if (this.initialSubject) {
            await this.focusEntity(this.initialSubject)
        }
    }

    private actionKey(subject: string, direction: string) {
        return `${direction}:${subject}`
    }

    private async loadIncoming(subject: string) {
        const progress = this.incomingProgress.get(subject) ?? { offset: 0, total: Number.POSITIVE_INFINITY }
        if (progress.offset >= progress.total) {
            return
        }
        await this.loadNeighborhood(subject, 'incoming', progress.offset, this.requestEpoch)
    }

    private async loadOutgoing(subject: string) {
        if (!this.outgoingLoaded.has(subject)) {
            await this.loadNeighborhood(subject, 'outgoing', 0, this.requestEpoch)
        }
    }

    private isSameDataset(id: string) {
        try {
            return new URL(id).host === new URL(this.activeSubject).host
        } catch {
            return false
        }
    }

    private async loadAllLinks() {
        const epoch = this.requestEpoch
        if (this.loading) {
            return
        }
        this.closeMenu()
        this.clearMenuOpenTimer()
        this.loading = true
        this.error = ''
        this.suppressFit = true
        this.notifyState()
        try {
            const expanded = new Set<string>()
            const queue: string[] = []

            const enqueue = (id: string) => {
                if (!expanded.has(id) && this.isSameDataset(id)) {
                    expanded.add(id)
                    queue.push(id)
                }
            }

            const enqueueNew = () => {
                for (const quad of this.quads.values()) {
                    if (quad.subject.termType === 'NamedNode') {
                        enqueue(nodeId(quad.subject, quad.graph.value))
                    }
                    if (quad.object.termType === 'NamedNode') {
                        enqueue(nodeId(quad.object, quad.graph.value))
                    }
                }
                enqueue(this.activeSubject)
            }

            enqueueNew()
            while (queue.length > 0 && epoch === this.requestEpoch) {
                const subject = queue.shift()!
                if (!this.outgoingLoaded.has(subject)) {
                    await this.loadNeighborhood(subject, 'outgoing', 0, epoch)
                    enqueueNew()
                }
                let offset = this.incomingProgress.get(subject)?.offset ?? 0
                let total = this.incomingProgress.get(subject)?.total ?? Number.POSITIVE_INFINITY
                while (epoch === this.requestEpoch && offset < total) {
                    await this.loadNeighborhood(subject, 'incoming', offset, epoch)
                    enqueueNew()
                    const progress = this.incomingProgress.get(subject)
                    if (!progress || progress.offset >= progress.total || progress.offset <= offset) {
                        break
                    }
                    offset = progress.offset
                    total = progress.total
                }
            }
        } finally {
            this.suppressFit = false
            if (epoch === this.requestEpoch) {
                this.loading = false
                this.fitGraph()
                this.notifyState()
            }
        }
    }

    private async loadNeighborhood(subject: string, direction: 'incoming' | 'outgoing', offset: number, epoch: number) {
        const key = this.actionKey(subject, direction)
        if (this.actionLoading.has(key)) {
            return
        }
        this.actionLoading = new Set(this.actionLoading).add(key)
        this.error = ''
        try {
            const params = new URLSearchParams({ subject, direction })
            if (direction === 'incoming') {
                params.set('offset', String(offset))
                params.set('limit', String(incomingBatchSize))
            }
            const response = await fetch(`${BACKEND_URL}/graph/neighborhood?${params}`)
            if (!response.ok) {
                const message = await response.json().then(body => body.error).catch(() => response.statusText)
                throw new Error(message || response.statusText)
            }
            const text = await response.text()
            if (epoch !== this.requestEpoch) {
                return
            }
            const parsed = new Parser({ format: 'N-Quads' }).parse(text)
            this.mergeQuads(parsed)
            if (direction === 'outgoing') {
                this.outgoingLoaded.add(subject)
            } else {
                const total = Number(response.headers.get('X-Total-Count') ?? 0)
                this.incomingProgress.set(subject, {
                    offset: Math.min(offset + incomingBatchSize, total),
                    total
                })
            }
            await this.drawGraph()
            this.notifyState()
        } catch (error) {
            if (epoch === this.requestEpoch) {
                this.error = `${i18n['graph_load_failed']}: ${error}`
            }
        } finally {
            const next = new Set(this.actionLoading)
            next.delete(key)
            this.actionLoading = next
        }
    }

    private mergeQuads(quads: Quad[]) {
        this.newNodes = mergeQuads(this.quads, quads)
    }

    private showInfoPane(node: Node, pinned: boolean) {
        const pane = this.infopane
        pane.replaceChildren()
        const title = document.createElement('h4')
        title.textContent = i18n[node.id] || node.id
        pane.appendChild(title)
        const list = document.createElement('dl')
        if (i18n[node.id]) {
            addDefinition(list, 'ID', node.id)
        }
        for (const [key, values] of Object.entries(node.properties)) {
            for (const value of values) {
                addDefinition(list, i18n[key] || key, i18n[value] || value)
            }
        }
        pane.appendChild(list)
        d3.select(pane).transition().style('opacity', 1)
        pane.classList.toggle('pinned', pinned)
    }

    private hideInfoPane(force = true) {
        if (force || !this.infopane.classList.contains('pinned')) {
            d3.select(this.infopane).transition().style('opacity', 0)
            this.infopane.classList.remove('pinned')
        }
    }

    private onBackgroundClick = (event: Event) => {
        if (event.target === this || event.target === this.mount || event.target === this.currentSvg) {
            this.closeMenu()
        }
    }

    private keyListener = (event: KeyboardEvent) => {
        if (event.key === 'Escape') {
            this.closeMenu()
        }
    }

    private clearMenuCloseTimer() {
        if (this.menuCloseTimer !== undefined) {
            clearTimeout(this.menuCloseTimer)
            this.menuCloseTimer = undefined
        }
    }

    private scheduleMenuClose() {
        this.clearMenuCloseTimer()
        this.menuCloseTimer = window.setTimeout(() => this.closeMenu(), 250)
    }

    private clearMenuOpenTimer() {
        if (this.menuOpenTimer !== undefined) {
            clearTimeout(this.menuOpenTimer)
            this.menuOpenTimer = undefined
        }
    }

    private scheduleMenuOpen(event: MouseEvent, node: Node, element: SVGGElement) {
        this.clearMenuOpenTimer()
        this.menuOpenTimer = window.setTimeout(() => {
            this.menuOpenTimer = undefined
            this.openMenu(event, node, element)
        }, 200)
    }

    private closeMenu() {
        this.clearMenuCloseTimer()
        this.menuSubject = ''
        this.menuPinned = false
        this.hideInfoPane()
    }

    private openMenu(event: MouseEvent | KeyboardEvent, node: Node, element: SVGGElement, pinned = false) {
        event.stopPropagation()
        if (!node.navigable) {
            return
        }
        const hostRect = this.getBoundingClientRect()
        const circle = element.querySelector<SVGCircleElement>('.node-circle')
        const rect = (circle ?? element).getBoundingClientRect()
        this.menuX = Math.max(95, Math.min(this.clientWidth - 95, rect.left + rect.width / 2 - hostRect.left))
        this.menuY = Math.max(95, Math.min(this.clientHeight - 95, rect.top + rect.height / 2 - hostRect.top))
        this.menuSubject = node.id
        this.menuPinned = pinned
        this.clearMenuCloseTimer()
        this.showInfoPane(node, pinned)
        if (event instanceof KeyboardEvent) {
            void this.updateComplete.then(() => this.shadowRoot?.querySelector<HTMLButtonElement>('.radial-menu button:not([disabled])')?.focus())
        }
    }

    private async drawGraph() {
        const version = ++this.drawVersion
        const graph = await this.buildGraph()
        if (version !== this.drawVersion) {
            return
        }
        this.mount.replaceChildren(graph)
        this.currentSvg = graph
        this.nodeCount = graph.nodeCount
        this.edgeCount = graph.edgeCount
        this.newNodes.clear()
        if (!this.suppressFit) {
            requestAnimationFrame(() => fitToView(graph))
        }
    }

    private async buildGraph() {
        const labelsToFetch = new Set<string>()
        const nodes = new Map<string, Node>()
        const links: Edge[] = []

        const ensureNode = (id: string, navigable: boolean) => {
            let node = nodes.get(id)
            if (!node) {
                const position = this.positions.get(id)
                node = { id, navigable, properties: {}, x: position?.x, y: position?.y }
                nodes.set(id, node)
            }
            return node
        }

        for (const quad of this.quads.values()) {
            const subjectId = nodeId(quad.subject, quad.graph.value)
            const subject = ensureNode(subjectId, quad.subject.termType === 'NamedNode')
            labelsToFetch.add(subject.id)
            labelsToFetch.add(quad.predicate.value)
            if (quad.predicate.value === RDF_TYPE.value && quad.object.termType === 'NamedNode') {
                subject.type = quad.object.value
                labelsToFetch.add(quad.object.value)
            } else if (quad.object.termType === 'Literal'
                || (quad.object.termType === 'NamedNode' && !this.isSameDataset(quad.object.value))) {
                (subject.properties[quad.predicate.value] ??= []).push(quad.object.value)
                if (quad.object.termType === 'NamedNode') {
                    labelsToFetch.add(quad.object.value)
                }
            } else if (quad.object.termType === 'NamedNode' || quad.object.termType === 'BlankNode') {
                const objectId = nodeId(quad.object, quad.graph.value)
                ensureNode(objectId, quad.object.termType === 'NamedNode')
                links.push({ source: subjectId, target: objectId, type: quad.predicate.value })
                if (quad.object.termType === 'NamedNode') {
                    labelsToFetch.add(objectId)
                }
            }
        }
        if (this.activeSubject && !nodes.has(this.activeSubject)) {
            ensureNode(this.activeSubject, true)
        }
        await fetchLabels(Array.from(labelsToFetch), true)

        const nodeArray = Array.from(nodes.values())
        const hydrated = new Set(Array.from(this.quads.values()).map(q => nodeId(q.subject, q.graph.value)))
        for (const node of nodeArray) {
            node.label = i18n[node.id]
            if (node.type) {
                const typeLabel = i18n[node.type] || node.type
                node.label = node.label
                    ? `${escapeHtml(node.label)} <tspan class="type node-type">&lt;${escapeHtml(typeLabel)}&gt;</tspan>`
                    : `<tspan class="type">&lt;${escapeHtml(typeLabel)}&gt;</tspan>`
            } else {
                node.label = escapeHtml(node.label || node.id)
            }
        }
        for (const link of links) {
            link.label = i18n[link.type]
        }

        const types = Array.from(new Set(links.map(link => link.type)))
        const color = d3.scaleOrdinal(types, d3.schemeTableau10)
        const simulation = d3.forceSimulation<Node, Edge>(nodeArray)
            .force('link', d3.forceLink<Node, Edge>(links).id(node => node.id).distance(65))
            .force('charge', d3.forceManyBody().strength(-850))
            .force('collide', d3.forceCollide<Node>().radius(22).iterations(2))
            .force('x', d3.forceX())
            .force('y', d3.forceY())
            .alpha(1.4).alphaMin(0.08).alphaDecay(0.07).velocityDecay(0.55)

        const svg = d3.create('svg').attr('viewBox', `${-width / 2} ${-height / 2} ${width} ${height}`)
            .attr('aria-label', i18n['graph_view'])
            .on('click.dismiss', () => {
                this.menuSubject = ''
                this.hideInfoPane()
            })
        const scene = svg.append('g').attr('id', 'scene')
        const zoom = d3.zoom<SVGSVGElement, undefined>().scaleExtent([0.25, 2.5]).on('zoom', event => scene.attr('transform', event.transform))
        svg.call(zoom)

        svg.append('defs').selectAll('marker').data(types).join('marker')
            .attr('id', (_, index) => `arrow-${index}`)
            .attr('viewBox', '0 -5 10 10').attr('refX', 11).attr('refY', -1)
            .attr('markerWidth', 6).attr('markerHeight', 6).attr('orient', 'auto')
            .attr('stroke', 'var(--background-color, white)').attr('stroke-width', 2)
            .append('path').attr('fill', type => color(type)).attr('d', 'M0,-5L10,0L0,5')

        const link = scene.append('g').attr('fill', 'none').attr('stroke-width', 2).attr('class', 'links')
            .selectAll('path').data(links).join('path')
            .attr('id', (_, index) => `link-path-${index}`)
            .attr('stroke', edge => color(edge.type))
            .attr('marker-end', edge => `url(${new URL(`#arrow-${types.indexOf(edge.type)}`, location.toString())})`)

        scene.append('g').attr('class', 'link-labels').selectAll('text').data(links).join('text')
            .attr('font-size', 7).attr('dy', '-0.3em').append('textPath')
            .attr('fill', edge => color(edge.type)).attr('href', (_, index) => `#link-path-${index}`)
            .attr('startOffset', '45%').attr('text-anchor', 'middle').text(edge => edge.label || edge.type)

        const node = scene.append('g').attr('fill', '#888').selectAll<SVGGElement, Node>('g').data(nodeArray).join('g')
            .attr('class', item => `node${item.id === this.activeSubject ? ' root' : ''}${!hydrated.has(item.id) ? ' stub' : ''}${this.newNodes.has(item.id) ? ' new' : ''}`)
            .attr('tabindex', item => item.navigable ? 0 : -1)
            .attr('role', item => item.navigable ? 'button' : null)
            .attr('aria-label', item => item.navigable ? `${i18n['graph_actions_for']} ${i18n[item.id] || item.id}` : null)
            .call(drag(simulation))

        node.append('circle').attr('class', 'new-ring').attr('r', 5)
        node.append('circle').attr('class', 'node-circle').attr('stroke', 'var(--background-color, white)')
            .attr('stroke-width', 0.5).attr('r', item => item.id === this.activeSubject ? 7 : 4)
        node.append('text').attr('x', 9).attr('y', '0.31em').html(item => item.label ?? escapeHtml(item.id))
            .clone(true).lower().attr('fill', 'none').attr('stroke', 'var(--background-color, white)').attr('stroke-width', 1)

        node.on('mouseenter', (event, item) => {
            if (!this.infopane.classList.contains('pinned')) {
                this.showInfoPane(item, false)
            }
            if (item.navigable) {
                if (this.menuSubject !== item.id) {
                    this.scheduleMenuOpen(event, item, event.currentTarget as SVGGElement)
                } else {
                    this.clearMenuCloseTimer()
                }
            }
        }).on('mouseleave', () => {
            this.hideInfoPane(false)
            this.clearMenuOpenTimer()
            if (!this.menuPinned) {
                this.scheduleMenuClose()
            }
        })
        node.on('click.menu', (event, item) => {
            if (this.menuSubject === item.id && this.menuPinned) {
                this.closeMenu()
            } else {
                this.openMenu(event, item, event.currentTarget as SVGGElement, true)
            }
        })
        node.on('keydown', (event, item) => {
            if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                this.openMenu(event, item, event.currentTarget as SVGGElement)
            }
        })
        node.on('pointerdown', event => event.stopPropagation())

        const updatePositions = () => {
            link.attr('d', linkArc)
            node.attr('transform', item => {
                this.positions.set(item.id, { x: item.x ?? 0, y: item.y ?? 0 })
                return `translate(${item.x},${item.y})`
            })
        }
        simulation.on('tick', updatePositions)
        for (let index = 0; index < 45; index++) {
            simulation.tick()
        }
        updatePositions()
        simulation.stop()
        return Object.assign(svg.node()!, { zoomBehaviour: zoom, nodeCount: nodeArray.length, edgeCount: links.length })
    }

    private fitGraph() {
        if (this.currentSvg) {
            fitToView(this.currentSvg)
        }
    }

    render() {
        const progress = this.incomingProgress.get(this.menuSubject)
        const incomingComplete = progress !== undefined && progress.offset >= progress.total
        const incomingLoading = this.actionLoading.has(this.actionKey(this.menuSubject, 'incoming'))
        const outgoingLoading = this.actionLoading.has(this.actionKey(this.menuSubject, 'outgoing'))
        const incomingLabel = incomingLoading
            ? i18n['graph_loading']
            : incomingComplete
                ? i18n['graph_incoming_loaded']
                : progress && progress.total > incomingBatchSize
                    ? `${i18n['graph_load_more']} (${progress.offset}/${progress.total})`
                    : i18n['graph_load_incoming']
        return html`
            <div id="mount"></div>
            <div class="toolbar" @click=${(event: Event) => event.stopPropagation()}>
                <button @click=${this.fitGraph} title=${i18n['graph_fit']} aria-label=${i18n['graph_fit']}><span class="material-icons">fit_screen</span></button>
                <button @click=${this.reset} title=${i18n['graph_reset']} aria-label=${i18n['graph_reset']} ?disabled=${this.loading}><span class="material-icons">restart_alt</span></button>
                <button @click=${this.loadAllLinks} title=${i18n['graph_load_all']} aria-label=${i18n['graph_load_all']} ?disabled=${this.loading}><span class="material-icons">account_tree</span></button>
                <span class="counts">${this.nodeCount} ${i18n['graph_nodes']} · ${this.edgeCount} ${i18n['graph_edges']}</span>
            </div>
            ${this.loading ? html`<div class="status">${i18n['graph_loading']}</div>` : nothing}
            ${this.error ? html`<div class="status error" role="alert">${this.error}</div>` : nothing}
            ${!this.menuSubject ? nothing : html`
                <div class="radial-menu" style="left:${this.menuX}px;top:${this.menuY}px" @click=${(event: Event) => event.stopPropagation()}>
                    <button class="incoming" @click=${() => this.loadIncoming(this.menuSubject)}
                        @mouseenter=${this.clearMenuCloseTimer} @mouseleave=${this.scheduleMenuClose}
                        ?disabled=${incomingComplete || incomingLoading}
                        title=${incomingLabel} aria-label=${incomingLabel}>
                        <span class="material-icons">${incomingLoading ? 'hourglass_top' : incomingComplete ? 'done' : 'call_received'}</span><span>${incomingLabel}</span>
                    </button>
                    <button class="outgoing" @click=${() => this.loadOutgoing(this.menuSubject)}
                        @mouseenter=${this.clearMenuCloseTimer} @mouseleave=${this.scheduleMenuClose}
                        ?disabled=${this.outgoingLoaded.has(this.menuSubject) || outgoingLoading}
                        title=${outgoingLoading ? i18n['graph_loading'] : i18n['graph_load_outgoing']} aria-label=${outgoingLoading ? i18n['graph_loading'] : i18n['graph_load_outgoing']}>
                        <span class="material-icons">${outgoingLoading ? 'hourglass_top' : this.outgoingLoaded.has(this.menuSubject) ? 'done' : 'call_made'}</span><span>${outgoingLoading ? i18n['graph_loading'] : i18n['graph_load_outgoing']}</span>
                    </button>
                    <button class="focus" @click=${() => this.focusEntity(this.menuSubject)} title=${i18n['graph_focus']} aria-label=${i18n['graph_focus']}
                        @mouseenter=${this.clearMenuCloseTimer} @mouseleave=${this.scheduleMenuClose}>
                        <span class="material-icons">center_focus_strong</span><span>${i18n['graph_focus']}</span>
                    </button>
                    <div class="center">${i18n[this.menuSubject] || this.menuSubject}</div>
                </div>
            `}
            <div id="info-pane" @click=${(event: Event) => event.stopPropagation()}></div>
        `
    }
}

function addDefinition(list: HTMLDListElement, key: string, value: string) {
    const dt = document.createElement('dt')
    dt.textContent = key
    const dd = document.createElement('dd')
    dd.textContent = value
    list.append(dt, dd)
}

function escapeHtml(value: string) {
    return value.replace(/[&<>"']/g, character => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' })[character]!)
}

function fitToView(svg: SVGSVGElement) {
    const scene = svg.querySelector<SVGGElement>('#scene')
    const zoom = (svg as SVGSVGElement & { zoomBehaviour?: d3.ZoomBehavior<SVGSVGElement, undefined> }).zoomBehaviour
    if (!scene || !zoom) {
        return
    }
    const bbox = scene.getBBox()
    if (!bbox.width || !bbox.height) {
        return
    }
    const pad = 30
    const scale = Math.min(Math.min((width - 2 * pad) / bbox.width, (height - 2 * pad) / bbox.height), 1.5)
    const transform = d3.zoomIdentity.translate(0, pad - height / 2).scale(scale)
        .translate(-(bbox.x + bbox.width / 2), -bbox.y)
    d3.select<SVGSVGElement, undefined>(svg).call(zoom.transform, transform)
}

function linkArc(edge: Edge) {
    const source = edge.source as Node
    const target = edge.target as Node
    const radius = Math.max(Math.hypot((target.x ?? 0) - (source.x ?? 0), (target.y ?? 0) - (source.y ?? 0)), 1)
    return `M${source.x},${source.y} A${radius},${radius} 0 0,1 ${target.x},${target.y}`
}

function drag(simulation: Simulation<Node, Edge>) {
    let startX = 0
    let startY = 0
    return d3.drag<SVGGElement, Node, Node>()
        .on('start', (event: D3DragEvent<SVGGElement, Node, Node>, node) => {
            const source = event.sourceEvent as PointerEvent | MouseEvent | undefined
            startX = source?.clientX ?? 0
            startY = source?.clientY ?? 0
            node.fx = node.x
            node.fy = node.y
        })
        .on('drag', (event: D3DragEvent<SVGGElement, Node, Node>, node) => {
            const source = event.sourceEvent as PointerEvent | MouseEvent | undefined
            if (Math.hypot((source?.clientX ?? 0) - startX, (source?.clientY ?? 0) - startY) > 2) {
                simulation.alphaTarget(0.3).restart()
            }
            node.fx = event.x
            node.fy = event.y
        })
        .on('end', (event: D3DragEvent<SVGGElement, Node, Node>, node) => {
            if (!event.active) {
                simulation.alphaTarget(0)
            }
            node.fx = null
            node.fy = null
        })
}
