# OpenAPI Error Contract Completeness

| | |
|---|---|
| **Issue** | N/A (platform gap review follow-up) |
| **Status** | Implemented |
| **Started** | 2026-06-28 |
| **Related** | #19 (proto IDL contract), #168 (public management APIs and SDK coverage) |

---

## Problem

attune's generated public OpenAPI document is not self-contained for error
handling today.

The current state has two user-visible problems:

1. `docs/openapi/openapi.yaml` points default non-2xx responses at
   `google.rpc.Status`, even though attune's real HTTP handlers emit the
   unified `{code, message, requestId}` `ErrorResponse` envelope.
2. `ErrorResponse` does not appear in the generated schema set, so
   `docs/openapi/README.md` has to carry a supplementary contract explaining the
   real wire shape.

That is good enough for maintainers who already know the codebase, but not good
enough for external SDK authors, API consumers, or contract validation tools.

## Goals

- Make `docs/openapi/openapi.yaml` self-contained for attune's HTTP error model.
- Ensure default error responses reference attune's `ErrorResponse`, not
  `google.rpc.Status`.
- Ensure explicit 4xx/5xx responses that today only carry descriptions also
  declare the shared JSON error schema.
- Keep `make proto` deterministic so the `proto-sync` CI gate still guarantees
  committed artifacts match the generation path.

## Non-goals

- Do not redesign the wire error envelope.
- Do not introduce a second hand-maintained OpenAPI source.
- Do not attempt a full custom OpenAPI generator replacement.
- Do not solve attune-wide API versioning in this change.

## Proposal

Add a deterministic post-processing step after `buf generate` in `make proto`.

The post-processor will:

1. Parse `docs/openapi/openapi.yaml`.
2. Inject an `ErrorResponse` schema under `components.schemas` if it is absent.
3. Rewrite default JSON error responses that currently point to `Status` so
   they point to `ErrorResponse`.
4. Add JSON `ErrorResponse` content to explicit 4xx/5xx responses that currently
   only have a description.
5. Preserve the rest of the generated document byte-stably as much as possible.

This keeps proto as the single source of truth for shapes and paths while
acknowledging a generator limitation around shared error envelopes.

## Alternatives considered

### 1. Leave the README supplement in place

Rejected. The spec would remain machine-incomplete, which is the core problem.

### 2. Hand-edit `docs/openapi/openapi.yaml`

Rejected. That violates the generated-artifact contract and would fight the
`proto-sync` CI gate on every regen.

### 3. Replace the current OpenAPI generator

Rejected for now. It is a much larger migration than the gap requires and would
mix a tooling-platform change with a contract-correctness fix.

## Risks / tradeoffs

- The post-processor is one more step in `make proto`, so its behavior must be
  tested and deterministic.
- The generated OpenAPI file now has a tiny amount of attune-owned repair logic
  layered on top of buf output. That is acceptable because the repair is
  mechanical, committed, and covered by tests.

## Implementation plan

1. Add `internal/tools/openapipatch` with a deterministic YAML patcher and unit
   tests.
2. Call the tool from the `proto` Makefile target after `buf generate`.
3. Regenerate `docs/openapi/openapi.yaml`.
4. Update `docs/openapi/README.md` so it documents the post-processed contract
   rather than a missing schema workaround.
5. Update `CHANGELOG.md`.

## Verification

- `go test ./internal/tools/openapipatch`
- `make proto`
- `git diff --exit-code docs/openapi/openapi.yaml` after a second `make proto`
- Spot-check that `/v1/feedback/ingest` and a few management endpoints reference
  `#/components/schemas/ErrorResponse` for non-2xx responses.

## References

- Proto/OpenAPI contract adoption proposal:
  `docs/proposals/2026/06/2026-06-06-protobuf-idl-contract.md`
- Dispatcher / unified error-envelope proposal:
  `docs/proposals/2026/06/2026-06-09-http-dispatcher.md`
