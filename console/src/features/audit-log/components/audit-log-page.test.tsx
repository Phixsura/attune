import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AuditLogPage } from '@/features/audit-log/components/audit-log-page'
import type { AuditLogViewState, SavedAuditLogView } from '@/proto/attune/v1/audit'
import { expectNoA11yViolations } from '@/testing/a11y'
import { server } from '@/testing/mocks/server'
import { act, fireEvent, renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

const triggerBlobDownloadMock = vi.hoisted(() => vi.fn())
const canPermissionMock = vi.hoisted(() => vi.fn(() => true))

vi.mock('@/lib/blob-download', () => ({
  triggerBlobDownload: triggerBlobDownloadMock,
}))

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}))

vi.mock('@/features/session/hooks/use-permissions', () => ({
  usePermissions: () => ({
    can: canPermissionMock,
  }),
}))

afterEach(() => {
  triggerBlobDownloadMock.mockReset()
  canPermissionMock.mockReset()
  canPermissionMock.mockReturnValue(true)
  vi.mocked(toast.error).mockClear()
  vi.mocked(toast.success).mockClear()
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
  it('renders the permission empty state without audit-log actions', () => {
    canPermissionMock.mockReturnValue(false)

    renderWithProviders(<AuditLogPage />)

    expect(screen.getByText('暂无审计记录')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '导出 CSV' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存当前' })).not.toBeInTheDocument()
  })

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

  it('retries the audit log request from the error state', async () => {
    let attempts = 0
    server.use(
      http.get('/fb/v1/console/audit-log', () => {
        attempts += 1
        if (attempts === 1) {
          return HttpResponse.json({ message: 'temporary outage' }, { status: 503 })
        }
        return HttpResponse.json({
          items: [
            {
              id: '1',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'member.invite',
              targetType: 'member',
              targetId: 'member-1',
              summary: 'Invited member after retry',
              createdAt: '2026-06-16T10:00:00Z',
            },
          ],
        })
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByText('加载审计记录失败')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })
    expect(attempts).toBe(2)
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
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(urls).toHaveLength(1)
    })

    await expandFiltersIfCollapsed(user)
    await user.click(screen.getByRole('button', { name: '动作' }))
    const actionChoices = screen.getAllByText('移除成员')
    await user.click(actionChoices[actionChoices.length - 1] as HTMLElement)
    await user.type(screen.getByLabelText('目标类型'), 'member')
    await user.type(screen.getByLabelText('目标 ID'), 'member-42')
    await user.type(screen.getByLabelText('操作者 ID'), 'user-1')
    await user.type(screen.getByLabelText('开始时间'), '2026-06-16T10:00')
    await user.type(screen.getByLabelText('结束时间'), '2026-06-16T12:00')
    expect(urls).toHaveLength(1)

    await user.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() => {
      expect(urls).toHaveLength(2)
    })
    expect(urls[1]).toContain('action=member.remove')
    expect(urls[1]).toContain('targetId=member-42')
    expect(urls[1]).toContain('targetType=member')
    expect(urls[1]).toContain('actorId=user-1')
    expect(new URL(urls[1] as string).searchParams.get('to')).toMatch(/^2026-06-16T.*Z$/)
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
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
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

  it('clears a stale inspected entry from the URL state when results no longer contain it', async () => {
    window.history.replaceState({}, '', '/administration/audit-log?entry=missing-entry')
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
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
    )

    renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('syncs filters from browser history popstate', async () => {
    const urls: string[] = []
    server.use(
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({
          items: [
            {
              id: '2',
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
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(urls).toHaveLength(1)
    })

    window.history.replaceState(
      {},
      '',
      '/administration/audit-log?action=member.remove&targetId=member-42&q=playwright',
    )
    act(() => {
      window.dispatchEvent(new Event('popstate'))
    })

    await waitFor(() => {
      expect(urls).toHaveLength(2)
    })
    expect(urls[1]).toContain('action=member.remove')
    expect(urls[1]).toContain('targetId=member-42')
    await expandFiltersIfCollapsed(user)
    expect(screen.getByLabelText('目标 ID')).toHaveValue('member-42')
    expect(
      screen.getByPlaceholderText('在已加载记录里继续搜索动作、摘要、操作者、目标或快照内容'),
    ).toHaveValue('playwright')
  })

  it('applies a quick preset into the server-side audit filters', async () => {
    const urls: string[] = []
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

    await user.click(screen.getAllByRole('button', { name: '成员与权限' })[0] as HTMLElement)

    await waitFor(() => {
      expect(urls).toHaveLength(2)
    })
    const params = new URL(urls[1] as string).searchParams
    expect(params.getAll('action')).toEqual([
      'member.invite',
      'member.remove',
      'member.update_role',
    ])
    expect(screen.getByText('已应用 1 条筛选')).toBeInTheDocument()
  })

  it('applies a quick preset from the expanded filter console', async () => {
    const urls: string[] = []
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

    await expandFiltersIfCollapsed(user)
    await user.click(screen.getAllByRole('button', { name: /^成员与权限/ })[0] as HTMLElement)

    await waitFor(() => {
      expect(urls).toHaveLength(2)
    })
    expect(new URL(urls[1] as string).searchParams.getAll('action')).toEqual([
      'member.invite',
      'member.remove',
      'member.update_role',
    ])
  })

  it('saves the current investigation view into the saved views sidebar', async () => {
    let savedViews: SavedAuditLogView[] = []
    let postedBody: { name: string; state?: AuditLogViewState } | null = null
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'member.remove',
              targetType: 'member',
              targetId: 'member-42',
              summary: 'Removed member',
              createdAt: '2026-06-16T10:00:00Z',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: savedViews })),
      http.post('/fb/v1/console/audit-log/views', async ({ request }) => {
        const body = (await request.json()) as { name: string; state?: AuditLogViewState }
        postedBody = body
        savedViews = [
          {
            id: 'view-1',
            name: '成员删除排查',
            state: body.state,
            createdAt: '2026-06-16T10:00:00Z',
            updatedAt: '2026-06-16T10:00:00Z',
          },
        ]
        return HttpResponse.json({ view: savedViews[0] })
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '保存当前' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '保存当前' }))
    await user.clear(screen.getByLabelText('视图名称'))
    await user.type(screen.getByLabelText('视图名称'), '成员删除排查')
    await user.click(screen.getByRole('button', { name: '保存视图' }))

    await waitFor(() => {
      expect(postedBody).toEqual({
        name: '成员删除排查',
        state: {
          actions: [],
          actorId: '',
          actorType: '',
          from: '',
          localQuery: '',
          targetId: '',
          targetType: '',
          to: '',
        },
      })
    })

    await waitFor(() => {
      expect(screen.getByText('当前选中视图：成员删除排查')).toBeInTheDocument()
    })
  })

  it('surfaces save-as-new failures from the saved views card', async () => {
    let postAttempts = 0
    server.use(
      http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
      http.post('/fb/v1/console/audit-log/views', () => {
        postAttempts += 1
        return HttpResponse.json({ message: 'save denied' }, { status: 503 })
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '另存为' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '另存为' }))
    await user.clear(screen.getByLabelText('视图名称'))
    fireEvent.submit(screen.getByLabelText('视图名称').closest('form') as HTMLFormElement)
    expect(postAttempts).toBe(0)
    await user.type(screen.getByLabelText('视图名称'), '无法保存的视图')
    await user.click(screen.getByRole('button', { name: '保存视图' }))

    await waitFor(() => expect(postAttempts).toBe(1))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('save denied'))
  })

  it('updates and deletes a selected investigation view', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const calls: Array<{ body?: unknown; method: string; path: string }> = []
    let savedViews: SavedAuditLogView[] = [
      {
        id: 'view-1',
        name: '成员删除排查',
        state: {
          actions: ['member.remove'],
          actorId: '',
          actorType: '',
          from: '',
          localQuery: '',
          targetId: '',
          targetType: '',
          to: '',
        },
        createdAt: '2026-06-16T10:00:00Z',
        updatedAt: '2026-06-16T10:00:00Z',
      },
    ]
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'member.remove',
              targetType: 'member',
              targetId: 'member-42',
              summary: 'Removed member',
              createdAt: '2026-06-16T10:00:00Z',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: savedViews })),
      http.put('/fb/v1/console/audit-log/views/view-1', async ({ request }) => {
        const body = await request.json()
        calls.push({ body, method: 'PUT', path: new URL(request.url).pathname })
        savedViews = [
          {
            ...savedViews[0],
            name: (body as { name: string }).name,
            state: (body as { state: AuditLogViewState }).state,
            updatedAt: '2026-06-16T11:00:00Z',
          },
        ]
        return HttpResponse.json({ view: savedViews[0] })
      }),
      http.delete('/fb/v1/console/audit-log/views/view-1', ({ request }) => {
        calls.push({ method: 'DELETE', path: new URL(request.url).pathname })
        savedViews = []
        return HttpResponse.json({})
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /^成员删除排查/ })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: /^成员删除排查/ }))
    await waitFor(() => {
      expect(screen.getByText('当前选中视图：成员删除排查')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '保存当前' }))
    await user.clear(screen.getByLabelText('视图名称'))
    await user.type(screen.getByLabelText('视图名称'), '成员删除排查 v2')
    await user.click(screen.getByRole('button', { name: '更新视图' }))

    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('已更新视图 成员删除排查 v2'))
    expect(calls).toContainEqual({
      body: expect.objectContaining({ name: '成员删除排查 v2' }),
      method: 'PUT',
      path: '/fb/v1/console/audit-log/views/view-1',
    })

    await user.click(screen.getByRole('button', { name: '删除视图 成员删除排查 v2' }))

    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('已删除视图 成员删除排查 v2'))
    expect(confirmSpy).toHaveBeenCalledWith('确认删除“成员删除排查 v2”吗？')
    expect(calls).toContainEqual({
      method: 'DELETE',
      path: '/fb/v1/console/audit-log/views/view-1',
    })
  })

  it('does not apply saved views that have no stored state', async () => {
    const urls: string[] = []
    server.use(
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({ items: [] })
      }),
      http.get('/fb/v1/console/audit-log/views', () =>
        HttpResponse.json({
          items: [
            {
              id: 'view-empty',
              name: '空视图',
              createdAt: '2026-06-16T10:00:00Z',
              updatedAt: '2026-06-16T10:00:00Z',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /^空视图/ })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: /^空视图/ }))

    expect(urls).toHaveLength(1)
    expect(window.location.search).toBe('')
  })

  it('does not delete a saved view when confirmation is cancelled', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    let deleteCalled = false
    server.use(
      http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/audit-log/views', () =>
        HttpResponse.json({
          items: [
            {
              id: 'view-1',
              name: '成员删除排查',
              state: {
                actions: [],
                actorId: '',
                actorType: '',
                from: '',
                localQuery: '',
                targetId: '',
                targetType: '',
                to: '',
              },
              createdAt: '2026-06-16T10:00:00Z',
              updatedAt: '2026-06-16T10:00:00Z',
            },
          ],
        }),
      ),
      http.delete('/fb/v1/console/audit-log/views/view-1', () => {
        deleteCalled = true
        return HttpResponse.json({})
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    const deleteViewButton = await screen.findByRole(
      'button',
      { name: '删除视图 成员删除排查' },
      { timeout: 5000 },
    )

    await user.click(deleteViewButton)

    expect(confirmSpy).toHaveBeenCalledWith('确认删除“成员删除排查”吗？')
    expect(deleteCalled).toBe(false)
  })

  it('surfaces saved view save and delete failures', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'member.remove',
              targetType: 'member',
              targetId: 'member-42',
              summary: 'Removed member',
              createdAt: '2026-06-16T10:00:00Z',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/audit-log/views', () =>
        HttpResponse.json({
          items: [
            {
              id: 'view-1',
              name: '成员删除排查',
              state: {
                actions: [],
                actorId: '',
                actorType: '',
                from: '',
                localQuery: '',
                targetId: '',
                targetType: '',
                to: '',
              },
              createdAt: '2026-06-16T10:00:00Z',
              updatedAt: '2026-06-16T10:00:00Z',
            },
          ],
        }),
      ),
      http.post('/fb/v1/console/audit-log/views', () => HttpResponse.json({})),
      http.delete('/fb/v1/console/audit-log/views/view-1', () =>
        HttpResponse.json({ message: 'delete refused' }, { status: 409 }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '保存当前' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '保存当前' }))
    await user.clear(screen.getByLabelText('视图名称'))
    await user.type(screen.getByLabelText('视图名称'), '坏响应视图')
    await user.click(screen.getByRole('button', { name: '保存视图' }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('保存视图失败'))
    await user.click(screen.getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: '删除视图 成员删除排查' }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('delete refused'))
    expect(confirmSpy).toHaveBeenCalledWith('确认删除“成员删除排查”吗？')
  })

  it('applies a saved investigation view from the sidebar', async () => {
    const urls: string[] = []
    server.use(
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        urls.push(request.url)
        const searchParams = new URL(request.url).searchParams
        if (searchParams.get('action') === 'member.remove') {
          return HttpResponse.json({
            items: [
              {
                id: '2',
                actorType: 'admin',
                actorId: 'user-1',
                action: 'member.remove',
                targetType: 'member',
                targetId: 'member-42',
                summary: 'playwright removed member',
                createdAt: '2026-06-16T09:30:00Z',
              },
            ],
          })
        }
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
      http.get('/fb/v1/console/audit-log/views', () =>
        HttpResponse.json({
          items: [
            {
              id: 'view-1',
              name: '成员删除排查',
              state: {
                actions: ['member.remove'],
                actorType: '',
                actorId: 'user-1',
                targetType: 'member',
                targetId: 'member-42',
                from: '',
                to: '',
                localQuery: 'playwright',
                inspectedEntryId: '2',
              },
              createdAt: '2026-06-16T10:00:00Z',
              updatedAt: '2026-06-16T10:00:00Z',
            },
          ],
        }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(urls).toHaveLength(1)
    })

    await user.click(screen.getByRole('button', { name: /^成员删除排查/ }))

    await waitFor(() => {
      expect(urls).toHaveLength(2)
    })
    expect(urls[1]).toContain('action=member.remove')
    expect(urls[1]).toContain('targetId=member-42')
    expect(urls[1]).toContain('actorId=user-1')
    expect(urls[1]).toContain('targetType=member')
    await waitFor(() => {
      expect(window.location.search).toContain('q=playwright')
    })
    expect(window.location.search).toContain('entry=2')

    await waitFor(() => {
      expect(screen.getByText('当前选中视图：成员删除排查')).toBeInTheDocument()
    })
    expect(screen.getByText('当前状态与保存视图一致。')).toBeInTheDocument()
  })

  it('reset clears the multi-action summary', async () => {
    server.use(http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })))

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '展开筛选' })).toBeInTheDocument()
    })

    await expandFiltersIfCollapsed(user)
    await user.click(screen.getByRole('button', { name: '动作' }))
    await user.type(screen.getByPlaceholderText('搜索动作名'), 'remove')
    await user.click(screen.getByText('移除成员'))
    expect(screen.getByRole('button', { name: '动作' })).toHaveTextContent('已选择 1 个动作')

    await user.click(screen.getByText('移除成员'))
    expect(screen.getByRole('button', { name: '动作' })).toHaveTextContent('选择一个或多个动作')

    await user.click(screen.getByText('移除成员'))
    expect(screen.getByRole('button', { name: '动作' })).toHaveTextContent('已选择 1 个动作')

    await user.click(screen.getByRole('button', { name: '清空动作' }))
    expect(screen.getByRole('button', { name: '动作' })).toHaveTextContent('选择一个或多个动作')

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

    const openDetailsButton = screen.getAllByRole('button', { name: '查看详情' })[0] as HTMLElement
    openDetailsButton.focus()
    await user.keyboard('[Enter]')

    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('请求元数据')).toBeInTheDocument()
    expect(screen.getAllByText('admin@example.com')).not.toHaveLength(0)
    expect(screen.getByText('127.0.0.1')).toBeInTheDocument()
    expect(screen.getByText('playwright')).toBeInTheDocument()
    expect(screen.getAllByText('变更摘要')).not.toHaveLength(0)
    expect(screen.getAllByText('角色')).not.toHaveLength(0)
    await expectNoA11yViolations(document.body)

    await user.keyboard('[Escape]')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(openDetailsButton).toHaveFocus()
  })

  it('uses details actions to copy identifiers and narrow the investigation', async () => {
    const urls: string[] = []
    const writeSpy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)
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
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await user.click(screen.getByRole('button', { name: '更多操作' }))
    await user.click(await screen.findByRole('menuitem', { name: '复制目标 ID' }))
    await waitFor(() => {
      expect(writeSpy).toHaveBeenCalledWith('member-1')
    })
    await user.click(screen.getByRole('button', { name: '更多操作' }))
    await user.click(await screen.findByRole('menuitem', { name: '复制操作者 ID' }))
    await waitFor(() => {
      expect(writeSpy).toHaveBeenCalledWith('user-1')
    })

    const detailButtons = screen.getAllByRole('button', { name: '查看详情' })
    await user.click(detailButtons[detailButtons.length - 1] as HTMLElement)

    await user.click(screen.getByRole('button', { name: '复制操作者 ID' }))
    await user.click(screen.getByRole('button', { name: '复制目标 ID' }))

    await waitFor(() => {
      expect(writeSpy).toHaveBeenCalledWith('user-1')
    })
    expect(writeSpy).toHaveBeenCalledWith('member-1')

    const entityCopyButtons = screen.getAllByRole('button', { name: '复制' })
    await user.click(entityCopyButtons[0] as HTMLElement)
    await user.click(entityCopyButtons[1] as HTMLElement)
    expect(writeSpy).toHaveBeenCalledWith('user-1')
    expect(writeSpy).toHaveBeenCalledWith('member-1')

    await user.click(screen.getByRole('button', { name: '只看这个动作' }))
    await waitFor(() => {
      expect(urls.at(-1)).toContain('action=member.invite')
    })

    await user.click(screen.getByRole('button', { name: '只看这个目标' }))
    await waitFor(() => {
      expect(urls.at(-1)).toContain('targetId=member-1')
    })

    await user.click(screen.getByRole('button', { name: '只看这个操作者' }))
    await waitFor(() => {
      expect(urls.at(-1)).toContain('actorId=user-1')
    })
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

    await user.click(screen.getAllByRole('button', { name: '聚焦这段最新事件' })[0] as HTMLElement)
    expect(screen.getByText('1/4')).toBeInTheDocument()

    await user.click(
      screen.getAllByRole('button', { name: '聚焦字段：prompt_template' })[0] as HTMLElement,
    )
    await waitFor(() => {
      expect(window.location.search).toContain('q=prompt_template')
    })

    await user.click(screen.getAllByRole('button', { name: '沿这段目标继续' })[0] as HTMLElement)
    await waitFor(() => {
      expect(urls).toHaveLength(2)
    })
    expect(urls.at(-1)).toContain('targetId=test-tenant')

    await user.click(screen.getAllByRole('button', { name: '沿这段动作继续' })[0] as HTMLElement)

    await waitFor(() => {
      expect(urls).toHaveLength(3)
    })
    expect(urls.at(-1)).toContain('action=enrich_config.update')
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

    await user.click(
      screen.getAllByRole('button', { name: '聚焦事件：更新富化配置' })[1] as HTMLElement,
    )
    expect(screen.getByText('2/5')).toBeInTheDocument()

    await user.click(screen.getAllByRole('button', { name: '查看详情' })[1] as HTMLElement)
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
    await user.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

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

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }))
    expect(screen.getByText('第 1 / 2 条')).toBeInTheDocument()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    await waitFor(() => {
      expect(window.location.search).toContain('entry=2')
    })
    expect(screen.getByText('第 2 / 2 条')).toBeInTheDocument()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    expect(screen.getByText('第 2 / 2 条')).toBeInTheDocument()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }))
    await waitFor(() => {
      expect(window.location.search).toContain('entry=1')
    })
    expect(screen.getByText('第 1 / 2 条')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '下一条' }))

    await waitFor(() => {
      expect(window.location.search).toContain('entry=2')
    })
    expect(screen.getByText('第 2 / 2 条')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '上一条' }))

    await waitFor(() => {
      expect(window.location.search).toContain('entry=1')
    })
    expect(screen.getByText('第 1 / 2 条')).toBeInTheDocument()
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

  it('lets the operator remove every applied server-side filter chip', async () => {
    const urls: string[] = []
    window.history.replaceState(
      {},
      '',
      '/administration/audit-log?action=member.invite&actorId=user-1&targetType=member&targetId=member-1&from=2026-06-16T08%3A00&to=2026-06-16T12%3A00',
    )
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
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    await user.click(screen.getByRole('button', { name: '移除筛选：已选择 1 个动作' }))
    await user.click(screen.getByRole('button', { name: '移除筛选：目标类型: member' }))
    await user.click(screen.getByRole('button', { name: '移除筛选：目标 ID: member-1' }))
    await user.click(screen.getByRole('button', { name: '移除筛选：操作者 ID: user-1' }))
    await user.click(screen.getByRole('button', { name: /移除筛选：开始时间:/ }))
    await user.click(screen.getByRole('button', { name: /移除筛选：结束时间:/ }))

    await waitFor(() => {
      expect(new URL(urls.at(-1) as string).searchParams.toString()).toBe('limit=50')
    })
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
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
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

  it('opens the evidence export dialog from the header action', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '导出证据包' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '导出证据包' }))

    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('合规证据包导出')).toBeInTheDocument()
  })

  it('exports audit logs using the current server-side filters', async () => {
    let exportUrl = ''
    window.history.replaceState(
      {},
      '',
      '/administration/audit-log?action=member.remove&targetId=member-42',
    )
    server.use(
      http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/audit-log/export.csv', ({ request }) => {
        exportUrl = request.url
        return HttpResponse.text('id,action\n1,member.remove\n', {
          headers: {
            'Content-Disposition': 'attachment; filename="audit-member.csv"',
            'Content-Type': 'text/csv',
          },
        })
      }),
    )

    const { user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '导出 CSV' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '导出 CSV' }))

    await waitFor(() => expect(triggerBlobDownloadMock).toHaveBeenCalledTimes(1))
    expect(exportUrl).toContain('action=member.remove')
    expect(exportUrl).toContain('targetId=member-42')
    const [, filename] = triggerBlobDownloadMock.mock.calls[0] ?? []
    expect(filename).toBe('audit-member.csv')
  })

  it('surfaces export and copy failures from header actions', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/audit-log/export.csv', () =>
        HttpResponse.json({ message: 'export denied' }, { status: 403 }),
      ),
    )

    const { user } = renderWithProviders(<AuditLogPage />)
    const writeSpy = vi
      .spyOn(navigator.clipboard, 'writeText')
      .mockRejectedValue(new Error('clipboard denied'))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '导出 CSV' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '导出 CSV' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('export denied'))

    await user.click(screen.getByRole('button', { name: '复制当前视角' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('复制失败'))
    expect(writeSpy).toHaveBeenCalledWith('/administration/audit-log')
  })

  it('copies the current investigation URL from the header', async () => {
    window.history.replaceState({}, '', '/administration/audit-log?targetId=member-42')
    server.use(
      http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
    )

    const { user } = renderWithProviders(<AuditLogPage />)
    const writeSpy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '复制当前视角' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '复制当前视角' }))

    await waitFor(() =>
      expect(writeSpy).toHaveBeenCalledWith('/administration/audit-log?targetId=member-42'),
    )
    expect(toast.success).toHaveBeenCalledWith('当前排查视角已复制')
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

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'j', ctrlKey: true, bubbles: true }))
    expect(screen.getByText('2/2')).toBeInTheDocument()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', bubbles: true }))

    await waitFor(() => {
      expect(screen.getByText('1/2')).toBeInTheDocument()
    })
  })

  it('ignores keyboard navigation when no audit event is selectable', async () => {
    server.use(http.get('/fb/v1/console/audit-log', () => HttpResponse.json({ items: [] })))

    renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getByText('暂无审计记录')).toBeInTheDocument()
    })

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'j', bubbles: true }))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', bubbles: true }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
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

    const { container, user } = renderWithProviders(<AuditLogPage />)

    await waitFor(() => {
      expect(screen.getAllByText('邀请成员').length).toBeGreaterThan(0)
    })

    document.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))

    const searchInput = screen.getByPlaceholderText(
      '在已加载记录里继续搜索动作、摘要、操作者、目标或快照内容',
    )
    expect(searchInput).toHaveFocus()
    expect(searchInput).toHaveAttribute('aria-keyshortcuts', '/')

    await user.type(searchInput, 'playwright')
    expect(searchInput).toHaveValue('playwright')
    fireEvent.click(screen.getAllByRole('button', { name: '清除本地检索' })[0] as HTMLElement)
    await waitFor(() => {
      expect(searchInput).toHaveValue('')
    })
    expect(searchInput).not.toHaveFocus()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
    await user.type(searchInput, 'playwright')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await waitFor(() => {
      expect(searchInput).toHaveValue('')
    })
    expect(searchInput).not.toHaveFocus()
    await expectNoA11yViolations(container)
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

  it('keeps keyboard shortcuts inert while typing in the local search box', async () => {
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

    const searchInput = screen.getByPlaceholderText(
      '在已加载记录里继续搜索动作、摘要、操作者、目标或快照内容',
    )
    searchInput.focus()
    searchInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    searchInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'j', bubbles: true }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByText('1/1')).toBeInTheDocument()
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
      http.get('/fb/v1/console/audit-log/views', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/audit-log/export.csv', () =>
        HttpResponse.text('id,action\n1,member.invite\n', {
          headers: {
            'Content-Disposition': 'attachment; filename="audit-detail.csv"',
            'Content-Type': 'text/csv',
          },
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
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '导出 CSV' }))
    await waitFor(() => expect(triggerBlobDownloadMock).toHaveBeenCalledTimes(1))

    triggerBlobDownloadMock.mockClear()

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
