import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { ClustersPage } from '@/features/feedback/components/clusters-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

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
            {
              clusterId: 'cluster-login',
              count: 6,
              latestAt: '2026-06-22T09:30:00Z',
              label: '登录失败率高',
              sampleTitle: '认证服务超时导致用户重复登录',
            },
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
    )

    renderWithProviders(<ClustersPage />)

    await waitFor(() => {
      expect(screen.getByText('筛选视角')).toBeInTheDocument()
    })

    expect(screen.getByText('聚类研判打法')).toBeInTheDocument()
    expect(screen.getAllByText('优先模式').length).toBeGreaterThan(0)
    expect(screen.getAllByText('登录失败率高').length).toBeGreaterThan(0)
    expect(screen.getByText('导入任务卡住')).toBeInTheDocument()
  })

  it('opens the members sheet when selecting a cluster', async () => {
    server.use(
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({
          items: [
            {
              clusterId: 'cluster-login',
              count: 6,
              latestAt: '2026-06-22T09:30:00Z',
              label: '登录失败率高',
              sampleTitle: '认证服务超时导致用户重复登录',
            },
          ],
          totalCount: 1,
          clusteringEnabled: true,
        }),
      ),
      http.get('/fb/v1/console/clusters/cluster-login/members', () =>
        HttpResponse.json({
          items: [
            {
              id: '101',
              content: '认证服务超时导致用户重复登录',
              enrichedTitle: '认证服务超时',
              source: 'app-store',
              createdAt: '2026-06-22T09:00:00Z',
              similarity: 0.92,
            },
          ],
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
})
