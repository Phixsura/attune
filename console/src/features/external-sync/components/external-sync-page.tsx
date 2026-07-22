import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  FileSearch,
  GitBranch,
  Loader2,
  Pencil,
  Plus,
  RefreshCcw,
  RotateCcw,
  ShieldAlert,
  Trash2,
} from 'lucide-react'
import type { FormEvent } from 'react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
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
import {
  batchResolveExternalSyncConflicts,
  type CreateExternalConnectionRequest,
  type CreateExternalProviderInstallationRequest,
  createExternalConnection,
  createExternalProviderInstallation,
  deleteExternalConnection,
  deleteExternalProviderInstallation,
  type ExternalConnection,
  type ExternalObjectMapping,
  type ExternalObjectSchema,
  type ExternalProviderInstallation,
  type ExternalProviderInstallationResource,
  type ExternalSyncConflict,
  ExternalSyncConflictResolution,
  ExternalSyncDirection,
  type ExternalSyncEvent,
  type ExternalSyncProvider,
  type ExternalSyncRecordFailure,
  type ExternalSyncRecordTimelineEntry,
  type ExternalSyncRun,
  type ExternalSyncRunDetail,
  externalProviderInstallationResourcesQuery,
  externalProviderInstallationsQuery,
  externalSyncConnectionSchemaQuery,
  externalSyncConnectionsQuery,
  externalSyncEventQuery,
  externalSyncEventsQuery,
  externalSyncHealthQuery,
  externalSyncMappingsQuery,
  externalSyncProvidersQuery,
  externalSyncQueryKeys,
  externalSyncRunQuery,
  externalSyncRunsQuery,
  getExternalSyncRecordTimeline,
  previewExternalObjectMapping,
  qualifyExternalConnection,
  qualifyExternalProviderInstallation,
  replayExternalSyncEvent,
  requestExternalSyncBackfill,
  requestExternalSyncRun,
  resetExternalSyncCursor,
  resolveExternalSyncConflict,
  resumeExternalConnection,
  retryExternalSyncFailure,
  retryExternalSyncRun,
  selectExternalProviderInstallationResources,
  testExternalConnection,
  type UpdateExternalConnectionRequest,
  updateExternalConnection,
  updateExternalMapping,
} from '@/features/external-sync/api/external-sync'
import { useDocumentTitle } from '@/hooks/use-document-title'
import { cn } from '@/lib/utils'

const runLimit = 25
const activeRunRefreshMs = 2_000
const activeRunStatuses = [
  'running',
  'queued',
  'EXTERNAL_SYNC_RUN_STATUS_RUNNING',
  'EXTERNAL_SYNC_RUN_STATUS_QUEUED',
]

type RecordTimelineTarget = {
  mappingId: string
  localObjectId: string
  externalKey: string
  label: string
}

type PendingValue<T> = {
  isPending: boolean
  variables?: T
}

export function activeConnectionActionIDsFromMutations({
  deleting,
  qualifying,
  resuming,
  testing,
  updating,
}: {
  deleting: PendingValue<string>
  qualifying: PendingValue<string>
  resuming: PendingValue<string>
  testing: PendingValue<string>
  updating: PendingValue<{ id?: string }>
}) {
  return {
    deleting: deleting.isPending ? deleting.variables : undefined,
    qualifying: qualifying.isPending ? qualifying.variables : undefined,
    resuming: resuming.isPending ? resuming.variables : undefined,
    testing: testing.isPending ? testing.variables : undefined,
    updating: updating.isPending ? updating.variables?.id : undefined,
  }
}

export function activeMappingActionIDsFromMutations({
  backfilling,
  previewing,
  resetting,
  saving,
}: {
  backfilling: PendingValue<{ id?: string }>
  previewing: PendingValue<{ id?: string }>
  resetting: PendingValue<{ id?: string }>
  saving: PendingValue<{ id?: string }>
}) {
  return {
    backfilling: backfilling.isPending ? backfilling.variables?.id : undefined,
    previewing: previewing.isPending ? previewing.variables?.id : undefined,
    resetting: resetting.isPending ? resetting.variables?.id : undefined,
    saving: saving.isPending ? saving.variables?.id : undefined,
  }
}

export function activeRunActionIDsFromMutations({
  replayingEvent,
  resolvingConflict,
  retryingFailure,
  retryingRun,
}: {
  replayingEvent: PendingValue<string>
  resolvingConflict: PendingValue<{ id?: string }>
  retryingFailure: PendingValue<string>
  retryingRun: PendingValue<string>
}) {
  return {
    replayingEvent: replayingEvent.isPending ? replayingEvent.variables : undefined,
    resolvingConflict: resolvingConflict.isPending ? resolvingConflict.variables?.id : undefined,
    retryingFailure: retryingFailure.isPending ? retryingFailure.variables : undefined,
    retryingRun: retryingRun.isPending ? retryingRun.variables : undefined,
  }
}

export function ExternalSyncPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  useDocumentTitle(t('nav.external_sync'))

  const health = useQuery(externalSyncHealthQuery())
  const providersQuery = useQuery(externalSyncProvidersQuery())
  const installationsQuery = useQuery(externalProviderInstallationsQuery())
  const connectionsQuery = useQuery(externalSyncConnectionsQuery())
  const providers = providersQuery.data ?? []
  const installations = installationsQuery.data ?? []
  const connections = connectionsQuery.data ?? []
  const [selectedInstallationID, setSelectedInstallationID] = useState('')
  const [selectedConnectionID, setSelectedConnectionID] = useState('')
  const [selectedRunID, setSelectedRunID] = useState('')
  const [selectedEventID, setSelectedEventID] = useState('')
  const [timelineTarget, setTimelineTarget] = useState<RecordTimelineTarget | null>(null)
  const [createInstallationOpen, setCreateInstallationOpen] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [editingConnection, setEditingConnection] = useState<ExternalConnection | null>(null)

  useEffect(() => {
    if (selectedInstallationID || installations.length === 0) return
    setSelectedInstallationID(installations[0]?.id ?? '')
  }, [installations, selectedInstallationID])

  useEffect(() => {
    if (selectedConnectionID || connections.length === 0) return
    setSelectedConnectionID(connections[0]?.id ?? '')
  }, [connections, selectedConnectionID])

  const installationResourcesQuery = useQuery(
    externalProviderInstallationResourcesQuery(selectedInstallationID || undefined),
  )
  const installationResources = installationResourcesQuery.data ?? []
  const selectedInstallation =
    installations.find((installation) => installation.id === selectedInstallationID) ?? null
  const selectedConnection = connections.find((conn) => conn.id === selectedConnectionID) ?? null
  const mappingsQuery = useQuery(externalSyncMappingsQuery(selectedConnectionID || undefined))
  const mappings = mappingsQuery.data ?? []
  const selectedMapping = mappings[0] ?? null
  const schemaQuery = useQuery(externalSyncConnectionSchemaQuery(selectedConnectionID || undefined))
  const schemas = schemaQuery.data ?? []
  const runsQuery = useInfiniteQuery({
    ...externalSyncRunsQuery(runLimit, { connectionId: selectedConnectionID }),
    enabled: Boolean(selectedConnectionID),
  })
  const runs = runsQuery.data?.pages.flatMap((page) => page.runs) ?? []
  const shouldRefreshActiveRuns = runs.some(isActiveRun)
  const eventsQuery = useInfiniteQuery({
    ...externalSyncEventsQuery(runLimit, { connectionId: selectedConnectionID }),
    enabled: Boolean(selectedConnectionID),
  })
  const events = eventsQuery.data?.pages.flatMap((page) => page.events) ?? []
  const runDetailQuery = useQuery({
    ...externalSyncRunQuery(selectedRunID),
    enabled: Boolean(selectedRunID),
  })
  const eventDetailQuery = useQuery({
    ...externalSyncEventQuery(selectedEventID),
    enabled: Boolean(selectedEventID),
  })

  useEffect(() => {
    if (!selectedConnectionID || !shouldRefreshActiveRuns) return

    const timer = window.setInterval(() => {
      void queryClient.invalidateQueries({
        queryKey: externalSyncQueryKeys.runs(runLimit, { connectionId: selectedConnectionID }),
      })
      void queryClient.invalidateQueries({ queryKey: externalSyncQueryKeys.health() })
      if (selectedRunID) {
        void queryClient.invalidateQueries({ queryKey: externalSyncQueryKeys.run(selectedRunID) })
      }
    }, activeRunRefreshMs)

    return () => window.clearInterval(timer)
  }, [queryClient, selectedConnectionID, selectedRunID, shouldRefreshActiveRuns])

  const invalidateExternalSync = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: externalSyncQueryKeys.root }),
      queryClient.invalidateQueries({ queryKey: externalSyncQueryKeys.connections() }),
    ])

  const createConnection = useMutation({
    mutationFn: createExternalConnection,
    onSuccess: async (conn) => {
      await invalidateExternalSync()
      setCreateOpen(false)
      setSelectedConnectionID(conn.id)
      setSelectedRunID('')
      setSelectedEventID('')
      toast.success(t('external_sync.toast.created'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const createProviderInstallation = useMutation({
    mutationFn: createExternalProviderInstallation,
    onSuccess: async (installation) => {
      await invalidateExternalSync()
      setCreateInstallationOpen(false)
      setSelectedInstallationID(installation.id)
      toast.success(t('external_sync.toast.installation_created'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const qualifyProviderInstallation = useMutation({
    mutationFn: qualifyExternalProviderInstallation,
    onSuccess: async (result) => {
      await invalidateExternalSync()
      const description = qualificationToastDescription(result.checks)
      if (result.ready) {
        toast.success(t('external_sync.toast.installation_ready', { grade: result.grade }), {
          description,
        })
      } else {
        toast.warning(t('external_sync.toast.installation_attention', { grade: result.grade }), {
          description,
        })
      }
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const deleteProviderInstallation = useMutation({
    mutationFn: deleteExternalProviderInstallation,
    onSuccess: async () => {
      await invalidateExternalSync()
      setSelectedInstallationID('')
      toast.success(t('external_sync.toast.installation_deleted'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const selectProviderResources = useMutation({
    mutationFn: ({ id, resourceIds }: { id: string; resourceIds: string[] }) =>
      selectExternalProviderInstallationResources(id, resourceIds),
    onSuccess: async () => {
      await invalidateExternalSync()
      if (selectedInstallationID) {
        await queryClient.invalidateQueries({
          queryKey: externalSyncQueryKeys.providerInstallationResources(selectedInstallationID),
        })
      }
      toast.success(t('external_sync.toast.installation_resources_saved'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const updateConnection = useMutation({
    mutationFn: updateExternalConnection,
    onSuccess: async (conn) => {
      await invalidateExternalSync()
      setEditingConnection(null)
      setSelectedConnectionID(conn.id)
      setSelectedEventID('')
      toast.success(t('external_sync.toast.updated'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const testConnection = useMutation({
    mutationFn: testExternalConnection,
    onSuccess: async (result) => {
      await invalidateExternalSync()
      if (result.ok) toast.success(t('external_sync.toast.test_ok', { ms: result.latencyMs }))
      else toast.error(result.error || t('external_sync.toast.test_failed'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const resumeConnection = useMutation({
    mutationFn: resumeExternalConnection,
    onSuccess: async (conn) => {
      await invalidateExternalSync()
      setSelectedConnectionID(conn.id)
      toast.success(t('external_sync.toast.connection_resumed'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const qualifyConnection = useMutation({
    mutationFn: qualifyExternalConnection,
    onSuccess: (result) => {
      const description = qualificationToastDescription(result.checks)
      if (result.ready) {
        toast.success(t('external_sync.toast.qualification_ready'), { description })
      } else {
        toast.warning(t('external_sync.toast.qualification_attention'), { description })
      }
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const deleteConnection = useMutation({
    mutationFn: deleteExternalConnection,
    onSuccess: async () => {
      await invalidateExternalSync()
      setSelectedConnectionID('')
      setSelectedRunID('')
      setSelectedEventID('')
      toast.success(t('external_sync.toast.deleted'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const requestRun = useMutation({
    mutationFn: requestExternalSyncRun,
    onSuccess: async (run) => {
      await invalidateExternalSync()
      setSelectedRunID(run.id)
      toast.success(t('external_sync.toast.run_requested'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const saveMapping = useMutation({
    mutationFn: updateExternalMapping,
    onSuccess: async () => {
      await invalidateExternalSync()
      toast.success(t('external_sync.toast.mapping_saved'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const previewMapping = useMutation({
    mutationFn: previewExternalObjectMapping,
    onSuccess: (result) => {
      if (result.errors.length > 0) {
        toast.error(t('external_sync.toast.preview_errors', { count: result.errors.length }), {
          description: result.errors.join('\n'),
        })
        return
      }
      if (result.warnings.length > 0) {
        toast.warning(
          t('external_sync.toast.preview_warnings', { count: result.warnings.length }),
          {
            description: result.warnings.join('\n'),
          },
        )
        return
      }
      toast.success(t('external_sync.toast.preview_ok'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const resetCursor = useMutation({
    mutationFn: resetExternalSyncCursor,
    onSuccess: async (result) => {
      await invalidateExternalSync()
      if (result.run?.id) setSelectedRunID(result.run.id)
      toast.success(t('external_sync.toast.cursor_reset'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const requestBackfill = useMutation({
    mutationFn: requestExternalSyncBackfill,
    onSuccess: async (result) => {
      await invalidateExternalSync()
      if (result.run?.id) setSelectedRunID(result.run.id)
      toast.success(t('external_sync.toast.backfill_requested'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const retryRun = useMutation({
    mutationFn: retryExternalSyncRun,
    onSuccess: async (run) => {
      await invalidateExternalSync()
      setSelectedRunID(run.id)
      toast.success(t('external_sync.toast.run_retried'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const retryFailure = useMutation({
    mutationFn: retryExternalSyncFailure,
    onSuccess: async () => {
      await invalidateExternalSync()
      if (selectedRunID) {
        await queryClient.invalidateQueries({ queryKey: externalSyncQueryKeys.run(selectedRunID) })
      }
      toast.success(t('external_sync.toast.failure_retried'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const resolveConflict = useMutation({
    mutationFn: ({ id, resolution }: { id: string; resolution: ExternalSyncConflictResolution }) =>
      resolveExternalSyncConflict(id, resolution),
    onSuccess: async () => {
      await invalidateExternalSync()
      if (selectedRunID) {
        await queryClient.invalidateQueries({ queryKey: externalSyncQueryKeys.run(selectedRunID) })
      }
      toast.success(t('external_sync.toast.conflict_resolved'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const batchResolveConflicts = useMutation({
    mutationFn: ({
      ids,
      resolution,
    }: {
      ids: string[]
      resolution: ExternalSyncConflictResolution
    }) => batchResolveExternalSyncConflicts(ids, resolution),
    onSuccess: async (result) => {
      await invalidateExternalSync()
      if (selectedRunID) {
        await queryClient.invalidateQueries({ queryKey: externalSyncQueryKeys.run(selectedRunID) })
      }
      toast.success(t('external_sync.toast.conflicts_resolved', { count: result.resolvedCount }))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const replayEvent = useMutation({
    mutationFn: replayExternalSyncEvent,
    onSuccess: async (result) => {
      await invalidateExternalSync()
      if (result.run?.id) setSelectedRunID(result.run.id)
      if (result.event?.id) setSelectedEventID(result.event.id)
      toast.success(t('external_sync.toast.event_replayed'))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const recordTimeline = useMutation({
    mutationFn: getExternalSyncRecordTimeline,
    onError: (err) => toast.error(errorMessage(err)),
  })

  const activeConnectionActionIDs = activeConnectionActionIDsFromMutations({
    deleting: deleteConnection,
    qualifying: qualifyConnection,
    resuming: resumeConnection,
    testing: testConnection,
    updating: updateConnection,
  })
  const activeMappingActionIDs = activeMappingActionIDsFromMutations({
    backfilling: requestBackfill,
    previewing: previewMapping,
    resetting: resetCursor,
    saving: saveMapping,
  })
  const activeRunActionIDs = activeRunActionIDsFromMutations({
    replayingEvent: replayEvent,
    resolvingConflict: resolveConflict,
    retryingFailure: retryFailure,
    retryingRun: retryRun,
  })

  const clearTimeline = () => {
    setTimelineTarget(null)
    recordTimeline.reset()
  }

  const showTimeline = (target: RecordTimelineTarget) => {
    setTimelineTarget(target)
    recordTimeline.mutate({
      mappingId: target.mappingId,
      localObjectId: target.localObjectId,
      externalKey: target.externalKey,
      limit: 20,
    })
  }

  const summary = health.data
  const activeRuns = summary?.activeRuns ?? 0
  const throttledRuns = summary?.throttledRuns ?? 0
  const unauthorizedRuns = summary?.unauthorizedRuns ?? 0
  const providerUnavailableRuns = summary?.providerUnavailableRuns ?? 0
  const delayedRetryRuns = summary?.delayedRetryRuns ?? 0
  const degradedConnections = summary?.degradedConnections ?? 0
  const quarantinedConnections = summary?.quarantinedConnections ?? 0
  const unhealthy =
    (summary?.failingConnections ?? 0) +
    (summary?.deadRuns ?? 0) +
    (summary?.openConflicts ?? 0) +
    degradedConnections +
    quarantinedConnections +
    throttledRuns +
    unauthorizedRuns +
    providerUnavailableRuns

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.integrations')}
        title={t('nav.external_sync')}
        subtitle={t('external_sync.subtitle')}
        actions={
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              onClick={() => setCreateInstallationOpen(true)}
              className="gap-2"
            >
              <Plus className="size-4" />
              {t('external_sync.actions.new_installation')}
            </Button>
            <Button onClick={() => setCreateOpen(true)} className="gap-2">
              <Plus className="size-4" />
              {t('external_sync.actions.new_connection')}
            </Button>
          </div>
        }
        metrics={
          <>
            <PageHeroMetric
              label={t('external_sync.summary.connections')}
              value={String(summary?.enabledConnections ?? 0)}
            />
            <PageHeroMetric
              label={t('external_sync.summary.active_runs')}
              value={String(activeRuns)}
              tone={activeRuns > 0 ? 'active' : 'default'}
            />
            <PageHeroMetric
              label={t('external_sync.summary.retryable')}
              value={String(summary?.retryableRuns ?? 0)}
            />
            <PageHeroMetric
              label={t('external_sync.summary.conflicts')}
              value={String(summary?.openConflicts ?? 0)}
              tone={(summary?.openConflicts ?? 0) > 0 ? 'urgent' : 'default'}
            />
            <PageHeroMetric
              label={t('external_sync.summary.unhealthy')}
              value={String(unhealthy)}
              tone={unhealthy > 0 ? 'urgent' : 'default'}
              hint={summary?.newestSuccessfulRunAt ? formatDate(summary.newestSuccessfulRunAt) : ''}
            />
            <PageHeroMetric
              label={t('external_sync.summary.throttled')}
              value={String(throttledRuns)}
              tone={throttledRuns > 0 ? 'urgent' : 'default'}
              hint={summary?.newestRetryAfter ? formatDate(summary.newestRetryAfter) : ''}
            />
            <PageHeroMetric
              label={t('external_sync.summary.unauthorized')}
              value={String(unauthorizedRuns)}
              tone={unauthorizedRuns > 0 ? 'urgent' : 'default'}
            />
            <PageHeroMetric
              label={t('external_sync.summary.provider_unavailable')}
              value={String(providerUnavailableRuns)}
              tone={providerUnavailableRuns > 0 ? 'urgent' : 'default'}
            />
            <PageHeroMetric
              label={t('external_sync.summary.delayed_retry')}
              value={String(delayedRetryRuns)}
              tone={delayedRetryRuns > 0 ? 'active' : 'default'}
            />
            <PageHeroMetric
              label={t('external_sync.summary.degraded_connections')}
              value={String(degradedConnections)}
              tone={degradedConnections > 0 ? 'urgent' : 'default'}
            />
            <PageHeroMetric
              label={t('external_sync.summary.quarantined_connections')}
              value={String(quarantinedConnections)}
              tone={quarantinedConnections > 0 ? 'urgent' : 'default'}
            />
          </>
        }
      />

      <div className="grid min-w-0 gap-6 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)]">
        <div className="min-w-0 space-y-6">
          <ProviderInstallationsCard
            installations={installations}
            resources={installationResources}
            loading={installationsQuery.isPending}
            resourcesLoading={
              installationResourcesQuery.isPending && Boolean(selectedInstallationID)
            }
            selectedID={selectedInstallationID}
            qualifyingID={
              qualifyProviderInstallation.isPending
                ? qualifyProviderInstallation.variables
                : undefined
            }
            deletingID={
              deleteProviderInstallation.isPending
                ? deleteProviderInstallation.variables
                : undefined
            }
            selecting={selectProviderResources.isPending}
            onSelect={setSelectedInstallationID}
            onQualify={(id) => qualifyProviderInstallation.mutate(id)}
            onDelete={(id) => deleteProviderInstallation.mutate(id)}
            onSaveResources={(id, resourceIds) =>
              selectProviderResources.mutate({ id, resourceIds })
            }
          />

          <ConnectionsCard
            connections={connections}
            loading={connectionsQuery.isPending}
            selectedID={selectedConnectionID}
            testingID={activeConnectionActionIDs.testing}
            deletingID={activeConnectionActionIDs.deleting}
            updatingID={activeConnectionActionIDs.updating}
            resumingID={activeConnectionActionIDs.resuming}
            qualifyingID={activeConnectionActionIDs.qualifying}
            requesting={requestRun.isPending}
            selectedMapping={selectedMapping}
            onSelect={(id) => {
              setSelectedConnectionID(id)
              setSelectedRunID('')
              setSelectedEventID('')
              clearTimeline()
            }}
            onEdit={setEditingConnection}
            onTest={(id) => testConnection.mutate(id)}
            onResume={(id) => resumeConnection.mutate(id)}
            onQualify={(id) => qualifyConnection.mutate(id)}
            onDelete={(id) => deleteConnection.mutate(id)}
            onRun={(connectionID, mapping) =>
              requestRun.mutate({
                connectionId: connectionID,
                mappingId: mapping?.id ?? '',
                direction:
                  mapping?.direction ?? ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_UNSPECIFIED,
                localObjectId: '',
                externalKey: '',
              })
            }
          />

          <MappingsCard
            connection={selectedConnection}
            mappings={mappings}
            schemas={schemas}
            loading={mappingsQuery.isPending}
            schemaLoading={schemaQuery.isPending && Boolean(selectedConnectionID)}
            savingID={activeMappingActionIDs.saving}
            previewingID={activeMappingActionIDs.previewing}
            resettingID={activeMappingActionIDs.resetting}
            backfillingID={activeMappingActionIDs.backfilling}
            onSave={(mapping) => saveMapping.mutate(mapping)}
            onPreview={(id, fieldMappingJson, statusMappingJson) =>
              previewMapping.mutate({ id, fieldMappingJson, statusMappingJson })
            }
            onResetCursor={(id) => resetCursor.mutate({ id })}
            onBackfill={(id, resetCursor) => requestBackfill.mutate({ id, resetCursor })}
          />
        </div>

        <div className="min-w-0 space-y-6">
          <RunsCard
            runs={runs}
            loading={Boolean(selectedConnectionID) && runsQuery.isPending}
            selectedID={selectedRunID}
            retryingID={activeRunActionIDs.retryingRun}
            hasNextPage={runsQuery.hasNextPage}
            loadingMore={runsQuery.isFetchingNextPage}
            onSelect={(id) => {
              setSelectedRunID(id)
              clearTimeline()
            }}
            onRetry={(id) => retryRun.mutate(id)}
            onLoadMore={() => void runsQuery.fetchNextPage()}
          />

          <EventsCard
            events={events}
            loading={Boolean(selectedConnectionID) && eventsQuery.isPending}
            selectedID={selectedEventID}
            replayingID={activeRunActionIDs.replayingEvent}
            hasNextPage={eventsQuery.hasNextPage}
            loadingMore={eventsQuery.isFetchingNextPage}
            onSelect={setSelectedEventID}
            onReplay={(id) => replayEvent.mutate(id)}
            onLoadMore={() => void eventsQuery.fetchNextPage()}
          />

          <EventDetailCard
            event={eventDetailQuery.data}
            loading={eventDetailQuery.isPending && Boolean(selectedEventID)}
          />

          <RunDetailCard
            detail={runDetailQuery.data}
            loading={runDetailQuery.isPending && Boolean(selectedRunID)}
            retryingFailureID={activeRunActionIDs.retryingFailure}
            resolvingConflictID={activeRunActionIDs.resolvingConflict}
            batchResolving={batchResolveConflicts.isPending}
            timelineEntries={recordTimeline.data?.entries ?? []}
            timelineLoading={recordTimeline.isPending}
            timelineTarget={timelineTarget}
            onRetryFailure={(id) => retryFailure.mutate(id)}
            onShowTimeline={showTimeline}
            onResolveConflict={(id, resolution) => resolveConflict.mutate({ id, resolution })}
            onBatchResolveConflicts={(ids, resolution) =>
              batchResolveConflicts.mutate({ ids, resolution })
            }
          />
        </div>
      </div>

      <CreateConnectionDialog
        open={createOpen}
        pending={createConnection.isPending}
        providers={providers}
        selectedInstallation={selectedInstallation}
        selectedInstallationResources={installationResources}
        onOpenChange={setCreateOpen}
        onSubmit={(body) => createConnection.mutate(body)}
      />
      <CreateProviderInstallationDialog
        open={createInstallationOpen}
        pending={createProviderInstallation.isPending}
        providers={providers}
        onOpenChange={setCreateInstallationOpen}
        onSubmit={(body) => createProviderInstallation.mutate(body)}
      />
      <EditConnectionDialog
        open={Boolean(editingConnection)}
        connection={editingConnection}
        pending={updateConnection.isPending}
        onOpenChange={(open) => {
          if (!open) setEditingConnection(null)
        }}
        onSubmit={(body) => updateConnection.mutate(body)}
      />
    </div>
  )
}

export function ProviderInstallationsCard({
  installations,
  resources,
  loading,
  resourcesLoading,
  selectedID,
  qualifyingID,
  deletingID,
  selecting,
  onSelect,
  onQualify,
  onDelete,
  onSaveResources,
}: {
  installations: ExternalProviderInstallation[]
  resources: ExternalProviderInstallationResource[]
  loading: boolean
  resourcesLoading: boolean
  selectedID: string
  qualifyingID?: string
  deletingID?: string
  selecting: boolean
  onSelect: (id: string) => void
  onQualify: (id: string) => void
  onDelete: (id: string) => void
  onSaveResources: (id: string, resourceIds: string[]) => void
}) {
  const { t } = useTranslation()
  const selected = installations.find((installation) => installation.id === selectedID) ?? null
  const [selectedResourceIDs, setSelectedResourceIDs] = useState<string[]>([])

  useEffect(() => {
    setSelectedResourceIDs(
      resources.filter((resource) => resource.selected).map((resource) => resource.id),
    )
  }, [resources])

  const toggleResource = (resourceID: string, checked: boolean) => {
    setSelectedResourceIDs((current) => {
      const next = new Set(current)
      if (checked) next.add(resourceID)
      else next.delete(resourceID)
      return Array.from(next)
    })
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('external_sync.installations.title')}</CardTitle>
        <CardDescription>
          {t('external_sync.installations.description', { count: installations.length })}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 pt-6">
        {loading ? (
          <Loading />
        ) : installations.length === 0 ? (
          <EmptyState
            icon={GitBranch}
            title={t('external_sync.installations.empty_title')}
            description={t('external_sync.installations.empty_body')}
          />
        ) : (
          <div className="space-y-2">
            {installations.map((installation) => {
              const grade = capabilityGrade(installation.capabilityProfileJson)
              return (
                <div
                  key={installation.id}
                  className={cn(
                    'flex w-full min-w-0 flex-col gap-3 rounded-lg border px-3 py-3 transition-colors sm:flex-row sm:items-start sm:justify-between',
                    selectedID === installation.id
                      ? 'border-primary/50 bg-primary/5'
                      : 'border-border/60 bg-background hover:bg-muted/50',
                  )}
                >
                  <button
                    type="button"
                    onClick={() => onSelect(installation.id)}
                    className="min-w-0 flex-1 text-left"
                  >
                    <div className="flex min-w-0 items-center gap-2">
                      <span className="truncate text-sm font-semibold">
                        {installation.displayName}
                      </span>
                      <StatusPill value={installation.status} />
                      <StatusPill value={installation.qualificationStatus || 'untested'} />
                    </div>
                    <div className="mt-1 flex flex-wrap gap-2 text-xs text-muted-foreground">
                      <span className="font-mono">{installation.provider}</span>
                      <span>{installation.installationKind}</span>
                      <span>{installation.resourceSelection}</span>
                      {grade && <span>{grade}</span>}
                      {installation.accountLogin && <span>{installation.accountLogin}</span>}
                    </div>
                    {installation.lastError && (
                      <div className="mt-2 line-clamp-2 text-xs text-destructive">
                        {installation.lastError}
                      </div>
                    )}
                  </button>
                  <div className="flex w-full flex-wrap items-center gap-1 sm:w-auto sm:shrink-0 sm:justify-end">
                    <Button
                      type="button"
                      size="icon-xs"
                      variant="ghost"
                      aria-label={t('external_sync.actions.qualify_installation')}
                      onClick={() => onQualify(installation.id)}
                      disabled={qualifyingID === installation.id}
                    >
                      {qualifyingID === installation.id ? (
                        <Loader2 className="size-3 animate-spin" />
                      ) : (
                        <FileSearch className="size-3" />
                      )}
                    </Button>
                    <Button
                      type="button"
                      size="icon-xs"
                      variant="ghost"
                      aria-label={t('external_sync.actions.delete_installation')}
                      onClick={() => onDelete(installation.id)}
                      disabled={deletingID === installation.id}
                    >
                      {deletingID === installation.id ? (
                        <Loader2 className="size-3 animate-spin" />
                      ) : (
                        <Trash2 className="size-3" />
                      )}
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        )}

        {selected && (
          <div className="rounded-lg border border-border/60 bg-muted/20 px-3 py-3">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <div className="text-sm font-semibold">
                  {t('external_sync.installations.resources_title')}
                </div>
                <div className="text-xs text-muted-foreground">
                  {t('external_sync.installations.resources_description', {
                    name: selected.displayName,
                    count: resources.length,
                  })}
                </div>
              </div>
              <Button
                type="button"
                size="sm"
                onClick={() => onSaveResources(selected.id, selectedResourceIDs)}
                disabled={selecting || resourcesLoading}
              >
                {selecting && <Loader2 className="size-3 animate-spin" />}
                {t('common.save')}
              </Button>
            </div>
            <div className="mt-3 space-y-2">
              {resourcesLoading ? (
                <Loading />
              ) : resources.length === 0 ? (
                <div className="text-sm text-muted-foreground">
                  {t('external_sync.installations.resources_empty')}
                </div>
              ) : (
                resources.map((resource) => (
                  <label
                    key={resource.id}
                    htmlFor={`provider-resource-${resource.id}`}
                    className="flex min-w-0 items-start gap-3 rounded border border-border/60 bg-background px-3 py-2"
                  >
                    <Checkbox
                      id={`provider-resource-${resource.id}`}
                      checked={selectedResourceIDs.includes(resource.id)}
                      onCheckedChange={(value) => toggleResource(resource.id, Boolean(value))}
                      disabled={selecting}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">
                        {resource.displayName}
                      </span>
                      <span className="mt-1 flex flex-wrap gap-2 text-xs text-muted-foreground">
                        <span className="font-mono">{resource.resourceKey}</span>
                        <span>{resource.resourceType}</span>
                        <span>{resource.status}</span>
                      </span>
                    </span>
                  </label>
                ))
              )}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

export function ConnectionsCard({
  connections,
  loading,
  selectedID,
  testingID,
  deletingID,
  updatingID,
  resumingID,
  qualifyingID,
  requesting,
  selectedMapping,
  onSelect,
  onEdit,
  onTest,
  onResume,
  onQualify,
  onDelete,
  onRun,
}: {
  connections: ExternalConnection[]
  loading: boolean
  selectedID: string
  testingID?: string
  deletingID?: string
  updatingID?: string
  resumingID?: string
  qualifyingID?: string
  requesting: boolean
  selectedMapping: ExternalObjectMapping | null
  onSelect: (id: string) => void
  onEdit: (connection: ExternalConnection) => void
  onTest: (id: string) => void
  onResume: (id: string) => void
  onQualify: (id: string) => void
  onDelete: (id: string) => void
  onRun: (connectionID: string, mapping: ExternalObjectMapping | null) => void
}) {
  const { t } = useTranslation()
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('external_sync.connections.title')}</CardTitle>
        <CardDescription>
          {t('external_sync.connections.description', { count: connections.length })}
        </CardDescription>
      </CardHeader>
      <CardContent className="pt-6">
        {loading ? (
          <Loading />
        ) : connections.length === 0 ? (
          <EmptyState
            icon={GitBranch}
            title={t('external_sync.connections.empty_title')}
            description={t('external_sync.connections.empty_body')}
          />
        ) : (
          <div className="space-y-2">
            {connections.map((conn) => (
              <div
                key={conn.id}
                className={cn(
                  'flex w-full min-w-0 flex-col gap-3 rounded-lg border px-3 py-3 transition-colors sm:flex-row sm:items-start sm:justify-between',
                  selectedID === conn.id
                    ? 'border-primary/50 bg-primary/5'
                    : 'border-border/60 bg-background hover:bg-muted/50',
                )}
              >
                <button
                  type="button"
                  onClick={() => onSelect(conn.id)}
                  className="min-w-0 flex-1 text-left"
                >
                  <div className="min-w-0">
                    <div className="flex min-w-0 items-center gap-2">
                      <span className="truncate text-sm font-semibold">{conn.name}</span>
                      <StatusPill value={conn.status} />
                    </div>
                    <div className="mt-1 flex flex-wrap gap-2 text-xs text-muted-foreground">
                      <span className="font-mono">{conn.provider}</span>
                      <span>{conn.authType}</span>
                      <span>{conn.lastTestStatus || 'untested'}</span>
                      {conn.webhookSecretConfigured && (
                        <span>{t('external_sync.connections.webhook_secret_configured')}</span>
                      )}
                    </div>
                    {conn.lastError && (
                      <div className="mt-2 line-clamp-2 text-xs text-destructive">
                        {conn.lastError}
                      </div>
                    )}
                  </div>
                </button>
                <div className="flex w-full flex-wrap items-center gap-1 sm:w-auto sm:shrink-0 sm:justify-end">
                  <Button
                    type="button"
                    size="icon-xs"
                    variant="ghost"
                    aria-label={t('common.edit')}
                    onClick={() => onEdit(conn)}
                    disabled={updatingID === conn.id}
                  >
                    {updatingID === conn.id ? (
                      <Loader2 className="size-3 animate-spin" />
                    ) : (
                      <Pencil className="size-3" />
                    )}
                  </Button>
                  <Button
                    type="button"
                    size="icon-xs"
                    variant="ghost"
                    aria-label={t('external_sync.actions.test')}
                    onClick={() => onTest(conn.id)}
                    disabled={testingID === conn.id}
                  >
                    {testingID === conn.id ? (
                      <Loader2 className="size-3 animate-spin" />
                    ) : (
                      <ShieldAlert className="size-3" />
                    )}
                  </Button>
                  <Button
                    type="button"
                    size="icon-xs"
                    variant="ghost"
                    aria-label={t('external_sync.actions.qualify')}
                    onClick={() => onQualify(conn.id)}
                    disabled={qualifyingID === conn.id}
                  >
                    {qualifyingID === conn.id ? (
                      <Loader2 className="size-3 animate-spin" />
                    ) : (
                      <FileSearch className="size-3" />
                    )}
                  </Button>
                  {(!conn.enabled || conn.status === 'quarantined') && (
                    <Button
                      type="button"
                      size="icon-xs"
                      variant="ghost"
                      aria-label={t('external_sync.actions.resume')}
                      onClick={() => onResume(conn.id)}
                      disabled={resumingID === conn.id}
                    >
                      {resumingID === conn.id ? (
                        <Loader2 className="size-3 animate-spin" />
                      ) : (
                        <RotateCcw className="size-3" />
                      )}
                    </Button>
                  )}
                  <Button
                    type="button"
                    size="icon-xs"
                    variant="ghost"
                    aria-label={t('external_sync.actions.run')}
                    onClick={() => onRun(conn.id, selectedMapping)}
                    disabled={requesting || selectedID !== conn.id}
                  >
                    <RefreshCcw className="size-3" />
                  </Button>
                  <Button
                    type="button"
                    size="icon-xs"
                    variant="ghost"
                    aria-label={t('common.delete')}
                    onClick={() => onDelete(conn.id)}
                    disabled={deletingID === conn.id}
                  >
                    {deletingID === conn.id ? (
                      <Loader2 className="size-3 animate-spin" />
                    ) : (
                      <Trash2 className="size-3" />
                    )}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function MappingsCard({
  connection,
  mappings,
  schemas,
  loading,
  schemaLoading,
  savingID,
  previewingID,
  resettingID,
  backfillingID,
  onSave,
  onPreview,
  onResetCursor,
  onBackfill,
}: {
  connection: ExternalConnection | null
  mappings: ExternalObjectMapping[]
  schemas: ExternalObjectSchema[]
  loading: boolean
  schemaLoading: boolean
  savingID?: string
  previewingID?: string
  resettingID?: string
  backfillingID?: string
  onSave: (mapping: ExternalObjectMapping) => void
  onPreview: (id: string, fieldMappingJson: string, statusMappingJson: string) => void
  onResetCursor: (id: string) => void
  onBackfill: (id: string, resetCursor: boolean) => void
}) {
  const { t } = useTranslation()
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('external_sync.mappings.title')}</CardTitle>
        <CardDescription>
          {connection
            ? t('external_sync.mappings.description', { name: connection.name })
            : t('external_sync.mappings.no_connection')}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3 pt-6">
        {connection && <SchemaSummary schemas={schemas} loading={schemaLoading} />}
        {loading ? (
          <Loading />
        ) : mappings.length === 0 ? (
          <EmptyState
            icon={GitBranch}
            title={t('external_sync.mappings.empty_title')}
            description={t('external_sync.mappings.empty_body')}
          />
        ) : (
          mappings.map((mapping) => (
            <MappingEditor
              key={mapping.id}
              mapping={mapping}
              schemas={schemas}
              pending={savingID === mapping.id}
              previewing={previewingID === mapping.id}
              resetting={resettingID === mapping.id}
              backfilling={backfillingID === mapping.id}
              onSave={onSave}
              onPreview={onPreview}
              onResetCursor={onResetCursor}
              onBackfill={onBackfill}
            />
          ))
        )}
      </CardContent>
    </Card>
  )
}

function SchemaSummary({
  schemas,
  loading,
}: {
  schemas: ExternalObjectSchema[]
  loading: boolean
}) {
  const { t } = useTranslation()
  if (loading) {
    return (
      <div className="rounded-lg border border-border/60 bg-muted/20 px-3 py-2">
        <Loading />
      </div>
    )
  }
  if (schemas.length === 0) return null

  return (
    <div className="space-y-2 rounded-lg border border-border/60 bg-muted/20 px-3 py-3">
      <div className="text-xs font-semibold tracking-[0.12em] text-muted-foreground uppercase">
        {t('external_sync.schemas.title')}
      </div>
      <div className="space-y-2">
        {schemas.map((schema) => (
          <div key={schema.type} className="min-w-0">
            <div className="font-mono text-xs font-semibold">{schema.type}</div>
            <div className="mt-1 flex flex-wrap gap-1.5">
              {schema.fields.map((field) => (
                <span
                  key={`${schema.type}-${field}`}
                  className="rounded border border-border/70 bg-background px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
                >
                  {field}
                </span>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

export function MappingEditor({
  mapping,
  schemas,
  pending,
  previewing,
  resetting,
  backfilling,
  onSave,
  onPreview,
  onResetCursor,
  onBackfill,
}: {
  mapping: ExternalObjectMapping
  schemas: ExternalObjectSchema[]
  pending: boolean
  previewing: boolean
  resetting: boolean
  backfilling: boolean
  onSave: (mapping: ExternalObjectMapping) => void
  onPreview: (id: string, fieldMappingJson: string, statusMappingJson: string) => void
  onResetCursor: (id: string) => void
  onBackfill: (id: string, resetCursor: boolean) => void
}) {
  const { t } = useTranslation()
  const [direction, setDirection] = useState(mapping.direction)
  const [enabled, setEnabled] = useState(mapping.enabled)
  const [fieldMapping, setFieldMapping] = useState(mapping.fieldMappingJson || '{}')
  const [statusMapping, setStatusMapping] = useState(mapping.statusMappingJson || '{}')
  const [conflictPolicy, setConflictPolicy] = useState(mapping.conflictPolicy || 'manual')
  const [tombstonePolicy, setTombstonePolicy] = useState(mapping.tombstonePolicy || 'mark_stale')
  const [backfillResetCursor, setBackfillResetCursor] = useState(false)

  useEffect(() => {
    setDirection(mapping.direction)
    setEnabled(mapping.enabled)
    setFieldMapping(mapping.fieldMappingJson || '{}')
    setStatusMapping(mapping.statusMappingJson || '{}')
    setConflictPolicy(mapping.conflictPolicy || 'manual')
    setTombstonePolicy(mapping.tombstonePolicy || 'mark_stale')
    setBackfillResetCursor(false)
  }, [mapping])

  const fieldMappingLabel = t('external_sync.mappings.field_mapping')
  const statusMappingLabel = t('external_sync.mappings.status_mapping')
  const directionLabelID = `mapping-direction-${mapping.id}`
  const fieldMappingResult = parseJSONRecord(fieldMapping)
  const statusMappingResult = parseJSONRecord(statusMapping)
  const fieldMappingError = fieldMappingResult.error
    ? t('external_sync.mappings.json_object_error', { label: fieldMappingLabel })
    : ''
  const statusMappingError = statusMappingResult.error
    ? t('external_sync.mappings.json_object_error', { label: statusMappingLabel })
    : ''
  const schema = schemas.find((item) => item.type === mapping.externalObjectType) ?? null
  const unknownFields =
    schema && fieldMappingResult.value
      ? unknownSchemaFields(fieldMappingResult.value, schema.fields)
      : []
  const schemaWarning =
    unknownFields.length > 0
      ? t('external_sync.mappings.schema_warning', { fields: unknownFields.join(', ') })
      : ''
  const fieldMappingDescription = [
    fieldMappingError ? `mapping-fields-error-${mapping.id}` : '',
    schemaWarning ? `mapping-fields-warning-${mapping.id}` : '',
  ]
    .filter(Boolean)
    .join(' ')
  const canSave = !pending && !fieldMappingError && !statusMappingError
  const canResetCursor = enabled && mappingAllowsPull(direction)
  const canBackfill = canResetCursor && !pending && !backfilling
  const canPreview = !pending && !previewing && !fieldMappingError && !statusMappingError

  const handleSave = () => {
    /* v8 ignore next -- @preserve: save is disabled while mapping JSON has validation errors. */
    if (fieldMappingError || statusMappingError) return
    onSave({
      ...mapping,
      direction,
      enabled,
      fieldMappingJson: normalizeJSONInput(fieldMapping),
      statusMappingJson: normalizeJSONInput(statusMapping),
      conflictPolicy,
      tombstonePolicy,
    })
  }

  return (
    <div className="rounded-lg border border-border/60 bg-background px-4 py-3">
      <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
        <div className="min-w-0">
          <div className="text-sm font-semibold">
            {mapping.localObjectType} → {mapping.externalObjectType}
          </div>
          <div className="mt-1 text-xs text-muted-foreground">
            {t('external_sync.mappings.version', { version: mapping.mappingVersion })}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Checkbox
            id={`mapping-enabled-${mapping.id}`}
            checked={enabled}
            onCheckedChange={(v) => setEnabled(Boolean(v))}
          />
          <Label
            htmlFor={`mapping-enabled-${mapping.id}`}
            className="text-xs text-muted-foreground"
          >
            {t('external_sync.mappings.enabled')}
          </Label>
        </div>
      </div>
      <div className="mt-4 grid gap-3 md:grid-cols-2">
        <div className="space-y-1.5">
          <Label id={directionLabelID}>{t('external_sync.mappings.direction')}</Label>
          <Select
            value={direction}
            onValueChange={(v) => setDirection(v as ExternalSyncDirection)}
            disabled={pending}
          >
            <SelectTrigger aria-labelledby={directionLabelID} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL}>
                {t('external_sync.direction.pull')}
              </SelectItem>
              <SelectItem value={ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PUSH}>
                {t('external_sync.direction.push')}
              </SelectItem>
              <SelectItem value={ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL}>
                {t('external_sync.direction.bidirectional')}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`mapping-conflict-${mapping.id}`}>
            {t('external_sync.mappings.conflict_policy')}
          </Label>
          <Input
            id={`mapping-conflict-${mapping.id}`}
            value={conflictPolicy}
            onChange={(e) => setConflictPolicy(e.target.value)}
            disabled={pending}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`mapping-fields-${mapping.id}`}>
            {t('external_sync.mappings.field_mapping')}
          </Label>
          <Input
            id={`mapping-fields-${mapping.id}`}
            value={fieldMapping}
            onChange={(e) => setFieldMapping(e.target.value)}
            disabled={pending}
            aria-invalid={Boolean(fieldMappingError)}
            aria-describedby={fieldMappingDescription || undefined}
          />
          {fieldMappingError && (
            <p id={`mapping-fields-error-${mapping.id}`} className="text-xs text-destructive">
              {fieldMappingError}
            </p>
          )}
          {schemaWarning && (
            <p
              id={`mapping-fields-warning-${mapping.id}`}
              className="text-xs text-amber-700 dark:text-amber-400"
            >
              {schemaWarning}
            </p>
          )}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`mapping-status-${mapping.id}`}>
            {t('external_sync.mappings.status_mapping')}
          </Label>
          <Input
            id={`mapping-status-${mapping.id}`}
            value={statusMapping}
            onChange={(e) => setStatusMapping(e.target.value)}
            disabled={pending}
            aria-invalid={Boolean(statusMappingError)}
            aria-describedby={statusMappingError ? `mapping-status-error-${mapping.id}` : undefined}
          />
          {statusMappingError && (
            <p id={`mapping-status-error-${mapping.id}`} className="text-xs text-destructive">
              {statusMappingError}
            </p>
          )}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`mapping-tombstone-${mapping.id}`}>
            {t('external_sync.mappings.tombstone_policy')}
          </Label>
          <Input
            id={`mapping-tombstone-${mapping.id}`}
            value={tombstonePolicy}
            onChange={(e) => setTombstonePolicy(e.target.value)}
            disabled={pending}
          />
        </div>
      </div>
      <div className="mt-4 flex flex-wrap justify-end gap-2">
        <div className="mr-auto flex items-center gap-2">
          <Checkbox
            id={`mapping-backfill-reset-${mapping.id}`}
            checked={backfillResetCursor}
            onCheckedChange={(v) => setBackfillResetCursor(Boolean(v))}
            disabled={!canResetCursor || backfilling}
          />
          <Label
            htmlFor={`mapping-backfill-reset-${mapping.id}`}
            className="text-xs text-muted-foreground"
          >
            {t('external_sync.mappings.backfill_reset_cursor')}
          </Label>
        </div>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={() =>
            onPreview(
              mapping.id,
              normalizeJSONInput(fieldMapping),
              normalizeJSONInput(statusMapping),
            )
          }
          disabled={!canPreview}
          aria-label={t('external_sync.mappings.preview_label', {
            object: mapping.externalObjectType,
          })}
        >
          {previewing ? (
            <Loader2 className="size-3 animate-spin" />
          ) : (
            <FileSearch className="size-3" />
          )}
          {t('external_sync.mappings.preview')}
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={() => onResetCursor(mapping.id)}
          disabled={pending || resetting || !canResetCursor}
          aria-label={t('external_sync.mappings.reset_cursor_label', {
            object: mapping.externalObjectType,
          })}
        >
          {resetting ? (
            <Loader2 className="size-3 animate-spin" />
          ) : (
            <RotateCcw className="size-3" />
          )}
          {t('external_sync.mappings.reset_cursor')}
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={() => onBackfill(mapping.id, backfillResetCursor)}
          disabled={!canBackfill}
          aria-label={t('external_sync.mappings.backfill_label', {
            object: mapping.externalObjectType,
          })}
        >
          {backfilling ? (
            <Loader2 className="size-3 animate-spin" />
          ) : (
            <RefreshCcw className="size-3" />
          )}
          {t('external_sync.mappings.backfill')}
        </Button>
        <Button size="sm" onClick={handleSave} disabled={!canSave}>
          {pending && <Loader2 className="size-3 animate-spin" />}
          {t('common.save')}
        </Button>
      </div>
    </div>
  )
}

function parseJSONRecord(raw: string): { value: Record<string, unknown> | null; error: boolean } {
  const value = raw.trim()
  if (!value) return { value: {}, error: false }
  try {
    const parsed: unknown = JSON.parse(value)
    if (!isRecord(parsed)) return { value: null, error: true }
    return { value: parsed, error: false }
  } catch {
    return { value: null, error: true }
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function normalizeJSONInput(raw: string) {
  return raw.trim() || '{}'
}

export function mappingAllowsPull(direction: ExternalSyncDirection) {
  return (
    direction === ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL ||
    direction === ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL
  )
}

export function unknownSchemaFields(mapping: Record<string, unknown>, schemaFields: string[]) {
  const known = new Set(schemaFields)
  return mappingFieldCandidates(mapping)
    .filter((field) => !known.has(field))
    .sort((a, b) => a.localeCompare(b))
}

function mappingFieldCandidates(mapping: Record<string, unknown>) {
  const valueCandidates = new Set<string>()
  for (const value of Object.values(mapping)) {
    collectStringCandidates(valueCandidates, value)
  }
  if (valueCandidates.size > 0) return Array.from(valueCandidates)

  const keyCandidates = new Set<string>()
  for (const key of Object.keys(mapping)) {
    addFieldCandidate(keyCandidates, key)
  }
  return Array.from(keyCandidates)
}

function collectStringCandidates(candidates: Set<string>, value: unknown) {
  if (typeof value === 'string') {
    addFieldCandidate(candidates, value)
    return
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      collectStringCandidates(candidates, item)
    }
    return
  }
  if (isRecord(value)) {
    for (const nestedValue of Object.values(value)) {
      collectStringCandidates(candidates, nestedValue)
    }
  }
}

function addFieldCandidate(candidates: Set<string>, value: string) {
  const field = value.trim()
  if (/^[A-Za-z_][A-Za-z0-9_.-]*$/.test(field)) {
    candidates.add(field)
  }
}

export function RunsCard({
  runs,
  loading,
  selectedID,
  retryingID,
  hasNextPage,
  loadingMore,
  onSelect,
  onRetry,
  onLoadMore,
}: {
  runs: ExternalSyncRun[]
  loading: boolean
  selectedID: string
  retryingID?: string
  hasNextPage: boolean
  loadingMore: boolean
  onSelect: (id: string) => void
  onRetry: (id: string) => void
  onLoadMore: () => void
}) {
  const { t } = useTranslation()
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('external_sync.runs.title')}</CardTitle>
        <CardDescription>{t('external_sync.runs.description')}</CardDescription>
      </CardHeader>
      <CardContent className="pt-6">
        {loading ? (
          <Loading />
        ) : runs.length === 0 ? (
          <EmptyState
            icon={RefreshCcw}
            title={t('external_sync.runs.empty_title')}
            description={t('external_sync.runs.empty_body')}
          />
        ) : (
          <div className="space-y-3">
            <div className="space-y-2">
              {runs.map((run) => (
                <div
                  key={run.id}
                  className={cn(
                    'flex w-full items-start justify-between gap-3 rounded-lg border px-3 py-3 transition-colors',
                    selectedID === run.id
                      ? 'border-primary/50 bg-primary/5'
                      : 'border-border/60 bg-background hover:bg-muted/50',
                  )}
                >
                  <button
                    type="button"
                    onClick={() => onSelect(run.id)}
                    className="min-w-0 flex-1 text-left"
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <StatusPill value={statusLabel(run.status)} />
                        <span className="text-xs text-muted-foreground">
                          {directionLabel(run.direction)}
                        </span>
                      </div>
                      <div className="mt-2 grid grid-cols-4 gap-2 text-xs tabular-nums text-muted-foreground">
                        <span>{t('external_sync.runs.seen', { value: run.recordsSeen })}</span>
                        <span>
                          {t('external_sync.runs.changed', { value: run.recordsChanged })}
                        </span>
                        <span>{t('external_sync.runs.failed', { value: run.recordsFailed })}</span>
                        <span>
                          {t('external_sync.runs.conflicts', { value: run.conflictsCreated })}
                        </span>
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        {formatDate(run.createdAt)}
                      </div>
                    </div>
                  </button>
                  {isRetryableRunStatus(run.status) && (
                    <Button
                      type="button"
                      size="icon-xs"
                      variant="ghost"
                      aria-label={t('common.retry')}
                      onClick={() => onRetry(run.id)}
                      disabled={retryingID === run.id}
                    >
                      {retryingID === run.id ? (
                        <Loader2 className="size-3 animate-spin" />
                      ) : (
                        <RotateCcw className="size-3" />
                      )}
                    </Button>
                  )}
                </div>
              ))}
            </div>
            {hasNextPage && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="w-full"
                onClick={onLoadMore}
                disabled={loadingMore}
              >
                {loadingMore && <Loader2 className="size-3 animate-spin" />}
                {t('external_sync.runs.load_more')}
              </Button>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

export function EventsCard({
  events,
  loading,
  selectedID,
  replayingID,
  hasNextPage,
  loadingMore,
  onSelect,
  onReplay,
  onLoadMore,
}: {
  events: ExternalSyncEvent[]
  loading: boolean
  selectedID: string
  replayingID?: string
  hasNextPage: boolean
  loadingMore: boolean
  onSelect: (id: string) => void
  onReplay: (id: string) => void
  onLoadMore: () => void
}) {
  const { t } = useTranslation()
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('external_sync.events.title')}</CardTitle>
        <CardDescription>{t('external_sync.events.description')}</CardDescription>
      </CardHeader>
      <CardContent className="pt-6">
        {loading ? (
          <Loading />
        ) : events.length === 0 ? (
          <EmptyState
            icon={GitBranch}
            title={t('external_sync.events.empty_title')}
            description={t('external_sync.events.empty_body')}
          />
        ) : (
          <div className="space-y-3">
            <div className="space-y-2">
              {events.map((event) => (
                <div
                  key={event.id}
                  className={cn(
                    'flex w-full items-start justify-between gap-3 rounded-lg border px-3 py-3 transition-colors',
                    selectedID === event.id
                      ? 'border-primary/50 bg-primary/5'
                      : 'border-border/60 bg-background hover:bg-muted/50',
                  )}
                >
                  <button
                    type="button"
                    className="min-w-0 flex-1 text-left"
                    aria-label={t('external_sync.events.select', { id: shortID(event.id) })}
                    onClick={() => onSelect(event.id)}
                  >
                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                      <span className="truncate text-sm font-medium">
                        {event.eventType || event.provider}
                      </span>
                      <StatusPill value={eventStatusLabel(event.status)} />
                      <StatusPill value={eventSignatureLabel(event.signatureStatus)} />
                    </div>
                    <div className="mt-1 flex flex-wrap gap-2 text-xs text-muted-foreground">
                      <span className="font-mono">{event.provider}</span>
                      {event.externalEventId && <span>{shortID(event.externalEventId)}</span>}
                      {event.runId && <span>{shortID(event.runId)}</span>}
                    </div>
                    <div className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
                      {event.payloadDigest}
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                      {formatDate(event.receivedAt)}
                    </div>
                    {event.failureReason && (
                      <div className="mt-1 line-clamp-2 text-xs text-destructive">
                        {event.failureReason}
                      </div>
                    )}
                  </button>
                  <Button
                    type="button"
                    size="xs"
                    variant="ghost"
                    onClick={() => onReplay(event.id)}
                    disabled={!isReplayableEvent(event) || replayingID === event.id}
                  >
                    {replayingID === event.id && <Loader2 className="size-3 animate-spin" />}
                    {t('external_sync.events.replay')}
                  </Button>
                </div>
              ))}
            </div>
            {hasNextPage && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="w-full"
                onClick={onLoadMore}
                disabled={loadingMore}
              >
                {loadingMore && <Loader2 className="size-3 animate-spin" />}
                {t('external_sync.events.load_more')}
              </Button>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

export function EventDetailCard({
  event,
  loading,
}: {
  event?: ExternalSyncEvent
  loading: boolean
}) {
  const { t } = useTranslation()
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('external_sync.event_detail.title')}</CardTitle>
        <CardDescription>
          {event
            ? t('external_sync.event_detail.description', { id: shortID(event.id) })
            : t('external_sync.event_detail.empty_body')}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 pt-6">
        {loading ? (
          <Loading />
        ) : !event ? (
          <EmptyState
            icon={GitBranch}
            title={t('external_sync.event_detail.empty_title')}
            description={t('external_sync.event_detail.empty_body')}
          />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <StatusPill value={eventStatusLabel(event.status)} />
              <StatusPill value={eventSignatureLabel(event.signatureStatus)} />
            </div>
            <DiagnosticRows
              rows={[
                { label: t('external_sync.event_detail.provider'), value: event.provider },
                { label: t('external_sync.event_detail.event_type'), value: event.eventType },
                {
                  label: t('external_sync.event_detail.external_event_id'),
                  value: event.externalEventId,
                },
                { label: t('external_sync.event_detail.dedupe_key'), value: event.dedupeKey },
                { label: t('external_sync.event_detail.run_id'), value: event.runId },
                {
                  label: t('external_sync.event_detail.payload_digest'),
                  value: event.payloadDigest,
                },
                {
                  label: t('external_sync.event_detail.received_at'),
                  value: formatDate(event.receivedAt),
                },
                {
                  label: t('external_sync.event_detail.replayed_at'),
                  value: formatDate(event.replayedAt),
                },
                { label: t('external_sync.event_detail.replayed_by'), value: event.replayedBy },
              ]}
            />
            {event.failureReason && (
              <div className="rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                {event.failureReason}
              </div>
            )}
            <JsonBlock
              label={t('external_sync.event_detail.normalized_payload')}
              value={event.normalizedPayloadJson}
            />
          </>
        )}
      </CardContent>
    </Card>
  )
}

export function RunDetailCard({
  detail,
  loading,
  retryingFailureID,
  resolvingConflictID,
  batchResolving,
  timelineEntries,
  timelineLoading,
  timelineTarget,
  onRetryFailure,
  onShowTimeline,
  onResolveConflict,
  onBatchResolveConflicts,
}: {
  detail?: ExternalSyncRunDetail
  loading: boolean
  retryingFailureID?: string
  resolvingConflictID?: string
  batchResolving: boolean
  timelineEntries: ExternalSyncRecordTimelineEntry[]
  timelineLoading: boolean
  timelineTarget: RecordTimelineTarget | null
  onRetryFailure: (id: string) => void
  onShowTimeline: (target: RecordTimelineTarget) => void
  onResolveConflict: (id: string, resolution: ExternalSyncConflictResolution) => void
  onBatchResolveConflicts: (ids: string[], resolution: ExternalSyncConflictResolution) => void
}) {
  const { t } = useTranslation()
  const run = detail?.run
  const openConflictIDs =
    detail?.conflicts
      .filter((conflict) => conflict.status === 'open')
      .map((conflict) => conflict.id) ?? []
  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('external_sync.detail.title')}</CardTitle>
        <CardDescription>
          {run
            ? t('external_sync.detail.description', { id: shortID(run.id) })
            : t('external_sync.detail.empty_body')}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5 pt-6">
        {loading ? (
          <Loading />
        ) : !detail || !run ? (
          <EmptyState
            icon={RefreshCcw}
            title={t('external_sync.detail.empty_title')}
            description={t('external_sync.detail.empty_body')}
          />
        ) : (
          <>
            <div className="grid gap-2 text-xs sm:grid-cols-3">
              <JsonBlock
                label={t('external_sync.detail.cursor_before')}
                value={run.cursorBeforeJson}
              />
              <JsonBlock
                label={t('external_sync.detail.cursor_after')}
                value={run.cursorAfterJson}
              />
              <JsonBlock
                label={t('external_sync.detail.input_metadata')}
                value={run.inputMetadataJson}
              />
            </div>

            <DetailSection title={t('external_sync.detail.attempts')}>
              {detail.attempts.length === 0 ? (
                <MutedLine>{t('external_sync.detail.none')}</MutedLine>
              ) : (
                detail.attempts.map((attempt) => (
                  <div
                    key={attempt.id}
                    className="rounded-lg border border-border/60 bg-background px-3 py-2 text-xs"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium">{attempt.result}</span>
                      <span className="text-muted-foreground">
                        #{attempt.attemptNumber} · {formatDate(attempt.startedAt)}
                      </span>
                    </div>
                    {attempt.errorMessage && (
                      <div className="mt-1 text-destructive">{attempt.errorMessage}</div>
                    )}
                    <DiagnosticRows
                      rows={[
                        {
                          label: t('external_sync.detail.http_status'),
                          value: attempt.httpStatus > 0 ? String(attempt.httpStatus) : '',
                        },
                        {
                          label: t('external_sync.detail.provider_request_id'),
                          value: attempt.providerRequestId,
                        },
                        {
                          label: t('external_sync.detail.retry_after'),
                          value: formatDate(attempt.retryAfter),
                        },
                        {
                          label: t('external_sync.detail.error_kind'),
                          value: attempt.errorKind,
                        },
                      ]}
                    />
                  </div>
                ))
              )}
            </DetailSection>

            <DetailSection title={t('external_sync.detail.failures')}>
              {detail.failures.length === 0 ? (
                <MutedLine>{t('external_sync.detail.none')}</MutedLine>
              ) : (
                detail.failures.map((failure) => (
                  <div
                    key={failure.id}
                    className="rounded-lg border border-border/60 bg-background px-3 py-2 text-xs"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium">
                        {failure.externalKey || failure.operation}
                      </span>
                      <div className="flex items-center gap-1">
                        <Button
                          type="button"
                          size="xs"
                          variant="ghost"
                          onClick={() => onShowTimeline(recordTimelineTargetFromFailure(failure))}
                          disabled={
                            !canShowRecordTimeline(failure.localObjectId, failure.externalKey)
                          }
                        >
                          <FileSearch className="size-3" />
                          {t('external_sync.detail.timeline')}
                        </Button>
                        <Button
                          type="button"
                          size="xs"
                          variant="ghost"
                          onClick={() => onRetryFailure(failure.id)}
                          disabled={!failure.retryable || retryingFailureID === failure.id}
                        >
                          {retryingFailureID === failure.id && (
                            <Loader2 className="size-3 animate-spin" />
                          )}
                          {t('common.retry')}
                        </Button>
                      </div>
                    </div>
                    <div className="mt-1 text-muted-foreground">{failure.failureKind}</div>
                    {failure.message && (
                      <div className="mt-1 text-destructive">{failure.message}</div>
                    )}
                    <DiagnosticRows
                      rows={[
                        {
                          label: t('external_sync.detail.operation'),
                          value: failure.operation,
                        },
                        {
                          label: t('external_sync.detail.local_object_id'),
                          value: failure.localObjectId,
                        },
                        {
                          label: t('external_sync.detail.payload_digest'),
                          value: failure.payloadDigest,
                        },
                        {
                          label: t('external_sync.detail.retry_mode'),
                          value: failure.retryMode,
                        },
                        {
                          label: t('external_sync.detail.resolved_by'),
                          value: failure.resolvedBy,
                        },
                      ]}
                    />
                    {failure.normalizedPayloadJson && (
                      <div className="mt-2">
                        <JsonBlock
                          label={t('external_sync.detail.normalized_payload')}
                          value={failure.normalizedPayloadJson}
                        />
                      </div>
                    )}
                  </div>
                ))
              )}
            </DetailSection>

            <DetailSection title={t('external_sync.detail.conflicts')}>
              {detail.conflicts.length === 0 ? (
                <MutedLine>{t('external_sync.detail.none')}</MutedLine>
              ) : (
                <div className="space-y-2">
                  {openConflictIDs.length > 1 && (
                    <BatchConflictResolutionControls
                      conflictCount={openConflictIDs.length}
                      pending={batchResolving}
                      onResolve={(resolution) =>
                        onBatchResolveConflicts(openConflictIDs, resolution)
                      }
                    />
                  )}
                  {detail.conflicts.map((conflict) => (
                    <div
                      key={conflict.id}
                      className="rounded-lg border border-border/60 bg-background px-3 py-2 text-xs"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="font-medium">
                          {conflict.externalKey || conflict.conflictKind}
                        </span>
                        <div className="flex items-center gap-1">
                          <Button
                            type="button"
                            size="xs"
                            variant="ghost"
                            onClick={() =>
                              onShowTimeline(recordTimelineTargetFromConflict(conflict))
                            }
                            disabled={
                              !canShowRecordTimeline(conflict.localObjectId, conflict.externalKey)
                            }
                          >
                            <FileSearch className="size-3" />
                            {t('external_sync.detail.timeline')}
                          </Button>
                          <ConflictResolutionControls
                            conflictID={conflict.id}
                            status={conflict.status}
                            pending={resolvingConflictID === conflict.id}
                            onResolve={(resolution) => onResolveConflict(conflict.id, resolution)}
                          />
                        </div>
                      </div>
                      <div className="mt-1 text-muted-foreground">
                        {conflict.conflictKind} · {conflict.status}
                      </div>
                      <DiagnosticRows
                        rows={[
                          {
                            label: t('external_sync.detail.local_object_id'),
                            value: conflict.localObjectId,
                          },
                          {
                            label: t('external_sync.detail.resolution'),
                            value: conflict.resolution,
                          },
                          {
                            label: t('external_sync.detail.resolved_by'),
                            value: conflict.resolvedBy,
                          },
                        ]}
                      />
                      {(conflict.localSnapshotJson || conflict.externalSnapshotJson) && (
                        <div className="mt-2 grid gap-2 sm:grid-cols-2">
                          {conflict.localSnapshotJson && (
                            <JsonBlock
                              label={t('external_sync.detail.local_snapshot')}
                              value={conflict.localSnapshotJson}
                            />
                          )}
                          {conflict.externalSnapshotJson && (
                            <JsonBlock
                              label={t('external_sync.detail.external_snapshot')}
                              value={conflict.externalSnapshotJson}
                            />
                          )}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </DetailSection>

            {timelineTarget && (
              <RecordTimelinePanel
                target={timelineTarget}
                entries={timelineEntries}
                loading={timelineLoading}
              />
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

export function RecordTimelinePanel({
  target,
  entries,
  loading,
}: {
  target: RecordTimelineTarget
  entries: ExternalSyncRecordTimelineEntry[]
  loading: boolean
}) {
  const { t } = useTranslation()
  return (
    <DetailSection title={t('external_sync.detail.timeline_title', { record: target.label })}>
      {loading ? (
        <Loading />
      ) : entries.length === 0 ? (
        <MutedLine>{t('external_sync.detail.timeline_empty')}</MutedLine>
      ) : (
        <div className="space-y-2">
          {entries.map((entry) => (
            <div
              key={`${entry.kind}-${entry.occurredAt}-${entry.runId}-${entry.summary}`}
              className="rounded-lg border border-border/60 bg-muted/20 px-3 py-2 text-xs"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="font-medium">{entry.summary || entry.kind}</span>
                <span className="text-muted-foreground">{formatDate(entry.occurredAt)}</span>
              </div>
              <DiagnosticRows
                rows={[
                  { label: t('external_sync.detail.kind'), value: entry.kind },
                  { label: t('external_sync.detail.status'), value: entry.status },
                  { label: t('external_sync.detail.operation'), value: entry.operation },
                  { label: t('external_sync.detail.run_id'), value: shortID(entry.runId) },
                  {
                    label: t('external_sync.detail.local_object_id'),
                    value: entry.localObjectId,
                  },
                  { label: t('external_sync.detail.external_key'), value: entry.externalKey },
                ]}
              />
              {entry.detailJson && (
                <div className="mt-2">
                  <JsonBlock
                    label={t('external_sync.detail.timeline_detail')}
                    value={entry.detailJson}
                  />
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </DetailSection>
  )
}

export function recordTimelineTargetFromFailure(
  failure: ExternalSyncRecordFailure,
): RecordTimelineTarget {
  return {
    mappingId: failure.mappingId,
    localObjectId: failure.localObjectId,
    externalKey: failure.externalKey,
    label: failure.externalKey || failure.localObjectId || shortID(failure.id),
  }
}

export function recordTimelineTargetFromConflict(
  conflict: ExternalSyncConflict,
): RecordTimelineTarget {
  return {
    mappingId: conflict.mappingId,
    localObjectId: conflict.localObjectId,
    externalKey: conflict.externalKey,
    label: conflict.externalKey || conflict.localObjectId || shortID(conflict.id),
  }
}

export function canShowRecordTimeline(localObjectId: string, externalKey: string) {
  return localObjectId.length > 0 || externalKey.length > 0
}

export function CreateConnectionDialog({
  open,
  pending,
  providers = [],
  selectedInstallation,
  selectedInstallationResources = [],
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  pending: boolean
  providers?: ExternalSyncProvider[]
  selectedInstallation?: ExternalProviderInstallation | null
  selectedInstallationResources?: ExternalProviderInstallationResource[]
  onOpenChange: (open: boolean) => void
  onSubmit: (body: CreateExternalConnectionRequest) => void
}) {
  const { t } = useTranslation()
  const [provider, setProvider] = useState('github')
  const [name, setName] = useState('')
  const [authType, setAuthType] = useState('token')
  const [credential, setCredential] = useState('')
  const [webhookSecret, setWebhookSecret] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [providerConfig, setProviderConfig] = useState('{}')
  const [scopes, setScopes] = useState('issues')
  const [enabled, setEnabled] = useState(true)
  const [providerInstallationID, setProviderInstallationID] = useState('')
  const hasProviders = providers.length > 0
  const defaultProvider = providers[0]?.provider ?? 'github'

  useEffect(() => {
    if (!hasProviders) return
    setProvider((current) =>
      providers.some((entry) => entry.provider === current) ? current : defaultProvider,
    )
  }, [defaultProvider, hasProviders, providers])

  useEffect(() => {
    if (!open) return
    if (!selectedInstallation) {
      setProviderInstallationID('')
      return
    }
    const derivedConfig = providerConfigFromInstallationResources(selectedInstallationResources)
    setProviderInstallationID(selectedInstallation.id)
    setProvider(selectedInstallation.provider)
    setName((current) => current || selectedInstallation.displayName)
    setBaseURL((current) => current || selectedInstallation.baseUrl)
    if (derivedConfig) setProviderConfig(derivedConfig)
  }, [open, selectedInstallation, selectedInstallationResources])

  const reset = () => {
    setProvider(defaultProvider)
    setName('')
    setAuthType('token')
    setCredential('')
    setWebhookSecret('')
    setBaseURL('')
    setProviderConfig('{}')
    setScopes('issues')
    setEnabled(true)
    setProviderInstallationID(selectedInstallation?.id ?? '')
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!provider.trim() || !name.trim() || !credential.trim()) return
    onSubmit({
      provider: provider.trim().toLowerCase(),
      name: name.trim(),
      authType,
      credential: credential.trim(),
      webhookSecret: webhookSecret.trim(),
      baseUrl: baseURL.trim(),
      providerConfigJson: providerConfig.trim() || '{}',
      scopes: parseScopes(scopes),
      enabled,
      providerInstallationId: providerInstallationID,
    })
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) reset()
      }}
    >
      <DialogContent className="sm:max-w-xl" aria-describedby={undefined}>
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{t('external_sync.create.title')}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-4 py-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-provider">{t('external_sync.create.provider')}</Label>
              {hasProviders ? (
                <Select
                  value={provider}
                  onValueChange={(value) => {
                    setProvider(value)
                    if (
                      providerInstallationID &&
                      selectedInstallation &&
                      value !== selectedInstallation.provider
                    ) {
                      setProviderInstallationID('')
                      setBaseURL('')
                      setProviderConfig('{}')
                    }
                  }}
                  disabled={pending}
                >
                  <SelectTrigger id="external-sync-provider" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {providers.map((entry) => (
                      <SelectItem key={entry.provider} value={entry.provider}>
                        {entry.display}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  id="external-sync-provider"
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                  disabled={pending}
                  required
                />
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-name">{t('external_sync.create.name')}</Label>
              <Input
                id="external-sync-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={pending}
                required
              />
            </div>
            {selectedInstallation && (
              <div className="space-y-1.5 sm:col-span-2">
                <Label htmlFor="external-sync-provider-installation">
                  {t('external_sync.create.provider_installation')}
                </Label>
                <Select
                  value={providerInstallationID || 'none'}
                  onValueChange={(value) => {
                    const next = value === 'none' ? '' : value
                    setProviderInstallationID(next)
                    if (!next) return
                    const derivedConfig = providerConfigFromInstallationResources(
                      selectedInstallationResources,
                    )
                    setProvider(selectedInstallation.provider)
                    setName((current) => current || selectedInstallation.displayName)
                    setBaseURL((current) => current || selectedInstallation.baseUrl)
                    if (derivedConfig) setProviderConfig(derivedConfig)
                  }}
                  disabled={pending}
                >
                  <SelectTrigger id="external-sync-provider-installation" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">
                      {t('external_sync.create.provider_installation_none')}
                    </SelectItem>
                    <SelectItem value={selectedInstallation.id}>
                      {t('external_sync.create.provider_installation_current', {
                        name: selectedInstallation.displayName,
                      })}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
            )}
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-auth-type">{t('external_sync.create.auth_type')}</Label>
              <Select value={authType} onValueChange={setAuthType} disabled={pending}>
                <SelectTrigger id="external-sync-auth-type" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="token">token</SelectItem>
                  <SelectItem value="api_key">api_key</SelectItem>
                  <SelectItem value="oauth">oauth</SelectItem>
                  <SelectItem value="basic">basic</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-base-url">{t('external_sync.create.base_url')}</Label>
              <Input
                id="external-sync-base-url"
                value={baseURL}
                onChange={(e) => setBaseURL(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="external-sync-credential">
                {t('external_sync.create.credential')}
              </Label>
              <Input
                id="external-sync-credential"
                type="password"
                value={credential}
                onChange={(e) => setCredential(e.target.value)}
                disabled={pending}
                required
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="external-sync-webhook-secret">
                {t('external_sync.create.webhook_secret')}
              </Label>
              <Input
                id="external-sync-webhook-secret"
                type="password"
                value={webhookSecret}
                onChange={(e) => setWebhookSecret(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-config">{t('external_sync.create.config')}</Label>
              <Input
                id="external-sync-config"
                value={providerConfig}
                onChange={(e) => setProviderConfig(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-scopes">{t('external_sync.create.scopes')}</Label>
              <Input
                id="external-sync-scopes"
                value={scopes}
                onChange={(e) => setScopes(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="flex items-center gap-2 sm:col-span-2">
              <Checkbox
                id="external-sync-enabled"
                checked={enabled}
                onCheckedChange={(v) => setEnabled(Boolean(v))}
              />
              <Label htmlFor="external-sync-enabled" className="text-sm">
                {t('external_sync.create.enabled')}
              </Label>
            </div>
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
              disabled={pending || !provider.trim() || !name.trim() || !credential.trim()}
            >
              {pending && <Loader2 className="size-3 animate-spin" />}
              {t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function CreateProviderInstallationDialog({
  open,
  pending,
  providers = [],
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  pending: boolean
  providers?: ExternalSyncProvider[]
  onOpenChange: (open: boolean) => void
  onSubmit: (body: CreateExternalProviderInstallationRequest) => void
}) {
  const { t } = useTranslation()
  const [provider, setProvider] = useState('github')
  const [displayName, setDisplayName] = useState('')
  const [installationKind, setInstallationKind] = useState('github_app')
  const [externalInstallationID, setExternalInstallationID] = useState('')
  const [accountLogin, setAccountLogin] = useState('')
  const [permissions, setPermissions] = useState('{"metadata":"read","issues":"write"}')
  const [resourceKey, setResourceKey] = useState('')
  const [resourceName, setResourceName] = useState('')
  const [resourceURL, setResourceURL] = useState('')
  const hasProviders = providers.length > 0
  const defaultProvider = providers[0]?.provider ?? 'github'
  const permissionsError = parseJSONRecord(permissions).error

  useEffect(() => {
    if (!hasProviders) return
    setProvider((current) =>
      providers.some((entry) => entry.provider === current) ? current : defaultProvider,
    )
  }, [defaultProvider, hasProviders, providers])

  const reset = () => {
    setProvider(defaultProvider)
    setDisplayName('')
    setInstallationKind('github_app')
    setExternalInstallationID('')
    setAccountLogin('')
    setPermissions('{"metadata":"read","issues":"write"}')
    setResourceKey('')
    setResourceName('')
    setResourceURL('')
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!provider.trim() || !displayName.trim() || permissionsError) return
    const resource = resourceKey.trim()
      ? [
          {
            resourceType: 'repository',
            externalResourceId: '',
            resourceKey: resourceKey.trim(),
            displayName: resourceName.trim() || resourceKey.trim(),
            htmlUrl: resourceURL.trim(),
            selected: true,
            status: 'active',
            permissionsJson: '{}',
          },
        ]
      : []
    onSubmit({
      provider: provider.trim().toLowerCase(),
      displayName: displayName.trim(),
      installationKind,
      externalInstallationId: externalInstallationID.trim(),
      accountLogin: accountLogin.trim(),
      accountId: '',
      accountUrl: '',
      baseUrl: '',
      permissionsJson: normalizeJSONInput(permissions),
      capabilityProfileJson: '{}',
      resourceSelection: resource.length > 0 ? 'selected' : 'none',
      resources: resource,
    })
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) reset()
      }}
    >
      <DialogContent className="sm:max-w-xl" aria-describedby={undefined}>
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{t('external_sync.installation_create.title')}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-4 py-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-install-provider">
                {t('external_sync.create.provider')}
              </Label>
              {hasProviders ? (
                <Select value={provider} onValueChange={setProvider} disabled={pending}>
                  <SelectTrigger id="external-sync-install-provider" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {providers.map((entry) => (
                      <SelectItem key={entry.provider} value={entry.provider}>
                        {entry.display}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  id="external-sync-install-provider"
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                  disabled={pending}
                  required
                />
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-install-kind">
                {t('external_sync.installation_create.kind')}
              </Label>
              <Select
                value={installationKind}
                onValueChange={setInstallationKind}
                disabled={pending}
              >
                <SelectTrigger id="external-sync-install-kind" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="github_app">github_app</SelectItem>
                  <SelectItem value="oauth_app">oauth_app</SelectItem>
                  <SelectItem value="token">token</SelectItem>
                  <SelectItem value="manual">manual</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-install-name">
                {t('external_sync.installation_create.display_name')}
              </Label>
              <Input
                id="external-sync-install-name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                disabled={pending}
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-install-id">
                {t('external_sync.installation_create.external_id')}
              </Label>
              <Input
                id="external-sync-install-id"
                value={externalInstallationID}
                onChange={(e) => setExternalInstallationID(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="external-sync-install-account">
                {t('external_sync.installation_create.account')}
              </Label>
              <Input
                id="external-sync-install-account"
                value={accountLogin}
                onChange={(e) => setAccountLogin(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="external-sync-install-permissions">
                {t('external_sync.installation_create.permissions')}
              </Label>
              <Input
                id="external-sync-install-permissions"
                value={permissions}
                onChange={(e) => setPermissions(e.target.value)}
                disabled={pending}
                aria-invalid={permissionsError}
              />
              {permissionsError && (
                <p className="text-xs text-destructive">
                  {t('external_sync.mappings.json_object_error', {
                    label: t('external_sync.installation_create.permissions'),
                  })}
                </p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-install-resource-key">
                {t('external_sync.installation_create.resource_key')}
              </Label>
              <Input
                id="external-sync-install-resource-key"
                value={resourceKey}
                onChange={(e) => setResourceKey(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-install-resource-name">
                {t('external_sync.installation_create.resource_name')}
              </Label>
              <Input
                id="external-sync-install-resource-name"
                value={resourceName}
                onChange={(e) => setResourceName(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="external-sync-install-resource-url">
                {t('external_sync.installation_create.resource_url')}
              </Label>
              <Input
                id="external-sync-install-resource-url"
                value={resourceURL}
                onChange={(e) => setResourceURL(e.target.value)}
                disabled={pending}
              />
            </div>
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
              disabled={pending || !provider.trim() || !displayName.trim() || permissionsError}
            >
              {pending && <Loader2 className="size-3 animate-spin" />}
              {t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function EditConnectionDialog({
  open,
  connection,
  pending,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  connection: ExternalConnection | null
  pending: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (body: UpdateExternalConnectionRequest) => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [credential, setCredential] = useState('')
  const [webhookSecret, setWebhookSecret] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [providerConfig, setProviderConfig] = useState('{}')
  const [scopes, setScopes] = useState('')
  const [enabled, setEnabled] = useState(true)

  useEffect(() => {
    if (!connection) return
    setName(connection.name)
    setCredential('')
    setWebhookSecret('')
    setBaseURL(connection.baseUrl)
    setProviderConfig(connection.providerConfigJson || '{}')
    setScopes(connection.scopes.join(', '))
    setEnabled(connection.enabled)
  }, [connection])

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!connection || !name.trim()) return

    const body: UpdateExternalConnectionRequest = {
      id: connection.id,
      name: name.trim(),
      enabled,
      baseUrl: baseURL.trim(),
      providerConfigJson: providerConfig.trim() || '{}',
      scopes: parseScopes(scopes),
    }
    const nextCredential = credential.trim()
    const nextWebhookSecret = webhookSecret.trim()
    if (nextCredential) body.credential = nextCredential
    if (nextWebhookSecret) body.webhookSecret = nextWebhookSecret

    onSubmit(body)
  }

  if (!connection) return null

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl" aria-describedby={undefined}>
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{t('external_sync.edit.title')}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-4 py-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-edit-provider">
                {t('external_sync.create.provider')}
              </Label>
              <Input
                id="external-sync-edit-provider"
                value={connection.provider}
                disabled
                readOnly
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-edit-name">{t('external_sync.create.name')}</Label>
              <Input
                id="external-sync-edit-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={pending}
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-edit-auth-type">
                {t('external_sync.create.auth_type')}
              </Label>
              <Input
                id="external-sync-edit-auth-type"
                value={connection.authType}
                disabled
                readOnly
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-edit-base-url">
                {t('external_sync.create.base_url')}
              </Label>
              <Input
                id="external-sync-edit-base-url"
                value={baseURL}
                onChange={(e) => setBaseURL(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="external-sync-edit-credential">
                {t('external_sync.edit.credential')}
              </Label>
              <Input
                id="external-sync-edit-credential"
                type="password"
                value={credential}
                onChange={(e) => setCredential(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="external-sync-edit-webhook-secret">
                {t('external_sync.edit.webhook_secret')}
              </Label>
              <Input
                id="external-sync-edit-webhook-secret"
                type="password"
                value={webhookSecret}
                onChange={(e) => setWebhookSecret(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-edit-config">{t('external_sync.create.config')}</Label>
              <Input
                id="external-sync-edit-config"
                value={providerConfig}
                onChange={(e) => setProviderConfig(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="external-sync-edit-scopes">{t('external_sync.create.scopes')}</Label>
              <Input
                id="external-sync-edit-scopes"
                value={scopes}
                onChange={(e) => setScopes(e.target.value)}
                disabled={pending}
              />
            </div>
            <div className="flex items-center gap-2 sm:col-span-2">
              <Checkbox
                id="external-sync-edit-enabled"
                checked={enabled}
                onCheckedChange={(v) => setEnabled(Boolean(v))}
              />
              <Label htmlFor="external-sync-edit-enabled" className="text-sm">
                {t('external_sync.create.enabled')}
              </Label>
            </div>
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
              {pending && <Loader2 className="size-3 animate-spin" />}
              {t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function parseScopes(value: string) {
  return value
    .split(',')
    .map((scope) => scope.trim())
    .filter(Boolean)
}

export function providerConfigFromInstallationResources(
  resources: ExternalProviderInstallationResource[],
) {
  const selected = resources.filter(
    (resource) =>
      resource.selected && resource.status === 'active' && resource.resourceType === 'repository',
  )
  if (selected.length !== 1) return ''
  const [owner, repo, ...extra] = selected[0].resourceKey.split('/')
  if (!owner || !repo || extra.length > 0) return ''
  return JSON.stringify({ owner, repo: repo.replace(/\.git$/, '') })
}

const conflictResolutionOptions = [
  ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS,
  ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_LOCAL_WINS,
  ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_MANUAL_MERGE,
  ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_IGNORED,
]

export function BatchConflictResolutionControls({
  conflictCount,
  pending,
  onResolve,
}: {
  conflictCount: number
  pending: boolean
  onResolve: (resolution: ExternalSyncConflictResolution) => void
}) {
  const { t } = useTranslation()
  const [resolution, setResolution] = useState(
    ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS,
  )

  return (
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border/60 bg-muted/20 px-3 py-2 text-xs">
      <span className="font-medium">
        {t('external_sync.conflict_resolution.batch_label', { count: conflictCount })}
      </span>
      <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
        <Select
          value={resolution}
          onValueChange={(value) => setResolution(value as ExternalSyncConflictResolution)}
          disabled={pending}
        >
          <SelectTrigger
            size="sm"
            aria-label={t('external_sync.conflict_resolution.batch_label', {
              count: conflictCount,
            })}
            className="w-[8.75rem] max-w-full text-xs"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {conflictResolutionOptions.map((option) => (
              <SelectItem key={option} value={option}>
                {t(conflictResolutionLabelKey(option))}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          type="button"
          size="xs"
          variant="ghost"
          onClick={() => onResolve(resolution)}
          disabled={pending}
        >
          {pending && <Loader2 className="size-3 animate-spin" />}
          {t('external_sync.actions.resolve_conflicts')}
        </Button>
      </div>
    </div>
  )
}

export function ConflictResolutionControls({
  conflictID,
  status,
  pending,
  onResolve,
}: {
  conflictID: string
  status: string
  pending: boolean
  onResolve: (resolution: ExternalSyncConflictResolution) => void
}) {
  const { t } = useTranslation()
  const [resolution, setResolution] = useState(
    ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS,
  )
  const disabled = status !== 'open' || pending
  const labelID = `conflict-resolution-label-${conflictID}`

  return (
    <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
      <Label id={labelID} className="sr-only">
        {t('external_sync.conflict_resolution.label')}
      </Label>
      <Select
        value={resolution}
        onValueChange={(value) => setResolution(value as ExternalSyncConflictResolution)}
        disabled={disabled}
      >
        <SelectTrigger
          size="sm"
          aria-labelledby={labelID}
          className="w-[8.75rem] max-w-full text-xs"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {conflictResolutionOptions.map((option) => (
            <SelectItem key={option} value={option}>
              {t(conflictResolutionLabelKey(option))}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button
        type="button"
        size="xs"
        variant="ghost"
        onClick={() => onResolve(resolution)}
        disabled={disabled}
      >
        {pending && <Loader2 className="size-3 animate-spin" />}
        {t('external_sync.actions.resolve_conflict')}
      </Button>
    </div>
  )
}

function conflictResolutionLabelKey(resolution: ExternalSyncConflictResolution) {
  switch (resolution) {
    case ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_LOCAL_WINS:
      return 'external_sync.conflict_resolution.local_wins'
    case ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_MANUAL_MERGE:
      return 'external_sync.conflict_resolution.manual_merge'
    case ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_IGNORED:
      return 'external_sync.conflict_resolution.ignored'
    default:
      return 'external_sync.conflict_resolution.external_wins'
  }
}

function DetailSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-2">
      <h2 className="text-xs font-semibold tracking-[0.12em] text-muted-foreground uppercase">
        {title}
      </h2>
      {children}
    </section>
  )
}

function JsonBlock({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-lg border border-border/60 bg-muted/25 px-3 py-2">
      <div className="text-[11px] font-semibold text-muted-foreground">{label}</div>
      <pre className="mt-1 max-h-24 overflow-auto text-xs whitespace-pre-wrap">
        {prettyJSON(value)}
      </pre>
    </div>
  )
}

type DiagnosticRow = {
  label: string
  value: string
}

export function DiagnosticRows({ rows }: { rows: DiagnosticRow[] }) {
  const visibleRows = rows.filter((row) => row.value.trim())
  if (visibleRows.length === 0) return null

  return (
    <dl className="mt-2 grid gap-x-3 gap-y-1 text-[11px] sm:grid-cols-2">
      {visibleRows.map((row) => (
        <div key={`${row.label}-${row.value}`} className="min-w-0">
          <dt className="text-muted-foreground">{row.label}</dt>
          <dd className="truncate font-mono">{row.value}</dd>
        </div>
      ))}
    </dl>
  )
}

function MutedLine({ children }: { children: React.ReactNode }) {
  return <div className="text-sm text-muted-foreground">{children}</div>
}

export function StatusPill({ value }: { value: string }) {
  const urgent = [
    'failed',
    'dead',
    'deleted',
    'EXTERNAL_SYNC_RUN_STATUS_FAILED',
    'EXTERNAL_SYNC_RUN_STATUS_DEAD',
  ]
  const active = [...activeRunStatuses, 'received']
  return (
    <span
      className={cn(
        'inline-flex max-w-36 items-center rounded-full border px-2 py-0.5 text-[11px] font-medium',
        urgent.includes(value)
          ? 'border-destructive/20 bg-destructive/10 text-destructive'
          : active.includes(value)
            ? 'border-amber-300/40 bg-amber-100/60 text-amber-800 dark:bg-amber-900/20 dark:text-amber-200'
            : 'border-border bg-muted/50 text-muted-foreground',
      )}
    >
      <span className="truncate">{value}</span>
    </span>
  )
}

export function isActiveRun(run: ExternalSyncRun) {
  return run.inFlight || activeRunStatuses.includes(run.status)
}

export function statusLabel(value: string) {
  return value.replace('EXTERNAL_SYNC_RUN_STATUS_', '').toLowerCase()
}

export function eventStatusLabel(value: string) {
  return value.replace('EXTERNAL_SYNC_EVENT_STATUS_', '').toLowerCase()
}

export function eventSignatureLabel(value: string) {
  return value.replace('EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_', '').toLowerCase()
}

export function isRetryableRunStatus(value: string) {
  const status = statusLabel(value)
  return status === 'failed' || status === 'dead'
}

export function isReplayableEvent(event: ExternalSyncEvent) {
  const status = eventStatusLabel(event.status)
  const signature = eventSignatureLabel(event.signatureStatus)
  return status === 'received' && (signature === 'verified' || signature === 'not_required')
}

export function directionLabel(value: string) {
  return value.replace('EXTERNAL_SYNC_DIRECTION_', '').toLowerCase()
}

export function formatDate(value: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export function capabilityGrade(profileJson: string) {
  const raw = profileJson.trim()
  if (!raw) return ''
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!isRecord(parsed) || typeof parsed.grade !== 'string') return ''
    return parsed.grade
  } catch {
    return ''
  }
}

export function prettyJSON(value: string) {
  if (!value) return '{}'
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

export function shortID(value: string) {
  return value.slice(0, 8)
}

export function qualificationToastDescription(checks: Array<{ status: string; summary: string }>) {
  const noteworthy = checks.filter(
    (check) => check.status.endsWith('_FAILED') || check.status.endsWith('_WARNING'),
  )
  const rows = noteworthy.length > 0 ? noteworthy : checks
  return rows
    .slice(0, 4)
    .map((check) => check.summary)
    .join('\n')
}

export function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : 'failed'
}
