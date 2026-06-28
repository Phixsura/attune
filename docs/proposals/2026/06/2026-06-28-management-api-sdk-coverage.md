# Management API and SDK coverage beyond ingest

| | |
|---|---|
| **Issue** | #168 |
| **Status** | Implemented |
| **Started** | 2026-06-28 |
| **Related** | #36 (Go SDK), #37 (Node/TS SDK), #41 (API key scopes), #43 (GDPR), #80 (enrichment runtime), #93 (MCP server), #153 (MCP governance), #33 (outbox dead queue), #152 (audit evidence export) |

---

## Problem

attune's public API and SDK story is still uneven.

The good news is that the repo is not starting from zero:

- `ingest` is public and supported in both SDKs.
- `tags` and `workflow` already crossed the bridge from console-only handlers to
  API-key routes plus SDK methods.
- `audit`, `gdpr`, `outbox`, `enrichment_runtime`, and `mcp_client` already have
  proto contracts and console handlers.

The gap is that these remaining management surfaces are still shaped primarily
for the Console:

1. **Most of the high-value control plane is session-only.**
   The handlers are mounted under `/fb/v1/console/...` with session auth and
   admin checks, so operators can click them in the UI but automation cannot
   call them through a scoped API key.
2. **The SDKs stop well short of the available contract.**
   Node already generates types for `audit/gdpr/outbox/mcp_client`, but the
   `Client` does not expose methods for them. Go currently generates only
   `common/ingest/tag/workflow`, so the contract and SDK shape have drifted.
3. **The existing scope model is only partly activated.**
   `audit:read`, `notify:read`, `notify:write`, `enrich:read`, and
   `gdpr:admin` already exist, but large parts of the matching API surface are
   not reachable by API keys. The exception is `mcp`: the existing
   `mcp:read/write/ingest` scopes describe tool execution by MCP clients, not
   governance of the OAuth clients themselves.
4. **A naive "expose everything" move would ship the wrong contract.**
   Several console endpoints are not good first public-management APIs:
  `VerifyGdprStepUp` is session-cookie/password based, `DownloadGdprExport` and
  audit evidence downloads are binary, and enrichment runtime is
  deployment-scoped rather than tenant-scoped.

Issue #168 is therefore not "invent more admin endpoints." It is to make the
already-real management plane safely automatable and SDK-backed, without
smuggling the entire Console verbatim into the public contract.

## Goals

- Expand the API-key-authenticated management surface beyond `ingest`,
  prioritizing operations where machine automation is clearly valid.
- Keep the contract **scope-first** and least-privilege.
- Reuse the existing handler and proto investments rather than forking a second
  implementation path.
- Add typed Node and Go SDK methods that follow the existing attune retry and
  error contract.
- Publish canonical public paths that do not bake `/console` into the long-term
  automation contract.
- Add docs and examples that make the difference between publishable ingest keys
  and server-only management keys explicit.

## Non-goals

- **Do not expose every console route.**
  This issue targets selected management operations, not UI parity.
- **Do not make management keys browser-safe.**
  Only `ingest:write` keeps the publishable-key posture. Management keys are
  server-only credentials.
- **Do not add a generic cross-resource pagination framework.**
  Resource-local async iterators / pagers are enough for this issue.
- **Do not redefine the MCP runtime tool scopes.**
  `mcp:read/write/ingest` remain the OAuth-granted scopes for MCP tool use.
- **Do not expose enrichment-runtime APIs over tenant API keys in this issue.**
  Runtime status and mutation are both deferred until the scope and ownership
  model is split from tenant-scoped enrich config.

## Current-state reconciliation

| Surface | Current repository state | Gap for #168 |
|---|---|---|
| Tags / workflow | Public `/v1/...` API-key routes and SDK methods already shipped | Baseline to extend, not part of the gap |
| Audit log | `proto/attune/v1/audit.proto` + console handlers under `/fb/v1/console/audit-log` | No API-key alias; no SDK methods |
| GDPR | `proto/attune/v1/gdpr.proto` + console handlers under `/fb/v1/console/gdpr` | Session-step-up design blocks straightforward machine use; no SDK methods |
| Outbox dead queue | `proto/attune/v1/outbox.proto` + console handlers | Session-only; no SDK methods |
| Enrichment runtime | `proto/attune/v1/enrichment_runtime.proto` + console handlers | Session-only; no SDK methods |
| MCP client governance | `proto/attune/v1/mcp_client.proto` + console handlers | Session-only; wrong scope family for API-key governance; no SDK methods |

Two implementation details matter:

1. Node already generates most of the wire types this issue needs, so the Node
   work is mostly client methods, exports, docs, and tests.
2. Go does not yet generate the admin-surface protos, so #168 must expand the
   SDK Go proto target before the client methods can exist.

## Industry research summary

Primary-source research across Stripe, GitHub, PostHog, Sentry, and
LaunchDarkly points to five stable patterns:

| Project | Pattern to borrow | Why it matters here |
|---|---|---|
| Stripe | Separate restricted management credentials from publishable write credentials; idempotency only where the server supports it | attune should keep `ingest` and management keys mentally separate |
| GitHub | Fine-grained endpoint permissions and a contract that does not leak UI structure | attune should map each public management route to an explicit scope and publish stable `/v1/...` paths |
| PostHog | Public data plane and private control plane are different products; async job APIs use `create -> status -> cancel/download/logs` resources | good fit for attune's GDPR and audit export shape |
| Sentry | Auth, pagination, and rate limiting are part of the API contract, not side notes | API-key management routes must inherit clear limit and cursor behavior |
| LaunchDarkly | Client/app governance is different from runtime action scopes | MCP client governance needs its own admin scope instead of reusing `mcp:write` |

The main conclusion is that top-tier projects do **not** make "everything the
console can do" public. They open the control plane in layers:

- read/status first,
- low-frequency machine-safe writes second,
- destructive or binary flows only when the auth and SDK semantics are explicit.

## Proposal

### 1. Publish canonical public management paths under `/v1/...`

For every selected operation, add a canonical public path under `/v1/...` and
keep the existing console path as an additional binding or alias.

Examples:

- `GET /v1/audit-log`
- `GET /v1/gdpr/requests`
- `POST /v1/gdpr/export`
- `GET /v1/outbox/deliveries`
- `GET /v1/mcp/clients`

The Console remains free to keep using `/fb/v1/console/...`, but public docs,
OpenAPI, and both SDKs should treat `/v1/...` as canonical.

This follows the precedent already established by `tags` and `workflow`: the
public automation contract should read like an API product, not like a browser
URL that happened to exist first.

### 2. Expand the API-key management surface in a selected, not total, set

The selected operations for #168 are:

| Resource | Public methods in scope | Required scope |
|---|---|---|
| Audit log | `ListAuditLog`, `ExportAuditLogCSV`, `CreateAuditEvidenceExport`, `GetAuditEvidenceExport`, `DownloadAuditEvidenceExport` | `audit:read` |
| GDPR jobs | `ExportGdprSubject`, `GetGdprExport`, `DownloadGdprExport`, `RevokeGdprExport`, `DeleteGdprSubject`, `CancelGdprRequest`, `ListGdprRequests`, `GetGdprOperations` | `gdpr:read` / `gdpr:export` / `gdpr:delete` (`gdpr:admin` implied for compatibility) |
| Outbox | `ListDeliveries`, `RetryDelivery` | `notify:read` / `notify:write` |
| MCP client governance | full `MCPClientService` | `mcpclient:admin` |

Explicitly deferred from this issue:

- `VerifyGdprStepUp`
- `GetEnrichmentRuntime`
- `UpdateEnrichmentRuntime`
- `ResetEnrichmentRuntime`
- `RollbackEnrichmentRuntime`

Why these are deferred:

- `VerifyGdprStepUp` is deliberately session/password-based;
- enrichment runtime is deployment-scoped, so even the status surface does not
  belong under the tenant-scoped `enrich:read` key family.

### 3. Reuse the console handlers through API-key route adapters

Implementation should extend the existing `MountAPIKeyAdminRoutes` pattern
rather than clone business logic.

The route shape is:

```text
/v1/... management route
  -> api-key middleware
  -> per-key limiter
  -> RequireExplicitScope(...)
  -> auth adapter
  -> existing console handler or service
```

This keeps one source of truth for:

- request validation,
- tenant isolation,
- audit payloads,
- error mapping,
- and the proto/OpenAPI contract.

The tags/workflow bridge already proved the pattern works. #168 should widen
that bridge, not replace it.

### 4. Add one new scope: `mcpclient:admin`

MCP client governance must **not** reuse `mcp:read`, `mcp:write`, or
`mcp:ingest`.

Those scopes are granted to OAuth clients so they can call MCP tools at runtime.
They are not the right permission family for:

- creating OAuth clients,
- changing tool policies,
- revoking sessions or refresh grants,
- or reading connection metadata.

Add a new API-key scope:

- `mcpclient:admin`

Rules:

- `mcpclient:admin` gates the public MCP client governance API.
- It is **not** implied by `mcp:write`.
- It is excluded from migration seeding and from the `full_access` preset, the
  same way `apikey:admin` is excluded today.

This is the cleanest way to keep "MCP runtime permissions" and "MCP control
plane permissions" from collapsing into one ambiguous scope.

### 5. Split GDPR permissions while preserving legacy compatibility

The current GDPR console flow requires recent step-up verification tied to a
session cookie and, when needed, a password prompt. That is correct for human
console use and wrong for machine automation.

For API-key-authenticated GDPR calls:

- `VerifyGdprStepUp` remains console-session-only.
- Public routes are split by least privilege:
  - `gdpr:read` for list/status/operations
  - `gdpr:export` for export/download/revoke
  - `gdpr:delete` for delete/cancel
- `gdpr:admin` remains valid and now implies the new granular scopes so
  existing machine keys keep working during migration.
- A migration backfills the new granular scopes onto keys that already hold
  `gdpr:admin`.
- Audit records must carry the actor as `apikey:<keyID>`.

This preserves the human step-up flow for browser sessions while making the
privacy job APIs usable for server-side automation.

Tradeoff:

- Existing keys that already hold `gdpr:admin` keep real machine GDPR power once
  these routes are exposed. That remains an intentional activation of an
  existing scope and must be called out in release notes.

### 6. Keep the existing SDK retry contract, but add idempotency where the server supports it

The new SDK methods should inherit the existing attune SDK rules:

- `GET`, `PUT`, `PATCH`, and `DELETE` retry on transient transport or retryable
  HTTP failure.
- Management `POST` methods that now carry a server-honored idempotency key
  auto-retry safely.
- `POST` methods without a server-honored idempotency key still do **not**
  auto-retry.

That means:

- `CreateTag`, `CreateWorkflowState`, `SeedWorkflowDefaults`,
  `CreateAuditEvidenceExport`, `ExportGdprSubject`, `RevokeGdprExport`,
  `DeleteGdprSubject`, `CancelGdprRequest`, `RetryDelivery`, and
  `CreateMCPClient` now auto-generate a stable per-call idempotency key and
  inherit the normal retry loop.
- `ListAuditLog`, `ListGdprRequests`, `GetGdprExport`, `ListDeliveries`, and
  `GetMCPClient` keep the existing retry behavior.

This keeps #168 consistent with world-class SDK ergonomics without broadening
the idempotency work past the routes the backend now explicitly supports.

### 7. Add SDK methods, not generated RPC stubs

Both SDKs should continue their current design:

- hand-written client methods,
- generated request/response types,
- one shared retry/error/security core.

#### Node/TypeScript

Add `Client` methods and index exports for:

- `listAuditLog`
- `exportAuditLogCsv`
- `createAuditEvidenceExport`
- `getAuditEvidenceExport`
- `downloadAuditEvidenceExport`
- `iterateAuditLog`
- `exportGdprSubject`
- `getGdprExport`
- `downloadGdprExport`
- `revokeGdprExport`
- `deleteGdprSubject`
- `cancelGdprRequest`
- `listGdprRequests`
- `iterateGdprRequests`
- `getGdprOperations`
- `listOutboxDeliveries`
- `iterateOutboxDeliveries`
- `retryOutboxDelivery`
- `listMcpClients`
- `createMcpClient`
- `getMcpClient`
- `revokeMcpClient`
- `updateMcpClient`
- `replaceMcpClientToolPolicies`
- `revokeMcpSession`
- `revokeMcpRefreshGrant`

Node already has the generated proto types for most of these surfaces under
`sdk/node/src/proto/attune/v1/`; the missing work is the client surface,
exports, docs, and tests. Binary download helpers should surface
`BinaryResponse.filename` from `Content-Disposition`, preferring RFC 5987
`filename*=` values for internationalized names while still honoring quoted
plain `filename="..."` fallbacks, and the existing 1 MiB response cap must
continue to hold even when a custom or polyfilled `fetch` falls back to
non-stream `arrayBuffer()` / `text()` reads.

#### Go

Expand the SDK Go proto generation to include:

- `audit.proto`
- `gdpr.proto`
- `outbox.proto`
- `mcp_client.proto`

Then add wrapper files and methods matching the established style:

- `ListAuditLog`
- `ExportAuditLogCSV`
- `CreateAuditEvidenceExport`
- `GetAuditEvidenceExport`
- `DownloadAuditEvidenceExport`
- `NewAuditLogPager`
- `ExportGdprSubject`
- `GetGdprExport`
- `DownloadGdprExport`
- `RevokeGdprExport`
- `DeleteGdprSubject`
- `CancelGdprRequest`
- `ListGdprRequests`
- `NewGdprRequestPager`
- `GetGdprOperations`
- `ListDeliveries`
- `NewOutboxDeliveryPager`
- `RetryDelivery`
- `ListMCPClients`
- `CreateMCPClient`
- `GetMCPClient`
- `RevokeMCPClient`
- `UpdateMCPClient`
- `ReplaceMCPClientToolPolicies`
- `RevokeMCPSession`
- `RevokeMCPRefreshGrant`

### 8. Documentation must distinguish publishable ingest keys from management keys

The docs for #168 should state this plainly:

- `ingest:write` keys may be used from browser-like producers as documented
  today;
- management scopes are for trusted server-side automation only;
- MCP client governance and GDPR automation are especially sensitive;
- the recommended posture is short-lived or easily-rotated keys, IP allowlists
  where available, and scope-minimal custom keys rather than broad presets.

At least one automation example each should be added for:

- audit log query,
- outbox retry,
- GDPR export job polling,
- and MCP client provisioning.

## Alternatives considered

### 1. Expose the existing `/fb/v1/console/...` paths directly

Rejected.

That would work mechanically, but it would freeze UI-history naming into the
public contract and make future API docs read like browser routes. `tags` and
`workflow` already established the cleaner `/v1/...` precedent.

### 2. Reuse `mcp:write` for MCP client governance

Rejected.

`mcp:write` describes what an OAuth client may do inside the MCP server, not who
may create or govern those OAuth clients. Reusing it would collapse runtime
authorization and control-plane authorization into one confusing permission.

### 3. Expose every proto-defined admin endpoint in one pass

Rejected.

That would drag binary downloads, human-password step-up, and deployment-wide
runtime mutators into the first public-management release. The selected subset
keeps the scope aligned with the issue body and with valid automation cases.

### 4. Auto-generate SDKs from OpenAPI or service definitions

Rejected.

attune's SDKs already encode security and retry decisions that are more specific
than a generic generated RPC client:

- no redirect following,
- bounded response buffering,
- reserved headers,
- and the "non-idempotent POST does not retry" rule.

The right move is to extend the hand-written clients, not replace them.

## Risks / tradeoffs

| Risk | Impact | Mitigation |
|---|---|---|
| Existing keys with `gdpr:admin` gain new machine power | Higher blast radius than today | Call out explicitly in release notes; keep management keys server-only; audit every action as `apikey:<keyID>` |
| Dual path support (`/v1/...` and `/fb/v1/console/...`) adds contract surface | Docs drift or inconsistent tests | Treat `/v1/...` as canonical everywhere; keep console path as compatibility alias only |
| PATCH-style governance updates can accidentally treat omission as "clear" | Silent policy/rate-limit loss on real automation clients | Preserve field presence on the private JSON route, reject empty update bodies, and default omitted public proto fields to the stored client values |
| SDK method count grows quickly | Larger `Client` surface | Accept for consistency with the current flat SDK shape; revisit namespacing only if the surface becomes unwieldy |
| Go SDK proto generation expands dependency footprint | More generated files and longer regen | This is already the accepted model for ingest/tag/workflow; extending it is lower risk than hand-writing wire types |
| Enrichment runtime remains session-only | Partial coverage may feel incomplete | Intentional scoping choice; deployment-scoped runtime control needs its own permission model |

## Implementation plan

1. Update proto HTTP bindings so the selected operations have canonical
   `/v1/...` paths and the console paths remain available.
2. Add `mcpclient:admin` to the scope model, scope metadata, presets, and API
   key docs.
3. Extend the API-key route mounting layer to expose the selected management
   operations with per-key limiting and scope checks.
4. Implement the GDPR machine-step-up path for API-key callers while keeping
   `VerifyGdprStepUp` session-only.
5. Extend the Node SDK client methods, exports, README, and tests.
6. Expand Go SDK proto generation, add client methods, README updates, and
   tests.
7. Add end-to-end coverage for scope denied, happy path, and tenant isolation
   on each newly exposed surface.
8. Add changelog and public docs updates in the implementation PR.

## Verification

Implementation of #168 should not be considered complete until the following
evidence exists:

- `make proto`
- `go test ./sdk/go/...`
- `pnpm --dir sdk/node test`
- focused Node unit coverage for binary `Content-Disposition filename*=`
  handling and non-stream response-cap fallbacks
- focused Go handler tests for each new API-key route group
- real-server e2e covering at least one happy-path and one scope-denied case for
  each of: audit, GDPR, outbox, and MCP governance
- browser-driven console QA covering admin login plus audit/GDPR/outbox/MCP
  operator pages, with GDPR exports requiring an explicit operator-triggered
  download instead of auto-downloading sensitive ZIPs on job completion
- updated README and automation examples for both SDKs

## References

- Stripe: [Restricted API keys](https://docs.stripe.com/keys/restricted-api-keys),
  [Idempotent requests](https://docs.stripe.com/api/idempotent_requests)
- GitHub: [Permissions for fine-grained personal access tokens](https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens),
  [Using pagination in the REST API](https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api)
- PostHog: [API overview](https://posthog.com/docs/api),
  [Personal API keys](https://posthog.com/docs/api/personal-api-keys),
  [File download batch exports](https://posthog.com/docs/api/file-download-batch-exports)
- Sentry: [Authentication](https://docs.sentry.io/api/auth/),
  [Paginating results](https://docs.sentry.io/api/pagination/),
  [Rate limits](https://docs.sentry.io/api/ratelimits/)
- LaunchDarkly: [API access tokens](https://launchdarkly.com/docs/home/account/api),
  [REST API overview](https://launchdarkly.com/docs/api),
  [LaunchDarkly local MCP server](https://launchdarkly.com/docs/home/getting-started/mcp-local)
