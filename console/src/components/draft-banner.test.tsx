import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders, screen } from '@/testing/test-utils'
import { DraftBanner } from './draft-banner'

describe('DraftBanner', () => {
  it('renders recovery variant with age', () => {
    renderWithProviders(
      <DraftBanner variant="recovery" draftAgeMs={120_000} onDiscard={vi.fn()} onKeep={vi.fn()} />,
    )
    expect(screen.getByText('检测到未保存的草稿')).toBeInTheDocument()
    expect(screen.getByText(/2 分钟/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '丢弃' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保留草稿' })).toBeInTheDocument()
  })

  it('renders recovery variant without age', () => {
    renderWithProviders(<DraftBanner variant="recovery" onDiscard={vi.fn()} onKeep={vi.fn()} />)
    expect(screen.getByText('存在未保存的草稿，与当前服务器配置不同。')).toBeInTheDocument()
  })

  it('renders conflict variant', () => {
    renderWithProviders(<DraftBanner variant="conflict" onDiscard={vi.fn()} onKeep={vi.fn()} />)
    expect(screen.getByText('服务器配置已更新')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '加载最新版本' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保留我的修改' })).toBeInTheDocument()
  })

  it('calls onDiscard when discard button clicked', async () => {
    const onDiscard = vi.fn()
    const { user } = renderWithProviders(
      <DraftBanner variant="recovery" onDiscard={onDiscard} onKeep={vi.fn()} />,
    )
    await user.click(screen.getByRole('button', { name: '丢弃' }))
    expect(onDiscard).toHaveBeenCalledOnce()
  })

  it('calls onKeep when keep button clicked', async () => {
    const onKeep = vi.fn()
    const { user } = renderWithProviders(
      <DraftBanner variant="conflict" onDiscard={vi.fn()} onKeep={onKeep} />,
    )
    await user.click(screen.getByRole('button', { name: '保留我的修改' }))
    expect(onKeep).toHaveBeenCalledOnce()
  })

  it('has role=alert for accessibility', () => {
    renderWithProviders(<DraftBanner variant="recovery" onDiscard={vi.fn()} onKeep={vi.fn()} />)
    const alerts = screen.getAllByRole('alert')
    expect(alerts.length).toBeGreaterThanOrEqual(1)
  })

  it('formats age in seconds', () => {
    renderWithProviders(
      <DraftBanner variant="recovery" draftAgeMs={30_000} onDiscard={vi.fn()} onKeep={vi.fn()} />,
    )
    expect(screen.getByText(/几秒/)).toBeInTheDocument()
  })

  it('formats age in hours', () => {
    renderWithProviders(
      <DraftBanner
        variant="recovery"
        draftAgeMs={7_200_000}
        onDiscard={vi.fn()}
        onKeep={vi.fn()}
      />,
    )
    expect(screen.getByText(/2 小时/)).toBeInTheDocument()
  })
})
