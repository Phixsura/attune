# Clear lint-slog warnings, flip `--strict`, and document the trace design

| | |
|---|---|
| **Issue** | #9 |
| **Status** | Accepted |
| **Started** | 2026-06-06 |
| **Related** | #55 (otelhttp transport — done), #61 (folded into #9, closed as dup), #48 (logext consolidation — reuses the facade exemption this adds), #15 (CI quality-gate doc sync), #4 (lint-slog origin), #3 (pre-commit), #1 (CI) |

## Problem

`scripts/lint-slog.sh` reports three known warnings (rule-3 ×1, rule-2 ×2) and is
wired **warn-only** everywhere — `.husky/pre-commit:62` runs it as
`bash scripts/lint-slog.sh || true`, and CI does not run it at all
(`.github/workflows/ci.yml:19-20` defers `--strict` gating to #9). So the rules
exist but cannot stop a regression.

Issue #9's body, written in Phase 1A, has since gone partly stale and partly
wrong. Verified against `main` (`16c6ea9`):

1. **Item ① (rule-3, otelhttp transport) is already done.** #55 → #58
   (`5e8d6a6`) added `Transport: otelhttp.NewTransport(http.DefaultTransport)` to
   the OpenAI backend (`internal/infra/llmclient/openai_backend.go:54`), with a
   test and a proposal. `#9`'s item ① duplicates the closed #55.

2. **Item ②③ (rule-2, `trace_id`/`span_id`) is not a code defect.** #9's premise
   — "the OTel SDK auto-injects same-named fields, producing two `trace_id` per
   line" — does **not** hold in this repo:
   - The log pipeline is `slog.New(TraceIDHandler(JSONHandler→os.Stdout))`
     (`cmd/attune/main.go:50`). There is **no** otelslog bridge / OTLP log
     exporter (`go.mod` has none).
   - `internal/observability/attrs.go:22-26` documents `trace_id`/`span_id` as
     the **canonical handler-injected keys** — `TraceIDHandler`
     (`internal/observability/slog.go:32-33`) is their *only* and *intended*
     source, shared across the BE/Gateway/Attune fleet.
   - `internal/observability/otel.go` keeps trace_id in logs even with no
     exporter ("noop tracer ... 仍产 trace_id(slog 注入)"), and
     `internal/observability/idgen.go`'s `ReadableIDGenerator` gives it a custom
     timestamp-prefixed format that the design *wants* surfaced.

   So `rule-2` firing on `slog.go:32-33` is a **linter false positive on its own
   source of truth**. The rule's stated intent (`lint-slog.sh:18`) is
   "***Business* field collides with an auto-injected key**" — but the
   observability handler is not business code; it *is* the injector.

3. **The `--strict` flip was never done.** #61 ("fix rule-2 + rule-3, then flip
   `--strict`") was closed `NOT_PLANNED` as *"Duplicate of #9 … clear the 3
   lint-slog warnings … then enable `--strict`"*. The maintainer folded that
   charter — including the **`--strict` flip that #9's body omits** — into #9.

4. **The canonical design doc is missing.** `docs/observability-trace-design.md`
   is referenced as the authoritative source by four code comments
   (`cmd/attune/main.go:43`, `internal/observability/otel.go:3`,
   `cmd/attune/server.go:48,155`) but has never existed (no git history). Its
   absence is arguably *why* #9 could be filed on a wrong premise.

## Goals

- `bash scripts/lint-slog.sh --strict` exits 0 (zero findings), and that state is
  **gated** in both pre-commit and CI so it cannot regress.
- Resolve `rule-2`'s false positive **without** changing any log output —
  `trace_id`/`span_id` keep their names and values.
- Do so via an abstraction #48 can reuse for `rule-1` (don't solve it twice).
- Land `docs/observability-trace-design.md` so the four code references resolve
  and the trace-field decision is documented once, authoritatively.
- Re-classify #9 to match the actual residual work.

## Non-goals (owned by other issues — do not touch here)

- **logext consolidation / banning direct `slog.*Context` in business code /
  migrating the ~107 structured sites / redefining `rule-1` / `attrs.go`'s fate /
  `CLAUDE.md` §7** → **#48**. This proposal only *prepares* the shared facade
  exemption #48 will consume; it does not tighten `rule-1`.
- **Reconciling the `CLAUDE.md` §1 quality-gate table with CI** (jscpd, lint-slog
  listing, etc.) → **#15**. We leave a note there that lint-slog is now a real
  gate, but do not rewrite §1.

## Proposal

Four changes, all on one `chore(observability)` branch.

### 1. `rule-2` → a facade-internal exemption in `lint-slog.sh` (Option D)

Add a single, documented path predicate to `lint-slog.sh`: files under the
**logging facade internals** — `internal/observability/` and `internal/logext/`
— are exempt from the *business-field* grep rules (`rule-1`, `rule-2`), because
those packages *define and inject* the reserved fields rather than misuse them.

```sh
# facade internals own the reserved keys; the business-field rules don't apply
is_facade_internal() {
  case "$1" in
    internal/observability/*|internal/logext/*) return 0 ;;
    *) return 1 ;;
  esac
}
```

…consulted inside `check_grep`'s match loop (skip a hit when
`is_facade_internal "$file"`). `slog.go` is **not edited** — its injection is
correct and stays verbatim.

This is the same exemption #48 needs ("keep `internal/observability` +
`internal/logext` … exempt") — built once here, consumed by #48 when it tightens
`rule-1`. It mirrors the industry-standard depguard pattern (allow-list the
facade package, restrict `log/slog` elsewhere) that attune already runs for §5
layering — a lightweight shell stand-in for the same idea.

### 2. Flip lint-slog to `--strict` (pre-commit + CI)

The script already implements `--strict` (findings → exit 1); only the callers
change.

- **`.husky/pre-commit`** — `bash scripts/lint-slog.sh || true` →
  `bash scripts/lint-slog.sh --strict || fail=1`; relabel the step from
  "(warn-only)" and update the header note ③. (The hook already accumulates
  `fail` and exits 1 at the end.)
- **`.github/workflows/ci.yml`** — drop the "deliberately omitted: lint-slog"
  note; add a small `lint-slog` job (checkout + `bash scripts/lint-slog.sh
  --strict`, gated on `needs.changes.outputs.go == 'true'`, no Go toolchain
  needed) and add it to the `ci-gate` `needs:` aggregator so it is genuinely
  required.

### 3. Write `docs/observability-trace-design.md`

Author the missing doc the four comments point to. Outline: the handler-injection
model (`TraceIDHandler` is the sole, intentional `trace_id`/`span_id` source —
*not* an OTel log bridge); the `ReadableIDGenerator` timestamp-prefixed format
and its operator rationale; why logs carry trace_id even under the noop tracer;
W3C propagation + FE-as-origin; and an explicit "reserved field-name contract"
section that the `rule-2` facade exemption now points to as its rationale.

### 4. Re-label #9 and fix its acceptance

`bug`, `type/fix` → `type/chore`; add `area/observability`, `area/ci`. Drop the
"CHANGELOG gets a `### Fixed` line" criterion: this is `chore`/`ci`/`docs` work,
which CI's changelog job (`ci.yml:206-210`) and `CLAUDE.md` §2 exempt — the
`chore(observability): …` PR title carries no changelog entry.

## Alternatives considered

- **A — rename to `inbound_trace_id`/`inbound_span_id`.** Rejected: `trace_id`/
  `span_id` are *cross-service* shared constants (`attrs.go`); renaming drifts the
  schema across the whole fleet + every dashboard, not just one repo. The name is
  also inaccurate — the handler captures the *active* span, and trace_id
  originates at the FE.
- **B — per-line `// lint-slog:allow rule-2` markers on `slog.go:32-33`.** This is
  what the script header explicitly blesses for "the canonical injector," and is
  the lowest-risk option. Rejected as *primary* only because it doesn't build the
  exemption #48 needs and must be re-applied if `slog.go` is refactored. Kept as
  the **fallback** if we decide not to touch shared tooling.
- **C — delete the two lines, "trust OTel auto-inject."** Rejected: there is no
  OTel log auto-inject in this pipeline — deleting strips `trace_id`/`span_id`
  from logs entirely, breaking the `attrs.go` contract, local-dev trace_id, and
  log↔trace correlation. #9 "leaned toward C" on a premise this repo does not
  satisfy; it would *fail* #9's own "see the full trace chain" acceptance.
- **Move rules into golangci-lint depguard/custom now.** The idiomatic end-state
  (and where #48 may go). Out of scope for #9 — a bigger migration; the shell
  linter is the current contract.

## Risks / tradeoffs

- **Facade exemption applies to `rule-1` too** (both use `check_grep`). Verified
  no behavior change today: business code has zero bare `slog.*(` calls, and the
  facade packages have none that `rule-1` currently catches — so the exemption is
  a no-op for `rule-1` now and only takes effect when #48 tightens it. (Re-checked
  at implementation for `internal/logext`.)
- **Pre-commit now blocks on whole-repo lint state**, not just staged files
  (the script scans `git ls-files`). Acceptable: post-change there are zero
  findings, and the hook is meant to mirror CI (equal, not stricter). Bypass
  remains `git -c core.hooksPath=/dev/null commit`.
- **Reduced rule coverage inside the facade packages.** Intentional and correct —
  those packages are the field owners; that is the whole point of the exemption.

## Implementation plan

1. `lint-slog.sh`: add `is_facade_internal()` + the skip in `check_grep`; update
   the header "Known warnings" block (now cleared). No change to `slog.go`.
2. `.husky/pre-commit`: `--strict || fail=1`; fix step label + header note ③.
3. `.github/workflows/ci.yml`: remove the omission note; add the `lint-slog` job;
   extend `ci-gate.needs`.
4. `docs/observability-trace-design.md`: author per the outline above.
5. Relabel #9; sync its body (① done by #55/#58; ②③ → facade exemption; add the
   `--strict` flip; note #61 folded-in; corrected acceptance).

## Verification

- `bash scripts/lint-slog.sh` → all three rules `✓ none`; `--strict` exits 0.
- Add a throwaway bare `slog.Info("x")` in a business package → `--strict` exits 1
  (rule still bites); remove it.
- Add a throwaway `slog.String("trace_id", …)` **inside** `internal/observability`
  → still exempt (facade); in a business package → flagged. Remove both.
- Run the binary with `ENV=dev` and no OTLP endpoint → log lines still carry
  `trace_id`/`span_id` (no output regression).
- `go build ./... && go test ./...` green; the four code comments now resolve to a
  real `docs/observability-trace-design.md`.
- CI: the new `lint-slog` job runs and is wired into `ci-gate`.

## References

- Issue #9; closed #55, #61; open #48, #15; #4/#3/#1 (lint-slog / hook / CI).
- `scripts/lint-slog.sh` (rules, `--strict`, exemption mechanism).
- `internal/observability/{slog,attrs,otel,idgen}.go`.
- golangci-lint `depguard` (allow-list a logging facade, restrict `log/slog`):
  <https://golangci-lint.run/docs/linters/configuration/>,
  <https://github.com/OpenPeeDeeP/depguard>.
