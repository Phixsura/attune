import { useQuery, useQueryClient } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { useTranslation } from 'react-i18next'
import { apiKeysQuery } from '@/features/api-keys/api/list-api-keys'
import { gdprOperationsQuery } from '@/features/gdpr/api/gdpr-control'
import { mcpClientsQuery } from '@/features/mcp-clients/api/list-mcp-clients'
import { deliveriesQuery } from '@/features/outbox-dead/api/list-deliveries'
import {
  type RecoveryContextResponse,
  recoveryContextQuery,
} from '@/features/reliability/api/get-recovery-context'
import { releaseContextQuery } from '@/features/reliability/api/get-release-context'
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

function lifecycleLabel(state: string | undefined, t: TranslationFunction) {
  switch (state) {
    case 'supported':
      return t('reliability.release.lifecycle_supported', 'supported')
    case 'deprecated':
      return t('reliability.release.lifecycle_deprecated', 'deprecated')
    case 'migrating':
      return t('reliability.release.lifecycle_migrating', 'migrating')
    case 'recovering':
      return t('reliability.release.lifecycle_recovering', 'recovering')
    case 'blocked':
      return t('reliability.release.lifecycle_blocked', 'blocked')
    default:
      return state ?? t('common.loading', 'Loading...')
  }
}

function lifecycleHint(state: string | undefined, t: TranslationFunction) {
  switch (state) {
    case 'supported':
      return t(
        'reliability.release.lifecycle_supported_hint',
        'Current runtime contract is within the supported window.',
      )
    case 'deprecated':
      return t(
        'reliability.release.lifecycle_deprecated_hint',
        'This surface is deprecated and should move behind its replacement.',
      )
    case 'migrating':
      return t(
        'reliability.release.lifecycle_migrating_hint',
        'Non-production runtime; compatibility can still shift while the contract is moving.',
      )
    case 'recovering':
      return t(
        'reliability.release.lifecycle_recovering_hint',
        'Recovery mode is active and the support window is still being re-established.',
      )
    case 'blocked':
      return t(
        'reliability.release.lifecycle_blocked_hint',
        'Production runtime is blocked because a dev build or blank release marker is present.',
      )
    default:
      return t('common.loading', 'Loading...')
  }
}

function formatRecoveryDuration(durationMs: number) {
  if (durationMs < 1000) {
    return `${durationMs}ms`
  }
  const seconds = durationMs / 1000
  if (seconds < 60) {
    return `${seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)}s`
  }
  const minutes = seconds / 60
  if (minutes < 60) {
    return `${minutes < 10 ? minutes.toFixed(1) : Math.round(minutes)}m`
  }
  const hours = minutes / 60
  if (hours < 24) {
    return `${hours < 10 ? hours.toFixed(1) : Math.round(hours)}h`
  }
  return `${Math.round(hours / 24)}d`
}

function formatRecoveryWindow(seconds: number) {
  if (seconds < 60) {
    return `${seconds}s`
  }
  const minutes = seconds / 60
  if (minutes < 60) {
    return `${Math.round(minutes)}m`
  }
  const hours = minutes / 60
  if (hours < 24) {
    return `${Math.round(hours)}h`
  }
  return `${Math.round(hours / 24)}d`
}

function recoveryHint(context: RecoveryContextResponse | undefined, t: TranslationFunction) {
  if (!context) {
    return t('common.loading', 'Loading...')
  }
  const details: string[] = []
  if (context.lastRun?.backupRef) {
    details.push(t('reliability.hints.recovery_backup', { value: context.lastRun.backupRef }))
  }
  if (context.lastRun) {
    details.push(
      t('reliability.hints.recovery_duration', {
        value: formatRecoveryDuration(context.lastRun.durationMs),
      }),
    )
  }
  if (context.freshnessWindowSeconds > 0) {
    details.push(
      t('reliability.hints.recovery_window', {
        value: formatRecoveryWindow(context.freshnessWindowSeconds),
      }),
    )
  }
  const parts = [context.message, ...details]
  if (context.remediation) {
    parts.push(context.remediation)
  }
  return parts.join(' · ')
}

function joinSemanticLabels<T extends { label: string }>(items: T[]) {
  return items.map((item) => item.label).join(' · ')
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
  const recoveryContext = useQuery(recoveryContextQuery())
  const releaseContext = useQuery(releaseContextQuery())

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
  const recoveryData = recoveryContext.data
  const releaseData = releaseContext.data
  const releaseStartedAt = releaseData?.startedAt
    ? formatDistanceToNow(new Date(releaseData.startedAt), {
        addSuffix: true,
        locale,
      })
    : null
  const releaseVersionTone =
    releaseData?.lifecycleState === 'blocked' ? 'urgent' : releaseData ? 'active' : 'default'
  const releaseLifecycleTone =
    releaseData?.lifecycleState === 'blocked' ? 'urgent' : releaseData ? 'active' : 'default'
  const releaseCompatibilityTone = releaseData ? 'active' : 'default'
  const releaseEnvironmentTone = releaseData ? 'active' : 'default'
  const releaseOwnerTone = releaseData ? 'active' : 'default'
  const releaseLifecycleValue = lifecycleLabel(releaseData?.lifecycleState, t)
  const releaseLifecycleHintValue = lifecycleHint(releaseData?.lifecycleState, t)
  const releaseRecoveryTone = statusTone(recoveryData?.status ?? 'skipped')
  const releaseRecoveryValue = statusLabel(recoveryData?.status, t)
  const releaseRecoveryHintValue = recoveryHint(recoveryData, t)
  const releaseCompatibilityValue = releaseData
    ? `${releaseData.compatibilityRules.length} rules`
    : t('common.loading', 'Loading...')
  const releaseCompatibilityHintValue = releaseData
    ? joinSemanticLabels(releaseData.compatibilityRules)
    : t('common.loading', 'Loading...')
  const releaseGlossaryValue = releaseData
    ? joinSemanticLabels(releaseData.glossary)
    : t('common.loading', 'Loading...')

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
    recoveryContext.error && {
      label: t('reliability.release.recovery', '恢复'),
      message: safeErrorMessage(recoveryContext.error, t('common.error', 'Error')),
    },
    releaseContext.error && {
      label: t('reliability.links.release_context', '发布与归属'),
      message: safeErrorMessage(releaseContext.error, t('common.error', 'Error')),
    },
  ].filter(Boolean) as Array<{ label: string; message: string }>

  const isRefreshing =
    me.isFetching ||
    authMode.isFetching ||
    preflight.isFetching ||
    apiKeys.isFetching ||
    mcpClients.isFetching ||
    gdprOps.isFetching ||
    deadDeliveries.isFetching ||
    recoveryContext.isFetching ||
    releaseContext.isFetching

  const refreshAll = () => {
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: meQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: authModeQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: preflightQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: apiKeysQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: mcpClientsQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: gdprOperationsQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: deliveriesQuery('dead').queryKey }),
      queryClient.invalidateQueries({ queryKey: recoveryContextQuery().queryKey }),
      queryClient.invalidateQueries({ queryKey: releaseContextQuery().queryKey }),
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
  const releaseVersionMetric: ReliabilityMetric = {
    tone: releaseVersionTone,
    heroTone: releaseVersionTone,
    value: releaseData?.serviceVersion || t('reliability.release.unknown', '未知'),
    hint: releaseStartedAt
      ? t('reliability.hints.release_started', { value: releaseStartedAt })
      : t('common.loading', 'Loading...'),
  }
  const releaseEnvironmentMetric: ReliabilityMetric = {
    tone: releaseEnvironmentTone,
    heroTone: releaseEnvironmentTone,
    value: releaseData?.environment || t('reliability.release.unknown', '未知'),
    hint: releaseData
      ? t('reliability.hints.release_environment', {
          value: releaseData.profile || t('reliability.release.unknown', '未知'),
        })
      : t('common.loading', 'Loading...'),
  }
  const releaseOwnerMetric: ReliabilityMetric = {
    tone: releaseOwnerTone,
    heroTone: releaseOwnerTone,
    value: releaseData?.ownerTeam || t('reliability.release.unknown', '未知'),
    hint: t('reliability.hints.release_owner', 'Runbook 与升级通道应保持同步。'),
  }
  const releaseLifecycleMetric: ReliabilityMetric = {
    tone: releaseLifecycleTone,
    heroTone: releaseLifecycleTone,
    value: releaseLifecycleValue,
    hint: releaseLifecycleHintValue,
  }
  const releaseRecoveryMetric: ReliabilityMetric = {
    tone: releaseRecoveryTone,
    heroTone: releaseRecoveryTone,
    value: releaseRecoveryValue,
    hint: releaseRecoveryHintValue,
  }
  const releaseCompatibilityMetric: ReliabilityMetric = {
    tone: releaseCompatibilityTone,
    heroTone: releaseCompatibilityTone,
    value: releaseCompatibilityValue,
    hint: releaseCompatibilityHintValue,
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
      releaseContext={{
        version: releaseVersionMetric,
        environment: releaseEnvironmentMetric,
        owner: releaseOwnerMetric,
        lifecycle: releaseLifecycleMetric,
        recovery: releaseRecoveryMetric,
        compatibility: releaseCompatibilityMetric,
        glossary: releaseGlossaryValue,
        runbookHref:
          releaseData?.runbookUrl ||
          'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
        escalationHref:
          releaseData?.escalationUrl || 'https://github.com/Phixsura/attune/issues/new/choose',
      }}
    />
  )
}
