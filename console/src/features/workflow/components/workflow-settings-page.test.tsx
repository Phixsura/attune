import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { WorkflowState, WorkflowTransition } from '@/proto/attune/v1/workflow'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'
import { WorkflowSettingsPage } from './workflow-settings-page'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

afterEach(() => {
  vi.mocked(toast.success).mockClear()
  vi.mocked(toast.error).mockClear()
})

const openState: WorkflowState = {
  id: 'ws-1',
  name: 'open',
  displayName: { entries: { default: 'Open' } },
  color: '#3b82f6',
  category: 'open',
  position: 0,
  isDefault: true,
  archived: false,
  createdAt: '2026-06-14T00:00:00Z',
  updatedAt: '2026-06-14T00:00:00Z',
}

const progressState: WorkflowState = {
  id: 'ws-2',
  name: 'in_progress',
  displayName: { entries: { default: 'In Progress' } },
  color: '#f59e0b',
  category: 'active',
  position: 1,
  isDefault: false,
  archived: false,
  createdAt: '2026-06-14T00:00:00Z',
  updatedAt: '2026-06-14T00:00:00Z',
}

const doneState: WorkflowState = {
  id: 'ws-3',
  name: 'done',
  displayName: { entries: { default: 'Done' } },
  color: '#22c55e',
  category: 'closed',
  position: 2,
  isDefault: false,
  archived: false,
  createdAt: '2026-06-14T00:00:00Z',
  updatedAt: '2026-06-14T00:00:00Z',
}

const archivedState: WorkflowState = {
  ...doneState,
  id: 'ws-4',
  name: 'archived',
  displayName: { entries: { default: 'Archived' } },
  archived: true,
}

const transitions: WorkflowTransition[] = [{ id: 'wt-1', fromStateId: 'ws-1', toStateId: 'ws-2' }]

function useDefaultHandlers(
  states: WorkflowState[] = [openState, progressState, doneState],
  trans: WorkflowTransition[] = transitions,
) {
  server.use(
    http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states })),
    http.get('/fb/v1/console/workflow/transitions', () =>
      HttpResponse.json({ transitions: trans }),
    ),
  )
}

describe('WorkflowSettingsPage', () => {
  it('renders states in a table', async () => {
    useDefaultHandlers()
    renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      // State names appear in both the table and the transition matrix,
      // so use getAllByText and assert on presence.
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })
    expect(screen.getAllByText('In Progress').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('Done').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('状态总数')).toBeInTheDocument()
    expect(screen.getByText('状态治理建议')).toBeInTheDocument()
  })

  it('shows empty state and auto-seeds when no states exist', async () => {
    let seeded = false
    server.use(
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/workflow/transitions', () => HttpResponse.json({ transitions: [] })),
      http.post('/fb/v1/console/workflow/seed', () => {
        seeded = true
        return HttpResponse.json({})
      }),
    )
    renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(seeded).toBe(true)
    })
  })

  it('renders default indicator for default state', async () => {
    useDefaultHandlers()
    renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })
    expect(screen.getByText('✓')).toBeInTheDocument()
  })

  it('renders edit and archive buttons for active states', async () => {
    useDefaultHandlers()
    renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })
    const editButtons = screen.getAllByRole('button', { name: '编辑状态' })
    expect(editButtons.length).toBeGreaterThanOrEqual(1)
    const archiveButtons = screen.getAllByRole('button', { name: '归档状态' })
    expect(archiveButtons.length).toBeGreaterThanOrEqual(1)
  })

  it('does not render edit/archive buttons for archived states', async () => {
    useDefaultHandlers([openState, archivedState])
    renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })
    // Only one set of edit/archive buttons (for Open, not Archived)
    const editButtons = screen.getAllByRole('button', { name: '编辑状态' })
    expect(editButtons).toHaveLength(1)
  })

  it('opens create dialog and submits new state', async () => {
    let createBody: unknown
    server.use(
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [openState] })),
      http.get('/fb/v1/console/workflow/transitions', () => HttpResponse.json({ transitions: [] })),
      http.post('/fb/v1/console/workflow/states', async ({ request }) => {
        createBody = await request.json()
        return HttpResponse.json({ state: progressState })
      }),
    )
    const { user } = renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })

    await user.click(screen.getByRole('button', { name: /新建状态/ }))
    const dialog = screen.getByRole('dialog')
    const keyInput = within(dialog).getByLabelText('Key（稳定标识）')
    await user.type(keyInput, 'in_progress')
    // Fill the default-locale display name (first empty textbox in the dialog).
    const emptyBox = within(dialog)
      .getAllByRole('textbox')
      .find(
        (el) =>
          (el as HTMLInputElement).id !== 'state-key' && (el as HTMLInputElement).value === '',
      )
    if (emptyBox) await user.type(emptyBox, 'In Progress')
    await user.click(within(dialog).getByRole('button', { name: '新建' }))

    await waitFor(() => {
      expect(createBody).toMatchObject({
        name: 'in_progress',
        displayName: { entries: { default: 'In Progress' } },
        category: 'open',
        position: 1,
      })
    })
  })

  it('opens edit dialog with existing state data', async () => {
    useDefaultHandlers()
    const { user } = renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })

    const editButtons = screen.getAllByRole('button', { name: '编辑状态' })
    await user.click(editButtons[0])

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('编辑状态')).toBeInTheDocument()
    // Key is shown locked as the slug; the display name is pre-filled.
    const keyInput = within(dialog).getByLabelText('Key（稳定标识）')
    expect(keyInput).toHaveValue('open')
    expect(keyInput).toHaveAttribute('readonly')
    expect(within(dialog).getByDisplayValue('Open')).toBeInTheDocument()
  })

  it('edit submits displayName and color without the immutable key', async () => {
    let patchBody: unknown
    server.use(
      http.get('/fb/v1/console/workflow/states', () =>
        HttpResponse.json({ states: [openState, progressState, doneState] }),
      ),
      http.get('/fb/v1/console/workflow/transitions', () => HttpResponse.json({ transitions })),
      http.patch('/fb/v1/console/workflow/states/:id', async ({ request }) => {
        patchBody = await request.json()
        return HttpResponse.json({ state: openState })
      }),
    )
    const { user } = renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })

    await user.click(screen.getAllByRole('button', { name: '编辑状态' })[0])
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '#ef4444' }))
    await user.click(within(dialog).getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(patchBody).toEqual({
        id: 'ws-1',
        displayName: { entries: { default: 'Open' } },
        color: '#ef4444',
      })
    })
  })

  it('opens archive confirmation dialog and archives on confirm', async () => {
    let deletedId = ''
    server.use(
      http.get('/fb/v1/console/workflow/states', () =>
        HttpResponse.json({ states: [openState, progressState, doneState] }),
      ),
      http.get('/fb/v1/console/workflow/transitions', () => HttpResponse.json({ transitions })),
      http.delete('/fb/v1/console/workflow/states/:id', ({ params }) => {
        deletedId = String(params.id)
        return HttpResponse.json({})
      }),
    )
    const { user } = renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })

    const archiveButtons = screen.getAllByRole('button', { name: '归档状态' })
    await user.click(archiveButtons[0])

    // Confirm dialog should appear
    const confirmDialog = screen.getByRole('dialog')
    expect(within(confirmDialog).getByText('归档状态')).toBeInTheDocument()

    await user.click(within(confirmDialog).getByRole('button', { name: '确认' }))

    await waitFor(() => {
      expect(deletedId).toBe('ws-1')
    })
  })

  it('dismisses archive dialog on cancel', async () => {
    useDefaultHandlers()
    const { user } = renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })

    const archiveButtons = screen.getAllByRole('button', { name: '归档状态' })
    await user.click(archiveButtons[0])

    const confirmDialog = screen.getByRole('dialog')
    await user.click(within(confirmDialog).getByRole('button', { name: '取消' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('surfaces create, edit, and archive failures while preserving dialogs', async () => {
    server.use(
      http.get('/fb/v1/console/workflow/states', () =>
        HttpResponse.json({ states: [openState, progressState, doneState] }),
      ),
      http.get('/fb/v1/console/workflow/transitions', () => HttpResponse.json({ transitions })),
      http.post('/fb/v1/console/workflow/states', () =>
        HttpResponse.json({ message: 'create failed' }, { status: 500 }),
      ),
      http.patch('/fb/v1/console/workflow/states/:id', () =>
        HttpResponse.json({ message: 'update failed' }, { status: 500 }),
      ),
      http.delete('/fb/v1/console/workflow/states/:id', () =>
        HttpResponse.json({ message: 'archive failed' }, { status: 500 }),
      ),
    )
    const { user } = renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })

    await user.click(screen.getByRole('button', { name: /新建状态/ }))
    let dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Key（稳定标识）'), 'blocked')
    const displayNameInput = within(dialog)
      .getAllByRole('textbox')
      .find(
        (el) =>
          (el as HTMLInputElement).id !== 'state-key' && (el as HTMLInputElement).value === '',
      )
    if (displayNameInput) await user.type(displayNameInput, 'Blocked')
    await user.click(within(dialog).getByRole('button', { name: '新建' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('create failed'))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' }))

    await user.click(screen.getAllByRole('button', { name: '编辑状态' })[0])
    dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('update failed'))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    await user.click(screen.getAllByRole('button', { name: '归档状态' })[0])
    dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '确认' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('archive failed'))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('saves transition matrix changes and surfaces save failures', async () => {
    const bodies: unknown[] = []
    server.use(
      http.get('/fb/v1/console/workflow/states', () =>
        HttpResponse.json({ states: [openState, progressState, doneState] }),
      ),
      http.get('/fb/v1/console/workflow/transitions', () => HttpResponse.json({ transitions: [] })),
      http.put('/fb/v1/console/workflow/transitions', async ({ request }) => {
        const body = await request.json()
        bodies.push(body)
        return HttpResponse.json({})
      }),
    )
    const { user } = renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getByText('流转规则')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('checkbox', { name: 'Open → Done' }))
    await user.click(screen.getByRole('button', { name: '保存规则' }))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('流转规则已保存'))
    expect(bodies.at(-1)).toEqual({
      transitions: [{ fromStateId: 'ws-1', toStateId: 'ws-3' }],
    })

    server.use(
      http.put('/fb/v1/console/workflow/transitions', () =>
        HttpResponse.json({ message: 'transition save failed' }, { status: 500 }),
      ),
    )
    await user.click(screen.getByRole('checkbox', { name: 'In Progress → Done' }))
    await user.click(screen.getByRole('button', { name: '保存规则' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('transition save failed'))
  })

  it('renders transition matrix when 2+ active states exist', async () => {
    useDefaultHandlers()
    renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getByText('流转规则')).toBeInTheDocument()
    })
  })

  it('does not render transition matrix with only 1 active state', async () => {
    useDefaultHandlers([openState])
    renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getAllByText('Open').length).toBeGreaterThanOrEqual(1)
    })
    expect(screen.queryByText('流转规则')).not.toBeInTheDocument()
  })

  it('renders page title and subtitle', async () => {
    useDefaultHandlers()
    renderWithProviders(<WorkflowSettingsPage />)

    await waitFor(() => {
      expect(screen.getByText('工作流状态')).toBeInTheDocument()
    })
    expect(
      screen.getByText('自定义反馈的处理阶段。每个状态属于「开放」「活跃」「关闭」三类中的一种。'),
    ).toBeInTheDocument()
  })
})
