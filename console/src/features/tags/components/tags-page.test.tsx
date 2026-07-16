import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { TagsPage } from '@/features/tags/components/tags-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

beforeEach(() => {
  vi.mocked(toast.success).mockClear()
  vi.mocked(toast.error).mockClear()
})

describe('TagsPage', () => {
  it('renders hero metrics and registry card from the tags query', async () => {
    server.use(
      http.get('/fb/v1/console/tags', () =>
        HttpResponse.json({
          tags: [
            {
              id: 'tag-1',
              name: 'Bug',
              color: '#ef4444',
              description: 'Bug report',
              exclusiveScope: 'issue_type',
              usageCount: '3',
              archived: false,
            },
          ],
        }),
      ),
    )

    renderWithProviders(<TagsPage />)

    await waitFor(() => {
      expect(
        screen.getByText('统一查看 1 个标签的用途、互斥范围和实际使用情况。'),
      ).toBeInTheDocument()
    })
    expect(screen.getByRole('heading', { name: '标签管理' })).toBeInTheDocument()
    expect(screen.getByText('标签总数')).toBeInTheDocument()
    expect(screen.getByText('已在使用')).toBeInTheDocument()
    expect(screen.getByText('标签目录')).toBeInTheDocument()
    expect(screen.getByText('标签治理建议')).toBeInTheDocument()
    expect(screen.getAllByText('Bug').length).toBeGreaterThanOrEqual(1)
  })

  it('creates a tag from the empty state dialog', async () => {
    let createdBody: unknown
    server.use(
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.post('/fb/v1/console/tags', async ({ request }) => {
        createdBody = await request.json()
        return HttpResponse.json({
          id: 'tag-new',
          name: 'Escalation',
          color: '#22c55e',
          description: 'Needs operator follow-up',
          exclusiveScope: 'priority',
          usageCount: '0',
        })
      }),
    )

    const { user } = renderWithProviders(<TagsPage />)

    expect(await screen.findByText('还没有标签')).toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: '新建标签' })[0])
    const dialog = await screen.findByRole('dialog', { name: '新建标签' })
    await user.type(within(dialog).getByLabelText('名称'), '  Escalation  ')
    await user.click(within(dialog).getByRole('button', { name: '#22c55e' }))
    await user.type(within(dialog).getByLabelText('描述'), '  Needs operator follow-up  ')
    await user.type(within(dialog).getByLabelText('互斥组'), '  priority  ')
    await user.click(within(dialog).getByRole('button', { name: '新建' }))

    await waitFor(() =>
      expect(createdBody).toMatchObject({
        name: 'Escalation',
        color: '#22c55e',
        description: 'Needs operator follow-up',
        exclusiveScope: 'priority',
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('标签已创建')
  })

  it('edits and archives an existing tag', async () => {
    const calls: Array<{ method: string; body?: unknown }> = []
    server.use(
      http.get('/fb/v1/console/tags', () =>
        HttpResponse.json({
          tags: [
            {
              id: 'tag-1',
              name: 'Bug',
              color: '#ef4444',
              description: '',
              exclusiveScope: '',
              usageCount: '0',
              archived: false,
            },
          ],
        }),
      ),
      http.patch('/fb/v1/console/tags/tag-1', async ({ request }) => {
        calls.push({ method: 'PATCH', body: await request.json() })
        return HttpResponse.json({
          id: 'tag-1',
          name: 'Bug report',
          color: '#3b82f6',
          description: 'Customer-visible defects',
          exclusiveScope: 'issue_type',
          usageCount: '0',
        })
      }),
      http.delete('/fb/v1/console/tags/tag-1', () => {
        calls.push({ method: 'DELETE' })
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const { user } = renderWithProviders(<TagsPage />)

    expect((await screen.findAllByText('Bug')).length).toBeGreaterThan(0)
    await user.click(screen.getByRole('button', { name: '编辑' }))
    const editDialog = await screen.findByRole('dialog', { name: '编辑标签' })
    await user.clear(within(editDialog).getByLabelText('名称'))
    await user.type(within(editDialog).getByLabelText('名称'), 'Bug report')
    await user.click(within(editDialog).getByRole('button', { name: '#3b82f6' }))
    await user.type(within(editDialog).getByLabelText('描述'), 'Customer-visible defects')
    await user.type(within(editDialog).getByLabelText('互斥组'), 'issue_type')
    await user.click(within(editDialog).getByRole('button', { name: '保存' }))

    await waitFor(() =>
      expect(calls).toContainEqual({
        method: 'PATCH',
        body: {
          id: 'tag-1',
          name: 'Bug report',
          color: '#3b82f6',
          description: 'Customer-visible defects',
          exclusiveScope: 'issue_type',
        },
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('已保存')

    await user.click(screen.getByRole('button', { name: '归档' }))
    const archiveDialog = await screen.findByRole('dialog', { name: '归档' })
    expect(within(archiveDialog).getByText(/确定要归档标签/)).toBeInTheDocument()
    await user.click(within(archiveDialog).getByRole('button', { name: '确认' }))

    await waitFor(() => expect(calls).toContainEqual({ method: 'DELETE' }))
    expect(toast.success).toHaveBeenCalledWith('标签已归档')
  })

  it('surfaces create errors from the dialog', async () => {
    server.use(
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.post('/fb/v1/console/tags', () =>
        HttpResponse.json({ message: 'tag name already exists' }, { status: 409 }),
      ),
    )

    const { user } = renderWithProviders(<TagsPage />)

    await screen.findByText('还没有标签')
    await user.click(screen.getAllByRole('button', { name: '新建标签' })[0])
    const dialog = await screen.findByRole('dialog', { name: '新建标签' })
    await user.type(within(dialog).getByLabelText('名称'), 'Bug')
    await user.click(within(dialog).getByRole('button', { name: '新建' }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('tag name already exists'))
  })
})
