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
  shouldHydrateRuntimeDraft,
  validateRuntimeSpec,
} from './enrichment-runtime-page'

describe('shouldHydrateRuntimeDraft', () => {
  it('hydrates when the desired version changes even if the local draft is dirty', () => {
    expect(
      shouldHydrateRuntimeDraft({
        desiredVersion: '8',
        lastHydratedVersion: '7',
        dirty: true,
      }),
    ).toBe(true)
  })

  it('does not overwrite a dirty draft while polling the same desired version', () => {
    expect(
      shouldHydrateRuntimeDraft({
        desiredVersion: '8',
        lastHydratedVersion: '8',
        dirty: true,
      }),
    ).toBe(false)
  })

  it('refreshes a clean draft while polling the same desired version', () => {
    expect(
      shouldHydrateRuntimeDraft({
        desiredVersion: '8',
        lastHydratedVersion: '8',
        dirty: false,
      }),
    ).toBe(true)
  })
})

describe('findLastKnownGoodEntry', () => {
  it('returns the matching history entry when last known good differs from current', () => {
    const found = findLastKnownGoodEntry({
      bootstrapDefault: {
        queueLen: 10,
        workers: 2,
        batchSize: 5,
        batchWindowSeconds: 1,
        sweepIntervalSeconds: 5,
        llmRateLimitEnabled: false,
        llmMaxQps: 0,
        llmBurst: 0,
      },
      desiredSpec: {
        queueLen: 20,
        workers: 4,
        batchSize: 5,
        batchWindowSeconds: 1,
        sweepIntervalSeconds: 5,
        llmRateLimitEnabled: true,
        llmMaxQps: 2,
        llmBurst: 2,
      },
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
        liveInstances: 2,
        staleInstances: 0,
        expiredInstances: 0,
        degradedInstances: 0,
        fullyAppliedInstances: 2,
        fullyConverged: true,
      },
      instances: [],
      history: [
        {
          revision: {
            version: '8',
            updatedBy: 'admin',
            updateReason: 'good',
            bootstrapSnapshotVersion: 'cfg',
            specVersion: 1,
            lastKnownGoodVersion: '7',
          },
          operationType: 'update',
          riskLevel: 'normal',
          sourceVersion: '7',
          targetVersion: '8',
          rollbackLineage: '',
        },
      ],
    })

    expect(found?.revision.version).toBe('8')
  })
})

describe('buildSpecDiffRows', () => {
  it('returns only changed fields', () => {
    const rows = buildSpecDiffRows(
      {
        queueLen: 10,
        workers: 2,
        batchSize: 5,
        batchWindowSeconds: 1,
        sweepIntervalSeconds: 5,
        llmRateLimitEnabled: false,
        llmMaxQps: 0,
        llmBurst: 0,
      },
      {
        queueLen: 20,
        workers: 2,
        batchSize: 5,
        batchWindowSeconds: 1,
        sweepIntervalSeconds: 5,
        llmRateLimitEnabled: true,
        llmMaxQps: 3,
        llmBurst: 3,
      },
    )

    expect(rows.map((row) => row.label)).toEqual(['Queue', 'Limiter', 'Max QPS', 'Burst'])
  })
})

describe('buildChangeImpactNotes', () => {
  it('returns deduplicated operator impact notes for changed runtime fields', () => {
    expect(
      buildChangeImpactNotes([
        { key: 'queueLen', current: '100', next: '200' },
        { key: 'workers', current: '2', next: '4' },
        { key: 'workers', current: '4', next: '8' },
      ]),
    ).toEqual([
      '队列容量变化会直接影响可吸收的输入波峰，以及触发背压的时机。',
      '工作协程变化会直接改变并行吞吐，也会同步放大或收紧对下游依赖的压力。',
    ])
  })
})

describe('validateRuntimeSpec', () => {
  it('returns a localized error key when workers exceed queue capacity', () => {
    expect(
      validateRuntimeSpec({
        queueLen: 2,
        workers: 3,
        batchSize: 1,
        batchWindowSeconds: 1,
        sweepIntervalSeconds: 5,
        llmRateLimitEnabled: false,
        llmMaxQps: 0,
        llmBurst: 0,
      }),
    ).toBe('settings.enrichment_runtime.errors.workers_lte_queue')
  })

  it('requires positive limiter values only when runtime rate limiting is enabled', () => {
    expect(
      validateRuntimeSpec({
        queueLen: 10,
        workers: 2,
        batchSize: 5,
        batchWindowSeconds: 1,
        sweepIntervalSeconds: 5,
        llmRateLimitEnabled: true,
        llmMaxQps: 0,
        llmBurst: 0,
      }),
    ).toBe('settings.enrichment_runtime.errors.llm_max_qps_required')
  })
})

describe('buildRuntimeConditions', () => {
  it('marks local limiter mode as a warning when multiple instances are live', () => {
    const conditions = buildRuntimeConditions({
      bootstrapDefault: {
        queueLen: 10,
        workers: 2,
        batchSize: 5,
        batchWindowSeconds: 1,
        sweepIntervalSeconds: 5,
        llmRateLimitEnabled: false,
        llmMaxQps: 0,
        llmBurst: 0,
      },
      desiredSpec: {
        queueLen: 10,
        workers: 2,
        batchSize: 5,
        batchWindowSeconds: 1,
        sweepIntervalSeconds: 5,
        llmRateLimitEnabled: true,
        llmMaxQps: 2,
        llmBurst: 2,
      },
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
        liveInstances: 3,
        staleInstances: 1,
        expiredInstances: 0,
        degradedInstances: 0,
        fullyAppliedInstances: 2,
        fullyConverged: false,
      },
      instances: [],
      history: [],
    })

    expect(
      conditions.some(
        (condition) => condition.label === 'Local limiter mode' && condition.tone === 'warn',
      ),
    ).toBe(true)
  })
})

describe('buildInstanceConditions', () => {
  it('flags applying, pending resize, and observed lag', () => {
    const conditions = buildInstanceConditions({
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
      appliedSpec: {
        queueLen: 10,
        workers: 2,
        batchSize: 5,
        batchWindowSeconds: 1,
        sweepIntervalSeconds: 5,
        llmRateLimitEnabled: false,
        llmMaxQps: 0,
        llmBurst: 0,
      },
    })

    expect(conditions.map((condition) => condition.label)).toEqual([
      'Reconciling',
      'Pending resize',
      'Observed lag',
    ])
  })
})

describe('buildInstanceDisplayName', () => {
  it('prefers a readable host label and keeps the raw id as secondary', () => {
    expect(
      buildInstanceDisplayName(
        {
          instanceId: 'phjdeMacBook-Pro.local',
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
          appliedSpec: {
            queueLen: 10,
            workers: 2,
            batchSize: 5,
            batchWindowSeconds: 1,
            sweepIntervalSeconds: 5,
            llmRateLimitEnabled: false,
            llmMaxQps: 0,
            llmBurst: 0,
          },
        },
        0,
      ),
    ).toEqual({
      primary: 'phjdeMacBook-Pro',
      secondary: 'phjdeMacBook-Pro.local',
    })
  })

  it('hides opaque runtime ids behind a generic node label', () => {
    expect(
      buildInstanceDisplayName(
        {
          instanceId: 'attune-c43e0420-6bcf-4ef1-9584-5759bdb271aa',
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
          appliedSpec: {
            queueLen: 10,
            workers: 2,
            batchSize: 5,
            batchWindowSeconds: 1,
            sweepIntervalSeconds: 5,
            llmRateLimitEnabled: false,
            llmMaxQps: 0,
            llmBurst: 0,
          },
        },
        2,
      ),
    ).toEqual({
      primary: '运行节点 3',
      secondary: 'attune-c43e0420-6bcf-4ef1-9584-5759bdb271aa',
    })
  })
})

describe('formatRuntimeActorLabel', () => {
  it('masks opaque actor identifiers', () => {
    expect(formatRuntimeActorLabel('04931811-7f6d-4fe4-8da9-09a672773d1f')).toBe('控制台管理员')
  })

  it('preserves readable actor labels', () => {
    expect(formatRuntimeActorLabel('ops@example.com')).toBe('ops@example.com')
  })
})

describe('partitionRuntimeInstances', () => {
  it('keeps the freshest rows in the active set based on the live instance count', () => {
    const partitioned = partitionRuntimeInstances(
      [
        {
          instanceId: 'node-old',
          bootId: 'boot-old',
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
          appliedSpec: {
            queueLen: 10,
            workers: 2,
            batchSize: 5,
            batchWindowSeconds: 1,
            sweepIntervalSeconds: 5,
            llmRateLimitEnabled: false,
            llmMaxQps: 0,
            llmBurst: 0,
          },
          lastSeenAt: '2026-06-18T01:00:00Z',
        },
        {
          instanceId: 'node-new',
          bootId: 'boot-new',
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
          appliedSpec: {
            queueLen: 10,
            workers: 2,
            batchSize: 5,
            batchWindowSeconds: 1,
            sweepIntervalSeconds: 5,
            llmRateLimitEnabled: false,
            llmMaxQps: 0,
            llmBurst: 0,
          },
          lastSeenAt: '2026-06-18T02:00:00Z',
        },
      ],
      1,
    )

    expect(partitioned.active.map((item) => item.instanceId)).toEqual(['node-new'])
    expect(partitioned.historical.map((item) => item.instanceId)).toEqual(['node-old'])
  })
})
