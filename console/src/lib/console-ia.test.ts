import { describe, expect, it } from 'vitest'
import {
  canAccessConsoleItem,
  consoleNavGroupOrder,
  consoleNavItems,
  getConsoleGroupDefaultPath,
  getConsoleItemsForRole,
  resolveLegacySettingsPath,
} from '@/lib/console-ia'
import type { Role } from '@/lib/permissions'

describe('console IA', () => {
  it('keeps settings aliases unique across the navigation schema', () => {
    const aliases = consoleNavItems.flatMap((item) => item.settingsAliases ?? [])
    const unique = new Set(aliases)
    expect(unique.size).toBe(aliases.length)
  })

  it('gives every group at least one accessible destination for admin', () => {
    for (const group of consoleNavGroupOrder) {
      expect(
        consoleNavItems.some((item) => item.group === group && canAccessConsoleItem('admin', item)),
      ).toBe(true)
    }
  })

  it('exposes the terminal failure workbench as a visible feedback destination', () => {
    const item = consoleNavItems.find(
      (candidate) => candidate.path === '/feedback/terminal-failures',
    )

    expect(item).toBeDefined()
    if (!item) {
      throw new Error('terminal failure nav item missing')
    }
    expect(item).toMatchObject({
      group: 'feedback',
      labelKey: 'nav.terminal_failures',
      path: '/feedback/terminal-failures',
    })
    expect(canAccessConsoleItem('admin', item)).toBe(true)
    expect(canAccessConsoleItem('member', item)).toBe(true)
    expect(canAccessConsoleItem('viewer', item)).toBe(true)
  })

  it('exposes the portal inbox as a visible feedback destination', () => {
    const item = consoleNavItems.find((candidate) => candidate.path === '/feedback/portal')

    expect(item).toBeDefined()
    if (!item) {
      throw new Error('portal inbox nav item missing')
    }
    expect(item).toMatchObject({
      group: 'feedback',
      labelKey: 'nav.portal_inbox',
      path: '/feedback/portal',
    })
    expect(canAccessConsoleItem('admin', item)).toBe(true)
    expect(canAccessConsoleItem('member', item)).toBe(true)
    expect(canAccessConsoleItem('viewer', item)).toBe(true)
  })

  it('exposes customer requests to every feedback viewer', () => {
    const item = consoleNavItems.find(
      (candidate) => candidate.path === '/feedback/customer-requests',
    )

    expect(item).toBeDefined()
    if (!item) {
      throw new Error('customer requests nav item missing')
    }
    expect(item).toMatchObject({
      group: 'feedback',
      labelKey: 'nav.customer_requests',
      path: '/feedback/customer-requests',
      permission: 'customer_request:view',
    })
    expect(canAccessConsoleItem('admin', item)).toBe(true)
    expect(canAccessConsoleItem('member', item)).toBe(true)
    expect(canAccessConsoleItem('viewer', item)).toBe(true)
  })

  it('exposes the reliability summary as an admin-only administration destination', () => {
    const item = consoleNavItems.find(
      (candidate) => candidate.path === '/administration/reliability',
    )

    expect(item).toBeDefined()
    if (!item) {
      throw new Error('reliability nav item missing')
    }
    expect(item).toMatchObject({
      group: 'administration',
      labelKey: 'nav.reliability',
      path: '/administration/reliability',
    })
    expect(canAccessConsoleItem('admin', item)).toBe(true)
    expect(canAccessConsoleItem('member', item)).toBe(false)
    expect(canAccessConsoleItem('viewer', item)).toBe(false)
  })

  it('exposes operational settings to delegated admins', () => {
    const config = consoleNavItems.find(
      (candidate) => candidate.path === '/configuration/enrichment-runtime',
    )
    const audit = consoleNavItems.find(
      (candidate) => candidate.path === '/administration/audit-log',
    )
    const apiKeys = consoleNavItems.find((candidate) => candidate.path === '/integrations/api-keys')

    expect(config).toBeDefined()
    expect(audit).toBeDefined()
    expect(apiKeys).toBeDefined()
    if (!config || !audit || !apiKeys) {
      throw new Error('expected delegated-admin nav items to exist')
    }

    expect(canAccessConsoleItem('delegated_admin', config)).toBe(true)
    expect(canAccessConsoleItem('delegated_admin', audit)).toBe(true)
    expect(canAccessConsoleItem('delegated_admin', apiKeys)).toBe(false)
  })

  it('exposes public visibility moderation to operators but not viewers', () => {
    const item = consoleNavItems.find(
      (candidate) => candidate.path === '/integrations/public-visibility',
    )

    expect(item).toBeDefined()
    if (!item) {
      throw new Error('public visibility nav item missing')
    }
    expect(item).toMatchObject({
      group: 'integrations',
      labelKey: 'nav.public_visibility',
      path: '/integrations/public-visibility',
      permission: 'moderation:view',
    })
    expect(canAccessConsoleItem('admin', item)).toBe(true)
    expect(canAccessConsoleItem('delegated_admin', item)).toBe(true)
    expect(canAccessConsoleItem('member', item)).toBe(true)
    expect(canAccessConsoleItem('viewer', item)).toBe(false)
  })

  it('derives visible items from the same access rules used by routes', () => {
    const memberItems = getConsoleItemsForRole('member')
    const viewerItems = getConsoleItemsForRole('viewer')

    expect(memberItems.some((item) => item.path === '/integrations/api-keys')).toBe(true)
    expect(memberItems.some((item) => item.path === '/administration/gdpr')).toBe(false)
    expect(viewerItems.every((item) => canAccessConsoleItem('viewer', item))).toBe(true)
  })

  it('resolves legacy settings paths from the visible navigation schema', () => {
    expect(resolveLegacySettingsPath('gdpr', 'member')).toBe('/configuration/classification')
    expect(resolveLegacySettingsPath('api-keys', 'member')).toBe('/integrations/api-keys')
    expect(resolveLegacySettingsPath(undefined, 'viewer')).toBe('/feedback')
  })

  it('keeps delegated administration defaults focused on operational audit review', () => {
    const delegatedItems = getConsoleItemsForRole('delegated_admin')

    expect(getConsoleGroupDefaultPath('administration', 'delegated_admin')).toBe(
      '/administration/audit-log',
    )
    expect(delegatedItems.some((item) => item.path === '/administration/members')).toBe(true)
  })

  it('keeps every default group path inside that group for each role', () => {
    const roles: Role[] = ['admin', 'delegated_admin', 'member', 'viewer']

    for (const role of roles) {
      for (const group of consoleNavGroupOrder) {
        const path = getConsoleGroupDefaultPath(group, role)
        if (path === '/feedback') continue
        const item = consoleNavItems.find((candidate) => candidate.path === path)
        expect(item?.group).toBe(group)
      }
    }
  })
})
