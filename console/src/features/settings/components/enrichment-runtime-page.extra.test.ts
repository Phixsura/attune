import { describe, expect, it } from 'vitest'
import {
  buildChangeImpactNotes,
  buildInstanceConditions,
  buildInstanceDisplayName,
  buildRuntimeConditions,
  buildSpecDiffRows,
  findLastKnownGoodEntry,
  formatRuntimeActorLabel,
  partitionRuntimeInstances,
  validateRuntimeSpec,
} from './enrichment-runtime-page'

describe('enrichment runtime helper coverage', () => {
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
})
