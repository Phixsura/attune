#!/usr/bin/env bash
# lint-maturity-contract — enforce the platform maturity program's proposal
# graph and verification metadata.

set -euo pipefail

cd "$(dirname "$0")/.."

umbrella="docs/proposals/2026/07/2026-07-05-platform-maturity-program.md"
tracks=(
  "docs/proposals/2026/07/2026-07-05-governance-plane-service-accounts-delegated-admin.md"
  "docs/proposals/2026/07/2026-07-05-extension-plane-registry-risk-signatures.md"
  "docs/proposals/2026/07/2026-07-05-lifecycle-plane-recovery-compatibility-contract.md"
  "docs/proposals/2026/07/2026-07-05-observability-plane-release-health-ownership-views.md"
  "docs/proposals/2026/07/2026-07-05-developer-parity-plane-fresh-clone-seeds.md"
  "docs/proposals/2026/07/2026-07-05-semantics-plane-stable-vocabulary-compatibility.md"
  "docs/proposals/2026/07/2026-07-05-enforcement-plane-ci-doc-gates.md"
)

fail=0

require_file() {
  local file="$1"
  if [[ ! -f "$file" ]]; then
    echo "ERROR: missing file $file" >&2
    fail=1
    return 1
  fi
}

extract_status() {
  local file="$1"
  local line status
  line="$(grep -m1 -E '^\|[[:space:]]*(\*\*)?Status(\*\*)?[[:space:]]*\|' "$file" || true)"
  if [[ -z "$line" ]]; then
    printf '\n'
    return 0
  fi
  status="$(printf '%s' "$line" | cut -d'|' -f3 | xargs)"
  printf '%s\n' "$status"
}

require_status() {
  local file="$1"
  local status
  status="$(extract_status "$file")"
  if [[ -z "$status" ]]; then
    echo "ERROR $file: missing Status row" >&2
    fail=1
    return 0
  fi
  case "$status" in
    Accepted|Implemented) ;;
    *)
      echo "ERROR $file: status must be Accepted or Implemented (got $status)" >&2
      fail=1
      ;;
  esac
}

require_verification() {
  local file="$1"
  if ! grep -qE '^## Verification|^### Verification' "$file"; then
    echo "ERROR $file: missing Verification section" >&2
    fail=1
  fi
}

require_related_umbrella() {
  local file="$1"
  if ! grep -qF 'platform-maturity-program.md' "$file"; then
    echo "ERROR $file: missing link back to the platform maturity umbrella" >&2
    fail=1
  fi
}

require_umbrella_child_link() {
  local child="$1"
  local base
  base="$(basename "$child")"
  if ! grep -qF "$base" "$umbrella"; then
    echo "ERROR $umbrella: missing child proposal link for $base" >&2
    fail=1
  fi
}

require_file "$umbrella"
require_status "$umbrella"
if ! grep -q '^## Child Proposals' "$umbrella"; then
  echo "ERROR $umbrella: missing Child Proposals section" >&2
  fail=1
fi

for track in "${tracks[@]}"; do
  require_file "$track"
  require_status "$track"
  require_verification "$track"
  require_related_umbrella "$track"
  require_umbrella_child_link "$track"
done

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi

echo "lint-maturity-contract: clean"
