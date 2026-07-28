import { customElement, property, state } from 'lit/decorators.js'
import { LitElement, PropertyValues, css, html } from 'lit'
import '@ulb-darmstadt/shacl-form/plugins/leaflet.js'
import { BACKEND_URL } from './constants'
import { fetchLabels, i18n } from './i18n'
// import  '@ro-kit/ui-widgets'
import { globalStyles } from './styles'

@customElement('profile-chooser')
export class ProfileChoose extends LitElement {
    static styles = [globalStyles, css`
        :host { }
        rokit-dialog::part(dialog) { min-height: min(434px, 90vh); width: min(90vw, 600px); }
        .main { display: flex; flex-direction: column; flex-grow: 1; }
        .selected-profile { width: 100%; }
        .selected-profile::part(input) { color: var(--secondary-color); }
        .select-profile-button { margin: 8px; }
        .selectprofile-hint {
            margin: 0 8px;
            padding: 8px 10px;
            color: #555;
            line-height: 1.4;
            text-align2: center;
            background: #FFF4EB;
            border: 1px solid #FFD2AD;
            border-radius: 6px;
        }
        .selectprofile-hint::before {
            content: '↑';
            display: block;
            margin-bottom: 4px;
            color: var(--secondary-color);
            font-size: 1.25rem;
            font-weight: 900;
        }
    `]
    @property()
    profiles?: string[]
    @property()
    selectedShape = '2'
    @property()
    open = false
    @state()
    loading = false

    willUpdate(changedProperties: PropertyValues) {
        if (changedProperties.has('profiles')) {
            this.loading = true
            // void this.loadProfiles(this.profiles)
        }
    }

    render() {
        return html`
            ${this.selectedShape ? html`
                <rokit-input clearable disabled class="selected-profile" label="${i18n['selectedprofile']}" value="${i18n[this.selectedShape] || this.selectedShape}" @change="${() => this.selectedShape = ''}"></rokit-input>
            ` : html `
                <rokit-button class="select-profile-button" @click="${() => this.open = true}">${i18n['selectprofile']}...</rokit-button>
                <div class="selectprofile-hint">${i18n['selectprofile-hint']}</div>
            `}
            <rokit-dialog .open="${this.open}" closable @close="${() => this.open = false }">
                <div slot="header">${i18n['selectprofile']}</div>
                <div class="main">
                </div>
            </rokit-dialog>
        `
    }
}