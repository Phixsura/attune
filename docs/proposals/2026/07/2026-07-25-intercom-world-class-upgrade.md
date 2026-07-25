# Intercom inbound adapter — world-class upgrade spec

| | |
|---|---|
| **Issue** | [#230](https://github.com/Phixsura/attune/issues/230) |
| **Status** | Implemented |
| **Started** | 2026-07-25T21:00:00+08:00 |
| **Related** | [intercom-inbound-adapter](2026-07-25-intercom-inbound-adapter.md) (MVP, Implemented), [zendesk-world-class-upgrade](2026-07-24-zendesk-world-class-upgrade.md) (the audit template) |

---

## Problem

The MVP Intercom adapter delivers watermark-based conversation extraction.
Auditing it against the Zendesk world-class bar (#229's 21-gap audit) and
Intercom-specific API surface identifies the remaining gaps. Several of
the #229 items were already built into the Intercom MVP from day one
(multi-page pagination, structural truncation, customer/agent tagging,
graceful per-item degradation, exponential backoff, sync stats, sync-now,
egress hardening, friendly errors) — this spec closes what is left.

## Gap audit vs. the #229 world-class checklist

| # | Capability (from #229 audit) | Intercom MVP status | Action |
|---|---|---|---|
| 1.1 | Customer-vs-agent tagging | ✅ shipped (incl. `[bot]`) | none |
| 1.2 | Metadata → enrichment Type hint | ✅ `inferType` (priority/rating) | **extend with Fin resolution signal** |
| 1.3 | Custom fields passthrough | ❌ `custom_attributes` dropped | **§1** |
| 1.4 | Smart truncation | ✅ head/tail + bot-first valve | none |
| 2.1 | Rate-limit retry with reset header | ✅ `X-RateLimit-Reset` | none |
| 2.2 | Exponential backoff | ✅ | none |
| 2.3 | Multi-page pagination | ✅ 10 pages/tick | none |
| 2.4 | Configurable budget | ✅ `MaxDetailFetches` | none |
| 2.5 | Smart fetching | ✅ one plaintext call/thread | none |
| 2.6 | Backfill progress logging | ✅ | none |
| 3.1 | Filtering | ✅ states | **add tag include/exclude (§2)** |
| 3.2 | Operator-visible sync progress | ✅ generic proto fields | none |
| 3.3 | Sync Now | ✅ | none |
| 4.1 | URL construction + SSRF | ✅ | none |
| 4.3 | 403 as permanent | ✅ `api_plan_restricted` | none |
| 5.1 | Recent sync preview | ✅ conversation ID shown | none |
| 5.2 | Post-connection summary | ✅ region toast | **upgrade to workspace name (§4)** |
| 5.3 | Onboarding guidance | ✅ Developer Hub link | none |
| — | **Intercom-only: Fin AI resolution telemetry** | ❌ `ai_agent` object dropped | **§3** |
| — | **Intercom-only: source URL (page where conversation started)** | ❌ dropped | **§1** |
| — | **Intercom-only: proactive rate self-throttle** | ❌ reactive-only | **§5** |

---

## 1. Content intelligence: custom attributes + source URL

Conversations carry `custom_attributes` (operator-defined structured
data — order IDs, plan tiers, error codes) and `source.url` (the page
the customer started the conversation from — Messenger conversations
carry the in-app URL, which is exactly attune's `PageURL` semantics for
web feedback).

- Client `Conversation` gains `CustomAttributes map[string]any` and
  `Source.URL`.
- Normalize: `intercom_custom_attributes` SourceMeta key (JSON string,
  same pattern as `zendesk_custom_fields`); `intercom_source_url` meta
  key. `PageURL` stays the inbox permalink (operator backlink is the
  documented contract); the customer-side URL travels in meta.

## 2. Tag include/exclude filtering

State filtering alone cannot express "only sync conversations tagged
`feature-request`" or "skip `spam`". Same operator need as Zendesk §3.1.

- `Config.FilterTags` / `Config.FilterExcludeTags` (`[]string`).
- `matchesFilter` extends the state check: exclude wins, include
  requires ALL listed tags (Zendesk parity), matched case-insensitively
  against tag names.
- Tags are present on search results (no extra API call), so filtering
  happens before the detail fetch — filtered conversations do not
  consume detail budget.
- Proto `IntercomConnConfig`: `repeated string filter_tags = 6;
  repeated string filter_exclude_tags = 7;` (additive).
- Console advanced section: two comma-separated inputs (Zendesk UI
  parity).

## 3. Fin AI-agent resolution telemetry

Intercom's `ai_agent` object records how the AI agent left the
conversation: `resolution_state` (assumed_resolution /
confirmed_resolution / escalated / negative_feedback), `rating`,
`last_answer_type`. For a product-signal platform this is high-value
routing metadata: an **escalated or negative-feedback Fin conversation
is a strong product-pain signal**, while a confirmed resolution is
routine support noise.

- Client `Conversation` gains `AIAgent *AIAgent` (source_type,
  resolution_state, last_answer_type, rating, rating_remark).
- SourceMeta keys: `intercom_ai_resolution_state`,
  `intercom_ai_rating`, `intercom_ai_last_answer_type` (present only
  when Fin participated).
- `inferType` upgrade: `resolution_state ∈ {escalated,
  negative_feedback}` → `complaint` hint (unless the priority rule
  already fired — priority stays the stronger signal).

## 4. Post-connection summary with workspace name

The create toast currently says "已连接到 Intercom（US 区域）". AuthTest
already returns the workspace name; thread it through so the toast says
which workspace was connected (Zendesk §5.2 parity: subdomain ↔
workspace name).

- `CreateInboundSourceResponse` cannot carry it without a proto change —
  instead reuse the audit-log field (already recorded) and surface the
  name in the create response via the existing generic path: add
  optional `connected_workspace = 3` to `CreateInboundSourceResponse`?
  **No** — keep it simpler: test-connection already validates before
  create in the UI flow; the toast upgrade reads the workspace name from
  the **test-connection response**. Add optional
  `workspace_name = 4` to `TestInboundConnectionResponse` (additive,
  channel-generic — Zendesk/Slack may fill it later).

## 5. Proactive rate self-throttle

The MVP only reacts to 429s. Private apps share a 10k req/min budget
per app across the whole workspace — attune should not eat it all
during backfill. Parse `X-RateLimit-Remaining` on every response; when
remaining drops below a floor (default 100, ~1% of budget), stop the
current tick early (transient note, resume next tick). This mirrors
Airbyte's budget-aware throttling in a poll-loop-appropriate form.

- Client: responses carry remaining-budget; expose as
  `RateBudget() int64` on the client (atomic, last-seen).
- Poll loop: check between pages and between detail fetches; below
  floor → log + stop tick gracefully (watermark keeps only processed
  items, so nothing is lost).

---

## Verification

1. Unit tests for every new code path (filter matrix, ai_agent hint
   precedence, custom-attributes meta, budget-floor stop).
2. Fixture tests updated: conversation with custom_attributes +
   ai_agent escalated + source URL.
3. `make ci-check` green.
4. Coverage stays ≥ 85% on both packages.
