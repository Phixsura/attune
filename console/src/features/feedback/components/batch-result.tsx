import { AlertTriangle, CheckCircle, ChevronDown, ChevronUp, XCircle } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface BatchItemFailure {
  feedbackId: string
  code: string
  message: string
}

interface BatchResultProps {
  totalMatched: number
  succeeded: number
  skipped: number
  failed: BatchItemFailure[]
  onDismiss?: () => void
  onRetry?: () => void
  retryLabel?: string
}

// BatchResult — displays batch operation results with success/partial/failure states.
// Shows counts as colored badges and optionally expands to show individual failure details.
export function BatchResult({
  totalMatched: _totalMatched,
  succeeded,
  skipped,
  failed,
  onDismiss,
  onRetry,
  retryLabel,
}: BatchResultProps) {
  const { t } = useTranslation()
  const [showFailures, setShowFailures] = useState(false)

  // Determine overall status
  const isFullSuccess = failed.length === 0 && skipped === 0
  const isPartialSuccess = succeeded > 0 && (failed.length > 0 || skipped > 0)
  const isFullFailure = succeeded === 0 && failed.length > 0

  const variant = isFullFailure ? 'destructive' : 'default'

  return (
    <Alert variant={variant}>
      {isFullSuccess && <CheckCircle className="h-4 w-4 text-green-500" />}
      {isPartialSuccess && <AlertTriangle className="h-4 w-4 text-amber-500" />}
      {isFullFailure && <XCircle className="h-4 w-4" />}
      <AlertTitle>
        {isFullSuccess && t('feedback.batch.result_success')}
        {isPartialSuccess && t('feedback.batch.result_partial')}
        {isFullFailure && t('feedback.batch.result_failed')}
      </AlertTitle>
      <AlertDescription>
        <div className="mt-2 flex flex-wrap gap-2">
          <CountBadge
            variant="success"
            count={succeeded}
            label={t('feedback.batch.result_succeeded', { count: succeeded })}
          />
          {skipped > 0 && (
            <CountBadge
              variant="muted"
              count={skipped}
              label={t('feedback.batch.result_skipped', { count: skipped })}
            />
          )}
          {failed.length > 0 && (
            <CountBadge
              variant="error"
              count={failed.length}
              label={t('feedback.batch.result_failed_count', { count: failed.length })}
            />
          )}
        </div>

        {/* Expandable failure details */}
        {failed.length > 0 && (
          <div className="mt-3">
            <button
              type="button"
              onClick={() => setShowFailures(!showFailures)}
              className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
            >
              {showFailures ? (
                <ChevronUp className="h-4 w-4" />
              ) : (
                <ChevronDown className="h-4 w-4" />
              )}
              {t('feedback.batch.show_failures')}
            </button>

            {showFailures && (
              <ul className="mt-2 space-y-1 text-sm">
                {failed.slice(0, 10).map((f) => (
                  <li key={f.feedbackId} className="flex items-center gap-2">
                    <span className="font-mono text-xs">{f.feedbackId.slice(0, 8)}...</span>
                    <span className="text-muted-foreground">{f.message}</span>
                  </li>
                ))}
                {failed.length > 10 && (
                  <li className="text-muted-foreground">
                    {t('feedback.batch.more_failures', { count: failed.length - 10 })}
                  </li>
                )}
              </ul>
            )}
          </div>
        )}

        {/* Actions */}
        <div className="mt-3 flex gap-2">
          {onRetry && failed.length > 0 && (
            <Button variant="outline" size="sm" onClick={onRetry}>
              {retryLabel ?? t('feedback.batch.retry_failed')}
            </Button>
          )}
          {onDismiss && (
            <Button variant="ghost" size="sm" onClick={onDismiss}>
              {t('common.dismiss')}
            </Button>
          )}
        </div>
      </AlertDescription>
    </Alert>
  )
}

interface CountBadgeProps {
  variant: 'success' | 'error' | 'muted'
  count: number
  label: string
}

function CountBadge({ variant, label }: CountBadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium',
        variant === 'success' &&
          'border-green-200 bg-green-50 text-green-700 dark:border-green-800 dark:bg-green-950 dark:text-green-300',
        variant === 'error' &&
          'border-red-200 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-300',
        variant === 'muted' && 'border-border bg-muted/40 text-muted-foreground',
      )}
    >
      {label}
    </span>
  )
}
