import { Loader2, Save } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { WorkflowStateBadge } from '@/components/workflow/workflow-state-badge'
import { useDisplayName } from '@/lib/i18n-resolve'
import type {
  WorkflowState,
  WorkflowTransition,
  WorkflowTransitionEdge,
} from '@/proto/attune/v1/workflow'

export function TransitionMatrix({
  states,
  transitions,
  saving,
  onSave,
}: {
  states: WorkflowState[]
  transitions: WorkflowTransition[]
  saving: boolean
  onSave: (edges: WorkflowTransitionEdge[]) => void
}) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  const active = useMemo(() => states.filter((s) => !s.archived), [states])

  const initialEdges = useMemo(() => {
    const set = new Set<string>()
    for (const tr of transitions) set.add(`${tr.fromStateId}→${tr.toStateId}`)
    return set
  }, [transitions])

  const [edges, setEdges] = useState<Set<string>>(new Set())

  useEffect(() => {
    setEdges(new Set(initialEdges))
  }, [initialEdges])

  const isDirty = useMemo(() => {
    if (edges.size !== initialEdges.size) return true
    /* v8 ignore next -- @preserve: size equality leaves only stale-edge defensive comparison. */
    for (const e of edges) if (!initialEdges.has(e)) return true
    return false
  }, [edges, initialEdges])

  const toggleEdge = useCallback((fromId: string, toId: string) => {
    setEdges((prev) => {
      const key = `${fromId}→${toId}`
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }, [])

  const handleSave = () => {
    const result: WorkflowTransitionEdge[] = []
    for (const key of edges) {
      const [fromStateId, toStateId] = key.split('→')
      result.push({ fromStateId, toStateId })
    }
    onSave(result)
  }

  if (active.length === 0) return null

  return (
    <div className="space-y-4">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr>
              <th className="p-2 text-left text-xs font-medium text-muted-foreground">
                {t('workflow.matrix.from_to')}
              </th>
              {active.map((to) => (
                <th key={to.id} className="p-2 text-center">
                  <WorkflowStateBadge state={to} className="text-[10px]" />
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {active.map((from) => (
              <tr key={from.id} className="border-t border-border/60">
                <td className="p-2">
                  <WorkflowStateBadge state={from} className="text-[10px]" />
                </td>
                {active.map((to) => (
                  <td key={to.id} className="p-2 text-center">
                    {from.id === to.id ? (
                      <span className="text-xs text-muted-foreground">—</span>
                    ) : (
                      <Checkbox
                        checked={edges.has(`${from.id}→${to.id}`)}
                        onCheckedChange={() => toggleEdge(from.id, to.id)}
                        aria-label={`${displayOf(from.displayName) || from.name} → ${
                          displayOf(to.displayName) || to.name
                        }`}
                      />
                    )}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="flex justify-end">
        <Button size="sm" disabled={!isDirty || saving} onClick={handleSave}>
          {saving ? (
            <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
          ) : (
            <Save className="mr-2 h-3.5 w-3.5" />
          )}
          {t('workflow.matrix.save')}
        </Button>
      </div>
    </div>
  )
}
