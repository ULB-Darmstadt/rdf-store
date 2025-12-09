import { customElement, property, state } from 'lit/decorators.js'
import { LitElement, PropertyValues, css, html } from 'lit'
import '@ulb-darmstadt/shacl-form/plugins/leaflet.js'
import { BACKEND_URL } from './constants'
import { globalStyles } from './styles'

@customElement('shacl-viewer')
export class Viewer extends LitElement {
    static styles = [globalStyles, css`
        rokit-dialog::part(dialog) { height: 90vh; width: min(90vw, 600px); }
        .main { display: flex; flex-direction: column; flex-grow: 1; }
        .main shacl-form { flex-grow: 1; }
    `]
    @property()
    rdfSubject = ''
    @state()
    rdf = ''
    @state()
    graphView = false

    updated(changedProperties: PropertyValues) {
        if (changedProperties.has('rdfSubject') && this.rdfSubject) {
            (async() => {
                this.rdf = await fetch(`${BACKEND_URL}/proxy?url=${this.rdfSubject}`).then(r => r.text())
            })()
        }
    }

    close() {
        this.rdfSubject = ''
        this.rdf = ''
        this.dispatchEvent(new Event('close'))
    }

    render() {
        return html `<rokit-dialog class="viewer-dialog" .open="${this.rdfSubject}" closable @close="${() => this.close()}">
            <div slot="header">
                <rokit-button ?primary="${!this.graphView}" @click="${() => this.graphView = false}">Form view</rokit-button>
                <rokit-button ?primary="${this.graphView}" @click="${() => this.graphView = true}">Graph view</rokit-button>
            </div>
            <div class="main">
            ${this.graphView ? html`
                Not implemented yet
            ` : html`
                <shacl-form
                    data-values="${this.rdf}"
                    data-values-subject="${this.rdfSubject}"
                    data-proxy="${BACKEND_URL}/proxy?url="
                    data-hierarchy-colors
                    data-show-root-shape-label
                ></shacl-form>
            `}
            </div>
        </rokit-dialog>`
    }
}
