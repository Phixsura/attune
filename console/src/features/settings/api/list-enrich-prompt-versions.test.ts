import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { listEnrichPromptVersions } from '@/features/settings/api/list-enrich-prompt-versions'
import { server } from '@/testing/mocks/server'

describe('listEnrichPromptVersions', () => {
  it('builds the default versions query', async () => {
    const captured: { url?: string } = {}
    server.use(
      http.get('/fb/v1/console/enrich-config/versions', ({ request }) => {
        captured.url = request.url
        return HttpResponse.json({ versions: [] })
      }),
    )

    const result = await listEnrichPromptVersions({})

    expect(result).toEqual({ versions: [] })
    const url = new URL(captured.url ?? '')
    expect(url.searchParams.get('limit')).toBe('12')
    expect(url.searchParams.has('cursor')).toBe(false)
    expect(url.searchParams.has('q')).toBe(false)
  })

  it('trims search text and includes cursor and custom limit', async () => {
    const captured: { url?: string } = {}
    server.use(
      http.get('/fb/v1/console/enrich-config/versions', ({ request }) => {
        captured.url = request.url
        return HttpResponse.json({ versions: [{ version: 'v2' }], nextCursor: 'cur-3' })
      }),
    )

    const result = await listEnrichPromptVersions({
      cursor: 'cur-2',
      limit: 5,
      q: '  escalation  ',
    })

    expect(result).toEqual({ versions: [{ version: 'v2' }], nextCursor: 'cur-3' })
    const url = new URL(captured.url ?? '')
    expect(url.searchParams.get('limit')).toBe('5')
    expect(url.searchParams.get('cursor')).toBe('cur-2')
    expect(url.searchParams.get('q')).toBe('escalation')
  })
})
