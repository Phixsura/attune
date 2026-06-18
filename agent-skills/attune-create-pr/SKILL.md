---
name: attune-create-pr
description: Open a pull request for attune that satisfies the repo's PR gates — Conventional-Commit title, a CHANGELOG [Unreleased] entry (unless purely docs/chore/ci/test/refactor), Closes #N, the linked proposal, and the Co-Authored-By trailer. Use when raising a PR or preparing a branch for review. Surfaced as /attune:create-pr.
---

# /attune:create-pr — open an attune pull request

Make the PR pass attune's process gates the first time. The "Semantic Pull
Request" workflow checks the title shape; the CI `changelog` job checks the
changelog entry. This skill assembles a compliant title + body.

## 1. PR title — Conventional Commit shape (CLAUDE.md §4)

Title is `type(scope): subject`, lowercase type and scope, imperative subject:

```
feat(notify): surface outbox dead-queue with manual retry
fix(notify): retry transient 5xx on raw webhook
docs(deploy): add private-deploy quickstart
chore(ci): bump lizard to 1.18
refactor(repo): extract Tenant.Resolve helper
test(handlers/console): add 401 path for missing api key
ci: switch govulncheck to 1.1.4
```

- **type** ∈ `feat | fix | docs | chore | refactor | test | ci` (Conventional
  Commits). `feat` → MINOR, `fix` → PATCH (§3 SemVer); a breaking field/config/
  schema/entrypoint change is MAJOR — say so explicitly in the changelog.
- **scope** is the package or area, lowercase, dash-allowed: `enricher`,
  `notify`, `infra/llmclient`, `console`, `deploy`, `repo`, `handlers/console`,
  `apikey`, … (use the real package path when in doubt). `ci` may drop the
  scope.

## 2. CHANGELOG — the rule is not optional (CLAUDE.md §2, §9)

- **Every PR with a code change adds an entry under `## [Unreleased]` in
  `CHANGELOG.md`**, in the same change. Format is Keep a Changelog 1.1.0.
  Sections: `### Added` / `### Changed` / `### Deprecated` / `### Removed` /
  `### Fixed` / `### Security`. Put breaking changes under `### Changed` with an
  explicit "breaking" call-out.
- **Skip the changelog only** for purely `docs/`, `chore/`, `ci/`, `test/`, or
  `refactor/` changes — and when you skip, **say so explicitly in the PR
  description** so the reviewer (and the `changelog` job) knows it was
  deliberate.

## 3. PR body

Include, in this order:

1. **What & why** — a short paragraph (and bullets for multi-part work).
2. **`Closes #N`** — links and auto-closes the issue.
3. **Proposal link** — point at the §10 doc
   (`docs/proposals/YYYY/MM/YYYY-MM-DD-<slug>.md`); mark its `Status` →
   `Implemented`.
4. **Verification** — cite the gate output you actually ran (see
   `/attune:preflight`): `make ci-check` result, `make proto` no-drift,
   `make test-integration` if a migration is involved, and a real-LLM run when
   the change touches enrichment. Don't claim "verified manually" without
   evidence (§9).
5. **Surfaced assumptions** — input shape, concurrency, failure modes (§6.4).
6. **Changelog note** — which `### Section` you added, or the explicit skip
   justification from §2.

End the PR body with:

```
🤖 Generated with [Claude Code](https://claude.com/claude-code)
```

## 4. Commit trailer convention

attune's git log uses a Co-Authored-By trailer on AI-assisted commits. Match
the existing form exactly (last blank line, then the trailer):

```
Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
```

## 5. Mechanics

- Branch off `main`; never commit straight to `main`. Commit/push only when the
  maintainer asks.
- Use `gh pr create` for the PR. Keep separate commits per workstream when a PR
  spans several, for reviewability.
- **Never bypass pre-commit or CI to make red go green** (§9) — fix the cause.

## Output

Print the proposed PR title and the assembled body for confirmation before
running `gh pr create`.
