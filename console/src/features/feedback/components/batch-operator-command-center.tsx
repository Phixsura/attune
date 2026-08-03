import {
  BellRing,
  CheckCircle2,
  ClipboardList,
  ExternalLink,
  Loader2,
  RefreshCw,
  ShieldCheck,
  UserRound,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

export interface OperatorBatchFailure {
  feedbackId: string
  code: string
  message: string
}

export interface OperatorBatchResult {
  action: 'assign' | 'dismiss' | 'link' | 'notify' | 'recommend' | 'retry' | 'tag'
  total: number
  succeeded: number
  skipped: number
  failed: OperatorBatchFailure[]
}

interface BatchOperatorCommandCenterProps {
  open: boolean
  count: number
  selectedFeedbackIds: string[]
  dismissStateLabel?: string
  terminalFailureCount: number
  latestResult?: OperatorBatchResult | null
  isDismissing?: boolean
  onOpenChange: (open: boolean) => void
  onLinkRequest: () => void
  onAssign: () => void
  onDismiss: () => void
  onNotify: () => void
  onRetryTerminalFailures: () => void
  onFocusFailed: () => void
  onClearResult: () => void
}

export function BatchOperatorCommandCenter({
  open,
  count,
  selectedFeedbackIds,
  dismissStateLabel,
  terminalFailureCount,
  latestResult,
  isDismissing,
  onOpenChange,
  onLinkRequest,
  onAssign,
  onDismiss,
  onNotify,
  onRetryTerminalFailures,
  onFocusFailed,
  onClearResult,
}: BatchOperatorCommandCenterProps) {
  const { t } = useTranslation()
  const hasFailures = (latestResult?.failed.length ?? 0) > 0
  const canDismiss = Boolean(dismissStateLabel)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldCheck className="size-5 text-primary" />
            {t('feedback.batch.operator.title')}
          </DialogTitle>
          <DialogDescription>
            {t('feedback.batch.operator.description', { count })}
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 lg:grid-cols-[minmax(0,1.4fr)_minmax(18rem,0.8fr)]">
          <section className="space-y-3" aria-label={t('feedback.batch.operator.actions_label')}>
            <OperatorAction
              icon={<ClipboardList className="size-4" />}
              title={t('feedback.batch.operator.link_title')}
              body={t('feedback.batch.operator.link_body')}
              actionLabel={t('feedback.batch.operator.link_action')}
              onAction={onLinkRequest}
            />
            <OperatorAction
              icon={<UserRound className="size-4" />}
              title={t('feedback.batch.operator.assign_title')}
              body={t('feedback.batch.operator.assign_body')}
              actionLabel={t('feedback.batch.assign')}
              onAction={onAssign}
            />
            <OperatorAction
              icon={
                isDismissing ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <CheckCircle2 className="size-4" />
                )
              }
              title={t('feedback.batch.operator.dismiss_title')}
              body={
                canDismiss
                  ? t('feedback.batch.operator.dismiss_body', { state: dismissStateLabel })
                  : t('feedback.batch.operator.dismiss_blocked')
              }
              actionLabel={t('feedback.batch.operator.dismiss_action')}
              disabled={!canDismiss || isDismissing}
              onAction={onDismiss}
            />
            <OperatorAction
              icon={<BellRing className="size-4" />}
              title={t('feedback.batch.operator.notify_title')}
              body={t('feedback.batch.operator.notify_body')}
              actionLabel={t('feedback.batch.operator.notify_action')}
              onAction={onNotify}
            />
          </section>

          <aside className="space-y-3" aria-label={t('feedback.batch.operator.recovery_label')}>
            <div className="rounded-md border bg-muted/20 p-3">
              <div className="text-sm font-medium">{t('feedback.batch.operator.scope_title')}</div>
              <p className="mt-1 text-sm text-muted-foreground">
                {t('feedback.batch.operator.scope_body', { count })}
              </p>
              <div className="mt-2 flex flex-wrap gap-1.5">
                {selectedFeedbackIds.slice(0, 8).map((id) => (
                  <span
                    key={id}
                    className="rounded border bg-background px-1.5 py-0.5 font-mono text-xs"
                  >
                    {id}
                  </span>
                ))}
                {selectedFeedbackIds.length > 8 ? (
                  <span className="rounded border bg-background px-1.5 py-0.5 text-xs text-muted-foreground">
                    {t('feedback.batch.operator.more_ids', {
                      count: selectedFeedbackIds.length - 8,
                    })}
                  </span>
                ) : null}
              </div>
            </div>

            <BatchRecoveryPanel
              latestResult={latestResult}
              terminalFailureCount={terminalFailureCount}
              hasFailures={hasFailures}
              onFocusFailed={onFocusFailed}
              onClearResult={onClearResult}
              onRetryTerminalFailures={onRetryTerminalFailures}
            />
          </aside>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function OperatorAction({
  icon,
  title,
  body,
  actionLabel,
  disabled,
  onAction,
}: {
  icon: ReactNode
  title: string
  body: string
  actionLabel: string
  disabled?: boolean
  onAction: () => void
}) {
  return (
    <div
      className={cn(
        'grid gap-3 rounded-md border bg-background p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center',
        disabled && 'opacity-70',
      )}
    >
      <div className="min-w-0">
        <div className="flex items-center gap-2 text-sm font-medium">
          {icon}
          {title}
        </div>
        <p className="mt-1 text-sm text-muted-foreground">{body}</p>
      </div>
      <Button variant="outline" size="sm" onClick={onAction} disabled={disabled}>
        {actionLabel}
      </Button>
    </div>
  )
}

function BatchRecoveryPanel({
  latestResult,
  terminalFailureCount,
  hasFailures,
  onFocusFailed,
  onClearResult,
  onRetryTerminalFailures,
}: {
  latestResult?: OperatorBatchResult | null
  terminalFailureCount: number
  hasFailures: boolean
  onFocusFailed: () => void
  onClearResult: () => void
  onRetryTerminalFailures: () => void
}) {
  const { t } = useTranslation()

  return (
    <div className="space-y-3">
      <Alert variant={hasFailures ? 'destructive' : 'default'}>
        {hasFailures ? <RefreshCw className="size-4" /> : <CheckCircle2 className="size-4" />}
        <AlertTitle>{t('feedback.batch.operator.recovery_title')}</AlertTitle>
        <AlertDescription>
          {latestResult ? (
            <div className="space-y-2">
              <p>
                {t('feedback.batch.operator.latest_result', {
                  succeeded: latestResult.succeeded,
                  skipped: latestResult.skipped,
                  failed: latestResult.failed.length,
                })}
              </p>
              {hasFailures ? (
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" onClick={onFocusFailed}>
                    <ExternalLink className="size-3.5" />
                    {t('feedback.batch.operator.focus_failed')}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={onClearResult}>
                    {t('feedback.batch.operator.clear_result')}
                  </Button>
                </div>
              ) : (
                <Button size="sm" variant="ghost" onClick={onClearResult}>
                  {t('feedback.batch.operator.clear_result')}
                </Button>
              )}
            </div>
          ) : (
            <p>{t('feedback.batch.operator.no_result')}</p>
          )}
        </AlertDescription>
      </Alert>

      <div className="rounded-md border bg-background p-3">
        <div className="text-sm font-medium">
          {t('feedback.batch.operator.terminal_failures_title', {
            count: terminalFailureCount,
          })}
        </div>
        <p className="mt-1 text-sm text-muted-foreground">
          {t('feedback.batch.operator.terminal_failures_body')}
        </p>
        <Button
          className="mt-3"
          size="sm"
          variant="outline"
          onClick={onRetryTerminalFailures}
          disabled={terminalFailureCount === 0}
        >
          <RefreshCw className="size-3.5" />
          {t('feedback.batch.retry_enrichment')}
        </Button>
      </div>
    </div>
  )
}
