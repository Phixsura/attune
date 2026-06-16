import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { AuditLogPage } from '@/features/audit-log/components/audit-log-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('@/features/session/hooks/use-permissions', () => ({
  usePermissions: () => ({
    can: () => true,
  }),
}))

describe('AuditLogPage', () => {
  it('shows an explicit error state when the audit log request fails', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({ message: 'forbidden' }, { status: 403 }),
      ),
    )

    renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByText('加载审计记录失败')).toBeInTheDocument()
    })
    expect(screen.queryByText('暂无审计记录')).not.toBeInTheDocument()
  })

  it('only refreshes filters after the user applies them', async () => {
    let requests = 0
    server.use(
      http.get('/fb/v1/console/audit-log', () => {
        requests += 1
        return HttpResponse.json({ items: [] })
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(requests).toBe(1)
    })

    await user.click(screen.getByRole('button', { name: '动作' }))
    await user.click(screen.getByText('member.remove'))
    await user.type(screen.getByLabelText('开始时间'), '2026-06-16T10:00')
    expect(requests).toBe(1)

    await user.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() => {
      expect(requests).toBe(2)
    })
  })

  it('loads the next page when the user clicks load more', async () => {
    let cursor = ''
    server.use(
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        cursor = new URL(request.url).searchParams.get('cursor') ?? ''
        return HttpResponse.json({
          items: cursor
            ? [
                {
                  id: '2',
                  actorType: 'admin',
                  actorId: 'user-1',
                  action: 'member.remove',
                  targetType: 'member',
                  targetId: 'member-2',
                  summary: 'Removed member',
                  createdAt: '2026-06-16T09:00:00Z',
                },
              ]
            : [
                {
                  id: '1',
                  actorType: 'admin',
                  actorId: 'user-1',
                  action: 'member.invite',
                  targetType: 'member',
                  targetId: 'member-1',
                  summary: 'Invited member',
                  createdAt: '2026-06-16T10:00:00Z',
                },
              ],
          nextCursor: cursor ? undefined : '1718539200000000000:42',
        })
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByText('member.invite')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '加载更多' }))

    await waitFor(() => {
      expect(screen.getByText('member.remove')).toBeInTheDocument()
    })
    expect(cursor).toBe('1718539200000000000:42')
  })

  it('reset clears the multi-action summary', async () => {
    server.use(http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })))

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '动作' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '动作' }))
    await user.click(screen.getByText('member.remove'))
    expect(screen.getByRole('button', { name: '动作' })).toHaveTextContent('已选择 1 个动作')

    await user.click(screen.getByRole('button', { name: '重置' }))

    expect(screen.getByRole('button', { name: '动作' })).toHaveTextContent('选择一个或多个动作')
  })

  it('shows request metadata inside details', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              actorType: 'admin',
              actorId: 'user-1',
              actorEmail: 'admin@example.com',
              actorIp: '127.0.0.1',
              actorUserAgent: 'playwright',
              action: 'member.invite',
              targetType: 'member',
              targetId: 'member-1',
              summary: 'Invited member',
              beforeJson: '{"role":"member"}',
              afterJson: '{"role":"admin"}',
              createdAt: '2026-06-16T10:00:00Z',
            },
          ],
        }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByText('member.invite')).toBeInTheDocument()
    })

    await user.click(screen.getAllByText('详情')[1])

    expect(screen.getByText('请求元数据')).toBeInTheDocument()
    expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    expect(screen.getByText('127.0.0.1')).toBeInTheDocument()
    expect(screen.getByText('playwright')).toBeInTheDocument()
  })
})
