// SPDX-License-Identifier: Apache-2.0

/**
 * Centralized permission matrix for RBAC.
 *
 * This file defines what each role can do. Both frontend (UI visibility)
 * and backend (API authorization) should align with this matrix.
 */

export type Role = 'admin' | 'delegated_admin' | 'member' | 'viewer'

export type Permission =
  // Feedback
  | 'feedback:view'
  | 'feedback:edit'
  | 'feedback:delete'
  | 'feedback:batch_delete'
  // Customer Requests
  | 'customer_request:view'
  | 'customer_request:edit'
  | 'customer_request:merge'
  // Settings - AI Classification
  | 'settings:enrich_config:view'
  | 'settings:enrich_config:edit'
  // Settings - Enrichment Runtime
  | 'settings:enrichment_runtime:view'
  | 'settings:enrichment_runtime:edit'
  // Settings - Guard Policies
  | 'settings:guard_policies:view'
  | 'settings:guard_policies:edit'
  // Settings - Inbound Sources
  | 'settings:inbound:view'
  | 'settings:inbound:edit'
  // Settings - Notify Targets
  | 'settings:notify_targets:view'
  | 'settings:notify_targets:edit'
  // Settings - Digest
  | 'settings:digest:view'
  | 'settings:digest:edit'
  // Settings - API Keys
  | 'settings:api_keys:view'
  | 'settings:api_keys:edit'
  // Settings - GDPR
  | 'settings:gdpr:view'
  | 'settings:gdpr:export'
  | 'settings:gdpr:delete'
  // Settings - Audit Log
  | 'settings:audit_log:view'
  // Settings - Tags
  | 'settings:tags:view'
  | 'settings:tags:edit'
  // Settings - Workflow
  | 'settings:workflow:view'
  | 'settings:workflow:edit'
  // Settings - Members
  | 'settings:members:view'
  | 'settings:members:invite'
  | 'settings:members:edit_role'
  | 'settings:members:remove'
  // LLM Config
  | 'llm_config:view'
  | 'llm_config:edit'
  // Usage
  | 'usage:view'
  // Navigation
  | 'nav:settings'
  | 'nav:llm_config'

/**
 * Permission matrix: role -> permissions
 *
 * - admin: Full access
 * - delegated_admin: Operational settings + governance review
 * - member: Can view and edit feedback, view settings
 * - viewer: Read-only access to feedback and usage
 */
const PERMISSION_MATRIX: Record<Role, Set<Permission>> = {
  admin: new Set([
    // Feedback - full access
    'feedback:view',
    'feedback:edit',
    'feedback:delete',
    'feedback:batch_delete',
    'customer_request:view',
    'customer_request:edit',
    'customer_request:merge',
    // Settings - full access
    'settings:enrich_config:view',
    'settings:enrich_config:edit',
    'settings:enrichment_runtime:view',
    'settings:enrichment_runtime:edit',
    'settings:guard_policies:view',
    'settings:guard_policies:edit',
    'settings:inbound:view',
    'settings:inbound:edit',
    'settings:notify_targets:view',
    'settings:notify_targets:edit',
    'settings:digest:view',
    'settings:digest:edit',
    'settings:api_keys:view',
    'settings:api_keys:edit',
    'settings:gdpr:view',
    'settings:gdpr:export',
    'settings:gdpr:delete',
    'settings:audit_log:view',
    'settings:tags:view',
    'settings:tags:edit',
    'settings:workflow:view',
    'settings:workflow:edit',
    'settings:members:view',
    'settings:members:invite',
    'settings:members:edit_role',
    'settings:members:remove',
    // LLM Config - full access
    'llm_config:view',
    'llm_config:edit',
    // Usage
    'usage:view',
    // Navigation
    'nav:settings',
    'nav:llm_config',
  ]),

  delegated_admin: new Set([
    // Feedback - operational access without batch delete
    'feedback:view',
    'feedback:edit',
    'feedback:delete',
    'customer_request:view',
    'customer_request:edit',
    'customer_request:merge',
    // Settings - operational access
    'settings:enrich_config:view',
    'settings:enrich_config:edit',
    'settings:enrichment_runtime:view',
    'settings:enrichment_runtime:edit',
    'settings:inbound:view',
    'settings:inbound:edit',
    'settings:notify_targets:view',
    'settings:notify_targets:edit',
    'settings:digest:view',
    'settings:digest:edit',
    'settings:tags:view',
    'settings:tags:edit',
    'settings:workflow:view',
    'settings:workflow:edit',
    'settings:audit_log:view',
    'settings:gdpr:view',
    // Usage
    'usage:view',
    // Navigation
    'nav:settings',
  ]),

  member: new Set([
    // Feedback - can edit but not batch delete
    'feedback:view',
    'feedback:edit',
    'feedback:delete',
    'customer_request:view',
    'customer_request:edit',
    // Settings - view only (no edit)
    'settings:enrich_config:view',
    'settings:guard_policies:view',
    'settings:inbound:view',
    'settings:notify_targets:view',
    'settings:digest:view',
    'settings:api_keys:view',
    'settings:tags:view',
    'settings:workflow:view',
    'settings:members:view',
    // LLM Config - view only
    'llm_config:view',
    // Usage
    'usage:view',
    // Navigation
    'nav:settings',
    'nav:llm_config',
  ]),

  viewer: new Set([
    // Feedback - read only
    'feedback:view',
    'customer_request:view',
    // Usage
    'usage:view',
    // No settings access, no LLM config access
  ]),
}

/**
 * Check if a role has a specific permission.
 */
export function hasPermission(role: Role, permission: Permission): boolean {
  return PERMISSION_MATRIX[role]?.has(permission) ?? false
}

/**
 * Check if a role has any of the given permissions.
 */
export function hasAnyPermission(role: Role, permissions: Permission[]): boolean {
  return permissions.some((p) => hasPermission(role, p))
}

/**
 * Check if a role has all of the given permissions.
 */
export function hasAllPermissions(role: Role, permissions: Permission[]): boolean {
  return permissions.every((p) => hasPermission(role, p))
}

/**
 * Get all permissions for a role.
 */
export function getPermissions(role: Role): Permission[] {
  return Array.from(PERMISSION_MATRIX[role] ?? [])
}
