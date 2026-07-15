import { toast } from 'sonner'
import { describe, expect, it, vi } from 'vitest'
import type { LLMChannel, LLMChannelAbility, LLMRoute } from '@/features/llm-config/api/llm-config'
import { llmConfigPageTestables } from './llm-config-page'

describe('llmConfigPageTestables', () => {
  it('formats error text with api details and falls back cleanly for non-errors', () => {
    const error = new Error('boom') as Error & { code?: string; requestId?: string }
    error.code = 'E_CONFIG'
    error.requestId = 'req-123'

    expect(llmConfigPageTestables.errorText(error)).toBe('boom (E_CONFIG · req-123)')
    expect(llmConfigPageTestables.errorText(new Error('plain'))).toBe('plain')
    expect(llmConfigPageTestables.errorText({})).toBe('')
    expect(llmConfigPageTestables.queryError(new Error('query failed'))).toBe('query failed')
  })

  it('describes delete targets by entity kind', () => {
    const channel = {
      id: 'channel-1',
      name: 'Primary',
      protocol: 'openai-compat',
      baseUrl: 'http://localhost:11434',
      authMode: 'bearer',
      hasApiKey: true,
      credentialKeyId: 'key-1',
      status: 'enabled',
      priority: 0,
      weight: 1,
      timeoutSeconds: 60,
    } as LLMChannel
    const ability = {
      channelId: 'channel-1',
      logicalModel: 'gpt-4o-mini',
    } as LLMChannelAbility
    const globalRoute = {
      purpose: 'routing',
      tenantId: '',
    } as LLMRoute
    const tenantRoute = {
      purpose: 'routing',
      tenantId: 'tenant-acme',
    } as LLMRoute
    const t = (key: string, opts?: Record<string, string>) =>
      `${key}${opts ? `:${Object.values(opts).join(':')}` : ''}`

    expect(llmConfigPageTestables.deleteTitle(null, t)).toBe('')
    expect(llmConfigPageTestables.deleteTitle({ kind: 'channel', row: channel }, t)).toBe(
      'llm_config.delete.channel_title',
    )
    expect(llmConfigPageTestables.deleteTitle({ kind: 'ability', row: ability }, t)).toBe(
      'llm_config.delete.ability_title',
    )
    expect(llmConfigPageTestables.deleteTitle({ kind: 'route', row: globalRoute }, t)).toBe(
      'llm_config.delete.route_title',
    )

    expect(llmConfigPageTestables.deleteBody(null, t)).toBe('')
    expect(llmConfigPageTestables.deleteBody({ kind: 'channel', row: channel }, t)).toBe(
      'llm_config.delete.channel_body:Primary',
    )
    expect(llmConfigPageTestables.deleteBody({ kind: 'ability', row: ability }, t)).toBe(
      'llm_config.delete.ability_body:gpt-4o-mini',
    )
    expect(llmConfigPageTestables.deleteBody({ kind: 'route', row: globalRoute }, t)).toBe(
      'llm_config.delete.route_body:routing:llm_config.routes.global',
    )
    expect(llmConfigPageTestables.deleteBody({ kind: 'route', row: tenantRoute }, t)).toBe(
      'llm_config.delete.route_body:routing:tenant-acme',
    )
  })

  it('sends toast errors through the shared toast facade', () => {
    const spy = vi.spyOn(toast, 'error').mockImplementation(() => 0)

    llmConfigPageTestables.toastError(new Error('boom'))

    expect(spy).toHaveBeenCalledWith('boom')
  })
})
