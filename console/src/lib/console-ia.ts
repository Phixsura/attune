import type { LucideIcon } from 'lucide-react'
import {
  BarChart3,
  Bell,
  Bot,
  DatabaseZap,
  GitBranch,
  History,
  KeyRound,
  Layers,
  LayoutDashboard,
  Lock,
  Radio,
  Scale,
  Settings2,
  ShieldCheck,
  ShieldEllipsis,
  Stethoscope,
  TriangleAlert,
  Users,
  Workflow,
} from 'lucide-react'
import { hasPermission, type Permission, type Role } from '@/lib/permissions'

export interface ConsoleRouteAccess {
  adminOnly?: boolean
  permission?: Permission
}

export interface ConsoleNavItem extends ConsoleRouteAccess {
  exact?: boolean
  group: ConsoleNavGroupId
  icon: LucideIcon
  labelKey: string
  path: string
  settingsAliases?: string[]
}

export type ConsoleNavGroupId =
  | 'feedback'
  | 'analytics'
  | 'configuration'
  | 'integrations'
  | 'administration'

export const consoleNavGroupOrder: ConsoleNavGroupId[] = [
  'feedback',
  'analytics',
  'configuration',
  'integrations',
  'administration',
]

export const consoleNavGroupLabelKeys: Record<ConsoleNavGroupId, string> = {
  feedback: 'shell.groups.feedback',
  analytics: 'shell.groups.analytics',
  configuration: 'shell.groups.configuration',
  integrations: 'shell.groups.integrations',
  administration: 'shell.groups.administration',
}

export const consoleNavItems: ConsoleNavItem[] = [
  {
    group: 'feedback',
    icon: LayoutDashboard,
    labelKey: 'nav.feedback',
    path: '/feedback',
    exact: true,
  },
  {
    group: 'feedback',
    icon: TriangleAlert,
    labelKey: 'nav.terminal_failures',
    path: '/feedback/terminal-failures',
  },
  {
    group: 'feedback',
    icon: Layers,
    labelKey: 'nav.clusters',
    path: '/feedback/clusters',
  },
  {
    group: 'analytics',
    icon: BarChart3,
    labelKey: 'nav.usage',
    path: '/analytics/usage',
    permission: 'usage:view',
  },
  {
    group: 'analytics',
    icon: Bot,
    labelKey: 'nav.llm_usage',
    path: '/analytics/llm-usage',
    permission: 'usage:view',
  },
  {
    group: 'configuration',
    icon: Settings2,
    labelKey: 'settings.areas.classification.title',
    path: '/configuration/classification',
    permission: 'settings:enrich_config:view',
    settingsAliases: ['classification'],
  },
  {
    group: 'configuration',
    icon: Radio,
    labelKey: 'nav.llm_config',
    path: '/configuration/llm',
    permission: 'llm_config:view',
    settingsAliases: ['llm'],
  },
  {
    group: 'configuration',
    icon: DatabaseZap,
    labelKey: 'settings.areas.enrichment_runtime.title',
    path: '/configuration/enrichment-runtime',
    permission: 'settings:enrichment_runtime:view',
    settingsAliases: ['enrichment-runtime', 'enrichment_runtime'],
  },
  {
    group: 'configuration',
    icon: Bell,
    labelKey: 'settings.areas.tags.title',
    path: '/configuration/tags',
    permission: 'settings:tags:view',
    settingsAliases: ['tags'],
  },
  {
    group: 'configuration',
    icon: Workflow,
    labelKey: 'settings.areas.workflow.title',
    path: '/configuration/workflow',
    permission: 'settings:workflow:view',
    settingsAliases: ['workflow'],
  },
  {
    group: 'integrations',
    icon: Radio,
    labelKey: 'nav.inbound_sources',
    path: '/integrations/inbound-sources',
    permission: 'settings:inbound:view',
    settingsAliases: ['inbound-sources', 'inbound_sources'],
  },
  {
    group: 'integrations',
    icon: Bell,
    labelKey: 'nav.notify_targets',
    path: '/integrations/notify-targets',
    permission: 'settings:notify_targets:view',
    settingsAliases: ['notify-targets', 'notify_targets'],
  },
  {
    group: 'integrations',
    icon: KeyRound,
    labelKey: 'nav.api_keys',
    path: '/integrations/api-keys',
    permission: 'settings:api_keys:view',
    settingsAliases: ['api-keys', 'api_keys'],
  },
  {
    group: 'integrations',
    icon: Bot,
    labelKey: 'nav.mcp_clients',
    path: '/mcp-clients',
    adminOnly: true,
  },
  {
    group: 'integrations',
    icon: GitBranch,
    labelKey: 'settings.areas.digest.title',
    path: '/integrations/digests',
    permission: 'settings:digest:view',
    settingsAliases: ['digest', 'digest-subscription', 'digest_subscription'],
  },
  {
    group: 'administration',
    icon: Users,
    labelKey: 'nav.members',
    path: '/administration/members',
    permission: 'settings:members:view',
    settingsAliases: ['members'],
  },
  {
    group: 'administration',
    icon: History,
    labelKey: 'nav.audit_log',
    path: '/administration/audit-log',
    permission: 'settings:audit_log:view',
    settingsAliases: ['audit', 'audit-log', 'audit_log'],
  },
  {
    group: 'administration',
    icon: Scale,
    labelKey: 'settings.areas.gdpr.title',
    path: '/administration/gdpr',
    permission: 'settings:gdpr:view',
    settingsAliases: ['gdpr'],
  },
  {
    group: 'administration',
    icon: ShieldCheck,
    labelKey: 'nav.guard_policies',
    path: '/administration/guard-policies',
    permission: 'settings:guard_policies:view',
    settingsAliases: ['guard-policies', 'guard_policies'],
  },
  {
    group: 'administration',
    icon: ShieldEllipsis,
    labelKey: 'nav.outbox_dead',
    path: '/administration/dead-deliveries',
    adminOnly: true,
  },
  {
    group: 'administration',
    icon: Stethoscope,
    labelKey: 'nav.system_readiness',
    path: '/administration/system-readiness',
    adminOnly: true,
  },
  {
    group: 'administration',
    icon: Lock,
    labelKey: 'nav.security',
    path: '/administration/security',
    adminOnly: true,
  },
]

export function canAccessConsoleItem(role: Role, access?: ConsoleRouteAccess): boolean {
  if (!access) return true
  if (access.adminOnly) return role === 'admin'
  if (access.permission) return hasPermission(role, access.permission)
  return true
}

export function getConsoleItemsForRole(role: Role): ConsoleNavItem[] {
  return consoleNavItems.filter((item) => canAccessConsoleItem(role, item))
}

export function getConsoleGroupDefaultPath(group: ConsoleNavGroupId, role: Role): string {
  return getConsoleItemsForRole(role).find((item) => item.group === group)?.path ?? '/feedback'
}

export function resolveLegacySettingsPath(section: string | undefined, role: Role): string {
  const visibleItems = getConsoleItemsForRole(role)
  const requested = section
    ? consoleNavItems.find((item) => item.settingsAliases?.includes(section))
    : undefined

  if (requested && canAccessConsoleItem(role, requested)) {
    return requested.path
  }

  return visibleItems.find((item) => item.settingsAliases?.length)?.path ?? '/feedback'
}
