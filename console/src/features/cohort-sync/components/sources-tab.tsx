import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Loader2, Pencil, Power, PowerOff, Trash2, Zap } from 'lucide-react'
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

  if (sources.length === 0) {
    return (
      <EmptyState
        title={t('cohort_sync.source.no_sources')}
        description={t('cohort_sync.source.no_sources_desc')}
        action={{ label: t('cohort_sync.source.create'), onClick: onCreateClick }}
      />
    )
  }

  return (
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
              />
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function SourceRow({
  source,
  testing,
  onTest,
  onEdit,
  onDelete,
  onToggleEnabled,
}: {
  source: CohortSource
  testing: boolean
  onTest: () => void
  onEdit: () => void
  onDelete: () => void
  onToggleEnabled: () => void
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
  if (!enabled) {
    return (
      <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
        {t('cohort_sync.source.status.disabled')}
      </span>
    )
  }
  if (status === 'error') {
    return (
      <span className="rounded-full bg-destructive/10 px-2 py-0.5 text-xs text-destructive">
        {t('cohort_sync.source.status.error')}
      </span>
    )
  }
  return (
    <span className="rounded-full bg-green-100 px-2 py-0.5 text-xs text-green-700 dark:bg-green-900/30 dark:text-green-400">
      {t('cohort_sync.source.status.active')}
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
