import { useMutation, useQuery } from '@tanstack/react-query'
import { Bell } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
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

export function NotifyTargetsPage() {
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
  const targets = list.data ?? []
  const healthyCount = targets.filter((target) => !target.disabled && !target.lastFailureAt).length
  const failingCount = targets.filter((target) => Boolean(target.lastFailureAt)).length
  const disabledCount = targets.filter((target) => target.disabled).length

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
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.integrations')}
        title={t('nav.notify_targets')}
        subtitle={t('notify_targets.subtitle')}
        actions={
          <Button onClick={() => setCreateOpen(true)}>{t('notify_targets.create_button')}</Button>
        }
        metrics={
          <>
            <PageHeroMetric
              label={t('notify_targets.summary.total')}
              value={String(targets.length)}
              hint={t('notify_targets.summary.total_hint')}
            />
            <PageHeroMetric
              label={t('notify_targets.summary.healthy')}
              value={String(healthyCount)}
              hint={t('notify_targets.summary.healthy_hint')}
            />
            <PageHeroMetric
              label={t('notify_targets.summary.failing')}
              value={String(failingCount)}
              hint={t('notify_targets.summary.failing_hint')}
              tone={failingCount > 0 ? 'urgent' : 'default'}
            />
            <PageHeroMetric
              label={t('notify_targets.summary.disabled')}
              value={String(disabledCount)}
              hint={t('notify_targets.summary.disabled_hint')}
            />
          </>
        }
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('notify_targets.registry_title')}</CardTitle>
            <CardDescription>
              {t('notify_targets.registry_description', { count: targets.length })}
            </CardDescription>
          </CardHeader>
          <CardContent className="pt-6">
            {list.isPending ? (
              <Loading />
            ) : targets.length > 0 ? (
              <TargetTable
                targets={targets}
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

        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('notify_targets.playbook_title')}</CardTitle>
            <CardDescription>{t('notify_targets.playbook_description')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 pt-6">
            <PlaybookRow
              title={t('notify_targets.playbook.scope_title')}
              body={t('notify_targets.playbook.scope_body')}
            />
            <PlaybookRow
              title={t('notify_targets.playbook.health_title')}
              body={t('notify_targets.playbook.health_body')}
            />
            <PlaybookRow
              title={t('notify_targets.playbook.test_title')}
              body={t('notify_targets.playbook.test_body')}
            />
          </CardContent>
        </Card>
      </div>

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

function PlaybookRow({ title, body }: { title: string; body: string }) {
  return (
    <div className="rounded-[1rem] border border-border/70 bg-background/85 px-4 py-3.5">
      <div className="text-sm font-semibold text-foreground">{title}</div>
      <div className="mt-1 text-sm leading-6 text-muted-foreground">{body}</div>
    </div>
  )
}
