import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { CohortSyncPage } from '@/features/cohort-sync/components/cohort-sync-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

beforeEach(() => {
  vi.mocked(toast.success).mockClear()
  vi.mocked(toast.error).mockClear()
})

const emptyHandlers = [
  http.get('/fb/v1/console/cohort-sync/sources', () => HttpResponse.json({ sources: [] })),
  http.get('/fb/v1/console/cohort-sync/cohorts', () => HttpResponse.json({ cohorts: [] })),
  http.get('/fb/v1/console/cohort-sync/health', () =>
    HttpResponse.json({
      sourceCount: 0,
      activeSources: 0,
      errorSources: 0,
      cohortCount: 0,
      totalActiveMembers: 0,
    }),
  ),
]

const sourceFixture = {
  id: 'src-1',
  provider: 'amplitude',
  name: 'My Amplitude',
  authType: 'api_key',
  baseUrl: '',
  enabled: true,
  status: 'active',
  lastSyncAt: '2026-07-28T00:00:00Z',
  lastError: '',
  webhookUrl: '/v1/cohort-sync/amplitude/t1/src-1/add',
  webhookUrls: [
    'https://attune.example.com/v1/cohort-sync/amplitude/t1/src-1/create',
    'https://attune.example.com/v1/cohort-sync/amplitude/t1/src-1/add',
    'https://attune.example.com/v1/cohort-sync/amplitude/t1/src-1/remove',
  ],
  createdAt: '2026-07-28T00:00:00Z',
  updatedAt: '2026-07-28T00:00:00Z',
}

const cohortFixture = {
  id: 'cohort-1',
  cohortSourceId: 'src-1',
  externalCohortId: 'enterprise-customers',
  name: 'Enterprise customers',
  description: 'Customers on enterprise plans',
  staleTtlDays: 7,
  memberCount: 42,
  enabled: true,
  lastSyncedAt: '2026-07-28T00:00:00Z',
  lastError: '',
  createdAt: '2026-07-28T00:00:00Z',
  updatedAt: '2026-07-28T00:00:00Z',
  sourceName: 'My Amplitude',
  sourceProvider: 'amplitude',
}

const withSourceHandlers = [
  http.get('/fb/v1/console/cohort-sync/sources', () =>
    HttpResponse.json({ sources: [sourceFixture] }),
  ),
  http.get('/fb/v1/console/cohort-sync/cohorts', () => HttpResponse.json({ cohorts: [] })),
  http.get('/fb/v1/console/cohort-sync/health', () =>
    HttpResponse.json({
      sourceCount: 1,
      activeSources: 1,
      errorSources: 0,
      cohortCount: 0,
      totalActiveMembers: 0,
    }),
  ),
]

const withCohortHandlers = [
  http.get('/fb/v1/console/cohort-sync/sources', () =>
    HttpResponse.json({ sources: [sourceFixture] }),
  ),
  http.get('/fb/v1/console/cohort-sync/cohorts', () =>
    HttpResponse.json({ cohorts: [cohortFixture] }),
  ),
  http.get('/fb/v1/console/cohort-sync/health', () =>
    HttpResponse.json({
      sourceCount: 1,
      activeSources: 1,
      errorSources: 0,
      cohortCount: 1,
      totalActiveMembers: 42,
    }),
  ),
]

describe('CohortSyncPage', () => {
  it('renders empty state when no sources exist', async () => {
    server.use(...emptyHandlers)
    renderWithProviders(<CohortSyncPage />)

    await waitFor(() => {
      expect(screen.getByText('尚未配置人群来源')).toBeInTheDocument()
    })
    // "添加来源" appears in both PageHero and EmptyState
    expect(screen.getAllByText('添加来源').length).toBeGreaterThanOrEqual(1)
  })

  it('renders PageHero with health metrics', async () => {
    server.use(...withSourceHandlers)
    renderWithProviders(<CohortSyncPage />)

    await waitFor(() => {
      expect(screen.getByText('1/1')).toBeInTheDocument()
    })
    expect(screen.getByText('活跃成员')).toBeInTheDocument()
  })

  it('renders source table with name and provider', async () => {
    server.use(...withSourceHandlers)
    renderWithProviders(<CohortSyncPage />)

    await waitFor(() => {
      expect(screen.getByText('My Amplitude')).toBeInTheDocument()
    })
    expect(screen.getByText('amplitude')).toBeInTheDocument()
  })

  it('renders populated source and cohort tables with tooltip context', async () => {
    server.use(...withCohortHandlers)
    renderWithProviders(<CohortSyncPage />)

    await waitFor(() => {
      expect(screen.getByText('Enterprise customers')).toBeInTheDocument()
    })
    expect(screen.getByText('42')).toBeInTheDocument()
  })

  it('creates source and shows toast on success', async () => {
    const calls: string[] = []
    server.use(
      ...emptyHandlers,
      http.post('/fb/v1/console/cohort-sync/sources', () => {
        calls.push('create')
        return HttpResponse.json(sourceFixture, { status: 201 })
      }),
    )

    const { user } = renderWithProviders(<CohortSyncPage />)
    await waitFor(() => expect(screen.getAllByText('添加来源').length).toBeGreaterThanOrEqual(1))

    // Click any "添加来源" button to open the create dialog
    await user.click(screen.getAllByText('添加来源')[0])

    // Fill the form
    await waitFor(() => expect(screen.getByLabelText('名称')).toBeInTheDocument())
    await user.type(screen.getByLabelText('名称'), 'Test Source')
    await user.type(screen.getByLabelText('Webhook 认证密钥'), 'test-key')

    // Submit
    await user.click(screen.getByText('新建'))
    await waitFor(() => expect(calls).toContain('create'))
    expect(toast.success).toHaveBeenCalledWith('新建')
  })

  it('shows toast.error on create failure', async () => {
    server.use(
      ...emptyHandlers,
      http.post('/fb/v1/console/cohort-sync/sources', () =>
        HttpResponse.json({ message: 'name is required' }, { status: 400 }),
      ),
    )

    const { user } = renderWithProviders(<CohortSyncPage />)
    await waitFor(() => expect(screen.getAllByText('添加来源').length).toBeGreaterThanOrEqual(1))

    await user.click(screen.getAllByText('添加来源')[0])
    await waitFor(() => expect(screen.getByLabelText('名称')).toBeInTheDocument())
    await user.type(screen.getByLabelText('名称'), 'x')
    await user.type(screen.getByLabelText('Webhook 认证密钥'), 'k')
    await user.click(screen.getByText('新建'))

    await waitFor(() => expect(toast.error).toHaveBeenCalled())
  })

  it('deletes source via dialog and shows toast', async () => {
    const calls: string[] = []
    server.use(
      ...withSourceHandlers,
      http.delete('/fb/v1/console/cohort-sync/sources/src-1', () => {
        calls.push('delete')
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const { user } = renderWithProviders(<CohortSyncPage />)
    await waitFor(() => expect(screen.getByText('My Amplitude')).toBeInTheDocument())

    // The action cell has icon buttons. Click the last one (delete/Trash2).
    const row = screen.getByText('My Amplitude').closest('tr')
    const actionButtons = row?.querySelectorAll('td:last-child button') ?? []
    await user.click(actionButtons[actionButtons.length - 1] as HTMLElement)

    // Confirm in dialog — wait for "删除来源" heading
    const heading = await screen.findByText('删除来源')
    expect(heading).toBeInTheDocument()

    // Find and click the destructive button by its variant class
    const dialog = heading.closest('[role="dialog"]') ?? heading.closest('[data-state="open"]')
    const confirmButton =
      (dialog?.querySelector('button.bg-destructive') as HTMLElement) ??
      (Array.from(dialog?.querySelectorAll('button') ?? []).at(-1) as HTMLElement)
    await user.click(confirmButton)

    await waitFor(() => expect(calls).toContain('delete'))
    expect(toast.success).toHaveBeenCalledWith('删除')
  })

  it('test source shows success toast', async () => {
    server.use(
      ...withSourceHandlers,
      http.post('/fb/v1/console/cohort-sync/sources/src-1:test', () =>
        HttpResponse.json({ ok: true, error: '' }),
      ),
    )

    const { user } = renderWithProviders(<CohortSyncPage />)
    await waitFor(() => expect(screen.getByText('My Amplitude')).toBeInTheDocument())

    // Find Zap icon button (test button)
    const allButtons = screen.getAllByRole('button')
    const testButton = allButtons.find((b) => b.closest('td') && b.querySelector('svg.lucide-zap'))
    expect(testButton).toBeDefined()
    await user.click(testButton as HTMLElement)

    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('连接成功'))
  })

  it('test source shows error toast on failure', async () => {
    server.use(
      ...withSourceHandlers,
      http.post('/fb/v1/console/cohort-sync/sources/src-1:test', () =>
        HttpResponse.json({ ok: false, error: 'auth failed' }),
      ),
    )

    const { user } = renderWithProviders(<CohortSyncPage />)
    await waitFor(() => expect(screen.getByText('My Amplitude')).toBeInTheDocument())

    const allButtons = screen.getAllByRole('button')
    const testButton = allButtons.find((b) => b.closest('td') && b.querySelector('svg.lucide-zap'))
    expect(testButton).toBeDefined()
    await user.click(testButton as HTMLElement)

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('连接失败: auth failed'))
  })
})
