import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { ClustersCard } from '@/features/feedback/components/clusters-card'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, className, to }: { children: ReactNode; className?: string; to: string }) => (
    <a className={className} href={to}>
      {children}
    </a>
  ),
}))

function clustersResponse(overrides: Record<string, unknown> = {}) {
  return {
    items: [],
    totalCount: 0,
    clusteringEnabled: true,
    ...overrides,
  }
}

describe('ClustersCard', () => {
  it('shows the compact loading state while the cluster query is pending', () => {
    server.use(http.get('/fb/v1/console/clusters', () => new Promise(() => undefined)))

    const { container } = renderWithProviders(<ClustersCard />)

    expect(screen.getByText('语义聚类')).toBeInTheDocument()
    expect(container.querySelector('.animate-spin')).not.toBeNull()
  })

  it('returns no card when clustering is disabled for the tenant', async () => {
    server.use(
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json(clustersResponse({ clusteringEnabled: false })),
      ),
    )

    const { container } = renderWithProviders(<ClustersCard />)

    await waitFor(() => {
      expect(container).toBeEmptyDOMElement()
    })
  })

  it('renders the enabled empty state without a view-all link', async () => {
    server.use(http.get('/fb/v1/console/clusters', () => HttpResponse.json(clustersResponse())))

    renderWithProviders(<ClustersCard />)

    await waitFor(() => {
      expect(screen.getByText('暂无聚类')).toBeInTheDocument()
    })
    expect(screen.getByText('当相似反馈达到 2 条以上，会自动聚类显示在这里。')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /查看全部聚类/ })).not.toBeInTheDocument()
  })

  it('renders the first three clusters with label fallback and aggregate total', async () => {
    server.use(
      http.get('/fb/v1/console/clusters', ({ request }) => {
        const params = new URL(request.url).searchParams
        expect(params.get('recency_days')).toBe('30')
        expect(params.get('min_count')).toBe('2')
        expect(params.get('limit')).toBe('5')
        return HttpResponse.json(
          clustersResponse({
            totalCount: 4,
            items: [
              {
                clusterId: 'cluster-login',
                count: 6,
                latestAt: '2026-06-22T09:30:00Z',
                label: '登录失败率高',
                sampleTitle: '认证服务超时导致用户重复登录',
              },
              {
                clusterId: 'cluster-empty-label',
                count: 3,
                latestAt: '2026-06-21T09:30:00Z',
                label: '',
                sampleTitle: 'CSV 导入卡在 95%',
              },
              {
                clusterId: 'cluster-billing',
                count: 2,
                latestAt: '2026-06-20T09:30:00Z',
                label: '账单页金额异常',
                sampleTitle: '账单明细的小计和总计不一致',
              },
              {
                clusterId: 'cluster-hidden',
                count: 2,
                latestAt: '2026-06-19T09:30:00Z',
                label: '第四个聚类',
                sampleTitle: '这个条目不应该出现在小卡片里',
              },
            ],
          }),
        )
      }),
    )

    renderWithProviders(<ClustersCard />)

    await waitFor(() => {
      expect(screen.getByText('登录失败率高')).toBeInTheDocument()
    })
    expect(screen.getAllByText('CSV 导入卡在 95%')).toHaveLength(2)
    expect(screen.getByText('账单页金额异常')).toBeInTheDocument()
    expect(screen.queryByText('第四个聚类')).not.toBeInTheDocument()
    expect(screen.getByText('共 4 个聚类')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /查看全部聚类/ })).toHaveAttribute(
      'href',
      '/feedback/clusters',
    )
  })
})
