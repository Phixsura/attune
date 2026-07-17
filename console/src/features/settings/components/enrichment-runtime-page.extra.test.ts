import { describe, expect, it } from 'vitest'
import {
  buildChangeImpactNotes,
  buildInstanceConditions,
  buildInstanceDisplayName,
  buildRuntimeConditions,
  buildSpecDiffRows,
  draftToSpec,
  findLastKnownGoodEntry,
  formatRelative,
  formatRuntimeActorLabel,
  formatStatus,
  formatTimestamp,
  localizeConditionLabel,
  localizeOperationType,
  localizeRiskLevel,
  localizeSpecLabel,
  looksOpaqueId,
  partitionRuntimeInstances,
  validateRuntimeSpec,
} from './enrichment-runtime-page'

describe('enrichment runtime helper coverage', () => {
  const t = ((key: string, options?: { count?: number; value?: string }) => {
    if (options?.count != null) return `${key}:${options.count}`
    if (options?.value) return `${key}:${options.value}`
    return key
  }) as Parameters<typeof formatStatus>[1]

  const baseSpec = {
    queueLen: 10,
    workers: 2,
    batchSize: 5,
    batchWindowSeconds: 1,
    sweepIntervalSeconds: 5,
    llmRateLimitEnabled: false,
    llmMaxQps: 0,
    llmBurst: 0,
  }

  const baseRuntime = {
    bootstrapDefault: baseSpec,
    desiredSpec: baseSpec,
    desiredRevision: {
      version: '9',
      updatedBy: 'admin',
      updateReason: '',
      bootstrapSnapshotVersion: 'cfg',
      specVersion: 1,
      lastKnownGoodVersion: '8',
    },
    summary: {
      desiredVersion: '9',
      liveInstances: 1,
      staleInstances: 0,
      expiredInstances: 0,
      degradedInstances: 0,
      fullyAppliedInstances: 1,
      fullyConverged: true,
    },
    instances: [],
    history: [],
  } as Parameters<typeof buildRuntimeConditions>[0]

  it.each([
    {
      name: 'queue len',
      spec: { ...baseSpec, queueLen: 0 },
      want: 'settings.enrichment_runtime.errors.queue_len_positive',
    },
    {
      name: 'workers',
      spec: { ...baseSpec, queueLen: 2, workers: 3 },
      want: 'settings.enrichment_runtime.errors.workers_lte_queue',
    },
    {
      name: 'batch size',
      spec: { ...baseSpec, queueLen: 2, batchSize: 3 },
      want: 'settings.enrichment_runtime.errors.batch_size_lte_queue',
    },
    {
      name: 'batch window',
      spec: { ...baseSpec, batchWindowSeconds: 0 },
      want: 'settings.enrichment_runtime.errors.batch_window_positive',
    },
    {
      name: 'sweep interval',
      spec: { ...baseSpec, sweepIntervalSeconds: 0 },
      want: 'settings.enrichment_runtime.errors.sweep_interval_positive',
    },
    {
      name: 'qps',
      spec: { ...baseSpec, llmMaxQps: -1 },
      want: 'settings.enrichment_runtime.errors.llm_max_qps_non_negative',
    },
    {
      name: 'burst',
      spec: { ...baseSpec, llmBurst: -1 },
      want: 'settings.enrichment_runtime.errors.llm_burst_non_negative',
    },
    {
      name: 'enabled qps',
      spec: { ...baseSpec, llmRateLimitEnabled: true, llmMaxQps: 0 },
      want: 'settings.enrichment_runtime.errors.llm_max_qps_required',
    },
    {
      name: 'enabled burst',
      spec: { ...baseSpec, llmRateLimitEnabled: true, llmMaxQps: 1, llmBurst: 0 },
      want: 'settings.enrichment_runtime.errors.llm_burst_required',
    },
  ])('validateRuntimeSpec returns $want for $name', ({ spec, want }) => {
    expect(validateRuntimeSpec(spec)).toBe(want)
  })

  it('accepts a valid runtime spec', () => {
    expect(
      validateRuntimeSpec({
        ...baseSpec,
        llmRateLimitEnabled: true,
        llmMaxQps: 2,
        llmBurst: 2,
      }),
    ).toBeNull()
  })

  it('rejects non-numeric draft values before building a runtime spec payload', () => {
    expect(
      draftToSpec({
        queueLen: 'not-a-number',
        workers: '2',
        batchSize: '5',
        batchWindowSeconds: '1',
        sweepIntervalSeconds: '5',
        llmRateLimitEnabled: false,
        llmMaxQps: '0',
        llmBurst: '0',
      }),
    ).toEqual({ ok: false })
  })

  it('covers the remaining validation boundary errors', () => {
    expect(validateRuntimeSpec({ ...baseSpec, workers: 0 })).toBe(
      'settings.enrichment_runtime.errors.workers_positive',
    )
    expect(validateRuntimeSpec({ ...baseSpec, batchSize: 0 })).toBe(
      'settings.enrichment_runtime.errors.batch_size_positive',
    )
  })

  it('returns null when last known good metadata is absent, current, or missing from history', () => {
    expect(findLastKnownGoodEntry(undefined)).toBeNull()
    expect(
      findLastKnownGoodEntry({
        ...baseRuntime,
        desiredRevision: { ...baseRuntime.desiredRevision, lastKnownGoodVersion: '' },
      }),
    ).toBeNull()
    expect(
      findLastKnownGoodEntry({
        ...baseRuntime,
        desiredRevision: { ...baseRuntime.desiredRevision, lastKnownGoodVersion: '9' },
      }),
    ).toBeNull()
    expect(
      findLastKnownGoodEntry({
        ...baseRuntime,
        desiredRevision: { ...baseRuntime.desiredRevision, lastKnownGoodVersion: '7' },
        history: [],
      }),
    ).toBeNull()
  })

  it('builds diff rows for every changed runtime field and returns empty for identical specs', () => {
    expect(buildSpecDiffRows(baseSpec, baseSpec)).toEqual([])
    expect(
      buildSpecDiffRows(baseSpec, {
        queueLen: 11,
        workers: 3,
        batchSize: 6,
        batchWindowSeconds: 2,
        sweepIntervalSeconds: 6,
        llmRateLimitEnabled: true,
        llmMaxQps: 1,
        llmBurst: 1,
      }).map((row) => row.key),
    ).toEqual([
      'queueLen',
      'workers',
      'batchSize',
      'batchWindowSeconds',
      'sweepIntervalSeconds',
      'llmRateLimitEnabled',
      'llmMaxQps',
      'llmBurst',
    ])
  })

  it('describes healthy runtime states and single-instance limiter activation', () => {
    expect(buildRuntimeConditions(baseRuntime)).toEqual([{ tone: 'good', label: 'Converged' }])
    expect(
      buildRuntimeConditions({
        ...baseRuntime,
        desiredSpec: { ...baseSpec, llmRateLimitEnabled: true, llmMaxQps: 2, llmBurst: 2 },
      }),
    ).toEqual([
      { tone: 'good', label: 'Converged' },
      { tone: 'good', label: 'Limiter active' },
    ])
  })

  it('describes converged, degraded, stale, expired, and partially applied runtime states', () => {
    const conditions = buildRuntimeConditions({
      ...baseRuntime,
      summary: {
        desiredVersion: '9',
        liveInstances: 3,
        staleInstances: 1,
        expiredInstances: 2,
        degradedInstances: 1,
        fullyAppliedInstances: 1,
        fullyConverged: false,
      },
      desiredSpec: { ...baseSpec, llmRateLimitEnabled: true, llmMaxQps: 2, llmBurst: 2 },
    })

    expect(conditions).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ label: 'Reconciling', tone: 'warn' }),
        expect.objectContaining({ label: 'Degraded 1', tone: 'bad' }),
        expect.objectContaining({ label: 'Stale 1', tone: 'warn' }),
        expect.objectContaining({ label: 'Expired 2', tone: 'bad' }),
        expect.objectContaining({ label: 'Applied 1/3', tone: 'warn' }),
        expect.objectContaining({ label: 'Local limiter mode', tone: 'warn' }),
      ]),
    )
  })

  it('covers healthy and degraded instance states', () => {
    expect(
      buildInstanceConditions({
        instanceId: 'i-1',
        bootId: 'b-1',
        desiredVersion: '9',
        observedDesiredVersion: '8',
        runnerEffectiveVersion: '0',
        limiterEffectiveVersion: '9',
        attemptedRunnerVersion: '9',
        attemptedLimiterVersion: '9',
        runnerApplyStatus: 'applying',
        limiterApplyStatus: 'applied',
        runnerLastApplyError: '',
        limiterLastApplyError: '',
        queueDepth: 12,
        queueCapacityTarget: 10,
        queueCapacityEffective: 12,
        queueResizePending: true,
        inFlight: 2,
        degradedReason: '',
        appliedSpec: baseSpec,
      }).map((condition) => condition.label),
    ).toEqual(['Reconciling', 'Pending resize', 'Observed lag'])

    expect(
      buildInstanceConditions({
        instanceId: 'i-2',
        bootId: 'b-2',
        desiredVersion: '9',
        observedDesiredVersion: '9',
        runnerEffectiveVersion: '9',
        limiterEffectiveVersion: '9',
        attemptedRunnerVersion: '9',
        attemptedLimiterVersion: '9',
        runnerApplyStatus: 'applied',
        limiterApplyStatus: 'applied',
        runnerLastApplyError: '',
        limiterLastApplyError: '',
        queueDepth: 0,
        queueCapacityTarget: 10,
        queueCapacityEffective: 10,
        queueResizePending: false,
        inFlight: 0,
        degradedReason: 'provider timeout',
        appliedSpec: baseSpec,
      }).map((condition) => condition.label),
    ).toEqual(['Applied', 'Degraded'])

    expect(
      buildInstanceConditions({
        instanceId: 'i-3',
        bootId: 'b-3',
        desiredVersion: '9',
        observedDesiredVersion: '9',
        runnerEffectiveVersion: '9',
        limiterEffectiveVersion: '8',
        attemptedRunnerVersion: '9',
        attemptedLimiterVersion: '9',
        runnerApplyStatus: 'applied',
        limiterApplyStatus: 'applying',
        runnerLastApplyError: '',
        limiterLastApplyError: '',
        queueDepth: 0,
        queueCapacityTarget: 10,
        queueCapacityEffective: 10,
        queueResizePending: false,
        inFlight: 0,
        degradedReason: '',
        appliedSpec: baseSpec,
      }).map((condition) => condition.label),
    ).toEqual(['Reconciling'])

    expect(
      buildInstanceConditions({
        instanceId: 'i-4',
        bootId: 'b-4',
        desiredVersion: '9',
        observedDesiredVersion: '9',
        runnerEffectiveVersion: '8',
        limiterEffectiveVersion: '9',
        attemptedRunnerVersion: '9',
        attemptedLimiterVersion: '9',
        runnerApplyStatus: 'failed',
        limiterApplyStatus: 'applied',
        runnerLastApplyError: 'boom',
        limiterLastApplyError: '',
        queueDepth: 0,
        queueCapacityTarget: 10,
        queueCapacityEffective: 10,
        queueResizePending: false,
        inFlight: 0,
        degradedReason: '',
        appliedSpec: baseSpec,
      }).map((condition) => condition.label),
    ).toEqual(['Degraded'])
  })

  it('normalizes proto enum apply statuses before building instance condition chips', () => {
    expect(
      buildInstanceConditions({
        instanceId: 'i-5',
        bootId: 'b-5',
        desiredVersion: '9',
        observedDesiredVersion: '9',
        runnerEffectiveVersion: '9',
        limiterEffectiveVersion: '9',
        attemptedRunnerVersion: '9',
        attemptedLimiterVersion: '9',
        runnerApplyStatus: 'RUNTIME_APPLY_STATUS_APPLIED',
        limiterApplyStatus: 'RUNTIME_APPLY_STATUS_APPLIED',
        runnerLastApplyError: '',
        limiterLastApplyError: '',
        queueDepth: 0,
        queueCapacityTarget: 10,
        queueCapacityEffective: 10,
        queueResizePending: false,
        inFlight: 0,
        degradedReason: '',
        appliedSpec: baseSpec,
      }).map((condition) => condition.label),
    ).toEqual(['Applied'])
  })

  it('formats runtime instance display names and actor labels', () => {
    expect(
      buildInstanceDisplayName(
        {
          instanceId: 'worker-1.example.internal',
          bootId: 'boot-3',
          desiredVersion: '9',
          observedDesiredVersion: '9',
          runnerEffectiveVersion: '9',
          limiterEffectiveVersion: '9',
          attemptedRunnerVersion: '9',
          attemptedLimiterVersion: '9',
          runnerApplyStatus: 'applied',
          limiterApplyStatus: 'applied',
          runnerLastApplyError: '',
          limiterLastApplyError: '',
          queueDepth: 0,
          queueCapacityTarget: 10,
          queueCapacityEffective: 10,
          queueResizePending: false,
          inFlight: 0,
          degradedReason: '',
          appliedSpec: baseSpec,
        },
        1,
      ),
    ).toEqual({ primary: 'worker-1', secondary: 'worker-1.example.internal' })

    expect(
      buildInstanceDisplayName(
        {
          instanceId: '   ',
          bootId: 'boot-4',
          desiredVersion: '9',
          observedDesiredVersion: '9',
          runnerEffectiveVersion: '9',
          limiterEffectiveVersion: '9',
          attemptedRunnerVersion: '9',
          attemptedLimiterVersion: '9',
          runnerApplyStatus: 'applied',
          limiterApplyStatus: 'applied',
          runnerLastApplyError: '',
          limiterLastApplyError: '',
          queueDepth: 0,
          queueCapacityTarget: 10,
          queueCapacityEffective: 10,
          queueResizePending: false,
          inFlight: 0,
          degradedReason: '',
          appliedSpec: baseSpec,
        },
        2,
      ),
    ).toEqual({ primary: '运行节点 3', secondary: '' })

    expect(
      buildInstanceDisplayName(
        {
          instanceId: 'plain-worker',
          bootId: 'boot-5',
          desiredVersion: '9',
          observedDesiredVersion: '9',
          runnerEffectiveVersion: '9',
          limiterEffectiveVersion: '9',
          attemptedRunnerVersion: '9',
          attemptedLimiterVersion: '9',
          runnerApplyStatus: 'applied',
          limiterApplyStatus: 'applied',
          runnerLastApplyError: '',
          limiterLastApplyError: '',
          queueDepth: 0,
          queueCapacityTarget: 10,
          queueCapacityEffective: 10,
          queueResizePending: false,
          inFlight: 0,
          degradedReason: '',
          appliedSpec: baseSpec,
        },
        4,
      ),
    ).toEqual({ primary: 'plain-worker', secondary: 'plain-worker' })

    expect(formatRuntimeActorLabel('   ')).toBe('-')
    expect(formatRuntimeActorLabel('ops@example.com')).toBe('ops@example.com')
    expect(formatRuntimeActorLabel('04931811-7f6d-4fe4-8da9-09a672773d1f')).toBe('控制台管理员')
    expect(formatRuntimeActorLabel('ops-user')).toBe('ops-user')
  })

  it('partitions runtime instances with clamped active counts and missing timestamps', () => {
    const instances = [
      {
        instanceId: 'missing-time',
        bootId: 'boot-1',
        desiredVersion: '9',
        observedDesiredVersion: '9',
        runnerEffectiveVersion: '9',
        limiterEffectiveVersion: '9',
        attemptedRunnerVersion: '9',
        attemptedLimiterVersion: '9',
        runnerApplyStatus: 'applied',
        limiterApplyStatus: 'applied',
        runnerLastApplyError: '',
        limiterLastApplyError: '',
        queueDepth: 0,
        queueCapacityTarget: 10,
        queueCapacityEffective: 10,
        queueResizePending: false,
        inFlight: 0,
        degradedReason: '',
        appliedSpec: baseSpec,
      },
      {
        instanceId: 'fresh',
        bootId: 'boot-2',
        desiredVersion: '9',
        observedDesiredVersion: '9',
        runnerEffectiveVersion: '9',
        limiterEffectiveVersion: '9',
        attemptedRunnerVersion: '9',
        attemptedLimiterVersion: '9',
        runnerApplyStatus: 'applied',
        limiterApplyStatus: 'applied',
        runnerLastApplyError: '',
        limiterLastApplyError: '',
        queueDepth: 0,
        queueCapacityTarget: 10,
        queueCapacityEffective: 10,
        queueResizePending: false,
        inFlight: 0,
        degradedReason: '',
        appliedSpec: baseSpec,
        lastSeenAt: '2026-06-18T02:00:00Z',
      },
    ]

    expect(partitionRuntimeInstances(instances, -1)).toEqual({
      active: [],
      historical: [instances[1], instances[0]],
    })
    expect(partitionRuntimeInstances(instances, 5)).toEqual({
      active: [instances[1], instances[0]],
      historical: [],
    })
  })

  it('summarizes the remaining runtime change impact categories', () => {
    expect(
      buildChangeImpactNotes([
        { key: 'batchSize', current: '10', next: '20' },
        { key: 'batchWindowSeconds', current: '5', next: '10' },
        { key: 'sweepIntervalSeconds', current: '30', next: '60' },
        { key: 'llmRateLimitEnabled', current: 'false', next: 'true' },
        { key: 'llmMaxQps', current: '1', next: '2' },
        { key: 'llmBurst', current: '1', next: '2' },
        { key: 'unknown', current: 'a', next: 'b' },
      ]),
    ).toEqual([
      '批处理策略变化会在吞吐效率和单条反馈等待时间之间重新取舍。',
      '扫队列间隔变化会改变 backlog 恢复节奏，过短更积极，过长更保守。',
      'LLM 限流策略变化会直接影响模型调用压力、成本和短时流量释放能力。',
    ])
    expect(buildChangeImpactNotes([])).toEqual([])
    expect(buildChangeImpactNotes([{ key: 'unknown', current: 'a', next: 'b' }])).toEqual([])
  })

  it('localizes runtime status, condition, operation, risk, and spec labels', () => {
    expect(formatStatus('RUNTIME_APPLY_STATUS_APPLIED', t)).toBe(
      'settings.enrichment_runtime.status_labels.applied',
    )
    expect(formatStatus('RUNTIME_APPLY_STATUS_APPLYING', t)).toBe(
      'settings.enrichment_runtime.status_labels.reconciling',
    )
    expect(formatStatus('failed', t)).toBe('settings.enrichment_runtime.status_labels.failed')
    expect(formatStatus('', t)).toBe('-')
    expect(formatStatus('custom_status', t)).toBe('custom_status')

    expect(localizeConditionLabel('Converged', t)).toBe(
      'settings.enrichment_runtime.condition_labels.converged',
    )
    expect(localizeConditionLabel('Reconciling', t)).toBe(
      'settings.enrichment_runtime.condition_labels.reconciling',
    )
    expect(localizeConditionLabel('Applied', t)).toBe(
      'settings.enrichment_runtime.condition_labels.applied',
    )
    expect(localizeConditionLabel('Pending resize', t)).toBe(
      'settings.enrichment_runtime.condition_labels.pending_resize',
    )
    expect(localizeConditionLabel('Observed lag', t)).toBe(
      'settings.enrichment_runtime.condition_labels.observed_lag',
    )
    expect(localizeConditionLabel('Degraded', t)).toBe(
      'settings.enrichment_runtime.condition_labels.degraded',
    )
    expect(localizeConditionLabel('Limiter active', t)).toBe(
      'settings.enrichment_runtime.condition_labels.limiter_active',
    )
    expect(localizeConditionLabel('Local limiter mode', t)).toBe(
      'settings.enrichment_runtime.condition_labels.local_limiter_mode',
    )
    expect(localizeConditionLabel('Degraded 2', t)).toBe(
      'settings.enrichment_runtime.condition_labels.degraded_count:2',
    )
    expect(localizeConditionLabel('Stale 3', t)).toBe(
      'settings.enrichment_runtime.condition_labels.stale_count:3',
    )
    expect(localizeConditionLabel('Expired 4', t)).toBe(
      'settings.enrichment_runtime.condition_labels.expired_count:4',
    )
    expect(localizeConditionLabel('Applied 1/3', t)).toBe(
      'settings.enrichment_runtime.condition_labels.applied_ratio:1/3',
    )
    expect(localizeConditionLabel('Custom condition', t)).toBe('Custom condition')

    expect(localizeOperationType('update', t)).toBe(
      'settings.enrichment_runtime.operation_labels.update',
    )
    expect(localizeOperationType('reset', t)).toBe(
      'settings.enrichment_runtime.operation_labels.reset',
    )
    expect(localizeOperationType('rollback', t)).toBe(
      'settings.enrichment_runtime.operation_labels.rollback',
    )
    expect(localizeOperationType('custom', t)).toBe('custom')
    expect(localizeOperationType(undefined, t)).toBe('-')

    expect(localizeRiskLevel('normal', t)).toBe('settings.enrichment_runtime.risk_labels.normal')
    expect(localizeRiskLevel('high', t)).toBe('settings.enrichment_runtime.risk_labels.high')
    expect(localizeRiskLevel('critical', t)).toBe(
      'settings.enrichment_runtime.risk_labels.critical',
    )
    expect(localizeRiskLevel('custom', t)).toBe('custom')
    expect(localizeRiskLevel(undefined, t)).toBe('-')

    expect(localizeSpecLabel('queueLen', 'Queue', t)).toBe(
      'settings.enrichment_runtime.fields.queue_len',
    )
    expect(localizeSpecLabel('workers', 'Workers', t)).toBe(
      'settings.enrichment_runtime.fields.workers',
    )
    expect(localizeSpecLabel('batchSize', 'Batch', t)).toBe(
      'settings.enrichment_runtime.fields.batch_size',
    )
    expect(localizeSpecLabel('batchWindowSeconds', 'Batch Window', t)).toBe(
      'settings.enrichment_runtime.fields.batch_window',
    )
    expect(localizeSpecLabel('sweepIntervalSeconds', 'Sweep', t)).toBe(
      'settings.enrichment_runtime.fields.sweep_interval',
    )
    expect(localizeSpecLabel('llmRateLimitEnabled', 'Limiter', t)).toBe(
      'settings.enrichment_runtime.fields.llm_rate_limit_enabled',
    )
    expect(localizeSpecLabel('llmMaxQps', 'Max QPS', t)).toBe(
      'settings.enrichment_runtime.fields.llm_max_qps',
    )
    expect(localizeSpecLabel('llmBurst', 'Burst', t)).toBe(
      'settings.enrichment_runtime.fields.llm_burst',
    )
    expect(localizeSpecLabel('custom', 'Custom label', t)).toBe('Custom label')
  })

  it('formats runtime identifiers and timestamps defensively', () => {
    expect(looksOpaqueId('04931811-7f6d-4fe4-8da9-09a672773d1f')).toBe(true)
    expect(looksOpaqueId('attune-c43e04206bcf4ef195845759bdb271aa')).toBe(true)
    expect(looksOpaqueId('worker-1')).toBe(false)

    expect(formatRelative()).toBe('-')
    expect(formatRelative('not-a-date')).toBe('-')
    expect(formatRelative('2026-06-18T03:44:45Z')).not.toBe('-')

    expect(formatTimestamp(undefined, t)).toBe('common.never')
    expect(formatTimestamp('not-a-date', t)).toBe('common.never')
    expect(formatTimestamp('2026-06-18T03:44:45Z', t)).toContain('2026')
  })
})
