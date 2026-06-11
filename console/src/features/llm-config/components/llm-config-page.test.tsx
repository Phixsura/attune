import { HttpResponse, http } from 'msw'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { LLMConfigPage } from './llm-config-page'

test('renders managed LLM config surfaces', async () => {
  renderWithProviders(<LLMConfigPage />)

  expect(await screen.findAllByText('Primary')).toHaveLength(2)
  expect(await screen.findAllByText('enrich-default')).toHaveLength(2)
  expect(await screen.findByText('gpt-4o-mini')).toBeInTheDocument()
  expect(await screen.findByText('Routes')).toBeInTheDocument()
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
