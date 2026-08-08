import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useAssignFeedback } from '@/features/feedback/api/assign-feedback'
import {
  feedbackAssignmentPolicyQuery,
  feedbackAssignmentPolicyQueryKey,
  feedbackAssignmentPolicyRevisionsQuery,
  feedbackAssignmentPolicyRevisionsQueryKey,
  useDryRunFeedbackAssignmentPolicy,
  useRestoreFeedbackAssignmentPolicy,
  useUpdateFeedbackAssignmentPolicy,
} from '@/features/feedback/api/assignment-policy'
import {
  useApplyFeedbackAssignmentRecommendations,
  useRecommendFeedbackAssignment,
} from '@/features/feedback/api/assignment-recommendations'
import { useBatchAssignFeedback } from '@/features/feedback/api/batch-assign-feedback'
import { feedbackAssignmentEscalationsQueryKey } from '@/features/feedback/api/get-feedback-assignment-escalations'
import type {
  DryRunFeedbackAssignmentPolicyRequest,
  FeedbackDetail,
  RestoreFeedbackAssignmentPolicyRequest,
  UpdateFeedbackAssignmentPolicyRequest,
} from '@/proto/attune/v1/ingest'
import { server } from '@/testing/mocks/server'

function wrapper(qc: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  )
}

describe('useAssignFeedback', () => {
  it('PATCHes owner/SLA fields and updates the detail assignment cache', async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    qc.setQueryData<FeedbackDetail>(['console', 'feedback', 'detail', '42'], {
      id: '42',
      content: 'feedback',
      source: 'api',
      type: 'bug',
      userId: 'user-1',
      pageUrl: '',
      enrichedAttrs: {},
      isUrgent: false,
      enrichmentStatus: 'done',
      createdAt: '2026-08-01T00:00:00Z',
      attachments: [],
      replyDraftEnabled: false,
      tags: [],
      allowedNextStates: [],
    })
    qc.setQueryData(feedbackAssignmentEscalationsQueryKey, { items: [] })
    let captured: {
      feedbackId?: string
      ownerMemberId?: string
      slaDueAt?: string
      note?: string
    } = {}
    server.use(
      http.patch('/fb/v1/console/feedback/:id/assignment', async ({ params, request }) => {
        captured = (await request.json()) as typeof captured
        expect(params.id).toBe('42')
        return HttpResponse.json({
          feedbackId: '42',
          owner: {
            memberId: captured.ownerMemberId,
            memberType: 'oidc_user',
            userId: 'owner-1',
            email: 'owner@example.com',
            role: 'member',
          },
          assignedAt: '2026-08-01T01:00:00Z',
          assignedBy: 'operator-1',
          slaDueAt: captured.slaDueAt,
          slaStatus: 'on_track',
          note: captured.note,
        })
      }),
    )

    const { result } = renderHook(() => useAssignFeedback('42'), { wrapper: wrapper(qc) })
    result.current.mutate({
      feedbackId: '42',
      ownerMemberId: 'member-1',
      slaDueAt: '2026-08-02T00:00:00Z',
      note: 'enterprise account',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(captured).toEqual({
      feedbackId: '42',
      ownerMemberId: 'member-1',
      slaDueAt: '2026-08-02T00:00:00Z',
      note: 'enterprise account',
    })
    expect(
      qc.getQueryData<FeedbackDetail>(['console', 'feedback', 'detail', '42'])?.assignment?.owner
        ?.email,
    ).toBe('owner@example.com')
    expect(qc.getQueryState(feedbackAssignmentEscalationsQueryKey)?.isInvalidated).toBe(true)
  })

  it('leaves an absent detail cache untouched after assignment succeeds', async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    server.use(
      http.patch('/fb/v1/console/feedback/:id/assignment', () =>
        HttpResponse.json({
          feedbackId: 'missing-detail',
          assignedAt: '2026-08-01T01:00:00Z',
          assignedBy: 'operator-1',
        }),
      ),
    )

    const { result } = renderHook(() => useAssignFeedback('missing-detail'), {
      wrapper: wrapper(qc),
    })
    result.current.mutate({ feedbackId: 'missing-detail', note: 'cache cold path' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(qc.getQueryData(['console', 'feedback', 'detail', 'missing-detail'])).toBeUndefined()
  })
})

describe('useBatchAssignFeedback', () => {
  it('POSTs bulk owner/SLA changes and invalidates feedback detail caches', async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    qc.setQueryData(['console', 'feedback'], { items: [] })
    qc.setQueryData(feedbackAssignmentEscalationsQueryKey, { items: [] })
    qc.setQueryData(['console', 'feedback', 'detail', '42'], { id: '42' })
    qc.setQueryData(['console', 'feedback', '42', 'audit'], { entries: [] })
    let captured: unknown
    server.use(
      http.post('/fb/v1/console/feedback/assignment\\:batch', async ({ request }) => {
        captured = await request.json()
        return HttpResponse.json({
          totalMatched: 2,
          succeeded: 2,
          failed: [],
        })
      }),
    )

    const { result } = renderHook(() => useBatchAssignFeedback(), { wrapper: wrapper(qc) })
    result.current.mutate({
      feedbackIds: ['42', '43'],
      ownerMemberIdSet: true,
      ownerMemberId: 'member-1',
      slaDueAtSet: true,
      slaDueAt: '2026-08-02T00:00:00Z',
      note: 'handoff',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(captured).toEqual({
      feedbackIds: ['42', '43'],
      ownerMemberIdSet: true,
      ownerMemberId: 'member-1',
      slaDueAtSet: true,
      slaDueAt: '2026-08-02T00:00:00Z',
      note: 'handoff',
    })
    expect(qc.getQueryState(['console', 'feedback'])?.isInvalidated).toBe(true)
    expect(qc.getQueryState(feedbackAssignmentEscalationsQueryKey)?.isInvalidated).toBe(true)
    expect(qc.getQueryState(['console', 'feedback', 'detail', '42'])?.isInvalidated).toBe(true)
    expect(qc.getQueryState(['console', 'feedback', '42', 'audit'])?.isInvalidated).toBe(true)
  })
})

describe('assignment recommendation hooks', () => {
  it('POSTs recommendation preview requests', async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    let captured: unknown
    server.use(
      http.post('/fb/v1/console/feedback/assignment\\:recommend', async ({ request }) => {
        captured = await request.json()
        return HttpResponse.json({
          totalMatched: 1,
          recommendations: [
            {
              feedbackId: '42',
              ruleKey: 'urgent_open',
              ruleName: 'Urgent open feedback',
              ownerLane: 'support_triage',
              severity: 'critical',
              slaHours: 24,
              recommendedSlaDueAt: '2026-08-02T00:00:00Z',
              rationale: 'urgent',
              alreadySatisfied: false,
            },
          ],
          failed: [],
        })
      }),
    )

    const { result } = renderHook(() => useRecommendFeedbackAssignment(), {
      wrapper: wrapper(qc),
    })
    result.current.mutate({ feedbackIds: ['42'] })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(captured).toEqual({ feedbackIds: ['42'] })
    expect(result.current.data?.recommendations[0]?.ruleKey).toBe('urgent_open')
  })

  it('POSTs recommendation apply requests and invalidates affected caches', async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    qc.setQueryData(['console', 'feedback'], { items: [] })
    qc.setQueryData(feedbackAssignmentEscalationsQueryKey, { items: [] })
    qc.setQueryData(['console', 'feedback', 'triage-command-center'], { lanes: [] })
    qc.setQueryData(['console', 'feedback', 'detail', '42'], { id: '42' })
    qc.setQueryData(['console', 'feedback', '42', 'audit'], { entries: [] })
    let captured: unknown
    server.use(
      http.post(
        '/fb/v1/console/feedback/assignment\\:apply-recommendations',
        async ({ request }) => {
          captured = await request.json()
          return HttpResponse.json({
            totalMatched: 1,
            succeeded: 1,
            skipped: 0,
            failed: [],
            applied: [],
          })
        },
      ),
    )

    const { result } = renderHook(() => useApplyFeedbackAssignmentRecommendations(), {
      wrapper: wrapper(qc),
    })
    result.current.mutate({
      feedbackIds: ['42'],
      ownerMemberId: 'member-1',
      note: 'policy sweep',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(captured).toEqual({
      feedbackIds: ['42'],
      ownerMemberId: 'member-1',
      note: 'policy sweep',
    })
    expect(qc.getQueryState(['console', 'feedback'])?.isInvalidated).toBe(true)
    expect(qc.getQueryState(feedbackAssignmentEscalationsQueryKey)?.isInvalidated).toBe(true)
    expect(qc.getQueryState(['console', 'feedback', 'triage-command-center'])?.isInvalidated).toBe(
      true,
    )
    expect(qc.getQueryState(['console', 'feedback', 'detail', '42'])?.isInvalidated).toBe(true)
    expect(qc.getQueryState(['console', 'feedback', '42', 'audit'])?.isInvalidated).toBe(true)
  })
})

describe('assignment policy hooks', () => {
  it('loads the tenant assignment policy', async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    server.use(
      http.get('/fb/v1/console/feedback/assignment/policy', () =>
        HttpResponse.json({
          rules: [
            {
              ruleKey: 'urgent_open',
              ruleName: 'Urgent open feedback',
              ownerLane: 'enterprise_triage',
              severity: 'critical',
              slaHours: 8,
              defaultOwnerMemberId: 'member-1',
              enabled: true,
              rationale: 'enterprise escalation',
            },
          ],
        }),
      ),
    )

    const { result } = renderHook(() => useQuery(feedbackAssignmentPolicyQuery()), {
      wrapper: wrapper(qc),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.rules[0]).toMatchObject({
      ruleKey: 'urgent_open',
      ownerLane: 'enterprise_triage',
      slaHours: 8,
      defaultOwnerMemberId: 'member-1',
    })
  })

  it('saves policy rules and refreshes recommendation inputs', async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    qc.setQueryData(feedbackAssignmentPolicyQueryKey, { rules: [] })
    qc.setQueryData(feedbackAssignmentPolicyRevisionsQueryKey, { revisions: [] })
    qc.setQueryData(feedbackAssignmentEscalationsQueryKey, { items: [] })
    qc.setQueryData(['console', 'feedback', 'triage-command-center'], { lanes: [] })
    let captured: UpdateFeedbackAssignmentPolicyRequest | undefined
    server.use(
      http.put('/fb/v1/console/feedback/assignment/policy', async ({ request }) => {
        captured = (await request.json()) as UpdateFeedbackAssignmentPolicyRequest
        return HttpResponse.json(captured)
      }),
    )

    const { result } = renderHook(() => useUpdateFeedbackAssignmentPolicy(), {
      wrapper: wrapper(qc),
    })
    result.current.mutate({
      rules: [
        {
          ruleKey: 'urgent_open',
          ruleName: 'Urgent open feedback',
          ownerLane: 'enterprise_triage',
          severity: 'critical',
          slaHours: 8,
          defaultOwnerMemberId: 'member-1',
          enabled: true,
          rationale: 'enterprise escalation',
        },
      ],
      note: 'tighten enterprise SLA',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(captured).toEqual({
      rules: [
        {
          ruleKey: 'urgent_open',
          ruleName: 'Urgent open feedback',
          ownerLane: 'enterprise_triage',
          severity: 'critical',
          slaHours: 8,
          defaultOwnerMemberId: 'member-1',
          enabled: true,
          rationale: 'enterprise escalation',
        },
      ],
      note: 'tighten enterprise SLA',
    })
    expect(qc.getQueryData(feedbackAssignmentPolicyQueryKey)).toEqual(captured)
    expect(qc.getQueryState(['console', 'feedback', 'triage-command-center'])?.isInvalidated).toBe(
      true,
    )
    expect(qc.getQueryState(feedbackAssignmentEscalationsQueryKey)?.isInvalidated).toBe(true)
    expect(qc.getQueryState(feedbackAssignmentPolicyRevisionsQueryKey)?.isInvalidated).toBe(true)
  })

  it('loads assignment policy revisions', async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    server.use(
      http.get('/fb/v1/console/feedback/assignment/policy/revisions', () =>
        HttpResponse.json({
          revisions: [
            {
              version: 2,
              updatedBy: 'admin-2',
              note: 'tighten urgent SLA',
              rules: [],
            },
          ],
        }),
      ),
    )

    const { result } = renderHook(() => useQuery(feedbackAssignmentPolicyRevisionsQuery()), {
      wrapper: wrapper(qc),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.revisions[0]).toMatchObject({ version: 2, updatedBy: 'admin-2' })
  })

  it('dry-runs assignment policy rules', async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    let captured: DryRunFeedbackAssignmentPolicyRequest | undefined
    server.use(
      http.post('/fb/v1/console/feedback/assignment/policy:dry-run', async ({ request }) => {
        captured = (await request.json()) as DryRunFeedbackAssignmentPolicyRequest
        return HttpResponse.json({
          totalMatched: 1,
          changed: 1,
          recommendations: [],
          failed: [],
          impacts: [
            {
              feedbackId: '7',
              currentRuleKey: 'urgent_open',
              currentRuleName: 'Urgent open feedback',
              currentOwnerLane: 'support_triage',
              currentSlaHours: 24,
              draftRuleKey: 'urgent_open',
              draftRuleName: 'Urgent open feedback',
              draftOwnerLane: 'enterprise_triage',
              draftSlaHours: 8,
              changed: true,
            },
          ],
        })
      }),
    )

    const { result } = renderHook(() => useDryRunFeedbackAssignmentPolicy(), {
      wrapper: wrapper(qc),
    })
    result.current.mutate({
      feedbackIds: ['7'],
      rules: [
        {
          ruleKey: 'urgent_open',
          ruleName: 'Urgent open feedback',
          ownerLane: 'enterprise_triage',
          severity: 'critical',
          slaHours: 8,
          enabled: true,
          rationale: 'enterprise escalation',
        },
      ],
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(captured?.feedbackIds).toEqual(['7'])
    expect(result.current.data?.changed).toBe(1)
  })

  it('restores assignment policy revisions and refreshes policy governance cache', async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    qc.setQueryData(feedbackAssignmentPolicyQueryKey, { rules: [], version: 2 })
    qc.setQueryData(feedbackAssignmentPolicyRevisionsQueryKey, { revisions: [] })
    qc.setQueryData(feedbackAssignmentEscalationsQueryKey, { items: [] })
    let captured: RestoreFeedbackAssignmentPolicyRequest | undefined
    server.use(
      http.post('/fb/v1/console/feedback/assignment/policy:restore', async ({ request }) => {
        captured = (await request.json()) as RestoreFeedbackAssignmentPolicyRequest
        return HttpResponse.json({ rules: [], version: 3, updatedBy: 'admin-3', note: 'restored' })
      }),
    )

    const { result } = renderHook(() => useRestoreFeedbackAssignmentPolicy(), {
      wrapper: wrapper(qc),
    })
    result.current.mutate({ version: 1, note: '' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(captured).toEqual({ version: 1, note: '' })
    expect(qc.getQueryData(feedbackAssignmentPolicyQueryKey)).toMatchObject({ version: 3 })
    expect(qc.getQueryState(feedbackAssignmentPolicyRevisionsQueryKey)?.isInvalidated).toBe(true)
    expect(qc.getQueryState(feedbackAssignmentEscalationsQueryKey)?.isInvalidated).toBe(true)
  })
})
