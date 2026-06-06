# Adopt protobuf as the HTTP contract IDL — complete migration (Go + TS + OpenAPI)

| | |
|---|---|
| **Issue** | #19 |
| **Status** | Accepted |
| **Started** | 2026-06-06 |
| **Related** | #66 (inbound adapter framework — relies on the channel-agnostic contract; deferred), #10 (per-tenant enrich-config types ride on this), #36/#37 (Go/TS SDKs generated from the contract), #28/#29/#30 (new console endpoints born proto-native), #15 (CI quality-gate doc), #1/#4 (CI / lint patterns) |

## Problem

The HTTP boundary has **no shared schema source of truth** (verified against `main`):

- Handlers hand-decode JSON into ad-hoc Go structs — e.g. `domain.IngestInput` (`internal/domain/feedback.go:78-84`); the ingest response is an anonymous `map` (`internal/handlers/ingest.go:76-79`); error bodies are `{"error": msg}` (`ingest.go:39/47/67`).
- The console derives TS through a **separate** pipeline: a hand-written `internal/handlers/console/openapi.yaml` → `openapi-typescript` → `console/src/api/types.ts` (`pnpm gen:api`).
- The two sides drift silently; every new endpoint needs synchronized edits across Go structs, TS types, and docs.

## Goals

- One source of truth — `.proto` — generating **Go types** (handlers), **TS types** (console), and **OpenAPI** (docs / future SDKs) for the **entire** canonical HTTP contract.
- **buf breaking-change detection** in CI so the wire contract can't silently break.
- **Complete migration:** every endpoint of our canonical contract moves to proto, and the hand-written console `openapi.yaml` + `openapi-typescript` are **retired**.
- Preserve the existing wire format except the deliberate, fleet-wide changes (Decisions 1–2).

## Non-goals

- **gRPC / Connect-RPC.** We stay on chi + JSON-over-HTTP; proto messages are decoded from JSON via `protojson` at the existing paths.
- **proto enums for the domain value sets** (Severity / Kind / Source / Audience) — kept as `string` (Decision 3).
- **`/v1/lark/event`.** Excluded — it consumes **Lark's external event format** (encrypt/challenge/event…), not our contract. Proto-defining it would weld Lark's schema into our IDL, the opposite of the adapter-isolation goal. It stays an adapter boundary (#66).
- **The Lark source de-rooting** (`lark-*` → `lark` + `source_meta`) — owned by #66 (deferred). `source` stays a proto `string`; `ValidSources` (incl. `lark-*`) and the Lark handler are untouched here.

## Scope — endpoints migrated

| Endpoint | Proto messages |
|---|---|
| `POST /v1/feedback/ingest` | `IngestRequest`, `IngestResponse` |
| `GET /fb/v1/console/me` | `MeResponse` |
| `GET/POST/DELETE /fb/v1/console/api-keys` | `APIKey`, `ListAPIKeysResponse`, `CreateAPIKeyRequest`, … |
| `GET /fb/v1/console/feedback` (+ `/stats`, `/{id}`) | `Feedback`, `ListFeedbackResponse`, `FeedbackStats`, … |
| `GET/POST/PATCH/DELETE /fb/v1/console/notify-targets` (+ `/test`) | `NotifyTarget`, `ListNotifyTargetsResponse`, … |
| `GET /fb/v1/console/usage` | `UsageResponse` |
| Shared | `ErrorResponse`, `Tenant`, … |

**Excluded:** `/v1/lark/event` (foreign format → #66).

## Design

### Tooling & layout
- `.proto` under `proto/attune/v1/`. `buf.yaml` (lint + breaking; **depends on `googleapis`** for `google.api.http` + `google.api.field_behavior`) + `buf.lock`; `buf.gen.yaml` (**managed mode** for `go_package`; **pinned plugin versions**).
- Plugins: `protoc-gen-go`, **`ts-proto`** (`onlyTypes`, `forceLong=string`), `protoc-gen-openapi`. Versions pinned for reproducibility; `make proto` runs them as buf **remote** plugins (only `buf` is needed locally). ts-proto ignores the `google.api.http` / `field_behavior` options, so they stay in the proto for OpenAPI paths without breaking the TS (see Alternatives).
- Generated artifacts (**committed, never hand-edited**): Go → `internal/proto/attune/v1/`; TS → `console/src/proto/`; OpenAPI → `docs/openapi/openapi.yaml`.
- A **`Makefile`** `proto` target (`make proto` = `buf generate` + `buf lint`); `pnpm gen:proto` delegates to it; Dev-README documents installing `buf`. (ts-proto's `onlyTypes` output is self-contained — no TS runtime dependency in the console.)
- `google.golang.org/protobuf` is already in `go.mod` — no new Go runtime dep.

### Per-endpoint shape
- Each endpoint is a `service` + `rpc` annotated with `google.api.http` (e.g. `post: "/v1/feedback/ingest" body: "*"`). We do **not** generate server stubs — chi routing stays — but the annotation lets **protoc-gen-openapi** emit the REST paths + operations.
- Handlers decode with `protojson.Unmarshal` (Decision 6) and encode with `protojson.Marshal`; error bodies use `ErrorResponse` (Decision 7). Existing validation, metrics, size caps, and observability are preserved verbatim.

### Decisions
1. **int64 ids → JSON strings** (protoJSON rule). `id` becomes `"123"`. Applied fleet-wide (ingest response, feedback ids, …). JS-safe; pre-1.0; flagged in CHANGELOG.
2. **Converge wire naming on camelCase.** protojson (and ts-proto's JSON field names) default to camelCase. Public ingest is already camelCase; **every console endpoint converges snake_case → camelCase** (`user_id`→`userId`, …) as it migrates — the SPA + backend regenerate together (internal, coordinated), eliminating today's split.
3. **Domain value sets stay `string`, not proto enums.** Their lowercase/hyphen wire values (`P0`, `bug`, `lark-group`) can't be clean proto-enum identifiers and migrating them would ripple through the DB / LLM parser / dashboards. Allowed-sets stay in the Go validators; surfaced to docs as OpenAPI string-enums. #19 delivers canonical **message shapes**, not enums.
4. **`field_behavior=REQUIRED` is contract/doc-only** — annotates OpenAPI; runtime validation stays in handlers.
5. **`source` is a channel-agnostic `string`.** #19 does not enshrine any channel; the `lark-*` de-rooting + edge legacy-alias map are #66 (deferred).
6. **`protojson.UnmarshalOptions{DiscardUnknown: true}`.** Today's `json.Decode` is lenient (ignores unknown fields); protojson is strict by default. DiscardUnknown preserves leniency so clients sending extra fields don't break.
7. **`ErrorResponse { string error }` in proto.** The ingest error body is `{"error": …}`; modeling it keeps OpenAPI complete (200 **and** 4xx/5xx). (The console's richer `{code, message, request_id}` envelope is folded in as its endpoints migrate.)

### Execution — incremental, even though complete
The migration ships as a **sequence of small PRs under #19**, never one giant PR; old and new paths coexist throughout:
1. **Foundation + ingest** — proto/buf/Makefile, `common.proto` (`ErrorResponse` + shared), `ingest.proto`, codegen, ingest handler on protojson, CI gates.
2. **Each console endpoint** — one PR each (proto messages, handler on protojson with camelCase + int64→string, console TS switched to generated types).
3. **Cleanup** — delete `console/openapi.yaml` + the `openapi-typescript` dep; OpenAPI becomes docs-only.

## Alternatives considered

- **OpenAPI-first** (hand-maintain OpenAPI → oapi-codegen + the existing openapi-typescript). Lighter, reuses the console pipeline. Rejected: weaker cross-language IDL + breaking-detection than buf; the maintainer wants proto as the multi-client contract spine.
- **TS via `protoc-gen-es` (protobuf-es).** Conformance-tested + first-party to buf, but it emits imports for the `google/api` option types referenced by `google.api.http` / `field_behavior` — which have no protobuf-es runtime package — breaking the console typecheck unless the annotations are dropped (losing OpenAPI paths) or the `google/api` TS is hand-vendored. **Reversed to ts-proto during implementation** when this surfaced.
- **proto enums for domain values.** Rejected — Decision 3.
- **Per-route dual naming codecs** (keep console snake_case). Rejected — converging on camelCase is the cleaner end-state.

## Risks / tradeoffs

- **Magnitude.** This is "rewrite the entire HTTP boundary + the console data layer," ~5 working days across Go + the whole console SPA. Mitigated by the strict per-endpoint incremental execution (each PR independently reviewable + revertible).
- **console snake→camel regression surface.** Every console field name changes; mitigated by regenerating types (compiler catches drift) + per-endpoint review.
- **DiscardUnknown** is load-bearing (Decision 6) — without it, lenient clients break.
- **int64 wire change** (Decision 1) — deliberate, documented.
- **New toolchain** (buf) + generated-file PR noise — `make proto`, dev-README, review-path exclusions.
- **ROI back-loaded** — pays off via the SDKs (#36/#37) and multi-client integrations; accepted (pillar-platform, "对接多端").

## Implementation plan

**PR 1 — foundation + ingest:** `proto/attune/v1/{common,ingest}.proto` (service + `google.api.http`; `ErrorResponse`); `buf.yaml`/`buf.lock`/`buf.gen.yaml`; `Makefile` `proto`; `pnpm gen:proto`; `@bufbuild/protobuf` dep; commit generated Go/TS/OpenAPI; migrate `ingest.go` to protojson (`DiscardUnknown`), preserving validation/metrics/obs; CI `buf lint` + `buf breaking` + `proto-sync` into `ci-gate` + paths-filter; round-trip + golden + DiscardUnknown tests; update `deploy/README.md` curl (id→string); CLAUDE.md workflow note + §5 proto layer; CHANGELOG `### Added` + `### Changed` (id→string).

**PRs 2…n — per console endpoint:** proto messages; handler on protojson (camelCase, int64→string); console TS → generated types; per-endpoint wire/contract test.

**Final PR — cleanup:** delete `console/openapi.yaml` + `openapi-typescript`; OpenAPI docs-only; CLAUDE.md updated.

## Verification

- `make proto` idempotent (`git diff --exit-code` clean — the CI `proto-sync` gate).
- ingest: today's JSON body still 200s; response `id` is now a string; a body with extra fields still 200s (DiscardUnknown).
- each migrated console endpoint: generated TS type-checks (`pnpm tsc`); field names are camelCase; ids are strings.
- `go build ./... && go test ./...` green; `buf lint`/`buf breaking`/`proto-sync` green; OpenAPI valid (paths + 2xx/4xx/5xx).

## References

- buf (lint/breaking/generate): <https://buf.build/docs>
- ts-proto: <https://github.com/stephenh/ts-proto>
- protoJSON mapping (int64→string, enum names, camelCase): <https://protobuf.dev/programming-guides/json/>
- protoc-gen-openapi: <https://github.com/grpc-ecosystem/grpc-gateway>
- Code: `internal/handlers/ingest.go`, `internal/domain/feedback.go:26-99`, `internal/handlers/console/`, `console/src/api/`, `cmd/attune/router.go`.
