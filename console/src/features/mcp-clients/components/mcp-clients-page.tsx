import { useMutation, useQuery } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Bot, Loader2, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useCreateMCPClient } from '@/features/mcp-clients/api/create-mcp-client'
import { type MCPClient, mcpClientsQuery } from '@/features/mcp-clients/api/list-mcp-clients'
import { useRevokeMCPClient } from '@/features/mcp-clients/api/revoke-mcp-client'
import type { CreateMCPClientRequest } from '../api/types'
import { CreateMCPClientDialog, RevokeMCPClientDialog } from './dialogs'

export function MCPClientsPage() {
  const { t } = useTranslation()
  const list = useQuery(mcpClientsQuery())
  const create = useCreateMCPClient()
  const revoke = useRevokeMCPClient()

  const [createOpen, setCreateOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<MCPClient | null>(null)

  const handleCreate = (params: CreateMCPClientRequest) => {
    return create.mutateAsync(params, {
      onSuccess: () => {
        setCreateOpen(false)
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

  const activeClients = list.data?.filter((c) => !c.revoked_at) ?? []

  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('mcp_clients.title')}</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            {t('mcp_clients.subtitle')}
          </p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>{t('mcp_clients.create_button')}</Button>
      </div>

      <Card className="mt-6">
        <CardHeader>
          <CardTitle>{t('mcp_clients.title')}</CardTitle>
          <CardDescription>
            {activeClients.length} {t('mcp_clients.active_count')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {list.isPending ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('app.loading')}
            </div>
          ) : list.data && list.data.length > 0 ? (
            <ClientTable
              clients={list.data}
              onRevoke={(c) => setRevokeTarget(c)}
              revokingId={revoke.isPending ? revokeTarget?.id : undefined}
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
      />
    </div>
  )
}

function ClientTable({
  clients,
  onRevoke,
  revokingId,
}: {
  clients: MCPClient[]
  onRevoke: (c: MCPClient) => void
  revokingId: string | undefined
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('mcp_clients.table.name')}</TableHead>
          <TableHead>{t('mcp_clients.table.client_id')}</TableHead>
          <TableHead>{t('mcp_clients.table.scopes')}</TableHead>
          <TableHead>{t('mcp_clients.table.created')}</TableHead>
          <TableHead>{t('mcp_clients.table.status')}</TableHead>
          <TableHead className="text-right">{t('mcp_clients.table.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {clients.map((c) => {
          const isActive = !c.revoked_at
          return (
            <TableRow key={c.id} className={isActive ? '' : 'opacity-50'}>
              <TableCell className="font-medium">{c.name}</TableCell>
              <TableCell className="font-mono text-xs">{c.id.slice(0, 8)}…</TableCell>
              <TableCell>
                <div className="flex flex-wrap gap-1">
                  {c.scopes.map((s) => (
                    <span
                      key={s}
                      className="inline-flex items-center rounded-md bg-muted px-2 py-0.5 text-xs font-medium"
                    >
                      {s}
                    </span>
                  ))}
                </div>
              </TableCell>
              <TableCell className="text-muted-foreground">
                {formatDistanceToNow(new Date(c.created_at), { addSuffix: true, locale: zhCN })}
              </TableCell>
              <TableCell>
                {isActive ? (
                  <span className="text-green-600">{t('mcp_clients.status.active')}</span>
                ) : (
                  <span className="text-muted-foreground">{t('mcp_clients.status.revoked')}</span>
                )}
              </TableCell>
              <TableCell className="text-right">
                {isActive ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={t('mcp_clients.revoke_button')}
                    title={t('mcp_clients.revoke_button')}
                    onClick={() => onRevoke(c)}
                    disabled={revokingId === c.id}
                  >
                    {revokingId === c.id ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
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
