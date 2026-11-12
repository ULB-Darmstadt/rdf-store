import { LitElement, css, html } from 'lit'
import { customElement, property, state } from 'lit/decorators.js'
import { i18n } from './i18n'
import { BACKEND_URL } from './constants'
import { ProfileHierarchy, type ProfileSummary } from './profile-hierarchy'
import { globalStyles } from './styles'

@customElement('profile-chooser')
export class ProfileChooser extends LitElement {
    static styles = [globalStyles, css`
        :host { display: block; flex: none; height: fit-content; min-height: 0; }
        rokit-dialog::part(dialog) { min-height: 80vh; width: min(90vw, 600px); }
        .main { display: flex; flex-direction: column; flex-grow: 1; }
        .profile-filter-button {
            margin: 8px;
            align-self: flex-start;
        }
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
        .profile-loading {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            flex-grow: 1;
            gap: 12px;
            padding: 32px;
        }
        .loading-spinner {
            width: 32px;
            height: 32px;
            border: 4px solid #e0e0e0;
            border-top-color: var(--secondary-color);
            border-radius: 50%;
            animation: profile-spin 0.8s linear infinite;
        }
        @keyframes profile-spin {
            to {
                transform: rotate(360deg);
            }
        }
        .profile-error {
            padding: 24px;
            text-align: center;
            color: #b00020;
        }
    `]

    @property()
    selectedProfile = ''
    @property()
    open = false
    @state()
    private profileSummaries?: ProfileSummary[]

    @state()
    private loadingProfiles = false

    @state()
    private profileLoadError = false

    @property({ attribute: false })
    defaultProfileIds: string[] = []

    @state()
    private validDefaultProfileIds: string[] = []

    private get hasDefaultProfiles(): boolean {
        return this.validDefaultProfileIds.length > 0
    }

    @state()
    showAllProfiles = this.defaultProfileIds.length === 0

    @property()
    index = ''
    @property({ type: Boolean })
    showHint = true

    private async openProfileDialog() {
        // Dialog sofort öffnen
        this.open = true
        this.profileLoadError = false

        // Bereits geladene Profile wiederverwenden
        if (this.profileSummaries !== undefined) {
            this.updateValidDefaultProfiles()
            return
        }

        // Ladeanzeige aktivieren
        this.loadingProfiles = true

        try {
            const response = await fetch(`${BACKEND_URL}/profiles`)

            if (!response.ok) {
                throw new Error(
                    `Failed loading profiles: ${response.status}`
                )
            }

            this.profileSummaries =
                await response.json() as ProfileSummary[]

            this.updateValidDefaultProfiles()

        } catch (error) {
            console.error('Could not load or validate profiles:', error)
            this.profileLoadError = true
            this.validDefaultProfileIds = []

        } finally {
            this.loadingProfiles = false
        }
    }

    private updateValidDefaultProfiles() {
        if (this.profileSummaries === undefined) {
            return
        }

        const availableIds = new Set(
            this.profileSummaries
                .filter(profile => !profile.id.startsWith('urn:'))
                .map(profile => profile.id)
        )

        this.validDefaultProfileIds = this.defaultProfileIds.filter(
            id => !id.startsWith('urn:') && availableIds.has(id)
        )

        this.showAllProfiles = !this.hasDefaultProfiles
    }
    
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
                <rokit-button
                    class="select-profile-button"
                    @click="${() => this.openProfileDialog()}"
                >
                    ${i18n['selectprofile']}...
                </rokit-button>

                ${this.showHint
                    ? html`<div class="selectprofile-hint">${i18n['selectprofile-hint']}</div>`
                    : ''}
            `}

            <rokit-dialog
                .open="${this.open}"
                closable
                @close="${() => this.open = false}"
            >
                <div slot="header">${i18n['selectprofile']}</div>

                <div class="main">

                    ${this.loadingProfiles ? html`
                        <div class="profile-loading" role="status">
                            <div class="loading-spinner"></div>
                            <span>Profile werden geladen ...</span>
                        </div>
                    ` : this.profileLoadError ? html`
                        <div class="profile-error">
                            <p>Die Profile konnten nicht geladen werden.</p>
                            <rokit-button
                                @click="${() => this.openProfileDialog()}"
                            >
                                Erneut versuchen
                            </rokit-button>
                        </div>
                    ` : html`

                        ${this.hasDefaultProfiles ? html`
                            <rokit-button
                                class="profile-filter-button"
                                @click="${() => this.showAllProfiles = !this.showAllProfiles}"
                            >
                                ${this.showAllProfiles
                                    ? i18n['show-default-profiles']
                                    : i18n['show-all-profiles']}
                            </rokit-button>
                        ` : ''}

                        <profile-hierarchy
                                .index="${this.index}"
                                .profileSummaries="${this.profileSummaries}"
                                .active="${this.open}"
                                .profileIds="${this.hasDefaultProfiles && !this.showAllProfiles
                                        ? this.validDefaultProfileIds
                                        : undefined}"
                                @change="${(event: Event) => {
                                    event.stopPropagation()
                                    this.selectProfile((event.currentTarget as ProfileHierarchy).selectedProfile)
                                }}"
                        ></profile-hierarchy>
                    `}
                
                </div>
            </rokit-dialog>
        `
    }
}
