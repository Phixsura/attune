import { useQuery } from '@tanstack/react-query'
import { formatDistanceToNow, type Locale } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { AlertTriangle, Bot, ShieldCheck, Trash2 } from 'lucide-react'
import { type MouseEvent, useCallback, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
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
import type { CreateServiceAccountParams } from '@/features/api-keys/api/create-service-account'
import { useCreateServiceAccount } from '@/features/api-keys/api/create-service-account'
import { useDeleteServiceAccount } from '@/features/api-keys/api/delete-service-account'
import type { ServiceAccount } from '@/features/api-keys/api/list-service-accounts'
import { serviceAccountsQuery } from '@/features/api-keys/api/list-service-accounts'
import type { UpdateServiceAccountParams } from '@/features/api-keys/api/update-service-account'
import { useUpdateServiceAccount } from '@/features/api-keys/api/update-service-account'
import { ServiceAccountDeleteDialog } from '@/features/api-keys/components/service-account-delete-dialog'
import { CreateServiceAccountDialog } from '@/features/api-keys/components/service-account-dialog'
import { ServiceAccountStatusDialog } from '@/features/api-keys/components/service-account-status-dialog'
import { cn } from '@/lib/utils'

export function ServiceAccountsCard({ canEdit }: { canEdit: boolean }) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language.startsWith('zh') ? zhCN : undefined
  const list = useQuery({ ...serviceAccountsQuery(), enabled: canEdit })
  const create = useCreateServiceAccount()
  const remove = useDeleteServiceAccount()
  const update = useUpdateServiceAccount()
  const [createOpen, setCreateOpen] = useState(false)
  const [toggleTarget, setToggleTarget] = useState<ServiceAccount | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ServiceAccount | null>(null)
  const createFocusRef = useRef<HTMLElement | null>(null)
  const toggleFocusRef = useRef<HTMLElement | null>(null)
  const deleteFocusRef = useRef<HTMLElement | null>(null)
  const retryList = useCallback(() => {
    void list.refetch()
  }, [list])

  const accounts = list.data ?? []
  const activeCount = accounts.filter((account) => account.isActive).length
  const toggleOpen = toggleTarget !== null
  const deleteOpen = deleteTarget !== null

  const setCreateButtonRef = useCallback((node: HTMLButtonElement | null) => {
    createFocusRef.current = node
  }, [])

  const openCreateDialog = (event?: MouseEvent<HTMLElement>) => {
    const active = document.activeElement
    createFocusRef.current =
      event?.currentTarget ??
      (active instanceof HTMLElement && active !== document.body ? active : null)
    setCreateOpen(true)
  }

  const openToggleDialog = useCallback(
    (account: ServiceAccount, event?: MouseEvent<HTMLButtonElement>) => {
      const active = document.activeElement
      toggleFocusRef.current =
        event?.currentTarget ??
        (active instanceof HTMLElement && active !== document.body ? active : null)
      setToggleTarget(account)
    },
    [],
  )

  const openDeleteDialog = useCallback(
    (account: ServiceAccount, event?: MouseEvent<HTMLButtonElement>) => {
      const active = document.activeElement
      deleteFocusRef.current =
        event?.currentTarget ??
        (active instanceof HTMLElement && active !== document.body ? active : null)
      setDeleteTarget(account)
    },
    [],
  )

  const handleCreate = (params: CreateServiceAccountParams) => {
    return create.mutateAsync(params, {
      onSuccess: (serviceAccount) => {
        setCreateOpen(false)
        toast.success(
          t('api_keys.service_accounts.created_success', {
            name: serviceAccount.name,
            defaultValue: '已创建服务账号 {{name}}',
          }),
        )
      },
      onError: (err) => {
        toast.error(err instanceof Error ? err.message : 'failed')
      },
    })
  }

  const handleToggle = () => {
    if (!toggleTarget) return Promise.resolve()
    const nextActive = !toggleTarget.isActive
    const params: UpdateServiceAccountParams = {
      id: toggleTarget.id,
      isActive: nextActive,
    }
    return update.mutateAsync(params, {
      onSuccess: (serviceAccount) => {
        toast.success(
          t('api_keys.service_accounts.updated_success', {
            name: serviceAccount.name,
            defaultValue: '已更新服务账号 {{name}} 状态',
          }),
        )
      },
      onError: (err) => {
        toast.error(err instanceof Error ? err.message : 'failed')
      },
    })
  }

  const handleDelete = () => {
    if (!deleteTarget) return Promise.resolve()
    const account = deleteTarget
    return remove.mutateAsync(account.id, {
      onSuccess: () => {
        toast.success(
          t('api_keys.service_accounts.deleted_success', {
            name: account.name,
            defaultValue: '已删除服务账号 {{name}}',
          }),
        )
      },
      onError: (err) => {
        toast.error(err instanceof Error ? err.message : 'failed')
      },
    })
  }

  if (!canEdit) {
    return null
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader className="gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1">
          <CardTitle className="text-base">
            {t('api_keys.service_accounts.title', '服务账号目录')}
          </CardTitle>
          <CardDescription>
            {t('api_keys.service_accounts.description', {
              count: accounts.length,
              active: activeCount,
            })}
          </CardDescription>
        </div>
        {canEdit ? (
          <Button ref={setCreateButtonRef} variant="outline" size="sm" onClick={openCreateDialog}>
            {t('api_keys.service_accounts.create_button', '新增服务账号')}
          </Button>
        ) : null}
      </CardHeader>
      <CardContent className="space-y-3">
        {list.isPending ? (
          <Loading />
        ) : list.isError && !list.data ? (
          <ServiceAccountsErrorState onRetry={retryList} />
        ) : accounts.length > 0 ? (
          <ServiceAccountTable
            accounts={accounts}
            locale={locale}
            canEdit={canEdit}
            pending={update.isPending || remove.isPending}
            onToggle={openToggleDialog}
            onDelete={openDeleteDialog}
          />
        ) : (
          <EmptyState
            icon={Bot}
            title={t('api_keys.service_accounts.empty_title', '还没有服务账号')}
            description={t(
              'api_keys.service_accounts.empty_body',
              '先创建一个服务账号，把自动化凭证和人类登录分开。',
            )}
            action={
              canEdit
                ? {
                    label: t('api_keys.service_accounts.create_button', '新增服务账号'),
                    onClick: openCreateDialog,
                  }
                : undefined
            }
          />
        )}
      </CardContent>

      <CreateServiceAccountDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
        pending={create.isPending}
        restoreFocusRef={createFocusRef}
      />

      <ServiceAccountStatusDialog
        open={toggleOpen}
        onOpenChange={(next) => {
          if (!next) {
            setToggleTarget(null)
          }
        }}
        serviceAccountName={toggleTarget?.name ?? ''}
        nextActive={toggleTarget ? !toggleTarget.isActive : true}
        pending={update.isPending}
        onConfirm={handleToggle}
        restoreFocusRef={toggleFocusRef}
      />

      <ServiceAccountDeleteDialog
        open={deleteOpen}
        onOpenChange={(next) => {
          if (!next) {
            setDeleteTarget(null)
          }
        }}
        serviceAccountName={deleteTarget?.name ?? ''}
        pending={remove.isPending}
        onConfirm={handleDelete}
        restoreFocusRef={deleteFocusRef}
      />
    </Card>
  )
}

function ServiceAccountsErrorState({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation()

  return (
    <div className="rounded-[1.1rem] border border-destructive/20 bg-[linear-gradient(180deg,rgba(254,242,242,0.82),rgba(255,255,255,0.96))] p-5 shadow-[0_24px_80px_-66px_rgba(185,28,28,0.42)]">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-start gap-4">
          <div className="flex size-11 shrink-0 items-center justify-center rounded-2xl border border-destructive/20 bg-background/92 text-destructive">
            <AlertTriangle className="size-5" aria-hidden="true" />
          </div>
          <div className="min-w-0">
            <h3 className="text-lg font-semibold tracking-tight text-foreground">
              {t('api_keys.service_accounts.error_title', '服务账号列表暂时无法加载')}
            </h3>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground text-pretty">
              {t(
                'api_keys.service_accounts.error_body',
                '请稍后重试；如果问题持续，确认当前账号是否具备管理服务账号的权限。',
              )}
            </p>
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={onRetry} className="shrink-0">
          {t('common.retry')}
        </Button>
      </div>
    </div>
  )
}

function ServiceAccountTable({
  accounts,
  locale,
  canEdit,
  pending,
  onToggle,
  onDelete,
}: {
  accounts: ServiceAccount[]
  locale: Locale | undefined
  canEdit: boolean
  pending: boolean
  onToggle: (account: ServiceAccount, event?: MouseEvent<HTMLButtonElement>) => void
  onDelete: (account: ServiceAccount, event?: MouseEvent<HTMLButtonElement>) => void
}) {
  const { t } = useTranslation()
  return (
    <Table aria-label={t('api_keys.service_accounts.table.aria_label', '服务账号列表')}>
      <TableCaption className="sr-only">
        {t('api_keys.service_accounts.table.aria_label', '服务账号列表')}
      </TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead>{t('api_keys.service_accounts.table.name', '名称')}</TableHead>
          <TableHead>{t('api_keys.service_accounts.table.description', '说明')}</TableHead>
          <TableHead>{t('api_keys.service_accounts.table.created', '创建于')}</TableHead>
          <TableHead>{t('api_keys.service_accounts.table.updated', '更新于')}</TableHead>
          <TableHead className="text-right">
            {t('api_keys.service_accounts.table.status', '状态')}
          </TableHead>
          {canEdit ? (
            <TableHead className="text-right">
              {t('api_keys.service_accounts.table.actions', '操作')}
            </TableHead>
          ) : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {accounts.map((account) => (
          <TableRow key={account.id}>
            <TableCell className="font-medium">
              <div className="space-y-0.5">
                <div className="text-sm font-semibold text-foreground">{account.name}</div>
                <div className="font-mono text-[11px] text-muted-foreground">{account.id}</div>
              </div>
            </TableCell>
            <TableCell className="max-w-[18rem] text-sm text-muted-foreground">
              {account.description || '—'}
            </TableCell>
            <TableCell className="text-sm text-muted-foreground">
              {formatRelativeTime(account.createdAt, locale)}
            </TableCell>
            <TableCell className="text-sm text-muted-foreground">
              {formatRelativeTime(account.updatedAt, locale)}
            </TableCell>
            <TableCell className="text-right">
              <StatusPill active={account.isActive} />
            </TableCell>
            {canEdit ? (
              <TableCell className="text-right">
                <div className="inline-flex items-center gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant={account.isActive ? 'destructive' : 'outline'}
                    disabled={pending}
                    aria-label={
                      account.isActive
                        ? t('api_keys.service_accounts.actions.disable_aria', {
                            name: account.name,
                            defaultValue: '停用服务账号 {{name}}',
                          })
                        : t('api_keys.service_accounts.actions.enable_aria', {
                            name: account.name,
                            defaultValue: '启用服务账号 {{name}}',
                          })
                    }
                    onClick={(event) => onToggle(account, event)}
                  >
                    {account.isActive
                      ? t('api_keys.service_accounts.actions.disable', '停用')
                      : t('api_keys.service_accounts.actions.enable', '启用')}
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    className="border-destructive/30 text-destructive hover:bg-destructive/5 hover:text-destructive"
                    disabled={pending}
                    aria-label={t('api_keys.service_accounts.actions.delete_aria', {
                      name: account.name,
                      defaultValue: '删除服务账号 {{name}}',
                    })}
                    onClick={(event) => onDelete(account, event)}
                  >
                    <Trash2 className="mr-1 h-3.5 w-3.5" aria-hidden="true" />
                    {t('api_keys.service_accounts.actions.delete', '删除')}
                  </Button>
                </div>
              </TableCell>
            ) : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function StatusPill({ active }: { active: boolean }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium',
        active
          ? 'border-emerald-300/60 bg-emerald-50 text-emerald-700 dark:border-emerald-500/40 dark:bg-emerald-500/10 dark:text-emerald-300'
          : 'border-amber-300/60 bg-amber-50 text-amber-700 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-300',
      )}
    >
      <ShieldCheck className="mr-1 h-3 w-3" aria-hidden="true" />
      {active
        ? t('api_keys.service_accounts.status.active', '启用')
        : t('api_keys.service_accounts.status.inactive', '停用')}
    </span>
  )
}

function formatRelativeTime(value: string, locale: Locale | undefined) {
  return formatDistanceToNow(new Date(value), {
    addSuffix: true,
    locale,
  })
}
