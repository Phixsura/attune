# attune public OpenAPI

`openapi.yaml` is generated from `proto/attune/v1/*.proto` via the
`buf.build/community/google-gnostic-openapi` plugin (see `buf.gen.yaml`), then
mechanically repaired by `internal/tools/openapipatch` so the published
document matches attune's real HTTP error contract and public API version
headers while dropping generator-only comments and external documentation links.
Do not edit it by hand — change the `.proto` and re-run `make proto`.

## Error envelope

Every non-2xx HTTP response from attune's product APIs uses the shared
`ErrorResponse` JSON envelope from `proto/attune/v1/common.proto`. The
post-processing step ensures the schema appears in
`#/components/schemas/ErrorResponse` and that default JSON error responses point
to it instead of `google.rpc.Status`.

```yaml
ErrorResponse:
  type: object
  required: [code, message]
  properties:
    code:
      type: string
      description: stable machine-readable category (ErrorCode enum name)
    message:
      type: string
      description: human-facing explanation; not stable
    requestId:
      type: string
      description: request id for support / log correlation
```

Clients should parse error bodies against this shape regardless of which
endpoint produced them.

## API version header

The API-key product surface keeps stable `/v1/...` paths, but callers may pin a
supported date-based contract with the optional
`X-Attune-Api-Version: 2026-06-28` request header.

- If the header is omitted, the server uses its current default and echoes the
  effective version back in the `X-Attune-Api-Version` response header.
- If a caller pins a deprecated-but-still-supported version, the response may
  also include standard `Deprecation` and `Sunset` headers.
- Unsupported version values fail with the shared `ErrorResponse` envelope.

`internal/tools/openapipatch` injects that request-header parameter and the
matching response-header docs across the generated public `/v1/...` operations
so the published OpenAPI stays aligned with the runtime contract.

## OAuth install endpoints (intentionally outside the spec)

`POST /fb/v1/console/install/start` and `GET /fb/v1/console/install/callback`
are 302-redirect endpoints (Lark OAuth handshake) — they're tied to the
browser flow, never called by an API consumer, and don't fit a proto
RPC model. They're routed in `internal/handlers/console/oauth.go` and
documented in the source.
