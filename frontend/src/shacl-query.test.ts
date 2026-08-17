import { describe, expect, it, vi } from 'vitest'
import type { QueryField } from '@ulb-darmstadt/shacl-form'

vi.mock('@ulb-darmstadt/shacl-form', () => ({
    isRangeQueryField: () => false
}))

import { queryFieldPaths } from './shacl-query'

function field(path: QueryField['path'], shapePath?: QueryField['shapePath']): QueryField {
    return { path, shapePath } as QueryField
}

describe('queryFieldPaths', () => {
    it('keeps qualified branches isolated', () => {
        expect(queryFieldPaths(field(
            ['http://example.org/hasPart', 'http://example.org/hasKindOfQuantity'],
            ['urn:property:fill-volume', 'http://example.org/hasKindOfQuantity']
        ))).toEqual([
            ['urn:property:fill-volume', 'http://example.org/hasKindOfQuantity']
        ])
    })

    it('falls back to RDF paths when no complete shape path exists', () => {
        expect(queryFieldPaths(field([
            ['http://example.org/hasPart', 'http://example.org/contains'],
            'http://example.org/value'
        ]))).toEqual([
            ['http://example.org/hasPart', 'http://example.org/value'],
            ['http://example.org/contains', 'http://example.org/value']
        ])
    })
})
