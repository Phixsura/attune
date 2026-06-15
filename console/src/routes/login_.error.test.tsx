import { describe, expect, it } from 'vitest'
import { Route as LoginErrorRoute } from '@/routes/login_.error'

describe('LoginError route validateSearch', () => {
  const validateSearch = LoginErrorRoute.options.validateSearch as (s: Record<string, unknown>) => {
    code?: string
    request_id?: string
    trace_id?: string
  }

  it('extracts all string params', () => {
    expect(validateSearch({ code: 'state_expired', request_id: 'r1', trace_id: 't1' })).toEqual({
      code: 'state_expired',
      request_id: 'r1',
      trace_id: 't1',
    })
  })

  it('returns undefined for non-string values', () => {
    expect(validateSearch({ code: 123 })).toEqual({
      code: undefined,
      request_id: undefined,
      trace_id: undefined,
    })
  })

  it('handles empty search', () => {
    expect(validateSearch({})).toEqual({
      code: undefined,
      request_id: undefined,
      trace_id: undefined,
    })
  })
})
