import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Loading } from '@/components/loading'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
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
import { WebhookUrlsDisplay } from './webhook-urls-display'

export function CohortSyncPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('cohort_sync.title'))
  const qc = useQueryClient()
  const invalidate = () => qc.invalidateQueries({ queryKey: ['cohort-sync'] })

  const sourcesQ = useQuery(listCohortSourcesQuery())
  const cohortsQ = useQuery(listCohortsQuery())
  const healthQ = useQuery(cohortSyncHealthQuery())

  const [createOpen, setCreateOpen] = useState(false)
  const [createdSource, setCreatedSource] = useState<CohortSource | null>(null)
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
    onSuccess: (source) => {
      invalidate()
      toast.success(t('common.create'))
      setCreateOpen(false)
      setCreatedSource(source)
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

  // Show error state if critical queries failed (sources or cohorts).
  // Health failure is non-critical — page still renders without metrics.
  if (sourcesQ.isError || cohortsQ.isError) {
    return (
      <>
        <PageHero
          eyebrow={t('shell.groups.integrations')}
          title={t('cohort_sync.title')}
          subtitle={t('cohort_sync.subtitle')}
        />
        <div className="mt-6 rounded-md border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
          {t('common.error')} — {t('common.retry')}
        </div>
      </>
    )
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
              {health.lastSyncAt && (
                <PageHeroMetric
                  label={t('cohort_sync.health.last_sync')}
                  value={formatDistanceToNow(new Date(health.lastSyncAt), {
                    addSuffix: true,
                    locale: zhCN,
                  })}
                />
              )}
            </>
          )
        }
      />

      {health && health.errorSources > 0 && (
        <div className="mt-4 rounded-md border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
          {t('cohort_sync.health.error_banner', { count: health.errorSources })}
        </div>
      )}

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
      {createdSource && (
        <Dialog open={!!createdSource} onOpenChange={(v) => !v && setCreatedSource(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t('cohort_sync.onboarding.created_title')}</DialogTitle>
            </DialogHeader>
            <div className="space-y-4">
              <p className="text-sm text-muted-foreground">
                {t('cohort_sync.onboarding.created_desc')}
              </p>
              <div className="rounded-md border bg-muted/30 p-3">
                <WebhookUrlsDisplay
                  urls={createdSource.webhookUrls ?? []}
                  provider={createdSource.provider}
                />
              </div>
              <div className="space-y-2 text-sm text-muted-foreground">
                <p className="font-medium text-foreground">
                  {t('cohort_sync.onboarding.next_steps')}
                </p>
                <ol className="list-inside list-decimal space-y-1">
                  <li>{t('cohort_sync.onboarding.next1')}</li>
                  <li>{t('cohort_sync.onboarding.next2')}</li>
                  <li>{t('cohort_sync.onboarding.next3')}</li>
                </ol>
              </div>
            </div>
            <DialogFooter>
              <Button onClick={() => setCreatedSource(null)}>{t('common.close')}</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}

export { cohortSyncHealthQuery, listCohortSourcesQuery, listCohortsQuery }
