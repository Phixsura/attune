import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders, screen } from '@/testing/test-utils'
import { UnsavedChangesDialog } from './unsaved-changes-dialog'

describe('UnsavedChangesDialog', () => {
  it('renders nothing when closed', () => {
    renderWithProviders(
      <UnsavedChangesDialog open={false} onConfirmLeave={vi.fn()} onCancelLeave={vi.fn()} />,
    )
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('renders title and body when open', () => {
    renderWithProviders(
      <UnsavedChangesDialog open={true} onConfirmLeave={vi.fn()} onCancelLeave={vi.fn()} />,
    )
    expect(screen.getByText('未保存的更改')).toBeInTheDocument()
    expect(screen.getByText('你有未保存的更改。离开此页面后更改将丢失。')).toBeInTheDocument()
  })

  it('calls onCancelLeave when cancel button is clicked', async () => {
    const onCancel = vi.fn()
    const { user } = renderWithProviders(
      <UnsavedChangesDialog open={true} onConfirmLeave={vi.fn()} onCancelLeave={onCancel} />,
    )
    await user.click(screen.getByRole('button', { name: '留在此页' }))
    expect(onCancel).toHaveBeenCalledOnce()
  })

  it('calls onConfirmLeave when discard button is clicked', async () => {
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <UnsavedChangesDialog open={true} onConfirmLeave={onConfirm} onCancelLeave={vi.fn()} />,
    )
    await user.click(screen.getByRole('button', { name: '放弃更改并离开' }))
    expect(onConfirm).toHaveBeenCalledOnce()
  })
})
