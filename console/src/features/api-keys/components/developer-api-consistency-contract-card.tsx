import {
  ArrowUpDown,
  ArrowUpRight,
  Braces,
  ClipboardCheck,
  FileWarning,
  Filter,
  GitCompareArrows,
  Repeat2,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  Workflow,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  DeveloperApiConsistencyContract,
  DeveloperApiConsistencyLane,
  DeveloperApiConsistencyLaneKey,
  DeveloperApiConsistencyStatus,
} from '../developer-api-consistency-contract'

export function DeveloperApiConsistencyContractCard({
  contract,
}: {
  contract: DeveloperApiConsistencyContract
}) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="developer-api-consistency-contract"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-background py-0 shadow-none"
    >
      <CardHeader className="border-b border-border/55 bg-muted/20 px-5 py-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.05rem] tracking-tight">
              <Workflow className="size-4 text-muted-foreground" />
              {t('api_keys.api_consistency.title', 'Developer API consistency contract')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-2xl text-sm leading-[1.35rem]">
              {t(
                'api_keys.api_consistency.description',
                'Pagination, filters, sort enums, error envelopes, idempotency, and SDK wire names stay regression-verifiable.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <DeveloperApiConsistencyTotal
              label={t('api_keys.api_consistency.verified', 'Verified')}
              tone="verified"
              value={contract.totals.verified}
            />
            <DeveloperApiConsistencyTotal
              label={t('api_keys.api_consistency.watch', 'Watch')}
              tone="watch"
              value={contract.totals.watch}
            />
            <DeveloperApiConsistencyTotal
              label={t('api_keys.api_consistency.blocked', 'Blocked')}
              tone="blocked"
              value={contract.totals.blocked}
            />
            <DeveloperApiConsistencyTotal
              label={t('api_keys.api_consistency.needs_data', 'Needs data')}
              tone="needs_data"
              value={contract.totals.needs_data}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-muted/10 px-3 py-3">
          <DeveloperApiConsistencyFact
            label={t('api_keys.api_consistency.fingerprint', 'Consistency fingerprint')}
            value={contract.fingerprint}
          />
          <DeveloperApiConsistencyFact
            label={t('api_keys.api_consistency.summary', 'Consistency decision')}
            value={contract.summary}
          />
        </div>
        <div className="grid gap-3">
          {contract.lanes.map((lane) => (
            <DeveloperApiConsistencyLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function DeveloperApiConsistencyTotal({
  label,
  tone,
  value,
}: {
  label: string
  tone: DeveloperApiConsistencyStatus
  value: number
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', developerApiConsistencySurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function DeveloperApiConsistencyLaneCard({ lane }: { lane: DeveloperApiConsistencyLane }) {
  const { t } = useTranslation()
  const Icon = developerApiConsistencyIcon(lane.key)
  return (
    <div
      data-testid={`developer-api-consistency-contract-${lane.key}`}
      className="flex min-w-0 flex-col rounded-[0.95rem] border border-border/60 bg-background px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="truncate text-sm font-semibold text-foreground">
              {t(`api_keys.api_consistency.lanes.${lane.key}.title`, lane.title)}
            </div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {t(`api_keys.api_consistency.lanes.${lane.key}.owner`, lane.owner)}
          </div>
        </div>
        <DeveloperApiConsistencyStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <DeveloperApiConsistencyFact
          label={t('api_keys.api_consistency.signal', 'Signal')}
          value={lane.signal}
        />
        <DeveloperApiConsistencyFact
          label={t('api_keys.api_consistency.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <DeveloperApiConsistencyFact
          label={t('api_keys.api_consistency.guardrail', 'Guardrail')}
          value={lane.guardrail}
        />
      </div>
      <a
        href={lane.actionHref}
        className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <span className="min-w-0 truncate">
          {t(`api_keys.api_consistency.lanes.${lane.key}.action`, lane.actionLabel)}
        </span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function DeveloperApiConsistencyFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function DeveloperApiConsistencyStatusBadge({ status }: { status: DeveloperApiConsistencyStatus }) {
  const { t } = useTranslation()
  const Icon =
    status === 'blocked'
      ? ShieldAlert
      : status === 'watch'
        ? SlidersHorizontal
        : status === 'needs_data'
          ? ClipboardCheck
          : ShieldCheck
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        developerApiConsistencyBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`api_keys.api_consistency.status.${status}`, status)}
    </span>
  )
}

function developerApiConsistencyIcon(key: DeveloperApiConsistencyLaneKey) {
  switch (key) {
    case 'pagination_contract':
      return GitCompareArrows
    case 'filter_contract':
      return Filter
    case 'sort_contract':
      return ArrowUpDown
    case 'error_envelope':
      return FileWarning
    case 'idempotency_contract':
      return Repeat2
    case 'sdk_wire_semantics':
      return Braces
    default:
      return Workflow
  }
}

function developerApiConsistencyBadgeClass(status: DeveloperApiConsistencyStatus) {
  switch (status) {
    case 'verified':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'watch':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    case 'blocked':
      return 'border-red-200 bg-red-50 text-red-700'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50 text-sky-800'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}

function developerApiConsistencySurfaceClass(status: DeveloperApiConsistencyStatus) {
  switch (status) {
    case 'verified':
      return 'border-emerald-200 bg-emerald-50/70'
    case 'watch':
      return 'border-amber-200 bg-amber-50/70'
    case 'blocked':
      return 'border-red-200 bg-red-50/70'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50/70'
    default:
      return 'border-border bg-muted/20'
  }
}
