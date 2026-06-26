import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { EvidenceExportDialog } from '@/features/audit-log/components/evidence-export-dialog'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

const emptyFilters = {}

describe('EvidenceExportDialog', () => {
  it('renders the dialog with start button when open', () => {
    renderWithProviders(
      <EvidenceExportDialog filters={emptyFilters} onOpenChange={vi.fn()} open={true} />,
    )
    expect(screen.getByText('合规证据包导出')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '开始导出' })).toBeInTheDocument()
  })

  it('does not render content when closed', () => {
    renderWithProviders(
      <EvidenceExportDialog filters={emptyFilters} onOpenChange={vi.fn()} open={false} />,
    )
    expect(screen.queryByText('合规证据包导出')).not.toBeInTheDocument()
  })

  it('shows completed status with download button', async () => {
    server.use(
      http.post('/fb/v1/console/audit-log/evidence', () =>
        HttpResponse.json({ jobId: 'job-1', status: 'queued', retryAfterSeconds: 1 }),
      ),
      http.get('/fb/v1/console/audit-log/evidence/job-1', () =>
        HttpResponse.json({
          jobId: 'job-1',
          status: 'completed',
          totalEvents: 5,
          createdAt: '2026-06-26T00:00:00Z',
          completedAt: '2026-06-26T00:00:02Z',
          expiresAt: '2026-06-29T00:00:00Z',
          archiveFilename: 'evidence.zip',
          retryAfterSeconds: 2,
        }),
      ),
    )

    const { user } = renderWithProviders(
      <EvidenceExportDialog filters={emptyFilters} onOpenChange={vi.fn()} open={true} />,
    )

    await user.click(screen.getByRole('button', { name: '开始导出' }))

    await waitFor(() => {
      expect(screen.getByText('下载证据包')).toBeInTheDocument()
    })
  })

  it('shows retry button for failed exports', async () => {
    server.use(
      http.post('/fb/v1/console/audit-log/evidence', () =>
        HttpResponse.json({ jobId: 'job-2', status: 'queued', retryAfterSeconds: 1 }),
      ),
      http.get('/fb/v1/console/audit-log/evidence/job-2', () =>
        HttpResponse.json({
          jobId: 'job-2',
          status: 'failed',
          totalEvents: 0,
          createdAt: '2026-06-26T00:00:00Z',
          error: 'internal error',
          retryAfterSeconds: 2,
        }),
      ),
    )

    const { user } = renderWithProviders(
      <EvidenceExportDialog filters={emptyFilters} onOpenChange={vi.fn()} open={true} />,
    )

    await user.click(screen.getByRole('button', { name: '开始导出' }))

    await waitFor(() => {
      expect(screen.getByText(/internal error/)).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
  })
})
