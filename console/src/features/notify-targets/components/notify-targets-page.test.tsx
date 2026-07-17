import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { NotifyTargetsPage } from '@/features/notify-targets/components/notify-targets-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

beforeEach(() => {
  vi.mocked(toast.success).mockClear()
  vi.mocked(toast.error).mockClear()
})

describe('NotifyTargetsPage', () => {
  it('renders hero metrics, governance card, and target rows', async () => {
    server.use(
      http.get('/fb/v1/console/notify-targets', () =>
        HttpResponse.json({
          items: [
            {
              id: 'nt-1',
              destinationType: 'raw-webhook',
              url: 'https://example.com/hook',
              timeoutSeconds: 10,
              disabled: false,
              lastFailureAt: '',
              lastError: '',
            },
            {
              id: 'nt-2',
              destinationType: 'raw-webhook',
              url: 'https://example.com/failing',
              timeoutSeconds: 15,
              disabled: true,
              lastFailureAt: '2026-06-21T00:00:00Z',
              lastError: 'timeout',
            },
          ],
        }),
      ),
    )

    renderWithProviders(<NotifyTargetsPage />)

    await waitFor(() => {
      expect(screen.getByText('https://example.com/hook')).toBeInTheDocument()
    })
    expect(screen.getByText('目标总数')).toBeInTheDocument()
    expect(screen.getByText('投递治理建议')).toBeInTheDocument()
    expect(screen.getByText('https://example.com/failing')).toBeInTheDocument()
  })

  it('creates, tests, edits, and deletes targets through page wiring', async () => {
    const calls: Array<{ type: string; body?: unknown }> = []
    server.use(
      http.get('/fb/v1/console/notify-targets', () =>
        HttpResponse.json({
          items: [
            {
              id: 'nt-1',
              destinationType: 'raw-webhook',
              audience: 'all',
              url: 'https://example.com/hook',
              timeoutSeconds: 10,
              disabled: false,
              lastFailureAt: '',
              lastError: '',
            },
          ],
        }),
      ),
      http.post('/fb/v1/console/notify-targets', async ({ request }) => {
        calls.push({ type: 'create', body: await request.json() })
        return HttpResponse.json({ id: 'nt-2', url: 'https://example.com/new' }, { status: 201 })
      }),
      http.post('/fb/v1/console/notify-targets/nt-1/test', () => {
        calls.push({ type: 'test' })
        return HttpResponse.json({ ok: true, statusCode: 202, latencyMs: 12 })
      }),
      http.patch('/fb/v1/console/notify-targets/nt-1', async ({ request }) => {
        calls.push({ type: 'patch', body: await request.json() })
        return HttpResponse.json({ id: 'nt-1', url: 'https://example.com/updated' })
      }),
      http.delete('/fb/v1/console/notify-targets/nt-1', () => {
        calls.push({ type: 'delete' })
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const { user } = renderWithProviders(<NotifyTargetsPage />)

    expect(await screen.findByText('https://example.com/hook')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '+ 添加目标' }))
    await user.type(screen.getByTestId('create-notify-url'), 'https://example.com/new')
    await user.click(screen.getByTestId('create-notify-submit'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'create',
        body: {
          destinationType: 'raw-webhook',
          url: 'https://example.com/new',
          audience: 'all',
          timeoutSeconds: 10,
          disabled: false,
        },
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('新建')

    await user.click(screen.getByTitle('测试'))
    await waitFor(() => expect(calls).toContainEqual({ type: 'test' }))
    expect(toast.success).toHaveBeenCalledWith('测试成功 · 12 ms')

    await user.click(screen.getByTitle('编辑'))
    await user.clear(screen.getByTestId('edit-notify-url'))
    await user.type(screen.getByTestId('edit-notify-url'), 'https://example.com/updated')
    await user.click(screen.getByTestId('edit-notify-save'))
    await waitFor(() =>
      expect(calls).toContainEqual({
        type: 'patch',
        body: { url: 'https://example.com/updated' },
      }),
    )
    expect(toast.success).toHaveBeenCalledWith('保存')

    await user.click(screen.getByTitle('删除'))
    expect(await screen.findByRole('dialog', { name: '删除这个通知目标？' })).toBeInTheDocument()
    await user.click(screen.getByTestId('delete-notify-confirm'))
    await waitFor(() => expect(calls).toContainEqual({ type: 'delete' }))
    expect(toast.success).toHaveBeenCalledWith('删除')
  })

  it('surfaces target test and delete failures', async () => {
    server.use(
      http.get('/fb/v1/console/notify-targets', () =>
        HttpResponse.json({
          items: [
            {
              id: 'nt-1',
              destinationType: 'raw-webhook',
              audience: 'all',
              url: 'https://example.com/hook',
              timeoutSeconds: 10,
              disabled: false,
              lastFailureAt: '',
              lastError: '',
            },
          ],
        }),
      ),
      http.post('/fb/v1/console/notify-targets/nt-1/test', () =>
        HttpResponse.json({ message: 'signature mismatch' }, { status: 502 }),
      ),
      http.delete('/fb/v1/console/notify-targets/nt-1', () =>
        HttpResponse.json({ message: 'cannot delete target' }, { status: 500 }),
      ),
    )

    const { user } = renderWithProviders(<NotifyTargetsPage />)

    expect(await screen.findByText('https://example.com/hook')).toBeInTheDocument()
    await user.click(screen.getByTitle('测试'))
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('测试失败 · HTTP 502 · signature mismatch'),
    )

    await user.click(screen.getByTitle('删除'))
    await user.click(await screen.findByTestId('delete-notify-confirm'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot delete target'))
  })

  it('opens create from the empty state and keeps the dialog open when creation fails', async () => {
    server.use(
      http.get('/fb/v1/console/notify-targets', () => HttpResponse.json({ items: [] })),
      http.post('/fb/v1/console/notify-targets', () =>
        HttpResponse.json({ message: 'cannot create target' }, { status: 500 }),
      ),
    )

    const { user } = renderWithProviders(<NotifyTargetsPage />)

    expect(await screen.findByText('还没有通知目标')).toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: '+ 添加目标' })[1])
    await user.type(screen.getByTestId('create-notify-url'), 'https://example.com/fail')
    await user.click(screen.getByTestId('create-notify-submit'))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot create target'))
    expect(screen.getByTestId('create-notify-url')).toHaveValue('https://example.com/fail')
  })

  it('surfaces edit failures and allows edit and delete dialogs to be cancelled', async () => {
    server.use(
      http.get('/fb/v1/console/notify-targets', () =>
        HttpResponse.json({
          items: [
            {
              id: 'nt-1',
              destinationType: 'raw-webhook',
              audience: 'all',
              url: 'https://example.com/hook',
              timeoutSeconds: 10,
              disabled: false,
              lastFailureAt: '',
              lastError: '',
            },
          ],
        }),
      ),
      http.patch('/fb/v1/console/notify-targets/nt-1', () =>
        HttpResponse.json({ message: 'cannot edit target' }, { status: 500 }),
      ),
    )

    const { user } = renderWithProviders(<NotifyTargetsPage />)

    expect(await screen.findByText('https://example.com/hook')).toBeInTheDocument()
    await user.click(screen.getByTitle('编辑'))
    await user.clear(screen.getByTestId('edit-notify-url'))
    await user.type(screen.getByTestId('edit-notify-url'), 'https://example.com/broken')
    await user.click(screen.getByTestId('edit-notify-save'))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot edit target'))
    await user.click(screen.getByTestId('edit-notify-cancel'))
    await waitFor(() => expect(screen.queryByTestId('edit-notify-url')).not.toBeInTheDocument())

    await user.click(screen.getByTitle('删除'))
    expect(await screen.findByRole('dialog', { name: '删除这个通知目标？' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})
