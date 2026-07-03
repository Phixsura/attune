# Reply draft review, edit history, and controlled send workflow

| | |
|---|---|
| **Issue** | [#164](https://github.com/Phixsura/attune/issues/164) |
| **Status** | Implemented |
| **Started** | 2026-07-03T11:01:33+08:00 |
| **Related** | [#26](../06/2026-06-13-enricher-reply-draft.md) (initial reply draft generation), [#34](../06/2026-06-14-outbound-adapter-framework.md) (outbound adapter framework), [#38](../06/2026-06-16-rbac-admin-member-viewer.md) (RBAC), [#39](../06/2026-06-16-audit-log-sensitive-console-actions.md) (global audit log), [#117](../06/2026-06-14-feedback-manual-tags.md) (feedback detail precedent), [#167](2026-07-01-outbound-adapter-conformance.md) (adapter conformance) |

---

## Problem

attune already generates operator-facing reply drafts, but the current feature is
intentionally narrow. The draft is a single overwrite-only text field on
`user_feedback`, surfaced in Console with Copy and Regenerate. It is a useful
assistant, not an operational workflow.

The current implementation has several hard limits:

- `user_feedback.reply_draft` and `reply_draft_generated_at` are inline fields
  added by [`026_reply_draft.sql`](../../../../internal/infra/database/migrations/026_reply_draft.sql).
- `internal/repo/replydraft.UpdateReplyDraft` overwrites the current draft text
  and timestamp in place.
- `ReplyDrafter.Generate` uses the wrapped LLM client and records cost through
  `llm_audit`, but it does not write workflow history.
- Console renders a single draft card with Copy and Regenerate in
  [`detail-sheet.tsx`](../../../../console/src/features/feedback/components/detail-sheet.tsx).
- The only reply-draft mutation endpoint is
  `POST /fb/v1/console/feedback/{id}/reply-draft/regenerate`.
- The global audit inventory currently treats regenerate as exempt because it
  was only content regeneration, not a controlled operational action.
- The original #26 proposal explicitly rejected version history and any path
  from reply drafts into notify/outbox.

#164 changes the product shape. It asks for review statuses, edit history,
Console editing and approval, permissioned sending hooks, and audit records for
generate/edit/approve/send. That means reply drafts need to become a small
human-in-the-loop workflow with explicit provenance and controlled delivery.

The safety line from #26 still stands: the LLM must never send a reply. The new
workflow gives operators and configured send hooks a controlled path after human
review, not an automatic LLM-to-customer path.

## Industry benchmark

The strongest industry pattern is not "AI writes and sends." Mature support
systems split AI assistance from customer-facing delivery, then put explicit
human control around high-impact actions.

| System | Relevant pattern | Design signal for attune |
|---|---|---|
| Zendesk Copilot suggested first replies | Agents can accept, edit, send, or reject suggested first replies. Zendesk adds an `accepted_suggested_first_reply` tag for reporting. | Model generate/edit/reject/accept as first-class events and expose adoption metrics. |
| Zendesk Auto Assist | Suggested replies and macros can be edited before sending. If the ticket changes while an agent edits a suggestion, the agent can review newer suggestions. | Approved drafts must become stale when their source context changes. |
| Intercom Fin Procedures | Human-in-the-loop approvals pause a procedure, route work to a teammate, then resume after a decision. | Approval is a resumable workflow state, not just a UI confirmation. |
| Salesforce Einstein Reply Recommendations | Agents review, edit, and send recommended responses from the service console, with permission-set access. | Reply send belongs behind explicit product permissions. |
| Freshdesk Freddy AI Copilot | Agents apply AI and canned response suggestions inside the reply editor; reporting tracks adoption and impact. Freshdesk also treats reply clashes as a product problem. | Draft revision guards and usage metrics should be part of the feature. |
| ServiceNow Now Assist for CSM | ServiceNow documents AI limitations, human oversight expectations, and data processing for inputs, outputs, and edits. | Store AI suggestion, human edit, and sent text separately. |
| HubSpot Breeze | Reply recommendations are inserted before sending; customer-agent actions can require manual approval; handoff rules are configurable. | Keep reply generation, approval, and delivery as separate actions with separate policies. |
| Help Scout AI Drafts | AI Drafts create draft-state replies that users review, revise, and send. Users need reply permission. Collision Detection blocks sends when a conversation changes during editing. | `stale` is a first-class status, and send must check source freshness. |
| Front Copilot and shared drafts | Compose drafts in the message editor, share drafts for collaboration, and use separate API scopes for draft creation and message sending. | Treat the draft as a collaborative object, not as an incidental string. |
| Gorgias AI Agent | Automated responses are labeled, grounded, source-reviewable, and audited through response/source review. Sensitive topics hand over to humans. | Record source/rationale metadata and keep review evidence visible. |
| Ada Handoffs | Handoffs are configured objects that pass context to human agents or external support systems, with channel-specific behavior. | A controlled send hook should be its own delivery abstraction, not a reuse of team-notification semantics. |

References:

- [Zendesk suggested first replies](https://support.zendesk.com/hc/en-us/articles/8037936748570-Turning-on-suggested-first-replies)
- [Zendesk auto assist](https://support.zendesk.com/hc/en-us/articles/7051314237466-Using-auto-assist-to-solve-tickets)
- [Intercom human-in-the-loop approvals](https://www.intercom.com/help/en/articles/14468561-human-in-the-loop-approvals-for-fin-procedures)
- [Freshdesk Freddy canned response suggestions](https://support.freshdesk.com/support/solutions/articles/50000002341-use-canned-response-suggestions-to-respond-to-tickets-faster)
- [ServiceNow Now Assist for CSM](https://www.servicenow.com/docs/r/customer-service-management/now-assist-for-csm/now-assist-csm.html)
- [HubSpot reply recommendations](https://knowledge.hubspot.com/social/use-quick-replies-in-your-social-tool)
- [HubSpot customer-agent handoff](https://knowledge.hubspot.com/customer-agent/set-up-and-customize-the-customer-agents-handoff-process)
- [Help Scout AI Drafts](https://docs.helpscout.com/article/1570-ai-drafts)
- [Help Scout Collision Detection](https://docs.helpscout.com/article/99-prevent-duplicate-replies-with-collision-detection)
- [Front shared drafts](https://help.front.com/en/articles/2216)
- [Front create draft API](https://dev.frontapp.com/reference/create-draft)
- [Gorgias AI Agent transparency](https://docs.gorgias.com/en-US/how-gorgiass-ai-agent-works-1997817)
- [Ada handoffs](https://docs.ada.cx/docs/handoffs)
- [OpenAI Agents SDK human-in-the-loop](https://openai.github.io/openai-agents-python/human_in_the_loop/)
- [Microsoft Foundry Agent Service transparency note](https://learn.microsoft.com/en-us/azure/foundry/responsible-ai/agents/transparency-note)

## Webhook delivery benchmark

After the initial workflow implementation, the remaining gap was webhook
operability rather than the review UI itself. Mature webhook products make
delivery observable, replayable, and easy to validate before production use.

| System | Relevant webhook pattern | Design signal for attune |
|---|---|---|
| Stripe webhooks | Signs every event, retries delivery, and documents undelivered-event recovery. | Reply send hooks need signed payloads, retry-backed sends, and a clear recovery path for failed deliveries. |
| GitHub webhooks | Shows recent deliveries with request/response evidence and supports redelivery. | Console needs a recent delivery log with status, HTTP result, attempts, errors, and redelivery for failed attempts. |
| Shopify webhooks | Documents retry behavior, failure metrics, and removed webhook monitoring. | Hook health should expose failure rate signals, attempts, and terminal failure state. |
| Linear webhooks | Signs the raw request body and includes timestamp guidance for replay protection. | Attune signatures should be versioned and timestamped so receivers can enforce a replay window. |
| Zendesk webhooks | Treats creation, testing, signing secret handling, and monitoring as one operator workflow. | Hook configuration should include a test event and never require users to infer whether an endpoint is live. |
| LaunchDarkly webhooks | Uses optional HMAC signatures and concise receiver guidance. | The Console contract panel should include headers, signature basis, and a compact payload sample. |
| GitLab webhooks | Recommends HMAC signing tokens over weaker shared-token headers. | Attune should keep the reply path on HMAC signatures, not bearer-token-style proof. |

References:

- [Stripe webhooks](https://docs.stripe.com/webhooks)
- [Stripe process undelivered events](https://docs.stripe.com/webhooks/process-undelivered-events)
- [GitHub viewing webhook deliveries](https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/viewing-webhook-deliveries)
- [GitHub validating webhook deliveries](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries)
- [Shopify webhook troubleshooting](https://shopify.dev/docs/apps/build/webhooks/troubleshoot)
- [Linear webhooks](https://linear.app/developers/webhooks)
- [Zendesk webhooks](https://developer.zendesk.com/documentation/webhooks/)
- [LaunchDarkly webhooks](https://launchdarkly.com/docs/home/infrastructure/webhooks)
- [GitLab webhooks](https://docs.gitlab.com/user/project/integrations/webhooks/)

## Goals / Non-goals

### Goals

- Add reply-draft review statuses and an explicit state machine.
- Preserve provenance across AI generation, human edits, approval, rejection,
  stale marking, send request, send success, and send failure.
- Let Console operators edit and approve a draft inside the feedback detail
  workspace.
- Keep AI suggestion, human-edited draft, and sent text visibly separate in the
  UI and API.
- Add a controlled outbound abstraction for reviewed replies.
- Define reply send hooks as their own admin-managed, secret-backed resources.
- Capture generation source, source fingerprint, language, enrichment status,
  and available safety/blocker metadata for every AI suggestion revision.
- Return server-computed allowed actions and blockers so Console does not
  duplicate workflow policy.
- Ensure no LLM generation path can enqueue or execute a send.
- Require explicit member-or-admin permission for generate, edit, approve,
  reject, and send operations.
- Require idempotency and explicit state guards on send attempts.
- Expose a delivery log for reply-send hook attempts, including test events,
  failure details, retry counts, and redelivery for failed attempts.
- Add an admin test event so operators can validate endpoint reachability,
  response handling, and signature verification before sending a customer reply.
- Version and timestamp reply-hook signatures so receivers can enforce replay
  windows.
- Emit global audit records for reply-draft workflow actions without storing
  full customer-facing reply text in `audit_log`.
- Keep full text in business tables that participate in tenant scoping, GDPR
  export, and GDPR deletion.
- Add focused backend, integration, and Console tests for permissions, history,
  state transitions, stale guards, and delivery behavior.
- Keep existing `reply_draft` API fields stable while Console migrates to the
  structured workflow shape.

### Non-goals

- Do not add autonomous LLM sending.
- Do not add SMTP, Zendesk, Front, Intercom, or Help Scout native delivery.
- Do not route reviewed replies through the existing `notify_outbox` event path.
- Do not guarantee final end-customer delivery beyond attune's configured send
  hook accepting the send request.
- Do not redesign the feedback workflow status system.
- Do not replace `llm_audit`; it remains the LLM cost and call-history ledger.
- Do not make multi-operator real-time collaborative editing part of this issue.
- Do not expose public API-key endpoints for reply approval or send.
- Do not expose raw send-hook secrets or credential-bearing URLs after creation.

## Proposal

### Product model

Reply draft workflow becomes a four-layer model:

1. **Suggestion layer**: the LLM produces an AI suggestion from feedback context.
   This layer can generate, regenerate, and record source metadata. It cannot
   send.
2. **Draft layer**: an operator saves human text derived from the suggestion.
   Each save creates a revision and advances the active draft revision.
3. **Approval layer**: an operator explicitly approves the active revision.
   Approval captures the source fingerprint and active revision.
4. **Delivery layer**: an operator sends an approved, non-stale revision through
   a configured reply send hook. Delivery has its own attempt record and audit
   trail.

`sent` means attune successfully delivered the approved sent snapshot to the
configured reply send hook and the hook accepted responsibility for the request.
It does not mean the end customer opened, read, or even received the message in
the final downstream system. If a hook can report a provider message ID or final
customer-delivery status, attune stores that as external delivery metadata.

The Console detail view presents those layers separately:

- AI suggestion: read-only, with generation time and source evidence.
- Human draft: editable current text, revision, and save/approve controls.
- Sent reply: immutable sent snapshot and delivery status after send.

### Reply cycles

A feedback row has one active reply-draft cycle at a time. The active cycle
tracks the current suggestion, human draft, approval, and delivery state for one
operator reply. Each cycle has a monotonic `cycle_no` scoped to
`(tenant_id, feedback_id)`.

The first implementation starts with `cycle_no = 1`. If an operator deliberately
starts another reply after a terminal `sent` state, the service creates
`cycle_no + 1` and preserves the previous cycle's revisions, events, and
delivery attempts. Regenerate after `sent` must be an explicit "new reply cycle"
operation in the service layer; a plain regenerate request cannot silently
replace a sent reply.

This keeps one-feedback / one-reply behavior simple while preventing the schema
from collapsing if an upstream source maps a multi-turn support thread to one
feedback record.

### State machine

The active draft for a feedback row has one status:

| Status | Meaning | Allowed next states |
|---|---|---|
| `suggested` | Latest text is AI-generated and not human-edited. | `edited`, `approved`, `rejected`, `stale`, `suggested` |
| `edited` | Latest active text was saved by an operator. | `edited`, `approved`, `rejected`, `stale`, `suggested` |
| `approved` | Active revision was explicitly approved. | `send_pending`, `rejected`, `stale`, `suggested` |
| `send_pending` | A delivery attempt exists and is running or retryable. | `sent`, `send_failed` |
| `sent` | The configured send hook accepted the immutable sent snapshot. | terminal for that cycle |
| `send_failed` | Delivery exhausted or returned a terminal failure. | `approved`, `send_pending`, `rejected`, `stale` |
| `rejected` | An operator rejected the current suggestion or draft. | `suggested` |
| `stale` | The source context changed after the active revision or approval. | `suggested`, `edited`, `rejected` |

Rules:

- `Generate` or `Regenerate` writes a new AI suggestion revision and sets status
  to `suggested`, except when the active draft is `send_pending`.
- `Edit` creates a human revision and sets status to `edited`.
  Editing after a failed send clears stale external delivery markers so the
  current draft does not carry the previous attempt's failure state.
- `Approve` requires non-empty active text, a matching expected revision, and an
  active reply send hook so the reviewed destination is captured with the
  approved revision.
- `Send` requires status `approved`, matching expected revision, matching source
  fingerprint, and an enabled send hook.
- `Send` rejects an approval whose captured send-hook ID or URL fingerprint no
  longer matches the active hook.
- Failed-delivery redelivery rechecks the approved revision, source fingerprint,
  and captured send-hook fingerprint before retrying the existing attempt.
- `Send` is idempotent by `(draft_id, approved_revision_id, hook_id)`.
- `sent` stores the exact text snapshot that left attune.
- A plain regenerate request is rejected while status is `send_pending` or
  `sent`.
- `Reject` is rejected while status is `send_pending` so an in-flight delivery
  attempt cannot be overwritten by a late review action.
- A source-context mismatch marks the draft `stale` and blocks send.

### Source freshness

Every generated, edited, and approved revision stores a `source_fingerprint`.
The fingerprint is a stable hash of the feedback context used to produce or
approve the draft:

- `user_feedback.content`
- `source`, `source_user`, and `source_meta`
- `enriched_attrs`, `enriched_rationale`, and `language`
- `enriched_title` and `enrichment_status`

Approval separately captures the active send-hook identity and destination
fingerprint so a changed hook cannot silently receive a previously reviewed
reply.

This design gives attune Help Scout-style stale protection without depending on
a full conversation engine. If the source fingerprint differs during `Approve`
or `Send`, the service rejects the action with `409 Conflict`, appends a
`stale` event, and asks the operator to regenerate or re-approve.

Stale triggers:

- feedback content changes
- `source`, `source_user`, or `source_meta` changes
- enrichment is re-run or `enriched_attrs` / rationale / language changes
- tenant reply-draft prompt template changes
- LLM routing or safety policy used for reply drafts changes
- the source integration reports a newer customer-visible thread revision
- a send hook is disabled, deleted, or materially reconfigured after approval
- an operator generates a new suggestion over an approved but unsent revision

### Source and safety metadata

Every AI revision stores a bounded metadata object that lets operators and
auditors understand the source context without reconstructing the LLM call from
logs. The persisted object contains source channel, language, enrichment
status, source fingerprint, and safety/blocker labels when the generator
supplies them. It is structured provenance, not a dump of the prompt or the
customer-facing reply.

If safety checks block generation, the workflow records a `generate_blocked`
event and returns the blocker to Console. It must not write a synthetic empty
draft as if the model had produced one.

### Allowed actions and blockers

The service returns allowed actions and blockers with every workflow read. The
UI renders server policy; it does not infer state transitions on its own.

Allowed actions:

- `regenerate`
- `edit`
- `approve`
- `reject`
- `send`

Blockers:

- `reply_draft_disabled`
- `not_enriched`
- `permission_denied`
- `cooldown`
- `revision_conflict`
- `stale_source`
- `reply_send_hook_missing`
- `send_hook_disabled`
- `send_in_progress`
- `already_sent`
- `send_failed`
- `safety_blocked`

### Database

Add a migration that creates workflow-owned tables while preserving existing
inline fields.

`reply_send_hooks`

| Column | Purpose |
|---|---|
| `id` | Primary key, UUID. |
| `tenant_id` | Tenant scope. |
| `name` | Operator-facing hook name. |
| `url_ciphertext`, `url_key_id` | Encrypted destination URL. |
| `url_host`, `url_fingerprint` | Redacted display and audit lookup fields. |
| `secret_ciphertext`, `secret_key_id` | Optional encrypted HMAC/shared secret. |
| `enabled`, `disabled_at` | Admin-controlled active state and disable timestamp. |
| `created_by`, `updated_by` | Admin actor IDs. |
| `created_at`, `updated_at` | Standard timestamps. |

Rules:

- Hook URLs and secrets are encrypted with `internal/infra/secretstore`.
- Associated data includes `reply_send_hook`, tenant ID, hook ID, and field
  name.
- Create and rotate responses may reveal a generated secret once. List and get
  responses never return raw secrets or raw URL paths.
- Logs, audit rows, and delivery errors use `url_host`, `url_fingerprint`, and
  hook ID instead of raw URL or secret values.

`reply_drafts`

| Column | Purpose |
|---|---|
| `id` | Primary key. |
| `tenant_id` | Tenant scope. |
| `feedback_id` | FK to `user_feedback(id)`. |
| `cycle_no` | Active reply cycle number for this feedback. |
| `status` | State machine value. |
| `active_revision_id` | Current revision. |
| `approved_revision_id` | Revision approved for send. |
| `sent_revision_id` | Revision that was delivered. |
| `source_fingerprint` | Latest active source hash. |
| `approved_source_fingerprint` | Source hash captured at approval. |
| `approved_hook_id` | Send hook selected at approval, nullable. |
| `approved_hook_fingerprint` | Hook destination fingerprint captured at approval. |
| `sent_hook_id` | Hook used for the sent snapshot. |
| `external_message_id` | Optional provider or customer-system message ID. |
| `external_delivery_status` | Optional downstream delivery status reported by the hook. |
| `last_blocker` | Last server-computed blocker, if any. |
| `revision` | Monotonic optimistic-concurrency counter. |
| `generated_at`, `generated_by` | Latest generation metadata. |
| `edited_at`, `edited_by` | Latest edit metadata. |
| `approved_at`, `approved_by` | Approval metadata. |
| `sent_at`, `sent_by` | Send metadata. |
| `created_at`, `updated_at` | Standard timestamps. |

Constraints:

- `UNIQUE (tenant_id, feedback_id, cycle_no)`
- partial unique index that allows only one non-terminal active cycle per
  `(tenant_id, feedback_id)`
- `CHECK (status IN (...))`
- `FOREIGN KEY (feedback_id) REFERENCES user_feedback(id) ON DELETE CASCADE`

`reply_draft_revisions`

| Column | Purpose |
|---|---|
| `id` | Primary key. |
| `draft_id`, `tenant_id`, `feedback_id` | Scope and joins. |
| `cycle_no` | Reply cycle number. |
| `revision_no` | Per-draft revision number. |
| `origin` | `ai`, `human`, or `system`. |
| `content` | Full draft text. |
| `content_sha256` | Text hash for audit references. |
| `source_fingerprint` | Context hash for this revision. |
| `metadata` | JSONB source, language, enrichment, and safety metadata. |
| `created_by` | Operator or system actor ID. |
| `created_at` | Timestamp. |

`reply_draft_events`

| Column | Purpose |
|---|---|
| `id` | Primary key. |
| `draft_id`, `tenant_id`, `feedback_id` | Scope and joins. |
| `cycle_no` | Reply cycle number. |
| `event_type` | `generate`, `generate_blocked`, `edit`, `approve`, `reject`, `stale`, `send_request`, `send_success`, `send_failure`. |
| `from_status`, `to_status` | State transition. |
| `revision_id` | Revision involved in the event. |
| `hook_id` | Hook involved in the event; approval records the hook captured for the reviewed destination. |
| `actor_id`, `actor_type` | Human, system, or worker actor. |
| `summary` | Short operator-facing event summary. |
| `metadata` | JSONB for hashes, delivery IDs, model name, hook ID, and error codes. |
| `created_at` | Timestamp. |

`reply_delivery_attempts`

| Column | Purpose |
|---|---|
| `id` | Primary key and delivery ID. |
| `draft_id`, `tenant_id`, `feedback_id`, `revision_id` | Scope and idempotency target. |
| `hook_id` | Configured reply send hook. |
| `event_type` | `reply.send` or synthetic `reply.test`. |
| `idempotency_key` | Stable send or test delivery key. |
| `status` | `pending`, `accepted`, `failed`, `dead`. |
| `attempts`, `max_attempts`, `next_retry_at`, `error`, `http_status` | Retry state. |
| `request_fingerprint` | Stable dedup key. |
| `external_message_id` | Optional downstream message ID returned by the hook. |
| `response_meta` | Redacted response details. |
| `requested_by_type`, `requested_by`, `requested_at` | Actor and request time. |
| `created_at`, `updated_at`, `completed_at` | Timestamps. |

Backfill:

- Existing non-empty `user_feedback.reply_draft` rows become
  `reply_drafts.status='suggested'` with `cycle_no=1`.
- Each backfilled row gets one AI-origin revision created by `legacy-backfill`
  and one `generate` event with `actor_type='system'`.
- `reply_draft_generated_at` is preserved when present.

Compatibility:

- Keep `user_feedback.reply_draft` and `reply_draft_generated_at` as a denormalized
  compatibility read model.
- On generate/edit, update the inline field to the active revision body.
- On reject, keep the last inline field until API consumers migrate; structured
  workflow status tells Console it is rejected.

GDPR:

- GDPR export includes `reply_drafts`, `reply_draft_revisions`,
  `reply_draft_events`, and `reply_delivery_attempts`.
- GDPR delete removes delivery attempts before deleting feedback rows if any FK
  does not cascade.
- Global `audit_log` rows remain append-only and contain only hashed or
  summarized reply metadata.

### Repository and service

Add repository methods under `internal/repo/replydraft`:

- `GetActiveDraft(ctx, tenantID, feedbackID)`
- `StoreGeneratedDraft(ctx, feedbackID, tenantID, draft, actorID)`
- `EditDraft(ctx, tenantID, feedbackID, content, expectedRevision, actor)`
- `ApproveDraft(ctx, tenantID, feedbackID, expectedRevision, actor)`
- `RejectDraft(ctx, tenantID, feedbackID, expectedRevision, actor)`
- `PrepareDelivery(ctx, tenantID, feedbackID, idempotencyKey, expectedRevision, actor)`
- `MarkDeliveryAccepted(ctx, attemptID, httpStatus, externalMessageID)`
- `MarkDeliveryFailed(ctx, attemptID, httpStatus, cause)`
- `ListEvents(ctx, tenantID, feedbackID)`
- `ListRevisions(ctx, tenantID, feedbackID)`
- `UpsertHook(ctx, input)`
- `DisableSendHook(ctx, input)`
- `GetActiveHook(ctx, tenantID)`
- `GetLatestHook(ctx, tenantID)`
- `ListDeliveryAttempts(ctx, tenantID, limit)`
- `PrepareHookTest(ctx, tenantID, idempotencyKey, actor)`
- `ClaimDueDeliveries(ctx, limit, actor)`
- `ResetStalePendingDeliveries(ctx, olderThan)`
- `PrepareRedelivery(ctx, tenantID, attemptID, actor)`

Add a workflow service under `internal/service/replydraft` that owns:

- status transitions
- expected-revision checks
- source-fingerprint checks
- source and safety metadata capture
- allowed-action and blocker calculation
- idempotency-key validation for send attempts
- policy-aware hook URL validation using the same egress policy enforced at
  delivery time
- send-hook destination fingerprint checks
- permission-aware actor metadata supplied by handlers
- audit event composition
- inline `user_feedback.reply_draft` synchronization
- scheduled due-delivery retry through `DeliveryWorker`

`ReplyDrafter.Generate` continues to own prompt rendering and LLM completion, but
its persistence step changes from overwrite-only `UpdateReplyDraft` to workflow
`UpsertGenerated`. Worker and synchronous regenerate share the same persistence
path.

### API contract

Update `proto/attune/v1/ingest.proto` and run `make proto`.

Keep these existing fields on `FeedbackDetail`:

- `reply_draft`
- `reply_draft_generated_at`
- `reply_draft_enabled`

Add a structured workflow field:

```proto
message ReplyDraftWorkflow {
  string draft_id = 1;
  int64 feedback_id = 2;
  int32 cycle_no = 3;
  string status = 4;
  optional string active_revision_id = 5;
  optional string approved_revision_id = 6;
  optional string sent_revision_id = 7;
  string active_text = 8;
  repeated ReplyDraftRevision revisions = 9;
  repeated ReplyDraftEvent events = 10;
  repeated string allowed_actions = 11;
  repeated string blockers = 12;
  bool hook_configured = 13;
  optional string generated_at = 14;
  optional string generated_by = 15;
  optional string edited_at = 16;
  optional string edited_by = 17;
  optional string approved_at = 18;
  optional string approved_by = 19;
  optional string rejected_at = 20;
  optional string rejected_by = 21;
  optional string sent_at = 22;
  optional string sent_by = 23;
  optional string external_delivery_status = 24;
  optional string external_message_id = 25;
  int64 revision = 26;
  string updated_at = 27;
}

message ReplyDraftRevision {
  string id = 1;
  string draft_id = 2;
  int32 cycle_no = 3;
  int32 revision_no = 4;
  string origin = 5;
  string content = 6;
  string created_by = 7;
  string created_at = 8;
  google.protobuf.Struct metadata = 9;
}
```

New console RPCs:

- `RegenerateReplyDraft`
  - `POST /fb/v1/console/feedback/{id}/reply-draft/regenerate`
  - body: empty JSON object
  - returns the legacy `reply_draft` fields and the structured workflow
- `UpdateReplyDraft`
  - `POST /fb/v1/console/feedback/{id}/reply-draft/edit`
  - body: `content`, `expected_revision`
- `ApproveReplyDraft`
  - `POST /fb/v1/console/feedback/{id}/reply-draft/approve`
  - body: `expected_revision`
- `RejectReplyDraft`
  - `POST /fb/v1/console/feedback/{id}/reply-draft/reject`
  - body: `expected_revision`
- `SendReplyDraft`
  - `POST /fb/v1/console/feedback/{id}/reply-draft/send`
  - body: `expected_revision`, optional generated-client `idempotency_key`
  - required header: `Idempotency-Key`

New admin reply-send-hook RPCs:

- `GetReplySendHook`
  - `GET /fb/v1/console/reply-send-hook`
- `UpsertReplySendHook`
  - `PUT /fb/v1/console/reply-send-hook`
  - body: `url`, optional `secret`, `name`, and `enabled`
- `DisableReplySendHook`
  - `DELETE /fb/v1/console/reply-send-hook`
- `GetReplySendHookHealth`
  - `GET /fb/v1/console/reply-send-hook/health`
  - returns aggregate total, accepted, failed, dead, pending, retryable counts,
    plus latest delivery and latest problem delivery
- `ListReplySendHookDeliveries`
  - `GET /fb/v1/console/reply-send-hook/deliveries`
- `TestReplySendHook`
  - `POST /fb/v1/console/reply-send-hook/test`
- `RedeliverReplySendHookDelivery`
  - `POST /fb/v1/console/reply-send-hook/deliveries/{id}/redeliver`

Delivery attempt status uses the stored values `pending`, `accepted`, `failed`,
and `dead`. Workflow status maps a hook-accepted reply send to `sent`.

Idempotency:

- `SendReplyDraft` accepts the standard `Idempotency-Key` header.
- Storage uniqueness is `(tenant_id, draft_id, idempotency_key)` and the stored
  request fingerprint binds the approved revision, hook destination
  fingerprint, and key used for the external delivery attempt.
- Reusing a key with a different request fingerprint returns
  `409 IDEMPOTENCY_CONFLICT`.
- Reusing a key while the first request is still processing returns
  `409 REQUEST_IN_PROGRESS`.
- Successful replay after hook acceptance returns the stored workflow and
  delivery status;
  failed or dead attempts with the same fingerprint can be reset for an explicit
  retry.

### Permissions

Map actions to RBAC:

| Action | Required role |
|---|---|
| View draft workflow and timeline | viewer, member, admin |
| Generate/regenerate | member, admin |
| Save edit | member, admin |
| Approve | member, admin |
| Reject | member, admin |
| Send | member, admin |
| Configure or replace reply send hooks | admin |
| Disable reply send hooks | admin |

Rationale:

- `member` is the operational role that can act on feedback.
- `viewer` can inspect AI output and history but cannot spend LLM budget or
  mutate operator workflow state.
- `admin` owns tenant-level send-hook configuration.
- Send hook configuration is a tenant-level integration setting because it can
  move customer-facing text into external systems.

Implementation options:

- Add a router helper that wraps `rbac.RequireMember()` and apply it to all
  reply-draft mutation routes.
- Keep handler-level tenant ownership checks so hidden or cross-tenant feedback
  returns the established 404 behavior.
- In legacy no-RBAC mode, preserve the existing admin-session compatibility
  behavior used by Console routes.

### Audit and provenance

Use two ledgers:

1. `reply_draft_events` is the full business provenance ledger for the feedback
   detail workspace. It can reference full-text revisions because it stays in
   tenant-scoped business data and participates in GDPR flows.
2. `audit_log` is the global control-plane ledger. It records action, actor,
   target, status, revision IDs, text hashes, lengths, hook IDs, and delivery
   IDs, but not full reply text.

Add audit actions:

- `reply_draft.generate`
- `reply_draft.generate_blocked`
- `reply_draft.edit`
- `reply_draft.approve`
- `reply_draft.reject`
- `reply_draft.stale`
- `reply_draft.send.request`
- `reply_draft.send.success`
- `reply_draft.send.failure`
- `reply_send_hook.upsert`
- `reply_send_hook.disable`

Required code changes:

- Extend `internal/service/auditlog/actions.go`.
- Add a migration that updates the `audit_log` action CHECK constraint.
- Update `internal/handlers/console/router_audit_inventory_test.go` so
  reply-draft mutation routes are audited instead of exempt.
- Add tests that assert full reply text is not written to `audit_log`.

### Controlled send hooks

The existing outbound framework is for notifications leaving attune:
per-feedback events, digests, GitHub issues, Slack messages, Lark cards, Discord
messages, and raw webhooks. Reviewed replies are a different domain: they are
customer-facing or customer-system-facing messages derived from human-approved
text.

Add a reply-specific outbound capability while reusing the safe transport
building blocks:

```go
type ReplyChannel interface {
    ID() string
    RenderReply(envelope *ReplyEnvelope, target Target) (Rendered, error)
}

type ReplyEnvelope struct {
    Version       string         `json:"version"`
    Timestamp     string         `json:"timestamp"`
    EventType     string         `json:"event_type"`
    TenantID      string         `json:"tenant_id"`
    FeedbackID    string         `json:"feedback_id"`
    CycleNo       int            `json:"cycle_no"`
    DraftID       string         `json:"draft_id"`
    RevisionID    string         `json:"revision_id"`
    DeliveryID    string         `json:"delivery_id"`
    Reply         ReplyPayload   `json:"reply"`
    Feedback      map[string]any `json:"feedback"`
}

type ReplyPayload struct {
    Body       string `json:"body"`
    BodySHA256 string `json:"body_sha256"`
    SentBy     string `json:"sent_by"`
    SentAt     string `json:"sent_at"`
}
```

The first channel is a reviewed-reply webhook. It sends the explicit
`reply.send` event to a configured customer system.

Reviewed-reply webhook contract:

- Method: `POST`
- Content type: `application/json`
- Signature header: `X-Attune-Signature`
- Timestamp header: `X-Attune-Timestamp`
- Delivery ID header: `X-Attune-Delivery-Id`
- Idempotency header: `X-Attune-Idempotency-Key`
- Signature version: `v1=` plus HMAC-SHA256 over
  `X-Attune-Timestamp + "." + raw JSON request bytes`.
- Success: any 2xx response means the hook accepted responsibility for the
  reviewed reply.
- Retryable failure: 408, 409, 425, 429, and 5xx unless the adapter classifies
  the response as terminal.
- Terminal failure: malformed hook response, 400, 401, 403, 404, 410, 413, 415,
  and provider-specific permanent rejection.

Delivery rules:

- Generate, regenerate, edit, and approve never create delivery attempts.
- Only `SendReplyDraft` creates or resumes a `reply_delivery_attempts` row.
- Delivery uses the same hardened HTTP transport principles as notify adapters:
  OTel transport, timeout, retry classification, redacted logs, and stable
  delivery IDs.
- Delivery is idempotent per approved revision and hook.
- Delivery attempts do not mutate feedback workflow status until the hook
  accepts or fails.
- Delivery completion is fenced by the attempt and the draft snapshot: only a
  still-pending attempt can be marked accepted or failed, and `reply.send`
  completion must still match the active draft's `send_pending` state, approved
  revision, and approved hook. Late failures cannot downgrade accepted sends,
  and late successes from obsolete attempts cannot overwrite a newly edited
  draft.
- Hook request bodies include the sent snapshot, not the current mutable draft.
- A hook success can include `external_message_id`; absence of that field does
  not block `sent`. Attune still records its own `external_delivery_status` as
  the hook acceptance state.
- Failed `reply.send` attempts with due `next_retry_at` are claimed with
  `FOR UPDATE SKIP LOCKED`, reset to `pending`, and resent by the
  reply-delivery worker. Stale pending rows are recovered on worker startup.
- Delivery attempts have tenant-scoped listing indexes plus dedicated partial
  indexes for global due-retry claims and stale-pending recovery.
- `reply.test` attempts have a tenant-scoped partial unique idempotency index
  because their `draft_id` is intentionally null. Accepted test replays return
  the cached attempt, pending test replays report in-progress, and failed/dead
  test replays reuse the same attempt for explicit redelivery. The test request
  fingerprint includes the hook URL fingerprint so reusing a key after the hook
  destination changes returns an idempotency conflict. Failed `reply.test`
  attempts are not scheduled into the background retry worker.
- Automatic redelivery reuses the same source-freshness and approved-hook
  freshness checks as manual redelivery. Attempts whose reviewed context or hook
  is no longer valid become terminal `dead` attempts instead of looping.

Configuration:

- Use the dedicated `reply_send_hooks` table. Do not store reply hooks in
  `tenant_notify_targets`.
- Hook create/update/rotate/test routes are admin-only.
- Hook names are trimmed at the service boundary, blank names use the default
  display name, and over-long or control-character names are rejected before
  they reach database constraints.
- Updating an existing hook with a blank secret preserves the encrypted current
  secret, including after a temporary disable; blank secret generation is only
  for first-time configuration or legacy rows that have no stored secret.
- Hook test sends a synthetic non-customer payload and records a global audit
  row plus a `reply.test` delivery attempt for diagnostics and redelivery.
- Hook get returns ID, name, enabled/disabled state, URL host, URL fingerprint,
  and timestamps. A disabled hook remains visible for admin inspection, but it
  is never returned by active-send lookup. The API never returns raw URL paths
  or raw secrets.

### Console UX

Replace the current inline reply draft card with a dedicated workflow section in
the feedback detail workspace. The section renders:

- read-only AI suggestion
- editable human draft textarea
- status badge
- allowed-action-driven controls
- blocker messages from the server
- stale warning when source fingerprint changed
- source and safety summary
- preflight checklist for approval, hook availability, freshness, and revision
  match
- source evidence panel with raw feedback, source metadata, confidence, and AI
  rationale
- AI-versus-human revision diff summary
- latest revision metadata
- compact revision and event timeline
- sent snapshot and delivery result after send

Controls:

- `Regenerate`
- `Save`
- `Approve`
- `Reject`
- `Send`

Control behavior:

- `Save` requires changed text.
- `Approve` requires a saved active revision.
- `Send` is disabled until status is `approved` and not stale.
- `Send` requires a confirmation dialog that shows the exact sent snapshot,
  source, actor, approved revision, and hook readiness.
- `Regenerate` warns before replacing an unsaved or approved draft.
- `Reject` records an event and leaves history visible.
- All mutation failures show specific conflict, permission, or delivery messages.

The UI must avoid implying that the AI can send. Labels should attribute
customer-facing delivery to the human operator or configured hook.

The implemented feedback detail sheet also keeps the sheet and reply workspace
surfaces explicitly opaque. Browser coverage checks both surfaces so the dimmed
feedback queue never visually bleeds through the review workspace.

The implemented reply-send-hook configuration page also includes the receiving
contract that operators need to wire a real support bridge: signed request
headers, delivery and idempotency headers, a representative `reply.send` JSON
payload, and the security checks for HTTPS production destinations, loopback
HTTP local-test endpoints, generated secrets, approved-hook fingerprint
locking, sanitized audit logging, and redacted transport errors that never
expose credential-bearing hook paths or query strings. It also summarizes the
recent delivery health window so admins can see accepted, failed, manually
redeliverable, and dead counts before scanning individual attempts. Successful hook configure
and disable responses are written into the Console query cache immediately so
the current-hook summary reflects the server-accepted host, fingerprint, and
enabled state without requiring a second read.
Hook configure, disable, test, redelivery, and reply send mutations invalidate
delivery health and delivery-log queries so the integration page reflects the
latest operational state. The feedback detail sheet keeps the most recent
successful reply workflow response until the refreshed feedback detail snapshot
catches up, preventing older mutation results from overriding newer back-to-back
actions.

### Metrics

Existing LLM generation metrics remain in place. The workflow adds durable
events, global audit rows, and delivery attempts so operational and product
analytics can be derived from stored tenant-scoped records:

- generated-to-approved rate
- generated-to-sent rate
- approved-to-sent rate
- rejection reason distribution
- median edit distance from AI suggestion to human draft
- stale rate by trigger
- send failure rate by hook
- time from generation to approval
- time from approval to hook acceptance

### Rate limits and cost controls

Keep the existing manual regenerate protections:

- per-tenant regenerate limiter
- per-row regenerate cooldown
- tenant `reply_draft_enabled`
- tenant `reply_draft_min_confidence`
- fail-closed workflow snapshot lookup before manual regenerate, so the handler
  does not generate when it cannot verify that no reply send is in progress

Add send protections:

- idempotency required by service policy when a send request reaches the
  delivery layer
- hard block when hook is missing, disabled, or reconfigured after approval
- revision and source-freshness checks before creating a delivery attempt
- duplicate-send rejection once a cycle reaches `sent`
- audit emission for send request, send success, and send failure

Generation cost remains governed by `llm_audit`, LLM runtime rate limits, tenant
enablement, and confidence gating. Send hook delivery is not an LLM cost center,
but it can move customer-facing content outside attune, so it is guarded by
idempotency, workflow state, hook freshness, and audit records.

## Alternatives considered

### Add columns to `user_feedback`

Adding `reply_draft_status`, `reply_draft_approved_at`, and
`reply_draft_sent_at` would be quick, but it would still lose edit history and
make provenance difficult to query. It also makes GDPR export/delete and audit
boundaries harder as the workflow grows.

Rejected because #164 explicitly requires edit history and provenance.

### Store every change only in `feedback_audit_log`

`feedback_audit_log` is useful for workflow field changes, but using it as the
primary reply-draft data model would mix structured product state with audit
records and would force full reply bodies into a generic audit table.

Rejected because the workflow needs current state, revision guards, delivery
attempts, and PII-aware storage.

### Use only global `audit_log`

The global audit log is append-only and intentionally sanitized. It is the right
ledger for sensitive Console actions, but not the right home for full editable
reply bodies.

Rejected because operators need a readable history and GDPR needs business-data
ownership of full text.

### Reuse `notify_outbox`

`notify_outbox` is already reliable, observable, and adapter-driven. However,
its domain is team notification delivery about feedback events. A reviewed reply
is customer-facing text with different permissions, idempotency, and safety
expectations.

Rejected for direct reuse. The proposal reuses transport and adapter patterns
without reusing the event-notification queue.

### Store reply hooks in `tenant_notify_targets`

`tenant_notify_targets` already stores destination type, audience, URL, secret,
timeout, disabled state, and signature version. Reusing it would reduce schema
work.

Rejected because audience semantics are wrong for customer-facing reviewed
replies. A target configured for internal team notifications must never become a
customer reply destination by changing an audience value. Reply hooks also need
stronger raw URL and secret exposure rules than the current notify-target list
API provides.

### Send directly from the handler

Direct synchronous send would be simpler but would create poor retry behavior,
weak delivery observability, and ambiguous user experience on upstream timeouts.

Rejected because delivery needs its own attempt ledger and idempotency key.

### Make send admin-only

Admin-only send is safer but does not match the operator workflow implied by
#164 or the existing member role as the operational actor.

Rejected for the default workflow. Admin remains required for hook
configuration; member-or-admin can send through an already configured hook.

### Treat hook acceptance as final customer delivery

Some customer systems will send the reviewed reply directly to a user. Others
will only enqueue it for additional moderation or channel-specific delivery.
Calling hook acceptance "customer delivered" would overstate attune's evidence.

Rejected. The workflow status `sent` means hook acceptance. Downstream message
IDs and delivery statuses are optional external metadata.

### Make idempotency optional only

Console can prevent double clicks, and the delivery table has a dedup key, so a
caller-supplied idempotency key could be treated as nice-to-have.

Rejected for send. A reviewed reply is a customer-facing action, and retries may
cross process boundaries. The service requires an idempotency record before it
creates or resumes a delivery attempt.

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| Operators send outdated replies after new context appears. | Store source fingerprints and block send on mismatch. Mark the draft `stale`. |
| Two operators overwrite each other. | Require `expected_revision` on edit, approve, reject, and send. Return `409 Conflict` on mismatch. |
| Full customer reply text leaks into global audit. | Store full text only in business tables. Store hashes, lengths, revision IDs, and hook IDs in `audit_log`. |
| Reply hooks are confused with team notification targets. | Use an explicit reply-send hook purpose/type and separate UI copy. |
| Duplicate send on retry or double click. | Unique `(tenant_id, draft_id, idempotency_key)` attempts plus a request fingerprint that binds the approved revision and hook destination fingerprint. |
| A delayed success or failure arrives after the workflow has advanced. | Treat delivery completion as a pending-attempt transition fenced by the approved revision and hook; stale completions become no-ops and preserve the current draft state. |
| Delivery hook accepts a request but downstream customer send fails. | Treat the hook as an integration boundary and record accepted delivery separately from provider-specific customer delivery unless the hook returns terminal failure details. |
| Hook URL or secret leaks through logs, audit, or API responses. | Encrypt URL and secret values, return only host/fingerprint, and add redaction tests. |
| Operators approve text generated from weak or unsafe context. | Store source/safety metadata, return blockers, and record `generate_blocked` events. |
| A sent feedback row needs another reply cycle. | Store `cycle_no` on drafts, revisions, events, and payloads so sent cycles remain immutable and schema-safe. |
| Console sends with stale policy because it inferred actions locally. | Return `allowed_actions` and `blockers` from the backend. |
| Migration creates duplicate workflow rows. | Backfill with `UNIQUE (tenant_id, feedback_id, cycle_no)` and idempotent insert/select logic. |
| New tables are missed by GDPR export/delete. | Extend GDPR repositories and integration tests in the same change. |
| Console complexity grows inside `detail-sheet.tsx`. | Extract a dedicated workflow section and focused API hooks. |

## Implementation plan

1. Add this proposal and keep it linked from the implementing PR.
2. Add database migration for `reply_send_hooks`, workflow tables, delivery
   attempts, cycle indexes, status checks, and audit action CHECK updates.
3. Backfill existing inline drafts into workflow tables with `cycle_no=1`.
4. Extend `internal/repo/replydraft` with workflow, revision, event, send-hook,
   and delivery-attempt methods.
5. Add encrypted send-hook secret handling using `internal/infra/secretstore`
   and associated data scoped to tenant, hook, and field.
6. Add `internal/service/replydraft` workflow methods for generate, edit,
   approve, reject, stale marking, allowed-action calculation, and send request.
7. Add source-fingerprint and source/safety metadata builders.
8. Change `ReplyDrafter.Generate` persistence to write workflow revisions and
   events while keeping inline `user_feedback.reply_draft` synchronized.
9. Extend `proto/attune/v1/ingest.proto` with workflow messages, allowed actions,
   blockers, hook readiness, delivery status fields, and new RPCs, then run
   `make proto`.
10. Add Console handlers and member-or-admin route protection for all
    reply-draft mutation endpoints.
11. Add admin-only send-hook get, upsert, disable, test, delivery-list, and
    failed-redelivery endpoints plus a Console configuration and operations
    page with recent delivery health; secret rotation uses hook replacement.
12. Add send idempotency handling using the standard `Idempotency-Key` header
    semantics.
13. Add global audit emission and update audit inventory tests.
14. Add reply send hook abstraction, reviewed-reply webhook delivery attempt
    handling, test events, versioned timestamped signatures, retry-backed
    sends, and redacted attempt metadata.
15. Add save-time hook URL validation against the configured egress policy.
16. Add an automatic reply-delivery worker for due failed attempts and stale
    pending recovery, with targeted indexes for the worker scans.
17. Preserve regenerate limiter/cooldown and enforce send idempotency plus
    workflow guards.
18. Extend GDPR export/delete repositories for new reply-draft and hook tables.
19. Implement the Console workflow section and add API hooks for edit, approve,
    reject, send, and cache-family invalidation after workflow mutations.
20. Add backend, integration, and Console tests.
    Include browser E2E coverage for the reply draft edit, approve, and send
    path, plus the admin reply-send-hook save, test, failed-redelivery, and
    disable path.
21. Update `CHANGELOG.md` under `[Unreleased]`.

## Verification

Backend:

- `go test ./internal/repo/replydraft ./internal/service/replydraft`
- `go test ./internal/handlers/console/feedback`
- `go test ./internal/service/auditlog ./internal/repo/auditlog`
- `go test ./internal/repo/gdpr ./internal/handlers/console/gdpr`
- `go test ./internal/outbound/... ./internal/notify/...`
- Unit coverage for allowed actions, blockers, source fingerprints,
  source/safety metadata, send-hook secret redaction, send idempotency, and
  hook-acceptance delivery semantics.

Integration:

- Add PostgreSQL integration coverage under
  `test/integration/postgres/replydraft/`.
- Cover migration backfill, tenant scoping, edit history, revision conflicts,
  stale transitions, send-hook encryption, idempotency replay/conflict,
  delivery attempts, audit rows, and GDPR deletion.

Console:

- `pnpm vitest run src/features/feedback/components/detail-sheet.test.tsx`
- `pnpm vitest run src/features/reply-send-hook/components/reply-send-hook-page.test.tsx`
- `pnpm test:e2e:a11y`
- Add focused tests for `ReplyDraftWorkflowSection`.
- Cover server-driven allowed actions/blockers, stale warning, send confirmation,
  cache invalidation, immediate hook cache updates, and hook delivery summaries.
- Browser E2E covers the feedback detail sheet edit, approve, and send flow with
  guarded expected revisions and an `Idempotency-Key`, plus the admin
  reply-send-hook save, generated secret, failed-redelivery, disable, route
  title, public HTTP rejection, loopback HTTP acceptance, and
  background-surface regression checks.
- `pnpm tsc -b --noEmit`
- `pnpm biome check`

Contract:

- `make proto`
- `go test ./internal/handlers/console -run AuditInventory`
- `scripts/lint-artifacts.sh --strict`
- Relevant subset first, then `make ci-check` before merge.

Run results:

- `make proto` passed after regenerating Go, Console TS, Node SDK, Go SDK, and
  OpenAPI artifacts, then running `buf lint`.
- `go test ./internal/repo/replydraft ./internal/service/replydraft ./internal/handlers/console/feedback ./internal/handlers/console ./internal/infra/database -run 'Reply|Hook|Delivery|Audit|Migrations' -count=1`
  passed.
- `go test -tags integration ./test/integration/postgres/replydraft -run 'Delivery|Redelivery|Pending|Stale' -count=1`
  passed in 31.889s on the final code.
- `go test ./internal/repo/replydraft ./internal/service/replydraft ./internal/handlers/console/feedback -run 'Reply|Hook|Delivery|Idempotency|SendReturns' -count=1`
  passed after adding strict reply-send idempotency-conflict handling and
  failed-send state-marking error propagation.
- `go test -tags integration ./test/integration/postgres/replydraft -run 'Delivery|Accepted|Idempotency|Pending|Stale' -count=1`
  passed in 36.211s, covering accepted replay and same-key different-revision
  conflict.
- `go test -tags integration ./test/integration/postgres/replydraft -run 'Approve|Delivery|Accepted|Idempotency|Pending|Stale|Redelivery' -count=1`
  passed in 49.021s after tightening approval to require a configured hook and
  adding redelivery freshness checks for changed hooks.
- `go test ./internal/repo/gdpr ./internal/service/gdpr -count=1` passed after
  adding reply-draft workflow rows, revisions, events, and delivery attempts to
  the GDPR export ZIP and delete counts.
- `go test -tags integration ./test/integration/postgres/gdpr -count=1`
  passed in 22.900s, covering export files, delete counts, FK cleanup, scheduled
  delete execution, export revoke, and the existing notify-outbox erasure
  regression.
- `pnpm --ignore-workspace exec tsc -b --noEmit --pretty false` passed.
- `pnpm --ignore-workspace biome check` passed.
- `pnpm --ignore-workspace vitest run src/features/reply-send-hook/components/reply-send-hook-page.test.tsx`
  passed, including empty and failed delivery-health summaries, local
  loopback hook configuration, and immediate current-hook cache updates after
  configure and disable mutations.
- `pnpm --ignore-workspace vitest run src/features/feedback/api/regenerate-reply-draft.test.tsx src/features/feedback/components/detail-sheet.test.tsx`
  passed: 32 tests, including feedback cache invalidation and reply-send-hook
  delivery-log invalidation after send success and send failure.
- `pnpm --ignore-workspace vitest run src/features/session/components/authed-shell.test.tsx src/features/reply-send-hook/components/reply-send-hook-page.test.tsx src/features/feedback/api/regenerate-reply-draft.test.tsx src/features/feedback/components/detail-sheet.test.tsx`
  passed: 44 tests, covering the shell, reply-send-hook page, reply-draft API
  cache invalidation, and feedback detail workflow surfaces.
- `pnpm --ignore-workspace exec vite build` passed with the existing
  large-chunk warning.
- `curl -I --max-time 5 http://127.0.0.1:4174/console/integrations/reply-send-hook`
  returned `HTTP/1.1 200 OK`, and
  `curl -sS --max-time 5 'http://127.0.0.1:4174/fb/v1/console/reply-send-hook/deliveries?limit=25'`
  returned the failed delivery fixture used by the browser/E2E flow.
- `ATTUNE_CONSOLE_E2E_PORT=4174 pnpm --ignore-workspace exec playwright test --config playwright.config.ts --project=chromium-desktop --grep 'reply draft review workflow|reply send hook page'`
  passed: 2 tests, covering the reply-draft edit/approve/send flow and the
  reply-send-hook save, test, failed-redelivery, disable, and opaque
  `body`/`#root`/`main` background flow in Chromium.
- `ATTUNE_CONSOLE_E2E_PORT=4174 pnpm --ignore-workspace exec playwright test --config playwright.config.ts --project=chromium-desktop --grep 'reply send hook page'`
  passed after adding the delivery-health summary to the 4174 preview.
- Chromium visual/CSS verification captured
  `/tmp/attune-reply-send-hook-health.png`; computed styles showed `body` and
  `#root` on an opaque theme background, and the health band visible with
  failed/retryable/dead summary, so the page no longer depends on a transparent
  document background.
- The reply-send-hook browser gate now asserts opaque computed backgrounds for
  `body`, `#root`, and `main`; it failed against the transparent `main` surface,
  then passed after `AuthedShell` set an explicit `bg-background` on
  `#main-content`.
- `git diff --check` passed.
- `go test ./internal/service/replydraft ./internal/repo/replydraft ./internal/notify ./cmd/attune`
  passed after adding policy-aware hook validation, the reply-delivery retry
  worker, and the `reply-send-hook` outbound metrics label.
- `go test ./internal/service/replydraft ./internal/repo/replydraft ./internal/infra/database ./cmd/attune`
  passed after adding worker drain hardening and the due-retry/stale-pending
  partial indexes.
- `go test ./internal/service/replydraft ./cmd/attune` passed after changing
  blank-secret hook updates to preserve the existing encrypted secret.
- `go test -tags=integration ./test/integration/postgres/replydraft` passed in
  85.779s, covering due failed-attempt claiming and a real service-to-Postgres
  signed `httptest` receiver flow that verifies `X-Attune-Signature`, accepts
  the reply, and records `external_message_id`.
- `go test -tags=integration ./test/integration/postgres/replydraft` passed in
  89.080s after the worker-index hardening changes.
- `go test -tags=integration ./test/integration/postgres/replydraft` passed in
  64.664s after adding the real DB assertion that blank-secret hook updates
  preserve the stored secret ciphertext.
- `pnpm --ignore-workspace vitest run src/features/reply-send-hook/components/reply-send-hook-page.test.tsx`
  passed: 9 tests covering the updated secret-preservation form guidance.
- `go test ./internal/handlers/console/feedback ./internal/repo/replydraft ./internal/service/replydraft ./cmd/attune`
  passed after changing manual regenerate to fail closed when workflow snapshot
  lookup fails.
- `go test -tags=integration ./test/integration/postgres/replydraft` passed in
  103.940s after adding assertions that approve events capture the approved hook
  ID and `send_pending` drafts cannot be rejected.
- `pnpm --ignore-workspace vitest run src/features/reply-send-hook/components/reply-send-hook-page.test.tsx src/features/feedback/api/regenerate-reply-draft.test.tsx src/features/feedback/components/detail-sheet.test.tsx`
  passed: 43 tests covering hook observability invalidation, send health/log
  invalidation, and back-to-back reply workflow mutation ordering.
- `go test -tags=integration ./test/integration/postgres/replydraft -run 'UpsertBlankSecretPreservesExistingSecret' -count=1`
  passed after verifying disabled hooks remain readable through `GetHook` and a
  blank-secret re-enable preserves the previous encrypted secret.
- `pnpm --ignore-workspace vitest run src/features/reply-send-hook/components/reply-send-hook-page.test.tsx`
  passed: 11 tests, including reload of an API-returned disabled hook with test
  actions disabled.
- `go test -tags=integration ./test/integration/postgres/replydraft -run 'ReplySendHookTest_IdempotencyKeyReturnsAcceptedAttemptFromCache' -count=1`
  passed after adding the partial unique `reply.test` idempotency index and
  verifying accepted test replays are cached while reused keys after hook
  destination changes return an idempotency conflict.
- `go test -tags=integration ./test/integration/postgres/replydraft -count=1`
  passed in 95.084s after binding reply-send and reply-test idempotency
  fingerprints to the hook destination fingerprint.
- `pnpm --ignore-workspace vitest run src/features/reply-send-hook/components/reply-send-hook-page.test.tsx`
  passed after relabeling failed/dead hook attempts as manually
  redeliverable in the Console health summary.
- `go test ./internal/handlers/console -run 'ReplySendHookDeliveriesRejectInvalidLimit|ReplySendHookRoutes|ReplyDraft'`
  passed after making malformed delivery-log `limit` query parameters return a
  precise bad-request envelope.
- `go test ./internal/service/replydraft -run 'UpsertHook|WebhookReplySenderDoesNotReuse|TestHookReturnsCached|DeliveryWorker'`
  passed after normalizing hook names and rejecting over-long or
  control-character names before storage.
- `go test -tags=integration ./test/integration/postgres/replydraft -run 'ReplySendHookDeliveryLog_RecordsTestAttemptFailure|ReplySendHookDelivery_ClaimDue|ReplySendHookTest_IdempotencyKeyReturnsAcceptedAttemptFromCache|ReplySendHook_UpsertBlankSecretPreservesExistingSecret' -count=1`
  passed after limiting background due-retry claims to `reply.send` attempts
  while keeping failed `reply.test` attempts manual-redelivery only.
- `go test -tags=integration ./test/integration/postgres/replydraft -run 'LateFailure|LateAccepted|ClaimDue|RecordsTestAttemptFailure|AcceptedAttemptReplays' -count=1`
  passed after fencing delivery completion to still-pending attempts that match
  the current approved revision and hook, and after clearing stale external
  delivery markers on edit after send failure.
- `go test ./internal/infra/database` passed after narrowing the
  reply-delivery due-retry partial index to `reply.send` attempts.
- `go test -tags=integration ./test/integration/postgres/replydraft -count=1`
  passed in 118.515s after the delivery retry state-machine changes.
- `go test -tags=integration ./test/integration/postgres/replydraft -count=1`
  passed in 113.375s after adding delivery-completion fencing for late
  accepted/failed results and edit-time external delivery marker cleanup.
- `PATH="/opt/homebrew/Cellar/node@22/22.22.3/bin:$PATH" make ci-check`
  passed after the late delivery-completion fencing changes with the final line
  `✓ ci-check passed — ready to push`.
- `lizard . -l go -C 15 -T nloc=100 --warnings_only` initially found
  `PrepareRedelivery` above the CCN gate; the final implementation splits
  redelivery loading and event-specific preparation helpers, and lizard is now
  clean.
- The local 4174 Console preview mock was updated to include reply-send-hook
  delivery list, test delivery, and failed-redelivery responses; `curl` verified
  `GET /fb/v1/console/reply-send-hook/deliveries?limit=25` returns the failed
  delivery fixture used by the browser/E2E flow.

## References

- [#164](https://github.com/Phixsura/attune/issues/164)
- [#26 proposal](../06/2026-06-13-enricher-reply-draft.md)
- [Current reply-draft migration](../../../../internal/infra/database/migrations/026_reply_draft.sql)
- [Current reply-draft repo](../../../../internal/repo/replydraft/task.go)
- [Current reply-draft service](../../../../internal/service/replydraft/drafter.go)
- [Current regenerate handler](../../../../internal/handlers/console/feedback/feedback_regenerate.go)
- [Current feedback detail UI](../../../../console/src/features/feedback/components/detail-sheet.tsx)
- [Current outbound framework](../../../../internal/outbound/outbound.go)
- [Current audit inventory](../../../../internal/handlers/console/router_audit_inventory_test.go)
