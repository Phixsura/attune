import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  RecordSearchEventRequest,
  RecordSearchEventResponse,
  SemanticSearchRequest,
  SemanticSearchResponse,
} from '@/proto/attune/v1/search'

export function useSemanticSearch() {
  return useMutation({
    mutationFn: (request: SemanticSearchRequest) =>
      api<SemanticSearchResponse>('/fb/v1/console/feedback/search', {
        method: 'POST',
        body: request,
      }),
  })
}

export function useRecordSearchEvent() {
  return useMutation({
    mutationFn: (request: RecordSearchEventRequest) =>
      api<RecordSearchEventResponse>('/fb/v1/console/feedback/search/events', {
        method: 'POST',
        body: request,
      }),
  })
}
