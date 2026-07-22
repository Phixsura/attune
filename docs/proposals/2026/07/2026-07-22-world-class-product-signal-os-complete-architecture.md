<!-- markdownlint-disable MD013 -->

# World-Class Product Signal OS Complete Architecture

| Field | Value |
|---|---|
| Issue | Complete target-state design following [#228](https://github.com/Phixsura/attune/issues/228); implementation issues are delivery containers, not scope reductions |
| Status | Accepted |
| Started | 2026-07-22T14:30:49+08:00 |
| Related | [#202](https://github.com/Phixsura/attune/issues/202), [#214](https://github.com/Phixsura/attune/issues/214), [#221](https://github.com/Phixsura/attune/issues/221), [#226](https://github.com/Phixsura/attune/issues/226), [#228](https://github.com/Phixsura/attune/issues/228), [external sync framework](./2026-07-08-external-sync-framework.md), [GitHub Issues bidirectional sync](./2026-07-20-github-issues-bidirectional-sync.md), [public feedback platform gap analysis](../../../research/2026-07-13-public-feedback-platform-phase-2-roadmap.md) |

## Problem

The GitHub Issues bidirectional sync work gives Attune a strong delivery-sync
kernel: durable external object identity, provider webhooks, polling
reconciliation, comment dedupe, conflict records, Console diagnostics, and a
fully green CI/coverage gate for the implementing pull request. That is enough
to ship #228 as an integration capability, but it does not make Attune a
world-class Product Signal OS by itself.

Top-tier systems do more than move issue fields. Linear, Jira Product Discovery,
GitHub, Productboard, Canny, Aha!, Featurebase, UserVoice, Pendo, Unito, Exalate,
Sentry, Zendesk, and Intercom converge on a broader operating model:

- integration setup is installation-based, least-privilege, self-diagnosing,
  and recoverable without database access;
- delivery status is a graph that includes issues, pull requests, commits,
  projects, deployments, releases, and support/customer context;
- feedback portals close the loop with followers, status notifications,
  changelog updates, audience controls, widgets, custom domains, and analytics;
- prioritization uses account, segment, revenue, urgency, confidence, feedback
  breadth, and evidence quality rather than one flat backlog score;
- synchronization is field-owned, rule-driven, replayable, and observable at
  tenant scale;
- enterprise operators can prove readiness through SLOs, canaries, audit
  evidence, retention controls, and repeatable incident recovery.

Attune has credible foundations in several of those areas. The current gap is
not a missing single endpoint. The gap is that the product, integration, and
operations layers around the sync kernel are not yet deep enough to match the
best commercial systems.

This proposal defines the readiness program needed to close that gap without
turning one GitHub sync issue into an unbounded product rewrite.

## Goals / Non-goals

### Goals

- Define the complete top-tier capability baseline for Attune after #228.
- Define the final product, platform, data, API, security, operations, and
  verification contract as one complete architecture.
- Turn the industry gap analysis into concrete product and platform
  capability domains.
- Preserve the existing architecture: Customer Requests remain primary, the
  external sync framework owns provider synchronization, and public visibility
  surfaces use public-safe projections.
- Add clear acceptance criteria for each capability domain so "world-class" is
  judged by observable behavior, not confidence language.
- Identify data model extensions that will support multiple providers rather
  than GitHub-only one-offs.
- Define verification gates that include real browser operation, provider
  sandbox/live tests, fault injection, observability assertions, and remote CI.
- Keep privacy boundaries explicit: internal notes, private request metadata,
  and public portal comments do not move to external systems without explicit
  policy.

### Non-goals

- Do not reopen the #228 implementation scope. #228 remains the managed GitHub
  Issues sync slice.
- Do not claim production can be mathematically zero-risk. This proposal targets
  zero known blocker and zero unclosed pre-release risk within defined gates.
- Do not build a full CRM, support desk, project tracker, or ETL product inside
  Attune.
- Do not replace GitHub, Jira, Linear, or customer support systems as delivery
  systems of record.
- Do not add unrestricted two-way mirroring. Field ownership, comment
  ownership, and conflict behavior remain explicit.
- Do not store unnecessary raw provider payloads, raw credentials, webhook
  signatures, or private customer content.
- Do not merge the full target state into one pull request. Delivery should be
  split for review safety, but the complete scope defined here remains the
  product contract.

## Current Attune Baseline

### Strong foundations

Attune already has several foundations that many early feedback products lack:

- generated proto/OpenAPI/SDK contracts for Console and API surfaces;
- tenant-scoped Customer Requests with linked feedback, votes, account context,
  decision score, delivery issue links, and issue sync metadata;
- public portal submission, public board, public request detail, public voting,
  public comments, roadmap columns, similar-request suggestions, tenant-scoped
  public rate limits, and moderation;
- generic external sync framework with encrypted credentials, webhook secrets,
  mappings, object links, cursors, runs, attempts, failures, conflicts, event
  ledger, worker retry, and Console diagnostics;
- first-class Jira and GitHub issue providers using the shared framework;
- GitHub comment child records, managed backlink comments, webhook-triggered
  runs, polling reconciliation, and legacy outbound compatibility;
- audit log, audit evidence export, SSO/OIDC, break-glass governance,
  deploy/runtime smoke checks, Helm, dashboards, CI, CodeQL, dependency review,
  secret scanning, workflow linting, and local browser acceptance checks.

### Hard gaps

The remaining gaps are real and should not be blurred by green CI:

- GitHub connections are token-oriented; there is no GitHub App installation
  flow, repository picker, least-privilege installation health, or automatic
  webhook registration.
- Delivery context stops at issue-level data. Pull requests, commits,
  deployments, releases, GitHub Projects, Jira delivery progress, and related
  engineering artifacts are not unified into one request delivery graph.
- Public feedback loops lack followers, subscription preferences, user-facing
  status notifications, release/changelog updates, and a visible "what changed"
  trail.
- Public board maturity lacks duplicate merge/split, shared saved views, tag
  filters, trending score, custom access, custom domains, widgets, multi-portal
  routing, localization, and board analytics.
- Sync mappings are provider-aware, but not yet a mature rule engine with field
  transforms, conditional filters, dry-run previews, per-field conflict policy,
  and batch repair UX.
- AI support exists in enrichment and search, but not as a request-level insight
  engine for duplicate clustering, theme extraction, severity confidence,
  feedback-to-spec drafting, or operator review queues.
- Enterprise readiness lacks tenant-level sync SLOs, provider synthetic
  canaries, rate-budget dashboards, replay drills, incident runbooks, and
  installable evidence packs that prove the integration is healthy after
  rollout.
- Go full-repository coverage remains below 100%. The right response is
  targeted risk-based coverage and package-level thresholds, not fake global
  accounting.

## Industry Baseline

The following baseline is synthesized from official vendor documentation and
observed product patterns. It is a product capability baseline, not a market
share claim.

| Product family | Representative systems | Top-tier pattern | Attune implication |
|---|---|---|---|
| Engineering delivery | GitHub, Linear, Jira | Issues are connected to pull requests, commits, projects, deployments, and release state. Installation permissions and webhook health are explicit. | Add delivery graph artifacts and GitHub App/Jira installation-grade setup. |
| Product discovery | Jira Product Discovery, Productboard, Aha! | Ideas retain product ownership while delivery tickets, insights, scoring, and roadmap fields remain inspectable. | Keep Customer Requests primary and make delivery state explanatory rather than authoritative. |
| Public feedback platforms | Canny, Featurebase, UserVoice, Nolt, Sleekplan | Portals support voting, statuses, duplicates, follows, notifications, changelogs, access rules, widgets, and analytics. | Move public board from usable surface to closed-loop feedback platform. |
| Support feedback loops | Zendesk, Intercom, Front, Help Scout | Support conversations can create or link issues, but private conversations are not blindly mirrored. | Add source connectors and request links without syncing private notes by default. |
| Monitoring-to-issue loops | Sentry, Bugsnag, Rollbar | Issues can be created/linked and resolved from engineering state, with audit-friendly provider context. | Reuse external sync for provider state and keep resolution suggestions separate from request lifecycle changes. |
| Sync platforms | Unito, Exalate, Zapier, Make, Workato | Durable mappings, field rules, transforms, dedupe, replay, and conflict resolution define trust. | Upgrade mapping UX and rule semantics without fragmenting provider-specific code. |
| Enterprise SaaS | Linear enterprise, Atlassian, Productboard enterprise | SAML/SCIM, audit logs, least-privilege app installs, retention, admin visibility, and reliability reporting are product features. | Convert readiness evidence into Console-visible governance, not only CI artifacts. |

## Complete Target State

The complete solution is not a GitHub sync add-on. The target product is a
Product Signal OS with seven integrated planes. Each plane has a durable data
model, generated API surface, Console experience, public or private projection,
audit trail, metrics, and browser-verifiable acceptance contract.

### Plane 1: Evidence Intake

Attune should ingest feedback from every source where product signal appears and
preserve source context without letting source-specific data leak into public or
provider surfaces.

Required capabilities:

- portal submissions;
- public board votes and comments;
- API, webhook, email, Slack, support-system, CRM, CSV/import, and browser
  widget feedback sources;
- source-aware dedupe and similar-request suggestions;
- tenant-scoped rate limits and abuse controls by source;
- provenance records that preserve origin, submitter display policy, account
  hints, source URLs, and consent state;
- source health for pull/poll adapters and delivery health for inbound
  webhooks;
- replayable ingestion failures with redacted diagnostics.

Canonical records:

- `feedback_sources`
- `feedback_items`
- `feedback_provenance`
- `feedback_source_health`
- existing raw feedback and inbound source tables where already implemented.

Product contract:

- operators can see where every request signal came from;
- import, webhook, portal, and support-source signals enter the same request
  evidence model;
- public projections never expose private source payloads;
- source-specific failures are recoverable without database access.

### Plane 2: Customer Request Intelligence

Customer Requests remain the product-owned object. They aggregate evidence,
votes, accounts, delivery artifacts, public visibility, and decisions.

Required capabilities:

- request creation, merge, split, archive, unarchive, and duplicate
  consolidation;
- evidence linking and unlinking with full provenance;
- account, segment, revenue, customer breadth, vote breadth, urgency,
  confidence, and evidence-quality rollups;
- configurable scoring policies with versioned explanations;
- AI-assisted duplicate, theme, severity, product-area, owner, and brief
  suggestions with explicit review;
- operator notes and decision log entries that never sync externally by
  default;
- delivery-state suggestions that do not silently mutate request lifecycle;
- audit-safe export of request evidence and decision rationale.

Canonical records:

- `customer_requests`;
- request feedback links, account links, votes, notes, issue links, and public
  profiles already present in the repository;
- `request_scoring_policies`;
- `request_insight_clusters`;
- `request_ai_suggestions`;
- `request_decision_events`.

Product contract:

- an operator can answer why a request exists, who asked for it, what evidence
  supports it, what value it carries, what duplicates were merged, and why its
  score changed;
- AI is advisory and reviewable;
- request lifecycle is product-owned even when delivery systems close or reopen
  linked work.

### Plane 3: Public Engagement

The public experience is a full feedback loop: submit, discover, vote, comment,
follow, receive updates, read release notes, and understand what changed.

Required capabilities:

- submit-only portal;
- public board, request detail, roadmap, and release/changelog pages;
- search, filters, tag filters, status filters, roadmap filters, top/recent and
  trending sort;
- vote, unvote, comment, follow, unsubscribe, notification preferences, and
  submitter confirmation;
- duplicate/similar request discovery before and after submission;
- merge redirects and visible public-safe merge history;
- custom access policies, custom domains, widgets, multi-portal routing, and
  localization;
- public-safe analytics for operators and public-safe activity history for
  visitors.

Canonical records:

- public visibility policy and moderation records already present;
- `public_request_subscriptions`;
- `public_release_updates`;
- `public_release_update_requests`;
- public slug aliases for merge redirects;
- custom access, domain, widget, and portal configuration records.

Product contract:

- a visitor can find the right request without browsing a flat backlog;
- a voter or follower receives exactly one relevant notification per public
  state change unless they chose a digest;
- a public release update can close the loop for many requests;
- public access rules never reveal hidden request existence.

### Plane 4: Delivery Graph

Delivery is a graph of external artifacts, not a single issue link.

Required capabilities:

- issue, pull request, commit, branch, deployment, release, project item,
  sub-issue, and support ticket artifacts;
- provider-neutral artifact relationships: implements, blocks, duplicates,
  references, ships-in, reported-from, parent, and child;
- delivery health rollup with stale, blocked, shipped, conflicting, failed, and
  unknown explanations;
- provider-specific deep links and compact redacted payloads;
- public-safe artifact projection controlled by policy;
- rebuildable projection from external sync state and provider events.

Canonical records:

- `delivery_artifacts`;
- external object links, comments, runs, failures, conflicts, and events;
- customer request issue links for backward-compatible issue surfaces.

Product contract:

- request detail can explain delivery state across GitHub, Jira, Linear, and
  support systems;
- merged PRs, deployments, releases, and linked tickets can produce suggestions
  but do not silently rewrite request lifecycle;
- every artifact has source, relationship, provider, last-seen timestamp, and
  reconstruction path.

### Plane 5: Sync And Automation

The sync engine must be a controlled automation plane, not a set of provider
scripts.

Required capabilities:

- provider installations and encrypted connections;
- provider capability discovery and qualification;
- mappings with direction, field ownership, transforms, filters, conflict
  policy, tombstone policy, and dry-run preview;
- pull, push, webhook-triggered, replay, backfill, and canary runs;
- cursor overlap, retry-after, secondary-rate-limit handling, idempotent
  writes, marker dedupe, and record-level failures;
- per-field conflicts and batch repair;
- provider sandbox mode and fake-provider conformance tests;
- provider event replay and dead-run recovery.

Canonical records:

- existing external sync framework tables;
- `provider_installations`;
- mapping rule versions;
- replay plans;
- readiness and canary records.

Product contract:

- operators can preview what a mapping will write before it writes;
- retries do not duplicate issues or comments;
- rate limits delay work instead of corrupting state;
- conflict resolution is explicit, audited, and reversible where possible.

### Plane 6: Governance And Security

Enterprise trust is part of the product.

Required capabilities:

- SSO/OIDC, role-based access, delegated admin, break-glass, and tenant member
  governance;
- GitHub App and provider least-privilege installs;
- encrypted credentials, webhook secrets, customer email addresses, and managed
  provider tokens;
- audit log and evidence export;
- retention and erasure policies for feedback, public subscriptions, provider
  payloads, AI prompts/outputs, and notification history;
- public/private data boundary enforcement through tests and lint;
- signed webhooks, replay protection, idempotency keys, CSRF protection, CSP,
  CORS, custom-domain ownership verification, and outbound egress policy.

Canonical records:

- existing auth, audit, break-glass, secret, and evidence-export tables;
- retention policies;
- privacy consent and notification preference records;
- provider installation security posture records.

Product contract:

- every security-sensitive mutation has an audit event;
- every exported evidence pack redacts secrets and private payloads;
- every public route uses an allowlisted projection;
- custom access and custom domains can be verified and diagnosed from Console.

### Plane 7: Operations And Readiness

Readiness must be continuously visible after merge.

Required capabilities:

- tenant/provider/request-level health scores;
- sync freshness, webhook lag, run success, conflict age, dead-run age,
  notification latency, public portal error rate, and provider rate-budget SLOs;
- synthetic canaries for provider install, signed webhook, issue/comment
  round-trip, notification delivery, public portal submission, and custom-domain
  readiness;
- incident runbooks and operator action links;
- release readiness evidence export tying local checks, remote CI, browser
  acceptance, canaries, and audit evidence together;
- cleanup tracking for external artifacts created by canaries or live tests.

Canonical records:

- `readiness_checks`;
- `synthetic_canary_runs`;
- SLO finding records;
- evidence pack export records;
- existing metrics and dashboard definitions.

Product contract:

- a tenant admin can answer whether the system is healthy without SQL or logs;
- an operator can replay failed provider events and record failures from
  Console;
- a release has auditable evidence for CI, runtime, browser, provider, and
  security gates.

## Complete Capability Contract

The complete solution is ready only when all rows below are satisfied. Delivery
sequence can vary, but these capabilities are not optional in the target state.

| Area | Complete capability | Required user-visible proof |
|---|---|---|
| Provider setup | GitHub App, Jira/Atlassian, token fallback, repository/project picker, webhook setup, qualification, permission drift detection | Connection setup wizard and readiness report |
| Evidence intake | Portal, board, API, webhook, email, Slack/support/CRM/import/widget sources with provenance and replay | Source health and request evidence view |
| Request intelligence | Merge/split, account/segment/revenue rollups, configurable scoring, AI suggestions, decision log | Request detail explanation and scoring policy diff |
| Public feedback | Submit, search, vote, comment, follow, notify, release updates, roadmap, custom access, custom domain, widget, multi-portal | Public browser flows and Console policy previews |
| Delivery graph | Issues, PRs, commits, deployments, releases, project items, support tickets, status suggestions | Request delivery graph and health explanation |
| Sync rules | Field ownership, transforms, filters, dry-run, per-field conflicts, replay, batch repair | Mapping preview and conflict studio |
| Security | Least privilege, encrypted secrets, public/private boundary, retention, audit, evidence export | Audit entries, redacted evidence packs, lint/tests |
| Readiness | SLOs, canaries, health findings, cleanup tracking, release evidence | Readiness center and exportable report |
| Verification | Unit, integration, browser, runtime, provider, remote CI, secret/code scanning | Passing gates with attached evidence |

## Complete Readiness Definition

The complete proposal is accepted only when all of these statements are true.
They are intentionally stricter than a feature launch checklist.

- **End-to-end signal loop:** a user can submit feedback, discover or follow the
  canonical public request, receive a public-safe status or release update, and
  see that the feedback loop closed.
- **End-to-end delivery loop:** an operator can link or create provider delivery
  artifacts, observe issue/PR/deployment/release progress, receive suggestions
  from delivery state, and resolve sync conflicts from Console.
- **End-to-end intelligence loop:** evidence, account value, segment context,
  scoring policy, AI suggestions, duplicate clusters, and decision events are
  visible together on the request.
- **End-to-end recovery loop:** webhook redelivery, provider outage, rate-limit,
  permission drift, mapping conflict, dead run, notification failure, and
  canary cleanup failure all have operator-visible diagnosis and recovery paths.
- **End-to-end governance loop:** every sensitive state change has audit
  evidence, every exported bundle is redacted, every public route uses an
  allowlisted projection, and every secret remains encrypted or write-only.
- **End-to-end verification loop:** local checks, remote CI, runtime smoke,
  browser acceptance, provider canaries, security scans, and readiness evidence
  agree on the same commit or deployment artifact.

The target state is not complete if any of these loops require direct SQL,
private log spelunking, raw provider payload inspection, or undocumented manual
steps.

## Complete Console Information Architecture

The Console target state needs dedicated operator surfaces instead of hiding all
new capabilities inside generic settings pages.

- **Control Tower**
  - tenant health score;
  - active incidents, SLO breaches, sync lag, public portal errors;
  - release readiness status and evidence export entry point.

- **Requests**
  - backlog, saved views, scoring explanations, merge/split controls;
  - request detail with evidence, accounts, decision log, public profile,
    delivery graph, AI suggestions, and audit trail;
  - bulk operations with dry-run and undo-aware confirmations.

- **Public Feedback**
  - portal, board, roadmap, release updates, subscriptions, notification
    templates, duplicate moderation, custom access, custom domain, widget, and
    multi-portal settings;
  - preview-as controls for anonymous, subscribed, denied, domain-matched, and
    SSO-matched visitors.

- **Integrations**
  - provider installations, connections, mappings, schema discovery,
    qualification, webhook setup, rate budgets, canaries, and sandbox mode;
  - provider-specific setup help without provider-specific orchestration forks.

- **Sync Operations**
  - run history, event ledger, record failures, conflict studio, replay plans,
    dead-run recovery, and cleanup tracking.

- **Intelligence**
  - insight clusters, duplicate suggestions, scoring policy versions, model
    drift, accepted/rejected suggestion rates, and review queues.

- **Governance**
  - members, roles, delegated admin, SSO/OIDC, break-glass, audit evidence,
    retention policies, security posture, and integration permissions.

## Complete API And Event Contract

All user-visible behavior should be backed by generated contracts where the
repository already follows proto-first development.

Required API families:

- provider installation, repository/project selection, qualification, canary,
  and webhook setup APIs;
- mapping rule, dry-run preview, mapping diff, conflict resolution, replay, and
  repair APIs;
- delivery graph read APIs and delivery suggestion action APIs;
- public subscription, notification preference, release update, custom access,
  custom domain, widget, and multi-portal APIs;
- request merge/split, scoring policy, AI suggestion review, insight cluster,
  and decision log APIs;
- readiness check, SLO finding, evidence pack export, and cleanup status APIs.

Required outbound events:

- request created, merged, split, status changed, public profile changed, and
  scoring policy changed;
- public request followed, unfollowed, notified, hidden, restored, and release
  update published;
- delivery artifact created, changed, stale, shipped, conflicted, and resolved;
- provider connection qualified, permission drift detected, webhook verified,
  canary passed, canary cleanup failed, and mapping changed;
- AI suggestion created, accepted, rejected, expired, and sampled.

Event rules:

- every event has a stable idempotency key and tenant id;
- public events carry only public-safe projections;
- private events redact secrets and raw provider payloads;
- replayable events can be correlated with audit events and readiness evidence.

## Proposal

Build the complete target state around the existing Customer Request and
external sync foundations. The work is grouped into seven capability domains
for review and ownership, but the domains together are the solution. Shipping
one domain alone does not satisfy this proposal. Each domain must expose
operator-visible state, durable audit/replay behavior, public/private data
boundaries, generated API coverage, and browser-verifiable acceptance checks.

### Capability Domain A: Installation-Grade Provider Trust

Top-tier integrations do not ask operators to paste broad tokens and hope the
provider behaves. They guide setup, request least privilege, verify permissions,
register webhooks safely, and surface drift.

Add provider installation records that sit beside `external_connections`:

- `provider_installations`
  - `id`, `tenant_id`, `provider`, `installation_kind`
  - `external_installation_id`, `external_account_id`, `external_account_name`
  - `base_url`, `browser_base_url`, `status`
  - `granted_scopes`, `granted_permissions`, `available_repositories`
  - `last_qualified_at`, `last_qualified_status`, `last_error`
  - `created_by`, `updated_by`, `created_at`, `updated_at`, `deleted_at`

For GitHub, support GitHub App installation as the preferred path:

- GitHub App manifest/configuration is deployment-owned and documented.
- Operators install the app into selected repositories.
- Attune exchanges installation context server-side and never exposes app
  private keys or installation tokens to the Console.
- Connection creation can select from installed repositories and derive
  owner/repo/base URL from the installation.
- GitHub App permissions are checked against required capabilities: issue read,
  issue write, issue comment read/write, metadata, and webhooks.
- Webhook setup validates event selection for `issues`, `issue_comment`, and
  `ping`.
- Existing token-based connections remain supported with a visible capability
  grade: `manual_token`, `limited`, `healthy`, or `action_required`.

Provider qualification should become a first-class readiness report:

- credential/app token validity;
- repository access;
- read/write permission checks by object type;
- webhook secret configured and last verified delivery;
- provider rate-limit state and retry-after behavior;
- egress policy validation;
- schema discovery coverage;
- mapping compatibility with granted capabilities.

Acceptance criteria:

- a tenant can create a GitHub connection from an app installation without
  pasting a broad PAT;
- Console clearly distinguishes healthy, limited, and drifted provider access;
- removing repository access in GitHub changes the next qualification result;
- webhook setup and ping status are visible without reading logs;
- token-based connections still work and show their limitations honestly.

### Capability Domain B: Delivery Graph

Customer Requests should expose delivery as a graph of external artifacts
rather than a single issue row. GitHub Issues sync is one node type.

Implemented in this change:

- generated `CustomerRequestDeliveryGraph`, artifact, and relationship API
  messages;
- request-root and external-issue graph projection in the Customer Request
  detail model;
- provider-neutral `tracked_by` relationships from Customer Requests to linked
  external issues;
- graph health explanation, artifact health, source, assignee, last-seen, and
  sync-error context;
- graph issue artifacts merged with provider-normalized external object link
  payloads, so provider title, state reason, assignees, URL, last-seen time, and
  external-sync failure state are visible without leaking raw payloads;
- provider-neutral `customer_request_delivery_artifacts` projection with
  request-scoped idempotency, PR/commit/branch/deployment/release/project-item/
  sub-issue/support-ticket artifact types, relationship semantics, payload
  storage, repository upsert, and Customer Request detail graph read-through;
- external sync pull child projection for PR/commit/branch/deployment/release/
  project-item/sub-issue/support-ticket artifacts, including soft deletion and
  parent issue link resolution inside the apply transaction;
- GitHub issue pull timeline normalization that emits referenced pull requests
  and closing commits as delivery artifact children, with end-to-end coverage
  from mocked provider HTTP responses through Customer Request graph reads;
- Console detail drawer graph summary and artifact list alongside the existing
  editable issue-link list;
- Go handler/repo tests and Console component coverage for populated and empty
  graph views.

Add a provider-neutral `delivery_artifacts` projection:

- `id`, `tenant_id`, `customer_request_id`
- `provider`, `connection_id`, `mapping_id`
- `artifact_type`
- `external_key`, `external_url`, `display_key`, `title`
- `status`, `status_category`, `state_reason`
- `relationship`
- `source`
- `external_updated_at`, `first_seen_at`, `last_seen_at`
- `payload`

Artifact types:

- `issue`
- `pull_request`
- `commit`
- `branch`
- `deployment`
- `release`
- `project_item`
- `sub_issue`
- `support_ticket`

Relationships:

- `implements`
- `blocks`
- `duplicates`
- `references`
- `ships_in`
- `reported_from`
- `parent`
- `child`

The external sync framework should remain the source of provider pulls and
pushes. `delivery_artifacts` is a read projection used by Customer Request
detail, delivery health rollups, operator dashboards, and public-safe timeline
generation. It should be rebuildable from external object links, provider
payloads, and delivery events.

GitHub expansion should prioritize:

- pull requests that close or reference linked issues;
- commits and branches associated with linked pull requests;
- deployments or releases associated with merged pull requests when provider
  data is available;
- project item status when a GitHub Project is configured;
- sub-issues when GitHub exposes them through the selected API path.

Lifecycle policy:

- Delivery artifact state does not directly mutate Customer Request status.
- Delivery state can produce suggestions such as "all linked delivery artifacts
  are merged" or "GitHub closed this issue while Attune status is planned".
- Operator-accepted suggestions are audited and update the Customer Request
  through existing status transition paths.

Acceptance criteria:

- request detail shows issue, pull request, and release/deployment evidence as
  separate delivery artifacts;
- artifact ingestion is idempotent and can be rebuilt;
- delivery health can explain stale, blocked, failed, shipped, and conflicting
  states with provider links;
- public projections expose only public-safe artifact summaries selected by
  policy.

### Capability Domain C: Closed-Loop Public Feedback

Top feedback products do not stop at collecting votes. They notify people when
the request moves.

Add a public-safe subscription model:

- `public_request_subscriptions`
  - `id`, `tenant_id`, `public_request_profile_id`
  - `visitor_id`, `email_hash`, `email_ciphertext_key_id`,
    `email_ciphertext`
  - `subscription_kind`, `status`, `confirmed_at`, `unsubscribed_at`
  - `preferences`, `created_at`, `updated_at`

Subscription kinds:

- `voter`
- `commenter`
- `follower`
- `submitter`

Notification events:

- public status changed;
- roadmap column changed;
- public comment approved;
- release update published;
- request merged into another public request;
- request hidden or closed with public explanation.

Add public release/update posts:

- `public_release_updates`
  - `id`, `tenant_id`, `slug`, `title`, `body`
  - `status`, `published_at`, `author_id`
  - `audience_policy`, `created_at`, `updated_at`

- `public_release_update_requests`
  - `tenant_id`, `release_update_id`, `customer_request_id`,
    `public_request_profile_id`

Delivery rules:

- notifications are queued through the existing outbox discipline;
- public-safe templates never include internal notes, revenue, private votes,
  or raw provider payloads;
- every email has one-click unsubscribe and preference management;
- notification dedupe keys include tenant, recipient, request, event type, and
  public state version;
- users can follow a request without voting.

Acceptance criteria:

- a voter receives one notification when a public request changes status;
- a follower can unsubscribe and stop receiving subsequent updates;
- release updates can reference multiple public requests;
- public request detail shows a clear "what changed" timeline;
- notification templates pass artifact lint and public projection tests.

### Capability Domain D: Public Board Product Maturity

The public board should scale from "usable page" to "feedback product surface".

Add:

- duplicate merge and split workflow with vote/comment/evidence provenance;
- tag filters and shared public board saved views;
- trending score based on recent votes, comments, unique visitors, and recency;
- custom access policies by email domain, SSO group, signed customer JWT,
  account segment, or allowlist;
- custom domains with verified DNS ownership and TLS readiness status;
- embeddable widget mode with CORS, CSP, origin allowlists, and compact public
  projection;
- multi-portal routing so one tenant can operate separate boards for product
  areas, customer segments, or languages;
- localization strategy for portal chrome and request metadata while preserving
  English canonical shipped artifacts in code and docs.

Duplicate merge behavior:

- merge keeps one canonical Customer Request;
- source request votes, comments, linked feedback, portal subscriptions, and
  public slugs are preserved through redirect/alias records;
- merge emits audit events and public timeline entries when the source request
  was public;
- unmerge is supported only while no irreversible notification has been sent,
  or else requires an explicit restoration workflow.

Custom access behavior:

- public, private, and custom-access boards use one policy engine;
- public-safe projections remain allowlisted even for authenticated boards;
- custom-access denial pages never reveal hidden request existence;
- Console previews can test access as anonymous, domain-matched, SSO-matched,
  and denied visitors.

Acceptance criteria:

- operators can merge duplicate public requests without losing votes,
  comments, followers, or provenance;
- users can filter by status, tag, roadmap column, vote state, and trending;
- custom domain readiness is visible and testable from Console;
- embedded portal loads only on allowed origins;
- multi-portal routing keeps tenant isolation and public-safe projection tests
  green.

### Capability Domain E: Decision Intelligence And AI Review

Top systems help teams decide what to build, not only where an issue lives.

Extend Customer Request decision intelligence with:

- configurable score weights with versioned scoring policies;
- confidence and evidence-quality fields;
- account and segment rollups from explicit customer links, votes, support
  sources, and CRM imports;
- theme and duplicate clusters generated from embeddings and reviewed by
  operators;
- AI-assisted triage suggestions for severity, product area, duplicate target,
  public response draft, and delivery owner;
- spec brief generation that references evidence links rather than inventing
  product claims;
- drift monitoring for AI suggestions, including accepted/rejected rates and
  sampling queues.

Add data models:

- `request_scoring_policies`
  - tenant-scoped score weights, caps, included signals, version, audit actor.

- `request_insight_clusters`
  - cluster type, representative request, member count, confidence, status,
    created model/version, reviewed_by, reviewed_at.

- `request_ai_suggestions`
  - request id, suggestion type, payload, confidence, model metadata, evidence
    references, status, reviewed_by, reviewed_at.

Rules:

- AI suggestions are never applied silently to public visibility, external
  delivery systems, or customer notifications.
- Generated specs must cite request ids, feedback ids, and public-safe
  summaries.
- Low-confidence suggestions go to review queues, not automatic state changes.
- Model prompts and outputs follow existing LLM audit and redaction rules.

Acceptance criteria:

- operators can inspect why a request score changed between policy versions;
- duplicate suggestions require explicit operator acceptance before merge;
- public response drafts never include internal notes or private account data;
- insight cluster sampling can be reviewed and exported as audit evidence.

### Capability Domain F: Sync Rule Engine And Repair UX

The external sync framework has the right backbone. Top sync platforms add a
rule engine and repair experience that operators can understand.

Add a mapping rule layer:

- field transforms: literal, copy, enum map, template, join, split, regex,
  date/time conversion, label prefix management;
- conditional filters: sync only when status, label, segment, source, owner, or
  workflow state matches;
- field ownership: local-wins, external-wins, managed-section, managed-prefix,
  read-only, write-only, and manual-conflict;
- per-field conflict policy;
- dry-run preview with sample records and expected provider writes;
- mapping version diff;
- replay plan generation before a mapping change is enabled.

Console repair UX:

- conflict studio grouped by mapping, run, provider, field, and severity;
- batch resolution with preview and audit reason;
- replay queue for record failures, provider events, and dead runs;
- "explain this failure" panel that shows redacted request id, provider request
  id, retry-after, mapping version, and next action;
- sandbox connection mode for fake-provider validation before live provider
  writes.

Provider contract additions:

- capability metadata for field transforms supported by the adapter;
- idempotency hints for create/update/comment operations;
- conditional request support when provider exposes ETags or versions;
- retry-budget status including provider primary and secondary rate limits.

Acceptance criteria:

- a mapping change can be previewed before writing to a provider;
- per-field conflicts are visible and batch-resolvable;
- provider rate-limit exhaustion creates delayed retries instead of dead runs
  when retry-after is available;
- sandbox provider validation exercises pull, push, webhook, replay, conflict,
  and repair paths without external network access.

### Capability Domain G: Enterprise Readiness Plane

Enterprise trust is not only feature count. It is the ability to prove the
system is healthy and recover it when it is not.

Add tenant-level readiness surfaces:

- integration health score per tenant/provider/mapping;
- SLO panels for sync freshness, run success rate, webhook delivery lag,
  conflict age, dead-run age, notification latency, and public portal error
  rate;
- synthetic canaries for provider qualification, signed webhook receive,
  issue/comment round-trip in sandbox repositories, public board submission,
  and notification delivery;
- incident runbooks linked from Console health findings;
- evidence pack export that bundles recent runs, audit events, config hashes,
  canary results, provider request ids, and verification timestamps;
- release readiness checklist that ties local `make ci-check`, browser
  acceptance, runtime smoke, integration tests, remote CI, and canary results
  to one artifact.

Data model:

- `readiness_checks`
  - `id`, `tenant_id`, `area`, `scope`, `status`, `severity`,
    `evidence`, `started_at`, `finished_at`, `created_by`.

- `synthetic_canary_runs`
  - `id`, `tenant_id`, `provider`, `connection_id`, `scenario`, `status`,
    `external_artifact_url`, `cleanup_status`, `evidence`, timestamps.

Rules:

- canaries must clean up external artifacts or mark cleanup failures
  explicitly;
- readiness exports redact credentials, secrets, webhook signatures, and raw
  private content;
- SLO violations create operator-visible findings and can optionally notify
  tenant admins;
- readiness gates distinguish local evidence, remote CI evidence, sandbox
  provider evidence, and live provider evidence.

Acceptance criteria:

- an operator can answer "is GitHub sync healthy for this tenant?" without SQL;
- stale webhooks, dead runs, high conflict age, and missing permissions appear
  as actionable health findings;
- canary-created provider artifacts are cleaned up or reported;
- release readiness evidence can be exported and attached to a PR or release.

## Data Boundary Contract

All capability domains must preserve these boundaries:

| Data class | Allowed destinations | Never by default |
|---|---|---|
| Internal notes | Console, audit summaries, private exports | GitHub/Jira comments, public portal, notification emails |
| Public portal comments | Public portal, public-safe notifications | GitHub/Jira comments, internal notes |
| Customer identity | Operator Console, public-safe display names when consented | Provider payloads unless explicitly mapped |
| Revenue/account value | Console, scoring, private exports | Public portal, external delivery comments |
| Provider raw payloads | Compact redacted diagnostics | Logs, public surfaces, outbound notifications |
| Credentials/secrets | Encrypted storage only | API responses, logs, audit snapshots |
| AI suggestions | Review queues and accepted changes | Silent public or provider writes |

## Alternatives Considered

### Treat #228 As The Complete Product

Rejected. GitHub Issues sync is necessary but not sufficient. It solves one
delivery-system integration, while top-tier Product Signal OS behavior also
requires public feedback loops, delivery graph visibility, decision
intelligence, enterprise installation, and operational evidence.

### Build Provider-Specific Product Surfaces

Rejected. GitHub, Jira, Linear, and support systems need consistent connection,
mapping, run, conflict, event, audit, and recovery behavior. Provider-specific
diagnostics would fragment operator understanding and duplicate the external
sync framework.

### Mirror Everything Bidirectionally

Rejected. Full mirrors leak private context and damage ownership boundaries.
Attune should sync managed fields, public-safe context, and provider delivery
state while preserving external system authorship.

### Prioritize Public Portal Maturity Before Provider Trust

Rejected as the only path. Public feedback loops and provider trust reinforce
each other: users need closed-loop updates, and operators need confidence that
delivery state is real. Capability domains should ship through reviewable pull
requests, but the complete target state requires all of them.

### Make Global 100 Percent Coverage The Primary Gate

Rejected. Full-repository coverage can become a vanity metric. Risk-based
coverage, package thresholds for new critical paths, mutation-style tests for
sync decisions, browser acceptance, live-provider canaries, and remote CI are
more meaningful. Console can keep its configured 100 percent gate; Go should use
critical package thresholds and no-decrease aggregate gates until a separate
coverage program raises the baseline honestly.

## Risks / Tradeoffs

- GitHub App installation adds deployment and operational complexity. The
  benefit is least privilege, repository selection, clearer health, and better
  enterprise trust.
- Delivery graph expansion can create noisy timelines. The UI must group
  artifacts and expose explanations rather than dumping provider events.
- Public notifications can become spam. Subscription defaults, dedupe keys,
  digest options, and preference management are required.
- Custom domains and embeds increase security surface. DNS verification, TLS
  readiness, CSP, CORS, origin allowlists, and public projection tests are
  mandatory.
- AI suggestions can create false confidence. Suggestions need confidence,
  review queues, evidence references, and rejection tracking.
- Field transform rules can become a complex product. Start with a small typed
  transform set and explicit unsupported-state errors.
- Canaries that write to live providers can create clutter. Use sandbox
  repositories by default, stable markers, and verified cleanup.
- More readiness gates can slow development. Gates should be tied to risk and
  automated wherever possible.

## Complete Implementation Specification

This section is the implementation contract for the full product, not a delivery
roadmap. A reviewer should be able to derive migrations, proto services, Go
packages, Console routes, workers, metrics, and tests from this section without
another architecture pass.

### Repository Architecture

The full system keeps attune's existing layer boundaries and adds feature
packages rather than provider-specific orchestration paths.

Go packages:

- `internal/service/signals`
  - source-aware feedback intake orchestration;
  - evidence provenance upserts;
  - ingestion replay and source health decisions.

- `internal/repo/signals`
  - feedback source health;
  - evidence provenance;
  - imported source batches and replayable ingestion failures.

- `internal/service/requestintel`
  - request merge/split;
  - scoring policy evaluation;
  - insight clusters;
  - AI suggestion review lifecycle;
  - decision event recording.

- `internal/repo/requestintel`
  - scoring policy versions;
  - insight clusters;
  - AI suggestions;
  - merge aliases and decision events.

- `internal/service/publicengagement`
  - public subscriptions;
  - notification preferences;
  - release updates;
  - custom access decisions;
  - widget and custom-domain readiness.

- `internal/repo/publicengagement`
  - subscription records;
  - release update records;
  - custom access, domain, widget, and portal configuration.

- `internal/service/deliverygraph`
  - provider artifact projection;
  - request delivery health;
  - delivery suggestions;
  - projection rebuilds.

- `internal/repo/deliverygraph`
  - delivery artifacts;
  - projection rebuild checkpoints;
  - delivery suggestion records.

- `internal/service/syncrules`
  - mapping rule validation;
  - dry-run preview;
  - per-field conflict classification;
  - replay plan generation.

- `internal/repo/syncrules`
  - mapping rule versions;
  - field conflict rows;
  - replay plans.

- `internal/service/readiness`
  - readiness checks;
  - SLO findings;
  - provider canaries;
  - evidence pack assembly;
  - canary cleanup tracking.

- `internal/repo/readiness`
  - readiness checks;
  - synthetic canary runs;
  - SLO findings;
  - evidence export references.

- `internal/service/providerinstall`
  - GitHub App installation lifecycle;
  - provider installation qualification;
  - repository/project selection;
  - webhook setup diagnostics.

- `internal/repo/providerinstall`
  - provider installations;
  - selected provider resources;
  - permission snapshots.

Adapters remain under `internal/externalsync/adapter/<provider>`. They expose
provider facts; they do not mutate Customer Requests directly. Product services
consume external sync records, provider capabilities, and delivery artifacts.

Console feature folders:

- `console/src/features/control-tower`
- `console/src/features/request-intelligence`
- `console/src/features/public-engagement`
- `console/src/features/delivery-graph`
- `console/src/features/provider-installations`
- `console/src/features/sync-operations`
- `console/src/features/readiness`

Generated proto services are the only public Console/API contract. Hand-written
JSON handlers are limited to provider webhooks, public HTML routes, and static
browser surfaces that already follow existing project conventions.

### Database Schema Contract

The full target state requires the following schema groups. Exact migration
numbers are assigned when implementation lands, but the logical contract is
fixed here.

`feedback_sources`

- `id`, `tenant_id`, `source_type`, `name`, `enabled`, `status`
- `provider`, `external_key`, `display_url`
- `config`, `credential_key_id`, `credential_ciphertext`
- `last_seen_at`, `last_ingested_at`, `last_error`
- `created_by`, `updated_by`, `created_at`, `updated_at`, `deleted_at`

`feedback_provenance`

- `id`, `tenant_id`, `feedback_id`, `source_id`
- `source_record_key`, `source_record_url`, `submitter_display`
- `account_key`, `customer_key`, `consent_state`
- `payload_digest`, `payload_summary`
- `created_at`

`feedback_ingestion_failures`

- `id`, `tenant_id`, `source_id`, `source_record_key`
- `error_kind`, `error_message`, `retryable`
- `payload_digest`, `attempts`, `next_retry_at`
- `first_seen_at`, `last_seen_at`, `resolved_at`

`request_scoring_policies`

- `id`, `tenant_id`, `version`, `name`, `status`
- `weights`, `caps`, `included_signals`
- `created_by`, `activated_by`, `created_at`, `activated_at`

`request_decision_events`

- `id`, `tenant_id`, `customer_request_id`, `event_type`
- `actor_id`, `source`, `reason`, `evidence`
- `created_at`

`request_merge_aliases`

- `id`, `tenant_id`, `source_request_id`, `target_request_id`
- `source_public_slug`, `merge_reason`, `merged_by`, `merged_at`
- `reverted_by`, `reverted_at`

`request_insight_clusters`

- `id`, `tenant_id`, `cluster_type`, `status`
- `representative_request_id`, `member_count`, `confidence`
- `model`, `model_version`, `evidence`
- `reviewed_by`, `reviewed_at`, `created_at`, `updated_at`

`request_ai_suggestions`

- `id`, `tenant_id`, `customer_request_id`, `suggestion_type`
- `payload`, `confidence`, `evidence_refs`
- `model`, `model_version`, `prompt_digest`
- `status`, `reviewed_by`, `reviewed_at`
- `created_at`, `expires_at`

`public_request_subscriptions`

- `id`, `tenant_id`, `public_request_profile_id`
- `visitor_id`, `email_hash`, `email_ciphertext_key_id`
- `email_ciphertext`, `subscription_kind`, `status`
- `preferences`, `confirmed_at`, `unsubscribed_at`
- `created_at`, `updated_at`

`public_release_updates`

- `id`, `tenant_id`, `slug`, `title`, `body`
- `status`, `audience_policy`, `published_at`, `author_id`
- `created_at`, `updated_at`, `deleted_at`

`public_release_update_requests`

- `tenant_id`, `release_update_id`, `customer_request_id`
- `public_request_profile_id`, `created_at`

`public_slug_aliases`

- `id`, `tenant_id`, `source_slug`, `target_slug`
- `reason`, `created_at`, `deleted_at`

`public_portals`

- `id`, `tenant_id`, `slug`, `name`, `locale`
- `access_policy`, `theme`, `enabled`
- `created_by`, `updated_by`, `created_at`, `updated_at`, `deleted_at`

`public_custom_domains`

- `id`, `tenant_id`, `portal_id`, `hostname`
- `verification_token_hash`, `verification_status`, `tls_status`
- `last_checked_at`, `last_error`, `created_at`, `updated_at`

`public_widgets`

- `id`, `tenant_id`, `portal_id`, `name`
- `origin_allowlist`, `mode`, `enabled`
- `created_by`, `updated_by`, `created_at`, `updated_at`

`provider_installations`

- `id`, `tenant_id`, `provider`, `installation_kind`
- `external_installation_id`, `external_account_id`, `external_account_name`
- `base_url`, `browser_base_url`, `status`
- `granted_scopes`, `granted_permissions`, `available_repositories`
- `last_qualified_at`, `last_qualified_status`, `last_error`
- `created_by`, `updated_by`, `created_at`, `updated_at`, `deleted_at`

`provider_installation_resources`

- `id`, `tenant_id`, `installation_id`, `resource_type`
- `external_key`, `name`, `url`, `permissions`
- `selected`, `last_seen_at`, `created_at`, `updated_at`

`delivery_artifacts`

- `id`, `tenant_id`, `customer_request_id`
- `provider`, `connection_id`, `mapping_id`
- `artifact_type`, `external_key`, `external_url`, `display_key`, `title`
- `status`, `status_category`, `state_reason`
- `relationship`, `source`, `payload`
- `external_updated_at`, `first_seen_at`, `last_seen_at`, `deleted_at`

`delivery_suggestions`

- `id`, `tenant_id`, `customer_request_id`, `suggestion_type`
- `status`, `confidence`, `reason`, `artifact_ids`
- `created_at`, `accepted_by`, `accepted_at`, `rejected_by`, `rejected_at`

`external_mapping_rule_versions`

- `id`, `tenant_id`, `mapping_id`, `version`
- `status`, `field_rules`, `filters`, `ownership`, `conflict_policy`
- `created_by`, `activated_by`, `created_at`, `activated_at`

`external_field_conflicts`

- `id`, `tenant_id`, `mapping_id`, `external_object_link_id`
- `field_name`, `local_value`, `external_value`
- `policy`, `status`, `resolution`, `resolved_by`, `resolved_at`
- `first_run_id`, `last_run_id`, `created_at`, `updated_at`

`external_replay_plans`

- `id`, `tenant_id`, `mapping_id`, `plan_type`, `status`
- `input`, `preview`, `created_by`, `approved_by`
- `created_at`, `approved_at`, `finished_at`

`readiness_checks`

- `id`, `tenant_id`, `area`, `scope`, `status`, `severity`
- `evidence`, `started_at`, `finished_at`, `created_by`

`synthetic_canary_runs`

- `id`, `tenant_id`, `provider`, `connection_id`, `scenario`
- `status`, `external_artifact_url`, `cleanup_status`, `evidence`
- `started_at`, `finished_at`, `cleanup_finished_at`

`slo_findings`

- `id`, `tenant_id`, `area`, `scope`, `status`, `severity`
- `slo_name`, `observed_value`, `threshold`, `evidence`
- `opened_at`, `acknowledged_by`, `acknowledged_at`, `resolved_at`

Schema rules:

- all tenant-owned tables include `tenant_id` and tenant-scoped indexes;
- soft-delete semantics use `deleted_at` where restoring or audit matters;
- encrypted columns use key registry references and associated data that binds
  tenant, table, row id, and purpose;
- public records never store internal notes or private provider payloads;
- provider payload fields are compact, redacted, and size-capped;
- every async or provider operation has a durable idempotency key or uniqueness
  constraint.

### Proto And API Surface

The complete API surface is split into generated services.

`ProviderInstallationService`

- `ListProviderInstallations`
- `CreateProviderInstallation`
- `CompleteProviderInstallation`
- `DeleteProviderInstallation`
- `ListProviderInstallationResources`
- `SelectProviderInstallationResources`
- `QualifyProviderInstallation`
- `GetProviderWebhookSetup`
- `VerifyProviderWebhookSetup`

`ExternalSyncRuleService`

- `ListMappingRuleVersions`
- `CreateMappingRuleVersion`
- `PreviewMappingRuleVersion`
- `ActivateMappingRuleVersion`
- `DiffMappingRuleVersions`
- `ListFieldConflicts`
- `ResolveFieldConflict`
- `BatchResolveFieldConflicts`
- `CreateReplayPlan`
- `PreviewReplayPlan`
- `ApproveReplayPlan`

`DeliveryGraphService`

- `ListDeliveryArtifacts`
- `GetDeliveryArtifact`
- `RebuildDeliveryArtifacts`
- `ListDeliverySuggestions`
- `AcceptDeliverySuggestion`
- `RejectDeliverySuggestion`

`PublicEngagementService`

- `ListPublicPortals`
- `CreatePublicPortal`
- `UpdatePublicPortal`
- `GetPublicPortalPolicyPreview`
- `ListPublicSubscriptions`
- `UpdatePublicSubscription`
- `UnsubscribePublicSubscription`
- `CreatePublicReleaseUpdate`
- `UpdatePublicReleaseUpdate`
- `PublishPublicReleaseUpdate`
- `ListPublicReleaseUpdates`
- `CreatePublicCustomDomain`
- `VerifyPublicCustomDomain`
- `CreatePublicWidget`
- `UpdatePublicWidget`

`RequestIntelligenceService`

- `CreateRequestMergePreview`
- `MergeCustomerRequests`
- `RevertCustomerRequestMerge`
- `SplitCustomerRequest`
- `ListScoringPolicies`
- `CreateScoringPolicy`
- `ActivateScoringPolicy`
- `ExplainRequestScore`
- `ListInsightClusters`
- `ReviewInsightCluster`
- `ListAISuggestions`
- `ReviewAISuggestion`
- `GenerateRequestBrief`

`ReadinessService`

- `ListReadinessChecks`
- `RunReadinessCheck`
- `ListSLOFindings`
- `AcknowledgeSLOFinding`
- `ResolveSLOFinding`
- `RunSyntheticCanary`
- `GetSyntheticCanaryRun`
- `ExportReadinessEvidencePack`

Public routes:

- `GET /portal/{portal_slug}`
- `GET /portal/{portal_slug}/requests`
- `GET /portal/{portal_slug}/requests/{public_slug}`
- `GET /portal/{portal_slug}/roadmap`
- `GET /portal/{portal_slug}/updates`
- `GET /portal/{portal_slug}/updates/{release_slug}`
- `POST /v1/portal/{portal_slug}/requests/{public_slug}:follow`
- `POST /v1/portal/{portal_slug}/requests/{public_slug}:unfollow`
- `GET /v1/portal/{portal_slug}/subscriptions/{token}`
- `POST /v1/portal/{portal_slug}/subscriptions/{token}:unsubscribe`
- `GET /v1/widgets/{widget_id}/bootstrap`
- `POST /v1/widgets/{widget_id}/feedback`

API rules:

- all Console mutations require CSRF and tenant membership;
- all public write endpoints require rate limits and idempotency keys;
- all preview APIs return no side effects;
- every mutation returns refreshed state plus audit/replay identifiers where
  applicable;
- errors use the generated error enum and existing error response contract.

### State Machines

Request lifecycle remains separate from provider delivery state.

Customer Request status:

- `open`
- `planned`
- `in_progress`
- `shipped`
- `cancelled`
- `archived`

Merge status:

- `canonical`
- `merged_source`
- `split_source`
- `restored`

Public request state:

- `draft`
- `pending_review`
- `public`
- `hidden`
- `merged_redirect`
- `closed`

Subscription state:

- `pending_confirmation`
- `active`
- `paused`
- `unsubscribed`
- `bounced`
- `suppressed`

Release update state:

- `draft`
- `scheduled`
- `published`
- `retracted`

Provider installation state:

- `pending`
- `active`
- `limited`
- `drifted`
- `suspended`
- `deleted`

Mapping rule state:

- `draft`
- `previewed`
- `active`
- `superseded`
- `disabled`

Field conflict state:

- `open`
- `resolved_local`
- `resolved_external`
- `resolved_manual`
- `ignored`
- `superseded`

Readiness check state:

- `queued`
- `running`
- `passed`
- `warning`
- `failed`
- `cancelled`

State transition rules:

- lifecycle transitions that affect public visibility or notifications record a
  `request_decision_events` row and audit event;
- public transitions enqueue notifications only after the public projection is
  committed;
- provider state changes create delivery suggestions, not direct request status
  mutations;
- conflict resolution writes audit reason and affected field list;
- readiness warnings and failures remain visible until acknowledged or resolved.

### Core Algorithms

Request merge:

1. lock source and target requests in tenant scope;
2. reject merge if source equals target, tenant differs, or either request is
   archived in a way policy disallows;
3. produce merge preview with feedback, votes, public comments, subscriptions,
   issue links, delivery artifacts, account rollups, and public slug effects;
4. on commit, move canonical references to target with provenance;
5. create `request_merge_aliases` and public slug redirect when public;
6. recompute rollups, scoring, delivery health, and public projections;
7. enqueue public-safe merge notifications when subscriptions exist;
8. record audit and decision events.

Scoring:

1. load active `request_scoring_policies`;
2. compute normalized signals for evidence count, distinct customers, distinct
   accounts, revenue, segment weight, urgency, votes, confidence, delivery
   freshness, and duplicate cluster size;
3. cap each signal by policy;
4. compute weighted score and explanation terms;
5. store score version and explanation digest;
6. expose score diff when policy changes.

Delivery artifact projection:

1. read external object links and provider child records for a request;
2. normalize provider payloads into artifact candidates;
3. upsert by tenant, provider, connection, artifact type, and external key;
4. mark missing artifacts stale before deleting them;
5. recompute delivery health and suggestions;
6. publish redacted events for dashboards and readiness checks.

Public notification dedupe:

1. derive recipient set from subscriptions, voters, commenters, and submitters;
2. apply notification preferences, suppression state, and public access policy;
3. render public-safe template from committed projection;
4. enqueue outbox row with dedupe key:
   `tenant_id:public_request_id:event_type:recipient_hash:public_version`;
5. mark notification sent only after transport success;
6. expose retry and failure in public engagement diagnostics.

Mapping dry-run:

1. validate mapping rule syntax and provider capabilities;
2. sample linked and unlinked local records;
3. apply transforms and filters;
4. classify writes as create, update, no-op, conflict, or unsupported;
5. include provider permissions and rate-budget warnings;
6. return preview without writing provider or cursor state.

Provider canary:

1. qualify connection and check required capabilities;
2. create a marked provider artifact in sandbox scope;
3. run pull, push, comment, webhook, and cleanup scenarios;
4. verify no duplicate writes after replay;
5. close/delete canary artifact where provider permits;
6. record cleanup success or failure as readiness evidence.

### Console Interaction Contract

The Console must make every critical loop operable without SQL.

Required pages and primary interactions:

| Page | Required interactions |
|---|---|
| Control Tower | inspect tenant health, SLO findings, release readiness, recent canaries, and incident links |
| Requests | search, filter, sort, merge, split, score, review AI suggestions, inspect evidence, inspect delivery graph |
| Request Detail | show evidence, accounts, score explanation, public profile, delivery artifacts, notes, decision log, audit events |
| Public Feedback | edit portals, policy, board filters, roadmap, release updates, subscriptions, custom domains, widgets |
| Provider Installations | install GitHub App, select repos, inspect permissions, qualify provider, inspect webhook setup |
| External Sync | edit mappings, preview mapping rules, inspect runs, replay events, repair record failures |
| Conflict Studio | group conflicts, preview resolution, batch resolve with audit reason |
| Intelligence | review clusters, accept/reject suggestions, inspect model drift and evidence references |
| Readiness | run checks, run canaries, inspect cleanup, export evidence packs |
| Governance | inspect audit evidence, retention, roles, break-glass, provider security posture |

Interaction rules:

- every destructive or externally writing action has preview, confirmation, and
  audit reason;
- bulk operations show item-level success, conflict, skipped, and failed counts;
- provider links open in new tabs and never expose secrets;
- public preview supports anonymous, allowed, denied, subscribed, and
  custom-domain modes;
- loading, empty, error, retrying, partial-success, and conflict states are
  visible and tested.

### Security And Permission Matrix

Roles:

- `owner`
- `admin`
- `operator`
- `viewer`
- `security_admin`
- `integration_admin`
- `public_moderator`

Permission groups:

| Permission | owner | admin | operator | viewer | security_admin | integration_admin | public_moderator |
|---|---|---|---|---|---|---|---|
| Manage provider installations | yes | yes | no | no | audit-only | yes | no |
| Rotate provider credentials | yes | yes | no | no | audit-only | yes | no |
| Edit mappings | yes | yes | no | no | no | yes | no |
| Run provider canaries | yes | yes | no | no | yes | yes | no |
| Resolve sync conflicts | yes | yes | yes | no | no | yes | no |
| Merge/split requests | yes | yes | yes | no | no | no | no |
| Review AI suggestions | yes | yes | yes | no | no | no | no |
| Publish public updates | yes | yes | yes | no | no | no | yes |
| Moderate public comments | yes | yes | yes | no | no | no | yes |
| Export readiness evidence | yes | yes | no | no | yes | no | no |
| Manage retention/security | yes | no | no | no | yes | no | no |

Security rules:

- provider installations use least-privilege permissions and show drift;
- personal tokens are labeled as manual-token mode and carry capability
  warnings;
- webhook secrets are write-only and rotated through explicit actions;
- customer email addresses in public subscriptions are encrypted and displayed
  only as redacted values;
- public widgets require origin allowlists and CSP-compatible bootstrap;
- custom domains require DNS proof and TLS readiness before activation;
- audit evidence exports never include raw secrets, raw webhook signatures, or
  full private provider payloads.

### Observability Contract

Metrics:

- `attune_feedback_ingestion_total{source_type,result}`
- `attune_request_merge_total{result}`
- `attune_request_score_recomputed_total{reason}`
- `attune_public_subscription_total{kind,status}`
- `attune_public_notification_total{event_type,result}`
- `attune_delivery_artifacts_total{provider,type,status_category}`
- `attune_delivery_health_total{category}`
- `attune_external_mapping_preview_total{provider,result}`
- `attune_external_field_conflicts_total{provider,status}`
- `attune_provider_installation_qualification_total{provider,status}`
- `attune_provider_canary_total{provider,scenario,status,cleanup_status}`
- `attune_readiness_checks_total{area,status,severity}`
- `attune_slo_findings_total{area,status,severity}`

Dashboard rows:

- source ingestion health;
- request merge and duplicate pressure;
- public notification latency and failure rate;
- delivery artifact freshness;
- external sync run outcomes and conflict age;
- provider permission drift and rate budgets;
- canary pass/fail and cleanup status;
- readiness findings and evidence export history.

Alert conditions:

- provider webhook lag exceeds SLO;
- conflict age exceeds SLO;
- dead runs exist for a critical mapping;
- canary cleanup fails;
- public notification failures exceed threshold;
- public portal 5xx rate exceeds threshold;
- provider permission drift blocks required sync capabilities.

### Complete Test Matrix

Backend unit tests:

- scoring policy math and explanation diffs;
- request merge/split preview and commit behavior;
- public subscription dedupe and unsubscribe;
- release update publication and notification event generation;
- delivery artifact projection and stale marking;
- mapping transform validation and dry-run preview;
- per-field conflict classification and resolution;
- provider installation permission qualification;
- readiness check severity classification;
- canary cleanup success and failure recording.

PostgreSQL integration tests:

- tenant isolation for every new table;
- merge preserves votes, comments, evidence, subscriptions, and public slugs;
- scoring recomputes from account and evidence changes;
- public notifications enqueue exactly once under retry;
- delivery artifacts rebuild idempotently;
- mapping rule activation is versioned and auditable;
- conflict studio batch resolution updates all linked rows atomically;
- readiness evidence export redacts secrets;
- retention jobs prune eligible records without breaking audit references.

Console tests:

- provider installation wizard;
- repository/project picker;
- mapping dry-run preview and activation;
- conflict studio batch resolution;
- request merge/split preview;
- request score explanation diff;
- AI suggestion review queue;
- public release update editor and publish flow;
- subscription and unsubscribe diagnostics;
- custom domain and widget configuration;
- readiness center and evidence export.

Browser acceptance:

- submit public feedback, find similar request, vote, follow, receive status
  update, unsubscribe;
- create and merge duplicate public requests, then confirm redirect and
  preserved counts;
- install provider, qualify connection, create request-linked artifact, receive
  webhook, repair conflict, and inspect delivery graph;
- publish release update linked to shipped request;
- run provider canary, verify cleanup, export readiness evidence;
- deny access through custom access policy without leaking hidden request
  existence.

Provider tests:

- GitHub App installation token exchange and permission drift;
- GitHub Issues, comments, pull requests, releases, and project item reads;
- Jira issue and delivery progress projection;
- rate-limit and secondary-rate-limit retry;
- webhook signature, replay, dedupe, and malformed payload rejection;
- provider sandbox conformance for pull, push, comment, conflict, replay, and
  cleanup.

## Verification

Every implementation PR under this proposal should provide targeted verification
evidence. The complete target state is considered ready only when these gates
are met.

### Static and unit gates

```sh
go vet ./...
go build ./...
go test -race ./...
go mod tidy && git diff --exit-code go.mod go.sum
golangci-lint run ./...
scripts/lint-slog.sh --strict
scripts/lint-artifacts.sh --strict
scripts/lint-rawptr.sh
scripts/lint-errorcode.sh
scripts/lint-integration-layout.sh
PATH="/opt/homebrew/opt/node@22/bin:$PATH" corepack pnpm --dir console tsc -b --noEmit
PATH="/opt/homebrew/opt/node@22/bin:$PATH" corepack pnpm --dir console biome check
PATH="/opt/homebrew/opt/node@22/bin:$PATH" corepack pnpm --dir console exec vite build
PATH="/opt/homebrew/opt/node@22/bin:$PATH" corepack pnpm --dir console vitest run --coverage
PATH="/opt/homebrew/opt/node@22/bin:$PATH" corepack pnpm --dir console arch
```

### Integration and runtime gates

```sh
DOCKER_HOST=unix:///Users/phj/.docker/run/docker.sock make test-integration
DOCKER_HOST=unix:///Users/phj/.docker/run/docker.sock make runtime-smoke
DOCKER_HOST=unix:///Users/phj/.docker/run/docker.sock make public-board-smoke
```

Current targeted evidence for the GitHub delivery artifact path:

```sh
go test ./internal/externalsync/adapter/githubissue ./internal/repo/externalsync ./internal/service/externalsync ./internal/repo/customerrequest
go test -tags integration ./test/integration/postgres/externalsync -run TestGitHubProviderPullDeliveryArtifactsReachCustomerRequestGraph -count=1 -v
go test -tags integration ./test/integration/postgres/externalsync -count=1
go test -tags integration ./test/integration/postgres/customerrequest -count=1
```

### Browser acceptance gates

- Run the deployed stack against real PostgreSQL.
- Operate Console and public portal in a visible browser with mouse input for
  the high-risk happy paths and recovery paths.
- Capture evidence for:
  - provider installation and qualification;
  - webhook ping and signed event delivery;
  - issue/comment sync and conflict repair;
  - public request subscribe/unsubscribe;
  - duplicate merge;
  - release update notification;
  - custom access allow/deny;
  - readiness evidence export.
- Assert zero application console errors for each accepted path.

### Provider gates

- GitHub App sandbox install can create, update, comment on, close, and clean up
  a marked issue.
- GitHub webhook redelivery dedupes by delivery id and marker.
- Rate-limit and retry-after responses delay work without duplicating writes.
- Revoked permissions appear as qualification failures before writes are
  attempted.
- Provider canaries either clean up external artifacts or report cleanup
  failure as a readiness finding.

### Observability gates

- Dashboards show sync freshness, run success rate, conflict age, webhook lag,
  dead-run age, public portal error rate, and notification latency.
- SLO breach conditions create low-cardinality metrics and operator-visible
  findings.
- Audit evidence export includes relevant readiness checks without secrets.

### Remote CI gates

- The pull request shows all required checks passing.
- No required check is skipped or pending.
- Coverage workflows pass without lowering configured thresholds.
- Secret scan, CodeQL, dependency review, workflow lint, Docker build, Helm,
  compose, proto, SDK, public board smoke, and integration checks are green.

## References

- [Attune external sync framework](./2026-07-08-external-sync-framework.md)
- [Attune GitHub Issues bidirectional sync](./2026-07-20-github-issues-bidirectional-sync.md)
- [Attune public feedback platform gap analysis](../../../research/2026-07-13-public-feedback-platform-phase-2-roadmap.md)
- [GitHub Apps permissions](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app)
- [GitHub REST API rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)
- [GitHub webhook best practices](https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks)
- [GitHub webhook signature validation](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries)
- [GitHub Projects](https://docs.github.com/en/issues/planning-and-tracking-with-projects/learning-about-projects/about-projects)
- [Linear customer requests](https://linear.app/docs/customer-requests)
- [Linear GitHub integration](https://linear.app/docs/github)
- [Jira Product Discovery](https://www.atlassian.com/software/jira/product-discovery)
- [Productboard portals](https://support.productboard.com/hc/en-us/articles/360056315454-Getting-started-with-portals)
- [Productboard portal updates](https://support.productboard.com/hc/en-us/articles/360058173353-Close-the-feedback-loop-with-Portal-card-updates)
- [Canny public boards](https://help.canny.io/en/articles/3832293-public-boards)
- [Canny changelog](https://help.canny.io/en/articles/3006399-changelog)
- [Canny widget](https://help.canny.io/en/articles/1058407-the-canny-widget)
- [Featurebase collect and manage feedback](https://help.featurebase.app/articles/6728409-collect-and-manage-feedback)
- [Featurebase segmentation](https://help.featurebase.app/articles/9188570-how-to-segment-your-users)
- [Featurebase changelog notification emails](https://help.featurebase.app/articles/7999387-changelog-notification-emails)
- [UserVoice forum setup](https://help.uservoice.com/hc/en-us/articles/360035473053-Setting-up-a-Forum)
- [UserVoice moderation](https://help.uservoice.com/hc/en-us/articles/360035481633-Moderate-Ideas-and-Comments)
- [Pendo request moderation](https://support.pendo.io/hc/en-us/articles/360032949332-Moderate-requests)
- [Pendo similar requests](https://support.pendo.io/hc/en-us/articles/360032949632-Deal-with-similar-requests)
- [Unito field mapping](https://guide.unito.io/how-to-map-fields)
- [Exalate Jira GitHub integration](https://exalate.com/integrations/jira-github/)
- [Sentry GitHub integration](https://docs.sentry.io/product/integrations/source-code-mgmt/github/)
