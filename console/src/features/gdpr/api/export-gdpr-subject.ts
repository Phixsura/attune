import { useMutation, useQuery } from '@tanstack/react-query'
import { api, getCsrfToken } from '@/lib/api-client'
import { triggerBlobDownload } from '@/lib/blob-download'
import {
  type ExportGdprSubjectResponse,
  GdprExportStatus,
  type GdprExportStatusResponse,
  type RevokeGdprExportResponse,
} from '@/proto/attune/v1/gdpr'

export function useStartGdprExport() {
  return useMutation({
    mutationFn: (subjectKey: string) =>
      api<ExportGdprSubjectResponse>('/fb/v1/console/gdpr/export', {
        method: 'POST',
        body: { subjectKey },
      }),
  })
}

export function useGdprExportStatus(jobId: null | string) {
  return useQuery({
    queryKey: ['console', 'gdpr-export', jobId],
    queryFn: ({ signal }) =>
      api<GdprExportStatusResponse>(`/fb/v1/console/gdpr/exports/${jobId}`, {
        signal,
      }),
    enabled: !!jobId,
    refetchInterval: (query) => {
      const data = query.state.data
      if (!data) return false
      if (isActiveStatus(data.status)) {
        return Math.max(1000, data.retryAfterSeconds * 1000)
      }
      return false
    },
  })
}

export function useRevokeGdprExport() {
  return useMutation({
    mutationFn: (jobId: string) =>
      api<RevokeGdprExportResponse>(`/fb/v1/console/gdpr/exports/${jobId}/revoke`, {
        method: 'POST',
        body: {},
      }),
  })
}

export async function downloadGdprExport(jobId: string, filename?: string) {
  const headers: Record<string, string> = {
    Accept: 'application/zip',
  }
  const csrfToken = getCsrfToken()
  if (csrfToken) {
    headers['X-CSRF-Token'] = csrfToken
  }
  const res = await fetch(`/fb/v1/console/gdpr/exports/${jobId}/download`, {
    method: 'GET',
    headers,
    credentials: 'include',
  })
  if (!res.ok) {
    throw new Error(await readErrorMessage(res))
  }
  const blob = await res.blob()
  const disposition = res.headers.get('Content-Disposition')
  const match = disposition?.match(/filename="([^"]+)"/)
  triggerBlobDownload(blob, match?.[1] ?? filename ?? 'gdpr-export.zip')
}

export function isActiveStatus(status: GdprExportStatus) {
  return (
    status === GdprExportStatus.GDPR_EXPORT_STATUS_QUEUED ||
    status === GdprExportStatus.GDPR_EXPORT_STATUS_RUNNING
  )
}

async function readErrorMessage(res: Response) {
  const fallback = `HTTP ${res.status}`
  const text = await res.text()
  if (!text) return fallback
  try {
    const parsed = JSON.parse(text) as { message?: string }
    return parsed.message || fallback
  } catch {
    return fallback
  }
}
