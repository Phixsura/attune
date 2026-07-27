import type { Cohort, CohortSource, CohortSyncHealth } from '../api/cohort-sync'

interface CohortSyncUIProps {
  sources: CohortSource[]
  cohorts: Cohort[]
  health: CohortSyncHealth | undefined
  isLoading: boolean
}

export function CohortSyncUI({ sources, cohorts, health, isLoading }: CohortSyncUIProps) {
  if (isLoading) {
    return (
      <div className="flex items-center justify-center p-12">
        <div className="text-sm text-muted-foreground">Loading cohort sync...</div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Cohort Sync</h2>
        <p className="text-sm text-muted-foreground">
          Import cohorts from Amplitude and Mixpanel to filter feedback and customer requests by
          audience membership.
        </p>
      </div>

      {health && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
          <HealthCard label="Sources" value={health.sourceCount} />
          <HealthCard label="Active" value={health.activeSources} />
          <HealthCard label="Errors" value={health.errorSources} variant="error" />
          <HealthCard label="Cohorts" value={health.cohortCount} />
          <HealthCard label="Members" value={health.totalActiveMembers} />
        </div>
      )}

      <div>
        <h3 className="mb-3 text-sm font-medium">Sources</h3>
        {sources.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No cohort sources configured. Connect Amplitude or Mixpanel to get started.
          </p>
        ) : (
          <div className="divide-y rounded-md border">
            {sources.map((source) => (
              <div key={source.id} className="flex items-center justify-between px-4 py-3">
                <div>
                  <div className="text-sm font-medium">{source.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {source.provider} &middot; {source.status}
                  </div>
                </div>
                <StatusBadge status={source.status} enabled={source.enabled} />
              </div>
            ))}
          </div>
        )}
      </div>

      <div>
        <h3 className="mb-3 text-sm font-medium">Cohorts</h3>
        {cohorts.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No cohorts synced yet. Cohorts will appear here after Amplitude or Mixpanel pushes
            membership data.
          </p>
        ) : (
          <div className="divide-y rounded-md border">
            {cohorts.map((cohort) => (
              <div key={cohort.id} className="flex items-center justify-between px-4 py-3">
                <div>
                  <div className="text-sm font-medium">{cohort.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {cohort.memberCount} members &middot; {cohort.externalCohortId}
                  </div>
                </div>
                <StatusBadge
                  status={cohort.lastError ? 'error' : 'active'}
                  enabled={cohort.enabled}
                />
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function HealthCard({
  label,
  value,
  variant,
}: {
  label: string
  value: number
  variant?: 'error'
}) {
  return (
    <div className="rounded-md border px-4 py-3 text-center">
      <div
        className={`text-2xl font-semibold ${variant === 'error' && value > 0 ? 'text-destructive' : ''}`}
      >
        {value}
      </div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  )
}

function StatusBadge({ status, enabled }: { status: string; enabled: boolean }) {
  if (!enabled) {
    return (
      <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
        Disabled
      </span>
    )
  }
  if (status === 'error') {
    return (
      <span className="rounded-full bg-destructive/10 px-2 py-0.5 text-xs text-destructive">
        Error
      </span>
    )
  }
  return (
    <span className="rounded-full bg-green-100 px-2 py-0.5 text-xs text-green-700 dark:bg-green-900/30 dark:text-green-400">
      Active
    </span>
  )
}
