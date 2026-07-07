import { createFileRoute } from '@tanstack/react-router'
import { useMemo } from 'react'
import { CustomerRequestsPage } from '@/features/customer-requests/components/customer-requests-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/feedback/customer-requests')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { permission: 'customer_request:view' }),
  validateSearch: (search: Record<string, unknown>) => ({
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
  return (
    <CustomerRequestsPage initialPromoteFeedbackIDs={feedbackIDs} initialFeedbackID={feedbackID} />
  )
}

function parseFeedbackIDs(raw: string) {
  return raw
    .split(',')
    .map((id) => id.trim())
    .filter(Boolean)
    .map((id) => Number(id))
    .filter((id) => Number.isInteger(id) && id > 0)
    .map((id) => String(id))
}

function parseFeedbackID(raw: string) {
  const parsed = Number(raw.trim())
  return Number.isInteger(parsed) && parsed > 0 ? String(parsed) : undefined
}
