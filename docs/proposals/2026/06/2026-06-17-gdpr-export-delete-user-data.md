<!-- markdownlint-disable MD013 -->

# GDPR-compliant export and delete user data

| Field | Value |
| --- | --- |
| **Issue** | [#43](https://github.com/Phixsura/attune/issues/43) |
| **Status** | Implemented |
| **Started** | 2026-06-17 15:40 CST |
| **Related** | [#38](https://github.com/Phixsura/attune/issues/38) (RBAC), [#39](https://github.com/Phixsura/attune/issues/39) (audit log), [#25](https://github.com/Phixsura/attune/issues/25) (embedding clustering), [#26](https://github.com/Phixsura/attune/issues/26) (reply drafts), [#30](https://github.com/Phixsura/attune/issues/30) (batch ops / semantic search), [#40](https://github.com/Phixsura/attune/issues/40) (OIDC SSO), [#41](https://github.com/Phixsura/attune/issues/41) (API key scopes) |

## Problem

Issue #43 asks for first-class GDPR support for data-subject access requests and
erasure:

- `POST /fb/v1/console/gdpr/export` to export one user's data as a ZIP
- `POST /fb/v1/console/gdpr/delete` to permanently delete one user's data
- admin-only access, with audit logging
- a Console settings surface under tenant settings

That direction is right, but the current repository shape matters:

1. The primary end-user data lives on `user_feedback`, not in a standalone user
   profile table.
2. The system does not yet have a tenant-scoped canonical subject identifier
   column that can be queried directly across all relevant features.
3. Several artifacts mentioned in the issue now have different names or storage
   shapes:
   - `feedback_workflow_audit` is now `feedback_audit_log`
   - `feedback_tags` is now `tenant_feedback_tags` +
     `feedback_tag_assignments`
   - embeddings and reply drafts live on `user_feedback`
   - `llm_audit` stores call metadata only and links to feedback via
     `feedback_id`
4. `audit_log` is append-only by design. We cannot "edit old audit rows" during
   erasure without violating the immutability guarantees introduced for #39.
5. The current `user_feedback.user_id` is a composed upstream identifier
   (`ext_<source-id>:<source-user>`), which is workable for ingest but awkward
   for GDPR operations. It mixes transport identity with the user-facing
   identifier we need admins to search for and confirm.

Without a deliberate subject-identity layer, the product would either:

- fail to find all rows for one person,
- over-delete rows that merely share a transport prefix,
- or leave the operator with an opaque identifier that cannot be audited or
  confirmed safely.

## Goals / Non-goals

### Goals

- Provide admin-only export and delete flows for one tenant-scoped end-user
  subject.
- Introduce a canonical, queryable subject identifier for GDPR operations.
- Export all subject-linked feedback data in a deterministic ZIP bundle.
- Permanently delete subject-linked feedback data and derived artifacts.
- Preserve a compliant audit trail for the export/delete operation itself.
- Reuse the existing repo/service/handler layering, proto-first APIs, and
  Console settings structure.
- Add focused unit and integration tests for completeness, authorization, and
  erasure behavior.

### Non-goals

- Do not implement universal tenant data export for every table in the system.
  This issue is about one end-user subject, not tenant backup/restore.
- Do not erase Console operator accounts (`admins`, `oidc_users`,
  `tenant_members`) as part of this feature. That is a separate identity-lifecycle
  concern.
- Do not mutate old `audit_log` rows in place. The table remains append-only.
- Do not treat tenant-scoped operational tables with no end-user linkage as
  GDPR subject data merely because they contain human-entered strings.
- Do not add a generic cross-table privacy framework before we have a concrete
  second use case.

## Current-state reconciliation

The issue body predates several schema and architecture changes. The proposal
therefore reconciles the requested scope with verified current reality.

| Issue mention | Current repository reality | Proposal decision |
| --- | --- | --- |
| `feedback` | `user_feedback` is the canonical end-user row | Include |
| `feedback_tags` | Tag assignments are in `feedback_tag_assignments`; tag definitions are tenant-wide in `tenant_feedback_tags` | Export assignment data; do not delete tenant tag definitions |
| `feedback_workflow_audit` | Replaced by `feedback_audit_log` | Include and delete via feedback linkage |
| Generated reply drafts | Stored inline on `user_feedback.reply_draft` | Included automatically with feedback row; deleted with feedback row |
| embeddings / vectors | Stored inline on `user_feedback.embedding*` / `cluster_*`; query cache is tenant/query scoped | Include feedback-embedded fields; do not touch `query_embedding_cache` |
| `audit_log` with anonymized trace | Append-only immutable control-plane log | Record `gdpr.export` / `gdpr.delete` audit rows with hashed subject metadata; do not rewrite history |
| `external_api_keys` | Tenant ingest credentials; no end-user subject foreign key or creator link | Exclude from subject export/delete in v1; document as intentionally out of scope |

The last row is the important reality check. `external_api_keys` belong to the
tenant integration surface, not to an end-user subject represented by feedback.
Current schema cannot safely answer "which API key belongs to this data
subject?", so including it would create false confidence.

## Industry benchmarking

Reviewed 10 mature products and platforms with documented data export and/or
erasure flows:

| Project | Verified pattern | Takeaway for Attune |
| --- | --- | --- |
| GitHub | Self-serve account export, delivered as an archive link over email; link expires and can be revoked before expiry. | Good model for export delivery: async archive generation plus revocable download link. |
| Slack | Admin/owner-gated exports, scoped by plan and conversation type; supports recurring exports for approved orgs. | Strong precedent for admin-only exports and explicit scope controls. |
| Atlassian | Requires apps to track personal data by a canonical `accountId`, report stored identities periodically, and erase/refresh based on API response. | Best precedent for introducing a first-class canonical subject key and storing one queryable copy. |
| Google | Export and deletion are separate actions; export never implies deletion. | Attune should keep `export` and `delete` as distinct, explicit operations. |
| Microsoft Teams Free | Exposes multiple narrow export/delete paths per data class; some deletions remove only one relationship or view, not all history. | Useful reminder to document exactly what each delete path does and does not remove. |
| Zendesk | Recommends deleting content first, then permanently deleting the user last; preserves some anonymized/reporting residue. | Very relevant for support tooling: deletion workflow should be ordered and preserve non-identifying operational evidence where appropriate. |
| HubSpot | GDPR delete is a distinct permanent-delete flow, with optional confirmation email; identified by contact record or email in API. | Good precedent for a separate irreversible delete action and explicit operator confirmation. |
| Freshdesk | Uses a two-step delete (`delete` then `delete forever`) for contacts and associated records. | Confirms the industry value of separating soft delete from hard delete, even if Attune's GDPR path skips the soft-delete stage. |
| Notion | Treats AI-derived data and embeddings as customer data, documents separate retention windows, and uses zero-retention where possible. | Strong precedent for explicitly including reply drafts / embeddings / derived AI artifacts in the GDPR model. |
| OpenAI | Central privacy portal for access/export/delete rights, with identity verification and expiring export links. | Good precedent for consolidating privacy operations behind a dedicated surface and ownership verification. |

### Patterns that show up repeatedly

1. **Canonical subject identity beats fuzzy search.**
   Atlassian is the clearest example: it standardizes everything around
   `accountId`, then builds privacy workflows on top of that exact key.
   Products that rely on looser identifiers tend to push more manual work onto
   admins.

2. **Export and delete are separate operations, not one combined button.**
   Google, GitHub, OpenAI, Slack, and Microsoft all separate these paths. This
   is both a UX safety measure and an auditability win.

3. **Exports are asynchronous archives, usually with short-lived links.**
   GitHub, Slack, and OpenAI all deliver a generated archive later, typically by
   email or a download dashboard. That maps well to Attune returning a ZIP
   bundle rather than trying to inline everything into JSON.

4. **Destructive privacy operations are privileged and explicit.**
   Slack, HubSpot, Zendesk, and Freshdesk all make deletion an admin or
   owner-controlled action with extra confirmation or a separate irreversible
   mode.

5. **Derived data is part of the privacy surface.**
   Notion is the clearest modern AI example: embeddings and AI-generated data
   are documented as customer data with their own retention treatment. Attune
   should do the same for reply drafts, embeddings, and `llm_audit` rows that
   remain linked to the subject.

6. **Deletion often preserves some non-PII operational residue.**
   Zendesk preserves placeholders and ratings while scrubbing user identity;
   audit/control-plane systems usually retain deletion evidence. This supports
   Attune's plan to keep `audit_log` rows but hash the subject identifier.

### What Attune should borrow directly

- A **canonical subject key** on `user_feedback`, inspired most strongly by
  Atlassian's `accountId` pattern.
- A dedicated **admin-only GDPR surface** in Console, closer to Slack/HubSpot
  than to a generic settings form.
- **Async export archive** semantics with deterministic bundle contents,
  following GitHub/OpenAI/Slack patterns.
- **Explicit irreversible delete** with confirmation, similar to HubSpot and
  Freshdesk.
- **Clear derived-data handling** for reply drafts and embeddings, following the
  transparency level Notion applies to AI artifacts.

### What Attune should avoid

- Treating tenant integration secrets as if they were end-user subject data just
  because they sit in the same product database.
- Performing privacy deletion through fuzzy search or free-text matching.
- Hiding irreversible deletion behind a generic "delete record" affordance.
- Claiming full erasure while leaving derived AI rows linked through nullable
  foreign keys.

## Decision summary

Based on the current codebase and the 10-project benchmark above, this proposal
locks in the following implementation decisions for Attune.

### Adopt now

| Decision | Why |
| --- | --- |
| Add a canonical `subject_key` to `user_feedback` | Closest fit to Atlassian's `accountId` pattern; avoids fuzzy GDPR lookup. |
| Keep `export` and `delete` as separate admin-only operations | Matches GitHub, Google, Slack, HubSpot, OpenAI; reduces destructive UX risk. |
| Export as a ZIP bundle with deterministic machine-readable files | Matches GitHub/OpenAI-style archive flows and preserves nested JSON fidelity. |
| Include derived AI artifacts in scope | Notion-style treatment is the right model for `reply_draft`, embeddings, and `llm_audit`. |
| Keep deletion hard-delete for the GDPR route | Aligns with the issue request and HubSpot/Freshdesk-style irreversible privacy deletion. |
| Preserve only hashed subject evidence in `audit_log` | Balances Zendesk-like operational residue with Attune's append-only audit design. |
| Make the GDPR UI a dedicated settings section | Better operational clarity than hiding it inside general feedback actions. |

### Defer

| Decision | Why deferred |
| --- | --- |
| Async background export job with expiring download link | Strong long-term pattern, but first version can return a ZIP directly if data size is modest. |
| Subject alias / merge registry | Valuable only once we see real collisions or multi-identifier subject resolution needs. |
| Confirmation email / two-person approval for deletion | Useful for larger enterprise controls, but not required for the first compliant path. |
| Automated privacy request intake portal | Out of scope for the current operator-driven Console feature. |
| Per-artifact retention policy UI | Current need is deterministic export/delete, not configurable privacy retention policy management. |

### Reject for v1

| Decision | Why rejected |
| --- | --- |
| Use `user_feedback.user_id` as the long-term GDPR API field | It is transport-shaped, not operator-friendly, and not stable enough as the privacy contract. |
| Include `external_api_keys` in subject export/delete | No subject linkage exists; doing so would mix tenant credentials with end-user privacy data. |
| Rewrite historical `audit_log` rows during erasure | Conflicts directly with #39 append-only guarantees. |
| Use fuzzy subject matching | Too risky for destructive operations. Exact canonical-key match only. |
| Treat soft delete as sufficient for GDPR erasure | Conflicts with the issue's hard-delete requirement and with user expectations for erasure. |

## Proposal

### 1. Add a canonical GDPR subject identity on `user_feedback`

Introduce explicit subject columns on `user_feedback`:

- `subject_key TEXT NOT NULL DEFAULT ''`
- `subject_display TEXT NOT NULL DEFAULT ''`
- `subject_hash TEXT NOT NULL DEFAULT ''`

Semantics:

- `subject_key` is the tenant-scoped canonical identifier used by APIs and DB
  lookups. It should be stable, exact-match, and human-confirmable.
- `subject_display` is a UI-friendly echo of the subject identifier for export
  manifests and confirmation prompts.
- `subject_hash` is a one-way digest used in audit metadata so we can preserve
  evidence of GDPR operations without storing the subject identifier in clear
  text inside `audit_log`.

Normalization rule:

- For new ingest, derive `subject_key` from the raw upstream subject identifier
  before the current transport prefix is composed into `user_feedback.user_id`.
- For historical rows, backfill from the existing composed `user_id` using the
  same parsing logic that the ingestor already relies on conceptually:
  `ext_<source-id>:<source-user>` -> canonical subject key = `<source-user>`.
- If a row lacks a parseable upstream subject, fall back to the full legacy
  `user_id` so existing data remains exportable and deletable.

Why columns on `user_feedback` instead of a new registry table:

- Every currently in-scope artifact hangs off feedback rows directly or
  indirectly.
- This keeps the first implementation queryable with one indexed predicate:
  `(tenant_id, subject_key)`.
- It avoids introducing subject lifecycle tables before the product needs
  subject merge, aliasing, or profile-level state.

### 2. Define the export/delete scope from feedback outward

Treat one GDPR subject as "all `user_feedback` rows in a tenant where
`subject_key = ?`", then derive the rest from those feedback IDs.

#### Exported data

- `user_feedback` rows, including:
  - content
  - source / source metadata
  - workflow fields
  - enriched attrs and rationale
  - reply draft
  - embedding / cluster metadata
  - timestamps
- tag assignments joined through `feedback_tag_assignments` and
  `tenant_feedback_tags`
- `feedback_audit_log` rows for those feedback IDs
- `llm_audit` rows for those feedback IDs
- a manifest with:
  - tenant id
  - subject key
  - export timestamp
  - row counts by artifact type
  - app version / schema notes

#### Deleted data

- matching `user_feedback` rows
- cascaded `feedback_tag_assignments`
- cascaded `feedback_audit_log`
- cascaded `embedding_task` / `reply_draft_task`
- implicitly removed inline reply drafts / embeddings because they live on the
  feedback rows themselves
- `llm_audit` rows for matching `feedback_id`

`llm_audit` needs explicit delete before or alongside feedback erasure because
its FK is `ON DELETE SET NULL`; relying on FK behavior would preserve detached
rows that still refer to the subject's generated outputs operationally.

### 3. Keep audit evidence without storing subject PII in `audit_log`

Add new audit actions:

- `gdpr.export`
- `gdpr.delete`

Each row should record:

- actor metadata from the current authenticated admin session
- `target_type = "gdpr_subject"`
- `target_id = subject_hash`
- summary like `Exported GDPR bundle for subject` / `Deleted GDPR subject data`
- `before_json` or `after_json` metadata with counts only, for example:
  - `subject_hash`
  - `feedback_count`
  - `feedback_audit_count`
  - `llm_audit_count`
  - `tag_assignment_count`

This preserves operator accountability and destructive-action evidence without
placing the end-user identifier in the immutable audit stream.

The proposal intentionally does not rewrite prior `audit_log` rows that may
reference deleted feedback IDs. Those target ids are internal record ids, not
subject identifiers, and #39's append-only contract remains intact.

### 4. Add a dedicated GDPR service package

Add `internal/service/gdpr` to orchestrate the workflow.

Responsibilities:

- validate the subject input
- resolve all matching feedback IDs for `(tenant_id, subject_key)`
- gather export datasets
- build deterministic archive payloads
- execute delete within a transaction
- write a sanitized `audit_log` row

Suggested API surface:

```go
type Service interface {
    Export(ctx context.Context, tenantID, subjectKey string, actor auditlog.Actor) (*ExportBundle, error)
    Delete(ctx context.Context, tenantID, subjectKey string, actor auditlog.Actor) (*DeleteResult, error)
}
```

Design constraints:

- export is read-heavy and can run outside one giant serializable transaction
  once the feedback id set is captured
- delete should run in a single transaction over the resolved id set plus the
  audit write
- repo methods should stay feature-scoped: feedback repo owns feedback lookups,
  audit repo owns audit writes, a small GDPR repo may own cross-artifact
  collection SQL where that keeps service logic from becoming SQL-shaped

### 5. Add admin-only proto and handler surfaces

Add a new proto, for example `proto/attune/v1/gdpr.proto`, with:

- `ExportGdprSubject`
- `DeleteGdprSubject`

HTTP surface:

- `POST /fb/v1/console/gdpr/export`
- `POST /fb/v1/console/gdpr/delete`

Request shape:

- `subject_key` as the required canonical identifier

Response shape:

- export returns a binary ZIP response from the handler
- delete returns structured counts in JSON/proto

Authorization:

- backend routes use `RequireAdminStrict()`
- denied non-admin requests return `403`

### 6. Add a GDPR settings area in Console

Extend the settings router with a new admin-only section:

- sidebar item under settings
- one input for subject key
- one `Export` action
- one destructive `Delete` action with explicit confirmation

UI behavior:

- show counts / warnings only after a preflight lookup, or on operation result
- require exact subject key confirmation before delete
- keep the surface plainly operational, consistent with the rest of the
  settings app rather than turning it into a marketing-style compliance page

Permissions:

- add `settings:gdpr:view`
- add `settings:gdpr:export`
- add `settings:gdpr:delete`

All should be admin-only in the frontend permission matrix.

### 7. Archive format

The ZIP bundle should be deterministic and machine-readable first.

Proposed layout:

```text
manifest.json
feedback.jsonl
feedback_tags.jsonl
feedback_audit_log.jsonl
llm_audit.jsonl
```

Why JSONL instead of CSV-only:

- nested JSON columns already exist (`source_meta`, `enriched_attrs`, etc.)
- JSONL preserves fidelity without inventing lossy flattening rules
- auditors and support tooling can still convert it downstream if needed

If product wants spreadsheet-friendly output later, CSV can be an additive
format, not the primary one.

### 8. Documentation and changelog

Implementation should update:

- `README.md` or a dedicated compliance doc section describing:
  - subject-key expectations
  - admin-only access
  - export bundle contents
  - erasure limitations
- `CHANGELOG.md` under `[Unreleased]`:
  - `### Added` for the GDPR endpoints/UI
  - `### Security` for the audit and erasure controls

## Alternatives considered

### A. Use the legacy `user_feedback.user_id` as the API contract

Rejected.

It encodes transport concerns (`ext_<source-id>`) into the operator-facing
privacy workflow, is harder to confirm safely in UI, and makes backfill/query
logic brittle. A dedicated `subject_key` is a small schema addition that pays
for itself immediately.

### B. Build a standalone `gdpr_subjects` registry with aliases

Deferred.

This would be useful if Attune needed subject merge, alias management, or
profile-centric workflows. The current product has none of those requirements,
and all relevant data is already feedback-centric.

### C. Soft-delete GDPR subjects first, then hard-delete later

Rejected for this issue.

The issue explicitly asks for hard deletion. The product already has soft-delete
for feedback batch ops; GDPR erasure should not silently become "archived but
recoverable."

### D. Include `external_api_keys` in the first export/delete

Rejected.

There is no end-user subject linkage in current schema. Including tenant API
keys in a subject export would conflate end-user privacy with tenant operator
credentials and create more risk, not less.

## Risks / tradeoffs

- **Backfill ambiguity**: some historical `user_id` values may not parse cleanly
  into the intended upstream identity. Mitigation: fallback to the legacy raw
  value and document that exact-match behavior.
- **Subject collision across channels**: two channels may emit the same visible
  identifier for different people. Mitigation: keep subject matching scoped to
  one tenant and document the canonicalization rule; if this becomes a real
  product problem, it justifies the deferred alias registry.
- **Delete fan-out correctness**: `llm_audit` uses `ON DELETE SET NULL`, so
  forgetting explicit cleanup would leave derived traces behind. Mitigation:
  make `llm_audit` deletion part of the service transaction and cover it in
  integration tests.
- **Audit/privacy tension**: operators need evidence of deletion, but GDPR
  operations should not preserve subject identifiers in an immutable table.
  Mitigation: store only `subject_hash` plus aggregate counts.
- **Large exports**: one subject with many feedback rows may produce a big ZIP.
  Mitigation: the first implementation can stream ZIP creation from the handler
  once the dataset is collected, and acceptance tests only require correctness
  for moderate row counts.

## Implementation plan

1. Add a migration for `user_feedback.subject_key`, `subject_display`,
   `subject_hash`, indexes, and backfill.
2. Add canonical subject normalization in ingest so new rows populate those
   fields on write.
3. Add repo methods to:
   - list feedback ids by `(tenant_id, subject_key)`
   - collect export datasets
   - delete matching `llm_audit` rows
4. Add `internal/service/gdpr` orchestration and audit integration.
5. Add `gdpr.proto`, generated code, and handler routes.
6. Extend RBAC/permission matrices and the settings router with the GDPR page.
7. Add Console export/delete UX and localized strings.
8. Update docs and changelog in the implementation PR.

## Verification

- **Migration test**: backfill produces expected `subject_key` / `subject_hash`
  values for legacy `ext_<source-id>:<source-user>` rows and reasonable fallback
  behavior for opaque historical values.
- **Repo/service unit tests**:
  - subject lookup returns only the target tenant's rows
  - export manifest counts match gathered datasets
  - delete removes `llm_audit` rows before feedback FK nulling can preserve them
- **Handler auth tests**:
  - admin succeeds
  - member/viewer receive `403`
- **Postgres integration tests**:
  - create 50 feedback rows for one subject across tags, workflow audit, reply
    drafts, embeddings, and llm audit
  - export ZIP contains all expected files and row counts
  - delete removes all subject-linked rows and leaves other subjects untouched
  - `audit_log` receives a `gdpr.delete` row containing only hashed subject
    metadata
- **Console tests**:
  - GDPR settings section visible to admins only
  - export button triggers file response path
  - delete confirmation gate prevents accidental destructive submission

## Production-readiness gaps after v1

The implemented v1 path is functionally complete: exact subject-key export and
hard delete now work end to end, and browser validation confirmed that the ZIP
download contains the expected manifest and JSONL datasets. However, the
industry review still shows a clear gap between "feature complete" and
"top-tier production privacy workflow."

### Gap summary

| Priority | Gap | Why it matters |
| --- | --- | --- |
| P0 | Synchronous export response | Mature products usually enqueue export, build the archive in the background, and deliver a short-lived link. Direct request/response ZIP download will age poorly on large subjects. |
| P0 | No expiring or revocable download artifact | GitHub/OpenAI-style export links usually expire and can be invalidated. Our current direct download has no separate token lifecycle or post-generation access control. |
| P1 | No step-up authentication for export/delete | Re-asking for password / recent auth / MFA on destructive privacy actions is common and reduces operator-account blast radius. |
| P1 | No deletion grace window | Some mature products allow a brief cancel window for irreversible deletes. Immediate hard delete is compliant, but less operator-safe. |
| P1 | No request-status center | Operators cannot yet see `requested` / `processing` / `ready` / `downloaded` / `deleted` as first-class privacy requests. Audit log is necessary but not sufficient for operational tracking. |
| P2 | Retention/backups/legal-hold contract is not explicit enough | Top-tier privacy surfaces document what is deleted immediately, what remains as hashed/immutable evidence, and what may persist in backups or de-identified aggregates. |

### What mature products do differently

- **Async archive generation**: GitHub, Google, OpenAI, and Zoom all bias
  toward background archive creation rather than tying export success to one
  interactive request.
- **Artifact lifecycle controls**: expiring links, revocation, or delivery to a
  verified mailbox are standard for privacy exports.
- **Higher-friction destructive actions**: password re-entry, approval, or
  cancel windows are common once deletion becomes permanent.
- **Request statefulness**: privacy actions are treated as requests with status,
  not as one-off button presses hidden only inside audit rows.
- **Explicit privacy contract**: mature products document how immutable audit,
  backups, and derived data fit into erasure claims.

### Recommended follow-up roadmap

#### Phase 1: Async export hardening

- Introduce a `gdpr_requests` table with request type, tenant, subject hash,
  status, actor, timestamps, archive location, and expiry.
- Convert `POST /fb/v1/console/gdpr/export` into:
  1. create request
  2. enqueue background job
  3. return request id / status
- Generate ZIP artifacts outside the interactive request path.
- Serve downloads through a short-lived signed token or one-time download id.
- Audit:
  - request created
  - archive ready
  - archive downloaded
  - archive expired / revoked

#### Phase 2: Destructive-action safety

- Require step-up auth for `gdpr.export` and `gdpr.delete`:
  - password re-entry for local admin auth
  - recent-auth session age check
  - MFA hook if/when Console supports it
- Change delete flow from immediate execution to:
  1. request created
  2. pending destructive confirmation
  3. optional short grace window
  4. execution
  5. completed
- Preserve the current exact-match `subject_key` requirement; do not relax to
  fuzzy search while adding safety features.

#### Phase 3: Operator workflow and contract clarity

- Add a Console GDPR request center with list/detail views for request status,
  archive expiry, delete completion, and failure reasons.
- Document the data-retention contract explicitly:
  - what is hard-deleted immediately
  - what remains only as hashed audit evidence
  - what may persist in backups until rotation
  - whether any de-identified or aggregated metrics are retained
- Extend proposal / user docs to describe how legal hold would interact with
  deletion if Attune later supports regulated-enterprise workflows.

### Acceptance criteria for "top-tier production ready"

Attune can claim parity with mature GDPR export/delete patterns when all of the
following are true:

1. Export is asynchronous and resilient to large subjects.
2. ZIP downloads are gated by expiring, revocable artifacts rather than only
   the original interactive request.
3. Delete requires recent-auth confirmation or equivalent step-up auth.
4. Operators can inspect the lifecycle of each GDPR request in product UI.
5. The privacy contract explicitly explains immutable audit residue, backups,
   and derived-data handling.
6. Browser and integration tests cover the request lifecycle, not only the
   immediate happy path.

## References

- Follow-up implementation status (2026-06-17):
  - added `gdpr_requests` as the request-center system of record for export and delete lifecycles
  - added session-backed step-up auth enforcement plus password re-verification for local admins
  - added an explicit GDPR operations panel for export TTL, audit retention, prune cadence, and live request/archive backlog
  - upgraded hard delete into a scheduled delete workflow with grace-window execution, request cancellation, and worker-driven completion
  - added explicit export-archive revocation so ready/downloaded GDPR ZIP artifacts can be invalidated before TTL expiry and are surfaced as first-class revoked requests in the Console

- Issue [#43](https://github.com/Phixsura/attune/issues/43)
- [internal/infra/database/migrations/001_init.sql](/Users/phj/Develop/attune/internal/infra/database/migrations/001_init.sql)
- [internal/infra/database/migrations/022_llm_confidence_cost.sql](/Users/phj/Develop/attune/internal/infra/database/migrations/022_llm_confidence_cost.sql)
- [internal/infra/database/migrations/025_embedding_clustering.sql](/Users/phj/Develop/attune/internal/infra/database/migrations/025_embedding_clustering.sql)
- [internal/infra/database/migrations/026_reply_draft.sql](/Users/phj/Develop/attune/internal/infra/database/migrations/026_reply_draft.sql)
- [internal/infra/database/migrations/029_feedback_tags.sql](/Users/phj/Develop/attune/internal/infra/database/migrations/029_feedback_tags.sql)
- [internal/infra/database/migrations/030_workflow_states.sql](/Users/phj/Develop/attune/internal/infra/database/migrations/030_workflow_states.sql)
- [internal/infra/database/migrations/037_audit_log.sql](/Users/phj/Develop/attune/internal/infra/database/migrations/037_audit_log.sql)
- [internal/repo/feedback/feedback.go](/Users/phj/Develop/attune/internal/repo/feedback/feedback.go)
- [internal/repo/feedback/feedback_console.go](/Users/phj/Develop/attune/internal/repo/feedback/feedback_console.go)
- [internal/repo/feedback/feedback_batch.go](/Users/phj/Develop/attune/internal/repo/feedback/feedback_batch.go)
- [console/src/routes/_authed.settings.tsx](/Users/phj/Develop/attune/console/src/routes/_authed.settings.tsx)
