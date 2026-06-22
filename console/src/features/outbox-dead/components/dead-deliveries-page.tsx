import { useQuery } from '@tanstack/react-query'
import { PackageX } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  type DeliveryStatusFilter,
  deliveriesQuery,
  type OutboxDelivery,
} from '@/features/outbox-dead/api/list-deliveries'
import { useRetryDelivery } from '@/features/outbox-dead/api/retry-delivery'
import { DeliveriesTable } from '@/features/outbox-dead/components/deliveries-table'

const FILTERS: DeliveryStatusFilter[] = ['dead', 'failed']

export function DeadDeliveriesPage() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<DeliveryStatusFilter>('dead')
  const list = useQuery(deliveriesQuery(status))
  const retry = useRetryDelivery()
  const deliveries = list.data ?? []
  const retryableCount = deliveries.filter((delivery) => !delivery.inFlight).length
  const inflightCount = deliveries.filter((delivery) => delivery.inFlight).length

  const handleRetry = (delivery: OutboxDelivery) => {
    retry.mutate(delivery.id, {
      onSuccess: () => toast.success(t('outbox_dead.retry_queued')),
      onError: (err) => {
        // ApiError carries the server envelope: 409 in-flight / not
        // retryable, 404 gone, 400 bad id. Surface the message verbatim.
        const apiErr = err as { status?: number; message?: string }
        toast.error(apiErr.message || t('outbox_dead.retry_failed'))
      },
    })
  }

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.administration')}
        title={t('nav.outbox_dead')}
        subtitle={t('outbox_dead.subtitle')}
        actions={
          <div className="flex items-center gap-1 rounded-md border border-border bg-background/85 p-1">
            {FILTERS.map((f) => (
              <Button
                key={f}
                variant={status === f ? 'secondary' : 'ghost'}
                size="sm"
                aria-pressed={status === f}
                onClick={() => setStatus(f)}
              >
                {t(`outbox_dead.filter.${f}`)}
              </Button>
            ))}
          </div>
        }
        metrics={
          <>
            <PageHeroMetric
              label={t('outbox_dead.summary.visible')}
              value={String(deliveries.length)}
              hint={t('outbox_dead.summary.visible_hint')}
            />
            <PageHeroMetric
              label={t('outbox_dead.summary.retryable')}
              value={String(retryableCount)}
              hint={t('outbox_dead.summary.retryable_hint')}
            />
            <PageHeroMetric
              label={t('outbox_dead.summary.inflight')}
              value={String(inflightCount)}
              hint={t('outbox_dead.summary.inflight_hint')}
              tone={inflightCount > 0 ? 'active' : 'default'}
            />
            <PageHeroMetric
              label={t('outbox_dead.summary.filter')}
              value={t(`outbox_dead.filter.${status}`)}
              hint={t(`outbox_dead.summary.${status}_hint`)}
            />
          </>
        }
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('outbox_dead.queue_title')}</CardTitle>
            <CardDescription>{t('outbox_dead.queue_description')}</CardDescription>
          </CardHeader>
          <CardContent className="pt-6">
            {list.isPending ? (
              <Loading />
            ) : deliveries.length > 0 ? (
              <DeliveriesTable
                deliveries={deliveries}
                retryingId={retry.isPending ? retry.variables : undefined}
                onRetry={handleRetry}
              />
            ) : (
              <EmptyState
                icon={PackageX}
                title={t(`outbox_dead.empty_title.${status}`)}
                description={t('outbox_dead.empty_body')}
              />
            )}
          </CardContent>
        </Card>

        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('outbox_dead.playbook_title')}</CardTitle>
            <CardDescription>{t('outbox_dead.playbook_description')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 pt-6">
            <PlaybookRow
              title={t('outbox_dead.playbook.classify_title')}
              body={t('outbox_dead.playbook.classify_body')}
            />
            <PlaybookRow
              title={t('outbox_dead.playbook.repair_title')}
              body={t('outbox_dead.playbook.repair_body')}
            />
            <PlaybookRow
              title={t('outbox_dead.playbook.escalate_title')}
              body={t('outbox_dead.playbook.escalate_body')}
            />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function PlaybookRow({ title, body }: { title: string; body: string }) {
  return (
    <div className="rounded-[1rem] border border-border/70 bg-background/85 px-4 py-3.5">
      <div className="text-sm font-semibold text-foreground">{title}</div>
      <div className="mt-1 text-sm leading-6 text-muted-foreground">{body}</div>
    </div>
  )
}
