import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
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
  serviceAccountId: 'sa-1',
}

const serviceAccountFixture = {
  id: 'sa-1',
  name: 'ci-bot',
  description: 'deployment pipeline',
  isActive: true,
  createdAt: '2026-06-07T00:00:00Z',
  updatedAt: '2026-06-08T00:00:00Z',
}

describe('ApiKeysPage user flow', () => {
  it('renders existing keys, creates a key with one-time secret, and revokes a key', async () => {
    let createBody: unknown
    let deletedId = ''
    server.use(
      http.get('/fb/v1/console/api-keys', () => HttpResponse.json({ items: [keyFixture] })),
      http.get('/fb/v1/console/service-accounts', () =>
        HttpResponse.json({ items: [serviceAccountFixture] }),
      ),
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

    await screen.findAllByText('prod ingest', {}, { timeout: 5_000 })
    expect(screen.getByText('Key 总数')).toBeInTheDocument()
    expect(screen.getByText('Key 治理建议')).toBeInTheDocument()
    const adoptionKit = await screen.findByTestId('developer-api-adoption-kit')
    expect(within(adoptionKit).getByText('开发者 API 采用包')).toBeInTheDocument()
    expect(
      within(adoptionKit).getByText(
        '0 scopes / 0 presets / 1 active keys / 1 service accounts / 14/14 artifacts verified',
      ),
    ).toBeInTheDocument()
    expect(
      within(adoptionKit).getByText('1 developer adoption lanes are blocked'),
    ).toBeInTheDocument()
    const sdkParityGate = await screen.findByTestId('developer-sdk-parity-gate')
    expect(within(sdkParityGate).getByText('开发者 SDK parity gate')).toBeInTheDocument()
    expect(
      within(sdkParityGate).getByText(
        '35/35 shared methods / verifier on / 0 browser-safe keys / 6/6 release gates',
      ),
    ).toBeInTheDocument()
    expect(within(sdkParityGate).getByText('1 SDK parity lanes need hardening')).toBeInTheDocument()
    const apiConsistencyContract = await screen.findByTestId('developer-api-consistency-contract')
    expect(
      within(apiConsistencyContract).getByText('开发者 API consistency contract'),
    ).toBeInTheDocument()
    expect(
      within(apiConsistencyContract).getByText(
        '3/3 public pagination surfaces / 3/3 console mirrors / 3/3 filters / 3/3 sort enums / verifier on',
      ),
    ).toBeInTheDocument()
    expect(
      within(apiConsistencyContract).getByText('developer API consistency contract is verified'),
    ).toBeInTheDocument()
    const importExportWorkbench = await screen.findByTestId('developer-import-export-workbench')
    expect(
      within(importExportWorkbench).getByText('开发者 import/export 工作台'),
    ).toBeInTheDocument()
    expect(
      within(importExportWorkbench).getByText(
        '2/2 formats / 4 templates / 4/4 required mappings / dry-run 37 create 2 update 1 reject / 4 recovery classes / verifier on',
      ),
    ).toBeInTheDocument()
    expect(
      within(importExportWorkbench).getByText('developer import/export workbench is verified'),
    ).toBeInTheDocument()
    await screen.findByText('服务账号目录', {}, { timeout: 5_000 })
    await screen.findByText('ci-bot', {}, { timeout: 5_000 })
    const apiKeyTable = screen.getByRole('table', { name: 'API key 列表' })
    expect(apiKeyTable).toBeInTheDocument()
    expect(within(apiKeyTable).getByText('ak_live_1234…')).toBeInTheDocument()
    expect(within(apiKeyTable).getByText('可用')).toBeInTheDocument()
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
    const revokeButton = screen.getAllByRole('button', { name: '撤销 API key prod ingest' })[0]
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

  it('shows the service-account empty state alongside the key registry empty state', async () => {
    server.use(
      http.get('/fb/v1/console/api-keys', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/service-accounts', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/api-keys/presets', () => HttpResponse.json({ presets: [] })),
      http.get('/fb/v1/console/api-keys/scopes', () => HttpResponse.json({ scopes: [] })),
    )

    const { container } = renderWithProviders(<ApiKeysPage />)

    await waitFor(() => {
      expect(screen.getByText('还没有 API key')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.getByText('还没有服务账号')).toBeInTheDocument()
    })
    expect(screen.getByText('服务账号目录')).toBeInTheDocument()
    await expectNoA11yViolations(container)
  })

  it('surfaces API key create and revoke failures from page handlers', async () => {
    server.use(
      http.get('/fb/v1/console/api-keys', () => HttpResponse.json({ items: [keyFixture] })),
      http.get('/fb/v1/console/service-accounts', () => HttpResponse.json({ items: [] })),
      http.get('/fb/v1/console/api-keys/presets', () => HttpResponse.json({ presets: [] })),
      http.get('/fb/v1/console/api-keys/scopes', () => HttpResponse.json({ scopes: [] })),
      http.post('/fb/v1/console/api-keys', () =>
        HttpResponse.json({ message: 'create denied' }, { status: 500 }),
      ),
      http.delete('/fb/v1/console/api-keys/:id', () =>
        HttpResponse.json({ message: 'revoke denied' }, { status: 500 }),
      ),
    )

    const { user } = renderWithProviders(<ApiKeysPage />)

    await screen.findAllByText('prod ingest', {}, { timeout: 5_000 })
    await user.click(screen.getByRole('button', { name: '+ 签发新 key' }))
    const createDialog = screen.getByRole('dialog', { name: '签发新 API key' })
    await user.type(within(createDialog).getByLabelText('用途备注'), 'ci ingest')
    await user.click(within(createDialog).getByTestId('create-key-submit'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('create denied'))

    vi.mocked(toast.error).mockClear()
    await user.click(within(createDialog).getByTestId('create-key-cancel'))
    await user.click(screen.getAllByRole('button', { name: '撤销 API key prod ingest' })[0])
    const revokeDialog = screen.getByRole('dialog', { name: '撤销这把 key？' })
    await user.click(within(revokeDialog).getByTestId('revoke-key-confirm'))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('revoke denied'))
  })
})
