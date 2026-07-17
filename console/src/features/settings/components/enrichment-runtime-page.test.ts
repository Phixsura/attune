import { HttpResponse, http } from 'msw'
import { createElement } from 'react'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EnrichmentRuntimePage } from '@/features/settings/components/enrichment-runtime-page'
import { server } from '@/testing/mocks/server'
import { fireEvent, renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'
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

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

let blockerState: {
  status: 'idle' | 'blocked'
  proceed: ReturnType<typeof vi.fn>
  reset: ReturnType<typeof vi.fn>
  shouldBlockFn?: (args: unknown) => boolean
} = {
  status: 'idle',
  proceed: vi.fn(),
  reset: vi.fn(),
}

function resetBlocker() {
  blockerState = {
    status: 'idle',
    proceed: vi.fn(),
    reset: vi.fn(),
  }
}

vi.mock('@tanstack/react-router', () => ({
  useBlocker: (opts?: { shouldBlockFn?: (args: unknown) => boolean }) => {
    if (opts?.shouldBlockFn) blockerState.shouldBlockFn = opts.shouldBlockFn
    return blockerState
  },
}))

afterEach(() => {
  localStorage.clear()
  sessionStorage.clear()
  resetBlocker()
  vi.mocked(toast.error).mockClear()
  vi.mocked(toast.success).mockClear()
  vi.unstubAllGlobals()
})

const baseRuntimeResponse = {
  runtime: {
    bootstrapDefault: {
      queueLen: 1000,
      workers: 3,
      batchSize: 10,
      batchWindow: '5s',
      sweepInterval: '30s',
      llmRateLimitEnabled: false,
      llmMaxQps: 0,
      llmBurst: 0,
    },
    desiredSpec: {
      queueLen: 1007,
      workers: 3,
      batchSize: 10,
      batchWindow: '5s',
      sweepInterval: '30s',
      llmRateLimitEnabled: false,
      llmMaxQps: 0,
      llmBurst: 0,
    },
    desiredRevision: {
      version: '14',
      updatedAt: '2026-06-18T03:44:45Z',
      updatedBy: 'admin-1',
      updateReason: 'browser e2e revert queue 1007',
      bootstrapSnapshotVersion: 'enricher:1000:3:10:5s:30s:0:0',
      specVersion: 1,
      lastKnownGoodVersion: '13',
    },
    summary: {
      desiredVersion: '14',
      liveInstances: 1,
      staleInstances: 0,
      expiredInstances: 5,
      degradedInstances: 0,
      fullyAppliedInstances: 1,
      fullyConverged: true,
    },
    instances: [
      {
        instanceId: 'phjdeMacBook-Pro.local',
        bootId: 'boot-live-1',
        desiredVersion: '14',
        observedDesiredVersion: '14',
        runnerEffectiveVersion: '14',
        limiterEffectiveVersion: '14',
        attemptedRunnerVersion: '14',
        attemptedLimiterVersion: '14',
        runnerApplyStatus: 'RUNTIME_APPLY_STATUS_APPLIED',
        limiterApplyStatus: 'RUNTIME_APPLY_STATUS_APPLIED',
        queueDepth: 0,
        queueCapacityTarget: 1007,
        queueCapacityEffective: 1007,
        queueResizePending: false,
        inFlight: 0,
        degradedReason: '',
        lastAppliedAt: '2026-06-18T03:44:45Z',
        lastReconciledAt: '2026-06-18T03:44:45Z',
        lastSeenAt: '2026-06-18T03:50:52Z',
        appliedSpec: {
          queueLen: 1007,
          workers: 3,
          batchSize: 10,
          batchWindow: '5s',
          sweepInterval: '30s',
          llmRateLimitEnabled: false,
          llmMaxQps: 0,
          llmBurst: 0,
        },
      },
      {
        instanceId: 'attune-c43e0420-6bcf-4ef1-9584-5759bdb271aa',
        bootId: 'boot-old-1',
        desiredVersion: '6',
        observedDesiredVersion: '6',
        runnerEffectiveVersion: '6',
        limiterEffectiveVersion: '6',
        attemptedRunnerVersion: '6',
        attemptedLimiterVersion: '6',
        runnerApplyStatus: 'RUNTIME_APPLY_STATUS_APPLIED',
        limiterApplyStatus: 'RUNTIME_APPLY_STATUS_APPLIED',
        queueDepth: 0,
        queueCapacityTarget: 1000,
        queueCapacityEffective: 1000,
        queueResizePending: false,
        inFlight: 0,
        degradedReason: '',
        lastAppliedAt: '2026-06-18T02:36:14Z',
        lastReconciledAt: '2026-06-18T02:36:14Z',
        lastSeenAt: '2026-06-18T02:36:14Z',
        appliedSpec: {
          queueLen: 1000,
          workers: 3,
          batchSize: 10,
          batchWindow: '5s',
          sweepInterval: '30s',
          llmRateLimitEnabled: false,
          llmMaxQps: 0,
          llmBurst: 0,
        },
      },
    ],
    history: [
      {
        revision: {
          version: '14',
          updatedAt: '2026-06-18T03:44:45Z',
          updatedBy: 'admin-1',
          updateReason: 'browser e2e revert queue 1007',
          bootstrapSnapshotVersion: 'cfg-v1',
          specVersion: 1,
          lastKnownGoodVersion: '13',
        },
        operationType: 'update',
        riskLevel: 'normal',
        sourceVersion: '13',
        targetVersion: '14',
        rollbackLineage: '',
      },
      {
        revision: {
          version: '13',
          updatedAt: '2026-06-18T03:44:41Z',
          updatedBy: 'admin-1',
          updateReason: 'browser e2e save queue 1008',
          bootstrapSnapshotVersion: 'cfg-v1',
          specVersion: 1,
          lastKnownGoodVersion: '12',
        },
        operationType: 'update',
        riskLevel: 'normal',
        sourceVersion: '12',
        targetVersion: '13',
        rollbackLineage: '',
      },
    ],
  },
}

const verifiedOperations = {
  stepUp: {
    satisfied: true,
    passwordAllowed: true,
    method: 'password',
    ttlSeconds: 900,
    verifiedAt: '2026-06-18T03:43:48Z',
    expiresAt: '2026-06-18T03:58:48Z',
  },
}

function mockRuntimePage(options?: {
  operations?: typeof verifiedOperations
  onRead?: () => void
  onUpdate?: (body: unknown) => void
  onReset?: (body: unknown) => void
  onRollback?: (body: unknown) => void
  onVerify?: (body: unknown) => void
}) {
  server.use(
    http.get('/fb/v1/console/enrichment-runtime', () => {
      options?.onRead?.()
      return HttpResponse.json(baseRuntimeResponse)
    }),
    http.get('/fb/v1/console/gdpr/operations', () =>
      HttpResponse.json(options?.operations ?? verifiedOperations),
    ),
    http.put('/fb/v1/console/enrichment-runtime', async ({ request }) => {
      const body = await request.json()
      options?.onUpdate?.(body)
      return HttpResponse.json({
        runtime: {
          ...baseRuntimeResponse.runtime,
          desiredSpec: {
            queueLen: 1008,
            workers: 3,
            batchSize: 10,
            batchWindow: '5s',
            sweepInterval: '30s',
            llmRateLimitEnabled: false,
            llmMaxQps: 0,
            llmBurst: 0,
          },
          desiredRevision: {
            ...baseRuntimeResponse.runtime.desiredRevision,
            version: '15',
            updateReason: 'raise queue',
            lastKnownGoodVersion: '14',
          },
          summary: {
            ...baseRuntimeResponse.runtime.summary,
            desiredVersion: '15',
          },
        },
      })
    }),
    http.post('/fb/v1/console/enrichment-runtime/reset', async ({ request }) => {
      const body = await request.json()
      options?.onReset?.(body)
      return HttpResponse.json(baseRuntimeResponse)
    }),
    http.post('/fb/v1/console/enrichment-runtime/rollback', async ({ request }) => {
      const body = await request.json()
      options?.onRollback?.(body)
      return HttpResponse.json(baseRuntimeResponse)
    }),
    http.post('/fb/v1/console/gdpr/step-up/verify', async ({ request }) => {
      const body = await request.json()
      options?.onVerify?.(body)
      return HttpResponse.json({
        verifiedAt: '2026-06-18T03:50:00Z',
        expiresAt: '2026-06-18T04:05:00Z',
        method: 'password',
      })
    }),
  )
}

describe('shouldHydrateRuntimeDraft', () => {
  it('returns conflict when server version changes and local draft is dirty', () => {
    expect(
      shouldHydrateRuntimeDraft({
        desiredVersion: '8',
        lastHydratedVersion: '7',
        dirty: true,
      }),
    ).toBe('conflict')
  })

  it('hydrates when server version changes and local draft is clean', () => {
    expect(
      shouldHydrateRuntimeDraft({
        desiredVersion: '8',
        lastHydratedVersion: '7',
        dirty: false,
      }),
    ).toBe('hydrate')
  })

  it('skips when polling the same version and draft is dirty', () => {
    expect(
      shouldHydrateRuntimeDraft({
        desiredVersion: '8',
        lastHydratedVersion: '8',
        dirty: true,
      }),
    ).toBe('skip')
  })

  it('hydrates when polling the same version and draft is clean', () => {
    expect(
      shouldHydrateRuntimeDraft({
        desiredVersion: '8',
        lastHydratedVersion: '8',
        dirty: false,
      }),
    ).toBe('hydrate')
  })

  it('always hydrates on initial load even when dirty (lastHydratedVersion undefined)', () => {
    expect(
      shouldHydrateRuntimeDraft({
        desiredVersion: '8',
        lastHydratedVersion: undefined,
        dirty: true,
      }),
    ).toBe('hydrate')
  })

  it('skips when desiredVersion is undefined', () => {
    expect(
      shouldHydrateRuntimeDraft({
        desiredVersion: undefined,
        lastHydratedVersion: '7',
        dirty: false,
      }),
    ).toBe('skip')
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

describe('EnrichmentRuntimePage', () => {
  it('renders operator-facing runtime state, hides raw ids by default, and expands historical nodes on demand', async () => {
    let runtimeReads = 0
    mockRuntimePage({
      onRead: () => {
        runtimeReads += 1
      },
    })
    const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))

    expect(await screen.findByText('富化运行时控制面')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '刷新状态' }))
    await waitFor(() => {
      expect(runtimeReads).toBeGreaterThan(1)
    })
    expect(screen.getByText('phjdeMacBook-Pro')).toBeInTheDocument()
    expect(
      screen.queryByText('attune-c43e0420-6bcf-4ef1-9584-5759bdb271aa'),
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '展开历史节点详情' }))
    expect(await screen.findByText('运行节点 2')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '收起历史节点详情' })).toHaveAttribute(
      'aria-expanded',
      'true',
    )

    const detailButtons = screen.getAllByRole('button', { name: '查看技术详情' })
    expect(detailButtons).toHaveLength(2)
    const historicalDetailButton = detailButtons[1]
    if (!historicalDetailButton) {
      throw new Error('expected historical detail button')
    }
    await user.hover(historicalDetailButton)
    expect(
      await screen.findAllByText((text) =>
        text.includes('attune-c43e0420-6bcf-4ef1-9584-5759bdb271aa'),
      ),
    ).not.toHaveLength(0)
  })

  it('validates edits in Chinese and saves the updated runtime spec', async () => {
    let updateBody: unknown
    mockRuntimePage({
      onUpdate: (body) => {
        updateBody = body
      },
    })
    const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))

    expect(await screen.findByText('目标策略')).toBeInTheDocument()
    const queueInput = screen.getByLabelText('队列容量')
    await user.clear(queueInput)
    await user.type(queueInput, '0')
    expect(await screen.findByText('队列容量必须大于 0')).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 's', ctrlKey: true })
    expect(toast.error).toHaveBeenCalledWith('队列容量必须大于 0')

    await user.clear(queueInput)
    await user.type(queueInput, '1008')
    const workersInput = screen.getByLabelText('工作协程数')
    await user.clear(workersInput)
    await user.type(workersInput, '4')
    const batchSizeInput = screen.getByLabelText('批处理大小')
    await user.clear(batchSizeInput)
    await user.type(batchSizeInput, '12')
    const batchWindowInput = screen.getByLabelText('批处理窗口')
    await user.clear(batchWindowInput)
    await user.type(batchWindowInput, '7')
    const sweepIntervalInput = screen.getByLabelText('扫队列间隔')
    await user.clear(sweepIntervalInput)
    await user.type(sweepIntervalInput, '45')
    await user.click(screen.getByRole('checkbox', { name: '启用 LLM 限流' }))
    const llmMaxQpsInput = screen.getByLabelText('LLM 最大 QPS')
    await user.clear(llmMaxQpsInput)
    await user.type(llmMaxQpsInput, '2')
    const llmBurstInput = screen.getByLabelText('LLM Burst')
    await user.clear(llmBurstInput)
    await user.type(llmBurstInput, '5')
    await user.type(screen.getByLabelText('变更说明'), 'raise queue')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(updateBody).toEqual({
        expectedVersion: '14',
        updateReason: 'raise queue',
        spec: {
          queueLen: 1008,
          workers: 4,
          batchSize: 12,
          batchWindow: '7s',
          sweepInterval: '45s',
          llmRateLimitEnabled: true,
          llmMaxQps: 2,
          llmBurst: 5,
        },
      })
    })
    expect((await screen.findAllByText('目标版本 15 · schema v1')).length).toBeGreaterThanOrEqual(1)
  })

  it('opens runtime-specific step-up copy and verifies by password when recent auth is missing', async () => {
    let verifyBody: unknown
    mockRuntimePage({
      operations: {
        stepUp: {
          satisfied: false,
          passwordAllowed: true,
          method: 'password',
          ttlSeconds: 900,
          verifiedAt: '',
          expiresAt: '',
        },
      },
      onVerify: (body) => {
        verifyBody = body
      },
    })
    const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))

    expect(await screen.findByText('需要二次验证')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '完成二次验证' }))
    let stepUpDialog = await screen.findByRole('dialog', { name: '确认运行时敏感操作' })
    expect(within(stepUpDialog).getByText(/运行时保存、重置和回滚操作/)).toBeInTheDocument()
    await user.click(within(stepUpDialog).getByRole('button', { name: '取消' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '确认运行时敏感操作' })).toBeNull()
    })

    await user.click(screen.getByRole('button', { name: '完成二次验证' }))
    stepUpDialog = await screen.findByRole('dialog', { name: '确认运行时敏感操作' })
    await user.type(within(stepUpDialog).getByLabelText('当前密码'), 'correct horse battery staple')
    await user.click(within(stepUpDialog).getByRole('button', { name: '验证并继续' }))
    await waitFor(() => {
      expect(verifyBody).toEqual({ password: 'correct horse battery staple' })
    })
  }, 20_000) // Full-page runtime smoke covers dialogs, step-up auth, and mutation refetches.

  it('requires recent step-up before saving runtime edits', async () => {
    let updateBody: unknown
    mockRuntimePage({
      operations: {
        stepUp: {
          satisfied: false,
          passwordAllowed: true,
          method: 'password',
          ttlSeconds: 900,
          verifiedAt: '',
          expiresAt: '',
        },
      },
      onUpdate: (body) => {
        updateBody = body
      },
    })
    const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))

    expect(await screen.findByText('需要二次验证')).toBeInTheDocument()
    const queueInput = screen.getByLabelText('队列容量')
    await user.clear(queueInput)
    await user.type(queueInput, '1008')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('请先完成运行时二次验证')
    })
    expect(updateBody).toBeUndefined()
    expect(await screen.findByRole('dialog', { name: '确认运行时敏感操作' })).toBeInTheDocument()
  }, 20_000)

  it('submits reset and rollback actions when recent auth is already satisfied', async () => {
    const resetBodies: unknown[] = []
    const rollbackBodies: unknown[] = []
    mockRuntimePage({
      onReset: (body) => {
        resetBodies.push(body)
      },
      onRollback: (body) => {
        rollbackBodies.push(body)
      },
    })
    const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))

    expect(await screen.findByText('目标策略')).toBeInTheDocument()
    expect(screen.getAllByText('二次验证已通过')).not.toHaveLength(0)

    await user.click(screen.getByRole('button', { name: '恢复全部默认' }))
    const resetAllDialog = await screen.findByRole('dialog', { name: '重置运行时配置' })
    expect(
      within(resetAllDialog).getByRole('checkbox', { name: '恢复全部字段到启动默认值' }),
    ).toBeChecked()
    await user.click(within(resetAllDialog).getByRole('button', { name: '取消' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '重置运行时配置' })).toBeNull()
    })

    await user.click(screen.getByRole('button', { name: '按字段重置' }))
    const resetDialog = await screen.findByRole('dialog', { name: '重置运行时配置' })
    await user.click(within(resetDialog).getByRole('button', { name: '执行重置' }))
    expect(toast.error).toHaveBeenCalledWith('请至少选择一个要重置的字段')
    const resetAllToggle = within(resetDialog).getByRole('checkbox', {
      name: '恢复全部字段到启动默认值',
    })
    await user.click(resetAllToggle)
    expect(within(resetDialog).queryByRole('checkbox', { name: '工作协程数' })).toBeNull()
    await user.click(resetAllToggle)
    const workersReset = within(resetDialog).getByRole('checkbox', { name: '工作协程数' })
    await user.click(workersReset)
    await user.click(workersReset)
    await user.click(workersReset)
    await user.type(within(resetDialog).getByLabelText('变更说明'), 'reset workers')
    await user.click(within(resetDialog).getByRole('button', { name: '执行重置' }))
    await waitFor(() => {
      expect(resetBodies.at(-1)).toEqual({
        expectedVersion: '14',
        fields: ['workers'],
        resetAll: false,
        updateReason: 'reset workers',
      })
    })

    await user.click(screen.getByRole('button', { name: '回滚到已知良好版本' }))
    const rollbackDialog = await screen.findByRole('dialog', { name: '回滚到历史版本' })
    expect(
      within(rollbackDialog).getByDisplayValue('rollback to last known good 13'),
    ).toBeInTheDocument()
    await user.click(within(rollbackDialog).getByRole('button', { name: '取消' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '回滚到历史版本' })).toBeNull()
    })

    await user.click(screen.getByRole('button', { name: '回滚到已知良好版本' }))
    const confirmRollbackDialog = await screen.findByRole('dialog', { name: '回滚到历史版本' })
    await user.clear(within(confirmRollbackDialog).getByLabelText('变更说明'))
    await user.type(
      within(confirmRollbackDialog).getByLabelText('变更说明'),
      'rollback for stability',
    )
    await user.click(within(confirmRollbackDialog).getByRole('button', { name: '确认回滚' }))
    await waitFor(() => {
      expect(rollbackBodies.at(-1)).toEqual({
        expectedVersion: '14',
        targetVersion: '13',
        updateReason: 'rollback for stability',
      })
    })

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '回滚到历史版本' })).toBeNull()
    })
    await user.click(screen.getByRole('button', { name: '回滚' }))
    const historyRollbackDialog = await screen.findByRole('dialog', { name: '回滚到历史版本' })
    await user.type(within(historyRollbackDialog).getByLabelText('变更说明'), 'rollback from table')
    await user.click(within(historyRollbackDialog).getByRole('button', { name: '确认回滚' }))
    await waitFor(() => {
      expect(rollbackBodies.at(-1)).toEqual({
        expectedVersion: '14',
        targetVersion: '13',
        updateReason: 'rollback from table',
      })
    })
  }, 20_000) // Full-page runtime smoke covers multiple guarded action dialogs.

  it('requires recent step-up before reset and rollback mutations', async () => {
    let resetBody: unknown
    let rollbackBody: unknown
    mockRuntimePage({
      operations: {
        stepUp: {
          satisfied: false,
          passwordAllowed: true,
          method: 'password',
          ttlSeconds: 900,
          verifiedAt: '',
          expiresAt: '',
        },
      },
      onReset: (body) => {
        resetBody = body
      },
      onRollback: (body) => {
        rollbackBody = body
      },
    })
    const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))

    expect(await screen.findByText('需要二次验证')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '按字段重置' }))
    const resetDialog = await screen.findByRole('dialog', { name: '重置运行时配置' })
    await user.click(within(resetDialog).getByRole('checkbox', { name: '工作协程数' }))
    await user.click(within(resetDialog).getByRole('button', { name: '执行重置' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('请先完成运行时二次验证')
    })
    expect(resetBody).toBeUndefined()
    const resetStepUpDialog = await screen.findByRole('dialog', { name: '确认运行时敏感操作' })
    await user.click(within(resetStepUpDialog).getByRole('button', { name: '取消' }))
    await user.click(within(resetDialog).getByRole('button', { name: '取消' }))

    await user.click(screen.getByRole('button', { name: '回滚到已知良好版本' }))
    const rollbackDialog = await screen.findByRole('dialog', { name: '回滚到历史版本' })
    await user.click(within(rollbackDialog).getByRole('button', { name: '确认回滚' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('请先完成运行时二次验证')
    })
    expect(rollbackBody).toBeUndefined()
  }, 20_000)

  it('surfaces step-up verification errors', async () => {
    server.use(
      http.get('/fb/v1/console/enrichment-runtime', () => HttpResponse.json(baseRuntimeResponse)),
      http.get('/fb/v1/console/gdpr/operations', () =>
        HttpResponse.json({
          stepUp: {
            satisfied: false,
            passwordAllowed: true,
            method: 'password',
            ttlSeconds: 900,
            verifiedAt: '',
            expiresAt: '',
          },
        }),
      ),
      http.post('/fb/v1/console/gdpr/step-up/verify', () =>
        HttpResponse.json(
          { code: 'GDPR_STEP_UP_FAILED', message: 'step-up rejected' },
          { status: 403 },
        ),
      ),
    )
    const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))

    expect(await screen.findByText('需要二次验证')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '完成二次验证' }))
    const stepUpDialog = await screen.findByRole('dialog', { name: '确认运行时敏感操作' })
    await user.type(within(stepUpDialog).getByLabelText('当前密码'), 'wrong password')
    await user.click(within(stepUpDialog).getByRole('button', { name: '验证并继续' }))

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('step-up rejected')
    })
  }, 20_000)

  it('surfaces reset and rollback mutation errors without closing the dialogs', async () => {
    server.use(
      http.get('/fb/v1/console/enrichment-runtime', () => HttpResponse.json(baseRuntimeResponse)),
      http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(verifiedOperations)),
      http.post('/fb/v1/console/enrichment-runtime/reset', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'reset exploded' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/enrichment-runtime/rollback', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'rollback exploded' }, { status: 500 }),
      ),
    )
    const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))

    expect(await screen.findByText('目标策略')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '按字段重置' }))
    const resetDialog = await screen.findByRole('dialog', { name: '重置运行时配置' })
    await user.click(within(resetDialog).getByRole('checkbox', { name: '工作协程数' }))
    await user.click(within(resetDialog).getByRole('button', { name: '执行重置' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('reset exploded')
    })
    expect(screen.getByRole('dialog', { name: '重置运行时配置' })).toBeInTheDocument()
    await user.click(within(resetDialog).getByRole('button', { name: '取消' }))

    await user.click(screen.getByRole('button', { name: '回滚到已知良好版本' }))
    const rollbackDialog = await screen.findByRole('dialog', { name: '回滚到历史版本' })
    await user.click(within(rollbackDialog).getByRole('button', { name: '确认回滚' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('rollback exploded')
    })
    expect(screen.getByRole('dialog', { name: '回滚到历史版本' })).toBeInTheDocument()
  }, 20_000)

  describe('draft durability (#172)', () => {
    it('refetches runtime state when another tab clears the draft', async () => {
      const channels: MockBroadcastChannel[] = []
      class MockBroadcastChannel {
        onmessage: ((event: MessageEvent) => void) | null = null
        readonly name: string

        constructor(name: string) {
          this.name = name
          channels.push(this)
        }

        postMessage = vi.fn()
        close = vi.fn()
      }
      vi.stubGlobal('BroadcastChannel', MockBroadcastChannel)

      let runtimeReads = 0
      mockRuntimePage({
        onRead: () => {
          runtimeReads += 1
        },
      })
      renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))

      await screen.findByText('目标策略')
      expect(channels).toHaveLength(1)
      channels[0]?.onmessage?.({
        data: { type: 'draft-cleared', key: 'enrichment-runtime' },
      } as MessageEvent)

      await waitFor(() => {
        expect(runtimeReads).toBeGreaterThan(1)
      })
    })

    it('restores draft from localStorage on mount', async () => {
      const storedDraft = {
        queueLen: '200',
        workers: '8',
        batchSize: '20',
        batchWindowSeconds: '10',
        sweepIntervalSeconds: '60',
        llmRateLimitEnabled: false,
        llmMaxQps: '0',
        llmBurst: '0',
      }
      localStorage.setItem(
        'attune:draft:enrichment-runtime',
        JSON.stringify({ _v: 1, _ts: Date.now(), data: storedDraft }),
      )
      mockRuntimePage()
      renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      await waitFor(() => {
        expect(screen.getByDisplayValue('200')).toBeInTheDocument()
      })
    })

    it('shows recovery banner when draft is restored from localStorage', async () => {
      const storedDraft = {
        queueLen: '200',
        workers: '8',
        batchSize: '20',
        batchWindowSeconds: '10',
        sweepIntervalSeconds: '60',
        llmRateLimitEnabled: false,
        llmMaxQps: '0',
        llmBurst: '0',
      }
      localStorage.setItem(
        'attune:draft:enrichment-runtime',
        JSON.stringify({ _v: 1, _ts: Date.now(), data: storedDraft }),
      )
      mockRuntimePage()
      const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      await waitFor(() => {
        expect(screen.getByText('检测到未保存的草稿')).toBeInTheDocument()
      })
      await user.click(screen.getByRole('button', { name: '保留草稿' }))
      expect(screen.queryByText('检测到未保存的草稿')).not.toBeInTheDocument()
    })

    it('clears localStorage on successful save', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      mockRuntimePage()
      const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      await screen.findByText('目标策略')
      const queueInput = screen.getByLabelText('队列容量')
      await user.clear(queueInput)
      await user.type(queueInput, '1008')
      vi.advanceTimersByTime(600)
      expect(localStorage.getItem('attune:draft:enrichment-runtime')).not.toBeNull()
      await user.type(screen.getByLabelText('变更说明'), 'test')
      await user.click(screen.getByRole('button', { name: '保存' }))
      await waitFor(() => {
        expect(localStorage.getItem('attune:draft:enrichment-runtime')).toBeNull()
      })
      vi.useRealTimers()
    })

    it('gracefully ignores corrupted localStorage', async () => {
      localStorage.setItem('attune:draft:enrichment-runtime', '{corrupted!!!')
      mockRuntimePage()
      renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      const queueInput = await screen.findByLabelText('队列容量')
      expect(queueInput).toHaveValue(1007)
    })

    it('fills missing fields with defaults when restoring a partial draft', async () => {
      localStorage.setItem(
        'attune:draft:enrichment-runtime',
        JSON.stringify({ _v: 1, _ts: Date.now(), data: { queueLen: '5000', workers: '8' } }),
      )
      mockRuntimePage()
      renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      const queueInput = await screen.findByLabelText('队列容量')
      expect(queueInput).toHaveValue(5000)
      expect(screen.getByLabelText('工作协程数')).toHaveValue(8)
    })

    it('does not persist drafts when canEdit is false', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      mockRuntimePage()
      renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: false }))
      await screen.findByText('目标策略')
      vi.advanceTimersByTime(600)
      expect(localStorage.getItem('attune:draft:enrichment-runtime')).toBeNull()
      vi.useRealTimers()
    })

    it('keeps draft in localStorage after a failed save', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      server.use(
        http.get('/fb/v1/console/enrichment-runtime', () => HttpResponse.json(baseRuntimeResponse)),
        http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(verifiedOperations)),
        http.put('/fb/v1/console/enrichment-runtime', () =>
          HttpResponse.json({ code: 'INTERNAL', message: 'save exploded' }, { status: 500 }),
        ),
      )
      const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      await screen.findByText('目标策略')
      const queueInput = screen.getByLabelText('队列容量')
      await user.clear(queueInput)
      await user.type(queueInput, '2000')
      vi.advanceTimersByTime(600)
      expect(localStorage.getItem('attune:draft:enrichment-runtime')).not.toBeNull()
      await user.type(screen.getByLabelText('变更说明'), 'test')
      await user.click(screen.getByRole('button', { name: '保存' }))
      await waitFor(() => {
        expect(localStorage.getItem('attune:draft:enrichment-runtime')).not.toBeNull()
      })
      expect(toast.error).toHaveBeenCalledWith('save exploded')
      vi.useRealTimers()
    })

    it('disables form inputs while save is in flight', async () => {
      let releasePut: () => void = () => {}
      const gate = new Promise<void>((r) => {
        releasePut = r
      })
      server.use(
        http.get('/fb/v1/console/enrichment-runtime', () => HttpResponse.json(baseRuntimeResponse)),
        http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(verifiedOperations)),
        http.put('/fb/v1/console/enrichment-runtime', async () => {
          await gate
          return HttpResponse.json(baseRuntimeResponse)
        }),
      )
      const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      await screen.findByText('目标策略')
      const queueInput = screen.getByLabelText('队列容量')
      await user.clear(queueInput)
      await user.type(queueInput, '2000')
      await user.type(screen.getByLabelText('变更说明'), 'test')
      await user.click(screen.getByRole('button', { name: '保存' }))
      await waitFor(() => expect(queueInput).toBeDisabled())
      releasePut()
      await waitFor(() => expect(queueInput).toBeEnabled())
    })

    it('persists updated draft value when editing a restored draft', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      const storedDraft = {
        queueLen: '200',
        workers: '3',
        batchSize: '10',
        batchWindowSeconds: '5',
        sweepIntervalSeconds: '30',
        llmRateLimitEnabled: false,
        llmMaxQps: '0',
        llmBurst: '0',
      }
      localStorage.setItem(
        'attune:draft:enrichment-runtime',
        JSON.stringify({ _v: 1, _ts: Date.now(), data: storedDraft }),
      )
      mockRuntimePage()
      const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      await waitFor(() => {
        expect(screen.getByDisplayValue('200')).toBeInTheDocument()
      })
      const queueInput = screen.getByLabelText('队列容量')
      await user.clear(queueInput)
      await user.type(queueInput, '4000')
      vi.advanceTimersByTime(600)
      const stored = localStorage.getItem('attune:draft:enrichment-runtime')
      expect(stored).not.toBeNull()
      const envelope = JSON.parse(stored as string)
      expect(envelope.data.queueLen).toBe('4000')
      vi.useRealTimers()
    })

    it('shows conflict banner when server version changes while user has dirty edits', async () => {
      let runtimeVersion = '14'
      server.use(
        http.get('/fb/v1/console/enrichment-runtime', () => {
          const resp = {
            runtime: {
              ...baseRuntimeResponse.runtime,
              desiredRevision: {
                ...baseRuntimeResponse.runtime.desiredRevision,
                version: runtimeVersion,
              },
              summary: {
                ...baseRuntimeResponse.runtime.summary,
                desiredVersion: runtimeVersion,
              },
            },
          }
          return HttpResponse.json(resp)
        }),
        http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(verifiedOperations)),
      )
      const { user, queryClient } = renderWithProviders(
        createElement(EnrichmentRuntimePage, { canEdit: true }),
      )
      await screen.findByText('目标策略')

      const queueInput = screen.getByLabelText('队列容量')
      await user.clear(queueInput)
      await user.type(queueInput, '2000')

      runtimeVersion = '15'
      void queryClient.invalidateQueries({ queryKey: ['console', 'enrichment-runtime'] })

      await waitFor(() => {
        expect(screen.getByText('服务器配置已更新')).toBeInTheDocument()
      })

      expect(screen.getByLabelText('队列容量')).toHaveValue(2000)
    })

    it('conflict banner discard resets draft to server spec', async () => {
      let runtimeVersion = '14'
      server.use(
        http.get('/fb/v1/console/enrichment-runtime', () => {
          const resp = {
            ...baseRuntimeResponse,
            runtime: {
              ...baseRuntimeResponse.runtime,
              desiredRevision: {
                ...baseRuntimeResponse.runtime.desiredRevision,
                version: runtimeVersion,
              },
              summary: {
                ...baseRuntimeResponse.runtime.summary,
                desiredVersion: runtimeVersion,
              },
            },
          }
          return HttpResponse.json(resp)
        }),
        http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(verifiedOperations)),
      )
      const { user, queryClient } = renderWithProviders(
        createElement(EnrichmentRuntimePage, { canEdit: true }),
      )
      await screen.findByText('目标策略')
      const queueInput = screen.getByLabelText('队列容量')
      await user.clear(queueInput)
      await user.type(queueInput, '2000')

      runtimeVersion = '15'
      void queryClient.invalidateQueries({ queryKey: ['console', 'enrichment-runtime'] })

      await waitFor(() => {
        expect(screen.getByText('服务器配置已更新')).toBeInTheDocument()
      })

      await user.click(screen.getByRole('button', { name: '加载最新版本' }))
      await waitFor(() => {
        expect(screen.queryByText('服务器配置已更新')).not.toBeInTheDocument()
      })
      expect(screen.getByLabelText('队列容量')).toHaveValue(
        baseRuntimeResponse.runtime.desiredSpec.queueLen,
      )
    })

    it('marks dirty even when input is invalid (non-numeric)', async () => {
      server.use(
        http.get('/fb/v1/console/enrichment-runtime', () => HttpResponse.json(baseRuntimeResponse)),
        http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(verifiedOperations)),
      )
      const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      await screen.findByText('目标策略')
      const queueInput = screen.getByLabelText('队列容量')
      await user.clear(queueInput)
      await user.type(queueInput, 'abc')
      expect(screen.getByText(/未保存/)).toBeInTheDocument()
      expect(blockerState.shouldBlockFn?.({} as never)).toBe(true)
    })

    it('shows SaveStatus unsaved indicator when form is dirty', async () => {
      server.use(
        http.get('/fb/v1/console/enrichment-runtime', () => HttpResponse.json(baseRuntimeResponse)),
        http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(verifiedOperations)),
      )
      const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      await screen.findByText('目标策略')
      expect(screen.queryByText(/未保存/)).not.toBeInTheDocument()
      const queueInput = screen.getByLabelText('队列容量')
      await user.clear(queueInput)
      await user.type(queueInput, '999')
      expect(screen.getByText(/未保存/)).toBeInTheDocument()
    })

    it('registers shouldBlockFn when form is dirty', async () => {
      server.use(
        http.get('/fb/v1/console/enrichment-runtime', () => HttpResponse.json(baseRuntimeResponse)),
        http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(verifiedOperations)),
      )
      const { user } = renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      await screen.findByText('目标策略')
      const queueInput = screen.getByLabelText('队列容量')
      await user.clear(queueInput)
      await user.type(queueInput, '999')
      expect(blockerState.shouldBlockFn).toBeDefined()
    })

    it('shows UnsavedChangesDialog when blocker status is blocked', async () => {
      server.use(
        http.get('/fb/v1/console/enrichment-runtime', () => HttpResponse.json(baseRuntimeResponse)),
        http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(verifiedOperations)),
      )
      blockerState.status = 'blocked'
      renderWithProviders(createElement(EnrichmentRuntimePage, { canEdit: true }))
      await waitFor(() => {
        expect(screen.getByText('未保存的更改')).toBeInTheDocument()
      })
    })
  })
})
