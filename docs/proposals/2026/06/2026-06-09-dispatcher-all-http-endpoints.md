# Complete remaining dispatcher endpoint migration

| | |
|---|---|
| **Issue** | #99 |
| **Status** | Implemented |
| **Started** | 2026-06-09 |
| **Related** | #71, #98, #19, #66 |

## Problem

#98 introduced `internal/dispatcher` as a typed proto handler loop and
migrated the first 18 product endpoints. Its proposal deliberately left
auth, inbound, webhook, and health routes native while the dispatcher proved
out. Issue #99 closes that migration gap for the remaining attune-owned
endpoints: auth and inbound are no longer carve-outs, but the dispatcher should
keep the same #98 shape rather than grow into a generic HTTP framework.

`main` still has endpoint handlers and middleware writing HTTP responses directly through
`respond.Error`, `respond.Proto`, `respond.ErrorWithExtra`, `WriteHeader`, or
`Write`.

That leaves two response paths:

- dispatcher-bound handlers with shared decode, envelope, status, and logging
  behavior;
- native handlers/middleware that can drift from the shared contract.

For #99 we want the stricter end state for attune-owned routes: every remaining
endpoint runs through `dispatcher.Bind` or a narrow dispatcher-owned equivalent.
`internal/respond` stays as the low-level protoJSON/envelope encoder, but
production handlers and middleware should not call it directly.

## Goals

- Migrate the #99 auth and inbound source endpoints to an option-driven
  `dispatcher.Bind(where, Input, handler, opts...)` contract, so auth,
  extra binding, and pre-handler checks are all declared at the route boundary.
- Preserve #98's core definition: `Bind` is proto request → proto response,
  with typed `RequestContext[Auth]`, shared decode/error envelope/logging, and
  route-local binders.
- Give middleware a dispatcher-owned rejection helper so pre-handler failures
  share the same envelope path as dispatcher-bound handlers.
- Support the remaining narrow response cases without creating a broad DSL:
  cookie side effects, middleware rejection envelopes, body-based
  webhook auth, and `/healthz`.
- Add an audit gate that prevents production code outside dispatcher/respond
  from reintroducing direct HTTP response emission.

## Non-goals

- Replacing chi middleware with dispatcher route handlers.
- Changing protobuf request/response shapes.
- Moving Prometheus `/metrics` behind dispatcher; it is a third-party scrape
  handler with its own format.
- Reworking service/repo/domain behavior.

## Proposal

Keep the #98 dispatcher loop intact and add only the capabilities required by
the #99 endpoint list. This proposal supersedes #98's middleware carve-out only
for HTTP rejection emission: middleware may call `dispatcher.Reject`, but it
does not use `Bind`, `RequestContext`, or dispatcher result types.

1. `dispatcher.Bind` accepts functional route options. The current options
   cover auth, extra binders, and pre-handler checks. Webhook uses the same
   `WithAuth` option as context-authenticated routes; its authenticator also
   fills the typed proto request while verifying HMAC, so the source row used
   for verification is the same source row exposed as
   `RequestContext[webhookAuth].Auth`. `BindOption` is request-typed
   (`BindOption[Req]`) instead of auth-typed, so future non-auth dispatcher
   options do not carry an unrelated `Auth` generic.
2. `dispatcher.Reject` for middleware and other pre-handler rejection paths
   that cannot be expressed as a full `Bind` route. This helper writes the
   standard `ErrorResponse` envelope via dispatcher-owned code; middleware
   owns branch-specific rejection logging.
3. `dispatcher.HealthzHandler` as an intentionally fixed health-probe helper.
   It does not widen `Result[Resp]`; product routes stay proto-shaped.
4. `RequestContext` exposes cookie side effects through `SetCookie` only. It
   does not expose the underlying `http.ResponseWriter`, so handler response
   body/status emission stays owned by dispatcher.

After that:

- `session.RequireSession`, `apikey.Middleware`, and rate-limit middleware call
  dispatcher rejection helpers instead of `respond.Error`.
- Rate-limit rejection keeps the standard `RATE_LIMITED` envelope and derives
  `Retry-After` from the tenant token bucket instead of a fixed constant.
- Console auth, change-password, inbound source management, and webhook ingest
  route through `dispatcher.Bind`.
- Console inbound test-connection now rejects malformed JSON with the standard
  dispatcher `400 BAD_REQUEST` envelope instead of returning `200 ok=false`.
  attune has not shipped a stable release with this console surface yet, so we
  prefer the simpler contract over preserving pre-release behavior. The console
  test-connection UI displays the server envelope message for these failures.
- Webhook HMAC verification uses `Bind(..., WithAuth(...))`, so the source row
  used for HMAC verification is the same source row used by the ingest handler.
- `/healthz` uses `dispatcher.HealthzHandler`; it remains outside the typed
  proto `Bind` loop by design.
- `respond` remains importable only by dispatcher/respond-owned code.

## Alternatives Considered

- **Middleware keeps using `respond.Error`.** Rejected. It preserves the same
  envelope today, but leaves middleware as a permanent second response-emission
  path that can drift from dispatcher behavior.
- **Binder side-data (`CustomWithState`).** Rejected. It avoids one extra
  webhook source lookup and preserves test-connection's pre-release invalid-JSON
  behavior, but it adds a second input channel beside the proto request.
- **Ad-hoc top-level error extras.** Rejected. Returning fields beside
  `{code,message,requestId}` reopens envelope drift; grace-window rotation now
  uses only the standard error envelope.
- **Proto health response.** Rejected. Existing deploy probes expect plain
  `ok`; a fixed `HealthzHandler` can own text emission without changing the
  product `Bind` contract.
- **A generic HTTP DSL.** Rejected. The needed surface is small and concrete;
  broad DSL primitives would obscure the product handlers.

## Risks / Tradeoffs

- The webhook authenticator is more complex than ordinary JSON/path binding
  because it performs unauthenticated body limiting, source lookup, HMAC
  verification, request filling, and metrics before the handler runs. The
  unified `WithAuth` option keeps the authenticated source explicit instead of
  using hidden binder-to-handler state, while avoiding a family of future
  `BindWithXxx` entrypoints.
- A strict audit gate can false-positive on tests or third-party handlers; the
  gate should scan production Go files and explicitly allow dispatcher,
  respond, and known non-attune response owners.
- Middleware remains native chi middleware, but dispatcher owns the final
  rejection envelope write. Middleware may still log branch-specific details
  before calling the helper.
- Handler cookie writes remain possible through `RequestContext.SetCookie`, but
  exposing the full response writer is intentionally rejected because it would
  let handlers bypass dispatcher-owned status/body emission.

## Implementation Plan

1. Add dispatcher typed body-auth binding, middleware rejection helper, and
   fixed health handler with unit tests.
2. Migrate session/API-key/rate-limit middleware to dispatcher-owned errors
   and cover the rate-limit envelope regression.
3. Migrate console auth and change-password handlers.
4. Migrate console inbound source handlers.
5. Migrate webhook adapter and `/healthz`.
6. Add/update endpoint smoke tests and router inventory tests.
7. Add a response-emission audit script to `scripts/check.sh`.
8. Narrow `RequestContext` side effects to cookies and cover webhook authenticated
   source handoff so the handler cannot regress to a second source lookup.
9. Cover API-key rejection bodies for raw-secret non-leakage and make
   rate-limit retry guidance match the bucket state.

## Verification

- `go vet ./...`
- `go build ./...`
- `go test ./...`
- `bash scripts/check.sh`
- Audit grep: no production direct response emission outside dispatcher/respond.

## References

- `docs/proposals/2026/06/2026-06-09-http-dispatcher.md`
- `proto/attune/v1/session.proto`
- `proto/attune/v1/inbound_source.proto`
