#!/usr/bin/env bash
# Full end-to-end harness for @phixsura/attune.
#
# Boots an isolated Postgres (pgvector) + a real attune server from source,
# provisions a tenant and a scoped management key, builds the SDK, runs the
# live vitest e2e suite (test/e2e) against the server, then verifies the packed
# publish artifact from a fresh external project against the real management
# surface (audit, GDPR, outbox, MCP clients) plus CommonJS interop. Everything
# is torn down on exit.
#
# Usage:  pnpm e2e        (from sdk/node/)
# Needs:  docker, go, pnpm. Touches nothing the developer already has running.
set -euo pipefail

SDK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$SDK_DIR/../.." && pwd)"
PICK_FREE_PORT="$REPO_ROOT/scripts/pick-free-port.mjs"
PNPM=(corepack pnpm)

CONTAINER="attune-sdk-e2e-$$"
DB_PORT="$(node "$PICK_FREE_PORT" random)"
SRV_PORT="$(node "$PICK_FREE_PORT" random "$DB_PORT")"
WEBHOOK_PORT="$(node "$PICK_FREE_PORT" random "$DB_PORT" "$SRV_PORT")"
BROWSER_ALLOWED_PORT="$(node "$PICK_FREE_PORT" random "$DB_PORT" "$SRV_PORT" "$WEBHOOK_PORT")"
BROWSER_BLOCKED_PORT="$(node "$PICK_FREE_PORT" random "$DB_PORT" "$SRV_PORT" "$WEBHOOK_PORT" "$BROWSER_ALLOWED_PORT")"
BROWSER_ALLOWED_ORIGIN="http://127.0.0.1:${BROWSER_ALLOWED_PORT}"
BASE_URL="http://127.0.0.1:${SRV_PORT}"
MARKER="sdk-e2e-$$"
WORK="$(mktemp -d)"
BIN="$WORK/attune"
CONFIG="$WORK/config.yaml"
SRV_LOG="$WORK/server.log"
SDK_BUILD_LOG="$WORK/sdk-build.log"
SRV_PID=""
WEBHOOK_LOG="$WORK/webhook.log"
WEBHOOK_PID=""
AUDIT_SIGNING_KEY=""
AUDIT_PUBLIC_KEY=""

log() { printf '\n\033[1;36m▸ %s\033[0m\n' "$*"; }
pg() { docker exec "$CONTAINER" psql -U attune -d attune -tAc "$1"; }
stop_bg() {
  local pid="$1"
  [ -n "$pid" ] || return 0
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}

cleanup() {
  log "teardown"
  stop_bg "$WEBHOOK_PID"
  stop_bg "$SRV_PID"
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
AUDIT_SIGNING_KEY="$(openssl rand -hex 32)"
AUDIT_PUBLIC_KEY="$("$BIN" audit export-public-key --signing-key "$AUDIT_SIGNING_KEY" 2>/dev/null | awk '/public_key:/{print $2}')"
[ -n "$AUDIT_PUBLIC_KEY" ] || { echo "failed to derive audit evidence public key"; exit 1; }

log "write throwaway config (keyset generated fresh)"
KEYSET="$("$BIN" secrets generate-keyset 2>/dev/null)"
INDENTED_KEYSET="$(printf '%s\n' "$KEYSET" | sed 's/^/    /')"
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
security:
  allow_loopback_egress: true
audit_evidence:
  signing_key: "${AUDIT_SIGNING_KEY}"
ingest:
  cors_allowed_origins:
    - "${BROWSER_ALLOWED_ORIGIN}"
secrets:
  tink_keyset: |
${INDENTED_KEYSET}
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

log "provision tenant + scoped management key"
"$BIN" --config "$CONFIG" tenant create --slug sdke2e --name "SDK E2E" >/dev/null
KEY_OUT="$("$BIN" --config "$CONFIG" keys issue --tenant sdke2e --label "$MARKER" 2>/dev/null)"
KEY="$(echo "$KEY_OUT" | awk '/ key:/{print $2}')"
KEY_ID="$(echo "$KEY_OUT" | awk '/ id:/{print $2}')"
[ -n "$KEY" ] && [ -n "$KEY_ID" ] || { echo "failed to issue/parse management key"; exit 1; }
pg "insert into api_key_scopes (key_id, scope) values
  ('${KEY_ID}', 'ingest:write'),
  ('${KEY_ID}', 'tags:read'),
  ('${KEY_ID}', 'tags:write'),
  ('${KEY_ID}', 'workflow:read'),
  ('${KEY_ID}', 'workflow:write'),
  ('${KEY_ID}', 'audit:read'),
  ('${KEY_ID}', 'gdpr:read'),
  ('${KEY_ID}', 'gdpr:export'),
  ('${KEY_ID}', 'gdpr:delete'),
  ('${KEY_ID}', 'notify:read'),
  ('${KEY_ID}', 'notify:write'),
  ('${KEY_ID}', 'mcpclient:admin')
  on conflict do nothing;" >/dev/null

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
if ! ( cd "$SDK_DIR" && "${PNPM[@]}" exec tsdown >"$SDK_BUILD_LOG" 2>&1 ); then
  echo "SDK package build failed; log:"
  tail -80 "$SDK_BUILD_LOG"
  exit 1
fi

log "run live vitest e2e suite against ${BASE_URL}"
(
  cd "$SDK_DIR"
  ATTUNE_E2E_BASE_URL="$BASE_URL" ATTUNE_E2E_API_KEY="$KEY" \
    ATTUNE_E2E_RESTRICTED_KEY="$RKEY" ATTUNE_E2E_TENANT2_KEY="$TKEY" ATTUNE_E2E_MARKER="$MARKER" \
    "${PNPM[@]}" exec vitest run test/e2e
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

log "seed a failed outbox row for external-consumer management verification"
TENANT_ID="$(pg "select id from tenants where slug = 'sdke2e';")"
[ -n "$TENANT_ID" ] || { echo "failed to resolve tenant id"; exit 1; }
FEEDBACK_ID="$(pg "select id from user_feedback where tenant_id = '${TENANT_ID}' order by id limit 1;")"
[ -n "$FEEDBACK_ID" ] || { echo "failed to locate a feedback row for outbox fixture"; exit 1; }
cat > "$WORK/webhook.go" <<'GO'
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
)

type deliveryLog struct {
	DeliveryID  string `json:"delivery_id"`
	Signature   string `json:"signature"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

func main() {
	logPath := os.Args[1]
	addr := os.Args[2]
	http.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			panic(err)
		}
		entry := deliveryLog{
			DeliveryID:  r.Header.Get("X-Attune-Delivery-Id"),
			Signature:   r.Header.Get("X-Attune-Signature"),
			ContentType: r.Header.Get("Content-Type"),
			Body:        string(body),
		}
		if err := json.NewEncoder(f).Encode(entry); err != nil {
			_ = f.Close()
			panic(err)
		}
		_ = f.Close()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	if err := http.ListenAndServe(addr, nil); err != nil {
		panic(err)
	}
}
GO
go run "$WORK/webhook.go" "$WEBHOOK_LOG" "127.0.0.1:${WEBHOOK_PORT}" >/dev/null 2>&1 &
WEBHOOK_PID=$!
sleep 1
pg "insert into tenant_notify_targets
  (tenant_id, destination_type, audience, url, secret, timeout_seconds, disabled, updated_at)
  values
  ('${TENANT_ID}', 'raw-webhook', 'pool', 'http://127.0.0.1:${WEBHOOK_PORT}/hook', '0123456789abcdef0123456789abcdef', 1, false, now())
  on conflict (tenant_id, destination_type, audience)
  do update set
    url = excluded.url,
    secret = excluded.secret,
    timeout_seconds = excluded.timeout_seconds,
    disabled = false,
    updated_at = now();" >/dev/null
pg "insert into notify_outbox
  (feedback_id, tenant_id, destination_type, destination_target, audience, payload, status,
   attempts, next_retry_at, trace_id, last_error, failure_kind, http_status)
  values
  (${FEEDBACK_ID}, '${TENANT_ID}', 'raw-webhook', 'http://127.0.0.1:${WEBHOOK_PORT}/hook', 'pool',
   '{\"version\":\"1\",\"timestamp\":\"2026-06-28T00:00:00Z\",\"event_type\":\"feedback.test\",\"tenant_id\":\"${TENANT_ID}\",\"feedback\":{\"marker\":\"${MARKER}-outbox-1\",\"content\":\"${MARKER} outbox payload 1\"}}'::jsonb,
   'failed', 1, now() + interval '1 hour', 'manual-${MARKER}-1', 'seeded e2e failure', 'connection', 0),
  (${FEEDBACK_ID}, '${TENANT_ID}', 'raw-webhook', 'http://127.0.0.1:${WEBHOOK_PORT}/hook', 'pool',
   '{\"version\":\"1\",\"timestamp\":\"2026-06-28T00:00:00Z\",\"event_type\":\"feedback.test\",\"tenant_id\":\"${TENANT_ID}\",\"feedback\":{\"marker\":\"${MARKER}-outbox-2\",\"content\":\"${MARKER} outbox payload 2\"}}'::jsonb,
   'failed', 1, now() + interval '1 hour', 'manual-${MARKER}-2', 'seeded e2e failure', 'connection', 0);" >/dev/null

log "integration: pack the published artifact, install it in a fresh external project, and use the SDK like a real management client"
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
import { writeFile } from 'node:fs/promises'
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

async function waitForAuditEvidence(client, jobId) {
  for (let i = 0; i < 30; i++) {
    const job = await client.getAuditEvidenceExport(jobId)
    if (job.status === 'completed') return job
    if (job.status === 'failed') throw new Error(`audit evidence failed: ${job.error ?? 'unknown error'}`)
    await sleep(Math.max(500, (job.retryAfterSeconds || 1) * 1000))
  }
  throw new Error('audit evidence export timed out')
}

async function waitForGdprExport(client, jobId) {
  for (let i = 0; i < 30; i++) {
    const job = await client.getGdprExport(jobId)
    if (job.status === 'GDPR_EXPORT_STATUS_COMPLETED') return job
    if (job.status === 'GDPR_EXPORT_STATUS_FAILED') {
      throw new Error(`gdpr export failed: ${job.error ?? 'unknown error'}`)
    }
    await sleep(Math.max(500, (job.retryAfterSeconds || 1) * 1000))
  }
  throw new Error('gdpr export timed out')
}

async function waitForOutboxDelivery(client, id) {
  for (let i = 0; i < 30; i++) {
    for await (const delivery of client.iterateOutboxDeliveries({
      status: ['pending', 'delivered', 'failed', 'dead'],
      limit: 1,
    })) {
      if (delivery.id === id) {
        if (delivery.status === 'delivered') return delivery
        if (delivery.status === 'failed' || delivery.status === 'dead') {
          throw new Error(`outbox delivery reached terminal status ${delivery.status}`)
        }
        break
      }
    }
    await sleep(1000)
  }
  throw new Error(`outbox delivery ${id} never reached delivered`)
}

const client = new Client({ baseURL: process.env.B, apiKey: process.env.K })
const mark = process.env.MARK
const subjectKey = `subject-${mark}`
const subjectKey2 = `subject-${mark}-secondary`
const olderOutboxTraceId = `manual-${mark}-1`
const newerOutboxTraceId = `manual-${mark}-2`

const { id } = await client.ingest({ content: `${mark} consumer-esm` })
if (!/^\d+$/.test(id)) {
  console.error('ESM: bad id', id)
  process.exit(1)
}

const olderTag = await client.createTag({ name: `consumer-tag-${mark}-older` })
const newerTag = await client.createTag({ name: `consumer-tag-${mark}-newer` })
if (!olderTag.id || !newerTag.id) throw new Error('createTag returned empty id')
await client.updateTag({ id: newerTag.id, name: `consumer-tag-${mark}-newer-v2`, color: '#22c55e' })

const auditPage = await client.listAuditLog({ action: 'tag.create', limit: 10 })
if (!(auditPage.items ?? []).some((item) => item.targetId === olderTag.id || item.targetId === newerTag.id)) {
  throw new Error('audit log did not include created tags')
}
let foundOlderTagAudit = false
for await (const item of client.iterateAuditLog({ action: 'tag.create', limit: 1 })) {
  if (item.targetId === olderTag.id) {
    foundOlderTagAudit = true
    break
  }
}
if (!foundOlderTagAudit) throw new Error('audit iterator never reached the older tag create event')

const auditCSV = await client.exportAuditLogCSV({ action: 'tag.create' })
if (auditCSV.data.length === 0) throw new Error('audit csv export was empty')

const evidence = await client.createAuditEvidenceExport({ actions: ['tag.create'] })
const evidenceJob = await waitForAuditEvidence(client, evidence.jobId)
const evidenceZip = await client.downloadAuditEvidenceExport(evidence.jobId)
if (!evidenceJob.downloadPath) throw new Error('audit evidence download path missing')
if (evidenceZip.data.length === 0 || !evidenceZip.filename?.endsWith('.zip')) {
  throw new Error('audit evidence archive missing')
}
await writeFile(process.env.AUDIT_ZIP, evidenceZip.data)

const subjectSeed = await client.ingest({
  content: `${mark} subject-seed`,
  sourceUser: subjectKey,
})
if (!/^\d+$/.test(subjectSeed.id)) throw new Error('subject seed ingest failed')
const subjectSeed2 = await client.ingest({
  content: `${mark} subject-seed-secondary`,
  sourceUser: subjectKey2,
})
if (!/^\d+$/.test(subjectSeed2.id)) throw new Error('secondary subject seed ingest failed')

const gdprOps = await client.getGdprOperations()
if (!gdprOps.stepUp?.satisfied || gdprOps.stepUp.method !== 'api_key') {
  throw new Error('gdpr operations did not report api_key step-up')
}

const gdprExport = await client.exportGdprSubject({ subjectKey })
const gdprJob = await waitForGdprExport(client, gdprExport.jobId)
const gdprZip = await client.downloadGdprExport(gdprExport.jobId)
if (gdprJob.feedbackCount < 1 || gdprZip.data.length === 0 || !gdprZip.filename?.endsWith('.zip')) {
  throw new Error('gdpr export archive missing subject data')
}
await writeFile(process.env.GDPR_ZIP, gdprZip.data)
const revoked = await client.revokeGdprExport(gdprExport.jobId)
if (revoked.status !== 'GDPR_EXPORT_STATUS_REVOKED') {
  throw new Error(`gdpr export revoke failed: ${revoked.status}`)
}

const gdprDelete = await client.deleteGdprSubject({ subjectKey })
const gdprDelete2 = await client.deleteGdprSubject({ subjectKey: subjectKey2 })
if (!gdprDelete.requestId || !gdprDelete2.requestId) throw new Error('gdpr delete request id missing')
const deleteRequests = await client.listGdprRequests({ requestType: 'delete', limit: 25 })
if (
  !(deleteRequests.items ?? []).some(
    (item) => item.requestId === gdprDelete.requestId || item.requestId === gdprDelete2.requestId,
  )
) {
  throw new Error('gdpr delete requests not listed')
}
let foundOlderDelete = false
for await (const item of client.iterateGdprRequests({ requestType: 'delete', limit: 1 })) {
  if (item.requestId === gdprDelete.requestId) {
    foundOlderDelete = true
    break
  }
}
if (!foundOlderDelete) throw new Error('gdpr request iterator never reached the older delete request')
const cancelled = await client.cancelGdprRequest(gdprDelete.requestId)
const cancelled2 = await client.cancelGdprRequest(gdprDelete2.requestId)
if (
  cancelled.status !== 'GDPR_REQUEST_STATUS_CANCELLED' ||
  cancelled2.status !== 'GDPR_REQUEST_STATUS_CANCELLED'
) {
  throw new Error('gdpr delete cancel failed')
}

const deliveries = await client.listOutboxDeliveries({ status: ['failed'], limit: 25 })
const seededDelivery = (deliveries.deliveries ?? []).find((delivery) => delivery.traceId === olderOutboxTraceId)
if (!seededDelivery) throw new Error('seeded outbox delivery not found')
if (!(deliveries.deliveries ?? []).some((delivery) => delivery.traceId === newerOutboxTraceId)) {
  throw new Error('newer outbox delivery not found')
}
let foundOlderDelivery = false
for await (const delivery of client.iterateOutboxDeliveries({ status: ['failed'], limit: 1 })) {
  if (delivery.traceId === olderOutboxTraceId) {
    foundOlderDelivery = true
    break
  }
}
if (!foundOlderDelivery) throw new Error('outbox iterator never reached the older failed row')
const retried = await client.retryOutboxDelivery(seededDelivery.id)
if (retried.delivery?.status !== 'pending') {
  throw new Error(`outbox retry did not re-arm row: ${retried.delivery?.status ?? 'missing delivery'}`)
}
const delivered = await waitForOutboxDelivery(client, seededDelivery.id)
if (delivered.status !== 'delivered') {
  throw new Error(`outbox delivery did not complete after retry: ${delivered.status}`)
}
await writeFile(process.env.OUTBOX_DELIVERY_ID, `${seededDelivery.id}\n`)

const createdMCP = await client.createMCPClient({
  name: `consumer-mcp-${mark}`,
  redirectUris: ['https://example.com/callback'],
  scopes: ['mcp:read'],
})
const clientId = createdMCP.client?.id
if (!clientId) throw new Error('createMCPClient returned empty id')
const listedMCP = await client.listMCPClients()
if (!(listedMCP.clients ?? []).some((item) => item.id === clientId)) {
  throw new Error('listMCPClients missed created client')
}
const fetchedMCP = await client.getMCPClient(clientId)
if (fetchedMCP.connection?.serverUrl !== `${process.env.B}/mcp`) {
  throw new Error(`unexpected MCP server url: ${fetchedMCP.connection?.serverUrl ?? ''}`)
}
await client.updateMCPClient({
  id: clientId,
  toolPolicyMode: 'allow_list',
  rateLimitRpm: 9,
  rateLimitBurst: 3,
})
const toolPolicies = await client.replaceMCPClientToolPolicies({
  id: clientId,
  policies: [{ toolName: 'list_feedback', effect: 'allow', rateLimitRpm: 7 }],
})
if (!(toolPolicies.tools ?? []).some((tool) => tool.name === 'list_feedback' && tool.effectiveAllowed)) {
  throw new Error('mcp tool policy replacement did not take effect')
}
await client.revokeMCPClient(clientId)
const revokedMCP = await client.getMCPClient(clientId)
if (!revokedMCP.client?.revokedAt) throw new Error('mcp client revoke did not persist')

await client.archiveTag(olderTag.id)
await client.archiveTag(newerTag.id)

console.log(`  ESM consumer id=${id}`)
console.log(`  ESM management flow OK (audit=${evidence.jobId} gdpr=${gdprExport.jobId} mcp=${clientId})`)
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
B="$BASE_URL" K="$KEY" MARK="$MARKER" AUDIT_ZIP="$CONSUMER/audit-evidence.zip" GDPR_ZIP="$CONSUMER/gdpr-export.zip" OUTBOX_DELIVERY_ID="$CONSUMER/outbox-delivery-id.txt" node "$CONSUMER/esm.mjs"
B="$BASE_URL" K="$KEY" MARK="$MARKER" node "$CONSUMER/cjs.cjs"

log "verify the packed artifact from a real browser across allowed + blocked origins"
(
  cd "$SDK_DIR"
  ATTUNE_BROWSER_E2E_CONSUMER_DIR="$CONSUMER" \
    ATTUNE_BROWSER_E2E_BASE_URL="$BASE_URL" \
    ATTUNE_BROWSER_E2E_API_KEY="$RKEY" \
    ATTUNE_BROWSER_E2E_MARKER="$MARKER" \
    ATTUNE_BROWSER_E2E_ALLOWED_PORT="$BROWSER_ALLOWED_PORT" \
    ATTUNE_BROWSER_E2E_BLOCKED_PORT="$BROWSER_BLOCKED_PORT" \
    node scripts/browser-smoke.mjs
)

log "verify browser ingest persisted only from the allowlisted origin"
BROWSER_ALLOWED_ROWS="$(docker exec "$CONTAINER" psql -U attune -d attune -tAc \
  "select count(*) from user_feedback where content = '${MARKER} browser-allowed';")"
echo "  rows for allowlisted browser origin: ${BROWSER_ALLOWED_ROWS}"
[ "$BROWSER_ALLOWED_ROWS" = "1" ] || {
  echo "expected exactly 1 allowlisted browser row, got ${BROWSER_ALLOWED_ROWS}"; exit 1;
}
docker exec "$CONTAINER" psql -U attune -d attune -tAc \
  "select source, page_url from user_feedback where content = '${MARKER} browser-allowed';" \
  | tee "$WORK/browser-allowed.txt"
grep -q "^web|${BROWSER_ALLOWED_ORIGIN}/browser-smoke.html$" "$WORK/browser-allowed.txt" \
  || { echo "browser allowlisted row missing expected source/page_url"; exit 1; }
BROWSER_BLOCKED_ROWS="$(docker exec "$CONTAINER" psql -U attune -d attune -tAc \
  "select count(*) from user_feedback where content = '${MARKER} browser-blocked';")"
echo "  rows for blocked browser origin: ${BROWSER_BLOCKED_ROWS}"
[ "$BROWSER_BLOCKED_ROWS" = "0" ] || {
  echo "expected 0 rows from blocked browser origin, got ${BROWSER_BLOCKED_ROWS}"; exit 1;
}

log "verify downloaded audit evidence and GDPR bundles like a real operator"
"$BIN" audit verify-export --public-key "$AUDIT_PUBLIC_KEY" "$CONSUMER/audit-evidence.zip"
unzip -p "$CONSUMER/audit-evidence.zip" manifest.json | grep -q '"format": "attune-audit-evidence-v1"' \
  || { echo "audit evidence manifest format mismatch"; exit 1; }
unzip -p "$CONSUMER/audit-evidence.zip" events.csv | grep -q 'tag.create' \
  || { echo "audit evidence events.csv missing tag.create"; exit 1; }
unzip -p "$CONSUMER/gdpr-export.zip" manifest.json | grep -q "\"subject_key\": \"subject-${MARKER}\"" \
  || { echo "gdpr manifest missing subject key"; exit 1; }
unzip -p "$CONSUMER/gdpr-export.zip" feedback.jsonl | grep -q "${MARKER} subject-seed" \
  || { echo "gdpr feedback export missing seeded content"; exit 1; }

log "verify manual outbox retry actually delivered to a live webhook receiver"
[ -s "$WEBHOOK_LOG" ] || { echo "webhook receiver saw no delivery"; exit 1; }
EXPECTED_DELIVERY_ID="$(tr -d '\n' < "$CONSUMER/outbox-delivery-id.txt")"
[ -n "$EXPECTED_DELIVERY_ID" ] || { echo "missing expected outbox delivery id artifact"; exit 1; }
grep -q "\"delivery_id\":\"${EXPECTED_DELIVERY_ID}\"" "$WEBHOOK_LOG" \
  || { echo "webhook receiver did not observe expected delivery id ${EXPECTED_DELIVERY_ID}"; cat "$WEBHOOK_LOG"; exit 1; }
grep -Eq '"signature":"[^"]+"' "$WEBHOOK_LOG" \
  || { echo "webhook receiver log missing attune signature"; cat "$WEBHOOK_LOG"; exit 1; }
grep -q "${MARKER}-outbox-1" "$WEBHOOK_LOG" \
  || { echo "webhook receiver payload missing expected marker"; cat "$WEBHOOK_LOG"; exit 1; }

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

log "E2E PASSED — SDK ↔ live server fully verified (incl. packed-artifact management consumer)"
