import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import {
  downloadEvidenceExport,
  isActiveEvidenceStatus,
  isTerminalEvidenceStatus,
} from '@/features/audit-log/api/evidence-export'
import { server } from '@/testing/mocks/server'

describe('evidence-export', () => {
  describe('isActiveEvidenceStatus', () => {
    it('returns true for queued and running', () => {
      expect(isActiveEvidenceStatus('queued')).toBe(true)
      expect(isActiveEvidenceStatus('running')).toBe(true)
    })

    it('returns false for terminal states', () => {
      expect(isActiveEvidenceStatus('completed')).toBe(false)
      expect(isActiveEvidenceStatus('failed')).toBe(false)
      expect(isActiveEvidenceStatus('expired')).toBe(false)
    })
  })

  describe('isTerminalEvidenceStatus', () => {
    it('returns true for terminal states', () => {
      expect(isTerminalEvidenceStatus('completed')).toBe(true)
      expect(isTerminalEvidenceStatus('failed')).toBe(true)
      expect(isTerminalEvidenceStatus('expired')).toBe(true)
    })

    it('returns false for active states', () => {
      expect(isTerminalEvidenceStatus('queued')).toBe(false)
      expect(isTerminalEvidenceStatus('running')).toBe(false)
    })
  })

  describe('downloadEvidenceExport', () => {
    it('throws on HTTP error with structured message', async () => {
      server.use(
        http.get('/fb/v1/console/audit-log/evidence/:id/download', () =>
          HttpResponse.json({ message: 'job not downloadable' }, { status: 400 }),
        ),
      )
      await expect(downloadEvidenceExport('job-1')).rejects.toThrow('job not downloadable')
    })

    it('throws with HTTP status fallback when no message', async () => {
      server.use(
        http.get(
          '/fb/v1/console/audit-log/evidence/:id/download',
          () => new HttpResponse(null, { status: 404 }),
        ),
      )
      await expect(downloadEvidenceExport('job-2')).rejects.toThrow('HTTP 404')
    })

    it('throws with HTTP status fallback when body is not JSON', async () => {
      server.use(
        http.get('/fb/v1/console/audit-log/evidence/:id/download', () =>
          HttpResponse.text('not json', { status: 500 }),
        ),
      )
      await expect(downloadEvidenceExport('job-3')).rejects.toThrow('HTTP 500')
    })

    it('throws with HTTP status fallback when JSON lacks message field', async () => {
      server.use(
        http.get('/fb/v1/console/audit-log/evidence/:id/download', () =>
          HttpResponse.json({ error: 'something' }, { status: 502 }),
        ),
      )
      await expect(downloadEvidenceExport('job-4')).rejects.toThrow('HTTP 502')
    })
  })
})
