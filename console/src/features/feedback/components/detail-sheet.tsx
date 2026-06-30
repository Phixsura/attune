import { useQuery } from '@tanstack/react-query'
import { format, formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { AlertCircle, Check, Copy, Loader2, RefreshCw, RotateCcw, Sparkles } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DimensionChips, UrgentDot } from '@/components/dim/dimension-chips'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { WorkflowStateBadge } from '@/components/workflow/workflow-state-badge'
import {
  type FeedbackDetail,
  feedbackDetailQuery,
} from '@/features/feedback/api/get-feedback-detail'
import { useRegenerateReplyDraft } from '@/features/feedback/api/regenerate-reply-draft'
import { useRetryEnrichment } from '@/features/feedback/api/retry-enrichment'
import { ConfidenceIndicator } from '@/features/feedback/components/confidence-indicator'
import { FeedbackTagSection } from '@/features/feedback/components/feedback-tags'
import { LanguageBadge, languagesDiffer } from '@/features/feedback/components/language-badge'
import {
  isTerminalFailure,
  MAX_ENRICHMENT_ATTEMPTS,
} from '@/features/feedback/lib/enrichment-utils'
import { useDisplayName } from '@/lib/i18n-resolve'
import { cn } from '@/lib/utils'
import type { Dimension } from '@/proto/attune/v1/common'
import type { Tag } from '@/proto/attune/v1/tag'

type FeedbackWorkbenchMode = 'all' | 'urgent' | 'active' | 'failed' | 'terminal' | 'ready'

// `dims` is supplied by the parent route so this component does not
// cross feature boundaries (the dim set is owned by the settings
// feature). The route already calls enrichConfigQuery once for the
// list/filter UI, so re-using that snapshot here avoids both the
// cross-feature import AND a redundant network call.
export function FeedbackDetailSheet({
  id,
  dims,
  availableTags,
  workbenchMode = 'all',
  onOpenChange,
  renderWorkflowTransition,
  renderAuditLog,
}: {
  id: string | null
  dims: Dimension[]
  availableTags: Tag[]
  workbenchMode?: FeedbackWorkbenchMode
  onOpenChange: (v: boolean) => void
  renderWorkflowTransition?: (data: FeedbackDetail) => React.ReactNode
  renderAuditLog?: (data: FeedbackDetail) => React.ReactNode
}) {
  const { t } = useTranslation()
  const open = id !== null
  const detail = useQuery({ ...feedbackDetailQuery(id ?? ''), enabled: open })
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full gap-0 overflow-y-auto bg-muted/10 sm:max-w-3xl">
        <SheetHeader className="sticky top-0 z-10 gap-3 border-b bg-background/96 px-6 pt-6 pb-5 pr-12 backdrop-blur">
          <SheetTitle>
            <span className="inline-flex items-center gap-2">
              <UrgentDot urgent={detail.data?.isUrgent} />
              {detail.data
                ? detail.data.enrichedDisplayTitle || detail.data.enrichedTitle || `#${id ?? '?'}`
                : `#${id ?? '?'}`}
            </span>
          </SheetTitle>
          {/* Meta chips render <div>s, so they live in a sibling div — never
              inside SheetDescription, which renders a <p> (block-in-p is an
              invalid-nesting hydration error). The description stays as an
              sr-only line so Radix still has an aria-describedby target. */}
          {detail.data && (
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5 text-xs">
              {workbenchMode !== 'all' ? (
                <span className="inline-flex items-center rounded-full border border-border/60 bg-muted/20 px-2 py-1 text-[11px] font-medium text-foreground">
                  {t('feedback.detail.workbench_mode', {
                    mode: workbenchModeLabel(workbenchMode, (key) => String(t(key))),
                  })}
                </span>
              ) : null}
              {dims.map((dim) => (
                <DimensionChips
                  key={dim.name}
                  dim={dim}
                  value={
                    (detail.data?.enrichedAttrs as Record<string, unknown> | undefined)?.[dim.name]
                  }
                  emptyDash={false}
                />
              ))}
              <LanguageBadge
                language={detail.data.language}
                className="h-5 min-w-8 px-1.5 text-[10px]"
              />
              <span className="text-muted-foreground">
                {format(new Date(detail.data.createdAt), 'PPP HH:mm', { locale: zhCN })}
              </span>
            </div>
          )}
          <SheetDescription className="sr-only">
            {t('feedback.detail.sheet_summary')}
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-5 px-6 py-6 text-sm">
          {detail.isPending && (
            <div className="flex justify-center py-8 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('app.loading')}
            </div>
          )}
          {detail.data && (
            <DetailBody
              data={detail.data}
              dims={dims}
              availableTags={availableTags}
              workbenchMode={workbenchMode}
              renderWorkflowTransition={renderWorkflowTransition}
              renderAuditLog={renderAuditLog}
            />
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function DetailBody({
  data,
  dims,
  availableTags,
  workbenchMode,
  renderWorkflowTransition,
  renderAuditLog,
}: {
  data: FeedbackDetail
  dims: Dimension[]
  availableTags: Tag[]
  workbenchMode: FeedbackWorkbenchMode
  renderWorkflowTransition?: (data: FeedbackDetail) => React.ReactNode
  renderAuditLog?: (data: FeedbackDetail) => React.ReactNode
}) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  const attrs = (data.enrichedAttrs ?? {}) as Record<string, unknown>
  const displayRationale = data.enrichedDisplayRationale || data.enrichedRationale
  const hasClassifiedAttrs = dims.some((dim) => {
    const value = attrs[dim.name]
    if (Array.isArray(value)) return value.length > 0
    return typeof value === 'string' ? value.length > 0 : value != null
  })
  const hasClassificationSignal =
    hasClassifiedAttrs || data.classificationConfidence != null || Boolean(displayRationale)
  const showNativeRationale =
    data.enrichedRationale &&
    data.enrichedDisplayRationale &&
    languagesDiffer(data.language, data.enrichedDisplayLocale) &&
    data.enrichedRationale !== data.enrichedDisplayRationale
  const summaryState = detailSummaryState(data, hasClassificationSignal, t)
  const workbenchCue = detailWorkbenchCue(workbenchMode, hasClassificationSignal, t)
  const hasFailureSnapshot = terminalFailureSnapshotPresent(data)
  return (
    <div className="space-y-5">
      <Card className="border-border/60 shadow-none">
        <CardHeader className="gap-3 pb-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0">
              <CardTitle className="text-base">{t('feedback.detail.sheet_summary')}</CardTitle>
              <p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground text-pretty">
                {data.enrichedDisplayTitle || data.enrichedTitle || data.content}
              </p>
            </div>
            <DetailBadge tone={summaryState.tone} label={summaryState.label} />
          </div>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          <SummaryItem
            label={t('feedback.detail.source')}
            value={data.source || '—'}
            mono={false}
          />
          <SummaryItem
            label={t('feedback.table.language')}
            valueNode={<LanguageBadge language={data.language} />}
          />
          <SummaryItem
            label={t('feedback.detail.workflow_state')}
            valueNode={
              data.workflowState ? (
                <WorkflowStateBadge state={data.workflowState} />
              ) : (
                <span className="text-muted-foreground">—</span>
              )
            }
          />
          <SummaryItem
            label={t('feedback.detail.ai_state')}
            valueNode={<DetailBadge tone={summaryState.tone} label={summaryState.label} compact />}
          />
          <SummaryItem
            label={t('feedback.detail.confidence')}
            valueNode={<ConfidenceIndicator confidence={data.classificationConfidence} />}
          />
          <SummaryItem
            label={t('feedback.detail.enrichedAt')}
            value={
              data.enrichedAt
                ? format(new Date(data.enrichedAt), 'PPP HH:mm:ss', { locale: zhCN })
                : '—'
            }
          />
        </CardContent>
      </Card>

      {!hasClassificationSignal && (
        <EnrichmentStatusBanner data={data} isTerminalFailure={isTerminalFailure(data)} />
      )}

      {workbenchCue ? (
        <DetailWorkbenchBanner
          eyebrow={t('feedback.detail.workbench_title')}
          title={workbenchCue.title}
          body={workbenchCue.body}
          tone={workbenchCue.tone}
        />
      ) : null}

      {hasFailureSnapshot ? (
        <Section label={t('feedback.detail.failure_snapshot')}>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            <SummaryItem
              label={t('feedback.detail.failure_reason_class')}
              value={terminalFailureReasonClassLabel(data.enrichmentFailureReasonClass, t)}
            />
            <SummaryItem
              label={t('feedback.detail.failure_model')}
              value={data.enrichmentFailureModel || '—'}
              mono
            />
            <SummaryItem
              label={t('feedback.detail.failure_channel')}
              valueNode={
                <div className="space-y-1">
                  <div>{data.enrichmentFailureChannelName || '—'}</div>
                  {data.enrichmentFailureChannelId ? (
                    <div className="font-mono text-xs text-muted-foreground">
                      {data.enrichmentFailureChannelId}
                    </div>
                  ) : null}
                </div>
              }
            />
            <SummaryItem
              label={t('feedback.detail.failure_config_fingerprint')}
              value={data.enrichmentFailureConfigFingerprint || '—'}
              mono
            />
            <SummaryItem
              label={t('feedback.detail.failure_prompt_version')}
              value={data.enrichmentFailurePromptVersion || '—'}
              mono
            />
          </div>
        </Section>
      ) : data.enrichmentStatus === 'failed' ? (
        <DetailStateBanner
          tone="muted"
          title={t('feedback.detail.failure_snapshot')}
          body={t('feedback.detail.failure_snapshot_missing')}
        />
      ) : null}

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)]">
        <Section label={t('feedback.detail.raw_content')}>
          <p className="whitespace-pre-wrap break-words leading-7">{data.content}</p>
        </Section>

        {dims.length > 0 && (
          <Section label={t('feedback.detail.attrs')}>
            {hasClassificationSignal ? (
              <dl className="space-y-3">
                <div className="flex items-start gap-3">
                  <dt className="w-28 shrink-0 text-xs text-muted-foreground">
                    {t('feedback.detail.confidence')}
                  </dt>
                  <dd className="flex-1 text-sm">
                    <ConfidenceIndicator confidence={data.classificationConfidence} />
                  </dd>
                </div>
                {dims.map((dim) => (
                  <div key={dim.name} className="flex items-start gap-3">
                    <dt className="w-28 shrink-0 text-xs text-muted-foreground">
                      {displayOf(dim.displayName) || dim.name}
                    </dt>
                    <dd className="flex-1 text-sm">
                      <DimensionChips dim={dim} value={attrs[dim.name]} />
                    </dd>
                  </div>
                ))}
              </dl>
            ) : (
              <div className="rounded-lg border border-dashed border-border/70 bg-muted/10 px-4 py-4 text-sm text-muted-foreground">
                {t('feedback.row.no_classification')}
              </div>
            )}
          </Section>
        )}
      </div>

      {displayRationale ? (
        <Section label={t('feedback.detail.ai_rationale')}>
          <p className="rounded-md border border-border bg-muted/40 p-3 whitespace-pre-wrap break-words text-muted-foreground">
            {displayRationale}
          </p>
        </Section>
      ) : null}

      {showNativeRationale ? (
        <Section label={t('feedback.detail.ai_rationale_source')}>
          <p className="rounded-md border border-border bg-background p-3 whitespace-pre-wrap break-words text-muted-foreground">
            {data.enrichedRationale}
          </p>
        </Section>
      ) : null}

      {data.replyDraftEnabled ? (
        <ReplyDraftSection
          id={String(data.id)}
          draft={data.replyDraft ?? ''}
          generatedAt={data.replyDraftGeneratedAt ?? ''}
        />
      ) : null}

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(0,0.92fr)]">
        <div className="space-y-5">
          <Section label={t('tags.feedback_section.title')}>
            <FeedbackTagSection
              feedbackId={String(data.id)}
              tags={data.tags ?? []}
              availableTags={availableTags}
              hideHeader
            />
          </Section>

          {renderWorkflowTransition && (
            <Section label={t('feedback.detail.workflow_state')}>
              {renderWorkflowTransition(data)}
            </Section>
          )}

          {data.enrichmentError && hasClassificationSignal ? (
            <Section label={t('feedback.detail.enrichmentError')}>
              <p className="rounded-md border border-destructive/30 bg-destructive/5 p-2 text-xs text-destructive">
                {data.enrichmentError}
              </p>
            </Section>
          ) : null}
        </div>

        <div className="space-y-5">
          <Section label={t('feedback.detail.source')}>
            <dl className="space-y-3">
              <FactRow label={t('feedback.detail.source')} value={data.source || '—'} mono />
              <FactRow label="userId" value={data.userId || '—'} mono />
              {data.pageUrl ? (
                <FactRow label="URL" value={data.pageUrl} className="break-all" />
              ) : null}
              {data.enrichedAt ? (
                <FactRow
                  label={t('feedback.detail.enrichedAt')}
                  value={format(new Date(data.enrichedAt), 'PPP HH:mm:ss', { locale: zhCN })}
                />
              ) : null}
            </dl>
          </Section>

          {data.sourceMeta && Object.keys(data.sourceMeta).length > 0 ? (
            <JsonSection label={t('feedback.detail.sourceMeta')} value={data.sourceMeta} />
          ) : null}

          {data.attachments && data.attachments.length > 0 ? (
            <JsonSection label={t('feedback.detail.attachments')} value={data.attachments} />
          ) : null}

          {renderAuditLog && (
            <Section label={t('feedback.detail.audit_log')}>{renderAuditLog(data)}</Section>
          )}
        </div>
      </div>
    </div>
  )
}

// ReplyDraftSection shows the operator-facing LLM draft with Copy and
// Regenerate. It renders whenever the tenant has reply-draft enabled, so an
// enabled-but-empty row (the confidence gate skipped auto-generation, or a
// prior generation degraded to an empty draft) still offers a Generate entry
// point. The draft is a suggestion only — never auto-sent.
//
// This is the one *actionable* artifact in an otherwise read-only sheet, so it
// earns a brand-tinted surface (the same vocabulary the enrichment-error block
// uses with a destructive tint) and full interactive-state cycles: a skeleton
// while the LLM runs, a composed empty state, and an inline copied confirmation.
// Copy is the primary action (filled), Regenerate drops to ghost.
function ReplyDraftSection({
  id,
  draft,
  generatedAt,
}: {
  id: string
  draft: string
  generatedAt: string
}) {
  const { t } = useTranslation()
  const regen = useRegenerateReplyDraft(id)
  const [justCopied, setJustCopied] = useState(false)
  const copyTimer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(copyTimer.current), [])

  // Explicit empty check: an empty string is a valid state and must NOT fall
  // through to a stale prop value via `??`. A fresh regenerate response wins
  // over the prop once it lands.
  const current = regen.data ? regen.data.replyDraft : draft
  const stamp = regen.data ? regen.data.replyDraftGeneratedAt : generatedAt
  const ago = relativeTime(stamp)
  const hasDraft = current !== ''
  const pending = regen.isPending

  const onCopy = () => {
    navigator.clipboard
      .writeText(current)
      .then(() => {
        toast.success(t('feedback.detail.reply_draft_copied'))
        setJustCopied(true)
        copyTimer.current = window.setTimeout(() => setJustCopied(false), 1500)
      })
      .catch(() => toast.error(t('feedback.detail.reply_draft_copy_failed')))
  }
  const onRegenerate = () => {
    regen.mutate(undefined, {
      onError: (err) => {
        // A 429 is the per-row cooldown backstop — message it distinctly so the
        // operator knows to wait, not that generation broke.
        const status = (err as { status?: number }).status
        toast.error(
          status === 429
            ? t('feedback.detail.reply_draft_cooldown')
            : t('feedback.detail.reply_draft_failed'),
        )
      },
    })
  }

  return (
    <div>
      <div className="mb-2 flex items-center gap-2">
        <Sparkles className="size-3.5 text-primary" />
        <h4 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t('feedback.detail.reply_draft')}
        </h4>
        {hasDraft && ago ? (
          <span className="ml-auto text-[11px] text-muted-foreground">
            {t('feedback.detail.reply_draft_generated_at', { ago })}
          </span>
        ) : null}
      </div>

      <div className="rounded-md border border-primary/20 bg-primary/[0.04] p-4">
        {pending ? (
          <DraftSkeleton />
        ) : hasDraft ? (
          <p className="whitespace-pre-wrap break-words leading-relaxed">{current}</p>
        ) : (
          <div className="flex items-start gap-3">
            <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-primary/10">
              <Sparkles className="size-4 text-primary" />
            </div>
            <div className="space-y-0.5">
              <p className="text-muted-foreground">{t('feedback.detail.reply_draft_empty')}</p>
              <p className="text-xs text-muted-foreground/80">
                {t('feedback.detail.reply_draft_empty_hint')}
              </p>
            </div>
          </div>
        )}

        <div className="mt-4 flex items-center gap-1.5">
          {hasDraft ? (
            <Button
              type="button"
              size="sm"
              onClick={onCopy}
              disabled={pending}
              className="motion-safe:active:scale-[0.98]"
            >
              {justCopied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
              {justCopied
                ? t('feedback.detail.reply_draft_copied_short')
                : t('feedback.detail.reply_draft_copy')}
            </Button>
          ) : null}
          <Button
            type="button"
            size="sm"
            variant={hasDraft ? 'ghost' : 'default'}
            onClick={onRegenerate}
            disabled={pending}
            className={cn('motion-safe:active:scale-[0.98]', hasDraft && 'text-muted-foreground')}
          >
            {pending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : hasDraft ? (
              <RefreshCw className="size-3.5" />
            ) : (
              <Sparkles className="size-3.5" />
            )}
            {hasDraft
              ? t('feedback.detail.reply_draft_regenerate')
              : t('feedback.detail.reply_draft_generate')}
          </Button>
        </div>
      </div>
    </div>
  )
}

// relativeTime renders a server timestamp as "x ago", or null when the value is
// absent or unparseable — so a malformed value degrades to no line instead of
// throwing RangeError on new Date(...) and blanking the whole sheet.
function relativeTime(stamp: string): string | null {
  if (!stamp) return null
  const d = new Date(stamp)
  return Number.isNaN(d.getTime())
    ? null
    : formatDistanceToNow(d, { addSuffix: true, locale: zhCN })
}

// DraftSkeleton mimics the shape of generated prose (a few ragged lines) so the
// LLM round-trip reads as "drafting", not a generic spinner.
function DraftSkeleton() {
  return (
    <div className="space-y-2" aria-hidden>
      <Skeleton className="h-3.5 w-[92%]" />
      <Skeleton className="h-3.5 w-full" />
      <Skeleton className="h-3.5 w-[76%]" />
    </div>
  )
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <section className="rounded-xl border border-border/60 bg-background p-4">
      <h4 className="mb-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </h4>
      {children}
    </section>
  )
}

function SummaryItem({
  label,
  value,
  valueNode,
  mono,
}: {
  label: string
  value?: string
  valueNode?: React.ReactNode
  mono?: boolean
}) {
  return (
    <div className="space-y-1.5 rounded-lg border border-border/50 bg-muted/10 px-3 py-3">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className={cn('text-sm text-foreground', mono && 'font-mono text-xs')}>
        {valueNode ?? value}
      </div>
    </div>
  )
}

function DetailBadge({
  tone,
  label,
  compact = false,
}: {
  tone: 'default' | 'error' | 'success' | 'muted'
  label: string
  compact?: boolean
}) {
  const toneClass =
    tone === 'error'
      ? 'border-destructive/20 bg-destructive/5 text-destructive'
      : tone === 'success'
        ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
        : tone === 'muted'
          ? 'border-border/60 bg-muted/10 text-muted-foreground'
          : 'border-border/60 bg-background text-foreground'
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border font-medium',
        compact ? 'px-2 py-1 text-[11px]' : 'px-2.5 py-1.5 text-xs',
        toneClass,
      )}
    >
      {label}
    </span>
  )
}

function FactRow({
  label,
  value,
  mono,
  className,
}: {
  label: string
  value: string
  mono?: boolean
  className?: string
}) {
  return (
    <div className="flex items-start gap-3">
      <dt className="w-24 shrink-0 text-[11px] font-medium uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </dt>
      <dd
        className={cn(
          'min-w-0 flex-1 text-sm text-foreground',
          mono && 'font-mono text-xs',
          className,
        )}
      >
        {value}
      </dd>
    </div>
  )
}

function DetailStateBanner({
  tone,
  title,
  body,
  detail,
}: {
  tone: 'error' | 'muted'
  title: string
  body: string
  detail?: string
}) {
  const toneClass =
    tone === 'error'
      ? 'border-destructive/25 bg-destructive/[0.04]'
      : 'border-border/60 bg-muted/12'
  const iconClass = tone === 'error' ? 'text-destructive' : 'text-muted-foreground'
  return (
    <div className={`rounded-xl border px-4 py-4 ${toneClass}`}>
      <div className="flex items-start gap-3">
        <div
          className={`flex size-9 shrink-0 items-center justify-center rounded-2xl border border-current/10 bg-background/85 ${iconClass}`}
        >
          <AlertCircle className="size-4" />
        </div>
        <div className="min-w-0">
          <h4 className="text-sm font-semibold text-foreground">{title}</h4>
          <p className="mt-1 text-sm leading-6 text-muted-foreground text-pretty">{body}</p>
          {detail ? (
            <p className="mt-3 rounded-lg border border-border/50 bg-background/80 px-3 py-2 font-mono text-xs text-muted-foreground">
              {detail}
            </p>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function DetailWorkbenchBanner({
  eyebrow,
  title,
  body,
  tone,
}: {
  eyebrow: string
  title: string
  body: string
  tone: 'default' | 'warning' | 'danger' | 'success'
}) {
  const toneClass =
    tone === 'danger'
      ? 'border-amber-200/80 bg-amber-50/65'
      : tone === 'warning'
        ? 'border-sky-200/80 bg-sky-50/65'
        : tone === 'success'
          ? 'border-emerald-200/80 bg-emerald-50/65'
          : 'border-border/60 bg-muted/[0.06]'
  return (
    <div className={`rounded-xl border px-4 py-4 ${toneClass}`}>
      <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
        {eyebrow}
      </div>
      <div className="mt-1 text-sm font-semibold text-foreground">{title}</div>
      <p className="mt-1 text-sm leading-6 text-muted-foreground text-pretty">{body}</p>
    </div>
  )
}

function detailSummaryState(
  data: FeedbackDetail,
  hasClassificationSignal: boolean,
  t: (key: string) => string,
) {
  if (data.enrichmentError) {
    return { tone: 'error' as const, label: t('feedback.row.classification_failed') }
  }
  if (hasClassificationSignal) {
    return { tone: 'success' as const, label: t('feedback.row.classification_ready') }
  }
  if (data.enrichmentStatus === 'enriching') {
    return { tone: 'muted' as const, label: t('feedback.row.classification_enriching') }
  }
  if (data.enrichmentStatus === 'pending') {
    return { tone: 'muted' as const, label: t('feedback.row.classification_pending') }
  }
  return { tone: 'muted' as const, label: t('feedback.row.unclassified_short') }
}

function workbenchModeLabel(mode: FeedbackWorkbenchMode, t: (key: string) => string) {
  if (mode === 'urgent') return t('feedback.queue_mode.urgent')
  if (mode === 'active') return t('feedback.queue_mode.active')
  if (mode === 'failed') return t('feedback.queue_mode.failed')
  if (mode === 'terminal') return t('feedback.queue_mode.terminal')
  if (mode === 'ready') return t('feedback.queue_mode.ready')
  return t('feedback.queue_mode.all')
}

function detailWorkbenchCue(
  mode: FeedbackWorkbenchMode,
  hasClassificationSignal: boolean,
  t: (key: string) => string,
) {
  if (mode === 'all') return null
  if (mode === 'failed') {
    return {
      tone: 'danger' as const,
      title: t('feedback.detail.workbench_failed_title'),
      body: t('feedback.detail.workbench_failed_body'),
    }
  }
  if (mode === 'terminal') {
    return {
      tone: 'danger' as const,
      title: t('feedback.detail.workbench_terminal_title'),
      body: t('feedback.detail.workbench_terminal_body'),
    }
  }
  if (mode === 'active') {
    return {
      tone: 'warning' as const,
      title: t('feedback.detail.workbench_active_title'),
      body: t('feedback.detail.workbench_active_body'),
    }
  }
  if (mode === 'urgent') {
    return {
      tone: 'danger' as const,
      title: t('feedback.detail.workbench_urgent_title'),
      body: t('feedback.detail.workbench_urgent_body'),
    }
  }
  if (mode === 'ready') {
    return {
      tone: hasClassificationSignal ? ('success' as const) : ('warning' as const),
      title: t('feedback.detail.workbench_ready_title'),
      body: hasClassificationSignal
        ? t('feedback.detail.workbench_ready_body')
        : t('feedback.detail.workbench_ready_missing_body'),
    }
  }
  return null
}

function terminalFailureSnapshotPresent(data: FeedbackDetail) {
  return Boolean(
    data.enrichmentFailureReasonClass ||
      data.enrichmentFailureModel ||
      data.enrichmentFailureChannelId ||
      data.enrichmentFailureChannelName ||
      data.enrichmentFailureConfigFingerprint ||
      data.enrichmentFailurePromptVersion,
  )
}

function terminalFailureReasonClassLabel(
  reasonClass: string | undefined | null,
  t: (key: string) => string,
) {
  if (reasonClass === 'llm_err') return t('feedback.detail.failure_reason_class_llm')
  if (reasonClass === 'parse_err') return t('feedback.detail.failure_reason_class_parse')
  if (reasonClass === 'other_err') return t('feedback.detail.failure_reason_class_other')
  return reasonClass || '—'
}

function ErrorMessageBlock({ error }: { error: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const timerRef = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(timerRef.current), [])

  const onCopy = () => {
    navigator.clipboard.writeText(error).then(() => {
      setCopied(true)
      timerRef.current = window.setTimeout(() => setCopied(false), 1500)
    })
  }

  return (
    <div className="mt-3 group relative rounded-lg border border-border/50 bg-background/80">
      <pre className="overflow-x-auto px-3 py-2 pr-10 font-mono text-xs text-muted-foreground whitespace-pre-wrap break-words">
        {error}
      </pre>
      <button
        type="button"
        onClick={onCopy}
        className="absolute right-2 top-2 rounded p-1 text-muted-foreground/60 opacity-0 transition-opacity hover:bg-muted hover:text-foreground group-hover:opacity-100"
        title={t('common.copy')}
      >
        {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      </button>
    </div>
  )
}

function JsonSection({ label, value }: { label: string; value: unknown }) {
  return (
    <Section label={label}>
      <details className="group">
        <summary className="cursor-pointer text-xs font-medium text-muted-foreground">详情</summary>
        <pre className="mt-3 overflow-x-auto rounded-md border bg-muted/30 p-3 text-xs">
          {JSON.stringify(value, null, 2)}
        </pre>
      </details>
    </Section>
  )
}

function EnrichmentStatusBanner({
  data,
  isTerminalFailure: terminal,
}: {
  data: FeedbackDetail
  isTerminalFailure: boolean
}) {
  const { t } = useTranslation()
  const retry = useRetryEnrichment(String(data.id))

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

  if (!data.enrichmentError) {
    return (
      <DetailStateBanner
        tone="muted"
        title={t('feedback.detail.pending_classification_title')}
        body={t('feedback.detail.pending_classification_body')}
      />
    )
  }

  const attempts = data.enrichmentAttempts ?? 0
  const nextRetry = data.enrichmentNextRetryAt

  return (
    <div className="rounded-xl border border-destructive/25 bg-destructive/[0.04] px-4 py-4">
      <div className="flex items-start gap-3">
        <div className="flex size-9 shrink-0 items-center justify-center rounded-2xl border border-current/10 bg-background/85 text-destructive">
          <AlertCircle className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <h4 className="text-sm font-semibold text-foreground">
            {terminal
              ? t('feedback.detail.terminal_failure_title')
              : t('feedback.detail.enrichment_failed_title')}
          </h4>
          <p className="mt-1 text-sm leading-6 text-muted-foreground text-pretty">
            {terminal
              ? t('feedback.detail.terminal_failure_body', { count: attempts })
              : t('feedback.detail.enrichment_failed_body')}
          </p>

          <div className="mt-3 space-y-3">
            <div className="flex items-center gap-3">
              <span className="text-xs text-muted-foreground shrink-0">
                {t('feedback.detail.enrichment_attempts')}
              </span>
              <div className="flex items-center gap-2 flex-1">
                <div className="flex gap-1">
                  {[0, 1, 2, 3, 4].map((idx) => (
                    <div
                      key={idx}
                      className={cn(
                        'size-2 rounded-full transition-colors',
                        idx < attempts
                          ? terminal
                            ? 'bg-destructive'
                            : 'bg-amber-500'
                          : 'bg-muted-foreground/20',
                      )}
                    />
                  ))}
                </div>
                <span className="text-sm font-medium tabular-nums">
                  {attempts}/{MAX_ENRICHMENT_ATTEMPTS}
                </span>
              </div>
            </div>
            <dl className="flex flex-wrap gap-x-6 gap-y-2 text-sm">
              {nextRetry && (
                <div className="flex items-center gap-2">
                  <dt className="text-xs text-muted-foreground">
                    {t('feedback.detail.enrichment_next_retry')}:
                  </dt>
                  <dd className="font-medium">
                    {formatDistanceToNow(new Date(nextRetry), { addSuffix: true, locale: zhCN })}
                  </dd>
                </div>
              )}
              {terminal && (
                <div className="flex items-center gap-2">
                  <dt className="text-xs text-muted-foreground">
                    {t('feedback.detail.enrichment_next_retry')}:
                  </dt>
                  <dd className="font-medium text-destructive">
                    {t('feedback.detail.enrichment_terminal')}
                  </dd>
                </div>
              )}
            </dl>
          </div>

          {data.enrichmentError && <ErrorMessageBlock error={data.enrichmentError} />}

          {terminal && (
            <div className="mt-4">
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={onRetry}
                disabled={retry.isPending}
                className="motion-safe:active:scale-[0.98]"
              >
                {retry.isPending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <RotateCcw className="size-3.5" />
                )}
                {t('feedback.detail.retry_enrichment')}
              </Button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
