import { describe, expect, it, vi } from 'vitest'
import type { LLMChannel, LLMProviderModel, LLMRoute } from '@/features/llm-config/api/llm-config'
import {
  AbilityDialog,
  ChannelDialog,
  ConfirmDialog,
  RouteDialog,
  TestChannelDialog,
} from '@/features/llm-config/components/dialogs'
import { fireEvent, renderWithProviders, screen, waitFor } from '@/testing/test-utils'

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
  it('ignores disabled form submissions for incomplete channel, ability, and route dialogs', () => {
    const onChannelSubmit = vi.fn().mockResolvedValue(undefined)
    const channelRender = renderWithProviders(
      <ChannelDialog
        open
        target={null}
        pending={false}
        onOpenChange={vi.fn()}
        onSubmit={onChannelSubmit}
      />,
    )
    fireEvent.submit(screen.getByLabelText('名称').closest('form') as HTMLFormElement)
    expect(onChannelSubmit).not.toHaveBeenCalled()
    channelRender.unmount()

    const onAbilitySubmit = vi.fn().mockResolvedValue(undefined)
    const abilityRender = renderWithProviders(
      <AbilityDialog
        open
        target={null}
        models={[]}
        modelsPending={false}
        modelsError=""
        pending={false}
        onRefreshModels={vi.fn()}
        onOpenChange={vi.fn()}
        onSubmit={onAbilitySubmit}
      />,
    )
    fireEvent.submit(screen.getByLabelText('Logical model').closest('form') as HTMLFormElement)
    expect(onAbilitySubmit).not.toHaveBeenCalled()
    abilityRender.unmount()

    const onRouteSubmit = vi.fn().mockResolvedValue(undefined)
    renderWithProviders(
      <RouteDialog
        open
        target={null}
        pending={false}
        onOpenChange={vi.fn()}
        onSubmit={onRouteSubmit}
      />,
    )
    fireEvent.submit(screen.getByLabelText('用途').closest('form') as HTMLFormElement)
    expect(onRouteSubmit).not.toHaveBeenCalled()
  })

  it('submits new channel settings with selected protocol, auth, status, and api key', async () => {
    const onOpenChange = vi.fn()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <ChannelDialog
        open
        target={null}
        pending={false}
        onOpenChange={onOpenChange}
        onSubmit={onSubmit}
      />,
    )

    await user.type(screen.getByLabelText('名称'), 'Anthropic Primary')
    await user.type(screen.getByLabelText('Base URL'), 'https://api.anthropic.com')
    await user.type(screen.getByLabelText('API key'), 'sk-test')

    await user.click(screen.getAllByRole('combobox')[0])
    await user.click(await screen.findByRole('option', { name: 'anthropic' }))
    await user.click(screen.getAllByRole('combobox')[1])
    await user.click(await screen.findByRole('option', { name: 'none' }))
    await user.click(screen.getAllByRole('combobox')[1])
    await user.click(await screen.findByRole('option', { name: 'bearer' }))
    await user.type(screen.getByLabelText('API key'), 'sk-test')
    await user.click(screen.getAllByRole('combobox')[2])
    await user.click(await screen.findByRole('option', { name: 'draining' }))

    await user.click(screen.getByRole('button', { name: '新建' }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        name: 'Anthropic Primary',
        protocol: 'anthropic',
        baseUrl: 'https://api.anthropic.com',
        authMode: 'bearer',
        status: 'draining',
        priority: 0,
        weight: 1,
        timeoutSeconds: 60,
        apiKey: 'sk-test',
      })
    })

    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

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
    const onOpenChange = vi.fn()
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
        onOpenChange={onOpenChange}
        onSubmit={onSubmit}
      />,
    )

    expect(screen.getByText('model discovery failed')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '刷新 models' }))
    await user.type(screen.getByLabelText('Logical model'), 'semantic-small')
    await user.type(screen.getByLabelText('Provider model'), 'text-embedding-3-small')
    await user.clear(screen.getByLabelText('优先级'))
    await user.clear(screen.getByLabelText('权重'))
    await user.click(screen.getByRole('combobox'))
    await user.click(await screen.findByRole('option', { name: 'disabled' }))
    await user.click(screen.getByRole('button', { name: '保存' }))

    expect(onRefreshModels).toHaveBeenCalledTimes(1)
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        logicalModel: 'semantic-small',
        providerModel: 'text-embedding-3-small',
        enabled: false,
        priority: 0,
        weight: 1,
      })
    })

    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('submits a new route and closes from the cancel action', async () => {
    const onOpenChange = vi.fn()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <RouteDialog
        open
        target={null}
        pending={false}
        onOpenChange={onOpenChange}
        onSubmit={onSubmit}
      />,
    )

    await user.type(screen.getByLabelText('Tenant ID'), 'tenant-1')
    await user.clear(screen.getByLabelText('用途'))
    await user.type(screen.getByLabelText('用途'), 'reply_draft')
    await user.type(screen.getByLabelText('Logical model'), 'gpt-4.1')
    await user.click(screen.getByRole('combobox'))
    await user.click(await screen.findByRole('option', { name: 'disabled' }))
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        tenantId: 'tenant-1',
        purpose: 'reply_draft',
        logicalModel: 'gpt-4.1',
        enabled: false,
      })
    })

    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
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

  it('closes the channel test dialog from cancel and escape', async () => {
    const onClose = vi.fn()
    const { user, rerender } = renderWithProviders(
      <TestChannelDialog
        target={channel}
        models={[]}
        modelsPending={false}
        modelsError=""
        pending={false}
        onRefreshModels={vi.fn()}
        onClose={onClose}
        onSubmit={vi.fn()}
      />,
    )

    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(onClose).toHaveBeenCalledTimes(1)

    rerender(
      <TestChannelDialog
        target={channel}
        models={[]}
        modelsPending={false}
        modelsError=""
        pending={false}
        onRefreshModels={vi.fn()}
        onClose={onClose}
        onSubmit={vi.fn()}
      />,
    )
    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('keeps the channel test dialog closed when there is no target channel', () => {
    renderWithProviders(
      <TestChannelDialog
        target={null}
        models={[]}
        modelsPending={false}
        modelsError=""
        pending={false}
        onRefreshModels={vi.fn()}
        onClose={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
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

  it('calls confirm cancel when the dialog closes itself', async () => {
    const onCancel = vi.fn()
    const { user } = renderWithProviders(
      <ConfirmDialog
        open
        title="删除 channel"
        body="Delete Primary"
        pending={false}
        onCancel={onCancel}
        onConfirm={vi.fn()}
      />,
    )

    await user.keyboard('{Escape}')

    expect(onCancel).toHaveBeenCalledTimes(1)
  })
})
