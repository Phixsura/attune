# attune Zapier integration

Zapier Platform CLI app for [attune](https://github.com/Phixsura/attune) (#234).
Not part of the main build — own `package.json`, tested with Node's built-in
test runner.

## Surface

| Kind | Key | attune API |
|---|---|---|
| Trigger (instant) | `new_feedback` | REST hook on `feedback.created` |
| Trigger (instant) | `urgent_feedback` | REST hook on `feedback.urgent` |
| Trigger (instant) | `new_request` | REST hook on `request.created` |
| Trigger (instant) | `request_status_changed` | REST hook on `request.status_changed` |
| Action | `create_feedback` | `POST /v1/feedback/ingest` |
| Action | `update_request` | `PATCH /v1/requests/{id}` |
| Action | `add_tag` | `POST /v1/feedback/{id}/tags` |
| Action | `add_note` | `POST /v1/requests/{id}/notes` (internal or moderated public) |

All triggers use the REST-hook contract: subscribe `POST /v1/hooks` (stores
the Zap's `targetUrl`, returns `{id}`), unsubscribe `DELETE /v1/hooks/{id}`,
performList fallback `GET /v1/hooks/samples/{event_type}`. Deliveries are
HMAC-signed (`X-Attune-Signature`) and deduplicated via
`X-Attune-Delivery-Id`; the Zapier item id is `entityId-eventType`.

## Auth

Custom (API key) auth with two connection fields: the attune base URL
(self-hosted OSS) and an API key carrying `hooks:manage`, `ingest:write`,
`requests:read`, `requests:write`, and `tags:write`. Connection test:
`GET /v1/auth/verify`; the label shows the workspace name.

## Development

```bash
npm install
npm test                      # nock-mocked integration tests
ATTUNE_LIVE_BASE_URL=http://127.0.0.1:8080 \
ATTUNE_LIVE_API_KEY=fbk_live_... npm test   # against a running local stack
npx zapier validate           # structural checks (needs zapier login for push)
```

Publishing to the Zapier platform (zapier push / directory listing) is a
separate release step — see `docs/integrations/zapier.md`.
