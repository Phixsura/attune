import { useQuery } from '@tanstack/react-query'
import {
  AlertTriangle,
  Check,
  Clock3,
  Code2,
  Copy,
  Fingerprint,
  KeyRound,
  Link2,
  ListChecks,
  Loader2,
  RotateCcw,
  Send,
  ShieldCheck,
  TestTube2,
  Trash2,
} from 'lucide-react'
import type { FormEvent, ReactNode } from 'react'
import { Fragment, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  replySendHookDeliveriesQuery,
  replySendHookHealthQuery,
  replySendHookQuery,
  useDisableReplySendHook,
  useRedeliverReplySendHookDelivery,
  useTestReplySendHook,
  useUpsertReplySendHook,
} from '@/features/reply-send-hook/api/reply-send-hook'
import { useDocumentTitle } from '@/hooks/use-document-title'
import type { ReplySendHookDelivery, ReplySendHookHealth } from '@/proto/attune/v1/ingest'

export function ReplySendHookPage() {
  const { i18n, t } = useTranslation()
  useDocumentTitle(t('nav.reply_send_hook'))
  const query = useQuery(replySendHookQuery())
  const deliveriesQuery = useQuery(replySendHookDeliveriesQuery(25))
  const healthQuery = useQuery(replySendHookHealthQuery())
  const hook = query.data ?? null
  const deliveries = deliveriesQuery.data ?? []
  const deliveryHealth = deliveryHealthForUI(healthQuery.data, deliveries)
  const latestDelivery = deliveryHealth.latestDelivery
  const upsert = useUpsertReplySendHook()
  const disable = useDisableReplySendHook()
  const testHook = useTestReplySendHook()
  const redeliver = useRedeliverReplySendHookDelivery()
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [secretOnce, setSecretOnce] = useState('')
  const [disableConfirmOpen, setDisableConfirmOpen] = useState(false)
  const [redeliveringId, setRedeliveringId] = useState('')

  useEffect(() => {
    setName(hook?.name ?? '')
    setEnabled(hook?.enabled ?? true)
  }, [hook?.name, hook?.enabled])

  const pending = upsert.isPending || disable.isPending || testHook.isPending || redeliver.isPending
  const statusKey = hook ? (hook.enabled ? 'active' : 'inactive') : 'missing'
  const latestStatusKey = latestDelivery?.status || 'none'
  const locale = i18n.resolvedLanguage || i18n.language || undefined
  const samplePayload = replySendSamplePayload(hook?.id)
  const cleanURL = url.trim()
  const urlErrorKey = cleanURL ? validateReplySendHookURL(cleanURL) : ''
  const cleanSecret = secret.trim()
  const secretErrorKey =
    cleanSecret && cleanSecret.length < 16 ? 'reply_send_hook.form.secret_error_min_length' : ''
  const canSave = Boolean(cleanURL) && !urlErrorKey && !secretErrorKey && !pending

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!cleanURL) return
    if (urlErrorKey || secretErrorKey) {
      toast.error(t('reply_send_hook.form.validation_failed'))
      return
    }
    upsert.mutate(
      {
        enabled,
        name: name.trim() || undefined,
        secret: cleanSecret || undefined,
        url: cleanURL,
      },
      {
        onSuccess: (saved) => {
          setUrl('')
          setSecret('')
          setSecretOnce(saved.secretOnce ?? '')
          toast.success(t('reply_send_hook.toast.saved'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleDisable = () => {
    disable.mutate(undefined, {
      onSuccess: () => {
        setSecretOnce('')
        setDisableConfirmOpen(false)
        toast.success(t('reply_send_hook.toast.disabled'))
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
    })
  }

  const handleCopySecret = () => {
    navigator.clipboard
      .writeText(secretOnce)
      .then(() => toast.success(t('reply_send_hook.toast.copied')))
      .catch(() => toast.error(t('reply_send_hook.toast.copy_failed')))
  }

  const handleCopyPayload = () => {
    navigator.clipboard
      .writeText(samplePayload)
      .then(() => toast.success(t('reply_send_hook.contract.sample_copied')))
      .catch(() => toast.error(t('reply_send_hook.contract.sample_copy_failed')))
  }

  const handleTest = () => {
    testHook.mutate(undefined, {
      onSuccess: (delivery) => {
        if (delivery.status === 'accepted') {
          toast.success(t('reply_send_hook.toast.test_accepted'))
          return
        }
        toast.error(t('reply_send_hook.toast.test_failed'))
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
    })
  }

  const handleRedeliver = (delivery: ReplySendHookDelivery) => {
    setRedeliveringId(delivery.id)
    redeliver.mutate(delivery.id, {
      onSuccess: (next) => {
        if (next.status === 'accepted') {
          toast.success(t('reply_send_hook.toast.redelivered'))
          return
        }
        toast.error(t('reply_send_hook.toast.redeliver_failed'))
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      onSettled: () => setRedeliveringId(''),
    })
  }

  return (
    <>
      <div className="space-y-6">
        <PageHero
          eyebrow={t('shell.groups.integrations')}
          title={t('nav.reply_send_hook')}
          subtitle={t('reply_send_hook.subtitle')}
          metrics={
            <>
              <PageHeroMetric
                label={t('reply_send_hook.summary.status')}
                value={t(`reply_send_hook.status.${statusKey}`)}
                tone={hook?.enabled ? 'active' : 'default'}
              />
              <PageHeroMetric
                label={t('reply_send_hook.summary.host')}
                value={hook?.urlHost || t('reply_send_hook.summary.none')}
              />
              <PageHeroMetric
                label={t('reply_send_hook.summary.fingerprint')}
                value={hook?.urlFingerprint ? shortFingerprint(hook.urlFingerprint) : '-'}
              />
              <PageHeroMetric
                label={t('reply_send_hook.summary.latest_delivery')}
                value={t(`reply_send_hook.delivery.status.${latestStatusKey}`)}
                tone={latestDelivery?.status === 'accepted' ? 'active' : 'default'}
              />
            </>
          }
        />

        <DeliveryHealthBand
          health={deliveryHealth}
          isLoading={healthQuery.isPending && !healthQuery.data}
          latestDelivery={latestDelivery}
          locale={locale}
        />

        <div className="grid gap-6 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
          <Card className="border-border/60 shadow-none">
            <CardHeader>
              <CardTitle className="text-base">{t('reply_send_hook.current.title')}</CardTitle>
              <CardDescription>{t('reply_send_hook.current.description')}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4 pt-6">
              {query.isPending ? (
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" />
                  {t('common.loading')}
                </div>
              ) : hook ? (
                <>
                  <HookField
                    icon={<Send className="size-4" />}
                    label={t('reply_send_hook.current.name')}
                    value={hook.name}
                  />
                  <HookField
                    icon={<Link2 className="size-4" />}
                    label={t('reply_send_hook.current.host')}
                    value={hook.urlHost}
                  />
                  <HookField
                    icon={<KeyRound className="size-4" />}
                    label={t('reply_send_hook.current.fingerprint')}
                    value={hook.urlFingerprint}
                    mono
                  />
                  <HookField
                    icon={<Check className="size-4" />}
                    label={t('reply_send_hook.current.state')}
                    value={t(`reply_send_hook.status.${hook.enabled ? 'active' : 'inactive'}`)}
                  />
                  <div className="flex flex-wrap gap-2 pt-1">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={handleTest}
                      disabled={!hook.enabled || pending}
                    >
                      {testHook.isPending ? (
                        <Loader2 className="size-3.5 animate-spin" />
                      ) : (
                        <TestTube2 className="size-3.5" />
                      )}
                      {t('reply_send_hook.test.action')}
                    </Button>
                    <Button
                      type="button"
                      variant="destructive"
                      size="sm"
                      onClick={() => setDisableConfirmOpen(true)}
                      disabled={!hook.enabled || pending}
                    >
                      {disable.isPending ? (
                        <Loader2 className="size-3.5 animate-spin" />
                      ) : (
                        <Trash2 className="size-3.5" />
                      )}
                      {t('reply_send_hook.disable')}
                    </Button>
                  </div>
                </>
              ) : (
                <div className="rounded-md border border-dashed border-border px-4 py-5">
                  <div className="text-sm font-medium text-foreground">
                    {t('reply_send_hook.empty.title')}
                  </div>
                  <div className="mt-1 text-sm leading-6 text-muted-foreground">
                    {t('reply_send_hook.empty.body')}
                  </div>
                </div>
              )}
            </CardContent>
          </Card>

          <Card className="border-border/60 shadow-none">
            <CardHeader>
              <CardTitle className="text-base">
                {hook ? t('reply_send_hook.form.replace_title') : t('reply_send_hook.form.title')}
              </CardTitle>
              <CardDescription>{t('reply_send_hook.form.description')}</CardDescription>
            </CardHeader>
            <CardContent className="pt-6">
              <form className="space-y-4" onSubmit={handleSubmit} noValidate>
                <div className="space-y-2">
                  <Label htmlFor="reply-send-hook-name">{t('reply_send_hook.form.name')}</Label>
                  <Input
                    id="reply-send-hook-name"
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    disabled={pending}
                    maxLength={120}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="reply-send-hook-url">{t('reply_send_hook.form.url')}</Label>
                  <Input
                    id="reply-send-hook-url"
                    value={url}
                    onChange={(event) => setUrl(event.target.value)}
                    disabled={pending}
                    placeholder="https://hooks.example.com/attune/replies"
                    required
                    aria-invalid={Boolean(urlErrorKey)}
                    aria-describedby={
                      urlErrorKey ? 'reply-send-hook-url-error' : 'reply-send-hook-url-help'
                    }
                  />
                  {urlErrorKey ? (
                    <p
                      id="reply-send-hook-url-error"
                      className="text-xs leading-5 text-destructive"
                    >
                      {t(urlErrorKey)}
                    </p>
                  ) : (
                    <p
                      id="reply-send-hook-url-help"
                      className="text-xs leading-5 text-muted-foreground"
                    >
                      {t('reply_send_hook.form.url_help')}
                    </p>
                  )}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="reply-send-hook-secret">{t('reply_send_hook.form.secret')}</Label>
                  <Input
                    id="reply-send-hook-secret"
                    type="password"
                    value={secret}
                    onChange={(event) => setSecret(event.target.value)}
                    disabled={pending}
                    minLength={16}
                    aria-invalid={Boolean(secretErrorKey)}
                    aria-describedby={
                      secretErrorKey
                        ? 'reply-send-hook-secret-error'
                        : 'reply-send-hook-secret-help'
                    }
                  />
                  {secretErrorKey ? (
                    <p
                      id="reply-send-hook-secret-error"
                      className="text-xs leading-5 text-destructive"
                    >
                      {t(secretErrorKey)}
                    </p>
                  ) : (
                    <p
                      id="reply-send-hook-secret-help"
                      className="text-xs leading-5 text-muted-foreground"
                    >
                      {t('reply_send_hook.form.secret_help')}
                    </p>
                  )}
                </div>
                <label
                  htmlFor="reply-send-hook-enabled"
                  className="flex items-center gap-2 text-sm text-foreground"
                >
                  <Checkbox
                    id="reply-send-hook-enabled"
                    checked={enabled}
                    onCheckedChange={(value) => setEnabled(value === true)}
                    disabled={pending}
                  />
                  {t('reply_send_hook.form.enabled')}
                </label>
                <div className="flex justify-end">
                  <Button type="submit" disabled={!canSave}>
                    {upsert.isPending ? (
                      <Loader2 className="size-3.5 animate-spin" />
                    ) : (
                      <Check className="size-3.5" />
                    )}
                    {t('common.save')}
                  </Button>
                </div>
              </form>

              {secretOnce ? (
                <Alert className="mt-5">
                  <KeyRound />
                  <AlertTitle>{t('reply_send_hook.secret_once.title')}</AlertTitle>
                  <AlertDescription>
                    <div className="flex w-full min-w-0 flex-col gap-2 sm:flex-row sm:items-center">
                      <code className="min-w-0 flex-1 break-all rounded-md bg-muted px-2 py-1 font-mono text-xs text-foreground">
                        {secretOnce}
                      </code>
                      <Button type="button" size="sm" variant="outline" onClick={handleCopySecret}>
                        <Copy className="size-3.5" />
                        {t('common.copy')}
                      </Button>
                    </div>
                    <p>{t('reply_send_hook.secret_once.body')}</p>
                  </AlertDescription>
                </Alert>
              ) : null}
            </CardContent>
          </Card>
        </div>

        <Card data-testid="reply-send-hook-deliveries" className="border-border/60 shadow-none">
          <CardHeader className="gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <CardTitle className="flex items-center gap-2 text-base">
                <Clock3 className="size-4 text-muted-foreground" />
                {t('reply_send_hook.delivery.title')}
              </CardTitle>
              <CardDescription>{t('reply_send_hook.delivery.description')}</CardDescription>
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handleTest}
              disabled={!hook?.enabled || pending}
            >
              {testHook.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <TestTube2 className="size-3.5" />
              )}
              {t('reply_send_hook.test.action')}
            </Button>
          </CardHeader>
          <CardContent className="pt-6">
            {deliveriesQuery.isPending ? (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                {t('common.loading')}
              </div>
            ) : deliveries.length === 0 ? (
              <div className="rounded-md border border-dashed border-border px-4 py-5 text-sm text-muted-foreground">
                {t('reply_send_hook.delivery.empty')}
              </div>
            ) : (
              <div className="min-w-0 overflow-x-auto">
                <Table className="w-full table-fixed">
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead>{t('reply_send_hook.delivery.col_event')}</TableHead>
                      <TableHead>{t('reply_send_hook.delivery.col_status')}</TableHead>
                      <TableHead>{t('reply_send_hook.delivery.col_http')}</TableHead>
                      <TableHead>{t('reply_send_hook.delivery.col_attempts')}</TableHead>
                      <TableHead>{t('reply_send_hook.delivery.col_time')}</TableHead>
                      <TableHead>{t('reply_send_hook.delivery.col_error')}</TableHead>
                      <TableHead className="w-[72px]" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {deliveries.map((delivery) => (
                      <Fragment key={delivery.id}>
                        <TableRow>
                          <TableCell>
                            <div className="font-mono text-xs text-foreground">
                              {delivery.eventType}
                            </div>
                            <div className="mt-1 font-mono text-[11px] text-muted-foreground">
                              {shortFingerprint(delivery.id)}
                            </div>
                          </TableCell>
                          <TableCell>
                            <DeliveryStatusPill delivery={delivery} />
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            {delivery.httpStatus || '-'}
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            {delivery.attempts}/{delivery.maxAttempts}
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            <div>
                              {formatDeliveryTime(
                                delivery.completedAt || delivery.requestedAt,
                                locale,
                              )}
                            </div>
                            {delivery.nextRetryAt ? (
                              <div className="mt-1 text-[11px] text-amber-700 dark:text-amber-300">
                                {t('reply_send_hook.delivery.next_retry_inline', {
                                  time: formatDeliveryTime(delivery.nextRetryAt, locale),
                                })}
                              </div>
                            ) : null}
                          </TableCell>
                          <TableCell className="max-w-[22rem] whitespace-normal break-words text-xs leading-5 text-muted-foreground">
                            {delivery.error || delivery.externalMessageId || '-'}
                          </TableCell>
                          <TableCell className="text-right">
                            {delivery.retryable ? (
                              <Button
                                type="button"
                                size="sm"
                                variant="ghost"
                                aria-label={t('reply_send_hook.delivery.redeliver_aria', {
                                  id: shortFingerprint(delivery.id),
                                })}
                                onClick={() => handleRedeliver(delivery)}
                                disabled={pending}
                              >
                                {redeliver.isPending && redeliveringId === delivery.id ? (
                                  <Loader2 className="size-3.5 animate-spin" />
                                ) : (
                                  <RotateCcw className="size-3.5" />
                                )}
                              </Button>
                            ) : null}
                          </TableCell>
                        </TableRow>
                        <TableRow className="border-b bg-muted/20 hover:bg-muted/20">
                          <TableCell colSpan={7} className="px-4 py-3">
                            <DeliveryDiagnostics delivery={delivery} locale={locale} />
                          </TableCell>
                        </TableRow>
                      </Fragment>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </CardContent>
        </Card>

        <div
          data-testid="reply-send-hook-contract"
          className="grid gap-6 xl:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]"
        >
          <Card className="border-border/60 shadow-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <Code2 className="size-4 text-muted-foreground" />
                {t('reply_send_hook.contract.title')}
              </CardTitle>
              <CardDescription>{t('reply_send_hook.contract.description')}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4 pt-6">
              <div className="grid gap-3 md:grid-cols-3">
                <ContractSignal
                  icon={<Send className="size-4" />}
                  title={t('reply_send_hook.contract.post.title')}
                  body={t('reply_send_hook.contract.post.body')}
                />
                <ContractSignal
                  icon={<Fingerprint className="size-4" />}
                  title={t('reply_send_hook.contract.signed.title')}
                  body={t('reply_send_hook.contract.signed.body')}
                />
                <ContractSignal
                  icon={<ShieldCheck className="size-4" />}
                  title={t('reply_send_hook.contract.idempotent.title')}
                  body={t('reply_send_hook.contract.idempotent.body')}
                />
              </div>

              <div className="overflow-hidden rounded-md border border-border/70">
                <div className="border-b border-border/70 bg-muted/30 px-3 py-2 text-xs font-medium text-muted-foreground">
                  {t('reply_send_hook.contract.headers')}
                </div>
                <dl className="divide-y divide-border/60">
                  {replySendHeaders(t).map((header) => (
                    <div
                      key={header.name}
                      className="grid gap-1 px-3 py-2 text-xs sm:grid-cols-[12rem_minmax(0,1fr)]"
                    >
                      <dt className="font-mono text-foreground">{header.name}</dt>
                      <dd className="min-w-0 break-words text-muted-foreground">{header.value}</dd>
                    </div>
                  ))}
                </dl>
              </div>

              <div className="overflow-hidden rounded-md border border-border/70">
                <div className="flex items-center justify-between gap-3 border-b border-border/70 bg-muted/30 px-3 py-2">
                  <span className="text-xs font-medium text-muted-foreground">
                    {t('reply_send_hook.contract.payload')}
                  </span>
                  <Button type="button" size="sm" variant="ghost" onClick={handleCopyPayload}>
                    <Copy className="size-3.5" />
                    {t('reply_send_hook.contract.copy_payload')}
                  </Button>
                </div>
                <textarea
                  aria-label={t('reply_send_hook.contract.payload_label')}
                  className="h-72 w-full resize-none overflow-auto border-0 bg-background p-3 font-mono text-xs leading-5 text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                  readOnly
                  value={samplePayload}
                />
              </div>
            </CardContent>
          </Card>

          <Card className="border-border/60 shadow-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <ListChecks className="size-4 text-muted-foreground" />
                {t('reply_send_hook.security.title')}
              </CardTitle>
              <CardDescription>{t('reply_send_hook.security.description')}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 pt-6">
              <SecurityCheckItem
                icon={<Link2 className="size-4" />}
                title={t('reply_send_hook.security.https.title')}
                body={t('reply_send_hook.security.https.body')}
              />
              <SecurityCheckItem
                icon={<KeyRound className="size-4" />}
                title={t('reply_send_hook.security.secret.title')}
                body={t('reply_send_hook.security.secret.body')}
              />
              <SecurityCheckItem
                icon={<Fingerprint className="size-4" />}
                title={t('reply_send_hook.security.fingerprint.title')}
                body={t('reply_send_hook.security.fingerprint.body')}
              />
              <SecurityCheckItem
                icon={<ShieldCheck className="size-4" />}
                title={t('reply_send_hook.security.audit.title')}
                body={t('reply_send_hook.security.audit.body')}
              />
            </CardContent>
          </Card>
        </div>
      </div>

      <Dialog open={disableConfirmOpen} onOpenChange={setDisableConfirmOpen}>
        <DialogContent role="alertdialog" showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>{t('reply_send_hook.disable_dialog.title')}</DialogTitle>
            <DialogDescription>
              {t('reply_send_hook.disable_dialog.body', {
                host: hook?.urlHost || t('reply_send_hook.summary.none'),
              })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setDisableConfirmOpen(false)}
              disabled={pending}
            >
              {t('common.cancel')}
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={handleDisable}
              disabled={!hook?.enabled || pending}
            >
              {disable.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Trash2 className="size-3.5" />
              )}
              {t('reply_send_hook.disable_dialog.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function validateReplySendHookURL(value: string) {
  let parsed: URL
  try {
    parsed = new URL(value)
  } catch {
    return 'reply_send_hook.form.url_error_invalid'
  }
  const loopbackHTTP = parsed.protocol === 'http:' && isLoopbackHost(parsed.hostname)
  if ((parsed.protocol !== 'https:' && !loopbackHTTP) || !parsed.hostname) {
    return 'reply_send_hook.form.url_error_https'
  }
  if (parsed.username || parsed.password) {
    return 'reply_send_hook.form.url_error_credentials'
  }
  return ''
}

function isLoopbackHost(hostname: string) {
  const host = hostname
    .trim()
    .toLowerCase()
    .replace(/^\[(.*)\]$/, '$1')
  if (host === 'localhost' || host === '::1') return true
  const parts = host.split('.')
  if (parts.length !== 4 || parts[0] !== '127') return false
  return parts.every((part) => {
    if (!/^\d+$/.test(part)) return false
    const value = Number(part)
    return value >= 0 && value <= 255
  })
}

interface DeliveryHealth {
  total: number
  accepted: number
  failed: number
  dead: number
  pending: number
  retryable: number
  latestDelivery?: ReplySendHookDelivery
  latestProblem?: ReplySendHookDelivery
}

function deliveryHealthForUI(
  health: ReplySendHookHealth | null | undefined,
  deliveries: ReplySendHookDelivery[],
): DeliveryHealth {
  if (!health) return summarizeDeliveryHealth(deliveries)
  return {
    accepted: countFromProto(health.accepted),
    dead: countFromProto(health.dead),
    failed: countFromProto(health.failed),
    latestDelivery: health.latestDelivery,
    latestProblem: health.latestProblem,
    pending: countFromProto(health.pending),
    retryable: countFromProto(health.retryable),
    total: countFromProto(health.total),
  }
}

function summarizeDeliveryHealth(deliveries: ReplySendHookDelivery[]): DeliveryHealth {
  return deliveries.reduce<DeliveryHealth>(
    (health, delivery) => {
      health.total += 1
      if (delivery.status === 'accepted') health.accepted += 1
      if (delivery.status === 'failed') health.failed += 1
      if (delivery.status === 'dead') health.dead += 1
      if (delivery.status === 'pending') health.pending += 1
      if (delivery.retryable) health.retryable += 1
      if (!health.latestProblem && (delivery.status === 'failed' || delivery.status === 'dead')) {
        health.latestProblem = delivery
      }
      if (!health.latestDelivery) health.latestDelivery = delivery
      return health
    },
    { accepted: 0, dead: 0, failed: 0, pending: 0, retryable: 0, total: 0 },
  )
}

function countFromProto(value: number | string | null | undefined) {
  if (typeof value === 'number') return value
  if (!value) return 0
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

function DeliveryHealthBand({
  health,
  isLoading,
  latestDelivery,
  locale,
}: {
  health: DeliveryHealth
  isLoading: boolean
  latestDelivery?: ReplySendHookDelivery
  locale?: string
}) {
  const { t } = useTranslation()
  const hasProblem = health.failed > 0 || health.dead > 0
  const statusKey = isLoading
    ? 'loading'
    : health.total === 0
      ? 'empty'
      : hasProblem
        ? 'attention'
        : 'ok'
  const containerClass = hasProblem
    ? 'border-amber-300 bg-amber-50/80 text-amber-950 dark:border-amber-900 dark:bg-amber-950/35 dark:text-amber-100'
    : 'border-border/70 bg-background text-foreground'
  const latestProblem = health.latestProblem

  return (
    <section
      data-testid="reply-send-hook-health"
      className={`rounded-md border px-4 py-3 ${containerClass}`}
      aria-label={t('reply_send_hook.health.title')}
    >
      <div className="flex flex-col gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-sm font-semibold">
            {hasProblem ? (
              <AlertTriangle className="size-4" />
            ) : isLoading ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <ShieldCheck className="size-4" />
            )}
            {t(`reply_send_hook.health.${statusKey}_title`)}
          </div>
          <p className="mt-1 text-sm leading-6 opacity-80">
            {latestProblem
              ? t('reply_send_hook.health.latest_problem', {
                  event: latestProblem.eventType,
                  status: t(`reply_send_hook.delivery.status.${latestProblem.status}`),
                  message: latestProblem.error || latestProblem.externalMessageId || '-',
                })
              : latestDelivery
                ? t('reply_send_hook.health.latest_ok', {
                    event: latestDelivery.eventType,
                    time: formatDeliveryTime(
                      latestDelivery.completedAt || latestDelivery.requestedAt,
                      locale,
                    ),
                  })
                : t(`reply_send_hook.health.${statusKey}_body`)}
          </p>
        </div>
        <div className="grid w-full min-w-0 grid-cols-2 gap-2 sm:grid-cols-4 lg:flex-1">
          <HealthStat label={t('reply_send_hook.health.accepted')} value={health.accepted} />
          <HealthStat label={t('reply_send_hook.health.failed')} value={health.failed} />
          <HealthStat label={t('reply_send_hook.health.retryable')} value={health.retryable} />
          <HealthStat label={t('reply_send_hook.health.dead')} value={health.dead} />
        </div>
      </div>
    </section>
  )
}

function HealthStat({ label, value }: { label: string; value: number }) {
  return (
    <div className="min-w-0 rounded-md border border-current/15 bg-background/70 px-3 py-2">
      <div className="text-[11px] font-medium opacity-70">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums">{value}</div>
    </div>
  )
}

function ContractSignal({ body, icon, title }: { body: string; icon: ReactNode; title: string }) {
  return (
    <div className="min-w-0 rounded-md border border-border/60 bg-background/75 px-3 py-3">
      <div className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
        <span className="text-muted-foreground">{icon}</span>
        {title}
      </div>
      <p className="text-xs leading-5 text-muted-foreground">{body}</p>
    </div>
  )
}

function SecurityCheckItem({
  body,
  icon,
  title,
}: {
  body: string
  icon: ReactNode
  title: string
}) {
  return (
    <div className="flex items-start gap-3 rounded-md border border-border/60 bg-background/75 px-3 py-3">
      <div className="mt-0.5 text-muted-foreground">{icon}</div>
      <div className="min-w-0">
        <div className="text-sm font-medium text-foreground">{title}</div>
        <p className="mt-1 text-xs leading-5 text-muted-foreground">{body}</p>
      </div>
    </div>
  )
}

function DeliveryStatusPill({ delivery }: { delivery: ReplySendHookDelivery }) {
  const { t } = useTranslation()
  const styles: Record<string, string> = {
    accepted:
      'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-300',
    pending:
      'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300',
    failed:
      'border-red-200 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-300',
    dead: 'border-zinc-300 bg-zinc-100 text-zinc-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
  }
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium ${
        styles[delivery.status] ?? 'border-border bg-muted text-muted-foreground'
      }`}
    >
      {delivery.status === 'failed' || delivery.status === 'dead' ? (
        <AlertTriangle className="size-3" />
      ) : null}
      {t(`reply_send_hook.delivery.status.${delivery.status}`, delivery.status)}
    </span>
  )
}

function DeliveryDiagnostics({
  delivery,
  locale,
}: {
  delivery: ReplySendHookDelivery
  locale?: string
}) {
  const { t } = useTranslation()
  const hookLabel = delivery.hookHost
    ? `${delivery.hookHost} · ${shortFingerprint(delivery.hookFingerprint || delivery.hookId)}`
    : shortFingerprint(delivery.hookId)

  return (
    <div className="grid gap-3 text-xs sm:grid-cols-2 lg:grid-cols-5">
      <DeliveryDiagnostic
        label={t('reply_send_hook.delivery.detail.delivery_id')}
        value={delivery.id}
        mono
      />
      <DeliveryDiagnostic
        label={t('reply_send_hook.delivery.detail.idempotency_key')}
        value={delivery.idempotencyKey}
        mono
      />
      <DeliveryDiagnostic label={t('reply_send_hook.delivery.detail.hook')} value={hookLabel} />
      <DeliveryDiagnostic
        label={t('reply_send_hook.delivery.detail.requested_at')}
        value={formatDeliveryTime(delivery.requestedAt, locale)}
      />
      <DeliveryDiagnostic
        label={
          delivery.nextRetryAt
            ? t('reply_send_hook.delivery.detail.next_retry_at')
            : t('reply_send_hook.delivery.detail.completed_at')
        }
        value={
          delivery.nextRetryAt
            ? formatDeliveryTime(delivery.nextRetryAt, locale)
            : formatDeliveryTime(delivery.completedAt, locale)
        }
      />
    </div>
  )
}

function DeliveryDiagnostic({
  label,
  mono,
  value,
}: {
  label: string
  mono?: boolean
  value: string
}) {
  const valueClass = mono
    ? 'mt-1 break-all font-mono text-[11px] leading-5 text-foreground'
    : 'mt-1 break-words text-xs leading-5 text-foreground'

  return (
    <div className="min-w-0 rounded-md border border-border/60 bg-background/70 px-3 py-2.5">
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className={valueClass}>{value || '-'}</div>
    </div>
  )
}

function HookField({
  icon,
  label,
  mono,
  value,
}: {
  icon: ReactNode
  label: string
  mono?: boolean
  value: string
}) {
  return (
    <div className="flex items-start gap-3 rounded-md border border-border/60 bg-background/75 px-3 py-2.5">
      <div className="mt-0.5 text-muted-foreground">{icon}</div>
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div
          className={
            mono
              ? 'mt-0.5 break-all font-mono text-xs text-foreground'
              : 'mt-0.5 break-words text-sm font-medium text-foreground'
          }
        >
          {value || '-'}
        </div>
      </div>
    </div>
  )
}

function shortFingerprint(value: string) {
  if (value.length <= 12) return value
  return `${value.slice(0, 8)}...${value.slice(-4)}`
}

function formatDeliveryTime(value?: string, locale?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function replySendHeaders(t: (key: string) => string) {
  return [
    {
      name: 'Content-Type',
      value: 'application/json; charset=utf-8',
    },
    {
      name: 'X-Attune-Timestamp',
      value: t('reply_send_hook.contract.header_timestamp'),
    },
    {
      name: 'X-Attune-Signature',
      value: t('reply_send_hook.contract.header_signature'),
    },
    {
      name: 'X-Attune-Delivery-Id',
      value: t('reply_send_hook.contract.header_delivery_id'),
    },
    {
      name: 'X-Attune-Idempotency-Key',
      value: t('reply_send_hook.contract.header_idempotency'),
    },
    {
      name: 'User-Agent',
      value: 'attune/1.0',
    },
  ]
}

function replySendSamplePayload(hookId?: string) {
  return JSON.stringify(
    {
      version: '1',
      event_type: 'reply.send',
      tenant_id: 'tenant_demo',
      feedback_id: '12345',
      draft_id: '0190704a-9f15-7a35-9d2d-365b77f9b641',
      revision_id: '0190704b-4d92-7b2d-a7e8-5ed084736f8f',
      cycle_no: 1,
      revision_no: 3,
      text: 'Thanks for the clear report. We reproduced the issue and are preparing a fix.',
      idempotency_key: 'reply_send_01HY5ZP6HQ8G2J4S7DFM2PA9PK',
      sent_at: '2026-07-03T03:30:00Z',
      metadata: {
        hook_id: hookId || '01907049-f7d3-7ff0-a0b5-521090d44ec2',
        attempt_id: '0190704c-064a-75a4-8fd6-1b916614d7f9',
      },
    },
    null,
    2,
  )
}
