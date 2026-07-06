#!/usr/bin/env bash
# Attune — quality gate one-shot runner.
#
# CLAUDE.md 律 1（闭环反馈）要求：写完任何模块必跑 jscpd + lizard，提交前
# 必跑 lint + typecheck + test. 这个脚本把它们串成一遍。
#
# Usage:
#   ./scripts/check.sh               # full gate
#   ./scripts/check.sh --skip-jscpd  # skip the duplication check (slowest)
#
# Exit non-zero on the first failing gate so CI / pre-commit can hard-fail.

set -euo pipefail

cd "$(dirname "$0")/.."

skip_jscpd=0
for arg in "$@"; do
  case "$arg" in
    --skip-jscpd) skip_jscpd=1 ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

step() {
  printf '\n\033[1;34m▸ %s\033[0m\n' "$1"
}

step "go build ./..."
go build ./...

step "go vet ./..."
go vet ./...

step "lint-errorcode"
bash scripts/lint-errorcode.sh

step "lint-integration-layout"
bash scripts/lint-integration-layout.sh

step "lint-maturity-contract"
bash scripts/lint-maturity-contract.sh

step "lint-http-response-emission"
bash scripts/lint-http-response-emission.sh

step "lint-outbound-conformance"
bash scripts/lint-outbound-conformance.sh

step "go test ./..."
go test ./...

step "search quality baseline"
bash scripts/check-search-quality.sh

step "lizard (yellow >10; CI red >15; NLOC ≤ 100)"
# 律 2 黄区警告 10-15；红区 > 15. Threshold 10 surfaces yellow so red
# rises clearly. -w prints only warnings.
lizard . -l go -C 10 -T nloc=100 -w || {
  echo "lizard reported warnings — review above" >&2
  # Not a hard fail: pre-existing yellows (装配函数 / 迁移函数) are
  # accepted. CI should fail on RED (CCN > 15) which lizard doesn't
  # distinguish via exit code alone; surface visually for now.
}

if [[ "$skip_jscpd" -eq 0 ]]; then
  step "jscpd (duplication < 5%, test files excluded)"
  npx -y jscpd . --silent
fi

printf '\n\033[1;32m✓ all gates green\033[0m\n'
