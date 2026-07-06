import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { describe, expect, it, vi } from 'vitest'
import {
  ReliabilityPage,
  type ReliabilityPageProps,
} from '@/features/reliability/components/reliability-page'
import { expectNoA11yViolations } from '@/testing/a11y'
import { renderWithProviders, screen } from '@/testing/test-utils'

function renderReliabilityPage(props: ReliabilityPageProps) {
  const rootRoute = createRootRoute()
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/',
    component: () => <ReliabilityPage {...props} />,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })

  return renderWithProviders(<RouterProvider router={router} />)
}

function buildProps(overrides: Partial<ReliabilityPageProps> = {}): ReliabilityPageProps {
  const onRefreshAll = overrides.onRefreshAll ?? vi.fn()
  const defaults: ReliabilityPageProps = {
    tenantName: 'Tenant One',
    dashboardHref: '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
    isRefreshing: false,
    onRefreshAll,
    failedQueries: [],
    readiness: {
      status: 'warn',
      tone: 'urgent',
      heroTone: 'urgent',
      value: '告警',
      hint: '2 个通过 · 1 个告警 · 0 个失败 · 用时 42ms。',
    },
    authMode: {
      tone: 'active',
      heroTone: 'active',
      value: '混合',
      hint: '仅 SSO 时需要确保 break-glass 路径可用。',
    },
    apiKeys: {
      tone: 'active',
      heroTone: 'default',
      value: '1/2',
      hint: '1 个活跃 · 1 个非活跃，共 2 个。',
    },
    mcpClients: {
      tone: 'active',
      heroTone: 'default',
      value: '1/2',
      hint: '1 个活跃 · 1 个已撤销，共 2 个。',
    },
    gdpr: {
      tone: 'urgent',
      heroTone: 'active',
      value: '3',
      hint: '1 个排队 · 2 个处理中 · 1 个已就绪导出 · 1 个待执行删除。 · 下一次导出过期：2 小时后。',
    },
    deadDeliveries: {
      tone: 'urgent',
      heroTone: 'urgent',
      value: '2',
      hint: '1 个可重试 · 1 个处理中。',
    },
    releaseContext: {
      version: {
        tone: 'active',
        heroTone: 'active',
        value: '5d6ea83',
        hint: '已启动 42 分钟前。',
      },
      environment: {
        tone: 'active',
        heroTone: 'active',
        value: 'production',
        hint: 'profile=production。',
      },
      owner: {
        tone: 'active',
        heroTone: 'active',
        value: 'Platform',
        hint: 'Runbook 与升级通道应保持同步。',
      },
      lifecycle: {
        tone: 'active',
        heroTone: 'active',
        value: 'supported',
        hint: 'Current runtime contract is within the supported window.',
      },
      recovery: {
        tone: 'active',
        heroTone: 'active',
        value: '通过',
        hint: 'Last restore drill passed (today)',
      },
      compatibility: {
        tone: 'active',
        heroTone: 'active',
        value: '5 rules',
        hint: 'Additive · Breaking · Deprecated with shim · Rename with alias · Migration step',
      },
      glossary:
        'Environment · Profile · Service · Owner · Policy mode · Release state · Lifecycle state · Risk class',
      runbookHref: 'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
      escalationHref: 'https://github.com/Phixsura/attune/issues/new/choose',
    },
  }
  return {
    ...defaults,
    ...overrides,
    onRefreshAll,
  }
}

describe('ReliabilityPage', () => {
  it('renders the reliability summary and dashboard shortcut', async () => {
    const onRefreshAll = vi.fn()
    const props = buildProps({ onRefreshAll })

    const { container, user } = renderReliabilityPage(props)

    await screen.findByText('可靠性总览')
    expect(screen.getByText('可靠性总览')).toBeInTheDocument()
    expect(screen.getByText('打开 tenant impact dashboard')).toBeInTheDocument()
    expect(screen.getByText('运行快照')).toBeInTheDocument()
    expect(screen.getByText('发布与归属')).toBeInTheDocument()
    expect(screen.getByText('快速入口')).toBeInTheDocument()
    expect(screen.getByText('1 个活跃 · 1 个非活跃，共 2 个。')).toBeInTheDocument()
    expect(
      screen.getByText(/1 个排队 · 2 个处理中 · 1 个已就绪导出 · 1 个待执行删除。/),
    ).toBeInTheDocument()
    expect(screen.getByText(/1 个可重试 · 1 个处理中。/)).toBeInTheDocument()
    expect(screen.getByText('5d6ea83')).toBeInTheDocument()
    expect(screen.getByText('supported')).toBeInTheDocument()
    expect(screen.getByText('恢复')).toBeInTheDocument()
    expect(screen.getByText('5 rules')).toBeInTheDocument()
    expect(screen.getByText('Canonical terms')).toBeInTheDocument()
    expect(
      screen.getByText(
        'Environment · Profile · Service · Owner · Policy mode · Release state · Lifecycle state · Risk class',
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /运行手册/ })).toHaveAttribute(
      'href',
      'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
    )
    expect(screen.getByRole('link', { name: /升级通道/ })).toHaveAttribute(
      'href',
      'https://github.com/Phixsura/attune/issues/new/choose',
    )

    const dashboardLink = screen.getByRole('link', { name: '打开 tenant impact dashboard' })
    expect(dashboardLink).toHaveAttribute(
      'href',
      '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
    )

    await user.click(screen.getByRole('button', { name: '刷新' }))
    expect(onRefreshAll).toHaveBeenCalledTimes(1)

    await expectNoA11yViolations(container)
  })

  it('surfaces partial query failures without hiding the rest of the page', async () => {
    const props = buildProps({
      failedQueries: [{ label: '系统就绪', message: 'boom' }],
    })

    const { container } = renderReliabilityPage(props)

    await screen.findByText('部分可靠性数据加载失败')
    expect(screen.getByText('部分可靠性数据加载失败')).toBeInTheDocument()
    expect(screen.getByText('boom')).toBeInTheDocument()
    expect(screen.getByText('运行快照')).toBeInTheDocument()
    expect(screen.getByText('打开 tenant impact dashboard')).toBeInTheDocument()

    await expectNoA11yViolations(container)
  })
})
