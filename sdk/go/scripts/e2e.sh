#!/usr/bin/env bash
# Full end-to-end harness for the attune Go SDK (github.com/Phixsura/attune/sdk/go).
#
# Boots an isolated Postgres (pgvector) + a real attune server from source,
# provisions a tenant and an ingest:write key, runs the live Go e2e suite
# (test/e2e, build tag `e2e`) against the server, then spot-checks Postgres for
# persisted rows, verifies idempotency + concurrent dedup at the DB level, smoke
# -runs the example CLI, and proves the module is importable by a fresh external
# consumer (go.mod replace). Everything is torn down on exit.
#
# Usage:  ./scripts/e2e.sh    (from sdk/go/)
# Needs:  docker, go. Touches nothing the developer already has running.
set -euo pipefail

SDK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$SDK_DIR/../.." && pwd)"

CONTAINER="attune-sdkgo-e2e-$$"
DB_PORT=55445
SRV_PORT=8098
BASE_URL="http://127.0.0.1:${SRV_PORT}"
MARKER="sdkgo-e2e-$$"
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
"$BIN" --config "$CONFIG" tenant create --slug sdkgoe2e --name "SDK Go E2E" >/dev/null
KEY="$("$BIN" --config "$CONFIG" keys issue --tenant sdkgoe2e --label "$MARKER" 2>/dev/null \
  | awk '/ key:/{print $2}')"
[ -n "$KEY" ] || { echo "failed to issue key"; exit 1; }

log "run live Go e2e suite (go test -tags e2e ./test/e2e) against ${BASE_URL}"
(
  cd "$SDK_DIR"
  ATTUNE_E2E_BASE_URL="$BASE_URL" ATTUNE_E2E_API_KEY="$KEY" ATTUNE_E2E_MARKER="$MARKER" \
    go test -tags e2e -count=1 -v ./test/e2e
)

log "verify rows persisted in Postgres"
ROWS="$(pg "select count(*) from user_feedback where content like '${MARKER}%';")"
echo "  rows tagged '${MARKER}': ${ROWS}"
[ "$ROWS" -ge 9 ] || { echo "expected >=9 persisted rows, got ${ROWS}"; exit 1; }

log "verify the full-fields row (source=web, sourceUser, pageUrl, sourceMeta)"
pg "select source, user_id, page_url, source_meta->>'plan' from user_feedback
    where content = '${MARKER} full-fields';" | tee "$WORK/full.txt"
grep -q '^web|' "$WORK/full.txt" || { echo "source not persisted as web"; exit 1; }
grep -q 'e2e-user-42' "$WORK/full.txt" || { echo "sourceUser not persisted"; exit 1; }
grep -q 'https://app.example.com/settings' "$WORK/full.txt" || { echo "pageUrl not persisted"; exit 1; }
grep -q 'pro' "$WORK/full.txt" || { echo "sourceMeta not persisted"; exit 1; }

log "verify idempotency dedup at the DB level (replay must NOT duplicate)"
DUP="$(pg "select count(*) from user_feedback where content = '${MARKER} idem-replay';")"
echo "  rows for replayed idempotency key: ${DUP}"
[ "$DUP" = "1" ] || { echo "expected exactly 1 row for replayed key, got ${DUP}"; exit 1; }

log "verify CONCURRENT dedup at the DB level (8 simultaneous → 1 row)"
CONC="$(pg "select count(*) from user_feedback where content = '${MARKER} idem-concurrent';")"
echo "  rows for concurrent idempotency key: ${CONC}"
[ "$CONC" = "1" ] || { echo "expected exactly 1 row for concurrent key, got ${CONC}"; exit 1; }

log "smoke-run the example CLI (built from source, ingest via stdin)"
CLI="$WORK/ingest-cli"
( cd "$SDK_DIR" && go build -o "$CLI" ./examples/ingest-cli )
echo "${MARKER} cli-stdin" | ATTUNE_BASE_URL="$BASE_URL" ATTUNE_API_KEY="$KEY" "$CLI"
CLIN="$(pg "select count(*) from user_feedback where content = '${MARKER} cli-stdin';")"
[ "$CLIN" = "1" ] || { echo "expected 1 row from the example CLI, got ${CLIN}"; exit 1; }

log "integration: import the module from a fresh external consumer (go.mod replace) and ingest"
# Mirrors npm pack/install: a throwaway module that imports the SDK by its public
# path, with a replace pointing at this working tree, proving the public import
# path + API compile and run for an external caller.
CONSUMER="$WORK/consumer"
mkdir -p "$CONSUMER"
cat > "$CONSUMER/go.mod" <<EOF
module attune-sdk-consumer

go 1.22

require github.com/Phixsura/attune/sdk/go v0.0.0

replace github.com/Phixsura/attune/sdk/go => ${SDK_DIR}
EOF
cat > "$CONSUMER/main.go" <<'GO'
package main

import (
	"context"
	"fmt"
	"os"

	attune "github.com/Phixsura/attune/sdk/go"
)

func main() {
	c, err := attune.New(os.Getenv("B"), os.Getenv("K"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	res, err := c.Ingest(context.Background(), attune.IngestInput{Content: os.Getenv("MARK") + " consumer"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("  external consumer id=" + res.ID)
}
GO
( cd "$CONSUMER" && go mod tidy >/dev/null 2>&1 && B="$BASE_URL" K="$KEY" MARK="$MARKER" go run . )
CONS="$(pg "select count(*) from user_feedback where content = '${MARKER} consumer';")"
echo "  rows from the external consumer: ${CONS}"
[ "$CONS" = "1" ] || { echo "expected 1 external-consumer row, got ${CONS}"; exit 1; }

log "E2E PASSED — Go SDK ↔ live server fully verified (incl. example CLI + external consumer)"
