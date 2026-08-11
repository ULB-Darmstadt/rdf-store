import { customElement, property, query, state } from 'lit/decorators.js'
import { LitElement, PropertyValues, TemplateResult, css, html } from 'lit'
import '@ulb-darmstadt/shacl-form/plugins/leaflet.js'
import { RokitSelect } from '@ro-kit/ui-widgets'
import { BACKEND_URL } from './constants'
import { fetchLabels, i18n } from './i18n'
import { AggregationFacet, executeSolrRequest } from './solr'
import { globalStyles } from './styles'

interface Profile {
    id: string
    docCount: number
    children: Profile[]
}

interface ProfileSummary {
    id: string
    parents: string[]
}

@customElement('profile-chooser')
export class ProfileChooser extends LitElement {
    static styles = [globalStyles, css`
        :host { display: block; flex: none; height: fit-content; min-height: 0; }
        rokit-dialog::part(dialog) { min-height: 80vh; width: min(90vw, 600px); }
        .main { display: flex; flex-direction: column; flex-grow: 1; }
        rokit-select { flex-grow: 1; border-bottom-color: #F5F5F5; background-color: white; }
        rokit-select::part(collapsible-header) { background-color: #F5F5F5 !important; }
        rokit-select::part(profile-count)::after { content: attr(data-count); color: var(--secondary-color); display: inline-block; font-family: monospace; margin-left: 7px; font-size: 12px; }
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
    @state()
    loading = false
    @state()
    profiles?: Profile[]
    @query('rokit-select')
    profileSelect?: RokitSelect

    willUpdate(pv: PropertyValues) {
        if (pv.has('index')) {
            this.profiles = undefined
        }
        if (this.open && this.index && this.profiles === undefined && !this.loading) {
            this.loadProfiles()
        }
    }

    async updated(pv: PropertyValues) {
        if (pv.has('open') && this.open) {
            await this.profileSelect?.updateComplete
            await this.profileSelect?.input.updateComplete
            this.profileSelect?.input.inputElement.focus()
        }
    }

    async loadProfiles() {
        if (this.loading) {
            return
        }
        this.loading = true
        try {
            const [response, solrResponse] = await Promise.all([
                fetch(`${BACKEND_URL}/profiles`),
                executeSolrRequest(this.index, {
                    query: '*',
                    limit: 0,
                    offset: 0,
                    facet: {
                        profiles: {
                            type: 'terms',
                            field: 'shape',
                            limit: -1
                        }
                    }
                })
            ])
            if (!response.ok) {
                throw new Error(`Failed loading profiles: ${response.status}`)
            }
            if (solrResponse.error) {
                throw new Error(solrResponse.error.msg || 'Failed loading profiles from Solr')
            }
            const facet = solrResponse.facets?.profiles as AggregationFacet | undefined
            const profileCounts = new Map(
                (facet?.buckets || [])
                    .filter(bucket => (bucket.count ?? 0) > 0)
                    .map(bucket => [String(bucket.val), bucket.count ?? 0])
            )
            const summaries = (await response.json() as ProfileSummary[])
                .filter(profile => !profile.id.startsWith('urn:'))
                .filter(profile => profileCounts.has(profile.id))
            const profiles = summaries.map(profile => profile.id)
            const profileIds = new Set(profiles)
            const parentIds = new Map(summaries.map(profile => [
                profile.id,
                new Set(profile.parents.filter(parent => profileIds.has(parent)))
            ]))
            await fetchLabels(profiles, true)

            const childIds = new Map<string, string[]>()
            for (const [id, parents] of parentIds) {
                for (const parent of parents) {
                    childIds.set(parent, [...(childIds.get(parent) || []), id])
                }
            }
            const roots = profiles.filter(id => !parentIds.get(id)?.size)
            const buildProfile = (id: string, ancestors = new Set<string>()): Profile => {
                const nextAncestors = new Set(ancestors).add(id)
                return {
                    id,
                    docCount: profileCounts.get(id) || 0,
                    children: (childIds.get(id) || [])
                        .filter(child => !nextAncestors.has(child))
                        .map(child => buildProfile(child, nextAncestors))
                }
            }
            this.profiles = (roots.length ? roots : profiles).map(id => buildProfile(id))
        } catch (error) {
            console.error(error)
            this.profiles = []
        } finally {
            this.loading = false
        }
    }

    renderProfile(profile: Profile): TemplateResult {
        return html`
            <li data-value="${profile.id}" title="${profile.id}">
                ${i18n[profile.id] || profile.id}
                <span part="profile-count" data-count="${profile.docCount}"></span>
                ${profile.children.length ? html`<ul>${profile.children.map(child => this.renderProfile(child))}</ul>` : ''}
            </li>
        `
    }

    selectProfile(profile: string) {
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
            ` : html `
                <rokit-button class="select-profile-button" @click="${() => this.open = true}">${i18n['selectprofile']}...</rokit-button>
                <div class="selectprofile-hint">${i18n['selectprofile-hint']}</div>
            `}
            <rokit-dialog .open="${this.open}" .loading="${this.loading}" closable @close="${() => this.open = false }">
                <div slot="header">${i18n['selectprofile']}</div>
                <div class="main">
                    <rokit-select fixedOpen collapse sort @change="${(event: Event) => {
                        event.stopPropagation()
                        const select = event.target as HTMLSelectElement
                        const profile = select.value
                        select.value = ''
                        this.selectProfile(profile)
                    }}">
                        <ul>${this.profiles?.map(profile => this.renderProfile(profile))}</ul>
                    </rokit-select>
                </div>
            </rokit-dialog>
        `
    }
}
