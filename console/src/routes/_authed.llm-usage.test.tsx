import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { LLMUsagePage } from '@/routes/_authed.llm-usage'
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

    renderWithProviders(<LLMUsagePage />)

    await waitFor(() => {
      expect(screen.getByText('gpt-5.5')).toBeInTheDocument()
    })
    expect(screen.getByText('涉及模型')).toBeInTheDocument()
    expect(screen.getByText('分析建议')).toBeInTheDocument()
    expect(screen.getAllByText('$0.0162').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('1,200').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('340').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('/1')).toBeInTheDocument()
  }, 20_000) // Route-level smoke includes lazy route preload plus async chart data.

  it('renders the empty state and reloads when filters change', async () => {
    const urls: string[] = []
    server.use(
      http.get('/fb/v1/console/llm-usage', ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({
          periodStart: '2026-06-01T00:00:00Z',
          periodEnd: '2026-06-11T00:00:00Z',
          granularity: 'week',
          promptTokens: '0',
          completionTokens: '0',
          costUsd: 0,
          calls: '0',
          errors: '0',
          series: [],
        })
      }),
    )

    const { user } = renderWithProviders(<LLMUsagePage />)

    expect(await screen.findByText('还没有 LLM 调用')).toBeInTheDocument()

    const choose = async (index: number, optionName: string) => {
      await user.click(screen.getAllByRole('combobox')[index])
      await user.click(await screen.findByRole('option', { name: optionName }))
    }

    await choose(0, '7 天')
    await waitFor(() =>
      expect(urls.some((url) => new URL(url).searchParams.get('range') === 'now-7d')).toBe(true),
    )

    await choose(1, '按月')
    await waitFor(() =>
      expect(urls.some((url) => new URL(url).searchParams.get('granularity') === 'month')).toBe(
        true,
      ),
    )
  }, 20_000)
})
