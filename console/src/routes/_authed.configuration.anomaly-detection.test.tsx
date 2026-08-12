import { screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { AnomalyConfig } from '@/features/anomalies/api/anomalies'
import { renderWithProviders } from '@/testing/test-utils'
import { AnomalyConfigPage } from './_authed.configuration.anomaly-detection'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => (opts: unknown) => opts,
  }
})

const sampleConfig: AnomalyConfig = {
  customSlices: [
    {
      definitionJson: '{"conditions":[{"field":"source","values":["api"]}]}',
      enabled: false,
      id: 's1',
      lastError: 'dimension deleted',
      name: 'api criticals',
    },
  ],
  detectionEnabled: true,
  dropEnabledSliceTypes: ['total', 'source'],
  enabledSliceTypes: ['total', 'source', 'dimension'],
  minCount: 10,
  notifyMode: 'immediate',
  sensitivity: 'medium',
  settleDelayHours: 3,
}

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api-client', () => ({ api: apiMock }))

describe('AnomalyConfigPage', () => {
  it('renders defaults, custom slices, and error badges', async () => {
    apiMock.mockResolvedValue({ config: sampleConfig })
    renderWithProviders(<AnomalyConfigPage />)

    await waitFor(() => expect(screen.getByTestId('anomaly-config-page')).toBeInTheDocument())
    expect((screen.getByTestId('min-count') as HTMLInputElement).value).toBe('10')
    expect((screen.getByTestId('settle-delay') as HTMLInputElement).value).toBe('3')
    expect(screen.getByTestId('custom-slice-list')).toBeInTheDocument()
    expect(screen.getByText('api criticals')).toBeInTheDocument()
    expect(screen.getByTestId('slice-error-badge')).toHaveTextContent('dimension deleted')
  })

  it('posts the edited config on save', async () => {
    apiMock.mockResolvedValue({ config: sampleConfig })
    const { user } = renderWithProviders(<AnomalyConfigPage />)
    await waitFor(() => expect(screen.getByTestId('anomaly-config-page')).toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: '低' }))
    apiMock.mockClear()
    apiMock.mockResolvedValue({ config: { ...sampleConfig, sensitivity: 'low' } })
    await user.click(screen.getByTestId('save-config'))

    await waitFor(() => {
      expect(apiMock).toHaveBeenCalledWith(
        '/fb/v1/console/anomaly-config',
        expect.objectContaining({
          body: expect.objectContaining({
            config: expect.objectContaining({ sensitivity: 'low' }),
          }),
          method: 'POST',
        }),
      )
    })
  })

  it('surfaces the server warning as a toast on save', async () => {
    const { toast } = await import('sonner')
    const warnSpy = vi.spyOn(toast, 'warning')
    apiMock.mockResolvedValue({ config: sampleConfig })
    const { user } = renderWithProviders(<AnomalyConfigPage />)
    await waitFor(() => expect(screen.getByTestId('anomaly-config-page')).toBeInTheDocument())

    apiMock.mockResolvedValue({
      config: sampleConfig,
      warning: 'detection is paused while historical volume is re-computed',
    })
    await user.click(screen.getByTestId('save-config'))
    await waitFor(() => {
      expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining('paused'), expect.anything())
    })
  })

  it('shows the empty state without custom slices', async () => {
    apiMock.mockResolvedValue({ config: { ...sampleConfig, customSlices: [] } })
    renderWithProviders(<AnomalyConfigPage />)
    await waitFor(() => expect(screen.getByTestId('no-custom-slices')).toBeInTheDocument())
  })
})
