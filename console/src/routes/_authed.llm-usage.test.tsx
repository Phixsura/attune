import { HttpResponse, http } from 'msw'
import { Suspense } from 'react'
import { describe, expect, it } from 'vitest'
import { Route as LLMUsageRoute } from '@/routes/_authed.llm-usage'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('_authed.llm-usage route', () => {
  it('renders aggregate cards and model rows from the usage query', async () => {
    server.use(
      http.get('/fb/v1/console/llm-usage', () =>
        HttpResponse.json({
          periodStart: '2026-06-01T00:00:00Z',
          periodEnd: '2026-06-11T00:00:00Z',
          granularity: 'week',
          promptTokens: '1200',
          completionTokens: '340',
          costUsd: 0.0162,
          calls: '3',
          errors: '1',
          series: [
            {
              bucket: '2026-06-08T00:00:00Z',
              tenantId: 'tenant-1',
              modelId: 'gpt-5.5',
              promptTokens: '1200',
              completionTokens: '340',
              costUsd: 0.0162,
              calls: '3',
              errors: '1',
            },
          ],
        }),
      ),
    )

    const LLMUsagePage = LLMUsageRoute.options.component as React.ComponentType & {
      preload?: () => Promise<unknown>
    }
    if (!LLMUsagePage) throw new Error('LLM usage component missing on Route.options')
    if (LLMUsagePage.preload) await LLMUsagePage.preload()

    renderWithProviders(
      <Suspense fallback={null}>
        <LLMUsagePage />
      </Suspense>,
    )

    await waitFor(() => {
      expect(screen.getByText('gpt-5.5')).toBeInTheDocument()
    })
    expect(screen.getAllByText('$0.0162').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('1,200').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('340').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('/1')).toBeInTheDocument()
  })
})
