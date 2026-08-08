import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { apiKeysQuery } from '@/features/api-keys/api/list-api-keys'
import { auditLogQuery } from '@/features/audit-log/api/list-audit-log'
import {
  externalSyncConnectionsQuery,
  externalSyncRecentEventsQuery,
} from '@/features/external-sync/api/external-sync'
import { gdprOperationsQuery } from '@/features/gdpr/api/gdpr-control'
import { inboundSourcesQuery } from '@/features/inbound-sources/api/list-inbound-sources'
import { llmChannelsQuery } from '@/features/llm-config/api/llm-config'
import { membersQuery } from '@/features/members/api/list-members'
import { notifyTargetsQuery } from '@/features/notify-targets/api/list-notify-targets'
import {
  moderationSubjectsQuery,
  publicVisibilityPolicyQuery,
} from '@/features/public-visibility/api/public-visibility'
import {
  replySendHookHealthQuery,
  replySendHookQuery,
} from '@/features/reply-send-hook/api/reply-send-hook'
import {
  requestNotificationDeliveriesQuery,
  requestNotificationSettingsQuery,
  requestNotificationWebhookTargetsQuery,
} from '@/features/request-notifications/api/request-notifications'
import { authModeQuery } from '@/features/security/api/auth-mode'
import {
  fieldPermissionsAuditLogFilters,
  governanceAuditLogFilters,
  SecurityPage,
} from '@/features/security/components/security-page'
import { preflightQuery } from '@/features/system-readiness/api/get-preflight'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/administration/security')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { adminOnly: true }),
  component: SecurityRoutePage,
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(authModeQuery()),
      context.queryClient.ensureQueryData(membersQuery()),
      context.queryClient.ensureQueryData(auditLogQuery(governanceAuditLogFilters)),
      context.queryClient.ensureQueryData(publicVisibilityPolicyQuery()),
      context.queryClient.ensureQueryData(moderationSubjectsQuery()),
      context.queryClient.ensureQueryData(auditLogQuery(fieldPermissionsAuditLogFilters)),
      context.queryClient.ensureQueryData(gdprOperationsQuery()),
      context.queryClient.ensureQueryData(notifyTargetsQuery()),
      context.queryClient.ensureQueryData(apiKeysQuery()),
      context.queryClient.ensureQueryData(inboundSourcesQuery()),
      context.queryClient.ensureQueryData(llmChannelsQuery()),
      context.queryClient.ensureQueryData(replySendHookQuery()),
      context.queryClient.ensureQueryData(replySendHookHealthQuery()),
      context.queryClient.ensureQueryData(preflightQuery()),
      context.queryClient.ensureQueryData(requestNotificationSettingsQuery()),
      context.queryClient.ensureQueryData(requestNotificationWebhookTargetsQuery()),
      context.queryClient.ensureQueryData(requestNotificationDeliveriesQuery(25)),
      context.queryClient.ensureQueryData(externalSyncConnectionsQuery()),
      context.queryClient.ensureQueryData(externalSyncRecentEventsQuery(25)),
    ]),
})

function SecurityRoutePage() {
  const { data: members } = useQuery(membersQuery())
  const { data: governanceAuditEntries } = useQuery(auditLogQuery(governanceAuditLogFilters))
  const { data: publicVisibilityPolicy } = useQuery(publicVisibilityPolicyQuery())
  const { data: moderationSubjects } = useQuery(moderationSubjectsQuery())
  const { data: fieldPermissionsAuditEntries } = useQuery(
    auditLogQuery(fieldPermissionsAuditLogFilters),
  )
  const { data: gdprOperations } = useQuery(gdprOperationsQuery())
  const { data: notifyTargets } = useQuery(notifyTargetsQuery())
  const { data: apiKeys } = useQuery(apiKeysQuery())
  const { data: inboundSources } = useQuery(inboundSourcesQuery())
  const { data: llmChannels } = useQuery(llmChannelsQuery())
  const { data: replySendHook } = useQuery(replySendHookQuery())
  const { data: replySendHookHealth } = useQuery(replySendHookHealthQuery())
  const { data: preflight } = useQuery(preflightQuery())
  const { data: requestNotificationSettings } = useQuery(requestNotificationSettingsQuery())
  const { data: requestNotificationWebhookTargets } = useQuery(
    requestNotificationWebhookTargetsQuery(),
  )
  const { data: requestNotificationDeliveries } = useQuery(requestNotificationDeliveriesQuery(25))
  const { data: externalSyncConnections } = useQuery(externalSyncConnectionsQuery())
  const { data: externalSyncEvents } = useQuery(externalSyncRecentEventsQuery(25))

  return (
    <SecurityPage
      evidence={{
        apiKeys,
        externalSyncConnections,
        externalSyncEvents,
        fieldPermissionsAuditEntries,
        gdprOperations,
        governanceAuditEntries,
        inboundSources,
        llmChannels,
        members,
        moderationSubjects,
        notifyTargets,
        preflightChecks: preflight?.checks,
        publicVisibilityPolicy,
        replySendHook,
        replySendHookHealth,
        requestNotificationDeliveries,
        requestNotificationSettings,
        requestNotificationWebhookTargets,
      }}
    />
  )
}
