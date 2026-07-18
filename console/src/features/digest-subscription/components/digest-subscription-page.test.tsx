import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { DigestSubscription } from '@/proto/attune/v1/digest_subscription'
import { server } from '@/testing/mocks/server'
import { fireEvent, renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { DigestSubscriptionPage } from './digest-subscription-page'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

beforeEach(() => {
  vi.mocked(toast.success).mockClear()
  vi.mocked(toast.error).mockClear()
})

function seeded(data: DigestSubscription | null) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  qc.setQueryData(['console', 'digest-subscription'], data)
  return qc
}

const sample: DigestSubscription = {
  enabled: true,
  frequency: 'daily',
  sendHour: 9,
  llmMinFeedback: 6,
  sendOnEmpty: false,
  nextRunAt: '2026-06-14T09:00:00Z',
  createdAt: '2026-06-13T00:00:00Z',
  updatedAt: '2026-06-13T00:00:00Z',
  clusteringEnabled: false,
}

describe('DigestSubscriptionPage', () => {
  it('populates the form from the loaded config and hides weekday for daily', async () => {
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(sample) })
    await waitFor(() => {
      expect((screen.getByTestId('digest-send-hour') as HTMLInputElement).value).toBe('9')
    })
    expect(screen.getByText('摘要编排面板')).toBeInTheDocument()
    expect(screen.getByText('摘要治理建议')).toBeInTheDocument()
    expect(screen.getByText('这个开关同时控制反馈聚类能力')).toBeInTheDocument()
    expect(screen.queryByTestId('digest-weekday')).toBeNull()
  })

  it('reveals the weekday selector when frequency is weekly', async () => {
    const weekly: DigestSubscription = { ...sample, frequency: 'weekly', byweekday: 1 }
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(weekly) })
    await waitFor(() => {
      expect(screen.queryByTestId('digest-weekday')).not.toBeNull()
    })
  })

  it('disables deletion when there is no existing digest subscription', async () => {
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(null) })
    await waitFor(() => {
      expect(screen.getByTestId('digest-delete')).toBeDisabled()
    })
    expect(screen.queryByTestId('digest-next-run')).not.toBeInTheDocument()
  })

  it('populates the clustering checkbox from loaded config', async () => {
    const withClustering: DigestSubscription = { ...sample, clusteringEnabled: true }
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(withClustering) })
    await waitFor(() => {
      expect((screen.getByTestId('digest-clustering-enabled') as HTMLInputElement).checked).toBe(
        true,
      )
    })
  })

  it('calls upsert on save and includes clusteringEnabled', async () => {
    let captured: unknown = null
    server.use(
      http.put('/fb/v1/console/digest-subscription', async ({ request }) => {
        captured = await request.json()
        return HttpResponse.json({ ...sample, clusteringEnabled: true })
      }),
    )
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(sample) })
    await waitFor(() => screen.getByTestId('digest-clustering-enabled'))
    fireEvent.click(screen.getByTestId('digest-clustering-enabled'))
    fireEvent.click(screen.getByTestId('digest-save'))
    await waitFor(() => expect(captured).not.toBeNull())
    expect((captured as { clusteringEnabled?: boolean }).clusteringEnabled).toBe(true)
  })

  it('saves weekly cadence changes with numeric fallbacks and a trimmed prompt', async () => {
    let captured: unknown = null
    server.use(
      http.put('/fb/v1/console/digest-subscription', async ({ request }) => {
        captured = await request.json()
        return HttpResponse.json(sample)
      }),
    )
    const { user } = renderWithProviders(<DigestSubscriptionPage />, {
      queryClient: seeded(sample),
    })
    await waitFor(() => screen.getByTestId('digest-frequency'))

    await user.click(screen.getByTestId('digest-frequency'))
    await user.click(await screen.findByRole('option', { name: '每周' }))
    await user.click(screen.getByTestId('digest-weekday'))
    await user.click(await screen.findByRole('option', { name: '周五' }))
    fireEvent.change(screen.getByTestId('digest-send-hour'), { target: { value: '' } })
    fireEvent.change(screen.getByTestId('digest-llm-min'), { target: { value: '' } })
    fireEvent.change(screen.getByTestId('digest-theme-prompt'), {
      target: { value: '  summarize high-risk themes  ' },
    })
    await user.click(screen.getByTestId('digest-save'))

    await waitFor(() =>
      expect(captured).toMatchObject({
        frequency: 'weekly',
        byweekday: 5,
        sendHour: 0,
        llmMinFeedback: 6,
        themePrompt: 'summarize high-risk themes',
      }),
    )
  })

  it('shows the save spinner while an upsert is pending', async () => {
    let releaseSave: () => void = () => {}
    server.use(
      http.put('/fb/v1/console/digest-subscription', async () => {
        await new Promise<void>((resolve) => {
          releaseSave = resolve
        })
        return HttpResponse.json(sample)
      }),
    )
    const { user } = renderWithProviders(<DigestSubscriptionPage />, {
      queryClient: seeded(sample),
    })
    await waitFor(() => screen.getByTestId('digest-save'))

    const saveButton = screen.getByTestId('digest-save')
    await user.click(saveButton)
    await waitFor(() => expect(saveButton).toBeDisabled())
    expect(saveButton.querySelector('.animate-spin')).toBeInTheDocument()
    releaseSave()
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('保存'))
  })

  it('surfaces save and delete errors', async () => {
    server.use(
      http.put('/fb/v1/console/digest-subscription', () =>
        HttpResponse.json({ message: 'save failed' }, { status: 500 }),
      ),
      http.delete('/fb/v1/console/digest-subscription', () =>
        HttpResponse.json({ message: 'delete failed' }, { status: 500 }),
      ),
    )
    const { user } = renderWithProviders(<DigestSubscriptionPage />, {
      queryClient: seeded(sample),
    })
    await waitFor(() => screen.getByTestId('digest-save'))

    await user.click(screen.getByTestId('digest-save'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('save failed'))
    vi.mocked(toast.error).mockClear()

    await user.click(screen.getByTestId('digest-delete'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('delete failed'))
  })

  it('calls delete mutation on delete click', async () => {
    let deleted = false
    server.use(
      http.delete('/fb/v1/console/digest-subscription', () => {
        deleted = true
        return HttpResponse.json({})
      }),
    )
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(sample) })
    await waitFor(() => screen.getByTestId('digest-delete'))
    fireEvent.click(screen.getByTestId('digest-delete'))
    await waitFor(() => expect(deleted).toBe(true))
  })

  it('shows loading spinner while query is pending', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: qc })
    expect(screen.getByText(/加载中/)).toBeInTheDocument()
  })

  it('updates sendOnEmpty checkbox on click', async () => {
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(sample) })
    await waitFor(() => screen.getByTestId('digest-send-on-empty'))
    const checkbox = screen.getByTestId('digest-send-on-empty') as HTMLInputElement
    expect(checkbox.checked).toBe(false)
    fireEvent.click(checkbox)
    expect(checkbox.checked).toBe(true)
  })

  it('updates enabled checkbox on click', async () => {
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(sample) })
    await waitFor(() => screen.getByTestId('digest-enabled'))
    const checkbox = screen.getByTestId('digest-enabled') as HTMLInputElement
    expect(checkbox.checked).toBe(true)
    fireEvent.click(checkbox)
    expect(checkbox.checked).toBe(false)
  })

  it('updates llmMin input on change', async () => {
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(sample) })
    await waitFor(() => screen.getByTestId('digest-llm-min'))
    const input = screen.getByTestId('digest-llm-min') as HTMLInputElement
    expect(input.value).toBe('6')
    fireEvent.change(input, { target: { value: '10' } })
    expect(input.value).toBe('10')
  })

  it('updates themePrompt input on change', async () => {
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(sample) })
    await waitFor(() => screen.getByTestId('digest-theme-prompt'))
    const input = screen.getByTestId('digest-theme-prompt') as HTMLInputElement
    fireEvent.change(input, { target: { value: 'custom prompt' } })
    expect(input.value).toBe('custom prompt')
  })
})
