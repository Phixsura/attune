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

  it('keeps every default group path inside that group for each role', () => {
    const roles: Role[] = ['admin', 'member', 'viewer']

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
