import { useQuery } from '@tanstack/react-query'
import { PackageX } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
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
    <div>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('nav.outbox_dead')}</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            {t('outbox_dead.subtitle')}
          </p>
        </div>
        <div className="flex items-center gap-1 rounded-md border border-border p-1">
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
      </div>

      <Card className="mt-6">
        <CardHeader>
          <CardTitle>{t(`outbox_dead.filter.${status}`)}</CardTitle>
          <CardDescription>{list.data?.length ?? 0}</CardDescription>
        </CardHeader>
        <CardContent>
          {list.isPending ? (
            <Loading />
          ) : list.data && list.data.length > 0 ? (
            <DeliveriesTable
              deliveries={list.data}
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
    </div>
  )
}
