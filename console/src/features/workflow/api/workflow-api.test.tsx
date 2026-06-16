import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { AuditEntry, WorkflowState, WorkflowTransition } from '@/proto/attune/v1/workflow'
import { server } from '@/testing/mocks/server'
import { renderHook, waitFor } from '@/testing/test-utils'
import { useArchiveState } from './archive-state'
import { useCreateState } from './create-state'
import { auditQuery, auditQueryKey } from './list-audit'
import { workflowStatesQuery, workflowStatesQueryKey } from './list-states'
import { workflowTransitionsQuery, workflowTransitionsQueryKey } from './list-transitions'
import { useReplaceTransitions } from './replace-transitions'
import { useSeedDefaults } from './seed-defaults'
import { useBatchTransitionFeedback, useTransitionFeedback } from './transition-feedback'
import { useUpdateState } from './update-state'

const stateFixture: WorkflowState = {
  id: 'ws-1',
  name: 'Open',
  color: '#3b82f6',
  category: 'open',
  position: 0,
  isDefault: true,
  archived: false,
  createdAt: '2026-06-14T00:00:00Z',
  updatedAt: '2026-06-14T00:00:00Z',
}

const transitionFixture: WorkflowTransition = {
  id: 'wt-1',
  fromStateId: 'ws-1',
  toStateId: 'ws-2',
}

const auditFixture: AuditEntry = {
  id: 'ae-1',
  feedbackId: '42',
  entityType: 'feedback',
  fieldName: 'state',
  oldValue: 'Open',
  newValue: 'In Progress',
  comment: 'starting work',
  changedBy: 'Tester',
  createdAt: '2026-06-14T10:00:00Z',
}

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function wrapperFor(queryClient: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

describe('workflow API hooks', () => {
  describe('queries', () => {
    it('workflowStatesQuery unwraps response states', async () => {
      server.use(
        http.get('/fb/v1/console/workflow/states', () =>
          HttpResponse.json({ states: [stateFixture] }),
        ),
      )
      const qc = makeQueryClient()
      await expect(qc.fetchQuery(workflowStatesQuery())).resolves.toEqual([stateFixture])
    })

    it('workflowStatesQuery returns empty array when states is undefined', async () => {
      server.use(http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({})))
      const qc = makeQueryClient()
      await expect(qc.fetchQuery(workflowStatesQuery())).resolves.toEqual([])
    })

    it('workflowStatesQuery passes include_archived param', async () => {
      let url = ''
      server.use(
        http.get('/fb/v1/console/workflow/states', ({ request }) => {
          url = request.url
          return HttpResponse.json({ states: [] })
        }),
      )
      const qc = makeQueryClient()
      await qc.fetchQuery(workflowStatesQuery(true))
      expect(url).toContain('include_archived=true')
    })

    it('workflowStatesQuery omits include_archived param by default', async () => {
      let url = ''
      server.use(
        http.get('/fb/v1/console/workflow/states', ({ request }) => {
          url = request.url
          return HttpResponse.json({ states: [] })
        }),
      )
      const qc = makeQueryClient()
      await qc.fetchQuery(workflowStatesQuery())
      expect(url).not.toContain('include_archived')
    })

    it('workflowTransitionsQuery unwraps response transitions', async () => {
      server.use(
        http.get('/fb/v1/console/workflow/transitions', () =>
          HttpResponse.json({ transitions: [transitionFixture] }),
        ),
      )
      const qc = makeQueryClient()
      await expect(qc.fetchQuery(workflowTransitionsQuery())).resolves.toEqual([transitionFixture])
    })

    it('workflowTransitionsQuery returns empty array when transitions is undefined', async () => {
      server.use(http.get('/fb/v1/console/workflow/transitions', () => HttpResponse.json({})))
      const qc = makeQueryClient()
      await expect(qc.fetchQuery(workflowTransitionsQuery())).resolves.toEqual([])
    })

    it('auditQuery unwraps response entries', async () => {
      server.use(
        http.get('/fb/v1/console/feedback/42/audit', () =>
          HttpResponse.json({ entries: [auditFixture] }),
        ),
      )
      const qc = makeQueryClient()
      await expect(qc.fetchQuery(auditQuery('42'))).resolves.toEqual([auditFixture])
    })

    it('auditQuery returns empty array when entries is undefined', async () => {
      server.use(http.get('/fb/v1/console/feedback/42/audit', () => HttpResponse.json({})))
      const qc = makeQueryClient()
      await expect(qc.fetchQuery(auditQuery('42'))).resolves.toEqual([])
    })

    it('auditQueryKey includes feedbackId', () => {
      expect(auditQueryKey('42')).toEqual(['console', 'feedback', '42', 'audit'])
    })
  })

  describe('mutations', () => {
    it('useCreateState posts and invalidates states', async () => {
      let body: unknown
      server.use(
        http.post('/fb/v1/console/workflow/states', async ({ request }) => {
          body = await request.json()
          return HttpResponse.json({ state: stateFixture })
        }),
      )
      const qc = makeQueryClient()
      const invalidate = vi.spyOn(qc, 'invalidateQueries')
      const { result } = renderHook(() => useCreateState(), { wrapper: wrapperFor(qc) })

      result.current.mutate({ name: 'New', color: '#3b82f6', category: 'open', position: 0 })
      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(body).toEqual({ name: 'New', color: '#3b82f6', category: 'open', position: 0 })
      expect(invalidate).toHaveBeenCalledWith({ queryKey: workflowStatesQueryKey })
    })

    it('useUpdateState patches and invalidates states', async () => {
      let body: unknown
      let patchUrl = ''
      server.use(
        http.patch('/fb/v1/console/workflow/states/:id', async ({ request }) => {
          patchUrl = new URL(request.url).pathname
          body = await request.json()
          return HttpResponse.json({ state: { ...stateFixture, name: 'Renamed' } })
        }),
      )
      const qc = makeQueryClient()
      const invalidate = vi.spyOn(qc, 'invalidateQueries')
      const { result } = renderHook(() => useUpdateState(), { wrapper: wrapperFor(qc) })

      result.current.mutate({ id: 'ws-1', name: 'Renamed', color: '#ef4444' })
      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(patchUrl).toBe('/fb/v1/console/workflow/states/ws-1')
      expect(body).toEqual({ id: 'ws-1', name: 'Renamed', color: '#ef4444' })
      expect(invalidate).toHaveBeenCalledWith({ queryKey: workflowStatesQueryKey })
    })

    it('useArchiveState deletes and invalidates states and transitions', async () => {
      let deleteUrl = ''
      server.use(
        http.delete('/fb/v1/console/workflow/states/:id', ({ request }) => {
          deleteUrl = new URL(request.url).pathname
          return HttpResponse.json({})
        }),
      )
      const qc = makeQueryClient()
      const invalidate = vi.spyOn(qc, 'invalidateQueries')
      const { result } = renderHook(() => useArchiveState(), { wrapper: wrapperFor(qc) })

      result.current.mutate('ws-1')
      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(deleteUrl).toBe('/fb/v1/console/workflow/states/ws-1')
      expect(invalidate).toHaveBeenCalledWith({ queryKey: workflowStatesQueryKey })
      expect(invalidate).toHaveBeenCalledWith({ queryKey: workflowTransitionsQueryKey })
    })

    it('useSeedDefaults posts and invalidates states and transitions', async () => {
      let method = ''
      server.use(
        http.post('/fb/v1/console/workflow/seed', ({ request }) => {
          method = request.method
          return HttpResponse.json({})
        }),
      )
      const qc = makeQueryClient()
      const invalidate = vi.spyOn(qc, 'invalidateQueries')
      const { result } = renderHook(() => useSeedDefaults(), { wrapper: wrapperFor(qc) })

      result.current.mutate()
      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(method).toBe('POST')
      expect(invalidate).toHaveBeenCalledWith({ queryKey: workflowStatesQueryKey })
      expect(invalidate).toHaveBeenCalledWith({ queryKey: workflowTransitionsQueryKey })
    })

    it('useReplaceTransitions puts and invalidates transitions', async () => {
      let body: unknown
      server.use(
        http.put('/fb/v1/console/workflow/transitions', async ({ request }) => {
          body = await request.json()
          return HttpResponse.json({ transitions: [transitionFixture] })
        }),
      )
      const qc = makeQueryClient()
      const invalidate = vi.spyOn(qc, 'invalidateQueries')
      const { result } = renderHook(() => useReplaceTransitions(), { wrapper: wrapperFor(qc) })

      result.current.mutate({
        transitions: [{ fromStateId: 'ws-1', toStateId: 'ws-2' }],
      })
      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(body).toEqual({
        transitions: [{ fromStateId: 'ws-1', toStateId: 'ws-2' }],
      })
      expect(invalidate).toHaveBeenCalledWith({ queryKey: workflowTransitionsQueryKey })
    })

    it('useTransitionFeedback posts transition and invalidates feedback', async () => {
      let body: unknown
      let postUrl = ''
      server.use(
        http.post('/fb/v1/console/feedback/:id/transition', async ({ request }) => {
          postUrl = new URL(request.url).pathname
          body = await request.json()
          return HttpResponse.json({ feedbackId: '42' })
        }),
      )
      const qc = makeQueryClient()
      const invalidate = vi.spyOn(qc, 'invalidateQueries')
      const { result } = renderHook(() => useTransitionFeedback(), { wrapper: wrapperFor(qc) })

      result.current.mutate({ feedbackId: 42, toStateId: 'ws-2', comment: 'done' })
      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(postUrl).toBe('/fb/v1/console/feedback/42/transition')
      expect(body).toEqual({ toStateId: 'ws-2', comment: 'done' })
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ['console', 'feedback'] })
    })

    it('useTransitionFeedback defaults comment to empty string', async () => {
      let body: unknown
      server.use(
        http.post('/fb/v1/console/feedback/:id/transition', async ({ request }) => {
          body = await request.json()
          return HttpResponse.json({ feedbackId: '42' })
        }),
      )
      const qc = makeQueryClient()
      const { result } = renderHook(() => useTransitionFeedback(), { wrapper: wrapperFor(qc) })

      result.current.mutate({ feedbackId: 42, toStateId: 'ws-2' })
      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(body).toEqual({ toStateId: 'ws-2', comment: '' })
    })

    it('useBatchTransitionFeedback posts batch and invalidates feedback', async () => {
      let body: unknown
      server.use(
        http.post('/fb/v1/console/feedback/transition/batch', async ({ request }) => {
          body = await request.json()
          return HttpResponse.json({ succeeded: 2, failed: [] })
        }),
      )
      const qc = makeQueryClient()
      const invalidate = vi.spyOn(qc, 'invalidateQueries')
      const { result } = renderHook(() => useBatchTransitionFeedback(), {
        wrapper: wrapperFor(qc),
      })

      result.current.mutate({
        feedbackIds: ['1', '2'],
        toStateId: 'ws-2',
        comment: 'batch move',
      })
      await waitFor(() => expect(result.current.isSuccess).toBe(true))

      expect(body).toEqual({
        feedbackIds: ['1', '2'],
        toStateId: 'ws-2',
        comment: 'batch move',
      })
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ['console', 'feedback'] })
    })
  })
})
