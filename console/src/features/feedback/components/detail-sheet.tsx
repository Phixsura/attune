import { useQuery } from '@tanstack/react-query'
import { format, formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Check, Copy, Loader2, RefreshCw, Sparkles } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DimensionChips, UrgentDot } from '@/components/dim/dimension-chips'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  type FeedbackDetail,
  feedbackDetailQuery,
} from '@/features/feedback/api/get-feedback-detail'
import { useRegenerateReplyDraft } from '@/features/feedback/api/regenerate-reply-draft'
import { ConfidenceIndicator } from '@/features/feedback/components/confidence-indicator'
import { LanguageBadge, languagesDiffer } from '@/features/feedback/components/language-badge'
import { useDisplayName } from '@/lib/i18n-resolve'
import { cn } from '@/lib/utils'
import type { Dimension } from '@/proto/attune/v1/common'

// `dims` is supplied by the parent route so this component does not
// cross feature boundaries (the dim set is owned by the settings
// feature). The route already calls enrichConfigQuery once for the
// list/filter UI, so re-using that snapshot here avoids both the
// cross-feature import AND a redundant network call.
export function FeedbackDetailSheet({
  id,
  dims,
  onOpenChange,
}: {
  id: string | null
  dims: Dimension[]
  onOpenChange: (v: boolean) => void
}) {
  const { t } = useTranslation()
  const open = id !== null
  const detail = useQuery({ ...feedbackDetailQuery(id ?? ''), enabled: open })
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-2xl">
        <SheetHeader className="gap-2 border-b px-6 pt-6 pb-4 pr-12">
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
        <div className="space-y-6 px-6 py-6 text-sm">
          {detail.isPending && (
            <div className="flex justify-center py-8 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('app.loading')}
            </div>
          )}
          {detail.data && <DetailBody data={detail.data} dims={dims} />}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function DetailBody({ data, dims }: { data: FeedbackDetail; dims: Dimension[] }) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  const attrs = (data.enrichedAttrs ?? {}) as Record<string, unknown>
  const displayRationale = data.enrichedDisplayRationale || data.enrichedRationale
  const showNativeRationale =
    data.enrichedRationale &&
    data.enrichedDisplayRationale &&
    languagesDiffer(data.language, data.enrichedDisplayLocale) &&
    data.enrichedRationale !== data.enrichedDisplayRationale
  return (
    <div className="space-y-7">
      <Section label={t('feedback.detail.raw_content')}>
        <p className="whitespace-pre-wrap break-words">{data.content}</p>
      </Section>

      {data.replyDraftEnabled ? (
        <ReplyDraftSection
          id={String(data.id)}
          draft={data.replyDraft ?? ''}
          generatedAt={data.replyDraftGeneratedAt ?? ''}
        />
      ) : null}

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

      {dims.length > 0 && (
        <Section label={t('feedback.detail.attrs')}>
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
        </Section>
      )}

      <Section label={t('feedback.detail.source')}>
        <p className="font-mono text-xs text-muted-foreground">
          {data.source} · userId={data.userId || '—'}
          {data.pageUrl && ` · ${data.pageUrl}`}
        </p>
      </Section>

      {data.sourceMeta && Object.keys(data.sourceMeta).length > 0 ? (
        <Section label={t('feedback.detail.sourceMeta')}>
          <pre className="overflow-x-auto rounded-md border bg-muted/40 p-2 text-xs">
            {JSON.stringify(data.sourceMeta, null, 2)}
          </pre>
        </Section>
      ) : null}

      {data.attachments && data.attachments.length > 0 ? (
        <Section label={t('feedback.detail.attachments')}>
          <pre className="overflow-x-auto rounded-md border bg-muted/40 p-2 text-xs">
            {JSON.stringify(data.attachments, null, 2)}
          </pre>
        </Section>
      ) : null}

      {data.enrichmentError ? (
        <Section label={t('feedback.detail.enrichmentError')}>
          <p className="rounded-md border border-destructive/30 bg-destructive/5 p-2 text-xs text-destructive">
            {data.enrichmentError}
          </p>
        </Section>
      ) : null}

      {data.enrichedAt ? (
        <Section label={t('feedback.detail.enrichedAt')}>
          <p className="text-xs text-muted-foreground">
            {format(new Date(data.enrichedAt), 'PPP HH:mm:ss', { locale: zhCN })}
          </p>
        </Section>
      ) : null}
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
    <div>
      <h4 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </h4>
      {children}
    </div>
  )
}
