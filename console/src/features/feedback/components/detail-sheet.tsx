import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { format, formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import type { TFunction } from 'i18next'
import {
  AlertCircle,
  Check,
  CheckCircle,
  ClipboardList,
  Copy,
  ExternalLink,
  GitCompareArrows,
  History,
  ListChecks,
  Loader2,
  PencilLine,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  Send,
  ShieldCheck,
  Sparkles,
  Trash2,
  XCircle,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DimensionChips, UrgentDot } from '@/components/dim/dimension-chips'
import { Loading } from '@/components/loading'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
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
import {
  useApproveReplyDraft,
  useRegenerateReplyDraft,
  useRejectReplyDraft,
  useSendReplyDraft,
  useUpdateReplyDraft,
} from '@/features/feedback/api/regenerate-reply-draft'
import { useRetryEnrichment } from '@/features/feedback/api/retry-enrichment'
import {
  type LinkedRequestRef,
  type SimilarFeedbackItem,
  similarFeedbackQuery,
} from '@/features/feedback/api/similar-feedback'
import { ConfidenceIndicator } from '@/features/feedback/components/confidence-indicator'
import { FeedbackTagSection } from '@/features/feedback/components/feedback-tags'
import { LanguageBadge, languagesDiffer } from '@/features/feedback/components/language-badge'
import {
  isTerminalFailure,
  MAX_ENRICHMENT_ATTEMPTS,
} from '@/features/feedback/lib/enrichment-utils'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { useRestoreFocusOnClose } from '@/hooks/use-restore-focus-on-close'
import {
  customerRequestsInfiniteQuery,
  useLinkCustomerRequestFeedback,
  useUnlinkCustomerRequestFeedback,
} from '@/lib/customer-request-api'
import { restoreFocusWhenReady } from '@/lib/focus'
import { useDisplayName } from '@/lib/i18n-resolve'
import { cn } from '@/lib/utils'
import type { Dimension } from '@/proto/attune/v1/common'
import {
  CustomerRequestImportance,
  CustomerRequestPriority,
  CustomerRequestSort,
  CustomerRequestStatus,
  type CustomerRequestSummary,
  CustomerRequestVisibility,
  SortDirection,
} from '@/proto/attune/v1/customer_request'
import type { ReplyDraftWorkflow } from '@/proto/attune/v1/ingest'
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
  restoreFocusRef,
  renderWorkflowTransition,
  renderAuditLog,
}: {
  id: string | null
  dims: Dimension[]
  availableTags: Tag[]
  workbenchMode?: FeedbackWorkbenchMode
  onOpenChange: (v: boolean) => void
  restoreFocusRef?: React.RefObject<HTMLElement | null>
  renderWorkflowTransition?: (data: FeedbackDetail) => React.ReactNode
  renderAuditLog?: (data: FeedbackDetail) => React.ReactNode
}) {
  const { t } = useTranslation()
  const open = id !== null
  const detail = useQuery({ ...feedbackDetailQuery(id ?? ''), enabled: open })
  const internalRestoreFocusRef = useRef<HTMLElement | null>(null)
  const focusRestoreRef = restoreFocusRef ?? internalRestoreFocusRef
  useRestoreFocusOnClose(open, focusRestoreRef)
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className="w-full gap-0 overflow-y-auto bg-background sm:max-w-[min(1120px,calc(100vw-2rem))]"
        onOpenAutoFocus={() => {
          if (focusRestoreRef.current?.isConnected) return
          const active = document.activeElement
          focusRestoreRef.current =
            active instanceof HTMLElement && active !== document.body ? active : null
        }}
        onCloseAutoFocus={(event) => {
          const restoreFocusTo = focusRestoreRef.current
          if (!restoreFocusTo?.isConnected) return
          event.preventDefault()
          restoreFocusWhenReady(restoreFocusTo)
          focusRestoreRef.current = null
        }}
      >
        <SheetHeader className="sticky top-0 z-10 gap-3 border-b bg-background px-6 pt-6 pb-5 pr-12">
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
          {detail.isPending && <Loading />}
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
  const permissions = usePermissions()
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
  const portalSubmission = portalSubmissionMeta(data.sourceMeta)
  const canPromoteCustomerRequest = permissions.can('customer_request:edit')
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
          workflow={data.replyDraftWorkflow}
          source={data.source || '—'}
          sourceUser={data.userId || '—'}
          pageUrl={data.pageUrl || ''}
          rawContent={data.content}
          rationale={displayRationale || ''}
          confidence={data.classificationConfidence}
          enrichedAt={data.enrichedAt || ''}
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

          {isPositiveIntString(String(data.id)) ? (
            <CustomerRequestLinksSection feedbackId={String(data.id)} />
          ) : null}

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
          {portalSubmission ? (
            <PortalSubmissionSection
              feedbackId={String(data.id)}
              canPromoteCustomerRequest={canPromoteCustomerRequest}
              submission={portalSubmission}
            />
          ) : null}

          {supportChannelCandidate(data.source, data.sourceMeta) && canPromoteCustomerRequest ? (
            <SupportPromoteCard
              feedbackId={String(data.id)}
              candidate={supportChannelCandidate(data.source, data.sourceMeta)}
            />
          ) : null}

          <Section label={t('feedback.detail.source')}>
            <dl className="space-y-3">
              <FactRow label={t('feedback.detail.source')} value={data.source || '—'} mono />
              <FactRow label="userId" value={data.userId || '—'} mono />
              {data.pageUrl ? (
                <FactRow
                  label="URL"
                  valueNode={
                    <a
                      href={data.pageUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 break-all text-primary underline-offset-2 hover:underline"
                    >
                      <span>{data.pageUrl}</span>
                      <ExternalLink className="size-3.5 shrink-0" />
                    </a>
                  }
                  className="break-all"
                />
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

function CustomerRequestLinksSection({ feedbackId }: { feedbackId: string }) {
  const { t } = useTranslation()
  const permissions = usePermissions()
  const canView = permissions.can('customer_request:view')
  const canEdit = permissions.can('customer_request:edit')
  const [query, setQuery] = useState('')
  const [selectedRequestID, setSelectedRequestID] = useState('')
  const linked = useInfiniteQuery({
    ...customerRequestsInfiniteQuery({
      feedbackId,
      visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL,
      sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT,
      direction: SortDirection.SORT_DIRECTION_DESC,
    }),
    enabled: canView,
  })
  const candidates = useInfiniteQuery({
    ...customerRequestsInfiniteQuery({
      q: query.trim() || undefined,
      visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE,
      sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT,
      direction: SortDirection.SORT_DIRECTION_DESC,
    }),
    enabled: canView && canEdit,
  })
  const link = useLinkCustomerRequestFeedback(selectedRequestID)

  if (!canView) return null

  const linkedItems = linked.data?.pages.flatMap((page) => page.requests) ?? []
  const linkedIDs = new Set(linkedItems.map((item) => item.id))
  const candidateItems = (candidates.data?.pages.flatMap((page) => page.requests) ?? [])
    .filter((item) => !linkedIDs.has(item.id))
    .slice(0, 6)
  const selected = candidateItems.find((item) => item.id === selectedRequestID)
  const canSubmit = canEdit && Boolean(selectedRequestID) && !link.isPending

  const onSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!canSubmit) return
    link.mutate(
      {
        feedbackId,
        importance: CustomerRequestImportance.CUSTOMER_REQUEST_IMPORTANCE_NORMAL,
      },
      {
        onSuccess: () => {
          toast.success(t('customer_requests.feedback_link_success'))
          setQuery('')
          setSelectedRequestID('')
        },
        onError: (err) =>
          toast.error(
            err instanceof Error ? err.message : t('customer_requests.feedback_link_failed'),
          ),
      },
    )
  }

  return (
    <Section label={t('customer_requests.feedback_linked_requests')}>
      <div className="space-y-3">
        {linked.isPending ? (
          <RequestLinkSkeleton />
        ) : linked.isError ? (
          <div className="flex items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm">
            <span className="text-destructive">{t('customer_requests.load_failed')}</span>
            <Button type="button" size="sm" variant="ghost" onClick={() => void linked.refetch()}>
              <RefreshCw className="size-3.5" />
              {t('common.retry')}
            </Button>
          </div>
        ) : linkedItems.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border/70 bg-muted/10 px-4 py-4 text-sm text-muted-foreground">
            {t('customer_requests.feedback_linked_empty')}
          </div>
        ) : (
          <div className="space-y-2">
            {linkedItems.map((item) => (
              <LinkedCustomerRequestRow
                key={item.id}
                item={item}
                feedbackId={feedbackId}
                canEdit={canEdit}
              />
            ))}
          </div>
        )}

        {canEdit ? (
          <form onSubmit={onSubmit} className="rounded-lg border border-border/70 bg-muted/10 p-3">
            <div className="flex flex-col gap-2 sm:flex-row">
              <div className="relative min-w-0 flex-1">
                <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={query}
                  onChange={(event) => {
                    setQuery(event.target.value)
                    setSelectedRequestID('')
                  }}
                  aria-label={t('customer_requests.feedback_link_search')}
                  placeholder={t('customer_requests.feedback_link_search_placeholder')}
                  className="pl-8"
                />
              </div>
              <Button type="submit" disabled={!canSubmit} className="shrink-0">
                {link.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Plus className="size-4" />
                )}
                {t('customer_requests.feedback_link_action')}
              </Button>
            </div>

            <div className="mt-3 space-y-1.5">
              {selected ? (
                <div className="text-xs text-muted-foreground">
                  {t('customer_requests.feedback_link_selected', {
                    displayId: selected.displayId,
                  })}
                </div>
              ) : null}
              {candidates.isPending ? (
                <div className="rounded-md border border-border/60 bg-background px-3 py-2 text-sm text-muted-foreground">
                  {t('common.loading')}
                </div>
              ) : candidateItems.length === 0 ? (
                <div className="rounded-md border border-dashed border-border/60 bg-background px-3 py-2 text-sm text-muted-foreground">
                  {t('customer_requests.feedback_link_search_empty')}
                </div>
              ) : (
                candidateItems.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    className={cn(
                      'flex w-full items-start gap-3 rounded-md border border-border/60 bg-background px-3 py-2 text-left transition hover:border-primary/40 hover:bg-muted/30',
                      selectedRequestID === item.id && 'border-primary bg-primary/5',
                    )}
                    onClick={() => setSelectedRequestID(item.id)}
                  >
                    <ClipboardList className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1">
                      <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                        <span className="font-mono text-xs font-semibold">{item.displayId}</span>
                        <span className="text-xs text-muted-foreground">
                          {customerRequestStatusLabel(item.status, t)} ·{' '}
                          {customerRequestPriorityLabel(item.priority, t)}
                        </span>
                      </span>
                      <span className="mt-0.5 line-clamp-2 text-sm">{item.title}</span>
                    </span>
                  </button>
                ))
              )}
            </div>
          </form>
        ) : null}
      </div>
    </Section>
  )
}

function LinkedCustomerRequestRow({
  item,
  feedbackId,
  canEdit,
}: {
  item: CustomerRequestSummary
  feedbackId: string
  canEdit: boolean
}) {
  const { t } = useTranslation()
  const unlink = useUnlinkCustomerRequestFeedback(item.id)
  return (
    <div className="flex items-start justify-between gap-3 rounded-lg border border-border/70 bg-background px-3 py-2">
      <div className="min-w-0 space-y-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="font-mono text-xs font-semibold">{item.displayId}</span>
          <span className="text-xs text-muted-foreground">
            {customerRequestStatusLabel(item.status, t)} ·{' '}
            {customerRequestPriorityLabel(item.priority, t)}
          </span>
        </div>
        <p className="line-clamp-2 text-sm font-medium">{item.title}</p>
        <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span>
            {t('customer_requests.feedback_count', { count: item.supportingFeedbackCount })}
          </span>
          <span>{t('customer_requests.customer_count', { count: item.customerCount })}</span>
          <span>{relativeTime(item.updatedAt) ?? item.updatedAt}</span>
        </div>
      </div>
      {canEdit ? (
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={t('customer_requests.feedback_unlink_action')}
          disabled={unlink.isPending}
          onClick={() =>
            unlink.mutate(feedbackId, {
              onSuccess: () => toast.success(t('customer_requests.feedback_unlink_success')),
              onError: (err) =>
                toast.error(
                  err instanceof Error
                    ? err.message
                    : t('customer_requests.feedback_unlink_failed'),
                ),
            })
          }
        >
          {unlink.isPending ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Trash2 className="size-4" />
          )}
        </Button>
      ) : null}
    </div>
  )
}

function RequestLinkSkeleton() {
  return (
    <div className="space-y-2">
      <Skeleton className="h-20 rounded-lg" />
      <Skeleton className="h-20 rounded-lg" />
    </div>
  )
}

function ReplyDraftSection({
  id,
  draft,
  generatedAt,
  workflow,
  source,
  sourceUser,
  pageUrl,
  rawContent,
  rationale,
  confidence,
  enrichedAt,
}: {
  id: string
  draft: string
  generatedAt: string
  workflow?: ReplyDraftWorkflow
  source: string
  sourceUser: string
  pageUrl: string
  rawContent: string
  rationale: string
  confidence?: number
  enrichedAt: string
}) {
  const { t } = useTranslation()
  const regen = useRegenerateReplyDraft(id)
  const updateDraft = useUpdateReplyDraft(id)
  const approveDraft = useApproveReplyDraft(id)
  const rejectDraft = useRejectReplyDraft(id)
  const sendDraft = useSendReplyDraft(id)
  const [justCopied, setJustCopied] = useState(false)
  const [isEditing, setIsEditing] = useState(false)
  const [preflightOpen, setPreflightOpen] = useState(false)
  const [editorText, setEditorText] = useState('')
  const [latestWorkflow, setLatestWorkflow] = useState<ReplyDraftWorkflow | undefined>()
  const copyTimer = useRef<number | undefined>(undefined)
  const workflowSnapshotKey = `${id}:${workflow?.draftId ?? ''}:${workflow?.revision ?? ''}`
  useEffect(() => () => window.clearTimeout(copyTimer.current), [])
  useEffect(() => {
    if (workflowSnapshotKey) setLatestWorkflow(undefined)
  }, [workflowSnapshotKey])

  const currentWorkflow = latestWorkflow ?? workflow
  const current = currentWorkflow?.activeText ?? (regen.data ? regen.data.replyDraft : draft)
  const stamp = regen.data ? regen.data.replyDraftGeneratedAt : generatedAt
  const ago = relativeTime(stamp)
  const hasDraft = current !== ''
  const allowed = new Set(currentWorkflow?.allowedActions ?? [])
  const status = currentWorkflow?.status ?? (hasDraft ? 'suggested' : '')
  const blockers = currentWorkflow?.blockers ?? []
  const canSend = !isEditing && hasDraft && allowed.has('send')
  const canApprove = !isEditing && hasDraft && allowed.has('approve')
  const canEdit = !isEditing && hasDraft && allowed.has('edit')
  const canReject = !isEditing && hasDraft && allowed.has('reject')
  const canRegenerate = !isEditing && (!currentWorkflow || allowed.has('regenerate'))
  const pending =
    regen.isPending ||
    updateDraft.isPending ||
    approveDraft.isPending ||
    rejectDraft.isPending ||
    sendDraft.isPending

  useEffect(() => {
    if (!isEditing) setEditorText(current)
  }, [current, isEditing])

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
      onSuccess: (next) => {
        if (isCompleteReplyDraftWorkflow(next.workflow)) setLatestWorkflow(next.workflow)
      },
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

  const onSave = () => {
    updateDraft.mutate(
      { content: editorText, expectedRevision: currentWorkflow?.revision ?? '0' },
      {
        onSuccess: (next) => {
          if (isCompleteReplyDraftWorkflow(next.workflow)) setLatestWorkflow(next.workflow)
          setIsEditing(false)
          toast.success(t('feedback.detail.reply_draft_saved'))
        },
        onError: () => toast.error(t('feedback.detail.reply_draft_save_failed')),
      },
    )
  }

  const onApprove = () => {
    approveDraft.mutate(currentWorkflow?.revision ?? '0', {
      onSuccess: (next) => {
        if (isCompleteReplyDraftWorkflow(next.workflow)) setLatestWorkflow(next.workflow)
        toast.success(t('feedback.detail.reply_draft_approved'))
      },
      onError: () => toast.error(t('feedback.detail.reply_draft_approve_failed')),
    })
  }

  const onReject = () => {
    rejectDraft.mutate(currentWorkflow?.revision ?? '0', {
      onSuccess: (next) => {
        if (isCompleteReplyDraftWorkflow(next.workflow)) setLatestWorkflow(next.workflow)
        toast.success(t('feedback.detail.reply_draft_rejected'))
      },
      onError: () => toast.error(t('feedback.detail.reply_draft_reject_failed')),
    })
  }

  const onSendConfirmed = () => {
    sendDraft.mutate(currentWorkflow?.revision ?? '0', {
      onSuccess: (next) => {
        if (isCompleteReplyDraftWorkflow(next.workflow)) setLatestWorkflow(next.workflow)
        setPreflightOpen(false)
        toast.success(t('feedback.detail.reply_draft_sent'))
      },
      onError: (err) => {
        const status = (err as { status?: number }).status
        toast.error(
          status === 409
            ? t('feedback.detail.reply_draft_send_blocked')
            : t('feedback.detail.reply_draft_send_failed'),
        )
      },
    })
  }

  return (
    <section className="overflow-hidden rounded-xl border border-border/70 bg-background">
      <div className="border-b border-border/60 bg-muted/20 px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <Sparkles className="size-3.5 text-primary" />
          <h3 className="text-sm font-semibold text-foreground">
            {t('feedback.detail.reply_draft')}
          </h3>
          <span className="text-xs text-muted-foreground">
            {t('feedback.detail.reply_draft_workspace')}
          </span>
          {status ? (
            <span className="rounded-full border border-border/70 bg-background px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
              {t(`feedback.detail.reply_draft_status_${status}`, { defaultValue: status })}
            </span>
          ) : null}
          {hasDraft && ago ? (
            <span className="ml-auto text-[11px] text-muted-foreground">
              {t('feedback.detail.reply_draft_generated_at', { ago })}
            </span>
          ) : null}
        </div>
      </div>

      <div className="grid gap-0 xl:grid-cols-[minmax(0,1.08fr)_minmax(19rem,0.92fr)]">
        <div data-testid="reply-draft-surface" className="bg-muted p-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <div>
              <div className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                {t('feedback.detail.reply_draft_composer')}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">
                {t('feedback.detail.reply_draft_composer_hint')}
              </div>
            </div>
            {currentWorkflow ? (
              <div className="shrink-0 rounded-md border border-border/60 bg-background px-2 py-1 font-mono text-[11px] text-muted-foreground">
                rev {currentWorkflow.revision}
              </div>
            ) : null}
          </div>

          <div className="rounded-lg border border-border/70 bg-background p-3">
            {pending ? (
              <DraftSkeleton />
            ) : isEditing ? (
              <textarea
                value={editorText}
                onChange={(event) => setEditorText(event.target.value)}
                className="min-h-44 w-full resize-y rounded-md border border-border bg-background px-3 py-2 text-sm leading-relaxed outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/15"
              />
            ) : hasDraft ? (
              <p className="whitespace-pre-wrap break-words leading-relaxed">{current}</p>
            ) : (
              <div className="flex items-start gap-3">
                <div className="flex size-8 shrink-0 items-center justify-center rounded-md bg-primary/10">
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
          </div>

          {blockers.length > 0 ? (
            <div className="mt-3 rounded-md border border-amber-300/50 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
              {blockers
                .map((blocker) => t(`feedback.detail.reply_draft_blocker_${blocker}`))
                .join(' · ')}
            </div>
          ) : null}

          <div className="mt-4 flex flex-wrap items-center gap-1.5">
            {isEditing ? (
              <>
                <Button
                  type="button"
                  size="sm"
                  onClick={onSave}
                  disabled={pending || editorText.trim() === ''}
                  className="motion-safe:active:scale-[0.98]"
                >
                  <Check className="size-3.5" />
                  {t('feedback.detail.reply_draft_save')}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setEditorText(current)
                    setIsEditing(false)
                  }}
                  disabled={pending}
                >
                  <XCircle className="size-3.5" />
                  {t('feedback.detail.reply_draft_cancel')}
                </Button>
              </>
            ) : hasDraft ? (
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
            {canEdit ? (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => {
                  setEditorText(current)
                  setIsEditing(true)
                }}
                disabled={pending}
              >
                <PencilLine className="size-3.5" />
                {t('feedback.detail.reply_draft_edit')}
              </Button>
            ) : null}
            {canApprove ? (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={onApprove}
                disabled={pending}
              >
                <CheckCircle className="size-3.5" />
                {t('feedback.detail.reply_draft_approve')}
              </Button>
            ) : null}
            {canSend ? (
              <Button
                type="button"
                size="sm"
                onClick={() => setPreflightOpen(true)}
                disabled={pending}
              >
                {sendDraft.isPending ? (
                  <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
                ) : (
                  <Send className="size-3.5" />
                )}
                {t('feedback.detail.reply_draft_send')}
              </Button>
            ) : null}
            {canReject ? (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={onReject}
                disabled={pending}
                className="text-destructive hover:text-destructive"
              >
                <XCircle className="size-3.5" />
                {t('feedback.detail.reply_draft_reject')}
              </Button>
            ) : null}
            {canRegenerate ? (
              <Button
                type="button"
                size="sm"
                variant={hasDraft ? 'ghost' : 'default'}
                onClick={onRegenerate}
                disabled={pending}
                className={cn(
                  'motion-safe:active:scale-[0.98]',
                  hasDraft && 'text-muted-foreground',
                )}
              >
                {pending ? (
                  <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
                ) : hasDraft ? (
                  <RefreshCw className="size-3.5" />
                ) : (
                  <Sparkles className="size-3.5" />
                )}
                {hasDraft
                  ? t('feedback.detail.reply_draft_regenerate')
                  : t('feedback.detail.reply_draft_generate')}
              </Button>
            ) : null}
          </div>

          {currentWorkflow ? (
            <ReplyDraftLayerSummary workflow={currentWorkflow} activeText={current} />
          ) : null}
        </div>

        <aside
          data-testid="reply-draft-evidence-panel"
          className="space-y-4 border-t border-border/60 bg-background p-4 xl:border-t-0 xl:border-l"
        >
          {currentWorkflow ? (
            <ReplyDraftPreflightChecklist workflow={currentWorkflow} blockers={blockers} />
          ) : null}
          <ReplyDraftEvidencePanel
            source={source}
            sourceUser={sourceUser}
            pageUrl={pageUrl}
            rawContent={rawContent}
            rationale={rationale}
            confidence={confidence}
            enrichedAt={enrichedAt}
          />
          {currentWorkflow ? <ReplyDraftTimeline workflow={currentWorkflow} /> : null}
        </aside>
      </div>

      {currentWorkflow ? (
        <ReplyDraftPreflightDialog
          open={preflightOpen}
          onOpenChange={setPreflightOpen}
          workflow={currentWorkflow}
          text={current}
          blockers={blockers}
          source={source}
          sourceUser={sourceUser}
          pending={sendDraft.isPending}
          onConfirm={onSendConfirmed}
        />
      ) : null}
    </section>
  )
}

function ReplyDraftLayerSummary({
  workflow,
  activeText,
}: {
  workflow: ReplyDraftWorkflow
  activeText: string
}) {
  const { t } = useTranslation()
  const ai = latestRevisionByOrigin(workflow, 'ai')
  const human = latestRevisionByOrigin(workflow, 'human')
  const sent =
    revisionByID(workflow, workflow.sentRevisionId) ??
    (workflow.status === 'sent'
      ? (revisionByID(workflow, workflow.approvedRevisionId) ??
        revisionByID(workflow, workflow.activeRevisionId))
      : undefined)
  const layers = [
    {
      key: 'ai',
      label: t('feedback.detail.reply_draft_layer_ai'),
      content: ai?.content ?? '',
      meta: ai ? `v${ai.revisionNo}` : '',
    },
    {
      key: 'human',
      label: t('feedback.detail.reply_draft_layer_human'),
      content: human?.content ?? '',
      meta: human ? `v${human.revisionNo}` : '',
    },
    {
      key: 'sent',
      label: t('feedback.detail.reply_draft_layer_sent'),
      content: sent?.content ?? (workflow.status === 'sent' ? activeText : ''),
      meta: workflow.status === 'sent' ? t('feedback.detail.reply_draft_status_sent') : '',
    },
  ].filter((layer) => layer.content !== '')

  if (layers.length < 2) return null

  return (
    <div className="mt-4 space-y-3">
      <div className="grid gap-2 sm:grid-cols-3">
        {layers.map((layer) => (
          <div
            key={layer.key}
            className="min-w-0 rounded-md border border-border/60 bg-background p-2"
          >
            <div className="mb-1 flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
              <span className="font-medium">{layer.label}</span>
              {layer.meta ? <span>{layer.meta}</span> : null}
            </div>
            <p className="line-clamp-2 whitespace-pre-wrap break-words text-xs text-muted-foreground">
              {layer.content}
            </p>
          </div>
        ))}
      </div>
      {ai && human ? <ReplyDraftDiffSummary ai={ai} human={human} /> : null}
    </div>
  )
}

function ReplyDraftDiffSummary({
  ai,
  human,
}: {
  ai: ReplyDraftRevision
  human: ReplyDraftRevision
}) {
  const { t } = useTranslation()
  const changed = ai.content.trim() !== human.content.trim()
  const delta = human.content.length - ai.content.length
  return (
    <div
      data-testid="reply-draft-diff"
      className="rounded-md border border-border/70 bg-background px-3 py-2"
    >
      <div className="mb-1 flex items-center gap-2 text-[11px] font-medium uppercase tracking-[0.12em] text-muted-foreground">
        <GitCompareArrows className="size-3.5" />
        {t('feedback.detail.reply_draft_diff')}
      </div>
      <p className="text-xs text-muted-foreground">
        {changed
          ? t('feedback.detail.reply_draft_diff_changed', {
              count: Math.abs(delta),
              direction:
                delta >= 0
                  ? t('feedback.detail.reply_draft_diff_longer')
                  : t('feedback.detail.reply_draft_diff_shorter'),
            })
          : t('feedback.detail.reply_draft_diff_unchanged')}
      </p>
    </div>
  )
}

function ReplyDraftPreflightChecklist({
  workflow,
  blockers,
}: {
  workflow: ReplyDraftWorkflow
  blockers: string[]
}) {
  const { t } = useTranslation()
  const checks = [
    {
      key: 'approved',
      label: t('feedback.detail.reply_draft_preflight_approved'),
      pass: Boolean(workflow.approvedRevisionId) || workflow.status === 'approved',
    },
    {
      key: 'hook',
      label: t('feedback.detail.reply_draft_preflight_hook'),
      pass: workflow.hookConfigured,
    },
    {
      key: 'fresh',
      label: t('feedback.detail.reply_draft_preflight_fresh'),
      pass: !blockers.some((blocker) => blocker.includes('stale') || blocker.includes('changed')),
    },
    {
      key: 'revision',
      label: t('feedback.detail.reply_draft_preflight_revision'),
      pass: Boolean(workflow.activeRevisionId && workflow.revision),
    },
  ]
  return (
    <div className="rounded-lg border border-border/70 bg-muted/10 p-3">
      <div className="mb-3 flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        <ListChecks className="size-3.5" />
        {t('feedback.detail.reply_draft_preflight')}
      </div>
      <div className="space-y-2">
        {checks.map((check) => (
          <ReplyDraftCheckRow key={check.key} label={check.label} pass={check.pass} />
        ))}
      </div>
    </div>
  )
}

function ReplyDraftCheckRow({ label, pass }: { label: string; pass: boolean }) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <span
        className={cn(
          'inline-flex size-5 shrink-0 items-center justify-center rounded-full border',
          pass
            ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-200'
            : 'border-amber-300/60 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200',
        )}
      >
        {pass ? <Check className="size-3" /> : <AlertCircle className="size-3" />}
      </span>
      <span className="text-muted-foreground">{label}</span>
    </div>
  )
}

function ReplyDraftEvidencePanel({
  source,
  sourceUser,
  pageUrl,
  rawContent,
  rationale,
  confidence,
  enrichedAt,
}: {
  source: string
  sourceUser: string
  pageUrl: string
  rawContent: string
  rationale: string
  confidence?: number
  enrichedAt: string
}) {
  const { t } = useTranslation()
  return (
    <div className="rounded-lg border border-border/70 bg-background p-3">
      <div className="mb-3 flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        <ShieldCheck className="size-3.5" />
        {t('feedback.detail.reply_draft_evidence')}
      </div>
      <dl className="space-y-2">
        <CompactFact label={t('feedback.detail.source')} value={source} mono />
        <CompactFact label="userId" value={sourceUser} mono />
        <CompactFact
          label={t('feedback.detail.confidence')}
          value={typeof confidence === 'number' ? `${Math.round(confidence * 100)}%` : '—'}
        />
        <CompactFact
          label={t('feedback.detail.enrichedAt')}
          value={relativeTime(enrichedAt) ?? '—'}
        />
        {pageUrl ? <CompactFact label="URL" value={pageUrl} /> : null}
      </dl>
      <EvidenceExcerpt label={t('feedback.detail.reply_draft_evidence_raw')} value={rawContent} />
      {rationale ? (
        <EvidenceExcerpt
          label={t('feedback.detail.reply_draft_evidence_rationale')}
          value={rationale}
        />
      ) : null}
    </div>
  )
}

function CompactFact({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] gap-2 text-xs">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn('min-w-0 break-words text-foreground', mono && 'font-mono')}>{value}</dd>
    </div>
  )
}

function EvidenceExcerpt({ label, value }: { label: string; value: string }) {
  return (
    <div className="mt-3 rounded-md border border-border/60 bg-muted/20 p-2">
      <div className="mb-1 text-[11px] font-medium text-muted-foreground">{label}</div>
      <p className="line-clamp-4 whitespace-pre-wrap break-words text-xs leading-5 text-muted-foreground">
        {value}
      </p>
    </div>
  )
}

function ReplyDraftTimeline({ workflow }: { workflow: ReplyDraftWorkflow }) {
  const { t } = useTranslation()
  const items = replyDraftTimelineItems(workflow)
  if (items.length === 0) return null
  return (
    <div data-testid="reply-draft-timeline" className="rounded-lg border border-border/70 p-3">
      <div className="mb-3 flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        <History className="size-3.5" />
        {t('feedback.detail.reply_draft_history')}
      </div>
      <div className="space-y-3">
        {items.slice(0, 6).map((item) => (
          <div key={item.key} className="grid grid-cols-[0.75rem_minmax(0,1fr)] gap-2">
            <span className="mt-1.5 size-2 rounded-full bg-primary/70" aria-hidden />
            <div>
              <div className="flex items-center justify-between gap-2 text-xs">
                <span className="font-medium text-foreground">
                  {item.origin
                    ? `${t(`feedback.detail.reply_draft_origin_${item.origin}`, {
                        defaultValue: item.origin,
                      })} v${item.revisionNo}`
                    : item.label}
                </span>
                <span className="shrink-0 text-[11px] text-muted-foreground">
                  {relativeTime(item.createdAt)}
                </span>
              </div>
              {item.detail ? (
                <p className="mt-0.5 line-clamp-2 whitespace-pre-wrap break-words text-xs text-muted-foreground">
                  {item.detail}
                </p>
              ) : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function replyDraftTimelineItems(workflow: ReplyDraftWorkflow) {
  const revisionItems = (workflow.revisions ?? []).map((revision) => ({
    key: revision.id,
    origin: revision.origin,
    revisionNo: revision.revisionNo,
    label: '',
    detail: revision.content,
    createdAt: revision.createdAt,
  }))
  const eventItems = (workflow.events ?? []).map((event) => ({
    key: event.id,
    origin: '',
    revisionNo: 0,
    label: event.eventType,
    detail: event.blocker ?? '',
    createdAt: event.createdAt,
  }))
  return [...eventItems, ...revisionItems].sort(
    (a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime(),
  )
}

function ReplyDraftPreflightDialog({
  open,
  onOpenChange,
  workflow,
  text,
  blockers,
  source,
  sourceUser,
  pending,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  workflow: ReplyDraftWorkflow
  text: string
  blockers: string[]
  source: string
  sourceUser: string
  pending: boolean
  onConfirm: () => void
}) {
  const { t } = useTranslation()
  const hardBlockers = blockers.filter(isReplyDraftHardBlocker)
  const blocked = hardBlockers.length > 0 || !workflow.hookConfigured
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="reply-send-preflight" className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('feedback.detail.reply_draft_preflight_title')}</DialogTitle>
          <DialogDescription>{t('feedback.detail.reply_draft_preflight_body')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 md:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
          <ReplyDraftPreflightChecklist workflow={workflow} blockers={blockers} />
          <div className="rounded-lg border border-border/70 bg-muted/20 p-3">
            <div className="mb-2 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
              {t('feedback.detail.reply_draft_preflight_snapshot')}
            </div>
            <dl className="space-y-2">
              <CompactFact label={t('feedback.detail.source')} value={source} mono />
              <CompactFact label="userId" value={sourceUser} mono />
              <CompactFact
                label={t('feedback.detail.reply_draft_preflight_revision_id')}
                value={workflow.approvedRevisionId ?? workflow.activeRevisionId ?? '—'}
                mono
              />
              <CompactFact
                label={t('feedback.detail.reply_draft_preflight_actor')}
                value={workflow.approvedBy ?? workflow.editedBy ?? workflow.generatedBy ?? '—'}
                mono
              />
            </dl>
          </div>
        </div>
        <div className="rounded-lg border border-border/70 bg-background p-3">
          <div className="mb-2 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            {t('feedback.detail.reply_draft_preflight_final_text')}
          </div>
          <p className="max-h-52 overflow-auto whitespace-pre-wrap break-words text-sm leading-6">
            {text}
          </p>
        </div>
        {blockers.length > 0 ? (
          <div className="rounded-md border border-amber-300/50 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
            {blockers
              .map((blocker) => t(`feedback.detail.reply_draft_blocker_${blocker}`))
              .join(' · ')}
          </div>
        ) : null}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" onClick={onConfirm} disabled={pending || blocked}>
            {pending ? (
              <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
            ) : (
              <Send className="size-3.5" />
            )}
            {t('feedback.detail.reply_draft_confirm_send')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function isReplyDraftHardBlocker(blocker: string): boolean {
  return (
    blocker === 'reply_send_hook_missing' ||
    blocker === 'send_in_progress' ||
    blocker === 'send_hook_changed' ||
    blocker === 'stale_source' ||
    blocker === 'source_changed' ||
    blocker === 'unknown_status'
  )
}

type ReplyDraftRevision = ReplyDraftWorkflow['revisions'][number]

function latestRevisionByOrigin(
  workflow: ReplyDraftWorkflow,
  origin: string,
): ReplyDraftRevision | undefined {
  return (workflow.revisions ?? []).find((revision) => revision.origin === origin)
}

function revisionByID(
  workflow: ReplyDraftWorkflow,
  id: string | undefined,
): ReplyDraftRevision | undefined {
  if (!id) return undefined
  return (workflow.revisions ?? []).find((revision) => revision.id === id)
}

function isCompleteReplyDraftWorkflow(
  workflow: ReplyDraftWorkflow | undefined,
): workflow is ReplyDraftWorkflow {
  return Boolean(workflow?.draftId && Array.isArray(workflow.revisions))
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

function isPositiveIntString(value: string) {
  return /^[1-9]\d*$/.test(value.trim())
}

function customerRequestStatusLabel(status: CustomerRequestStatus, t: TFunction) {
  switch (status) {
    case CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED:
      return t('customer_requests.statuses.planned')
    case CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_IN_PROGRESS:
      return t('customer_requests.statuses.in_progress')
    case CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_SHIPPED:
      return t('customer_requests.statuses.shipped')
    case CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_CANCELLED:
      return t('customer_requests.statuses.cancelled')
    default:
      return t('customer_requests.statuses.open')
  }
}

function customerRequestPriorityLabel(priority: CustomerRequestPriority, t: TFunction) {
  switch (priority) {
    case CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_LOW:
      return t('customer_requests.priorities.low')
    case CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_MEDIUM:
      return t('customer_requests.priorities.medium')
    case CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH:
      return t('customer_requests.priorities.high')
    case CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT:
      return t('customer_requests.priorities.urgent')
    default:
      return t('customer_requests.priorities.none')
  }
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
      <h3 className="mb-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </h3>
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
  valueNode,
  mono,
  className,
}: {
  label: string
  value?: string
  valueNode?: React.ReactNode
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
        {valueNode ?? value ?? '—'}
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
          <h3 className="text-sm font-semibold text-foreground">{title}</h3>
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

type PortalSubmissionEvidence = {
  kind: string
  title: string
  details: string
  pageUrl?: string
  privateContact?: Record<string, unknown>
  customFields?: Record<string, unknown>
  userAgent?: string
}

function portalSubmissionMeta(
  sourceMeta: FeedbackDetail['sourceMeta'],
): PortalSubmissionEvidence | null {
  if (!isPortalRecord(sourceMeta)) return null
  const raw = sourceMeta.portal_submission
  if (!isPortalRecord(raw)) return null

  const kind = portalSubmissionText(raw.kind, true)
  const title = portalSubmissionText(raw.title, true)
  const details = portalSubmissionText(raw.details, true)
  const pageUrl = portalSubmissionText(raw.page_url, true)
  const userAgent = portalSubmissionText(raw.user_agent, true)
  const privateContact =
    isPortalRecord(raw.private_contact) && Object.keys(raw.private_contact).length > 0
      ? raw.private_contact
      : undefined
  const customFields =
    isPortalRecord(raw.custom_fields) && Object.keys(raw.custom_fields).length > 0
      ? raw.custom_fields
      : undefined

  if (!kind && !title && !details && !pageUrl && !userAgent && !privateContact && !customFields) {
    return null
  }

  return {
    kind: kind || '—',
    title: title || '—',
    details: details || '—',
    ...(pageUrl ? { pageUrl } : {}),
    ...(privateContact ? { privateContact } : {}),
    ...(customFields ? { customFields } : {}),
    ...(userAgent ? { userAgent } : {}),
  }
}

type SupportCandidate = {
  channel: string
  customer: string
  company: string
  signal: 'fin_escalated' | 'priority' | 'default'
}

// supportChannelCandidate decides whether a support-channel feedback row
// should surface a "promote to customer request" candidate card, and
// with what urgency signal. Fin escalations and priority conversations
// are the strongest request-candidate indicators.
function supportChannelCandidate(
  source: string,
  sourceMeta: FeedbackDetail['sourceMeta'],
): SupportCandidate | null {
  if (source !== 'intercom' && source !== 'zendesk') return null
  if (!isPortalRecord(sourceMeta)) return null
  const prefix = `${source}_`
  const text = (key: string): string => {
    const v = sourceMeta[prefix + key]
    if (typeof v === 'string') return v.trim()
    if (typeof v === 'number') return String(v)
    return ''
  }
  const customer =
    text('contact_name') ||
    text('requester_name') ||
    text('contact_email') ||
    text('requester_email')
  const company = text('company_name') || text('organization_name')
  let signal: SupportCandidate['signal'] = 'default'
  if (
    text('ai_resolution_state') === 'escalated' ||
    text('ai_resolution_state') === 'negative_feedback'
  ) {
    signal = 'fin_escalated'
  } else if (
    text('priority') === 'priority' ||
    text('priority') === 'urgent' ||
    text('priority') === 'high'
  ) {
    signal = 'priority'
  }
  return { channel: source, customer, company, signal }
}

// promoteIDList joins the anchor feedback with its UNTRACKED recurrence
// neighbors so one click promotes the whole recurring signal as
// evidence. Neighbors already linked to a customer request are excluded
// — bundling them into a new request would double-track them.
function promoteIDList(feedbackId: string, similar: SimilarFeedbackItem[]): string {
  const ids = [
    feedbackId,
    ...similar.filter((s) => !s.linked_requests?.length).map((s) => String(s.id)),
  ]
  return [...new Set(ids)].join(',')
}

// existingRequestFor picks the customer request already tracking this
// cluster — the dedup target. The anchor's own links win outright (if
// THIS feedback is already tracked, never offer a duplicate promote);
// otherwise the highest-similarity neighbor with a link wins, and within
// one row's links the most-recently-updated request is first (backend
// order).
function existingRequestFor(
  anchorLinks: LinkedRequestRef[],
  similar: SimilarFeedbackItem[],
): LinkedRequestRef | null {
  if (anchorLinks[0]) return anchorLinks[0]
  for (const item of similar) {
    const ref = item.linked_requests?.[0]
    if (ref) return ref
  }
  return null
}

function SupportPromoteCard({
  feedbackId,
  candidate,
}: {
  feedbackId: string
  candidate: SupportCandidate | null
}) {
  const { t } = useTranslation()
  const enabled = !!candidate && isPositiveIntString(feedbackId)
  const similar = useQuery({ ...similarFeedbackQuery(feedbackId), enabled })
  const anchorLinks = similar.data?.anchor_linked_requests ?? []
  const anchorAlreadyLinked = anchorLinks.length > 0
  const existingRequest = existingRequestFor(anchorLinks, similar.data?.items ?? [])
  const linkExisting = useLinkCustomerRequestFeedback(existingRequest?.id ?? '')
  if (!candidate || !enabled) return null
  const signalKey =
    candidate.signal === 'fin_escalated'
      ? 'feedback.detail.support_promote_signal_fin'
      : candidate.signal === 'priority'
        ? 'feedback.detail.support_promote_signal_priority'
        : 'feedback.detail.support_promote_signal_default'
  const who = [candidate.customer, candidate.company].filter(Boolean).join(' · ')
  const neighbors = similar.data?.items ?? []
  const promoteIDs = promoteIDList(feedbackId, neighbors)

  // Hold the card until the dedup data arrives — rendering the promote
  // action during the pending window would flash past the terminal
  // state for an already-tracked anchor.
  if (similar.isPending) {
    return (
      <div className="rounded-lg border border-primary/15 bg-primary/5 px-4 py-4">
        <div className="text-xs font-semibold uppercase tracking-[0.12em] text-primary">
          {t('feedback.detail.support_promote_title')}
        </div>
        <div className="mt-2 flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-3.5 animate-spin" />
          {t('common.loading')}
        </div>
      </div>
    )
  }

  // Terminal state: this feedback is already tracked by a customer
  // request — the card's job (get the signal tracked) is done. Offering
  // any promote here would create the duplicate the card exists to
  // prevent; the linked-requests section above shows the tracking CR.
  if (anchorAlreadyLinked) {
    return (
      <div className="rounded-lg border border-border/60 bg-muted/20 px-4 py-3">
        <div className="text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
          {t('feedback.detail.support_promote_title')}
        </div>
        <p className="mt-1 text-sm leading-6 text-muted-foreground">
          {t('feedback.detail.support_promote_already_tracked', {
            cr: `CR-${anchorLinks[0].cr_no}`,
            title: anchorLinks[0].title,
          })}
        </p>
      </div>
    )
  }
  return (
    <div className="rounded-lg border border-primary/15 bg-primary/5 px-4 py-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="text-xs font-semibold uppercase tracking-[0.12em] text-primary">
            {t('feedback.detail.support_promote_title')}
          </div>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">
            {t(signalKey)}
            {who ? ` ${t('feedback.detail.support_promote_from', { who })}` : ''}
          </p>
          {neighbors.length > 0 ? (
            <div className="mt-2 space-y-1">
              <p className="text-xs font-medium text-primary">
                {t('feedback.detail.support_promote_recurring', { count: neighbors.length })}
              </p>
              <ul className="space-y-0.5">
                {neighbors.slice(0, 3).map((item) => (
                  <li key={item.id} className="truncate text-xs text-muted-foreground">
                    #{item.id} · {item.title}
                    <span className="ml-1 text-[10px] tabular-nums">
                      {Math.round(item.similarity * 100)}%
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          {existingRequest ? (
            <p className="mt-2 text-xs font-medium text-amber-700 dark:text-amber-500">
              {t('feedback.detail.support_promote_existing', {
                cr: `CR-${existingRequest.cr_no}`,
                title: existingRequest.title,
              })}
            </p>
          ) : null}
        </div>
        <div className="flex shrink-0 flex-col items-stretch gap-2">
          {existingRequest ? (
            <Button
              size="sm"
              variant="secondary"
              disabled={linkExisting.isPending}
              onClick={() =>
                linkExisting.mutate(
                  {
                    feedbackId,
                    importance: CustomerRequestImportance.CUSTOMER_REQUEST_IMPORTANCE_NORMAL,
                  },
                  {
                    onSuccess: () =>
                      toast.success(
                        t('feedback.detail.support_promote_linked', {
                          cr: `CR-${existingRequest.cr_no}`,
                        }),
                      ),
                    onError: (err) =>
                      toast.error(err instanceof Error ? err.message : t('common.error')),
                  },
                )
              }
            >
              {linkExisting.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              {t('feedback.detail.support_promote_link_action', {
                cr: `CR-${existingRequest.cr_no}`,
              })}
            </Button>
          ) : null}
          <Button asChild size="sm" variant={existingRequest ? 'outline' : 'default'}>
            <Link
              to="/feedback/customer-requests"
              search={{
                request_id: undefined,
                merge_target_id: undefined,
                promote_feedback_ids: promoteIDs,
                feedback_id: feedbackId,
              }}
            >
              {promoteIDs.includes(',')
                ? t('feedback.detail.support_promote_action_bundle', {
                    count: promoteIDs.split(',').length,
                  })
                : t('feedback.detail.support_promote_action')}
            </Link>
          </Button>
        </div>
      </div>
    </div>
  )
}

function PortalSubmissionSection({
  feedbackId,
  canPromoteCustomerRequest,
  submission,
}: {
  feedbackId: string
  canPromoteCustomerRequest: boolean
  submission: PortalSubmissionEvidence
}) {
  const { t } = useTranslation()
  const privateContactEntries = portalSubmissionEntries(submission.privateContact)
  const customFieldEntries = portalSubmissionEntries(submission.customFields)

  return (
    <Section label={t('feedback.detail.portal_submission')}>
      <div className="space-y-4">
        <dl className="grid gap-3 sm:grid-cols-2">
          <FactRow
            label={t('feedback.detail.portal_submission_kind')}
            value={portalSubmissionKindLabel(submission.kind, t)}
          />
          <FactRow label={t('feedback.detail.portal_submission_title')} value={submission.title} />
          {submission.pageUrl ? (
            <FactRow
              label={t('feedback.detail.portal_submission_page_url')}
              valueNode={
                <a
                  href={submission.pageUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 break-all text-primary underline-offset-2 hover:underline"
                >
                  <span>{submission.pageUrl}</span>
                  <ExternalLink className="size-3.5 shrink-0" />
                </a>
              }
              className="break-all"
            />
          ) : null}
          {submission.userAgent ? (
            <FactRow
              label={t('feedback.detail.portal_submission_user_agent')}
              value={submission.userAgent}
              mono
              className="break-all"
            />
          ) : null}
        </dl>

        {canPromoteCustomerRequest && isPositiveIntString(feedbackId) ? (
          <div className="rounded-lg border border-primary/15 bg-primary/5 px-4 py-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0">
                <div className="text-xs font-semibold uppercase tracking-[0.12em] text-primary">
                  {t('feedback.detail.portal_submission_promote_title')}
                </div>
                <p className="mt-1 text-sm leading-6 text-muted-foreground">
                  {t('feedback.detail.portal_submission_promote_body')}
                </p>
              </div>
              <Button asChild size="sm" className="shrink-0">
                <Link
                  to="/feedback/customer-requests"
                  search={{
                    request_id: undefined,
                    merge_target_id: undefined,
                    promote_feedback_ids: feedbackId,
                    feedback_id: feedbackId,
                  }}
                >
                  {t('feedback.detail.portal_submission_promote_action')}
                </Link>
              </Button>
            </div>
          </div>
        ) : null}

        <div className="rounded-lg border border-border/50 bg-muted/10 px-3 py-3">
          <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            {t('feedback.detail.portal_submission_details')}
          </div>
          <p className="mt-2 whitespace-pre-wrap break-words leading-6 text-foreground">
            {submission.details}
          </p>
        </div>

        {privateContactEntries.length > 0 ? (
          <div className="space-y-2">
            <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
              {t('feedback.detail.portal_submission_private_contact')}
            </div>
            <dl className="space-y-3">
              {privateContactEntries.map(([key, value]) => (
                <FactRow
                  key={key}
                  label={portalSubmissionContactFieldLabel(key, t)}
                  valueNode={portalSubmissionValueNode(value)}
                  className="break-all"
                />
              ))}
            </dl>
          </div>
        ) : null}

        {customFieldEntries.length > 0 ? (
          <div className="space-y-2">
            <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
              {t('feedback.detail.portal_submission_custom_fields')}
            </div>
            <dl className="space-y-3">
              {customFieldEntries.map(([key, value]) => (
                <FactRow
                  key={key}
                  label={key}
                  valueNode={portalSubmissionValueNode(value)}
                  className="break-all"
                />
              ))}
            </dl>
          </div>
        ) : null}
      </div>
    </Section>
  )
}

function portalSubmissionEntries(values?: Record<string, unknown>) {
  return values ? Object.entries(values).sort(([left], [right]) => left.localeCompare(right)) : []
}

function portalSubmissionKindLabel(kind: string, t: TFunction) {
  if (kind === 'request') return t('feedback.type.request')
  if (kind === 'bug') return t('feedback.type.bug')
  if (kind === 'general') return t('feedback.type.general')
  return kind || '—'
}

function portalSubmissionContactFieldLabel(key: string, t: TFunction) {
  if (key === 'display_name') return t('feedback.detail.portal_submission_display_name')
  if (key === 'organization') return t('feedback.detail.portal_submission_organization')
  return key
}

function portalSubmissionValueNode(value: unknown) {
  if (value == null) return '—'
  if (typeof value === 'string') return value.trim() || '—'
  if (typeof value === 'number' || typeof value === 'boolean' || typeof value === 'bigint') {
    return String(value)
  }
  if (Array.isArray(value) || isPortalRecord(value)) {
    try {
      return (
        <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded-md border border-border/50 bg-background px-3 py-2 font-mono text-xs text-foreground">
          {JSON.stringify(value, null, 2)}
        </pre>
      )
    } catch {
      return '—'
    }
  }
  return String(value)
}

function portalSubmissionText(value: unknown, trim = false) {
  if (typeof value !== 'string') return ''
  return trim ? value.trim() : value
}

function isPortalRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export const feedbackDetailSheetTestables = {
  customerRequestPriorityLabel,
  customerRequestStatusLabel,
  detailSummaryState,
  detailWorkbenchCue,
  existingRequestFor,
  isCompleteReplyDraftWorkflow,
  isPortalRecord,
  isPositiveIntString,
  isReplyDraftHardBlocker,
  latestRevisionByOrigin,
  promoteIDList,
  portalSubmissionContactFieldLabel,
  portalSubmissionEntries,
  portalSubmissionKindLabel,
  portalSubmissionMeta,
  portalSubmissionText,
  portalSubmissionValueNode,
  relativeTime,
  replyDraftTimelineItems,
  revisionByID,
  supportChannelCandidate,
  terminalFailureReasonClassLabel,
  terminalFailureSnapshotPresent,
  workbenchModeLabel,
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
          <h3 className="text-sm font-semibold text-foreground">
            {terminal
              ? t('feedback.detail.terminal_failure_title')
              : t('feedback.detail.enrichment_failed_title')}
          </h3>
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
                  <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
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
