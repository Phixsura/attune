import type { AnchorHTMLAttributes, ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { TerminalFailureWorkbenchPanel } from '@/features/feedback/components/terminal-failure-workbench'
import { expectNoA11yViolations } from '@/testing/a11y'
import { renderWithProviders, screen } from '@/testing/test-utils'

vi.mock('@tanstack/react-router', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-router')>('@tanstack/react-router')
  return {
    ...actual,
    Link: ({
      to,
      children,
      ...props
    }: {
      to: string
      children: ReactNode
    } & AnchorHTMLAttributes<HTMLAnchorElement>) => (
      <a href={to} {...props}>
        {children}
      </a>
    ),
  }
})

vi.mock('@/features/session/hooks/use-permissions', () => ({
  usePermissions: () => ({
    can: () => true,
  }),
}))

const { retryMutate } = vi.hoisted(() => ({
  retryMutate: vi.fn(),
}))

vi.mock('@/features/feedback/api/retry-enrichment', () => ({
  useRetryEnrichment: () => ({
    mutate: retryMutate,
    isPending: false,
  }),
}))

const sampleWorkbench = {
  periodStart: '2026-06-01T00:00:00Z',
  periodEnd: '2026-06-30T12:00:00Z',
  totalTerminalFailures: '4',
  oldestCreatedAt: '2026-06-01T00:00:00Z',
  reasonClassClusters: [
    {
      key: 'llm_err',
      label: 'LLM 错误',
      count: '3',
      oldestCreatedAt: '2026-06-01T00:00:00Z',
      newestCreatedAt: '2026-06-01T02:00:00Z',
      sampleFeedbackIds: ['123', '124', '125'],
      remediationHint: 'Check the routed LLM channel and provider health.',
    },
  ],
  modelChannelClusters: [
    {
      key: 'openai::primary',
      label: 'OpenAI / Primary',
      count: '2',
      oldestCreatedAt: '2026-06-03T00:00:00Z',
      newestCreatedAt: '2026-06-04T00:00:00Z',
      sampleFeedbackIds: ['201'],
      remediationHint: 'Check the routed channel mapping and provider pool.',
    },
  ],
  configFingerprintClusters: [
    {
      key: 'sha256:abc123',
      label: 'Default policy snapshot',
      count: '1',
      oldestCreatedAt: '2026-06-05T00:00:00Z',
      newestCreatedAt: '2026-06-05T00:00:00Z',
      sampleFeedbackIds: ['301'],
      remediationHint: 'Compare this fingerprint with the active prompt policy.',
    },
  ],
  ageBucketClusters: [],
}

describe('TerminalFailureWorkbenchPanel', () => {
  it('renders nothing until workbench data is available', () => {
    const { container } = renderWithProviders(
      <TerminalFailureWorkbenchPanel
        data={undefined}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={vi.fn()}
      />,
    )

    expect(container).toBeEmptyDOMElement()
  })

  it('renders an empty state when there are no terminal failures', () => {
    renderWithProviders(
      <TerminalFailureWorkbenchPanel
        data={{ ...sampleWorkbench, totalTerminalFailures: '0' }}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={vi.fn()}
      />,
    )

    expect(screen.getByText('当前窗口没有终态失败')).toBeInTheDocument()
  })

  it('renders retry and open-detail actions for samples', async () => {
    retryMutate.mockReset()
    retryMutate.mockImplementation((_vars, options) => options.onSuccess())
    const onOpenFeedback = vi.fn()
    const { user } = renderWithProviders(
      <TerminalFailureWorkbenchPanel
        data={sampleWorkbench}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={onOpenFeedback}
      />,
    )

    await user.click(screen.getByRole('button', { name: '重试富化 #123' }))

    expect(retryMutate).toHaveBeenCalledTimes(1)
  })

  it('surfaces retry mutation failures without closing the workbench', async () => {
    retryMutate.mockReset()
    retryMutate.mockImplementation((_vars, options) => options.onError())
    const { user } = renderWithProviders(
      <TerminalFailureWorkbenchPanel
        data={sampleWorkbench}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={vi.fn()}
      />,
    )

    await user.click(screen.getByRole('button', { name: '重试富化 #123' }))

    expect(retryMutate).toHaveBeenCalledTimes(1)
    expect(screen.getByText('终态失败聚类工位')).toBeInTheDocument()
  })

  it('renders remediation links for config-related clusters', () => {
    retryMutate.mockReset()
    const onOpenFeedback = vi.fn()
    renderWithProviders(
      <TerminalFailureWorkbenchPanel
        data={sampleWorkbench}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={onOpenFeedback}
      />,
    )

    expect(screen.getByRole('link', { name: '打开 LLM 配置' })).toHaveAttribute(
      'href',
      '/configuration/llm',
    )
    expect(screen.getByRole('link', { name: '打开富化运行时' })).toHaveAttribute(
      'href',
      '/configuration/enrichment-runtime',
    )
  })

  it('renders a global priority recommendation and evidence panel', () => {
    retryMutate.mockReset()
    const onOpenFeedback = vi.fn()
    renderWithProviders(
      <TerminalFailureWorkbenchPanel
        data={sampleWorkbench}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={onOpenFeedback}
      />,
    )

    expect(screen.getByText('建议优先处理')).toBeInTheDocument()
    expect(screen.getByText('来自 原因分类')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '优先处理 #123' })).toBeInTheDocument()
    expect(screen.getAllByText('最早出现').length).toBeGreaterThan(0)
    expect(screen.getAllByText('最近出现').length).toBeGreaterThan(0)
  })

  it('opens the first priority sample from the recommendation card', async () => {
    retryMutate.mockReset()
    const onOpenFeedback = vi.fn()
    const { user } = renderWithProviders(
      <TerminalFailureWorkbenchPanel
        data={sampleWorkbench}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={onOpenFeedback}
      />,
    )

    await user.click(screen.getByRole('button', { name: '优先处理 #123' }))

    expect(onOpenFeedback).toHaveBeenCalledWith('123')
  })

  it('renders in-page jump links for each failure dimension', () => {
    retryMutate.mockReset()
    const onOpenFeedback = vi.fn()
    renderWithProviders(
      <TerminalFailureWorkbenchPanel
        data={sampleWorkbench}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={onOpenFeedback}
      />,
    )

    const jumpLinks = screen
      .getAllByRole('link')
      .filter((element) => element.getAttribute('href')?.startsWith('#terminal-workbench-'))

    expect(jumpLinks).toHaveLength(4)
    expect(jumpLinks.map((element) => element.getAttribute('href'))).toEqual([
      '#terminal-workbench-reason_class',
      '#terminal-workbench-model_channel',
      '#terminal-workbench-config_fingerprint',
      '#terminal-workbench-age_bucket',
    ])
  })

  it('renders the cluster summary and sample drill-down controls', async () => {
    retryMutate.mockReset()
    const onOpenFeedback = vi.fn()
    const { container, user } = renderWithProviders(
      <TerminalFailureWorkbenchPanel
        data={sampleWorkbench}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={onOpenFeedback}
      />,
    )

    expect(screen.getByText('终态失败聚类工位')).toBeInTheDocument()
    expect(screen.getAllByText('LLM 错误').length).toBeGreaterThan(0)
    expect(screen.getAllByText('3 条').length).toBeGreaterThan(0)

    await user.click(screen.getByRole('button', { name: '#123' }))

    expect(onOpenFeedback).toHaveBeenCalledWith('123')
    await expectNoA11yViolations(container)
  })
})
