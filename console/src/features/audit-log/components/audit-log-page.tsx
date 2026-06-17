import { useInfiniteQuery } from '@tanstack/react-query'
import { format, formatDistanceToNow, type Locale } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import type { LucideIcon } from 'lucide-react'
import {
  CalendarClock,
  ChevronDown,
  Download,
  FileSearch,
  Filter,
  Loader2,
  ScanSearch,
  ShieldCheck,
  TimerReset,
  X,
} from 'lucide-react'
import { startTransition, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { auditActionOptions } from '@/features/audit-log/actions'
import {
  type AuditLogEntry,
  type AuditLogFilters,
  auditLogInfiniteQuery,
  downloadAuditLogCsv,
} from '@/features/audit-log/api/list-audit-log'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { cn } from '@/lib/utils'

export function AuditLogPage() {
  const { t, i18n } = useTranslation()
  const { can } = usePermissions()
  const canView = can('settings:audit_log:view')
  const locale = i18n.language.startsWith('zh') ? zhCN : undefined

  const [selectedActions, setSelectedActions] = useState<string[]>([])
  const [targetType, setTargetType] = useState('')
  const [actorId, setActorId] = useState('')
  const [fromValue, setFromValue] = useState('')
  const [toValue, setToValue] = useState('')
  const [filters, setFilters] = useState<AuditLogFilters>({ limit: 50 })
  const [isExporting, setIsExporting] = useState(false)

  const query = useInfiniteQuery(auditLogInfiniteQuery(filters))
  const items = query.data?.pages.flatMap((page) => page.items ?? []) ?? []
  const latestEntry = items[0]
  const uniqueActions = new Set(items.map((item) => item.action)).size
  const activeFilterCount = [
    filters.actions?.length ?? 0,
    filters.actorId ? 1 : 0,
    filters.targetType ? 1 : 0,
    filters.from ? 1 : 0,
    filters.to ? 1 : 0,
  ].reduce((sum, count) => sum + (count > 0 ? 1 : 0), 0)

  const handleExport = async () => {
    const exportFilters = {
      actions: filters.actions,
      actorId: filters.actorId,
      from: filters.from,
      targetType: filters.targetType,
      to: filters.to,
    }
    setIsExporting(true)
    try {
      await downloadAuditLogCsv(exportFilters)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('audit_log.export_failed'))
    } finally {
      setIsExporting(false)
    }
  }

  const handleApply = () => {
    startTransition(() => {
      setFilters({
        actions: selectedActions.length > 0 ? selectedActions : undefined,
        actorId: actorId.trim() || undefined,
        from: localDateTimeToISO(fromValue),
        limit: 50,
        targetType: targetType.trim() || undefined,
        to: localDateTimeToISO(toValue),
      })
    })
  }

  const handleReset = () => {
    setSelectedActions([])
    setTargetType('')
    setActorId('')
    setFromValue('')
    setToValue('')
    startTransition(() => {
      setFilters({ limit: 50 })
    })
  }

  if (!canView) {
    return (
      <EmptyState
        icon={FileSearch}
        title={t('audit_log.empty_title')}
        description={t('audit_log.empty_body')}
      />
    )
  }

  return (
    <section className="space-y-6">
      <header className="overflow-hidden rounded-[1.5rem] border border-primary/15 bg-[linear-gradient(135deg,rgba(255,255,255,0.98),rgba(255,248,240,0.96)_46%,rgba(255,238,219,0.88))] shadow-[0_1px_0_rgba(15,23,42,0.03),0_18px_50px_-28px_rgba(234,88,12,0.35)]">
        <div className="grid gap-6 px-6 py-6 lg:grid-cols-[minmax(0,1.45fr)_minmax(18rem,0.9fr)] lg:px-8 lg:py-8">
          <div className="space-y-5">
            <div className="inline-flex items-center gap-2 rounded-full border border-primary/20 bg-white/80 px-3 py-1 text-[11px] font-semibold tracking-[0.18em] text-primary uppercase shadow-sm backdrop-blur">
              <ShieldCheck className="h-3.5 w-3.5" />
              {t('audit_log.hero_eyebrow')}
            </div>
            <div className="space-y-2">
              <h1 className="max-w-3xl text-3xl font-semibold tracking-[-0.04em] text-foreground [text-wrap:balance] md:text-[2.55rem]">
                {t('audit_log.title')}
              </h1>
              <p className="max-w-3xl text-sm leading-6 text-muted-foreground [text-wrap:pretty] md:text-[15px]">
                {t('audit_log.subtitle')}
              </p>
            </div>
            <div className="flex flex-wrap gap-3">
              <Button type="button" disabled={isExporting} onClick={() => void handleExport()}>
                <Download className="mr-2 h-4 w-4" />
                {isExporting ? t('audit_log.exporting') : t('audit_log.export')}
              </Button>
              <div className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-white/72 px-3 py-2 text-xs text-muted-foreground shadow-sm backdrop-blur">
                <ScanSearch className="h-3.5 w-3.5 text-primary" />
                {t('audit_log.table_help')}
              </div>
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-3 lg:grid-cols-1 xl:grid-cols-3">
            <StatCard
              icon={FileSearch}
              label={t('audit_log.stats.loaded')}
              value={String(items.length)}
              hint={t('audit_log.stats.loaded_hint')}
            />
            <StatCard
              icon={Filter}
              label={t('audit_log.stats.filters')}
              value={String(activeFilterCount)}
              hint={
                activeFilterCount > 0
                  ? t('audit_log.stats.filters_active')
                  : t('audit_log.stats.filters_idle')
              }
            />
            <StatCard
              icon={CalendarClock}
              label={t('audit_log.stats.latest')}
              value={
                latestEntry
                  ? formatDistanceToNow(new Date(latestEntry.createdAt), {
                      addSuffix: true,
                      locale,
                    })
                  : t('audit_log.stats.latest_empty')
              }
              hint={
                latestEntry
                  ? formatAbsoluteTimestamp(latestEntry.createdAt, locale)
                  : t('audit_log.stats.latest_hint')
              }
            />
            <StatCard
              icon={ShieldCheck}
              label={t('audit_log.stats.actions')}
              value={String(uniqueActions)}
              hint={t('audit_log.stats.actions_hint')}
              className="sm:col-span-3 lg:col-span-1 xl:col-span-3"
            />
          </div>
        </div>
      </header>

      <div className="grid gap-6 xl:grid-cols-[minmax(18rem,22rem)_minmax(0,1fr)]">
        <Card className="overflow-hidden border-border/70 shadow-[0_1px_0_rgba(15,23,42,0.02),0_16px_40px_-30px_rgba(15,23,42,0.24)] xl:sticky xl:top-6 xl:self-start">
          <CardHeader className="border-b border-border/70 bg-[linear-gradient(180deg,rgba(248,250,252,0.9),rgba(255,255,255,0.95))]">
            <div className="inline-flex items-center gap-2 text-xs font-semibold tracking-[0.18em] text-muted-foreground uppercase">
              <Filter className="h-3.5 w-3.5 text-primary" />
              {t('audit_log.filters_title')}
            </div>
            <CardTitle className="text-xl tracking-[-0.03em]">
              {t('audit_log.filters_panel_title')}
            </CardTitle>
            <CardDescription className="max-w-md leading-6">
              {t('audit_log.filters_panel_body')}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-5 px-6 py-6">
            <FilterField label={t('audit_log.fields.action')} htmlFor="audit-actions-trigger">
              <AuditActionMultiSelect
                selected={selectedActions}
                onChange={setSelectedActions}
                triggerId="audit-actions-trigger"
              />
            </FilterField>

            <div className="grid gap-5 md:grid-cols-2 xl:grid-cols-1">
              <FilterField label={t('audit_log.fields.target_type')} htmlFor="audit-target-type">
                <Input
                  id="audit-target-type"
                  value={targetType}
                  onChange={(e) => setTargetType(e.target.value)}
                  placeholder={t('audit_log.placeholders.target_type')}
                />
              </FilterField>

              <FilterField label={t('audit_log.fields.actor_id')} htmlFor="audit-actor-id">
                <Input
                  id="audit-actor-id"
                  value={actorId}
                  onChange={(e) => setActorId(e.target.value)}
                  placeholder={t('audit_log.placeholders.actor_id')}
                />
              </FilterField>

              <FilterField label={t('audit_log.filters_from')} htmlFor="audit-from">
                <Input
                  id="audit-from"
                  type="datetime-local"
                  value={fromValue}
                  onChange={(e) => setFromValue(e.target.value)}
                />
              </FilterField>

              <FilterField label={t('audit_log.filters_to')} htmlFor="audit-to">
                <Input
                  id="audit-to"
                  type="datetime-local"
                  value={toValue}
                  onChange={(e) => setToValue(e.target.value)}
                />
              </FilterField>
            </div>

            <div className="rounded-2xl border border-border/60 bg-muted/20 p-3">
              <div className="mb-2 text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                {t('audit_log.filters_summary_title')}
              </div>
              <div className="flex flex-wrap gap-2">
                <FilterChip active={selectedActions.length > 0}>
                  {selectedActions.length > 0
                    ? t('audit_log.filters_actions_selected', { count: selectedActions.length })
                    : t('audit_log.filters_actions_placeholder')}
                </FilterChip>
                <FilterChip active={Boolean(targetType)}>
                  {targetType || t('audit_log.fields.target_type')}
                </FilterChip>
                <FilterChip active={Boolean(actorId)}>
                  {actorId || t('audit_log.fields.actor_id')}
                </FilterChip>
                <FilterChip active={Boolean(fromValue)}>
                  {fromValue || t('audit_log.filters_from')}
                </FilterChip>
                <FilterChip active={Boolean(toValue)}>
                  {toValue || t('audit_log.filters_to')}
                </FilterChip>
              </div>
            </div>

            <div className="flex flex-wrap gap-3 pt-1">
              <Button type="button" className="min-w-32" onClick={handleApply}>
                {t('audit_log.apply')}
              </Button>
              <Button type="button" variant="ghost" className="min-w-28" onClick={handleReset}>
                <TimerReset className="mr-2 h-4 w-4" />
                {t('audit_log.reset')}
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card className="overflow-hidden border-border/70 shadow-[0_1px_0_rgba(15,23,42,0.02),0_16px_40px_-32px_rgba(15,23,42,0.22)]">
          <CardHeader className="border-b border-border/70 bg-[linear-gradient(180deg,rgba(255,255,255,0.98),rgba(248,250,252,0.86))]">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div className="space-y-2">
                <div className="inline-flex items-center gap-2 text-xs font-semibold tracking-[0.18em] text-muted-foreground uppercase">
                  <ScanSearch className="h-3.5 w-3.5 text-primary" />
                  {t('audit_log.table_title')}
                </div>
                <CardTitle className="text-xl tracking-[-0.03em]">
                  {t('audit_log.stream_title')}
                </CardTitle>
                <CardDescription className="max-w-2xl leading-6">
                  {t('audit_log.stream_help')}
                </CardDescription>
              </div>
              <div className="rounded-2xl border border-border/70 bg-white/80 px-4 py-3 shadow-sm">
                <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                  {t('audit_log.stream_count_label')}
                </div>
                <div className="mt-1 text-2xl font-semibold tracking-[-0.04em] text-foreground tabular-nums">
                  {items.length}
                </div>
              </div>
            </div>
          </CardHeader>
          <CardContent className="px-0 py-0">
            {query.isPending ? (
              <div className="flex items-center justify-center py-16 text-muted-foreground">
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                {t('app.loading')}
              </div>
            ) : query.isError ? (
              <EmptyState
                icon={FileSearch}
                title={t('audit_log.error_title')}
                description={t('audit_log.error_body')}
                action={{ label: t('audit_log.retry'), onClick: () => void query.refetch() }}
                className="px-6 py-16"
              />
            ) : items.length > 0 ? (
              <div className="space-y-0">
                <AuditLogStream items={items} locale={locale} />
                {query.hasNextPage ? (
                  <div className="border-t border-border/70 px-6 py-5">
                    <Button
                      type="button"
                      variant="outline"
                      className="w-full sm:w-auto"
                      disabled={query.isFetchingNextPage}
                      onClick={() => void query.fetchNextPage()}
                    >
                      {query.isFetchingNextPage ? (
                        <>
                          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                          {t('app.loading')}
                        </>
                      ) : (
                        t('audit_log.load_more')
                      )}
                    </Button>
                  </div>
                ) : null}
              </div>
            ) : (
              <EmptyState
                icon={FileSearch}
                title={t('audit_log.empty_title')}
                description={t('audit_log.empty_body')}
                className="px-6 py-16"
              />
            )}
          </CardContent>
        </Card>
      </div>
    </section>
  )
}

function AuditLogStream({ items, locale }: { items: AuditLogEntry[]; locale?: Locale }) {
  const { t } = useTranslation()

  return (
    <div className="divide-y divide-border/70">
      {items.map((item) => (
        <article
          key={item.id}
          className="group px-6 py-5 transition-colors hover:bg-muted/[0.16] md:px-7"
        >
          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_15rem] lg:items-start">
            <div className="space-y-4">
              <div className="flex flex-wrap items-start gap-3">
                <AuditToken className="border-primary/15 bg-primary/8 text-primary">
                  {item.action}
                </AuditToken>
                <AuditToken>{item.targetType || t('audit_log.target_unknown')}</AuditToken>
                <AuditToken>{item.actorType || t('audit_log.actor_unknown')}</AuditToken>
              </div>

              <div className="space-y-2">
                <div className="text-lg font-semibold tracking-[-0.03em] text-foreground [text-wrap:balance]">
                  {item.summary || t('audit_log.summary_fallback')}
                </div>
                <p className="text-sm leading-6 text-muted-foreground">
                  <span className="font-medium text-foreground">{t('audit_log.actor_line')}</span>{' '}
                  <span className="font-mono text-[13px]">
                    {item.actorId || t('audit_log.actor_unknown')}
                  </span>
                  {' · '}
                  <span className="font-medium text-foreground">{t('audit_log.target_line')}</span>{' '}
                  <span className="font-mono text-[13px]">
                    {item.targetId || t('audit_log.target_unknown')}
                  </span>
                </p>
              </div>

              <div className="grid gap-3 md:grid-cols-2">
                <EntityPanel
                  title={t('audit_log.fields.actor')}
                  primary={item.actorType || t('audit_log.actor_unknown')}
                  secondary={item.actorId || t('audit_log.actor_unknown')}
                />
                <EntityPanel
                  title={t('audit_log.fields.target')}
                  primary={item.targetType || t('audit_log.target_unknown')}
                  secondary={item.targetId || t('audit_log.target_unknown')}
                />
              </div>

              <details className="group rounded-2xl border border-border/70 bg-white/85 px-4 py-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.75)]">
                <summary className="cursor-pointer list-none text-sm font-medium text-primary">
                  <span className="inline-flex items-center gap-2">
                    <ChevronDown className="h-4 w-4 transition group-open:rotate-180" />
                    {t('audit_log.fields.details')}
                  </span>
                </summary>
                <div className="mt-4 space-y-4">
                  <RequestMetadata item={item} />
                  <div className="grid gap-4 xl:grid-cols-2">
                    <JsonBlock label={t('audit_log.before')} value={item.beforeJson} />
                    <JsonBlock label={t('audit_log.after')} value={item.afterJson} />
                  </div>
                </div>
              </details>
            </div>

            <aside className="space-y-3 rounded-2xl border border-border/70 bg-[linear-gradient(180deg,rgba(248,250,252,0.86),rgba(255,255,255,0.96))] p-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.75)]">
              <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                {t('audit_log.fields.created_at')}
              </div>
              <div className="text-base font-semibold tracking-[-0.03em] text-foreground tabular-nums">
                {formatDistanceToNow(new Date(item.createdAt), { addSuffix: true, locale })}
              </div>
              <div className="text-sm leading-6 text-muted-foreground tabular-nums">
                {formatAbsoluteTimestamp(item.createdAt, locale)}
              </div>
            </aside>
          </div>
        </article>
      ))}
    </div>
  )
}

function FilterField({
  children,
  htmlFor,
  label,
}: {
  children: React.ReactNode
  htmlFor: string
  label: string
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={htmlFor} className="text-[11px] font-semibold tracking-[0.16em] uppercase">
        {label}
      </Label>
      {children}
    </div>
  )
}

function StatCard({
  className,
  hint,
  icon: Icon,
  label,
  value,
}: {
  className?: string
  hint: string
  icon: LucideIcon
  label: string
  value: string
}) {
  return (
    <div
      className={cn(
        'rounded-[1.15rem] border border-white/70 bg-white/82 p-4 shadow-[0_8px_30px_-24px_rgba(15,23,42,0.4)] backdrop-blur',
        className,
      )}
    >
      <div className="flex items-center justify-between gap-3">
        <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
          {label}
        </div>
        <div className="rounded-full bg-primary/10 p-2 text-primary">
          <Icon className="h-4 w-4" />
        </div>
      </div>
      <div className="mt-4 text-2xl font-semibold tracking-[-0.04em] text-foreground [text-wrap:balance]">
        {value}
      </div>
      <div className="mt-1 text-xs leading-5 text-muted-foreground">{hint}</div>
    </div>
  )
}

function AuditActionMultiSelect({
  onChange,
  selected,
  triggerId,
}: {
  onChange: (actions: string[]) => void
  selected: string[]
  triggerId: string
}) {
  const { t } = useTranslation()
  const summary =
    selected.length === 0
      ? t('audit_log.filters_actions_placeholder')
      : t('audit_log.filters_actions_selected', { count: selected.length })

  const toggle = (action: string) => {
    onChange(
      selected.includes(action)
        ? selected.filter((item) => item !== action)
        : [...selected, action].sort(),
    )
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          id={triggerId}
          type="button"
          variant="outline"
          className="h-11 w-full justify-between rounded-xl border-border/70 bg-background text-left shadow-none"
          aria-label={t('audit_log.fields.action')}
        >
          <span className="truncate">{summary}</span>
          <ChevronDown className="ml-2 h-4 w-4 shrink-0" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 rounded-2xl p-3">
        <div className="mb-3 flex items-center justify-between gap-2">
          <div className="text-sm font-medium">{t('audit_log.fields.action')}</div>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={selected.length === 0}
            onClick={() => onChange([])}
          >
            <X className="mr-1 h-3.5 w-3.5" />
            {t('audit_log.filters_actions_clear')}
          </Button>
        </div>
        <div className="max-h-72 space-y-2 overflow-y-auto pr-1">
          {auditActionOptions.map((action) => {
            const id = `audit-action-${action.replaceAll('.', '-')}`
            return (
              <label
                key={action}
                htmlFor={id}
                className="flex cursor-pointer items-center gap-3 rounded-xl border border-transparent px-2.5 py-2 transition hover:border-border/70 hover:bg-muted/40"
              >
                <Checkbox
                  id={id}
                  checked={selected.includes(action)}
                  onCheckedChange={() => toggle(action)}
                />
                <span className="font-mono text-xs">{action}</span>
              </label>
            )
          })}
        </div>
      </PopoverContent>
    </Popover>
  )
}

function RequestMetadata({ item }: { item: AuditLogEntry }) {
  const { t } = useTranslation()
  const metadata = [
    [t('audit_log.fields.actor_email'), item.actorEmail],
    [t('audit_log.fields.actor_ip'), item.actorIp],
    [t('audit_log.fields.actor_user_agent'), item.actorUserAgent],
  ].filter(([, value]) => value)
  if (metadata.length === 0) return null

  return (
    <div className="rounded-2xl border border-border/70 bg-muted/18 p-4">
      <div className="mb-3 text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {t('audit_log.request_metadata')}
      </div>
      <dl className="space-y-3 text-xs">
        {metadata.map(([label, value]) => (
          <div key={label} className="grid gap-1 sm:grid-cols-[132px_1fr] sm:items-start">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="break-all font-mono">{value}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

function JsonBlock({ label, value }: { label: string; value?: string }) {
  if (!value) return null

  return (
    <div className="rounded-2xl border border-border/70 bg-muted/18 p-4">
      <div className="mb-2 text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {label}
      </div>
      <pre className="overflow-x-auto rounded-xl bg-background/92 p-3 text-xs leading-6 text-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.7)]">
        {formatJson(value)}
      </pre>
    </div>
  )
}

function EntityPanel({
  primary,
  secondary,
  title,
}: {
  primary: string
  secondary: string
  title: string
}) {
  return (
    <div className="rounded-2xl border border-border/70 bg-muted/18 p-4">
      <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {title}
      </div>
      <div className="mt-3 text-sm font-medium text-foreground">{primary}</div>
      <div className="mt-1 break-all font-mono text-xs text-muted-foreground">{secondary}</div>
    </div>
  )
}

function FilterChip({ active, children }: { active: boolean; children: React.ReactNode }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border px-3 py-1 text-xs transition-colors',
        active
          ? 'border-primary/18 bg-primary/9 text-primary'
          : 'border-border/60 bg-background/80 text-muted-foreground',
      )}
    >
      {children}
    </span>
  )
}

function AuditToken({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border border-border/70 bg-muted/22 px-3 py-1 font-mono text-[11px] tracking-[0.02em] text-muted-foreground',
        className,
      )}
    >
      {children}
    </span>
  )
}

function formatJson(raw: string) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function formatAbsoluteTimestamp(raw: string, locale?: Locale) {
  return format(new Date(raw), 'yyyy-MM-dd HH:mm:ss', { locale })
}

function localDateTimeToISO(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const parsed = new Date(trimmed)
  if (Number.isNaN(parsed.getTime())) return undefined
  return parsed.toISOString()
}
