import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { describe, expect, it, vi } from 'vitest'
import { GDPRPage } from '@/features/gdpr/components/gdpr-page'
import { GdprExportStatus, GdprRequestStatus, GdprRequestType } from '@/proto/attune/v1/gdpr'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

const permissionsMock = vi.hoisted(() => vi.fn())
const triggerBlobDownloadMock = vi.hoisted(() => vi.fn())

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

vi.mock('@/features/session/hooks/use-permissions', () => ({
  usePermissions: permissionsMock,
}))

vi.mock('@/lib/blob-download', () => ({
  triggerBlobDownload: triggerBlobDownloadMock,
}))

const baseOperations = {
  stepUp: {
    satisfied: true,
    passwordAllowed: true,
    method: 'password',
    ttlSeconds: 900,
    verifiedAt: '2026-06-17T10:00:00Z',
    expiresAt: '2026-06-17T10:15:00Z',
  },
  exportTtlSeconds: 7200,
  auditRetentionDays: 30,
  auditPruneIntervalSeconds: 86400,
  queuedRequestCount: 1,
  activeRequestCount: 2,
  readyExportCount: 1,
  nextExportExpiryAt: '2026-06-18T10:00:00Z',
  hashedAuditResidue: true,
  backupsMayRetainUntilRotation: true,
  legalHoldSupported: true,
  deleteGraceWindowSeconds: 1800,
  scheduledDeleteCount: 1,
}

const scheduledDeleteRequest = {
  requestId: 'req-delete-1',
  requestType: GdprRequestType.GDPR_REQUEST_TYPE_DELETE,
  status: GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED,
  subjectKey: 'alice@example.com',
  subjectDisplay: 'Alice',
  createdBy: 'admin-1',
  feedbackCount: 5,
  tagAssignmentCount: 2,
  feedbackAuditCount: 4,
  llmAuditCount: 3,
  createdAt: '2026-06-17T09:00:00Z',
  executeAfter: '2026-06-17T09:30:00Z',
}

const readyExportRequest = {
  requestId: 'job-ready-1',
  requestType: GdprRequestType.GDPR_REQUEST_TYPE_EXPORT,
  status: GdprRequestStatus.GDPR_REQUEST_STATUS_READY,
  subjectKey: 'bob@example.com',
  subjectDisplay: 'Bob',
  createdBy: 'admin-1',
  feedbackCount: 7,
  tagAssignmentCount: 3,
  feedbackAuditCount: 6,
  llmAuditCount: 4,
  createdAt: '2026-06-17T08:00:00Z',
  archiveFilename: 'bob-export.zip',
  expiresAt: '2026-06-18T08:00:00Z',
}

const downloadedExportRequest = {
  requestId: 'job-downloaded-1',
  requestType: GdprRequestType.GDPR_REQUEST_TYPE_EXPORT,
  status: GdprRequestStatus.GDPR_REQUEST_STATUS_DOWNLOADED,
  subjectKey: 'charlie@example.com',
  subjectDisplay: 'Charlie',
  createdBy: 'admin-1',
  feedbackCount: 2,
  tagAssignmentCount: 1,
  feedbackAuditCount: 1,
  llmAuditCount: 1,
  createdAt: '2026-06-16T08:00:00Z',
  archiveFilename: 'charlie-export.zip',
  downloadedAt: '2026-06-16T09:00:00Z',
}

describe('GDPRPage', () => {
  it('runs the end-to-end operator flow for export, delete, revoke, cancel, filtering, and paging', async () => {
    permissionsMock.mockReturnValue({
      can: () => true,
    })

    let exportBody: unknown
    let deleteBody: unknown
    let cancelledRequestId = ''
    let revokedJobId = ''
    const requestTypeQueries: string[] = []
    let loadMoreCursor = ''

    server.use(
      http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(baseOperations)),
      http.get('/fb/v1/console/gdpr/requests', ({ request }) => {
        const url = new URL(request.url)
        const requestType = url.searchParams.get('request_type') ?? ''
        const cursor = url.searchParams.get('cursor') ?? ''
        requestTypeQueries.push(requestType || 'all')
        loadMoreCursor = cursor

        if (requestType === 'delete') {
          return HttpResponse.json({ items: [scheduledDeleteRequest] })
        }
        if (requestType === 'export') {
          return HttpResponse.json({ items: [readyExportRequest, downloadedExportRequest] })
        }
        if (cursor === 'page-2') {
          return HttpResponse.json({ items: [downloadedExportRequest] })
        }
        return HttpResponse.json({
          items: [scheduledDeleteRequest, readyExportRequest],
          nextCursor: 'page-2',
        })
      }),
      http.post('/fb/v1/console/gdpr/requests/:id/cancel', ({ params }) => {
        cancelledRequestId = String(params.id)
        return HttpResponse.json({
          requestId: String(params.id),
          status: GdprRequestStatus.GDPR_REQUEST_STATUS_CANCELLED,
        })
      }),
      http.post('/fb/v1/console/gdpr/exports/:id/revoke', ({ params }) => {
        revokedJobId = String(params.id)
        return HttpResponse.json({
          jobId: String(params.id),
          status: GdprExportStatus.GDPR_EXPORT_STATUS_REVOKED,
          requestStatus: GdprRequestStatus.GDPR_REQUEST_STATUS_REVOKED,
        })
      }),
      http.post('/fb/v1/console/gdpr/export', async ({ request }) => {
        exportBody = await request.json()
        return HttpResponse.json({
          jobId: 'job-new-1',
          status: GdprExportStatus.GDPR_EXPORT_STATUS_QUEUED,
          retryAfterSeconds: 1,
        })
      }),
      http.get('/fb/v1/console/gdpr/exports/:id', ({ params }) =>
        HttpResponse.json({
          jobId: String(params.id),
          subjectKey: 'alice@example.com',
          subjectDisplay: 'Alice',
          status: GdprExportStatus.GDPR_EXPORT_STATUS_COMPLETED,
          retryAfterSeconds: 1,
          downloadPath: `/fb/v1/console/gdpr/exports/${String(params.id)}/download`,
          archiveFilename: 'alice-export.zip',
          feedbackCount: 5,
          tagAssignmentCount: 2,
          feedbackAuditCount: 4,
          llmAuditCount: 3,
          createdAt: '2026-06-17T10:00:00Z',
          completedAt: '2026-06-17T10:01:00Z',
        }),
      ),
      http.get(
        '/fb/v1/console/gdpr/exports/:id/download',
        () =>
          new HttpResponse(new Uint8Array([1, 2, 3]), {
            status: 200,
            headers: {
              'Content-Disposition': 'attachment; filename="alice-export.zip"',
              'Content-Type': 'application/zip',
            },
          }),
      ),
      http.post('/fb/v1/console/gdpr/delete', async ({ request }) => {
        deleteBody = await request.json()
        return HttpResponse.json({
          requestId: 'req-delete-new',
          status: GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED,
          executeAfter: '2026-06-17T11:00:00Z',
          subjectKey: 'alice@example.com',
          feedbackCount: 5,
          tagAssignmentCount: 2,
          feedbackAuditCount: 4,
          llmAuditCount: 3,
        })
      }),
    )

    const { user } = renderWithProviders(<GDPRPage />)

    await waitFor(() => {
      expect(screen.getByText('GDPR 数据请求')).toBeInTheDocument()
    })

    expect(screen.getByText('请求历史与保留策略')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByTestId('gdpr-request-row-req-delete-1')).toBeInTheDocument()
      expect(screen.getByTestId('gdpr-request-row-job-ready-1')).toBeInTheDocument()
    })
    expect(screen.getByText('30 天')).toBeInTheDocument()

    await user.click(screen.getByTestId('gdpr-cancel-request-req-delete-1'))
    await waitFor(() => {
      expect(cancelledRequestId).toBe('req-delete-1')
    })

    await user.click(screen.getByTestId('gdpr-revoke-export-job-ready-1'))
    await waitFor(() => {
      expect(revokedJobId).toBe('job-ready-1')
    })

    await user.click(screen.getByRole('button', { name: '加载更多' }))
    await waitFor(() => {
      expect(screen.getByTestId('gdpr-request-row-job-downloaded-1')).toBeInTheDocument()
    })
    expect(loadMoreCursor).toBe('page-2')

    await user.click(screen.getByRole('button', { name: '导出' }))
    await waitFor(() => {
      expect(screen.queryByTestId('gdpr-request-row-req-delete-1')).not.toBeInTheDocument()
      expect(screen.getByTestId('gdpr-request-row-job-ready-1')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '全部' }))
    await waitFor(() => {
      expect(screen.getByTestId('gdpr-request-row-req-delete-1')).toBeInTheDocument()
    })

    await user.type(screen.getByTestId('gdpr-subject-key'), 'alice@example.com')
    await user.click(screen.getByRole('button', { name: '导出 ZIP' }))

    await waitFor(() => {
      expect(exportBody).toEqual({ subjectKey: 'alice@example.com' })
      expect(triggerBlobDownloadMock).toHaveBeenCalled()
    })

    expect(screen.getByText(/alice-export\.zip/)).toBeInTheDocument()
    expect(screen.getAllByText(/5\/2\/4\/3/)).toHaveLength(2)

    await user.type(screen.getByTestId('gdpr-confirm-subject-key'), 'alice@example.com')
    await user.click(screen.getByTestId('gdpr-delete-submit'))

    await waitFor(() => {
      expect(deleteBody).toEqual({ subjectKey: 'alice@example.com' })
    })

    expect(requestTypeQueries).toContain('all')
    expect(requestTypeQueries).toContain('export')
  })

  it('requires step-up verification before sensitive actions and unlocks after password verification', async () => {
    permissionsMock.mockReturnValue({
      can: () => true,
    })

    let stepUpSatisfied = false
    let verifyBody: unknown
    let exportBody: unknown

    server.use(
      http.get('/fb/v1/console/gdpr/operations', () =>
        HttpResponse.json({
          ...baseOperations,
          stepUp: {
            ...baseOperations.stepUp,
            satisfied: stepUpSatisfied,
            expiresAt: stepUpSatisfied ? '2026-06-17T10:15:00Z' : undefined,
          },
        }),
      ),
      http.get('/fb/v1/console/gdpr/requests', () => HttpResponse.json({ items: [] })),
      http.post('/fb/v1/console/gdpr/step-up/verify', async ({ request }) => {
        verifyBody = await request.json()
        stepUpSatisfied = true
        return HttpResponse.json({
          stepUp: {
            ...baseOperations.stepUp,
            satisfied: true,
          },
        })
      }),
      http.post('/fb/v1/console/gdpr/export', async ({ request }) => {
        exportBody = await request.json()
        return HttpResponse.json({
          jobId: 'job-step-up-1',
          status: GdprExportStatus.GDPR_EXPORT_STATUS_QUEUED,
          retryAfterSeconds: 1,
        })
      }),
      http.get('/fb/v1/console/gdpr/exports/:id', ({ params }) =>
        HttpResponse.json({
          jobId: String(params.id),
          subjectKey: 'protected@example.com',
          subjectDisplay: 'Protected User',
          status: GdprExportStatus.GDPR_EXPORT_STATUS_FAILED,
          retryAfterSeconds: 1,
          error: 'archive backend unavailable',
          feedbackCount: 0,
          tagAssignmentCount: 0,
          feedbackAuditCount: 0,
          llmAuditCount: 0,
          createdAt: '2026-06-17T10:00:00Z',
        }),
      ),
    )

    const { user } = renderWithProviders(<GDPRPage />)

    await waitFor(() => {
      expect(screen.getByText('GDPR 数据请求')).toBeInTheDocument()
    })

    await user.type(screen.getByTestId('gdpr-subject-key'), 'protected@example.com')
    await user.click(screen.getByRole('button', { name: '导出 ZIP' }))

    const dialog = await screen.findByRole('dialog', { name: '确认敏感操作' })
    await user.type(within(dialog).getByLabelText('当前密码'), 'correct horse battery staple')
    await user.click(within(dialog).getByRole('button', { name: '验证并继续' }))

    await waitFor(() => {
      expect(verifyBody).toEqual({ password: 'correct horse battery staple' })
    })

    await user.click(screen.getByRole('button', { name: '导出 ZIP' }))
    await waitFor(() => {
      expect(exportBody).toEqual({ subjectKey: 'protected@example.com' })
    })

    expect(screen.getByText('archive backend unavailable')).toBeInTheDocument()
    expect(screen.getByText('还没有 GDPR 请求记录。')).toBeInTheDocument()
  })

  it('shows the permission empty state when the operator cannot view GDPR controls', async () => {
    permissionsMock.mockReturnValue({
      can: (permission: string) => permission !== 'settings:gdpr:view',
    })

    server.use(
      http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(baseOperations)),
      http.get('/fb/v1/console/gdpr/requests', () => HttpResponse.json({ items: [] })),
    )

    renderWithProviders(<GDPRPage />)

    expect(screen.getByText('无权限访问 GDPR 页面')).toBeInTheDocument()
    expect(screen.getByText('当前账号没有查看或执行数据主体导出/删除的权限。')).toBeInTheDocument()
  })

  it('shows validation toast for missing subject key and disables delete until confirmation matches', async () => {
    permissionsMock.mockReturnValue({
      can: () => true,
    })

    server.use(
      http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(baseOperations)),
      http.get('/fb/v1/console/gdpr/requests', () => HttpResponse.json({ items: [] })),
    )

    const { user } = renderWithProviders(<GDPRPage />)

    await waitFor(() => {
      expect(screen.getByText('GDPR 数据请求')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '导出 ZIP' }))
    expect(toast.error).toHaveBeenCalledWith('请输入 subject key')

    await user.type(screen.getByTestId('gdpr-subject-key'), 'alice@example.com')
    await user.type(screen.getByTestId('gdpr-confirm-subject-key'), 'alice+wrong@example.com')
    expect(screen.getByTestId('gdpr-delete-submit')).toBeDisabled()
  })
})
