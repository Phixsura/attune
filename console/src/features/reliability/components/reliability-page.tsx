import { Link } from '@tanstack/react-router'
import {
  AlertTriangle,
  ArrowUpRight,
  Bot,
  KeyRound,
  type LucideIcon,
  PackageX,
  Radar,
  RefreshCw,
  Scale,
  ShieldAlert,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { useDocumentTitle } from '@/hooks/use-document-title'
import { cn } from '@/lib/utils'

export type Tone = 'default' | 'active' | 'urgent'

export type ReliabilityStatus = 'pass' | 'warn' | 'fail' | 'skipped'

export type ReliabilityMetric = {
  tone: Tone
  heroTone?: Tone
  value: string
  hint: string
}

export type ReliabilityReadinessMetric = ReliabilityMetric & {
  status?: ReliabilityStatus
}

export interface ReliabilityPageProps {
  tenantName: string
  dashboardHref: string
  isRefreshing: boolean
  onRefreshAll: () => void
  failedQueries: Array<{ label: string; message: string }>
  readiness: ReliabilityReadinessMetric
  authMode: ReliabilityMetric
  apiKeys: ReliabilityMetric
  mcpClients: ReliabilityMetric
  gdpr: ReliabilityMetric
  deadDeliveries: ReliabilityMetric
  releaseContext: {
    version: ReliabilityMetric
    environment: ReliabilityMetric
    owner: ReliabilityMetric
    lifecycle: ReliabilityMetric
    recovery: ReliabilityMetric
    compatibility: ReliabilityMetric
    glossary: string
    runbookHref: string
    escalationHref?: string
  }
}

const TONE_CLASS: Record<Tone, { box: string; icon: string }> = {
  default: {
    box: 'border-border/60 bg-background/88 shadow-[0_18px_38px_-34px_rgba(15,23,42,0.18)]',
    icon: 'text-slate-500',
  },
  active: {
    box: 'border-emerald-300/55 bg-emerald-50/85 shadow-[0_18px_38px_-34px_rgba(16,185,129,0.2)]',
    icon: 'text-emerald-600 dark:text-emerald-400',
  },
  urgent: {
    box: 'border-amber-300/55 bg-amber-50/85 shadow-[0_18px_38px_-34px_rgba(245,158,11,0.2)]',
    icon: 'text-amber-600 dark:text-amber-400',
  },
}

function ReliabilityStat({
  label,
  value,
  hint,
  tone = 'default',
  icon: Icon,
}: {
  label: string
  value: string
  hint?: string
  tone?: Tone
  icon: LucideIcon
}) {
  const styles = TONE_CLASS[tone]
  return (
    <div className={cn('rounded-[1.15rem] border px-4 py-3.5', styles.box)}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-[11px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
            {label}
          </div>
          <div className="mt-1 text-[1.45rem] font-semibold tracking-tight text-foreground [font-variant-numeric:tabular-nums]">
            {value}
          </div>
        </div>
        <Icon className={cn('mt-0.5 h-4 w-4 shrink-0', styles.icon)} strokeWidth={1.8} />
      </div>
      {hint ? <div className="mt-1.5 text-xs leading-5 text-muted-foreground">{hint}</div> : null}
    </div>
  )
}

function QuickLink({
  title,
  description,
  icon: Icon,
  to,
}: {
  title: string
  description: string
  icon: LucideIcon
  to: string
}) {
  return (
    <Link
      to={to}
      className="group flex items-start gap-3 rounded-[1rem] border border-border/60 bg-background/85 px-4 py-3.5 transition-all hover:-translate-y-0.5 hover:border-primary/20 hover:bg-background hover:shadow-[0_18px_40px_-36px_rgba(15,23,42,0.18)]"
    >
      <div className="mt-0.5 rounded-lg border border-border/60 bg-muted/25 p-2 text-foreground/80">
        <Icon className="h-4 w-4" strokeWidth={1.8} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-3">
          <div className="text-sm font-semibold text-foreground">{title}</div>
          <ArrowUpRight className="h-4 w-4 shrink-0 text-muted-foreground/60 transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-foreground" />
        </div>
        <div className="mt-1 text-sm leading-6 text-muted-foreground">{description}</div>
      </div>
    </Link>
  )
}

function ExternalActionLink({
  title,
  description,
  href,
}: {
  title: string
  description: string
  href: string
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="group flex items-start gap-3 rounded-[1rem] border border-border/60 bg-background/85 px-4 py-3.5 transition-all hover:-translate-y-0.5 hover:border-primary/20 hover:bg-background hover:shadow-[0_18px_40px_-36px_rgba(15,23,42,0.18)]"
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-3">
          <div className="text-sm font-semibold text-foreground">{title}</div>
          <ArrowUpRight className="h-4 w-4 shrink-0 text-muted-foreground/60 transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-foreground" />
        </div>
        <div className="mt-1 text-sm leading-6 text-muted-foreground">{description}</div>
      </div>
    </a>
  )
}

export function ReliabilityPage({
  tenantName,
  dashboardHref,
  isRefreshing,
  onRefreshAll,
  failedQueries,
  readiness,
  authMode,
  apiKeys,
  mcpClients,
  gdpr,
  deadDeliveries,
  releaseContext,
}: ReliabilityPageProps) {
  const { t } = useTranslation()
  useDocumentTitle(t('reliability.title', 'Reliability'))

  const readinessIcon = readiness.status === 'fail' ? ShieldAlert : ShieldCheck

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.administration')}
        title={t('reliability.title', 'Reliability')}
        subtitle={t('reliability.subtitle', { tenant: tenantName })}
        actions={
          <>
            <Button variant="outline" size="sm" disabled={isRefreshing} onClick={onRefreshAll}>
              <RefreshCw className={cn('mr-1.5 h-3.5 w-3.5', isRefreshing && 'animate-spin')} />
              {t('reliability.refresh', '刷新')}
            </Button>
            <Button asChild size="sm" variant="default">
              <a href={dashboardHref} target="_blank" rel="noreferrer">
                <Radar className="mr-1.5 h-3.5 w-3.5" />
                {t('reliability.open_dashboard', '打开 tenant impact dashboard')}
              </a>
            </Button>
          </>
        }
        metrics={
          <>
            <PageHeroMetric
              label={t('reliability.metrics.readiness', '就绪')}
              value={readiness.value}
              hint={readiness.hint}
              tone={readiness.heroTone ?? readiness.tone}
            />
            <PageHeroMetric
              label={t('reliability.metrics.auth', '认证')}
              value={authMode.value}
              hint={authMode.hint}
              tone={authMode.heroTone ?? authMode.tone}
            />
            <PageHeroMetric
              label={t('reliability.metrics.api_keys', 'API key')}
              value={apiKeys.value}
              hint={apiKeys.hint}
              tone={apiKeys.heroTone ?? apiKeys.tone}
            />
            <PageHeroMetric
              label={t('reliability.metrics.mcp_clients', 'MCP 客户端')}
              value={mcpClients.value}
              hint={mcpClients.hint}
              tone={mcpClients.heroTone ?? mcpClients.tone}
            />
            <PageHeroMetric
              label={t('reliability.metrics.gdpr', 'GDPR')}
              value={gdpr.value}
              hint={gdpr.hint}
              tone={gdpr.heroTone ?? gdpr.tone}
            />
            <PageHeroMetric
              label={t('reliability.metrics.dead_deliveries', '死信投递')}
              value={deadDeliveries.value}
              hint={deadDeliveries.hint}
              tone={deadDeliveries.heroTone ?? deadDeliveries.tone}
            />
          </>
        }
      />

      {failedQueries.length > 0 && (
        <Card className="border-amber-600/20 bg-amber-600/[0.04] shadow-none dark:border-amber-500/20 dark:bg-amber-500/[0.04]">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm font-semibold text-amber-900 dark:text-amber-200">
              <AlertTriangle className="h-4 w-4" />
              {t('reliability.error_title', '部分可靠性数据加载失败')}
            </CardTitle>
            <CardDescription className="text-amber-950/70 dark:text-amber-100/70">
              {t('reliability.error_body', '你可以先查看已加载的卡片，再刷新重试。')}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {failedQueries.map((failure) => (
              <div
                key={failure.label}
                className="rounded-[0.9rem] border border-amber-500/15 bg-background/75 px-3 py-2.5 text-sm"
              >
                <div className="font-medium text-foreground">{failure.label}</div>
                <div className="mt-1 text-muted-foreground">{failure.message}</div>
              </div>
            ))}
            <Button variant="outline" size="sm" onClick={onRefreshAll}>
              <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
              {t('reliability.retry_all', '重试全部')}
            </Button>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.2fr)_minmax(22rem,0.8fr)]">
        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="text-base">
              {t('reliability.sections.snapshot', '运行快照')}
            </CardTitle>
            <CardDescription>
              {t(
                'reliability.sections.snapshot_description',
                '把当前控制面、认证、密钥、MCP、GDPR 和投递压力放在一张卡片里。',
              )}
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
            <ReliabilityStat
              label={t('reliability.links.system_readiness', '系统就绪')}
              value={readiness.value}
              hint={readiness.hint}
              tone={readiness.tone}
              icon={readinessIcon}
            />
            <ReliabilityStat
              label={t('reliability.links.security', '安全设置')}
              value={authMode.value}
              hint={authMode.hint}
              tone={authMode.tone}
              icon={ShieldCheck}
            />
            <ReliabilityStat
              label={t('reliability.links.api_keys', 'API key')}
              value={apiKeys.value}
              hint={apiKeys.hint}
              tone={apiKeys.tone}
              icon={KeyRound}
            />
            <ReliabilityStat
              label={t('reliability.links.mcp_clients', 'MCP 客户端')}
              value={mcpClients.value}
              hint={mcpClients.hint}
              tone={mcpClients.tone}
              icon={Bot}
            />
            <ReliabilityStat
              label={t('reliability.links.gdpr', 'GDPR')}
              value={gdpr.value}
              hint={gdpr.hint}
              tone={gdpr.tone}
              icon={Scale}
            />
            <ReliabilityStat
              label={t('reliability.links.dead_deliveries', '死信投递')}
              value={deadDeliveries.value}
              hint={deadDeliveries.hint}
              tone={deadDeliveries.tone}
              icon={PackageX}
            />
          </CardContent>
        </Card>

        <div className="space-y-6">
          <Card className="border-border/60 shadow-none">
            <CardHeader>
              <CardTitle className="text-base">
                {t('reliability.sections.release', '发布与归属')}
              </CardTitle>
              <CardDescription>
                {t(
                  'reliability.sections.release_description',
                  '把当前运行版本、部署环境、生命周期状态、恢复结果、责任团队和兼容性规则放在同一处。',
                )}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="grid gap-2 sm:grid-cols-2">
                <ReliabilityStat
                  label={t('reliability.release.version', '版本')}
                  value={releaseContext.version.value}
                  hint={releaseContext.version.hint}
                  tone={releaseContext.version.tone}
                  icon={Radar}
                />
                <ReliabilityStat
                  label={t('reliability.release.environment', '环境')}
                  value={releaseContext.environment.value}
                  hint={releaseContext.environment.hint}
                  tone={releaseContext.environment.tone}
                  icon={ShieldCheck}
                />
              </div>
              <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                <ReliabilityStat
                  label={t('reliability.release.owner', '责任团队')}
                  value={releaseContext.owner.value}
                  hint={releaseContext.owner.hint}
                  tone={releaseContext.owner.tone}
                  icon={Scale}
                />
                <ReliabilityStat
                  label={t('reliability.release.lifecycle', 'Lifecycle')}
                  value={releaseContext.lifecycle.value}
                  hint={releaseContext.lifecycle.hint}
                  tone={releaseContext.lifecycle.tone}
                  icon={ShieldCheck}
                />
                <ReliabilityStat
                  label={t('reliability.release.recovery', '恢复')}
                  value={releaseContext.recovery.value}
                  hint={releaseContext.recovery.hint}
                  tone={releaseContext.recovery.tone}
                  icon={RefreshCw}
                />
              </div>
              <div className="grid gap-2 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
                <ReliabilityStat
                  label={t('reliability.release.compatibility', 'Compatibility')}
                  value={releaseContext.compatibility.value}
                  hint={releaseContext.compatibility.hint}
                  tone={releaseContext.compatibility.tone}
                  icon={Radar}
                />
                <div className="rounded-[1.15rem] border border-border/60 bg-background/88 px-4 py-3.5 shadow-[0_18px_38px_-34px_rgba(15,23,42,0.18)]">
                  <div className="text-[11px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
                    {t('reliability.release.glossary', 'Canonical terms')}
                  </div>
                  <div className="mt-1.5 text-sm font-medium leading-6 text-foreground">
                    {releaseContext.glossary}
                  </div>
                  <div className="mt-1.5 text-xs leading-5 text-muted-foreground">
                    {t(
                      'reliability.release.glossary_hint',
                      'Keep these terms stable across docs, UI, and telemetry.',
                    )}
                  </div>
                </div>
              </div>
              <div className="space-y-2">
                <ExternalActionLink
                  title={t('reliability.release.runbook', '运行手册')}
                  description={t(
                    'reliability.release.runbook_desc',
                    '打开当前发布对应的操作手册。',
                  )}
                  href={releaseContext.runbookHref}
                />
                {releaseContext.escalationHref ? (
                  <ExternalActionLink
                    title={t('reliability.release.escalation', '升级通道')}
                    description={t(
                      'reliability.release.escalation_desc',
                      '把问题交给责任人或升级渠道。',
                    )}
                    href={releaseContext.escalationHref}
                  />
                ) : null}
              </div>
            </CardContent>
          </Card>

          <Card className="border-border/60 shadow-none">
            <CardHeader>
              <CardTitle className="text-base">
                {t('reliability.sections.shortcuts', '快速入口')}
              </CardTitle>
              <CardDescription>
                {t('reliability.sections.shortcuts_description', '跳到对应页面做深挖。')}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-2">
              <QuickLink
                title={t('reliability.links.system_readiness', '系统就绪')}
                description={t(
                  'reliability.links.system_readiness_desc',
                  '配置、数据库、迁移、加密和 worker 检查。',
                )}
                icon={Radar}
                to="/administration/system-readiness"
              />
              <QuickLink
                title={t('reliability.links.security', '安全设置')}
                description={t('reliability.links.security_desc', '认证模式和 break-glass token。')}
                icon={ShieldCheck}
                to="/administration/security"
              />
              <QuickLink
                title={t('reliability.links.api_keys', 'API key')}
                description={t('reliability.links.api_keys_desc', '密钥库存、过期和使用情况。')}
                icon={KeyRound}
                to="/integrations/api-keys"
              />
              <QuickLink
                title={t('reliability.links.mcp_clients', 'MCP 客户端')}
                description={t('reliability.links.mcp_clients_desc', 'OAuth 客户端和工具策略。')}
                icon={Bot}
                to="/mcp-clients"
              />
              <QuickLink
                title={t('reliability.links.gdpr', 'GDPR')}
                description={t('reliability.links.gdpr_desc', '请求队列、导出和删除计划。')}
                icon={Scale}
                to="/administration/gdpr"
              />
              <QuickLink
                title={t('reliability.links.dead_deliveries', '死信投递')}
                description={t(
                  'reliability.links.dead_deliveries_desc',
                  '失败类型、重试状态和 in-flight 任务。',
                )}
                icon={PackageX}
                to="/administration/dead-deliveries"
              />
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}
