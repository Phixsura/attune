import { describe, expect, it, vi } from 'vitest'
import { AssignmentPolicyPanel } from '@/features/feedback/components/assignment-policy-panel'
import type { FeedbackAssignmentPolicy } from '@/proto/attune/v1/ingest'
import type { Member } from '@/proto/attune/v1/member'
import { renderWithProviders, screen } from '@/testing/test-utils'

const policy: FeedbackAssignmentPolicy = {
  version: 1,
  updatedBy: 'system',
  note: 'Default assignment policy',
  rules: [
    {
      ruleKey: 'urgent_open',
      ruleName: 'Urgent open feedback',
      ownerLane: 'support_triage',
      severity: 'critical',
      slaHours: 24,
      enabled: true,
      rationale: 'urgent customer-visible feedback needs a fast owner',
    },
  ],
}

const members: Member[] = [
  {
    id: 'member-1',
    memberType: 'tenant_user',
    userId: 'owner-1',
    email: 'owner@example.com',
    role: 'member',
    roleSource: 'manual',
    invitedAt: '0',
    acceptedAt: '1',
  },
  {
    id: 'member-viewer',
    memberType: 'tenant_user',
    userId: 'viewer-1',
    email: 'viewer@example.com',
    role: 'viewer',
    roleSource: 'manual',
    invitedAt: '0',
    acceptedAt: '1',
  },
]

describe('AssignmentPolicyPanel', () => {
  it('edits policy owner lane, SLA, default owner, and audit note', async () => {
    const onSave = vi.fn()
    const { user } = renderWithProviders(
      <AssignmentPolicyPanel
        policy={policy}
        members={members}
        canEdit
        isLoading={false}
        isMembersLoading={false}
        isSaving={false}
        isPreviewing={false}
        isRestoring={false}
        previewFeedbackIds={['7']}
        dryRun={undefined}
        revisions={[]}
        onSave={onSave}
        onDryRun={vi.fn()}
        onRestore={vi.fn()}
      />,
    )

    await user.clear(screen.getByLabelText('Urgent open feedback owner lane'))
    await user.type(screen.getByLabelText('Urgent open feedback owner lane'), 'enterprise_triage')
    await user.clear(screen.getByLabelText('Urgent open feedback SLA 小时'))
    await user.type(screen.getByLabelText('Urgent open feedback SLA 小时'), '8')
    await user.click(screen.getByLabelText('Urgent open feedback 默认负责人'))
    await user.click(screen.getByRole('option', { name: 'owner@example.com' }))
    expect(screen.queryByRole('option', { name: 'viewer@example.com' })).not.toBeInTheDocument()
    await user.type(screen.getByLabelText('变更备注'), 'Enterprise escalation policy')
    await user.click(screen.getByRole('button', { name: '保存策略' }))

    expect(onSave).toHaveBeenCalledWith({
      rules: [
        {
          ...policy.rules[0],
          ownerLane: 'enterprise_triage',
          slaHours: 8,
          defaultOwnerMemberId: 'member-1',
        },
      ],
      note: 'Enterprise escalation policy',
    })
  })

  it('keeps save disabled for read-only users', () => {
    renderWithProviders(
      <AssignmentPolicyPanel
        policy={policy}
        members={members}
        canEdit={false}
        isLoading={false}
        isMembersLoading={false}
        isSaving={false}
        isPreviewing={false}
        isRestoring={false}
        previewFeedbackIds={['7']}
        dryRun={undefined}
        revisions={[]}
        onSave={vi.fn()}
        onDryRun={vi.fn()}
        onRestore={vi.fn()}
      />,
    )

    expect(screen.getByRole('button', { name: '保存策略' })).toBeDisabled()
    expect(screen.getByLabelText('Urgent open feedback owner lane')).toBeDisabled()
  })

  it('previews policy changes and restores previous versions', async () => {
    const onDryRun = vi.fn()
    const onRestore = vi.fn()
    const { user } = renderWithProviders(
      <AssignmentPolicyPanel
        policy={{ ...policy, version: 2, updatedBy: 'admin-2', note: 'tighten urgent SLA' }}
        members={members}
        canEdit
        isLoading={false}
        isMembersLoading={false}
        isSaving={false}
        isPreviewing={false}
        isRestoring={false}
        previewFeedbackIds={['7']}
        dryRun={{
          totalMatched: 1,
          changed: 1,
          recommendations: [],
          failed: [],
          impacts: [
            {
              feedbackId: '7',
              currentRuleKey: 'urgent_open',
              currentRuleName: 'Urgent open feedback',
              currentOwnerLane: 'support_triage',
              currentSlaHours: 24,
              draftRuleKey: 'urgent_open',
              draftRuleName: 'Urgent open feedback',
              draftOwnerLane: 'enterprise_triage',
              draftSlaHours: 8,
              changed: true,
            },
          ],
        }}
        revisions={[
          { version: 2, updatedBy: 'admin-2', note: 'tighten urgent SLA', rules: policy.rules },
          { version: 1, updatedBy: 'admin-1', note: 'enterprise lane', rules: policy.rules },
        ]}
        onSave={vi.fn()}
        onDryRun={onDryRun}
        onRestore={onRestore}
      />,
    )

    await user.click(screen.getByRole('button', { name: '预演影响' }))
    expect(onDryRun).toHaveBeenCalledWith({ rules: policy.rules, feedbackIds: ['7'] })
    expect(screen.getByText('1/1 条反馈会变化')).toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: '恢复' })[1])
    expect(onRestore).toHaveBeenCalledWith({ version: 1, note: '' })
  })
})
