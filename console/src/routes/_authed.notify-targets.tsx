import { useMutation, useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Bell } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
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
