import { createFileRoute } from '@tanstack/react-router'
import { useCallback, useMemo } from 'react'
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
    account_key: parseAccountKey(typeof search.account_key === 'string' ? search.account_key : ''),
  }),
  component: CustomerRequestsRoutePage,
})

function CustomerRequestsRoutePage() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const feedbackIDs = useMemo(
    () => parseFeedbackIDs(search.promote_feedback_ids ?? ''),
    [search.promote_feedback_ids],
  )
  const feedbackID = useMemo(() => parseFeedbackID(search.feedback_id ?? ''), [search.feedback_id])
  const accountKey = useMemo(() => parseAccountKey(search.account_key ?? ''), [search.account_key])
  const requestID = useMemo(() => parseRequestID(search.request_id ?? ''), [search.request_id])
  const mergeTargetID = useMemo(
    () => parseRequestID(search.merge_target_id ?? ''),
    [search.merge_target_id],
  )
  const handlePromoteClose = useCallback(() => {
    void navigate({
      to: '/feedback/customer-requests',
      search: {
        request_id: requestID,
        merge_target_id: mergeTargetID,
        promote_feedback_ids: undefined,
        feedback_id: feedbackID,
        account_key: accountKey,
      },
    })
  }, [accountKey, feedbackID, mergeTargetID, navigate, requestID])
  const handleAccountKeyInspect = useCallback(
    (accountKey: string) => {
      void navigate({
        to: '/feedback/customer-requests',
        search: {
          request_id: undefined,
          merge_target_id: undefined,
          promote_feedback_ids: undefined,
          feedback_id: undefined,
          account_key: accountKey,
        },
      })
    },
    [navigate],
  )
  return (
    <CustomerRequestsPage
      initialPromoteFeedbackIDs={feedbackIDs}
      initialFeedbackID={feedbackID}
      initialAccountKey={accountKey}
      initialRequestID={requestID}
      initialMergeTargetID={mergeTargetID}
      onAccountKeyInspect={handleAccountKeyInspect}
      onPromoteClose={handlePromoteClose}
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

export function parseAccountKey(raw: string) {
  const normalized = raw.trim()
  if (!normalized || Array.from(normalized).length > 512) {
    return undefined
  }
  return normalized
}

function isUUID(raw: string) {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(raw)
}
