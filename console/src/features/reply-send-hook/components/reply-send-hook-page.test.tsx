import { within } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ReplySendHookPage } from '@/features/reply-send-hook/components/reply-send-hook-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

describe('ReplySendHookPage', () => {
  beforeEach(() => {
    vi.mocked(toast.success).mockClear()
    vi.mocked(toast.error).mockClear()
    server.use(
      http.get('/fb/v1/console/reply-send-hook/health', () =>
        HttpResponse.json({
          accepted: '0',
          dead: '0',
          failed: '0',
          pending: '0',
          retryable: '0',
          total: '0',
        }),
      ),
      http.get('/fb/v1/console/reply-send-hook/deliveries', () => HttpResponse.json({ items: [] })),
    )
  })

  it('renders an empty state when no hook is configured', async () => {
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'not configured' }, { status: 409 }),
      ),
    )

    renderWithProviders(<ReplySendHookPage />)

    await waitFor(() => expect(screen.getByText('还没有回复发送 Hook')).toBeInTheDocument())
    expect(screen.getByText('还没有投递健康信号')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /保存/ })).toBeDisabled()
  })

  it('saves a hook and shows the generated secret once', async () => {
    let posted: Record<string, unknown> | null = null
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'not configured' }, { status: 409 }),
      ),
      http.put('/fb/v1/console/reply-send-hook', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({
          id: 'hook-1',
          name: 'Zendesk reply hook',
          enabled: true,
          urlHost: 'hooks.example.test',
          urlFingerprint: 'abc123def456',
          secretOnce: 'generated-secret-123456',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:00:00Z',
        })
      }),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)

    await user.type(screen.getByLabelText('名称'), 'Zendesk reply hook')
    await user.type(screen.getByLabelText('Webhook URL'), 'https://hooks.example.test/replies')
    await user.click(screen.getByRole('button', { name: /保存/ }))

    await waitFor(() =>
      expect(posted).toMatchObject({
        enabled: true,
        name: 'Zendesk reply hook',
        url: 'https://hooks.example.test/replies',
      }),
    )
    expect(await screen.findByText('generated-secret-123456')).toBeInTheDocument()
    expect((await screen.findAllByText('hooks.example.test')).length).toBeGreaterThanOrEqual(1)
  })

  it('copies the generated secret and sample payload', async () => {
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'not configured' }, { status: 409 }),
      ),
      http.put('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({
          id: 'hook-copy',
          name: 'Copy hook',
          enabled: true,
          urlHost: 'hooks.example.test',
          urlFingerprint: 'abc123def456',
          secretOnce: 'generated-secret-copy',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:00:00Z',
        }),
      ),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)

    await user.type(screen.getByLabelText('Webhook URL'), 'https://hooks.example.test/replies')
    await user.click(screen.getByRole('button', { name: /保存/ }))
    expect(await screen.findByText('generated-secret-copy')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '复制' }))
    await waitFor(() => expect(writeText).toHaveBeenLastCalledWith('generated-secret-copy'))
    expect(toast.success).toHaveBeenCalledWith('secret 已复制')

    await user.click(screen.getByRole('button', { name: '复制 payload' }))
    await waitFor(() =>
      expect(writeText).toHaveBeenLastCalledWith(
        expect.stringContaining('"event_type": "reply.send"'),
      ),
    )
    expect(toast.success).toHaveBeenCalledWith('示例 payload 已复制')
  })

  it('shows copy failure toasts for secret and sample payload', async () => {
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'not configured' }, { status: 409 }),
      ),
      http.put('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({
          id: 'hook-copy-failure',
          name: 'Copy hook',
          enabled: true,
          urlHost: 'hooks.example.test',
          urlFingerprint: 'abc123def456',
          secretOnce: 'generated-secret-copy',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:00:00Z',
        }),
      ),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('denied'))

    await user.type(screen.getByLabelText('Webhook URL'), 'https://hooks.example.test/replies')
    await user.click(screen.getByRole('button', { name: /保存/ }))
    expect(await screen.findByText('generated-secret-copy')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '复制' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('复制失败'))

    vi.mocked(toast.error).mockClear()
    await user.click(screen.getByRole('button', { name: '复制 payload' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('复制失败'))
  })

  it('refreshes delivery health and logs after saving a hook', async () => {
    let healthCalls = 0
    let deliveryCalls = 0
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'not configured' }, { status: 409 }),
      ),
      http.get('/fb/v1/console/reply-send-hook/health', () => {
        healthCalls += 1
        return HttpResponse.json({
          accepted: '0',
          dead: '0',
          failed: '0',
          pending: '0',
          retryable: '0',
          total: '0',
        })
      }),
      http.get('/fb/v1/console/reply-send-hook/deliveries', () => {
        deliveryCalls += 1
        return HttpResponse.json({ items: [] })
      }),
      http.put('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({
          id: 'hook-1',
          name: 'Reply hook',
          enabled: true,
          urlHost: 'hooks.example.test',
          urlFingerprint: 'abc123def456',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:00:00Z',
        }),
      ),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)

    await waitFor(() => expect(healthCalls).toBeGreaterThan(0))
    await waitFor(() => expect(deliveryCalls).toBeGreaterThan(0))
    await user.type(screen.getByLabelText('Webhook URL'), 'https://hooks.example.test/replies')
    await user.click(screen.getByRole('button', { name: /保存/ }))

    await waitFor(() => expect(healthCalls).toBeGreaterThan(1))
    await waitFor(() => expect(deliveryCalls).toBeGreaterThan(1))
  })

  it('blocks unsafe hook configuration before submitting', async () => {
    let submitted = false
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'not configured' }, { status: 409 }),
      ),
      http.put('/fb/v1/console/reply-send-hook', () => {
        submitted = true
        return HttpResponse.json({})
      }),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)

    await user.type(screen.getByLabelText('Webhook URL'), 'http://hooks.example.test')
    await user.type(screen.getByLabelText('签名 secret'), 'short')

    expect(
      await screen.findByText('回复发送 Hook 只接受 HTTPS URL；本地测试可使用 loopback HTTP。'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('secret 至少需要 16 个字符；也可以留空让服务端生成或保留当前 secret。'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /保存/ })).toBeDisabled()
    expect(submitted).toBe(false)
  })

  it('rejects credential-bearing HTTPS hook URLs before submitting', async () => {
    let submitted = false
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'not configured' }, { status: 409 }),
      ),
      http.put('/fb/v1/console/reply-send-hook', () => {
        submitted = true
        return HttpResponse.json({})
      }),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)

    const credentials = 'user:pass'
    await user.type(
      screen.getByLabelText('Webhook URL'),
      `https://${credentials}@hooks.example.test/replies`,
    )
    await user.type(screen.getByLabelText('签名 secret'), '1234567890123456')

    expect(await screen.findByText('URL 不能包含 user:pass 这类凭据。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /保存/ })).toBeDisabled()
    expect(submitted).toBe(false)
  })

  it('clears a generated one-time secret after saving a user-provided secret', async () => {
    let requestNo = 0
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'not configured' }, { status: 409 }),
      ),
      http.put('/fb/v1/console/reply-send-hook', async ({ request }) => {
        requestNo++
        const body = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({
          id: 'hook-1',
          name: 'Reply hook',
          enabled: true,
          urlHost: new URL(String(body.url)).hostname,
          urlFingerprint: `fp-${requestNo}`,
          secretOnce: body.secret ? undefined : 'generated-secret-123456',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:00:00Z',
        })
      }),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)

    await user.type(screen.getByLabelText('名称'), 'Reply hook')
    await user.type(screen.getByLabelText('Webhook URL'), 'http://127.0.0.1:9393/replies')
    await user.click(screen.getByRole('button', { name: /保存/ }))
    expect(await screen.findByText('generated-secret-123456')).toBeInTheDocument()

    await user.type(screen.getByLabelText('Webhook URL'), 'https://hooks.example.test/replies')
    await user.type(screen.getByLabelText('签名 secret'), '1234567890123456')
    await user.click(screen.getByRole('button', { name: /保存/ }))

    await waitFor(() =>
      expect(screen.queryByText('generated-secret-123456')).not.toBeInTheDocument(),
    )
  })

  it('allows loopback HTTP hook configuration for local receiver tests', async () => {
    let posted: Record<string, unknown> | null = null
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'not configured' }, { status: 409 }),
      ),
      http.put('/fb/v1/console/reply-send-hook', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({
          id: 'hook-local',
          name: 'Local receiver',
          enabled: true,
          urlHost: '127.0.0.1',
          urlFingerprint: 'local123',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:00:00Z',
        })
      }),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)

    await user.type(screen.getByLabelText('名称'), 'Local receiver')
    await user.type(screen.getByLabelText('Webhook URL'), 'http://127.0.0.1:9393/replies')
    await user.click(screen.getByRole('button', { name: /保存/ }))

    await waitFor(() =>
      expect(posted).toMatchObject({
        enabled: true,
        name: 'Local receiver',
        url: 'http://127.0.0.1:9393/replies',
      }),
    )
    expect((await screen.findAllByText('127.0.0.1')).length).toBeGreaterThanOrEqual(1)
  })

  it('documents the delivery contract and security checks', async () => {
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({
          id: 'hook-1',
          name: 'Reply hook',
          enabled: true,
          urlHost: 'hooks.example.test',
          urlFingerprint: 'abcdef1234567890',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:00:00Z',
        }),
      ),
    )

    renderWithProviders(<ReplySendHookPage />)

    expect(await screen.findByText('投递契约')).toBeInTheDocument()
    expect(screen.getByText('X-Attune-Signature')).toBeInTheDocument()
    expect(screen.getByText('X-Attune-Timestamp')).toBeInTheDocument()
    expect(screen.getByText('X-Attune-Idempotency-Key')).toBeInTheDocument()
    expect(screen.getByText('安全检查')).toBeInTheDocument()
    expect(screen.getAllByText('最近投递').length).toBeGreaterThanOrEqual(1)
    const payload = screen.getByLabelText('回复发送 Hook 示例 payload') as HTMLTextAreaElement
    expect(payload.value).toContain('"event_type": "reply.send"')
  })

  it('renders a disabled hook returned by the API after reload', async () => {
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({
          id: 'hook-disabled',
          name: 'Paused reply hook',
          enabled: false,
          urlHost: 'hooks.example.test',
          urlFingerprint: 'abcdef1234567890',
          disabledAt: '2026-07-03T10:10:00Z',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:10:00Z',
        }),
      ),
    )

    renderWithProviders(<ReplySendHookPage />)

    expect(await screen.findByText('Paused reply hook')).toBeInTheDocument()
    expect(screen.getAllByText('停用').length).toBeGreaterThanOrEqual(1)
    for (const button of screen.getAllByRole('button', { name: '测试 Hook' })) {
      expect(button).toBeDisabled()
    }
    expect(screen.getByRole('button', { name: /停用 Hook/ })).toBeDisabled()
  })

  it('tests the hook and redelivers a failed delivery', async () => {
    let tested = false
    let redelivered = false
    let resolveRedelivery: (() => void) | undefined
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({
          id: 'hook-1',
          name: 'Reply hook',
          enabled: true,
          urlHost: 'hooks.example.test',
          urlFingerprint: 'abcdef1234567890',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:00:00Z',
        }),
      ),
      http.get('/fb/v1/console/reply-send-hook/deliveries', () =>
        HttpResponse.json({
          items: [
            {
              id: 'attempt-1',
              hookId: 'hook-1',
              hookHost: 'hooks.example.test',
              hookFingerprint: 'abcdef1234567890',
              eventType: 'reply.test',
              status: 'failed',
              idempotencyKey: 'reply_test_123456',
              httpStatus: 500,
              attempts: 1,
              maxAttempts: 8,
              error: 'receiver returned 500',
              requestedByType: 'admin',
              requestedBy: 'admin-1',
              requestedAt: '2026-07-03T10:00:00Z',
              createdAt: '2026-07-03T10:00:00Z',
              updatedAt: '2026-07-03T10:00:00Z',
              retryable: true,
            },
            {
              id: 'attempt-2',
              hookId: 'hook-1',
              hookHost: 'hooks.example.test',
              hookFingerprint: 'abcdef1234567890',
              eventType: 'reply.test',
              status: 'failed',
              idempotencyKey: 'reply_test_654321',
              httpStatus: 502,
              attempts: 1,
              maxAttempts: 8,
              error: 'receiver returned 502',
              requestedByType: 'admin',
              requestedBy: 'admin-1',
              requestedAt: '2026-07-03T09:59:00Z',
              createdAt: '2026-07-03T09:59:00Z',
              updatedAt: '2026-07-03T09:59:00Z',
              retryable: true,
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/reply-send-hook/health', () =>
        HttpResponse.json({
          accepted: '0',
          dead: '0',
          failed: '2',
          pending: '0',
          retryable: '2',
          total: '2',
          latestProblem: {
            id: 'attempt-1',
            hookId: 'hook-1',
            hookHost: 'hooks.example.test',
            hookFingerprint: 'abcdef1234567890',
            eventType: 'reply.test',
            status: 'failed',
            idempotencyKey: 'reply_test_123456',
            httpStatus: 500,
            attempts: 1,
            maxAttempts: 8,
            error: 'receiver returned 500',
            requestedByType: 'admin',
            requestedBy: 'admin-1',
            requestedAt: '2026-07-03T10:00:00Z',
            createdAt: '2026-07-03T10:00:00Z',
            updatedAt: '2026-07-03T10:00:00Z',
            retryable: true,
          },
        }),
      ),
      http.post('/fb/v1/console/reply-send-hook/test', () => {
        tested = true
        return HttpResponse.json({
          id: 'attempt-test',
          hookId: 'hook-1',
          hookHost: 'hooks.example.test',
          hookFingerprint: 'abcdef1234567890',
          eventType: 'reply.test',
          status: 'accepted',
          idempotencyKey: 'reply_test_accepted',
          httpStatus: 204,
          attempts: 1,
          maxAttempts: 8,
          requestedByType: 'admin',
          requestedBy: 'admin-1',
          requestedAt: '2026-07-03T10:01:00Z',
          createdAt: '2026-07-03T10:01:00Z',
          updatedAt: '2026-07-03T10:01:00Z',
          retryable: false,
        })
      }),
      http.post('/fb/v1/console/reply-send-hook/deliveries/attempt-1/redeliver', () => {
        return new Promise((resolve) => {
          resolveRedelivery = () => {
            redelivered = true
            resolve(
              HttpResponse.json({
                id: 'attempt-1',
                hookId: 'hook-1',
                hookHost: 'hooks.example.test',
                hookFingerprint: 'abcdef1234567890',
                eventType: 'reply.test',
                status: 'accepted',
                idempotencyKey: 'reply_test_123456',
                httpStatus: 204,
                attempts: 2,
                maxAttempts: 8,
                requestedByType: 'admin',
                requestedBy: 'admin-1',
                requestedAt: '2026-07-03T10:02:00Z',
                createdAt: '2026-07-03T10:00:00Z',
                updatedAt: '2026-07-03T10:02:00Z',
                retryable: false,
              }),
            )
          }
        })
      }),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)

    expect(await screen.findByText('receiver returned 500')).toBeInTheDocument()
    const health = screen.getByTestId('reply-send-hook-health')
    expect(within(health).getByText('最近投递需要处理')).toBeInTheDocument()
    expect(
      within(health).getByText('最近异常 reply.test · 失败 · receiver returned 500'),
    ).toBeInTheDocument()
    expect(within(health).getByText('可重放')).toBeInTheDocument()
    expect(screen.getByText('reply_test_123456')).toBeInTheDocument()
    expect(screen.queryByText('下次重试')).not.toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: '测试 Hook' })[0])
    await waitFor(() => expect(tested).toBe(true))
    const firstRedeliver = screen.getByRole('button', { name: '重放投递 attempt-1' })
    const secondRedeliver = screen.getByRole('button', { name: '重放投递 attempt-2' })
    await user.click(firstRedeliver)
    await waitFor(() => expect(firstRedeliver.querySelector('.animate-spin')).toBeTruthy())
    expect(secondRedeliver.querySelector('.animate-spin')).toBeNull()
    resolveRedelivery?.()
    await waitFor(() => expect(redelivered).toBe(true))
  })

  it('surfaces failed hook tests and failed redelivery attempts', async () => {
    let tested = false
    let redelivered = false
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({
          id: 'hook-1',
          name: 'Reply hook',
          enabled: true,
          urlHost: 'hooks.example.test',
          urlFingerprint: 'abcdef1234567890',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:00:00Z',
        }),
      ),
      http.get('/fb/v1/console/reply-send-hook/deliveries', () =>
        HttpResponse.json({
          items: [
            {
              id: 'attempt-retry',
              hookId: 'hook-1',
              hookHost: 'hooks.example.test',
              hookFingerprint: 'abcdef1234567890',
              eventType: 'reply.send',
              status: 'failed',
              idempotencyKey: 'reply_send_retry',
              httpStatus: 503,
              attempts: 2,
              maxAttempts: 8,
              error: 'receiver unavailable',
              requestedByType: 'admin',
              requestedBy: 'admin-1',
              requestedAt: '2026-07-03T10:00:00Z',
              nextRetryAt: '2026-07-03T10:05:00Z',
              createdAt: '2026-07-03T10:00:00Z',
              updatedAt: '2026-07-03T10:01:00Z',
              retryable: true,
            },
          ],
        }),
      ),
      http.post('/fb/v1/console/reply-send-hook/test', () => {
        tested = true
        return HttpResponse.json({
          id: 'attempt-test-failed',
          hookId: 'hook-1',
          hookHost: 'hooks.example.test',
          hookFingerprint: 'abcdef1234567890',
          eventType: 'reply.test',
          status: 'failed',
          idempotencyKey: 'reply_test_failed',
          httpStatus: 500,
          attempts: 1,
          maxAttempts: 8,
          error: 'test failed',
          requestedByType: 'admin',
          requestedBy: 'admin-1',
          requestedAt: '2026-07-03T10:01:00Z',
          createdAt: '2026-07-03T10:01:00Z',
          updatedAt: '2026-07-03T10:01:00Z',
          retryable: true,
        })
      }),
      http.post('/fb/v1/console/reply-send-hook/deliveries/attempt-retry/redeliver', () => {
        redelivered = true
        return HttpResponse.json({
          id: 'attempt-retry',
          hookId: 'hook-1',
          hookHost: 'hooks.example.test',
          hookFingerprint: 'abcdef1234567890',
          eventType: 'reply.send',
          status: 'failed',
          idempotencyKey: 'reply_send_retry',
          httpStatus: 503,
          attempts: 3,
          maxAttempts: 8,
          error: 'still unavailable',
          requestedByType: 'admin',
          requestedBy: 'admin-1',
          requestedAt: '2026-07-03T10:05:00Z',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:05:00Z',
          retryable: true,
        })
      }),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)

    expect(await screen.findByText('receiver unavailable')).toBeInTheDocument()
    expect(screen.getByText(/下次 07\/03/)).toBeInTheDocument()

    await user.click(screen.getAllByRole('button', { name: '测试 Hook' })[0])
    await waitFor(() => expect(tested).toBe(true))
    expect(toast.error).toHaveBeenCalledWith('测试投递失败，请查看最近投递')

    vi.mocked(toast.error).mockClear()
    await user.click(screen.getByRole('button', { name: /重放投递 attempt-.*etry/ }))
    await waitFor(() => expect(redelivered).toBe(true))
    expect(toast.error).toHaveBeenCalledWith('重放仍失败，请查看最近投递')
  })

  it('shows the current hook and disables it', async () => {
    let deleted = false
    server.use(
      http.get('/fb/v1/console/reply-send-hook', () =>
        HttpResponse.json({
          id: 'hook-1',
          name: 'Reply hook',
          enabled: true,
          urlHost: 'hooks.example.test',
          urlFingerprint: 'abcdef1234567890',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:00:00Z',
        }),
      ),
      http.delete('/fb/v1/console/reply-send-hook', () => {
        deleted = true
        return HttpResponse.json({
          id: 'hook-1',
          name: 'Reply hook',
          enabled: false,
          urlHost: 'hooks.example.test',
          urlFingerprint: 'abcdef1234567890',
          disabledAt: '2026-07-03T10:10:00Z',
          createdAt: '2026-07-03T10:00:00Z',
          updatedAt: '2026-07-03T10:10:00Z',
        })
      }),
    )

    const { user } = renderWithProviders(<ReplySendHookPage />)

    await waitFor(() =>
      expect(screen.getAllByText('hooks.example.test').length).toBeGreaterThanOrEqual(1),
    )
    await user.click(screen.getByRole('button', { name: /停用 Hook/ }))
    expect(await screen.findByRole('alertdialog', { name: '停用回复发送 Hook？' })).toBeVisible()
    await user.click(screen.getByRole('button', { name: '确认停用' }))
    await waitFor(() => expect(deleted).toBe(true))
    expect((await screen.findAllByText('停用')).length).toBeGreaterThanOrEqual(1)
  })
})
