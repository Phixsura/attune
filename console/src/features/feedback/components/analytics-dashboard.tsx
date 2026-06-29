import { queryOptions, useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { AlertTriangle, BarChart3, Loader2, SmilePlus, TrendingUp } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { feedbackStatsQuery } from '@/features/feedback/api/get-feedback-stats'
import { DimStatsBars } from '@/features/feedback/components/dim-stats-bars'
import { SentimentChart } from '@/features/feedback/components/sentiment-chart'
import { api } from '@/lib/api-client'
import type { GetEnrichConfigResponse } from '@/proto/attune/v1/enrich_config'
import type { GetUsageResponse } from '@/proto/attune/v1/usage'

export function AnalyticsDashboard() {
  const { t } = useTranslation()
  const stats = useQuery(feedbackStatsQuery())
  const config = useQuery(enrichConfigQuery())
  const usage = useQuery(usageQuery())

  const dimStats = stats.data?.dims ?? []
  const sentimentData = useMemo(() => {
    const sentimentDim = dimStats.find((d) => d.dim === 'sentiment')
    if (!sentimentDim) return []
    return sentimentDim.top.map((v) => ({ value: v.value, count: Number(v.count) }))
  }, [dimStats])

  if (stats.isPending || config.isPending || usage.isPending) {
    return (
      <div className="flex items-center justify-center py-12 text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        {t('app.loading')}
      </div>
    )
  }

  const total = Number(stats.data?.total ?? 0)
  const urgentCount = Number(stats.data?.urgentCount ?? 0)
  const dims = config.data?.dimensions ?? []
  const periodStart = stats.data?.periodStart ?? ''
  const periodEnd = stats.data?.periodEnd ?? ''
  const urgentPct = total > 0 ? Math.round((urgentCount / total) * 100) : 0

  const usageSeries = (usage.data?.series ?? []).map((b) => ({
    bucket: b.bucket,
    value: Number(b.value),
  }))
  const activeDays = usageSeries.filter((e) => e.value > 0).length

  const periodLabel =
    periodStart && periodEnd
      ? t('usage.period', {
          start: format(new Date(periodStart), 'yyyy-MM-dd', { locale: zhCN }),
          end: format(new Date(periodEnd), 'yyyy-MM-dd', { locale: zhCN }),
        })
      : ''

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.analytics')}
        title={t('dashboard.title')}
        subtitle={t('dashboard.subtitle')}
        metrics={
          <>
            <PageHeroMetric
              label={t('feedback.stats.total')}
              value={String(total)}
              hint={periodLabel}
            />
            <PageHeroMetric
              label={t('feedback.stats.urgent')}
              value={String(urgentCount)}
              hint={
                urgentCount > 0
                  ? t('feedback.stats.urgent_hint', { pct: urgentPct })
                  : t('feedback.stats.urgent_empty')
              }
              tone={urgentCount > 0 ? 'urgent' : 'default'}
            />
            <PageHeroMetric
              label={t('usage.summary.active_days')}
              value={String(activeDays)}
              hint={t('usage.summary.active_days_hint')}
            />
          </>
        }
      />

      {total === 0 ? (
        <EmptyState
          icon={BarChart3}
          title={t('dashboard.empty_title')}
          description={t('dashboard.empty_body')}
          className="py-16"
        />
      ) : (
        <>
          <div className="grid gap-6 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,0.85fr)]">
            <Card className="border-border/70 shadow-none">
              <CardHeader className="border-b border-border/60 bg-muted/15">
                <CardTitle className="flex items-center gap-2">
                  <AlertTriangle className="h-4 w-4" />
                  {t('dashboard.dim_overview')}
                </CardTitle>
                <CardDescription>{t('dashboard.dim_overview_hint')}</CardDescription>
              </CardHeader>
              <CardContent className="pt-6">
                <DimStatsBars
                  dims={dims}
                  stats={dimStats}
                  total={total}
                  urgentCount={urgentCount}
                />
              </CardContent>
            </Card>

            <Card className="border-border/70 shadow-none">
              <CardHeader className="border-b border-border/60 bg-muted/15">
                <CardTitle className="flex items-center gap-2">
                  <TrendingUp className="h-4 w-4" />
                  {t('dashboard.trend_card')}
                </CardTitle>
                <CardDescription>{periodLabel}</CardDescription>
              </CardHeader>
              <CardContent className="pt-6">
                {usageSeries.length > 0 ? (
                  <div className="space-y-4">
                    <div className="rounded-[1.25rem] border border-border/70 bg-[linear-gradient(180deg,rgba(255,248,240,0.75),rgba(255,255,255,0.98))] px-5 py-5">
                      <div className="flex items-end gap-2">
                        <div className="text-5xl font-semibold tracking-tight tabular-nums">
                          {total}
                        </div>
                        <div className="pb-1 text-sm text-muted-foreground">{t('usage.unit')}</div>
                      </div>
                      <div className="mt-6">
                        <UsageSparkline series={usageSeries} />
                        <div className="mt-1 text-[11px] text-muted-foreground">
                          {t('usage.trend_30d')}
                        </div>
                      </div>
                    </div>
                  </div>
                ) : (
                  <EmptyState
                    icon={BarChart3}
                    title={t('usage.empty_title')}
                    description={t('usage.empty_body')}
                    className="py-16"
                  />
                )}
              </CardContent>
            </Card>
          </div>

          <div className="grid gap-6 xl:grid-cols-2">
            {usageSeries.length > 0 && (
              <Card className="border-border/70 shadow-none">
                <CardHeader className="border-b border-border/60 bg-muted/15">
                  <CardTitle>{t('usage.by_day')}</CardTitle>
                  <CardDescription>{t('usage.by_day_hint')}</CardDescription>
                </CardHeader>
                <CardContent className="pt-6">
                  <UsageBarChart series={usageSeries} />
                </CardContent>
              </Card>
            )}

            {sentimentData.length > 0 && (
              <Card className="border-border/70 shadow-none">
                <CardHeader className="border-b border-border/60 bg-muted/15">
                  <CardTitle className="flex items-center gap-2">
                    <SmilePlus className="h-4 w-4" />
                    {t('analytics.sentiment_distribution')}
                  </CardTitle>
                  <CardDescription>{t('analytics.sentiment_hint')}</CardDescription>
                </CardHeader>
                <CardContent className="pt-6">
                  <SentimentChart data={sentimentData} />
                </CardContent>
              </Card>
            )}
          </div>
        </>
      )}
    </div>
  )
}

type Usage = GetUsageResponse

interface UsageBucket {
  bucket: string
  value: number
}

function usageQuery() {
  return queryOptions({
    queryKey: ['console', 'usage'],
    queryFn: ({ signal }) => api<Usage>('/fb/v1/console/usage', { signal }),
    // 1 min - usage updates as ingest comes in; no need to thrash refetch.
    staleTime: 60_000,
  })
}

function enrichConfigQuery() {
  return queryOptions({
    queryKey: ['console', 'enrich-config'],
    queryFn: async ({ signal }) => {
      const resp = await api<GetEnrichConfigResponse>('/fb/v1/console/enrich-config', { signal })
      return resp.config
    },
    staleTime: 30_000,
  })
}

function UsageSparkline({ series }: { series: UsageBucket[] }) {
  const { t } = useTranslation()
  if (series.length === 0) return null
  const WIDTH = 220
  const HEIGHT = 36
  const PAD_X = 4
  const PAD_Y = 4
  const max = Math.max(...series.map((b) => b.value), 1)
  const barAreaW = WIDTH - PAD_X * 2
  const barAreaH = HEIGHT - PAD_Y * 2
  const gap = 2
  const maxBarW = 10
  const computedBarW = Math.max((barAreaW - gap * (series.length - 1)) / series.length, 2)
  const barW = Math.min(computedBarW, maxBarW)
  const usedW = barW * series.length + gap * Math.max(series.length - 1, 0)
  const offsetX = PAD_X + Math.max((barAreaW - usedW) / 2, 0)
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      className="h-9 w-[220px]"
      role="img"
      aria-label="Daily ingest sparkline"
    >
      <line
        x1={PAD_X}
        x2={WIDTH - PAD_X}
        y1={HEIGHT - PAD_Y}
        y2={HEIGHT - PAD_Y}
        className="stroke-border/70"
        strokeWidth={1}
      />
      {series.map((b, i) => {
        const h = (b.value / max) * barAreaH
        const x = offsetX + i * (barW + gap)
        const y = HEIGHT - PAD_Y - h
        return (
          <rect
            key={b.bucket}
            x={x}
            y={y}
            width={barW}
            height={h}
            className="fill-primary/75"
            rx={1.5}
          >
            <title>
              {t('usage.bar_tooltip', {
                date: format(new Date(b.bucket), t('usage.bar_date_format'), { locale: zhCN }),
                count: b.value,
              })}
            </title>
          </rect>
        )
      })}
    </svg>
  )
}

function UsageBarChart({ series }: { series: UsageBucket[] }) {
  const { t } = useTranslation()
  if (series.length === 0) return null
  const WIDTH = 600
  const HEIGHT = 220
  const PAD_X = 28
  const PAD_Y = 18
  const max = Math.max(...series.map((b) => b.value), 1)
  const barAreaW = WIDTH - PAD_X * 2
  const barAreaH = HEIGHT - PAD_Y * 2
  const gap = series.length > 12 ? 4 : 8
  const maxBarW = 30
  const computedBarW = Math.max((barAreaW - gap * (series.length - 1)) / series.length, 8)
  const barW = Math.min(computedBarW, maxBarW)
  const usedW = barW * series.length + gap * Math.max(series.length - 1, 0)
  const offsetX = PAD_X + Math.max((barAreaW - usedW) / 2, 0)
  const guideValues = [0.25, 0.5, 0.75, 1]
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      className="h-56 w-full"
      role="img"
      aria-label="Daily ingest counts"
    >
      {guideValues.map((ratio) => {
        const y = HEIGHT - PAD_Y - barAreaH * ratio
        return (
          <line
            key={ratio}
            x1={PAD_X}
            x2={WIDTH - PAD_X}
            y1={y}
            y2={y}
            className="stroke-border/70"
            strokeWidth={1}
            strokeDasharray="3 5"
          />
        )
      })}
      <line
        x1={PAD_X}
        x2={WIDTH - PAD_X}
        y1={HEIGHT - PAD_Y}
        y2={HEIGHT - PAD_Y}
        className="stroke-border"
        strokeWidth={1.2}
      />
      {series.map((b, i) => {
        const h = (b.value / max) * barAreaH
        const x = offsetX + i * (barW + gap)
        const y = HEIGHT - PAD_Y - h
        const showTick = series.length <= 7 || i === 0 || i === series.length - 1
        return (
          <g key={b.bucket}>
            <rect
              x={x}
              y={y}
              width={barW}
              height={h}
              className="fill-primary/85"
              rx={barW > 10 ? 4 : 2}
            >
              <title>
                {t('usage.bar_tooltip', {
                  date: format(new Date(b.bucket), t('usage.bar_date_format'), { locale: zhCN }),
                  count: b.value,
                })}
              </title>
            </rect>
            {showTick && (
              <text
                x={x + barW / 2}
                y={HEIGHT - 4}
                textAnchor="middle"
                className="fill-muted-foreground text-[10px]"
              >
                {format(new Date(b.bucket), 'M/d', { locale: zhCN })}
              </text>
            )}
          </g>
        )
      })}
    </svg>
  )
}
