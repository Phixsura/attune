import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import type { AuditEntry } from '@/proto/attune/v1/workflow'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { AuditTimeline } from './audit-timeline'

const entry: AuditEntry = {
  id: 'ae-1',
  feedbackId: '42',
  entityType: 'feedback',
  fieldName: 'state',
  oldValue: 'Open',
  newValue: 'In Progress',
  comment: 'starting work',
  changedBy: 'Alice',
  createdAt: '2026-06-14T10:00:00Z',
}

const entryNoOld: AuditEntry = {
  ...entry,
  id: 'ae-2',
  oldValue: '',
  comment: '',
}

describe('AuditTimeline', () => {
  it('shows loading state', () => {
    server.use(
      http.get('/fb/v1/console/feedback/42/audit', () =>
        // Delay response so loading state is visible
        HttpResponse.json({ entries: [entry] }),
      ),
    )
    // Use a fresh QueryClient that has not pre-fetched
    renderWithProviders(<AuditTimeline feedbackId="42" />)
    expect(screen.getByText('加载中…')).toBeInTheDocument()
  })

  it('shows empty state when no entries', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/99/audit', () => HttpResponse.json({ entries: [] })),
    )
    renderWithProviders(<AuditTimeline feedbackId="99" />)
    await waitFor(() => {
      expect(screen.getByText('暂无操作记录')).toBeInTheDocument()
    })
  })

  it('renders audit entries with field name, old value, new value, and comment', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/42/audit', () => HttpResponse.json({ entries: [entry] })),
    )
    renderWithProviders(<AuditTimeline feedbackId="42" />)
    await waitFor(() => {
      expect(screen.getByText('state')).toBeInTheDocument()
    })
    expect(screen.getByText('Open')).toBeInTheDocument()
    expect(screen.getByText('In Progress')).toBeInTheDocument()
    expect(screen.getByText(/starting work/)).toBeInTheDocument()
    expect(screen.getByText(/Alice/)).toBeInTheDocument()
  })

  it('omits old value span when oldValue is empty', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/42/audit', () =>
        HttpResponse.json({ entries: [entryNoOld] }),
      ),
    )
    renderWithProviders(<AuditTimeline feedbackId="42" />)
    await waitFor(() => {
      expect(screen.getByText('state')).toBeInTheDocument()
    })
    expect(screen.getByText('In Progress')).toBeInTheDocument()
    expect(screen.queryByText('Open')).not.toBeInTheDocument()
  })

  it('omits comment paragraph when comment is empty', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/42/audit', () =>
        HttpResponse.json({ entries: [entryNoOld] }),
      ),
    )
    renderWithProviders(<AuditTimeline feedbackId="42" />)
    await waitFor(() => {
      expect(screen.getByText('state')).toBeInTheDocument()
    })
    expect(screen.queryByText(/starting work/)).not.toBeInTheDocument()
  })

  it('renders entries without a timestamp', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/42/audit', () =>
        HttpResponse.json({ entries: [{ ...entry, id: 'ae-no-time', createdAt: '' }] }),
      ),
    )
    renderWithProviders(<AuditTimeline feedbackId="42" />)
    await waitFor(() => {
      expect(screen.getByText('state')).toBeInTheDocument()
    })
    expect(screen.getByText('Alice')).toBeInTheDocument()
  })

  it('renders multiple entries as an ordered list', async () => {
    const second: AuditEntry = {
      ...entry,
      id: 'ae-3',
      fieldName: 'priority',
      oldValue: 'low',
      newValue: 'high',
      comment: '',
    }
    server.use(
      http.get('/fb/v1/console/feedback/42/audit', () =>
        HttpResponse.json({ entries: [entry, second] }),
      ),
    )
    renderWithProviders(<AuditTimeline feedbackId="42" />)
    await waitFor(() => {
      expect(screen.getByText('state')).toBeInTheDocument()
    })
    expect(screen.getByText('priority')).toBeInTheDocument()
    const list = screen.getByRole('list')
    expect(list.querySelectorAll('li')).toHaveLength(2)
  })

  it('uses pre-seeded query data when available', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    qc.setQueryData(['console', 'feedback', '42', 'audit'], [entry])
    renderWithProviders(<AuditTimeline feedbackId="42" />, { queryClient: qc })
    // Should render immediately without loading state
    expect(screen.getByText('state')).toBeInTheDocument()
    expect(screen.getByText('In Progress')).toBeInTheDocument()
  })
})
