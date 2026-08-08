import { describe, expect, it, vi } from 'vitest'
import {
  BatchOperatorCommandCenter,
  type OperatorBatchResult,
} from '@/features/feedback/components/batch-operator-command-center'
import { renderWithProviders, screen, within } from '@/testing/test-utils'

describe('BatchOperatorCommandCenter', () => {
  const defaultProps = {
    open: true,
    count: 3,
    selectedFeedbackIds: ['101', '102', '103'],
    dismissStateLabel: 'Done',
    terminalFailureCount: 1,
    latestResult: null,
    isDismissing: false,
    onOpenChange: vi.fn(),
    onLinkRequest: vi.fn(),
    onAssign: vi.fn(),
    onDismiss: vi.fn(),
    onNotify: vi.fn(),
    onRetryTerminalFailures: vi.fn(),
    onFocusFailed: vi.fn(),
    onClearResult: vi.fn(),
  }

  it('exposes link, assign, dismiss, and notify actions', async () => {
    const handlers = {
      onLinkRequest: vi.fn(),
      onAssign: vi.fn(),
      onDismiss: vi.fn(),
      onNotify: vi.fn(),
    }
    const { user } = renderWithProviders(
      <BatchOperatorCommandCenter {...defaultProps} {...handlers} />,
    )

    const dialog = screen.getByRole('dialog', { name: '批量操作指挥面' })
    await user.click(within(dialog).getByRole('button', { name: 'Link 需求' }))
    await user.click(within(dialog).getByRole('button', { name: '批量分派' }))
    await user.click(within(dialog).getByRole('button', { name: '批量关闭' }))
    await user.click(within(dialog).getByRole('button', { name: '批量预览通知' }))

    expect(handlers.onLinkRequest).toHaveBeenCalledTimes(1)
    expect(handlers.onAssign).toHaveBeenCalledTimes(1)
    expect(handlers.onDismiss).toHaveBeenCalledTimes(1)
    expect(handlers.onNotify).toHaveBeenCalledTimes(1)
  })

  it('blocks dismiss when no terminal workflow state is configured', () => {
    renderWithProviders(
      <BatchOperatorCommandCenter {...defaultProps} dismissStateLabel={undefined} />,
    )

    expect(screen.getByRole('button', { name: '批量关闭' })).toBeDisabled()
    expect(screen.getByText('当前租户还没有可用于 dismiss 的终态 workflow state。')).toBeVisible()
  })

  it('shows failure recovery actions for the latest partial result', async () => {
    const latestResult: OperatorBatchResult = {
      action: 'assign',
      total: 3,
      succeeded: 2,
      skipped: 0,
      failed: [{ feedbackId: '102', code: 'OWNER_NOT_FOUND', message: 'owner missing' }],
    }
    const onFocusFailed = vi.fn()
    const onClearResult = vi.fn()
    const onRetryTerminalFailures = vi.fn()
    const { user } = renderWithProviders(
      <BatchOperatorCommandCenter
        {...defaultProps}
        latestResult={latestResult}
        onFocusFailed={onFocusFailed}
        onClearResult={onClearResult}
        onRetryTerminalFailures={onRetryTerminalFailures}
      />,
    )

    expect(screen.getByText('2 条成功 · 0 条跳过 · 1 条失败')).toBeVisible()
    await user.click(screen.getByRole('button', { name: '聚焦失败项' }))
    await user.click(screen.getByRole('button', { name: '清除结果' }))
    await user.click(screen.getByRole('button', { name: '重试富化' }))

    expect(onFocusFailed).toHaveBeenCalledTimes(1)
    expect(onClearResult).toHaveBeenCalledTimes(1)
    expect(onRetryTerminalFailures).toHaveBeenCalledTimes(1)
  })
})
