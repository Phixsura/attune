import { ConfidenceIndicator } from '@/features/feedback/components/confidence-indicator'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('ConfidenceIndicator', () => {
  it('renders low confidence below the threshold', () => {
    renderWithProviders(<ConfidenceIndicator confidence={0.49} />)
    expect(screen.getByTitle('低置信度')).toHaveTextContent('49%')
  })

  it('renders medium confidence at the threshold', () => {
    renderWithProviders(<ConfidenceIndicator confidence={0.5} />)
    expect(screen.getByTitle('中置信度')).toHaveTextContent('50%')
  })

  it('renders high confidence at 75 percent', () => {
    renderWithProviders(<ConfidenceIndicator confidence={0.75} />)
    expect(screen.getByTitle('高置信度')).toHaveTextContent('75%')
  })

  it('renders an unavailable placeholder when confidence is missing', () => {
    renderWithProviders(<ConfidenceIndicator />)
    expect(screen.getByTitle('暂无置信度')).toHaveTextContent('--')
  })
})
