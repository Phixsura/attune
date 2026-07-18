import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { createElement, type PropsWithChildren } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  downloadGdprExport,
  isActiveStatus,
  useRevokeGdprExport,
} from '@/features/gdpr/api/export-gdpr-subject'
import { setCsrfToken } from '@/lib/api-client'
import { GdprExportStatus } from '@/proto/attune/v1/gdpr'
import { server } from '@/testing/mocks/server'
import { renderHook, waitFor } from '@/testing/test-utils'

vi.mock('@/lib/blob-download', () => ({
  triggerBlobDownload: vi.fn(),
}))

import { triggerBlobDownload } from '@/lib/blob-download'

describe('gdpr export api helpers', () => {
  afterEach(() => {
    setCsrfToken(null)
    vi.clearAllMocks()
  })

  it('downloads export archive with filename from content-disposition', async () => {
    server.use(
      http.get(
        '/fb/v1/console/gdpr/exports/job-123/download',
        () =>
          new HttpResponse('zip-bytes', {
            status: 200,
            headers: {
              'Content-Disposition': 'attachment; filename="gdpr-export-job-123.zip"',
              'Content-Type': 'application/zip',
            },
          }),
      ),
    )

    await downloadGdprExport('job-123')

    expect(triggerBlobDownload).toHaveBeenCalledTimes(1)
    const [blob, filename] = vi.mocked(triggerBlobDownload).mock.calls[0] ?? []
    expect(blob).toMatchObject({
      size: 9,
      type: 'application/zip',
    })
    expect(blob?.type).toBe('application/zip')
    expect(filename).toBe('gdpr-export-job-123.zip')
  })

  it('sends the CSRF token and uses the caller filename when no server filename is present', async () => {
    let csrfHeader: string | null = null
    setCsrfToken('csrf-token-1')
    server.use(
      http.get('/fb/v1/console/gdpr/exports/job-456/download', ({ request }) => {
        csrfHeader = request.headers.get('X-CSRF-Token')
        return new HttpResponse('zip-bytes', {
          status: 200,
          headers: { 'Content-Type': 'application/zip' },
        })
      }),
    )

    await downloadGdprExport('job-456', 'requested-gdpr.zip')

    expect(csrfHeader).toBe('csrf-token-1')
    expect(triggerBlobDownload).toHaveBeenCalledTimes(1)
    const [, filename] = vi.mocked(triggerBlobDownload).mock.calls[0] ?? []
    expect(filename).toBe('requested-gdpr.zip')
  })

  it('surfaces structured download errors', async () => {
    server.use(
      http.get('/fb/v1/console/gdpr/exports/job-123/download', () =>
        HttpResponse.json({ message: 'archive expired' }, { status: 409 }),
      ),
    )

    await expect(downloadGdprExport('job-123')).rejects.toThrow('archive expired')
  })

  it('falls back to the HTTP status when the error body is empty', async () => {
    server.use(
      http.get(
        '/fb/v1/console/gdpr/exports/job-empty/download',
        () => new HttpResponse(null, { status: 404 }),
      ),
    )

    await expect(downloadGdprExport('job-empty')).rejects.toThrow('HTTP 404')
  })

  it('falls back to the HTTP status when the error body is not structured JSON', async () => {
    server.use(
      http.get('/fb/v1/console/gdpr/exports/job-plain/download', () =>
        HttpResponse.text('not json', { status: 500 }),
      ),
    )

    await expect(downloadGdprExport('job-plain')).rejects.toThrow('HTTP 500')
  })

  it('falls back to the HTTP status when JSON lacks a message field', async () => {
    server.use(
      http.get('/fb/v1/console/gdpr/exports/job-json/download', () =>
        HttpResponse.json({ error: 'expired' }, { status: 410 }),
      ),
    )

    await expect(downloadGdprExport('job-json')).rejects.toThrow('HTTP 410')
  })

  it('treats queued and running exports as active', () => {
    expect(isActiveStatus(GdprExportStatus.GDPR_EXPORT_STATUS_QUEUED)).toBe(true)
    expect(isActiveStatus(GdprExportStatus.GDPR_EXPORT_STATUS_RUNNING)).toBe(true)
    expect(isActiveStatus(GdprExportStatus.GDPR_EXPORT_STATUS_COMPLETED)).toBe(false)
  })

  it('revokes a ready export archive', async () => {
    server.use(
      http.post('/fb/v1/console/gdpr/exports/job-123/revoke', () =>
        HttpResponse.json({
          jobId: 'job-123',
          status: GdprExportStatus.GDPR_EXPORT_STATUS_REVOKED,
          requestStatus: 10,
        }),
      ),
    )

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const wrapper = ({ children }: PropsWithChildren) =>
      createElement(QueryClientProvider, { client: queryClient }, children)

    const { result } = renderHook(() => useRevokeGdprExport(), { wrapper })
    result.current.mutate('job-123')

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })
    expect(result.current.data?.jobId).toBe('job-123')
    expect(result.current.data?.status).toBe(GdprExportStatus.GDPR_EXPORT_STATUS_REVOKED)
  })
})
