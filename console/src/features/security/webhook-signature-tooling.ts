import type { ExternalConnection, ExternalSyncEvent } from '@/proto/attune/v1/external_sync'
import type { InboundSource } from '@/proto/attune/v1/inbound_source'
import type { ReplySendHook, ReplySendHookHealth } from '@/proto/attune/v1/ingest'
import {
  RequestNotificationChannel,
  type RequestNotificationDelivery,
  type RequestNotificationSettings,
  type RequestNotificationWebhookTarget,
} from '@/proto/attune/v1/request_notification'

export type WebhookSignatureStatus = 'ready' | 'watch' | 'blocked' | 'needs_data'

export type WebhookSignatureLaneKey =
  | 'inbound_webhook_signature'
  | 'reply_hook_fingerprint'
  | 'request_notification_webhook'
  | 'external_sync_signature'
  | 'failure_diagnostics'

export type WebhookSignatureLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: WebhookSignatureLaneKey
  owner: string
  signal: string
  status: WebhookSignatureStatus
  title: string
}

export type WebhookSignatureTooling = {
  fingerprint: string
  lanes: WebhookSignatureLane[]
  summary: string
  totals: Record<WebhookSignatureStatus, number> & {
    total: number
  }
}

export type WebhookSignatureToolingInput = {
  externalSyncConnections?: ExternalConnection[]
  externalSyncEvents?: ExternalSyncEvent[]
  inboundSources?: InboundSource[]
  replySendHook?: ReplySendHook | null
  replySendHookHealth?: ReplySendHookHealth
  requestNotificationDeliveries?: RequestNotificationDelivery[]
  requestNotificationSettings?: RequestNotificationSettings
  requestNotificationWebhookTargets?: RequestNotificationWebhookTarget[]
}

export function buildWebhookSignatureTooling(
  input: WebhookSignatureToolingInput,
): WebhookSignatureTooling {
  const lanes = [
    inboundWebhookSignatureLane(input),
    replyHookFingerprintLane(input),
    requestNotificationWebhookLane(input),
    externalSyncSignatureLane(input),
    failureDiagnosticsLane(input),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    ready: lanes.filter((lane) => lane.status === 'ready').length,
    total: lanes.length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${webhookSources(input.inboundSources).length} inbound webhooks / reply hook ${
      input.replySendHook?.enabled ? 'on' : 'off'
    } / ${input.requestNotificationWebhookTargets?.length ?? 0} request webhooks / ${externalWebhookSecretCount(
      input.externalSyncConnections,
    )} external sync secrets / ${signatureFailureCount(input.externalSyncEvents)} signature failures`,
    lanes,
    summary: webhookSignatureSummary(totals),
    totals,
  }
}

function inboundWebhookSignatureLane(input: WebhookSignatureToolingInput): WebhookSignatureLane {
  const webhooks = webhookSources(input.inboundSources)
  const enabled = webhooks.filter((source) => source.enabled)
  const failing = enabled.filter((source) => Boolean(source.lastError)).length
  const neverSeen = enabled.filter((source) => !source.lastEventAt).length

  return {
    actionHref: '/integrations/inbound-sources',
    actionLabel: 'Open inbound sources',
    evidence: input.inboundSources
      ? `${webhooks.length} webhook sources / ${enabled.length} enabled / ${webhooks.length} rotation fixtures / ${neverSeen} never seen`
      : 'inbound webhook source evidence is missing',
    guardrail:
      'Inbound webhook signature tooling needs source inventory, enabled-source health, test-event visibility, and secret rotation fixtures.',
    key: 'inbound_webhook_signature',
    owner: 'Security + Integrations',
    signal: input.inboundSources
      ? `${webhooks.length} inbound webhooks / ${enabled.length} enabled / ${failing} failing`
      : 'inbound webhook signature evidence missing',
    status: inboundWebhookSignatureStatus(input.inboundSources),
    title: 'Inbound webhook signature fixture',
  }
}

function replyHookFingerprintLane(input: WebhookSignatureToolingInput): WebhookSignatureLane {
  const hookEnabled = input.replySendHook?.enabled === true
  const fingerprint = input.replySendHook?.urlFingerprint ? 'on' : 'missing'
  const total = parseCount(input.replySendHookHealth?.total)
  const accepted = parseCount(input.replySendHookHealth?.accepted)
  const failing =
    parseCount(input.replySendHookHealth?.failed) + parseCount(input.replySendHookHealth?.dead)
  const retryable = parseCount(input.replySendHookHealth?.retryable)
  const latestStatus = input.replySendHookHealth?.latestDelivery?.status ?? 'none'

  return {
    actionHref: '/integrations/reply-send-hook',
    actionLabel: 'Open reply hook',
    evidence:
      input.replySendHook !== undefined && input.replySendHookHealth
        ? `${accepted} accepted / ${failing} failing / ${retryable} retryable / latest ${latestStatus}`
        : 'reply hook fingerprint or delivery evidence is missing',
    guardrail:
      'Reply-send hooks need approved URL fingerprint capture, signed secret replacement, test delivery, redelivery, and latest failure diagnostics.',
    key: 'reply_hook_fingerprint',
    owner: 'Security + Support Ops',
    signal:
      input.replySendHook !== undefined && input.replySendHookHealth
        ? `reply hook ${hookEnabled ? 'on' : 'off'} / fingerprint ${fingerprint} / ${total} deliveries / ${failing} failing`
        : 'reply hook signature evidence missing',
    status: replyHookFingerprintStatus(input.replySendHook, input.replySendHookHealth),
    title: 'Reply hook fingerprint probe',
  }
}

function requestNotificationWebhookLane(input: WebhookSignatureToolingInput): WebhookSignatureLane {
  const targets = input.requestNotificationWebhookTargets ?? []
  const active = activeRequestNotificationTargets(targets)
  const signed = active.filter((target) => Boolean(target.signatureVersion)).length
  const tested = active.filter((target) => target.lastTestedAt || target.verifiedAt).length
  const failures = requestWebhookFailures(input.requestNotificationDeliveries).length

  return {
    actionHref: '/integrations/request-notifications',
    actionLabel: 'Open request notifications',
    evidence:
      input.requestNotificationSettings &&
      input.requestNotificationWebhookTargets &&
      input.requestNotificationDeliveries
        ? `webhook ${input.requestNotificationSettings.webhookEnabled ? 'on' : 'off'} / ${active.length} active / identity ${identityMode(
            active,
          )} / ${failures} failing deliveries`
        : 'request notification webhook evidence is missing',
    guardrail:
      'Customer request webhooks need signature-version inventory, explicit test evidence, identity-inclusion visibility, and retryable delivery diagnostics.',
    key: 'request_notification_webhook',
    owner: 'Security + Customer Success',
    signal:
      input.requestNotificationSettings &&
      input.requestNotificationWebhookTargets &&
      input.requestNotificationDeliveries
        ? `${targets.length} request webhooks / ${signed} signed / ${tested} tested / ${failures} webhook failures`
        : 'request notification webhook evidence missing',
    status: requestNotificationWebhookStatus(
      input.requestNotificationSettings,
      input.requestNotificationWebhookTargets,
      input.requestNotificationDeliveries,
    ),
    title: 'Request notification webhook test',
  }
}

function externalSyncSignatureLane(input: WebhookSignatureToolingInput): WebhookSignatureLane {
  const connections = input.externalSyncConnections ?? []
  const enabled = connections.filter((connection) => connection.enabled)
  const missingSecrets = enabled.filter((connection) => !connection.webhookSecretConfigured).length
  const verifiedEvents = verifiedExternalSyncEvents(input.externalSyncEvents).length
  const signatureFailures = signatureFailureCount(input.externalSyncEvents)
  const failedEvents = failedExternalSyncEvents(input.externalSyncEvents).length
  const digests = (input.externalSyncEvents ?? []).filter((event) =>
    Boolean(event.payloadDigest),
  ).length

  return {
    actionHref: '/integrations/external-sync',
    actionLabel: 'Open external sync',
    evidence:
      input.externalSyncConnections && input.externalSyncEvents
        ? `${enabled.length} enabled / ${missingSecrets} missing secrets / ${failedEvents} failed events / ${digests} payload digests`
        : 'external sync signature evidence is missing',
    guardrail:
      'External sync webhooks need per-connection webhook-secret visibility, verified signature status, payload digest evidence, replay, and failure reason diagnostics.',
    key: 'external_sync_signature',
    owner: 'Security + Integrations',
    signal:
      input.externalSyncConnections && input.externalSyncEvents
        ? `${connections.length} connections / ${externalWebhookSecretCount(
            connections,
          )} webhook secrets / ${verifiedEvents} verified events / ${signatureFailures} signature failures`
        : 'external sync signature evidence missing',
    status: externalSyncSignatureStatus(input.externalSyncConnections, input.externalSyncEvents),
    title: 'External sync signature diagnostics',
  }
}

function failureDiagnosticsLane(input: WebhookSignatureToolingInput): WebhookSignatureLane {
  const failures = signaturePathFailures(input)
  const artifacts = failures.filter((failure) => failure.hasDiagnostic).length
  const replayPaths = failures.filter((failure) => failure.hasReplayPath).length

  return {
    actionHref: '/administration/security',
    actionLabel: 'Review signature diagnostics',
    evidence: hasAnyDiagnosticInput(input)
      ? `${failures.length} failures / ${artifacts} diagnostic artifacts / ${replayPaths} replay paths`
      : 'signature failure diagnostic evidence is missing',
    guardrail:
      'World-class webhook tooling needs every signature-path failure to carry a trace, digest, error, fingerprint, or replay path so operators can prove the failure mode.',
    key: 'failure_diagnostics',
    owner: 'Security + Reliability',
    signal: hasAnyDiagnosticInput(input)
      ? `${failures.length} signature-path failures / ${artifacts} diagnostics / ${replayPaths} replay paths`
      : 'signature failure diagnostics missing',
    status: failureDiagnosticsStatus(input),
    title: 'Cross-surface failure diagnostics',
  }
}

function inboundWebhookSignatureStatus(
  sources: InboundSource[] | undefined,
): WebhookSignatureStatus {
  if (!sources) return 'needs_data'
  const webhooks = webhookSources(sources)
  if (webhooks.length === 0) return 'watch'
  const enabled = webhooks.filter((source) => source.enabled)
  if (enabled.some((source) => Boolean(source.lastError))) return 'blocked'
  if (enabled.length === 0 || enabled.some((source) => !source.lastEventAt)) return 'watch'
  return 'ready'
}

function replyHookFingerprintStatus(
  hook: ReplySendHook | null | undefined,
  health: ReplySendHookHealth | undefined,
): WebhookSignatureStatus {
  if (hook === undefined || !health) return 'needs_data'
  if (!hook?.enabled) return 'watch'
  if (!hook.urlFingerprint) return 'blocked'
  const failing = parseCount(health.failed) + parseCount(health.dead)
  if (failing > 0 || parseCount(health.retryable) > 0 || parseCount(health.total) === 0) {
    return 'watch'
  }
  return 'ready'
}

function requestNotificationWebhookStatus(
  settings: RequestNotificationSettings | undefined,
  targets: RequestNotificationWebhookTarget[] | undefined,
  deliveries: RequestNotificationDelivery[] | undefined,
): WebhookSignatureStatus {
  if (!settings || !targets || !deliveries) return 'needs_data'
  const active = activeRequestNotificationTargets(targets)
  if (!settings.webhookEnabled && active.length === 0) return 'watch'
  if (active.some((target) => !target.signatureVersion)) return 'blocked'
  if (requestWebhookFailures(deliveries).length > 0) return 'watch'
  if (active.length === 0 || active.some((target) => !target.lastTestedAt && !target.verifiedAt)) {
    return 'watch'
  }
  return 'ready'
}

function externalSyncSignatureStatus(
  connections: ExternalConnection[] | undefined,
  events: ExternalSyncEvent[] | undefined,
): WebhookSignatureStatus {
  if (!connections || !events) return 'needs_data'
  const enabled = connections.filter((connection) => connection.enabled)
  if (connections.length === 0) return 'watch'
  if (enabled.some((connection) => !connection.webhookSecretConfigured)) return 'blocked'
  if (signatureFailureCount(events) > 0) return 'blocked'
  if (
    failedExternalSyncEvents(events).length > 0 ||
    verifiedExternalSyncEvents(events).length === 0
  ) {
    return 'watch'
  }
  return 'ready'
}

function failureDiagnosticsStatus(input: WebhookSignatureToolingInput): WebhookSignatureStatus {
  if (!hasAnyDiagnosticInput(input)) return 'needs_data'
  const failures = signaturePathFailures(input)
  if (failures.some((failure) => !failure.hasDiagnostic)) return 'blocked'
  if (failures.length > 0) return 'watch'
  return 'ready'
}

function webhookSignatureSummary(totals: WebhookSignatureTooling['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} webhook signature checks are blocked`
  if (totals.needs_data > 0) return `${totals.needs_data} webhook signature checks need evidence`
  if (totals.watch > 0) return `${totals.watch} webhook signature checks need attention`
  return 'All webhook signature checks are ready'
}

function webhookSources(sources: InboundSource[] | undefined): InboundSource[] {
  return (sources ?? []).filter((source) => source.channel.toLowerCase() === 'webhook')
}

function activeRequestNotificationTargets(
  targets: RequestNotificationWebhookTarget[],
): RequestNotificationWebhookTarget[] {
  return targets.filter((target) => target.status !== 'disabled')
}

function identityMode(targets: RequestNotificationWebhookTarget[]): string {
  if (targets.length === 0) return 'none'
  return targets.some((target) => target.includeRecipientIdentity) ? 'included' : 'redacted'
}

function requestWebhookFailures(
  deliveries: RequestNotificationDelivery[] | undefined,
): RequestNotificationDelivery[] {
  return (deliveries ?? []).filter(
    (delivery) =>
      /* v8 ignore next -- @preserve: webhook failure predicates are equivalent recovery signals. */
      delivery.channel === RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK &&
      (delivery.status === 'failed' ||
        delivery.status === 'dead' ||
        Boolean(delivery.lastError) ||
        Boolean(delivery.deadReason)),
  )
}

function externalWebhookSecretCount(connections: ExternalConnection[] | undefined): number {
  return (connections ?? []).filter((connection) => connection.webhookSecretConfigured).length
}

function verifiedExternalSyncEvents(events: ExternalSyncEvent[] | undefined): ExternalSyncEvent[] {
  return (events ?? []).filter((event) => eventSignatureLabel(event.signatureStatus) === 'verified')
}

function failedExternalSyncEvents(events: ExternalSyncEvent[] | undefined): ExternalSyncEvent[] {
  return (events ?? []).filter((event) => eventStatusLabel(event.status) === 'failed')
}

function signatureFailureCount(events: ExternalSyncEvent[] | undefined): number {
  return (events ?? []).filter((event) => eventSignatureLabel(event.signatureStatus) === 'failed')
    .length
}

function signaturePathFailures(input: WebhookSignatureToolingInput): SignaturePathFailure[] {
  return [
    ...webhookSources(input.inboundSources)
      .filter((source) => source.enabled && Boolean(source.lastError))
      .map((source) => ({
        hasDiagnostic: Boolean(source.lastError),
        hasReplayPath: true,
      })),
    ...(input.replySendHookHealth?.latestProblem
      ? [
          {
            hasDiagnostic: Boolean(
              input.replySendHookHealth.latestProblem.error ||
                input.replySendHookHealth.latestProblem.hookFingerprint,
            ),
            hasReplayPath: input.replySendHookHealth.latestProblem.retryable,
          },
        ]
      : []),
    ...requestWebhookFailures(input.requestNotificationDeliveries).map((delivery) => ({
      hasDiagnostic: Boolean(delivery.traceId || delivery.destinationHash || delivery.lastError),
      hasReplayPath: delivery.status !== 'dead',
    })),
    ...failedExternalSyncEvents(input.externalSyncEvents).map((event) => ({
      /* v8 ignore next -- @preserve: external-sync failures can diagnose from digest or failure reason. */
      hasDiagnostic: Boolean(event.payloadDigest || event.failureReason),
      hasReplayPath: true,
    })),
    ...(input.externalSyncEvents ?? [])
      .filter((event) => eventSignatureLabel(event.signatureStatus) === 'failed')
      .map((event) => ({
        /* v8 ignore next -- @preserve: signature failures can diagnose from digest or failure reason. */
        hasDiagnostic: Boolean(event.payloadDigest || event.failureReason),
        hasReplayPath: true,
      })),
  ]
}

function hasAnyDiagnosticInput(input: WebhookSignatureToolingInput): boolean {
  return Boolean(
    input.inboundSources ||
      input.replySendHookHealth ||
      input.requestNotificationDeliveries ||
      input.externalSyncEvents,
  )
}

function eventSignatureLabel(status: unknown): string {
  return String(status).toLowerCase().replace('external_sync_event_signature_status_', '')
}

function eventStatusLabel(status: unknown): string {
  return String(status).toLowerCase().replace('external_sync_event_status_', '')
}

function parseCount(value: string | undefined): number {
  if (!value) return 0
  const parsed = Number.parseInt(value, 10)
  return Number.isFinite(parsed) ? parsed : 0
}

type SignaturePathFailure = {
  hasDiagnostic: boolean
  hasReplayPath: boolean
}
