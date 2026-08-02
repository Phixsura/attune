import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  CircleGauge,
  ClipboardCheck,
  DatabaseZap,
  HeartPulse,
  Loader2,
  MousePointerClick,
  Play,
  Radar,
  RotateCcw,
  Search,
  ShieldCheck,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  type ClassificationQuality,
  classificationQualityQuery,
  defaultClassificationQualityFilters,
} from '@/features/classification-quality/api/get-classification-quality'
import { type FeedbackStats, feedbackStatsQuery } from '@/features/feedback/api/get-feedback-stats'
import {
  type InboundSource,
  inboundSourcesQuery,
} from '@/features/inbound-sources/api/list-inbound-sources'
import {
  type QualityAction,
  type QualityActionStatus,
  qualityActionsQuery,
  useUpdateQualityAction,
} from '@/features/quality-actions/api/quality-actions'
import {
  defaultSearchQualityFilters,
  type SearchQuality,
  searchQualityQuery,
} from '@/features/search-quality/api/get-search-quality'
import { meQuery } from '@/features/session/api/get-me'
import { surveyAnalyticsQuery } from '@/features/surveys/api/surveys'
import { recoveryReadinessScore } from '@/features/surveys/lib/recovery-readiness'
import { useDocumentTitle } from '@/hooks/use-document-title'
import { api } from '@/lib/api-client'
import type { Role } from '@/lib/permissions'
import { cn } from '@/lib/utils'
import type { SurveyAnalytics } from '@/proto/attune/v1/survey'

type SignalSeverity = 'alert' | 'watch' | 'normal' | 'insufficient_data'

type ControlTowerRisk = {
  id: string
  actionKey: string
  titleKey: string
  bodyKey: string
  recommendationKey: string
  metric: string
  severity: SignalSeverity
  href: '/analytics/classification-quality' | '/analytics/search-quality' | '/integrations/surveys'
}

type ControlTowerLane = {
  id: string
  titleKey: string
  bodyKey: string
  value: string
  severity: SignalSeverity
}

const cohortSyncHealthQuery = {
  queryKey: ['cohort-sync', 'health'],
  queryFn: () =>
    api<{
      sourceCount: number
      activeSources: number
      errorSources: number
      disabledSources: number
      cohortCount: number
      totalActiveMembers: number
      syncsLast24h: number
    }>('/fb/v1/console/cohort-sync/health').catch(() => ({
      sourceCount: 0,
      activeSources: 0,
      errorSources: 0,
      disabledSources: 0,
      cohortCount: 0,
      totalActiveMembers: 0,
      syncsLast24h: 0,
    })),
}

type ProductReadinessStatus = 'pass' | 'watch' | 'blocked' | 'insufficient_data'

type ProductReadinessItem = {
  id: string
  titleKey: string
  standardKey: string
  evidenceKey: string
  evidenceValues: Record<string, string | number>
  gapKey: string
  status: ProductReadinessStatus
}

type FirstValueItem = {
  id: string
  titleKey: string
  evidenceKey: string
  evidenceValues: Record<string, string | number>
  nextKey: string
  status: ProductReadinessStatus
}

type FirstValueScorecard = {
  items: FirstValueItem[]
  passed: number
  total: number
}

type SourceHealthProblem = {
  id: string
  count: number
  labelKey: string
  status: ProductReadinessStatus
}

type SourceHealthSource = {
  channel: string
  id: string
  lastError: string
  lastEventAt: string
  name: string
  status: ProductReadinessStatus
}

type SourceHealthCommandCenter = {
  available: boolean
  nextActionKey: string
  nextActionValues: Record<string, string | number>
  problems: SourceHealthProblem[]
  sources: SourceHealthSource[]
  status: ProductReadinessStatus
  totals: {
    active: number
    disabled: number
    errors: number
    fresh: number
    neverSeen: number
    stale: number
    total: number
  }
}

type RecoveryCommandBlocker = {
  id: string
  count: number
  labelKey: string
  status: ProductReadinessStatus
}

type RecoveryCommandOwnerLoad = {
  critical: number
  dueSoon: number
  oldestDueAt: string
  open: number
  overdue: number
  ownerMemberId: string
  pendingContact: number
  workload: number
}

type RecoveryCommandCenter = {
  available: boolean
  blockers: RecoveryCommandBlocker[]
  nextActionKey: string
  nextActionValues: Record<string, string | number>
  ownerLoads: RecoveryCommandOwnerLoad[]
  status: ProductReadinessStatus
  totals: {
    dueSoon: number
    evidenceDebt: number
    ownerCount: number
    overdue: number
    pendingContact: number
    unassigned: number
  }
}

type ReleaseVerificationEvidence = {
  evidenceKey: string
  id: string
  status: ProductReadinessStatus
  titleKey: string
}

type ReleaseVerificationCommandCenter = {
  evidence: ReleaseVerificationEvidence[]
  nextActionKey: string
  nextActionValues: Record<string, string | number>
  status: ProductReadinessStatus
  totals: {
    blocked: number
    evidencePassed: number
    evidenceTotal: number
    unresolvedRisks: number
    watch: number
  }
}

type WorldClassMaturityGapStatus = 'covered' | 'partial' | 'gap'

type WorldClassMaturityGapDefinition = {
  id: string
  status: WorldClassMaturityGapStatus
}

type WorldClassMaturityExecutionDefinition = {
  gapId: string
  id: string
  priority: number
}

type WorldClassMaturityCategoryDefinition = {
  id: string
  items: WorldClassMaturityGapDefinition[]
}

type WorldClassMaturityGapItem = {
  id: string
  status: WorldClassMaturityGapStatus
  titleKey: string
}

type WorldClassMaturityCategory = {
  descriptionKey: string
  id: string
  items: WorldClassMaturityGapItem[]
  titleKey: string
  totals: {
    covered: number
    gap: number
    partial: number
    total: number
  }
}

type WorldClassMaturityExecutionSlice = {
  acceptanceKey: string
  categoryTitleKey: string
  gapId: string
  id: string
  ownerKey: string
  priority: number
  status: WorldClassMaturityGapStatus
  titleKey: string
  verificationKey: string
}

type WorldClassMaturityRegister = {
  categories: WorldClassMaturityCategory[]
  executionQueue: WorldClassMaturityExecutionSlice[]
  nextActionKey: string
  nextActionValues: Record<string, string | number>
  totals: {
    covered: number
    gap: number
    partial: number
    total: number
  }
}

export const controlTowerQueries = [
  classificationQualityQuery(defaultClassificationQualityFilters),
  searchQualityQuery(defaultSearchQualityFilters),
  qualityActionsQuery({ status: 'all', limit: 100 }),
  cohortSyncHealthQuery,
  feedbackStatsQuery(),
] as const

const sourceFreshnessWindowMs = 48 * 60 * 60 * 1000

export function ControlTowerPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('control_tower.title'))
  const classification = useQuery(controlTowerQueries[0])
  const search = useQuery(controlTowerQueries[1])
  const qualityActions = useQuery(controlTowerQueries[2])
  const cohortHealth = useQuery(controlTowerQueries[3])
  const feedbackStats = useQuery(controlTowerQueries[4])
  const me = useQuery(meQuery())
  const role = me.data?.user?.role as Role | undefined
  const canReadSurveyAnalytics = roleCanReadSurveyAnalytics(role)
  const canReadInboundSources = roleCanReadInboundSources(role)
  const inboundSources = useQuery({
    ...inboundSourcesQuery(),
    enabled: canReadInboundSources,
  })
  const surveyAnalytics = useQuery({
    ...surveyAnalyticsQuery(),
    enabled: canReadSurveyAnalytics,
  })

  const model = useMemo(
    () =>
      buildControlTowerModel(
        classification.data,
        search.data,
        qualityActions.data,
        surveyAnalytics.data,
        canReadSurveyAnalytics,
        feedbackStats.data,
        inboundSources.data,
        canReadInboundSources,
      ),
    [
      canReadSurveyAnalytics,
      canReadInboundSources,
      classification.data,
      feedbackStats.data,
      inboundSources.data,
      qualityActions.data,
      search.data,
      surveyAnalytics.data,
    ],
  )
  const pending =
    me.isPending ||
    classification.isPending ||
    search.isPending ||
    cohortHealth.isPending ||
    feedbackStats.isPending ||
    (canReadInboundSources && inboundSources.isPending)
  const failed = me.isError || classification.isError || search.isError || feedbackStats.isError

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.overview')}
        title={t('control_tower.title')}
        subtitle={t('control_tower.subtitle')}
        metrics={
          <>
            <PageHeroMetric
              label={t('control_tower.hero.readiness')}
              value={t(`control_tower.severity.${model.overallSeverity}`)}
              tone={metricTone(model.overallSeverity)}
            />
            <PageHeroMetric
              label={t('control_tower.hero.active_risks')}
              value={String(model.risks.length)}
              tone={model.risks.length > 0 ? 'urgent' : 'default'}
            />
            <PageHeroMetric
              label={t('control_tower.hero.classification_events')}
              value={formatInt(model.classificationEvents)}
            />
            <PageHeroMetric
              label={t('control_tower.hero.search_coverage')}
              value={formatRate(model.searchCoverage)}
              tone={model.searchCoverage < 0.95 ? 'active' : 'default'}
            />
            {cohortHealth.data && cohortHealth.data.sourceCount > 0 && (
              <PageHeroMetric
                label={t('cohort_sync.title')}
                value={`${cohortHealth.data.activeSources}/${cohortHealth.data.sourceCount}`}
                tone={cohortHealth.data.errorSources > 0 ? 'urgent' : 'default'}
              />
            )}
            <PageHeroMetric
              label={t('control_tower.hero.closed_loop')}
              value={
                model.closedLoop.available
                  ? `${model.closedLoop.readiness}/100`
                  : t('control_tower.proof.restricted')
              }
              tone={metricTone(model.closedLoop.severity)}
            />
          </>
        }
      />

      {pending ? (
        <div className="flex items-center justify-center py-12 text-muted-foreground">
          <Loader2 className="mr-2 size-4 animate-spin" />
          {t('app.loading')}
        </div>
      ) : failed ? (
        <ControlTowerError />
      ) : (
        <ControlTowerBody model={model} actionsUnavailable={qualityActions.isError} />
      )}
    </div>
  )
}

function ControlTowerBody({
  model,
  actionsUnavailable,
}: {
  model: ReturnType<typeof buildControlTowerModel>
  actionsUnavailable: boolean
}) {
  const { t } = useTranslation()
  const updateAction = useUpdateQualityAction()

  const update = (risk: ControlTowerRisk, status: QualityActionStatus) => {
    updateAction.mutate(
      {
        actionKey: risk.actionKey,
        signal: risk.id,
        status,
        severity: risk.severity,
        targetPath: risk.href,
        metricLabel: t(risk.titleKey),
        metricValue: risk.metric,
        recommendationKey: risk.recommendationKey,
        evidenceJson: JSON.stringify({
          metric: risk.metric,
          targetPath: risk.href,
        }),
      },
      {
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
        onSuccess: () => toast.success(t(`control_tower.actions.toast.${status}`)),
      },
    )
  }

  return (
    <div className="space-y-5">
      <div className="grid gap-4 lg:grid-cols-2 2xl:grid-cols-4">
        {model.lanes.map((lane) => (
          <Card key={lane.id} className="rounded-lg border-border/70 shadow-none">
            <CardHeader className="pb-3">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <CardTitle className="text-base">{t(lane.titleKey)}</CardTitle>
                  <CardDescription>{t(lane.bodyKey)}</CardDescription>
                </div>
                <SeverityBadge severity={lane.severity} />
              </div>
            </CardHeader>
            <CardContent>
              <div className="flex flex-wrap items-center gap-2 text-2xl font-semibold tracking-tight">
                <LaneIcon lane={lane.id} />
                <span className="break-all">{lane.value}</span>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      <FirstValueScorecardCard scorecard={model.firstValue} />

      <SourceHealthCommandCenterCard command={model.sourceHealth} />

      <ProductReadinessMatrix items={model.readinessItems} />

      <ReleaseVerificationCommandCenterCard command={model.releaseVerification} />

      <WorldClassMaturityRegisterCard register={model.worldClassMaturity} />

      <RecoveryCommandCenterCard command={model.recoveryCommand} />

      <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_24rem]">
        <Card className="rounded-lg border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('control_tower.actions.title')}</CardTitle>
            <CardDescription>
              {t('control_tower.actions.description', { count: model.risks.length })}
            </CardDescription>
          </CardHeader>
          <CardContent className="divide-y divide-border/60 p-0">
            {model.risks.length === 0 ? (
              <div className="flex items-start gap-3 p-5">
                <CheckCircle2 className="mt-0.5 size-5 text-emerald-600" />
                <div>
                  <div className="font-medium">{t('control_tower.actions.empty_title')}</div>
                  <p className="mt-1 text-sm text-muted-foreground">
                    {t('control_tower.actions.empty_body')}
                  </p>
                </div>
              </div>
            ) : (
              model.risks.map((risk) => (
                <div
                  key={risk.id}
                  data-testid={`control-tower-risk-${risk.id}`}
                  className="flex flex-col gap-3 p-5 sm:flex-row sm:items-start"
                >
                  <SeverityIcon severity={risk.severity} />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="font-medium">{t(risk.titleKey)}</h2>
                      <span className="rounded-full border border-border/70 bg-muted/30 px-2 py-0.5 text-xs text-muted-foreground">
                        {risk.metric}
                      </span>
                      <ActionStatusBadge status={risk.action?.status} />
                    </div>
                    <p className="mt-1 text-sm text-muted-foreground">{t(risk.bodyKey)}</p>
                    <p className="mt-2 text-sm font-medium">{t(risk.recommendationKey)}</p>
                    {actionsUnavailable ? (
                      <p className="mt-2 text-xs text-amber-700">
                        {t('control_tower.actions.unavailable')}
                      </p>
                    ) : null}
                  </div>
                  <div className="flex shrink-0 flex-wrap gap-2 sm:justify-end">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={updateAction.isPending || risk.action?.status === 'acknowledged'}
                      onClick={() => update(risk, 'acknowledged')}
                    >
                      <Play className="size-4" />
                      {t('control_tower.actions.start')}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={updateAction.isPending || risk.action?.status === 'resolved'}
                      onClick={() => update(risk, 'resolved')}
                    >
                      <ClipboardCheck className="size-4" />
                      {t('control_tower.actions.verify')}
                    </Button>
                    <Button asChild variant="ghost" size="sm">
                      <Link to={risk.href}>
                        {t('control_tower.actions.evidence')}
                        <ArrowRight className="size-4" />
                      </Link>
                    </Button>
                  </div>
                </div>
              ))
            )}
          </CardContent>
        </Card>

        <Card className="rounded-lg border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('control_tower.proof.title')}</CardTitle>
            <CardDescription>{t('control_tower.proof.description')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4 pt-5">
            <ProofRow
              icon={<ShieldCheck className="size-4" />}
              label={t('control_tower.proof.classification')}
              value={t('control_tower.proof.classification_value', {
                count: model.classificationWarnings,
              })}
            />
            <ProofRow
              icon={<Search className="size-4" />}
              label={t('control_tower.proof.search')}
              value={model.topZeroResultQuery || t('control_tower.proof.no_zero_query')}
            />
            <ProofRow
              icon={<MousePointerClick className="size-4" />}
              label={t('control_tower.proof.search_engagement')}
              value={formatRate(model.searchClickThrough)}
            />
            <ProofRow
              icon={<Radar className="size-4" />}
              label={t('control_tower.proof.ranking')}
              value={model.rankingVersion || t('control_tower.proof.no_ranking')}
            />
            <ProofRow
              icon={<HeartPulse className="size-4" />}
              label={t('control_tower.proof.closed_loop')}
              value={closedLoopProofValue(model.closedLoop, t)}
            />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function FirstValueScorecardCard({ scorecard }: { scorecard: FirstValueScorecard }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="control-tower-first-value"
      className="rounded-lg border-border/70 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle>{t('control_tower.first_value.title')}</CardTitle>
            <CardDescription>{t('control_tower.first_value.description')}</CardDescription>
          </div>
          <div className="rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium">
            {t('control_tower.first_value.progress', {
              passed: scorecard.passed,
              total: scorecard.total,
            })}
          </div>
        </div>
      </CardHeader>
      <CardContent className="grid gap-3 pt-5 md:grid-cols-2 2xl:grid-cols-5">
        {scorecard.items.map((item) => (
          <div
            key={item.id}
            data-testid={`control-tower-first-value-${item.id}`}
            className="flex min-w-0 flex-col gap-3 rounded-md border border-border/70 p-3"
          >
            <div className="flex flex-wrap items-start justify-between gap-2">
              <h2 className="text-sm font-semibold">{t(item.titleKey)}</h2>
              <ProductReadinessStatusBadge status={item.status} />
            </div>
            <ReadinessEvidence
              label={t('control_tower.first_value.evidence')}
              value={t(item.evidenceKey, item.evidenceValues)}
            />
            <ReadinessEvidence
              label={t('control_tower.first_value.next')}
              value={t(item.nextKey)}
            />
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function SourceHealthCommandCenterCard({ command }: { command: SourceHealthCommandCenter }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="control-tower-source-health"
      className="rounded-lg border-border/70 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle>{t('control_tower.source_health.title')}</CardTitle>
            <CardDescription>{t('control_tower.source_health.description')}</CardDescription>
          </div>
          <ProductReadinessStatusBadge status={command.status} />
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-5">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <RecoveryCommandMetric
            label={t('control_tower.source_health.metrics.active')}
            value={
              command.available
                ? t('control_tower.source_health.metrics.active_value', {
                    active: command.totals.active,
                    total: command.totals.total,
                  })
                : t('control_tower.proof.restricted')
            }
          />
          <RecoveryCommandMetric
            label={t('control_tower.source_health.metrics.freshness')}
            value={
              command.available
                ? t('control_tower.source_health.metrics.freshness_value', {
                    fresh: command.totals.fresh,
                    stale: command.totals.stale,
                  })
                : t('control_tower.proof.restricted')
            }
          />
          <RecoveryCommandMetric
            label={t('control_tower.source_health.metrics.failures')}
            value={
              command.available
                ? t('control_tower.source_health.metrics.failures_value', {
                    errors: command.totals.errors,
                    never: command.totals.neverSeen,
                  })
                : t('control_tower.proof.restricted')
            }
          />
          <RecoveryCommandMetric
            label={t('control_tower.source_health.metrics.disabled')}
            value={
              command.available
                ? t('control_tower.source_health.metrics.disabled_value', {
                    disabled: command.totals.disabled,
                  })
                : t('control_tower.proof.restricted')
            }
          />
        </div>

        <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_minmax(0,24rem)]">
          <div className="rounded-md border border-border/70 p-4">
            <div className="text-sm font-semibold">
              {t('control_tower.source_health.problems.title')}
            </div>
            <div className="mt-3 grid gap-2 sm:grid-cols-2">
              {command.problems.length === 0 ? (
                <div className="flex items-start gap-2 text-sm text-muted-foreground sm:col-span-2">
                  <CheckCircle2 className="mt-0.5 size-4 text-emerald-600" />
                  {t('control_tower.source_health.problems.empty')}
                </div>
              ) : (
                command.problems.map((problem) => (
                  <div
                    key={problem.id}
                    data-testid={`control-tower-source-problem-${problem.id}`}
                    className="flex min-w-0 items-center justify-between gap-3 rounded-md border border-border/60 px-3 py-2"
                  >
                    <span className="truncate text-sm">{t(problem.labelKey)}</span>
                    <span className="inline-flex shrink-0 items-center gap-2">
                      <ProductReadinessStatusBadge status={problem.status} />
                      <span className="text-sm font-semibold tabular-nums">{problem.count}</span>
                    </span>
                  </div>
                ))
              )}
            </div>
          </div>

          <div className="min-w-0 rounded-md border border-border/70 p-4">
            <div className="break-words text-sm font-semibold">
              {t('control_tower.source_health.sources.title')}
            </div>
            <div className="mt-3 space-y-3">
              {command.sources.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  {command.available
                    ? t('control_tower.source_health.sources.empty')
                    : t('control_tower.source_health.sources.restricted')}
                </p>
              ) : (
                command.sources.slice(0, 4).map((source) => (
                  <div key={source.id} className="min-w-0">
                    <div className="flex flex-col items-start gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
                      <div className="min-w-0 flex-1 break-words text-sm font-medium">
                        {source.name}
                      </div>
                      <ProductReadinessStatusBadge status={source.status} />
                    </div>
                    <p className="mt-1 break-words text-xs text-muted-foreground">
                      {t('control_tower.source_health.sources.detail', {
                        channel: source.channel,
                      })}
                    </p>
                    <p className="mt-1 break-words text-xs text-muted-foreground">
                      {source.lastEventAt
                        ? t('control_tower.source_health.sources.last_event', {
                            value: formatDateTime(source.lastEventAt),
                          })
                        : t('control_tower.source_health.sources.no_event')}
                    </p>
                    {source.lastError ? (
                      <p className="mt-1 break-words text-xs text-red-700">
                        {t('control_tower.source_health.sources.error', {
                          value: source.lastError,
                        })}
                      </p>
                    ) : null}
                  </div>
                ))
              )}
            </div>
          </div>
        </div>

        <div className="rounded-md border border-border/70 bg-muted/15 p-4">
          <div className="text-xs font-medium text-muted-foreground">
            {t('control_tower.source_health.next_action')}
          </div>
          <div className="mt-1 text-sm font-medium">
            {t(command.nextActionKey, command.nextActionValues)}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function ProductReadinessMatrix({ items }: { items: ProductReadinessItem[] }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="control-tower-readiness-matrix"
      className="rounded-lg border-border/70 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <CardTitle>{t('control_tower.readiness_matrix.title')}</CardTitle>
        <CardDescription>{t('control_tower.readiness_matrix.description')}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3 pt-5 md:grid-cols-2 2xl:grid-cols-5">
        {items.map((item) => (
          <div
            key={item.id}
            data-testid={`control-tower-readiness-${item.id}`}
            className="flex min-w-0 flex-col gap-3 rounded-md border border-border/70 p-3"
          >
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="min-w-0">
                <h2 className="text-sm font-semibold">{t(item.titleKey)}</h2>
                <p className="mt-1 text-xs leading-5 text-muted-foreground">
                  {t(item.standardKey)}
                </p>
              </div>
              <ProductReadinessStatusBadge status={item.status} />
            </div>
            <div className="mt-auto space-y-2">
              <ReadinessEvidence
                label={t('control_tower.readiness_matrix.evidence')}
                value={t(item.evidenceKey, item.evidenceValues)}
              />
              <ReadinessEvidence
                label={t('control_tower.readiness_matrix.gap')}
                value={t(item.gapKey)}
              />
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function ReleaseVerificationCommandCenterCard({
  command,
}: {
  command: ReleaseVerificationCommandCenter
}) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="control-tower-release-verification"
      className="rounded-lg border-border/70 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle>{t('control_tower.release_verification.title')}</CardTitle>
            <CardDescription>{t('control_tower.release_verification.description')}</CardDescription>
          </div>
          <ProductReadinessStatusBadge status={command.status} />
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-5">
        <div className="grid gap-3 md:grid-cols-3">
          <RecoveryCommandMetric
            label={t('control_tower.release_verification.metrics.runtime')}
            value={t('control_tower.release_verification.metrics.runtime_value', {
              blocked: command.totals.blocked,
              watch: command.totals.watch,
            })}
          />
          <RecoveryCommandMetric
            label={t('control_tower.release_verification.metrics.risks')}
            value={t('control_tower.release_verification.metrics.risks_value', {
              risks: command.totals.unresolvedRisks,
            })}
          />
          <RecoveryCommandMetric
            label={t('control_tower.release_verification.metrics.evidence')}
            value={t('control_tower.release_verification.metrics.evidence_value', {
              passed: command.totals.evidencePassed,
              total: command.totals.evidenceTotal,
            })}
          />
        </div>

        <div className="rounded-md border border-border/70 p-4">
          <div className="text-sm font-semibold">
            {t('control_tower.release_verification.evidence.title')}
          </div>
          <div className="mt-3 grid gap-2 md:grid-cols-2 xl:grid-cols-3">
            {command.evidence.map((item) => (
              <div
                key={item.id}
                data-testid={`control-tower-release-evidence-${item.id}`}
                className="flex min-w-0 flex-col gap-2 rounded-md border border-border/60 px-3 py-2"
              >
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <span className="text-sm font-medium">{t(item.titleKey)}</span>
                  <ProductReadinessStatusBadge status={item.status} />
                </div>
                <p className="text-xs leading-5 text-muted-foreground">{t(item.evidenceKey)}</p>
              </div>
            ))}
          </div>
        </div>

        <div className="rounded-md border border-border/70 bg-muted/15 p-4">
          <div className="text-xs font-medium text-muted-foreground">
            {t('control_tower.release_verification.next_action')}
          </div>
          <div className="mt-1 text-sm font-medium">
            {t(command.nextActionKey, command.nextActionValues)}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function WorldClassMaturityRegisterCard({ register }: { register: WorldClassMaturityRegister }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="control-tower-world-class-maturity"
      className="rounded-lg border-border/70 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle>{t('control_tower.world_class_maturity.title')}</CardTitle>
            <CardDescription>{t('control_tower.world_class_maturity.description')}</CardDescription>
          </div>
          <div className="rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium">
            {t('control_tower.world_class_maturity.summary', {
              total: register.totals.total,
            })}
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-5">
        <div className="grid gap-3 md:grid-cols-3">
          <RecoveryCommandMetric
            label={t('control_tower.world_class_maturity.metrics.gap')}
            value={t('control_tower.world_class_maturity.metrics.gap_value', {
              count: register.totals.gap,
            })}
          />
          <RecoveryCommandMetric
            label={t('control_tower.world_class_maturity.metrics.partial')}
            value={t('control_tower.world_class_maturity.metrics.partial_value', {
              count: register.totals.partial,
            })}
          />
          <RecoveryCommandMetric
            label={t('control_tower.world_class_maturity.metrics.covered')}
            value={t('control_tower.world_class_maturity.metrics.covered_value', {
              count: register.totals.covered,
            })}
          />
        </div>

        <div className="rounded-md border border-border/70 p-4">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="text-sm font-semibold">
                {t('control_tower.world_class_maturity.execution.title')}
              </div>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                {t('control_tower.world_class_maturity.execution.description')}
              </p>
            </div>
            <div className="rounded-sm border border-border/70 bg-muted/20 px-2 py-1 text-xs font-medium">
              {t('control_tower.world_class_maturity.execution.count', {
                count: register.executionQueue.length,
              })}
            </div>
          </div>
          <div className="mt-3 grid gap-2 xl:grid-cols-2">
            {register.executionQueue.map((slice) => (
              <div
                key={slice.id}
                data-testid={`control-tower-world-class-execution-${slice.id}`}
                className="min-w-0 rounded-md border border-border/60 px-3 py-3"
              >
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="text-xs font-medium text-muted-foreground">
                      {t('control_tower.world_class_maturity.execution.priority', {
                        priority: slice.priority,
                      })}
                      {' · '}
                      {t(slice.categoryTitleKey)}
                    </div>
                    <div className="mt-1 text-sm font-semibold">{t(slice.titleKey)}</div>
                  </div>
                  <WorldClassMaturityStatusBadge status={slice.status} />
                </div>
                <div className="mt-3 grid gap-2 md:grid-cols-3">
                  <ReadinessEvidence
                    label={t('control_tower.world_class_maturity.execution.owner')}
                    value={t(slice.ownerKey)}
                  />
                  <ReadinessEvidence
                    label={t('control_tower.world_class_maturity.execution.acceptance')}
                    value={t(slice.acceptanceKey)}
                  />
                  <ReadinessEvidence
                    label={t('control_tower.world_class_maturity.execution.verification')}
                    value={t(slice.verificationKey)}
                  />
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="grid gap-4 xl:grid-cols-2">
          {register.categories.map((category) => (
            <div
              key={category.id}
              data-testid={`control-tower-world-class-category-${category.id}`}
              className="min-w-0 rounded-md border border-border/70 p-4"
            >
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-sm font-semibold">{t(category.titleKey)}</div>
                  <p className="mt-1 text-xs leading-5 text-muted-foreground">
                    {t(category.descriptionKey)}
                  </p>
                </div>
                <div className="shrink-0 rounded-sm border border-border/70 bg-muted/20 px-2 py-1 text-xs font-medium">
                  {t('control_tower.world_class_maturity.category_summary', {
                    covered: category.totals.covered,
                    gap: category.totals.gap,
                    partial: category.totals.partial,
                    total: category.totals.total,
                  })}
                </div>
              </div>
              <div className="mt-3 grid gap-2">
                {category.items.map((item) => (
                  <div
                    key={item.id}
                    data-testid={`control-tower-world-class-gap-${item.id}`}
                    className="flex min-w-0 flex-wrap items-start justify-between gap-2 rounded-md border border-border/60 px-3 py-2"
                  >
                    <span className="min-w-0 flex-1 break-words text-sm">{t(item.titleKey)}</span>
                    <WorldClassMaturityStatusBadge status={item.status} />
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>

        <div className="rounded-md border border-border/70 bg-muted/15 p-4">
          <div className="text-xs font-medium text-muted-foreground">
            {t('control_tower.world_class_maturity.next_action')}
          </div>
          <div className="mt-1 text-sm font-medium">
            {t(register.nextActionKey, register.nextActionValues)}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function WorldClassMaturityStatusBadge({ status }: { status: WorldClassMaturityGapStatus }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center rounded-full border px-2 py-0.5 text-xs font-medium',
        status === 'covered' &&
          'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60',
        status === 'partial' &&
          'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60',
        status === 'gap' && 'border-red-200 bg-red-50 text-red-700 dark:border-red-900/60',
      )}
    >
      {t(`control_tower.world_class_maturity.status.${status}`)}
    </span>
  )
}

function RecoveryCommandCenterCard({ command }: { command: RecoveryCommandCenter }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="control-tower-recovery-command"
      className="rounded-lg border-border/70 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle>{t('control_tower.recovery_command.title')}</CardTitle>
            <CardDescription>{t('control_tower.recovery_command.description')}</CardDescription>
          </div>
          <ProductReadinessStatusBadge status={command.status} />
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-5">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <RecoveryCommandMetric
            label={t('control_tower.recovery_command.metrics.sla')}
            value={
              command.available
                ? t('control_tower.recovery_command.metrics.sla_value', {
                    dueSoon: command.totals.dueSoon,
                    overdue: command.totals.overdue,
                  })
                : t('control_tower.proof.restricted')
            }
          />
          <RecoveryCommandMetric
            label={t('control_tower.recovery_command.metrics.ownership')}
            value={
              command.available
                ? t('control_tower.recovery_command.metrics.ownership_value', {
                    owners: command.totals.ownerCount,
                    unassigned: command.totals.unassigned,
                  })
                : t('control_tower.proof.restricted')
            }
          />
          <RecoveryCommandMetric
            label={t('control_tower.recovery_command.metrics.customer_contact')}
            value={
              command.available
                ? t('control_tower.recovery_command.metrics.customer_contact_value', {
                    pending: command.totals.pendingContact,
                  })
                : t('control_tower.proof.restricted')
            }
          />
          <RecoveryCommandMetric
            label={t('control_tower.recovery_command.metrics.evidence_debt')}
            value={
              command.available
                ? t('control_tower.recovery_command.metrics.evidence_debt_value', {
                    debt: command.totals.evidenceDebt,
                  })
                : t('control_tower.proof.restricted')
            }
          />
        </div>

        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_24rem]">
          <div className="rounded-md border border-border/70 p-4">
            <div className="text-sm font-semibold">
              {t('control_tower.recovery_command.blockers.title')}
            </div>
            <div className="mt-3 grid gap-2 sm:grid-cols-2">
              {command.blockers.length === 0 ? (
                <div className="flex items-start gap-2 text-sm text-muted-foreground sm:col-span-2">
                  <CheckCircle2 className="mt-0.5 size-4 text-emerald-600" />
                  {t('control_tower.recovery_command.blockers.empty')}
                </div>
              ) : (
                command.blockers.map((blocker) => (
                  <div
                    key={blocker.id}
                    data-testid={`control-tower-recovery-blocker-${blocker.id}`}
                    className="flex min-w-0 items-center justify-between gap-3 rounded-md border border-border/60 px-3 py-2"
                  >
                    <span className="truncate text-sm">{t(blocker.labelKey)}</span>
                    <span className="inline-flex shrink-0 items-center gap-2">
                      <ProductReadinessStatusBadge status={blocker.status} />
                      <span className="text-sm font-semibold tabular-nums">{blocker.count}</span>
                    </span>
                  </div>
                ))
              )}
            </div>
          </div>

          <div className="rounded-md border border-border/70 p-4">
            <div className="text-sm font-semibold">
              {t('control_tower.recovery_command.owner_load.title')}
            </div>
            <div className="mt-3 space-y-3">
              {command.ownerLoads.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  {t('control_tower.recovery_command.owner_load.empty')}
                </p>
              ) : (
                command.ownerLoads.slice(0, 3).map((owner) => (
                  <div key={owner.ownerMemberId} className="min-w-0">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="truncate text-sm font-medium">{owner.ownerMemberId}</div>
                      <span className="rounded-sm border border-border/70 bg-muted/30 px-2 py-0.5 text-xs">
                        {t('control_tower.recovery_command.owner_load.workload', {
                          score: owner.workload,
                        })}
                      </span>
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {t('control_tower.recovery_command.owner_load.detail', {
                        critical: owner.critical,
                        dueSoon: owner.dueSoon,
                        open: owner.open,
                        overdue: owner.overdue,
                        pending: owner.pendingContact,
                      })}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {owner.oldestDueAt
                        ? t('control_tower.recovery_command.owner_load.oldest_due', {
                            value: formatDateTime(owner.oldestDueAt),
                          })
                        : t('control_tower.recovery_command.owner_load.no_due')}
                    </p>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>

        <div className="rounded-md border border-border/70 bg-muted/15 p-4">
          <div className="text-xs font-medium text-muted-foreground">
            {t('control_tower.recovery_command.next_action')}
          </div>
          <div className="mt-1 text-sm font-medium">
            {t(command.nextActionKey, command.nextActionValues)}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function RecoveryCommandMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-md border border-border/70 p-3">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div className="mt-1 break-words text-sm font-semibold">{value}</div>
    </div>
  )
}

function ProductReadinessStatusBadge({ status }: { status: ProductReadinessStatus }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        status === 'pass' &&
          'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60',
        status === 'watch' &&
          'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60',
        status === 'blocked' && 'border-red-200 bg-red-50 text-red-700 dark:border-red-900/60',
        status === 'insufficient_data' && 'border-border bg-muted/30 text-muted-foreground',
      )}
    >
      <ProductReadinessStatusIcon status={status} />
      {t(`control_tower.readiness_matrix.status.${status}`)}
    </span>
  )
}

function ProductReadinessStatusIcon({ status }: { status: ProductReadinessStatus }) {
  const className = 'size-3'
  if (status === 'pass') return <CheckCircle2 className={className} />
  if (status === 'blocked') return <AlertTriangle className={className} />
  return <CircleGauge className={className} />
}

function ReadinessEvidence({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 break-words text-sm">{value}</div>
    </div>
  )
}

function ControlTowerError() {
  const { t } = useTranslation()
  return (
    <Card className="rounded-lg border-border/70 shadow-none">
      <CardContent className="flex items-start gap-3 p-5">
        <AlertTriangle className="mt-0.5 size-5 shrink-0 text-amber-600" />
        <div>
          <div className="font-medium">{t('control_tower.error.title')}</div>
          <p className="mt-1 text-sm text-muted-foreground">{t('control_tower.error.body')}</p>
        </div>
      </CardContent>
    </Card>
  )
}

function buildControlTowerModel(
  classification?: ClassificationQuality,
  search?: SearchQuality,
  qualityActions: QualityAction[] = [],
  survey?: SurveyAnalytics,
  canReadSurveyAnalytics = true,
  feedback?: FeedbackStats,
  inboundSources?: InboundSource[],
  canReadInboundSources = true,
) {
  const classificationSummary = classification?.summary
  const searchSummary = search?.summary
  const classificationEvents = toNumber(classificationSummary?.classificationEvents)
  const classificationWarnings = classification?.warnings.length ?? 0
  const lowConfidenceRate = toNumber(classificationSummary?.lowConfidenceRate)
  const offListRate = toNumber(classificationSummary?.offListRate)
  const searchQueries = toNumber(searchSummary?.queryCount)
  const zeroResultRate = toNumber(searchSummary?.zeroResultRate)
  const fallbackRate = toNumber(searchSummary?.fallbackRate)
  const searchCoverage = clampUnit(toNumber(search?.indexHealth?.coverageRatio, 1))
  const searchClickThrough = clampUnit(toNumber(searchSummary?.clickThroughRate))
  const p95LatencyMs = toNumber(searchSummary?.p95LatencyMs)
  const closedLoop = buildClosedLoopScorecard(survey, canReadSurveyAnalytics)
  const risks = compactRisks([
    classificationWarnings > 0 && {
      id: 'classification-warnings',
      actionKey: 'control_tower.classification_warnings',
      titleKey: 'control_tower.risk.classification_warnings.title',
      bodyKey: 'control_tower.risk.classification_warnings.body',
      recommendationKey: 'control_tower.action.classification_warnings',
      metric: String(classificationWarnings),
      severity: normalizeSeverity(classificationSummary?.worstSeverity, 'watch'),
      href: '/analytics/classification-quality',
    },
    lowConfidenceRate >= 0.05 && {
      id: 'low-confidence',
      actionKey: 'control_tower.low_confidence',
      titleKey: 'control_tower.risk.low_confidence.title',
      bodyKey: 'control_tower.risk.low_confidence.body',
      recommendationKey: 'control_tower.action.low_confidence',
      metric: formatRate(lowConfidenceRate),
      severity: lowConfidenceRate >= 0.1 ? 'alert' : 'watch',
      href: '/analytics/classification-quality',
    },
    closedLoop.available &&
      closedLoop.overdueReviews > 0 && {
        id: 'closed-loop-overdue',
        actionKey: 'control_tower.closed_loop_overdue',
        titleKey: 'control_tower.risk.closed_loop_overdue.title',
        bodyKey: 'control_tower.risk.closed_loop_overdue.body',
        recommendationKey: 'control_tower.action.closed_loop_overdue',
        metric: String(closedLoop.overdueReviews),
        severity: 'alert',
        href: '/integrations/surveys',
      },
    closedLoop.available &&
      closedLoop.openReviews > 0 &&
      closedLoop.readiness < 70 && {
        id: 'closed-loop-recovery',
        actionKey: 'control_tower.closed_loop_recovery',
        titleKey: 'control_tower.risk.closed_loop_recovery.title',
        bodyKey: 'control_tower.risk.closed_loop_recovery.body',
        recommendationKey: 'control_tower.action.closed_loop_recovery',
        metric: `${closedLoop.readiness}/100`,
        severity: closedLoop.readiness < 50 ? 'alert' : 'watch',
        href: '/integrations/surveys',
      },
    closedLoop.available &&
      closedLoop.invitations >= 5 &&
      closedLoop.responseRate < 0.1 && {
        id: 'closed-loop-response-rate',
        actionKey: 'control_tower.closed_loop_response_rate',
        titleKey: 'control_tower.risk.closed_loop_response_rate.title',
        bodyKey: 'control_tower.risk.closed_loop_response_rate.body',
        recommendationKey: 'control_tower.action.closed_loop_response_rate',
        metric: formatRate(closedLoop.responseRate),
        severity: 'watch',
        href: '/integrations/surveys',
      },
    offListRate >= 0.02 && {
      id: 'off-list',
      actionKey: 'control_tower.off_list',
      titleKey: 'control_tower.risk.off_list.title',
      bodyKey: 'control_tower.risk.off_list.body',
      recommendationKey: 'control_tower.action.off_list',
      metric: formatRate(offListRate),
      severity: offListRate >= 0.05 ? 'alert' : 'watch',
      href: '/analytics/classification-quality',
    },
    zeroResultRate >= 0.1 && {
      id: 'zero-result',
      actionKey: 'control_tower.zero_result',
      titleKey: 'control_tower.risk.zero_result.title',
      bodyKey: 'control_tower.risk.zero_result.body',
      recommendationKey: 'control_tower.action.zero_result',
      metric: formatRate(zeroResultRate),
      severity: zeroResultRate >= 0.2 ? 'alert' : 'watch',
      href: '/analytics/search-quality',
    },
    fallbackRate >= 0.1 && {
      id: 'fallback',
      actionKey: 'control_tower.fallback',
      titleKey: 'control_tower.risk.fallback.title',
      bodyKey: 'control_tower.risk.fallback.body',
      recommendationKey: 'control_tower.action.fallback',
      metric: formatRate(fallbackRate),
      severity: fallbackRate >= 0.25 ? 'alert' : 'watch',
      href: '/analytics/search-quality',
    },
    searchCoverage < 0.95 && {
      id: 'index-coverage',
      actionKey: 'control_tower.index_coverage',
      titleKey: 'control_tower.risk.index_coverage.title',
      bodyKey: 'control_tower.risk.index_coverage.body',
      recommendationKey: 'control_tower.action.index_coverage',
      metric: formatRate(searchCoverage),
      severity: searchCoverage < 0.8 ? 'alert' : 'watch',
      href: '/analytics/search-quality',
    },
    p95LatencyMs >= 2500 && {
      id: 'search-latency',
      actionKey: 'control_tower.search_latency',
      titleKey: 'control_tower.risk.search_latency.title',
      bodyKey: 'control_tower.risk.search_latency.body',
      recommendationKey: 'control_tower.action.search_latency',
      metric: formatLatency(p95LatencyMs),
      severity: p95LatencyMs >= 5000 ? 'alert' : 'watch',
      href: '/analytics/search-quality',
    },
  ]).slice(0, 7)
  const actionByKey = new Map(qualityActions.map((action) => [action.actionKey, action]))
  const risksWithActions = risks.map((risk) => ({
    ...risk,
    action: actionByKey.get(risk.actionKey),
  }))
  const firstValue = buildFirstValueScorecard({
    canReadInboundSources,
    classificationEvents,
    closedLoop,
    feedback,
    inboundSources,
    risks: risksWithActions,
    searchCoverage,
    searchQueries,
  })
  const sourceHealth = buildSourceHealthCommandCenter({
    canReadInboundSources,
    inboundSources,
  })
  const recoveryCommand = buildRecoveryCommandCenter(closedLoop)

  const lanes: ControlTowerLane[] = [
    {
      id: 'classification',
      titleKey: 'control_tower.lanes.classification.title',
      bodyKey: 'control_tower.lanes.classification.body',
      value: formatRate(1 - Math.max(lowConfidenceRate, offListRate)),
      severity: normalizeSeverity(classificationSummary?.worstSeverity, 'normal'),
    },
    {
      id: 'search',
      titleKey: 'control_tower.lanes.search.title',
      bodyKey: 'control_tower.lanes.search.body',
      value: searchQueries > 0 ? formatRate(1 - Math.max(zeroResultRate, fallbackRate)) : '0',
      severity: normalizeSeverity(searchSummary?.worstSeverity, 'normal'),
    },
    {
      id: 'index',
      titleKey: 'control_tower.lanes.index.title',
      bodyKey: 'control_tower.lanes.index.body',
      value: formatRate(searchCoverage),
      severity: searchCoverage < 0.8 ? 'alert' : searchCoverage < 0.95 ? 'watch' : 'normal',
    },
    {
      id: 'closed_loop',
      titleKey: 'control_tower.lanes.closed_loop.title',
      bodyKey: 'control_tower.lanes.closed_loop.body',
      value: closedLoop.available
        ? `${closedLoop.readiness}/100`
        : canReadSurveyAnalytics
          ? '0'
          : '—',
      severity: closedLoop.severity,
    },
  ]
  const readinessItems = buildProductReadinessItems({
    classificationEvents,
    classificationWarnings,
    closedLoop,
    fallbackRate,
    lowConfidenceRate,
    offListRate,
    p95LatencyMs,
    risks: risksWithActions,
    searchCoverage,
    searchQueries,
    zeroResultRate,
  })
  const releaseVerification = buildReleaseVerificationCommandCenter({
    firstValue,
    readinessItems,
    recoveryCommand,
    risks: risksWithActions,
    sourceHealth,
  })
  const worldClassMaturity = buildWorldClassMaturityRegister()

  return {
    classificationEvents,
    classificationWarnings,
    closedLoop,
    firstValue,
    lanes,
    overallSeverity: worstSeverity([
      ...lanes.map((lane) => lane.severity),
      ...risks.map((risk) => risk.severity),
      classificationEvents === 0 && searchQueries === 0 ? 'insufficient_data' : 'normal',
    ]),
    readinessItems,
    releaseVerification,
    recoveryCommand,
    rankingVersion: search?.rankingVersions[0]?.rankingVersion ?? '',
    risks: risksWithActions,
    searchClickThrough,
    searchCoverage,
    sourceHealth,
    topZeroResultQuery: search?.zeroResultQueries[0]?.queryPreview ?? '',
    worldClassMaturity,
  }
}

function LaneIcon({ lane }: { lane: string }) {
  const className = 'size-6 text-muted-foreground'
  if (lane === 'classification') return <ShieldCheck className={className} />
  if (lane === 'search') return <Search className={className} />
  if (lane === 'index') return <DatabaseZap className={className} />
  return <HeartPulse className={className} />
}

function SeverityBadge({ severity }: { severity: SignalSeverity }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center rounded-full border px-2 py-0.5 text-xs font-medium',
        severity === 'alert' && 'border-red-200 bg-red-50 text-red-700 dark:border-red-900/60',
        severity === 'watch' &&
          'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60',
        severity === 'normal' &&
          'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60',
        severity === 'insufficient_data' && 'border-border bg-muted/30 text-muted-foreground',
      )}
    >
      {t(`control_tower.severity.${severity}`)}
    </span>
  )
}

function SeverityIcon({ severity }: { severity: SignalSeverity }) {
  if (severity === 'alert') return <AlertTriangle className="mt-0.5 size-5 shrink-0 text-red-600" />
  if (severity === 'watch') return <CircleGauge className="mt-0.5 size-5 shrink-0 text-amber-600" />
  return <CheckCircle2 className="mt-0.5 size-5 shrink-0 text-emerald-600" />
}

function ActionStatusBadge({ status }: { status: string | undefined }) {
  const { t } = useTranslation()
  const normalized = status || 'open'
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-sm border px-1.5 py-0.5 text-xs font-medium',
        normalized === 'open' && 'border-border bg-muted/30 text-muted-foreground',
        normalized === 'acknowledged' &&
          'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60',
        normalized === 'resolved' &&
          'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60',
        normalized === 'dismissed' && 'border-border bg-muted/20 text-muted-foreground',
      )}
    >
      {normalized === 'resolved' ? (
        <CheckCircle2 className="size-3" />
      ) : (
        <RotateCcw className="size-3" />
      )}
      {t(`control_tower.actions.status.${normalized}`)}
    </span>
  )
}

function ProofRow({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
  return (
    <div className="flex items-start gap-3">
      <span className="mt-0.5 text-muted-foreground">{icon}</span>
      <div className="min-w-0">
        <div className="text-xs font-medium text-muted-foreground">{label}</div>
        <div className="mt-0.5 truncate text-sm font-medium">{value}</div>
      </div>
    </div>
  )
}

type ClosedLoopScorecard = {
  available: boolean
  criticalReviews: number
  invitations: number
  missingActionRecoveryQueue: number
  missingRootCauseRecoveryQueue: number
  oldestOpenLowScoreReviewDueAt: string
  openReviews: number
  overdueRecoveryQueue: number
  overdueReviews: number
  ownerLoads: RecoveryCommandOwnerLoad[]
  pendingContactRecoveryQueue: number
  pendingContactReviews: number
  readiness: number
  responseRate: number
  severity: SignalSeverity
  unassignedRecoveryQueue: number
  unassignedReviews: number
}

function buildFirstValueScorecard({
  canReadInboundSources,
  classificationEvents,
  closedLoop,
  feedback,
  inboundSources,
  risks,
  searchCoverage,
  searchQueries,
}: {
  canReadInboundSources: boolean
  classificationEvents: number
  closedLoop: ClosedLoopScorecard
  feedback?: FeedbackStats
  inboundSources?: InboundSource[]
  risks: Array<ControlTowerRisk & { action?: QualityAction }>
  searchCoverage: number
  searchQueries: number
}): FirstValueScorecard {
  const feedbackTotal = toNumber(feedback?.total)
  const activeSources = inboundSources?.filter((source) => source.enabled).length ?? 0
  const sourceErrors =
    inboundSources?.filter((source) => (source.lastError ?? '').trim()).length ?? 0
  const sourceStatus = firstValueSourceStatus(
    canReadInboundSources,
    inboundSources,
    activeSources,
    sourceErrors,
  )
  const signalStatus = feedbackTotal > 0 ? 'pass' : 'blocked'
  const insightStatus =
    feedbackTotal === 0
      ? 'blocked'
      : classificationEvents > 0
        ? riskBacklogStatus(risks)
        : 'blocked'
  const discoveryStatus =
    feedbackTotal === 0
      ? 'blocked'
      : searchCoverage < 0.8
        ? 'blocked'
        : searchQueries > 0
          ? 'pass'
          : 'watch'
  const closedLoopStatus = firstValueClosedLoopStatus(closedLoop)
  const items: FirstValueItem[] = [
    {
      id: 'source-connected',
      titleKey: 'control_tower.first_value.items.source_connected.title',
      evidenceKey: canReadInboundSources
        ? 'control_tower.first_value.items.source_connected.evidence'
        : 'control_tower.first_value.items.source_connected.restricted_evidence',
      evidenceValues: {
        active: activeSources,
        errors: sourceErrors,
        total: inboundSources?.length ?? 0,
      },
      nextKey: `control_tower.first_value.items.source_connected.next.${sourceStatus}`,
      status: sourceStatus,
    },
    {
      id: 'signal-captured',
      titleKey: 'control_tower.first_value.items.signal_captured.title',
      evidenceKey: 'control_tower.first_value.items.signal_captured.evidence',
      evidenceValues: {
        total: formatInt(feedbackTotal),
        urgent: formatInt(toNumber(feedback?.urgentCount)),
      },
      nextKey: `control_tower.first_value.items.signal_captured.next.${signalStatus}`,
      status: signalStatus,
    },
    {
      id: 'insight-generated',
      titleKey: 'control_tower.first_value.items.insight_generated.title',
      evidenceKey: 'control_tower.first_value.items.insight_generated.evidence',
      evidenceValues: {
        events: formatInt(classificationEvents),
        risks: risks.length,
      },
      nextKey: `control_tower.first_value.items.insight_generated.next.${insightStatus}`,
      status: insightStatus,
    },
    {
      id: 'discovery-ready',
      titleKey: 'control_tower.first_value.items.discovery_ready.title',
      evidenceKey: 'control_tower.first_value.items.discovery_ready.evidence',
      evidenceValues: {
        coverage: formatRate(searchCoverage),
        queries: formatInt(searchQueries),
      },
      nextKey: `control_tower.first_value.items.discovery_ready.next.${discoveryStatus}`,
      status: discoveryStatus,
    },
    {
      id: 'closed-loop-tested',
      titleKey: 'control_tower.first_value.items.closed_loop_tested.title',
      evidenceKey: closedLoop.available
        ? 'control_tower.first_value.items.closed_loop_tested.evidence'
        : 'control_tower.first_value.items.closed_loop_tested.restricted_evidence',
      evidenceValues: {
        invitations: formatInt(closedLoop.invitations),
        readiness: closedLoop.readiness,
      },
      nextKey: `control_tower.first_value.items.closed_loop_tested.next.${closedLoopStatus}`,
      status: closedLoopStatus,
    },
  ]

  return {
    items,
    passed: items.filter((item) => item.status === 'pass').length,
    total: items.length,
  }
}

function firstValueSourceStatus(
  canReadInboundSources: boolean,
  inboundSources: InboundSource[] | undefined,
  activeSources: number,
  sourceErrors: number,
): ProductReadinessStatus {
  if (!canReadInboundSources || !inboundSources) return 'insufficient_data'
  if (activeSources === 0) return 'blocked'
  if (sourceErrors > 0) return 'watch'
  return 'pass'
}

function riskBacklogStatus(
  risks: Array<ControlTowerRisk & { action?: QualityAction }>,
): ProductReadinessStatus {
  if (
    risks.some(
      (risk) => risk.severity === 'alert' && (!risk.action || risk.action.status === 'open'),
    )
  ) {
    return 'blocked'
  }
  if (risks.length > 0) return 'watch'
  return 'pass'
}

function firstValueClosedLoopStatus(closedLoop: ClosedLoopScorecard): ProductReadinessStatus {
  if (!closedLoop.available) return 'insufficient_data'
  if (closedLoop.overdueReviews > 0 || closedLoop.readiness < 50) return 'blocked'
  if (closedLoop.invitations === 0 && closedLoop.openReviews === 0) return 'watch'
  if (closedLoop.readiness < 70) return 'watch'
  return 'pass'
}

function buildSourceHealthCommandCenter({
  canReadInboundSources,
  inboundSources,
  now = new Date(),
}: {
  canReadInboundSources: boolean
  inboundSources?: InboundSource[]
  now?: Date
}): SourceHealthCommandCenter {
  if (!canReadInboundSources || !inboundSources) {
    return {
      available: false,
      nextActionKey: 'control_tower.source_health.next.insufficient_data',
      nextActionValues: {},
      problems: [],
      sources: [],
      status: 'insufficient_data',
      totals: {
        active: 0,
        disabled: 0,
        errors: 0,
        fresh: 0,
        neverSeen: 0,
        stale: 0,
        total: 0,
      },
    }
  }

  const sources = inboundSources.map((source) => ({
    channel: source.channel,
    id: source.id,
    lastError: (source.lastError ?? '').trim(),
    lastEventAt: source.lastEventAt ?? '',
    name: source.name || source.slug || source.id,
    status: sourceHealthStatus(source, now),
  }))
  const total = inboundSources.length
  const active = inboundSources.filter((source) => source.enabled).length
  const disabled = total - active
  const errors = sources.filter((source) => source.lastError).length
  const neverSeen = inboundSources.filter((source) => source.enabled && !source.lastEventAt).length
  const stale = inboundSources.filter(
    (source) =>
      source.enabled &&
      Boolean(source.lastEventAt) &&
      !sourceIsFresh(source.lastEventAt ?? '', now),
  ).length
  const fresh = inboundSources.filter(
    (source) => source.enabled && sourceIsFresh(source.lastEventAt ?? '', now),
  ).length
  const status = sourceHealthCommandStatus({
    active,
    disabled,
    errors,
    neverSeen,
    stale,
    total,
  })
  const problems = sourceHealthProblems({
    active,
    disabled,
    errors,
    neverSeen,
    stale,
    total,
  })
  const sortedSources = [...sources].sort(
    (a, b) => sourceStatusRank(a.status) - sourceStatusRank(b.status),
  )

  return {
    available: true,
    nextActionKey: sourceHealthNextActionKey({
      active,
      disabled,
      errors,
      neverSeen,
      stale,
      total,
    }),
    nextActionValues: {
      active,
      disabled,
      errors,
      neverSeen,
      stale,
      total,
    },
    problems,
    sources: sortedSources,
    status,
    totals: {
      active,
      disabled,
      errors,
      fresh,
      neverSeen,
      stale,
      total,
    },
  }
}

function sourceHealthStatus(source: InboundSource, now: Date): ProductReadinessStatus {
  if (!source.enabled) return 'watch'
  if ((source.lastError ?? '').trim()) return 'blocked'
  if (!source.lastEventAt) return 'blocked'
  if (!sourceIsFresh(source.lastEventAt, now)) return 'watch'
  return 'pass'
}

function sourceIsFresh(value: string, now: Date) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return false
  const age = Math.max(0, now.getTime() - date.getTime())
  return age <= sourceFreshnessWindowMs
}

function sourceHealthCommandStatus({
  active,
  disabled,
  errors,
  neverSeen,
  stale,
  total,
}: {
  active: number
  disabled: number
  errors: number
  neverSeen: number
  stale: number
  total: number
}): ProductReadinessStatus {
  if (total === 0 || active === 0 || errors > 0 || neverSeen > 0) return 'blocked'
  if (stale > 0 || disabled > 0) return 'watch'
  return 'pass'
}

function sourceHealthProblems({
  active,
  disabled,
  errors,
  neverSeen,
  stale,
  total,
}: {
  active: number
  disabled: number
  errors: number
  neverSeen: number
  stale: number
  total: number
}): SourceHealthProblem[] {
  return [
    total === 0 && {
      id: 'no-sources',
      count: 1,
      labelKey: 'control_tower.source_health.problems.no_sources',
      status: 'blocked' as const,
    },
    total > 0 &&
      active === 0 && {
        id: 'no-active',
        count: total,
        labelKey: 'control_tower.source_health.problems.no_active',
        status: 'blocked' as const,
      },
    errors > 0 && {
      id: 'errors',
      count: errors,
      labelKey: 'control_tower.source_health.problems.errors',
      status: 'blocked' as const,
    },
    neverSeen > 0 && {
      id: 'never-seen',
      count: neverSeen,
      labelKey: 'control_tower.source_health.problems.never_seen',
      status: 'blocked' as const,
    },
    stale > 0 && {
      id: 'stale',
      count: stale,
      labelKey: 'control_tower.source_health.problems.stale',
      status: 'watch' as const,
    },
    disabled > 0 && {
      id: 'disabled',
      count: disabled,
      labelKey: 'control_tower.source_health.problems.disabled',
      status: 'watch' as const,
    },
  ].filter(Boolean) as SourceHealthProblem[]
}

function sourceHealthNextActionKey({
  active,
  disabled,
  errors,
  neverSeen,
  stale,
  total,
}: {
  active: number
  disabled: number
  errors: number
  neverSeen: number
  stale: number
  total: number
}) {
  if (total === 0) return 'control_tower.source_health.next.connect_source'
  if (active === 0) return 'control_tower.source_health.next.resume_source'
  if (errors > 0) return 'control_tower.source_health.next.fix_errors'
  if (neverSeen > 0) return 'control_tower.source_health.next.send_test_event'
  if (stale > 0) return 'control_tower.source_health.next.refresh_stale'
  if (disabled > 0) return 'control_tower.source_health.next.review_disabled'
  return 'control_tower.source_health.next.monitor'
}

function sourceStatusRank(status: ProductReadinessStatus) {
  if (status === 'blocked') return 0
  if (status === 'watch') return 1
  if (status === 'insufficient_data') return 2
  return 3
}

function buildRecoveryCommandCenter(closedLoop: ClosedLoopScorecard): RecoveryCommandCenter {
  if (!closedLoop.available) {
    return {
      available: false,
      blockers: [],
      nextActionKey: 'control_tower.recovery_command.next.insufficient_data',
      nextActionValues: {},
      ownerLoads: [],
      status: 'insufficient_data',
      totals: {
        dueSoon: 0,
        evidenceDebt: 0,
        ownerCount: 0,
        overdue: 0,
        pendingContact: 0,
        unassigned: 0,
      },
    }
  }

  const dueSoon = closedLoop.ownerLoads.reduce((sum, owner) => sum + owner.dueSoon, 0)
  const overdue = Math.max(closedLoop.overdueRecoveryQueue, closedLoop.overdueReviews)
  const unassigned = Math.max(closedLoop.unassignedRecoveryQueue, closedLoop.unassignedReviews)
  const pendingContact = Math.max(
    closedLoop.pendingContactRecoveryQueue,
    closedLoop.pendingContactReviews,
  )
  const evidenceDebt =
    closedLoop.missingRootCauseRecoveryQueue + closedLoop.missingActionRecoveryQueue
  const totals = {
    dueSoon,
    evidenceDebt,
    ownerCount: closedLoop.ownerLoads.length,
    overdue,
    pendingContact,
    unassigned,
  }
  const status = recoveryCommandStatus({
    available: closedLoop.available,
    evidenceDebt,
    invitations: closedLoop.invitations,
    openReviews: closedLoop.openReviews,
    overdue,
    pendingContact,
    unassigned,
  })
  const blockers = recoveryCommandBlockers({
    missingAction: closedLoop.missingActionRecoveryQueue,
    missingRootCause: closedLoop.missingRootCauseRecoveryQueue,
    overdue,
    pendingContact,
    unassigned,
  })
  const ownerLoads = [...closedLoop.ownerLoads].sort((a, b) => b.workload - a.workload)

  return {
    available: true,
    blockers,
    nextActionKey: recoveryCommandNextActionKey({
      evidenceDebt,
      invitations: closedLoop.invitations,
      openReviews: closedLoop.openReviews,
      overdue,
      pendingContact,
      unassigned,
    }),
    nextActionValues: {
      debt: evidenceDebt,
      open: closedLoop.openReviews,
      overdue,
      pending: pendingContact,
      unassigned,
    },
    ownerLoads,
    status,
    totals,
  }
}

function recoveryCommandStatus({
  available,
  evidenceDebt,
  invitations,
  openReviews,
  overdue,
  pendingContact,
  unassigned,
}: {
  available: boolean
  evidenceDebt: number
  invitations: number
  openReviews: number
  overdue: number
  pendingContact: number
  unassigned: number
}): ProductReadinessStatus {
  if (!available) return 'insufficient_data'
  if (overdue > 0 || unassigned > 0) return 'blocked'
  if (pendingContact > 0 || evidenceDebt > 0 || openReviews > 0 || invitations === 0) return 'watch'
  return 'pass'
}

function recoveryCommandBlockers({
  missingAction,
  missingRootCause,
  overdue,
  pendingContact,
  unassigned,
}: {
  missingAction: number
  missingRootCause: number
  overdue: number
  pendingContact: number
  unassigned: number
}): RecoveryCommandBlocker[] {
  return [
    overdue > 0 && {
      id: 'overdue',
      count: overdue,
      labelKey: 'control_tower.recovery_command.blockers.overdue',
      status: 'blocked' as const,
    },
    unassigned > 0 && {
      id: 'unassigned',
      count: unassigned,
      labelKey: 'control_tower.recovery_command.blockers.unassigned',
      status: 'blocked' as const,
    },
    pendingContact > 0 && {
      id: 'pending-contact',
      count: pendingContact,
      labelKey: 'control_tower.recovery_command.blockers.pending_contact',
      status: 'watch' as const,
    },
    missingRootCause > 0 && {
      id: 'missing-root-cause',
      count: missingRootCause,
      labelKey: 'control_tower.recovery_command.blockers.missing_root_cause',
      status: 'watch' as const,
    },
    missingAction > 0 && {
      id: 'missing-action',
      count: missingAction,
      labelKey: 'control_tower.recovery_command.blockers.missing_action',
      status: 'watch' as const,
    },
  ].filter(Boolean) as RecoveryCommandBlocker[]
}

function recoveryCommandNextActionKey({
  evidenceDebt,
  invitations,
  openReviews,
  overdue,
  pendingContact,
  unassigned,
}: {
  evidenceDebt: number
  invitations: number
  openReviews: number
  overdue: number
  pendingContact: number
  unassigned: number
}) {
  if (overdue > 0) return 'control_tower.recovery_command.next.resolve_overdue'
  if (unassigned > 0) return 'control_tower.recovery_command.next.assign_owners'
  if (pendingContact > 0) return 'control_tower.recovery_command.next.contact_customers'
  if (evidenceDebt > 0) return 'control_tower.recovery_command.next.document_evidence'
  if (openReviews > 0) return 'control_tower.recovery_command.next.progress_reviews'
  if (invitations === 0) return 'control_tower.recovery_command.next.run_test'
  return 'control_tower.recovery_command.next.monitor'
}

function buildProductReadinessItems({
  classificationEvents,
  classificationWarnings,
  closedLoop,
  fallbackRate,
  lowConfidenceRate,
  offListRate,
  p95LatencyMs,
  risks,
  searchCoverage,
  searchQueries,
  zeroResultRate,
}: {
  classificationEvents: number
  classificationWarnings: number
  closedLoop: ClosedLoopScorecard
  fallbackRate: number
  lowConfidenceRate: number
  offListRate: number
  p95LatencyMs: number
  risks: Array<ControlTowerRisk & { action?: QualityAction }>
  searchCoverage: number
  searchQueries: number
  zeroResultRate: number
}): ProductReadinessItem[] {
  const signalStatus = signalQualityReadinessStatus(
    classificationEvents,
    classificationWarnings,
    lowConfidenceRate,
    offListRate,
  )
  const semanticStatus = semanticDiscoveryReadinessStatus(
    searchQueries,
    searchCoverage,
    zeroResultRate,
    fallbackRate,
    p95LatencyMs,
  )
  const closedLoopStatus = closedLoopReadinessStatus(closedLoop)
  const actionStatus = actionAccountabilityReadinessStatus(risks)
  const startedActions = risks.filter((risk) => risk.action?.status === 'acknowledged').length
  const verifiedActions = risks.filter((risk) => risk.action?.status === 'resolved').length
  const openActions = risks.filter((risk) => !risk.action || risk.action.status === 'open').length

  return [
    {
      id: 'signal-quality',
      titleKey: 'control_tower.readiness_matrix.items.signal_quality.title',
      standardKey: 'control_tower.readiness_matrix.items.signal_quality.standard',
      evidenceKey: 'control_tower.readiness_matrix.items.signal_quality.evidence',
      evidenceValues: {
        events: formatInt(classificationEvents),
        health: formatRate(1 - Math.max(lowConfidenceRate, offListRate)),
        warnings: classificationWarnings,
      },
      gapKey: `control_tower.readiness_matrix.items.signal_quality.gap.${signalStatus}`,
      status: signalStatus,
    },
    {
      id: 'semantic-discovery',
      titleKey: 'control_tower.readiness_matrix.items.semantic_discovery.title',
      standardKey: 'control_tower.readiness_matrix.items.semantic_discovery.standard',
      evidenceKey: 'control_tower.readiness_matrix.items.semantic_discovery.evidence',
      evidenceValues: {
        coverage: formatRate(searchCoverage),
        fallback: formatRate(fallbackRate),
        latency: formatLatency(p95LatencyMs),
        zero: formatRate(zeroResultRate),
      },
      gapKey: `control_tower.readiness_matrix.items.semantic_discovery.gap.${semanticStatus}`,
      status: semanticStatus,
    },
    {
      id: 'closed-loop',
      titleKey: 'control_tower.readiness_matrix.items.closed_loop.title',
      standardKey: 'control_tower.readiness_matrix.items.closed_loop.standard',
      evidenceKey: closedLoop.available
        ? 'control_tower.readiness_matrix.items.closed_loop.evidence'
        : 'control_tower.readiness_matrix.items.closed_loop.restricted_evidence',
      evidenceValues: {
        overdue: closedLoop.overdueReviews,
        readiness: closedLoop.readiness,
        responseRate: formatRate(closedLoop.responseRate),
      },
      gapKey: `control_tower.readiness_matrix.items.closed_loop.gap.${closedLoopStatus}`,
      status: closedLoopStatus,
    },
    {
      id: 'action-accountability',
      titleKey: 'control_tower.readiness_matrix.items.action_accountability.title',
      standardKey: 'control_tower.readiness_matrix.items.action_accountability.standard',
      evidenceKey: 'control_tower.readiness_matrix.items.action_accountability.evidence',
      evidenceValues: {
        active: risks.length,
        open: openActions,
        started: startedActions,
        verified: verifiedActions,
      },
      gapKey: `control_tower.readiness_matrix.items.action_accountability.gap.${actionStatus}`,
      status: actionStatus,
    },
    {
      id: 'release-verification',
      titleKey: 'control_tower.readiness_matrix.items.release_verification.title',
      standardKey: 'control_tower.readiness_matrix.items.release_verification.standard',
      evidenceKey: 'control_tower.readiness_matrix.items.release_verification.evidence',
      evidenceValues: {},
      gapKey: 'control_tower.readiness_matrix.items.release_verification.gap.pass',
      status: 'pass',
    },
  ]
}

function buildReleaseVerificationCommandCenter({
  firstValue,
  readinessItems,
  recoveryCommand,
  risks,
  sourceHealth,
}: {
  firstValue: FirstValueScorecard
  readinessItems: ProductReadinessItem[]
  recoveryCommand: RecoveryCommandCenter
  risks: Array<ControlTowerRisk & { action?: QualityAction }>
  sourceHealth: SourceHealthCommandCenter
}): ReleaseVerificationCommandCenter {
  const evidence = releaseVerificationEvidenceItems()
  const runtimeStatuses: ProductReadinessStatus[] = [
    firstValueReleaseStatus(firstValue),
    sourceHealth.status,
    recoveryCommand.status,
    ...readinessItems.map((item) => item.status),
  ]
  const blocked = runtimeStatuses.filter(releaseVerificationStatusBlocksRelease).length
  const watch = runtimeStatuses.filter((status) => status === 'watch').length
  const unresolvedRisks = risks.filter((risk) => {
    const status = risk.action?.status ?? 'open'
    return status === 'open' || status === 'acknowledged'
  }).length
  const evidencePassed = evidence.filter((item) => item.status === 'pass').length
  const status = releaseVerificationCommandStatus({
    blocked,
    unresolvedRisks,
    watch,
  })

  return {
    evidence,
    nextActionKey: releaseVerificationNextActionKey({
      blocked,
      unresolvedRisks,
      watch,
    }),
    nextActionValues: {
      blocked,
      risks: unresolvedRisks,
      watch,
    },
    status,
    totals: {
      blocked,
      evidencePassed,
      evidenceTotal: evidence.length,
      unresolvedRisks,
      watch,
    },
  }
}

function releaseVerificationEvidenceItems(): ReleaseVerificationEvidence[] {
  return [
    {
      id: 'proposal',
      titleKey: 'control_tower.release_verification.evidence.proposal.title',
      evidenceKey: 'control_tower.release_verification.evidence.proposal.body',
      status: 'pass',
    },
    {
      id: 'product-contract',
      titleKey: 'control_tower.release_verification.evidence.product_contract.title',
      evidenceKey: 'control_tower.release_verification.evidence.product_contract.body',
      status: 'pass',
    },
    {
      id: 'unit',
      titleKey: 'control_tower.release_verification.evidence.unit.title',
      evidenceKey: 'control_tower.release_verification.evidence.unit.body',
      status: 'pass',
    },
    {
      id: 'browser',
      titleKey: 'control_tower.release_verification.evidence.browser.title',
      evidenceKey: 'control_tower.release_verification.evidence.browser.body',
      status: 'pass',
    },
    {
      id: 'bundle',
      titleKey: 'control_tower.release_verification.evidence.bundle.title',
      evidenceKey: 'control_tower.release_verification.evidence.bundle.body',
      status: 'pass',
    },
    {
      id: 'release-smoke',
      titleKey: 'control_tower.release_verification.evidence.release_smoke.title',
      evidenceKey: 'control_tower.release_verification.evidence.release_smoke.body',
      status: 'pass',
    },
  ]
}

function firstValueReleaseStatus(firstValue: FirstValueScorecard): ProductReadinessStatus {
  if (firstValue.total === 0) return 'insufficient_data'
  if (firstValue.passed === firstValue.total) return 'pass'
  if (firstValue.passed === 0) return 'blocked'
  return 'watch'
}

function releaseVerificationStatusBlocksRelease(status: ProductReadinessStatus) {
  return status === 'blocked' || status === 'insufficient_data'
}

function releaseVerificationCommandStatus({
  blocked,
  unresolvedRisks,
  watch,
}: {
  blocked: number
  unresolvedRisks: number
  watch: number
}): ProductReadinessStatus {
  if (blocked > 0) return 'blocked'
  if (watch > 0 || unresolvedRisks > 0) return 'watch'
  return 'pass'
}

function releaseVerificationNextActionKey({
  blocked,
  unresolvedRisks,
  watch,
}: {
  blocked: number
  unresolvedRisks: number
  watch: number
}) {
  if (blocked > 0) return 'control_tower.release_verification.next.clear_blockers'
  if (watch > 0 || unresolvedRisks > 0) {
    return 'control_tower.release_verification.next.close_watch'
  }
  return 'control_tower.release_verification.next.attach_ci'
}

const worldClassMaturityDefinitions: WorldClassMaturityCategoryDefinition[] = [
  {
    id: 'capture',
    items: [
      maturityGap('capture_source_health', 'covered'),
      maturityGap('capture_multi_channel_setup', 'partial'),
      maturityGap('capture_import_preview', 'gap'),
      maturityGap('capture_source_slo', 'partial'),
      maturityGap('capture_source_weighting', 'gap'),
      maturityGap('capture_event_replay', 'gap'),
      maturityGap('capture_noise_filtering', 'partial'),
      maturityGap('capture_identity_join', 'gap'),
      maturityGap('capture_schema_versions', 'gap'),
      maturityGap('capture_first_value_wizard', 'partial'),
    ],
  },
  {
    id: 'identity',
    items: [
      maturityGap('identity_user_graph', 'covered'),
      maturityGap('identity_account_model', 'covered'),
      maturityGap('identity_commercial_context', 'gap'),
      maturityGap('identity_contact_roles', 'gap'),
      maturityGap('identity_account_hierarchy', 'gap'),
      maturityGap('identity_consent_retention', 'partial'),
      maturityGap('identity_customer_timeline', 'gap'),
      maturityGap('identity_impacted_accounts', 'partial'),
      maturityGap('identity_high_value_escalation', 'gap'),
      maturityGap('identity_context_freshness', 'gap'),
    ],
  },
  {
    id: 'evidence',
    items: [
      maturityGap('evidence_citations', 'partial'),
      maturityGap('evidence_quote_workbench', 'gap'),
      maturityGap('evidence_quality_score', 'covered'),
      maturityGap('evidence_contrary_signal', 'gap'),
      maturityGap('evidence_bundle_export', 'gap'),
      maturityGap('evidence_research_assets', 'gap'),
      maturityGap('evidence_redaction_preview', 'partial'),
      maturityGap('evidence_insight_lifecycle', 'gap'),
      maturityGap('evidence_ai_review_state', 'gap'),
      maturityGap('evidence_explainability', 'partial'),
    ],
  },
  {
    id: 'requests',
    items: [
      maturityGap('request_problem_solution_split', 'partial'),
      maturityGap('request_merge_split', 'gap'),
      maturityGap('request_scoring_model', 'partial'),
      maturityGap('request_configurable_formula', 'gap'),
      maturityGap('request_scenario_planning', 'gap'),
      maturityGap('request_stakeholder_views', 'gap'),
      maturityGap('request_saved_views', 'gap'),
      maturityGap('request_decision_record', 'covered'),
      maturityGap('request_private_public_roadmap', 'partial'),
      maturityGap('request_release_linkage', 'gap'),
    ],
  },
  {
    id: 'closed_loop',
    items: [
      maturityGap('closed_loop_supporter_detection', 'partial'),
      maturityGap('closed_loop_status_templates', 'partial'),
      maturityGap('closed_loop_changelog_comms', 'gap'),
      maturityGap('closed_loop_preferences', 'partial'),
      maturityGap('closed_loop_failed_notification_queue', 'partial'),
      maturityGap('closed_loop_survey_surface', 'covered'),
      maturityGap('closed_loop_low_score_cases', 'covered'),
      maturityGap('closed_loop_reopen_demand', 'gap'),
      maturityGap('closed_loop_notified_evidence', 'covered'),
      maturityGap('closed_loop_public_trust_trail', 'gap'),
    ],
  },
  {
    id: 'operator',
    items: [
      maturityGap('operator_triage_queue', 'partial'),
      maturityGap('operator_owner_deadline_sla', 'partial'),
      maturityGap('operator_batch_actions', 'covered'),
      maturityGap('operator_command_palette', 'gap'),
      maturityGap('operator_collaboration', 'gap'),
      maturityGap('operator_activity_timeline', 'partial'),
      maturityGap('operator_weekly_artifact', 'gap'),
      maturityGap('operator_exception_queues', 'partial'),
      maturityGap('operator_dashboard_drilldown', 'partial'),
      maturityGap('operator_empty_state_path', 'partial'),
    ],
  },
  {
    id: 'ai',
    items: [
      maturityGap('ai_eval_harness_productized', 'partial'),
      maturityGap('ai_prompt_version_audit', 'partial'),
      maturityGap('ai_cited_confidence', 'partial'),
      maturityGap('ai_no_evidence_guardrail', 'partial'),
      maturityGap('ai_accept_dismiss_learning', 'covered'),
      maturityGap('ai_cost_latency_dashboard', 'partial'),
      maturityGap('ai_model_drift_detection', 'gap'),
      maturityGap('ai_tenant_policy', 'partial'),
      maturityGap('ai_sensitive_redaction_policy', 'partial'),
      maturityGap('ai_action_approval', 'gap'),
    ],
  },
  {
    id: 'reliability',
    items: [
      maturityGap('reliability_pipeline_slo', 'covered'),
      maturityGap('reliability_error_budget', 'covered'),
      maturityGap('reliability_end_to_end_trace', 'covered'),
      maturityGap('reliability_backfill_replay', 'covered'),
      maturityGap('reliability_backup_restore_drill', 'covered'),
      maturityGap('reliability_tenant_quota_dashboard', 'covered'),
      maturityGap('reliability_incident_timeline', 'covered'),
      maturityGap('reliability_release_health', 'covered'),
      maturityGap('reliability_consistency_checks', 'covered'),
      maturityGap('reliability_quality_gate_history', 'covered'),
    ],
  },
  {
    id: 'governance',
    items: [
      maturityGap('governance_sso_scim_rbac', 'covered'),
      maturityGap('governance_field_level_permissions', 'covered'),
      maturityGap('governance_public_privacy_preflight', 'covered'),
      maturityGap('governance_audit_export', 'covered'),
      maturityGap('governance_retention_legal_hold', 'covered'),
      maturityGap('governance_compliance_package', 'covered'),
      maturityGap('governance_key_rotation_ui', 'covered'),
      maturityGap('governance_webhook_signature_tooling', 'covered'),
      maturityGap('governance_data_request_workflow', 'covered'),
      maturityGap('governance_security_runbook', 'covered'),
    ],
  },
  {
    id: 'developer',
    items: [
      maturityGap('developer_openapi_sdk_examples', 'covered'),
      maturityGap('developer_sdk_parity', 'covered'),
      maturityGap('developer_connector_sdk', 'covered'),
      maturityGap('developer_field_mapping_ui', 'covered'),
      maturityGap('developer_api_consistency', 'covered'),
      maturityGap('developer_import_export_ui', 'covered'),
      maturityGap('developer_integration_catalog', 'covered'),
      maturityGap('developer_upgrade_diagnostics', 'covered'),
      maturityGap('developer_demo_workspace', 'covered'),
      maturityGap('developer_north_star_metrics', 'partial'),
    ],
  },
]

const worldClassMaturityExecutionDefinitions: WorldClassMaturityExecutionDefinition[] = [
  { gapId: 'identity_user_graph', id: 'identity-graph-foundation', priority: 1 },
  { gapId: 'identity_account_model', id: 'account-model-foundation', priority: 2 },
  { gapId: 'evidence_quality_score', id: 'evidence-quality-score', priority: 3 },
  { gapId: 'request_decision_record', id: 'decision-record', priority: 4 },
  { gapId: 'closed_loop_notified_evidence', id: 'notified-customer-evidence', priority: 5 },
  { gapId: 'operator_batch_actions', id: 'operator-batch-actions', priority: 6 },
  { gapId: 'ai_accept_dismiss_learning', id: 'ai-review-feedback-loop', priority: 7 },
  { gapId: 'reliability_end_to_end_trace', id: 'end-to-end-signal-trace', priority: 8 },
  { gapId: 'reliability_error_budget', id: 'error-budget-burn-ledger', priority: 9 },
  { gapId: 'reliability_release_health', id: 'release-health-correlation', priority: 10 },
  { gapId: 'reliability_incident_timeline', id: 'incident-timeline-reconstruction', priority: 11 },
  {
    gapId: 'reliability_tenant_quota_dashboard',
    id: 'tenant-quota-saturation-dashboard',
    priority: 12,
  },
  {
    gapId: 'reliability_backup_restore_drill',
    id: 'backup-restore-drill-evidence',
    priority: 13,
  },
  {
    gapId: 'reliability_consistency_checks',
    id: 'consistency-checks-audit',
    priority: 14,
  },
  {
    gapId: 'reliability_pipeline_slo',
    id: 'pipeline-slo-ledger',
    priority: 15,
  },
  {
    gapId: 'governance_sso_scim_rbac',
    id: 'governance-rbac-readiness',
    priority: 16,
  },
  {
    gapId: 'governance_field_level_permissions',
    id: 'field-level-permissions-ledger',
    priority: 17,
  },
  {
    gapId: 'governance_public_privacy_preflight',
    id: 'public-privacy-preflight',
    priority: 18,
  },
  {
    gapId: 'governance_retention_legal_hold',
    id: 'retention-legal-hold-workflow',
    priority: 19,
  },
  {
    gapId: 'governance_compliance_package',
    id: 'compliance-package-evidence',
    priority: 20,
  },
  {
    gapId: 'governance_key_rotation_ui',
    id: 'key-rotation-readiness',
    priority: 21,
  },
  {
    gapId: 'governance_webhook_signature_tooling',
    id: 'webhook-signature-tooling',
    priority: 22,
  },
  {
    gapId: 'governance_security_runbook',
    id: 'security-incident-runbook',
    priority: 23,
  },
  {
    gapId: 'developer_openapi_sdk_examples',
    id: 'developer-api-adoption-kit',
    priority: 24,
  },
  {
    gapId: 'developer_sdk_parity',
    id: 'developer-sdk-parity-gate',
    priority: 25,
  },
  {
    gapId: 'developer_connector_sdk',
    id: 'developer-connector-sdk-conformance',
    priority: 26,
  },
  {
    gapId: 'developer_field_mapping_ui',
    id: 'developer-field-mapping-workbench',
    priority: 27,
  },
  {
    gapId: 'developer_api_consistency',
    id: 'developer-api-consistency-contract',
    priority: 28,
  },
  {
    gapId: 'developer_import_export_ui',
    id: 'developer-import-export-workbench',
    priority: 29,
  },
  {
    gapId: 'developer_integration_catalog',
    id: 'developer-integration-catalog',
    priority: 30,
  },
  {
    gapId: 'developer_upgrade_diagnostics',
    id: 'developer-upgrade-diagnostics',
    priority: 31,
  },
  {
    gapId: 'developer_north_star_metrics',
    id: 'developer-north-star-metrics',
    priority: 32,
  },
]

function maturityGap(
  id: string,
  status: WorldClassMaturityGapStatus,
): WorldClassMaturityGapDefinition {
  return { id, status }
}

function buildWorldClassMaturityRegister(): WorldClassMaturityRegister {
  const categoryByGapId = new Map<string, WorldClassMaturityCategoryDefinition>()
  const categories = worldClassMaturityDefinitions.map((category) => {
    category.items.forEach((item) => {
      categoryByGapId.set(item.id, category)
    })
    const items = category.items.map((item) => ({
      id: item.id,
      status: item.status,
      titleKey: `control_tower.world_class_maturity.items.${item.id}`,
    }))
    return {
      descriptionKey: `control_tower.world_class_maturity.categories.${category.id}.description`,
      id: category.id,
      items,
      titleKey: `control_tower.world_class_maturity.categories.${category.id}.title`,
      totals: worldClassMaturityTotals(items),
    }
  })
  const totals = worldClassMaturityTotals(categories.flatMap((category) => category.items))
  const itemById = new Map(
    categories.flatMap((category) => category.items.map((item) => [item.id, item] as const)),
  )
  const executionQueue = worldClassMaturityExecutionDefinitions
    .map((definition) => {
      const item = itemById.get(definition.gapId)
      const category = categoryByGapId.get(definition.gapId)
      if (!item || !category) return null
      if (item.status === 'covered') return null
      return {
        acceptanceKey: `control_tower.world_class_maturity.execution.items.${definition.id}.acceptance`,
        categoryTitleKey: `control_tower.world_class_maturity.categories.${category.id}.title`,
        gapId: definition.gapId,
        id: definition.id,
        ownerKey: `control_tower.world_class_maturity.execution.items.${definition.id}.owner`,
        priority: definition.priority,
        status: item.status,
        titleKey: `control_tower.world_class_maturity.items.${definition.gapId}`,
        verificationKey: `control_tower.world_class_maturity.execution.items.${definition.id}.verification`,
      }
    })
    .filter(Boolean) as WorldClassMaturityExecutionSlice[]
  executionQueue.sort((a, b) => a.priority - b.priority)

  return {
    categories,
    executionQueue,
    nextActionKey: 'control_tower.world_class_maturity.next.close_gaps',
    nextActionValues: totals,
    totals,
  }
}

function worldClassMaturityTotals(items: Array<{ status: WorldClassMaturityGapStatus }>) {
  return {
    covered: items.filter((item) => item.status === 'covered').length,
    gap: items.filter((item) => item.status === 'gap').length,
    partial: items.filter((item) => item.status === 'partial').length,
    total: items.length,
  }
}

function signalQualityReadinessStatus(
  classificationEvents: number,
  classificationWarnings: number,
  lowConfidenceRate: number,
  offListRate: number,
): ProductReadinessStatus {
  if (classificationEvents === 0) return 'insufficient_data'
  if (classificationWarnings > 0 || lowConfidenceRate >= 0.1 || offListRate >= 0.05) {
    return 'blocked'
  }
  if (lowConfidenceRate >= 0.05 || offListRate >= 0.02) return 'watch'
  return 'pass'
}

function semanticDiscoveryReadinessStatus(
  searchQueries: number,
  searchCoverage: number,
  zeroResultRate: number,
  fallbackRate: number,
  p95LatencyMs: number,
): ProductReadinessStatus {
  if (searchQueries === 0) return 'insufficient_data'
  if (
    searchCoverage < 0.8 ||
    zeroResultRate >= 0.2 ||
    fallbackRate >= 0.25 ||
    p95LatencyMs >= 5000
  ) {
    return 'blocked'
  }
  if (
    searchCoverage < 0.95 ||
    zeroResultRate >= 0.1 ||
    fallbackRate >= 0.1 ||
    p95LatencyMs >= 2500
  ) {
    return 'watch'
  }
  return 'pass'
}

function closedLoopReadinessStatus(closedLoop: ClosedLoopScorecard): ProductReadinessStatus {
  if (!closedLoop.available) return 'insufficient_data'
  if (closedLoop.overdueReviews > 0 || closedLoop.readiness < 50) return 'blocked'
  if (closedLoop.readiness < 70 || (closedLoop.invitations >= 5 && closedLoop.responseRate < 0.1)) {
    return 'watch'
  }
  return 'pass'
}

function actionAccountabilityReadinessStatus(
  risks: Array<ControlTowerRisk & { action?: QualityAction }>,
): ProductReadinessStatus {
  if (risks.length === 0) return 'pass'
  if (risks.some((risk) => !risk.action || risk.action.status === 'open')) return 'blocked'
  return 'watch'
}

function buildClosedLoopScorecard(
  survey: SurveyAnalytics | undefined,
  canReadSurveyAnalytics: boolean,
): ClosedLoopScorecard {
  if (!canReadSurveyAnalytics || !survey) {
    return {
      available: false,
      criticalReviews: 0,
      invitations: 0,
      missingActionRecoveryQueue: 0,
      missingRootCauseRecoveryQueue: 0,
      oldestOpenLowScoreReviewDueAt: '',
      openReviews: 0,
      overdueRecoveryQueue: 0,
      overdueReviews: 0,
      ownerLoads: [],
      pendingContactRecoveryQueue: 0,
      pendingContactReviews: 0,
      readiness: 0,
      responseRate: 0,
      severity: 'insufficient_data',
      unassignedRecoveryQueue: 0,
      unassignedReviews: 0,
    }
  }
  const readiness = recoveryReadinessScore(survey)
  const responseRate = clampUnit(toNumber(survey.responseRate))
  const invitations = toNumber(survey.invitationCount)
  const openReviews = toNumber(survey.openLowScoreReviewCount)
  const overdueReviews = toNumber(survey.overdueLowScoreReviewCount)
  const unassignedReviews = toNumber(survey.unassignedLowScoreReviewCount)
  const criticalReviews = toNumber(survey.criticalLowScoreReviewCount)
  const pendingContactReviews = toNumber(survey.pendingCustomerContactReviewCount)
  const severity =
    overdueReviews > 0 || readiness < 50
      ? 'alert'
      : readiness < 70 || (invitations >= 5 && responseRate < 0.1)
        ? 'watch'
        : 'normal'
  return {
    available: true,
    criticalReviews,
    invitations,
    missingActionRecoveryQueue: toNumber(survey.missingActionRecoveryQueueCount),
    missingRootCauseRecoveryQueue: toNumber(survey.missingRootCauseRecoveryQueueCount),
    oldestOpenLowScoreReviewDueAt: survey.oldestOpenLowScoreReviewDueAt ?? '',
    openReviews,
    overdueRecoveryQueue: toNumber(survey.overdueRecoveryQueueCount),
    overdueReviews,
    ownerLoads: (survey.ownerRecoveryLoads ?? []).map((owner) => ({
      critical: toNumber(owner.criticalCount),
      dueSoon: toNumber(owner.dueSoonCount),
      oldestDueAt: owner.oldestOpenDueAt ?? '',
      open: toNumber(owner.openCount),
      overdue: toNumber(owner.overdueCount),
      ownerMemberId: owner.ownerMemberId,
      pendingContact: toNumber(owner.pendingContactCount),
      workload: toNumber(owner.workloadScore),
    })),
    pendingContactRecoveryQueue: toNumber(survey.pendingContactRecoveryQueueCount),
    pendingContactReviews,
    readiness,
    responseRate,
    severity,
    unassignedRecoveryQueue: toNumber(survey.unassignedRecoveryQueueCount),
    unassignedReviews,
  }
}

function closedLoopProofValue(
  closedLoop: ClosedLoopScorecard,
  t: (key: string, opts?: Record<string, unknown>) => string,
) {
  if (!closedLoop.available) {
    return t('control_tower.proof.restricted')
  }
  return t('control_tower.proof.closed_loop_value', {
    open: closedLoop.openReviews,
    overdue: closedLoop.overdueReviews,
    rate: formatRate(closedLoop.responseRate),
  })
}

function roleCanReadSurveyAnalytics(role: Role | undefined) {
  return role === 'admin' || role === 'delegated_admin'
}

function roleCanReadInboundSources(role: Role | undefined) {
  return role === 'admin' || role === 'delegated_admin' || role === 'member'
}

function compactRisks(items: Array<ControlTowerRisk | false>): ControlTowerRisk[] {
  return items.filter(Boolean) as ControlTowerRisk[]
}

function normalizeSeverity(value: string | undefined, fallback: SignalSeverity): SignalSeverity {
  if (
    value === 'alert' ||
    value === 'watch' ||
    value === 'normal' ||
    value === 'insufficient_data'
  ) {
    return value
  }
  return fallback
}

function worstSeverity(severities: SignalSeverity[]): SignalSeverity {
  if (severities.includes('alert')) return 'alert'
  if (severities.includes('watch')) return 'watch'
  if (severities.includes('insufficient_data')) return 'insufficient_data'
  return 'normal'
}

function metricTone(severity: SignalSeverity) {
  if (severity === 'alert') return 'urgent'
  if (severity === 'watch' || severity === 'insufficient_data') return 'active'
  return 'default'
}

function toNumber(value: number | string | undefined, fallback = 0) {
  if (typeof value === 'number') return Number.isFinite(value) ? value : fallback
  if (typeof value === 'string') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : fallback
  }
  return fallback
}

function clampUnit(value: number) {
  if (!Number.isFinite(value)) return 0
  if (value < 0) return 0
  if (value > 1) return 1
  return value
}

function formatInt(value: number) {
  return new Intl.NumberFormat('zh-CN').format(Math.round(value))
}

function formatRate(value: number) {
  return new Intl.NumberFormat('zh-CN', {
    maximumFractionDigits: 1,
    style: 'percent',
  }).format(clampUnit(value))
}

function formatLatency(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(1)}s`
  return `${Math.round(value)}ms`
}

function formatDateTime(value: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    month: '2-digit',
    year: 'numeric',
  }).format(date)
}

export const controlTowerPageTestables = {
  actionAccountabilityReadinessStatus,
  buildReleaseVerificationCommandCenter,
  buildRecoveryCommandCenter,
  buildSourceHealthCommandCenter,
  buildWorldClassMaturityRegister,
  clampUnit,
  closedLoopReadinessStatus,
  formatDateTime,
  firstValueReleaseStatus,
  firstValueClosedLoopStatus,
  firstValueSourceStatus,
  formatLatency,
  metricTone,
  normalizeSeverity,
  recoveryReadinessScore,
  releaseVerificationCommandStatus,
  releaseVerificationNextActionKey,
  releaseVerificationStatusBlocksRelease,
  recoveryCommandNextActionKey,
  recoveryCommandStatus,
  riskBacklogStatus,
  roleCanReadInboundSources,
  roleCanReadSurveyAnalytics,
  semanticDiscoveryReadinessStatus,
  signalQualityReadinessStatus,
  sourceHealthCommandStatus,
  sourceHealthNextActionKey,
  sourceHealthStatus,
  sourceIsFresh,
  toNumber,
  worldClassMaturityExecutionDefinitions,
  worldClassMaturityTotals,
  worstSeverity,
}
