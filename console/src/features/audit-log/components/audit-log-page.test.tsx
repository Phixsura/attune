import { HttpResponse, http } from 'msw'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AuditLogPage } from '@/features/audit-log/components/audit-log-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('@/features/session/hooks/use-permissions', () => ({
  usePermissions: () => ({
    can: () => true,
  }),
}))

afterEach(() => {
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/administration/audit-log')
})

async function expandFiltersIfCollapsed(user: { click: (element: Element) => Promise<void> }) {
  const trigger = screen.queryByRole('button', { name: '展开筛选' })
  if (trigger) {
    await user.click(trigger)
  }
}

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
    const urls: string[] = []
    server.use(
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({
          items: [
            {
              id: '1',
              actorType: 'admin',
              actorId: 'user-1',
              actorUserAgent: 'playwright',
              action: 'member.remove',
              targetType: 'member',
              targetId: 'member-42',
              summary: 'Removed member',
              createdAt: '2026-06-16T10:00:00Z',
            },
          ],
        })
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(urls).toHaveLength(1)
    })

    await expandFiltersIfCollapsed(user)
    await user.click(screen.getByRole('button', { name: '动作' }))
    const actionChoices = screen.getAllByText('移除成员')
    await user.click(actionChoices[actionChoices.length - 1] as HTMLElement)
    await user.type(screen.getByLabelText('目标 ID'), 'member-42')
    await user.type(screen.getByLabelText('开始时间'), '2026-06-16T10:00')
    expect(urls).toHaveLength(1)

    await user.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() => {
      expect(urls).toHaveLength(2)
    })
    expect(urls[1]).toContain('action=member.remove')
    expect(urls[1]).toContain('targetId=member-42')
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
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await user.click(screen.getByRole('button', { name: '加载更多' }))

    await waitFor(() => {
      expect(screen.getAllByText('移除成员').length).toBeGreaterThan(0)
    })
    expect(cursor).toBe('1718539200000000000:42')
  })

  it('hydrates the current investigation view from the URL', async () => {
    const urls: string[] = []
    window.history.replaceState(
      {},
      '',
      '/administration/audit-log?action=member.remove&targetId=member-42&q=playwright',
    )

    server.use(
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({ items: [] })
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(urls).toHaveLength(1)
    })

    expect(urls[0]).toContain('action=member.remove')
    expect(urls[0]).toContain('targetId=member-42')
    await expandFiltersIfCollapsed(user)
    expect(screen.getByLabelText('目标 ID')).toHaveValue('member-42')
    expect(
      screen.getByPlaceholderText('在已加载记录里继续搜索动作、摘要、操作者、目标或快照内容'),
    ).toHaveValue('playwright')
  })

  it('reset clears the multi-action summary', async () => {
    server.use(http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })))

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '展开筛选' })).toBeInTheDocument()
    })

    await expandFiltersIfCollapsed(user)
    await user.click(screen.getByRole('button', { name: '动作' }))
    await user.click(screen.getByText('移除成员'))
    expect(screen.getByRole('button', { name: '动作' })).toHaveTextContent('已选择 1 个动作')

    await user.click(screen.getByRole('button', { name: '重置' }))

    expect(screen.getByRole('button', { name: '动作' })).toHaveTextContent('选择一个或多个动作')
  })

  it('opens the details drawer with metadata and change summary', async () => {
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
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await user.click(screen.getAllByRole('button', { name: '查看详情' })[0])

    expect(screen.getByText('请求元数据')).toBeInTheDocument()
    expect(screen.getAllByText('admin@example.com')).not.toHaveLength(0)
    expect(screen.getByText('127.0.0.1')).toBeInTheDocument()
    expect(screen.getByText('playwright')).toBeInTheDocument()
    expect(screen.getAllByText('变更摘要')).not.toHaveLength(0)
    expect(screen.getAllByText('角色')).not.toHaveLength(0)
  })

  it('preserves the current scope when opening the details drawer', async () => {
    const pushStateSpy = vi.spyOn(window.history, 'pushState')
    window.history.replaceState({}, '', '/administration/audit-log?targetId=member-1')

    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
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
        }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await user.click(screen.getAllByRole('button', { name: '查看详情' })[0] as HTMLElement)

    await waitFor(() => {
      expect(window.location.search).toContain('entry=1')
    })
    expect(window.location.search).toContain('targetId=member-1')
    expect(pushStateSpy).toHaveBeenCalledWith({}, '', expect.stringContaining('entry=1'))
  })

  it('lets the operator focus a different event directly from the stream', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
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
            {
              id: '2',
              actorType: 'admin',
              actorId: 'user-2',
              action: 'member.remove',
              targetType: 'member',
              targetId: 'member-2',
              summary: 'Removed member',
              createdAt: '2026-06-16T09:00:00Z',
            },
          ],
        }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await user.click(screen.getByRole('button', { name: '聚焦事件：移除成员' }))

    expect(screen.getByText('2/2')).toBeInTheDocument()
  })

  it('keeps the advanced filter console collapsed until the operator expands it', async () => {
    server.use(http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })))

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '展开筛选' })).toBeInTheDocument()
    })

    expect(
      screen.queryByPlaceholderText('例如：member / api_key / workflow'),
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '展开筛选' }))

    expect(screen.getByRole('button', { name: '收起筛选' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('例如：member / api_key / workflow')).toBeInTheDocument()
  })

  it('groups adjacent repeated activity into a burst summary', async () => {
    const urls: string[] = []
    server.use(
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({
          items: [
            {
              id: '1',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'enrich_config.update',
              targetType: 'tenant',
              targetId: 'test-tenant',
              summary: 'Updated enrich config',
              beforeJson: '{"prompt_template":"v1"}',
              afterJson: '{"prompt_template":"v2"}',
              createdAt: '2026-06-16T10:00:00Z',
            },
            {
              id: '2',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'enrich_config.update',
              targetType: 'tenant',
              targetId: 'test-tenant',
              summary: 'Updated enrich config again',
              beforeJson: '{"prompt_template":"v2"}',
              afterJson: '{"prompt_template":"v3"}',
              createdAt: '2026-06-16T09:30:00Z',
            },
            {
              id: '3',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'enrich_config.update',
              targetType: 'tenant',
              targetId: 'test-tenant',
              summary: 'Updated enrich config once more',
              beforeJson: '{"prompt_template":"v3"}',
              afterJson: '{"prompt_template":"v4"}',
              createdAt: '2026-06-16T09:00:00Z',
            },
            {
              id: '4',
              actorType: 'admin',
              actorId: 'user-2',
              action: 'member.invite',
              targetType: 'member',
              targetId: 'member-2',
              summary: 'Invited member',
              createdAt: '2026-06-16T08:00:00Z',
            },
          ],
        })
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByText('连续 3 条相近事件')).toBeInTheDocument()
    })

    expect(screen.getByText('这一段里反复变化的字段')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '聚焦字段：prompt_template' })).not.toHaveLength(0)
    expect(screen.getAllByRole('button', { name: '更多操作' })).toHaveLength(2)

    await user.click(screen.getAllByRole('button', { name: '沿这段动作继续' })[0] as HTMLElement)

    await waitFor(() => {
      expect(urls).toHaveLength(2)
    })
    expect(urls[1]).toContain('action=enrich_config.update')
    expect(screen.queryByRole('button', { name: '沿这段动作继续' })).not.toBeInTheDocument()
  })

  it('collapses long repeated bursts until the operator expands them', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'enrich_config.update',
              targetType: 'tenant',
              targetId: 'test-tenant',
              summary: 'Updated config 1',
              createdAt: '2026-06-16T10:00:00Z',
            },
            {
              id: '2',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'enrich_config.update',
              targetType: 'tenant',
              targetId: 'test-tenant',
              summary: 'Updated config 2',
              createdAt: '2026-06-16T09:50:00Z',
            },
            {
              id: '3',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'enrich_config.update',
              targetType: 'tenant',
              targetId: 'test-tenant',
              summary: 'Updated config 3',
              createdAt: '2026-06-16T09:40:00Z',
            },
            {
              id: '4',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'enrich_config.update',
              targetType: 'tenant',
              targetId: 'test-tenant',
              summary: 'Updated config 4',
              createdAt: '2026-06-16T09:30:00Z',
            },
            {
              id: '5',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'enrich_config.update',
              targetType: 'tenant',
              targetId: 'test-tenant',
              summary: 'Updated config 5',
              createdAt: '2026-06-16T09:20:00Z',
            },
          ],
        }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByText('连续 5 条相近事件')).toBeInTheDocument()
    })

    expect(screen.getByText(/默认收起后续 2 条记录/)).toBeInTheDocument()
    const collapsedCount = screen.getAllByText('更新富化配置').length

    await user.click(screen.getAllByRole('button', { name: '展开这段 5 条事件' })[0] as HTMLElement)

    expect(screen.getByText(/这一段的 5 条事件已经全部展开/)).toBeInTheDocument()
    const expandedCount = screen.getAllByText('更新富化配置').length
    expect(expandedCount).toBeGreaterThan(collapsedCount)

    await user.click(screen.getByRole('button', { name: '收起这段事件' }))

    expect(screen.getAllByText('更新富化配置').length).toBeLessThan(expandedCount)
  })

  it('lets the operator move through adjacent events inside the details drawer', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
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
          ],
        }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await user.click(screen.getAllByRole('button', { name: '查看详情' })[0] as HTMLElement)

    expect(screen.getByText('第 1 / 2 条')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '下一条' }))

    await waitFor(() => {
      expect(window.location.search).toContain('entry=2')
    })
    expect(screen.getByText('第 2 / 2 条')).toBeInTheDocument()
  })

  it('supports inline current-focus browsing from the summary bar', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
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
            {
              id: '2',
              actorType: 'admin',
              actorId: 'user-2',
              action: 'member.remove',
              targetType: 'member',
              targetId: 'member-2',
              summary: 'Removed member',
              createdAt: '2026-06-16T09:00:00Z',
            },
            {
              id: '3',
              actorType: 'admin',
              actorId: 'user-3',
              action: 'member.update_role',
              targetType: 'member',
              targetId: 'member-3',
              summary: 'Updated member role',
              createdAt: '2026-06-16T08:00:00Z',
            },
          ],
        }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await user.click(screen.getByRole('button', { name: '下一条' }))

    await waitFor(() => {
      expect(screen.getByText('2/3')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '上一条' }))

    expect(screen.getByText('1/3')).toBeInTheDocument()
  })

  it('lets the operator clear the local query from the active scope chips', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              actorType: 'admin',
              actorId: 'user-1',
              actorUserAgent: 'playwright',
              action: 'member.invite',
              targetType: 'member',
              targetId: 'member-1',
              summary: 'Invited member',
              createdAt: '2026-06-16T10:00:00Z',
            },
          ],
        }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    const localSearch = screen.getByPlaceholderText(
      '在已加载记录里继续搜索动作、摘要、操作者、目标或快照内容',
    )
    await user.type(localSearch, 'playwright')

    expect(screen.getAllByText('本地检索: playwright')).not.toHaveLength(0)

    await user.click(screen.getByRole('button', { name: '移除筛选：本地检索: playwright' }))

    expect(localSearch).toHaveValue('')
  })

  it('shows active filter count chip in collapsed view after applying filters', async () => {
    const urls: string[] = []
    server.use(
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({
          items: [
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
        })
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(urls).toHaveLength(1)
    })

    await expandFiltersIfCollapsed(user)
    await user.type(screen.getByLabelText('目标 ID'), 'member-1')
    await user.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() => {
      expect(urls).toHaveLength(2)
    })

    await user.click(screen.getByRole('button', { name: '收起筛选' }))

    expect(screen.getByText('已应用 1 条筛选')).toBeInTheDocument()
  })

  it('hides filter count chip when no filters are active in collapsed view', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
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
        }),
      ),
    )

    renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '展开筛选' })).toBeInTheDocument()
    })

    expect(screen.queryByText(/已应用.*条筛选/)).not.toBeInTheDocument()
    expect(screen.queryByText('未应用服务器筛选')).not.toBeInTheDocument()
  })

  it('renders compact page header with title and action buttons', async () => {
    server.use(http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })))

    renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '审计日志' })).toBeInTheDocument()
    })

    expect(screen.getByRole('button', { name: /导出 CSV/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /导出证据包/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /复制当前视角/ })).toBeInTheDocument()
  })

  it('shows empty state when no audit records exist', async () => {
    server.use(http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })))

    renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByText('暂无审计记录')).toBeInTheDocument()
    })
  })

  it('navigates with keyboard shortcuts', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
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
            {
              id: '2',
              actorType: 'admin',
              actorId: 'user-2',
              action: 'member.remove',
              targetType: 'member',
              targetId: 'member-2',
              summary: 'Removed member',
              createdAt: '2026-06-16T09:00:00Z',
            },
          ],
        }),
      ),
    )

    renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await waitFor(() => {
      expect(screen.getByText('1/2')).toBeInTheDocument()
    })

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'j', bubbles: true }))

    await waitFor(() => {
      expect(screen.getByText('2/2')).toBeInTheDocument()
    })

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', bubbles: true }))

    await waitFor(() => {
      expect(screen.getByText('1/2')).toBeInTheDocument()
    })
  })

  it('focuses search input with / shortcut', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
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
        }),
      ),
    )

    renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    document.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))

    const searchInput = screen.getByPlaceholderText(
      '在已加载记录里继续搜索动作、摘要、操作者、目标或快照内容',
    )
    expect(searchInput).toHaveFocus()
  })

  it('opens details drawer with enter key', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
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
        }),
      ),
    )

    renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))

    await waitFor(() => {
      expect(window.location.search).toContain('entry=1')
    })
  })

  it('closes details drawer with escape key', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
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
        }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await user.click(screen.getAllByRole('button', { name: '查看详情' })[0] as HTMLElement)

    await waitFor(() => {
      expect(window.location.search).toContain('entry=1')
    })

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))

    await waitFor(() => {
      expect(window.location.search).not.toContain('entry=1')
    })
  })

  it('shows a local-search empty state without losing the loaded results', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
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
        }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await user.type(
      screen.getByPlaceholderText('在已加载记录里继续搜索动作、摘要、操作者、目标或快照内容'),
      'no-match',
    )

    await waitFor(() => {
      expect(screen.getByText('已加载记录里没有匹配项')).toBeInTheDocument()
    })
    expect(
      screen.getByText('服务器结果已经返回，但当前本地检索词把它们全部过滤掉了。'),
    ).toBeInTheDocument()
  })
})
