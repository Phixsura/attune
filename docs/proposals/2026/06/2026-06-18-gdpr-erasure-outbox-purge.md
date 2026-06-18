# GDPR erasure must purge notify_outbox (fix FK-violation abort + residual PII)

| | |
|---|---|
| **Issue** | #131 |
| **Status** | Proposed |
| **Started** | 2026-06-18 |
| **Related** | #126 (GDPR export/delete — origin), #33 (notify outbox dead-queue), defect audit 2026-06-18 |

## Problem

`notify_outbox.feedback_id` is `NOT NULL REFERENCES user_feedback(id)` with **no
`ON DELETE` action** (`internal/infra/database/migrations/005_notify_outbox.sql:16`).
The GDPR erasure path `deleteLockedSubject` deletes `llm_audit` then
`user_feedback` (`internal/repo/gdpr/gdpr.go:225-230`) but **never touches
`notify_outbox`**. For any subject who ever had a delivery:

1. **Erasure hard-fails** — `DELETE FROM user_feedback` raises an FK violation
   and the entire deletion transaction rolls back. This is not "incomplete
   erasure" — it is a broken, throwing delete (availability + GDPR Art.17).
2. `notify_outbox.payload JSONB` stores the feedback content verbatim
   (migration `005:8-10,21`), so the subject's PII persists in delivery
   envelopes regardless.

Other children differ: `feedback_tag_assignments` + `feedback_audit_log`
cascade on the `user_feedback` delete; `llm_audit` is deleted explicitly;
`notify_outbox` is the only child left unhandled. (Verified adversarially
against `main`.)

## Goals
- Erasure of any subject — including those with pending/delivered/dead outbox
  rows — completes without FK violation and removes their outbox payloads.
- Report the count of purged outbox rows for the deletion record.
- Regression test that locks the behavior.

## Non-goals
- General outbox **retention/pruning** of delivered rows' PII for *non-erased*
  subjects (a separate retention concern — see Risks).
- Adding `OutboxCount` to the console GDPR proto/response (additive, can follow).

## Proposal
In `deleteLockedSubject` (inside the existing erasure tx, **before** the
`user_feedback` delete), add `DELETE FROM notify_outbox WHERE feedback_id =
ANY($1)` — mirroring the existing explicit `DELETE FROM llm_audit`. Count the
rows into a new `Counts.OutboxCount`.

## Alternatives considered
- **`ALTER … ON DELETE CASCADE` on the FK (migration).** Cleaner long-term (any
  future feedback-delete path auto-cleans outbox), but (a) changes delete
  semantics repo-wide, (b) the repo's established pattern for non-cascading
  children is an explicit `DELETE` (llm_audit), and (c) a code-only fix is
  smaller and trivially reversible. Chosen the explicit delete for consistency
  + minimal blast radius; CASCADE can be revisited if more delete paths appear.

## Risks / tradeoffs
- Deleting outbox rows for an erased subject drops their delivery history — this
  is the intended effect of erasure, acceptable.
- **Residual PII for non-erased subjects** still sits in `payload` until
  delivery/cleanup — out of scope here; worth a follow-up to redact/prune
  payloads post-delivery. Flagged, not fixed.

## Verification
- New integration test `TestPG_GDPRDeletePurgesNotifyOutbox`
  (`test/integration/postgres/gdpr/gdpr_outbox_test.go`): a subject with a
  `notify_outbox` row is deleted with **no** FK error, `Counts.OutboxCount == 1`,
  and the outbox row is gone.
- Existing `TestPG_GDPRExportDeleteLifecycle` still passes (no outbox rows there
  → `OutboxCount == 0` on both export and delete, so the count-equality assertion
  holds).
- `make test-integration` (gdpr package) green.

## References
- `internal/repo/gdpr/gdpr.go:211-232`,
  `internal/infra/database/migrations/005_notify_outbox.sql:16,21`.
- Defect audit 2026-06-18 (P0-1).
