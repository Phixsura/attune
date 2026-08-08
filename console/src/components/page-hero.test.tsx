import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { PageHero } from './page-hero'

describe('PageHero', () => {
  it('applies its content width basis only when the hero becomes horizontal', () => {
    render(<PageHero title="Survey operations" subtitle="Track campaign health." />)

    const content = screen.getByRole('heading', { name: 'Survey operations' }).parentElement

    expect(content).toHaveClass('sm:basis-[260px]')
    expect(content).not.toHaveClass('basis-[260px]')
  })
})
