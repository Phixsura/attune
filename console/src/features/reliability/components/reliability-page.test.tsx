import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import {
  ReliabilityPage,
  type ReliabilityPageProps,
} from '@/features/reliability/components/reliability-page'
import { reliabilityCatalog } from '@/features/reliability/reliability-catalog'
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
    expect(screen.getByText('SLO 目录')).toBeInTheDocument()
    expect(screen.getByText('打开 tenant impact dashboard')).toBeInTheDocument()
    expect(screen.getByText('运行快照')).toBeInTheDocument()
    expect(screen.getByText('快速入口')).toBeInTheDocument()
    expect(screen.getByText('1 个活跃 · 1 个非活跃，共 2 个。')).toBeInTheDocument()
    expect(
      screen.getByText(/1 个排队 · 2 个处理中 · 1 个已就绪导出 · 1 个待执行删除。/),
    ).toBeInTheDocument()
    expect(screen.getByText(/1 个可重试 · 1 个处理中。/)).toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(reliabilityCatalog.length)
    expect(screen.getByText(reliabilityCatalog[0].title)).toBeInTheDocument()
    expect(
      within(screen.getAllByRole('listitem')[0]).getByText(
        new RegExp(reliabilityCatalog[0].escalation),
      ),
    ).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: /runbook/i })).toHaveLength(
      reliabilityCatalog.length,
    )
    expect(screen.getAllByText('推荐策略')).toHaveLength(reliabilityCatalog.length)
    expect(screen.getAllByText('预算例外')).toHaveLength(reliabilityCatalog.length)
    expect(
      within(screen.getAllByRole('listitem')[0]).getByText(reliabilityCatalog[0].policySummary),
    ).toBeInTheDocument()
    expect(
      within(screen.getAllByRole('listitem')[0]).getByText(
        reliabilityCatalog[0].budgetExceptionPolicy,
      ),
    ).toBeInTheDocument()
    expect(
      within(screen.getAllByRole('listitem')[0]).getByText(
        reliabilityCatalog[0].budgetExceptionNote,
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^回放报告/ })).toHaveAttribute(
      'href',
      'https://github.com/Phixsura/attune/blob/main/observability/reports/attune-slo-replay-template.md',
    )
    expect(screen.getByRole('link', { name: /^策略参考/ })).toHaveAttribute(
      'href',
      'https://github.com/Phixsura/attune/blob/main/observability/reports/attune-slo-policy-reference.md',
    )
    expect(screen.getByRole('link', { name: /^OpenSLO 导出/ })).toHaveAttribute(
      'href',
      'https://github.com/Phixsura/attune/blob/main/observability/openslo/attune-slo.yaml',
    )
    const replayDownloadLink = screen.getByRole('link', { name: /^下载 replay 工作表/ })
    expect(replayDownloadLink).toHaveAttribute('download', 'attune-slo-replay-template.md')
    const replayDownloadHref = replayDownloadLink.getAttribute('href')
    expect(replayDownloadHref).toContain('data:text/markdown;charset=utf-8,')
    const replayDownloadMarkdown = decodeURIComponent(replayDownloadHref?.split(',', 2)[1] ?? '')
    expect(replayDownloadMarkdown).toContain('Tenant One')
    expect(replayDownloadMarkdown).toContain(
      '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
    )
    expect(replayDownloadMarkdown).toContain(
      '| SLO | Current policy | Replay lens | Budget exception | Historical observation | Verdict | Runbook |',
    )
    expect(screen.getByText('Replay 工作区')).toBeInTheDocument()

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
