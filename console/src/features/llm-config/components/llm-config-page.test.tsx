import { HttpResponse, http } from 'msw'
import { defaultLLMChannelsList } from '@/testing/mocks/handlers'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'
import { LLMConfigPage } from './llm-config-page'

test('renders managed LLM config surfaces', async () => {
  renderWithProviders(<LLMConfigPage />)

  await waitFor(() => expect(screen.getAllByText('Primary').length).toBeGreaterThanOrEqual(2))
  await waitFor(() =>
    expect(screen.getAllByText('enrich-default').length).toBeGreaterThanOrEqual(2),
  )
  expect(await screen.findByText('gpt-4o-mini')).toBeInTheDocument()
  expect(await screen.findByText('Routes')).toBeInTheDocument()
})

test('renders empty states when no LLM channels or routes exist', async () => {
  server.use(
    http.get('/fb/v1/console/llm/channels', () => HttpResponse.json({ items: [] })),
    http.get('/fb/v1/console/llm/routes', () => HttpResponse.json({ items: [] })),
  )

  renderWithProviders(<LLMConfigPage />)

  expect(await screen.findByText('还没有 channel')).toBeInTheDocument()
  expect(screen.getByText('还没有路由')).toBeInTheDocument()
  expect(screen.getAllByText('未选择 channel').length).toBeGreaterThan(0)
  expect(screen.getAllByText('创建 channel 后再绑定 logical model 能力。').length).toBeGreaterThan(
    0,
  )
})

test('shows query error states for channels, routes, and abilities', async () => {
  server.use(
    http.get('/fb/v1/console/llm/channels', () => HttpResponse.json(defaultLLMChannelsList)),
    http.get('/fb/v1/console/llm/routes', () => new HttpResponse(null, { status: 500 })),
    http.get(
      '/fb/v1/console/llm/channels/:id/abilities',
      () => new HttpResponse(null, { status: 500 }),
    ),
  )

  renderWithProviders(<LLMConfigPage />)

  expect((await screen.findAllByText('Primary')).length).toBeGreaterThan(0)
  await waitFor(() => {
    expect(screen.getAllByText('出错了').length).toBeGreaterThanOrEqual(2)
  })
})

test('loads provider model options in the ability dialog', async () => {
  const { user } = renderWithProviders(<LLMConfigPage />)

  await screen.findAllByText('Primary')
  await user.click(screen.getByTitle('新增能力'))

  const picker = await screen.findByRole('combobox', { name: '选择 model' })

  expect(
    await screen.findByRole('option', { name: 'gpt-4.1-mini (GPT 4.1 mini)' }),
  ).toBeInTheDocument()

  await user.selectOptions(picker, 'gpt-4.1-mini')
  expect(picker).toHaveValue('gpt-4.1-mini')
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
