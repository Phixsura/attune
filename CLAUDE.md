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
| `go vet ./...` | 0 warnings | pre-commit + CI |
| `go build ./...` | 0 errors | pre-commit + CI |
| `go test -short ./...` | All pass on changed code | CI |
| Function CCN | ≤ 15 | `lizard . -l go -C 15` |
| Function NLOC | ≤ 100 | `lizard . -l go -T nloc=100` |
| Code duplication | < 2% | `npx -y jscpd . --pattern '**/*.go' --threshold 5` |
| Internal info | 0 leaks (IPs, /opt paths, brand names) | grep |
| Outbound HTTP clients | must wrap with `otelhttp.NewTransport` | `scripts/lint-slog.sh` Rule 3 |
| Logging | `logext.*` + `ctx` first | `scripts/lint-slog.sh` Rule 1 |

The pre-commit hook enforces a subset locally; CI enforces all. Red gates =
PR cannot merge.

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
layout, gitea pattern — see `docs/proposals/2026-06-06-feature-organization.md`):

```
service/  enrich/  ingest/  outbox/  apikey/  eval/
repo/     feedback/  apikey/  outbox/  notifytarget/  tenant/  lark/
notify/   adapter/{rawwebhook,larkwebhook,githubissue}/   (transport stays at root)
```

`internal/service/apikey` and `internal/repo/apikey` collide on package name;
importers needing both alias the repo side as `apikeyrepo` (same for `outboxrepo`
and `larkrepo` where it collides with `internal/infra/lark`).

Cross-layer rules — a violation is a rejection-grade lint:

- `handlers` never writes SQL.
- `service` never writes HTTP.
- `notify` never imports `service`.
- `infra` never imports `service` or `repo`.

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

- All logs use `logext.Infof` / `logext.Warnf` / `logext.Errorf` with `ctx`
  as the first argument.
- HTTP clients hitting external services must wrap their transport with
  `otelhttp.NewTransport(http.DefaultTransport)`.
- New metrics live in `internal/infra/metrics` and follow the existing
  naming pattern (`attune_<area>_<thing>_<unit>`).
- Sensitive fields (API keys, secrets, raw token values) are never logged
  in clear text. Hash or `truncate()` them at the call site.

---

## 8 · Security baseline

- No new external dependencies without a PR-described justification (bundle
  cost, activity, alternatives considered).
- Customer-facing webhook secrets must be ≥ 16 chars (enforced in config).
- HTTPS for all outbound customer URLs; loopback `http://127.0.0.1` /
  `localhost` / `[::1]` are the documented exemptions.
- Database connection strings, LLM API keys, signing secrets — only via env
  vars or mounted files. Never committed to YAML.
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

- **Location / naming:** `docs/proposals/YYYY-MM-DD-<slug>.md` — the date prefix
  is when the proposal was *started*, so `ls docs/proposals` reads as a timeline.
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
[README.md](README.md). For private deployment, see `docs/private-deploy.md`
(coming in a follow-up issue).
