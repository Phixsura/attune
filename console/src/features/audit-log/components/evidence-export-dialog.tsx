import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import {
  AlertCircle,
  CheckCircle2,
  Download,
  Loader2,
  RefreshCw,
  Shield,
  XCircle,
} from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  downloadEvidenceExport,
  type EvidenceExportFilter,
  isActiveEvidenceStatus,
  useEvidenceExportStatus,
  useStartEvidenceExport,
} from '@/features/audit-log/api/evidence-export'
import type { AuditLogFilters } from '@/features/audit-log/api/list-audit-log'

interface EvidenceExportDialogProps {
  filters: AuditLogFilters
  onOpenChange: (open: boolean) => void
  open: boolean
}

export function EvidenceExportDialog({ filters, onOpenChange, open }: EvidenceExportDialogProps) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language.startsWith('zh') ? zhCN : undefined
  const startMutation = useStartEvidenceExport()
  const [jobId, setJobId] = useState<null | string>(null)
  const [isDownloading, setIsDownloading] = useState(false)
  const statusQuery = useEvidenceExportStatus(jobId)

  const status = statusQuery.data?.status
  const isActive = status ? isActiveEvidenceStatus(status) : false
  const busy = startMutation.isPending || isActive || isDownloading

  useEffect(() => {
    if (!open) {
      setJobId(null)
    }
  }, [open])

  const buildBody = useCallback((): EvidenceExportFilter => {
    return {
      actions: filters.actions ?? [],
      actorId: filters.actorId ?? '',
      actorType: '',
      from: filters.from,
      targetId: filters.targetId ?? '',
      targetType: filters.targetType ?? '',
      to: filters.to,
    }
  }, [filters])

  const handleStart = () => {
    startMutation.mutate(buildBody(), {
      onSuccess: (resp) => setJobId(resp.jobId),
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : t('audit_log.evidence_start_failed')),
    })
  }

  const handleRetry = () => {
    setJobId(null)
    startMutation.mutate(buildBody(), {
      onSuccess: (resp) => setJobId(resp.jobId),
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : t('audit_log.evidence_start_failed')),
    })
  }

  const handleDownload = useCallback(async () => {
    /* v8 ignore next -- @preserve: the download button is only rendered after a job id exists. */
    if (!jobId) return
    setIsDownloading(true)
    try {
      await downloadEvidenceExport(jobId, statusQuery.data?.archiveFilename ?? undefined)
      toast.success(t('audit_log.evidence_downloaded'))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('audit_log.evidence_download_failed'))
    } finally {
      setIsDownloading(false)
    }
  }, [jobId, statusQuery.data?.archiveFilename, t])

  return (
    <Dialog open={open} onOpenChange={busy ? undefined : onOpenChange}>
      <DialogContent showCloseButton={!busy}>
        {!jobId ? (
          <ReadyView
            isPending={startMutation.isPending}
            onStart={handleStart}
            onClose={
              /* v8 ignore next -- @preserve: ReadyView close wiring is a Radix dialog chrome callback. */
              () => onOpenChange(false)
            }
          />
        ) : isActive ? (
          <ProcessingView status={status} />
        ) : status === 'completed' && statusQuery.data ? (
          <CompletedView
            data={statusQuery.data}
            locale={locale}
            isDownloading={isDownloading}
            onDownload={() => void handleDownload()}
            onClose={() => onOpenChange(false)}
          />
        ) : status === 'failed' && statusQuery.data ? (
          <FailedView
            error={statusQuery.data.error}
            isPending={startMutation.isPending}
            onRetry={handleRetry}
            onClose={() => onOpenChange(false)}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function ReadyView({
  isPending,
  onStart,
  onClose,
}: {
  isPending: boolean
  onClose: () => void
  onStart: () => void
}) {
  const { t } = useTranslation()

  return (
    <>
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2">
          <Shield className="h-5 w-5 text-primary" />
          {t('audit_log.evidence_title')}
        </DialogTitle>
        <DialogDescription>{t('audit_log.evidence_description')}</DialogDescription>
      </DialogHeader>
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="outline" disabled={isPending} onClick={onClose}>
            {t('common.cancel')}
          </Button>
        </DialogClose>
        <Button disabled={isPending} onClick={onStart}>
          {isPending ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('audit_log.evidence_starting')}
            </>
          ) : (
            t('audit_log.evidence_start')
          )}
        </Button>
      </DialogFooter>
    </>
  )
}

function ProcessingView({ status }: { status: string | undefined }) {
  const { t } = useTranslation()

  return (
    <div className="flex flex-col items-center gap-4 py-8">
      <div className="flex h-16 w-16 items-center justify-center rounded-full bg-primary/10">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
      </div>
      <div className="text-center">
        <p className="text-sm font-medium">
          {status === 'queued'
            ? t('audit_log.evidence_status_queued')
            : t('audit_log.evidence_status_running')}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">
          {t('audit_log.evidence_processing_hint')}
        </p>
      </div>
    </div>
  )
}

function CompletedView({
  data,
  locale,
  isDownloading,
  onDownload,
  onClose,
}: {
  data: {
    completedAt?: string
    createdAt: string
    expiresAt?: string
    totalEvents: number
  }
  isDownloading: boolean
  locale?: typeof zhCN
  onClose: () => void
  onDownload: () => void
}) {
  const { t } = useTranslation()

  return (
    <>
      <div className="flex flex-col items-center gap-3 pt-2">
        <div className="flex h-14 w-14 items-center justify-center rounded-full bg-emerald-50 dark:bg-emerald-950/30">
          <CheckCircle2 className="h-7 w-7 text-emerald-600" />
        </div>
        <div className="text-center">
          <p className="text-base font-semibold">{t('audit_log.evidence_status_completed')}</p>
          <p className="mt-1 text-sm text-muted-foreground">
            {t('audit_log.evidence_total_events', { count: data.totalEvents })}
            {data.expiresAt
              ? ` · ${t('audit_log.evidence_expires_at', {
                  time: formatDistanceToNow(new Date(data.expiresAt), { addSuffix: true, locale }),
                })}`
              : ''}
          </p>
        </div>
      </div>

      <div className="flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200">
        <Shield className="mt-0.5 h-3.5 w-3.5 shrink-0" />
        <span>{t('audit_log.evidence_verify_hint')}</span>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t('audit_log.evidence_close')}
        </Button>
        <Button disabled={isDownloading} onClick={onDownload}>
          {isDownloading ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('audit_log.evidence_downloading')}
            </>
          ) : (
            <>
              <Download className="mr-2 h-4 w-4" />
              {t('audit_log.evidence_download')}
            </>
          )}
        </Button>
      </DialogFooter>
    </>
  )
}

function FailedView({
  error,
  isPending,
  onRetry,
  onClose,
}: {
  error?: string
  isPending: boolean
  onClose: () => void
  onRetry: () => void
}) {
  const { t } = useTranslation()

  return (
    <>
      <div className="flex flex-col items-center gap-3 pt-2">
        <div className="flex h-14 w-14 items-center justify-center rounded-full bg-destructive/10">
          <XCircle className="h-7 w-7 text-destructive" />
        </div>
        <div className="text-center">
          <p className="text-base font-semibold">{t('audit_log.evidence_status_failed')}</p>
          {error ? <p className="mt-1 text-sm text-muted-foreground">{error}</p> : null}
        </div>
      </div>

      <div className="flex items-start gap-2 rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">
        <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
        <span>{t('audit_log.evidence_failed_hint')}</span>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t('audit_log.evidence_close')}
        </Button>
        <Button disabled={isPending} onClick={onRetry}>
          {isPending ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('common.processing')}
            </>
          ) : (
            <>
              <RefreshCw className="mr-2 h-4 w-4" />
              {t('audit_log.evidence_retry')}
            </>
          )}
        </Button>
      </DialogFooter>
    </>
  )
}
