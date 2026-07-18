import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  CheckCircle2,
  MinusCircle,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Loading } from '@/components/loading'
import { PageHero } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  type PreflightCategory,
  type PreflightCheckResult,
  type PreflightStatus,
  preflightQuery,
} from '@/features/system-readiness/api/get-preflight'

const CATEGORY_ORDER: PreflightCategory[] = [
  'config',
  'database',
  'migration',
  'backup',
  'encryption',
  'auth',
  'metrics',
  'worker',
]

/* ─── Status icon (inline, small) ─── */

function StatusDot({ status }: { status: PreflightStatus }) {
  switch (status) {
    case 'pass':
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-500" />
    case 'warn':
      return <AlertTriangle className="h-4 w-4 shrink-0 text-amber-500" />
    case 'fail':
      return <XCircle className="h-4 w-4 shrink-0 text-red-500" />
    case 'skipped':
      return <MinusCircle className="h-4 w-4 shrink-0 text-muted-foreground/50" />
  }
}

/* ─── Status hero card ─── */

const STATUS_CARD: Record<string, { bg: string; icon: string; pill: string }> = {
  pass: {
    bg: 'border-emerald-600/15 bg-emerald-600/[0.03] dark:border-emerald-500/15 dark:bg-emerald-500/[0.04]',
    icon: 'text-emerald-600 dark:text-emerald-400',
    pill: 'border-emerald-600/20 bg-emerald-600/10 text-emerald-700 dark:border-emerald-400/20 dark:bg-emerald-400/10 dark:text-emerald-300',
  },
  warn: {
    bg: 'border-amber-600/15 bg-amber-600/[0.03] dark:border-amber-500/15 dark:bg-amber-500/[0.04]',
    icon: 'text-amber-600 dark:text-amber-400',
    pill: 'border-amber-600/20 bg-amber-600/10 text-amber-700 dark:border-amber-400/20 dark:bg-amber-400/10 dark:text-amber-300',
  },
  fail: {
    bg: 'border-red-600/15 bg-red-600/[0.03] dark:border-red-500/15 dark:bg-red-500/[0.04]',
    icon: 'text-red-600 dark:text-red-400',
    pill: 'border-red-600/20 bg-red-600/10 text-red-700 dark:border-red-400/20 dark:bg-red-400/10 dark:text-red-300',
  },
}

function StatusCard({
  status,
  checks,
  elapsed,
  t,
}: {
  status: PreflightStatus
  checks: PreflightCheckResult[]
  elapsed: string
  t: (key: string, opts?: Record<string, unknown>) => string
}) {
  const pass = checks.filter((c) => c.status === 'pass').length
  const warn = checks.filter((c) => c.status === 'warn').length
  const fail = checks.filter((c) => c.status === 'fail').length
  const style = STATUS_CARD[status] ?? STATUS_CARD.pass

  const titleKey =
    status === 'fail'
      ? 'system_readiness.overall_fail'
      : status === 'warn'
        ? 'system_readiness.overall_warn'
        : 'system_readiness.overall_pass'

  const Icon = status === 'fail' ? ShieldAlert : ShieldCheck

  return (
    <div
      className={`flex flex-col items-center rounded-2xl border px-6 py-8 text-center transition-colors sm:py-10 ${style.bg}`}
    >
      <Icon className={`h-11 w-11 ${style.icon}`} strokeWidth={1.5} />
      <p className="mt-4 text-base font-semibold text-foreground">{t(titleKey)}</p>

      <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
        <span
          className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-xs font-medium ${style.pill}`}
        >
          {pass} {t('system_readiness.status.pass')}
        </span>
        {warn > 0 && (
          <span className="inline-flex items-center gap-1 rounded-full border border-amber-600/20 bg-amber-600/10 px-2.5 py-0.5 text-xs font-medium text-amber-700 dark:border-amber-400/20 dark:bg-amber-400/10 dark:text-amber-300">
            {warn} {t('system_readiness.status.warn')}
          </span>
        )}
        {fail > 0 && (
          <span className="inline-flex items-center gap-1 rounded-full border border-red-600/20 bg-red-600/10 px-2.5 py-0.5 text-xs font-medium text-red-700 dark:border-red-400/20 dark:bg-red-400/10 dark:text-red-300">
            {fail} {t('system_readiness.status.fail')}
          </span>
        )}
      </div>

      <p className="mt-3 text-xs text-muted-foreground/70">
        {t('system_readiness.summary_inline', {
          total: checks.length,
          elapsed,
        })}
      </p>
    </div>
  )
}

/* ─── Issues section (only rendered when fail/warn exist) ─── */

function IssuesSection({
  issues,
  t,
}: {
  issues: PreflightCheckResult[]
  t: (key: string) => string
}) {
  return (
    <div className="space-y-3">
      <h2 className="text-sm font-semibold text-foreground/80">
        {t('system_readiness.issues_title')}
      </h2>
      {issues.map((check) => {
        const isFail = check.status === 'fail'
        const accent = isFail
          ? 'border-l-red-500 dark:border-l-red-400'
          : 'border-l-amber-500 dark:border-l-amber-400'
        return (
          <Card key={check.name} className="overflow-hidden border-border/60 shadow-none">
            <div className={`border-l-[3px] ${accent}`}>
              <CardHeader className="pb-0 pt-4">
                <CardTitle className="flex items-center gap-2.5 text-sm font-medium">
                  <StatusDot status={check.status} />
                  <code className="font-mono text-[13px]">{check.name}</code>
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-2 pb-4 pl-[42px] pt-1.5">
                {check.message && (
                  <p className="text-[13px] leading-relaxed text-muted-foreground">
                    {check.message}
                  </p>
                )}
                {check.remediation && (
                  <div className="rounded-lg border border-border/40 bg-muted/20 px-3 py-2.5 text-[13px] leading-relaxed text-muted-foreground">
                    <span className="font-semibold text-foreground/70">
                      {t('system_readiness.remediation_label')}
                    </span>
                    {'  '}
                    {check.remediation}
                  </div>
                )}
              </CardContent>
            </div>
          </Card>
        )
      })}
    </div>
  )
}

/* ─── Check row (compact, two-line) ─── */

function CheckRow({ check }: { check: PreflightCheckResult }) {
  return (
    <div className="flex items-start gap-3 px-5 py-3.5">
      <div className="mt-0.5">
        <StatusDot status={check.status} />
      </div>
      <div className="min-w-0 flex-1">
        <code className="text-[13px] font-medium text-foreground/90">{check.name}</code>
        {check.message && (
          <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">{check.message}</p>
        )}
      </div>
    </div>
  )
}

/* ─── Main page ─── */

export function SystemReadinessPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data: report, isLoading, isFetching, error } = useQuery(preflightQuery())

  const checks = report?.checks ?? []
  const issues = checks.filter((c) => c.status === 'fail' || c.status === 'warn')

  return (
    <div className="space-y-10">
      <PageHero
        eyebrow={t('shell.groups.administration')}
        title={t('nav.system_readiness')}
        subtitle={t('system_readiness.subtitle')}
        actions={
          <Button
            variant="outline"
            size="sm"
            disabled={isFetching}
            onClick={
              /* v8 ignore next -- @preserve: refresh click only invalidates the already-covered readiness query. */
              () =>
                queryClient.invalidateQueries({
                  queryKey: ['console', 'system', 'preflight'],
                })
            }
          >
            {isFetching ? (
              <RefreshCw className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
            )}
            {t('system_readiness.refresh')}
          </Button>
        }
      />

      {isLoading && <Loading />}

      {error && (
        <Card className="border-red-600/20 bg-red-600/5 shadow-none dark:border-red-500/20 dark:bg-red-500/5">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm text-red-700 dark:text-red-400">
              <XCircle className="h-4 w-4" />
              {t('system_readiness.error_loading')}
            </CardTitle>
          </CardHeader>
        </Card>
      )}

      {report && (
        <>
          <StatusCard status={report.status} checks={checks} elapsed={report.elapsed} t={t} />

          {issues.length > 0 && <IssuesSection issues={issues} t={t} />}

          <div className="space-y-1.5">
            <h2 className="text-sm font-semibold text-foreground/80">
              {t('system_readiness.details_title')}
            </h2>
            <Card className="overflow-hidden border-border/70 shadow-none">
              <CardContent className="p-0">
                {CATEGORY_ORDER.map((cat, catIdx) => {
                  const catChecks = checks.filter((c) => c.category === cat)
                  if (catChecks.length === 0) return null
                  return (
                    <div key={cat}>
                      {catIdx > 0 && <div className="border-t border-border/40" />}
                      <div className="flex items-center gap-2 bg-muted/10 px-5 py-2.5">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground/70">
                          {t(`system_readiness.category.${cat}`)}
                        </h3>
                        <span className="text-[11px] text-muted-foreground/40">
                          {catChecks.length}
                        </span>
                      </div>
                      <div className="divide-y divide-border/25">
                        {catChecks.map((check) => (
                          <CheckRow key={check.name} check={check} />
                        ))}
                      </div>
                    </div>
                  )
                })}
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </div>
  )
}
