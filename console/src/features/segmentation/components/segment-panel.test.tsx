import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import type { SegmentItem } from './segment-panel'
import { SegmentPanel } from './segment-panel'

const SEGMENTS: SegmentItem[] = [
  {
    id: '1',
    name: 'Enterprise',
    tier: 'enterprise',
    customerCount: 50,
    feedbackCount: 200,
    revenueWeight: 60,
  },
  { id: '2', name: 'Pro', tier: 'pro', customerCount: 200, feedbackCount: 150, revenueWeight: 30 },
  {
    id: '3',
    name: 'Free',
    tier: 'free',
    customerCount: 1000,
    feedbackCount: 100,
    revenueWeight: 10,
  },
]

describe('SegmentPanel', () => {
  it('renders segments with tier and counts', () => {
    renderWithProviders(<SegmentPanel segments={SEGMENTS} />)
    expect(screen.getByText('Enterprise')).toBeInTheDocument()
    expect(screen.getByText('Pro')).toBeInTheDocument()
    expect(screen.getByText('Free')).toBeInTheDocument()
  })

  it('shows empty state', () => {
    renderWithProviders(<SegmentPanel segments={[]} />)
    expect(screen.getByText('暂无客户分群')).toBeInTheDocument()
  })

  it('calls onSelect when clicked', async () => {
    const onSelect = vi.fn()
    renderWithProviders(<SegmentPanel segments={SEGMENTS} onSelect={onSelect} />)
    await userEvent.click(screen.getByText('Enterprise'))
    expect(onSelect).toHaveBeenCalledWith('1')
  })
})
