# P0/P1 production / security / reliability hardening batch

| | |
|---|---|
| **Issue** | #84 (DB statement_timeout) + 2026-06-18 defect audit |
| **Status** | Accepted |
| **Started** | 2026-06-19 |
| **Related** | #131 (GDPR erasure — shipped separately in #132), #81 (terminal enrichment failures), #91 (mutation testing). Large features + design-heavy items get their own proposals (see Non-goals). |

## Problem

The 2026-06-18 industry gap audit (adversarially code-verified against `main`)
surfaced a cluster of concrete P0/P1 production/security/reliability defects.
This batch fixes the **surgical** ones in one PR (separate commits per concern).
Verified findings addressed here:

- **DB pool unbounded** — `internal/infra/database/pool.go` set no `MaxConns`,
  `connect_timeout`, or `statement_timeout`; a slow query pins a connection past
  the HTTP timeout (#84). *(commit 1, done)*
- **HTTP server** set only `ReadHeaderTimeout` (`cmd/attune/server.go:188`) —
  slow-loris on body + slow readers unbounded. *(commit 1, done)*
- **Worker panic crashes the pod** — every background worker is launched as a
  bare `go X.Run(ctx)` (`cmd/attune/server.go` ~11 sites); no `recover()`, so one
  panic (e.g. malformed LLM JSON) takes down the HTTP server and all workers.
- **SSRF** — outbound webhook delivery (`internal/notify/transport.go:83`), LLM
  `base_url` (`internal/service/llmconfig/service.go:704`), and the email adapter
  (`internal/inbound/adapter/email/host_guard.go:43`, fail-open on DNS error) have
  no dial-time private-IP guard; a tenant can target `169.254.169.254`/RFC1918.
  Note: `internal/infra/config/oidc.go` already has a full guard — it just isn't
  reused on these paths.
- **API-key IP allowlist bypass** — `extractClientIP` trusts the leftmost
  `X-Forwarded-For` with no trusted-proxy config (`internal/infra/apikey/middleware.go:143`).
- **Duplicate delivery** — GitHub-issue delivery is non-idempotent (crash after
  HTTP-200 before `MarkDelivered` re-creates the issue), the envelope carries no
  per-delivery id, and `ingest` has no idempotency key (re-ingest dupes feedback).
- **Terminal enrichment failures invisible (#81)** — no metric/alert when a row
  exhausts retries; worker panics likewise unobserved.
- **outbox + embedding workers have zero tests**; no SKIP-LOCKED concurrency test.

## Goals
- A worker panic is recovered, logged, metered, and the worker restarts — never
  crashes the pod.
- Outbound egress (webhook/LLM/email) refuses private/metadata IPs; XFF is not
  spoofable for the IP allowlist.
- At-least-once delivery and ingest are idempotent (no duplicate GitHub issues /
  feedback rows).
- Terminal enrichment failures + worker panics are observable (metric + alert).
- The at-least-once workers get unit + concurrency tests.

## Non-goals (each gets its own proposal)
- **LLM retry / circuit breaker** — wants a new dependency (`sony/gobreaker`) →
  §8 dependency justification + its own design.
- **RLS / dbauthz tenant-isolation defense-in-depth** — architectural; large.
- **Graceful worker drain on SIGTERM** — touches the shutdown path; separate.
- **Embedding batching, per-tenant LLM spend cap, rate-limit shared store** —
  moderate, follow-ups.
- **Large product/DX features** — public read API, SDKs (#36/#37), docs site,
  public voting/roadmap surface, Discord (#32), MCP (#93).
- **P2 cleanups** — CSRF token rotation, audit hash-chain, down-migrations, etc.

## Proposal (per commit)
1. **Timeouts (#84) — DONE (`23627a1a`).** Pool `MaxConns`/`connect_timeout`/
   `statement_timeout` (30s, URL overrides win); server `ReadTimeout`/
   `WriteTimeout`/`IdleTimeout`.
2. **Worker panic recovery.** Add `safego(ctx, name, fn)` in `cmd/attune`:
   `defer recover()` → log stack + `attune_worker_panics_total{worker}` →
   restart `fn` after a short backoff unless `ctx` is cancelled. Wrap the
   background goroutine launches (outbox, enrichRuntime, embedding, reply-draft,
   gdpr, digest, lag/queue refreshers, audit pruner). `enrichRunner` keeps its
   existing `Wait`-based lifecycle.
3. **SSRF + XFF.** Extract `config/oidc.go`'s private-IP/metadata/DNS-rebinding
   guard into a shared `internal/pkg/nethardening` dialer; route the notify
   transport + email dial through it and validate the LLM `base_url` host with
   it; **fail-closed** on DNS error. XFF: a configured trusted-proxy/hop-count;
   parse right-to-left skipping trusted hops, else use `RemoteAddr`.
4. **Delivery dedup.** Emit `X-Attune-Delivery-Id` (the `notify_outbox.id`,
   stable across at-least-once retries) on raw-webhook deliveries so consumers
   can dedup replays. **Implemented.**
   - **Deferred (own follow-up): GitHub search-before-create.** The outbound
     framework's single-request `Build` model has no pre-flight hook, and GitHub
     issue search is rate-limited + eventually-consistent (a just-created issue
     isn't immediately searchable), so search-before-create is neither a clean
     fit nor reliable against rapid replay. Doing it right needs a framework
     `Preflight` hook — its own proposal.
   - **Deferred (own follow-up): ingest idempotency.** A `(tenant_id, source,
     source_user, content_hash)` unique index changes write semantics (a user
     legitimately resending identical text becomes a silent no-op); an opt-in
     `Idempotency-Key` is the safer Stripe-style design. Both need a migration +
     a semantics decision — its own proposal.
5. **Observability + worker tests.** Add `attune_enrichment_terminal_failures_total`
   (+ alert, #81) and `attune_worker_panics_total` in **one** metric-drift-gate-
   compliant change (register + catalog + dashboard + README). Unit tests for the
   outbox + embedding workers (delivered/failed/dead/disabled paths) and an
   integration test asserting two concurrent `ClaimBatch` calls never double-claim.

## Alternatives considered
- **`statement_timeout`: pool-wide vs per-request context timeout.** Chose
  pool-wide per #84 (also covers background workers); migrations run well under
  30s today (a future long backfill should `SET LOCAL statement_timeout = 0`).
- **SSRF: dial-time resolve-and-block vs config allowlist.** Chose resolve-and-
  block (reuse the existing oidc guard) — covers DNS-rebinding/redirects.
- **Dedup: `ON DELETE CASCADE` vs explicit / idempotency-key vs content-hash.**
  Chose consumer delivery-id + GitHub search-before-create + an ingest dedup
  index, matching Svix/Stripe consumer-dedup guidance.

## Risks / tradeoffs / decisions to settle
- **SSRF vs the loopback exemption (DECIDED 2026-06-19).** attune intentionally
  allows `http://127.0.0.1` provider base_urls (the local-dev + real-LLM-e2e
  loopback reverse-proxy depends on it). That same exemption is an SSRF vector.
  **Decision:** block RFC1918 / link-local / metadata always; keep loopback
  allowed **only** when an explicit dev/config flag is set (default: blocked in a
  TLS-fronted/production deploy, mirroring the existing `insecure_cookies`/
  `dev_login` startup refusal).
- `statement_timeout` could break a future long migration (mitigation noted).
- `safego` restart-on-panic must not fight `enrichRunner`'s `Wait` — so
  `enrichRunner` is left on its existing lifecycle.
- ingest dedup index changes write semantics (duplicate submits become no-ops) —
  intended, but a behavior change to document in the changelog.

## Implementation plan
Commits 1–5 above on `fix/p0p1-hardening-batch`; one PR (`Closes #84`, references
#81 + the audit), **stop at ready-to-merge** for review + merge. Sub-issues per
fix can be filed on acceptance if you want finer tracking.

## Verification
Per fix: SSRF test (host resolving to 169.254/RFC1918 → blocked; loopback gated by
flag); XFF-spoof test (forged header doesn't pass the allowlist); panic-recovery
test (a panicking worker doesn't kill the process + restarts); delivery-replay +
ingest-idempotency tests (no dupes); outbox/embedding worker unit + SKIP-LOCKED
concurrency tests. `make ci-check` + `make test-integration` green.

## References
- Defect audit 2026-06-18 (verified); #84; `internal/infra/config/oidc.go`
  (existing SSRF guard to reuse); OWASP SSRF / "real client IP" (adam-p).
