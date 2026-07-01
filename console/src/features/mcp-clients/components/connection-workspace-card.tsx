import { Check, CircleAlert, Copy, ExternalLink, PlugZap } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { MCPClient, MCPConnectionProfile } from '../api/types'

const claudeCallbackPort = 8080
const claudeCallbackURI = `http://localhost:${claudeCallbackPort}/callback`
const cursorRedirectURI = 'https://www.cursor.com/agents/mcp/oauth/callback'
const vscodeRedirectURIs = ['http://127.0.0.1:33418', 'https://vscode.dev/redirect']

type TemplateID = 'claude_cli' | 'claude_json' | 'cursor' | 'vscode' | 'curl'

type TemplateModel = {
  id: TemplateID
  docsUrl?: string
  label: string
  description: string
  requiredRedirects: string[]
  snippet: string
}

export function ConnectionWorkspaceCard({
  client,
  connection,
}: {
  client: MCPClient
  connection?: MCPConnectionProfile
}) {
  const { t } = useTranslation()
  const [selectedTemplateID, setSelectedTemplateID] = useState<TemplateID>('claude_cli')
  const [copiedKey, setCopiedKey] = useState<string | null>(null)

  const templates = useMemo(() => buildTemplates(client, connection, t), [client, connection, t])
  const selectedTemplate =
    templates.find((template) => template.id === selectedTemplateID) ?? templates[0]
  const clientRevoked = Boolean(client.revoked_at)
  const missingRedirects = selectedTemplate
    ? selectedTemplate.requiredRedirects.filter((uri) => !hasRedirectURI(client.redirect_uris, uri))
    : []

  if (!connection || !selectedTemplate) {
    return (
      <Card className="min-w-0">
        <CardHeader>
          <CardTitle>{t('mcp_clients.connection.title')}</CardTitle>
          <CardDescription>{t('mcp_clients.connection.description')}</CardDescription>
        </CardHeader>
        <CardContent>
          <Alert>
            <CircleAlert className="h-4 w-4" />
            <AlertTitle>{t('mcp_clients.connection.unavailable_title')}</AlertTitle>
            <AlertDescription>{t('mcp_clients.connection.unavailable_body')}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  const endpointItems = [
    {
      key: 'server_url',
      label: t('mcp_clients.connection.endpoints.server_url'),
      value: connection.server_url,
    },
    {
      key: 'resource_url',
      label: t('mcp_clients.connection.endpoints.resource_url'),
      value: connection.resource_url,
    },
    {
      key: 'oauth_issuer',
      label: t('mcp_clients.connection.endpoints.oauth_issuer'),
      value: connection.oauth_issuer,
    },
    {
      key: 'authorization_endpoint',
      label: t('mcp_clients.connection.endpoints.authorization_endpoint'),
      value: connection.authorization_endpoint,
    },
    {
      key: 'token_endpoint',
      label: t('mcp_clients.connection.endpoints.token_endpoint'),
      value: connection.token_endpoint,
    },
    {
      key: 'protected_resource_metadata_url',
      label: t('mcp_clients.connection.endpoints.protected_resource_metadata_url'),
      value: connection.protected_resource_metadata_url,
    },
    {
      key: 'authorization_server_metadata_url',
      label: t('mcp_clients.connection.endpoints.authorization_server_metadata_url'),
      value: connection.authorization_server_metadata_url,
    },
    {
      key: 'legacy_protected_resource_metadata_url',
      label: t('mcp_clients.connection.endpoints.legacy_protected_resource_metadata_url'),
      value: connection.legacy_protected_resource_metadata_url,
    },
  ]

  const handleCopy = async (key: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopiedKey(key)
      toast.success(t('common.copied'))
      window.setTimeout(() => {
        setCopiedKey((current) => (current === key ? null : current))
      }, 1500)
    } catch {
      toast.error(t('mcp_clients.connection.copy_failed'))
    }
  }

  const redirectStatusReady = !clientRevoked && missingRedirects.length === 0

  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <PlugZap className="h-4 w-4" />
          {t('mcp_clients.connection.title')}
        </CardTitle>
        <CardDescription>{t('mcp_clients.connection.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid grid-cols-1 gap-3 xl:grid-cols-2">
          {endpointItems.map((item) => (
            <EndpointRow
              key={item.key}
              label={item.label}
              value={item.value}
              copied={copiedKey === item.key}
              onCopy={() => handleCopy(item.key, item.value)}
            />
          ))}
        </div>

        <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,240px)_minmax(0,1fr)]">
          <div className="space-y-2">
            <Label htmlFor="mcp-connection-template">
              {t('mcp_clients.connection.template_label')}
            </Label>
            <Select
              value={selectedTemplate.id}
              onValueChange={(value) => setSelectedTemplateID(value as TemplateID)}
            >
              <SelectTrigger id="mcp-connection-template" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {templates.map((template) => (
                  <SelectItem key={template.id} value={template.id}>
                    {template.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs leading-5 text-muted-foreground">
              {selectedTemplate.description}
            </p>
            {selectedTemplate.docsUrl ? (
              <a
                className="inline-flex items-center gap-1 text-xs text-primary underline-offset-4 hover:underline"
                href={selectedTemplate.docsUrl}
                rel="noreferrer"
                target="_blank"
              >
                {t('mcp_clients.connection.docs_link')}
                <ExternalLink className="h-3 w-3" />
              </a>
            ) : null}
          </div>

          <div className="min-w-0 space-y-3">
            <Alert>
              <CircleAlert className="h-4 w-4" />
              <AlertTitle>{t('mcp_clients.connection.interactive_only_title')}</AlertTitle>
              <AlertDescription>
                {t('mcp_clients.connection.interactive_only_body')}
              </AlertDescription>
            </Alert>

            <Alert>
              {redirectStatusReady ? (
                <Check className="h-4 w-4" />
              ) : (
                <CircleAlert className="h-4 w-4" />
              )}
              <AlertTitle>
                {clientRevoked
                  ? t('mcp_clients.connection.revoked_title')
                  : redirectStatusReady
                    ? t('mcp_clients.connection.ready_title')
                    : t('mcp_clients.connection.action_required_title')}
              </AlertTitle>
              <AlertDescription>
                <p>
                  {clientRevoked
                    ? t('mcp_clients.connection.revoked_body')
                    : redirectStatusReady
                      ? t('mcp_clients.connection.ready_body')
                      : t('mcp_clients.connection.action_required_body')}
                </p>
                {selectedTemplate.requiredRedirects.length > 0 ? (
                  <div className="space-y-1">
                    <p>{t('mcp_clients.connection.required_redirects')}</p>
                    {selectedTemplate.requiredRedirects.map((uri) => (
                      <code
                        key={uri}
                        className="block break-all rounded-md border border-border bg-muted/40 px-2 py-1 font-mono text-xs"
                      >
                        {uri}
                      </code>
                    ))}
                  </div>
                ) : (
                  <p>{t('mcp_clients.connection.no_redirects_required')}</p>
                )}
                {missingRedirects.length > 0 ? (
                  <div className="space-y-1">
                    <p>{t('mcp_clients.connection.missing_redirects')}</p>
                    {missingRedirects.map((uri) => (
                      <code
                        key={uri}
                        className="block break-all rounded-md border border-border bg-muted/40 px-2 py-1 font-mono text-xs"
                      >
                        {uri}
                      </code>
                    ))}
                  </div>
                ) : null}
              </AlertDescription>
            </Alert>

            <div className="rounded-lg border border-border">
              <div className="flex items-center justify-between border-b border-border px-3 py-2">
                <div>
                  <div className="text-sm font-medium">{selectedTemplate.label}</div>
                  <div className="text-xs text-muted-foreground">
                    {t('mcp_clients.connection.snippet_hint')}
                  </div>
                </div>
                {clientRevoked ? null : (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="gap-1 text-xs"
                    onClick={() =>
                      handleCopy(`snippet:${selectedTemplate.id}`, selectedTemplate.snippet)
                    }
                  >
                    <Copy className="h-3 w-3" />
                    {copiedKey === `snippet:${selectedTemplate.id}`
                      ? t('common.copied')
                      : t('common.copy')}
                  </Button>
                )}
              </div>
              {clientRevoked ? (
                <div className="bg-muted/30 p-3 text-xs leading-5 text-muted-foreground">
                  {t('mcp_clients.connection.revoked_snippet_hint')}
                </div>
              ) : (
                <textarea
                  aria-label={t('mcp_clients.connection.snippet_hint')}
                  className="block w-full resize-none overflow-x-auto border-0 bg-muted/30 p-3 font-mono text-xs leading-5 outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                  readOnly
                  rows={Math.min(selectedTemplate.snippet.split('\n').length + 1, 18)}
                  spellCheck={false}
                  value={selectedTemplate.snippet}
                  wrap="off"
                />
              )}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function EndpointRow({
  label,
  value,
  copied,
  onCopy,
}: {
  label: string
  value: string
  copied: boolean
  onCopy: () => void
}) {
  const { t } = useTranslation()

  return (
    <div className="rounded-lg border border-border bg-muted/10 p-3">
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="text-xs font-medium text-muted-foreground">{label}</span>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 gap-1 text-xs"
          onClick={onCopy}
        >
          <Copy className="h-3 w-3" />
          {copied ? t('common.copied') : t('common.copy')}
        </Button>
      </div>
      <code className="block break-all font-mono text-xs leading-5">{value}</code>
    </div>
  )
}

function buildTemplates(
  client: MCPClient,
  connection: MCPConnectionProfile | undefined,
  t: (key: string) => string,
): TemplateModel[] {
  const serverName = normalizeServerName(client.name)
  const scopes = client.scopes

  return [
    {
      id: 'claude_cli',
      label: t('mcp_clients.connection.templates.claude_cli.label'),
      description: t('mcp_clients.connection.templates.claude_cli.description'),
      docsUrl: 'https://code.claude.com/docs/en/mcp',
      requiredRedirects: [claudeCallbackURI],
      snippet: buildClaudeCLICommand(
        serverName,
        client.id,
        connection?.server_url ?? '',
        claudeCallbackPort,
      ),
    },
    {
      id: 'claude_json',
      label: t('mcp_clients.connection.templates.claude_json.label'),
      description: t('mcp_clients.connection.templates.claude_json.description'),
      docsUrl: 'https://code.claude.com/docs/en/mcp',
      requiredRedirects: [claudeCallbackURI],
      snippet: prettyJSON({
        mcpServers: {
          [serverName]: {
            type: 'http',
            url: connection?.server_url ?? '',
            oauth: {
              clientId: client.id,
              callbackPort: claudeCallbackPort,
              scopes: scopes.join(' '),
            },
          },
        },
      }),
    },
    {
      id: 'cursor',
      label: t('mcp_clients.connection.templates.cursor.label'),
      description: t('mcp_clients.connection.templates.cursor.description'),
      docsUrl: 'https://cursor.com/docs/mcp',
      requiredRedirects: [cursorRedirectURI],
      snippet: prettyJSON({
        mcpServers: {
          [serverName]: {
            url: connection?.server_url ?? '',
            auth: {
              CLIENT_ID: client.id,
              scopes,
            },
          },
        },
      }),
    },
    {
      id: 'vscode',
      label: t('mcp_clients.connection.templates.vscode.label'),
      description: t('mcp_clients.connection.templates.vscode.description'),
      docsUrl: 'https://code.visualstudio.com/docs/agents/reference/mcp-configuration',
      requiredRedirects: vscodeRedirectURIs,
      snippet: prettyJSON({
        servers: {
          [serverName]: {
            type: 'http',
            url: connection?.server_url ?? '',
            oauth: {
              clientId: client.id,
            },
          },
        },
      }),
    },
    {
      id: 'curl',
      label: t('mcp_clients.connection.templates.curl.label'),
      description: t('mcp_clients.connection.templates.curl.description'),
      requiredRedirects: [],
      snippet: buildCurlSnippet(connection),
    },
  ]
}

function buildClaudeCLICommand(
  serverName: string,
  clientID: string,
  serverURL: string,
  callbackPort: number,
): string {
  return [
    'claude mcp add --transport http \\',
    `  --client-id ${clientID} --callback-port ${callbackPort} \\`,
    `  ${serverName} ${serverURL}`,
    `claude mcp login ${serverName}`,
  ].join('\n')
}

function buildCurlSnippet(connection: MCPConnectionProfile | undefined): string {
  const resourceURL = connection?.resource_url ?? ''
  const prmURL = connection?.protected_resource_metadata_url ?? ''
  const authURL = connection?.authorization_server_metadata_url ?? ''

  return [
    `curl -fsSL '${prmURL}'`,
    '',
    `curl -fsSL '${authURL}'`,
    '',
    `curl -i -X POST '${resourceURL}' \\`,
    "  -H 'Content-Type: application/json' \\",
    `  -d '{"jsonrpc":"2.0","id":"diag-1","method":"list_feedback"}'`,
  ].join('\n')
}

function hasRedirectURI(redirectURIs: string[], target: string): boolean {
  const normalizedTarget = target.trim()
  return redirectURIs.some((value) => value.trim() === normalizedTarget)
}

function normalizeServerName(value: string): string {
  const normalized = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, '-')
  return normalized || 'attune'
}

function prettyJSON(value: unknown): string {
  return JSON.stringify(value, null, 2)
}
