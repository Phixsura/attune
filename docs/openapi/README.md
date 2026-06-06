# attune public OpenAPI

`openapi.yaml` is generated from `proto/attune/v1/*.proto` via the
`buf.build/community/google-gnostic-openapi` plugin (see `buf.gen.yaml`).
Do not edit it by hand — change the `.proto` and re-run `make proto`.

## Error envelope (not in the generated spec)

Every error from a `/fb/v1/*` endpoint shares one shape, defined in
`proto/attune/v1/common.proto`:

```yaml
ErrorResponse:
  type: object
  required: [code, message]
  properties:
    code:
      type: string
      description: stable machine-readable category (e.g. "bad_request",
        "unauthorized", "conflict", "body_too_large", "internal")
    message:
      type: string
      description: human-friendly Chinese explanation
    requestId:
      type: string
      description: chi RequestID, echoed for support triage. Optional —
        may be empty if the middleware didn't run (e.g. a 404 outside
        the mounted router).
```

The `gnostic` generator only includes messages that are referenced from
an RPC's request/response/path/query — `ErrorResponse` is the response
shape for **all non-2xx replies** rather than a typed return of any
specific RPC, so it does not surface in the generated `openapi.yaml`.
This README is the supplementary contract: clients should parse error
bodies against the schema above regardless of which endpoint produced
them.

The `requestId` value matches the `X-Trace-Id` response header (set by
the chi RequestID middleware) and the `trace_id` field on internal
structured logs (slog handler in `internal/infra/observability/`), so
operators can correlate end-to-end.

## OAuth install endpoints (intentionally outside the spec)

`POST /fb/v1/console/install/start` and `GET /fb/v1/console/install/callback`
are 302-redirect endpoints (Lark OAuth handshake) — they're tied to the
browser flow, never called by an API consumer, and don't fit a proto
RPC model. They're routed in `internal/handlers/console/oauth.go` and
documented in the source.
