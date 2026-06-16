import { useMutation, useQuery } from '@tanstack/react-query'
import { AlertTriangle, Bot, Plus, RefreshCw, RouteIcon, SlidersHorizontal } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  type CreateLLMChannelRequest,
  type LLMChannel,
  type LLMChannelAbility,
  type LLMRoute,
  llmAbilitiesQuery,
  llmChannelModelsQuery,
  llmChannelsQuery,
  llmRoutesQuery,
  type TestLLMChannelResponse,
  type UpdateLLMChannelRequest,
  type UpsertLLMChannelAbilityRequest,
  type UpsertLLMRouteRequest,
  useCreateLLMChannel,
  useDeleteLLMAbility,
  useDeleteLLMChannel,
  useDeleteLLMRoute,
  useTestLLMChannel,
  useUpdateLLMChannel,
  useUpsertLLMAbility,
  useUpsertLLMRoute,
} from '@/features/llm-config/api/llm-config'
import {
  AbilityDialog,
  ChannelDialog,
  ConfirmDialog,
  RouteDialog,
  TestChannelDialog,
} from '@/features/llm-config/components/dialogs'
import { AbilityTable, ChannelTable, RouteTable } from '@/features/llm-config/components/tables'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import type { ApiError } from '@/lib/api-client'

export function LLMConfigPage() {
  const { t } = useTranslation()
  const { can } = usePermissions()
  const canEdit = can('llm_config:edit')
  const channels = useQuery(llmChannelsQuery())
  const routes = useQuery(llmRoutesQuery())
  const [selectedId, setSelectedId] = useState('')
  const selected = useMemo(
    () => channels.data?.find((channel) => channel.id === selectedId) ?? channels.data?.[0] ?? null,
    [channels.data, selectedId],
  )
  const abilities = useQuery(llmAbilitiesQuery(selected?.id ?? ''))

  useEffect(() => {
    if (!selectedId && channels.data?.[0]) {
      setSelectedId(channels.data[0].id)
    }
  }, [channels.data, selectedId])

  const createChannel = useCreateLLMChannel()
  const updateChannel = useUpdateLLMChannel()
  const deleteChannel = useDeleteLLMChannel()
  const testChannel = useTestLLMChannel()
  const upsertAbility = useUpsertLLMAbility()
  const deleteAbility = useDeleteLLMAbility()
  const upsertRoute = useUpsertLLMRoute()
  const deleteRoute = useDeleteLLMRoute()

  const [channelDialog, setChannelDialog] = useState<{ open: boolean; target: LLMChannel | null }>({
    open: false,
    target: null,
  })
  const [abilityDialog, setAbilityDialog] = useState<LLMChannelAbility | 'new' | null>(null)
  const [routeDialog, setRouteDialog] = useState<LLMRoute | 'new' | null>(null)
  const [testTarget, setTestTarget] = useState<LLMChannel | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<
    | { kind: 'channel'; row: LLMChannel }
    | { kind: 'ability'; row: LLMChannelAbility }
    | { kind: 'route'; row: LLMRoute }
    | null
  >(null)
  const [lastTest, setLastTest] = useState<TestLLMChannelResponse | null>(null)
  const abilityModels = useQuery(
    llmChannelModelsQuery(selected?.id ?? '', abilityDialog !== null && selected !== null),
  )
  const testModels = useQuery(llmChannelModelsQuery(testTarget?.id ?? '', testTarget !== null))

  const refresh = () => {
    void channels.refetch()
    void routes.refetch()
    if (selected?.id) {
      void abilities.refetch()
    }
  }

  const submitChannel = (
    body: CreateLLMChannelRequest | Omit<UpdateLLMChannelRequest, 'id'>,
  ): Promise<unknown> => {
    const target = channelDialog.target
    if (target) {
      return updateChannel.mutateAsync(
        { id: target.id, body },
        {
          onSuccess: (row) => {
            setSelectedId(row.id)
            setChannelDialog({ open: false, target: null })
            toast.success(t('llm_config.toast.channel_saved'))
          },
          onError: toastError,
        },
      )
    }
    return createChannel.mutateAsync(body as CreateLLMChannelRequest, {
      onSuccess: (row) => {
        setSelectedId(row.id)
        setChannelDialog({ open: false, target: null })
        toast.success(t('llm_config.toast.channel_saved'))
      },
      onError: toastError,
    })
  }

  const submitAbility = (
    body: Omit<UpsertLLMChannelAbilityRequest, 'channelId'>,
  ): Promise<unknown> => {
    if (!selected) return Promise.resolve()
    return upsertAbility.mutateAsync(
      { channelId: selected.id, body },
      {
        onSuccess: () => {
          setAbilityDialog(null)
          toast.success(t('llm_config.toast.ability_saved'))
        },
        onError: toastError,
      },
    )
  }

  const submitRoute = (body: UpsertLLMRouteRequest): Promise<unknown> =>
    upsertRoute.mutateAsync(body, {
      onSuccess: () => {
        setRouteDialog(null)
        toast.success(t('llm_config.toast.route_saved'))
      },
      onError: toastError,
    })

  const runTest = (body: { providerModel: string; prompt: string }): Promise<unknown> => {
    if (!testTarget) return Promise.resolve()
    return testChannel.mutateAsync(
      { id: testTarget.id, body },
      {
        onSuccess: (result) => {
          setLastTest(result)
          setTestTarget(null)
          toast.success(t('llm_config.toast.test_ok', { ms: result.latencyMs || '0' }))
        },
        onError: (err) => {
          toastError(err)
        },
      },
    )
  }

  const confirmDelete = useMutation({
    mutationFn: async () => {
      if (!deleteTarget) return
      if (deleteTarget.kind === 'channel') {
        await deleteChannel.mutateAsync(deleteTarget.row.id)
      } else if (deleteTarget.kind === 'ability') {
        await deleteAbility.mutateAsync({
          channelId: deleteTarget.row.channelId,
          logicalModel: deleteTarget.row.logicalModel,
        })
      } else {
        await deleteRoute.mutateAsync({
          tenantId: deleteTarget.row.tenantId,
          purpose: deleteTarget.row.purpose,
        })
      }
    },
    onSuccess: () => {
      setDeleteTarget(null)
      toast.success(t('common.delete'))
    },
    onError: toastError,
  })

  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('nav.llm_config')}</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{t('llm_config.subtitle')}</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={refresh} title={t('llm_config.actions.refresh')}>
            <RefreshCw className="size-4" />
          </Button>
          {canEdit && (
            <Button onClick={() => setChannelDialog({ open: true, target: null })}>
              <Plus className="mr-2 size-4" />
              {t('llm_config.channels.create')}
            </Button>
          )}
        </div>
      </div>

      <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1.45fr)_minmax(22rem,0.8fr)]">
        <Card>
          <CardHeader>
            <CardTitle>{t('llm_config.channels.title')}</CardTitle>
            <CardDescription>{channels.data?.length ?? 0}</CardDescription>
          </CardHeader>
          <CardContent>
            {channels.isPending ? (
              <Loading />
            ) : channels.isError ? (
              <QueryErrorState
                message={queryError(channels.error)}
                onRetry={() => {
                  void channels.refetch()
                }}
              />
            ) : channels.data && channels.data.length > 0 ? (
              <ChannelTable
                channels={channels.data}
                selectedId={selected?.id ?? ''}
                testingId={testChannel.isPending ? testChannel.variables?.id : undefined}
                onSelect={(channel) => setSelectedId(channel.id)}
                onEdit={
                  canEdit
                    ? (channel) => setChannelDialog({ open: true, target: channel })
                    : undefined
                }
                onAbilities={
                  canEdit
                    ? (channel) => {
                        setSelectedId(channel.id)
                        setAbilityDialog('new')
                      }
                    : undefined
                }
                onTest={canEdit ? setTestTarget : undefined}
                onDelete={
                  canEdit
                    ? (channel) => setDeleteTarget({ kind: 'channel', row: channel })
                    : undefined
                }
              />
            ) : (
              <EmptyState
                icon={Bot}
                title={t('llm_config.channels.empty_title')}
                description={t('llm_config.channels.empty_body')}
                action={
                  canEdit
                    ? {
                        label: t('llm_config.channels.create'),
                        onClick: () => setChannelDialog({ open: true, target: null }),
                      }
                    : undefined
                }
              />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex-row items-start justify-between space-y-0">
            <div>
              <CardTitle>{t('llm_config.abilities.title')}</CardTitle>
              <CardDescription>
                {selected?.name ?? t('llm_config.abilities.no_channel')}
              </CardDescription>
            </div>
            {canEdit && (
              <Button
                size="sm"
                disabled={!selected}
                onClick={() => setAbilityDialog('new')}
                title={t('llm_config.abilities.create')}
              >
                <Plus className="size-4" />
              </Button>
            )}
          </CardHeader>
          <CardContent>
            {!selected ? (
              <EmptyState
                icon={SlidersHorizontal}
                title={t('llm_config.abilities.no_channel')}
                description={t('llm_config.channels.empty_body')}
              />
            ) : abilities.isPending ? (
              <Loading />
            ) : abilities.isError ? (
              <QueryErrorState
                message={queryError(abilities.error)}
                onRetry={() => {
                  void abilities.refetch()
                }}
              />
            ) : abilities.data && abilities.data.length > 0 ? (
              <AbilityTable
                abilities={abilities.data}
                onEdit={canEdit ? (ability) => setAbilityDialog(ability) : undefined}
                onDelete={
                  canEdit
                    ? (ability) => setDeleteTarget({ kind: 'ability', row: ability })
                    : undefined
                }
              />
            ) : (
              <EmptyState
                icon={SlidersHorizontal}
                title={t('llm_config.abilities.empty_title')}
                description={t('llm_config.abilities.empty_body')}
                action={
                  canEdit
                    ? {
                        label: t('llm_config.abilities.create'),
                        onClick: () => setAbilityDialog('new'),
                      }
                    : undefined
                }
              />
            )}
          </CardContent>
        </Card>
      </div>

      <Card className="mt-6">
        <CardHeader className="flex-row items-start justify-between space-y-0">
          <div>
            <CardTitle>{t('llm_config.routes.title')}</CardTitle>
            <CardDescription>{routes.data?.length ?? 0}</CardDescription>
          </div>
          {canEdit && (
            <Button
              size="sm"
              onClick={() => setRouteDialog('new')}
              title={t('llm_config.routes.create')}
            >
              <Plus className="size-4" />
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {routes.isPending ? (
            <Loading />
          ) : routes.isError ? (
            <QueryErrorState
              message={queryError(routes.error)}
              onRetry={() => {
                void routes.refetch()
              }}
            />
          ) : routes.data && routes.data.length > 0 ? (
            <RouteTable
              routes={routes.data}
              onEdit={canEdit ? (route) => setRouteDialog(route) : undefined}
              onDelete={
                canEdit ? (route) => setDeleteTarget({ kind: 'route', row: route }) : undefined
              }
            />
          ) : (
            <EmptyState
              icon={RouteIcon}
              title={t('llm_config.routes.empty_title')}
              description={t('llm_config.routes.empty_body')}
              action={
                canEdit
                  ? {
                      label: t('llm_config.routes.create'),
                      onClick: () => setRouteDialog('new'),
                    }
                  : undefined
              }
            />
          )}
        </CardContent>
      </Card>

      {lastTest && (
        <div className="mt-4 rounded-md border bg-muted/30 px-3 py-2 text-sm">
          <span className="font-medium">{lastTest.providerModel}</span>
          <span className="ml-3 text-muted-foreground">
            {lastTest.inputTokens} / {lastTest.outputTokens} tokens · {lastTest.latencyMs}ms
          </span>
        </div>
      )}

      <ChannelDialog
        open={channelDialog.open}
        target={channelDialog.target}
        pending={createChannel.isPending || updateChannel.isPending}
        onOpenChange={(open) =>
          setChannelDialog({ open, target: open ? channelDialog.target : null })
        }
        onSubmit={submitChannel}
      />
      <AbilityDialog
        open={abilityDialog !== null}
        target={abilityDialog === 'new' ? null : abilityDialog}
        models={abilityModels.data ?? []}
        modelsPending={abilityModels.isFetching}
        modelsError={queryError(abilityModels.error)}
        pending={upsertAbility.isPending}
        onRefreshModels={() => {
          void abilityModels.refetch()
        }}
        onOpenChange={(open) => !open && setAbilityDialog(null)}
        onSubmit={submitAbility}
      />
      <RouteDialog
        open={routeDialog !== null}
        target={routeDialog === 'new' ? null : routeDialog}
        pending={upsertRoute.isPending}
        onOpenChange={(open) => !open && setRouteDialog(null)}
        onSubmit={submitRoute}
      />
      <TestChannelDialog
        target={testTarget}
        models={testModels.data ?? []}
        modelsPending={testModels.isFetching}
        modelsError={queryError(testModels.error)}
        pending={testChannel.isPending}
        onRefreshModels={() => {
          void testModels.refetch()
        }}
        onClose={() => setTestTarget(null)}
        onSubmit={runTest}
      />
      <ConfirmDialog
        open={deleteTarget !== null}
        title={deleteTitle(deleteTarget, t)}
        body={deleteBody(deleteTarget, t)}
        pending={confirmDelete.isPending}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => confirmDelete.mutate()}
      />
    </div>
  )
}

function QueryErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <EmptyState
      icon={AlertTriangle}
      title={t('common.error')}
      description={message}
      action={{ label: t('llm_config.actions.refresh'), onClick: onRetry }}
    />
  )
}

function toastError(err: unknown) {
  toast.error(errorText(err) || 'failed')
}

function queryError(err: unknown) {
  return errorText(err)
}

function errorText(err: unknown) {
  if (!(err instanceof Error)) return ''
  const apiErr = err as ApiError
  const details = [apiErr.code, apiErr.requestId].filter(Boolean).join(' · ')
  return details ? `${err.message} (${details})` : err.message
}

function deleteTitle(
  target:
    | { kind: 'channel'; row: LLMChannel }
    | { kind: 'ability'; row: LLMChannelAbility }
    | { kind: 'route'; row: LLMRoute }
    | null,
  t: (key: string) => string,
) {
  if (!target) return ''
  return t(`llm_config.delete.${target.kind}_title`)
}

function deleteBody(
  target:
    | { kind: 'channel'; row: LLMChannel }
    | { kind: 'ability'; row: LLMChannelAbility }
    | { kind: 'route'; row: LLMRoute }
    | null,
  t: (key: string, opts?: Record<string, string>) => string,
) {
  if (!target) return ''
  if (target.kind === 'channel') {
    return t('llm_config.delete.channel_body', { name: target.row.name })
  }
  if (target.kind === 'ability') {
    return t('llm_config.delete.ability_body', { model: target.row.logicalModel })
  }
  return t('llm_config.delete.route_body', {
    purpose: target.row.purpose,
    scope: target.row.tenantId || t('llm_config.routes.global'),
  })
}
