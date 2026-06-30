import { Link } from '@tanstack/react-router'
import { format, formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { AlertCircle, Clock3, Fingerprint, Loader2, RotateCcw, TriangleAlert } from 'lucide-react'
import { type ReactNode, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import type { TerminalFailureWorkbench } from '@/features/feedback/api/get-terminal-failure-workbench'
import { useRetryEnrichment } from '@/features/feedback/api/retry-enrichment'
import {
  selectTerminalFailurePriority,
  type TerminalFailureWorkbenchClusterLike,
  type TerminalFailureWorkbenchSectionLike,
} from '@/features/feedback/lib/terminal-failure-workbench'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { cn } from '@/lib/utils'

interface TerminalFailureWorkbenchPanelProps {
  data?: TerminalFailureWorkbench
  isLoading: boolean
  isError: boolean
  errorMessage?: string
  onRetry: () => void
  onOpenFeedback: (id: string) => void
}

export function TerminalFailureWorkbenchPanel({
  data,
  isLoading,
  isError,
  errorMessage,
  onRetry,
  onOpenFeedback,
}: TerminalFailureWorkbenchPanelProps) {
  const { t } = useTranslation()
  const { can } = usePermissions()
  const canViewLLMConfig = can('llm_config:view')
  const canViewRuntimeConfig = can('settings:enrichment_runtime:view')
  const clusterSections = useMemo<TerminalFailureWorkbenchSectionLike[]>(
    () => [
      {
        key: 'reason_class',
        title: t('feedback.terminal_workbench.reason_class_title'),
        description: t('feedback.terminal_workbench.reason_class_description'),
        clusters: data?.reasonClassClusters ?? [],
        tone: 'danger',
      },
      {
        key: 'model_channel',
        title: t('feedback.terminal_workbench.model_channel_title'),
        description: t('feedback.terminal_workbench.model_channel_description'),
        clusters: data?.modelChannelClusters ?? [],
        tone: 'active',
        remediationPath: canViewLLMConfig ? '/configuration/llm' : undefined,
        remediationLabel: t('feedback.terminal_workbench.remediation_llm'),
      },
      {
        key: 'config_fingerprint',
        title: t('feedback.terminal_workbench.config_fingerprint_title'),
        description: t('feedback.terminal_workbench.config_fingerprint_description'),
        clusters: data?.configFingerprintClusters ?? [],
        tone: 'neutral',
        remediationPath: canViewRuntimeConfig ? '/configuration/enrichment-runtime' : undefined,
        remediationLabel: t('feedback.terminal_workbench.remediation_runtime'),
      },
      {
        key: 'age_bucket',
        title: t('feedback.terminal_workbench.age_bucket_title'),
        description: t('feedback.terminal_workbench.age_bucket_description'),
        clusters: data?.ageBucketClusters ?? [],
        tone: 'success',
      },
    ],
    [
      canViewLLMConfig,
      canViewRuntimeConfig,
      data?.ageBucketClusters,
      data?.configFingerprintClusters,
      data?.modelChannelClusters,
      data?.reasonClassClusters,
      t,
    ],
  )
  const priorityCluster = useMemo(
    () => selectTerminalFailurePriority(clusterSections),
    [clusterSections],
  )
  const priorityClusterKey = priorityCluster
    ? `${priorityCluster.sectionKey}:${priorityCluster.cluster.key}`
    : undefined
  const prioritySectionKey = priorityCluster?.sectionKey

  if (isLoading) {
    return (
      <WorkbenchShell tone="loading">
        <div className="flex items-center gap-3 text-muted-foreground">
          <Loader2 className="size-4 animate-spin" />
          <span>{t('app.loading')}</span>
        </div>
      </WorkbenchShell>
    )
  }

  if (isError) {
    return (
      <WorkbenchShell tone="error">
        <div className="flex items-start gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-2xl border border-destructive/20 bg-destructive/10 text-destructive">
            <AlertCircle className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-base font-semibold text-foreground">
              {t('feedback.terminal_workbench.error_title')}
            </h2>
            <p className="mt-1 text-sm leading-6 text-muted-foreground text-pretty">
              {errorMessage || t('feedback.terminal_workbench.error_body')}
            </p>
            <div className="mt-4">
              <Button type="button" size="sm" variant="outline" onClick={onRetry}>
                <RotateCcw className="size-3.5" />
                {t('common.retry')}
              </Button>
            </div>
          </div>
        </div>
      </WorkbenchShell>
    )
  }

  if (!data) {
    return null
  }

  const totalTerminalFailures = Number(data.totalTerminalFailures)

  if (totalTerminalFailures === 0) {
    return (
      <WorkbenchShell tone="empty">
        <EmptyState
          icon={TriangleAlert}
          title={t('feedback.terminal_workbench.empty_title')}
          description={t('feedback.terminal_workbench.empty_body')}
          className="py-4"
        />
      </WorkbenchShell>
    )
  }

  const oldestAt = data.oldestCreatedAt ? new Date(data.oldestCreatedAt) : null
  const periodStart = new Date(data.periodStart)
  const periodEnd = new Date(data.periodEnd)

  return (
    <WorkbenchShell tone="active">
      <div className="flex flex-col gap-5 xl:flex-row xl:items-end xl:justify-between">
        <div className="min-w-0 max-w-3xl">
          <div className="inline-flex items-center gap-2 rounded-full border border-destructive/15 bg-destructive/8 px-3 py-1 text-[11px] font-semibold tracking-[0.16em] text-destructive uppercase">
            <TriangleAlert className="size-3.5" />
            {t('feedback.terminal_workbench.eyebrow')}
          </div>
          <h2 className="mt-3 text-[1.8rem] font-semibold tracking-tight text-foreground text-balance sm:text-[2rem]">
            {t('feedback.terminal_workbench.title')}
          </h2>
          <p className="mt-2.5 max-w-2xl text-[13.5px] leading-[1.65rem] text-muted-foreground text-pretty">
            {t('feedback.terminal_workbench.description')}
          </p>
        </div>

        <div className="grid gap-2 sm:grid-cols-3 xl:min-w-[34rem]">
          <WorkbenchMetric
            label={t('feedback.terminal_workbench.total')}
            value={String(totalTerminalFailures)}
            hint={t('feedback.terminal_workbench.total_hint')}
            tone="danger"
          />
          <WorkbenchMetric
            label={t('feedback.terminal_workbench.oldest')}
            value={
              oldestAt
                ? formatDistanceToNow(oldestAt, { addSuffix: true, locale: zhCN })
                : t('common.never')
            }
            hint={t('feedback.terminal_workbench.oldest_hint')}
          />
          <WorkbenchMetric
            label={t('feedback.terminal_workbench.window')}
            value={`${format(periodStart, 'MM-dd', { locale: zhCN })} → ${format(periodEnd, 'MM-dd HH:mm', { locale: zhCN })}`}
            hint={t('feedback.terminal_workbench.window_hint')}
          />
        </div>
      </div>

      {priorityCluster ? (
        <WorkbenchPriorityBanner
          cluster={priorityCluster.cluster}
          dimensionLabel={priorityCluster.sectionTitle}
          remediationPath={priorityCluster.remediationPath}
          remediationLabel={priorityCluster.remediationLabel}
          onOpenFeedback={onOpenFeedback}
        />
      ) : null}

      <WorkbenchSectionJumpNav sections={clusterSections} prioritySectionKey={prioritySectionKey} />

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <WorkbenchStat
          label={t('feedback.terminal_workbench.reason_class_title')}
          value={String(data.reasonClassClusters.length)}
          tone="danger"
        />
        <WorkbenchStat
          label={t('feedback.terminal_workbench.model_channel_title')}
          value={String(data.modelChannelClusters.length)}
          tone="active"
        />
        <WorkbenchStat
          label={t('feedback.terminal_workbench.config_fingerprint_title')}
          value={String(data.configFingerprintClusters.length)}
          tone="neutral"
        />
        <WorkbenchStat
          label={t('feedback.terminal_workbench.age_bucket_title')}
          value={String(data.ageBucketClusters.length)}
          tone="success"
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        {clusterSections.map((section) => (
          <WorkbenchClusterCard
            key={section.key}
            sectionKey={section.key}
            title={section.title}
            description={section.description}
            clusters={section.clusters}
            tone={section.tone}
            onOpenFeedback={onOpenFeedback}
            remediationPath={section.remediationPath}
            remediationLabel={section.remediationLabel}
            priorityClusterKey={priorityClusterKey}
          />
        ))}
      </div>
    </WorkbenchShell>
  )
}

function formatTerminalFailureEvidenceTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? '—'
    : `${format(date, 'yyyy-MM-dd HH:mm', { locale: zhCN })} UTC`
}

function WorkbenchShell({
  children,
  tone,
}: {
  children: ReactNode
  tone: 'active' | 'empty' | 'error' | 'loading'
}) {
  const shellClass =
    tone === 'error'
      ? 'border-destructive/20 bg-[linear-gradient(180deg,rgba(255,245,245,0.98),rgba(255,255,255,0.995))]'
      : tone === 'empty'
        ? 'border-amber-200/65 bg-[linear-gradient(180deg,rgba(255,252,245,0.98),rgba(255,255,255,0.995))]'
        : 'border-border/70 bg-[linear-gradient(180deg,rgba(255,250,245,0.98),rgba(255,255,255,0.995))]'
  return (
    <section className="rounded-[1.45rem] border p-2 shadow-[0_24px_70px_-58px_rgba(15,23,42,0.2)]">
      <div className={cn('rounded-[1.28rem] border px-5 py-5 sm:px-6', shellClass)}>{children}</div>
    </section>
  )
}

function WorkbenchMetric({
  label,
  value,
  hint,
  tone = 'neutral',
}: {
  label: string
  value: string
  hint: string
  tone?: 'neutral' | 'danger'
}) {
  return (
    <div
      className={cn(
        'rounded-[1rem] border bg-background/90 px-3 py-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.75)]',
        tone === 'danger' ? 'border-destructive/15' : 'border-border/60',
      )}
    >
      <div className="text-[10px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
        {label}
      </div>
      <div
        className={cn(
          'mt-1 text-sm font-semibold tabular-nums',
          tone === 'danger' && 'text-destructive',
        )}
      >
        {value}
      </div>
      <div className="mt-1 text-xs leading-5 text-muted-foreground">{hint}</div>
    </div>
  )
}

function WorkbenchStat({
  label,
  value,
  tone,
}: {
  label: string
  value: string
  tone: 'danger' | 'active' | 'neutral' | 'success'
}) {
  const toneClass =
    tone === 'danger'
      ? 'border-destructive/15 bg-destructive/[0.05] text-destructive'
      : tone === 'active'
        ? 'border-sky-200/70 bg-sky-50/70 text-sky-700'
        : tone === 'success'
          ? 'border-emerald-200/70 bg-emerald-50/70 text-emerald-700'
          : 'border-border/60 bg-muted/15 text-foreground'
  return (
    <div className={cn('rounded-[1rem] border px-3 py-3', toneClass)}>
      <div className="text-[10px] font-semibold tracking-[0.14em] uppercase">{label}</div>
      <div className="mt-1 text-xl font-semibold tabular-nums">{value}</div>
    </div>
  )
}

function WorkbenchPriorityBanner({
  cluster,
  dimensionLabel,
  remediationPath,
  remediationLabel,
  onOpenFeedback,
}: {
  cluster: TerminalFailureWorkbenchClusterLike
  dimensionLabel: string
  remediationPath?: string
  remediationLabel?: string
  onOpenFeedback: (id: string) => void
}) {
  const { t } = useTranslation()
  const firstSampleId = cluster.sampleFeedbackIds[0]
  return (
    <Card className="gap-0 overflow-hidden rounded-[1.15rem] border border-amber-200/75 bg-amber-50/50 py-0 shadow-none">
      <CardHeader className="border-b border-amber-200/60 px-4 py-4">
        <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
          <div className="min-w-0">
            <div className="inline-flex items-center gap-2 rounded-full border border-amber-300/70 bg-amber-100/80 px-2.5 py-1 text-[11px] font-semibold tracking-[0.14em] text-amber-800 uppercase">
              <TriangleAlert className="size-3.5" />
              {t('feedback.terminal_workbench.priority_title')}
            </div>
            <CardTitle className="mt-3 text-[1rem]">{cluster.label}</CardTitle>
            <CardDescription className="mt-1.5 text-sm leading-6 text-foreground/75">
              {t('feedback.terminal_workbench.priority_body')}
            </CardDescription>
          </div>
          <div className="shrink-0 rounded-full border border-amber-300/70 bg-background/85 px-3 py-1.5 text-xs font-medium text-amber-800">
            {t('feedback.terminal_workbench.priority_scope', { dimension: dimensionLabel })}
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 px-4 py-4">
        <div className="grid gap-2 md:grid-cols-3">
          <WorkbenchEvidenceItem
            label={t('feedback.terminal_workbench.evidence_count')}
            value={t('feedback.terminal_workbench.count', { count: Number(cluster.count) })}
          />
          <WorkbenchEvidenceItem
            label={t('feedback.terminal_workbench.evidence_first_seen')}
            value={formatTerminalFailureEvidenceTime(cluster.oldestCreatedAt)}
          />
          <WorkbenchEvidenceItem
            label={t('feedback.terminal_workbench.evidence_last_seen')}
            value={formatTerminalFailureEvidenceTime(cluster.newestCreatedAt)}
          />
        </div>

        {cluster.remediationHint ? (
          <p className="text-sm leading-6 text-muted-foreground text-pretty">
            {cluster.remediationHint}
          </p>
        ) : null}

        <div className="flex flex-wrap items-center gap-2">
          {firstSampleId ? (
            <Button
              type="button"
              size="sm"
              variant="secondary"
              className="rounded-full px-3 text-xs"
              onClick={() => onOpenFeedback(String(firstSampleId))}
            >
              {t('feedback.queue.priority_action_with_id', { id: String(firstSampleId) })}
            </Button>
          ) : null}
          {remediationPath && remediationLabel ? (
            <Button
              asChild
              type="button"
              size="sm"
              variant="outline"
              className="rounded-full px-3 text-xs"
            >
              <Link to={remediationPath}>{remediationLabel}</Link>
            </Button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  )
}

function WorkbenchSectionJumpNav({
  sections,
  prioritySectionKey,
}: {
  sections: TerminalFailureWorkbenchSectionLike[]
  prioritySectionKey?: TerminalFailureWorkbenchSectionLike['key']
}) {
  const { t } = useTranslation()
  const navItems = useMemo(
    () =>
      sections.map((section) => ({
        sectionKey: section.key,
        title: section.title,
        totalCount: section.clusters.reduce((sum, cluster) => sum + Number(cluster.count), 0),
        isPriority: section.key === prioritySectionKey,
      })),
    [prioritySectionKey, sections],
  )

  return (
    <div className="rounded-[1rem] border border-border/60 bg-background/80 px-3 py-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div className="text-[10px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
          {t('feedback.terminal_workbench.section_jump_title')}
        </div>
        <div className="text-xs text-muted-foreground">
          {t('feedback.terminal_workbench.section_jump_body')}
        </div>
      </div>
      <div className="mt-2 flex flex-wrap gap-2">
        {navItems.map((section) => (
          <Button
            key={section.sectionKey}
            asChild
            type="button"
            size="sm"
            variant={section.isPriority ? 'secondary' : 'outline'}
            className="h-8 rounded-full px-3 text-xs"
          >
            <a href={`#terminal-workbench-${section.sectionKey}`}>
              <span className="truncate">{section.title}</span>
              <span className="text-[10px] font-medium text-muted-foreground">
                {t('feedback.terminal_workbench.count', { count: section.totalCount })}
              </span>
              {section.isPriority ? (
                <span className="rounded-full border border-amber-300/80 bg-amber-100/80 px-1.5 py-0.5 text-[10px] font-semibold tracking-[0.08em] text-amber-800 uppercase">
                  {t('feedback.terminal_workbench.priority_badge')}
                </span>
              ) : null}
            </a>
          </Button>
        ))}
      </div>
    </div>
  )
}

function WorkbenchClusterCard({
  sectionKey,
  title,
  description,
  clusters,
  tone = 'neutral',
  onOpenFeedback,
  remediationPath,
  remediationLabel,
  priorityClusterKey,
}: {
  sectionKey: TerminalFailureWorkbenchSectionLike['key']
  title: string
  description?: string
  clusters: TerminalFailureWorkbenchClusterLike[]
  tone?: 'danger' | 'active' | 'neutral' | 'success'
  onOpenFeedback: (id: string) => void
  remediationPath?: string
  remediationLabel?: string
  priorityClusterKey?: string
}) {
  const { t } = useTranslation()
  const accent =
    tone === 'danger'
      ? 'border-destructive/15 bg-destructive/[0.03]'
      : tone === 'active'
        ? 'border-sky-200/70 bg-sky-50/50'
        : tone === 'success'
          ? 'border-emerald-200/70 bg-emerald-50/50'
          : 'border-border/60 bg-muted/10'

  return (
    <Card
      id={`terminal-workbench-${sectionKey}`}
      className={cn(
        'scroll-mt-24 gap-0 overflow-hidden rounded-[1.15rem] border py-0 shadow-none',
        accent,
      )}
    >
      <CardHeader className="border-b border-border/50 px-4 py-4">
        <CardTitle className="text-[1rem]">{title}</CardTitle>
        <CardDescription className="text-sm leading-6">{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3 px-4 py-4">
        {clusters.length === 0 ? (
          <div className="rounded-[0.85rem] border border-dashed border-border/60 bg-background/70 px-3 py-4 text-sm text-muted-foreground">
            {t('feedback.terminal_workbench.cluster_empty')}
          </div>
        ) : (
          clusters.map((cluster, index) => {
            const clusterKey = `${sectionKey}:${cluster.key}`
            const isOverallPriority = priorityClusterKey === clusterKey
            const isSectionHead = index === 0
            return (
              <div
                key={`${title}-${cluster.key}`}
                className={cn(
                  'rounded-[0.95rem] border px-3 py-3',
                  isOverallPriority
                    ? 'border-amber-300/75 bg-amber-50/75 shadow-[0_0_0_1px_rgba(245,158,11,0.08)]'
                    : isSectionHead
                      ? 'border-sky-200/50 bg-sky-50/35'
                      : 'border-border/60 bg-background/90',
                )}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate text-sm font-semibold text-foreground">
                      {cluster.label}
                    </div>
                    <div className="mt-0.5 flex items-center gap-2 text-[11px] text-muted-foreground">
                      {tone === 'neutral' ? (
                        <Fingerprint className="size-3.5" />
                      ) : (
                        <Clock3 className="size-3.5" />
                      )}
                      <span className="truncate font-mono">{cluster.key}</span>
                    </div>
                  </div>
                  <div className="flex shrink-0 flex-col items-end gap-1">
                    <div className="rounded-full border border-border/60 bg-muted/35 px-2.5 py-1 text-xs font-medium tabular-nums">
                      {t('feedback.terminal_workbench.count', { count: Number(cluster.count) })}
                    </div>
                    {isOverallPriority ? (
                      <span className="rounded-full border border-amber-300/80 bg-amber-100/80 px-2.5 py-1 text-[10px] font-semibold tracking-[0.12em] text-amber-800 uppercase">
                        {t('feedback.terminal_workbench.priority_badge')}
                      </span>
                    ) : isSectionHead ? (
                      <span className="rounded-full border border-sky-200/70 bg-sky-50/90 px-2.5 py-1 text-[10px] font-semibold tracking-[0.12em] text-sky-700 uppercase">
                        {t('feedback.terminal_workbench.section_head_badge')}
                      </span>
                    ) : null}
                  </div>
                </div>

                {cluster.remediationHint ? (
                  <p className="mt-2 text-xs leading-5 text-muted-foreground">
                    {cluster.remediationHint}
                  </p>
                ) : null}

                {isSectionHead ? (
                  <div className="mt-3">
                    <div className="mb-2 text-[11px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
                      {t('feedback.terminal_workbench.evidence_title')}
                    </div>
                    <div className="grid gap-2 sm:grid-cols-3">
                      <WorkbenchEvidenceItem
                        label={t('feedback.terminal_workbench.evidence_first_seen')}
                        value={formatTerminalFailureEvidenceTime(cluster.oldestCreatedAt)}
                      />
                      <WorkbenchEvidenceItem
                        label={t('feedback.terminal_workbench.evidence_last_seen')}
                        value={formatTerminalFailureEvidenceTime(cluster.newestCreatedAt)}
                      />
                      <WorkbenchEvidenceItem
                        label={t('feedback.terminal_workbench.evidence_samples')}
                        value={cluster.sampleFeedbackIds.map((id) => `#${id}`).join(' · ')}
                      />
                    </div>
                  </div>
                ) : null}

                <div className="mt-3 flex flex-wrap gap-1.5">
                  {cluster.sampleFeedbackIds.map((id) => (
                    <WorkbenchSampleActions
                      key={id}
                      id={String(id)}
                      onOpenFeedback={onOpenFeedback}
                    />
                  ))}
                </div>

                {remediationPath && remediationLabel ? (
                  <div className="mt-3 flex items-center justify-between gap-2 border-t border-border/50 pt-3">
                    <span className="text-[11px] font-medium text-muted-foreground">
                      {t('feedback.terminal_workbench.remediation_label')}
                    </span>
                    <Button
                      asChild
                      type="button"
                      size="sm"
                      variant="secondary"
                      className="h-7 rounded-full px-2.5 text-xs"
                    >
                      <Link to={remediationPath}>{remediationLabel}</Link>
                    </Button>
                  </div>
                ) : null}
              </div>
            )
          })
        )}
      </CardContent>
    </Card>
  )
}

function WorkbenchEvidenceItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-[0.85rem] border border-border/50 bg-background/85 px-3 py-2.5">
      <div className="text-[10px] font-semibold tracking-[0.12em] text-muted-foreground uppercase">
        {label}
      </div>
      <div className="mt-1 break-words text-xs font-medium leading-5 text-foreground">{value}</div>
    </div>
  )
}

function WorkbenchSampleActions({
  id,
  onOpenFeedback,
}: {
  id: string
  onOpenFeedback: (id: string) => void
}) {
  const { t } = useTranslation()
  const retry = useRetryEnrichment(id)

  const onRetry = () => {
    retry.mutate(undefined, {
      onSuccess: () => {
        toast.success(t('feedback.detail.retry_enrichment_success'))
      },
      onError: () => {
        toast.error(t('feedback.detail.retry_enrichment_failed'))
      },
    })
  }

  return (
    <>
      <Button
        type="button"
        size="sm"
        variant="ghost"
        className="h-7 rounded-full border border-border/50 bg-background/85 px-2.5 text-xs"
        onClick={() => onOpenFeedback(id)}
      >
        <span className="font-mono">#{id}</span>
      </Button>
      <Button
        type="button"
        size="sm"
        variant="outline"
        className="h-7 rounded-full border-border/50 bg-background/85 px-2.5 text-xs"
        onClick={onRetry}
        disabled={retry.isPending}
      >
        {retry.isPending ? (
          <Loader2 className="size-3.5 animate-spin" />
        ) : (
          <RotateCcw className="size-3.5" />
        )}
        {t('common.retry')}
      </Button>
    </>
  )
}
