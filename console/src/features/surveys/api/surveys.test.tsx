import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it } from 'vitest'
import {
  surveyAnalyticsInsightsQuery,
  surveyAnalyticsInsightsQueryKey,
  surveyAnalyticsQuery,
  surveyAnalyticsSegmentsQuery,
  surveyAnalyticsSegmentsQueryKey,
  surveyAnalyticsTrendQuery,
  surveyAnalyticsTrendQueryKey,
  surveyCampaignHealthQuery,
  surveyCampaignHealthQueryKey,
  surveyCampaignsQuery,
  surveyCampaignsQueryKey,
  surveyInvitationsQuery,
  surveyInvitationsQueryKey,
  surveyResponsesQuery,
  surveyResponsesQueryKey,
  useArchiveSurveyCampaign,
  useAssignSurveyLowScoreReviews,
  useBatchUpdateSurveyLowScoreReviews,
  useCreateSurveyCampaign,
  useCreateSurveyHostedLink,
  useEscalateSurveyLowScoreReviews,
  usePreviewSurveyRecipients,
  useRetrySurveyInvitationDelivery,
  useSendSurveyTestEmail,
  useUpdateSurveyCampaign,
  useUpdateSurveyLowScoreReview,
} from '@/features/surveys/api/surveys'
import { setCsrfToken } from '@/lib/api-client'
import {
  SurveyAnalyticsSegmentDimension,
  SurveyCampaignStatus,
  SurveyDedupePolicy,
  SurveyDistributionMode,
  SurveyLowScoreReviewStatus,
  SurveyLowScoreSeverity,
  SurveyRecoverySlaStatus,
  SurveyResponseStatus,
  SurveySuppressionStatus,
  SurveyTriggerEvent,
  SurveyType,
} from '@/proto/attune/v1/survey'
import {
  defaultSurveyAnalytics,
  defaultSurveyAnalyticsInsights,
  defaultSurveyAnalyticsSegments,
  defaultSurveyAnalyticsTrend,
  defaultSurveyCampaignHealth,
  sampleSurveyCampaign,
  sampleSurveyInvitation,
  sampleSurveyLowScoreReview,
  sampleSurveyRecipientPreview,
  sampleSurveyResponse,
} from '@/testing/mocks/handlers'
import { server } from '@/testing/mocks/server'

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function renderMutation<T>(hook: () => T) {
  const queryClient = makeQueryClient()
  queryClient.setQueryData([...surveyCampaignsQueryKey, '', 50], [])
  queryClient.setQueryData([...surveyInvitationsQueryKey, '', '', '', 25], [])
  queryClient.setQueryData(
    [...surveyResponsesQueryKey, '', false, '', '', '', '', '', '', '', 25],
    [],
  )
  queryClient.setQueryData([...surveyAnalyticsTrendQueryKey, '', '', ''], [])
  queryClient.setQueryData([...surveyAnalyticsSegmentsQueryKey, '', '', '', '', 8], [])
  queryClient.setQueryData([...surveyAnalyticsInsightsQueryKey, '', '', '', 6], [])
  queryClient.setQueryData(
    [...surveyCampaignHealthQueryKey, 'survey-campaign-1'],
    defaultSurveyCampaignHealth,
  )
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, ...renderHook(hook, { wrapper }) }
}

beforeEach(() => {
  setCsrfToken(null)
})

describe('survey API hooks', () => {
  it('serializes list filters for campaigns, invitations, responses, and analytics', async () => {
    const seen = new Set<string>()
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        expect(url.searchParams.get('status')).toBe(
          SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ACTIVE,
        )
        expect(url.searchParams.get('limit')).toBe('12')
        return HttpResponse.json({ campaigns: [sampleSurveyCampaign] })
      }),
      http.get('/fb/v1/console/surveys/invitations', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        expect(url.searchParams.get('campaign_id')).toBe('survey-campaign-1')
        expect(url.searchParams.get('response_status')).toBe(
          SurveyResponseStatus.SURVEY_RESPONSE_STATUS_COMPLETED,
        )
        expect(url.searchParams.get('suppression_status')).toBe(
          SurveySuppressionStatus.SURVEY_SUPPRESSION_STATUS_NOT_SUPPRESSED,
        )
        expect(url.searchParams.get('limit')).toBe('7')
        return HttpResponse.json({ invitations: [sampleSurveyInvitation] })
      }),
      http.get('/fb/v1/console/surveys/responses', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        expect(url.searchParams.get('campaign_id')).toBe('survey-campaign-1')
        expect(url.searchParams.get('low_score_only')).toBe('true')
        expect(url.searchParams.get('from')).toBe('2026-07-01T00:00:00Z')
        expect(url.searchParams.get('to')).toBe('2026-07-30T00:00:00Z')
        expect(url.searchParams.get('recovery_sla_status')).toBe(
          SurveyRecoverySlaStatus.SURVEY_RECOVERY_SLA_STATUS_OVERDUE,
        )
        expect(url.searchParams.get('recovery_blocker_reason')).toBe('owner_missing')
        expect(url.searchParams.get('review_severity')).toBe(
          SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
        )
        expect(url.searchParams.get('owner_member_id')).toBe('22222222-2222-2222-2222-222222222222')
        expect(url.searchParams.get('account_key')).toBe('acct:acme')
        expect(url.searchParams.get('limit')).toBe('9')
        return HttpResponse.json({ responses: [sampleSurveyResponse] })
      }),
      http.get('/fb/v1/console/surveys/analytics', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        expect(url.searchParams.get('campaign_id')).toBe('survey-campaign-1')
        expect(url.searchParams.get('from')).toBe('2026-07-01T00:00:00Z')
        expect(url.searchParams.get('to')).toBe('2026-07-30T00:00:00Z')
        return HttpResponse.json(defaultSurveyAnalytics)
      }),
      http.get('/fb/v1/console/surveys/analytics/trend', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        expect(url.searchParams.get('campaign_id')).toBe('survey-campaign-1')
        expect(url.searchParams.get('from')).toBe('2026-07-01T00:00:00Z')
        expect(url.searchParams.get('to')).toBe('2026-07-30T00:00:00Z')
        return HttpResponse.json({ buckets: defaultSurveyAnalyticsTrend })
      }),
      http.get('/fb/v1/console/surveys/analytics/segments', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        expect(url.searchParams.get('campaign_id')).toBe('survey-campaign-1')
        expect(url.searchParams.get('from')).toBe('2026-07-01T00:00:00Z')
        expect(url.searchParams.get('to')).toBe('2026-07-30T00:00:00Z')
        expect(url.searchParams.get('dimension')).toBe(
          SurveyAnalyticsSegmentDimension.SURVEY_ANALYTICS_SEGMENT_DIMENSION_SOURCE_TYPE,
        )
        expect(url.searchParams.get('limit')).toBe('6')
        return HttpResponse.json({ segments: defaultSurveyAnalyticsSegments })
      }),
      http.get('/fb/v1/console/surveys/analytics/insights', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        expect(url.searchParams.get('campaign_id')).toBe('survey-campaign-1')
        expect(url.searchParams.get('from')).toBe('2026-07-01T00:00:00Z')
        expect(url.searchParams.get('to')).toBe('2026-07-30T00:00:00Z')
        expect(url.searchParams.get('limit')).toBe('4')
        return HttpResponse.json({ insights: defaultSurveyAnalyticsInsights })
      }),
      http.get('/fb/v1/console/surveys/campaigns/survey-campaign-1/health', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        return HttpResponse.json(defaultSurveyCampaignHealth)
      }),
    )

    const queryClient = makeQueryClient()
    await expect(
      queryClient.fetchQuery(
        surveyCampaignsQuery(SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ACTIVE, 12),
      ),
    ).resolves.toEqual([sampleSurveyCampaign])
    await expect(
      queryClient.fetchQuery(
        surveyInvitationsQuery({
          campaignId: 'survey-campaign-1',
          responseStatus: SurveyResponseStatus.SURVEY_RESPONSE_STATUS_COMPLETED,
          suppressionStatus: SurveySuppressionStatus.SURVEY_SUPPRESSION_STATUS_NOT_SUPPRESSED,
          limit: 7,
        }),
      ),
    ).resolves.toEqual([sampleSurveyInvitation])
    await expect(
      queryClient.fetchQuery(
        surveyResponsesQuery({
          campaignId: 'survey-campaign-1',
          from: '2026-07-01T00:00:00Z',
          to: '2026-07-30T00:00:00Z',
          lowScoreOnly: true,
          recoverySlaStatus: SurveyRecoverySlaStatus.SURVEY_RECOVERY_SLA_STATUS_OVERDUE,
          recoveryBlockerReason: 'owner_missing',
          reviewSeverity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
          ownerMemberId: '22222222-2222-2222-2222-222222222222',
          accountKey: 'acct:acme',
          limit: 9,
        }),
      ),
    ).resolves.toEqual([sampleSurveyResponse])
    await expect(
      queryClient.fetchQuery(
        surveyAnalyticsQuery({
          campaignId: 'survey-campaign-1',
          from: '2026-07-01T00:00:00Z',
          to: '2026-07-30T00:00:00Z',
        }),
      ),
    ).resolves.toEqual(defaultSurveyAnalytics)
    await expect(
      queryClient.fetchQuery(
        surveyAnalyticsTrendQuery({
          campaignId: 'survey-campaign-1',
          from: '2026-07-01T00:00:00Z',
          to: '2026-07-30T00:00:00Z',
        }),
      ),
    ).resolves.toEqual(defaultSurveyAnalyticsTrend)
    await expect(
      queryClient.fetchQuery(
        surveyAnalyticsSegmentsQuery({
          campaignId: 'survey-campaign-1',
          from: '2026-07-01T00:00:00Z',
          to: '2026-07-30T00:00:00Z',
          dimension: SurveyAnalyticsSegmentDimension.SURVEY_ANALYTICS_SEGMENT_DIMENSION_SOURCE_TYPE,
          limit: 6,
        }),
      ),
    ).resolves.toEqual(defaultSurveyAnalyticsSegments)
    await expect(
      queryClient.fetchQuery(
        surveyAnalyticsInsightsQuery({
          campaignId: 'survey-campaign-1',
          from: '2026-07-01T00:00:00Z',
          to: '2026-07-30T00:00:00Z',
          limit: 4,
        }),
      ),
    ).resolves.toEqual(defaultSurveyAnalyticsInsights)
    await expect(
      queryClient.fetchQuery(surveyCampaignHealthQuery('survey-campaign-1')),
    ).resolves.toEqual(defaultSurveyCampaignHealth)
    expect(seen).toEqual(
      new Set([
        '/fb/v1/console/surveys/campaigns?limit=12&status=SURVEY_CAMPAIGN_STATUS_ACTIVE',
        '/fb/v1/console/surveys/invitations?campaign_id=survey-campaign-1&response_status=SURVEY_RESPONSE_STATUS_COMPLETED&suppression_status=SURVEY_SUPPRESSION_STATUS_NOT_SUPPRESSED&limit=7',
        '/fb/v1/console/surveys/responses?account_key=acct%3Aacme&campaign_id=survey-campaign-1&low_score_only=true&from=2026-07-01T00%3A00%3A00Z&to=2026-07-30T00%3A00%3A00Z&recovery_sla_status=SURVEY_RECOVERY_SLA_STATUS_OVERDUE&recovery_blocker_reason=owner_missing&review_severity=SURVEY_LOW_SCORE_SEVERITY_CRITICAL&owner_member_id=22222222-2222-2222-2222-222222222222&limit=9',
        '/fb/v1/console/surveys/analytics?campaign_id=survey-campaign-1&from=2026-07-01T00%3A00%3A00Z&to=2026-07-30T00%3A00%3A00Z',
        '/fb/v1/console/surveys/analytics/trend?campaign_id=survey-campaign-1&from=2026-07-01T00%3A00%3A00Z&to=2026-07-30T00%3A00%3A00Z',
        '/fb/v1/console/surveys/analytics/segments?campaign_id=survey-campaign-1&from=2026-07-01T00%3A00%3A00Z&to=2026-07-30T00%3A00%3A00Z&dimension=SURVEY_ANALYTICS_SEGMENT_DIMENSION_SOURCE_TYPE&limit=6',
        '/fb/v1/console/surveys/analytics/insights?campaign_id=survey-campaign-1&from=2026-07-01T00%3A00%3A00Z&to=2026-07-30T00%3A00%3A00Z&limit=4',
        '/fb/v1/console/surveys/campaigns/survey-campaign-1/health',
      ]),
    )
  })

  it('keeps default survey queries stable when list fields are omitted', async () => {
    const seen = new Set<string>()
    server.use(
      http.get('/fb/v1/console/surveys/campaigns', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({})
      }),
      http.get('/fb/v1/console/surveys/invitations', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({})
      }),
      http.get('/fb/v1/console/surveys/responses', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({})
      }),
      http.get('/fb/v1/console/surveys/analytics', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        return HttpResponse.json(defaultSurveyAnalytics)
      }),
      http.get('/fb/v1/console/surveys/analytics/trend', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({})
      }),
      http.get('/fb/v1/console/surveys/analytics/segments', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({})
      }),
      http.get('/fb/v1/console/surveys/analytics/insights', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({})
      }),
      http.get('/fb/v1/console/surveys/campaigns//health', ({ request }) => {
        const url = new URL(request.url)
        seen.add(`${url.pathname}${url.search}`)
        return HttpResponse.json(defaultSurveyCampaignHealth)
      }),
    )

    const queryClient = makeQueryClient()
    await expect(queryClient.fetchQuery(surveyCampaignsQuery())).resolves.toEqual([])
    await expect(queryClient.fetchQuery(surveyInvitationsQuery())).resolves.toEqual([])
    await expect(queryClient.fetchQuery(surveyResponsesQuery())).resolves.toEqual([])
    await expect(queryClient.fetchQuery(surveyAnalyticsQuery())).resolves.toEqual(
      defaultSurveyAnalytics,
    )
    await expect(queryClient.fetchQuery(surveyAnalyticsTrendQuery())).resolves.toEqual([])
    await expect(queryClient.fetchQuery(surveyAnalyticsSegmentsQuery())).resolves.toEqual([])
    await expect(queryClient.fetchQuery(surveyAnalyticsInsightsQuery())).resolves.toEqual([])
    await expect(queryClient.fetchQuery(surveyCampaignHealthQuery())).resolves.toEqual(
      defaultSurveyCampaignHealth,
    )
    expect(surveyCampaignHealthQuery().enabled).toBe(false)
    expect(seen).toEqual(
      new Set([
        '/fb/v1/console/surveys/campaigns?limit=50',
        '/fb/v1/console/surveys/invitations?limit=25',
        '/fb/v1/console/surveys/responses?limit=25',
        '/fb/v1/console/surveys/analytics',
        '/fb/v1/console/surveys/analytics/trend',
        '/fb/v1/console/surveys/analytics/segments?limit=8',
        '/fb/v1/console/surveys/analytics/insights?limit=6',
        '/fb/v1/console/surveys/campaigns//health',
      ]),
    )
  })

  it('creates a campaign with CSRF and invalidates campaign analytics data', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post('/fb/v1/console/surveys/campaigns', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toMatchObject({
          name: 'Resolution CSAT',
          surveyType: SurveyType.SURVEY_TYPE_CSAT,
          triggerEvent: SurveyTriggerEvent.SURVEY_TRIGGER_EVENT_WORKFLOW_TRANSITION,
        })
        return HttpResponse.json(sampleSurveyCampaign, { status: 201 })
      }),
    )
    const { queryClient, result } = renderMutation(() => useCreateSurveyCampaign())

    result.current.mutate({
      name: 'Resolution CSAT',
      surveyType: SurveyType.SURVEY_TYPE_CSAT,
      status: SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ACTIVE,
      triggerEvent: SurveyTriggerEvent.SURVEY_TRIGGER_EVENT_WORKFLOW_TRANSITION,
      distributionMode: SurveyDistributionMode.SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL,
      dedupePolicy: SurveyDedupePolicy.SURVEY_DEDUPE_POLICY_ONE_PER_RESOLUTION,
      locale: 'zh-CN',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(queryClient.getQueryState([...surveyCampaignsQueryKey, '', 50])?.isInvalidated).toBe(
      true,
    )
    expect(
      queryClient.getQueryState([...surveyAnalyticsTrendQueryKey, '', '', ''])?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyAnalyticsSegmentsQueryKey, '', '', '', '', 8])
        ?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyAnalyticsInsightsQueryKey, '', '', '', 6])?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyCampaignHealthQueryKey, 'survey-campaign-1'])
        ?.isInvalidated,
    ).toBe(true)
  })

  it('updates a campaign through an encoded campaign URL', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.patch('/fb/v1/console/surveys/campaigns/campaign%20one', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toMatchObject({
          id: 'campaign one',
          name: 'Updated CSAT',
          status: SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ACTIVE,
          triggerEvent: SurveyTriggerEvent.SURVEY_TRIGGER_EVENT_WORKFLOW_TRANSITION,
          distributionMode: SurveyDistributionMode.SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL,
          dedupePolicy: SurveyDedupePolicy.SURVEY_DEDUPE_POLICY_ONE_PER_RESOLUTION,
          samplingPercent: 80,
          minDaysBetweenContact: 21,
        })
        return HttpResponse.json({
          ...sampleSurveyCampaign,
          id: 'campaign one',
          name: 'Updated CSAT',
          samplingPercent: 80,
          minDaysBetweenContact: 21,
        })
      }),
    )
    const { queryClient, result } = renderMutation(() => useUpdateSurveyCampaign())

    result.current.mutate({
      id: 'campaign one',
      name: 'Updated CSAT',
      status: SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ACTIVE,
      triggerEvent: SurveyTriggerEvent.SURVEY_TRIGGER_EVENT_WORKFLOW_TRANSITION,
      distributionMode: SurveyDistributionMode.SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL,
      dedupePolicy: SurveyDedupePolicy.SURVEY_DEDUPE_POLICY_ONE_PER_RESOLUTION,
      samplingPercent: 80,
      minDaysBetweenContact: 21,
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(queryClient.getQueryState([...surveyCampaignsQueryKey, '', 50])?.isInvalidated).toBe(
      true,
    )
    expect(
      queryClient.getQueryState([...surveyCampaignHealthQueryKey, 'survey-campaign-1'])
        ?.isInvalidated,
    ).toBe(true)
  })

  it('archives campaigns, previews recipients, sends test email, creates hosted links, and retries invitations through encoded URLs', async () => {
    setCsrfToken('csrf-token')
    const calls: Array<{ type: string; body?: unknown }> = []
    server.use(
      http.post(
        '/fb/v1/console/surveys/campaigns/campaign%20one\\:archive',
        async ({ request }) => {
          calls.push({ type: 'archive', body: await request.json() })
          expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
          return HttpResponse.json({
            ...sampleSurveyCampaign,
            id: 'campaign one',
            status: SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ARCHIVED,
          })
        },
      ),
      http.post(
        '/fb/v1/console/surveys/campaigns/campaign%20one/hosted-links',
        async ({ request }) => {
          calls.push({ type: 'hosted-link', body: await request.json() })
          expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
          return HttpResponse.json({
            ...sampleSurveyInvitation,
            campaignId: 'campaign one',
            publicUrl: '/surveys/token-space',
          })
        },
      ),
      http.post(
        '/fb/v1/console/surveys/campaigns/campaign%20one/recipients\\:preview',
        async ({ request }) => {
          calls.push({ type: 'recipient-preview', body: await request.json() })
          expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
          return HttpResponse.json({
            ...sampleSurveyRecipientPreview,
            campaignId: 'campaign one',
          })
        },
      ),
      http.post(
        '/fb/v1/console/surveys/campaigns/campaign%20one\\:sendTestEmail',
        async ({ request }) => {
          calls.push({ type: 'test-email', body: await request.json() })
          expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
          return HttpResponse.json({
            ok: true,
            provider: 'postmark',
            sentAt: '2026-07-30T12:00:00Z',
          })
        },
      ),
      http.post(
        '/fb/v1/console/surveys/invitations/invitation%2Fslash\\:retry',
        async ({ request }) => {
          calls.push({ type: 'retry', body: await request.json() })
          expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
          return HttpResponse.json({
            ...sampleSurveyInvitation,
            id: 'invitation/slash',
            deliveryRetryable: true,
          })
        },
      ),
    )

    const archive = renderMutation(() => useArchiveSurveyCampaign())
    archive.result.current.mutate('campaign one')
    await waitFor(() => expect(archive.result.current.isSuccess).toBe(true))

    const hostedLink = renderMutation(() => useCreateSurveyHostedLink())
    hostedLink.result.current.mutate({
      campaignId: 'campaign one',
      sourceType: 'feedback',
      sourceId: '101',
    })
    await waitFor(() => expect(hostedLink.result.current.isSuccess).toBe(true))

    const preview = renderMutation(() => usePreviewSurveyRecipients())
    preview.result.current.mutate({
      campaignId: 'campaign one',
      sourceType: 'feedback',
      sourceId: '101',
      context: { workflow_category: 'closed' },
      limit: 10,
    })
    await waitFor(() => expect(preview.result.current.isSuccess).toBe(true))
    expect(preview.result.current.data?.eligibleCount).toBe(1)

    const testEmail = renderMutation(() => useSendSurveyTestEmail())
    testEmail.result.current.mutate({
      campaignId: 'campaign one',
      toEmail: 'operator@example.test',
    })
    await waitFor(() => expect(testEmail.result.current.isSuccess).toBe(true))
    expect(testEmail.result.current.data?.provider).toBe('postmark')

    const retry = renderMutation(() => useRetrySurveyInvitationDelivery())
    retry.result.current.mutate('invitation/slash')
    await waitFor(() => expect(retry.result.current.isSuccess).toBe(true))

    expect(calls).toEqual([
      { type: 'archive', body: { id: 'campaign one' } },
      {
        type: 'hosted-link',
        body: { campaignId: 'campaign one', sourceType: 'feedback', sourceId: '101' },
      },
      {
        type: 'recipient-preview',
        body: {
          campaignId: 'campaign one',
          sourceType: 'feedback',
          sourceId: '101',
          context: { workflow_category: 'closed' },
          limit: 10,
        },
      },
      {
        type: 'test-email',
        body: { campaignId: 'campaign one', toEmail: 'operator@example.test' },
      },
      { type: 'retry', body: { id: 'invitation/slash' } },
    ])
    expect(
      retry.queryClient.getQueryState([...surveyInvitationsQueryKey, '', '', '', 25])
        ?.isInvalidated,
    ).toBe(true)
    expect(
      retry.queryClient.getQueryState([...surveyCampaignHealthQueryKey, 'survey-campaign-1'])
        ?.isInvalidated,
    ).toBe(true)
  })

  it('updates low-score reviews and invalidates response data', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.patch(
        '/fb/v1/console/surveys/responses/response%20one/low-score-review',
        async ({ request }) => {
          expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
          await expect(request.json()).resolves.toEqual({
            responseId: 'response one',
            status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED,
            severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH,
            customerContacted: true,
          })
          return HttpResponse.json({
            ...sampleSurveyLowScoreReview,
            responseId: 'response one',
            status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED,
            severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH,
            customerContacted: true,
          })
        },
      ),
    )
    const { queryClient, result } = renderMutation(() => useUpdateSurveyLowScoreReview())

    result.current.mutate({
      responseId: 'response one',
      status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED,
      severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH,
      customerContacted: true,
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(
      queryClient.getQueryState([
        ...surveyResponsesQueryKey,
        '',
        false,
        '',
        '',
        '',
        '',
        '',
        '',
        '',
        25,
      ])?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyAnalyticsTrendQueryKey, '', '', ''])?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyAnalyticsSegmentsQueryKey, '', '', '', '', 8])
        ?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyAnalyticsInsightsQueryKey, '', '', '', 6])?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyCampaignHealthQueryKey, 'survey-campaign-1'])
        ?.isInvalidated,
    ).toBe(true)
  })

  it('batch updates low-score reviews and invalidates recovery data', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post(
        '/fb/v1/console/surveys/responses/low-score-reviews\\:batchUpdate',
        async ({ request }) => {
          expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
          await expect(request.json()).resolves.toEqual({
            responseIds: ['response one', 'response two'],
            ownerMemberId: '22222222-2222-2222-2222-222222222222',
            status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW,
            severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
            customerContacted: true,
            dueAt: '2026-07-31T09:00:00.000Z',
          })
          return HttpResponse.json({
            reviews: [
              {
                ...sampleSurveyLowScoreReview,
                responseId: 'response one',
                ownerMemberId: '22222222-2222-2222-2222-222222222222',
              },
              {
                ...sampleSurveyLowScoreReview,
                responseId: 'response two',
                ownerMemberId: '22222222-2222-2222-2222-222222222222',
              },
            ],
          })
        },
      ),
    )
    const { queryClient, result } = renderMutation(() => useBatchUpdateSurveyLowScoreReviews())

    result.current.mutate({
      responseIds: ['response one', 'response two'],
      ownerMemberId: '22222222-2222-2222-2222-222222222222',
      status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW,
      severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
      customerContacted: true,
      dueAt: '2026-07-31T09:00:00.000Z',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(
      queryClient.getQueryState([
        ...surveyResponsesQueryKey,
        '',
        false,
        '',
        '',
        '',
        '',
        '',
        '',
        '',
        25,
      ])?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyAnalyticsTrendQueryKey, '', '', ''])?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyAnalyticsInsightsQueryKey, '', '', '', 6])?.isInvalidated,
    ).toBe(true)
  })

  it('assigns low-score reviews through the recovery assignment endpoint', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post(
        '/fb/v1/console/surveys/responses/low-score-reviews\\:assign',
        async ({ request }) => {
          expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
          await expect(request.json()).resolves.toEqual({
            responseIds: ['response one', 'response two'],
            candidateOwnerMemberIds: [
              '11111111-1111-1111-1111-111111111111',
              '22222222-2222-2222-2222-222222222222',
            ],
          })
          return HttpResponse.json({
            reviews: [
              {
                ...sampleSurveyLowScoreReview,
                responseId: 'response one',
                ownerMemberId: '11111111-1111-1111-1111-111111111111',
              },
            ],
            decisions: [
              {
                responseId: 'response one',
                ownerMemberId: '11111111-1111-1111-1111-111111111111',
                dueAt: '2026-07-31T09:00:00Z',
                severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
                escalated: true,
                reason: 'critical_same_day',
                workloadScoreBefore: 12,
                workloadScoreAfter: 43,
              },
            ],
          })
        },
      ),
    )
    const { queryClient, result } = renderMutation(() => useAssignSurveyLowScoreReviews())

    result.current.mutate({
      responseIds: ['response one', 'response two'],
      candidateOwnerMemberIds: [
        '11111111-1111-1111-1111-111111111111',
        '22222222-2222-2222-2222-222222222222',
      ],
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(
      queryClient.getQueryState([
        ...surveyResponsesQueryKey,
        '',
        false,
        '',
        '',
        '',
        '',
        '',
        '',
        '',
        25,
      ])?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyAnalyticsInsightsQueryKey, '', '', '', 6])?.isInvalidated,
    ).toBe(true)
  })

  it('escalates low-score reviews through the recovery escalation endpoint', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post(
        '/fb/v1/console/surveys/responses/low-score-reviews\\:escalate',
        async ({ request }) => {
          expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
          await expect(request.json()).resolves.toEqual({
            responseIds: ['response one'],
            dueInHours: 6,
            note: 'needs lead visibility',
          })
          return HttpResponse.json({
            reviews: [
              {
                ...sampleSurveyLowScoreReview,
                responseId: 'response one',
                severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
              },
            ],
            decisions: [
              {
                responseId: 'response one',
                previousSeverity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH,
                severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
                dueAt: '2026-07-31T09:00:00Z',
                ownerMissing: true,
                dueAtChanged: true,
                reason: 'owner_missing',
                actionTaken: 'Escalated recovery: reason=owner_missing; severity=critical.',
              },
            ],
          })
        },
      ),
    )
    const { queryClient, result } = renderMutation(() => useEscalateSurveyLowScoreReviews())

    result.current.mutate({
      responseIds: ['response one'],
      dueInHours: 6,
      note: 'needs lead visibility',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(
      queryClient.getQueryState([
        ...surveyResponsesQueryKey,
        '',
        false,
        '',
        '',
        '',
        '',
        '',
        '',
        '',
        25,
      ])?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState([...surveyAnalyticsInsightsQueryKey, '', '', '', 6])?.isInvalidated,
    ).toBe(true)
  })
})
