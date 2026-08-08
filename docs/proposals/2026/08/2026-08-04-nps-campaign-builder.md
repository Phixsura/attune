<!-- markdownlint-disable MD013 -->

# NPS Campaign Builder

| Field | Value |
| --- | --- |
| Issue | [#236](https://github.com/Phixsura/attune/issues/236) |
| Status | Implemented |
| Started | 2026-08-04T00:00:00+08:00 |
| Related | [#202](https://github.com/Phixsura/attune/issues/202), [#219](https://github.com/Phixsura/attune/issues/219), [#235](https://github.com/Phixsura/attune/issues/235) |

## Problem

Attune can send transactional CSAT and CES surveys after a feedback or request
event. It cannot measure a declared customer population at a deliberate point
in time. Adding `nps` as a third score range would make the resulting trend
untrustworthy: an operator could not later establish which cohort was measured,
which eligible contacts were invited, or which question and collection window
produced a result.

#236 adds one relationship-NPS vertical slice. It must reuse the existing
survey invitation, hosted response, low-score review, feedback enrichment, and
customer-request workflow rather than introduce a second signal or task model.

The Console's shared page hero must preserve its intended compact mobile rhythm.
Its desktop content-width basis cannot become a vertical flex basis on narrow
screens, where it would push NPS operational evidence below an artificial blank
region.

## Goals

- Let an operator create an NPS campaign for one existing cohort and schedule
  one immediate or future email run, then optionally continue it as a durable
  30-365 day relationship-NPS pulse. Manual scheduling remains available. A
  successor is scheduled only after the prior run closes and only once for each
  source run.
- Give each recurring pulse an explicit, frozen allocation target: 25% of the
  currently serviceable contact population by default, configurable from 1-100%.
  The target is a planning rule, not a promise to send; cooldown, consent, and
  transaction-time eligibility remain authoritative.
- Deliver a fixed 0-10 NPS question and one optional comment through the
  existing identified, hosted survey link.
- Persist an immutable run boundary, the invitations produced by that run, NPS
  buckets, and enough counts to explain the distribution and trend.
- Create existing detractor recovery work for scores 0-6, durably enqueue its
  initial owner notification, and bridge every non-empty comment to canonical
  `user_feedback` and enrichment.
- Ask an optional, response-specific NPS follow-up question and make its answer
  visible to the recovery owner without treating the answer as a substitute for
  subscription state or an organization-specific legal basis.
- Show a compact Console builder plus NPS distribution and trend cards.
- Expose run-scoped recovery outcome evidence beside each NPS measurement, so
  operators can distinguish a score that created work from a score whose
  detractor work was contacted, understood, acted on, or explicitly resolved.
- Preserve the first customer-contact and first terminal-disposition timestamps
  against each review's immutable initial target, so the operator can see
  whether the closed loop moved in time rather than merely whether its fields
  were eventually filled in.

## Non-goals

- Generic survey authoring, branching, templates, panels, or a form DSL.
- Reminders, a new fatigue policy, or a separate invitation-message ledger.
  Recurrence uses one frozen allocation rule plus the existing run and
  contact-cooldown controls; it does not create a second delivery policy.
- Browser-widget implementation, anonymous, source-link, SMS, or account-known
  delivery. The issue's optional widget entry point is a future adapter owned by
  [#219](https://github.com/Phixsura/attune/issues/219), after it can preserve
  this issue's identified-response invariants.
- Passive/promoter tasking, advocacy workflows, NPS-specific AI themes, or a
  new recovery workspace.
- A new NPS-to-customer-request relation, automatic request promotion, external
  benchmarks, statistical significance claims, or causal retention claims. The
  sample planner below is a conservative capacity estimate only; it does not
  turn a self-selected response set into a representative sample.

## Proposal

### Product boundary

The single supported journey is:

```text
draft campaign -> schedule one run -> freeze run definition
-> create consented contact-email invitations -> hosted score/comment response
-> existing detractor review (0-6) + durable initial owner notification
-> canonical feedback bridge (non-empty text)
-> enrichment -> run distribution/trend -> existing human request review
```

The campaign is relationship NPS only. Its question and bucket boundaries are
server-defined: detractor `0-6`, passive `7-8`, promoter `9-10`. A campaign may
have several manually scheduled runs over time, or may opt into a 30-365 day
pulse. A closed run schedules at most one successor, and only one run may be
nonterminal at a time. Disabling the cadence stops future successors without
altering completed measurements.

The Console's native schedule field accepts the operator's browser-local wall
time. It names that browser IANA time zone beside both schedule entry and run
history, renders each NPS run with its full calendar year, and shows the exact
normalized UTC value once a schedule is entered. The API stores RFC3339 UTC.
This leaves an auditable absolute execution time across regional operators and
daylight-saving transitions without silently reinterpreting a browser-local
entry as the tenant's configured time zone.

Run history is cursor-paginated by the immutable, campaign-local sequence in
descending order. The Console begins with the newest page and may request only
older sequences; a newly scheduled run therefore cannot shift an operator into
a duplicate or skipped historical measurement. Each page aggregates invitation
and response metrics only for its returned run IDs, keeping a long-lived
campaign's history view bounded as its ledger grows.

The authoritative audience is the active membership of one cohort joined to
`customer_notification_contacts` by tenant and `external_user_id`/`subject_key`.
The resolver requires an opted-in, unsuppressed, usable contact and excludes
tenant-update unsubscribe or suppression records. A cohort email is never used
to create or upgrade a notification contact. Schedule-time resolution is
authoritative: manual runs choose a deterministic, seeded subset up to the
configured recipient cap. A recurring run first computes
`ceil(serviceable_contacts * recurrence_sampling_percent / 100)`; the default
target is 25%, with 1-100% validation. The recipient cap remains a blast-radius
guardrail, and a small non-empty audience still receives one candidate when the
target is non-zero. The resolver then applies the existing tenant-wide contact
cooldown to prior non-suppressed survey invitations, so repeated pulses rotate
through other eligible contacts rather than repeatedly contacting the same
people. The percentage and cooldown are copied into the immutable run
definition. NPS fixes the existing `max_daily_invitations` to zero: the run cap,
rather than permanent daily-limit suppression, is its blast-radius control. The
Console and run reads show the allocation target separately from the actual
invitation ledger.

Before scheduling, the preflight returns the same aggregate audience counts
plus an exclusive, no-PII exclusion distribution: no notification contact,
contact currently unavailable, or contact cooldown. These buckets sum to the
current excluded count and give an operator an actionable correction path
without exposing a cohort member, contact, name, or email. It also carries the
frozen minimum submitted-response threshold and explicitly warns when the
current invitation estimate falls below it: even perfect completion from that
estimate could only yield a directional result. For recurring cadence it also
reports the frozen allocation percentage separately from the planned count. This
is not a response-rate forecast or a scheduling block; the scheduled worker
re-evaluates the immutable run definition and does not persist preflight
classifications as a recipient ledger.

Preflight also reports an optional sample-planning estimate over the current
serviceable population. Operators choose a 90%, 95%, or 99% planning confidence
level, a 1-25 percentage-point margin-of-error target, and an expected submitted
response rate. Attune uses the worst-case binomial variance with finite-population
correction to estimate required submitted responses, then translates that target
into invitations. Preflight separately identifies when the invitation target
is above the configured recipient cap, so operators can distinguish a cap
constraint from a small eligible population. The estimate is frozen into the run's definition parameters
and recomputed against the materialized eligible population; it is explicitly
labelled capacity planning, not an NPS confidence interval, significance test,
or representativeness guarantee. A shortfall remains an actionable warning,
not an invisible change to the run or its operating-evidence qualification. The
run evidence presents actual invitations beside the planning target and keeps
the shortfall visible after the run closes.

Every NPS campaign also carries an explicit operating-evidence threshold: a
minimum number of submitted responses and a minimum submitted-responses per
invitation rate. Defaults are 30 submitted responses and 10 percent, and the
operator may tune both for the cohort. Both values are copied into every run's
immutable definition. A closed run is marked qualified only when its current,
non-redacted evidence reaches both frozen thresholds; otherwise it remains a
directional result. A collecting run is always preliminary, and a run with
GDPR-redacted responses is explicitly incomplete. Qualification is an
operator-defined decision guardrail, not a claim of random sampling,
population representativeness, confidence intervals, or statistical
significance. The completed-response threshold may not exceed the run recipient
cap: no rate can make more completed responses than invitations. The service
rejects that impossible configuration, PostgreSQL enforces it for all writes,
and the Console makes the mismatch visible before submission. The current
preflight audience remains advisory, however; its eligible count can change
before the worker creates the immutable recipient ledger.

### Recovery outcome evidence

NPS is not complete when a detractor alert is created. The selected measurement
therefore exposes the recovery outcome ledger already attached to that run's
low-score reviews: total reviews, explicitly resolved reviews, dismissed
reviews, customer-contacted reviews, reviews with a recorded root cause, and
reviews with a recorded action. The same filter scopes every count to the
selected immutable run and its response window; it does not aggregate a later
campaign run into the measurement being interpreted.

These fields are operational evidence, not a promise that a customer was made
whole or that retention changed. A dismissal remains visible and never counts
as a resolved recovery. Similarly, an automatic escalation note is an action
record, but it cannot stand in for customer contact or an explicit resolution.
The Console presents the values as numerators over the run's review count, next
to the score and qualification policy, so a team can see whether its closed-loop
discipline is keeping pace with the signal it collects.

Each new review also freezes its initial response target. The first recorded
customer contact and the first terminal disposition are append-only timestamps
(`customer_contacted_at` and `first_terminal_at`); subsequent reassignment or
due-date changes cannot make a late event appear on time. `reviewed_at` remains
the established general closure field so legacy CSAT/CES operations keep their
existing behavior. Selected-run evidence reports on-time and late counts only
where both the immutable target and the relevant event timestamp exist.
Historical reviews without an original timestamp remain visible in ordinary
recovery counts but are excluded from the timeliness denominator rather than
backfilled from an unrelated update time. Historical reviews without an original
target retain an immutable terminal-timeliness-unknown marker, so reopening and
later editing a review cannot fabricate a first closure time. A terminal
disposition includes both an explicit
resolution and a dismissal, which keeps closure speed distinct from customer
recovery success.

### Widget interoperability boundary

The hosted survey is a response-capability document: its opaque token resolves
one invitation, which in turn establishes the selected cohort member and
profile/contact linkage. The page intentionally rejects framing and sends a
`frame-ancestors 'none'` policy. Relaxing those headers, placing an invitation
token in a browser snippet, or accepting an anonymous browser ingest as an NPS
response would create a clickjacking or capability-leak path and would sever the
run's audience and profile guarantees.

Attune's browser feedback widget is separately scoped by #219. A future
integration may present an NPS entry point only after it can (1) establish a
trusted profile/contact identity, (2) apply the same run audience and consent
rules before issuing an invitation, and (3) issue and consume the existing
per-invitation response capability. It must not use the general
`POST /v1/feedback/ingest` endpoint as an NPS response sink. Until then, the
hosted email link is the complete and intentionally top-level respondent entry
point for this campaign type.

### Data and contract changes

Add migration `129_nps_campaign_runs.sql` and evolve the generated survey
contract. These are additive; CSAT and CES semantics remain unchanged.

| Area | Change |
| --- | --- |
| Campaign | Add `nps` survey type, `scheduled_run` trigger, and `one_per_run` dedupe policy. A row-level check fixes NPS to contact email, low-score threshold `6`, and `max_daily_invitations = 0`; service validation rejects free-form question content. The server owns canonical English and Simplified-Chinese respondent copy selected from the campaign locale. Each immutable wording is identified by a content revision; a later wording change adds a new revision rather than modifying an existing one. A locale change creates a new content version, while every delivered invitation retains its original localized snapshot and wording revision. The generic Chinese tag plus explicit Simplified-Chinese script or region tags select Simplified-Chinese copy; Traditional-Chinese and all other unsupported tags use the canonical English fallback rather than accepting a metric-changing question. |
| NPS settings | Add one tenant-scoped campaign settings row with cohort ID, detractor owner, collection window, immutable sampling seed, maximum recipient count, explicit minimum submitted-response and submitted-response-rate thresholds, an optional 30-365 day recurrence interval, a 1-100% recurring-pulse allocation target (default 25%), a separate 30-3650 day per-contact recurrence cooldown (default 365 days), and conservative sample-planning inputs (95% confidence, 10 percentage-point margin of error, 20% expected submitted-response rate). The minimum completed-response threshold cannot exceed the recipient cap, so each configured operating guardrail is at least mathematically reachable. Composite foreign keys prevent cross-tenant cohort and owner references. |
| Launch preflight | Return current aggregate audience, cap, and recurring allocation evidence with mutually exclusive exclusion reasons for missing contact, unavailable contact, and contact cooldown. The planned invitation count uses the recurring allocation target when cadence is enabled, while the target is reported separately from actual materialization. Return the frozen minimum submitted-response threshold and a separate advisory indicator when the current invitation estimate cannot reach it even with perfect completion. Also return the current serviceable population, conservative required-complete estimate, expected-rate invitation target, planning assumptions, and an explicit shortfall warning. The reasons are counts only, sum to the excluded total, and remain advisory until the worker resolves the scheduled run. |
| Run | Add `survey_campaign_runs` with campaign ID, sequence, client request key, request fingerprint, scheduled/open/close timestamps, lifecycle (`scheduled`, `evaluating`, `collecting`, `closed`, `failed`, `cancelled`), immutable definition snapshot, evaluated/eligible/invitation counts, frozen recurring allocation percentage, frozen sample-planning parameters, derived materialized-population planning evidence, current-response redaction count, cancellation timestamp and actor, bounded failure reason, and optional recurrence source plus claim/processed lease state. An operator may cancel only a `scheduled` or `evaluating` run, before invitations materialize; a repeated request returns that same terminal result and a collecting run is a conflict. Run reads derive current delivery, hosted-page visits, completions, explicit hosted visit rate (`started / invitations`), submitted response rate (`completed / invitations`), page-visit conversion proxy (`completed / started`), NPS bucket counts, and frozen operating-evidence qualification from that run's invitation ledger; completed respondents remain in the started count. A hosted-page visit may be created by an email security scanner or prefetcher and is never presented as verified human participation. The legacy `response_rate` remains an exact deprecated compatibility alias of `hosted_visit_rate`. Email-provider opens are delivery telemetry, not a hosted-page visit. The Console uses submitted-response evidence for NPS measurement and labels visit telemetry separately. A unique `(tenant_id, campaign_id, client_request_key)` returns the same run for a retry; a partial unique index permits one nonterminal run per campaign, and a unique source-run link makes recurrence idempotent. |
| Evidence export | Add a tenant-scoped CSV download for one NPS run. It contains a versioned, aggregate-only row with the immutable measurement fingerprint, denominator chain, rates, qualification thresholds, sample-planning evidence, every 0-10 score bucket, an explicit `nps_available` value, and the count of response-quality flags so no response or privacy-redacted denominator is interpreted as numeric NPS zero. It excludes comments, emails, contacts, and respondent identifiers. Creation uses an explicit POST with a required client request key: a retry replays the original immutable artifact with `200 OK` and no duplicate creation audit event, while a new artifact returns `201 Created`. Each generated report is persisted with creator, timestamp, SHA-256, and a 30-day retention deadline, then exposed through a bounded history list and an exact-byte re-download path; creation and download are audited. Expired downloads return `410 Gone`. A survey worker removes expired artifact rows in bounded, tenant-labelled batches while retaining the audit events. PostgreSQL rejects artifact/digest mismatches, and repository reads verify stored bytes before download. New reports use schema version 3; versions 1 and 2 remain replayable. |
| Invitations | Add tenant-scoped run linkage, bound by a composite foreign key to the same campaign. Existing invitations stay valid with a null run ID; invitations created for a run remain the recipient ledger. |
| Responses | Add an immutable `survey_type` snapshot and NPS bucket. A row-level score check enforces CSAT `1-5`, CES `1-7`, and NPS `0-10`; the bucket is required only for NPS. Migration backfills existing response types from campaigns before making the field non-null. New NPS responses also persist an immutable, response-level follow-up answer; CSAT/CES rows cannot carry one and older NPS responses remain explicitly unknown. The Console distinguishes that legacy unknown state from an explicit refusal. Public request metadata persists only versioned, tenant-scoped, domain-separated HMAC pseudonyms derived from the managed Tink primary key; the tenant is resolved from the opaque invitation token rather than public input, and a primary-key rotation begins a new correlation boundary. Public submissions also persist a versioned quality observation with bounded reason codes for automated-client indicators, missing request context, direct submission without a hosted visit, and unusually fast completion. These observations are additive evidence only: they do not remove a response or alter score denominators, and the Console exposes the count and per-response status. |
| Feedback bridge | Add `survey_response_feedback_links`, unique by response and linked feedback ID. A bridge stores IDs only, never copied comment, email, or request data, and is written in the same transaction as its response. |
| Feedback vocabulary | Reserve `survey` as a core feedback source and use `nps` as the feedback type. Metadata identifies the campaign, run, invitation, response, score, bucket, and contact. |
| Observability | Export `attune_survey_nps_run_materialization_total` and `attune_survey_nps_recurrence_total` with tenant, bounded result, and bounded reason labels for materialized, terminally failed, retrying, superseded, scheduled, and skipped worker decisions. Surface both in the Operations dashboard; raw errors remain in logs and the run record, never metric labels. |

Extend `proto/attune/v1/survey.proto` with typed NPS settings and run messages,
`ScheduleNpsCampaignRun`, `CancelNpsCampaignRun`, and `ListNpsCampaignRuns`, a run filter for analytics,
and NPS value/bucket fields in aggregate and trend responses. Scheduling requires
a Console-generated UUID request key. Repeating a key with the same fingerprint
returns the original run with `200 OK` and no second scheduling audit event; the
initial request is the only `201 Created` and audit-producing operation. A
different fingerprint is a conflict. Continue using
the existing recipient-preview, public response, low-score-review, and analytics
operations. Run `make proto`; generated Go, TypeScript, and OpenAPI output are
part of the change.

### Service behavior

`internal/service/survey` schedules a run under a campaign-level lock, then
claims due NPS runs using `SKIP LOCKED`, one run at a time, so a large earlier
materialization cannot consume a later run's lease while it waits in the same
worker batch. An evaluating claim expires after five minutes, so another worker
can resume after a process crash; materialization and failure writes are fenced
to the current claim owner. The worker resolves the
frozen audience before materialization. In its recipient-ledger transaction it
locks each selected contact in a stable order and creates one invitation only
when that contact remains opted in, unsuppressed, unbounced, uncompromised by a
complaint, and subscribed to tenant updates. Before inserting the ledger, the
transaction rechecks the frozen cooldown against all committed non-suppressed
invitations. Tenant-level unsubscribe writes take the same contact lock before
changing preferences, so an unsubscribe and an invitation materialization have
a clear ordering. Standard contact-addressed campaign writes take the same
lock, so an NPS cooldown and eligibility decision is made at a contact
serialization point rather than from a stale audience read. Each standard
campaign retains its own configured cooldown policy after acquiring that lock
and records `contact_not_eligible` when that final recheck skips a recipient.
It then hands delivery to the existing invitation worker and its provider-event
model. The run becomes collecting only after invitation
materialization; invitation expiry and the collection close time share the same
run window. `failed` is reserved for materialization failure before commit.
Provider failures remain invitation-level delivery facts and do not cause the
worker to create a second run. Scheduling refuses an unavailable email sender
or delivery secret store before a run is persisted.

The same survey worker also enforces export storage retention. It deletes only
NPS evidence rows whose 30-day deadline has passed, takes a bounded batch with
`SKIP LOCKED`, and reports removals by tenant through a low-cardinality outcome
metric. The audit log is intentionally independent of the artifact row, so
governance reviewers retain proof that an export existed and was downloaded
even after the aggregate bytes have been removed. A missed cleanup tick is
therefore recoverable without blocking the response or campaign workers.

When recurrence is enabled, the same worker first claims closed source runs
whose cadence is due. A claim has a bounded lease and uses `SKIP LOCKED`, so a
crashed process can be replaced without an in-memory timer or duplicate pulse.
The scheduler re-reads the current campaign, settings, and delivery readiness;
archived campaigns, non-NPS campaigns, and disabled cadence are marked
processed without creating a successor, while a temporary delivery or database
failure leaves the claim retryable. The successor is created with a deterministic
request key, the source-run foreign key, and the existing one-nonterminal-run
constraint. Only after the schedule commit succeeds is the source marked
processed. The child starts at `source.closes_at + interval`, clamped to now if
the worker is late, and its own close window and immutable measurement
definition are evaluated independently.

Recurrence also freezes a separate per-contact cooldown, defaulting to 365 days,
so a quarterly program can keep a steady measurement flow without inviting the
same contact every quarter. It also freezes the configured allocation target.
The audience resolver calculates `ceil(serviceable_contacts * target / 100)`,
limits it by the run cap, orders candidates with the immutable seed and run ID,
and lets the contact cooldown remove prior recipients. This produces a stable
flow across pulses without claiming statistical representativeness; each run
still exposes eligible, planned, and actual invitation counts separately.

Every recurrence decision emits a bounded result/reason metric. `scheduled` is
split into `created` and `existing_successor`; terminal skips identify missing
or inactive campaigns, non-NPS campaigns, and disabled cadence; retryable paths
use `delivery_not_ready`, `not_found`, `validation`, `conflict`, or `transient`.
This gives operators a stable alert and dashboard vocabulary without leaking
database or provider error text into metric labels.

Public response quality follows the same evidence-first principle. The HTML and
JSON submission paths share one evaluator after tenant-scoped request
fingerprinting. It records only a version, `observed`/`flagged` status, and
bounded reason codes. It can identify known automation markers in the supplied
user-agent, missing request context, a direct API submission before a hosted
visit, and a sub-three-second completion after a hosted page visit. Raw
user-agent and address values remain outside the response record. Flagged
responses stay in the default analytics and NPS denominator; quality is shown
as a separate operational signal so automated filtering cannot silently change
the measurement. Legacy responses without the metadata are treated as
`observed` for compatibility.

The cohort and its source must both remain enabled. The Console omits disabled
cohorts from new NPS configuration, scheduling locks and validates the selected
cohort/source before persisting a new run, and materialization repeats that
locked check before any invitation insert. A disablement that commits first
therefore prevents recipient-ledger creation and records the bounded
`cohort_unavailable` failure. `stale_ttl_days` is deliberately not reused as a
source-freshness SLA: in cohort sync it owns departed-membership retention, not
the freshness semantics of a successful provider read.

An exact request-key replay resolves the previously persisted run before
evaluating the current campaign state, so archival or delivery-configuration
changes cannot turn a successful scheduling request into a different retry
result. A new run carries the Campaign revision used to build its definition;
the scheduling transaction locks that Campaign row and compares the revision
before insert. An overlapping Campaign update therefore either follows the run
commit or causes the stale scheduler to retry, never producing a definition
assembled from two configuration versions.

An operator can cancel only a `scheduled` or claimed `evaluating` run. The
cancellation transaction locks the run row, marks it `cancelled`, clears the
worker lease, and records the actor and time. It is idempotent for an already
cancelled run. The materializer takes the same run lock before writing an
invitation ledger, so cancellation and materialization linearize: a cancellation
that commits first fences the worker; a materialization that commits first makes
cancellation a conflict. Attune does not present cancellation as revocation for
a collecting run because its invitation capabilities may already be delivered.

The response path adds an explicit unit-of-work boundary; it must not compose
the current independently committing `CreateResponse` and `InsertTx` methods.
The survey repository gains `WithTx(ctx, func(pgx.Tx) error)` and
transaction-scoped response, invitation, review, and bridge methods.
`internal/service/survey` coordinates those methods through that callback and
receives a narrow injected feedback writer whose implementation delegates to
`FeedbackRepo.InsertIdempotentTx` using the same transaction. For a non-empty
comment it uses the response ID as the server-generated idempotency key and a
stable request fingerprint. The review seed persists the campaign's validated
owner; NPS always creates it for scores 0-6. For every NPS detractor, that same
transaction also creates a pending owner notification with reason
`nps_detractor_response`; its notification payload has the distinct
`survey.recovery_opened` event type. A valid submission must never depend on a
synchronous email call: the existing durable notification worker sends, retries,
and records its delivery after commit. If the configured owner is no longer
deliverable, the response still succeeds and the system records a
skipped-notification metric; correcting the owner is an operational task, not a
reason to discard customer feedback. Only then may the transaction commit.

The NPS hosted page asks one optional follow-up question. An omitted JSON value
or an unchecked HTML control persists as an explicit `false` for newly created
NPS responses, preventing absence from being mistaken for permission; only
pre-existing rows remain `NULL`/unknown. The recovery queue and its durable
owner-notification payload expose that value beside the low-score evidence.
It is deliberately neither a mutation of `customer_notification_contacts` nor
a hard block on the existing `customer_contacted` workflow: tenants may have
different service, contractual, or legal policies for contact. The field gives
the responsible operator a response-specific customer preference rather than
claiming to decide that policy for them.

Each contact-email invitation owns one encrypted tenant-unsubscribe URL. The
worker, a hosted-page refresh, and a delivery retry reuse that URL rather than
creating a new token; after its 90-day lifetime, an atomic compare-and-swap
rotates it with its replacement. This keeps customer controls stable while
preventing refreshes, preview clients, and retries from growing the token ledger.

Response expiry is authoritative at the invitation transaction's database-clock
boundary. If a previously admitted response wins the invitation lock while a
page concurrently tries to mark that invitation expired, the expiry write
re-reads the terminal row and returns the immutable completed receipt. The
respondent never sees a false missing-link result for feedback that committed.
The same transaction rechecks invitation suppression, so a provider complaint
or other recipient revocation that commits first cannot admit a response from a
page that performed its initial link check just beforehand.

The hosted-read path revalidates the same lifecycle immediately after recording
a respondent start. An archive, expiry, or suppression that overlaps that
transition returns its terminal public result instead of rendering a page whose
subsequent submission must be rejected.

The response transaction locks its invitation and reads the PostgreSQL wall
clock after that lock is acquired. It rejects a completed or expired invitation,
or atomically marks a deadline-crossed invitation expired before any response,
feedback, review, or notification can be written. This closes the interval
between public-token resolution and persistence, so the NPS collection cutoff
is an enforced data boundary rather than a best-effort application check. The
same query takes a shared lock on the campaign and requires it to remain active,
so archival is also linearized against response persistence rather than merely
checked during earlier token resolution.

Provider email opens and hosted-page visits remain separate facts. Generic
survey analytics counts an email open from its durable `opened_at` evidence
even after a hosted visit moves the invitation into `started`; it separately
reports hosted-page visits and the visit-to-submission conversion proxy
(`completed / started`). A page visit may be scanner or prefetch activity, so
submitted response rate is the only run result denominator. This preserves
delivery and page telemetry without presenting either as respondent participation.

The durable feedback row starts in the existing pending-enrichment state, so
the installed enrichment worker owns downstream candidate generation. A retry
cannot create a second response, review, feedback row, or bridge. The existing
feedback-to-customer-request promotion and linking APIs remain the only request
path: a survey comment is evidence for a human decision, not an automatically
created request.

GDPR deletion resolves the notification-contact identity even when an NPS
response has no comment and therefore no feedback row. It removes the response,
whose foreign-key cascade removes its bridge when one exists, and then removes
feedback for the affected subject. It does not remove a
campaign-wide run. A run's definition and materialization counts remain
immutable; score distribution and NPS are calculated from remaining responses.
The deletion transaction increments the run's non-identifying
redacted-response count, and the Console labels the current response denominator
and redaction count separately. It never presents materialization counts as the
NPS denominator after an erasure. A date-filtered aggregate selects redaction
evidence by each run's measurement start (`opened_at`), rather than retaining a
deleted respondent's submission timestamp; a run selection remains the most
precise historical comparison boundary. Materialization locks the selected
contact, verifies its unchanged identity, and then locks its active cohort
membership before inserting an invitation. This prevents a claimed worker with
a stale audience preview from recreating survey data after an erasure removes
that membership. Immediate and scheduled erasure use the same contact-then-
membership lock order.
Legacy feedback records with unbackfilled subject identity columns continue to
resolve through the existing source-user fallback, so extending contact-linked
NPS identity does not narrow prior GDPR access or erasure rights.

### Console behavior

Extend the existing `console/src/features/surveys` surface rather than create a
second survey application. The NPS flow has five states:

1. Draft: name, server-fixed NPS question, fixed contact-email channel, cohort,
   window, cap, and detractor owner. Generic source-link and event-recipient
   controls are not offered for an NPS campaign.
2. Schedule and runs: immediate or future scheduling, lifecycle, and evaluated,
   eligible, and invitation counts. Before scheduling, a privacy-safe launch
   preflight shows the current cohort-member, eligible-contact, excluded, and
   capped invitation counts alongside delivery readiness, then breaks excluded
   members into missing contact, unavailable contact, and cooldown counts. It
   never returns contact identifiers, names, or email addresses. The preflight
   is explicitly a current-state estimate: the worker resolves the immutable
   run audience at its scheduled time and remains the authoritative send
   boundary.
3. Analytics: NPS, completed-response denominator, 0-10 distribution, bucket
   counts, and any privacy-redacted response count. The NPS trend has one point
   per immutable run, with hosted-page visits / invited and the
   `completed / started` page-visit conversion proxy; selecting a run scopes aggregate metrics to
   that measurement boundary. An opaque, versioned measurement fingerprint
  covers the immutable NPS question, canonical content revision, cohort, run
  cap, sample seed, collection window, contact cooldown, and recurring
  allocation target. Version 4 adds the content-revision and allocation
  boundaries; legacy
   definitions without that field produce no comparable key. The Console
   connects only adjacent runs with the same fingerprint and labels every
   changed definition as a new baseline; it never implies comparison across
   changed measurement definitions.
4. Response detail: existing detractor-review workflow, profile-linked feedback
   candidates, and the response-specific NPS follow-up answer. When a comment
   has a durable bridge, the recovery card opens that exact canonical feedback
   signal in the existing Feedback workbench, where the normal customer-request
   review flow remains available. The recovery card distinguishes explicit
   permission, explicit refusal, and legacy answers for which this preference
   was never recorded.

The Console's NPS distribution, funnel, and top-level score metrics are scoped
to exactly one immutable run. With no manual selection, it chooses the newest
closed run; a collecting run is used only when no closed measurement exists and
is labelled as preliminary. The historical trend contains closed runs only.
Operational detractor recovery remains campaign-scoped because it is a queue of
individual customer commitments, not a comparative measurement.

The run selector, run cards, and measurement trend start from the newest
bounded history page. An explicit control loads older immutable pages by
sequence, so operators can inspect the full retained measurement history
without asking the service to recompute every run's invitation and response
metrics on each refresh.

Every selected NPS measurement also exposes the frozen run's eligible audience,
invitations, submitted responses, recipient coverage, submission coverage,
collection window, recipient cap, recurring allocation percentage, and contact
cooldown beside its score. A
changed cooldown changes the eligibility frame and therefore starts a new trend
baseline. This is an evidence disclosure rather than a claim of statistical
significance: email respondents may self-select, and Attune does not infer a
whole-customer-population score or emit confidence intervals without an
explicitly representative sampling model. Operators can therefore see the
actual denominator chain and frozen selection rules before acting on a striking
distribution or trend.

The selected run also offers an aggregate-only CSV evidence download for offline
review and retention. The file is versioned and contains the immutable
measurement fingerprint, denominator chain, qualification policy,
sample-planning evidence, and the complete 0-10 distribution in one row. It
never exports comments, email addresses, contacts, or respondent identifiers,
and the download is recorded as a tenant-scoped audit event. The response also
publishes a standard `Digest: sha-256=...` body digest and a strong `ETag` based
on the same bytes; the audit event retains the hex digest for offline chain of
custody checks.

No Console control is added for a widget, passive/promoter action, or
NPS-specific request linking. A future NPS widget is an integration with
#219's publishable-key and identity contract, not an iframe variant of the
hosted survey.

## Alternatives Considered

| Alternative | Decision | Reason |
| --- | --- | --- |
| Add only `nps` to the current survey type enum | Rejected | It does not preserve a population or execution boundary for a relationship measurement, or make score `0` database-safe. |
| Store schedule and audience in generic campaign JSON | Rejected | Typed, immutable run and settings data is necessary for validation and historical explanation. |
| Build a widget before the durable email/hosted-link loop | Rejected | A browser widget requires #219's publishable-key and identity contract, while recurrence can reuse the established run, invitation, and cooldown invariants. |
| Create a survey-specific request or task object | Rejected | `user_feedback`, low-score reviews, and reviewed request linking already own the needed semantics. |
| Derive trends from mutable campaign timestamps | Rejected | A run is the measurement boundary; campaign edits must not rewrite history. |
| Connect every historical NPS run in one line | Rejected | A question, cohort, sample, collection-window, contact-cooldown, or recurring allocation edit changes the measurement definition; an uninterrupted line would falsely imply comparability. |
| Aggregate all NPS runs into the default distribution or funnel | Rejected | It would recreate the same invalid comparison outside the trend chart. The default measurement scope is one finalized run; live collection remains clearly provisional. |
| Silently lower the completed-response threshold when an operator reduces the recipient cap | Rejected | It would turn a deliberate evidence policy into an invisible weaker policy. The operator must choose a reachable threshold, while the preflight continues to disclose the current, non-binding audience estimate. |
| Show an NPS score without its eligible, invited, and submitted denominators | Rejected | A compact score can overstate a small or self-selected response set. The Console shows the run's denominator chain but does not claim population-level statistical significance. |
| Keep only a recipient cap for recurring programs | Rejected | A cap bounds blast radius but cannot express a stable quarterly or annual flow. A frozen percentage of currently serviceable contacts, followed by cooldown and contact-lock checks, makes the allocation intent explicit without pretending to be a representative sample. |
| Treat tenant-update subscription as permission to contact a respondent about this NPS answer | Rejected | Delivery subscription and response-specific follow-up preference answer different questions. The hosted NPS page captures the latter without modifying the former. |
| Hard-block recovery owners from recording customer contact when the NPS follow-up answer is false | Rejected | The answer informs the recovery decision but cannot encode every tenant's service, contractual, or legal policy. A hard block would claim authority the survey feature does not have. |

## Risks / Tradeoffs

| Risk | Mitigation |
| --- | --- |
| Cohort membership lacks a consented contact | Resolve only through notification contacts; show exclusion counts; never trust cohort email. |
| Incorrect cohort creates an excessive send | Require cap; schedule-time resolution selects a deterministic subset before invitation creation; preserve run failure evidence. |
| An operator disables a cohort or its source after an NPS configuration or future run exists | Omit disabled cohorts from new Console selection, then lock and recheck cohort/source enablement at both schedule and materialization boundaries. A disablement that commits first produces `cohort_unavailable` and no invitation ledger. |
| Repeated relationship-NPS runs fatigue the same customer | Apply the existing contact-level invitation cooldown across all non-suppressed survey invitations; default NPS to 90 days, constrain it to 30-365 days, freeze it per run, index both the invitation ledger lookup and the cohort-to-contact `(tenant_id, subject_key)` join, and lock/recheck the contact at recipient-ledger write time. |
| A recurring pulse slowly under-delivers because prior recipients are in cooldown | Compute the target from the current serviceable population before cooldown, then apply cooldown to the selected audience. Report the allocation target, eligible count, and actual invitation count separately; never inflate the actual ledger to satisfy a planning target. |
| An operator launches without understanding the response volume or reachable audience | Return an aggregate, no-PII NPS launch preflight before scheduling; show the cap, delivery blocker, and mutually exclusive missing-contact, unavailable-contact, and cooldown counts while labelling its numbers as a current-state estimate rather than a persisted recipient ledger. |
| Sender or secret-store readiness changes after a future run is scheduled | Recheck delivery readiness immediately before materialization. A missing configuration fails the run without creating a recipient ledger; transient database, encryption, or entropy failures retain the evaluating lease for a bounded automatic retry. |
| Cohort eligibility or contact consent changes between scheduling and materialization | Recheck consent, suppression, bounce, complaint, tenant-update unsubscribe, and cooldown rules after locking each selected contact. `evaluated_count` and `eligible_count` preserve the worker's immutable audience-resolution evidence, while `invitation_count` is the final contact-locked recipient ledger; their difference exposes late eligibility drift without overstating delivery. If no contact remains, end the run with a stable `no_eligible_recipients` failure reason, retain the audience evidence, create no invitations, and release the campaign for a corrected re-run rather than holding an empty collection window open. |
| A recipient withdraws, bounces, complains, or is manually suppressed after a delivery worker has already claimed an invitation | Every contact-suppression writer revokes queued or claimed contact-email invitations in its own transaction and clears their delivery secrets and leases. Each revocation makes its invitation update depend on the contact update, preserving the worker's contact-before-invitation lock order. The worker locks and rechecks that same contact before it creates customer controls and again immediately before provider handoff; a revocation that commits before this final fence wins, while a stale worker cannot mark the revoked invitation delivered. |
| A provider repeats an event, reuses the same fallback payload across distinct invitations, or delivers any stale outcome after a terminal delivery event | Store each provider event with a tenant-scoped idempotency key. An exact conflict commits no new projection side effects, while a fallback key binds the payload to its invitation or provider-message locator so different deliveries retain independent history. Only allow `temporarily_delayed` to move an invitation among pending, accepted, and delayed states, and prevent every later provider event, including another terminal outcome, from overwriting terminal suppression, timestamps, or failure evidence. Late events remain available in event history without changing the invitation read model. |
| An operator archives a campaign while a worker is materializing its run | Lock and recheck the campaign inside the invitation transaction before locking the run. The worker and archive operation serialize in a consistent order; an archive that commits first produces `campaign_not_active` with no recipient ledger. |
| An operator cancels while a worker is materializing | Lock the same run row for cancellation and materialization. Exactly one transaction can win: a committed cancellation fences the worker before any invitation insert, while a committed materialization makes cancellation an explicit conflict instead of a false success. |
| An operator edits or archives a campaign while a recipient still holds its link | Validate the current campaign status as the immediate revocation control, lock and recheck it with the response write, but render and score from the invitation's persisted definition so content, locale, score semantics, and low-score routing do not change after delivery. |
| A locale change quietly changes a respondent's metric or mixes languages in one measurement | Keep NPS wording server-canonical for each supported locale, create a new content version whenever the locale changes, render all hosted-page controls from that frozen locale-aware definition, and normalize both page metadata and stored response locale to the language tag of the shipped content. Only English and Simplified-Chinese wording ship today; Traditional-Chinese tags explicitly fall back to English instead of labeling Simplified copy as Traditional Chinese. On every public read, rebuild NPS copy from the snapshot's immutable canonical-content revision rather than trusting legacy or malformed text. An unknown explicit revision disables the link instead of silently rendering a different question after a partial deployment or rollback. |
| Duplicate public submission creates duplicate feedback | Enforce invitation response uniqueness and response-feedback bridge uniqueness in the same transaction. |
| A submission crosses the collection deadline while waiting on another write | Lock and recheck the invitation with the database wall clock in the response transaction; atomically expire the invitation before rejecting the submission. |
| A public survey's pseudonymous network fingerprint changes with each source port, trusts a forged forwarding header, or can be enumerated from a leaked low-entropy hash | Resolve the client address through the same configured trusted-proxy model as API-key policy, then create versioned, domain-separated HMAC pseudonyms for the normalized address and user agent at both JSON and hosted-form boundaries. Derive the HMAC subkey from the managed Tink primary key, never persist raw metadata, and let primary-key rotation begin a new correlation boundary. |
| A detractor response is committed but its owner is not alerted until the SLA has already expired | Create the initial owner notification in the same transaction as the response and low-score review; reserve later recovery automation for escalation. |
| A recovery owner mistakes generic invitation consent for permission to contact a respondent about a specific NPS answer | Ask and persist an optional response-level follow-up answer, display explicit permission, refusal, or legacy unknown state in the recovery queue, and include known answers in the durable owner-notification payload. Keep it distinct from the notification-contact subscription and do not present it as a legal-basis decision. |
| Repeated schedule request sends the cohort twice | Persist a request key and fingerprint; return the existing run for a matching retry and reject a mismatched reuse. |
| A worker exits after claiming a run | Reclaim evaluating runs after a bounded lease; owner-fenced writes prevent an old worker from mutating the reclaimed run. |
| Campaign daily limit silently suppresses a run | NPS fixes `max_daily_invitations` to zero; reject any NPS configuration that changes it. |
| NPS is read as causal or representative | Always show completed-response count and distribution; make no benchmark, significance, or causal claim. |
| GDPR changes a historical NPS result | Keep only non-identifying materialization counts immutable; recompute score metrics from remaining responses and show the redaction count. Lock the subject's invitation ledger before counting and deleting, so an in-flight response either commits before redaction accounting or fails after erasure. |
| Widget pressure weakens respondent security | Keep hosted survey framing forbidden; require #219's trusted identity, consent, and per-invitation capability contract before adding a widget adapter. |
| Scope expands beyond the estimate | Keep recurrence limited to fixed relationship-NPS cadence and the existing invitation/run controls; do not add widget implementation, workflow variants, or new request/task models to this issue. |

## Delivery Decision

The original issue estimates 2-3 days. That is credible only for a happy-path
score form. It is not credible for the issue's stated profile linkage, feedback
signal, trustworthy trend, and the transaction, idempotency, and deletion
invariants above. This proposal therefore requires a fresh estimate after a
short implementation spike. The expected implementation is 4-6 engineering
days plus CI remediation; reducing it to 2-3 days requires an explicit issue
scope change, not a silent weakening of data integrity.

## Implementation Plan

1. **Contract and storage:** Add NPS/run proto types and fields, request-key
   and cancellation semantics, generated output, migrations 129-139, database checks/indexes,
   repository models/queries, feedback source vocabulary, and migration tests.
2. **Atomic backend loop:** Add the service-owned transaction boundary and
transaction-scoped survey methods; implement cohort resolution, run claim,
invitation/run linkage, forced NPS detractor review owner, comment bridge,
pending-enrichment handoff, transactional initial detractor notification, and
GDPR/redaction behavior.
3. **Console and release proof:** Add the builder, schedule/run list,
   distribution/trend/redaction counts, cadence and recurring-allocation controls, focused Console tests,
   and the changelog entry.
4. **Recovery outcome evidence:** Extend the existing analytics contract and
   selected NPS measurement with run-scoped review, resolution, contact,
   root-cause, and action-record evidence. Preserve dismissed work as a distinct
   outcome and verify every count is constrained to the selected run.
5. **Recurring pulse reliability:** Add durable cadence state, the source-run
   idempotency constraint, lease claim/recovery behavior, and regression coverage
   for success, duplicate recovery, disabled cadence, archived campaigns, and
   temporary delivery failure.

The three steps are review boundaries, not a requirement for three pull
requests. A single coherent pull request is acceptable if the generated
contract, migration, backend loop, and Console tests remain reviewable.

## Verification

### Issue #236 acceptance evidence

| Acceptance criterion | Implementation evidence | Executable evidence |
| --- | --- | --- |
| Operators can create and launch an NPS campaign | The Console creates the server-defined NPS campaign, exposes a no-PII preflight, schedules an idempotent run, and permits cancellation before materialization. | `console/e2e/accessibility/surveys.spec.ts` covers creation, preflight, scheduling, and cancellation. `TestPGNPSConsoleRoutesCreatePreflightScheduleListAndCancel` drives the real Console router with a signed session, RBAC, PostgreSQL persistence, and audit rows; `TestPGNPSCampaignRunMaterializesCohortAndBridgesComment` proves the materialization path. |
| Responses are stored and linked to customer profiles | Each hosted response is bound to one invitation and its consented notification contact; the response transaction locks and revalidates that identity boundary. | `TestPGNPSPublicSurveyAPIReportsNPSContractType`, `TestPGNPSPublicResponseWaitsForConcurrentInvitationSuppression`, and `TestPGNPSHostedSurveyPagePersistsProfileLinkedFeedback` cover the public contract, rendered hosted form, response persistence, and response-reload races. |
| NPS comments can become feedback signals | A non-empty comment creates one canonical `user_feedback` record and a unique response-feedback bridge in the response transaction; an operator may explicitly promote that signal into a customer request. | `TestPGNPSCampaignRunMaterializesCohortAndBridgesComment` verifies the durable bridge, pending enrichment queue handoff, and claimable enrichment input; `TestPGNPSHostedSurveyPagePersistsProfileLinkedFeedback` proves the hosted form retains that feedback ID when re-read by invitation; `TestPGNPSDetractorResponseRollsBackWhenInitialNotificationCannotPersist` proves no partial response, feedback, or notification state survives an atomic failure. `make public-board-smoke` follows the compiled Console promotion flow and checks the resulting request, evidence link, and immutable audit event in PostgreSQL. |
| Dashboard shows score distribution and trend | Analytics is selected by immutable run, preserves denominator and qualification evidence, exposes explicit NPS availability, and exposes NPS distribution, trend, and recovery outcomes in the existing Survey Console. | `TestPGNPSRunMetricsStayBoundedToEachMeasurementRun`, `TestPGNPSAnalyticsRedactionsRespectRunMeasurementWindow`, Console component tests, and the NPS browser accessibility scenario cover the storage, aggregate, and rendered surfaces. |

`make public-board-smoke` also creates a separate 390px authenticated Console
session for the completed NPS campaign. It verifies the selected measurement,
the complete score distribution, no horizontal overflow, and the compact shared
page-hero geometry so desktop width constraints cannot become mobile height
reservations.

The issue's widget entry point is optional. It remains deliberately outside this
change because #219 owns the trusted identity and publishable-capability
contract required to preserve the run and profile-linkage guarantees above.

- Race regression: `go test -race ./internal/infra/database ./internal/repo/survey ./internal/service/survey ./internal/handlers/console/survey` and `go test -race -tags integration ./test/integration/postgres/survey -run '^TestPGNPS' -count=1` cover the changed backend packages and the NPS PostgreSQL concurrency scenarios.

- Unit tests: NPS score/bucket boundaries, NPS-only zero, forced threshold `6`,
  fixed content, collection-window validation, stable schedule fingerprints,
  run-scoped aggregate filtering, conservative finite-population sample
  planning, planning-input validation, and initial notification decisions.
- Repository and handler tests: NPS campaign parsing, schedule/list endpoints,
  run filter binding, feedback source vocabulary, and GDPR redaction update
  sequencing.
- PostgreSQL integration: reclaim a stale evaluation lease and reject a stale
  worker's fenced failure or materialization write after another worker takes
  the run; persist exactly one initial NPS detractor notification atomically
  with the response.
  Persist an aggregate NPS evidence artifact, list it without loading the CSV
  body, and prove that a subsequent download returns the exact stored bytes.
  The aggregate report also carries explicit `nps_available` state in schema
  version 2, while the handler regression preserves replay of version 1
  artifacts.
- Provider-event regression: preserve a delivered invitation's terminal state,
  timestamps, and clean failure evidence when a delayed event arrives late;
  preserve a complaint's suppression and failure evidence when an opened event
  arrives out of order; replay an accepted event after a newer delay without
  changing the projection; and retain two independent event-history rows when
  separate invitations use the same fallback payload without a provider event ID.
  A distinct complaint after a bounce is retained as event history while the
  invitation preserves its original bounce evidence.
- Service regression: claim each due run immediately before its materialization,
  preserving the worker invocation's total process limit without pre-consuming
  later leases.
- Recurring-pulse regression: close a source run, process its due cadence, and
  verify exactly one successor carries the source-run ID and deterministic
  request key. Re-run the worker, reclaim an expired recurrence lease, disable
  cadence, archive the campaign, and remove delivery readiness; each case must
  either remain retryable or produce one auditable skip without a duplicate
  successor. Assert the recurrence Prometheus counter records the corresponding
  bounded result/reason for created, existing, skipped, and retryable paths, and
  assert the successor freezes the configured per-contact recurrence cooldown
  and recurring sampling percentage. With a multi-contact cohort, verify the
  target is computed from serviceable contacts before cooldown, the prior
  recipient is excluded, and repeated reads of one run return the same seeded
  candidate set.
- PostgreSQL scheduling regression: while the first run is blocked inside its
  materialization transaction, a later due run remains `scheduled`; releasing
  the lock then materializes both runs within the same invocation limit.
- Cancellation race regression: cancel a scheduled run twice and verify only
  the first write changes it, no invitation exists, and a replacement can be
  scheduled. Race cancellation with a claimed worker and verify the resulting
  state is either cancelled with no invitations or collecting with a cancellation
  conflict, never a successful cancellation after an invitation commit.
- Tenant-boundary regression: an unrelated tenant and a mismatched campaign ID
  both receive `not_found` for schedule, preflight, and cancellation attempts;
  no foreign run or invitation is observable or changed. Replayed cancellation
  writes no second audit event.
- Request-key regression: a retry with the original schedule returns its
  original run, while reuse of that key with a different schedule is a conflict
  and leaves the persisted schedule unchanged. The original replay remains
  available after archival, while a stale Campaign revision is rejected inside
  the scheduling transaction before any run can persist.
- Observability regression: a terminal no-recipient materialization increments
  the tenant-scoped NPS counter under the stable `failed` /
  `no_eligible_recipients` labels; metrics registration and dashboard coverage
  reject missing or malformed contract updates.
- Public-contract regression: an NPS hosted-survey API response reports
  `SURVEY_TYPE_NPS` with its `0-10` range, so API clients cannot downgrade the
  response to an unspecified survey type.
- Public-input regression: HTML and JSON submission surfaces share a 64 KiB
  request cap, and an overlong comment fails validation before any NPS
  response, feedback bridge, review, or recovery notification can persist.
- Public response-quality regression: HTML and JSON submissions produce the
  same versioned status and bounded reason codes, known automation markers and
  sub-three-second hosted completions are flagged, raw request metadata is not
  persisted, flagged responses remain in the score denominator, and analytics,
  trend, Console response records, and version-3 evidence exports expose the
  flag count without reclassifying legacy rows.
- Follow-up-preference regression: NPS HTML and JSON submissions persist an
  explicit true or false answer, CSAT/CES rejects the field, legacy NPS rows
  remain visibly unknown rather than being presented as a refusal, and a
  detractor's owner notification plus Console recovery card expose known
  answers without mutating notification subscription.
- Expiry-completion race regression: a completed invitation causes an expiry
  write to return the typed not-found result; the public NPS read and retry
  paths re-read the terminal invitation and preserve its completed receipt.
- Suppression race regression: hold a provider-style invitation suppression
  update while an NPS submission reaches its response lock, commit the
  suppression, and verify the submission returns disabled with no response.
- Hosted-read lifecycle regression: after recording a hosted start, revalidate
  expiry, suppression, and campaign archival before rendering the response
  page. An isolated PostgreSQL trigger archives an NPS campaign from the real
  `started` update; the public JSON API must return `403/FORBIDDEN` and leave
  no response behind. A second trigger archives from the invitation
  unsubscribe-link persistence update, proving the final lifecycle read also
  rejects a stale page.
- GDPR concurrency regression: hold a real in-flight NPS response transaction,
  verify the erasure blocks on its invitation lock, then commit the response and
  verify the deletion removes it while incrementing only that run's redaction
  count. Separate immediate and scheduled-delete regressions verify the same
  export and erasure rights for a contact-linked NPS score with no comment and
  therefore no feedback row. A stale-audience regression claims a run, resolves
  its audience, executes GDPR erasure, and proves materialization cannot
  recreate the erased subject's invitation.
- Redaction-presentation regression: when a selected NPS measurement retains
  only GDPR redaction evidence and no current responses, Console presents NPS
  as unavailable rather than the valid but misleading numeric value `0`.
- Scheduled-run regression: disable the verified sender after scheduling and
  verify that the run records an actionable delivery failure without creating an
  invitation; transient materialization errors remain evaluating for lease-based
  retry rather than becoming irreversible failures.
- Empty-audience regression: suppress every eligible contact after scheduling
  and verify a terminal run with persisted audience evidence, no invitation,
  localized Console guidance, and immediate ability to schedule a corrected
  replacement run.
- Cohort-availability regression: disable a selected cohort or its source after
  scheduling and verify the worker records `cohort_unavailable` without an
  invitation; disablement before scheduling rejects the new run at the locked
  scheduling boundary. Hold an uncommitted cohort disablement while the worker
  reaches materialization, then commit it and verify the shared-lock recheck
  resumes as `cohort_unavailable` with no recipient ledger.
- Archive-race regression: hold an uncommitted archive update while a worker
  reaches its final materialization transaction, then commit the archive and
  verify a `campaign_not_active` run with no invitations.
- Funnel regression: a provider-opened invitation that later enters a hosted
  survey retains the email-open signal while generic analytics and campaign
  health report separate hosted-page visit and page-visit conversion metrics.
- Recovery-outcome regression: analytics for one NPS run reports its review,
  resolved, dismissed, customer-contacted, root-cause-recorded, and
  action-recorded counts without including a matching campaign's later run; the
  Console renders every numerator against that run's review denominator and
  never counts a dismissal as resolved.
- Recovery-timeliness regression: a new review retains its original target
  through a later due-date change, records the first customer-contact and first
  terminal timestamps exactly once, and reports on-time versus late selected-run
  evidence without treating a dismissal as a resolved customer recovery or
  treating timestamp-less historical work as timely.
- Invitation-definition regression: edit a live campaign after delivery and
  verify its existing public link still renders, thanks, localizes, and routes
  low-score follow-up from the persisted invitation definition, while a current
  draft or archived status still disables an incomplete link.
- Locale-version regression: create a Simplified-Chinese NPS campaign, verify
  that the immutable question and every hosted response-page control are
  Chinese, change its locale to English, and verify that the canonical content
  version advances without rewriting the already-delivered Chinese invitation.
- Locale-script regression: verify generic Chinese plus `zh-Hans`, `zh-CN`,
  and `zh-SG` select the shipped Simplified-Chinese content, while
  `zh-Hant`, `zh-TW`, and `zh-HK` produce the canonical English page
  language, wording, and persisted response locale until Traditional-Chinese
  wording is shipped.
- Legacy-snapshot regression: overwrite a structurally valid NPS invitation
  snapshot with an unsupported locale and arbitrary question, then verify the
  public API, hosted page, thank-you response, and persisted response locale
  still use the shipped canonical English definition.
- Wording-revision regression: assert every new NPS run and invitation snapshot
  records its canonical wording revision; a legacy snapshot without that field
  may be inferred only from an exact shipped definition, while an unknown
  explicit revision disables the link rather than replacing its question.
- Measurement-boundary regression: assert the version-4 fingerprint includes
  the canonical content revision and recurring allocation target, changes when
  either changes, and is
  empty for a legacy definition that has no revision, so old and new wording
  cannot share a trend baseline.
- Contact-cooldown regression: materialize an NPS run, age its invitation just
  beyond a later lowered campaign cooldown but not the scheduled run's 90-day
  definition, then verify that scheduled run terminates with no eligible
  recipient rather than silently re-contacting the customer.
- Contact-cooldown concurrency regression: pre-resolve two NPS runs for the
  same contact and materialize them in parallel; then race an NPS ledger write
  with a regular contact-addressed campaign configured with a cooldown. In
  either ordering, exactly one non-suppressed invitation may survive and the
  standard campaign records a `contact_cooldown` suppression when its write
  loses. If an NPS run loses its final contact-lock recheck for every planned
  recipient, it never enters `collecting` with an empty ledger; the worker
  records its `no_eligible_recipients` terminal result and frees the campaign
  for a corrected re-run.
- Recipient-consent regression: resolve an NPS audience, then consume that
  contact's tenant-level unsubscribe token before materialization. The locked
  recipient recheck creates no invitation and returns the terminal
  `no_eligible_recipients` path rather than delivering to a withdrawn contact.
- Delivery-revocation regression: claim a contact-email invitation, then
  consume that contact's tenant-level unsubscribe token. The transaction clears
  its delivery secret and lease; the stale worker's final preparation returns
  non-deliverable, its owner-fenced delivery write is rejected, and a later
  worker performs no provider call.
- Contact-suppression regression: repeat the claimed-invitation race through
  manual contact suppression, notification-layer email-hash bounce and complaint
  handling, and survey-provider bounce and complaint events. Each source clears
  pending delivery state; each provider event also revokes a second queued
  invitation for the same contact with its own forensic reason code.
- Measurement-qualification regression: freeze a campaign's minimum submitted
  response and submitted-response-rate thresholds into a run, then verify that
  a collecting run is preliminary, a closed run below either threshold is
  directional, a run meeting both is qualified, and GDPR redaction overrides a
  qualified-looking count with an incomplete-evidence state. Confirm the
  completed-response rate is `completed / invitations`, distinct from hosted
  page-visit telemetry and the page-visit conversion proxy.
- Console tests: NPS analytics/distribution metrics and generated contract
  fixtures remain compatible with the existing reliability checks; the launch
  preflight renders aggregate audience, exclusive no-PII exclusion evidence,
  and delivery-readiness evidence without exposing a recipient; the run measurement trend renders the NPS and sample
  denominator plus hosted-page visit and conversion-proxy rates from each immutable run
  rather than response-day buckets, and breaks at an opaque immutable-definition
  fingerprint change instead of joining incomparable runs. A response with a
  feedback bridge presents a direct, ID-scoped Feedback workbench link, while
  responses without a bridge present no dead-end control.
- Evidence-export tests: the selected run exposes a download link whose CSV
  contains only the versioned aggregate evidence schema, includes all 0-10
  score buckets, rejects cross-campaign run lookup, and records the download in
  the audit log without exporting respondent fields. Service and repository
  coverage proves the persisted artifact is immutable and re-downloadable;
  the migrated PostgreSQL audit-action constraint accepts both create and
  download events; the Console history query preserves campaign/run scope; and
  the browser smoke compares the historical HTTP response byte-for-byte with
  the actual downloaded file while checking its digest and strong ETag.
  PostgreSQL integration also proves that a direct artifact mutation is
  rejected by the content-addressing trigger. The POST contract is tested for
  required request-key handling, `201` creation versus `200` replay, single
  creation audit emission, retention metadata, and `410` rejection after
  expiry; the PostgreSQL cleanup path then removes the expired artifact while
  leaving both audit events intact. The Console API and component suites verify
  CSRF, download bytes, query invalidation, and the disabled expired-history
  state.
- Production-shape browser regression: start the compiled Attune binary with a
  temporary PostgreSQL cluster and production Console bundle; create and verify
  the encrypted sender through the Console, schedule then cancel a future NPS
  run and prove it leaves no invitation while writing exactly one cancellation
  audit event, then schedule an NPS campaign against a consented encrypted
  contact plus one cohort member without a notification contact, observe the
  missing-contact preflight count, receive the worker's HTTP email delivery, submit a desktop and mobile
  hosted detractor response, then verify the feedback bridge, recovery delivery,
  and rendered NPS run trend in the Console.
- Required commands: `make proto`, `go vet ./...`, `go build ./...`,
  `go test -race ./...`, `make test-integration`, Console type-check, Biome,
  Vitest, `pnpm arch`, and `make ci-check`.

Regression coverage also verifies that an unscored hosted NPS form renders no
selected radio while explicit `score=0` remains selectable; campaign-plus-run
trend queries execute against PostgreSQL; the schedule audit action is accepted
by the migrated constraint; and populated cohort/source tables render inside a
Tooltip provider.

The Console disables NPS scheduling while a run is scheduled, evaluating, or
collecting; a component regression test covers this guard while database
uniqueness remains the authoritative concurrency boundary. PostgreSQL coverage
also records opposing NPS scores in two runs on the same day and proves their
per-run scores and response rates remain distinct after the first recipient
leaves the configured cooldown. It also follows one
invitation from a hosted-survey visit to submission and proves the hosted-visit audience remains
stable while the page-visit and conversion metrics advance.

Completed verification on 2026-08-05: `make proto`, focused Go and Console
tests, and the full `make test-integration` suite passed. The production-shape
`make public-board-smoke` journey reported success after observing the
missing-contact preflight evidence, scheduling/cancelling/running the campaign,
submitting a hosted response, and verifying the feedback, recovery, and trend
surfaces. The Codex in-app browser then confirmed the production Console renders
the aggregate exclusion evidence without horizontal overflow or browser
diagnostics. A final clean `make ci-check` passed its Go, Console, architecture,
and secret-scan gates.

Final verification on 2026-08-05 passed `make test-integration` and `make
ci-check`, including the frozen NPS contact-cooldown regression. A PostgreSQL
regression also creates a public link, edits the live campaign, and proves the
existing link retains its original definition and low-score handling while a
later draft status revokes a separate link. Browser E2E verified that the
aggregate, no-PII preflight blocks an
unavailable delivery path, becomes ready after local sender configuration,
schedules and completes a consented-contact run, and renders its NPS trend,
response funnel, feedback bridge, and recovery workflow. The same browser smoke
proves the bridged canonical feedback is left in the durable `pending`
enrichment state, so the existing enrichment runner can claim it without a
transient handoff. PostgreSQL regressions separately verify that a zero-eligible
run terminates without invitations and preserves its audience evidence.

The final PostgreSQL concurrency regression pre-resolved two NPS runs for the
same contact and materialized them in parallel, then raced an NPS ledger write
with a 30-day contact-cooled CSAT trigger. It proved that only one
non-suppressed invitation survives while the losing standard campaign records
the auditable `contact_cooldown` suppression.

The 2026-08-05 browser regression used the Codex in-app browser against a
fresh compiled local stack. It logged in, selected NPS from the Console builder,
observed the NPS contact-cooldown control with its default value of `90`, and
changed that control to `120` without browser-console errors. The disposable
demo tenant intentionally had no eligible cohort, so the builder correctly kept
the create action disabled. `make public-board-smoke` then passed against its
own temporary PostgreSQL cluster and production bundle, covering the full
configured-cohort creation, future-run cancellation with zero invitations and
one cancellation audit event, a replacement scheduled run, hosted response,
feedback bridge, recovery, and trend path with the `90`-day default assertion.

The production-bundle Console browser matrix now runs the NPS operator flow in
Chromium desktop/mobile plus Firefox and WebKit. It verifies the fixed question,
the cohort and detractor-owner creation gate, the default contact cooldown,
launch preflight, exact local-time-to-RFC3339 scheduling payload, cancellation
confirmation, released scheduling guard, generated request key, no viewport
overflow, zero axe findings, and zero browser diagnostics. This contract-level
matrix uses deterministic Console API routes; `make public-board-smoke` remains
the separate real-service evidence for delivery, response, persistence, and
feedback enrichment.

Latest verification on 2026-08-06 passed `make proto`, the focused repository,
service, and Console handler suites, the Survey Console qualification component
suite, and the complete `make test-integration` PostgreSQL tier. The latter ran
all packages serially against one shared pgvector container; the Survey package
completed in 107.593 seconds after every preceding package passed. The new
coverage verifies frozen threshold defaults and validation, distinct submitted
response rate, all six measurement-readiness outcomes, and preservation of the
NPS comparison fingerprint when only operating guardrails change.

Browser verification on 2026-08-06 passed the Chromium Console flow for NPS
creation, launch preflight, scheduling, and cancellation with the `30` and
`10` defaults asserted in both the visible controls and the outbound contract.
It also exercises a preflight response without an exclusion distribution, which
must render as an empty detail rather than fail the Console. A fresh
`make public-board-smoke` production-stack run then passed the complete NPS
journey and verified the closed one-response measurement reports the frozen
`1 / 30` and `100% / 10%` evidence as a directional result. The same browser
regression reports no viewport overflow, axe violations, or Console diagnostics.

Configuration-integrity verification on 2026-08-06 first set the recipient cap
to `29` in both the deterministic Chromium flow and the compiled Console
production smoke. Both surfaces disabled submission and exposed the accessible
validation message while the default `30` completed-response threshold was
unreachable. Restoring the cap to `30` allowed the real-service smoke to finish
its sender setup, preflight, cancellation, delivery, hosted response, feedback
bridge, recovery, and directional-result checks. A PostgreSQL regression also
attempted to write `minimum_completed_responses = maximum_run_recipients + 1`
directly and confirmed the database constraint rejected it. The full
`make test-integration` suite passed, with the Survey package completing in
87.790 seconds. The final `make ci-check` passed with 204 Console test files,
1945 assertions, no dependency-cruiser violations, and no TruffleHog findings.

Metric-semantics verification on 2026-08-07 made hosted-page visit telemetry
explicit in the public NPS run contract. `response_rate` remains a deprecated,
wire-compatible alias of `hosted_visit_rate`; submitted response rate remains
`completed / invitations`. The Console now labels hosted-page visits and the
visit-to-submission conversion proxy separately, including the fact that email
security scanners and prefetchers can load a hosted link. A browser fixture
deliberately returned `response_rate = 40%` and `hosted_visit_rate = 45%`; the
desktop and mobile Chromium matrix asserted that the rendered run shows `45%`.
`make proto`, targeted race tests, the full `make ci-check` (204 Vitest files,
1953 tests), and the full `make test-integration` PostgreSQL matrix passed. The
Survey integration package completed in 109.700 seconds after all preceding
integration packages passed in the same fresh pgvector container.

Network-provenance verification on 2026-08-07 aligned JSON and hosted-form
survey submission metadata with the process-wide trusted-proxy policy. Both
paths hash a normalized client address, not a `RemoteAddr` value containing an
ephemeral source port; direct connections ignore an untrusted forwarding header.
PostgreSQL regressions verify the stored hashes for both a standard response and
an NPS hosted response without persisting a raw address.

Current regression evidence on 2026-08-07 passed focused portal and network
hardening race tests, the full NPS PostgreSQL suite, and the complete
`make test-integration` matrix; the Survey package completed in 106.795 seconds.
`make public-board-smoke` passed against a temporary production binary, production
Console bundle, PostgreSQL cluster, and Chromium session. Its NPS measurement
assertions require the distinct hosted-page visit and visit-to-submission labels.
The final `make ci-check` passed with 204 Console test files, 1953 tests, no
dependency-cruiser violations, and no TruffleHog findings.

Keyed-pseudonym verification on 2026-08-07 established a new stored metadata
format, `hmac-sha256:v1`. The secret-store regression proves stable output for
the same input, separation across metadata purposes, and no raw address in the
value. Survey service and PostgreSQL regressions prove that identical metadata
uses different pseudonyms for different invitation tenants. Service and portal
regressions fail closed without a keyed pseudonymizer. PostgreSQL tests verify
the JSON and hosted NPS submissions store
only versioned HMAC values, while the production browser smoke verifies the
actual Tink-backed server writes both values after Chromium submits the form and
that the same browser metadata receives different values for two invitation
tenants.
The keyset regression proves that promoting a fresh primary key changes the
pseudonym for the same value, preserving the documented correlation boundary.
The refreshed complete `make test-integration` matrix passed with the Survey
package completing in 109.571 seconds. The final `make ci-check` passed with 204
Console test files, 1953 tests, no dependency-cruiser violations, and no
TruffleHog findings.

Console-control-plane verification on 2026-08-07 added
`TestPGNPSConsoleRoutesCreatePreflightScheduleListAndCancel`. It creates a
server-shaped NPS campaign through the real Console router, rejects a
non-privileged member, verifies the signed administrator session and RBAC path,
reads a no-PII preflight, schedules an idempotent request-key replay, lists and
cancels the run, and confirms each state-changing action has exactly one audit
row. The focused race run passed three times. Chromium desktop and mobile then
passed all six NPS accessibility/browser scenarios. The complete
`make test-integration` matrix passed with the Survey package in 116.080
seconds, and `make ci-check` passed with 204 Console test files, 1953 tests,
no dependency-cruiser violations, and no TruffleHog findings.

Production dashboard verification on 2026-08-07 strengthened
`make public-board-smoke` so the compiled Console must expose the semantic NPS
score-distribution list after a real hosted submission. The smoke submits score
`0`, then verifies the rendered distribution has exactly one bucket containing
that score with count `1`, alongside the existing NPS trend, immutable
measurement evidence, feedback bridge, and recovery assertions.

The same production journey now also proves the closed-loop owner action rather
than only its notification: the administrator records a root cause and recovery
action, marks the customer contacted, and resolves the generated detractor
review through the compiled Console. PostgreSQL must preserve the immutable
first-contact and first-terminal timestamps with those facts, and the selected
run must render each recovery outcome plus both on-time measurements as `1 / 1`.

When a selected NPS measurement has retained responses, its Console distribution
renders the complete fixed 0-10 response scale, including zero-count buckets.
Detractor, passive, and promoter bars use distinct semantic colors while the
numeric, run-scoped server aggregation remains sparse and unchanged. A
measurement with no retained response remains an explicit empty state rather
than a synthetic all-zero distribution.

The refreshed production smoke submits a real score `0`, then requires eleven
rendered score buckets: the first must show score `0` with count `1` and the
last score `10` with count `0`. Component regression separately proves that
privacy redaction retains the explicit empty state instead of manufacturing an
all-zero scale; desktop and mobile accessibility journeys cover the same
rendered Console surface.

The production journey also opens the bridged feedback in the command center,
selects it for owner review, follows the Console promotion route, submits a
customer request, and verifies the persisted request title and description,
feedback-evidence link, and `customer_request.promote_feedback` audit event.
This keeps the customer-request candidate flow explicitly human-controlled
while making its persistence and traceability regression-proof. Once the
dialog closes, the Console removes the one-time promotion parameter while
retaining the remaining deep-link context, so a page refresh cannot create a
second accidental promotion attempt. The route waits for session permissions
before opening that form; view-only operators have the parameter cleared
without being presented a write action the server will reject.

A PostgreSQL service-level regression also attempts to promote a foreign
tenant's feedback. It must return the same not-found boundary as a missing
feedback row, create neither a customer request nor an audit entry, and leave
the failed idempotency key retryable rather than pending.

A separate lock-coordinated PostgreSQL regression holds a failed idempotency
row while two compatible retry calls reach the recovery transition. Exactly one
caller may acquire execution; after the lock is released, the other must observe
the key as pending. An expired-key regression also proves that a delayed recovery
cannot remove a fresh retry key. This prevents a retry storm from creating
duplicate customer-request promotions.

Scheduling-time verification on 2026-08-07 now makes the Console's local-time
contract executable. The production-stack smoke enters a browser-local future
time, confirms that the native control describes its active IANA time zone and
that the visible preview contains the exact UTC-normalized RFC3339 value, then
clears the field and requires the preview to disappear. It also requires the
new run history to retain the schedule's full calendar year. Its separate 390px
Console session requires the time-zone notice to remain visible without
horizontal overflow. A Codex in-app-browser pass independently confirmed the
rendered `Asia/Singapore` notice, its accessibility relationship to the native
control, and desktop containment against the compiled Console.

Acceptance is met when an operator can launch the journey above without SQL or
hidden APIs; each response is profile-linked; every non-empty comment becomes a
traceable feedback signal; and the Console shows an NPS distribution and trend
with their completed-response denominator.

## References

- [Issue #236](https://github.com/Phixsura/attune/issues/236)
- [Embeddable in-app feedback widget issue #219](https://github.com/Phixsura/attune/issues/219)
- [NIST SP 800-63C: pairwise pseudonymous identifiers](https://pages.nist.gov/800-63-3/sp800-63c.html)
- [RFC 2104: HMAC](https://www.rfc-editor.org/rfc/rfc2104.html)
- [Post-resolution CSAT and CES proposal](../07/2026-07-29-post-resolution-csat-ces-surveys.md)
- [Customer Signal OS proposal](../07/2026-07-30-customer-signal-os.md)
- [Google Research: questionnaire biases across sample providers](https://research.google/pubs/a-comparison-of-questionnaire-biases-across-sample-providers/)
- [AAPOR: data-quality metrics for online samples](https://aapor.org/reports/data-quality-metrics-for-online-samples-considerations-for-study-design-analysis/)
- [AAPOR: Standard Definitions, 10th edition](https://aapor.org/wp-content/uploads/2024/03/Standards-Definitions-10th-edition.pdf)
- [Bain: the Net Promoter System outer loop](https://media.bain.com/Images/BAIN_BRIEF_Loyalty_Insights_The_Net_Promoter_System%27s_outer_loop.pdf)
- [Qualtrics: action workflows for a closed-loop NPS program](https://www.qualtrics.com/articles/customer-experience/servicenow-action-workflows-culture/)
- [Qualtrics XM Institute: defining closed-loop response and closure metrics](https://www.qualtrics.com/articles/customer-experience/how-create-closed-loop-program/)
- [Qualtrics: measurable CX action plans](https://www.qualtrics.com/support/vocalize/dashboard-management-cx/creating-action-plans-cx/)
- [Qualtrics: response export and download governance](https://www.qualtrics.com/support/survey-platform/data-and-analysis-module/data/download-data/export-data-overview/)
- [Qualtrics: fraud detection for bot and duplicate responses](https://www.qualtrics.com/support/survey-platform/survey-module/survey-checker/fraud-detection/?parent=p0094)
- [Qualtrics: response quality review signals](https://www.qualtrics.com/support/survey-platform/survey-module/survey-checker/response-quality/)
- [Qualtrics: relationship surveys and recurring measurement cadence](https://www.qualtrics.com/support/customer-experience-features/customer-experience-dashboards/relationship-surveys/)
- [Medallia: closed-loop feedback framework](https://www.medallia.com/blog/closed-loop-feedback-program-try-this-framework/)
- [Qualtrics: NPS questions and follow-up permission](https://www.qualtrics.com/articles/customer-experience/nps-questions-examples-and-template/)
- [Medallia: service recovery](https://docs.medallia.com/en/agent-connect/surveys/service-recovery)
- [Qualtrics: distribution summary](https://www.qualtrics.com/support/survey-platform/distributions-module/distribution-summary/)
- [Qualtrics: manage previous response exports](https://www.qualtrics.com/support/survey-platform/data-and-analysis-module/data/download-data/export-data-overview/)
- [Qualtrics: retention policies](https://www.qualtrics.com/support/survey-platform/sp-administration/data-privacy-tab/data-retention/)
- [Qualtrics: margin of error guide](https://www.qualtrics.com/articles/strategy-research/margin-of-error/)
- [Qualtrics: panel-size and response-rate planning](https://www.qualtrics.com/uk/experience-management/research/manage-your-panel/)
- [Medallia: sample size and required invitations](https://docs.medallia.com/en/medallia-agile-research/distribution/distribution/optimal-sample-size)
- [Delighted: project sampling for steady feedback](https://help.delighted.com/article/587-getting-the-Most-out-of-your-delighted)
- [Delighted: Autopilot distribution and adaptive sampling](https://help.delighted.com/article/631-module-4)
- [HubSpot: customer feedback survey cadence and reminders](https://knowledge.hubspot.com/customer-feedback/customer-feedback-faqs?c=1040)
- [SurveyMonkey: sample-size calculator and methodology](https://www.surveymonkey.com/learn/research-and-analysis/sample-size-calculator/)
- [SurveyMonkey: NPS survey question guide](https://www.surveymonkey.com/learn/customer-feedback/nps-survey-question-guide/)
- [Formbricks survey schema](https://github.com/formbricks/formbricks/blob/2cf309c8f05beca41dfd390ad592fc25ec619c5d/packages/database/schema/main.prisma)
- [PostHog survey iteration migration](https://github.com/PostHog/posthog/blob/c45ba4e3750a6ee35512c540ea94da518a5feeb2/posthog/migrations/0424_survey_current_iteration_and_more.py)
