import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./constants', () => ({
    BACKEND_URL: 'http://backend.test'
}))

import { search } from './solr'

describe('search', () => {
    beforeEach(() => {
        vi.unstubAllGlobals()
        vi.stubGlobal('fetch', vi.fn(async() => ({
            json: async() => ({
                responseHeader: { status: 0, QTime: 0 },
                response: { numFound: 0, start: 0, docs: [] }
            })
        })))
    })

    it('matches the search term within longer text tokens', async() => {
        await search('documents', { term: 'super' })

        const request = vi.mocked(fetch).mock.calls[0][1] as RequestInit
        const body = JSON.parse(request.body as string) as { filter: string[] }
        expect(body.filter).toContain('_text_:*super*')
    })
})
