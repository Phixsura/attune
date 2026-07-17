import { zhCN } from 'date-fns/locale'
import type { TFunction } from 'i18next'
import { afterEach, describe, expect, it } from 'vitest'
import type { AuditLogEntry, AuditLogFilters } from '@/features/audit-log/api/list-audit-log'
import type { AuditLogViewState, SavedAuditLogView } from '@/proto/attune/v1/audit'
import { auditLogPageTestables } from './audit-log-page'

afterEach(() => {
  window.history.replaceState({}, '', '/administration/audit-log')
})

const t = ((key: string, options?: Record<string, unknown>) =>
  options ? `${key}:${Object.values(options).join(':')}` : key) as TFunction

const baseEntry: AuditLogEntry = {
  action: 'member.remove',
  actorEmail: 'admin@example.com',
  actorId: 'user-1',
  actorIp: '203.0.113.5',
  actorType: 'admin',
  actorUserAgent: 'playwright',
  afterJson: '{"label":"after-snapshot","nested":{"x":2},"arr":[1,3]}',
  beforeJson: '{"label":"before-snapshot","nested":{"x":1},"arr":[1,2]}',
  createdAt: '2026-06-16T10:00:00Z',
  id: 'entry-1',
  summary: 'Removed member',
  targetId: 'member-1',
  targetType: 'member',
} as AuditLogEntry

describe('auditLogPageTestables', () => {
  it('round-trips filters and saved view state helpers', () => {
    const filters: AuditLogFilters = {
      actions: ['member.remove', 'member.invite'],
      actorId: 'user-1',
      from: '2026-06-16T09:00:00Z',
      targetId: 'member-1',
      targetType: 'member',
      to: '2026-06-17T09:00:00Z',
    }
    const state: AuditLogViewState = {
      actions: ['member.invite', 'member.remove'],
      actorId: 'user-1',
      actorType: '',
      from: '2026-06-16T09:00:00Z',
      inspectedEntryId: 'entry-1',
      localQuery: 'playwright',
      targetId: 'member-1',
      targetType: 'member',
      to: '2026-06-17T09:00:00Z',
    }
    const view: SavedAuditLogView = {
      id: 'view-1',
      name: '成员删除排查',
      state,
      createdAt: '2026-06-16T10:00:00Z',
      updatedAt: '2026-06-16T10:00:00Z',
    }

    expect(auditLogPageTestables.normalizeArray(['b', 'a'])).toEqual(['a', 'b'])
    expect(auditLogPageTestables.emptyToUndefined('  ')).toBeUndefined()
    expect(auditLogPageTestables.emptyToUndefined(' value ')).toBe('value')
    expect(auditLogPageTestables.hasSingleActionFilter(filters, 'member.remove')).toBe(false)
    expect(auditLogPageTestables.hasTargetFilter(filters, 'member-1')).toBe(true)
    expect(auditLogPageTestables.hasActorFilter(filters, 'user-1')).toBe(true)
    expect(auditLogPageTestables.countActiveFilters(filters)).toBe(6)
    expect(auditLogPageTestables.areFiltersEqual(filters, filters)).toBe(true)
    expect(
      auditLogPageTestables.draftToFilters(auditLogPageTestables.filtersToDraft(filters)),
    ).toEqual({
      actions: ['member.remove', 'member.invite'],
      actorId: 'user-1',
      from: '2026-06-16T09:00:00.000Z',
      targetId: 'member-1',
      targetType: 'member',
      to: '2026-06-17T09:00:00.000Z',
    })
    expect(
      auditLogPageTestables.buildSavedViewState({
        filters,
        inspectedEntryId: 'entry-1',
        localQuery: 'playwright',
      }),
    ).toEqual(state)
    expect(auditLogPageTestables.savedViewToAuditUrlState(state)).toEqual({
      filters: {
        actions: ['member.invite', 'member.remove'],
        actorId: 'user-1',
        from: '2026-06-16T09:00:00Z',
        targetId: 'member-1',
        targetType: 'member',
        to: '2026-06-17T09:00:00Z',
      },
      inspectedEntryId: 'entry-1',
      localQuery: 'playwright',
    })
    expect(auditLogPageTestables.savedViewStateSignature(state)).toBe(
      'member.invite|member.remove::user-1::2026-06-16T09:00:00Z::member-1::member::2026-06-17T09:00:00Z::playwright::entry-1',
    )
    expect(auditLogPageTestables.savedViewMatchesCurrent(view, state)).toBe(true)
    expect(auditLogPageTestables.suggestSavedViewName(state, t)).toBe(
      'audit_log.filters_actions_selected:2 · audit_log.fields.target_type: member',
    )
    expect(
      auditLogPageTestables
        .buildAuditLogSearchParams({
          filters,
          inspectedEntryId: 'entry-1',
          localQuery: 'playwright',
        })
        .toString(),
    ).toBe(
      'action=member.remove&action=member.invite&actorId=user-1&from=2026-06-16T09%3A00%3A00Z&targetId=member-1&targetType=member&to=2026-06-17T09%3A00%3A00Z&q=playwright&entry=entry-1',
    )
    expect(auditLogPageTestables.buildPresets(t)).toHaveLength(4)
  })

  it('covers burst grouping, matching, and change summaries', () => {
    const entries: AuditLogEntry[] = [
      baseEntry,
      {
        ...baseEntry,
        id: 'entry-2',
        createdAt: '2026-06-16T09:30:00Z',
        summary: 'Removed member again',
      },
      {
        ...baseEntry,
        id: 'entry-3',
        action: 'member.invite',
        createdAt: '2026-06-17T08:00:00Z',
        summary: 'Invited member',
        targetId: 'member-2',
        beforeJson: '{"status":"before"}',
        afterJson: '{"status":"after"}',
      },
    ]

    expect(auditLogPageTestables.buildBurstSignature(entries[0])).toBe(
      'target:member.remove|member|member-1',
    )
    expect(
      auditLogPageTestables.buildBurstSignature({
        ...baseEntry,
        id: 'x',
        targetId: '',
        targetType: '',
        summary: '  summary ',
      }),
    ).toBe('summary:member.remove|summary')
    expect(
      auditLogPageTestables.buildBurstSignature({
        ...baseEntry,
        id: 'x',
        targetId: '',
        targetType: '',
        summary: ' ',
      }),
    ).toBe('action:member.remove')

    const groups = auditLogPageTestables.buildStreamGroups(entries)
    expect(groups).toHaveLength(2)
    expect(groups[0].blocks.some((block) => block.kind === 'burst')).toBe(true)
    const burstContext = auditLogPageTestables.findBurstContextForEntry(groups, 'entry-2')
    expect(burstContext?.burstItemCount).toBe(2)
    expect(burstContext?.burstKey).toContain('entry-1|entry-2')

    const burst = auditLogPageTestables.createActivityBurst(entries.slice(0, 2))
    expect(burst.count).toBe(2)
    expect(burst.paths).toEqual(['arr[1]', 'label', 'nested.x'])

    expect(auditLogPageTestables.describeAuditChanges(entries[0])).toEqual({
      count: 3,
      paths: ['arr[1]', 'label', 'nested.x'],
    })
    expect(auditLogPageTestables.safeParseJson('not-json')).toBe('not-json')
    expect(auditLogPageTestables.collectChangedPaths({ a: 1 }, { a: 2 })).toEqual(['a'])
    expect(auditLogPageTestables.collectChangedPaths([1, 2], [1, 3])).toEqual(['[1]'])
    expect(auditLogPageTestables.isPlainObject({ a: 1 })).toBe(true)
    expect(auditLogPageTestables.isEqualValue({ a: [1, 2] }, { a: [1, 2] })).toBe(true)
    expect(auditLogPageTestables.formatJson('{"x":1}')).toContain('"x": 1')
    expect(auditLogPageTestables.formatDateChip('2026-06-16T10:00:00Z')).toMatch(
      /^\d{2}-\d{2} \d{2}:\d{2}$/,
    )
    const today = new Date()
    const yesterday = new Date(today)
    yesterday.setDate(today.getDate() - 1)
    expect(auditLogPageTestables.formatDayLabel(today)).toBe('Today')
    expect(auditLogPageTestables.formatDayLabel(today, zhCN)).toBe('今天')
    expect(auditLogPageTestables.formatDayLabel(yesterday)).toBe('Yesterday')
    expect(auditLogPageTestables.formatDayLabel(yesterday, zhCN)).toBe('昨天')
    expect(auditLogPageTestables.localDateTimeToISO('2026-06-16T10:00')).toMatch(/Z$/)
    expect(auditLogPageTestables.isoToLocalDateTimeInput('2026-06-16T10:00:00Z')).toMatch(
      /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/,
    )
    expect(auditLogPageTestables.isCommandSurfaceTarget(document.createElement('button'))).toBe(
      true,
    )
    expect(auditLogPageTestables.isTextEntryTarget(document.createElement('input'))).toBe(true)
    expect(auditLogPageTestables.matchesLocalAuditQuery(entries[0], 'member', t)).toBe(true)
    expect(auditLogPageTestables.describeLocalAuditMatch(entries[0], 'member')).toEqual(
      expect.arrayContaining(['summary', 'action', 'target']),
    )
    expect(auditLogPageTestables.describeLocalAuditMatch(entries[0], 'user-1')).toEqual(
      expect.arrayContaining(['actor']),
    )
    expect(auditLogPageTestables.describeLocalAuditMatch(entries[0], 'playwright')).toEqual(
      expect.arrayContaining(['metadata']),
    )
    expect(auditLogPageTestables.describeLocalAuditMatch(entries[0], 'before-snapshot')).toEqual(
      expect.arrayContaining(['snapshot']),
    )
  })

  it('reads and writes audit log url state', () => {
    window.history.replaceState(
      {},
      '',
      '/administration/audit-log?action=member.remove&actions=member.invite,member.approve&actorId=user-1&from=2026-06-16T09:00:00Z&targetId=member-1&targetType=member&to=2026-06-17T09:00:00Z&entry=entry-1&q=playwright',
    )

    expect(auditLogPageTestables.readAuditLogStateFromUrl()).toEqual({
      filters: {
        actions: ['member.remove', 'member.invite', 'member.approve'],
        actorId: 'user-1',
        from: '2026-06-16T09:00:00Z',
        targetId: 'member-1',
        targetType: 'member',
        to: '2026-06-17T09:00:00Z',
      },
      inspectedEntryId: 'entry-1',
      localQuery: 'playwright',
    })

    auditLogPageTestables.writeAuditLogStateToUrl(
      {
        filters: {
          actions: ['member.remove'],
        },
        inspectedEntryId: 'entry-2',
        localQuery: 'playwright',
      },
      'replace',
    )

    expect(window.location.search).toContain('action=member.remove')
    expect(window.location.search).toContain('entry=entry-2')
    expect(
      auditLogPageTestables.buildAuditLogUrl({
        filters: {},
        inspectedEntryId: null,
        localQuery: '',
      }),
    ).toContain('/administration/audit-log')
  })

  it('covers defensive helper fallbacks for missing state, invalid dates, and empty diffs', () => {
    expect(auditLogPageTestables.savedViewStateToFilters(undefined)).toEqual({})
    expect(auditLogPageTestables.savedViewToAuditUrlState(undefined)).toEqual({
      filters: {},
      inspectedEntryId: null,
      localQuery: '',
    })
    expect(
      auditLogPageTestables.findBurstContextForEntry(
        [
          {
            blocks: [{ item: baseEntry, kind: 'item' }],
            items: [baseEntry],
            key: '2026-06-16',
            label: '2026-06-16',
          },
        ],
        baseEntry.id,
      ),
    ).toBeNull()
    expect(
      auditLogPageTestables.findBurstContextForEntry(
        [
          {
            blocks: [{ burstKey: 'missing', item: baseEntry, kind: 'item' }],
            items: [baseEntry],
            key: '2026-06-16',
            label: '2026-06-16',
          },
        ],
        baseEntry.id,
      ),
    ).toBeNull()
    expect(
      auditLogPageTestables.describeAuditChanges({ ...baseEntry, afterJson: baseEntry.beforeJson }),
    ).toBeNull()
    expect(auditLogPageTestables.safeParseJson()).toBeUndefined()
    expect(auditLogPageTestables.formatJson('not-json')).toBe('not-json')
    expect(auditLogPageTestables.localDateTimeToISO('not-a-date')).toBeUndefined()
    expect(auditLogPageTestables.isoToLocalDateTimeInput('not-a-date')).toBe('')
    expect(auditLogPageTestables.describeLocalAuditMatch(baseEntry, 'nested.x')).toContain(
      'changePath',
    )
  })
})
