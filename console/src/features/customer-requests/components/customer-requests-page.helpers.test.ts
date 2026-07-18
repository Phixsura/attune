import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  customerRequestPageTestables,
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
  CustomerRequestSort,
  CustomerRequestStatus,
  CustomerRequestVisibility,
  SortDirection,
} from '@/proto/attune/v1/customer_request'
import type { Member } from '@/proto/attune/v1/member'

const t = (key: string) => key

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

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

  it('builds owner filter options with member and selected-id fallbacks', () => {
    const invite = {
      id: 'invite-1',
      memberType: 'invite',
      email: 'pending@example.com',
      userId: 'pending',
    } as Member
    const member = {
      id: 'member-1',
      memberType: 'tenant_user',
      email: '',
      userId: 'ops-user',
    } as Member

    const options = customerRequestPageTestables.ownerFilterOptions(
      [
        {
          id: 'request-1',
          owner: {
            id: 'owner-1',
            memberType: 'tenant_user',
            email: '',
            userId: 'owner-user',
            role: 'owner',
          },
        },
        {
          id: 'request-2',
          owner: {
            id: 'member-1',
            memberType: 'tenant_user',
            email: 'ignored@example.com',
            userId: '',
            role: 'owner',
          },
        },
      ] as never,
      [invite, member],
      'missing-owner',
    )

    expect(options).toEqual([
      { id: 'missing-owner', label: 'missing-owner' },
      { id: 'member-1', label: 'ops-user' },
      { id: 'owner-1', label: 'owner-user' },
    ])
    expect(
      customerRequestPageTestables.ownerLabel({
        id: 'owner-id',
        memberType: 'tenant_user',
        userId: '',
        email: '',
        role: 'owner',
      }),
    ).toBe('owner-id')
    expect(customerRequestPageTestables.memberLabel({ id: 'member-id' } as Member)).toBe(
      'member-id',
    )
  })

  it('round-trips saved view state defaults and explicit filters', () => {
    expect(customerRequestPageTestables.savedViewStateToFilters()).toEqual(
      customerRequestPageTestables.DEFAULT_FILTERS,
    )

    const savedState = customerRequestPageTestables.filtersToSavedViewState({
      q: '  renewal ',
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
      ownerMemberId: 'owner-1',
      visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL,
      sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE,
      direction: SortDirection.SORT_DIRECTION_ASC,
      feedbackId: '42',
    })

    expect(savedState).toMatchObject({
      q: 'renewal',
      status: [CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED],
      priority: [CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH],
      ownerMemberId: 'owner-1',
      visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL,
      sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE,
      direction: SortDirection.SORT_DIRECTION_ASC,
      feedbackId: '42',
    })
    expect(
      customerRequestPageTestables.savedViewStateToFilters({
        q: '',
        status: [],
        priority: [],
        visibility: undefined as unknown as CustomerRequestVisibility,
        sort: undefined as unknown as CustomerRequestSort,
        direction: undefined as unknown as SortDirection,
      }),
    ).toMatchObject(customerRequestPageTestables.DEFAULT_FILTERS)
  })

  it('normalizes scoring settings, dates, and idempotency keys', () => {
    const form = customerRequestPageTestables.scoringSettingsToForm({
      tenantId: 'tenant-1',
      priorityNoneWeight: 1,
      priorityLowWeight: 2,
      priorityMediumWeight: 3,
      priorityHighWeight: 4,
      priorityUrgentWeight: 5,
      feedbackWeight: 6,
      feedbackCap: 7,
      customerWeight: 8,
      customerCap: 9,
      accountWeight: 10,
      accountCap: 11,
      voteWeight: 12,
      voteCap: 13,
      revenueCentsPerPoint: '',
      revenueCap: 14,
      updatedBy: '',
      updatedAt: '',
    })
    expect(form.revenueCentsPerPoint).toBe('100000')
    expect(
      customerRequestPageTestables.scoringFormToRequest({
        ...form,
        revenueCentsPerPoint: '0',
      }).revenueCentsPerPoint,
    ).toBe('100000')
    expect(
      customerRequestPageTestables.scoringFormToRequest({
        ...form,
        revenueCentsPerPoint: '250000',
      }).revenueCentsPerPoint,
    ).toBe('250000')
    expect(customerRequestPageTestables.normalizeInteger('12')).toBe(12)
    expect(customerRequestPageTestables.normalizeInteger('-1')).toBe(0)
    expect(customerRequestPageTestables.normalizeInteger('bad')).toBe(0)
    expect(customerRequestPageTestables.normalizeIntegerString(' 42 ', '100')).toBe('42')
    expect(customerRequestPageTestables.normalizeIntegerString('abc', '100')).toBe('100')
    expect(customerRequestPageTestables.formatDate(undefined)).toBe('')
    expect(customerRequestPageTestables.formatDate('not-a-date')).toBe('not-a-date')
    expect(customerRequestPageTestables.formatDate('2026-07-07T00:00:00Z')).toContain('2026')

    vi.stubGlobal('crypto', { randomUUID: () => '12345678-1234-1234-1234-123456789abc' })
    expect(customerRequestPageTestables.makeIdempotencyKey()).toBe('cr_123456781234123412341234')
    vi.stubGlobal('crypto', {})
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
    expect(customerRequestPageTestables.makeIdempotencyKey()).toMatch(/^cr_[a-z0-9]+$/)
  })
})
