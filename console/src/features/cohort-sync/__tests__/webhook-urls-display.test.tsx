import { describe, expect, it } from 'vitest'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { WebhookUrlsDisplay } from '../components/webhook-urls-display'

const amplitudeUrls = [
  'https://attune.example.com/v1/cohort-sync/amplitude/t1/src-1/create',
  'https://attune.example.com/v1/cohort-sync/amplitude/t1/src-1/add',
  'https://attune.example.com/v1/cohort-sync/amplitude/t1/src-1/remove',
]

describe('WebhookUrlsDisplay', () => {
  it("renders '—' when urls is empty", () => {
    renderWithProviders(<WebhookUrlsDisplay urls={[]} provider="amplitude" />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('compact mode shows only first URL with copy button', () => {
    renderWithProviders(<WebhookUrlsDisplay urls={amplitudeUrls} provider="amplitude" compact />)

    // Only the first URL should be rendered
    expect(screen.getByText(amplitudeUrls[0])).toBeInTheDocument()
    expect(screen.queryByText(amplitudeUrls[1])).not.toBeInTheDocument()
    expect(screen.queryByText(amplitudeUrls[2])).not.toBeInTheDocument()

    // One copy button visible
    expect(screen.getByRole('button', { name: /复制/ })).toBeInTheDocument()
  })

  it('expanded mode shows all 3 Amplitude URLs with labels', () => {
    renderWithProviders(<WebhookUrlsDisplay urls={amplitudeUrls} provider="amplitude" />)

    // All three URLs rendered
    expect(screen.getByText(amplitudeUrls[0])).toBeInTheDocument()
    expect(screen.getByText(amplitudeUrls[1])).toBeInTheDocument()
    expect(screen.getByText(amplitudeUrls[2])).toBeInTheDocument()

    // Amplitude-specific labels
    expect(screen.getByText('创建地址')).toBeInTheDocument()
    expect(screen.getByText('添加地址')).toBeInTheDocument()
    expect(screen.getByText('移除地址')).toBeInTheDocument()

    // Three copy buttons
    const copyButtons = screen.getAllByRole('button', { name: /复制/ })
    expect(copyButtons).toHaveLength(3)
  })

  it("copy button changes to '已复制' on click", async () => {
    // userEvent.setup() provides its own clipboard mock, so we rely on
    // that for writeText to resolve successfully.
    const { user } = renderWithProviders(
      <WebhookUrlsDisplay urls={amplitudeUrls} provider="amplitude" compact />,
    )

    const copyButton = screen.getByRole('button', { name: /复制/ })
    await user.click(copyButton)

    // After click, the button text should change to "已复制"
    await waitFor(() => {
      expect(screen.getByText('已复制')).toBeInTheDocument()
    })
    // The original "复制" label should no longer be visible
    expect(screen.queryByText('复制')).not.toBeInTheDocument()
  })
})
