import { LitElement, css, html } from 'lit'
import { customElement, property } from 'lit/decorators.js'
import { i18n } from './i18n'
import { ProfileHierarchy } from './profile-hierarchy'
import { globalStyles } from './styles'

@customElement('profile-chooser')
export class ProfileChooser extends LitElement {
    static styles = [globalStyles, css`
        :host { display: block; flex: none; height: fit-content; min-height: 0; }
        rokit-dialog::part(dialog) { min-height: 80vh; width: min(90vw, 600px); }
        .main { display: flex; flex-direction: column; flex-grow: 1; }
        .selected-profile { width: 100%; }
        .selected-profile::part(input) { color: var(--secondary-color); }
        .select-profile-button { margin: 8px; }
        .selectprofile-hint {
            margin: 0 8px;
            padding: 2px 10px 8px 10px;
            color: #555;
            line-height: 1.2;
            background: #FFF4EB;
            border: 1px solid #FFD2AD;
            border-radius: 6px;
        }
        .selectprofile-hint::before {
            content: '↑';
            display: block;
            color: var(--secondary-color);
            font-size: 1.25rem;
            font-weight: 900;
        }
    `]

    @property()
    selectedProfile = ''
    @property()
    open = false
    @property()
    index = ''
    @property({ type: Boolean })
    showHint = true
    private selectProfile(profile: string) {
        this.selectedProfile = profile
        this.open = false
        this.dispatchEvent(new Event('change', { bubbles: true, composed: true }))
    }

    render() {
        return html`
            ${this.selectedProfile ? html`
                <rokit-textarea clearable disabled resize="auto" class="selected-profile" label="${i18n['selectedprofile']}" value="${i18n[this.selectedProfile] || this.selectedProfile}" @change="${(event: Event) => {
                    event.stopPropagation()
                    this.selectProfile('')
                }}"></rokit-textarea>
            ` : html`
                <rokit-button class="select-profile-button" @click="${() => this.open = true}">${i18n['selectprofile']}...</rokit-button>
                ${this.showHint ? html`<div class="selectprofile-hint">${i18n['selectprofile-hint']}</div>` : ''}
            `}
            <rokit-dialog .open="${this.open}" closable @close="${() => this.open = false}">
                <div slot="header">${i18n['selectprofile']}</div>
                <div class="main">
                    <profile-hierarchy
                        .index="${this.index}"
                        .active="${this.open}"
                        @change="${(event: Event) => {
                            event.stopPropagation()
                            this.selectProfile((event.currentTarget as ProfileHierarchy).selectedProfile)
                        }}"
                    ></profile-hierarchy>
                </div>
            </rokit-dialog>
        `
    }
}
