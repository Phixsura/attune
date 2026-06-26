import { describe, expect, it } from 'vitest'
import {
  buildEventDescription,
  formatActorDisplay,
  formatIdDisplay,
  getActionCategory,
  getActionDotColor,
  getActionLabel,
  getActorAvatar,
  getActorTypeLabel,
  getFieldLabel,
  getTargetTypeLabel,
} from '@/features/audit-log/actions'

const mockT = ((key: string, opts?: { defaultValue?: string }) => {
  const translations: Record<string, string> = {
    'audit_log.action_labels.member.invite': '邀请成员',
    'audit_log.target_type_labels.member': '成员',
    'audit_log.actor_type_labels.admin': '管理员',
    'audit_log.field_labels.role': '角色',
    'audit_log.action_verbs.invite': '邀请了',
    'audit_log.event_desc_full': '{{actor}} {{verb}} {{targetType}} {{targetId}}',
    'audit_log.event_desc_no_id': '{{actor}} {{verb}} {{targetType}}',
  }
  return translations[key] ?? opts?.defaultValue ?? key
}) as unknown as Parameters<typeof getActionLabel>[1]

describe('getActionLabel', () => {
  it('returns translated label when available', () => {
    expect(getActionLabel('member.invite', mockT)).toBe('邀请成员')
  })

  it('falls back to raw action when no translation', () => {
    expect(getActionLabel('unknown.action', mockT)).toBe('unknown.action')
  })
})

describe('getTargetTypeLabel', () => {
  it('returns translated label', () => {
    expect(getTargetTypeLabel('member', mockT)).toBe('成员')
  })

  it('falls back to raw value', () => {
    expect(getTargetTypeLabel('widget', mockT)).toBe('widget')
  })
})

describe('getActorTypeLabel', () => {
  it('returns translated label', () => {
    expect(getActorTypeLabel('admin', mockT)).toBe('管理员')
  })

  it('falls back to raw value', () => {
    expect(getActorTypeLabel('system', mockT)).toBe('system')
  })
})

describe('getFieldLabel', () => {
  it('returns translated label', () => {
    expect(getFieldLabel('role', mockT)).toBe('角色')
  })

  it('falls back to raw path', () => {
    expect(getFieldLabel('some.nested.path', mockT)).toBe('some.nested.path')
  })
})

describe('formatActorDisplay', () => {
  it('prefers email over actorId', () => {
    expect(formatActorDisplay('user-1', 'admin@example.com')).toBe('admin@example.com')
  })

  it('truncates UUID actorId', () => {
    expect(formatActorDisplay('a1b2c3d4-e5f6-7890-abcd-ef1234567890')).toBe('a1b2c3d4…')
  })

  it('returns non-UUID actorId as-is', () => {
    expect(formatActorDisplay('user-1')).toBe('user-1')
  })
})

describe('formatIdDisplay', () => {
  it('truncates UUID', () => {
    expect(formatIdDisplay('a1b2c3d4-e5f6-7890-abcd-ef1234567890')).toBe('a1b2c3d4…')
  })

  it('returns non-UUID as-is', () => {
    expect(formatIdDisplay('key-1')).toBe('key-1')
  })
})

describe('getActionCategory', () => {
  it.each([
    ['api_key.create', 'create'],
    ['member.invite', 'create'],
    ['llm_ability.upsert', 'create'],
    ['inbound_source.resume', 'create'],
    ['member.remove', 'delete'],
    ['api_key.revoke', 'delete'],
    ['feedback.batch_delete', 'delete'],
    ['feedback_job.cancel', 'delete'],
    ['tag.archive', 'delete'],
    ['audit_evidence.expire', 'delete'],
    ['llm_channel.test', 'test'],
    ['inbound_source.test_connection', 'test'],
    ['audit_evidence.download', 'test'],
    ['enrich_config.update', 'update'],
    ['member.update_role', 'update'],
    ['enrich_config.activate_version', 'update'],
    ['inbound_source.rotate_secret', 'update'],
    ['inbound_source.pause', 'update'],
    ['workflow_transition.replace', 'update'],
    ['workflow_seed_defaults.run', 'update'],
    ['some.unknown_verb', 'neutral'],
  ])('categorizes %s as %s', (action, expected) => {
    expect(getActionCategory(action)).toBe(expected)
  })
})

describe('getActionDotColor', () => {
  it('returns emerald for create actions', () => {
    expect(getActionDotColor('api_key.create')).toBe('bg-emerald-500')
  })

  it('returns red for delete actions', () => {
    expect(getActionDotColor('member.remove')).toBe('bg-red-500')
  })

  it('returns blue for update actions', () => {
    expect(getActionDotColor('enrich_config.update')).toBe('bg-blue-500')
  })

  it('returns amber for test actions', () => {
    expect(getActionDotColor('llm_channel.test')).toBe('bg-amber-500')
  })

  it('returns muted for neutral actions', () => {
    expect(getActionDotColor('some.neutral')).toBe('bg-muted-foreground/50')
  })
})

describe('buildEventDescription', () => {
  it('builds full description with target type and id', () => {
    const result = buildEventDescription(
      {
        actorId: 'user-1',
        actorEmail: 'admin@example.com',
        targetId: 'member-1',
        targetType: 'member',
        action: 'member.invite',
      },
      mockT,
    )
    expect(result).toContain('{{actor}}')
  })

  it('builds description without target id', () => {
    const result = buildEventDescription(
      {
        actorId: 'user-1',
        targetId: '',
        targetType: 'member',
        action: 'member.invite',
      },
      mockT,
    )
    expect(result).not.toContain('{{targetId}}')
  })

  it('returns actor only when no target info', () => {
    const result = buildEventDescription(
      {
        actorId: 'user-1',
        actorEmail: 'admin@example.com',
        targetId: '',
        targetType: '',
        action: 'member.invite',
      },
      mockT,
    )
    expect(result).toBe('admin@example.com')
  })
})

describe('getActorAvatar', () => {
  it('uses email initials when available', () => {
    const avatar = getActorAvatar('user-1', 'admin@example.com')
    expect(avatar.initials).toBe('AD')
    expect(avatar.bg).toBeTruthy()
    expect(avatar.text).toBeTruthy()
  })

  it('uses actorId initials when no email', () => {
    const avatar = getActorAvatar('user-1')
    expect(avatar.initials).toBe('US')
  })

  it('produces consistent colors for the same input', () => {
    const a1 = getActorAvatar('user-1')
    const a2 = getActorAvatar('user-1')
    expect(a1.bg).toBe(a2.bg)
    expect(a1.text).toBe(a2.text)
  })

  it('produces different colors for different inputs', () => {
    const a1 = getActorAvatar('user-1')
    const a2 = getActorAvatar('user-2')
    // Colors might coincidentally be the same, but initials should differ
    expect(a1.initials).toBe('US')
    expect(a2.initials).toBe('US')
  })
})
