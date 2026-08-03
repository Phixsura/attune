import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SurveysPage } from '@/features/surveys/components/surveys-page'
import type { Member } from '@/proto/attune/v1/member'
import {
  SurveyCampaignStatus,
  SurveyDeliveryStatus,
  SurveyLowScoreReviewStatus,
  SurveyLowScoreSeverity,
  SurveyRecoveryNotificationStatus,
  SurveyType,
} from '@/proto/attune/v1/survey'
import {
  defaultSurveyCampaignHealth,
  sampleSurveyCampaign,
  sampleSurveyInvitation,
  sampleSurveyLowScoreReview,
  sampleSurveyRecipientPreview,
  sampleSurveyResponse,
} from '@/testing/mocks/handlers'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

let scrollIntoView: ReturnType<typeof vi.fn>

beforeEach(() => {
  scrollIntoView = vi.fn()
  Object.defineProperty(Element.prototype, 'scrollIntoView', {
    configurable: true,
    value: scrollIntoView,
  })
  vi.mocked(toast.success).mockClear()
  vi.mocked(toast.error).mockClear()
})

describe('SurveysPage', () => {
  it('renders campaign telemetry, recent invitations, and the low-score queue', async () => {
    const responseSearches: string[] = []
    const ownerMember: Member = {
      id: '22222222-2222-2222-2222-222222222222',
      memberType: 'tenant_user',
      userId: 'ops-user',
      email: 'ops@example.com',
      role: 'member',
      roleSource: 'manual',
      invitedAt: '1783382400',
      acceptedAt: '1783382400',
    }
    server.use(
      http.get('/fb/v1/console/members', () => HttpResponse.json({ members: [ownerMember] })),
      http.get('/fb/v1/console/surveys/responses', ({ request }) => {
        const url = new URL(request.url)
        responseSearches.push(url.search)
        expect(url.searchParams.get('low_score_only')).toBe('true')
        return HttpResponse.json({ responses: [sampleSurveyResponse] })
      }),
    )

    const { user } = renderWithProviders(<SurveysPage />)

    expect(await screen.findByRole('heading', { name: '满意度调查' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Resolution CSAT' })).toBeInTheDocument()
    expect(screen.getByText('The fix helped, but it took too many messages.')).toBeInTheDocument()
    expect(screen.getByText('/surveys/token-1')).toBeInTheDocument()
    expect(screen.getAllByText('33%').length).toBeGreaterThan(0)
    expect(screen.getAllByText('100%').length).toBeGreaterThan(0)
    expect(screen.getByText('2.0')).toBeInTheDocument()
    expect(screen.getByText('2h 0m')).toBeInTheDocument()
    expect(screen.getAllByText('未开始').length).toBeGreaterThan(0)
    expect(screen.getAllByText('已打开').length).toBeGreaterThan(0)
    expect(screen.getAllByText('已过期').length).toBeGreaterThan(0)
    expect(screen.getByText('运营洞察')).toBeInTheDocument()
    await waitFor(() =>
      expect(screen.getByTestId('survey-campaign-health')).toHaveTextContent(
        `${defaultSurveyCampaignHealth.readinessScore}/100`,
      ),
    )
    expect(screen.getByTestId('survey-campaign-health')).toHaveTextContent('活动健康诊断')
    expect(screen.getByTestId('survey-campaign-health')).toHaveTextContent('受阻')
    expect(screen.getByTestId('survey-campaign-health')).toHaveTextContent('低分恢复队列')
    expect(screen.getByText('低分跟进已逾期')).toBeInTheDocument()
    expect(
      screen.getByText('建议：先分配负责人并处理逾期低分，再继续扩大调查量。'),
    ).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /打开低分队列/ }))
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' })
    expect(screen.getByText('低分恢复指挥台')).toBeInTheDocument()
    expect(screen.getByText('54/100')).toBeInTheDocument()
    expect(screen.getByText('负责人负载')).toBeInTheDocument()
    expect(screen.getByText('建议下一位负责人：ops@example.com')).toBeInTheDocument()
    expect(screen.getByText('压力分')).toBeInTheDocument()
    expect(screen.getByText('91')).toBeInTheDocument()
    expect(screen.getByText('处理逾期项并更新状态')).toBeInTheDocument()
    expect(screen.getByText('给未分配项指定负责人')).toBeInTheDocument()
    expect(screen.getByText('30 天趋势')).toBeInTheDocument()
    expect(screen.getByText('回复率趋势')).toBeInTheDocument()
    expect(screen.getByText('问题分群')).toBeInTheDocument()
    expect(screen.getByText('关注分')).toBeInTheDocument()
    expect(screen.getByText('低分率')).toBeInTheDocument()
    expect(screen.getByText('待跟进')).toBeInTheDocument()
    expect(screen.getByText('逾期跟进')).toBeInTheDocument()
    expect(screen.getByTestId('survey-low-score-focus-all')).toHaveTextContent(/全部\s*1/)
    expect(screen.getByTestId('survey-low-score-focus-overdue')).toHaveTextContent(/逾期\s*1/)
    expect(screen.getByText('账户 Acme Corp')).toBeInTheDocument()
    await user.type(screen.getByTestId('survey-low-score-account-filter'), 'acct:acme')
    await waitFor(() =>
      expect(responseSearches.some((search) => search.includes('account_key=acct%3Aacme'))).toBe(
        true,
      ),
    )
    await user.click(screen.getByTestId('survey-low-score-focus-critical'))
    await waitFor(() =>
      expect(
        responseSearches.some((search) =>
          search.includes('review_severity=SURVEY_LOW_SCORE_SEVERITY_CRITICAL'),
        ),
      ).toBe(true),
    )
    await user.click(screen.getByTestId('survey-low-score-owner-filter'))
    await user.click(await screen.findByRole('option', { name: ownerMember.email }))
    await waitFor(() =>
      expect(
        responseSearches.some((search) =>
          search.includes('owner_member_id=22222222-2222-2222-2222-222222222222'),
        ),
      ).toBe(true),
    )
    await user.click(screen.getByTestId('survey-low-score-focus-unassigned'))
    await waitFor(() =>
      expect(responseSearches[responseSearches.length - 1]).toContain(
        'recovery_blocker_reason=owner_missing',
      ),
    )
    expect(responseSearches[responseSearches.length - 1]).not.toContain('owner_member_id=')
    expect(screen.getByText('状态 待处理')).toBeInTheDocument()
    expect(screen.getByText('严重度 高')).toBeInTheDocument()
    expect(screen.getAllByText('SLA 逾期').length).toBeGreaterThan(0)
    expect(screen.getByText('阻塞：已逾期')).toBeInTheDocument()
    expect(screen.getByText('下一步：处理逾期项并更新状态')).toBeInTheDocument()
    expect(screen.getByText('95')).toBeInTheDocument()
    expect(screen.getByText(/^截止 /)).toBeInTheDocument()
    expect(screen.getByText('联系人冷却中')).toBeInTheDocument()
  })

  it('prioritizes active low-score reviews by due date and severity', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/responses', () =>
        HttpResponse.json({
          responses: [
            {
              ...sampleSurveyResponse,
              id: 'survey-response-later',
              comment: 'Lower urgency, later due.',
              submittedAt: '2026-07-30T00:20:00Z',
              lowScoreReview: {
                ...sampleSurveyLowScoreReview,
                responseId: 'survey-response-later',
                dueAt: '2026-08-01T00:20:00Z',
              },
            },
            {
              ...sampleSurveyResponse,
              id: 'survey-response-critical',
              score: 1,
              comment: 'Critical customer recovery needed.',
              submittedAt: '2026-07-30T00:05:00Z',
              lowScoreReview: {
                ...sampleSurveyLowScoreReview,
                responseId: 'survey-response-critical',
                severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
                dueAt: '2026-07-30T01:00:00Z',
              },
            },
          ],
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(await screen.findByText('Critical customer recovery needed.')).toBeInTheDocument()
    const cards = screen.getAllByTestId(/^survey-low-score-survey-response-/)
    expect(cards[0]).toHaveTextContent('Critical customer recovery needed.')
    expect(cards[0]).toHaveTextContent('严重度 紧急')
    expect(cards[1]).toHaveTextContent('Lower urgency, later due.')
  })

  it('surfaces automated low-score recovery escalation evidence', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/responses', () =>
        HttpResponse.json({
          responses: [
            {
              ...sampleSurveyResponse,
              lowScoreReview: {
                ...sampleSurveyLowScoreReview,
                actionTaken:
                  'Escalated recovery: reason=overdue_sla. Note: automation=survey_recovery_worker; trigger=overdue_sla',
                recoveryNotificationStatus:
                  SurveyRecoveryNotificationStatus.SURVEY_RECOVERY_NOTIFICATION_STATUS_DELIVERED,
                recoveryNotificationReason: 'overdue_sla',
                recoveryNotificationDeliveredAt: '2026-07-30T12:15:00Z',
              },
            },
          ],
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(await screen.findByText('自动升级')).toBeInTheDocument()
    expect(screen.getByText('通知已发送')).toBeInTheDocument()
    expect(screen.getAllByText('SLA 逾期').length).toBeGreaterThanOrEqual(2)
  })

  it('creates campaigns, hosted links, and low-score review updates through page actions', async () => {
    const calls: Array<{ type: string; body?: unknown }> = []
    const ownerMember: Member = {
      id: '22222222-2222-2222-2222-222222222222',
      memberType: 'tenant_user',
      userId: 'ops-user',
      email: 'ops@example.com',
      role: 'member',
      roleSource: 'manual',
      invitedAt: '1783382400',
      acceptedAt: '1783382400',
    }
    server.use(
      http.get('/fb/v1/console/members', () => HttpResponse.json({ members: [ownerMember] })),
      http.post('/fb/v1/console/surveys/campaigns', async ({ request }) => {
        calls.push({ type: 'create', body: await request.json() })
        return HttpResponse.json(
          {
            ...sampleSurveyCampaign,
            id: 'survey-campaign-new',
            name: 'Post ship CSAT',
          },
          { status: 201 },
        )
      }),
      http.patch('/fb/v1/console/surveys/campaigns/survey-campaign-1', async ({ request }) => {
        calls.push({ type: 'update', body: await request.json() })
        return HttpResponse.json({
          ...sampleSurveyCampaign,
          samplingPercent: 80,
          minDaysBetweenContact: 21,
        })
      }),
      http.get('/fb/v1/console/surveys/invitations', () =>
        HttpResponse.json({
          invitations: [
            {
              ...sampleSurveyInvitation,
              deliveryStatus: SurveyDeliveryStatus.SURVEY_DELIVERY_STATUS_DELAYED,
              deliveryRetryable: true,
            },
          ],
        }),
      ),
      http.post(
        '/fb/v1/console/surveys/campaigns/survey-campaign-1/hosted-links',
        async ({ request }) => {
          calls.push({ type: 'hosted-link', body: await request.json() })
          return HttpResponse.json({
            ...sampleSurveyInvitation,
            id: 'survey-invitation-new',
            publicUrl: '/surveys/token-new',
          })
        },
      ),
      http.post(
        '/fb/v1/console/surveys/campaigns/survey-campaign-1/recipients\\:preview',
        async ({ request }) => {
          calls.push({ type: 'recipient-preview', body: await request.json() })
          return HttpResponse.json(sampleSurveyRecipientPreview)
        },
      ),
      http.post(
        '/fb/v1/console/surveys/campaigns/survey-campaign-1\\:sendTestEmail',
        async ({ request }) => {
          calls.push({ type: 'test-email', body: await request.json() })
          return HttpResponse.json({
            ok: true,
            provider: 'postmark',
            sentAt: '2026-07-30T01:20:00Z',
          })
        },
      ),
      http.post(
        '/fb/v1/console/surveys/invitations/survey-invitation-1\\:retry',
        async ({ request }) => {
          calls.push({ type: 'retry-invitation', body: await request.json() })
          return HttpResponse.json({
            ...sampleSurveyInvitation,
            deliveryStatus: SurveyDeliveryStatus.SURVEY_DELIVERY_STATUS_PENDING,
            deliveryRetryable: true,
          })
        },
      ),
      http.patch(
        '/fb/v1/console/surveys/responses/survey-response-1/low-score-review',
        async ({ request }) => {
          calls.push({ type: 'low-score', body: await request.json() })
          return HttpResponse.json({
            ...sampleSurveyLowScoreReview,
            status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED,
          })
        },
      ),
      http.post(
        '/fb/v1/console/surveys/responses/low-score-reviews\\:batchUpdate',
        async ({ request }) => {
          calls.push({ type: 'low-score-batch', body: await request.json() })
          return HttpResponse.json({
            reviews: [
              {
                ...sampleSurveyLowScoreReview,
                status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW,
                severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
                customerContacted: true,
              },
            ],
          })
        },
      ),
      http.post(
        '/fb/v1/console/surveys/responses/low-score-reviews\\:assign',
        async ({ request }) => {
          calls.push({ type: 'low-score-assign', body: await request.json() })
          return HttpResponse.json({
            reviews: [
              {
                ...sampleSurveyLowScoreReview,
                ownerMemberId: ownerMember.id,
                status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW,
              },
            ],
            decisions: [
              {
                responseId: 'survey-response-1',
                ownerMemberId: ownerMember.id,
                dueAt: '2026-07-31T09:00:00Z',
                severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH,
                escalated: true,
                reason: 'overdue_escalation',
                workloadScoreBefore: 12,
                workloadScoreAfter: 53,
              },
            ],
          })
        },
      ),
      http.post(
        '/fb/v1/console/surveys/responses/low-score-reviews\\:escalate',
        async ({ request }) => {
          calls.push({ type: 'low-score-escalate', body: await request.json() })
          return HttpResponse.json({
            reviews: [
              {
                ...sampleSurveyLowScoreReview,
                status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW,
                severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
              },
            ],
            decisions: [
              {
                responseId: 'survey-response-1',
                previousSeverity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH,
                severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
                previousDueAt: '2026-07-30T01:00:00Z',
                dueAt: '2026-07-30T01:00:00Z',
                ownerMissing: false,
                dueAtChanged: false,
                reason: 'overdue_sla',
                actionTaken: 'Escalated recovery: reason=overdue_sla; severity=critical.',
              },
            ],
          })
        },
      ),
    )

    const { user } = renderWithProviders(<SurveysPage />)

    expect(await screen.findByRole('button', { name: 'Resolution CSAT' })).toBeInTheDocument()
    await user.clear(screen.getByTestId('survey-settings-sampling'))
    await user.type(screen.getByTestId('survey-settings-sampling'), '80')
    await user.clear(screen.getByTestId('survey-settings-cooldown'))
    await user.type(screen.getByTestId('survey-settings-cooldown'), '21')
    await user.click(screen.getByTestId('survey-settings-save'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'update',
        body: expect.objectContaining({
          id: 'survey-campaign-1',
          samplingPercent: 80,
          minDaysBetweenContact: 21,
          triggerFilter: { workflow_category: 'closed' },
        }),
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('调查活动已更新')

    await user.type(screen.getByTestId('survey-name'), 'Post ship CSAT')
    await user.click(screen.getByTestId('survey-create'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'create',
        body: expect.objectContaining({
          name: 'Post ship CSAT',
          surveyType: SurveyType.SURVEY_TYPE_CSAT,
          status: SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ACTIVE,
          triggerFilter: { workflow_category: 'closed' },
        }),
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('调查活动已创建')

    await user.type(screen.getByTestId('survey-preview-source-id'), '101')
    await user.click(screen.getByTestId('survey-preview-run'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'recipient-preview',
        body: {
          campaignId: 'survey-campaign-1',
          sourceType: 'feedback',
          sourceId: '101',
          context: { workflow_category: 'closed' },
          limit: 10,
        },
      }),
    )
    expect(await screen.findByTestId('survey-preview-result')).toHaveTextContent('可发送')
    expect(screen.getByTestId('survey-preview-result')).toHaveTextContent('就绪')
    expect(screen.getByText('联系人冷却中 · 1')).toBeInTheDocument()

    await user.type(screen.getByTestId('survey-test-email-to'), 'operator@example.test')
    await user.click(screen.getByTestId('survey-test-email-send'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'test-email',
        body: {
          campaignId: 'survey-campaign-1',
          toEmail: 'operator@example.test',
        },
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('测试邮件已通过 postmark 发送')

    await user.type(screen.getByTestId('hosted-link-source-id'), 'feedback-101')
    const createHostedLink = screen.getByTestId('survey-hosted-link-create')
    await waitFor(() => expect(createHostedLink).toBeEnabled())
    await user.click(createHostedLink)
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'hosted-link',
        body: {
          campaignId: 'survey-campaign-1',
          sourceType: 'feedback',
          sourceId: 'feedback-101',
          context: { source: 'console' },
        },
      }),
    )
    expect(await screen.findByTestId('survey-hosted-link-url')).toHaveTextContent(
      '/surveys/token-new',
    )

    await user.click(screen.getByTestId('survey-invitation-retry-survey-invitation-1'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'retry-invitation',
        body: { id: 'survey-invitation-1' },
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('调查邀请已重新入队')

    await user.type(
      screen.getByTestId('survey-low-score-root-cause-survey-response-1'),
      'Slow support handoff',
    )
    await user.type(
      screen.getByTestId('survey-low-score-action-survey-response-1'),
      'Escalated to support lead',
    )
    await user.click(screen.getByLabelText('已联系客户'))
    await user.click(screen.getByTestId('survey-low-score-save-survey-response-1'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'low-score',
        body: expect.objectContaining({
          responseId: 'survey-response-1',
          status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_OPEN,
          severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH,
          rootCause: 'Slow support handoff',
          actionTaken: 'Escalated to support lead',
          customerContacted: true,
        }),
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('低分跟进已更新')

    await user.click(screen.getByLabelText('选择当前 1 条'))
    await user.click(screen.getByTestId('survey-low-score-batch-status'))
    await user.click(await screen.findByRole('option', { name: '跟进中' }))
    await user.click(screen.getByTestId('survey-low-score-batch-severity'))
    await user.click(await screen.findByRole('option', { name: '紧急' }))
    await user.click(screen.getByLabelText('批量标记已联系客户'))
    await user.click(screen.getByTestId('survey-low-score-batch-apply'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'low-score-batch',
        body: {
          responseIds: ['survey-response-1'],
          status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW,
          severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
          customerContacted: true,
        },
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('已批量更新 1 条低分跟进')

    await user.click(screen.getByLabelText('选择当前 1 条'))
    const assignButton = screen.getByTestId('survey-low-score-assign')
    await waitFor(() => expect(assignButton).toBeEnabled())
    await user.click(assignButton)
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'low-score-assign',
        body: {
          responseIds: ['survey-response-1'],
          candidateOwnerMemberIds: [ownerMember.id],
        },
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('已智能分配 1 条低分跟进，其中 1 条升级处理')

    await user.click(screen.getByLabelText('选择当前 1 条'))
    await user.click(screen.getByTestId('survey-low-score-escalate'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'low-score-escalate',
        body: {
          responseIds: ['survey-response-1'],
        },
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('已升级 1 条低分跟进，其中 0 条收紧截止时间')

    await user.click(screen.getByTestId('survey-low-score-resolve-survey-response-1'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'low-score',
        body: expect.objectContaining({
          responseId: 'survey-response-1',
          status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED,
        }),
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('低分跟进已更新')
  })
})
