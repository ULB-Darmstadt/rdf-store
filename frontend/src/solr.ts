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
    offset?: number
    limit?: number
    filters?: string[]
}

export type SearchRequest = {
    query?: unknown
    sort?: string
    limit: number
    offset: number
    facet?: Record<string, unknown>
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
    facets?: Record<string, unknown>
}

export interface SearchDocument {
    id: string
    resourceId: string
    subject: string
    label: string[]
    shape: string[]
    creator: string
    lastModified: string
}

export type AggregationFacet = {
    buckets?: { val: number | string, count: number }[]
    gridLevel?: number
    columns?: number
    rows?: number
    minX?: number
    maxX?: number
    minY?: number
    maxY?: number
    counts_ints2D?: number[][]
}

export async function executeSolrRequest(index: string, query: SearchRequest, signal?: AbortSignal): Promise<SearchResponse> {
    const resp = await fetch(`${BACKEND_URL}/solr/${index}/query`, {
        method: 'POST',
        cache: 'no-cache',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(query),
        signal
    })
    return await resp.json() as SearchResponse
}

export async function search(index: string, params?: SearchOptions): Promise<SearchResponse> {
    const query: SearchRequest = {
        limit: params?.limit !== undefined ? params.limit : 10,
        offset: params?.offset || 0,
        sort: params?.sort ? `${params.sort}` : '',
        fields: ['id', 'label', 'shape', 'lastModified', 'resourceId', 'subject'],
        query: '*'
    }
    // Value documents are nested below searchable entity documents. Always
    // constrain ordinary searches to parents so children can never be hits.
    query.filter = ['docType:entity']
    if (params?.creator) {
        query.filter = query.filter || []
        query.filter.push(`creator:"${params.creator}"`)
    }
    if (params?.term) {
        query.filter = query.filter || []
        // escape characters that would break the SOLR query
        query.filter.push(`_text_:*${params.term.replace(/([+\-!(){}[\]^"~*?:\\/]|&&|\|\|)/g, '\\$1')}*`)
    }
    if (params?.filters?.length) {
        query.filter = [...(query.filter || []), ...params.filters]
    }

    const response = await executeSolrRequest(index, query)
    return response
}
