import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { replySendHookQuery } from '@/features/reply-send-hook/api/reply-send-hook'
import { server } from '@/testing/mocks/server'

function makeQc() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

describe('replySendHookQuery', () => {
  it('returns null when the optional hook is not configured', async () => {
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ message: 'missing' }, { status: 404 }),
      ),
    )

    await expect(makeQc().fetchQuery(replySendHookQuery())).resolves.toBeNull()
  })

  it('rethrows unexpected API errors', async () => {
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ message: 'boom' }, { status: 500 }),
      ),
    )

    await expect(makeQc().fetchQuery(replySendHookQuery())).rejects.toThrow('boom')
  })
})
