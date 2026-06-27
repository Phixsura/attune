import { useQuery } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import type { TFunction } from 'i18next'
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CircleHelp,
  DatabaseBackup,
  Gauge,
  Loader2,
  RefreshCcw,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  TimerReset,
} from 'lucide-react'
import { type ReactNode, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { UnsavedChangesDialog } from '@/components/unsaved-changes-dialog'
import {
  type EnrichmentRuntimeHistoryEntryView,
  type EnrichmentRuntimeInstanceView,
  type EnrichmentRuntimeSpecView,
  type EnrichmentRuntimeView,
  enrichmentRuntimeQuery,
  type RuntimeResetField,
  runtimeStepUpQuery,
  useResetEnrichmentRuntime,
  useRollbackEnrichmentRuntime,
  useUpdateEnrichmentRuntime,
  useVerifyRuntimeStepUp,
} from '@/features/settings/api/enrichment-runtime'
import { readDraft, useDraftGuard } from '@/hooks/use-draft-guard'

const RESET_FIELDS: Array<{ field: RuntimeResetField; labelKey: string }> = [
  {
    field: 'queue_len',
    labelKey: 'settings.enrichment_runtime.fields.queue_len',
  },
  { field: 'workers', labelKey: 'settings.enrichment_runtime.fields.workers' },
  {
    field: 'batch_size',
    labelKey: 'settings.enrichment_runtime.fields.batch_size',
  },
  {
    field: 'batch_window',
    labelKey: 'settings.enrichment_runtime.fields.batch_window',
  },
  {
    field: 'sweep_interval',
    labelKey: 'settings.enrichment_runtime.fields.sweep_interval',
  },
  {
    field: 'llm_rate_limit_enabled',
    labelKey: 'settings.enrichment_runtime.fields.llm_rate_limit_enabled',
  },
  {
    field: 'llm_max_qps',
    labelKey: 'settings.enrichment_runtime.fields.llm_max_qps',
  },
  {
    field: 'llm_burst',
    labelKey: 'settings.enrichment_runtime.fields.llm_burst',
  },
]

interface DraftState {
  queueLen: string
  workers: string
  batchSize: string
  batchWindowSeconds: string
  sweepIntervalSeconds: string
  llmRateLimitEnabled: boolean
  llmMaxQps: string
  llmBurst: string
}

type RuntimeValidationErrorKey =
  | 'settings.enrichment_runtime.errors.queue_len_positive'
  | 'settings.enrichment_runtime.errors.workers_positive'
  | 'settings.enrichment_runtime.errors.batch_size_positive'
  | 'settings.enrichment_runtime.errors.batch_window_positive'
  | 'settings.enrichment_runtime.errors.sweep_interval_positive'
  | 'settings.enrichment_runtime.errors.workers_lte_queue'
  | 'settings.enrichment_runtime.errors.batch_size_lte_queue'
  | 'settings.enrichment_runtime.errors.llm_max_qps_non_negative'
  | 'settings.enrichment_runtime.errors.llm_burst_non_negative'
  | 'settings.enrichment_runtime.errors.llm_max_qps_required'
  | 'settings.enrichment_runtime.errors.llm_burst_required'

export function shouldHydrateRuntimeDraft(args: {
  desiredVersion?: string
  lastHydratedVersion?: string
  dirty: boolean
}) {
  const { desiredVersion, lastHydratedVersion, dirty } = args
  if (!desiredVersion) return false
  if (desiredVersion !== lastHydratedVersion) return true
  return !dirty
}

export function validateRuntimeSpec(
  spec: EnrichmentRuntimeSpecView,
): RuntimeValidationErrorKey | null {
  if (spec.queueLen <= 0) return 'settings.enrichment_runtime.errors.queue_len_positive'
  if (spec.workers <= 0) return 'settings.enrichment_runtime.errors.workers_positive'
  if (spec.batchSize <= 0) return 'settings.enrichment_runtime.errors.batch_size_positive'
  if (spec.batchWindowSeconds <= 0) {
    return 'settings.enrichment_runtime.errors.batch_window_positive'
  }
  if (spec.sweepIntervalSeconds <= 0) {
    return 'settings.enrichment_runtime.errors.sweep_interval_positive'
  }
  if (spec.workers > spec.queueLen) {
    return 'settings.enrichment_runtime.errors.workers_lte_queue'
  }
  if (spec.batchSize > spec.queueLen) {
    return 'settings.enrichment_runtime.errors.batch_size_lte_queue'
  }
  if (spec.llmMaxQps < 0) {
    return 'settings.enrichment_runtime.errors.llm_max_qps_non_negative'
  }
  if (spec.llmBurst < 0) {
    return 'settings.enrichment_runtime.errors.llm_burst_non_negative'
  }
  if (spec.llmRateLimitEnabled && spec.llmMaxQps <= 0) {
    return 'settings.enrichment_runtime.errors.llm_max_qps_required'
  }
  if (spec.llmRateLimitEnabled && spec.llmBurst < 1) {
    return 'settings.enrichment_runtime.errors.llm_burst_required'
  }
  return null
}

export function findLastKnownGoodEntry(
  runtime?: EnrichmentRuntimeView,
): EnrichmentRuntimeHistoryEntryView | null {
  if (!runtime) return null
  const targetVersion = runtime.desiredRevision.lastKnownGoodVersion
  if (!targetVersion || targetVersion === runtime.desiredRevision.version) {
    return null
  }
  return runtime.history.find((entry) => entry.revision.version === targetVersion) ?? null
}

export function buildSpecDiffRows(
  current: EnrichmentRuntimeSpecView,
  next: EnrichmentRuntimeSpecView,
) {
  return [
    {
      key: 'queueLen',
      label: 'Queue',
      current: String(current.queueLen),
      next: String(next.queueLen),
    },
    {
      key: 'workers',
      label: 'Workers',
      current: String(current.workers),
      next: String(next.workers),
    },
    {
      key: 'batchSize',
      label: 'Batch',
      current: String(current.batchSize),
      next: String(next.batchSize),
    },
    {
      key: 'batchWindowSeconds',
      label: 'Batch Window',
      current: `${current.batchWindowSeconds}s`,
      next: `${next.batchWindowSeconds}s`,
    },
    {
      key: 'sweepIntervalSeconds',
      label: 'Sweep',
      current: `${current.sweepIntervalSeconds}s`,
      next: `${next.sweepIntervalSeconds}s`,
    },
    {
      key: 'llmRateLimitEnabled',
      label: 'Limiter',
      current: current.llmRateLimitEnabled ? 'on' : 'off',
      next: next.llmRateLimitEnabled ? 'on' : 'off',
    },
    {
      key: 'llmMaxQps',
      label: 'Max QPS',
      current: String(current.llmMaxQps),
      next: String(next.llmMaxQps),
    },
    {
      key: 'llmBurst',
      label: 'Burst',
      current: String(current.llmBurst),
      next: String(next.llmBurst),
    },
  ].filter((row) => row.current !== row.next)
}

export function buildRuntimeConditions(runtime: EnrichmentRuntimeView) {
  const conditions: Array<{
    tone: 'good' | 'warn' | 'bad'
    label: string
  }> = []

  conditions.push({
    tone: runtime.summary.fullyConverged ? 'good' : 'warn',
    label: runtime.summary.fullyConverged ? 'Converged' : 'Reconciling',
  })

  if (runtime.summary.degradedInstances > 0) {
    conditions.push({
      tone: 'bad',
      label: `Degraded ${runtime.summary.degradedInstances}`,
    })
  }
  if (runtime.summary.staleInstances > 0) {
    conditions.push({
      tone: 'warn',
      label: `Stale ${runtime.summary.staleInstances}`,
    })
  }
  if (runtime.summary.expiredInstances > 0) {
    conditions.push({
      tone: 'bad',
      label: `Expired ${runtime.summary.expiredInstances}`,
    })
  }
  if (runtime.summary.liveInstances > runtime.summary.fullyAppliedInstances) {
    conditions.push({
      tone: 'warn',
      label: `Applied ${runtime.summary.fullyAppliedInstances}/${runtime.summary.liveInstances}`,
    })
  }
  if (runtime.desiredSpec.llmRateLimitEnabled) {
    conditions.push({
      tone: runtime.summary.liveInstances > 1 ? 'warn' : 'good',
      label: runtime.summary.liveInstances > 1 ? 'Local limiter mode' : 'Limiter active',
    })
  }

  return conditions
}

export function buildInstanceConditions(item: EnrichmentRuntimeInstanceView) {
  const conditions: Array<{
    tone: 'good' | 'warn' | 'bad'
    label: string
  }> = []

  if (item.runnerApplyStatus === 'applied' && item.limiterApplyStatus === 'applied') {
    conditions.push({ tone: 'good', label: 'Applied' })
  }
  if (item.runnerApplyStatus === 'applying' || item.limiterApplyStatus === 'applying') {
    conditions.push({ tone: 'warn', label: 'Reconciling' })
  }
  if (item.queueResizePending) {
    conditions.push({ tone: 'warn', label: 'Pending resize' })
  }
  if (
    item.runnerApplyStatus === 'failed' ||
    item.limiterApplyStatus === 'failed' ||
    item.degradedReason
  ) {
    conditions.push({ tone: 'bad', label: 'Degraded' })
  }
  if (item.observedDesiredVersion !== item.desiredVersion) {
    conditions.push({ tone: 'warn', label: 'Observed lag' })
  }

  return conditions
}

export function buildInstanceDisplayName(item: EnrichmentRuntimeInstanceView, index: number) {
  const raw = item.instanceId?.trim() ?? ''
  if (!raw) {
    return { primary: `运行节点 ${index + 1}`, secondary: '' }
  }

  const localHostMatch = raw.match(/^([a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)*)\.local$/)
  if (localHostMatch) {
    return { primary: localHostMatch[1], secondary: raw }
  }

  const genericHostMatch = raw.match(/^([a-zA-Z0-9_-]+)\.[a-zA-Z0-9.-]+$/)
  if (genericHostMatch && genericHostMatch[1].length <= 24) {
    return { primary: genericHostMatch[1], secondary: raw }
  }

  if (raw.startsWith('attune-') || looksOpaqueId(raw)) {
    return { primary: `运行节点 ${index + 1}`, secondary: raw }
  }

  return { primary: raw, secondary: raw }
}

export function formatRuntimeActorLabel(value?: string) {
  const raw = value?.trim()
  if (!raw) return '-'
  if (raw.includes('@')) return raw
  if (looksOpaqueId(raw)) return '控制台管理员'
  return raw
}

export function partitionRuntimeInstances(
  instances: EnrichmentRuntimeInstanceView[],
  liveInstances: number,
) {
  const sorted = [...instances].sort((left, right) => {
    const leftAt = left.lastSeenAt ? new Date(left.lastSeenAt).getTime() : 0
    const rightAt = right.lastSeenAt ? new Date(right.lastSeenAt).getTime() : 0
    return rightAt - leftAt
  })

  const activeCount = Math.max(0, Math.min(liveInstances, sorted.length))
  return {
    active: sorted.slice(0, activeCount),
    historical: sorted.slice(activeCount),
  }
}

export function buildChangeImpactNotes(
  rows: Array<{ key: string; current: string; next: string }>,
) {
  const notes: string[] = []
  for (const row of rows) {
    switch (row.key) {
      case 'queueLen':
        notes.push('队列容量变化会直接影响可吸收的输入波峰，以及触发背压的时机。')
        break
      case 'workers':
        notes.push('工作协程变化会直接改变并行吞吐，也会同步放大或收紧对下游依赖的压力。')
        break
      case 'batchSize':
      case 'batchWindowSeconds':
        notes.push('批处理策略变化会在吞吐效率和单条反馈等待时间之间重新取舍。')
        break
      case 'sweepIntervalSeconds':
        notes.push('扫队列间隔变化会改变 backlog 恢复节奏，过短更积极，过长更保守。')
        break
      case 'llmRateLimitEnabled':
      case 'llmMaxQps':
      case 'llmBurst':
        notes.push('LLM 限流策略变化会直接影响模型调用压力、成本和短时流量释放能力。')
        break
      default:
        break
    }
  }
  return [...new Set(notes)]
}

export function EnrichmentRuntimePage({ canEdit }: { canEdit: boolean }) {
  const { t } = useTranslation()
  const runtimeQuery = useQuery(enrichmentRuntimeQuery())
  const operationsQuery = useQuery(runtimeStepUpQuery())
  const updateMutation = useUpdateEnrichmentRuntime()
  const resetMutation = useResetEnrichmentRuntime()
  const rollbackMutation = useRollbackEnrichmentRuntime()
  const verifyStepUpMutation = useVerifyRuntimeStepUp()

  const runtime = runtimeQuery.data
  const desiredSpec = runtime?.desiredSpec
  const desiredVersion = runtime?.desiredRevision.version ?? '0'
  const stepUp = operationsQuery.data?.stepUp
  const stepUpSatisfied = stepUp?.satisfied ?? false

  const [draft, setDraft] = useState<DraftState>(
    () => readDraft<DraftState>('enrichment-runtime') ?? emptyDraft(),
  )
  const [lastHydratedVersion, setLastHydratedVersion] = useState<string>()
  const [updateReason, setUpdateReason] = useState('')
  const [stepUpOpen, setStepUpOpen] = useState(false)
  const [stepUpPassword, setStepUpPassword] = useState('')
  const [resetOpen, setResetOpen] = useState(false)
  const [resetReason, setResetReason] = useState('')
  const [resetAll, setResetAll] = useState(false)
  const [resetFields, setResetFields] = useState<RuntimeResetField[]>([])
  const [rollbackTarget, setRollbackTarget] = useState<EnrichmentRuntimeHistoryEntryView | null>(
    null,
  )
  const [rollbackReason, setRollbackReason] = useState('')
  const [historicalExpanded, setHistoricalExpanded] = useState(false)

  const parsedSpec = useMemo(() => draftToSpec(draft), [draft])
  const validationError = useMemo(
    () =>
      parsedSpec.ok
        ? translateRuntimeValidationError(validateRuntimeSpec(parsedSpec.value), t)
        : t('settings.enrichment_runtime.errors.invalid_number'),
    [parsedSpec, t],
  )
  const dirty = useMemo(() => {
    if (!desiredSpec || !parsedSpec.ok) return false
    return JSON.stringify(parsedSpec.value) !== JSON.stringify(desiredSpec)
  }, [desiredSpec, parsedSpec])

  const guard = useDraftGuard({
    storageKey: 'enrichment-runtime',
    draft,
    dirty,
    disabled: !canEdit,
  })

  const lastKnownGoodEntry = useMemo(() => findLastKnownGoodEntry(runtime), [runtime])
  const runtimeConditions = useMemo(
    () => (runtime ? buildRuntimeConditions(runtime) : []),
    [runtime],
  )
  const currentVsBootstrapDiff = useMemo(() => {
    if (!runtime) return []
    return buildSpecDiffRows(runtime.bootstrapDefault, runtime.desiredSpec)
  }, [runtime])
  const draftVsDesiredDiff = useMemo(() => {
    if (!desiredSpec || !parsedSpec.ok) return []
    return buildSpecDiffRows(desiredSpec, parsedSpec.value)
  }, [desiredSpec, parsedSpec])
  const changeImpactNotes = useMemo(
    () => buildChangeImpactNotes(draftVsDesiredDiff),
    [draftVsDesiredDiff],
  )
  const partitionedInstances = useMemo(() => {
    if (!runtime) {
      return { active: [], historical: [] }
    }
    return partitionRuntimeInstances(runtime.instances, runtime.summary.liveInstances)
  }, [runtime])
  const runtimePosture = useMemo(() => {
    if (!runtime) return []
    return buildRuntimePosture(runtime.desiredSpec, runtime.summary.liveInstances)
  }, [runtime])

  useEffect(() => {
    if (!desiredSpec) return
    if (
      !shouldHydrateRuntimeDraft({
        desiredVersion,
        lastHydratedVersion,
        dirty,
      })
    ) {
      return
    }
    setDraft(specToDraft(desiredSpec))
    setLastHydratedVersion(desiredVersion)
  }, [desiredSpec, desiredVersion, dirty, lastHydratedVersion])

  const handleVerifyStepUp = () => {
    verifyStepUpMutation.mutate(stepUpPassword, {
      onSuccess: async () => {
        setStepUpPassword('')
        setStepUpOpen(false)
        toast.success(t('settings.enrichment_runtime.step_up.success'))
        await Promise.all([runtimeQuery.refetch(), operationsQuery.refetch()])
      },
      onError: (err) =>
        toast.error(
          err instanceof Error ? err.message : t('settings.enrichment_runtime.step_up.failed'),
        ),
    })
  }

  const ensureStepUp = () => {
    if (stepUpSatisfied) return true
    setStepUpOpen(true)
    toast.error(t('settings.enrichment_runtime.step_up.required'))
    return false
  }

  const handleSave = () => {
    if (!canEdit || !runtime || !parsedSpec.ok) return
    const errText = translateRuntimeValidationError(validateRuntimeSpec(parsedSpec.value), t)
    if (errText) {
      toast.error(errText)
      return
    }
    if (!ensureStepUp()) return
    updateMutation.mutate(
      {
        expectedVersion: desiredVersion,
        spec: parsedSpec.value,
        updateReason: updateReason.trim(),
      },
      {
        onSuccess: () => {
          setUpdateReason('')
          guard.clearDraft()
          toast.success(t('settings.enrichment_runtime.toasts.updated'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleReset = () => {
    if (!runtime || !canEdit) return
    if (!resetAll && resetFields.length === 0) {
      toast.error(t('settings.enrichment_runtime.errors.reset_empty'))
      return
    }
    if (!ensureStepUp()) return
    resetMutation.mutate(
      {
        expectedVersion: desiredVersion,
        fields: resetFields,
        resetAll,
        updateReason: resetReason.trim(),
      },
      {
        onSuccess: () => {
          setResetOpen(false)
          setResetAll(false)
          setResetFields([])
          setResetReason('')
          guard.clearDraft()
          toast.success(t('settings.enrichment_runtime.toasts.reset'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleRollback = () => {
    if (!rollbackTarget || !runtime || !canEdit) return
    if (!ensureStepUp()) return
    rollbackMutation.mutate(
      {
        expectedVersion: desiredVersion,
        targetVersion: rollbackTarget.revision.version,
        updateReason: rollbackReason.trim(),
      },
      {
        onSuccess: () => {
          setRollbackTarget(null)
          setRollbackReason('')
          guard.clearDraft()
          toast.success(t('settings.enrichment_runtime.toasts.rollback'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleRollbackToLastKnownGood = () => {
    if (!lastKnownGoodEntry) return
    setRollbackTarget(lastKnownGoodEntry)
    setRollbackReason(
      runtime?.desiredRevision.lastKnownGoodVersion
        ? `rollback to last known good ${runtime.desiredRevision.lastKnownGoodVersion}`
        : '',
    )
  }

  if (runtimeQuery.isPending) {
    return (
      <div className="flex items-center justify-center py-16 text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        {t('app.loading')}
      </div>
    )
  }

  if (!runtime) return null

  return (
    <>
      <TooltipProvider>
        <section className="min-w-0 space-y-6">
          <PageHero
            eyebrow={t('shell.groups.configuration')}
            title={t('settings.enrichment_runtime.title')}
            subtitle={t('settings.enrichment_runtime.help')}
            actions={
              <>
                <Button
                  variant={stepUpSatisfied ? 'outline' : 'default'}
                  onClick={() => setStepUpOpen(true)}
                >
                  <ShieldCheck className="mr-2 h-4 w-4" />
                  {stepUpSatisfied ? t('gdpr.step_up_verified') : t('gdpr.step_up_button')}
                </Button>
                <Button
                  variant="outline"
                  onClick={() => void runtimeQuery.refetch()}
                  disabled={runtimeQuery.isFetching}
                >
                  {runtimeQuery.isFetching ? (
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  ) : (
                    <RefreshCcw className="mr-2 h-4 w-4" />
                  )}
                  {t('settings.enrichment_runtime.refresh')}
                </Button>
              </>
            }
            metrics={
              <>
                <PageHeroMetric
                  label={t('settings.enrichment_runtime.metrics.desired_version')}
                  value={runtime.summary.desiredVersion}
                  hint={t('settings.enrichment_runtime.revision_meta', {
                    version: runtime.desiredRevision.version,
                    specVersion: runtime.desiredRevision.specVersion,
                  })}
                />
                <PageHeroMetric
                  label={t('settings.enrichment_runtime.metrics.live_instances')}
                  value={`${runtime.summary.liveInstances}`}
                  hint={t('settings.enrichment_runtime.metrics.fully_applied_hint', {
                    count: runtime.summary.fullyAppliedInstances,
                  })}
                  tone={runtime.summary.fullyConverged ? 'active' : 'default'}
                />
                <PageHeroMetric
                  label={t('settings.enrichment_runtime.metrics.stale_instances')}
                  value={`${runtime.summary.staleInstances}`}
                  hint={t('settings.enrichment_runtime.metrics.expired_hint', {
                    count: runtime.summary.expiredInstances,
                  })}
                  tone={runtime.summary.staleInstances > 0 ? 'urgent' : 'default'}
                />
                <PageHeroMetric
                  label={t('settings.enrichment_runtime.metrics.degraded_instances')}
                  value={`${runtime.summary.degradedInstances}`}
                  hint={
                    runtime.summary.fullyConverged
                      ? t('settings.enrichment_runtime.metrics.converged')
                      : t('settings.enrichment_runtime.metrics.not_converged')
                  }
                  tone={runtime.summary.degradedInstances > 0 ? 'urgent' : 'default'}
                />
              </>
            }
          />

          <div className="grid gap-4 xl:grid-cols-[1.12fr,0.88fr]">
            <Card className="overflow-hidden border-border/70 bg-[linear-gradient(180deg,rgba(255,247,237,0.62),rgba(255,255,255,0.98)_34%)] shadow-none">
              <CardHeader className="pb-4">
                <CardTitle>{t('settings.enrichment_runtime.value_title')}</CardTitle>
                <CardDescription className="max-w-3xl text-sm leading-6 text-muted-foreground">
                  {t('settings.enrichment_runtime.value_body')}
                </CardDescription>
              </CardHeader>
              <CardContent className="grid gap-3 md:grid-cols-3">
                {(['capacity', 'stability', 'rollback'] as const).map((key) => (
                  <div
                    key={key}
                    className="rounded-[1rem] border border-border/80 bg-background/95 p-4 shadow-[0_1px_2px_rgba(15,23,42,0.04)]"
                  >
                    <div className="text-sm font-medium tracking-tight">
                      {t(`settings.enrichment_runtime.value_cards.${key}.title`)}
                    </div>
                    <div className="mt-2 text-sm leading-6 text-muted-foreground">
                      {t(`settings.enrichment_runtime.value_cards.${key}.body`)}
                    </div>
                  </div>
                ))}
              </CardContent>
            </Card>

            <Card className="border-border/70 bg-zinc-50/60 shadow-none">
              <CardHeader className="pb-4">
                <CardTitle>{t('settings.enrichment_runtime.playbook_title')}</CardTitle>
                <CardDescription className="text-sm leading-6 text-muted-foreground">
                  {t('settings.enrichment_runtime.playbook_body')}
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                {(['review', 'change', 'observe', 'rollback'] as const).map((key, index) => (
                  <div
                    key={key}
                    className="flex gap-3 rounded-[1rem] border border-border/80 bg-background/90 px-3 py-3 shadow-[0_1px_2px_rgba(15,23,42,0.04)]"
                  >
                    <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-zinc-950 text-xs font-semibold text-white">
                      {index + 1}
                    </div>
                    <div>
                      <div className="text-sm font-medium">
                        {t(`settings.enrichment_runtime.playbook_steps.${key}.title`)}
                      </div>
                      <div className="mt-1 text-sm leading-6 text-muted-foreground">
                        {t(`settings.enrichment_runtime.playbook_steps.${key}.body`)}
                      </div>
                    </div>
                  </div>
                ))}
              </CardContent>
            </Card>
          </div>

          {!runtime.summary.fullyConverged || runtime.summary.degradedInstances > 0 ? (
            <Alert variant="destructive">
              <AlertTriangle />
              <AlertTitle>{t('settings.enrichment_runtime.convergence_warning_title')}</AlertTitle>
              <AlertDescription>
                <p>{t('settings.enrichment_runtime.convergence_warning_body')}</p>
              </AlertDescription>
            </Alert>
          ) : (
            <Alert>
              <CheckCircle2 />
              <AlertTitle>{t('settings.enrichment_runtime.convergence_ok_title')}</AlertTitle>
              <AlertDescription>
                <p>{t('settings.enrichment_runtime.convergence_ok_body')}</p>
              </AlertDescription>
            </Alert>
          )}

          <div className="grid gap-4 xl:grid-cols-3">
            {runtimePosture.map((item) => (
              <Card
                key={item.title}
                className="border-border/70 bg-zinc-50/60 shadow-[0_1px_2px_rgba(15,23,42,0.04)]"
              >
                <CardContent className="space-y-2 p-4">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <item.icon className="h-4 w-4 text-muted-foreground" />
                    {item.title}
                  </div>
                  <div className="text-sm leading-6 text-muted-foreground">{item.body}</div>
                </CardContent>
              </Card>
            ))}
          </div>

          <div className="grid gap-4 xl:grid-cols-[1.2fr,0.8fr]">
            <Card className="border-border/70 bg-zinc-50/40 shadow-sm">
              <CardHeader className="pb-3">
                <CardTitle>{t('settings.enrichment_runtime.conditions_title')}</CardTitle>
                <CardDescription>
                  {t('settings.enrichment_runtime.conditions_help')}
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex flex-wrap gap-2">
                  {runtimeConditions.map((condition) => (
                    <ConditionChip
                      key={condition.label}
                      tone={condition.tone}
                      label={localizeConditionLabel(condition.label, t)}
                    />
                  ))}
                </div>
                {runtime.desiredSpec.llmRateLimitEnabled && runtime.summary.liveInstances > 1 ? (
                  <Alert>
                    <AlertTriangle />
                    <AlertTitle>{t('settings.enrichment_runtime.local_limiter_title')}</AlertTitle>
                    <AlertDescription>
                      <p>
                        {t('settings.enrichment_runtime.local_limiter_body', {
                          count: runtime.summary.liveInstances,
                        })}
                      </p>
                    </AlertDescription>
                  </Alert>
                ) : null}
              </CardContent>
            </Card>

            <Card className="border-border/70 bg-zinc-50/60 shadow-sm">
              <CardHeader className="pb-3">
                <CardTitle>{t('settings.enrichment_runtime.operator_guidance_title')}</CardTitle>
                <CardDescription>
                  {t('settings.enrichment_runtime.operator_guidance_body')}
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <GuidanceBlock
                  title={t('settings.enrichment_runtime.when_change_title')}
                  items={[
                    t('settings.enrichment_runtime.when_change_items.backlog'),
                    t('settings.enrichment_runtime.when_change_items.latency'),
                    t('settings.enrichment_runtime.when_change_items.provider'),
                  ]}
                />
                <GuidanceBlock
                  title={t('settings.enrichment_runtime.after_change_title')}
                  items={[
                    t('settings.enrichment_runtime.after_change_items.instances'),
                    t('settings.enrichment_runtime.after_change_items.degraded'),
                    t('settings.enrichment_runtime.after_change_items.history'),
                  ]}
                />
              </CardContent>
            </Card>
          </div>

          <div className="grid gap-6 xl:grid-cols-[1.3fr,0.9fr]">
            <Card className="border-border/70 shadow-sm">
              <CardHeader className="border-b border-border/70 bg-zinc-50/50 pb-4">
                <CardTitle>{t('settings.enrichment_runtime.policy_title')}</CardTitle>
                <CardDescription>{t('settings.enrichment_runtime.policy_help')}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-5">
                <div className="space-y-4">
                  <RuntimeFormSection
                    icon={Gauge}
                    title={t('settings.enrichment_runtime.form_sections.throughput_title')}
                    body={t('settings.enrichment_runtime.form_sections.throughput_body')}
                  >
                    <div className="grid gap-4 md:grid-cols-2">
                      <NumberField
                        id="runtime-queue-len"
                        label={t('settings.enrichment_runtime.fields.queue_len')}
                        hint={t('settings.enrichment_runtime.field_hints.queue_len')}
                        value={draft.queueLen}
                        disabled={!canEdit}
                        onChange={(value) => setDraft((prev) => ({ ...prev, queueLen: value }))}
                      />
                      <NumberField
                        id="runtime-workers"
                        label={t('settings.enrichment_runtime.fields.workers')}
                        hint={t('settings.enrichment_runtime.field_hints.workers')}
                        value={draft.workers}
                        disabled={!canEdit}
                        onChange={(value) => setDraft((prev) => ({ ...prev, workers: value }))}
                      />
                    </div>
                  </RuntimeFormSection>

                  <RuntimeFormSection
                    icon={TimerReset}
                    title={t('settings.enrichment_runtime.form_sections.batch_title')}
                    body={t('settings.enrichment_runtime.form_sections.batch_body')}
                  >
                    <div className="grid gap-4 md:grid-cols-3">
                      <NumberField
                        id="runtime-batch-size"
                        label={t('settings.enrichment_runtime.fields.batch_size')}
                        hint={t('settings.enrichment_runtime.field_hints.batch_size')}
                        value={draft.batchSize}
                        disabled={!canEdit}
                        onChange={(value) => setDraft((prev) => ({ ...prev, batchSize: value }))}
                      />
                      <NumberField
                        id="runtime-batch-window"
                        label={t('settings.enrichment_runtime.fields.batch_window')}
                        hint={t('settings.enrichment_runtime.field_hints.batch_window')}
                        suffix={t('settings.enrichment_runtime.seconds_suffix')}
                        value={draft.batchWindowSeconds}
                        disabled={!canEdit}
                        onChange={(value) =>
                          setDraft((prev) => ({ ...prev, batchWindowSeconds: value }))
                        }
                      />
                      <NumberField
                        id="runtime-sweep-interval"
                        label={t('settings.enrichment_runtime.fields.sweep_interval')}
                        hint={t('settings.enrichment_runtime.field_hints.sweep_interval')}
                        suffix={t('settings.enrichment_runtime.seconds_suffix')}
                        value={draft.sweepIntervalSeconds}
                        disabled={!canEdit}
                        onChange={(value) =>
                          setDraft((prev) => ({
                            ...prev,
                            sweepIntervalSeconds: value,
                          }))
                        }
                      />
                    </div>
                  </RuntimeFormSection>

                  <RuntimeFormSection
                    icon={SlidersHorizontal}
                    title={t('settings.enrichment_runtime.form_sections.llm_title')}
                    body={t('settings.enrichment_runtime.form_sections.llm_body')}
                  >
                    <div className="grid gap-4 md:grid-cols-3">
                      <div className="space-y-2 md:col-span-3">
                        <Label>
                          {t('settings.enrichment_runtime.fields.llm_rate_limit_enabled')}
                        </Label>
                        <div className="flex items-center gap-3 rounded-lg border border-border/80 bg-background px-3 py-3 text-sm">
                          <Checkbox
                            aria-label={t(
                              'settings.enrichment_runtime.fields.llm_rate_limit_enabled',
                            )}
                            checked={draft.llmRateLimitEnabled}
                            disabled={!canEdit}
                            onCheckedChange={(checked) =>
                              setDraft((prev) => ({
                                ...prev,
                                llmRateLimitEnabled: checked === true,
                              }))
                            }
                          />
                          <span>{t('settings.enrichment_runtime.rate_limit_toggle')}</span>
                        </div>
                        <p className="text-xs text-muted-foreground">
                          {t('settings.enrichment_runtime.field_hints.llm_rate_limit_enabled')}
                        </p>
                      </div>
                      <NumberField
                        id="runtime-llm-max-qps"
                        label={t('settings.enrichment_runtime.fields.llm_max_qps')}
                        hint={t('settings.enrichment_runtime.field_hints.llm_max_qps')}
                        value={draft.llmMaxQps}
                        disabled={!canEdit}
                        onChange={(value) => setDraft((prev) => ({ ...prev, llmMaxQps: value }))}
                      />
                      <NumberField
                        id="runtime-llm-burst"
                        label={t('settings.enrichment_runtime.fields.llm_burst')}
                        hint={t('settings.enrichment_runtime.field_hints.llm_burst')}
                        value={draft.llmBurst}
                        disabled={!canEdit}
                        onChange={(value) => setDraft((prev) => ({ ...prev, llmBurst: value }))}
                      />
                    </div>
                  </RuntimeFormSection>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="runtime-update-reason">
                    {t('settings.enrichment_runtime.reason_label')}
                  </Label>
                  <Input
                    id="runtime-update-reason"
                    placeholder={t('settings.enrichment_runtime.reason_placeholder')}
                    value={updateReason}
                    onChange={(e) => setUpdateReason(e.target.value)}
                    disabled={!canEdit}
                  />
                </div>

                <div className="rounded-md border bg-muted/20 px-3 py-2 text-sm text-muted-foreground">
                  <div>
                    {t('settings.enrichment_runtime.revision_meta', {
                      version: runtime.desiredRevision.version,
                      specVersion: runtime.desiredRevision.specVersion,
                    })}
                  </div>
                  <div className="mt-1">
                    {t('settings.enrichment_runtime.last_updated_meta', {
                      by: formatRuntimeActorLabel(runtime.desiredRevision.updatedBy),
                      at: formatTimestamp(runtime.desiredRevision.updatedAt, t),
                    })}
                  </div>
                  <div className="mt-1">
                    {t('settings.enrichment_runtime.bootstrap_meta', {
                      value: runtime.desiredRevision.bootstrapSnapshotVersion || '-',
                    })}
                  </div>
                  <div className="mt-1">
                    {t('settings.enrichment_runtime.last_known_good_meta', {
                      value: runtime.desiredRevision.lastKnownGoodVersion || '-',
                    })}
                  </div>
                </div>

                {validationError ? (
                  <p className="text-sm text-destructive">{validationError}</p>
                ) : null}

                {draftVsDesiredDiff.length > 0 ? (
                  <div className="space-y-2 rounded-md border bg-muted/20 p-3 text-sm">
                    <div className="font-medium">
                      {t('settings.enrichment_runtime.pending_diff_title')}
                    </div>
                    <DiffList rows={draftVsDesiredDiff} t={t} />
                    {changeImpactNotes.length > 0 ? (
                      <div className="space-y-2 rounded-md border bg-background/80 p-3">
                        <div className="font-medium">
                          {t('settings.enrichment_runtime.change_impact_title')}
                        </div>
                        <div className="space-y-2">
                          {changeImpactNotes.map((note) => (
                            <div key={note} className="flex gap-2 text-sm text-muted-foreground">
                              <span className="mt-1 h-1.5 w-1.5 shrink-0 rounded-full bg-foreground/60" />
                              <span>{note}</span>
                            </div>
                          ))}
                        </div>
                      </div>
                    ) : null}
                  </div>
                ) : null}

                {canEdit ? (
                  <div className="flex flex-wrap justify-end gap-2">
                    <Button
                      variant="outline"
                      onClick={() => {
                        setResetAll(true)
                        setResetFields([])
                        setResetOpen(true)
                      }}
                    >
                      <DatabaseBackup className="mr-2 h-4 w-4" />
                      {t('settings.enrichment_runtime.reset_all_button')}
                    </Button>
                    <Button variant="outline" onClick={() => setResetOpen(true)}>
                      <RefreshCcw className="mr-2 h-4 w-4" />
                      {t('settings.enrichment_runtime.reset_partial_button')}
                    </Button>
                    <Button
                      onClick={handleSave}
                      disabled={!dirty || !!validationError || updateMutation.isPending}
                    >
                      {updateMutation.isPending ? (
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      ) : null}
                      {t('common.save')}
                    </Button>
                  </div>
                ) : null}
              </CardContent>
            </Card>

            <Card className="border-border/70 bg-zinc-50/30 shadow-sm">
              <CardHeader>
                <CardTitle>{t('settings.enrichment_runtime.guardrail_title')}</CardTitle>
                <CardDescription>{t('settings.enrichment_runtime.guardrail_help')}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="rounded-md border px-3 py-2 text-sm">
                  <div className="flex items-center gap-2 font-medium">
                    <ShieldCheck className="h-4 w-4" />
                    {stepUpSatisfied ? t('gdpr.step_up_verified') : t('gdpr.step_up_pending')}
                  </div>
                  <div className="mt-1 text-muted-foreground">
                    {stepUp?.expiresAt
                      ? t('gdpr.step_up_expires_at', {
                          value: formatTimestamp(stepUp.expiresAt, t),
                        })
                      : t('settings.enrichment_runtime.step_up.needed')}
                  </div>
                </div>
                <div className="space-y-2 rounded-md border border-amber-500/20 bg-amber-500/5 p-3 text-sm">
                  <div className="font-medium">
                    {t('settings.enrichment_runtime.guardrail_warning_title')}
                  </div>
                  <p className="text-muted-foreground">
                    {t('settings.enrichment_runtime.guardrail_warning_body')}
                  </p>
                </div>
                {lastKnownGoodEntry ? (
                  <div className="space-y-3 rounded-md border bg-muted/20 p-3 text-sm">
                    <div>
                      <div className="font-medium">
                        {t('settings.enrichment_runtime.last_known_good_title')}
                      </div>
                      <div className="mt-1 text-muted-foreground">
                        {t('settings.enrichment_runtime.last_known_good_body', {
                          version: lastKnownGoodEntry.revision.version,
                        })}
                      </div>
                    </div>
                    {canEdit ? (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={handleRollbackToLastKnownGood}
                        disabled={rollbackMutation.isPending}
                      >
                        <DatabaseBackup className="mr-2 h-3.5 w-3.5" />
                        {t('settings.enrichment_runtime.rollback_last_known_good')}
                      </Button>
                    ) : null}
                  </div>
                ) : null}
                <div className="space-y-2 rounded-md border bg-muted/20 p-3 text-sm">
                  <div className="font-medium">
                    {t('settings.enrichment_runtime.desired_title')}
                  </div>
                  <SpecSummary spec={runtime.desiredSpec} />
                </div>
                <div className="space-y-2 rounded-md border bg-muted/20 p-3 text-sm">
                  <div className="font-medium">
                    {t('settings.enrichment_runtime.bootstrap_title')}
                  </div>
                  <SpecSummary spec={runtime.bootstrapDefault} />
                </div>
                <div className="space-y-2 rounded-md border bg-muted/20 p-3 text-sm">
                  <div className="font-medium">
                    {t('settings.enrichment_runtime.bootstrap_diff_title')}
                  </div>
                  {currentVsBootstrapDiff.length > 0 ? (
                    <DiffList rows={currentVsBootstrapDiff} t={t} />
                  ) : (
                    <div className="text-muted-foreground">
                      {t('settings.enrichment_runtime.bootstrap_diff_empty')}
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>{t('settings.enrichment_runtime.instances_title')}</CardTitle>
              <CardDescription>{t('settings.enrichment_runtime.instances_help')}</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="mb-4 rounded-lg border border-emerald-200/70 bg-emerald-50/60 p-3">
                <div className="text-sm font-medium">
                  {t('settings.enrichment_runtime.active_instances_title', {
                    count: partitionedInstances.active.length,
                  })}
                </div>
                <div className="mt-1 text-sm text-muted-foreground">
                  {t('settings.enrichment_runtime.active_instances_body')}
                </div>
              </div>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('settings.enrichment_runtime.instance_cols.instance')}</TableHead>
                    <TableHead>{t('settings.enrichment_runtime.instance_cols.status')}</TableHead>
                    <TableHead>{t('settings.enrichment_runtime.instance_cols.version')}</TableHead>
                    <TableHead>{t('settings.enrichment_runtime.instance_cols.queue')}</TableHead>
                    <TableHead>
                      {t('settings.enrichment_runtime.instance_cols.rate_limit')}
                    </TableHead>
                    <TableHead>
                      {t('settings.enrichment_runtime.instance_cols.last_seen')}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {partitionedInstances.active.map((item, index) => (
                    <TableRow key={item.instanceId}>
                      <TableCell className="align-top">
                        <div className="flex items-center gap-2">
                          <div className="font-medium">
                            {buildInstanceDisplayName(item, index).primary}
                          </div>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <button
                                type="button"
                                className="text-muted-foreground transition-colors hover:text-foreground"
                                aria-label={t('settings.enrichment_runtime.technical_details')}
                              >
                                <CircleHelp className="h-3.5 w-3.5" />
                              </button>
                            </TooltipTrigger>
                            <TooltipContent className="max-w-72 space-y-1">
                              <div>
                                {t('settings.enrichment_runtime.instance_id_label')}
                                {item.instanceId || '-'}
                              </div>
                              <div>
                                {t('settings.enrichment_runtime.instance_boot_id_label')}
                                {item.bootId || '-'}
                              </div>
                            </TooltipContent>
                          </Tooltip>
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {t('settings.enrichment_runtime.instance_identity_hint')}
                        </div>
                      </TableCell>
                      <TableCell className="align-top">
                        <StatusPair
                          t={t}
                          runnerStatus={item.runnerApplyStatus}
                          limiterStatus={item.limiterApplyStatus}
                        />
                        <div className="mt-2 flex flex-wrap gap-1">
                          {buildInstanceConditions(item).map((condition) => (
                            <ConditionChip
                              key={`${item.instanceId}-${condition.label}`}
                              tone={condition.tone}
                              label={localizeConditionLabel(condition.label, t)}
                            />
                          ))}
                        </div>
                        {item.degradedReason ? (
                          <div className="mt-1 max-w-72 whitespace-normal text-xs text-destructive">
                            {item.degradedReason}
                          </div>
                        ) : null}
                      </TableCell>
                      <TableCell className="align-top">
                        <div>
                          {t('settings.enrichment_runtime.version_runner', {
                            value: item.runnerEffectiveVersion,
                          })}
                        </div>
                        <div className="text-muted-foreground">
                          {t('settings.enrichment_runtime.version_limiter', {
                            value: item.limiterEffectiveVersion,
                          })}
                        </div>
                        <div className="text-muted-foreground">
                          {t('settings.enrichment_runtime.version_observed', {
                            value: item.observedDesiredVersion,
                          })}
                        </div>
                      </TableCell>
                      <TableCell className="align-top">
                        <div>
                          {t('settings.enrichment_runtime.instance_queue', {
                            depth: item.queueDepth,
                            capacity: item.queueCapacityEffective,
                          })}
                        </div>
                        <div className="text-muted-foreground">
                          {t('settings.enrichment_runtime.instance_inflight', {
                            count: item.inFlight,
                          })}
                        </div>
                        {item.queueResizePending ? (
                          <div className="text-xs text-amber-600">
                            {t('settings.enrichment_runtime.instance_resize_pending')}
                          </div>
                        ) : null}
                      </TableCell>
                      <TableCell className="align-top">
                        <div>
                          {item.appliedSpec.llmRateLimitEnabled ? t('common.yes') : t('common.no')}
                        </div>
                        <div className="text-muted-foreground">
                          {t('settings.enrichment_runtime.instance_qps_burst', {
                            qps: item.appliedSpec.llmMaxQps,
                            burst: item.appliedSpec.llmBurst,
                          })}
                        </div>
                      </TableCell>
                      <TableCell className="align-top">
                        <div>{formatRelative(item.lastSeenAt)}</div>
                        <div className="text-muted-foreground">
                          {formatTimestamp(item.lastAppliedAt, t)}
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                  {partitionedInstances.active.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={6} className="text-center text-muted-foreground">
                        {t('settings.enrichment_runtime.instances_empty')}
                      </TableCell>
                    </TableRow>
                  ) : null}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          {partitionedInstances.historical.length > 0 ? (
            <Card>
              <CardHeader>
                <CardTitle>
                  {t('settings.enrichment_runtime.historical_instances_title', {
                    count: partitionedInstances.historical.length,
                  })}
                </CardTitle>
                <CardDescription>
                  {t('settings.enrichment_runtime.historical_instances_help')}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  aria-expanded={historicalExpanded}
                  onClick={() => setHistoricalExpanded((prev) => !prev)}
                >
                  {historicalExpanded
                    ? t('settings.enrichment_runtime.historical_instances_toggle_collapse')
                    : t('settings.enrichment_runtime.historical_instances_toggle')}
                </Button>
                {historicalExpanded ? (
                  <div className="mt-4">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>
                            {t('settings.enrichment_runtime.instance_cols.instance')}
                          </TableHead>
                          <TableHead>
                            {t('settings.enrichment_runtime.instance_cols.status')}
                          </TableHead>
                          <TableHead>
                            {t('settings.enrichment_runtime.instance_cols.version')}
                          </TableHead>
                          <TableHead>
                            {t('settings.enrichment_runtime.instance_cols.queue')}
                          </TableHead>
                          <TableHead>
                            {t('settings.enrichment_runtime.instance_cols.rate_limit')}
                          </TableHead>
                          <TableHead>
                            {t('settings.enrichment_runtime.instance_cols.last_seen')}
                          </TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {partitionedInstances.historical.map((item, index) => (
                          <TableRow key={`historical-${item.instanceId}-${item.bootId}`}>
                            <TableCell className="align-top">
                              <div className="flex items-center gap-2">
                                <div className="font-medium">
                                  {
                                    buildInstanceDisplayName(
                                      item,
                                      index + partitionedInstances.active.length,
                                    ).primary
                                  }
                                </div>
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <button
                                      type="button"
                                      className="text-muted-foreground transition-colors hover:text-foreground"
                                      aria-label={t(
                                        'settings.enrichment_runtime.technical_details',
                                      )}
                                    >
                                      <CircleHelp className="h-3.5 w-3.5" />
                                    </button>
                                  </TooltipTrigger>
                                  <TooltipContent className="max-w-72 space-y-1">
                                    <div>
                                      {t('settings.enrichment_runtime.instance_id_label')}
                                      {item.instanceId || '-'}
                                    </div>
                                    <div>
                                      {t('settings.enrichment_runtime.instance_boot_id_label')}
                                      {item.bootId || '-'}
                                    </div>
                                  </TooltipContent>
                                </Tooltip>
                              </div>
                              <div className="text-xs text-muted-foreground">
                                {t('settings.enrichment_runtime.instance_identity_hint')}
                              </div>
                            </TableCell>
                            <TableCell className="align-top">
                              <StatusPair
                                t={t}
                                runnerStatus={item.runnerApplyStatus}
                                limiterStatus={item.limiterApplyStatus}
                              />
                              <div className="mt-2 flex flex-wrap gap-1">
                                {buildInstanceConditions(item).map((condition) => (
                                  <ConditionChip
                                    key={`${item.instanceId}-${condition.label}`}
                                    tone={condition.tone}
                                    label={localizeConditionLabel(condition.label, t)}
                                  />
                                ))}
                              </div>
                            </TableCell>
                            <TableCell className="align-top">
                              <div>
                                {t('settings.enrichment_runtime.version_runner', {
                                  value: item.runnerEffectiveVersion,
                                })}
                              </div>
                              <div className="text-muted-foreground">
                                {t('settings.enrichment_runtime.version_limiter', {
                                  value: item.limiterEffectiveVersion,
                                })}
                              </div>
                              <div className="text-muted-foreground">
                                {t('settings.enrichment_runtime.version_observed', {
                                  value: item.observedDesiredVersion,
                                })}
                              </div>
                            </TableCell>
                            <TableCell className="align-top">
                              <div>
                                {t('settings.enrichment_runtime.instance_queue', {
                                  depth: item.queueDepth,
                                  capacity: item.queueCapacityEffective,
                                })}
                              </div>
                              <div className="text-muted-foreground">
                                {t('settings.enrichment_runtime.instance_inflight', {
                                  count: item.inFlight,
                                })}
                              </div>
                            </TableCell>
                            <TableCell className="align-top">
                              <div>
                                {item.appliedSpec.llmRateLimitEnabled
                                  ? t('common.yes')
                                  : t('common.no')}
                              </div>
                              <div className="text-muted-foreground">
                                {t('settings.enrichment_runtime.instance_qps_burst', {
                                  qps: item.appliedSpec.llmMaxQps,
                                  burst: item.appliedSpec.llmBurst,
                                })}
                              </div>
                            </TableCell>
                            <TableCell className="align-top">
                              <div>{formatRelative(item.lastSeenAt)}</div>
                              <div className="text-muted-foreground">
                                {formatTimestamp(item.lastAppliedAt, t)}
                              </div>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                ) : null}
              </CardContent>
            </Card>
          ) : null}

          <Card>
            <CardHeader>
              <CardTitle>{t('settings.enrichment_runtime.history_title')}</CardTitle>
              <CardDescription>{t('settings.enrichment_runtime.history_help')}</CardDescription>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('settings.enrichment_runtime.history_cols.version')}</TableHead>
                    <TableHead>{t('settings.enrichment_runtime.history_cols.operation')}</TableHead>
                    <TableHead>{t('settings.enrichment_runtime.history_cols.risk')}</TableHead>
                    <TableHead>{t('settings.enrichment_runtime.history_cols.actor')}</TableHead>
                    <TableHead>{t('settings.enrichment_runtime.history_cols.reason')}</TableHead>
                    <TableHead>{t('settings.enrichment_runtime.history_cols.updated')}</TableHead>
                    <TableHead>{t('settings.enrichment_runtime.history_cols.actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {runtime.history.map((entry) => {
                    const isCurrent = entry.revision.version === runtime.desiredRevision.version
                    return (
                      <TableRow key={entry.revision.version}>
                        <TableCell>
                          <div className="font-medium">{entry.revision.version}</div>
                          <div className="text-xs text-muted-foreground">
                            {t('settings.enrichment_runtime.history_source_target', {
                              from: entry.sourceVersion || '0',
                              to: entry.targetVersion || '0',
                            })}
                          </div>
                        </TableCell>
                        <TableCell>{localizeOperationType(entry.operationType, t)}</TableCell>
                        <TableCell>{localizeRiskLevel(entry.riskLevel, t)}</TableCell>
                        <TableCell>
                          <div>{formatRuntimeActorLabel(entry.revision.updatedBy)}</div>
                          {entry.revision.updatedBy &&
                          formatRuntimeActorLabel(entry.revision.updatedBy) !==
                            entry.revision.updatedBy ? (
                            <div className="text-xs text-muted-foreground">
                              {t('settings.enrichment_runtime.history_actor_hint')}
                            </div>
                          ) : null}
                        </TableCell>
                        <TableCell className="max-w-72 whitespace-normal">
                          {entry.revision.updateReason || '-'}
                          {entry.rollbackLineage ? (
                            <div className="mt-1 text-xs text-muted-foreground">
                              {entry.rollbackLineage}
                            </div>
                          ) : null}
                        </TableCell>
                        <TableCell>{formatTimestamp(entry.revision.updatedAt, t)}</TableCell>
                        <TableCell>
                          <Button
                            variant="outline"
                            size="sm"
                            disabled={!canEdit || isCurrent || rollbackMutation.isPending}
                            onClick={() => setRollbackTarget(entry)}
                          >
                            <DatabaseBackup className="mr-2 h-3.5 w-3.5" />
                            {isCurrent
                              ? t('settings.enrichment_runtime.current_badge')
                              : t('settings.enrichment_runtime.rollback_button')}
                          </Button>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                  {runtime.history.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={7} className="text-center text-muted-foreground">
                        {t('settings.enrichment_runtime.history_empty')}
                      </TableCell>
                    </TableRow>
                  ) : null}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </section>
      </TooltipProvider>

      <Dialog open={stepUpOpen} onOpenChange={setStepUpOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('settings.enrichment_runtime.step_up.dialog_title')}</DialogTitle>
            <DialogDescription>
              {t('settings.enrichment_runtime.step_up.dialog_body')}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="runtime-step-up-password">
              {t('settings.enrichment_runtime.step_up.password_label')}
            </Label>
            <Input
              id="runtime-step-up-password"
              type="password"
              value={stepUpPassword}
              onChange={(e) => setStepUpPassword(e.target.value)}
              placeholder={t('settings.enrichment_runtime.step_up.password_placeholder')}
            />
            <p className="text-xs text-muted-foreground">
              {t('settings.enrichment_runtime.step_up.dialog_hint')}
            </p>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setStepUpOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button onClick={handleVerifyStepUp} disabled={verifyStepUpMutation.isPending}>
              {verifyStepUpMutation.isPending ? (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              ) : null}
              {t('settings.enrichment_runtime.step_up.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={resetOpen}
        onOpenChange={(open) => {
          setResetOpen(open)
          if (!open) {
            setResetAll(false)
            setResetFields([])
            setResetReason('')
          }
        }}
      >
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{t('settings.enrichment_runtime.reset_dialog_title')}</DialogTitle>
            <DialogDescription>
              {t('settings.enrichment_runtime.reset_dialog_body')}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex items-center gap-3 rounded-md border px-3 py-2 text-sm">
              <Checkbox
                aria-label={t('settings.enrichment_runtime.reset_all_toggle')}
                checked={resetAll}
                onCheckedChange={(checked) => {
                  const next = checked === true
                  setResetAll(next)
                  if (next) setResetFields([])
                }}
              />
              <span>{t('settings.enrichment_runtime.reset_all_toggle')}</span>
            </div>
            {!resetAll ? (
              <div className="grid gap-2 sm:grid-cols-2">
                {RESET_FIELDS.map((item) => (
                  <div
                    key={item.field}
                    className="flex items-center gap-3 rounded-lg border border-border/80 bg-background px-3 py-2.5 text-sm"
                  >
                    <Checkbox
                      aria-label={t(item.labelKey)}
                      checked={resetFields.includes(item.field)}
                      onCheckedChange={(checked) =>
                        setResetFields((prev) =>
                          checked === true
                            ? [...prev, item.field]
                            : prev.filter((value) => value !== item.field),
                        )
                      }
                    />
                    <span>{t(item.labelKey)}</span>
                  </div>
                ))}
              </div>
            ) : null}
            <div className="space-y-2">
              <Label htmlFor="runtime-reset-reason">
                {t('settings.enrichment_runtime.reason_label')}
              </Label>
              <Input
                id="runtime-reset-reason"
                placeholder={t('settings.enrichment_runtime.reason_placeholder')}
                value={resetReason}
                onChange={(e) => setResetReason(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setResetOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button onClick={handleReset} disabled={resetMutation.isPending}>
              {resetMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              {t('settings.enrichment_runtime.reset_confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={rollbackTarget !== null}
        onOpenChange={(open) => {
          if (!open) {
            setRollbackTarget(null)
            setRollbackReason('')
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('settings.enrichment_runtime.rollback_dialog_title')}</DialogTitle>
            <DialogDescription>
              {t('settings.enrichment_runtime.rollback_dialog_body', {
                version: rollbackTarget?.revision.version ?? '0',
              })}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2 rounded-md border bg-muted/20 p-3 text-sm">
            <div>
              {t('settings.enrichment_runtime.history_source_target', {
                from: rollbackTarget?.sourceVersion ?? '0',
                to: rollbackTarget?.targetVersion ?? '0',
              })}
            </div>
            <div className="text-muted-foreground">
              {rollbackTarget?.revision.updateReason ||
                t('settings.enrichment_runtime.reason_empty')}
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="runtime-rollback-reason">
              {t('settings.enrichment_runtime.reason_label')}
            </Label>
            <Input
              id="runtime-rollback-reason"
              placeholder={t('settings.enrichment_runtime.rollback_reason_placeholder')}
              value={rollbackReason}
              onChange={(e) => setRollbackReason(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRollbackTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button onClick={handleRollback} disabled={rollbackMutation.isPending}>
              {rollbackMutation.isPending ? (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              ) : null}
              {t('settings.enrichment_runtime.rollback_confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <UnsavedChangesDialog
        open={guard.dialogOpen}
        onConfirmLeave={guard.confirmLeave}
        onCancelLeave={guard.cancelLeave}
      />
    </>
  )
}

function emptyDraft(): DraftState {
  return {
    queueLen: '',
    workers: '',
    batchSize: '',
    batchWindowSeconds: '',
    sweepIntervalSeconds: '',
    llmRateLimitEnabled: false,
    llmMaxQps: '',
    llmBurst: '',
  }
}

function specToDraft(spec: EnrichmentRuntimeSpecView): DraftState {
  return {
    queueLen: String(spec.queueLen),
    workers: String(spec.workers),
    batchSize: String(spec.batchSize),
    batchWindowSeconds: String(spec.batchWindowSeconds),
    sweepIntervalSeconds: String(spec.sweepIntervalSeconds),
    llmRateLimitEnabled: spec.llmRateLimitEnabled,
    llmMaxQps: String(spec.llmMaxQps),
    llmBurst: String(spec.llmBurst),
  }
}

function draftToSpec(
  draft: DraftState,
): { ok: true; value: EnrichmentRuntimeSpecView } | { ok: false } {
  const queueLen = Number(draft.queueLen)
  const workers = Number(draft.workers)
  const batchSize = Number(draft.batchSize)
  const batchWindowSeconds = Number(draft.batchWindowSeconds)
  const sweepIntervalSeconds = Number(draft.sweepIntervalSeconds)
  const llmMaxQps = Number(draft.llmMaxQps)
  const llmBurst = Number(draft.llmBurst)
  const values = [
    queueLen,
    workers,
    batchSize,
    batchWindowSeconds,
    sweepIntervalSeconds,
    llmMaxQps,
    llmBurst,
  ]
  if (values.some((value) => Number.isNaN(value))) {
    return { ok: false }
  }
  return {
    ok: true,
    value: {
      queueLen,
      workers,
      batchSize,
      batchWindowSeconds,
      sweepIntervalSeconds,
      llmRateLimitEnabled: draft.llmRateLimitEnabled,
      llmMaxQps,
      llmBurst,
    },
  }
}

function translateRuntimeValidationError(
  key: RuntimeValidationErrorKey | null,
  t: TFunction,
): string | null {
  return key ? t(key) : null
}

function ConditionChip({ tone, label }: { tone: 'good' | 'warn' | 'bad'; label: string }) {
  const cls =
    tone === 'good'
      ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700'
      : tone === 'warn'
        ? 'border-amber-500/30 bg-amber-500/10 text-amber-700'
        : 'border-destructive/30 bg-destructive/10 text-destructive'
  return <span className={`rounded-md border px-2 py-1 text-xs font-medium ${cls}`}>{label}</span>
}

function GuidanceBlock({ title, items }: { title: string; items: string[] }) {
  return (
    <div className="space-y-2 rounded-lg border border-border/80 bg-background/90 p-4 shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
      <div className="text-sm font-medium">{title}</div>
      <div className="space-y-2">
        {items.map((item) => (
          <div key={item} className="flex gap-2 text-sm text-muted-foreground">
            <span className="mt-1 h-1.5 w-1.5 shrink-0 rounded-full bg-foreground/60" />
            <span>{item}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function RuntimeFormSection({
  icon: Icon,
  title,
  body,
  children,
}: {
  icon: typeof Gauge
  title: string
  body: string
  children: ReactNode
}) {
  return (
    <section className="rounded-xl border border-border/80 bg-zinc-50/40 p-4 shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
      <div className="mb-4 flex items-start gap-3">
        <div className="rounded-full border border-background/80 bg-background p-2">
          <Icon className="h-4 w-4 text-muted-foreground" />
        </div>
        <div className="min-w-0">
          <div className="text-sm font-medium">{title}</div>
          <div className="mt-1 text-sm leading-6 text-muted-foreground">{body}</div>
        </div>
      </div>
      {children}
    </section>
  )
}

function DiffList({
  rows,
  t,
}: {
  rows: Array<{ key?: string; label: string; current: string; next: string }>
  t: TFunction
}) {
  return (
    <div className="space-y-2">
      {rows.map((row) => (
        <div
          key={row.label}
          className="flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm"
        >
          <div className="font-medium">{localizeSpecLabel(row.key ?? row.label, row.label, t)}</div>
          <div className="text-right text-muted-foreground">
            <span>{row.current}</span>
            <span className="mx-2">{'->'}</span>
            <span className="font-medium text-foreground">{row.next}</span>
          </div>
        </div>
      ))}
    </div>
  )
}

function NumberField({
  id,
  label,
  value,
  onChange,
  disabled,
  suffix,
  hint,
}: {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  disabled: boolean
  suffix?: string
  hint?: string
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex items-center gap-2">
        <Input
          id={id}
          type="number"
          value={value}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value)}
        />
        {suffix ? <span className="text-sm text-muted-foreground">{suffix}</span> : null}
      </div>
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

function SpecSummary({ spec }: { spec: EnrichmentRuntimeSpecView }) {
  return (
    <div className="space-y-2 text-sm text-muted-foreground">
      <div>
        队列容量 {spec.queueLen}，工作协程 {spec.workers}，批处理 {spec.batchSize}
      </div>
      <div>
        批处理窗口 {spec.batchWindowSeconds}s，扫队列间隔 {spec.sweepIntervalSeconds}s
      </div>
      <div>
        LLM 限流 {spec.llmRateLimitEnabled ? '开启' : '关闭'}，QPS {spec.llmMaxQps}，Burst{' '}
        {spec.llmBurst}
      </div>
    </div>
  )
}

function StatusPair({
  t,
  runnerStatus,
  limiterStatus,
}: {
  t: TFunction
  runnerStatus: string
  limiterStatus: string
}) {
  return (
    <div className="space-y-1 text-sm">
      <div>
        {t('settings.enrichment_runtime.runner_status_label')}
        {formatStatus(runnerStatus, t)}
      </div>
      <div className="text-muted-foreground">
        {t('settings.enrichment_runtime.limiter_status_label')}
        {formatStatus(limiterStatus, t)}
      </div>
    </div>
  )
}

function formatStatus(value: string, t: TFunction): string {
  const normalized = value.replace('RUNTIME_APPLY_STATUS_', '').toLowerCase()
  switch (normalized) {
    case 'applied':
      return t('settings.enrichment_runtime.status_labels.applied')
    case 'applying':
      return t('settings.enrichment_runtime.status_labels.reconciling')
    case 'failed':
      return t('settings.enrichment_runtime.status_labels.failed')
    default:
      return normalized || '-'
  }
}

function localizeConditionLabel(value: string, t: TFunction): string {
  switch (value) {
    case 'Converged':
      return t('settings.enrichment_runtime.condition_labels.converged')
    case 'Reconciling':
      return t('settings.enrichment_runtime.condition_labels.reconciling')
    case 'Applied':
      return t('settings.enrichment_runtime.condition_labels.applied')
    case 'Pending resize':
      return t('settings.enrichment_runtime.condition_labels.pending_resize')
    case 'Observed lag':
      return t('settings.enrichment_runtime.condition_labels.observed_lag')
    case 'Degraded':
      return t('settings.enrichment_runtime.condition_labels.degraded')
    case 'Limiter active':
      return t('settings.enrichment_runtime.condition_labels.limiter_active')
    case 'Local limiter mode':
      return t('settings.enrichment_runtime.condition_labels.local_limiter_mode')
    default:
      if (value.startsWith('Degraded ')) {
        return t('settings.enrichment_runtime.condition_labels.degraded_count', {
          count: Number(value.replace('Degraded ', '')),
        })
      }
      if (value.startsWith('Stale ')) {
        return t('settings.enrichment_runtime.condition_labels.stale_count', {
          count: Number(value.replace('Stale ', '')),
        })
      }
      if (value.startsWith('Expired ')) {
        return t('settings.enrichment_runtime.condition_labels.expired_count', {
          count: Number(value.replace('Expired ', '')),
        })
      }
      if (value.startsWith('Applied ')) {
        return t('settings.enrichment_runtime.condition_labels.applied_ratio', {
          value: value.replace('Applied ', ''),
        })
      }
      return value
  }
}

function localizeOperationType(value: string | undefined, t: TFunction): string {
  switch (value) {
    case 'update':
      return t('settings.enrichment_runtime.operation_labels.update')
    case 'reset':
      return t('settings.enrichment_runtime.operation_labels.reset')
    case 'rollback':
      return t('settings.enrichment_runtime.operation_labels.rollback')
    default:
      return value || '-'
  }
}

function localizeRiskLevel(value: string | undefined, t: TFunction): string {
  switch (value) {
    case 'normal':
      return t('settings.enrichment_runtime.risk_labels.normal')
    case 'high':
      return t('settings.enrichment_runtime.risk_labels.high')
    case 'critical':
      return t('settings.enrichment_runtime.risk_labels.critical')
    default:
      return value || '-'
  }
}

function localizeSpecLabel(key: string, fallback: string, t: TFunction): string {
  switch (key) {
    case 'queueLen':
      return t('settings.enrichment_runtime.fields.queue_len')
    case 'workers':
      return t('settings.enrichment_runtime.fields.workers')
    case 'batchSize':
      return t('settings.enrichment_runtime.fields.batch_size')
    case 'batchWindowSeconds':
      return t('settings.enrichment_runtime.fields.batch_window')
    case 'sweepIntervalSeconds':
      return t('settings.enrichment_runtime.fields.sweep_interval')
    case 'llmRateLimitEnabled':
      return t('settings.enrichment_runtime.fields.llm_rate_limit_enabled')
    case 'llmMaxQps':
      return t('settings.enrichment_runtime.fields.llm_max_qps')
    case 'llmBurst':
      return t('settings.enrichment_runtime.fields.llm_burst')
    default:
      return fallback
  }
}

function buildRuntimePosture(spec: EnrichmentRuntimeSpecView, liveInstances: number) {
  return [
    {
      icon: Activity,
      title: '当前吞吐姿态',
      body:
        spec.workers >= Math.max(4, Math.ceil(spec.queueLen / 300))
          ? '当前更偏向积极吞吐，适合快速吃掉积压，但也会更快放大下游压力。'
          : '当前更偏向保守吞吐，适合稳定运行，但面对突发积压时恢复会更慢。',
    },
    {
      icon: TimerReset,
      title: '当前批处理姿态',
      body:
        spec.batchWindowSeconds >= 10 || spec.batchSize >= 20
          ? '当前更偏向效率优先，会增加聚合等待来换取更好的批处理收益。'
          : '当前更偏向响应优先，更快开始处理单条任务，但批处理效率更保守。',
    },
    {
      icon: spec.llmRateLimitEnabled ? ShieldAlert : SlidersHorizontal,
      title: '当前模型调用姿态',
      body: spec.llmRateLimitEnabled
        ? liveInstances > 1
          ? '已开启本地限流，能约束单节点压力，但多个节点不会共享同一份全局额度。'
          : '已开启本地限流，当前节点会主动约束模型调用速率，适合供应商波动或成本敏感场景。'
        : '当前未开启运行时 LLM 限流，更适合追求吞吐，但需要依赖上游服务自身的承载能力。',
    },
  ]
}

function looksOpaqueId(value: string): boolean {
  return (
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value) ||
    /^[a-z0-9_-]{24,}$/i.test(value)
  )
}

function formatRelative(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return formatDistanceToNow(date, { addSuffix: true, locale: zhCN })
}

function formatTimestamp(value: string | undefined, t: TFunction): string {
  if (!value) return t('common.never')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return t('common.never')
  return date.toLocaleString('zh-CN', { hour12: false })
}
