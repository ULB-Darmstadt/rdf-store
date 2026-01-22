import { customElement, property, state } from 'lit/decorators.js'
import { LitElement, PropertyValues, css, html, nothing } from 'lit'
import '@ulb-darmstadt/shacl-form/plugins/leaflet.js'
import { BACKEND_URL } from './constants'
import { globalStyles } from './styles'
import './graph'
import { i18n } from './i18n'
import { showSnackbarMessage } from '@ro-kit/ui-widgets'
import { ShaclForm } from '@ulb-darmstadt/shacl-form'
import { Config } from '.'
import { classInstanceProvider } from './editor'

@customElement('rdf-viewer')
export class Viewer extends LitElement {
    static styles = [globalStyles, css`
        :host { --background-color: #F5F5F5; position: relative; background-color: var(--background-color); display: flex; flex-direction: column; }
        .main { display: flex; flex-direction: column; flex-grow: 1; padding: 5px; }
        .header { display: flex; gap: 5px; align-items: center; border-bottom: 2px solid #CCC; padding: 2px 5px 2px 5px; background-color: #EEE; }
        shacl-form, rdf-graph { flex-grow: 1; }
        .placeholder { display: flex; justify-content: center; align-items: center; flex-grow: 1; color: #888; }
        .arrow-left:before { content: '\\21E6'; font-size: 28px; padding-right: 10px; }
        .header rokit-button[text] { margin-bottom: -4px; border-bottom: 2px solid transparent; }
        .header rokit-button[text][primary] { border-bottom: 2px solid var(--rokit-primary-color) }
        .spacer { flex-grow: 1; }
        #delete-button { --rokit-light-background-color: #FEE; color: #F00; }
    `]
    @property()
    rdfSubject = ''
    @property()
    rdfNamespace = ''
    @property()
    highlightSubject = ''
    @property()
    config?: Config
    @state()
    editable = false
    @state()
    rdf = ''
    @state()
    graphView = true
    @state()
    editMode = false
    @state()
    saving = false

    updated(changedProperties: PropertyValues) {
        if ((changedProperties.has('rdfSubject') || changedProperties.has('highlightSubject')) && this.rdfSubject) {
            this.highlightSubject = this.highlightSubject || this.rdfSubject
            this.editMode = false
            this.editable = false
            this.load()
        }
        if ((changedProperties.has('graphView') || changedProperties.has('editMode')) && !this.graphView && this.editMode) {
            (this.shadowRoot!.querySelector('shacl-form') as ShaclForm)?.setClassInstanceProvider(classInstanceProvider)
        }
    }

    private async load() {
        if (this.rdfSubject) {
            try {
                const resp = await fetch(`${BACKEND_URL}/resource/${encodeURIComponent(this.rdfSubject)}?union`)
                if (resp.ok) {
                    this.rdf = await resp.text()
                    // check if editable
                    const creator = resp.headers.get('X-Creator')
                    this.editable = (!this.config?.authEnabled || (this.config?.authUser && this.config?.authUser === creator)) ? true : false
                } else {
                    throw new Error(i18n['noresults'])
                }
            } catch(e) {
                showSnackbarMessage({message: '' + e, ttl: 0, cssClass: 'error' })
            }
        }
    }

    private export() {
        if (this.rdf) {
          const link = document.createElement('a')
          link.href = window.URL.createObjectURL(new Blob([this.rdf], { type: "text/turtle" }))
          link.download = 'metadata.ttl'
          link.click()
        }
    }

    private async save() {
        const form = this.shadowRoot?.querySelector<ShaclForm>('#form')
        if (!form) {
            showSnackbarMessage({message: 'form not found', cssClass: 'error' })
            return
        }
        if (form.form.reportValidity()) {
            const report = await form.validate() as any
            const ttl = form.serialize()
            if (report.conforms) {
                this.saving = true
                const formData = new URLSearchParams()
                formData.append('ttl', ttl)
                try {
                    const resp = await fetch(`${BACKEND_URL}/resource/${encodeURIComponent(this.rdfSubject)}`, { method: 'PUT', cache: 'no-cache', body: formData })
                    if (!resp.ok) {
                        let message = i18n['resource_save_failed'] + '<br><small>Status: ' + resp.status + '</small>'
                        const contentType = resp.headers.get('content-type')
                        if (contentType?.includes('application/json')) {
                            const data = await resp.json()
                            if (data.error) {
                                message += '<br><small>' + i18n['error'] + ': ' + data.error + '</small>'
                            }
                        }
                        showSnackbarMessage({message: message, ttl: 0, cssClass: 'error' })
                    } else {
                        showSnackbarMessage({ message: i18n['resource_save_succeeded'], cssClass: 'success' })
                        this.editMode = false
                        this.load()
                    }
                } catch(e) {
                    showSnackbarMessage({message: '' + e, ttl: 0, cssClass: 'error' })
                } finally {
                    this.saving = false
                }
            } else {
                console.log(ttl)
                console.warn(report)
            }
        }
    }

    private async delete() {
        try {
            const url = BACKEND_URL + '/resource/' + encodeURIComponent(this.rdfSubject)
            const resp = await fetch(url, { method: 'DELETE', cache: 'no-cache' })
            if (!resp.ok) {
                let message = i18n['resource_delete_failed'] + '<br><small>Status: ' + resp.status + '</small>'
                const contentType = resp.headers.get('content-type')
                if (contentType?.includes('application/json')) {
                    const data = await resp.json()
                    if (data.error) {
                        message += '<br><small>' + i18n['error'] + ': ' + data.error + '</small>'
                    }
                }
                throw(message)
            }
            this.rdfSubject = ''
            this.highlightSubject = ''
            this.rdf = ''
            this.editMode = false
            showSnackbarMessage({ message: i18n['resource_delete_succeeded'], cssClass: 'success' })
            this.dispatchEvent(new Event('delete'))
        } catch(e) {
            showSnackbarMessage({message: '' + e, ttl: 0, cssClass: 'error' })
        }
    }

    render() {
        return this.rdf ? html`
            <div class="header">
            ${this.editMode ? html`
                <rokit-button id="delete-button" @click="${this.delete}" ?disabled="${this.saving}"><span class="material-icons">delete</span>${i18n['delete']}</rokit-button>
                <div class="spacer"></div>
                <rokit-button @click="${() => { this.editMode = false }}" ?disabled="${this.saving}">${i18n['cancel']}</rokit-button>
                <rokit-button primary @click="${this.save}" ?disabled="${this.saving}" class="${this.saving ? 'loading' : ''}"><span class="material-icons">cloud_upload</span>${i18n['save']}</rokit-button>
            ` : html`
                <rokit-button ?primary="${this.graphView}" text @click="${() => this.graphView = true}">${i18n['graph_view']}</rokit-button>
                <rokit-button ?primary="${!this.graphView}" text @click="${() => this.graphView = false}">${i18n['detail_view']}</rokit-button>
                <div class="spacer"></div>
                ${!this.editable ? nothing : html`
                    <rokit-button @click="${() => { this.editMode = true; this.graphView = false }}"><span class="material-icons">edit</span>${i18n['edit']}</rokit-button>
                `}
                <rokit-button @click="${() => { this.export() }}"><span class="material-icons">download</span>${i18n['export']}</rokit-button>
            `}
            </div>
            <div class="main">
            ${this.graphView ? html`
                <rdf-graph rdfSubject="${this.rdfSubject}" highlightSubject="${this.highlightSubject}" rdf="${this.rdf}"></rdf-graph>
            ` : html`
                <shacl-form
                    id="form"
                    data-values="${this.rdf}"
                    data-values-subject="${this.rdfSubject}"
                    data-values-namespace="${this.rdfNamespace}"
                    data-proxy="${BACKEND_URL}/rdfproxy?url="
                    data-hierarchy-colors
                    ?data-view=${!this.editMode}
                    data-show-root-shape-label
                ></shacl-form>
            `}
            </div>
        ` :
        html`
            <div class="placeholder">
                <span class="arrow-left"></span> ${i18n['click_hit_to_view']}
            </div>
        `
    }
}
