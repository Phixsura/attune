import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import {
  AlertTriangle,
  Key,
  Loader2,
  Mail,
  MessageSquare,
  PauseCircle,
  PlayCircle,
  Trash2,
  Webhook,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { InboundSource } from '@/features/inbound-sources/api/list-inbound-sources'

// SourcesTable — pure presentation. The route owns the data + mutations
// and passes pending flags + callbacks; this component just renders.

export function SourcesTable({
  sources,
  togglingId,
  onRotate,
  onPause,
  onResume,
  onDelete,
}: {
  sources: InboundSource[]
  togglingId: string | undefined
  onRotate: (s: InboundSource) => void
  onPause: (s: InboundSource) => void
  onResume: (s: InboundSource) => void
  onDelete: (s: InboundSource) => void
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('inbound_sources.table.name')}</TableHead>
          <TableHead>{t('inbound_sources.table.channel')}</TableHead>
          <TableHead>{t('inbound_sources.table.slug')}</TableHead>
          <TableHead>{t('inbound_sources.table.state')}</TableHead>
          <TableHead>{t('inbound_sources.table.last_event')}</TableHead>
          <TableHead className="text-right">{t('inbound_sources.table.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {sources.map((src) => (
          <TableRow key={src.id} className={src.enabled ? '' : 'opacity-50'}>
            <TableCell className="font-medium">{src.name}</TableCell>
            <TableCell>
              <ChannelPill channel={src.channel} />
            </TableCell>
            <TableCell className="font-mono text-xs text-muted-foreground">{src.slug}</TableCell>
            <TableCell>
              <StateBadge source={src} />
            </TableCell>
            <TableCell className="text-muted-foreground text-xs">
              {src.lastEventAt
                ? formatDistanceToNow(new Date(src.lastEventAt), {
                    addSuffix: true,
                    locale: zhCN,
                  })
                : t('common.never')}
            </TableCell>
            <TableCell className="text-right">
              {src.channel === 'webhook' && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => onRotate(src)}
                  title={t('inbound_sources.actions.rotate')}
                >
                  <Key className="h-3.5 w-3.5" />
                </Button>
              )}
              {src.enabled ? (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => onPause(src)}
                  disabled={togglingId === src.id}
                  title={t('inbound_sources.actions.pause')}
                >
                  {togglingId === src.id ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <PauseCircle className="h-3.5 w-3.5" />
                  )}
                </Button>
              ) : (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => onResume(src)}
                  disabled={togglingId === src.id}
                  title={t('inbound_sources.actions.resume')}
                >
                  {togglingId === src.id ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <PlayCircle className="h-3.5 w-3.5" />
                  )}
                </Button>
              )}
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onDelete(src)}
                title={t('inbound_sources.actions.delete')}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function ChannelPill({ channel }: { channel: string }) {
  const { t } = useTranslation()
  const Icon = channel === 'email' ? Mail : channel === 'slack' ? MessageSquare : Webhook
  const label =
    channel === 'email'
      ? t('inbound_sources.channel.email')
      : channel === 'slack'
        ? t('inbound_sources.channel.slack')
        : channel === 'webhook'
          ? t('inbound_sources.channel.webhook')
          : channel
  return (
    <span className="inline-flex items-center gap-1 rounded-md border border-border bg-muted/40 px-1.5 py-0.5 text-xs">
      <Icon className="h-3 w-3" />
      {label}
    </span>
  )
}

function StateBadge({ source }: { source: InboundSource }) {
  const { t } = useTranslation()
  if (!source.enabled) {
    return (
      <span
        className="inline-flex items-center gap-1 rounded-md border border-border bg-muted/40 px-1.5 py-0.5 text-xs text-muted-foreground"
        title={t('inbound_sources.state.paused_tooltip')}
      >
        <PauseCircle className="h-3 w-3" />
        {t('inbound_sources.state.paused')}
      </span>
    )
  }
  if (source.lastError) {
    return (
      <span
        className="inline-flex items-center gap-1 rounded-md border border-destructive/30 bg-destructive/10 px-1.5 py-0.5 text-xs text-destructive"
        title={source.lastError}
      >
        <AlertTriangle className="h-3 w-3" />
        {t('inbound_sources.state.error')}
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-md border border-green-600/30 bg-green-600/10 px-1.5 py-0.5 text-xs text-green-700 dark:text-green-500">
      {t('inbound_sources.state.healthy')}
    </span>
  )
}
