import { describe, expect, it, vi } from 'vitest'
import { SecretRevealDialog } from '@/features/inbound-sources/components/secret-reveal-dialog'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('SecretRevealDialog', () => {
  it('renders optional values and copies each field', async () => {
    const { user } = renderWithProviders(
      <SecretRevealDialog
        open
        onClose={vi.fn()}
        url="https://attune.example.test/hooks/source-1"
        secretHex="abc123"
        curlExample="curl -H 'X-Attune-Signature: sig' https://attune.example.test/hooks/source-1"
      />,
    )
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)

    expect(screen.getByText('请妥善保存这个 secret')).toBeInTheDocument()
    expect(screen.getByText('https://attune.example.test/hooks/source-1')).toBeInTheDocument()
    expect(screen.getByText('abc123')).toBeInTheDocument()
    expect(screen.getByText(/curl -H/)).toBeInTheDocument()

    const copyButtons = screen.getAllByRole('button', { name: /复制/ })
    await user.click(copyButtons[0])
    await waitFor(() =>
      expect(writeText).toHaveBeenLastCalledWith('https://attune.example.test/hooks/source-1'),
    )
    expect(screen.getByRole('button', { name: /已复制/ })).toBeInTheDocument()

    await user.click(copyButtons[1])
    await waitFor(() => expect(writeText).toHaveBeenLastCalledWith('abc123'))

    await user.click(copyButtons[2])
    await waitFor(() =>
      expect(writeText).toHaveBeenLastCalledWith(
        "curl -H 'X-Attune-Signature: sig' https://attune.example.test/hooks/source-1",
      ),
    )
  })

  it('omits optional blocks and tolerates clipboard failures', async () => {
    const { user } = renderWithProviders(
      <SecretRevealDialog open onClose={vi.fn()} secretHex="def456" />,
    )
    const writeText = vi
      .spyOn(navigator.clipboard, 'writeText')
      .mockRejectedValue(new Error('denied'))

    expect(screen.queryByText('Webhook URL')).not.toBeInTheDocument()
    expect(screen.queryByText('示例 curl 调用')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /复制/ }))

    expect(writeText).toHaveBeenCalledWith('def456')
    expect(screen.queryByRole('button', { name: /已复制/ })).not.toBeInTheDocument()
  })

  it('closes when acknowledged', async () => {
    const onClose = vi.fn()
    const { user } = renderWithProviders(
      <SecretRevealDialog open onClose={onClose} secretHex="def456" />,
    )

    await user.click(screen.getByRole('button', { name: '我已妥善保存' }))

    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('does not mount content while closed', () => {
    renderWithProviders(<SecretRevealDialog open={false} onClose={vi.fn()} secretHex="def456" />)

    expect(screen.queryByText('请妥善保存这个 secret')).not.toBeInTheDocument()
  })
})
