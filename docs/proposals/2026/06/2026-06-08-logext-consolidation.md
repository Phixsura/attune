# Consolidate logging onto the `logext` facade (ban direct `slog.*` in business code)

| | |
|---|---|
| **Issue** | #48 |
| **Status** | Implemented |
| **Started** | 2026-06-08 |
| **Related** | #9 (built the facade exemption + `--strict` + trace-design doc — this consumes them), #4 (lint-slog origin), #3 (pre-commit), #1 (CI gate), #15 (CLAUDE.md §1 quality-gate table sync), #6/#57 (Prometheus/Grafana overlay — the actual dashboard backend) |

## Problem

The repo runs two logging idioms on one slog backbone:

| Idiom | Sites (verified on `main`, 2026-06-08) | Nature |
|---|---|---|
| `logext.*f` (printf wrapper) | **~273** (Errorf 109 / Infof 106 / Warnf 58 / Debugf 0) | human one-liners; fields flattened into the message |
| direct `slog.*Context` (structured kv) | **97** (Info 39 / Warn 30 / Error 23 / Debug 5) | structured fields |

`CLAUDE.md` §7 mandates `logext.*`, yet 97 business sites bypass it with direct
`slog.*Context`, and the tooling permits it: `scripts/lint-slog.sh` rule-1 only
catches the *non-context* variants (`slog\.(Debug|Info|Warn|Error)\(` —
[lint-slog.sh:146](../../../../scripts/lint-slog.sh)), of which there are now
**0**. So the doc says one thing, the code+linter allow another. **Decision
(issue #48): consolidate onto a single facade — `logext` — and make a direct
`slog.*` in business code a hard error.**

Trace correlation is **not** at stake. `trace_id`/`span_id` are injected by
`TraceIDHandler.Handle` ([slog.go:33-42](../../../../internal/infra/observability/slog.go))
from the ctx span; they survive any facade as long as `ctx` is passed (both
idioms already pass it).

### What the code actually shows (assumptions in the issue body, re-verified)

The issue (written 2026-06-05, pre-#19/#67 reorg) carries stale premises. Corrected:

1. **Paths moved.** Facade is `internal/pkg/logext/`, observability is
   `internal/infra/observability/`, entrypoint is `cmd/attune/main.go` — not the
   `internal/logext` / `internal/observability` / `cmd/listen` the issue cites.
2. **`attrs.go` is already dead.** The `observability.Attr*` constants
   ([attrs.go](../../../../internal/infra/observability/attrs.go)) have **0**
   references outside their own definitions — not in business code, the
   observability package, or tests. `TraceIDHandler` injects `trace_id`/`span_id`
   as **string literals** ([slog.go:37-38](../../../../internal/infra/observability/slog.go)),
   not via these constants.
3. **The "queryable field" motivation is hypothetical here.** The issue cites
   `rps`/`http_status`/`duration_ms` as SLS-`GROUP BY` material that flattening
   would lose. In fact `http_status_code`/`duration_ms`/`http_method`/`client_ip`/
   `http_route` each appear **0–1** times, `rps` appears **0** times, and there is
   **no access-log middleware** at all. The 97 sites are dominated by `"err", err`
   (37×) plus an *inconsistent* long tail (`tenant_id` 8× vs `tenant` 5× for the
   same concept). The one genuinely useful recurring structured field is
   **`inbound_trace_id`** (12×) — async-delivery → inbound-request correlation.
4. **Dashboards/alerts are metric-backed, not log-backed.** They read Prometheus
   metrics ([metrics.go](../../../../internal/infra/metrics/metrics.go):
   `attune_enrich_duration_seconds`, `attune_outbox_lag_seconds`,
   `attune_notify_failures_total`, …) via the #6/#57 Grafana overlay. No pipeline
   does `GROUP BY` on log fields. The trace-design doc already concedes `attrs.go`
   "may be vestigial for attune"
   ([observability-trace-design.md:92](../../../../docs/observability-trace-design.md)).

So the field-level queryability that flattening "loses" is, today, theoretical.

## Goals

- One logging entry point — `logext.*` — across business code (`internal/`, `cmd/`).
- A direct `log/slog` use in business code is a **hard CI error**.
- `trace_id`/`span_id` still emitted (handler-injected — regression-checked).
- `CLAUDE.md` §7/§1 and the lint tooling agree with the code.

## Non-goals

- **Preserving structured kv fields** — explicitly dropped (option A, below). If
  real SLS aggregation ever lands, the facade lets us add `logext.Infow` later and
  migrate *only* the sites that need it — a localized future change, not a
  re-architecture. We are not pre-building that.
- **Touching `TraceIDHandler` / the injection contract** — it is correct and stays
  verbatim.
- **Rewriting the `CLAUDE.md` §1 quality-gate *table*** beyond the logging row —
  owned by #15.

## Proposal

### Decision: option A — pure printf

Migrate the 97 `slog.*Context` sites to the existing `logext.*f` wrappers; **no
new logext API**. Remove `attrs.go`. Chosen over option B (add
`logext.Infow(ctx, msg, kv...)`) — see Alternatives for the full weighing; the
short version is that the structured fields B preserves are, per the evidence
above, not consumed, and A yields the smaller end-state surface the issue wants.

**Three levels only** (scope adjustment during implementation, at the user's
direction): the facade exposes **`Infof` / `Warnf` / `Errorf`** — Debug is
dropped from the API surface. The four pre-existing Debug call sites
(`outbox_worker` clear-failure error, `digest_weekly` two skip-paths,
`me.go` touch-last-seen) were reclassified: best-effort DB writes that
indicate trouble when they accumulate became **`Warnf`**, normal flow-explanation
"skipping" messages became **`Infof`**. Rationale: attune is a business
open-source project, ops needs to be able to explain "why didn't I get a
digest?" from the logs — a level filtered out by default isn't useful for that.
The "if a record is worth shipping it's an Info; otherwise it doesn't belong"
rule replaces the implicit "log it at Debug if unsure" pattern.

**Migration shape** (follow the established `logext` convention —
comma-joined `key:val`, optional `where` const, as in
[outbox_worker.go:115](../../../../internal/service/outbox/outbox_worker.go)):

```go
// before
slog.WarnContext(ctx, "outbox: mark delivered failed",
    "id", row.ID, "inbound_trace_id", row.TraceID, "err", err)
// after
logext.Warnf(ctx, "outbox: mark delivered failed,id:%d,inbound_trace_id:%s,err:%v",
    row.ID, row.TraceID, err)
```

`inbound_trace_id` survives as greppable message text. Error values follow the
codebase's existing `%v` / `%+v`+`err.Error()` convention.

### Enforcement: depguard, run locally **and** in CI; retire rule-1

Acceptance #1 is phrased about **imports** ("only `…/logext` + `…/observability`
import `log/slog`"), which is exactly what golangci-lint's **depguard** checks —
and the repo *already* uses depguard for §5 layering
([.golangci.yml:24-46](../../../../.golangci.yml): `infra-isolation`,
`notify-isolation`). Add a third rule:

```yaml
# CLAUDE.md §7: only the logging facade may import log/slog.
slog-facade:
  list-mode: lax
  files:
    - "**/*.go"
    - "!**/internal/pkg/logext/**"
    - "!**/internal/infra/observability/**"
  deny:
    - pkg: log/slog
      desc: "log via internal/pkg/logext, not log/slog directly (CLAUDE.md §7, #48)"
```

This is the idiomatic end-state the #9 proposal already flagged
([2026-06-06-lint-slog-strict.md:157-159](2026-06-06-lint-slog-strict.md)) and is
AST-accurate, unlike the grep rule.

**Wire golangci-lint into pre-commit too.** Today the hook runs only `go vet` +
`lint-slog.sh --strict` ([.husky/pre-commit:62-72](../../../../.husky/pre-commit));
CI is the "authoritative full gate." Adding `golangci-lint run` to the hook gives
depguard a local-bite (no CI-only gap) and lets us cleanly **retire grep rule-1**
— one rule, one place. Speed is preserved by relying on golangci's on-disk cache
(`~/.cache/golangci-lint`): a clean first run is one-off; cached re-runs on small
diffs are typically sub-second. If wall-clock matters more than absolute
correctness, the hook can pass `--new-from-rev=HEAD` (only newly introduced
findings) — kept as a fallback knob, not the default. This is a deliberate
narrowing of the #9 "fast pre-commit / authoritative CI" split for this rule;
accepted because the alternative (a second grep-shaped mirror of an AST-shaped
rule) costs more in long-run maintenance than the cached lint pass costs in
wall-clock.

`lint-slog.sh` becomes a 2-rule script (rule-2 reserved-key collisions, rule-3
http.Client transport check) — both are pattern checks golangci does not cover,
so the shell linter retains a reason to exist.

Note rule-1's *rationale* would have inverted (it was "non-context loses trace
correlation"; would have become "any direct `slog.*` bypasses the facade") —
retiring it sidesteps the doc-shuffle entirely.

### Relocate the bootstrap out of `cmd/`

[cmd/attune/main.go:47-53](../../../../cmd/attune/main.go) is the **one** business
site that legitimately needs `log/slog` (it builds the handler chain +
`slog.SetDefault`). depguard would (correctly) flag it. Move the block into the
facade it belongs to — `internal/infra/observability` already owns
`NewTraceIDHandler`:

```go
// observability.InstallDefaultLogger reads ENV, builds the JSON/text handler
// chain, wraps it in TraceIDHandler, and installs it as slog's default.
func InstallDefaultLogger() { /* the cmd/attune/main.go:47-53 block, verbatim */ }
```

`main` then calls `observability.InstallDefaultLogger()` and drops its `log/slog`
import. No behavior change — same handlers, same `ENV=dev` switch.

### Doc + tooling reconciliation

- **`CLAUDE.md` §7** ([:142](../../../../CLAUDE.md)) — already says "All logs use
  `logext.*`"; add the explicit ban ("never `log/slog` directly in business code")
  and point at depguard. **§1 logging row** ([:24](../../../../CLAUDE.md)) — change
  the "How" from "`lint-slog.sh` Rule 1" to "golangci-lint depguard
  (pre-commit + CI)."
- **`scripts/lint-slog.sh`** — remove rule-1 entirely (replaced by depguard); strip
  rule-2's `attrs.go` pointer in its fix text
  ([:24-25,151](../../../../scripts/lint-slog.sh)); keep rule-2 + rule-3 (neither
  is covered by golangci). Update file header to reflect the 2-rule scope and
  the depguard hand-off for rule-1's intent.
- **`docs/observability-trace-design.md`** — update the "Logging facade & field
  names" section + rule-1 row to match (attrs.go removed; facade now enforced).
- Historical proposals that mention `attrs.go` are point-in-time records — **left
  untouched**.

## Alternatives considered

- **B — add `logext.Infow(ctx, msg, kv ...any)`, keep `attrs.go`.** Preserves
  structured fields and is actually the *lower-risk migration* (a pure
  `slog.InfoContext`→`logext.Infow` rename, args unchanged, vs A's per-site
  semantic rewrite). **Rejected** because the fields it preserves are not consumed
  (Problem §3-4): `attrs.go` is dead, dashboards are metric-backed, the cited
  queryable fields barely exist. B keeps a structured idiom alive for a queryability
  story the code doesn't have, enlarging the facade surface the issue set out to
  shrink. *Honest caveat:* A doubles down on printf (against the modern slog grain)
  and is a coarser door — but the facade preserves the option to add `Infow` later
  if SLS aggregation becomes real, so the door isn't bolted.
  - Note the issue's "(or `...slog.Attr`)" parenthetical is a **trap**: an
    `...slog.Attr` signature forces callers to write `slog.String(...)` →
    `import "log/slog"` → violates acceptance #1. B would have to use `...any`.
- **E1 — depguard CI-only + reversed grep rule-1 as pre-commit mirror.**
  Two-tier enforcement (fast-local / accurate-CI), preserves the #9 hook design
  unchanged. Rejected after user input: maintains a grep approximation of an
  AST rule, and pre-commit can absorb golangci-lint with caching cheaply enough
  that the second tier is not worth the maintenance.
- **E2 — keep enforcement as grep rule-1 only (no depguard).** The issue's literal
  ask. Rejected: grep can't see `import`, alias renames, or `*slog.Logger`
  instances; depguard is AST-accurate and already in the repo. Extending the grep
  hack when the idiomatic tool is one `deny:` block away is the wrong direction.

## Risks / tradeoffs

- **Structured fields are permanently flattened.** Accepted: not consumed today
  (Problem §3-4); `inbound_trace_id` stays greppable as text; re-introduction later
  is a localized `logext.Infow` add, not a re-architecture.
- **A's migration is manual per-site** (semantic rewrite, not a rename) — higher
  hand-edit risk than B. Mitigation: migrate package-by-package, `go build`+`go test`
  after each; depguard + rule-1 mechanically catch any missed site; small PR-able
  batches.
- **rule-1 retired** — anyone with the old mental model ("`*Context` is the fix")
  must re-learn that *direct slog is now banned*. Mitigation: the §7 / trace-doc
  updates + the depguard error message ("log via `internal/pkg/logext`, not
  `log/slog` directly") land in the same PR; the depguard message is what the
  developer sees when they hit it, so it doubles as documentation.
- **depguard false-exempt risk** — the facade glob must exactly match the two
  packages; a typo would silently allow `slog` everywhere. Mitigation: a
  verification step adds a throwaway `slog.Info` in a business pkg and confirms
  both pre-commit and CI red.
- **pre-commit slower (first run only)** — golangci-lint's cold cache is multi-
  second; cached re-runs are typically sub-second on the diff. Acceptable trade
  for one-shape-of-enforcement. Documented escape hatch: a developer hitting an
  unusually slow run can pass `--new-from-rev=HEAD` to limit scope (kept as a
  knob, not the default — full lint matches CI).
- **`attrs.go` removal** touches a file four docs reference. Mitigation: the doc
  sweep above; functionally nothing imports it.

## Implementation plan

One `chore(observability)` branch (changelog-exempt per `CLAUDE.md` §2 — CI's
changelog job exempts `chore:` titles, [ci.yml:265](../../../../.github/workflows/ci.yml)):

1. **Facade bootstrap** — add `observability.InstallDefaultLogger()`; call it from
   `cmd/attune/main.go`; remove `log/slog` from `main.go`. (+ a test that it
   installs a default whose records carry `trace_id` under an active span.)
2. **Migrate the 97 sites**, package-by-package (handlers → service → notify →
   repo → cmd), `slog.*Context` → `logext.*f`, `build`+`test` per package.
3. **Remove `attrs.go`**; strip its pointer from `lint-slog.sh`'s rule-2 fix text.
4. **depguard** — add the `slog-facade` rule to `.golangci.yml`.
5. **Retire grep rule-1** from `lint-slog.sh` (delete the `check_grep "rule-1" …`
   block + its header section); script header updated to advertise the 2-rule
   scope and the depguard hand-off.
6. **Pre-commit gets `golangci-lint run`** — add a step to `.husky/pre-commit`
   after `lint-slog.sh`, accumulating `fail=1` on non-zero exit. Header notes
   list updated to include it.
7. **Docs** — `CLAUDE.md` §7/§1 logging row; `observability-trace-design.md`
   facade/field-name section + rule-1 row dropped from the lint table.
8. Run the full local gate (`go build/vet/test`, `lint-slog.sh --strict`,
   `golangci-lint run`); sync the final decision (A + depguard + pre-commit
   golangci) back to issue #48.

## Verification

- `rg 'log/slog' --type go -l` → only `internal/pkg/logext/` +
  `internal/infra/observability/`.
- Throwaway `slog.InfoContext(ctx,"x")` in a business pkg → pre-commit fails
  (golangci-lint depguard) and CI fails; remove it.
- Throwaway `slog.InfoContext` **inside** the facade pkgs → green (depguard
  exemption holds for `internal/pkg/logext` + `internal/infra/observability`).
- `lint-slog.sh --strict` runs and reports only rule-2 + rule-3 (no rule-1 line);
  exit 0 on clean main.
- Binary with `ENV=dev` and no OTLP endpoint → log lines still carry
  `trace_id`/`span_id` (no output regression; acceptance #3).
- `go build ./... && go test ./...` green; `lizard` CCN/NLOC unchanged;
  `jscpd` < 4%.
- `CLAUDE.md` grep: no remaining "Rule 1" wording that implies `*Context` is the
  fix; §7 and §1 agree.

## References

- Issue #48; #9 ([proposal](2026-06-06-lint-slog-strict.md)) built the facade
  exemption + trace-design doc this consumes; #4/#3/#1 (lint-slog / hook / CI).
- Code: [logext.go](../../../../internal/pkg/logext/logext.go),
  [observability/{slog,attrs}.go](../../../../internal/infra/observability/),
  [lint-slog.sh](../../../../scripts/lint-slog.sh),
  [.golangci.yml](../../../../.golangci.yml),
  [metrics.go](../../../../internal/infra/metrics/metrics.go).
- depguard (allow-list a logging facade, deny `log/slog` elsewhere):
  <https://github.com/OpenPeeDeeP/depguard>.
