import {
  ArrowUpRight,
  ClipboardCheck,
  Code2,
  GitCompareArrows,
  Globe2,
  PackageCheck,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  TerminalSquare,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  DeveloperSdkParityGate,
  DeveloperSdkParityLane,
  DeveloperSdkParityLaneKey,
  DeveloperSdkParityStatus,
} from '../developer-sdk-parity-gate'

export function DeveloperSdkParityGateCard({ gate }: { gate: DeveloperSdkParityGate }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="developer-sdk-parity-gate"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(248,250,252,0.985))] py-0 shadow-none"
    >
      <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(248,250,252,0.86),rgba(255,255,255,0.94))] px-5 py-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.05rem] tracking-tight">
              <GitCompareArrows className="size-4 text-muted-foreground" />
              {t('api_keys.sdk_parity.title', 'Developer SDK parity gate')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-2xl text-sm leading-[1.35rem]">
              {t(
                'api_keys.sdk_parity.description',
                'Node and Go SDK public surfaces, errors, retries, idempotency, browser boundary, and release artifacts stay verifiable.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <DeveloperSdkParityTotal
              label={t('api_keys.sdk_parity.verified', 'Verified')}
              tone="verified"
              value={gate.totals.verified}
            />
            <DeveloperSdkParityTotal
              label={t('api_keys.sdk_parity.watch', 'Watch')}
              tone="watch"
              value={gate.totals.watch}
            />
            <DeveloperSdkParityTotal
              label={t('api_keys.sdk_parity.blocked', 'Blocked')}
              tone="blocked"
              value={gate.totals.blocked}
            />
            <DeveloperSdkParityTotal
              label={t('api_keys.sdk_parity.needs_data', 'Needs data')}
              tone="needs_data"
              value={gate.totals.needs_data}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-background/80 px-3 py-3">
          <DeveloperSdkParityFact
            label={t('api_keys.sdk_parity.fingerprint', 'Parity fingerprint')}
            value={gate.fingerprint}
          />
          <DeveloperSdkParityFact
            label={t('api_keys.sdk_parity.summary', 'Parity decision')}
            value={gate.summary}
          />
        </div>
        <div className="grid gap-3">
          {gate.lanes.map((lane) => (
            <DeveloperSdkParityLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function DeveloperSdkParityTotal({
  label,
  tone,
  value,
}: {
  label: string
  tone: DeveloperSdkParityStatus
  value: number
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', developerSdkParitySurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function DeveloperSdkParityLaneCard({ lane }: { lane: DeveloperSdkParityLane }) {
  const { t } = useTranslation()
  const Icon = developerSdkParityIcon(lane.key)
  return (
    <div
      data-testid={`developer-sdk-parity-gate-${lane.key}`}
      className="flex min-w-0 flex-col rounded-[0.95rem] border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="truncate text-sm font-semibold text-foreground">
              {t(`api_keys.sdk_parity.lanes.${lane.key}.title`, lane.title)}
            </div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {t(`api_keys.sdk_parity.lanes.${lane.key}.owner`, lane.owner)}
          </div>
        </div>
        <DeveloperSdkParityStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <DeveloperSdkParityFact
          label={t('api_keys.sdk_parity.signal', 'Signal')}
          value={lane.signal}
        />
        <DeveloperSdkParityFact
          label={t('api_keys.sdk_parity.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <DeveloperSdkParityFact
          label={t('api_keys.sdk_parity.guardrail', 'Guardrail')}
          value={lane.guardrail}
        />
      </div>
      <a
        href={lane.actionHref}
        className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <span className="min-w-0 truncate">
          {t(`api_keys.sdk_parity.lanes.${lane.key}.action`, lane.actionLabel)}
        </span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function DeveloperSdkParityFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function DeveloperSdkParityStatusBadge({ status }: { status: DeveloperSdkParityStatus }) {
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
        developerSdkParityBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`api_keys.sdk_parity.status.${status}`, status)}
    </span>
  )
}

function developerSdkParityIcon(key: DeveloperSdkParityLaneKey) {
  switch (key) {
    case 'management_surface':
      return Code2
    case 'error_contract':
      return TerminalSquare
    case 'retry_idempotency':
      return RefreshCw
    case 'browser_boundary':
      return Globe2
    case 'release_artifacts':
      return PackageCheck
    default:
      return GitCompareArrows
  }
}

function developerSdkParityBadgeClass(status: DeveloperSdkParityStatus) {
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

function developerSdkParitySurfaceClass(status: DeveloperSdkParityStatus) {
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
