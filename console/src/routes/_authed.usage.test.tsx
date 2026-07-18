import { isRedirect } from '@tanstack/react-router'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { Route as LegacyUsageRoute, UsagePage } from '@/routes/_authed.usage'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('_authed.usage route', () => {
  it('redirects the legacy usage shortcut to analytics usage', () => {
    const beforeLoad = LegacyUsageRoute.options.beforeLoad as () => void
    let thrown: unknown

    try {
      beforeLoad()
    } catch (err) {
      thrown = err
    }

    expect(isRedirect(thrown)).toBe(true)
  })

  it('renders the upgraded hero summary and chart sections from the usage query', async () => {
    server.use(
      http.get('/fb/v1/console/usage', () =>
        HttpResponse.json({
          total: '4',
          quota: '',
          periodStart: '2026-06-01T00:00:00Z',
          periodEnd: '2026-06-21T00:00:00Z',
          series: [{ bucket: '2026-06-21T00:00:00Z', value: '4' }],
        }),
      ),
    )

    renderWithProviders(<UsagePage />)

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '用量' })).toBeInTheDocument()
    })
    expect(screen.getByText('当前接收量')).toBeInTheDocument()
    expect(screen.getByText('活跃天数')).toBeInTheDocument()
    expect(screen.getByText('日均接收')).toBeInTheDocument()
    expect(screen.getByText('本月已接收')).toBeInTheDocument()
    expect(screen.getByText('每日分布')).toBeInTheDocument()
  })

  it('renders an empty chart state when the usage series is empty', async () => {
    server.use(
      http.get('/fb/v1/console/usage', () =>
        HttpResponse.json({
          total: '0',
          quota: '100',
          periodStart: '2026-06-01T00:00:00Z',
          periodEnd: '2026-06-21T00:00:00Z',
          series: [],
        }),
      ),
    )

    renderWithProviders(<UsagePage />)

    expect(await screen.findByText('本月还没有反馈')).toBeInTheDocument()
  })

  it('renders nothing when the usage payload is empty', async () => {
    server.use(http.get('/fb/v1/console/usage', () => HttpResponse.json(null)))

    const { container } = renderWithProviders(<UsagePage />)

    await waitFor(() => expect(container).toBeEmptyDOMElement())
  })
})
