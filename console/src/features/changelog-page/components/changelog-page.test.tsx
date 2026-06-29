import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import type { ChangelogEntry } from './changelog-page'
import { ChangelogPage } from './changelog-page'

const ENTRIES: ChangelogEntry[] = [
  {
    id: '1',
    title: 'v1.2.0 — Teams integration',
    body: 'Added Microsoft Teams adapter.',
    published: true,
    createdAt: '2026-06-25',
  },
  {
    id: '2',
    title: 'v1.3.0 — Survey builder',
    body: 'NPS/CSAT/CES surveys.',
    published: false,
    createdAt: '2026-06-29',
  },
]

describe('ChangelogPage', () => {
  it('renders entries with published badge', () => {
    renderWithProviders(
      <ChangelogPage entries={ENTRIES} onAdd={vi.fn()} onRemove={vi.fn()} onPublish={vi.fn()} />,
    )
    expect(screen.getByText('v1.2.0 — Teams integration')).toBeInTheDocument()
    expect(screen.getByText('已发布')).toBeInTheDocument()
    expect(screen.getByText('草稿')).toBeInTheDocument()
  })

  it('shows empty state', () => {
    renderWithProviders(
      <ChangelogPage entries={[]} onAdd={vi.fn()} onRemove={vi.fn()} onPublish={vi.fn()} />,
    )
    expect(screen.getByText('暂无更新')).toBeInTheDocument()
  })

  it('calls onPublish for draft entry', async () => {
    const onPublish = vi.fn()
    renderWithProviders(
      <ChangelogPage entries={ENTRIES} onAdd={vi.fn()} onRemove={vi.fn()} onPublish={onPublish} />,
    )
    const publishBtn = screen.getByRole('button', { name: '发布' })
    await userEvent.click(publishBtn)
    expect(onPublish).toHaveBeenCalledWith('2')
  })
})
