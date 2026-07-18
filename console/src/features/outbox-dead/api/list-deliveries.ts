import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ListDeliveriesResponse, OutboxDelivery } from '@/proto/attune/v1/outbox'

// Re-export the proto row type under a feature-stable alias so consumers
// (table rows, page, route file) don't follow ts-proto rename churn.
export type { OutboxDelivery }

// DeliveryStatusFilter is the simple dead/failed toggle the operator
// view exposes. The backend accepts a repeatable `status` query param;
// the page picks exactly one of these at a time.
export type DeliveryStatusFilter = 'dead' | 'failed'

// Keyset page size. The backend clamps to [1,200]; 50 matches the
// default the issue spec calls out and keeps the table scannable.
export const DELIVERIES_PAGE_SIZE = 50

// deliveriesQuery lists the caller's tenant deliveries filtered to a
// single status, newest first. The query key carries the status so the
// dead/failed toggle gets its own cache entry (and its own refetch on
// retry-invalidate). beforeId is omitted here: the operator view shows
// the freshest page; pagination is a follow-up if the dead queue grows.
export const deliveriesQuery = (status: DeliveryStatusFilter) =>
  queryOptions({
    queryKey: ['console', 'outbox-deliveries', status],
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams()
      params.append('status', status)
      params.set('limit', String(DELIVERIES_PAGE_SIZE))
      const resp = await api<ListDeliveriesResponse>(
        `/fb/v1/console/outbox/deliveries?${params.toString()}`,
        { signal },
      )
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      return resp.deliveries ?? []
    },
    staleTime: 15_000,
  })
