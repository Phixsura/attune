#!/usr/bin/env bash
# lint-rawptr — wrapper around cmd/lint-rawptr/.
#
# Bans raw *<expr> deref and &<expr> address-of in attune's own source.
# Authors must funnel pointer creation/reading through internal/pkg/ptrext.
#
# See cmd/lint-rawptr/main.go for the full ruleset + allowlist.
#
# Exit 0: clean. Exit 1: at least one finding. Exit 2: tool error.

set -euo pipefail

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  cat <<EOF
usage: scripts/lint-rawptr.sh [-v] [paths...]

  default paths: ./internal ./cmd
  -v             also print allowed sites (debug)

Rules:
  rule-deref     *p as rvalue              → ptrext.Indirect / IndirectOr
  rule-addr      &expr address-of          → ptrext.Of

Per-line escape hatch:
  ... &someExpr ... // ptrext:allow      (silences the line)
EOF
  exit 0
fi

exec go run ./cmd/lint-rawptr "$@"
