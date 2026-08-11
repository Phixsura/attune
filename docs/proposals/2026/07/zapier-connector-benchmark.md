# Zapier / webhook connector benchmark — 72 products (2026-07-29)

Competitive benchmark backing the Zapier connector proposal
(`2026-07-29-zapier-connector.md`, issue #234). Method: parallel web survey
of the Zapier app directory + official developer docs across 72 products in
9 categories (feedback, PM, support, analytics, dev/ops, CRM, forms,
commerce, collaboration, OSS peers); 66/72 rated high-confidence; unknowns
left blank rather than guessed. Raw corpus:
[`zapier-connector-benchmark.json`](zapier-connector-benchmark.json).

## Quantitative landscape

| Metric | Result |
|---|---|
| Official Zapier app | **64 / 72** |
| Trigger mechanism (of 57 with known mechanism) | **28 all-instant**, 19 mixed, 10 polling-only |
| Zapier auth | 39 OAuth2, **25 API key/token** |
| Programmatic webhook-subscription API | **34 programmatic**, 35 dashboard-only/unknown |
| Per-subscription event-type filtering | **57 / 72** |
| HMAC/SHA webhook signatures | **60 / 72** |

All 11 surveyed feedback-category competitors with a Zapier app (Canny,
Productboard, Pendo, Sprig, Featurebase, Frill, Nolt, Upvoty, Sleekplan,
featureOS, Savio) ship one; the leaders (Canny, Frill, Featurebase) are
all-instant REST hooks. API-key auth is the norm in the feedback category
(Canny, Frill, Nolt, Upvoty, Sleekplan) — OAuth2 dominates only in the
big-platform categories (CRM, PM, collaboration).

## Design patterns among the leaders

- **Canonical subscription shape**: `POST /api/vN/webhooks` accepting
  `{url, events[]}` → `{id}`; `DELETE /webhooks/{id}`. Seen at GitHub
  (`POST /repos/{o}/{r}/hooks` with `events[]`), Stripe
  (`POST /v1/webhook_endpoints` with `enabled_events[]`), Intercom, Front,
  Shopify, Calendly, PostHog, Cal.com, Novu, Chatwoot, and the feedback
  peers that expose one.
- **Event vocabulary**: dot-namespaced `entity.verb` dominates
  (`charge.succeeded`, `conversation.user.created`, `issue.created`,
  `post.created`); vocabularies grow append-only — no surveyed product
  renames a live event token.
- **Signatures**: HMAC-SHA256 over the body with a vendor header
  (`X-Hub-Signature-256`, `Stripe-Signature`, `X-Canny-Signature` …) in
  60/72. Ten products (Canny, Zendesk, Linear, Front, Crisp, Chatwoot,
  Upvoty, Featurebase, HubSpot, Mailchimp) fold a **timestamp** into the
  signed material for replay resistance.
- **Retry/disable**: exponential backoff for hours-to-days, then
  auto-disable after sustained failure; explicit 410-stops documented by
  GitHub, Stripe, and Zapier's own contract.
- **Dedup**: a stable delivery/event id header or body field
  (`X-GitHub-Delivery`, Stripe `event.id`) is the norm.
- **Zapier feedback-category surface**: new post/feedback, status change,
  and new comment/vote triggers; create-post and find actions; instant
  hooks; per-board/source trigger filters where the product has boards.

## Validation of attune's design against the corpus

**Confirmations** (design matches the dominant pattern):

- All-instant REST hooks — matches the 28 all-instant leaders incl. every
  feedback-category leader (Canny/Frill/Featurebase).
- `POST /v1/hooks {target_url, event_types[]}` → `{id}` /
  `DELETE /v1/hooks/{id}` — the canonical shape (GitHub/Stripe/Intercom…).
- Per-subscription `event_types[]` filter — 57/72 do exactly this.
- Dot-namespaced append-only vocabulary — universal convention.
- HMAC-SHA256 signature + stable delivery-id header — 60/72 sign, GitHub/
  Stripe-style dedup id.
- API-key auth with self-serve keys — publishable and the feedback-category
  norm; base-URL connection field mirrors PostHog/Chatwoot (self-hosted).
- 410 auto-disable + backoff-then-dead — GitHub/Stripe/Zapier contract.

**Deviations** — none load-bearing. attune's per-subscription secret is
*stronger* than the shared-secret norm (only Stripe consistently does
per-endpoint secrets); the 25-per-tenant cap is stricter than most (Zendesk
caps at 10 targets, GitHub at 20 hooks/repo — same order of magnitude).

**Gaps** (things multiple leaders do that v1 omits):

| Gap | Products | Verdict |
|---|---|---|
| Test/ping delivery endpoint (`POST /hooks/{id}/ping`) | GitHub, ClickUp, Intercom, Chatwoot, Height, Productboard… | **DEFER** — notify-targets already has `/test`; hooks samples endpoint covers the Zap-editor need; cheap follow-up |
| Timestamp folded into the signature (replay resistance) | Canny, Zendesk, Linear, +7 | **DEFER** — attune's `v2-content-hash` scheme is shared across all outbound webhooks; changing it is a cross-cutting signing-scheme decision, not a Zapier-scoped one |
| Event-catalog endpoint (`GET /hooks/events`) | Typeform (rare) | **REJECT** — vocabulary is 4 tokens, documented; error messages list valid values |
| Subscription expiry / auto-resubscribe | Microsoft Graph (outside corpus), rare elsewhere | **REJECT** — Zapier handles resubscribe on its own; adds state for no consumer |

**ADOPT-NOW: none.** The implemented design already covers every pattern
the corpus marks as table-stakes; the two DEFERs are tracked as follow-ups
in the proposal.

## Appendix — full corpus

See [`zapier-connector-benchmark.json`](zapier-connector-benchmark.json)
(72 records: per-product Zapier trigger/action counts, instant vs polling
split, auth scheme, subscription endpoint shape, event-string examples,
signature scheme, retry/disable policy, dedup mechanism, sources, and
confidence).
