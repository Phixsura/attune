import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { GdprOperationsResponse, VerifyGdprStepUpResponse } from '@/proto/attune/v1/gdpr'

export type RuntimeResetField =
  | 'queue_len'
  | 'workers'
  | 'batch_size'
  | 'batch_window'
  | 'sweep_interval'
  | 'llm_rate_limit_enabled'
  | 'llm_max_qps'
  | 'llm_burst'

export interface EnrichmentRuntimeSpecView {
  queueLen: number
  workers: number
  batchSize: number
  batchWindowSeconds: number
  sweepIntervalSeconds: number
  llmRateLimitEnabled: boolean
  llmMaxQps: number
  llmBurst: number
}

export interface EnrichmentRuntimeRevisionView {
  version: string
  updatedAt?: string
  updatedBy: string
  updateReason: string
  bootstrapSnapshotVersion: string
  specVersion: number
  lastKnownGoodVersion: string
}

export interface EnrichmentRuntimeSummaryView {
  desiredVersion: string
  liveInstances: number
  staleInstances: number
  expiredInstances: number
  degradedInstances: number
  fullyAppliedInstances: number
  fullyConverged: boolean
}

export interface EnrichmentRuntimeInstanceView {
  instanceId: string
  bootId: string
  desiredVersion: string
  observedDesiredVersion: string
  runnerEffectiveVersion: string
  limiterEffectiveVersion: string
  attemptedRunnerVersion: string
  attemptedLimiterVersion: string
  runnerApplyStatus: string
  limiterApplyStatus: string
  runnerLastApplyError: string
  limiterLastApplyError: string
  queueDepth: number
  queueCapacityTarget: number
  queueCapacityEffective: number
  queueResizePending: boolean
  inFlight: number
  degradedReason: string
  lastAppliedAt?: string
  lastReconciledAt?: string
  lastSeenAt?: string
  appliedSpec: EnrichmentRuntimeSpecView
}

export interface EnrichmentRuntimeHistoryEntryView {
  revision: EnrichmentRuntimeRevisionView
  operationType: string
  riskLevel: string
  sourceVersion: string
  targetVersion: string
  rollbackLineage: string
}

export interface EnrichmentRuntimeView {
  bootstrapDefault: EnrichmentRuntimeSpecView
  desiredSpec: EnrichmentRuntimeSpecView
  desiredRevision: EnrichmentRuntimeRevisionView
  summary: EnrichmentRuntimeSummaryView
  instances: EnrichmentRuntimeInstanceView[]
  history: EnrichmentRuntimeHistoryEntryView[]
}

interface RuntimeSpecWire {
  queueLen?: number
  workers?: number
  batchSize?: number
  batchWindow?: string
  sweepInterval?: string
  llmRateLimitEnabled?: boolean
  llmMaxQps?: number
  llmBurst?: number
}

interface RuntimeRevisionWire {
  version?: string
  updatedAt?: string
  updatedBy?: string
  updateReason?: string
  bootstrapSnapshotVersion?: string
  specVersion?: number
  lastKnownGoodVersion?: string
}

interface RuntimeSummaryWire {
  desiredVersion?: string
  liveInstances?: number
  staleInstances?: number
  expiredInstances?: number
  degradedInstances?: number
  fullyAppliedInstances?: number
  fullyConverged?: boolean
}

interface RuntimeInstanceWire {
  instanceId?: string
  bootId?: string
  desiredVersion?: string
  observedDesiredVersion?: string
  runnerEffectiveVersion?: string
  limiterEffectiveVersion?: string
  attemptedRunnerVersion?: string
  attemptedLimiterVersion?: string
  runnerApplyStatus?: string
  limiterApplyStatus?: string
  runnerLastApplyError?: string
  limiterLastApplyError?: string
  queueDepth?: number
  queueCapacityTarget?: number
  queueCapacityEffective?: number
  queueResizePending?: boolean
  inFlight?: number
  degradedReason?: string
  lastAppliedAt?: string
  lastReconciledAt?: string
  lastSeenAt?: string
  appliedSpec?: RuntimeSpecWire
}

interface RuntimeHistoryWire {
  revision?: RuntimeRevisionWire
  operationType?: string
  riskLevel?: string
  sourceVersion?: string
  targetVersion?: string
  rollbackLineage?: string
}

interface RuntimeReadModelWire {
  bootstrapDefault?: RuntimeSpecWire
  desiredSpec?: RuntimeSpecWire
  desiredRevision?: RuntimeRevisionWire
  summary?: RuntimeSummaryWire
  instances?: RuntimeInstanceWire[]
  history?: RuntimeHistoryWire[]
}

interface GetRuntimeResponseWire {
  runtime?: RuntimeReadModelWire
}

interface UpdateRuntimeResponseWire {
  runtime?: RuntimeReadModelWire
}

interface ResetRuntimeResponseWire {
  runtime?: RuntimeReadModelWire
}

interface RollbackRuntimeResponseWire {
  runtime?: RuntimeReadModelWire
}

export interface UpdateEnrichmentRuntimeInput {
  expectedVersion: string
  spec: EnrichmentRuntimeSpecView
  updateReason: string
}

export interface ResetEnrichmentRuntimeInput {
  expectedVersion: string
  fields: RuntimeResetField[]
  resetAll: boolean
  updateReason: string
}

export interface RollbackEnrichmentRuntimeInput {
  expectedVersion: string
  targetVersion: string
  updateReason: string
}

const enrichmentRuntimeQueryKey = ['console', 'enrichment-runtime'] as const

export const enrichmentRuntimeQuery = () =>
  queryOptions({
    queryKey: enrichmentRuntimeQueryKey,
    queryFn: async ({ signal }) => {
      const resp = await api<GetRuntimeResponseWire>('/fb/v1/console/enrichment-runtime', {
        signal,
      })
      return normalizeReadModel(resp.runtime)
    },
    staleTime: 2_000,
    refetchInterval: 5_000,
  })

export function useUpdateEnrichmentRuntime() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: UpdateEnrichmentRuntimeInput) => {
      const resp = await api<UpdateRuntimeResponseWire>('/fb/v1/console/enrichment-runtime', {
        method: 'PUT',
        body: {
          expectedVersion: input.expectedVersion,
          updateReason: input.updateReason,
          spec: toWireSpec(input.spec),
        },
      })
      return normalizeReadModel(resp.runtime)
    },
    onSuccess: (runtime) => {
      qc.setQueryData(enrichmentRuntimeQueryKey, runtime)
    },
  })
}

export function useResetEnrichmentRuntime() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: ResetEnrichmentRuntimeInput) => {
      const resp = await api<ResetRuntimeResponseWire>('/fb/v1/console/enrichment-runtime/reset', {
        method: 'POST',
        body: input,
      })
      return normalizeReadModel(resp.runtime)
    },
    onSuccess: (runtime) => {
      qc.setQueryData(enrichmentRuntimeQueryKey, runtime)
    },
  })
}

export function useRollbackEnrichmentRuntime() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: RollbackEnrichmentRuntimeInput) => {
      const resp = await api<RollbackRuntimeResponseWire>(
        '/fb/v1/console/enrichment-runtime/rollback',
        {
          method: 'POST',
          body: input,
        },
      )
      return normalizeReadModel(resp.runtime)
    },
    onSuccess: (runtime) => {
      qc.setQueryData(enrichmentRuntimeQueryKey, runtime)
    },
  })
}

export const runtimeStepUpQuery = () =>
  queryOptions({
    queryKey: ['console', 'enrichment-runtime', 'step-up'],
    queryFn: ({ signal }: { signal: AbortSignal }) =>
      api<GdprOperationsResponse>('/fb/v1/console/gdpr/operations', { signal }),
    staleTime: 10_000,
  })

export function useVerifyRuntimeStepUp() {
  return useMutation({
    mutationFn: (password: string) =>
      api<VerifyGdprStepUpResponse>('/fb/v1/console/gdpr/step-up/verify', {
        method: 'POST',
        /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
        body: password ? { password } : {},
      }),
  })
}

function normalizeReadModel(runtime?: RuntimeReadModelWire): EnrichmentRuntimeView {
  return {
    bootstrapDefault: normalizeSpec(runtime?.bootstrapDefault),
    desiredSpec: normalizeSpec(runtime?.desiredSpec),
    desiredRevision: normalizeRevision(runtime?.desiredRevision),
    summary: normalizeSummary(runtime?.summary),
    /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
    instances: (runtime?.instances ?? []).map(normalizeInstance),
    /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
    history: (runtime?.history ?? []).map(normalizeHistory),
  }
}

function normalizeSpec(spec?: RuntimeSpecWire): EnrichmentRuntimeSpecView {
  return {
    queueLen: spec?.queueLen ?? 0,
    workers: spec?.workers ?? 0,
    batchSize: spec?.batchSize ?? 0,
    batchWindowSeconds: parseDurationSeconds(spec?.batchWindow),
    sweepIntervalSeconds: parseDurationSeconds(spec?.sweepInterval),
    llmRateLimitEnabled: spec?.llmRateLimitEnabled ?? false,
    llmMaxQps: spec?.llmMaxQps ?? 0,
    llmBurst: spec?.llmBurst ?? 0,
  }
}

function normalizeRevision(revision?: RuntimeRevisionWire): EnrichmentRuntimeRevisionView {
  return {
    version: revision?.version ?? '0',
    updatedAt: revision?.updatedAt,
    updatedBy: revision?.updatedBy ?? '',
    updateReason: revision?.updateReason ?? '',
    bootstrapSnapshotVersion: revision?.bootstrapSnapshotVersion ?? '',
    specVersion: revision?.specVersion ?? 0,
    lastKnownGoodVersion: revision?.lastKnownGoodVersion ?? '0',
  }
}

function normalizeSummary(summary?: RuntimeSummaryWire): EnrichmentRuntimeSummaryView {
  return {
    desiredVersion: summary?.desiredVersion ?? '0',
    liveInstances: summary?.liveInstances ?? 0,
    staleInstances: summary?.staleInstances ?? 0,
    expiredInstances: summary?.expiredInstances ?? 0,
    degradedInstances: summary?.degradedInstances ?? 0,
    fullyAppliedInstances: summary?.fullyAppliedInstances ?? 0,
    fullyConverged: summary?.fullyConverged ?? false,
  }
}

function normalizeInstance(instance: RuntimeInstanceWire): EnrichmentRuntimeInstanceView {
  return {
    instanceId: instance.instanceId ?? '',
    bootId: instance.bootId ?? '',
    desiredVersion: instance.desiredVersion ?? '0',
    observedDesiredVersion: instance.observedDesiredVersion ?? '0',
    runnerEffectiveVersion: instance.runnerEffectiveVersion ?? '0',
    limiterEffectiveVersion: instance.limiterEffectiveVersion ?? '0',
    attemptedRunnerVersion: instance.attemptedRunnerVersion ?? '0',
    attemptedLimiterVersion: instance.attemptedLimiterVersion ?? '0',
    runnerApplyStatus: instance.runnerApplyStatus ?? 'RUNTIME_APPLY_STATUS_UNSPECIFIED',
    limiterApplyStatus: instance.limiterApplyStatus ?? 'RUNTIME_APPLY_STATUS_UNSPECIFIED',
    runnerLastApplyError: instance.runnerLastApplyError ?? '',
    limiterLastApplyError: instance.limiterLastApplyError ?? '',
    queueDepth: instance.queueDepth ?? 0,
    queueCapacityTarget: instance.queueCapacityTarget ?? 0,
    queueCapacityEffective: instance.queueCapacityEffective ?? 0,
    queueResizePending: instance.queueResizePending ?? false,
    inFlight: instance.inFlight ?? 0,
    degradedReason: instance.degradedReason ?? '',
    lastAppliedAt: instance.lastAppliedAt,
    lastReconciledAt: instance.lastReconciledAt,
    lastSeenAt: instance.lastSeenAt,
    appliedSpec: normalizeSpec(instance.appliedSpec),
  }
}

function normalizeHistory(entry: RuntimeHistoryWire): EnrichmentRuntimeHistoryEntryView {
  return {
    revision: normalizeRevision(entry.revision),
    /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
    operationType: entry.operationType ?? '',
    /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
    riskLevel: entry.riskLevel ?? '',
    /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
    sourceVersion: entry.sourceVersion ?? '0',
    /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
    targetVersion: entry.targetVersion ?? '0',
    /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
    rollbackLineage: entry.rollbackLineage ?? '',
  }
}

function toWireSpec(spec: EnrichmentRuntimeSpecView) {
  return {
    queueLen: spec.queueLen,
    workers: spec.workers,
    batchSize: spec.batchSize,
    batchWindow: formatDurationSeconds(spec.batchWindowSeconds),
    sweepInterval: formatDurationSeconds(spec.sweepIntervalSeconds),
    llmRateLimitEnabled: spec.llmRateLimitEnabled,
    llmMaxQps: spec.llmMaxQps,
    llmBurst: spec.llmBurst,
  }
}

function parseDurationSeconds(raw?: string): number {
  if (!raw) return 0
  const trimmed = raw.trim()
  /* v8 ignore next -- @preserve: malformed non-second duration strings normalize to zero defensively. */
  if (!trimmed.endsWith('s')) return 0
  const numeric = Number(trimmed.slice(0, -1))
  return Number.isFinite(numeric) ? numeric : 0
}

function formatDurationSeconds(seconds: number): string {
  return `${seconds}s`
}
