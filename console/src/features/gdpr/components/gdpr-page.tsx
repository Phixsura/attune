import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  Download,
  Loader2,
  Lock,
  RefreshCw,
  Scale,
  ShieldCheck,
  ShieldX,
  Trash2,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
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
import { useDeleteGdprSubject } from '@/features/gdpr/api/delete-gdpr-subject'
import {
  downloadGdprExport,
  isActiveStatus,
  useGdprExportStatus,
  useRevokeGdprExport,
  useStartGdprExport,
} from '@/features/gdpr/api/export-gdpr-subject'
import {
  gdprRequestsInfiniteQuery,
  useCancelGdprRequest,
  useGdprOperations,
  useVerifyGdprStepUp,
} from '@/features/gdpr/api/gdpr-control'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import {
  GdprExportStatus,
  type GdprExportStatusResponse,
  GdprRequestStatus,
  GdprRequestType,
} from '@/proto/attune/v1/gdpr'

type RequestFilter = '' | 'delete' | 'export'

export function GDPRPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { can } = usePermissions()
  const canView = can('settings:gdpr:view')
  const canExport = can('settings:gdpr:export')
  const canDelete = can('settings:gdpr:delete')
  const deleteMutation = useDeleteGdprSubject()
  const cancelRequestMutation = useCancelGdprRequest()
  const startExportMutation = useStartGdprExport()
  const revokeExportMutation = useRevokeGdprExport()
  const verifyStepUpMutation = useVerifyGdprStepUp()
  const operationsQuery = useGdprOperations()

  const [subjectKey, setSubjectKey] = useState('')
  const [confirmSubjectKey, setConfirmSubjectKey] = useState('')
  const [exportJobId, setExportJobId] = useState<null | string>(null)
  const [downloadedJobId, setDownloadedJobId] = useState<null | string>(null)
  const [notifiedTerminalState, setNotifiedTerminalState] = useState<null | string>(null)
  const [isDownloading, setIsDownloading] = useState(false)
  const [revokingJobId, setRevokingJobId] = useState<null | string>(null)
  const [cancellingRequestId, setCancellingRequestId] = useState<null | string>(null)
  const [requestFilter, setRequestFilter] = useState<RequestFilter>('')
  const [stepUpOpen, setStepUpOpen] = useState(false)
  const [stepUpPassword, setStepUpPassword] = useState('')
  const exportStatusQuery = useGdprExportStatus(exportJobId)
  const requestsQuery = useInfiniteQuery(gdprRequestsInfiniteQuery(requestFilter))

  const normalizedSubjectKey = subjectKey.trim()
  const deleteConfirmed =
    normalizedSubjectKey !== '' && normalizedSubjectKey === confirmSubjectKey.trim()
  const exportStatus = exportStatusQuery.data?.status
  const exportBusy =
    startExportMutation.isPending ||
    isDownloading ||
    (exportStatus ? isActiveStatus(exportStatus) : false)

  const stepUp = operationsQuery.data?.stepUp
  const stepUpSatisfied = stepUp?.satisfied ?? false
  const requests = useMemo(
    () => requestsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [requestsQuery.data],
  )

  const exportSummary = useMemo(() => {
    const data = exportStatusQuery.data
    if (!data) return null
    return {
      statusLabel: exportStatusLabel(data.status, t),
      counts: `${data.feedbackCount}/${data.tagAssignmentCount}/${data.feedbackAuditCount}/${data.llmAuditCount}`,
      error: data.error,
    }
  }, [exportStatusQuery.data, t])

  const refreshCenter = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['console', 'gdpr', 'operations'] }),
      queryClient.invalidateQueries({ queryKey: ['console', 'gdpr', 'requests'] }),
    ])
  }, [queryClient])

  const ensureStepUp = () => {
    if (stepUpSatisfied) return true
    setStepUpOpen(true)
    toast.error(t('gdpr.step_up_required'))
    return false
  }

  const handleExport = async () => {
    if (!normalizedSubjectKey) {
      toast.error(t('gdpr.subject_required'))
      return
    }
    if (!ensureStepUp()) return
    startExportMutation.mutate(normalizedSubjectKey, {
      onSuccess: async (resp) => {
        setExportJobId(resp.jobId)
        setDownloadedJobId(null)
        setNotifiedTerminalState(null)
        toast.success(t('gdpr.export_queued'))
        await refreshCenter()
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : t('gdpr.export_failed')),
    })
  }

  const handleDelete = () => {
    if (!normalizedSubjectKey) {
      toast.error(t('gdpr.subject_required'))
      return
    }
    if (!deleteConfirmed) {
      toast.error(t('gdpr.confirm_mismatch'))
      return
    }
    if (!ensureStepUp()) return
    deleteMutation.mutate(normalizedSubjectKey, {
      onSuccess: async (resp) => {
        setConfirmSubjectKey('')
        toast.success(
          t('gdpr.delete_scheduled', {
            feedback: resp.feedbackCount,
            llmAudit: resp.llmAuditCount,
            executeAfter: formatTimestamp(resp.executeAfter),
          }),
        )
        await refreshCenter()
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : t('gdpr.delete_failed')),
    })
  }

  const handleDownload = useCallback(
    async (data?: GdprExportStatusResponse | null) => {
      if (!exportJobId) return
      const exportData = data ?? exportStatusQuery.data
      if (!exportData) return
      setIsDownloading(true)
      try {
        await downloadGdprExport(exportJobId, exportData.archiveFilename)
        setDownloadedJobId(exportJobId)
        toast.success(t('gdpr.export_downloaded'))
        await refreshCenter()
      } catch (err) {
        toast.error(err instanceof Error ? err.message : t('gdpr.export_failed'))
      } finally {
        setIsDownloading(false)
      }
    },
    [exportJobId, exportStatusQuery.data, refreshCenter, t],
  )

  const handleVerifyStepUp = () => {
    verifyStepUpMutation.mutate(stepUpPassword, {
      onSuccess: async () => {
        setStepUpPassword('')
        setStepUpOpen(false)
        toast.success(t('gdpr.step_up_success'))
        await refreshCenter()
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : t('gdpr.step_up_failed')),
    })
  }

  const handleCancelRequest = (requestId: string) => {
    if (!ensureStepUp()) return
    setCancellingRequestId(requestId)
    cancelRequestMutation.mutate(requestId, {
      onSuccess: async () => {
        toast.success(t('gdpr.cancel_delete_success'))
        await refreshCenter()
      },
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : t('gdpr.cancel_delete_failed')),
      onSettled: () => setCancellingRequestId(null),
    })
  }

  const handleRevokeExport = (jobId: string) => {
    if (!ensureStepUp()) return
    setRevokingJobId(jobId)
    revokeExportMutation.mutate(jobId, {
      onSuccess: async () => {
        toast.success(t('gdpr.revoke_export_success'))
        if (exportJobId === jobId) {
          await queryClient.invalidateQueries({ queryKey: ['console', 'gdpr-export', jobId] })
        }
        await refreshCenter()
      },
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : t('gdpr.revoke_export_failed')),
      onSettled: () => setRevokingJobId(null),
    })
  }

  useEffect(() => {
    const data = exportStatusQuery.data
    if (!data || !exportJobId) return
    if (
      data.status === GdprExportStatus.GDPR_EXPORT_STATUS_COMPLETED &&
      data.downloadPath &&
      downloadedJobId !== exportJobId &&
      !isDownloading
    ) {
      void handleDownload(data)
      return
    }
    const terminalKey = `${exportJobId}:${data.status}:${data.error ?? ''}`
    if (notifiedTerminalState === terminalKey) return
    if (data.status === GdprExportStatus.GDPR_EXPORT_STATUS_FAILED) {
      setNotifiedTerminalState(terminalKey)
      toast.error(data.error || t('gdpr.export_failed'))
      return
    }
    if (data.status === GdprExportStatus.GDPR_EXPORT_STATUS_EXPIRED) {
      setNotifiedTerminalState(terminalKey)
      toast.error(t('gdpr.export_expired'))
      void refreshCenter()
    }
  }, [
    downloadedJobId,
    exportJobId,
    exportStatusQuery.data,
    handleDownload,
    isDownloading,
    notifiedTerminalState,
    refreshCenter,
    t,
  ])

  if (!canView) {
    return (
      <EmptyState icon={Scale} title={t('gdpr.empty_title')} description={t('gdpr.empty_body')} />
    )
  }

  return (
    <>
      <section className="space-y-6">
        <PageHero
          eyebrow={t('shell.groups.administration')}
          title={t('gdpr.title')}
          subtitle={t('gdpr.subtitle')}
          actions={
            <Button
              variant={stepUpSatisfied ? 'outline' : 'default'}
              onClick={() => setStepUpOpen(true)}
            >
              <ShieldCheck className="mr-2 h-4 w-4" />
              {stepUpSatisfied ? t('gdpr.step_up_verified') : t('gdpr.step_up_button')}
            </Button>
          }
          metrics={
            <>
              <PageHeroMetric
                label={t('gdpr.ops_queued')}
                value={String(operationsQuery.data?.queuedRequestCount ?? 0)}
                hint={t('gdpr.ops_help')}
              />
              <PageHeroMetric
                label={t('gdpr.ops_ready_exports')}
                value={String(operationsQuery.data?.readyExportCount ?? 0)}
                hint={t('gdpr.export_help')}
              />
              <PageHeroMetric
                label={t('gdpr.ops_scheduled_deletes')}
                value={String(operationsQuery.data?.scheduledDeleteCount ?? 0)}
                hint={t('gdpr.delete_help')}
                tone={(operationsQuery.data?.scheduledDeleteCount ?? 0) > 0 ? 'urgent' : 'default'}
              />
              <PageHeroMetric
                label={t('gdpr.ops_step_up_window')}
                value={stepUpSatisfied ? t('gdpr.step_up_verified') : t('gdpr.step_up_pending')}
                hint={
                  stepUp?.expiresAt
                    ? t('gdpr.step_up_expires_at', { value: formatTimestamp(stepUp.expiresAt) })
                    : t('gdpr.step_up_needed')
                }
                tone={stepUpSatisfied ? 'default' : 'active'}
              />
            </>
          }
        />

        <div className="grid gap-6 xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
          <div className="space-y-6">
            <Card className="border-border/60 shadow-none">
              <CardHeader>
                <CardTitle className="text-base">{t('gdpr.subject_title')}</CardTitle>
                <CardDescription>{t('gdpr.subject_help')}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-6">
                <div className="space-y-2">
                  <Label htmlFor="gdpr-subject-key">{t('gdpr.subject_label')}</Label>
                  <Input
                    id="gdpr-subject-key"
                    data-testid="gdpr-subject-key"
                    value={subjectKey}
                    onChange={(e) => setSubjectKey(e.target.value)}
                    placeholder={t('gdpr.subject_placeholder')}
                  />
                </div>
                <p className="text-xs leading-5 text-muted-foreground">{t('gdpr.subject_hint')}</p>
              </CardContent>
            </Card>

            <div className="grid gap-6 xl:grid-cols-2">
              <Card className="border-border/60 shadow-none">
                <CardHeader>
                  <CardTitle className="text-base">{t('gdpr.export_title')}</CardTitle>
                  <CardDescription>{t('gdpr.export_help')}</CardDescription>
                </CardHeader>
                <CardContent className="space-y-4 pt-6">
                  <p className="text-sm leading-6 text-muted-foreground">{t('gdpr.export_body')}</p>
                  <Button onClick={() => void handleExport()} disabled={!canExport || exportBusy}>
                    {exportBusy ? (
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    ) : (
                      <Download className="mr-2 h-4 w-4" />
                    )}
                    {exportBusy ? t('gdpr.exporting') : t('gdpr.export_button')}
                  </Button>
                  {exportStatusQuery.data ? (
                    <div className="rounded-md border bg-muted/20 p-3 text-sm">
                      <div className="font-medium">
                        {t('gdpr.export_status_label')}: {exportSummary?.statusLabel}
                      </div>
                      <div className="mt-1 text-muted-foreground">
                        {t('gdpr.export_job_id')}: {exportStatusQuery.data.jobId}
                      </div>
                      <div className="mt-1 text-muted-foreground">
                        {t('gdpr.export_counts')}: {exportSummary?.counts}
                      </div>
                      {exportStatusQuery.data.archiveFilename ? (
                        <div className="mt-1 text-muted-foreground">
                          {t('gdpr.export_filename')}: {exportStatusQuery.data.archiveFilename}
                        </div>
                      ) : null}
                      {exportStatusQuery.data.error ? (
                        <div className="mt-2 text-destructive">{exportStatusQuery.data.error}</div>
                      ) : null}
                      {exportStatusQuery.data.status ===
                      GdprExportStatus.GDPR_EXPORT_STATUS_COMPLETED ? (
                        <div className="mt-3 flex flex-wrap gap-2">
                          <Button
                            variant="outline"
                            onClick={() => void handleDownload()}
                            disabled={isDownloading}
                          >
                            {isDownloading ? (
                              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                            ) : (
                              <Download className="mr-2 h-4 w-4" />
                            )}
                            {t('gdpr.export_download_button')}
                          </Button>
                          <Button
                            variant="outline"
                            onClick={() => handleRevokeExport(exportStatusQuery.data.jobId)}
                            disabled={revokingJobId === exportStatusQuery.data.jobId}
                          >
                            {revokingJobId === exportStatusQuery.data.jobId ? (
                              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                            ) : (
                              <ShieldX className="mr-2 h-4 w-4" />
                            )}
                            {t('gdpr.revoke_export_button')}
                          </Button>
                        </div>
                      ) : null}
                    </div>
                  ) : null}
                </CardContent>
              </Card>

              <Card className="border-destructive/25 shadow-none">
                <CardHeader className="border-b border-destructive/15 bg-destructive/[0.03]">
                  <CardTitle className="text-base">{t('gdpr.delete_title')}</CardTitle>
                  <CardDescription>{t('gdpr.delete_help')}</CardDescription>
                </CardHeader>
                <CardContent className="space-y-4 pt-6">
                  <p className="text-sm leading-6 text-muted-foreground">{t('gdpr.delete_body')}</p>
                  <div className="space-y-2">
                    <Label htmlFor="gdpr-confirm-subject-key">{t('gdpr.confirm_label')}</Label>
                    <Input
                      id="gdpr-confirm-subject-key"
                      data-testid="gdpr-confirm-subject-key"
                      value={confirmSubjectKey}
                      onChange={(e) => setConfirmSubjectKey(e.target.value)}
                      placeholder={normalizedSubjectKey || t('gdpr.confirm_placeholder')}
                    />
                  </div>
                  <Button
                    data-testid="gdpr-delete-submit"
                    variant="destructive"
                    onClick={handleDelete}
                    disabled={!canDelete || deleteMutation.isPending || !deleteConfirmed}
                  >
                    {deleteMutation.isPending ? (
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    ) : (
                      <Trash2 className="mr-2 h-4 w-4" />
                    )}
                    {deleteMutation.isPending ? t('common.deleting') : t('gdpr.delete_button')}
                  </Button>
                </CardContent>
              </Card>
            </div>

            <Card className="border-border/60 shadow-none">
              <CardHeader>
                <CardTitle className="text-base">{t('gdpr.request_center_title')}</CardTitle>
                <CardDescription>{t('gdpr.request_center_help')}</CardDescription>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('gdpr.request_col_type')}</TableHead>
                      <TableHead>{t('gdpr.request_col_subject')}</TableHead>
                      <TableHead>{t('gdpr.request_col_status')}</TableHead>
                      <TableHead>{t('gdpr.request_col_counts')}</TableHead>
                      <TableHead>{t('gdpr.request_col_timing')}</TableHead>
                      <TableHead className="text-right">{t('gdpr.request_col_action')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {requests.map((item) => (
                      <TableRow
                        key={item.requestId}
                        data-testid={`gdpr-request-row-${item.requestId}`}
                      >
                        <TableCell className="font-medium">
                          {requestTypeLabel(item.requestType, t)}
                        </TableCell>
                        <TableCell>
                          <div>{item.subjectDisplay || item.subjectKey}</div>
                          <div className="font-mono text-xs text-muted-foreground">
                            {item.subjectKey}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div>{requestStatusLabel(item.status, t)}</div>
                          {item.archiveFilename ? (
                            <div className="text-xs text-muted-foreground">
                              {item.archiveFilename}
                            </div>
                          ) : null}
                          {item.error ? (
                            <div className="text-xs text-destructive">{item.error}</div>
                          ) : null}
                        </TableCell>
                        <TableCell className="font-mono text-xs text-muted-foreground">
                          {item.feedbackCount}/{item.tagAssignmentCount}/{item.feedbackAuditCount}/
                          {item.llmAuditCount}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          <div>{formatTimestamp(item.createdAt)}</div>
                          {item.executeAfter ? (
                            <div>
                              {t('gdpr.request_execute_after', {
                                value: formatTimestamp(item.executeAfter),
                              })}
                            </div>
                          ) : null}
                          {item.cancelledAt ? (
                            <div>
                              {t('gdpr.request_cancelled_at', {
                                value: formatTimestamp(item.cancelledAt),
                              })}
                            </div>
                          ) : null}
                          {item.revokedAt ? (
                            <div>
                              {t('gdpr.request_revoked_at', {
                                value: formatTimestamp(item.revokedAt),
                              })}
                            </div>
                          ) : null}
                          {item.expiresAt ? (
                            <div>
                              {t('gdpr.request_expiry', { value: formatTimestamp(item.expiresAt) })}
                            </div>
                          ) : null}
                          {item.downloadedAt ? (
                            <div>
                              {t('gdpr.request_downloaded', {
                                value: formatTimestamp(item.downloadedAt),
                              })}
                            </div>
                          ) : null}
                        </TableCell>
                        <TableCell className="text-right">
                          {canCancelRequest(item.status, item.requestType) ? (
                            <Button
                              data-testid={`gdpr-cancel-request-${item.requestId}`}
                              variant="outline"
                              size="sm"
                              onClick={() => handleCancelRequest(item.requestId)}
                              disabled={
                                cancellingRequestId === item.requestId ||
                                cancelRequestMutation.isPending
                              }
                            >
                              {cancellingRequestId === item.requestId ? (
                                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                              ) : null}
                              {t('gdpr.cancel_delete_button')}
                            </Button>
                          ) : canRevokeRequest(item.status, item.requestType) ? (
                            <Button
                              data-testid={`gdpr-revoke-export-${item.requestId}`}
                              variant="outline"
                              size="sm"
                              onClick={() => handleRevokeExport(item.requestId)}
                              disabled={
                                revokingJobId === item.requestId || revokeExportMutation.isPending
                              }
                            >
                              {revokingJobId === item.requestId ? (
                                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                              ) : (
                                <ShieldX className="mr-2 h-4 w-4" />
                              )}
                              {t('gdpr.revoke_export_button')}
                            </Button>
                          ) : (
                            <span className="text-xs text-muted-foreground">
                              {t('gdpr.request_action_none')}
                            </span>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                    {!requests.length ? (
                      <TableRow>
                        <TableCell
                          colSpan={6}
                          className="py-8 text-center text-sm text-muted-foreground"
                        >
                          {t('gdpr.request_center_empty')}
                        </TableCell>
                      </TableRow>
                    ) : null}
                  </TableBody>
                </Table>
                {requestsQuery.hasNextPage ? (
                  <div className="mt-4 flex justify-center">
                    <Button
                      variant="outline"
                      onClick={() => void requestsQuery.fetchNextPage()}
                      disabled={requestsQuery.isFetchingNextPage}
                    >
                      {requestsQuery.isFetchingNextPage ? (
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      ) : null}
                      {t('gdpr.load_more')}
                    </Button>
                  </div>
                ) : null}
              </CardContent>
            </Card>
          </div>

          <div className="space-y-6">
            <Card className="border-border/60 shadow-none">
              <CardHeader>
                <CardTitle className="text-base">{t('gdpr.ops_title')}</CardTitle>
                <CardDescription>{t('gdpr.ops_help')}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-6">
                <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
                  <MetricCard
                    label={t('gdpr.ops_step_up_window')}
                    value={formatSeconds(operationsQuery.data?.stepUp?.ttlSeconds)}
                  />
                  <MetricCard
                    label={t('gdpr.ops_export_ttl')}
                    value={formatSeconds(operationsQuery.data?.exportTtlSeconds)}
                  />
                  <MetricCard
                    label={t('gdpr.ops_audit_retention')}
                    value={t('gdpr.ops_days', {
                      count: operationsQuery.data?.auditRetentionDays ?? 0,
                    })}
                  />
                  <MetricCard
                    label={t('gdpr.ops_prune_interval')}
                    value={formatSeconds(operationsQuery.data?.auditPruneIntervalSeconds)}
                  />
                  <MetricCard
                    label={t('gdpr.ops_delete_grace_window')}
                    value={formatSeconds(operationsQuery.data?.deleteGraceWindowSeconds)}
                  />
                </div>
                <div className="grid gap-3 sm:grid-cols-4">
                  <MetricCard
                    label={t('gdpr.ops_queued')}
                    value={String(operationsQuery.data?.queuedRequestCount ?? 0)}
                  />
                  <MetricCard
                    label={t('gdpr.ops_active')}
                    value={String(operationsQuery.data?.activeRequestCount ?? 0)}
                  />
                  <MetricCard
                    label={t('gdpr.ops_ready_exports')}
                    value={String(operationsQuery.data?.readyExportCount ?? 0)}
                  />
                  <MetricCard
                    label={t('gdpr.ops_scheduled_deletes')}
                    value={String(operationsQuery.data?.scheduledDeleteCount ?? 0)}
                  />
                </div>
                <div className="rounded-md border bg-muted/20 p-3 text-sm text-muted-foreground">
                  <div>{t('gdpr.ops_hashed_audit')}</div>
                  <div className="mt-1">{t('gdpr.ops_backups')}</div>
                  <div className="mt-1">{t('gdpr.ops_aggregate_residue')}</div>
                  <div className="mt-1">
                    {operationsQuery.data?.legalHoldSupported
                      ? t('gdpr.ops_legal_hold_supported')
                      : t('gdpr.ops_legal_hold_unsupported')}
                  </div>
                  <div className="mt-1">
                    {operationsQuery.data?.nextExportExpiryAt
                      ? t('gdpr.ops_next_expiry', {
                          value: formatTimestamp(operationsQuery.data.nextExportExpiryAt),
                        })
                      : t('gdpr.ops_next_expiry_empty')}
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card className="border-amber-500/20 shadow-none">
              <CardHeader className="border-b border-amber-500/15 bg-amber-50/40">
                <CardTitle className="text-base">{t('gdpr.guardrail_title')}</CardTitle>
                <CardDescription>{t('gdpr.guardrail_help')}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-6">
                <div className="rounded-md border px-3 py-2 text-sm">
                  <div className="flex items-center gap-2 font-medium">
                    <Lock className="h-4 w-4" />
                    {stepUpSatisfied ? t('gdpr.step_up_verified') : t('gdpr.step_up_pending')}
                  </div>
                  <div className="mt-1 text-muted-foreground">
                    {stepUp?.expiresAt
                      ? t('gdpr.step_up_expires_at', { value: formatTimestamp(stepUp.expiresAt) })
                      : t('gdpr.step_up_needed')}
                  </div>
                </div>
                <div className="space-y-2 rounded-md border border-destructive/20 bg-destructive/5 p-3 text-sm">
                  <div className="flex items-center gap-2 font-medium text-destructive">
                    <AlertTriangle className="h-4 w-4" />
                    {t('gdpr.guardrail_warning_title')}
                  </div>
                  <p className="leading-6 text-muted-foreground">
                    {t('gdpr.guardrail_warning_body')}
                  </p>
                  <p className="leading-6 text-muted-foreground">
                    {t('gdpr.guardrail_recovery_body')}
                  </p>
                </div>
              </CardContent>
            </Card>

            <Card className="border-border/60 shadow-none">
              <CardHeader>
                <div>
                  <CardTitle className="text-base">{t('gdpr.filter_all')}</CardTitle>
                  <CardDescription>{t('gdpr.request_center_help')}</CardDescription>
                </div>
              </CardHeader>
              <CardContent className="space-y-4 pt-6">
                <div className="flex flex-wrap gap-2">
                  {(
                    [
                      ['', t('gdpr.filter_all')],
                      ['export', t('gdpr.filter_export')],
                      ['delete', t('gdpr.filter_delete')],
                    ] as const
                  ).map(([value, label]) => (
                    <Button
                      key={value || 'all'}
                      variant={requestFilter === value ? 'default' : 'outline'}
                      size="sm"
                      onClick={() => setRequestFilter(value)}
                    >
                      {label}
                    </Button>
                  ))}
                  <Button variant="outline" size="sm" onClick={() => void refreshCenter()}>
                    <RefreshCw className="mr-2 h-4 w-4" />
                    {t('gdpr.refresh')}
                  </Button>
                </div>
                <div className="rounded-md border border-border/60 bg-background/85 px-4 py-3 text-sm text-muted-foreground">
                  {t('gdpr.guardrail_help')}
                </div>
              </CardContent>
            </Card>
          </div>
        </div>
      </section>

      <Dialog open={stepUpOpen} onOpenChange={setStepUpOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t('gdpr.step_up_dialog_title')}</DialogTitle>
            <DialogDescription>{t('gdpr.step_up_dialog_body')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-2">
              <Label htmlFor="gdpr-step-up-password">{t('gdpr.step_up_password_label')}</Label>
              <Input
                id="gdpr-step-up-password"
                type="password"
                value={stepUpPassword}
                onChange={(e) => setStepUpPassword(e.target.value)}
                placeholder={t('gdpr.step_up_password_placeholder')}
              />
            </div>
            <p className="text-xs leading-5 text-muted-foreground">
              {t('gdpr.step_up_dialog_hint')}
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
              {t('gdpr.step_up_confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function MetricCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border bg-muted/15 p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-base font-semibold">{value}</div>
    </div>
  )
}

function exportStatusLabel(status: GdprExportStatus, t: (key: string) => string) {
  switch (status) {
    case GdprExportStatus.GDPR_EXPORT_STATUS_QUEUED:
      return t('gdpr.status_queued')
    case GdprExportStatus.GDPR_EXPORT_STATUS_RUNNING:
      return t('gdpr.status_running')
    case GdprExportStatus.GDPR_EXPORT_STATUS_COMPLETED:
      return t('gdpr.status_completed')
    case GdprExportStatus.GDPR_EXPORT_STATUS_FAILED:
      return t('gdpr.status_failed')
    case GdprExportStatus.GDPR_EXPORT_STATUS_EXPIRED:
      return t('gdpr.status_expired')
    case GdprExportStatus.GDPR_EXPORT_STATUS_REVOKED:
      return t('gdpr.status_revoked')
    default:
      return t('gdpr.status_unknown')
  }
}

function requestStatusLabel(status: GdprRequestStatus, t: (key: string) => string) {
  switch (status) {
    case GdprRequestStatus.GDPR_REQUEST_STATUS_QUEUED:
      return t('gdpr.request_status_queued')
    case GdprRequestStatus.GDPR_REQUEST_STATUS_RUNNING:
      return t('gdpr.request_status_running')
    case GdprRequestStatus.GDPR_REQUEST_STATUS_READY:
      return t('gdpr.request_status_ready')
    case GdprRequestStatus.GDPR_REQUEST_STATUS_DOWNLOADED:
      return t('gdpr.request_status_downloaded')
    case GdprRequestStatus.GDPR_REQUEST_STATUS_COMPLETED:
      return t('gdpr.request_status_completed')
    case GdprRequestStatus.GDPR_REQUEST_STATUS_FAILED:
      return t('gdpr.request_status_failed')
    case GdprRequestStatus.GDPR_REQUEST_STATUS_EXPIRED:
      return t('gdpr.request_status_expired')
    case GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED:
      return t('gdpr.request_status_scheduled')
    case GdprRequestStatus.GDPR_REQUEST_STATUS_CANCELLED:
      return t('gdpr.request_status_cancelled')
    case GdprRequestStatus.GDPR_REQUEST_STATUS_REVOKED:
      return t('gdpr.request_status_revoked')
    default:
      return t('gdpr.status_unknown')
  }
}

function canCancelRequest(status: GdprRequestStatus, type: GdprRequestType) {
  return (
    type === GdprRequestType.GDPR_REQUEST_TYPE_DELETE &&
    status === GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED
  )
}

function canRevokeRequest(status: GdprRequestStatus, type: GdprRequestType) {
  return (
    type === GdprRequestType.GDPR_REQUEST_TYPE_EXPORT &&
    (status === GdprRequestStatus.GDPR_REQUEST_STATUS_READY ||
      status === GdprRequestStatus.GDPR_REQUEST_STATUS_DOWNLOADED)
  )
}

function requestTypeLabel(type: GdprRequestType, t: (key: string) => string) {
  switch (type) {
    case GdprRequestType.GDPR_REQUEST_TYPE_EXPORT:
      return t('gdpr.filter_export')
    case GdprRequestType.GDPR_REQUEST_TYPE_DELETE:
      return t('gdpr.filter_delete')
    default:
      return t('gdpr.filter_all')
  }
}

function formatTimestamp(value?: null | string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function formatSeconds(value?: number) {
  if (!value) return '—'
  if (value % 86400 === 0) return `${value / 86400}d`
  if (value % 3600 === 0) return `${value / 3600}h`
  if (value % 60 === 0) return `${value / 60}m`
  return `${value}s`
}
