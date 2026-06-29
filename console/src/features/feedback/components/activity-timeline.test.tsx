import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import type { ActivityEvent } from './activity-timeline'
import { ActivityTimeline } from './activity-timeline'

const EVENTS: ActivityEvent[] = [
  {
    id: '1',
    action: 'created feedback',
    actor: 'System',
    timestamp: '2026-06-29 10:00',
    detail: 'via API',
  },
  { id: '2', action: 'enriched', actor: 'AI', timestamp: '2026-06-29 10:01' },
  {
    id: '3',
    action: 'replied',
    actor: 'Alice',
    timestamp: '2026-06-29 10:05',
    detail: 'Draft sent',
  },
]

describe('ActivityTimeline', () => {
  it('renders events with actors and actions', () => {
    renderWithProviders(<ActivityTimeline events={EVENTS} />)
    expect(screen.getByText('System')).toBeInTheDocument()
    expect(screen.getByText('enriched')).toBeInTheDocument()
    expect(screen.getByText('Alice')).toBeInTheDocument()
  })

  it('shows empty state', () => {
    renderWithProviders(<ActivityTimeline events={[]} />)
    expect(screen.getByText('暂无活动记录')).toBeInTheDocument()
  })

  it('shows event detail when provided', () => {
    renderWithProviders(<ActivityTimeline events={EVENTS} />)
    expect(screen.getByText('via API')).toBeInTheDocument()
    expect(screen.getByText('Draft sent')).toBeInTheDocument()
  })
})
