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
      { value: 'P0', displayName: { entries: { default: 'P0 — critical' } } },
      { value: 'P1', displayName: { entries: { default: 'P1' } } },
    ],
    urgentSet: ['P0'],
    required: false,
  },
]

describe('DimStatsBars', () => {
  it('returns null (nothing rendered) when total=0', () => {
    const { container } = renderWithProviders(<DimStatsBars dims={dims} stats={[]} total={0} />)
    expect(container.firstChild).toBeNull()
  })

  it('returns null when stats is empty even if total>0', () => {
    const { container } = renderWithProviders(<DimStatsBars dims={dims} stats={[]} total={5} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders one bar block per stat dim with display labels', () => {
    const stats: DimStats[] = [
      {
        dim: 'severity',
        top: [
          { value: 'P0', count: BigInt(7) },
          { value: 'P1', count: BigInt(3) },
        ],
      },
    ]
    renderWithProviders(<DimStatsBars dims={dims} stats={stats} total={10} />)
    // The dim label resolves from displayName.
    expect(screen.getByText('Severity')).toBeInTheDocument()
    // Each taxonomy value renders with its DisplayName.
    expect(screen.getByText('P0 — critical')).toBeInTheDocument()
    expect(screen.getByText('P1')).toBeInTheDocument()
  })
})
