import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { useLogout } from '@/features/session/api/logout'
import { getCsrfToken, setCsrfToken } from '@/lib/api-client'
import { server } from '@/testing/mocks/server'
import { renderHook, waitFor } from '@/testing/test-utils'

describe('useLogout', () => {
  beforeEach(() => setCsrfToken('csrf-token'))
  afterEach(() => setCsrfToken(null))

  it('posts logout, clears CSRF state, and removes the cached session', async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    queryClient.setQueryData(['console', 'me'], { user: { name: 'Ada' } })
    server.use(
      http.post('/fb/v1/console/logout', ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
    const { result } = renderHook(() => useLogout(), { wrapper })

    result.current.mutate()

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(getCsrfToken()).toBeNull()
    expect(queryClient.getQueryData(['console', 'me'])).toBeUndefined()
  })
})
