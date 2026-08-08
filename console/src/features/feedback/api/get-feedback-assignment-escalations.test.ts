import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import {
  feedbackAssignmentEscalationsQuery,
  feedbackAssignmentEscalationsQueryKey,
} from '@/features/feedback/api/get-feedback-assignment-escalations'
import { server } from '@/testing/mocks/server'

describe('feedbackAssignmentEscalationsQuery', () => {
  it('GETs /feedback/assignment/escalations with a bounded limit', async () => {
    let capturedLimit = ''
    server.use(
      http.get('/fb/v1/console/feedback/assignment/escalations', ({ request }) => {
        capturedLimit = new URL(request.url).searchParams.get('limit') ?? ''
        return HttpResponse.json({
          generatedAt: '2026-08-01T00:00:00Z',
          overdueCount: '1',
          dueSoonCount: '1',
          missingOwnerCount: '1',
          missingSlaCount: '1',
          items: [
            {
              feedbackId: '42',
              title: 'Enterprise login regression',
              source: 'web',
              type: 'bug',
              isUrgent: true,
              createdAt: '2026-07-31T00:00:00Z',
              assignment: {
                feedbackId: '42',
                slaDueAt: '2026-07-31T20:00:00Z',
                slaStatus: 'overdue',
                note: 'Escalate before renewal.',
              },
              escalationReasons: ['overdue', 'missing_owner'],
              hoursUntilDue: -4,
              priority: 'critical',
              accountContext: {
                accountKey: 'acct:acme',
                accountDisplay: 'Acme Corp',
                source: 'source_meta',
              },
            },
          ],
        })
      }),
    )

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const data = await qc.fetchQuery(feedbackAssignmentEscalationsQuery(7))

    expect(capturedLimit).toBe('7')
    expect(data.items[0]?.priority).toBe('critical')
    expect(data.items[0]?.accountContext?.accountKey).toBe('acct:acme')
    expect(data.items[0]?.escalationReasons).toEqual(['overdue', 'missing_owner'])
    expect(feedbackAssignmentEscalationsQueryKey).toEqual([
      'console',
      'feedback',
      'assignment-escalations',
    ])
  })
})
