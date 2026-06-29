import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { AnalyticsDashboard } from '@/features/feedback/components/analytics-dashboard'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

const statsOk = {
  total: '42',
  urgentCount: '3',
  periodStart: '2026-06-01T00:00:00Z',
  periodEnd: '2026-06-29T00:00:00Z',
  dims: [{ dim: 'severity', top: [{ value: 'P0', count: '7' }] }],
}

const configOk = {
  config: {
    dimensions: [
      {
        name: 'severity',
        displayName: { entries: { default: 'Severity' } },
        kind: 'single',
        taxonomy: [{ value: 'P0', displayName: { entries: { default: 'P0' } }, examples: [] }],
        urgentSet: ['P0'],
        required: false,
        examples: [],
        extractionHint: '',
      },
    ],
  },
}

const usageOk = {
  total: '42',
  periodStart: '2026-06-01T00:00:00Z',
  periodEnd: '2026-06-29T00:00:00Z',
  series: [
    { bucket: '2026-06-28T00:00:00Z', value: '10' },
    { bucket: '2026-06-29T00:00:00Z', value: '5' },
  ],
}

function setupHandlers(overrides?: { stats?: object; config?: object; usage?: object }) {
  server.use(
    http.get('/fb/v1/console/feedback/stats', () => HttpResponse.json(overrides?.stats ?? statsOk)),
    http.get('/fb/v1/console/enrich-config', () =>
      HttpResponse.json(overrides?.config ?? configOk),
    ),
    http.get('/fb/v1/console/usage', () => HttpResponse.json(overrides?.usage ?? usageOk)),
  )
}

describe('AnalyticsDashboard', () => {
  it('renders key metrics from stats + usage', async () => {
    setupHandlers()
    renderWithProviders(<AnalyticsDashboard />)
    await waitFor(() => expect(screen.getByText('分析总览')).toBeInTheDocument())
    expect(screen.getAllByText('42').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('3').length).toBeGreaterThanOrEqual(1)
  })

  it('renders empty state when total is 0', async () => {
    setupHandlers({
      stats: { ...statsOk, total: '0', urgentCount: '0', dims: [] },
      usage: { ...usageOk, total: '0', series: [] },
    })
    renderWithProviders(<AnalyticsDashboard />)
    await waitFor(() => expect(screen.getByText('本月暂无反馈')).toBeInTheDocument())
  })

  it('renders dimension distribution when data exists', async () => {
    setupHandlers()
    renderWithProviders(<AnalyticsDashboard />)
    await waitFor(() => expect(screen.getByText('Severity')).toBeInTheDocument())
    expect(screen.getByText('P0')).toBeInTheDocument()
  })
})
