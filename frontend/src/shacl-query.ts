import type {
    QueryCriterion,
    QueryFacet as ShaclQueryFacet,
    QueryFacetRequest,
    QueryField,
    QueryFacetProvider,
    Query,
    QueryChange
} from '@ulb-darmstadt/shacl-form'
import { isRangeQueryField } from '@ulb-darmstadt/shacl-form'
import type { Term } from '@rdfjs/types'
import { DataFactory } from 'n3'
import { executeSolrRequest, SearchRequest, AggregationFacet } from './solr'
import { fetchLabels, i18n } from './i18n'
import { Config } from '.'
import { convertTermToSi, statFromSi, UnitConversionResolver, QuantitySelection } from './unit-conversion'

const XSD = 'http://www.w3.org/2001/XMLSchema#'
const DATE_TYPES = new Set([`${XSD}date`, `${XSD}dateTime`])
const LUCENE_SPECIAL_RE = /([+\-!(){}[\]^"~*?:\\/]|&&|\|\||\s)/g
const pathIds = new Map<string, Promise<string>>()
const WORLD_BOUNDS = '[-90,-180 TO 90,180]'

type SolrFacetBucket = { val: string | number | boolean, count: number, entities?: number }
type SolrFacetResult = {
    count?: number
    entities?: number
    min?: string | number
    max?: string | number
    buckets?: SolrFacetBucket[]
}

function queryPathId(path: string[]): Promise<string> {
    const input = path.join('\0')
    let id = pathIds.get(input)
    if (!id) {
        id = crypto.subtle.digest('SHA-256', new TextEncoder().encode(input)).then(digest =>
            Array.from(new Uint8Array(digest, 0, 16), byte => byte.toString(16).padStart(2, '0')).join('')
        )
        pathIds.set(input, id)
    }
    return id
}

function expandQueryPath(path: QueryField['path']): string[][] {
    return path.reduce<string[][]>((paths, segment) => {
        const alternatives = Array.isArray(segment) ? segment : [segment]
        return paths.flatMap(path => alternatives.map(alternative => [...path, alternative]))
    }, [[]])
}

export function queryFieldPaths(field: QueryField): string[][] {
    const path = field.shapePath?.length === field.path.length
        ? field.shapePath
        : field.path
    return expandQueryPath(path)
}

function quote(value: string): string {
    return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
}

function normalizeDate(value: string): string {
    if (value.length === 10) {
        return `${value}T00:00:00Z`
    }
    return /(?:Z|[+-]\d\d:\d\d)$/.test(value) ? value : `${value}Z`
}

function storedValue(term: Term, field: QueryField): string {
    if (term.termType === 'NamedNode') {
        return `<${term.value}>`
    }
    return field.datatype && DATE_TYPES.has(field.datatype) ? normalizeDate(term.value) : term.value
}

function wktPolygonToBbox(wkt: string): string | undefined {
    const match = wkt.match(/^POLYGON[(]{2}(.*)[)]{2}$/)
    if (!match) {
        return undefined
    }
    const coords = match[1].split(',').map(pair => {
        const [x, y] = pair.trim().split(/\s+/).map(Number)
        return [x, y] as [number, number]
    })
    if (coords.length < 3) {
        return undefined
    }
    const lngs = coords.map(c => c[0])
    const lats = coords.map(c => c[1])
    return `[${Math.min(...lats)},${Math.min(...lngs)} TO ${Math.max(...lats)},${Math.max(...lngs)}]`
}

function termFromSolr(value: string | number | boolean, field: QueryField): Term {
    const lexical = String(value)
    if (!field.datatype && lexical.startsWith('<') && lexical.endsWith('>')) {
        return DataFactory.namedNode(lexical.slice(1, -1))
    }
    if (field.datatype) {
        const normalized = field.datatype === `${XSD}date` ? lexical.slice(0, 10) : lexical
        return DataFactory.literal(normalized, DataFactory.namedNode(field.datatype))
    }
    return DataFactory.literal(lexical)
}

export class SolrQueryFacetProvider implements QueryFacetProvider {
    private baseFilters: string[] = []
    private readonly unitConversion: UnitConversionResolver
    private readonly initialUnitFacets = new Map<string, ShaclQueryFacet>()
    private loadingCount = 0
    onLoadingChange?: (loading: boolean) => void

    constructor(
        private readonly config: Config
    ) {
        this.unitConversion = new UnitConversionResolver(config, queryFieldPaths)
    }

    private async trackLoading<T>(task: Promise<T>): Promise<T> {
        if (++this.loadingCount === 1) {
            this.onLoadingChange?.(true)
        }
        try {
            return await task
        } finally {
            if (--this.loadingCount === 0) {
                this.onLoadingChange?.(false)
            }
        }
    }

    setBaseFilters(filters: string[]) {
        this.baseFilters = [...filters]
    }

    setSearchContext(term?: string, creator?: string) {
        const filters: string[] = []
        if (creator) {
            filters.push(`creator:${quote(creator)}`)
        }
        if (term) {
            filters.push(`_text_:*${term.replace(LUCENE_SPECIAL_RE, '\\$1')}*`)
        }
        this.setBaseFilters(filters)
    }

    invalidatedFields(change: QueryChange): string[] {
        return this.unitConversion.invalidatedValueFields(
            change.fields,
            change.previousQuery.criteria,
            change.query.criteria
        )
    }

    buildFilters(query: Query, quantities?: QuantitySelection): Promise<string[]> {
        return this.trackLoading(this.buildFiltersTask(query, quantities))
    }

    private async buildFiltersTask(query: Query, quantities?: QuantitySelection): Promise<string[]> {
        const selection = quantities
            ?? await this.unitConversion.resolveSelection(query.criteria.map(criterion => criterion.field), query.criteria)
        const filters = [this.conformanceFilter(query), ...this.baseFilters].filter(Boolean)
        const criteria = await Promise.all(query.criteria.map(criterion => this.criterionFilter(criterion, selection)))
        filters.push(...criteria.filter((filter): filter is string => !!filter))
        return filters
    }

    getFacets(request: QueryFacetRequest): Promise<ShaclQueryFacet[]> {
        return this.trackLoading(this.getFacetsTask(request))
    }

    private async getFacetsTask(request: QueryFacetRequest, allowLearnRetry = true): Promise<ShaclQueryFacet[]> {
        if (!request.fields.length) {
            return []
        }

        const quantitySelection = await this.unitConversion.resolveSelection(request.fields, request.query.criteria)
        const filters = ['docType:entity', ...await this.buildFilters(request.query, quantitySelection)]
        const facet: Record<string, unknown> = {}
        const paths = await Promise.all(request.fields.map(field => this.pathFilter(field)))

        request.fields.forEach((field, index) => {
            const domain = { blockChildren: 'docType:entity', filter: ['docType:value', paths[index]] }
            const valueField = this.facetValueField(field)
            facet[`f${index}_count`] = {
                type: 'query', q: `${valueField}:[* TO *]`, domain,
                facet: { entities: 'uniqueBlock(_root_)' }
            }
            if (isRangeQueryField(field)) {
                facet[`f${index}_stats`] = {
                    type: 'query', q: `${valueField}:[* TO *]`, domain,
                    facet: { entities: 'uniqueBlock(_root_)', min: `min(${valueField})`, max: `max(${valueField})` }
                }
            } else if (this.isSpatialField(field)) {
                facet[`f${index}_heatmap`] = { type: 'heatmap', field: valueField, geom: WORLD_BOUNDS, domain }
            } else {
                facet[`f${index}_buckets`] = {
                    type: 'terms', field: valueField, limit: this.config.solrMaxAggregations, domain,
                    facet: { entities: 'uniqueBlock(_root_)' }
                }
            }
        })

        const query: SearchRequest = { query: '*', filter: filters, facet, limit: 0, offset: 0 }
        const response = await executeSolrRequest(this.config.index, query, request.signal)
        if (response.error) {
            throw new Error(response.error.msg || response.error.trace || 'Solr facet request failed')
        }
        const aggregations = (response.facets || {}) as Record<string, unknown>

        // a kind facet with exactly one bucket identifies the quantity kind unambiguously
        let autoKindDetected = false
        request.fields.forEach((field, index) => {
            queryFieldPaths(field).forEach(path => {
                if (path[path.length - 1] !== this.config.conversionQuantity) {
                    return
                }
                const buckets = (aggregations[`f${index}_buckets`] as SolrFacetResult | undefined)?.buckets || []
                if (buckets.length !== 1) {
                    return
                }
                const term = termFromSolr(buckets[0].val, field)
                const subject = path.slice(0, -1).join('\0')
                if (term.termType === 'NamedNode' && this.unitConversion.learnAutoKind(subject, term.value)) {
                    autoKindDetected = true
                }
            })
        })
        if (autoKindDetected && allowLearnRetry) {
            // re-run the whole request so filters and statistics are computed
            // with the same conversion state
            return this.getFacetsTask(request, false)
        }

        const labelIds = new Set<string>()
        request.fields.forEach((field, index) => {
            if (isRangeQueryField(field) || this.isSpatialField(field)) {
                return
            }
            const bucketResult = aggregations[`f${index}_buckets`] as SolrFacetResult | undefined
            for (const bucket of bucketResult?.buckets || []) {
                const value = termFromSolr(bucket.val, field)
                if (value.termType === 'NamedNode') {
                    labelIds.add(value.value)
                }
            }
        })
        quantitySelection.defaultUnits.forEach(unitURI => labelIds.add(unitURI))
        await fetchLabels(Array.from(labelIds), true)

        const result = request.fields.map((field, index) => {
            const countResult = aggregations[`f${index}_count`] as SolrFacetResult | undefined
            const queryFacet: ShaclQueryFacet = { fieldId: field.id, count: countResult?.entities || 0 }
            if (isRangeQueryField(field)) {
                const stats = aggregations[`f${index}_stats`] as SolrFacetResult | undefined
                const conversion = quantitySelection.conversions.get(field.id)
                if (stats?.min !== undefined) {
                    queryFacet.min = termFromSolr(statFromSi(stats.min, conversion), field)
                }
                if (stats?.max !== undefined) {
                    queryFacet.max = termFromSolr(statFromSi(stats.max, conversion), field)
                }
            } else if (this.isSpatialField(field)) {
                const heatmap = aggregations[`f${index}_heatmap`] as AggregationFacet | undefined
                if (heatmap?.counts_ints2D?.length) {
                    queryFacet.heatmap = {
                        columns: heatmap.columns!, rows: heatmap.rows!,
                        minX: heatmap.minX!, maxX: heatmap.maxX!, minY: heatmap.minY!, maxY: heatmap.maxY!,
                        counts: heatmap.counts_ints2D
                    }
                }
            } else {
                const buckets = (aggregations[`f${index}_buckets`] as SolrFacetResult | undefined)?.buckets || []
                queryFacet.buckets = buckets.map(bucket => {
                    const value = termFromSolr(bucket.val, field)
                    return { value, label: i18n[value.value] || value.value, count: bucket.entities || 0 }
                })
            }
            if (!this.isQuantityUnitField(field)) {
                return queryFacet
            }

            const defaultUnitURI = quantitySelection.defaultUnits.get(field.id)
            if (defaultUnitURI && queryFacet.buckets) {
                const index = queryFacet.buckets.findIndex(bucket => bucket.value.value === defaultUnitURI)
                const defaultBucket = index >= 0
                    ? queryFacet.buckets.splice(index, 1)[0]
                    : {
                        value: DataFactory.namedNode(defaultUnitURI),
                        label: i18n[defaultUnitURI] || defaultUnitURI,
                        count: queryFacet.count
                    }
                queryFacet.buckets.unshift(defaultBucket)
            }

            const initial = this.initialUnitFacets.get(field.id)
            if (initial) {
                return this.cloneFacet(initial)
            }
            this.initialUnitFacets.set(field.id, this.cloneFacet(queryFacet))
            return queryFacet
        })
        this.hideUnavailableQuantityMetadata(request.fields, result)
        quantitySelection.defaultUnits.forEach((unitURI, fieldId) => {
            const facet = result.find(facet => facet.fieldId === fieldId)
            if (facet && !facet.unavailable && facet.count > 0) {
                facet.initialValue = DataFactory.namedNode(unitURI)
            }
        })
        return result
    }

    private hideUnavailableQuantityMetadata(fields: QueryField[], facets: ShaclQueryFacet[]): void {
        if (!this.unitConversion.enabled) {
            return
        }
        const facetsByField = new Map(facets.map(facet => [facet.fieldId, facet]))
        const unavailableBySubject = new Map<string, boolean>()
        for (const field of fields) {
            if (!isRangeQueryField(field)) {
                continue
            }
            const facet = facetsByField.get(field.id)
            const min = facet?.min ? Number(facet.min.value) : NaN
            const max = facet?.max ? Number(facet.max.value) : NaN
            const unavailable = !Number.isFinite(min) || !Number.isFinite(max) || min >= max
            for (const path of queryFieldPaths(field)) {
                if (path[path.length - 1] !== this.config.conversionValue) {
                    continue
                }
                const subject = path.slice(0, -1).join('\0')
                unavailableBySubject.set(subject, (unavailableBySubject.get(subject) ?? true) && unavailable)
            }
        }
        if (!unavailableBySubject.size) {
            return
        }
        for (const field of fields) {
            const subjects = queryFieldPaths(field).flatMap(path => {
                const predicate = path[path.length - 1]
                return predicate === this.config.conversionUnit || predicate === this.config.conversionQuantity
                    ? [path.slice(0, -1).join('\0')]
                    : []
            })
            if (subjects.length && subjects.every(subject => unavailableBySubject.get(subject) === true)) {
                const facet = facetsByField.get(field.id)
                if (facet) {
                    facet.count = 0
                    facet.unavailable = true
                }
            }
        }
    }

    private conformanceFilter(query: Query): string {
        return query.rootShapeId
            ? `shape:${quote(query.rootShapeId)}`
            : ''
    }

    private async pathFilter(field: QueryField): Promise<string> {
        if (!field.path.length) {
            return 'path:__missing_shacl_path__'
        }
        const ids = await Promise.all(queryFieldPaths(field).map(queryPathId))
        return ids.length === 1
            ? `path:${quote(ids[0])}`
            : `(${ids.map(id => `path:${quote(id)}`).join(' OR ')})`
    }

    private valueField(field: QueryField, term?: Term, contains = false): string {
        if (contains) {
            return 'valueText'
        }
        if (this.isSpatialField(field)) {
            return 'valueGeo'
        }
        if (field.datatype && DATE_TYPES.has(field.datatype)) {
            return 'valueDate'
        }
        if (isRangeQueryField(field)) {
            return 'valueNumber'
        }
        if (field.datatype === `${XSD}boolean`) {
            return 'valueBoolean'
        }
        if (term?.termType === 'NamedNode') {
            return 'valueString'
        }
        return 'valueString'
    }

    private facetValueField(field: QueryField): string {
        return this.valueField(field)
    }

    private isQuantityUnitField(field: QueryField): boolean {
        return !!this.config.conversionUnit && queryFieldPaths(field).some(path =>
            path[path.length - 1] === this.config.conversionUnit
        )
    }

    private cloneFacet(facet: ShaclQueryFacet): ShaclQueryFacet {
        return {
            ...facet,
            buckets: facet.buckets?.map(bucket => ({ ...bucket }))
        }
    }

    private isSpatialField(field: QueryField): boolean {
        return !!this.config.geoDataType && field.datatype === this.config.geoDataType
    }

    private async criterionFilter(criterion: QueryCriterion, quantities?: QuantitySelection): Promise<string | undefined> {
        // the unit selection is pure metadata: it drives value conversion only and must not restrict results
        if (quantities?.unitFields.has(criterion.field.id)) {
            return undefined
        }
        const path = await this.pathFilter(criterion.field)
        let valueFilter: string | undefined
        if (criterion.operator === 'contains' && criterion.value) {
            const escaped = criterion.value.value.replace(LUCENE_SPECIAL_RE, '\\$1')
            valueFilter = `valueText:*${escaped}*`
        } else if (criterion.operator === 'equals' && criterion.value) {
            const valueField = this.valueField(criterion.field, criterion.value)
            if (valueField === 'valueGeo') {
                const bbox = wktPolygonToBbox(criterion.value.value)
                if (bbox) {
                    valueFilter = `${valueField}:${bbox}`
                }
            }
            valueFilter ||= `${valueField}:${quote(storedValue(criterion.value, criterion.field))}`
        } else if (criterion.operator === 'range') {
            const conversion = quantities?.conversions.get(criterion.field.id)
            const min = criterion.min ? quote(storedValue(convertTermToSi(criterion.min, conversion), criterion.field)) : '*'
            const max = criterion.max ? quote(storedValue(convertTermToSi(criterion.max, conversion), criterion.field)) : '*'
            if (min !== '*' || max !== '*') {
                valueFilter = `${this.valueField(criterion.field)}:[${min} TO ${max}]`
            }
        }
        if (!valueFilter) {
            return undefined
        }
        return `{!parent which=docType:entity}(docType:value AND ${path} AND ${valueFilter})`
    }
}
