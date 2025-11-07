import { search, Schema, Field } from '../solr'
import { Facet } from './base'
import { KeywordFacet } from './keyword'
import { GeoLocationFacet } from './geo-location'
import { NumberRangeFacet } from './number-range'
import { DateRangeFacet } from './date-range'
import { Config } from '../index'
import { fetchLabels, i18n } from '../i18n'

export { Facet }

export function facetFactory(field: Field, schema: Schema, config: Config): Facet | null {
    // exlcude id and internal fields from faceting
    if (field.name === schema.uniqueKey || (field.name.startsWith('_') && field.name !== '_shape')) {
        return null
    }
    switch(field.type) {
        case 'string': return new KeywordFacet(field.name, config)
        case 'location_rpt': return new GeoLocationFacet(field.name)
        case 'plong':
        case 'pint':
            return new NumberRangeFacet(field.name, '1')
        case 'pfloat':
        case 'pdouble':
            return new NumberRangeFacet(field.name)
        case 'pdate':
            return new DateRangeFacet(field.name)
    }
    return null
}

export async function initFacets(config: Config, schema: Schema): Promise<Facet[]> {
    // derive facets from schema
    const facets: Facet[] = []
    const labelIds: Set<string> = new Set()
    for (const field of schema.fields) {
        if (field.type && (field.indexed === undefined || field.indexed === true)) {
            const facet = facetFactory(field, schema, config)
            if (facet) {
                facets.push(facet)
                // the colon matches the way that the backend stores the labels
                labelIds.add(field.name)
                if (facet.profile) {
                    labelIds.add(facet.profile)
                }
            }
        }
    }
    // fetch field labels
    await fetchLabels(Array.from(labelIds), true, true)
    // set label and title on facets
    for (const facet of facets) {
        facet.label = i18n[facet.indexField] || facet.indexField
        facet.title = i18n[facet.profile] ? (i18n['_shape'] + ': ' + i18n[facet.profile]) : ''
    }
    // let search function set available values and doc counts on the facets
    await search(config.index, { limit: 0, facets: facets })
    return facets
}
