import { customElement, property, query, state } from 'lit/decorators.js'
import { LitElement, html, nothing, unsafeCSS } from 'lit'
import '@fontsource/roboto'
import '@fontsource/material-icons'
import styles from './styles.css?inline'
import { globalStyles } from './styles'
import './editor'
import './viewer'
import { BACKEND_URL, APP_PATH } from './constants'
import { RokitInput, showSnackbarMessage } from '@ro-kit/ui-widgets'
import { initFacets } from './facets'
import { search, SearchDocument } from './solr'
import { fetchLabels, i18n } from './i18n'
import { Editor } from './editor'
import { map } from 'lit/directives/map.js'
import { range } from 'lit/directives/range.js'
import { registerPlugin } from '@ulb-darmstadt/shacl-form'
import { LeafletPlugin } from '@ulb-darmstadt/shacl-form/plugins/leaflet.js'
import { Facets } from './facets/base'

export type Config = {
    layout: string
    profiles: string[]
    index: string
    geoDataType: string
    solrMaxAggregations: number
    authEnabled: boolean
    authWriteAccess: boolean
    authUser: string
    authEmail: string
    contactEmail: string
    rdfNamespace: string
}

@customElement('rdf-store')
export class App extends LitElement {
    static styles = [unsafeCSS(styles), globalStyles]
    @property()
    offset = 0
    @property()
    limit = 10
    @property()
    searchTerm = ''
    @property()
    searchCreator?: string

    @state()
    searchHits: SearchDocument[] = []
    @state()
    facets?: Facets
    @state()
    totalHits = 0
    @state()
    viewRdfSubject?: string
    @state()
    viewHiglightSubject?: string
    @state()
    config: Config | undefined

    @query('#search-field')
    searchField?: RokitInput
    @query('#search-own')
    searchOwn?: HTMLInputElement

    debounceTimeout: ReturnType<typeof setTimeout> | undefined
    handleLocationChange = () => {
        const index = window.location.pathname.indexOf('/resource/')
        if (index > -1) {
            const id = window.location.pathname.substring(index + 10)
            if (id && this.config) {
                this.viewRdfSubject = this.config.rdfNamespace + id
            }
        }
    }

    connectedCallback() {
        super.connectedCallback();
        window.addEventListener('popstate', this.handleLocationChange)
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        window.removeEventListener('popstate', this.handleLocationChange)
    }

    viewResource(subject: string | SearchDocument | null) {
        let path = APP_PATH
        if (subject) {
            path += 'resource/'
            if (typeof subject === 'string') {
                path += subject.replace(this.config?.rdfNamespace ?? '', '')
            } else {
                path += subject._root_.replace('container_', '').replace(this.config?.rdfNamespace ?? '', '')
                this.viewHiglightSubject = subject.id
            }
        }
        history.pushState('', '', path)
        this.handleLocationChange()
    }

    async firstUpdated() {
        try {
            const resp = await fetch(`${BACKEND_URL}/config`)
            if (!resp.ok) {
                throw 'Failed loading application configuration'
            }
            this.config = await resp.json() as Config
            await this.applyLayout(this.config.layout || 'default')

              // fetch labels for profiles. this is needed for the editor dialog, which renders before the keyword facet can load the labels
            await fetchLabels(this.config.profiles, true)
            this.facets = await initFacets(this.config.index, this.config.solrMaxAggregations)
            if (this.config.geoDataType) {
                registerPlugin(new LeafletPlugin({ datatype: this.config.geoDataType }))
            }

            if (this.config.authEnabled && this.config.authUser && !this.config.authWriteAccess) {
                let message = 'You don\'t currently have the necessary permissions to create resources.'
                if (this.config.contactEmail) {
                    const subject = encodeURIComponent(`Request for write access to ${window.location.href}`)
                    const body = encodeURIComponent(`Hi,\n\nI need write access to ${window.location.href}.\n\nBest regards\n`)
                    message += ` Please contact<br><a href="mailto:${this.config.contactEmail}?subject=${subject}&body=${body}">${this.config.contactEmail}</a><br>to request access.`
                }
                showSnackbarMessage({ message: message, ttl: 0, cssClass: 'error'})
            }
            this.shadowRoot?.querySelector('#search-filter')?.addEventListener('change', () => this.filterChanged())
            this.filterChanged()
            this.handleLocationChange()
        } catch(e) {
            console.error(e)
            showSnackbarMessage({ message: '' + e, ttl: 0, cssClass: 'error'})
        }
    }

    filterChanged(fromPager = false) {
        this.querySelector('#main')?.classList.add('loading')
        clearTimeout(this.debounceTimeout)
        this.debounceTimeout = setTimeout(async() => {
            try {
                this.searchTerm = this.searchField?.value || ''
                this.searchCreator = this.searchOwn?.checked ? this.config?.authUser : undefined
                if (fromPager) {
                    scrollTo(0, 0)
                } else {
                    this.offset = 0
                }
                const searchResult = await search(this.config!.index, {
                    offset: this.offset,
                    limit: this.limit,
                    sort: `lastModified desc`,
                    term: this.searchTerm,
                    creator: this.searchCreator,
                    facets: this.facets,
                })
                if (searchResult.error) {
                    throw searchResult.error.msg || searchResult.error.trace
                }
                this.totalHits = searchResult.response.numFound
                this.searchHits = searchResult.response.docs
            } catch(e) {
                console.error(e)
                showSnackbarMessage({ message: '' + e, ttl: 0, cssClass: 'error'})
            } finally {
                this.querySelector('#main')?.classList.remove('loading')
            }
        }, 20)
    }

    openEditor() {
        const editor = this.shadowRoot!.querySelector<Editor>('rdf-editor')
        if (editor) {
            editor.open = true
        }
    }

    async applyLayout(layout: string) {
        // set favicon
        let icon = document.head.querySelector<HTMLLinkElement>("link[rel='icon']")
        if (!icon) {
            icon = document.createElement('link')
            icon.rel = 'icon'
            document.head.appendChild(icon)
        }
        icon.href = new URL(`./layouts/${layout}/favicon.png`, import.meta.url).href
        document.body.dataset['layout'] = layout
        await import(`./layouts/${layout}/layout.ts`)
    }

    render() {
        return html`
        ${!this.config ? nothing : html`
        <layout-header>
            <div id="header-buttons">
            ${!this.config.authWriteAccess ? nothing : html `
                <rokit-button primary @click="${() => { this.openEditor() }}"><span class="material-icons">add</span>${i18n['add_resource']}</rokit-button>
            `}
            ${!this.config.authEnabled || this.config.authUser  ? nothing : html `
                <rokit-button primary href="${APP_PATH}oauth2/sign_in"><span class="material-icons icon">login</span>${i18n['sign_in']}</rokit-button>
            `}
            ${!this.config.authEnabled || !this.config.authUser ? nothing : html`
                <a id="sign-out" href="${APP_PATH}oauth2/sign_out">${i18n['sign_out']} ${this.config.authEmail || this.config.authUser}</a>
            `}
            </div>
        </layout-header>
        <div id="main">
            <rokit-progressbar class="progress"></rokit-progressbar>
            <div id="search-filter">
                <rokit-input id="search-field" label="${i18n['fulltextsearch']}" placeholder="${i18n['fulltextsearchplaceholder']}" value="${this.searchTerm}" clearable>
                    <span slot="prefix" class="material-icons icon">search</span>
                </rokit-input>
                ${!this.config.authUser ? nothing : html`
                    <div class="own"><label><input id="search-own" type="checkbox">${i18n['search_filter_own']}</label></div>
                `}
                ${!this.facets ? nothing : Object.keys(this.facets.facets).sort((a, b) => i18n[a]?.localeCompare(i18n[b])).map(profile => !this.facets?.hasValidFacet(profile) ? nothing : html`
                    <div class="profile-wrapper">
                        <header>${i18n[profile]}</header>
                        ${this.facets.facets[profile].sort((a, b) => a.label.localeCompare(b.label)).map((facet) => facet.valid ? html`${facet}` : nothing)}
                    </div>
                `)}
            </div>
            <rokit-splitpane class="flex-grow-1" minPos="25" maxPos="65">
                <div id="search-result" slot="pane1">
                    <div class="stats">
                        ${this.totalHits < 1 ?
                        html`<span>${i18n['noresults']}</span>` :
                        html`${i18n['results']} <span class="secondary">${this.offset + 1}</span> - <span class="secondary">${this.offset + this.searchHits.length}</span> ${i18n['of']} <span class="secondary">${this.totalHits}</span>: <span class="loading">${i18n['loading']}...</span>`}
                    </div>
                    ${this.totalHits < 1 ? nothing : html`
                        <div class="hits">
                        ${this.searchHits.map((hit) => html`
                            <div class="hit${hit.id === this.viewRdfSubject ? ' active' : ''}" @click="${() => this.viewResource(hit)}">
                                <div class="header">
                                    ${hit.label?.length ? hit.label.join(', ') : hit.id}
                                </div>
                                <div>${i18n['shape']}: ${hit.shape?.length ? (i18n[hit.shape[0]] ? i18n[hit.shape[0]] : hit.shape[0]) : 'No profile'}</div>
                                ${!hit.lastModified ? nothing : html`<div>Last modified: ${new Date(hit.lastModified).toDateString()}</div>`}
                            </div>`
                        )}
                        </div>
                        ${this.totalHits <= this.limit ? nothing : html`
                        <div class="pager">
                            ${i18n['pages']}:
                            ${map(range(1, Math.ceil(this.totalHits / this.limit) + 1), i => html`
                                <rokit-button ?primary="${this.offset == this.limit * (i - 1)}" disabled="${this.offset == this.limit * (i - 1) || nothing}" @click="${() => { this.offset = this.limit * (i - 1); this.filterChanged(true)}}">${i}</rokit-button>
                            `)}
                        </div>
                        `}
                    `
                    }
                </div>
                <rdf-viewer slot="pane2"
                    rdfSubject="${this.viewRdfSubject}"
                    rdfNamespace="${this.config.rdfNamespace}"
                    highlightSubject="${this.viewHiglightSubject}"
                    .config="${this.config}"
                    @delete="${() => { this.viewResource(null); this.filterChanged() }}"
                ></rdf-viewer>
            </rokit-splitpane>
        ${!this.config.authWriteAccess ?  nothing : html`
            <rdf-editor
                .profiles="${this.config.profiles}"
                rdfNamespace="${this.config.rdfNamespace}"
                @saved="${(event: CustomEvent) => { this.filterChanged(); this.viewResource(event.detail.id) }}"
            ></rdf-editor>
        `}
        </div>
        <layout-footer></layout-footer>
        `}
       <rokit-snackbar></rokit-snackbar>
    `
    }
}