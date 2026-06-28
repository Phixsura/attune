# Browser ingest CORS for publishable write keys

| Field | Value |
| --- | --- |
| **Issue** | [#37](https://github.com/Phixsura/attune/issues/37) |
| **Status** | Implemented |
| **Started** | 2026-06-28 |
| **Related** | [#168](https://github.com/Phixsura/attune/issues/168) (public API / SDK hardening), [2026-06-19-node-typescript-sdk.md](./2026-06-19-node-typescript-sdk.md), [2026-06-28-public-api-version-contract.md](./2026-06-28-public-api-version-contract.md) |

## Problem

attune's Node SDK and top-level README both describe browser ingest as a
first-class path: `ingest:write` keys are treated as publishable credentials
for an in-app widget and the SDK works in modern browsers without a special
"dangerous" opt-in.

That product claim had a runtime gap: the repository already shipped a generic
`internal/handlers/cors` package, but the main server never mounted it on the
public ingest route and the config model had no way to declare allowed browser
origins. In practice this meant:

- same-origin browser deployments could work by accident;
- cross-origin browser widgets depended on an external reverse proxy knowing the
  exact attune request/response headers to allow; and
- attune itself had no explicit first-party CORS posture for the one route that
  is intentionally browser-safe.

This is exactly the kind of "README says yes, runtime still says maybe" gap
that world-class SDK/API products close quickly.

## Goals

- Add first-party, optional CORS support for browser ingest.
- Scope it only to the publishable ingest surface, not to management APIs.
- Keep it default-off and exact-origin allowlisted.
- Include the SDK-required headers, including `X-Attune-Api-Version`.
- Keep intermediary caches safe by emitting the right `Vary` headers whenever
  origin-specific CORS behavior is active.

## Non-goals

- Making management APIs browser-safe.
- Adding a general site-wide CORS toggle for every route.
- Adding credentialed browser sessions to the ingest API; publishable ingest
  uses API keys, not cookies.

## Proposal

1. Add `ingest.cors_allowed_origins` to runtime config.
2. Normalize and validate each configured origin:
   - exact `scheme://host[:port]` only;
   - `http`/`https` only;
   - no path/query/fragment/credentials;
   - optional `*` wildcard supported only as the sole entry.
3. Mount CORS middleware only on the public ingest route group
   (`/v1/feedback/...`), before API-key auth, so browser preflight `OPTIONS`
   requests succeed without credentials.
4. On that browser-safe ingest route, run CORS before public API version
   validation so stale or malformed `X-Attune-Api-Version` pins still surface
   as readable JSON `400` responses to allowed origins rather than opaque CORS
   failures.
5. Leave management routes and the rest of `/v1` untouched, preserving the
   server-only posture from #168.
6. Document the new config in README/private-deploy/deploy sample config and
   update SDK browser guidance to point at attune-native CORS when not
   terminating CORS upstream.
7. Harden the generic middleware so explicit-origin responses always add
   `Vary: Origin`, preflights also vary on
   `Access-Control-Request-Method` / `Access-Control-Request-Headers`, and
   wildcard-plus-credentials requests reflect the concrete origin instead of
   emitting the invalid `*` + credentials combination.

## Alternatives considered

### Enable CORS on all `/v1` routes

Rejected. That would weaken the product boundary between the publishable ingest
data plane and the server-only management plane.

### Keep CORS proxy-only forever

Rejected. Reverse proxies still matter, but attune should have a first-party
runtime story for the one browser-safe route it explicitly documents.

## Risks / tradeoffs

- `*` is intentionally supported for low-friction evaluation setups, but exact
  origins remain the recommended production posture.
- The generic middleware becomes slightly more opinionated about cache metadata,
  but that matches browser/proxy reality better than the previous under-specified
  behavior.
- This adds one more runtime knob to deployment docs, but only for teams that
  actually embed cross-origin widgets.

## Implementation plan

1. Extend config parsing/validation with `ingest.cors_allowed_origins`.
2. Mount CORS only on the public ingest route group, ahead of version
   validation on that route so browser callers can still read contract errors.
3. Add unit tests for config normalization, preflight/auth ordering, and
   cache/credentials/version-error edge cases.
4. Update docs, sample config, and changelog.

## Verification

- Config tests cover normalization, default-off behavior, and invalid origin
  rejection.
- Router tests cover allowed-origin preflight, actual request headers, and
  disallowed-origin behavior.
- Router tests also cover allowed-origin version rejections so browser callers
  see the JSON contract error instead of a synthetic CORS failure.
- Middleware tests cover `Vary` behavior for explicit origins and the wildcard
  plus credentials edge case.
- Existing SDK tests continue to pass with the new version header.

## References

- Existing browser-ingest design in `2026-06-19-node-typescript-sdk.md`.
- Existing public API hardening in `2026-06-28-management-api-sdk-coverage.md`.
