import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'
import type { DigestSubscription } from '@/proto/attune/v1/digest_subscription'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { DigestSubscriptionPage } from './digest-subscription-page'

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
    expect(screen.queryByTestId('digest-weekday')).toBeNull()
  })

  it('reveals the weekday selector when frequency is weekly', async () => {
    const weekly: DigestSubscription = { ...sample, frequency: 'weekly', byweekday: 1 }
    renderWithProviders(<DigestSubscriptionPage />, { queryClient: seeded(weekly) })
    await waitFor(() => {
      expect(screen.queryByTestId('digest-weekday')).not.toBeNull()
    })
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
})
