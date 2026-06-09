# Typed HTTP dispatcher — Spring Web-style bind, generic `RequestContext[Auth]`

| | |
|---|---|
| **Issue** | #71 |
| **Status** | Implemented |
| **Started** | 2026-06-09 |
| **Related** | #19 / #67 (proto IDL contract — the wire shapes the dispatcher encodes/decodes), #66 (inbound adapter framework — lark event lives there, not here), #48 (logext facade — the dispatcher consumes it for built-in observability) |

## Problem

PR #67 landed the proto IDL contract and the unified `ErrorResponse` envelope. While reviewing the result a structural pattern surfaced — **every product HTTP handler is overwhelmingly boilerplate**, and the apikey-middleware envelope leak that PR #67 sealed after-the-fact grew in exactly that boilerplate.

**Quantified against `main` post-#67** (each file `wc -l`'d, business-vs-boilerplate split by hand, no estimation):

| File | LOC | Boilerplate % | Notes |
|---|---|---|---|
| `internal/handlers/console/notifytarget/notify_targets_write.go` (`Delete`) | 26 | **92%** | 2 business lines (`repo.Delete` + status) |
| `internal/handlers/console/notifytarget/notify_targets_patch.go` (`Patch`) | 90 | **86%** | UUID parse + decode + get + merge + update spread over 5 error branches |
| `internal/handlers/ingest.go` (`Ingest`) | 57 | **88%** | 3 business lines (`ingestor.IngestRow`) |
| `internal/handlers/console/apikey/api_keys.go` (`List`) | 18 | **89%** | 1 business line (`svc.List`) |
| `internal/handlers/console/feedback/feedback.go` (`Stats`) | 57 | **39%** | aggregation-heavy — the floor case |
| **Aggregate** (13 main files, 2,018 LOC, 20+ endpoints) | 2,018 | **~68% weighted** | issue #71 quoted 56% — actual is higher |

The repeated template is **decode → 1 MiB cap → session/apikey extract → validate → call → `errors.Is` map to status → `respond.Error` → entry/exit `logext`**. 4xx maps and 5xx logging are inlined per call-site; one missed `respond.Error` is exactly the bug PR #67 chased.

**The fix is a typed dispatcher**, Spring Web in spirit (`@RequestBody` + `ResponseStatusException`) but Go-shaped (generics + explicit helpers, no annotations).

## Goals

- **Handler business density ≥ 60%** (vs ~32% today, weighted) — boilerplate moves into one 280-LOC framework package.
- **Wire envelope byte-identical in shape and status semantics** to today — `{code, message, requestId}` envelope shape preserved; response statuses preserved per branch, including `204` no-content paths; same protojson `EmitUnpopulated=true` behavior. **One deliberate exception**: `ErrorResponse.code` values normalize from previous lower_snake strings to `ErrorCode` enum names (`"VALIDATION"`, `"BAD_REQUEST"`, …). The protobuf field stays `string` for compatibility; see Decision 9 / §"Error code IDL contract".
- **Compile-time auth type safety per endpoint** — a console handler cannot accidentally read an apikey field, an ingest handler cannot accidentally read a session field; the mismatch is a build error, not a runtime nil.
- **Phased rollout with a small proving slice.** The dispatcher must support the full canonical product API, but endpoints migrate in reviewable groups so tests can prove each class of binding (empty request, body decode, path param, 204) before the higher-coupling handlers move.
- **Service / repo / domain / infra layers untouched.** Lower layers see only `context.Context` and explicit args (`tenantID`, `userID`, …) exactly as today. CLAUDE.md §5 invariant intact.
- **Observability strengthened, not weakened** — the dispatcher itself emits the entry/exit `logext` triple (info/warn/error per branch) so 5xx-without-stack-log becomes structurally impossible.

## Non-goals

- **Connect-RPC / gRPC.** Out of scope; requires a new wire path scheme, a new console SDK, and has no escape hatch for OAuth/healthz/lark. Tracked as a possible Phase 2 if/when the dispatcher proves out (see Alternatives).
- **Direct adoption of Huma.** Considered seriously and rejected (Alternative §A4) — its `application/problem+json` envelope and `struct → OpenAPI` reflection conflict with attune's existing envelope and the proto-IDL truth-source rule (CLAUDE.md §11).
- **RFC 7807 / 9457 envelope migration.** attune's current `{code, message, requestId}` is an AIP-193 minimal form; aligning to RFC 9457 is a clean future minor bump but not coupled to dispatcher.
- **Switching observability to a hook model.** logext is a hard convention (CLAUDE.md §7); the dispatcher hard-defaults it and exposes an opt-out, not the inverse.
- **Service / repo refactor.** Lower layers stay byte-identical. The dispatcher is strictly a handler-layer rewrite.
- **DSL-first codegen (Goa-style).** Proto already is attune's second IDL layer; a third (route DSL) is rejected.
- **Lark event endpoint / OAuth callback / `/healthz` / `dev-login`.** These five endpoints are carve-outs — see Scope.

## Scope — endpoints targeted

The product HTTP API on `main` is **23 endpoints**; **18 migrate to the dispatcher**, **5 stay native chi as carve-outs**.

| # | Method | Path | Auth | Migrates |
|---|---|---|---|---|
| 1 | GET | `/fb/v1/console/me` | session | ✅ |
| 2 | POST | `/fb/v1/console/logout` | session | ✅ |
| 3–5 | GET/POST/DELETE | `/fb/v1/console/api-keys[/{id}]` | session | ✅ |
| 6–10 | GET/POST/PATCH/DELETE/POST | `/fb/v1/console/notify-targets[/{id}[/test]]` | session | ✅ |
| 11–13 | GET | `/fb/v1/console/feedback[, /{id}, /stats]` | session | ✅ |
| 14 | GET | `/fb/v1/console/usage` | session | ✅ |
| 15–17 | GET/PUT/POST | `/fb/v1/console/enrich-config[, /preview]` | session | ✅ |
| 18 | POST | `/v1/feedback/ingest` | apikey | ✅ |
| C1 | GET | `/healthz` | none | **carve-out** — k8s probe reads HTTP status only; proto-ifying buys nothing |
| C2 | POST | `/v1/lark/event` | lark-signature | **carve-out** — foreign event format (#66 / CLAUDE.md §11) |
| C3 | GET | `/fb/v1/console/install/start` | none | **carve-out** — OAuth state machine, form-encoded + 302 redirect |
| C4 | GET | `/fb/v1/console/install/callback` | none | **carve-out** — same |
| C5 | GET | `/fb/v1/console/install/dev-login` | none | **carve-out** — HTTP-only test loop, form-encoded |

The carve-outs share one property: their HTTP shape is **not `proto.Message in → proto.Message out`**. Forcing them through the dispatcher would burn 100% of the escape hatches Huma needs to support them; carving them out keeps the dispatcher's contract pure.

Issue #71 quoted "11 endpoints"; the true count is **18 in-scope + 5 carve-out**. Both are corrected in this proposal.

Implementation checkpoint (2026-06-09): all 18 in-scope endpoints now route through `internal/dispatcher`. The five carve-outs remain native `chi` because their HTTP shape is not `proto.Message in → proto.Message out`.

Handler package organization follows **one endpoint per file**. Shared package-level helpers (handler structs, narrow service/repo interfaces, DTO conversion, validation helpers) stay in the package root file; each HTTP method lives in its own file such as `api_keys_create.go` or `notify_targets_list.go`.

## Design

### Architecture

```
                            chi.Router
                                │
            ┌───────────────────┴─────────────────────┐
            │                                         │
       dispatcher (NEW, ~280 LOC)              carve-outs (5)
            │                                /healthz, /lark/event,
            │                                /install/start|callback|dev-login
    Bind(where, authFn, Input, handler)
            │
            ▼
 ┌─────────────────────────────────────────────────┐
 │ entry log → decode + 1 MiB cap → auth extract → │  ← framework
 │   handler(rc, req) → encode + envelope          │     (logext + respond)
 │   ↳ 4xx warn  ↳ 5xx errorf %+v  ↳ 2xx info      │
 └─────────────────────────────────────────────────┘
            │
            ▼
        service ── repo ── sentinel errors (Err*)
                              │
                              ▼
            errors.As(err, *dispatcher.Error) — dispatched at envelope
```

The dispatcher is the **only** new concept. `respond.{Decode,Error,Proto}`, `session.RequireSession`, `session.FromContext`, `apikey.Middleware`, `apikey.TenantIDFromContext`, `logext.{Info,Warn,Error}f`, and every sentinel error in `domain/` and `repo/` are reused verbatim.

### Handler signature — generic `RequestContext[Auth]`

```go
package dispatcher

// Single shape, parameterized by Auth.
type RequestContext[Auth any] struct {
    context.Context // embedded — *RequestContext[Auth] satisfies context.Context
    Auth Auth
}

func (c *RequestContext[Auth]) Response() http.ResponseWriter

type Input[Req proto.Message] struct {
    new  func() Req
    bind func(*http.Request, Req) error
}

func Bind[Auth any, Req, Resp proto.Message](
    where string,
    authFn func(context.Context) Auth,
    input Input[Req],
    handler func(*RequestContext[Auth], Req) (Result[Resp], error),
) http.HandlerFunc
```

The auth extractor is supplied at the route site (`session.FromContext`, `apikey.FromContext`, or a tiny wrapper), which keeps `internal/dispatcher` out of `handlers/console/internal/session`'s `internal/` visibility boundary.

A console handler then reads:

```go
// Before — internal/handlers/console/notifytarget/notify_targets_write.go:22-47 (26 LOC, 92% boilerplate)
func (h *NotifyTargetsHandler) Delete(w http.ResponseWriter, r *http.Request) {
    const where = "console.NotifyTargetsHandler.Delete"
    ctx := r.Context()
    auth := session.FromContext(ctx)
    id, err := uuid.Parse(chi.URLParam(r, "id"))
    if err != nil {
        logext.Warnf(ctx, "[%s] reject: bad uuid,tenant_id:%s", where, auth.TenantID)
        respond.Error(ctx, w, http.StatusBadRequest, "bad_id", "id is not a UUID")
        return
    }
    logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
    if err := h.repo.Delete(ctx, auth.TenantID, id); err != nil {
        if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
            logext.Warnf(ctx, "[%s] reject: not found,...", where)
            respond.Error(ctx, w, http.StatusNotFound, "not_found", "notify target not found")
            return
        }
        logext.Errorf(ctx, "[%s] internal: db delete failed,err:%+v", where, err)
        respond.Error(ctx, w, http.StatusInternalServerError, "internal", "...")
        return
    }
    w.WriteHeader(http.StatusNoContent)
    logext.Infof(ctx, "[%s] OK,...", where)
}

// After — the route-local binder handles chi param extraction and the dispatcher
// handles auth, body decode, status selection, and envelope writing.
func (h *NotifyTargetsHandler) Delete(
    ctx *dispatcher.RequestContext[*session.AuthCtx],
    req *v1.DeleteNotifyTargetRequest,
) (dispatcher.Result[*v1.DeleteNotifyTargetResponse], error) {
    id, err := uuid.Parse(req.Id)
    if err != nil {
        return dispatcher.Fail[*v1.DeleteNotifyTargetResponse](http.StatusBadRequest, v1.ErrorCode_BAD_ID, "id is not a UUID")
    }
    if err := h.repo.Delete(ctx, ctx.Auth.TenantID, id); err != nil {
        if errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
            return dispatcher.Fail[*v1.DeleteNotifyTargetResponse](http.StatusNotFound, v1.ErrorCode_NOT_FOUND, "notify target not found")
        }
        return dispatcher.Result[*v1.DeleteNotifyTargetResponse]{}, err // → dispatcher: 500 envelope + logext.Errorf("err:%+v")
    }
    return dispatcher.NoContent[*v1.DeleteNotifyTargetResponse]()
}
```

The router registration loses 50+ lines per handler file and becomes:

```go
r.Delete("/notify-targets/{id}", dispatcher.Bind(
    "console.NotifyTargetsHandler.Delete",
    consoleSession,
    dispatcher.Path(
        func() *v1.DeleteNotifyTargetRequest { return &v1.DeleteNotifyTargetRequest{} },
        dispatcher.Param("id", func(req *v1.DeleteNotifyTargetRequest, id string) {
            req.Id = id
        }),
    ),
    h.Delete,
))
```

For the common cases, route sites read as intent instead of mechanics:

```go
dispatcher.Empty(func() *v1.GetUsageRequest { return &v1.GetUsageRequest{} })
dispatcher.JSON(func() *v1.CreateApiKeyRequest { return &v1.CreateApiKeyRequest{} })
dispatcher.Query(func() *v1.ListFeedbackRequest { return &v1.ListFeedbackRequest{} }, bindListFeedbackRequest)
dispatcher.Path(
    func() *v1.DeleteApiKeyRequest { return &v1.DeleteApiKeyRequest{} },
    dispatcher.Param("id", func(req *v1.DeleteApiKeyRequest, id string) {
        req.Id = id
    }),
)
dispatcher.Combine(
    func() *v1.UpdateNotifyTargetRequest { return &v1.UpdateNotifyTargetRequest{} },
    dispatcher.JSONBody[*v1.UpdateNotifyTargetRequest],
    dispatcher.Param("id", func(req *v1.UpdateNotifyTargetRequest, id string) {
        req.Id = id
    }),
)
```

`Custom` remains as the escape hatch for endpoint-specific binders that do not fit the common helpers. `Response()` is intentionally narrow: it exists for header/cookie side-effects such as `/logout`, while status/body writing stays owned by the dispatcher.

`Bind` is the public route-site adapter. A package-local `bind` function holds the lower-level implementation (single canonical request loop — 6 steps):

```go
func Bind[Auth any, Req, Resp proto.Message](
    where string,
    authFn func(context.Context) Auth,
    input Input[Req],
    handler func(*RequestContext[Auth], Req) (Result[Resp], error),
) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        start := time.Now()

        // 1. Auth — auth middleware ran upstream, authFn is route-local.
        auth := authFn(ctx)
        rc := &RequestContext[Auth]{Context: ctx, Auth: auth}

        logext.Infof(ctx, "[%s] start,method:%s,path:%s", where, r.Method, r.URL.Path)

        // 2. Allocate + bind route fields/body.
        req := input.new()
        if input.bind != nil {
            if err := input.bind(r, req); err != nil {
                writeDecodeError(ctx, w, err)  // 400 / 413 mapping
                return
            }
        }

        // 3. Call business handler.
        result, err := handler(rc, req)

        // 4. Dispatch — typed Error → status+envelope, ctx.Err → 499/504, bare err → 500.
        if err != nil {
            writeHandlerError(ctx, w, err, where, start)
            return
        }

        // 5. Encode payload when present.
        if result.Status != http.StatusNoContent {
            respond.Proto(w, result.Status, result.Body)
        } else {
            w.WriteHeader(http.StatusNoContent)
        }

        // 6. Exit log.
        logext.Infof(ctx, "[%s] OK,status:%d,latency_ms:%d", where, result.Status, time.Since(start).Milliseconds())
    }
}
```

The route-local `authFn` and `Input` binder are the extension points. The 6-step skeleton is the contract — every migrated endpoint runs through these steps in this order.

### Error model — single typed error, dispatcher-side dispatch

Lower layers keep their `Err*` sentinels (`domain/apikey.go`, `repo/notifytarget/`, `repo/tenant/`, …). Handlers classify with `errors.Is` and return `dispatcher.Fail[Resp](status, code, msg)`. The underlying `NewError` factory shape matches `respond.Error(ctx, w, status, code, msg)` (`internal/respond/respond.go:64`) exactly — the dispatcher is a 1:1 lift of the existing wire-write call into the type system, while `Fail` removes the repeated zero `Result` ceremony.

Handlers no longer pass free-form code strings. They pass the `proto`-defined `ErrorCode` enum (see §"Error code IDL contract" below), and `respond.Error` serializes the enum name into the existing string field. That gives Go call-sites compile-time safety while keeping protobuf compatibility for `ErrorResponse`.

```go
package dispatcher

import attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"

type Error struct {
    Status  int
    Code    attunev1.ErrorCode
    Message string
}

func (e *Error) Error() string { return e.Message }

type Result[Resp proto.Message] struct {
    Status int
    Body   Resp
}

func Success[Resp proto.Message](status int, body Resp) (Result[Resp], error) {
    return Result[Resp]{Status: status, Body: body}, nil
}

func OK[Resp proto.Message](body Resp) (Result[Resp], error) {
    return Success(http.StatusOK, body)
}

func Created[Resp proto.Message](body Resp) (Result[Resp], error) {
    return Success(http.StatusCreated, body)
}

func NoContent[Resp proto.Message]() (Result[Resp], error) {
    return Result[Resp]{Status: http.StatusNoContent}, nil
}

// NewError is the typed error constructor. Shape matches respond.Error
// (internal/respond/respond.go:64) after respond.Error itself absorbs
// the ErrorCode upgrade — call-site migration is a near-mechanical
// rewrite, no new mental model.
func NewError(status int, code attunev1.ErrorCode, msg string) *Error {
    return &Error{Status: status, Code: code, Message: msg}
}

func Fail[Resp proto.Message](status int, code attunev1.ErrorCode, msg string) (Result[Resp], error) {
    return Result[Resp]{}, NewError(status, code, msg)
}
```

The dispatcher request loop extracts via `errors.As` and writes via the existing `respond.Error` (whose `code` parameter also absorbs the `ErrorCode` upgrade — see Implementation plan):

```go
var e *dispatcher.Error
if errors.As(err, &e) {
    respond.Error(ctx, w, e.Status, e.Code, e.Message)
    return
}
// bare err → 500 with full-stack log
respond.Error(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "internal server error")
logext.Errorf(ctx, "[%s] internal,err:%+v", where, err)
```

A bare `err` returned from a handler **always** maps to 500 `code=INTERNAL` and is logged with the full stack — making the apikey-leak class of bug structurally impossible.

`context.Canceled` / `context.DeadlineExceeded` are special-cased: 499 / 504 with `CLIENT_CANCELED` / `DEADLINE_EXCEEDED` codes, **not 500**, and at INFO level — these aren't service bugs.

### Error code IDL contract — `ErrorCode` enum with string-compatible `ErrorResponse.code`

attune's `respond.Error` call-sites today produce **19 distinct stable `code` strings** (verified by grep across `internal/`): `bad_id`, `bad_request`, `body_too_large`, `conflict`, `csrf_invalid`, `delivery_failed`, `internal`, `label_too_long`, `missing_label`, `missing_sample`, `missing_tenant`, `not_found`, `not_implemented`, `redirect_failed`, `session_sign_failed`, `tenant_not_found`, `unauthorized`, `user_gone`, `validation`. They are part of the wire contract — the console SPA branches on them — yet they live as untyped strings on both sides, drifting silently. The audit step also normalizes the two current outliers: the apikey middleware's `unauthenticated` spelling and the OAuth `err.Error()`-as-code path.

Per CLAUDE.md §11 (proto is the single truth source for the HTTP contract), error codes belong in the proto layer:

```protobuf
// proto/attune/v1/common.proto
enum ErrorCode {
  ERROR_CODE_UNSPECIFIED  = 0;   // proto3 default — never wire
  BAD_REQUEST             = 1;
  UNAUTHORIZED            = 2;
  NOT_FOUND               = 3;
  CONFLICT                = 4;
  VALIDATION              = 5;
  BODY_TOO_LARGE          = 6;
  INTERNAL                = 7;
  NOT_IMPLEMENTED         = 8;
  BAD_GATEWAY             = 9;
  CLIENT_CANCELED         = 10;
  DEADLINE_EXCEEDED       = 11;
  // attune business-specific (extend as new sentinels are surfaced)
  BAD_ID                  = 12;
  TENANT_NOT_FOUND        = 13;
  USER_GONE               = 14;
  CSRF_INVALID            = 15;
  SESSION_SIGN_FAILED     = 16;
  MISSING_TENANT          = 17;
  MISSING_LABEL           = 18;
  MISSING_SAMPLE          = 19;
  LABEL_TOO_LONG          = 20;
  DELIVERY_FAILED         = 21;
  REDIRECT_FAILED         = 22;
}

message ErrorResponse {
  string code          = 1;   // enum name string, e.g. "VALIDATION"
  string    message    = 2;
  string    request_id = 3;
}
```

`buf.yaml` adds `ENUM_VALUE_PREFIX` to the lint exclusion list for `ErrorCode` so values aren't forced to carry the `ERROR_CODE_` prefix on the wire — keeping wire bytes short and human-readable. The `UNSPECIFIED` value at position 0 follows attune's existing proto3 convention.

This is **attune's first `.proto`-defined `enum`** — `grep "^enum " proto/attune/v1/*.proto` on `main` returns 0 matches. Toolchain consequences:

- **`buf.gen.yaml` must add `stringEnums=true` to the ts-proto plugin options.** Without it, ts-proto v2 generates number-based TypeScript enums (`CONFLICT = 4`), and console JSON deserialization maps the wire `"CONFLICT"` string to the number `4`. The console code `if (err.code === ErrorCode.CONFLICT)` still type-checks (number compare), but loses string-grep refactorability and reads opaquely against logs. With `stringEnums=true`, the generated TS is `export enum ErrorCode { CONFLICT = "CONFLICT", ... }` — wire value, enum constant, and any log message are the same string.
- **No Go-side generator config change needed.** `respond.Error` calls `code.String()` and writes the enum name into the existing `ErrorResponse.code` string field.
- **OpenAPI remains conservative.** Because `ErrorResponse.code` stays a string field, the generated schema remains string-shaped; the closed set lives in the adjacent `ErrorCode` enum and is enforced at Go call-sites.

**Wire change (pre-1.0 minor bump)**: protoJSON's default enum serialization writes the enum value name verbatim, so `{"code": "conflict"}` becomes `{"code": "CONFLICT"}`. The console SPA migrates its 8 string-match sites to enum constants from generated `console/src/proto/attune/v1/common.ts` **in the same PR**.

Handlers consume the enum directly:

```go
return dispatcher.Fail[*attunev1.NotifyTarget](http.StatusConflict, attunev1.ErrorCode_CONFLICT, "...")
```

`dispatcher.Fail` / `dispatcher.NewError` take `attunev1.ErrorCode`, not `string` — typos and drift are caught at compile time. Likewise, `respond.Error`'s `code` parameter upgrades to `ErrorCode`; the apikey middleware, the OAuth/dev-login carve-outs, and the ingest writeError all migrate in the same wave (Implementation plan Commit 2). The audit step resolves the current `unauthenticated`/`err.Error()` outliers before that commit lands.

`buf breaking` allows enum value additions (non-breaking) but blocks enum renames/removals and any future `ErrorResponse` field-type churn. The remaining string-field emission is guarded by the typed `respond.Error` signature and endpoint smoke tests.

### Observability — hard default, escape hatch

The dispatcher emits the standard logext triple per request, removing it from every handler body:

- **entry** — `INFO  [dispatcher] start,method=POST,path=/notify-targets,tenant_id=…`
- **2xx exit** — `INFO  [dispatcher] OK,status=201,latency_ms=…`
- **4xx exit** — `WARN  [dispatcher] reject,status=409,code=CONFLICT,latency_ms=…`
- **5xx exit** — `ERROR [dispatcher] internal,err=%+v,latency_ms=…`

`trace_id` / `span_id` flow automatically — `logext` already pulls them from the active OTel span on `ctx` per the CLAUDE.md §7 contract. The dispatcher injects no new context values.

### Package boundary — `dispatcher` types stay in handler files

The dispatcher's `RequestContext` is **forbidden** outside `internal/handlers/`. Lower layers see only `context.Context` and explicit args, exactly as today:

```go
// repo / service — byte-identical signatures.
func (r *Repo) Delete(ctx context.Context, tenantID string, id uuid.UUID) error
func (s *Service) DoThing(ctx context.Context, tenantID, userID string, ...) error
```

The handler call-site uses `rc` directly because `*RequestContext[Auth]` satisfies `context.Context` via embedded-field method promotion:

```go
h.repo.Delete(rc, rc.Auth.TenantID, id)
//             ^^ context.Context (embedded promotion)
//                ^^^^^^^^^^^^^^^^^ explicit field unpack
```

This is the load-bearing reason embedding (`context.Context` as an embedded interface field) was chosen: handler parameters can be named `ctx` and passed directly to service / repo calls, while `ctx.Auth` keeps auth typed and explicit.

Enforcement starts with review plus `go vet`/tests in this proving slice. A dedicated `cmd/lint-dispatcher-leak` AST check is a follow-up if the dispatcher expands beyond handler packages.

### Generics adoption note

`type RequestContext[Auth any] struct { ... }` is **attune's first generic struct type definition**. The 15 existing generic functions all live in `internal/pkg/ptrext` (single-type-parameter `[T any]` / `[T comparable]` / `[T cmp.Ordered]`) — readers familiar with ptrext have the building blocks but haven't seen them composed at the type-definition layer.

Predicted cost: reviewers need 2-3 clarification rounds on the generic-struct mechanics during initial review. Mitigation:

- `internal/dispatcher/doc.go` opens with a "Why generics here" paragraph cross-referencing ptrext's rationale (zero-cost type erasure, no interface dispatch on the hot path).
- `Bind` and `RequestContext` are the only generic surfaces in the package; handler authors interact with concrete instantiations inferred at the router call-site.
- If post-merge experience reveals genuine team-wide friction, fallback path is Alternative §A1 (3-parameter handler signature). Handler bodies are unchanged — only signatures refactor; estimated ~1 day cleanup, no behavioral change.

### Decisions

1. **Self-build, not Huma.** Huma's `application/problem+json` envelope + struct-derived OpenAPI conflict with attune's `{code, message, requestId}` envelope and CLAUDE.md §11 (proto is the single source of truth). Adapting Huma costs more LOC than the 280-LOC self-build and leaves a permanent two-truth-source maintenance tax. (See Alternatives §A4.) The dispatcher package sits **at the handler layer** (same level as `respond` / `session` / `apikey`), not as a neutral framework — importing `session.AuthCtx` and `apikey.AuthCtx` for the type aliases matches the existing 14 import sites in handler subpackages (`grep -rn "handlers/console/internal/session\|infra/apikey" internal/handlers/`) and follows attune's hybrid feature-package convention (CLAUDE.md §5). There is no "framework purity" goal to violate.
2. **Generic `RequestContext[Auth]`.** Single concept across all auth dimensions; compile-time type safety per dimension; `Auth` reachable as a typed field (`rc.Auth.TenantID`); `context.Context` embedded so `rc` is pass-throughable. (See Alternatives §A1–A3 for rejected paths.)
3. **Embed `context.Context`.** Embedding turns the handler parameter into a drop-in `context.Context`, killing call-site ceremony at every service/repo boundary. The construction site (the dispatcher itself) controls nil-safety.
4. **Single failure helper `Fail[Resp](status, code, msg)`.** The underlying `NewError` shape matches `respond.Error(ctx, w, status, code, msg)` (`internal/respond/respond.go:64`); migration is a near-mechanical rewrite, with no zero-result ceremony at handler call-sites. The Huma-style nine `Error4XX`-named helpers were rejected because attune's existing call-sites do not have a 1:1 status→code mapping (400 is variously `"bad_request"` / `"bad_id"` / `"validation"`; 401 is `"unauthenticated"` / `"user_gone"`; etc.) — any "default code per helper" abstraction would be wrong half the time, leaving an override path as the de-facto convention. (See Alternatives §A8.)
5. **No global mutable state, no factory hooks.** attune's framework-y packages document the convention explicitly: `internal/infra/observability/slog.go:28-29` — "Pure — no global state. Tests construct a handler over a bytes.Buffer here without touching slog.Default." A repo-wide `grep '^var [A-Z][a-zA-Z]* = func' internal/ cmd/` returns 0 results. The dispatcher follows the same convention — no `var NewError = func(...)` swap point. Envelope evolution (future RFC 9457, etc.) is achieved by editing the dispatcher's internal `respond.Error` call site, not by exposing a mutable factory.
6. **Observability hard-default.** Entry/exit `logext` triple is mandatory. Quieter logging can be added later as an explicit option once there is a real quiet-by-design endpoint.
7. **5 carve-outs.** `/healthz`, `/v1/lark/event`, and the three `/install/*` OAuth-flow endpoints stay native `http.HandlerFunc`. CLAUDE.md §11 already names lark; this proposal extends the carve-out list to the other four with the same justification (HTTP shape is not `proto.Message in → proto.Message out`).
8. **One `Bind` plus explicit auth extractor.** A session-specific helper cannot live in `internal/dispatcher` because it would need to import `handlers/console/internal/session`, which is outside Go's `internal/` visibility boundary. Passing `authFn` at the route site keeps the package boundary honest while preserving compile-time auth typing.
9. **Wire envelope byte-identical, with one deliberate exception.** `{code, message, requestId}` envelope shape preserved; status codes and protojson `EmitUnpopulated` behavior preserved. The deliberate exception: `ErrorResponse.code` values normalize from lower_snake strings to `ErrorCode` enum names while the protobuf field remains `string` for compatibility — see §"Error code IDL contract". Justified: error codes are wire contract that the console SPA branches on; per CLAUDE.md §11 they belong in proto, not in Go/TS hardcoded strings drifting silently. pre-1.0, no public SDK consumers; `### Changed` in CHANGELOG. E2E baseline gate enforces the rest of the wire fleet-wide; a future RFC 9457 alignment is a separate minor bump, not coupled here.
10. **Phased endpoint migration.** Start with mechanical handlers (`usage`, API keys, notify-target List/Create), then move the richer by-id and validation-heavy handlers once the dispatcher behavior is covered. Mixed style is temporary and tracked by this proposal's implementation checklist.

## Alternatives considered

### A1. Issue draft signature — three parameters `(ctx, auth, req)`

The issue draft proposed:
```go
func (h *Handler) Delete(
    ctx context.Context,
    auth *session.AuthCtx,
    req *v1.DeleteNotifyTargetRequest,
) (*v1.DeleteNotifyTargetResponse, error)
```

Compile-time-safe, no new abstraction, downstream pass-through is trivial — but **three parameters per handler** and **no single concept** that future cross-cutting context fields (`FeatureFlag()`, `TenantConfig()`) could be hung off of. Rejected in favor of Decision 2; preserved here as the minimum-deviation fallback if the generic path turns out to have an unforeseen ergonomic cost on Go 1.x of the day.

### A2. Single non-generic `*dispatcher.RequestContext`

```go
type RequestContext struct {
    context.Context
    TenantID string
    UserID   string   // session-only — zero value on apikey endpoints
    KeyID    uuid.UUID // apikey-only — zero value on session endpoints
}
```

Loses the core type-safety promise. A console handler can compile a call to `rc.KeyID`, get `uuid.Nil`, and ship a silent bug — exactly the kind of mismatch the dispatcher exists to eliminate. Rejected.

### A3. Two typed interfaces — `SessionContext` / `IngestContext`

```go
type SessionContext interface {
    context.Context
    TenantID() string
    UserID() string
}
type IngestContext interface {
    context.Context
    TenantID() string
    KeyID() uuid.UUID
}
```

Recovers type safety, but **two concepts**, **dispatcher must write wrapper implementations**, and **tests must construct interface mocks rather than struct literals**. Adopted approach (Decision 2) achieves the same compile-time safety with a single generic type and zero wrappers (`&dispatcher.RequestContext[*session.AuthCtx]{Context: ctx, Auth: ac}` is one literal). Rejected.

### A4. Direct adoption of Huma

Huma cleanly cover the generic-handler + tag-bind dimensions and has an actively maintained `humachi` adapter. But:

- **Envelope mismatch.** Huma defaults to RFC 9457 `application/problem+json` + an `ErrorModel{Type,Title,Status,Detail,Instance,Errors}`. attune wires `{code, message, requestId}` with `code` as the machine-readable kind and `requestId` lifted to the top level (AIP-193 minimal). Adapting Huma to attune's envelope requires overwriting `huma.NewError` and writing a custom `Format` — viable but the integration glue is comparable in size to the self-build.
- **OpenAPI two-truth-source.** Huma derives OpenAPI from Go input/output struct reflection. attune derives OpenAPI from `.proto` via `protoc-gen-openapi` (PR #67, CLAUDE.md §11). Two sources drift, by construction.
- **logext contract.** Huma exposes middleware hooks; attune's logext is a strong project-wide convention. Strapping logext to a Huma middleware works but inverts the "framework defaults are the convention" property.

Rejected. The dispatcher borrows one Huma design point directly: the `errors.As(err, *StatusError)`-style dispatch loop, which cleanly separates handler-returned typed errors from bare `err` 500s. It rejects two other Huma patterns explicitly — the `var NewError` factory hook (Decision 5, §A8) and the per-status `Error4XX`-named helpers (Decision 4, §A8) — for attune-codebase-fit reasons.

### A5. Domain-named Bind variants — `BindConsole` / `BindIngest` / `BindAnon`

Reads slightly more natural to attune-newcomers (Console = the operator console), but couples the framework type to the current product taxonomy. If apikey ever needs to authenticate console actions, or session ever appears outside `/fb/v1/console/`, the names mislead. Mechanism-named (Decision 8) is stable forever. Rejected.

### A6. Connect-RPC

The natural progression from PR #67 (also buf-family). Rejected because:

- Connect mounts services at `/<package>.<Service>/<Method>`, not at attune's existing REST paths. Either rewrite all console URLs or run a URL rewrite layer permanently — both bad.
- Connect-Web is a new TS client; replaces today's protojson + fetch console SDK. Cost: 8 call-site rewrites + a new dependency.
- OAuth callback / lark event / healthz have no Connect-shaped form. They'd still need carve-outs **and** the dispatcher contract would lose the wider "all product API" coverage.

Tracked as a Phase 2 possibility once the dispatcher proves out, but explicitly not chosen now.

### A7. Goa DSL + codegen

Goa is the canonical Go "DSL first, generate server + client + OpenAPI" framework. Rejected: proto is already the second IDL layer; a DSL would be a third truth source. Goa also generates substantially more code than 18 endpoints could justify amortizing.

### A8. Huma-style nine `Error4XX`-named helpers + `var NewError` factory hook

Huma exposes 28 helpers (`Error400BadRequest`, `Error401Unauthorized`, …, `Error511NetworkAuthenticationRequired`) plus a swappable `var NewError = func(...)` factory (`huma/error.go:231`). The first draft of this proposal copied that pattern.

Rejected on attune-codebase fit grounds:

- **The nine-helper convention assumes a stable status→code mapping.** attune doesn't have one — current `respond.Error` call-sites use `"bad_id"` / `"bad_request"` / `"validation"` for 400, `"unauthenticated"` / `"user_gone"` for 401, etc. A default-code per helper would be wrong frequently, leaving `WithCode` override as the de-facto path — extra surface area for zero gain.
- **`var NewError` is a globally mutable function variable.** `grep '^var [A-Z][a-zA-Z]* = func' internal/ cmd/` returns **0** results — attune has zero precedent for this pattern. `internal/infra/observability/slog.go:28-29` documents the opposite convention explicitly ("Pure — no global state. Tests construct ... without touching slog.Default.").
- **Test ergonomics are worse, not better.** Parallel `go test` packages racing on a shared `var NewError` would require `t.Cleanup` discipline at every call-site. Real testability comes from asserting the wire output (status + envelope bytes), which `NewError(status, code, msg)` enables directly via `errors.As(err, *dispatcher.Error)` — no hook needed.

The replacement is the single `dispatcher.Fail[Resp](status, code, msg)` handler helper, backed by `NewError(status, code, msg)` matching `respond.Error` (`internal/respond/respond.go:64`) shape-for-shape.

### A9. `ErrorCode` enum design sub-choices

The §"Error code IDL contract" decision is to add `enum ErrorCode`, keep `ErrorResponse.code` as a compatibility string, and make every Go writer pass the enum. Several sub-choices were considered:

- **`ERROR_CODE_` prefix on values vs. bare names.** buf lint's `ENUM_VALUE_PREFIX` rule wants the prefix (`ERROR_CODE_CONFLICT`). Rejected: protoJSON serializes the verbatim name to wire, so the prefix would bloat every error response by 11+ bytes and degrade readability (`{"code": "ERROR_CODE_CONFLICT"}`). The buf rule is excluded for this enum specifically — the convention's tradeoff doesn't apply when the enum value name *is* the wire value.
- **Only-common codes in enum, business-specific stays string.** Considered: enum exposes 8 generic `BAD_REQUEST`/`NOT_FOUND`/… codes; business specifics like `notify_target_conflict` stay free-form string. Rejected: a two-tier system (typed enum + escape-hatch string field) is harder to consume (the SPA always needs to handle "the other case") and never converges — every new business code becomes another untyped string. The enum is the **single** source.
- **Open-set vs closed-set enum.** proto3 enums are open (unknown values are preserved on the wire), so old clients receiving new codes degrade to "unknown" rather than crash. Combined with `buf breaking`'s allow-add-block-rename behavior, this gives forward compatibility for free.
- **`UNSPECIFIED` semantics.** Position 0, default value, **never emitted on the wire** by any handler — `respond.Error` panics in dev / `logext.Errorf` in prod if asked to write `UNSPECIFIED`. Enforces "every error must have an intentional code".
- **Lowercase wire compatibility (keeping `"conflict"` instead of `"CONFLICT"`).** The compatibility-string field makes custom lowercase mapping possible, but it would put the proto enum and HTTP wire value back out of sync. The explicit decision is to make enum value name, TS enum constant, logs, and HTTP `code` string identical.

## Risks / tradeoffs

| Risk | Severity | Mitigation |
|---|---|---|
| **Reflection / generic indirection regresses p99.** Huma v1 was 50× slower than v2 because reflection wasn't cached. | Medium | The dispatcher's hot path is one generic indirection per request — no reflection (proto messages are concrete types known at `Bind` site). Benchmark evidence is a follow-up once attune has benchstat infrastructure. |
| **Phased migration leaves two handler styles temporarily.** | Medium | Keep each slice small, tested, and recorded in this proposal. Pick mechanical endpoints first; move stateful `ResponseWriter` cases only after the side-effect shape is explicit. |
| **`dispatcher` type leaks into `service` / `repo` / `domain` / `infra`.** | Low (process) | Review and `rg 'internal/dispatcher' internal/{service,repo,domain,infra}` during the proving slice; AST linter remains a follow-up if the package spreads. |
| **Embedded `Context` field nil → `ctx.Done()` panic.** | Low | Construction is owned solely by `dispatcher.Bind`; unit-test coverage covers the constructor path. No public helper is needed for handlers to construct `RequestContext` in production. |
| **Carve-out list grows over time.** | Low | Each new carve-out requires a CLAUDE.md §11 update justifying why it isn't `proto in → proto out`. The bar is self-policing. |
| **`ErrorResponse.code` wire value change (lower_snake → UPPER_SNAKE) breaks any third-party client hardcoding `"conflict"`-style strings.** | Low | attune is pre-1.0 with no public SDK consumers; the console SPA is the only known consumer and migrates its string-match fixtures in the same PR; documented in CHANGELOG `### Changed`; typed Go signatures plus `buf breaking` from this baseline forward catch future drift. |

## Implementation plan

Phased implementation on the issue branch. Each slice should stay reviewable, keep `go test -short ./...` green, and update this proposal's checkpoint.

**Commit 1 — `proto/attune/v1/common.proto` adds `ErrorCode` enum while keeping `ErrorResponse.code` string-compatible.**
- Adds `enum ErrorCode` (closed set, `UNSPECIFIED` at 0) per §"Error code IDL contract".
- Keeps `ErrorResponse.code` as `string` so `buf breaking --against main` passes; values are enum names emitted by `respond.Error`.
- `buf.yaml` excludes `ENUM_VALUE_PREFIX` rule for `ErrorCode` (rationale in §A9).
- `make proto` regenerates Go (`internal/proto/attune/v1/common.pb.go`) and TS (`console/src/proto/attune/v1/common.ts`).

**Commit 2 — `respond.Error` signature upgrade.**
- `respond.Error`'s `code` parameter type `string` → `attunev1.ErrorCode`.
- All non-dispatcher call-sites migrate in the same commit: `apikey.Middleware`, `oauth.Start/Callback`, `DevLogin`, `IngestHandler.writeError`, `lark.Handler.Event`. Roughly 30 call-sites total per the audit (§"Problem"). The raw `err.Error()` OAuth path is normalized to a stable enum before this lands.
- `internal/handlers/console/internal/respond` re-export stays a thin wrapper; signature mirrors automatically.
- Existing tests continue to pass: the wire value changes from `"conflict"` to `"CONFLICT"` per-fixture (golden files regenerated), while the protobuf field shape stays compatible.

**Commit 3 — `internal/dispatcher/` package.**
- `types.go` — `RequestContext[Auth]`, `Result[Resp]`, `OK`, `Created`, `Success`, `NoContent`, `Error`, `NewError`, `Fail`, `DecodeJSON`.
- `input.go` — `Input[Req]`, `Empty`, `JSON`, `Path`, `Query`, `Param`, `ParamInt64`, `Combine`, and `Custom` bind helpers.
- `bind.go` — generic `Bind(where, authFn, input, handler)` request loop, with package-local `bind` kept as the implementation adapter.
- `bind_test.go` — coverage for OK, 204, typed errors, JSON body binding, path/query binding, context cancellation/deadline mapping, and oversized bodies.

**Commit 4 — package-boundary audit.**
- Keep `internal/dispatcher` at handler-layer only. No service/repo/domain/infra import is introduced by the proving slice.
- A dedicated AST linter remains a follow-up if the dispatcher expands beyond the initial endpoints.

**Commit 5 — sentinel completeness audit.** Pass over `internal/domain/`, `internal/service/`, `internal/repo/`. Today's named sentinels (`ErrAPIKeyNotFound`, `ErrNotifyTargetConflict`, `ErrNotifyTargetNotFound`, `ErrTenantNotFound`, `ErrFeedbackNotFound`, `ErrDimensionKindInvalid`, …) plus any unnamed errors discovered during migration get a name. No behavior change — just exporting `var Err* = errors.New(...)`. **If the audit surfaces an error path whose handler-level `code` is not yet in `ErrorCode`**, the new enum value is appended to `proto/attune/v1/common.proto` in this same commit and `make proto` re-runs — `buf breaking` permits enum value addition, so this is non-breaking. The audit also normalizes the current `unauthenticated` and OAuth raw-code outliers before the dispatcher migration starts. The audit and the enum should converge to a single source of truth for "every distinct error a handler can return."

**Commit 6+ — phased endpoint migration.** Each endpoint group:
1. Rewrites the handler to `func(rc, req) (dispatcher.Result[*resp], error)` form.
2. Updates the router registration to `dispatcher.Bind(...)` with `dispatcher.Empty` / `dispatcher.JSON` / `dispatcher.Path` / `dispatcher.Query` / `dispatcher.Param` / `dispatcher.ParamInt64` / `dispatcher.Combine` / `dispatcher.Custom`.
3. Updates or adds handler tests for preserved behavior.

Migrated endpoint groups:
- `usage.Get`
- `api-keys.{List,Create,Revoke}`
- `notify-targets.{List,Create,Patch,Delete,Test}`
- `feedback.{List,Stats,Get}`
- `enrichconfig.{Get,Update,Preview}`
- `me.{Me,Logout}` with `RequestContext.Response()` for cookie clearing
- public `ingest.Ingest` with API-key auth and preserved ingest metrics/body-limit behavior

**Follow-up — `console/` SPA migrates string-match sites to `ErrorCode` enum.**
- Identify the 8 call-sites where the SPA branches on `err.code === "..."` strings.
- Replace string literals with `ErrorCode.<NAME>` constants imported from `console/src/proto/attune/v1/common.ts`.
- `pnpm tsc -b --noEmit` green; `pnpm vitest run --coverage` green (fixtures updated to new wire shape in Commit 2 already).

**Follow-up — docs and changelog.**
- CHANGELOG `### Added`: `internal/dispatcher/` package; `enum ErrorCode` in `proto/attune/v1/common.proto`.
- CHANGELOG `### Changed`: handler layer rewritten on the typed dispatcher; `ErrorResponse.code` wire value from lower_snake to `ErrorCode` enum-name string — pre-1.0 minor bump; console SPA migrated in same release.
- CLAUDE.md §11 carve-out list extended: `/healthz`, `/v1/lark/event`, `/fb/v1/console/install/{start,callback,dev-login}`.

**Follow-up — consolidated wire-test battery + CI lock.** The per-endpoint wire tests added during phased migration are reviewed as a coherent set; `internal/handlers/ingest_wire_test.go` stays as the public-API anchor. Expected diff vs pre-dispatcher hand-snapshots: only the `code` value case (`"conflict"` → `"CONFLICT"`), every other byte identical.

## Verification

- `go vet ./...` — 0 warnings.
- `go build ./...` — 0 errors.
- `go test -short ./...` — all pass.
- Endpoint smoke tests cover all 18 migrated product endpoints through `dispatcher.Bind` with real JSON/query/path binders: session (2), API keys (3), notify targets (5, including loopback `TestSend`), feedback (3), usage (1), enrich config (3), and public ingest (1). `internal/handlers/console/router_inventory_test.go` also locks the console route surface so an endpoint cannot silently fall out of the dispatcher migration.
- `bash scripts/lint-errorcode.sh` — clean; this catches `attunev1.ErrorResponse{Code: "..."}` and forces `attunev1.ErrorCode` use.
- `go test ./cmd/lint-errorcode` — pass.
- `bash scripts/lint-rawptr.sh` and `go test ./cmd/lint-rawptr` — pass; the linter now treats `dispatcher.JSONBody[*Req]`-style exported generic selector type arguments as type position while still flagging real value-position pointer dereferences.
- `git diff --check` — clean.
- `lizard . -l go -C 15 -T nloc=100` — no thresholds exceeded.
- `npx -y jscpd . -f go -i '**/*.pb.go' -t 4 --silent` — 2.04% duplicated lines with generated protobuf Go excluded, under the 4% gate.
- `buf generate` — regenerated Go / TS / OpenAPI outputs; a second `buf generate && git diff --check` was clean.
- `buf lint` and `buf breaking --against '.git#branch=main'` — both pass with a checksum-verified official `buf` v1.70.0 binary. CI now downloads `sha256.txt` and compares the Linux `buf` asset before running proto gates.
- `PATH=/private/tmp/attune-buf:$PATH make proto-lint` and `make proto-breaking` — both pass before the temporary `buf` binary is removed.
- Frontend `pnpm typecheck` — pass.
- Frontend `pnpm test` — 18 files / 82 tests pass.

## References

- Issue [#71](https://github.com/Phixsura/attune/issues/71) — the originating ticket, source of the four Open Questions answered above.
- Related proposals: `docs/proposals/2026/06/2026-06-06-protobuf-idl-contract.md` (#19), `docs/proposals/2026/06/2026-06-06-inbound-adapter-framework.md` (#66), `docs/proposals/2026/06/2026-06-08-logext-consolidation.md` (#48).
- **Huma** — `huma.go` <https://github.com/danielgtaylor/huma/blob/main/huma.go> (`Register`, `processInputType`, dispatch loop), `error.go` <https://github.com/danielgtaylor/huma/blob/main/error.go> (`ErrorModel`, `StatusError`, `var NewError`, `Error4XX` helpers), `humachi` adapter <https://github.com/danielgtaylor/huma/blob/main/adapters/humachi/humachi.go>, benchmarks <https://huma.rocks/why/benchmarks/>.
- **Connect-Go** — `handler.go` <https://github.com/connectrpc/connect-go/blob/main/handler.go> (`NewUnaryHandler`, `NewUnaryHandlerSimple`), `error.go` <https://github.com/connectrpc/connect-go/blob/main/error.go> (`Error`, `Code`), `code.go` <https://github.com/connectrpc/connect-go/blob/main/code.go> (16-code system).
- **Spring Web** error model — <https://docs.spring.io/spring-framework/reference/web/webmvc/mvc-ann-rest-exceptions.html> (`ResponseStatusException`, `ProblemDetail`).
- **RFC 9457** Problem Details for HTTP APIs — <https://datatracker.ietf.org/doc/html/rfc9457>.
- **Google AIP-193** Errors — <https://google.aip.dev/193>.
- **Go 1.22 ServeMux + generics typed-handler pattern** — <https://www.willem.dev/articles/generic-http-handlers/> (the minimal community reference; the dispatcher generalizes this with auth dimensions + envelope).
- attune code touched: `internal/handlers/console/`, `internal/handlers/ingest.go`, `internal/handlers/lark.go`, `internal/respond/respond.go`, `internal/handlers/console/internal/session/session.go`, `internal/infra/apikey/middleware.go`, `internal/pkg/logext/`, sentinel definitions across `internal/domain/` and `internal/repo/`.
