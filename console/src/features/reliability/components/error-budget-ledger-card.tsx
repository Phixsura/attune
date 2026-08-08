import {
  AlertTriangle,
  ArrowUpRight,
  BarChart3,
  ClipboardList,
  DatabaseZap,
  ShieldAlert,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  ErrorBudgetLedger,
  ErrorBudgetLedgerEntry,
  ErrorBudgetLedgerStatus,
} from '../error-budget-ledger'

export function ErrorBudgetLedgerCard({ ledger }: { ledger: ErrorBudgetLedger }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="reliability-error-budget-ledger"
      className="border-border/60 bg-background/95 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <BarChart3 className="size-4 text-muted-foreground" />
              {t('reliability.error_budget_ledger.title', 'Error budget / burn-rate ledger')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-3xl">
              {t(
                'reliability.error_budget_ledger.description',
                'Each service SLO carries objective, burn query, remaining-budget evidence, exception policy, and incident proof in one auditable ledger.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <LedgerTotal
              label={t('reliability.error_budget_ledger.monitored', 'Monitored')}
              value={ledger.totals.monitored}
              tone="monitored"
            />
            <LedgerTotal
              label={t('reliability.error_budget_ledger.attention', 'Attention')}
              value={ledger.totals.attention}
              tone="attention"
            />
            <LedgerTotal
              label={t('reliability.error_budget_ledger.blocked', 'Blocked')}
              value={ledger.totals.blocked}
              tone="blocked"
            />
            <LedgerTotal
              label={t('reliability.error_budget_ledger.needs_data', 'Needs data')}
              value={ledger.totals.needs_data}
              tone="needs_data"
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 pt-4">
        {ledger.entries.map((entry) => (
          <ErrorBudgetLedgerRow key={entry.key} entry={entry} />
        ))}
      </CardContent>
    </Card>
  )
}

function LedgerTotal({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: ErrorBudgetLedgerStatus
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', ledgerSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function ErrorBudgetLedgerRow({ entry }: { entry: ErrorBudgetLedgerEntry }) {
  const { t } = useTranslation()
  return (
    <div
      data-testid={`reliability-error-budget-ledger-${entry.key}`}
      className="grid gap-3 rounded-md border border-border/60 bg-muted/10 px-3 py-3 xl:grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)_minmax(0,1.15fr)_12rem]"
    >
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <LedgerStatusBadge status={entry.status} />
          <div className="truncate text-sm font-semibold text-foreground">{entry.title}</div>
        </div>
        <div className="mt-1 text-xs leading-5 text-muted-foreground">
          {entry.owner} / {entry.escalation} / {entry.scopeLabel}
        </div>
        <div className="mt-2 rounded-sm border border-border/60 bg-background/75 px-2 py-1.5 font-mono text-xs text-muted-foreground">
          {entry.currentSignal}
        </div>
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        <LedgerFact
          label={t('reliability.error_budget_ledger.objective', 'Objective')}
          value={`${entry.objectiveLabel} / ${entry.budgetAllowanceLabel}`}
        />
        <LedgerFact
          label={t('reliability.error_budget_ledger.thresholds', 'Thresholds')}
          value={`${entry.fastBurnThreshold} / ${entry.slowBurnThreshold}`}
        />
        <LedgerFact
          label={t('reliability.error_budget_ledger.exception_policy', 'Exception policy')}
          value={entry.exceptionPolicy}
        />
        <LedgerFact
          label={t('reliability.error_budget_ledger.incident_evidence', 'Incident evidence')}
          value={entry.incidentEvidence}
        />
      </div>

      <div className="grid gap-2">
        <LedgerCodeFact
          label={t('reliability.error_budget_ledger.burn_rate_query', 'Burn-rate query')}
          value={entry.burnRateQuery}
        />
        <LedgerCodeFact
          label={t(
            'reliability.error_budget_ledger.remaining_budget_query',
            'Remaining-budget query',
          )}
          value={entry.remainingBudgetQuery}
        />
      </div>

      <div className="flex min-w-0 flex-col gap-2">
        <a
          href={entry.dashboardHref}
          target="_blank"
          rel="noreferrer"
          className="inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
        >
          <DatabaseZap className="size-4" />
          <span className="min-w-0 truncate">
            {t('reliability.error_budget_ledger.open_dashboard', 'Open dashboard')}
          </span>
          <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
        </a>
        <a
          href={entry.runbookHref}
          target="_blank"
          rel="noreferrer"
          className="inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
        >
          <ClipboardList className="size-4" />
          <span className="min-w-0 truncate">
            {t('reliability.error_budget_ledger.open_runbook', 'Open runbook')}
          </span>
          <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
        </a>
      </div>
    </div>
  )
}

function LedgerFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function LedgerCodeFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <code className="mt-1 block break-all rounded-sm bg-muted/35 px-2 py-1.5 font-mono text-[11px] leading-5 text-foreground">
        {value}
      </code>
    </div>
  )
}

function LedgerStatusBadge({ status }: { status: ErrorBudgetLedgerStatus }) {
  const { t } = useTranslation()
  const Icon =
    status === 'blocked'
      ? ShieldAlert
      : status === 'attention'
        ? AlertTriangle
        : status === 'needs_data'
          ? DatabaseZap
          : ShieldCheck
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        ledgerBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {ledgerStatusLabel(status, t)}
    </span>
  )
}

function ledgerStatusLabel(status: ErrorBudgetLedgerStatus, t: (key: string) => string) {
  switch (status) {
    case 'monitored':
      return t('reliability.error_budget_ledger.status.monitored')
    case 'attention':
      return t('reliability.error_budget_ledger.status.attention')
    case 'blocked':
      return t('reliability.error_budget_ledger.status.blocked')
    case 'needs_data':
      return t('reliability.error_budget_ledger.status.needs_data')
    default:
      return status
  }
}

function ledgerBadgeClass(status: ErrorBudgetLedgerStatus) {
  switch (status) {
    case 'monitored':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'attention':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    case 'blocked':
      return 'border-red-200 bg-red-50 text-red-700'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50 text-sky-800'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}

function ledgerSurfaceClass(status: ErrorBudgetLedgerStatus) {
  switch (status) {
    case 'monitored':
      return 'border-emerald-200 bg-emerald-50/70'
    case 'attention':
      return 'border-amber-200 bg-amber-50/70'
    case 'blocked':
      return 'border-red-200 bg-red-50/70'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50/70'
    default:
      return 'border-border bg-muted/20'
  }
}
