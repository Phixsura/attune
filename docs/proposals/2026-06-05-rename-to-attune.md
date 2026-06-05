# Proposal — project-wide rename `listen` → `attune`

| | |
|---|---|
| **Issue** | #8 (retargeted) |
| **Status** | Implemented |
| **Started** | 2026-06-05 11:30 CST |
| **Related** | #2 (release → `ghcr.io/phixsura/attune`, unblocked by this), #5 (compose) |

## Problem

The GitHub repo was renamed `listen-feedback` → **`attune`**, but the code and
docs still carried the old identity under *three* inconsistent names:

| Surface | Old name |
|---|---|
| Go module (`go.mod`) | `github.com/Phixsura/listen` |
| Issue/CHANGELOG/image | `listen-feedback` |
| Chinese brand | `听见` |

Issue #8 originally proposed renaming the module to `listen-feedback`; the repo
rename makes **`attune`** the canonical identity instead. A "meaningful release"
(#2) shouldn't bake in a name that disagrees with the repo, so the rename lands
first. This proposal supersedes #8's target.

## Goals

- One canonical name — **`attune`** — across module path, binary, brand prose,
  metrics, wire contracts, and the published image.
- Zero behavioural change: a pure rename, fully green on `build`/`vet`/`test`.
- Preserve the literal verb (`net/http`'s `ListenAndServe`, "listening", the
  "HTTP listen port" comment) — only the *brand* `listen` moves.

## Non-goals

- The `FEEDBACK_API_*` env-var prefix — it's "feedback-api", not the `listen`
  brand; renaming it is a separate, more-breaking decision (deferred).
- Scrubbing the pre-existing `§1` internal-info leaks the sweep surfaced
  (internal IP in `scripts/health-check.sh`, `casceneai` in
  `observability/README.md`, "Aliyun RDS" in migration comments) — tracked
  separately; not bundled into a rename.
- Rewriting immutable history: applied SQL migrations, the #14 ADR, and the
  `ci.yml` "gateway/ + listen/" history comment keep their original `listen`
  text.

## Proposal

A scoped, verb-safe find/replace, decided in layers (the scope boundary was
confirmed with the maintainer):

| Layer | `listen` → `attune` | Notes |
|---|---|---|
| Module path | `github.com/Phixsura/attune` | 59 files, 154 import lines |
| Binary + dir | `cmd/attune`, binary `attune` | Dockerfile + README + `.gitignore` |
| Metrics | `attune_*` prefix | + dashboard + Prometheus scrape job |
| Cookies | `attune_session` / `attune_oauth_state` | console-only, HttpOnly |
| **Wire contracts** | `X-Attune-Signature`, labels `attune/*`, UA `attune/<n>` | **breaking**; free pre-1.0 (0 releases) |
| Brand prose | `Attune` / `听见`→`Attune` | README, CLAUDE.md, templates, console UI |
| LICENSE | holder → **Lyphixia Wang** | (maintainer's call; dropped `万幕成川 / 听见 Listen`) |

Mechanics: `\bListen\b` / `\blisten\b` word-boundary substitution preserves the
verb forms automatically; `听见 [·/()] Listen` combos are collapsed *before* the
ASCII pass to avoid double-replacement; Go identifiers (`listenEnvelope` →
`attuneEnvelope`) and underscore identifiers (`listen_*`) get explicit passes
since `\b` doesn't cover them.

## Alternatives considered

- **Blanket `s/listen/attune/g`** — rejected: breaks `ListenAndServe`,
  `listening`, "HTTP listen port".
- **Keep wire contracts on `listen`** — rejected by the maintainer in favour of
  full consistency, since pre-1.0 / 0 releases makes the break free now.
- **Rename module to `listen-feedback`** (#8's original) — obsolete after the
  repo became `attune`.

## Risks / tradeoffs

- **Breaking** import path / binary / metric / header / label changes. Mitigated
  by landing pre-1.0 as one documented `### Changed`; no live consumers exist
  (0 tags, 0 releases).
- Migrations keep `listen` comments → cosmetic drift between DDL comments and the
  rest of the tree (acceptable; migrations are immutable artifacts).

## Implementation plan

1. Module path (`go mod edit` + import rewrite) → `go build/vet/tidy`.
2. Binary + `cmd/attune` (`git mv`) + Dockerfile + README + `.gitignore`.
3. Metrics + cookies + dashboard (`git mv` to `attune-overview.json`) + scrape job.
4. Brand pass: Chinese combos → `听见` standalone → ASCII (verb-safe) → restore
   the one verb casualty.
5. Fallout fixes the sweep caught: `.golangci.yml` depguard import path, a
   test-fixture bug (URL rewritten but expectation wasn't).
6. CHANGELOG `### Changed`; this proposal; repo description.

## Verification

Run 2026-06-05 — **✅ pure rename, fully green**:

| Check | Result |
|---|---|
| `go build ./...` / `go vet ./...` | ✅ |
| `go test ./...` | ✅ all packages |
| `gofmt -l .` | ✅ clean (6-char `listen`↔`attune` preserves alignment) |
| Residual `listen` (case-insensitive) | only preserved verbs + excluded historical |
| Wire-contract consistency (setter == test) | ✅ `X-Attune-Signature` ×7, `attune/*` labels, `attuneEnvelope` |
| Module path in test output | ✅ `github.com/Phixsura/attune/...` |

Scope: **100 files, +432/−420**.

## References

- #8 (module rename, retargeted to attune), #2 (release, unblocked), #5 (compose).
- Pre-existing `§1` leaks to scrub separately (see Non-goals).
