import { FileSearch, GitBranch, Loader2, RefreshCcw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  type ExternalSyncConflict,
  ExternalSyncConflictResolution,
  type ExternalSyncEvent,
  type ExternalSyncRecordFailure,
  type ExternalSyncRecordTimelineEntry,
  type ExternalSyncRunDetail,
} from '@/features/external-sync/api/external-sync'
import {
  DetailSection,
  DiagnosticRows,
  eventSignatureLabel,
  eventStatusLabel,
  formatDate,
  JsonBlock,
  MutedLine,
  StatusPill,
  shortID,
} from './external-sync-ui'

export type RecordTimelineTarget = {
  mappingId: string
  localObjectId: string
  externalKey: string
  label: string
}

export function EventDetailCard({
  event,
  loading,
}: {
  event?: ExternalSyncEvent
  loading: boolean
}) {
  const { t } = useTranslation()
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('external_sync.event_detail.title')}</CardTitle>
        <CardDescription>
          {event
            ? t('external_sync.event_detail.description', { id: shortID(event.id) })
            : t('external_sync.event_detail.empty_body')}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 pt-6">
        {loading ? (
          <Loading />
        ) : !event ? (
          <EmptyState
            icon={GitBranch}
            title={t('external_sync.event_detail.empty_title')}
            description={t('external_sync.event_detail.empty_body')}
          />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <StatusPill value={eventStatusLabel(event.status)} />
              <StatusPill value={eventSignatureLabel(event.signatureStatus)} />
            </div>
            <DiagnosticRows
              rows={[
                { label: t('external_sync.event_detail.provider'), value: event.provider },
                { label: t('external_sync.event_detail.event_type'), value: event.eventType },
                {
                  label: t('external_sync.event_detail.external_event_id'),
                  value: event.externalEventId,
                },
                { label: t('external_sync.event_detail.dedupe_key'), value: event.dedupeKey },
                { label: t('external_sync.event_detail.run_id'), value: event.runId },
                {
                  label: t('external_sync.event_detail.payload_digest'),
                  value: event.payloadDigest,
                },
                {
                  label: t('external_sync.event_detail.received_at'),
                  value: formatDate(event.receivedAt),
                },
                {
                  label: t('external_sync.event_detail.replayed_at'),
                  value: formatDate(event.replayedAt),
                },
                { label: t('external_sync.event_detail.replayed_by'), value: event.replayedBy },
              ]}
            />
            {event.failureReason && (
              <div className="rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                {event.failureReason}
              </div>
            )}
            <JsonBlock
              label={t('external_sync.event_detail.normalized_payload')}
              value={event.normalizedPayloadJson}
            />
          </>
        )}
      </CardContent>
    </Card>
  )
}

export function RunDetailCard({
  detail,
  loading,
  retryingFailureID,
  resolvingConflictID,
  batchResolving,
  timelineEntries,
  timelineLoading,
  timelineTarget,
  onRetryFailure,
  onShowTimeline,
  onResolveConflict,
  onBatchResolveConflicts,
}: {
  detail?: ExternalSyncRunDetail
  loading: boolean
  retryingFailureID?: string
  resolvingConflictID?: string
  batchResolving: boolean
  timelineEntries: ExternalSyncRecordTimelineEntry[]
  timelineLoading: boolean
  timelineTarget: RecordTimelineTarget | null
  onRetryFailure: (id: string) => void
  onShowTimeline: (target: RecordTimelineTarget) => void
  onResolveConflict: (id: string, resolution: ExternalSyncConflictResolution) => void
  onBatchResolveConflicts: (ids: string[], resolution: ExternalSyncConflictResolution) => void
}) {
  const { t } = useTranslation()
  const run = detail?.run
  const openConflictIDs =
    detail?.conflicts
      .filter((conflict) => conflict.status === 'open')
      .map((conflict) => conflict.id) ?? []
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('external_sync.detail.title')}</CardTitle>
        <CardDescription>
          {run
            ? t('external_sync.detail.description', { id: shortID(run.id) })
            : t('external_sync.detail.empty_body')}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5 pt-6">
        {loading ? (
          <Loading />
        ) : !detail || !run ? (
          <EmptyState
            icon={RefreshCcw}
            title={t('external_sync.detail.empty_title')}
            description={t('external_sync.detail.empty_body')}
          />
        ) : (
          <>
            <div className="grid gap-2 text-xs sm:grid-cols-3">
              <JsonBlock
                label={t('external_sync.detail.cursor_before')}
                value={run.cursorBeforeJson}
              />
              <JsonBlock
                label={t('external_sync.detail.cursor_after')}
                value={run.cursorAfterJson}
              />
              <JsonBlock
                label={t('external_sync.detail.input_metadata')}
                value={run.inputMetadataJson}
              />
            </div>

            <DetailSection title={t('external_sync.detail.attempts')}>
              {detail.attempts.length === 0 ? (
                <MutedLine>{t('external_sync.detail.none')}</MutedLine>
              ) : (
                detail.attempts.map((attempt) => (
                  <div
                    key={attempt.id}
                    className="rounded-lg border border-border/60 bg-background px-3 py-2 text-xs"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium">{attempt.result}</span>
                      <span className="text-muted-foreground">
                        #{attempt.attemptNumber} · {formatDate(attempt.startedAt)}
                      </span>
                    </div>
                    {attempt.errorMessage && (
                      <div className="mt-1 text-destructive">{attempt.errorMessage}</div>
                    )}
                    <DiagnosticRows
                      rows={[
                        {
                          label: t('external_sync.detail.http_status'),
                          value: attempt.httpStatus > 0 ? String(attempt.httpStatus) : '',
                        },
                        {
                          label: t('external_sync.detail.provider_request_id'),
                          value: attempt.providerRequestId,
                        },
                        {
                          label: t('external_sync.detail.retry_after'),
                          value: formatDate(attempt.retryAfter),
                        },
                        {
                          label: t('external_sync.detail.error_kind'),
                          value: attempt.errorKind,
                        },
                      ]}
                    />
                  </div>
                ))
              )}
            </DetailSection>

            <DetailSection title={t('external_sync.detail.failures')}>
              {detail.failures.length === 0 ? (
                <MutedLine>{t('external_sync.detail.none')}</MutedLine>
              ) : (
                detail.failures.map((failure) => (
                  <div
                    key={failure.id}
                    className="rounded-lg border border-border/60 bg-background px-3 py-2 text-xs"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium">
                        {failure.externalKey || failure.operation}
                      </span>
                      <div className="flex items-center gap-1">
                        <Button
                          type="button"
                          size="xs"
                          variant="ghost"
                          onClick={() => onShowTimeline(recordTimelineTargetFromFailure(failure))}
                          disabled={
                            !canShowRecordTimeline(failure.localObjectId, failure.externalKey)
                          }
                        >
                          <FileSearch className="size-3" />
                          {t('external_sync.detail.timeline')}
                        </Button>
                        <Button
                          type="button"
                          size="xs"
                          variant="ghost"
                          onClick={() => onRetryFailure(failure.id)}
                          disabled={!failure.retryable || retryingFailureID === failure.id}
                        >
                          {retryingFailureID === failure.id && (
                            <Loader2 className="size-3 animate-spin" />
                          )}
                          {t('common.retry')}
                        </Button>
                      </div>
                    </div>
                    <div className="mt-1 text-muted-foreground">{failure.failureKind}</div>
                    {failure.message && (
                      <div className="mt-1 text-destructive">{failure.message}</div>
                    )}
                    <DiagnosticRows
                      rows={[
                        {
                          label: t('external_sync.detail.operation'),
                          value: failure.operation,
                        },
                        {
                          label: t('external_sync.detail.local_object_id'),
                          value: failure.localObjectId,
                        },
                        {
                          label: t('external_sync.detail.payload_digest'),
                          value: failure.payloadDigest,
                        },
                        {
                          label: t('external_sync.detail.retry_mode'),
                          value: failure.retryMode,
                        },
                        {
                          label: t('external_sync.detail.resolved_by'),
                          value: failure.resolvedBy,
                        },
                      ]}
                    />
                    {failure.normalizedPayloadJson && (
                      <div className="mt-2">
                        <JsonBlock
                          label={t('external_sync.detail.normalized_payload')}
                          value={failure.normalizedPayloadJson}
                        />
                      </div>
                    )}
                  </div>
                ))
              )}
            </DetailSection>

            <DetailSection title={t('external_sync.detail.conflicts')}>
              {detail.conflicts.length === 0 ? (
                <MutedLine>{t('external_sync.detail.none')}</MutedLine>
              ) : (
                <div className="space-y-2">
                  {openConflictIDs.length > 1 && (
                    <BatchConflictResolutionControls
                      conflictCount={openConflictIDs.length}
                      pending={batchResolving}
                      onResolve={(resolution) =>
                        onBatchResolveConflicts(openConflictIDs, resolution)
                      }
                    />
                  )}
                  {detail.conflicts.map((conflict) => (
                    <div
                      key={conflict.id}
                      className="rounded-lg border border-border/60 bg-background px-3 py-2 text-xs"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="font-medium">
                          {conflict.externalKey || conflict.conflictKind}
                        </span>
                        <div className="flex items-center gap-1">
                          <Button
                            type="button"
                            size="xs"
                            variant="ghost"
                            onClick={() =>
                              onShowTimeline(recordTimelineTargetFromConflict(conflict))
                            }
                            disabled={
                              !canShowRecordTimeline(conflict.localObjectId, conflict.externalKey)
                            }
                          >
                            <FileSearch className="size-3" />
                            {t('external_sync.detail.timeline')}
                          </Button>
                          <ConflictResolutionControls
                            conflictID={conflict.id}
                            status={conflict.status}
                            pending={resolvingConflictID === conflict.id}
                            onResolve={(resolution) => onResolveConflict(conflict.id, resolution)}
                          />
                        </div>
                      </div>
                      <div className="mt-1 text-muted-foreground">
                        {conflict.conflictKind} · {conflict.status}
                      </div>
                      <DiagnosticRows
                        rows={[
                          {
                            label: t('external_sync.detail.local_object_id'),
                            value: conflict.localObjectId,
                          },
                          {
                            label: t('external_sync.detail.resolution'),
                            value: conflict.resolution,
                          },
                          {
                            label: t('external_sync.detail.resolved_by'),
                            value: conflict.resolvedBy,
                          },
                        ]}
                      />
                      {(conflict.localSnapshotJson || conflict.externalSnapshotJson) && (
                        <div className="mt-2 grid gap-2 sm:grid-cols-2">
                          {conflict.localSnapshotJson && (
                            <JsonBlock
                              label={t('external_sync.detail.local_snapshot')}
                              value={conflict.localSnapshotJson}
                            />
                          )}
                          {conflict.externalSnapshotJson && (
                            <JsonBlock
                              label={t('external_sync.detail.external_snapshot')}
                              value={conflict.externalSnapshotJson}
                            />
                          )}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </DetailSection>

            {timelineTarget && (
              <RecordTimelinePanel
                target={timelineTarget}
                entries={timelineEntries}
                loading={timelineLoading}
              />
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

export function RecordTimelinePanel({
  target,
  entries,
  loading,
}: {
  target: RecordTimelineTarget
  entries: ExternalSyncRecordTimelineEntry[]
  loading: boolean
}) {
  const { t } = useTranslation()
  return (
    <DetailSection title={t('external_sync.detail.timeline_title', { record: target.label })}>
      {loading ? (
        <Loading />
      ) : entries.length === 0 ? (
        <MutedLine>{t('external_sync.detail.timeline_empty')}</MutedLine>
      ) : (
        <div className="space-y-2">
          {entries.map((entry) => (
            <div
              key={`${entry.kind}-${entry.occurredAt}-${entry.runId}-${entry.summary}`}
              className="rounded-lg border border-border/60 bg-muted/20 px-3 py-2 text-xs"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="font-medium">{entry.summary || entry.kind}</span>
                <span className="text-muted-foreground">{formatDate(entry.occurredAt)}</span>
              </div>
              <DiagnosticRows
                rows={[
                  { label: t('external_sync.detail.kind'), value: entry.kind },
                  { label: t('external_sync.detail.status'), value: entry.status },
                  { label: t('external_sync.detail.operation'), value: entry.operation },
                  { label: t('external_sync.detail.run_id'), value: shortID(entry.runId) },
                  {
                    label: t('external_sync.detail.local_object_id'),
                    value: entry.localObjectId,
                  },
                  { label: t('external_sync.detail.external_key'), value: entry.externalKey },
                ]}
              />
              {entry.detailJson && (
                <div className="mt-2">
                  <JsonBlock
                    label={t('external_sync.detail.timeline_detail')}
                    value={entry.detailJson}
                  />
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </DetailSection>
  )
}

export function recordTimelineTargetFromFailure(
  failure: ExternalSyncRecordFailure,
): RecordTimelineTarget {
  return {
    mappingId: failure.mappingId,
    localObjectId: failure.localObjectId,
    externalKey: failure.externalKey,
    label: failure.externalKey || failure.localObjectId || shortID(failure.id),
  }
}

export function recordTimelineTargetFromConflict(
  conflict: ExternalSyncConflict,
): RecordTimelineTarget {
  return {
    mappingId: conflict.mappingId,
    localObjectId: conflict.localObjectId,
    externalKey: conflict.externalKey,
    label: conflict.externalKey || conflict.localObjectId || shortID(conflict.id),
  }
}

export function canShowRecordTimeline(localObjectId: string, externalKey: string) {
  return localObjectId.length > 0 || externalKey.length > 0
}

const conflictResolutionOptions = [
  ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS,
  ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_LOCAL_WINS,
  ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_MANUAL_MERGE,
  ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_IGNORED,
]

export function BatchConflictResolutionControls({
  conflictCount,
  pending,
  onResolve,
}: {
  conflictCount: number
  pending: boolean
  onResolve: (resolution: ExternalSyncConflictResolution) => void
}) {
  const { t } = useTranslation()
  const [resolution, setResolution] = useState(
    ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS,
  )

  return (
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border/60 bg-muted/20 px-3 py-2 text-xs">
      <span className="font-medium">
        {t('external_sync.conflict_resolution.batch_label', { count: conflictCount })}
      </span>
      <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
        <Select
          value={resolution}
          onValueChange={(value) => setResolution(value as ExternalSyncConflictResolution)}
          disabled={pending}
        >
          <SelectTrigger
            size="sm"
            aria-label={t('external_sync.conflict_resolution.batch_label', {
              count: conflictCount,
            })}
            className="w-[8.75rem] max-w-full text-xs"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {conflictResolutionOptions.map((option) => (
              <SelectItem key={option} value={option}>
                {t(conflictResolutionLabelKey(option))}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          type="button"
          size="xs"
          variant="ghost"
          onClick={() => onResolve(resolution)}
          disabled={pending}
        >
          {pending && <Loader2 className="size-3 animate-spin" />}
          {t('external_sync.actions.resolve_conflicts')}
        </Button>
      </div>
    </div>
  )
}

export function ConflictResolutionControls({
  conflictID,
  status,
  pending,
  onResolve,
}: {
  conflictID: string
  status: string
  pending: boolean
  onResolve: (resolution: ExternalSyncConflictResolution) => void
}) {
  const { t } = useTranslation()
  const [resolution, setResolution] = useState(
    ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS,
  )
  const disabled = status !== 'open' || pending
  const labelID = `conflict-resolution-label-${conflictID}`

  return (
    <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
      <Label id={labelID} className="sr-only">
        {t('external_sync.conflict_resolution.label')}
      </Label>
      <Select
        value={resolution}
        onValueChange={(value) => setResolution(value as ExternalSyncConflictResolution)}
        disabled={disabled}
      >
        <SelectTrigger
          size="sm"
          aria-labelledby={labelID}
          className="w-[8.75rem] max-w-full text-xs"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {conflictResolutionOptions.map((option) => (
            <SelectItem key={option} value={option}>
              {t(conflictResolutionLabelKey(option))}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button
        type="button"
        size="xs"
        variant="ghost"
        onClick={() => onResolve(resolution)}
        disabled={disabled}
      >
        {pending && <Loader2 className="size-3 animate-spin" />}
        {t('external_sync.actions.resolve_conflict')}
      </Button>
    </div>
  )
}

function conflictResolutionLabelKey(resolution: ExternalSyncConflictResolution) {
  switch (resolution) {
    case ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_LOCAL_WINS:
      return 'external_sync.conflict_resolution.local_wins'
    case ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_MANUAL_MERGE:
      return 'external_sync.conflict_resolution.manual_merge'
    case ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_IGNORED:
      return 'external_sync.conflict_resolution.ignored'
    default:
      return 'external_sync.conflict_resolution.external_wins'
  }
}
