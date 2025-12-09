import { LitElement, css } from 'lit'
import { AggregationFacet, QueryFacet } from '../solr'
import { property, state } from 'lit/decorators.js'

import { globalStyles } from '../styles'

export class Facets {
    facets: Record<string, Facet[]> = {}

    add(profile: string, facet: Facet) {
        if (this.facets[profile] === undefined) {
            this.facets[profile] = []
        }
        this.facets[profile].push(facet)
    }

    hasValidFacet(profile: string) {
        for (const facet of this.facets[profile]) {
            if (facet.valid) {
                return true
            }
        }
        return false
    }
}

export abstract class Facet extends LitElement {
    static styles = [css`
        :host([valid=false]) { display: none; }
        .icon { color: #888; margin-right: 4px; }
        .w-100 { width: 100%; }
    `, globalStyles]

    @property({ reflect: true })
    valid: boolean = false
    @state()
    active: boolean = false

    indexField: string
    indexFieldWithoutDatatype = ''
    profile = ''
    label = ''

    constructor(indexField: string) {
        super()
        this.indexField = indexField
        const tokens = indexField.split('.')
        if (tokens.length == 2) {
            this.profile = tokens[0]
            this.indexFieldWithoutDatatype = tokens[1].lastIndexOf('_') > -1 ? tokens[1].substring(0, tokens[1].lastIndexOf('_')) : tokens[1]
        } else {
            this.indexFieldWithoutDatatype = indexField
        }
    }

    abstract updateValues(aggs: Record<string, AggregationFacet | number | string>): void
    abstract applyAggregationQuery(facets: Record<string, QueryFacet | string>): void
    abstract applyFilterQuery(filter: string[]): void
}
