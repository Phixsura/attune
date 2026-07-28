import { useMutation, useQueryClient } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Loader2, RefreshCcw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { type Cohort, syncCohort } from '../api/cohort-sync'
import { CohortDetailSheet } from './cohort-detail-sheet'

export function CohortsTab({ cohorts }: { cohorts: Cohort[] }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [detailCohort, setDetailCohort] = useState<Cohort | null>(null)

  const syncM = useMutation({
    mutationFn: syncCohort,
    onSuccess: (result) => {
      qc.invalidateQueries({ queryKey: ['cohort-sync'] })
      const run = result.run
      if (run) {
        toast.success(
          t('cohort_sync.cohort.sync_ok', {
            added: run.membersAdded,
            removed: run.membersRemoved,
            total: run.membersTotal,
          }),
        )
      } else {
        toast.success(t('common.save'))
      }
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
  })

  if (cohorts.length === 0) {
    return (
      <EmptyState
        title={t('cohort_sync.cohort.no_cohorts')}
        description={t('cohort_sync.cohort.no_cohorts_desc')}
      />
    )
  }

  return (
    <>
      <Card className="border-border/60 shadow-none">
        <CardHeader>
          <CardTitle className="text-base">{t('cohort_sync.tabs.cohorts')}</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('cohort_sync.source.name')}</TableHead>
                <TableHead>{t('cohort_sync.source.provider')}</TableHead>
                <TableHead>{t('cohort_sync.cohort.members')}</TableHead>
                <TableHead>{t('cohort_sync.source.last_sync')}</TableHead>
                <TableHead className="text-right">{t('common.edit')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {cohorts.map((cohort) => (
                <TableRow
                  key={cohort.id}
                  className="cursor-pointer"
                  onClick={() => setDetailCohort(cohort)}
                >
                  <TableCell>
                    <div className="text-sm font-medium">{cohort.name}</div>
                    <div className="text-xs text-muted-foreground">{cohort.externalCohortId}</div>
                    {cohort.lastError && (
                      <div className="mt-0.5 max-w-[20rem] truncate text-xs text-destructive">
                        {cohort.lastError}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-sm">
                    {cohort.sourceProvider ?? '—'}
                    {cohort.sourceName && (
                      <span className="ml-1 text-xs text-muted-foreground/60">
                        ({cohort.sourceName})
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="text-sm">
                    {t('cohort_sync.cohort.member_count', { count: cohort.memberCount })}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-sm">
                    {cohort.lastSyncedAt
                      ? formatDistanceToNow(new Date(cohort.lastSyncedAt), {
                          addSuffix: true,
                          locale: zhCN,
                        })
                      : t('common.never')}
                  </TableCell>
                  <TableCell className="text-right">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={syncM.isPending && syncM.variables === cohort.id}
                          onClick={(e) => {
                            e.stopPropagation()
                            syncM.mutate(cohort.id)
                          }}
                        >
                          {syncM.isPending && syncM.variables === cohort.id ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <RefreshCcw className="h-3.5 w-3.5" />
                          )}
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>{t('cohort_sync.cohort.sync_now')}</TooltipContent>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {detailCohort && (
        <CohortDetailSheet
          cohort={detailCohort}
          open={!!detailCohort}
          onOpenChange={(v) => !v && setDetailCohort(null)}
        />
      )}
    </>
  )
}
