# Enforce per-API-key rate limits on ingest

| | |
|---|---|
| **Issue** | #41 (advanced API-key security controls) |
| **Status** | Implemented |
| **Started** | 2026-06-19 |
| **Related** | #37 (Node SDK — its browser-key guidance leaned on a per-key limit that doesn't exist; surfaced this gap), ingest rate limiting (`internal/infra/ratelimit`) |

## Problem

`external_api_keys.rate_limit_rpm` exists, is settable via the console
(`APIKeyInsertParams.RateLimitRPM`), and is echoed back by `GET /v1/auth/verify`
(`LookupResult.RateLimitRPM`) — but it is **never enforced on the request
path**. `grep` for `RateLimitRPM` outside storage/display finds nothing.

The only limiter on `POST /v1/feedback/ingest` is `rateLimiter.Middleware`
(`cmd/attune/router.go`), which is **per-tenant** with a single global default
(`DefaultRateLimitPerMinute=60`, burst 300) keyed solely on `tenant_id`
(`ratelimit.Limiter.Allow(tenantID)`). Every key in a tenant shares one bucket.

Consequences:
- A leaked/abused `ingest:write` key cannot be throttled independently — it
  consumes the whole tenant's ingest budget and can deny the tenant's other
  keys. This directly undercuts the Node SDK's documented browser-key safety
  story (#37), which is why the SDK docs were corrected to *not* claim per-key
  throttling.
- Operators set `rate_limit_rpm` in the console and reasonably expect it to do
  something; today it is silently inert.

## Goals
- When a key has `rate_limit_rpm` set (non-null, > 0), requests authenticated
  with that key are limited to that rate, independent of the tenant bucket.
- A throttled request returns `429 RATE_LIMITED` with a `Retry-After` header —
  identical envelope to the existing tenant limiter.
- A per-key throttle event is observable (a metric).
- Keys without `rate_limit_rpm` keep today's behavior (tenant limiter only).

## Non-goals
- Distributed/cross-replica rate limiting (the current limiter is in-memory
  per-process; this stays consistent with it — documented).
- Changing the tenant-level default or its config.
- Burst/quota policy beyond a simple per-minute token bucket.

## Proposal

1. **Surface the key's RPM to the request context.** Add `RateLimitRPM *int`
   to `apikey.AuthCtx`, and have the api-key middleware populate it. The data is
   already loaded — `LookupByHash` selects `rate_limit_rpm` and `LookupFull`
   already returns it. Extend the middleware's lookup path (a new
   `Verifier` method that returns the RPM, or fold it into the existing
   `LookupWithScopesAndIP` return) so the middleware sets `AuthCtx.RateLimitRPM`.

2. **A per-key limiter.** The existing `ratelimit.Limiter` keys a token bucket
   by an arbitrary string but uses one fixed `perMinute` for all. Per-key limits
   are *variable per key*, so add a `PerKeyLimiter` that maintains a token
   bucket per `keyID` at *that key's* rate (`rate.NewLimiter(rpm/60, burst)`,
   burst derived from rpm). It mirrors `Limiter`'s LRU/eviction and
   `Retry-After` computation. When `AuthCtx.RateLimitRPM` is nil/≤0 it is a
   no-op (tenant limiter still applies).

3. **Enforce after auth.** A middleware (or an extension of the api-key
   middleware, which already runs first and sets `AuthCtx`) checks the per-key
   limiter using `AuthCtx.KeyID` + `RateLimitRPM`. On limit: `429` +
   `Retry-After` + `ErrorCode_RATE_LIMITED` (reuse `dispatcher.Reject`, matching
   the tenant limiter) and bump `attune_apikey_rate_limited_total{tenant}`.
   Ordering: per-key check sits alongside the existing tenant `rateLimiter`;
   both must pass.

4. **Metric.** New `attune_apikey_rate_limited_total` counter (registered +
   added to the metric catalog + dashboard + README per the metric-drift gate).

## Alternatives considered
- **Reuse `Limiter` keyed by keyID with the global perMinute.** Rejected — the
  limit must come from the key's own `rate_limit_rpm`, which varies per key; the
  existing limiter has one fixed rate.
- **Enforce in the handler/service.** Rejected — rate limiting is a transport
  concern; the middleware already has `AuthCtx` and is the established site
  (mirrors the tenant `rateLimiter.Middleware`).
- **Distributed limiter (Redis).** Out of scope; the tenant limiter is already
  in-memory per-process. Consistency over a new dependency.

## Risks / tradeoffs
- **In-memory, per-replica.** Under N replicas the effective limit is ~N× the
  configured rpm (same property the tenant limiter already has). Document it;
  revisit with a shared store if needed.
- **Memory.** One bucket per active key — bounded by an LRU/TTL like the tenant
  limiter, so a flood of distinct keys can't grow it unbounded.
- **Behavior change.** Keys with `rate_limit_rpm` already set start being
  enforced (previously inert). This is the intended fix; call it out in the
  changelog as a behavior change.

## Implementation plan
1. `ratelimit.PerKeyLimiter` (per-key token bucket at the key's rpm, LRU-bounded,
   `Retry-After` computation) + unit tests.
2. `AuthCtx.RateLimitRPM` + middleware populates it (extend the lookup that
   already reads `rate_limit_rpm`).
3. Enforcement middleware (429 + Retry-After + RATE_LIMITED) wired on the ingest
   group in `cmd/attune/router.go`; metric `attune_apikey_rate_limited_total`.
4. Tests: unit (limiter math, no-op when rpm nil); integration (a key with
   `rate_limit_rpm=1` → 2nd rapid request 429; a key without rpm → unaffected).
5. Metric registered + catalog + dashboard + README (metric-drift gate).
6. CHANGELOG (Added + a Changed note: keys with `rate_limit_rpm` now enforced).

## Verification
- `make ci-check` subset (go-checks, golangci-lint, metric-drift, integration).
- Integration test proves a 1-rpm key throttles and a no-rpm key doesn't.
- `/v1/auth/verify` still reports `rate_limit_rpm` unchanged.

## References
- `internal/infra/ratelimit/limiter.go` (tenant limiter to mirror),
  `internal/infra/apikey/middleware.go` (`AuthCtx`, `Verifier`),
  `internal/service/apikey/apikey.go` (`LookupFull` already returns RPM),
  `cmd/attune/router.go` (ingest route wiring), `#41` (where `rate_limit_rpm`
  was added), `#37` SDK proposal (browser-key safety dependency).
