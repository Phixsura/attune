import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export interface SimilarFeedbackItem {
  id: number
  title: string
  source: string
  similarity: number
  created_at: string
}

interface SimilarFeedbackResponse {
  items: SimilarFeedbackItem[]
}

// similarFeedbackQuery fetches semantically-similar feedback — the
// recurrence signal behind a request candidate ("this issue came up N
// more times"). Empty when embeddings are unavailable.
export const similarFeedbackQuery = (feedbackId: string) =>
  queryOptions({
    queryKey: ['console', 'feedback', 'similar', feedbackId] as const,
    queryFn: async ({ signal }) =>
      api<SimilarFeedbackResponse>(`/fb/v1/console/feedback/${feedbackId}/similar`, { signal }),
    staleTime: 60_000,
    enabled: !!feedbackId,
  })
