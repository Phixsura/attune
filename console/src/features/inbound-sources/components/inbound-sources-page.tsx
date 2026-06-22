import { useMutation, useQuery } from '@tanstack/react-query'
import { InboxIcon } from 'lucide-react'
import { useState } from 'react'
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
import {
  type InboundSource,
  inboundSourcesQuery,
} from '@/features/inbound-sources/api/list-inbound-sources'
import { usePauseInboundSource } from '@/features/inbound-sources/api/pause-inbound-source'
import { useResumeInboundSource } from '@/features/inbound-sources/api/resume-inbound-source'
import { useRotateInboundSource } from '@/features/inbound-sources/api/rotate-inbound-source'
import { CreateInboundSourceDialog } from '@/features/inbound-sources/components/create-dialog'
import { DeleteInboundSourceDialog } from '@/features/inbound-sources/components/delete-dialog'
import { RotateConfirmDialog } from '@/features/inbound-sources/components/rotate-dialog'
import { SecretRevealDialog } from '@/features/inbound-sources/components/secret-reveal-dialog'
import { SourcesTable } from '@/features/inbound-sources/components/sources-table'
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
  const list = useQuery(inboundSourcesQuery())
  const create = useCreateInboundSource()
  const rotate = useRotateInboundSource()
  const pause = usePauseInboundSource()
  const resume = useResumeInboundSource()
  const del = useDeleteInboundSource()

  const [createOpen, setCreateOpen] = useState(false)
  const [rotateTarget, setRotateTarget] = useState<InboundSource | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<InboundSource | null>(null)
  const [reveal, setReveal] = useState<RevealState | null>(null)
  const sources = list.data ?? []
  const healthyCount = sources.filter((source) => source.enabled && !source.lastError).length
  const pausedCount = sources.filter((source) => !source.enabled).length
  const errorCount = sources.filter((source) => source.enabled && Boolean(source.lastError)).length

  const handleCreate = (body: InboundSourceCreate) =>
    create.mutateAsync(body, {
      onSuccess: (res) => {
        setCreateOpen(false)
        toast.success(t('inbound_sources.toast.created'))
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
    onSuccess: () => {
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
        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('inbound_sources.registry_title')}</CardTitle>
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
                togglingId={togglingId}
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

        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('inbound_sources.playbook_title')}</CardTitle>
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
    <div className="rounded-[1rem] border border-border/70 bg-background/85 px-4 py-3.5">
      <div className="text-sm font-semibold text-foreground">{title}</div>
      <div className="mt-1 text-sm leading-6 text-muted-foreground">{body}</div>
    </div>
  )
}
