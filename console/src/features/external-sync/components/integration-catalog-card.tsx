import {
  ArrowUpRight,
  BadgeCheck,
  FileJson,
  GitBranch,
  HeartPulse,
  KeyRound,
  PackageCheck,
  PlugZap,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  Store,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  IntegrationCatalog,
  IntegrationCatalogConnector,
  IntegrationCatalogHealthBadge,
  IntegrationCatalogInstallStatus,
  IntegrationCatalogLane,
  IntegrationCatalogLaneKey,
  IntegrationCatalogStatus,
} from '../integration-catalog'

export function IntegrationCatalogCard({ catalog }: { catalog: IntegrationCatalog }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="external-sync-integration-catalog"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(248,250,252,0.985))] py-0 shadow-none"
    >
      <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(248,250,252,0.86),rgba(255,255,255,0.94))] px-5 py-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.05rem] tracking-tight">
              <Store className="size-4 text-muted-foreground" />
              {t('external_sync.integration_catalog.title', 'Integration catalog')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-2xl text-sm leading-[1.35rem]">
              {t(
                'external_sync.integration_catalog.description',
                'Connector catalog cards, install states, permissions, health badges, replay samples, and upgrade paths stay visible before rollout.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <IntegrationCatalogTotal
              label={t('external_sync.integration_catalog.verified', 'Verified')}
              tone="verified"
              value={catalog.totals.verified}
            />
            <IntegrationCatalogTotal
              label={t('external_sync.integration_catalog.watch', 'Watch')}
              tone="watch"
              value={catalog.totals.watch}
            />
            <IntegrationCatalogTotal
              label={t('external_sync.integration_catalog.blocked', 'Blocked')}
              tone="blocked"
              value={catalog.totals.blocked}
            />
            <IntegrationCatalogTotal
              label={t('external_sync.integration_catalog.needs_data', 'Needs data')}
              tone="needs_data"
              value={catalog.totals.needs_data}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-background/80 px-3 py-3 md:grid-cols-2">
          <IntegrationCatalogFact
            label={t('external_sync.integration_catalog.fingerprint', 'Catalog fingerprint')}
            value={catalog.fingerprint}
          />
          <IntegrationCatalogFact
            label={t('external_sync.integration_catalog.summary', 'Catalog decision')}
            value={catalog.summary}
          />
        </div>
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-6">
          {catalog.lanes.map((lane) => (
            <IntegrationCatalogLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
        <div className="space-y-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
            <PackageCheck className="size-4 text-muted-foreground" />
            {t('external_sync.integration_catalog.connector_matrix', 'Connector matrix')}
          </div>
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            {catalog.connectors.map((connector) => (
              <IntegrationCatalogConnectorCard key={connector.id} connector={connector} />
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function IntegrationCatalogTotal({
  label,
  tone,
  value,
}: {
  label: string
  tone: IntegrationCatalogStatus
  value: number
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', integrationCatalogSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function IntegrationCatalogLaneCard({ lane }: { lane: IntegrationCatalogLane }) {
  const { t } = useTranslation()
  const Icon = integrationCatalogIcon(lane.key)
  return (
    <div
      data-testid={`external-sync-integration-catalog-${lane.key}`}
      className="flex min-w-0 flex-col rounded-[0.95rem] border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="text-sm font-semibold leading-5 text-foreground">
              {t(`external_sync.integration_catalog.lanes.${lane.key}.title`, lane.title)}
            </div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {t(`external_sync.integration_catalog.lanes.${lane.key}.owner`, lane.owner)}
          </div>
        </div>
        <IntegrationCatalogStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <IntegrationCatalogFact
          label={t('external_sync.integration_catalog.signal', 'Signal')}
          value={lane.signal}
        />
        <IntegrationCatalogFact
          label={t('external_sync.integration_catalog.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <IntegrationCatalogFact
          label={t('external_sync.integration_catalog.detail', 'Detail')}
          value={lane.detail}
        />
      </div>
      <a
        href="https://github.com/Phixsura/attune/tree/main/integrations/integration-catalog"
        className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <span className="min-w-0 truncate">
          {t(`external_sync.integration_catalog.lanes.${lane.key}.action`, lane.actionLabel)}
        </span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function IntegrationCatalogConnectorCard({
  connector,
}: {
  connector: IntegrationCatalogConnector
}) {
  const { t } = useTranslation()
  return (
    <div
      data-testid={`external-sync-integration-catalog-connector-${connector.id}`}
      className="min-w-0 rounded-[0.95rem] border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold text-foreground">
            {connector.displayName}
          </div>
          <div className="mt-1 text-xs text-muted-foreground">{connector.category}</div>
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1">
          <IntegrationCatalogInstallBadge status={connector.runtimeInstallStatus} />
          <IntegrationCatalogHealthPill badge={connector.runtimeHealthBadge} />
        </div>
      </div>
      <p className="mt-3 min-h-10 text-xs leading-5 text-muted-foreground">
        {connector.description}
      </p>
      <div className="mt-3 grid gap-2">
        <IntegrationCatalogFact
          label={t('external_sync.integration_catalog.permissions', 'Permissions')}
          value={`${connector.scopes.length} scopes / ${connector.dataClasses.join(', ')}`}
        />
        <IntegrationCatalogFact
          label={t('external_sync.integration_catalog.sample_replay', 'Sample replay')}
          value={`${connector.replayEvent} -> ${connector.replayNormalizedType}`}
        />
        <IntegrationCatalogFact
          label={t('external_sync.integration_catalog.upgrade', 'Upgrade')}
          value={`${connector.version} / ${connector.upgradePath}`}
        />
        <IntegrationCatalogFact
          label={t('external_sync.integration_catalog.live_connection', 'Live connection')}
          value={
            connector.liveConnectionName
              ? t('external_sync.integration_catalog.installed_runtime')
              : t('external_sync.integration_catalog.not_installed')
          }
        />
      </div>
    </div>
  )
}

function IntegrationCatalogFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function IntegrationCatalogStatusBadge({ status }: { status: IntegrationCatalogStatus }) {
  const { t } = useTranslation()
  const Icon =
    status === 'blocked'
      ? ShieldAlert
      : status === 'watch'
        ? SlidersHorizontal
        : status === 'needs_data'
          ? BadgeCheck
          : ShieldCheck
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        integrationCatalogBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`external_sync.integration_catalog.status.${status}`, status)}
    </span>
  )
}

function IntegrationCatalogInstallBadge({ status }: { status: IntegrationCatalogInstallStatus }) {
  const { t } = useTranslation()
  return (
    <span className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] font-medium text-slate-700">
      {t(`external_sync.integration_catalog.install_status.${status}`, status)}
    </span>
  )
}

function IntegrationCatalogHealthPill({ badge }: { badge: IntegrationCatalogHealthBadge }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'rounded-full border px-2 py-0.5 text-[11px] font-medium',
        healthPillClass(badge),
      )}
    >
      {t(`external_sync.integration_catalog.health_badge.${badge}`, badge)}
    </span>
  )
}

function integrationCatalogIcon(key: IntegrationCatalogLaneKey) {
  switch (key) {
    case 'catalog_cards':
      return Store
    case 'install_status':
      return PlugZap
    case 'permission_scope':
      return KeyRound
    case 'health_badge':
      return HeartPulse
    case 'sample_replay':
      return FileJson
    case 'upgrade_path':
      return GitBranch
    default:
      return PackageCheck
  }
}

function integrationCatalogBadgeClass(status: IntegrationCatalogStatus) {
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

function integrationCatalogSurfaceClass(status: IntegrationCatalogStatus) {
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

function healthPillClass(badge: IntegrationCatalogHealthBadge) {
  switch (badge) {
    case 'healthy':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'ready':
      return 'border-sky-200 bg-sky-50 text-sky-800'
    case 'watch':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    case 'degraded':
      return 'border-red-200 bg-red-50 text-red-700'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}
