import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { GetSearchQualityResponse } from '@/proto/attune/v1/search'

export type SearchQuality = GetSearchQualityResponse
export type SearchQualityRange = '7d' | '30d' | '90d'
export type SearchQualityBucket = 'hour' | 'day'

export interface SearchQualityFilters {
  range?: SearchQualityRange
  bucketWidth?: SearchQualityBucket
  limit?: number
}

export const defaultSearchQualityFilters: Required<
  Pick<SearchQualityFilters, 'range' | 'bucketWidth' | 'limit'>
> = {
  range: '7d',
  bucketWidth: 'day',
  limit: 10,
}

export const searchQualityQuery = (filters: SearchQualityFilters = {}) =>
  queryOptions({
    queryKey: ['console', 'search-quality', normalizeSearchQualityFilters(filters)],
    queryFn: ({ signal }) => {
      const qs = searchQualitySearchParams(filters)
      const suffix = qs.size > 0 ? `?${qs.toString()}` : ''
      return api<SearchQuality>(`/fb/v1/console/feedback/search/quality${suffix}`, {
        signal,
      })
    },
    staleTime: 60_000,
  })

export function searchQualitySearchParams(filters: SearchQualityFilters) {
  const merged = normalizeSearchQualityFilters(filters)
  const window = searchQualityWindowForRange(merged.range)
  const qs = new URLSearchParams()
  qs.set('current_from', window.currentFrom)
  qs.set('current_to', window.currentTo)
  qs.set('bucket_width', merged.bucketWidth)
  qs.set('limit', String(merged.limit))
  return qs
}

export function normalizeSearchQualityFilters(filters: SearchQualityFilters = {}) {
  const merged = { ...defaultSearchQualityFilters, ...filters }
  return {
    ...merged,
    bucketWidth: searchQualityBucketForRange(merged.range, merged.bucketWidth),
    limit: clampSearchQualityLimit(merged.limit),
  }
}

export function searchQualityBucketForRange(
  range: SearchQualityRange,
  bucketWidth: SearchQualityBucket,
) {
  return range === '7d' ? bucketWidth : 'day'
}

export function searchQualityWindowForRange(range: SearchQualityRange, now = new Date()) {
  const days = rangeToDays(range)
  const currentTo = utcDayStart(now)
  const currentFrom = addUTCDays(currentTo, -days)
  return {
    currentFrom: currentFrom.toISOString(),
    currentTo: currentTo.toISOString(),
  }
}

function clampSearchQualityLimit(limit: number | undefined) {
  if (!limit || limit < 1) return defaultSearchQualityFilters.limit
  return Math.min(Math.floor(limit), 50)
}

function rangeToDays(range: SearchQualityRange) {
  if (range === '30d') return 30
  if (range === '90d') return 90
  return 7
}

function utcDayStart(date: Date) {
  return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()))
}

function addUTCDays(date: Date, days: number) {
  const out = new Date(date)
  out.setUTCDate(out.getUTCDate() + days)
  return out
}
