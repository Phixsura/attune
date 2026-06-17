import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { createElement, type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import {
  enrichmentRuntimeQuery,
  useResetEnrichmentRuntime,
  useRollbackEnrichmentRuntime,
  useUpdateEnrichmentRuntime,
} from '@/features/settings/api/enrichment-runtime'
import { server } from '@/testing/mocks/server'

function wrap() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children)
  return { qc, wrapper }
}

describe('enrichmentRuntimeQuery', () => {
  it('normalizes wire runtime payload into UI-friendly shape', async () => {
    server.use(
      http.get('/fb/v1/console/enrichment-runtime', () =>
        HttpResponse.json({
          runtime: {
            bootstrapDefault: {
              queueLen: 64,
              workers: 8,
              batchSize: 16,
              batchWindow: '2s',
              sweepInterval: '5s',
              llmRateLimitEnabled: true,
              llmMaxQps: 3.5,
              llmBurst: 7,
            },
            desiredSpec: {
              queueLen: 128,
              workers: 12,
              batchSize: 24,
              batchWindow: '1.5s',
              sweepInterval: '4s',
              llmRateLimitEnabled: false,
              llmMaxQps: 0,
              llmBurst: 0,
            },
            desiredRevision: {
              version: '9',
              updatedAt: '2026-06-18T12:00:00Z',
              updatedBy: 'admin-1',
              updateReason: 'burst tuning',
              bootstrapSnapshotVersion: 'cfg-v3',
              specVersion: 1,
              lastKnownGoodVersion: '8',
            },
            summary: {
              desiredVersion: '9',
              liveInstances: 2,
              staleInstances: 1,
              expiredInstances: 0,
              degradedInstances: 1,
              fullyAppliedInstances: 1,
              fullyConverged: false,
            },
            instances: [
              {
                instanceId: 'node-a',
                bootId: 'boot-a',
                desiredVersion: '9',
                observedDesiredVersion: '9',
                runnerEffectiveVersion: '9',
                limiterEffectiveVersion: '9',
                attemptedRunnerVersion: '9',
                attemptedLimiterVersion: '9',
                runnerApplyStatus: 'RUNTIME_APPLY_STATUS_APPLIED',
                limiterApplyStatus: 'RUNTIME_APPLY_STATUS_APPLIED',
                queueDepth: 4,
                queueCapacityTarget: 128,
                queueCapacityEffective: 128,
                queueResizePending: false,
                inFlight: 3,
                degradedReason: '',
                lastAppliedAt: '2026-06-18T12:00:01Z',
                lastReconciledAt: '2026-06-18T12:00:02Z',
                lastSeenAt: '2026-06-18T12:00:03Z',
                appliedSpec: {
                  queueLen: 128,
                  workers: 12,
                  batchSize: 24,
                  batchWindow: '1.5s',
                  sweepInterval: '4s',
                  llmRateLimitEnabled: false,
                  llmMaxQps: 0,
                  llmBurst: 0,
                },
              },
            ],
            history: [
              {
                revision: {
                  version: '9',
                  updatedAt: '2026-06-18T12:00:00Z',
                  updatedBy: 'admin-1',
                  updateReason: 'burst tuning',
                  bootstrapSnapshotVersion: 'cfg-v3',
                  specVersion: 1,
                  lastKnownGoodVersion: '8',
                },
                operationType: 'update',
                riskLevel: 'normal',
                sourceVersion: '8',
                targetVersion: '9',
                rollbackLineage: '',
              },
            ],
          },
        }),
      ),
    )

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const runtime = await qc.fetchQuery(enrichmentRuntimeQuery())

    expect(runtime.desiredSpec.batchWindowSeconds).toBe(1.5)
    expect(runtime.bootstrapDefault.sweepIntervalSeconds).toBe(5)
    expect(runtime.instances[0]?.appliedSpec.batchWindowSeconds).toBe(1.5)
    expect(runtime.history[0]).toMatchObject({
      operationType: 'update',
      riskLevel: 'normal',
      sourceVersion: '8',
      targetVersion: '9',
    })
  })
})

describe('useUpdateEnrichmentRuntime', () => {
  it('writes normalized runtime into the console cache on success', async () => {
    server.use(
      http.put('/fb/v1/console/enrichment-runtime', async ({ request }) => {
        const body = (await request.json()) as {
          expectedVersion: string
          spec: {
            queueLen: number
            workers: number
            batchSize: number
            batchWindow: string
            sweepInterval: string
            llmRateLimitEnabled: boolean
            llmMaxQps: number
            llmBurst: number
          }
          updateReason: string
        }
        return HttpResponse.json({
          runtime: {
            bootstrapDefault: body.spec,
            desiredSpec: body.spec,
            desiredRevision: {
              version: '10',
              updatedAt: '2026-06-18T12:30:00Z',
              updatedBy: 'admin-1',
              updateReason: body.updateReason,
              bootstrapSnapshotVersion: 'cfg-v3',
              specVersion: 1,
              lastKnownGoodVersion: body.expectedVersion,
            },
            summary: {
              desiredVersion: '10',
              liveInstances: 1,
              staleInstances: 0,
              expiredInstances: 0,
              degradedInstances: 0,
              fullyAppliedInstances: 1,
              fullyConverged: true,
            },
            instances: [],
            history: [],
          },
        })
      }),
    )

    const { qc, wrapper } = wrap()
    const { result } = renderHook(() => useUpdateEnrichmentRuntime(), { wrapper })

    result.current.mutate({
      expectedVersion: '9',
      updateReason: 'raise capacity',
      spec: {
        queueLen: 256,
        workers: 32,
        batchSize: 64,
        batchWindowSeconds: 2,
        sweepIntervalSeconds: 6,
        llmRateLimitEnabled: true,
        llmMaxQps: 8,
        llmBurst: 16,
      },
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(qc.getQueryData(['console', 'enrichment-runtime'])).toMatchObject({
      desiredSpec: {
        queueLen: 256,
        workers: 32,
        batchSize: 64,
        batchWindowSeconds: 2,
        sweepIntervalSeconds: 6,
        llmRateLimitEnabled: true,
        llmMaxQps: 8,
        llmBurst: 16,
      },
      desiredRevision: {
        version: '10',
        updateReason: 'raise capacity',
        lastKnownGoodVersion: '9',
      },
    })
  })
})

describe('runtime mutation routes', () => {
  it('posts reset requests to the slash route and updates cache', async () => {
    let seenPath = ''
    server.use(
      http.post('/fb/v1/console/enrichment-runtime/reset', async ({ request }) => {
        seenPath = new URL(request.url).pathname
        return HttpResponse.json({
          runtime: {
            bootstrapDefault: {
              queueLen: 64,
              workers: 8,
              batchSize: 16,
              batchWindow: '2s',
              sweepInterval: '5s',
              llmRateLimitEnabled: true,
              llmMaxQps: 3.5,
              llmBurst: 7,
            },
            desiredSpec: {
              queueLen: 64,
              workers: 8,
              batchSize: 16,
              batchWindow: '2s',
              sweepInterval: '5s',
              llmRateLimitEnabled: true,
              llmMaxQps: 3.5,
              llmBurst: 7,
            },
            desiredRevision: {
              version: '11',
              updatedBy: 'admin-1',
              updateReason: 'reset queue',
              bootstrapSnapshotVersion: 'cfg-v3',
              specVersion: 1,
              lastKnownGoodVersion: '10',
            },
            summary: { desiredVersion: '11' },
            instances: [],
            history: [],
          },
        })
      }),
    )

    const { qc, wrapper } = wrap()
    const { result } = renderHook(() => useResetEnrichmentRuntime(), { wrapper })

    result.current.mutate({
      expectedVersion: '10',
      fields: ['queue_len'],
      resetAll: false,
      updateReason: 'reset queue',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(seenPath).toBe('/fb/v1/console/enrichment-runtime/reset')
    expect(qc.getQueryData(['console', 'enrichment-runtime'])).toMatchObject({
      desiredRevision: { version: '11', updateReason: 'reset queue' },
    })
  })

  it('posts rollback requests to the slash route and updates cache', async () => {
    let seenPath = ''
    server.use(
      http.post('/fb/v1/console/enrichment-runtime/rollback', async ({ request }) => {
        seenPath = new URL(request.url).pathname
        return HttpResponse.json({
          runtime: {
            bootstrapDefault: {
              queueLen: 64,
              workers: 8,
              batchSize: 16,
              batchWindow: '2s',
              sweepInterval: '5s',
              llmRateLimitEnabled: true,
              llmMaxQps: 3.5,
              llmBurst: 7,
            },
            desiredSpec: {
              queueLen: 128,
              workers: 12,
              batchSize: 24,
              batchWindow: '1.5s',
              sweepInterval: '4s',
              llmRateLimitEnabled: false,
              llmMaxQps: 0,
              llmBurst: 0,
            },
            desiredRevision: {
              version: '12',
              updatedBy: 'admin-1',
              updateReason: 'rollback hotfix',
              bootstrapSnapshotVersion: 'cfg-v3',
              specVersion: 1,
              lastKnownGoodVersion: '11',
            },
            summary: { desiredVersion: '12' },
            instances: [],
            history: [],
          },
        })
      }),
    )

    const { qc, wrapper } = wrap()
    const { result } = renderHook(() => useRollbackEnrichmentRuntime(), { wrapper })

    result.current.mutate({
      expectedVersion: '11',
      targetVersion: '9',
      updateReason: 'rollback hotfix',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(seenPath).toBe('/fb/v1/console/enrichment-runtime/rollback')
    expect(qc.getQueryData(['console', 'enrichment-runtime'])).toMatchObject({
      desiredRevision: { version: '12', updateReason: 'rollback hotfix' },
    })
  })
})
