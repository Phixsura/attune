import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import type { WorkflowState } from '@/proto/attune/v1/workflow'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { WorkflowTransitionSelect } from './workflow-transition-select'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

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

describe('WorkflowTransitionSelect', () => {
  it('renders current state badge', () => {
    renderWithProviders(
      <WorkflowTransitionSelect
        feedbackId="42"
        currentState={openState}
        allowedNext={[progressState, doneState]}
      />,
    )
    expect(screen.getByText('Open')).toBeInTheDocument()
    expect(screen.getByText(/当前状态/)).toBeInTheDocument()
  })

  it('renders localized display names in the current badge and the option list', async () => {
    const { user } = renderWithProviders(
      <WorkflowTransitionSelect
        feedbackId="42"
        currentState={openState}
        allowedNext={[progressState, doneState]}
      />,
    )
    // Current-state badge resolves displayName, not the "open" slug.
    expect(screen.getByText('Open')).toBeInTheDocument()
    expect(screen.queryByText('open')).not.toBeInTheDocument()

    await user.click(screen.getByRole('combobox'))
    // Options resolve displayName too.
    expect(screen.getByRole('option', { name: /In Progress/ })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Done/ })).toBeInTheDocument()
  })

  it('falls back to the machine key when the current state has no displayName', () => {
    renderWithProviders(
      <WorkflowTransitionSelect
        feedbackId="42"
        currentState={{ ...openState, displayName: undefined }}
        allowedNext={[]}
      />,
    )
    expect(screen.getByText('open')).toBeInTheDocument()
  })

  it('shows no-states message when no current state and no allowed next', () => {
    renderWithProviders(
      <WorkflowTransitionSelect feedbackId="42" currentState={undefined} allowedNext={[]} />,
    )
    expect(screen.getByText('尚未配置工作流状态')).toBeInTheDocument()
  })

  it('renders transition button disabled when no state selected', () => {
    renderWithProviders(
      <WorkflowTransitionSelect
        feedbackId="42"
        currentState={openState}
        allowedNext={[progressState]}
      />,
    )
    const transitionBtn = screen.getByRole('button', { name: /流转/ })
    expect(transitionBtn).toBeDisabled()
  })

  it('renders comment input', () => {
    renderWithProviders(
      <WorkflowTransitionSelect
        feedbackId="42"
        currentState={openState}
        allowedNext={[progressState]}
      />,
    )
    expect(screen.getByPlaceholderText('备注（可选）')).toBeInTheDocument()
  })

  it('does not render select or button when allowedNext is empty', () => {
    renderWithProviders(
      <WorkflowTransitionSelect feedbackId="42" currentState={openState} allowedNext={[]} />,
    )
    expect(screen.queryByRole('button', { name: /流转/ })).not.toBeInTheDocument()
  })

  it('submits transition and resets fields on success', async () => {
    let body: unknown
    server.use(
      http.post('/fb/v1/console/feedback/:id/transition', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({
          feedbackId: '42',
          fromState: openState,
          toState: progressState,
        })
      }),
    )

    const { user } = renderWithProviders(
      <WorkflowTransitionSelect
        feedbackId="42"
        currentState={openState}
        allowedNext={[progressState, doneState]}
      />,
    )

    // Open the select dropdown
    await user.click(screen.getByRole('combobox'))
    // Select "In Progress"
    await user.click(screen.getByRole('option', { name: /In Progress/ }))

    // Type a comment
    const commentInput = screen.getByPlaceholderText('备注（可选）')
    await user.type(commentInput, ' some note ')

    // Click transition button -- after selecting a state, button should be enabled.
    // The button text is "流转" but the combobox placeholder also contains "流转到…",
    // so filter to the button whose text contains "流转" but not "流转到".
    const transitionBtn = screen
      .getAllByRole('button')
      .filter((b) => b.textContent?.includes('流转') && !b.textContent?.includes('流转到'))
    expect(transitionBtn).toHaveLength(1)
    await user.click(transitionBtn[0])

    await waitFor(() => {
      expect(body).toEqual({
        toStateId: 'ws-2',
        comment: 'some note',
      })
    })

    // Fields should reset after success
    await waitFor(() => {
      expect(commentInput).toHaveValue('')
    })
  })

  it('shows error toast on failure', async () => {
    const { toast } = await import('sonner')
    server.use(
      http.post('/fb/v1/console/feedback/:id/transition', () =>
        HttpResponse.json({ message: 'not allowed' }, { status: 400 }),
      ),
    )

    const { user } = renderWithProviders(
      <WorkflowTransitionSelect
        feedbackId="42"
        currentState={openState}
        allowedNext={[progressState]}
      />,
    )

    await user.click(screen.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: /In Progress/ }))

    const transitionBtn2 = screen
      .getAllByRole('button')
      .filter((b) => b.textContent?.includes('流转') && !b.textContent?.includes('流转到'))
    await user.click(transitionBtn2[0])

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalled()
    })
  })
})
