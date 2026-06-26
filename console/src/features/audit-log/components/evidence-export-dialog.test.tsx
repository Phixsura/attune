import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { EvidenceExportDialog } from '@/features/audit-log/components/evidence-export-dialog'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('@/features/audit-log/api/evidence-export', async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>
  return {
    ...actual,
    downloadEvidenceExport: vi.fn().mockResolvedValue(undefined),
  }
})

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

  it('shows processing view with queued status', async () => {
    server.use(
      http.post('/fb/v1/console/audit-log/evidence', () =>
        HttpResponse.json({ jobId: 'job-q', status: 'queued', retryAfterSeconds: 1 }),
      ),
      http.get('/fb/v1/console/audit-log/evidence/job-q', () =>
        HttpResponse.json({
          jobId: 'job-q',
          status: 'queued',
          totalEvents: 0,
          createdAt: '2026-06-26T00:00:00Z',
          retryAfterSeconds: 2,
        }),
      ),
    )

    const { user } = renderWithProviders(
      <EvidenceExportDialog filters={emptyFilters} onOpenChange={vi.fn()} open={true} />,
    )

    await user.click(screen.getByRole('button', { name: '开始导出' }))

    await waitFor(() => {
      expect(screen.getByText('排队中')).toBeInTheDocument()
    })
  })

  it('shows completed view without expiry when expiresAt is absent', async () => {
    server.use(
      http.post('/fb/v1/console/audit-log/evidence', () =>
        HttpResponse.json({ jobId: 'job-ne', status: 'queued', retryAfterSeconds: 1 }),
      ),
      http.get('/fb/v1/console/audit-log/evidence/job-ne', () =>
        HttpResponse.json({
          jobId: 'job-ne',
          status: 'completed',
          totalEvents: 3,
          createdAt: '2026-06-26T00:00:00Z',
          completedAt: '2026-06-26T00:00:01Z',
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
    expect(screen.queryByText(/过期/)).not.toBeInTheDocument()
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

  it('triggers download when clicking the download button', async () => {
    const { downloadEvidenceExport } = await import('@/features/audit-log/api/evidence-export')

    server.use(
      http.post('/fb/v1/console/audit-log/evidence', () =>
        HttpResponse.json({ jobId: 'job-dl', status: 'queued', retryAfterSeconds: 1 }),
      ),
      http.get('/fb/v1/console/audit-log/evidence/job-dl', () =>
        HttpResponse.json({
          jobId: 'job-dl',
          status: 'completed',
          totalEvents: 2,
          createdAt: '2026-06-26T00:00:00Z',
          completedAt: '2026-06-26T00:00:01Z',
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

    await user.click(screen.getByRole('button', { name: '下载证据包' }))

    await waitFor(() => {
      expect(downloadEvidenceExport).toHaveBeenCalledWith('job-dl', 'evidence.zip')
    })
  })

  it('retries export after failure', async () => {
    let postCount = 0
    server.use(
      http.post('/fb/v1/console/audit-log/evidence', () => {
        postCount++
        return HttpResponse.json({
          jobId: `job-retry-${postCount}`,
          status: 'queued',
          retryAfterSeconds: 1,
        })
      }),
      http.get('/fb/v1/console/audit-log/evidence/job-retry-1', () =>
        HttpResponse.json({
          jobId: 'job-retry-1',
          status: 'failed',
          totalEvents: 0,
          createdAt: '2026-06-26T00:00:00Z',
          error: 'timeout',
          retryAfterSeconds: 2,
        }),
      ),
      http.get('/fb/v1/console/audit-log/evidence/job-retry-2', () =>
        HttpResponse.json({
          jobId: 'job-retry-2',
          status: 'completed',
          totalEvents: 1,
          createdAt: '2026-06-26T00:00:00Z',
          completedAt: '2026-06-26T00:00:01Z',
          retryAfterSeconds: 2,
        }),
      ),
    )

    const { user } = renderWithProviders(
      <EvidenceExportDialog filters={emptyFilters} onOpenChange={vi.fn()} open={true} />,
    )

    await user.click(screen.getByRole('button', { name: '开始导出' }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => {
      expect(screen.getByText('下载证据包')).toBeInTheDocument()
    })
    expect(postCount).toBe(2)
  })

  it('shows failed view without error message', async () => {
    server.use(
      http.post('/fb/v1/console/audit-log/evidence', () =>
        HttpResponse.json({ jobId: 'job-ne', status: 'queued', retryAfterSeconds: 1 }),
      ),
      http.get('/fb/v1/console/audit-log/evidence/job-ne', () =>
        HttpResponse.json({
          jobId: 'job-ne',
          status: 'failed',
          totalEvents: 0,
          createdAt: '2026-06-26T00:00:00Z',
          retryAfterSeconds: 2,
        }),
      ),
    )

    const { user } = renderWithProviders(
      <EvidenceExportDialog filters={emptyFilters} onOpenChange={vi.fn()} open={true} />,
    )

    await user.click(screen.getByRole('button', { name: '开始导出' }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
    })
  })

  it('shows processing view with running status', async () => {
    server.use(
      http.post('/fb/v1/console/audit-log/evidence', () =>
        HttpResponse.json({ jobId: 'job-run', status: 'queued', retryAfterSeconds: 1 }),
      ),
      http.get('/fb/v1/console/audit-log/evidence/job-run', () =>
        HttpResponse.json({
          jobId: 'job-run',
          status: 'running',
          totalEvents: 0,
          createdAt: '2026-06-26T00:00:00Z',
          retryAfterSeconds: 2,
        }),
      ),
    )

    const { user } = renderWithProviders(
      <EvidenceExportDialog filters={emptyFilters} onOpenChange={vi.fn()} open={true} />,
    )

    await user.click(screen.getByRole('button', { name: '开始导出' }))

    await waitFor(() => {
      expect(screen.getByText('生成中')).toBeInTheDocument()
    })
    expect(screen.getByText('正在打包审计日志，完成后可直接下载。')).toBeInTheDocument()
  })

  it('passes filters to the export request', async () => {
    const capturedBodies: unknown[] = []
    server.use(
      http.post('/fb/v1/console/audit-log/evidence', async ({ request }) => {
        capturedBodies.push(await request.json())
        return HttpResponse.json({ jobId: 'job-f', status: 'queued', retryAfterSeconds: 1 })
      }),
      http.get('/fb/v1/console/audit-log/evidence/job-f', () =>
        HttpResponse.json({
          jobId: 'job-f',
          status: 'queued',
          totalEvents: 0,
          createdAt: '2026-06-26T00:00:00Z',
          retryAfterSeconds: 60,
        }),
      ),
    )

    const filters = { actions: ['member.invite'], targetId: 'member-1' }
    const { user } = renderWithProviders(
      <EvidenceExportDialog filters={filters} onOpenChange={vi.fn()} open={true} />,
    )

    await user.click(screen.getByRole('button', { name: '开始导出' }))

    await waitFor(() => {
      expect(capturedBodies).toHaveLength(1)
    })
    expect(capturedBodies[0]).toMatchObject({
      actions: ['member.invite'],
      targetId: 'member-1',
    })
  })
})
