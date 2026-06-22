import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { ClassificationSettingsPage } from '@/features/settings/components/classification-settings-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('ClassificationSettingsPage', () => {
  it('renders hero metrics and preview empty state from the enrich config query', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: {
            promptTemplate: 'Prompt',
            defaultPromptTemplate: 'Prompt',
            dimensions: [
              {
                name: 'severity',
                displayName: { entries: { default: 'Severity' } },
                kind: 'single',
                taxonomy: [{ value: 'P0', displayName: { entries: { default: 'P0' } } }],
                urgentSet: ['P0'],
                required: false,
                examples: [],
                extractionHint: '',
              },
            ],
          },
        }),
      ),
      http.get('/fb/v1/console/eval/suggestions', () =>
        HttpResponse.json({
          suggestions: [],
          coverage: [],
        }),
      ),
    )

    renderWithProviders(<ClassificationSettingsPage />)

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'AI 分类设置' })).toBeInTheDocument()
    })
    expect(screen.getByText('维度总数')).toBeInTheDocument()
    expect(screen.getByText('白名单维度')).toBeInTheDocument()
    expect(screen.getByText('紧急规则维度')).toBeInTheDocument()
    expect(screen.getByText('还没有生成预览')).toBeInTheDocument()
    expect(screen.getAllByText('分类提示词').length).toBeGreaterThanOrEqual(1)
  })
})
