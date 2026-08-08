import { expect, type Route, test } from '@playwright/test'
import {
  collectConsoleDiagnostics,
  expectNoAxeViolations,
  expectNoConsoleDiagnostics,
  expectNoDocumentOverflow,
  gotoConsoleRoute,
} from './helpers'
import { installConsoleApiMocks } from './route-mocks'

const zh = {
  blockedDelivery: '\u963b\u65ad',
  campaignHealth: '\u6d3b\u52a8\u5065\u5eb7\u8bca\u65ad',
  completionRate: '\u8bbf\u95ee\u540e\u5b8c\u6210\u7387',
  eligible: '\u53ef\u53d1\u9001',
  emailOpened: '\u90ae\u4ef6\u5df2\u6253\u5f00',
  feedbackID: '\u53cd\u9988\u0020\u0049\u0044',
  lowScoreRecovery: '\u4f4e\u5206\u6062\u590d\u961f\u5217',
  needsAttention: '\u9700\u5173\u6ce8',
  npsJourney:
    '\u7ba1\u7406\u89e3\u51b3\u540e\u7684 CSAT\u3001CES \u548c\u5468\u671f\u6027 NPS \u6d3b\u52a8\u3001\u9080\u8bf7\u6f0f\u6597\u3001\u6258\u7ba1\u94fe\u63a5\u548c\u4f4e\u5206\u8ddf\u8fdb\u3002',
  pageVisitRate: '\u9875\u9762\u8bbf\u95ee\u7387',
  recipientPreview: '\u6536\u4ef6\u4eba\u9884\u68c0',
  recipientPreviewSubmit: '\u9884\u68c0\u6536\u4ef6\u4eba',
  sendTestEmail: '\u53d1\u9001\u6d4b\u8bd5\u90ae\u4ef6',
  sentByPostmark:
    '\u6d4b\u8bd5\u90ae\u4ef6\u5df2\u901a\u8fc7\u0020\u0070\u006f\u0073\u0074\u006d\u0061\u0072\u006b\u0020\u53d1\u9001',
  suppressionRate: '\u6291\u5236\u7387',
  started: '\u5df2\u8bbf\u95ee\u95ee\u5377\u9875\u9762',
  surveys: '\u6ee1\u610f\u5ea6\u8c03\u67e5',
  testEmail: '\u6d4b\u8bd5\u90ae\u4ef6',
  toEmail: '\u6536\u4ef6\u90ae\u7bb1',
} as const

const npsBrowserCampaign = {
  cohortID: 'cohort-browser-nps',
  cohortName: 'NPS browser audience',
  id: 'campaign-browser-nps',
  name: 'NPS browser campaign',
  ownerID: 'member-a11y-ops',
  runID: 'run-browser-nps',
}

test.describe('Survey browser behavior', () => {
  test('campaign health and delivery drills stay verifiable from the console', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/surveys')

    await expect(page.getByRole('heading', { level: 1, name: zh.surveys })).toBeVisible()
    await expect(page.getByRole('button', { name: 'A11y CSAT' })).toBeVisible()

    const healthCard = page.getByTestId('survey-campaign-health')
    await expect(healthCard.getByText(zh.campaignHealth)).toBeVisible()
    await expect(healthCard.getByText(zh.needsAttention)).toBeVisible()
    await expect(healthCard.getByText('80/100')).toBeVisible()
    await expect(healthCard.getByTestId('survey-campaign-health-visit-rate')).toHaveText(
      `${zh.pageVisitRate}50%`,
    )
    await expect(healthCard.getByTestId('survey-campaign-health-completion-rate')).toHaveText(
      `${zh.completionRate}100%`,
    )
    await expect(healthCard.getByText(zh.suppressionRate, { exact: true })).toHaveCount(2)
    await expect(healthCard.getByText('25%')).toBeVisible()
    await expect(healthCard.getByText(zh.lowScoreRecovery, { exact: true })).toBeVisible()
    await expect(healthCard.getByText(/suppression_rate=0\.25/)).toBeVisible()

    const analyticsCard = page.getByTestId('survey-analytics-funnel')
    await expect(analyticsCard.getByTestId('survey-analytics-opened')).toHaveText(
      `${zh.emailOpened}1`,
    )
    await expect(analyticsCard.getByTestId('survey-analytics-started')).toHaveText(`${zh.started}2`)
    await expect(analyticsCard.getByTestId('survey-analytics-visit-rate')).toHaveText(
      `${zh.pageVisitRate}50%`,
    )
    await expect(analyticsCard.getByTestId('survey-analytics-completion-rate')).toHaveText(
      `${zh.completionRate}100%`,
    )

    await page.getByLabel(zh.feedbackID).fill('feedback-101')
    await page.getByTestId('survey-preview-run').click()

    const preview = page.getByTestId('survey-preview-result')
    await expect(preview.getByText(zh.eligible, { exact: true })).toHaveCount(2)
    await expect(preview.getByText('A11y Customer')).toBeVisible()
    await expect(preview.getByText('workflow_transition \u00b7 feedback-101')).toBeVisible()
    await expect(preview.getByText(zh.blockedDelivery)).toHaveCount(0)

    await page.getByLabel(zh.toEmail).fill('qa-recipient@example.com')
    await page.getByTestId('survey-test-email-send').click()
    await expect(page.getByText(zh.sentByPostmark)).toBeVisible()

    expect(apiMocks.surveyRequests).toEqual([
      expect.objectContaining({
        method: 'POST',
        path: '/surveys/campaigns/survey-a11y-csat/recipients:preview',
        body: expect.objectContaining({
          campaignId: 'survey-a11y-csat',
          context: { workflow_category: 'closed' },
          limit: 10,
          sourceId: 'feedback-101',
          sourceType: 'feedback',
        }),
      }),
      expect.objectContaining({
        method: 'POST',
        path: '/surveys/campaigns/survey-a11y-csat:sendTestEmail',
        body: {
          campaignId: 'survey-a11y-csat',
          toEmail: 'qa-recipient@example.com',
        },
      }),
    ])
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('NPS creation, launch preflight, scheduling, and cancellation stay operable', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)
    const state = {
      campaigns: [] as Record<string, unknown>[],
      cancelRequest: null as Record<string, unknown> | null,
      createRequest: null as Record<string, unknown> | null,
      runs: [] as Record<string, unknown>[],
      scheduleRequest: null as Record<string, unknown> | null,
    }

    await installNpsCampaignRoutes(page, state)
    await gotoConsoleRoute(page, '/integrations/surveys')

    await expect(page.getByText(zh.npsJourney, { exact: true })).toBeVisible()
    await page.getByTestId('survey-name').fill(npsBrowserCampaign.name)
    await page.getByTestId('survey-type').click()
    await page.getByRole('option', { name: 'NPS', exact: true }).click()

    const create = page.getByTestId('survey-create')
    const cohort = page.getByTestId('survey-nps-cohort')
    const owner = page.getByTestId('survey-nps-owner')
    await expect(page.getByTestId('survey-nps-question')).toHaveValue(
      '\u60a8\u5411\u540c\u4e8b\u63a8\u8350\u6211\u4eec\u7684\u53ef\u80fd\u6027\u6709\u591a\u5927\uff1f',
    )
    await expect(page.getByTestId('survey-nps-contact-cooldown')).toHaveValue('90')
    await expect(page.getByTestId('survey-nps-minimum-completed-responses')).toHaveValue('30')
    await expect(page.getByTestId('survey-nps-minimum-completed-responses')).toHaveAttribute(
      'max',
      '500',
    )
    await expect(page.getByTestId('survey-nps-minimum-response-rate')).toHaveValue('10')
    await expect(create).toBeDisabled()
    await expect(cohort).toBeEnabled()
    await expect(owner).toBeEnabled()

    await cohort.click()
    await page.getByRole('option', { name: npsBrowserCampaign.cohortName, exact: true }).click()
    await owner.click()
    await page.getByRole('option', { name: 'ops@example.com', exact: true }).click()
    await page.getByTestId('survey-nps-recipient-cap').fill('29')
    await expect(create).toBeDisabled()
    await expect(page.getByTestId('survey-nps-measurement-validation')).toBeVisible()
    await page.getByTestId('survey-nps-recipient-cap').fill('500')
    await expect(create).toBeEnabled()
    await create.click()

    const campaign = page.getByRole('button', { name: npsBrowserCampaign.name, exact: true })
    await expect(campaign).toBeVisible()
    await expect(page.getByTestId('nps-launch-preflight')).toContainText('\u6295\u9012\u5c31\u7eea')
    await expect(page.getByTestId('nps-preflight-measurement-warning')).toContainText('30')
    await expect(page.getByTestId('nps-schedule-run')).toBeEnabled()

    const scheduledAtInput = '2030-01-02T03:04'
    const expectedScheduledAt = await page.evaluate(
      (value) => new Date(value).toISOString(),
      scheduledAtInput,
    )
    await page.locator('#nps-schedule-at').fill(scheduledAtInput)
    await page.getByTestId('nps-schedule-run').click()
    await expect(page.getByTestId(`nps-cancel-run-${npsBrowserCampaign.runID}`)).toBeVisible()
    await expect(page.getByTestId('nps-schedule-run')).toBeDisabled()

    await page.getByTestId(`nps-cancel-run-${npsBrowserCampaign.runID}`).click()
    await expect(page.getByTestId('nps-cancel-run-confirm')).toBeVisible()
    await page.getByTestId('nps-cancel-run-confirm').click()
    await expect(page.locator('[data-slot="dialog-content"]')).toHaveCount(0)
    await expect(page.getByTestId(`nps-cancel-run-${npsBrowserCampaign.runID}`)).toHaveCount(0)
    await expect(page.getByTestId('nps-schedule-run')).toBeEnabled()

    expect(state.createRequest).toMatchObject({
      dedupePolicy: 'SURVEY_DEDUPE_POLICY_ONE_PER_RUN',
      distributionMode: 'SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL',
      lowScoreThreshold: 6,
      maxDailyInvitations: 0,
      minDaysBetweenContact: 90,
      name: npsBrowserCampaign.name,
      npsSettings: {
        cohortId: npsBrowserCampaign.cohortID,
        collectionDays: 14,
        detractorOwnerMemberId: npsBrowserCampaign.ownerID,
        maximumRunRecipients: 500,
        minimumCompletedResponses: 30,
        minimumResponseRatePercent: 10,
        samplePlanningConfidencePercent: 95,
        samplePlanningMarginOfErrorPercent: 10,
        samplePlanningExpectedResponseRatePercent: 20,
      },
      surveyType: 'SURVEY_TYPE_NPS',
      triggerEvent: 'SURVEY_TRIGGER_EVENT_SCHEDULED_RUN',
    })
    expect(state.scheduleRequest).toMatchObject({
      campaignId: npsBrowserCampaign.id,
      clientRequestKey: expect.stringMatching(
        /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
      ),
      scheduledAt: expectedScheduledAt,
    })
    expect(state.cancelRequest).toEqual({
      campaignId: npsBrowserCampaign.id,
      runId: npsBrowserCampaign.runID,
    })
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('a finalized NPS measurement shows its recorded recovery outcomes', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)
    const state = {
      campaigns: [
        {
          content: {
            intro: 'Tell us how we did.',
            question: 'How likely are you to recommend us?',
            thankYou: 'Thanks for your feedback.',
            title: 'NPS browser campaign',
          },
          contentVersion: 1,
          createdAt: '2030-01-01T00:00:00Z',
          createdBy: 'user-a11y',
          dedupePolicy: 'SURVEY_DEDUPE_POLICY_ONE_PER_RUN',
          distributionMode: 'SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL',
          id: npsBrowserCampaign.id,
          lowScoreThreshold: 6,
          name: npsBrowserCampaign.name,
          npsSettings: {
            cohortId: npsBrowserCampaign.cohortID,
            collectionDays: 14,
            detractorOwnerMemberId: npsBrowserCampaign.ownerID,
            maximumRunRecipients: 500,
            minimumCompletedResponses: 30,
            minimumResponseRatePercent: 10,
            samplePlanningConfidencePercent: 95,
            samplePlanningMarginOfErrorPercent: 10,
            samplePlanningExpectedResponseRatePercent: 20,
          },
          status: 'SURVEY_CAMPAIGN_STATUS_ACTIVE',
          surveyType: 'SURVEY_TYPE_NPS',
          triggerEvent: 'SURVEY_TRIGGER_EVENT_SCHEDULED_RUN',
          triggerFilter: {},
          updatedAt: '2030-01-01T00:00:00Z',
          updatedBy: 'user-a11y',
        },
      ],
      cancelRequest: null as Record<string, unknown> | null,
      createRequest: null as Record<string, unknown> | null,
      runs: [
        {
          campaignId: npsBrowserCampaign.id,
          closesAt: '2030-01-16T03:04:00Z',
          completedCount: 40,
          completedResponseRate: 0.4,
          createdAt: '2030-01-02T03:04:00Z',
          deliveredCount: 100,
          eligibleCount: 100,
          evaluatedCount: 101,
          id: npsBrowserCampaign.runID,
          invitationCount: 100,
          measurementReadiness: 'NPS_MEASUREMENT_READINESS_QUALIFIED',
          minimumCompletedResponses: 30,
          minimumResponseRatePercent: 10,
          samplePlanningPopulationCount: 100,
          samplePlanningRequiredCompletedResponses: 49,
          samplePlanningInvitationTarget: 245,
          samplePlanningConfidencePercent: 95,
          samplePlanningMarginOfErrorPercent: 10,
          samplePlanningExpectedResponseRatePercent: 20,
          invitationCountBelowSamplePlanningTarget: true,
          nps: 25,
          passiveCount: 10,
          promoterCount: 20,
          responseRate: 0.4,
          hostedVisitRate: 0.45,
          completionRate: 40 / 45,
          scheduledAt: '2030-01-02T03:04:00Z',
          sequence: 1,
          startedCount: 45,
          status: 'NPS_CAMPAIGN_RUN_STATUS_CLOSED',
          updatedAt: '2030-01-16T03:04:00Z',
        },
      ],
      scheduleRequest: null as Record<string, unknown> | null,
    }

    await installNpsCampaignRoutes(page, state)
    await installNpsMeasurementAnalyticsRoute(page)
    await gotoConsoleRoute(page, '/integrations/surveys')

    await expect(page.getByTestId('nps-campaign-runs')).toContainText(`${zh.pageVisitRate}45%`)
    const outcomes = page.getByTestId('survey-analytics-nps-recovery-outcomes')
    await expect(outcomes).toContainText('1 / 3')
    await expect(outcomes).toContainText('2 / 3')
    await expect(outcomes).toContainText('已驳回（不计为解决）')
    await expect(outcomes).toContainText('不代表客户结果或留存影响')
    const timeliness = page.getByTestId('survey-analytics-nps-recovery-timeliness')
    await expect(timeliness).toContainText('恢复时效证据')
    await expect(timeliness).toContainText('1 / 2')
    await expect(timeliness).toContainText('以首次固定目标为准；后续调整截止时间不会改写结果。')
    await expect(timeliness).toContainText('首次终态包含明确解决和驳回')
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })
})

async function installNpsMeasurementAnalyticsRoute(
  page: Parameters<typeof installConsoleApiMocks>[0],
) {
  await page.route('**/fb/v1/console/surveys/analytics**', async (route) => {
    const url = new URL(route.request().url())
    if (url.searchParams.get('campaign_id') !== npsBrowserCampaign.id) {
      await route.fallback()
      return
    }
    await fulfillJSON(route, {
      completedCount: 40,
      completionRate: 0.8,
      criticalLowScoreReviewCount: 0,
      deliveredCount: 100,
      detractorCount: 3,
      expiredCount: 0,
      invitationCount: 100,
      lowScoreCount: 3,
      notStartedCount: 5,
      nps: 25,
      openLowScoreReviewCount: 1,
      openedCount: 50,
      overdueLowScoreReviewCount: 0,
      overdueRecoveryQueueCount: 0,
      passiveCount: 10,
      pendingContactRecoveryQueueCount: 0,
      pendingCustomerContactReviewCount: 0,
      pendingDeliveryCount: 0,
      promoterCount: 20,
      positiveScoreCount: 20,
      positiveScoreRate: 0.5,
      recoveryOutcome: {
        actionRecordedCount: 1,
        customerContactedCount: 2,
        dismissedCount: 1,
        resolvedCount: 1,
        reviewCount: 3,
        rootCauseRecordedCount: 2,
        contactedTimelinessEvidenceCount: 2,
        contactedOnTimeCount: 1,
        contactedLateCount: 1,
        terminalTimelinessEvidenceCount: 2,
        terminalOnTimeCount: 1,
        terminalLateCount: 1,
      },
      redactedResponseCount: 0,
      responseRate: 0.4,
      scoreDistribution: [
        { count: 3, score: 1 },
        { count: 10, score: 8 },
        { count: 20, score: 10 },
      ],
      startedCount: 45,
      startRate: 0.45,
      suppressedCount: 0,
      suppressionReasonDistribution: [],
      unassignedLowScoreReviewCount: 0,
      unassignedRecoveryQueueCount: 0,
      averageResponseSeconds: 3600,
      averageScore: 7.5,
      delayedDeliveryCount: 0,
      missingActionRecoveryQueueCount: 0,
      missingRootCauseRecoveryQueueCount: 0,
      ownerRecoveryLoads: [],
      rejectedDeliveryCount: 0,
    })
  })
}

async function installNpsCampaignRoutes(
  page: Parameters<typeof installConsoleApiMocks>[0],
  state: {
    campaigns: Record<string, unknown>[]
    cancelRequest: Record<string, unknown> | null
    createRequest: Record<string, unknown> | null
    runs: Record<string, unknown>[]
    scheduleRequest: Record<string, unknown> | null
  },
) {
  await page.route('**/fb/v1/console/cohort-sync/cohorts', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        cohorts: [
          { id: npsBrowserCampaign.cohortID, name: npsBrowserCampaign.cohortName, enabled: true },
        ],
      }),
    })
  })
  await page.route('**/fb/v1/console/surveys/campaigns**', async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    const isCampaignCollection = pathname === '/fb/v1/console/surveys/campaigns'

    if (request.method() === 'GET' && isCampaignCollection) {
      await fulfillJSON(route, { campaigns: state.campaigns })
      return
    }
    if (request.method() === 'POST' && isCampaignCollection) {
      state.createRequest = request.postDataJSON() as Record<string, unknown>
      state.campaigns = [
        {
          ...state.createRequest,
          createdAt: '2030-01-01T00:00:00Z',
          createdBy: 'user-a11y',
          contentVersion: 1,
          id: npsBrowserCampaign.id,
          updatedAt: '2030-01-01T00:00:00Z',
          updatedBy: 'user-a11y',
        },
      ]
      await fulfillJSON(route, state.campaigns[0], 201)
      return
    }
    if (
      request.method() === 'GET' &&
      pathname === `/fb/v1/console/surveys/campaigns/${npsBrowserCampaign.id}/nps-preflight`
    ) {
      await fulfillJSON(route, {
        campaignId: npsBrowserCampaign.id,
        deliveryBlocker: '',
        deliveryReady: true,
        eligibleCount: 4,
        evaluatedCount: 5,
        excludedCount: 1,
        generatedAt: '2030-01-01T00:00:00Z',
        maximumRunRecipients: 500,
        minimumCompletedResponses: 30,
        plannedInvitationCount: 4,
        plannedInvitationCountBelowMinimumCompletedResponses: true,
      })
      return
    }
    if (
      request.method() === 'GET' &&
      pathname === `/fb/v1/console/surveys/campaigns/${npsBrowserCampaign.id}/nps-runs`
    ) {
      await fulfillJSON(route, { runs: state.runs })
      return
    }
    if (
      request.method() === 'POST' &&
      pathname === `/fb/v1/console/surveys/campaigns/${npsBrowserCampaign.id}:scheduleNpsRun`
    ) {
      const scheduleRequest = request.postDataJSON() as Record<string, unknown>
      state.scheduleRequest = scheduleRequest
      const run = {
        campaignId: npsBrowserCampaign.id,
        closesAt: '2030-01-16T03:04:00Z',
        completedCount: 0,
        createdAt: '2030-01-02T03:04:00Z',
        deliveredCount: 0,
        eligibleCount: 4,
        evaluatedCount: 5,
        failureReason: '',
        id: npsBrowserCampaign.runID,
        invitationCount: 0,
        nps: 0,
        passiveCount: 0,
        promoterCount: 0,
        redactedResponseCount: 0,
        responseRate: 0,
        hostedVisitRate: 0,
        scheduledAt: scheduleRequest.scheduledAt,
        sequence: 1,
        startedCount: 0,
        status: 'NPS_CAMPAIGN_RUN_STATUS_SCHEDULED',
        updatedAt: '2030-01-02T03:04:00Z',
      }
      state.runs = [run]
      await fulfillJSON(route, run, 201)
      return
    }
    if (
      request.method() === 'POST' &&
      pathname ===
        `/fb/v1/console/surveys/campaigns/${npsBrowserCampaign.id}/nps-runs/${npsBrowserCampaign.runID}:cancel`
    ) {
      state.cancelRequest = request.postDataJSON() as Record<string, unknown>
      state.runs = state.runs.map((run) => ({
        ...run,
        cancelledAt: '2030-01-02T03:05:00Z',
        status: 'NPS_CAMPAIGN_RUN_STATUS_CANCELLED',
        updatedAt: '2030-01-02T03:05:00Z',
      }))
      await fulfillJSON(route, state.runs[0])
      return
    }
    await route.fallback()
  })
}

async function fulfillJSON(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    body: JSON.stringify(body),
    contentType: 'application/json',
    status,
  })
}
