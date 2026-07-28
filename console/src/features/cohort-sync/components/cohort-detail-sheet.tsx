import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Loader2, Pencil, Save } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Loading } from '@/components/loading'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  type Cohort,
  type CohortMembership,
  type CohortSyncRun,
  listCohortMembersQuery,
  listCohortSyncRunsQuery,
  updateCohort,
} from '../api/cohort-sync'

export function CohortDetailSheet({
  cohort,
  open,
  onOpenChange,
}: {
  cohort: Cohort
  open: boolean
  onOpenChange: (v: boolean) => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const membersQ = useQuery(listCohortMembersQuery(cohort.id))
  const runsQ = useQuery(listCohortSyncRunsQuery(cohort.id))

  const [editing, setEditing] = useState(false)
  const [editName, setEditName] = useState(cohort.name)
  const [editTTL, setEditTTL] = useState(String(cohort.staleTtlDays))

  const saveMutation = useMutation({
    mutationFn: () =>
      updateCohort(cohort.id, {
        name: editName,
        staleTtlDays: Number.parseInt(editTTL, 10) || cohort.staleTtlDays,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cohort-sync'] })
      toast.success(t('common.save'))
      setEditing(false)
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
  })

  const toggleMutation = useMutation({
    mutationFn: () => updateCohort(cohort.id, { enabled: !cohort.enabled }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cohort-sync'] })
      toast.success(t('common.save'))
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
  })

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-[480px] overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{cohort.name}</SheetTitle>
          <SheetDescription>
            {cohort.externalCohortId}
            {cohort.sourceProvider && ` · ${cohort.sourceProvider}`}
            {cohort.sourceName && ` (${cohort.sourceName})`}
          </SheetDescription>
        </SheetHeader>

        <div className="mt-4 grid grid-cols-3 gap-4 text-center">
          <Stat label={t('cohort_sync.cohort.members')} value={String(cohort.memberCount)} />
          <Stat label={t('cohort_sync.cohort.ttl_days')} value={String(cohort.staleTtlDays)} />
          <Stat
            label={t('cohort_sync.source.last_sync')}
            value={
              cohort.lastSyncedAt
                ? formatDistanceToNow(new Date(cohort.lastSyncedAt), {
                    addSuffix: true,
                    locale: zhCN,
                  })
                : t('common.never')
            }
          />
        </div>

        {/* Edit controls */}
        <section className="mt-4 flex items-center gap-2">
          {editing ? (
            <form
              className="flex flex-1 items-end gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                saveMutation.mutate()
              }}
            >
              <div className="flex-1 space-y-1">
                <Label htmlFor="edit-cohort-name">{t('cohort_sync.source.name')}</Label>
                <Input
                  id="edit-cohort-name"
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  disabled={saveMutation.isPending}
                />
              </div>
              <div className="w-20 space-y-1">
                <Label htmlFor="edit-cohort-ttl">{t('cohort_sync.cohort.ttl_days')}</Label>
                <Input
                  id="edit-cohort-ttl"
                  type="number"
                  min={1}
                  max={365}
                  value={editTTL}
                  onChange={(e) => setEditTTL(e.target.value)}
                  disabled={saveMutation.isPending}
                />
              </div>
              <Button type="submit" size="sm" disabled={saveMutation.isPending}>
                {saveMutation.isPending ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Save className="h-3.5 w-3.5" />
                )}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => setEditing(false)}
                disabled={saveMutation.isPending}
              >
                {t('common.cancel')}
              </Button>
            </form>
          ) : (
            <>
              <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
                <Pencil className="mr-1 h-3.5 w-3.5" />
                {t('common.edit')}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => toggleMutation.mutate()}
                disabled={toggleMutation.isPending}
              >
                {cohort.enabled
                  ? t('cohort_sync.source.status.disabled')
                  : t('cohort_sync.source.enabled')}
              </Button>
            </>
          )}
        </section>

        {/* Members preview */}
        <section className="mt-6">
          <h3 className="mb-2 text-sm font-medium">{t('cohort_sync.cohort.members')}</h3>
          {membersQ.isLoading ? <Loading /> : <MembersTable members={membersQ.data ?? []} />}
        </section>

        {/* Sync run history */}
        <section className="mt-6">
          <h3 className="mb-2 text-sm font-medium">{t('cohort_sync.cohort.runs')}</h3>
          {runsQ.isLoading ? <Loading /> : <RunsTable runs={runsQ.data ?? []} />}
        </section>
      </SheetContent>
    </Sheet>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border px-3 py-2">
      <div className="text-lg font-semibold">{value}</div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  )
}

function MembersTable({ members }: { members: CohortMembership[] }) {
  const { t } = useTranslation()
  if (members.length === 0) {
    return <p className="text-sm text-muted-foreground">{t('cohort_sync.cohort.no_cohorts')}</p>
  }
  return (
    <div className="max-h-60 overflow-y-auto rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>Email</TableHead>
            <TableHead>{t('cohort_sync.source.last_sync')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {members.map((m) => (
            <TableRow key={m.id}>
              <TableCell className="font-mono text-xs">{m.externalUserId}</TableCell>
              <TableCell className="text-xs">{m.email || '—'}</TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {m.lastSeenAt
                  ? formatDistanceToNow(new Date(m.lastSeenAt), { addSuffix: true, locale: zhCN })
                  : '—'}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function RunsTable({ runs }: { runs: CohortSyncRun[] }) {
  const { t } = useTranslation()
  if (runs.length === 0) {
    return <p className="text-sm text-muted-foreground">{t('cohort_sync.cohort.no_cohorts')}</p>
  }
  return (
    <div className="max-h-60 overflow-y-auto rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('cohort_sync.run.trigger.webhook')}</TableHead>
            <TableHead>{t('cohort_sync.run.status.succeeded')}</TableHead>
            <TableHead>+/-</TableHead>
            <TableHead>{t('cohort_sync.run.error')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {runs.map((run) => (
            <TableRow key={run.id}>
              <TableCell className="text-xs">
                <div>
                  {run.trigger === 'manual'
                    ? t('cohort_sync.run.trigger.manual')
                    : t('cohort_sync.run.trigger.webhook')}
                </div>
                <div className="text-muted-foreground">
                  {run.startedAt
                    ? formatDistanceToNow(new Date(run.startedAt), {
                        addSuffix: true,
                        locale: zhCN,
                      })
                    : '—'}
                </div>
              </TableCell>
              <TableCell>
                <RunStatusBadge status={run.status} />
              </TableCell>
              <TableCell className="font-mono text-xs">
                <span className="text-green-600">+{run.membersAdded}</span>{' '}
                <span className="text-red-600">-{run.membersRemoved}</span>
              </TableCell>
              <TableCell
                className="max-w-[10rem] truncate text-xs text-destructive"
                title={run.errorMessage}
              >
                {run.errorMessage || '—'}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function RunStatusBadge({ status }: { status: string }) {
  const { t } = useTranslation()
  const map: Record<string, { cls: string; key: string }> = {
    succeeded: {
      cls: 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400',
      key: 'cohort_sync.run.status.succeeded',
    },
    failed: { cls: 'bg-destructive/10 text-destructive', key: 'cohort_sync.run.status.failed' },
    skipped: { cls: 'bg-muted text-muted-foreground', key: 'cohort_sync.run.status.skipped' },
    running: {
      cls: 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
      key: 'cohort_sync.run.status.running',
    },
  }
  const entry = map[status] ?? { cls: 'bg-muted text-muted-foreground', key: status }
  return <span className={`rounded-full px-2 py-0.5 text-xs ${entry.cls}`}>{t(entry.key)}</span>
}
