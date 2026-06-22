import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useJobStatus } from '@/features/feedback/hooks/use-job-status'
import { server } from '@/testing/mocks/server'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useJobStatus', () => {
  it('returns null when jobId is null', async () => {
    const { result } = renderHook(() => useJobStatus(null), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current.isFetching).toBe(false)
    })

    expect(result.current.data).toBeUndefined()
  })

  it('fetches job status when jobId is provided', async () => {
    server.use(
      http.get('/fb/v1/console/jobs/:jobId', ({ params }) =>
        HttpResponse.json({
          jobId: params.jobId,
          status: 'running',
          total: 100,
          progress: 50,
        }),
      ),
    )

    const { result } = renderHook(() => useJobStatus('job-123'), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data).toEqual({
      jobId: 'job-123',
      status: 'running',
      total: 100,
      progress: 50,
    })
  })

  it('returns completed job status', async () => {
    server.use(
      http.get('/fb/v1/console/jobs/:jobId', () =>
        HttpResponse.json({
          jobId: 'job-done',
          status: 'completed',
          total: 100,
          progress: 100,
          result: { succeeded: 100 },
        }),
      ),
    )

    const { result } = renderHook(() => useJobStatus('job-done'), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data?.status).toBe('completed')
    expect(result.current.data?.result).toEqual({ succeeded: 100 })
  })

  it('returns failed job status with error', async () => {
    server.use(
      http.get('/fb/v1/console/jobs/:jobId', () =>
        HttpResponse.json({
          jobId: 'job-fail',
          status: 'failed',
          total: 10,
          progress: 3,
          error: 'Something went wrong',
        }),
      ),
    )

    const { result } = renderHook(() => useJobStatus('job-fail'), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data?.status).toBe('failed')
    expect(result.current.data?.error).toBe('Something went wrong')
  })

  it('does not fetch when enabled is false', async () => {
    const { result } = renderHook(() => useJobStatus('job-123', { enabled: false }), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current.isFetching).toBe(false)
    })

    expect(result.current.data).toBeUndefined()
  })

  it('returns queued job status', async () => {
    server.use(
      http.get('/fb/v1/console/jobs/:jobId', () =>
        HttpResponse.json({
          jobId: 'job-queued',
          status: 'queued',
          total: 50,
          progress: 0,
        }),
      ),
    )

    const { result } = renderHook(() => useJobStatus('job-queued'), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data?.status).toBe('queued')
  })
})
