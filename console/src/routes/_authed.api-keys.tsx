import { useMutation, useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Key, Loader2, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { ApiKey, NewApiKey } from '@/api/queries'
import { apiKeysQuery, useCreateApiKey, useRevokeApiKey } from '@/api/queries'
import { CreateKeyDialog, RevokeKeyDialog, SecretKeyDialog } from '@/components/api-keys/dialogs'
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

export const Route = createFileRoute('/_authed/api-keys')({
  component: ApiKeysPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(apiKeysQuery()),
})

function ApiKeysPage() {
  const { t } = useTranslation()
  const list = useQuery(apiKeysQuery())
  const create = useCreateApiKey()
  const revoke = useRevokeApiKey()

  const [createOpen, setCreateOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<ApiKey | null>(null)
  const [issued, setIssued] = useState<NewApiKey | null>(null)

  const handleCreate = (label: string) => {
    return create.mutateAsync(label, {
      onSuccess: (newKey) => {
        setCreateOpen(false)
        setIssued(newKey)
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
      toast.success(t('api_keys.status.revoked'))
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
  })

  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('nav.api_keys')}</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{t('api_keys.subtitle')}</p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>{t('api_keys.create_button')}</Button>
      </div>

      <Card className="mt-6">
        <CardHeader>
          <CardTitle>{t('nav.api_keys')}</CardTitle>
          <CardDescription>{list.data?.length ?? 0}</CardDescription>
        </CardHeader>
        <CardContent>
          {list.isPending ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('app.loading')}
            </div>
          ) : list.data && list.data.length > 0 ? (
            <KeyTable
              keys={list.data}
              onRevoke={(k) => setRevokeTarget(k)}
              revokingId={revoke.isPending ? revokeTarget?.id : undefined}
            />
          ) : (
            <EmptyState
              icon={Key}
              title={t('api_keys.empty_title')}
              description={t('api_keys.empty_body')}
              action={{
                label: t('api_keys.create_button'),
                onClick: () => setCreateOpen(true),
              }}
            />
          )}
        </CardContent>
      </Card>

      <CreateKeyDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
        pending={create.isPending}
      />
      <SecretKeyDialog issued={issued} onClose={() => setIssued(null)} />
      <RevokeKeyDialog
        target={revokeTarget}
        onCancel={() => setRevokeTarget(null)}
        onConfirm={() => revokeTarget && handleRevoke.mutate(revokeTarget.id)}
        pending={handleRevoke.isPending}
      />
    </div>
  )
}

function KeyTable({
  keys,
  onRevoke,
  revokingId,
}: {
  keys: ApiKey[]
  onRevoke: (k: ApiKey) => void
  revokingId: string | undefined
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('api_keys.table.prefix')}</TableHead>
          <TableHead>{t('api_keys.table.label')}</TableHead>
          <TableHead>{t('api_keys.table.created')}</TableHead>
          <TableHead>{t('api_keys.table.last_used')}</TableHead>
          <TableHead>{t('api_keys.table.status')}</TableHead>
          <TableHead className="text-right">{t('api_keys.table.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {keys.map((k) => (
          <TableRow key={k.id} className={k.isActive ? '' : 'opacity-50'}>
            <TableCell className="font-mono text-xs">{k.keyPrefix}…</TableCell>
            <TableCell>{k.label || '—'}</TableCell>
            <TableCell className="text-muted-foreground">
              {formatDistanceToNow(new Date(k.createdAt), { addSuffix: true, locale: zhCN })}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {k.lastUsedAt
                ? formatDistanceToNow(new Date(k.lastUsedAt), { addSuffix: true, locale: zhCN })
                : t('common.never')}
            </TableCell>
            <TableCell>
              {k.isActive ? (
                <span className="text-green-600">{t('api_keys.status.active')}</span>
              ) : (
                <span className="text-muted-foreground">{t('api_keys.status.revoked')}</span>
              )}
            </TableCell>
            <TableCell className="text-right">
              {k.isActive ? (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => onRevoke(k)}
                  disabled={revokingId === k.id}
                >
                  {revokingId === k.id ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Trash2 className="h-3.5 w-3.5" />
                  )}
                </Button>
              ) : null}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
