import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { format, formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { InboxIcon, Loader2, RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  type InboundSourceCreate,
  useCreateInboundSource,
} from '@/features/inbound-sources/api/create-inbound-source'
import { useDeleteInboundSource } from '@/features/inbound-sources/api/delete-inbound-source'
import { inboundSourceQuery } from '@/features/inbound-sources/api/get-inbound-source'
import {
  type InboundSource,
  inboundSourcesQuery,
} from '@/features/inbound-sources/api/list-inbound-sources'
import { usePauseInboundSource } from '@/features/inbound-sources/api/pause-inbound-source'
import { useRecentFeedback } from '@/features/inbound-sources/api/recent-feedback'
import { useResumeInboundSource } from '@/features/inbound-sources/api/resume-inbound-source'
import { useRotateInboundSource } from '@/features/inbound-sources/api/rotate-inbound-source'
import { useSyncNow } from '@/features/inbound-sources/api/sync-now'
import { CreateInboundSourceDialog } from '@/features/inbound-sources/components/create-dialog'
import { DeleteInboundSourceDialog } from '@/features/inbound-sources/components/delete-dialog'
import { RotateConfirmDialog } from '@/features/inbound-sources/components/rotate-dialog'
import { SecretRevealDialog } from '@/features/inbound-sources/components/secret-reveal-dialog'
import {
  ChannelPill,
  SourcesTable,
  StateBadge,
} from '@/features/inbound-sources/components/sources-table'
import { ErrorCode } from '@/proto/attune/v1/common'

// reveal — the local state slot for "show the freshly-minted webhook
// secret". Cleared on dialog close so the value never lingers in
// React tree memory.
interface RevealState {
  url?: string
  secretHex: string
  curlExample?: string
}

export function InboundSourcesPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const list = useQuery(inboundSourcesQuery())
  const create = useCreateInboundSource()
  const rotate = useRotateInboundSource()
  const pause = usePauseInboundSource()
  const resume = useResumeInboundSource()
  const del = useDeleteInboundSource()

  const [createOpen, setCreateOpen] = useState(false)
  const [selectedSourceID, setSelectedSourceID] = useState('')
  const [rotateTarget, setRotateTarget] = useState<InboundSource | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<InboundSource | null>(null)
  const [reveal, setReveal] = useState<RevealState | null>(null)
  const sources = list.data ?? []
  const selectedSource = sources.find((source) => source.id === selectedSourceID) ?? null
  const selectedSourceQuery = useQuery({
    ...inboundSourceQuery(selectedSourceID || 'selected-source'),
    enabled: Boolean(selectedSourceID),
    placeholderData: selectedSource ?? undefined,
  })
  const selectedDetail = selectedSourceQuery.data ?? selectedSource
  const healthyCount = sources.filter((source) => source.enabled && !source.lastError).length
  const pausedCount = sources.filter((source) => !source.enabled).length
  const errorCount = sources.filter((source) => source.enabled && Boolean(source.lastError)).length

  useEffect(() => {
    if (sources.length === 0) {
      if (selectedSourceID) setSelectedSourceID('')
      return
    }
    if (selectedSourceID && sources.some((source) => source.id === selectedSourceID)) {
      return
    }
    setSelectedSourceID(sources[0]?.id ?? '')
  }, [selectedSourceID, sources])

  const handleCreate = (body: InboundSourceCreate) =>
    create.mutateAsync(body, {
      onSuccess: (res) => {
        const source = res.source
        setCreateOpen(false)
        if (source) {
          queryClient.setQueryData<InboundSource[]>(
            ['console', 'inbound-sources'],
            (current = []) => {
              const next = current.filter((item) => item.id !== source.id)
              return [...next, source]
            },
          )
          queryClient.setQueryData(['console', 'inbound-sources', 'detail', source.id], source)
          setSelectedSourceID(source.id)
        }
        if (body.channel === 'zendesk' && body.zendeskConfig?.subdomain) {
          toast.success(
            t('inbound_sources.toast.zendesk_connected', {
              subdomain: body.zendeskConfig.subdomain,
            }),
          )
        } else if (body.channel === 'intercom') {
          toast.success(
            t('inbound_sources.toast.intercom_connected', {
              region: (body.intercomConfig?.region ?? 'us').toUpperCase(),
            }),
          )
        } else {
          toast.success(
            source
              ? t('inbound_sources.toast.created_with_name', { name: source.name })
              : t('inbound_sources.toast.created'),
          )
        }
        if (res.webhookSecretReveal?.secretHex) {
          setReveal({
            url: res.webhookSecretReveal.url || undefined,
            secretHex: res.webhookSecretReveal.secretHex,
            curlExample: res.webhookSecretReveal.curlExample || undefined,
          })
        }
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
    })

  const handleRotate = useMutation({
    mutationFn: (id: string) => rotate.mutateAsync(id),
    onSuccess: (res) => {
      setRotateTarget(null)
      setReveal({
        secretHex: res.secretHex,
      })
      toast.success(t('inbound_sources.toast.rotated'))
    },
    onError: (err) => {
      const apiErr = err as { status?: number; code?: string; message?: string }
      if (apiErr.code === ErrorCode.ROTATION_IN_GRACE_WINDOW) {
        toast.error(t('inbound_sources.toast.rotation_in_grace'))
      } else {
        toast.error(apiErr.message || 'failed')
      }
    },
  })

  const handleDelete = useMutation({
    mutationFn: (id: string) => del.mutateAsync(id),
    onSuccess: (_result, id) => {
      queryClient.setQueryData<InboundSource[]>(['console', 'inbound-sources'], (current = []) =>
        current.filter((source) => source.id !== id),
      )
      if (selectedSourceID === id) {
        setSelectedSourceID('')
      }
      setDeleteTarget(null)
      toast.success(t('inbound_sources.toast.deleted'))
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
  })

  const handlePause = (s: InboundSource) =>
    pause.mutate(s.id, {
      onSuccess: () => toast.success(t('inbound_sources.toast.paused')),
      onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
    })

  const handleResume = (s: InboundSource) =>
    resume.mutate(s.id, {
      onSuccess: () => toast.success(t('inbound_sources.toast.resumed')),
      onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
    })

  const togglingId = pause.isPending
    ? pause.variables
    : resume.isPending
      ? resume.variables
      : undefined

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.integrations')}
        title={t('nav.inbound_sources')}
        subtitle={t('inbound_sources.subtitle')}
        actions={
          <Button onClick={() => setCreateOpen(true)}>{t('inbound_sources.create_button')}</Button>
        }
        metrics={
          <>
            <PageHeroMetric
              label={t('inbound_sources.summary.total')}
              value={String(sources.length)}
              hint={t('inbound_sources.summary.total_hint')}
            />
            <PageHeroMetric
              label={t('inbound_sources.summary.healthy')}
              value={String(healthyCount)}
              hint={t('inbound_sources.summary.healthy_hint')}
            />
            <PageHeroMetric
              label={t('inbound_sources.summary.paused')}
              value={String(pausedCount)}
              hint={t('inbound_sources.summary.paused_hint')}
            />
            <PageHeroMetric
              label={t('inbound_sources.summary.errors')}
              value={String(errorCount)}
              hint={t('inbound_sources.summary.errors_hint')}
              tone={errorCount > 0 ? 'urgent' : 'default'}
            />
          </>
        }
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="text-base">{t('inbound_sources.registry_title')}</CardTitle>
            <CardDescription>
              {t('inbound_sources.registry_description', { count: sources.length })}
            </CardDescription>
          </CardHeader>
          <CardContent className="pt-6">
            {list.isPending ? (
              <Loading />
            ) : sources.length > 0 ? (
              <SourcesTable
                sources={sources}
                selectedID={selectedSourceID}
                togglingId={togglingId}
                onSelect={(s) => setSelectedSourceID(s.id)}
                onRotate={(s) => setRotateTarget(s)}
                onPause={handlePause}
                onResume={handleResume}
                onDelete={(s) => setDeleteTarget(s)}
              />
            ) : (
              <EmptyState
                icon={InboxIcon}
                title={t('inbound_sources.empty_title')}
                description={t('inbound_sources.empty_body')}
                action={{
                  label: t('inbound_sources.create_button'),
                  onClick: () => setCreateOpen(true),
                }}
              />
            )}
          </CardContent>
        </Card>

        <div className="space-y-6">
          <SourceDetailCard
            source={selectedDetail}
            loading={selectedSourceQuery.isFetching && Boolean(selectedSourceID)}
            onCreate={() => setCreateOpen(true)}
          />

          <Card className="border-border/60 shadow-none">
            <CardHeader>
              <CardTitle className="text-base">{t('inbound_sources.playbook_title')}</CardTitle>
              <CardDescription>{t('inbound_sources.playbook_description')}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 pt-6">
              <PlaybookRow
                title={t('inbound_sources.playbook.segmentation_title')}
                body={t('inbound_sources.playbook.segmentation_body')}
              />
              <PlaybookRow
                title={t('inbound_sources.playbook.rotation_title')}
                body={t('inbound_sources.playbook.rotation_body')}
              />
              <PlaybookRow
                title={t('inbound_sources.playbook.pause_title')}
                body={t('inbound_sources.playbook.pause_body')}
              />
            </CardContent>
          </Card>
        </div>
      </div>

      <CreateInboundSourceDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
        pending={create.isPending}
      />
      <RotateConfirmDialog
        source={rotateTarget}
        onCancel={() => setRotateTarget(null)}
        onConfirm={() => rotateTarget && handleRotate.mutate(rotateTarget.id)}
        pending={handleRotate.isPending}
      />
      <DeleteInboundSourceDialog
        source={deleteTarget}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => deleteTarget && handleDelete.mutate(deleteTarget.id)}
        pending={handleDelete.isPending}
      />
      <SecretRevealDialog
        open={reveal !== null}
        onClose={() => setReveal(null)}
        url={reveal?.url}
        secretHex={reveal?.secretHex ?? ''}
        curlExample={reveal?.curlExample}
      />
    </div>
  )
}

function PlaybookRow({ title, body }: { title: string; body: string }) {
  return (
    <div className="rounded-[1rem] border border-border/60 bg-background/85 px-4 py-3.5">
      <div className="text-sm font-semibold text-foreground">{title}</div>
      <div className="mt-1 text-sm leading-6 text-muted-foreground">{body}</div>
    </div>
  )
}

function SourceDetailCard({
  source,
  loading,
  onCreate,
}: {
  source: InboundSource | null
  loading: boolean
  onCreate: () => void
}) {
  const { t } = useTranslation()
  const syncNow = useSyncNow()
  const recent = useRecentFeedback(source?.id ?? null)

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('inbound_sources.detail_title')}</CardTitle>
        <CardDescription>
          {source
            ? t('inbound_sources.detail_description', { name: source.name })
            : t('inbound_sources.detail_empty_body')}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 pt-6">
        {!source ? (
          <EmptyState
            icon={InboxIcon}
            title={t('inbound_sources.detail_empty_title')}
            description={t('inbound_sources.detail_empty_body')}
            action={{
              label: t('inbound_sources.create_button'),
              onClick: onCreate,
            }}
          />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <ChannelPill channel={source.channel} />
              <StateBadge source={source} />
              <span className="rounded-md border border-border bg-muted/40 px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
                {source.slug}
              </span>
              {loading && (
                <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  {t('inbound_sources.detail_refreshing')}
                </span>
              )}
            </div>

            {source.enabled && (
              <Button
                variant="outline"
                size="sm"
                disabled={syncNow.isPending}
                onClick={() => {
                  syncNow.mutate(source.id, {
                    onSuccess: () => toast.success(t('inbound_sources.toast.sync_requested')),
                    onError: (err) =>
                      toast.error(err instanceof Error ? err.message : t('common.error')),
                  })
                }}
              >
                {syncNow.isPending ? (
                  <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
                ) : (
                  <RefreshCw className="mr-2 h-3.5 w-3.5" />
                )}
                {t('inbound_sources.detail.sync_now')}
              </Button>
            )}

            <DetailGrid
              rows={[
                { label: t('inbound_sources.detail.source_id'), value: source.id, mono: true },
                { label: t('inbound_sources.detail.slug'), value: source.slug, mono: true },
                {
                  label: t('inbound_sources.detail.last_event'),
                  value: source.lastEventAt
                    ? formatDistanceToNow(new Date(source.lastEventAt), {
                        addSuffix: true,
                        locale: zhCN,
                      })
                    : t('common.never'),
                  hint: source.lastEventAt
                    ? format(new Date(source.lastEventAt), 'PPP HH:mm', { locale: zhCN })
                    : t('inbound_sources.detail.last_event_empty'),
                },
                {
                  label: t('inbound_sources.detail.cursor'),
                  value:
                    source.lastUid && source.lastUid !== '0'
                      ? source.lastUid
                      : t('inbound_sources.detail.cursor_empty'),
                  hint:
                    source.lastUid && source.lastUid !== '0'
                      ? t('inbound_sources.detail.cursor_hint')
                      : t('inbound_sources.detail.cursor_empty_hint'),
                  mono: true,
                },
                {
                  label: t('inbound_sources.detail.created_at'),
                  value: formatDistanceToNow(new Date(source.createdAt), {
                    addSuffix: true,
                    locale: zhCN,
                  }),
                  hint: format(new Date(source.createdAt), 'PPP HH:mm', { locale: zhCN }),
                },
                {
                  label: t('inbound_sources.detail.updated_at'),
                  value: formatDistanceToNow(new Date(source.updatedAt), {
                    addSuffix: true,
                    locale: zhCN,
                  }),
                  hint: format(new Date(source.updatedAt), 'PPP HH:mm', { locale: zhCN }),
                },
                ...(source.ticketsSynced
                  ? [
                      {
                        label: t('inbound_sources.detail.tickets_synced'),
                        value: String(source.ticketsSynced),
                      },
                      {
                        label: t('inbound_sources.detail.backfill_status'),
                        value: source.backfillDone
                          ? t('inbound_sources.detail.backfill_done')
                          : t('inbound_sources.detail.backfill_in_progress'),
                      },
                    ]
                  : []),
              ]}
            />

            <div
              className={
                source.lastError
                  ? 'rounded-[1rem] border border-destructive/30 bg-destructive/10 p-4'
                  : 'rounded-[1rem] border border-border/60 bg-background/85 p-4'
              }
            >
              <div className="text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">
                {t('inbound_sources.detail.last_error')}
              </div>
              <div
                className={
                  source.lastError
                    ? 'mt-2 break-words text-sm leading-6 text-destructive'
                    : 'mt-2 text-sm leading-6 text-muted-foreground'
                }
              >
                {source.lastError || t('inbound_sources.detail.no_error')}
              </div>
            </div>

            {recent.data?.items && recent.data.items.length > 0 && (
              <div className="space-y-2">
                <div className="text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">
                  {t('inbound_sources.detail.recent_title')}
                </div>
                <div className="space-y-1.5">
                  {recent.data.items.map((item) => (
                    <div
                      key={item.id}
                      className="rounded-md border border-border/60 bg-muted/20 px-3 py-2 text-xs"
                    >
                      <span className="text-muted-foreground">
                        {item.source_meta?.zendesk_ticket_id
                          ? `#${item.source_meta.zendesk_ticket_id} · `
                          : item.source_meta?.intercom_conversation_id
                            ? `#${item.source_meta.intercom_conversation_id} · `
                            : ''}
                      </span>
                      <span className="text-foreground">{item.content_preview}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

function DetailGrid({
  rows,
}: {
  rows: Array<{ hint?: string; label: string; mono?: boolean; value: string }>
}) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      {rows.map((row) => (
        <div key={row.label} className="rounded-[1rem] border border-border/60 bg-muted/20 p-3.5">
          <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
            {row.label}
          </div>
          <div
            className={
              row.mono ? 'mt-2 font-mono text-sm text-foreground' : 'mt-2 text-sm text-foreground'
            }
          >
            {row.value}
          </div>
          {row.hint && (
            <span className="mt-1 block text-xs leading-5 text-muted-foreground">{row.hint}</span>
          )}
        </div>
      ))}
    </div>
  )
}
