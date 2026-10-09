import { css, html, LitElement, type PropertyValues } from 'lit'
import { customElement, property, query, state } from 'lit/decorators.js'
import * as d3 from 'd3'
import { type D3DragEvent, type Simulation, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3'
import { Parser, Quad } from 'n3'
import { BACKEND_URL, RDF_TYPE } from './constants'
import { fetchLabels, i18n } from './i18n'
import type { Config } from '.'
import {
    collectGraphNodeIds, computeFlowDirections, flattenLiteralCollections, mergeQuads, nodeId, quadKey,
    reserveRequestWave, serializeNQuads, stableGraphSeed, selectLayoutEngine,
    type GraphLayoutEdge
} from './graph-layout'
import './graph-layout-force'
import './graph-layout-radial'
import './graph-layout-hybrid'
import './graph-layout-hierarchical'
import { globalStyles } from './styles'
import { removeSnackbarMessages, RokitSnackbar, showSnackbarMessage } from '@ro-kit/ui-widgets'

type Node = SimulationNodeDatum & {
    id: string
    label?: string
    type?: string
    navigable: boolean
    properties: Record<string, string[]>
}

type Edge = SimulationLinkDatum<Node> & {
    id: string
    sourceId: string
    targetId: string
    type: string
    label?: string
    web?: boolean
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
const revealLayerGap = 85
const growAnimationDuration = 420
const automaticPageSize = 10
const automaticWaveSize = 4
const defaultGraphNodeLimit = 50
const defaultGraphEdgeLimit = 100

const directions: Direction[] = ['outgoing', 'incoming']

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
        .node-badge { cursor: pointer; }
        .node-badge:hover rect { fill: color-mix(in srgb, var(--rokit-primary-color, #008877) 80%, black); }
        .node-badge rect { fill: var(--rokit-primary-color, #008877); stroke: var(--background-color, white); stroke-width: 1; }
        .node-badge text { fill: #fff; font-size: 8px; font-weight: 600; pointer-events: none; }
        .link-labels, .node-type { visibility: hidden; }
        svg:hover { .link-labels, .node-type { visibility: visible; } }
        .links .web { pointer-events: none; }

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
        .toolbar button {
            border: 0; border-radius: 8px; background: transparent; color: inherit; cursor: pointer;
            display: inline-flex; align-items: center; justify-content: center;
        }
        .toolbar button { width: 34px; height: 34px; }
        .toolbar button:hover, .toolbar button:focus-visible {
            background: color-mix(in srgb, var(--rokit-primary-color, #008877) 14%, transparent);
            outline: 2px solid color-mix(in srgb, var(--rokit-primary-color, #008877) 45%, transparent);
        }
        .toolbar .counts { font-size: 11px; color: #666; white-space: nowrap; padding-right: 5px; }
        .material-icons { font-size: 20px; }

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
    @property({ attribute: false }) config?: Config

    @state() private loading = false
    @state() private nodeCount = 0
    @state() private edgeCount = 0
    @state() private actionLoading = new Set<string>()

    private suppressFit = false
    private capacityNotified = false
    private revealing = false
    private layoutTimer?: d3.Timer

    @query('#info-pane') private infopane!: HTMLElement
    @query('#mount') private mount!: HTMLElement
    @query('#snackbar') private snackbar!: RokitSnackbar

    private initialSubject = ''
    private activeSubject = ''
    private quads = new Map<string, Quad>()
    private neighborhoodProgress = new Map<string, NeighborhoodProgress>()
    private expanded = new Set<string>()
    private localSubjects = new Map<string, boolean>()
    private positions = new Map<string, { x: number, y: number }>()
    private newNodes = new Set<string>()
    private requestEpoch = 0
    private abortController?: AbortController
    private drawVersion = 0
    private currentSvg?: SVGSVGElement
    private lastLayout?: { nodes: Node[], links: Edge[] }

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
        const { nodes, links } = this.lastLayout
        const positionMap = new Map(nodes.map(n => [n.id, { x: n.x ?? 0, y: n.y ?? 0 }]))
        const flowDirections = computeFlowDirections(positionMap, links.map(l => ({ id: l.id, source: l.sourceId, target: l.targetId })))
        const debugNodes = nodes.map(n => ({
            id: n.id,
            label: n.label?.replace(/<[^>]*>/g, '') ?? n.id,
            x: Math.round((n.x ?? 0) * 100) / 100,
            y: Math.round((n.y ?? 0) * 100) / 100
        }))
        const debugEdges = links.map(l => {
            const srcPos = positionMap.get(l.sourceId)
            const tgtPos = positionMap.get(l.targetId)
            const srcFlow = flowDirections.get(l.sourceId)
            const tgtFlow = flowDirections.get(l.targetId)
            return {
                id: l.id,
                source: l.sourceId,
                target: l.targetId,
                type: l.type,
                srcAngle: srcPos ? Math.atan2(srcPos.y, srcPos.x) : undefined,
                tgtAngle: tgtPos ? Math.atan2(tgtPos.y, tgtPos.x) : undefined,
                srcFlowAngle: srcFlow ? Math.round(Math.atan2(srcFlow.y, srcFlow.x) * 1000) / 1000 : undefined,
                tgtFlowAngle: tgtFlow ? Math.round(Math.atan2(tgtFlow.y, tgtFlow.x) * 1000) / 1000 : undefined
            }
        })
        const output = {
            nodeCount: nodes.length,
            edgeCount: links.length,
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

    private stopLayoutAnimation() {
        this.layoutTimer?.stop()
        this.layoutTimer = undefined
    }

    private clearGraph() {
        this.abortController?.abort()
        this.abortController = undefined
        this.requestEpoch++
        this.drawVersion++
        this.stopLayoutAnimation()
        this.activeSubject = ''
        this.quads.clear()
        this.neighborhoodProgress.clear()
        this.actionLoading = new Set()
        this.expanded.clear()
        this.localSubjects.clear()
        this.positions.clear()
        this.newNodes.clear()
        this.capacityNotified = false
        this.hideInfoPane()
        this.loading = false
        this.nodeCount = 0
        this.edgeCount = 0
        this.currentSvg = undefined
        this.mount?.replaceChildren()
        this.notifyState()
    }

    private async focusEntity(subject: string) {
        this.abortController?.abort()
        this.abortController = new AbortController()
        const epoch = ++this.requestEpoch
        this.drawVersion++
        this.stopLayoutAnimation()
        this.activeSubject = subject
        this.quads.clear()
        this.neighborhoodProgress.clear()
        this.actionLoading = new Set()
        this.expanded.clear()
        this.expanded.add(subject)
        this.localSubjects.clear()
        this.localSubjects.set(subject, true)
        this.positions.clear()
        this.newNodes.clear()
        this.capacityNotified = false
        this.hideInfoPane()
        this.loading = true
        this.nodeCount = 0
        this.edgeCount = 0
        this.currentSvg = undefined
        this.mount?.replaceChildren()
        this.notifyState()
        await this.loadInitialNeighborhood(subject, epoch)
        if (epoch === this.requestEpoch) {
            this.loading = false
            this.notifyState()
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

    private graphLimits() {
        return {
            nodeLimit: this.config?.graphNodeLimit ?? defaultGraphNodeLimit,
            edgeLimit: this.config?.graphEdgeLimit ?? defaultGraphEdgeLimit
        }
    }

    private automaticCapacity() {
        const visible = this.visibleNodes()
        const parentIn = this.discoveryParents(this.graphAdjacency(), visible)
        const { nodeLimit, edgeLimit } = this.graphLimits()
        return Math.min(nodeLimit - visible.size, edgeLimit - parentIn.size)
    }

    private async loadInitialNeighborhood(subject: string, epoch: number) {
        this.suppressFit = true
        try {
            await this.loadNodeFully(subject, epoch)
            await this.prefetchFrontier(epoch)
        } finally {
            this.suppressFit = false
            if (epoch === this.requestEpoch) {
                await this.drawGraph()
                this.fitGraph()
            }
        }
    }

    private graphAdjacency() {
        const adjacency = new Map<string, Set<string>>()
        const connect = (source: string, target: string) => {
            if (source === target) {
                return
            }
            let neighbors = adjacency.get(source)
            if (!neighbors) {
                adjacency.set(source, neighbors = new Set())
            }
            neighbors.add(target)
        }
        for (const quad of flattenLiteralCollections(this.quads.values())) {
            if (!this.isGraphNodeObject(quad)) {
                continue
            }
            const subject = nodeId(quad.subject, quad.graph.value)
            const object = nodeId(quad.object, quad.graph.value)
            connect(subject, object)
            connect(object, subject)
        }
        return adjacency
    }

    private visibleNodes() {
        const visible = new Set<string>()
        if (!this.activeSubject) {
            return visible
        }
        const adjacency = this.graphAdjacency()
        const queue = [this.activeSubject]
        visible.add(this.activeSubject)
        for (let index = 0; index < queue.length; index++) {
            const current = queue[index]
            if (current !== this.activeSubject && !this.expanded.has(current)) {
                continue
            }
            for (const neighbor of adjacency.get(current) ?? []) {
                if (!visible.has(neighbor)) {
                    visible.add(neighbor)
                    queue.push(neighbor)
                }
            }
        }
        return visible
    }

    // Forward (discovery) tree from the active subject restricted to the visible
    // set. Any edge outside this tree is a cross-link (reuse/web). Traversal is
    // restricted to expanded nodes (mirroring visibleNodes), so a node's parent
    // is always the node whose expansion actually revealed it - an unexpanded
    // sibling can never claim it, and web arcs never become navigational.
    private discoveryParents(adjacency: Map<string, Set<string>>, visible: Set<string>): Map<string, string> {
        const parentIn = new Map<string, string>()
        if (!this.activeSubject) {
            return parentIn
        }
        const seen = new Set([this.activeSubject])
        const walk = [this.activeSubject]
        for (let index = 0; index < walk.length; index++) {
            const current = walk[index]
            if (current !== this.activeSubject && !this.expanded.has(current)) {
                continue
            }
            for (const neighbor of adjacency.get(current) ?? []) {
                if (!visible.has(neighbor) || seen.has(neighbor)) {
                    continue
                }
                seen.add(neighbor)
                parentIn.set(neighbor, current)
                walk.push(neighbor)
            }
        }
        return parentIn
    }

    private frontierTasks() {
        const tasks: NeighborhoodTask[] = []
        for (const subject of this.visibleNodes()) {
            for (const direction of directions) {
                if (!this.neighborhoodProgress.get(this.actionKey(subject, direction))?.initialized) {
                    tasks.push({ subject, direction })
                }
            }
        }
        return tasks
    }

    private async prefetchFrontier(epoch: number) {
        await this.runNeighborhoodTasks(this.frontierTasks(), epoch)
    }

    private async runNeighborhoodTasks(tasks: NeighborhoodTask[], epoch: number) {
        const seen = new Set<string>()
        const pending = tasks.filter(task => {
            const key = this.actionKey(task.subject, task.direction)
            if (seen.has(key) || this.actionLoading.has(key)) {
                return false
            }
            seen.add(key)
            return true
        })
        for (let index = 0; index < pending.length && epoch === this.requestEpoch;) {
            const remaining = this.automaticCapacity()
            if (remaining <= 0) {
                this.notifyCapacityLimit()
                break
            }
            const reservations = reserveRequestWave(pending.slice(index), remaining, automaticPageSize, automaticWaveSize)
            if (reservations.length === 0) {
                break
            }
            index += reservations.length
            const results = await Promise.allSettled(reservations.map(({ item, limit }) => this.fetchNeighborhoodPage(item, limit, epoch)))
            if (epoch !== this.requestEpoch) {
                return
            }
            const loaded: LoadedPage[] = []
            for (const result of results) {
                if (result.status === 'fulfilled' && result.value) {
                    loaded.push(result.value)
                } else if (result.status === 'rejected' && !isAbortError(result.reason)) {
                    showSnackbarMessage({ message: `${i18n['graph_load_failed']}: ${result.reason}`, ttl: 0, cssClass: 'error', closable: true }, this.snackbar)
                }
            }
            if (loaded.length > 0) {
                this.applyPages(loaded)
            }
        }
    }

    private async loadNodeFully(subject: string, epoch: number) {
        for (const direction of directions) {
            const task: NeighborhoodTask = { subject, direction }
            while (epoch === this.requestEpoch) {
                const key = this.actionKey(subject, direction)
                const progress = this.progressFor(task)
                if ((progress.initialized && !progress.hasMore) || this.actionLoading.has(key)) {
                    break
                }
                if (this.automaticCapacity() <= 0) {
                    this.notifyCapacityLimit()
                    break
                }
                try {
                    const loaded = await this.fetchNeighborhoodPage(task, automaticPageSize, epoch)
                    if (!loaded || loaded.page.returned === 0) {
                        break
                    }
                    this.applyPages([loaded])
                } catch (error) {
                    if (epoch === this.requestEpoch && !isAbortError(error)) {
                        showSnackbarMessage({ message: `${i18n['graph_load_failed']}: ${error}`, ttl: 0, cssClass: 'error', closable: true }, this.snackbar)
                    }
                    break
                }
            }
        }
    }

    private notifyCapacityLimit() {
        if (this.capacityNotified) {
            return
        }
        this.capacityNotified = true
        removeSnackbarMessages(this.snackbar)
        const { nodeLimit, edgeLimit } = this.graphLimits()
        const message = i18n['graph_automatic_limited']
            .replace('{nodeLimit}', String(nodeLimit))
            .replace('{edgeLimit}', String(edgeLimit))
        showSnackbarMessage({ message, ttl: 7000, cssClass: 'success', closable: true }, this.snackbar)
    }

    private async expandNode(node: Node) {
        if (!node.navigable || this.activeSubject === node.id || this.expanded.has(node.id)) {
            return
        }
        this.capacityNotified = false
        this.expanded.add(node.id)
        const epoch = this.requestEpoch
        this.suppressFit = true
        this.revealing = true
        try {
            await this.drawGraph()
            await this.loadNodeFully(node.id, epoch)
            await this.prefetchFrontier(epoch)
            this.revealing = false
            if (epoch === this.requestEpoch) {
                await this.drawGraph()
            }
        } finally {
            this.revealing = false
            this.suppressFit = false
        }
    }

    private async collapseNode(node: Node) {
        if (this.activeSubject === node.id || !this.expanded.delete(node.id)) {
            return
        }
        this.suppressFit = true
        try {
            await this.drawGraph()
        } finally {
            this.suppressFit = false
        }
    }

    private async toggleNode(node: Node) {
        if (this.expanded.has(node.id)) {
            await this.collapseNode(node)
        } else {
            await this.expandNode(node)
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
            if (quads.length < page.returned) {
                throw new Error('graph neighborhood payload contains fewer quads than returned statements')
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

    private applyPages(loaded: LoadedPage[]) {
        for (const { page } of loaded) {
            for (const subject of page.localSubjects) {
                this.localSubjects.set(subject, true)
            }
        }
        this.mergeQuads(loaded.flatMap(result => result.quads))
        for (const { task, page } of loaded) {
            const progress = this.progressFor(task)
            progress.offset = page.nextOffset
            progress.initialized = true
            progress.hasMore = page.hasMore
        }
    }

    private isGraphNodeObject(quad: Quad) {
        return quad.predicate.value !== RDF_TYPE.value && (quad.object.termType === 'BlankNode'
            || (quad.object.termType === 'NamedNode' && this.localSubjects.get(quad.object.value) === true))
    }

    private mergeQuads(quads: Quad[]) {
        this.newNodes = mergeQuads(this.quads, quads, values =>
            collectGraphNodeIds(flattenLiteralCollections(values), quad => this.isGraphNodeObject(quad)))
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

    private deselect() {
        this.hideInfoPane()
    }

    private onBackgroundClick = (event: Event) => {
        if (event.target === this || event.target === this.mount || event.target === this.currentSvg) {
            this.deselect()
        }
    }

    private keyListener = (event: KeyboardEvent) => {
        if (event.key === 'Escape') {
            this.deselect()
        }
    }

    private async drawGraph() {
        const version = ++this.drawVersion
        const previousTransform = this.currentSvg ? d3.zoomTransform(this.currentSvg) : undefined
        const graph = await this.buildGraph()
        if (version !== this.drawVersion) {
            return
        }
        this.mount.replaceChildren(graph)
        this.currentSvg = graph
        this.nodeCount = graph.nodeCount
        this.edgeCount = graph.edgeCount
        this.newNodes.clear()
        if (previousTransform && graph.zoomBehaviour) {
            d3.select<SVGSVGElement, undefined>(graph).call(graph.zoomBehaviour.transform, previousTransform)
        }
        if (!this.suppressFit) {
            requestAnimationFrame(() => fitToView(graph))
        }
    }

    private async buildGraph() {
        this.stopLayoutAnimation()
        const labelsToFetch = new Set<string>()
        const nodes = new Map<string, Node>()
        const links: Edge[] = []
        const visibleQuads = flattenLiteralCollections(this.quads.values())
        const visible = this.visibleNodes()

        const ensureNode = (id: string, navigable: boolean) => {
            let node = nodes.get(id)
            if (!node) {
                const position = this.positions.get(id)
                node = { id, navigable, properties: {}, x: position?.x, y: position?.y }
                nodes.set(id, node)
            }
            return node
        }

        for (const quad of visibleQuads) {
            const subjectId = nodeId(quad.subject, quad.graph.value)
            if (!visible.has(subjectId)) {
                continue
            }
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
                if (!visible.has(objectId)) {
                    continue
                }
                ensureNode(objectId, quad.object.termType === 'NamedNode')
                links.push({
                    id: quadKey(quad),
                    source: subjectId,
                    target: objectId,
                    sourceId: subjectId,
                    targetId: objectId,
                    type: quad.predicate.value
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
        const hydrated = new Set(visibleQuads.map(q => nodeId(q.subject, q.graph.value)))
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

        const adjacency = this.graphAdjacency()
        const badges = new Map<string, string>()
        for (const id of visible) {
            if (nodes.get(id)?.navigable !== true) {
                continue
            }
            let hidden = 0
            for (const neighbor of adjacency.get(id) ?? []) {
                if (!visible.has(neighbor)) {
                    hidden++
                }
            }
            if (hidden > 0) {
                badges.set(id, `+${hidden}`)
            } else if (id !== this.activeSubject && this.expanded.has(id) && (adjacency.get(id)?.size ?? 0) > 0) {
                badges.set(id, '\u2212')
            }
        }

        // Build the discovery tree from the active subject. Edges connecting each
        // node to its parent are the readable skeleton; any edge outside that tree
        // is a cross-link (reuse/web) and gets demoted to a faint backdrop. This is
        // data-driven and predicate-agnostic: read it as "how did I get here?".
        const parentIn = this.discoveryParents(adjacency, visible)
        for (const link of links) {
            link.web = parentIn.get(link.targetId) !== link.sourceId && parentIn.get(link.sourceId) !== link.targetId
        }
        const skeletonLinks = links.filter(link => !link.web)

        const layoutEdges = skeletonLinks.map(graphLayoutEdge)
        const seed = stableGraphSeed(this.activeSubject, nodeArray.map(node => node.id), skeletonLinks.map(link => link.id))
        const engine = selectLayoutEngine(nodeArray, layoutEdges, this.activeSubject)
        const layout = engine.compute(nodeArray, layoutEdges, this.activeSubject, seed)
        const force = layout.force

        const placed = new Set(nodeArray.filter(node => this.positions.has(node.id)).map(node => node.id))
        for (const node of nodeArray) {
            if (node.id === this.activeSubject) {
                node.x = 0
                node.y = 0
            } else if (!placed.has(node.id)) {
                const pos = layout.positions.get(node.id)
                if (pos) {
                    node.x = pos.x
                    node.y = pos.y
                }
            }
        }

        // Reveal new nodes from the position of an already-placed neighbor so
        // they bloom out of the node that was expanded instead of jumping.
        const parentOf = new Map<string, string>()
        if (placed.size > 0) {
            for (const node of nodeArray) {
                if (placed.has(node.id) || node.id === this.activeSubject) {
                    continue
                }
                for (const neighbor of adjacency.get(node.id) ?? []) {
                    if (placed.has(neighbor)) {
                        parentOf.set(node.id, neighbor)
                        break
                    }
                }
            }
            const childrenOf = new Map<string, string[]>()
            for (const [child, parent] of parentOf) {
                const children = childrenOf.get(parent) ?? []
                children.push(child)
                childrenOf.set(parent, children)
            }
            const nodeById = new Map(nodeArray.map(node => [node.id, node]))
            for (const [parent, children] of childrenOf) {
                const anchor = nodeById.get(parent)
                if (!anchor || anchor.x === undefined || anchor.y === undefined) {
                    continue
                }
                const anchorRadius = Math.hypot(anchor.x, anchor.y)
                const anchorAngle = Math.atan2(anchor.y, anchor.x)
                const step = Math.min(0.5, (Math.PI / 2) / Math.max(1, children.length))
                children.sort()
                children.forEach((child, index) => {
                    const node = nodeById.get(child)
                    if (!node) {
                        return
                    }
                    const offset = (index - (children.length - 1) / 2) * step
                    const angle = anchorAngle + offset
                    const radius = anchorRadius + revealLayerGap
                    node.x = Math.cos(angle) * radius
                    node.y = Math.sin(angle) * radius
                })
            }
        }

        for (const node of nodeArray) {
            if (placed.has(node.id)) {
                node.fx = node.x
                node.fy = node.y
            }
        }

        const types = Array.from(new Set(skeletonLinks.map(link => link.type)))
        const color = d3.scaleOrdinal(types, d3.schemeTableau10)
        const simulation = d3.forceSimulation<Node, Edge>(nodeArray)
            .randomSource(d3.randomLcg(seed))
        if (force) {
            const linkForce = d3.forceLink<Node, Edge>(skeletonLinks).id(node => node.id)
                .distance(link => {
                    const src = typeof link.source === 'object' ? link.source : { id: String(link.source) }
                    const tgt = typeof link.target === 'object' ? link.target : { id: String(link.target) }
                    return force.linkDistance(src, tgt)
                })
            if (force.linkStrength !== undefined) {
                linkForce.strength(force.linkStrength)
            }
            simulation.force('link', linkForce)
            simulation.force('charge', d3.forceManyBody().strength(force.chargeStrength))
            simulation.force('collide', d3.forceCollide<Node>().radius(force.collideRadius).iterations(force.collideIterations))
            if (force.radialForce) {
                simulation.force('radial', d3.forceRadial<Node>(force.radialForce, 0, 0).strength(force.radialStrength))
            }
            if (force.centerStrength !== undefined) {
                simulation.force('x', d3.forceX<Node>().strength(force.centerStrength))
                simulation.force('y', d3.forceY<Node>().strength(force.centerStrength))
            }
            simulation.alpha(force.alpha).alphaMin(force.alphaMin).alphaDecay(force.alphaDecay).velocityDecay(force.velocityDecay)
        }
        simulation.stop()

        const svg = d3.create('svg').attr('viewBox', `${-width / 2} ${-height / 2} ${width} ${height}`)
            .attr('aria-label', i18n['graph_view'])
            .on('click.dismiss', () => {
                this.deselect()
            })
        const scene = svg.append('g').attr('id', 'scene')
        const zoom = d3.zoom<SVGSVGElement, undefined>().scaleExtent([0.25, 2.5]).on('zoom', event => scene.attr('transform', event.transform))
        svg.call(zoom)

        const defs = svg.append('defs')
        defs.selectAll('marker').data(types).join('marker')
            .attr('id', (_, index) => `arrow-${index}`)
            .attr('viewBox', '0 -5 10 10').attr('refX', 11).attr('refY', 0)
            .attr('markerWidth', 6).attr('markerHeight', 6).attr('orient', 'auto')
            .attr('stroke', 'var(--background-color, white)').attr('stroke-width', 2)
            .append('path').attr('fill', type => color(type)).attr('d', 'M0,-5L10,0L0,5')

        const webLink = scene.append('g').attr('fill', 'none').attr('stroke-width', 2).attr('class', 'links web')
            .selectAll('path').data(links.filter(link => link.web)).join('path')
            .attr('class', 'web')
            .attr('stroke', '#999').attr('stroke-dasharray', '2 3').attr('stroke-opacity', 0.3)
            .attr('stroke-width', 1)

        const link = scene.append('g').attr('fill', 'none').attr('stroke-width', 2).attr('class', 'links')
            .selectAll('path').data(skeletonLinks).join('path')
            .attr('id', (_, index) => `link-path-${index}`)
            .attr('stroke', edge => color(edge.type))
            .attr('marker-end', edge => `url(${new URL(`#arrow-${types.indexOf(edge.type)}`, location.toString())})`)

        scene.append('g').attr('class', 'link-labels').selectAll('text').data(links).join('text')
            .attr('font-size', 7).attr('dy', '-0.3em')
            .attr('paint-order', 'stroke').attr('stroke', 'var(--background-color, white)').attr('stroke-width', 2)
            .append('textPath')
            .attr('fill', edge => color(edge.type)).attr('href', (_, index) => `#link-path-${index}`)
            .attr('startOffset', '45%').attr('text-anchor', 'middle').text(edge => edge.label || edge.type)

        const node = scene.append('g').attr('fill', '#888').selectAll<SVGGElement, Node>('g').data(nodeArray).join('g')
            .attr('class', item => `node${item.id === this.activeSubject ? ' root' : ''}${!hydrated.has(item.id) ? ' stub' : ''}${this.newNodes.has(item.id) ? ' new' : ''}${this.expanded.has(item.id) ? ' expanded' : ''}${badges.has(item.id) ? ' has-badge' : ''}`)
            .attr('tabindex', item => item.navigable ? 0 : -1)
            .attr('role', item => item.navigable ? 'button' : null)
            .attr('aria-label', item => item.navigable ? `${i18n['graph_actions_for']} ${i18n[item.id] || item.id}` : null)
            .call(drag(simulation))

        node.append('circle').attr('class', 'new-ring').attr('r', 5)
        node.append('circle').attr('class', 'node-circle').attr('stroke', 'var(--background-color, white)')
            .attr('stroke-width', 0.5).attr('r', item => item.id === this.activeSubject ? 7 : 4)
        node.append('text').attr('x', 9).attr('y', '0.31em').html(item => item.label ?? escapeHtml(item.id))
            .clone(true).lower().attr('fill', 'none').attr('stroke', 'var(--background-color, white)').attr('stroke-width', 1)

        const badgeLabel = (item: Node) => badges.get(item.id) ?? ''
        const badgeWidth = (item: Node) => Math.max(13, badgeLabel(item).length * 5 + 7)
        const badge = node.filter(item => badges.has(item.id)).append('g').attr('class', 'node-badge')
            .attr('role', 'button')
            .attr('aria-label', item => {
                const action = this.expanded.has(item.id)
                    ? i18n['graph_collapse']
                    : i18n['graph_hidden_neighbors'].replace('{count}', (badges.get(item.id) ?? '+0').replace('+', ''))
                return `${action}: ${i18n[item.id] || item.id}`
            })
            .on('click', (event: MouseEvent, item: Node) => {
                event.stopPropagation()
                this.showInfoPane(item, true)
                void this.toggleNode(item)
            })
            .on('pointerdown', (event: Event) => event.stopPropagation())
        badge.append('title').text(item => this.expanded.has(item.id) ? i18n['graph_collapse'] : i18n['graph_expand'])
        badge.append('rect')
            .attr('x', item => 6 - badgeWidth(item))
            .attr('y', -17).attr('width', item => badgeWidth(item)).attr('height', 12).attr('rx', 6)
        badge.append('text')
            .attr('x', item => 6 - badgeWidth(item) / 2).attr('y', -11).attr('dy', '0.32em').attr('text-anchor', 'middle')
            .attr('aria-hidden', 'true').text(badgeLabel)

        node.on('mouseenter', (_event, item) => {
            if (!this.infopane.classList.contains('pinned')) {
                this.showInfoPane(item, false)
            }
        }).on('mouseleave', () => {
            this.hideInfoPane(false)
        })
        node.on('click', (event, item) => {
            event.stopPropagation()
            this.showInfoPane(item, true)
        })
        node.on('keydown', (event, item) => {
            if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                this.showInfoPane(item, true)
                void this.toggleNode(item)
            }
        })
        node.on('pointerdown', event => event.stopPropagation())

        const renderPositions = () => {
            const positions = new Map(nodeArray.map(item => [item.id, { x: item.x ?? 0, y: item.y ?? 0 }]))
            link.attr('d', edge => arcPath(positions.get(edge.sourceId), positions.get(edge.targetId)))
            webLink.attr('d', edge => arcPath(positions.get(edge.sourceId), positions.get(edge.targetId)))
            node.attr('transform', item => `translate(${item.x ?? 0},${item.y ?? 0})`)
        }
        const commitPositions = () => {
            renderPositions()
            for (const item of nodeArray) {
                if (this.revealing && parentOf.has(item.id)) {
                    continue
                }
                this.positions.set(item.id, { x: item.x ?? 0, y: item.y ?? 0 })
            }
        }
        simulation.on('tick', commitPositions)
        for (let index = 0; index < 390; index++) {
            simulation.tick()
        }
        simulation.stop()
        commitPositions()
        for (const item of nodeArray) {
            item.fx = null
            item.fy = null
        }

        const finalPositions = new Map(nodeArray.map(item => [item.id, { x: item.x ?? 0, y: item.y ?? 0 }]))
        const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
        if (placed.size > 0 && !reduceMotion && parentOf.size > 0) {
            for (const item of nodeArray) {
                const parent = parentOf.get(item.id)
                const start = parent ? finalPositions.get(parent) : undefined
                if (start) {
                    item.x = start.x
                    item.y = start.y
                }
            }
            renderPositions()
            const timer = d3.timer(elapsed => {
                const progress = Math.min(1, elapsed / growAnimationDuration)
                const eased = d3.easeCubicOut(progress)
                for (const item of nodeArray) {
                    const parent = parentOf.get(item.id)
                    if (!parent) {
                        continue
                    }
                    const start = finalPositions.get(parent)!
                    const end = finalPositions.get(item.id)!
                    item.x = start.x + (end.x - start.x) * eased
                    item.y = start.y + (end.y - start.y) * eased
                }
                renderPositions()
                if (progress >= 1) {
                    timer.stop()
                    if (this.layoutTimer === timer) {
                        this.layoutTimer = undefined
                    }
                }
            })
            this.layoutTimer = timer
        }
        this.lastLayout = {
            nodes: nodeArray,
            links
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
        return html`
            <div id="mount"></div>
            <div class="toolbar" @click=${(event: Event) => event.stopPropagation()}>
                <button @click=${this.fitGraph} title=${i18n['graph_fit']} aria-label=${i18n['graph_fit']}><span class="material-icons">fit_screen</span></button>
                <span class="counts">${this.nodeCount} ${i18n['graph_nodes']} · ${this.edgeCount} ${i18n['graph_edges']}</span>
            </div>
            <div id="info-pane" @click=${(event: Event) => event.stopPropagation()}></div>
            <rokit-snackbar id="snackbar" class="right contained"></rokit-snackbar>
        `
    }
}

function isAbortError(error: unknown) {
    return error instanceof DOMException && error.name === 'AbortError'
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

function arcPath(source: { x: number, y: number } | undefined, target: { x: number, y: number } | undefined) {
    const s = source ?? { x: 0, y: 0 }
    const t = target ?? { x: 0, y: 0 }
    const r = Math.hypot(t.x - s.x, t.y - s.y)
    return `M${s.x},${s.y} A${r},${r} 0 0,1 ${t.x},${t.y}`
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
