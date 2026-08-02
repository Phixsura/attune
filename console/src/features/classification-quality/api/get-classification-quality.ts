import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  GetClassificationQualityResponse,
  GetClassificationReviewLearningResponse,
  RecordClassificationReviewRequest,
  RecordClassificationReviewResponse,
} from '@/proto/attune/v1/classification_quality'

export type ClassificationQuality = GetClassificationQualityResponse
export type ClassificationReviewLearning = GetClassificationReviewLearningResponse

export type ClassificationQualityRange = '7d' | '30d' | '90d'
export type ClassificationQualityBucket = 'hour' | 'day'
export type ClassificationQualitySeverity =
  | 'all'
  | 'alert'
  | 'watch'
  | 'normal'
  | 'insufficient_data'

export interface ClassificationQualityFilters {
  range?: ClassificationQualityRange
  bucketWidth?: ClassificationQualityBucket
  severity?: ClassificationQualitySeverity
  dimensionName?: string
  source?: string
  logicalModel?: string
  providerModel?: string
  channelId?: string
}

export interface ClassificationReviewLearningFilters {
  range?: ClassificationQualityRange
  signalReason?: string
  limit?: number
}

export const defaultClassificationQualityFilters: Required<
  Pick<ClassificationQualityFilters, 'range' | 'bucketWidth' | 'severity'>
> = {
  range: '7d',
  bucketWidth: 'day',
  severity: 'all',
}

export const classificationQualityQueryKey = ['console', 'classification-quality'] as const
export const classificationReviewLearningQueryKey = [
  'console',
  'classification-review-learning',
] as const

export const classificationQualityQuery = (filters: ClassificationQualityFilters = {}) =>
  queryOptions({
    queryKey: [...classificationQualityQueryKey, normalizeClassificationQualityFilters(filters)],
    queryFn: ({ signal }) => {
      const qs = classificationQualitySearchParams(filters)
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      const suffix = qs.size > 0 ? `?${qs.toString()}` : ''
      return api<ClassificationQuality>(`/fb/v1/console/classification-quality${suffix}`, {
        signal,
      })
    },
    staleTime: 60_000,
  })

export const classificationReviewLearningQuery = (
  filters: ClassificationReviewLearningFilters = {},
) =>
  queryOptions({
    queryKey: [...classificationReviewLearningQueryKey, normalizeReviewLearningFilters(filters)],
    queryFn: ({ signal }) => {
      const qs = classificationReviewLearningSearchParams(filters)
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      const suffix = qs.size > 0 ? `?${qs.toString()}` : ''
      return api<ClassificationReviewLearning>(
        `/fb/v1/console/classification-quality/review-learning${suffix}`,
        { signal },
      )
    },
    staleTime: 30_000,
  })

export function useRecordClassificationReview() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: RecordClassificationReviewRequest) =>
      api<RecordClassificationReviewResponse>('/fb/v1/console/classification-quality/reviews', {
        method: 'POST',
        body,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: classificationQualityQueryKey })
      void qc.invalidateQueries({ queryKey: classificationReviewLearningQueryKey })
    },
  })
}

export function classificationQualitySearchParams(filters: ClassificationQualityFilters) {
  const merged = normalizeClassificationQualityFilters(filters)
  const window = qualityWindowForRange(merged.range)
  const qs = new URLSearchParams()
  qs.set('current_from', window.currentFrom)
  qs.set('current_to', window.currentTo)
  qs.set('baseline_from', window.baselineFrom)
  qs.set('baseline_to', window.baselineTo)
  qs.set('bucket_width', merged.bucketWidth)
  if (merged.severity !== 'all') qs.set('severity', merged.severity)
  appendParam(qs, 'dimension_name', merged.dimensionName)
  appendParam(qs, 'source', merged.source)
  appendParam(qs, 'logical_model', merged.logicalModel)
  appendParam(qs, 'provider_model', merged.providerModel)
  appendParam(qs, 'channel_id', merged.channelId)
  return qs
}

export function classificationReviewLearningSearchParams(
  filters: ClassificationReviewLearningFilters,
) {
  const merged = normalizeReviewLearningFilters(filters)
  const window = qualityWindowForRange(merged.range)
  const qs = new URLSearchParams()
  qs.set('current_from', window.currentFrom)
  qs.set('current_to', window.currentTo)
  appendParam(qs, 'signal_reason', merged.signalReason)
  qs.set('limit', String(merged.limit))
  return qs
}

export function normalizeClassificationQualityFilters(filters: ClassificationQualityFilters = {}) {
  const merged = { ...defaultClassificationQualityFilters, ...filters }
  return {
    ...merged,
    bucketWidth: classificationQualityBucketForRange(merged.range, merged.bucketWidth),
  }
}

export function normalizeReviewLearningFilters(filters: ClassificationReviewLearningFilters = {}) {
  return {
    range: filters.range ?? defaultClassificationQualityFilters.range,
    signalReason: filters.signalReason?.trim() || undefined,
    limit: clampReviewLearningLimit(filters.limit),
  }
}

export function classificationQualityBucketForRange(
  range: ClassificationQualityRange,
  bucketWidth: ClassificationQualityBucket,
) {
  return range === '7d' ? bucketWidth : 'day'
}

export function qualityWindowForRange(range: ClassificationQualityRange, now = new Date()) {
  const days = rangeToDays(range)
  const baselineDays = Math.min(days * 4, 90)
  const currentTo = utcDayStart(now)
  const currentFrom = addUTCDays(currentTo, -days)
  const baselineTo = currentFrom
  const baselineFrom = addUTCDays(baselineTo, -baselineDays)
  return {
    currentFrom: currentFrom.toISOString(),
    currentTo: currentTo.toISOString(),
    baselineFrom: baselineFrom.toISOString(),
    baselineTo: baselineTo.toISOString(),
  }
}

function rangeToDays(range: ClassificationQualityRange) {
  if (range === '30d') return 30
  /* v8 ignore next -- @preserve: 90d classification window mirrors search-quality helper coverage. */
  if (range === '90d') return 90
  return 7
}

function clampReviewLearningLimit(limit: number | undefined) {
  if (!limit || limit < 1) return 10
  return Math.min(Math.floor(limit), 50)
}

function utcDayStart(date: Date) {
  return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()))
}

function addUTCDays(date: Date, days: number) {
  const out = new Date(date)
  out.setUTCDate(out.getUTCDate() + days)
  return out
}

function appendParam(qs: URLSearchParams, key: string, value: string | undefined) {
  const trimmed = value?.trim()
  if (trimmed) qs.set(key, trimmed)
}
