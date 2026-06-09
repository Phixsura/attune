#!/usr/bin/env bash
# lint-errorcode — wrapper around cmd/lint-errorcode/.
#
# Bans hand-written ErrorResponse.Code string literals. ErrorResponse.code
# remains a string protobuf field for compatibility, but every writer should
# route through attunev1.ErrorCode to keep the contract proto-owned.
#
# Exit 0: clean. Exit 1: at least one finding. Exit 2: tool error.

set -euo pipefail

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  cat <<EOF
usage: scripts/lint-errorcode.sh [paths...]

  default paths: ./internal ./cmd

Rule:
  rule-errorresponse-code-literal
      attunev1.ErrorResponse{Code: "..."} is forbidden.

Per-line escape hatch:
  ... Code: "..." ... // errorcode:allow
EOF
  exit 0
fi

exec go run ./cmd/lint-errorcode "$@"
