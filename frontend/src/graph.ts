import { css, html, LitElement, nothing, type PropertyValues } from 'lit'
import { customElement, property, query, state } from 'lit/decorators.js'
import * as d3 from 'd3'
import { type D3DragEvent, type Simulation, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3'
import { Parser, Quad } from 'n3'
import { BACKEND_URL, RDF_TYPE } from './constants'
import { fetchLabels, i18n } from './i18n'
import {
    collectGraphNodeIds, countRouteCrossings, edgePath, graphDepths, mergeQuads, nodeId, quadKey, radialRadii,
    reserveRequestWave, routeGraphEdges, serializeNQuads, stableGraphSeed, type EdgeRoute, type GraphLayoutEdge
} from './graph-model'
import { globalStyles } from './styles'
import { removeSnackbarMessages, RokitSnackbar, showSnackbarMessage } from '@ro-kit/ui-widgets'

type Node = SimulationNodeDatum & {
    id: string
    label?: string
    type?: string
    navigable: boolean
    properties: Record<string, string[]>
    depth?: number
    radialRadius?: number
}

type Edge = SimulationLinkDatum<Node> & {
    id: string
    sourceId: string
    targetId: string
    type: string
    label?: string
    route: EdgeRoute
}

type Direction = 'incoming' | 'outgoing'
type NeighborhoodProgress = { offset: number, initialized: boolean, hasMore: boolean }
type NeighborhoodTask = { subject: string, direction: Direction }
type NeighborhoodPage = {
    quads: string
    localSubjects: string[]
    offset: number
    limit: number
    returned: number
    hasMore: boolean
    nextOffset: number
}
type LoadedPage = { task: NeighborhoodTask, page: NeighborhoodPage, quads: Quad[] }

const width = 400
const height = 400
const automaticPageSize = 10
const manualPageSize = 25
const automaticWaveSize = 4
const automaticNodeLimit = 50
const automaticEdgeLimit = 100

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

        #snackbar::part(snackbar) { width: 500px; }
    `]

    @property() rdfSubject = ''
    @property() highlightSubject = ''

    @state() private loading = false
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
    @query('#snackbar') private snackbar!: RokitSnackbar

    private initialSubject = ''
    private activeSubject = ''
    private quads = new Map<string, Quad>()
    private neighborhoodProgress = new Map<string, NeighborhoodProgress>()
    private automaticQueue: NeighborhoodTask[] = []
    private queuedTasks = new Set<string>()
    private localSubjects = new Map<string, boolean>()
    private positions = new Map<string, { x: number, y: number }>()
    private newNodes = new Set<string>()
    private requestEpoch = 0
    private abortController?: AbortController
    private drawVersion = 0
    private currentSvg?: SVGSVGElement
    private lastLayout?: { nodes: Node[], links: Edge[], routes: Map<string, EdgeRoute>, crossings: number }

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
        this.abortController?.abort()
        this.abortController = undefined
        this.requestEpoch++
        if (this.loading) {
            this.loading = false
            this.notifyState()
        }
    }

    exportNQuads() {
        return serializeNQuads(this.quads.values())
    }

    hasExportData() {
        return !this.loading && this.quads.size > 0
    }

    debugLayout() {
        if (!this.lastLayout) {
            console.log('No layout data available')
            return
        }
        const { nodes, links, routes, crossings } = this.lastLayout
        const nodeMap = new Map(nodes.map(n => [n.id, n]))
        const debugNodes = nodes.map(n => ({
            id: n.id,
            label: n.label?.replace(/<[^>]*>/g, '') ?? n.id,
            depth: n.depth,
            x: Math.round((n.x ?? 0) * 100) / 100,
            y: Math.round((n.y ?? 0) * 100) / 100,
            angle: n.radialRadius ? Math.round(Math.atan2(n.y ?? 0, n.x ?? 0) * 1000) / 1000 : undefined,
            radius: n.radialRadius
        }))
        const debugEdges = links.map(l => {
            const route = routes.get(l.id)
            const src = nodeMap.get(l.sourceId)
            const tgt = nodeMap.get(l.targetId)
            return {
                id: l.id,
                source: l.sourceId,
                target: l.targetId,
                type: l.type,
                bend: route?.bend ?? 0,
                srcAngle: src ? Math.atan2(src.y ?? 0, src.x ?? 0) : undefined,
                tgtAngle: tgt ? Math.atan2(tgt.y ?? 0, tgt.x ?? 0) : undefined
            }
        })
        const output = {
            crossings,
            nodeCount: nodes.length,
            edgeCount: links.length,
            depthCounts: Object.entries(
                nodes.reduce((acc, n) => { acc[n.depth ?? 0] = (acc[n.depth ?? 0] ?? 0) + 1; return acc }, {} as Record<number, number>)
            ).map(([d, c]) => `depth ${d}: ${c} nodes`).join(', '),
            nodes: debugNodes,
            edges: debugEdges
        }
        console.log(JSON.stringify(output, null, 2))
        return output
    }

    private notifyState() {
        this.dispatchEvent(new CustomEvent('graph-state-change', {
            detail: { loading: this.loading, hasData: this.quads.size > 0 },
            bubbles: true,
            composed: true
        }))
    }

    private clearGraph() {
        this.abortController?.abort()
        this.abortController = undefined
        this.requestEpoch++
        this.drawVersion++
        this.activeSubject = ''
        this.quads.clear()
        this.neighborhoodProgress.clear()
        this.automaticQueue = []
        this.queuedTasks.clear()
        this.actionLoading = new Set()
        this.localSubjects.clear()
        this.positions.clear()
        this.closeMenu()
        this.clearMenuOpenTimer()
        this.loading = false
        this.nodeCount = 0
        this.edgeCount = 0
        this.mount?.replaceChildren()
        this.notifyState()
    }

    private async focusEntity(subject: string) {
        this.abortController?.abort()
        this.abortController = new AbortController()
        const epoch = ++this.requestEpoch
        this.drawVersion++
        this.activeSubject = subject
        this.quads.clear()
        this.neighborhoodProgress.clear()
        this.automaticQueue = []
        this.queuedTasks.clear()
        this.actionLoading = new Set()
        this.localSubjects.clear()
        this.localSubjects.set(subject, true)
        this.positions.clear()
        this.newNodes.clear()
        this.closeMenu()
        this.clearMenuOpenTimer()
        this.loading = true
        this.nodeCount = 0
        this.edgeCount = 0
        this.mount?.replaceChildren()
        this.notifyState()
        this.enqueueSubject(subject)
        await this.loadAdaptiveGraph(epoch)
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

    private progressFor(task: NeighborhoodTask) {
        const key = this.actionKey(task.subject, task.direction)
        let progress = this.neighborhoodProgress.get(key)
        if (!progress) {
            progress = { offset: 0, initialized: false, hasMore: true }
            this.neighborhoodProgress.set(key, progress)
        }
        return progress
    }

    private enqueueTask(task: NeighborhoodTask) {
        const key = this.actionKey(task.subject, task.direction)
        const progress = this.progressFor(task)
        if ((!progress.initialized || progress.hasMore) && !this.queuedTasks.has(key)) {
            this.automaticQueue.push(task)
            this.queuedTasks.add(key)
        }
    }

    private enqueueSubject(subject: string) {
        this.enqueueTask({ subject, direction: 'outgoing' })
        this.enqueueTask({ subject, direction: 'incoming' })
    }

    private automaticCapacity() {
        const nodes = collectGraphNodeIds(this.quads.values(), quad => this.isGraphNodeObject(quad))
        if (this.activeSubject) {
            nodes.add(this.activeSubject)
        }
        const edges = Array.from(this.quads.values()).filter(quad => this.isGraphNodeObject(quad)).length
        return Math.min(automaticNodeLimit - nodes.size, automaticEdgeLimit - edges)
    }

    private async loadAdaptiveGraph(epoch: number) {
        this.suppressFit = true
        let graphChanged = false
        try {
            while (this.automaticQueue.length > 0 && epoch === this.requestEpoch) {
                const remaining = this.automaticCapacity()
                if (remaining <= 0) {
                    removeSnackbarMessages(this.snackbar)
                    showSnackbarMessage({ message: i18n['graph_automatic_limited'], ttl: 5000, cssClass: 'success', closable: true }, this.snackbar)
                    break
                }
                const reservations = reserveRequestWave(this.automaticQueue, remaining, automaticPageSize, automaticWaveSize)
                const wave: Array<{ task: NeighborhoodTask, limit: number }> = []
                for (const reservation of reservations) {
                    const task = reservation.item
                    this.automaticQueue.shift()
                    this.queuedTasks.delete(this.actionKey(task.subject, task.direction))
                    wave.push({ task, limit: reservation.limit })
                }
                const results = await Promise.allSettled(wave.map(({ task, limit }) => this.fetchNeighborhoodPage(task, limit, epoch)))
                if (epoch !== this.requestEpoch) {
                    return
                }
                const loaded: LoadedPage[] = []
                let failed = false
                for (const result of results) {
                    if (result.status === 'fulfilled' && result.value) {
                        loaded.push(result.value)
                    } else if (result.status === 'rejected' && !(result.reason instanceof DOMException && result.reason.name === 'AbortError')) {
                        showSnackbarMessage({ message: `${i18n['graph_load_failed']}: ${result.reason}`, ttl: 0, cssClass: 'error', closable: true }, this.snackbar)
                        failed = true
                    }
                }
                if (loaded.length > 0) {
                    this.applyPages(loaded, true)
                    graphChanged = true
                }
                if (failed) {
                    break
                }
            }
        } finally {
            this.suppressFit = false
            if (epoch === this.requestEpoch) {
                if (graphChanged || !this.currentSvg) {
                    await this.drawGraph()
                }
                this.fitGraph()
            }
        }
    }

    private async loadDirection(subject: string, direction: Direction) {
        const epoch = this.requestEpoch
        const task = { subject, direction }
        const key = this.actionKey(subject, direction)
        const progress = this.progressFor(task)
        if (this.loading || this.actionLoading.has(key) || (progress.initialized && !progress.hasMore)) {
            return
        }
        this.closeMenu()
        try {
            const loaded = await this.fetchNeighborhoodPage(task, manualPageSize, epoch)
            if (loaded) {
                this.applyPages([loaded], false)
                await this.drawGraph()
                this.notifyState()
            }
        } catch (error) {
            if (epoch === this.requestEpoch && !(error instanceof DOMException && error.name === 'AbortError')) {
                showSnackbarMessage({ message: `${i18n['graph_load_failed']}: ${error}`, ttl: 0, cssClass: 'error', closable: true }, this.snackbar)
            }
        }
    }

    private async fetchNeighborhoodPage(task: NeighborhoodTask, limit: number, epoch: number): Promise<LoadedPage | undefined> {
        const key = this.actionKey(task.subject, task.direction)
        const progress = this.progressFor(task)
        this.actionLoading = new Set(this.actionLoading).add(key)
        try {
            const params = new URLSearchParams({
                subject: task.subject,
                direction: task.direction,
                offset: String(progress.offset),
                limit: String(limit)
            })
            const response = await fetch(`${BACKEND_URL}/graph/neighborhood?${params}`, { signal: this.abortController?.signal })
            if (!response.ok) {
                const message = await response.json().then(body => body.error).catch(() => response.statusText)
                throw new Error(message || response.statusText)
            }
            const page = await response.json() as NeighborhoodPage
            if (!isNeighborhoodPage(page) || page.offset !== progress.offset || page.limit !== limit) {
                throw new Error('invalid graph neighborhood response')
            }
            if (epoch !== this.requestEpoch) {
                return undefined
            }
            const quads = new Parser({ format: 'N-Quads' }).parse(page.quads)
            if (quads.length !== page.returned) {
                throw new Error('graph neighborhood row count does not match its payload')
            }
            return { task, page, quads }
        } finally {
            if (epoch === this.requestEpoch) {
                const next = new Set(this.actionLoading)
                next.delete(key)
                this.actionLoading = next
            }
        }
    }

    private applyPages(loaded: LoadedPage[], enqueueDiscovered: boolean) {
        for (const { page } of loaded) {
            for (const subject of page.localSubjects) {
                this.localSubjects.set(subject, true)
            }
        }
        const quads = loaded.flatMap(result => result.quads)
        this.mergeQuads(quads)
        for (const { task, page } of loaded) {
            const progress = this.progressFor(task)
            progress.offset = page.nextOffset
            progress.initialized = true
            progress.hasMore = page.hasMore
            if (enqueueDiscovered && page.hasMore) {
                this.enqueueTask(task)
            }
        }
        if (enqueueDiscovered) {
            for (const { quads: pageQuads } of loaded) {
                for (const quad of pageQuads) {
                    if (quad.subject.termType === 'NamedNode') {
                        this.enqueueSubject(quad.subject.value)
                    }
                    if (quad.predicate.value !== RDF_TYPE.value && quad.object.termType === 'NamedNode'
                        && this.localSubjects.get(quad.object.value) === true) {
                        this.enqueueSubject(quad.object.value)
                    }
                }
            }
        }
    }

    private isGraphNodeObject(quad: Quad) {
        return quad.predicate.value !== RDF_TYPE.value && (quad.object.termType === 'BlankNode'
            || (quad.object.termType === 'NamedNode' && this.localSubjects.get(quad.object.value) === true))
    }

    private mergeQuads(quads: Quad[]) {
        this.newNodes = mergeQuads(this.quads, quads, values => collectGraphNodeIds(values, quad => this.isGraphNodeObject(quad)))
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
                || (quad.object.termType === 'NamedNode' && this.localSubjects.get(quad.object.value) !== true)) {
                (subject.properties[quad.predicate.value] ??= []).push(quad.object.value)
                if (quad.object.termType === 'NamedNode') {
                    labelsToFetch.add(quad.object.value)
                }
            } else if (this.isGraphNodeObject(quad)) {
                const objectId = nodeId(quad.object, quad.graph.value)
                ensureNode(objectId, quad.object.termType === 'NamedNode')
                links.push({
                    id: quadKey(quad),
                    source: subjectId,
                    target: objectId,
                    sourceId: subjectId,
                    targetId: objectId,
                    type: quad.predicate.value,
                    route: { bend: 0 }
                })
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

        const layoutEdges = links.map(graphLayoutEdge)
        const depths = graphDepths(nodeArray.map(node => node.id), layoutEdges, this.activeSubject)
        const phase = stableGraphSeed(this.activeSubject, nodeArray.map(node => node.id), links.map(link => link.id)) * Math.PI * 2
        const rings = new Map<number, Node[]>()
        for (const node of nodeArray) {
            node.depth = depths.get(node.id) ?? 1
            const ring = rings.get(node.depth) ?? []
            ring.push(node)
            rings.set(node.depth, ring)
        }
        const ringRadii = radialRadii(nodeArray.map(node => node.depth ?? 1))
        const neighborMap = new Map<string, string[]>()
        for (const link of links) {
            if (link.sourceId !== link.targetId) {
                let list = neighborMap.get(link.sourceId)
                if (!list) {
                    neighborMap.set(link.sourceId, list = [])
                }
                list.push(link.targetId)
                let revList = neighborMap.get(link.targetId)
                if (!revList) {
                    neighborMap.set(link.targetId, revList = [])
                }
                revList.push(link.sourceId)
            }
        }
        const nodeAngle = new Map<string, number>()
        const sortedDepths = Array.from(rings.keys()).sort((a, b) => a - b)
        for (const depth of sortedDepths) {
            const ring = rings.get(depth)!
            if (depth === 0) {
                ring.sort((left, right) => left.id.localeCompare(right.id))
            } else {
                ring.sort((left, right) => {
                    const lBary = barycentricAngle(neighborMap.get(left.id), nodeAngle)
                    const rBary = barycentricAngle(neighborMap.get(right.id), nodeAngle)
                    return lBary - rBary || left.id.localeCompare(right.id)
                })
            }
            for (let index = 0; index < ring.length; index++) {
                nodeAngle.set(ring[index].id, index / ring.length * Math.PI * 2)
            }
        }
        for (const depth of sortedDepths) {
            if (depth === 0) {
                continue
            }
            const ring = rings.get(depth)!
            ring.sort((left, right) => {
                const lBary = barycentricAngle(neighborMap.get(left.id), nodeAngle)
                const rBary = barycentricAngle(neighborMap.get(right.id), nodeAngle)
                return lBary - rBary || left.id.localeCompare(right.id)
            })
            for (let index = 0; index < ring.length; index++) {
                nodeAngle.set(ring[index].id, index / ring.length * Math.PI * 2)
            }
        }
        for (const depth of sortedDepths) {
            const ring = rings.get(depth)!
            for (let index = 0; index < ring.length; index++) {
                const node = ring[index]
                node.radialRadius = ringRadii.get(depth) ?? depth * 85
                if (node.id === this.activeSubject) {
                    node.x = 0
                    node.y = 0
                    node.fx = 0
                    node.fy = 0
                } else if (!this.positions.has(node.id)) {
                    const angle = phase + (nodeAngle.get(node.id) ?? 0)
                    node.x = Math.cos(angle) * node.radialRadius
                    node.y = Math.sin(angle) * node.radialRadius
                }
            }
        }

        const types = Array.from(new Set(links.map(link => link.type)))
        const color = d3.scaleOrdinal(types, d3.schemeTableau10)
        const simulation = d3.forceSimulation<Node, Edge>(nodeArray)
            .randomSource(d3.randomLcg(stableGraphSeed(this.activeSubject, nodeArray.map(node => node.id), links.map(link => link.id))))
            .force('link', d3.forceLink<Node, Edge>(links).id(node => node.id).distance(edge => {
                const source = edge.source as Node
                const target = edge.target as Node
                return Math.max(65, Math.abs((source.radialRadius ?? 0) - (target.radialRadius ?? 0)))
            }).strength(0.2))
            .force('charge', d3.forceManyBody().strength(-200))
            .force('collide', d3.forceCollide<Node>().radius(22).iterations(2))
            .force('radial', d3.forceRadial<Node>(node => node.radialRadius ?? 85, 0, 0).strength(node => node.id === this.activeSubject ? 1 : 0.9))
            .alpha(1.2).alphaMin(0.03).alphaDecay(0.055).velocityDecay(0.5)
            .stop()

        const svg = d3.create('svg').attr('viewBox', `${-width / 2} ${-height / 2} ${width} ${height}`)
            .attr('aria-label', i18n['graph_view'])
            .on('click.dismiss', () => {
                this.menuSubject = ''
                this.hideInfoPane()
            })
        const scene = svg.append('g').attr('id', 'scene')
        const zoom = d3.zoom<SVGSVGElement, undefined>().scaleExtent([0.25, 2.5]).on('zoom', event => scene.attr('transform', event.transform))
        svg.call(zoom)

        const defs = svg.append('defs')
        defs.selectAll('marker').data(types).join('marker')
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

        const labelGuide = defs.append('g').selectAll('path').data(links).join('path')
            .attr('id', (_, index) => `label-path-${index}`)

        scene.append('g').attr('class', 'link-labels').selectAll('text').data(links).join('text')
            .attr('font-size', 7).attr('dy', '-0.3em')
            .attr('paint-order', 'stroke').attr('stroke', 'var(--background-color, white)').attr('stroke-width', 2)
            .append('textPath')
            .attr('fill', edge => color(edge.type)).attr('href', (_, index) => `#label-path-${index}`)
            .attr('startOffset', '50%').attr('text-anchor', 'middle').text(edge => edge.label || edge.type)

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

        const currentPositions = () => new Map(nodeArray.map(node => [node.id, { x: node.x ?? 0, y: node.y ?? 0 }]))
        let lastRoutes = new Map<string, EdgeRoute>()
        const assignRoutes = () => {
            const positions = nodeArray.map(node => ({ id: node.id, x: node.x ?? 0, y: node.y ?? 0 }))
            lastRoutes = routeGraphEdges(positions, layoutEdges)
            for (const edge of links) {
                edge.route = lastRoutes.get(edge.id) ?? { bend: 0 }
            }
        }
        const updatePositions = () => {
            const positions = currentPositions()
            link.attr('d', edge => edgePath(graphLayoutEdge(edge), edge.route, positions))
            labelGuide.attr('d', edge => edgePath(graphLayoutEdge(edge), edge.route, positions, true))
            node.attr('transform', item => {
                this.positions.set(item.id, { x: item.x ?? 0, y: item.y ?? 0 })
                return `translate(${item.x},${item.y})`
            })
        }
        simulation.on('tick', updatePositions).on('end', () => {
            assignRoutes()
            updatePositions()
        })
        for (let index = 0; index < 90; index++) {
            simulation.tick()
        }
        const activeNode = nodeArray.find(node => node.id === this.activeSubject)
        if (activeNode) {
            activeNode.fx = null
            activeNode.fy = null
        }
        simulation.alpha(1.0)
        for (let index = 0; index < 300; index++) {
            simulation.tick()
        }
        assignRoutes()
        updatePositions()
        simulation.stop()
        this.lastLayout = {
            nodes: nodeArray,
            links,
            routes: lastRoutes,
            crossings: countRouteCrossings(nodeArray.map(n => ({ id: n.id, x: n.x ?? 0, y: n.y ?? 0 })), layoutEdges, lastRoutes)
        }
        return Object.assign(svg.node()!, {
            zoomBehaviour: zoom,
            nodeCount: nodeArray.length,
            edgeCount: links.length
        })
    }

    private fitGraph() {
        if (this.currentSvg) {
            fitToView(this.currentSvg)
        }
    }

    render() {
        const incomingProgress = this.neighborhoodProgress.get(this.actionKey(this.menuSubject, 'incoming'))
        const outgoingProgress = this.neighborhoodProgress.get(this.actionKey(this.menuSubject, 'outgoing'))
        const incomingComplete = incomingProgress?.initialized === true && !incomingProgress.hasMore
        const outgoingComplete = outgoingProgress?.initialized === true && !outgoingProgress.hasMore
        const incomingLoading = this.actionLoading.has(this.actionKey(this.menuSubject, 'incoming'))
        const outgoingLoading = this.actionLoading.has(this.actionKey(this.menuSubject, 'outgoing'))
        const incomingLabel = incomingLoading
            ? i18n['graph_loading']
            : incomingComplete
                ? i18n['graph_incoming_loaded']
                : incomingProgress?.initialized
                    ? i18n['graph_load_more']
                    : i18n['graph_load_incoming']
        const outgoingLabel = outgoingLoading
            ? i18n['graph_loading']
            : outgoingComplete
                ? i18n['graph_outgoing_loaded']
                : outgoingProgress?.initialized
                    ? i18n['graph_load_more']
                    : i18n['graph_load_outgoing']
        return html`
            <div id="mount"></div>
            <div class="toolbar" @click=${(event: Event) => event.stopPropagation()}>
                <button @click=${this.fitGraph} title=${i18n['graph_fit']} aria-label=${i18n['graph_fit']}><span class="material-icons">fit_screen</span></button>
                <button @click=${this.reset} title=${i18n['graph_reset']} aria-label=${i18n['graph_reset']} ?disabled=${this.loading}><span class="material-icons">restart_alt</span></button>
                <span class="counts">${this.nodeCount} ${i18n['graph_nodes']} · ${this.edgeCount} ${i18n['graph_edges']}</span>
            </div>
            ${!this.menuSubject ? nothing : html`
                <div class="radial-menu" style="left:${this.menuX}px;top:${this.menuY}px" @click=${(event: Event) => event.stopPropagation()}>
                    <button class="incoming" @click=${() => this.loadDirection(this.menuSubject, 'incoming')}
                        @mouseenter=${this.clearMenuCloseTimer} @mouseleave=${this.scheduleMenuClose}
                        ?disabled=${this.loading || incomingComplete || incomingLoading}
                        title=${incomingLabel} aria-label=${incomingLabel}>
                        <span class="material-icons">${incomingLoading ? 'hourglass_top' : incomingComplete ? 'done' : 'call_received'}</span><span>${incomingLabel}</span>
                    </button>
                    <button class="outgoing" @click=${() => this.loadDirection(this.menuSubject, 'outgoing')}
                        @mouseenter=${this.clearMenuCloseTimer} @mouseleave=${this.scheduleMenuClose}
                        ?disabled=${this.loading || outgoingComplete || outgoingLoading}
                        title=${outgoingLabel} aria-label=${outgoingLabel}>
                        <span class="material-icons">${outgoingLoading ? 'hourglass_top' : outgoingComplete ? 'done' : 'call_made'}</span><span>${outgoingLabel}</span>
                    </button>
                    <button class="focus" @click=${() => this.focusEntity(this.menuSubject)} title=${i18n['graph_focus']} aria-label=${i18n['graph_focus']}
                        @mouseenter=${this.clearMenuCloseTimer} @mouseleave=${this.scheduleMenuClose}>
                        <span class="material-icons">center_focus_strong</span><span>${i18n['graph_focus']}</span>
                    </button>
                    <div class="center">${i18n[this.menuSubject] || this.menuSubject}</div>
                </div>
            `}
            <div id="info-pane" @click=${(event: Event) => event.stopPropagation()}></div>
            <rokit-snackbar id="snackbar" class="right contained"></rokit-snackbar>
        `
    }
}

function isNeighborhoodPage(value: unknown): value is NeighborhoodPage {
    if (!value || typeof value !== 'object') {
        return false
    }
    const page = value as Partial<NeighborhoodPage>
    return typeof page.quads === 'string'
        && Array.isArray(page.localSubjects) && page.localSubjects.every(subject => typeof subject === 'string')
        && Number.isInteger(page.offset) && (page.offset ?? -1) >= 0
        && Number.isInteger(page.limit) && (page.limit ?? 0) >= 1 && (page.limit ?? 101) <= 100
        && Number.isInteger(page.returned) && (page.returned ?? -1) >= 0 && (page.returned ?? 101) <= (page.limit ?? 0)
        && typeof page.hasMore === 'boolean'
        && Number.isInteger(page.nextOffset) && page.nextOffset === (page.offset ?? 0) + (page.returned ?? 0)
}

function addDefinition(list: HTMLDListElement, key: string, value: string) {
    const dt = document.createElement('dt')
    dt.textContent = key
    const dd = document.createElement('dd')
    dd.textContent = value
    list.append(dt, dd)
}

function barycentricAngle(neighbors: string[] | undefined, nodeAngle: Map<string, number>): number {
    if (!neighbors || neighbors.length === 0) {
        return Infinity
    }
    let sumSin = 0
    let sumCos = 0
    let counted = 0
    for (const id of neighbors) {
        const angle = nodeAngle.get(id)
        if (angle !== undefined) {
            sumSin += Math.sin(angle)
            sumCos += Math.cos(angle)
            counted++
        }
    }
    if (counted === 0) {
        return Infinity
    }
    return Math.atan2(sumSin / counted, sumCos / counted)
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

function graphLayoutEdge(edge: Edge): GraphLayoutEdge {
    return { id: edge.id, source: edge.sourceId, target: edge.targetId, label: edge.label }
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
