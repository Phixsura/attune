import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export interface RecentFeedbackItem {
  id: number
  content_preview: string
  source: string
  source_meta?: Record<string, unknown>
  created_at: string
}

interface RecentFeedbackResponse {
  items: RecentFeedbackItem[]
}

export function recentFeedbackQuery(sourceId: string | null) {
  return {
    queryKey: ['console', 'inbound-sources', 'recent', sourceId],
    queryFn: () => api<RecentFeedbackResponse>(`/fb/v1/console/inbound/sources/${sourceId}/recent`),
    enabled: !!sourceId,
    staleTime: 15_000,
  }
}

export function useRecentFeedback(sourceId: string | null) {
  return useQuery(recentFeedbackQuery(sourceId))
}
