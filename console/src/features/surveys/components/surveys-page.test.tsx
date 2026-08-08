import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { SurveysPage } from '@/features/surveys/components/surveys-page'
import type { Member } from '@/proto/attune/v1/member'
import {
  NpsBucket,
  NpsCampaignRunStatus,
  NpsMeasurementReadiness,
  SurveyCampaignHealthCheckStatus,
  SurveyCampaignStatus,
  SurveyDeliveryStatus,
  SurveyLowScoreReviewStatus,
  SurveyLowScoreSeverity,
  SurveyRecoveryNotificationStatus,
  SurveyType,
} from '@/proto/attune/v1/survey'
import {
  defaultSurveyAnalytics,
  defaultSurveyAnalyticsTrend,
  defaultSurveyCampaignHealth,
  sampleNpsCampaignPreflight,
  sampleNpsCampaignRun,
  sampleNpsSurveyCampaign,
  sampleSurveyCampaign,
  sampleSurveyInvitation,
  sampleSurveyLowScoreReview,
  sampleSurveyRecipientPreview,
  sampleSurveyResponse,
} from '@/testing/mocks/handlers'
import { server } from '@/testing/mocks/server'
import { fireEvent, renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const triggerBlobDownloadMock = vi.hoisted(() => vi.fn())
vi.mock('@/lib/blob-download', () => ({ triggerBlobDownload: triggerBlobDownloadMock }))

let scrollIntoView: ReturnType<typeof vi.fn>

beforeEach(() => {
  scrollIntoView = vi.fn()
  Object.defineProperty(Element.prototype, 'scrollIntoView', {
    configurable: true,
    value: scrollIntoView,
  })
  vi.mocked(toast.success).mockClear()
  vi.mocked(toast.error).mockClear()
  triggerBlobDownloadMock.mockReset()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('SurveysPage', () => {
  it('creates NPS campaigns with selectable audience and owner controls', async () => {
    const createCalls: unknown[] = []
    const npsOwner: Member = {
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
      http.get('/fb/v1/console/cohort-sync/cohorts', () =>
        HttpResponse.json({
          cohorts: [
            {
              id: '11111111-1111-1111-1111-111111111111',
              name: 'Strategic accounts',
              enabled: true,
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/members', () => HttpResponse.json({ members: [npsOwner] })),
      http.post('/fb/v1/console/surveys/campaigns', async ({ request }) => {
        createCalls.push(await request.json())
        return HttpResponse.json(sampleNpsSurveyCampaign, { status: 201 })
      }),
    )

    const { user } = renderWithProviders(<SurveysPage />)

    expect(await screen.findByTestId('survey-type')).toBeInTheDocument()
    await user.click(screen.getByTestId('survey-type'))
    await user.click(await screen.findByRole('option', { name: 'NPS' }))
    expect(screen.getByTestId('survey-nps-cohort')).toBeInTheDocument()
    expect(screen.queryByTestId('survey-trigger')).not.toBeInTheDocument()
    expect(screen.getByTestId('survey-nps-question')).toHaveValue(
      '您向同事推荐我们的可能性有多大？',
    )
    expect(screen.getByTestId('survey-nps-distribution')).toHaveValue('联系人邮件')
    expect(screen.getByTestId('survey-nps-contact-cooldown')).toHaveValue(90)
    expect(screen.getByTestId('survey-nps-minimum-completed-responses')).toHaveAttribute(
      'max',
      '500',
    )
    await user.type(screen.getByTestId('survey-name'), 'Relationship NPS')
    await waitFor(() => expect(screen.getByTestId('survey-nps-cohort')).not.toBeDisabled())
    expect(screen.getByTestId('survey-nps-cohort')).toHaveTextContent('选择人群')
    expect(screen.getByTestId('survey-nps-owner')).toHaveTextContent('选择负责人')
    await user.click(screen.getByTestId('survey-nps-cohort'))
    await user.click(await screen.findByRole('option', { name: 'Strategic accounts' }))
    await user.click(screen.getByTestId('survey-nps-owner'))
    await user.click(await screen.findByRole('option', { name: 'ops@example.com' }))
    await user.click(screen.getByTestId('survey-nps-recurrence'))
    await user.click(await screen.findByRole('option', { name: '每季度（90 天）' }))
    await user.click(screen.getByTestId('survey-create'))

    await waitFor(() =>
      expect(createCalls).toContainEqual(
        expect.objectContaining({
          name: 'Relationship NPS',
          surveyType: SurveyType.SURVEY_TYPE_NPS,
          triggerEvent: 'SURVEY_TRIGGER_EVENT_SCHEDULED_RUN',
          distributionMode: 'SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL',
          dedupePolicy: 'SURVEY_DEDUPE_POLICY_ONE_PER_RUN',
          triggerFilter: {},
          content: {
            title: '产品反馈',
            intro: '您的反馈将帮助我们改进。',
            question: '您向同事推荐我们的可能性有多大？',
            comment_prompt: '您给出这个评分的主要原因是什么？',
            thank_you: '感谢您的反馈。',
          },
          maxDailyInvitations: 0,
          minDaysBetweenContact: 90,
          lowScoreThreshold: 6,
          npsSettings: {
            cohortId: '11111111-1111-1111-1111-111111111111',
            detractorOwnerMemberId: '22222222-2222-2222-2222-222222222222',
            collectionDays: 14,
            maximumRunRecipients: 500,
            minimumCompletedResponses: 30,
            minimumResponseRatePercent: 10,
            samplePlanningConfidencePercent: 95,
            samplePlanningMarginOfErrorPercent: 10,
            samplePlanningExpectedResponseRatePercent: 20,
            recurrenceIntervalDays: 90,
            recurrenceContactCooldownDays: 365,
            recurrenceSamplingPercent: 25,
          },
        }),
      ),
    )
    expect(toast.success).toHaveBeenCalledWith('调查活动已创建')
  })

  it('keeps NPS audience data separate from a warm cohort-management cache', async () => {
    server.use(
      http.get('/fb/v1/console/cohort-sync/cohorts', () =>
        HttpResponse.json({
          cohorts: [
            {
              id: '11111111-1111-1111-1111-111111111111',
              name: 'Strategic accounts',
              enabled: true,
            },
          ],
        }),
      ),
    )

    const { queryClient, user } = renderWithProviders(<SurveysPage />)
    queryClient.setQueryData(
      ['cohort-sync', 'cohorts', undefined],
      [{ id: '11111111-1111-1111-1111-111111111111', name: 'Strategic accounts', enabled: true }],
    )

    await user.click(await screen.findByTestId('survey-type'))
    await user.click(await screen.findByRole('option', { name: 'NPS' }))
    await waitFor(() => expect(screen.getByTestId('survey-nps-cohort')).not.toBeDisabled())
    await user.click(screen.getByTestId('survey-nps-cohort'))

    expect(await screen.findByRole('option', { name: 'Strategic accounts' })).toBeInTheDocument()
  })

  it('labels a scheduled NPS campaign with its actual launch model', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    const campaign = await screen.findByRole('button', { name: sampleNpsSurveyCampaign.name })
    expect(campaign.parentElement).toHaveTextContent('计划运行')
  })

  it('does not offer disabled cohorts as a new NPS audience', async () => {
    server.use(
      http.get('/fb/v1/console/cohort-sync/cohorts', () =>
        HttpResponse.json({
          cohorts: [
            {
              id: '11111111-1111-1111-1111-111111111111',
              name: 'Paused accounts',
              enabled: false,
            },
          ],
        }),
      ),
    )

    const { user } = renderWithProviders(<SurveysPage />)

    await user.click(await screen.findByTestId('survey-type'))
    await user.click(await screen.findByRole('option', { name: 'NPS' }))

    await waitFor(() => expect(screen.getByTestId('survey-nps-cohort')).toBeDisabled())
  })

  it('schedules an NPS run and scopes analytics to its run ID', async () => {
    const analyticsSearches: string[] = []
    const scheduledRuns: unknown[] = []
    const scheduledAt = '2099-02-03T04:05'
    vi.stubGlobal('crypto', { randomUUID: () => 'nps-client-key' })
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({
          runs: [
            {
              ...sampleNpsCampaignRun,
              status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_CLOSED,
              scheduledAt: '2099-07-30T00:00:00Z',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/surveys/analytics', ({ request }) => {
        analyticsSearches.push(new URL(request.url).search)
        return HttpResponse.json({
          ...defaultSurveyAnalytics,
          detractorCount: 3,
          passiveCount: 1,
          promoterCount: 4,
          responseRate: 0.42,
          nps: 28,
          npsAvailable: true,
        })
      }),
      http.get('/fb/v1/console/surveys/analytics/trend', () =>
        HttpResponse.json({
          buckets: defaultSurveyAnalyticsTrend.map((bucket, index) => ({
            ...bucket,
            nps: index === 0 ? -50 : 25,
            npsAvailable: true,
            detractorCount: index === 0 ? 3 : 1,
            passiveCount: 1,
            promoterCount: index === 0 ? 1 : 3,
          })),
        }),
      ),
      http.post(
        '/fb/v1/console/surveys/campaigns/nps-campaign-1\\:scheduleNpsRun',
        async ({ request }) => {
          scheduledRuns.push(await request.json())
          return HttpResponse.json(
            { ...sampleNpsCampaignRun, id: 'nps-run-2', sequence: 4 },
            { status: 201 },
          )
        },
      ),
    )

    const { user } = renderWithProviders(<SurveysPage />)

    expect(await screen.findByTestId('nps-campaign-runs')).toHaveTextContent('第 3 次运行')
    expect(screen.getByTestId('nps-campaign-runs')).toHaveTextContent('2099')
    expect(await screen.findByTestId('nps-launch-preflight')).toHaveTextContent('人群成员')
    expect(screen.queryByTestId('nps-preflight-measurement-warning')).not.toBeInTheDocument()
    expect(screen.getByTestId('nps-launch-preflight')).toHaveTextContent(
      String(sampleNpsCampaignPreflight.plannedInvitationCount),
    )
    expect(screen.getByTestId('nps-preflight-exclusion-reasons')).toHaveTextContent('排除依据')
    expect(screen.getByTestId('nps-preflight-exclusion-contact_missing')).toHaveTextContent(
      '联系人缺失 · 9',
    )
    expect(screen.getByTestId('nps-preflight-exclusion-contact_unavailable')).toHaveTextContent(
      '联系人当前不可用 · 6',
    )
    expect(screen.getByTestId('nps-preflight-exclusion-contact_cooldown')).toHaveTextContent(
      '联系人冷却中 · 9',
    )
    expect(screen.queryByTestId('survey-settings-trigger')).not.toBeInTheDocument()
    expect(screen.getByTestId('survey-settings-nps-distribution')).toHaveValue('联系人邮件')
    expect(screen.queryByTestId('survey-preview-run')).not.toBeInTheDocument()
    expect(screen.queryByTestId('survey-hosted-link-create')).not.toBeInTheDocument()
    expect(await screen.findByRole('img', { name: 'NPS 测量趋势' })).toBeInTheDocument()
    expect(screen.getByTestId('nps-run-measurement-trend')).toHaveTextContent('42 / 96 已提交')
    expect(screen.getByTestId('nps-export-evidence')).toHaveRole('button')
    await user.click(screen.getByTestId('nps-export-evidence'))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('NPS 证据已生成并开始下载。'))
    expect(triggerBlobDownloadMock).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('nps-run-measurement-trend')).toHaveTextContent(
      '页面访问率 56% · 访问后完成率 78%',
    )
    const scoreDistribution = screen.getByRole('list', { name: '分数分布' })
    expect(scoreDistribution.querySelectorAll('li')).toHaveLength(11)
    expect(scoreDistribution.querySelector('li')).toHaveTextContent('0')
    expect(scoreDistribution.querySelectorAll('li')[10]).toHaveTextContent('10')
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent(
      '可联系',
    )
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent(
      '受众触达率',
    )
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent(
      '收件人提交率',
    )
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent(
      'NPS 仅描述已提交回复，不能单独代表整个客户群。',
    )
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent(
      '这些规则在安排时固定；更改受众、抽样或联系间隔会开始新的测量基线。',
    )
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent(
      '收集窗口14 天',
    )
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent(
      '单次收件人上限500',
    )
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent(
      '联系间隔90 天',
    )
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent(
      '实际邀请 / 规划目标96 / 245',
    )
    expect(screen.getByTestId('nps-analytics-sample-plan-shortfall')).toHaveTextContent(
      '本次运行实际邀请 96 人，低于规划目标 245 人',
    )
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent('44%')
    expect(screen.getByTestId('survey-analytics-nps-recovery-outcomes')).toHaveTextContent(
      '恢复结果证据',
    )
    expect(screen.getByTestId('survey-analytics-nps-recovery-outcomes')).toHaveTextContent('1 / 3')
    expect(screen.getByTestId('survey-analytics-nps-recovery-outcomes')).toHaveTextContent('2 / 3')
    expect(screen.getByTestId('survey-analytics-nps-recovery-outcomes')).toHaveTextContent(
      '已驳回（不计为解决）',
    )
    expect(screen.getByTestId('survey-analytics-nps-recovery-outcomes')).toHaveTextContent(
      '这些是已记录的运营动作，不代表客户结果或留存影响。',
    )
    expect(screen.getByTestId('survey-analytics-nps-recovery-timeliness')).toHaveTextContent(
      '恢复时效证据',
    )
    expect(screen.getByTestId('survey-analytics-nps-recovery-timeliness')).toHaveTextContent(
      '1 / 2',
    )
    expect(screen.getByTestId('survey-analytics-nps-recovery-timeliness')).toHaveTextContent(
      '以首次固定目标为准；后续调整截止时间不会改写结果。',
    )
    expect(screen.getByTestId('survey-analytics-nps-recovery-timeliness')).toHaveTextContent(
      '首次终态包含明确解决和驳回，且不代表客户结果或留存影响。',
    )
    expect(screen.queryByText('30 天趋势')).not.toBeInTheDocument()
    await waitFor(() =>
      expect(analyticsSearches.some((search) => search.includes('run_id=nps-run-1'))).toBe(true),
    )
    const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
    expect(screen.getByTestId('nps-schedule-time-zone')).toHaveTextContent(
      `当前浏览器时区（${browserTimeZone}）`,
    )
    fireEvent.change(screen.getByTestId('nps-schedule-at'), { target: { value: scheduledAt } })
    expect(screen.getByTestId('nps-schedule-utc-preview')).toHaveTextContent(
      new Date(scheduledAt).toISOString(),
    )
    await user.click(screen.getByTestId('nps-schedule-run'))

    await waitFor(() =>
      expect(scheduledRuns).toEqual([
        {
          campaignId: 'nps-campaign-1',
          clientRequestKey: 'nps-client-key',
          scheduledAt: new Date(scheduledAt).toISOString(),
        },
      ]),
    )
    await waitFor(() =>
      expect(analyticsSearches.some((search) => search.includes('run_id=nps-run-2'))).toBe(true),
    )
    expect(toast.success).toHaveBeenCalledWith('已安排 NPS 运行。')
  })

  it('keeps a live NPS run out of final trend and defaults metrics to the newest closed run', async () => {
    const analyticsSearches: string[] = []
    const finalizedRun = {
      ...sampleNpsCampaignRun,
      id: 'nps-run-finalized',
      sequence: 5,
      measurementKey: 'nps:v1:stable',
      status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_CLOSED,
      nps: 42,
    }
    const liveRun = {
      ...sampleNpsCampaignRun,
      id: 'nps-run-live',
      sequence: 6,
      measurementKey: 'nps:v1:stable',
      status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_COLLECTING,
      nps: -100,
    }
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({ runs: [liveRun, finalizedRun] }),
      ),
      http.get('/fb/v1/console/surveys/analytics', ({ request }) => {
        analyticsSearches.push(new URL(request.url).search)
        return HttpResponse.json(defaultSurveyAnalytics)
      }),
    )

    renderWithProviders(<SurveysPage />)

    await waitFor(() =>
      expect(analyticsSearches.some((search) => search.includes('run_id=nps-run-finalized'))).toBe(
        true,
      ),
    )
    expect(analyticsSearches.some((search) => search.includes('run_id=nps-run-live'))).toBe(false)
    expect(await screen.findByTestId('nps-measurement-point-nps-run-finalized')).toBeInTheDocument()
    expect(screen.queryByTestId('nps-measurement-point-nps-run-live')).not.toBeInTheDocument()
    expect(screen.getByTestId('nps-run-preliminary-nps-run-live')).toHaveTextContent(
      '此数值仍在收集中',
    )
    expect(screen.getByTestId('nps-measurement-live-notice')).toHaveTextContent(
      '第 6 次运行仍在收集中',
    )
    expect(screen.queryByTestId('survey-analytics-nps-preliminary')).not.toBeInTheDocument()
  })

  it('does not display an NPS zero after privacy redaction removes its response denominator', async () => {
    const redactedRun = {
      ...sampleNpsCampaignRun,
      eligibleCount: 0,
      invitationCount: 0,
      completedCount: 0,
      detractorCount: 0,
      passiveCount: 0,
      promoterCount: 0,
      nps: 0,
      npsAvailable: false,
      redactedResponseCount: 1,
    }
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({ runs: [redactedRun] }),
      ),
      http.get('/fb/v1/console/surveys/analytics', () =>
        HttpResponse.json({
          ...defaultSurveyAnalytics,
          completedCount: 0,
          detractorCount: 0,
          passiveCount: 0,
          promoterCount: 0,
          nps: 0,
          redactedResponseCount: 1,
          recoveryOutcome: {
            reviewCount: 0,
            resolvedCount: 0,
            dismissedCount: 0,
            customerContactedCount: 0,
            rootCauseRecordedCount: 0,
            actionRecordedCount: 0,
            contactedTimelinessEvidenceCount: 0,
            contactedOnTimeCount: 0,
            contactedLateCount: 0,
            terminalTimelinessEvidenceCount: 0,
            terminalOnTimeCount: 0,
            terminalLateCount: 0,
          },
          scoreDistribution: [],
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(
      await screen.findByTestId('nps-campaign-runs', {}, { timeout: 5_000 }),
    ).toBeInTheDocument()
    expect(
      await screen.findByTestId('survey-analytics-nps-unavailable', {}, { timeout: 5_000 }),
    ).toHaveTextContent('当前没有保留的 NPS 回复，无法计算 NPS。')
    expect(screen.queryByTestId('survey-analytics-nps')).not.toBeInTheDocument()
    expect(screen.getByTestId('nps-run-redacted-nps-run-1')).toHaveTextContent('已删除 1 条回复')
    expect(screen.getByTestId('survey-analytics-nps-measurement-evidence')).toHaveTextContent('-')
    expect(screen.queryByRole('list', { name: '分数分布' })).not.toBeInTheDocument()
    expect(screen.getByText('暂无分数分布。')).toBeInTheDocument()
    expect(screen.getByTestId('survey-analytics-nps-recovery-outcomes')).toHaveTextContent(
      '此测量尚未创建 detractor 跟进。',
    )
  })

  it('starts a new NPS measurement baseline when the run definition changes', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({
          runs: [
            {
              ...sampleNpsCampaignRun,
              id: 'nps-run-baseline',
              sequence: 1,
              measurementKey: 'nps:v1:baseline',
              status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_CLOSED,
            },
            {
              ...sampleNpsCampaignRun,
              id: 'nps-run-changed-definition',
              sequence: 2,
              measurementKey: 'nps:v1:changed-definition',
              status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_CLOSED,
            },
          ],
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(await screen.findByTestId('nps-measurement-change-notice')).toHaveTextContent(
      '检测到测量定义变化',
    )
    expect(screen.getByText('第 2 次运行开始新的测量基线')).toBeInTheDocument()
    expect(screen.getAllByTestId(/nps-measurement-segment-/)).toHaveLength(2)
  })

  it('hydrates persisted NPS audience settings in the campaign editor', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
    )

    const { user } = renderWithProviders(<SurveysPage />)

    await waitFor(() => {
      expect(screen.getByTestId('survey-settings-nps-cohort')).toHaveTextContent(
        sampleNpsSurveyCampaign.npsSettings?.cohortId ?? '',
      )
      expect(screen.getByTestId('survey-settings-nps-owner')).toHaveTextContent(
        sampleNpsSurveyCampaign.npsSettings?.detractorOwnerMemberId ?? '',
      )
      expect(screen.getByTestId('survey-settings-nps-minimum-completed-responses')).toHaveValue(30)
      expect(screen.getByTestId('survey-settings-nps-minimum-response-rate')).toHaveValue(10)
      expect(screen.getByTestId('survey-settings-nps-recurrence')).toHaveTextContent('手动排程')
    })

    const recipientCap = screen.getByTestId('survey-settings-nps-recipient-cap')
    await user.clear(recipientCap)
    await user.type(recipientCap, '29')

    expect(screen.getByTestId('survey-settings-nps-minimum-completed-responses')).toHaveAttribute(
      'aria-invalid',
      'true',
    )
    expect(screen.getByTestId('survey-settings-nps-measurement-validation')).toHaveTextContent(
      '最少已提交回复不能超过单次收件人上限。',
    )
    expect(screen.getByTestId('survey-settings-save')).toBeDisabled()
  })

  it('explains frozen NPS operating evidence without claiming statistical significance', async () => {
    const qualifiedRun = {
      ...sampleNpsCampaignRun,
      status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_CLOSED,
      measurementReadiness: NpsMeasurementReadiness.NPS_MEASUREMENT_READINESS_QUALIFIED,
    }
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({ runs: [qualifiedRun] }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    const qualification = await screen.findByTestId('nps-run-measurement-qualification-nps-run-1')
    expect(qualification).toHaveTextContent('达到运营门槛')
    expect(qualification).toHaveTextContent('42 / 30')
    expect(qualification).toHaveTextContent('44% / 10%')
    expect(qualification).toHaveTextContent('这不是统计显著性或总体代表性的声明。')
  })

  it('blocks NPS scheduling while another run is in progress', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({ runs: [sampleNpsCampaignRun] }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(await screen.findByTestId('nps-schedule-run')).toBeDisabled()
    expect(screen.getByTestId('nps-schedule-at')).toBeDisabled()
  })

  it('loads older NPS runs with the stable history cursor', async () => {
    const cursors: Array<string | null> = []
    const newerRun = {
      ...sampleNpsCampaignRun,
      id: 'nps-run-2',
      sequence: 2,
      status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_CLOSED,
    }
    const olderRun = {
      ...sampleNpsCampaignRun,
      id: 'nps-run-1',
      sequence: 1,
      status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_CLOSED,
    }
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('before_sequence')
        cursors.push(cursor)
        return HttpResponse.json(
          cursor === '2' ? { runs: [olderRun] } : { runs: [newerRun], nextBeforeSequence: 2 },
        )
      }),
    )

    const { user } = renderWithProviders(<SurveysPage />)

    expect((await screen.findAllByText('第 2 次运行')).length).toBeGreaterThan(0)
    await user.click(screen.getByTestId('nps-load-more-runs'))

    expect((await screen.findAllByText('第 1 次运行')).length).toBeGreaterThan(0)
    expect(cursors).toEqual([null, '2'])
  })

  it('cancels a scheduled NPS run and never offers cancellation after materialization', async () => {
    const cancelCalls: unknown[] = []
    let runs = [
      {
        ...sampleNpsCampaignRun,
        status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_SCHEDULED,
        invitationCount: 0,
        deliveredCount: 0,
        startedCount: 0,
        completedCount: 0,
      },
    ]
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({ runs }),
      ),
      http.post(
        '/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs/nps-run-1\\:cancel',
        async ({ request }) => {
          cancelCalls.push(await request.json())
          runs = [
            {
              ...runs[0],
              status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_CANCELLED,
              cancelledAt: '2026-08-05T06:00:00Z',
            },
          ]
          return HttpResponse.json(runs[0])
        },
      ),
    )

    const { user } = renderWithProviders(<SurveysPage />)

    await user.click(await screen.findByTestId('nps-cancel-run-nps-run-1'))
    expect(await screen.findByText('取消 NPS 运行？')).toBeInTheDocument()
    await user.click(screen.getByTestId('nps-cancel-run-confirm'))

    await waitFor(() =>
      expect(cancelCalls).toEqual([{ campaignId: 'nps-campaign-1', runId: 'nps-run-1' }]),
    )
    expect(await screen.findByText('已取消')).toBeInTheDocument()
    expect(screen.queryByTestId('nps-cancel-run-nps-run-1')).not.toBeInTheDocument()
    expect(toast.success).toHaveBeenCalledWith('已取消 NPS 运行。')
  })

  it('renders an actionable localized reason for a failed NPS run', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({
          runs: [
            {
              ...sampleNpsCampaignRun,
              status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_FAILED,
              failureReason: 'no_eligible_recipients',
              evaluatedCount: 12,
              eligibleCount: 0,
              invitationCount: 0,
            },
          ],
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(
      await screen.findByText(
        '安排时间已没有符合资格的联系人。请更新人群或联系许可后重新安排运行。',
      ),
    ).toBeInTheDocument()
    expect(screen.getByTestId('nps-campaign-runs')).toHaveTextContent('12')
  })

  it('explains when an NPS campaign was archived during materialization', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({
          runs: [
            {
              ...sampleNpsCampaignRun,
              status: NpsCampaignRunStatus.NPS_CAMPAIGN_RUN_STATUS_FAILED,
              failureReason: 'campaign_not_active',
              evaluatedCount: 0,
              eligibleCount: 0,
              invitationCount: 0,
            },
          ],
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(
      await screen.findByText('活动已归档或停用，未创建邀请。恢复活动后重新安排运行。'),
    ).toBeInTheDocument()
  })

  it('blocks NPS scheduling when email delivery readiness fails', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/health', () =>
        HttpResponse.json({
          ...defaultSurveyCampaignHealth,
          campaignId: sampleNpsSurveyCampaign.id,
          checks: [
            {
              id: 'delivery-readiness',
              status: SurveyCampaignHealthCheckStatus.SURVEY_CAMPAIGN_HEALTH_CHECK_STATUS_FAIL,
              title: 'Delivery path is blocked',
              summary: 'The campaign cannot safely deliver survey invitations.',
              recommendedAction: 'Configure a sender before scheduling a run.',
              evidence: 'blocker=email_sender_not_configured',
            },
          ],
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(await screen.findByTestId('nps-schedule-run')).toBeDisabled()
    expect(screen.getByTestId('nps-schedule-at')).toBeDisabled()
  })

  it('shows the aggregate launch preflight and blocks scheduling when its delivery gate fails', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({ runs: [] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-preflight', () =>
        HttpResponse.json({
          ...sampleNpsCampaignPreflight,
          deliveryReady: false,
          deliveryBlocker: 'email_sender_not_configured',
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    const preflight = await screen.findByTestId('nps-launch-preflight')
    expect(preflight).toHaveTextContent('投递阻断')
    expect(preflight).toHaveTextContent('人群成员')
    expect(preflight).toHaveTextContent('发件人未配置')
    expect(screen.getByTestId('nps-schedule-run')).toBeDisabled()
    expect(screen.getByTestId('nps-schedule-at')).toBeDisabled()
  })

  it('warns when the current NPS preflight cannot reach its frozen completion threshold', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-runs', () =>
        HttpResponse.json({ runs: [] }),
      ),
      http.get('/fb/v1/console/surveys/campaigns/nps-campaign-1/nps-preflight', () =>
        HttpResponse.json({
          ...sampleNpsCampaignPreflight,
          maximumRunRecipients: 100,
          minimumCompletedResponses: 30,
          plannedInvitationCount: 24,
          plannedInvitationCountBelowMinimumCompletedResponses: true,
          samplePlanningTargetExceedsRecipientCap: true,
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(await screen.findByTestId('nps-preflight-measurement-warning')).toHaveTextContent(
      '本次预检最多邀请 24 人，低于最少已提交回复 30。',
    )
    expect(screen.getByTestId('nps-preflight-sample-plan-warning')).toHaveTextContent(
      '当前预计邀请 24 人，低于规划目标 245',
    )
    expect(screen.getByTestId('nps-preflight-sample-plan-cap-warning')).toHaveTextContent(
      '规划目标 245 人超过单次收件人上限 100',
    )
    expect(screen.getByTestId('nps-schedule-run')).toBeEnabled()
    expect(screen.getByTestId('nps-schedule-at')).toBeEnabled()
  })

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
    expect(screen.getAllByText('邮件已打开').length).toBeGreaterThan(0)
    expect(screen.getAllByText('已访问问卷页面').length).toBeGreaterThan(0)
    expect(screen.getAllByText('已过期').length).toBeGreaterThan(0)
    expect(screen.getByText('运营洞察')).toBeInTheDocument()
    await waitFor(() =>
      expect(screen.getByTestId('survey-campaign-health')).toHaveTextContent(
        `${defaultSurveyCampaignHealth.readinessScore}/100`,
      ),
    )
    expect(screen.getByTestId('survey-campaign-health')).toHaveTextContent('活动健康诊断')
    expect(screen.getByTestId('survey-campaign-health')).toHaveTextContent('页面访问率')
    expect(screen.getByTestId('survey-campaign-health')).toHaveTextContent('访问后完成率')
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

  it('distinguishes explicit and legacy NPS follow-up permission states', async () => {
    const npsResponse = (id: string, followUpConsent: boolean | undefined) => ({
      ...sampleSurveyResponse,
      id,
      campaignId: sampleNpsSurveyCampaign.id,
      npsBucket: NpsBucket.NPS_BUCKET_DETRACTOR,
      followUpConsent,
      lowScoreReview: { ...sampleSurveyLowScoreReview, responseId: id },
    })
    const responses = [
      npsResponse('nps-follow-up-granted', true),
      npsResponse('nps-follow-up-not-granted', false),
      npsResponse('nps-follow-up-not-recorded', undefined),
      { ...sampleSurveyResponse, id: 'csat-follow-up-not-recorded' },
    ]
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/responses', () => HttpResponse.json({ responses })),
    )

    renderWithProviders(<SurveysPage />)

    expect(await screen.findByTestId('survey-low-score-nps-follow-up-granted')).toHaveTextContent(
      '客户允许就此反馈跟进',
    )
    expect(screen.getByTestId('survey-low-score-nps-follow-up-not-granted')).toHaveTextContent(
      '客户未允许就此反馈跟进',
    )
    expect(screen.getByTestId('survey-low-score-nps-follow-up-not-recorded')).toHaveTextContent(
      '未记录此反馈的回访许可',
    )
    expect(
      screen.getByTestId('survey-low-score-csat-follow-up-not-recorded'),
    ).not.toHaveTextContent('未记录此反馈的回访许可')
  })

  it('shows the immutable first terminal evidence separately from the review timestamp', async () => {
    server.use(
      http.get('/fb/v1/console/surveys/responses', () =>
        HttpResponse.json({
          responses: [
            {
              ...sampleSurveyResponse,
              lowScoreReview: {
                ...sampleSurveyLowScoreReview,
                status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED,
                initialDueAt: '2026-08-05T08:00:00Z',
                customerContactedAt: '2026-08-05T08:30:00Z',
                firstTerminalAt: '2026-08-05T09:00:00Z',
                reviewedAt: '2026-08-05T10:00:00Z',
              },
            },
          ],
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(await screen.findByText(/^原始目标：/)).toBeInTheDocument()
    expect(screen.getByText(/^首次联系客户：/)).toBeInTheDocument()
    expect(screen.getByText(/^首次结案：/)).toBeInTheDocument()
  })

  it('links an NPS comment to its canonical feedback signal', async () => {
    const responseID = 'nps-feedback-link'
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({ campaigns: [sampleNpsSurveyCampaign] }),
      ),
      http.get('/fb/v1/console/surveys/responses', () =>
        HttpResponse.json({
          responses: [
            {
              ...sampleSurveyResponse,
              id: responseID,
              campaignId: sampleNpsSurveyCampaign.id,
              npsBucket: NpsBucket.NPS_BUCKET_DETRACTOR,
              feedbackId: '9001',
              lowScoreReview: { ...sampleSurveyLowScoreReview, responseId: responseID },
            },
          ],
        }),
      ),
    )

    renderWithProviders(<SurveysPage />)

    expect(await screen.findByRole('link', { name: '查看反馈信号' })).toHaveAttribute(
      'href',
      '/feedback?ids=9001',
    )
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
      http.get('/fb/v1/console/surveys/campaigns', () =>
        HttpResponse.json({
          campaigns: [
            sampleSurveyCampaign,
            {
              ...sampleSurveyCampaign,
              id: 'source-link-campaign-1',
              name: 'Product form CSAT',
              distributionMode: 'SURVEY_DISTRIBUTION_MODE_SOURCE_LINK',
              triggerEvent: 'SURVEY_TRIGGER_EVENT_MANUAL_LINK',
              dedupePolicy: 'SURVEY_DEDUPE_POLICY_ONE_PER_SOURCE',
            },
          ],
        }),
      ),
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
        '/fb/v1/console/surveys/campaigns/source-link-campaign-1/hosted-links',
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
          campaignId: 'source-link-campaign-1',
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
