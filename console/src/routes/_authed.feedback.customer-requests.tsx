import { createFileRoute } from '@tanstack/react-router'
import { useMemo } from 'react'
import { CustomerRequestsPage } from '@/features/customer-requests/components/customer-requests-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/feedback/customer-requests')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { permission: 'customer_request:view' }),
  validateSearch: (search: Record<string, unknown>) => ({
    request_id: parseRequestID(typeof search.request_id === 'string' ? search.request_id : ''),
    merge_target_id: parseRequestID(
      typeof search.merge_target_id === 'string' ? search.merge_target_id : '',
    ),
    promote_feedback_ids:
      typeof search.promote_feedback_ids === 'string' ? search.promote_feedback_ids : undefined,
    feedback_id: typeof search.feedback_id === 'string' ? search.feedback_id : undefined,
  }),
  component: CustomerRequestsRoutePage,
})

function CustomerRequestsRoutePage() {
  const search = Route.useSearch()
  const feedbackIDs = useMemo(
    () => parseFeedbackIDs(search.promote_feedback_ids ?? ''),
    [search.promote_feedback_ids],
  )
  const feedbackID = useMemo(() => parseFeedbackID(search.feedback_id ?? ''), [search.feedback_id])
  const requestID = useMemo(() => parseRequestID(search.request_id ?? ''), [search.request_id])
  const mergeTargetID = useMemo(
    () => parseRequestID(search.merge_target_id ?? ''),
    [search.merge_target_id],
  )
  return (
    <CustomerRequestsPage
      initialPromoteFeedbackIDs={feedbackIDs}
      initialFeedbackID={feedbackID}
      initialRequestID={requestID}
      initialMergeTargetID={mergeTargetID}
    />
  )
}

export function parseFeedbackIDs(raw: string) {
  return raw
    .split(',')
    .map((id) => id.trim())
    .filter(Boolean)
    .map((id) => Number(id))
    .filter((id) => Number.isInteger(id) && id > 0)
    .map((id) => String(id))
}

export function parseFeedbackID(raw: string) {
  const parsed = Number(raw.trim())
  return Number.isInteger(parsed) && parsed > 0 ? String(parsed) : undefined
}

export function parseRequestID(raw: string) {
  const normalized = raw.trim().toLowerCase()
  return isUUID(normalized) ? normalized : undefined
}

function isUUID(raw: string) {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(raw)
}
