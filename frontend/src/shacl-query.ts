import type {
    QueryCriterion,
    QueryFacet as ShaclQueryFacet,
    QueryFacetRequest,
    QueryField,
    QueryFacetProvider,
    Query
} from '@ulb-darmstadt/shacl-form'
import { isRangeQueryField } from '@ulb-darmstadt/shacl-form'
import type { Term } from '@rdfjs/types'
import { DataFactory } from 'n3'
import { executeSolrRequest, fetchFields, SearchRequest, AggregationFacet } from './solr'
import { fetchLabels, i18n } from './i18n'

const XSD = 'http://www.w3.org/2001/XMLSchema#'
const DATE_TYPES = new Set([`${XSD}date`, `${XSD}dateTime`])
const LUCENE_SPECIAL_RE = /([+\-!(){}[\]^"~*?:\\/]|&&|\|\||\s)/g
const queryFieldPrefixes = new Map<string, Promise<string>>()

type SolrFacetBucket = { val: string | number | boolean, count: number }
type SolrFacetResult = { count?: number, buckets?: SolrFacetBucket[] }

function queryFieldPrefix(rootShapeId: string, path: string[]): Promise<string> {
    const input = [rootShapeId, ...path].join('\0')
    let prefix = queryFieldPrefixes.get(input)
    if (!prefix) {
        prefix = crypto.subtle.digest('SHA-256', new TextEncoder().encode(input)).then(digest => {
            const hash = Array.from(new Uint8Array(digest, 0, 16), byte => byte.toString(16).padStart(2, '0')).join('')
            return `_query_.${hash}_`
        })
        queryFieldPrefixes.set(input, prefix)
    }
    return prefix
}

function queryFieldPaths(field: QueryField): string[][] {
    if (!field.shapePath || field.shapePath.length !== field.path.length) return [field.path]

    const paths: string[][] = []
    const seen = new Set<string>()
    const differingSegments = field.shapePath.flatMap((segment, index) =>
        segment === field.path[index] ? [] : [index]
    )

    // Prefer the qualified SHACL path. The indexer can also reach the same value
    // through an inherited, unqualified property and then stores that segment as
    // its RDF predicate. Try increasingly less-specific paths as fallbacks.
    for (let replacements = 0; replacements <= differingSegments.length; replacements++) {
        const visit = (start: number, selected: number[]) => {
            if (selected.length === replacements) {
                const path = [...field.shapePath!]
                for (const index of selected) path[index] = field.path[index]
                const key = path.join('\0')
                if (!seen.has(key)) {
                    seen.add(key)
                    paths.push(path)
                }
                return
            }
            for (let index = start; index < differingSegments.length; index++) {
                visit(index + 1, [...selected, differingSegments[index]])
            }
        }
        visit(0, [])
    }
    return paths
}

function quote(value: string): string {
    return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
}

function normalizeDate(value: string): string {
    if (value.length === 10) return `${value}T00:00:00Z`
    return /(?:Z|[+-]\d\d:\d\d)$/.test(value) ? value : `${value}Z`
}

function storedValue(term: Term, field: QueryField): string {
    if (term.termType === 'NamedNode') return `<${term.value}>`
    return field.datatype && DATE_TYPES.has(field.datatype) ? normalizeDate(term.value) : term.value
}

function isSpatialField(fieldName: string): boolean {
    return fieldName.endsWith('_srpt')
}

const WORLD_BOUNDS = '[-90,-180 TO 90,180]'

function wktPolygonToBbox(wkt: string): string | undefined {
    const match = wkt.match(/^POLYGON[(]{2}(.*)[)]{2}$/)
    if (!match) return undefined
    const coords = match[1].split(',').map(pair => {
        const [x, y] = pair.trim().split(/\s+/).map(Number)
        return [x, y] as [number, number]
    })
    if (coords.length < 3) return undefined
    const lngs = coords.map(c => c[0])
    const lats = coords.map(c => c[1])
    const west = Math.min(...lngs)
    const east = Math.max(...lngs)
    const south = Math.min(...lats)
    const north = Math.max(...lats)
    return `[${south},${west} TO ${north},${east}]`
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
    private fieldNames?: string[]
    private baseFilters: string[] = []

    constructor(
        private readonly index: string,
        private readonly bucketLimit = 100,
    ) {}

    setBaseFilters(filters: string[]) {
        this.baseFilters = [...filters]
    }

    setSearchContext(term?: string, creator?: string) {
        const filters: string[] = []
        if (creator) filters.push(`creator:${quote(creator)}`)
        if (term) filters.push(`_text_:*${term.replace(LUCENE_SPECIAL_RE, '\\$1')}*`)
        this.setBaseFilters(filters)
    }

    async buildFilters(query: Query): Promise<string[]> {
        const fields = await this.getFieldNames()
        return this.buildFiltersWithFields(query, fields)
    }

    private async buildFiltersWithFields(query: Query, fields: string[]): Promise<string[]> {
        const filters = [this.rootFilter(query), ...this.baseFilters]
        const fieldSet = new Set(fields)
        const indexFields = await Promise.all(query.criteria.map(criterion =>
            this.resolveField(query.rootShapeId, criterion.field, fieldSet, criterion)
        ))
        query.criteria.forEach((criterion, index) => {
            const indexField = indexFields[index]
            if (!indexField) {
                filters.push('id:__missing_shacl_query_field__')
                return
            }
            const filter = this.criterionFilter(criterion, indexField)
            if (filter) filters.push(filter)
        })
        return filters
    }

    async getFacets(request: QueryFacetRequest): Promise<ShaclQueryFacet[]> {
        // Solr dynamic fields can appear after resources are created or the
        // index is rebuilt. Refresh discovery with every facet request so a
        // long-lived query form does not hide newly available controls.
        const fields = await this.getFieldNames(true)
        const filters = await this.buildFiltersWithFields(request.query, fields)
        const fieldSet = new Set(fields)
        const facet: Record<string, object | string> = {}
        const resolved = new Map<number, string>()
        const result: ShaclQueryFacet[] = []

        const indexFields = await Promise.all(request.fields.map(field =>
            this.resolveField(request.query.rootShapeId, field, fieldSet)
        ))
        request.fields.forEach((field, index) => {
            const indexField = indexFields[index]
            if (!indexField) {
                result.push({ fieldId: field.id, count: 0 })
                return
            }
            resolved.set(index, indexField)
            facet[`f${index}_count`] = { type: 'query', q: `${indexField}:[* TO *]` }
            if (isRangeQueryField(field)) {
                facet[`f${index}_min`] = `min(${indexField})`
                facet[`f${index}_max`] = `max(${indexField})`
            } else if (isSpatialField(indexField)) {
                facet[`f${index}_heatmap`] = { type: 'heatmap', field: indexField, geom: WORLD_BOUNDS }
            } else {
                facet[`f${index}_buckets`] = { type: 'terms', field: indexField, limit: this.bucketLimit }
            }
        })

        if (resolved.size === 0) return result
        const query: SearchRequest = { query: '*', filter: filters, facet: facet as SearchRequest['facet'], limit: 0, offset: 0 }
        const response = await executeSolrRequest(this.index, query, request.signal)
        if (response.error) throw new Error(response.error.msg || response.error.trace || 'Solr facet request failed')
        const aggregations = response.facets || {}

        const labelIds = new Set<string>()
        for (const index of resolved.keys()) {
            const field = request.fields[index]
            if (isRangeQueryField(field) || isSpatialField(resolved.get(index)!)) continue
            const bucketResult = aggregations[`f${index}_buckets`] as SolrFacetResult | undefined
            for (const bucket of bucketResult?.buckets || []) {
                const value = termFromSolr(bucket.val, field)
                if (value.termType === 'NamedNode') labelIds.add(value.value)
            }
        }
        await fetchLabels(Array.from(labelIds), true)

        for (const index of resolved.keys()) {
            const field = request.fields[index]
            const countResult = aggregations[`f${index}_count`] as SolrFacetResult | undefined
            const queryFacet: ShaclQueryFacet = { fieldId: field.id, count: countResult?.count || 0 }
            if (isRangeQueryField(field)) {
                const min = aggregations[`f${index}_min`]
                const max = aggregations[`f${index}_max`]
                if (min !== undefined && typeof min !== 'object') queryFacet.min = termFromSolr(min, field)
                if (max !== undefined && typeof max !== 'object') queryFacet.max = termFromSolr(max, field)
            } else if (isSpatialField(resolved.get(index)!)) {
                const heatmap = aggregations[`f${index}_heatmap`] as AggregationFacet | undefined
                if (heatmap?.counts_ints2D?.length) {
                    queryFacet.heatmap = {
                        columns: heatmap.columns!,
                        rows: heatmap.rows!,
                        minX: heatmap.minX!,
                        maxX: heatmap.maxX!,
                        minY: heatmap.minY!,
                        maxY: heatmap.maxY!,
                        counts: heatmap.counts_ints2D
                    }
                }
            } else {
                const bucketResult = aggregations[`f${index}_buckets`] as SolrFacetResult | undefined
                queryFacet.buckets = (bucketResult?.buckets || []).map(bucket => {
                    const value = termFromSolr(bucket.val, field)
                    return { value, label: i18n[value.value] || value.value, count: bucket.count }
                })
            }
            result.push(queryFacet)
        }
        console.log('--- facets', result)
        return result
    }

    private async getFieldNames(refresh = false): Promise<string[]> {
        if (refresh || !this.fieldNames) {
            this.fieldNames = await fetchFields(this.index)
        }
        return this.fieldNames
    }

    private rootFilter(query: Query): string {
        return query.rootShapeId ? `rootShape:${quote(query.rootShapeId)}` : ''
    }

    private async resolveField(rootShapeId: string, field: QueryField, fields: ReadonlySet<string>, criterion?: QueryCriterion): Promise<string | undefined> {
        if (!field.path.length) return undefined
        const suffixes = criterion?.operator === 'contains'
            ? ['txt']
            : isRangeQueryField(field)
                ? [field.datatype && DATE_TYPES.has(field.datatype) ? 'dts' : 'ds']
                : field.datatype === `${XSD}boolean`
                    ? ['bs', 'ss']
                    : ['srpt', 'txt', 'ss']
        for (const path of queryFieldPaths(field)) {
            const prefix = await queryFieldPrefix(rootShapeId, path)
            const resolved = suffixes.map(suffix => `${prefix}${suffix}`).find(name => fields.has(name))
                ?? Array.from(fields).find(name => name.startsWith(prefix))
            if (resolved) return resolved
        }
        return undefined
    }

    private criterionFilter(criterion: QueryCriterion, indexField: string): string | undefined {
        if (criterion.operator === 'contains' && criterion.value) {
            const escaped = criterion.value.value.replace(LUCENE_SPECIAL_RE, '\\$1')
            return `${indexField}:*${escaped}*`
        }
        if (criterion.operator === 'equals' && criterion.value) {
            if (indexField.endsWith('_srpt')) {
                const bbox = wktPolygonToBbox(criterion.value.value)
                if (bbox) return `${indexField}:${bbox}`
            }
            return `${indexField}:${quote(storedValue(criterion.value, criterion.field))}`
        }
        if (criterion.operator === 'range') {
            const min = criterion.min ? quote(storedValue(criterion.min, criterion.field)) : '*'
            const max = criterion.max ? quote(storedValue(criterion.max, criterion.field)) : '*'
            if (min !== '*' || max !== '*') return `${indexField}:[${min} TO ${max}]`
        }
        return undefined
    }
}
