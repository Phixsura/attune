import { api } from '@/lib/api-client'
import type { ListEnrichPromptVersionsResponse } from '@/proto/attune/v1/enrich_config'

export async function listEnrichPromptVersions({
  cursor,
  limit = 12,
  q,
  signal,
}: {
  cursor?: string
  limit?: number
  q?: string
  signal?: AbortSignal
}) {
  const params = new URLSearchParams()
  params.set('limit', String(limit))
  if (cursor) params.set('cursor', cursor)
  if (q?.trim()) params.set('q', q.trim())
  return api<ListEnrichPromptVersionsResponse>(
    `/fb/v1/console/enrich-config/versions?${params.toString()}`,
    { signal },
  )
}
