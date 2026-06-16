// SPDX-License-Identifier: Apache-2.0

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, renderHook, screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { Can, useCan } from '@/features/session/components/auth/can'
import { server } from '@/testing/mocks/server'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

function mockMeResponse(role: string) {
  server.use(
    http.get('/fb/v1/console/me', () =>
      HttpResponse.json({
        tenant: { id: 't1', name: 'Test', slug: 'test', locale: 'zh-CN', timezone: 'UTC' },
        user: { openId: 'user-1', name: 'Test User', role },
        csrfToken: 'test-token',
      }),
    ),
  )
}

describe('Can component', () => {
  it('renders children when admin has permission', async () => {
    mockMeResponse('admin')
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    render(
      <QueryClientProvider client={queryClient}>
        <Can permission="settings:members:invite">
          <button type="button">Invite</button>
        </Can>
      </QueryClientProvider>,
    )

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Invite' })).toBeInTheDocument()
    })
  })

  it('renders fallback when viewer lacks permission', async () => {
    mockMeResponse('viewer')
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    render(
      <QueryClientProvider client={queryClient}>
        <Can permission="settings:members:invite" fallback={<span>No access</span>}>
          <button type="button">Invite</button>
        </Can>
      </QueryClientProvider>,
    )

    await waitFor(() => {
      expect(screen.getByText('No access')).toBeInTheDocument()
    })
    expect(screen.queryByRole('button', { name: 'Invite' })).not.toBeInTheDocument()
  })

  it('renders nothing when no fallback and permission denied', async () => {
    mockMeResponse('viewer')
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    render(
      <QueryClientProvider client={queryClient}>
        <Can permission="settings:members:invite">
          <button type="button">Invite</button>
        </Can>
      </QueryClientProvider>,
    )

    await waitFor(() => {
      expect(screen.queryByRole('button')).not.toBeInTheDocument()
    })
  })

  it('member can view feedback but not batch delete', async () => {
    mockMeResponse('member')
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    render(
      <QueryClientProvider client={queryClient}>
        <Can permission="feedback:view">
          <span data-testid="view">Can View</span>
        </Can>
        <Can permission="feedback:batch_delete">
          <span data-testid="batch">Can Batch Delete</span>
        </Can>
      </QueryClientProvider>,
    )

    await waitFor(() => {
      expect(screen.getByTestId('view')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('batch')).not.toBeInTheDocument()
  })
})

describe('useCan hook', () => {
  it('returns true for admin with any permission', async () => {
    mockMeResponse('admin')
    const { result } = renderHook(() => useCan('settings:members:invite'), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current).toBe(true)
    })
  })

  it('returns false for viewer with edit permission', async () => {
    mockMeResponse('viewer')
    const { result } = renderHook(() => useCan('feedback:edit'), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current).toBe(false)
    })
  })

  it('returns true for member with feedback:view', async () => {
    mockMeResponse('member')
    const { result } = renderHook(() => useCan('feedback:view'), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current).toBe(true)
    })
  })
})
