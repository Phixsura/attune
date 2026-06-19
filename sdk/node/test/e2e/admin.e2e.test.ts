// Real end-to-end suite for the tag + workflow-config surface (the API-key
// admin routes). Exercises the published SDK against a LIVE attune server over
// HTTP. Skipped unless ATTUNE_E2E_BASE_URL + ATTUNE_E2E_API_KEY are set (the
// scripts/e2e.sh harness boots a server and sets them, plus a restricted key
// and a second-tenant key for the scope/isolation tests).
import { describe, expect, it } from 'vitest'
import { AttuneError, Client } from '../../src/index'

const baseURL = process.env.ATTUNE_E2E_BASE_URL
const apiKey = process.env.ATTUNE_E2E_API_KEY
const restrictedKey = process.env.ATTUNE_E2E_RESTRICTED_KEY
const tenant2Key = process.env.ATTUNE_E2E_TENANT2_KEY
const live = describe.skipIf(!baseURL || !apiKey)
const BASE = baseURL ?? ''
const KEY = apiKey ?? ''
const MARKER = process.env.ATTUNE_E2E_MARKER ?? 'sdk-e2e'

live('tag + workflow config against a live attune server', () => {
  const client = () => new Client({ baseURL: BASE, apiKey: KEY })

  describe('tag CRUD', () => {
    it('create → list → update → archive round-trips', async () => {
      const c = client()
      const name = `sdk-tag-${MARKER}`

      const created = await c.createTag({ name })
      expect(created.id).toBeTruthy()
      expect(created.name).toBe(name)

      const list = await c.listTags()
      expect(list.tags.some((t) => t.id === created.id)).toBe(true)

      // Update is replace-semantics — send the full desired state (name + a
      // valid hex color, since a nil color would fail the hex constraint).
      const updated = await c.updateTag({
        id: created.id,
        name: `${name}-v2`,
        color: '#3b82f6',
      })
      expect(updated.name).toBe(`${name}-v2`)

      const archived = await c.archiveTag(created.id)
      expect(archived).toBeDefined()
    })

    it('rejects an empty tag name with a 4xx validation envelope', async () => {
      const err = await client()
        .createTag({ name: '' })
        .catch((e: unknown) => e)
      expect(err).toBeInstanceOf(AttuneError)
      const ae = err as AttuneError
      expect(ae.status).toBeGreaterThanOrEqual(400)
      expect(ae.status).toBeLessThan(500)
      expect(ae.code).toBeTruthy()
    })
  })

  describe('workflow config', () => {
    it('seeds defaults, then lists states + transitions', async () => {
      const c = client()
      await c.seedWorkflowDefaults()
      const states = await c.listWorkflowStates()
      expect(states.states.length).toBeGreaterThan(0)
      const transitions = await c.listWorkflowTransitions()
      expect(transitions).toBeDefined()
    })

    it('rejects a transition to an unknown state with 400 (cross-tenant guard)', async () => {
      const err = await client()
        .replaceWorkflowTransitions({
          transitions: [
            {
              fromStateId: '00000000-0000-0000-0000-000000000000',
              toStateId: '00000000-0000-0000-0000-000000000001',
            },
          ],
        })
        .catch((e: unknown) => e)
      expect(err).toBeInstanceOf(AttuneError)
      expect((err as AttuneError).status).toBe(400)
    })

    it('create → update → archive a workflow state round-trips', async () => {
      const c = client()
      const created = await c.createWorkflowState({
        name: 'sdk_state',
        color: '#3b82f6',
        category: 'active',
        position: 99,
        displayName: { entries: { en: 'SDK State' } },
      })
      const id = created.state?.id
      expect(id).toBeTruthy()
      if (!id) throw new Error('created state has no id')

      await c.updateWorkflowState({
        id,
        color: '#22c55e',
        position: 98,
        displayName: { entries: { en: 'SDK State v2' } },
      })

      const archived = await c.archiveWorkflowState(id)
      expect(archived).toBeDefined()
    })
  })

  describe.skipIf(!restrictedKey)('scope gating', () => {
    const RKEY = restrictedKey ?? ''
    const restricted = () => new Client({ baseURL: BASE, apiKey: RKEY })

    it('a key scoped to ingest:write only is FORBIDDEN on tag writes', async () => {
      const err = await restricted()
        .createTag({ name: 'should-be-denied' })
        .catch((e: unknown) => e)
      expect(err).toBeInstanceOf(AttuneError)
      expect(err).toMatchObject({ code: 'FORBIDDEN', status: 403 })
    })

    it('the same key is FORBIDDEN on workflow writes', async () => {
      const err = await restricted()
        .seedWorkflowDefaults()
        .catch((e: unknown) => e)
      expect((err as AttuneError).status).toBe(403)
    })

    it('the same key CAN still ingest — its actual scope works', async () => {
      const res = await restricted().ingest({ content: `${MARKER} restricted-can-ingest` })
      expect(res.id).toMatch(/^\d+$/)
    })
  })

  describe.skipIf(!tenant2Key)('cross-tenant isolation', () => {
    const TKEY = tenant2Key ?? ''

    it('tenant-2 cannot see or archive tenant-1 tags', async () => {
      const c1 = client()
      const c2 = new Client({ baseURL: BASE, apiKey: TKEY })

      const tag = await c1.createTag({ name: `iso-${MARKER}` })

      // tenant-2 must NOT see tenant-1's tag.
      const list2 = await c2.listTags({ includeArchived: true })
      expect(list2.tags.some((t) => t.id === tag.id)).toBe(false)

      // tenant-2 archiving tenant-1's tag must not affect it (scoped by tenant).
      await c2.archiveTag(tag.id).catch(() => undefined)
      const list1 = await c1.listTags()
      expect(list1.tags.some((t) => t.id === tag.id)).toBe(true)
    })
  })
})
