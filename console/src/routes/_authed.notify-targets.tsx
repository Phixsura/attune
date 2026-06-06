import { useMutation, useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Bell, CheckCircle2, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  type NotifyTargetCreate,
  useCreateNotifyTarget,
} from '@/features/notify-targets/api/create-notify-target'
import { useDeleteNotifyTarget } from '@/features/notify-targets/api/delete-notify-target'
import {
  type NotifyTarget,
  notifyTargetsQuery,
} from '@/features/notify-targets/api/list-notify-targets'
import { useTestNotifyTarget } from '@/features/notify-targets/api/test-notify-target'
import {
  type NotifyTargetPatch,
  useUpdateNotifyTarget,
} from '@/features/notify-targets/api/update-notify-target'
import {
  CreateNotifyDialog,
  DeleteNotifyDialog,
} from '@/features/notify-targets/components/dialogs'
import { EditNotifyDialog } from '@/features/notify-targets/components/edit-dialog'
import { TargetTable, type TestState } from '@/features/notify-targets/components/table'

export const Route = createFileRoute('/_authed/notify-targets')({
  component: NotifyTargetsPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(notifyTargetsQuery()),
})

function NotifyTargetsPage() {
  const { t } = useTranslation()
  const list = useQuery(notifyTargetsQuery())
  const create = useCreateNotifyTarget()
  const del = useDeleteNotifyTarget()
  const test = useTestNotifyTarget()

  const [createOpen, setCreateOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<NotifyTarget | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<NotifyTarget | null>(null)
  const [lastTest, setLastTest] = useState<TestState | null>(null)
  const update = useUpdateNotifyTarget()

  const handleCreate = (body: NotifyTargetCreate) =>
    create.mutateAsync(body, {
      onSuccess: () => {
        setCreateOpen(false)
        toast.success(t('common.create'))
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
    })

  const handleEdit = (patch: NotifyTargetPatch) => {
    if (!editTarget) return Promise.resolve()
    return update.mutateAsync(
      { id: editTarget.id, patch },
      {
        onSuccess: () => {
          setEditTarget(null)
          toast.success(t('common.save'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  const handleDelete = useMutation({
    mutationFn: (id: string) => del.mutateAsync(id),
    onSuccess: () => {
      setDeleteTarget(null)
      toast.success(t('common.delete'))
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
  })

  const handleTest = (target: NotifyTarget) => {
    test.mutate(target.id, {
      onSuccess: (result) => {
        setLastTest({ id: target.id, result })
        toast.success(t('notify_targets.test_result.ok', { ms: result.latencyMs }))
      },
      onError: (err) => {
        // The /test endpoint returns 502 with envelope on delivery failure;
        // ApiError captures code+message+request_id.
        const apiErr = err as { status?: number; code?: string; message?: string }
        setLastTest({
          id: target.id,
          result: { ok: false, statusCode: apiErr.status, message: apiErr.message },
        })
        toast.error(
          t('notify_targets.test_result.fail', {
            code: apiErr.status ?? '—',
            message: apiErr.message ?? '?',
          }),
        )
      },
    })
  }

  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('nav.notify_targets')}</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            {t('notify_targets.subtitle')}
          </p>
        </div>
        <div className="flex items-center gap-3">
          {list.data && <LarkChip targets={list.data} />}
          <Button onClick={() => setCreateOpen(true)}>{t('notify_targets.create_button')}</Button>
        </div>
      </div>

      <Card className="mt-6">
        <CardHeader>
          <CardTitle>{t('nav.notify_targets')}</CardTitle>
          <CardDescription>{list.data?.length ?? 0}</CardDescription>
        </CardHeader>
        <CardContent>
          {list.isPending ? (
            <Loading />
          ) : list.data && list.data.length > 0 ? (
            <TargetTable
              targets={list.data}
              lastTest={lastTest}
              testingId={test.isPending ? test.variables : undefined}
              onTest={handleTest}
              onEdit={(t) => setEditTarget(t)}
              onDelete={(t) => setDeleteTarget(t)}
            />
          ) : (
            <EmptyState
              icon={Bell}
              title={t('notify_targets.empty_title')}
              description={t('notify_targets.empty_body')}
              action={{
                label: t('notify_targets.create_button'),
                onClick: () => setCreateOpen(true),
              }}
            />
          )}
        </CardContent>
      </Card>

      <CreateNotifyDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
        pending={create.isPending}
      />
      <EditNotifyDialog
        target={editTarget}
        onClose={() => setEditTarget(null)}
        onSubmit={handleEdit}
        pending={update.isPending}
      />
      <DeleteNotifyDialog
        target={deleteTarget}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => deleteTarget && handleDelete.mutate(deleteTarget.id)}
        pending={handleDelete.isPending}
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

// LarkChip shows green "✓ 飞书已接入" near the header when the tenant
// has at least one active, healthy lark-bot target. Disabled or failing
// lark-bot targets fall back to a softer "已配置但已暂停" hint. No chip
// when no lark-bot exists — silence is the right signal for "go set one
// up" (the create button takes them there).
function LarkChip({ targets }: { targets: NotifyTarget[] }) {
  const { t } = useTranslation()
  const larkBots = targets.filter((x) => x.destinationType === 'lark-bot')
  if (larkBots.length === 0) return null
  const healthy = larkBots.find((x) => !x.disabled && !x.lastFailureAt)
  if (healthy) {
    return (
      <span className="inline-flex items-center gap-1 rounded-full border border-primary/30 bg-primary/10 px-2.5 py-1 text-xs font-medium text-primary">
        <CheckCircle2 className="h-3.5 w-3.5" />
        {t('notify_targets.lark_chip_active')}
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-muted bg-muted/30 px-2.5 py-1 text-xs text-muted-foreground">
      {t('notify_targets.lark_chip_disabled')}
    </span>
  )
}

// TargetTable + FailureBadge live in @/features/notify-targets/components/table
