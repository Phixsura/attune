import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { ExternalLink, History, Loader2, Pencil, Power, PowerOff, Trash2, Zap } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { CohortSource } from '../api/cohort-sync'
import { SourceEventsSheet } from './source-events-sheet'
import { WebhookUrlsDisplay } from './webhook-urls-display'

export function SourcesTab({
  sources,
  testingId,
  onTest,
  onEdit,
  onDelete,
  onToggleEnabled,
  onCreateClick,
}: {
  sources: CohortSource[]
  testingId?: string
  onTest: (s: CohortSource) => void
  onEdit: (s: CohortSource) => void
  onDelete: (s: CohortSource) => void
  onToggleEnabled: (s: CohortSource) => void
  onCreateClick: () => void
}) {
  const { t } = useTranslation()
  const [eventsSource, setEventsSource] = useState<CohortSource | null>(null)

  if (sources.length === 0) {
    return (
      <div className="mx-auto max-w-lg space-y-6 py-12 text-center">
        <EmptyState
          title={t('cohort_sync.source.no_sources')}
          description={t('cohort_sync.source.no_sources_desc')}
          action={{ label: t('cohort_sync.source.create'), onClick: onCreateClick }}
        />
        <div className="space-y-3 rounded-md border bg-muted/30 p-4 text-left text-sm">
          <p className="font-medium">{t('cohort_sync.onboarding.title')}</p>
          <ol className="list-inside list-decimal space-y-2 text-muted-foreground">
            <li>{t('cohort_sync.onboarding.step1')}</li>
            <li>{t('cohort_sync.onboarding.step2')}</li>
            <li>{t('cohort_sync.onboarding.step3')}</li>
          </ol>
          <div className="flex gap-3 pt-2">
            <a
              href="https://www.docs.developers.amplitude.com/analytics/apis/cohort-api/"
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-xs text-primary hover:underline"
            >
              Amplitude {t('cohort_sync.onboarding.docs')}
              <ExternalLink className="h-3 w-3" />
            </a>
            <a
              href="https://docs.mixpanel.com/docs/cohort-sync/overview"
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-xs text-primary hover:underline"
            >
              Mixpanel {t('cohort_sync.onboarding.docs')}
              <ExternalLink className="h-3 w-3" />
            </a>
          </div>
        </div>
      </div>
    )
  }

  return (
    <>
      <Card className="border-border/60 shadow-none">
        <CardHeader>
          <CardTitle className="text-base">{t('cohort_sync.tabs.sources')}</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('cohort_sync.source.name')}</TableHead>
                <TableHead>{t('cohort_sync.source.provider')}</TableHead>
                <TableHead>{t('cohort_sync.source.status_label')}</TableHead>
                <TableHead>{t('cohort_sync.source.last_sync')}</TableHead>
                <TableHead>{t('cohort_sync.source.webhook_urls')}</TableHead>
                <TableHead className="text-right">{t('common.edit')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sources.map((source) => (
                <SourceRow
                  key={source.id}
                  source={source}
                  testing={testingId === source.id}
                  onTest={() => onTest(source)}
                  onEdit={() => onEdit(source)}
                  onDelete={() => onDelete(source)}
                  onToggleEnabled={() => onToggleEnabled(source)}
                  onShowEvents={() => setEventsSource(source)}
                />
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      {eventsSource && (
        <SourceEventsSheet
          source={eventsSource}
          open={!!eventsSource}
          onOpenChange={(v) => !v && setEventsSource(null)}
        />
      )}
    </>
  )
}

function SourceRow({
  source,
  testing,
  onTest,
  onEdit,
  onDelete,
  onToggleEnabled,
  onShowEvents,
}: {
  source: CohortSource
  testing: boolean
  onTest: () => void
  onEdit: () => void
  onDelete: () => void
  onToggleEnabled: () => void
  onShowEvents: () => void
}) {
  const { t } = useTranslation()

  return (
    <TableRow className={source.enabled ? '' : 'opacity-50'}>
      <TableCell>
        <div className="text-sm font-medium">{source.name}</div>
        {source.lastError && (
          <div
            className="mt-0.5 max-w-[20rem] truncate text-xs text-destructive"
            title={source.lastError}
          >
            {source.lastError}
          </div>
        )}
      </TableCell>
      <TableCell className="text-muted-foreground">{source.provider}</TableCell>
      <TableCell>
        <StatusBadge status={source.status} enabled={source.enabled} />
      </TableCell>
      <TableCell className={`text-sm ${stalenessColor(source.lastSyncAt)}`}>
        {source.lastSyncAt
          ? formatDistanceToNow(new Date(source.lastSyncAt), { addSuffix: true, locale: zhCN })
          : t('common.never')}
      </TableCell>
      <TableCell>
        <WebhookUrlsDisplay urls={source.webhookUrls ?? []} provider={source.provider} compact />
      </TableCell>
      <TableCell className="text-right">
        {source.lastTestOk != null && (
          <span
            className={`mr-1 text-xs ${source.lastTestOk ? 'text-green-600' : 'text-destructive'}`}
            title={
              source.lastTestedAt
                ? formatDistanceToNow(new Date(source.lastTestedAt), {
                    addSuffix: true,
                    locale: zhCN,
                  })
                : undefined
            }
          >
            {source.lastTestOk ? '✓' : '✗'}
          </span>
        )}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="sm" onClick={onTest} disabled={testing}>
              {testing ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Zap className="h-3.5 w-3.5" />
              )}
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('cohort_sync.source.test')}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="sm" onClick={onEdit}>
              <Pencil className="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('common.edit')}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="sm" onClick={onToggleEnabled}>
              {source.enabled ? (
                <PowerOff className="h-3.5 w-3.5" />
              ) : (
                <Power className="h-3.5 w-3.5" />
              )}
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            {source.enabled ? t('cohort_sync.cohort.disable') : t('cohort_sync.cohort.enable')}
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="sm" onClick={onShowEvents}>
              <History className="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('cohort_sync.events.title')}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="sm" onClick={onDelete}>
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('common.delete')}</TooltipContent>
        </Tooltip>
      </TableCell>
    </TableRow>
  )
}

function StatusBadge({ status, enabled }: { status: string; enabled: boolean }) {
  const { t } = useTranslation()
  const label = !enabled
    ? t('cohort_sync.source.status.disabled')
    : status === 'error'
      ? t('cohort_sync.source.status.error')
      : t('cohort_sync.source.status.active')
  const cls = !enabled
    ? 'border-border bg-muted/50 text-muted-foreground'
    : status === 'error'
      ? 'border-destructive/20 bg-destructive/10 text-destructive'
      : 'border-green-300/40 bg-green-100/60 text-green-800 dark:bg-green-900/20 dark:text-green-200'
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[11px] font-medium ${cls}`}
    >
      {label}
    </span>
  )
}

function stalenessColor(lastSyncAt: string | Date | undefined): string {
  if (!lastSyncAt) return 'text-muted-foreground'
  const ts = lastSyncAt instanceof Date ? lastSyncAt.getTime() : new Date(lastSyncAt).getTime()
  const hours = (Date.now() - ts) / 3_600_000
  if (hours < 1) return 'text-green-600 dark:text-green-400'
  if (hours < 24) return 'text-amber-600 dark:text-amber-400'
  return 'text-destructive'
}
