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
  {
    name: 'labels',
    displayName: { entries: { default: 'Labels' } },
    kind: 'multi',
    taxonomy: [],
    urgentSet: [],
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

  it('renders a calm urgent summary when no feedback is urgent', () => {
    renderWithProviders(<DimStatsBars dims={dims} stats={[]} total={5} />)
    expect(screen.getByText('暂无紧急反馈')).toBeInTheDocument()
    expect(screen.getByText('已出现分类值')).toBeInTheDocument()
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

  it('renders multi dimensions as a tag cluster instead of a percentage bar', () => {
    const stats: DimStats[] = [
      {
        dim: 'labels',
        top: [
          { value: 'checkout', count: '7' },
          { value: 'payment', count: '6' },
          { value: 'mobile', count: '2' },
        ],
      },
    ]
    renderWithProviders(<DimStatsBars dims={dims} stats={stats} total={10} />)
    expect(screen.getByText('Labels')).toBeInTheDocument()
    expect(screen.getByText('checkout')).toBeInTheDocument()
    expect(screen.getByText('payment')).toBeInTheDocument()
    expect(screen.getByText('mobile')).toBeInTheDocument()
    expect(screen.getAllByText('3')).toHaveLength(2)
  })
})
