import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { ClustersPage } from '@/features/feedback/components/clusters-page'
import { server } from '@/testing/mocks/server'
import { fireEvent, renderWithProviders, screen, waitFor } from '@/testing/test-utils'

function clusterFixture(
  overrides: Partial<Record<'clusterId' | 'label' | 'sampleTitle', string>> = {},
) {
  return {
    clusterId: overrides.clusterId ?? 'cluster-login',
    count: 6,
    latestAt: '2026-06-22T09:30:00Z',
    label: overrides.label ?? '登录失败率高',
    sampleTitle: overrides.sampleTitle ?? '认证服务超时导致用户重复登录',
  }
}

function memberFixture(
  overrides: Partial<Record<'id' | 'content' | 'enrichedTitle', string>> = {},
) {
  return {
    id: overrides.id ?? '101',
    content: overrides.content ?? '认证服务超时导致用户重复登录',
    enrichedTitle: overrides.enrichedTitle ?? '认证服务超时',
    source: 'app-store',
    createdAt: '2026-06-22T09:00:00Z',
    similarity: 0.92,
  }
}

describe('ClustersPage', () => {
  it('renders the full disabled-state workbench when clustering is off', async () => {
    server.use(
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({
          items: [],
          totalCount: 0,
          clusteringEnabled: false,
        }),
      ),
    )

    renderWithProviders(<ClustersPage />)

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '语义聚类' })).toBeInTheDocument()
    })

    expect(screen.getByText('覆盖反馈')).toBeInTheDocument()
    expect(screen.getAllByText('聚类状态').length).toBeGreaterThan(0)
    expect(screen.getAllByText('未启用').length).toBeGreaterThan(0)
    expect(screen.getByText('当前租户还没有开放语义聚类')).toBeInTheDocument()
    expect(screen.getByText('启用后会出现什么')).toBeInTheDocument()
    expect(screen.getByTestId('clusters-enable-link')).toHaveAttribute(
      'href',
      '/integrations/digests',
    )
  })

  it('renders the workbench, spotlight card, and list when clustering is enabled', async () => {
    server.use(
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({
          items: [
            clusterFixture(),
            {
              clusterId: 'cluster-import',
              count: 3,
              latestAt: '2026-06-20T09:30:00Z',
              label: '导入任务卡住',
              sampleTitle: 'CSV 导入在 95% 卡住',
            },
          ],
          totalCount: 2,
          clusteringEnabled: true,
        }),
      ),
      http.get('/fb/v1/console/clusters/cluster-login/members', () =>
        HttpResponse.json({
          items: [memberFixture()],
          clusterLabel: '登录失败率高',
          totalCount: 1,
        }),
      ),
    )

    const { user } = renderWithProviders(<ClustersPage />)

    await waitFor(() => {
      expect(screen.getByText('筛选视角')).toBeInTheDocument()
    })

    expect(screen.getByText('聚类研判打法')).toBeInTheDocument()
    expect(screen.getAllByText('优先模式').length).toBeGreaterThan(0)
    expect(screen.getAllByText('登录失败率高').length).toBeGreaterThan(0)
    expect(screen.getByText('导入任务卡住')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '查看成员' }))

    await waitFor(() => {
      expect(screen.getByText('成员总数')).toBeInTheDocument()
    })
  })

  it('opens the members sheet when selecting a cluster', async () => {
    server.use(
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({
          items: [clusterFixture()],
          totalCount: 1,
          clusteringEnabled: true,
        }),
      ),
      http.get('/fb/v1/console/clusters/cluster-login/members', () =>
        HttpResponse.json({
          items: [memberFixture()],
          clusterLabel: '登录失败率高',
          totalCount: 1,
        }),
      ),
    )

    const { user } = renderWithProviders(<ClustersPage />)

    await waitFor(() => {
      expect(screen.getAllByText('登录失败率高').length).toBeGreaterThan(0)
    })

    await user.click(screen.getAllByRole('button', { name: /登录失败率高/ })[0])

    await waitFor(() => {
      expect(screen.getByText('成员总数')).toBeInTheDocument()
    })
    expect(screen.getByText('认证服务超时')).toBeInTheDocument()
    expect(screen.getAllByText('认证服务超时导致用户重复登录').length).toBeGreaterThan(0)
    expect(screen.getByText(/相似度 92%/)).toBeInTheDocument()
  })

  it('updates filters and refetches the workbench query', async () => {
    const searches: string[] = []
    server.use(
      http.get('/fb/v1/console/clusters', ({ request }) => {
        const url = new URL(request.url)
        searches.push(url.search)
        return HttpResponse.json({
          items: [clusterFixture()],
          totalCount: 1,
          clusteringEnabled: true,
        })
      }),
    )

    const { user } = renderWithProviders(<ClustersPage />)

    await waitFor(() => {
      expect(screen.getByText('筛选视角')).toBeInTheDocument()
    })

    fireEvent.change(screen.getByRole('searchbox', { name: '搜索聚类标签...' }), {
      target: { value: 'login' },
    })

    await waitFor(() => {
      expect(searches.some((search) => new URLSearchParams(search).get('q') === 'login')).toBe(true)
    })

    await user.click(screen.getAllByRole('combobox')[0])
    await user.click(await screen.findByRole('option', { name: '反馈数量' }))
    await waitFor(() => {
      expect(screen.getAllByRole('combobox')).toHaveLength(3)
    })
    await user.click(screen.getAllByRole('combobox')[1])
    await user.click(await screen.findByRole('option', { name: '近 7 天' }))
    await waitFor(() => {
      expect(screen.getAllByRole('combobox')).toHaveLength(3)
    })
    await user.click(screen.getAllByRole('combobox')[2])
    await user.click(await screen.findByRole('option', { name: '至少 3 条' }))

    await waitFor(() => {
      expect(
        searches.some((search) => {
          const params = new URLSearchParams(search)
          return (
            params.get('q') === 'login' &&
            params.get('sort') === 'count' &&
            params.get('recency_days') === '7' &&
            params.get('min_count') === '3'
          )
        }),
      ).toBe(true)
    })
    expect(screen.getByText('已启用 4 项')).toBeInTheDocument()
  })

  it('retries after a cluster query failure', async () => {
    let attempts = 0
    server.use(
      http.get('/fb/v1/console/clusters', () => {
        attempts += 1
        if (attempts === 1) {
          return HttpResponse.json({ message: 'cluster query failed' }, { status: 500 })
        }
        return HttpResponse.json({
          items: [clusterFixture()],
          totalCount: 1,
          clusteringEnabled: true,
        })
      }),
    )

    const { user } = renderWithProviders(<ClustersPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => {
      expect(screen.getAllByText('登录失败率高').length).toBeGreaterThan(0)
    })
    expect(attempts).toBeGreaterThanOrEqual(2)
  })

  it('loads additional cluster rows from the virtualized list', async () => {
    server.use(
      http.get('/fb/v1/console/clusters', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor')
        if (cursor === 'next-cluster') {
          return HttpResponse.json({
            items: [
              clusterFixture({
                clusterId: 'cluster-import',
                label: '导入任务卡住',
                sampleTitle: 'CSV 导入在 95% 卡住',
              }),
            ],
            totalCount: 2,
            clusteringEnabled: true,
          })
        }
        return HttpResponse.json({
          items: [clusterFixture()],
          nextCursor: 'next-cluster',
          totalCount: 2,
          clusteringEnabled: true,
        })
      }),
    )

    const { user } = renderWithProviders(<ClustersPage />)

    await waitFor(() => {
      expect(screen.getAllByText('登录失败率高').length).toBeGreaterThan(0)
    })

    await user.click(screen.getByRole('button', { name: '加载更多' }))

    await waitFor(() => {
      expect(screen.getByText('导入任务卡住')).toBeInTheDocument()
    })
  })

  it('pages through members and closes the selected cluster sheet', async () => {
    server.use(
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({
          items: [clusterFixture()],
          totalCount: 1,
          clusteringEnabled: true,
        }),
      ),
      http.get('/fb/v1/console/clusters/cluster-login/members', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor')
        if (cursor === 'next-member') {
          return HttpResponse.json({
            items: [
              memberFixture({
                id: '102',
                content: '登录后立即被踢回登录页',
                enrichedTitle: '登录态失效',
              }),
            ],
            clusterLabel: '登录失败率高',
            totalCount: 2,
          })
        }
        return HttpResponse.json({
          items: [memberFixture()],
          nextCursor: 'next-member',
          clusterLabel: '登录失败率高',
          totalCount: 2,
        })
      }),
    )

    const { user } = renderWithProviders(<ClustersPage />)

    await waitFor(() => {
      expect(screen.getAllByText('登录失败率高').length).toBeGreaterThan(0)
    })

    await user.click(screen.getByRole('button', { name: '登录失败率高, 6 6 条' }))

    await waitFor(() => {
      expect(screen.getByText('成员总数')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '加载更多' }))

    await waitFor(() => {
      expect(screen.getByText('登录态失效')).toBeInTheDocument()
    })

    await user.keyboard('{Escape}')

    await waitFor(() => {
      expect(screen.queryByText('成员总数')).not.toBeInTheDocument()
    })
  })
})
