import { Facet } from './facets'
import { BACKEND_URL } from './constants'

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
    facets?: Facet[]
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
}

export type SearchResponse = {
    responseHeader: { 
        status: number
        QTime: number
    }
    response: {
        numFound: number
        start: number
        docs: Document[]
    }
    error?: {
        msg?: string
        trace?: string
        code: number
    }
    facets?: Record<string, AggregationFacet | number>
}

export type Document = {
    id: string
    _shape: string[]
    _rdf: string
    _creator: string
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
}

export type AggregationFacet = {
    buckets?: { val: number, count: number}[]
    gridLevel?: number
    columns?: number
    rows?: number
    minX?: number
    maxX?: number
    minY?: number
    maxY?: number
    counts_ints2D?: number[][]
}

export type Schema = { 
    uniqueKey: string
    fields: Field[]
}

export async function fetchSchema(index: string): Promise<Schema> {
    const resp = await fetch(`${BACKEND_URL}/solr/${index}/schema`)
    const schemaResult = await resp.json()
    return schemaResult.schema
}

export async function search(index: string, params?: SearchOptions): Promise<SearchResponse> {
    const query: SearchRequest = {
        limit: params?.limit !== undefined ? params.limit : 10,
        offset: params?.offset || 0,
        query: params?.term ? `*${params.term}*` : '*',
        sort: params?.sort ?  `${params.sort}` : '',
    }
    if (params?.facets) {
        query.facet = {}
        query.filter = []
        for (const facet of params.facets) {
            facet.applyAggregationQuery(query.facet)
            if (facet.active) {
                facet.applyFilterQuery(query.filter)
            }
        }
    }
    if (params?.creator) {
        query.filter = query.filter || []
        query.filter.push(`_creator:"${params.creator}"`)
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
        for (const facet of params.facets) {
            facet.updateValues(response.facets)
        }
    }
    return response
}