// SPDX-License-Identifier: Apache-2.0

import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import type { BreakGlassLockout, BreakGlassToken } from '@/features/security/api/breakglass'
import { SecurityPage } from '@/features/security/components/security-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

const toastError = vi.fn()
const toastSuccess = vi.fn()
vi.mock('sonner', () => ({
  toast: {
    error: (...args: unknown[]) => toastError(...args),
    success: (...args: unknown[]) => toastSuccess(...args),
  },
}))

const activeToken: BreakGlassToken = {
  id: 'token-1',
  admin_email: 'admin@example.com',
  expires_at: '2099-07-06T12:30:00Z',
  issued_by: 'issuer-1',
  issued_at: '2026-07-05T11:30:00Z',
  status: 'valid',
  allowed_ips: ['203.0.113.0/24'],
}

const sampleLockout: BreakGlassLockout = {
  ip: '203.0.113.10',
  locked_until: '2026-07-05T15:00:00Z',
  remaining_mins: 11,
  attempts: 5,
}

function setupSecurityResponses({
  tokens = [activeToken],
  lockouts = [sampleLockout],
  mode = 'hybrid',
}: {
  tokens?: BreakGlassToken[]
  lockouts?: BreakGlassLockout[]
  mode?: 'hybrid' | 'sso_only'
} = {}) {
  let currentTokens = [...tokens]
  let currentLockouts = [...lockouts]

  server.use(
    http.get('/fb/v1/console/auth/sso/mode', () => HttpResponse.json({ mode })),
    http.get('/fb/v1/console/auth/breakglass/tokens', () =>
      HttpResponse.json({ tokens: currentTokens }),
    ),
    http.get('/fb/v1/console/auth/breakglass/lockouts', () =>
      HttpResponse.json({ lockouts: currentLockouts }),
    ),
    http.post('/fb/v1/console/auth/breakglass/tokens/revoke-all', () => {
      const revoked = currentTokens.filter(
        (token) => token.status === 'valid' || token.status === 'expiring',
      ).length
      currentTokens = []
      return HttpResponse.json({ revoked })
    }),
    http.post('/fb/v1/console/auth/breakglass/lockouts/:ip/unlock', ({ params }) => {
      currentLockouts = currentLockouts.filter((row) => row.ip !== String(params.ip))
      return new HttpResponse(null, { status: 204 })
    }),
  )
}

describe('SecurityPage', () => {
  it('renders break-glass tokens and lockout monitoring', async () => {
    setupSecurityResponses()
    renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    expect(screen.getByText('203.0.113.10')).toBeInTheDocument()
    expect(screen.getByText('5 次失败')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '撤销全部活跃令牌' })).toBeEnabled()
    expect(screen.getByText('Break-Glass 锁定')).toBeInTheDocument()
  })

  it('revokes all active tokens from the security page', async () => {
    setupSecurityResponses()
    const { user } = renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '撤销全部活跃令牌' }))

    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '撤销全部活跃令牌' }))

    await waitFor(() => {
      expect(screen.queryByText('admin@example.com')).not.toBeInTheDocument()
    })
    expect(screen.getByText('暂无令牌')).toBeInTheDocument()
  })

  it('unlocks a locked IP from the lockout table', async () => {
    setupSecurityResponses()
    const { user } = renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('203.0.113.10')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '解除锁定' }))

    await waitFor(() => {
      expect(screen.queryByText('203.0.113.10')).not.toBeInTheDocument()
    })
    expect(screen.getByText('暂无锁定 IP')).toBeInTheDocument()
  })
})
