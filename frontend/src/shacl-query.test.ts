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
const DELTA_PREDICATE = 'http://qudt.org/schema/qudt#isDeltaQuantity'

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

function deltaCriterion(value: string): QueryCriterion {
    return {
        field: field('delta', ['part', DELTA_PREDICATE]),
        operator: 'equals',
        value: DataFactory.literal(value)
    }
}

function mockQuantityFetch(
    conversions: Record<string, { multiplier: number, offset: number } | null>,
    canonicalUnits: Record<string, string> = {}
) {
    vi.stubGlobal('fetch', vi.fn(async(_url: string, init?: RequestInit) => ({
        ok: true,
        json: async() => JSON.parse(String(init?.body)).map((quantity: { unitURI: string, quantityKindURI: string }) => ({
            ...quantity,
            canonicalUnitURI: canonicalUnits[quantity.quantityKindURI],
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

    it('invalidates the related value range when the selected unit changes', () => {
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const value = field('value', ['part', VALUE_PREDICATE])
        const unitField = field('unit', ['part', UNIT_PREDICATE])
        const previousUnit = unitCriterion(unit('old'))
        const selectedUnit = unitCriterion(unit('new'))

        expect(provider.invalidatedFields({
            previousQuery: { rootShapeId: 'urn:shape:root', criteria: [previousUnit] },
            query: { rootShapeId: 'urn:shape:root', criteria: [selectedUnit] },
            fields: [value, unitField]
        })).toEqual(['value'])
        expect(provider.invalidatedFields({
            previousQuery: { rootShapeId: 'urn:shape:root', criteria: [] },
            query: { rootShapeId: 'urn:shape:root', criteria: [selectedUnit] },
            fields: [value, unitField]
        })).toEqual(['value'])
        expect(provider.invalidatedFields({
            previousQuery: { rootShapeId: 'urn:shape:root', criteria: [selectedUnit] },
            query: { rootShapeId: 'urn:shape:root', criteria: [selectedUnit] },
            fields: [value, unitField]
        })).toEqual([])
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

    it('keeps unit criteria as filters when the quantity group is incomplete', async() => {
        mockQuantityFetch({ [unit('t4')]: { multiplier: 1, offset: 273.15 } })
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('t4'))
        ])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["0" TO "100"]'))
        expect(filters).toContainEqual(expect.stringContaining(unit('t4')))
    })

    it('keeps unit criteria as filters for unknown units', async() => {
        mockQuantityFetch({ [unit('t3')]: null })
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('t3')),
            kindCriterion(kind('t3'))
        ])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["0" TO "100"]'))
        expect(filters).toContainEqual(expect.stringContaining(unit('t3')))
    })

    it('degrades to unconverted filtering when the backend fails', async() => {
        vi.stubGlobal('fetch', vi.fn(async() => ({ ok: false, status: 500, json: async() => ({}) })))
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('t11')),
            kindCriterion(kind('t11'))
        ])
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["0" TO "100"]'))
    })

    it('converts delta quantities without offsets', async() => {
        vi.stubGlobal('fetch', vi.fn(async(_url: string, init?: RequestInit) => ({
            ok: true,
            json: async() => JSON.parse(String(init?.body)).map((quantity: { unitURI: string, isDelta?: boolean }) => ({
                ...quantity,
                conversion: quantity.isDelta
                    ? { multiplier: 1, offset: 0 }
                    : { multiplier: 1, offset: 273.15 }
            }))
        })))
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(unit('d1')),
            kindCriterion(kind('d1')),
            deltaCriterion('true')
        ])
        // a difference of 0..100 °C is a difference of 0..100 K
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["0" TO "100"]'))
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

    it('injects and pre-selects a missing canonical SI unit', async() => {
        const canonical = unit('si')
        mockQuantityFetch({}, { [kind('default')]: canonical })
        vi.mocked(executeSolrRequest).mockResolvedValue({
            facets: {
                f0_count: { entities: 3 },
                f0_buckets: { buckets: [{ val: `<${unit('source')}>`, count: 3, entities: 3 }] },
                f1_count: { entities: 3 },
                f1_buckets: { buckets: [{ val: `<${kind('default')}>`, count: 3, entities: 3 }] }
            }
        } as unknown as SearchResponse)
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const request: QueryFacetRequest = {
            query: { rootShapeId: 'urn:shape:root', criteria: [] },
            fields: [
                field('unit', ['part', UNIT_PREDICATE]),
                field('kind', ['part', KIND_PREDICATE])
            ],
            signal: new AbortController().signal
        }

        const callsBefore = vi.mocked(executeSolrRequest).mock.calls.length
        const facets = await provider.getFacets(request)

        expect(facets[0].buckets?.map(bucket => bucket.value.value)).toEqual([canonical, unit('source')])
        expect(facets[0].buckets?.[0].count).toBe(3)
        expect(vi.mocked(executeSolrRequest).mock.calls.length - callsBefore).toBe(2)
        expect(facets[0].initialValue?.value).toBe(canonical)

        const filters = await buildFilters(provider, [
            rangeCriterion('0', '100'),
            unitCriterion(canonical),
            kindCriterion(kind('default'))
        ])
        expect(fetch).toHaveBeenCalledTimes(1)
        expect(filters).toContainEqual(expect.stringContaining('valueNumber:["0" TO "100"]'))
    })

    it('uses a shape-fixed quantity kind to inject and pre-select the canonical SI unit', async() => {
        const canonical = unit('fixed-si')
        const fixedKind = kind('fixed')
        mockQuantityFetch({}, { [fixedKind]: canonical })
        vi.mocked(executeSolrRequest).mockResolvedValue({
            facets: {
                f0_count: { entities: 3 },
                f0_buckets: { buckets: [] },
                f1_count: { entities: 3 },
                f1_buckets: { buckets: [] }
            }
        } as unknown as SearchResponse)
        const kindField = field('kind', ['part', KIND_PREDICATE])
        kindField.fixedValue = DataFactory.namedNode(fixedKind)
        const provider = new SolrQueryFacetProvider(quantityConfig())

        const callsBefore = vi.mocked(executeSolrRequest).mock.calls.length
        const facets = await provider.getFacets({
            query: { rootShapeId: 'urn:shape:root', criteria: [] },
            fields: [field('unit', ['part', UNIT_PREDICATE]), kindField],
            signal: new AbortController().signal
        })

        expect(facets[0].buckets?.map(bucket => bucket.value.value)).toEqual([canonical])
        expect(facets[0].initialValue?.value).toBe(canonical)
        expect(vi.mocked(executeSolrRequest).mock.calls.length - callsBefore).toBe(1)
        expect(fetch).toHaveBeenCalledTimes(1)
    })

    it('pre-selects every unit facet that shares a fixed quantity kind', async() => {
        const canonical = unit('shared-si')
        const fixedKind = kind('shared')
        mockQuantityFetch({}, { [fixedKind]: canonical })
        vi.mocked(executeSolrRequest).mockResolvedValue({
            facets: {
                f0_count: { entities: 2 }, f0_buckets: { buckets: [] },
                f1_count: { entities: 2 }, f1_buckets: { buckets: [] },
                f2_count: { entities: 2 }, f2_buckets: { buckets: [] },
                f3_count: { entities: 2 }, f3_buckets: { buckets: [] }
            }
        } as unknown as SearchResponse)
        const fixedKindField = (id: string, subject: string) => {
            const result = field(id, [subject, KIND_PREDICATE])
            result.fixedValue = DataFactory.namedNode(fixedKind)
            return result
        }
        const provider = new SolrQueryFacetProvider(quantityConfig())

        const facets = await provider.getFacets({
            query: { rootShapeId: 'urn:shape:root', criteria: [] },
            fields: [
                field('unit-a', ['part-a', UNIT_PREDICATE]), fixedKindField('kind-a', 'part-a'),
                field('unit-b', ['part-b', UNIT_PREDICATE]), fixedKindField('kind-b', 'part-b')
            ],
            signal: new AbortController().signal
        })

        expect(facets[0].initialValue?.value).toBe(canonical)
        expect(facets[2].initialValue?.value).toBe(canonical)
        expect(fetch).toHaveBeenCalledTimes(1)
    })

    it('pre-selects a canonical unit fixed by the shape', async() => {
        const volt = unit('fixed-volt')
        const voltage = kind('voltage')
        mockQuantityFetch({ [volt]: { multiplier: 1, offset: 0 } }, { [voltage]: volt })
        vi.mocked(executeSolrRequest).mockResolvedValue({
            facets: {
                f0_count: { entities: 4 },
                f0_buckets: { buckets: [{ val: `<${volt}>`, count: 4, entities: 4 }] },
                f1_count: { entities: 4 },
                f1_buckets: { buckets: [{ val: `<${voltage}>`, count: 4, entities: 4 }] }
            }
        } as unknown as SearchResponse)
        const unitField = field('unit', ['part', UNIT_PREDICATE])
        unitField.fixedValue = DataFactory.namedNode(volt)
        const kindField = field('kind', ['part', KIND_PREDICATE])
        kindField.fixedValue = DataFactory.namedNode(voltage)
        const provider = new SolrQueryFacetProvider(quantityConfig())

        const facets = await provider.getFacets({
            query: { rootShapeId: 'urn:shape:root', criteria: [] },
            fields: [unitField, kindField],
            signal: new AbortController().signal
        })

        expect(facets[0].initialValue?.value).toBe(volt)
        expect(fetch).toHaveBeenCalledTimes(1)
    })

    it('moves an existing canonical SI unit first without duplicating it', async() => {
        const canonical = unit('si-existing')
        mockQuantityFetch({}, { [kind('existing')]: canonical })
        vi.mocked(executeSolrRequest).mockResolvedValue({
            facets: {
                f0_count: { entities: 5 },
                f0_buckets: { buckets: [
                    { val: `<${unit('source')}>`, count: 3, entities: 3 },
                    { val: `<${canonical}>`, count: 2, entities: 2 }
                ] },
                f1_count: { entities: 5 },
                f1_buckets: { buckets: [{ val: `<${kind('existing')}>`, count: 5, entities: 5 }] }
            }
        } as unknown as SearchResponse)
        const provider = new SolrQueryFacetProvider(quantityConfig())

        const facets = await provider.getFacets({
            query: { rootShapeId: 'urn:shape:root', criteria: [kindCriterion(kind('existing'))] },
            fields: [
                field('unit', ['part', UNIT_PREDICATE]),
                field('kind', ['part', KIND_PREDICATE])
            ],
            signal: new AbortController().signal
        })

        expect(facets[0].buckets?.map(bucket => bucket.value.value)).toEqual([canonical, unit('source')])
        expect(facets[0].buckets?.[0].count).toBe(2)
    })

    it('hides quantity metadata without a usable range in the same group', async() => {
        mockQuantityFetch({})
        vi.mocked(executeSolrRequest)
            .mockResolvedValueOnce({
                facets: {
                    f0_count: { entities: 3 },
                    f0_stats: { min: 42, max: 42 },
                    f1_count: { entities: 3 },
                    f1_buckets: { buckets: [] },
                    f2_count: { entities: 3 },
                    f2_buckets: { buckets: [{ val: `<${unit('collapsed')}>`, count: 3, entities: 3 }] },
                    f3_count: { entities: 3 },
                    f3_buckets: { buckets: [] },
                    f4_count: { entities: 3 },
                    f4_buckets: { buckets: [{ val: `<${unit('input')}>`, count: 3, entities: 3 }] }
                }
            } as unknown as SearchResponse)
            .mockResolvedValueOnce({
                facets: {
                    f0_count: { entities: 0 },
                    f1_count: { entities: 0 },
                    f1_buckets: { buckets: [] },
                    f2_count: { entities: 0 },
                    f2_buckets: { buckets: [] },
                    f3_count: { entities: 0 },
                    f3_buckets: { buckets: [] },
                    f4_count: { entities: 0 },
                    f4_buckets: { buckets: [] }
                }
            } as unknown as SearchResponse)
            .mockResolvedValueOnce({
                facets: {
                    f0_count: { entities: 3 },
                    f0_stats: { min: 41, max: 43 },
                    f1_count: { entities: 3 },
                    f1_buckets: { buckets: [] },
                    f2_count: { entities: 3 },
                    f2_buckets: { buckets: [{ val: `<${unit('collapsed')}>`, count: 3, entities: 3 }] },
                    f3_count: { entities: 3 },
                    f3_buckets: { buckets: [] },
                    f4_count: { entities: 3 },
                    f4_buckets: { buckets: [{ val: `<${unit('input')}>`, count: 3, entities: 3 }] }
                }
            } as unknown as SearchResponse)
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const request: QueryFacetRequest = {
            query: { rootShapeId: 'urn:shape:root', criteria: [] },
            fields: [
                field('value', ['part', VALUE_PREDICATE]),
                field('kind', ['part', KIND_PREDICATE]),
                field('unit', ['part', UNIT_PREDICATE]),
                field('input-kind', ['part', 'input', KIND_PREDICATE]),
                field('input-unit', ['part', 'input', UNIT_PREDICATE])
            ],
            signal: new AbortController().signal
        }

        const collapsed = await provider.getFacets(request)
        expect(collapsed.map(facet => facet.count)).toEqual([3, 0, 0, 3, 3])
        expect(collapsed.map(facet => facet.unavailable)).toEqual([undefined, true, true, undefined, undefined])

        const missing = await provider.getFacets(request)
        expect(missing.map(facet => facet.count)).toEqual([0, 0, 0, 0, 3])
        expect(missing.map(facet => facet.unavailable)).toEqual([undefined, true, true, undefined, undefined])

        const available = await provider.getFacets(request)
        expect(available.map(facet => facet.count)).toEqual([3, 3, 3, 3, 3])
        expect(available.map(facet => facet.unavailable)).toEqual([undefined, undefined, undefined, undefined, undefined])
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

    it('keeps quantity unit facets stable after the first facet request', async() => {
        mockQuantityFetch({})
        vi.mocked(executeSolrRequest)
            .mockResolvedValueOnce({
                facets: {
                    f0_count: { entities: 5 },
                    f0_buckets: { buckets: [
                        { val: `<${unit('facet-a')}>`, count: 3, entities: 3 },
                        { val: `<${unit('facet-b')}>`, count: 2, entities: 2 }
                    ] }
                }
            } as unknown as SearchResponse)
            .mockResolvedValueOnce({
                facets: {
                    f0_count: { entities: 1 },
                    f0_buckets: { buckets: [
                        { val: `<${unit('facet-a')}>`, count: 1, entities: 1 }
                    ] }
                }
            } as unknown as SearchResponse)
        const provider = new SolrQueryFacetProvider(quantityConfig())
        const request: QueryFacetRequest = {
            query: { rootShapeId: 'urn:shape:root', criteria: [] },
            fields: [field('unit', ['part', UNIT_PREDICATE])],
            signal: new AbortController().signal
        }

        const initial = await provider.getFacets(request)
        const refreshed = await provider.getFacets({
            ...request,
            query: { ...request.query, criteria: [kindCriterion(kind('facet'))] }
        })

        expect(initial[0].count).toBe(5)
        expect(refreshed[0]).toEqual(initial[0])
        expect(refreshed[0].buckets?.map(bucket => bucket.value.value)).toEqual([
            unit('facet-a'),
            unit('facet-b')
        ])
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
