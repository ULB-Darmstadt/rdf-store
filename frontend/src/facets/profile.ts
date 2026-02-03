import { css, html } from 'lit'
import { customElement, property, query } from 'lit/decorators.js'
import { RokitSelect } from '@ro-kit/ui-widgets'

import { Facet } from './base'
import { AggregationFacet, QueryFacet } from '../solr'
import { fetchLabels, i18n } from '../i18n'

@customElement('profile-facet')
export class ProfileFacet extends Facet {
    static styles = [...Facet.styles, css`
        rokit-select { width: 100%; }
        rokit-select::part(facet-count)::after { content: attr(data-count); color: var(--secondary-color); display: inline-block; font-family: monospace; margin-left: 7px; font-size: 12px; }
    `]

    @property()
    values: { value: string | number, docCount: number }[] = []
    @property()
    selectedValue = ''
    @query('#select')
    select!: RokitSelect
    solrMaxAggregations: number

    constructor(solrMaxAggregations: number) {
        super('shape')
        this.solrMaxAggregations = solrMaxAggregations
    }

    onChange() {
        this.selectedValue = this.select.value
        this.active = this.selectedValue.length > 0
        this.dispatchEvent(new Event('change', { bubbles: true }))
    }

    updateValues(aggs: Record<string, AggregationFacet>) {
        const values: Record<string, any> =  {}
        const missingLabels: string[] = []

        let facet = aggs[this.indexField]
        if (facet?.buckets?.length) {
            for (const bucket of facet.buckets) {
                if (bucket.count > 0 && typeof(bucket.val) === 'string') {
                    values[bucket.val] = { value: bucket.val, docCount: bucket.count, ref: false }
                }
            }
        }

        // check if labels of facet values are missing
        for (const v of Object.keys(values)) {
            if (i18n[v] === undefined) {
                missingLabels.push(v)
            }
        }
        (async () => {
            await fetchLabels(missingLabels, true)
            this.values = Object.values(values)
            this.valid = this.values.length > 0
        })()
    }

    applyAggregationQuery(facets: Record<string, QueryFacet>) {
        facets['shape'] = { field: 'shape', type: 'terms', limit: -1 }
    }

    applyFilterQuery(filter: string[]) {
        const val = this.selectedValue.replace(/:/, '\\:')
        filter.push(`+shape:${val}`)
    }

    render() {
        return html`
            <rokit-select id="select" dense value="${this.selectedValue}" title="${this.title}" label="${this.label}" @change="${() => this.onChange()}" clearable>
                <span class="material-icons icon" slot="prefix">list</span>
                <ul>
                ${this.values.map((v) => html`
                    <li data-value="${v.value}">${i18n[v.value] || v.value}<span class="facet-count" part="facet-count" data-count="${v.docCount}"></span></li>
                `)}
                </ul>
            </rokit-select>
        ` 
    }
}
