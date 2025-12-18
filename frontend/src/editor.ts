import { customElement, property, query, } from 'lit/decorators.js'
import { LitElement, PropertyValues, css, html } from 'lit'
import { ShaclForm } from '@ulb-darmstadt/shacl-form'
import '@ulb-darmstadt/shacl-form/plugins/leaflet.js'
import { BACKEND_URL } from './constants'
import { i18n } from './i18n'
import { RokitSnackbar, RokitSnackbarEvent, showSnackbarMessage } from '@ro-kit/ui-widgets'
import { globalStyles } from './styles'

@customElement('shacl-editor')
export class Editor extends LitElement {
    static styles = [globalStyles, css`
        rokit-dialog::part(dialog) { min-height: min(434px, 90vh); width: min(90vw, 600px); }
        .main { display: flex; flex-direction: column; flex-grow: 1; }
        .main shacl-form { flex-grow: 1; }
        .buttons { display: flex; gap:16px; justify-content: space-between; padding-top: 16px; align-items: center; }
        #delete-button { --rokit-light-background-color: #FEE; color: #F00; }
    `]
    @property()
    profiles?: string[]
    @property()
    selectedShape = ''
    @property()
    rdfNamespace = ''
    @property()
    open = false
    @property()
    saving = false
    @query('shacl-form')
    form?: ShaclForm

    updated(changedProperties: PropertyValues) {
        if (changedProperties.has('open') && this.open) {
            setTimeout(() => {
                this.shadowRoot?.querySelector<HTMLInputElement>('rokit-select')?.focus()    
            })
        }
        if (changedProperties.has('selectedShape') && this.selectedShape) {
            this.form!.setClassInstanceProvider(clazz => {
                const url = BACKEND_URL + '/sparql/query'
                return new Promise<string>(async (resolve, reject) => {
                    const formData = new URLSearchParams()
                    // load class instances from all graphs
                    formData.append('query', `CONSTRUCT { ?s ?p ?o } WHERE { GRAPH ?g { ?c a <${clazz}> . ?c (<>|!<>)* ?s . ?s ?p ?o }}`)
                    try {
                        const resp = await fetch(url, {
                            method: "POST",
                            cache: "no-cache",
                            body: formData
                        })
                        if (resp.status !== 200) {
                            throw new Error('server returned status ' + resp.status)
                        }
                        const result = await resp.text()
                        resolve(result)
                    } catch(e) {
                        reject(e)
                    }
                })
            })
        }
    }

    async saveRDF(ttl: string) {
        this.saving = true
        const formData = new URLSearchParams()
        formData.append('ttl', ttl)
        try {
            const resp = await fetch(`${BACKEND_URL}/resource`, { method: 'POST', cache: 'no-cache', body: formData })
            if (!resp.ok) {
                let message = i18n['resource_save_failed'] + '<br><small>Status: ' + resp.status + '</small>'
                const contentType = resp.headers.get('content-type')
                if (contentType?.includes('application/json')) {
                    const data = await resp.json()
                    if (data.error) {
                        message += '<br><small>' + i18n['error'] + ': ' + data.error + '</small>'
                    }
                }
                this.showErrorMessage(message)
            } else {
                this.close()
                this.dispatchEvent(new Event('saved'))
                document.dispatchEvent(new RokitSnackbarEvent({ message: i18n['resource_save_succeeded'], cssClass: 'success' }))
            }
        } catch(e) {
            this.showErrorMessage('' + e)
        } finally {
            this.saving = false
        }
    }

    showErrorMessage(text: string) {
        showSnackbarMessage({ message: text, ttl: 0, cssClass: 'error'}, this.shadowRoot!.querySelector<RokitSnackbar>('rokit-snackbar') || undefined)
    }

    close() {
        this.open = false
        this.selectedShape = ''
    }

    render() {
        return html `<rokit-dialog class="editor-dialog" .open="${this.open}" closable @close="${() => this.close()}">
            <div slot="header">${this.selectedShape ? i18n['new'] + ' ' + i18n[this.selectedShape] : i18n['add_resource']}</div>
            <div class="main">
            ${this.selectedShape ? html`
                <shacl-form
                    data-shape-subject="${this.selectedShape}"
                    data-shapes-url="${this.selectedShape}"
                    data-values-namespace="${this.rdfNamespace}"
                    data-proxy="${BACKEND_URL}/proxy?url="
                    data-hierarchy-colors
                ></shacl-form>
                <div class="buttons">
                    <span></span>
                    <rokit-button primary ?disabled="${this.saving}" class="${this.saving ? 'loading' : ''}" @click="${async () => {
                        if (this.form!.form.reportValidity()) {
                            const report = await this.form!.validate() as any
                            if (report.conforms) {
                                this.saveRDF(this.form!.serialize())
                            } else {
                                console.log(this.form!.serialize())
                                console.warn(report)
                            }
                        }
                    }}"><span class="material-icons">cloud_upload</span>${i18n['save']}</rokit-button>
                </div>
                <rokit-snackbar></rokit-snackbar>
            ` : html`
                <rokit-select label="${i18n['selectprofile']}" sort tabindex="-1" fixedOpen @change="${(ev: Event) => this.selectedShape = (ev.target as HTMLSelectElement).value }">
                    <ul>
                        ${this.profiles?.map((id) => html`<li data-value="${id}">${i18n[id] || id}</li>`)}
                    </ul>
                </rokit-select>
            `}
            </div>
        </rokit-dialog>`
    }
}
