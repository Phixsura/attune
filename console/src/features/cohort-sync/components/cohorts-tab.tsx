import { useMutation, useQueryClient } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Loader2, RefreshCcw, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
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
  const [searchQuery, setSearchQuery] = useState('')

  const filteredCohorts = useMemo(() => {
    if (!searchQuery.trim()) return cohorts
    const q = searchQuery.toLowerCase()
    return cohorts.filter(
      (c) =>
        c.name.toLowerCase().includes(q) ||
        c.externalCohortId.toLowerCase().includes(q) ||
        (c.sourceName ?? '').toLowerCase().includes(q),
    )
  }, [cohorts, searchQuery])

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
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-base">{t('cohort_sync.tabs.cohorts')}</CardTitle>
          <div className="relative w-48">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="h-8 pl-8 text-sm"
              placeholder={t('common.search')}
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
          </div>
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
              {filteredCohorts.map((cohort) => (
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
                  <TableCell
                    className={`text-sm ${stalenessColor(cohort.lastSyncedAt, cohort.staleTtlDays)}`}
                  >
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

function stalenessColor(lastSyncAt: string | Date | undefined, staleTtlDays?: number): string {
  if (!lastSyncAt) return 'text-muted-foreground'
  const ts = lastSyncAt instanceof Date ? lastSyncAt.getTime() : new Date(lastSyncAt).getTime()
  const hours = (Date.now() - ts) / 3_600_000
  // If TTL is set, compare against it: warn at 50% of TTL, critical at 90%.
  if (staleTtlDays && staleTtlDays > 0) {
    const ttlHours = staleTtlDays * 24
    if (hours > ttlHours * 0.9) return 'text-destructive'
    if (hours > ttlHours * 0.5) return 'text-amber-600 dark:text-amber-400'
    return 'text-green-600 dark:text-green-400'
  }
  if (hours < 1) return 'text-green-600 dark:text-green-400'
  if (hours < 24) return 'text-amber-600 dark:text-amber-400'
  return 'text-destructive'
}
