import { HttpResponse, http } from 'msw'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ClassificationSettingsPage } from '@/features/settings/components/classification-settings-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

let blockerState: {
  status: 'idle' | 'blocked'
  proceed: ReturnType<typeof vi.fn>
  reset: ReturnType<typeof vi.fn>
  shouldBlockFn?: (args: unknown) => boolean
} = {
  status: 'idle',
  proceed: vi.fn(),
  reset: vi.fn(),
}

function resetBlocker() {
  blockerState = {
    status: 'idle',
    proceed: vi.fn(),
    reset: vi.fn(),
  }
}

vi.mock('@tanstack/react-router', () => ({
  useBlocker: (opts?: { shouldBlockFn?: (args: unknown) => boolean }) => {
    if (opts?.shouldBlockFn) blockerState.shouldBlockFn = opts.shouldBlockFn
    return blockerState
  },
}))

afterEach(() => {
  localStorage.clear()
  sessionStorage.clear()
  resetBlocker()
})

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

  describe('draft durability (#172)', () => {
    it('persists draft to localStorage after editing the prompt', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      seedConfig()
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await user.clear(textarea)
      await user.type(textarea, 'New prompt')
      vi.advanceTimersByTime(600)
      const stored = localStorage.getItem('attune:draft:classification-settings')
      expect(stored).not.toBeNull()
      const envelope = JSON.parse(stored as string)
      expect(envelope._v).toBe(1)
      expect(envelope.data.prompt).toBe('New prompt')
      vi.useRealTimers()
    })

    it('restores draft from localStorage on mount', async () => {
      localStorage.setItem(
        'attune:draft:classification-settings',
        JSON.stringify({ _v: 1, _ts: Date.now(), data: { prompt: 'Stored prompt', rows: [] } }),
      )
      seedConfig()
      renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      expect(textarea).toHaveValue('Stored prompt')
    })

    it('clears localStorage on successful save', async () => {
      localStorage.setItem(
        'attune:draft:classification-settings',
        JSON.stringify({ _v: 1, _ts: Date.now(), data: { prompt: 'Draft', rows: [] } }),
      )
      seedConfig()
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      await screen.findByLabelText('提示词模板')
      await user.click(screen.getByRole('button', { name: '保存' }))
      await waitFor(() => {
        expect(localStorage.getItem('attune:draft:classification-settings')).toBeNull()
      })
    })

    it('shows discard button only when touched', async () => {
      seedConfig()
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      expect(screen.queryByRole('button', { name: '放弃更改' })).not.toBeInTheDocument()
      await user.clear(textarea)
      await user.type(textarea, 'edit')
      expect(screen.getByRole('button', { name: '放弃更改' })).toBeInTheDocument()
    })

    it('disables prompt textarea while save is in flight', async () => {
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
          await gate
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
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      await user.clear(textarea)
      await user.type(textarea, 'edit')
      await user.click(screen.getByRole('button', { name: '保存' }))
      await waitFor(() => expect(textarea).toBeDisabled())
      releasePut()
      await waitFor(() => expect(textarea).toBeEnabled())
    })

    it('keeps draft in localStorage after a failed save', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      server.use(
        http.get('/fb/v1/console/enrich-config', () =>
          HttpResponse.json({
            config: { promptTemplate: 'Prompt', defaultPromptTemplate: 'Prompt', dimensions: [] },
          }),
        ),
        http.get('/fb/v1/console/eval/suggestions', () =>
          HttpResponse.json({ suggestions: [], coverage: [] }),
        ),
        http.put('/fb/v1/console/enrich-config', () =>
          HttpResponse.json({ error: { code: 'INTERNAL', message: 'boom' } }, { status: 500 }),
        ),
      )
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await user.clear(textarea)
      await user.type(textarea, 'Unsaved edit')
      vi.advanceTimersByTime(600)
      expect(localStorage.getItem('attune:draft:classification-settings')).not.toBeNull()
      await user.click(screen.getByRole('button', { name: '保存' }))
      await waitFor(() => {
        expect(localStorage.getItem('attune:draft:classification-settings')).not.toBeNull()
      })
      expect(screen.getByRole('button', { name: '放弃更改' })).toBeInTheDocument()
      vi.useRealTimers()
    })

    it('gracefully ignores corrupted localStorage', async () => {
      localStorage.setItem('attune:draft:classification-settings', '{bad json!!!')
      seedConfig()
      renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      expect(textarea).toHaveValue('Prompt')
    })

    it('discard resets form to server state and re-enables editing', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      seedConfig()
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      await user.clear(textarea)
      await user.type(textarea, 'dirty edit')
      vi.advanceTimersByTime(600)
      expect(localStorage.getItem('attune:draft:classification-settings')).not.toBeNull()
      await user.click(screen.getByRole('button', { name: '放弃更改' }))
      const dialog = await screen.findByRole('alertdialog')
      const confirmBtn = within(dialog).getByRole('button', { name: '放弃更改' })
      await user.click(confirmBtn)
      await waitFor(() => {
        expect(textarea).toHaveValue('Prompt')
        expect(localStorage.getItem('attune:draft:classification-settings')).toBeNull()
        expect(screen.queryByRole('button', { name: '放弃更改' })).not.toBeInTheDocument()
      })
      await user.clear(textarea)
      await user.type(textarea, 'new edit after discard')
      vi.advanceTimersByTime(600)
      expect(localStorage.getItem('attune:draft:classification-settings')).not.toBeNull()
      expect(screen.getByRole('button', { name: '放弃更改' })).toBeInTheDocument()
      vi.useRealTimers()
    })

    it('restore default marks form dirty and persists draft', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      server.use(
        http.get('/fb/v1/console/enrich-config', () =>
          HttpResponse.json({
            config: {
              promptTemplate: 'Custom prompt',
              defaultPromptTemplate: 'Default prompt',
              dimensions: [],
            },
          }),
        ),
        http.get('/fb/v1/console/eval/suggestions', () =>
          HttpResponse.json({ suggestions: [], coverage: [] }),
        ),
      )
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      expect(textarea).toHaveValue('Custom prompt')
      expect(screen.queryByRole('button', { name: '放弃更改' })).not.toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: '恢复默认模板' }))
      expect(textarea).toHaveValue('Default prompt')
      expect(screen.getByRole('button', { name: '放弃更改' })).toBeInTheDocument()
      vi.advanceTimersByTime(600)
      const stored = localStorage.getItem('attune:draft:classification-settings')
      expect(stored).not.toBeNull()
      const envelope = JSON.parse(stored as string)
      expect(envelope.data.prompt).toBe('Default prompt')
      vi.useRealTimers()
    })

    it('restore default button is disabled during save', async () => {
      let releasePut: () => void = () => {}
      const gate = new Promise<void>((r) => {
        releasePut = r
      })
      server.use(
        http.get('/fb/v1/console/enrich-config', () =>
          HttpResponse.json({
            config: { promptTemplate: 'Prompt', defaultPromptTemplate: 'Default', dimensions: [] },
          }),
        ),
        http.get('/fb/v1/console/eval/suggestions', () =>
          HttpResponse.json({ suggestions: [], coverage: [] }),
        ),
        http.put('/fb/v1/console/enrich-config', async ({ request }) => {
          const body = (await request.json()) as { dimensions?: unknown[] }
          await gate
          return HttpResponse.json({
            config: {
              promptTemplate: 'Prompt',
              defaultPromptTemplate: 'Default',
              dimensions: body.dimensions ?? [],
            },
          })
        }),
      )
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      await user.clear(textarea)
      await user.type(textarea, 'edit')
      await user.click(screen.getByRole('button', { name: '保存' }))
      await waitFor(() => {
        expect(screen.getByRole('button', { name: '恢复默认模板' })).toBeDisabled()
      })
      releasePut()
      await waitFor(() => {
        expect(screen.getByRole('button', { name: '恢复默认模板' })).toBeEnabled()
      })
    })

    it('shows recovery banner when draft exists on mount', async () => {
      localStorage.setItem(
        'attune:draft:classification-settings',
        JSON.stringify({
          _v: 1,
          _ts: Date.now() - 60_000,
          data: { prompt: 'Recovered', rows: [] },
        }),
      )
      seedConfig()
      renderWithProviders(<ClassificationSettingsPage />)
      await waitFor(() => {
        expect(screen.getByText('检测到未保存的草稿')).toBeInTheDocument()
      })
    })

    it('recovery banner dismiss resets form to server state', async () => {
      localStorage.setItem(
        'attune:draft:classification-settings',
        JSON.stringify({
          _v: 1,
          _ts: Date.now() - 60_000,
          data: { prompt: 'Recovered', rows: [] },
        }),
      )
      seedConfig()
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      await waitFor(() => {
        expect(screen.getByText('检测到未保存的草稿')).toBeInTheDocument()
      })
      await user.click(screen.getByRole('button', { name: '丢弃' }))
      await waitFor(() => {
        expect(screen.queryByText('检测到未保存的草稿')).not.toBeInTheDocument()
      })
      const textarea = await screen.findByLabelText('提示词模板')
      expect(textarea).toHaveValue('Prompt')
    })

    it('shows SaveStatus unsaved indicator when dirty', async () => {
      seedConfig()
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      expect(screen.queryByText(/未保存/)).not.toBeInTheDocument()
      await user.clear(textarea)
      await user.type(textarea, 'dirty')
      expect(screen.getByText(/未保存/)).toBeInTheDocument()
    })

    it('shows SaveStatus saving indicator during save', async () => {
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
          await gate
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
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      await user.clear(textarea)
      await user.type(textarea, 'edit')
      await user.click(screen.getByRole('button', { name: '保存' }))
      await waitFor(() => {
        expect(screen.getAllByText('保存中…').length).toBeGreaterThanOrEqual(1)
      })
      releasePut()
      await waitFor(() => expect(textarea).toBeEnabled())
    })

    it('highlights prompt textarea border when prompt is dirty', async () => {
      seedConfig()
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      expect(textarea.className).not.toContain('border-l-amber-500')
      await user.clear(textarea)
      await user.type(textarea, 'changed')
      expect(textarea.className).toContain('border-l-amber-500')
    })

    it('registers shouldBlockFn when form is dirty', async () => {
      seedConfig()
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      await user.clear(textarea)
      await user.type(textarea, 'dirty edit')
      expect(blockerState.shouldBlockFn).toBeDefined()
    })

    it('shows UnsavedChangesDialog when blocker status is blocked', async () => {
      seedConfig()
      blockerState.status = 'blocked'
      renderWithProviders(<ClassificationSettingsPage />)
      await waitFor(() => {
        expect(screen.getByText('未保存的更改')).toBeInTheDocument()
      })
    })

    it('save-and-leave calls proceed after successful save', async () => {
      localStorage.setItem(
        'attune:draft:classification-settings',
        JSON.stringify({
          _v: 1,
          _ts: Date.now(),
          data: { prompt: 'Dirty edit', rows: [] },
        }),
      )
      seedConfig()
      blockerState.status = 'blocked'
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      await screen.findByLabelText('提示词模板')
      const dialog = await screen.findByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: '保存并离开' }))
      await waitFor(() => {
        expect(blockerState.proceed).toHaveBeenCalledOnce()
      })
    })

    it('shows conflict banner when server data changes while user has dirty edits', async () => {
      let callCount = 0
      server.use(
        http.get('/fb/v1/console/enrich-config', () => {
          callCount++
          return HttpResponse.json({
            config: {
              promptTemplate: callCount === 1 ? 'Original' : 'Updated by someone else',
              defaultPromptTemplate: 'Default',
              dimensions: [],
            },
          })
        }),
        http.get('/fb/v1/console/eval/suggestions', () =>
          HttpResponse.json({ suggestions: [], coverage: [] }),
        ),
      )
      const { user, queryClient: qc } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      await user.clear(textarea)
      await user.type(textarea, 'My local edit')

      await qc.refetchQueries({ queryKey: ['console', 'enrich-config'] })

      await waitFor(() => {
        expect(screen.getByText('服务器配置已更新')).toBeInTheDocument()
      })
      expect(textarea).toHaveValue('My local edit')
    })

    it('conflict banner discard loads server version', async () => {
      let callCount = 0
      server.use(
        http.get('/fb/v1/console/enrich-config', () => {
          callCount++
          return HttpResponse.json({
            config: {
              promptTemplate: callCount === 1 ? 'Original' : 'Server updated',
              defaultPromptTemplate: 'Default',
              dimensions: [],
            },
          })
        }),
        http.get('/fb/v1/console/eval/suggestions', () =>
          HttpResponse.json({ suggestions: [], coverage: [] }),
        ),
      )
      const { user, queryClient: qc } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      await user.clear(textarea)
      await user.type(textarea, 'My local edit')

      await qc.refetchQueries({ queryKey: ['console', 'enrich-config'] })

      await waitFor(() => {
        expect(screen.getByText('服务器配置已更新')).toBeInTheDocument()
      })
      await user.click(screen.getByRole('button', { name: '加载最新版本' }))
      await waitFor(() => {
        expect(textarea).toHaveValue('Server updated')
        expect(screen.queryByText('服务器配置已更新')).not.toBeInTheDocument()
      })
    })

    it('conflict banner keep-draft dismisses banner and preserves edits', async () => {
      let callCount = 0
      server.use(
        http.get('/fb/v1/console/enrich-config', () => {
          callCount++
          return HttpResponse.json({
            config: {
              promptTemplate: callCount === 1 ? 'Original' : 'Server updated',
              defaultPromptTemplate: 'Default',
              dimensions: [],
            },
          })
        }),
        http.get('/fb/v1/console/eval/suggestions', () =>
          HttpResponse.json({ suggestions: [], coverage: [] }),
        ),
      )
      const { user, queryClient: qc } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      await user.clear(textarea)
      await user.type(textarea, 'My local edit')

      await qc.refetchQueries({ queryKey: ['console', 'enrich-config'] })

      await waitFor(() => {
        expect(screen.getByText('服务器配置已更新')).toBeInTheDocument()
      })
      await user.click(screen.getByRole('button', { name: '保留我的修改' }))
      await waitFor(() => {
        expect(screen.queryByText('服务器配置已更新')).not.toBeInTheDocument()
      })
      expect(textarea).toHaveValue('My local edit')
    })

    it('auto-hydrates server data when not dirty and initial changes', async () => {
      let callCount = 0
      server.use(
        http.get('/fb/v1/console/enrich-config', () => {
          callCount++
          return HttpResponse.json({
            config: {
              promptTemplate: callCount === 1 ? 'Original' : 'Server updated',
              defaultPromptTemplate: 'Default',
              dimensions: [],
            },
          })
        }),
        http.get('/fb/v1/console/eval/suggestions', () =>
          HttpResponse.json({ suggestions: [], coverage: [] }),
        ),
      )
      const { queryClient: qc } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      expect(textarea).toHaveValue('Original')

      await qc.refetchQueries({ queryKey: ['console', 'enrich-config'] })

      await waitFor(() => {
        expect(textarea).toHaveValue('Server updated')
      })
      expect(screen.queryByText('服务器配置已更新')).not.toBeInTheDocument()
    })

    it('does not show blocker dialog after a successful save', async () => {
      seedConfig()
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      await user.clear(textarea)
      await user.type(textarea, 'dirty edit')
      await user.click(screen.getByRole('button', { name: '保存' }))
      await waitFor(() => {
        expect(localStorage.getItem('attune:draft:classification-settings')).toBeNull()
      })
      expect(blockerState.shouldBlockFn?.({ location: {} })).toBeFalsy()
    })

    it('discard fires undo toast with action that restores the draft', async () => {
      const infoSpy = vi.spyOn(await import('sonner').then((m) => m.toast), 'info')
      seedConfig()
      const { user } = renderWithProviders(<ClassificationSettingsPage />)
      const textarea = await screen.findByLabelText('提示词模板')
      await waitFor(() => expect(textarea).toBeEnabled())
      await user.clear(textarea)
      await user.type(textarea, 'will undo')
      await user.click(screen.getByRole('button', { name: '放弃更改' }))
      const dialog = await screen.findByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: '放弃更改' }))
      await waitFor(() => expect(textarea).toHaveValue('Prompt'))
      expect(infoSpy).toHaveBeenCalledOnce()
      const toastOpts = infoSpy.mock.calls[0][1] as unknown as {
        action: { onClick: () => void }
      }
      toastOpts.action.onClick()
      await waitFor(() => expect(textarea).toHaveValue('will undo'))
      infoSpy.mockRestore()
    })
  })
})
