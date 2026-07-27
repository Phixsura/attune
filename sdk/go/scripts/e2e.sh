#!/usr/bin/env bash
# Full end-to-end harness for the attune Go SDK (github.com/Phixsura/attune/sdk/go).
#
# Boots an isolated Postgres (pgvector) + a real attune server from source,
# provisions a tenant and a scoped management key, runs the live Go e2e suite
# (test/e2e, build tag `e2e`) against the server, then spot-checks Postgres for
# persisted rows, verifies idempotency + concurrent dedup at the DB level, smoke
# -runs the example CLI, and proves the module is importable by a fresh external
# consumer that drives the real management surface (audit, GDPR, outbox, MCP
# clients). Everything is torn down on exit.
#
# Usage:  ./scripts/e2e.sh    (from sdk/go/)
# Needs:  docker, go. Touches nothing the developer already has running.
set -euo pipefail

SDK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$SDK_DIR/../.." && pwd)"
PICK_FREE_PORT="$REPO_ROOT/scripts/pick-free-port.mjs"

CONTAINER="attune-sdkgo-e2e-$$"
DB_PORT="$(node "$PICK_FREE_PORT" random)"
SRV_PORT="$(node "$PICK_FREE_PORT" random "$DB_PORT")"
WEBHOOK_PORT="$(node "$PICK_FREE_PORT" random "$DB_PORT" "$SRV_PORT")"
BASE_URL="http://127.0.0.1:${SRV_PORT}"
MARKER="sdkgo-e2e-$$"
WORK="$(mktemp -d)"
BIN="$WORK/attune"
CONFIG="$WORK/config.yaml"
SRV_LOG="$WORK/server.log"
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
"$BIN" --config "$CONFIG" tenant create --slug sdkgoe2e --name "SDK Go E2E" >/dev/null
KEY_OUT="$("$BIN" --config "$CONFIG" keys issue --tenant sdkgoe2e --label "$MARKER" 2>/dev/null)"
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
# scope row directly, which restricts the key to that scope.
RKEY_OUT="$("$BIN" --config "$CONFIG" keys issue --tenant sdkgoe2e --label "$MARKER-restricted" 2>/dev/null)"
RKEY="$(echo "$RKEY_OUT" | awk '/ key:/{print $2}')"
RKEY_ID="$(echo "$RKEY_OUT" | awk '/ id:/{print $2}')"
# Guard BOTH before the insert: a missing RKEY_ID would otherwise INSERT an empty
# key_id and the scope restriction would silently never apply (or fail opaquely).
[ -n "$RKEY" ] && [ -n "$RKEY_ID" ] || { echo "failed to issue/parse restricted key"; exit 1; }
pg "insert into api_key_scopes (key_id, scope) values ('${RKEY_ID}', 'ingest:write');" >/dev/null

# A second tenant + key, to verify cross-tenant isolation.
"$BIN" --config "$CONFIG" tenant create --slug sdkgoe2e2 --name "SDK Go E2E 2" >/dev/null
TKEY="$("$BIN" --config "$CONFIG" keys issue --tenant sdkgoe2e2 --label "$MARKER-t2" 2>/dev/null \
  | awk '/ key:/{print $2}')"
[ -n "$TKEY" ] || { echo "failed to issue tenant-2 key"; exit 1; }

log "run live Go e2e suite (go test -tags e2e ./test/e2e) against ${BASE_URL}"
(
  cd "$SDK_DIR"
  ATTUNE_E2E_BASE_URL="$BASE_URL" ATTUNE_E2E_API_KEY="$KEY" \
    ATTUNE_E2E_RESTRICTED_KEY="$RKEY" ATTUNE_E2E_TENANT2_KEY="$TKEY" ATTUNE_E2E_MARKER="$MARKER" \
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

log "seed a failed outbox row for external-consumer management verification"
TENANT_ID="$(pg "select id from tenants where slug = 'sdkgoe2e';")"
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

log "integration: import the module from a fresh external consumer (go.mod replace) and use the SDK like a real management client"
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
	"io"
	"os"
	"time"

	attune "github.com/Phixsura/attune/sdk/go"
)

func ptr[T any](v T) *T { return &v }

func waitForAuditEvidence(ctx context.Context, client *attune.Client, jobID string) (*attune.GetAuditEvidenceExportResponse, error) {
	for i := 0; i < 30; i++ {
		job, err := client.GetAuditEvidenceExport(ctx, jobID)
		if err != nil {
			return nil, err
		}
		if job.GetStatus() == "completed" {
			return job, nil
		}
		if job.GetStatus() == "failed" {
			return nil, fmt.Errorf("audit evidence failed: %s", job.GetError())
		}
		time.Sleep(time.Duration(max(1, int(job.GetRetryAfterSeconds()))) * time.Second)
	}
	return nil, fmt.Errorf("audit evidence export timed out")
}

func waitForGdprExport(ctx context.Context, client *attune.Client, jobID string) (*attune.GdprExportStatusResponse, error) {
	for i := 0; i < 30; i++ {
		job, err := client.GetGdprExport(ctx, jobID)
		if err != nil {
			return nil, err
		}
		if job.GetStatus().String() == "GDPR_EXPORT_STATUS_COMPLETED" {
			return job, nil
		}
		if job.GetStatus().String() == "GDPR_EXPORT_STATUS_FAILED" {
			return nil, fmt.Errorf("gdpr export failed: %s", job.GetError())
		}
		time.Sleep(time.Duration(max(1, int(job.GetRetryAfterSeconds()))) * time.Second)
	}
	return nil, fmt.Errorf("gdpr export timed out")
}

func waitForOutboxDelivery(ctx context.Context, client *attune.Client, id int64) (*attune.OutboxDelivery, error) {
	for i := 0; i < 30; i++ {
		pager := client.NewOutboxDeliveryPager(&attune.ListDeliveriesRequest{
			Status: []string{"pending", "delivered", "failed", "dead"},
			Limit:  1,
		})
		for {
			page, err := pager.NextPage(ctx)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			for _, delivery := range page.GetDeliveries() {
				if delivery.GetId() == id {
					switch delivery.GetStatus() {
					case "delivered":
						return delivery, nil
					case "failed", "dead":
						return nil, fmt.Errorf("outbox delivery reached terminal status %s", delivery.GetStatus())
					default:
						goto NextPoll
					}
				}
			}
		}
	NextPoll:
		time.Sleep(1 * time.Second)
	}
	return nil, fmt.Errorf("outbox delivery %d never reached delivered", id)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	c, err := attune.New(os.Getenv("B"), os.Getenv("K"), attune.WithTimeout(10*time.Second))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	mark := os.Getenv("MARK")
	subjectKey := "subject-" + mark
	subjectKey2 := "subject-" + mark + "-secondary"
	olderOutboxTraceID := "manual-" + mark + "-1"
	newerOutboxTraceID := "manual-" + mark + "-2"

	res, err := c.Ingest(ctx, attune.IngestInput{Content: mark + " consumer"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if res.ID == "" {
		fmt.Fprintln(os.Stderr, "consumer ingest returned empty id")
		os.Exit(1)
	}

	olderTag, err := c.CreateTag(ctx, &attune.CreateTagRequest{Name: "consumer-tag-" + mark + "-older"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "CreateTag:", err)
		os.Exit(1)
	}
	newerTag, err := c.CreateTag(ctx, &attune.CreateTagRequest{Name: "consumer-tag-" + mark + "-newer"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "CreateTag second:", err)
		os.Exit(1)
	}
	if _, err := c.UpdateTag(ctx, &attune.UpdateTagRequest{
		Id:    newerTag.GetId(),
		Name:  ptr("consumer-tag-" + mark + "-newer-v2"),
		Color: ptr("#22c55e"),
	}); err != nil {
		fmt.Fprintln(os.Stderr, "UpdateTag:", err)
		os.Exit(1)
	}

	auditPage, err := c.ListAuditLog(ctx, &attune.ListAuditLogRequest{Action: "tag.create", Limit: 10})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListAuditLog:", err)
		os.Exit(1)
	}
	foundTagAudit := false
	for _, item := range auditPage.GetItems() {
		if item.GetTargetId() == olderTag.GetId() || item.GetTargetId() == newerTag.GetId() {
			foundTagAudit = true
			break
		}
	}
	if !foundTagAudit {
		fmt.Fprintln(os.Stderr, "audit log did not include created tags")
		os.Exit(1)
	}
	auditPager := c.NewAuditLogPager(&attune.ListAuditLogRequest{Action: "tag.create", Limit: 1})
	foundOlderTagAudit := false
	for {
		page, err := auditPager.NextPage(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Audit pager:", err)
			os.Exit(1)
		}
		for _, item := range page.GetItems() {
			if item.GetTargetId() == olderTag.GetId() {
				foundOlderTagAudit = true
				break
			}
		}
		if foundOlderTagAudit {
			break
		}
	}
	if !foundOlderTagAudit {
		fmt.Fprintln(os.Stderr, "audit pager never reached the older tag create event")
		os.Exit(1)
	}

	auditCSV, err := c.ExportAuditLogCSV(ctx, &attune.ListAuditLogRequest{Action: "tag.create"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ExportAuditLogCSV:", err)
		os.Exit(1)
	}
	if len(auditCSV.Data) == 0 {
		fmt.Fprintln(os.Stderr, "audit csv export was empty")
		os.Exit(1)
	}

	evidence, err := c.CreateAuditEvidenceExport(ctx, &attune.CreateAuditEvidenceExportRequest{
		Actions: []string{"tag.create"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "CreateAuditEvidenceExport:", err)
		os.Exit(1)
	}
	evidenceJob, err := waitForAuditEvidence(ctx, c, evidence.GetJobId())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	evidenceZip, err := c.DownloadAuditEvidenceExport(ctx, evidence.GetJobId())
	if err != nil {
		fmt.Fprintln(os.Stderr, "DownloadAuditEvidenceExport:", err)
		os.Exit(1)
	}
	if evidenceJob.GetDownloadPath() == "" || len(evidenceZip.Data) == 0 {
		fmt.Fprintln(os.Stderr, "audit evidence archive missing")
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv("AUDIT_ZIP"), evidenceZip.Data, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write audit zip:", err)
		os.Exit(1)
	}

	subjectSeed, err := c.Ingest(ctx, attune.IngestInput{
		Content:    mark + " subject-seed",
		SourceUser: subjectKey,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "subject ingest:", err)
		os.Exit(1)
	}
	if subjectSeed.ID == "" {
		fmt.Fprintln(os.Stderr, "subject ingest returned empty id")
		os.Exit(1)
	}
	subjectSeed2, err := c.Ingest(ctx, attune.IngestInput{
		Content:    mark + " subject-seed-secondary",
		SourceUser: subjectKey2,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "secondary subject ingest:", err)
		os.Exit(1)
	}
	if subjectSeed2.ID == "" {
		fmt.Fprintln(os.Stderr, "secondary subject ingest returned empty id")
		os.Exit(1)
	}

	ops, err := c.GetGdprOperations(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetGdprOperations:", err)
		os.Exit(1)
	}
	if ops.GetStepUp() == nil || !ops.GetStepUp().GetSatisfied() || ops.GetStepUp().GetMethod() != "api_key" {
		fmt.Fprintln(os.Stderr, "gdpr operations did not report api_key step-up")
		os.Exit(1)
	}

	gdprExport, err := c.ExportGdprSubject(ctx, &attune.ExportGdprSubjectRequest{SubjectKey: subjectKey})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ExportGdprSubject:", err)
		os.Exit(1)
	}
	gdprJob, err := waitForGdprExport(ctx, c, gdprExport.GetJobId())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	gdprZip, err := c.DownloadGdprExport(ctx, gdprExport.GetJobId())
	if err != nil {
		fmt.Fprintln(os.Stderr, "DownloadGdprExport:", err)
		os.Exit(1)
	}
	if gdprJob.GetFeedbackCount() < 1 || len(gdprZip.Data) == 0 {
		fmt.Fprintln(os.Stderr, "gdpr export archive missing subject data")
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv("GDPR_ZIP"), gdprZip.Data, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write gdpr zip:", err)
		os.Exit(1)
	}
	revokedExport, err := c.RevokeGdprExport(ctx, gdprExport.GetJobId())
	if err != nil {
		fmt.Fprintln(os.Stderr, "RevokeGdprExport:", err)
		os.Exit(1)
	}
	if revokedExport.GetStatus().String() != "GDPR_EXPORT_STATUS_REVOKED" {
		fmt.Fprintln(os.Stderr, "gdpr export revoke did not return revoked status")
		os.Exit(1)
	}

	deleteReq, err := c.DeleteGdprSubject(ctx, &attune.DeleteGdprSubjectRequest{SubjectKey: subjectKey})
	if err != nil {
		fmt.Fprintln(os.Stderr, "DeleteGdprSubject:", err)
		os.Exit(1)
	}
	deleteReq2, err := c.DeleteGdprSubject(ctx, &attune.DeleteGdprSubjectRequest{SubjectKey: subjectKey2})
	if err != nil {
		fmt.Fprintln(os.Stderr, "DeleteGdprSubject second:", err)
		os.Exit(1)
	}
	deleteList, err := c.ListGdprRequests(ctx, &attune.ListGdprRequestsRequest{RequestType: "delete", Limit: 25})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListGdprRequests:", err)
		os.Exit(1)
	}
	foundDelete := false
	for _, item := range deleteList.GetItems() {
		if item.GetRequestId() == deleteReq.GetRequestId() || item.GetRequestId() == deleteReq2.GetRequestId() {
			foundDelete = true
			break
		}
	}
	if !foundDelete {
		fmt.Fprintln(os.Stderr, "gdpr delete requests not listed")
		os.Exit(1)
	}
	gdprPager := c.NewGdprRequestPager(&attune.ListGdprRequestsRequest{RequestType: "delete", Limit: 1})
	foundOlderDelete := false
	for {
		page, err := gdprPager.NextPage(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "GDPR pager:", err)
			os.Exit(1)
		}
		for _, item := range page.GetItems() {
			if item.GetRequestId() == deleteReq.GetRequestId() {
				foundOlderDelete = true
				break
			}
		}
		if foundOlderDelete {
			break
		}
	}
	if !foundOlderDelete {
		fmt.Fprintln(os.Stderr, "gdpr pager never reached the older delete request")
		os.Exit(1)
	}
	cancelled, err := c.CancelGdprRequest(ctx, deleteReq.GetRequestId())
	if err != nil {
		fmt.Fprintln(os.Stderr, "CancelGdprRequest:", err)
		os.Exit(1)
	}
	if cancelled.GetStatus().String() != "GDPR_REQUEST_STATUS_CANCELLED" {
		fmt.Fprintln(os.Stderr, "gdpr cancel did not return cancelled status")
		os.Exit(1)
	}
	cancelled2, err := c.CancelGdprRequest(ctx, deleteReq2.GetRequestId())
	if err != nil {
		fmt.Fprintln(os.Stderr, "CancelGdprRequest second:", err)
		os.Exit(1)
	}
	if cancelled2.GetStatus().String() != "GDPR_REQUEST_STATUS_CANCELLED" {
		fmt.Fprintln(os.Stderr, "second gdpr cancel did not return cancelled status")
		os.Exit(1)
	}

	deliveries, err := c.ListOutboxDeliveries(ctx, &attune.ListDeliveriesRequest{Status: []string{"failed"}, Limit: 25})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListOutboxDeliveries:", err)
		os.Exit(1)
	}
	var seededDelivery *attune.OutboxDelivery
	for _, delivery := range deliveries.GetDeliveries() {
		if delivery.GetTraceId() == olderOutboxTraceID {
			seededDelivery = delivery
			break
		}
	}
	if seededDelivery == nil {
		fmt.Fprintln(os.Stderr, "seeded outbox delivery not found")
		os.Exit(1)
	}
	foundNewerFailed := false
	for _, delivery := range deliveries.GetDeliveries() {
		if delivery.GetTraceId() == newerOutboxTraceID {
			foundNewerFailed = true
			break
		}
	}
	if !foundNewerFailed {
		fmt.Fprintln(os.Stderr, "newer outbox delivery not found")
		os.Exit(1)
	}
	outboxPager := c.NewOutboxDeliveryPager(&attune.ListDeliveriesRequest{Status: []string{"failed"}, Limit: 1})
	foundOlderFailed := false
	for {
		page, err := outboxPager.NextPage(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Outbox pager:", err)
			os.Exit(1)
		}
		for _, delivery := range page.GetDeliveries() {
			if delivery.GetTraceId() == olderOutboxTraceID {
				foundOlderFailed = true
				break
			}
		}
		if foundOlderFailed {
			break
		}
	}
	if !foundOlderFailed {
		fmt.Fprintln(os.Stderr, "outbox pager never reached the older failed row")
		os.Exit(1)
	}
	retried, err := c.RetryOutboxDelivery(ctx, seededDelivery.GetId())
	if err != nil {
		fmt.Fprintln(os.Stderr, "RetryOutboxDelivery:", err)
		os.Exit(1)
	}
	if retried.GetDelivery() == nil || retried.GetDelivery().GetStatus() != "pending" {
		fmt.Fprintln(os.Stderr, "outbox retry did not re-arm row")
		os.Exit(1)
	}
	delivered, err := waitForOutboxDelivery(ctx, c, seededDelivery.GetId())
	if err != nil {
		fmt.Fprintln(os.Stderr, "waitForOutboxDelivery:", err)
		os.Exit(1)
	}
	if delivered.GetStatus() != "delivered" {
		fmt.Fprintln(os.Stderr, "outbox delivery did not complete after retry")
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv("OUTBOX_DELIVERY_ID"), []byte(fmt.Sprintf("%d\n", seededDelivery.GetId())), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write outbox delivery id:", err)
		os.Exit(1)
	}

	createdMCP, err := c.CreateMCPClient(ctx, &attune.CreateMCPClientRequest{
		Name:         "consumer-mcp-" + mark,
		RedirectUris: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "CreateMCPClient:", err)
		os.Exit(1)
	}
	clientID := createdMCP.GetClient().GetId()
	if clientID == "" {
		fmt.Fprintln(os.Stderr, "createMCPClient returned empty id")
		os.Exit(1)
	}
	mcpList, err := c.ListMCPClients(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListMCPClients:", err)
		os.Exit(1)
	}
	foundClient := false
	for _, item := range mcpList.GetClients() {
		if item.GetId() == clientID {
			foundClient = true
			break
		}
	}
	if !foundClient {
		fmt.Fprintln(os.Stderr, "listMCPClients missed created client")
		os.Exit(1)
	}
	mcpDetail, err := c.GetMCPClient(ctx, clientID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetMCPClient:", err)
		os.Exit(1)
	}
	if mcpDetail.GetConnection() == nil || mcpDetail.GetConnection().GetServerUrl() != os.Getenv("B")+"/mcp" {
		fmt.Fprintln(os.Stderr, "unexpected MCP server url")
		os.Exit(1)
	}
	if _, err := c.UpdateMCPClient(ctx, &attune.UpdateMCPClientRequest{
		Id:             clientID,
		ToolPolicyMode: ptr("allow_list"),
		RateLimitRpm:   ptr(int32(9)),
		RateLimitBurst: ptr(int32(3)),
	}); err != nil {
		fmt.Fprintln(os.Stderr, "UpdateMCPClient:", err)
		os.Exit(1)
	}
	tools, err := c.ReplaceMCPClientToolPolicies(ctx, &attune.ReplaceMCPClientToolPoliciesRequest{
		Id: clientID,
		Policies: []*attune.MCPToolPolicyInput{
			{ToolName: "list_feedback", Effect: "allow", RateLimitRpm: ptr(int32(7))},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ReplaceMCPClientToolPolicies:", err)
		os.Exit(1)
	}
	foundTool := false
	for _, tool := range tools.GetTools() {
		if tool.GetName() == "list_feedback" && tool.GetEffectiveAllowed() {
			foundTool = true
			break
		}
	}
	if !foundTool {
		fmt.Fprintln(os.Stderr, "mcp tool policy replacement did not take effect")
		os.Exit(1)
	}
	if _, err := c.RevokeMCPClient(ctx, clientID); err != nil {
		fmt.Fprintln(os.Stderr, "RevokeMCPClient:", err)
		os.Exit(1)
	}
	revokedClient, err := c.GetMCPClient(ctx, clientID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetMCPClient after revoke:", err)
		os.Exit(1)
	}
	if revokedClient.GetClient() == nil || revokedClient.GetClient().GetRevokedAt() == "" {
		fmt.Fprintln(os.Stderr, "mcp revoke did not persist revokedAt")
		os.Exit(1)
	}

	if _, err := c.ArchiveTag(ctx, olderTag.GetId()); err != nil {
		fmt.Fprintln(os.Stderr, "ArchiveTag older:", err)
		os.Exit(1)
	}
	if _, err := c.ArchiveTag(ctx, newerTag.GetId()); err != nil {
		fmt.Fprintln(os.Stderr, "ArchiveTag newer:", err)
		os.Exit(1)
	}

	if _, err := io.WriteString(os.Stdout, "  external consumer id="+res.ID+"\n"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := io.WriteString(os.Stdout, "  external management flow OK (audit="+evidence.GetJobId()+" gdpr="+gdprExport.GetJobId()+" mcp="+clientID+")\n"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
GO
( cd "$CONSUMER" && go mod tidy >/dev/null 2>&1 && B="$BASE_URL" K="$KEY" MARK="$MARKER" AUDIT_ZIP="$CONSUMER/audit-evidence.zip" GDPR_ZIP="$CONSUMER/gdpr-export.zip" OUTBOX_DELIVERY_ID="$CONSUMER/outbox-delivery-id.txt" go run . )

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
CONS="$(pg "select count(*) from user_feedback where content = '${MARKER} consumer';")"
echo "  rows from the external consumer: ${CONS}"
[ "$CONS" = "1" ] || { echo "expected 1 external-consumer row, got ${CONS}"; exit 1; }

log "E2E PASSED — Go SDK ↔ live server fully verified (incl. example CLI + external management consumer)"
