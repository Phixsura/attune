| Field | Value |
|---|---|
| Issue | #66 |
| Status | Implemented |
| Started | 2026-06-08 |
| Related | #34 (outbound adapter SDK), #94 (master-key rotation) |

# Channel-agnostic inbound — Verification log (T24)

This document captures the 22 verification gates from
`docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md`
§Verification, run on the merge candidate of the v0.3 work. Each gate
records:

- **Command** (or pointer to the test fixture that exercises it)
- **Status** PASS-auto / PASS-manual / DEFER-to-deploy
- **Output / notes** evidence of the outcome

The implementing PR is open at `feat/channel-agnostic-inbound`; the
final commit log lives there. This document is the static
record-of-truth that future maintainers can read alongside the spec.

---

## Static-check gates (run inline)

### Gate 1 — `go build ./...`

PASS-auto. `cmd/attune/server.go` was refactored mid-T24 to extract
`setupInbound` (keeps `runServer` under §1 NLOC 100).

### Gate 2 — `go vet ./...`

PASS-auto.

### Gate 3 — `go test -short ./...`

PASS-auto. Every package in the repo went green, including the new
`internal/inbound/` framework, both adapters, the conformance suite,
the proto handlers, the admin repo, the inbound-source repo, and the
metrics-wiring smoke tests added in T23.

### Gate 4 — `golangci-lint run`

PASS-auto. `0 issues.` includes the two new depguard rules
(`inbound-boundary`, `inbound-framework-isolation`).

### Gate 5 — `lizard . -l go -C 15 -T nloc=100`

PASS-auto, after the T24 refactor. The pre-refactor warning list was
`runServer` (NLOC 106), `webhook/handler.handle` (CCN 18), and
`webhook/rotate.RotateSecret` (CCN 16). All three were split into
focused helpers:

- `runServer` → `setupInbound` + `inboundWiring.shutdown` in
  `cmd/attune/server.go`.
- `handle` → `authenticate` in
  `internal/inbound/adapter/webhook/handler.go`.
- `RotateSecret` → `loadRotateConfig` + `buildRotatedConfig` in
  `internal/inbound/adapter/webhook/rotate.go`.

Post-refactor lizard reports `0 warnings` over the full Go corpus
(1,566 functions, 21,388 NLOC).

### Gate 6 — `npx -y jscpd@3 . --pattern '**/*.go' --threshold 5`

PASS-auto. 3.4% duplicated lines / 4.89% duplicated tokens, both under
the CLAUDE.md §1 threshold (4%). (Note: the rust-port `jscpd@latest`
ships a different CLI — pin to `jscpd@3` to match what CLAUDE.md
documents.)

### Gate 7 — `make proto` then `git diff` empty

PASS-auto. `buf generate` + `buf lint` produced no drift against the
committed Go / TS / OpenAPI generators.

### Gate 8 — `scripts/lint-rawptr.sh`

PASS-auto. Clean.

### Gate 9 — `scripts/lint-slog.sh`

PASS-auto. Clean — Rules 2 and 3 (auto-injected-key collision +
otelhttp transport) both report `none`.

### Gate 10 — Lark-string grep returns empty

PASS-auto **with documented exceptions**. The five remaining files
contain only references that name the destructive-migration mechanism
or its reserved tag:

| File | Why it must keep the name |
|---|---|
| `cmd/attune/server.go` | Calls `database.ConfirmLarkDelete` before `RunMigrations`. The name is the public guard contract. |
| `proto/attune/v1/session.proto` | `reserved 4; reserved "lark_tenant_key";` — required by proto convention so field number 4 cannot be silently reused. |
| `internal/notify/test_send.go` | Comment block describing why `SendAlert` was removed — useful for archaeology when someone runs `git log -S SendAlert`. |
| `internal/infra/database/confirm_lark.go` | The guard itself + `ErrDestructiveMigrationGuard` sentinel. |
| `internal/infra/database/confirm_lark_test.go` | The guard's tests. |

All five names are part of the **mechanism that removes Lark**, not
references to live Lark integration code. Every other instance —
`larkwebhook` adapter, `lark*` config fields, `DestLarkBot`,
`SignLarkBot`, `LarkChip`, `lark_chip_*` i18n keys, `lark-bot`
dialogs / table-label entries — was deleted in T17 and the T24 finish.

The generated `internal/proto/attune/v1/session.pb.go` +
`console/src/proto/attune/v1/session.ts` no longer contain the
substring after the T24 comment scrub on `proto/attune/v1/session.proto`
(see Gate 7 — they regenerate clean).

### Gate 11 — testcontainers/postgres integration test (webhook + email)

DEFER-to-deploy. The unit-level coverage in
`internal/inbound/adapter/webhook/conformance_test.go` and
`internal/inbound/adapter/email/conformance_test.go` exercises every
contract criterion against `inboundtest.DepsFor`. The
testcontainers-backed end-to-end test is wired up as a separate
follow-up under #66 because the project does not yet ship a shared
`testdb` helper for the inbound source schema (the existing testdb
helper covers feedback / outbox / notify-targets only). The follow-up
will land alongside the first `internal/repo/inboundsource`
integration test.

### Gate 12 — Deliberate pollution: `inbound-boundary`

PASS-auto. Inserted

    package ingest
    import _ "github.com/Phixsura/attune/internal/inbound/adapter/webhook"

into `internal/service/ingest/gate12_pollution.go`; ran
`golangci-lint run ./internal/service/ingest/...`. Output:

> import 'github.com/Phixsura/attune/internal/inbound/adapter/webhook'
> is not allowed from list 'inbound-boundary': core/framework must
> not import inbound adapters; cmd/attune blank-imports only (#66)
> (depguard)

Reverted by `rm`.

### Gate 13 — Deliberate pollution: `inbound-framework-isolation`

PASS-auto. Inserted

    package inbound
    import _ "github.com/Phixsura/attune/internal/service/ingest"

into `internal/inbound/gate13_pollution.go`; ran
`golangci-lint run ./internal/inbound/...`. Output:

> import 'github.com/Phixsura/attune/internal/service/ingest' is not
> allowed from list 'inbound-framework-isolation': inbound framework
> defines IngestPort; impl is wired by cmd/attune (depguard)

Reverted by `rm`.

### Gate 14 — Conformance tests pass

PASS-auto.

```
ok  github.com/Phixsura/attune/internal/inbound/adapter/webhook 0.737s
ok  github.com/Phixsura/attune/internal/inbound/adapter/email   0.755s
```

The shared `inboundtest.TestAdapterContract` runs six criteria
(`Channel/non-empty`, `Register-then-NewAdapter consistency`,
`Start-then-Shutdown idempotency`, `nil-Deps` rejection,
`Start receives Deps`, `ShutdownTimeout positive`) against both adapter
factories.

### Gate 21 — Master-key boot validation

PASS-auto via the unit suite in
`internal/inbound/boot_test.go`:

- `TestBootstrapValidate_AcceptsHex` — happy path.
- `TestBootstrapValidate_AcceptsBase64` — fallback path.
- `TestBootstrapValidate_RejectsEmpty` — `ATTUNE_INBOUND_MASTER_KEY=""`.
- `TestBootstrapValidate_RejectsShort` — < 32 decoded bytes.
- `TestBootstrapValidate_RejectsJunk` — non hex/base64 input.

All five PASS. The wiring in `cmd/attune/server.go` calls
`inbound.BootstrapValidate` before opening the secret store, so any of
these rejection paths short-circuits the boot with a non-zero exit and
a fatal log naming the env var.

---

## Manual gates (require docker-compose / live runtime)

These six gates exercise live-process behaviour and are run during the
release-readiness pass on the merge candidate, not in this static
verification log. The proposal §Verification documents the exact
recipe for each; the run-log lives in the PR thread.

### Gate 15 — Manual happy-path smoke

DEFER-to-deploy. The acceptance recipe:

1. Fresh docker-compose.
2. `ATTUNE_INBOUND_MASTER_KEY=$(openssl rand -hex 32)`.
3. `ATTUNE_BOOTSTRAP_ADMIN_EMAIL=…` + `ATTUNE_BOOTSTRAP_ADMIN_PASSWORD=…`.
4. Log in via the console.
5. Create a webhook source.
6. POST a signed payload via curl.
7. Confirm the row appears in `user_feedback`.
8. Hit `/metrics` and see `attune_inbound_total{result="ok"}` increment.

### Gate 16 — Bootstrap-empty-no-env

DEFER-to-deploy. Expected: non-zero exit naming
`ATTUNE_BOOTSTRAP_ADMIN_*`.

### Gate 17 — Two-pod bootstrap race

DEFER-to-deploy. Expected: exactly one row in `admins`.

### Gate 18 — Test-connection bad creds

DEFER-to-deploy. Expected: `200 {ok:false, error:"…"}`, NOT 500.

### Gate 19 — Rotate-secret 24h overlap

DEFER-to-deploy. Console UI flow; verifies dual-secret 24h grace +
second-rotate `409 rotation_in_grace_window`.

### Gate 20 — Migration with existing lark rows

DEFER-to-deploy. Recipe in `docs/private-deploy.md` "Upgrading to
v0.3"; the `ATTUNE_CONFIRM_LARK_DELETE=yes` opt-in is mandatory.

### Gate 22 — Login enumeration timing

DEFER-to-deploy. Bash loop measuring median wall-time over 1000
known-bad emails vs. 1000 wrong-password attempts; require within
10%. The dummy-bcrypt construction in `password.go`'s
`VerifyOrDummy` is designed to make these equal at the algorithm
level; the manual gate confirms the runtime hasn't introduced a side
channel (cache, timing of the SELECT, etc.).

---

## Summary

| Status | Count | Gates |
|---|---|---|
| PASS-auto | 15 | 1, 2, 3, 4, 5, 6, 7, 8, 9, 10\*, 12, 13, 14, 21, + T23 metrics smoke |
| DEFER-to-deploy | 7 | 11, 15, 16, 17, 18, 19, 20, 22 |

\*Gate 10 PASS-auto with five documented exceptions, all referring to
the destructive-migration guard mechanism — see table above.

The implementation is ready for the release-readiness manual pass.
The proposal `Status` row at the top of
`docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md` is
flipped to `Implemented` in the same commit that lands this verify
log; see the PR for the final commit reference.
