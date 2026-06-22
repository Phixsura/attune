import { describe, expect, it, vi } from 'vitest'
import { BatchResult } from '@/features/feedback/components/batch-result'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('BatchResult', () => {
  it('shows full success state', () => {
    renderWithProviders(<BatchResult totalMatched={10} succeeded={10} skipped={0} failed={[]} />)
    expect(screen.getByText('批量操作成功')).toBeInTheDocument()
    expect(screen.getByText('成功 10 条')).toBeInTheDocument()
  })

  it('shows partial success state', () => {
    renderWithProviders(
      <BatchResult
        totalMatched={10}
        succeeded={7}
        skipped={2}
        failed={[{ feedbackId: 'f-1', code: 'ERROR', message: 'Failed' }]}
      />,
    )
    expect(screen.getByText('批量操作部分成功')).toBeInTheDocument()
    expect(screen.getByText('成功 7 条')).toBeInTheDocument()
    expect(screen.getByText('跳过 2 条')).toBeInTheDocument()
    expect(screen.getByText('失败 1 条')).toBeInTheDocument()
  })

  it('shows full failure state', () => {
    renderWithProviders(
      <BatchResult
        totalMatched={3}
        succeeded={0}
        skipped={0}
        failed={[
          { feedbackId: 'f-1', code: 'ERROR', message: 'Failed 1' },
          { feedbackId: 'f-2', code: 'ERROR', message: 'Failed 2' },
        ]}
      />,
    )
    expect(screen.getByText('批量操作失败')).toBeInTheDocument()
  })

  it('expands to show failure details', async () => {
    const { user } = renderWithProviders(
      <BatchResult
        totalMatched={2}
        succeeded={0}
        skipped={0}
        failed={[
          { feedbackId: 'feedback-id-1', code: 'ERROR', message: 'Error message 1' },
          { feedbackId: 'feedback-id-2', code: 'ERROR', message: 'Error message 2' },
        ]}
      />,
    )

    const expandBtn = screen.getByText('查看失败详情')
    await user.click(expandBtn)

    expect(screen.getByText('Error message 1')).toBeInTheDocument()
    expect(screen.getByText('Error message 2')).toBeInTheDocument()
  })

  it('collapses failure details when clicked again', async () => {
    const { user } = renderWithProviders(
      <BatchResult
        totalMatched={1}
        succeeded={0}
        skipped={0}
        failed={[{ feedbackId: 'f-1', code: 'ERROR', message: 'Error msg' }]}
      />,
    )

    const expandBtn = screen.getByText('查看失败详情')
    await user.click(expandBtn)
    expect(screen.getByText('Error msg')).toBeInTheDocument()

    await user.click(expandBtn)
    expect(screen.queryByText('Error msg')).not.toBeInTheDocument()
  })

  it('shows "more failures" when more than 10 failures', async () => {
    const failures = Array.from({ length: 15 }, (_, i) => ({
      feedbackId: `f-${i}`,
      code: 'ERROR',
      message: `Error ${i}`,
    }))

    const { user } = renderWithProviders(
      <BatchResult totalMatched={15} succeeded={0} skipped={0} failed={failures} />,
    )

    await user.click(screen.getByText('查看失败详情'))
    expect(screen.getByText(/还有 5 条失败/)).toBeInTheDocument()
  })

  it('calls onDismiss when dismiss button clicked', async () => {
    const onDismiss = vi.fn()
    const { user } = renderWithProviders(
      <BatchResult totalMatched={5} succeeded={5} skipped={0} failed={[]} onDismiss={onDismiss} />,
    )

    await user.click(screen.getByRole('button', { name: /关闭/ }))
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it('calls onRetry when retry button clicked', async () => {
    const onRetry = vi.fn()
    const { user } = renderWithProviders(
      <BatchResult
        totalMatched={2}
        succeeded={1}
        skipped={0}
        failed={[{ feedbackId: 'f-1', code: 'ERROR', message: 'Failed' }]}
        onRetry={onRetry}
      />,
    )

    await user.click(screen.getByRole('button', { name: /重试/ }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('does not show retry button when no failures', () => {
    const onRetry = vi.fn()
    renderWithProviders(
      <BatchResult totalMatched={5} succeeded={5} skipped={0} failed={[]} onRetry={onRetry} />,
    )

    expect(screen.queryByRole('button', { name: /重试/ })).not.toBeInTheDocument()
  })

  it('does not show skipped badge when skipped is 0', () => {
    renderWithProviders(<BatchResult totalMatched={5} succeeded={5} skipped={0} failed={[]} />)

    expect(screen.queryByText(/跳过/)).not.toBeInTheDocument()
  })
})
