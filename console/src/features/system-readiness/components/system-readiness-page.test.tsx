import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import type { PreflightReport } from '@/features/system-readiness/api/get-preflight'
import { SystemReadinessPage } from '@/features/system-readiness/components/system-readiness-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

const allPassReport: PreflightReport = {
  status: 'pass',
  elapsed: '42ms',
  checks: [
    { name: 'config:parse', category: 'config', status: 'pass', message: 'OK' },
    { name: 'database:connectivity', category: 'database', status: 'pass', message: 'Connected' },
    { name: 'migration:pending', category: 'migration', status: 'pass', message: 'All applied' },
    { name: 'encryption:tink_keyset', category: 'encryption', status: 'pass', message: 'Loaded' },
    { name: 'auth:session_key', category: 'auth', status: 'pass', message: 'Present' },
    { name: 'metrics:registry', category: 'metrics', status: 'pass', message: 'Healthy' },
    { name: 'worker:enricher', category: 'worker', status: 'pass', message: '3 workers' },
    {
      name: 'backup:restore_drill',
      category: 'backup',
      status: 'pass',
      message: 'Last restore drill passed (today)',
    },
  ],
}

const mixedReport: PreflightReport = {
  status: 'fail',
  elapsed: '105ms',
  checks: [
    { name: 'config:parse', category: 'config', status: 'pass', message: 'OK' },
    {
      name: 'database:connectivity',
      category: 'database',
      status: 'fail',
      message: 'Connection refused',
      remediation: 'Check database.url in config',
    },
    {
      name: 'worker:enricher',
      category: 'worker',
      status: 'warn',
      message: 'Workers set to 0',
      remediation: 'Set enricher.workers to a positive integer',
    },
    {
      name: 'auth:oidc_reachable',
      category: 'auth',
      status: 'skipped',
      message: 'OIDC not configured',
    },
  ],
}

function useMock(report: PreflightReport) {
  server.use(http.get('/fb/v1/console/system/preflight', () => HttpResponse.json(report)))
}

describe('SystemReadinessPage', () => {
  it('renders all-pass status card with pass title', async () => {
    useMock(allPassReport)
    renderWithProviders(<SystemReadinessPage />)

    await waitFor(() => {
      expect(screen.getByText('所有检查通过')).toBeInTheDocument()
    })
  })

  it('renders fail status with issues section and remediation', async () => {
    useMock(mixedReport)
    renderWithProviders(<SystemReadinessPage />)

    await waitFor(() => {
      expect(screen.getByText('存在失败检查项')).toBeInTheDocument()
    })

    expect(screen.getByText('需要关注')).toBeInTheDocument()
    expect(screen.getAllByText('database:connectivity')).toHaveLength(2)
    expect(screen.getByText('Check database.url in config')).toBeInTheDocument()
  })

  it('renders category headers in check details', async () => {
    useMock(allPassReport)
    renderWithProviders(<SystemReadinessPage />)

    await waitFor(() => {
      expect(screen.getByText('配置')).toBeInTheDocument()
    })
    expect(screen.getByText('数据库')).toBeInTheDocument()
    expect(screen.getByText('Worker')).toBeInTheDocument()
    expect(screen.getByText('备份')).toBeInTheDocument()
  })

  it('hides issues section when all checks pass', async () => {
    useMock(allPassReport)
    renderWithProviders(<SystemReadinessPage />)

    await waitFor(() => {
      expect(screen.getByText('所有检查通过')).toBeInTheDocument()
    })
    expect(screen.queryByText('需要关注')).not.toBeInTheDocument()
  })

  it('shows error card on fetch failure', async () => {
    server.use(
      http.get('/fb/v1/console/system/preflight', () =>
        HttpResponse.json({ error: 'internal' }, { status: 500 }),
      ),
    )
    renderWithProviders(<SystemReadinessPage />)

    await waitFor(() => {
      expect(screen.getByText('加载预检报告失败')).toBeInTheDocument()
    })
  })

  it('shows warn and fail pill badges for mixed report', async () => {
    useMock(mixedReport)
    renderWithProviders(<SystemReadinessPage />)

    await waitFor(() => {
      expect(screen.getByText('存在失败检查项')).toBeInTheDocument()
    })

    expect(screen.getByText(/1\s+告警/)).toBeInTheDocument()
    expect(screen.getByText(/1\s+失败/)).toBeInTheDocument()
  })

  it('renders skipped checks in details without issue cards', async () => {
    useMock(mixedReport)
    renderWithProviders(<SystemReadinessPage />)

    await waitFor(() => {
      expect(screen.getByText('auth:oidc_reachable')).toBeInTheDocument()
    })

    const remediations = screen.getAllByText('修复建议')
    expect(remediations).toHaveLength(2)
  })
})
