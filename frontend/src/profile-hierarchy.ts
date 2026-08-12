import { LitElement, PropertyValues, TemplateResult, css, html } from 'lit'
import { customElement, property, query, state } from 'lit/decorators.js'
import { RokitSelect } from '@ro-kit/ui-widgets'
import { BACKEND_URL } from './constants'
import { fetchLabels, i18n } from './i18n'
import { AggregationFacet, executeSolrRequest } from './solr'
import { globalStyles } from './styles'

interface Profile {
    id: string
    docCount?: number
    children: Profile[]
}

interface ProfileSummary {
    id: string
    parents: string[]
}

@customElement('profile-hierarchy')
export class ProfileHierarchy extends LitElement {
    static styles = [globalStyles, css`
        :host { display: block; flex: 1; min-height: 0; }
        rokit-select { width: 100%; height: 100%; border-bottom-color: #F5F5F5; background-color: white; }
        rokit-select::part(collapsible-header) { background-color: #F5F5F5 !important; }
        rokit-select::part(profile-count)::after { content: attr(data-count); color: var(--secondary-color); display: inline-block; font-family: monospace; margin-left: 7px; font-size: 12px; }
    `]

    @property()
    index = ''
    @property({ attribute: false })
    profileIds?: string[]
    @property({ type: Boolean })
    active = false
    @property()
    selectedProfile = ''
    @state()
    loading = false
    @state()
    profiles?: Profile[]
    @query('rokit-select')
    profileSelect?: RokitSelect

    willUpdate(changedProperties: PropertyValues) {
        if (changedProperties.has('index') || changedProperties.has('profileIds')) {
            this.profiles = undefined
        }
        if (this.active && (this.index || this.profileIds) && this.profiles === undefined && !this.loading) {
            void this.loadProfiles()
        }
    }

    async updated(changedProperties: PropertyValues) {
        if (!changedProperties.has('active') || !this.active) {
            return
        }
        await this.profileSelect?.updateComplete
        await this.profileSelect?.input.updateComplete
        if (this.profileSelect) {
            this.profileSelect.collapsible.content.scrollTop = 0
            this.profileSelect.input.inputElement.focus()
        }
    }

    private async loadProfiles() {
        if (this.loading) {
            return
        }
        this.loading = true
        try {
            if (this.profileIds) {
                const sourceProfileIds = this.profileIds
                const profileIds = sourceProfileIds.filter(id => !id.startsWith('urn:'))
                const response = await fetch(`${BACKEND_URL}/profiles`)
                if (!response.ok) {
                    throw new Error(`Failed loading profiles: ${response.status}`)
                }
                const allowedIds = new Set(profileIds)
                const summaries = (await response.json() as ProfileSummary[])
                    .filter(profile => allowedIds.has(profile.id))
                await fetchLabels(profileIds, true)
                if (this.profileIds === sourceProfileIds) {
                    this.profiles = this.buildProfileTree(profileIds, summaries)
                }
                return
            }

            const [response, solrResponse] = await Promise.all([
                fetch(`${BACKEND_URL}/profiles`),
                executeSolrRequest(this.index, {
                    query: '*',
                    filter: ['docType:entity'],
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
            const profileIds = summaries.map(profile => profile.id)
            await fetchLabels(profileIds, true)
            this.profiles = this.buildProfileTree(profileIds, summaries, profileCounts)
        } catch (error) {
            console.error(error)
            this.profiles = []
        } finally {
            this.loading = false
        }
    }

    private buildProfileTree(profileIds: string[], summaries: ProfileSummary[], profileCounts?: Map<string, number>): Profile[] {
        const allowedIds = new Set(profileIds)
        const parentIds = new Map(summaries.map(profile => [
            profile.id,
            new Set(profile.parents.filter(parent => allowedIds.has(parent)))
        ]))
        const childIds = new Map<string, string[]>()
        for (const [id, parents] of parentIds) {
            for (const parent of parents) {
                childIds.set(parent, [...(childIds.get(parent) || []), id])
            }
        }
        const roots = profileIds.filter(id => !parentIds.get(id)?.size)
        const buildProfile = (id: string, ancestors = new Set<string>()): Profile => {
            const nextAncestors = new Set(ancestors).add(id)
            return {
                id,
                docCount: profileCounts?.get(id),
                children: (childIds.get(id) || [])
                    .filter(child => !nextAncestors.has(child))
                    .map(child => buildProfile(child, nextAncestors))
            }
        }
        return (roots.length ? roots : profileIds).map(id => buildProfile(id))
    }

    private renderProfile(profile: Profile): TemplateResult {
        return html`
            <li data-value="${profile.id}" title="${profile.id}">
                ${i18n[profile.id] || profile.id}
                ${profile.docCount === undefined ? '' : html`<span part="profile-count" data-count="${profile.docCount}"></span>`}
                ${profile.children.length ? html`<ul>${profile.children.map(child => this.renderProfile(child))}</ul>` : ''}
            </li>
        `
    }

    render() {
        return html`
            <rokit-select fixedOpen collapse sort class="${this.loading ? 'loading' : ''}" @change="${(event: Event) => {
                event.stopPropagation()
                const select = event.target as HTMLSelectElement
                this.selectedProfile = select.value
                select.value = ''
                this.dispatchEvent(new Event('change', { bubbles: true, composed: true }))
            }}">
                <ul>${this.profiles?.map(profile => this.renderProfile(profile))}</ul>
            </rokit-select>
        `
    }
}
