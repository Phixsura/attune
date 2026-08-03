import {
  AlertTriangle,
  ArrowUpRight,
  Bot,
  BrainCircuit,
  DatabaseZap,
  Gauge,
  KeyRound,
  PackageX,
  ShieldAlert,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { cn } from '@/lib/utils'
import type {
  TenantQuotaLane,
  TenantQuotaLaneKey,
  TenantQuotaSaturation,
  TenantQuotaSaturationStatus,
} from '../tenant-quota-saturation'

export function TenantQuotaSaturationCard({ quota }: { quota: TenantQuotaSaturation }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="reliability-tenant-quota-saturation"
      className="border-border/60 bg-background/95 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <Gauge className="size-4 text-muted-foreground" />
              {t('reliability.tenant_quota.title', 'Tenant quota / saturation dashboard')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-3xl">
              {t(
                'reliability.tenant_quota.description',
                'Ingest, enrichment, MCP, GDPR, and outbox capacity signals stay tied to the same tenant boundary.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <TenantQuotaTotal
              label={t('reliability.tenant_quota.healthy', 'Healthy')}
              value={quota.totals.healthy}
              tone="healthy"
            />
            <TenantQuotaTotal
              label={t('reliability.tenant_quota.watch', 'Watch')}
              value={quota.totals.watch}
              tone="watch"
            />
            <TenantQuotaTotal
              label={t('reliability.tenant_quota.saturated', 'Saturated')}
              value={quota.totals.saturated}
              tone="saturated"
            />
            <TenantQuotaTotal
              label={t('reliability.tenant_quota.needs_data', 'Needs data')}
              value={quota.totals.needs_data}
              tone="needs_data"
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-4">
        <div className="grid gap-3 rounded-md border border-border/60 bg-muted/10 px-3 py-3 md:grid-cols-3">
          <TenantQuotaFact
            label={t('reliability.tenant_quota.fingerprint', 'Quota fingerprint')}
            value={quota.fingerprint}
          />
          <TenantQuotaFact
            label={t('reliability.tenant_quota.summary', 'Quota decision')}
            value={quota.summary}
          />
          <TenantQuotaFact
            label={t('reliability.tenant_quota.window', 'Window')}
            value={quota.windowLabel}
          />
        </div>
        <div className="grid gap-3 xl:grid-cols-5">
          {quota.lanes.map((lane) => (
            <TenantQuotaLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function TenantQuotaTotal({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: TenantQuotaSaturationStatus
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', tenantQuotaSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function TenantQuotaLaneCard({ lane }: { lane: TenantQuotaLane }) {
  const { t } = useTranslation()
  const Icon = tenantQuotaIcon(lane.key)
  const progressValue =
    lane.saturationPct === null ? 0 : Math.max(0, Math.min(lane.saturationPct, 100))
  return (
    <div
      data-testid={`reliability-tenant-quota-${lane.key}`}
      className="flex min-w-0 flex-col rounded-md border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="truncate text-sm font-semibold text-foreground">{lane.title}</div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">{lane.owner}</div>
        </div>
        <TenantQuotaStatusBadge status={lane.status} />
      </div>

      <div className="mt-3 space-y-2">
        <div>
          <div className="flex items-center justify-between gap-2 text-xs">
            <span className="text-muted-foreground">
              {t('reliability.tenant_quota.saturation', 'Saturation')}
            </span>
            <span className="font-medium tabular-nums text-foreground">
              {lane.saturationPct === null
                ? t('common.unknown', 'Unknown')
                : `${lane.saturationPct}%`}
            </span>
          </div>
          <Progress
            value={progressValue}
            aria-label={`${lane.title} saturation`}
            className="mt-2 h-2 bg-muted"
          />
        </div>
        <TenantQuotaFact
          label={t('reliability.tenant_quota.signal', 'Signal')}
          value={lane.signal}
        />
        <TenantQuotaFact
          label={t('reliability.tenant_quota.capacity', 'Capacity')}
          value={lane.capacityLabel}
        />
        <TenantQuotaFact
          label={t('reliability.tenant_quota.consumption', 'Consumption')}
          value={lane.consumptionLabel}
        />
        <TenantQuotaFact
          label={t('reliability.tenant_quota.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <TenantQuotaFact
          label={t('reliability.tenant_quota.guardrail', 'Guardrail')}
          value={lane.guardrail}
        />
      </div>

      <a
        href={lane.actionHref}
        className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <span className="min-w-0 truncate">{lane.actionLabel}</span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function TenantQuotaFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function TenantQuotaStatusBadge({ status }: { status: TenantQuotaSaturationStatus }) {
  const { t } = useTranslation()
  const Icon =
    status === 'saturated'
      ? ShieldAlert
      : status === 'watch'
        ? AlertTriangle
        : status === 'needs_data'
          ? DatabaseZap
          : ShieldCheck
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        tenantQuotaBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`reliability.tenant_quota.status.${status}`, status)}
    </span>
  )
}

function tenantQuotaIcon(key: TenantQuotaLaneKey) {
  switch (key) {
    case 'ingest':
      return KeyRound
    case 'enrichment':
      return BrainCircuit
    case 'mcp':
      return Bot
    case 'gdpr':
      return ShieldCheck
    case 'outbox':
      return PackageX
    default:
      return Gauge
  }
}

function tenantQuotaBadgeClass(status: TenantQuotaSaturationStatus) {
  switch (status) {
    case 'healthy':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'watch':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    case 'saturated':
      return 'border-red-200 bg-red-50 text-red-700'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50 text-sky-800'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}

function tenantQuotaSurfaceClass(status: TenantQuotaSaturationStatus) {
  switch (status) {
    case 'healthy':
      return 'border-emerald-200 bg-emerald-50/70'
    case 'watch':
      return 'border-amber-200 bg-amber-50/70'
    case 'saturated':
      return 'border-red-200 bg-red-50/70'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50/70'
    default:
      return 'border-border bg-muted/20'
  }
}
