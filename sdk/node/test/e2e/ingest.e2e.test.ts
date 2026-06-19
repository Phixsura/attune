// Real end-to-end suite: exercises the published SDK against a LIVE attune
// server over HTTP. Skipped unless ATTUNE_E2E_BASE_URL + ATTUNE_E2E_API_KEY are
// set (the scripts/e2e.sh harness boots a server and sets them). Env-driven, so
// it also runs against any real deployment:
//
//   ATTUNE_E2E_BASE_URL=https://attune.example.com \
//   ATTUNE_E2E_API_KEY=fbk_live_... \
//   pnpm exec vitest run test/e2e
import { describe, expect, it } from 'vitest'
import { AttuneError, Client } from '../../src/index'

const baseURL = process.env.ATTUNE_E2E_BASE_URL
const apiKey = process.env.ATTUNE_E2E_API_KEY
const live = describe.skipIf(!baseURL || !apiKey)
// Narrowed for use inside the suite (only runs when both are set).
const BASE = baseURL ?? ''
const KEY = apiKey ?? ''

// Tag content so the harness can find these rows in Postgres afterwards.
export const E2E_MARKER = process.env.ATTUNE_E2E_MARKER ?? 'sdk-e2e'

live('ingest against a live attune server', () => {
  const client = () => new Client({ baseURL: BASE, apiKey: KEY })

  describe('happy paths', () => {
    it('ingests minimal { content } and returns a numeric id + pending', async () => {
      const res = await client().ingest({ content: `${E2E_MARKER} minimal` })
      expect(res.enrichmentStatus).toBe('pending')
      expect(res.id).toMatch(/^\d+$/) // int64 serialized as a numeric string
    })

    it('ingests all optional fields (source=web, sourceUser, pageUrl, sourceMeta)', async () => {
      const res = await client().ingest({
        content: `${E2E_MARKER} full-fields`,
        source: 'web',
        sourceUser: 'e2e-user-42',
        pageUrl: 'https://app.example.com/settings',
        sourceMeta: { plan: 'pro', tries: 3 },
      })
      expect(res.id).toMatch(/^\d+$/)
      expect(res.enrichmentStatus).toBe('pending')
    })

    it('assigns distinct, increasing ids to sequential ingests', async () => {
      const a = await client().ingest({ content: `${E2E_MARKER} seq-a` })
      const b = await client().ingest({ content: `${E2E_MARKER} seq-b` })
      // Compare as BigInt — id is an int64 string; Number() would lose precision.
      expect(BigInt(b.id) > BigInt(a.id)).toBe(true)
    })

    it('accepts unicode content', async () => {
      const res = await client().ingest({ content: `${E2E_MARKER} café Ångström 🎉 €` })
      expect(res.id).toMatch(/^\d+$/)
    })

    it('accepts content at the 5000-char cap', async () => {
      const res = await client().ingest({ content: 'a'.repeat(5000) })
      expect(res.id).toMatch(/^\d+$/)
    })

    it('accepts every documented source', async () => {
      for (const source of ['api', 'webhook', 'email', 'web', 'other'] as const) {
        const res = await client().ingest({ content: `${E2E_MARKER} src-${source}`, source })
        expect(res.id).toMatch(/^\d+$/)
      }
    })
  })

  describe('server validation → AttuneError (no retry)', () => {
    it('rejects empty content with VALIDATION / 400', async () => {
      await expect(client().ingest({ content: '' })).rejects.toMatchObject({
        name: 'AttuneError',
        code: 'VALIDATION',
        status: 400,
      })
    })

    it('rejects content over the 5000-char cap with VALIDATION / 400', async () => {
      await expect(client().ingest({ content: 'a'.repeat(5001) })).rejects.toMatchObject({
        code: 'VALIDATION',
        status: 400,
      })
    })

    it('rejects an unknown source with VALIDATION / 400', async () => {
      await expect(
        client().ingest({ content: `${E2E_MARKER} bad-source`, source: 'totally-bogus' }),
      ).rejects.toMatchObject({ code: 'VALIDATION', status: 400 })
    })
  })

  describe('auth', () => {
    it('rejects an unknown api key with UNAUTHORIZED / 401 and a requestId', async () => {
      const bad = new Client({ baseURL: BASE, apiKey: 'fbk_live_definitely_invalid' })
      const err = await bad.ingest({ content: 'x' }).catch((e: unknown) => e)
      expect(err).toBeInstanceOf(AttuneError)
      expect(err).toMatchObject({ code: 'UNAUTHORIZED', status: 401 })
      expect((err as AttuneError).requestId).toBeTruthy()
    })
  })

  describe('idempotency', () => {
    it('replaying the same Idempotency-Key returns the same id (no duplicate row)', async () => {
      const key = `e2e-idem-${E2E_MARKER}-replay`
      const c = client()
      const first = await c.ingest(
        { content: `${E2E_MARKER} idem-replay` },
        { idempotencyKey: key },
      )
      const second = await c.ingest(
        { content: `${E2E_MARKER} idem-replay` },
        { idempotencyKey: key },
      )
      expect(second.id).toBe(first.id)
    })

    it('N CONCURRENT ingests with the same key collapse to one row (same id)', async () => {
      const key = `e2e-idem-${E2E_MARKER}-concurrent`
      const c = client()
      const results = await Promise.all(
        Array.from({ length: 8 }, () =>
          c.ingest({ content: `${E2E_MARKER} idem-concurrent` }, { idempotencyKey: key }),
        ),
      )
      const ids = new Set(results.map((r) => r.id))
      expect(ids.size).toBe(1) // all 8 concurrent calls resolved to the same row
    })

    it('same key with a different body → 409 IDEMPOTENCY_CONFLICT', async () => {
      const key = `e2e-idem-${E2E_MARKER}-conflict`
      const c = client()
      await c.ingest({ content: `${E2E_MARKER} idem-A` }, { idempotencyKey: key })
      await expect(
        c.ingest({ content: `${E2E_MARKER} idem-B-different` }, { idempotencyKey: key }),
      ).rejects.toMatchObject({ code: 'IDEMPOTENCY_CONFLICT', status: 409 })
    })
  })

  describe('transport failures', () => {
    it('surfaces a connection failure as code NETWORK', async () => {
      const dead = new Client({ baseURL: 'http://127.0.0.1:1', apiKey: KEY, maxRetries: 0 })
      await expect(dead.ingest({ content: 'x' })).rejects.toMatchObject({ code: 'NETWORK' })
    })

    it('surfaces a per-attempt timeout as code TIMEOUT', async () => {
      const slow = new Client({ baseURL: BASE, apiKey: KEY, timeout: 1, maxRetries: 0 })
      await expect(slow.ingest({ content: 'x' })).rejects.toMatchObject({ code: 'TIMEOUT' })
    })

    it('honors a pre-aborted signal with code ABORTED', async () => {
      const controller = new AbortController()
      controller.abort()
      await expect(
        client().ingest({ content: 'x' }, { signal: controller.signal }),
      ).rejects.toMatchObject({ code: 'ABORTED' })
    })
  })
})
