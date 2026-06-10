import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { BarChart3, CircleDollarSign, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  type LLMUsage,
  type LLMUsageFilters,
  llmUsageQuery,
} from '@/features/usage/api/get-llm-usage'
import type { LLMUsageBucket } from '@/proto/attune/v1/usage'

const defaultFilters: Required<LLMUsageFilters> = { granularity: 'week', range: 'now-30d' }

export const Route = createFileRoute('/_authed/llm-usage')({
  component: LLMUsagePage,
  loader: ({ context }) => context.queryClient.ensureQueryData(llmUsageQuery(defaultFilters)),
})

function LLMUsagePage() {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<Required<LLMUsageFilters>>(defaultFilters)
  const usage = useQuery(llmUsageQuery(filters))

  return (
    <div>
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('nav.llm_usage')}</h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{t('llm_usage.subtitle')}</p>
        </div>
        <div className="flex items-center gap-2">
          <Select
            value={filters.range}
            onValueChange={(range) =>
              setFilters((old) => ({ ...old, range: range as Required<LLMUsageFilters>['range'] }))
            }
          >
            <SelectTrigger className="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="now-7d">{t('llm_usage.range.7d')}</SelectItem>
              <SelectItem value="now-30d">{t('llm_usage.range.30d')}</SelectItem>
              <SelectItem value="now-90d">{t('llm_usage.range.90d')}</SelectItem>
              <SelectItem value="now/M">{t('llm_usage.range.month')}</SelectItem>
            </SelectContent>
          </Select>
          <Select
            value={filters.granularity}
            onValueChange={(granularity) =>
              setFilters((old) => ({
                ...old,
                granularity: granularity as Required<LLMUsageFilters>['granularity'],
              }))
            }
          >
            <SelectTrigger className="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="day">{t('llm_usage.granularity.day')}</SelectItem>
              <SelectItem value="week">{t('llm_usage.granularity.week')}</SelectItem>
              <SelectItem value="month">{t('llm_usage.granularity.month')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      {usage.isPending ? (
        <div className="flex items-center justify-center py-12 text-muted-foreground">
          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          {t('app.loading')}
        </div>
      ) : usage.data ? (
        <LLMUsageBody data={usage.data} />
      ) : null}
    </div>
  )
}

function LLMUsageBody({ data }: { data: LLMUsage }) {
  const { t } = useTranslation()
  const rows = data.series ?? []
  const period = t('llm_usage.period', {
    start: format(new Date(data.periodStart), 'yyyy-MM-dd', { locale: zhCN }),
    end: format(new Date(data.periodEnd), 'yyyy-MM-dd', { locale: zhCN }),
  })

  return (
    <>
      <div className="mt-6 grid gap-3 md:grid-cols-4">
        <Stat label={t('llm_usage.total_cost')} value={formatUSD(data.costUsd)} hint={period} />
        <Stat
          label={t('llm_usage.prompt_tokens')}
          value={formatInt(data.promptTokens)}
          hint={t('llm_usage.tokens_unit')}
        />
        <Stat
          label={t('llm_usage.completion_tokens')}
          value={formatInt(data.completionTokens)}
          hint={t('llm_usage.tokens_unit')}
        />
        <Stat
          label={t('llm_usage.calls')}
          value={formatInt(data.calls)}
          hint={t('llm_usage.errors', { value: formatInt(data.errors) })}
        />
      </div>

      <Card className="mt-4 gap-0 overflow-hidden rounded-lg border-border/60 py-0 shadow-none">
        <CardHeader className="border-b border-border/60 bg-muted/20 px-4 py-3">
          <CardTitle>{t('llm_usage.table.title')}</CardTitle>
        </CardHeader>
        <CardContent className="px-0">
          {rows.length > 0 ? (
            <UsageTable rows={rows} />
          ) : (
            <EmptyState
              icon={BarChart3}
              title={t('llm_usage.empty_title')}
              description={t('llm_usage.empty_body')}
            />
          )}
        </CardContent>
      </Card>
    </>
  )
}

function Stat({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <Card className="rounded-lg border-border/60 shadow-none">
      <CardContent className="p-4">
        <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <CircleDollarSign className="size-3.5" />
          {label}
        </div>
        <div className="mt-2 text-2xl font-semibold tabular-nums">{value}</div>
        <div className="mt-1 truncate text-xs text-muted-foreground">{hint}</div>
      </CardContent>
    </Card>
  )
}

function UsageTable({ rows }: { rows: LLMUsageBucket[] }) {
  const { t } = useTranslation()
  return (
    <div>
      <div className="grid grid-cols-[8rem_minmax(10rem,1fr)_7rem_7rem_6rem_5rem] gap-4 border-b border-border/60 px-4 py-2 text-[11px] font-medium text-muted-foreground max-lg:hidden">
        <div>{t('llm_usage.table.bucket')}</div>
        <div>{t('llm_usage.table.model')}</div>
        <div>{t('llm_usage.table.prompt')}</div>
        <div>{t('llm_usage.table.completion')}</div>
        <div>{t('llm_usage.table.cost')}</div>
        <div>{t('llm_usage.table.calls')}</div>
      </div>
      <div className="divide-y divide-border/60">
        {rows.map((row) => (
          <div
            key={`${row.bucket}-${row.modelId}`}
            className="grid gap-2 px-4 py-3 text-sm lg:grid-cols-[8rem_minmax(10rem,1fr)_7rem_7rem_6rem_5rem] lg:gap-4"
          >
            <div className="font-mono text-xs text-muted-foreground">
              {format(new Date(row.bucket), 'yyyy-MM-dd', { locale: zhCN })}
            </div>
            <div className="truncate font-mono text-xs">{row.modelId || 'unknown'}</div>
            <div className="font-mono text-xs tabular-nums">{formatInt(row.promptTokens)}</div>
            <div className="font-mono text-xs tabular-nums">{formatInt(row.completionTokens)}</div>
            <div className="font-mono text-xs tabular-nums">{formatUSD(row.costUsd)}</div>
            <div className="font-mono text-xs tabular-nums">
              {formatInt(row.calls)}
              {Number(row.errors) > 0 ? (
                <span className="ml-1 text-red-600">/{formatInt(row.errors)}</span>
              ) : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function formatUSD(value: number | undefined) {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 4,
  }).format(value ?? 0)
}

function formatInt(value: string | number | undefined) {
  return new Intl.NumberFormat('en-US').format(Number(value ?? 0))
}
