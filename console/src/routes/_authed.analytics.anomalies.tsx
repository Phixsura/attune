import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  type AnomaliesFilters,
  type AnomalyEvent,
  anomaliesQuery,
  anomalyEvidenceQuery,
  anomalySeriesQuery,
} from '@/features/anomalies/api/anomalies'
import { AnomalyCard } from '@/features/anomalies/components/anomaly-card'
import { AnomalySeriesChart } from '@/features/anomalies/components/anomaly-series-chart'
import { ContributionBars } from '@/features/anomalies/components/contribution-bars'
import { useDocumentTitle } from '@/hooks/use-document-title'
import { cn } from '@/lib/utils'

interface AnomaliesSearch {
  event?: string
  status?: AnomaliesFilters['status']
}

export const Route = createFileRoute('/_authed/analytics/anomalies')({
  component: AnomaliesPage,
  validateSearch: (search: Record<string, unknown>): AnomaliesSearch => ({
    event: typeof search.event === 'string' ? search.event : undefined,
    status:
      search.status === 'resolved' || search.status === 'all' || search.status === 'retracted'
        ? search.status
        : undefined,
  }),
})

const STATUS_TABS = ['open', 'resolved', 'all'] as const

export function AnomaliesPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('anomalies.title', 'Anomalies'))
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const status = search.status ?? 'open'
  const { data: events, isLoading } = useQuery(anomaliesQuery({ status }))
  const selected = useMemo(
    () => events?.find((e) => e.eventId === search.event) ?? events?.[0],
    [events, search.event],
  )
  const selectEvent = (eventId: string) => {
    void navigate({ search: (prev: AnomaliesSearch) => ({ ...prev, event: eventId }) })
  }

  return (
    <div className="space-y-4 p-4" data-testid="anomalies-page">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t('anomalies.title', 'Anomalies')}</h1>
        <div className="flex gap-1 rounded-lg border p-0.5">
          {STATUS_TABS.map((tab) => (
            <button
              className={cn(
                'rounded-md px-3 py-1 text-sm',
                status === tab ? 'bg-accent font-medium' : 'text-muted-foreground',
              )}
              data-testid={`status-tab-${tab}`}
              key={tab}
              onClick={() => void navigate({ search: { status: tab } })}
              type="button"
            >
              {t(`anomalies.tabs.${tab}`, tab)}
            </button>
          ))}
        </div>
      </div>

      {isLoading ? (
        <Skeleton className="h-40 w-full" />
      ) : !events || events.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            {t('anomalies.empty', 'No anomalies detected — volume is tracking its baseline.')}
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 lg:grid-cols-[320px_1fr]">
          <div className="space-y-2" data-testid="anomaly-list">
            {events.map((event) => (
              <AnomalyCard
                event={event}
                key={event.eventId}
                onSelect={(e) => selectEvent(e.eventId)}
                selected={selected?.eventId === event.eventId}
              />
            ))}
          </div>
          {selected ? <AnomalyDetail event={selected} /> : null}
        </div>
      )}
    </div>
  )
}

function AnomalyDetail({ event }: { event: AnomalyEvent }) {
  const { t } = useTranslation()
  const { data: series } = useQuery(anomalySeriesQuery(event.sliceType, event.sliceKey))
  const { data: evidence } = useQuery(anomalyEvidenceQuery(event.eventId))

  return (
    <div className="space-y-4" data-testid="anomaly-detail">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{event.sliceDisplay || event.sliceKey}</CardTitle>
        </CardHeader>
        <CardContent>
          {series ? (
            <AnomalySeriesChart points={series.points} />
          ) : (
            <Skeleton className="h-52 w-full" />
          )}
        </CardContent>
      </Card>
      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">
              {t('anomalies.contribution.title', 'Main contributors')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {evidence ? <ContributionBars evidence={evidence} /> : <Skeleton className="h-20" />}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t('anomalies.evidence.title', 'Evidence')}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <p className="text-muted-foreground">
              {t('anomalies.evidence.window', 'Window {{from}} – {{to}}', {
                from: event.firstBucketDate,
                to: event.lastBucketDate,
              })}
            </p>
            {evidence && evidence.feedbackIds.length > 0 ? (
              <Link
                className="text-primary underline-offset-2 hover:underline"
                data-testid="evidence-link"
                search={{ ids: evidence.feedbackIds.join(',') }}
                to="/feedback"
              >
                {t('anomalies.evidence.view', 'View {{n}} sample feedback items', {
                  n: evidence.feedbackIds.length,
                })}
              </Link>
            ) : (
              <p className="text-muted-foreground">
                {t('anomalies.evidence.none', 'No sample feedback captured')}
              </p>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
