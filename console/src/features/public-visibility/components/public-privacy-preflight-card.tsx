import {
  ArrowUpRight,
  ClipboardCheck,
  Eye,
  FileWarning,
  Fingerprint,
  Globe2,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  Undo2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  PublicPrivacyPreflight,
  PublicPrivacyPreflightLane,
  PublicPrivacyPreflightLaneKey,
  PublicPrivacyPreflightStatus,
} from '../public-privacy-preflight'

export function PublicPrivacyPreflightCard({ preflight }: { preflight: PublicPrivacyPreflight }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="public-privacy-preflight"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(248,250,252,0.985))] py-0 shadow-[0_28px_72px_-52px_rgba(15,23,42,0.22)]"
    >
      <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(248,250,252,0.86),rgba(255,255,255,0.94))] px-5 py-4 sm:px-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.18rem] tracking-tight">
              <FileWarning className="size-4 text-muted-foreground" />
              {t('public_visibility.privacy_preflight.title', 'Public privacy preflight')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-3xl text-sm leading-[1.35rem]">
              {t(
                'public_visibility.privacy_preflight.description',
                'Public access, moderation defaults, identity exposure, portal fields, and review recovery stay visible before content reaches public surfaces.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <PreflightTotal
              label={t('public_visibility.privacy_preflight.ready', 'Ready')}
              tone="ready"
              value={preflight.totals.ready}
            />
            <PreflightTotal
              label={t('public_visibility.privacy_preflight.watch', 'Watch')}
              tone="watch"
              value={preflight.totals.watch}
            />
            <PreflightTotal
              label={t('public_visibility.privacy_preflight.blocked', 'Blocked')}
              tone="blocked"
              value={preflight.totals.blocked}
            />
            <PreflightTotal
              label={t('public_visibility.privacy_preflight.needs_data', 'Needs data')}
              tone="needs_data"
              value={preflight.totals.needs_data}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5 sm:p-6">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-background/80 px-3 py-3 md:grid-cols-2">
          <PreflightFact
            label={t('public_visibility.privacy_preflight.fingerprint', 'Privacy fingerprint')}
            value={preflight.fingerprint}
          />
          <PreflightFact
            label={t('public_visibility.privacy_preflight.summary', 'Preflight decision')}
            value={preflight.summary}
          />
        </div>
        <div className="grid gap-3 xl:grid-cols-5">
          {preflight.lanes.map((lane) => (
            <PreflightLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function PreflightTotal({
  label,
  tone,
  value,
}: {
  label: string
  tone: PublicPrivacyPreflightStatus
  value: number
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', preflightSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function PreflightLaneCard({ lane }: { lane: PublicPrivacyPreflightLane }) {
  const { t } = useTranslation()
  const Icon = preflightIcon(lane.key)
  return (
    <div
      data-testid={`public-privacy-preflight-${lane.key}`}
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
        <PreflightStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <PreflightFact
          label={t('public_visibility.privacy_preflight.signal', 'Signal')}
          value={lane.signal}
        />
        <PreflightFact
          label={t('public_visibility.privacy_preflight.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <PreflightFact
          label={t('public_visibility.privacy_preflight.guardrail', 'Guardrail')}
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

function PreflightFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function PreflightStatusBadge({ status }: { status: PublicPrivacyPreflightStatus }) {
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
        preflightBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`public_visibility.privacy_preflight.status.${status}`, status)}
    </span>
  )
}

function preflightIcon(key: PublicPrivacyPreflightLaneKey) {
  switch (key) {
    case 'access_boundary':
      return Globe2
    case 'moderation_gate':
      return ShieldCheck
    case 'identity_exposure':
      return Fingerprint
    case 'submission_fields':
      return Eye
    case 'review_recovery':
      return Undo2
    default:
      return FileWarning
  }
}

function preflightBadgeClass(status: PublicPrivacyPreflightStatus) {
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

function preflightSurfaceClass(status: PublicPrivacyPreflightStatus) {
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
