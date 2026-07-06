#!/usr/bin/env bash
# attune — ensure production HTTP responses are emitted by dispatcher/respond-owned code.

set -euo pipefail

cd "$(dirname "$0")/.."

fail=0

while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  file="${line%%:*}"
  case "$file" in
    internal/dispatcher/*|internal/respond/*|\
    internal/pkg/*|\
    internal/preflight/*|\
    internal/mcp/*|\
    internal/outbound/outboundtest/*|\
    internal/service/auditevidence/*|\
    internal/handlers/apiversion/*|\
    internal/handlers/requestvalidation/*|\
    internal/handlers/timeout/*|\
    internal/handlers/cors/*|\
    internal/handlers/etag/*|\
    internal/handlers/mcp/*|\
    internal/handlers/console/apikey_idempotency.go|\
    internal/handlers/console/auth/*|\
    internal/handlers/console/auditlog/*|\
    internal/handlers/console/mcpclient/*|\
    internal/handlers/console/oidc/*|\
    internal/handlers/console/system/*|\
    cmd/attune/router.go) continue ;;
    *_test.go) continue ;;
  esac
  printf 'direct HTTP response emission: %s\n' "$line" >&2
  fail=1
done < <(
  git grep -nE 'respond\.(Error|Proto)|WriteHeader\(|http\.Error\(|http\.Redirect\(|http\.SetCookie\((w|rw|resp|res|writer),|json\.NewEncoder\((w|rw|resp|res|writer)\)|io\.WriteString\((w|rw|resp|res|writer),|fmt\.Fprint(f|ln)?\((w|rw|resp|res|writer),|(^|[^[:alnum:]_])(w|rw|resp|res|writer)\.Write\(' -- \
    'internal/**/*.go' \
    'cmd/attune/*.go' \
    ':!**/*_test.go' \
    ':!internal/proto/**' \
    ':!console/src/proto/**' || true
)

if [[ "$fail" -ne 0 ]]; then
  cat >&2 <<'EOF'

Use dispatcher.Bind for endpoint handlers or dispatcher.Reject for middleware.
internal/respond is the low-level encoder, not a production handler dependency.
EOF
  exit 1
fi

echo "lint-http-response-emission: clean"
