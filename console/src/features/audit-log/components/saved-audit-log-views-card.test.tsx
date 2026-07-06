import { describe, expect, it, vi } from 'vitest'
import { SavedAuditLogViewsCard } from '@/features/audit-log/components/saved-audit-log-views-card'
import { renderWithProviders, screen } from '@/testing/test-utils'

const view = {
  id: 'view-1',
  name: '成员删除排查',
  state: {
    actions: ['member.remove', 'member.invite'],
    actorType: '',
    actorId: 'user-1',
    targetType: 'member',
    targetId: 'member-42',
    from: '',
    to: '',
    localQuery: 'playwright',
  },
  createdAt: '2026-06-16T10:00:00Z',
  updatedAt: '2026-06-16T10:05:00Z',
}

describe('SavedAuditLogViewsCard', () => {
  it('renders saved views and forwards apply and delete actions', async () => {
    const onApplyView = vi.fn()
    const onDeleteView = vi.fn()

    const { user } = renderWithProviders(
      <SavedAuditLogViewsCard
        deletingViewId={null}
        errorMessage={null}
        isLoading={false}
        onApplyView={onApplyView}
        onDeleteView={onDeleteView}
        onSaveAsNew={vi.fn()}
        onSaveCurrent={vi.fn()}
        selectedViewId="view-1"
        selectedViewMatchesCurrent={true}
        selectedViewName="成员删除排查"
        views={[view]}
      />,
    )

    expect(screen.getByText('已保存视图')).toBeInTheDocument()
    expect(screen.getByText('当前选中视图：成员删除排查')).toBeInTheDocument()
    expect(screen.getByText('当前状态与保存视图一致。')).toBeInTheDocument()
    expect(
      screen.getByText('动作 2 个 · 操作者 user-1 · 目标 member / member-42'),
    ).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /^成员删除排查/ }))
    expect(onApplyView).toHaveBeenCalledWith(view)

    await user.click(screen.getByRole('button', { name: '删除视图 成员删除排查' }))
    expect(onDeleteView).toHaveBeenCalledWith(view)
  })
})
