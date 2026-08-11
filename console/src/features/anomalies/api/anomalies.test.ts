import { describe, expect, it, vi } from 'vitest'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api-client', () => ({ api: apiMock }))

import {
  anomaliesQuery,
  anomalyConfigQuery,
  anomalyEvidenceQuery,
  anomalySeriesQuery,
} from './anomalies'

type AnyQuery = { queryFn?: unknown }

// run invokes a queryOptions queryFn with a minimal context.
async function run<T>(q: AnyQuery): Promise<T> {
  const fn = q.queryFn as (ctx: { signal: AbortSignal }) => Promise<T>
  return fn({ signal: new AbortController().signal })
}

describe('anomalies api', () => {
  it('anomaliesQuery defaults to open status and limit 50', async () => {
    apiMock.mockResolvedValue({ events: [{ eventId: 'e1' }] })
    const q = anomaliesQuery()
    const events = await run<unknown[]>(q)
    expect(apiMock).toHaveBeenCalledWith(
      '/fb/v1/console/anomalies?status=open&limit=50',
      expect.anything(),
    )
    expect(events).toHaveLength(1)
  })

  it('anomaliesQuery omits status for all', async () => {
    apiMock.mockResolvedValue({ events: [] })
    await run(anomaliesQuery({ limit: 10, status: 'all' }))
    expect(apiMock).toHaveBeenCalledWith('/fb/v1/console/anomalies?limit=10', expect.anything())
  })

  it('anomalySeriesQuery encodes slice params', async () => {
    apiMock.mockResolvedValue({ points: [], sliceDisplay: '' })
    await run(anomalySeriesQuery('dimension', 'dim:severity=1a2b3c4d', 30))
    const url = apiMock.mock.calls.at(-1)?.[0] as string
    expect(url).toContain('/fb/v1/console/anomalies/series?')
    expect(url).toContain('slice_type=dimension')
    expect(url).toContain('days=30')
  })

  it('anomalyEvidenceQuery targets the event path', async () => {
    apiMock.mockResolvedValue({ contributions: [], feedbackIds: [], spread: false })
    await run(anomalyEvidenceQuery('ev-1'))
    expect(apiMock).toHaveBeenCalledWith(
      '/fb/v1/console/anomalies/ev-1/evidence',
      expect.anything(),
    )
  })

  it('anomalyConfigQuery unwraps the config field', async () => {
    apiMock.mockResolvedValue({ config: { sensitivity: 'medium' } })
    const cfg = await run(anomalyConfigQuery())
    expect(cfg).toEqual({ sensitivity: 'medium' })
  })
})
