import type { ApiKey } from '@/proto/attune/v1/api_key'
import type { AuditLogEntry } from '@/proto/attune/v1/audit'
import type { ExternalConnection, ExternalSyncEvent } from '@/proto/attune/v1/external_sync'
import type { GdprOperationsResponse } from '@/proto/attune/v1/gdpr'
import type { InboundSource } from '@/proto/attune/v1/inbound_source'
import type { ReplySendHook, ReplySendHookHealth } from '@/proto/attune/v1/ingest'
import type { LLMChannel } from '@/proto/attune/v1/llm_config'
import type { Member } from '@/proto/attune/v1/member'
import type { NotifyTarget } from '@/proto/attune/v1/notify_target'
import {
  ModerationState,
  type ModerationSubject,
  PublicAccessMode,
  type PublicVisibilityPolicy,
} from '@/proto/attune/v1/public_visibility'
import {
  RequestNotificationChannel,
  type RequestNotificationDelivery,
  type RequestNotificationSettings,
  type RequestNotificationWebhookTarget,
} from '@/proto/attune/v1/request_notification'
import type { PreflightCheckResult } from '@/proto/attune/v1/system'
import type { AuthModeResponse } from './api/auth-mode'
import type { BreakGlassLockout, BreakGlassToken } from './api/breakglass'

export type SecurityIncidentRunbookStatus = 'ready' | 'watch' | 'blocked' | 'needs_data'

export type SecurityIncidentRunbookLaneKey =
  | 'credential_compromise'
  | 'webhook_signature_incident'
  | 'access_identity_incident'
  | 'public_privacy_incident'
  | 'customer_notification_recovery'

export type SecurityIncidentRunbookLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: SecurityIncidentRunbookLaneKey
  owner: string
  signal: string
  status: SecurityIncidentRunbookStatus
  title: string
}

export type SecurityIncidentRunbook = {
  fingerprint: string
  lanes: SecurityIncidentRunbookLane[]
  summary: string
  totals: Record<SecurityIncidentRunbookStatus, number> & {
    total: number
  }
}

export type SecurityIncidentRunbookInput = {
  apiKeys?: ApiKey[]
  auditEntries?: AuditLogEntry[]
  authMode?: AuthModeResponse
  externalSyncConnections?: ExternalConnection[]
  externalSyncEvents?: ExternalSyncEvent[]
  gdprOperations?: GdprOperationsResponse
  inboundSources?: InboundSource[]
  llmChannels?: LLMChannel[]
  lockouts?: BreakGlassLockout[]
  members?: Member[]
  moderationSubjects?: ModerationSubject[]
  notifyTargets?: NotifyTarget[]
  preflightChecks?: PreflightCheckResult[]
  publicVisibilityPolicy?: PublicVisibilityPolicy
  replySendHook?: ReplySendHook | null
  replySendHookHealth?: ReplySendHookHealth
  requestNotificationDeliveries?: RequestNotificationDelivery[]
  requestNotificationSettings?: RequestNotificationSettings
  requestNotificationWebhookTargets?: RequestNotificationWebhookTarget[]
  tokens?: BreakGlassToken[]
}

export function buildSecurityIncidentRunbook(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbook {
  const lanes = [
    credentialCompromiseLane(input),
    webhookSignatureIncidentLane(input),
    accessIdentityIncidentLane(input),
    publicPrivacyIncidentLane(input),
    customerNotificationRecoveryLane(input),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    ready: lanes.filter((lane) => lane.status === 'ready').length,
    total: lanes.length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    /* v8 ignore next -- @preserve: public-surface count defaults to zero when privacy evidence is absent. */
    fingerprint: `${activeApiKeys(input.apiKeys).length} API keys / ${signatureFailureCount(
      input.externalSyncEvents,
    )} signature failures / ${activeAdminCount(input.members)} admins / ${publicSurfaceCount(
      input.publicVisibilityPolicy,
    )} public surfaces / ${notificationFailureCount(input)} notification failures`,
    lanes,
    summary: securityIncidentRunbookSummary(totals),
    totals,
  }
}

function credentialCompromiseLane(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbookLane {
  const activeKeys = activeApiKeys(input.apiKeys)
  const managedLlm = managedLlmChannels(input.llmChannels)
  const keysetChecks = secretPreflightChecks(input.preflightChecks)
  const breakglass = activeBreakglassTokens(input.tokens).length
  const graceKeys = activeKeys.filter((key) => Boolean(key.gracePeriodEndsAt)).length
  const neverExpires = activeKeys.filter((key) => !key.expiresAt).length

  return {
    actionHref: '/integrations/api-keys',
    actionLabel: 'Review credential runbook',
    evidence:
      input.apiKeys && input.llmChannels && input.preflightChecks && input.tokens
        ? `${activeKeys.length} active keys / ${managedLlm.length} managed LLM keys / ${keysetChecks.length} keyset checks / ${breakglass} break-glass / ${graceKeys} grace keys / ${neverExpires} never expires`
        : 'credential compromise runbook evidence is missing',
    guardrail:
      'Credential compromise response needs keyset decryptability proof, active API-key inventory, managed LLM credentials, break-glass access, and old-key retirement evidence.',
    key: 'credential_compromise',
    owner: 'Security + Platform',
    signal:
      input.apiKeys && input.llmChannels && input.preflightChecks && input.tokens
        ? `${activeKeys.length} API keys / ${managedLlm.length} managed LLM keys / ${keysetChecks.length} keyset checks / ${breakglass} break-glass`
        : 'credential incident evidence missing',
    status: credentialCompromiseStatus(input),
    title: 'Credential compromise response',
  }
}

function webhookSignatureIncidentLane(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbookLane {
  const inboundFailures = inboundWebhookFailures(input.inboundSources)
  const replyFailures = replyHookFailures(input.replySendHookHealth)
  const requestFailures = requestWebhookFailures(input.requestNotificationDeliveries).length
  const externalFailures = failedExternalSyncEvents(input.externalSyncEvents).length
  const signatureFailures = signatureFailureCount(input.externalSyncEvents)

  return {
    actionHref: '/administration/security',
    actionLabel: 'Review webhook incident path',
    evidence: hasWebhookIncidentInputs(input)
      ? `${inboundFailures} inbound failures / ${replyFailures} reply failures / ${requestFailures} request webhook failures / ${externalFailures} external event failures`
      : 'webhook incident runbook evidence is missing',
    guardrail:
      'Webhook signature incidents need failing-source visibility, URL fingerprints, request webhook delivery diagnostics, external payload digests, and replay paths.',
    key: 'webhook_signature_incident',
    owner: 'Security + Integrations',
    signal: hasWebhookIncidentInputs(input)
      ? `${signatureFailures} signature failures / ${replyFailures} reply failures / ${requestFailures} request webhook failures / ${externalFailures} external failures`
      : 'webhook incident evidence missing',
    status: webhookSignatureIncidentStatus(input),
    title: 'Webhook signature incident',
  }
}

function accessIdentityIncidentLane(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbookLane {
  const activeMembers = activeMemberRows(input.members)
  const admins = activeAdminCount(input.members)
  const idpManaged = activeMembers.filter((member) => member.roleSource === 'idp').length
  const memberAudit = memberAuditEntries(input.auditEntries).length
  const lockouts = input.lockouts?.length ?? 0

  return {
    actionHref: '/administration/security',
    actionLabel: 'Review access incident path',
    evidence:
      input.authMode && input.members && input.tokens && input.lockouts && input.auditEntries
        ? `${input.authMode.mode} auth / ${admins} admins / ${idpManaged} IdP-managed / ${lockouts} lockouts / ${memberAudit} member audit events`
        : 'access incident runbook evidence is missing',
    guardrail:
      'Access incidents need SSO mode, break-glass continuity, last-admin protection, IdP/SCIM ownership, lockout visibility, and member audit export evidence.',
    key: 'access_identity_incident',
    owner: 'Security + IT',
    signal:
      input.authMode && input.members && input.tokens && input.lockouts && input.auditEntries
        ? `${input.authMode.mode} / ${admins} admins / ${idpManaged} IdP-managed / ${memberAudit} member audit events`
        : 'access incident evidence missing',
    status: accessIdentityIncidentStatus(input),
    title: 'Access and identity incident',
  }
}

function publicPrivacyIncidentLane(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbookLane {
  const surfaces = publicSurfaceCount(input.publicVisibilityPolicy)
  const pendingModeration = (input.moderationSubjects ?? []).filter(
    (subject) => subject.state === ModerationState.MODERATION_STATE_PENDING,
  ).length
  const blockedModeration = terminalModerationSubjects(input.moderationSubjects).length
  /* v8 ignore next -- @preserve: missing GDPR operations default scheduled deletes to zero in display-only evidence. */
  const scheduledDeletes = input.gdprOperations?.scheduledDeleteCount ?? 0

  return {
    actionHref: '/integrations/public-visibility',
    actionLabel: 'Review privacy incident path',
    evidence:
      input.publicVisibilityPolicy && input.moderationSubjects && input.gdprOperations
        ? `${surfaces} public surfaces / ${pendingModeration} pending moderation / ${blockedModeration} blocked / ${scheduledDeletes} scheduled deletes`
        : 'public privacy incident runbook evidence is missing',
    guardrail:
      'Public privacy incidents need public-surface inventory, moderation gates, identity exposure rules, GDPR delete/export state, legal-hold support, and audit evidence.',
    key: 'public_privacy_incident',
    owner: 'Security + Product',
    signal:
      input.publicVisibilityPolicy && input.moderationSubjects && input.gdprOperations
        ? `${surfaces} public surfaces / ${pendingModeration} pending / legal hold ${onOff(
            input.gdprOperations.legalHoldSupported,
          )} / ${scheduledDeletes} scheduled deletes`
        : 'public privacy incident evidence missing',
    status: publicPrivacyIncidentStatus(input),
    title: 'Public privacy incident',
  }
}

function customerNotificationRecoveryLane(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbookLane {
  const notifyFailures = notifyTargetFailures(input.notifyTargets)
  const requestFailures = requestNotificationFailures(input.requestNotificationDeliveries)
  const replyFailures = replyHookFailures(input.replySendHookHealth)
  const enabledNotifyTargets = (input.notifyTargets ?? []).filter((target) => !target.disabled)
  const webhookEnabled = input.requestNotificationSettings?.webhookEnabled === true
  const emailEnabled = input.requestNotificationSettings?.emailEnabled === true

  return {
    actionHref: '/integrations/request-notifications',
    actionLabel: 'Review customer recovery',
    evidence:
      input.notifyTargets &&
      input.requestNotificationSettings &&
      input.requestNotificationDeliveries &&
      input.replySendHookHealth
        ? `${enabledNotifyTargets.length} outbound targets / email ${onOff(
            emailEnabled,
          )} / webhook ${onOff(webhookEnabled)} / ${requestFailures} request notification failures / ${replyFailures} reply failures`
        : 'customer notification recovery evidence is missing',
    guardrail:
      'Customer-facing security incidents need outbound target health, request notification recovery, reply-hook redelivery, traceable failures, and customer notification evidence.',
    key: 'customer_notification_recovery',
    owner: 'Security + Customer Success',
    signal:
      input.notifyTargets &&
      input.requestNotificationSettings &&
      input.requestNotificationDeliveries &&
      input.replySendHookHealth
        ? `${enabledNotifyTargets.length} outbound targets / ${requestFailures} request failures / ${replyFailures} reply failures / ${notifyFailures} target failures`
        : 'customer notification incident evidence missing',
    status: customerNotificationRecoveryStatus(input),
    title: 'Customer notification recovery',
  }
}

function credentialCompromiseStatus(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbookStatus {
  if (!input.apiKeys || !input.llmChannels || !input.preflightChecks || !input.tokens) {
    return 'needs_data'
  }
  const activeKeys = activeApiKeys(input.apiKeys)
  const secretChecks = secretPreflightChecks(input.preflightChecks)
  if (secretChecks.length === 0 || secretChecks.some((check) => check.status === 'fail')) {
    return 'blocked'
  }
  if (activeKeys.some((key) => key.scopes.length === 0)) return 'blocked'
  if (enabledLlmChannels(input.llmChannels).some(needsApiKeyButMissing)) return 'blocked'
  if (
    activeKeys.length === 0 ||
    activeBreakglassTokens(input.tokens).length === 0 ||
    activeKeys.some((key) => !key.expiresAt || key.gracePeriodEndsAt) ||
    secretChecks.some((check) => check.status === 'warn') ||
    enabledLlmChannels(input.llmChannels).some((channel) => !channel.lastTestedAt)
  ) {
    return 'watch'
  }
  return 'ready'
}

function webhookSignatureIncidentStatus(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbookStatus {
  if (!hasWebhookIncidentInputs(input)) return 'needs_data'
  if (
    inboundWebhookFailures(input.inboundSources) > 0 ||
    signatureFailureCount(input.externalSyncEvents) > 0 ||
    enabledExternalSyncConnections(input.externalSyncConnections).some(
      (connection) => !connection.webhookSecretConfigured,
    ) ||
    activeRequestTargets(input.requestNotificationWebhookTargets).some(
      (target) => !target.signatureVersion,
    )
  ) {
    return 'blocked'
  }
  if (
    replyHookFailures(input.replySendHookHealth) > 0 ||
    parseCount(input.replySendHookHealth?.retryable) > 0 ||
    requestWebhookFailures(input.requestNotificationDeliveries).length > 0 ||
    failedExternalSyncEvents(input.externalSyncEvents).length > 0 ||
    activeRequestTargets(input.requestNotificationWebhookTargets).some(
      (target) => !target.lastTestedAt && !target.verifiedAt,
    )
  ) {
    return 'watch'
  }
  return 'ready'
}

function accessIdentityIncidentStatus(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbookStatus {
  if (
    !input.authMode ||
    !input.members ||
    !input.tokens ||
    !input.lockouts ||
    !input.auditEntries
  ) {
    return 'needs_data'
  }
  const admins = activeAdminCount(input.members)
  const activeBreakglass = activeBreakglassTokens(input.tokens).length
  if (activeMemberRows(input.members).length === 0 || admins < 1) return 'blocked'
  if (input.authMode.mode === 'sso_only' && activeBreakglass === 0) return 'blocked'
  if (
    admins < 2 ||
    input.authMode.mode === 'hybrid' ||
    input.lockouts.length > 0 ||
    activeMemberRows(input.members).some((member) => member.roleSource !== 'idp') ||
    memberAuditEntries(input.auditEntries).length === 0
  ) {
    return 'watch'
  }
  return 'ready'
}

function publicPrivacyIncidentStatus(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbookStatus {
  if (!input.publicVisibilityPolicy || !input.moderationSubjects || !input.gdprOperations) {
    return 'needs_data'
  }
  const policy = input.publicVisibilityPolicy
  if (
    policy.portalAccessMode === PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC &&
    (policy.defaultRequestState === ModerationState.MODERATION_STATE_APPROVED ||
      policy.defaultCommentState === ModerationState.MODERATION_STATE_APPROVED)
  ) {
    return 'blocked'
  }
  if (terminalModerationSubjects(input.moderationSubjects).length > 0) {
    return 'blocked'
  }
  if (
    publicSurfaceCount(policy) > 0 ||
    !policy.hidePublicTimestamps ||
    policy.showSubmitterDisplay ||
    input.moderationSubjects.some(
      (subject) => subject.state === ModerationState.MODERATION_STATE_PENDING,
    ) ||
    !input.gdprOperations.legalHoldSupported ||
    input.gdprOperations.scheduledDeleteCount > 0
  ) {
    return 'watch'
  }
  return 'ready'
}

function customerNotificationRecoveryStatus(
  input: SecurityIncidentRunbookInput,
): SecurityIncidentRunbookStatus {
  if (
    !input.notifyTargets ||
    !input.requestNotificationSettings ||
    !input.requestNotificationDeliveries ||
    !input.replySendHookHealth
  ) {
    return 'needs_data'
  }
  const enabledTargets = input.notifyTargets.filter((target) => !target.disabled)
  if (
    enabledTargets.some((target) => !target.url.startsWith('https://')) ||
    (!input.requestNotificationSettings.emailEnabled &&
      !input.requestNotificationSettings.webhookEnabled &&
      enabledTargets.length === 0)
  ) {
    return 'blocked'
  }
  if (
    notifyTargetFailures(input.notifyTargets) > 0 ||
    requestNotificationFailures(input.requestNotificationDeliveries) > 0 ||
    replyHookFailures(input.replySendHookHealth) > 0 ||
    parseCount(input.replySendHookHealth.retryable) > 0
  ) {
    return 'watch'
  }
  return 'ready'
}

function securityIncidentRunbookSummary(totals: SecurityIncidentRunbook['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} security incident runbook lanes are blocked`
  if (totals.needs_data > 0) return `${totals.needs_data} security incident lanes need evidence`
  if (totals.watch > 0) return `${totals.watch} security incident lanes need rehearsal`
  return 'security incident runbook evidence is ready'
}

function hasWebhookIncidentInputs(input: SecurityIncidentRunbookInput): boolean {
  return Boolean(
    input.inboundSources &&
      input.replySendHook !== undefined &&
      input.replySendHookHealth &&
      input.requestNotificationDeliveries &&
      input.requestNotificationWebhookTargets &&
      input.externalSyncConnections &&
      input.externalSyncEvents,
  )
}

function activeApiKeys(keys: ApiKey[] | undefined): ApiKey[] {
  return (keys ?? []).filter((key) => key.isActive)
}

function activeBreakglassTokens(tokens: BreakGlassToken[] | undefined): BreakGlassToken[] {
  return (tokens ?? []).filter((token) => token.status === 'valid' || token.status === 'expiring')
}

function activeMemberRows(members: Member[] | undefined): Member[] {
  return (members ?? []).filter((member) => member.memberType !== 'invite')
}

function activeAdminCount(members: Member[] | undefined): number {
  return activeMemberRows(members).filter((member) => member.role === 'admin').length
}

function memberAuditEntries(entries: AuditLogEntry[] | undefined): AuditLogEntry[] {
  return (entries ?? []).filter((entry) =>
    ['member.invite', 'member.remove', 'member.update_role'].includes(entry.action),
  )
}

function publicSurfaceCount(policy: PublicVisibilityPolicy | undefined): number {
  if (!policy) return 0
  return [
    policy.roadmapEnabled,
    policy.changelogEnabled,
    policy.requestsEnabled,
    policy.commentsEnabled,
  ].filter(Boolean).length
}

function secretPreflightChecks(checks: PreflightCheckResult[] | undefined): PreflightCheckResult[] {
  return (checks ?? []).filter((check) => check.name.startsWith('secrets:'))
}

function enabledLlmChannels(channels: LLMChannel[] | undefined): LLMChannel[] {
  return (channels ?? []).filter((channel) => channel.status === 'enabled')
}

function managedLlmChannels(channels: LLMChannel[] | undefined): LLMChannel[] {
  return enabledLlmChannels(channels).filter((channel) => Boolean(channel.credentialKeyId))
}

function needsApiKeyButMissing(channel: LLMChannel): boolean {
  return channel.authMode === 'bearer' && !channel.hasApiKey && !channel.credentialKeyId
}

function inboundWebhookFailures(sources: InboundSource[] | undefined): number {
  return (sources ?? []).filter(
    (source) => source.channel.toLowerCase() === 'webhook' && source.enabled && source.lastError,
  ).length
}

function replyHookFailures(health: ReplySendHookHealth | undefined): number {
  return parseCount(health?.failed) + parseCount(health?.dead)
}

function requestWebhookFailures(
  deliveries: RequestNotificationDelivery[] | undefined,
): RequestNotificationDelivery[] {
  return (deliveries ?? []).filter(
    (delivery) =>
      delivery.channel === RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK &&
      (delivery.status === 'failed' ||
        delivery.status === 'dead' ||
        Boolean(delivery.lastError) ||
        Boolean(delivery.deadReason)),
  )
}

function requestNotificationFailures(
  deliveries: RequestNotificationDelivery[] | undefined,
): number {
  return (deliveries ?? []).filter(
    (delivery) =>
      /* v8 ignore next -- @preserve: request notification failure predicates are equivalent recovery signals. */
      delivery.status === 'failed' ||
      delivery.status === 'dead' ||
      Boolean(delivery.lastError) ||
      Boolean(delivery.deadReason),
  ).length
}

function notifyTargetFailures(targets: NotifyTarget[] | undefined): number {
  return (targets ?? []).filter(
    (target) => !target.disabled && (target.lastFailureAt || target.lastError),
  ).length
}

function activeRequestTargets(
  targets: RequestNotificationWebhookTarget[] | undefined,
): RequestNotificationWebhookTarget[] {
  /* v8 ignore next -- @preserve: missing request webhook inventory normalizes to no active targets. */
  return (targets ?? []).filter((target) => target.status !== 'disabled')
}

function terminalModerationSubjects(
  subjects: ModerationSubject[] | undefined,
): ModerationSubject[] {
  return (subjects ?? []).filter((subject) =>
    [
      ModerationState.MODERATION_STATE_HIDDEN,
      ModerationState.MODERATION_STATE_REJECTED,
      ModerationState.MODERATION_STATE_SPAM,
    ].includes(subject.state),
  )
}

function enabledExternalSyncConnections(
  connections: ExternalConnection[] | undefined,
): ExternalConnection[] {
  /* v8 ignore next -- @preserve: missing external-sync inventory normalizes to no enabled connections. */
  return (connections ?? []).filter((connection) => connection.enabled)
}

function failedExternalSyncEvents(events: ExternalSyncEvent[] | undefined): ExternalSyncEvent[] {
  return (events ?? []).filter((event) => eventStatusLabel(event.status) === 'failed')
}

function signatureFailureCount(events: ExternalSyncEvent[] | undefined): number {
  return (events ?? []).filter((event) => eventSignatureLabel(event.signatureStatus) === 'failed')
    .length
}

function notificationFailureCount(input: SecurityIncidentRunbookInput): number {
  return (
    notifyTargetFailures(input.notifyTargets) +
    requestNotificationFailures(input.requestNotificationDeliveries) +
    replyHookFailures(input.replySendHookHealth)
  )
}

function eventSignatureLabel(status: unknown): string {
  return String(status).toLowerCase().replace('external_sync_event_signature_status_', '')
}

function eventStatusLabel(status: unknown): string {
  return String(status).toLowerCase().replace('external_sync_event_status_', '')
}

function onOff(value: boolean): string {
  return value ? 'on' : 'off'
}

function parseCount(value: string | undefined): number {
  if (!value) return 0
  const parsed = Number.parseInt(value, 10)
  /* v8 ignore next -- @preserve: malformed count strings normalize to zero defensively. */
  return Number.isFinite(parsed) ? parsed : 0
}
