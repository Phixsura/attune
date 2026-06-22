import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { TagsPage } from '@/features/tags/components/tags-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('TagsPage', () => {
  it('renders hero metrics and registry card from the tags query', async () => {
    server.use(
      http.get('/fb/v1/console/tags', () =>
        HttpResponse.json({
          tags: [
            {
              id: 'tag-1',
              name: 'Bug',
              color: '#ef4444',
              description: 'Bug report',
              exclusiveScope: 'issue_type',
              usageCount: '3',
              archived: false,
            },
          ],
        }),
      ),
    )

    renderWithProviders(<TagsPage />)

    await waitFor(() => {
      expect(
        screen.getByText('统一查看 1 个标签的用途、互斥范围和实际使用情况。'),
      ).toBeInTheDocument()
    })
    expect(screen.getByRole('heading', { name: '标签管理' })).toBeInTheDocument()
    expect(screen.getByText('标签总数')).toBeInTheDocument()
    expect(screen.getByText('已在使用')).toBeInTheDocument()
    expect(screen.getByText('标签目录')).toBeInTheDocument()
    expect(screen.getByText('标签治理建议')).toBeInTheDocument()
    expect(screen.getAllByText('Bug').length).toBeGreaterThanOrEqual(1)
  })
})
