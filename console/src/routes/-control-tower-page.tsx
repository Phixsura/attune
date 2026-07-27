import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  CircleGauge,
  ClipboardCheck,
  DatabaseZap,
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
import { api } from '@/lib/api-client'
import { cn } from '@/lib/utils'

type SignalSeverity = 'alert' | 'watch' | 'normal' | 'insufficient_data'

type ControlTowerRisk = {
  id: string
  actionKey: string
  titleKey: string
  bodyKey: string
  recommendationKey: string
  metric: string
  severity: SignalSeverity
  href: '/analytics/classification-quality' | '/analytics/search-quality'
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
      cohortCount: number
      totalActiveMembers: number
    }>('/fb/v1/console/cohort-sync/health').catch(() => ({
      sourceCount: 0,
      activeSources: 0,
      errorSources: 0,
      cohortCount: 0,
      totalActiveMembers: 0,
    })),
}

export const controlTowerQueries = [
  classificationQualityQuery(defaultClassificationQualityFilters),
  searchQualityQuery(defaultSearchQualityFilters),
  qualityActionsQuery({ status: 'all', limit: 100 }),
  cohortSyncHealthQuery,
] as const

export function ControlTowerPage() {
  const { t } = useTranslation()
  const classification = useQuery(controlTowerQueries[0])
  const search = useQuery(controlTowerQueries[1])
  const qualityActions = useQuery(controlTowerQueries[2])
  const cohortHealth = useQuery(controlTowerQueries[3])

  const model = useMemo(
    () => buildControlTowerModel(classification.data, search.data, qualityActions.data),
    [classification.data, search.data, qualityActions.data],
  )
  const pending = classification.isPending || search.isPending
  const failed = classification.isError || search.isError

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
                label="Cohort Sync"
                value={`${cohortHealth.data.activeSources}/${cohortHealth.data.sourceCount}`}
                tone={cohortHealth.data.errorSources > 0 ? 'urgent' : 'default'}
              />
            )}
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
      <div className="grid gap-4 lg:grid-cols-3">
        {model.lanes.map((lane) => (
          <Card key={lane.id} className="rounded-lg border-border/70 shadow-none">
            <CardHeader className="pb-3">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <CardTitle className="text-base">{t(lane.titleKey)}</CardTitle>
                  <CardDescription>{t(lane.bodyKey)}</CardDescription>
                </div>
                <SeverityBadge severity={lane.severity} />
              </div>
            </CardHeader>
            <CardContent>
              <div className="flex items-center gap-2 text-2xl font-semibold tracking-tight">
                <LaneIcon lane={lane.id} />
                <span>{lane.value}</span>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
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
                <div key={risk.id} className="flex flex-col gap-3 p-5 sm:flex-row sm:items-start">
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
          </CardContent>
        </Card>
      </div>
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
  ]).slice(0, 5)
  const actionByKey = new Map(qualityActions.map((action) => [action.actionKey, action]))
  const risksWithActions = risks.map((risk) => ({
    ...risk,
    action: actionByKey.get(risk.actionKey),
  }))

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
  ]

  return {
    classificationEvents,
    classificationWarnings,
    lanes,
    overallSeverity: worstSeverity([
      ...lanes.map((lane) => lane.severity),
      ...risks.map((risk) => risk.severity),
      classificationEvents === 0 && searchQueries === 0 ? 'insufficient_data' : 'normal',
    ]),
    rankingVersion: search?.rankingVersions[0]?.rankingVersion ?? '',
    risks: risksWithActions,
    searchClickThrough,
    searchCoverage,
    topZeroResultQuery: search?.zeroResultQueries[0]?.queryPreview ?? '',
  }
}

function LaneIcon({ lane }: { lane: string }) {
  const className = 'size-6 text-muted-foreground'
  if (lane === 'classification') return <ShieldCheck className={className} />
  if (lane === 'search') return <Search className={className} />
  return <DatabaseZap className={className} />
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

export const controlTowerPageTestables = {
  clampUnit,
  formatLatency,
  metricTone,
  normalizeSeverity,
  toNumber,
  worstSeverity,
}
