import { useQuery, useQueryClient } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { useTranslation } from 'react-i18next'
import { apiKeysQuery } from '@/features/api-keys/api/list-api-keys'
import { gdprOperationsQuery } from '@/features/gdpr/api/gdpr-control'
import { mcpClientsQuery } from '@/features/mcp-clients/api/list-mcp-clients'
import { deliveriesQuery } from '@/features/outbox-dead/api/list-deliveries'
import {
  type ReliabilityMetric,
  ReliabilityPage,
  type ReliabilityReadinessMetric,
  type Tone,
} from '@/features/reliability/components/reliability-page'
import { authModeQuery } from '@/features/security/api/auth-mode'
import { meQuery } from '@/features/session/api/get-me'
import { type PreflightStatus, preflightQuery } from '@/features/system-readiness/api/get-preflight'

type TranslationFunction = ReturnType<typeof useTranslation>['t']

function safeErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message.trim()) {
    return error.message
  }
  return fallback
}

function formatCount(value: number): string {
  return new Intl.NumberFormat('en-US').format(value)
}

function statusTone(status: PreflightStatus): Tone {
  switch (status) {
    case 'pass':
      return 'active'
    case 'warn':
    case 'fail':
      return 'urgent'
    default:
      return 'default'
  }
}

function statusLabel(status: PreflightStatus | undefined, t: TranslationFunction) {
  switch (status) {
    case 'pass':
      return t('reliability.status.pass', '通过')
    case 'warn':
      return t('reliability.status.warn', '告警')
    case 'fail':
      return t('reliability.status.fail', '失败')
    case 'skipped':
      return t('reliability.status.skipped', '跳过')
    default:
      return t('common.loading', '加载中…')
  }
}

export function ReliabilityRoutePage() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const locale = i18n.language.startsWith('zh') ? zhCN : undefined

  const me = useQuery(meQuery())
  const authMode = useQuery(authModeQuery())
  const preflight = useQuery(preflightQuery())
  const apiKeys = useQuery(apiKeysQuery())
  const mcpClients = useQuery(mcpClientsQuery())
  const gdprOps = useQuery(gdprOperationsQuery())
  const deadDeliveries = useQuery(deliveriesQuery('dead'))

  const tenantName = me.data?.tenant?.name ?? t('common.loading', 'Loading...')
  const tenantSlug = me.data?.tenant?.slug
  const dashboardHref = tenantSlug
    ? `/d/attune-tenant-impact/attune-tenant-impact?var-tenant=${encodeURIComponent(tenantSlug)}`
    : '/d/attune-tenant-impact/attune-tenant-impact'

  const preflightChecks = preflight.data?.checks ?? []
  const passedChecks = preflightChecks.filter((check) => check.status === 'pass').length
  const warnChecks = preflightChecks.filter((check) => check.status === 'warn').length
  const failChecks = preflightChecks.filter((check) => check.status === 'fail').length

  const activeApiKeys = apiKeys.data?.filter((key) => key.isActive).length ?? 0
  const inactiveApiKeys = (apiKeys.data?.length ?? 0) - activeApiKeys
  const totalApiKeys = apiKeys.data?.length ?? 0

  const activeMcpClients = mcpClients.data?.filter((client) => !client.revoked_at).length ?? 0
  const revokedMcpClients = (mcpClients.data?.length ?? 0) - activeMcpClients
  const totalMcpClients = mcpClients.data?.length ?? 0

  const queuedGdpr = gdprOps.data?.queuedRequestCount ?? 0
  const activeGdpr = gdprOps.data?.activeRequestCount ?? 0
  const readyExports = gdprOps.data?.readyExportCount ?? 0
  const scheduledDeletes = gdprOps.data?.scheduledDeleteCount ?? 0
  const nextExportExpiryLabel = gdprOps.data?.nextExportExpiryAt
    ? formatDistanceToNow(new Date(gdprOps.data.nextExportExpiryAt), {
        addSuffix: true,
        locale,
      })
    : null

  const deadDeliveryCount = deadDeliveries.data?.length ?? 0
  const retryableDeadDeliveries =
    deadDeliveries.data?.filter((delivery) => !delivery.inFlight).length ?? 0
  const inflightDeadDeliveries =
    deadDeliveries.data?.filter((delivery) => delivery.inFlight).length ?? 0

  const readinessStatus = preflight.data?.status
  const readinessTone = readinessStatus ? statusTone(readinessStatus) : 'default'

  const authModeTone = authMode.data?.mode === 'sso_only' ? 'active' : 'default'
  const apiKeysTone = activeApiKeys > 0 ? 'active' : 'default'
  const mcpTone = activeMcpClients > 0 ? 'active' : 'default'
  const gdprTone = queuedGdpr + activeGdpr > 0 ? 'urgent' : 'default'
  const deadTone = deadDeliveryCount > 0 ? 'urgent' : 'default'

  const readinessHint = preflight.data
    ? t('reliability.hints.system_readiness', {
        passed: passedChecks,
        warn: warnChecks,
        fail: failChecks,
        elapsed: preflight.data.elapsed,
      })
    : t('common.loading', 'Loading...')

  const authModeValue =
    authMode.data?.mode === 'sso_only'
      ? t('reliability.auth.sso_only', '仅 SSO')
      : authMode.data?.mode === 'hybrid'
        ? t('reliability.auth.hybrid', '混合')
        : t('common.loading', 'Loading...')

  const apiKeysHint = t('reliability.hints.api_keys', {
    active: activeApiKeys,
    inactive: inactiveApiKeys,
    total: totalApiKeys,
  })
  const mcpHint = t('reliability.hints.mcp_clients', {
    active: activeMcpClients,
    revoked: revokedMcpClients,
    total: totalMcpClients,
  })
  const gdprHint = t('reliability.hints.gdpr', {
    queued: queuedGdpr,
    active: activeGdpr,
    ready: readyExports,
    scheduled: scheduledDeletes,
  })
  const gdprExpiryHint = nextExportExpiryLabel
    ? t('reliability.hints.gdpr_expiry', { value: nextExportExpiryLabel })
    : t('common.never', '从未')
  const deadDeliveriesHint = t('reliability.hints.dead_deliveries', {
    retryable: retryableDeadDeliveries,
    inflight: inflightDeadDeliveries,
  })

  const failedQueries = [
    preflight.error && {
      label: t('reliability.links.system_readiness', '系统就绪'),
      message: safeErrorMessage(preflight.error, t('common.error', 'Error')),
    },
    authMode.error && {
      label: t('reliability.links.security', '安全设置'),
      message: safeErrorMessage(authMode.error, t('common.error', 'Error')),
    },
    apiKeys.error && {
      label: t('reliability.links.api_keys', 'API key'),
      message: safeErrorMessage(apiKeys.error, t('common.error', 'Error')),
    },
    mcpClients.error && {
      label: t('reliability.links.mcp_clients', 'MCP 客户端'),
      message: safeErrorMessage(mcpClients.error, t('common.error', 'Error')),
    },
    gdprOps.error && {
      label: t('reliability.links.gdpr', 'GDPR'),
      message: safeErrorMessage(gdprOps.error, t('common.error', 'Error')),
    },
    deadDeliveries.error && {
      label: t('reliability.links.dead_deliveries', '死信投递'),
      message: safeErrorMessage(deadDeliveries.error, t('common.error', 'Error')),
    },
  ].filter(Boolean) as Array<{ label: string; message: string }>

  const isRefreshing =
    me.isFetching ||
    authMode.isFetching ||
    preflight.isFetching ||
    apiKeys.isFetching ||
    mcpClients.isFetching ||
    gdprOps.isFetching ||
    deadDeliveries.isFetching

  const refreshAll = () => {
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: meQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: authModeQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: preflightQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: apiKeysQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: mcpClientsQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: gdprOperationsQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: deliveriesQuery('dead').queryKey }),
    ])
  }

  const readiness: ReliabilityReadinessMetric = {
    tone: readinessTone,
    heroTone: readinessTone,
    status: readinessStatus,
    value: statusLabel(readinessStatus, t),
    hint: readinessHint,
  }
  const authModeMetric: ReliabilityMetric = {
    tone: authModeTone,
    heroTone: authModeTone,
    value: authModeValue,
    hint: t('reliability.hints.auth_mode', '仅 SSO 时需要确保 break-glass 路径可用。'),
  }
  const apiKeysMetric: ReliabilityMetric = {
    tone: apiKeysTone,
    heroTone: 'default',
    value: apiKeys.data
      ? `${formatCount(activeApiKeys)}/${formatCount(totalApiKeys)}`
      : t('common.loading', 'Loading...'),
    hint: apiKeysHint,
  }
  const mcpMetric: ReliabilityMetric = {
    tone: mcpTone,
    heroTone: 'default',
    value: mcpClients.data
      ? `${formatCount(activeMcpClients)}/${formatCount(totalMcpClients)}`
      : t('common.loading', 'Loading...'),
    hint: mcpHint,
  }
  const gdprMetric: ReliabilityMetric = {
    tone: gdprTone,
    heroTone: queuedGdpr + activeGdpr > 0 ? 'active' : 'default',
    value: gdprOps.data ? formatCount(queuedGdpr + activeGdpr) : t('common.loading', 'Loading...'),
    hint: `${gdprHint}${nextExportExpiryLabel ? ` · ${gdprExpiryHint}` : ''}`,
  }
  const deadMetric: ReliabilityMetric = {
    tone: deadTone,
    heroTone: deadTone,
    value: deadDeliveries.data ? formatCount(deadDeliveryCount) : t('common.loading', 'Loading...'),
    hint: deadDeliveriesHint,
  }

  return (
    <ReliabilityPage
      tenantName={tenantName}
      dashboardHref={dashboardHref}
      isRefreshing={isRefreshing}
      onRefreshAll={refreshAll}
      failedQueries={failedQueries}
      readiness={readiness}
      authMode={authModeMetric}
      apiKeys={apiKeysMetric}
      mcpClients={mcpMetric}
      gdpr={gdprMetric}
      deadDeliveries={deadMetric}
    />
  )
}
