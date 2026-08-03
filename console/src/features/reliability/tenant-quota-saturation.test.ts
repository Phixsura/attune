import { describe, expect, it } from 'vitest'
import {
  buildTenantQuotaSaturation,
  type TenantQuotaSaturationInput,
} from './tenant-quota-saturation'

type CompleteTenantQuotaSaturationInput = TenantQuotaSaturationInput &
  Required<
    Pick<
      TenantQuotaSaturationInput,
      'apiKeys' | 'gdprOperations' | 'llmUsage' | 'mcpClients' | 'usage'
    >
  >

function completeInput(): CompleteTenantQuotaSaturationInput {
  return {
    apiKeys: [
      {
        id: 'key-1',
        keyPrefix: 'ak_1',
        label: 'limited',
        isActive: true,
        createdAt: '2026-07-01T00:00:00Z',
        scopes: ['ingest:write'],
        allowedCidrs: [],
        usageCount: '40',
        rateLimitRpm: 120,
        environment: 'production',
      },
      {
        id: 'key-2',
        keyPrefix: 'ak_2',
        label: 'unbounded',
        isActive: true,
        createdAt: '2026-07-01T00:00:00Z',
        scopes: ['ingest:write'],
        allowedCidrs: [],
        usageCount: '32',
        environment: 'production',
      },
    ],
    dashboardHref: '/d/tenant',
    deadDeliveryCount: 2,
    gdprOperations: {
      stepUp: {
        satisfied: true,
        passwordAllowed: true,
        method: 'password',
        ttlSeconds: 900,
      },
      exportTtlSeconds: 86400,
      auditRetentionDays: 30,
      auditPruneIntervalSeconds: 3600,
      queuedRequestCount: 1,
      activeRequestCount: 2,
      readyExportCount: 1,
      hashedAuditResidue: true,
      backupsMayRetainUntilRotation: true,
      legalHoldSupported: false,
      deleteGraceWindowSeconds: 3600,
      scheduledDeleteCount: 1,
    },
    inflightDeadDeliveries: 1,
    llmUsage: {
      periodStart: '2026-07-01T00:00:00Z',
      periodEnd: '2026-07-31T23:59:59Z',
      granularity: 'week',
      series: [],
      promptTokens: '12000',
      completionTokens: '4000',
      costUsd: 2.34,
      calls: '20',
      errors: '1',
    },
    mcpClients: [
      {
        id: 'mcp-1',
        name: 'limited-agent',
        redirect_uris: ['http://localhost/callback'],
        scopes: ['mcp:read'],
        tool_policy_mode: 'allow_list',
        rate_limit_rpm: 60,
        rate_limit_burst: 10,
        created_at: '2026-07-01T00:00:00Z',
        created_by: 'admin',
      },
      {
        id: 'mcp-2',
        name: 'unbounded-agent',
        redirect_uris: ['http://localhost/callback'],
        scopes: ['mcp:read'],
        tool_policy_mode: 'legacy_allow_all',
        rate_limit_rpm: null,
        rate_limit_burst: null,
        created_at: '2026-07-01T00:00:00Z',
        created_by: 'admin',
      },
    ],
    retryableDeadDeliveries: 1,
    tenantName: 'Tenant One',
    usage: {
      periodStart: '2026-07-01T00:00:00Z',
      periodEnd: '2026-07-31T23:59:59Z',
      total: '72',
      quota: '100',
      series: [{ bucket: '2026-07-01T00:00:00Z', value: '72' }],
    },
  }
}

function healthyInput(
  overrides: Partial<TenantQuotaSaturationInput> = {},
): TenantQuotaSaturationInput {
  const base = completeInput()
  return {
    ...base,
    apiKeys: base.apiKeys?.map((key) => ({
      ...key,
      rateLimitRpm: 120,
    })),
    deadDeliveryCount: 0,
    gdprOperations: base.gdprOperations
      ? {
          ...base.gdprOperations,
          activeRequestCount: 0,
          queuedRequestCount: 0,
          scheduledDeleteCount: 0,
        }
      : undefined,
    inflightDeadDeliveries: 0,
    llmUsage: base.llmUsage
      ? {
          ...base.llmUsage,
          calls: '20',
          errors: '0',
        }
      : undefined,
    mcpClients: base.mcpClients?.map((client) => ({
      ...client,
      rate_limit_burst: 10,
      rate_limit_rpm: 60,
    })),
    retryableDeadDeliveries: 0,
    usage: base.usage
      ? {
          ...base.usage,
          total: '40',
        }
      : undefined,
    ...overrides,
  }
}

describe('buildTenantQuotaSaturation', () => {
  it('ties ingest, enrichment, MCP, GDPR, and outbox capacity to one tenant boundary', () => {
    const quota = buildTenantQuotaSaturation(completeInput())

    expect(quota.lanes).toHaveLength(5)
    expect(quota.fingerprint).toBe(
      'Tenant One / 2026-07-01T00:00:00Z -> 2026-07-31T23:59:59Z / watch',
    )
    expect(quota.summary).toBe('4 quota lanes need attention')
    expect(quota.totals).toEqual({
      healthy: 1,
      needs_data: 0,
      saturated: 0,
      total: 5,
      watch: 4,
    })
    expect(quota.lanes.find((lane) => lane.key === 'ingest')).toMatchObject({
      signal: '72% used / 1 unbounded active API keys',
      status: 'watch',
    })
    expect(quota.lanes.find((lane) => lane.key === 'mcp')).toMatchObject({
      signal: '1 unbounded / 2 active MCP clients',
      status: 'watch',
    })
    expect(quota.lanes.find((lane) => lane.key === 'outbox')).toMatchObject({
      signal: '40% dead-letter saturation',
      status: 'watch',
    })
  })

  it('marks hard saturation when usage, providers, MCP clients, GDPR, and outbox exceed guardrails', () => {
    const base = completeInput()
    const quota = buildTenantQuotaSaturation({
      ...base,
      deadDeliveryCount: 5,
      gdprOperations: base.gdprOperations
        ? {
            ...base.gdprOperations,
            queuedRequestCount: 4,
            activeRequestCount: 4,
            scheduledDeleteCount: 2,
          }
        : undefined,
      llmUsage: base.llmUsage
        ? {
            ...base.llmUsage,
            calls: '20',
            errors: '2',
          }
        : undefined,
      mcpClients: base.mcpClients?.map((client) => ({
        ...client,
        rate_limit_burst: null,
        rate_limit_rpm: null,
      })),
      usage: base.usage
        ? {
            ...base.usage,
            total: '120',
            quota: '100',
          }
        : undefined,
    })

    expect(quota.summary).toBe('5 quota lanes are saturated')
    expect(quota.totals.saturated).toBe(5)
    expect(quota.worstLaneKey).toBe('ingest')
    expect(quota.worstSaturationPct).toBe(120)
  })

  it('keeps missing quota evidence visible as data gaps', () => {
    const quota = buildTenantQuotaSaturation({
      dashboardHref: '/d/tenant',
      tenantName: '',
    })

    expect(quota.fingerprint).toBe('tenant unknown / current quota window / needs data')
    expect(quota.summary).toBe('5 quota lanes need data')
    expect(quota.totals).toEqual({
      healthy: 0,
      needs_data: 5,
      saturated: 0,
      total: 5,
      watch: 0,
    })
  })

  it('marks all quota boundaries healthy when every guardrail has headroom', () => {
    const quota = buildTenantQuotaSaturation(healthyInput())

    expect(quota.fingerprint).toBe(
      'Tenant One / 2026-07-01T00:00:00Z -> 2026-07-31T23:59:59Z / healthy',
    )
    expect(quota.summary).toBe('tenant quota boundaries are healthy')
    expect(quota.totals).toEqual({
      healthy: 5,
      needs_data: 0,
      saturated: 0,
      total: 5,
      watch: 0,
    })
    expect(quota.worstLaneKey).toBe('ingest')
    expect(quota.worstSaturationPct).toBe(40)
  })

  it('uses the LLM usage window when ingest usage is unavailable', () => {
    const quota = buildTenantQuotaSaturation(
      healthyInput({
        usage: undefined,
      }),
    )

    expect(quota.windowLabel).toBe('2026-07-01T00:00:00Z -> 2026-07-31T23:59:59Z')
    expect(quota.summary).toBe('1 quota lanes need data')
    expect(quota.lanes.find((lane) => lane.key === 'ingest')).toMatchObject({
      status: 'needs_data',
    })
  })

  it('saturates provider error capacity when errors appear without successful calls', () => {
    const quota = buildTenantQuotaSaturation(
      healthyInput({
        llmUsage: {
          ...completeInput().llmUsage,
          calls: '0',
          errors: '1',
        },
      }),
    )

    expect(quota.summary).toBe('1 quota lanes are saturated')
    expect(quota.lanes.find((lane) => lane.key === 'enrichment')).toMatchObject({
      signal: '100% provider-error saturation',
      status: 'saturated',
    })
  })

  it('keeps MCP quota as data-needed when every client is revoked', () => {
    const quota = buildTenantQuotaSaturation(
      healthyInput({
        mcpClients: completeInput().mcpClients?.map((client) => ({
          ...client,
          revoked_at: '2026-07-10T00:00:00Z',
        })),
      }),
    )

    expect(quota.summary).toBe('1 quota lanes need data')
    expect(quota.lanes.find((lane) => lane.key === 'mcp')).toMatchObject({
      signal: '0 unbounded / 0 active MCP clients',
      status: 'needs_data',
    })
  })

  it('watches GDPR workload once it reaches the manual review guardrail', () => {
    const quota = buildTenantQuotaSaturation(
      healthyInput({
        gdprOperations: {
          ...completeInput().gdprOperations,
          activeRequestCount: 2,
          queuedRequestCount: 4,
          scheduledDeleteCount: 2,
        },
      }),
    )

    expect(quota.summary).toBe('1 quota lanes need attention')
    expect(quota.lanes.find((lane) => lane.key === 'gdpr')).toMatchObject({
      signal: '80% GDPR workload saturation',
      status: 'watch',
    })
  })

  it('treats malformed usage counters as missing quota evidence', () => {
    const quota = buildTenantQuotaSaturation(
      healthyInput({
        llmUsage: {
          ...completeInput().llmUsage,
          calls: 'not-a-number',
          periodEnd: '',
          periodStart: '',
        },
        usage: {
          ...completeInput().usage,
          quota: ' ',
          total: 'not-a-number',
        },
      }),
    )

    expect(quota.summary).toBe('2 quota lanes need data')
    expect(quota.lanes.find((lane) => lane.key === 'ingest')).toMatchObject({
      consumptionLabel: 'ingest consumption unknown',
      status: 'needs_data',
    })
    expect(quota.lanes.find((lane) => lane.key === 'enrichment')).toMatchObject({
      consumptionLabel: 'LLM calls unknown',
      status: 'needs_data',
    })
  })
})
