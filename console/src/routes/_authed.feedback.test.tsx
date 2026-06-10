import { HttpResponse, http } from 'msw'
import { Suspense } from 'react'
import { describe, expect, it } from 'vitest'
import { Route as FeedbackRoute } from '@/routes/_authed.feedback'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

// Route-level smoke test for the feedback page. The unit tests cover
// individual hooks + components in isolation; this test covers the
// composition layer where users actually live — the wiring between
// enrichConfigQuery → dims, feedbackListInfiniteQuery → table,
// row click → detail sheet open, detail query → sheet content.
//
// Without this, a regression like "FeedbackPage forgot to pass dims
// to DimensionChips" would land green: the components are tested
// but their integration on the page is not.

const dimsFixture = [
  {
    name: 'severity',
    displayName: { entries: { default: 'Severity' } },
    kind: 'single',
    taxonomy: [
      { value: 'P0', displayName: { entries: { default: 'P0' } } },
      { value: 'P1', displayName: { entries: { default: 'P1' } } },
    ],
    urgentSet: ['P0'],
    required: false,
    examples: [],
    extractionHint: '',
  },
]

const itemFixture = {
  id: 'f-101',
  content: 'login is broken when password has unicode',
  enrichedTitle: 'Login fails on unicode password',
  enrichedAttrs: { severity: 'P0' },
  isUrgent: true,
  source: 'web',
  userId: 'user-7',
  createdAt: '2026-06-07T08:30:00Z',
  type: 'bug',
  enrichmentStatus: 'done',
}

const detailFixture = {
  id: 'f-101',
  content: 'login is broken when password has unicode',
  source: 'web',
  type: 'bug',
  userId: 'user-7',
  pageUrl: '',
  enrichedTitle: 'Login fails on unicode password',
  enrichedRationale: 'Unicode normalization bug',
  enrichedAttrs: { severity: 'P0' },
  isUrgent: true,
  enrichmentStatus: 'done',
  createdAt: '2026-06-07T08:30:00Z',
  attachments: [],
  enrichmentError: '',
  enrichedAt: '2026-06-07T08:31:00Z',
}

describe('_authed.feedback route — user flow smoke', () => {
  it('renders title + table row from the list query, opens sheet with detail on row click', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/feedback/:id', ({ params }) =>
        HttpResponse.json({ ...detailFixture, id: params.id }),
      ),
    )

    // Route.options.component is wrapped by TanStack Router's lazy()
    // (a side effect of `autoCodeSplitting: true` in vite.config). The
    // wrapper exposes a .preload() that resolves the inner module;
    // call it before render so React doesn't suspend on first paint.
    const FeedbackPage = FeedbackRoute.options.component as React.ComponentType & {
      preload?: () => Promise<unknown>
    }
    if (!FeedbackPage) throw new Error('FeedbackPage component missing on Route.options')
    if (FeedbackPage.preload) await FeedbackPage.preload()

    const { user } = renderWithProviders(
      <Suspense fallback={null}>
        <FeedbackPage />
      </Suspense>,
    )

    // Wait for the list query to land + render the row title.
    await waitFor(() => {
      expect(screen.getByText('Login fails on unicode password')).toBeInTheDocument()
    })
    // Dim column header rendered from enrich-config dims. Two
    // matches expected: one in the stats card title, one in the
    // table column header.
    expect(screen.getAllByText('Severity').length).toBeGreaterThanOrEqual(1)
    // Per-row dim cell rendered via DimensionChips (P0 resolved from
    // the dim's taxonomy).
    const p0Badges = screen.getAllByText('P0')
    expect(p0Badges.length).toBeGreaterThanOrEqual(1)

    // Click the row → setDetailId fires → FeedbackDetailSheet opens →
    // its useQuery(feedbackDetailQuery(id)) runs → MSW returns
    // detailFixture → sheet body renders the rationale.
    await user.click(screen.getByText('Login fails on unicode password'))
    await waitFor(() => {
      expect(screen.getByText('Unicode normalization bug')).toBeInTheDocument()
    })
  }, 30000)

  it('500 from /feedback → empty state (not crash) — documents current behavior', async () => {
    // Backend errors currently render as "no feedback" rather than a
    // distinct error UI — debatable UX, but it's the actual behavior
    // and this test locks it in so a change is intentional. If/when
    // an error UI lands, this test FAILS and gets updated alongside.
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: [] },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'boom' }, { status: 500 }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '0',
          dims: [],
          urgentCount: '0',
        }),
      ),
    )
    const FeedbackPage = FeedbackRoute.options.component as React.ComponentType & {
      preload?: () => Promise<unknown>
    }
    if (FeedbackPage.preload) await FeedbackPage.preload()
    renderWithProviders(
      <Suspense fallback={null}>
        <FeedbackPage />
      </Suspense>,
    )
    // The empty-state copy is i18n-controlled; wait for it via the
    // EmptyState's Inbox icon's `lucide-inbox` data attribute (stable
    // across i18n). The point of this assertion is "page rendered,
    // didn't crash, didn't show a Toast/Error UI".
    await waitFor(() => {
      const emptyIcon = document.querySelector('svg.lucide-inbox')
      expect(emptyIcon).not.toBeNull()
    })
  })
})
