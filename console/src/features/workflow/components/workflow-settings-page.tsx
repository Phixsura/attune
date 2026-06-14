import { useQuery } from '@tanstack/react-query'
import { Archive, Pencil, Plus } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
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
import type { WorkflowTransitionEdge } from '@/proto/attune/v1/workflow'
import { StateFormDialog } from './state-form-dialog'
import { TransitionMatrix } from './transition-matrix'

export function WorkflowSettingsPage() {
  const { t } = useTranslation()
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

  useEffect(() => {
    if (statesQ.isSuccess && items.length === 0 && !autoSeeded.current && !seedDefaults.isPending) {
      autoSeeded.current = true
      seedDefaults.mutate(undefined, {
        onSuccess: () => toast.success(t('workflow.seeded')),
      })
    }
  }, [statesQ.isSuccess, items.length, seedDefaults, t])
  const transitions = transitionsQ.data ?? []
  const active = items.filter((s) => !s.archived)

  const handleCreate = (data: { name: string; color: string; category: string }) => {
    createState.mutate(
      { name: data.name, color: data.color, category: data.category, position: active.length },
      {
        onSuccess: () => {
          setCreateOpen(false)
          toast.success(t('workflow.created'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleUpdate = (data: { name: string; color: string; category: string }) => {
    if (!editState) return
    updateState.mutate(
      { id: editState.id, name: data.name, color: data.color },
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
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">{t('workflow.title')}</h2>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t('workflow.subtitle')}</p>
        </div>
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <Plus className="mr-2 h-4 w-4" />
          {t('workflow.create_button')}
        </Button>
      </div>

      {items.length === 0 ? (
        <EmptyState title={t('workflow.empty.title')} description={t('workflow.empty.body')} />
      ) : (
        <>
          <Card className="gap-0 overflow-hidden py-0">
            <CardHeader className="py-4">
              <CardTitle>{t('workflow.states_title')}</CardTitle>
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
                        <WorkflowStateBadge name={s.name} color={s.color} category={s.category} />
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
            <Card>
              <CardHeader>
                <CardTitle>{t('workflow.transitions_title')}</CardTitle>
                <CardDescription>{t('workflow.transitions_subtitle')}</CardDescription>
              </CardHeader>
              <CardContent>
                <TransitionMatrix
                  states={items}
                  transitions={transitions}
                  saving={replaceTransitions.isPending}
                  onSave={handleSaveTransitions}
                />
              </CardContent>
            </Card>
          )}
        </>
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
              {t('workflow.archive_confirm', { name: archiveTarget?.name })}
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
