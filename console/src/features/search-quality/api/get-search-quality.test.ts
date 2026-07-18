import { describe, expect, it } from 'vitest'
import {
  normalizeSearchQualityFilters,
  searchQualityBucketForRange,
  searchQualitySearchParams,
  searchQualityWindowForRange,
} from '@/features/search-quality/api/get-search-quality'

describe('search-quality query helpers', () => {
  it('normalizes invalid and oversized limits', () => {
    expect(normalizeSearchQualityFilters({ limit: 0 }).limit).toBe(10)
    expect(normalizeSearchQualityFilters({ limit: -1 }).limit).toBe(10)
    expect(normalizeSearchQualityFilters({ limit: 12.8 }).limit).toBe(12)
    expect(normalizeSearchQualityFilters({ limit: 100 }).limit).toBe(50)
  })

  it('uses day buckets for longer windows and preserves explicit 7d buckets', () => {
    expect(searchQualityBucketForRange('7d', 'hour')).toBe('hour')
    expect(searchQualityBucketForRange('30d', 'hour')).toBe('day')
    expect(searchQualityBucketForRange('90d', 'hour')).toBe('day')
  })

  it('builds deterministic UTC windows and query params', () => {
    const now = new Date('2026-07-17T23:45:00Z')

    expect(searchQualityWindowForRange('30d', now)).toEqual({
      currentFrom: '2026-06-17T00:00:00.000Z',
      currentTo: '2026-07-17T00:00:00.000Z',
    })
    expect(searchQualityWindowForRange('90d', now).currentFrom).toBe('2026-04-18T00:00:00.000Z')

    const params = searchQualitySearchParams({ range: '90d', bucketWidth: 'hour', limit: 100 })
    expect(params.get('bucket_width')).toBe('day')
    expect(params.get('limit')).toBe('50')
  })
})
