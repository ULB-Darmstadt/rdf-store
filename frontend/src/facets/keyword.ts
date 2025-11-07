import { css, html } from 'lit'
import { customElement, property, query } from 'lit/decorators.js'
import { RokitSelect } from '@ro-kit/ui-widgets'

import { Facet } from './base'
import { AggregationFacet, QueryFacet } from '../solr'
import { Config } from '../index'
import { fetchLabels, i18n } from '../i18n'

@customElement('keyword-facet')
export class KeywordFacet extends Facet {
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
    config: Config

    constructor(indexField: string, config: Config) {
        super(indexField)
        this.config = config
    }

    onChange() {
        this.selectedValue = this.select.value
        this.active = (this.selectedValue !== undefined && this.selectedValue.length > 0) || false
        this.dispatchEvent(new Event('change', { bubbles: true }))
    }

    updateValues(aggs: Record<string, AggregationFacet>) {
        const values = []
        const facet = aggs[this.indexField]
        if (facet?.buckets?.length) {
            for (const bucket of facet.buckets) {
                if (bucket.count > 0) {
                    values.push({ value: bucket.val, docCount: bucket.count })
                }
            }
        }
        // check if labels of facet values are missing
        const missingLabels: string[] = []
        for (const v of values) {
            // only request term labels and not literals
            if (typeof(v.value) === 'string' && (v.value as string).startsWith('<') && (v.value as string).endsWith('>') && i18n[v.value] === undefined) {
                missingLabels.push(v.value)
            }
        }
        (async () => {
            await fetchLabels(missingLabels)
            this.values = values
            this.valid = values.length > 0
        })()
    }

    applyAggregationQuery(facets: Record<string, QueryFacet>) {
        facets[this.indexField] = { field: this.indexField, type: 'terms', limit: this.config.solrMaxAggregations }
    }

    applyFilterQuery(filter: string[]) {
        filter.push(`${this.indexField}:${this.selectedValue?.replace(/:/, '\\:')}`)
    }

    render() {
        return html`
            <rokit-select id="select" value="${this.selectedValue}" title="${this.title}" label="${this.label}" @change="${() => this.onChange()}" clearable>
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
