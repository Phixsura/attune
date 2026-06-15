# CLAUDE.md · Engineering Guidelines for attune

> Apache-2.0 open-source service. Keep it simple, observable, well-tested. Every
> meaningful change is documented in [CHANGELOG.md](CHANGELOG.md) — no exceptions
> outside of pure `docs` / `chore` / `ci` / `test` / `refactor` PRs.

This file is the project-level engineering contract. It applies to humans and to
AI assistants (Claude Code, Cursor, etc.) working on this repository.

---

## 1 · Quality gates (the lines we don't cross)

| Check | Threshold | How |
|---|---|---|
| **Go backend** | | |
| `go vet ./...` | 0 warnings | pre-commit + CI |
| `go build ./...` | 0 errors | pre-commit + CI |
| `go test -race ./...` | All pass on changed code | CI `go-checks` |
| Go module files | `go.mod` / `go.sum` tidy output committed | `go mod tidy && git diff --exit-code go.mod go.sum` |
| `golangci-lint` | 0 findings | CI (`govet`, `depguard`, `bodyclose`, `noctx`, etc.) |
| Function CCN | ≤ 15 | `lizard . -l go -C 15 -T nloc=100 --warnings_only` |
| Function NLOC | ≤ 100 | `lizard . -l go -C 15 -T nloc=100 --warnings_only` |
| Code duplication | < 5% | `npx -y jscpd . -f go -i '**/*.pb.go' -t 5 --silent` |
| Logging facade | no direct `log/slog` in business code | `golangci-lint` depguard `slog-facade` |
| Log fields / outbound HTTP | 0 `lint-slog` findings | `scripts/lint-slog.sh --strict` (rules 2 + 3) |
| Raw pointer ops | 0 bare `*p` deref / `&x` address-of (use `internal/pkg/ptrext`) | `scripts/lint-rawptr.sh` |
| Error response codes | `ErrorResponse.code` comes from the enum | `scripts/lint-errorcode.sh` |
| Integration layout | 0 misplaced integration-tagged tests | `scripts/lint-integration-layout.sh` |
| PostgreSQL integration tests | All pass on Go changes | `make test-integration` in CI |
| **Console (TS / React)** | | |
| `pnpm tsc -b --noEmit` | 0 errors | CI |
| `pnpm biome check` | 0 errors | pre-commit (staged) + CI |
| `pnpm exec vite build` | 0 errors | CI build smoke test |
| `pnpm vitest run --coverage` | All pass + per-file thresholds met (see `console/vite.config.ts`) | CI |
| `pnpm arch` (dependency-cruiser) | 0 violations (shared → features → app, no cross-feature) | CI |
| **Contracts / deploy** | | |
| Proto contract | lint clean, no breaking drift, generated output committed | `buf lint`, `buf breaking`, `buf generate && git diff --exit-code` |
| Docker image | smoke build succeeds | CI `docker-build` |
| Compose config | base + observability overlay parses | `docker compose ... config -q` |
| **Security / supply chain** | | |
| Secrets | no verified or unknown committed secrets | TruffleHog `Secret Scan` workflow |
| Code scanning | CodeQL completes for Go and JS/TS | `CodeQL` workflow |
| Dependency review | no high-severity vulnerable deps; no denied copyleft licenses | `Dependency Review` workflow |
| Workflow security | 0 zizmor findings | `Workflow Lint` workflow |
| **PR process / coverage review** | | |
| Changelog | code PRs update `[Unreleased]` unless exempt | CI `changelog` job |
| PR title | Conventional Commit shape | `Semantic Pull Request` workflow |
| Go coverage regression | aggregate coverage does not decrease unless skipped | `Go Coverage` workflow |
| Console coverage regression | aggregate coverage does not decrease unless skipped | `Console Coverage` workflow |

The pre-commit hook enforces a subset locally. `.github/workflows/ci.yml` is
aggregated by `ci-gate`; dedicated PR workflows run alongside it. Rows are listed
only when an implemented command, script, or workflow owns the check.

Informational / scheduled monitors are not red gates unless branch protection is
changed to require them: PR `govulncheck ./...` is `continue-on-error`; Codecov
uploads are `continue-on-error`; scheduled Govulncheck SARIF and OpenSSF
Scorecard publish findings for follow-up.

---

## 2 · Changelog rule

**Every PR with code change must add an entry under `[Unreleased]` in [CHANGELOG.md](CHANGELOG.md).**

Format follows [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/).

Sections used:
- `### Added` — new features
- `### Changed` — changes in existing functionality
- `### Deprecated` — soon-to-be removed features
- `### Removed` — removed features
- `### Fixed` — bug fixes
- `### Security` — security-relevant fixes

**Skip changelog only** for purely `docs/`, `chore/`, `ci/`, `test/`, or
`refactor/` changes (still call this out explicitly in the PR description).

### On release

1. Move `[Unreleased]` content into a new `[X.Y.Z] - YYYY-MM-DD` section.
2. Update the comparison links at the bottom of CHANGELOG.md.
3. Create a git tag `vX.Y.Z` on the merge commit.
4. The release workflow auto-builds + pushes the image and creates a GitHub
   Release whose notes are pulled from the corresponding CHANGELOG section.

---

## 3 · Versioning

[Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html):

- **MAJOR** — breaking API / config field / DB schema / image entrypoint change
- **MINOR** — backwards-compatible new feature
- **PATCH** — backwards-compatible bug fix

Pre-1.0: minor bumps **may** be breaking; we'll flag this in the changelog
explicitly. Beyond 1.0, breaking == major bump only.

---

## 4 · Conventional commits

Use [Conventional Commits](https://www.conventionalcommits.org/) for commit
subjects. The `type(scope): subject` shape is required for release-note
generation later.

```
feat(enricher): tenant-configurable module list
fix(notify): retry transient 5xx on raw webhook
docs(deploy): add private-deploy quickstart
chore(ci): bump lizard to 1.18
refactor(repo): extract Tenant.Resolve helper
test(handlers/console): add 401 path for missing api key
ci: switch govulncheck to 1.1.4
```

Scope is the package or area, lowercase, dash-allowed (`enricher`, `notify`,
`infra/llmclient`, `console`, `deploy`, …).

---

## 5 · Package layering

```
handlers  →  service  →  repo
                       →  notify (transport in infra)
                       →  infra/llmclient
handlers  →  domain  (pure types, any direction)
handlers  →  infra/apikey (middleware) → service (via Verifier interface)
```

Inside each layer, files are grouped into **feature subpackages** (hybrid
layout, gitea pattern — see `docs/proposals/2026/06/2026-06-06-feature-organization.md`):

```
service/  enrich/  ingest/  outbox/  apikey/  eval/
repo/     feedback/  apikey/  outbox/  notifytarget/  tenant/  admin/  inboundsource/
notify/   adapter/{rawwebhook,githubissue}/   (transport stays at root)
inbound/  (framework root)  adapter/{webhook,email}/  inboundtest/
```

`internal/service/apikey` and `internal/repo/apikey` collide on package name;
importers needing both alias the repo side as `apikeyrepo` (same for
`outboxrepo` and `inboundsourcerepo` where they collide).

Cross-layer rules — a violation is a rejection-grade lint:

- `handlers` never writes SQL.
- `service` never writes HTTP.
- `notify` never imports `service`.
- `infra` never imports `service` or `repo`.

### Inbound framework (#66) — extra rules

`internal/inbound/` is the channel-agnostic ingest framework. Adapters
self-register via `init()` and are blank-imported by `cmd/attune` only.
The CI depguard rules (`.golangci.yml` → `inbound-boundary` +
`inbound-framework-isolation`) enforce:

- `inbound` (framework root) doesn't import `service` / `repo` /
  `handlers` / `notify`. `IngestPort` / `SourceStore` / `SecretStore` are
  declared as interfaces in `inbound`; `cmd/attune` injects the
  implementations.
- `inbound/adapter/<channel>` may import `inbound` (the framework root)
  but NEVER any sibling adapter package, never `service` / `repo` /
  `handlers` / `notify` / `infra` / `domain` (except `domain.IngestInput`
  used in the port signature).
- The only legal blank-import site for any `internal/inbound/adapter/*`
  package is `cmd/attune`. `handlers/console/inbound/` is the documented
  exception — it calls `webhook.RotateSecret` as the operator-side
  rotation primitive, analogous to how `cmd/attune` calls
  `Manager.StartAll`.
- The framework absorbs four execution modes — push (webhook), poll
  (email IMAP / RSS), schedule (cron crawler), stream (MQ / WebSocket /
  firehose) — behind the same `Adapter { Channel, Start, Shutdown }`
  port. Adding a channel = adding a package + one blank-import line +
  one `domain.ValidSources` entry (#95 will fold the third away).

For the full layout, see [`README.md`](README.md).

---

## 6 · When in doubt

1. **Read existing code in the same package first.** Match the established
   pattern unless you have a clear reason not to.
2. **If 2+ similar implementations exist, abstract before adding the 3rd.**
3. **Write tests with new code.** No exceptions for "easy" changes.
4. **Surface assumptions in the PR description**, especially about input
   shape, concurrency, or failure modes.
5. **If the change is bigger than a paragraph in the commit message, open
   an issue first** to align on scope before you build it.

---

## 7 · Observability conventions

- All logs go through `logext.Infof` / `logext.Warnf` / `logext.Errorf`
  with `ctx` as the first argument. Three levels only (no Debug — #48):
  if a "Debug" record is worth shipping to ops, it's an `Info`; if it's
  not, it shouldn't be in the code. Business code never imports `log/slog`
  directly — golangci-lint's depguard `slog-facade` rule (`.golangci.yml`)
  enforces this; only `internal/pkg/logext` + `internal/infra/observability`
  (the facade internals that own the `trace_id`/`span_id` injection
  contract) may touch `log/slog`.
- The slog default is installed by `observability.InstallDefaultLogger()`
  at startup; it wraps a JSON/text handler in `TraceIDHandler` so every
  record carries `trace_id`/`span_id` from the active OTel span on ctx.
- HTTP clients hitting external services must wrap their transport with
  `otelhttp.NewTransport(http.DefaultTransport)`.
- New metrics live in `internal/infra/metrics` and follow the existing
  naming pattern (`attune_<area>_<thing>_<unit>`).
- Sensitive fields (API keys, secrets, raw token values) are never logged
  in clear text. Hash or `truncate()` them at the call site.

---

## 7b · Pointer hygiene — `internal/pkg/ptrext`

Reading and creating pointers goes through `internal/pkg/ptrext`. Bare
`*p` dereference and `&x` address-of are flagged by `scripts/lint-rawptr.sh`
and blocked in CI.

| Pattern | Use |
|---|---|
| `s := V; obj.F = &s` | `obj.F = ptrext.Of(V)` |
| `&MyStruct{…}` | `ptrext.Of(MyStruct{…})` |
| `v := *p` | `v := ptrext.Indirect(p)` (nil → zero) |
| `if p != nil { x = *p } else { x = fb }` | `x := ptrext.IndirectOr(p, fb)` |

Cases the lint explicitly allows — wrapping would break correctness, so
they stay raw:

- `*T` in any **type** position (params / return / struct field / type
  assertion / method receiver).
- `*p = v` on the LHS of an assignment (Go has no expression form for
  "addressable indirect").
- `&xs[i]` addressing a slice element.
- `&x` passed to a known **out-parameter** API (`json.Unmarshal`,
  `*Row.Scan`, `flag.*Var`, `errors.As`, `encoding/binary.Read`,
  attune's `postJSON`, …). The matched callee name list is in
  `cmd/lint-rawptr/main.go` — add new helpers there rather than
  per-call markers.

When wrapping would copy an identity-bearing value (a `sync.Mutex`
embedded in a proto message, a `strings.Builder` you're still writing
to, a captured `*[]byte` write-back slot), escape per-line with
`// ptrext:allow <one-word reason>`. For files where the whole pattern
is endemic — config-binding tables, test mock fixtures — use the
file-level directive `// ptrext:file-allow <one-line reason>` at the
top of the file.

---

## 8 · Security baseline

- No new external dependencies without a PR-described justification (bundle
  cost, activity, alternatives considered).
- Customer-facing webhook secrets must be ≥ 16 chars (enforced in config).
- HTTPS for all outbound customer URLs; loopback `http://127.0.0.1` /
  `localhost` / `[::1]` are the documented exemptions.
- Process runtime config is one private YAML file selected by `--config`; never
  commit real deploy config with live database URLs, signing keys, Tink keysets,
  or bootstrap credentials.
- LLM provider API keys are write-only inputs through Console/API/CLI and are
  persisted encrypted with `secrets.tink_keyset`; never store provider keys in
  process YAML or committed fixtures.
- Console `dev_login` + `insecure_cookies` flags are HTTP-only test loops;
  combined check refuses startup if either is set in a TLS-fronted deploy.

---

## 9 · For AI assistants specifically

When Claude / Cursor / similar tooling is editing this repo:

- The **changelog rule** is not optional. Update `CHANGELOG.md` in the same
  commit / PR that introduces the change.
- Add tests for new behavior — don't claim "I verified manually" without
  evidence in the PR description.
- Never bypass pre-commit hooks or CI gates to "make red go green." Fix the
  underlying issue.
- **Every issue gets a proposal** (§10) written before/with the code.

---

## 10 · Proposals (one per issue)

**Every issue we work on gets a short design proposal, written before/with the
implementation and committed alongside the change.**

- **Location / naming:** `docs/proposals/YYYY/MM/YYYY-MM-DD-<slug>.md` — the date
  prefix is when the proposal was *started*; year/month directories keep the tree
  browsable as it grows.
- **Header:** a table with `Issue` (#N), `Status` (`Proposed` → `Accepted` →
  `Implemented`), `Started` (timestamp), and `Related` issues.
- **Sections** (ADR/RFC-lite): Problem → Goals / Non-goals → Proposal →
  Alternatives considered → Risks / tradeoffs → Implementation plan →
  Verification → References.
- **Association:** the proposal links its issue; the implementing PR uses
  `Closes #N` and includes the proposal doc in the same change.

This is where assumptions get surfaced and alternatives weighed *before* writing
code (see §6). Update the `Status` as the work lands.

---

## 11 · Proto IDL contract

The HTTP request/response contract is defined in `.proto` (`proto/attune/v1/`)
and code-generated — not hand-written (#19):

- **To change an HTTP shape: edit the `.proto`, run `make proto`, then commit
  both the proto and the regenerated Go / TS / OpenAPI.** Never hand-edit
  generated files (`internal/proto/**`, `console/src/proto/**`, `docs/openapi/**`).
- `make proto` needs `buf` (https://buf.build/docs/installation); plugins run as
  buf remote plugins, so no local `protoc-gen-*` installs are required.
- CI's `proto-sync` job runs `buf generate` and fails if the committed output
  drifts. The migration is incremental — one endpoint per PR; `/v1/lark/event`
  stays off the contract (it consumes Lark's external event format — see #66).

---

For project-level architecture, overview, and quickstart, see
[README.md](README.md). For private deployment, see
[`docs/private-deploy.md`](docs/private-deploy.md).
