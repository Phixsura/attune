import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import { SentimentChart } from './sentiment-chart'

describe('SentimentChart', () => {
  it('renders bars for each sentiment', () => {
    const { container } = renderWithProviders(
      <SentimentChart
        data={[
          { value: 'positive', count: 10 },
          { value: 'negative', count: 3 },
          { value: 'neutral', count: 7 },
        ]}
      />,
    )
    const rects = container.querySelectorAll('rect')
    expect(rects).toHaveLength(3)
  })

  it('renders nothing with empty data', () => {
    const { container } = renderWithProviders(<SentimentChart data={[]} />)
    expect(container.querySelector('svg')).toBeNull()
  })

  it('sorts by sentiment order', () => {
    const { container } = renderWithProviders(
      <SentimentChart
        data={[
          { value: 'negative', count: 5 },
          { value: 'positive', count: 8 },
          { value: 'neutral', count: 3 },
        ]}
      />,
    )
    const labels = Array.from(container.querySelectorAll('text'))
      .map((t) => t.textContent)
      .filter((t) => t && !/^\d+$/.test(t))
    expect(labels).toEqual(['positive', 'neutral', 'negative'])
  })
})
