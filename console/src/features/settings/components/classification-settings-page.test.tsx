import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { ClassificationSettingsPage } from '@/features/settings/components/classification-settings-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('ClassificationSettingsPage', () => {
  it('renders hero metrics and preview empty state from the enrich config query', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: {
            promptTemplate: 'Prompt',
            defaultPromptTemplate: 'Prompt',
            dimensions: [
              {
                name: 'severity',
                displayName: { entries: { default: 'Severity' } },
                kind: 'single',
                taxonomy: [{ value: 'P0', displayName: { entries: { default: 'P0' } } }],
                urgentSet: ['P0'],
                required: false,
                examples: [],
                extractionHint: '',
              },
            ],
          },
        }),
      ),
      http.get('/fb/v1/console/eval/suggestions', () =>
        HttpResponse.json({
          suggestions: [],
          coverage: [],
        }),
      ),
    )

    renderWithProviders(<ClassificationSettingsPage />)

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'AI 分类设置' })).toBeInTheDocument()
    })
    expect(screen.getByText('维度总数')).toBeInTheDocument()
    expect(screen.getByText('白名单维度')).toBeInTheDocument()
    expect(screen.getByText('紧急规则维度')).toBeInTheDocument()
    expect(screen.getByText('还没有生成预览')).toBeInTheDocument()
    expect(screen.getAllByText('分类提示词').length).toBeGreaterThanOrEqual(1)
  })

  // Seed one persisted dim + a handler that captures the PUT body.
  function seedConfig(onPut?: (body: { dimensions?: unknown[] }) => void) {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: {
            promptTemplate: 'Prompt',
            defaultPromptTemplate: 'Prompt',
            dimensions: [
              {
                name: 'severity',
                displayName: { entries: { default: 'Severity' } },
                kind: 'single',
                taxonomy: [],
                urgentSet: [],
                required: false,
                examples: [],
                extractionHint: '',
              },
            ],
          },
        }),
      ),
      http.get('/fb/v1/console/eval/suggestions', () =>
        HttpResponse.json({ suggestions: [], coverage: [] }),
      ),
      http.put('/fb/v1/console/enrich-config', async ({ request }) => {
        const body = (await request.json()) as { dimensions?: unknown[]; promptTemplate?: string }
        onPut?.(body)
        return HttpResponse.json({
          config: {
            promptTemplate: 'Prompt',
            defaultPromptTemplate: 'Prompt',
            dimensions: body.dimensions ?? [],
          },
        })
      }),
    )
  }

  it('strips client-only identity fields from the saved dimensions (#90 G7)', async () => {
    const captured: { body: { dimensions?: unknown[] } | null } = { body: null }
    seedConfig((b) => {
      captured.body = b
    })
    const { user } = renderWithProviders(<ClassificationSettingsPage />)
    await waitFor(() => expect(screen.getByTestId('dim-editor-add-dim')).toBeInTheDocument())
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(captured.body).not.toBeNull())
    // Seeded 'severity' + the added row must BOTH be sent (so an empty-array
    // mutant fails here)...
    expect(captured.body?.dimensions).toHaveLength(2)
    // ...as plain Dimension[] — no _key/_isNew anywhere.
    expect(JSON.stringify(captured.body?.dimensions)).not.toMatch(/_key|_isNew/)
  })

  it('strips client-only fields from the PREVIEW request too (#90 G7)', async () => {
    const captured: { body: { dimensions?: unknown[] } | null } = { body: null }
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: {
            promptTemplate: 'Prompt',
            defaultPromptTemplate: 'Prompt',
            dimensions: [
              {
                name: 'severity',
                displayName: { entries: { default: 'Severity' } },
                kind: 'single',
                taxonomy: [],
                urgentSet: [],
                required: false,
                examples: [],
                extractionHint: '',
              },
            ],
          },
        }),
      ),
      http.get('/fb/v1/console/eval/suggestions', () =>
        HttpResponse.json({ suggestions: [], coverage: [] }),
      ),
      http.post('/fb/v1/console/enrich-config/preview', async ({ request }) => {
        captured.body = (await request.json()) as typeof captured.body
        return HttpResponse.json({ renderedPrompt: 'rendered' })
      }),
    )
    const { user } = renderWithProviders(<ClassificationSettingsPage />)
    await waitFor(() => expect(screen.getByTestId('dim-editor-add-dim')).toBeInTheDocument())
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    await user.click(screen.getByRole('button', { name: '生成预览' }))
    await waitFor(() => expect(captured.body).not.toBeNull())
    expect(captured.body?.dimensions).toHaveLength(2)
    expect(JSON.stringify(captured.body?.dimensions)).not.toMatch(/_key|_isNew/)
  })

  it('locks a newly-added dimension after a successful save (#90 reconcile)', async () => {
    seedConfig()
    const { user } = renderWithProviders(<ClassificationSettingsPage />)
    await waitFor(() => expect(screen.getByTestId('dim-editor-add-dim')).toBeInTheDocument())
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    const nameInput = () => document.querySelector<HTMLInputElement>('input[id^="dim-name-"]')
    await user.type(nameInput() as HTMLInputElement, 'newdim')
    expect(nameInput()?.readOnly).toBe(false)
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(nameInput()?.readOnly).toBe(true))
  })

  it('keeps a newly-added dimension editable after a FAILED save', async () => {
    let putCalled = false
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: {
            promptTemplate: 'Prompt',
            defaultPromptTemplate: 'Prompt',
            dimensions: [],
          },
        }),
      ),
      http.get('/fb/v1/console/eval/suggestions', () =>
        HttpResponse.json({ suggestions: [], coverage: [] }),
      ),
      http.put('/fb/v1/console/enrich-config', () => {
        putCalled = true
        return HttpResponse.json({ error: { code: 'INTERNAL', message: 'boom' } }, { status: 500 })
      }),
    )
    const { user } = renderWithProviders(<ClassificationSettingsPage />)
    await waitFor(() => expect(screen.getByTestId('dim-editor-add-dim')).toBeInTheDocument())
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    const nameInput = () => document.querySelector<HTMLInputElement>('input[id^="dim-name-"]')
    await user.type(nameInput() as HTMLInputElement, 'newdim')
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(putCalled).toBe(true))
    // A failed save must NOT reconcile — the new row stays editable so the
    // operator can fix and retry.
    expect(nameInput()?.readOnly).toBe(false)
  })

  it('folds a promoted suggested value into the editor draft so it rides the next save (#90)', async () => {
    const captured: {
      body: { dimensions?: Array<{ name: string; taxonomy?: Array<{ value: string }> }> } | null
    } = { body: null }
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: {
            promptTemplate: 'Prompt',
            defaultPromptTemplate: 'Prompt',
            dimensions: [
              {
                name: 'modules',
                displayName: { entries: { default: 'Modules' } },
                kind: 'multi',
                taxonomy: [],
                urgentSet: [],
                required: false,
                examples: [],
                extractionHint: '',
              },
            ],
          },
        }),
      ),
      http.post('/fb/v1/console/enrich-config/eval-suggestions\\:analyze', () =>
        HttpResponse.json({
          candidates: [
            { dim: 'modules', value: 'billing', count: 5, confidence: 1, coverageImpact: 0.3 },
          ],
          recommendations: [],
          coverage: {},
        }),
      ),
      http.post('/fb/v1/console/enrich-config/promote', () =>
        HttpResponse.json({ dimension: { name: 'modules', taxonomy: [] } }),
      ),
      http.put('/fb/v1/console/enrich-config', async ({ request }) => {
        captured.body = (await request.json()) as typeof captured.body
        return HttpResponse.json({
          config: {
            promptTemplate: 'Prompt',
            defaultPromptTemplate: 'Prompt',
            dimensions: captured.body?.dimensions ?? [],
          },
        })
      }),
    )
    const { user } = renderWithProviders(<ClassificationSettingsPage />)
    await waitFor(() => expect(screen.getByTestId('analyze-suggestions')).toBeInTheDocument())
    await user.click(screen.getByTestId('analyze-suggestions'))
    await waitFor(() => expect(screen.getByTestId('promote-billing')).toBeInTheDocument())
    await user.click(screen.getByTestId('promote-billing'))
    // Without the onPromoted wiring the value never reaches the draft and the
    // next save silently drops it.
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(captured.body).not.toBeNull())
    const modules = captured.body?.dimensions?.find((d) => d.name === 'modules')
    expect(modules?.taxonomy?.map((t) => t.value)).toContain('billing')
    expect(JSON.stringify(captured.body?.dimensions ?? [])).not.toMatch(/_key|_isNew/)
  })

  it('locks the editor while a save is in flight, then re-enables it (#90 mid-save race)', async () => {
    let releasePut: () => void = () => {}
    const gate = new Promise<void>((r) => {
      releasePut = r
    })
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: 'Prompt', defaultPromptTemplate: 'Prompt', dimensions: [] },
        }),
      ),
      http.get('/fb/v1/console/eval/suggestions', () =>
        HttpResponse.json({ suggestions: [], coverage: [] }),
      ),
      http.put('/fb/v1/console/enrich-config', async ({ request }) => {
        const body = (await request.json()) as { dimensions?: unknown[] }
        await gate // hold the PUT in flight
        return HttpResponse.json({
          config: {
            promptTemplate: 'Prompt',
            defaultPromptTemplate: 'Prompt',
            dimensions: body.dimensions ?? [],
          },
        })
      }),
    )
    const { user } = renderWithProviders(<ClassificationSettingsPage />)
    await waitFor(() => expect(screen.getByTestId('dim-editor-add-dim')).toBeInTheDocument())
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    const nameInput = () => document.querySelector<HTMLInputElement>('input[id^="dim-name-"]')
    await user.type(nameInput() as HTMLInputElement, 'newdim')
    await user.click(screen.getByRole('button', { name: '保存' }))
    // While the PUT is in flight the editor is disabled — the Add control is
    // gone — so a mid-save identifier edit can't diverge from the submission.
    await waitFor(() => expect(screen.queryByTestId('dim-editor-add-dim')).toBeNull())
    releasePut()
    await waitFor(() => expect(screen.getByTestId('dim-editor-add-dim')).toBeInTheDocument())
  })
})
