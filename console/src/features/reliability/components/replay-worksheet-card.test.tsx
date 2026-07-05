import { describe, expect, it, vi } from 'vitest'
import { ReplayWorksheetCard } from '@/features/reliability/components/replay-worksheet-card'
import { expectNoA11yViolations } from '@/testing/a11y'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('ReplayWorksheetCard', () => {
  it('renders a tenant-prefilled worksheet with copy and download actions', async () => {
    const { container, user } = renderWithProviders(
      <ReplayWorksheetCard
        tenantName="Tenant One"
        dashboardHref="/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1"
      />,
    )
    const writeSpy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)

    expect(screen.getByText('Replay 工作区')).toBeInTheDocument()
    expect(screen.getByText('当前 tenant')).toBeInTheDocument()
    expect(screen.getByText('Tenant One')).toBeInTheDocument()
    expect(screen.getByText('Markdown 预览')).toBeInTheDocument()

    const preview = screen.getByLabelText('Replay 工作表 Markdown 预览')
    expect((preview as HTMLTextAreaElement).value).toContain('Tenant One')
    expect((preview as HTMLTextAreaElement).value).toContain('Comparison matrix')

    const downloadLink = screen.getByRole('link', { name: '下载 markdown' })
    expect(downloadLink).toHaveAttribute('download', 'attune-slo-replay-template.md')

    await user.click(screen.getByRole('button', { name: '复制工作表' }))
    await waitFor(() =>
      expect(writeSpy).toHaveBeenCalledWith(expect.stringContaining('Tenant One')),
    )
    expect(screen.getByRole('button', { name: '已复制' })).toBeInTheDocument()

    await expectNoA11yViolations(container)
  })
})
