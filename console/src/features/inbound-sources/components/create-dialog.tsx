import { CheckCircle2, Loader2, Mail, MessageSquare, Webhook, XCircle } from 'lucide-react'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { InboundSourceCreate } from '@/features/inbound-sources/api/create-inbound-source'
import {
  type DiscoverSlackChannelsResult,
  useDiscoverSlackChannels,
} from '@/features/inbound-sources/api/discover-slack-channels'
import {
  type TestInboundConnectionResult,
  useTestInboundSourceConnection,
} from '@/features/inbound-sources/api/test-connection'
import { cn } from '@/lib/utils'
import type { SlackChannel } from '@/proto/attune/v1/inbound_source'

type Channel = 'webhook' | 'email' | 'slack'

interface EmailFields {
  host: string
  port: number
  tls: boolean
  username: string
  password: string
  folder: string
}

interface SlackFields {
  botToken: string
  channelId: string
}

const defaultEmail: EmailFields = {
  host: '',
  port: 993,
  tls: true,
  username: '',
  password: '',
  folder: 'INBOX',
}

const defaultSlack: SlackFields = {
  botToken: '',
  channelId: '',
}

// CreateInboundSourceDialog — three-channel wizard. The user picks
// webhook, email, or slack at the top; the form body swaps between
// the channel-specific field sets. Email and Slack branches both expose
// a "Test connection" action, and Slack also supports channel discovery
// before create.
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
  const discover = useDiscoverSlackChannels()
  const [channel, setChannel] = useState<Channel>('webhook')
  const [name, setName] = useState('')
  const [email, setEmail] = useState<EmailFields>(defaultEmail)
  const [slack, setSlack] = useState<SlackFields>(defaultSlack)
  const [slackChannels, setSlackChannels] = useState<SlackChannel[]>([])
  const [slackDiscoverNote, setSlackDiscoverNote] = useState<string | null>(null)
  const [testResult, setTestResult] = useState<TestInboundConnectionResult | null>(null)

  const reset = () => {
    setChannel('webhook')
    setName('')
    setEmail(defaultEmail)
    setSlack(defaultSlack)
    setSlackChannels([])
    setSlackDiscoverNote(null)
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
    if (channel === 'slack') {
      return {
        channel: 'slack',
        name: name.trim(),
        slackConfig: {
          botToken: slack.botToken.trim(),
          channelId: slack.channelId.trim(),
        },
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
    if (channel === 'slack' && (!slack.botToken.trim() || !slack.channelId.trim())) {
      return
    }
    void onSubmit(buildBody()).then(() => reset())
  }

  const handleTest = () => {
    if (channel === 'email') {
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
          onError: (err) =>
            setTestResult({
              ok: false,
              error: err instanceof Error ? err.message : t('common.error'),
            }),
        },
      )
      return
    }
    if (channel === 'slack') {
      test.mutate(
        {
          channel: 'slack',
          slackConfig: {
            botToken: slack.botToken.trim(),
            channelId: slack.channelId.trim(),
          },
        },
        {
          onSuccess: (res) => setTestResult(res),
          onError: (err) =>
            setTestResult({
              ok: false,
              error: err instanceof Error ? err.message : t('common.error'),
            }),
        },
      )
    }
  }

  const handleDiscoverSlack = () => {
    if (!slack.botToken.trim()) return
    discover.mutate(
      {
        slackConfig: {
          botToken: slack.botToken.trim(),
          channelId: slack.channelId.trim(),
        },
      },
      {
        onSuccess: (res: DiscoverSlackChannelsResult) => {
          const channels = res.channels ?? []
          setSlackChannels(channels)
          if (!channels.length) {
            setSlackDiscoverNote(t('inbound_sources.create.slack.discover_empty'))
            setSlack((prev) => ({ ...prev, channelId: '' }))
            return
          }
          setSlackDiscoverNote(
            t('inbound_sources.create.slack.discover_ok', { count: channels.length }),
          )
          setSlack((prev) => {
            if (prev.channelId && channels.some((channel) => channel.id === prev.channelId)) {
              return prev
            }
            return { ...prev, channelId: channels[0]?.id ?? '' }
          })
        },
        onError: (err) => {
          const message = err instanceof Error ? err.message : t('common.error')
          setSlackDiscoverNote(message)
        },
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
              <div className="grid grid-cols-3 gap-2">
                <ChannelOption
                  selected={channel === 'webhook'}
                  onClick={() => {
                    setChannel('webhook')
                    setTestResult(null)
                    setSlackDiscoverNote(null)
                  }}
                  icon={<Webhook className="h-4 w-4" />}
                  label={t('inbound_sources.channel.webhook')}
                  help={t('inbound_sources.create.webhook_help')}
                />
                <ChannelOption
                  selected={channel === 'email'}
                  onClick={() => {
                    setChannel('email')
                    setTestResult(null)
                    setSlackDiscoverNote(null)
                  }}
                  icon={<Mail className="h-4 w-4" />}
                  label={t('inbound_sources.channel.email')}
                  help={t('inbound_sources.create.email_help')}
                />
                <ChannelOption
                  selected={channel === 'slack'}
                  onClick={() => {
                    setChannel('slack')
                    setTestResult(null)
                  }}
                  icon={<MessageSquare className="h-4 w-4" />}
                  label={t('inbound_sources.channel.slack')}
                  help={t('inbound_sources.create.slack_help')}
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

            {channel === 'slack' && (
              <SlackFieldset
                values={slack}
                channels={slackChannels}
                discoverNote={slackDiscoverNote}
                onChange={setSlack}
                pending={pending}
                onDiscover={handleDiscoverSlack}
                discovering={discover.isPending}
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

function SlackFieldset({
  values,
  channels,
  discoverNote,
  onChange,
  pending,
  onDiscover,
  discovering,
  onTest,
  testing,
  testResult,
}: {
  values: SlackFields
  channels: SlackChannel[]
  discoverNote: string | null
  onChange: (next: SlackFields) => void
  pending: boolean
  onDiscover: () => void
  discovering: boolean
  onTest: () => void
  testing: boolean
  testResult: TestInboundConnectionResult | null
}) {
  const { t } = useTranslation()
  const set = <K extends keyof SlackFields>(k: K, v: SlackFields[K]) =>
    onChange({ ...values, [k]: v })
  return (
    <div className="space-y-3 rounded-md border border-border p-3">
      <div className="space-y-2">
        <Label htmlFor="is-slack-token">{t('inbound_sources.create.slack.token')}</Label>
        <Input
          id="is-slack-token"
          type="password"
          value={values.botToken}
          onChange={(e) => set('botToken', e.target.value)}
          disabled={pending}
          placeholder="xoxb-..."
          required
        />
        <p className="text-xs text-muted-foreground">
          {t('inbound_sources.create.slack.token_help')}
        </p>
      </div>

      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
        <div className="space-y-2">
          <Label htmlFor="is-slack-channel">{t('inbound_sources.create.slack.channel')}</Label>
          <Select
            value={values.channelId}
            onValueChange={(next) => set('channelId', next)}
            disabled={pending || channels.length === 0}
          >
            <SelectTrigger id="is-slack-channel" className="w-full">
              <SelectValue placeholder={t('inbound_sources.create.slack.channel_placeholder')} />
            </SelectTrigger>
            <SelectContent>
              {channels.map((channel) => (
                <SelectItem key={channel.id} value={channel.id}>
                  {formatSlackChannelLabel(channel, t)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            {channels.length
              ? t('inbound_sources.create.slack.channel_help', { count: channels.length })
              : t('inbound_sources.create.slack.channel_empty_help')}
          </p>
        </div>
        <div className="flex items-end">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onDiscover}
            disabled={discovering || pending || !values.botToken.trim()}
          >
            {discovering ? <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> : null}
            {t('inbound_sources.create.slack.discover_button')}
          </Button>
        </div>
      </div>

      {discoverNote && (
        <div className="text-xs text-muted-foreground" aria-live="polite">
          {discoverNote}
        </div>
      )}

      <div className="flex items-center gap-3">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={onTest}
          disabled={testing || pending || !values.botToken.trim()}
        >
          {testing ? <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> : null}
          {t('inbound_sources.create.slack.test_button')}
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
                {t('inbound_sources.create.slack.test_ok', { ms: testResult.latencyMs ?? '?' })}
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

function formatSlackChannelLabel(
  channel: SlackChannel,
  t: (key: string, options?: Record<string, unknown>) => string,
) {
  const suffix: string[] = []
  if (channel.isPrivate) {
    suffix.push(t('inbound_sources.create.slack.private_channel'))
  }
  if (channel.isShared) {
    suffix.push(t('inbound_sources.create.slack.shared_channel'))
  }
  if (!suffix.length) {
    return `#${channel.name}`
  }
  return `#${channel.name} · ${suffix.join(' · ')}`
}
