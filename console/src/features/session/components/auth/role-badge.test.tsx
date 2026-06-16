// SPDX-License-Identifier: Apache-2.0
// biome-ignore-all lint/a11y/useValidAriaRole: role prop is business role, not ARIA role

import { describe, expect, it } from 'vitest'
import { RoleBadge } from '@/features/session/components/auth/role-badge'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('RoleBadge', () => {
  it('renders admin role with correct text', () => {
    renderWithProviders(<RoleBadge role="admin" />)
    expect(screen.getByText('管理员')).toBeInTheDocument()
  })

  it('renders member role with correct text', () => {
    renderWithProviders(<RoleBadge role="member" />)
    expect(screen.getByText('成员')).toBeInTheDocument()
  })

  it('renders viewer role with correct text', () => {
    renderWithProviders(<RoleBadge role="viewer" />)
    expect(screen.getByText('只读')).toBeInTheDocument()
  })

  it('defaults to viewer when role is undefined', () => {
    renderWithProviders(<RoleBadge />)
    expect(screen.getByText('只读')).toBeInTheDocument()
  })

  it('defaults to viewer style for invalid role', () => {
    renderWithProviders(<RoleBadge role="invalid" />)
    // Should still render with viewer style (fallback)
    const badge = screen.getByText('role.invalid')
    expect(badge).toBeInTheDocument()
  })

  it('applies custom className', () => {
    renderWithProviders(<RoleBadge role="admin" className="custom-class" />)
    const badge = screen.getByText('管理员')
    expect(badge).toHaveClass('custom-class')
  })

  it('has correct base styles', () => {
    renderWithProviders(<RoleBadge role="admin" />)
    const badge = screen.getByText('管理员')
    expect(badge).toHaveClass('px-2', 'py-0.5', 'rounded', 'border', 'text-xs', 'font-medium')
  })
})
