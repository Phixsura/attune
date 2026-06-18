import type { TFunction } from 'i18next'
import { OutboxFailureKind } from '@/proto/attune/v1/outbox'

// i18n key suffix per OutboxFailureKind enum member. Maps the wire enum
// to a short, human-readable label under outbox_dead.failure_kind.* so
// the operator sees "timeout" / "DNS" / "HTTP 5xx" instead of the raw
// OUTBOX_FAILURE_KIND_TIMEOUT constant.
const FAILURE_KIND_KEY: Record<OutboxFailureKind, string> = {
  [OutboxFailureKind.OUTBOX_FAILURE_KIND_UNSPECIFIED]: 'unspecified',
  [OutboxFailureKind.OUTBOX_FAILURE_KIND_HTTP_4XX]: 'http_4xx',
  [OutboxFailureKind.OUTBOX_FAILURE_KIND_HTTP_5XX]: 'http_5xx',
  [OutboxFailureKind.OUTBOX_FAILURE_KIND_TIMEOUT]: 'timeout',
  [OutboxFailureKind.OUTBOX_FAILURE_KIND_DNS]: 'dns',
  [OutboxFailureKind.OUTBOX_FAILURE_KIND_CONNECTION]: 'connection',
  [OutboxFailureKind.OUTBOX_FAILURE_KIND_TLS]: 'tls',
  [OutboxFailureKind.OUTBOX_FAILURE_KIND_TERMINAL]: 'terminal',
  [OutboxFailureKind.OUTBOX_FAILURE_KIND_OTHER]: 'other',
  [OutboxFailureKind.UNRECOGNIZED]: 'unspecified',
}

// failureKindLabel renders the structured failure classification. Falls
// back to the unspecified label for any value outside the known enum
// (forward-compatible with a backend that adds a kind we don't map yet).
export function failureKindLabel(t: TFunction, kind: OutboxFailureKind): string {
  const suffix = FAILURE_KIND_KEY[kind] ?? 'unspecified'
  return t(`outbox_dead.failure_kind.${suffix}`)
}

// hasFailureKind is true when the row carries a meaningful classification
// (anything other than the unset/unrecognized sentinels). Used to decide
// whether to render the failure-kind badge at all.
export function hasFailureKind(kind: OutboxFailureKind): boolean {
  return (
    kind !== OutboxFailureKind.OUTBOX_FAILURE_KIND_UNSPECIFIED &&
    kind !== OutboxFailureKind.UNRECOGNIZED
  )
}

// httpStatusLabel renders the HTTP response code as "HTTP 503", or null
// when there was no response (0 = none, per the proto contract).
export function httpStatusLabel(httpStatus: number): string | null {
  return httpStatus > 0 ? `HTTP ${httpStatus}` : null
}
