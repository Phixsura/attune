---
name: attune-preflight
description: Run attune's local CI pre-flight (make ci-check) before pushing or claiming work is done, and read the failures. Mirrors the full CI gate — go vet/build/test-race, golangci-lint, lizard, the lint-* shell checks, jscpd, and console tsc/biome/vitest. Never claim green without citing the output. Surfaced as /attune:preflight.
---

# /attune:preflight — run attune's CI gate locally

`make ci-check` mirrors the full CI gate on your machine (CLAUDE.md §1). Run it
before pushing and before telling anyone a change is done.

## Run it

```
make ci-check
```

Needs `go`, `golangci-lint`, `lizard`, `pnpm`, and optionally `trufflehog`. It
runs, in order, and stops at the first failure:

| Step | Command | Gate |
|---|---|---|
| go vet | `go vet ./...` | 0 warnings |
| go build | `go build ./...` | 0 errors |
| go test (unit) | `go test -race -short ./...` | all pass |
| golangci-lint | `golangci-lint run` | 0 findings (incl. depguard `slog-facade`) |
| lizard | `lizard . -l go -C 15 -T nloc=100 --warnings_only` | CCN ≤ 15, NLOC ≤ 100 |
| lint-slog | `bash scripts/lint-slog.sh --strict` | 0 findings (log fields / outbound HTTP, rules 2+3) |
| lint-rawptr | `bash scripts/lint-rawptr.sh` | no bare `*p` / `&x` (use `internal/pkg/ptrext`) |
| lint-errorcode | `bash scripts/lint-errorcode.sh` | `ErrorResponse.code` from the enum |
| lint-integration-layout | `bash scripts/lint-integration-layout.sh` | no misplaced integration-tagged tests |
| jscpd | `npx -y jscpd . -f go -i '**/*.pb.go' -t 5 --silent` | duplication < 5% |
| console tsc | `cd console && pnpm tsc -b --noEmit` | 0 type errors |
| console biome | `cd console && pnpm biome check` | 0 errors |
| console vitest | `cd console && pnpm vitest run` | all pass + per-file coverage thresholds |
| trufflehog | `trufflehog git file://. --only-verified --fail` | no verified secrets (skipped if not installed) |

## Not covered by `ci-check` — run when relevant

- **Proto contract.** If you touched `proto/**`, run `make proto` and confirm no
  diff (`git diff --exit-code`); CI's `proto-sync` fails on drift. Never
  hand-edit generated files (`internal/proto/**`, `console/src/proto/**`,
  `docs/openapi/**`) — CLAUDE.md §11.
- **Postgres integration.** If you added/changed a migration or a DB repo, run
  `make test-integration` (needs Docker or `ATTUNE_TEST_DATABASE_URL`).
- **Real-LLM end-to-end.** For enrichment-touching changes, acceptance includes
  a real-LLM run (`make test-live`, env keys per `make test-live-list`), not
  just unit tests.
- **go.mod tidy.** `go mod tidy && git diff --exit-code go.mod go.sum`.

## Reading failures

- The script prints `▸ <step>` before each check and `✓ <step>` after it
  passes. The **last `▸` with no matching `✓`** is the failing step — fix that
  one first, then re-run.
- `lint-rawptr` / `lint-slog` / `lint-artifacts` failures usually mean an
  allow-directive is needed or, more often, the code should be rewritten through
  the facade (`logext`, `ptrext`) — prefer the rewrite (CLAUDE.md §7, §7b).
- `lizard` failures (CCN/NLOC over budget) mean splitting the function, not
  raising the threshold.
- `vitest` per-file coverage failures: add the missing tests; thresholds live in
  `console/vite.config.ts`.

## The rule

**Never claim green without citing the output.** "Verified manually" with no
evidence does not count (CLAUDE.md §9). Quote the final
`✓ ci-check passed — ready to push` line (or the specific failure) in your
summary / PR description. Never bypass a hook or gate to make red go green — fix
the underlying cause.
