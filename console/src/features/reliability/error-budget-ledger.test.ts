import { describe, expect, it } from 'vitest'
import { buildErrorBudgetLedger } from './error-budget-ledger'

function completeInput() {
  return {
    activeApiKeys: 1,
    activeGdpr: 0,
    activeMcpClients: 1,
    authMode: 'hybrid',
    dashboardHref: '/d/tenant',
    deadDeliveryCount: 0,
    inflightDeadDeliveries: 0,
    queuedGdpr: 0,
    readinessStatus: 'pass',
    recoveryStatus: 'pass',
    releaseLifecycleState: 'supported',
    retryableDeadDeliveries: 0,
    scheduledDeletes: 0,
    tenantName: 'Tenant One',
    totalApiKeys: 2,
    totalMcpClients: 2,
  }
}

describe('buildErrorBudgetLedger', () => {
  it('maps every reliability SLO to an auditable error-budget row', () => {
    const ledger = buildErrorBudgetLedger(completeInput())

    expect(ledger.entries).toHaveLength(7)
    expect(ledger.totals).toEqual({
      attention: 0,
      blocked: 0,
      monitored: 7,
      needs_data: 0,
      total: 7,
    })
    expect(ledger.entries.find((entry) => entry.key === 'ingest_service')).toMatchObject({
      alertName: 'AttuneIngestServiceFastBurn',
      budgetAllowanceLabel: '0.10% budget allowance',
      burnRateQuery: 'attune:ingest_service_failure_ratio:ratio5m / 0.001',
      objectiveLabel: '99.9%',
      remainingBudgetQuery:
        'clamp_min(1 - (attune:ingest_service_failure_ratio:ratio6h / 0.001), 0)',
      runbookHref:
        'https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneingestservicefastburn',
    })
    expect(ledger.entries.find((entry) => entry.key === 'enrichment_latency')).toMatchObject({
      budgetAllowanceLabel: '5.00% budget allowance',
      burnRateQuery: '(1 - attune:enrich_success_under_5s:ratio5m) / 0.05',
      objectiveLabel: '95%',
    })
  })

  it('marks rows that need operator attention from current runtime signals', () => {
    const ledger = buildErrorBudgetLedger({
      ...completeInput(),
      activeGdpr: 2,
      deadDeliveryCount: 3,
      inflightDeadDeliveries: 1,
      queuedGdpr: 1,
      readinessStatus: 'warn',
      retryableDeadDeliveries: 2,
      scheduledDeletes: 1,
    })

    expect(ledger.totals.attention).toBeGreaterThanOrEqual(4)
    expect(ledger.entries.find((entry) => entry.key === 'outbox_delivery')).toMatchObject({
      currentSignal: '2 retryable / 1 in-flight / 3 dead',
      status: 'attention',
    })
    expect(ledger.entries.find((entry) => entry.key === 'gdpr_job')).toMatchObject({
      currentSignal: '1 queued / 2 active / 1 scheduled delete',
      status: 'attention',
    })
  })

  it('blocks the ledger when the release lifecycle is blocked', () => {
    const ledger = buildErrorBudgetLedger({
      ...completeInput(),
      releaseLifecycleState: 'blocked',
    })

    expect(ledger.totals).toEqual({
      attention: 0,
      blocked: 7,
      monitored: 0,
      needs_data: 0,
      total: 7,
    })
  })

  it('keeps missing operational inputs visible as data gaps', () => {
    const ledger = buildErrorBudgetLedger({
      dashboardHref: '/d/tenant',
      tenantName: 'Tenant One',
    })

    expect(ledger.totals.needs_data).toBeGreaterThan(0)
    expect(ledger.entries.find((entry) => entry.key === 'oidc_login')).toMatchObject({
      currentSignal: 'auth mode: unknown',
      status: 'needs_data',
    })
  })

  it('blocks readiness-backed budgets when readiness fails', () => {
    const ledger = buildErrorBudgetLedger({
      ...completeInput(),
      readinessStatus: 'fail',
    })

    expect(ledger.totals.blocked).toBe(2)
    expect(ledger.entries.find((entry) => entry.key === 'ingest_service')).toMatchObject({
      currentSignal: 'Tenant One intake readiness: fail',
      status: 'blocked',
    })
    expect(ledger.entries.find((entry) => entry.key === 'enrichment_latency')).toMatchObject({
      currentSignal: 'Tenant One enrichment readiness: fail',
      status: 'blocked',
    })
  })

  it('marks account-access budgets as attention when no active credentials remain', () => {
    const ledger = buildErrorBudgetLedger({
      ...completeInput(),
      activeApiKeys: 0,
      activeMcpClients: 0,
    })

    expect(ledger.totals.attention).toBe(2)
    expect(ledger.entries.find((entry) => entry.key === 'apikey_access')).toMatchObject({
      currentSignal: '0 active / 2 total API keys',
      status: 'attention',
    })
    expect(ledger.entries.find((entry) => entry.key === 'mcp_tool')).toMatchObject({
      currentSignal: '0 active / 2 total MCP clients',
      status: 'attention',
    })
  })

  it('separates failed and skipped GDPR recovery evidence', () => {
    const failed = buildErrorBudgetLedger({
      ...completeInput(),
      recoveryStatus: 'fail',
    })
    const skipped = buildErrorBudgetLedger({
      ...completeInput(),
      recoveryStatus: 'skipped',
    })

    expect(failed.entries.find((entry) => entry.key === 'gdpr_job')).toMatchObject({
      status: 'blocked',
    })
    expect(skipped.entries.find((entry) => entry.key === 'gdpr_job')).toMatchObject({
      status: 'needs_data',
    })
  })
})
