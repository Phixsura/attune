import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
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
  type Cohort,
  type CohortSource,
  type CohortSyncHealth,
  createCohortSource,
  deleteCohortSource,
  syncCohort,
  testCohortSource,
} from '../api/cohort-sync'

interface CohortSyncUIProps {
  sources: CohortSource[]
  cohorts: Cohort[]
  health: CohortSyncHealth | undefined
  isLoading: boolean
}

export function CohortSyncUI({ sources, cohorts, health, isLoading }: CohortSyncUIProps) {
  const queryClient = useQueryClient()
  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['cohort-sync'] })
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center p-12">
        <div className="text-sm text-muted-foreground">Loading cohort sync...</div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">Cohort Sync</h2>
          <p className="text-sm text-muted-foreground">
            Import cohorts from Amplitude and Mixpanel to filter feedback and customer requests by
            audience membership.
          </p>
        </div>
        <CreateSourceDialog onCreated={invalidate} />
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
          <div className="rounded-md border border-dashed p-8 text-center">
            <p className="text-sm text-muted-foreground">No cohort sources configured.</p>
            <p className="mt-1 text-xs text-muted-foreground">
              Connect Amplitude or Mixpanel to start importing cohorts.
            </p>
          </div>
        ) : (
          <div className="divide-y rounded-md border">
            {sources.map((source) => (
              <SourceRow key={source.id} source={source} onAction={invalidate} />
            ))}
          </div>
        )}
      </div>

      <div>
        <h3 className="mb-3 text-sm font-medium">Cohorts</h3>
        {cohorts.length === 0 ? (
          <div className="rounded-md border border-dashed p-8 text-center">
            <p className="text-sm text-muted-foreground">No cohorts synced yet.</p>
            <p className="mt-1 text-xs text-muted-foreground">
              Cohorts will appear here after Amplitude or Mixpanel pushes membership data.
            </p>
          </div>
        ) : (
          <div className="divide-y rounded-md border">
            {cohorts.map((cohort) => (
              <CohortRow key={cohort.id} cohort={cohort} onAction={invalidate} />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function SourceRow({ source, onAction }: { source: CohortSource; onAction: () => void }) {
  const [testResult, setTestResult] = useState<{ ok: boolean; error?: string } | null>(null)
  const testMutation = useMutation({
    mutationFn: () => testCohortSource(source.id),
    onSuccess: (data) => {
      setTestResult({ ok: data.ok, error: data.error })
      onAction()
    },
    onError: () => {
      setTestResult({ ok: false, error: 'Network error' })
    },
  })
  const deleteMutation = useMutation({
    mutationFn: () => deleteCohortSource(source.id),
    onSuccess: () => onAction(),
  })

  return (
    <div className="px-4 py-3">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-medium">{source.name}</div>
          <div className="text-xs text-muted-foreground">
            {source.provider} &middot; {source.status}
            {source.lastSyncAt && (
              <> &middot; last synced {new Date(source.lastSyncAt).toLocaleString()}</>
            )}
          </div>
          {source.lastError && (
            <div className="mt-1 text-xs text-destructive">{source.lastError}</div>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => testMutation.mutate()}
            disabled={testMutation.isPending}
          >
            {testMutation.isPending ? 'Testing...' : 'Test'}
          </Button>
          {testResult && (
            <span className={`text-xs ${testResult.ok ? 'text-green-600' : 'text-destructive'}`}>
              {testResult.ok ? '✓ OK' : `✗ ${testResult.error || 'Failed'}`}
            </span>
          )}
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              if (window.confirm(`Delete source "${source.name}"?`)) {
                deleteMutation.mutate()
              }
            }}
            disabled={deleteMutation.isPending}
          >
            Delete
          </Button>
          <StatusBadge status={source.status} enabled={source.enabled} />
        </div>
      </div>
      {source.webhookUrl && (
        <div className="mt-2 rounded bg-muted/50 px-3 py-2">
          <div className="text-xs font-medium text-muted-foreground">Webhook URL</div>
          <code className="block break-all text-xs">{source.webhookUrl}</code>
          {source.provider === 'amplitude' && (
            <div className="mt-1 text-xs text-muted-foreground">
              Amplitude needs three URLs (replace /add with /create and /remove).
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function CohortRow({ cohort, onAction }: { cohort: Cohort; onAction: () => void }) {
  const syncMutation = useMutation({
    mutationFn: () => syncCohort(cohort.id),
    onSuccess: () => onAction(),
  })

  return (
    <div className="flex items-center justify-between px-4 py-3">
      <div>
        <div className="text-sm font-medium">{cohort.name}</div>
        <div className="text-xs text-muted-foreground">
          {cohort.memberCount} members &middot; {cohort.externalCohortId}
          {cohort.lastSyncedAt && (
            <> &middot; synced {new Date(cohort.lastSyncedAt).toLocaleString()}</>
          )}
        </div>
        {cohort.lastError && (
          <div className="mt-1 text-xs text-destructive">{cohort.lastError}</div>
        )}
      </div>
      <div className="flex gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={() => syncMutation.mutate()}
          disabled={syncMutation.isPending}
        >
          {syncMutation.isPending ? 'Syncing...' : 'Sync Now'}
        </Button>
        <StatusBadge status={cohort.lastError ? 'error' : 'active'} enabled={cohort.enabled} />
      </div>
    </div>
  )
}

function CreateSourceDialog({ onCreated }: { onCreated: () => void }) {
  const [open, setOpen] = useState(false)
  const [provider, setProvider] = useState('amplitude')
  const [name, setName] = useState('')
  const [credential, setCredential] = useState('')
  const [pullCredential, setPullCredential] = useState('')

  const mutation = useMutation({
    mutationFn: () =>
      createCohortSource({
        provider,
        name,
        authType: 'api_key',
        credential,
        enabled: true,
        pullCredential: pullCredential || undefined,
      }),
    onSuccess: () => {
      setOpen(false)
      setName('')
      setCredential('')
      setPullCredential('')
      onCreated()
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>Add Source</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add Cohort Source</DialogTitle>
          <DialogDescription>
            Connect an Amplitude or Mixpanel project to sync cohort membership.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div>
            <Label htmlFor="provider">Provider</Label>
            <Select value={provider} onValueChange={setProvider}>
              <SelectTrigger id="provider">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="amplitude">Amplitude</SelectItem>
                <SelectItem value="mixpanel">Mixpanel</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div>
            <Label htmlFor="name">Name</Label>
            <Input
              id="name"
              placeholder="My Amplitude Project"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div>
            <Label htmlFor="credential">Webhook API Key</Label>
            <Input
              id="credential"
              type="password"
              placeholder="API key for webhook authentication"
              value={credential}
              onChange={(e) => setCredential(e.target.value)}
            />
            <p className="mt-1 text-xs text-muted-foreground">
              This key is used to authenticate incoming webhook pushes from the provider.
            </p>
          </div>
          <div>
            <Label htmlFor="pullCredential">Pull Credential (optional)</Label>
            <Input
              id="pullCredential"
              type="password"
              placeholder={
                provider === 'amplitude' ? 'api_key:secret_key' : 'service_account:secret'
              }
              value={pullCredential}
              onChange={(e) => setPullCredential(e.target.value)}
            />
            <p className="mt-1 text-xs text-muted-foreground">
              {provider === 'amplitude'
                ? 'Your Amplitude API key and Secret Key (format: api_key:secret_key). Needed for Test Connection and Sync Now.'
                : 'Your Mixpanel service account credentials (format: username:secret). Needed for Test Connection and Sync Now.'}
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={!name || !credential || mutation.isPending}
          >
            {mutation.isPending ? 'Creating...' : 'Create'}
          </Button>
        </DialogFooter>
        {mutation.error && (
          <p className="text-sm text-destructive">
            {mutation.error instanceof Error ? mutation.error.message : 'Failed to create source'}
          </p>
        )}
      </DialogContent>
    </Dialog>
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
