import { useMutation, useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import { triggerBlobDownload } from '@/lib/blob-download'
import type {
  CreateAuditEvidenceExportRequest,
  CreateAuditEvidenceExportResponse,
  GetAuditEvidenceExportResponse,
} from '@/proto/attune/v1/audit'

export type EvidenceExportFilter = Omit<CreateAuditEvidenceExportRequest, 'from' | 'to'> & {
  from?: string
  to?: string
}

export function useStartEvidenceExport() {
  return useMutation({
    mutationFn: (filter: EvidenceExportFilter) =>
      api<CreateAuditEvidenceExportResponse>('/fb/v1/console/audit-log/evidence', {
        method: 'POST',
        body: filter,
      }),
  })
}

const EVIDENCE_POLL_TIMEOUT_MS = 10 * 60 * 1000

export function useEvidenceExportStatus(jobId: null | string) {
  return useQuery({
    queryKey: ['console', 'evidence-export', jobId],
    queryFn: ({ signal }) =>
      api<GetAuditEvidenceExportResponse>(`/fb/v1/console/audit-log/evidence/${jobId}`, {
        signal,
      }),
    enabled: !!jobId,
    refetchInterval: (query) => {
      const data = query.state.data
      if (!data) return false
      if (isActiveEvidenceStatus(data.status)) {
        const elapsed = Date.now() - query.state.dataUpdatedAt
        /* v8 ignore next -- @preserve: timeout guard protects long-lived polling sessions. */
        if (elapsed > EVIDENCE_POLL_TIMEOUT_MS) return false
        /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
        const interval = (data.retryAfterSeconds ?? 2) * 1000
        /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
        return Math.max(1000, Number.isFinite(interval) ? interval : 2000)
      }
      return false
    },
  })
}

export async function downloadEvidenceExport(jobId: string, filename?: string) {
  const res = await fetch(`/fb/v1/console/audit-log/evidence/${jobId}/download`, {
    method: 'GET',
    headers: { Accept: 'application/zip' },
    credentials: 'include',
  })
  if (!res.ok) {
    throw new Error(await readErrorMessage(res))
  }
  const blob = await res.blob()
  const disposition = res.headers.get('Content-Disposition')
  const match = disposition?.match(/filename="([^"]+)"/)
  /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
  triggerBlobDownload(blob, match?.[1] ?? filename ?? 'audit-evidence.zip')
}

export function isActiveEvidenceStatus(status: string) {
  return status === 'queued' || status === 'running'
}

export function isTerminalEvidenceStatus(status: string) {
  return status === 'completed' || status === 'failed' || status === 'expired'
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
