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

  it('renders save-and-leave button when handler provided', () => {
    renderWithProviders(
      <UnsavedChangesDialog
        open={true}
        onConfirmLeave={vi.fn()}
        onCancelLeave={vi.fn()}
        onSaveAndLeave={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: '保存并离开' })).toBeInTheDocument()
  })

  it('calls onSaveAndLeave when save button is clicked', async () => {
    const onSave = vi.fn()
    const { user } = renderWithProviders(
      <UnsavedChangesDialog
        open={true}
        onConfirmLeave={vi.fn()}
        onCancelLeave={vi.fn()}
        onSaveAndLeave={onSave}
      />,
    )
    await user.click(screen.getByRole('button', { name: '保存并离开' }))
    expect(onSave).toHaveBeenCalledOnce()
  })

  it('disables all buttons while saving including save-and-leave', () => {
    renderWithProviders(
      <UnsavedChangesDialog
        open={true}
        onConfirmLeave={vi.fn()}
        onCancelLeave={vi.fn()}
        onSaveAndLeave={vi.fn()}
        saving={true}
      />,
    )
    expect(screen.getByRole('button', { name: '留在此页' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '放弃更改并离开' })).toBeDisabled()
    expect(screen.getByText('保存中…')).toBeInTheDocument()
  })

  it('does not call onCancelLeave when dialog is dismissed via overlay while saving', async () => {
    const onCancel = vi.fn()
    renderWithProviders(
      <UnsavedChangesDialog
        open={true}
        onConfirmLeave={vi.fn()}
        onCancelLeave={onCancel}
        onSaveAndLeave={vi.fn()}
        saving={true}
      />,
    )
    const dialog = screen.getByRole('alertdialog')
    expect(dialog).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '留在此页' })).toBeDisabled()
    expect(onCancel).not.toHaveBeenCalled()
  })

  it('shows change count in body text when provided', () => {
    renderWithProviders(
      <UnsavedChangesDialog
        open={true}
        onConfirmLeave={vi.fn()}
        onCancelLeave={vi.fn()}
        changeCount={3}
      />,
    )
    expect(screen.getByText(/3 处未保存的更改/)).toBeInTheDocument()
  })

  it('falls back to default body when changeCount is 0', () => {
    renderWithProviders(
      <UnsavedChangesDialog
        open={true}
        onConfirmLeave={vi.fn()}
        onCancelLeave={vi.fn()}
        changeCount={0}
      />,
    )
    expect(screen.getByText('你有未保存的更改。离开此页面后更改将丢失。')).toBeInTheDocument()
  })
})
