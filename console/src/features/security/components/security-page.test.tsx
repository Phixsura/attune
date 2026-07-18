// SPDX-License-Identifier: Apache-2.0

import { HttpResponse, http } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'
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

function makeToken(overrides: Partial<BreakGlassToken> = {}): BreakGlassToken {
  return {
    ...activeToken,
    ...overrides,
  }
}

function apiFailure(message: string, status = 500) {
  return HttpResponse.json({ code: 'FAILED', message }, { status })
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
    http.post('/fb/v1/console/auth/breakglass/issue', async ({ request }) => {
      const body = (await request.json()) as {
        admin_email: string
        ttl_minutes: number
        allowed_ips?: string[]
      }
      const issuedToken: BreakGlassToken = {
        id: `token-${currentTokens.length + 1}`,
        admin_email: body.admin_email,
        expires_at: '2099-07-06T12:30:00Z',
        issued_by: 'issuer-1',
        issued_at: '2026-07-05T11:30:00Z',
        status: 'valid',
        allowed_ips: body.allowed_ips ?? [],
      }
      currentTokens = [...currentTokens, issuedToken]
      return HttpResponse.json({
        token: issuedToken,
        raw_token: 'bg-issued-token',
        expires_at: issuedToken.expires_at,
      })
    }),
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
    http.post('/fb/v1/console/auth/breakglass/tokens/:tokenId/revoke', ({ params }) => {
      currentTokens = currentTokens.map((token) =>
        token.id === String(params.tokenId)
          ? {
              ...token,
              revoked_at: '2026-07-05T12:05:00Z',
              revoked_by: 'issuer-1',
              status: 'revoked',
            }
          : token,
      )
      return new HttpResponse(null, { status: 204 })
    }),
    http.post('/fb/v1/console/auth/breakglass/lockouts/:ip/unlock', ({ params }) => {
      currentLockouts = currentLockouts.filter((row) => row.ip !== String(params.ip))
      return new HttpResponse(null, { status: 204 })
    }),
  )
}

describe('SecurityPage', () => {
  beforeEach(() => {
    toastError.mockClear()
    toastSuccess.mockClear()
  })

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

  it('covers issue, copy, revoke-all, and cutover failure flows', async () => {
    setupSecurityResponses({ tokens: [], lockouts: [], mode: 'hybrid' })
    server.use(
      http.post('/fb/v1/console/auth/sso/cutover', () =>
        HttpResponse.json({
          success: false,
          message: 'SSO cutover blocked by preflight checks',
          preflight: {
            status: 'fail',
            checks: [
              {
                name: 'sso:oidc_enabled',
                status: 'pass',
                message: 'OIDC configured',
              },
              {
                name: 'sso:redirect_uri_match',
                status: 'fail',
                message: 'redirect mismatch',
                remediation: 'Fix redirect URI',
              },
            ],
          },
        }),
      ),
    )

    const writeSpy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)
    const { user } = renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('暂无令牌')).toBeInTheDocument()
    })
    expect(screen.getByText('暂无锁定 IP')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '撤销全部活跃令牌' })).toBeDisabled()

    const issueButtons = screen.getAllByRole('button', { name: '签发令牌' })
    await user.click(issueButtons[issueButtons.length - 1])
    const issueDialog = await screen.findByRole('dialog')
    await user.type(within(issueDialog).getByLabelText('管理员邮箱'), 'ops@example.com')
    await user.type(within(issueDialog).getByLabelText(/IP 白名单/), '203.0.113.0/24')
    await user.click(within(issueDialog).getByRole('button', { name: '签发令牌' }))

    const tokenDialog = await screen.findByRole('dialog')
    await user.click(within(tokenDialog).getAllByRole('button')[0])
    expect(writeSpy).toHaveBeenCalledWith(expect.stringContaining('bg-issued-token'))
    expect(toastSuccess).toHaveBeenCalledWith('已复制')

    await user.click(within(tokenDialog).getByRole('button', { name: '完成' }))
    await waitFor(() => {
      expect(screen.getByText('ops@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '撤销全部活跃令牌' }))
    const revokeDialog = await screen.findByRole('dialog')
    await user.click(within(revokeDialog).getByRole('button', { name: '撤销全部活跃令牌' }))

    await waitFor(() => {
      expect(screen.getByText('暂无令牌')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '强制 SSO' }))
    const cutoverDialog = await screen.findByRole('dialog')
    expect(within(cutoverDialog).getByText('无活跃令牌')).toBeInTheDocument()
    await user.click(within(cutoverDialog).getByRole('button', { name: '切换至 SSO' }))

    await waitFor(() => {
      expect(screen.getByText('预检失败')).toBeInTheDocument()
    })
    expect(screen.getByText('OIDC configured')).toBeInTheDocument()
    expect(screen.getByText('redirect mismatch')).toBeInTheDocument()

    await user.click(within(cutoverDialog).getByRole('checkbox', { name: '跳过 break-glass 检查' }))
    await user.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '强制 SSO' }))
    const resetDialog = await screen.findByRole('dialog')
    expect(within(resetDialog).queryByText('预检失败')).not.toBeInTheDocument()
    expect(
      within(resetDialog).getByRole('checkbox', { name: '跳过 break-glass 检查' }),
    ).not.toBeChecked()
    await user.click(within(resetDialog).getByRole('button', { name: '取消' }))
  })

  it('switches back to hybrid mode from the SSO-only action path', async () => {
    setupSecurityResponses({ tokens: [], lockouts: [], mode: 'sso_only' })
    let currentMode: 'hybrid' | 'sso_only' = 'sso_only'
    server.use(
      http.get('/fb/v1/console/auth/sso/mode', () => HttpResponse.json({ mode: currentMode })),
      http.post('/fb/v1/console/auth/sso/fallback', () => {
        currentMode = 'hybrid'
        return HttpResponse.json({ success: true, message: 'Switched to hybrid mode' })
      }),
    )

    const { user } = renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('仅 SSO')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '启用密码登录' }))
    const fallbackDialog = await screen.findByRole('dialog')
    await user.click(within(fallbackDialog).getByRole('button', { name: '启用密码' }))

    await waitFor(() => {
      expect(screen.getByText('混合模式')).toBeInTheDocument()
    })
  })

  it('switches to SSO-only mode after a successful cutover', async () => {
    let currentMode: 'hybrid' | 'sso_only' = 'hybrid'
    let cutoverRequest: { skip_breakglass_check?: boolean } = {}
    setupSecurityResponses({ lockouts: [], mode: currentMode })
    server.use(
      http.get('/fb/v1/console/auth/sso/mode', () => HttpResponse.json({ mode: currentMode })),
      http.post('/fb/v1/console/auth/sso/cutover', async ({ request }) => {
        cutoverRequest = (await request.json()) as { skip_breakglass_check?: boolean }
        currentMode = 'sso_only'
        return HttpResponse.json({ success: true, message: 'Switched to SSO-only mode' })
      }),
    )

    const { user } = renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('混合模式')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '强制 SSO' }))
    const cutoverDialog = await screen.findByRole('dialog')
    await user.click(within(cutoverDialog).getByRole('checkbox', { name: '跳过 break-glass 检查' }))
    await user.click(within(cutoverDialog).getByRole('button', { name: '切换至 SSO' }))

    await waitFor(() => {
      expect(toastSuccess).toHaveBeenCalledWith('已切换至仅 SSO 模式')
    })
    expect(cutoverRequest.skip_breakglass_check).toBe(true)
    await waitFor(() => {
      expect(screen.getByText('仅 SSO')).toBeInTheDocument()
    })
  })

  it('paginates and revokes a single break-glass token', async () => {
    const tokens = Array.from({ length: 12 }, (_, index) =>
      makeToken({
        id: `token-${index + 1}`,
        admin_email: `pager-${index + 1}@example.com`,
      }),
    )
    setupSecurityResponses({ tokens, lockouts: [] })
    const { user } = renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('pager-1@example.com')).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: '上一页' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '下一页' })).toBeEnabled()

    await user.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => {
      expect(screen.getByText('pager-11@example.com')).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: '上一页' }))
    await waitFor(() => {
      expect(screen.getByText('pager-1@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '撤销令牌 pager-1@example.com' }))
    await waitFor(() => {
      expect(toastSuccess).toHaveBeenCalledWith('令牌已撤销')
    })
    await waitFor(() => {
      expect(
        screen.queryByRole('button', { name: '撤销令牌 pager-1@example.com' }),
      ).not.toBeInTheDocument()
    })
  })

  it('sorts break-glass lockouts by soonest unlock time', async () => {
    setupSecurityResponses({
      lockouts: [
        {
          ip: '203.0.113.20',
          locked_until: '2026-07-05T16:00:00Z',
          remaining_mins: 70,
          attempts: 6,
        },
        {
          ip: '203.0.113.11',
          locked_until: '2026-07-05T14:00:00Z',
          remaining_mins: 10,
          attempts: 4,
        },
      ],
    })
    renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('203.0.113.11')).toBeInTheDocument()
    })

    const lockoutRows = screen
      .getAllByRole('row')
      .filter((row) => row.textContent?.includes('203.0.113.'))
    expect(lockoutRows.map((row) => row.textContent)).toEqual([
      expect.stringContaining('203.0.113.11'),
      expect.stringContaining('203.0.113.20'),
    ])
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

  it('keeps issue and revoke-all dialogs open when mutations fail', async () => {
    setupSecurityResponses()
    server.use(
      http.post('/fb/v1/console/auth/breakglass/issue', () => apiFailure('issue denied')),
      http.post('/fb/v1/console/auth/breakglass/tokens/revoke-all', () =>
        apiFailure('revoke all denied'),
      ),
    )
    const { user } = renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '签发令牌' }))
    const issueDialog = await screen.findByRole('dialog')
    await user.type(within(issueDialog).getByLabelText('管理员邮箱'), 'ops@example.com')
    await user.click(within(issueDialog).getByRole('button', { name: '签发令牌' }))

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('issue denied')
    })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.click(within(issueDialog).getByRole('button', { name: '取消' }))

    await user.click(screen.getByRole('button', { name: '撤销全部活跃令牌' }))
    const revokeDialog = await screen.findByRole('dialog')
    await user.click(within(revokeDialog).getByRole('button', { name: '撤销全部活跃令牌' }))

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('revoke all denied')
    })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.click(within(revokeDialog).getByRole('button', { name: '取消' }))
  })

  it('shows errors when single-token revoke and lockout unlock fail', async () => {
    setupSecurityResponses()
    server.use(
      http.post('/fb/v1/console/auth/breakglass/tokens/:tokenId/revoke', () =>
        apiFailure('single revoke denied'),
      ),
      http.post('/fb/v1/console/auth/breakglass/lockouts/:ip/unlock', () =>
        apiFailure('unlock denied'),
      ),
    )
    const { user } = renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '撤销令牌 admin@example.com' }))
    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('single revoke denied')
    })

    await user.click(screen.getByRole('button', { name: '解除锁定' }))
    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('unlock denied')
    })
  })

  it('surfaces cutover errors when the cutover request cannot be parsed', async () => {
    setupSecurityResponses({ lockouts: [] })
    server.use(
      http.post(
        '/fb/v1/console/auth/sso/cutover',
        () => new HttpResponse('not-json', { status: 500 }),
      ),
    )
    const { user } = renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '强制 SSO' }))
    const cutoverDialog = await screen.findByRole('dialog')
    await user.click(within(cutoverDialog).getByRole('button', { name: '切换至 SSO' }))

    await waitFor(() => {
      expect(toastError).toHaveBeenCalled()
    })
  })

  it('keeps the fallback dialog open when switching back to hybrid fails', async () => {
    setupSecurityResponses({ tokens: [], lockouts: [], mode: 'sso_only' })
    server.use(http.post('/fb/v1/console/auth/sso/fallback', () => apiFailure('fallback denied')))
    const { user } = renderWithProviders(<SecurityPage />)

    await waitFor(() => {
      expect(screen.getByText('仅 SSO')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '启用密码登录' }))
    const fallbackDialog = await screen.findByRole('dialog')
    await user.click(within(fallbackDialog).getByRole('button', { name: '启用密码' }))

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('fallback denied')
    })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.click(within(fallbackDialog).getByRole('button', { name: '取消' }))
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
