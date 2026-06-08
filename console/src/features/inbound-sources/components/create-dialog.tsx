import { CheckCircle2, Loader2, Mail, Webhook, XCircle } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { InboundSourceCreate } from '@/features/inbound-sources/api/create-inbound-source'
import {
  type TestInboundConnectionResult,
  useTestInboundSourceConnection,
} from '@/features/inbound-sources/api/test-connection'
import { cn } from '@/lib/utils'

type Channel = 'webhook' | 'email'

interface EmailFields {
  host: string
  port: number
  tls: boolean
  username: string
  password: string
  folder: string
}

const defaultEmail: EmailFields = {
  host: '',
  port: 993,
  tls: true,
  username: '',
  password: '',
  folder: 'INBOX',
}

// CreateInboundSourceDialog — two-channel wizard. The user picks
// webhook or email at the top; the form body swaps between two field
// sets. Email branch also exposes a "Test connection" button that
// validates IMAP creds without persisting.
export function CreateInboundSourceDialog({
  open,
  onOpenChange,
  onSubmit,
  pending,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onSubmit: (body: InboundSourceCreate) => Promise<unknown>
  pending: boolean
}) {
  const { t } = useTranslation()
  const test = useTestInboundSourceConnection()
  const [channel, setChannel] = useState<Channel>('webhook')
  const [name, setName] = useState('')
  const [email, setEmail] = useState<EmailFields>(defaultEmail)
  const [testResult, setTestResult] = useState<TestInboundConnectionResult | null>(null)

  const reset = () => {
    setChannel('webhook')
    setName('')
    setEmail(defaultEmail)
    setTestResult(null)
  }

  const buildBody = (): InboundSourceCreate => {
    if (channel === 'webhook') {
      return {
        channel: 'webhook',
        name: name.trim(),
        webhookConfig: {},
      }
    }
    return {
      channel: 'email',
      name: name.trim(),
      emailConfig: {
        host: email.host.trim(),
        port: email.port,
        tls: email.tls,
        username: email.username.trim(),
        password: email.password,
        folder: email.folder.trim() || 'INBOX',
        startFrom: 'now',
        afterIngest: 'mark_seen',
      },
    }
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return
    if (channel === 'email') {
      if (
        !email.host.trim() ||
        !email.username.trim() ||
        !email.password ||
        email.port < 1 ||
        email.port > 65535
      ) {
        return
      }
    }
    void onSubmit(buildBody()).then(() => reset())
  }

  const handleTest = () => {
    if (channel !== 'email') return
    test.mutate(
      {
        channel: 'email',
        emailConfig: {
          host: email.host.trim(),
          port: email.port,
          tls: email.tls,
          username: email.username.trim(),
          password: email.password,
          folder: email.folder.trim() || 'INBOX',
        },
      },
      {
        onSuccess: (res) => setTestResult(res),
        onError: () => setTestResult({ ok: false, error: t('common.error') }),
      },
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) reset()
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>{t('inbound_sources.create.title')}</DialogTitle>
          </DialogHeader>

          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>{t('inbound_sources.create.channel_label')}</Label>
              <div className="grid grid-cols-2 gap-2">
                <ChannelOption
                  selected={channel === 'webhook'}
                  onClick={() => setChannel('webhook')}
                  icon={<Webhook className="h-4 w-4" />}
                  label={t('inbound_sources.channel.webhook')}
                  help={t('inbound_sources.create.webhook_help')}
                />
                <ChannelOption
                  selected={channel === 'email'}
                  onClick={() => setChannel('email')}
                  icon={<Mail className="h-4 w-4" />}
                  label={t('inbound_sources.channel.email')}
                  help={t('inbound_sources.create.email_help')}
                />
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="is-name">{t('inbound_sources.create.name_field')}</Label>
              <Input
                id="is-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('inbound_sources.create.name_placeholder')}
                disabled={pending}
                required
                maxLength={200}
              />
              <p className="text-xs text-muted-foreground">
                {t('inbound_sources.create.name_help')}
              </p>
            </div>

            {channel === 'email' && (
              <EmailFieldset
                values={email}
                onChange={setEmail}
                pending={pending}
                onTest={handleTest}
                testing={test.isPending}
                testResult={testResult}
              />
            )}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={pending}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={pending || !name.trim()}>
              {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
              {t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ChannelOption({
  selected,
  onClick,
  icon,
  label,
  help,
}: {
  selected: boolean
  onClick: () => void
  icon: React.ReactNode
  label: string
  help: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'flex flex-col items-start gap-1 rounded-md border p-3 text-left text-sm transition-colors',
        selected
          ? 'border-primary bg-primary/5 ring-1 ring-primary/40'
          : 'border-border hover:bg-muted/40',
      )}
    >
      <span className="flex items-center gap-2 font-medium">
        {icon}
        {label}
      </span>
      <span className="text-xs text-muted-foreground">{help}</span>
    </button>
  )
}

function EmailFieldset({
  values,
  onChange,
  pending,
  onTest,
  testing,
  testResult,
}: {
  values: EmailFields
  onChange: (next: EmailFields) => void
  pending: boolean
  onTest: () => void
  testing: boolean
  testResult: TestInboundConnectionResult | null
}) {
  const { t } = useTranslation()
  const set = <K extends keyof EmailFields>(k: K, v: EmailFields[K]) =>
    onChange({ ...values, [k]: v })
  return (
    <div className="space-y-3 rounded-md border border-border p-3">
      <div className="grid grid-cols-3 gap-3">
        <div className="col-span-2 space-y-2">
          <Label htmlFor="is-host">{t('inbound_sources.create.email.host')}</Label>
          <Input
            id="is-host"
            value={values.host}
            onChange={(e) => set('host', e.target.value)}
            placeholder="imap.example.com"
            disabled={pending}
            required
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="is-port">{t('inbound_sources.create.email.port')}</Label>
          <Input
            id="is-port"
            type="number"
            min={1}
            max={65535}
            value={values.port}
            onChange={(e) => set('port', Number(e.target.value) || 993)}
            disabled={pending}
            required
          />
        </div>
      </div>

      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={values.tls}
          onChange={(e) => set('tls', e.target.checked)}
          disabled={pending}
        />
        {t('inbound_sources.create.email.tls')}
      </label>

      <div className="space-y-2">
        <Label htmlFor="is-username">{t('inbound_sources.create.email.username')}</Label>
        <Input
          id="is-username"
          value={values.username}
          onChange={(e) => set('username', e.target.value)}
          placeholder="feedback@example.com"
          disabled={pending}
          required
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="is-password">{t('inbound_sources.create.email.password')}</Label>
        <Input
          id="is-password"
          type="password"
          value={values.password}
          onChange={(e) => set('password', e.target.value)}
          disabled={pending}
          required
        />
        <p className="text-xs text-muted-foreground">
          {t('inbound_sources.create.email.password_help')}
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-2">
          <Label htmlFor="is-folder">{t('inbound_sources.create.email.folder')}</Label>
          <Input
            id="is-folder"
            value={values.folder}
            onChange={(e) => set('folder', e.target.value)}
            disabled={pending}
            placeholder="INBOX"
          />
        </div>
      </div>

      <div className="flex items-center gap-3">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={onTest}
          disabled={testing || pending || !values.host || !values.username || !values.password}
        >
          {testing ? <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> : null}
          {t('inbound_sources.create.email.test_button')}
        </Button>
        {testResult && (
          <span
            className={cn(
              'inline-flex items-center gap-1 text-xs',
              testResult.ok ? 'text-green-700 dark:text-green-500' : 'text-destructive',
            )}
          >
            {testResult.ok ? (
              <>
                <CheckCircle2 className="h-3.5 w-3.5" />
                {t('inbound_sources.create.email.test_ok', { ms: testResult.latencyMs ?? '?' })}
              </>
            ) : (
              <>
                <XCircle className="h-3.5 w-3.5" />
                {testResult.error || t('common.error')}
              </>
            )}
          </span>
        )}
      </div>
    </div>
  )
}
