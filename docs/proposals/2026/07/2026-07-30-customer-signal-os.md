<!-- markdownlint-disable MD013 -->

# Customer Signal OS

| Field | Value |
|---|---|
| Issue | [#202](https://github.com/Phixsura/attune/issues/202) (industry gap closure meta issue) |
| Status | Proposed |
| Started | 2026-07-30T11:16:19+08:00 |
| Related | [#236](https://github.com/Phixsura/attune/issues/236), [Post-resolution CSAT and CES Surveys](./2026-07-29-post-resolution-csat-ces-surveys.md), [Customer Requests](./2026-07-07-customer-requests.md), [Customer Request Decision Intelligence](./2026-07-07-customer-request-decision-intelligence.md), [External Sync Framework](./2026-07-08-external-sync-framework.md), [Public Roadmap From Workflow States](./2026-07-14-public-roadmap-from-workflow-states.md), [Close the Loop Request Notifications](./2026-07-16-close-the-loop-request-notifications.md), [Semantic Search Operator Workflow](./2026-07-02-semantic-search-operator-workflow.md), [Platform Maturity Program](./2026-07-05-platform-maturity-program.md) |

## Problem

Attune has evolved from a feedback ingestion service into a broader customer
signal platform. It can ingest user feedback, classify and enrich it with LLMs,
cluster similar reports, run operator workflow, draft replies, promote feedback
into Customer Requests, publish public request and roadmap surfaces, notify
request subscribers, and schedule post-resolution CSAT and CES surveys.

That foundation is strong, but it still reads as a capable MVP rather than a
world-class product system. The largest remaining product gap is not a single
missing API. The gap is that signals, evidence, prioritization, roadmap
communication, customer context, behavioral analytics, survey measurement, and
external delivery context are not yet experienced as one continuous operating
loop.

World-class products in this space converge on a larger shape:

- Product feedback tools turn feedback into evidence-backed ideas and roadmap
  decisions.
- Product analytics tools combine behavior, cohorts, session replay,
  experimentation, in-app guides, and surveys.
- Customer experience platforms turn survey and support signals into
  closed-loop action programs.
- Support and customer success systems preserve conversation, ticket, contact,
  account, and workflow context.
- Research repositories preserve qualitative evidence as reusable knowledge.
- Planning tools connect customer requests to issues, projects, releases, and
  roadmap state.

Attune has useful pieces in several of those categories, but they are not yet
bound into a coherent product promise. Operators can inspect feedback, requests,
public visibility, notifications, sync health, and survey APIs, yet they still
need to mentally assemble the core answer:

> What are customers asking for, who is affected, how valuable is the demand,
> what evidence supports it, what did we decide, did we deliver it, did we tell
> people, and did the outcome improve customer experience?

This proposal defines the product architecture for making that answer native to
Attune.

## Goals

- Position Attune as an open-source Customer Signal OS rather than a clone of a
  feedback board, survey tool, helpdesk, or product analytics suite.
- Define the product loop that connects signal capture, context resolution,
  evidence, insight, prioritization, roadmap communication, closed-loop
  notification, outcome measurement, and governance.
- Turn the benchmark against world-class products into concrete Attune product
  pillars and implementation slices.
- Preserve Attune's differentiators: self-hosted control, provider-neutral LLMs,
  generated API contracts, observability, auditability, and open-source
  deployment.
- Prefer productized workflows over isolated backend primitives.
- Sequence the work so early slices create visible operator value without
  forcing Attune to build a full product analytics suite, full helpdesk, full
  CRM, or full research repository.

## Non-goals

- Do not clone Productboard, Canny, Pendo, PostHog, Qualtrics, Medallia,
  Intercom, Zendesk, Dovetail, Linear, or Jira Product Discovery end to end.
- Do not replace external systems of record for helpdesk tickets, CRM accounts,
  product analytics events, or delivery issue tracking.
- Do not build arbitrary custom objects before the core signal loop is useful.
- Do not build a generic survey form builder as part of this strategy.
- Do not make LLMs the source of truth for prioritization, eligibility,
  consent, delivery state, or public visibility.
- Do not introduce a cloud-only dependency that weakens Attune's self-hosted
  positioning.

## Industry Benchmark

The benchmark covers more than 50 products grouped by product motion. The
details differ, but the strongest systems all convert fragmented customer
signals into product decisions with visible evidence.

| Category | Products reviewed | World-class pattern | Design signal for Attune |
|---|---|---|---|
| Product feedback and roadmap | Productboard, Canny, Aha!, UserVoice, ProductPlan, ProdPad, Featurebase, Frill, Savio, FeatureOS | Feedback portals, idea intake, voter/customer context, prioritization, roadmap state, status updates, changelog communication. | Attune should make requests, roadmap items, evidence, supporters, status communication, and survey outcomes one operator workflow. |
| Product analytics and product experience | Pendo, PostHog, Amplitude, Mixpanel, Heap, FullStory, Hotjar, Contentsquare, Statsig, LaunchDarkly | Behavioral events, funnels, cohorts, feature flags, experiments, session replay, in-app guidance, surveys. | Attune should bridge product behavior before attempting a full analytics engine; user actions should become evidence when they explain feedback demand. |
| Customer experience and survey platforms | Qualtrics, Medallia, SurveyMonkey, Sprig, Alchemer, Typeform, Delighted, Survicate, AskNicely, Formbricks | Multi-channel distribution, targeting, response analysis, low-score workflows, journey and account context, AI-assisted synthesis. | Attune surveys should become a Console product surface with targeting, diagnostics, trends, and recovery queues, not only public links and backend APIs. |
| Support and customer success | Intercom, Zendesk, HubSpot Service Hub, Freshdesk, Salesforce Service Cloud, ServiceNow CSM, Gainsight PX, Front, Help Scout, Gorgias | Conversations and tickets preserve customer, account, SLA, workflow, knowledge-base, AI agent, and satisfaction context. | Attune should ingest and synchronize context from these systems instead of trying to become a complete support desk. |
| Research repositories | Dovetail, Maze, UserTesting, Condens, EnjoyHQ | Interviews, usability tests, quotes, clips, tags, transcripts, repositories, and AI synthesis become durable research assets. | Attune should support evidence artifacts and citations without over-expanding into full participant recruitment or testing workflows. |
| Planning and execution | Linear, Jira Product Discovery, GitHub Issues and Projects, DevRev, Shortcut | Customer requests connect to ideas, issues, projects, roadmaps, releases, and delivery health. | Attune should treat external issue links, roadmap state, and release communication as part of the evidence chain. |

## World-class Product Review

The first version of this proposal correctly identified the Customer Signal OS
direction, but it still leaned too much toward platform architecture. A second
review against world-class products exposed several product gaps that need to be
corrected before implementation begins.

| Review lens | World-class benchmark signal | Proposal gap | Correction |
|---|---|---|---|
| Productboard, Canny, Aha!, UserVoice | Product teams do not just collect feedback; they turn it into decisions, launch communication, and visible customer follow-through. | The proposal covered requests and public roadmap, but underweighted changelog-style launch communication and cross-functional views for Sales, CS, Engineering, and leadership. | Add a public trust package and stakeholder views so roadmap state, launch notes, supporters, request evidence, and survey outcome live in the same loop. |
| Pendo, PostHog, Amplitude, Mixpanel | Product analytics products connect events, cohorts, session replay, flags, experiments, alerts, dashboards, and AI queries. | The behavior bridge was directionally right but too vague about whether Attune should import, store, or compute behavioral data. | Make behavior import-first, aggregate-first, and optional. Attune should cite behavior as decision evidence, not compete as a full analytics warehouse. |
| Qualtrics, Medallia, Sprig, SurveyMonkey | Customer experience systems route feedback into accountable action workflows, not only reports. | The closed-loop center had surveys and low-score review, but lacked explicit ownership, SLA, and recovery-case semantics. | Treat low survey scores, failed notifications, and reopened demand as closed-loop cases with owner, severity, deadline, resolution, and evidence. |
| Intercom, Zendesk, HubSpot, Freshdesk | Support systems preserve conversation, ticket, contact, knowledge-base, and satisfaction context. | The source activation plan named connectors but did not specify how support context should remain bounded. | Import support systems as source context with privacy filters, redaction, conversation references, and status snapshots; do not recreate ticket assignment or helpdesk SLA ownership. |
| Dovetail, Maze, UserTesting, Condens | Research tools preserve clips, quotes, transcripts, tags, reports, and cited AI synthesis. | The evidence workbench covered citations, but did not define a research-lite artifact path. | Add evidence artifacts for interview quotes, notes, clips, transcripts, and research reports, with source-level retention and redaction controls. |
| Linear, Jira Product Discovery, GitHub Projects, DevRev | Planning tools make prioritization visible through views, metadata, automation, status updates, and links to execution. | The decision cockpit covered scoring and scenarios, but did not define daily operator views and meeting artifacts sharply enough. | Add saved planning views, weekly review exports, scenario snapshots, and status-update evidence bundles. |
| DevRev, Productboard Spark, Dovetail AI, PostHog AI | AI is strongest when it reasons across live context and acts through governed tools. | The AI analyst pillar was broad but not specific about memory, permissions, or action boundaries. | Store AI suggestions as reviewable objects with citations, tool-scope policy, accepted/dismissed feedback, and no automatic public mutation. |
| Product-led onboarding across the category | Top products make first value visible quickly through templates, wizards, sample data, import previews, and guided next steps. | The proposal put source activation first but did not define a measurable first-value promise. | Add a first-value scorecard: connected source, imported signals, generated insight, linked request, published preview, closed-loop test, and operator invite. |

The corrected strategy is narrower:

> Attune should become the self-hosted operating layer for evidence-backed
> customer signal decisions, not the system of record for every adjacent
> workflow.

This keeps the proposal ambitious without making it unfocused. Attune should
own the connective tissue: signal normalization, evidence linking, customer and
account context, decision support, public-safe communication, measurement, and
governance.

### Competitive boundary

The comparison only helps if it sharpens what Attune must own and what it should
borrow from the ecosystem.

| Capability | Attune must own | Attune should integrate |
|---|---|---|
| Feedback and request evidence | Normalize, enrich, cluster, cite, and link evidence to decisions. | Import from support, sales, community, and feedback portals where customers already work. |
| Product behavior | Store selected aggregates and cite them in decisions. | Import events, cohorts, flags, experiments, and replay references from analytics systems. |
| Account value | Preserve account context, segment labels, and revenue signals used for prioritization. | Sync CRM and customer success systems as the source of truth for account ownership and lifecycle. |
| Roadmap communication | Own public-safe request, roadmap, notification, and outcome-measurement loops. | Link to delivery planning systems and release-management tools. |
| Surveys | Own transactional CSAT and CES campaigns tied to Attune decisions. | Leave broad market research, panels, long-form survey branching, and advanced statistical tooling to survey platforms. |
| AI | Own cited suggestions, summarization, duplicate detection, and decision explanations. | Use provider-neutral LLM routes and externally governed tools rather than embedding one closed AI stack. |
| Governance | Own audit, RBAC, retention, public-surface privacy, and deployment trust evidence. | Sync identity, secrets, SIEM, and compliance tooling where operators already have systems of record. |

This boundary prevents feature parity from becoming the goal. The goal is a
coherent decision layer that makes other systems more useful.

## Cross-product Patterns

### 1. Signal breadth becomes a moat

The strongest products collect signals across feedback portals, support
tickets, sales notes, surveys, product analytics, calls, community threads,
research sessions, and issue trackers. A narrow inbox is not enough once a team
starts making roadmap decisions from the data.

Attune's implication: keep the normalized feedback pipeline, but add bridge
layers for product events, surveys, support systems, CRM/account enrichment, and
research artifacts.

### 2. Identity and account context are part of the product

Raw text only answers "what was said." Product teams also need to know who said
it, what account they belong to, how valuable or strategic the account is, what
segment they represent, and whether the signal repeats across many accounts.

Attune's implication: account and customer context should become a first-class
signal graph, not scattered optional fields on separate objects.

### 3. Evidence needs citation, freshness, and quality

Top tools do not merely summarize demand. They show evidence: quotes, request
links, tickets, votes, survey responses, product events, related issues, and
source timestamps.

Attune's implication: every generated insight, priority score, and roadmap
recommendation should cite bounded evidence and expose when that evidence is
stale, low-confidence, duplicated, or missing important context.

### 4. Product analytics and qualitative feedback are converging

Pendo, PostHog, Amplitude, Mixpanel, Hotjar, FullStory, and Sprig demonstrate
the same trend: product teams want to compare what customers say with what they
do. Feedback without usage can overvalue loud requests. Usage without feedback
can miss motivation.

Attune's implication: do not attempt to outbuild analytics suites immediately.
Add product-event ingestion and connector bridges that enrich Attune decisions
with selected behavior signals.

### 5. Roadmaps are communication systems

World-class roadmap tools use roadmap state to align internal teams and close
the loop with customers. The roadmap is not only a display surface; it is a
commitment, a notification trigger, and a customer trust mechanism.

Attune's implication: public roadmap, request notifications, surveys, and
delivery issue status should feed each other.

### 6. Closed loop means operational ownership

Survey low scores, status updates, bounced notifications, stale issue links, and
reopened requests all create work. Mature products provide queues, saved views,
owners, SLAs, health diagnostics, and recovery paths.

Attune's implication: every customer-facing loop should have an operator-facing
recovery surface.

### 7. AI is useful only when it is explainable and actionable

Leading products increasingly advertise AI analysis, synthesis, replies, and
agent workflows. The durable product pattern is not "AI writes text"; it is
"AI reduces review effort while preserving evidence and human control."

Attune's implication: AI analyst features should create suggestions with
citations, confidence, and reversible operator actions.

### 8. Self-serve activation is a product feature

World-class products make the first useful result visible quickly through
templates, demo data, importers, connectors, previews, and guided setup. A
technically correct backend that requires database knowledge or manual wiring
does not activate a product team.

Attune's implication: first-value flows must become as important as ingestion
and enrichment internals.

## Current State

| Area | Attune today | Product gap |
|---|---|---|
| Ingestion | Public API, Node and Go SDKs, HTTP webhook, email, Slack, inbound source framework. | No guided source setup that turns a fresh install into useful insights quickly. |
| Enrichment | LLM classification, sentiment, language, modules, severity, semantic search, clustering, reply draft. | AI output is not yet packaged as an insight analyst with evidence, confidence, and suggested actions. |
| Customer Requests | Curated requests, feedback evidence, supporters, votes, account and revenue fields, decision score, structured score factors, account summaries, and audit-backed decision records. | Request decisioning is not yet the central cockpit for prioritization, scenarios, and roadmap tradeoffs. |
| Public surfaces | Public request board, request detail, voting, comments, public roadmap, moderation policy. | Public surfaces are not fully connected to outcome measurement, changelog-style communication, or customer timelines. |
| Closed loop | Request notifications, unsubscribe, consent, provider events, survey foundation. | Operators still lack a unified closed-loop center for campaigns, delivery health, low-score recovery, and customer follow-through. |
| Surveys | CSAT and CES campaign API, hosted links, invitation and response persistence, analytics aggregate, low-score review model. | Console survey product surface, targeting builder, diagnostics funnel, and recovery workflow are missing or incomplete. |
| External sync | Generic framework for connections, mappings, runs, failures, conflicts, and issue-link health. | Priority connectors need buyer-facing workflows and setup diagnostics, not only a platform framework. |
| Analytics | Usage, LLM usage, classification quality, search quality, API-key analytics, operational dashboards. | No product behavior event model, funnels, cohorts, adoption, or session context. |
| Governance | RBAC, audit log, GDPR, guardrails, security surfaces, observability, production safety work. | Enterprise trust is not yet packaged as a single buyer-visible control plane. |

## Proposal

Attune should define and build toward a Customer Signal OS. The product promise:

> Attune connects fragmented customer signals to product decisions, roadmap
> communication, and customer outcomes with self-hosted control and
> evidence-backed AI.

The system should be organized around one operating loop:

```text
Capture -> Resolve -> Link -> Understand -> Decide -> Communicate -> Measure -> Govern
```

- **Capture:** collect feedback, support tickets, survey responses, product
  events, public votes, research evidence, sales notes, and external issue
  state.
- **Resolve:** map raw signals to customers, accounts, segments, source systems,
  workflow states, consent state, and product areas.
- **Link:** attach evidence to Customer Requests, insights, roadmap items,
  delivery issues, and survey campaigns.
- **Understand:** cluster, summarize, classify, detect trend shifts, and expose
  citations.
- **Decide:** prioritize requests by demand, account value, revenue impact,
  evidence quality, effort, confidence, and roadmap fit.
- **Communicate:** publish public roadmap state, notify supporters, send
  customer updates, and expose public-safe request pages.
- **Measure:** collect CSAT/CES, response rates, low-score recovery state,
  reopened demand, and post-delivery feedback.
- **Govern:** apply RBAC, audit, retention, source health, connector health,
  model configuration, and deployment readiness controls.

### Operating wedge

The wedge is not "feedback boards" and it is not "product analytics." The wedge
is the decision record between them:

```text
Customer said X
Customer did Y
Account context says Z
Evidence supports request R
Team decided D
Roadmap state became S
Customer was notified N
Outcome measured O
```

Attune should make that chain visible, queryable, reviewable, and safe to share.
This is where open-source control and evidence-backed AI are defensible.

The first shippable product promise should be:

> Connect one real customer source, generate evidence-backed product themes,
> promote the strongest theme into a Customer Request, publish a public-safe
> roadmap preview, and verify the closed loop with a test notification or survey.

### Primary users

| User | Job | Required product surface |
|---|---|---|
| Product manager | Decide what to build and explain why. | Insights, Customer Requests, decision cockpit, roadmap scenarios, evidence export. |
| Product operations | Keep intake, taxonomy, workflows, connectors, and reporting trustworthy. | Source activation, signal graph, governance, health diagnostics, saved views. |
| Customer success or support lead | Make sure customer pain is captured and follow-up happens. | Support imports, customer timelines, request supporters, closed-loop center, low-score cases. |
| Founder or product leader | See whether the team is learning from customers and shipping the right work. | Control Tower, roadmap health, decision score trends, account impact, outcome measurement. |
| Developer or engineering lead | Understand customer context behind planned work. | Delivery issue links, evidence bundles, roadmap state, request context, external sync health. |
| Security or IT owner | Trust the deployment, data handling, and automation boundaries. | Governance, audit, RBAC, retention, connector permissions, LLM route controls. |

### Product scorecard

Every implementing slice should move at least one of these product metrics:

| Metric | Why it matters |
|---|---|
| Time to first insight | Measures whether a fresh tenant reaches visible value quickly. |
| Source activation completion rate | Measures whether setup is productized rather than support-heavy. |
| Signal identity resolution coverage | Measures whether signals are attached to people, accounts, and segments. |
| Insight citation coverage | Measures whether AI and summaries remain evidence-backed. |
| Feedback-to-request link rate | Measures whether raw signal becomes decision-ready work. |
| Request evidence depth | Measures whether roadmap decisions are supported by enough distinct sources. |
| Account-scoped request review coverage | Measures whether operators can inspect demand by affected account instead of manually searching raw evidence. |
| Roadmap decision explainability | Measures whether operators can explain priority and placement. |
| Closed-loop completion rate | Measures whether published changes lead to customer follow-through. |
| Feedback SLA escalation coverage | Measures whether open feedback with overdue, soon-due, missing-owner, or missing-SLA risk is visible as owned operational work. |
| Survey response and recovery rate | Measures whether outcome measurement creates action. |
| Connector health and freshness | Measures whether external context can be trusted. |
| Weekly active operators | Measures whether the product becomes a daily or weekly habit. |

These metrics should appear in Control Tower and in release notes for product
slices that claim Customer Signal OS progress.

Initial Control Tower coverage combines classification quality, search quality,
index coverage, and survey closed-loop recovery into one operating scorecard.
The closed-loop lane uses survey analytics to surface recovery readiness,
overdue low-score follow-up, open recovery load, and response rate without
calling survey administration APIs for roles that cannot read them. Control
Tower also includes a world-class readiness matrix that maps live operating
signals to explicit product standards for signal understanding, semantic
discovery, closed-loop operations, action accountability, and release
verification, so reviewers can see blocked, watch, pass, and insufficient-data
states instead of treating the scorecard as a finished-product claim. Control
Tower also includes a first-value activation scorecard that proves whether a
tenant has connected a source, captured feedback, generated trustworthy
insight, made semantic discovery usable, and tested the closed-loop path. A
source health command center breaks intake into enabled sources, freshness,
source-level errors, never-seen sources, disabled sources, and the next intake
repair action. A closed-loop recovery command center breaks survey recovery
into overdue SLA work, unassigned recovery, pending customer contact,
root-cause/action evidence debt, owner workload, and the next prioritized
recovery action. A release verification evidence center joins runtime release
blockers with the product evidence contract for proposal coverage, product
contract linting, unit coverage, browser coverage, bundle budget, and
release-smoke wiring. A world-class maturity gap register turns the top 100
benchmark gaps into a visible Control Tower register grouped by capture,
identity, evidence, request decisions, closed-loop operations, operator
workflow, AI governance, reliability, governance, and developer-platform
maturity. A world-class execution queue selects the highest-priority maturity
gaps and attaches owner, acceptance evidence, and verification scope so the gap
register becomes an executable backlog rather than a static self-assessment.
As covered slices land, the register promotes them to verified evidence and
removes them from the active execution queue; the durable feedback identity
graph, account/company model, request evidence-quality score, request decision
records, status-level notification evidence, operator batch actions, and AI
review learning, and end-to-end signal trace are promoted slices, so operators
keep seeing remaining world-class gaps instead of already-verified foundations.
The AI review learning slice adds a classification review learning ledger for
accepted, edited, and dismissed AI classification samples, records audited
operator review events with optional correction JSON, exposes a learning
summary through the generated classification-quality contract, and renders the
review controls plus learning summary in Console. Control Tower promotes the
AI accept/dismiss learning gap only when that proto, database, audit, handler,
Console, unit, and browser evidence remains wired.
The end-to-end signal trace slice persists a durable `signal_trace_id` on every
feedback row, derives it from source metadata when providers already have an
event anchor, passes the same ID into enrichment jobs, and exposes a generated
Feedback Signal Trace endpoint. The trace projection joins source, enrichment,
request, notification, and survey tables into ordered stages with terminal
status, missing-stage evidence, and bounded event metadata. Console renders the
trace in feedback details, MSW and browser fixtures exercise the same endpoint,
and Control Tower promotes `reliability_end_to_end_trace` only while the
contract, migration, repository, handler, route, UI, unit, browser, and product
contract evidence remain wired.
The replay/backfill reliability slice turns the static SLO worksheet into an
operator drill inside the Reliability page: each product SLO now shows its
replay lens, entry point, action, evidence artifact, owner, escalation path, and
status derived from system readiness, recovery, release lifecycle, GDPR pressure,
and dead-delivery pressure. Browser coverage opens the Reliability route with
system preflight, recovery, release, GDPR, API-key, MCP, and outbox mocks so the
drill is a real product path instead of an unmounted component. Control Tower
promotes `reliability_backfill_replay` only while the model, card, route, unit
test, browser test, product contract, proposal, and changelog evidence stay
wired; the next execution slice becomes `reliability_error_budget`.
The error-budget reliability slice adds an auditable burn-rate ledger inside the
Reliability page. Each generated product SLO now shows its objective, monthly
budget allowance, fast/slow burn thresholds, Prometheus burn query,
remaining-budget query, exception policy, incident evidence, runbook, owner,
escalation path, and current runtime signal. The ledger explicitly marks missing
operational inputs as data gaps instead of silently treating them as healthy,
and it blocks every row when release lifecycle evidence says the runtime is not
ship-ready. Control Tower promotes `reliability_error_budget` only while the
model, UI, route wiring, unit coverage, browser smoke, product contract,
proposal, and changelog evidence stay wired; the next execution slice becomes
`reliability_release_health`.
The release-health reliability slice adds a release correlation ledger inside
the Reliability page. The route now preloads feedback stats and request
notification status evidence alongside system release and restore-drill context,
so runtime version, lifecycle state, restore result, feedback pressure, and
notification failures are visible in one release decision. The model keeps each
lane as ready, needs-attention, blocked, or data-missing evidence and avoids
treating missing release, feedback, or notification data as healthy. Control
Tower promotes `reliability_release_health` only while the loader, model, UI,
unit coverage, browser smoke, product contract, proposal, and changelog evidence
stay wired; the next execution slice becomes `reliability_incident_timeline`.
The incident-timeline reliability slice reconstructs production incidents inside
the Reliability page. The model links incident start, readiness detection,
customer impact, mitigation pressure, restore-drill recovery, and customer
notification status into one ordered timeline with owner, action, signal,
evidence, timestamp, status, and an incident fingerprint. It keeps missing
release, readiness, feedback, mitigation, recovery, or notification evidence as
explicit data gaps, and blocks the timeline when lifecycle, impact, mitigation,
recovery, or customer-notification pressure is unsafe. Control Tower promotes
`reliability_incident_timeline` only while the model, UI, route wiring, unit
coverage, browser smoke, product contract, proposal, and changelog evidence stay
wired; the next execution slice becomes `reliability_tenant_quota_dashboard`.
The tenant-quota reliability slice adds a tenant capacity and saturation
dashboard inside the Reliability page. The model joins ingest usage quota,
API-key per-key RPM coverage, LLM provider error pressure, MCP client rpm/burst
coverage, GDPR workload pressure, and outbox dead-letter saturation into one
tenant boundary with capacity, consumption, evidence, guardrail, action, owner,
status, and a quota fingerprint. It treats missing quota contracts and
unbounded active clients as explicit risk evidence instead of healthy defaults.
Control Tower promotes `reliability_tenant_quota_dashboard` only while the usage
and LLM loaders, model, UI, route wiring, unit coverage, browser smoke, product
contract, proposal, and changelog evidence stay wired; the next execution slice
becomes `reliability_backup_restore_drill`.
The backup-restore reliability slice adds a recovery evidence center inside the
Reliability page. The model joins backup freshness, restore execution, migration
readiness, runbook ownership, and remediation evidence into one tenant recovery
proof with status, signal, guardrail, owner, action, and fingerprint. It treats
missing backup references, stale restore windows, absent compatibility rules,
and missing owner/runbook/escalation evidence as visible data gaps or risk
states instead of healthy defaults. Control Tower promotes
`reliability_backup_restore_drill` only while recovery, release, and preflight
evidence stay wired through the model, UI, route, unit coverage, browser smoke,
product contract, proposal, and changelog; the next execution slice becomes
`reliability_consistency_checks`.
The consistency-check reliability slice adds a data consistency evidence center
inside the Reliability page. The model joins ingest usage, feedback stats,
customer request projections, request notification status evidence, survey
analytics, and low-score recovery queues into one auditable chain with status,
signal, guardrail, owner, action, and fingerprint. It treats missing aggregates,
orphaned requests, failed or recovery-pending notifications, impossible survey
counts, and unassigned or overdue recovery work as visible data gaps or risk
states. Control Tower promotes `reliability_consistency_checks` only while the
customer request and survey loaders, model, UI, route wiring, unit coverage,
browser smoke, product contract, proposal, and changelog evidence stay wired;
the next execution slice becomes `reliability_pipeline_slo`.
The pipeline-SLO reliability slice adds a pipeline SLO ledger inside the
Reliability page. The model joins ingest usage, LLM enrichment usage, outbox
dead-letter recovery, request sync projection health, preflight evidence, and
release lifecycle state into four auditable pipeline lanes: ingest, enrich,
outbox, and sync. Each lane carries objective, burn signal, release gate,
owner, escalation, runbook, action, evidence, and status, treating missing
telemetry, failed release gates, enrichment errors, unrecovered dead letters,
and stale or failed sync projections as visible gaps. Control Tower promotes
`reliability_pipeline_slo` only while the model, UI, route wiring, unit
coverage, browser smoke, product contract, proposal, changelog, and bundle
budget evidence stay wired; the next execution slice becomes
`governance_sso_scim_rbac`.
The governance/RBAC readiness slice adds a Security evidence center that joins
auth mode, break-glass token inventory, lockout evidence, member role/source
coverage, last-admin continuity, and bounded member audit snapshots. The model
treats missing SSO evidence, missing break-glass coverage, unmanaged member
sources, narrow role breadth, single-admin continuity, and absent access-review
events as explicit product risks instead of burying them in separate admin
tables. The Security route preloads the governance evidence, browser smoke
verifies the card on the real page, Control Tower promotes
`governance_sso_scim_rbac`, and the next execution slice becomes
`governance_field_level_permissions`.
The field-level permissions slice adds a Security ledger that joins the
central role permission matrix, public visibility policy, public write and
identity modes, moderation/redaction queue state, and public moderation audit
snapshots. The ledger treats public auto-approval, anonymous auto-public
submissions, visible submitter identity, public timestamps, pending moderation,
and missing audit evidence as explicit product risks. Security route preload,
unit coverage, browser smoke, product contract, proposal, and changelog now keep
`governance_field_level_permissions` wired; the next execution slice becomes
`governance_public_privacy_preflight`.
The public privacy preflight slice adds a Public Visibility evidence center that
joins public access mode, enabled public surfaces, search indexing, default
moderation gates, submitter identity exposure, public timestamps, portal
submission fields, page-URL collection, and review recovery state. The model
treats public access, auto-approved requests or comments, visible submitter
identity, visible timestamps, required or page-URL portal fields, pending
moderation, and unreasoned blocked decisions as explicit publication risks.
Public Visibility render coverage, browser smoke, product contract, proposal,
and changelog now keep `governance_public_privacy_preflight` wired; the next
execution slice becomes `governance_retention_legal_hold`.
The retention/legal-hold slice adds a GDPR evidence center that joins tenant
retention policy, legal-hold gate, delete grace window, export artifact
residue, backup-retention residue, hashed audit residue, and visible GDPR
request records. The model treats missing retention windows, unbounded export
artifacts, scheduled deletes without legal-hold support, zero delete grace, and
non-hashed audit residue as explicit compliance risks. GDPR render coverage,
browser smoke, product contract, proposal, and changelog now keep
`governance_retention_legal_hold` wired; the next execution slice becomes
`governance_compliance_package`.
The compliance-package slice adds a Security evidence center that joins auth
mode, RBAC and member audit evidence, break-glass continuity, public data-flow
inventory, public moderation evidence, audit evidence export availability, GDPR
retention and data-subject request controls, and outbound notification target
boundaries. The model treats missing package inputs, weak admin continuity,
hybrid auth, missing break-glass coverage, public exposure, missing audit
events, retention or legal-hold risk, non-hashed audit residue, non-HTTPS
outbound targets, failing targets, and empty outbound inventory as explicit
compliance package risks. Security render coverage, route preload coverage,
browser smoke, product contract, proposal, and changelog now keep
`governance_compliance_package` wired; the next execution slice becomes
`governance_key_rotation_ui`.
The key-rotation slice adds a Security evidence center that joins system
preflight Tink keyset and decryptability checks, API key expiry/grace/boundary
inventory, inbound webhook source rotation readiness, outbound notify-target and
reply-hook secret boundaries, and LLM provider managed credential/test evidence.
The model treats missing runtime keyset evidence, failing encryption checks,
active API keys without scopes, never-expiring or grace-period API keys, weak
API-key network or rate boundaries, failing webhook sources, non-HTTPS outbound
targets, outbound delivery failures, enabled bearer LLM channels without keys,
and untested or failing LLM provider channels as explicit rotation risks.
Security render coverage, route preload coverage, browser smoke, product
contract, proposal, and changelog now keep `governance_key_rotation_ui` wired;
the next execution slice becomes `governance_webhook_signature_tooling`.
The webhook-signature tooling slice adds a Security evidence center that joins
inbound webhook source health and rotation fixtures, reply-hook URL fingerprint
and delivery probe evidence, request-notification webhook signature-version,
test, identity-inclusion, and delivery evidence, external-sync webhook-secret
and event signature status, and cross-surface failure diagnostics. The model
treats missing signature inputs, enabled inbound source failures, reply hooks
without fingerprints, failed or retryable reply deliveries, request webhooks
without signature version or test evidence, webhook delivery failures, external
sync enabled connections without webhook secrets, explicit signature failures,
and failures without trace, digest, error, fingerprint, or replay artifacts as
risks. Security render coverage, route preload coverage, browser smoke, product
contract, proposal, and changelog now keep
`governance_webhook_signature_tooling` wired; the next execution slice becomes
`governance_security_runbook`.
The security incident-runbook evidence center adds a Security rehearsal surface
that joins credential compromise readiness, webhook signature incident handling,
access and identity incident continuity, public privacy incident controls, and
customer notification recovery. The model treats missing keyset, API-key, LLM,
break-glass, webhook, access, audit, public-surface, GDPR, outbound target, and
request notification evidence as needs-data, and promotes failing preflight
checks, unscoped active keys, missing bearer LLM secrets, explicit signature
failures, unsigned request webhooks, SSO-only deployments without break-glass,
public auto-approval, terminal public moderation states, non-HTTPS outbound
targets, and missing customer notification channels to blocked. Security render
coverage, route preload coverage, browser smoke, product contract, proposal,
and changelog now keep `governance_security_runbook` wired; the next execution
slice becomes `developer_openapi_sdk_examples`.
The developer API adoption kit evidence center adds a developer-platform
control surface to the API Keys page that joins generated OpenAPI coverage,
scope and preset registry evidence, Node SDK packaging and live/browser smoke
coverage, Go SDK module and live e2e coverage, example-app paths, deterministic
demo bootstrap, service-account automation identity, and webhook replay
artifacts. The model treats missing contract metadata or asset coverage as
blocked, missing page evidence as needs-data, and unlinked automation identity
or absent live keys as watch, so external integrators can see whether the API is
ready to adopt rather than only whether a key can be issued. Console render
coverage, browser smoke, product contract, proposal, and changelog now keep
`developer_openapi_sdk_examples` wired; the next execution slice becomes
`developer_sdk_parity`.
The developer SDK parity gate evidence center adds a developer-platform
verification surface and repository gate that prove Node and Go SDKs stay
aligned across public management methods, generated error envelopes, transport
error categories, retry and Retry-After behavior, stable Idempotency-Key
handling, browser-safe key boundaries, package metadata, generated types,
packed browser smoke, and live e2e entrypoints. The repository verifier reads
SDK source, README, tests, e2e scripts, and package metadata directly, while
the API Keys page shows the resulting parity lanes beside the adoption kit so
operators can see whether SDK maturity is verified, watchlisted, blocked, or
missing evidence. Console render coverage, browser smoke, product contract,
proposal, and changelog now keep `developer_sdk_parity` wired; the next
execution slice becomes `developer_connector_sdk`.
The connector conformance slice turns external integrations from ad hoc
adapters into a replayable platform contract. The
`integrations/connector-conformance` tree defines the required connector
lifecycle hooks, a provider
manifest, signed GitHub Issues webhook fixtures, fixture replay expectations,
field-mapping requirements, and a provider error recovery matrix. The
repository verifier validates install metadata, HMAC signatures, normalized
payload output, mapped required fields, and retry/reauthorize/dead-letter
classification using only local fixtures. External Sync renders the same
contract as a live gate beside tenant connection, mapping, schema, event, run,
and health evidence, so operators can distinguish verified, watchlisted,
blocked, and missing connector-readiness lanes before enabling broader
integration rollout. Console render coverage, browser smoke, product contract,
proposal, and changelog now keep `developer_connector_sdk` wired; the next
execution slice becomes `developer_field_mapping_ui`.
The field mapping workbench slice replaces JSON-only integration mapping
evidence with an operator-readable control surface. External Sync now computes
a selected-mapping workbench from connection, schema, mapping, run, and health
state, then shows provider schema diff, required Attune field coverage, status
lifecycle mapping, preview/backfill safety, rollback recovery posture, and a
field-by-field matrix with saved, suggested, missing, and drifted states. This
does not pretend every tenant mapping is healthy; live failed records and open
conflicts remain watch signals while missing required fields and status gaps
block the mapping lane. Console model tests, render tests, browser smoke,
product contract, proposal, and changelog now keep
`developer_field_mapping_ui` wired; the next execution slice becomes
`developer_api_consistency`.
The developer API consistency contract evidence center turns API semantics into
a first-class developer-platform gate rather than relying on handwritten
examples. The repository verifier now reads OpenAPI, proto-generated contract
artifacts, Node SDK source and tests, and Go SDK source and tests to pin
pagination surfaces, console mirrors, filter wire names, Customer Request sort
enums, shared ErrorResponse parsing, Idempotency-Key handling, SDK pagers, and
exact query-string fixtures for repeated actions, `request_type`, and
`before_id`. The API Keys page renders the same contract beside adoption and
SDK parity evidence, so operators can see whether public API semantics are
verified, watchlisted, or blocked before teams automate imports or exports.
Console model tests, render tests, browser smoke, product contract, proposal,
and changelog now keep `developer_api_consistency` wired; the next execution
slice becomes `developer_import_export_ui`.
The developer import/export workbench evidence center gives Product Ops and
developer teams a visible, fixture-backed path for bulk data movement. The
workbench joins CSV and JSON templates, import and export coverage, schema
preview, required-field mapping, dry-run create/update/reject diffs, rejected
row recovery classes, permission scopes, PII redaction, and audit events for
dry-run, commit, and export download. The repository verifier validates the
fixture manifest, CSV and JSON samples, required schema mappings, dry-run row
actions, recovery matrix, permission scopes, and audit event inventory before
the UI can claim readiness. Console model tests, render tests, browser smoke,
product contract, proposal, and changelog now keep
`developer_import_export_ui` wired; the next execution slice becomes
`developer_integration_catalog`.
The developer integration catalog evidence center turns connector availability
from a static list into a verifier-backed marketplace surface. The
`integrations/integration-catalog` tree now declares Jira, GitHub, Intercom,
Zendesk, Salesforce, HubSpot, custom webhook, and CSV connector cards with
install states, auth modes, setup checks, permission scopes, data classes,
health signals, sample replay fixtures, audit events, versioned upgrade paths,
and rollback evidence. The repository verifier validates the manifest, every
required connector, replay fixture shape, permission map, health badge, install
state, and upgrade path before the UI can claim catalog readiness. External Sync
renders the same evidence next to live tenant connection health so operators can
see catalog coverage, installed providers, permission boundaries, unhealthy
tenant connectors, replay evidence, and upgrade readiness in one place. Console
model tests, render tests, browser smoke, product contract, proposal, and
changelog now keep `developer_integration_catalog` wired; the next execution
slice becomes `developer_upgrade_diagnostics`.
The developer upgrade diagnostics evidence center turns scattered install,
permission, schema, webhook, replay, and version signals into one executable
upgrade decision. The `integrations/upgrade-diagnostics` tree now defines six
diagnostic checks, connector compatibility rows, recovery playbooks, and fixture
cases for GitHub, CSV, and Salesforce upgrade scenarios. The repository verifier
validates the diagnostic manifest, required checks, compatibility matrix,
playbook coverage, and fixture expectations before the UI can claim upgrade
readiness. External Sync renders the same diagnosis beside catalog,
conformance, and field-mapping evidence, so operators can see which connector
health, permission, schema drift, webhook signature, replay fixture, or rollback
condition blocks or watchlists an upgrade and what recovery step to run next.
Console model tests, render tests, browser smoke, product contract, proposal,
and changelog now keep `developer_upgrade_diagnostics` wired; the next execution
slice becomes `developer_north_star_metrics`.
The request
decision-record slice preserves status-change rationale, owner identity,
evidence bundle references, and public-safe review state in the audit snapshot,
projects them through the Console API, and renders them in the request detail
timeline. The status-level notification evidence slice projects request-status
closed-loop counts for expected, notified, failed, suppressed, and
recovery-pending customers through the generated notification contract,
backend aggregation, Console route preload, visible status evidence table, and
browser accessibility coverage.
The first identity slice adds a feedback-detail identity evidence projection
that normalizes `user_id` plus `source_meta` email, external ID, source contact
ID, CRM ID, and support ID signals before deeper graph tables are introduced.
It now also adds a deterministic identity-resolution assessment with strength,
recommended action, stable-key/source-path counts, missing identity kinds, and
risk reasons so operators can judge merge readiness before the full reversible
merge and split workflow exists.
The next identity slice adds a feedback identity merge review queue that groups
recent feedback by stable identity keys, separates merge candidates from weak
evidence, and lets operators inspect supporting feedback before approving
durable subject merges.
The durable identity slice introduces tenant-scoped `signal_subjects`,
`signal_subject_identities`, and `signal_subject_merge_events`, with an audited
review-merge action from the Feedback workbench. Approved merge candidates now
create or reuse a durable signal subject, attach reviewed identity evidence,
record the supporting feedback IDs, and write `signal_subject.merge` audit
events with hashed identity values. The same workbench now surfaces recent
reviewed merges and provides an audited split action that revokes the selected
identity link, refreshes the subject's primary identity, and writes
`signal_subject.split` evidence so identity resolution remains reversible. The
review response now also exposes an active subject roster with subject,
identity, and evidence totals plus top resolved subjects, turning the graph into
an operator-inspectable asset before the full Accounts workspace exists.
Operators can open a roster subject to inspect active and revoked identities
plus merge/split timeline events with bounded feedback evidence excerpts, so
identity resolution has a reviewable history instead of only a current-state
summary or opaque feedback IDs.
A feedback triage command center adds an accountable queue projection above the
Feedback workbench. It groups urgent open feedback, untriaged rows, stalled
active work, terminal enrichment failures, and identity evidence debt into
owner lanes with SLA hours, overdue and due-soon counts, recommended actions,
filter queries, and bounded sample feedback IDs. This turns the raw feedback
list into an operator-visible execution system. The durable feedback owner/SLA
assignment slice then turns that command center from queue visibility into
accountable execution by persisting tenant-member ownership, SLA due dates, and
operator notes on feedback detail rows, validating assignees against active
tenant members, writing audit entries for every changed assignment field, and
surfacing the controls directly in the Console detail sheet with browser
coverage. The bulk feedback owner/SLA assignment slice extends the same
semantics to selected feedback rows, giving operators an explicit
keep/clear/set model for owner and SLA, bounded batch validation, per-row audit
evidence, partial-failure reporting, and a Console selection-bar dialog covered
by unit, integration, and browser tests. The operator batch command slice adds
a single selection-scoped command center for link, assign, dismiss, and notify
paths, reusing Customer Request promotion for link, durable assignment for
assign, workflow batch transition for dismiss, and a batch Request Notification
preview/publish dialog for notify. The notification path uses generated
multi-request preview and publish contracts, aggregates eligible/excluded
audience counts, publishes successful request updates, and returns per-request
validation/policy failures without blocking the rest of the batch. It also
persists the latest batch result in the workbench and lets operators focus
failed feedback IDs or retry terminal enrichment failures, so failure recovery
remains visible after the selection clears. The feedback assignment
recommendations slice adds a deterministic policy preview and apply path for
selected feedback rows, translating urgent intake, stalled active work,
terminal AI failures, identity evidence debt, and untriaged rows into owner
lanes, severities, SLA deadlines, rationale, already-satisfied skips, partial
failures, optional member overrides, and audited policy notes. This gives the
operator an inspectable recommendation loop before fully configurable team
routing exists. The feedback assignment policy center then turns that loop from
fixed built-in rules into tenant-level operational policy: delegated
administrators can enable or disable each recommendation rule, tune owner
lanes and SLA hours, choose validated default member owners, and record a
bounded change note. The persisted policy is stored in `system_settings`, feeds
both preview and apply flows, assigns policy default owners when no operator
override is provided, and writes `feedback_assignment.policy_update` audit
events in the same database transaction as the settings update. The policy
center also keeps bounded version history in tenant system settings, exposes a
dry-run impact preview that compares draft rules against the active policy over
the current feedback workset, and lets delegated administrators restore a prior
version as a new audited revision with `feedback_assignment.policy_restore`.
The feedback assignment SLA escalation queue then closes the operator loop by
surfacing open feedback with overdue deadlines, deadlines inside the 12-hour
warning window, missing owners, or missing SLA commitments. It is backed by a
generated read API, a durable PostgreSQL projection that excludes closed
workflow states, HTTP and integration tests for tenant-scoped counts and
ordering, a Console workbench panel, typed MSW fixtures, mutation cache
invalidation, and browser accessibility coverage that opens the at-risk
feedback from the queue.
The first account slices add account-key request filtering across the generated
HTTP contract, repository query, Console toolbar, saved-view state, and browser
accessibility flow, then connect Customer Request account profile rows back into
that account-scoped request review path. The account-scoped list now includes a
signal overview with demand volume, supporting feedback, customers, votes,
delivery health, revenue impact, account-level decision scores, decision
signals, request-level structured decision-score factors, an account evidence
event timeline, request-level decision records with score snapshots, and a
request timeline. That
overview is backed by a generated backend account summary endpoint instead of
front-end pagination state, so the metrics remain authoritative even when the
list has only loaded part of the account's request portfolio. This turns the
account model from an invisible field into an operator-verifiable account
context workflow. Customer Request detail now also reconstructs decision records
from audit events, showing changed status, priority, ownership, title, or
description alongside the decision score, score factors, delivery health,
evidence counts, votes, and revenue context captured at the time of the
decision. This makes prioritization explainable as a timeline rather than a
single current-state number.
Browser accessibility coverage now exercises the Control Tower route and
verifies that the closed-loop recovery scorecard, source health command center,
closed-loop recovery command center, first-value activation scorecard, release
verification evidence center, world-class maturity gap register, world-class
execution queue, and readiness matrix remain visible, evidence-backed, and
actionable through the quality action workflow.
The dedicated `control-tower-smoke` release target rebuilds the production
Console bundle and runs the desktop/mobile Chromium scorecard flow before
release smoke can pass.
The broader `console-browser-smoke` release target also rebuilds the production
Console bundle and runs the full desktop/mobile Chromium accessibility, reflow,
forced-colors, route-churn, and interaction suite across critical Console
routes. The `console-browser-supplemental-smoke` release target extends that
browser evidence to the CI-safe Firefox and WebKit desktop projects.

### Maturity ladder

The product should advance through a narrow maturity ladder:

| Level | Product state | Required proof |
|---|---|---|
| Signal intake | Teams can capture customer feedback from at least one real source. | Ingested signals are visible, searchable, enriched, and source-attributed. |
| Evidence-backed requests | Teams can convert signal into requests with citations and customer/account context. | Each prioritized request shows evidence, supporters, account context, and score explanation. |
| Closed-loop operations | Teams can notify customers and measure outcomes after action. | Notifications, survey responses, failed delivery, and low-score cases are visible and owned. |
| Decision cockpit | Teams can compare roadmap scenarios and explain tradeoffs. | Scenario snapshots, score inputs, evidence freshness, and exports support a product review meeting. |
| Behavior-aware decisions | Teams can enrich requests with selected product usage evidence. | Behavior aggregates or imported cohorts can be cited without requiring full analytics adoption. |
| Governed AI analyst | Teams receive AI suggestions that are cited, permissioned, and reviewable. | Suggestions persist as objects with evidence, action boundaries, and operator feedback. |

This ladder prevents broad category parity from turning into a product trap. Each
level must be useful on its own and must strengthen the previous level.

### Product pillars

#### Pillar 1: Source activation

Create a source setup experience that turns a fresh tenant into useful signal
coverage quickly.

Target capabilities:

- source setup wizard for API, Slack, email, public portal, GitHub, Linear,
  Jira, Zendesk, Intercom, Salesforce, HubSpot, and CSV import;
- connection test, permission diagnostics, sample import, and dry-run preview;
- source health dashboard with last event, lag, errors, suppression, and stale
  credentials;
- canonical demo dataset that exercises feedback, requests, surveys, public
  roadmap, notifications, and external sync together.

Acceptance criteria:

- a new operator can connect one source and see at least one generated insight
  or request without using direct database access;
- each supported source exposes health and failure evidence in Console;
- source setup is covered by unit tests, handler tests, and browser smoke tests
  for at least the primary self-hosted path.

#### Pillar 2: Customer and account signal graph

Introduce a canonical graph projection that resolves signals to subjects,
accounts, segments, source records, and consent state. This should start as a
projection over existing tables rather than a disruptive replacement.

Target capabilities:

- normalized subject identities for email, external user ID, portal visitor,
  support contact, and product user;
- account profiles with revenue, tier, lifecycle status, CRM identifiers, and
  segment labels;
- membership edges from subjects to accounts;
- source-record edges from feedback, requests, votes, comments, surveys,
  notifications, product events, research artifacts, and external issues;
- privacy-preserving hashes and redaction boundaries for sensitive identity
  fields;
- tenant-scoped merge and split operations with audit evidence.

Acceptance criteria:

- feedback detail exposes an identity evidence projection with source-user,
  email, external ID, source contact ID, CRM ID, and support ID provenance,
  plus a generated resolution assessment that explains whether the evidence is
  strong, reviewable, or too weak for merge review;
- the Feedback workbench exposes a read-only feedback identity merge review
  queue that scans a bounded recent window, groups stable identity keys into
  merge candidates, separates weak evidence that needs more keys, and opens the
  exact feedback rows that justify each review item;
- Customer Request lists expose account-key request filtering, and saved views
  preserve that filter so operators can review account-scoped demand without
  rebuilding the query;
- Customer Request details expose an account profile action that closes the
  detail drawer and reviews all requests for that account through the same
  account-key list contract;
- account-scoped Customer Request lists call an authoritative backend account
  summary API and show a signal overview plus request timeline so operators can
  reason about demand concentration, revenue impact, delivery health, and
  decision drivers for one account without relying on currently loaded
  pagination state; the same overview includes an account evidence event
  timeline spanning request creation, feedback links, account customer links,
  votes, issue links, issue sync events, and internal notes;
- every Customer Request can show affected customers, accounts, segments, and
  evidence sources from one graph projection;
- duplicate identity resolution is reviewable and reversible;
- no public surface exposes private account or revenue fields.

#### Pillar 3: Evidence graph and insight workbench

Create first-class insight objects backed by citations. An insight is not only a
summary; it is a durable claim with evidence, confidence, freshness, and linked
requests.

Target capabilities:

- insight themes generated from clusters, semantic search, surveys, public
  comments, product events, and external systems;
- evidence links that can point at feedback rows, survey responses, request
  comments, product event cohorts, support tickets, research quotes, and issue
  metadata;
- evidence health fields: source, age, confidence, duplication, account
  coverage, segment coverage, and sentiment;
- operator actions: accept insight, dismiss insight, merge themes, link to
  request, create request, request more evidence;
- AI summaries that include citations and never replace stored evidence.

Acceptance criteria:

- an operator can open an insight and inspect the exact evidence that supports
  it;
- every AI-generated insight carries citations, confidence, and review state;
- insight actions write audit events and remain reversible where data retention
  permits.

#### Pillar 4: Decision cockpit

Upgrade Customer Requests from a curated backlog into the main prioritization
workspace.

Target capabilities:

- request list modes for triage, planning, roadmap, low-confidence evidence,
  high-value accounts, low-score impact, and stale delivery;
- configurable decision scoring with visible components and limits;
- audit-backed decision records that preserve the score, score factors,
  evidence counts, delivery health, revenue context, and changed fields at the
  moment an operator changes request state;
- impact versus effort fields and RICE-style scoring without requiring one
  opinionated framework;
- roadmap scenarios that let operators compare Now, Next, Later, and custom
  mappings before publishing;
- delivery confidence that combines external issue health, request workflow
  state, evidence freshness, and notification state;
- exportable decision evidence for product review meetings.

Acceptance criteria:

- operators can explain why one request ranks above another without reading raw
  SQL or logs;
- ranking inputs are visible, bounded, and testable;
- each status, priority, owner, title, or description mutation leaves a
  reviewable decision record with the evidence snapshot used at that moment;
- public roadmap state changes remain policy-gated and auditable.

#### Pillar 5: Closed-loop center

Turn notifications and surveys into one operational surface for customer
follow-through.

Target capabilities:

- campaign builder for CSAT and CES;
- trigger preview by workflow state, reply send, request status, source, tag,
  segment, account tier, and consent state;
- delivery health funnel: triggered, matched, eligible, suppressed, queued,
  sent, failed, expired, opened when available, submitted;
- low-score recovery queue with owner, severity, response context, linked
  request, and review outcome;
- customer timeline that shows published updates, notifications, survey
  invitations, survey responses, and reopened demand;
- safe public survey page with CSAT/CES rating, comment prompt, thank-you
  state, expiry, and already-submitted handling.

Acceptance criteria:

- an administrator can create, preview, activate, and inspect a CSAT or CES
  campaign from Console;
- low scores create actionable review rows;
- every suppressed or failed invitation has an explainable reason.

#### Pillar 6: Product behavior bridge

Add enough behavioral signal to make feedback and roadmap decisions less blind
without turning Attune into a full analytics product.

Target capabilities:

- lightweight SDK events: `identify`, `track`, `captureFeedback`, and
  `showSurvey`;
- event import bridges from PostHog, Amplitude, Mixpanel, Pendo, and Segment
  style warehouses where tenants already have analytics;
- simple event aggregates for feature usage, affected cohort size, recent
  activity, and funnel drop-off references;
- links from behavioral cohorts to requests and insights;
- storage controls that cap raw event retention while preserving aggregate
  evidence.

Acceptance criteria:

- a request can cite a behavior aggregate such as affected active users or
  recent failed workflow count;
- product events are never required for teams that only use feedback and
  support sources;
- raw behavior storage has retention, sampling, and tenant-level controls.

#### Pillar 7: AI analyst with evidence constraints

Move from enrichment tasks to an assistant that proposes work while preserving
operator control.

Target capabilities:

- daily or weekly insight digest with citations;
- suggested request merges and duplicate detection;
- suggested evidence links between feedback, surveys, requests, and issues;
- impact explanation for decision score changes;
- low-score recovery summary with suggested owner and next action;
- connector-health and campaign-health summaries;
- guardrails requiring evidence citation, confidence, and reversible actions.
- classification review learning ledger events for accepted, edited, and
  dismissed AI classification samples.

Acceptance criteria:

- AI-generated recommendations never mutate public state without operator
  approval;
- every recommendation can be traced to source evidence;
- operators can accept, dismiss, or edit suggestions and those actions improve
  future ranking behavior through explicit feedback events.
- classification-quality samples expose audited accept, edit, and dismiss
  review actions before the broader suggestion object model expands.

#### Pillar 8: Trust and governance packaging

Make the existing platform maturity work visible as a coherent control plane.

Target capabilities:

- deployment readiness, source health, connector health, LLM route health,
  survey sender health, notification health, and sync health in one reliability
  view;
- RBAC and audit views aligned to product workflows;
- retention, export, deletion, and public-surface privacy controls;
- model configuration and provider-neutral LLM routing surfaced as a buyer
  differentiator;
- evidence export for roadmap, survey, and customer-request decisions.

Acceptance criteria:

- enterprise operators can inspect who changed a customer-facing decision, what
  evidence supported it, and whether the related external systems were healthy;
- trust controls are discoverable from Console without reading deployment docs;
- existing audit and GDPR guarantees remain intact.

## Information Architecture

The Console should make the product loop visible with a small set of durable
workspaces:

| Workspace | Purpose |
|---|---|
| Control Tower | Operational landing page for signal volume, health, blockers, and current priorities. |
| Signals | Normalized stream of feedback, survey responses, public comments, product events, imported tickets, and research artifacts. |
| Insights | AI-assisted themes with citations, confidence, trends, and evidence health. |
| Customer Requests | Decision cockpit for demand, accounts, revenue, scores, roadmap placement, and delivery links. |
| Roadmap | Internal planning and public roadmap publication controls. |
| Closed Loop | Notifications, survey campaigns, low-score recovery, and customer timelines. |
| Integrations | Source setup, external sync, connector health, mappings, and import diagnostics. |
| Accounts | Account and subject graph, segments, merge review, and privacy-safe identity resolution. |
| Governance | RBAC, audit, retention, guardrails, LLM routing, deployment readiness, and compliance evidence. |

This does not require every workspace to be built at once. The important rule is
that each new surface should strengthen the same loop instead of creating a
separate island.

## Architecture Direction

The architecture should preserve existing package layering:

```text
handlers -> service -> repo
                  -> external sync adapters
                  -> notify transports
                  -> llm clients
handlers -> domain
```

Recommended package additions should be narrow and projection-oriented:

- `internal/repo/signalgraph`
- `internal/service/signalgraph`
- `internal/handlers/console/signalgraph`
- `internal/repo/insight`
- `internal/service/insight`
- `internal/handlers/console/insight`
- `console/src/features/signals`
- `console/src/features/insights`
- `console/src/features/surveys`
- `console/src/features/accounts`

The first implementation should avoid a risky table rewrite. Existing
`user_feedback`, Customer Request, public visibility, notification, survey, and
external sync tables should feed read projections and link tables. Canonical
tables can be introduced only where a stable identity or evidence edge is needed.

Candidate durable objects:

- `signal_subjects`: tenant-scoped normalized people and visitors;
- `signal_accounts`: tenant-scoped account profiles;
- `signal_subject_account_links`: memberships and confidence;
- `signal_source_records`: external and internal source object references;
- `signal_evidence_links`: typed edges from source records to requests,
  insights, surveys, and roadmap decisions;
- `insight_themes`: reviewed themes with summary, status, owner, and confidence;
- `insight_evidence`: citations and evidence health for each theme;
- `decision_scenarios`: named request ranking and roadmap scenario snapshots;
- `decision_evidence_exports`: immutable export bundles for review and audit.

## Implementation Plan

The work order should optimize for visible product value before deep platform
expansion:

| Order | Slice | Product reason |
|---|---|---|
| 0 | First-value packaging | Proves that a fresh tenant can understand the product loop quickly. |
| 1 | Survey productization | Converts the already-built survey foundation into visible closed-loop value. |
| 2 | Integration activation pack | Brings real customer context into Attune before broader graph work expands. |
| 3 | Signal graph projection | Consolidates identity and evidence after source coverage exists. |
| 4 | Insight workbench | Turns signal volume into cited themes and operator decisions. |
| 5 | Decision cockpit | Makes prioritization and roadmap tradeoffs explainable. |
| 6 | Product behavior bridge | Adds usage evidence without forcing analytics parity. |
| 7 | AI analyst | Adds governed automation once evidence, identity, and actions are inspectable. |
| 8 | Trust packaging | Consolidates controls as the product surface broadens. |

### Slice Zero: First-value packaging

- Define the first-value path in Console: connect or choose a source, import or
  seed realistic signals, generate themes, promote one Customer Request, preview
  the public roadmap, and send one test closed-loop notification or survey.
- Add guided empty states and setup checklists for feedback, requests,
  public visibility, notifications, and surveys.
- Add a demo workspace that shows the full product loop, not only isolated data
  tables.
- Add Control Tower scorecards for first insight, source health, request
  evidence depth, and closed-loop readiness.

### Slice A: Survey productization

- Add Console survey campaign list, create, edit, archive, hosted-link, preview,
  test-send, response list, analytics, and low-score review pages.
- Add public HTML survey rendering if API-only public responses are not enough
  for actual customer distribution.
- Model low scores, failed sends, and reopened post-resolution demand as
  closed-loop cases with owner, severity, due date, status, and resolution
  evidence.
- Add browser smoke coverage that exercises create campaign, hosted link,
  public response submit, analytics update, and low-score review.

### Slice B: Integration activation pack

- Choose three high-leverage connectors for product teams: GitHub or Linear for
  delivery, Zendesk or Intercom for support context, and HubSpot or Salesforce
  for account context.
- Build guided setup, connection tests, mapping preview, backfill, health, and
  failure recovery around the existing external sync framework.
- Preserve imported support context as source references and snapshots, with
  redaction and privacy boundaries, rather than trying to own helpdesk workflow.
- Make setup success visible in Control Tower and Customer Requests.

### Slice C: Signal graph projection

- Add read projection over feedback, Customer Requests, votes, comments,
  surveys, notification contacts, public visitors, and external issue links.
- Add account and subject graph APIs that keep private fields out of public
  surfaces.
- Add identity-resolution assessment to feedback detail so operators can see
  stable-key count, source-path count, missing keys, risk reasons, and the next
  review action before subject merge/split operations are introduced.
- Add a bounded identity merge review projection to the Feedback workbench that
  groups recent feedback by stable email, external ID, source contact, CRM, or
  support keys, surfaces merge candidates, and lists weak evidence that still
  needs stronger identity keys.
- Add durable subject merge persistence for reviewed identity candidates using
  tenant-scoped subject, identity, and merge-event tables, a generated merge
  endpoint, transactional audit logging, and a Console approve action.
- Add recent reviewed merge visibility plus an audited split action that
  revokes an identity link, recalculates the subject's primary identity, and
  leaves reversible evidence in the audit log.
- Add a subject roster projection to the identity review response so operators
  can inspect active subjects, identity counts, evidence totals, and top
  resolved subjects from Console before the full Accounts workspace exists.
- Add subject detail inspection from the roster with active and revoked
  identities plus merge/split timeline events that carry bounded feedback
  evidence excerpts, keeping identity graph history reviewable before the full
  Accounts workspace exists.
- Add account-key request filtering to the generated API contract, repository
  query, Console toolbar, saved views, and browser accessibility suite as the
  first operator-facing account-model foundation.
- Add raw Feedback account context by normalizing account/company hints from
  source metadata into `FeedbackAccountContext`, list/detail projections,
  assignment escalation rows, the `account_key` list filter, Console filter
  controls, unit coverage, PostgreSQL integration coverage, and browser
  accessibility coverage. This gives operators an account-scoped feedback queue
  before the full Accounts workspace owns canonical profiles.
- Add account-scoped recovery queue filtering for post-resolution surveys by
  normalizing account/company hints from response metadata and invitation
  recipient snapshots into `SurveyAccountContext`, exposing `account_key` on
  list responses, and proving the loop in unit, PostgreSQL integration, and
  browser accessibility coverage. This completes the first Account/Company
  throughline across feedback, requests, and closed-loop recovery.
- Add account-profile request pivot actions in request detail so an operator
  can pivot from one affected account to all account-scoped demand without
  leaving the request review workflow.
- Add an account-scoped signal overview above filtered request lists, backed by
  a generated backend account summary API with account-level decision score and
  decision-signal fields plus an account evidence event timeline, covered by
  repository, service, handler, Console unit, and browser accessibility tests.
- Replace opaque request decision-score debug strings with generated
  decision-score factor fields for priority, feedback, customers, accounts,
  votes, revenue, and delivery-health context, covered by repo, handler,
  Console unit, browser accessibility, and product-readiness contract checks.
- Add audit-backed Customer Request decision records so future create, promote,
  update, link, unlink, merge, vote, and issue-link actions retain the decision
  score, score factors, evidence counts, delivery health, revenue context, and
  visible field transitions that operators need for decision review meetings.
- Add status-level notification evidence for request notifications so each
  public request status exposes expected, notified, failed, suppressed, and
  recovery-pending customer counts through the generated API contract, backend
  delivery aggregation, Console evidence table, route preload, unit coverage,
  and browser accessibility coverage.
- Add Customer Request evidence-quality scoring across the generated contract,
  repository summary projection, handler mapping, Console list/detail UI,
  PostgreSQL integration coverage, browser accessibility coverage, and product
  readiness contract. The score now exposes evidence count, distinct source
  count, customer and account breadth, freshness, stale/low-confidence flags,
  strengths, and gap reasons so requests can be reviewed for trust instead of
  only for priority.
- Add merge review for duplicate subjects and accounts with audit events.

### Slice D: Insight workbench

- Add insight theme objects and evidence links.
- Generate candidate themes from clusters, semantic search, survey comments,
  public board comments, and external support imports.
- Add research-lite evidence artifacts for interview quotes, transcripts, clips,
  notes, usability reports, and study summaries.
- Require citations, confidence, freshness, and human review before an insight
  influences request ranking or public communication.

### Slice E: Decision cockpit

- Expand Customer Requests with scenario views, configurable score weights,
  evidence quality, impact/effort fields, stale evidence warnings, and roadmap
  planning controls.
- Add saved planning views, weekly review exports, and status-update bundles for
  product, engineering, sales, customer success, and leadership audiences.
- Export a decision evidence bundle that includes request metadata, supporting
  evidence, accounts, revenue, survey outcomes, external issue health, and audit
  entries.

### Slice F: Product behavior bridge

- Add SDK-level `identify` and `track` operations with strict retention and
  sampling controls.
- Add import adapters or webhook receivers for external analytics cohorts and
  selected events.
- Link behavior aggregates to insights and requests without requiring Attune to
  store unlimited raw analytics.

### Slice G: AI analyst

- Add suggestion objects for insight creation, request linking, duplicate merge,
  low-score triage, and decision-score explanation.
- Keep every suggestion cited, reviewable, and auditable.
- Apply tool-scope policy to any AI action that touches external systems,
  private customer context, or public surfaces.
- Add feedback events for accepted, edited, and dismissed suggestions.
- Add classification-quality review learning endpoints, ledger persistence,
  audit events, Console review controls, and browser coverage as the first
  governed feedback loop for AI suggestions.

### Slice H: Trust packaging

- Consolidate deployment readiness, connector health, source health, notification
  health, survey health, sync health, LLM route health, audit, and retention into
  a cohesive governance and reliability story.
- Ensure public-surface privacy, revenue visibility, survey tokens, provider
  secrets, and identity hashes are covered by tests and artifact linting.

## Risks / Tradeoffs

- **Scope explosion.** The product categories are broad. The mitigation is to
  keep Attune's wedge narrow: evidence-backed customer signal operations, not a
  replacement for every adjacent system.
- **Over-abstracted graph model.** A generic graph can become hard to reason
  about. The mitigation is to begin with read projections and typed edge tables
  tied to concrete product workflows.
- **Behavior analytics storage cost.** Product-event ingestion can become
  expensive. The mitigation is aggregate-first storage, retention controls,
  connector bridges, and optional raw event capture.
- **AI trust risk.** AI summaries can overstate evidence. The mitigation is
  mandatory citations, confidence, human review, and no direct public mutation.
- **Privacy and revenue exposure.** Account and revenue context must never leak
  to public surfaces. The mitigation is explicit field-level projection tests and
  public contract tests.
- **Integration reliability.** Connectors create operational burden. The
  mitigation is connection qualification, health views, record-level failures,
  retry controls, and quarantine states.

## Product Acceptance Bar

No slice should be considered complete merely because the backend API exists.
World-class parity requires an operator-visible loop.

| Bar | Requirement |
|---|---|
| User-visible workflow | The primary operator can complete the job from Console or a documented CLI path without direct database access. |
| Evidence | New insights, rankings, notifications, survey outcomes, and AI suggestions cite the source records that justify them. |
| Health | Source freshness, worker state, delivery status, suppression, retryability, and stale data are visible where the operator acts. |
| Recovery | Failed imports, failed sends, low scores, stale links, duplicate identities, and rejected AI suggestions have explicit next actions. |
| Public safety | Public pages expose only public-safe fields, and private account, revenue, identity, audit, and token data stay inside trusted surfaces. |
| Governance | Mutations that affect customer communication, scoring, identity resolution, connectors, or public visibility write audit events. |
| Adoption | The slice improves at least one scorecard metric such as time to first insight, request evidence depth, connector freshness, or closed-loop completion. |
| Performance | Console and public-surface slices stay inside explicit bundle budgets instead of relying on non-blocking build warnings. |
| Verification | The implementation includes contract, backend, integration, Console, browser, and artifact checks appropriate to its blast radius. |

## Verification

Each implementing slice should include the relevant subset of:

- proto generation and drift checks for any HTTP contract change;
- Go unit tests for repository, service, handler, and worker behavior;
- PostgreSQL integration tests for migrations, projections, and cross-table
  invariants;
- Console typecheck, biome, unit tests, and accessibility checks for new routes;
- Console production build plus bundle budget checks for new or expanded
  operator workflows;
- browser smoke coverage for source setup, survey public flow, Control Tower
  closed-loop scorecards, Control Tower source health command centers, Control
  Tower closed-loop recovery command centers, Control Tower first-value
  activation scorecards, Control Tower release verification evidence centers,
  Control Tower world-class maturity gap registers, Control Tower world-class
  execution queues, Control Tower readiness matrices, feedback-detail identity
  evidence projections, feedback identity merge review queues, durable signal
  subject merge and split operations, durable signal subject roster projections,
  durable signal subject detail timelines with evidence excerpts,
  classification review learning ledgers and classification-quality review
  controls, end-to-end signal trace feedback details, replay/backfill
  reliability drills, error-budget and burn-rate reliability ledgers,
  release-health reliability ledgers, incident-timeline reliability
  reconstructions, tenant-quota reliability saturation dashboards,
  backup-restore reliability evidence centers, consistency-check reliability
  evidence centers, pipeline-SLO reliability ledgers, security governance/RBAC
  readiness evidence centers, security field-level permissions ledgers, public
  privacy preflight evidence centers, GDPR retention/legal-hold evidence centers,
  security compliance package evidence centers, security key-rotation readiness evidence centers,
  security webhook-signature tooling evidence centers, security incident-runbook evidence center,
  developer API adoption kit evidence center,
  developer SDK parity gate evidence center,
  developer API consistency contract evidence center,
  developer import/export workbench evidence center,
  developer integration catalog evidence center,
  developer upgrade diagnostics evidence center,
  feedback triage command centers, durable feedback owner/SLA assignment,
  bulk feedback owner/SLA assignment, feedback assignment recommendations,
  operator batch command centers with link, assign, dismiss, notify, and
  failed-item recovery paths, feedback assignment policy centers with revision
  history, dry-run impact previews, restore coverage, feedback assignment SLA
  escalation queues,
  feedback account context and account-key feedback filtering,
  account-key request filtering,
  account-profile request pivots,
  account-scoped signal overviews, request decision-record timelines with
  rationale, owner, evidence bundle, and public-safe review state, insight
  review, request prioritization, and public roadmap
  publication where applicable;
- release-smoke wiring for dedicated Control Tower closed-loop browser coverage
  when a slice claims operating-scorecard readiness;
- release-smoke wiring for full Console desktop/mobile browser accessibility
  and reflow coverage before a release candidate is treated as product-ready;
- release-smoke wiring for supplemental Firefox/WebKit Console accessibility
  coverage before a release candidate claims browser parity;
- `scripts/lint-artifacts.sh --strict`;
- `scripts/lint-slog.sh --strict`;
- `scripts/lint-errorcode.sh`;
- `scripts/lint-integration-layout.sh`;
- `scripts/lint-product-readiness-contract.sh`;
- `make ci-check` before claiming an implementation is complete.

## References

- [Productboard](https://www.productboard.com/)
- [Productboard Integrations](https://www.productboard.com/platform/integrations/)
- [Canny](https://canny.io/)
- [Canny Integrations](https://help.canny.io/en/collections/325118-canny-integrations)
- [Aha! Roadmaps](https://www.aha.io/roadmaps/overview)
- [Aha! Ideas](https://www.aha.io/product/ideas)
- [Aha! Integrations](https://www.aha.io/support/roadmaps/videos/aha-roadmaps-overview/aha-integrations)
- [UserVoice](https://www.uservoice.com/)
- [Pendo](https://www.pendo.io/product/)
- [PostHog Product Analytics](https://posthog.com/product-analytics)
- [PostHog Feature Flags](https://posthog.com/docs/feature-flags)
- [Amplitude Product Analytics](https://amplitude.com/platform/product-analytics)
- [Mixpanel](https://mixpanel.com/)
- [Qualtrics Customer Experience](https://www.qualtrics.com/customer-experience/)
- [Medallia Platform](https://www.medallia.com/platform/)
- [Sprig Web and App Surveys](https://sprig.com/deploy/web-apps-websites)
- [SurveyMonkey](https://www.surveymonkey.com/)
- [Intercom Customer Service Software](https://www.intercom.com/customer-service-software)
- [Zendesk Service](https://www.zendesk.com/service/)
- [HubSpot Service Hub](https://www.hubspot.com/products/service)
- [Gainsight PX](https://www.gainsight.com/product-experience/)
- [Dovetail](https://dovetail.com/)
- [Maze](https://maze.co/)
- [UserTesting](https://www.usertesting.com/)
- [Linear Customer Requests](https://linear.app/customer-requests)
- [Jira Product Discovery](https://www.atlassian.com/software/jira/product-discovery)
- [GitHub Projects](https://docs.github.com/issues/planning-and-tracking-with-projects/learning-about-projects/about-projects)
- [DevRev](https://devrev.ai/)
- [Featurebase](https://www.featurebase.app/)
- [Featurebase Integrations](https://help.featurebase.app/articles/3071974-integrate-with-other-tools)
