import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, userEvent, waitFor } from '@/testing/test-utils'
import { SuggestedValuesPanel } from './suggested-values-panel'

const SUGGESTIONS_URL = '/fb/v1/console/enrich-config/eval-suggestions:analyze'
const PROMOTE_URL = '/fb/v1/console/enrich-config/promote'

function suggestionsResponse() {
  return {
    coverage: { modules: 0.3333 },
    candidates: [
      { dim: 'modules', value: 'checkout', count: 7, confidence: 1, coverageImpact: 0.33 },
      { dim: 'modules', value: 'billing', count: 5, confidence: 0.71, coverageImpact: 0.24 },
    ],
    recommendations: [],
  }
}

describe('SuggestedValuesPanel', () => {
  it('gates the eval behind an explicit Analyze click', async () => {
    let called = false
    server.use(
      http.post(SUGGESTIONS_URL, () => {
        called = true
        return HttpResponse.json(suggestionsResponse())
      }),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    // Not fetched on mount — the eval is expensive.
    expect(called).toBe(false)
    expect(screen.getByTestId('analyze-suggestions')).toBeInTheDocument()
    expect(screen.queryByTestId('suggestions-table')).not.toBeInTheDocument()
  })

  it('renders candidates after Analyze with count/confidence/impact', async () => {
    server.use(http.post(SUGGESTIONS_URL, () => HttpResponse.json(suggestionsResponse())))
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))

    await waitFor(() => expect(screen.getByTestId('suggestions-table')).toBeInTheDocument())
    expect(screen.getByTestId('suggestion-row-checkout')).toBeInTheDocument()
    expect(screen.getByTestId('suggestion-row-billing')).toBeInTheDocument()
    // confidence 1 → 100%, impact 0.33 → +33%
    expect(screen.getByText('100%')).toBeInTheDocument()
    expect(screen.getByText('+33%')).toBeInTheDocument()
  })

  it('renders <1% for a nonzero confidence/impact that rounds to zero', async () => {
    server.use(
      http.post(SUGGESTIONS_URL, () =>
        HttpResponse.json({
          coverage: { modules: 0.999 },
          candidates: [
            { dim: 'modules', value: 'rare', count: 1, confidence: 0.003, coverageImpact: 0.002 },
          ],
          recommendations: [],
        }),
      ),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)
    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('suggestion-row-rare')).toBeInTheDocument())
    const row = screen.getByTestId('suggestion-row-rare')
    expect(row).toHaveTextContent('<1%') // confidence 0.3% → <1%
    expect(row).toHaveTextContent('+<1%') // impact 0.2% → +<1%
  })

  it('promotes only once when the button is double-clicked', async () => {
    let promoteCalls = 0
    server.use(
      http.post(SUGGESTIONS_URL, () => HttpResponse.json(suggestionsResponse())),
      http.post(PROMOTE_URL, async () => {
        promoteCalls += 1
        await new Promise((r) => setTimeout(r, 50))
        return HttpResponse.json({ dimension: { name: 'modules', taxonomy: [] } })
      }),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)
    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('promote-checkout')).toBeInTheDocument())

    const btn = screen.getByTestId('promote-checkout')
    // fire two clicks back-to-back before the request resolves
    btn.click()
    btn.click()
    await new Promise((r) => setTimeout(r, 150))
    expect(promoteCalls).toBe(1)
  })

  it('promotes a candidate via POST /promote', async () => {
    let promoted: { dimensionName: string; value: string } | null = null
    server.use(
      http.post(SUGGESTIONS_URL, () => HttpResponse.json(suggestionsResponse())),
      http.post(PROMOTE_URL, async ({ request }) => {
        promoted = (await request.json()) as { dimensionName: string; value: string }
        return HttpResponse.json({ dimension: { name: 'modules', taxonomy: [] } })
      }),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('promote-checkout')).toBeInTheDocument())
    await userEvent.click(screen.getByTestId('promote-checkout'))

    await waitFor(() => expect(promoted).not.toBeNull())
    expect(promoted).toMatchObject({ dimensionName: 'modules', value: 'checkout' })
  })

  it('drops the promoted candidate without re-running the eval analysis', async () => {
    let analyzeCalls = 0
    server.use(
      http.post(SUGGESTIONS_URL, () => {
        analyzeCalls += 1
        return HttpResponse.json(suggestionsResponse())
      }),
      http.post(PROMOTE_URL, () =>
        HttpResponse.json({ dimension: { name: 'modules', taxonomy: [] } }),
      ),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('promote-checkout')).toBeInTheDocument())
    expect(analyzeCalls).toBe(1)

    await userEvent.click(screen.getByTestId('promote-checkout'))

    // checkout drops from the list, billing stays — via cache update, not a refetch.
    await waitFor(() =>
      expect(screen.queryByTestId('suggestion-row-checkout')).not.toBeInTheDocument(),
    )
    expect(screen.getByTestId('suggestion-row-billing')).toBeInTheDocument()
    // crucially, the expensive eval was NOT re-run.
    expect(analyzeCalls).toBe(1)
  })

  it('keeps the candidate and shows a toast when promote fails (e.g. 409)', async () => {
    server.use(
      http.post(SUGGESTIONS_URL, () => HttpResponse.json(suggestionsResponse())),
      http.post(PROMOTE_URL, () =>
        HttpResponse.json({ code: 'VALIDATION', message: 'value already exists' }, { status: 409 }),
      ),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('promote-checkout')).toBeInTheDocument())
    await userEvent.click(screen.getByTestId('promote-checkout'))

    // Row stays (no optimistic removal), and the promote button is usable again.
    await waitFor(() => expect(screen.getByTestId('promote-checkout')).toBeEnabled())
    expect(screen.getByTestId('suggestion-row-checkout')).toBeInTheDocument()
  })

  it('shows a read-only hint and no Analyze button when canEdit is false', async () => {
    let called = false
    server.use(
      http.post(SUGGESTIONS_URL, () => {
        called = true
        return HttpResponse.json(suggestionsResponse())
      }),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={false} />)

    // View-only members must not be able to trigger the admin-only eval.
    expect(screen.getByTestId('suggestions-readonly')).toBeInTheDocument()
    expect(screen.queryByTestId('analyze-suggestions')).not.toBeInTheDocument()
    expect(called).toBe(false)
  })

  it('renders per-dimension coverage badges after Analyze', async () => {
    server.use(http.post(SUGGESTIONS_URL, () => HttpResponse.json(suggestionsResponse())))
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('coverage-summary')).toBeInTheDocument())
    // coverage 0.3333 → 33%
    expect(screen.getByTestId('coverage-modules')).toHaveTextContent('33%')
  })

  it('shows coverage even when there are no candidates (all on-list)', async () => {
    server.use(
      http.post(SUGGESTIONS_URL, () =>
        HttpResponse.json({ coverage: { modules: 1 }, candidates: [], recommendations: [] }),
      ),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('coverage-modules')).toBeInTheDocument())
    expect(screen.getByTestId('coverage-modules')).toHaveTextContent('100%')
    // no candidates → no table
    expect(screen.queryByTestId('suggestions-table')).not.toBeInTheDocument()
  })

  it('shows an empty state when there are no off-list values', async () => {
    server.use(
      http.post(SUGGESTIONS_URL, () =>
        HttpResponse.json({ coverage: {}, candidates: [], recommendations: [] }),
      ),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('suggestions-empty')).toBeInTheDocument())
  })

  it('shows an error state when the eval fails', async () => {
    server.use(
      http.post(SUGGESTIONS_URL, () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'boom' }, { status: 500 }),
      ),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('suggestions-error')).toBeInTheDocument())
  })
})
