# @phixsura/attune

Official Node / TypeScript client for the [attune](https://github.com/Phixsura/attune)
ingest and tenant management APIs. ESM-first, CommonJS-compatible, zero runtime
dependencies — uses the platform `fetch`. Runs on Node 20+ and modern browsers.

```bash
npm install @phixsura/attune
# or: pnpm add @phixsura/attune   /   yarn add @phixsura/attune
```

## Usage

```ts
import { Client } from '@phixsura/attune'

const client = new Client({
  baseURL: 'https://attune.example.com',
  apiKey: process.env.ATTUNE_API_KEY!, // ingest:write scope
})

const { id, enrichmentStatus } = await client.ingest({
  content: 'The export button on the billing page is broken',
  source: 'api', // optional; server defaults to "api"
  sourceUser: 'user_123', // optional
  pageUrl: 'https://app.example.com/billing', // optional
  sourceMeta: { plan: 'pro' }, // optional, arbitrary JSON
})

console.log(id, enrichmentStatus) // "4242" "pending"
```

`content` is the only required field; every other field is optional. `id` is
returned as a **string** (the feedback row id is a 64-bit integer, which
JavaScript cannot represent safely as a number).

## Options

```ts
new Client({
  baseURL,
  apiKey,
  timeout: 30_000,  // per-attempt timeout (ms); default 30s
  maxRetries: 2,    // transient-failure retries; default 2
  fetch,            // inject a custom fetch (older runtimes / tests)
  defaultHeaders: { 'x-trace-id': '…' }, // extra headers on every request
})
```

Every request carries a versioned `User-Agent` (`attune-node/<version> node/<ver>`)
so the server can attribute SDK traffic. `defaultHeaders` are added to every
request; reserved headers (`X-API-Key`, `Idempotency-Key`, `User-Agent`,
`content-type`) always take precedence and can't be overridden.

Per-call cancellation:

```ts
const controller = new AbortController()
const p = client.ingest({ content: 'x' }, { signal: controller.signal })
controller.abort() // → throws AttuneError with code "ABORTED"
```

## Errors & retries

Every failure throws an `AttuneError`:

```ts
import { AttuneError } from '@phixsura/attune'

try {
  await client.ingest({ content: 'x' })
} catch (err) {
  if (err instanceof AttuneError) {
    err.code // stable machine code — switch on this, never on message
    err.status // HTTP status (undefined for transport failures)
    err.requestId // for support / log correlation
  }
}
```

`code` is either a server [`ErrorCode`](https://github.com/Phixsura/attune)
value (`"VALIDATION"`, `"UNAUTHORIZED"`, `"RATE_LIMITED"`, …) or a transport
code (`"NETWORK"`, `"TIMEOUT"`, `"ABORTED"`).

The client retries transient failures — HTTP `408` / `429` / `5xx`, network
errors, and timeouts — up to `maxRetries`, with exponential backoff (±25%
jitter) that honors a `Retry-After` response header. Deterministic client errors
(`400`, `401`, `403`, `404`, `409`, `422`, …) are never retried — the only `409`
is `IDEMPOTENCY_CONFLICT` (permanent), and concurrent same-key retries are
deduped server-side rather than bounced back. This policy is shared verbatim
with the Go SDK.

## Idempotency

Ingest creates a row, so a blind retry of a request the server already processed
would duplicate it. The client prevents that: every `ingest()` call sends an
`Idempotency-Key` header (a fresh UUID by default), **held stable across that
call's retries**, and the server returns the original id instead of inserting
again. Pass your own key to dedup across separate calls:

```ts
await client.ingest({ content: 'x' }, { idempotencyKey: 'order-4242-feedback' })
```

Replaying a key with a different body throws `AttuneError` `IDEMPOTENCY_CONFLICT`
(409).

## Management APIs

Beyond ingest, the client can manage a tenant's **tags**, **workflow
configuration**, **audit log queries and exports**, **GDPR jobs and archive
downloads**, **outbox retries**, and **MCP OAuth clients**. These routes need
server-only management keys with the matching scopes:

- `tags:read` / `tags:write`
- `workflow:read` / `workflow:write`
- `audit:read`
- `gdpr:read` / `gdpr:export` / `gdpr:delete`
- `notify:read` / `notify:write`
- `mcpclient:admin`

Do not ship those broader-scope keys to the browser.

```ts
// Tags (tags:read / tags:write)
const { tags } = await client.listTags() // { includeArchived: true } to include archived
const tag = await client.createTag({ name: 'billing', color: '#3b82f6' })
await client.updateTag({ id: tag.id, name: 'billing', color: '#22c55e' }) // replace-semantics
await client.archiveTag(tag.id)

// Workflow config (workflow:read / workflow:write)
await client.seedWorkflowDefaults()
const { states } = await client.listWorkflowStates()
const { state } = await client.createWorkflowState({
  name: 'triage', // stable machine key, ^[a-z][a-z0-9_]{0,30}$
  color: '#3b82f6',
  category: 'active',
  position: 1,
  displayName: { entries: { en: 'Triage' } },
})
await client.updateWorkflowState({ id: state!.id, color: '#22c55e' }) // replace-semantics
await client.archiveWorkflowState(state!.id)

const { transitions } = await client.listWorkflowTransitions()
await client.replaceWorkflowTransitions({ transitions })

// Audit log (audit:read)
const { items } = await client.listAuditLog({ actions: ['tag.create'], limit: 25 })
const csv = await client.exportAuditLogCSV({ actions: ['tag.create'] })
const auditJob = await client.createAuditEvidenceExport({
  from: '2026-06-01T00:00:00Z',
  to: '2026-06-30T00:00:00Z',
  actions: ['tag.create'],
  actorType: '',
  actorId: '',
  targetType: '',
  targetId: '',
})
const auditStatus = await client.getAuditEvidenceExport(auditJob.jobId)
if (auditStatus.downloadPath) {
  await client.downloadAuditEvidenceExport(auditJob.jobId)
}

// GDPR jobs (gdpr:read / gdpr:export / gdpr:delete)
const exportJob = await client.exportGdprSubject({ subjectKey: 'user:123' })
const exportStatus = await client.getGdprExport(exportJob.jobId)
if (exportStatus.downloadPath) {
  await client.downloadGdprExport(exportJob.jobId)
}
await client.revokeGdprExport(exportJob.jobId)
await client.deleteGdprSubject({ subjectKey: 'user:123' })
await client.cancelGdprRequest('req_123')
const requests = await client.listGdprRequests({ limit: 20, requestType: 'export' })
const ops = await client.getGdprOperations()

// Outbox (notify:read / notify:write)
const deliveries = await client.listOutboxDeliveries({ status: ['dead'], limit: 50 })
await client.retryOutboxDelivery(deliveries.deliveries[0]!.id)

// MCP client governance (mcpclient:admin)
const { clients } = await client.listMCPClients()
const created = await client.createMCPClient({
  name: 'ops-agent',
  redirectUris: ['https://example.com/callback'],
  scopes: ['mcp:read'],
})
await client.getMCPClient(created.client!.id)
```

`updateTag` / `updateWorkflowState` are **replace-semantics**: send the full
desired state, not a sparse patch. `GET` / `PUT` / `PATCH` / `DELETE` are
retried on transient failure, and management `POST`s that the server now
deduplicates (`create*`, `seed*`, audit evidence creation, GDPR export/delete/
cancel/revoke, outbox retry, MCP client creation) also auto-retry with a stable
per-call `Idempotency-Key`.

## Browser use & key safety

The SDK runs in the browser with no special flag, so you can ingest directly
from a web widget (`source: 'web'`, `pageUrl`). **Only ship an `ingest:write`
key to the browser**, and only over HTTPS. Such a key is a *publishable*
credential — like a Segment write key or a Sentry DSN — safe in client code
because it is narrowly scoped to one write-only action. A scraped key can still
spam ingest, so treat it as low-trust:

- restrict it with the key's **IP/CIDR allowlist** in the console where the
  origin is predictable;
- a tenant-wide ingest **rate limit** caps total ingest volume (note: this is
  per-tenant, shared by all keys — not per-key today);
- **rotate/revoke** the key if it leaks.

Never put a broader-scope key in client-side code.

## Examples

- [`examples/node-ingest`](./examples/node-ingest) — minimal Node script
- [`examples/browser-ingest`](./examples/browser-ingest) — minimal web widget

## Types

The request/response types are generated from attune's proto contract
(`proto/attune/v1`), so they track the server exactly — they are never
hand-written. Regenerate with `pnpm gen:proto` (runs `make proto` at the repo
root).

## Development

```bash
pnpm install
pnpm test         # unit tests (the live e2e suite auto-skips)
pnpm build        # dual ESM/CJS via tsdown
pnpm e2e          # full e2e: boots Postgres + a real attune server, runs the
                  # live suite against it, checks persistence, then tears down
```

`pnpm test:e2e` runs the env-driven live suite (`test/e2e`) against any existing
deployment: set `ATTUNE_E2E_BASE_URL` and `ATTUNE_E2E_API_KEY` first. `pnpm e2e`
additionally packs the publishable tarball, installs it into a throwaway project,
ingests through it via both ESM and CJS, and bundles it for the browser
(esbuild `platform=browser`, asserting no Node built-ins leak) — so the real
artifact is exercised, not just the source.

## Publishing

Releases are automated by the `SDK Release` workflow
(`.github/workflows/sdk-release.yml`) on an `sdk-vX.Y.Z` tag: it verifies the tag
matches `package.json`'s `version`, runs the type check + tests, and publishes
with provenance (a signed SLSA attestation). The workflow supports either an
`NPM_TOKEN` repo secret or npm trusted publishing for this GitHub repository;
in token mode it also checks that the authenticated npm account is actually an
owner of `@phixsura/attune` before the final publish step. `publishConfig`
forces public access to the public registry and `prepack` builds `dist/` before
packing.

```bash
# cut a release: bump the version, then tag to trigger publish
npm version <patch|minor|major>      # bump package.json + create git commit/tag
git push && git push --tags          # or: git tag sdk-v0.1.1 <sha> && git push origin sdk-v0.1.1
```

`workflow_dispatch` runs the same path as a publish dry-run (build + pack, no
publish) to exercise it without cutting a tag. To inspect the tarball locally:
`npm pack --dry-run`.

## License

Apache-2.0
