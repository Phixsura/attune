# Zapier connector

attune ships a Zapier integration (`integrations/zapier/`) built on the
automation API surface added in #234: instant REST-hook triggers on feedback
and customer-request events, and actions for feedback, requests, tags, and
notes — all over API-key auth, no Console session.

## Connector auth

1. In attune Console → **Settings → API Keys**, create a key with the scopes
   the connector needs:
   - `hooks:manage` — trigger subscriptions (`/v1/hooks`)
   - `ingest:write` — Create Feedback action
   - `requests:read`, `requests:write` — request actions
   - `tags:write` — Add Tag action
2. In Zapier, connect attune with two fields: your **base URL**
   (e.g. `https://attune.example.com`) and the **API key**. Zapier validates
   the connection via `GET /v1/auth/verify` and labels it with your
   workspace name.

Scopes `hooks:manage`, `requests:read`, and `requests:write` are
**explicit-grant**: legacy keys created without scopes do not get them
implicitly.

## Triggers (all instant)

| Trigger | Event | Fires when |
|---|---|---|
| New Feedback | `feedback.created` | a feedback item is ingested and enriched |
| New Urgent Feedback | `feedback.urgent` | the enriched item is classified urgent |
| New Customer Request | `request.created` | a customer request is created |
| Request Status Changed | `request.status_changed` | a request's status transitions |

Mechanics: turning a Zap on calls `POST /v1/hooks` with the Zap's target URL
and the event type; turning it off calls `DELETE /v1/hooks/{id}`. Deliveries
are HMAC-SHA256 signed (`X-Attune-Signature`, content-hash scheme) and carry
`X-Attune-Delivery-Id` for at-least-once dedup. If the hook URL answers HTTP
410, attune disables the subscription and stops sending. Subscriptions are
capped at 25 per workspace.

The same events are available to any consumer — `POST /v1/hooks` with
`consumer: "generic"` is the vendor-neutral subscription API;
`GET /v1/hooks/samples/{event_type}` returns recent envelopes
(schema-identical to live deliveries) for testing.

## Actions

| Action | Endpoint | Notes |
|---|---|---|
| Create Feedback | `POST /v1/feedback/ingest` | enriched asynchronously |
| Update Request Workflow | `PATCH /v1/requests/{id}` | status: open / planned / in_progress / shipped / cancelled |
| Add Tag to Feedback | `POST /v1/feedback/{id}/tags` | tag must exist |
| Add Note to Request | `POST /v1/requests/{id}/notes` | `visibility`: `internal` (default) or `public` — public notes flow through portal moderation |

All actions are audited with the API key as actor.

## Sample recipes

- **Zendesk ticket → attune feedback**: Zapier's Zendesk "New Ticket"
  trigger → attune *Create Feedback* (map subject+description to content).
- **Urgent feedback → Slack DM**: attune *New Urgent Feedback* →
  Slack "Send Direct Message" with `feedback.enriched.title` and content.
- **Typeform → customer request**: Typeform "New Entry" → attune
  *Update Request Workflow* or a create-request Zap via *Create Feedback* +
  Console promotion, or directly `POST /v1/requests` with the API Request
  action.
- **Request shipped → customer email**: attune *Request Status Changed*
  (filter `request.status = shipped`) → Gmail/SendGrid send email using
  `request.title` and `request.display_id`.

## Error contract

| Status | Meaning | Zapier behavior |
|---|---|---|
| 401 | invalid/revoked key | prompts the user to reconnect |
| 403 | missing scope (message names it) | run errors with the fix |
| 409 | idempotency/limit conflict | run errors; adjust the Zap |
| 429 + `Retry-After` | rate limited | waits and retries automatically |

## Local validation

```bash
cd integrations/zapier && npm install
npm test                                   # nock-mocked
ATTUNE_LIVE_BASE_URL=http://127.0.0.1:8080 \
ATTUNE_LIVE_API_KEY=fbk_live_... npm test  # against a local stack
```

Publishing the app to the Zapier directory (zapier push, review) is a
separate release step and needs a Zapier platform account.
