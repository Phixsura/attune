// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest'
import {
  getPermissions,
  hasAllPermissions,
  hasAnyPermission,
  hasPermission,
  type Role,
} from './permissions'

describe('hasPermission', () => {
  describe('admin role', () => {
    const role: Role = 'admin'

    it('has all feedback permissions', () => {
      expect(hasPermission(role, 'feedback:view')).toBe(true)
      expect(hasPermission(role, 'feedback:edit')).toBe(true)
      expect(hasPermission(role, 'feedback:delete')).toBe(true)
      expect(hasPermission(role, 'feedback:batch_delete')).toBe(true)
      expect(hasPermission(role, 'customer_request:view')).toBe(true)
      expect(hasPermission(role, 'customer_request:edit')).toBe(true)
      expect(hasPermission(role, 'customer_request:merge')).toBe(true)
    })

    it('has all settings permissions', () => {
      expect(hasPermission(role, 'settings:enrich_config:view')).toBe(true)
      expect(hasPermission(role, 'settings:enrich_config:edit')).toBe(true)
      expect(hasPermission(role, 'settings:members:view')).toBe(true)
      expect(hasPermission(role, 'settings:members:invite')).toBe(true)
      expect(hasPermission(role, 'settings:members:edit_role')).toBe(true)
      expect(hasPermission(role, 'settings:members:remove')).toBe(true)
    })

    it('has navigation permissions', () => {
      expect(hasPermission(role, 'nav:settings')).toBe(true)
      expect(hasPermission(role, 'nav:llm_config')).toBe(true)
    })
  })

  describe('delegated admin role', () => {
    const role: Role = 'delegated_admin'

    it('can manage operational feedback and settings', () => {
      expect(hasPermission(role, 'feedback:view')).toBe(true)
      expect(hasPermission(role, 'feedback:edit')).toBe(true)
      expect(hasPermission(role, 'feedback:delete')).toBe(true)
      expect(hasPermission(role, 'feedback:batch_delete')).toBe(false)
      expect(hasPermission(role, 'customer_request:view')).toBe(true)
      expect(hasPermission(role, 'customer_request:edit')).toBe(true)
      expect(hasPermission(role, 'customer_request:merge')).toBe(true)
      expect(hasPermission(role, 'settings:enrich_config:view')).toBe(true)
      expect(hasPermission(role, 'settings:enrich_config:edit')).toBe(true)
      expect(hasPermission(role, 'settings:notify_targets:view')).toBe(true)
      expect(hasPermission(role, 'settings:notify_targets:edit')).toBe(true)
      expect(hasPermission(role, 'settings:audit_log:view')).toBe(true)
      expect(hasPermission(role, 'settings:gdpr:view')).toBe(true)
    })

    it('does not get security-owner navigation', () => {
      expect(hasPermission(role, 'settings:members:view')).toBe(false)
      expect(hasPermission(role, 'settings:api_keys:view')).toBe(false)
      expect(hasPermission(role, 'llm_config:view')).toBe(false)
      expect(hasPermission(role, 'nav:llm_config')).toBe(false)
    })
  })

  describe('member role', () => {
    const role: Role = 'member'

    it('can view and edit feedback but not batch delete', () => {
      expect(hasPermission(role, 'feedback:view')).toBe(true)
      expect(hasPermission(role, 'feedback:edit')).toBe(true)
      expect(hasPermission(role, 'feedback:delete')).toBe(true)
      expect(hasPermission(role, 'feedback:batch_delete')).toBe(false)
      expect(hasPermission(role, 'customer_request:view')).toBe(true)
      expect(hasPermission(role, 'customer_request:edit')).toBe(true)
      expect(hasPermission(role, 'customer_request:merge')).toBe(false)
    })

    it('can view settings but not edit', () => {
      expect(hasPermission(role, 'settings:enrich_config:view')).toBe(true)
      expect(hasPermission(role, 'settings:enrich_config:edit')).toBe(false)
      expect(hasPermission(role, 'settings:gdpr:view')).toBe(false)
      expect(hasPermission(role, 'settings:members:view')).toBe(true)
      expect(hasPermission(role, 'settings:members:invite')).toBe(false)
      expect(hasPermission(role, 'settings:members:edit_role')).toBe(false)
      expect(hasPermission(role, 'settings:members:remove')).toBe(false)
    })

    it('has navigation to settings', () => {
      expect(hasPermission(role, 'nav:settings')).toBe(true)
      expect(hasPermission(role, 'nav:llm_config')).toBe(true)
    })
  })

  describe('viewer role', () => {
    const role: Role = 'viewer'

    it('can only view feedback', () => {
      expect(hasPermission(role, 'feedback:view')).toBe(true)
      expect(hasPermission(role, 'feedback:edit')).toBe(false)
      expect(hasPermission(role, 'feedback:delete')).toBe(false)
      expect(hasPermission(role, 'feedback:batch_delete')).toBe(false)
      expect(hasPermission(role, 'customer_request:view')).toBe(true)
      expect(hasPermission(role, 'customer_request:edit')).toBe(false)
      expect(hasPermission(role, 'customer_request:merge')).toBe(false)
    })

    it('cannot access settings', () => {
      expect(hasPermission(role, 'settings:enrich_config:view')).toBe(false)
      expect(hasPermission(role, 'settings:members:view')).toBe(false)
      expect(hasPermission(role, 'nav:settings')).toBe(false)
    })

    it('can view usage', () => {
      expect(hasPermission(role, 'usage:view')).toBe(true)
    })
  })
})

describe('hasAnyPermission', () => {
  it('returns true if role has any of the permissions', () => {
    expect(hasAnyPermission('member', ['feedback:edit', 'feedback:batch_delete'])).toBe(true)
    expect(hasAnyPermission('viewer', ['feedback:view', 'feedback:edit'])).toBe(true)
  })

  it('returns false if role has none of the permissions', () => {
    expect(hasAnyPermission('viewer', ['feedback:edit', 'feedback:delete'])).toBe(false)
  })
})

describe('hasAllPermissions', () => {
  it('returns true if role has all permissions', () => {
    expect(hasAllPermissions('admin', ['feedback:view', 'feedback:edit'])).toBe(true)
    expect(hasAllPermissions('member', ['feedback:view', 'feedback:edit'])).toBe(true)
  })

  it('returns false if role is missing any permission', () => {
    expect(hasAllPermissions('member', ['feedback:view', 'feedback:batch_delete'])).toBe(false)
    expect(hasAllPermissions('viewer', ['feedback:view', 'feedback:edit'])).toBe(false)
  })
})

describe('getPermissions', () => {
  it('returns all permissions for admin', () => {
    const perms = getPermissions('admin')
    expect(perms).toContain('feedback:view')
    expect(perms).toContain('settings:members:invite')
    expect(perms.length).toBeGreaterThan(20)
  })

  it('returns operational permissions for delegated admin', () => {
    const perms = getPermissions('delegated_admin')
    expect(perms).toContain('feedback:view')
    expect(perms).toContain('settings:enrich_config:edit')
    expect(perms).toContain('settings:audit_log:view')
    expect(perms).toContain('settings:gdpr:view')
    expect(perms).not.toContain('settings:members:invite')
    expect(perms).not.toContain('nav:llm_config')
  })

  it('returns limited permissions for viewer', () => {
    const perms = getPermissions('viewer')
    expect(perms).toContain('feedback:view')
    expect(perms).toContain('customer_request:view')
    expect(perms).toContain('usage:view')
    expect(perms).not.toContain('feedback:edit')
    expect(perms.length).toBe(3)
  })

  it('returns empty array for invalid role', () => {
    expect(getPermissions('invalid' as Role)).toEqual([])
  })
})
