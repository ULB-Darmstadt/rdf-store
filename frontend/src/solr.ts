import { BACKEND_URL } from './constants'
import { Facets } from './facets/base'

export type Field = {
    name: string
    type: string
    stored: boolean
    indexed: boolean
    docValues: boolean
}

export type SearchOptions = {
    term?: string
    creator?: string
    sort?: string
    facets?: Facets
    offset?: number
    limit?: number
}

export type SearchRequest = {
    query?: any
    sort?: string
    limit: number
    offset: number
    facet?: Record<string, QueryFacet | string>
    filter?: string[]
    fields?: string[]
}

export type SearchResponse = {
    responseHeader: { 
        status: number
        QTime: number
    }
    response: {
        numFound: number
        start: number
        docs: SearchDocument[]
    }
    error?: {
        msg?: string
        trace?: string
        code: number
    }
    facets?: Record<string, AggregationFacet | number>
}

export interface SearchDocument {
    id: string
    label: string[]
    shape: string[]
    rdf: string
    creator: string
    lastModified: string
    _nest_parent_: string
    _root_: string
}

export type QueryFacet = {
    field: string
    type: 'terms' | 'query' | 'heatmap'
    start?: number
    end?: number
    gap?: number
    q?: string
    limit?: number
    geom?: string
    gridLevel?: number
    domain?: {}
}

export type AggregationFacet = {
    buckets?: { val: number | string, count: number}[]
    gridLevel?: number
    columns?: number
    rows?: number
    minX?: number
    maxX?: number
    minY?: number
    maxY?: number
    counts_ints2D?: number[][]
}

export async function fetchFields(index: string): Promise<string[]> {
    const fieldList = await fetch(`${BACKEND_URL}/solr/${index}/select?fl=*&q=*&rows=0&wt=csv`).then(r => r.text())
    return fieldList.split(',').map(field => field.trim())
}

export async function search(index: string, params?: SearchOptions): Promise<SearchResponse> {
    const query: SearchRequest = {
        limit: params?.limit !== undefined ? params.limit : 10,
        offset: params?.offset || 0,
        sort: params?.sort ?  `${params.sort}` : '',
        fields: ['*'],
        query: '*'
    }
    if (params?.facets) {
        query.facet = {}
        query.filter = []
        for (const profile of Object.keys(params.facets.facets)) {
            for (const facet of params.facets.facets[profile]) {
                facet.applyAggregationQuery(query.facet)
                if (facet.active) {
                    facet.applyFilterQuery(query.filter)
                }
            }
        }
    }
    if (params?.creator) {
        query.filter = query.filter || []
        query.filter.push(`creator:"${params.creator}"`)
    }
    if (params?.term) {
        query.filter = query.filter || []
        // escape characters that would break the SOLR query
        query.filter.push(`_text_:*${params.term.replace(/([+\-!(){}[\]^"~*?:\\/]|&&|\|\|)/g, '\\$1')}`)
    }

    const resp = await fetch(`${BACKEND_URL}/solr/${index}/query`, {
        method: "POST",
        cache: "no-cache",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(query)
    })
    const response = await resp.json() as SearchResponse
    // update facet values
    if (params?.facets && response.facets) {
        for (const profile of Object.keys(params.facets.facets)) {
            for (const facet of params.facets.facets[profile]) {
                facet.updateValues(response.facets)
            }
        }
    }
    return response
}