import { describe, expect, it } from 'vitest'
import {
  deliveryHealthLabel,
  formatMoney,
  parseFeedbackIDs,
  parseMoneyCents,
  priorityLabel,
  statusLabel,
  supporterLabel,
  syncStateLabel,
} from '@/features/customer-requests/components/customer-requests-page'
import {
  CustomerRequestDeliveryHealth,
  CustomerRequestIssueSyncState,
  CustomerRequestPriority,
  CustomerRequestStatus,
} from '@/proto/attune/v1/customer_request'

const t = (key: string) => key

describe('customer request page helpers', () => {
  it('maps status, priority, delivery, and sync enums to labels', () => {
    expect(
      [
        CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED,
        CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_IN_PROGRESS,
        CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_SHIPPED,
        CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_CANCELLED,
        CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
      ].map((status) => statusLabel(t, status)),
    ).toEqual([
      'customer_requests.statuses.planned',
      'customer_requests.statuses.in_progress',
      'customer_requests.statuses.shipped',
      'customer_requests.statuses.cancelled',
      'customer_requests.statuses.open',
    ])
    expect(
      [
        CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_LOW,
        CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_MEDIUM,
        CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
        CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT,
        CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_NONE,
      ].map((priority) => priorityLabel(t, priority)),
    ).toEqual([
      'customer_requests.priorities.low',
      'customer_requests.priorities.medium',
      'customer_requests.priorities.high',
      'customer_requests.priorities.urgent',
      'customer_requests.priorities.none',
    ])
    expect(
      [
        CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_FAILED,
        CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_STALE,
        CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_PENDING,
        CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_SYNCED,
        CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_MANUAL,
        CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_NO_LINKS,
      ].map((health) => deliveryHealthLabel(health, t)),
    ).toEqual([
      'customer_requests.delivery_health_states.failed',
      'customer_requests.delivery_health_states.stale',
      'customer_requests.delivery_health_states.pending',
      'customer_requests.delivery_health_states.synced',
      'customer_requests.delivery_health_states.manual',
      'customer_requests.delivery_health_states.no_links',
    ])
    expect(
      [
        CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_PENDING,
        CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
        CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_STALE,
        CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_FAILED,
        CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_MANUAL,
      ].map((state) => syncStateLabel(state, t)),
    ).toEqual([
      'customer_requests.sync_states.pending',
      'customer_requests.sync_states.synced',
      'customer_requests.sync_states.stale',
      'customer_requests.sync_states.failed',
      'customer_requests.sync_states.manual',
    ])
  })

  it('falls back through supporter labels', () => {
    expect(
      supporterLabel({
        subjectDisplay: 'Display',
        subjectKey: 'subject',
        subjectHash: 'hash',
        accountDisplay: 'Account',
        accountKey: 'acct',
      }),
    ).toBe('Display')
    expect(
      supporterLabel({
        subjectDisplay: '',
        subjectKey: 'subject',
        subjectHash: 'hash',
        accountDisplay: 'Account',
        accountKey: 'acct',
      }),
    ).toBe('subject')
    expect(
      supporterLabel({
        subjectDisplay: '',
        subjectKey: '',
        subjectHash: 'hash',
        accountDisplay: 'Account',
        accountKey: 'acct',
      }),
    ).toBe('hash')
    expect(
      supporterLabel({
        subjectDisplay: '',
        subjectKey: '',
        subjectHash: '',
        accountDisplay: 'Account',
        accountKey: 'acct',
      }),
    ).toBe('Account')
    expect(
      supporterLabel({
        subjectDisplay: '',
        subjectKey: '',
        subjectHash: '',
        accountDisplay: '',
        accountKey: 'acct',
      }),
    ).toBe('acct')
  })

  it('parses money, formats money, and parses feedback ids defensively', () => {
    expect(parseMoneyCents('')).toBeUndefined()
    expect(parseMoneyCents('   ')).toBeUndefined()
    expect(parseMoneyCents(' 1200 ')).toBe(1200)
    expect(parseMoneyCents('0')).toBe(0)
    expect(parseMoneyCents('-1')).toBeNull()
    expect(parseMoneyCents('12.5')).toBeNull()
    expect(formatMoney('2400000', 'USD')).toContain('$')
    expect(formatMoney(undefined, '')).toContain('$')
    expect(formatMoney(999, 'USD')).toContain('9.99')
    expect(formatMoney(Number.NaN, 'USD')).toBe('USD 0')
    expect(formatMoney(125000, 'NOT_A_CURRENCY')).toBe('NOT_A_CURRENCY 1250')
    expect(formatMoney(1200, 'NOT_A_CURRENCY')).toBe('NOT_A_CURRENCY 12.00')
    expect(parseFeedbackIDs(' 1, nope, 2.5, -3, 4 ,, 0 ')).toEqual(['1', '4'])
  })
})
