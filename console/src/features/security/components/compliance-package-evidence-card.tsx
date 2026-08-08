import {
  ArrowUpRight,
  ClipboardCheck,
  DatabaseZap,
  FileWarning,
  Fingerprint,
  Globe2,
  Network,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  CompliancePackageEvidence,
  CompliancePackageLane,
  CompliancePackageLaneKey,
  CompliancePackageStatus,
} from '../compliance-package-evidence'

export function CompliancePackageEvidenceCard({
  evidence,
}: {
  evidence: CompliancePackageEvidence
}) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="security-compliance-package-evidence"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(248,250,252,0.985))] py-0 shadow-[0_28px_72px_-52px_rgba(15,23,42,0.22)]"
    >
      <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(248,250,252,0.86),rgba(255,255,255,0.94))] px-5 py-4 sm:px-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.18rem] tracking-tight">
              <FileWarning className="size-4 text-muted-foreground" />
              {t('security.compliance_package.title', 'Compliance package evidence')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-3xl text-sm leading-[1.35rem]">
              {t(
                'security.compliance_package.description',
                'DPA, SOC2-ready controls, data flow inventory, audit export, retention, and subprocessor boundaries stay in one customer-verifiable package.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <ComplianceTotal
              label={t('security.compliance_package.ready', 'Ready')}
              tone="ready"
              value={evidence.totals.ready}
            />
            <ComplianceTotal
              label={t('security.compliance_package.watch', 'Watch')}
              tone="watch"
              value={evidence.totals.watch}
            />
            <ComplianceTotal
              label={t('security.compliance_package.blocked', 'Blocked')}
              tone="blocked"
              value={evidence.totals.blocked}
            />
            <ComplianceTotal
              label={t('security.compliance_package.needs_data', 'Needs data')}
              tone="needs_data"
              value={evidence.totals.needs_data}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5 sm:p-6">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-background/80 px-3 py-3 md:grid-cols-2">
          <ComplianceFact
            label={t('security.compliance_package.fingerprint', 'Compliance fingerprint')}
            value={evidence.fingerprint}
          />
          <ComplianceFact
            label={t('security.compliance_package.summary', 'Compliance decision')}
            value={evidence.summary}
          />
        </div>
        <div className="grid gap-3 xl:grid-cols-5">
          {evidence.lanes.map((lane) => (
            <ComplianceLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function ComplianceTotal({
  label,
  tone,
  value,
}: {
  label: string
  tone: CompliancePackageStatus
  value: number
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', complianceSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function ComplianceLaneCard({ lane }: { lane: CompliancePackageLane }) {
  const { t } = useTranslation()
  const Icon = complianceIcon(lane.key)
  return (
    <div
      data-testid={`security-compliance-package-${lane.key}`}
      className="flex min-w-0 flex-col rounded-[0.95rem] border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="truncate text-sm font-semibold text-foreground">
              {t(`security.compliance_package.lanes.${lane.key}.title`, lane.title)}
            </div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {t(`security.compliance_package.lanes.${lane.key}.owner`, lane.owner)}
          </div>
        </div>
        <ComplianceStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <ComplianceFact
          label={t('security.compliance_package.signal', 'Signal')}
          value={lane.signal}
        />
        <ComplianceFact
          label={t('security.compliance_package.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <ComplianceFact
          label={t('security.compliance_package.guardrail', 'Guardrail')}
          value={lane.guardrail}
        />
      </div>
      <a
        href={lane.actionHref}
        className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <span className="min-w-0 truncate">
          {t(`security.compliance_package.lanes.${lane.key}.action`, lane.actionLabel)}
        </span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function ComplianceFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function ComplianceStatusBadge({ status }: { status: CompliancePackageStatus }) {
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
        complianceBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`security.compliance_package.status.${status}`, status)}
    </span>
  )
}

function complianceIcon(key: CompliancePackageLaneKey) {
  switch (key) {
    case 'control_mapping':
      return Fingerprint
    case 'data_flow_inventory':
      return Globe2
    case 'audit_evidence_package':
      return FileWarning
    case 'retention_dsr':
      return DatabaseZap
    case 'subprocessor_boundary':
      return Network
    default:
      return FileWarning
  }
}

function complianceBadgeClass(status: CompliancePackageStatus) {
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

function complianceSurfaceClass(status: CompliancePackageStatus) {
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
