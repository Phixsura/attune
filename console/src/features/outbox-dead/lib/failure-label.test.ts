import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { OutboxFailureKind } from '@/proto/attune/v1/outbox'
import { failureKindLabel, hasFailureKind, httpStatusLabel } from './failure-label'

// Pure mapping logic: the wire enum + http code → readable labels the
// operator table renders. Drives the failure-cell branches and guards
// against a backend adding an enum member we forget to map.

const t = i18n.getFixedT('zh-CN')

describe('failureKindLabel', () => {
  it('maps each known enum member to its localized label', () => {
    expect(failureKindLabel(t, OutboxFailureKind.OUTBOX_FAILURE_KIND_TIMEOUT)).toBe('超时')
    expect(failureKindLabel(t, OutboxFailureKind.OUTBOX_FAILURE_KIND_HTTP_5XX)).toBe('HTTP 5xx')
    expect(failureKindLabel(t, OutboxFailureKind.OUTBOX_FAILURE_KIND_DNS)).toBe('DNS')
    expect(failureKindLabel(t, OutboxFailureKind.OUTBOX_FAILURE_KIND_TLS)).toBe('TLS')
    expect(failureKindLabel(t, OutboxFailureKind.OUTBOX_FAILURE_KIND_CONNECTION)).toBe('连接')
    expect(failureKindLabel(t, OutboxFailureKind.OUTBOX_FAILURE_KIND_TERMINAL)).toBe('终止')
    expect(failureKindLabel(t, OutboxFailureKind.OUTBOX_FAILURE_KIND_HTTP_4XX)).toBe('HTTP 4xx')
    expect(failureKindLabel(t, OutboxFailureKind.OUTBOX_FAILURE_KIND_OTHER)).toBe('其他')
  })

  it('falls back to the unspecified label for unset / unrecognized', () => {
    expect(failureKindLabel(t, OutboxFailureKind.OUTBOX_FAILURE_KIND_UNSPECIFIED)).toBe('未知')
    expect(failureKindLabel(t, OutboxFailureKind.UNRECOGNIZED)).toBe('未知')
    // A value outside the enum (forward-compat) also falls back.
    expect(failureKindLabel(t, 'OUTBOX_FAILURE_KIND_FUTURE' as OutboxFailureKind)).toBe('未知')
  })
})

describe('hasFailureKind', () => {
  it('is false for the unset / unrecognized sentinels', () => {
    expect(hasFailureKind(OutboxFailureKind.OUTBOX_FAILURE_KIND_UNSPECIFIED)).toBe(false)
    expect(hasFailureKind(OutboxFailureKind.UNRECOGNIZED)).toBe(false)
  })

  it('is true for a meaningful classification', () => {
    expect(hasFailureKind(OutboxFailureKind.OUTBOX_FAILURE_KIND_TIMEOUT)).toBe(true)
    expect(hasFailureKind(OutboxFailureKind.OUTBOX_FAILURE_KIND_HTTP_4XX)).toBe(true)
  })
})

describe('httpStatusLabel', () => {
  it('renders a positive code as "HTTP <n>"', () => {
    expect(httpStatusLabel(503)).toBe('HTTP 503')
    expect(httpStatusLabel(404)).toBe('HTTP 404')
  })

  it('returns null when there was no response (0)', () => {
    expect(httpStatusLabel(0)).toBeNull()
  })
})
