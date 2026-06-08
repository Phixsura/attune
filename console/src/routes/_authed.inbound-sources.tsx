import { useMutation, useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
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
import {
  EmptyInboundSourcesIcon,
  SourcesTable,
} from '@/features/inbound-sources/components/sources-table'

export const Route = createFileRoute('/_authed/inbound-sources')({
  component: InboundSourcesPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(inboundSourcesQuery()),
})

// reveal — the local state slot for "show the freshly-minted webhook
// secret". Cleared on dialog close so the value never lingers in
// React tree memory.
interface RevealState {
  url?: string
  secretHex: string
  curlExample?: string
}

function InboundSourcesPage() {
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
      if (apiErr.code === 'rotation_in_grace_window') {
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
    <div>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('nav.inbound_sources')}</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            {t('inbound_sources.subtitle')}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Button onClick={() => setCreateOpen(true)}>{t('inbound_sources.create_button')}</Button>
        </div>
      </div>

      <Card className="mt-6">
        <CardHeader>
          <CardTitle>{t('nav.inbound_sources')}</CardTitle>
          <CardDescription>{list.data?.length ?? 0}</CardDescription>
        </CardHeader>
        <CardContent>
          {list.isPending ? (
            <Loading />
          ) : list.data && list.data.length > 0 ? (
            <SourcesTable
              sources={list.data}
              togglingId={togglingId}
              onRotate={(s) => setRotateTarget(s)}
              onPause={handlePause}
              onResume={handleResume}
              onDelete={(s) => setDeleteTarget(s)}
            />
          ) : (
            <EmptyState
              icon={EmptyInboundSourcesIcon}
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

function Loading() {
  const { t } = useTranslation()
  return (
    <div className="flex items-center justify-center py-8 text-muted-foreground">
      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
      {t('app.loading')}
    </div>
  )
}
