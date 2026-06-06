#!/usr/bin/env bash
# attune — slog / OTel / http.Client static linter.
#
# A fast (≤ 1s) grep+awk pass that catches the three observability mistakes
# most likely to silently break tracing. Ported from the parent monorepo's
# bin/lint-slog.sh, trimmed to attune scope. Invoked by the pre-commit hook
# (.husky/pre-commit, issue #3) and — with the known warnings cleared
# (issue #9) — by CI in --strict mode (issue #1). See CLAUDE.md §1 / §7.
#
# ── Rules ───────────────────────────────────────────────────────────────────
#
#   rule-1  Non-context slog call.
#           `slog.Info(…)` / `slog.Warn(…)` / `slog.Error(…)` / `slog.Debug(…)`
#           lose trace_id/span_id correlation because they carry no ctx.
#           FIX: use the *Context variants — `slog.InfoContext(ctx, …)` — or the
#           `logext.Infof(ctx, …)` wrappers (ctx-first by signature).
#
#   rule-2  Business field collides with an OTel auto-injected key.
#           Keys like "trace_id" / "span_id" are injected by TraceIDHandler
#           (internal/observability/slog.go); "service.name" / "http.method" …
#           are injected by the OTel SDK. Re-setting them from business code
#           produces duplicate/ambiguous fields in the log backend.
#           FIX: don't set auto-injected keys by hand. For deliberate business
#           fields use the underscore constants in observability/attrs.go
#           (AttrHTTPMethod = "http_method", …), which are chosen to NOT collide.
#
#   rule-3  &http.Client{…} without an otelhttp Transport.
#           A client built without `Transport: otelhttp.NewTransport(…)` emits
#           no outbound spans, so APM topology loses the downstream node.
#           FIX: `&http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport), …}`.
#
# ── Exemptions ──────────────────────────────────────────────────────────────
#
#   Append a rule-scoped marker comment to the offending line to silence it:
#
#       slog.String("trace_id", id)   // lint-slog:allow rule-2
#
#   The marker is per-rule (`rule-1` / `rule-2` / `rule-3`) and per-line; it
#   suppresses only that rule on that line. Use sparingly and only where the
#   collision/omission is intentional (e.g. the canonical injector itself).
#
#   Facade internals — internal/observability/ and internal/logext/ — are
#   auto-exempt from the business-field rules (rule-1, rule-2): they define and
#   inject the reserved keys rather than misuse them. No per-line marker needed.
#
# ── Limitations (this is grep/awk, not a Go parser) ─────────────────────────
#
#   Pure `//` comment lines are skipped. NOT handled — use an exemption if hit:
#     • tokens inside string literals or /* block comments */
#     • slog via a *slog.Logger instance (`l.Info(…)`) or `slog.Default().Info(…)`
#     • reserved keys written map-style (`"trace_id": v`) or listed in a slice/case
#
# ── Usage / exit codes ──────────────────────────────────────────────────────
#
#   scripts/lint-slog.sh            warn-only: reports findings, ALWAYS exit 0
#                                   (the caller decides whether to block).
#   scripts/lint-slog.sh --strict   findings → exit 1 (CI / hard gate).
#
#   exit 0  clean, or warn-only with findings
#   exit 1  --strict and ≥1 finding
#   exit 2  usage error / not a git work tree
#
# History (Phase 1A warnings, all cleared in issue #9):
#   internal/infra/llmclient/openai_backend.go   rule-3 — fixed (#55: otelhttp Transport)
#   internal/observability/slog.go:32,33         rule-2 — auto-exempt (facade internal)

set -uo pipefail  # NOT -e: grep exits 1 on "no match", which is the happy path.

cd "$(dirname "$0")/.." || { echo "lint-slog: cannot cd to repo root" >&2; exit 2; }

# ── args ─────────────────────────────────────────────────────────────────────
strict=0
usage() {
  cat <<'EOF'
lint-slog — static slog / OTel / http.Client linter (rules 1-3; see file header).

usage: scripts/lint-slog.sh [--strict]

  (no args)   warn-only: report findings, always exit 0 (caller decides to block)
  --strict    findings → exit 1 (CI / hard gate)
  -h, --help  this help

exemption: append `// lint-slog:allow rule-N` to the offending line.
EOF
}
for arg in "$@"; do
  case "$arg" in
    --strict) strict=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "lint-slog: unknown arg: $arg (try --help)" >&2; exit 2 ;;
  esac
done

# ── colors (TTY only) ────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
  bold=$'\033[1m'; dim=$'\033[2m'; red=$'\033[31m'; green=$'\033[32m'; yellow=$'\033[33m'; reset=$'\033[0m'
else
  bold=''; dim=''; red=''; green=''; yellow=''; reset=''
fi

# ── files: tracked *.go (vendor-free; git ls-files skips untracked junk) ──────
# while-read (not mapfile) so this runs on macOS's stock bash 3.2 — the
# pre-commit hook (#3) executes locally. Go paths never contain newlines.
files=()
while IFS= read -r f; do files+=("$f"); done < <(git ls-files '*.go' 2>/dev/null)
if [[ "${#files[@]}" -eq 0 ]]; then
  echo "lint-slog: no tracked *.go files (not a git work tree?)" >&2
  exit 2
fi

# facade internals (the slog facade + the OTel handler that *injects* the
# reserved keys) are exempt from the business-field rules (rule-1, rule-2) —
# they own those keys, not misuse them. #48 reuses this when it tightens rule-1.
is_facade_internal() {
  case "$1" in
    internal/observability/*|internal/logext/*) return 0 ;;
    *) return 1 ;;
  esac
}

warn=0

# grep-based rule: $1=id $2=title $3=fix $4=extended-regex
check_grep() {
  local id="$1" title="$2" fix="$3" regex="$4" found=0 line file tmp ln code
  printf '\n%s▸ %s%s  %s\n' "$bold" "$id" "$reset" "$title"
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    [[ "$line" == *"lint-slog:allow ${id}"* ]] && continue   # per-line exemption
    file="${line%%:*}"; tmp="${line#*:}"; ln="${tmp%%:*}"; code="${tmp#*:}"
    is_facade_internal "$file" && continue                   # facade owns the reserved keys (#9; #48 reuses)
    [[ "$code" =~ ^[[:space:]]*// ]] && continue             # pure // comment line (URL-safe: anchored at line start)
    printf '    %s%s:%s%s\n' "$yellow" "$file" "$ln" "$reset"
    found=$((found + 1)); warn=$((warn + 1))
  done < <(grep -nHE "$regex" "${files[@]}" 2>/dev/null || true)
  if [[ "$found" -eq 0 ]]; then
    printf '    %s✓ none%s\n' "$green" "$reset"
  else
    printf '    %sfix:%s %s\n' "$dim" "$reset" "$fix"
  fi
}

check_grep "rule-1" \
  "non-context slog call (loses trace correlation)" \
  "use slog.InfoContext(ctx, …) or logext.Infof(ctx, …)" \
  'slog\.(Debug|Info|Warn|Error)\('

check_grep "rule-2" \
  "business field collides with an OTel auto-injected key" \
  "drop the manual key, or use the non-colliding observability/attrs.go constants" \
  '"(trace_id|span_id|service\.name|http\.method|http\.route|http\.status_code)"[[:space:]]*,'

# rule-3: brace-aware scan of each &http.Client{…} literal (handles multi-line).
printf '\n%s▸ rule-3%s  %s\n' "$bold" "$reset" "&http.Client without otelhttp.NewTransport (no outbound spans)"
r3_found=0
while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  printf '    %s%s%s\n' "$yellow" "$line" "$reset"
  r3_found=$((r3_found + 1)); warn=$((warn + 1))
done < <(awk '
  FNR == 1 { inblk = 0; depth = 0 }
  {
    has = ($0 ~ /Transport:/ || $0 ~ /otelhttp\.NewTransport/)
    exempt = ($0 ~ /lint-slog:allow rule-3/)
    if (inblk == 0) {
      if (match($0, /http\.Client\{/)) {
        if ($0 ~ /^[[:space:]]*\/\//) next            # pure // comment line, not a real literal
        seg = substr($0, RSTART)
        depth = gsub(/\{/, "{", seg) - gsub(/\}/, "}", seg)
        inblk = 1; startf = FILENAME; startl = FNR; sawT = has; sawX = exempt
        if (depth <= 0) { if (!sawT && !sawX) print startf ":" startl; inblk = 0 }
      }
    } else {
      depth += gsub(/\{/, "{", $0) - gsub(/\}/, "}", $0)
      if (has) sawT = 1
      if (exempt) sawX = 1
      if (depth <= 0) { if (!sawT && !sawX) print startf ":" startl; inblk = 0 }
    }
  }
' "${files[@]}" 2>/dev/null)
if [[ "$r3_found" -eq 0 ]]; then
  printf '    %s✓ none%s\n' "$green" "$reset"
else
  printf '    %sfix:%s wrap with Transport: otelhttp.NewTransport(http.DefaultTransport)\n' "$dim" "$reset"
fi

# ── verdict ──────────────────────────────────────────────────────────────────
printf '\n'
if [[ "$warn" -eq 0 ]]; then
  printf '%s✓ lint-slog: clean%s\n' "$green" "$reset"
  exit 0
fi
if [[ "$strict" -eq 1 ]]; then
  printf '%s✗ lint-slog: %d warning(s) — failing (--strict)%s\n' "$red" "$warn" "$reset"
  exit 1
fi
printf '%s⚠ lint-slog: %d warning(s) — warn-only (run with --strict to gate)%s\n' "$yellow" "$warn" "$reset"
exit 0
