import { describe, expect, it } from 'vitest'
import {
  parseFeedbackID,
  parseFeedbackIDs,
  Route,
} from '@/routes/_authed.feedback.customer-requests'

describe('customer requests route search parsing', () => {
  it('normalizes valid feedback ids and drops invalid entries', () => {
    expect(parseFeedbackIDs(' 1, nope, 2.5, -3, 4 ')).toEqual(['1', '4'])
    expect(parseFeedbackIDs('')).toEqual([])
    expect(parseFeedbackID(' 42 ')).toBe('42')
    expect(parseFeedbackID('nope')).toBeUndefined()
    expect(parseFeedbackID('-1')).toBeUndefined()
  })

  it('validates route search params by type before component parsing', () => {
    const validateSearch = Route.options.validateSearch as (search: Record<string, unknown>) => {
      promote_feedback_ids?: string
      feedback_id?: string
    }
    expect(validateSearch({ promote_feedback_ids: '1,2', feedback_id: '42' })).toEqual({
      promote_feedback_ids: '1,2',
      feedback_id: '42',
    })
    expect(validateSearch({ promote_feedback_ids: 1, feedback_id: null })).toEqual({
      promote_feedback_ids: undefined,
      feedback_id: undefined,
    })
  })
})
