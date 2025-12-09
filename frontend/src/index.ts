import { customElement, property, query, state } from 'lit/decorators.js'
import { LitElement, html, nothing, unsafeCSS } from 'lit'
import '@fontsource/roboto'
import '@fontsource/material-icons'
import styles from './styles.css?inline'
import { globalStyles } from './styles'
import './editor'
import './viewer'
import { BACKEND_URL } from './constants'
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
    editorRdfSubject = ''
    @state()
    viewerRdfSubject = ''
    @state()
    viewerRdf = ''
    @state()
    config: Config | undefined

    @query('#search-field')
    searchField?: RokitInput
    @query('#search-own')
    searchOwn?: HTMLInputElement

    debounceTimeout: ReturnType<typeof setTimeout> | undefined

    async firstUpdated() {
        try {
            const resp = await fetch(`${BACKEND_URL}/config`)
            if (!resp.ok) {
                throw 'Failed loading application configuration'
            }
            this.config = await resp.json() as Config
            // this.config = Object.assign (this.config!, { authUser: '633fe62d-132c-44ca-91ae-b1a425f80837', authEmail:'test@sdfksjdhfdsjnfkdsjfkdsfjh.com', authWriteAccess: true })

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

    openEditor(subject?: string) {
        this.editorRdfSubject = subject || ''
        const editor = this.shadowRoot!.querySelector<Editor>('shacl-editor')
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
        ${this.config === undefined ? nothing : html`
        <layout-header>
            <div id="header-buttons">
            ${!this.config.authWriteAccess ? nothing : html `
                <rokit-button primary @click="${() => { this.openEditor() }}"><span class="material-icons">add</span>${i18n['add_resource']}</rokit-button>
            `}
            ${!this.config.authEnabled || this.config.authUser  ? nothing : html `
                <rokit-button primary href="./oauth2/sign_in"><span class="material-icons icon">login</span>${i18n['sign_in']}</rokit-button>
            `}
            ${!this.config.authEnabled || !this.config.authUser ? nothing : html`
                <a id="sign-out" href="./oauth2/sign_out">${i18n['sign_out']} ${this.config.authEmail || this.config.authUser}</a>
            `}
            </div>
        </layout-header>
        <div id="main">
            <rokit-progressbar class="progress"></rokit-progressbar>
            <div id="search-filter">
                <rokit-input id="search-field" dense label="${i18n['fulltextsearch']}" placeholder="${i18n['fulltextsearchplaceholder']}" value="${this.searchTerm}" clearable>
                    <span slot="prefix" class="material-icons icon">search</span>
                </rokit-input>
                ${this.config.authUser ? html`
                    <div class="own"><label><input id="search-own" type="checkbox">${i18n['search_filter_own']}</label></div>
                ` : nothing}
                ${!this.facets ? nothing : Object.keys(this.facets.facets).sort((a, b) => i18n[a]?.localeCompare(i18n[b])).map(profile => !this.facets?.hasValidFacet(profile) ? nothing : html`
                    <div class="profile-wrapper">
                        <header>${i18n[profile]}</header>
                        ${this.facets.facets[profile].sort((a, b) => a.label.localeCompare(b.label)).map((facet) => facet.valid ? html`${facet}` : nothing)}
                    </div>
                `)}
            </div>
            <div id="search-result">
                <div class="stats">
                    ${this.totalHits < 1 ?
                    html`<span>${i18n['noresults']}</span>` :
                    html`${i18n['results']} <span class="secondary">${this.offset + 1}</span> - <span class="secondary">${this.offset+this.searchHits.length}</span> ${i18n['of']} <span class="secondary">${this.totalHits}</span>: <span class="loading">${i18n['loading']}...</span>`}
                </div>
                ${this.totalHits < 1 ? nothing : html`
                    <div class="hits">
                    ${this.searchHits.map((hit) => html`
                        <div class="card">
                            ${!hit.shape?.length ? nothing : html`<div class="header">${i18n[hit.shape[0]]}</div>`}
                            <shacl-form
                                data-view
                                data-loading=""
                                data-proxy="${BACKEND_URL}/proxy?url="
                                data-collapse
                                data-shape-subject="${hit.shape[0]}"
                                data-shapes-url="${hit.shape[0]}"
                                data-values="${hit.rdf}"
                                data-values-subject="${decodeURIComponent(hit.id)}">
                            </shacl-form>
                            ${hit._nest_parent_ === hit._root_ && (!this.config?.authEnabled || (this.config?.authUser && this.config?.authUser == hit.creator)) ? html`<rokit-button class="edit-button" icon @click="${() => { this.openEditor(decodeURIComponent(hit.id)) }}"><span class="material-icons">edit</span></rokit-button>` : nothing }
                            ${hit._nest_parent_ === hit._root_ ? nothing : html`
                                Used by <a @click="${() => { this.viewerRdfSubject = hit._nest_parent_; this.viewerRdf = hit.rdf }}">${hit._nest_parent_}</a>
                            `}
                        </div>`
                    )}
                    </div>
                    ${this.totalHits <= this.limit ? nothing : html`
                    <div class="pager">
                        ${i18n['pages']}:
                        ${map(range(1, Math.ceil(this.totalHits / this.limit) + 1), (i) => html`
                            <rokit-button variant="${this.offset == this.limit*(i - 1) ? 'filled' : 'text' }" disabled="${this.offset == this.limit*(i - 1) || nothing}" @click="${() => { this.offset=this.limit*(i - 1); this.filterChanged(true)}}">${i}</rokit-button>
                        `)}
                    </div>
                    `}
                `
                }
            </div>
            ${this.config.authWriteAccess ? html`
                <shacl-editor .profiles="${this.config.profiles}" .rdfSubject="${this.editorRdfSubject}" .rdfNamespace="${this.config.rdfNamespace}"
                    @saved="${() => { this.filterChanged() }}"
                    @close="${() => { this.editorRdfSubject = '' }}"
                ></shacl-editor>`: nothing}
            <shacl-viewer .rdfSubject="${this.viewerRdfSubject}" .rdf="${this.viewerRdf}" @close="${() => { this.viewerRdfSubject = '' }}">
        </div>
        <layout-footer></layout-footer>
        `}
       <rokit-snackbar></rokit-snackbar>
    `
    }
}