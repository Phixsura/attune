import { describe, expect, it } from 'vitest'
import { isTerminalFailure, MAX_ENRICHMENT_ATTEMPTS } from './enrichment-utils'

describe('enrichment-utils', () => {
  describe('MAX_ENRICHMENT_ATTEMPTS', () => {
    it('is 5', () => {
      expect(MAX_ENRICHMENT_ATTEMPTS).toBe(5)
    })
  })

  describe('isTerminalFailure', () => {
    it('returns true when status is failed, attempts >= 5, and no next retry', () => {
      expect(
        isTerminalFailure({
          enrichmentStatus: 'failed',
          enrichmentAttempts: 5,
          enrichmentNextRetryAt: null,
        }),
      ).toBe(true)
    })

    it('returns true when attempts exceed max', () => {
      expect(
        isTerminalFailure({
          enrichmentStatus: 'failed',
          enrichmentAttempts: 10,
          enrichmentNextRetryAt: null,
        }),
      ).toBe(true)
    })

    it('returns false when status is not failed', () => {
      expect(
        isTerminalFailure({
          enrichmentStatus: 'success',
          enrichmentAttempts: 5,
          enrichmentNextRetryAt: null,
        }),
      ).toBe(false)
    })

    it('returns false when status is pending', () => {
      expect(
        isTerminalFailure({
          enrichmentStatus: 'pending',
          enrichmentAttempts: 0,
          enrichmentNextRetryAt: null,
        }),
      ).toBe(false)
    })

    it('returns false when attempts < 5', () => {
      expect(
        isTerminalFailure({
          enrichmentStatus: 'failed',
          enrichmentAttempts: 3,
          enrichmentNextRetryAt: null,
        }),
      ).toBe(false)
    })

    it('returns false when next retry is scheduled', () => {
      expect(
        isTerminalFailure({
          enrichmentStatus: 'failed',
          enrichmentAttempts: 5,
          enrichmentNextRetryAt: '2026-06-22T12:00:00Z',
        }),
      ).toBe(false)
    })

    it('handles undefined enrichmentAttempts as 0', () => {
      expect(
        isTerminalFailure({
          enrichmentStatus: 'failed',
          enrichmentAttempts: undefined,
          enrichmentNextRetryAt: null,
        }),
      ).toBe(false)
    })

    it('handles null enrichmentNextRetryAt correctly', () => {
      expect(
        isTerminalFailure({
          enrichmentStatus: 'failed',
          enrichmentAttempts: 5,
          enrichmentNextRetryAt: null,
        }),
      ).toBe(true)
    })

    it('handles undefined enrichmentNextRetryAt correctly', () => {
      expect(
        isTerminalFailure({
          enrichmentStatus: 'failed',
          enrichmentAttempts: 5,
          enrichmentNextRetryAt: undefined,
        }),
      ).toBe(true)
    })
  })
})
