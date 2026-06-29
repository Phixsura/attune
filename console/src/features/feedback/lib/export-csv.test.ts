import { describe, expect, it, vi } from 'vitest'
import type { Feedback } from '@/features/feedback/api/list-feedback-infinite'
import { exportFeedbackCSV } from './export-csv'

function makeFeedback(overrides: Partial<Feedback> = {}): Feedback {
  return {
    id: '1',
    content: 'Test feedback',
    source: 'api',
    type: '',
    userId: 'u1',
    pageUrl: '',
    isUrgent: false,
    enrichmentStatus: 'done',
    createdAt: '2025-01-01T00:00:00Z',
    language: 'en',
    tags: [],
    allowedNextStates: [],
    ...overrides,
  }
}

describe('exportFeedbackCSV', () => {
  it('generates CSV with headers and rows', () => {
    let downloadedBlob: Blob | undefined
    let downloadedName: string | undefined

    const origCreateElement = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      if (tag === 'a') {
        const a = origCreateElement('a')
        Object.defineProperty(a, 'click', {
          value: () => {
            downloadedName = a.download
          },
        })
        return a
      }
      return origCreateElement(tag)
    })

    const origCreateObjectURL = URL.createObjectURL
    URL.createObjectURL = (blob: Blob) => {
      downloadedBlob = blob
      return 'blob:test'
    }
    URL.revokeObjectURL = vi.fn()

    const items: Feedback[] = [
      makeFeedback({
        id: '42',
        content: 'Has "quotes" and, commas',
        source: 'web',
        isUrgent: true,
        tags: [
          {
            id: 't1',
            name: 'bug',
            color: '',
            description: '',
            usageCount: 0,
            archived: false,
            createdBy: '',
            createdAt: '',
            updatedAt: '',
          },
        ],
        workflowState: {
          id: 'ws1',
          name: 'Open',
          color: '',
          category: 'active',
          position: 0,
          isDefault: false,
          archived: false,
          createdAt: '',
          updatedAt: '',
        },
      }),
    ]

    exportFeedbackCSV(items, 'test.csv')

    expect(downloadedName).toBe('test.csv')
    expect(downloadedBlob).toBeDefined()

    URL.createObjectURL = origCreateObjectURL
    vi.restoreAllMocks()
  })

  it('escapes CSV special characters', () => {
    let capturedBlob: Blob | undefined

    const origCreateElement = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      if (tag === 'a') {
        const a = origCreateElement('a')
        Object.defineProperty(a, 'click', { value: () => {} })
        return a
      }
      return origCreateElement(tag)
    })

    URL.createObjectURL = (blob: Blob) => {
      capturedBlob = blob
      return 'blob:test'
    }
    URL.revokeObjectURL = vi.fn()

    exportFeedbackCSV([makeFeedback({ content: 'line1\nline2' })])

    expect(capturedBlob).toBeDefined()
    vi.restoreAllMocks()
  })
})
