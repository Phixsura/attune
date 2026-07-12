# Changelog

All notable changes to attune are documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Portal submissions now can jump straight to Customer Requests.**
  Added a direct promotion action from portal submission details into the
  Customer Requests flow so operators can move from intake to follow-up without
  returning to the queue.

### Fixed

- **Public portal submission form now sends protobuf enum values.**
  The portal page now submits the submission kind using the proto enum names
  expected by the JSON API, and the audit log allow-list now accepts the
  resulting `portal_submission.create` event so real browser submissions can
  commit successfully.

- **Portal inbox empty state now guides operators to the live portal.**
  When the dedicated portal queue has no submissions yet, the Console now
  shows a portal-specific empty state with direct links to the live portal
  preview and public visibility settings instead of a generic filtered empty
  screen.

- **Portal inbox view for public submissions.**
  Added a dedicated `/feedback/portal` Console queue with a portal-scoped
  default filter, a sidebar entry, and page chrome that makes public portal
  submissions easier to triage without manual filtering.

- **Public portal preview now opens the live submission page.**
  Added a direct link from the public visibility portal preview to the tenant's
  live `/portal/{tenant_slug}` page so operators can validate the current
  submission form without hunting for the URL.

- **Portal submissions now show structured evidence in feedback details.**
  Added a dedicated portal submission evidence card in the feedback detail
  sheet so operators can review submission kind, title, page URL, identity
  fields, custom fields, and user-agent context without opening the raw JSON
  meta block first.

- **Portal feedback rows now use a clearer source label.**
  Displayed portal-origin feedback rows with a dedicated "portal submission"
  label in the console list so the inbox queue is easier to scan at a glance.

- **Public end-user feedback submission portal.**
  Added a tenant-branded submit-only portal at `/portal/{tenant_slug}` with
  public submission-config and submission endpoints, configurable portal form
  fields in the public visibility policy, raw feedback persistence with a
  dedicated `portal` source and submission kind, moderation-subject creation,
  and Console portal-form editing plus live preview support.

- **Public visibility policy and moderation foundation.**
  Added tenant-scoped public visibility policy storage, moderation subject
  state transitions, audit events, generated Console and public portal
  contracts, a public-safe Customer Request read projection under `/v1/portal`,
  anonymous portal rate limiting, Console request-profile publication APIs, and
  a Console policy/moderation page for reviewing public-facing request
  visibility through the shared contract. The Console policy page also exposes
  vote write mode, comment default state, submitter identity, and count/display
  controls, while public request projections enforce anonymous identity mode
  before emitting submitter display names. Console public-visibility handlers
  also reject unknown numeric enum values and unknown query-string enum filters
  before they can be silently coerced into default policy or moderation filters.
  Database constraints now also reject rejected, hidden, or spam policy default
  states, so direct writes cannot bypass the runtime default-state contract.
  Public portal APIs now include tenant-scoped request-list and roadmap-list
  endpoints that share the same policy, moderation, live-record, field
  allowlist, no-store cache, and noindex controls as public request details.
  Public moderation subjects now enforce bounded non-empty subject identifiers
  and are covered across request-comment and portal-submission surfaces, so the
  shared queue contract is not request-only.
  Moderation actions now carry stable
  reason codes and optional bounded notes, the Console queue can switch between
  pending, published, blocked, and all review states, and public request reads
  emit a no-store cache policy on success, not-found, invalid-query, and
  rate-limited responses so moderation hides and restores take effect in
  browsers.
  Console request-profile details now update their displayed moderation state
  immediately after approve, hide, spam, reject, or restore actions.

- **External sync framework foundation.**
  Added tenant-scoped external connection, object mapping, object link, cursor,
  sync-run, attempt, record-failure, and conflict tables; generated Console API
  contracts; encrypted connection credential storage; provider registry and
  no-op adapter; GitHub Issues repository checks, paginated issue pulls, issue
  create/update pushes, Customer Request push-record preparation, provider
  write-result application, and Customer Request marker bridging; idempotent
  pull and push ledger application with cursor advancement, Customer Request
  issue-link bridging, partial-run conflict/failure handling, external sync
  tombstone revival handling, provider-classified retryable and terminal run
  failures, provider event ledger and replay APIs for signed webhook delivery
  diagnostics, Console event-detail inspection for dedupe keys and normalized
  delivery payloads, encrypted webhook secret storage, a GitHub
  `X-Hub-Signature-256` webhook receiver, worker lifecycle, audit actions,
  provider schema discovery APIs, Console provider-schema visibility,
  required/writable field metadata, schema-aware mapping JSON validation,
  mapping preview diagnostics, Console operations page with connection
  editing, credential and webhook secret rotation controls,
  mapping-direction-aware run requests, operator-safe cursor reset that clears
  mapping cursors and enqueues recovery pull runs, explicit backfill requests
  with optional cursor reset, record timelines for object-link, failure,
  conflict, and run ledger inspection,
  health-summary breakdowns for throttled, unauthorized, provider-unavailable,
  delayed-retry sync runs, repeatedly failing connections, and quarantined
  connections, provider `Retry-After`-aware run scheduling, automatic
  connection quarantine after repeated terminal failures, explicit quarantined
  connection resume actions, connection qualification reports that run provider
  checks, schema discovery, schema metadata inspection, and scope visibility
  checks, batch conflict resolution, emitted record/conflict counters plus lag
  and dead-run gauges for external sync dashboards,
  per-connection paginated run history, run-detail diagnostics for provider
  request ids, HTTP status, `Retry-After` hints, payload digests, normalized payloads,
  conflict snapshots, and explicit conflict resolution choices, webhook secret
  configuration state,
  filterable run-list and event-list APIs, record-failure retries that enqueue
  retry runs, adapter authoring documentation, and low-cardinality Prometheus
  metrics for provider operations.

- **Customer Requests now turn feedback evidence into product request objects.**
  Added tenant-scoped Customer Requests with stable display IDs, status,
  priority, owners, linked feedback evidence, explicit customer/account links,
  internal demand votes, duplicate-request tracking, delivery issue references,
  merge-safe backlinks, idempotent create/promote/merge operations, audit
  events, generated proto/OpenAPI contracts, and Console list/detail plus
  feedback-selection promotion, feedback-detail link-management, owner filtering
  and assignment, merge-target detail handoff, and feedback-scoped deep-link
  workflows.

- **Customer Requests now include account value and delivery sync decision signals.**
  Added request-level revenue impact, deterministic decision scores, account
  profiles on customer links and votes, revenue/score sorting, issue-link sync
  metadata, generated contract fields, audit coverage, and Console controls for
  inspecting account context and recording delivery issue sync state.

- **Customer Requests now roll linked issue sync state into delivery health.**
  Added request-level delivery health, per-state linked issue sync counts,
  delivery-health sorting, generated contract fields, Console row badges and
  detail metrics, and PostgreSQL coverage for failed, stale, pending, manual,
  synced, and no-link rollups.

- **Customer Requests now support internal collaboration notes.**
  Added tenant-scoped request notes with generated API contracts, Console add
  and delete controls, audit events that record note identity and length without
  duplicating note bodies, request touch semantics, merge-context preservation,
  and PostgreSQL coverage for note lifecycle and audit behavior.

- **Customer Requests now support tenant-configurable decision scoring.**
  Added scoring settings APIs, persisted tenant weights and caps, audited
  updates, generated proto/OpenAPI contracts, Console controls for priority,
  evidence, customer, account, vote, and revenue signals, and PostgreSQL
  coverage that verifies custom settings reorder decision-score results.

- **Customer Requests now support saved prioritization views.**
  Added per-user saved views for Customer Request list filters and sorting, with
  generated API contracts, tenant-scoped persistence, Console save/apply/delete
  controls, and tests for the saved-view workflow.

- **Private-deploy smoke suite now covers base, observability, TLS, failure, and upgrade modules.**
  Added dedicated smoke targets for the compose base stack, the observability
  overlay, the TLS reverse-proxy front door, deterministic startup failures,
  and seeded upgrade paths. The umbrella `make private-deploy-smoke` target now
  runs the full suite, the CI workflow now exercises the same entrypoint for
  deploy-related changes, failure artifacts are uploaded from CI, and the
  private-deploy / testing docs and proposals were updated to match the shipped
  contract. The observability module also persists Prometheus query payloads
  plus Grafana datasource and dashboard snapshots into the artifact bundle so
  failed CI runs leave inspectable overlay evidence.

- **Published release images now get the same private-deploy smoke coverage.**
  Added a `make private-deploy-smoke-published` entrypoint and a `published`
  smoke mode that reuses the full suite against a published release image,
  defaulting to `ghcr.io/phixsura/attune:latest` unless overridden. The release
  workflow now runs that published-image smoke after pushing a tag, so the
  shipped image is verified before the release finishes.

- **GDPR delete request history now includes outbox purge counts.**
  The request history and delete response now persist and surface
  `notify_outbox` purge counts alongside the existing feedback, tag, and audit
  totals, so operators can see the full deletion footprint in the Console.

- **Delegated admin role for operational governance.**
  Added a new `delegated_admin` tenant role, plus RBAC and Console permission
  support for operational settings, audit-log review, and GDPR overview flows.
  The members page can now assign the role directly, and the runtime checks and
  DB constraints accept it across both tenant membership and OIDC role sync.

- **Break-glass emergency controls in the Security page.**
  Added Security-page actions for revoking all active break-glass tokens,
  reviewing locked IP addresses, and manually clearing lockouts. The page now
  keeps the emergency access surface in one place instead of splitting token
  issuance from lockout recovery.

- **Service account governance card, create flow, and runtime toggle enforcement.**
  Added a Console API Keys panel that lists tenant service accounts and lets
  admins create new service accounts with operator-facing naming guidance. The
  same surface now lets admins enable or disable or delete service accounts in
  place, and linked API keys fail authentication immediately while their
  service account is inactive. Deleted service accounts are detached from
  linked API keys, and service account create, status-toggle, and delete
  actions now also emit audit events.

### Fixed

- **Public portal submissions now honor idempotency keys.**
  The public submission path now deduplicates browser retries with the
  standard `Idempotency-Key` contract, skips duplicate moderation-subject
  creation on replay, and rejects malformed keys before persistence.

- **External sync browser E2E findings are closed.**
  External connection qualification and resume actions now satisfy the database
  audit action constraint, active external sync runs refresh in the Console
  until queued/running work settles, and canceled external sync health requests
  propagate through the shared client-canceled path instead of being logged as
  internal failures. External sync action buttons also re-enable after
  successful mutations instead of staying disabled on the last mutation target.

- **External sync Console layout and select accessibility now pass the browser gate.**
  The external sync operations page now keeps its two-column workspace,
  connection actions, and mapping controls inside narrow and text-zoomed
  viewports, and its select triggers expose explicit labels for assistive
  technology.

- **Feedback tag combobox now suppresses duplicate-looking create actions for already assigned tags.**
  The feedback detail sheet now checks the full tag catalog before showing the
  inline create CTA, so searching a tag that already exists on the feedback no
  longer suggests a duplicate-looking create path. The selectable list still
  hides assigned tags, batch tag pickers keep their existing behavior, the
  combobox now shows a clearer "already added" empty state for exact matches,
  and it ignores empty-result arrow navigation instead of drifting into a
  broken active state.

- **Reliability overview now survives a single snapshot endpoint failure.**
  The `/administration/reliability` route now preloads its snapshot queries on
  a best-effort basis, so one 503 no longer kicks operators to the global error
  boundary. The page can still render the healthy cards and surface the failing
  card inline, which keeps the reliability dashboard usable during partial
  backend outages.

- **System readiness now renders failing preflight reports instead of hiding them behind a fetch error.**
  The `/administration/system-readiness` page now accepts the preflight
  endpoint's 503 report payload as data, so operators still see the failing
  checks, remediation text, and summary counts. Reliability now reuses the same
  interpretation, which keeps the readiness card visible during partial backend
  failures.

- **Console OIDC RBAC now resolves the session and membership member-type alias.**
  OIDC sessions store `UserType="oidc"` in the cookie while tenant membership
  rows use `member_type=oidc_user`. RBAC now normalizes that alias before
  role lookup and cache invalidation, so Dex SSO users can actually land on
  the Control Tower instead of tripping the `not a tenant member` error
  boundary.

- **Console accessibility mocks now cover service-account reads on API Keys.**
  The Playwright accessibility route harness now responds to the
  `/service-accounts` list request that the API Keys page issues, so the
  browser gate no longer treats that legitimate query as an unhandled request.

- **Console accessibility mocks now cover saved audit-log views.**
  The Playwright accessibility route harness now responds to the audit-log
  saved-view list and mutation endpoints that the Audit Log page loads, so the
  browser gate no longer treats that saved-view traffic as an unhandled
  request.

- **GDPR request summary keeps existing field numbers.**
  `outbox_count` now lands at the end of `GdprRequestSummary`, so the new
  summary field stays wire-compatible with the existing proto schema and
  `buf breaking` passes again.

- **Console workspace install now recognizes the local package.**
  `console/pnpm-workspace.yaml` now declares the Console package explicitly, so
  `pnpm install --frozen-lockfile` in CI can resolve the workspace instead of
  failing with an empty packages configuration.

- **Sample deploy config no longer carries committed dev credentials.**
  The checked-in `deploy/config.yaml` now keeps PostgreSQL, Console, and Tink
  values as placeholders again, so the private-deploy template no longer ships
  with generated credentials in the repository.

- **Service-account audit writes now match the database constraint.**
  Console service-account create, update, and delete actions now persist audit
  rows successfully again. The audit action allow-list and the PostgreSQL
  `chk_audit_action_value` constraint are back in sync, so the API Keys service
  account flow no longer fails with `failed to write audit log`.

- **Local demo bootstrap now seeds embedded feedback rows reliably.**
  `attune demo bootstrap` now writes demo feedback with explicit SQL parameter
  types, so the local docker-compose workspace can be bootstrapped repeatedly
  without hitting PostgreSQL type inference errors on embedded rows.

- **Service account loading and tenant binding are now fail-closed.**
  The API Keys page no longer preloads the admin-only service-account list for
  member access, the service-account panel now shows a retryable error state
  instead of an empty list when the list request fails, and API-key linking now
  verifies the target service account belongs to the same tenant before
  persisting the binding. Cross-tenant service-account IDs in existing data now
  fail authentication instead of silently inheriting another tenant's status.

- **Maturity contract lint gate for the platform program.**
  Added `scripts/lint-maturity-contract.sh` plus local `make ci-check` and CI
  wiring so the platform maturity umbrella proposal, child track links, and
  verification sections stay aligned. The gate now fails if the program loses
  traceability or a child proposal drops its verification contract.

- **HTTP response-emission audit false-positive cleanup.**
  Narrowed the direct-response lint to skip documented low-level response
  owners and wrappers, and routed the Console auth-providers payload through
  the shared JSON helper so the main router no longer emits JSON directly.

- **Reliability release context and ownership card.**
  Added a system release metadata endpoint plus a Reliability-page card that
  surfaces the active service version, deployment environment, owning team,
  restore-drill status, runbook link, escalation link, and runtime age.
  Operators can now see what release is running, how recovery is trending, and
  where to hand off without leaving the Console. The release / recovery /
  preflight system endpoints now also share one proto/OpenAPI contract, so the
  Console and future clients read the same shapes.

- **Dedicated recovery endpoint for restore-drill state.**
  Added an admin-only `/system/recovery` endpoint backed by the latest recorded
  restore drill, plus a shared grading helper so preflight and Console now use
  the same recovery policy. The Reliability page reads that dedicated contract
  directly, and now surfaces the latest backup reference, drill duration, and
  freshness window alongside recoverability and remediation so operators do not
  need to infer them from readiness checks. The preflight, recovery, and
  release system endpoints are now defined in proto as well, so the runtime
  contract stays in sync with the generated OpenAPI and client types.

- **Canonical platform semantics and lifecycle state.**
  Added a domain-level glossary for the platform's core terms plus canonical
  compatibility rules, and surfaced the current lifecycle state on the release
  card so operators can tell at a glance whether the runtime is supported,
  migrating, or blocked.

- **Saved audit-log investigation views.**
  Added per-user saved audit-log investigation views backed by system
  settings, with CRUD endpoints and Console support for saving, restoring, and
  deleting named investigation states. The audit-log sidebar now shows the
  active saved view binding and highlights when the live investigation matches
  a saved snapshot.

- **Developer parity demo bootstrap/reset loop.**
  Added `attune demo seed|reset|bootstrap` plus matching `make demo-*`
  targets so contributors can clear and rebuild the demo workspace without
  manual SQL. Demo reset now clears the seeded feedback, telemetry, and
  control-tower action rows before reseeding, and the README and testing guide
  now document the fresh-clone bootstrap loop.

- **MCP extension catalog kinds and governance exposure.**
  Added explicit `core` / `optional` / `external` kinds to the MCP tool
  catalog, plus owner and default-enablement metadata so the extension plane
  can distinguish built-in runtime tools from optional and external
  extensions. The runtime policy evaluator now treats disabled extensions as
  denied-by-default, and the console governance DTOs and MCP client contract
  carry the catalog metadata too, which keeps the classification visible at
  the API boundary instead of burying it inside internal code. The catalog now
  also supports compatibility aliases with deprecation and replacement
  metadata, the JSON-RPC registry registers those aliases alongside the
  canonical method names, policy replacement inputs normalize aliased tool
  names to their canonical catalog entries, and the Console tool table renders
  the alias / replacement relationship directly for administrators. The
  catalog contract also now carries optional provenance metadata so external
  extensions can declare artifact references without redesigning the registry
  again later. Canonical tools can now also declare explicit deprecated /
  replacement metadata, and the Console tool table renders that lifecycle
  state alongside the alias compatibility hints.

- **Runtime production profile and startup safety contract (#56).**
  Added a top-level runtime `profile` field with `dev` and `production`
  modes, plus a production startup safety gate that rejects unsafe Console
  base URLs, egress settings, malformed Console URLs, and missing bootstrap-
  admin coverage on fresh installs. The shared preflight checks now mirror the
  same production rules and warn when the bootstrap seed is still present after
  admins already exist, and the Helm / Compose examples and deployment docs now
  render and describe the profile and security proxy settings explicitly.
  Observability defaults now follow the runtime profile when `observability.environment`
  is omitted, so production instances report as `production` without extra config.
  The Helm chart now mirrors that behavior as well: an empty rendered
  observability environment falls back to the chart `profile`, so dev installs
  stop advertising themselves as production and production installs still
  default to `production`.

- **Tenant impact SLO surface for reliability burn-rate operations.**
  Added the new `Attune Tenant Impact` dashboard, including impacted-tenant
  burn ranking, a Console reliability summary page, burn-rate recording rules
  and MWMB alerts for ingest, enrichment, outbox delivery, OIDC login, API key
  access denials, MCP tool calls, and GDPR job completion, plus an exact 5s
  enrichment bucket so the latency SLO is measured directly instead of
  approximated. The GDPR completion ratio now excludes cancelled and revoked
  jobs from the denominator so the burn-rate view stays aligned with the
  dashboard completion lens. The tenant-impact generator now also exposes MCP
  and GDPR drilldowns with tool and request-type label filters, and API key
  usage is now recorded so the existing access-denial telemetry and security
  panels have a real success denominator.

- **Reply draft review and controlled-send workflow (#164).**
  Added persisted reply-draft cycles, revision history, workflow events,
  Console edit/approve/reject/send actions, admin-only encrypted reply-send
  hook configuration, idempotent reviewed-reply webhook delivery, generated
  proto/OpenAPI/SDK contract updates, source-freshness and approved-hook
  freshness guards, send-pending duplicate-delivery locking, retryable
  send-failure state, and audit coverage for generate, edit, approve, reject,
  send, and hook configuration actions.
  Added a dedicated Console reply workspace with an opaque composer surface,
  preflight checklist, evidence panel, revision timeline, AI-versus-human diff
  summary, and two-step send confirmation that shows the exact final text before
  delivery. Added a reply-send-hook delivery contract panel with signed-header,
  timestamp, idempotency, sample-payload, and security-check guidance; admin
  test delivery, diagnostic recent delivery log with delivery IDs, full
  idempotency keys, hook fingerprints, and retry timing, failed-delivery
  redelivery controls, a structured reply-send-hook health API and summary for accepted,
  failed, retryable, and dead deliveries, versioned reply-hook signatures, strict
  idempotency conflict detection, loopback HTTP hook endpoints for local
  receiver testing while keeping production hooks on HTTPS, state-marking error
  propagation for failed sends, external message ID capture from accepted hook
  responses, redacted transport errors so credential-bearing hook paths and
  query strings never reach delivery logs, retry-backed webhook sends,
  policy-aware SSRF validation at hook save time, and an automatic
  reply-delivery retry worker that drains due `reply.send` attempts with indexed
  due-retry and stale-pending recovery scans; plus dedicated outbound metrics
  for reply-send hooks, hook updates that preserve the existing secret unless a
  replacement secret is provided, disabled hook visibility after page reload,
  secret preservation across temporary hook disable/re-enable, full
  feedback cache invalidation for reply workflow mutations, immediate
  reply-send-hook cache updates from successful configure/disable responses, and
  reply-send delivery-log and health refresh after send attempts, hook
  observability refresh after configure/disable, and latest-workflow handling
  for back-to-back Console reply actions. Reply-send-hook delivery logging now
  reports the final attempt's HTTP status instead of carrying a previous retry
  response status into a later network failure, and inactive hooks no longer
  expose active-only Console actions. Reply-send idempotency fingerprints now
  bind the hook destination fingerprint as well as the approved revision, and
  reply-send-hook test events now enforce tenant-scoped idempotency keys:
  accepted test replays return the cached attempt, pending test replays report
  in-progress, and failed or dead test attempts reuse the same delivery attempt
  for explicit redelivery. Reusing a test idempotency key after the hook
  destination fingerprint changes now returns an idempotency conflict instead
  of replaying an old test result. Failed hook test deliveries no longer enter
  the background retry worker, while failed `reply.send` deliveries still use
  scheduled retry. Late delivery completions now only mutate still-pending
  attempts for the same approved revision and hook, so accepted sends cannot be
  downgraded by delayed failures and old successes cannot overwrite edited
  drafts. Editing after a failed send also clears stale external delivery
  markers. Hook names are normalized and validated before storage, and malformed
  delivery-log `limit` query parameters now return a precise bad-request error
  instead of the generic JSON decode message.
  Reply workflow mutations and send-hook actions now fail closed when their
  global audit record cannot be written, revision responses include structured
  source metadata, and Console hides regenerate controls whenever the backend
  omits `regenerate` from allowed actions.
  Console labels failed/dead hook attempts as manually redeliverable instead of
  implying dead attempts will be automatically retried.
  Reply approval now requires an active send hook so the reviewed destination is
  captured at approval time, approve events record the captured hook, pending
  sends cannot be rejected over an in-flight attempt, manual regeneration fails
  closed when workflow state cannot be checked, and failed-delivery redelivery
  rechecks source and hook freshness before retrying.
  Extended GDPR export/delete coverage to include reply draft workflows,
  revisions, events, and reply delivery attempts. Added Console
  browser E2E coverage for the review/edit/approve/send path,
  opaque-background guard, and admin reply-send-hook configuration, test, and
  redelivery path. Proto code generation now falls back to fixed-version local
  plugins when Buf remote plugins are unavailable, keeping `make proto` and the
  proto-sync gate reproducible during Buf service outages.

- **Feedback intelligence control tower (#162).**
  Added a Console Control Tower landing page that synthesizes classification
  quality, semantic-search quality, index coverage, top risks, and proof-trail
  evidence into the authenticated default route.
  Added a quality-action ledger with generated proto/OpenAPI/SDK bindings,
  Console status controls for acknowledging, resolving, or dismissing quality
  risks, runtime-smoke coverage for the Control Tower path, and
  `attune demo seed` for a reproducible local workspace with realistic feedback,
  search telemetry, and action state.

- **Semantic search operator workflow (#162).**
  Added a feedback workbench search mode switch that lets operators run
  natural-language semantic searches inside the existing queue, reuse supported
  filters, view match metadata and keyword-fallback state, and continue opening
  returned feedback rows from the same operational surface.
  The search backend now also uses a PostgreSQL full-text lexical scorer,
  reciprocal rank fusion, ranking-version metadata, coverage metadata, fallback
  reasons, and response evidence snippets, with matching Console evidence
  display, OpenAPI/proto/SDK contract updates, a full-text search index, and
  reusable relevance metric primitives plus validated synthetic golden search
  fixtures, a deterministic baseline report, a local baseline-check tool wired into
  developer quality gates with explicit ranking-version drift errors, and
  Prometheus search health metrics for fallback reasons and embedding coverage.
  The Console now surfaces the active ranking version in semantic result status
  so operators can match screenshots and support reports to the ranking contract.
  Search responses now include a run ID, and the Console records semantic result
  open events against that run for bounded relevance telemetry.
  Added search operations storage, quality APIs, and an Analytics search-quality
  dashboard covering search volume, zero-result rate, fallback rate, click-through
  rate, p95 latency, top queries, zero-result queries, fallback reasons, index
  coverage, and ranking-version status.
  Search requests now trim surrounding
  whitespace before execution, reject whitespace-only queries consistently, and
  treat `%` / `_` as literal characters in lexical partial-match fallback.
  The semantic-search quality baseline now covers 25 synthetic queries, and a
  dedicated performance harness can run HTTP load checks plus PostgreSQL
  `EXPLAIN (ANALYZE, BUFFERS)` for lexical search plans.

- **Classification quality dashboard with drift detection (#161).**
  Added the console classification-quality dashboard, bounded sample drilldowns,
  drift-aware aggregate storage, failure-event snapshots, list filters for
  quality signals, generated proto/OpenAPI/SDK bindings, and Prometheus/Grafana
  coverage for low confidence, off-list values, parse failures, terminal
  failures, active warnings, and per-dimension drift.

- **Layered test command topology.**
  Added `make fast-check`, `make adversarial-check`, `make runtime-smoke`, and
  `make release-smoke`, plus a reusable Docker runtime smoke script that boots
  the built image against throwaway pgvector PostgreSQL and verifies health,
  Console assets, metrics, migrations, and classification-quality schema.

- **Outbound adapter conformance harness (#167).**
  Added a shared `internal/outbound/outboundtest` suite, per-adapter golden
  request snapshots, fake-provider delivery mocks, and a CI lint gate so
  outbound adapters must prove response classification, redaction,
  mention-safety, and provider-shaped delivery behavior before they can be
  added or changed. The harness now also validates provider-specific payload
  shapes, captures fake-provider assertion failures after the HTTP response,
  requires adapters to declare golden snapshots, provider shapes, shared
  response profiles, and async-safe provider checks in both the CI lint gate
  and the Go conformance runner, removes the older goroutine-local fake-provider
  assertion entry point, and includes opt-in live smoke tests for raw webhook,
  Slack, Discord, Lark, and GitHub issue delivery.

- **Outbound delivery observability (#167).**
  Added low-cardinality transport metrics for provider delivery attempts,
  end-to-end delivery duration, and honored `Retry-After` responses, with
  generated Grafana dashboard coverage and metrics reference documentation.

- **Terminal failure workbench for the feedback console (#159).**
  - Added a dedicated `/feedback/terminal-failures` console view that opens on terminal rows and surfaces bounded cluster summaries by failure class, routed model/channel, terminal config fingerprint, and age bucket.
  - The workbench now highlights a global priority cluster, with evidence panels for the first cluster in each dimension so operators can see the strongest signal before drilling into samples.
  - The main feedback queue now reuses the same terminal-failure priority signal, so the list view and the dedicated workbench point at the same next sample.
  - The priority rail now also exposes direct navigation to the matching remediation surface when the operator can access it.
  - The queue view and the dedicated workbench now include explicit one-click switches between each other, so operators can move between the overview and the deep-dive surface without hunting through the sidebar.
  - The terminal workbench now exposes in-page jump links to each failure dimension, making the busiest review flows faster on long pages.
  - Sample rows in the workbench now include direct retry actions, and successful retries refresh the feedback workbench cache family so operators see the updated terminal counts immediately.
  - Terminal enrichment failures now persist failure-time snapshots on the row itself so the console can show stable remediation context even after config changes.
  - Added a bounded reason-class metric breakdown for terminal failures, plus the supporting proto, OpenAPI, console navigation, and detail-sheet snapshot fields.
  - Regenerated the published OpenAPI contract and aligned the Node/Go SDK generation and lint output so the proto-sync and SDK CI jobs stay green.
  - Sanitized the generated OpenAPI artifact so generator comments and external documentation links no longer trigger the PR secret-scan job.
  - Rebuilt the Node SDK baseURL credentials test input at runtime so the source no longer carries a literal embedded-credentials URI that secret scanning flags.

- **Public management APIs and SDK coverage for audit/GDPR/outbox/MCP governance (#168).**
  Scoped API-key routes now expose selected admin operations under canonical
  `/v1/...` paths, with matching Node and Go SDK methods.
  - New public API-key management routes for audit-log query, GDPR job control,
    outbox visibility/retry, and MCP OAuth client governance, all reusing the
    existing console handlers.
  - New public audit/GDPR binary lifecycle routes for `GET /v1/audit-log/export.csv`,
    audit evidence export/create/download, and `GET /v1/gdpr/exports/{job_id}/download`,
    with regenerated OpenAPI and proto bindings.
  - GDPR management is now least-privilege by default: new `gdpr:read`,
    `gdpr:export`, and `gdpr:delete` scopes split the old `gdpr:admin` umbrella,
    while a migration backfills the granular scopes onto existing
    `gdpr:admin` keys for compatibility.
  - New `mcpclient:admin` scope for MCP client governance, kept separate from
    runtime `mcp:*` tool scopes and excluded from the `full_access` preset.
  - Newly exposed management routes require explicit scopes, so legacy keys
    without a scopes list do not silently gain access.
  - Node and Go SDKs now cover the selected audit/GDPR/outbox/MCP management
    surfaces alongside the existing tags/workflow APIs, including binary
    download helpers plus audit/GDPR/outbox pagination iterators/pagers.
  - Management `POST` routes that are safe to deduplicate now honor
    `Idempotency-Key`, and both SDKs auto-generate stable per-call keys so tag,
    workflow, audit evidence, GDPR, outbox retry, and MCP client create flows
    can safely retry transient failures.

- **Date-pinned public API version contract for `/v1` routes.**
  - API-key product routes now accept an optional
    `X-Attune-Api-Version: 2026-06-28` request header, echo the effective
    version on the response, and reject unsupported/empty/ambiguous values with
    the shared `400 BAD_REQUEST` error envelope.
  - The official Go and Node SDKs now send that header automatically on every
    public API request and reserve it against caller override, so published SDK
    artifacts stay pinned to the contract they were built against.
  - The generated OpenAPI document now publishes the version request header plus
    response `X-Attune-Api-Version` / `Deprecation` / `Sunset` metadata for
    public `/v1/...` operations.
  - The API-version middleware now preserves optional response-writer
    interfaces such as `http.Flusher`, `io.ReaderFrom`, and `http.Hijacker`, so
    `/v1` export/download paths keep their underlying transport behavior while
    still injecting the contract headers.

### Fixed

- Darkened the Console destructive action color token so destructive buttons
  meet browser-verified WCAG contrast thresholds.
- Made the Console feedback detail sheet and reply-draft surface opaque so the
  underlying feedback queue cannot bleed through reviewed-reply content.

- **Semantic search availability (#162).**
  Soft-deleted feedback rows with embeddings no longer make a tenant appear
  semantically searchable when no live embedded feedback remains.
  Search quality metrics and hybrid ranking now count each relevant feedback ID
  once per query, so duplicate ranked results cannot inflate recall, NDCG, or
  fused result scores.
  Semantic search now rejects overlong queries and unsupported control
  characters before they reach embedding generation or PostgreSQL full-text
  search.

- **Outbound adapter delivery safety (#167).**
  Fixed Lark webhook URL logging, GitHub issue request-body logging, GitHub 403
  classification, Lark malformed-provider-body handling, and GitHub/Lark
  rendering of outbox-shaped envelopes and mention-bearing user text. Outbound
  delivery retry timing now honors valid provider `Retry-After` headers on
  retryable responses while still short-circuiting terminal failures, clamps
  oversized retry backoff without overflowing, and handles non-positive render
  truncation limits safely. GitHub issue delivery now sanitizes derived labels
  before sending them to the provider and no longer relies on a package-global
  API-base override in provider-mock tests. Lark digest delivery now renders
  structured cards for JSON-roundtripped digest views, and Lark note elements
  now use the provider's string `content` shape. Outbox transport labels now
  carry a bounded destination type so shared delivery metrics can attribute
  retries and terminal outcomes without high-cardinality labels.

- **Local duplication gate.**
  Aligned `scripts/check.sh` and the contributor guide with the CI-backed
  `.jscpd.json` configuration, so local checks use the same threshold and test
  fixture exclusions as pull requests.

- **Prometheus alert naming.**
  Renamed the global enrichment latency alert to
  `AttuneGlobalEnrichmentLatencyHigh`, so it no longer collides with the
  tenant-scoped `AttuneEnrichmentLatencyHigh` alert in Prometheus rule checks.

- **Private deploy Compose database image.**
  Switched the embedded Compose PostgreSQL service to `pgvector/pgvector:pg17`,
  matching the migration requirement for the `vector` extension.

- **Docker build context hygiene.**
  Ignored local Console workspace files, generated test binaries, and Node SDK
  install artifacts so ordinary Docker builds stay reproducible from a dirty
  developer checkout.

- **Classification quality adversarial input guards (#161).**
  Rejected non-finite low-confidence thresholds and non-positive sample IDs,
  ignored non-positive diagnostic counts during aggregate refresh, and added
  adversarial/property coverage for malformed classification-quality payloads,
  bounded samples, and UTF-8-safe value displays. Duplicate values from one
  classification event no longer double-count value-level confidence, and empty
  quality sample arrays now persist as PostgreSQL empty arrays instead of NULL.
  PostgreSQL integration coverage now also scans persisted quality buckets for
  impossible count relationships, oversized sample arrays, invalid sample IDs,
  and overlong value displays.

- **Console accessibility and keyboard triage for critical workbenches (#171).**
  - Added a dev-only `axe-core` Vitest helper and smoke coverage for selected
    Console workbench states.
  - Added an app-shell skip link, a stable `#main-content` target, and polite
    loading status semantics.
  - Made MCP client selection keyboard reachable with real controls, and added
    tool-specific labels for MCP policy toggles and rate-limit inputs.
  - Added accessible names and state for semantic search clearing, API-key scope
    disclosure, GDPR request filters, outbox retry actions, and audit-log
    shortcut/search affordances.
  - Fixed GDPR request-center mobile overflow by allowing the main grid columns
    to shrink before dense table and card content scrolls.
  - Covered the new keyboard, focus, pressed-state, and disclosure behavior in
    focused Console tests.
  - Added a Playwright browser accessibility gate for the critical Console
    routes in desktop and mobile Chromium, including real-browser axe checks,
    document-overflow assertions, console-error failure, and deterministic
    Console API route mocks.
  - Fixed the Console skip link so activation moves focus to the main content
    landmark instead of leaving keyboard users in the app chrome.
  - Fixed Console shell header overflow on narrow mobile viewports by collapsing
    low-priority account chrome before it can widen the document.
  - Added browser coverage for legacy Console route redirects and shell
    navigation to canonical MCP client and dead-delivery pages.
  - Added MCP clients to the primary Console navigation for administrators.
  - Extended the browser accessibility gate to cover API-key create/revoke
    success and error paths plus success and error status messages for feedback
    retry-enrichment, MCP tool policies, MCP session/grant revocation, GDPR
    validation, and dead-delivery retry, with toast checks bound to the Sonner
    polite live region.
  - Added accessible names for MCP session and refresh-grant tables.
  - Added route-specific Console document titles for the critical workbenches.
  - Tightened Console status, dialog-helper, toast, selected-row, warning, and
    token colors so the browser axe gate passes WCAG AA contrast checks in the
    covered states.
  - Extended the browser gate with narrow-viewport route churn, long GDPR
    subject keys, repeated dialog/sheet open-close cycles, and terminal
    workbench jump-link checks; fixed the workflow transition select so its
    placeholder state has an accessible name.
  - Added feedback-id context to terminal workbench sample retry buttons so
    assistive technology and browser tests can distinguish each retry target.
  - Added Console accessibility component contracts, WCAG 2.2 A/AA
    traceability, and an assistive-technology matrix for manual screen-reader
    evidence.
  - Added a Console accessibility release checklist and opt-in supplemental
    Playwright projects for Edge, Firefox, and WebKit desktop browser sweeps.
  - Hardened controlled Dialog and Sheet focus restoration so WebKit/Safari
    routes return focus to the invoking control instead of the main landmark.
  - Extended the browser accessibility gate with forced-colors, WCAG
    text-spacing, and 200% text-sizing sweeps for the critical Console routes,
    and fixed terminal-failure metrics plus MCP client layout so text-resize
    reflow does not create document-level horizontal overflow.
  - Added a manual/scheduled Console accessibility supplemental workflow for
    the CI-safe Firefox and WebKit browser sweep, while keeping Edge available
    in the local supplemental Playwright script.

- **Terminal failure detail sheet close affordance now stays above the sticky header.**
  - The terminal failure detail sheet's visible close button now renders above the sticky workbench header and can be dismissed reliably from the UI.

- **Generated OpenAPI now publishes attune's real error contract.**
  - `docs/openapi/openapi.yaml` now includes the shared `ErrorResponse` schema
    and points default JSON error responses at it instead of
    `google.rpc.Status`, so the published contract is self-contained for SDKs
    and external API consumers.
  - Explicit non-2xx responses that previously carried only descriptions now
    also declare the shared JSON error schema, including the public
    `/v1/feedback/ingest` idempotency / body-too-large / rate-limit responses.
  - `make proto` now runs a deterministic `internal/tools/openapipatch`
    post-processing step so the `proto-sync` path keeps generating the corrected
    OpenAPI artifact automatically instead of relying on README-only guidance.

- **Browser ingest now has first-party, route-scoped CORS support.**
  - attune now accepts optional `ingest.cors_allowed_origins` runtime config
    and mounts CORS only on the publishable `POST /v1/feedback/ingest` surface,
    so browser widgets can use official SDK headers (including
    `X-Attune-Api-Version`) without opening the server-only management API to
    cross-origin use.
  - Browser-facing ingest now applies CORS before public API version
    validation, so an unsupported or malformed `X-Attune-Api-Version` still
    returns a readable `400 BAD_REQUEST` to allowed origins instead of
    degrading into an opaque browser CORS failure.
  - Config validation now normalizes exact origins, rejects invalid
    scheme/path/query/credential shapes, and keeps CORS disabled by default.
    Default-port origins are canonicalized too, so
    `https://app.example.com:443` / `http://localhost:80` still match the
    browser's serialized `Origin` header. Explicit ports are now also validated
    and normalized numerically, so malformed values like `:99999` fail fast and
    equivalent spellings such as `:0443` do not silently miss browser CORS
    matches.
  - Explicit-origin responses now always emit cache-safe `Vary` metadata, and
    wildcard CORS automatically reflects the request origin when credentials are
    enabled so attune never emits the invalid `Access-Control-Allow-Origin: *`
    plus `Access-Control-Allow-Credentials: true` combination.
  - Allowed browser origins now echo additional valid
    `Access-Control-Request-Headers` values on preflight, so cross-origin SDK
    callers can attach custom trace or correlation headers without patching the
    server's fixed allowlist, while malformed header tokens are still ignored.

- **Node SDK publish-artifact e2e now includes a real browser cross-origin smoke.**
  - `pnpm e2e` now installs the packed npm tarball into a throwaway consumer
    and loads it in a real Chromium-family browser from both an allowlisted and
    a blocked origin, verifying actual preflight/CORS behavior, successful
    browser ingest, exposed `X-Attune-Api-Version`, and blocked-origin failure
    before release.
  - That browser smoke now runs with the least-privilege `ingest:write` key
    rather than the broader management key, prefers the freshly installed or
    cached Playwright Chromium binary before ambient system channels, and
    cleans up its temporary files / local static servers even when startup
    fails partway through.
  - Browser runtimes no longer try to set a custom `User-Agent` header; Node
    still sends the versioned SDK `User-Agent`, while browser callers rely on
    standard platform behavior plus `X-Attune-Api-Version`. Worker-like
    runtimes without a genuine Node process now follow that same no-custom-UA
    path instead of incorrectly inheriting the Node-only header behavior.
  - The Node and Go SDK live e2e harnesses now pick free localhost ports
    instead of assuming fixed ones, so browser smoke/static servers, webhook
    receivers, and local attune instances do not collide with parallel SDK
    runs or unrelated developer processes.

- **Go SDK redirect hardening now survives caller-injected `http.Client`s.**
  - `attune.WithHTTPClient(...)` now always copies the supplied client and
    forces the SDK's internal redirect policy to refuse 3xx responses, so a
    permissive caller `CheckRedirect` callback cannot accidentally re-enable
    `X-API-Key` leakage to a redirect target.
  - The caller's original `*http.Client` remains unmodified, so shared
    transports and library-level client reuse keep their original behavior
    outside the SDK.
  - The Node and Go SDKs now reject invalid `baseURL` forms earlier and more
    consistently: embedded credentials, query strings, fragments, and invalid
    ports now fail at construction instead of producing malformed outbound URLs
    or surfacing much later as transport errors.
  - Go SDK binary download helpers now prefer and decode RFC 5987
    `Content-Disposition: filename*=` values, so audit/GDPR exports preserve
    internationalized attachment names instead of falling back to ASCII
    placeholders or raw percent-encoded filenames. When a server sends both a
    plain `filename` fallback and a malformed `filename*`, the SDKs now prefer
    the valid fallback name instead of exposing the broken RFC 5987 value.

- **Production hardening for GDPR exports and console session reads (#168).**
  - GDPR exports no longer auto-download as soon as a job completes; operators
    must explicitly download sensitive ZIP archives from the status card or the
    request center, and previously ready/downloaded exports now keep a visible
    download action after page reload. Newly completed exports also refresh the
    request center automatically so operators immediately see the ready state
    without a manual page refresh.
  - `GET /fb/v1/console/me` and the legacy admin gate now treat canceled or
    timed-out requests as client aborts / timeouts instead of logging them as
    internal `500` errors.
  - OIDC `/me` no longer clears a valid session when the user lookup fails for
    an internal repository error; only a real `oidc user not found` condition
    invalidates the session.
  - The public API-key MCP client governance routes now emit the published
    proto/OpenAPI wire contract (lowerCamelCase protojson plus standard
    `ErrorResponse` codes) instead of the Console-only snake_case payloads, so
    Node SDK callers see the documented field names on `/v1/mcp/clients/...`
    responses and both SDKs receive canonical public error codes.
  - The Node SDK now treats successful `204 No Content` management responses as
    valid empty results, so MCP revoke routes no longer fail with a client-side
    JSON parse error during real publish-artifact usage, while still rejecting
    empty non-`204` success bodies so a broken `200/202` response cannot be
    silently mistaken for a valid typed payload.
  - Async GDPR export/delete workers and audit-evidence export jobs now persist
    and replay the original actor type alongside `created_by`, and the
    audit-evidence create/download path now respects non-admin session types, so
    machine, OIDC, and admin initiated compliance operations no longer collapse
    to `actor_type=admin` after leaving the interactive path.
  - MCP OAuth client redirect URI validation now rejects hostless or
    scheme-relative values such as `https:callback`, `https:///callback`, and
    `//localhost/callback` on both the console and public API-key management
    routes, closing an edge-case gap where malformed redirect targets could pass
    the previous scheme-only check.
  - The Node SDK now rejects ill-typed config, request payloads, request
    options, and management/query inputs with stable `AttuneError` validation
    failures before sending any request, so plain JavaScript callers no longer
    hit raw `TypeError` crashes or accidentally emit malformed bodies, ignore
    broken or fake `signal`/`options` values, or continue with silently coerced
    admin inputs.
  - The Node SDK now also rejects malformed query/options inputs such as
    `includeArchived: "false"` and `actions: "tag.create"` instead of silently
    coercing them into wrong query strings or truthy flags, closing another
    plain-JavaScript edge case where callers could send unintended admin
    filters without realizing it.
  - Node SDK binary download helpers now prefer and decode RFC 5987
    `Content-Disposition: filename*=` values, keeping audit/GDPR attachment
    names consistent with the Go SDK and preserving internationalized
    filenames instead of dropping back to ASCII placeholders; quoted fallback
    `filename="..."` values still parse correctly even when the attachment name
    itself contains semicolons.
  - The Node SDK's 1 MiB response-body cap now still applies when a custom or
    polyfilled `fetch` only exposes non-stream `arrayBuffer()` / `text()`
    fallbacks, closing a memory-hardening gap outside the standard streaming
    fetch path. Both SDKs now also short-circuit immediately when the declared
    `Content-Length` already exceeds the cap, avoiding unnecessary reads of
    obviously oversized responses.
  - The Node SDK now rejects malformed admin query scalars such as
    `limit: NaN`, `cursor: {}`, `from: {}`, and `beforeId: 99`, and it also
    rejects non-string resource ids like `archiveTag(123)` or
    `getGdprExport(123)` with stable `AttuneError` validation failures instead
    of leaking raw `TypeError`s or silently stringifying broken values into
    outbound `/v1/...` requests.
  - The Node SDK now also rejects nonsensical pagination and outbox numeric
    values such as `limit: 0`, `limit: -1`, `beforeId: '-99'`,
    `beforeId: '92233720368547758070'`, and `retryOutboxDelivery('abc')`
    instead of quietly sending server-side default-amplifying or overflowed
    `/v1/...` requests.
  - The public audit-log, GDPR request-list, and outbox list handlers now also
    reject explicit non-positive `limit` values at the HTTP boundary, and the
    outbox list handler rejects negative `before_id`, so raw `/v1/...` callers
    no longer get silent default/clamped pagination from obviously invalid
    inputs.
  - GDPR request-list validation now treats `request_type` as the closed
    `export|delete` set across the Node SDK, Go SDK, and backend service path,
    and malformed GDPR list cursors now map to `400 BAD_REQUEST` instead of
    falling through as internal errors.
  - The Node and Go SDKs now also treat outbox `status` as the closed
    `pending|delivered|failed|dead` set, trimming and deduping valid entries
    while rejecting blank / bogus statuses and negative Go `beforeId` /
    pagination values before they can silently widen a default query.
  - The Node and Go SDKs now validate MCP client / session / refresh-grant path
    IDs as UUIDs, matching the published `/v1/mcp/clients/...` contract so
    callers fail fast locally instead of sending impossible resource IDs over
    the network.
  - The Node and Go SDKs now also enforce MCP governance body constraints
    before sending: create requests require at least one safe redirect URI and
    only `mcp:read|mcp:write|mcp:ingest` scopes, update requests require the
    live `legacy_allow_all|allow_list` mode and positive rate limits, and tool
    policy replacements reject blank / duplicate tool names, invalid effects,
    and non-positive per-tool rate limits. The SDKs also normalize trimmed
    governance mode / tool policy values before serialization, keeping the
    publishable clients aligned with the backend's real replace-semantics
    contract.
  - MCP client creation now rejects blank or over-long names at the handler and
    SDK layers, and MCP client-name uniqueness / check-constraint failures now
    map to stable `400/409` validation-style responses instead of falling
    through as raw database-backed `500 INTERNAL` errors on either the console
    route or the public API-key proto surface.
  - MCP tool-policy replacement now requires an explicit `policies` field
    across the console route, public API-key proto route, and both SDKs, so an
    accidental `{id}` request can no longer be misread as “clear every tool
    override”; callers must now send `policies: []` intentionally to wipe all
    overrides.
  - MCP client governance PATCH requests now treat omitted fields as unchanged
    instead of silently clearing them, reject empty `{id}`-style updates, and
    keep Console's explicit JSON `null` semantics for clearing rate limits on
    the private admin route.
  - The Go SDK now rejects nil admin request structs client-side instead of
    silently serializing them to `{}`, and it now rejects empty non-`204`
    success bodies across SDK calls so a broken `200/202` response cannot be
    mistaken for a valid zero-value result.
- **Draft durability and navigation guards for settings editors (#172).**
  localStorage-backed draft persistence with versioned envelope format and
  unsaved-change protection for classification settings and enrichment runtime
  editor pages.
  - Drafts auto-save to localStorage with `StoredEnvelope` wrapper (schema
    version + timestamp), debounced 500ms, with 24h TTL auto-expiry.
    Transparent migration from legacy sessionStorage on first read.
  - `useBlocker` + `beforeunload` prevent accidental navigation with a shared
    `UnsavedChangesDialog` two-button modal (stay / discard-and-leave).
  - Persistent inline `DraftBanner` (recovery/conflict variants) replaces
    transient toasts for draft recovery and conflict notification.
  - `BroadcastChannel` cross-tab notification on draft clear.
  - `beforeunload` immediate flush via refs for stale-closure safety.
  - Keyboard shortcut: Cmd/Ctrl+S to save (hook also supports Cmd/Ctrl+Enter
    when `onSubmit` is wired).
  - `SaveStatus` indicator: saving spinner or unsaved dot; returns null when
    saved (no persistent timestamp).
  - `document.title` dirty indicator (`●` prefix when form is dirty).
  - Discard button to revert to last-saved server state.
  - Drafts cleared automatically on successful save/reset/rollback.
  - Background refresh conflict detection on both classification and runtime
    editors: when a refetch (polling, BroadcastChannel, or window focus)
    returns changed server data while the user has dirty edits, a conflict
    banner offers to load the latest version or keep local edits, without
    silently overwriting the in-progress draft.
  - Form inputs disabled during in-flight save to prevent mid-mutation edits.
- **SSO cutover and break-glass recovery controls (#158).** Enterprise-grade
  SSO enforcement with emergency access path for admin recovery.
  - Runtime-switchable auth mode (`hybrid` ↔ `sso_only`) stored in DB, no
    restart required. `system_settings` table for per-tenant config.
  - Break-glass tokens: one-time, time-limited (default 30m, configurable
    5m–24h), bcrypt-hashed, `bg_` prefix for log grep. Full lifecycle:
    issue via CLI/Console, validate on login, auto-expire.
  - Preflight checks block SSO cutover unless: OIDC enabled + issuer
    reachable + redirect_uri matches console.base_url + ≥1 break-glass
    token exists (or explicit skip).
  - Audit actions: `auth.mode_change`, `breakglass.issue`, `breakglass.use`,
    `breakglass.revoke`, `breakglass.expire`.
  - CLI: `attune auth breakglass {issue,list,revoke}`.
  - Console: Settings → Security → Auth Mode + Break-Glass Tokens.
  - Session `UserType` field distinguishes `local`, `oidc`, `breakglass`.
  - Design based on NIST SP 800-63C-4 replay protection requirements and
    patterns from Authentik, Ory Kratos, Okta, Microsoft Entra ID.
- **Signed compliance evidence packs for audit logs (#152).** Export
  tamper-evident ZIP archives of audit log entries with SHA-256 hash chains
  (RFC 8785 canonical JSON) and optional Ed25519 digital signatures.
  - Backend: `audit_evidence_export` migration, `repo/auditevidence`,
    `service/auditevidence` (archive builder + async claim/heartbeat/drain
    worker), `canonicaljson` package, `audit_evidence` config section.
  - Proto API: `CreateAuditEvidenceExport`, `GetAuditEvidenceExport`,
    `DownloadAuditEvidenceExport` RPCs on `AuditLogService`.
  - Console handler: `POST /audit-log/evidence`, `GET .../evidence/{job_id}`,
    `GET .../evidence/{job_id}/download` (admin-only, binary ZIP download).
  - CLI: `attune audit verify-export` (offline chain + signature verification),
    `generate-signing-key`, `export-public-key`.
  - Enriched manifest format: `export_id`, `created_by`, `filter`, `stats`
    (total_events, first/last event timestamps, action_counts), per-file
    SHA-256 hashes, `signing.public_key_fingerprint` for key rotation, and
    `external_anchors` reservation for future RFC 3161 timestamping.
  - Per-file SHA-256 verification in CLI verifier and Python reference
    verifier (`scripts/verify-evidence.py`).
- **HA worker leases and queue-drain safety (#155).** Standardizes background
  worker safety for multi-replica deploys:
  - `claimed_by` column across all worker task tables (reply_draft_task,
    embedding_task, digest_runs, batch_jobs, notify_outbox, gdpr_export_jobs,
    gdpr_requests) so heartbeat refresh only touches rows this worker instance
    holds — prevents stale replica A from extending a row that replica B
    legitimately re-claimed
  - `enrichment_claimed_by` column on user_feedback for enricher fencing
  - Fencing tokens on all MarkDone/MarkFailed/MarkDelivered/MarkDead/Complete/Fail
    methods — `WHERE claimed_by = $owner` prevents re-claimed tasks from being
    clobbered by slow original worker completing after lease expired
  - BatchJobWorker now uses full fencing: Claim/Heartbeat/UpdateProgress/Complete/
    Fail all check claimed_by
  - GDPR Worker now uses full fencing: ClaimNextExportJobWithOwner, HeartbeatExport
    JobWithOwner, CompleteExportJobWithOwner, FailExportJobWithOwner all check
    claimed_by; added drain support and lease lost early exit
  - Heartbeat goroutine in DraftWorker, EmbeddingWorker, DigestWorker, BatchJob
    Worker, GDPRWorker (90s/30s interval); uses `context.Background()` to avoid
    racing with parent context cancellation
  - **Lease lost early exit (Temporal pattern):** when heartbeat detects another
    worker re-claimed the task, it cancels the processing context immediately —
    aborts in-flight LLM/API calls rather than wasting tokens on work that will
    be discarded. New `lease_lost` metric label tracks these aborts.
  - Graceful shutdown drain with 30s timeout in all workers — SIGTERM waits for
    in-flight work before returning, so rolling deploys don't leave tasks
    half-processed
  - BatchJobWorker Stop() now has 30s timeout to prevent indefinite blocking
  - Unified `workerdrain` package eliminates ~100 lines of duplicate drain code
    across workers
  - Advisory locks for singleton pruners (audit, idempotency-key, MCP) so only
    one replica runs each pruner during overlapping replicas; uses dedicated
    connection to hold session-level lock correctly in connection pool
  - New metrics: `attune_worker_drain_total`, `attune_worker_in_flight`,
    `attune_worker_stale_claims_recovered_total`, `attune_worker_heartbeat_total`,
    `attune_advisory_lock_total`
  - Worker startup logs now include `owner` identifier for multi-replica tracing
  - New Prometheus alerts: `AttuneWorkerHeartbeatLost`, `AttuneWorkerDrainTimeout`,
    `AttuneWorkerStaleClaimsHigh` with corresponding runbook entries
  - Worker configuration now configurable via `workers.*` YAML block:
    `heartbeat_interval`, `stale_duration`, `drain_timeout`, `poll_interval`,
    `max_attempts`
  - HA integration tests for concurrent claiming, fencing, heartbeat, and stale
    claim recovery
  - Circuit breaker for LLM clients: fast-fails when upstream providers are
    unhealthy, preventing cascading failures and wasted tokens. Configurable
    failure threshold, success threshold, open duration, and half-open
    concurrency. New metrics: `attune_circuit_breaker_results_total`,
    `attune_circuit_breaker_rejected_total`, `attune_circuit_breaker_transitions_total`
  - `/startupz` endpoint for Kubernetes startup probes (30s timeout vs /readyz 2s)
  - gzip compression middleware for HTTP responses
  - Token bucket limiter cleanup goroutine to prevent unbounded memory growth
  - LLM response body size limits (10 MiB for chat, 50 MiB for embeddings)
  - Benchmark tests for rate limiters
  - New Prometheus alerts: `AttuneGlobalEnrichmentLatencyHigh`,
    `AttuneSearchLatencyHigh`, `AttuneCircuitBreakerOpen`,
    `AttuneEmbeddingQueueDepthHigh`
  - `safegoroutine` package for panic-safe goroutine launching with automatic
    recovery and metrics
  - Worker config validation ensuring heartbeat_interval < stale_duration
  - Dependency health check metrics: `attune_dependency_health_check_total`,
    `attune_dependency_health_check_duration_seconds`
  - Fuzz tests for rate limiters
  - `errwrap` package for consistent error wrapping
  - Refactored `parseDerivedFields` into focused helper functions (reduced CCN)
  - Typed OIDC configuration errors for better error handling
  - Rate limiting on `/auth/verify` endpoint to prevent brute-force
  - Database migrations:
    - `079_fix_foreign_key_cascades.sql`: adds ON DELETE to orphan-prone FKs
    - `080_add_unique_constraints.sql`: unique indexes for slug/name columns
    - `081_add_missing_indexes.sql`: FK indexes for query performance
  - Service layer tests for apikey and guardpolicy packages

- **Security headers and code quality improvements (#155).**
  - Security headers middleware (`internal/handlers/security/headers.go`):
    - X-Frame-Options: DENY
    - X-Content-Type-Options: nosniff
    - X-XSS-Protection: 1; mode=block
    - Referrer-Policy: strict-origin-when-cross-origin
    - Permissions-Policy: geolocation=(), microphone=(), camera=()
    - Content-Security-Policy for Console SPA
  - Safe query parameter parsing (`internal/handlers/queryparams/params.go`):
    - Type-safe Bool, Int, Int64, String, Enum, Duration, Time parsers
    - Built-in bounds checking and default values
  - Log sanitization utilities (`internal/pkg/logsanitize/sanitize.go`):
    - Prevents log injection via newlines/control characters
    - Safe truncation with length limits
  - Error wrapping utilities (`internal/pkg/errwrap/wrap.go`):
    - Nil-safe Wrap/Wrapf helpers
    - Shorthand Is/As/Join/New/Newf functions
  - Generic slice utilities (`internal/pkg/sliceutil/sliceutil.go`):
    - Map, Filter, Contains, Unique, Chunk, First, Last
  - Generic map utilities (`internal/pkg/maputil/maputil.go`):
    - Keys, Values, Merge, FilterKeys, FilterValues, GetOr
  - String utilities (`internal/pkg/stringutil/stringutil.go`):
    - Truncate, TruncateWords, IsBlank, FirstNonEmpty
    - ToSnakeCase, ToCamelCase, RemovePrefix, RemoveSuffix
  - Time utilities (`internal/pkg/timeutil/timeutil.go`):
    - Now, Unix, ParseRFC3339, FormatRFC3339 with explicit UTC
    - StartOfDay, EndOfDay, DaysAgo, DaysFromNow
  - HTTP utilities (`internal/pkg/httputil/httputil.go`):
    - WriteJSON, WriteError, WriteNoContent helpers
    - IsSuccess, IsClientError, IsServerError, IsRetryable predicates
  - Database table name constants (`internal/repo/tables.go`)
  - Digest worker test coverage
  - Default configuration constants (`internal/pkg/defaults/defaults.go`):
    - HTTP, Worker, Database, LLM, OIDC, Retry, Cache, RateLimit timeouts
    - Batch sizes and string limits
  - Error code constants (`internal/pkg/errorcode/codes.go`):
    - Standardized client/server error codes
    - ErrorResponse type for API responses
  - LRU cache with TTL (`internal/pkg/cache/lru.go`):
    - Thread-safe generic LRU cache
    - Automatic TTL expiration and cleanup
  - HTTP retry utility (`internal/pkg/httpretry/retry.go`):
    - Configurable retry with exponential backoff
    - Retryable status code detection
  - pgx rows utility (`internal/pkg/pgxutil/rows.go`):
    - CollectRows, CollectOne, ForEachRow helpers
    - Automatic rows.Close() handling
  - Database pool health check (`internal/infra/database/pool.go`):
    - CheckPoolHealth returns connection stats
    - Ping verifies database connectivity
  - Service package doc.go files for 10 packages
  - Benchmark tests for sliceutil
  - Fuzz tests for stringutil
  - CORS middleware (`internal/handlers/cors/cors.go`):
    - Configurable allowed origins, methods, headers
    - Preflight handling with caching
    - Credentials support
  - Rate limit headers (`internal/handlers/ratelimitheaders/headers.go`):
    - X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset
    - RateLimit-* headers (IETF draft standard)
    - Retry-After header support
  - Cursor pagination (`internal/pkg/pagination/cursor.go`):
    - Base64-encoded cursor with ID/timestamp
    - Generic Page[T] type
    - SQL condition generation
  - Feature flags (`internal/pkg/featureflag/flags.go`):
    - Thread-safe flag store
    - Context propagation
    - Global store singleton
  - Config reload (`internal/pkg/configreload/reload.go`):
    - SIGHUP signal watching
    - Debounced reload callbacks
    - Atomic Value[T] wrapper
  - RFC 7807 Problem Details (`internal/pkg/problemdetails/problem.go`):
    - Standardized error responses
    - Extension fields support
    - Common problem constructors
  - ETag support (`internal/handlers/etag/etag.go`):
    - Strong/weak ETag generation
    - If-None-Match / If-Match handling
    - 304 Not Modified responses
  - Request timeout middleware (`internal/handlers/timeout/timeout.go`):
    - Context-based timeout enforcement
    - Optional 503 response on timeout
  - Request validation middleware (`internal/handlers/requestvalidation/`):
    - Content-Type / Accept validation
    - Content-Length limits
    - Required header checks
    - HTTP method restrictions

- **Beyond-industry capabilities (#155).**
  - Adaptive rate limiting (`internal/infra/ratelimit/adaptive.go`):
    - AIMD (Additive Increase Multiplicative Decrease) algorithm
    - CPU/latency/success-rate aware throttling
    - Load shedding with automatic adjustment
  - Chaos engineering framework (`internal/pkg/chaos/chaos.go`):
    - Fault injection: latency, errors, panics, timeouts, partitions
    - Probability-based fault triggering
    - HTTP middleware for chaos testing
    - Experiment runner for controlled chaos
  - Canary release controller (`internal/pkg/canary/canary.go`):
    - Traffic percentage routing with auto-progression
    - Metrics-based auto-rollback (error rate, latency)
    - A/B testing with statistical comparison
    - Release state management
  - SLO automation (`internal/pkg/slo/slo.go`):
    - Service Level Objective tracking
    - Error budget calculation and monitoring
    - Window-based SLI measurement
    - Policy-driven alerting and degradation
  - Zero-trust authorization (`internal/pkg/zerotrust/zerotrust.go`):
    - Principal-based access control
    - Policy evaluation with conditions (MFA, device trust)
    - Resource/action matching with glob patterns
    - HTTP middleware for request authorization

- **MCP governance controls and admin surface (#153).** Expands the MCP server
  from scope-only access into a governed access plane. Includes:
  - Centralized MCP tool metadata catalog with runtime risk/data classification
  - Per-client governance defaults: tool-policy mode (`legacy_allow_all` /
    `allow_list`) and client-level rate-limit settings
  - Per-tool override records with explicit allow/deny and override RPM/burst
  - Token-bucket enforcement for client/tool RPM + burst limits, so configured
    burst ceilings take effect at runtime instead of remaining display-only
  - Request-accurate MCP session activity tracking (`last_tool_name`,
    `last_decision`, IP, user agent, close reason/by)
  - Audit events for MCP authorization denials, MCP rate-limit denials, client
    governance changes, tool-policy changes, single-session revocation, and
    single refresh-grant revocation
  - Console MCP access page expansion: client detail, active sessions, refresh
    grants, governance editor, tool-policy editor, single-session revoke, and
    single refresh-grant revoke
  - MCP client admin proto/OpenAPI contract for the Console control plane

### Changed

- **Console UI minimalism overhaul (#172).** Converge card styling, page
  layout, and shared components toward a restrained, industry-standard
  aesthetic across all Console pages.
  - `PageHero`: plain text layout with inline metrics (tooltip hints via
    `title`); removed gradient backgrounds, shadows, and card wrappers.
  - `DraftBanner`: border-l-2 inline bar with text buttons; removed
    rounded card wrapper and heavy Button components.
  - `SaveStatus`: text-xs only (saving spinner or unsaved dot); returns
    null when clean — no persistent saved-at timestamp.
  - `UnsavedChangesDialog`: two equal-weight outline buttons (stay /
    leave); removed save-and-leave third option and destructive styling.
  - Card headers: removed `bg-muted/15` backgrounds and `border-b`
    separators across notify-targets, guard-policies, api-keys,
    digest-subscription, workflow-settings, tags, GDPR, outbox-dead,
    inbound-sources, and LLM config pages. Unified `CardTitle` to
    `text-base`.
  - Card borders: unified to `border-border/60 shadow-none` across all
    settings and configuration pages.
  - Enrichment runtime page: removed Value Cards, Playbook steps,
    Convergence Alert, RuntimePosture cards, Operator Guidance, and
    GuidanceBlock sections (~250 lines). `RuntimeFormSection` uses
    `border-t` separators with inline icons instead of rounded containers.

### Fixed

- **Migration count and protobuf vet cleanup.** Updated the database
  migration-count expectation to reflect the renamed `098_delegated_admin_role.sql`
  migration, and switched the audit-log saved-view protobuf mappers to return
  pointers directly so `go vet` no longer reports protobuf lock-value copy
  warnings.

- **MCP protocol compatibility and governance hardening (#153).** Improves the
  MCP server's real-world interoperability and operator safety by:
  - adding a dedicated MCP public base URL path so discovery no longer has to
    piggyback on `console.base_url`, with fallback to the configured MCP issuer
    before falling back to the Console URL
  - publishing RFC-compliant OAuth protected-resource and authorization-server
    metadata routes, while keeping the older `/mcp/**/.well-known/*` paths for
    backwards compatibility with existing manual setups
  - codifying a 10-scenario MCP compatibility regression matrix covering root
    discovery, legacy discovery, OpenID compatibility metadata, Bearer
    challenges, resource indicators, and authorization redirect issuer hints
  - adding a Console MCP connection workspace with deployment-derived endpoint
    URLs plus fixed-client templates for Claude Code, Cursor, VS Code, and curl
    diagnostics so operators can wire real MCP hosts without hand-deriving
    OAuth/discovery settings
  - explicitly labeling the connection workspace as interactive-OAuth oriented,
    so operators do not mistake the current fixed-client templates for support
    of headless `client_credentials` hosts or CI-style remote MCP consumers
  - allowing the Console client-registration flow to submit multiple redirect
    URIs, so multi-callback hosts such as VS Code can be registered directly
    instead of remaining permanently stuck in a "missing redirect URI" state
  - fixing MCP Console card/table layout containment on narrow viewports so the
    admin surface keeps horizontal scrolling inside dense tables instead of
    forcing whole-page overflow on mobile-sized screens
  - advertising PKCE / public-client metadata and returning explicit
    `WWW-Authenticate` insufficient-scope challenges so MCP hosts can react to
    scope failures more reliably
  - including the authorization server issuer on OAuth authorization-code
    redirects for stronger client-side mix-up protection
  - preventing revoked MCP clients from being mutated through the governance
    API and normalizing legacy admin sessions into auditable admin actors for
    MCP control-plane audit logs
  - aligning the Console detail workspace with revoked-client reality by
    marking revoked clients read-only, removing “ready to connect” guidance,
    and stopping the UI from offering copyable host snippets that can no
    longer complete OAuth

- **Backup and restore drill with decryptability verification (#151).** A
  repeatable, backup-tool-agnostic drill that verifies an already-restored
  PostgreSQL database is actually recoverable — without production traffic.
  Includes:
  - New `attune restore-drill run --target <url> [--baseline-url <url>]
    [--backup-ref <s>] [--record]` CLI command, and `attune restore-drill
    status` for audit retrieval.
  - Verifier battery (`internal/restoredrill`): connectivity, schema/migration
    state (reuses checksum + manifest + dirty verifiers, with version-skew
    awareness — an older backup warns rather than fails), pgvector extension +
    sample similarity query, row counts vs. a live baseline, and **full-population
    Tink decryption of every real managed secret** (LLM credentials with AAD
    binding, and the two-level webhook/email inbound envelopes) — failing loudly
    on keyset/restore drift. No sampling, so drift cannot hide in an unchecked
    row. Decrypted plaintext is never logged or reported.
  - Whole-population key guard: every distinct `llm_channels` key id must be
    resident AND enabled in the live keyset (a fast pre-check before the
    full-population decryption); the report states decrypted-of-total counts.
  - Opt-in `--deep` tier: index validity + amcheck `bt_index_parent_check`
    B-Tree structural verification. Plus `--warn-exit` and structured logging.
  - Recovery objectives: `--backup-taken-at` / `--restore-duration` measure RPO
    (data-loss window) and RTO (restore time), graded against `--rpo-target` /
    `--rto-target` SLAs by a `recovery_objectives` check (warns when breached),
    persisted as `rpo_seconds`/`rto_seconds` for audit. `attune restore-drill
    history` shows the trend; `attune_restore_drill_last_rto_seconds` exposes it.
  - Push-button restore: `--restore-from <file> --admin-url <url>` provisions an
    ephemeral database, restores the backup into it (`psql` for plain SQL,
    `pg_restore` for custom/dir/tar — password passed via `PG*` env, never argv),
    auto-measures the RTO, runs the full battery, and tears the ephemeral DB
    down. Requires `psql`/`pg_restore` in the runtime.
  - Pre-restore artifact verification: `attune restore-drill verify-backup <dir>`
    runs `pg_verifybackup` against a `pg_basebackup` directory, catching a
    corrupt/incomplete backup before any restore.
  - Broad silent-restore-failure detection: `constraints` (baseline-relative —
    restore-introduced `NOT VALID` constraints), `sequences` (serial/identity
    sequence behind its column max → next insert collides), `encoding` (non-UTF8
    target corrupts multibyte text), `materialized_views` (unpopulated after
    restore), and `extensions` (baseline-relative — extension lost in restore).
  - Structured JSON `DrillReport` suitable as audit evidence, recorded
    (append-only) to the new `restore_drill_runs` table (migration 072).
  - New preflight check `backup:restore_drill` (new `backup` category) surfaces
    the latest drill result in `attune doctor` and the Console System Readiness
    page, graded by recency.
  - Server-side derived metrics `attune_restore_drill_last_success_timestamp_seconds`
    and `attune_restore_drill_runs_total{status}` (read from `restore_drill_runs`
    at scrape time), an `AttuneRestoreDrillStale` alert + runbook, and an
    opt-in Helm CronJob (`restoreDrill.enabled`) for in-cluster drills.

- **Migration checksum ledger and integrity verification (#150).** World-class
  migration tracking for production deployments, matching Flyway/Prisma/Atlas
  capabilities. Includes:
  - SHA-256 checksum verification for all applied migrations (drift detection)
  - Manifest hash for reordering detection (linear hash chain, Atlas pattern)
  - Dirty state detection with two-phase write (success=FALSE marker)
  - Duplicate numeric prefix detection at startup (fails before any apply)
  - Execution metadata: `duration_ms`, `applied_by` (binary version), `success`
  - CLI commands: `status`, `verify`, `dry-run`, `repair`, `baseline`
  - Prometheus metrics: `attune_migration_*` (applied, duration, pending, drift)
  - Grafana dashboard panels and alert rules for migration monitoring
  - New preflight check: `migration:integrity` (checksums + duplicates)
  - CI lint script: `scripts/lint-migrations.sh` (naming, no-tx validation)
  - Recovery procedures documented in `docs/private-deploy.md`
  - Renumbered MCP migrations 058-062 to 065-069 to eliminate prefix collisions

- **Production readiness preflight (#149).** New `attune doctor` CLI command and
  `/fb/v1/console/system/preflight` HTTP endpoint that run 11 production
  readiness checks (config, database connectivity, pgvector, migration state,
  Tink keyset, session key, OIDC reachability, metrics registry, enricher
  workers). Output follows IETF health-check semantics (pass/warn/fail) with
  actionable remediation text. Includes Console "System Readiness" admin page
  with collapsible category cards and status indicators.

- **Terminal enrichment failure visibility (#81).** Operators can now filter and
  manually retry feedback rows that have exhausted the enrichment retry budget
  (5 attempts). Includes:
  - New `enrichment_status` and `terminal_failed_only` query filters on
    `GET /fb/v1/console/feedback`
  - `enrichment_attempts` and `enrichment_next_retry_at` metadata on list/detail
    responses
  - `POST /fb/v1/console/feedback/{id}/retry-enrichment` endpoint to reset a
    terminal-failed row for re-enrichment (409 if row is not in failed status)
  - `retry_enrichment` audit action
  - Console: terminal failure count in stats panel with red visual tone
  - Console: "终态失败" queue mode filter and header summary pill
  - Console: red left border on terminal failure rows in list view
  - Console: visual retry progress indicator (5 dots showing attempts/max)
  - Console: error message block with copy-to-clipboard button
  - Console: batch delete dialog for permanent removal of terminal failures

- **MCP (Model Context Protocol) server with OAuth 2.1 (#93).** Adds a
  full MCP server implementation enabling AI agents to interact with
  attune feedback data. Includes:
  - OAuth 2.1 Authorization Server with PKCE (S256 required)
  - JWT access tokens (HS256) with configurable TTL
  - JSON-RPC 2.0 over Streamable HTTP transport
  - 10 MCP tools: `list_feedback`, `get_feedback`, `list_workflow_states`,
    `get_workflow_state`, `list_tags` (mcp:read scope); `update_workflow_state`,
    `add_tag`, `remove_tag`, `set_urgent` (mcp:write scope); `submit_feedback`
    (mcp:ingest scope)
  - OAuth client management via database tables (`mcp_oauth_clients`,
    `mcp_oauth_codes`, `mcp_oauth_refresh_tokens`, `mcp_sessions`)
  - Discovery endpoint at `/.well-known/oauth-protected-resource`
  - New scopes: `mcp:read`, `mcp:write`, `mcp:ingest` (write implies read)
  - Audit actions for all MCP operations

- **Configurable enrichment prompt policy (#107).** Enrichment prompt selection
  now resolves through a typed policy layer with output-language, title/rationale
  length, display-field, tone, and domain-guidance knobs; canonical
  `prompt_version` identities; legacy custom-template compatibility validation;
  locale-aware default prompt previews; Console-visible policy/contract metadata;
  immutable saved-version snapshots with rollback activation; and semantic-run
  provenance tied to the active prompt version snapshot. The Console rollback
  flow now requires a diff-confirmation step, guards unsaved drafts from
  background refreshes, drops stale preview responses, exposes load failures
  with retry, and keeps audit snapshots/role permissions aligned with the new
  policy fields. The active version pointer is constrained to the owning tenant,
  and legacy direct config writes clear the active pointer instead of leaving
  stale rollback provenance. Off-list eval suggestions now run through an
  explicit POST analyze action instead of GET, and promote-suggested audit
  failures are treated consistently with other enrich-config mutations. Prompt
  policy responses and version-history snapshots now expose resolved prompt and
  schema fingerprints so Console history can be correlated with semantic-run
  provenance.


- **`AttuneOutboxDeadRowsHigh` alert (#32).** The `attune_outbox_dead_rows`
  gauge was dashboard-only; a webhook deleted on the destination side (a common
  self-service action for Discord/Slack channel webhooks) silently piled
  terminal-failed deliveries into the dead queue with no alert. A new alert
  fires when any delivery sits in the `dead` state for 15m, with a runbook
  pointing the operator to Console > Dead deliveries.

- **Discord webhook outbound adapter (#32).** A new `discord` destination type
  delivers per-event and daily-digest notifications as Discord embed objects
  (`internal/outbound/adapter/discord/`). The embed color is keyed off the
  enrichment severity (`critical`→red, `major`→orange, `minor`→yellow, with a
  gray fallback for tenant-custom taxonomies); `is_urgent` overrides to red.
  Built end-to-end on the #31 framework: outbox routing, Console CRUD, config
  validation (`destination_type: discord`, secret optional — the webhook URL is
  the credential), the registry-driven Test button, and a `058` migration
  widening the `destination_type` CHECK. A `checkDiscord` response checker
  treats Discord's `204 No Content` as success and 408/429 as retryable; all
  embeds ship `allowed_mentions:{parse:[]}` so user content can never trigger an
  `@everyone`/`@here` ping, and every field is rune-safe truncated to Discord's
  embed limits (title 256, description 4096, 6000 total). A malformed upstream
  timestamp is dropped rather than passed through (Discord 400s on a bad
  `embed.timestamp`), and the daily-digest path renders both clustered themes
  and a recent-items fallback.

- **Slack outbound adapter wired into delivery pipeline (#31).** The existing
  Slack Block Kit adapter (`internal/outbound/adapter/slack/`) is now
  delivery-reachable end-to-end: outbox routing creates rows for `slack`
  (and `lark`) destination types, Console CRUD validates and accepts the new
  types, `notify.TestSend` uses the adapter registry instead of a hardcoded
  switch, `CustomWebhookDest` config accepts a `destination_type` field
  (defaulting to `raw-webhook`), and the audit snapshot hashes the URL for
  signing-less destinations (Slack incoming webhooks have no request signing).
  The adapter renders both per-event and daily-digest Block Kit messages with
  severity/category fields from the enrichment pipeline. A custom
  `checkSlack` response checker classifies 408/429 as retryable and 4xx as
  terminal (replacing the generic `CheckWebhook`). Config validation
  rejects unknown `destination_type` values (typo guard) and skips the
  shared-secret requirement for Slack and Lark (the URL is the credential).
  Response bodies are capped at 1 MiB via `io.LimitReader` for both
  `Transport.Send` and `TestSend`. Block Kit header/section text is
  truncated to Slack's hard limits (150/3000 chars, rune-safe).

### Changed

- **Console audit log page UX overhaul (#152).** Brings the audit log page to
  industry-leading quality (Datadog / WorkOS / Clerk level):
  - Replaced hero header (gradient, badge, 2.3rem title, subtitle) with a
    compact single-line title bar + action buttons (~300px → ~48px)
  - Simplified filter panel header: removed eyebrow label, description text,
    gradient background; title now inline with toggle
  - Removed sync status bars ("当前输入已经和结果列表同步") from both expanded
    and collapsed filter states — no longer exposes internal draft/commit
    architecture
  - Collapsed filter empty state now conditionally rendered: hidden when no
    filters active, shows count chip only when filters are set
  - Replaced investigation workspace (~460 lines) with compact toolbar (~50
    lines): inline avatar + action dot + description + position counter
  - Removed stat cards (loaded count, filters, latest event, actions) and
    floating help pill from header area
  - Removed stream section header (eyebrow, title, description, count badge,
    scope area with date range/events/bursts chips)
  - Event card selection: "当前聚焦" chip → left border accent with subtle
    background tint
  - Change-path chips on event cards now hover-only (hidden → visible on
    group hover)
  - Cleaned up ~790 lines of unused code (components, state, helpers)
  - Updated tests: removed 6 workspace-specific tests, updated 3 tests for
    new compact UI format (664 tests passing)

- **Source vocabulary is now registry-driven (#95).** Adding an inbound channel
  no longer requires editing a hardcoded map in the core `domain` package — the
  valid source set and its display labels are assembled once at startup from
  `domain.CoreSources` (the never-an-adapter sources `api`/`web`/`mcp`/`other`)
  unioned with each self-registered inbound adapter's channel, and injected as an
  immutable `domain.SourceSet` into the ingest validator, the
  `attune_ingest_total` source-label bound, and the guard-policy target
  validator. Adapters now declare their human label at the registration site
  (`inbound.Register(channel, display, factory)`), and the resolved label travels
  on the outbound envelope as an additive `source_display` field (the
  github-issue renderers fall back to the existing label shim for older queued
  rows). `CoreSources` keys are reserved: an adapter channel that collides with
  one is a fatal boot error (the assembly returns an error that propagates to the
  process exit, so the collision/duplicate guards are unit-testable) rather than
  a silent shadow. The reserved core map is unexported behind `domain.CoreSources()`
  / `IsReservedSource` so it can't be mutated by an importer, and `inbound.Register`
  now rejects a nil factory, empty channel, or empty display at the call site. The
  source-validation error now lists the valid sources, and the guard-policy channel
  error names the offending value. `domain.ValidSources` is removed;
  `domain.SourceDisplayName` is retained as the permanent read-path fallback. No new source values are
  introduced and the existing six are preserved verbatim (the source string is an
  append-only storage + wire token).

- **Console navigation now uses a production-style app shell (#144).** The
  authenticated Console moved from a flat top navigation plus oversized
  `Settings` page to a grouped sidebar/drawer shell with canonical homes for
  Feedback, Analytics, Configuration, Integrations, and Administration. Legacy
  routes such as `/settings?section=...`, `/usage`, `/llm-usage`, `/llm-config`,
  `/api-keys`, `/inbound-sources`, `/notify-targets`, `/guard-policies`,
  `/outbox-dead`, and `/clusters` now redirect to the new canonical paths
  (for example `/analytics/usage`, `/configuration/llm`, and
  `/feedback/clusters`). The shell also now wires a real theme provider and
  toggle instead of shipping dormant dark-mode-only tokens. The tenant members
  surface was also upgraded from a flat CRUD table into a governance-oriented
  page with active-member vs pending-invite separation, search/filter controls,
  admin continuity guardrails, and explicit pending-invitation revocation. The
  feedback workbench was likewise rebuilt around an operator layout: an
  overview hero, dedicated filter rail, clearer monthly signal summary, a
  stronger work queue, and a sticky focus panel that keeps batch-selection and
  active scope visible while triaging. This overhaul now also applies a shared
  hero/metric language to weak Console surfaces such as Usage and Clusters,
  replacing giant empty slabs and low-signal cards with clearer top-of-page
  state summaries, denser filter controls, and more intentional empty states.
  The semantic-clusters flow is now also wired end-to-end: when clustering is
  disabled, `/feedback/clusters` points operators directly to the digest
  settings surface that controls the tenant-level clustering flag, and that
  settings page now explicitly explains its impact on the clusters workspace so
  the feature no longer reads like a dead-end disabled module.
  The shared authenticated shell now keeps the desktop navigation rail in its
  own scroll container, so long settings menus stop dragging the full page
  when operators move through administration-heavy surfaces such as GDPR.
  Workflow, tagging, classification, and enrichment-runtime pages now follow
  the same production-style pattern, pairing concise page-level metrics with
  governance guidance instead of shipping raw form stacks as the primary UI.
  Integration and administration surfaces such as API keys, inbound sources,
  notify targets, and guard policies now use the same operational layout, so
  teams get consistent summaries, ownership cues, and runbook-style guidance
  before acting on a live control surface. GDPR and LLM-cost views were also
  upgraded into the same information hierarchy so high-risk request handling
  and spend analysis no longer fall back to flat legacy admin layouts. The
  audit-log surface now behaves like an actual investigation workbench rather
  than a card list: operators get quick presets, full target-id filtering, a
  local spotlight search over the loaded slice, grouped day buckets, sharable
  URL-backed investigation state, one-click narrowing by action/actor/target,
  history-aware detail navigation, removable active-scope chips for both server
  and local search constraints, keyboard-friendly focus/open shortcuts, inline
  current-focus browsing across the visible stream, a new "current slice
  signals" layer that surfaces repeated actions/actors/targets/change-paths as
  one-click facets, explicit "why this matched" hints whenever a local
  spotlight is active, clickable field-path chips that can relaunch the local
  spotlight directly from either the stream or the detail drawer, compact-by-
  default filter and signal surfaces so the event stream stays above the fold,
  clearer absolute timestamps inside repetitive rows, a richer investigation
  workspace around the currently selected event, a current-scope strip that
  explains what slice is on screen, burst summaries that call out adjacent
  repeated activity before operators read each row, compact follow-on rows
  inside repeated bursts so long runs stop reading like a wall of duplicate
  cards, redundant burst actions that disappear once the current scope already
  matches them, burst-aware workspace controls that show where the current
  event sits inside a repeated run, one-click jump/expand controls for that
  run, and inline reset affordances that let operators widen the investigation
  without hunting back through the filter rail, plus a dedicated detail drawer
  that surfaces request metadata and field-level change summaries without
  forcing users to expand one bulky row at a time. The
  feedback landing page was then tightened again into a true list-first
  triage workspace: the oversized hero was removed in favor of a compact
  header plus inline queue metrics, search/filter controls now live in a
  single operator toolbar, live scope moved to a side rail, and the feedback
  rows themselves were flattened so the queue reads like a serious workbench
  instead of a dashboard made of stacked cards. The empty queue now ships a
  structured first-run onboarding state with direct links to API-key and
  classification setup, while feedback-list failures render a distinct,
  retryable error surface instead of degrading into a misleading "no data"
  view. Filtered zero-result views are also now distinguished from first-run
  onboarding, so narrowing the queue no longer falsely implies that feedback
  ingestion has not been configured. Queue rows and the right-side detail sheet
  now also surface enrichment readiness more clearly: list rows show explicit
  classification-status pills, and detail views render a dedicated failed /
  pending AI-state banner instead of collapsing low-signal feedback into empty
  cards. The detail sheet's lower half is now organized as an actual case
  workspace, separating operator actions (tags, workflow) from supporting
  context (source facts, metadata, audit trail) so the panel reads like a
  usable work surface rather than a stack of generic sections. The upper half
  now also behaves more like a case overview: AI state and workflow status are
  surfaced directly in the summary block, and raw content / classification now
  share a tighter side-by-side review layout on wider screens. Workflow
  transitions and audit history inside the sheet were also upgraded from thin
  inline controls into denser operator surfaces, with clearer next-state
  guidance, structured comments, and more readable old→new change records. The
  queue itself now reads more like an operational workbench too: list content
  stays as the primary surface while a dedicated right-side rail keeps current
  scope, queue health, and next-step guidance visible during triage. The list
  header now also exposes recommended triage lanes, quick scopes, and removable
  active-filter chips so operators can both narrow and unwind queue state
  without leaving the main work surface. Queue order itself is now an explicit
  triage control too, with switchable newest / urgent / in-progress modes for
  the same visible set of feedback rows. Operators can now also drop the
  currently loaded queue into local subqueues such as urgent, in-progress,
  failed enrichment, and AI-ready, so they can tighten the visible work surface
  without firing a fresh server-side filter roundtrip or losing the broader
  filter context. When one of those local subqueues has zero matching rows, the
  workbench now renders a dedicated local-empty recovery state instead of
  degrading into a blank queue body, so operators can step back to the broader
  subqueue or clear the full filter stack intentionally. The queue's
  recommendation banner, direct-action block, and triage playbook now also
  react to the active local work surface itself, so switching into urgent,
  in-progress, failed, or AI-ready lanes no longer leaves stale generic advice
  on the page. The right-side queue rail now also summarizes the current work
  surface as explicit posture and AI-health signals instead of only showing raw
  counters, the AI-ready lane now uses a stricter “usable AI output” definition
  rather than any partial enrichment artifact, and runtime-remediation links are
  only surfaced when the current queue truly looks like a configuration/runtime
  problem instead of appearing on every lane that merely contains a failed row.

- **Code duplication check now excludes test files (#81).** The jscpd duplication
  gate (< 5%) now excludes `*_test.go`, `testdata/`, and `test/` via a
  `.jscpd.json` config file. Test fixtures commonly have acceptable duplication
  (similar setup patterns), and production code alone is at 3.4%. This follows
  industry practice and focuses the gate on shipping code quality.

### Security

- **Dead-queue console surface no longer leaks token-in-URL webhook
  credentials (#32).** The outbox dead-queue API (`ToProto`) returned the full
  `destination_target` URL — and any URL echoed into `last_error` /
  `dead_reason` via a `*url.Error` — verbatim to operators. For Slack/Lark/
  Discord the token lives in the URL path, so an operator viewing a failed
  delivery saw the credential in clear text. All three fields are now redacted
  to scheme://host at the read boundary (`nethardening.RedactURL` /
  `RedactURLIn`); the full URL is still stored at rest for redelivery.

- **`notify.TestSend` no longer leaks the webhook URL in transport errors
  (#32).** A connection/DNS/TLS failure returned the raw `*url.Error` (which Go
  formats with the full request URL) to the API response and audit log. The
  returned error is now scrubbed with `nethardening.RedactURLIn`.

- **Audit snapshot no longer leaks token-in-URL webhook credentials (#32).**
  `auditNotifyTargetSnapshot` chose between a hashed URL and a path-preserving
  "sanitized" URL based solely on whether a `secret` was set. For destinations
  whose token lives in the URL path (Slack/Lark/Discord incoming webhooks), an
  operator who also supplied an optional `secret` would get the full webhook
  token written to the audit log in clear text. The snapshot now keys on the
  destination type (`notifytarget.URLIsCredential`) and always hashes the URL
  for token-in-URL destinations regardless of any secret. Fixes a latent leak
  that also affected the existing Slack/Lark types.

- **Slack webhook URL redacted in logs (#31).** The adapter now logs
  `nethardening.RedactURL(dst.URL)` (scheme + host only) instead of the
  full incoming-webhook URL, which contains the authentication token in
  the path segment.
- **Slack mrkdwn injection prevented (#31).** User-supplied content
  (title, severity, category, source, digest theme/item titles) embedded
  in mrkdwn fields is escaped (`<` → `&lt;`, `>` → `&gt;`, `&` → `&amp;`)
  to prevent `<!channel>` / `<!here>` mention injection and unintended
  link markup.

### Fixed

- **Dimensions editor lost new/persisted identity on remount, and the
  Settings page silently dropped unsaved drafts (#90).** Row identity and
  the "new (identifier editable) vs persisted (locked)" flag were tracked
  in component-instance `useRef` (`WeakMap` + `Set`), so any genuine editor
  remount reclassified an unsaved dimension as persisted and locked its
  Name. The stated repro actually tripped a second bug: the Classification
  Settings route blind-synced `dimensions` from the query cache on remount
  and on every `refetchOnWindowFocus`, discarding in-progress edits. Both
  are fixed by moving identity into the working data — a client-only `_key`
  + `_isNew` (minted once at row creation, stripped before the wire) shared
  by the Dimension and Taxonomy layers via `src/lib/editable-rows.ts` — and
  by seeding the edit model once (gated child + `useState` initializer)
  instead of a `useEffect` re-sync. Also fixes duplicate DOM ids on
  empty/duplicate names, the divergent `readOnly`/`disabled` lock semantics
  (now uniformly read-only), the value-keyed urgent-set chips, and adds
  focus management on add (WCAG 2.4.3). See
  `docs/proposals/2026/06/2026-06-22-dimensions-editor-identity-remount.md`.

- **Batch delete API body was double-stringified (#81).** The
  `useBatchDeleteFeedback` hook passed an already-stringified JSON body
  to the API client, which stringified it again. Fixed to pass the object
  directly.

- **Parallel test race in `internal/notify` (#81).** `ResetForTest()`
  cleared the entire outbound adapter registry, causing random failures
  when parallel tests interleaved. Added `UnregisterForTest(id)` to remove
  only the specific test adapter, making parallel tests safe.

- **Lark adapter `truncate` is now rune-safe (#32).** It used byte slicing
  (`s[:n]`), which split multibyte UTF-8 mid-rune — corrupting CJK/emoji titles
  and risking an upstream 400 — and appended `"..."` past `n`. Replaced with the
  rune-safe version already used by the Slack/Discord adapters (found while
  reviewing the Discord copy-adapt for shared-helper drift).

- **`outbound.ErrTerminal` now recognised by transport retry loop (#31).**
  `outbound.ErrTerminal` and `notify.ErrTerminal` were separate
  `errors.New` sentinels (depguard prohibits the import). The outbox
  worker's `wrapCheck` bridge now translates `outbound.ErrTerminal` →
  `notify.ErrTerminal` so that adapter-terminal responses (e.g. Slack
  403) stop retrying immediately instead of exhausting all attempts.
- **Outbox envelope field mapping fixed for Slack adapter (#31).**
  The outbox serialises `title`/`is_urgent` inside `feedback.enriched`
  and `severity`/`category` inside `enriched.attrs`, but the adapter
  read them from the flat `feedback` level. The adapter now probes
  both paths. Timestamp (`delivered_at` → `timestamp`) and `tenant_id`
  (nested → top-level) are also fixed up during unmarshal.
- **Digest view JSON roundtrip aligned with upstream types (#31).**
  Local digest structs used lowercase json tags while upstream
  `digest.Result` / `feedback.DigestWindowStats` / `digest.Theme`
  use Go-default capitalised field names (no json tags). The roundtrip
  in `toDigestView` silently dropped all nested data. Tags removed so
  both sides marshal identically.
- **`truncate()` no longer exceeds the requested limit (#31).**
  The ellipsis suffix was appended beyond `n`, producing `n+3` chars.
  Now stays within `n` by reserving 3 chars for `"..."`.

- **Eval report now surfaces off-list module suggestions (#83).** When the LLM
  systematically suggests taxonomy values that are filtered out (e.g., tenant
  whitelist is `["payment"]` but LLM outputs `["payment", "checkout"]`), the
  eval report now captures these suggestions with frequency distribution,
  coverage metrics, confidence scoring, and actionable recommendations. This
  enables operators to identify systematic gaps in their taxonomy and evolve
  it based on LLM behavior. CLI output includes a "suggested values" section
  with per-dimension coverage and top suggestions. Console API endpoints
  `GET /enrich-config/eval-suggestions` and `POST /enrich-config/promote`
  allow viewing suggestions and promoting values to the taxonomy directly.
  The Console settings → classification tab adds a "Suggested values" panel:
  an on-demand "Analyze" action runs the eval, shows per-dimension coverage,
  lists each off-list candidate with its frequency, confidence, and predicted
  coverage gain, and a one-click "Promote" adds it to the dimension taxonomy
  (after which it leaves the list and coverage rises). Analyze + Promote are
  gated behind edit permission, matching the admin-only server routes, so a
  view-only member sees the data scope without triggering a 403. Suggested
  candidates are ordered deterministically (confidence desc, then dim + value)
  so equal-frequency values don't reshuffle between eval runs. Hardening: the
  eval-suggestions endpoint is per-tenant rate limited (6/min) since each call
  runs an LLM eval; the accumulator counts each off-list value once per row;
  candidate counts are clamped on the int32 wire narrowing; the panel shows
  "<1%" for a nonzero confidence/impact that rounds to 0; and the promote
  button has a synchronous re-entrancy latch so a double-click sends one POST. Promote input is canonicalized (values trimmed;
  whitespace-only rejected as 400, trimmed-duplicate as 409, missing display
  name defaulted to the value) and domain-validation failures map to 4xx
  instead of 500.

### Fixed

- **Audit writes for promote-suggested and API-key rotation no longer silently
  dropped (#83).** Two emitted audit actions were missing from both the Go
  allow-list (`internal/service/auditlog/actions.go`) and the DB
  `chk_audit_action_value` constraint, so `auditlog.Service.Record` rejected
  them and the handlers — which log the error but still return 200 — left no
  audit trail: `enrich_config.promote_suggested` (taxonomy promotion, #83) and
  `api_key.rotate` (emitted since key rotation shipped; a security-observability
  gap where rotations went unaudited). Both are now registered in lockstep
  (migration `057_eval_promote_audit_action.sql`), and a router cross-check test
  asserts every emitted audit action is allow-listed so this class of bug fails
  CI instead of production. Found via the full-stack e2e for #83.

- **GDPR erasure no longer aborts on subjects with deliveries (#131).** The
  data-subject delete now purges `notify_outbox` (a `NOT NULL` FK to
  `user_feedback` with no `ON DELETE` action, whose `payload` holds the feedback
  PII) inside the erasure transaction. Previously, deleting any subject who had
  ever had a notification raised a foreign-key violation that rolled back the
  whole erasure (a GDPR Art.17 + availability bug); residual PII in the delivery
  envelope is now removed too. The deletion record reports an `OutboxCount`.
  
### Fixed

- **Malformed workflow-state id now returns 400, not 500 (#36).** `PATCH`/`DELETE
  /v1/workflow/states/{id}` passed the raw id straight to a UUID-typed column, so a
  non-UUID id (reachable via the SDK admin surface) produced a Postgres 22P02 →
  opaque `500 INTERNAL` that the SDKs then retried (PATCH/DELETE are idempotent).
  Both `UpdateState` and `ArchiveState` now `uuid.Parse`-guard the id and forward
  the **canonical** form (so a `urn:uuid:…` that `uuid.Parse` accepts but Postgres
  rejects can't slip through), returning `400 BAD_ID` for anything invalid.

- **Self-loop / duplicate workflow transitions now return 400, not 500 (#36).**
  `ReplaceTransitions` validated state ownership but not `from == to` or repeated
  edges, which tripped the DB `chk_wt_no_self_loop` / unique-edge constraints and
  surfaced as `500 INTERNAL` (reachable with an arbitrary SDK payload). Both are
  now rejected up front as `400 VALIDATION`.

- **Tag create/update constraint violations now return 400, not 500 (#36).** An
  empty/over-long name or malformed color reached a DB `CHECK` constraint and was
  misclassified as `500 INTERNAL`; the repo now maps SQLSTATE 23514 (via a new
  `pgxutil.IsCheckViolation`) to a `400 VALIDATION` envelope.

- **Go SDK: a truncated/reset response body now retries (#37).** `doOnce`
  discarded the body-read error, so a mid-stream connection reset on a 2xx became
  a permanent `INTERNAL` decode error instead of a retryable `NETWORK` failure;
  it is now surfaced as `NETWORK` so an idempotent request retries.

- **Go SDK: reserved headers stripped from `WithDefaultHeaders` (#37).** Matching
  the Node hardening, the reserved headers (Content-Type, X-API-Key,
  Idempotency-Key, User-Agent) are dropped (canonical, case-insensitive) at the
  construction boundary so a default header can't override them — including
  `Content-Type` on bodyless GET/DELETE requests.

- **SDKs reject dot-segment / slash ids (#37).** `archiveTag('..')` /
  `updateWorkflowState({id:'../x'})` and friends are rejected before sending
  (`encodeURIComponent`/`url.PathEscape` leave `.`/`..` untouched, which would
  walk the request path); empty, `.`, `..`, and slash-containing ids now fail
  fast as `BAD_REQUEST` in both SDKs.

- **Node SDK: reserved headers can no longer be overridden via `defaultHeaders`
  casing (#37).** A `defaultHeaders` entry whose key was a case-variant of a
  reserved header (e.g. `content-type` vs the canonical `Content-Type`, or
  `x-api-key`) survived as a distinct object key and got *concatenated* by the
  WHATWG `Headers` constructor into a malformed header — silently overriding the
  documented "reserved headers always take precedence". Reserved keys are now
  stripped case-insensitively at construction.

- **Node SDK: baseURL is validated at construction (#37).** A malformed,
  non-`http(s)`, or host-less `baseURL` (e.g. `http:foo`, which `new URL` would
  coerce to host `foo`) now throws `BAD_REQUEST` immediately — requiring a real
  `scheme://host` authority for parity with the Go SDK, instead of constructing
  successfully and silently sending to the wrong host.

- **SDK retry parity: fractional `Retry-After` (#37).** The Node SDK accepted a
  fractional `Retry-After: 1.5` (→1500 ms) while the Go SDK rejected it and fell
  back to backoff; Node now treats only integer delta-seconds (RFC 9110) as a
  delay, keeping the two SDKs in lockstep.

### Security

- **Per-API-key rate limit now also covers the tag/workflow admin surface (#36).**
  A key's `rate_limit_rpm` is the key's overall request budget, but it was only
  enforced on `/v1/feedback/ingest`; the API-key tag/workflow routes
  (`/v1/tags`, `/v1/workflow/...`, added in this release) applied no limiter, so
  a rate-capped key could mutate tags/workflow config without bound (DB-write /
  audit-log amplification). The existing `PerKeyLimiter` middleware is now also
  mounted on the admin group, returning `429 RATE_LIMITED` + `Retry-After`. Keys
  without an rpm are unaffected. The per-tenant ingest limiter remains
  ingest-scoped by design.

- **Per-API-key rate limiting is now enforced (#41).** `external_api_keys.rate_limit_rpm`
  was stored and shown in the console but never enforced — only a per-tenant
  limit applied, so a leaked/abused key could consume the whole tenant's ingest
  budget. A new per-key token-bucket limiter (`ratelimit.PerKeyLimiter`, mounted
  after auth on `/v1/feedback/ingest`) now caps each key at its own
  `rate_limit_rpm`, returning `429 RATE_LIMITED` + `Retry-After`. New metric
  `attune_apikey_rate_limited_total{tenant}` (dashboarded). Keys without an rpm
  are unaffected. **Behavior change:** keys that already have `rate_limit_rpm`
  set start being enforced.

- **API-key IP allowlist now works and fails closed (#41).** Two bugs: (1) any
  key with a non-empty `allowed_cidrs` failed every lookup — pgx couldn't scan
  the `inet[]` column into `[]string` (`cannot scan _inet`), so the allowlist
  feature was effectively non-functional; fixed by selecting
  `allowed_cidrs::text[]`. (2) the allowlist check was skipped when the resolved
  client IP was empty (fail-open); it now fails closed — an empty/unresolvable
  client IP with an allowlist configured is rejected (`ErrIPNotAllowed`), not
  bypassed. Covered by a Postgres integration test (in-range accepted;
  out-of-range / empty / unparseable rejected).

- **Node SDK does not follow HTTP redirects (#37).** `@phixsura/attune` issues
  ingest requests with `redirect: "manual"`, so a compromised or misconfigured
  endpoint can't 3xx-redirect the request and have `fetch` re-send the
  `X-API-Key` header to an attacker host (credential leak). A 3xx now surfaces as
  an `AttuneError` instead of being followed.

- **Node SDK input/response hardening (#37).** CR/LF in `apiKey` or a caller
  `idempotencyKey` is rejected up front as `BAD_REQUEST` (header-injection guard,
  and avoids retrying a deterministic config error); the response body is read
  under a 1 MiB cap so a hostile server can't OOM the client with an unbounded
  body.

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

- **OpenAPI now documents the ingest idempotency contract (#37).** The generated
  `docs/openapi/openapi.yaml` for `POST /v1/feedback/ingest` now declares the
  optional `Idempotency-Key` request header and the `409 IDEMPOTENCY_CONFLICT`,
  `413 BODY_TOO_LARGE`, and `429 RATE_LIMITED` responses — header-driven behavior
  the proto request/response types can't express. Done via `gnostic.openapi.v3`
  operation annotations on the proto (still generated, never hand-edited); adds
  the build-only `github.com/google/gnostic-models` dependency (blank-imported by
  generated Go to register the extension; no runtime use).

- **Oversized ingest body now returns `413 BODY_TOO_LARGE` (#37).** `POST
  /v1/feedback/ingest` previously returned `400 BAD_REQUEST` for a body over the
  64 KiB cap; it now returns `413` with code `BODY_TOO_LARGE`, matching the rest
  of the HTTP API. A malformed (but in-size) body is still `400`.

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

- **Tag + workflow CRUD over the API-key surface, with Go + Node SDK methods (#36, #37).**
  The tag and workflow-state config endpoints — previously console-session-only —
  are now also mounted under the API-key group (`/v1/tags`, `/v1/workflow/...`),
  scope-gated by `tags:read`/`tags:write` and `workflow:read`/`workflow:write`
  (scopes that were defined but enforced nowhere). The existing console handlers
  are reused via an apikey→AuthCtx adapter (`console.MountAPIKeyAdminRoutes`),
  with the actor audited as `apikey:<keyID>`. Both SDKs gain the matching
  methods — `listTags`/`createTag`/`updateTag`/`archiveTag` and
  `listWorkflowStates`/`createWorkflowState`/`updateWorkflowState`/
  `archiveWorkflowState`/`listWorkflowTransitions`/`replaceWorkflowTransitions`/
  `seedWorkflowDefaults` (Go uses the `Ingest*`-style PascalCase) — built on a
  generalized request core shared with `ingest`. The core preserves ingest's
  retry-safety contract: idempotent `GET`/`PUT`/`PATCH`/`DELETE` are retried on
  transient failure, but the non-idempotent `create*`/`seed*` `POST`s are not, so
  a lost response can't create a duplicate resource; path ids are URL-escaped.
  Both surfaces are verified by real-server e2e, including scope-denied (403) and
  cross-tenant isolation. The server also now rejects a transition referencing a
  state outside the tenant with `400 VALIDATION` (was a potential cross-tenant
  reference / 500). Note: tag/state update is replace-semantics (send full
  state), and these endpoints expose admin config to scoped API keys.

- **Node/TypeScript client SDK `@phixsura/attune` (#37).** Published client for
  the ingest API under `sdk/node/`: `new Client({ baseURL, apiKey })` →
  `await client.ingest({ content, … })` returning the stored row `id`. ESM + CJS
  (tsdown), zero runtime dependencies, native `fetch`, Node 20+ and browsers.
  Request/response types are generated from the proto contract (a new ts-proto
  `buf.gen` target → `sdk/node/src/proto/`, guarded by the `proto-sync` gate) —
  never hand-written. Transactional await-throw model with a typed `AttuneError`
  (`code`/`status`/`requestId`) and a shared retry contract (408/429/5xx +
  network/timeout, never 400/409/422, `Retry-After`-aware, default 2 retries)
  that the Go SDK (#36) will adopt. Ships `examples/node-ingest` and
  `examples/browser-ingest`; `ingest:write` keys are documented as publishable
  browser-safe credentials. Follows SDK conventions: a versioned `User-Agent`
  (`attune-node/<version>`), a `defaultHeaders` option, a shipped `LICENSE`, and
  sourcemaps in `dist`. The package is publish-ready (`publishConfig` public
  access to npmjs + `prepack` build) and the e2e harness installs the packed
  tarball into a fresh project to verify ESM + CJS consumption against a live
  server. A `SDK Release` workflow publishes to npm on an `sdk-v*` tag with npm
  provenance (signed SLSA attestation via OIDC).

- **Go client SDK `github.com/Phixsura/attune/sdk/go` (#36).** Published client
  for the ingest API under `sdk/go/`: `attune.New(baseURL, apiKey)` →
  `c.Ingest(ctx, attune.IngestInput{Content: …})` returning the stored row `id`,
  `ctx` first arg always. A nested module (minimum Go 1.25). Like the Node SDK,
  the request/response wire types are **generated from the proto contract** (a
  second `buf.gen` target → the public `sdk/go/attune/v1` package, guarded by the
  `proto-sync` gate) and marshaled with `protojson`, so the wire shape is
  single-sourced from proto and never hand-maintained. Like the Node SDK, those
  generated types are part of the public surface: the root package re-exports
  `IngestRequest`/`IngestResponse`/`ErrorResponse`/`ErrorCode`, plus the retry
  helpers `IsRetryable`/`BackoffDelay`/`ParseRetryAfter` and `TransportErrorCode`.
  `IngestInput`/`IngestResult` remain thin ergonomic facades over the generated
  messages. This pulls `google.golang.org/protobuf` + `genproto` +
  `gnostic-models`, which set the Go 1.25 floor. Adopts the Node SDK's contract
  verbatim: a typed `AttuneError` (`Code`/`Status`/`RequestID`, switch on `Code`)
  and shared retry policy (408/429/5xx + network/timeout, never 400/409/422,
  `Retry-After`-aware, default 2 retries with bounded exponential backoff +
  jitter). Each call carries a stable, auto-generated `Idempotency-Key` (reused
  across retries) so a blind retry is deduped server-side; override with
  `WithIdempotencyKey`. `WithDefaultHeaders` adds custom headers (reserved
  headers always win). The full Node safeguard set is matched: 3xx responses are
  surfaced as errors rather than followed (no `X-API-Key` leak to a redirect
  target), CR/LF in `apiKey`/`idempotencyKey` is rejected up front, the response
  body is read under a 1 MiB cap, `AttuneError` carries the response `Headers`,
  idempotency-key generation never fails (a unique fallback when the crypto
  source is unavailable), and a negative `maxRetries` clamps to 0. Ships
  `examples/ingest-cli` (built in
  CI) and an e2e harness (`scripts/e2e.sh`) that boots a real server + Postgres
  and verifies live ingest, idempotency + concurrent dedup, real 401/validation
  errors, the example CLI, and an external `go.mod`-consumer import. A `SDK Go
  Release` workflow cuts a GitHub Release on a `sdk/go/vX.Y.Z` tag; the Go module
  proxy serves `go get …@vX.Y.Z` on demand.

- **Idempotent ingest via the `Idempotency-Key` header (#37).** `POST
  /v1/feedback/ingest` now honors an optional `Idempotency-Key` request header:
  a replay with the same key + body returns the original feedback id without
  inserting again; the same key with a different body is `409
  IDEMPOTENCY_CONFLICT`; a malformed key is `400 VALIDATION`. Dedup is enforced
  by a partial unique index on `user_feedback (tenant_id, idempotency_key)`
  (migrations 055–056) with `INSERT … ON CONFLICT`, so it is atomic even under
  concurrent retries (N simultaneous same-key requests collapse to one row). The
  index is built `CONCURRENTLY` (migration 056, via new non-transactional
  migration support) so deploying it does not lock ingest on a large table.
  Ingest now feeds `attune_idempotency_key_usage_total{outcome}`
  (new/cache_hit/conflict), so the Operations "Idempotency" dashboard covers
  single-row ingest, not just batch. A background pruner releases idempotency
  keys older than 48h (NULLing the columns) so the partial index stays bounded
  to the recent retry window. The Node SDK sends a per-call key automatically, held stable across retries, so a
  retried at-least-once delivery cannot create a duplicate feedback row.

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
