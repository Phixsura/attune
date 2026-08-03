import type { ApiKey } from '@/proto/attune/v1/api_key'
import type { InboundSource } from '@/proto/attune/v1/inbound_source'
import type { ReplySendHook, ReplySendHookHealth } from '@/proto/attune/v1/ingest'
import type { LLMChannel } from '@/proto/attune/v1/llm_config'
import type { NotifyTarget } from '@/proto/attune/v1/notify_target'
import type { PreflightCheckResult } from '@/proto/attune/v1/system'

export type KeyRotationStatus = 'ready' | 'watch' | 'blocked' | 'needs_data'

export type KeyRotationLaneKey =
  | 'tink_keyset_runtime'
  | 'api_key_rotation'
  | 'inbound_webhook_rotation'
  | 'outbound_secret_boundary'
  | 'llm_provider_secret_rotation'

export type KeyRotationLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: KeyRotationLaneKey
  owner: string
  signal: string
  status: KeyRotationStatus
  title: string
}

export type KeyRotationReadiness = {
  fingerprint: string
  lanes: KeyRotationLane[]
  summary: string
  totals: Record<KeyRotationStatus, number> & {
    total: number
  }
}

export type KeyRotationReadinessInput = {
  apiKeys?: ApiKey[]
  inboundSources?: InboundSource[]
  llmChannels?: LLMChannel[]
  notifyTargets?: NotifyTarget[]
  preflightChecks?: PreflightCheckResult[]
  replySendHook?: ReplySendHook | null
  replySendHookHealth?: ReplySendHookHealth
}

export function buildKeyRotationReadiness(input: KeyRotationReadinessInput): KeyRotationReadiness {
  const lanes = [
    tinkKeysetRuntimeLane(input),
    apiKeyRotationLane(input),
    inboundWebhookRotationLane(input),
    outboundSecretBoundaryLane(input),
    llmProviderSecretRotationLane(input),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    ready: lanes.filter((lane) => lane.status === 'ready').length,
    total: lanes.length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${activeApiKeys(input.apiKeys).length} active API keys / ${
      webhookSources(input.inboundSources).length
    } webhook sources / ${managedLlmChannels(input.llmChannels).length} managed LLM keys / ${outboundTargetCount(
      input.notifyTargets,
      input.replySendHook,
    )} outbound targets / ${secretPreflightChecks(input.preflightChecks).length} keyset checks`,
    lanes,
    summary: keyRotationSummary(totals),
    totals,
  }
}

function tinkKeysetRuntimeLane(input: KeyRotationReadinessInput): KeyRotationLane {
  const checks = secretPreflightChecks(input.preflightChecks)
  const passing = checks.filter((check) => check.status === 'pass').length
  const failing = checks.filter((check) => check.status === 'fail').length
  const warning = checks.filter((check) => check.status === 'warn').length

  return {
    actionHref: '/administration/system-readiness',
    actionLabel: 'Open system preflight',
    evidence: input.preflightChecks
      ? `${checks.length} secret preflight checks / ${passing} passing / ${failing} failing`
      : 'system preflight evidence is missing',
    guardrail:
      'Tink keyset rotation needs runtime proof that encryption checks run, the primary key is usable, and decryptability evidence is available.',
    key: 'tink_keyset_runtime',
    owner: 'Security + Platform',
    signal: input.preflightChecks
      ? `${checks.length} keyset checks / ${passing} passing / ${warning} warning`
      : 'Tink keyset preflight missing',
    status: tinkKeysetRuntimeStatus(input.preflightChecks),
    title: 'Tink keyset runtime proof',
  }
}

function apiKeyRotationLane(input: KeyRotationReadinessInput): KeyRotationLane {
  const active = activeApiKeys(input.apiKeys)
  const expiring = active.filter((key) => Boolean(key.expiresAt)).length
  const grace = active.filter((key) => Boolean(key.gracePeriodEndsAt)).length
  const neverExpires = active.filter((key) => !key.expiresAt).length
  const weakBoundaries = active.filter(hasWeakApiKeyBoundary).length

  return {
    actionHref: '/integrations/api-keys',
    actionLabel: 'Review API keys',
    evidence: input.apiKeys
      ? `${active.length} active / ${expiring} expiring / ${grace} in grace / ${weakBoundaries} weak boundaries`
      : 'API key inventory evidence is missing',
    guardrail:
      'Customer-facing API keys need visible expiry, grace windows, least-privilege scopes, IP or rate boundaries, and old-key retirement evidence.',
    key: 'api_key_rotation',
    owner: 'Security + Developer Platform',
    signal: input.apiKeys
      ? `${active.length} active keys / ${expiring} expiring / ${grace} in grace / ${neverExpires} never expires`
      : 'API key rotation evidence missing',
    status: apiKeyRotationStatus(input.apiKeys),
    title: 'API key rotation inventory',
  }
}

function inboundWebhookRotationLane(input: KeyRotationReadinessInput): KeyRotationLane {
  const webhooks = webhookSources(input.inboundSources)
  const enabled = webhooks.filter((source) => source.enabled)
  const failing = enabled.filter((source) => Boolean(source.lastError)).length
  const neverSeen = enabled.filter((source) => !source.lastEventAt).length

  return {
    actionHref: '/integrations/inbound-sources',
    actionLabel: 'Review inbound sources',
    evidence: input.inboundSources
      ? `${webhooks.length} webhook sources / ${enabled.length} enabled / ${failing} failing / ${neverSeen} never seen`
      : 'inbound webhook source evidence is missing',
    guardrail:
      'Inbound webhook secrets need a visible source inventory, explicit rotation action, healthy enabled sources, and test-event evidence.',
    key: 'inbound_webhook_rotation',
    owner: 'Security + Integrations',
    signal: input.inboundSources
      ? `${webhooks.length} webhook sources / ${enabled.length} enabled / ${failing} failing`
      : 'inbound webhook rotation evidence missing',
    status: inboundWebhookRotationStatus(input.inboundSources),
    title: 'Inbound webhook secret rotation',
  }
}

function outboundSecretBoundaryLane(input: KeyRotationReadinessInput): KeyRotationLane {
  const enabledNotifyTargets = enabledOutboundTargets(input.notifyTargets)
  const nonHttps = enabledNotifyTargets.filter(
    (target) => !target.url.startsWith('https://'),
  ).length
  const failingNotifyTargets = enabledNotifyTargets.filter(
    (target) => Boolean(target.lastFailureAt) || Boolean(target.lastError),
  ).length
  const hookEnabled = input.replySendHook?.enabled === true
  const deliveryFailures =
    parseCount(input.replySendHookHealth?.failed) + parseCount(input.replySendHookHealth?.dead)
  const retryable = parseCount(input.replySendHookHealth?.retryable)

  return {
    actionHref: '/integrations/notify-targets',
    actionLabel: 'Review outbound secrets',
    evidence:
      input.notifyTargets && input.replySendHook !== undefined && input.replySendHookHealth
        ? `${enabledNotifyTargets.length} notify targets / reply hook ${
            hookEnabled ? 'on' : 'off'
          } / ${failingNotifyTargets} failing / ${nonHttps} non-HTTPS / ${deliveryFailures} delivery failures / ${retryable} retryable`
        : 'outbound target and reply-hook evidence is missing',
    guardrail:
      'Outbound webhook and reply-hook secrets need HTTPS boundaries, rotation-safe replacement paths, delivery health, and failure recovery evidence.',
    key: 'outbound_secret_boundary',
    owner: 'Security + Customer Success',
    signal:
      input.notifyTargets && input.replySendHook !== undefined && input.replySendHookHealth
        ? `${enabledNotifyTargets.length} notify targets / reply hook ${
            hookEnabled ? 'on' : 'off'
          } / ${deliveryFailures} delivery failures`
        : 'outbound secret boundary evidence missing',
    status: outboundSecretBoundaryStatus(
      input.notifyTargets,
      input.replySendHook,
      input.replySendHookHealth,
    ),
    title: 'Outbound secret boundary',
  }
}

function llmProviderSecretRotationLane(input: KeyRotationReadinessInput): KeyRotationLane {
  const enabled = enabledLlmChannels(input.llmChannels)
  const managed = managedLlmChannels(input.llmChannels)
  const tested = enabled.filter((channel) => Boolean(channel.lastTestedAt)).length
  const failing = enabled.filter(isFailingLlmChannel).length
  const missingKey = enabled.filter(needsApiKeyButMissing).length

  return {
    actionHref: '/administration/llm-config',
    actionLabel: 'Review LLM providers',
    evidence: input.llmChannels
      ? `${enabled.length} enabled / ${managed.length} managed keys / ${tested} tested / ${missingKey} missing keys`
      : 'LLM provider credential evidence is missing',
    guardrail:
      'LLM provider secrets need managed credential ids, write-only key replacement, recent test evidence, and failing channel visibility.',
    key: 'llm_provider_secret_rotation',
    owner: 'Security + AI Platform',
    signal: input.llmChannels
      ? `${enabled.length} LLM channels / ${managed.length} managed keys / ${tested} tested / ${failing} failing`
      : 'LLM provider secret evidence missing',
    status: llmProviderSecretRotationStatus(input.llmChannels),
    title: 'LLM provider secret rotation',
  }
}

function tinkKeysetRuntimeStatus(checks: PreflightCheckResult[] | undefined): KeyRotationStatus {
  if (!checks) return 'needs_data'
  const secretChecks = secretPreflightChecks(checks)
  if (secretChecks.length === 0) return 'needs_data'
  if (secretChecks.some((check) => check.status === 'fail')) return 'blocked'
  if (secretChecks.some((check) => check.status === 'warn')) return 'watch'
  return 'ready'
}

function apiKeyRotationStatus(keys: ApiKey[] | undefined): KeyRotationStatus {
  if (!keys) return 'needs_data'
  const active = activeApiKeys(keys)
  if (active.length === 0) return 'watch'
  if (active.some((key) => key.scopes.length === 0)) return 'blocked'
  if (active.some((key) => !key.expiresAt || key.gracePeriodEndsAt || hasWeakApiKeyBoundary(key))) {
    return 'watch'
  }
  return 'ready'
}

function inboundWebhookRotationStatus(sources: InboundSource[] | undefined): KeyRotationStatus {
  if (!sources) return 'needs_data'
  const webhooks = webhookSources(sources)
  if (webhooks.length === 0) return 'watch'
  const enabled = webhooks.filter((source) => source.enabled)
  if (enabled.some((source) => Boolean(source.lastError))) return 'blocked'
  if (enabled.length === 0 || enabled.some((source) => !source.lastEventAt)) return 'watch'
  return 'ready'
}

function outboundSecretBoundaryStatus(
  notifyTargets: NotifyTarget[] | undefined,
  replySendHook: ReplySendHook | null | undefined,
  replySendHookHealth: ReplySendHookHealth | undefined,
): KeyRotationStatus {
  if (!notifyTargets || replySendHook === undefined || !replySendHookHealth) return 'needs_data'
  const enabledTargets = enabledOutboundTargets(notifyTargets)
  const hookEnabled = replySendHook?.enabled === true
  if (enabledTargets.some((target) => !target.url.startsWith('https://'))) return 'blocked'
  const deliveryFailures =
    parseCount(replySendHookHealth.failed) + parseCount(replySendHookHealth.dead)
  if (
    enabledTargets.some((target) => target.lastFailureAt || target.lastError) ||
    deliveryFailures > 0 ||
    parseCount(replySendHookHealth.retryable) > 0
  ) {
    return 'watch'
  }
  if (enabledTargets.length === 0 && !hookEnabled) return 'watch'
  return 'ready'
}

function llmProviderSecretRotationStatus(channels: LLMChannel[] | undefined): KeyRotationStatus {
  if (!channels) return 'needs_data'
  const enabled = enabledLlmChannels(channels)
  if (enabled.length === 0) return 'watch'
  if (enabled.some(needsApiKeyButMissing)) return 'blocked'
  if (
    enabled.some(isFailingLlmChannel) ||
    enabled.some((channel) => channel.authMode === 'bearer' && !channel.credentialKeyId) ||
    enabled.some((channel) => !channel.lastTestedAt)
  ) {
    return 'watch'
  }
  return 'ready'
}

function keyRotationSummary(totals: KeyRotationReadiness['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} key rotation checks are blocked`
  if (totals.needs_data > 0) return `${totals.needs_data} key rotation checks need evidence`
  if (totals.watch > 0) return `${totals.watch} key rotation checks need attention`
  return 'All key rotation checks are ready'
}

function activeApiKeys(keys: ApiKey[] | undefined): ApiKey[] {
  return (keys ?? []).filter((key) => key.isActive && !key.revokedAt)
}

function hasWeakApiKeyBoundary(key: ApiKey): boolean {
  return key.allowedCidrs.length === 0 || key.rateLimitRpm === undefined
}

function webhookSources(sources: InboundSource[] | undefined): InboundSource[] {
  return (sources ?? []).filter((source) => source.channel.toLowerCase() === 'webhook')
}

function enabledOutboundTargets(targets: NotifyTarget[] | undefined): NotifyTarget[] {
  return (targets ?? []).filter((target) => !target.disabled)
}

function outboundTargetCount(
  notifyTargets: NotifyTarget[] | undefined,
  replySendHook: ReplySendHook | null | undefined,
): number {
  return enabledOutboundTargets(notifyTargets).length + (replySendHook?.enabled === true ? 1 : 0)
}

function enabledLlmChannels(channels: LLMChannel[] | undefined): LLMChannel[] {
  return (channels ?? []).filter((channel) => channel.status === 'enabled')
}

function managedLlmChannels(channels: LLMChannel[] | undefined): LLMChannel[] {
  return enabledLlmChannels(channels).filter(
    (channel) => channel.hasApiKey && Boolean(channel.credentialKeyId),
  )
}

function needsApiKeyButMissing(channel: LLMChannel): boolean {
  return channel.authMode === 'bearer' && !channel.hasApiKey
}

function isFailingLlmChannel(channel: LLMChannel): boolean {
  const status = channel.lastTestStatus.toLowerCase()
  return (
    Boolean(channel.lastError) || status === 'fail' || status === 'failed' || status === 'error'
  )
}

function secretPreflightChecks(checks: PreflightCheckResult[] | undefined): PreflightCheckResult[] {
  return (checks ?? []).filter((check) => {
    const haystack = `${check.name} ${check.category} ${check.message} ${
      check.remediation ?? ''
    }`.toLowerCase()
    return (
      check.category === 'encryption' ||
      haystack.includes('tink') ||
      haystack.includes('keyset') ||
      haystack.includes('secret') ||
      haystack.includes('encrypt') ||
      haystack.includes('decrypt')
    )
  })
}

function parseCount(value: string | undefined): number {
  if (!value) return 0
  const parsed = Number.parseInt(value, 10)
  return Number.isFinite(parsed) ? parsed : 0
}
