import { useMutation, useQuery } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Key, Loader2, Trash2 } from 'lucide-react'
import { useCallback, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  type CreateApiKeyParams,
  type NewApiKey,
  useCreateApiKey,
} from '@/features/api-keys/api/create-api-key'
import { type ApiKey, apiKeysQuery } from '@/features/api-keys/api/list-api-keys'
import { useRevokeApiKey } from '@/features/api-keys/api/revoke-api-key'
import {
  CreateKeyDialog,
  RevokeKeyDialog,
  SecretKeyDialog,
} from '@/features/api-keys/components/dialogs'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { useDocumentTitle } from '@/hooks/use-document-title'
import { restoreFocusWhenReady } from '@/lib/focus'

export function ApiKeysPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('nav.api_keys'))
  const { can } = usePermissions()
  const canEdit = can('settings:api_keys:edit')
  const list = useQuery(apiKeysQuery())
  const create = useCreateApiKey()
  const revoke = useRevokeApiKey()

  const [createOpen, setCreateOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<ApiKey | null>(null)
  const [issued, setIssued] = useState<NewApiKey | null>(null)
  const createFocusRef = useRef<HTMLElement | null>(null)
  const revokeFocusRef = useRef<HTMLElement | null>(null)
  const keys = list.data ?? []
  const activeCount = keys.filter((key) => key.isActive).length
  const revokedCount = keys.length - activeCount
  const recentlyUsedCount = keys.filter((key) => Boolean(key.lastUsedAt)).length
  const setCreateButtonRef = useCallback((node: HTMLButtonElement | null) => {
    createFocusRef.current = node
  }, [])

  const handleCreate = (params: CreateApiKeyParams) => {
    return create.mutateAsync(params, {
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

  const openCreateDialog = (event?: React.MouseEvent<HTMLElement>) => {
    const active = document.activeElement
    createFocusRef.current =
      event?.currentTarget ??
      (active instanceof HTMLElement && active !== document.body ? active : null)
    setCreateOpen(true)
  }
  const handleCreateOpenChange = (open: boolean) => {
    setCreateOpen(open)
    if (!open && issued === null) {
      restoreFocusWhenReady(
        document.querySelector<HTMLElement>('[data-testid="api-key-create-button"]') ??
          createFocusRef.current,
      )
    }
  }

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.integrations')}
        title={t('nav.api_keys')}
        subtitle={t('api_keys.subtitle')}
        actions={
          canEdit ? (
            <Button
              data-testid="api-key-create-button"
              ref={setCreateButtonRef}
              onClick={openCreateDialog}
            >
              {t('api_keys.create_button')}
            </Button>
          ) : undefined
        }
        metrics={
          <>
            <PageHeroMetric
              label={t('api_keys.summary.total')}
              value={String(keys.length)}
              hint={t('api_keys.summary.total_hint')}
            />
            <PageHeroMetric
              label={t('api_keys.summary.active')}
              value={String(activeCount)}
              hint={t('api_keys.summary.active_hint')}
            />
            <PageHeroMetric
              label={t('api_keys.summary.revoked')}
              value={String(revokedCount)}
              hint={t('api_keys.summary.revoked_hint')}
            />
            <PageHeroMetric
              label={t('api_keys.summary.recent')}
              value={String(recentlyUsedCount)}
              hint={t('api_keys.summary.recent_hint')}
            />
          </>
        }
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="text-base">{t('api_keys.registry_title')}</CardTitle>
            <CardDescription>
              {t('api_keys.registry_description', { count: keys.length })}
            </CardDescription>
          </CardHeader>
          <CardContent className="pt-6">
            {list.isPending ? (
              <Loading />
            ) : keys.length > 0 ? (
              <KeyTable
                keys={keys}
                onRevoke={
                  canEdit
                    ? (key, restoreFocusTo) => {
                        revokeFocusRef.current = restoreFocusTo
                        setRevokeTarget(key)
                      }
                    : undefined
                }
                revokingId={revoke.isPending ? revokeTarget?.id : undefined}
              />
            ) : (
              <EmptyState
                icon={Key}
                title={t('api_keys.empty_title')}
                description={t('api_keys.empty_body')}
                action={
                  canEdit
                    ? {
                        label: t('api_keys.create_button'),
                        onClick: openCreateDialog,
                      }
                    : undefined
                }
              />
            )}
          </CardContent>
        </Card>

        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="text-base">{t('api_keys.playbook_title')}</CardTitle>
            <CardDescription>{t('api_keys.playbook_description')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 pt-6">
            <PlaybookRow
              title={t('api_keys.playbook.issue_title')}
              body={t('api_keys.playbook.issue_body')}
            />
            <PlaybookRow
              title={t('api_keys.playbook.review_title')}
              body={t('api_keys.playbook.review_body')}
            />
            <PlaybookRow
              title={t('api_keys.playbook.revoke_title')}
              body={t('api_keys.playbook.revoke_body')}
            />
          </CardContent>
        </Card>
      </div>

      <CreateKeyDialog
        open={createOpen}
        onOpenChange={handleCreateOpenChange}
        onSubmit={handleCreate}
        pending={create.isPending}
        restoreFocusRef={createFocusRef}
        restoreFocusOnClose={issued === null}
      />
      <SecretKeyDialog
        issued={issued}
        onClose={() => setIssued(null)}
        restoreFocusRef={createFocusRef}
      />
      <RevokeKeyDialog
        target={revokeTarget}
        onCancel={() => setRevokeTarget(null)}
        onConfirm={() => revokeTarget && handleRevoke.mutate(revokeTarget.id)}
        pending={handleRevoke.isPending}
        restoreFocusRef={revokeFocusRef}
      />
    </div>
  )
}

function PlaybookRow({ title, body }: { title: string; body: string }) {
  return (
    <div className="rounded-[1rem] border border-border/60 bg-background/85 px-4 py-3.5">
      <div className="text-sm font-semibold text-foreground">{title}</div>
      <div className="mt-1 text-sm leading-6 text-muted-foreground">{body}</div>
    </div>
  )
}

function KeyTable({
  keys,
  onRevoke,
  revokingId,
}: {
  keys: ApiKey[]
  onRevoke?: (k: ApiKey, restoreFocusTo: HTMLElement) => void
  revokingId: string | undefined
}) {
  const { t } = useTranslation()
  return (
    <Table aria-label={t('api_keys.table.aria_label')}>
      <TableCaption className="sr-only">{t('api_keys.table.aria_label')}</TableCaption>
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
                <span className="text-emerald-700 dark:text-emerald-300">
                  {t('api_keys.status.active')}
                </span>
              ) : (
                <span className="text-muted-foreground">{t('api_keys.status.revoked')}</span>
              )}
            </TableCell>
            <TableCell className="text-right">
              {k.isActive && onRevoke ? (
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label={t('api_keys.revoke_key_aria', {
                    label: k.label || k.keyPrefix,
                  })}
                  title={t('api_keys.revoke_button')}
                  onClick={(event) => onRevoke(k, event.currentTarget)}
                  disabled={revokingId === k.id}
                >
                  {revokingId === k.id ? (
                    <Loader2 aria-hidden="true" className="h-3.5 w-3.5 animate-spin" />
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
