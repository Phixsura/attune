import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { AlertTriangle, BarChart3, Loader2, TrendingUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { feedbackStatsQuery } from '@/features/feedback/api/get-feedback-stats'
import { DimStatsBars } from '@/features/feedback/components/dim-stats-bars'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { usageQuery } from '@/features/usage/api/get-usage'
import { UsageBarChart } from '@/features/usage/components/bar-chart'
import { UsageSparkline } from '@/features/usage/components/sparkline'

export function AnalyticsDashboard() {
  const { t } = useTranslation()
  const stats = useQuery(feedbackStatsQuery())
  const config = useQuery(enrichConfigQuery())
  const usage = useQuery(usageQuery())

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
  const dimStats = stats.data?.dims ?? []
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
        </>
      )}
    </div>
  )
}
