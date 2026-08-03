import {
  AlertTriangle,
  ArrowUpRight,
  ClipboardCheck,
  Fingerprint,
  KeyRound,
  ShieldAlert,
  ShieldCheck,
  UserCheck,
  UserCog,
  UsersRound,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  GovernanceRbacLane,
  GovernanceRbacLaneKey,
  GovernanceRbacReadiness,
  GovernanceRbacStatus,
} from '../governance-rbac-readiness'

export function GovernanceRbacReadinessCard({ readiness }: { readiness: GovernanceRbacReadiness }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="security-governance-rbac-readiness"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(249,250,251,0.985))] py-0 shadow-[0_28px_72px_-52px_rgba(15,23,42,0.22)]"
    >
      <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(248,250,252,0.82),rgba(255,255,255,0.92))] px-5 py-4 sm:px-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.18rem] tracking-tight">
              <Fingerprint className="size-4 text-muted-foreground" />
              {t('security.governance.title', 'Governance / RBAC readiness')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-3xl text-sm leading-[1.35rem]">
              {t(
                'security.governance.description',
                'SSO, SCIM or IdP role sources, RBAC coverage, last-admin continuity, and access-review audit evidence stay visible in one control surface.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <GovernanceTotal
              label={t('security.governance.ready', 'Ready')}
              value={readiness.totals.ready}
              tone="ready"
            />
            <GovernanceTotal
              label={t('security.governance.watch', 'Watch')}
              value={readiness.totals.watch}
              tone="watch"
            />
            <GovernanceTotal
              label={t('security.governance.blocked', 'Blocked')}
              value={readiness.totals.blocked}
              tone="blocked"
            />
            <GovernanceTotal
              label={t('security.governance.needs_data', 'Needs data')}
              value={readiness.totals.needs_data}
              tone="needs_data"
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5 sm:p-6">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-background/80 px-3 py-3 md:grid-cols-2">
          <GovernanceFact
            label={t('security.governance.fingerprint', 'Governance fingerprint')}
            value={readiness.fingerprint}
          />
          <GovernanceFact
            label={t('security.governance.summary', 'Governance decision')}
            value={readiness.summary}
          />
        </div>
        <div className="grid gap-3 xl:grid-cols-5">
          {readiness.lanes.map((lane) => (
            <GovernanceLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function GovernanceTotal({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: GovernanceRbacStatus
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', governanceSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function GovernanceLaneCard({ lane }: { lane: GovernanceRbacLane }) {
  const { t } = useTranslation()
  const Icon = governanceIcon(lane.key)
  return (
    <div
      data-testid={`security-governance-rbac-${lane.key}`}
      className="flex min-w-0 flex-col rounded-[0.95rem] border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="truncate text-sm font-semibold text-foreground">{lane.title}</div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">{lane.owner}</div>
        </div>
        <GovernanceStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <GovernanceFact label={t('security.governance.signal', 'Signal')} value={lane.signal} />
        <GovernanceFact
          label={t('security.governance.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <GovernanceFact
          label={t('security.governance.guardrail', 'Guardrail')}
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

function GovernanceFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function GovernanceStatusBadge({ status }: { status: GovernanceRbacStatus }) {
  const { t } = useTranslation()
  const Icon =
    status === 'blocked'
      ? ShieldAlert
      : status === 'watch'
        ? AlertTriangle
        : status === 'needs_data'
          ? ClipboardCheck
          : ShieldCheck
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        governanceBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`security.governance.status.${status}`, status)}
    </span>
  )
}

function governanceIcon(key: GovernanceRbacLaneKey) {
  switch (key) {
    case 'sso_breakglass':
      return KeyRound
    case 'scim_idp':
      return UserCheck
    case 'rbac_roles':
      return UserCog
    case 'last_admin_guard':
      return UsersRound
    case 'access_review':
      return ClipboardCheck
    default:
      return Fingerprint
  }
}

function governanceBadgeClass(status: GovernanceRbacStatus) {
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

function governanceSurfaceClass(status: GovernanceRbacStatus) {
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
