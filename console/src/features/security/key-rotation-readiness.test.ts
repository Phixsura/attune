import { describe, expect, it } from 'vitest'
import type { ApiKey } from '@/proto/attune/v1/api_key'
import type { InboundSource } from '@/proto/attune/v1/inbound_source'
import type { ReplySendHook, ReplySendHookHealth } from '@/proto/attune/v1/ingest'
import type { LLMChannel } from '@/proto/attune/v1/llm_config'
import type { NotifyTarget } from '@/proto/attune/v1/notify_target'
import type { PreflightCheckResult } from '@/proto/attune/v1/system'
import { buildKeyRotationReadiness } from './key-rotation-readiness'

const apiKeys: ApiKey[] = [
  {
    allowedCidrs: ['203.0.113.0/24'],
    createdAt: '2026-01-01T00:00:00Z',
    environment: 'production',
    expiresAt: '2026-08-15T00:00:00Z',
    id: 'key-new',
    isActive: true,
    keyPrefix: 'att_live_new',
    label: 'production ingest',
    lastUsedAt: '2026-07-01T00:00:00Z',
    rateLimitRpm: 120,
    scopes: ['ingest:write'],
    usageCount: '42',
  },
  {
    allowedCidrs: [],
    createdAt: '2025-01-01T00:00:00Z',
    environment: 'production',
    gracePeriodEndsAt: '2026-07-02T00:00:00Z',
    id: 'key-old',
    isActive: true,
    keyPrefix: 'att_live_old',
    label: 'legacy ingest',
    lastUsedAt: '2026-06-20T00:00:00Z',
    scopes: ['feedback:read'],
    usageCount: '5',
  },
]

const inboundSources: InboundSource[] = [
  {
    channel: 'webhook',
    createdAt: '2026-06-01T00:00:00Z',
    enabled: true,
    id: 'src-webhook',
    lastError: '',
    lastEventAt: '2026-07-01T00:00:00Z',
    lastUid: '0',
    name: 'Product webhook',
    slug: 'product-webhook',
    tenantId: 'tenant-1',
    updatedAt: '2026-07-01T00:00:00Z',
  },
]

const notifyTargets: NotifyTarget[] = [
  {
    audience: 'all',
    createdAt: '2026-07-01T00:00:00Z',
    destinationType: 'raw-webhook',
    disabled: false,
    id: 'notify-1',
    lastError: '',
    timeoutSeconds: 10,
    url: 'https://hooks.example.com/notify',
  },
]

const llmChannels: LLMChannel[] = [
  {
    authMode: 'bearer',
    baseUrl: 'https://api.openai.com/v1',
    createdAt: '2026-06-01T00:00:00Z',
    credentialKeyId: 'tink-key-1',
    hasApiKey: true,
    id: 'llm-1',
    lastError: '',
    lastTestStatus: 'pass',
    lastTestedAt: '2026-07-01T00:00:00Z',
    name: 'OpenAI production',
    priority: 1,
    protocol: 'openai-responses',
    status: 'enabled',
    timeoutSeconds: 30,
    updatedAt: '2026-07-01T00:00:00Z',
    weight: 1,
  },
]

const replySendHook: ReplySendHook = {
  createdAt: '2026-06-01T00:00:00Z',
  enabled: true,
  id: 'hook-1',
  name: 'Reply hook',
  updatedAt: '2026-07-01T00:00:00Z',
  urlFingerprint: 'sha256:reply',
  urlHost: 'hooks.example.com',
}

const replySendHookHealth: ReplySendHookHealth = {
  accepted: '1',
  dead: '0',
  failed: '0',
  pending: '0',
  retryable: '0',
  total: '1',
}

const preflightChecks: PreflightCheckResult[] = [
  {
    category: 'encryption',
    message: 'Tink keyset parsed and primary key available',
    name: 'secrets:tink_keyset',
    remediation: '',
    status: 'pass',
  },
  {
    category: 'backup',
    message: 'Managed secret samples decrypted',
    name: 'secrets:decryptability',
    remediation: '',
    status: 'pass',
  },
]

describe('buildKeyRotationReadiness', () => {
  it('marks all key rotation lanes ready with bounded active assets', () => {
    const readiness = buildKeyRotationReadiness({
      apiKeys: [apiKeys[0]],
      inboundSources,
      llmChannels,
      notifyTargets,
      preflightChecks,
      replySendHook,
      replySendHookHealth,
    })

    expect(readiness.summary).toBe('All key rotation checks are ready')
    expect(readiness.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 5, watch: 0 })
    expect(readiness.lanes.find((lane) => lane.key === 'api_key_rotation')).toMatchObject({
      signal: '1 active keys / 1 expiring / 0 in grace / 0 never expires',
      status: 'ready',
    })
  })

  it('joins keyset, API key, webhook, outbound, and LLM evidence into one decision', () => {
    const readiness = buildKeyRotationReadiness({
      apiKeys,
      inboundSources,
      llmChannels,
      notifyTargets,
      preflightChecks,
      replySendHook,
      replySendHookHealth,
    })

    expect(readiness.fingerprint).toBe(
      '2 active API keys / 1 webhook sources / 1 managed LLM keys / 2 outbound targets / 2 keyset checks',
    )
    expect(readiness.summary).toBe('1 key rotation checks need attention')
    expect(readiness.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 4, watch: 1 })
    expect(readiness.lanes.find((lane) => lane.key === 'tink_keyset_runtime')).toMatchObject({
      signal: '2 keyset checks / 2 passing / 0 warning',
      status: 'ready',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'api_key_rotation')).toMatchObject({
      signal: '2 active keys / 1 expiring / 1 in grace / 1 never expires',
      status: 'watch',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'inbound_webhook_rotation')).toMatchObject({
      signal: '1 webhook sources / 1 enabled / 0 failing',
      status: 'ready',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'outbound_secret_boundary')).toMatchObject({
      signal: '1 notify targets / reply hook on / 0 delivery failures',
      status: 'ready',
    })
    expect(
      readiness.lanes.find((lane) => lane.key === 'llm_provider_secret_rotation'),
    ).toMatchObject({
      signal: '1 LLM channels / 1 managed keys / 1 tested / 0 failing',
      status: 'ready',
    })
  })

  it('keeps Tink readiness at needs-data when preflight has no encryption proof', () => {
    const readiness = buildKeyRotationReadiness({
      apiKeys,
      inboundSources,
      llmChannels,
      notifyTargets,
      preflightChecks: [
        {
          category: 'database',
          message: 'Database reachable',
          name: 'database',
          remediation: '',
          status: 'pass',
        },
      ],
      replySendHook,
      replySendHookHealth,
    })

    expect(readiness.summary).toBe('1 key rotation checks need evidence')
    expect(readiness.lanes.find((lane) => lane.key === 'tink_keyset_runtime')).toMatchObject({
      evidence: '0 secret preflight checks / 0 passing / 0 failing',
      status: 'needs_data',
    })
  })

  it('keeps missing inventories visible as evidence gaps', () => {
    const readiness = buildKeyRotationReadiness({})

    expect(readiness.summary).toBe('5 key rotation checks need evidence')
    expect(readiness.totals).toMatchObject({ blocked: 0, needs_data: 5, ready: 0, watch: 0 })
    expect(readiness.fingerprint).toBe(
      '0 active API keys / 0 webhook sources / 0 managed LLM keys / 0 outbound targets / 0 keyset checks',
    )
  })

  it('watches empty active inventories without hiding available evidence', () => {
    const readiness = buildKeyRotationReadiness({
      apiKeys: [],
      inboundSources: [],
      llmChannels: [],
      notifyTargets: [],
      preflightChecks,
      replySendHook: { ...replySendHook, enabled: false },
      replySendHookHealth,
    })

    expect(readiness.summary).toBe('4 key rotation checks need attention')
    expect(readiness.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 1, watch: 4 })
    expect(readiness.lanes.find((lane) => lane.key === 'outbound_secret_boundary')).toMatchObject({
      signal: '0 notify targets / reply hook off / 0 delivery failures',
      status: 'watch',
    })
  })

  it('prioritizes hard rotation blockers over softer warnings', () => {
    const readiness = buildKeyRotationReadiness({
      apiKeys: [{ ...apiKeys[0], scopes: [] }],
      inboundSources: [{ ...inboundSources[0], lastError: 'signature mismatch' }],
      llmChannels: [
        {
          ...llmChannels[0],
          credentialKeyId: '',
          hasApiKey: false,
          id: 'llm-missing-key',
        },
      ],
      notifyTargets: [{ ...notifyTargets[0], url: 'http://hooks.example.com/notify' }],
      preflightChecks: [{ ...preflightChecks[0], status: 'warn' }],
      replySendHook,
      replySendHookHealth,
    })

    expect(readiness.summary).toBe('4 key rotation checks are blocked')
    expect(readiness.totals).toMatchObject({ blocked: 4, needs_data: 0, ready: 0, watch: 1 })
    expect(readiness.lanes.find((lane) => lane.key === 'tink_keyset_runtime')).toMatchObject({
      signal: '1 keyset checks / 0 passing / 1 warning',
      status: 'watch',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'outbound_secret_boundary')).toMatchObject({
      evidence:
        '1 notify targets / reply hook on / 0 failing / 1 non-HTTPS / 0 delivery failures / 0 retryable',
      status: 'blocked',
    })
  })

  it('watches outbound delivery failures and LLM channels that need credential hardening', () => {
    const readiness = buildKeyRotationReadiness({
      apiKeys: [apiKeys[0]],
      inboundSources,
      llmChannels: [
        {
          ...llmChannels[0],
          credentialKeyId: '',
          hasApiKey: true,
          lastError: '',
          lastTestStatus: 'error',
          lastTestedAt: '',
        },
      ],
      notifyTargets: [{ ...notifyTargets[0], lastError: '500' }],
      preflightChecks,
      replySendHook,
      replySendHookHealth: {
        ...replySendHookHealth,
        dead: '1',
        failed: 'not-a-number',
        retryable: '2',
      },
    })

    expect(readiness.summary).toBe('2 key rotation checks need attention')
    expect(readiness.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 3, watch: 2 })
    expect(readiness.lanes.find((lane) => lane.key === 'outbound_secret_boundary')).toMatchObject({
      signal: '1 notify targets / reply hook on / 1 delivery failures',
      status: 'watch',
    })
    expect(
      readiness.lanes.find((lane) => lane.key === 'llm_provider_secret_rotation'),
    ).toMatchObject({
      signal: '1 LLM channels / 0 managed keys / 0 tested / 1 failing',
      status: 'watch',
    })
  })

  it('blocks failed keyset preflight checks and watches enabled webhooks without event proof', () => {
    const readiness = buildKeyRotationReadiness({
      apiKeys: [apiKeys[0]],
      inboundSources: [{ ...inboundSources[0], lastEventAt: '' }],
      llmChannels,
      notifyTargets,
      preflightChecks: [
        {
          category: 'database',
          message: 'Primary key cannot decrypt managed secret samples.',
          name: 'runtime',
          status: 'fail',
        },
      ],
      replySendHook,
      replySendHookHealth,
    })

    expect(readiness.summary).toBe('1 key rotation checks are blocked')
    expect(readiness.totals).toMatchObject({ blocked: 1, needs_data: 0, ready: 3, watch: 1 })
    expect(readiness.lanes.find((lane) => lane.key === 'tink_keyset_runtime')).toMatchObject({
      signal: '1 keyset checks / 0 passing / 0 warning',
      status: 'blocked',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'inbound_webhook_rotation')).toMatchObject({
      evidence: '1 webhook sources / 1 enabled / 0 failing / 1 never seen',
      status: 'watch',
    })
  })

  it('blocks rotation readiness when bearer LLM channels are enabled without keys', () => {
    const readiness = buildKeyRotationReadiness({
      apiKeys,
      inboundSources,
      llmChannels: [
        {
          ...llmChannels[0],
          credentialKeyId: '',
          hasApiKey: false,
          id: 'llm-missing',
          lastTestedAt: undefined,
        },
      ],
      notifyTargets,
      preflightChecks,
      replySendHook,
      replySendHookHealth,
    })

    expect(readiness.summary).toBe('1 key rotation checks are blocked')
    expect(
      readiness.lanes.find((lane) => lane.key === 'llm_provider_secret_rotation'),
    ).toMatchObject({
      signal: '1 LLM channels / 0 managed keys / 0 tested / 0 failing',
      status: 'blocked',
    })
  })
})
