import { AlertCircle, CheckCircle2, Loader2, PlayCircle, Send } from 'lucide-react'
import { type ReactNode, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  useBatchPreviewFeedbackRequestNotifications,
  useBatchPublishFeedbackRequestUpdates,
} from '@/features/feedback/api/batch-request-notifications'
import type { OperatorBatchResult } from '@/features/feedback/components/batch-operator-command-center'
import {
  type BatchPreviewRequestNotificationsResponse,
  type BatchPublishRequestUpdatesResponse,
  RequestNotificationChannel,
} from '@/proto/attune/v1/request_notification'

interface BatchRequestNotificationDialogProps {
  open: boolean
  selectedFeedbackCount: number
  onCancel: () => void
  onCompleted: (result: OperatorBatchResult) => void
}

export function BatchRequestNotificationDialog({
  open,
  selectedFeedbackCount,
  onCancel,
  onCompleted,
}: BatchRequestNotificationDialogProps) {
  const { t } = useTranslation()
  const preview = useBatchPreviewFeedbackRequestNotifications()
  const publish = useBatchPublishFeedbackRequestUpdates()
  const [requestIDs, setRequestIDs] = useState('')
  const [kind, setKind] = useState('status_change')
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [email, setEmail] = useState(true)
  const [webhook, setWebhook] = useState(false)
  const [previewResult, setPreviewResult] =
    useState<BatchPreviewRequestNotificationsResponse | null>(null)
  const [publishResult, setPublishResult] = useState<BatchPublishRequestUpdatesResponse | null>(
    null,
  )

  const parsedRequestIDs = useMemo(() => parseRequestIDs(requestIDs), [requestIDs])
  const channels = useMemo(() => selectedChannels(email, webhook), [email, webhook])
  const canSubmit =
    parsedRequestIDs.length > 0 && title.trim() !== '' && body.trim() !== '' && channels.length > 0

  const requestPayload = () => ({
    updates: parsedRequestIDs.map((requestId) => ({
      requestId,
      title: title.trim(),
      body: body.trim(),
      kind,
      notifySubscribers: true,
    })),
    channels,
  })

  const handlePreview = () => {
    if (!canSubmit) return
    preview.mutate(requestPayload(), {
      onSuccess: (result) => {
        setPreviewResult(result)
        setPublishResult(null)
      },
      onError: () => toast.error(t('feedback.batch.notify.preview_failed')),
    })
  }

  const handlePublish = () => {
    if (!canSubmit) return
    publish.mutate(
      { ...requestPayload(), confirmLargeAudience: true },
      {
        onSuccess: (result) => {
          setPublishResult(result)
          onCompleted(batchResultFromPublish(result))
          if ((result.failed?.length ?? 0) > 0) {
            toast.error(t('feedback.batch.notify.publish_partial', { count: result.failed.length }))
            return
          }
          toast.success(t('feedback.batch.notify.publish_success', { count: result.succeeded }))
        },
        onError: () => toast.error(t('feedback.batch.notify.publish_failed')),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onCancel()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t('feedback.batch.notify.title')}</DialogTitle>
          <DialogDescription>
            {t('feedback.batch.notify.description', { count: selectedFeedbackCount })}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <FormField label={t('feedback.batch.notify.request_ids')}>
            <textarea
              data-testid="feedback-batch-notify-request-ids"
              className="min-h-24 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-2 font-mono text-sm shadow-xs outline-none transition-[color,box-shadow] focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
              placeholder={t('feedback.batch.notify.request_ids_placeholder')}
              value={requestIDs}
              onChange={(event) => setRequestIDs(event.target.value)}
            />
          </FormField>
          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem]">
            <FormField label={t('request_notifications.publish.title_label')}>
              <Input
                aria-label={t('request_notifications.publish.title_label')}
                value={title}
                onChange={(event) => setTitle(event.target.value)}
              />
            </FormField>
            <FormField label={t('request_notifications.publish.kind')}>
              <Select value={kind} onValueChange={setKind}>
                <SelectTrigger aria-label={t('request_notifications.publish.kind')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="status_change">
                    {t('request_notifications.publish.kind_status')}
                  </SelectItem>
                  <SelectItem value="shipped">
                    {t('request_notifications.publish.kind_shipped')}
                  </SelectItem>
                </SelectContent>
              </Select>
            </FormField>
          </div>
          <FormField label={t('request_notifications.publish.body')}>
            <textarea
              aria-label={t('request_notifications.publish.body')}
              className="min-h-28 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow] focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
              value={body}
              onChange={(event) => setBody(event.target.value)}
            />
          </FormField>
          <div className="flex flex-wrap gap-4">
            <ChannelToggle
              checked={email}
              label={t('request_notifications.channels.email')}
              onCheckedChange={setEmail}
            />
            <ChannelToggle
              checked={webhook}
              label={t('request_notifications.channels.webhook')}
              onCheckedChange={setWebhook}
            />
          </div>
          <BatchNotifySummary previewResult={previewResult} publishResult={publishResult} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="outline"
            disabled={!canSubmit || preview.isPending}
            onClick={handlePreview}
          >
            {preview.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <PlayCircle className="size-4" />
            )}
            {t('feedback.batch.notify.preview')}
          </Button>
          <Button disabled={!canSubmit || publish.isPending} onClick={handlePublish}>
            {publish.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Send className="size-4" />
            )}
            {t('feedback.batch.notify.publish')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function BatchNotifySummary({
  previewResult,
  publishResult,
}: {
  previewResult: BatchPreviewRequestNotificationsResponse | null
  publishResult: BatchPublishRequestUpdatesResponse | null
}) {
  const { t } = useTranslation()
  const result = publishResult ?? previewResult
  if (!result) {
    return (
      <Alert>
        <AlertCircle className="size-4" />
        <AlertTitle>{t('feedback.batch.notify.no_preview_title')}</AlertTitle>
        <AlertDescription>{t('feedback.batch.notify.no_preview_body')}</AlertDescription>
      </Alert>
    )
  }
  const failed = result.failed ?? []
  return (
    <Alert variant={failed.length > 0 ? 'destructive' : 'default'}>
      {failed.length > 0 ? <AlertCircle className="size-4" /> : <CheckCircle2 className="size-4" />}
      <AlertTitle>
        {publishResult
          ? t('feedback.batch.notify.publish_result')
          : t('feedback.batch.notify.preview_result')}
      </AlertTitle>
      <AlertDescription>
        <div className="space-y-2">
          <p>
            {publishResult
              ? t('feedback.batch.notify.publish_counts', {
                  failed: failed.length,
                  succeeded: publishResult.succeeded,
                  total: publishResult.totalMatched,
                })
              : t('feedback.batch.notify.preview_counts', {
                  eligible: previewResult?.eligibleRecipients ?? 0,
                  excluded: previewResult?.excludedRecipients ?? 0,
                  failed: failed.length,
                  total: previewResult?.totalMatched ?? 0,
                })}
          </p>
          <FailureList failures={failed} />
        </div>
      </AlertDescription>
    </Alert>
  )
}

function FailureList({ failures }: { failures: Array<{ requestId: string; message: string }> }) {
  if (failures.length === 0) return null
  return (
    <ul className="space-y-1">
      {failures.slice(0, 6).map((failure) => (
        <li key={`${failure.requestId}:${failure.message}`} className="flex gap-2 text-sm">
          <span className="font-mono text-xs">{failure.requestId || 'unknown'}</span>
          <span>{failure.message}</span>
        </li>
      ))}
    </ul>
  )
}

function FormField({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-2">
      <Label>{label}</Label>
      {children}
    </div>
  )
}

function ChannelToggle({
  checked,
  label,
  onCheckedChange,
}: {
  checked: boolean
  label: string
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <Label>
      <Checkbox checked={checked} onCheckedChange={(next) => onCheckedChange(next === true)} />
      {label}
    </Label>
  )
}

function selectedChannels(email: boolean, webhook: boolean) {
  const channels: RequestNotificationChannel[] = []
  if (email) channels.push(RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL)
  if (webhook) channels.push(RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK)
  return channels
}

function parseRequestIDs(raw: string) {
  const seen = new Set<string>()
  const out: string[] = []
  for (const value of raw.split(/[\s,]+/)) {
    const requestID = value.trim()
    if (!requestID || seen.has(requestID)) continue
    seen.add(requestID)
    out.push(requestID)
  }
  return out
}

function batchResultFromPublish(result: BatchPublishRequestUpdatesResponse): OperatorBatchResult {
  return {
    action: 'notify',
    total: result.totalMatched,
    succeeded: result.succeeded,
    skipped: result.skipped,
    failed: (result.failed ?? []).map((failure) => ({
      feedbackId: failure.requestId,
      code: failure.code,
      message: failure.message,
    })),
  }
}
