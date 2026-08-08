import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { GDPRPage, gdprPageTestables } from '@/features/gdpr/components/gdpr-page'
import { GdprExportStatus, GdprRequestStatus, GdprRequestType } from '@/proto/attune/v1/gdpr'
import { expectNoA11yViolations } from '@/testing/a11y'
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

afterEach(() => {
  vi.clearAllMocks()
})

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
  outboxCount: 1,
  surveyInvitationCount: 2,
  surveyResponseCount: 1,
  surveyLowScoreReviewCount: 1,
  surveyProviderEventCount: 1,
  surveyRecoveryNotificationCount: 0,
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
  outboxCount: 0,
  surveyInvitationCount: 2,
  surveyResponseCount: 2,
  surveyLowScoreReviewCount: 0,
  surveyProviderEventCount: 1,
  surveyRecoveryNotificationCount: 0,
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
  outboxCount: 0,
  surveyInvitationCount: 0,
  surveyResponseCount: 0,
  surveyLowScoreReviewCount: 0,
  surveyProviderEventCount: 0,
  surveyRecoveryNotificationCount: 0,
  createdAt: '2026-06-16T08:00:00Z',
  archiveFilename: 'charlie-export.zip',
  downloadedAt: '2026-06-16T09:00:00Z',
}

const newReadyExportRequest = {
  requestId: 'job-new-1',
  requestType: GdprRequestType.GDPR_REQUEST_TYPE_EXPORT,
  status: GdprRequestStatus.GDPR_REQUEST_STATUS_READY,
  subjectKey: 'alice@example.com',
  subjectDisplay: 'Alice',
  createdBy: 'admin-1',
  feedbackCount: 5,
  tagAssignmentCount: 2,
  feedbackAuditCount: 4,
  llmAuditCount: 3,
  outboxCount: 0,
  surveyInvitationCount: 2,
  surveyResponseCount: 1,
  surveyLowScoreReviewCount: 1,
  surveyProviderEventCount: 1,
  surveyRecoveryNotificationCount: 0,
  createdAt: '2026-06-17T10:00:00Z',
  archiveFilename: 'alice-export.zip',
  expiresAt: '2026-06-18T10:00:00Z',
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
    let newExportReady = false

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
          return HttpResponse.json({
            items: newExportReady
              ? [newReadyExportRequest, readyExportRequest, downloadedExportRequest]
              : [readyExportRequest, downloadedExportRequest],
          })
        }
        if (cursor === 'page-2') {
          return HttpResponse.json({ items: [downloadedExportRequest] })
        }
        return HttpResponse.json({
          items: newExportReady
            ? [newReadyExportRequest, scheduledDeleteRequest, readyExportRequest]
            : [scheduledDeleteRequest, readyExportRequest],
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
        (() => {
          if (String(params.id) === 'job-new-1') {
            newExportReady = true
          }
          return HttpResponse.json({
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
            surveyInvitationCount: 2,
            surveyResponseCount: 1,
            surveyLowScoreReviewCount: 1,
            surveyProviderEventCount: 1,
            surveyRecoveryNotificationCount: 0,
            createdAt: '2026-06-17T10:00:00Z',
            completedAt: '2026-06-17T10:01:00Z',
          })
        })(),
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
          outboxCount: 1,
          surveyInvitationCount: 2,
          surveyResponseCount: 1,
          surveyLowScoreReviewCount: 1,
          surveyProviderEventCount: 1,
          surveyRecoveryNotificationCount: 0,
        })
      }),
    )

    const { container, user } = renderWithProviders(<GDPRPage />)

    await waitFor(() => {
      expect(screen.getByText('GDPR 数据请求')).toBeInTheDocument()
    })

    expect(screen.getAllByText('待执行删除').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('敏感操作保护')).toBeInTheDocument()
    expect(screen.getByText('请求历史与保留策略')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByTestId('gdpr-request-row-req-delete-1')).toBeInTheDocument()
      expect(screen.getByTestId('gdpr-request-row-job-ready-1')).toBeInTheDocument()
    })
    const retentionWorkflow = screen.getByTestId('gdpr-retention-legal-hold-workflow')
    expect(within(retentionWorkflow).getByText('Retention / legal hold 工作流')).toBeInTheDocument()
    expect(
      within(retentionWorkflow).getByText(
        '30d audit / 30m delete grace / legal hold on / 2 request records',
      ),
    ).toBeInTheDocument()
    expect(
      within(retentionWorkflow).getByText('3 retention and legal-hold checks need attention'),
    ).toBeInTheDocument()
    expect(
      within(retentionWorkflow).getByText('audit 30d / export 2h / prune 1d'),
    ).toBeInTheDocument()
    expect(
      within(retentionWorkflow).getByText('legal hold on / 1 scheduled deletes'),
    ).toBeInTheDocument()
    expect(
      within(retentionWorkflow).getByText('grace 30m / 1 scheduled deletes / 1 visible'),
    ).toBeInTheDocument()
    expect(
      within(retentionWorkflow).getByText(
        '1 ready exports / expires 2026-06-18T10:00:00Z / TTL 2h',
      ),
    ).toBeInTheDocument()
    expect(
      within(retentionWorkflow).getByText('hashed audit on / backup residue on / audit 30d'),
    ).toBeInTheDocument()
    expect(screen.getByText('30 天')).toBeInTheDocument()
    await expectNoA11yViolations(container)

    await user.click(screen.getByTestId('gdpr-download-export-job-ready-1'))
    await waitFor(() => {
      expect(triggerBlobDownloadMock).toHaveBeenCalledTimes(1)
    })

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
    expect(screen.getByRole('button', { name: '导出' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '全部' })).toHaveAttribute('aria-pressed', 'false')

    await user.click(screen.getByRole('button', { name: '全部' }))
    await waitFor(() => {
      expect(screen.getByTestId('gdpr-request-row-req-delete-1')).toBeInTheDocument()
    })

    await user.type(screen.getByTestId('gdpr-subject-key'), 'alice@example.com')
    await user.click(screen.getByRole('button', { name: '导出 ZIP' }))

    await waitFor(() => {
      expect(exportBody).toEqual({ subjectKey: 'alice@example.com' })
    })
    await waitFor(() => {
      expect(screen.getByTestId('gdpr-download-current-export')).toBeInTheDocument()
    })
    expect(screen.getByText(/Survey: 5\/2\/4\/3\/5/)).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByTestId('gdpr-request-row-job-new-1')).toBeInTheDocument()
      expect(screen.getByTestId('gdpr-download-export-job-new-1')).toBeInTheDocument()
    })
    expect(triggerBlobDownloadMock).toHaveBeenCalledTimes(1)

    await user.click(screen.getByTestId('gdpr-download-current-export'))
    await waitFor(() => {
      expect(triggerBlobDownloadMock).toHaveBeenCalledTimes(2)
    })

    await user.click(screen.getByTestId('gdpr-revoke-current-export'))
    await waitFor(() => {
      expect(revokedJobId).toBe('job-new-1')
    })

    expect(screen.getAllByText(/alice-export\.zip/).length).toBeGreaterThanOrEqual(2)
    expect(
      within(screen.getByTestId('gdpr-request-row-req-delete-1')).getByText('5/2/4/3/1/5'),
    ).toBeInTheDocument()
    expect(
      within(screen.getByTestId('gdpr-request-row-job-new-1')).getByText('5/2/4/3/0/5'),
    ).toBeInTheDocument()

    await user.type(screen.getByTestId('gdpr-confirm-subject-key'), 'alice@example.com')
    await user.click(screen.getByTestId('gdpr-delete-submit'))

    await waitFor(() => {
      expect(deleteBody).toEqual({ subjectKey: 'alice@example.com' })
    })
    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith(expect.stringContaining('Outbox'))
    })
    expect(toast.success).toHaveBeenCalledWith(expect.stringContaining('Survey'))

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
          surveyInvitationCount: 0,
          surveyResponseCount: 0,
          surveyLowScoreReviewCount: 0,
          surveyProviderEventCount: 0,
          surveyRecoveryNotificationCount: 0,
          createdAt: '2026-06-17T10:00:00Z',
        }),
      ),
    )

    const { user } = renderWithProviders(<GDPRPage />)

    await waitFor(() => {
      expect(screen.getByText('GDPR 数据请求')).toBeInTheDocument()
    })

    await user.type(screen.getByTestId('gdpr-subject-key'), 'protected@example.com')
    const exportButton = screen.getByRole('button', { name: '导出 ZIP' })
    exportButton.focus()
    await user.keyboard('[Enter]')

    const dialog = await screen.findByRole('dialog', { name: '确认敏感操作' })
    expect(within(dialog).getByLabelText('当前密码')).toHaveFocus()
    await expectNoA11yViolations(document.body)
    await user.keyboard('[Escape]')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(exportButton).toHaveFocus()

    await user.click(exportButton)
    const reopenedDialog = await screen.findByRole('dialog', { name: '确认敏感操作' })
    expect(within(reopenedDialog).getByLabelText('当前密码')).toHaveFocus()
    await user.type(
      within(reopenedDialog).getByLabelText('当前密码'),
      'correct horse battery staple',
    )
    await user.click(within(reopenedDialog).getByRole('button', { name: '验证并继续' }))

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

  it('surfaces mutation, download, verification, cancel, and revoke failures', async () => {
    permissionsMock.mockReturnValue({
      can: () => true,
    })

    let operationsReads = 0

    server.use(
      http.get('/fb/v1/console/gdpr/operations', () => {
        operationsReads += 1
        return HttpResponse.json(baseOperations)
      }),
      http.get('/fb/v1/console/gdpr/requests', () =>
        HttpResponse.json({ items: [scheduledDeleteRequest, readyExportRequest] }),
      ),
      http.post('/fb/v1/console/gdpr/export', () =>
        HttpResponse.json({ message: 'export denied' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/gdpr/delete', () =>
        HttpResponse.json({ message: 'delete denied' }, { status: 500 }),
      ),
      http.get('/fb/v1/console/gdpr/exports/:id/download', () =>
        HttpResponse.json({ message: 'download denied' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/gdpr/step-up/verify', () =>
        HttpResponse.json({ message: 'step-up denied' }, { status: 401 }),
      ),
      http.post('/fb/v1/console/gdpr/requests/:id/cancel', () =>
        HttpResponse.json({ message: 'cancel denied' }, { status: 409 }),
      ),
      http.post('/fb/v1/console/gdpr/exports/:id/revoke', () =>
        HttpResponse.json({ message: 'revoke denied' }, { status: 409 }),
      ),
    )

    const { user } = renderWithProviders(<GDPRPage />)

    await waitFor(() => {
      expect(screen.getByText('GDPR 数据请求')).toBeInTheDocument()
    })

    const initialOperationsReads = operationsReads
    await user.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(operationsReads).toBeGreaterThan(initialOperationsReads))

    await user.type(screen.getByTestId('gdpr-subject-key'), 'alice@example.com')
    await user.click(screen.getByRole('button', { name: '导出 ZIP' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('export denied'))

    await user.type(screen.getByTestId('gdpr-confirm-subject-key'), 'alice@example.com')
    await user.click(screen.getByTestId('gdpr-delete-submit'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('delete denied'))

    await user.click(screen.getByTestId('gdpr-download-export-job-ready-1'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('download denied'))

    await user.click(screen.getByRole('button', { name: '二次验证已通过' }))
    const dialog = await screen.findByRole('dialog', { name: '确认敏感操作' })
    await user.type(within(dialog).getByLabelText('当前密码'), 'wrong password')
    await user.click(within(dialog).getByRole('button', { name: '验证并继续' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('step-up denied'))
    await user.click(within(dialog).getByRole('button', { name: '取消' }))

    await user.click(screen.getByTestId('gdpr-cancel-request-req-delete-1'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cancel denied'))

    await user.click(screen.getByTestId('gdpr-revoke-export-job-ready-1'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('revoke denied'))
  })

  it('requires step-up before cancelling deletes or revoking ready exports', async () => {
    permissionsMock.mockReturnValue({
      can: () => true,
    })

    server.use(
      http.get('/fb/v1/console/gdpr/operations', () =>
        HttpResponse.json({
          ...baseOperations,
          stepUp: {
            ...baseOperations.stepUp,
            satisfied: false,
            expiresAt: undefined,
          },
        }),
      ),
      http.get('/fb/v1/console/gdpr/requests', () =>
        HttpResponse.json({ items: [scheduledDeleteRequest, readyExportRequest] }),
      ),
    )

    const { user } = renderWithProviders(<GDPRPage />)

    await waitFor(() => {
      expect(screen.getByText('GDPR 数据请求')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.getByTestId('gdpr-cancel-request-req-delete-1')).toBeInTheDocument()
      expect(screen.getByTestId('gdpr-revoke-export-job-ready-1')).toBeInTheDocument()
    })

    await user.click(screen.getByTestId('gdpr-cancel-request-req-delete-1'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('请先完成二次验证'))
    await user.click(
      within(screen.getByRole('dialog', { name: '确认敏感操作' })).getByRole('button', {
        name: '取消',
      }),
    )

    await user.click(screen.getByTestId('gdpr-revoke-export-job-ready-1'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('请先完成二次验证'))
  })

  it('requires step-up before scheduling a delete request', async () => {
    permissionsMock.mockReturnValue({
      can: () => true,
    })

    server.use(
      http.get('/fb/v1/console/gdpr/operations', () =>
        HttpResponse.json({
          ...baseOperations,
          stepUp: {
            ...baseOperations.stepUp,
            satisfied: false,
            expiresAt: undefined,
          },
        }),
      ),
      http.get('/fb/v1/console/gdpr/requests', () => HttpResponse.json({ items: [] })),
    )

    const { user } = renderWithProviders(<GDPRPage />)

    await waitFor(() => {
      expect(screen.getByText('GDPR 数据请求')).toBeInTheDocument()
    })
    await user.type(screen.getByTestId('gdpr-subject-key'), 'alice@example.com')
    await user.type(screen.getByTestId('gdpr-confirm-subject-key'), 'alice@example.com')
    await user.click(screen.getByTestId('gdpr-delete-submit'))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('请先完成二次验证'))
    expect(screen.getByRole('dialog', { name: '确认敏感操作' })).toBeInTheDocument()
  })

  it('reports expired export terminal state', async () => {
    permissionsMock.mockReturnValue({
      can: () => true,
    })

    server.use(
      http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(baseOperations)),
      http.get('/fb/v1/console/gdpr/requests', () => HttpResponse.json({ items: [] })),
      http.post('/fb/v1/console/gdpr/export', () =>
        HttpResponse.json({
          jobId: 'job-expired-1',
          status: GdprExportStatus.GDPR_EXPORT_STATUS_QUEUED,
          retryAfterSeconds: 1,
        }),
      ),
      http.get('/fb/v1/console/gdpr/exports/:id', ({ params }) =>
        HttpResponse.json({
          jobId: String(params.id),
          subjectKey: 'expired@example.com',
          subjectDisplay: 'Expired User',
          status: GdprExportStatus.GDPR_EXPORT_STATUS_EXPIRED,
          retryAfterSeconds: 1,
          feedbackCount: 0,
          tagAssignmentCount: 0,
          feedbackAuditCount: 0,
          llmAuditCount: 0,
          surveyInvitationCount: 0,
          surveyResponseCount: 0,
          surveyLowScoreReviewCount: 0,
          surveyProviderEventCount: 0,
          surveyRecoveryNotificationCount: 0,
          createdAt: '2026-06-17T10:00:00Z',
        }),
      ),
    )

    const { user } = renderWithProviders(<GDPRPage />)

    await waitFor(() => {
      expect(screen.getByText('GDPR 数据请求')).toBeInTheDocument()
    })

    await user.type(screen.getByTestId('gdpr-subject-key'), 'expired@example.com')
    await user.click(screen.getByRole('button', { name: '导出 ZIP' }))

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('导出归档已过期，请重新发起导出')
    })
  })

  it('covers GDPR label, action, and formatter helpers', () => {
    const t = (key: string) => key

    expect(gdprPageTestables.exportStatusLabel(GdprExportStatus.GDPR_EXPORT_STATUS_QUEUED, t)).toBe(
      'gdpr.status_queued',
    )
    expect(
      gdprPageTestables.exportStatusLabel(GdprExportStatus.GDPR_EXPORT_STATUS_RUNNING, t),
    ).toBe('gdpr.status_running')
    expect(
      gdprPageTestables.exportStatusLabel(GdprExportStatus.GDPR_EXPORT_STATUS_COMPLETED, t),
    ).toBe('gdpr.status_completed')
    expect(gdprPageTestables.exportStatusLabel(GdprExportStatus.GDPR_EXPORT_STATUS_FAILED, t)).toBe(
      'gdpr.status_failed',
    )
    expect(
      gdprPageTestables.exportStatusLabel(GdprExportStatus.GDPR_EXPORT_STATUS_EXPIRED, t),
    ).toBe('gdpr.status_expired')
    expect(
      gdprPageTestables.exportStatusLabel(GdprExportStatus.GDPR_EXPORT_STATUS_REVOKED, t),
    ).toBe('gdpr.status_revoked')
    expect(gdprPageTestables.exportStatusLabel(99 as unknown as GdprExportStatus, t)).toBe(
      'gdpr.status_unknown',
    )

    expect(
      gdprPageTestables.requestStatusLabel(GdprRequestStatus.GDPR_REQUEST_STATUS_QUEUED, t),
    ).toBe('gdpr.request_status_queued')
    expect(
      gdprPageTestables.requestStatusLabel(GdprRequestStatus.GDPR_REQUEST_STATUS_RUNNING, t),
    ).toBe('gdpr.request_status_running')
    expect(
      gdprPageTestables.requestStatusLabel(GdprRequestStatus.GDPR_REQUEST_STATUS_READY, t),
    ).toBe('gdpr.request_status_ready')
    expect(
      gdprPageTestables.requestStatusLabel(GdprRequestStatus.GDPR_REQUEST_STATUS_DOWNLOADED, t),
    ).toBe('gdpr.request_status_downloaded')
    expect(
      gdprPageTestables.requestStatusLabel(GdprRequestStatus.GDPR_REQUEST_STATUS_COMPLETED, t),
    ).toBe('gdpr.request_status_completed')
    expect(
      gdprPageTestables.requestStatusLabel(GdprRequestStatus.GDPR_REQUEST_STATUS_FAILED, t),
    ).toBe('gdpr.request_status_failed')
    expect(
      gdprPageTestables.requestStatusLabel(GdprRequestStatus.GDPR_REQUEST_STATUS_EXPIRED, t),
    ).toBe('gdpr.request_status_expired')
    expect(
      gdprPageTestables.requestStatusLabel(GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED, t),
    ).toBe('gdpr.request_status_scheduled')
    expect(
      gdprPageTestables.requestStatusLabel(GdprRequestStatus.GDPR_REQUEST_STATUS_CANCELLED, t),
    ).toBe('gdpr.request_status_cancelled')
    expect(
      gdprPageTestables.requestStatusLabel(GdprRequestStatus.GDPR_REQUEST_STATUS_REVOKED, t),
    ).toBe('gdpr.request_status_revoked')
    expect(gdprPageTestables.requestStatusLabel(99 as unknown as GdprRequestStatus, t)).toBe(
      'gdpr.status_unknown',
    )

    expect(
      gdprPageTestables.canCancelRequest(
        GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED,
        GdprRequestType.GDPR_REQUEST_TYPE_DELETE,
      ),
    ).toBe(true)
    expect(
      gdprPageTestables.canCancelRequest(
        GdprRequestStatus.GDPR_REQUEST_STATUS_READY,
        GdprRequestType.GDPR_REQUEST_TYPE_DELETE,
      ),
    ).toBe(false)
    expect(
      gdprPageTestables.canDownloadRequest(
        GdprRequestStatus.GDPR_REQUEST_STATUS_READY,
        GdprRequestType.GDPR_REQUEST_TYPE_EXPORT,
      ),
    ).toBe(true)
    expect(
      gdprPageTestables.canRevokeRequest(
        GdprRequestStatus.GDPR_REQUEST_STATUS_DOWNLOADED,
        GdprRequestType.GDPR_REQUEST_TYPE_EXPORT,
      ),
    ).toBe(true)
    expect(
      gdprPageTestables.canRevokeRequest(
        GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED,
        GdprRequestType.GDPR_REQUEST_TYPE_DELETE,
      ),
    ).toBe(false)

    expect(gdprPageTestables.requestTypeLabel(GdprRequestType.GDPR_REQUEST_TYPE_EXPORT, t)).toBe(
      'gdpr.filter_export',
    )
    expect(gdprPageTestables.requestTypeLabel(GdprRequestType.GDPR_REQUEST_TYPE_DELETE, t)).toBe(
      'gdpr.filter_delete',
    )
    expect(gdprPageTestables.requestTypeLabel(99 as unknown as GdprRequestType, t)).toBe(
      'gdpr.filter_all',
    )

    expect(gdprPageTestables.formatTimestamp()).toBe('—')
    expect(gdprPageTestables.formatTimestamp('not-a-date')).toBe('not-a-date')
    expect(gdprPageTestables.formatSeconds()).toBe('—')
    expect(gdprPageTestables.formatSeconds(86_400)).toBe('1d')
    expect(gdprPageTestables.formatSeconds(7_200)).toBe('2h')
    expect(gdprPageTestables.formatSeconds(180)).toBe('3m')
    expect(gdprPageTestables.formatSeconds(7)).toBe('7s')
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
