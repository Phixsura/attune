import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { createElement, type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { usePreviewEnrichPrompt } from '@/features/settings/api/preview-enrich-prompt'
import { server } from '@/testing/mocks/server'

describe('usePreviewEnrichPrompt', () => {
  it('POSTs body to /enrich-config/preview and returns the rendered prompt', async () => {
    let observed: unknown = null
    server.use(
      http.post('/fb/v1/console/enrich-config/preview', async ({ request }) => {
        observed = await request.json()
        return HttpResponse.json({ rendered: '[rendered output]' })
      }),
    )
    const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children)
    const { result } = renderHook(() => usePreviewEnrichPrompt(), { wrapper })
    result.current.mutate({
      content: 'sample',
      modules: ['m1'],
    } as Parameters<typeof result.current.mutate>[0])
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(observed).toEqual({ content: 'sample', modules: ['m1'] })
    expect(result.current.data?.rendered).toBe('[rendered output]')
  })
})
