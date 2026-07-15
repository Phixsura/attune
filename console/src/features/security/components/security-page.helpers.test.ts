import { afterEach, describe, expect, it, vi } from 'vitest'
import { securityPageTestables } from './security-page'

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('securityPageTestables', () => {
  it('classifies break-glass token status by revocation, use, and expiry', () => {
    const now = Date.parse('2026-07-05T12:00:00Z')
    vi.useFakeTimers()
    vi.setSystemTime(now)

    expect(
      securityPageTestables.getTokenStatus({
        id: 'token-revoked',
        admin_email: 'admin@example.com',
        allowed_ips: [],
        expires_at: '2026-07-05T13:00:00Z',
        issued_at: '2026-07-05T11:00:00Z',
        issued_by: 'issuer',
        revoked_at: '2026-07-05T11:30:00Z',
        status: 'valid',
      }),
    ).toBe('revoked')

    expect(
      securityPageTestables.getTokenStatus({
        id: 'token-used',
        admin_email: 'admin@example.com',
        allowed_ips: [],
        expires_at: '2026-07-05T13:00:00Z',
        issued_at: '2026-07-05T11:00:00Z',
        issued_by: 'issuer',
        status: 'valid',
        used_at: '2026-07-05T11:45:00Z',
      }),
    ).toBe('used')

    expect(
      securityPageTestables.getTokenStatus({
        id: 'token-expired',
        admin_email: 'admin@example.com',
        allowed_ips: [],
        expires_at: '2026-07-05T11:59:59Z',
        issued_at: '2026-07-05T11:00:00Z',
        issued_by: 'issuer',
        status: 'valid',
      }),
    ).toBe('expired')

    expect(
      securityPageTestables.getTokenStatus({
        id: 'token-expiring',
        admin_email: 'admin@example.com',
        allowed_ips: [],
        expires_at: '2026-07-05T12:30:00Z',
        issued_at: '2026-07-05T11:00:00Z',
        issued_by: 'issuer',
        status: 'valid',
      }),
    ).toBe('expiring')

    expect(
      securityPageTestables.getTokenStatus({
        id: 'token-active',
        admin_email: 'admin@example.com',
        allowed_ips: [],
        expires_at: '2026-07-05T16:30:00Z',
        issued_at: '2026-07-05T11:00:00Z',
        issued_by: 'issuer',
        status: 'valid',
      }),
    ).toBe('active')
  })

  it('formats time remaining across all supported buckets', () => {
    const now = Date.parse('2026-07-05T12:00:00Z')
    vi.useFakeTimers()
    vi.setSystemTime(now)
    const t = (key: string, fallback: string, options?: Record<string, unknown>) =>
      options?.count ? `${key}:${options.count}` : `${key}:${fallback}`

    expect(securityPageTestables.formatTimeRemaining('2026-07-05T11:59:59Z', t)).toBe(
      'security.time.expired:Expired',
    )
    expect(securityPageTestables.formatTimeRemaining('2026-07-05T12:59:00Z', t)).toBe(
      'security.time.minutes:59',
    )
    expect(securityPageTestables.formatTimeRemaining('2026-07-05T14:00:00Z', t)).toBe(
      'security.time.hours:2',
    )
    expect(securityPageTestables.formatTimeRemaining('2026-07-08T12:00:00Z', t)).toBe(
      'security.time.days:3',
    )
  })
})
