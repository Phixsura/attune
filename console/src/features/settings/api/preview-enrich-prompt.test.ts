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
        return HttpResponse.json({
          renderedPrompt: '[rendered output]',
          promptPolicy: {
            policyId: 'enrich.default',
            policyVersion: '1',
            promptVersion: 'enrich.default@1',
            mode: 'default',
            promptSource: 'built_in',
            templateLanguage: 'en',
            displayLocale: 'zh-CN',
            displayLanguageName: 'Simplified Chinese',
            variables: [],
            outputs: [],
            warnings: [],
          },
        })
      }),
    )
    const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children)
    const { result } = renderHook(() => usePreviewEnrichPrompt(), { wrapper })
    result.current.mutate({
      sampleContent: 'sample feedback content',
      useDraftConfig: false,
      dimensions: [],
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(observed).toEqual({
      sampleContent: 'sample feedback content',
      useDraftConfig: false,
      dimensions: [],
    })
    expect(result.current.data?.renderedPrompt).toBe('[rendered output]')
    expect(result.current.data?.promptPolicy?.promptVersion).toBe('enrich.default@1')
  })
})
