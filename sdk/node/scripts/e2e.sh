#!/usr/bin/env bash
# Full end-to-end harness for @phixsura/attune.
#
# Boots an isolated Postgres (pgvector) + a real attune server from source,
# provisions a tenant and an ingest:write key, builds the SDK, runs the live
# vitest e2e suite (test/e2e) against the server, then spot-checks Postgres for
# persisted rows and verifies CommonJS interop. Everything is torn down on exit.
#
# Usage:  pnpm e2e        (from sdk/node/)
# Needs:  docker, go, pnpm. Touches nothing the developer already has running.
set -euo pipefail

SDK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$SDK_DIR/../.." && pwd)"

CONTAINER="attune-sdk-e2e-$$"
DB_PORT=55444
SRV_PORT=8097
BASE_URL="http://127.0.0.1:${SRV_PORT}"
MARKER="sdk-e2e-$$"
WORK="$(mktemp -d)"
BIN="$WORK/attune"
CONFIG="$WORK/config.yaml"
SRV_LOG="$WORK/server.log"
SRV_PID=""

log() { printf '\n\033[1;36m▸ %s\033[0m\n' "$*"; }

cleanup() {
  log "teardown"
  [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null || true
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

log "start isolated pgvector (container=$CONTAINER port=$DB_PORT)"
docker run -d --name "$CONTAINER" \
  -e POSTGRES_USER=attune -e POSTGRES_PASSWORD=attune -e POSTGRES_DB=attune \
  -p "${DB_PORT}:5432" pgvector/pgvector:pg17 >/dev/null
for _ in $(seq 1 30); do
  docker exec "$CONTAINER" pg_isready -U attune -d attune >/dev/null 2>&1 && break
  sleep 1
done

log "build attune server from source"
( cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/attune )

log "write throwaway config (keyset generated fresh)"
KEYSET="$("$BIN" secrets generate-keyset 2>/dev/null)"
cat > "$CONFIG" <<EOF
port: ${SRV_PORT}
database:
  url: "postgres://attune:attune@127.0.0.1:${DB_PORT}/attune?sslmode=disable"
console:
  base_url: "${BASE_URL}"
  session_key: "e2e-only-session-key-at-least-32-chars-long-xxxx"
  bootstrap_admin:
    email: "admin@example.com"
    password: "e2e-admin-password-change-me"
secrets:
  tink_keyset: |
    ${KEYSET}
EOF

log "boot server (auto-migrates)"
"$BIN" --config "$CONFIG" server > "$SRV_LOG" 2>&1 &
SRV_PID=$!
for _ in $(seq 1 30); do
  [ "$(curl -s -m 2 "${BASE_URL}/healthz" 2>/dev/null)" = "ok" ] && break
  sleep 1
done
if [ "$(curl -s -m 2 "${BASE_URL}/healthz" 2>/dev/null)" != "ok" ]; then
  echo "server failed to come up; log:"; tail -20 "$SRV_LOG"; exit 1
fi

log "provision tenant + ingest:write key"
"$BIN" --config "$CONFIG" tenant create --slug sdke2e --name "SDK E2E" >/dev/null
KEY="$("$BIN" --config "$CONFIG" keys issue --tenant sdke2e --label "$MARKER" 2>/dev/null \
  | awk '/ key:/{print $2}')"
[ -n "$KEY" ] || { echo "failed to issue key"; exit 1; }

log "build the SDK package (dist/)"
( cd "$SDK_DIR" && pnpm exec tsdown >/dev/null )

log "run live vitest e2e suite against ${BASE_URL}"
(
  cd "$SDK_DIR"
  ATTUNE_E2E_BASE_URL="$BASE_URL" ATTUNE_E2E_API_KEY="$KEY" ATTUNE_E2E_MARKER="$MARKER" \
    pnpm exec vitest run test/e2e
)

log "verify rows persisted in Postgres"
ROWS="$(docker exec "$CONTAINER" psql -U attune -d attune -tAc \
  "select count(*) from user_feedback where content like '${MARKER}%';")"
echo "  rows tagged '${MARKER}': ${ROWS}"
[ "$ROWS" -ge 9 ] || { echo "expected >=9 persisted rows, got ${ROWS}"; exit 1; }

log "verify the full-fields row (source=web, sourceUser, pageUrl, sourceMeta)"
docker exec "$CONTAINER" psql -U attune -d attune -tAc \
  "select source, user_id, page_url, source_meta->>'plan' from user_feedback
   where content = '${MARKER} full-fields';" | tee "$WORK/full.txt"
grep -q '^web|' "$WORK/full.txt" || { echo "source not persisted as web"; exit 1; }
grep -q 'e2e-user-42' "$WORK/full.txt" || { echo "sourceUser not persisted"; exit 1; }
grep -q 'https://app.example.com/settings' "$WORK/full.txt" || { echo "pageUrl not persisted"; exit 1; }
grep -q 'pro' "$WORK/full.txt" || { echo "sourceMeta not persisted"; exit 1; }

log "verify idempotency dedup at the DB level (replay must NOT duplicate)"
DUP="$(docker exec "$CONTAINER" psql -U attune -d attune -tAc \
  "select count(*) from user_feedback where content = '${MARKER} idem-replay';")"
echo "  rows for replayed idempotency key: ${DUP}"
[ "$DUP" = "1" ] || { echo "expected exactly 1 row for replayed key, got ${DUP}"; exit 1; }

log "verify CommonJS interop (require the CJS build)"
node --input-type=commonjs -e "
const { Client, AttuneError, ErrorCode } = require('${SDK_DIR}/dist/index.cjs');
if (typeof Client !== 'function' || typeof AttuneError !== 'function' || ErrorCode.VALIDATION !== 'VALIDATION') {
  throw new Error('CJS interop broken');
}
console.log('  CJS require OK');
"

log "E2E PASSED — SDK ↔ live server fully verified"
