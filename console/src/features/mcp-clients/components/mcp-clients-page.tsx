import { useMutation, useQuery } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Bot, Loader2, Shield, Trash2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
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
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useCreateMCPClient } from '@/features/mcp-clients/api/create-mcp-client'
import { mcpClientDetailQuery } from '@/features/mcp-clients/api/get-mcp-client'
import { type MCPClient, mcpClientsQuery } from '@/features/mcp-clients/api/list-mcp-clients'
import { useRevokeMCPClient } from '@/features/mcp-clients/api/revoke-mcp-client'
import { useRevokeMCPGrant } from '@/features/mcp-clients/api/revoke-mcp-grant'
import { useRevokeMCPSession } from '@/features/mcp-clients/api/revoke-mcp-session'
import { useDocumentTitle } from '@/hooks/use-document-title'
import type {
  CreateMCPClientRequest,
  MCPClientTool,
  MCPRefreshGrant,
  MCPSession,
} from '../api/types'
import { useUpdateMCPClient } from '../api/update-mcp-client'
import { useUpdateMCPToolPolicies } from '../api/update-mcp-tool-policies'
import { ConnectionWorkspaceCard } from './connection-workspace-card'
import { CreateMCPClientDialog, RevokeMCPClientDialog } from './dialogs'

type ToolDraft = {
  effect: '' | 'allow' | 'deny'
  rateLimitRPM: string
  rateLimitBurst: string
}

export function MCPClientsPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('nav.mcp_clients'))
  const list = useQuery(mcpClientsQuery())
  const create = useCreateMCPClient()
  const revoke = useRevokeMCPClient()
  const updateClient = useUpdateMCPClient()
  const updateToolPolicies = useUpdateMCPToolPolicies()
  const revokeSession = useRevokeMCPSession()
  const revokeGrant = useRevokeMCPGrant()

  const [createOpen, setCreateOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<MCPClient | null>(null)
  const [selectedClientId, setSelectedClientId] = useState<string | null>(null)
  const [toolPolicyMode, setToolPolicyMode] = useState<'legacy_allow_all' | 'allow_list'>(
    'legacy_allow_all',
  )
  const [clientRateLimitRPM, setClientRateLimitRPM] = useState('')
  const [clientRateLimitBurst, setClientRateLimitBurst] = useState('')
  const [toolDrafts, setToolDrafts] = useState<Record<string, ToolDraft>>({})
  const revokeFocusRef = useRef<HTMLElement | null>(null)

  const detail = useQuery(mcpClientDetailQuery(selectedClientId))

  useEffect(() => {
    if (!list.data || list.data.length === 0) {
      setSelectedClientId(null)
      return
    }
    if (!selectedClientId || !list.data.some((client) => client.id === selectedClientId)) {
      const preferred = list.data.find((client) => !client.revoked_at) ?? list.data[0]
      setSelectedClientId(preferred.id)
    }
  }, [list.data, selectedClientId])

  useEffect(() => {
    if (!detail.data) {
      return
    }
    setToolPolicyMode(detail.data.client.tool_policy_mode)
    setClientRateLimitRPM(detail.data.client.rate_limit_rpm?.toString() ?? '')
    setClientRateLimitBurst(detail.data.client.rate_limit_burst?.toString() ?? '')

    const nextDrafts: Record<string, ToolDraft> = {}
    for (const tool of detail.data.tools) {
      nextDrafts[tool.name] = {
        effect: tool.effect,
        rateLimitRPM: tool.rate_limit_rpm?.toString() ?? '',
        rateLimitBurst: tool.rate_limit_burst?.toString() ?? '',
      }
    }
    setToolDrafts(nextDrafts)
  }, [detail.data])

  const handleCreate = (params: CreateMCPClientRequest) => {
    return create.mutateAsync(params, {
      onSuccess: (client) => {
        setCreateOpen(false)
        setSelectedClientId(client.id)
        toast.success(t('mcp_clients.created'))
      },
      onError: (err) => {
        toast.error(err instanceof Error ? err.message : 'failed')
      },
    })
  }

  const handleRevoke = useMutation({
    mutationFn: (id: string) => revoke.mutateAsync(id),
    onSuccess: () => {
      setRevokeTarget(null)
      toast.success(t('mcp_clients.revoked'))
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
  })

  const handleSaveGovernance = async () => {
    if (!selectedClientId) return
    try {
      await updateClient.mutateAsync({
        id: selectedClientId,
        body: {
          tool_policy_mode: toolPolicyMode,
          rate_limit_rpm: parseNullableInt(clientRateLimitRPM),
          rate_limit_burst: parseNullableInt(clientRateLimitBurst),
        },
      })
      toast.success(t('mcp_clients.governance.saved'))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'failed')
    }
  }

  const handleSaveToolPolicies = async () => {
    if (!selectedClientId || !detail.data) return

    const policies = detail.data.tools
      .map((tool) => {
        const draft = toolDrafts[tool.name]
        if (!draft || draft.effect === '' || !tool.scope_granted) {
          return null
        }
        return {
          tool_name: tool.name,
          effect: draft.effect,
          rate_limit_rpm: parseNullableInt(draft.rateLimitRPM),
          rate_limit_burst: parseNullableInt(draft.rateLimitBurst),
        }
      })
      .filter((policy): policy is NonNullable<typeof policy> => policy !== null)

    try {
      await updateToolPolicies.mutateAsync({
        id: selectedClientId,
        body: { policies },
      })
      toast.success(t('mcp_clients.tools.saved'))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'failed')
    }
  }

  const handleRevokeSession = async (sessionId: string) => {
    if (!selectedClientId) return
    try {
      await revokeSession.mutateAsync({ clientId: selectedClientId, sessionId })
      toast.success(t('mcp_clients.sessions.revoked'))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'failed')
    }
  }

  const handleRevokeGrant = async (grantId: string) => {
    if (!selectedClientId) return
    try {
      await revokeGrant.mutateAsync({ clientId: selectedClientId, grantId })
      toast.success(t('mcp_clients.grants.revoked'))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'failed')
    }
  }

  const activeClients = list.data?.filter((client) => !client.revoked_at) ?? []
  const selectedClient = list.data?.find((client) => client.id === selectedClientId) ?? null

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('mcp_clients.title')}</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            {t('mcp_clients.subtitle')}
          </p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>{t('mcp_clients.create_button')}</Button>
      </div>

      <div className="grid grid-cols-1 gap-6 2xl:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
        <Card className="min-w-0">
          <CardHeader>
            <CardTitle>{t('mcp_clients.title')}</CardTitle>
            <CardDescription>
              {activeClients.length} {t('mcp_clients.active_count')}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {list.isPending ? (
              <Loading />
            ) : list.data && list.data.length > 0 ? (
              <ClientTable
                clients={list.data}
                selectedClientId={selectedClientId}
                onSelect={setSelectedClientId}
                onRevoke={(client, restoreFocusTo) => {
                  revokeFocusRef.current = restoreFocusTo
                  setRevokeTarget(client)
                }}
                revokingId={handleRevoke.isPending ? revokeTarget?.id : undefined}
              />
            ) : (
              <EmptyState
                icon={Bot}
                title={t('mcp_clients.empty_title')}
                description={t('mcp_clients.empty_body')}
                action={{
                  label: t('mcp_clients.create_button'),
                  onClick: () => setCreateOpen(true),
                }}
              />
            )}
          </CardContent>
        </Card>

        <div className="min-w-0 space-y-6">
          {!selectedClient ? (
            <Card className="min-w-0">
              <CardContent className="flex min-h-56 items-center justify-center text-center text-sm text-muted-foreground">
                {t('mcp_clients.detail.empty')}
              </CardContent>
            </Card>
          ) : detail.isPending ? (
            <Card className="min-w-0">
              <CardContent className="flex min-h-56 items-center justify-center text-muted-foreground">
                <Loading className="py-0" />
              </CardContent>
            </Card>
          ) : detail.data ? (
            <>
              <ClientSummaryCard client={detail.data.client} />

              <ConnectionWorkspaceCard
                key={detail.data.client.id}
                client={detail.data.client}
                connection={detail.data.connection}
              />

              <GovernanceCard
                revoked={Boolean(detail.data.client.revoked_at)}
                mode={toolPolicyMode}
                rateLimitRPM={clientRateLimitRPM}
                rateLimitBurst={clientRateLimitBurst}
                pending={updateClient.isPending}
                onModeChange={(value) =>
                  setToolPolicyMode(value as 'legacy_allow_all' | 'allow_list')
                }
                onRateLimitRPMChange={setClientRateLimitRPM}
                onRateLimitBurstChange={setClientRateLimitBurst}
                onSave={handleSaveGovernance}
              />

              <ToolPoliciesCard
                revoked={Boolean(detail.data.client.revoked_at)}
                mode={toolPolicyMode}
                tools={detail.data.tools}
                drafts={toolDrafts}
                pending={updateToolPolicies.isPending}
                onToggle={(tool) => {
                  setToolDrafts((current) => {
                    const existing = current[tool.name] ?? emptyDraft()
                    const enabledEffect = toolPolicyMode === 'allow_list' ? 'allow' : 'deny'
                    const nextEffect = existing.effect === enabledEffect ? '' : enabledEffect
                    return {
                      ...current,
                      [tool.name]: { ...existing, effect: nextEffect },
                    }
                  })
                }}
                onRPMChange={(tool, value) => {
                  setToolDrafts((current) => ({
                    ...current,
                    [tool.name]: { ...(current[tool.name] ?? emptyDraft()), rateLimitRPM: value },
                  }))
                }}
                onBurstChange={(tool, value) => {
                  setToolDrafts((current) => ({
                    ...current,
                    [tool.name]: { ...(current[tool.name] ?? emptyDraft()), rateLimitBurst: value },
                  }))
                }}
                onSave={handleSaveToolPolicies}
              />

              <SessionsCard
                sessions={detail.data.sessions}
                pendingSessionId={
                  revokeSession.isPending ? revokeSession.variables?.sessionId : undefined
                }
                onRevoke={handleRevokeSession}
              />

              <RefreshGrantsCard
                grants={detail.data.refresh_grants}
                pendingGrantId={revokeGrant.isPending ? revokeGrant.variables?.grantId : undefined}
                onRevoke={handleRevokeGrant}
              />
            </>
          ) : null}
        </div>
      </div>

      <CreateMCPClientDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
        pending={create.isPending}
      />
      <RevokeMCPClientDialog
        target={revokeTarget}
        onCancel={() => setRevokeTarget(null)}
        onConfirm={() => revokeTarget && handleRevoke.mutate(revokeTarget.id)}
        pending={handleRevoke.isPending}
        restoreFocusRef={revokeFocusRef}
      />
    </div>
  )
}

function ClientTable({
  clients,
  selectedClientId,
  onSelect,
  onRevoke,
  revokingId,
}: {
  clients: MCPClient[]
  selectedClientId: string | null
  onSelect: (id: string) => void
  onRevoke: (client: MCPClient, restoreFocusTo: HTMLElement) => void
  revokingId: string | undefined
}) {
  const { t } = useTranslation()

  return (
    <Table aria-label={t('mcp_clients.table.aria_label')}>
      <TableCaption className="sr-only">{t('mcp_clients.table.aria_label')}</TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead>{t('mcp_clients.table.name')}</TableHead>
          <TableHead>{t('mcp_clients.table.client_id')}</TableHead>
          <TableHead>{t('mcp_clients.table.scopes')}</TableHead>
          <TableHead>{t('mcp_clients.table.policy_mode')}</TableHead>
          <TableHead>{t('mcp_clients.table.created')}</TableHead>
          <TableHead>{t('mcp_clients.table.status')}</TableHead>
          <TableHead className="text-right">{t('mcp_clients.table.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {clients.map((client) => {
          const isActive = !client.revoked_at
          const isSelected = client.id === selectedClientId
          return (
            <TableRow
              key={client.id}
              data-state={isSelected ? 'selected' : undefined}
              className={isActive ? '' : 'opacity-50'}
            >
              <TableCell className="font-medium">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="-ml-2 h-auto max-w-full justify-start px-2 py-1 text-left font-medium whitespace-normal"
                  aria-label={t('mcp_clients.table.select_client', { name: client.name })}
                  aria-current={isSelected ? 'true' : undefined}
                  onClick={() => onSelect(client.id)}
                >
                  {client.name}
                </Button>
              </TableCell>
              <TableCell className="font-mono text-xs">{truncateMiddle(client.id)}</TableCell>
              <TableCell>
                <div className="flex flex-wrap gap-1">
                  {client.scopes.map((scope) => (
                    <span
                      key={scope}
                      className="inline-flex items-center rounded-md bg-muted px-2 py-0.5 text-xs font-medium"
                    >
                      {scope}
                    </span>
                  ))}
                </div>
              </TableCell>
              <TableCell className="text-xs text-zinc-700 dark:text-zinc-300">
                {formatPolicyMode(client.tool_policy_mode, t)}
              </TableCell>
              <TableCell className="text-zinc-700 dark:text-zinc-300">
                {formatDistanceToNow(new Date(client.created_at), {
                  addSuffix: true,
                  locale: zhCN,
                })}
              </TableCell>
              <TableCell>
                {isActive ? (
                  <span className="text-emerald-700 dark:text-emerald-300">
                    {t('mcp_clients.status.active')}
                  </span>
                ) : (
                  <span className="text-muted-foreground">{t('mcp_clients.status.revoked')}</span>
                )}
              </TableCell>
              <TableCell className="text-right">
                {isActive ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={t('mcp_clients.revoke_client_aria', { name: client.name })}
                    title={t('mcp_clients.revoke_button')}
                    onClick={(event) => onRevoke(client, event.currentTarget)}
                    disabled={revokingId === client.id}
                  >
                    {revokingId === client.id ? (
                      <Loader2 aria-hidden="true" className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <Trash2 className="h-3.5 w-3.5" />
                    )}
                  </Button>
                ) : null}
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}

function ClientSummaryCard({ client }: { client: MCPClient }) {
  const { t } = useTranslation()

  return (
    <Card className="min-w-0">
      <CardHeader>
        <div className="flex items-start justify-between gap-4">
          <div>
            <CardTitle className="flex items-center gap-2">
              <Shield className="h-4 w-4" />
              {client.name}
            </CardTitle>
            <CardDescription className="mt-1 font-mono text-xs">{client.id}</CardDescription>
          </div>
          <span
            className={
              client.revoked_at ? 'text-muted-foreground' : 'text-emerald-700 dark:text-emerald-300'
            }
          >
            {client.revoked_at ? t('mcp_clients.status.revoked') : t('mcp_clients.status.active')}
          </span>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <div>
          <div className="mb-2 text-xs font-medium text-muted-foreground">
            {t('mcp_clients.detail.scopes')}
          </div>
          <div className="flex flex-wrap gap-1.5">
            {client.scopes.map((scope) => (
              <span
                key={scope}
                className="inline-flex items-center rounded-md bg-muted px-2 py-0.5 text-xs font-medium"
              >
                {scope}
              </span>
            ))}
          </div>
        </div>
        <div>
          <div className="mb-2 text-xs font-medium text-muted-foreground">
            {t('mcp_clients.detail.redirect_uris')}
          </div>
          <div className="space-y-1 font-mono text-xs text-muted-foreground">
            {client.redirect_uris.map((uri) => (
              <div key={uri}>{uri}</div>
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function GovernanceCard({
  revoked,
  mode,
  rateLimitRPM,
  rateLimitBurst,
  pending,
  onModeChange,
  onRateLimitRPMChange,
  onRateLimitBurstChange,
  onSave,
}: {
  revoked: boolean
  mode: 'legacy_allow_all' | 'allow_list'
  rateLimitRPM: string
  rateLimitBurst: string
  pending: boolean
  onModeChange: (value: string) => void
  onRateLimitRPMChange: (value: string) => void
  onRateLimitBurstChange: (value: string) => void
  onSave: () => void
}) {
  const { t } = useTranslation()

  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle>{t('mcp_clients.governance.title')}</CardTitle>
        <CardDescription>{t('mcp_clients.governance.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {revoked ? (
          <Alert>
            <AlertTitle>{t('mcp_clients.governance.revoked_title')}</AlertTitle>
            <AlertDescription>{t('mcp_clients.governance.revoked_body')}</AlertDescription>
          </Alert>
        ) : null}
        <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
          <div className="space-y-2">
            <Label htmlFor="mcp-client-policy-mode">
              {t('mcp_clients.governance.policy_mode')}
            </Label>
            <Select value={mode} onValueChange={onModeChange} disabled={revoked}>
              <SelectTrigger id="mcp-client-policy-mode" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="legacy_allow_all">
                  {t('mcp_clients.governance.mode.legacy_allow_all')}
                </SelectItem>
                <SelectItem value="allow_list">
                  {t('mcp_clients.governance.mode.allow_list')}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="mcp-client-rate-limit-rpm">
              {t('mcp_clients.governance.rate_limit_rpm')}
            </Label>
            <Input
              id="mcp-client-rate-limit-rpm"
              inputMode="numeric"
              placeholder={t('mcp_clients.governance.unset')}
              value={rateLimitRPM}
              disabled={revoked}
              onChange={(event) => onRateLimitRPMChange(event.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="mcp-client-rate-limit-burst">
              {t('mcp_clients.governance.rate_limit_burst')}
            </Label>
            <Input
              id="mcp-client-rate-limit-burst"
              inputMode="numeric"
              placeholder={t('mcp_clients.governance.unset')}
              value={rateLimitBurst}
              disabled={revoked}
              onChange={(event) => onRateLimitBurstChange(event.target.value)}
            />
          </div>
        </div>
        <div className="flex justify-end">
          <Button onClick={onSave} disabled={pending || revoked}>
            {pending ? <Loader2 aria-hidden="true" className="mr-2 h-4 w-4 animate-spin" /> : null}
            {t('mcp_clients.governance.save')}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

function ToolPoliciesCard({
  revoked,
  mode,
  tools,
  drafts,
  pending,
  onToggle,
  onRPMChange,
  onBurstChange,
  onSave,
}: {
  revoked: boolean
  mode: 'legacy_allow_all' | 'allow_list'
  tools: MCPClientTool[]
  drafts: Record<string, ToolDraft>
  pending: boolean
  onToggle: (tool: MCPClientTool) => void
  onRPMChange: (tool: MCPClientTool, value: string) => void
  onBurstChange: (tool: MCPClientTool, value: string) => void
  onSave: () => void
}) {
  const { t } = useTranslation()
  const activeEffect = mode === 'allow_list' ? 'allow' : 'deny'

  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle>{t('mcp_clients.tools.title')}</CardTitle>
        <CardDescription>
          {mode === 'allow_list'
            ? t('mcp_clients.tools.allow_list_hint')
            : t('mcp_clients.tools.legacy_hint')}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {revoked ? (
          <Alert>
            <AlertTitle>{t('mcp_clients.tools.revoked_title')}</AlertTitle>
            <AlertDescription>{t('mcp_clients.tools.revoked_body')}</AlertDescription>
          </Alert>
        ) : null}
        <Table aria-label={t('mcp_clients.tools.table_aria_label')}>
          <TableCaption className="sr-only">{t('mcp_clients.tools.table_aria_label')}</TableCaption>
          <TableHeader>
            <TableRow>
              <TableHead>{t('mcp_clients.tools.table.tool')}</TableHead>
              <TableHead>{t('mcp_clients.tools.table.scope')}</TableHead>
              <TableHead>{t('mcp_clients.tools.table.risk')}</TableHead>
              <TableHead>{t('mcp_clients.tools.table.effect')}</TableHead>
              <TableHead>{t('mcp_clients.tools.table.toggle')}</TableHead>
              <TableHead>{t('mcp_clients.tools.table.rpm')}</TableHead>
              <TableHead>{t('mcp_clients.tools.table.burst')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tools.map((tool) => {
              const draft = drafts[tool.name] ?? emptyDraft()
              const checked = draft.effect === activeEffect
              const disabled = revoked || !tool.scope_granted
              return (
                <TableRow key={tool.name} className={disabled ? 'opacity-50' : ''}>
                  <TableCell>
                    <div className="font-medium">{tool.name}</div>
                    <div className="text-xs text-muted-foreground">{tool.data_class}</div>
                  </TableCell>
                  <TableCell className="font-mono text-xs">{tool.required_scope}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{tool.risk}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {draft.effect === ''
                      ? t('mcp_clients.tools.effect.none')
                      : t(`mcp_clients.tools.effect.${draft.effect}`)}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <Checkbox
                        checked={checked}
                        disabled={disabled}
                        aria-label={t('mcp_clients.tools.toggle_label', {
                          tool: tool.name,
                          effect:
                            mode === 'allow_list'
                              ? t('mcp_clients.tools.toggle_allow')
                              : t('mcp_clients.tools.toggle_deny'),
                        })}
                        onCheckedChange={() => onToggle(tool)}
                      />
                      <span className="text-xs text-muted-foreground">
                        {disabled
                          ? t('mcp_clients.tools.scope_missing')
                          : mode === 'allow_list'
                            ? t('mcp_clients.tools.toggle_allow')
                            : t('mcp_clients.tools.toggle_deny')}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Input
                      inputMode="numeric"
                      className="h-8 w-24"
                      disabled={disabled}
                      aria-label={t('mcp_clients.tools.rpm_label', { tool: tool.name })}
                      placeholder={tool.default_rpm.toString()}
                      value={draft.rateLimitRPM}
                      onChange={(event) => onRPMChange(tool, event.target.value)}
                    />
                  </TableCell>
                  <TableCell>
                    <Input
                      inputMode="numeric"
                      className="h-8 w-24"
                      disabled={disabled}
                      aria-label={t('mcp_clients.tools.burst_label', { tool: tool.name })}
                      placeholder={tool.default_burst.toString()}
                      value={draft.rateLimitBurst}
                      onChange={(event) => onBurstChange(tool, event.target.value)}
                    />
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        <div className="flex justify-end">
          <Button onClick={onSave} disabled={pending || revoked}>
            {pending ? <Loader2 aria-hidden="true" className="mr-2 h-4 w-4 animate-spin" /> : null}
            {t('mcp_clients.tools.save')}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

function SessionsCard({
  sessions,
  pendingSessionId,
  onRevoke,
}: {
  sessions: MCPSession[]
  pendingSessionId: string | undefined
  onRevoke: (sessionId: string) => void
}) {
  const { t } = useTranslation()

  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle>{t('mcp_clients.sessions.title')}</CardTitle>
        <CardDescription>{t('mcp_clients.sessions.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {sessions.length === 0 ? (
          <div className="text-sm text-muted-foreground">{t('mcp_clients.sessions.empty')}</div>
        ) : (
          <Table aria-label={t('mcp_clients.sessions.table_aria_label')}>
            <TableCaption className="sr-only">
              {t('mcp_clients.sessions.table_aria_label')}
            </TableCaption>
            <TableHeader>
              <TableRow>
                <TableHead>{t('mcp_clients.sessions.table.session')}</TableHead>
                <TableHead>{t('mcp_clients.sessions.table.last_tool')}</TableHead>
                <TableHead>{t('mcp_clients.sessions.table.last_seen')}</TableHead>
                <TableHead>{t('mcp_clients.sessions.table.network')}</TableHead>
                <TableHead>{t('mcp_clients.sessions.table.status')}</TableHead>
                <TableHead className="text-right">
                  {t('mcp_clients.sessions.table.actions')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sessions.map((session) => {
                const active = !session.closed_at
                return (
                  <TableRow key={session.id}>
                    <TableCell className="font-mono text-xs">
                      {truncateMiddle(session.id)}
                    </TableCell>
                    <TableCell>
                      <div className="text-sm">{session.last_tool_name || '-'}</div>
                      <div className="text-xs text-muted-foreground">
                        {session.last_decision || '-'}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatDistanceToNow(new Date(session.last_active_at), {
                        addSuffix: true,
                        locale: zhCN,
                      })}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      <div>{session.last_ip || '-'}</div>
                      <div>{truncateAgent(session.last_user_agent)}</div>
                    </TableCell>
                    <TableCell className="text-xs">
                      {active ? (
                        <span className="text-emerald-700 dark:text-emerald-300">
                          {t('mcp_clients.status.active')}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">
                          {session.closed_reason || t('mcp_clients.status.revoked')}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      {active ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => onRevoke(session.id)}
                          disabled={pendingSessionId === session.id}
                        >
                          {pendingSessionId === session.id ? (
                            <Loader2 aria-hidden="true" className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            t('mcp_clients.sessions.revoke_button')
                          )}
                        </Button>
                      ) : null}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function RefreshGrantsCard({
  grants,
  pendingGrantId,
  onRevoke,
}: {
  grants: MCPRefreshGrant[]
  pendingGrantId: string | undefined
  onRevoke: (grantId: string) => void
}) {
  const { t } = useTranslation()

  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle>{t('mcp_clients.grants.title')}</CardTitle>
        <CardDescription>{t('mcp_clients.grants.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {grants.length === 0 ? (
          <div className="text-sm text-muted-foreground">{t('mcp_clients.grants.empty')}</div>
        ) : (
          <Table aria-label={t('mcp_clients.grants.table_aria_label')}>
            <TableCaption className="sr-only">
              {t('mcp_clients.grants.table_aria_label')}
            </TableCaption>
            <TableHeader>
              <TableRow>
                <TableHead>{t('mcp_clients.grants.table.grant')}</TableHead>
                <TableHead>{t('mcp_clients.grants.table.session')}</TableHead>
                <TableHead>{t('mcp_clients.grants.table.scopes')}</TableHead>
                <TableHead>{t('mcp_clients.grants.table.expires')}</TableHead>
                <TableHead className="text-right">
                  {t('mcp_clients.grants.table.actions')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {grants.map((grant) => (
                <TableRow key={grant.id}>
                  <TableCell className="font-mono text-xs">{truncateMiddle(grant.id)}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {truncateMiddle(grant.session_id)}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {grant.scopes.map((scope) => (
                        <span
                          key={scope}
                          className="inline-flex items-center rounded-md bg-muted px-2 py-0.5 text-xs font-medium"
                        >
                          {scope}
                        </span>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatDistanceToNow(new Date(grant.expires_at), {
                      addSuffix: true,
                      locale: zhCN,
                    })}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => onRevoke(grant.id)}
                      disabled={pendingGrantId === grant.id}
                    >
                      {pendingGrantId === grant.id ? (
                        <Loader2 aria-hidden="true" className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        t('mcp_clients.grants.revoke_button')
                      )}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function emptyDraft(): ToolDraft {
  return {
    effect: '',
    rateLimitRPM: '',
    rateLimitBurst: '',
  }
}

function parseNullableInt(value: string): number | null {
  const trimmed = value.trim()
  if (!trimmed) {
    return null
  }
  const parsed = Number.parseInt(trimmed, 10)
  return Number.isNaN(parsed) ? null : parsed
}

function truncateMiddle(value: string): string {
  if (value.length <= 12) {
    return value
  }
  return `${value.slice(0, 8)}…${value.slice(-4)}`
}

function truncateAgent(value: string): string {
  if (!value) {
    return '-'
  }
  if (value.length <= 28) {
    return value
  }
  return `${value.slice(0, 25)}…`
}

function formatPolicyMode(
  mode: 'legacy_allow_all' | 'allow_list',
  t: (key: string) => string,
): string {
  return mode === 'allow_list'
    ? t('mcp_clients.governance.mode.allow_list')
    : t('mcp_clients.governance.mode.legacy_allow_all')
}
