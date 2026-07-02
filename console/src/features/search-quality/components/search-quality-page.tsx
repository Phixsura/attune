import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import {
  Activity,
  AlertTriangle,
  DatabaseZap,
  GitBranch,
  Loader2,
  MousePointerClick,
  Search,
  Zap,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  defaultSearchQualityFilters,
  type SearchQuality,
  type SearchQualityBucket,
  type SearchQualityFilters,
  type SearchQualityRange,
  searchQualityBucketForRange,
  searchQualityQuery,
} from '@/features/search-quality/api/get-search-quality'
import type {
  SearchFallbackBreakdown,
  SearchIndexHealth,
  SearchQualityQuery,
  SearchQualityTimeBucket,
  SearchRankingVersion,
} from '@/proto/attune/v1/search'

type SearchQualityUiFilters = Required<
  Pick<SearchQualityFilters, 'range' | 'bucketWidth' | 'limit'>
> &
  SearchQualityFilters

export function SearchQualityPage() {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<SearchQualityUiFilters>(defaultSearchQualityFilters)
  const quality = useQuery(searchQualityQuery(filters))
  const summary = quality.data?.summary

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.analytics')}
        title={t('nav.search_quality')}
        subtitle={t('search_quality.subtitle')}
        actions={<SearchQualityControls filters={filters} onChange={setFilters} />}
        metrics={
          <>
            <PageHeroMetric
              label={t('search_quality.summary.queries')}
              value={formatInt(summary?.queryCount)}
              hint={t('search_quality.summary.queries_hint')}
            />
            <PageHeroMetric
              label={t('search_quality.summary.zero_result')}
              value={formatRate(summary?.zeroResultRate)}
              tone={rateTone(summary?.zeroResultRate, 0.2, 0.1)}
            />
            <PageHeroMetric
              label={t('search_quality.summary.fallback')}
              value={formatRate(summary?.fallbackRate)}
              tone={rateTone(summary?.fallbackRate, 0.25, 0.1)}
            />
            <PageHeroMetric
              label={t('search_quality.summary.p95')}
              value={formatLatency(summary?.p95LatencyMs)}
              tone={latencyTone(summary?.p95LatencyMs)}
            />
          </>
        }
      />

      {quality.isPending ? (
        <div className="flex items-center justify-center py-12 text-muted-foreground">
          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          {t('app.loading')}
        </div>
      ) : quality.data ? (
        <SearchQualityBody data={quality.data} />
      ) : null}
    </div>
  )
}

function SearchQualityControls({
  filters,
  onChange,
}: {
  filters: SearchQualityUiFilters
  onChange: (filters: SearchQualityUiFilters) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-wrap items-center justify-end gap-2">
      <Select
        value={filters.range}
        onValueChange={(range) => {
          const nextRange = range as SearchQualityRange
          onChange({
            ...filters,
            range: nextRange,
            bucketWidth: searchQualityBucketForRange(nextRange, filters.bucketWidth),
          })
        }}
      >
        <SelectTrigger className="w-28 bg-background/90">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="7d">{t('search_quality.range.7d')}</SelectItem>
          <SelectItem value="30d">{t('search_quality.range.30d')}</SelectItem>
          <SelectItem value="90d">{t('search_quality.range.90d')}</SelectItem>
        </SelectContent>
      </Select>
      <Select
        value={filters.bucketWidth}
        onValueChange={(bucketWidth) =>
          onChange({
            ...filters,
            bucketWidth: searchQualityBucketForRange(
              filters.range,
              bucketWidth as SearchQualityBucket,
            ),
          })
        }
      >
        <SelectTrigger className="w-28 bg-background/90">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="day">{t('search_quality.bucket.day')}</SelectItem>
          <SelectItem value="hour" disabled={filters.range !== '7d'}>
            {t('search_quality.bucket.hour')}
          </SelectItem>
        </SelectContent>
      </Select>
      <Select
        value={String(filters.limit)}
        onValueChange={(limit) => onChange({ ...filters, limit: Number(limit) })}
      >
        <SelectTrigger className="w-28 bg-background/90">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="10">{t('search_quality.limit.10')}</SelectItem>
          <SelectItem value="25">{t('search_quality.limit.25')}</SelectItem>
          <SelectItem value="50">{t('search_quality.limit.50')}</SelectItem>
        </SelectContent>
      </Select>
    </div>
  )
}

function SearchQualityBody({ data }: { data: SearchQuality }) {
  const { t } = useTranslation()
  const period = t('search_quality.period', {
    start: formatDate(data.currentFrom),
    end: formatDate(data.currentTo),
  })
  return (
    <div className="space-y-5">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
        <SearchTrend series={data.series} period={period} />
        <IndexHealthCard health={data.indexHealth} />
      </div>
      <div className="grid gap-4 xl:grid-cols-3">
        <SignalCard
          icon={<MousePointerClick className="h-4 w-4" />}
          title={t('search_quality.signals.ctr')}
          value={formatRate(data.summary?.clickThroughRate)}
          description={t('search_quality.signals.ctr_body', {
            count: toNumber(data.summary?.clickCount),
          })}
        />
        <SignalCard
          icon={<Search className="h-4 w-4" />}
          title={t('search_quality.signals.avg_results')}
          value={formatDecimal(data.summary?.averageResultCount)}
          description={t('search_quality.signals.avg_results_body')}
        />
        <SignalCard
          icon={<AlertTriangle className="h-4 w-4" />}
          title={t('search_quality.signals.severity')}
          value={severityLabel(data.summary?.worstSeverity, t)}
          description={t('search_quality.signals.severity_body')}
          tone={data.summary?.worstSeverity === 'alert' ? 'urgent' : 'default'}
        />
      </div>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <QueryTable
          title={t('search_quality.queries.title')}
          description={t('search_quality.queries.description')}
          queries={data.queries}
        />
        <FallbackBreakdownCard rows={data.fallbackBreakdown} />
      </div>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <QueryTable
          title={t('search_quality.zero_queries.title')}
          description={t('search_quality.zero_queries.description')}
          queries={data.zeroResultQueries}
          zeroFocus
        />
        <RankingVersionsCard rows={data.rankingVersions} />
      </div>
    </div>
  )
}

function SearchTrend({ series, period }: { series: SearchQualityTimeBucket[]; period: string }) {
  const { t } = useTranslation()
  const maxQueries = Math.max(...series.map((bucket) => toNumber(bucket.queryCount)), 0)
  return (
    <Card className="overflow-hidden">
      <CardHeader>
        <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle>{t('search_quality.trend.title')}</CardTitle>
            <CardDescription>{period}</CardDescription>
          </div>
          <div className="flex items-center gap-2 rounded-full border border-border/65 bg-muted/30 px-3 py-1 text-xs text-muted-foreground">
            <Activity className="h-3.5 w-3.5" />
            {t('search_quality.trend.legend')}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {series.length === 0 ? (
          <EmptyState
            title={t('search_quality.empty.title')}
            description={t('search_quality.empty.body')}
          />
        ) : (
          <div className="flex h-48 items-end gap-2">
            {series.map((bucket) => {
              const queries = toNumber(bucket.queryCount)
              const zeroHeight = Math.max(bucket.zeroResultRate * 100, queries > 0 ? 3 : 0)
              const fallbackHeight = Math.max(bucket.fallbackRate * 100, queries > 0 ? 3 : 0)
              const volumeHeight = maxQueries > 0 ? Math.max((queries / maxQueries) * 100, 4) : 0
              return (
                <div
                  key={bucket.bucket}
                  className="flex min-w-0 flex-1 flex-col items-center gap-2"
                >
                  <div
                    className="flex h-36 w-full max-w-12 items-end justify-center gap-1 rounded-t-md border border-border/45 bg-muted/20 px-1"
                    title={`${formatDate(bucket.bucket)} · ${formatInt(bucket.queryCount)}`}
                  >
                    <div
                      className="w-2 rounded-t bg-zinc-700"
                      style={{ height: `${volumeHeight}%` }}
                    />
                    <div
                      className="w-2 rounded-t bg-amber-500"
                      style={{ height: `${zeroHeight}%` }}
                    />
                    <div
                      className="w-2 rounded-t bg-sky-500"
                      style={{ height: `${fallbackHeight}%` }}
                    />
                  </div>
                  <span className="max-w-full truncate text-[10px] text-muted-foreground">
                    {formatShortDate(bucket.bucket)}
                  </span>
                </div>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function SignalCard({
  icon,
  title,
  value,
  description,
  tone = 'default',
}: {
  icon: ReactNode
  title: string
  value: string
  description: string
  tone?: 'default' | 'urgent'
}) {
  return (
    <Card className="overflow-hidden">
      <CardContent className="flex items-start gap-3 p-5">
        <div
          className={
            tone === 'urgent'
              ? 'rounded-lg bg-rose-50 p-2 text-rose-700'
              : 'rounded-lg bg-muted p-2 text-foreground'
          }
        >
          {icon}
        </div>
        <div className="min-w-0">
          <div className="text-sm font-medium text-muted-foreground">{title}</div>
          <div className="mt-1 text-2xl font-semibold tracking-tight">{value}</div>
          <p className="mt-1.5 text-sm leading-6 text-muted-foreground">{description}</p>
        </div>
      </CardContent>
    </Card>
  )
}

function IndexHealthCard({ health }: { health?: SearchIndexHealth }) {
  const { t } = useTranslation()
  const coverage = health?.coverageRatio ?? 0
  return (
    <Card className="overflow-hidden">
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle>{t('search_quality.index.title')}</CardTitle>
            <CardDescription>{t('search_quality.index.description')}</CardDescription>
          </div>
          <DatabaseZap className="h-5 w-5 text-muted-foreground" />
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div>
          <div className="mb-2 flex items-center justify-between text-sm">
            <span className="text-muted-foreground">{t('search_quality.index.coverage')}</span>
            <span className="font-semibold">{formatRate(coverage)}</span>
          </div>
          <div className="h-2 rounded-full bg-muted">
            <div
              className="h-2 rounded-full bg-zinc-800"
              style={{ width: `${Math.max(0, Math.min(coverage * 100, 100))}%` }}
            />
          </div>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <IndexFact
            label={t('search_quality.index.searchable')}
            value={formatInt(health?.totalWithEmbeddings)}
          />
          <IndexFact
            label={t('search_quality.index.total')}
            value={formatInt(health?.totalLiveFeedback)}
          />
          <IndexFact
            label={t('search_quality.index.missing')}
            value={formatInt(health?.missingFeedbackCount)}
          />
          <IndexFact
            label={t('search_quality.index.model')}
            value={health?.embeddingModel || t('search_quality.index.no_model')}
          />
        </div>
        {health?.oldestMissingFeedbackAt ? (
          <p className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">
            {t('search_quality.index.oldest_missing', {
              date: formatDateTime(health.oldestMissingFeedbackAt),
            })}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}

function IndexFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border/65 bg-muted/20 px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 truncate text-sm font-semibold">{value}</div>
    </div>
  )
}

function QueryTable({
  title,
  description,
  queries,
  zeroFocus = false,
}: {
  title: string
  description: string
  queries: SearchQualityQuery[]
  zeroFocus?: boolean
}) {
  const { t } = useTranslation()
  return (
    <Card className="overflow-hidden">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        {queries.length === 0 ? (
          <EmptyState
            title={t('search_quality.queries.empty_title')}
            description={t('search_quality.queries.empty_body')}
          />
        ) : (
          queries.map((query) => (
            <div
              key={`${query.queryHash}:${query.lastSeenAt}`}
              className="grid gap-3 rounded-lg border border-border/65 px-3 py-3 lg:grid-cols-[minmax(0,1fr)_12rem]"
            >
              <div className="min-w-0">
                <div className="truncate text-sm font-semibold">
                  {query.queryPreview || query.queryHash.slice(0, 12)}
                </div>
                <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <span>{formatDateTime(query.lastSeenAt)}</span>
                  <span>
                    {t('search_quality.queries.hash', { hash: query.queryHash.slice(0, 8) })}
                  </span>
                </div>
              </div>
              <div className="grid grid-cols-3 gap-2 text-right text-xs">
                <MiniStat
                  label={t('search_quality.queries.count')}
                  value={formatInt(query.queryCount)}
                />
                <MiniStat
                  label={
                    zeroFocus ? t('search_quality.queries.zero') : t('search_quality.queries.ctr')
                  }
                  value={
                    zeroFocus
                      ? formatRate(query.zeroResultRate)
                      : formatRate(query.clickThroughRate)
                  }
                />
                <MiniStat
                  label={t('search_quality.queries.p95')}
                  value={formatLatency(query.p95LatencyMs)}
                />
              </div>
            </div>
          ))
        )}
      </CardContent>
    </Card>
  )
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="truncate text-muted-foreground">{label}</div>
      <div className="mt-1 truncate font-semibold text-foreground">{value}</div>
    </div>
  )
}

function FallbackBreakdownCard({ rows }: { rows: SearchFallbackBreakdown[] }) {
  const { t } = useTranslation()
  return (
    <Card className="overflow-hidden">
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle>{t('search_quality.fallback.title')}</CardTitle>
            <CardDescription>{t('search_quality.fallback.description')}</CardDescription>
          </div>
          <Zap className="h-5 w-5 text-muted-foreground" />
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {rows.length === 0 ? (
          <EmptyState
            title={t('search_quality.fallback.empty_title')}
            description={t('search_quality.fallback.empty_body')}
          />
        ) : (
          rows.map((row) => (
            <div key={row.reason} className="space-y-1.5">
              <div className="flex items-center justify-between gap-3 text-sm">
                <span className="truncate font-medium">{fallbackReasonLabel(row.reason, t)}</span>
                <span className="text-muted-foreground">{formatInt(row.count)}</span>
              </div>
              <div className="h-2 rounded-full bg-muted">
                <div
                  className="h-2 rounded-full bg-sky-600"
                  style={{ width: `${Math.max(2, Math.min(row.share * 100, 100))}%` }}
                />
              </div>
            </div>
          ))
        )}
      </CardContent>
    </Card>
  )
}

function RankingVersionsCard({ rows }: { rows: SearchRankingVersion[] }) {
  const { t } = useTranslation()
  return (
    <Card className="overflow-hidden">
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle>{t('search_quality.ranking.title')}</CardTitle>
            <CardDescription>{t('search_quality.ranking.description')}</CardDescription>
          </div>
          <GitBranch className="h-5 w-5 text-muted-foreground" />
        </div>
      </CardHeader>
      <CardContent className="space-y-2">
        {rows.map((row) => (
          <div key={row.rankingVersion} className="rounded-lg border border-border/65 px-3 py-3">
            <div className="flex items-center justify-between gap-3">
              <div className="min-w-0 truncate text-sm font-semibold">{row.rankingVersion}</div>
              <span className="rounded-full border border-border/65 px-2 py-0.5 text-xs text-muted-foreground">
                {rankingStatusLabel(row.status, t)}
              </span>
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <span>{t('search_quality.ranking.traffic', { value: row.trafficPercent })}</span>
              {row.updatedAt ? <span>{formatDateTime(row.updatedAt)}</span> : null}
            </div>
            {row.notes ? (
              <p className="mt-2 text-sm leading-6 text-muted-foreground">{row.notes}</p>
            ) : null}
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function formatInt(value: string | number | undefined) {
  const n = toNumber(value)
  return new Intl.NumberFormat('zh-CN').format(n)
}

function formatDecimal(value: number | undefined) {
  if (value == null || !Number.isFinite(value)) return '0.0'
  return value.toFixed(1)
}

function formatRate(value: number | undefined) {
  const n = typeof value === 'number' && Number.isFinite(value) ? value : 0
  return `${Math.round(n * 100)}%`
}

function formatLatency(value: string | number | undefined) {
  return `${formatInt(value)} ms`
}

function formatDate(value: string) {
  return format(new Date(value), 'yyyy-MM-dd', { locale: zhCN })
}

function formatShortDate(value: string) {
  return format(new Date(value), 'MM-dd', { locale: zhCN })
}

function formatDateTime(value: string) {
  return format(new Date(value), 'MM-dd HH:mm', { locale: zhCN })
}

function rateTone(value: number | undefined, alert: number, watch: number) {
  const n = typeof value === 'number' ? value : 0
  if (n >= alert) return 'urgent'
  if (n >= watch) return 'active'
  return 'default'
}

function latencyTone(value: string | number | undefined) {
  const ms = toNumber(value)
  if (ms >= 3000) return 'urgent'
  if (ms >= 1200) return 'active'
  return 'default'
}

function toNumber(value: string | number | undefined) {
  if (typeof value === 'number') return Number.isFinite(value) ? value : 0
  if (!value) return 0
  const n = Number(value)
  return Number.isFinite(n) ? n : 0
}

function severityLabel(severity: string | undefined, t: (key: string) => string) {
  return t(`search_quality.severity.${severity || 'normal'}`)
}

function rankingStatusLabel(status: string, t: (key: string) => string) {
  return t(`search_quality.ranking.status.${status || 'active'}`)
}

function fallbackReasonLabel(reason: string, t: (key: string) => string) {
  return t(`search_quality.fallback.reason.${reason || 'unknown'}`)
}
