import {
  CheckCircle2,
  Headphones,
  Loader2,
  Mail,
  MessageSquare,
  Webhook,
  XCircle,
} from 'lucide-react'
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

type Channel = 'webhook' | 'email' | 'slack' | 'zendesk'

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

interface ZendeskFields {
  subdomain: string
  authMode: 'api_token' | 'oauth'
  email: string
  apiToken: string
  oauthAccessToken: string
  oauthRefreshToken: string
  oauthClientId: string
  oauthClientSecret: string
  startFrom: string
  filterTags: string
  filterExcludeTags: string
  filterStatuses: string[]
  maxCommentFetches: number
}

const defaultZendesk: ZendeskFields = {
  subdomain: '',
  authMode: 'api_token',
  email: '',
  apiToken: '',
  oauthAccessToken: '',
  oauthRefreshToken: '',
  oauthClientId: '',
  oauthClientSecret: '',
  startFrom: 'now',
  filterTags: '',
  filterExcludeTags: '',
  filterStatuses: ['open', 'pending', 'solved', 'closed'],
  maxCommentFetches: 50,
}

// CreateInboundSourceDialog — four-channel wizard. The user picks
// webhook, email, slack, or zendesk at the top; the form body swaps
// between the channel-specific field sets. Email, Slack, and Zendesk
// branches expose a "Test connection" action, and Slack also supports
// channel discovery before create.
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
  const [zd, setZd] = useState<ZendeskFields>(defaultZendesk)
  const [testResult, setTestResult] = useState<TestInboundConnectionResult | null>(null)

  const reset = () => {
    setChannel('webhook')
    setName('')
    setEmail(defaultEmail)
    setSlack(defaultSlack)
    setZd(defaultZendesk)
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
    if (channel === 'zendesk') {
      return {
        channel: 'zendesk',
        name: name.trim(),
        zendeskConfig: {
          subdomain: zd.subdomain.trim().toLowerCase(),
          authMode: zd.authMode,
          email: zd.email.trim(),
          apiToken: zd.apiToken.trim(),
          oauthAccessToken: zd.oauthAccessToken.trim(),
          oauthRefreshToken: zd.oauthRefreshToken.trim(),
          oauthClientIdV2: zd.oauthClientId.trim(),
          oauthClientSecretV2: zd.oauthClientSecret.trim(),
          startFrom: zd.startFrom,
          filterTags: zd.filterTags
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean),
          filterExcludeTags: zd.filterExcludeTags
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean),
          filterStatuses: zd.filterStatuses,
          maxCommentFetches: zd.maxCommentFetches,
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

  const isFormComplete = (): boolean => {
    if (!name.trim()) return false
    if (channel === 'email') {
      return !!(
        email.host.trim() &&
        email.username.trim() &&
        email.password &&
        email.port >= 1 &&
        email.port <= 65535
      )
    }
    if (channel === 'slack') {
      return !!(slack.botToken.trim() && slack.channelId.trim())
    }
    if (channel === 'zendesk') {
      if (!zd.subdomain.trim()) return false
      if (zd.authMode === 'api_token') return !!(zd.email.trim() && zd.apiToken.trim())
      if (zd.authMode === 'oauth') return !!zd.oauthAccessToken.trim()
      return false
    }
    return true // webhook: name only
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!isFormComplete()) return
    void onSubmit(buildBody())
      .then(() => reset())
      .catch(() => {
        // The parent mutation owns user-facing error handling; keep the
        // dialog open with the operator's input intact.
      })
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
    if (channel === 'zendesk') {
      test.mutate(
        {
          channel: 'zendesk',
          zendeskConfig: {
            subdomain: zd.subdomain.trim().toLowerCase(),
            authMode: zd.authMode,
            email: zd.email.trim(),
            apiToken: zd.apiToken.trim(),
            oauthAccessToken: zd.oauthAccessToken.trim(),
            oauthRefreshToken: zd.oauthRefreshToken.trim(),
            oauthClientIdV2: zd.oauthClientId.trim(),
            oauthClientSecretV2: zd.oauthClientSecret.trim(),
            filterTags: [] as string[],
            filterExcludeTags: [] as string[],
            filterStatuses: [] as string[],
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
    /* v8 ignore next -- @preserve: discovery is disabled until a Slack bot token is entered. */
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
              <div
                className="grid grid-cols-2 gap-2 sm:grid-cols-4"
                role="radiogroup"
                aria-label={t('inbound_sources.create.channel_label')}
              >
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
                <ChannelOption
                  selected={channel === 'zendesk'}
                  onClick={() => {
                    setChannel('zendesk')
                    setTestResult(null)
                  }}
                  icon={<Headphones className="h-4 w-4" />}
                  label={t('inbound_sources.channel.zendesk')}
                  help={t('inbound_sources.create.zendesk_help')}
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

            {channel === 'zendesk' && (
              <ZendeskFieldset
                values={zd}
                onChange={(next) => {
                  setZd(next)
                  setTestResult(null)
                }}
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
            <Button
              type="submit"
              disabled={pending || !isFormComplete()}
              title={!isFormComplete() ? t('inbound_sources.create.fill_required') : undefined}
            >
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
    // biome-ignore lint/a11y/useSemanticElements: styled card radio — native input would lose the card layout
    <button
      type="button"
      role="radio"
      aria-checked={selected}
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

function ZendeskFieldset({
  values,
  onChange,
  pending,
  onTest,
  testing,
  testResult,
}: {
  values: ZendeskFields
  onChange: (next: ZendeskFields) => void
  pending: boolean
  onTest: () => void
  testing: boolean
  testResult: TestInboundConnectionResult | null
}) {
  const { t } = useTranslation()
  const set = <K extends keyof ZendeskFields>(k: K, v: ZendeskFields[K]) =>
    onChange({ ...values, [k]: v })
  return (
    <div className="space-y-3 rounded-md border border-border p-3">
      <div className="space-y-2">
        <Label htmlFor="is-zd-subdomain">{t('inbound_sources.create.zendesk.subdomain')}</Label>
        <Input
          id="is-zd-subdomain"
          aria-describedby="is-zd-subdomain-help"
          value={values.subdomain}
          onChange={(e) => set('subdomain', e.target.value)}
          placeholder={t('inbound_sources.create.zendesk.subdomain_placeholder')}
          disabled={pending}
          required
        />
        <p id="is-zd-subdomain-help" className="text-xs text-muted-foreground">
          {t('inbound_sources.create.zendesk.subdomain_help')}
        </p>
      </div>

      <div className="space-y-2">
        <Label>{t('inbound_sources.create.zendesk.auth_mode')}</Label>
        <div
          className="grid grid-cols-2 gap-2"
          role="radiogroup"
          aria-label={t('inbound_sources.create.zendesk.auth_mode')}
        >
          {/* biome-ignore lint/a11y/useSemanticElements: styled card radio */}
          <button
            type="button"
            role="radio"
            aria-checked={values.authMode === 'api_token'}
            onClick={() => set('authMode', 'api_token')}
            className={cn(
              'rounded-md border p-2 text-left text-sm transition-colors',
              values.authMode === 'api_token'
                ? 'border-primary bg-primary/5 ring-1 ring-primary/40'
                : 'border-border hover:bg-muted/40',
            )}
          >
            {t('inbound_sources.create.zendesk.auth_api_token')}
          </button>
          {/* biome-ignore lint/a11y/useSemanticElements: styled card radio */}
          <button
            type="button"
            role="radio"
            aria-checked={values.authMode === 'oauth'}
            onClick={() => set('authMode', 'oauth')}
            className={cn(
              'rounded-md border p-2 text-left text-sm transition-colors',
              values.authMode === 'oauth'
                ? 'border-primary bg-primary/5 ring-1 ring-primary/40'
                : 'border-border hover:bg-muted/40',
            )}
          >
            {t('inbound_sources.create.zendesk.auth_oauth')}
          </button>
        </div>
      </div>

      {values.authMode === 'api_token' && (
        <>
          <div className="space-y-2">
            <Label htmlFor="is-zd-email">{t('inbound_sources.create.zendesk.email')}</Label>
            <Input
              id="is-zd-email"
              type="email"
              value={values.email}
              onChange={(e) => set('email', e.target.value)}
              placeholder="admin@example.com"
              disabled={pending}
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="is-zd-token">{t('inbound_sources.create.zendesk.api_token')}</Label>
            <Input
              id="is-zd-token"
              aria-describedby="is-zd-token-help"
              type="password"
              autoComplete="off"
              value={values.apiToken}
              onChange={(e) => set('apiToken', e.target.value)}
              disabled={pending}
              required
            />
            <p id="is-zd-token-help" className="text-xs text-muted-foreground">
              {t('inbound_sources.create.zendesk.api_token_help')}{' '}
              <a
                href="https://support.zendesk.com/hc/en-us/articles/4408889192858"
                target="_blank"
                rel="noopener noreferrer"
                className="underline hover:text-foreground"
              >
                {t('inbound_sources.create.zendesk.api_token_create_link')}
              </a>
            </p>
          </div>
        </>
      )}

      {values.authMode === 'oauth' && (
        <>
          <div className="space-y-2">
            <Label htmlFor="is-zd-access-token">
              {t('inbound_sources.create.zendesk.oauth_access_token')}
            </Label>
            <Input
              id="is-zd-access-token"
              type="password"
              autoComplete="off"
              value={values.oauthAccessToken}
              onChange={(e) => set('oauthAccessToken', e.target.value)}
              disabled={pending}
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="is-zd-refresh-token">
              {t('inbound_sources.create.zendesk.oauth_refresh_token')}
            </Label>
            <Input
              id="is-zd-refresh-token"
              type="password"
              autoComplete="off"
              value={values.oauthRefreshToken}
              onChange={(e) => set('oauthRefreshToken', e.target.value)}
              disabled={pending}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="is-zd-client-id">
              {t('inbound_sources.create.zendesk.oauth_client_id')}
            </Label>
            <Input
              id="is-zd-client-id"
              value={values.oauthClientId}
              onChange={(e) => set('oauthClientId', e.target.value)}
              disabled={pending}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="is-zd-client-secret">
              {t('inbound_sources.create.zendesk.oauth_client_secret')}
            </Label>
            <Input
              id="is-zd-client-secret"
              type="password"
              autoComplete="off"
              value={values.oauthClientSecret}
              onChange={(e) => set('oauthClientSecret', e.target.value)}
              disabled={pending}
            />
            <p className="text-xs text-muted-foreground">
              {t('inbound_sources.create.zendesk.oauth_paste_help')}
            </p>
          </div>
        </>
      )}

      <div className="space-y-2">
        <Label htmlFor="is-zd-start-from">{t('inbound_sources.create.zendesk.start_from')}</Label>
        <Select value={values.startFrom} onValueChange={(v) => set('startFrom', v)}>
          <SelectTrigger id="is-zd-start-from" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="now">
              {t('inbound_sources.create.zendesk.start_from_now')}
            </SelectItem>
            <SelectItem value="full">
              {t('inbound_sources.create.zendesk.start_from_full')}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <details className="space-y-3">
        <summary className="cursor-pointer text-sm font-medium text-muted-foreground hover:text-foreground">
          {t('inbound_sources.create.zendesk.advanced_label')}
        </summary>
        <div className="space-y-3 pt-2">
          <div className="space-y-2">
            <Label htmlFor="is-zd-filter-tags">
              {t('inbound_sources.create.zendesk.filter_tags')}
            </Label>
            <Input
              id="is-zd-filter-tags"
              value={values.filterTags}
              onChange={(e) => set('filterTags', e.target.value)}
              disabled={pending}
              placeholder="feature-request, billing"
            />
            <p className="text-xs text-muted-foreground">
              {t('inbound_sources.create.zendesk.filter_tags_help')}
            </p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="is-zd-exclude-tags">
              {t('inbound_sources.create.zendesk.filter_exclude_tags')}
            </Label>
            <Input
              id="is-zd-exclude-tags"
              value={values.filterExcludeTags}
              onChange={(e) => set('filterExcludeTags', e.target.value)}
              disabled={pending}
              placeholder="spam, test"
            />
            <p className="text-xs text-muted-foreground">
              {t('inbound_sources.create.zendesk.filter_exclude_tags_help')}
            </p>
          </div>
          <div className="space-y-2">
            <Label>{t('inbound_sources.create.zendesk.filter_statuses')}</Label>
            <div className="flex flex-wrap gap-3">
              {['open', 'pending', 'solved', 'closed'].map((s) => (
                <label key={s} className="flex items-center gap-1.5 text-sm">
                  <input
                    type="checkbox"
                    checked={values.filterStatuses.includes(s)}
                    onChange={(e) => {
                      const next = e.target.checked
                        ? [...values.filterStatuses, s]
                        : values.filterStatuses.filter((v) => v !== s)
                      set('filterStatuses', next)
                    }}
                    disabled={pending}
                  />
                  {s}
                </label>
              ))}
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="is-zd-comment-budget">
              {t('inbound_sources.create.zendesk.max_comment_fetches')}
            </Label>
            <Input
              id="is-zd-comment-budget"
              type="number"
              min={1}
              max={200}
              value={values.maxCommentFetches}
              onChange={(e) => set('maxCommentFetches', Number(e.target.value) || 50)}
              disabled={pending}
            />
            <p className="text-xs text-muted-foreground">
              {t('inbound_sources.create.zendesk.max_comment_fetches_help')}
            </p>
          </div>
        </div>
      </details>

      <div className="flex items-center gap-3">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={onTest}
          disabled={
            testing ||
            pending ||
            !values.subdomain.trim() ||
            (values.authMode === 'api_token' &&
              (!values.email.trim() || !values.apiToken.trim())) ||
            (values.authMode === 'oauth' && !values.oauthAccessToken.trim())
          }
        >
          {testing ? <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> : null}
          {t('inbound_sources.create.zendesk.test_button')}
        </Button>
        {testResult && (
          <span
            role="alert"
            className={cn(
              'inline-flex items-center gap-1 text-xs',
              testResult.ok ? 'text-green-700 dark:text-green-500' : 'text-destructive',
            )}
          >
            {testResult.ok ? (
              <>
                <CheckCircle2 className="h-3.5 w-3.5" />
                {t('inbound_sources.create.zendesk.test_ok', { ms: testResult.latencyMs ?? '?' })}
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
