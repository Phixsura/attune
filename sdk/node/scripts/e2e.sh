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
pg() { docker exec "$CONTAINER" psql -U attune -d attune -tAc "$1"; }

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

# A second key restricted to ingest:write ONLY, so the e2e can verify scope
# denial (tag/workflow write → 403). The CLI issues unrestricted keys; we pin a
# scope row directly, which restricts the key to that single scope.
RKEY_OUT="$("$BIN" --config "$CONFIG" keys issue --tenant sdke2e --label "$MARKER-restricted" 2>/dev/null)"
RKEY="$(echo "$RKEY_OUT" | awk '/ key:/{print $2}')"
RKEY_ID="$(echo "$RKEY_OUT" | awk '/ id:/{print $2}')"
# Guard BOTH before the insert: a missing RKEY_ID would otherwise INSERT an empty
# key_id and the scope restriction would silently never apply (or fail opaquely).
[ -n "$RKEY" ] && [ -n "$RKEY_ID" ] || { echo "failed to issue/parse restricted key"; exit 1; }
pg "insert into api_key_scopes (key_id, scope) values ('${RKEY_ID}', 'ingest:write');" >/dev/null

# A second tenant + key, to verify cross-tenant isolation.
"$BIN" --config "$CONFIG" tenant create --slug sdke2e2 --name "SDK E2E 2" >/dev/null
TKEY="$("$BIN" --config "$CONFIG" keys issue --tenant sdke2e2 --label "$MARKER-t2" 2>/dev/null \
  | awk '/ key:/{print $2}')"
[ -n "$TKEY" ] || { echo "failed to issue tenant-2 key"; exit 1; }

log "build the SDK package (dist/)"
( cd "$SDK_DIR" && pnpm exec tsdown >/dev/null )

log "run live vitest e2e suite against ${BASE_URL}"
(
  cd "$SDK_DIR"
  ATTUNE_E2E_BASE_URL="$BASE_URL" ATTUNE_E2E_API_KEY="$KEY" \
    ATTUNE_E2E_RESTRICTED_KEY="$RKEY" ATTUNE_E2E_TENANT2_KEY="$TKEY" ATTUNE_E2E_MARKER="$MARKER" \
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

log "verify CONCURRENT dedup at the DB level (8 simultaneous → 1 row)"
CONC="$(docker exec "$CONTAINER" psql -U attune -d attune -tAc \
  "select count(*) from user_feedback where content = '${MARKER} idem-concurrent';")"
echo "  rows for concurrent idempotency key: ${CONC}"
[ "$CONC" = "1" ] || { echo "expected exactly 1 row for concurrent key, got ${CONC}"; exit 1; }

log "integration: pack the published artifact, install it in a fresh external project, ingest (ESM + CJS)"
# npm pack runs prepack (tsdown) and writes the real publishable tarball — the
# same bytes `npm publish` would upload. We install it into a throwaway project
# with plain npm (no src, no workspace link) so this exercises the package
# exactly as an external consumer would: exports map, dist, types, zero deps.
( cd "$SDK_DIR" && npm pack --silent --pack-destination "$WORK" >/dev/null 2>&1 )
TARBALL="$(ls "$WORK"/*.tgz | head -1)"
[ -n "$TARBALL" ] || { echo "npm pack produced no tarball"; exit 1; }
echo "  packed: $(basename "$TARBALL")"
CONSUMER="$WORK/consumer"
mkdir -p "$CONSUMER"
(
  cd "$CONSUMER"
  npm init -y >/dev/null 2>&1
  npm install --silent --no-audit --no-fund "$TARBALL" >/dev/null 2>&1
)
cat > "$CONSUMER/esm.mjs" <<'JS'
import { Client } from '@phixsura/attune'
const { id } = await new Client({ baseURL: process.env.B, apiKey: process.env.K })
  .ingest({ content: process.env.MARK + ' consumer-esm' })
if (!/^\d+$/.test(id)) { console.error('ESM: bad id', id); process.exit(1) }
console.log('  ESM consumer id=' + id)
JS
cat > "$CONSUMER/cjs.cjs" <<'JS'
const { Client, ErrorCode } = require('@phixsura/attune')
if (ErrorCode.VALIDATION !== 'VALIDATION') { console.error('CJS: enum missing'); process.exit(1) }
new Client({ baseURL: process.env.B, apiKey: process.env.K })
  .ingest({ content: process.env.MARK + ' consumer-cjs' })
  .then(({ id }) => {
    if (!/^\d+$/.test(id)) { console.error('CJS: bad id', id); process.exit(1) }
    console.log('  CJS consumer id=' + id)
  })
  .catch((e) => { console.error('CJS:', e); process.exit(1) })
JS
B="$BASE_URL" K="$KEY" MARK="$MARKER" node "$CONSUMER/esm.mjs"
B="$BASE_URL" K="$KEY" MARK="$MARKER" node "$CONSUMER/cjs.cjs"

log "verify the package bundles for the browser (esbuild platform=browser, no Node built-ins)"
cat > "$CONSUMER/browser-entry.mjs" <<'JS'
import { Client, AttuneError, ErrorCode } from '@phixsura/attune'
const c = new Client({ baseURL: 'https://x.example', apiKey: 'ak_pub' })
globalThis.__keep = [typeof c.ingest, typeof AttuneError, ErrorCode.VALIDATION]
JS
(
  cd "$CONSUMER"
  npx -y esbuild@latest browser-entry.mjs --bundle --platform=browser --format=esm --outfile=browser-bundle.js >/dev/null 2>&1
)
[ -f "$CONSUMER/browser-bundle.js" ] || { echo "browser bundle failed to build"; exit 1; }
if grep -qE 'node:|require\("(http|crypto|stream|buffer|net|tls|fs)"\)' "$CONSUMER/browser-bundle.js"; then
  echo "browser bundle leaked a Node built-in"; exit 1
fi
echo "  browser bundle OK ($(wc -c < "$CONSUMER/browser-bundle.js") bytes, no Node built-ins)"

log "verify the external-consumer ingests landed in Postgres (ESM + CJS → 2 rows)"
CONS="$(docker exec "$CONTAINER" psql -U attune -d attune -tAc \
  "select count(*) from user_feedback where content like '${MARKER} consumer-%';")"
echo "  rows from the packed-tarball consumer: ${CONS}"
[ "$CONS" = "2" ] || { echo "expected 2 consumer rows (esm+cjs), got ${CONS}"; exit 1; }

log "E2E PASSED — SDK ↔ live server fully verified (incl. packed-artifact consumer)"
