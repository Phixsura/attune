import {
  ArrowUpRight,
  Boxes,
  Braces,
  ClipboardCheck,
  Code2,
  FileJson2,
  FlaskConical,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  Webhook,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  DeveloperApiAdoptionKit,
  DeveloperApiAdoptionLane,
  DeveloperApiAdoptionLaneKey,
  DeveloperApiAdoptionStatus,
} from '../developer-api-adoption-kit'

export function DeveloperApiAdoptionKitCard({ kit }: { kit: DeveloperApiAdoptionKit }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="developer-api-adoption-kit"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(248,250,252,0.985))] py-0 shadow-none"
    >
      <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(248,250,252,0.86),rgba(255,255,255,0.94))] px-5 py-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.05rem] tracking-tight">
              <Braces className="size-4 text-muted-foreground" />
              {t('api_keys.developer_adoption.title', 'Developer API adoption kit')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-2xl text-sm leading-[1.35rem]">
              {t(
                'api_keys.developer_adoption.description',
                'OpenAPI, SDKs, examples, sandbox bootstrap, and webhook replay evidence stay in one adoption surface.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <DeveloperAdoptionTotal
              label={t('api_keys.developer_adoption.ready', 'Ready')}
              tone="ready"
              value={kit.totals.ready}
            />
            <DeveloperAdoptionTotal
              label={t('api_keys.developer_adoption.watch', 'Watch')}
              tone="watch"
              value={kit.totals.watch}
            />
            <DeveloperAdoptionTotal
              label={t('api_keys.developer_adoption.blocked', 'Blocked')}
              tone="blocked"
              value={kit.totals.blocked}
            />
            <DeveloperAdoptionTotal
              label={t('api_keys.developer_adoption.needs_data', 'Needs data')}
              tone="needs_data"
              value={kit.totals.needs_data}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-background/80 px-3 py-3">
          <DeveloperAdoptionFact
            label={t('api_keys.developer_adoption.fingerprint', 'Adoption fingerprint')}
            value={kit.fingerprint}
          />
          <DeveloperAdoptionFact
            label={t('api_keys.developer_adoption.summary', 'Adoption decision')}
            value={kit.summary}
          />
        </div>
        <div className="grid gap-3">
          {kit.lanes.map((lane) => (
            <DeveloperAdoptionLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function DeveloperAdoptionTotal({
  label,
  tone,
  value,
}: {
  label: string
  tone: DeveloperApiAdoptionStatus
  value: number
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', developerAdoptionSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function DeveloperAdoptionLaneCard({ lane }: { lane: DeveloperApiAdoptionLane }) {
  const { t } = useTranslation()
  const Icon = developerAdoptionIcon(lane.key)
  return (
    <div
      data-testid={`developer-api-adoption-kit-${lane.key}`}
      className="flex min-w-0 flex-col rounded-[0.95rem] border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="truncate text-sm font-semibold text-foreground">
              {t(`api_keys.developer_adoption.lanes.${lane.key}.title`, lane.title)}
            </div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {t(`api_keys.developer_adoption.lanes.${lane.key}.owner`, lane.owner)}
          </div>
        </div>
        <DeveloperAdoptionStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <DeveloperAdoptionFact
          label={t('api_keys.developer_adoption.signal', 'Signal')}
          value={lane.signal}
        />
        <DeveloperAdoptionFact
          label={t('api_keys.developer_adoption.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <DeveloperAdoptionFact
          label={t('api_keys.developer_adoption.guardrail', 'Guardrail')}
          value={lane.guardrail}
        />
      </div>
      <a
        href={lane.actionHref}
        className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <span className="min-w-0 truncate">
          {t(`api_keys.developer_adoption.lanes.${lane.key}.action`, lane.actionLabel)}
        </span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function DeveloperAdoptionFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function DeveloperAdoptionStatusBadge({ status }: { status: DeveloperApiAdoptionStatus }) {
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
        developerAdoptionBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`api_keys.developer_adoption.status.${status}`, status)}
    </span>
  )
}

function developerAdoptionIcon(key: DeveloperApiAdoptionLaneKey) {
  switch (key) {
    case 'openapi_contract':
      return FileJson2
    case 'node_sdk':
      return Code2
    case 'go_sdk':
      return Boxes
    case 'example_sandbox':
      return FlaskConical
    case 'webhook_replay':
      return Webhook
    default:
      return Braces
  }
}

function developerAdoptionBadgeClass(status: DeveloperApiAdoptionStatus) {
  switch (status) {
    case 'ready':
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

function developerAdoptionSurfaceClass(status: DeveloperApiAdoptionStatus) {
  switch (status) {
    case 'ready':
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
