# @phixsura/attune

Official Node / TypeScript client for the [attune](https://github.com/Phixsura/attune)
feedback **ingest** API. ESM-first, CommonJS-compatible, zero runtime
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
})
```

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
jitter) that honors a `Retry-After` response header. A `409 REQUEST_IN_PROGRESS`
(a concurrent request with the same idempotency key is in flight) is retried so
the in-flight call can finish; a `409 IDEMPOTENCY_CONFLICT` and other
deterministic client errors (`400`, `401`, `403`, `404`, `422`, …) are never
retried. This policy is shared verbatim with the Go SDK.

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

## Browser use & key safety

The SDK runs in the browser with no special flag, so you can ingest directly
from a web widget (`source: 'web'`, `pageUrl`). **Only ship an `ingest:write`
key to the browser.** Such a key is a *publishable* credential — like a Segment
write key or a Sentry DSN — and is safe in client code precisely because it is
narrowly scoped. Protect it with the per-key rate limit and IP/origin allowlist
in the attune console. Never put a broader-scope key in client-side code.

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
deployment: set `ATTUNE_E2E_BASE_URL` and `ATTUNE_E2E_API_KEY` first.

## License

Apache-2.0
