import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { ApiKeysPage } from '@/features/api-keys/components/api-keys-page'
import { expectNoA11yViolations } from '@/testing/a11y'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const keyFixture = {
  id: 'key-1',
  keyPrefix: 'ak_live_1234',
  label: 'prod ingest',
  isActive: true,
  createdAt: '2026-06-07T00:00:00Z',
  scopes: [],
  allowedCidrs: [],
  usageCount: '0',
  environment: '',
}

describe('ApiKeysPage user flow', () => {
  it('renders existing keys, creates a key with one-time secret, and revokes a key', async () => {
    let createBody: unknown
    let deletedId = ''
    server.use(
      http.get('/fb/v1/console/api-keys', () => HttpResponse.json({ items: [keyFixture] })),
      http.get('/fb/v1/console/api-keys/presets', () => HttpResponse.json({ presets: [] })),
      http.get('/fb/v1/console/api-keys/scopes', () => HttpResponse.json({ scopes: [] })),
      http.post('/fb/v1/console/api-keys', async ({ request }) => {
        createBody = await request.json()
        return HttpResponse.json({
          key: { ...keyFixture, id: 'key-2', keyPrefix: 'ak_live_5678', label: 'ci ingest' },
          secret: 'ak_live_the-secret',
        })
      }),
      http.delete('/fb/v1/console/api-keys/:id', ({ params }) => {
        deletedId = String(params.id)
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const { container, user } = renderWithProviders(<ApiKeysPage />)

    await waitFor(() => {
      expect(screen.getByText('prod ingest')).toBeInTheDocument()
    })
    expect(screen.getByText('Key 总数')).toBeInTheDocument()
    expect(screen.getByText('Key 治理建议')).toBeInTheDocument()
    expect(screen.getByText('ak_live_1234…')).toBeInTheDocument()
    expect(screen.getByRole('table', { name: 'API key 列表' })).toBeInTheDocument()
    expect(screen.getByText('可用')).toBeInTheDocument()
    await expectNoA11yViolations(container)

    const createButton = screen.getByRole('button', { name: '+ 签发新 key' })
    createButton.focus()
    await user.keyboard('[Enter]')
    expect(screen.getByRole('dialog', { name: '签发新 API key' })).toBeInTheDocument()
    await user.keyboard('[Escape]')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(createButton).toHaveFocus()

    await user.click(createButton)
    const createDialog = screen.getByRole('dialog', { name: '签发新 API key' })
    expect(within(createDialog).getByLabelText('用途备注')).toHaveFocus()
    await expectNoA11yViolations(document.body)
    await user.type(within(createDialog).getByLabelText('用途备注'), 'ci ingest')
    await user.click(within(createDialog).getByTestId('create-key-submit'))
    await waitFor(() => {
      expect(createBody).toEqual({ label: 'ci ingest', scopes: [] })
      expect(screen.getByText('ak_live_the-secret')).toBeInTheDocument()
    })

    const secretDialog = screen.getByRole('dialog', { name: '你的新 key（仅此一次显示）' })
    expect(secretDialog).toContainElement(document.activeElement as HTMLElement)
    expect(createButton).not.toHaveFocus()
    await expectNoA11yViolations(document.body)
    await user.click(screen.getByRole('button', { name: '关闭' }))
    expect(createButton).toHaveFocus()
    const revokeButton = screen.getByRole('button', { name: '撤销 API key prod ingest' })
    revokeButton.focus()
    await user.keyboard('[Enter]')
    const revokeDialog = screen.getByRole('dialog', { name: '撤销这把 key？' })
    expect(within(revokeDialog).getByText(/prod ingest/)).toBeInTheDocument()
    await expectNoA11yViolations(document.body)
    await user.keyboard('[Escape]')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(revokeButton).toHaveFocus()

    await user.click(revokeButton)
    const confirmDialog = screen.getByRole('dialog', { name: '撤销这把 key？' })
    await user.click(within(confirmDialog).getByTestId('revoke-key-confirm'))
    await waitFor(() => {
      expect(deletedId).toBe('key-1')
    })
  })
})
