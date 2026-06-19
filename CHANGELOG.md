# Changelog

All notable changes to attune are documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- **GDPR erasure no longer aborts on subjects with deliveries (#131).** The
  data-subject delete now purges `notify_outbox` (a `NOT NULL` FK to
  `user_feedback` with no `ON DELETE` action, whose `payload` holds the feedback
  PII) inside the erasure transaction. Previously, deleting any subject who had
  ever had a notification raised a foreign-key violation that rolled back the
  whole erasure (a GDPR Art.17 + availability bug); residual PII in the delivery
  envelope is now removed too. The deletion record reports an `OutboxCount`.
  
### Security

- **SSRF-resistant outbound egress (#64).** A new `internal/pkg/nethardening`
  guard enforces an egress policy at dial time (after DNS resolution, so it
  defeats DNS rebinding) on outbound webhook delivery (including the console
  "test webhook" path), every LLM provider call, and the inbound email IMAP dial
  (closing its prior fail-open-on-DNS gap). Blocked IPv4 targets wrapped in IPv6
  transition formats (6to4, NAT64, Teredo, IPv4-compatible `::a.b.c.d`) are
  unwrapped and re-checked. Cloud-metadata (e.g. `169.254.169.254`), link-local,
  unspecified, and
  multicast destinations are always blocked; loopback and RFC1918 are blocked by
  default and re-permitted only via `security.allow_loopback_egress` /
  `security.allow_private_egress`. LLM `base_url` validation rejects literal
  metadata/link-local IPs at config time. Previously a tenant-controlled webhook
  or LLM base URL could reach the cloud metadata endpoint or internal services.
  The guard also blocks 6to4 (`2002::/16`) and NAT64 (`64:ff9b::/96`) IPv6
  addresses that wrap a blocked IPv4 target, refuses to honor `HTTP(S)_PROXY` on
  these egress paths (a proxy would hide the real destination IP from the
  dial-time check), and validates that any trusted `X-Forwarded-For` hop is a
  parseable IP (falling back to the direct peer otherwise).

- **X-Forwarded-For spoofing fixed for the API-key IP allowlist (#64).** The
  client IP behind the per-key IP allowlist is now resolved using the new
  `security.trusted_proxy_hops` setting: with no trusted proxy (default)
  `X-Forwarded-For` is ignored and the direct peer is used, so a client on a
  direct connection can no longer forge an allowlisted source IP by setting the
  header (only `X-Forwarded-For` is consulted — single-header model; deployments
  whose proxy emits only `X-Real-IP` must also append XFF). Behind N reverse
  proxies, set `trusted_proxy_hops: N`. Also **removed chi's `middleware.RealIP`**
  from the router — it unconditionally rewrote `RemoteAddr` from
  `X-Forwarded-For`/`X-Real-IP`, which by itself made the allowlist spoofable
  regardless of the setting above. **Behavioral note:** with `RealIP` gone,
  `audit_log.actor_ip` and the enrichment-runtime actor IP now resolve through
  the same `trusted_proxy_hops` model, so behind a proxy they record the real
  client IP (set `trusted_proxy_hops`) rather than — as a side effect of `RealIP`
  — the leftmost `X-Forwarded-For` value.

### Changed

- **DB pool and HTTP server are now bounded (#64).** `database.NewPool` applies
  defaults for `MaxConns` (20), `connect_timeout` (10s), and `statement_timeout`
  (30s) unless the database URL already sets them, so a single stuck query can't
  pin a connection or run unbounded past the request deadline. The HTTP server
  gained `ReadTimeout` (60s), `WriteTimeout` (315s, above the in-handler 305s
  timeout), and `IdleTimeout` (120s)
  alongside the existing `ReadHeaderTimeout`, closing slow-loris and slow-reader
  exposure.

- **Background workers are panic-supervised (#64).** Every long-running worker
  (outbox, enrichment runtime, embedding, reply-draft, digest, GDPR export, audit
  pruner, queue/lag refreshers) now runs under a `safego` supervisor that recovers
  panics, counts them in the new `attune_worker_panics_total` metric, and restarts
  the worker with capped backoff. The enrichment runner — the highest-panic-risk
  path, since it parses LLM responses — additionally recovers panics at each of
  its goroutine boundaries (per-job, processor, sweeper). Previously a single
  panic in any worker crashed the whole process — HTTP server and all other
  workers included. Added `AttuneWorkerPanics` and `AttuneEnrichmentTerminalFailures`
  Prometheus alerts + runbooks.

- **Migrations are exempt from the `statement_timeout` default (#64).** The new
  30s `statement_timeout` (above) is cleared on the dedicated migration
  connection so a legitimately long migration can't be killed mid-statement.

- **Outbox is safe under multiple worker replicas (#64).** `ClaimBatch` now
  excludes rows claimed within a 10-minute window, so a second outbox worker
  replica can't re-claim an in-flight row and double-deliver (FOR UPDATE SKIP
  LOCKED only guards the lock window). While a worker drains a batch it renews
  the claims (owner-scoped lease heartbeat, new `claimed_by` column) so a long,
  slow batch can't age its tail rows past the window mid-flight regardless of
  batch size, and one replica never renews a row another replica re-claimed. A
  worker that crashes mid-delivery still has its rows retried after the window.

- **Workflow state names are now localized (#64).** `WorkflowState.name` is now a
  stable machine key (slug `^[a-z][a-z0-9_]{0,30}$`); the human-facing label moves
  to a new per-locale `display_name` (`I18nString`), mirroring how dimensions
  already separate a stable key from an editable localized display. Default seed
  states ship `default`/`en`/`zh` labels. **Behavioral break (pre-1.0):** API
  clients that read `WorkflowState.name` as a display string should read
  `display_name`; create/update payloads carry `display_name`. Migration 053
  backfills existing rows (label preserved in `display_name`, key slugged).

### Added

- **Node/TypeScript client SDK `@phixsura/attune` (#37).** Published client for
  the ingest API under `sdk/node/`: `new Client({ baseURL, apiKey })` →
  `await client.ingest({ content, … })` returning the stored row `id`. ESM + CJS
  (tsdown), zero runtime dependencies, native `fetch`, Node 20+ and browsers.
  Request/response types are generated from the proto contract (a new ts-proto
  `buf.gen` target → `sdk/node/src/proto/`, guarded by the `proto-sync` gate) —
  never hand-written. Transactional await-throw model with a typed `AttuneError`
  (`code`/`status`/`requestId`) and a shared retry contract (408/409/429/5xx +
  network/timeout, never 400/422, `Retry-After`-aware, default 2 retries) that
  the Go SDK (#36) will adopt. Ships `examples/node-ingest` and
  `examples/browser-ingest`; `ingest:write` keys are documented as publishable
  browser-safe credentials.

- **Idempotent ingest via the `Idempotency-Key` header (#37).** `POST
  /v1/feedback/ingest` now honors an optional `Idempotency-Key` request header:
  a replay with the same key + body returns the original feedback id without
  inserting again; the same key with a different body is `409
  IDEMPOTENCY_CONFLICT`; a concurrent in-flight key is `409 REQUEST_IN_PROGRESS`;
  a malformed key is `400 VALIDATION`. Reuses the existing `idempotency_keys`
  store (previously only `/batch`). The Node SDK sends a per-call key
  automatically, held stable across retries, so a retried at-least-once delivery
  cannot create a duplicate feedback row.

- **Terminal enrichment-failure observability (#64).** New metric
  `attune_enrichment_terminal_failures_total{tenant}` counts feedback rows that
  exhaust all enrichment retries and stop in `failed`, plus an
  `AttuneEnrichmentTerminalFailures` Prometheus alert + runbook and an AI Pipeline
  dashboard series. Previously a row could silently stop enriching with no signal.

- **`X-Attune-Delivery-Id` header on outbound webhook deliveries (#64).** Raw
  webhook deliveries now carry the outbox row id, which is stable across the
  at-least-once retries of that delivery, so consumers can dedup replays (a
  delivery that succeeds downstream but crashes before `MarkDelivered` is retried
  with the same id). Signature and payload are unchanged.

- **Shipped-artifact hygiene CI guard (#64).** New `scripts/lint-artifacts.sh`
  (wired into `ci-gate`, the pre-commit hook, and `make ci-check`) fails on leaked
  intermediate-state scaffolding in shipped artifacts: internal `Phase N` roadmap
  markers and non-i18n CJK in `*.go` / `openapi.yaml` / shipped `*.md` (i18n
  files, localized product data, and test fixtures are allow-listed via
  `// lint-artifacts:[file-]allow`). The accompanying one-time sweep removed the
  residual `Phase` markers and two leaked Chinese Console labels. `CLAUDE.md` and
  `AGENTS.md` now carry the English-canonical policy and a "run `make ci-check`
  before claiming done" rule.

- **AI-collaboration skills directory (#64).** New `.agents/skills/` ships
  reusable, tool-neutral Agent-Skills (Anthropic `SKILL.md` format) for attune's
  conventions — `/attune:proposal`, `/attune:create-pr`, `/attune:preflight` — so
  AI assistants follow the proposal/changelog/preflight rules consistently.

- **Notify outbox dead-queue operator surface (#33).** Operators can now inspect
  and recover failed webhook deliveries from the Console/API instead of querying
  Postgres directly. New admin-only endpoints `GET
  /fb/v1/console/outbox/deliveries` (tenant-scoped, status-filtered, keyset
  paginated) and `POST /fb/v1/console/outbox/{id}/retry` (re-arm one dead/failed
  row in place — resets to pending; the worker redelivers on its next poll;
  returns 202). Retry is concurrency-safe (a row a worker is currently sending is
  rejected with 409, never double-delivered) and the `outbox.retry` audit row is
  written inside the same transaction as the re-arm (via a new
  `auditlog.Service.RecordTx`), so a retry can never commit without its audit
  trail. Failures are now classified into a structured `failure_kind`
  (`http_4xx`/`http_5xx`/`timeout`/`dns`/`connection`/`tls`/`terminal`/`other`)
  with the upstream `http_status` stored separately, so operators can triage by
  failure type. New `attune_outbox_dead_rows` gauge surfaces dead-letter depth
  for alerting. Standalone "Dead deliveries" Console view lists deliveries with
  per-row retry. Migration 051 adds the `failure_kind`, `http_status`, and
  manual-retry bookkeeping columns; migration 052 registers the `outbox.retry`
  audit action.

- **Fine-grained API key scopes (#41).** API keys now support 24 resource:action
  scopes for least-privilege access control. Scopes are stored in a normalized
  `api_key_scopes` table and loaded atomically with key lookup (fail-closed).
  Five preset templates (Ingest Only, Read Only, Developer, Integration, Full
  Access) simplify common configurations. The `apikey:admin` scope is excluded
  from migration seeding and the Full Access preset, preventing existing or new
  keys from managing other keys by default. New Console endpoints `GET
  /fb/v1/console/api-keys/scopes` and `GET /fb/v1/console/api-keys/presets`
  expose scope metadata and templates. The `attune_apikey_scope_denied_total`
  metric tracks scope enforcement denials by required scope.

- **Enterprise API key features.** Comprehensive API key management matching
  industry leaders (Stripe, Cloudflare, GitHub):
  - **Key rotation with grace period** — `POST /fb/v1/console/api-keys/{id}/rotate`
    creates a new key while keeping the old key valid during a configurable grace
    period (default 24h), enabling zero-downtime credential rotation.
  - **Request logs** — `GET /fb/v1/console/api-keys/{id}/logs` returns recent
    requests made with a key (method, path, status, latency, client IP).
    Partitioned monthly for performance.
  - **Environment tags** — Keys can be tagged as production/staging/development/test
    via `PATCH /fb/v1/console/api-keys/{id}/environment`.
  - **Service accounts** — Non-human identities for CI/CD and automation via
    `GET/POST /fb/v1/console/service-accounts`.
  - **Event webhooks** — Subscribe to key lifecycle events (created, rotated,
    revoked, expired, expiring_soon) via `POST /fb/v1/console/api-keys/event-subscriptions`.
  - **Expiry alerts** — `GET /fb/v1/console/api-keys/expiring` returns keys
    expiring within a time window (default 7d).
  - **Leak detection** — Framework for tracking leaked keys from external sources
    (GitHub secret scanning integration ready).
  - **Resource binding** — Database schema ready for restricting keys to specific
    resource subsets (API endpoints TBD).
  - **Org-level policies** — Tenant-wide API key policies via `GET/PUT
    /fb/v1/console/api-keys/policy` control max expiry, require IP allowlist,
    require description, limit keys per service account, restrict environments,
    require MFA for create, require approval for production keys, auto-revoke
    after N days unused.
  - **Project/workspace binding** — Isolate API keys by project via `GET/POST
    /fb/v1/console/projects` and `POST /fb/v1/console/api-keys/{id}/project`.
  - **Budget/spend limits** — Per-key budget controls via `PUT
    /fb/v1/console/api-keys/{id}/budget` with block/alert/none overage actions.
  - **Custom metadata tags** — Key-value labels via `GET/PUT
    /fb/v1/console/api-keys/{id}/tags` for team ownership, cost center, etc.
  - **Temporary tokens** — Short-lived tokens derived from parent keys via
    `POST /fb/v1/console/api-keys/{id}/temp-token` with expiration and max-uses.
  - **Approval workflows** — Require approval for production key creation via
    `GET/POST /fb/v1/console/api-keys/approvals` and review via
    `POST /fb/v1/console/api-keys/approvals/{id}/review`.
  - **OAuth2 client credentials** — M2M authentication via `GET/POST
    /fb/v1/console/oauth2/clients` for service-to-service flows.
  - **Analytics dashboard** — Hourly aggregated key usage metrics via `GET
    /fb/v1/console/api-keys/analytics` and `GET /fb/v1/console/api-keys/{id}/analytics`.
  - **Secret manager integration** — External secret manager configs (Vault,
    AWS, GCP, Azure) via `GET/POST /fb/v1/console/secret-managers`.

- **Advanced API key security features.** Additional enterprise security controls
  matching AWS IAM, Twilio, and Azure best practices:
  - **Browser detection** — `IsBrowserUserAgent()` helper detects browser User-Agent
    patterns to prevent secret key exposure in frontend code (Supabase pattern).
  - **Scheduled auto-rotation** — `GET/POST /fb/v1/console/api-keys/{id}/rotation-schedule`
    configures automatic key rotation intervals with grace periods and notifications
    (AWS Config 90-day pattern).
  - **Unused permission detection** — `GET /fb/v1/console/api-keys/{id}/unused-scopes`
    identifies scopes granted but never used, enabling least-privilege refinement
    (AWS Access Analyzer pattern).
  - **Public key cryptography verification (PKCV)** — `GET/POST
    /fb/v1/console/api-keys/{id}/signing-keys` enables request signing with
    asymmetric keys (Twilio pattern).
  - **Managed identities** — `GET/POST /fb/v1/console/managed-identities` for
    secretless authentication via cloud provider workload identity (Azure/GCP pattern).
  - **SIEM integration** — `GET/POST /fb/v1/console/siem-integrations` for streaming
    API key events to Splunk, Datadog, Elastic, and other SIEM providers.
  - **AI agent configurations** — `GET/POST /fb/v1/console/ai-agents` for LLM-based
    API access patterns with scope restrictions (Okta AI agent pattern).
  - **Key health score** — `GET /fb/v1/console/api-keys/{id}/health` returns a
    composite security score (0-100) based on expiry, IP restrictions, rate limits,
    unused scopes, and rotation age.

### Security

- **API key security enhancements.** Added industry-standard security controls
  for API keys: expiration (`expires_at`), IP allowlist (`allowed_cidrs` with
  CIDR notation), usage tracking (`usage_count`), and per-key rate limits
  (`rate_limit_rpm`). The middleware now validates expiration and IP before
  allowing access. New metrics `attune_apikey_expired_total` and
  `attune_apikey_ip_denied_total` track denied requests, while
  `attune_apikey_usage_total` tracks successful authentications by key prefix.
  Added `GET /v1/auth/verify` endpoint for token verification. The Security &
  Compliance dashboard now includes an API Key Security section showing access
  denials and usage patterns.

- **GDPR subject export/delete controls (#43).** Added canonical
  `user_feedback.subject_key` identity tracking plus admin-only
  `/fb/v1/console/gdpr/export` and `/fb/v1/console/gdpr/delete` flows for
  tenant-scoped data-subject access and erasure. GDPR exports now bundle
  feedback rows, tag assignments, workflow audit rows, reply drafts, embedding
  metadata, and linked `llm_audit` rows into a ZIP archive, while GDPR delete
  hard-deletes subject-linked feedback data and derived AI artifacts. Unified
  audit log coverage now records hashed `gdpr.export` / `gdpr.delete` events
  without writing subject identifiers in clear text to the append-only
  `audit_log` stream.

- **Immutable console audit log for sensitive actions (#39).** Added an
  append-only `audit_log` table with retention pruning, admin-only read/export
  APIs, and request-scoped actor metadata (user type/id, IP, user-agent).
  Sensitive console mutations now emit immutable audit rows for API keys,
  tenant members, notify targets (including test sends), digest subscriptions,
  tag configuration, inbound source management (including test-connection),
  feedback job cancellation, guard policies, enrich config, workflow settings,
  LLM config, and feedback batch delete. Added a mutating-route audit coverage
  inventory test plus an audit action allowlist so future console write
  endpoints must declare an explicit audit decision and register any new audit
  action. Notify-target audit snapshots now strip embedded URL credentials and
  query tokens.

- **Constant-time hash comparison in idempotency repo (#30).** Replaced
  `bytes.Equal()` with `crypto/subtle.ConstantTimeCompare()` for comparing
  request hashes, eliminating a theoretical timing attack vector.

- **SSRF protection for HTTPS notify targets (#34).** Console API now rejects
  `https://` URLs targeting private/internal IP ranges (10.x, 172.16-31.x,
  192.168.x, link-local, loopback). DNS resolution is performed at validation
  time to catch domain names pointing to private IPs. HTTP loopback for local
  testing remains allowed as documented.

- **Secret length enforcement (#34).** Console API now requires secrets to be
  at least 16 characters when provided, matching the config-based webhook
  validation. Empty secrets remain allowed for channels with embedded tokens
  (Lark/Slack webhook URLs).

### Added

- **Bounded enrichment execution and local LLM throttling (#80).** Ingest now
  submits enrichment work into a bounded in-process queue instead of spawning an
  unbounded goroutine per accepted row. A shared enrichment runner batches work,
  executes `EnrichOne` with capped concurrency, and refills from pending DB rows
  on a sweep interval so restart recovery still comes from `user_feedback`.
  Added `enricher.queue_len`, `enricher.workers`, `enricher.batch_window`,
  `enricher.llm_max_qps`, and `enricher.llm_burst` config knobs plus new queue
  and limiter metrics (`attune_enrich_queue_depth`,
  `attune_enrich_queue_full_total`, `attune_enrich_batch_size`,
  `attune_enrich_sweep_submitted_total`,
  `attune_llm_rate_limit_wait_seconds`). Rate-limit wait cancellation now leaves
  rows recoverable instead of recording them as ordinary provider failures.

- **Enrichment runtime control-plane and live Console controls (#80).** Added a
  new `EnrichmentRuntimeService` proto contract, Postgres tables for desired
  runtime policy/history/per-instance status, admin-only Console routes for
  get/update/reset/rollback, a deployment-scoped runtime service that persists
  desired policy revisions and publishes local instance state, plus mutable
  enrichment runner/LLM limiter primitives that support live reconfiguration
  without process restart. The Settings UI now exposes full queue/worker/batch
  and LLM rate-limit controls, step-up protected reset and rollback actions,
  live per-instance convergence status, and recent revision history. Follow-up
  hardening aligned runtime auth with RBAC admins, stopped heartbeat no-op
  reconciles from refilling local rate-limit tokens, kept queue resize state in
  `applying` until effective capacity actually converges, stabilized instance
  identity across process restarts, wired runtime mutations into unified audit
  logging plus DB-backed action allowlists, and prevented background polling in
  the Console from overwriting an operator's in-progress edits. The operator
  experience was then refined into a more product-grade control surface with
  value framing, direct operating guidance, field-level explanations, secondary
  disclosure for opaque IDs, and clearer separation between current live nodes
  and historical runtime rows.

- **Console GDPR settings surface (#43).** Added a dedicated Settings > GDPR
  page for exact subject-key export and permanent delete operations, wired to
  the new proto/OpenAPI contract and admin-only permission gates.

- **GDPR request center and operations panel (#43).** Extended the GDPR
  settings surface with a first-class request center backed by `gdpr_requests`,
  live request-status history for export/delete operations, and an explicit
  operations panel showing step-up windows, export artifact TTL, audit
  retention, prune cadence, and current archive/request backlog.

- **Sensitive-action GDPR step-up flow (#43).** Added recent-auth session
  tracking plus password-based step-up verification for local admin sessions,
  and now require recent step-up auth before GDPR export or permanent delete
  actions can execute.

- **Scheduled GDPR deletes with cancel window (#43).** Permanent delete
  requests now enter an explicit grace-period queue backed by `gdpr_requests`,
  expose `execute_after` / cancellation state in the Console request center,
  support audited `POST /fb/v1/console/gdpr/requests/{request_id}/cancel`,
  and execute through the background GDPR worker instead of deleting inline on
  the request thread.

- **Revocable GDPR export artifacts (#43).** Ready/downloaded GDPR archives
  can now be explicitly revoked before TTL expiry through the Console request
  center and export status panel, with audited
  `POST /fb/v1/console/gdpr/exports/{job_id}/revoke`, explicit `revoked`
  lifecycle state, and server-side archive invalidation that blocks any later
  download attempt.

- **Mixin-grade Grafana dashboard coverage (#63).** Added generated first-party
  dashboards for inbound, AI pipeline, operations, security/compliance, overview,
  and LLM cost signals, with drift guards for metric coverage, datasource-less
  portability, generated JSON freshness, and Helm dashboard copy sync. The
  overview now follows RED/golden-signal operational flow, adds diagnostic
  descriptions and thresholds, and ships a load E2E script for validating metric
  values through Prometheus and Grafana.
- **Prometheus rules for Attune observability (#63).** Added portable recording
  and alert rules for ingest, inbound, AI, operations, LLM provider, and security
  signals, wired into the Docker Compose observability overlay and exposed as an
  optional Helm `PrometheusRule`. Alert annotations now include dashboard links,
  runbook links, and first-response actions.

- **Helm chart for Kubernetes deployment (#42).** Added a first-party
  `deploy/helm/attune` chart with Attune Deployment/Service/Ingress/HPA/PDB,
  optional embedded pgvector Postgres, config Secret or `existingSecret`
  support, ServiceMonitor, Grafana dashboard ConfigMaps, NetworkPolicy,
  values schema validation, Helm smoke tests for Service DNS, TCP, and HTTP
  readiness, fail-fast validation for unsafe production/HPA/PDB/NetworkPolicy
  value combinations, rolling release hardening, blue/green traffic Service
  support, kubeconform plus multi-replica kind install, upgrade, scale-out, and
  sudden pod failure and blue/green switch smoke checks in CI, GHCR OCI chart
  publication during releases, cross-triggered Go/Console/proto/image/chart CI
  checks, and Kubernetes deployment docs.
  The server now also exposes `/readyz` for PostgreSQL-backed readiness while
  `/healthz` remains process liveness, and `SIGTERM` marks readiness unhealthy
  before bounded HTTP shutdown so Kubernetes rollouts can drain traffic.

- **Console audit log page and CSV export (#39).** Added `/fb/v1/console/audit-log`
  plus `/fb/v1/console/audit-log/export.csv`, a new Settings > Audit Log page,
  audit-log proto/OpenAPI contract generation, retention config
  (`audit.retention_days`, `audit.prune_interval`), and audit metrics
  (`attune_audit_rows_written_total`, `attune_audit_rows_pruned_total`,
  `attune_audit_prune_duration_seconds`). The audit log list now supports
  cursor pagination with a matching “加载更多” Console flow, multi-action and
  date-range filtering, and request-metadata drill-down in row details, while
  CSV export returns the full filtered result set instead of only the visible
  page.

- **RBAC: admin/member/viewer roles (#38).** Three-level role hierarchy with
  route-level middleware (`RequireRole`) and resource-level policy classes
  (`FeedbackPolicy`, `MemberPolicy`). Role cached for 5min with explicit
  invalidation; sensitive operations bypass cache. Last-admin protection
  prevents demoting/removing the sole admin. Migration promotes all existing
  users to admin for zero disruption. New `tenant_members` table unifies
  authorization across admin/oidc_user/tenant_user sources. Adds
  `attune_authz_denied_total` metric.

- **OIDC SSO for Console (#40).** Enterprise single sign-on via standard OpenID
  Connect. Features: PKCE (S256) + nonce for security, group-based role mapping
  (admin/member), AES-256-GCM encrypted state cookies, configurable allowed
  groups, optional `oidc_only` mode to hide local login. New endpoints:
  `/auth/oidc/start`, `/auth/oidc/callback`, `/auth/providers`. Console login
  UI shows SSO button when OIDC is configured. Adds `oidc_users` table for
  OIDC user persistence with group sync on each login.

- **HDBSCAN clustering for digest themes (#27).** Implemented pure-Go HDBSCAN
  algorithm (`internal/pkg/hdbscan/`) for automatic theme discovery. When
  embeddings are available, digest aggregator clusters feedback by semantic
  similarity and names clusters via LLM. Replaces the naive hardcoded-3-themes
  approach with dynamic 5-15 theme discovery, centroid-based example selection,
  and per-cluster naming. Falls back to naive LLM path when embeddings are
  insufficient. Console API exposes `clustering_enabled` toggle in
  `/digest-subscription` for operators to enable/disable per tenant.

- **Outbound channel-adapter framework (#34).** `internal/outbound/` provides
  the pluggable delivery SDK mirroring `internal/inbound/`. Features:
  composition interfaces (`EventChannel` / `DigestChannel`) with compile-time
  capability discovery; self-registration via `init()`; content-hash signing
  (`sha256(canonical(envelope))`) for field-order-independent HMAC verification;
  response checkers for webhook and GitHub semantics; depguard rules enforcing
  the adapter boundary. Outbox worker now dispatches via `outbound.LookupEvent`
  instead of a hardcoded switch.

- **Lark and Slack delivery adapters (#34).** Native card/Block Kit rendering
  for Lark (Feishu) and Slack. Lark adapter supports custom bot in-body signing
  (`timestamp` + `sign` fields). Both channels support event notifications and
  daily digests.

- **Console destination type select (#34).** The notify targets dialog now has
  a typed destination select (Raw Webhook / GitHub Issue / Lark / Slack) with
  per-channel help text for URL and secret fields.

- **Signature version column (#34).** `tenant_notify_targets.signature_version`
  enables gradual rollout of content-hash signing. Values: `v2-content-hash`
  (new default, field-order independent) or `v2-bytes` (legacy, for customers
  not yet upgraded).

- **Multi-channel digest delivery (#34).** The digest worker now fans out to
  all targets with `audience=digest`, not just raw-webhook. A tenant can
  configure Lark + Slack + raw-webhook simultaneously; each receives the
  digest in its native format (card/Block Kit/JSON).

- **Digest enrichment (#27).** Daily digests now include period-over-period
  deltas (↑/↓ arrows), 7-day sparkline trends, theme lifecycle badges
  ([NEW]/[BACK] for new vs returning themes), example quotes per theme,
  and deep links to the Console. The markdown rendering is rewritten
  severity-first with all enrichment data visible.

- **Per-channel test-send (#34).** The Console "Test" button now works for
  Lark and Slack targets, not just raw-webhook. Lark test sends include
  in-body signature when a secret is configured; Slack test sends use
  Block Kit format. Per-channel response checking validates `StatusCode`
  for Lark and status code + body for Slack.

### Fixed

- **Notify target test-send stability (#63).** Console notify-target test sends
  now use an isolated OTel-wrapped HTTP transport instead of the shared default
  client, avoiding cross-test idle-connection interference in race/coverage runs.

- **Serialized startup migrations (#42).** `database.RunMigrations` now takes a
  PostgreSQL advisory lock so multiple replicas cannot race schema migrations
  during Kubernetes rollouts or other parallel starts.

- **Kubernetes cold-start database readiness (#42).** Server startup now retries
  the initial PostgreSQL ping for a bounded window, avoiding container restarts
  while Kubernetes Service DNS and embedded Postgres endpoints settle.

- **Kubernetes service selector isolation (#42).** The chart now labels and
  selects Attune app pods with `app.kubernetes.io/component=app`, preventing the
  main Service and ServiceMonitor from matching embedded Postgres or Helm test
  pods.

- **Console audit timeline ID handling (#42).** Feedback audit queries now keep
  protoJSON int64 feedback IDs as strings instead of converting them through
  JavaScript numbers, avoiding `NaN` paths and large-ID precision loss. Console
  coverage tests also use a coverage-only timeout budget and default workflow
  audit/state mocks so the frontend CI gate is stable.

- **Lark/Slack destination validation (#34).** Handler now accepts `lark`
  and `slack` as valid `destination_type` values. Previously the switch
  statement rejected them with "destination_type value is not allowed".

- **Adapter digest JSON field tags (#34).** Lark and Slack adapters now
  correctly deserialize digest view fields (`Stats`, `Themes`, `Items`)
  using uppercase JSON tags matching the source struct. Previously the
  lowercase tags caused silent fallback to the generic markdown renderer.

- **UTF-8 safe truncation (#34).** New `internal/pkg/truncate` package
  provides `Bytes()` and `Runes()` functions that never split multi-byte
  UTF-8 characters. All outbound adapters and notify test_send now use
  this shared implementation instead of inline truncate functions.

- **Nil Feedback map panic (#34).** Lark, Slack, and GitHub Issue adapters now
  handle `env.Feedback == nil` gracefully instead of panicking on map access.

- **Invalid signature_version rejection (#34).** Generic adapter now returns
  an error for unknown `signature_version` values instead of silently
  falling through to content-hash signing.

- **Lark card note element format (#34).** Fixed Lark interactive card JSON
  structure for `note` elements. The inner elements now use plain `content`
  strings instead of nested `larkText` objects, matching Feishu's card spec.

- **Outbox routing for Lark/Slack (#34).** Added `lark` and `slack` to the
  `outboxDestTypes` map so enriched feedback triggers outbox delivery to
  these channels. Previously only `raw-webhook` and `github-issue` were
  routed through outbox.

- **Shared digest model package (#34).** `internal/outbound/digestmodel/`
  provides shared types for digest rendering (View, Result, Stats, Theme,
  Item, Deltas, DeltaValue). Lark and Slack adapters now use type aliases
  to this package, eliminating 7 duplicate struct definitions.

- **Shared Lark signing package (#34).** `internal/pkg/larksig/` provides
  `Sign(timestamp, secret)` for Lark custom bot signature generation.
  Used by both the Lark adapter and test_send, eliminating duplicate
  implementations.

- **Consistent label format (#34).** Outbound adapter labels now use
  `{channel}-{kind}-{tenant}` format (e.g., `generic-event-tenant1`,
  `lark-digest-tenant1`). GitHub Issue adapter uses `github-issue-{owner}/{repo}`
  as it targets repositories rather than tenants.

- **Non-HTTP channel support (#34).** `outbound.Rendered` now supports two
  delivery modes: HTTP (Build+Check) for webhooks/REST APIs, and Custom
  (Send) for non-HTTP channels like email/SMS/push. Existing adapters
  continue to use HTTP mode unchanged; new adapters can use `Send` for
  direct delivery control.

### Removed

- **Dead inline notifier path (#34).** Deleted `notify.Notifier` interface,
  `MultiNotifier`, `RawWebhookRouter`, `buildNotifier`, `Enricher.SetNotifier`,
  and `Enricher.fanOut`. All delivery now goes through the outbox worker with
  the #34 outbound adapter framework. This removes ~400 lines of unreachable
  code that predates the outbox pattern.

- **Cursor pagination for job list endpoint (#30).** `GET /fb/v1/console/jobs`
  now supports cursor-based pagination via `cursor` query parameter and returns
  `next_cursor` in the response. Consistent with other list APIs.

- **Batch operations for feedback (#30).** Operators can now apply bulk changes
  to feedback rows via `POST /fb/v1/console/feedback/batch`. Supports tag
  add/remove, workflow state transitions, and soft/hard delete. Selection modes:
  explicit ID list (max 100) or filter-based query. Safety features: idempotency
  key for safe retries, dry run mode for previewing affected items, optimistic
  locking via `if_unmodified_since` header. Large batches (>100 items) execute
  asynchronously with job tracking (`GET/POST /fb/v1/console/jobs`). Background
  worker with heartbeat, stuck job recovery, and real-time progress updates.
  Rate limiting: 30/min for tag/workflow ops, 10/min for delete ops.

- **Semantic search for feedback (#30).** New `POST /fb/v1/console/feedback/search`
  endpoint enables natural-language search across feedback content. Hybrid search
  combines semantic similarity (pgvector embeddings) with keyword fallback for
  optimal results. Query embedding cache improves performance for repeated
  searches. Configurable similarity thresholds and semantic/keyword weights.
  Rate limit: 60/min.

- **Console UI enhancements for batch and search (#30).** Enhanced
  `SelectionActionBar` with delete action, loading states, and batch confirmation
  dialogs. Job progress component displays real-time execution status.
  Semantic search bar with results display and similarity indicators shows
  how closely each result matches the query.

- **Customizable feedback workflow status (#29).** Per-tenant workflow state
  machine with three fixed categories (open / active / closed), custom states
  within each category, and a directed-graph transition edge table enforcing
  allowed moves. Features: single and batch state transitions with 409 on
  invalid moves; field-level audit log (`feedback_audit_log`) recording every
  state change with optional comment; seed-defaults endpoint for one-click
  setup; workflow settings page in Console (Settings → 工作流) with state CRUD,
  color picker, category selector, and interactive transition matrix editor;
  workflow state badge on feedback list rows; state filter in the feedback
  filter bar; transition dropdown + audit timeline in feedback detail sheet;
  batch transition in the selection action bar; auto-seed on first visit
  (empty state list triggers `SeedDefaults` automatically); new feedback
  automatically assigned the tenant's default workflow state on ingest
  (SQL subquery, no extra round-trip). Migration 030
  (`tenant_workflow_states`, `tenant_workflow_transitions`,
  `feedback_audit_log`, `ALTER user_feedback`), proto contract
  (`WorkflowService` with 10 RPCs), integration tests, Prometheus metrics
  (`attune_workflow_transitions_total`, `attune_workflow_batch_size`), and
  zh-CN i18n included.

### Fixed

- **Rate limiter memory cleanup (#30).** Added `StartCleanup()` to
  `MemorySlidingLimiter` to periodically evict keys with no recent activity,
  preventing unbounded memory growth from abandoned rate limit keys.

- **Audit log cursor pagination skipped one record (#29).** The keyset cursor
  used the overflow row's ID (`out[limit]`) instead of the last returned row's
  ID (`out[limit-1]`), causing `id < cursor` to skip one entry on the next page.

- **Workflow allowed-next-states query returned empty due to column ambiguity
  (#29).** The `AllowedNext` SQL joined `tenant_workflow_states` with
  `tenant_workflow_transitions` but used unqualified column names (`id`,
  `tenant_id`, `created_at`), causing PostgreSQL `42702 ambiguous column`
  errors silently swallowed by the handler. Added table-qualified column
  constant `selectStateColsQualified` and used it in the JOIN query.

- **Manual feedback tags (#28).** Per-tenant tag registry with colors,
  descriptions, exclusive scopes (at most one tag per scope per feedback row),
  usage tracking, and archival. Tags can be assigned/removed on individual
  feedback rows or in batch (up to 100 rows × 20 ops). The feedback list
  supports `?tag=<uuid>` filtering, and both list and detail endpoints hydrate
  assigned tags. Console UI: Settings → 标签 for CRUD management; feedback
  detail sheet for add/remove via dropdown; **improved UX**: tags visible in
  list rows (below title), tag filter in the filter bar, Combobox with search
  + inline creation, checkbox multi-select with floating batch action bar,
  rich tooltip on tag badges (description, exclusive scope, usage count).
  Proto contract (`FeedbackTagService`), migration 029 (`tenant_feedback_tags`
  + `feedback_tag_assignments`), integration tests, and zh-CN i18n included.

### Fixed

- **Tag add/remove did not refresh feedback detail sheet.** The query
  invalidation key in `useAddFeedbackTag` and `useRemoveFeedbackTag` included
  a `feedbackId` segment that prevented React Query prefix-matching from
  reaching the detail query (`['console', 'feedback', 'detail', id]`).
  Broadened both to `['console', 'feedback']` so all feedback queries refresh.

- **Daily digest roll-up with LLM-labeled top themes (#27).** A new per-tenant
  scheduled worker (`internal/service/digest`) delivers one morning summary of
  yesterday's enriched feedback instead of per-row noise. At the tenant's local
  send hour it aggregates the day's feedback and surfaces the top themes by
  **reusing the #114 embedding clusters** — counts and example IDs are
  SQL/code-derived, never LLM-fabricated; a naive single LLM call (over the
  already-configured `enrich` route, so no new routing config) is the fallback
  for clustering-off tenants — then POSTs a rendered JSON+markdown payload to the
  tenant's `audience='digest'` raw-webhook target — created in the notify-targets
  UI/API (Settings → 通知目标, audience=digest) — via the shared
  `notify.Transport`. The schedule is a first-class entity, configurable in
  Console (Settings → 日报摘要) and over the API
  (`GET/PUT/DELETE /fb/v1/console/digest-subscription`): enabled, daily/weekly,
  local send hour, weekday, LLM theme threshold, send-on-empty, prompt override.
  Volume tiers the output — 0 enriched rows skip (unless opted in), 1–5 send a
  themeless list, ≥ threshold send LLM themes. A `digest_runs(tenant_id,
  run_date)` ledger with a `UNIQUE` claim guarantees **at most one digest per
  tenant per local day** across restarts and replicas; the scheduler is
  timezone-/DST-correct (civil-time math, no cron dependency) and defers when the
  embedding queue is backlogged so themes aren't computed on half-clustered data.
  Migration 027 adds `digest_subscriptions` (+ a `digest` audience value on
  `tenant_notify_targets`); migration 028 adds `digest_runs`.
- **Per-feedback LLM reply draft (#26).** After classification, an opt-in
  per-tenant pipeline pre-generates an empathetic, operator-facing reply draft
  via a second LLM call. It runs on a new async `reply_draft_task` outbox +
  worker — built on a new generic `internal/repo/taskoutbox` queue that
  `embedding_task` was refactored onto — so a draft-LLM failure is isolated from,
  and never rolls back, the classification result. The shared `taskoutbox` queue
  claims with `FOR UPDATE SKIP LOCKED` and enforces exponential-backoff retries
  (a queue fix that also corrects `embedding_task`); a single draft call is
  bounded by a timeout so a hung provider can't stall the worker. Enablement is
  per-tenant
  (`tenants.reply_draft_enabled`, default off — it doubles LLM cost) with an
  optional confidence gate (`reply_draft_min_confidence`, only draft rows whose
  classification confidence clears the threshold) and a prompt override
  (`reply_draft_prompt_template`). The draft is overwrite-stored on
  `user_feedback.reply_draft`; token usage and cost are recorded in `llm_audit`
  with `purpose='reply_draft'` through the existing audit-wrapping client (no
  schema change). Console shows the draft below the raw content with Copy and a
  synchronous Regenerate
  (`POST /fb/v1/console/feedback/{id}/reply-draft/regenerate`), which is
  tenant-scoped, guarded (ownership / opt-in / enriched), rate-limited by both a
  per-row cooldown and a per-tenant ceiling, and stays reachable for an
  enabled-but-empty row via a Generate entry point. The draft is
  operator-facing only and is never auto-sent. Migration 026 adds the feedback
  and tenant columns plus the `reply_draft_task` table; new metrics
  `attune_reply_draft_generated_total`, `attune_reply_draft_errors_total`,
  `attune_reply_draft_duration_seconds`, `attune_reply_draft_queue_depth`.
- **Embedding-based feedback clustering (#25).** Adds semantic deduplication of
  user feedback using pgvector embeddings with HNSW indexing. Feedback items
  are automatically grouped into clusters when cosine similarity exceeds a
  configurable threshold (default 0.85). Uses Matryoshka 256-dim embeddings for
  6x storage savings with ~95% quality retention. The implementation follows
  an outbox pattern (`embedding_task` table) for reliable async processing
  with backpressure and retry. Clusters with 3+ members get LLM-generated labels.
  Migration 024 enables the pgvector extension and adds embedding columns,
  cluster assignment, and tenant clustering config. The embedding worker
  processes tasks and writes usage to `llm_audit` with `purpose='embed'`.
  Includes metrics: `attune_embed_cluster_assignments_total`,
  `attune_embed_errors_total`, `attune_embed_duration_seconds`,
  `attune_embed_queue_depth`. Requires pgvector >= 0.5.0 for HNSW indexes.
  Console: adds independent `/clusters` page with keyset-cursor pagination,
  virtual scrolling via react-window v2, search/filter/sort controls, and
  sidebar member details. The clusters card on the feedback page shows a
  summary with a link to the full clusters page. Cursor pagination format:
  `"unix_nanos:uuid"` for clusters, `"unix_nanos:id"` for members.
- **Config-first runtime and DB-managed LLM channels (#23).** Adds a
  Tink-backed shared secret store (`secrets.tink_keyset`), DB metadata for
  runtime secret-key registry state, managed `llm_channels`,
  `llm_channel_abilities`, and `llm_routes` tables, Console API endpoints,
  a `/console/llm-config` React management page, `attune llm ...` CLI commands
  for provider CRUD/routing, and a DB-backed LLM router that records
  channel/protocol/provider-model metadata in `llm_audit`. Channel `api_key`
  input is write-only and persisted encrypted with the shared Tink keyset.
  The Console/API can discover provider model IDs from a channel and use them
  as selectable candidates in ability/test forms while still allowing manual
  entry for local or non-discoverable providers. Console admin sessions minted
  before the first tenant exists now self-heal to the first active tenant once
  one is created, so tenant-scoped pages such as Feedback do not require a
  logout/login cycle after first-tenant bootstrap.
  Startup now refuses split-brain secret-key rollouts when stored LLM
  credentials, inbound outer configs, or nested webhook/email ciphertexts
  reference key ids missing from the local keyset; it also rejects LLM
  credential rows whose stored key-id metadata disagrees with the Tink
  ciphertext prefix. Legacy inbound AES-GCM envelopes can be read with the
  migration-only `secrets.legacy_inbound_master_key` and rewritten to Tink with
  `attune secrets reencrypt --apply`. Runtime LLM routing now applies
  per-channel timeouts, skips routeable bearer channels without credentials,
  and fails over across eligible channel candidates. The command family
  `attune secrets keyset-info|add-key|set-primary|reencrypt|retire-key|delete-key`
  provides an explicit distributed key rotation path. The managed LLM surface is
  admin-gated, validates tenant routes and provider base URLs, sanitizes channel
  test errors before persistence, prevents disabled tenant routes from falling
  back to global routes, preserves existing encrypted credentials on metadata
  edits, and records enrichment retry/backoff metadata so permanent provider or
  persistence failures do not burn tokens forever.
- **LLM classification confidence review signal (#24).** The enricher prompt
  now asks models for `classification_confidence` in `[0.0, 1.0]` with `0.5`
  defined as ambiguous enough for human review. The parser accepts numeric and
  numeric-string values, clamps out-of-range responses, preserves missing or
  invalid values as `NULL`, stores the fast snapshot on
  `user_feedback.classification_confidence`, records structured confidence
  evidence in `semantic_extraction_runs`, and surfaces green/yellow/red
  confidence indicators in the console list and detail views.
- **Per-tenant LLM cost observability (#24).** Adds the `llm_audit` call-level
  fact table, a provider-agnostic LLM audit wrapper, a vendored LiteLLM model
  price catalog, Prometheus metrics for LLM calls / tokens / estimated USD cost,
  `GET /fb/v1/console/llm-usage` with
  day/week/month grouping plus Grafana-style range filters, a console LLM cost dashboard, and
  `observability/dashboards/llm-cost.json` for Grafana. A scheduled workflow
  opens update PRs when the upstream LiteLLM price catalog changes.
- **Language-aware enrichment (#22).** Adds dependency-free source-language
  detection for feedback rows (`zh`, `en`, `ja`, `unknown`), persists the
  detected code on `user_feedback.language`, keeps native `title` /
  `rationale` in the source language, generates tenant-locale display
  summaries in new `enriched_display_*` fields, records language/prompt
  provenance in semantic extraction runs, includes native/display summaries in
  webhook envelopes, and renders source-language badges plus tenant-locale
  summaries in the console list and detail views.
- **Semantic understanding layer foundation + customer tone (#21).** Extends
  metadata-driven Dimensions with descriptions, examples, extraction hints, and
  renderer metadata; adds `sentiment` as the customer-feedback pack's default
  Customer tone dimension (`positive` / `negative` / `neutral` /
  `frustrated`); and introduces `semantic_extraction_runs` so each full LLM
  classification records model/schema provenance, attrs, rationale, and
  structured dropped-attr diagnostics separately from the fast
  `user_feedback.enriched_attrs` snapshot. Renderer metadata is now validated
  at the Dimension boundary, and the codebase has a typed
  `customer_feedback.v1` semantic pack fixture for future vertical packs.
- **Source-aware LLM guard policy foundation (#20).** Adds a
  `guard_policies` ruleset table, `internal/infra/llmguard` LLMClient wrapper,
  DB-backed policy resolver, Console API endpoints for per-policy
  create/patch/delete plus tenant bulk replace and effective-policy preview,
  bounded guard metrics, and a standalone `docs/guardrails.md` maintenance
  guide. Guard policies resolve by tenant, channel, source, source tags,
  purpose, and stage with `baseline` / `default` / `override` semantics; YAML
  remains bootstrap-only rather than the long-term policy store.
- **PostgreSQL integration tier (#12).** Adds a dual-mode
  `internal/testdb` harness: CI runs `make test-integration` against a
  GitHub Actions `postgres:18` service container, while local runs fall
  back to `postgres:18` via testcontainers-go when no
  `ATTUNE_TEST_DATABASE_URL` is set. PostgreSQL smoke suites are
  centralized under `test/integration/postgres/<area>` and protected by
  `scripts/lint-integration-layout.sh` so future integration tests do
  not drift back into package-adjacent `*_io_test.go` files. The tier
  adds real Postgres smoke coverage for migrations, the Lark-delete
  preflight guard, feedback JSONB queries, API key issue/lookup/revoke,
  tenant + notify-target CRUD, admin bootstrap/lockout state, inbound
  source repo state, console inbound delete branches, and the ingest →
  enrich → outbox queue → outbox drain path. `OutboxWorker.ProcessOnce(ctx)`
  exposes one deterministic batch-drain cycle for tests and future
  manual drain use.
- **Dispatcher-owned HTTP response emission across attune-owned routes (#99).**
  Remaining console auth, change-password, inbound source management,
  webhook inbound ingest, API-key/session/rate-limit middleware, and `/healthz`
  now emit responses through `internal/dispatcher`. The dispatcher grew
  middleware rejection helpers, a fixed health-check response helper, and an
  option-driven `Bind(..., WithAuth(...))` path that covers both context-auth
  routes and webhook pre-auth source lookup/HMAC verification. `RequestContext`
  now exposes only cookie side effects, keeping
  response body/status writing owned by dispatcher. `respond` is now a
  low-level encoder used by dispatcher instead of a production handler
  dependency. A new
  `scripts/lint-http-response-emission.sh` gate is wired into
  `scripts/check.sh` to block future direct `respond.*`, `WriteHeader`, or
  `http.Error`/`http.SetCookie` response emission outside dispatcher/respond-owned
  code. Rate limiting now returns the standard error envelope with proto code
  `RATE_LIMITED` and a bucket-derived `Retry-After`; console test-connection
  now surfaces dispatcher error-envelope messages on malformed requests.

- **Console UI: inbound sources page + admin change-password (#66 B2 / H10).**
  Fills the SPA gap left by the backend-first #66 landing — operators now
  manage webhook + email inbound sources directly from the console
  instead of via curl. New `/inbound-sources` route covers list, create
  (two-channel wizard: webhook = name only, email = full IMAP fields +
  inline test-connection probe), one-shot webhook secret reveal on
  create + rotate, pause / resume toggle, and delete. The reveal dialog
  surfaces secret_hex + the public webhook URL + a curl example with a
  copy-button affordance per field. A new `/change-password` route lets
  console admins rotate their bootstrap password (current ≥ 12-char new,
  confirm match, server-side bcrypt cost 12 with timing-equalised wrong
  current-password rejection). The user menu in the TopBar gets a
  "Change password" item for admins; tenant-user sessions are filtered
  client- and server-side. Backend wires
  `POST /fb/v1/console/me/change-password` as a new RPC in
  `proto/attune/v1/session.proto`, an admin-only
  `auth.ChangePasswordHandler`, and `admin.Repo.UpdatePasswordHash` —
  all under the existing `RequireSession` + CSRF guard.
- **Channel-agnostic inbound framework + channel-native console auth (#66).**
  attune now serves as a self-hosted, multi-source feedback ingestion plane
  with a unified port (`internal/inbound.Adapter`) shared across push, poll,
  schedule, and stream modes. Two production adapters ship alongside the
  framework:
  - `internal/inbound/adapter/webhook` — Stripe-style `X-Attune-Timestamp` /
    `X-Attune-Signature` HMAC-SHA256 over `"<ts>.<body>"` with a ±300 s replay
    window, dual-secret rotation (24 h grace, then `409
    rotation_in_grace_window`), and per-source 401 enumeration resistance
    (same status + path + stub-HMAC timing for unknown slugs).
  - `internal/inbound/adapter/email` — IMAP poller (TLS-only) using
    `emersion/go-imap/v2` + `go-message`; multipart/alternative prefers
    `text/plain`; `lastUID` cursor advances per poll; `after_ingest:
    mark_seen` (default), `keep_unseen`, and `move_to:<folder>` all drive
    the IMAP STORE / MOVE wire commands (the v0.3 review-H3 follow-up
    replaced the documented v2-beta no-op with `clientOps.MarkSeen` /
    `clientOps.MoveTo` against a narrow `imapOps` interface).
  - **Encryption at rest**: every inbound secret (webhook HMAC,
    IMAP username / password) is sealed by the shared Tink AEAD runtime
    keyset (`secrets.tink_keyset`), the same keyring used for managed LLM
    provider credentials. The earlier inbound-only master-key envelope is
    replaced by the #23 Tink key registry and rotation commands.
  - **Boundary enforcement**: two depguard rules ship in `.golangci.yml` —
    `inbound-boundary` (framework core `internal/inbound/*` may NOT import
    adapters under `.../adapter/*`) and `inbound-framework-isolation`
    (framework may NOT import service / repo / handlers / notify). Adapters
    self-register via `init() + inbound.Register`, blank-imported from
    `cmd/attune/main.go` (Caddy/Bento pattern) — adding a new channel is one
    package with no edits to `cmd/`.
  - **Conformance**: `internal/inbound/inboundtest` ships fakes
    (FakeIngest / FakeSources / FakeSecrets / FakeMetrics / FakeMux)
    and a `TestAdapterContract` with five gates every adapter must
    pass (ChannelNonEmpty / StartShutdownOK / CtxCancelGraceful /
    IdempotentShutdown / DuplicateRegisterPanics).
  - **Console**: a first-class **inbound sources** page at
    `/console/inbound-sources` — CRUD + rotate + pause/resume + test
    connection, served by `internal/handlers/console/inbound` against the
    proto contract in `proto/attune/v1/inbound_source.proto`
    (`InboundSourceService` × 8 RPCs).
- **Console authentication: email + bcrypt password** (#66). The
  Lark-OAuth login is removed; the console now signs in via
  `POST /fb/v1/console/install/login` against bcrypt-hashed credentials in a new
  `admins` table (migration `016_create_admins.sql`). bcrypt cost 12 +
  timing-equalized dummy-hash on unknown emails keeps the login path
  constant-time. A safe-redirect helper rejects open redirects on the
  `next=` query. Session cookies retain `HttpOnly + Secure +
  SameSite=Lax + Path=/`.
- **Bootstrap admin** (#66). On first start, attune reads
  `console.bootstrap_admin` from the private YAML config and creates the first
  console admin. TOCTOU-safe via `pg_advisory_xact_lock` + `ON CONFLICT (email)
  DO NOTHING`; subsequent starts read `admins` and skip bootstrap creation.

- **`internal/dispatcher` typed HTTP helper and product API migration.** Adds a generic bind/result layer with `Empty` / `JSON` / `Path` / `Query` / `Param` / `ParamInt64` / `Combine` / `Custom` input helpers, moves all 18 in-scope product endpoints onto it, and adds typed session/API-key auth contexts for dispatcher handlers.

- **`cmd/lint-errorcode` / `scripts/lint-errorcode.sh` — bans hand-written `ErrorResponse.Code` string literals.** The lint keeps the compatibility string field routed through `attune.v1.ErrorCode` by failing on `attunev1.ErrorResponse{Code: "..."}` drift.

- **Console SPA test suite (#13).** Vitest (jsdom) + MSW + Testing
  Library + v8 coverage. ~80 cases cover the api-client (CSRF
  injection, error envelope, signal), the i18n resolver, the
  `meQuery` CSRF side effect, the `_authed` route guard, the feedback
  / settings / api-keys / notify-targets queries and mutations
  (including the `EditNotifyDialog` sparse-PATCH diff and the
  `useUpdateEnrichConfig` cache-write side effect), and the `dim`
  components (`i18n-input` and `dimensions-editor`'s WeakMap-based
  identity tracking). Per-file coverage thresholds on 17 surfaces
 gate CI against regressions.

### Changed

- **Migration 031 adds batch infrastructure columns and tables (#30).**
  `user_feedback` gains `updated_at` (trigger-maintained) and `deleted_at`
  (soft delete support). New tables: `idempotency_keys` for batch retry safety,
  `batch_jobs` for async job execution tracking.

- **All LLM provider HTTP clients now set a request timeout.** The chat backends
  (OpenAI-compatible, Anthropic, Gemini, OpenAI-Responses) previously had no
  `http.Client` timeout — only the embedding client did — so a hung or cold
  provider could block an enrich, embedding, or reply-draft worker (or a
  synchronous Regenerate request) indefinitely. They now share a 120s timeout.
- **Breaking: process config is now YAML-only (#23).** `attune` reads one
  private config file via `--config` (default `config.yaml`) and rejects unknown
  old fields. Database URL, console bootstrap, migration guard, observability,
  rate limits, custom webhook bootstrap, and the shared Tink keyset now live in
  YAML; LLM provider state moved to DB-managed channels/routes.
- **Console settings now owns configuration navigation.** The top navigation
  keeps daily-use Feedback, Usage, and Settings entries in that order, while
  `/settings` exposes an in-page sidebar for AI classification, Guardrails,
  inbound sources, notify targets, and API key management instead of sending
  those configuration workflows out to standalone pages.
- **Error response codes are now proto-owned UPPER_SNAKE names.** `ErrorResponse.code` remains a string field for protobuf compatibility, but values are normalized from previous lower_snake strings such as `validation` / `bad_request` to `VALIDATION` / `BAD_REQUEST` via `attune.v1.ErrorCode`. Pre-1.0 breaking change for clients that switch on the `code` string.

- **CI now verifies downloaded `buf` binaries by SHA-256 and ignores generated protobuf Go in the duplication gate.** The `jscpd` gate scans hand-written Go with `**/*.pb.go` excluded, keeping generated code noise out of the duplication signal.

- **`lint-rawptr` now recognizes exported generic selector type arguments.** This prevents false positives for helper values such as `dispatcher.JSONBody[*Req]` while still reporting real value-position pointer dereferences.

- **#66 review pass: 13 audit findings inline-fixed** — C1-C5 / H1-H6 /
  M2 / M4-M8.
  - `respond.Error` / `respond.Proto` unified the ingest + console
    response paths; deleted local `writeJSONProto` / `writeError` /
    `errInternal` shims (C1, M1).
  - Removed dead `auth.Handler.Routes` + `Logout` (covered by `me.Logout`
    via the documented session route — C2), `session.OAuthStateCookie /
    OAuthStateTTL` constants leftover from T17 OAuth removal (C3).
  - `InboundSource` proto gained `created_at` / `updated_at` so the
    contract matches the SQL row (C4); both Repo selects and the
    console `listAllForTenant` SQL scan the new columns.
  - `login.tsx` now uses the proto-generated `LoginRequest` /
    `LoginResponse` types and canonical `redirectUri` camelCase key
    (C5).
  - `webhook.Config` + `email.Config` exported from the adapter
    packages and reused by the console handler — deleted the duplicated
    `webhookConfigEnvelope` / `emailConfigEnvelope` (H1).
  - `EmailCreateConfig.poll_interval_seconds` retired (field reserved
    in proto) — the batch-loop topology has only ever honoured a fixed
    60s `loopInterval`, the per-source knob was unconsumed (H2).
  - `dialIMAP` shrunk to `(ctx, addr, *imapclient.Options)` after the
    `cfg.TLS` bool was deleted in the TLS-only refactor (H3).
  - `inbound.BootstrapValidate` now calls
    `internal/infra/config.GetOrFile(MasterKeyEnv)` instead of a
    local `readKeyEnv` reimplementation — one `*_FILE` semantic for the
    whole codebase (H4).
  - `me.MeHandler.Logout` returns `200 + LogoutResponse{}` instead of
    `204 No Content` so the OpenAPI shape stays consistent with every
    other proto RPC (H5).
  - Docs corrected: `Settings → Inbound Sources` → "the Inbound Sources
    page, route `/console/inbound-sources`" in `docs/private-deploy.md`
    + this changelog (H6).
  - `email.nowFn` rewritten as the idiomatic
    `var nowFn = time.Now` (M2).
  - `console/src/components/loading.tsx` consolidates the three
    identical `Loading` spinners on inbound-sources / feedback /
    notify-targets routes (M4).
  - Deleted `EmptyInboundSourcesIcon` re-export — routes import
    `lucide-react` directly per the rest of the SPA (M5).
  - Channel literal sources collapsed to `webhook.Channel` /
    `email.Channel`; the console handler aliases via
    `channelWebhook = webhook.Channel` etc., so changing the channel
    name requires exactly one edit (M7).
  - Lizard-verified the `Login` / `decodeLoginRequest` /
    `authenticate` / `resolveAdminScope` split: inlining would push the
    merged `Login` to CCN ~24 against the `≤15` gate — split is
    justified, comment locked in (M8).
- **Email adapter `after_ingest` policy (`mark_seen` / `keep_unseen` /
  `move_to:<folder>`) now actually fires the IMAP STORE / MOVE — previously
  a documented no-op in v0.3 (review H3, #66).** The original
  `applyAfterIngest` shipped as a comment-heavy no-op citing the
  `go-imap/v2` beta API; revisiting the cached source showed `Move` exposes
  `Wait() (*MoveData, error)` and `Store` returns a `*FetchCommand` whose
  `Close()` cleanly drains the wire. The implementation now narrows the
  client surface to an `imapOps { MarkSeen(uid); MoveTo(uid, mailbox) }`
  interface — production wraps a live `*imapclient.Client`, tests drop in
  a recording stub. STORE/MOVE failures are logged via
  `logext.Warnf` and swallowed: the `lastUID` cursor in
  `pollSource` is the correctness primitive and has already advanced, so
  a one-shot wire failure is recoverable on the next round without
  duplicate ingest. `move_to:` with an empty folder degrades to
  `mark_seen` rather than passing an empty mailbox to IMAP.

### Removed

- **Removed env-var and `*_FILE` runtime config paths (#23).** The old
  `FEEDBACK_API_*`, `ATTUNE_INBOUND_MASTER_KEY`,
  `ATTUNE_BOOTSTRAP_ADMIN_*`, `ATTUNE_CONFIRM_LARK_DELETE`, and
  `OTEL_EXPORTER_OTLP_*` process configuration paths no longer affect attune
  runtime configuration.

### Fixed

- **Console dev login now respects the Vite basepath.** Local admin login and
  logout route to `/` / `/login` in Vite dev and `/console/` /
  `/console/login` in production, avoiding a post-login Not Found during
  browser smoke tests.
- **CodeQL `go/allocation-size-overflow` on AES-GCM encrypt paths**
  (#66 / commit `e6142cc`). The `internal/inbound/secrets.go` and
  `internal/inbound/inboundtest/fakes.go` Encrypt functions now bound
  the plaintext at 1 MiB before constructing the output slice, which
  prevents an attacker-controlled length from causing an integer
  overflow in slice capacity arithmetic. CI's CodeQL "Go" job was
  failing on the first scan; the cap matches the inbound-source
  config / webhook secret use case (both under 1 KiB in practice).

### Security

- **PII guardrails now redact sensitive feedback before LLM calls (#20).**
  Migration `018_guard_policies.sql` seeds a system `default` policy for
  `purpose=enrich` / `stage=llm_input` that redacts email, phone, Chinese
  mobile, Chinese ID, and Luhn-validated credit-card entities before provider
  calls. The canonical `user_feedback.content` row remains the original
  user-submitted text; guard logs and metrics record only bounded entity/action
  counts, never raw matched values. Because the seeded policy is a default and
  not a baseline, tenants/sources can later relax it for trusted local LLM paths
  or tighten it with blocking policies for regulated sources. Public API-key
  ingest strips reserved inbound-source metadata so clients cannot spoof a
  trusted source override, and LLM parse failures no longer persist or log raw
  model output.
- **#66 hardening pass (1 BLOCKER + 6 HIGH + 8 MEDIUM after Phase-4
  Chrome E2E)**.
  - Email adapter Shutdown now honours the per-adapter timeout —
    `wg.Wait()` is multiplexed against `ctx.Done()` so a wedged IMAP
    `Login/Select/UIDSearch/fetchOne` blocking read can no longer pin
    the whole process at shutdown (B1).
  - IMAP LOGIN failures only auto-disable the source on
    AUTHENTICATIONFAILED — transient network errors stay transient,
    preventing a single bad TCP round-trip from flipping a healthy
    source off (H-1).
  - Per-message size cap (`maxMessageBytes = 8 MiB`) on email fetch +
    parse paths. Over-size messages mark `validate_err` and advance
    the cursor; bounds peak per-tick RSS at ~800 MiB against a
    malformed / hostile server (H-2).
  - `Rotate` handler now uses the `next_eligible_at` value the
    rotator computed (and persisted into the DB envelope) — the
    response field and the actual grace boundary agree to the
    nanosecond. Prior recompute drifted by microseconds (H-3).
  - Webhook `adapter.stubSecret` cached field deleted; `handle` calls
    the `ProcessStubSecret` package-level sync.Once singleton
    directly. Two layers of caching collapsed to one (H-4).
  - SSRF guard at both runtime poll and console TestConnection:
    `email.ValidateOutboundHost` rejects link-local (covers AWS / GCP
    IMDS 169.254.169.254), unspecified, and IPv4 multicast. Loopback
    + RFC1918 stay allowed on purpose for on-prem deployments (M-3).
  - Decrypted IMAP password returned as `[]byte` instead of `string`
    and explicitly zeroed after LOGIN — Go strings are immutable, so
    the prior `string` return pinned plaintext until GC. (M-4)
  - Bcrypt lockout: `IncrementFailedAttempts` only sets
    `locked_until` on the exact `failed_attempts + 1 = $threshold`
    transition (was `>=`), AND the auth handler resets
    `failed_attempts` after a prior lockout expires — together they
    close the indefinite-DoS-of-legitimate-admin loophole (M-1).
  - `BootstrapAdmin` now enforces the same 12-character password
    floor as `ChangePassword` — the operator can no longer ship a
    weak first-admin password by env-var (M-2).
- **#66 cleanup pass** (M-5 / M-6): collapsed the consumer-side
  `sourceRepo` interface in the console handler to the framework's
  own `inbound.SourceStore`; deleted `internal/inbound/chi_mux.go`
  (a `chi.Router` directly satisfies the `inbound.Mux` single-method
  interface).

- **BREAKING — integral Lark removal** (#66). The Lark Open Platform
  integration ships its **destructive** retirement in v0.3 — there is no
  feature flag, no compat shim, no preserve-the-data path. The deploy
  flow is documented end-to-end in
  [`docs/private-deploy.md`](docs/private-deploy.md) "Upgrading to v0.3".
  - **Code**: `internal/infra/lark/`, `internal/notify/adapter/larkwebhook/`,
    `internal/repo/lark/`, `internal/handlers/console/oauth/`, and every
    `lark*` ingest path under `internal/handlers/` are deleted. The
    `internal/domain.ValidSources` enum drops the four `lark-*` Sprint-1.2
    sources; the canonical set is now `{api, webhook, email, web, other}`.
  - **Database** (migration `015_drop_lark.sql`):
    `DELETE FROM user_feedback WHERE source LIKE 'lark-%'`,
    `DELETE FROM outbox WHERE channel ILIKE 'lark%'`,
    `DELETE FROM tenant_notify_targets WHERE destination_type ILIKE '%lark%'`,
    `DELETE FROM tenant_users WHERE user_id LIKE 'ext_<nil-uuid>:%'`,
    plus `DROP COLUMN tenant_users.lark_open_id / tenants.lark_install /
    tenants.lark_tenant_key`, `DROP TABLE tenant_lark_install / lark_install`.
    The deploy startup path runs `internal/infra/database.ConfirmLarkDelete`
    **before** the migration: if any lark-typed row exists AND
    `migrations.confirm_lark_delete: true` is not set in the private YAML
    config, startup hard-fails with a pointer to `docs/private-deploy.md` —
    silent loss is impossible.
  - **Proto**: `proto/attune/v1/session.proto` adds `Login` /
    `LoginRequest` / `LoginResponse`; the `Tenant` message uses
    `reserved 4; reserved "lark_tenant_key";` so the field number can't
    be silently reused. All Lark-prefixed RPCs / fields are gone from the
    generated Go / TS / OpenAPI.
  - **Console SPA**: every Lark string in `console/src/i18n/zh-CN.json`
    is removed; the login route is `console/src/routes/login.tsx`
    (TanStack file-based router, email + password form). The Lark
    OAuth-callback route is gone.
  - **Outbound notify**: the inline Lark group-bot path is removed —
    notify-target alerts (raw-webhook failure surfacing, etc.) await the
    #34 outbound-adapter SDK for a channel-agnostic alert channel; they
    log-only in v0.3.
- Unused `react-hook-form`, `zod`, `@hookform/resolvers` from
  `console/dependencies` (no references in `console/src/**`).
- Dead `pnpm gen:api` / `src/api/types.ts` references in
  `console/.gitignore` and `console/biome.json` (abandoned
  openapi-typescript plan; #19 made the contract proto-driven).

### Webhook consumer migration (raw-webhook & outbox v1 → v2)

The `enriched` block in the outbox / raw-webhook envelope changes shape
this release. The top-level `version` field goes from `"1"` to `"2"` so
consumers can branch decoding by version.

**Before (v1):**
```json
{
  "version": "1",
  "event_type": "feedback.enriched",
  "feedback": {
    "id": 42,
    "content": "...",
    "enriched": {
      "title": "Payment failed",
      "kind": "bug",
      "severity": "P0",
      "modules": ["payment", "checkout"],
      "priority": 1.0,
      "rationale": "...",
      "enriched_at": "2026-06-07T..."
    }
  }
}
```

**After (v2):**
```json
{
  "version": "2",
  "event_type": "feedback.enriched",
  "feedback": {
    "id": 42,
    "content": "...",
    "enriched": {
      "title": "Payment failed",
      "attrs": {
        "type": "bug",
        "severity": "critical",
        "labels": ["payment", "checkout"]
      },
      "is_urgent": true,
      "rationale": "...",
      "enriched_at": "2026-06-07T..."
    }
  }
}
```

Concrete consumer changes:

- `enriched.kind` → `enriched.attrs.type` (Value is now operator-owned; the
  default seed keeps `bug`/`feature`/`question`/`other`)
- `enriched.severity` (was `P0`..`P3`) → `enriched.attrs.severity` (Value is
  now operator-owned; the default seed is `critical`/`major`/`minor`)
- `enriched.modules` (array) → `enriched.attrs.labels` (array; per-tenant
  taxonomy, freeform by default)
- `enriched.priority` (float) → REMOVED; use the derived `enriched.is_urgent`
  boolean for routing (`severity.urgent_set=["critical"]` is the default
  seed, so any `severity == "critical"` row carries `is_urgent: true`)
- Any consumer that walked `enriched` by hand must switch to
  `enriched.attrs[<dim.name>]`. Operators may add or remove dims from the
  console Settings page at runtime, so consumers should be tolerant of
  unknown keys in `attrs` and missing keys (a dim was deleted or the LLM
  declined an optional dim).

The example values above are the DEFAULT OSS seed. Once operators edit
their dim set via Settings, attribute Values become whatever the operator
authored — wire-stable, never auto-renamed.

### Added

- **`scripts/lint-rawptr.sh` (and `cmd/lint-rawptr/`) — bans bare `*p`
  deref and `&x` address-of, redirecting authors to `internal/pkg/ptrext`
  helpers.** Adds a new CI gate (`lint-rawptr`) and a pre-commit hook step.
  The AST-aware linter correctly skips `*T` in type position, `*p = v`
  on the LHS of an assignment, `&xs[i]` slice-element addressing, and
  `&arg` passed to known out-parameter APIs (`json.Unmarshal`, `*Row.Scan`,
  `flag.*Var`, `errors.As`, `encoding/binary.Read`, attune's `postJSON`).
  Per-line `// ptrext:allow <reason>` and per-file `// ptrext:file-allow
  <reason>` escape hatches cover identity-bearing values (sync.Mutex
  in proto messages, strings.Builder accumulators, out-parameter capture
  fixtures) where wrapping would break correctness. The in-tree sweep
  rewrote 261 sites across 92 files. See CLAUDE.md §7b for the policy.
  (A one-shot `-fix` AST rewriter assisted the initial sweep; it lived
  briefly in commit 44d4545 and was removed once the tree was clean —
  cherry-pick it from there if a future mass migration needs it.)

- **`internal/pkg/` umbrella for stdlib-extension packages.** `logext` moves
  here (was `internal/logext`); new sibling `ptrext` ships small generic
  pointer helpers (`Of`, `Indirect`, `IndirectOr`, `OfNotZero`, `OfPositive`,
  `IsNil`/`IsNotNil`/`IsNilOrZero`, `HasZeroValue`/`HasNonZeroValue`, `Equal`,
  `EqualTo`, `Clone`/`CloneBy`, `Map`). Zero external deps — pure stdlib +
  `cmp.Ordered`. `pkg/` is reserved for stateless language/stdlib extensions;
  cross-cutting infrastructure (DB, observability, HTTP middleware) stays
  under `infra/`. The `internal/logext` → `internal/pkg/logext` move is a
  pure import-path rename — no behaviour change.

- **`FEEDBACK_API_LLM_MODEL` env override / `llm_model` YAML key** for the
  enrichment model id. Default remains `gpt-4o-mini`; set this when your
  LLM gateway aliases the model name (e.g. corporate gateway exposes
  `gpt-5.5` for an OpenAI-compat endpoint). The previous hardcoded
  constant forced operators to fork the binary; this change keeps every
  knob in one config layer.

### Changed

- **BREAKING — enrichment classification pivots to metadata-driven `Dimension`s
  with first-class i18n** (#10, supersedes the original kind/severity/modules
  axes and the intermediate flat-labels iteration). Modelled on the
  industry-converged Custom Fields / Tags / Properties layer (Jira / Linear
  2024 / Sentry / Datadog / GitHub Projects v2 — see proposal §Top-repo
  benchmarking). Each `Dimension` has a stable `Name`
  (`^[a-z][a-z0-9_]{0,30}$`, immutable), an i18n `DisplayName`, a `Kind ∈
  {single, multi}`, a `Taxonomy` of `{Value, DisplayName: I18nString}`
  entries, an `UrgentSet` of stable values that derive `is_urgent`, and a
  `Required` flag. Adding a new axis is now a Settings edit, not a code
  change.
  - **Wire shape** (envelope `version` bumped `"1" → "2"`): `enriched.attrs`
    is a `map<dim.name, string | [string]>` replacing the old `kind` /
    `severity` / `modules` / `priority` keys; `enriched.is_urgent` replaces
    P0/P1 routing. Raw-webhook / outbox / lark / github-issue all emit the
    new shape — customer consumers must update their decoders.
  - **Schema**: migration `014_enrich_dimensions.sql` drops the old columns
    (`enriched_kind` / `enriched_severity` / `enriched_modules` /
    `enriched_priority` / `tenants.enrich_modules`) and introduces
    `tenants.enrich_dimensions JSONB` (dim metadata) + `user_feedback.
    enriched_attrs JSONB` (LLM-emitted values) + `is_urgent BOOLEAN`
    (snapshot at write time). GIN `jsonb_path_ops` index on
    `enriched_attrs` powers `?type=bug&severity=critical&labels=payment`
    containment filters. The migration seeds three default dimensions
    (`type` / `severity` / `labels`) with `default` / `zh` / `en` display
    names; `severity.urgent_set = ["critical"]` so radar routing is
    non-empty out of the box. Pre-1.0 DROP — no 015 cleanup tail.
  - **i18n** is first-class on every operator-authored display:
    `Dimension.DisplayName` and every `Taxonomy.DisplayName` are
    `map<locale, string>` with BCP 47 keys + a `"default"` fallback. The
    SPA resolves at render time (`useDisplayName` walks user locale →
    `"default"` → first non-empty), so a bilingual team can configure
    both languages once without parallel pipelines. The LLM prompt prints
    `Value (zh | en)` hints to disambiguate Chinese content against
    English Values; the LLM always emits stable Values, and `attrs` JSONB
    stores only those.
  - **Identifier discipline**: `Dimension.Name` and `Taxonomy.Value` are
    immutable after creation. Renaming = delete + recreate (operator owns
    the migration of any consumers / dashboards). `DisplayName` is freely
    editable; UI-only.
  - **Domain / service**: `domain.Enriched` and `Snapshot` carry
    `Attrs map[string]any` (replaces `Labels []string`); prompt rendering,
    JSON schema generation, post-parse whitelist filter (`FilterAttrs` per
    dim), and urgency derivation (`ComputeIsUrgent` ORs every dim's
    `UrgentSet`) all driven by the tenant's `DimensionSet`. No hard-coded
    per-axis code in the enricher.
  - **Console**: `Settings` becomes a metadata editor — add / remove dims,
    edit i18n `DisplayName`, edit `Taxonomy` (with per-entry i18n), edit
    `UrgentSet`, toggle `Required`. `Feedback` list / detail render
    `<DimensionChips>` for every configured dim, with stable `Value` going
    through containment filters and resolved `DisplayName` rendered in
    the user's locale. Per-dim "top values" replaces the old single-axis
    kind donut.
  - **Metrics**: `attune_enrich_attrs_dropped_total{tenant, dim}` and
    `attune_enrich_suggested_attrs_total{tenant, dim}` replace the
    `labels_dropped` / `suggested_labels` counters;
    `attune_enrich_duration_seconds` carries `dims_mode`
    (`freeform` / `constrained`) instead of `module_mode`.
  - **`attune eval`**: per-dim `AttrAccuracy` (single) + `AttrSumIoU`
    (multi); CSV header is data-driven from the tenant's dim set
    (`--tenant <id>` flag required for `export-for-human` /
    `score-human`).
  - Proposal: `docs/proposals/2026/06/2026-06-07-flat-labels.md`.

### Added

- **Per-tenant enricher prompt + module whitelist** (#10) — tenants can
  override the classification prompt (`{{content}}` / `{{modules}}` token
  substitution, SSTI-safe) and declare a modules vocabulary. Gate (2)
  post-filter guarantees stored `modules` ⊆ configured list; off-list
  labels surface as a suggested-module signal (metric + log). Gate (3)
  structured output is wired across four LLM protocols (`openai-compat`,
  `openai-responses`, `anthropic`, `gemini`). Console adds
  `/settings` (GET/PUT `/enrich-config`, POST `/enrich-config/preview`).
  Migration `012_enricher_per_tenant_prompt.sql`; proposal
  `docs/proposals/2026-06-06-enricher-per-tenant-prompt.md`.
- **AI rationale surfaced on the console feedback detail sheet** (#10
  follow-up) — the LLM's short "why this kind/severity" justification
  was already on the wire to webhook consumers via the outbox envelope
  (`Snapshot.Rationale`), but `MarkDone` silently dropped it on the SQL
  write path so console reviewers never got to audit the AI's
  reasoning. Five-layer fix: migration `013_enriched_rationale.sql`
  adds the column; `markDoneSQL` persists it; `GetForConsole` selects
  it into `ConsoleDetailRow.EnrichedRationale`; proto adds
  `optional string enriched_rationale = 18` to `FeedbackDetail`; the
  detail sheet renders it under a new "AI 解读" section. Webhook
  envelope shape unchanged. Surfaced during the #10 browser smoke
  walk-through.
- **CI architectural-boundary gate for the console SPA** (#19) — runs
  `dependency-cruiser` on every console PR with four rules: no cross-feature
  imports, shared layers (components/lib/proto) may not reach into features/
  routes/app, features may not reach into routes/app, no circular deps.
  Config in `console/.dependency-cruiser.cjs`; `pnpm arch` runs it locally
  (requires Node ≥20.12 || ≥22 || ≥24).
- **Observability overlay** (`deploy/docker-compose.obs.yml`) — optional
  Prometheus + Grafana stack (pinned images, memory-capped) layered with
  `-f docker-compose.yml -f docker-compose.obs.yml` (#6). Auto-provisions the
  Prometheus datasource and the "Attune Overview" dashboard, and documents the
  `attune_*` metrics as a backend-agnostic contract in `observability/README.md`.
- CI: a `deploy/**`-filtered `docker compose config` smoke check.
- **Protobuf IDL contract** (#19) — `.proto` in `proto/attune/v1/` is now the
  single source of truth for the HTTP contract, generating Go (`internal/proto/`),
  TypeScript (`console/src/proto/`, via ts-proto) and OpenAPI (`docs/openapi/`).
  `make proto` regenerates all three; a CI `proto-sync` gate fails on drift.
  Every HTTP endpoint is decoded/encoded via `protojson` against the generated
  types: the public `POST /v1/feedback/ingest` plus the full console API
  (session, API keys, notify-targets, feedback, usage).
- **Unified error envelope** (#19) — every HTTP error now shares one shape,
  `{"code","message","requestId"}` (`ErrorResponse` in
  `proto/attune/v1/common.proto`), where `requestId` echoes the request's chi
  correlation id for support triage. The shared writer lives at
  `internal/respond.Error` so handler subpackages and infra-layer
  middlewares emit the same envelope; `internal/handlers/console/internal/respond`
  re-exports it so existing console handlers don't change.

### Fixed

- **Empty list / scalar fields now emit on console proto responses**
  (post-#19) — `internal/respond.Proto` was using protojson's default
  marshal options, which OMIT zero-valued fields. The wire shape for
  `ListFooResponse{ Items: nil }` was therefore `{}` (no `items` key),
  and every SPA query that ran `return resp.items` got `undefined`,
  tripping react-query's "data cannot be undefined" guard. The
  symptom: every console tab except `/settings` and `/login` crashed
  on a fresh tenant (the four list / stats pages — feedback,
  notify-targets, api-keys, usage). Fix: opt the canonical marshaler
  into `EmitUnpopulated: true`. Empty repeated / message / scalar
  fields now emit their zero value (`"items":[]`, `"count":"0"`,
  `"series":[]`); proto3 explicit `optional` fields are not affected
  (their underlying oneof is intentionally exempt). Surfaced during
  the #10 browser smoke walk-through.
- **Outbound `submitted_at` reflects actual ingest time** (#82) — both the
  outbox webhook envelope (`internal/service/enrich/enricher_outbox.go`)
  and the inline raw-webhook envelope
  (`internal/notify/adapter/rawwebhook/raw_webhook.go`) previously emitted
  `submitted_at = EnrichedAt` (LLM completion time), offset from real
  submission by enrichment latency (typically seconds to minutes). Now
  emits `user_feedback.created_at` plumbed through `EnrichInput.CreatedAt`
  → `Snapshot.SubmittedAt`. Consumers doing time-series ordering or SLA
  calculation see the real timeline.
  - **Action for raw-webhook consumers**: JSON shape and field names are
    unchanged, but the `submitted_at` *value* now differs from
    `enriched_at` by enrichment latency. Consumers that previously
    treated the two as interchangeable (e.g. used `submitted_at` to
    derive a "time-to-classification" duration) should switch to
    `enriched_at - submitted_at` for that metric — they are now
    correctly distinguishable for the first time.
- **Triage no longer discards 2-rune CJK feedback** (#85 R7) — `runeCount
  < 3` previously dropped "崩了" / "闪退" / "卡死" (among the most common
  Chinese severe-bug shapes) before reaching the LLM. Threshold lowered
  to `< 2`; 2-rune ASCII ("ok" / "no") also passes and is correctly
  classified as low-signal by the LLM at negligible cost. Covered by
  `TestTriage_TwoRuneCJKFeedbackPassesThrough`.
- **Claim stale-threshold unified to 5 minutes** (#85 R8) — `TryClaim`
  (`internal/repo/feedback/feedback.go`) previously refused to steal
  stuck `enriching` rows until 15 minutes, while `ListPending` listed
  them as stale at 5 minutes. Result: a stuck row produced spurious
  `attune_claim_contention_total` increments every 30s tick for 9
  minutes until the 15-minute window opened. Both operations now use
  5 minutes, matching the documented invariant and the LLM 60s timeout
  envelope.
- **apikey middleware no longer leaks the legacy `{"error":"..."}` shape**
  (#19) — caught by docker-compose smoke tests: `POST /v1/feedback/ingest`
  without (or with an invalid) `X-API-Key` previously returned the old
  one-key envelope — the only customer-facing endpoint that did. Now
  emits `{code,message,requestId}` like every other path, with
  `code=unauthenticated` on 401s and `code=internal` on lookup failures.
  Covered by new `internal/infra/apikey/middleware_test.go` (4 cases:
  missing header, invalid prefix, lookup-failure 500, happy-path
  forwarding).

### Changed

- **Live LLM tests segregated under `test/live/llmclient/` with a
  `//go:build live` tag** (#10) — the four-backend e2e suite previously
  lived in `internal/infra/llmclient/e2e_test.go` next to the mock
  unit tests, gated only by `t.Skipf` on missing env vars. It now sits
  in its own package + directory + build tag (three layers of
  isolation, matching `sashabaranov/go-openai` and AWS SDK Go v2),
  so `go test ./...` cannot accidentally enter it. New `make` targets:
  `make test` (unit, default) and `make test-live` (opt-in). Operational
  surface (env-var matrix, recipes, cost guardrails) documented in
  `docs/testing.md`. Tests renamed `TestE2E_*` → `TestLive_*` so
  `make test-live` can filter `-run '^TestLive_'`. No CI workflow yet
  — adding `workflow_dispatch`-triggered `live-tests.yml` waits for a
  sandbox key.
- **Backend reorganized into hybrid layer-outside / feature-inside packages**
  (#19) — `internal/{service,repo,notify}` no longer flat. Each layer keeps
  its name + the four CLAUDE.md §5 rules (re-verified clean by grep after
  the move) and adds feature subpackages inside:
  - `internal/service/{enrich,ingest,outbox,apikey,eval}/`
  - `internal/repo/{feedback,apikey,outbox,notifytarget,tenant,lark}/`
  - `internal/notify/adapter/{rawwebhook,larkwebhook,githubissue}/`
    (Transport framework stays in the root `internal/notify` package).
  Importers needing both `service/apikey` and `repo/apikey` alias the repo
  side as `apikeyrepo`; same for `outboxrepo` and `larkrepo`.
- **Console SPA migrated to feature-based layout** (#19) — `src/api/` retired;
  every console resource now lives under `src/features/<x>/{api,components}/`
  per bulletproof-react conventions, with React Query co-located per feature
  (queryOptions + hook one file per operation). `src/components/` keeps only
  truly shared primitives (`ui/`, `brand/`, layout shells).
- **`internal/observability` → `internal/infra/observability`** (#19) — naming
  consistency with sibling infra packages (`infra/trace`, `infra/metrics`).
  Bootstrap-only package; only `cmd/attune` importers updated.
- **Console API responses are now lowerCamelCase** (#19, breaking) — protoJSON
  renders fields in lowerCamelCase, so console endpoints under `/fb/v1/console/*`
  now return `userId`, `createdAt`, `enrichedTitle`, … instead of the previous
  snake_case (`user_id`, `created_at`, …). Request bodies still accept both
  casings. The bundled console SPA is updated in lockstep; any out-of-tree
  console API client must follow. (Pre-1.0 breaking change, flagged per §3.)
- **64-bit integer fields are now JSON strings** (#19) — protoJSON serializes
  `int64`/`uint64` as strings (`{"id":"123"}`), which is also safe for JavaScript
  clients. Affects the ingest response `id`, the console feedback `id`, usage
  totals/buckets, and feedback-stats counts.
- Renamed the bundled Grafana dashboard "Attune Overview (Wave 1.2)" → "Attune
  Overview" and removed internal roadmap jargon from `observability/` and the
  `metrics` package doc (no metric names changed).

### Removed

- **`openapi-typescript` and the hand-written `openapi.yaml`** (#19) — console
  TypeScript types are now generated from `.proto` via ts-proto, retiring the
  hand-maintained `internal/handlers/console/openapi.yaml` and the
  `openapi-typescript` dev dependency. The `gen:api` npm script is replaced by
  `gen:proto` (→ `make proto`).

### Fixed

- **Feedback detail labels regress to raw keys** (#19) — `zh-CN.json` still
  held snake_case keys (`source_meta`, `enrichment_error`, `enriched_at`)
  after the protoJSON lowerCamelCase rename; the detail panel rendered the
  literal key strings instead of the Chinese labels. Keys renamed to match.
- **Unified error envelope leak** (#19) — `console.writeError` (auth/oauth/
  dev_login paths, used by RequireSession middleware) still emitted
  `{code,message}` without `requestId`, contradicting the CHANGELOG's
  "every HTTP error shares one shape" claim. Routed through `respondError`
  so the chi RequestID is included on every 401/403/4xx from these paths.
- **`NotifyTarget.CreatedAt` was synthesized** (#19) — the response field
  was set to `time.Now()` on every read with a TODO comment; every notify
  target in the console UI displayed "just created" regardless of true DB
  creation time. Added `CreatedAt` to the repo model, surfaced the
  `tenant_notify_targets.created_at` column in all SELECT/RETURNING paths.
- **`decodeProto` silently truncated oversized bodies** (#19) — bodies > 1 MiB
  were chopped to exactly 1 MiB and surfaced as vague "invalid json" 400s
  instead of a clear 413. Now returns `errBodyTooLarge` so handlers map it
  to `HTTP 413 body_too_large`.

### Changed

- **`scripts/check.sh` jscpd threshold 2% → 4%** (#19) — the intentional
  helper duplicates from the package split (cycle-prevention copies of
  `truncate`, `signRawBody`, `signLarkBot`, `isUniqueViolation` across
  `repo/{outbox,notifytarget}/helpers.go` + `notify/adapter/*/`) push the
  Go duplication ratio from 1.9% to ~3%. CLAUDE.md §1 raised to match.

### Security

- **Encryption at rest for inbound secrets** (#66 / #23). All customer
  webhook HMAC secrets and IMAP credentials are sealed with the shared Tink
  AEAD keyset in `secrets.tink_keyset`. Plaintext never enters the database,
  the OpenAPI surface, or the console UI; on reveal the secret is shown
  **once**, post-creation RPCs return only an opaque last-4 hint. Key rotation
  is now handled by the same `attune secrets reencrypt` / `retire-key` path as
  managed LLM credentials.
- **Webhook replay + enumeration resistance** (#66). The webhook
  adapter rejects requests outside a ±300 s timestamp window
  (Stripe-style) and computes a stub HMAC on unknown source slugs so
  that the 401-handling path is the same wall-clock for known vs.
  unknown sources — operators can't enumerate slugs by timing.
- **Console auth: bcrypt cost 12 + dummy-bcrypt equalization** (#66).
  `VerifyOrDummy` runs a constant-time bcrypt against a
  package-private stub hash when the email is unknown, so the 401
  path is observationally identical to the wrong-password path. Cookie
  attributes (`HttpOnly + Secure + SameSite=Lax + Path=/`) match the
  guidance in CLAUDE.md §8.
- **Bootstrap admin TOCTOU guard** (#66). `BootstrapAdmin` runs inside
  `pg_advisory_xact_lock` + `ON CONFLICT (email) DO NOTHING`; two
  attune replicas racing on the same first start cannot create two
  rows.
- Bounded the `source` label on `attune_ingest_total`: a rejected (invalid)
  client-supplied `source` is now recorded as `invalid` instead of the raw value,
  closing an unbounded metric-cardinality vector on the ingest validation-error
  path.

## [0.2.0] - 2026-06-05

### Added

- **Private-deploy docker-compose kit** under `deploy/` (#5): `docker-compose.yml`
  (attune + postgres), a documented `.env.example`, an optional `config.yaml`
  template, and a quickstart `README.md`. `cd deploy && cp .env.example .env &&
  docker compose up -d` brings up a hardened (loopback-bound, `no-new-privileges`,
  read-only attune rootfs), persistent stack; first-tenant bootstrap via
  `docker compose run --rm attune tenant create / keys issue`.
- **`/healthz` liveness endpoint** (#5) — the Kubernetes/cloud-native convention
  (trailing `z` avoids colliding with a real application route).

### Changed

- **BREAKING — project-wide rename `listen` → `attune`** (#8). The Go module
  path is now `github.com/Phixsura/attune`; the binary/command is `attune`
  (was `listen`); Prometheus metrics use the `attune_*` prefix (dashboard +
  scrape job relabelled); the outbound webhook signature header is
  `X-Attune-Signature`, GitHub-dispatch labels are `attune/*`, and the
  `User-Agent` is `attune/<n>`; console session cookies are `attune_session` /
  `attune_oauth_state`. Pre-1.0, so this lands as a single breaking change —
  update any scrapers, dashboards, webhook verifiers, or label filters
  accordingly. The `FEEDBACK_API_*` env prefix is intentionally unchanged.

### Removed

- **BREAKING — the `/health` endpoint is removed** (#5); use `/healthz` instead.
  Pre-1.0, so it lands as a flagged minor bump (CLAUDE.md §3). Update any uptime
  monitor, load-balancer, or container probe that hit `/health`.

### Security

- Bump dependencies carrying published advisories: `github.com/jackc/pgx/v5`
  5.9.1→5.9.2 (GHSA-j88v-2chj-qfwx), `golang.org/x/net`→0.55.0 and
  `golang.org/x/sys`→0.45.0 (GO-2026-4918 / 5024–5030), and `vitest`→4.1
  (GHSA-5xrq-8626-4rwp). `govulncheck` confirms none of the Go advisories were
  reachable from attune's code, and `vitest` is a test-only dependency with no
  tests and no UI server, so there was no exploitable exposure — bumped for
  hygiene and to clear the alerts.

## [0.1.0] - 2026-06-04

### Added

- Initial public release (Apache-2.0).
- Go 1.25 HTTP server (chi router + structured slog + OpenTelemetry).
- PostgreSQL storage with auto-applied migrations.
- LLM enrichment via any OpenAI-compatible `/v1/chat/completions` endpoint
  (OpenAI / Azure OpenAI / vllm / ollama / oneapi).
- Outbound delivery — Lark group bot webhooks (inline, best-effort) and
  customer HTTPS webhooks (via outbox, at-least-once).
- Stage B web console (React + Vite + biome) for tenant / API key /
  notify-target / feedback CRUD.
- Prometheus `/metrics` endpoint and a base Grafana dashboard JSON shipped
  under `observability/dashboards/`.
- Configurable per-tenant token-bucket rate limiting.
- Lark event subscription handler with signature verification.

[Unreleased]: https://github.com/Phixsura/attune/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/Phixsura/attune/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/Phixsura/attune/releases/tag/v0.1.0
