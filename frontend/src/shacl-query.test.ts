import { describe, expect, it, vi, beforeEach } from 'vitest'
import type { QueryCriterion, QueryFacetRequest, QueryField } from '@ulb-darmstadt/shacl-form'
import type { Config } from '.'

vi.mock('@ulb-darmstadt/shacl-form', () => ({
    isRangeQueryField: vi.fn(() => false)
}))

vi.mock('./solr', () => ({
    executeSolrRequest: vi.fn()
}))

vi.mock('./i18n', () => ({
    fetchLabels: vi.fn(async() => undefined),
    i18n: {}
}))

vi.mock('.', () => ({}))

vi.mock('./constants', () => ({
    BACKEND_URL: 'http://backend.test'
}))

import { isRangeQueryField } from '@ulb-darmstadt/shacl-form'
import { DataFactory } from 'n3'
import { executeSolrRequest } from './solr'
import type { SearchResponse } from './solr'
import { SolrQueryFacetProvider, queryFieldPaths } from './shacl-query'

const UNIT_PREDICATE = 'http://example.org/hasUnit'
const KIND_PREDICATE = 'http://example.org/hasKindOfQuantity'
const VALUE_PREDICATE = 'http://example.org/hasNumericalValue'

function field(id: string, path: QueryField['path'], shapePath?: QueryField['shapePath']): QueryField {
    return { id, path, shapePath } as QueryField
}

function quantityConfig(): Config {
    return {
        conversionUnit: UNIT_PREDICATE,
        conversionQuantity: KIND_PREDICATE,
        conversionValue: VALUE_PREDICATE,
        index: 'test',
        geoDataType: '',
        solrMaxAggregations: 100
    } as unknown as Config
}

function rangeCriterion(min: string, max: string): QueryCriterion {
    return {
        field: field('value', ['part', VALUE_PREDICATE]),
        operator: 'range',
        min: DataFactory.literal(min),
        max: DataFactory.literal(max)
    }
}

function unitCriterion(unitURI: string): QueryCriterion {
    return {
        field: field('unit', ['part', UNIT_PREDICATE]),
        operator: 'equals',
        value: DataFactory.namedNode(unitURI)
    }
}

function kindCriterion(kindURI: string): QueryCriterion {
    return {
        field: field('kind', ['part', KIND_PREDICATE]),
        operator: 'equals',
        value: DataFactory.namedNode(kindURI)
    }
}

function mockQuantityFetch(conversions: Record<string, { multiplier: number, offset: number } | null>) {
    vi.stubGlobal('fetch', vi.fn(async(_url: string, init?: RequestInit) => ({
        json: async() => JSON.parse(String(init?.body)).map((quantity: { unitURI: string }) => ({
            ...quantity,
            conversion: conversions[quantity.unitURI] ?? null
        }))
    })))
}

async function buildFilters(provider: SolrQueryFacetProvider, criteria: QueryCriterion[]): Promise<string[]> {
    return provider.buildFilters({ rootShapeId: 'urn:shape:root', criteria })
}

describe('queryFieldPaths', () => {
    beforeEach(() => {
        vi.mocked(isRangeQueryField).mockReturnValue(false)
    })

    it('keeps qualified branches isolated', () => {
        expect(queryFieldPaths(field(
            'f',
            ['http://example.org/hasPart', 'http://example.org/hasKindOfQuantity'],
            ['urn:property:fill-volume', 'http://example.org/hasKindOfQuantity']
        ))).toEqual([
            ['urn:property:fill-volume', 'http://example.org/hasKindOfQuantity']
        ])
    })

    it('falls back to RDF paths when no complete shape path exists', () => {
        expect(queryFieldPaths(field('f', [
            ['http://example.org/hasPart', 'http://example.org/contains'],
            'http://example.org/value'
        ]))).toEqual([
            ['http://example.org/hasPart', 'http://example.org/value'],
            ['http://example.org/contains', 'http://example.org/value']
        ])
    })
})

describe('quantity conversion', () => {
    // unique URIs per test: quantityCache is module state shared across tests
    const celsius = 'http://qudt.org/vocab/unit/DEG_C'
    const temperature = 'http://qudt.org/vocab/quantitykind/Temperature'
    const unit = (suffix: string) => `${celsius}-${suffix}`
    const kind = (suffix: string) => `${temperature}-${suffix}`

    beforeEach(() => {
        vi.unstubAllGlobals()
        vi.mocked(isRangeQueryField).mockImplementation((field: QueryField) => field.id === 'value')
    })

    it('converts range bounds to SI when unit and kind are selected', async() => {
        mockQuantityFetch({ [unit('t1')]: { multiplier: 1, offset: 273.15 } })
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('t1')),
            kindCriterion(kind('t1'))
        ])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["273.15" TO "373.15"]'))
    })

    it('does not restrict results by the unit selection', async() => {
        mockQuantityFetch({ [unit('t2')]: { multiplier: 1, offset: 273.15 } })
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('t2')),
            kindCriterion(kind('t2'))
        ])
        expect(filters).toHaveLength(3)
        expect(filters[0]).toBe('shape:"urn:shape:root"')
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["273.15" TO "373.15"]'))
        expect(filters.filter(filter => filter.includes('docType:value'))).toHaveLength(2)
    })

    it('accepts unit selections stored as bracketed IRI literals', async() => {
        mockQuantityFetch({ [unit('t8')]: { multiplier: 1, offset: 273.15 } })
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            {
                field: field('unit', ['part', UNIT_PREDICATE]),
                operator: 'equals',
                value: DataFactory.literal(`<${unit('t8')}>`)
            },
            kindCriterion(kind('t8'))
        ])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["273.15" TO "373.15"]'))
        expect(filters).not.toContainEqual(expect.stringContaining(unit('t8')))
    })

    it('leaves range bounds untouched without unit or kind selection', async() => {
        mockQuantityFetch({})
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const filters = await buildFilters(provider, [rangeCriterion('0', '100')])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["0" TO "100"]'))
    })

    it('leaves range bounds untouched for unknown units', async() => {
        mockQuantityFetch({ [unit('t3')]: null })
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('t3')),
            kindCriterion(kind('t3'))
        ])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["0" TO "100"]'))
    })

    it('ignores incomplete quantity groups', async() => {
        mockQuantityFetch({ [unit('t4')]: { multiplier: 1, offset: 273.15 } })
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('t4'))
        ])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["0" TO "100"]'))
        expect(filters).not.toContainEqual(expect.stringContaining(unit('t4')))
    })

    it('does not convert when conversion is disabled by config', async() => {
        mockQuantityFetch({ [unit('t5')]: { multiplier: 1, offset: 273.15 } })
        const config = { ...quantityConfig(), conversionUnit: '' } as unknown as Config
        const provider = new SolrQueryFacetProvider(config)
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('t5')),
            kindCriterion(kind('t5'))
        ])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["0" TO "100"]'))
    })

    it('converts facet min and max back to the selected unit', async() => {
        mockQuantityFetch({ [unit('t6')]: { multiplier: 1, offset: 273.15 } })
        vi.mocked(executeSolrRequest).mockResolvedValue({
            facets: {
                f0_count: { entities: 7 },
                f0_stats: { min: 300, max: 373.15 }
            }
        } as unknown as SearchResponse)
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const request: QueryFacetRequest = {
            query: { rootShapeId: 'urn:shape:root', criteria: [unitCriterion(unit('t6')), kindCriterion(kind('t6'))] },
            fields: [field('value', ['part', VALUE_PREDICATE])],
            signal: new AbortController().signal
        }
        const facets = await provider.getFacets(request)
        expect(facets[0].count).toBe(7)
        expect(facets[0].min?.value).toBe('26.85')
        expect(facets[0].max?.value).toBe('100')
    })

    it('returns raw facet stats without a selected unit', async() => {
        mockQuantityFetch({})
        vi.mocked(executeSolrRequest).mockResolvedValue({
            facets: {
                f0_count: { entities: 7 },
                f0_stats: { min: 300, max: 373.15 }
            }
        } as unknown as SearchResponse)
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const request: QueryFacetRequest = {
            query: { rootShapeId: 'urn:shape:root', criteria: [] },
            fields: [field('value', ['part', VALUE_PREDICATE])],
            signal: new AbortController().signal
        }
        const facets = await provider.getFacets(request)
        expect(facets[0].min?.value).toBe('300')
        expect(facets[0].max?.value).toBe('373.15')
    })

    it('uses a single quantity kind bucket automatically', async() => {
        mockQuantityFetch({ [unit('t9')]: { multiplier: 1, offset: 273.15 } })
        vi.mocked(executeSolrRequest).mockResolvedValue({
            facets: {
                f0_count: { entities: 3 },
                f0_stats: { min: 373.15, max: 373.15 },
                f1_count: { entities: 3 },
                f1_buckets: { buckets: [{ val: `<${kind('t9')}>`, count: 3 }] }
            }
        } as unknown as SearchResponse)
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const request: QueryFacetRequest = {
            query: { rootShapeId: 'urn:shape:root', criteria: [unitCriterion(unit('t9'))] },
            fields: [
                field('value', ['part', VALUE_PREDICATE]),
                field('kind', ['part', KIND_PREDICATE])
            ],
            signal: new AbortController().signal
        }
        const facets = await provider.getFacets(request)
        expect(facets[0].min?.value).toBe('100')
        expect(facets[0].max?.value).toBe('100')
        // the learned kind also applies to subsequent searches
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('t9'))
        ])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["273.15" TO "373.15"]'))
    })

    it('ignores ambiguous quantity kind facets', async() => {
        mockQuantityFetch({ [unit('t10')]: { multiplier: 1, offset: 273.15 } })
        vi.mocked(executeSolrRequest).mockResolvedValue({
            facets: {
                f0_count: { entities: 3 },
                f0_stats: { min: 373.15, max: 373.15 },
                f1_count: { entities: 3 },
                f1_buckets: { buckets: [
                    { val: `<${kind('t10')}-a>`, count: 2 },
                    { val: `<${kind('t10')}-b>`, count: 1 }
                ] }
            }
        } as unknown as SearchResponse)
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const request: QueryFacetRequest = {
            query: { rootShapeId: 'urn:shape:root', criteria: [unitCriterion(unit('t10'))] },
            fields: [
                field('value', ['part', VALUE_PREDICATE]),
                field('kind', ['part', KIND_PREDICATE])
            ],
            signal: new AbortController().signal
        }
        const facets = await provider.getFacets(request)
        expect(facets[0].min?.value).toBe('373.15')
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('t10'))
        ])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["0" TO "100"]'))
    })
})

describe('loading notification', () => {
    beforeEach(() => {
        vi.unstubAllGlobals()
        vi.mocked(isRangeQueryField).mockImplementation((field: QueryField) => field.id === 'value')
    })

    function facetRequest(): QueryFacetRequest {
        return {
            query: { rootShapeId: 'urn:shape:root', criteria: [] },
            fields: [field('value', ['part', VALUE_PREDICATE])],
            signal: new AbortController().signal
        }
    }

    it('notifies loading state around facet requests', async() => {
        mockQuantityFetch({})
        vi.mocked(executeSolrRequest).mockResolvedValue({
            facets: { f0_count: { entities: 1 } }
        } as unknown as SearchResponse)
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const events: boolean[] = []
        provider.onLoadingChange = loading => events.push(loading)
        await provider.getFacets(facetRequest())
        expect(events).toEqual([true, false])
    })

    it('resets loading state on errors', async() => {
        mockQuantityFetch({})
        vi.mocked(executeSolrRequest).mockRejectedValue(new Error('solr down'))
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const events: boolean[] = []
        provider.onLoadingChange = loading => events.push(loading)
        await expect(provider.getFacets(facetRequest())).rejects.toThrow('solr down')
        expect(events).toEqual([true, false])
    })
})
