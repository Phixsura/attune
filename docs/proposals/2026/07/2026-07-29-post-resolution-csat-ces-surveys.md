<!-- markdownlint-disable MD013 -->

# Post-resolution CSAT and CES Surveys

| Field | Value |
|---|---|
| Issue | [#235](https://github.com/Phixsura/attune/issues/235) |
| Status | Implemented |
| Started | 2026-07-29T23:57:01+08:00 |
| Related | [#202](https://github.com/Phixsura/attune/issues/202), [#224](https://github.com/Phixsura/attune/issues/224), [#212](https://github.com/Phixsura/attune/issues/212), [Reply Draft Review Send Workflow](./2026-07-03-reply-draft-review-send-workflow.md), [Close the Loop Request Notifications](./2026-07-16-close-the-loop-request-notifications.md), [Feedback Workflow Status](../06/2026-06-14-feedback-workflow-status.md), [Customer Requests](./2026-07-07-customer-requests.md) |

## Problem

Attune can ingest feedback, enrich it, route it through operator workflow
states, generate reviewed reply drafts, send controlled replies through a hook,
promote feedback into Customer Requests, publish public-safe request surfaces,
and notify request subscribers when product work changes state.

It still cannot measure whether a resolved support or product interaction felt
good to the customer.

Issue #235 asks Attune to add CSAT and CES campaigns after resolution. This is
not a simple email template. The feature touches workflow transitions, reply
send completion, customer request context, public hosted links, email outbound,
consent, unsubscribe, anti-spam controls, analytics, and low-score inspection.

The current foundations are useful but incomplete:

- `tenant_workflow_states.category = 'closed'` identifies terminal feedback
  states, but no post-transition measurement event is recorded.
- Reply drafts can reach `sent` after a reviewed send hook accepts the request,
  but no post-reply survey is scheduled.
- Customer Requests collect demand and public close-the-loop notifications, but
  request outcomes are not measured with CSAT or CES.
- `customer_notification_contacts`, request notification email senders,
  unsubscribe tokens, and request notification rate limits provide a consent and
  delivery precedent, but they are scoped to request updates rather than survey
  measurement.
- There is no public survey response endpoint, no invitation token model, no
  response table, and no Console trend or low-score drilldown.

The product risk is two-sided. If Attune sends surveys too loosely, customers
receive noisy, repetitive, or non-consensual messages. If it sends too narrowly,
operators get incomplete measurement and cannot see which resolved interactions
still caused effort or dissatisfaction. The design must make measurement
trustworthy by default.

## Industry Benchmark

The benchmark covers 50 products across support desks, enterprise voice-of-
customer suites, product-led survey platforms, roadmap feedback tools, and
contact-center platforms. The strongest common lesson is that leading systems
treat CSAT and CES as a measurement workflow, not as a notification side effect.

### Product matrix

| Product | Observed pattern | Design signal for Attune |
|---|---|---|
| Zendesk | Sends satisfaction requests from solved-ticket automation and allows trigger customization. | Model solved or closed workflow transitions as configurable trigger events. |
| Intercom | Sends CSAT when a conversation closes and filters unsuitable conversations. | Trigger events must pass an eligibility step before invitation creation. |
| Freshdesk | Sends CSAT from resolved or closed tickets with expiry and frequency controls. | Store invitation TTL, contact cooldown, and suppression reasons. |
| Help Scout | Embeds one-click satisfaction ratings in customer email conversations. | Keep the first customer action extremely short. |
| Front | Surfaces conversation satisfaction in analytics. | Analytics must stay tied to the support context, not only tenant totals. |
| Gorgias | Reports CSAT score, response rate, and comment detail for support interactions. | Persist a full invitation and response ledger. |
| Kustomer | Uses conversation lifecycle and channel context for satisfaction measurement. | Preserve source, channel, and context snapshots with each response. |
| Salesforce Service Cloud | Links case, contact, and survey invitation through a service workflow. | Survey responses must link to feedback or request anchors and contact context. |
| ServiceNow CSM | Ties customer satisfaction surveys to case state and portal flows. | Hosted survey links belong in the public portal trust boundary. |
| Jira Service Management | Sends CSAT after a request is resolved. | Request-resolution semantics should be available as campaign triggers. |
| HubSpot Service Hub | Offers customer support surveys after ticket closure and prevents repeated sends per ticket. | Use dedupe keys per campaign and source object. |
| Zoho Desk | Measures happiness across ticket interactions and closure. | Leave room for reply-level and closure-level measurement under one model. |
| Freshservice | Uses conditional CSAT rules for selected tickets. | Campaign triggers need filters, not hard-coded state names. |
| HappyFox | Separates automation that sends CSAT from automation that records responses. | Sending and response intake must be independently durable. |
| Kayako | Allows satisfaction surveys to be enabled or disabled by workflow state. | Survey eligibility should support per-state enablement. |
| Gladly | Aggregates satisfaction signals from support and partner survey flows. | Keep external provider response imports possible through the same response model. |
| monday service | Provides a packaged CSAT workflow for service boards. | Ship a default campaign template so operators do not start from a blank state. |
| Qualtrics | Combines workflows, directory contacts, transactional data, distributions, opt-out, and actions. | Separate campaign, contact, trigger event, invitation, and response tables. |
| Medallia | Routes low scores into closed-loop case management. | Low-score drilldown should be operational, not only a chart. |
| SurveyMonkey | Provides CSAT templates and aggregate analysis. | Fixed CSAT and CES templates are enough for this issue. |
| Typeform | Uses conditional comment prompts for poor scores. | Prompt for comments more strongly when the score is low. |
| Alchemer | Treats surveys as workflow steps with distribution and response actions. | Survey campaign execution should be worker-driven and retryable. |
| QuestionPro | Combines multi-channel distribution, real-time analytics, and response action workflows. | Response events should feed metrics and low-score workflows. |
| Delighted | Normalizes NPS, CSAT, CES, and transactional survey campaigns. | Model `metric_type` instead of creating CSAT-only fields. |
| AskNicely | Routes real-time low scores to frontline teams. | Store owner and routing context for low-score inspection. |
| Retently | Distinguishes transactional campaigns from recurring campaigns. | This issue should implement transactional post-resolution campaigns. |
| Customer Thermometer | Uses email-embedded rating links and comment capture. | Email buttons may prefill a score, but GET requests must not submit responses. |
| Nicereply | Sends CSAT, CES, and NPS through helpdesk-triggered flows. | One survey engine can support CSAT and CES without a full form builder. |
| Simplesat | Integrates with Intercom, Zendesk, and other support systems and writes responses back. | Responses should expose source object IDs for exports and integrations. |
| Survicate | Combines website, product, email, and link surveys with alerts. | The channel model should not assume email forever. |
| PostHog | Uses event and property targeting, hosted survey links, dashboards, and response analysis. | Campaign filters should use feedback, request, source, tag, and context fields. |
| Pendo | Uses segment targeting, recurrence, in-app/email surfaces, and response exports. | Contact cooldown and recurrence controls are first-class settings. |
| Sprig | Uses sampling, recontact periods, targeting, diagnostics, and bot detection. | Store sampling decisions and avoid counting scanners as responses. |
| Hotjar | Uses user attributes for targeting and analysis. | Snapshot context at invitation time and response time. |
| Formbricks | Provides open-source link, website, app, and product surveys. | Attune can keep hosted links simple and self-contained. |
| Userpilot | Configures survey audience, localization, frequency, and analytics. | Campaigns need locale and frequency fields. |
| Appcues | Controls NPS audience and display frequency by user qualification. | Eligibility belongs in one service, not in UI code. |
| Chameleon | Uses microsurveys for short product feedback collection. | One rating question plus one comment question is a strong starting surface. |
| Userflow | Uses in-app surveys and NPS with journey context. | The schema should preserve trigger context even when channel support expands. |
| Customer.io | Uses workflow logic to branch on survey or NPS responses. | Response-created events should support automation and alerts. |
| Canny | Sends status change emails to voters. | Request-related survey audience should come from related people, not a manual list only. |
| Productboard | Sends portal card updates to voters, requesters, and insight submitters. | Customer Request context should determine survey audience when present. |
| UserVoice | Notifies supporters when idea status changes. | Request subscribers are a valid survey audience if consent allows it. |
| Aha! Ideas | Handles portal email subscriptions, status updates, comments, and unsubscribe. | Survey emails need tenant and campaign unsubscribe links. |
| Jira Product Discovery | Links insights to ideas and delivery context. | Survey responses should become evidence for the request they measure. |
| Linear Customer Requests | Links customers and requests to product work. | Measurement should attach to the request and source feedback together. |
| Genesys Cloud | Supports post-interaction customer surveys across channels. | Survey triggers should be interaction-event based, not page-based only. |
| NICE CXone | Supports post-contact survey programs and CSAT-style questions. | Keep scale metadata explicit instead of assuming a single score range. |
| Talkdesk | Captures post-call CSAT and open comments. | Comments are part of the response contract, not an optional side table. |
| Aircall | Sends post-call surveys through SMS or email with trigger rules. | Delivery attempts should be channel-neutral even when this issue enables email and hosted links. |

### Cross-product patterns

1. **Triggering and delivery are separate.** A status change or reply send
   creates a candidate event. Campaign eligibility, consent, cooldown, sampling,
   and dedupe decide whether an invitation exists.
2. **Transactional surveys dominate support workflows.** The relevant unit is a
   resolved ticket, closed conversation, sent reply, support interaction, or
   shipped request, not a periodic marketing campaign.
3. **Survey scope is short.** The strongest post-resolution flows use one score
   question and one comment prompt.
4. **Context is saved with the response.** Mature systems keep case, contact,
   channel, agent, account, segment, request, and timestamp context so trends
   can be filtered after the source object changes.
5. **Anti-spam controls are visible product behavior.** Expiry, frequency caps,
   opt-out, suppression, and dedupe are table-backed and explainable.
6. **Email link scanners are expected.** A link click cannot equal a submitted
   response; the user must explicitly submit on the hosted page.
7. **Low scores drive work.** Medallia-style closed-loop programs treat low
   scores as work needing review, ownership, and evidence.
8. **CSAT, CES, and NPS share an engine.** This issue covers CSAT and CES, but
   the data model should use a metric abstraction so it does not need a rewrite
   for adjacent measurement.
9. **Hosted links are their own distribution mode.** Link surveys can be copied,
   embedded, or routed through external systems even when Attune has no verified
   email contact.
10. **Distribution, response, and suppression states are separate.** Mature
   survey tools explain whether an invitation was targeted, sent, opened,
   submitted, expired, or suppressed without collapsing those dimensions into
   one overloaded status.
11. **Campaign health is a diagnostics funnel.** Operators need to see triggered
   population, matched population, eligible population, suppression reasons,
   sent count, opened count, and submitted count.

## Goals

- Add tenant-scoped CSAT and CES survey campaigns.
- Trigger campaigns from configured feedback workflow transitions, especially
  closed-category states.
- Trigger campaigns from reply-send completion when a reviewed reply is
  accepted by the configured send hook.
- Allow Customer Request status changes and shipped request context to be used
  as survey anchors when a request is linked.
- Send invitations by email when a verified customer notification sender and
  eligible contact exist.
- Generate first-class public hosted survey links that can be copied, embedded
  in email, or surfaced from customer-facing flows even when email delivery is
  not available.
- Let operators generate a hosted survey link explicitly for a feedback or
  Customer Request anchor when a campaign allows source-linked invitations.
- Store score, comment, submitted timestamp, response latency, recipient
  snapshot, trigger snapshot, campaign version, question snapshot, operator
  snapshot, and linked feedback/request context.
- Support CSAT and CES scale configuration without building an arbitrary form
  builder.
- Enforce unsubscribe, suppression, cooldown, recent-activity gates, tenant send
  limit, contact send limit, campaign dedupe policy, source-object dedupe,
  sampling, and invitation expiry before sending.
- Revalidate delayed sends at delivery time so reopened, deleted, archived,
  auto-closed, stale, or newly suppressed source objects do not receive old
  invitations.
- Record explainable suppression reasons.
- Add Console campaign setup, survey trend reporting, response-rate reporting,
  campaign-health diagnostics, and low-score drilldown.
- Add campaign preview and test-send tooling for admins.
- Record email provider bounce, complaint, delivery, and rejection events when
  the configured sender supports them.
- Add audit records for campaign mutations, manual invitation retry, response
  suppression, and low-score review state changes.
- Keep raw email addresses, tokens, and provider secrets out of logs and audit
  metadata.
- Keep the implementation compatible with proto-generated Go, TypeScript, and
  OpenAPI contracts.

## Non-goals

- Do not build a generic survey form builder.
- Do not add NPS in this issue.
- Do not add multi-question branching beyond one rating question and one
  comment prompt.
- Do not add SMS, WhatsApp, IVR, in-app widget, or native Zendesk/Intercom/
  Salesforce delivery.
- Do not send surveys to contacts without consent or an allowed transactional
  basis captured in Attune.
- Do not infer consent from a raw email address found inside feedback content.
- Do not make the LLM decide when to survey a customer.
- Do not automatically create public Customer Request updates.
- Do not expose internal feedback text, private request fields, audit entries,
  owner metadata, or delivery errors on public survey pages.
- Do not guarantee inbox placement or final downstream delivery after a
  configured email provider accepts the message.

## Current State

### Workflow and reply foundations

Feedback workflow states already provide a tenant-specific state registry with
fixed categories: `open`, `active`, and `closed`. The default seed includes
closed states such as `fixed` and `wont_fix`. The workflow service validates
transitions and records audit events.

Reply drafts already have a human-in-the-loop workflow: generated, edited,
approved, sent, failed, rejected, and stale states. A successful send records
delivery metadata for a reviewed reply. That `sent` transition is a high-quality
post-interaction trigger because it means an operator-approved response was
accepted by Attune's configured reply send hook.

### Customer Requests

Customer Requests link one or more feedback rows to a curated product request.
They carry status, owner, priority, evidence, votes, and delivery issue links.
This gives survey responses a higher-level context: a response can measure the
customer's experience with a resolved feedback item, a reply, or the request
that the feedback supported.

### Close-the-loop notifications

Request notifications already introduced:

- `customer_notification_contacts`;
- `customer_notification_email_senders`;
- `customer_request_unsubscribe_tokens`;
- `customer_request_notification_events`;
- `customer_request_notification_deliveries`;
- tenant and contact email rate limits;
- list-unsubscribe behavior;
- encrypted email payloads;
- webhook targets and delivery logs.

Survey invitations should reuse the same contact and email-sender foundations.
They should not reuse request notification events as their business ledger,
because survey invitations and survey responses have different lifecycle states,
dedupe keys, response tokens, analytics, and retention needs.

## Proposal

Introduce a new survey bounded context:

```text
internal/repo/survey
internal/service/survey
internal/handlers/console/survey
internal/handlers/portal
console/src/features/surveys
proto/attune/v1/survey.proto
```

The survey service owns campaign configuration, trigger event resolution,
invitation tokens, response intake, suppression decisions, aggregate analytics,
and low-score review state.

The existing request-notification contact and email-sender tables remain the
source of truth for deliverable customer email identity. Survey-specific tables
reference those contacts where email delivery is used.

### Product model

The model has five layers:

1. **Campaign**: tenant-configured CSAT or CES measurement policy.
2. **Trigger event**: a durable record that a workflow transition, reply send,
   or request status change could start measurement.
3. **Invitation**: a recipient-specific hosted survey link with expiry,
   suppression state, and delivery state.
4. **Response**: the submitted score, comment, latency, and context snapshot.
5. **Review**: a low-score inspection state for operators.

This separation lets Attune explain why a customer was or was not invited, retry
email delivery safely, accept responses without a logged-in session, and report
trends even if the source feedback or request changes later.

Invitation state is decomposed into three dimensions:

- `delivery_status`: whether Attune needs to send, has sent, failed to send, or
  does not need a delivery attempt because the invitation is a hosted link.
- `response_status`: whether the hosted page is pending, opened, submitted, or
  expired.
- `suppression_status`: whether the invitation is eligible or suppressed, with
  a reason.

This avoids the common bug where "email delivery failed" hides the fact that a
hosted link can still receive a valid response.

### Metric semantics

Supported `metric_type` values:

- `csat`: customer satisfaction after a support or product interaction.
- `ces`: customer effort after a support or product interaction.

Campaigns carry score scale metadata:

- `scale_min`
- `scale_max`
- `higher_is_better`
- `positive_threshold`
- `low_score_threshold`

Default templates:

| Metric | Scale | Higher is better | Question | Positive threshold | Low-score threshold |
|---|---:|---|---|---:|---:|
| `csat` | 1-5 | yes | "How satisfied were you with this resolution?" | 4 | 2 |
| `ces` | 1-7 | yes | "How easy was it to get this resolved?" | 6 | 4 |

The stored response includes both the raw score and normalized score:

```text
normalized_score = 100 * (score_value - scale_min) / (scale_max - scale_min)
```

This keeps CSAT and CES comparable in aggregate views without losing the raw
survey scale.

### Campaign versions and snapshots

Every user-visible campaign change increments `content_version`. User-visible
content includes question text, comment prompt, scale labels, email subject,
email intro copy, locale, thresholds, and score ordering. Operational settings
such as send limits or retry backoff can update without changing the content
version.

Each invitation stores a campaign snapshot with:

- campaign ID and version;
- metric type;
- question text;
- comment prompt;
- scale min and max;
- score labels when configured;
- positive and low-score thresholds;
- locale;
- email subject and intro copy when email delivery is used;
- consent policy and legal basis snapshot.

Each response copies the same snapshot. This gives operators an exact record of
what the customer saw when campaigns are edited later.

### Triggering

Add a durable `survey_trigger_events` table. Trigger recorders write rows with
stable dedupe keys. A survey worker resolves pending trigger events into
campaign-specific invitations.

Supported trigger types:

- `feedback.workflow_transitioned`
- `reply_draft.sent`
- `customer_request.status_changed`

Trigger rules are campaign-owned:

- feedback workflow state IDs;
- workflow category, including `closed`;
- reply-draft sent status;
- request statuses such as `shipped` or `cancelled`;
- feedback source filter;
- feedback tag filter;
- request status filter;
- optional minimum time since feedback creation;
- optional minimum time since previous reply.
- optional maximum time since the last customer activity;
- optional suppression of auto-closed or system-closed items;
- campaign dedupe policy.

Supported campaign dedupe policies:

- `one_per_source`: one invitation per campaign, source object, and contact.
- `one_per_resolution`: one invitation per campaign, source object, resolution
  cycle, and contact.
- `one_per_trigger`: one invitation per campaign, trigger event, and contact.

`one_per_source` matches helpdesk defaults where one ticket should receive one
survey. `one_per_resolution` supports reopened feedback or request cycles.
`one_per_trigger` is useful for explicit hosted-link generation and carefully
sampled operational measurement.

The workflow and reply-draft services should record trigger events through an
injected small interface:

```go
type SurveyTriggerRecorder interface {
    RecordSurveyTrigger(ctx context.Context, event survey.TriggerEventInput) error
}
```

If survey wiring is absent, the recorder is a no-op. This keeps workflow and
reply draft behavior stable while letting the survey service own campaign
matching.

### Eligibility

When resolving a trigger event, the survey service evaluates each enabled
campaign:

1. The trigger type matches a campaign rule.
2. The feedback or request belongs to the authenticated tenant.
3. The source object is not deleted or archived.
4. The source object has either a deliverable contact audience or a campaign
   mode that allows source-linked hosted invitations.
5. The contact has an allowed consent state or allowed transactional basis.
6. The contact is not bounced, complained, suppressed, or opted out.
7. The source object has recent customer activity when the campaign requires it.
8. The source object was not auto-resolved when the campaign suppresses that
   outcome.
9. The contact has not exceeded the campaign cooldown.
10. The tenant has not exceeded the survey hourly limit.
11. The contact has not exceeded the survey daily limit.
12. The campaign sample-rate decision includes this source object.
13. No invitation already exists for the selected dedupe policy.
14. The campaign is still enabled when the delayed send becomes due.

Each failed eligibility check creates an invitation or event-level suppression
record with one of these reasons:

- `campaign_disabled`
- `rule_not_matched`
- `source_not_found`
- `source_deleted`
- `no_contact`
- `hosted_link_disabled`
- `missing_consent`
- `tenant_opted_out`
- `campaign_opted_out`
- `contact_suppressed`
- `email_bounced`
- `email_complained`
- `campaign_cooldown`
- `stale_customer_activity`
- `auto_resolved`
- `tenant_hourly_send_limit`
- `contact_daily_send_limit`
- `already_invited`
- `sampled_out`
- `expired_before_send`
- `email_sender_missing`
- `email_sender_unverified`

Suppression rows are as important as sent rows because operators need to trust
response-rate denominators.

### Audience

The survey audience is source-object aware.

For feedback-anchored events:

- the portal submitter contact if the feedback came from a portal submission;
- the linked customer notification contact when the feedback was promoted into
  a request and a contact link exists;
- a manually linked notification contact if one is present.
- a source-linked hosted invitation when the campaign allows hosted links and no
  contact is available.

For request-anchored events:

- request subscribers from `customer_request_subscriptions`;
- linked-feedback submitter contacts;
- explicit customer links that have a verified notification contact.
- a request-linked hosted invitation when the campaign allows hosted links and
  an operator needs to distribute the link outside Attune.

The service dedupes by the campaign's selected dedupe policy. Contact-backed
invitations include `contact_id`; source-link invitations use a stable
source-link recipient hash. This allows hosted links without turning anonymous
responses into unanchored surveys.

### Public hosted survey links

Each invitation receives a high-entropy token. Only the token hash is stored.
The public route renders a minimal, public-safe survey page, while the `/v1`
routes keep a JSON API for embedded and automated clients:

```text
GET  /surveys/{token}
POST /surveys/{token}/responses
GET  /v1/surveys/{token}
POST /v1/surveys/{token}/responses
```

The hosted HTML and JSON public survey routes are protected by two anonymous
rate-limit dimensions:

- a client bucket keyed by a hash of the resolved client IP, tenant-scoped when
  the route includes a tenant slug;
- a survey bucket keyed by a hash of the invitation token.

The client bucket limits broad token scanning from one source. The token bucket
limits concentrated abuse against a single invitation even when requests come
from many client addresses. Anonymous limiter buckets are evicted after an idle
window so random token probes cannot create unbounded in-process state.

The GET route may accept a `score` query parameter from an email button and
render that score selected. It must never create a response or mark the
invitation as opened. This avoids false submissions and inflated open funnels
from email security scanners, link previewers, and safe-link prefetchers.

Survey invitation emails render score deep links for the campaign scale. Each
link opens the hosted page with `?score=N`, leaving the final response creation
to the explicit page form submission.

Contact-backed survey invitation emails reuse the public notification
tenant-level unsubscribe token and endpoint. The same URL is exposed through the
message body and `List-Unsubscribe` / `List-Unsubscribe-Post` headers so mailbox
providers can perform one-click unsubscribe without touching the survey
response token.

Contact-backed public survey retrieval also returns an `unsubscribe_url` and
the hosted page renders it with `rel="nofollow"`. The URL uses a freshly minted
tenant-level notification unsubscribe token and does not reuse the survey
response token.

Hosted links are valid for two recipient modes:

- `contact`: a known notification contact receives or can be handed a personal
  link.
- `source_link`: Attune creates a link anchored to a feedback or request object
  without a known contact. The response is linked to the source object but not
  to a contact.

Source-link invitations are useful for operator copy/paste workflows and
external systems. They still require a tenant, campaign, source object, token,
expiry, and campaign snapshot.

Public survey responses must set privacy-focused headers:

- `Referrer-Policy: no-referrer`
- `Cache-Control: no-store`
- `X-Robots-Tag: noindex, nofollow`

HTTP access logs and application logs must redact survey tokens from request
paths. The token is authentication material even though only its hash is stored.

The POST route validates:

- token exists and hash matches;
- tenant slug matches the invitation tenant;
- invitation status allows submission;
- token has not expired;
- score is within the campaign scale;
- comment length is valid;
- duplicate submissions return the existing accepted response.

The public page renders only:

- tenant public display name or slug;
- survey question;
- score controls;
- comment field;
- unsubscribe link when the invitation is contact-backed;
- a short source-safe label such as "your recent request" or the public request
  title when visibility allows it.

It must not render raw feedback text, private request fields, operator names,
internal workflow labels, audit data, or delivery errors.

### Email invitations

Email invitation delivery should reuse:

- `customer_notification_contacts` for encrypted recipient email;
- `customer_notification_email_senders` for sender/provider configuration;
- existing email validation and secret-handling helpers where possible;
- List-Unsubscribe behavior and tenant-scoped unsubscribe URL patterns.

Survey delivery rows stay in survey-owned tables because the payload, dedupe,
response token, and analytics are survey-specific.

Survey email requirements:

- include one link per score value, each opening the hosted survey page with a
  preselected score;
- include a separate "leave a comment" or "open survey" link;
- include a campaign unsubscribe link and a tenant-surveys unsubscribe link;
- include stable `survey_id`, `campaign_id`, `invitation_id`, and `trace_id` in
  webhook or provider metadata;
- avoid raw email address, feedback body, or customer comments in logs.
- store an encrypted `webhook_secret` in provider config when the sender should
  receive automated provider callbacks.

Provider events are recorded when available:

- accepted;
- delivered;
- bounced;
- complained;
- rejected;
- temporarily delayed;
- opened when the provider sends a trustworthy event.

Bounce and complaint events update `customer_notification_contacts` suppression
state through the same safety rules as request notifications. Provider opens do
not create responses and do not affect score metrics. Provider delayed events
are stored as delivery diagnostics and remove the invitation from Attune's local
resend queue after the message has been accepted by the provider.

Automated provider callbacks use a raw HTTP sink:

```text
POST /v1/surveys/provider-events/{tenant_id}/{sender_id}
```

The sink verifies `X-Attune-Webhook-Timestamp` plus
`X-Attune-Webhook-Signature-256` before parsing the body. The signature is
HMAC-SHA256 over `timestamp + "." + raw_body` using the sender's encrypted
`webhook_secret`. Requests outside the replay window are rejected before the
event reaches the survey state machine.

The first deliverable channel is `email`. Hosted links are generated for every
invitation regardless of whether email is enabled, so operators can copy a link
from Console when the contact should receive it through an external channel.

### Response intake

`survey_responses` stores the first valid response for an invitation. The first
valid submission wins. Duplicate submissions are idempotent and return the
existing response summary.

Stored response fields:

- tenant ID;
- campaign ID;
- campaign version;
- invitation ID;
- trigger event ID;
- metric type;
- score value;
- scale min and max;
- normalized score;
- comment;
- submitted timestamp;
- response latency seconds;
- low-score boolean;
- feedback ID;
- request ID;
- contact ID where known;
- recipient kind;
- recipient source;
- campaign snapshot;
- operator snapshot;
- source snapshot;
- response metadata with hashed IP and hashed user agent.

Comments are customer-provided content. They should be visible in Console
drilldown, covered by GDPR export/delete behavior, and omitted from logs and
audit metadata.

### Low-score review

Low-score responses create a review state:

- `open`
- `acknowledged`
- `resolved`
- `suppressed`

The review row links to the response and optionally to the Customer Request.
It stores owner member ID, due time, severity, routing metadata, internal notes,
root cause, action taken, whether the customer was contacted, and timestamps.
It does not send any customer-facing message by itself.

Console can use this review state to show "unreviewed low scores" without
turning the survey subsystem into a support-ticket tool.

## Data Model

Add a migration such as
`internal/infra/database/migrations/123_post_resolution_surveys.sql`.

### `survey_campaigns`

```sql
CREATE TABLE IF NOT EXISTS survey_campaigns (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                        TEXT NOT NULL,
    metric_type                 TEXT NOT NULL,
    enabled                     BOOLEAN NOT NULL DEFAULT false,
    locale                      TEXT NOT NULL DEFAULT '',
    content_version             INT NOT NULL DEFAULT 1,
    question_text               TEXT NOT NULL,
    comment_prompt              TEXT NOT NULL DEFAULT '',
    email_subject               TEXT NOT NULL DEFAULT '',
    email_intro                 TEXT NOT NULL DEFAULT '',
    localized_content           JSONB NOT NULL DEFAULT '{}'::jsonb,
    public_page_config          JSONB NOT NULL DEFAULT '{}'::jsonb,
    low_score_comment_required  BOOLEAN NOT NULL DEFAULT false,
    scale_min                   SMALLINT NOT NULL,
    scale_max                   SMALLINT NOT NULL,
    scale_labels                JSONB NOT NULL DEFAULT '{}'::jsonb,
    higher_is_better            BOOLEAN NOT NULL DEFAULT true,
    positive_threshold          SMALLINT NOT NULL,
    low_score_threshold         SMALLINT NOT NULL,
    low_score_sla_hours         INT NOT NULL DEFAULT 72,
    dedupe_policy               TEXT NOT NULL DEFAULT 'one_per_source',
    consent_policy              TEXT NOT NULL DEFAULT 'explicit_opt_in',
    email_purpose               TEXT NOT NULL DEFAULT 'transactional_survey',
    legal_basis                 TEXT NOT NULL DEFAULT '',
    allow_source_link           BOOLEAN NOT NULL DEFAULT true,
    max_time_since_customer_activity_hours INT NOT NULL DEFAULT 168,
    suppress_auto_closed        BOOLEAN NOT NULL DEFAULT true,
    send_delay_seconds          INT NOT NULL DEFAULT 0,
    invitation_ttl_hours        INT NOT NULL DEFAULT 336,
    cooldown_hours              INT NOT NULL DEFAULT 720,
    sample_rate_basis_points    INT NOT NULL DEFAULT 10000,
    tenant_hourly_send_limit    INT NOT NULL DEFAULT 500,
    contact_daily_send_limit    INT NOT NULL DEFAULT 2,
    created_by                  TEXT NOT NULL,
    updated_by                  TEXT NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at                 TIMESTAMPTZ,
    CONSTRAINT uq_survey_campaigns_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT chk_survey_campaigns_metric CHECK (metric_type IN ('csat', 'ces')),
    CONSTRAINT chk_survey_campaigns_name_length CHECK (length(name) BETWEEN 1 AND 120),
    CONSTRAINT chk_survey_campaigns_content_version CHECK (content_version > 0),
    CONSTRAINT chk_survey_campaigns_question_length CHECK (length(question_text) BETWEEN 1 AND 500),
    CONSTRAINT chk_survey_campaigns_comment_prompt_length CHECK (length(comment_prompt) <= 500),
    CONSTRAINT chk_survey_campaigns_email_subject_length CHECK (length(email_subject) <= 200),
    CONSTRAINT chk_survey_campaigns_email_intro_length CHECK (length(email_intro) <= 2000),
    CONSTRAINT chk_survey_campaigns_localized_content_object CHECK (jsonb_typeof(localized_content) = 'object'),
    CONSTRAINT chk_survey_campaigns_public_page_config_object CHECK (jsonb_typeof(public_page_config) = 'object'),
    CONSTRAINT chk_survey_campaigns_scale_labels_object CHECK (jsonb_typeof(scale_labels) = 'object'),
    CONSTRAINT chk_survey_campaigns_scale CHECK (scale_min >= 0 AND scale_max > scale_min),
    CONSTRAINT chk_survey_campaigns_thresholds CHECK (
        positive_threshold BETWEEN scale_min AND scale_max AND
        low_score_threshold BETWEEN scale_min AND scale_max
    ),
    CONSTRAINT chk_survey_campaigns_dedupe_policy
        CHECK (dedupe_policy IN ('one_per_source', 'one_per_resolution', 'one_per_trigger')),
    CONSTRAINT chk_survey_campaigns_consent_policy
        CHECK (consent_policy IN ('explicit_opt_in', 'transactional_allowed', 'existing_app_consent')),
    CONSTRAINT chk_survey_campaigns_email_purpose
        CHECK (email_purpose IN ('transactional_survey', 'product_feedback_survey')),
    CONSTRAINT chk_survey_campaigns_limits CHECK (
        send_delay_seconds >= 0 AND
        invitation_ttl_hours BETWEEN 1 AND 8760 AND
        cooldown_hours >= 0 AND
        low_score_sla_hours >= 0 AND
        max_time_since_customer_activity_hours >= 0 AND
        sample_rate_basis_points BETWEEN 0 AND 10000 AND
        tenant_hourly_send_limit >= 0 AND
        contact_daily_send_limit >= 0
    )
);
```

Indexes:

- `(tenant_id, enabled, updated_at DESC) WHERE archived_at IS NULL`
- `(tenant_id, metric_type, updated_at DESC) WHERE archived_at IS NULL`

### `survey_campaign_triggers`

```sql
CREATE TABLE IF NOT EXISTS survey_campaign_triggers (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    campaign_id                 UUID NOT NULL,
    trigger_type                TEXT NOT NULL,
    workflow_state_id           UUID,
    workflow_category           TEXT NOT NULL DEFAULT '',
    request_status              TEXT NOT NULL DEFAULT '',
    source_filter               JSONB NOT NULL DEFAULT '{}'::jsonb,
    tag_filter                  JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_filter              JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled                     BOOLEAN NOT NULL DEFAULT true,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_campaign_triggers_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT fk_survey_campaign_triggers_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_survey_campaign_triggers_workflow_state
        FOREIGN KEY (tenant_id, workflow_state_id)
        REFERENCES tenant_workflow_states(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_survey_campaign_triggers_type CHECK (
        trigger_type IN (
            'feedback.workflow_transitioned',
            'reply_draft.sent',
            'customer_request.status_changed'
        )
    ),
    CONSTRAINT chk_survey_campaign_triggers_workflow_category
        CHECK (workflow_category IN ('', 'open', 'active', 'closed')),
    CONSTRAINT chk_survey_campaign_triggers_source_filter_object CHECK (jsonb_typeof(source_filter) = 'object'),
    CONSTRAINT chk_survey_campaign_triggers_tag_filter_object CHECK (jsonb_typeof(tag_filter) = 'object'),
    CONSTRAINT chk_survey_campaign_triggers_request_filter_object CHECK (jsonb_typeof(request_filter) = 'object')
);
```

Indexes:

- `(tenant_id, campaign_id)`
- `(tenant_id, trigger_type, enabled)`
- `(tenant_id, workflow_state_id) WHERE workflow_state_id IS NOT NULL`

### `survey_trigger_events`

```sql
CREATE TABLE IF NOT EXISTS survey_trigger_events (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    trigger_type                TEXT NOT NULL,
    dedupe_key                  TEXT NOT NULL,
    outcome_cycle_key           TEXT NOT NULL DEFAULT '',
    feedback_id                 BIGINT,
    request_id                  UUID,
    reply_draft_id              UUID,
    workflow_from_state_id      UUID,
    workflow_to_state_id        UUID,
    old_request_status          TEXT NOT NULL DEFAULT '',
    new_request_status          TEXT NOT NULL DEFAULT '',
    customer_last_activity_at   TIMESTAMPTZ,
    closed_by_type              TEXT NOT NULL DEFAULT '',
    auto_closed                 BOOLEAN NOT NULL DEFAULT false,
    source_snapshot             JSONB NOT NULL DEFAULT '{}'::jsonb,
    operator_snapshot           JSONB NOT NULL DEFAULT '{}'::jsonb,
    resolution_snapshot         JSONB NOT NULL DEFAULT '{}'::jsonb,
    status                      TEXT NOT NULL DEFAULT 'pending',
    attempts                    SMALLINT NOT NULL DEFAULT 0,
    next_retry_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at                  TIMESTAMPTZ,
    claimed_by                  TEXT NOT NULL DEFAULT '',
    resolved_at                 TIMESTAMPTZ,
    last_error                  TEXT NOT NULL DEFAULT '',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_trigger_events_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT uq_survey_trigger_events_dedupe UNIQUE (tenant_id, dedupe_key),
    CONSTRAINT fk_survey_trigger_events_feedback
        FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES user_feedback(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_trigger_events_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_trigger_events_from_state
        FOREIGN KEY (tenant_id, workflow_from_state_id)
        REFERENCES tenant_workflow_states(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_trigger_events_to_state
        FOREIGN KEY (tenant_id, workflow_to_state_id)
        REFERENCES tenant_workflow_states(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_survey_trigger_events_type CHECK (
        trigger_type IN (
            'feedback.workflow_transitioned',
            'reply_draft.sent',
            'customer_request.status_changed'
        )
    ),
    CONSTRAINT chk_survey_trigger_events_status
        CHECK (status IN ('pending', 'resolving', 'resolved', 'failed', 'dead')),
    CONSTRAINT chk_survey_trigger_events_closed_by_type
        CHECK (closed_by_type IN ('', 'customer', 'operator', 'system', 'integration')),
    CONSTRAINT chk_survey_trigger_events_snapshot_object CHECK (jsonb_typeof(source_snapshot) = 'object'),
    CONSTRAINT chk_survey_trigger_events_operator_snapshot_object CHECK (jsonb_typeof(operator_snapshot) = 'object'),
    CONSTRAINT chk_survey_trigger_events_resolution_snapshot_object CHECK (jsonb_typeof(resolution_snapshot) = 'object')
);
```

Indexes:

- `(status, next_retry_at, created_at, id) WHERE status IN ('pending', 'failed')`
- `(tenant_id, feedback_id, created_at DESC) WHERE feedback_id IS NOT NULL`
- `(tenant_id, request_id, created_at DESC) WHERE request_id IS NOT NULL`

### `survey_invitations`

```sql
CREATE TABLE IF NOT EXISTS survey_invitations (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    campaign_id                 UUID NOT NULL,
    trigger_event_id            UUID,
    feedback_id                 BIGINT,
    request_id                  UUID,
    contact_id                  UUID,
    recipient_kind              TEXT NOT NULL DEFAULT 'contact',
    recipient_source            TEXT NOT NULL DEFAULT '',
    recipient_hash              TEXT NOT NULL DEFAULT '',
    token_version               TEXT NOT NULL DEFAULT 'v1',
    token_hash                  TEXT NOT NULL,
    distribution_mode           TEXT NOT NULL DEFAULT 'hosted_link',
    delivery_status             TEXT NOT NULL DEFAULT 'not_required',
    response_status             TEXT NOT NULL DEFAULT 'pending',
    suppression_status          TEXT NOT NULL DEFAULT 'eligible',
    suppression_reason          TEXT NOT NULL DEFAULT '',
    dedupe_policy               TEXT NOT NULL,
    dedupe_key                  TEXT NOT NULL,
    outcome_cycle_key           TEXT NOT NULL DEFAULT '',
    campaign_version            INT NOT NULL,
    campaign_snapshot           JSONB NOT NULL DEFAULT '{}'::jsonb,
    consent_snapshot            JSONB NOT NULL DEFAULT '{}'::jsonb,
    due_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at                     TIMESTAMPTZ,
    opened_at                   TIMESTAMPTZ,
    submitted_at                TIMESTAMPTZ,
    expires_at                  TIMESTAMPTZ NOT NULL,
    last_revalidated_at         TIMESTAMPTZ,
    source_snapshot             JSONB NOT NULL DEFAULT '{}'::jsonb,
    recipient_snapshot          JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_invitations_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT uq_survey_invitations_token UNIQUE (token_hash),
    CONSTRAINT uq_survey_invitations_dedupe UNIQUE (tenant_id, dedupe_key),
    CONSTRAINT fk_survey_invitations_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_invitations_trigger
        FOREIGN KEY (tenant_id, trigger_event_id)
        REFERENCES survey_trigger_events(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_invitations_feedback
        FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES user_feedback(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_invitations_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_invitations_contact
        FOREIGN KEY (tenant_id, contact_id)
        REFERENCES customer_notification_contacts(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_survey_invitations_recipient_kind CHECK (recipient_kind IN ('contact', 'source_link')),
    CONSTRAINT chk_survey_invitations_recipient_shape CHECK (
        (recipient_kind = 'contact' AND contact_id IS NOT NULL) OR
        (recipient_kind = 'source_link' AND contact_id IS NULL)
    ),
    CONSTRAINT chk_survey_invitations_distribution_mode CHECK (distribution_mode IN ('hosted_link', 'email')),
    CONSTRAINT chk_survey_invitations_delivery_status CHECK (
        delivery_status IN ('not_required', 'pending', 'scheduled', 'sent', 'failed', 'dead', 'suppressed')
    ),
    CONSTRAINT chk_survey_invitations_response_status CHECK (response_status IN ('pending', 'opened', 'submitted', 'expired')),
    CONSTRAINT chk_survey_invitations_suppression_status CHECK (suppression_status IN ('eligible', 'suppressed')),
    CONSTRAINT chk_survey_invitations_dedupe_policy
        CHECK (dedupe_policy IN ('one_per_source', 'one_per_resolution', 'one_per_trigger')),
    CONSTRAINT chk_survey_invitations_campaign_version CHECK (campaign_version > 0),
    CONSTRAINT chk_survey_invitations_campaign_snapshot_object CHECK (jsonb_typeof(campaign_snapshot) = 'object'),
    CONSTRAINT chk_survey_invitations_consent_snapshot_object CHECK (jsonb_typeof(consent_snapshot) = 'object'),
    CONSTRAINT chk_survey_invitations_source_snapshot_object CHECK (jsonb_typeof(source_snapshot) = 'object'),
    CONSTRAINT chk_survey_invitations_recipient_snapshot_object CHECK (jsonb_typeof(recipient_snapshot) = 'object')
);
```

Indexes:

- `(tenant_id, campaign_id, delivery_status, response_status, created_at DESC, id DESC)`
- `(tenant_id, feedback_id, created_at DESC) WHERE feedback_id IS NOT NULL`
- `(tenant_id, request_id, created_at DESC) WHERE request_id IS NOT NULL`
- `(tenant_id, contact_id, created_at DESC) WHERE contact_id IS NOT NULL`
- `(delivery_status, due_at, created_at, id) WHERE delivery_status IN ('pending', 'scheduled', 'failed')`

### `survey_delivery_attempts`

```sql
CREATE TABLE IF NOT EXISTS survey_delivery_attempts (
    id                          BIGSERIAL PRIMARY KEY,
    tenant_id                   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    invitation_id               UUID NOT NULL,
    campaign_id                 UUID NOT NULL,
    channel                     TEXT NOT NULL,
    destination_hash            TEXT NOT NULL,
    payload                     JSONB NOT NULL,
    sensitive_payload           BYTEA,
    status                      TEXT NOT NULL DEFAULT 'pending',
    provider_message_id         TEXT NOT NULL DEFAULT '',
    provider_status             TEXT NOT NULL DEFAULT '',
    attempts                    SMALLINT NOT NULL DEFAULT 0,
    failure_kind                TEXT NOT NULL DEFAULT '',
    http_status                 INT,
    last_error                  TEXT NOT NULL DEFAULT '',
    dead_reason                 TEXT NOT NULL DEFAULT '',
    next_retry_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    trace_id                    TEXT NOT NULL DEFAULT '',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at                TIMESTAMPTZ,
    claimed_at                  TIMESTAMPTZ,
    claimed_by                  TEXT NOT NULL DEFAULT '',
    last_manual_retry_at        TIMESTAMPTZ,
    retried_by                  TEXT NOT NULL DEFAULT '',
    manual_retry_count          INT NOT NULL DEFAULT 0,
    CONSTRAINT fk_survey_delivery_attempts_invitation
        FOREIGN KEY (tenant_id, invitation_id)
        REFERENCES survey_invitations(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_delivery_attempts_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_survey_delivery_attempts_channel CHECK (channel IN ('email')),
    CONSTRAINT chk_survey_delivery_attempts_status
        CHECK (status IN ('pending', 'delivered', 'failed', 'dead', 'suppressed')),
    CONSTRAINT chk_survey_delivery_attempts_provider_status_length CHECK (length(provider_status) <= 80),
    CONSTRAINT chk_survey_delivery_attempts_payload_object CHECK (jsonb_typeof(payload) = 'object')
);
```

Indexes:

- `(status, next_retry_at, created_at, id) WHERE status IN ('pending', 'failed')`
- `(tenant_id, campaign_id, status, created_at DESC, id DESC)`
- `(tenant_id, invitation_id, created_at DESC)`

### `survey_provider_events`

```sql
CREATE TABLE IF NOT EXISTS survey_provider_events (
    id                  BIGSERIAL PRIMARY KEY,
    tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    invitation_id       UUID,
    provider            TEXT NOT NULL,
    provider_event_type TEXT NOT NULL,
    provider_message_id TEXT NOT NULL DEFAULT '',
    provider_event_key  TEXT NOT NULL DEFAULT '',
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_survey_provider_events_invitation
        FOREIGN KEY (tenant_id, invitation_id)
        REFERENCES survey_invitations(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_survey_provider_events_provider_length CHECK (length(provider) BETWEEN 1 AND 120),
    CONSTRAINT chk_survey_provider_events_type
        CHECK (provider_event_type IN ('accepted', 'delivered', 'bounced', 'complained', 'rejected', 'temporarily_delayed', 'opened')),
    CONSTRAINT chk_survey_provider_events_message_length CHECK (length(provider_message_id) <= 512),
    CONSTRAINT chk_survey_provider_events_key_length CHECK (length(provider_event_key) <= 512),
    CONSTRAINT chk_survey_provider_events_payload_object CHECK (jsonb_typeof(payload) = 'object')
);
```

Indexes:

- `(tenant_id, invitation_id, created_at DESC) WHERE invitation_id IS NOT NULL`
- `(tenant_id, provider, provider_message_id) WHERE provider_message_id <> ''`
- unique `(tenant_id, provider, provider_event_key) WHERE provider_event_key <> ''`

### `survey_responses`

```sql
CREATE TABLE IF NOT EXISTS survey_responses (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    invitation_id               UUID NOT NULL,
    campaign_id                 UUID NOT NULL,
    trigger_event_id            UUID,
    feedback_id                 BIGINT,
    request_id                  UUID,
    contact_id                  UUID,
    metric_type                 TEXT NOT NULL,
    campaign_version            INT NOT NULL,
    recipient_kind              TEXT NOT NULL,
    score_value                 SMALLINT NOT NULL,
    scale_min                   SMALLINT NOT NULL,
    scale_max                   SMALLINT NOT NULL,
    normalized_score            NUMERIC(6,2) NOT NULL,
    low_score                   BOOLEAN NOT NULL DEFAULT false,
    positive_score              BOOLEAN NOT NULL DEFAULT false,
    comment                     TEXT NOT NULL DEFAULT '',
    response_latency_seconds    INT NOT NULL DEFAULT 0,
    campaign_snapshot           JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_snapshot             JSONB NOT NULL DEFAULT '{}'::jsonb,
    operator_snapshot           JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_meta               JSONB NOT NULL DEFAULT '{}'::jsonb,
    submitted_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_responses_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT uq_survey_responses_invitation UNIQUE (tenant_id, invitation_id),
    CONSTRAINT fk_survey_responses_invitation
        FOREIGN KEY (tenant_id, invitation_id)
        REFERENCES survey_invitations(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_responses_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_responses_trigger
        FOREIGN KEY (tenant_id, trigger_event_id)
        REFERENCES survey_trigger_events(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_responses_feedback
        FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES user_feedback(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_responses_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_responses_contact
        FOREIGN KEY (tenant_id, contact_id)
        REFERENCES customer_notification_contacts(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_survey_responses_metric CHECK (metric_type IN ('csat', 'ces')),
    CONSTRAINT chk_survey_responses_campaign_version CHECK (campaign_version > 0),
    CONSTRAINT chk_survey_responses_recipient_kind CHECK (recipient_kind IN ('contact', 'source_link')),
    CONSTRAINT chk_survey_responses_score_range CHECK (score_value BETWEEN scale_min AND scale_max),
    CONSTRAINT chk_survey_responses_normalized CHECK (normalized_score BETWEEN 0 AND 100),
    CONSTRAINT chk_survey_responses_comment_length CHECK (length(comment) <= 10000),
    CONSTRAINT chk_survey_responses_campaign_snapshot_object CHECK (jsonb_typeof(campaign_snapshot) = 'object'),
    CONSTRAINT chk_survey_responses_source_snapshot_object CHECK (jsonb_typeof(source_snapshot) = 'object'),
    CONSTRAINT chk_survey_responses_operator_snapshot_object CHECK (jsonb_typeof(operator_snapshot) = 'object'),
    CONSTRAINT chk_survey_responses_meta_object CHECK (jsonb_typeof(response_meta) = 'object')
);
```

Indexes:

- `(tenant_id, campaign_id, submitted_at DESC, id DESC)`
- `(tenant_id, campaign_id, low_score, submitted_at DESC, id DESC)`
- `(tenant_id, feedback_id, submitted_at DESC) WHERE feedback_id IS NOT NULL`
- `(tenant_id, request_id, submitted_at DESC) WHERE request_id IS NOT NULL`

### `survey_low_score_reviews`

```sql
CREATE TABLE IF NOT EXISTS survey_low_score_reviews (
    response_id        UUID PRIMARY KEY,
    tenant_id          TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    campaign_id        UUID NOT NULL,
    status             TEXT NOT NULL DEFAULT 'open',
    severity           TEXT NOT NULL DEFAULT 'medium',
    owner_member_id    UUID,
    root_cause         TEXT NOT NULL DEFAULT '',
    action_taken       TEXT NOT NULL DEFAULT '',
    customer_contacted BOOLEAN NOT NULL DEFAULT false,
    due_at             TIMESTAMPTZ,
    reviewed_at        TIMESTAMPTZ,
    claimed_at         TIMESTAMPTZ,
    claimed_by         TEXT NOT NULL DEFAULT '',
    updated_by         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_low_score_reviews_tenant_response UNIQUE (tenant_id, response_id),
    CONSTRAINT fk_survey_low_score_reviews_response
        FOREIGN KEY (tenant_id, response_id)
        REFERENCES survey_responses(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_survey_low_score_reviews_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_survey_low_score_reviews_status
        CHECK (status IN ('open', 'in_review', 'resolved', 'dismissed')),
    CONSTRAINT chk_survey_low_score_reviews_severity
        CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    CONSTRAINT chk_survey_low_score_reviews_root_cause_length CHECK (length(root_cause) <= 120),
    CONSTRAINT chk_survey_low_score_reviews_action_length CHECK (length(action_taken) <= 5000),
    CONSTRAINT chk_survey_low_score_reviews_claimed_by_length CHECK (length(claimed_by) <= 256),
    CONSTRAINT chk_survey_low_score_reviews_updated_by_length CHECK (length(updated_by) <= 256)
);
```

Indexes:

- `(tenant_id, campaign_id, status, due_at NULLS LAST, severity, updated_at DESC)`
- `(due_at ASC NULLS FIRST, updated_at ASC, response_id ASC) WHERE status IN ('open', 'in_review')`

### `survey_recovery_notifications`

```sql
CREATE TABLE IF NOT EXISTS survey_recovery_notifications (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    response_id         UUID NOT NULL,
    owner_member_id     UUID,
    channel             TEXT NOT NULL DEFAULT 'email',
    status              TEXT NOT NULL DEFAULT 'pending',
    reason              TEXT NOT NULL DEFAULT '',
    destination_hash    TEXT NOT NULL DEFAULT '',
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    provider            TEXT NOT NULL DEFAULT '',
    provider_message_id TEXT NOT NULL DEFAULT '',
    attempts            INT NOT NULL DEFAULT 0,
    failure_kind        TEXT NOT NULL DEFAULT '',
    http_status         INT,
    last_error          TEXT NOT NULL DEFAULT '',
    claimed_at          TIMESTAMPTZ,
    claimed_by          TEXT NOT NULL DEFAULT '',
    next_retry_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Indexes:

- `(tenant_id, response_id, owner_member_id, channel, reason) WHERE owner_member_id IS NOT NULL`
- `(next_retry_at ASC, created_at ASC, id ASC) WHERE channel = 'email' AND status IN ('pending', 'failed')`

### `survey_contact_preferences`

```sql
CREATE TABLE IF NOT EXISTS survey_contact_preferences (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    contact_id                  UUID NOT NULL,
    campaign_id                 UUID,
    scope                       TEXT NOT NULL,
    status                      TEXT NOT NULL DEFAULT 'active',
    reason                      TEXT NOT NULL DEFAULT '',
    updated_by                  TEXT NOT NULL DEFAULT '',
    unsubscribed_at             TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_contact_preferences_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT fk_survey_contact_preferences_contact
        FOREIGN KEY (tenant_id, contact_id)
        REFERENCES customer_notification_contacts(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_survey_contact_preferences_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_survey_contact_preferences_scope CHECK (scope IN ('tenant_surveys', 'campaign')),
    CONSTRAINT chk_survey_contact_preferences_status CHECK (status IN ('active', 'unsubscribed', 'suppressed')),
    CONSTRAINT chk_survey_contact_preferences_shape CHECK (
        (scope = 'tenant_surveys' AND campaign_id IS NULL) OR
        (scope = 'campaign' AND campaign_id IS NOT NULL)
    )
);
```

Unique indexes:

- `(tenant_id, contact_id, scope) WHERE scope = 'tenant_surveys'`
- `(tenant_id, contact_id, campaign_id) WHERE campaign_id IS NOT NULL`

### `survey_unsubscribe_tokens`

```sql
CREATE TABLE IF NOT EXISTS survey_unsubscribe_tokens (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    contact_id                  UUID NOT NULL,
    campaign_id                 UUID,
    scope                       TEXT NOT NULL,
    token_version               TEXT NOT NULL DEFAULT 'v1',
    token_hash                  TEXT NOT NULL UNIQUE,
    used_by_user_agent          TEXT NOT NULL DEFAULT '',
    expires_at                  TIMESTAMPTZ,
    used_at                     TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_survey_unsubscribe_tokens_contact
        FOREIGN KEY (tenant_id, contact_id)
        REFERENCES customer_notification_contacts(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_survey_unsubscribe_tokens_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_survey_unsubscribe_tokens_scope CHECK (scope IN ('tenant_surveys', 'campaign')),
    CONSTRAINT chk_survey_unsubscribe_tokens_shape CHECK (
        (scope = 'tenant_surveys' AND campaign_id IS NULL) OR
        (scope = 'campaign' AND campaign_id IS NOT NULL)
    )
);
```

## API Contract

Add `proto/attune/v1/survey.proto` and route it through `make proto`.

### Console service

```proto
service SurveyService {
  rpc ListSurveyCampaigns(ListSurveyCampaignsRequest) returns (ListSurveyCampaignsResponse);
  rpc CreateSurveyCampaign(CreateSurveyCampaignRequest) returns (SurveyCampaign);
  rpc UpdateSurveyCampaign(UpdateSurveyCampaignRequest) returns (SurveyCampaign);
  rpc ArchiveSurveyCampaign(ArchiveSurveyCampaignRequest) returns (SurveyCampaign);
  rpc CreateSurveyHostedLink(CreateSurveyHostedLinkRequest) returns (SurveyInvitation);
  rpc PreviewSurveyRecipients(PreviewSurveyRecipientsRequest) returns (PreviewSurveyRecipientsResponse);
  rpc SendSurveyTestEmail(SendSurveyTestEmailRequest) returns (SendSurveyTestEmailResponse);
  rpc GetSurveyCampaignHealth(GetSurveyCampaignHealthRequest) returns (SurveyCampaignHealth);
  rpc RecordSurveyProviderEvent(RecordSurveyProviderEventRequest) returns (SurveyInvitation);
  rpc ListSurveyInvitations(ListSurveyInvitationsRequest) returns (ListSurveyInvitationsResponse);
  rpc RetrySurveyInvitationDelivery(RetrySurveyInvitationDeliveryRequest) returns (SurveyInvitation);
  rpc ListSurveyResponses(ListSurveyResponsesRequest) returns (ListSurveyResponsesResponse);
  rpc GetSurveyAnalytics(GetSurveyAnalyticsRequest) returns (SurveyAnalytics);
  rpc GetSurveyAnalyticsTrend(GetSurveyAnalyticsTrendRequest) returns (GetSurveyAnalyticsTrendResponse);
  rpc GetSurveyAnalyticsSegments(GetSurveyAnalyticsSegmentsRequest) returns (GetSurveyAnalyticsSegmentsResponse);
  rpc GetSurveyAnalyticsInsights(GetSurveyAnalyticsInsightsRequest) returns (GetSurveyAnalyticsInsightsResponse);
  rpc UpdateSurveyLowScoreReview(UpdateSurveyLowScoreReviewRequest) returns (SurveyLowScoreReview);
  rpc BatchUpdateSurveyLowScoreReviews(BatchUpdateSurveyLowScoreReviewsRequest) returns (BatchUpdateSurveyLowScoreReviewsResponse);
  rpc AssignSurveyLowScoreReviews(AssignSurveyLowScoreReviewsRequest) returns (AssignSurveyLowScoreReviewsResponse);
  rpc EscalateSurveyLowScoreReviews(EscalateSurveyLowScoreReviewsRequest) returns (EscalateSurveyLowScoreReviewsResponse);
}
```

HTTP routes:

- `GET /fb/v1/console/surveys/campaigns`
- `POST /fb/v1/console/surveys/campaigns`
- `PATCH /fb/v1/console/surveys/campaigns/{id}`
- `POST /fb/v1/console/surveys/campaigns/{id}:archive`
- `POST /fb/v1/console/surveys/campaigns/{campaign_id}/hosted-links`
- `POST /fb/v1/console/surveys/campaigns/{campaign_id}/recipients:preview`
- `POST /fb/v1/console/surveys/campaigns/{campaign_id}:sendTestEmail`
- `GET /fb/v1/console/surveys/campaigns/{campaign_id}/health`
- `POST /fb/v1/console/surveys/provider-events:record`
- `GET /fb/v1/console/surveys/invitations`
- `POST /fb/v1/console/surveys/invitations/{id}:retry`
- `GET /fb/v1/console/surveys/responses`
- `GET /fb/v1/console/surveys/analytics`
- `GET /fb/v1/console/surveys/analytics/trend`
- `GET /fb/v1/console/surveys/analytics/segments`
- `GET /fb/v1/console/surveys/analytics/insights`
- `PATCH /fb/v1/console/surveys/responses/{response_id}/low-score-review`
- `POST /fb/v1/console/surveys/responses/low-score-reviews:batchUpdate`
- `POST /fb/v1/console/surveys/responses/low-score-reviews:assign`
- `POST /fb/v1/console/surveys/responses/low-score-reviews:escalate`

`SurveyInvitation.delivery_retryable` is computed server-side from the
distribution mode, delivery status, suppression state, response state, and
presence of the encrypted delivery secret. Console should use that boolean
instead of inferring retry eligibility from status labels alone.

`PreviewSurveyRecipients` is a read-only dry run for a selected campaign and
source sample. It uses the same trigger filter, deterministic sampling,
cooldown, daily-limit, recent-activity, auto-resolved, contact-eligibility, and
dedupe-policy rules as invitation creation, but returns only candidate counts,
eligibility state, suppression reason buckets, source identifiers, contact IDs,
display labels, recipient snapshots, and whether the selected distribution
channel is ready to deliver. It does not return raw email addresses, public
survey tokens, or encrypted delivery secrets, and it does not persist
invitations.

`SendSurveyTestEmail` is an audited admin action for campaign readiness. It
uses the active encrypted email sender and the registered email notification
adapter to render and send a clearly marked test survey email to the supplied
operator address. The service normalizes and validates the recipient address,
rejects archived campaigns, does not persist a survey invitation, does not
generate a response token, does not enqueue worker delivery, and does not
affect analytics denominators.

### Public service

```proto
service PublicSurveyService {
  rpc GetPublicSurvey(GetPublicSurveyRequest) returns (PublicSurvey);
  rpc SubmitPublicSurveyResponse(SubmitPublicSurveyResponseRequest) returns (PublicSurveyResponseReceipt);
}
```

HTTP routes:

- `GET /surveys/{token}`
- `POST /surveys/{token}/responses`
- `GET /v1/surveys/{token}`
- `POST /v1/surveys/{token}/responses`
- `POST /v1/surveys/provider-events/{tenant_id}/{sender_id}`

### Permission model

Add permissions:

- `surveys:campaigns:view`
- `surveys:campaigns:edit`
- `surveys:invitations:create_link`
- `surveys:invitations:retry_delivery`
- `surveys:provider_events:record`
- `surveys:responses:view`
- `surveys:responses:review`

Suggested defaults:

- admin: all survey permissions;
- member: responses view and review;
- viewer: responses view;
- service account: none unless explicitly configured.

## Service Behavior

### Campaign creation and validation

The service validates:

- name length;
- metric type;
- score scale and thresholds;
- campaign content version;
- dedupe policy;
- consent policy, email purpose, and legal basis;
- question and comment prompt length;
- sample rate bounds;
- limits and cooldowns;
- recent customer activity gate;
- referenced workflow states belong to the tenant;
- request statuses are in the known Customer Request status set;
- trigger rules are not empty when enabling a campaign.

The service seeds no enabled campaign automatically. It may expose disabled
templates for "CSAT after closed workflow state" and "CES after reply sent" in
Console so admins can enable them deliberately.

### Worker loop

The worker starts each tick by expiring a bounded batch of stale, unfinished
invitations with `FOR UPDATE SKIP LOCKED`, then runs recovery and delivery
loops:

1. Claim active low-score recovery reviews that need automatic SLA attention:
   overdue reviews, reviews missing a due time, critical reviews, and reviews
   that remain unowned after 24 hours. The claim is fenced with `claimed_at` and
   `claimed_by`, stale after 10 minutes, and uses `FOR UPDATE SKIP LOCKED` so
   multiple server replicas can run the policy without double-processing.
2. Enqueue owner recovery notifications for automatically escalated reviews when
   an accepted owner member has a usable email address. The notification queue is
   unique by tenant, response, owner, channel, and reason so repeat worker runs
   do not fan out duplicate emails.
3. Claim pending or failed owner recovery notifications and send them through
   the tenant active email sender. Missing sender configuration or missing owner
   email suppresses the notification with a visible reason; provider and
   transport failures remain retryable until the worker marks them dead.
4. Claim due survey delivery attempts and send email invitations. Hosted-link
   invitations do not need a delivery attempt unless an operator later requests
   email delivery for a contact-backed invitation.

These loops use bounded batches, owner markers, retry backoff or stale-claim
recovery where relevant, terminal dead state where delivery can exhaust, and
metrics following the request notification worker pattern.

Low-score recovery automation reuses the manual escalation rules: move the
review to `in_review`, promote severity to `critical`, preserve an already
stricter or overdue due time, append durable action evidence with
`automation=survey_recovery_worker`, and increment
`attune_survey_recovery_automation_total{tenant,result,reason}`. Owner
notification enqueue, send, suppression, retry failure, and dead-letter outcomes
increment `attune_survey_recovery_notification_total{tenant,result,reason}`.

Before a due email delivery is sent, the worker revalidates the invitation:

- campaign still exists, is enabled, and has not been archived;
- source feedback or request still exists and is visible for the campaign;
- source has not reopened when the trigger required a closed outcome;
- contact is still deliverable and has not opted out, bounced, complained, or
  become suppressed;
- campaign and contact send limits still allow delivery;
- invitation has not expired.

If revalidation fails, the worker marks the invitation as suppressed or expired,
marks the delivery attempt suppressed, records `last_revalidated_at`, and does
not call the email provider.

### Recent activity gates

Campaigns can suppress stale interactions with
`max_time_since_customer_activity_hours`. A value of `0` disables the gate. When
enabled, the trigger event must carry `customer_last_activity_at`. Missing or
old customer activity suppresses the invitation with
`stale_customer_activity`.

Campaigns can also suppress auto-closed or system-closed outcomes. This mirrors
support tools that avoid surveying a customer when the close event was a cleanup
task rather than a meaningful resolution.

### Provider events

Provider event handling is best-effort and idempotent by provider event ID,
explicit adapter key, or normalized payload hash. Provider callbacks are
accepted only through a sender-scoped signed webhook sink, and provider adapters
must normalize raw provider events into Attune's event vocabulary before calling
the survey state machine. The service stores a redacted provider snapshot,
synchronizes invitation delivery and open state, and updates contact
suppression state for bounce and complaint events.

Provider opens are diagnostics only. They may update invitation `opened_at` and
`response_status = opened` only when the invitation has not been submitted or
expired. They never create responses and never contribute to score metrics.

### Idempotency

Dedupe keys:

- trigger event: `(trigger_type, source object, transition revision or sent revision)`;
- invitation, `one_per_source`: `(campaign_id, source kind, source id, recipient key)`;
- invitation, `one_per_resolution`: `(campaign_id, source kind, source id, outcome_cycle_key, recipient key)`;
- invitation, `one_per_trigger`: `(campaign_id, trigger_event_id, recipient key)`;
- response: `(invitation_id)`;
- provider event: `(tenant_id, provider, provider_event_key)` when the provider
  exposes a stable event id or the adapter can derive a stable payload hash.

`recipient key` is `contact_id` for contact-backed invitations and the
source-link recipient hash for source-linked hosted invitations.

Public response submission is idempotent: after the first accepted response,
the same token returns the same receipt without changing the stored score or
comment.

### Expiry

Invitation TTL defaults to 14 days. Expired invitations cannot accept new
responses. Expiry updates status to `expired` and keeps the row for analytics.

### Sampling

Sampling uses a stable hash of `(tenant_id, campaign_id, source kind, source
id, contact_id)` against `sample_rate_basis_points`. That makes sampling
deterministic and explainable without storing raw random state.

### Low-score review creation

When a response is submitted and `low_score = true`, the service inserts a
`survey_low_score_reviews` row in the same transaction. The due time is
based on the default closed-loop SLA for the computed severity: critical in 24
hours, high in 48 hours, medium in 72 hours, and low in seven days. Severity
defaults from the score distance below the low-score threshold and can be
updated by operators. If a response is idempotently re-submitted, the service
does not create another review row.

## Console Experience

Add a Surveys area under the operations or insights navigation group.

### Campaigns

The campaign page includes:

- campaign list with metric, trigger summary, enabled state, response rate,
  last send time, and low-score count;
- create/edit dialog with CSAT/CES templates;
- trigger builder for workflow states, closed-category states, reply sent, and
  request statuses;
- scale preview;
- dedupe policy, hosted-link mode, recent-activity gate, delay, TTL, cooldown,
  sampling, tenant limit, and contact limit controls;
- recipient preview for a selected trigger sample, with localized eligibility,
  suppression, delivery readiness, and lifecycle reason labels;
- test-email send using the active sender/provider, with visible test content
  markers and no invitation persistence;
- explicit hosted-link creation for a selected feedback or Customer Request
  when the campaign allows source-linked invitations;
- disabled, enabled, and archived states.

### Analytics

The analytics page includes:

- response rate trend;
- daily trend buckets for invitations, submissions, low scores, response rate,
  average score, and invitation response states;
- segment diagnostics by source type, campaign, distribution mode, and trigger
  event, using invitation-cohort response rates, low-score rates, suppression
  rates, and attention scores;
- operational health insights for overdue, unassigned, critical, and
  pending-contact low-score reviews, low-score rate, response rate, suppression
  rate, expiry rate, and high-attention segments;
- low-score recovery command metrics for open, overdue, unassigned, critical,
  pending-contact, and oldest-due review work;
- owner recovery workload metrics with open, overdue, due-soon, critical,
  pending-contact, oldest-due, and workload-score fields so managers can see
  which assignees are carrying the riskiest recovery queue;
- low-score recovery assignment decisions, using candidate-owner workload,
  severity, and SLA pressure to pick an owner, set the review to in-review,
  preserve stricter existing due dates, and return the decision reason plus
  before/after workload scores;
- low-score recovery escalation decisions, using current SLA pressure, owner
  gaps, and severity to move selected reviews into in-review, promote them to
  critical severity, preserve already stricter due dates, and append
  escalation evidence to the internal action record;
- low-score recovery automation metrics, using bounded result and reason labels
  so operators can track system-escalated, skipped, and failed recovery actions
  without scraping internal notes;
- low-score recovery owner notification metrics, using bounded result and reason
  labels so operators can distinguish queued, sent, suppressed, retrying, and
  dead owner alerts;
- recovery focus queue counts for the same server-side slices used by the
  low-score drilldown controls;
- per-review recovery playbooks with SLA status, blocker reason, next-best
  action, and risk score;
- operational insights when active recovery reviews lack root-cause or
  action-taken evidence;
- campaign-health endpoint and Console card with pass/warn/fail checks for
  campaign state, delivery readiness, delivery backlog, response rate,
  suppression rate, expiry rate, and low-score recovery queue risk;
- campaign-health funnel: generated, pending, delayed, delivered, opened,
  submitted, suppressed, expired, rejected, low-score, and overdue-recovery;
- suppression reason breakdown;
- average response latency from invitation creation to customer submission;
- CSAT positive percentage and CES low-effort percentage;
- CES average score;
- normalized score trend;
- low-score count trend;
- open and overdue low-score review counts;
- filters for campaign, metric type, workflow state, source, request status,
  tag, and date range;
- table export-ready response rows with feedback/request links.

### Low-score drilldown

The low-score view includes:

- score and comment;
- campaign and metric;
- server-filtered and backend-prioritized low-score responses, so high-volume
  positive feedback or newer low-risk replies cannot push actionable low scores
  out of the visible queue before pagination;
- server-side recovery focus queues for SLA status and blocker reason,
  including overdue, unassigned, pending-contact, missing-root-cause, and
  missing-action work;
- server-side review-severity and owner-member filters, so critical recovery
  work and an individual owner's queue remain first-class even when the low
  score backlog grows past the first page;
- owner workload board for assigned recovery work, including the most pressured
  assignees and a low-load suggested next owner for newly assigned recovery
  items;
- selected-row smart assignment, so operators can let Attune distribute
  low-score recovery work across eligible members while retaining a decision
  trail for escalation and workload balancing;
- selected-row SLA escalation, so overdue or high-risk recovery work can be
  promoted to critical with a same-day recovery clock and durable action
  evidence before the queue loses context;
- automated escalation visibility, so playbooks show when the recovery worker
  has already applied the SLA policy and appended automation evidence;
- owner notification visibility, so playbooks show whether an automated
  recovery alert is queued, sent, retrying, failed, or suppressed with a
  readable reason;
- batch low-score review updates for selected queue rows, so operators can
  assign owners, escalate severity, start reviews, set due dates, or mark
  customer contact across a recovery slice without opening every response;
- response rows and their low-score review state loaded in one ordered query
  for the recovery queue;
- linked feedback;
- linked Customer Request;
- source, workflow state, and response latency;
- owner, severity, due time, and review status;
- active reviews sorted by overdue state, due time, and severity before terminal
  reviews;
- root cause and action taken fields;
- customer-contacted timestamp;
- actions to acknowledge, resolve, suppress, or add an internal note.

### Feedback and request detail

Feedback detail should show:

- whether a survey invitation was sent or suppressed;
- response score and comment if present;
- link to low-score review if present.

Customer Request detail should show aggregate survey response count, score
summary, and low-score comments linked to that request.

## Public Experience

The public survey page is intentionally small:

- tenant brand/display name;
- one rating question;
- one comment field;
- submit button;
- unsubscribe affordance for contact-backed invitations;
- expired or already-submitted states;
- hidden honeypot field so generic form bots do not create survey responses;
- noindex, no-referrer, clickjacking, MIME-sniffing, permissions, and CSP
  headers on the hosted HTML surface;
- no authenticated Console dependencies.

Accessibility requirements:

- radio-group semantics for score choices;
- clear focus order;
- no hover-only instructions;
- visible error summary for invalid score or comment;
- mobile layout without horizontal overflow;
- success state announced with `role="status"`.

## Privacy and Security

- Store only token hashes.
- Generate tokens with at least 128 bits of entropy.
- Never log raw tokens, raw email addresses, provider secrets, or response
  comments.
- Hash IP address and user agent when storing response metadata.
- Encrypt deliverable email payloads through existing secret primitives.
- Keep survey comments in tenant-scoped business tables covered by GDPR export
  and deletion.
- Include survey invitations, responses, low-score reviews, provider events, and
  recovery notifications in GDPR request/export job counts so operators can see
  the privacy blast radius before and after execution.
- Respect tenant-wide contact suppression, bounced and complained states, and
  survey-specific opt-outs.
- Include List-Unsubscribe headers in survey emails.
- Do not accept response submission on GET.
- Do not let public GET requests update `opened_at` or `response_status`.
- Reject or silently drop generic form-bot submissions that fill hidden trap
  fields before calling the response service.
- Serve hosted HTML with noindex, no-referrer, `X-Frame-Options: DENY`,
  `X-Content-Type-Options: nosniff`, restrictive `Permissions-Policy`, and a
  CSP that blocks third-party script, frame, and connection surfaces while
  allowing the same-origin survey form POST.
- Do not expose whether an email address exists from public routes.
- Rate limit public survey GET and POST routes by hashed client IP and hashed
  invitation token. Tenant-scoped portal routes include the tenant slug in the
  client bucket; bare survey-token routes do not require a database lookup before
  the limiter runs.
- Rate limit signed provider event callbacks by hashed client IP and hashed
  sender route identifiers before signature verification reaches the database.
- Verify provider event callbacks with sender-scoped HMAC-SHA256 signatures,
  require a timestamp header, and reject callbacks outside the replay window.
- Evict idle anonymous rate-limit buckets to prevent random token probes from
  creating unbounded process memory growth.
- Treat expired, invalid, and already-used tokens as generic public outcomes.

### Retention and deletion

Survey tables use restrictive foreign keys to prevent accidental orphaning, but
GDPR and tenant-deletion paths must explicitly delete survey data before deleting
the source feedback, request, or contact.

Deletion order for a subject erasure:

1. `survey_recovery_notifications`
2. `survey_low_score_reviews`
3. `survey_provider_events`
4. `survey_responses`
5. `survey_invitations`
6. contact, feedback, and request rows handled by their owning services

Response aggregates are computed from current rows rather than stored in a
separate rollup table for this issue, so deleting a response removes it from
future analytics without stale personal data.

GDPR exports include survey data as separate JSONL files:

- `survey_invitations.jsonl`
- `survey_responses.jsonl`
- `survey_low_score_reviews.jsonl`
- `survey_provider_events.jsonl`
- `survey_recovery_notifications.jsonl`

## Observability

Add metrics:

- `attune_survey_trigger_events_total{tenant,trigger_type,result}`
- `attune_survey_invitations_total{tenant,campaign,metric_type,status,reason}`
- `attune_survey_delivery_attempts_total{tenant,campaign,channel,result,failure_kind}`
- `attune_survey_responses_total{tenant,campaign,metric_type,low_score}`
- `attune_survey_response_latency_seconds{tenant,campaign,metric_type}`
- `attune_survey_funnel_total{tenant,campaign,metric_type,step,result}`
- `attune_survey_worker_claimed_total{worker,kind,result}`
- `attune_survey_low_score_reviews{tenant,status}`
- `attune_survey_recovery_automation_total{tenant,result,reason}`
- `attune_survey_recovery_notification_total{tenant,result,reason}`

Bound labels:

- tenant ID is already used in Attune metrics;
- campaign labels should use a bounded campaign metric key; raw campaign IDs are
  not emitted unless a later metrics policy explicitly accepts that cardinality;
- reason and failure_kind must use enum-like values.

Logs use `logext` and include tenant ID, campaign ID, invitation ID, trigger ID,
delivery ID, and status. Logs must not include raw comments, raw emails, raw
tokens, or provider response bodies.

## Audit

Add audit actions:

- `survey.campaign_create`
- `survey.campaign_update`
- `survey.campaign_archive`
- `survey.hosted_link_create`
- `survey.test_email_send`
- `survey.provider_event_record`
- `survey.invitation_delivery_retry`
- `survey.low_score_review_update`
- `survey.low_score_review_batch_update`
- `survey.low_score_review_assign`
- `survey.low_score_review_escalate`

Audit metadata should include IDs, status changes, trigger type, metric type,
counts, and non-sensitive delivery evidence for test survey emails such as
provider, sent timestamp, test-only marker, and whether an invitation was
persisted. Failed test survey emails should record only stable error categories
such as `validation`, `not_found`, `conflict`, `disabled`, or `internal`, plus
the same safety markers. Provider-event audit metadata should include only safe
delivery-state evidence such as provider, canonical event type, delivery status,
response status, suppression status, contact-suppression flag, payload-present
flag, provider-message-id-present flag, and provider-event-key-present flag.
Audit metadata should not include customer comments, raw email addresses,
provider response bodies, raw provider payloads, raw provider message IDs, raw
provider event keys, raw errors, or tokens.

## Error Handling

Public routes:

- invalid token -> generic 404-style survey unavailable response;
- expired token -> 410-style expired survey response;
- already submitted -> 200 with existing receipt;
- invalid score -> 400 `VALIDATION`;
- rate limited -> 429 `RATE_LIMITED`.

Console routes:

- missing campaign -> 404 `NOT_FOUND`;
- invalid workflow state or request status -> 400 `VALIDATION`;
- duplicate enabled campaign rule conflict -> 409 `CONFLICT`;
- unverified sender retry -> 409 `CONFLICT`;
- delivery retry for non-dead/non-failed attempt -> 409 `CONFLICT`.

## Alternatives Considered

### Reuse request notification events

Request notification events already have contacts, delivery rows, unsubscribe,
and rate limits. Reusing them would reduce table count, but it would force
survey-specific concepts into a request-update ledger: response tokens,
submitted scores, comments, low-score review state, and response-rate
denominators. The proposal reuses contact and sender foundations while keeping
survey measurement tables separate.

### Add CSAT fields directly to `user_feedback`

Inline fields such as `csat_score` and `csat_comment` would be quick for one
feedback row, but they fail for CES, Customer Requests, multiple campaigns,
multiple contacts, invitation suppression, response latency, and dedupe. A
campaign/invitation/response model is the smallest durable shape.

### Only send surveys after reply-send completion

Reply sent is a strong trigger, but some teams resolve feedback without using
Attune reply drafts. Workflow-closed triggers are required for acceptance.

### Only send surveys after workflow closed

Workflow closed captures lifecycle resolution, but reply-send completion is the
best signal for "the customer just received a reviewed response." Both triggers
are needed.

### Build a generic survey builder

A generic builder would handle arbitrary questions, branching, scoring, and
layout. It would also expand the issue far beyond post-resolution measurement.
CSAT and CES need one score and one comment. A metric-specific design meets the
issue while keeping the schema ready for adjacent measurement types.

### Submit score directly from email clicks

Email-embedded one-click scoring is attractive, but security scanners and
prefetchers can click links. The proposal uses score-prefilled hosted pages and
requires explicit POST submission.

### Store anonymous responses without invitations

Anonymous hosted surveys are useful for broad research, but #235 requires
responses to link back to relevant feedback or requests. Invitation tokens are
the safer anchor.

## Risks and Tradeoffs

### Response bias

Post-resolution surveys over-represent customers who read email and are willing
to respond. The proposal stores sent, suppressed, expired, and submitted counts
so response-rate context is visible.

### Noise from repeated contact

CSAT and CES can become irritating when every interaction triggers a survey. The
proposal requires cooldowns, contact daily limits, campaign dedupe, tenant send
limits, sampling, and opt-out.

### Link scanner false positives

Email scanners can open survey links. GET never submits a response and never
marks an invitation as opened. Opened timestamps come from trusted provider
events and are useful as diagnostics only; they should not be used as a success
metric.

### Token exposure

Hosted survey tokens are bearer credentials. Browser history, reverse proxies,
analytics scripts, and access logs can leak URL paths if they are not handled
deliberately. The public survey surface must disable third-party analytics,
redact tokens from logs, use no-store cache headers, and send a no-referrer
policy.

### Provider event ambiguity

Email providers do not report delivery, bounce, complaint, and open events with
identical semantics. The proposal stores provider events as diagnostics and uses
only bounce and complaint events for suppression. Score and response metrics are
based on submitted survey responses, not provider engagement events.

### Privacy exposure

Survey comments may contain personal data or sensitive customer details. The
proposal keeps comments out of logs and audit, stores comments in tenant-scoped
business tables, and requires GDPR export/delete integration.

### Schema size

The proposal adds several tables. This is deliberate because campaign config,
trigger events, invitations, responses, delivery attempts, preferences, and
low-score reviews have different retention and query patterns.

### Cross-service coupling

Workflow, reply draft, and Customer Request services need to record survey
trigger events. Using a small injected recorder interface avoids a hard
dependency on survey campaign logic from those services.

### Email infrastructure maturity

The survey system depends on configured customer notification email sender
support. When no verified sender exists, invitations are suppressed with
`email_sender_missing` or `email_sender_unverified` and hosted links remain
available for operator-driven delivery.

## Implementation Plan

1. Add this proposal and keep status `Proposed`.
2. Add `proto/attune/v1/survey.proto`, regenerate Go, TypeScript, and OpenAPI.
3. Add the survey migration with campaigns, triggers, trigger events,
   invitations, delivery attempts, responses, preferences, unsubscribe tokens,
   low-score reviews, campaign snapshots, decomposed invitation states, and
   hosted-link recipient mode.
4. Add `internal/repo/survey` with tenant-scoped CRUD, claim loops, response
   writes, analytics queries, and suppression lookups.
5. Add `internal/service/survey` with validation, trigger matching,
   eligibility, token generation, invitation creation, response intake, email
   delivery orchestration, campaign version snapshots, dedupe policies, recent
   activity gates, delayed-send revalidation, bounded stale-invitation expiry
   sweeps, provider event handling, recipient previews, test email sending,
   low-score review creation, and metrics.
6. Wire survey trigger recording into workflow transitions, reply-draft sent
   success, and request status changes through the injected recorder interface.
   Keep Console-default trigger filter keys such as `workflow_category` and
   `request_status` aligned with the server-side trigger attributes.
7. Add Console handlers for campaigns, explicit hosted-link creation,
   recipient preview, test email sending, provider-event recording, invitation
   delivery retry, invitations, responses, analytics, low-score review, and
   recovery focus filters.
8. Add public handlers for survey rendering, response submission, and survey
   unsubscribe.
9. Add Console features for campaign configuration, pre-send recipient
   previews, test email sending, analytics, low-score drilldown,
   campaign-health diagnostics, and feedback/request detail summaries.
10. Add recovery automation for overdue, due-missing, critical, and stale
    unowned low-score review work, including stale-claim fencing and bounded
    Prometheus outcome metrics.
11. Add retryable owner notifications for automatically escalated low-score
    recovery work, including duplicate suppression, provider failure state, and
    bounded Prometheus outcome metrics.
12. Extend audit action allowlists and i18n labels.
13. Extend GDPR export/delete paths for survey invitations, responses,
    low-score review notes, provider events, and recovery notifications,
    including request/export job count fields and Console request-history
    summaries.
14. Add dashboard panels and alertable metrics for survey delivery failures,
    low response rate, and low-score review backlog.
15. Update this proposal to `Implemented` when the implementation lands.

## Verification

Backend:

- Unit tests for campaign validation, trigger matching, eligibility,
  suppression reasons, hosted-link source invitations, invitation state
  decomposition, sampling determinism, cooldowns, dedupe policies, recent
  activity gates, delayed-send revalidation, stale-invitation expiry sweeps,
  campaign snapshots, provider event handling, recipient preview dry runs,
  previewed dedupe conflicts, previewed email-sender readiness,
  campaign-health delivery blockers and operational risk promotion,
  Console-default trigger filter aliases, test email rendering through the
  active sender/provider without invitation persistence,
  token hashing, response idempotency, low-score review creation, smart
  low-score assignment decisions, low-score SLA escalation evidence, recovery
  automation claim/skip behavior, owner notification enqueue, worker owner
  notification delivery, and analytics aggregation.
- Handler tests for every Console route and public route, including the audited
  survey test-email send route and the campaign-health diagnostic route.
- Repository tests for tenant isolation, composite foreign keys, claim loops,
  dedupe constraints, pagination, low-score filters, recovery automation SQL,
  recovery notification queue SQL, and recovery focus SQL.
- Integration tests under `test/integration/postgres/survey/` covering workflow
  transition -> invitation, reply sent -> invitation, response submission, and
  unsubscribe behavior.
- Integration tests covering contactless hosted-link creation, email delivery
  failure followed by successful hosted-link response, campaign edit after
  invitation creation, reopened source dedupe policy, and stale auto-close
  suppression.
- Integration tests covering accepted, temporarily delayed, opened, bounce, and
  complaint provider events synchronizing invitation state, avoiding duplicate
  event rows, suppressing shared contacts, and preventing local resends after a
  provider-delayed event.
- Integration tests covering signed provider webhooks with real encrypted sender
  config, HMAC verification, bounce suppression, and rejected invalid signatures
  that do not create provider event rows.
- Integration tests covering manual requeue for eligible delayed survey email
  invitations and refusal to retry suppressed invitations.
- GDPR tests ensuring survey comments, internal low-score notes, provider events,
  and recovery notification payloads export and delete correctly without touching
  another subject's survey response.

Console:

- Vitest coverage for campaign forms, trigger builder, analytics filters,
  campaign-health funnel, recipient preview form/result states, test email send
  actions, localized survey reason labels, low-score focus controls, owner
  filters, owner workload display, smart assignment actions, SLA
  escalation actions, batch recovery updates, automated escalation playbook
  visibility, low-score table actions, empty states, and error states.
- MSW route coverage for campaign CRUD, recipient preview, test email send,
  campaign health, invitation listing, analytics, response listing, low-score
  update, smart low-score assignment, and SLA escalation.
- Browser coverage for the Console survey route covering campaign-health
  diagnostics, recipient preview request payloads and results, test email send
  payloads and toast feedback, unhandled API detection, document overflow, and
  axe accessibility checks across desktop and mobile Chromium.

Public surfaces:

- Real-service browser smoke coverage for public survey pages through
  `cd console && pnpm run test:e2e:public-board`, including seeded survey
  campaigns and invitations, preselected score render, desktop and mobile
  overflow checks, axe accessibility checks under the survey CSP, low-score form
  submission, already-submitted reloads, and PostgreSQL response/review
  persistence assertions.

Contracts and gates:

- `make proto`
- `go test ./internal/repo/survey ./internal/service/survey ./internal/handlers/console/survey ./internal/handlers/portal`
- `go test ./internal/repo/gdpr ./internal/service/gdpr ./internal/handlers/console/gdpr`
- `go test ./internal/service/workflow ./internal/service/replydraft ./internal/service/customerrequest`
- `go test ./test/integration/postgres/gdpr -tags=integration`
- `go test ./test/integration/postgres/survey -tags=integration`
- `cd console && pnpm tsc -b --noEmit`
- `cd console && pnpm biome check`
- `cd console && pnpm vitest run --coverage`
- `cd console && pnpm exec playwright test --config playwright.config.ts e2e/accessibility/surveys.spec.ts --project=chromium-desktop --project=chromium-mobile`
- `cd console && pnpm run test:e2e:public-board`
- `scripts/lint-artifacts.sh --strict`
- `make ci-check`

## References

- [Zendesk satisfaction survey workflow](https://support.zendesk.com/hc/en-us/articles/4408887007386-Workflow-How-to-send-satisfaction-surveys-when-tickets-get-solved)
- [Intercom CSAT after conversation close](https://www.intercom.com/help/en/articles/11799242-configuring-and-sending-a-csat-survey-when-a-conversation-is-closed)
- [Intercom conversation ratings](https://www.intercom.com/help/en/articles/7872853-measure-customer-satisfaction-with-conversation-ratings)
- [Freshdesk CSAT module](https://support.freshdesk.com/support/solutions/articles/50000009790-the-new-csat-module-how-to-set-up-send-out-and-collect-responses)
- [Help Scout satisfaction ratings](https://docs.helpscout.com/article/386-gather-feedback-with-satisfaction-ratings)
- [Jira Service Management CSAT](https://support.atlassian.com/jira%2Dservice%2Dmanagement%2Dcloud/docs/what-are-customer-satisfaction-surveys-csats/)
- [Qualtrics Workflows](https://www.qualtrics.com/support/survey-platform/actions-module/setting-up-actions/)
- [Qualtrics email opt-out](https://www.qualtrics.com/support/survey-platform/distributions-module/email-distribution/emails-overview/)
- [Medallia closed-loop feedback framework](https://www.medallia.com/blog/closed-loop-feedback-program-try-this-framework/)
- [PostHog surveys](https://posthog.com/docs/surveys/creating-surveys)
- [PostHog survey results](https://posthog.com/docs/surveys/viewing-results)
- [Pendo NPS survey setup](https://support.pendo.io/hc/en-us/articles/43643936689691-Set-up-an-NPS-survey)
- [Pendo survey responses](https://support.pendo.io/hc/en-us/articles/44433686595099-View-survey-responses)
- [Sprig in-product surveys](https://docs.sprig.com/docs/sprig-studies/in-product-surveys)
- [Sprig survey diagnostics](https://docs.sprig.com/docs/sprig-studies/in-product-surveys/survey-diagnostics-funnel)
- [Hotjar surveys with user attributes](https://help.hotjar.com/hc/en-us/articles/36820025646609-How-to-Use-User-Attributes-with-Surveys)
- [Canny status change emails](https://help.canny.io/en/articles/1291127-status-change-emails)
- [Productboard portal card updates](https://support.productboard.com/hc/en-us/articles/360058173353-Close-the-feedback-loop-with-Portal-card-updates)
- [UserVoice public status updates](https://help.uservoice.com/hc/en-us/articles/360034984834-Public-Status-Updates-on-Ideas)
- [Aha portal notification emails](https://support.aha.io/aha-roadmaps/support-articles/ideas/portal-notification-emails~7444635037496648198)
- [Linear Customer Requests](https://linear.app/docs/customer-requests)
- [Genesys Cloud customer surveys](https://help.mypurecloud.com/articles/about-customer-surveys/)
- [Talkdesk IVR survey setup](https://support.talkdesk.com/hc/en-us/articles/4411041346459-Talkdesk-Feedback-Setting-up-an-IVR-Survey)
- [Aircall post-call survey](https://support.aircall.io/en-gb/articles/11412500885405)
