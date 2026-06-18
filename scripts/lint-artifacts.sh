#!/usr/bin/env bash
# attune — shipped-artifact hygiene linter.
#
# A fast (≤ 1s) grep/perl pass that keeps intermediate-state scaffolding out
# of shipped code, comments, and API docs. attune is an English, Apache-2.0
# OSS project built collaboratively in Chinese and in phases, so AI-assisted
# changes have historically left roadmap markers and stray CJK behind. CI
# rejects what an author might forget. Invoked by the pre-commit hook
# (.husky/pre-commit) and by CI in --strict mode (issue #64). See CLAUDE.md §1.
#
# Sibling of scripts/lint-slog.sh — same house contract: warn-only by default,
# --strict to gate, per-line + whole-file allow directives, git ls-files for the
# file set, bash-3.2-safe while-read loops, TTY colors, usage/--help.
#
# ── Checks ────────────────────────────────────────────────────────────────────
#
#   Check A — roadmap / phase markers  (GATES under --strict)
#           ERE `\bPhase[[:space:]]*[0-9]`. `Phase N` is the reliable anchor for
#           every real intermediate-state marker (#64 cites `Phase 3.3`,
#           `Phase 5 M6`, `Phase 1.5 Layer 2`, …). Bare `Layer N` / `M6` are too
#           false-positive-prone to gate (documented limitation).
#           FIX: drop the marker — shipped artifacts describe what *is*, not the
#           rollout phase it arrived in.
#
#   Check B — non-i18n CJK (Han)  (GATES under --strict)
#           `\p{Han}` via `perl -CSD` (portable; BSD grep lacks -P, so a
#           `grep -P` would break the macOS pre-commit run). English is canonical
#           in shipped code/comments/API docs.
#           FIX: write the comment/string in English, or — for genuine localized
#           product data / language-aware prompts — add a file-allow directive.
#
#   Check C — transitional language  (ADVISORY — printed, never gates)
#           `coming in a follow-up|lands with #N|for now|deferred`. Prose is too
#           false-positive-prone to fail a build on, so this is informational
#           only and never affects the exit code (D3).
#           FIX (when real): delete the placeholder; ship the thing or open an
#           issue and reference it without the weasel words.
#
# ── Scanned file set ──────────────────────────────────────────────────────────
#
#   Check A: tracked *.go + docs/openapi/openapi.yaml + shipped markdown.
#   Check B: tracked *.go EXCEPT *_test.go + docs/openapi/openapi.yaml + shipped md.
#   Check C: shipped markdown + tracked *.go.
#
#   "Shipped markdown" = README.md plus docs/**/*.md, EXCEPT docs/proposals/**
#   and docs/superpowers/** — those design docs legitimately quote `Phase N` and
#   CJK examples (this guard's own proposal does).
#
# ── Allow directives ──────────────────────────────────────────────────────────
#
#   Per-line:   append `// lint-artifacts:allow <reason>` to skip that one line.
#   Whole-file: put `// lint-artifacts:file-allow <reason>` anywhere in the file
#               to skip the entire file. Mirrors cmd/lint-rawptr's
#               `// ptrext:file-allow`. Markdown/YAML can use the same token in
#               an HTML comment or a `#` comment — the match is substring-based.
#
#   File-allowed today (legitimate localized content, D1):
#     internal/service/enrich/enricher_prompt.go  — language-aware zh prompt
#     internal/domain/semantic_pack.go             — localized semantic-pack data
#     internal/service/workflow/workflow.go        — localized seed-state names
#
# ── Limitations (this is grep/perl, not a parser) ──────────────────────────────
#
#   Check A scans comment lines too (markers usually live in comments), so a
#   `// Phase 2 …` line IS flagged unless allow-marked. Check C's prose patterns
#   will catch incidental "for now" in legitimate sentences — that's why C is
#   advisory only.
#
# ── Usage / exit codes ─────────────────────────────────────────────────────────
#
#   scripts/lint-artifacts.sh            warn-only: reports findings, ALWAYS exit 0
#                                        (the caller decides whether to block).
#   scripts/lint-artifacts.sh --strict   Check A or B finding → exit 1 (CI / gate).
#                                        Check C never gates.
#
#   exit 0  clean, or warn-only with findings, or only Check C findings
#   exit 1  --strict and ≥1 Check A/B finding remains
#   exit 2  usage error / not a git work tree

set -uo pipefail  # NOT -e: grep exits 1 on "no match", which is the happy path.

cd "$(dirname "$0")/.." || { echo "lint-artifacts: cannot cd to repo root" >&2; exit 2; }

# ── args ─────────────────────────────────────────────────────────────────────
strict=0
usage() {
  cat <<'EOF'
lint-artifacts — shipped-artifact hygiene linter (see file header).

usage: scripts/lint-artifacts.sh [--strict]

  (no args)   warn-only: report findings, always exit 0 (caller decides to block)
  --strict    Check A (roadmap markers) or B (non-i18n CJK) finding → exit 1
              Check C (transitional language) is advisory and never gates.
  -h, --help  this help

allow: per-line `// lint-artifacts:allow <reason>` or whole-file
       `// lint-artifacts:file-allow <reason>` (substring-matched).
EOF
}
for arg in "$@"; do
  case "$arg" in
    --strict) strict=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "lint-artifacts: unknown arg: $arg (try --help)" >&2; exit 2 ;;
  esac
done

# ── colors (TTY only) ────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
  bold=$'\033[1m'; dim=$'\033[2m'; red=$'\033[31m'; green=$'\033[32m'; yellow=$'\033[33m'; reset=$'\033[0m'
else
  bold=''; dim=''; red=''; green=''; yellow=''; reset=''
fi

# ── file sets (git ls-files; vendor-free, skips untracked junk) ───────────────
# while-read (not mapfile) so this runs on macOS's stock bash 3.2 — the
# pre-commit hook executes locally. Tracked paths never contain newlines.
#
# is_shipped_md: README.md or docs/**/*.md, but NOT docs/proposals/** or
# docs/superpowers/** (those design docs legitimately quote Phase/CJK examples).
is_shipped_md() {
  case "$1" in
    docs/proposals/*|docs/superpowers/*) return 1 ;;
    README.md|docs/*.md) return 0 ;;
    *) return 1 ;;
  esac
}

openapi="docs/openapi/openapi.yaml"

go_files=()
while IFS= read -r f; do go_files+=("$f"); done < <(git ls-files '*.go' 2>/dev/null)
if [[ "${#go_files[@]}" -eq 0 ]]; then
  echo "lint-artifacts: no tracked *.go files (not a git work tree?)" >&2
  exit 2
fi

# *.go minus *_test.go (Check B exempts test fixtures — non-English test data is fine).
go_nontest=()
for f in "${go_files[@]}"; do
  case "$f" in *_test.go) ;; *) go_nontest+=("$f") ;; esac
done

# shipped markdown set.
md_files=()
while IFS= read -r f; do
  is_shipped_md "$f" && md_files+=("$f")
done < <(git ls-files 'README.md' 'docs/*.md' 'docs/**/*.md' 2>/dev/null)

# ── per-file allow cache ──────────────────────────────────────────────────────
# A file carrying `// lint-artifacts:file-allow` (or the token in any comment
# form) is skipped wholesale. We collect the matching paths into a single
# space-delimited string (bash 3.2 has no portable `declare -gA`) and grep each
# candidate file at most once.
file_allow_hits=" "
scan_file_allows() {
  local f
  for f in "$@"; do
    case "$file_allow_hits" in *" $f "*) continue ;; esac
    if grep -q 'lint-artifacts:file-allow' "$f" 2>/dev/null; then
      file_allow_hits="${file_allow_hits}${f} "
    fi
  done
}
scan_file_allows "${go_files[@]}" "${md_files[@]}"
[[ -f "$openapi" ]] && scan_file_allows "$openapi"

is_file_allowed() {
  case "$file_allow_hits" in *" $1 "*) return 0 ;; *) return 1 ;; esac
}

warnAB=0   # Check A + B findings — these gate under --strict
warnC=0    # Check C findings — advisory only

# ── Check A / C engine — line-oriented grep over an ERE ───────────────────────
# $1=id  $2=title  $3=fix  $4=extended-regex  $5=gate(1|0)  $6.. = files
run_line_check() {
  local id="$1" title="$2" fix="$3" regex="$4" gate="$5"; shift 5
  local found=0 line file tmp ln code
  printf '\n%s▸ %s%s  %s\n' "$bold" "$id" "$reset" "$title"
  if [[ "$#" -eq 0 ]]; then
    printf '    %s✓ none%s\n' "$green" "$reset"; return
  fi
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    [[ "$line" == *"lint-artifacts:allow"* ]] && continue   # per-line exemption
    file="${line%%:*}"; tmp="${line#*:}"; ln="${tmp%%:*}"; code="${tmp#*:}"
    is_file_allowed "$file" && continue                     # whole-file exemption
    printf '    %s%s:%s%s  %s\n' "$yellow" "$file" "$ln" "$reset" "${code#"${code%%[![:space:]]*}"}"
    found=$((found + 1))
    if [[ "$gate" -eq 1 ]]; then warnAB=$((warnAB + 1)); else warnC=$((warnC + 1)); fi
  done < <(grep -nHE "$regex" "$@" 2>/dev/null || true)
  if [[ "$found" -eq 0 ]]; then
    printf '    %s✓ none%s\n' "$green" "$reset"
  else
    printf '    %sfix:%s %s\n' "$dim" "$reset" "$fix"
  fi
}

# ── Check B engine — Han via perl -CSD (portable; BSD grep lacks -P) ──────────
# Per-line allow + whole-file allow honored the same way. perl prints
# "<file>:<lineno>:<line>" for every line containing a Han codepoint.
run_han_check() {
  local found=0 line file tmp ln code
  printf '\n%s▸ Check B%s  %s\n' "$bold" "$reset" "non-i18n CJK (Han) in shipped code / API docs"
  if [[ "$#" -eq 0 ]]; then
    printf '    %s✓ none%s\n' "$green" "$reset"; return
  fi
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    [[ "$line" == *"lint-artifacts:allow"* ]] && continue
    file="${line%%:*}"; tmp="${line#*:}"; ln="${tmp%%:*}"; code="${tmp#*:}"
    is_file_allowed "$file" && continue
    printf '    %s%s:%s%s  %s\n' "$yellow" "$file" "$ln" "$reset" "${code#"${code%%[![:space:]]*}"}"
    found=$((found + 1)); warnAB=$((warnAB + 1))
  done < <(perl -CSD -ne 'print "$ARGV:$.:$_" if /\p{Han}/; close ARGV if eof' "$@" 2>/dev/null || true)
  if [[ "$found" -eq 0 ]]; then
    printf '    %s✓ none%s\n' "$green" "$reset"
  else
    printf '    %sfix:%s write in English, or file-allow genuine localized data\n' "$dim" "$reset"
  fi
}

# ── Check A — roadmap / phase markers (GATES) ─────────────────────────────────
a_files=("${go_files[@]}")
[[ -f "$openapi" ]] && a_files+=("$openapi")
a_files+=("${md_files[@]}")
run_line_check "Check A" \
  "roadmap / phase markers (Phase N)" \
  "drop the marker — shipped artifacts describe what is, not the rollout phase" \
  '\bPhase[[:space:]]*[0-9]' \
  1 \
  "${a_files[@]}"

# ── Check B — non-i18n CJK (GATES) ────────────────────────────────────────────
b_files=("${go_nontest[@]}")
[[ -f "$openapi" ]] && b_files+=("$openapi")
b_files+=("${md_files[@]}")
run_han_check "${b_files[@]}"

# ── Check C — transitional language (ADVISORY) ────────────────────────────────
c_files=("${md_files[@]}")
c_files+=("${go_files[@]}")
run_line_check "Check C" \
  "transitional language (advisory — never gates)" \
  "delete the placeholder once shipped; reference an issue without weasel words" \
  'coming in a follow-up|lands with #[0-9]|\bfor now\b|\bdeferred\b' \
  0 \
  "${c_files[@]}"

# ── verdict ──────────────────────────────────────────────────────────────────
printf '\n'
if [[ "$warnAB" -eq 0 && "$warnC" -eq 0 ]]; then
  printf '%s✓ lint-artifacts: clean%s\n' "$green" "$reset"
  exit 0
fi
if [[ "$warnAB" -eq 0 ]]; then
  # only advisory Check C findings — never gates.
  printf '%s⚠ lint-artifacts: %d advisory (Check C) finding(s) — informational, not gating%s\n' "$yellow" "$warnC" "$reset"
  exit 0
fi
if [[ "$strict" -eq 1 ]]; then
  printf '%s✗ lint-artifacts: %d gating (Check A/B) finding(s) — failing (--strict)%s\n' "$red" "$warnAB" "$reset"
  [[ "$warnC" -gt 0 ]] && printf '%s  (+ %d advisory Check C finding(s), not counted)%s\n' "$dim" "$warnC" "$reset"
  exit 1
fi
printf '%s⚠ lint-artifacts: %d gating finding(s) — warn-only (run with --strict to gate)%s\n' "$yellow" "$warnAB" "$reset"
[[ "$warnC" -gt 0 ]] && printf '%s  (+ %d advisory Check C finding(s))%s\n' "$dim" "$warnC" "$reset"
exit 0
