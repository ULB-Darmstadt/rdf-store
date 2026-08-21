import type { QueryCriterion, QueryField } from '@ulb-darmstadt/shacl-form'
import type { Term } from '@rdfjs/types'
import { DataFactory } from 'n3'
import { BACKEND_URL } from './constants'

export type QuantityUnitConversion = {
    multiplier: number
    offset: number
}

export type QuantitySelection = {
    conversions: Map<string, QuantityUnitConversion>
    unitFields: Set<string>
}

type Quantity = {
    unitURI: string
    quantityKindURI: string
    conversion: QuantityUnitConversion | null | undefined
}

type QuantityGroup = {
    valueField: QueryField
    unitURI: string
    quantityKindURI: string
    conversion: QuantityUnitConversion | null | undefined
}

type ConversionConfig = {
    conversionUnit?: string
    conversionQuantity?: string
    conversionValue?: string
}

function toSi(value: number, conversion: QuantityUnitConversion): number {
    return (value + conversion.offset) * conversion.multiplier
}

function fromSi(value: number, conversion: QuantityUnitConversion): number {
    return value / conversion.multiplier - conversion.offset
}

function trimFloat(value: number): number {
    return Number(value.toPrecision(12))
}

export function convertTermToSi(term: Term, conversion?: QuantityUnitConversion): Term {
    if (!conversion || term.termType !== 'Literal') {
        return term
    }
    const value = Number(term.value)
    if (!Number.isFinite(value)) {
        return term
    }
    return DataFactory.literal(String(toSi(value, conversion)), term.datatype)
}

export function statFromSi(value: string | number, conversion?: QuantityUnitConversion): number {
    const num = Number(value)
    if (!conversion || !Number.isFinite(num)) {
        return num
    }
    return trimFloat(fromSi(num, conversion))
}

function selectedIri(term?: Term): string | undefined {
    if (!term) {
        return undefined
    }
    if (term.termType === 'NamedNode') {
        return term.value
    }
    if (term.termType === 'Literal') {
        // facet selections arrive as stored strings, which wrap IRIs in angle brackets
        const value = term.value.trim()
        return value.startsWith('<') && value.endsWith('>') ? value.slice(1, -1) : value
    }
    return undefined
}

export class UnitConversionResolver {
    private readonly conversionUnit: string
    private readonly conversionQuantity: string
    private readonly conversionValue: string
    private readonly expandPaths: (field: QueryField) => string[][]
    private readonly quantityCache = new Map<string, Quantity>()
    // quantity kinds learned from kind facets that offer exactly one bucket,
    // keyed by the measurement subject's path prefix
    private readonly autoQuantityKinds = new Map<string, string>()

    constructor(config: ConversionConfig, expandPaths: (field: QueryField) => string[][]) {
        this.conversionUnit = config.conversionUnit ?? ''
        this.conversionQuantity = config.conversionQuantity ?? ''
        this.conversionValue = config.conversionValue ?? ''
        this.expandPaths = expandPaths
    }

    get enabled(): boolean {
        return this.conversionUnit.length > 0 && this.conversionQuantity.length > 0 && this.conversionValue.length > 0
    }

    learnAutoKind(subject: string, kindURI: string): boolean {
        if (this.autoQuantityKinds.get(subject) === kindURI) {
            return false
        }
        this.autoQuantityKinds.set(subject, kindURI)
        return true
    }

    async resolveSelection(fields: QueryField[], criteria: QueryCriterion[]): Promise<QuantitySelection> {
        const selection: QuantitySelection = { conversions: new Map(), unitFields: new Set() }
        if (!this.enabled) {
            return selection
        }
        // unit/kind sibling fields may not be faceted themselves, but their
        // criteria still identify the selected unit and quantity kind
        const scanned = new Map<string, QueryField>(fields.map(field => [field.id, field]))
        criteria.forEach(criterion => scanned.set(criterion.field.id, criterion.field))
        const { groups, unitFields } = this.scanForQuantities(Array.from(scanned.values()), criteria)
        selection.unitFields = unitFields
        const missing = new Map<string, QuantityGroup>()
        for (const group of groups.values()) {
            const key = this.quantityKey(group)
            if (!this.quantityCache.has(key)) {
                missing.set(key, group)
            }
        }
        await this.fetchQuantities(Array.from(missing.values()))
        for (const group of groups.values()) {
            const cached = this.quantityCache.get(this.quantityKey(group))
            if (cached?.conversion) {
                selection.conversions.set(group.valueField.id, cached.conversion)
            }
        }
        return selection
    }

    private scanForQuantities(fields: QueryField[], criteria: QueryCriterion[]): { groups: Map<string, QuantityGroup>, unitFields: Set<string> } {
        type SubjectGroup = { values: QueryField[], units: QueryField[], kinds: QueryField[] }
        const subjects = new Map<string, SubjectGroup>()
        const unitFields = new Set<string>()
        fields.forEach(field => {
            this.expandPaths(field).forEach(path => {
                const predicate = path[path.length - 1]
                let role: keyof SubjectGroup | undefined
                if (predicate === this.conversionValue) {
                    role = 'values'
                } else if (predicate === this.conversionUnit) {
                    role = 'units'
                } else if (predicate === this.conversionQuantity) {
                    role = 'kinds'
                }
                if (!role) {
                    return
                }
                if (role === 'units') {
                    unitFields.add(field.id)
                }
                const subject = path.slice(0, -1).join('\0')
                const group = subjects.get(subject) ?? { values: [], units: [], kinds: [] }
                subjects.set(subject, group)
                if (!group[role].includes(field)) {
                    group[role].push(field)
                }
            })
        })
        const selected = new Map<string, string>()
        criteria.forEach(criterion => {
            if (criterion.operator === 'equals') {
                const iri = selectedIri(criterion.value)
                if (iri) {
                    selected.set(criterion.field.id, iri)
                }
            }
        })
        const groups = new Map<string, QuantityGroup>()
        for (const [subject, group] of subjects.entries()) {
            const unitURI = group.units.map(field => selected.get(field.id)).find(Boolean)
            const quantityKindURI = group.kinds.map(field => selected.get(field.id)).find(Boolean)
                ?? this.autoQuantityKinds.get(subject)
            if (!unitURI || !quantityKindURI) {
                continue
            }
            for (const valueField of group.values) {
                groups.set(valueField.id, { valueField, unitURI, quantityKindURI, conversion: undefined })
            }
        }
        return { groups, unitFields }
    }

    private async fetchQuantities(quantities: Quantity[]) {
        if (quantities.length === 0) {
            return
        }
        const resp: Quantity[] = await fetch(`${BACKEND_URL}/quantities`, { method: 'POST', body: JSON.stringify(quantities) }).then(r => r.json())
        for (const quantity of resp) {
            // if no conversion possible, then mark and cache this
            if (quantity.conversion === undefined) {
                quantity.conversion = null
            }
            this.quantityCache.set(this.quantityKey(quantity), quantity)
        }
    }

    private quantityKey(quantity: Quantity) {
        return `${quantity.quantityKindURI}\u0000${quantity.unitURI}`
    }
}
