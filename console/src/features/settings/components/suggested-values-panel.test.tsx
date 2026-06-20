import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, userEvent, waitFor } from '@/testing/test-utils'
import { SuggestedValuesPanel } from './suggested-values-panel'

const SUGGESTIONS_URL = '/fb/v1/console/enrich-config/eval-suggestions'
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
      http.get(SUGGESTIONS_URL, () => {
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
    server.use(http.get(SUGGESTIONS_URL, () => HttpResponse.json(suggestionsResponse())))
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))

    await waitFor(() => expect(screen.getByTestId('suggestions-table')).toBeInTheDocument())
    expect(screen.getByTestId('suggestion-row-checkout')).toBeInTheDocument()
    expect(screen.getByTestId('suggestion-row-billing')).toBeInTheDocument()
    // confidence 1 → 100%, impact 0.33 → +33%
    expect(screen.getByText('100%')).toBeInTheDocument()
    expect(screen.getByText('+33%')).toBeInTheDocument()
  })

  it('promotes a candidate via POST /promote', async () => {
    let promoted: { dimensionName: string; value: string } | null = null
    server.use(
      http.get(SUGGESTIONS_URL, () => HttpResponse.json(suggestionsResponse())),
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

  it('keeps the candidate and shows a toast when promote fails (e.g. 409)', async () => {
    server.use(
      http.get(SUGGESTIONS_URL, () => HttpResponse.json(suggestionsResponse())),
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

  it('hides promote buttons when canEdit is false', async () => {
    server.use(http.get(SUGGESTIONS_URL, () => HttpResponse.json(suggestionsResponse())))
    renderWithProviders(<SuggestedValuesPanel canEdit={false} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('suggestions-table')).toBeInTheDocument())
    expect(screen.queryByTestId('promote-checkout')).not.toBeInTheDocument()
  })

  it('shows an empty state when there are no off-list values', async () => {
    server.use(
      http.get(SUGGESTIONS_URL, () =>
        HttpResponse.json({ coverage: {}, candidates: [], recommendations: [] }),
      ),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('suggestions-empty')).toBeInTheDocument())
  })

  it('shows an error state when the eval fails', async () => {
    server.use(
      http.get(SUGGESTIONS_URL, () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'boom' }, { status: 500 }),
      ),
    )
    renderWithProviders(<SuggestedValuesPanel canEdit={true} />)

    await userEvent.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('suggestions-error')).toBeInTheDocument())
  })
})
