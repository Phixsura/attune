import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { ApiKeysPage } from '@/features/api-keys/components/api-keys-page'
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
}

describe('ApiKeysPage user flow', () => {
  it('renders existing keys, creates a key with one-time secret, and revokes a key', async () => {
    let createBody: unknown
    let deletedId = ''
    server.use(
      http.get('/fb/v1/console/api-keys', () => HttpResponse.json({ items: [keyFixture] })),
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

    const { user } = renderWithProviders(<ApiKeysPage />)

    await waitFor(() => {
      expect(screen.getByText('prod ingest')).toBeInTheDocument()
    })
    expect(screen.getByText('ak_live_1234…')).toBeInTheDocument()
    expect(screen.getByText('可用')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '+ 签发新 key' }))
    const createDialog = screen.getByRole('dialog', { name: '签发新 API key' })
    await user.type(within(createDialog).getByLabelText('用途备注'), 'ci ingest')
    await user.click(within(createDialog).getByTestId('create-key-submit'))
    await waitFor(() => {
      expect(createBody).toEqual({ label: 'ci ingest' })
      expect(screen.getByText('ak_live_the-secret')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '关闭' }))
    await user.click(screen.getByRole('button', { name: '撤销' }))
    const revokeDialog = screen.getByRole('dialog', { name: '撤销这把 key？' })
    expect(within(revokeDialog).getByText(/prod ingest/)).toBeInTheDocument()
    await user.click(within(revokeDialog).getByTestId('revoke-key-confirm'))
    await waitFor(() => {
      expect(deletedId).toBe('key-1')
    })
  })
})
