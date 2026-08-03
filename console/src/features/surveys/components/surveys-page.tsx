import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import {
  Archive,
  ArrowRight,
  CheckCircle2,
  CircleAlert,
  Clipboard,
  ClipboardCheck,
  Gauge,
  Loader2,
  MailCheck,
  RefreshCw,
  Send,
  ShieldAlert,
  Users,
  XCircle,
} from 'lucide-react'
import type { FormEvent, ReactNode } from 'react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import {
  type SurveyResponseFilters,
  surveyAnalyticsInsightsQuery,
  surveyAnalyticsQuery,
  surveyAnalyticsSegmentsQuery,
  surveyAnalyticsTrendQuery,
  surveyCampaignHealthQuery,
  surveyCampaignsQuery,
  surveyInvitationsQuery,
  surveyResponsesQuery,
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
import { recoveryReadinessScore } from '@/features/surveys/lib/recovery-readiness'
import { useDocumentTitle } from '@/hooks/use-document-title'
import { type Member, membersQuery } from '@/lib/members-api'
import { cn } from '@/lib/utils'
import {
  type BatchUpdateSurveyLowScoreReviewsRequest,
  type PreviewSurveyRecipientsResponse,
  type SurveyAnalytics,
  type SurveyAnalyticsInsight,
  SurveyAnalyticsInsightSeverity,
  type SurveyAnalyticsSegment,
  SurveyAnalyticsSegmentDimension,
  type SurveyAnalyticsTrendBucket,
  type SurveyCampaign,
  type SurveyCampaignHealth,
  type SurveyCampaignHealthCheck,
  SurveyCampaignHealthCheckStatus,
  SurveyCampaignHealthStatus,
  SurveyCampaignStatus,
  SurveyDedupePolicy,
  SurveyDistributionMode,
  type SurveyInvitation,
  SurveyLowScoreReviewStatus,
  SurveyLowScoreSeverity,
  type SurveyRecipientPreview,
  SurveyRecoveryNotificationStatus,
  type SurveyRecoveryOwnerLoad,
  SurveyRecoverySlaStatus,
  type SurveyResponse,
  SurveyTriggerEvent,
  SurveyType,
} from '@/proto/attune/v1/survey'

const activeCampaignStatus = SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ACTIVE
const draftCampaignStatus = SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_DRAFT
const contactEmailMode = SurveyDistributionMode.SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL
const sourceLinkMode = SurveyDistributionMode.SURVEY_DISTRIBUTION_MODE_SOURCE_LINK
const workflowTrigger = SurveyTriggerEvent.SURVEY_TRIGGER_EVENT_WORKFLOW_TRANSITION
const replySentTrigger = SurveyTriggerEvent.SURVEY_TRIGGER_EVENT_REPLY_SENT
const manualLinkTrigger = SurveyTriggerEvent.SURVEY_TRIGGER_EVENT_MANUAL_LINK
const requestResolvedTrigger = SurveyTriggerEvent.SURVEY_TRIGGER_EVENT_REQUEST_RESOLVED
const onePerSourceDedupe = SurveyDedupePolicy.SURVEY_DEDUPE_POLICY_ONE_PER_SOURCE
const onePerResolutionDedupe = SurveyDedupePolicy.SURVEY_DEDUPE_POLICY_ONE_PER_RESOLUTION
const onePerTriggerDedupe = SurveyDedupePolicy.SURVEY_DEDUPE_POLICY_ONE_PER_TRIGGER
const defaultLowScoreReviewStatus = SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_OPEN
const defaultLowScoreSeverity = SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_MEDIUM
const resolvedReviewStatus = SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED
const dismissedReviewStatus = SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_DISMISSED
const unassignedOwnerValue = 'unassigned'
const batchNoChangeValue = 'no-change'
const trendChartWidth = 560
const trendChartHeight = 96
const trendChartPad = 10
const sourceTypeSegmentDimension =
  SurveyAnalyticsSegmentDimension.SURVEY_ANALYTICS_SEGMENT_DIMENSION_SOURCE_TYPE
const criticalInsightSeverity =
  SurveyAnalyticsInsightSeverity.SURVEY_ANALYTICS_INSIGHT_SEVERITY_CRITICAL
const warningInsightSeverity =
  SurveyAnalyticsInsightSeverity.SURVEY_ANALYTICS_INSIGHT_SEVERITY_WARNING
const campaignHealthBlocked = SurveyCampaignHealthStatus.SURVEY_CAMPAIGN_HEALTH_STATUS_BLOCKED
const campaignHealthNeedsAttention =
  SurveyCampaignHealthStatus.SURVEY_CAMPAIGN_HEALTH_STATUS_NEEDS_ATTENTION
const healthCheckFail = SurveyCampaignHealthCheckStatus.SURVEY_CAMPAIGN_HEALTH_CHECK_STATUS_FAIL
const healthCheckWarn = SurveyCampaignHealthCheckStatus.SURVEY_CAMPAIGN_HEALTH_CHECK_STATUS_WARN
const recoverySLAOverdue = SurveyRecoverySlaStatus.SURVEY_RECOVERY_SLA_STATUS_OVERDUE
const recoverySLADueSoon = SurveyRecoverySlaStatus.SURVEY_RECOVERY_SLA_STATUS_DUE_SOON
const recoveryNotificationPending =
  SurveyRecoveryNotificationStatus.SURVEY_RECOVERY_NOTIFICATION_STATUS_PENDING
const recoveryNotificationDelivered =
  SurveyRecoveryNotificationStatus.SURVEY_RECOVERY_NOTIFICATION_STATUS_DELIVERED
const recoveryNotificationFailed =
  SurveyRecoveryNotificationStatus.SURVEY_RECOVERY_NOTIFICATION_STATUS_FAILED
const recoveryNotificationDead =
  SurveyRecoveryNotificationStatus.SURVEY_RECOVERY_NOTIFICATION_STATUS_DEAD
const recoveryNotificationSuppressed =
  SurveyRecoveryNotificationStatus.SURVEY_RECOVERY_NOTIFICATION_STATUS_SUPPRESSED
const recoveryBlockerOwner = 'owner_missing'
const recoveryBlockerContact = 'customer_contact_missing'
const recoveryBlockerRootCause = 'root_cause_missing'
const recoveryBlockerAction = 'action_missing'
const recoveryAutomationMarker = 'automation=survey_recovery_worker'
const allLowScoreOwnersValue = 'all'
type InsightActionTarget = 'analytics' | 'low-scores' | 'segments' | 'settings'
type LowScoreFocus =
  | 'all'
  | 'critical'
  | 'overdue'
  | 'unassigned'
  | 'pending-contact'
  | 'root-cause'
  | 'action-missing'

const lowScoreFocuses: LowScoreFocus[] = [
  'all',
  'critical',
  'overdue',
  'unassigned',
  'pending-contact',
  'root-cause',
  'action-missing',
]

function lowScoreFocusFilters(focus: LowScoreFocus): SurveyResponseFilters {
  switch (focus) {
    case 'critical':
      return { reviewSeverity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL }
    case 'overdue':
      return { recoverySlaStatus: recoverySLAOverdue }
    case 'unassigned':
      return { recoveryBlockerReason: recoveryBlockerOwner }
    case 'pending-contact':
      return { recoveryBlockerReason: recoveryBlockerContact }
    case 'root-cause':
      return { recoveryBlockerReason: recoveryBlockerRootCause }
    case 'action-missing':
      return { recoveryBlockerReason: recoveryBlockerAction }
    default:
      return {}
  }
}

export function SurveysPage() {
  const { t } = useTranslation()
  const permissions = usePermissions()
  useDocumentTitle(t('surveys.title'))

  const [selectedCampaignID, setSelectedCampaignID] = useState('')
  const [lowScoreFocus, setLowScoreFocus] = useState<LowScoreFocus>('all')
  const [lowScoreOwnerID, setLowScoreOwnerID] = useState(allLowScoreOwnersValue)
  const [lowScoreAccountKey, setLowScoreAccountKey] = useState('')
  const analyticsRef = useRef<HTMLDivElement>(null)
  const lowScoresRef = useRef<HTMLDivElement>(null)
  const segmentsRef = useRef<HTMLDivElement>(null)
  const settingsRef = useRef<HTMLDivElement>(null)
  const campaignsQuery = useQuery(surveyCampaignsQuery(undefined, 50))
  const membersResult = useQuery({
    ...membersQuery(),
    enabled: permissions.can('settings:members:view'),
  })
  const analyticsQuery = useQuery(
    surveyAnalyticsQuery(selectedCampaignID ? { campaignId: selectedCampaignID } : {}),
  )
  const insightsQuery = useQuery(
    surveyAnalyticsInsightsQuery({
      campaignId: selectedCampaignID || undefined,
      limit: 6,
    }),
  )
  const healthQuery = useQuery(surveyCampaignHealthQuery(selectedCampaignID || undefined))
  const trendQuery = useQuery(
    surveyAnalyticsTrendQuery(selectedCampaignID ? { campaignId: selectedCampaignID } : {}),
  )
  const segmentsQuery = useQuery(
    surveyAnalyticsSegmentsQuery({
      campaignId: selectedCampaignID || undefined,
      dimension: sourceTypeSegmentDimension,
      limit: 8,
    }),
  )
  const invitationsQuery = useQuery(
    surveyInvitationsQuery({
      campaignId: selectedCampaignID || undefined,
      limit: 25,
    }),
  )
  const responsesQuery = useQuery(
    surveyResponsesQuery({
      campaignId: selectedCampaignID || undefined,
      limit: 25,
      lowScoreOnly: true,
      accountKey: lowScoreAccountKey.trim() || undefined,
      ownerMemberId: lowScoreOwnerID === allLowScoreOwnersValue ? undefined : lowScoreOwnerID,
      ...lowScoreFocusFilters(lowScoreFocus),
    }),
  )

  const campaigns = campaignsQuery.data ?? []
  const activeCampaigns = campaigns.filter((campaign) => campaign.status === activeCampaignStatus)
  const members = membersResult.data ?? []
  const selectedCampaign = campaigns.find((campaign) => campaign.id === selectedCampaignID)
  const analytics = analyticsQuery.data
  const insights = insightsQuery.data ?? []
  const campaignHealth = healthQuery.data
  const trend = trendQuery.data ?? []
  const segments = segmentsQuery.data ?? []
  const invitations = invitationsQuery.data ?? []
  const responses = responsesQuery.data ?? []
  const lowScoreResponses = prioritizeLowScoreResponses(
    responses.filter((response) => response.lowScore),
    Date.now(),
  )
  const updateLowScoreFocus = (next: LowScoreFocus) => {
    setLowScoreFocus(next)
    if (next === 'unassigned') setLowScoreOwnerID(allLowScoreOwnersValue)
  }
  const updateLowScoreOwner = (next: string) => {
    setLowScoreOwnerID(next)
    if (next !== allLowScoreOwnersValue && lowScoreFocus === 'unassigned') {
      setLowScoreFocus('all')
    }
  }
  const activateInsightTarget = (target: InsightActionTarget) => {
    const targets = {
      analytics: analyticsRef,
      'low-scores': lowScoresRef,
      segments: segmentsRef,
      settings: settingsRef,
    }
    targets[target].current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  useEffect(() => {
    if (selectedCampaignID || campaigns.length === 0) return
    const firstActive = activeCampaigns[0] ?? campaigns[0]
    setSelectedCampaignID(firstActive.id)
  }, [activeCampaigns, campaigns, selectedCampaignID])

  const loading =
    campaignsQuery.isPending ||
    analyticsQuery.isPending ||
    insightsQuery.isPending ||
    (healthQuery.isPending && Boolean(selectedCampaignID)) ||
    trendQuery.isPending ||
    segmentsQuery.isPending ||
    invitationsQuery.isPending ||
    responsesQuery.isPending
  const loadError =
    campaignsQuery.error ??
    analyticsQuery.error ??
    insightsQuery.error ??
    healthQuery.error ??
    trendQuery.error ??
    segmentsQuery.error ??
    invitationsQuery.error ??
    responsesQuery.error

  if (loading && campaigns.length === 0) {
    return (
      <div className="flex items-center justify-center py-16 text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        {t('app.loading')}
      </div>
    )
  }

  return (
    <section className="min-w-0 space-y-6">
      <PageHero
        eyebrow={t('shell.groups.integrations')}
        title={t('surveys.title')}
        subtitle={t('surveys.subtitle')}
        metrics={
          <>
            <PageHeroMetric
              label={t('surveys.metrics.campaigns')}
              value={String(campaigns.length)}
              hint={t('surveys.metrics.campaigns_hint')}
            />
            <PageHeroMetric
              label={t('surveys.metrics.response_rate')}
              value={formatPercent(analytics?.responseRate)}
              hint={t('surveys.metrics.response_rate_hint')}
            />
            <PageHeroMetric
              label={t('surveys.metrics.positive_rate')}
              value={formatPercent(analytics?.positiveScoreRate)}
              hint={t('surveys.metrics.positive_rate_hint')}
            />
            <PageHeroMetric
              label={t('surveys.metrics.average_score')}
              value={formatScore(analytics)}
              hint={t('surveys.metrics.average_score_hint')}
            />
            <PageHeroMetric
              label={t('surveys.metrics.low_scores')}
              value={String(analytics?.lowScoreCount ?? 0)}
              hint={t('surveys.metrics.low_scores_hint')}
              tone={(analytics?.lowScoreCount ?? 0) > 0 ? 'urgent' : 'default'}
            />
          </>
        }
      />

      {loadError ? (
        <Alert variant="destructive">
          <ShieldAlert className="h-4 w-4" />
          <AlertTitle>{t('surveys.load_error_title')}</AlertTitle>
          <AlertDescription>
            {loadError instanceof Error ? loadError.message : t('common.error')}
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(23rem,0.85fr)]">
        <div className="min-w-0 space-y-6">
          <CampaignCreateCard />
          <CampaignsCard
            campaigns={campaigns}
            selectedCampaignID={selectedCampaignID}
            onSelect={setSelectedCampaignID}
          />
          <InvitationsCard invitations={invitations} />
        </div>

        <div className="min-w-0 space-y-6">
          <CampaignScopeCard
            campaigns={campaigns}
            selectedCampaignID={selectedCampaignID}
            selectedCampaign={selectedCampaign}
            onSelect={setSelectedCampaignID}
          />
          <CampaignHealthCard health={campaignHealth} isLoading={healthQuery.isFetching} />
          <div ref={settingsRef}>
            <CampaignSettingsCard campaign={selectedCampaign} />
          </div>
          <RecipientPreviewCard campaigns={campaigns} selectedCampaignID={selectedCampaignID} />
          <SurveyTestEmailCard campaigns={campaigns} selectedCampaignID={selectedCampaignID} />
          <AnalyticsInsightsCard insights={insights} onActivate={activateInsightTarget} />
          <div ref={analyticsRef}>
            <AnalyticsCard analytics={analytics} />
          </div>
          <AnalyticsTrendCard buckets={trend} />
          <div ref={segmentsRef}>
            <AnalyticsSegmentsCard segments={segments} />
          </div>
          <HostedLinkCard campaigns={activeCampaigns} selectedCampaignID={selectedCampaignID} />
          <div ref={lowScoresRef} className="space-y-6">
            <LowScoreCommandCard analytics={analytics} members={members} />
            <LowScoreCard
              analytics={analytics}
              focus={lowScoreFocus}
              accountKey={lowScoreAccountKey}
              members={members}
              ownerMemberId={lowScoreOwnerID}
              responses={lowScoreResponses}
              onAccountKeyChange={setLowScoreAccountKey}
              onFocusChange={updateLowScoreFocus}
              onOwnerMemberChange={updateLowScoreOwner}
            />
          </div>
        </div>
      </div>
    </section>
  )
}

function CampaignCreateCard() {
  const { t } = useTranslation()
  const createCampaign = useCreateSurveyCampaign()
  const [form, setForm] = useState({
    name: '',
    surveyType: SurveyType.SURVEY_TYPE_CSAT,
    status: activeCampaignStatus,
    triggerEvent: workflowTrigger,
    distributionMode: contactEmailMode,
    dedupePolicy: onePerResolutionDedupe,
    locale: 'zh-CN',
    samplingPercent: '100',
    minDaysBetweenContact: '14',
    expiresAfterDays: '14',
    maxDailyInvitations: '500',
    lowScoreThreshold: '3',
    requireRecentCustomerActivity: false,
    recentActivityDays: '30',
    suppressAutoResolved: true,
  })

  const update = (patch: Partial<typeof form>) => setForm((current) => ({ ...current, ...patch }))

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    createCampaign.mutate(
      {
        name: form.name.trim(),
        surveyType: form.surveyType,
        status: form.status,
        triggerEvent: form.triggerEvent,
        distributionMode: form.distributionMode,
        dedupePolicy: form.dedupePolicy,
        triggerFilter: defaultTriggerFilter(form.triggerEvent),
        content: defaultSurveyContent(form.surveyType),
        locale: form.locale.trim() || 'zh-CN',
        samplingPercent: numberOrUndefined(form.samplingPercent),
        minDaysBetweenContact: integerOrUndefined(form.minDaysBetweenContact),
        expiresAfterDays: integerOrUndefined(form.expiresAfterDays),
        maxDailyInvitations: integerOrUndefined(form.maxDailyInvitations),
        lowScoreThreshold: integerOrUndefined(form.lowScoreThreshold),
        requireRecentCustomerActivity: form.requireRecentCustomerActivity,
        recentActivityDays: integerOrUndefined(form.recentActivityDays),
        suppressAutoResolved: form.suppressAutoResolved,
      },
      {
        onSuccess: () => {
          toast.success(t('surveys.toasts.campaign_created'))
          update({ name: '' })
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('surveys.create.title')}</CardTitle>
        <CardDescription>{t('surveys.create.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="grid gap-4 md:grid-cols-2" onSubmit={submit}>
          <div className="space-y-2 md:col-span-2">
            <Label htmlFor="survey-name">{t('surveys.fields.name')}</Label>
            <Input
              id="survey-name"
              value={form.name}
              onChange={(event) => update({ name: event.target.value })}
              data-testid="survey-name"
            />
          </div>
          <SelectField
            id="survey-type"
            label={t('surveys.fields.type')}
            value={form.surveyType}
            onValueChange={(value) => update({ surveyType: value as SurveyType })}
            options={[
              [SurveyType.SURVEY_TYPE_CSAT, t('surveys.type.csat')],
              [SurveyType.SURVEY_TYPE_CES, t('surveys.type.ces')],
            ]}
          />
          <SelectField
            id="survey-status"
            label={t('surveys.fields.status')}
            value={form.status}
            onValueChange={(value) => update({ status: value as SurveyCampaignStatus })}
            options={[
              [activeCampaignStatus, t('surveys.status.active')],
              [draftCampaignStatus, t('surveys.status.draft')],
            ]}
          />
          <SelectField
            id="survey-trigger"
            label={t('surveys.fields.trigger')}
            value={form.triggerEvent}
            onValueChange={(value) => update({ triggerEvent: value as SurveyTriggerEvent })}
            options={[
              [workflowTrigger, t('surveys.trigger.workflow_transition')],
              [replySentTrigger, t('surveys.trigger.reply_sent')],
              [requestResolvedTrigger, t('surveys.trigger.request_resolved')],
              [manualLinkTrigger, t('surveys.trigger.manual_link')],
            ]}
          />
          <SelectField
            id="survey-distribution"
            label={t('surveys.fields.distribution')}
            value={form.distributionMode}
            onValueChange={(value) => update({ distributionMode: value as SurveyDistributionMode })}
            options={[
              [contactEmailMode, t('surveys.distribution.contact_email')],
              [sourceLinkMode, t('surveys.distribution.source_link')],
            ]}
          />
          <SelectField
            id="survey-dedupe"
            label={t('surveys.fields.dedupe')}
            value={form.dedupePolicy}
            onValueChange={(value) => update({ dedupePolicy: value as SurveyDedupePolicy })}
            options={[
              [onePerResolutionDedupe, t('surveys.dedupe.one_per_resolution')],
              [onePerSourceDedupe, t('surveys.dedupe.one_per_source')],
              [onePerTriggerDedupe, t('surveys.dedupe.one_per_trigger')],
            ]}
          />
          <TextField
            id="survey-locale"
            label={t('surveys.fields.locale')}
            value={form.locale}
            onChange={(value) => update({ locale: value })}
          />
          <TextField
            id="survey-sampling"
            label={t('surveys.fields.sampling')}
            value={form.samplingPercent}
            onChange={(value) => update({ samplingPercent: value })}
            type="number"
          />
          <TextField
            id="survey-cooldown"
            label={t('surveys.fields.cooldown')}
            value={form.minDaysBetweenContact}
            onChange={(value) => update({ minDaysBetweenContact: value })}
            type="number"
          />
          <TextField
            id="survey-expiry"
            label={t('surveys.fields.expiry')}
            value={form.expiresAfterDays}
            onChange={(value) => update({ expiresAfterDays: value })}
            type="number"
          />
          <TextField
            id="survey-daily-limit"
            label={t('surveys.fields.daily_limit')}
            value={form.maxDailyInvitations}
            onChange={(value) => update({ maxDailyInvitations: value })}
            type="number"
          />
          <TextField
            id="survey-low-score"
            label={t('surveys.fields.low_score')}
            value={form.lowScoreThreshold}
            onChange={(value) => update({ lowScoreThreshold: value })}
            type="number"
          />
          <TextField
            id="survey-recent-days"
            label={t('surveys.fields.recent_days')}
            value={form.recentActivityDays}
            onChange={(value) => update({ recentActivityDays: value })}
            type="number"
          />
          <div className="flex flex-col gap-3 md:col-span-2">
            <CheckboxRow
              id="survey-recent-required"
              checked={form.requireRecentCustomerActivity}
              label={t('surveys.fields.require_recent')}
              onCheckedChange={(checked) => update({ requireRecentCustomerActivity: checked })}
            />
            <CheckboxRow
              id="survey-suppress-auto"
              checked={form.suppressAutoResolved}
              label={t('surveys.fields.suppress_auto')}
              onCheckedChange={(checked) => update({ suppressAutoResolved: checked })}
            />
          </div>
          <div className="flex justify-end md:col-span-2">
            <Button
              type="submit"
              disabled={!form.name.trim() || createCampaign.isPending}
              data-testid="survey-create"
            >
              {createCampaign.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {t('surveys.create.submit')}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}

function CampaignsCard({
  campaigns,
  onSelect,
  selectedCampaignID,
}: {
  campaigns: SurveyCampaign[]
  onSelect: (campaignID: string) => void
  selectedCampaignID: string
}) {
  const { t } = useTranslation()
  const archiveCampaign = useArchiveSurveyCampaign()

  const archive = (campaign: SurveyCampaign) => {
    archiveCampaign.mutate(campaign.id, {
      onSuccess: () => toast.success(t('surveys.toasts.campaign_archived')),
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
    })
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <ClipboardCheck className="h-4 w-4" />
          {t('surveys.campaigns.title')}
        </CardTitle>
        <CardDescription>{t('surveys.campaigns.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {campaigns.length === 0 ? (
          <EmptyState>{t('surveys.campaigns.empty')}</EmptyState>
        ) : (
          <div className="space-y-2">
            {campaigns.map((campaign) => (
              <div
                key={campaign.id}
                className={cn(
                  'grid min-w-0 gap-3 rounded-md border border-border/60 p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center',
                  campaign.id === selectedCampaignID && 'bg-muted/50',
                )}
              >
                <div className="min-w-0">
                  <button
                    type="button"
                    className="max-w-full break-words text-left font-medium underline-offset-4 hover:underline"
                    onClick={() => onSelect(campaign.id)}
                  >
                    {campaign.name}
                  </button>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {surveyTypeLabel(t, campaign.surveyType)} · {campaign.locale}
                  </div>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <StatusBadge>{triggerLabel(t, campaign.triggerEvent)}</StatusBadge>
                    <StatusBadge>{distributionLabel(t, campaign.distributionMode)}</StatusBadge>
                    <StatusBadge>{campaignStatusLabel(t, campaign.status)}</StatusBadge>
                  </div>
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={
                    campaign.status === SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ARCHIVED ||
                    archiveCampaign.isPending
                  }
                  onClick={() => archive(campaign)}
                  data-testid={`survey-archive-${campaign.id}`}
                  className="justify-self-start sm:justify-self-end"
                >
                  <Archive className="h-4 w-4" />
                  <span className="sr-only">{t('surveys.campaigns.archive')}</span>
                </Button>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function CampaignScopeCard({
  campaigns,
  onSelect,
  selectedCampaign,
  selectedCampaignID,
}: {
  campaigns: SurveyCampaign[]
  onSelect: (campaignID: string) => void
  selectedCampaign?: SurveyCampaign
  selectedCampaignID: string
}) {
  const { t } = useTranslation()
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('surveys.scope.title')}</CardTitle>
        <CardDescription>{t('surveys.scope.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <Select value={selectedCampaignID} onValueChange={onSelect}>
          <SelectTrigger
            aria-label={t('surveys.scope.filter_label')}
            data-testid="survey-campaign-filter"
          >
            <SelectValue placeholder={t('surveys.scope.placeholder')} />
          </SelectTrigger>
          <SelectContent>
            {campaigns.map((campaign) => (
              <SelectItem key={campaign.id} value={campaign.id}>
                {campaign.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {selectedCampaign ? (
          <dl className="grid gap-3 text-sm sm:grid-cols-2">
            <Definition label={t('surveys.fields.type')}>
              {surveyTypeLabel(t, selectedCampaign.surveyType)}
            </Definition>
            <Definition label={t('surveys.fields.low_score')}>
              {selectedCampaign.lowScoreThreshold}
            </Definition>
            <Definition label={t('surveys.fields.cooldown')}>
              {t('surveys.days', { count: selectedCampaign.minDaysBetweenContact })}
            </Definition>
            <Definition label={t('surveys.fields.expiry')}>
              {t('surveys.days', { count: selectedCampaign.expiresAfterDays })}
            </Definition>
          </dl>
        ) : (
          <EmptyState>{t('surveys.scope.empty')}</EmptyState>
        )}
      </CardContent>
    </Card>
  )
}

function CampaignSettingsCard({ campaign }: { campaign?: SurveyCampaign }) {
  const { t } = useTranslation()
  const updateCampaign = useUpdateSurveyCampaign()
  const [form, setForm] = useState(() => campaignSettingsForm(campaign))

  useEffect(() => {
    setForm(campaignSettingsForm(campaign))
  }, [campaign])

  const update = (patch: Partial<ReturnType<typeof campaignSettingsForm>>) =>
    setForm((current) => ({ ...current, ...patch }))

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!campaign) return
    updateCampaign.mutate(
      {
        id: campaign.id,
        name: form.name.trim(),
        status: form.status,
        triggerEvent: form.triggerEvent,
        distributionMode: form.distributionMode,
        dedupePolicy: form.dedupePolicy,
        triggerFilter:
          campaign.triggerEvent === form.triggerEvent
            ? campaign.triggerFilter
            : defaultTriggerFilter(form.triggerEvent),
        locale: form.locale.trim() || campaign.locale || 'zh-CN',
        samplingPercent: numberOrUndefined(form.samplingPercent),
        minDaysBetweenContact: integerOrUndefined(form.minDaysBetweenContact),
        expiresAfterDays: integerOrUndefined(form.expiresAfterDays),
        maxDailyInvitations: integerOrUndefined(form.maxDailyInvitations),
        lowScoreThreshold: integerOrUndefined(form.lowScoreThreshold),
        requireRecentCustomerActivity: form.requireRecentCustomerActivity,
        recentActivityDays: integerOrUndefined(form.recentActivityDays),
        suppressAutoResolved: form.suppressAutoResolved,
      },
      {
        onSuccess: () => toast.success(t('surveys.toasts.campaign_updated')),
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const disabled =
    !campaign ||
    campaign.status === SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ARCHIVED ||
    updateCampaign.isPending

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('surveys.settings.title')}</CardTitle>
        <CardDescription>{t('surveys.settings.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {!campaign ? (
          <EmptyState>{t('surveys.settings.empty')}</EmptyState>
        ) : (
          <form className="grid gap-4 sm:grid-cols-2" onSubmit={submit}>
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="survey-settings-name">{t('surveys.fields.name')}</Label>
              <Input
                id="survey-settings-name"
                value={form.name}
                onChange={(event) => update({ name: event.target.value })}
                data-testid="survey-settings-name"
                disabled={disabled}
              />
            </div>
            <SelectField
              id="survey-settings-status"
              label={t('surveys.fields.status')}
              value={form.status}
              onValueChange={(value) => update({ status: value as SurveyCampaignStatus })}
              options={[
                [activeCampaignStatus, t('surveys.status.active')],
                [draftCampaignStatus, t('surveys.status.draft')],
              ]}
              disabled={disabled}
            />
            <SelectField
              id="survey-settings-trigger"
              label={t('surveys.fields.trigger')}
              value={form.triggerEvent}
              onValueChange={(value) => update({ triggerEvent: value as SurveyTriggerEvent })}
              options={[
                [workflowTrigger, t('surveys.trigger.workflow_transition')],
                [replySentTrigger, t('surveys.trigger.reply_sent')],
                [requestResolvedTrigger, t('surveys.trigger.request_resolved')],
                [manualLinkTrigger, t('surveys.trigger.manual_link')],
              ]}
              disabled={disabled}
            />
            <SelectField
              id="survey-settings-distribution"
              label={t('surveys.fields.distribution')}
              value={form.distributionMode}
              onValueChange={(value) =>
                update({ distributionMode: value as SurveyDistributionMode })
              }
              options={[
                [contactEmailMode, t('surveys.distribution.contact_email')],
                [sourceLinkMode, t('surveys.distribution.source_link')],
              ]}
              disabled={disabled}
            />
            <SelectField
              id="survey-settings-dedupe"
              label={t('surveys.fields.dedupe')}
              value={form.dedupePolicy}
              onValueChange={(value) => update({ dedupePolicy: value as SurveyDedupePolicy })}
              options={[
                [onePerResolutionDedupe, t('surveys.dedupe.one_per_resolution')],
                [onePerSourceDedupe, t('surveys.dedupe.one_per_source')],
                [onePerTriggerDedupe, t('surveys.dedupe.one_per_trigger')],
              ]}
              disabled={disabled}
            />
            <TextField
              id="survey-settings-locale"
              label={t('surveys.fields.locale')}
              value={form.locale}
              onChange={(value) => update({ locale: value })}
              disabled={disabled}
            />
            <TextField
              id="survey-settings-sampling"
              label={t('surveys.fields.sampling')}
              value={form.samplingPercent}
              onChange={(value) => update({ samplingPercent: value })}
              type="number"
              disabled={disabled}
            />
            <TextField
              id="survey-settings-cooldown"
              label={t('surveys.fields.cooldown')}
              value={form.minDaysBetweenContact}
              onChange={(value) => update({ minDaysBetweenContact: value })}
              type="number"
              disabled={disabled}
            />
            <TextField
              id="survey-settings-expiry"
              label={t('surveys.fields.expiry')}
              value={form.expiresAfterDays}
              onChange={(value) => update({ expiresAfterDays: value })}
              type="number"
              disabled={disabled}
            />
            <TextField
              id="survey-settings-daily-limit"
              label={t('surveys.fields.daily_limit')}
              value={form.maxDailyInvitations}
              onChange={(value) => update({ maxDailyInvitations: value })}
              type="number"
              disabled={disabled}
            />
            <TextField
              id="survey-settings-low-score"
              label={t('surveys.fields.low_score')}
              value={form.lowScoreThreshold}
              onChange={(value) => update({ lowScoreThreshold: value })}
              type="number"
              disabled={disabled}
            />
            <TextField
              id="survey-settings-recent-days"
              label={t('surveys.fields.recent_days')}
              value={form.recentActivityDays}
              onChange={(value) => update({ recentActivityDays: value })}
              type="number"
              disabled={disabled}
            />
            <div className="flex flex-col gap-3 sm:col-span-2">
              <CheckboxRow
                id="survey-settings-recent-required"
                checked={form.requireRecentCustomerActivity}
                label={t('surveys.fields.require_recent')}
                onCheckedChange={(checked) => update({ requireRecentCustomerActivity: checked })}
                disabled={disabled}
              />
              <CheckboxRow
                id="survey-settings-suppress-auto"
                checked={form.suppressAutoResolved}
                label={t('surveys.fields.suppress_auto')}
                onCheckedChange={(checked) => update({ suppressAutoResolved: checked })}
                disabled={disabled}
              />
            </div>
            <div className="flex justify-end sm:col-span-2">
              <Button
                type="submit"
                disabled={disabled || !form.name.trim()}
                data-testid="survey-settings-save"
              >
                {updateCampaign.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                {t('surveys.settings.save')}
              </Button>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  )
}

function RecipientPreviewCard({
  campaigns,
  selectedCampaignID,
}: {
  campaigns: SurveyCampaign[]
  selectedCampaignID: string
}) {
  const { t } = useTranslation()
  const previewRecipients = usePreviewSurveyRecipients()
  const previewCampaigns = campaigns.filter(
    (campaign) => campaign.status !== SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ARCHIVED,
  )
  const defaultCampaignID = previewCampaigns.some((campaign) => campaign.id === selectedCampaignID)
    ? selectedCampaignID
    : previewCampaigns[0]?.id || ''
  const selectedCampaign = previewCampaigns.find((campaign) => campaign.id === defaultCampaignID)
  const [form, setForm] = useState({
    campaignId: defaultCampaignID,
    sourceType: previewDefaultSourceType(selectedCampaign),
    sourceId: '',
    requestId: '',
  })

  useEffect(() => {
    setForm((current) => {
      if (previewCampaigns.some((campaign) => campaign.id === current.campaignId)) return current
      const nextCampaign = previewCampaigns.find((campaign) => campaign.id === defaultCampaignID)
      return {
        ...current,
        campaignId: defaultCampaignID,
        sourceType: previewDefaultSourceType(nextCampaign),
      }
    })
  }, [defaultCampaignID, previewCampaigns])

  const campaign = previewCampaigns.find((item) => item.id === form.campaignId)
  const preview = previewRecipients.data
  const previewDisabled =
    !form.campaignId || !form.sourceId.trim() || previewRecipients.isPending || !campaign

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!campaign) return
    previewRecipients.mutate(
      {
        campaignId: form.campaignId,
        sourceType: form.sourceType.trim() || previewDefaultSourceType(campaign),
        sourceId: form.sourceId.trim(),
        requestId: form.requestId.trim() || undefined,
        context: previewDefaultContext(campaign),
        limit: 10,
      },
      {
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const updateCampaign = (campaignId: string) => {
    const nextCampaign = previewCampaigns.find((item) => item.id === campaignId)
    setForm((current) => ({
      ...current,
      campaignId,
      sourceType: previewDefaultSourceType(nextCampaign),
      requestId: '',
    }))
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Users className="h-4 w-4" />
          {t('surveys.recipient_preview.title')}
        </CardTitle>
        <CardDescription>{t('surveys.recipient_preview.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {previewCampaigns.length === 0 ? (
          <EmptyState>{t('surveys.recipient_preview.empty')}</EmptyState>
        ) : (
          <>
            <form className="grid gap-4 sm:grid-cols-2" onSubmit={submit}>
              <div className="sm:col-span-2">
                <SelectField
                  id="survey-preview-campaign"
                  label={t('surveys.recipient_preview.campaign')}
                  value={form.campaignId}
                  onValueChange={updateCampaign}
                  options={previewCampaigns.map((item) => [item.id, item.name])}
                />
              </div>
              <TextField
                id="survey-preview-source-type"
                label={t('surveys.recipient_preview.source_type')}
                value={form.sourceType}
                onChange={(value) => setForm((current) => ({ ...current, sourceType: value }))}
              />
              <TextField
                id="survey-preview-source-id"
                label={previewSourceIDLabel(t, campaign)}
                value={form.sourceId}
                onChange={(value) => setForm((current) => ({ ...current, sourceId: value }))}
              />
              <div className="sm:col-span-2">
                <TextField
                  id="survey-preview-request-id"
                  label={t('surveys.recipient_preview.request_id')}
                  value={form.requestId}
                  onChange={(value) => setForm((current) => ({ ...current, requestId: value }))}
                />
              </div>
              <div className="flex justify-end sm:col-span-2">
                <Button type="submit" disabled={previewDisabled} data-testid="survey-preview-run">
                  {previewRecipients.isPending ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : null}
                  {t('surveys.recipient_preview.submit')}
                </Button>
              </div>
            </form>
            {preview ? <RecipientPreviewResult preview={preview} /> : null}
          </>
        )}
      </CardContent>
    </Card>
  )
}

function RecipientPreviewResult({ preview }: { preview: PreviewSurveyRecipientsResponse }) {
  const { t } = useTranslation()
  const recipients = preview.recipients ?? []
  return (
    <div className="space-y-3" data-testid="survey-preview-result">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <MetricBox label={t('surveys.recipient_preview.matched')} value={preview.matchedCount} />
        <MetricBox label={t('surveys.recipient_preview.eligible')} value={preview.eligibleCount} />
        <MetricBox
          label={t('surveys.recipient_preview.suppressed')}
          value={preview.suppressedCount}
          tone={preview.suppressedCount > 0 ? 'urgent' : 'default'}
        />
        <MetricBox
          label={t('surveys.recipient_preview.delivery')}
          value={
            preview.deliveryReady
              ? t('surveys.recipient_preview.ready_badge')
              : t('surveys.recipient_preview.blocked_badge')
          }
          tone={preview.deliveryReady ? 'default' : 'urgent'}
        />
      </div>
      {!preview.triggerMatched || !preview.sampleIncluded || !preview.deliveryReady ? (
        <div className="rounded-md border border-amber-300 bg-amber-50/60 px-3 py-2 text-sm text-amber-900">
          {previewReadinessText(t, preview)}
        </div>
      ) : null}
      {recipients.length === 0 ? (
        <EmptyState>{t('surveys.recipient_preview.no_recipients')}</EmptyState>
      ) : (
        <ul className="space-y-2" aria-label={t('surveys.recipient_preview.recipients')}>
          {recipients.map((recipient) => (
            <RecipientPreviewRow
              key={`${recipient.sourceType}-${recipient.sourceId}-${recipient.contactId ?? ''}`}
              recipient={recipient}
            />
          ))}
        </ul>
      )}
      {(preview.suppressionReasonDistribution ?? []).length > 0 ? (
        <div className="space-y-2">
          <p className="text-xs font-semibold uppercase text-muted-foreground">
            {t('surveys.recipient_preview.reason_distribution')}
          </p>
          <div className="flex flex-wrap gap-2">
            {preview.suppressionReasonDistribution.map((bucket) => (
              <StatusBadge key={bucket.reason}>
                {surveyReasonLabel(t, bucket.reason)} · {bucket.count}
              </StatusBadge>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}

function RecipientPreviewRow({ recipient }: { recipient: SurveyRecipientPreview }) {
  const { t } = useTranslation()
  return (
    <li className="grid min-w-0 gap-2 rounded-md border border-border/60 px-3 py-2 text-sm sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
      <div className="min-w-0">
        <p className="truncate font-medium">{recipientPreviewLabel(t, recipient)}</p>
        <p className="mt-1 break-words text-xs text-muted-foreground">
          {recipient.sourceType} · {recipient.sourceId}
        </p>
      </div>
      <div className="flex flex-wrap gap-2 sm:justify-end">
        <StatusBadge>{shortEnum(recipient.channel || 'unknown')}</StatusBadge>
        <StatusBadge>
          {recipient.eligible
            ? t('surveys.recipient_preview.eligible_badge')
            : surveyReasonLabel(t, recipient.suppressionReason)}
        </StatusBadge>
      </div>
    </li>
  )
}

function SurveyTestEmailCard({
  campaigns,
  selectedCampaignID,
}: {
  campaigns: SurveyCampaign[]
  selectedCampaignID: string
}) {
  const { t } = useTranslation()
  const sendTestEmail = useSendSurveyTestEmail()
  const testCampaigns = campaigns.filter(
    (campaign) => campaign.status !== SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ARCHIVED,
  )
  const defaultCampaignID = testCampaigns.some((campaign) => campaign.id === selectedCampaignID)
    ? selectedCampaignID
    : testCampaigns[0]?.id || ''
  const [form, setForm] = useState({
    campaignId: defaultCampaignID,
    toEmail: '',
  })

  useEffect(() => {
    setForm((current) => {
      if (testCampaigns.some((campaign) => campaign.id === current.campaignId)) return current
      return { ...current, campaignId: defaultCampaignID }
    })
  }, [defaultCampaignID, testCampaigns])

  const disabled = !form.campaignId || !form.toEmail.trim() || sendTestEmail.isPending

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (disabled) return
    sendTestEmail.mutate(
      {
        campaignId: form.campaignId,
        toEmail: form.toEmail.trim(),
      },
      {
        onSuccess: (result) => {
          toast.success(
            t('surveys.toasts.test_email_sent', {
              provider: result.provider || t('surveys.test_email.provider_unknown'),
            }),
          )
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <MailCheck className="h-4 w-4" />
          {t('surveys.test_email.title')}
        </CardTitle>
        <CardDescription>{t('surveys.test_email.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {testCampaigns.length === 0 ? (
          <EmptyState>{t('surveys.test_email.empty')}</EmptyState>
        ) : (
          <form className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]" onSubmit={submit}>
            <SelectField
              id="survey-test-email-campaign"
              label={t('surveys.test_email.campaign')}
              value={form.campaignId}
              onValueChange={(value) => setForm((current) => ({ ...current, campaignId: value }))}
              options={testCampaigns.map((campaign) => [campaign.id, campaign.name])}
            />
            <TextField
              id="survey-test-email-to"
              label={t('surveys.test_email.to_email')}
              value={form.toEmail}
              onChange={(value) => setForm((current) => ({ ...current, toEmail: value }))}
              type="email"
            />
            <div className="flex justify-end sm:col-span-2">
              <Button
                type="submit"
                disabled={disabled}
                data-testid="survey-test-email-send"
                className="gap-2"
              >
                {sendTestEmail.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                {t('surveys.test_email.submit')}
              </Button>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  )
}

function CampaignHealthCard({
  health,
  isLoading,
}: {
  health?: SurveyCampaignHealth
  isLoading: boolean
}) {
  const { t } = useTranslation()
  const funnel = health?.funnel
  const checks = health?.checks ?? []
  const visibleChecks = [...checks].sort(
    (left, right) => healthCheckRank(right.status) - healthCheckRank(left.status),
  )
  return (
    <Card className="border-border/60 shadow-none" data-testid="survey-campaign-health">
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <Gauge className="h-4 w-4" />
              {t('surveys.health.title')}
            </CardTitle>
            <CardDescription>{t('surveys.health.description')}</CardDescription>
          </div>
          <span
            className={cn(
              'shrink-0 rounded-md border px-2 py-1 text-xs font-medium',
              healthStatusBadge(health?.status),
            )}
          >
            {isLoading && !health
              ? t('surveys.health.loading')
              : healthStatusLabel(t, health?.status)}
          </span>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {health ? (
          <>
            <div className="grid grid-cols-2 gap-3">
              <MetricBox
                label={t('surveys.health.score')}
                value={`${health.readinessScore}/100`}
                tone={health.status === campaignHealthBlocked ? 'urgent' : 'default'}
              />
              <MetricBox
                label={t('surveys.health.delivery_rate')}
                value={formatPercent(funnel?.deliveryRate)}
              />
              <MetricBox
                label={t('surveys.health.response_rate')}
                value={formatPercent(funnel?.responseRate)}
              />
              <MetricBox
                label={t('surveys.health.suppression_rate')}
                value={formatPercent(funnel?.suppressionRate)}
                tone={(funnel?.suppressionRate ?? 0) >= 0.25 ? 'urgent' : 'default'}
              />
              <MetricBox
                label={t('surveys.health.pending_delivery')}
                value={funnel?.pendingCount ?? 0}
                tone={(funnel?.pendingCount ?? 0) > 0 ? 'urgent' : 'default'}
              />
              <MetricBox
                label={t('surveys.health.overdue_recovery')}
                value={funnel?.overdueLowScoreReviewCount ?? 0}
                tone={(funnel?.overdueLowScoreReviewCount ?? 0) > 0 ? 'urgent' : 'default'}
              />
            </div>
            {visibleChecks.length > 0 ? (
              <ul className="space-y-2" aria-label={t('surveys.health.checks')}>
                {visibleChecks.map((check) => (
                  <li
                    key={check.id}
                    className={cn('rounded-md border px-3 py-3', healthCheckBorder(check.status))}
                  >
                    <div className="flex items-start gap-2">
                      {healthCheckIcon(check.status)}
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium">{healthCheckTitle(t, check)}</p>
                        <p className="mt-1 text-xs text-muted-foreground">
                          {healthCheckSummary(t, check)}
                        </p>
                        <p className="mt-2 text-xs font-medium text-foreground">
                          {healthCheckAction(t, check)}
                        </p>
                        {check.evidence ? (
                          <p className="mt-1 truncate text-xs text-muted-foreground">
                            {check.evidence}
                          </p>
                        ) : null}
                      </div>
                    </div>
                  </li>
                ))}
              </ul>
            ) : null}
          </>
        ) : (
          <EmptyState>{t('surveys.health.empty')}</EmptyState>
        )}
      </CardContent>
    </Card>
  )
}

function AnalyticsInsightsCard({
  insights,
  onActivate,
}: {
  insights: SurveyAnalyticsInsight[]
  onActivate: (target: InsightActionTarget) => void
}) {
  const { t } = useTranslation()
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('surveys.analytics_insights.title')}</CardTitle>
        <CardDescription>{t('surveys.analytics_insights.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {insights.length === 0 ? (
          <EmptyState>{t('surveys.analytics_insights.empty')}</EmptyState>
        ) : (
          <ul className="space-y-3" aria-label={t('surveys.analytics_insights.title')}>
            {insights.map((insight) => (
              <li
                key={insight.id}
                className={cn(
                  'rounded-md border px-3 py-3',
                  insightSeverityBorder(insight.severity),
                )}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="text-sm font-medium">{insightTitle(t, insight)}</p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {insightSummary(t, insight)}
                    </p>
                  </div>
                  <span
                    className={cn(
                      'shrink-0 rounded-md border px-2 py-1 text-xs font-medium',
                      insightSeverityBadge(insight.severity),
                    )}
                  >
                    {insightSeverityLabel(t, insight.severity)}
                  </span>
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2 text-xs">
                  <MetricBox
                    label={t('surveys.analytics_insights.metric')}
                    value={formatInsightValue(insight)}
                  />
                  <MetricBox
                    label={t('surveys.analytics_insights.threshold')}
                    value={formatInsightThreshold(insight)}
                  />
                </div>
                {insight.segmentLabel || insight.segmentKey ? (
                  <p className="mt-2 text-xs text-muted-foreground">
                    {t('surveys.analytics_insights.segment', {
                      value: shortEnum(insight.segmentLabel || insight.segmentKey || 'unknown'),
                    })}
                  </p>
                ) : null}
                <p className="mt-2 text-xs font-medium text-foreground">
                  {t('surveys.analytics_insights.action', {
                    value: insightAction(t, insight),
                  })}
                </p>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  className="mt-3 gap-2"
                  onClick={() => onActivate(insightActionTarget(insight))}
                >
                  {insightActionLabel(t, insight)}
                  <ArrowRight className="h-4 w-4" />
                </Button>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

function AnalyticsCard({ analytics }: { analytics?: SurveyAnalytics }) {
  const { t } = useTranslation()
  const buckets = analytics?.scoreDistribution ?? []
  const suppressionReasons = analytics?.suppressionReasonDistribution ?? []
  const maxCount = Math.max(1, ...buckets.map((bucket) => bucket.count))
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('surveys.analytics.title')}</CardTitle>
        <CardDescription>{t('surveys.analytics.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-2 gap-3">
          <MetricBox
            label={t('surveys.analytics.invitations')}
            value={analytics?.invitationCount}
          />
          <MetricBox label={t('surveys.analytics.delivered')} value={analytics?.deliveredCount} />
          <MetricBox
            label={t('surveys.analytics.not_started')}
            value={analytics?.notStartedCount}
          />
          <MetricBox label={t('surveys.analytics.opened')} value={analytics?.openedCount} />
          <MetricBox label={t('surveys.analytics.completed')} value={analytics?.completedCount} />
          <MetricBox label={t('surveys.analytics.expired')} value={analytics?.expiredCount} />
          <MetricBox
            label={t('surveys.analytics.average_response_time')}
            value={formatDuration(analytics?.averageResponseSeconds)}
          />
          <MetricBox
            label={t('surveys.analytics.positive')}
            value={analytics?.positiveScoreCount}
          />
          <MetricBox
            label={t('surveys.analytics.open_reviews')}
            value={analytics?.openLowScoreReviewCount}
          />
          <MetricBox
            label={t('surveys.analytics.overdue_reviews')}
            value={analytics?.overdueLowScoreReviewCount}
          />
          <MetricBox label={t('surveys.analytics.suppressed')} value={analytics?.suppressedCount} />
        </div>
        {buckets.length === 0 ? (
          <EmptyState>{t('surveys.analytics.empty')}</EmptyState>
        ) : (
          <ul className="space-y-2" aria-label={t('surveys.analytics.distribution')}>
            {buckets.map((bucket) => (
              <li key={bucket.score} className="grid grid-cols-[2rem_1fr_3rem] items-center gap-2">
                <span className="text-sm tabular-nums text-muted-foreground">{bucket.score}</span>
                <div className="h-2 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-emerald-600"
                    style={{ width: `${Math.max(8, (bucket.count / maxCount) * 100)}%` }}
                  />
                </div>
                <span className="text-right text-sm tabular-nums">{bucket.count}</span>
              </li>
            ))}
          </ul>
        )}
        {suppressionReasons.length > 0 ? (
          <div className="space-y-2">
            <p className="text-xs font-semibold uppercase text-muted-foreground">
              {t('surveys.analytics.suppression_reasons')}
            </p>
            <ul className="space-y-2" aria-label={t('surveys.analytics.suppression_reasons')}>
              {suppressionReasons.map((bucket) => (
                <li
                  key={bucket.reason}
                  className="flex items-center justify-between gap-3 rounded-md border border-border/60 px-3 py-2 text-sm"
                >
                  <span className="min-w-0 truncate text-muted-foreground">
                    {surveyReasonLabel(t, bucket.reason)}
                  </span>
                  <span className="font-medium tabular-nums">{bucket.count}</span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

function AnalyticsTrendCard({ buckets }: { buckets: SurveyAnalyticsTrendBucket[] }) {
  const { i18n, t } = useTranslation()
  const latest = buckets[buckets.length - 1]
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('surveys.analytics_trend.title')}</CardTitle>
        <CardDescription>{t('surveys.analytics_trend.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {buckets.length === 0 ? (
          <EmptyState>{t('surveys.analytics_trend.empty')}</EmptyState>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-3">
              <MetricBox
                label={t('surveys.analytics_trend.latest_response_rate')}
                value={formatPercent(latest?.responseRate)}
              />
              <MetricBox
                label={t('surveys.analytics_trend.latest_completed')}
                value={latest?.completedCount}
              />
              <MetricBox
                label={t('surveys.analytics_trend.latest_low_scores')}
                value={latest?.lowScoreCount}
              />
              <MetricBox
                label={t('surveys.analytics_trend.latest_expired')}
                value={latest?.expiredCount}
              />
            </div>
            <div className="space-y-4">
              <TrendSparkline
                buckets={buckets}
                label={t('surveys.analytics_trend.response_rate')}
                selectValue={(bucket) => bucket.responseRate * 100}
                formatValue={(value) => `${Math.round(value)}%`}
              />
              <TrendSparkline
                buckets={buckets}
                label={t('surveys.analytics_trend.completed')}
                selectValue={(bucket) => bucket.completedCount}
                formatValue={(value) => String(Math.round(value))}
              />
              <TrendSparkline
                buckets={buckets}
                label={t('surveys.analytics_trend.low_scores')}
                selectValue={(bucket) => bucket.lowScoreCount}
                formatValue={(value) => String(Math.round(value))}
              />
            </div>
            <ul className="space-y-2" aria-label={t('surveys.analytics_trend.recent_days')}>
              {buckets.slice(-3).map((bucket) => (
                <li
                  key={bucket.date}
                  className="grid grid-cols-[minmax(4rem,0.8fr)_repeat(3,minmax(0,1fr))] items-center gap-2 rounded-md border border-border/60 px-3 py-2 text-xs"
                >
                  <span className="min-w-0 font-medium">
                    {formatTrendDate(bucket.date, i18n.language)}
                  </span>
                  <span className="min-w-0 text-muted-foreground">
                    {t('surveys.analytics_trend.completed_short', {
                      count: bucket.completedCount,
                    })}
                  </span>
                  <span className="min-w-0 text-muted-foreground">
                    {t('surveys.analytics_trend.rate_short', {
                      value: formatPercent(bucket.responseRate),
                    })}
                  </span>
                  <span className="min-w-0 text-muted-foreground">
                    {t('surveys.analytics_trend.low_short', { count: bucket.lowScoreCount })}
                  </span>
                </li>
              ))}
            </ul>
          </>
        )}
      </CardContent>
    </Card>
  )
}

function TrendSparkline({
  buckets,
  formatValue,
  label,
  selectValue,
}: {
  buckets: SurveyAnalyticsTrendBucket[]
  formatValue: (value: number) => string
  label: string
  selectValue: (bucket: SurveyAnalyticsTrendBucket) => number
}) {
  const values = buckets.map((bucket) => Math.max(0, selectValue(bucket)))
  const max = Math.max(1, ...values)
  const width = trendChartWidth
  const height = trendChartHeight
  const chartWidth = width - trendChartPad * 2
  const chartHeight = height - trendChartPad * 2
  const points = values.map((value, index) => {
    const x = trendChartPad + (chartWidth * index) / Math.max(buckets.length - 1, 1)
    const y = height - trendChartPad - (value / max) * chartHeight
    return { value, x, y }
  })
  const path = points.map((point) => `${point.x},${point.y}`).join(' ')
  const latest = values[values.length - 1] ?? 0
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-3 text-xs">
        <span className="font-medium text-muted-foreground">{label}</span>
        <span className="tabular-nums">{formatValue(latest)}</span>
      </div>
      <svg viewBox={`0 0 ${width} ${height}`} className="h-24 w-full" role="img" aria-label={label}>
        <line
          x1={trendChartPad}
          x2={width - trendChartPad}
          y1={height - trendChartPad}
          y2={height - trendChartPad}
          className="stroke-border"
          strokeWidth={1}
        />
        <polyline points={path} fill="none" className="stroke-emerald-600" strokeWidth={3} />
        {points.map((point, index) => (
          <circle
            key={`${label}-${buckets[index]?.date}`}
            cx={point.x}
            cy={point.y}
            r={index === points.length - 1 ? 4 : 2.5}
            className="fill-background stroke-emerald-700"
            strokeWidth={2}
          >
            <title>{`${buckets[index]?.date}: ${formatValue(point.value)}`}</title>
          </circle>
        ))}
      </svg>
    </div>
  )
}

function AnalyticsSegmentsCard({ segments }: { segments: SurveyAnalyticsSegment[] }) {
  const { t } = useTranslation()
  const maxAttention = Math.max(1, ...segments.map((segment) => segment.attentionScore))
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('surveys.analytics_segments.title')}</CardTitle>
        <CardDescription>{t('surveys.analytics_segments.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {segments.length === 0 ? (
          <EmptyState>{t('surveys.analytics_segments.empty')}</EmptyState>
        ) : (
          <ul className="space-y-3" aria-label={t('surveys.analytics_segments.title')}>
            {segments.map((segment) => (
              <li
                key={`${segment.dimension}-${segment.key}`}
                className="rounded-md border border-border/60 px-3 py-3"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{segmentLabel(segment)}</p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {t('surveys.analytics_segments.sample', {
                        completed: segment.completedCount,
                        invitations: segment.invitationCount,
                      })}
                    </p>
                  </div>
                  <div className="shrink-0 text-right">
                    <p className="text-sm font-semibold tabular-nums">
                      {Math.round(segment.attentionScore)}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {t('surveys.analytics_segments.attention')}
                    </p>
                  </div>
                </div>
                <div className="mt-3 h-2 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-amber-500"
                    style={{
                      width: `${Math.max(8, (segment.attentionScore / maxAttention) * 100)}%`,
                    }}
                  />
                </div>
                <div className="mt-3 grid grid-cols-3 gap-2 text-xs">
                  <SegmentRate
                    label={t('surveys.analytics_segments.response_rate')}
                    value={formatPercent(segment.responseRate)}
                  />
                  <SegmentRate
                    label={t('surveys.analytics_segments.low_score_rate')}
                    value={formatPercent(segment.lowScoreRate)}
                    tone={segment.lowScoreRate > 0 ? 'urgent' : 'default'}
                  />
                  <SegmentRate
                    label={t('surveys.analytics_segments.suppression_rate')}
                    value={formatPercent(segment.suppressionRate)}
                    tone={segment.suppressionRate > 0 ? 'urgent' : 'default'}
                  />
                </div>
                <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  <span>
                    {t('surveys.analytics_segments.expired', { count: segment.expiredCount })}
                  </span>
                  <span>
                    {t('surveys.analytics_segments.average_score', {
                      value: segment.averageScore.toFixed(1),
                    })}
                  </span>
                  <span>
                    {t('surveys.analytics_segments.response_time', {
                      value: formatDuration(segment.averageResponseSeconds),
                    })}
                  </span>
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

function SegmentRate({
  label,
  tone = 'default',
  value,
}: {
  label: string
  tone?: 'default' | 'urgent'
  value: string
}) {
  return (
    <div>
      <p className="text-muted-foreground">{label}</p>
      <p className={cn('mt-1 font-medium tabular-nums', tone === 'urgent' && 'text-amber-700')}>
        {value}
      </p>
    </div>
  )
}

function segmentLabel(segment: SurveyAnalyticsSegment) {
  return shortEnum(segment.label || segment.key || 'unknown')
}

function HostedLinkCard({
  campaigns,
  selectedCampaignID,
}: {
  campaigns: SurveyCampaign[]
  selectedCampaignID: string
}) {
  const { t } = useTranslation()
  const createHostedLink = useCreateSurveyHostedLink()
  const defaultCampaignID = selectedCampaignID || campaigns[0]?.id || ''
  const [form, setForm] = useState({
    campaignId: defaultCampaignID,
    sourceType: 'feedback',
    sourceId: '',
    requestId: '',
  })
  const [latestURL, setLatestURL] = useState('')

  useEffect(() => {
    setForm((current) => {
      if (campaigns.some((campaign) => campaign.id === current.campaignId)) return current
      return { ...current, campaignId: defaultCampaignID }
    })
  }, [campaigns, defaultCampaignID])

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    createHostedLink.mutate(
      {
        campaignId: form.campaignId,
        sourceType: form.sourceType.trim(),
        sourceId: form.sourceId.trim(),
        requestId: form.requestId.trim() || undefined,
        context: { source: 'console' },
      },
      {
        onSuccess: (invitation) => {
          setLatestURL(invitation.publicUrl ?? '')
          toast.success(t('surveys.toasts.hosted_link_created'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const copyLatest = async () => {
    if (!latestURL) return
    await navigator.clipboard?.writeText(latestURL)
    toast.success(t('common.copied'))
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Send className="h-4 w-4" />
          {t('surveys.hosted_link.title')}
        </CardTitle>
        <CardDescription>{t('surveys.hosted_link.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="space-y-4" onSubmit={submit}>
          <SelectField
            id="hosted-link-campaign"
            label={t('surveys.hosted_link.campaign')}
            value={form.campaignId}
            onValueChange={(value) => setForm((current) => ({ ...current, campaignId: value }))}
            options={campaigns.map((campaign) => [campaign.id, campaign.name])}
          />
          <TextField
            id="hosted-link-source-type"
            label={t('surveys.hosted_link.source_type')}
            value={form.sourceType}
            onChange={(value) => setForm((current) => ({ ...current, sourceType: value }))}
          />
          <TextField
            id="hosted-link-source-id"
            label={t('surveys.hosted_link.source_id')}
            value={form.sourceId}
            onChange={(value) => setForm((current) => ({ ...current, sourceId: value }))}
          />
          <TextField
            id="hosted-link-request-id"
            label={t('surveys.hosted_link.request_id')}
            value={form.requestId}
            onChange={(value) => setForm((current) => ({ ...current, requestId: value }))}
          />
          <Button
            type="submit"
            disabled={!form.campaignId || !form.sourceType.trim() || !form.sourceId.trim()}
            data-testid="survey-hosted-link-create"
          >
            {createHostedLink.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {t('surveys.hosted_link.submit')}
          </Button>
          {latestURL ? (
            <div className="flex min-w-0 items-center gap-2 rounded-md border border-border/60 px-3 py-2 text-sm">
              <span className="min-w-0 flex-1 truncate" data-testid="survey-hosted-link-url">
                {latestURL}
              </span>
              <Button type="button" variant="ghost" size="sm" onClick={copyLatest}>
                <Clipboard className="h-4 w-4" />
                <span className="sr-only">{t('common.copy')}</span>
              </Button>
            </div>
          ) : null}
        </form>
      </CardContent>
    </Card>
  )
}

function InvitationsCard({ invitations }: { invitations: SurveyInvitation[] }) {
  const { t } = useTranslation()
  const retryDelivery = useRetrySurveyInvitationDelivery()
  const [retryingId, setRetryingId] = useState('')

  const retryInvitation = (invitation: SurveyInvitation) => {
    setRetryingId(invitation.id)
    retryDelivery.mutate(invitation.id, {
      onSuccess: () => toast.success(t('surveys.toasts.invitation_retry_requested')),
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      onSettled: () => setRetryingId(''),
    })
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <MailCheck className="h-4 w-4" />
          {t('surveys.invitations.title')}
        </CardTitle>
        <CardDescription>{t('surveys.invitations.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {invitations.length === 0 ? (
          <EmptyState>{t('surveys.invitations.empty')}</EmptyState>
        ) : (
          <div className="space-y-2">
            {invitations.map((invitation) => (
              <div
                key={invitation.id}
                className="grid min-w-0 gap-3 rounded-md border border-border/60 p-3"
              >
                <div className="min-w-0">
                  <div className="font-medium">{invitation.sourceType}</div>
                  <div className="break-words text-xs text-muted-foreground">
                    {invitation.sourceId}
                  </div>
                </div>
                <div className="flex flex-wrap gap-2">
                  <StatusBadge>
                    {surveyDeliveryStatusLabel(t, invitation.deliveryStatus)}
                  </StatusBadge>
                  <StatusBadge>
                    {surveyResponseStatusLabel(t, invitation.responseStatus)}
                  </StatusBadge>
                  {invitation.deliveryRetryable ? (
                    <Button
                      type="button"
                      variant="outline"
                      size="icon"
                      className="h-7 w-7"
                      aria-label={t('surveys.invitations.retry')}
                      data-testid={`survey-invitation-retry-${invitation.id}`}
                      disabled={retryingId === invitation.id}
                      onClick={() => retryInvitation(invitation)}
                    >
                      {retryingId === invitation.id ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <RefreshCw className="h-3.5 w-3.5" />
                      )}
                    </Button>
                  ) : null}
                </div>
                <div className="min-w-0 text-sm">
                  {invitation.publicUrl ? (
                    <a
                      className="break-all underline-offset-4 hover:underline"
                      href={invitation.publicUrl}
                    >
                      {invitation.publicUrl}
                    </a>
                  ) : (
                    <span className="text-muted-foreground">{t('common.never')}</span>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function LowScoreCommandCard({
  analytics,
  members,
}: {
  analytics?: SurveyAnalytics
  members: Member[]
}) {
  const { i18n, t } = useTranslation()
  const readiness = recoveryReadinessScore(analytics)
  const oldestDue = analytics?.oldestOpenLowScoreReviewDueAt
  const ownerLoads = analytics?.ownerRecoveryLoads ?? []
  const suggestedOwner = suggestedRecoveryOwner(members, ownerLoads)
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <ShieldAlert className="h-4 w-4" />
          {t('surveys.low_score_command.title')}
        </CardTitle>
        <CardDescription>{t('surveys.low_score_command.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">{lowScoreCommandSummary(t, analytics)}</p>
        <div className="grid grid-cols-2 gap-3">
          <MetricBox
            label={t('surveys.low_score_command.readiness')}
            value={`${readiness}/100`}
            tone={readiness < 70 ? 'urgent' : 'default'}
          />
          <MetricBox
            label={t('surveys.low_score_command.open')}
            value={analytics?.openLowScoreReviewCount}
            tone={(analytics?.openLowScoreReviewCount ?? 0) > 0 ? 'urgent' : 'default'}
          />
          <MetricBox
            label={t('surveys.low_score_command.overdue')}
            value={analytics?.overdueLowScoreReviewCount}
            tone={(analytics?.overdueLowScoreReviewCount ?? 0) > 0 ? 'urgent' : 'default'}
          />
          <MetricBox
            label={t('surveys.low_score_command.critical')}
            value={analytics?.criticalLowScoreReviewCount}
            tone={(analytics?.criticalLowScoreReviewCount ?? 0) > 0 ? 'urgent' : 'default'}
          />
          <MetricBox
            label={t('surveys.low_score_command.unassigned')}
            value={analytics?.unassignedLowScoreReviewCount}
            tone={(analytics?.unassignedLowScoreReviewCount ?? 0) > 0 ? 'urgent' : 'default'}
          />
          <MetricBox
            label={t('surveys.low_score_command.pending_contact')}
            value={analytics?.pendingCustomerContactReviewCount}
            tone={(analytics?.pendingCustomerContactReviewCount ?? 0) > 0 ? 'urgent' : 'default'}
          />
          <MetricBox
            label={t('surveys.low_score_command.missing_root_cause')}
            value={analytics?.missingRootCauseRecoveryQueueCount}
            tone={(analytics?.missingRootCauseRecoveryQueueCount ?? 0) > 0 ? 'urgent' : 'default'}
          />
          <MetricBox
            label={t('surveys.low_score_command.missing_action')}
            value={analytics?.missingActionRecoveryQueueCount}
            tone={(analytics?.missingActionRecoveryQueueCount ?? 0) > 0 ? 'urgent' : 'default'}
          />
        </div>
        <div className="rounded-md border border-border/60 px-3 py-2">
          <div className="text-xs text-muted-foreground">
            {t('surveys.low_score_command.oldest_due')}
          </div>
          <div className="mt-1 text-sm font-medium">
            {oldestDue
              ? formatTimestamp(oldestDue, i18n.language)
              : t('surveys.low_score_command.no_due')}
          </div>
        </div>
        <div className="rounded-md border border-border/60 px-3 py-2">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <div className="text-xs font-medium text-muted-foreground">
                {t('surveys.low_score_command.owner_load.title')}
              </div>
              {suggestedOwner ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  {t('surveys.low_score_command.owner_load.suggested', {
                    owner: memberLabel(suggestedOwner),
                  })}
                </p>
              ) : null}
            </div>
            <StatusBadge>
              {t('surveys.low_score_command.owner_load.count', {
                count: ownerLoads.length,
              })}
            </StatusBadge>
          </div>
          {ownerLoads.length === 0 ? (
            <p className="mt-3 text-sm text-muted-foreground">
              {t('surveys.low_score_command.owner_load.empty')}
            </p>
          ) : (
            <div className="mt-3 space-y-2">
              {ownerLoads.slice(0, 4).map((load) => (
                <div
                  key={load.ownerMemberId}
                  className="grid min-w-0 gap-2 rounded-md bg-muted/30 px-2 py-2 text-sm sm:grid-cols-[minmax(0,1fr)_auto]"
                >
                  <div className="min-w-0">
                    <div className="truncate font-medium">{ownerLoadLabel(load, members)}</div>
                    <div className="mt-1 flex flex-wrap gap-2">
                      <StatusBadge>
                        {t('surveys.low_score_command.owner_load.open', {
                          count: load.openCount,
                        })}
                      </StatusBadge>
                      <StatusBadge>
                        {t('surveys.low_score_command.owner_load.overdue', {
                          count: load.overdueCount,
                        })}
                      </StatusBadge>
                      <StatusBadge>
                        {t('surveys.low_score_command.owner_load.critical', {
                          count: load.criticalCount,
                        })}
                      </StatusBadge>
                    </div>
                  </div>
                  <div className="text-right tabular-nums">
                    <div className="text-xs text-muted-foreground">
                      {t('surveys.low_score_command.owner_load.score')}
                    </div>
                    <div className={cn('font-semibold', ownerLoadUrgent(load) && 'text-amber-800')}>
                      {load.workloadScore}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
        <div className="rounded-md border border-border/60 px-3 py-2">
          <div className="text-xs font-medium text-muted-foreground">
            {t('surveys.low_score_command.next_steps')}
          </div>
          <ol className="mt-2 space-y-1 text-sm">
            {lowScoreCommandSteps(t, analytics).map((step) => (
              <li key={step}>{step}</li>
            ))}
          </ol>
        </div>
      </CardContent>
    </Card>
  )
}

function LowScoreCard({
  accountKey,
  analytics,
  focus,
  members,
  onAccountKeyChange,
  onFocusChange,
  onOwnerMemberChange,
  ownerMemberId,
  responses,
}: {
  accountKey: string
  analytics?: SurveyAnalytics
  focus: LowScoreFocus
  members: Member[]
  onAccountKeyChange: (accountKey: string) => void
  onFocusChange: (focus: LowScoreFocus) => void
  onOwnerMemberChange: (ownerMemberId: string) => void
  ownerMemberId: string
  responses: SurveyResponse[]
}) {
  const { i18n, t } = useTranslation()
  const updateReview = useUpdateSurveyLowScoreReview()
  const batchUpdateReviews = useBatchUpdateSurveyLowScoreReviews()
  const assignReviews = useAssignSurveyLowScoreReviews()
  const escalateReviews = useEscalateSurveyLowScoreReviews()
  const [selectedResponseIDs, setSelectedResponseIDs] = useState<Set<string>>(() => new Set())
  const [batchForm, setBatchForm] = useState(() => lowScoreBatchDraft())
  const focusAnalytics = ownerMemberId === allLowScoreOwnersValue ? analytics : undefined
  const assignmentCandidateIDs = lowScoreAssignmentCandidateIDs(members)
  const selectedIDs = responses
    .filter((response) => selectedResponseIDs.has(response.id))
    .map((response) => response.id)
  const selectedCount = selectedIDs.length
  const batchDisabled =
    updateReview.isPending ||
    batchUpdateReviews.isPending ||
    assignReviews.isPending ||
    escalateReviews.isPending
  const batchReady = selectedCount > 0 && lowScoreBatchPatchPresent(batchForm)
  const assignmentReady = selectedCount > 0 && assignmentCandidateIDs.length > 0
  const escalationReady = selectedCount > 0

  useEffect(() => {
    setSelectedResponseIDs((current) => {
      const visibleIDs = new Set(responses.map((response) => response.id))
      const next = new Set([...current].filter((id) => visibleIDs.has(id)))
      return next.size === current.size ? current : next
    })
  }, [responses])

  const updateBatchForm = (patch: Partial<LowScoreBatchDraft>) =>
    setBatchForm((current) => ({ ...current, ...patch }))
  const updateSelection = (responseID: string, checked: boolean) =>
    setSelectedResponseIDs((current) => {
      const next = new Set(current)
      if (checked) {
        next.add(responseID)
      } else {
        next.delete(responseID)
      }
      return next
    })
  const selectVisible = (checked: boolean) =>
    setSelectedResponseIDs((current) => {
      const next = new Set(current)
      for (const response of responses) {
        if (checked) {
          next.add(response.id)
        } else {
          next.delete(response.id)
        }
      }
      return next
    })
  const clearSelection = () => setSelectedResponseIDs(new Set())
  const applyBatch = () => {
    if (!batchReady) return
    batchUpdateReviews.mutate(lowScoreBatchRequest(selectedIDs, batchForm), {
      onSuccess: (result) => {
        toast.success(
          t('surveys.toasts.low_score_batch_updated', {
            count: result.reviews?.length ?? selectedCount,
          }),
        )
        setSelectedResponseIDs(new Set())
        setBatchForm(lowScoreBatchDraft())
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
    })
  }
  const assignSelected = () => {
    if (!assignmentReady) return
    assignReviews.mutate(
      {
        responseIds: selectedIDs,
        candidateOwnerMemberIds: assignmentCandidateIDs,
      },
      {
        onSuccess: (result) => {
          const decisions = result.decisions ?? []
          toast.success(
            t('surveys.toasts.low_score_assigned', {
              count: result.reviews?.length ?? decisions.length ?? selectedCount,
              escalated: decisions.filter((decision) => decision.escalated).length,
            }),
          )
          setSelectedResponseIDs(new Set())
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }
  const escalateSelected = () => {
    if (!escalationReady) return
    escalateReviews.mutate(
      {
        responseIds: selectedIDs,
      },
      {
        onSuccess: (result) => {
          const decisions = result.decisions ?? []
          toast.success(
            t('surveys.toasts.low_score_escalated', {
              count: result.reviews?.length ?? decisions.length ?? selectedCount,
              dueChanged: decisions.filter((decision) => decision.dueAtChanged).length,
            }),
          )
          setSelectedResponseIDs(new Set())
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const saveReview = (response: SurveyResponse, form: LowScoreReviewDraft) => {
    updateReview.mutate(
      {
        responseId: response.id,
        status: form.status,
        severity: form.severity,
        ownerMemberId: form.ownerMemberId.trim(),
        customerContacted: form.customerContacted,
        rootCause: form.rootCause.trim(),
        actionTaken: form.actionTaken.trim(),
        dueAt: dateTimeLocalToRFC3339(form.dueAt),
      },
      {
        onSuccess: () => toast.success(t('surveys.toasts.low_score_updated')),
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const mark = (response: SurveyResponse, status: SurveyLowScoreReviewStatus) => {
    updateReview.mutate(
      {
        responseId: response.id,
        status,
        severity: response.lowScoreReview?.severity || defaultLowScoreSeverity,
        customerContacted: response.lowScoreReview?.customerContacted ?? false,
        rootCause: response.lowScoreReview?.rootCause || undefined,
        actionTaken: response.lowScoreReview?.actionTaken || undefined,
      },
      {
        onSuccess: () => toast.success(t('surveys.toasts.low_score_updated')),
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <ShieldAlert className="h-4 w-4" />
          {t('surveys.low_scores.title')}
        </CardTitle>
        <CardDescription>{t('surveys.low_scores.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="mb-4 grid gap-3 xl:grid-cols-[minmax(0,1fr)_minmax(12rem,16rem)_minmax(12rem,16rem)]">
          <fieldset className="space-y-2">
            <legend className="text-xs font-medium text-muted-foreground">
              {t('surveys.low_scores.focus_label')}
            </legend>
            <div className="flex flex-wrap gap-2">
              {lowScoreFocuses.map((item) => (
                <LowScoreFocusButton
                  key={item}
                  count={lowScoreFocusCount(focusAnalytics, item)}
                  focus={focus}
                  value={item}
                  onFocusChange={onFocusChange}
                />
              ))}
            </div>
          </fieldset>
          <SelectField
            id="survey-low-score-owner-filter"
            label={t('surveys.low_scores.owner_filter')}
            value={ownerMemberId}
            onValueChange={onOwnerMemberChange}
            options={lowScoreOwnerFilterOptions(t, members, ownerMemberId)}
          />
          <TextField
            id="survey-low-score-account-filter"
            label={t('surveys.low_scores.account_filter')}
            value={accountKey}
            onChange={onAccountKeyChange}
            placeholder={t('surveys.low_scores.account_filter_placeholder')}
          />
        </div>
        {responses.length === 0 ? (
          <EmptyState>{t('surveys.low_scores.empty')}</EmptyState>
        ) : (
          <div className="space-y-3">
            <div className="rounded-md border border-border/60 bg-muted/20 p-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <CheckboxRow
                  id="survey-low-score-select-visible"
                  checked={selectedCount === responses.length && responses.length > 0}
                  label={t('surveys.low_scores.batch.select_visible', {
                    count: responses.length,
                  })}
                  onCheckedChange={selectVisible}
                  disabled={batchDisabled}
                />
                <div className="flex flex-wrap items-center gap-2">
                  <StatusBadge>
                    {t('surveys.low_scores.batch.selected', { count: selectedCount })}
                  </StatusBadge>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={selectedCount === 0 || batchDisabled}
                    onClick={clearSelection}
                  >
                    {t('surveys.low_scores.batch.clear')}
                  </Button>
                </div>
              </div>
              {selectedCount > 0 ? (
                <div className="mt-3 grid gap-3 md:grid-cols-2">
                  <SelectField
                    id="survey-low-score-batch-owner"
                    label={t('surveys.low_scores.owner')}
                    value={batchForm.ownerMemberId}
                    onValueChange={(value) => updateBatchForm({ ownerMemberId: value })}
                    options={lowScoreBatchOwnerOptions(t, members, batchForm.ownerMemberId)}
                    disabled={batchDisabled}
                  />
                  <SelectField
                    id="survey-low-score-batch-status"
                    label={t('surveys.low_scores.status_label')}
                    value={batchForm.status}
                    onValueChange={(value) => updateBatchForm({ status: value })}
                    options={lowScoreBatchStatusOptions(t)}
                    disabled={batchDisabled}
                  />
                  <SelectField
                    id="survey-low-score-batch-severity"
                    label={t('surveys.low_scores.severity_label')}
                    value={batchForm.severity}
                    onValueChange={(value) => updateBatchForm({ severity: value })}
                    options={lowScoreBatchSeverityOptions(t)}
                    disabled={batchDisabled}
                  />
                  <TextField
                    id="survey-low-score-batch-due"
                    label={t('surveys.low_scores.due_label')}
                    value={batchForm.dueAt}
                    onChange={(value) => updateBatchForm({ dueAt: value })}
                    type="datetime-local"
                    disabled={batchDisabled}
                  />
                  <div className="flex flex-wrap items-center justify-between gap-3 md:col-span-2">
                    <CheckboxRow
                      id="survey-low-score-batch-contacted"
                      checked={batchForm.customerContacted}
                      label={t('surveys.low_scores.batch.customer_contacted')}
                      onCheckedChange={(checked) => updateBatchForm({ customerContacted: checked })}
                      disabled={batchDisabled}
                    />
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={!assignmentReady || batchDisabled}
                      onClick={assignSelected}
                      data-testid="survey-low-score-assign"
                    >
                      {assignReviews.isPending ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <ArrowRight className="h-4 w-4" />
                      )}
                      {t('surveys.low_scores.assignment.apply')}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={!escalationReady || batchDisabled}
                      onClick={escalateSelected}
                      data-testid="survey-low-score-escalate"
                    >
                      {escalateReviews.isPending ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <ShieldAlert className="h-4 w-4" />
                      )}
                      {t('surveys.low_scores.escalation.apply')}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      disabled={!batchReady || batchDisabled}
                      onClick={applyBatch}
                      data-testid="survey-low-score-batch-apply"
                    >
                      {batchUpdateReviews.isPending ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <ClipboardCheck className="h-4 w-4" />
                      )}
                      {t('surveys.low_scores.batch.apply')}
                    </Button>
                  </div>
                </div>
              ) : null}
            </div>
            {responses.map((response) => (
              <div
                key={response.id}
                className="min-w-0 rounded-md border border-border/60 p-3"
                data-testid={`survey-low-score-${response.id}`}
              >
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="flex min-w-0 gap-3">
                    <Checkbox
                      id={`survey-low-score-select-${response.id}`}
                      checked={selectedResponseIDs.has(response.id)}
                      onCheckedChange={(value) => updateSelection(response.id, value === true)}
                      disabled={batchDisabled}
                      aria-label={t('surveys.low_scores.batch.select_response', {
                        source: response.sourceId,
                      })}
                    />
                    <div className="min-w-0">
                      <div className="font-medium">
                        {t('surveys.low_scores.score', { score: response.score })}
                      </div>
                      <p className="mt-1 line-clamp-3 text-sm text-muted-foreground">
                        {response.comment || t('surveys.low_scores.no_comment')}
                      </p>
                      <div className="mt-2 text-xs text-muted-foreground">
                        {response.sourceType} · {response.sourceId}
                      </div>
                      <div className="mt-2 flex flex-wrap gap-2">
                        {response.accountContext?.accountKey ? (
                          <StatusBadge>
                            {t('surveys.low_scores.account_context', {
                              account:
                                response.accountContext.accountDisplay ||
                                response.accountContext.accountKey,
                            })}
                          </StatusBadge>
                        ) : null}
                        <StatusBadge>
                          {t('surveys.low_scores.status', {
                            value: lowScoreReviewStatusLabel(t, response.lowScoreReview?.status),
                          })}
                        </StatusBadge>
                        <StatusBadge>
                          {t('surveys.low_scores.severity', {
                            value: lowScoreSeverityLabel(t, response.lowScoreReview?.severity),
                          })}
                        </StatusBadge>
                        <StatusBadge>
                          {response.lowScoreReview?.dueAt
                            ? t('surveys.low_scores.due', {
                                value: formatTimestamp(
                                  response.lowScoreReview.dueAt,
                                  i18n.language,
                                ),
                              })
                            : t('surveys.low_scores.no_due')}
                        </StatusBadge>
                        {response.lowScoreReview ? (
                          <StatusBadge>
                            {recoverySLAStatusLabel(t, response.lowScoreReview.slaStatus)}
                          </StatusBadge>
                        ) : null}
                      </div>
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={updateReview.isPending}
                      onClick={() =>
                        mark(
                          response,
                          SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW,
                        )
                      }
                    >
                      {t('surveys.low_scores.in_review')}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      disabled={updateReview.isPending}
                      onClick={() =>
                        mark(
                          response,
                          SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED,
                        )
                      }
                      data-testid={`survey-low-score-resolve-${response.id}`}
                    >
                      {t('surveys.low_scores.resolve')}
                    </Button>
                  </div>
                </div>
                <LowScoreRecoveryPlaybook review={response.lowScoreReview} />
                <LowScoreReviewEditor
                  response={response}
                  members={members}
                  disabled={updateReview.isPending}
                  onSave={saveReview}
                />
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function LowScoreFocusButton({
  count,
  focus,
  onFocusChange,
  value,
}: {
  count?: number
  focus: LowScoreFocus
  onFocusChange: (focus: LowScoreFocus) => void
  value: LowScoreFocus
}) {
  const { t } = useTranslation()
  const active = focus === value
  return (
    <Button
      type="button"
      size="sm"
      variant={active ? 'default' : 'outline'}
      onClick={() => onFocusChange(value)}
      data-testid={`survey-low-score-focus-${value}`}
    >
      <span>{lowScoreFocusLabel(t, value)}</span>
      {count === undefined ? null : (
        <span
          className={cn(
            'ml-1 rounded-sm px-1.5 py-0.5 text-[0.625rem] leading-none tabular-nums',
            active ? 'bg-primary-foreground/20' : 'bg-muted text-muted-foreground',
          )}
        >
          {count}
        </span>
      )}
    </Button>
  )
}

type LowScoreReviewDraft = ReturnType<typeof lowScoreReviewDraft>
type LowScoreBatchDraft = ReturnType<typeof lowScoreBatchDraft>

function LowScoreRecoveryPlaybook({ review }: { review?: SurveyResponse['lowScoreReview'] }) {
  const { t } = useTranslation()
  if (!review) return null
  const urgent = review.slaStatus === recoverySLAOverdue || review.riskScore >= 70
  const automated = recoveryAutomated(review)
  const notificationStatus = recoveryNotificationStatusLabel(t, review.recoveryNotificationStatus)
  const notificationReason = recoveryNotificationReasonLabel(t, review.recoveryNotificationReason)
  return (
    <div
      className={cn(
        'mt-3 grid gap-3 rounded-md border px-3 py-3 text-sm md:grid-cols-[auto_1fr]',
        urgent ? 'border-amber-300 bg-amber-50/60' : 'border-border/60 bg-muted/20',
      )}
    >
      <div>
        <div className="text-xs text-muted-foreground">
          {t('surveys.low_scores.playbook.risk_score')}
        </div>
        <div className={cn('mt-1 text-lg font-semibold tabular-nums', urgent && 'text-amber-800')}>
          {review.riskScore}
        </div>
      </div>
      <div className="min-w-0 space-y-1">
        <div className="flex flex-wrap gap-2">
          <StatusBadge>{recoverySLAStatusLabel(t, review.slaStatus)}</StatusBadge>
          <StatusBadge>{recoveryBlockerLabel(t, review.blockerReason)}</StatusBadge>
          {automated ? (
            <StatusBadge>{t('surveys.low_scores.playbook.automated_escalation')}</StatusBadge>
          ) : null}
          {notificationStatus ? <StatusBadge>{notificationStatus}</StatusBadge> : null}
          {notificationReason ? <StatusBadge>{notificationReason}</StatusBadge> : null}
        </div>
        <p className="text-sm font-medium">
          {t('surveys.low_scores.playbook.next_action', {
            value: recoveryActionLabel(t, review.nextBestAction),
          })}
        </p>
      </div>
    </div>
  )
}

function recoveryAutomated(review: SurveyResponse['lowScoreReview']) {
  return Boolean(review?.actionTaken?.includes(recoveryAutomationMarker))
}

function LowScoreReviewEditor({
  disabled,
  onSave,
  response,
  members,
}: {
  disabled: boolean
  members: Member[]
  onSave: (response: SurveyResponse, form: LowScoreReviewDraft) => void
  response: SurveyResponse
}) {
  const { t } = useTranslation()
  const [form, setForm] = useState(() => lowScoreReviewDraft(response))

  useEffect(() => {
    setForm(lowScoreReviewDraft(response))
  }, [response])

  const update = (patch: Partial<LowScoreReviewDraft>) =>
    setForm((current) => ({ ...current, ...patch }))

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    onSave(response, form)
  }

  return (
    <form className="mt-4 min-w-0 border-t border-border/60 pt-3" onSubmit={submit}>
      <div className="grid min-w-0 gap-3 sm:grid-cols-2">
        <SelectField
          id={`survey-low-score-status-${response.id}`}
          label={t('surveys.low_scores.status_label')}
          value={form.status}
          onValueChange={(value) => update({ status: value as SurveyLowScoreReviewStatus })}
          options={lowScoreStatusOptions(t)}
          disabled={disabled}
        />
        <SelectField
          id={`survey-low-score-severity-${response.id}`}
          label={t('surveys.low_scores.severity_label')}
          value={form.severity}
          onValueChange={(value) => update({ severity: value as SurveyLowScoreSeverity })}
          options={lowScoreSeverityOptions(t)}
          disabled={disabled}
        />
        <SelectField
          id={`survey-low-score-owner-${response.id}`}
          label={t('surveys.low_scores.owner')}
          value={form.ownerMemberId || unassignedOwnerValue}
          onValueChange={(value) =>
            update({
              ownerMemberId: value === unassignedOwnerValue ? '' : value,
            })
          }
          options={lowScoreOwnerOptions(t, members, form.ownerMemberId)}
          disabled={disabled}
        />
        <TextField
          id={`survey-low-score-due-${response.id}`}
          label={t('surveys.low_scores.due_label')}
          value={form.dueAt}
          onChange={(value) => update({ dueAt: value })}
          type="datetime-local"
          disabled={disabled}
        />
        <TextAreaField
          id={`survey-low-score-root-cause-${response.id}`}
          label={t('surveys.low_scores.root_cause')}
          value={form.rootCause}
          onChange={(value) => update({ rootCause: value })}
          disabled={disabled}
        />
        <TextAreaField
          id={`survey-low-score-action-${response.id}`}
          label={t('surveys.low_scores.action_taken')}
          value={form.actionTaken}
          onChange={(value) => update({ actionTaken: value })}
          disabled={disabled}
        />
      </div>
      <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
        <CheckboxRow
          id={`survey-low-score-contacted-${response.id}`}
          label={t('surveys.low_scores.customer_contacted')}
          checked={form.customerContacted}
          onCheckedChange={(checked) => update({ customerContacted: checked })}
          disabled={disabled}
        />
        <Button
          type="submit"
          size="sm"
          disabled={disabled}
          data-testid={`survey-low-score-save-${response.id}`}
        >
          {t('surveys.low_scores.save')}
        </Button>
      </div>
    </form>
  )
}

function MetricBox({
  label,
  tone = 'default',
  value,
}: {
  label: string
  tone?: 'default' | 'urgent'
  value?: number | string
}) {
  return (
    <div
      className={cn(
        'rounded-md border px-3 py-2',
        tone === 'urgent' ? 'border-amber-300 bg-amber-50/60' : 'border-border/60',
      )}
    >
      <div className="text-xs text-muted-foreground">{label}</div>
      <div
        className={cn(
          'mt-1 text-lg font-semibold tabular-nums',
          tone === 'urgent' && 'text-amber-800',
        )}
      >
        {value ?? 0}
      </div>
    </div>
  )
}

function SelectField({
  disabled = false,
  id,
  label,
  onValueChange,
  options,
  value,
}: {
  id: string
  label: string
  onValueChange: (value: string) => void
  options: [string, string][]
  value: string
  disabled?: boolean
}) {
  return (
    <div className="min-w-0 space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <Select value={value} onValueChange={onValueChange} disabled={disabled}>
        <SelectTrigger id={id} data-testid={id} className="min-w-0 max-w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map(([optionValue, optionLabel]) => (
            <SelectItem key={optionValue} value={optionValue}>
              {optionLabel}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

function TextField({
  disabled = false,
  id,
  label,
  onChange,
  placeholder,
  type = 'text',
  value,
}: {
  id: string
  label: string
  onChange: (value: string) => void
  placeholder?: string
  type?: 'datetime-local' | 'email' | 'number' | 'text'
  value: string
  disabled?: boolean
}) {
  return (
    <div className="min-w-0 space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type={type}
        className="min-w-0"
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        data-testid={id}
        disabled={disabled}
      />
    </div>
  )
}

function TextAreaField({
  disabled = false,
  id,
  label,
  onChange,
  value,
}: {
  id: string
  label: string
  onChange: (value: string) => void
  value: string
  disabled?: boolean
}) {
  return (
    <div className="min-w-0 space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <textarea
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        data-testid={id}
        disabled={disabled}
        className="min-h-20 w-full min-w-0 rounded-md border border-input bg-background px-3 py-2 text-sm outline-none ring-offset-background placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
      />
    </div>
  )
}

function CheckboxRow({
  checked,
  id,
  label,
  onCheckedChange,
  disabled = false,
}: {
  checked: boolean
  id: string
  label: string
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
}) {
  return (
    <label className="flex items-center gap-2 text-sm" htmlFor={id}>
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(value) => onCheckedChange(value === true)}
        disabled={disabled}
      />
      {label}
    </label>
  )
}

function Definition({ children, label }: { children: ReactNode; label: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-medium">{children}</dd>
    </div>
  )
}

function EmptyState({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-md border border-dashed border-border/70 px-4 py-6 text-center text-sm text-muted-foreground">
      {children}
    </div>
  )
}

function StatusBadge({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex rounded-full border border-border/60 px-2 py-0.5 text-xs font-medium">
      {children}
    </span>
  )
}

function previewDefaultSourceType(campaign?: SurveyCampaign) {
  switch (campaign?.triggerEvent) {
    case requestResolvedTrigger:
      return 'request'
    case manualLinkTrigger:
      return 'manual'
    default:
      return 'feedback'
  }
}

function previewDefaultContext(campaign: SurveyCampaign) {
  if (campaign.triggerEvent === workflowTrigger) return { workflow_category: 'closed' }
  if (campaign.triggerEvent === requestResolvedTrigger) return { request_status: 'shipped' }
  return {}
}

function previewSourceIDLabel(t: TFunction, campaign?: SurveyCampaign) {
  switch (campaign?.triggerEvent) {
    case requestResolvedTrigger:
      return t('surveys.recipient_preview.request_source_id')
    case workflowTrigger:
    case replySentTrigger:
      return t('surveys.recipient_preview.feedback_id')
    default:
      return t('surveys.recipient_preview.source_id')
  }
}

function previewReadinessText(t: TFunction, preview: PreviewSurveyRecipientsResponse) {
  if (!preview.triggerMatched) return t('surveys.recipient_preview.filter_mismatch')
  if (!preview.sampleIncluded) return t('surveys.recipient_preview.sampled_out')
  if (!preview.deliveryReady) {
    if (preview.deliveryBlocker) {
      return t('surveys.recipient_preview.delivery_blocked_reason', {
        reason: surveyReasonLabel(t, preview.deliveryBlocker),
      })
    }
    return t('surveys.recipient_preview.delivery_blocked')
  }
  return ''
}

function recipientPreviewLabel(t: TFunction, recipient: SurveyRecipientPreview) {
  return (
    recipient.displayName ||
    recipient.subjectDisplay ||
    recipient.contactId ||
    t('surveys.recipient_preview.anonymous')
  )
}

function defaultTriggerFilter(triggerEvent: SurveyTriggerEvent) {
  if (triggerEvent === workflowTrigger) return { workflow_category: 'closed' }
  if (triggerEvent === requestResolvedTrigger) return { request_status: 'shipped' }
  return {}
}

function defaultSurveyContent(surveyType: SurveyType) {
  if (surveyType === SurveyType.SURVEY_TYPE_CES) {
    return {
      title: 'Effort check',
      intro: 'Help us understand the resolution experience.',
      question: 'How easy was it to get this resolved?',
      comment_prompt: 'What made this easier or harder?',
      thank_you: 'Thanks for the feedback.',
    }
  }
  return {
    title: 'Satisfaction check',
    intro: 'Help us understand the resolution experience.',
    question: 'How satisfied are you with this resolution?',
    comment_prompt: 'What should we know?',
    thank_you: 'Thanks for the feedback.',
  }
}

function campaignSettingsForm(campaign?: SurveyCampaign) {
  return {
    name: campaign?.name ?? '',
    status: campaign?.status ?? activeCampaignStatus,
    triggerEvent: campaign?.triggerEvent ?? workflowTrigger,
    distributionMode: campaign?.distributionMode ?? contactEmailMode,
    dedupePolicy: campaign?.dedupePolicy ?? onePerResolutionDedupe,
    locale: campaign?.locale ?? 'zh-CN',
    samplingPercent: String(campaign?.samplingPercent ?? 100),
    minDaysBetweenContact: String(campaign?.minDaysBetweenContact ?? 14),
    expiresAfterDays: String(campaign?.expiresAfterDays ?? 14),
    maxDailyInvitations: String(campaign?.maxDailyInvitations ?? 500),
    lowScoreThreshold: String(campaign?.lowScoreThreshold ?? 3),
    requireRecentCustomerActivity: campaign?.requireRecentCustomerActivity ?? false,
    recentActivityDays: String(campaign?.recentActivityDays ?? 30),
    suppressAutoResolved: campaign?.suppressAutoResolved ?? true,
  }
}

function lowScoreReviewDraft(response: SurveyResponse) {
  return {
    status: response.lowScoreReview?.status || defaultLowScoreReviewStatus,
    severity: response.lowScoreReview?.severity || defaultLowScoreSeverity,
    ownerMemberId: response.lowScoreReview?.ownerMemberId || '',
    rootCause: response.lowScoreReview?.rootCause || '',
    actionTaken: response.lowScoreReview?.actionTaken || '',
    customerContacted: response.lowScoreReview?.customerContacted ?? false,
    dueAt: dateTimeLocalValue(response.lowScoreReview?.dueAt),
  }
}

function lowScoreBatchDraft() {
  return {
    status: batchNoChangeValue,
    severity: batchNoChangeValue,
    ownerMemberId: batchNoChangeValue,
    customerContacted: false,
    dueAt: '',
  }
}

function lowScoreBatchPatchPresent(form: LowScoreBatchDraft) {
  return (
    form.status !== batchNoChangeValue ||
    form.severity !== batchNoChangeValue ||
    form.ownerMemberId !== batchNoChangeValue ||
    form.customerContacted ||
    form.dueAt.trim() !== ''
  )
}

function lowScoreBatchRequest(
  responseIDs: string[],
  form: LowScoreBatchDraft,
): BatchUpdateSurveyLowScoreReviewsRequest {
  const body: BatchUpdateSurveyLowScoreReviewsRequest = {
    responseIds: responseIDs,
    status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_UNSPECIFIED,
    severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_UNSPECIFIED,
  }
  if (form.status !== batchNoChangeValue) {
    body.status = form.status as SurveyLowScoreReviewStatus
  }
  if (form.severity !== batchNoChangeValue) {
    body.severity = form.severity as SurveyLowScoreSeverity
  }
  if (form.ownerMemberId !== batchNoChangeValue) {
    body.ownerMemberId = form.ownerMemberId === unassignedOwnerValue ? '' : form.ownerMemberId
  }
  if (form.customerContacted) {
    body.customerContacted = true
  }
  if (form.dueAt.trim()) {
    body.dueAt = dateTimeLocalToRFC3339(form.dueAt)
  }
  return body
}

function lowScoreAssignmentCandidateIDs(members: Member[]) {
  const ids = new Set<string>()
  for (const member of members) {
    if (!member.id || member.role === 'viewer' || member.acceptedAt === '0') continue
    ids.add(member.id)
  }
  return Array.from(ids).sort()
}

function integerOrUndefined(raw: string) {
  const value = Number(raw)
  return Number.isInteger(value) ? value : undefined
}

function numberOrUndefined(raw: string) {
  const value = Number(raw)
  return Number.isFinite(value) ? value : undefined
}

function healthStatusLabel(t: TFunction, value?: SurveyCampaignHealthStatus) {
  if (value === campaignHealthBlocked) return t('surveys.health.status.blocked')
  if (value === campaignHealthNeedsAttention) return t('surveys.health.status.needs_attention')
  return t('surveys.health.status.healthy')
}

function healthStatusBadge(value?: SurveyCampaignHealthStatus) {
  if (value === campaignHealthBlocked) return 'border-destructive/40 text-destructive'
  if (value === campaignHealthNeedsAttention) return 'border-amber-400 text-amber-700'
  return 'border-emerald-500/50 text-emerald-700'
}

function healthCheckRank(value: SurveyCampaignHealthCheckStatus) {
  if (value === healthCheckFail) return 3
  if (value === healthCheckWarn) return 2
  return 1
}

function healthCheckBorder(value: SurveyCampaignHealthCheckStatus) {
  if (value === healthCheckFail) return 'border-destructive/50 bg-destructive/5'
  if (value === healthCheckWarn) return 'border-amber-300 bg-amber-50/70'
  return 'border-border/60 bg-muted/20'
}

function healthCheckIcon(value: SurveyCampaignHealthCheckStatus): ReactNode {
  if (value === healthCheckFail) {
    return <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
  }
  if (value === healthCheckWarn) {
    return <CircleAlert className="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
  }
  return <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-600" />
}

function healthCheckTitle(t: TFunction, check: SurveyCampaignHealthCheck) {
  return surveyCheckText(t, check.id, 'title', check.title)
}

function healthCheckSummary(t: TFunction, check: SurveyCampaignHealthCheck) {
  return surveyCheckText(t, check.id, 'summary', check.summary)
}

function healthCheckAction(t: TFunction, check: SurveyCampaignHealthCheck) {
  return surveyCheckText(t, check.id, 'action', check.recommendedAction)
}

function surveyCheckText(
  t: TFunction,
  id: string,
  field: 'action' | 'summary' | 'title',
  fallback: string,
) {
  const key = `surveys.health.check_items.${surveyEnumKey(id)}.${field}`
  return t(key, { defaultValue: fallback })
}

function insightSeverityLabel(t: TFunction, value: SurveyAnalyticsInsightSeverity) {
  if (value === criticalInsightSeverity) return t('surveys.analytics_insights.severity.critical')
  if (value === warningInsightSeverity) return t('surveys.analytics_insights.severity.warning')
  return t('surveys.analytics_insights.severity.info')
}

function insightSeverityBorder(value: SurveyAnalyticsInsightSeverity) {
  if (value === criticalInsightSeverity) return 'border-destructive/50 bg-destructive/5'
  if (value === warningInsightSeverity) return 'border-amber-300 bg-amber-50/70'
  return 'border-border/60 bg-muted/20'
}

function insightSeverityBadge(value: SurveyAnalyticsInsightSeverity) {
  if (value === criticalInsightSeverity) return 'border-destructive/40 text-destructive'
  if (value === warningInsightSeverity) return 'border-amber-400 text-amber-700'
  return 'border-border/60 text-muted-foreground'
}

function insightTitle(t: TFunction, insight: SurveyAnalyticsInsight) {
  if (insight.id.startsWith('survey-segment-attention-')) {
    return t('surveys.analytics_insights.items.segment.title')
  }
  switch (insight.id) {
    case 'survey-overdue-low-score-reviews':
      return t('surveys.analytics_insights.items.overdue.title')
    case 'survey-critical-low-score-reviews':
      return t('surveys.analytics_insights.items.critical_reviews.title')
    case 'survey-unassigned-low-score-reviews':
      return t('surveys.analytics_insights.items.unassigned_reviews.title')
    case 'survey-pending-customer-contact':
      return t('surveys.analytics_insights.items.pending_contact.title')
    case 'survey-missing-root-cause-reviews':
      return t('surveys.analytics_insights.items.missing_root_cause.title')
    case 'survey-missing-action-reviews':
      return t('surveys.analytics_insights.items.missing_action.title')
    case 'survey-low-score-rate':
      return t('surveys.analytics_insights.items.low_score_rate.title')
    case 'survey-response-rate':
      return t('surveys.analytics_insights.items.response_rate.title')
    case 'survey-suppression-rate':
      return t('surveys.analytics_insights.items.suppression_rate.title')
    case 'survey-expired-rate':
      return t('surveys.analytics_insights.items.expired_rate.title')
    case 'survey-health-stable':
      return t('surveys.analytics_insights.items.stable.title')
    default:
      return insight.title
  }
}

function insightSummary(t: TFunction, insight: SurveyAnalyticsInsight) {
  if (insight.id.startsWith('survey-segment-attention-')) {
    return t('surveys.analytics_insights.items.segment.summary')
  }
  switch (insight.id) {
    case 'survey-overdue-low-score-reviews':
      return t('surveys.analytics_insights.items.overdue.summary')
    case 'survey-critical-low-score-reviews':
      return t('surveys.analytics_insights.items.critical_reviews.summary')
    case 'survey-unassigned-low-score-reviews':
      return t('surveys.analytics_insights.items.unassigned_reviews.summary')
    case 'survey-pending-customer-contact':
      return t('surveys.analytics_insights.items.pending_contact.summary')
    case 'survey-missing-root-cause-reviews':
      return t('surveys.analytics_insights.items.missing_root_cause.summary')
    case 'survey-missing-action-reviews':
      return t('surveys.analytics_insights.items.missing_action.summary')
    case 'survey-low-score-rate':
      return t('surveys.analytics_insights.items.low_score_rate.summary')
    case 'survey-response-rate':
      return t('surveys.analytics_insights.items.response_rate.summary')
    case 'survey-suppression-rate':
      return t('surveys.analytics_insights.items.suppression_rate.summary')
    case 'survey-expired-rate':
      return t('surveys.analytics_insights.items.expired_rate.summary')
    case 'survey-health-stable':
      return t('surveys.analytics_insights.items.stable.summary')
    default:
      return insight.summary
  }
}

function insightAction(t: TFunction, insight: SurveyAnalyticsInsight) {
  if (insight.id.startsWith('survey-segment-attention-')) {
    return t('surveys.analytics_insights.items.segment.action')
  }
  switch (insight.id) {
    case 'survey-overdue-low-score-reviews':
      return t('surveys.analytics_insights.items.overdue.action')
    case 'survey-critical-low-score-reviews':
      return t('surveys.analytics_insights.items.critical_reviews.action')
    case 'survey-unassigned-low-score-reviews':
      return t('surveys.analytics_insights.items.unassigned_reviews.action')
    case 'survey-pending-customer-contact':
      return t('surveys.analytics_insights.items.pending_contact.action')
    case 'survey-missing-root-cause-reviews':
      return t('surveys.analytics_insights.items.missing_root_cause.action')
    case 'survey-missing-action-reviews':
      return t('surveys.analytics_insights.items.missing_action.action')
    case 'survey-low-score-rate':
      return t('surveys.analytics_insights.items.low_score_rate.action')
    case 'survey-response-rate':
      return t('surveys.analytics_insights.items.response_rate.action')
    case 'survey-suppression-rate':
      return t('surveys.analytics_insights.items.suppression_rate.action')
    case 'survey-expired-rate':
      return t('surveys.analytics_insights.items.expired_rate.action')
    case 'survey-health-stable':
      return t('surveys.analytics_insights.items.stable.action')
    default:
      return insight.recommendedAction
  }
}

function insightActionTarget(insight: SurveyAnalyticsInsight): InsightActionTarget {
  if (insight.id.startsWith('survey-segment-attention-')) return 'segments'
  switch (insight.id) {
    case 'survey-overdue-low-score-reviews':
    case 'survey-critical-low-score-reviews':
    case 'survey-unassigned-low-score-reviews':
    case 'survey-pending-customer-contact':
    case 'survey-missing-root-cause-reviews':
    case 'survey-missing-action-reviews':
    case 'survey-low-score-rate':
      return 'low-scores'
    case 'survey-response-rate':
    case 'survey-suppression-rate':
    case 'survey-expired-rate':
      return 'settings'
    default:
      return 'analytics'
  }
}

function insightActionLabel(t: TFunction, insight: SurveyAnalyticsInsight) {
  switch (insightActionTarget(insight)) {
    case 'low-scores':
      return t('surveys.analytics_insights.cta.low_scores')
    case 'segments':
      return t('surveys.analytics_insights.cta.segments')
    case 'settings':
      return t('surveys.analytics_insights.cta.settings')
    case 'analytics':
      return t('surveys.analytics_insights.cta.analytics')
  }
}

function formatInsightValue(insight: SurveyAnalyticsInsight) {
  if (insight.metric === 'overall_health') return 'OK'
  if (insight.metric.endsWith('_rate')) return formatPercent(insight.value)
  if (Number.isInteger(insight.value)) return String(insight.value)
  return insight.value.toFixed(1)
}

function formatInsightThreshold(insight: SurveyAnalyticsInsight) {
  if (insight.metric === 'overall_health') return 'OK'
  if (insight.metric.endsWith('_rate')) return formatPercent(insight.threshold)
  if (Number.isInteger(insight.threshold)) return String(insight.threshold)
  return insight.threshold.toFixed(1)
}

function formatPercent(value?: number) {
  return `${Math.round((value ?? 0) * 100)}%`
}

function formatScore(analytics?: SurveyAnalytics) {
  if (!analytics || analytics.completedCount === 0) return '0'
  return analytics.averageScore.toFixed(1)
}

function formatDuration(seconds?: number) {
  const total = Math.max(0, Math.round(seconds ?? 0))
  if (total === 0) return '0m'
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  if (minutes > 0) return `${minutes}m`
  return '<1m'
}

function formatTrendDate(value: string, locale: string) {
  const date = new Date(`${value}T00:00:00Z`)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale || 'zh-CN', {
    month: '2-digit',
    day: '2-digit',
    timeZone: 'UTC',
  }).format(date)
}

function shortEnum(value: string) {
  return value
    .replace(/^SURVEY_/, '')
    .replace(/^(DELIVERY|RESPONSE|SUPPRESSION)_STATUS_/, '')
    .replace(/_/g, ' ')
    .toLowerCase()
}

function surveyDeliveryStatusLabel(t: TFunction, value?: string) {
  return surveyEnumLabel(t, 'surveys.delivery_status', value)
}

function surveyResponseStatusLabel(t: TFunction, value?: string) {
  return surveyEnumLabel(t, 'surveys.response_status', value)
}

function surveyReasonLabel(t: TFunction, value?: string) {
  return surveyEnumLabel(t, 'surveys.reasons', value)
}

function surveyEnumLabel(t: TFunction, namespace: string, value?: string) {
  const key = surveyEnumKey(value)
  const fallback = shortEnum(value || 'unknown')
  return t(`${namespace}.${key}`, { defaultValue: fallback })
}

function surveyEnumKey(value?: string) {
  return (value || 'unknown')
    .replace(/^SURVEY_/, '')
    .replace(/^(DELIVERY|RESPONSE|SUPPRESSION)_STATUS_/, '')
    .toLowerCase()
}

function formatTimestamp(value: string, locale: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale || 'zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function dateTimeLocalValue(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}`
}

function dateTimeLocalToRFC3339(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return ''
  const date = new Date(trimmed)
  if (Number.isNaN(date.getTime())) return trimmed
  return date.toISOString()
}

function lowScoreCommandSummary(t: TFunction, analytics?: SurveyAnalytics) {
  if (!analytics || analytics.openLowScoreReviewCount === 0) {
    return t('surveys.low_score_command.summary.stable')
  }
  if (analytics.overdueLowScoreReviewCount > 0) {
    return t('surveys.low_score_command.summary.overdue')
  }
  if (analytics.unassignedLowScoreReviewCount > 0) {
    return t('surveys.low_score_command.summary.unassigned')
  }
  if (analytics.criticalLowScoreReviewCount > 0) {
    return t('surveys.low_score_command.summary.critical')
  }
  if (analytics.pendingCustomerContactReviewCount > 0) {
    return t('surveys.low_score_command.summary.contact')
  }
  if (analytics.missingRootCauseRecoveryQueueCount > 0) {
    return t('surveys.low_score_command.summary.root_cause')
  }
  if (analytics.missingActionRecoveryQueueCount > 0) {
    return t('surveys.low_score_command.summary.action')
  }
  return t('surveys.low_score_command.summary.healthy')
}

function lowScoreCommandSteps(t: TFunction, analytics?: SurveyAnalytics) {
  if (!analytics || analytics.openLowScoreReviewCount === 0) {
    return [
      t('surveys.low_score_command.steps.monitor'),
      t('surveys.low_score_command.steps.keep_thresholds'),
      t('surveys.low_score_command.steps.review_segments'),
    ]
  }
  const steps: string[] = []
  if (analytics.overdueLowScoreReviewCount > 0) {
    steps.push(t('surveys.low_score_command.steps.resolve_overdue'))
  }
  if (analytics.unassignedLowScoreReviewCount > 0) {
    steps.push(t('surveys.low_score_command.steps.assign_owners'))
  }
  if (analytics.criticalLowScoreReviewCount > 0) {
    steps.push(t('surveys.low_score_command.steps.escalate_critical'))
  }
  if (analytics.pendingCustomerContactReviewCount > 0) {
    steps.push(t('surveys.low_score_command.steps.contact_customers'))
  }
  if (analytics.missingRootCauseRecoveryQueueCount > 0) {
    steps.push(t('surveys.low_score_command.steps.record_root_cause'))
  }
  if (analytics.missingActionRecoveryQueueCount > 0) {
    steps.push(t('surveys.low_score_command.steps.record_actions'))
  }
  if (steps.length === 0) {
    steps.push(t('surveys.low_score_command.steps.record_root_cause'))
  }
  return steps.slice(0, 4)
}

function ownerLoadLabel(load: SurveyRecoveryOwnerLoad, members: Member[]) {
  const member = members.find((item) => item.id === load.ownerMemberId)
  return member ? memberLabel(member) : load.ownerMemberId
}

function ownerLoadUrgent(load: SurveyRecoveryOwnerLoad) {
  return load.overdueCount > 0 || load.criticalCount > 0 || load.workloadScore >= 60
}

function suggestedRecoveryOwner(members: Member[], loads: SurveyRecoveryOwnerLoad[]) {
  const scores = new Map(loads.map((load) => [load.ownerMemberId, load.workloadScore]))
  return members
    .filter((member) => member.memberType !== 'invite')
    .sort((left, right) => {
      const scoreDiff = (scores.get(left.id) ?? 0) - (scores.get(right.id) ?? 0)
      if (scoreDiff !== 0) return scoreDiff
      return memberLabel(left).localeCompare(memberLabel(right))
    })[0]
}

function prioritizeLowScoreResponses(responses: SurveyResponse[], nowMs: number) {
  return [...responses].sort((left, right) => {
    const leftRank = lowScoreResponseRank(left, nowMs)
    const rightRank = lowScoreResponseRank(right, nowMs)
    for (let idx = 0; idx < leftRank.length; idx += 1) {
      const diff = leftRank[idx] - rightRank[idx]
      if (diff !== 0) return diff
    }
    return left.id.localeCompare(right.id)
  })
}

function lowScoreResponseRank(response: SurveyResponse, nowMs: number) {
  const review = response.lowScoreReview
  const dueMs = timestampMs(review?.dueAt)
  const submittedMs = timestampMs(response.submittedAt)
  const terminal =
    review?.status === resolvedReviewStatus || review?.status === dismissedReviewStatus
  const overdue = !terminal && dueMs < nowMs
  return [
    terminal ? 1 : 0,
    overdue ? 0 : 1,
    dueMs,
    -severityPriority(review?.severity),
    -submittedMs,
  ]
}

function timestampMs(value?: string) {
  if (!value) return Number.POSITIVE_INFINITY
  const parsed = Date.parse(value)
  return Number.isNaN(parsed) ? Number.POSITIVE_INFINITY : parsed
}

function lowScoreSeverityLabel(t: TFunction, value?: SurveyLowScoreSeverity) {
  if (value === SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL) {
    return t('surveys.severity.critical')
  }
  if (value === SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH) {
    return t('surveys.severity.high')
  }
  if (value === SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_LOW) {
    return t('surveys.severity.low')
  }
  return t('surveys.severity.medium')
}

function severityPriority(value?: SurveyLowScoreSeverity) {
  if (value === SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL) return 4
  if (value === SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH) return 3
  if (value === SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_MEDIUM) return 2
  if (value === SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_LOW) return 1
  return 0
}

function recoverySLAStatusLabel(t: TFunction, value?: SurveyRecoverySlaStatus) {
  if (value === recoverySLAOverdue) return t('surveys.low_scores.sla.overdue')
  if (value === recoverySLADueSoon) return t('surveys.low_scores.sla.due_soon')
  if (value === SurveyRecoverySlaStatus.SURVEY_RECOVERY_SLA_STATUS_CLOSED) {
    return t('surveys.low_scores.sla.closed')
  }
  if (value === SurveyRecoverySlaStatus.SURVEY_RECOVERY_SLA_STATUS_ON_TRACK) {
    return t('surveys.low_scores.sla.on_track')
  }
  return t('surveys.low_scores.sla.unknown')
}

function lowScoreFocusLabel(t: TFunction, value: LowScoreFocus) {
  switch (value) {
    case 'critical':
      return t('surveys.low_scores.focus.critical')
    case 'overdue':
      return t('surveys.low_scores.focus.overdue')
    case 'unassigned':
      return t('surveys.low_scores.focus.unassigned')
    case 'pending-contact':
      return t('surveys.low_scores.focus.pending_contact')
    case 'root-cause':
      return t('surveys.low_scores.focus.root_cause')
    case 'action-missing':
      return t('surveys.low_scores.focus.action_missing')
    default:
      return t('surveys.low_scores.focus.all')
  }
}

function lowScoreFocusCount(analytics: SurveyAnalytics | undefined, value: LowScoreFocus) {
  if (!analytics) return undefined
  switch (value) {
    case 'critical':
      return analytics.criticalLowScoreReviewCount
    case 'overdue':
      return analytics.overdueRecoveryQueueCount
    case 'unassigned':
      return analytics.unassignedRecoveryQueueCount
    case 'pending-contact':
      return analytics.pendingContactRecoveryQueueCount
    case 'root-cause':
      return analytics.missingRootCauseRecoveryQueueCount
    case 'action-missing':
      return analytics.missingActionRecoveryQueueCount
    default:
      return analytics.lowScoreCount
  }
}

function lowScoreOwnerFilterOptions(
  t: TFunction,
  members: Member[],
  selectedID: string,
): [string, string][] {
  const owners = new Map<string, string>([
    [allLowScoreOwnersValue, t('surveys.low_scores.all_owners')],
  ])
  for (const member of members) {
    if (member.memberType === 'invite') continue
    owners.set(member.id, memberLabel(member))
  }
  if (selectedID !== allLowScoreOwnersValue && selectedID && !owners.has(selectedID)) {
    owners.set(selectedID, selectedID)
  }
  return Array.from(owners.entries()).sort((left, right) => {
    if (left[0] === allLowScoreOwnersValue) return -1
    if (right[0] === allLowScoreOwnersValue) return 1
    return left[1].localeCompare(right[1])
  })
}

function recoveryBlockerLabel(t: TFunction, value?: string) {
  switch (value) {
    case 'overdue_sla':
      return t('surveys.low_scores.blocker.overdue_sla')
    case 'owner_missing':
      return t('surveys.low_scores.blocker.owner_missing')
    case 'due_missing':
      return t('surveys.low_scores.blocker.due_missing')
    case 'customer_contact_missing':
      return t('surveys.low_scores.blocker.customer_contact_missing')
    case 'root_cause_missing':
      return t('surveys.low_scores.blocker.root_cause_missing')
    case 'action_missing':
      return t('surveys.low_scores.blocker.action_missing')
    case 'none':
      return t('surveys.low_scores.blocker.none')
    default:
      return t('surveys.low_scores.blocker.unknown')
  }
}

function recoveryNotificationStatusLabel(t: TFunction, value?: SurveyRecoveryNotificationStatus) {
  switch (value) {
    case recoveryNotificationPending:
      return t('surveys.low_scores.notification_status.pending')
    case recoveryNotificationDelivered:
      return t('surveys.low_scores.notification_status.delivered')
    case recoveryNotificationFailed:
      return t('surveys.low_scores.notification_status.failed')
    case recoveryNotificationDead:
      return t('surveys.low_scores.notification_status.dead')
    case recoveryNotificationSuppressed:
      return t('surveys.low_scores.notification_status.suppressed')
    default:
      return ''
  }
}

function recoveryNotificationReasonLabel(t: TFunction, value?: string) {
  switch (value) {
    case '':
    case undefined:
      return ''
    case 'overdue_sla':
    case 'owner_missing':
    case 'due_missing':
    case 'customer_contact_missing':
    case 'root_cause_missing':
    case 'action_missing':
    case 'none':
      return recoveryBlockerLabel(t, value)
    case 'critical_recovery':
      return t('surveys.low_scores.notification_reason.critical_recovery')
    case 'high_risk_recovery':
      return t('surveys.low_scores.notification_reason.high_risk_recovery')
    case 'owner_unavailable':
      return t('surveys.low_scores.notification_reason.owner_unavailable')
    case 'owner_email_missing':
      return t('surveys.low_scores.notification_reason.owner_email_missing')
    case 'email_sender_not_configured':
      return t('surveys.low_scores.notification_reason.email_sender_not_configured')
    case 'transport':
      return t('surveys.low_scores.notification_reason.transport')
    default:
      return value
  }
}

function recoveryActionLabel(t: TFunction, value?: string) {
  switch (value) {
    case 'resolve_overdue':
      return t('surveys.low_scores.action.resolve_overdue')
    case 'assign_owner':
      return t('surveys.low_scores.action.assign_owner')
    case 'set_due_date':
      return t('surveys.low_scores.action.set_due_date')
    case 'contact_customer':
      return t('surveys.low_scores.action.contact_customer')
    case 'capture_root_cause':
      return t('surveys.low_scores.action.capture_root_cause')
    case 'record_action':
      return t('surveys.low_scores.action.record_action')
    case 'start_review':
      return t('surveys.low_scores.action.start_review')
    case 'complete_review':
      return t('surveys.low_scores.action.complete_review')
    case 'monitor_recovery':
      return t('surveys.low_scores.action.monitor_recovery')
    default:
      return t('surveys.low_scores.action.unknown')
  }
}

function lowScoreReviewStatusLabel(t: TFunction, value?: SurveyLowScoreReviewStatus) {
  if (value === SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW) {
    return t('surveys.review_status.in_review')
  }
  if (value === resolvedReviewStatus) return t('surveys.review_status.resolved')
  if (value === dismissedReviewStatus) return t('surveys.review_status.dismissed')
  return t('surveys.review_status.open')
}

function lowScoreStatusOptions(t: TFunction): [string, string][] {
  return [
    [
      SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_OPEN,
      t('surveys.review_status.open'),
    ],
    [
      SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW,
      t('surveys.review_status.in_review'),
    ],
    [resolvedReviewStatus, t('surveys.review_status.resolved')],
    [dismissedReviewStatus, t('surveys.review_status.dismissed')],
  ]
}

function lowScoreSeverityOptions(t: TFunction): [string, string][] {
  return [
    [SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_LOW, t('surveys.severity.low')],
    [defaultLowScoreSeverity, t('surveys.severity.medium')],
    [SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH, t('surveys.severity.high')],
    [SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL, t('surveys.severity.critical')],
  ]
}

function lowScoreBatchStatusOptions(t: TFunction): [string, string][] {
  return [
    [batchNoChangeValue, t('surveys.low_scores.batch.no_change')],
    ...lowScoreStatusOptions(t),
  ]
}

function lowScoreBatchSeverityOptions(t: TFunction): [string, string][] {
  return [
    [batchNoChangeValue, t('surveys.low_scores.batch.no_change')],
    ...lowScoreSeverityOptions(t),
  ]
}

function lowScoreBatchOwnerOptions(
  t: TFunction,
  members: Member[],
  selectedID: string,
): [string, string][] {
  return [
    [batchNoChangeValue, t('surveys.low_scores.batch.no_change')],
    ...lowScoreOwnerOptions(t, members, selectedID === batchNoChangeValue ? '' : selectedID),
  ]
}

function lowScoreOwnerOptions(
  t: TFunction,
  members: Member[],
  selectedID: string,
): [string, string][] {
  const owners = new Map<string, string>([
    [unassignedOwnerValue, t('surveys.low_scores.owner_unassigned')],
  ])
  for (const member of members) {
    if (member.memberType === 'invite') continue
    owners.set(member.id, memberLabel(member))
  }
  if (selectedID && !owners.has(selectedID)) {
    owners.set(selectedID, selectedID)
  }
  return Array.from(owners.entries()).sort((left, right) => {
    if (left[0] === unassignedOwnerValue) return -1
    if (right[0] === unassignedOwnerValue) return 1
    return left[1].localeCompare(right[1])
  })
}

function memberLabel(member: Member) {
  return member.email || member.userId || member.id
}

function surveyTypeLabel(t: TFunction, value: SurveyType) {
  if (value === SurveyType.SURVEY_TYPE_CES) return t('surveys.type.ces')
  return t('surveys.type.csat')
}

function campaignStatusLabel(t: TFunction, value: SurveyCampaignStatus) {
  if (value === activeCampaignStatus) return t('surveys.status.active')
  if (value === draftCampaignStatus) return t('surveys.status.draft')
  return t('surveys.status.archived')
}

function triggerLabel(t: TFunction, value: SurveyTriggerEvent) {
  if (value === replySentTrigger) return t('surveys.trigger.reply_sent')
  if (value === requestResolvedTrigger) return t('surveys.trigger.request_resolved')
  if (value === manualLinkTrigger) return t('surveys.trigger.manual_link')
  return t('surveys.trigger.workflow_transition')
}

function distributionLabel(t: TFunction, value: SurveyDistributionMode) {
  if (value === sourceLinkMode) return t('surveys.distribution.source_link')
  return t('surveys.distribution.contact_email')
}
