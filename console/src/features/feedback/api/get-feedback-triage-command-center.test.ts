import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import {
  feedbackTriageCommandCenterQuery,
  feedbackTriageCommandCenterQueryKey,
} from '@/features/feedback/api/get-feedback-triage-command-center'
import { server } from '@/testing/mocks/server'

describe('feedbackTriageCommandCenterQuery', () => {
  it('GETs /feedback/triage-command-center with a stable query key', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/triage-command-center', () =>
        HttpResponse.json({
          generatedAt: '2026-08-01T00:00:00Z',
          openCount: '7',
          activeCount: '2',
          closedCount: '5',
          urgentOpenCount: '1',
          terminalFailureCount: '1',
          identityDebtCount: '3',
          overdueCount: '2',
          dueSoonCount: '1',
          lanes: [
            {
              key: 'urgent_open',
              label: 'Urgent open feedback',
              ownerLane: 'support_triage',
              severity: 'critical',
              slaHours: 24,
              count: '1',
              overdueCount: '1',
              dueSoonCount: '0',
              oldestCreatedAt: '2026-07-31T00:00:00Z',
              nextDeadlineAt: '2026-08-01T00:00:00Z',
              recommendedAction: 'Contact the customer and assign the owner.',
              filterQuery: 'urgent=true',
              sampleFeedbackIds: ['101'],
            },
          ],
        }),
      ),
    )

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const data = await qc.fetchQuery(feedbackTriageCommandCenterQuery())

    expect(data.openCount).toBe('7')
    expect(data.lanes[0]?.ownerLane).toBe('support_triage')
    expect(feedbackTriageCommandCenterQueryKey).toEqual([
      'console',
      'feedback',
      'triage-command-center',
    ])
  })
})
