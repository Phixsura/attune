import { useQuery } from '@tanstack/react-query'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { BarChart3, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { usageQuery } from '@/features/usage/api/get-usage'
import { UsageBarChart } from '@/features/usage/components/bar-chart'
import { UsageSparkline } from '@/features/usage/components/sparkline'

export const Route = createFileRoute('/_authed/usage')({
  beforeLoad: () => {
    throw redirect({ to: '/analytics/usage' })
  },
})

export function UsagePage() {
  const { t } = useTranslation()
  const usage = useQuery(usageQuery())

  if (usage.isPending) {
    return (
      <div className="flex items-center justify-center py-12 text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        {t('app.loading')}
      </div>
    )
  }
  if (!usage.data) return null

  const { total, quota, periodStart, periodEnd } = usage.data
  const series = usage.data.series.map((b) => ({ bucket: b.bucket, value: Number(b.value) }))
  const activeDays = series.filter((entry) => entry.value > 0).length
  const avgPerBucket =
    series.length > 0
      ? Math.round(series.reduce((sum, item) => sum + item.value, 0) / series.length)
      : 0
  const periodLabel = t('usage.period', {
    start: format(new Date(periodStart), 'yyyy-MM-dd', { locale: zhCN }),
    end: format(new Date(periodEnd), 'yyyy-MM-dd', { locale: zhCN }),
  })

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.analytics')}
        title={t('nav.usage')}
        subtitle={t('usage.subtitle')}
        metrics={
          <>
            <PageHeroMetric
              label={t('usage.summary.total')}
              value={String(total)}
              hint={periodLabel}
            />
            <PageHeroMetric
              label={t('usage.summary.quota')}
              value={quota != null ? String(quota) : '—'}
              hint={quota != null ? t('usage.summary.quota_hint') : t('usage.quota_unset')}
            />
            <PageHeroMetric
              label={t('usage.summary.active_days')}
              value={String(activeDays)}
              hint={t('usage.summary.active_days_hint')}
            />
            <PageHeroMetric
              label={t('usage.summary.average')}
              value={String(avgPerBucket)}
              hint={t('usage.summary.average_hint')}
            />
          </>
        }
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,0.82fr)_minmax(0,1.18fr)]">
        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('usage.total_card')}</CardTitle>
            <CardDescription>{periodLabel}</CardDescription>
          </CardHeader>
          <CardContent className="pt-6">
            <div className="rounded-[1.25rem] border border-border/70 bg-[linear-gradient(180deg,rgba(255,248,240,0.75),rgba(255,255,255,0.98))] px-5 py-5">
              <div className="flex items-end gap-2">
                <div className="text-5xl font-semibold tracking-tight tabular-nums">{total}</div>
                <div className="pb-1 text-sm text-muted-foreground">{t('usage.unit')}</div>
              </div>
              <div className="mt-2 text-sm text-muted-foreground">
                {quota != null ? `${t('usage.summary.quota')} / ${quota}` : t('usage.quota_unset')}
              </div>
              {series.length > 0 && (
                <div className="mt-6">
                  <UsageSparkline series={series} />
                  <div className="mt-1 text-[11px] text-muted-foreground">
                    {t('usage.trend_30d')}
                  </div>
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('usage.by_day')}</CardTitle>
            <CardDescription>{t('usage.by_day_hint')}</CardDescription>
          </CardHeader>
          <CardContent className="pt-6">
            {series.length > 0 ? (
              <UsageBarChart series={series} />
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
    </div>
  )
}
