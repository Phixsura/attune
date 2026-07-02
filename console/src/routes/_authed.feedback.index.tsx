import { createFileRoute } from '@tanstack/react-router'
import { FeedbackRoutePage } from '@/routes/-feedback-route-page'

type FeedbackSearchScalar = string | number

interface FeedbackIndexSearch {
  ids?: FeedbackSearchScalar
  quality_signal?: FeedbackSearchScalar
  confidence_lte?: FeedbackSearchScalar
  created_from?: FeedbackSearchScalar
  created_to?: FeedbackSearchScalar
  enriched_from?: FeedbackSearchScalar
  enriched_to?: FeedbackSearchScalar
}

export const Route = createFileRoute('/_authed/feedback/')({
  validateSearch: (search: Record<string, unknown>): FeedbackIndexSearch => ({
    ids: asSearchScalar(search.ids),
    quality_signal: asSearchScalar(search.quality_signal),
    confidence_lte: asSearchScalar(search.confidence_lte),
    created_from: asSearchScalar(search.created_from),
    created_to: asSearchScalar(search.created_to),
    enriched_from: asSearchScalar(search.enriched_from),
    enriched_to: asSearchScalar(search.enriched_to),
  }),
  component: FeedbackIndexRoutePage,
})

function FeedbackIndexRoutePage() {
  const search = Route.useSearch()
  const confidenceLteRaw = searchToString(search.confidence_lte)
  const confidenceLte =
    confidenceLteRaw && Number.isFinite(Number(confidenceLteRaw))
      ? Number(confidenceLteRaw)
      : undefined
  return (
    <FeedbackRoutePage
      initialQualityFilters={{
        ids: parseIDs(search.ids),
        qualitySignal: searchToString(search.quality_signal),
        confidenceLte,
        createdFrom: searchToString(search.created_from),
        createdTo: searchToString(search.created_to),
        enrichedFrom: searchToString(search.enriched_from),
        enrichedTo: searchToString(search.enriched_to),
      }}
    />
  )
}

function asSearchScalar(value: unknown) {
  if (typeof value === 'string') return value
  if (typeof value === 'number' && Number.isFinite(value)) return value
  return undefined
}

function searchToString(value: FeedbackSearchScalar | undefined) {
  return value == null ? undefined : String(value)
}

function parseIDs(raw: FeedbackSearchScalar | undefined) {
  if (!raw) return undefined
  const ids = String(raw)
    .split(',')
    .map((id) => id.trim())
    .filter(Boolean)
  return ids.length > 0 ? ids : undefined
}
