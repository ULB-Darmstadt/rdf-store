import { search, fetchSchema } from '../solr'
import { Facet, Facets } from './base'
import { KeywordFacet } from './keyword'
import { GeoLocationFacet } from './geo-location'
import { NumberRangeFacet } from './number-range'
import { DateRangeFacet } from './date-range'
import { fetchLabels, i18n } from '../i18n'
import { ProfileFacet } from './profile'

export { Facet }

export function facetFactory(field: string, solrMaxAggregations: number): Facet | null {
    if (field.endsWith('_ss')) {
        return new KeywordFacet(field, solrMaxAggregations)
    }
    if (field.endsWith('_is')) {
        return new NumberRangeFacet(field, '1')
    }
    if (field.endsWith('_fs') || field.endsWith('_ds')) {
        return new NumberRangeFacet(field)
    }
    if (field.endsWith('_dts')) {
        return new DateRangeFacet(field)
    }
    if (field.endsWith('_srpt')) {
        return new GeoLocationFacet(field)
    }
    return null
}

export async function initFacets(index: string, solrMaxAggregations: number): Promise<Facets> {
    // derive facets from schema
    const fieldList = await fetchSchema(index)
    
    const facets = new Facets()
    facets.add('', new ProfileFacet(solrMaxAggregations))
    const labelIds: Set<string> = new Set()
    for (const field of fieldList) {
        const facet = facetFactory(field, solrMaxAggregations)
        if (facet) {
            facets.add(facet.profile, facet)
            labelIds.add(facet.indexFieldWithoutDatatype)
            labelIds.add(facet.profile)
        }
    }
    // fetch field labels
    await fetchLabels(Array.from(labelIds), true, true)
    // set label and title on facets
    for (const profile of Object.keys(facets.facets)) {
        for (const facet of facets.facets[profile]) {
            facet.label = i18n[facet.indexFieldWithoutDatatype] || facet.indexFieldWithoutDatatype
        }
    }
    // let search function set available values and doc counts on the facets
    await search(index, { limit: 0, facets: facets })
    return facets
}
