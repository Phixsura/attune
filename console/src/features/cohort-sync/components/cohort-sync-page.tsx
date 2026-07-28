import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Loading } from '@/components/loading'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { useDocumentTitle } from '@/hooks/use-document-title'
import {
  type CohortSource,
  cohortSyncHealthQuery,
  createCohortSource,
  deleteCohortSource,
  listCohortSourcesQuery,
  listCohortsQuery,
  testCohortSource,
  updateCohortSource,
} from '../api/cohort-sync'
import { CohortsTab } from './cohorts-tab'
import { DeleteSourceDialog } from './delete-source-dialog'
import { type SourceFormData, SourceFormDialog } from './source-form-dialog'
import { SourcesTab } from './sources-tab'

export function CohortSyncPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('cohort_sync.title'))
  const qc = useQueryClient()
  const invalidate = () => qc.invalidateQueries({ queryKey: ['cohort-sync'] })

  const sourcesQ = useQuery(listCohortSourcesQuery())
  const cohortsQ = useQuery(listCohortsQuery())
  const healthQ = useQuery(cohortSyncHealthQuery())

  const [createOpen, setCreateOpen] = useState(false)
  const [editSource, setEditSource] = useState<CohortSource | null>(null)
  const [deleteSource, setDeleteSource] = useState<CohortSource | null>(null)

  const createM = useMutation({
    mutationFn: (data: SourceFormData) =>
      createCohortSource({
        provider: data.provider,
        name: data.name,
        authType: 'api_key',
        credential: data.credential ?? '',
        pullCredential: data.pullCredential,
        baseUrl: data.baseUrl,
        enabled: true,
      }),
    onSuccess: () => {
      invalidate()
      toast.success(t('common.create'))
      setCreateOpen(false)
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
  })

  const updateM = useMutation({
    mutationFn: ({ id, ...data }: { id: string } & SourceFormData) =>
      updateCohortSource(id, {
        name: data.name,
        credential: data.credential || undefined,
        pullCredential: data.pullCredential || undefined,
        baseUrl: data.baseUrl || undefined,
        enabled: data.enabled,
      }),
    onSuccess: () => {
      invalidate()
      toast.success(t('common.save'))
      setEditSource(null)
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
  })

  const deleteM = useMutation({
    mutationFn: deleteCohortSource,
    onSuccess: () => {
      invalidate()
      toast.success(t('common.delete'))
      setDeleteSource(null)
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
  })

  const testM = useMutation({
    mutationFn: testCohortSource,
    onSuccess: (result) => {
      invalidate()
      if (result.ok) {
        toast.success(t('cohort_sync.source.test_ok'))
      } else {
        toast.error(t('cohort_sync.source.test_fail', { error: result.error }))
      }
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
  })

  if (sourcesQ.isLoading || cohortsQ.isLoading || healthQ.isLoading) {
    return <Loading />
  }

  const sources = sourcesQ.data ?? []
  const cohorts = cohortsQ.data ?? []
  const health = healthQ.data

  return (
    <>
      <PageHero
        eyebrow={t('shell.groups.integrations')}
        title={t('cohort_sync.title')}
        subtitle={t('cohort_sync.subtitle')}
        actions={
          <Button onClick={() => setCreateOpen(true)}>{t('cohort_sync.source.create')}</Button>
        }
        metrics={
          health && (
            <>
              <PageHeroMetric
                label={t('cohort_sync.health.sources')}
                value={`${health.activeSources}/${health.sourceCount}`}
                tone={health.errorSources > 0 ? 'urgent' : 'default'}
              />
              <PageHeroMetric
                label={t('cohort_sync.health.cohorts')}
                value={String(health.cohortCount)}
              />
              <PageHeroMetric
                label={t('cohort_sync.health.members')}
                value={String(health.totalActiveMembers)}
              />
            </>
          )
        }
      />

      <div className="mt-6 space-y-8">
        <SourcesTab
          sources={sources}
          testingId={testM.isPending ? (testM.variables as string) : undefined}
          onTest={(s) => testM.mutate(s.id)}
          onEdit={setEditSource}
          onDelete={setDeleteSource}
          onToggleEnabled={(s) =>
            updateM.mutate({ id: s.id, provider: s.provider, name: s.name, enabled: !s.enabled })
          }
          onCreateClick={() => setCreateOpen(true)}
        />
        <CohortsTab cohorts={cohorts} />
      </div>

      <SourceFormDialog
        mode="create"
        open={createOpen}
        onOpenChange={setCreateOpen}
        pending={createM.isPending}
        onSubmit={(data) => createM.mutate(data)}
      />
      {editSource && (
        <SourceFormDialog
          mode="edit"
          open={!!editSource}
          onOpenChange={(v) => !v && setEditSource(null)}
          pending={updateM.isPending}
          source={editSource}
          onSubmit={(data: SourceFormData) => updateM.mutate({ id: editSource.id, ...data })}
        />
      )}
      {deleteSource && (
        <DeleteSourceDialog
          open={!!deleteSource}
          onOpenChange={(v) => !v && setDeleteSource(null)}
          sourceName={deleteSource.name}
          pending={deleteM.isPending}
          onConfirm={() => deleteM.mutate(deleteSource.id)}
        />
      )}
    </>
  )
}

export { cohortSyncHealthQuery, listCohortSourcesQuery, listCohortsQuery }
