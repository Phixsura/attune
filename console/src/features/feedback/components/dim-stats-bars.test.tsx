import { describe, expect, it } from 'vitest'
import { DimStatsBars } from '@/features/feedback/components/dim-stats-bars'
import type { Dimension } from '@/proto/attune/v1/common'
import type { DimStats } from '@/proto/attune/v1/ingest'
import { renderWithProviders, screen } from '@/testing/test-utils'

const dims: Dimension[] = [
  {
    name: 'severity',
    displayName: { entries: { default: 'Severity' } },
    kind: 'single',
    taxonomy: [
      { value: 'P0', displayName: { entries: { default: 'P0 — critical' } }, examples: [] },
      { value: 'P1', displayName: { entries: { default: 'P1' } }, examples: [] },
    ],
    urgentSet: ['P0'],
    required: false,
    examples: [],
    extractionHint: '',
  },
]

describe('DimStatsBars', () => {
  it('returns null (nothing rendered) when total=0', () => {
    const { container } = renderWithProviders(<DimStatsBars dims={dims} stats={[]} total={0} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders summary metrics even when stat dims are empty', () => {
    renderWithProviders(<DimStatsBars dims={dims} stats={[]} total={5} urgentCount={1} />)
    expect(screen.getByText('反馈')).toBeInTheDocument()
    expect(screen.getByText('紧急')).toBeInTheDocument()
    expect(screen.getByText('20% 需要优先看')).toBeInTheDocument()
  })

  it('renders one insight tile per stat dim with dominant and secondary values', () => {
    const stats: DimStats[] = [
      {
        dim: 'severity',
        top: [
          { value: 'P0', count: '7' },
          { value: 'P1', count: '3' },
        ],
      },
    ]
    renderWithProviders(<DimStatsBars dims={dims} stats={stats} total={10} />)
    // The dim label resolves from displayName.
    expect(screen.getByText('Severity')).toBeInTheDocument()
    // Each taxonomy value renders with its DisplayName.
    expect(screen.getByText('P0 — critical')).toBeInTheDocument()
    expect(screen.getByText('P1')).toBeInTheDocument()
    expect(screen.getByText('70%')).toBeInTheDocument()
  })
})
