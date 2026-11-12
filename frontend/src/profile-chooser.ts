import { LitElement, css, html } from 'lit'
import { customElement, property, state } from 'lit/decorators.js'
import { i18n } from './i18n'
import { ProfileHierarchy } from './profile-hierarchy'
import { globalStyles } from './styles'

const defaultProfiles = import.meta.glob<{
    DEFAULT_PROFILE_IDS?: readonly string[]
}>(
    './default-profiles.ts',
    { eager: true }
)

const DEFAULT_PROFILE_IDS =
    defaultProfiles['./default-profiles.ts']?.DEFAULT_PROFILE_IDS ?? []

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
    `]

    @property()
    selectedProfile = ''
    @property()
    open = false
    
    private readonly defaultProfileIds = [...DEFAULT_PROFILE_IDS]

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
                    @click="${async () => {
                        this.validDefaultProfileIds = []

                        if (this.defaultProfileIds.length > 0) {
                        try {
                            const response = await fetch(`${BACKEND_URL}/profiles`)

                            if (!response.ok) {
                                throw new Error(`Failed loading profiles: ${response.status}`)
                            }

                            const summaries = await response.json() as { id: string }[]

                            const availableIds = new Set(
                                summaries
                                    .filter(profile => !profile.id.startsWith('urn:'))
                                    .map(profile => profile.id)
                            )

                            this.validDefaultProfileIds = this.defaultProfileIds.filter(
                                id => !id.startsWith('urn:') && availableIds.has(id)
                            )

                        } catch (error) {
                            console.error('Could not validate default profiles:', error)
                            this.validDefaultProfileIds = []
                        }
                    }

                    this.showAllProfiles = !this.hasDefaultProfiles
                    this.open = true
                }}"
                >
                ${i18n['selectprofile']}...
            </rokit-button>
                ${this.showHint ? html`<div class="selectprofile-hint">${i18n['selectprofile-hint']}</div>` : ''}
            `}
            <rokit-dialog .open="${this.open}" closable @close="${() => this.open = false}">
                <div slot="header">${i18n['selectprofile']}</div>
                <div class="main">
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
                        .active="${this.open}"
                        .profileIds="${this.hasDefaultProfiles && !this.showAllProfiles
                                    ? this.validDefaultProfileIds
                                    : undefined}"
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
