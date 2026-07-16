import { describe, expect, it, vi } from 'vitest'
import type { LLMChannel, LLMProviderModel, LLMRoute } from '@/features/llm-config/api/llm-config'
import {
  AbilityDialog,
  ChannelDialog,
  ConfirmDialog,
  RouteDialog,
  TestChannelDialog,
} from '@/features/llm-config/components/dialogs'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

const channel: LLMChannel = {
  id: 'channel-1',
  name: 'Primary',
  protocol: 'openai-compat',
  baseUrl: 'http://localhost:11434',
  authMode: 'bearer',
  hasApiKey: true,
  credentialKeyId: 'key-1',
  status: 'enabled',
  priority: 100,
  weight: 2,
  timeoutSeconds: 60,
  createdAt: '2026-06-11T00:00:00Z',
  updatedAt: '2026-06-11T00:00:00Z',
  lastTestStatus: '',
  lastError: '',
}

const model: LLMProviderModel = {
  id: 'gpt-4.1-mini',
  displayName: 'GPT 4.1 mini',
  ownedBy: 'openai',
}

describe('LLM config dialogs', () => {
  it('submits edited channel settings without requiring a replacement api key', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <ChannelDialog
        open
        target={channel}
        pending={false}
        onOpenChange={vi.fn()}
        onSubmit={onSubmit}
      />,
    )

    await user.clear(screen.getByLabelText('名称'))
    await user.type(screen.getByLabelText('名称'), 'Primary Updated')
    await user.clear(screen.getByLabelText('优先级'))
    await user.clear(screen.getByLabelText('权重'))
    await user.clear(screen.getByLabelText('超时秒数'))
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        name: 'Primary Updated',
        protocol: 'openai-compat',
        baseUrl: 'http://localhost:11434',
        authMode: 'bearer',
        status: 'enabled',
        priority: 0,
        weight: 1,
        timeoutSeconds: 60,
      })
    })
  })

  it('submits ability settings and exposes model refresh errors', async () => {
    const onRefreshModels = vi.fn()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <AbilityDialog
        open
        target={null}
        models={[]}
        modelsPending={false}
        modelsError="model discovery failed"
        pending={false}
        onRefreshModels={onRefreshModels}
        onOpenChange={vi.fn()}
        onSubmit={onSubmit}
      />,
    )

    expect(screen.getByText('model discovery failed')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '刷新 models' }))
    await user.type(screen.getByLabelText('Logical model'), 'semantic-small')
    await user.type(screen.getByLabelText('Provider model'), 'text-embedding-3-small')
    await user.clear(screen.getByLabelText('权重'))
    await user.click(screen.getByRole('button', { name: '保存' }))

    expect(onRefreshModels).toHaveBeenCalledTimes(1)
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        logicalModel: 'semantic-small',
        providerModel: 'text-embedding-3-small',
        enabled: true,
        priority: 0,
        weight: 1,
      })
    })
  })

  it('submits route edits with trimmed fields', async () => {
    const route: LLMRoute = {
      id: 'route-1',
      tenantId: '',
      purpose: 'enrich',
      logicalModel: 'gpt-4o-mini',
      enabled: true,
      createdAt: '2026-06-11T00:00:00Z',
      updatedAt: '2026-06-11T00:00:00Z',
    }
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <RouteDialog
        open
        target={route}
        pending={false}
        onOpenChange={vi.fn()}
        onSubmit={onSubmit}
      />,
    )

    await user.clear(screen.getByLabelText('Logical model'))
    await user.type(screen.getByLabelText('Logical model'), '  gpt-4.1-mini  ')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        tenantId: '',
        purpose: 'enrich',
        logicalModel: 'gpt-4.1-mini',
        enabled: true,
      })
    })
  })

  it('submits a channel test with selected provider model and prompt', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <TestChannelDialog
        target={channel}
        models={[model]}
        modelsPending={false}
        modelsError=""
        pending={false}
        onRefreshModels={vi.fn()}
        onClose={vi.fn()}
        onSubmit={onSubmit}
      />,
    )

    await user.selectOptions(screen.getByRole('combobox', { name: '选择 model' }), model.id)
    await user.type(screen.getByLabelText('Prompt'), '  ping  ')
    await user.click(screen.getByRole('button', { name: '测试' }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        providerModel: model.id,
        prompt: 'ping',
      })
    })
  })

  it('resets an open test dialog when the target channel changes', async () => {
    const { rerender, user } = renderWithProviders(
      <TestChannelDialog
        target={channel}
        models={[model]}
        modelsPending={false}
        modelsError=""
        pending={false}
        onRefreshModels={vi.fn()}
        onClose={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )

    await user.selectOptions(screen.getByRole('combobox', { name: '选择 model' }), model.id)
    await user.type(screen.getByLabelText('Prompt'), 'ping')
    rerender(
      <TestChannelDialog
        target={{ ...channel, id: 'channel-2', name: 'Secondary' }}
        models={[model]}
        modelsPending={false}
        modelsError=""
        pending={false}
        onRefreshModels={vi.fn()}
        onClose={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )

    expect(screen.getByLabelText('Prompt')).toHaveValue('')
    expect(screen.getByRole('combobox', { name: '选择 model' })).toHaveValue('')
  })

  it('wires confirm dialog cancel and confirm actions', async () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <ConfirmDialog
        open
        title="删除 channel"
        body="Delete Primary"
        pending={false}
        onCancel={onCancel}
        onConfirm={onConfirm}
      />,
    )

    await user.click(screen.getByRole('button', { name: '取消' }))
    await user.click(screen.getByRole('button', { name: '删除' }))

    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })
})
