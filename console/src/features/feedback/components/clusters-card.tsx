import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronRight, Layers, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { clustersQuery } from '@/features/feedback/api/list-clusters'

export function ClustersCard() {
  const { t } = useTranslation()
  const clusters = useQuery(clustersQuery({ recencyDays: 30, minCount: 2, limit: 5 }))

  if (clusters.isPending) {
    return (
      <Card className="mt-6 gap-4 rounded-lg border-border/60 bg-muted/20 py-4 shadow-none">
        <CardHeader className="px-4">
          <CardTitle className="text-base">{t('feedback.clusters.title')}</CardTitle>
        </CardHeader>
        <CardContent className="flex items-center justify-center px-4 py-8">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </CardContent>
      </Card>
    )
  }

  if (!clusters.data?.clusteringEnabled) {
    return null
  }

  const items = clusters.data?.items ?? []
  const totalCount = clusters.data?.totalCount ?? 0

  return (
    <Card className="mt-6 gap-4 rounded-lg border-border/60 bg-muted/20 py-4 shadow-none">
      <CardHeader className="px-4">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-base">{t('feedback.clusters.title')}</CardTitle>
            <CardDescription>{t('feedback.clusters.subtitle')}</CardDescription>
          </div>
          {totalCount > 0 && (
            <Link
              to="/clusters"
              className="flex items-center gap-1 text-sm text-primary hover:underline"
            >
              {t('feedback.clusters.view_all')}
              <ChevronRight className="h-4 w-4" />
            </Link>
          )}
        </div>
      </CardHeader>
      <CardContent className="px-4">
        {items.length === 0 ? (
          <EmptyState
            icon={Layers}
            title={t('feedback.clusters.empty_title')}
            description={t('feedback.clusters.empty_body')}
            className="py-6"
          />
        ) : (
          <div className="space-y-2">
            {items.slice(0, 3).map((cluster) => (
              <Link
                key={cluster.clusterId}
                to="/clusters"
                className="flex w-full items-center gap-3 rounded-md border border-border/60 bg-background px-3 py-2.5 text-left transition-colors hover:bg-muted/30"
              >
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
                  <Layers className="h-4 w-4" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium text-sm">
                      {cluster.label || cluster.sampleTitle}
                    </span>
                    <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                      {t('feedback.clusters.count', { count: cluster.count })}
                    </span>
                  </div>
                  <div className="mt-0.5 truncate text-xs text-muted-foreground">
                    {cluster.sampleTitle}
                  </div>
                </div>
                <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
              </Link>
            ))}
            {totalCount > 3 && (
              <div className="pt-1 text-center text-xs text-muted-foreground">
                {t('feedback.clusters.total', { total: totalCount })}
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
