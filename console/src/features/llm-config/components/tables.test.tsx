import { describe, expect, it, vi } from 'vitest'
import type { LLMChannel, LLMChannelAbility, LLMRoute } from '@/features/llm-config/api/llm-config'
import { fireEvent, renderWithProviders, screen, within } from '@/testing/test-utils'
import { AbilityTable, ChannelTable, RouteTable } from './tables'

describe('LLM config tables', () => {
  it('renders channel auth/status branches and dispatches row actions', async () => {
    const channels = [
      testChannel({ id: 'ch-none', name: 'No auth', authMode: 'none', status: 'enabled' }),
      testChannel({
        id: 'ch-set',
        name: 'Credential set',
        hasApiKey: true,
        baseUrl: 'https://llm.example.test',
        status: 'ok',
        lastTestStatus: 'ok',
      }),
      testChannel({ id: 'ch-missing', name: 'Missing key', status: 'draining' }),
    ]
    const onSelect = vi.fn()
    const onTest = vi.fn()
    const onAbilities = vi.fn()
    const onEdit = vi.fn()
    const onDelete = vi.fn()
    const { user } = renderWithProviders(
      <ChannelTable
        channels={channels}
        selectedId="ch-missing"
        testingId="ch-missing"
        onSelect={onSelect}
        onTest={onTest}
        onAbilities={onAbilities}
        onEdit={onEdit}
        onDelete={onDelete}
      />,
    )

    expect(screen.getAllByText('default')).toHaveLength(2)
    expect(screen.getByText('https://llm.example.test')).toBeInTheDocument()
    expect(screen.getAllByText('ok')).toHaveLength(2)
    const selectedRow = screen.getByText('Missing key').closest('tr')
    expect(selectedRow).toHaveAttribute('aria-selected', 'true')
    expect(within(selectedRow as HTMLElement).getAllByRole('button')[0]).toBeDisabled()

    const firstRow = screen.getByText('No auth').closest('tr') as HTMLElement
    await user.click(firstRow)
    fireEvent.keyDown(firstRow, { key: 'Enter' })
    fireEvent.keyDown(firstRow, { key: ' ' })
    expect(onSelect).toHaveBeenCalledTimes(3)
    expect(onSelect).toHaveBeenLastCalledWith(channels[0])

    const [testButton, abilitiesButton, editButton, deleteButton] =
      within(firstRow).getAllByRole('button')
    await user.click(testButton)
    await user.click(abilitiesButton)
    await user.click(editButton)
    await user.click(deleteButton)

    expect(onSelect).toHaveBeenCalledTimes(3)
    expect(onTest).toHaveBeenCalledWith(channels[0])
    expect(onAbilities).toHaveBeenCalledWith(channels[0])
    expect(onEdit).toHaveBeenCalledWith(channels[0])
    expect(onDelete).toHaveBeenCalledWith(channels[0])
  })

  it('renders abilities and route actions', async () => {
    const ability = testAbility({ enabled: false })
    const route = testRoute({
      tenantId: '',
      purpose: 'background-summary',
      logicalModel: 'summary-model',
      enabled: false,
    })
    const onEditAbility = vi.fn()
    const onDeleteAbility = vi.fn()
    const onEditRoute = vi.fn()
    const onDeleteRoute = vi.fn()
    const { user } = renderWithProviders(
      <>
        <AbilityTable abilities={[ability]} onEdit={onEditAbility} onDelete={onDeleteAbility} />
        <RouteTable routes={[route]} onEdit={onEditRoute} onDelete={onDeleteRoute} />
      </>,
    )

    expect(screen.getByText('gpt-4.1-mini')).toBeInTheDocument()
    expect(screen.getByText('chat')).toBeInTheDocument()
    expect(screen.getByText('background-summary')).toBeInTheDocument()
    expect(screen.getAllByText('disabled')).toHaveLength(2)

    const abilityRow = screen.getByText('gpt-4.1-mini').closest('tr') as HTMLElement
    const [editAbility, deleteAbility] = within(abilityRow).getAllByRole('button')
    await user.click(editAbility)
    await user.click(deleteAbility)
    expect(onEditAbility).toHaveBeenCalledWith(ability)
    expect(onDeleteAbility).toHaveBeenCalledWith(ability)

    const routeRow = screen.getByText('background-summary').closest('tr') as HTMLElement
    const [editRoute, deleteRoute] = within(routeRow).getAllByRole('button')
    await user.click(editRoute)
    await user.click(deleteRoute)
    expect(onEditRoute).toHaveBeenCalledWith(route)
    expect(onDeleteRoute).toHaveBeenCalledWith(route)
  })
})

function testChannel(overrides: Partial<LLMChannel> = {}): LLMChannel {
  return {
    id: 'ch-1',
    name: 'Primary',
    protocol: 'openai-responses',
    baseUrl: '',
    authMode: 'bearer',
    hasApiKey: false,
    credentialKeyId: '',
    status: 'enabled',
    priority: 10,
    weight: 100,
    timeoutSeconds: 30,
    createdAt: '2026-07-16T00:00:00Z',
    updatedAt: '2026-07-16T00:00:00Z',
    lastTestStatus: '',
    lastError: '',
    ...overrides,
  }
}

function testAbility(overrides: Partial<LLMChannelAbility> = {}): LLMChannelAbility {
  return {
    id: 'ability-1',
    channelId: 'ch-1',
    logicalModel: 'chat',
    providerModel: 'gpt-4.1-mini',
    enabled: true,
    priority: 1,
    weight: 100,
    createdAt: '2026-07-16T00:00:00Z',
    updatedAt: '2026-07-16T00:00:00Z',
    ...overrides,
  }
}

function testRoute(overrides: Partial<LLMRoute> = {}): LLMRoute {
  return {
    id: 'route-1',
    tenantId: 'tenant-1',
    purpose: 'chat',
    logicalModel: 'chat',
    enabled: true,
    createdAt: '2026-07-16T00:00:00Z',
    updatedAt: '2026-07-16T00:00:00Z',
    ...overrides,
  }
}
