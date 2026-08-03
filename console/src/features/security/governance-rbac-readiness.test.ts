import { describe, expect, it } from 'vitest'
import { buildGovernanceRbacReadiness } from './governance-rbac-readiness'

describe('buildGovernanceRbacReadiness', () => {
  it('verifies SSO, IdP, RBAC, last-admin, and access-review evidence', () => {
    const readiness = buildGovernanceRbacReadiness({
      auditEntries: [
        {
          id: 'audit-1',
          actorType: 'admin',
          actorId: 'admin-1',
          actorEmail: 'admin@example.com',
          actorIp: '127.0.0.1',
          actorUserAgent: 'test',
          action: 'member.update_role',
          targetType: 'member',
          targetId: 'member-2',
          summary: 'Updated member role',
          beforeJson: '{}',
          afterJson: '{}',
          createdAt: '2026-07-01T00:00:00Z',
        },
      ],
      authMode: { mode: 'sso_only' },
      lockouts: [],
      members: [
        activeMember('member-admin-1', 'admin', 'idp'),
        activeMember('member-admin-2', 'admin', 'idp'),
        activeMember('member-delegated', 'delegated_admin', 'idp'),
        activeMember('member-viewer', 'viewer', 'idp'),
      ],
      tokens: [breakglassToken()],
    })

    expect(readiness.fingerprint).toBe(
      'sso_only / 4 active members / 2 admins / 1 member audit events',
    )
    expect(readiness.summary).toBe('governance readiness evidence is verified')
    expect(readiness.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      ready: 5,
      total: 5,
      watch: 0,
    })
    expect(readiness.lanes.find((lane) => lane.key === 'sso_breakglass')).toMatchObject({
      signal: 'sso_only / 1 active break-glass / 0 lockouts',
      status: 'ready',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'access_review')).toMatchObject({
      signal: '1 member audit events / 1 actors',
      status: 'ready',
    })
  })

  it('blocks last-admin continuity and keeps missing IdP and access review visible', () => {
    const readiness = buildGovernanceRbacReadiness({
      auditEntries: [],
      authMode: { mode: 'hybrid' },
      lockouts: [
        {
          ip: '203.0.113.10',
          locked_until: '2026-07-01T00:30:00Z',
          remaining_mins: 10,
          attempts: 5,
        },
      ],
      members: [
        activeMember('member-admin-1', 'admin', 'manual'),
        activeMember('member-1', 'member', 'manual'),
      ],
      tokens: [breakglassToken()],
    })

    expect(readiness.summary).toBe('1 governance readiness checks are blocked')
    expect(readiness.totals).toEqual({
      blocked: 1,
      needs_data: 0,
      ready: 0,
      total: 5,
      watch: 4,
    })
    expect(readiness.lanes.find((lane) => lane.key === 'sso_breakglass')).toMatchObject({
      signal: 'hybrid / 1 active break-glass / 1 lockouts',
      status: 'watch',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'last_admin_guard')).toMatchObject({
      signal: '1 active admins',
      status: 'blocked',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'access_review')).toMatchObject({
      signal: '0 member audit events / 0 actors',
      status: 'watch',
    })
  })

  it('keeps absent governance evidence as data gaps', () => {
    const readiness = buildGovernanceRbacReadiness({})

    expect(readiness.fingerprint).toBe(
      'auth unknown / 0 active members / 0 admins / 0 member audit events',
    )
    expect(readiness.summary).toBe('5 governance checks need evidence')
    expect(readiness.totals).toEqual({
      blocked: 0,
      needs_data: 5,
      ready: 0,
      total: 5,
      watch: 0,
    })
  })

  it('counts unknown roles as members and recognizes member audit actions by action name', () => {
    const readiness = buildGovernanceRbacReadiness({
      auditEntries: [
        {
          id: 'audit-action',
          actorType: 'admin',
          actorId: 'admin-1',
          actorEmail: 'admin@example.com',
          actorIp: '127.0.0.1',
          actorUserAgent: 'test',
          action: 'member.remove',
          targetType: 'api_key',
          targetId: 'member-old',
          summary: 'Removed member',
          beforeJson: '{}',
          afterJson: '{}',
          createdAt: '2026-07-01T00:00:00Z',
        },
      ],
      authMode: { mode: 'sso_only' },
      lockouts: [],
      members: [
        activeMember('member-admin-1', 'admin', 'idp'),
        activeMember('member-admin-2', 'admin', 'idp'),
        activeMember('member-custom', 'custom_role', 'idp'),
        activeMember('member-viewer', 'viewer', 'idp'),
      ],
      tokens: [breakglassToken()],
    })

    expect(readiness.summary).toBe('governance readiness evidence is verified')
    expect(readiness.lanes.find((lane) => lane.key === 'rbac_roles')).toMatchObject({
      evidence: '2 admin / 0 delegated admin / 1 member / 1 viewer',
      status: 'ready',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'access_review')).toMatchObject({
      signal: '1 member audit events / 1 actors',
      status: 'ready',
    })
  })

  it('separates expiring and inactive break-glass tokens', () => {
    const expiring = buildGovernanceRbacReadiness({
      auditEntries: [],
      authMode: { mode: 'sso_only' },
      lockouts: [],
      members: [
        activeMember('member-admin-1', 'admin', 'idp'),
        activeMember('member-admin-2', 'admin', 'idp'),
        activeMember('member-viewer', 'viewer', 'idp'),
      ],
      tokens: [
        {
          ...breakglassToken(),
          expires_at: new Date(Date.now() + 30 * 60 * 1000).toISOString(),
        },
      ],
    })
    const inactive = buildGovernanceRbacReadiness({
      auditEntries: [],
      authMode: { mode: 'sso_only' },
      lockouts: [],
      members: [
        activeMember('member-admin-1', 'admin', 'idp'),
        activeMember('member-admin-2', 'admin', 'idp'),
        activeMember('member-viewer', 'viewer', 'idp'),
      ],
      tokens: [
        {
          ...breakglassToken(),
          expires_at: 'not-a-date',
        },
      ],
    })

    expect(expiring.lanes.find((lane) => lane.key === 'sso_breakglass')).toMatchObject({
      evidence: 'sso_only mode / 0 active tokens / 1 expiring / 0 lockouts',
      status: 'ready',
    })
    expect(inactive.lanes.find((lane) => lane.key === 'sso_breakglass')).toMatchObject({
      evidence: 'sso_only mode / 0 active tokens / 0 expiring / 0 lockouts',
      status: 'blocked',
    })
  })

  it('blocks empty member inventories and watches invite-only access reviews', () => {
    const readiness = buildGovernanceRbacReadiness({
      auditEntries: [
        {
          id: 'audit-invite',
          actorType: 'admin',
          actorId: 'admin-1',
          actorEmail: 'admin@example.com',
          actorIp: '127.0.0.1',
          actorUserAgent: 'test',
          action: 'member.invite',
          targetType: 'member',
          targetId: 'member-new',
          summary: 'Invited member',
          beforeJson: '{}',
          afterJson: '{}',
          createdAt: '2026-07-01T00:00:00Z',
        },
      ],
      authMode: { mode: 'sso_only' },
      lockouts: [],
      members: [],
      tokens: [
        {
          ...breakglassToken(),
          revoked_at: '2026-07-01T00:10:00Z',
        },
      ],
    })

    expect(readiness.summary).toBe('4 governance readiness checks are blocked')
    expect(readiness.lanes.find((lane) => lane.key === 'sso_breakglass')).toMatchObject({
      status: 'blocked',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'scim_idp')).toMatchObject({
      status: 'blocked',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'rbac_roles')).toMatchObject({
      status: 'blocked',
    })
    expect(readiness.lanes.find((lane) => lane.key === 'access_review')).toMatchObject({
      signal: '1 member audit events / 1 actors',
      status: 'watch',
    })
  })
})

function activeMember(id: string, role: string, roleSource: string) {
  return {
    id,
    acceptedAt: '1',
    email: `${id}@example.com`,
    invitedAt: '0',
    memberType: 'oidc_user',
    role,
    roleSource,
    userId: id,
  }
}

function breakglassToken() {
  return {
    id: 'token-1',
    admin_email: 'admin@example.com',
    expires_at: '2099-07-01T00:00:00Z',
    issued_by: 'admin-1',
    issued_at: '2026-07-01T00:00:00Z',
    status: 'valid',
  }
}
