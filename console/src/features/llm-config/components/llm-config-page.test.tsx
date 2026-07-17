import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { afterEach, vi } from 'vitest'
import { defaultLLMChannelsList } from '@/testing/mocks/handlers'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'
import { LLMConfigPage } from './llm-config-page'

afterEach(() => {
  vi.restoreAllMocks()
})

test('renders managed LLM config surfaces', async () => {
  const { user } = renderWithProviders(<LLMConfigPage />)

  await waitFor(() => expect(screen.getAllByText('Primary').length).toBeGreaterThanOrEqual(2))
  await waitFor(() =>
    expect(screen.getAllByText('enrich-default').length).toBeGreaterThanOrEqual(2),
  )
  expect(await screen.findByText('gpt-4o-mini')).toBeInTheDocument()
  expect(await screen.findByText('Routes')).toBeInTheDocument()

  const primaryRow = screen.getAllByRole('row').find((row) => row.textContent?.includes('Primary'))
  if (!primaryRow) throw new Error('channel row not found')
  await user.click(primaryRow)
  await user.click(screen.getByTitle('刷新'))
})

test('renders empty states when no LLM channels or routes exist', async () => {
  server.use(
    http.get('/fb/v1/console/llm/channels', () => HttpResponse.json({ items: [] })),
    http.get('/fb/v1/console/llm/routes', () => HttpResponse.json({ items: [] })),
  )

  const { user } = renderWithProviders(<LLMConfigPage />)

  expect(await screen.findByText('还没有 channel')).toBeInTheDocument()
  expect(screen.getAllByText('还没有路由').length).toBeGreaterThan(0)
  expect(screen.getAllByText('未选择 channel').length).toBeGreaterThan(0)
  expect(screen.getAllByText('创建 channel 后再绑定 logical model 能力。').length).toBeGreaterThan(
    0,
  )

  const createButtons = screen.getAllByRole('button', { name: '新建 channel' })
  await user.click(createButtons[createButtons.length - 1])
  expect(await screen.findByRole('heading', { name: '新建 LLM channel' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '取消' }))

  const routeButtons = screen.getAllByRole('button', { name: '新增路由' })
  await user.click(routeButtons[routeButtons.length - 1])
  expect(await screen.findByRole('heading', { name: 'LLM 路由' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '取消' }))
})

test('shows query error states for channels, routes, and abilities', async () => {
  let routeCalls = 0
  let abilityCalls = 0
  server.use(
    http.get('/fb/v1/console/llm/channels', () => HttpResponse.json(defaultLLMChannelsList)),
    http.get('/fb/v1/console/llm/routes', () => {
      routeCalls += 1
      return routeCalls === 1
        ? new HttpResponse(null, { status: 500 })
        : HttpResponse.json({
            items: [
              {
                id: 'route-1',
                tenantId: '',
                purpose: 'enrich',
                logicalModel: 'enrich-default',
                enabled: true,
                createdAt: '2026-06-11T00:00:00Z',
                updatedAt: '2026-06-11T00:00:00Z',
              },
            ],
          })
    }),
    http.get('/fb/v1/console/llm/channels/:id/abilities', () => {
      abilityCalls += 1
      return abilityCalls === 1
        ? new HttpResponse(null, { status: 500 })
        : HttpResponse.json({
            items: [
              {
                id: 'ability-1',
                channelId: '11111111-1111-1111-1111-111111111111',
                logicalModel: 'enrich-default',
                providerModel: 'gpt-4o-mini',
                enabled: true,
                priority: 100,
                weight: 1,
                createdAt: '2026-06-11T00:00:00Z',
                updatedAt: '2026-06-11T00:00:00Z',
              },
            ],
          })
    }),
  )

  const { user } = renderWithProviders(<LLMConfigPage />)

  expect((await screen.findAllByText('Primary')).length).toBeGreaterThan(0)
  await waitFor(() => {
    expect(screen.getAllByText('出错了').length).toBeGreaterThanOrEqual(2)
  })

  const routeCard = screen.getByText('Routes').closest('[data-slot="card"]')
  if (!routeCard) throw new Error('route card not found')
  await user.click(within(routeCard as HTMLElement).getByRole('button', { name: '刷新' }))

  const abilityCard = screen.getByText('能力').closest('[data-slot="card"]')
  if (!abilityCard) throw new Error('ability card not found')
  await user.click(within(abilityCard as HTMLElement).getByRole('button', { name: '刷新' }))

  await waitFor(() => {
    expect(screen.getAllByText('enrich-default').length).toBeGreaterThanOrEqual(2)
  })
})

test('retries channel query errors', async () => {
  let channelCalls = 0
  server.use(
    http.get('/fb/v1/console/llm/channels', () => {
      channelCalls += 1
      return channelCalls === 1
        ? new HttpResponse(null, { status: 500 })
        : HttpResponse.json(defaultLLMChannelsList)
    }),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  expect(await screen.findByText('出错了')).toBeInTheDocument()
  const refreshButtons = screen.getAllByRole('button', { name: '刷新' })
  await user.click(refreshButtons[refreshButtons.length - 1])

  await waitFor(() => {
    expect(screen.getAllByText('Primary').length).toBeGreaterThan(0)
  })
})

test('loads provider model options from the channel row ability action', async () => {
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  const primaryRow = screen.getAllByRole('row').find((row) => row.textContent?.includes('Primary'))
  if (!primaryRow) throw new Error('channel row not found')
  await user.click(within(primaryRow).getByTitle('能力'))

  const picker = await screen.findByRole('combobox', { name: '选择 model' })

  expect(
    await screen.findByRole('option', { name: 'gpt-4.1-mini (GPT 4.1 mini)' }),
  ).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: '刷新 models' }))
  await user.selectOptions(picker, 'gpt-4.1-mini')
  expect(picker).toHaveValue('gpt-4.1-mini')
})

test('opens the ability empty-state action for the selected channel', async () => {
  server.use(
    http.get('/fb/v1/console/llm/channels/:id/abilities', () => HttpResponse.json({ items: [] })),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  expect(await screen.findByText('还没有能力')).toBeInTheDocument()
  const abilityButtons = screen.getAllByRole('button', { name: '新增能力' })
  await user.click(abilityButtons[abilityButtons.length - 1])

  expect(await screen.findByRole('heading', { name: 'Channel 能力' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '取消' }))
})

test('creates a bearer channel with write-only api key input', async () => {
  let posted: unknown
  server.use(
    http.post('/fb/v1/console/llm/channels', async ({ request }) => {
      posted = await request.json()
      return HttpResponse.json({
        id: '44444444-4444-4444-4444-444444444444',
        name: 'Proxy Provider',
        protocol: 'openai-compat',
        baseUrl: 'http://localhost:3000',
        authMode: 'bearer',
        hasApiKey: true,
        credentialKeyId: '456',
        status: 'enabled',
        priority: 0,
        weight: 1,
        timeoutSeconds: 60,
        createdAt: '2026-06-11T00:00:00Z',
        updatedAt: '2026-06-11T00:00:00Z',
        lastTestStatus: '',
        lastError: '',
      })
    }),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  await user.click(screen.getByRole('button', { name: '新建 channel' }))
  await user.type(screen.getByLabelText('名称'), 'Proxy Provider')
  await user.type(screen.getByLabelText('Base URL'), 'http://localhost:3000')
  await user.type(screen.getByLabelText('API key'), 'sk-test-write-only')
  await user.click(screen.getByRole('button', { name: '新建' }))

  await waitFor(() => expect(posted).toBeDefined())
  expect(posted).toMatchObject({
    name: 'Proxy Provider',
    protocol: 'openai-compat',
    baseUrl: 'http://localhost:3000',
    authMode: 'bearer',
    apiKey: 'sk-test-write-only',
    status: 'enabled',
    priority: 0,
    weight: 1,
    timeoutSeconds: 60,
  })
})

test('tests a channel with a discovered provider model', async () => {
  let posted: unknown
  server.use(
    http.post('/fb/v1/console/llm/channels/:id/test', async ({ request }) => {
      posted = await request.json()
      return HttpResponse.json({
        ok: true,
        providerModel: 'gpt-4.1-mini',
        text: 'attune-ok',
        inputTokens: 3,
        outputTokens: 2,
        latencyMs: '42',
        channel: {
          id: '11111111-1111-1111-1111-111111111111',
          name: 'Primary',
          protocol: 'openai-compat',
          baseUrl: 'http://localhost:11434',
          authMode: 'bearer',
          hasApiKey: true,
          credentialKeyId: '123',
          status: 'enabled',
          priority: 100,
          weight: 1,
          timeoutSeconds: 60,
          createdAt: '2026-06-11T00:00:00Z',
          updatedAt: '2026-06-11T00:00:00Z',
          lastTestStatus: 'ok',
          lastError: '',
        },
      })
    }),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  await user.click(screen.getByTitle('测试'))
  await user.click(await screen.findByRole('button', { name: '刷新 models' }))
  const picker = await screen.findByRole('combobox', { name: '选择 model' })
  await user.selectOptions(picker, 'gpt-4.1-mini')
  await user.type(screen.getByLabelText('Prompt'), 'ping attune')
  await user.click(screen.getByRole('button', { name: '测试' }))

  await screen.findByText('gpt-4.1-mini')
  expect(await screen.findByText('3 / 2 tokens · 42ms')).toBeInTheDocument()
  expect(posted).toMatchObject({
    providerModel: 'gpt-4.1-mini',
    prompt: 'ping attune',
  })
})

test('surfaces channel test failures', async () => {
  const toastSpy = vi.spyOn(toast, 'error').mockImplementation(() => 0)
  server.use(
    http.post('/fb/v1/console/llm/channels/:id/test', () =>
      HttpResponse.json({ code: 'TEST_FAILED', message: 'test denied' }, { status: 500 }),
    ),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  await user.click(screen.getByTitle('测试'))
  await user.selectOptions(
    await screen.findByRole('combobox', { name: '选择 model' }),
    'gpt-4o-mini',
  )
  await user.click(screen.getByRole('button', { name: '测试' }))

  await waitFor(() => {
    expect(toastSpy).toHaveBeenCalledWith('test denied (TEST_FAILED)')
  })
})

test('cancels editor dialogs and delete confirmation', async () => {
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')

  await user.click(screen.getByRole('button', { name: '新建 channel' }))
  expect(await screen.findByRole('heading', { name: '新建 LLM channel' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '取消' }))

  await user.click(screen.getByTitle('新增能力'))
  expect(await screen.findByRole('heading', { name: 'Channel 能力' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '取消' }))

  await user.click(screen.getByTitle('新增路由'))
  expect(await screen.findByRole('heading', { name: 'LLM 路由' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '取消' }))

  await user.click(screen.getByTitle('测试'))
  expect(await screen.findByRole('heading', { name: '测试 channel' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '取消' }))

  await user.click(screen.getAllByTitle('删除')[0])
  expect(await screen.findByRole('heading', { name: '删除 channel' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '取消' }))
})

test('confirms channel deletion', async () => {
  let deleted = ''
  server.use(
    http.delete('/fb/v1/console/llm/channels/:id', ({ params }) => {
      deleted = String(params.id)
      return new HttpResponse(null, { status: 204 })
    }),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  await user.click(screen.getAllByTitle('删除')[0])
  expect(await screen.findByRole('heading', { name: '删除 channel' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '删除' }))

  await waitFor(() => {
    expect(deleted).toBe('11111111-1111-1111-1111-111111111111')
  })
})

test('updates an existing channel without replacing its api key', async () => {
  let patched: unknown
  server.use(
    http.patch('/fb/v1/console/llm/channels/:id', async ({ request, params }) => {
      patched = { id: params.id, body: await request.json() }
      return HttpResponse.json({
        ...defaultLLMChannelsList.items[0],
        id: String(params.id),
        name: 'Primary Edited',
      })
    }),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  await user.click(screen.getAllByTitle('编辑')[0])
  expect(await screen.findByRole('heading', { name: '编辑 LLM channel' })).toBeInTheDocument()
  await user.clear(screen.getByLabelText('名称'))
  await user.type(screen.getByLabelText('名称'), 'Primary Edited')
  await user.click(screen.getByRole('button', { name: '保存' }))

  await waitFor(() => expect(patched).toBeDefined())
  expect(patched).toMatchObject({
    id: '11111111-1111-1111-1111-111111111111',
    body: {
      name: 'Primary Edited',
      protocol: 'openai-compat',
      baseUrl: 'http://localhost:11434',
      authMode: 'bearer',
      status: 'enabled',
      priority: 100,
      weight: 1,
      timeoutSeconds: 60,
    },
  })
  expect(JSON.stringify(patched)).not.toContain('apiKey')
})

test('creates and deletes a selected channel ability', async () => {
  let upserted: unknown
  let deleted: unknown
  server.use(
    http.put('/fb/v1/console/llm/channels/:id/abilities', async ({ request, params }) => {
      upserted = { channelId: params.id, body: await request.json() }
      return HttpResponse.json({
        id: 'ability-new',
        channelId: String(params.id),
        logicalModel: 'semantic-small',
        providerModel: 'gpt-4.1-mini',
        enabled: true,
        priority: 0,
        weight: 1,
        createdAt: '2026-06-11T00:00:00Z',
        updatedAt: '2026-06-11T00:00:00Z',
      })
    }),
    http.post('/fb/v1/console/llm/channels/:id/abilities/delete', async ({ request, params }) => {
      deleted = { channelId: params.id, body: await request.json() }
      return new HttpResponse(null, { status: 204 })
    }),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  await user.click(screen.getByTitle('新增能力'))
  await user.type(await screen.findByLabelText('Logical model'), 'semantic-small')
  await user.selectOptions(screen.getByRole('combobox', { name: '选择 model' }), 'gpt-4.1-mini')
  await user.click(screen.getByRole('button', { name: '保存' }))

  await waitFor(() => expect(upserted).toBeDefined())
  expect(upserted).toMatchObject({
    channelId: '11111111-1111-1111-1111-111111111111',
    body: {
      logicalModel: 'semantic-small',
      providerModel: 'gpt-4.1-mini',
      enabled: true,
      priority: 0,
      weight: 1,
    },
  })

  const abilityRow = screen.getByText('gpt-4o-mini').closest('tr')
  if (!abilityRow) throw new Error('ability row not found')
  await user.click(within(abilityRow).getByTitle('删除'))
  expect(await screen.findByRole('heading', { name: '删除能力' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '删除' }))

  await waitFor(() => expect(deleted).toBeDefined())
  expect(deleted).toMatchObject({
    channelId: '11111111-1111-1111-1111-111111111111',
    body: { logicalModel: 'enrich-default' },
  })
})

test('edits a selected channel ability', async () => {
  let upserted: unknown
  server.use(
    http.put('/fb/v1/console/llm/channels/:id/abilities', async ({ request, params }) => {
      upserted = { channelId: params.id, body: await request.json() }
      return HttpResponse.json({
        id: 'ability-1',
        channelId: String(params.id),
        logicalModel: 'enrich-default',
        providerModel: 'gpt-4.1-mini',
        enabled: false,
        priority: 100,
        weight: 1,
        createdAt: '2026-06-11T00:00:00Z',
        updatedAt: '2026-06-11T00:00:00Z',
      })
    }),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  const abilityRow = screen.getByText('gpt-4o-mini').closest('tr')
  if (!abilityRow) throw new Error('ability row not found')
  await user.click(within(abilityRow).getByTitle('编辑'))
  await user.selectOptions(
    await screen.findByRole('combobox', { name: '选择 model' }),
    'gpt-4.1-mini',
  )
  await user.click(screen.getByRole('button', { name: '保存' }))

  await waitFor(() => expect(upserted).toBeDefined())
  expect(upserted).toMatchObject({
    channelId: '11111111-1111-1111-1111-111111111111',
    body: {
      logicalModel: 'enrich-default',
      providerModel: 'gpt-4.1-mini',
      enabled: true,
      priority: 100,
      weight: 1,
    },
  })
})

test('creates and deletes an LLM route', async () => {
  let upserted: unknown
  let deleted: unknown
  server.use(
    http.put('/fb/v1/console/llm/routes', async ({ request }) => {
      upserted = await request.json()
      return HttpResponse.json({
        id: 'route-new',
        tenantId: 'tenant-a',
        purpose: 'semantic_search',
        logicalModel: 'semantic-small',
        enabled: true,
        createdAt: '2026-06-11T00:00:00Z',
        updatedAt: '2026-06-11T00:00:00Z',
      })
    }),
    http.post('/fb/v1/console/llm/routes/delete', async ({ request }) => {
      deleted = await request.json()
      return new HttpResponse(null, { status: 204 })
    }),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  await user.click(screen.getByTitle('新增路由'))
  expect(await screen.findByRole('heading', { name: 'LLM 路由' })).toBeInTheDocument()
  await user.type(screen.getByLabelText('Tenant ID'), 'tenant-a')
  await user.clear(screen.getByLabelText('用途'))
  await user.type(screen.getByLabelText('用途'), 'semantic_search')
  await user.clear(screen.getByLabelText('Logical model'))
  await user.type(screen.getByLabelText('Logical model'), 'semantic-small')
  await user.click(screen.getByRole('button', { name: '保存' }))

  await waitFor(() => expect(upserted).toBeDefined())
  expect(upserted).toMatchObject({
    tenantId: 'tenant-a',
    purpose: 'semantic_search',
    logicalModel: 'semantic-small',
    enabled: true,
  })

  const routeRow = screen.getByText('global').closest('tr')
  if (!routeRow) throw new Error('route row not found')
  await user.click(within(routeRow).getByTitle('删除'))
  expect(await screen.findByRole('heading', { name: '删除路由' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '删除' }))

  await waitFor(() => expect(deleted).toBeDefined())
  expect(deleted).toMatchObject({ tenantId: '', purpose: 'enrich' })
})

test('edits an LLM route', async () => {
  let upserted: unknown
  server.use(
    http.put('/fb/v1/console/llm/routes', async ({ request }) => {
      upserted = await request.json()
      return HttpResponse.json({
        id: 'route-1',
        tenantId: '',
        purpose: 'enrich',
        logicalModel: 'semantic-small',
        enabled: true,
        createdAt: '2026-06-11T00:00:00Z',
        updatedAt: '2026-06-11T00:00:00Z',
      })
    }),
  )
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  const routeRow = screen.getByText('global').closest('tr')
  if (!routeRow) throw new Error('route row not found')
  await user.click(within(routeRow).getByTitle('编辑'))
  await user.clear(await screen.findByLabelText('Logical model'))
  await user.type(screen.getByLabelText('Logical model'), 'semantic-small')
  await user.click(screen.getByRole('button', { name: '保存' }))

  await waitFor(() => expect(upserted).toBeDefined())
  expect(upserted).toMatchObject({
    tenantId: '',
    purpose: 'enrich',
    logicalModel: 'semantic-small',
    enabled: true,
  })
})
