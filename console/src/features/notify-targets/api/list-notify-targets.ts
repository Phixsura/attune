import { queryOptions } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { ListNotifyTargetsResponse, NotifyTarget } from '@/proto/attune/v1/notify_target'

// Re-export the proto type under a feature-stable alias so consumers
// (table rows, dialog props, route file) don't follow ts-proto rename churn.
export type { NotifyTarget }

export const notifyTargetsQuery = () =>
  queryOptions({
    queryKey: ['console', 'notify-targets'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListNotifyTargetsResponse>('/fb/v1/console/notify-targets', {
        signal,
      })
      return resp.items
    },
    staleTime: 30_000,
  })
