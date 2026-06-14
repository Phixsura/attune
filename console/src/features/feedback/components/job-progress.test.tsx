import { describe, expect, it, vi } from 'vitest'
import { JobProgress } from '@/features/feedback/components/job-progress'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('JobProgress', () => {
  const defaultProps = {
    jobId: 'job-123',
    status: 'running' as const,
    total: 100,
    progress: 50,
  }

  it('shows progress percentage for running job', () => {
    renderWithProviders(<JobProgress {...defaultProps} />)
    expect(screen.getByText('50 / 100 (50%)')).toBeInTheDocument()
  })

  it('shows progress percentage for queued job', () => {
    renderWithProviders(<JobProgress {...defaultProps} status="queued" progress={0} />)
    expect(screen.getByText('0 / 100 (0%)')).toBeInTheDocument()
  })

  it('shows status label for running', () => {
    renderWithProviders(<JobProgress {...defaultProps} />)
    expect(screen.getByText('处理中')).toBeInTheDocument()
  })

  it('shows status label for completed', () => {
    renderWithProviders(<JobProgress {...defaultProps} status="completed" progress={100} />)
    expect(screen.getByText('已完成')).toBeInTheDocument()
  })

  it('shows status label for failed', () => {
    renderWithProviders(
      <JobProgress {...defaultProps} status="failed" error="Something went wrong" />,
    )
    expect(screen.getByText('失败')).toBeInTheDocument()
  })

  it('shows status label for cancelled', () => {
    renderWithProviders(<JobProgress {...defaultProps} status="cancelled" />)
    expect(screen.getByText('已取消')).toBeInTheDocument()
  })

  it('shows status label for queued', () => {
    renderWithProviders(<JobProgress {...defaultProps} status="queued" progress={0} />)
    expect(screen.getByText('排队中')).toBeInTheDocument()
  })

  it('shows cancel button for running jobs', async () => {
    const onCancel = vi.fn()
    const { user } = renderWithProviders(<JobProgress {...defaultProps} onCancel={onCancel} />)
    const cancelBtn = screen.getByRole('button', { name: /取消/ })
    await user.click(cancelBtn)
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('shows cancel button for queued jobs', () => {
    const onCancel = vi.fn()
    renderWithProviders(
      <JobProgress {...defaultProps} status="queued" progress={0} onCancel={onCancel} />,
    )
    expect(screen.getByRole('button', { name: /取消/ })).toBeInTheDocument()
  })

  it('does not show cancel button when onCancel not provided', () => {
    renderWithProviders(<JobProgress {...defaultProps} />)
    expect(screen.queryByRole('button', { name: /取消/ })).not.toBeInTheDocument()
  })

  it('shows dismiss button for completed jobs', async () => {
    const onDismiss = vi.fn()
    const { user } = renderWithProviders(
      <JobProgress {...defaultProps} status="completed" progress={100} onDismiss={onDismiss} />,
    )
    const dismissBtn = screen.getByRole('button', { name: /关闭/ })
    await user.click(dismissBtn)
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it('shows dismiss button for failed jobs', () => {
    const onDismiss = vi.fn()
    renderWithProviders(<JobProgress {...defaultProps} status="failed" onDismiss={onDismiss} />)
    expect(screen.getByRole('button', { name: /关闭/ })).toBeInTheDocument()
  })

  it('shows dismiss button for cancelled jobs', () => {
    const onDismiss = vi.fn()
    renderWithProviders(<JobProgress {...defaultProps} status="cancelled" onDismiss={onDismiss} />)
    expect(screen.getByRole('button', { name: /关闭/ })).toBeInTheDocument()
  })

  it('shows error message for failed jobs', () => {
    renderWithProviders(
      <JobProgress {...defaultProps} status="failed" error="Connection timeout" />,
    )
    expect(screen.getByText('Connection timeout')).toBeInTheDocument()
  })

  it('does not show error when status is not failed', () => {
    renderWithProviders(<JobProgress {...defaultProps} status="running" error="Some error" />)
    expect(screen.queryByText('Some error')).not.toBeInTheDocument()
  })

  it('handles zero total gracefully', () => {
    renderWithProviders(<JobProgress {...defaultProps} total={0} progress={0} />)
    expect(screen.getByText('0 / 0 (0%)')).toBeInTheDocument()
  })
})
