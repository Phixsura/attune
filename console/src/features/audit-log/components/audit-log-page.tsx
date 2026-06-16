import { useInfiniteQuery } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { ChevronDown, Download, FileSearch, Loader2, X } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { auditActionOptions } from '@/features/audit-log/actions'
import {
  type AuditLogEntry,
  type AuditLogFilters,
  auditLogInfiniteQuery,
  downloadAuditLogCsv,
} from '@/features/audit-log/api/list-audit-log'
import { usePermissions } from '@/features/session/hooks/use-permissions'

export function AuditLogPage() {
  const { t } = useTranslation()
  const { can } = usePermissions()
  const canView = can('settings:audit_log:view')

  const [selectedActions, setSelectedActions] = useState<string[]>([])
  const [targetType, setTargetType] = useState('')
  const [actorId, setActorId] = useState('')
  const [fromValue, setFromValue] = useState('')
  const [toValue, setToValue] = useState('')
  const [filters, setFilters] = useState<AuditLogFilters>({ limit: 50 })
  const [isExporting, setIsExporting] = useState(false)
  const query = useInfiniteQuery(auditLogInfiniteQuery(filters))
  const items = query.data?.pages.flatMap((page) => page.items ?? []) ?? []

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
    setFilters({
      actions: selectedActions.length > 0 ? selectedActions : undefined,
      actorId: actorId.trim() || undefined,
      from: localDateTimeToISO(fromValue),
      limit: 50,
      targetType: targetType.trim() || undefined,
      to: localDateTimeToISO(toValue),
    })
  }

  const handleReset = () => {
    setSelectedActions([])
    setTargetType('')
    setActorId('')
    setFromValue('')
    setToValue('')
    setFilters({ limit: 50 })
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
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('audit_log.title')}</h1>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t('audit_log.subtitle')}</p>
        </div>
        <Button
          type="button"
          variant="outline"
          disabled={isExporting}
          onClick={() => void handleExport()}
        >
          <Download className="mr-2 h-4 w-4" />
          {isExporting ? t('audit_log.exporting') : t('audit_log.export')}
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('audit_log.filters_title')}</CardTitle>
          <CardDescription>{t('audit_log.filters_help')}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2 xl:grid-cols-5">
          <div className="space-y-2">
            <Label htmlFor="audit-actions-trigger">{t('audit_log.fields.action')}</Label>
            <AuditActionMultiSelect
              selected={selectedActions}
              onChange={setSelectedActions}
              triggerId="audit-actions-trigger"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="audit-target-type">{t('audit_log.fields.target_type')}</Label>
            <Input
              id="audit-target-type"
              value={targetType}
              onChange={(e) => setTargetType(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="audit-actor-id">{t('audit_log.fields.actor_id')}</Label>
            <Input
              id="audit-actor-id"
              value={actorId}
              onChange={(e) => setActorId(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="audit-from">{t('audit_log.filters_from')}</Label>
            <Input
              id="audit-from"
              type="datetime-local"
              value={fromValue}
              onChange={(e) => setFromValue(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="audit-to">{t('audit_log.filters_to')}</Label>
            <Input
              id="audit-to"
              type="datetime-local"
              value={toValue}
              onChange={(e) => setToValue(e.target.value)}
            />
          </div>
          <div className="md:col-span-2 xl:col-span-5 flex flex-wrap gap-3">
            <Button type="button" onClick={handleApply}>
              {t('audit_log.apply')}
            </Button>
            <Button type="button" variant="ghost" onClick={handleReset}>
              {t('audit_log.reset')}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('audit_log.table_title')}</CardTitle>
          <CardDescription>{t('audit_log.table_help')}</CardDescription>
        </CardHeader>
        <CardContent>
          {query.isPending ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('app.loading')}
            </div>
          ) : query.isError ? (
            <EmptyState
              icon={FileSearch}
              title={t('audit_log.error_title')}
              description={t('audit_log.error_body')}
              action={{ label: t('audit_log.retry'), onClick: () => void query.refetch() }}
            />
          ) : items.length > 0 ? (
            <div className="space-y-4">
              <AuditLogTable items={items} />
              {query.hasNextPage ? (
                <div className="flex justify-center">
                  <Button
                    type="button"
                    variant="outline"
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
            />
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function AuditLogTable({ items }: { items: AuditLogEntry[] }) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language.startsWith('zh') ? zhCN : undefined
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('audit_log.fields.created_at')}</TableHead>
          <TableHead>{t('audit_log.fields.action')}</TableHead>
          <TableHead>{t('audit_log.fields.actor')}</TableHead>
          <TableHead>{t('audit_log.fields.target')}</TableHead>
          <TableHead>{t('audit_log.fields.summary')}</TableHead>
          <TableHead>{t('audit_log.fields.details')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((item) => (
          <TableRow key={item.id}>
            <TableCell className="text-sm text-muted-foreground">
              {formatDistanceToNow(new Date(item.createdAt), { addSuffix: true, locale })}
            </TableCell>
            <TableCell className="font-mono text-xs">{item.action}</TableCell>
            <TableCell className="text-sm">
              <div>{item.actorType}</div>
              <div className="font-mono text-xs text-muted-foreground">{item.actorId}</div>
            </TableCell>
            <TableCell className="text-sm">
              <div>{item.targetType}</div>
              <div className="font-mono text-xs text-muted-foreground">{item.targetId}</div>
            </TableCell>
            <TableCell>{item.summary || '—'}</TableCell>
            <TableCell>
              <details className="max-w-md">
                <summary className="cursor-pointer text-sm text-primary">
                  {t('audit_log.fields.details')}
                </summary>
                <div className="mt-2 space-y-2">
                  <RequestMetadata item={item} />
                  <JsonBlock label={t('audit_log.before')} value={item.beforeJson} />
                  <JsonBlock label={t('audit_log.after')} value={item.afterJson} />
                </div>
              </details>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
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
          className="w-full justify-between"
          aria-label={t('audit_log.fields.action')}
        >
          <span className="truncate">{summary}</span>
          <ChevronDown className="ml-2 h-4 w-4 shrink-0" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 p-3">
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
                className="flex cursor-pointer items-center gap-3 rounded-md px-2 py-1.5 hover:bg-muted/60"
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
    <div className="rounded-md border border-border/70 bg-muted/20 p-3">
      <div className="mb-2 text-xs font-medium text-muted-foreground">
        {t('audit_log.request_metadata')}
      </div>
      <dl className="space-y-2 text-xs">
        {metadata.map(([label, value]) => (
          <div key={label} className="grid gap-1 sm:grid-cols-[120px_1fr] sm:items-start">
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
    <div>
      <div className="mb-1 text-xs font-medium text-muted-foreground">{label}</div>
      <pre className="overflow-x-auto rounded-md bg-muted/40 p-3 text-xs">{formatJson(value)}</pre>
    </div>
  )
}

function formatJson(raw: string) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function localDateTimeToISO(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const parsed = new Date(trimmed)
  if (Number.isNaN(parsed.getTime())) return undefined
  return parsed.toISOString()
}
