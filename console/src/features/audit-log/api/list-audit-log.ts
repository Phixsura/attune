import { infiniteQueryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import { triggerBlobDownload } from '@/lib/blob-download'
import type { AuditLogEntry, ListAuditLogResponse } from '@/proto/attune/v1/audit'

export type { AuditLogEntry }

export interface AuditLogFilters {
  actions?: string[]
  actorId?: string
  actorType?: string
  cursor?: string
  from?: string
  limit?: number
  targetId?: string
  targetType?: string
  to?: string
}

function toSearchParams(filters: AuditLogFilters) {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(filters)) {
    if (value === undefined || value === null || value === '') continue
    if (Array.isArray(value)) {
      for (const item of value) {
        const trimmed = item.trim()
        if (!trimmed) continue
        params.append(key === 'actions' ? 'action' : key, trimmed)
      }
      continue
    }
    params.set(key, String(value))
  }
  return params
}

export const auditLogInfiniteQuery = (filters: AuditLogFilters) =>
  infiniteQueryOptions({
    queryKey: ['console', 'audit-log', filters] as const,
    queryFn: async ({ pageParam, signal }) => {
      const params = toSearchParams({ ...filters, cursor: pageParam ?? undefined })
      const suffix = params.toString() ? `?${params.toString()}` : ''
      return api<ListAuditLogResponse>(`/fb/v1/console/audit-log${suffix}`, { signal })
    },
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? null,
    staleTime: 15_000,
  })

export async function downloadAuditLogCsv(filters: AuditLogFilters) {
  const params = toSearchParams(filters)
  const suffix = params.toString() ? `?${params.toString()}` : ''
  const res = await fetch(`/fb/v1/console/audit-log/export.csv${suffix}`, {
    credentials: 'include',
  })
  if (!res.ok) {
    throw new Error(await readErrorMessage(res))
  }
  const blob = await res.blob()
  const disposition = res.headers.get('Content-Disposition')
  const match = disposition?.match(/filename="([^"]+)"/)
  triggerBlobDownload(blob, match?.[1] ?? 'audit-log.csv')
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
