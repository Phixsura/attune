import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { BarChart3, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { usageQuery } from '@/features/usage/api/get-usage'
import { UsageBarChart } from '@/features/usage/components/bar-chart'
import { UsageSparkline } from '@/features/usage/components/sparkline'

export const Route = createFileRoute('/_authed/usage')({
  component: UsagePage,
  loader: ({ context }) => context.queryClient.ensureQueryData(usageQuery()),
})

function UsagePage() {
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
  // int64 buckets arrive as JSON strings; the charts want numbers.
  const series = usage.data.series.map((b) => ({ bucket: b.bucket, value: Number(b.value) }))
  const periodLabel = t('usage.period', {
    start: format(new Date(periodStart), 'yyyy-MM-dd', { locale: zhCN }),
    end: format(new Date(periodEnd), 'yyyy-MM-dd', { locale: zhCN }),
  })

  return (
    <div>
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t('nav.usage')}</h1>
        <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{t('usage.subtitle')}</p>
      </div>

      {/* Top stat card */}
      <Card className="mt-6">
        <CardHeader>
          <CardTitle>{t('usage.total_card')}</CardTitle>
          <CardDescription>{periodLabel}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-end justify-between gap-4">
            <div className="flex items-baseline gap-2">
              <div className="text-4xl font-semibold tabular-nums">{total}</div>
              <div className="text-sm text-muted-foreground">{t('usage.unit')}</div>
              {quota != null && <div className="ml-4 text-sm text-muted-foreground">/ {quota}</div>}
              {quota == null && (
                <div className="ml-4 text-xs text-muted-foreground">{t('usage.quota_unset')}</div>
              )}
            </div>
            {series.length > 0 && (
              <div className="text-right">
                <UsageSparkline series={series} />
                <div className="mt-0.5 text-[10px] text-muted-foreground">
                  {t('usage.trend_30d')}
                </div>
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Daily bar chart */}
      <Card className="mt-4">
        <CardHeader>
          <CardTitle>{t('usage.by_day')}</CardTitle>
        </CardHeader>
        <CardContent>
          {series.length > 0 ? (
            <UsageBarChart series={series} />
          ) : (
            <EmptyState
              icon={BarChart3}
              title={t('usage.empty_title')}
              description={t('usage.empty_body')}
            />
          )}
        </CardContent>
      </Card>
    </div>
  )
}
