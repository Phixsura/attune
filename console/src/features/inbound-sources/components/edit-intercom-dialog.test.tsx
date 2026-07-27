import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import type { InboundSource } from '@/features/inbound-sources/api/list-inbound-sources'
import { EditIntercomSourceDialog } from '@/features/inbound-sources/components/edit-intercom-dialog'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

const source: InboundSource = {
  id: 'source-ic-1',
  tenantId: 'tenant-1',
  channel: 'intercom',
  name: 'Support Inbox',
  slug: 'support-inbox',
  enabled: true,
  lastUid: '0',
  lastError: '',
  createdAt: '2026-07-17T00:00:00Z',
  updatedAt: '2026-07-17T00:00:00Z',
}

const detailWithSettings = {
  ...source,
  intercomSettings: {
    region: 'eu',
    startFrom: 'full',
    filterStates: ['open'],
    filterTags: ['bug', 'vip'],
    filterExcludeTags: ['spam'],
    maxDetailFetches: 120,
    workspaceId: 'ws-9',
  },
}

describe('EditIntercomSourceDialog', () => {
  it('prefills stored filters and budget from the detail read-back', async () => {
    server.use(
      http.get('/fb/v1/console/inbound/sources/:id', () => HttpResponse.json(detailWithSettings)),
    )
    renderWithProviders(<EditIntercomSourceDialog source={source} onClose={vi.fn()} />)

    expect(screen.getByLabelText('名称')).toHaveValue('Support Inbox')
    await waitFor(() => {
      expect(screen.getByLabelText('包含标签')).toHaveValue('bug, vip')
    })
    expect(screen.getByLabelText('排除标签')).toHaveValue('spam')
    expect(screen.getByLabelText('每轮会话详情预算')).toHaveValue(120)
  })

  it('re-submits stored settings so an untouched save is lossless', async () => {
    server.use(
      http.get('/fb/v1/console/inbound/sources/:id', () => HttpResponse.json(detailWithSettings)),
    )
    let patched: Record<string, unknown> | null = null
    server.use(
      http.patch('/fb/v1/console/inbound/sources/:id', async ({ request }) => {
        patched = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(detailWithSettings)
      }),
    )
    const onClose = vi.fn()
    const { user } = renderWithProviders(
      <EditIntercomSourceDialog source={source} onClose={onClose} />,
    )
    await waitFor(() => {
      expect(screen.getByLabelText('每轮会话详情预算')).toHaveValue(120)
    })

    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(onClose).toHaveBeenCalled())
    const cfg = (patched as unknown as { intercomConfig: Record<string, unknown> })?.intercomConfig
    expect(cfg.filterTags).toEqual(['bug', 'vip'])
    expect(cfg.filterExcludeTags).toEqual(['spam'])
    expect(cfg.filterStates).toEqual(['open'])
    expect(cfg.maxDetailFetches).toBe(120)
    expect(cfg.accessToken).toBe('')
  })

  it('surfaces the API error on a rejected save', async () => {
    server.use(
      http.get('/fb/v1/console/inbound/sources/:id', () => HttpResponse.json(detailWithSettings)),
      http.patch('/fb/v1/console/inbound/sources/:id', () =>
        HttpResponse.json({ message: 'region is immutable' }, { status: 400 }),
      ),
    )
    const onClose = vi.fn()
    const { user } = renderWithProviders(
      <EditIntercomSourceDialog source={source} onClose={onClose} />,
    )
    await waitFor(() => {
      expect(screen.getByLabelText('每轮会话详情预算')).toHaveValue(120)
    })

    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(onClose).not.toHaveBeenCalled())
  })

  it('keeps content unmounted when no source is selected', () => {
    renderWithProviders(<EditIntercomSourceDialog source={null} onClose={vi.fn()} />)
    expect(screen.queryByText('编辑 Intercom 来源')).not.toBeInTheDocument()
  })
})
