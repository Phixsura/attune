import { useQuery } from '@tanstack/react-query'
import { Archive, Pencil, Plus } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { WorkflowStateBadge } from '@/components/workflow/workflow-state-badge'
import { useArchiveState } from '@/features/workflow/api/archive-state'
import { useCreateState } from '@/features/workflow/api/create-state'
import { type WorkflowState, workflowStatesQuery } from '@/features/workflow/api/list-states'
import { workflowTransitionsQuery } from '@/features/workflow/api/list-transitions'
import { useReplaceTransitions } from '@/features/workflow/api/replace-transitions'
import { useSeedDefaults } from '@/features/workflow/api/seed-defaults'
import { useUpdateState } from '@/features/workflow/api/update-state'
import { useDisplayName } from '@/lib/i18n-resolve'
import type { WorkflowTransitionEdge } from '@/proto/attune/v1/workflow'
import { type StateFormData, StateFormDialog } from './state-form-dialog'
import { TransitionMatrix } from './transition-matrix'

export function WorkflowSettingsPage() {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  const statesQ = useQuery(workflowStatesQuery())
  const transitionsQ = useQuery(workflowTransitionsQuery())
  const createState = useCreateState()
  const updateState = useUpdateState()
  const archiveState = useArchiveState()
  const replaceTransitions = useReplaceTransitions()
  const seedDefaults = useSeedDefaults()

  const [createOpen, setCreateOpen] = useState(false)
  const [editState, setEditState] = useState<WorkflowState | undefined>()
  const [archiveTarget, setArchiveTarget] = useState<WorkflowState | undefined>()
  const autoSeeded = useRef(false)

  const items = statesQ.data ?? []
  const archivedCount = items.filter((s) => s.archived).length
  const defaultCount = items.filter((s) => s.isDefault).length
  const transitionCount = transitionsQ.data?.length ?? 0

  useEffect(() => {
    if (statesQ.isSuccess && items.length === 0 && !autoSeeded.current && !seedDefaults.isPending) {
      autoSeeded.current = true
      seedDefaults.mutate(undefined, {
        onSuccess: () => toast.success(t('workflow.seeded')),
      })
    }
  }, [statesQ.isSuccess, items.length, seedDefaults.isPending, seedDefaults.mutate, t])
  const transitions = transitionsQ.data ?? []
  const active = items.filter((s) => !s.archived)

  const handleCreate = (data: StateFormData) => {
    createState.mutate(
      {
        name: data.name,
        displayName: { entries: data.displayName },
        color: data.color,
        category: data.category,
        position: active.length,
      },
      {
        onSuccess: () => {
          setCreateOpen(false)
          toast.success(t('workflow.created'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleUpdate = (data: StateFormData) => {
    if (!editState) return
    // The machine key (name) is immutable — send displayName to relabel.
    updateState.mutate(
      { id: editState.id, displayName: { entries: data.displayName }, color: data.color },
      {
        onSuccess: () => {
          setEditState(undefined)
          toast.success(t('settings.saved'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleArchiveConfirm = () => {
    if (!archiveTarget) return
    archiveState.mutate(archiveTarget.id, {
      onSuccess: () => {
        setArchiveTarget(undefined)
        toast.success(t('workflow.archived'))
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
    })
  }

  const handleSaveTransitions = (edges: WorkflowTransitionEdge[]) => {
    replaceTransitions.mutate(
      { transitions: edges },
      {
        onSuccess: () => toast.success(t('workflow.transitions_saved')),
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  return (
    <section className="min-w-0 space-y-6">
      <PageHero
        eyebrow={t('shell.groups.configuration')}
        title={t('workflow.title')}
        subtitle={t('workflow.subtitle')}
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            {t('workflow.create_button')}
          </Button>
        }
        metrics={
          <>
            <PageHeroMetric
              label={t('workflow.summary.total')}
              value={String(items.length)}
              hint={t('workflow.summary.total_hint')}
            />
            <PageHeroMetric
              label={t('workflow.summary.active')}
              value={String(active.length)}
              hint={t('workflow.summary.active_hint')}
            />
            <PageHeroMetric
              label={t('workflow.summary.transitions')}
              value={String(transitionCount)}
              hint={t('workflow.summary.transitions_hint')}
            />
            <PageHeroMetric
              label={t('workflow.summary.archived')}
              value={String(archivedCount)}
              hint={
                defaultCount > 0
                  ? t('workflow.summary.archived_hint')
                  : t('workflow.summary.default_missing')
              }
            />
          </>
        }
      />

      {items.length === 0 ? (
        <EmptyState title={t('workflow.empty.title')} description={t('workflow.empty.body')} />
      ) : (
        <div className="grid gap-6 xl:grid-cols-[minmax(0,1.15fr)_minmax(20rem,0.85fr)]">
          <div className="space-y-6">
            <Card className="gap-0 overflow-hidden border-border/60 py-0 shadow-none">
              <CardHeader>
                <CardTitle className="text-base">{t('workflow.states_title')}</CardTitle>
                <CardDescription>{t('workflow.states_subtitle')}</CardDescription>
              </CardHeader>
              <CardContent className="px-0 pb-0">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('workflow.table.name')}</TableHead>
                      <TableHead>{t('workflow.table.category')}</TableHead>
                      <TableHead className="text-center">{t('workflow.table.default')}</TableHead>
                      <TableHead className="w-24" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map((s) => (
                      <TableRow key={s.id} className={s.archived ? 'opacity-50' : ''}>
                        <TableCell>
                          <WorkflowStateBadge state={s} />
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {t(`workflow.categories.${s.category}`)}
                        </TableCell>
                        <TableCell className="text-center text-xs text-muted-foreground">
                          {s.isDefault ? '✓' : '—'}
                        </TableCell>
                        <TableCell>
                          {!s.archived && (
                            <div className="flex justify-end gap-1">
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-7 w-7"
                                aria-label={t('workflow.edit_label')}
                                onClick={() => setEditState(s)}
                              >
                                <Pencil className="h-3.5 w-3.5" />
                              </Button>
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-7 w-7 text-muted-foreground hover:text-destructive"
                                aria-label={t('workflow.archive_label')}
                                onClick={() => setArchiveTarget(s)}
                              >
                                <Archive className="h-3.5 w-3.5" />
                              </Button>
                            </div>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>

            {active.length >= 2 && (
              <Card className="border-border/60 shadow-none">
                <CardHeader>
                  <CardTitle className="text-base">{t('workflow.transitions_title')}</CardTitle>
                  <CardDescription>{t('workflow.transitions_subtitle')}</CardDescription>
                </CardHeader>
                <CardContent className="pt-6">
                  <TransitionMatrix
                    states={items}
                    transitions={transitions}
                    saving={replaceTransitions.isPending}
                    onSave={handleSaveTransitions}
                  />
                </CardContent>
              </Card>
            )}
          </div>

          <Card className="border-border/60 shadow-none">
            <CardHeader>
              <CardTitle className="text-base">{t('workflow.playbook_title')}</CardTitle>
              <CardDescription>{t('workflow.playbook_description')}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 pt-6">
              <WorkflowPlaybookRow
                index="1"
                title={t('workflow.playbook.open_title')}
                body={t('workflow.playbook.open_body')}
              />
              <WorkflowPlaybookRow
                index="2"
                title={t('workflow.playbook.active_title')}
                body={t('workflow.playbook.active_body')}
              />
              <WorkflowPlaybookRow
                index="3"
                title={t('workflow.playbook.closed_title')}
                body={t('workflow.playbook.closed_body')}
              />
            </CardContent>
          </Card>
        </div>
      )}

      <StateFormDialog
        open={createOpen}
        pending={createState.isPending}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
      />

      <StateFormDialog
        open={!!editState}
        state={editState}
        pending={updateState.isPending}
        onOpenChange={(v) => {
          if (!v) setEditState(undefined)
        }}
        onSubmit={handleUpdate}
      />

      <Dialog
        open={!!archiveTarget}
        onOpenChange={(v) => {
          if (!v) setArchiveTarget(undefined)
        }}
      >
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t('workflow.archive_label')}</DialogTitle>
            <DialogDescription>
              {t('workflow.archive_confirm', {
                name: archiveTarget
                  ? displayOf(archiveTarget.displayName) || archiveTarget.name
                  : '',
              })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setArchiveTarget(undefined)}>
              {t('common.cancel')}
            </Button>
            <Button variant="destructive" onClick={handleArchiveConfirm}>
              {t('common.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}

function WorkflowPlaybookRow({
  index,
  title,
  body,
}: {
  index: string
  title: string
  body: string
}) {
  return (
    <div className="rounded-[1rem] border border-border/60 bg-background/85 px-4 py-3.5">
      <div className="flex items-start gap-3">
        <div className="flex size-7 shrink-0 items-center justify-center rounded-full bg-foreground text-xs font-semibold text-background">
          {index}
        </div>
        <div>
          <div className="text-sm font-semibold text-foreground">{title}</div>
          <div className="mt-1 text-sm leading-6 text-muted-foreground">{body}</div>
        </div>
      </div>
    </div>
  )
}
