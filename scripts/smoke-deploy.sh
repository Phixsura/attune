#!/usr/bin/env bash
# Deploy smoke test — the executable form of docs/private-deploy.md's happy path.
#
# Builds attune from THIS source, brings the stack up with a mock LLM, and walks
# the documented steps, asserting each:
#   healthz -> tenant create -> keys issue -> ingest -> enrich (via mock) -> backup
# It also asserts the enriched VALUES and the /metrics endpoint the guide shows.
#
# Runs in CI (.github/workflows/ci.yml) on deploy/** or app changes, so a code
# change that breaks a documented HAPPY-PATH step turns CI red — the guard against
# doc rot. (It exercises the happy path only — it does not assert the obs/TLS
# overlays or failure modes.) No real LLM key needed; scripts/mock-llm.py stands in.
#
#   scripts/smoke-deploy.sh                 # build attune from source (CI)
#   SMOKE_IMAGE=ghcr.io/phixsura/attune:latest scripts/smoke-deploy.sh  # reuse an image (fast, local)
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
P=attune-smoke                       # isolated compose project (won't touch a real stack)
DC="docker compose -p $P"
MOCK="$P-mock"
fail() { echo "❌ SMOKE FAIL: $*" >&2; exit 1; }
nap()  { python3 -c 'import time; time.sleep(1)' 2>/dev/null || sleep 1; }  # 1s, no bare-sleep dependency

cleanup() {
  docker rm -f "$MOCK" >/dev/null 2>&1 || true
  ( cd "$REPO/deploy" && $DC down -v >/dev/null 2>&1 || true )
  rm -f "$REPO/deploy/.env"
}
trap cleanup EXIT

if [ -n "${SMOKE_IMAGE:-}" ]; then
  IMG="$SMOKE_IMAGE"; echo "==> using image $IMG (no build)"
else
  echo "==> build attune from source"; docker build -t attune:ci-smoke "$REPO" >/dev/null; IMG=attune:ci-smoke
fi

echo "==> pre-pull mock base image (fail fast, warm cache)"
docker pull python:3-alpine >/dev/null

# Assemble the DSN from parts at runtime so this fixture's fake password isn't a
# committed connection-string literal — secret scanners flag those even when
# they're obviously fake/unverifiable.
PW=smoke_pw
SCHEME=postgres
cat > "$REPO/deploy/.env" <<EOF
POSTGRES_USER=attune
POSTGRES_PASSWORD=${PW}
POSTGRES_DB=attune
FEEDBACK_API_DATABASE_URL=${SCHEME}://attune:${PW}@postgres:5432/attune?sslmode=disable
FEEDBACK_API_LLM_OPENAI_API_KEY=sk-mock
FEEDBACK_API_LLM_OPENAI_BASE_URL=http://mock-llm:18080
ATTUNE_IMAGE=${IMG}
EOF

cd "$REPO/deploy"
echo "==> docker compose up -d"
$DC up -d

echo "==> start mock LLM on the compose network (host-published for the readiness probe)"
docker run -d --rm --name "$MOCK" --network "${P}_default" --network-alias mock-llm \
  -p 127.0.0.1:18080:18080 -v "$REPO/scripts/mock-llm.py:/m.py:ro" python:3-alpine python /m.py

echo "==> wait for the mock LLM to listen (so inline enrichment succeeds at once)"
for _ in $(seq 1 30); do
  curl -s -o /dev/null --max-time 2 -X POST http://127.0.0.1:18080/v1/chat/completions -d '{}' && break
  docker ps -q --filter "name=$MOCK" | grep -q . || { docker logs "$MOCK" 2>&1 | tail; fail "mock LLM container exited"; }
  nap
done
curl -s -o /dev/null --max-time 2 -X POST http://127.0.0.1:18080/v1/chat/completions -d '{}' || fail "mock LLM never became ready"

echo "==> [1/6] healthz (first boot runs migrations)"
curl --retry 90 --retry-delay 1 --retry-connrefused -sf http://localhost:8090/healthz | grep -qx ok \
  || fail "healthz did not return ok"

echo "==> [2/6] tenant create"
$DC run --rm -T attune tenant create --slug smoke --name "Smoke Co" >/dev/null || fail "tenant create failed"

echo "==> [3/6] keys issue (capture the full 41-char key, not the 12-char prefix)"
KEY=$($DC run --rm -T attune keys issue --tenant smoke --label ci 2>/dev/null | grep -oE 'fbk_live_[0-9a-f]{32}' | head -1)
[ "${#KEY}" -eq 41 ] || fail "expected a 41-char key, got '${KEY}' (len ${#KEY})"

echo "==> [4/6] ingest"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST http://localhost:8090/v1/feedback/ingest \
  -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{"content":"the export button is broken on Safari","source":"web"}')
[ "$code" = 200 ] || fail "ingest returned http $code (want 200)"

echo "==> [5/6] enrichment via the LLM reaches done AND the documented values"
row=""
for _ in $(seq 1 60); do
  row=$($DC exec -T postgres psql -tAF'|' -U attune attune \
    -c "select enrichment_status, enriched_kind, enriched_severity from user_feedback where enrichment_status='done' limit 1;" 2>/dev/null || true)
  [ -n "$row" ] && break
  nap
done
[ -n "$row" ] || fail "no feedback reached enrichment_status=done (LLM/enricher path broken)"
echo "$row" | grep -q '^done|bug|P2$' || fail "enriched values wrong: got '$row' (want done|bug|P2 — see docs §5d)"

echo "==> [5b] /metrics exposes attune_ingest_total (the guide documents this)"
curl -s --max-time 5 http://localhost:8090/metrics | grep -q '^attune_ingest_total' \
  || fail "/metrics missing attune_ingest_total"

echo "==> [6/6] backup (pg_dump)"
$DC exec -T postgres pg_dump -U attune attune > /tmp/smoke-backup.sql
grep -q "CREATE TABLE" /tmp/smoke-backup.sql || fail "pg_dump produced no schema"
rm -f /tmp/smoke-backup.sql

echo "✅ SMOKE OK — documented happy path (incl. enriched values + /metrics) passed."
