import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { BatchRequestNotificationDialog } from '@/features/feedback/components/batch-request-notification-dialog'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('BatchRequestNotificationDialog', () => {
  it('previews and publishes request notifications with per-request failures', async () => {
    const calls: Array<{ path: string; body: unknown }> = []
    server.use(
      http.post('/fb/v1/console/request-notifications:batch-preview', async ({ request }) => {
        calls.push({ path: new URL(request.url).pathname, body: await request.json() })
        return HttpResponse.json({
          totalMatched: 2,
          eligibleRecipients: 3,
          excludedRecipients: 1,
          items: [
            {
              requestId: '11111111-1111-1111-1111-111111111111',
              eligibleRecipients: 3,
              excludedRecipients: 1,
            },
          ],
          failed: [{ requestId: 'bad-request', code: 'validation', message: 'invalid request id' }],
        })
      }),
      http.post('/fb/v1/console/request-notifications:batch-publish', async ({ request }) => {
        calls.push({ path: new URL(request.url).pathname, body: await request.json() })
        return HttpResponse.json({
          totalMatched: 2,
          succeeded: 1,
          skipped: 0,
          events: [{ id: 'event-1', status: 'pending' }],
          failed: [{ requestId: 'bad-request', code: 'validation', message: 'invalid request id' }],
        })
      }),
    )
    const onCompleted = vi.fn()
    const { user } = renderWithProviders(
      <BatchRequestNotificationDialog
        open
        selectedFeedbackCount={2}
        onCancel={vi.fn()}
        onCompleted={onCompleted}
      />,
    )

    await user.type(
      screen.getByTestId('feedback-batch-notify-request-ids'),
      '11111111-1111-1111-1111-111111111111\nbad-request',
    )
    await user.type(screen.getByLabelText('标题'), 'Shipped')
    await user.type(screen.getByLabelText('正文'), 'CSV export is now available.')
    await user.click(screen.getByRole('button', { name: '预览 audience' }))

    await screen.findByText('2 个需求 · 3 位可发送 · 1 位已排除 · 1 个失败')
    expect(screen.getByText('bad-request')).toBeVisible()

    await user.click(screen.getByRole('button', { name: '发布并通知' }))
    await waitFor(() => expect(onCompleted).toHaveBeenCalledTimes(1))
    expect(onCompleted).toHaveBeenCalledWith({
      action: 'notify',
      total: 2,
      succeeded: 1,
      skipped: 0,
      failed: [
        {
          feedbackId: 'bad-request',
          code: 'validation',
          message: 'invalid request id',
        },
      ],
    })
    expect(calls.map((call) => call.path)).toEqual([
      '/fb/v1/console/request-notifications:batch-preview',
      '/fb/v1/console/request-notifications:batch-publish',
    ])
    expect(calls[0].body).toMatchObject({
      updates: [
        { requestId: '11111111-1111-1111-1111-111111111111' },
        { requestId: 'bad-request' },
      ],
      channels: ['REQUEST_NOTIFICATION_CHANNEL_EMAIL'],
    })
    expect(calls[1].body).toMatchObject({ confirmLargeAudience: true })
  })
})
