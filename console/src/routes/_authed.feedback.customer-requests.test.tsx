import { describe, expect, it } from 'vitest'
import {
  parseFeedbackID,
  parseFeedbackIDs,
  parseRequestID,
  Route,
} from '@/routes/_authed.feedback.customer-requests'

describe('customer requests route search parsing', () => {
  it('normalizes valid feedback ids and drops invalid entries', () => {
    expect(parseFeedbackIDs(' 1, nope, 2.5, -3, 4 ')).toEqual(['1', '4'])
    expect(parseFeedbackIDs('')).toEqual([])
    expect(parseFeedbackID(' 42 ')).toBe('42')
    expect(parseFeedbackID('nope')).toBeUndefined()
    expect(parseFeedbackID('-1')).toBeUndefined()
    expect(parseRequestID(' ABCDEFAB-1234-5678-9ABC-DEF012345678 ')).toBe(
      'abcdefab-1234-5678-9abc-def012345678',
    )
    expect(parseRequestID('not-a-uuid')).toBeUndefined()
  })

  it('validates route search params by type before component parsing', () => {
    const validateSearch = Route.options.validateSearch as (search: Record<string, unknown>) => {
      request_id?: string
      merge_target_id?: string
      promote_feedback_ids?: string
      feedback_id?: string
    }
    expect(
      validateSearch({
        request_id: 'ABCDEFAB-1234-5678-9ABC-DEF012345678',
        merge_target_id: '12345678-1234-1234-1234-1234567890AB',
        promote_feedback_ids: '1,2',
        feedback_id: '42',
      }),
    ).toEqual({
      request_id: 'abcdefab-1234-5678-9abc-def012345678',
      merge_target_id: '12345678-1234-1234-1234-1234567890ab',
      promote_feedback_ids: '1,2',
      feedback_id: '42',
    })
    expect(validateSearch({ promote_feedback_ids: 1, feedback_id: null })).toEqual({
      request_id: undefined,
      merge_target_id: undefined,
      promote_feedback_ids: undefined,
      feedback_id: undefined,
    })
  })
})
