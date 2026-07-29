import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { type SourceFormData, SourceFormDialog } from '../components/source-form-dialog'

const defaultProps = {
  mode: 'create' as const,
  open: true,
  onOpenChange: vi.fn(),
  pending: false,
  onSubmit: vi.fn(),
}

const sourceFixture = {
  id: 'src-1',
  provider: 'amplitude',
  name: 'My Amplitude',
  authType: 'api_key',
  baseUrl: 'https://custom.amplitude.com',
  enabled: true,
  status: 'active',
  lastSyncAt: new Date('2026-07-28T00:00:00Z'),
  lastError: '',
  webhookUrl: '/v1/cohort-sync/amplitude/t1/src-1/add',
  webhookUrls: [],
  createdAt: new Date('2026-07-28T00:00:00Z'),
  updatedAt: new Date('2026-07-28T00:00:00Z'),
}

describe('SourceFormDialog', () => {
  it('opens in create mode with empty fields', () => {
    renderWithProviders(<SourceFormDialog {...defaultProps} />)

    expect(screen.getByText('添加来源')).toBeInTheDocument()
    expect(screen.getByLabelText('名称')).toHaveValue('')
    expect(screen.getByLabelText(/Webhook 认证密钥/)).toHaveValue('')
    // Submit button shows "新建" in create mode
    expect(screen.getByRole('button', { name: '新建' })).toBeInTheDocument()
  })

  it('opens in edit mode with pre-populated fields', () => {
    renderWithProviders(<SourceFormDialog {...defaultProps} mode="edit" source={sourceFixture} />)

    expect(screen.getByText('编辑来源')).toBeInTheDocument()
    expect(screen.getByLabelText('名称')).toHaveValue('My Amplitude')
    // Submit button shows "保存" in edit mode
    expect(screen.getByRole('button', { name: '保存' })).toBeInTheDocument()
  })

  it('provider select disabled in edit mode', () => {
    renderWithProviders(<SourceFormDialog {...defaultProps} mode="edit" source={sourceFixture} />)

    // The select trigger should be disabled in edit mode
    const providerTrigger = screen.getByRole('combobox')
    expect(providerTrigger).toBeDisabled()
  })

  it('submit button disabled when name empty', () => {
    renderWithProviders(<SourceFormDialog {...defaultProps} />)

    // Name is empty, credential is empty => button should be disabled
    const submitButton = screen.getByRole('button', { name: '新建' })
    expect(submitButton).toBeDisabled()
  })

  it('submit calls onSubmit with correct data', async () => {
    const onSubmit = vi.fn()
    const { user } = renderWithProviders(<SourceFormDialog {...defaultProps} onSubmit={onSubmit} />)

    await user.type(screen.getByLabelText('名称'), 'Test Source')
    await user.type(screen.getByLabelText(/Webhook 认证密钥/), 'my-secret-key')

    const submitButton = screen.getByRole('button', { name: '新建' })
    expect(submitButton).toBeEnabled()
    await user.click(submitButton)

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        provider: 'amplitude',
        name: 'Test Source',
        credential: 'my-secret-key',
      } satisfies Partial<SourceFormData>),
    )
  })

  it('form resets on close and reopen', async () => {
    const onOpenChange = vi.fn()
    const { user, rerender } = renderWithProviders(
      <SourceFormDialog {...defaultProps} onOpenChange={onOpenChange} />,
    )

    // Type a name into the form
    await user.type(screen.getByLabelText('名称'), 'Dirty Name')
    expect(screen.getByLabelText('名称')).toHaveValue('Dirty Name')

    // Simulate close then reopen: re-render with open=false then open=true
    rerender(<SourceFormDialog {...defaultProps} open={false} onOpenChange={onOpenChange} />)
    rerender(<SourceFormDialog {...defaultProps} open={true} onOpenChange={onOpenChange} />)

    // After reopening, the name field should be reset to empty
    await waitFor(() => {
      expect(screen.getByLabelText('名称')).toHaveValue('')
    })
  })
})
