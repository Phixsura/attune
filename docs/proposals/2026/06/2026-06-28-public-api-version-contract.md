# Public API version contract for `/v1`

| Field | Value |
| --- | --- |
| **Issue** | [#168](https://github.com/Phixsura/attune/issues/168) |
| **Status** | Implemented |
| **Started** | 2026-06-28 |
| **Related** | [#19](https://github.com/Phixsura/attune/issues/19) (proto/OpenAPI contract), [2026-06-28-management-api-sdk-coverage.md](./2026-06-28-management-api-sdk-coverage.md), [2026-06-28-openapi-error-contract-completeness.md](./2026-06-28-openapi-error-contract-completeness.md) |

## Problem

attune's public API now spans more than ingest: SDK callers can automate tags,
workflow config, audit/GDPR flows, outbox retries, and MCP client governance.
That surface is still published only as a path version (`/v1/...`), which is
stable but too coarse for production-grade SDK compatibility management.

Without an explicit request/response version contract:

- the server cannot distinguish "old SDK pinned an older compatible shape" from
  "caller just hit `/v1`";
- SDKs cannot declare which public contract they were built against;
- future breaking-but-still-`/v1` changes have no compatibility lane; and
- the OpenAPI document cannot teach external integrators how version pinning,
  deprecation, and sunset will work.

World-class public APIs separate URL stability from contract stability. Stripe,
GitHub, and similar platforms keep stable resource paths while pinning a
date-based or header-based contract per request. attune needs the same control
surface before the growing SDK and management API matrix becomes ambiguous.

## Goals

- Keep the stable public path prefix at `/v1/...`.
- Add an explicit per-request public API version header.
- Make both official SDKs send the current version automatically.
- Reject unsupported pinned versions with the standard `ErrorResponse` envelope.
- Publish the contract in OpenAPI, including future deprecation/sunset headers.
- Preserve optional response-writer capabilities while injecting the version
  headers.

## Non-goals

- Replacing path versioning; `/v1` remains the canonical path family.
- Supporting multiple deprecated versions immediately; the first cut can ship
  one current version plus scaffolding for future overlap windows.
- Applying this contract to inbound third-party webhook paths under
  `/v1/inbound/...`; those consume external event formats, not attune's own
  product API contract.

## Proposal

1. Add an `X-Attune-Api-Version` request/response contract for the API-key
   product surface under `/v1/...` except `/v1/inbound/...`.
2. Treat the header as optional for now:
   - omitted header => current server default;
   - supported explicit value => use and echo that value;
   - unsupported, empty, or ambiguous value => `400 BAD_REQUEST` with the
     shared `ErrorResponse` envelope.
3. Use a date-based version token, starting at `2026-06-28`.
4. Add response scaffolding for `Deprecation` and `Sunset` so older-but-still-
   supported versions can be announced without another contract redesign.
5. Make the Go and Node SDKs send `X-Attune-Api-Version: 2026-06-28`
   automatically and reserve that header against caller override.
6. Extend the OpenAPI post-processor so generated docs always include the
   version request header plus the response headers on public `/v1/...`
   operations.
7. Preserve optional writer capabilities such as `http.Flusher`,
   `io.ReaderFrom`, and `http.Hijacker` when the middleware wraps `/v1`
   responses, so export/download handlers keep their existing transport
   behavior.
8. On the browser-safe ingest route only, apply CORS before version validation
   so unsupported version pins still surface as readable API errors to
   allowlisted browser origins.

## Alternatives considered

### Path-only versioning

Rejected. It keeps the URL surface simple, but it cannot express "SDK built
against contract A, server default is now contract B" without forcing a new
path family immediately.

### Required version header from day one

Rejected for now. It is operationally stricter, but it would break existing raw
clients overnight. Making the header optional while the official SDKs pin it
automatically gives attune a clean migration path.

### Reusing `Accept` media-type parameters

Rejected. It is harder to document, harder to test across fetch/http.Client,
and less legible than a dedicated header for a small API surface.

## Risks / tradeoffs

- Raw non-SDK callers who pin an unsupported version will now receive `400`
  instead of silently falling through. That is intentional and documented.
- Browser deployments that terminate CORS outside attune must allow the new
  request header and expose the response header. The Node SDK README calls this
  out explicitly.
- The first cut still has one supported version. The value is in the explicit
  contract and overlap scaffolding, not in immediate multi-version support.
- Response-writer wrapping can accidentally hide optional interfaces; the
  middleware must forward the ones `/v1` handlers or transports may rely on.

## Implementation plan

1. Add a dedicated API-version middleware package and mount it on the API-key
   `/v1/...` groups only.
2. Update SDK request builders, reserved-header filters, tests, and docs.
3. Extend `internal/tools/openapipatch` to publish the header contract.
4. Preserve optional writer interfaces on the middleware wrapper.
5. Keep browser-safe ingest ordering at `CORS -> version contract -> auth`.
6. Regenerate `docs/openapi/openapi.yaml` and verify idempotence.

## Verification

- Unit tests for the middleware cover defaulting, supported versions,
  deprecation headers, empty values, multi-value rejection, and optional writer
  interface passthrough.
- Router tests cover the browser-safe ingest interaction so an unsupported
  version still returns a CORS-readable JSON error to an allowed origin.
- Go SDK tests assert the version header is sent and cannot be overridden.
- Node SDK tests assert the same behavior.
- OpenAPI post-processor tests assert the request header parameter and response
  header docs are injected.

## References

- GitHub REST API versioning: header-pinned API versions on stable paths.
- Stripe API versioning: date-based request contract pinning on stable
  resource URLs.
