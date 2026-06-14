import { Ban, CheckCircle, Loader2, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'

interface JobProgressProps {
  jobId: string
  status: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled'
  total: number
  progress: number
  error?: string
  onCancel?: () => void
  onDismiss?: () => void
}

export function JobProgress({
  jobId: _jobId,
  status,
  total,
  progress,
  error,
  onCancel,
  onDismiss,
}: JobProgressProps) {
  const { t } = useTranslation()
  const percentage = total > 0 ? Math.round((progress / total) * 100) : 0

  return (
    <Card className="w-full max-w-md">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          {status === 'running' && <Loader2 className="h-4 w-4 animate-spin" />}
          {status === 'completed' && <CheckCircle className="h-4 w-4 text-green-500" />}
          {status === 'failed' && <XCircle className="h-4 w-4 text-red-500" />}
          {status === 'cancelled' && <Ban className="h-4 w-4 text-yellow-500" />}
          {status === 'queued' && <Loader2 className="h-4 w-4" />}
          {t(`feedback.job.status_${status}`)}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {(status === 'running' || status === 'queued') && (
          <>
            <Progress value={percentage} className="mb-2" />
            <p className="text-xs text-muted-foreground">
              {t('feedback.job.progress', { progress, total, percentage })}
            </p>
          </>
        )}

        {status === 'failed' && error && <p className="text-sm text-red-500">{error}</p>}

        <div className="mt-3 flex gap-2">
          {(status === 'queued' || status === 'running') && onCancel && (
            <Button variant="outline" size="sm" onClick={onCancel}>
              {t('common.cancel')}
            </Button>
          )}
          {(status === 'completed' || status === 'failed' || status === 'cancelled') &&
            onDismiss && (
              <Button variant="outline" size="sm" onClick={onDismiss}>
                {t('common.close')}
              </Button>
            )}
        </div>
      </CardContent>
    </Card>
  )
}
