//go:build integration

package gdpr_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	"github.com/Phixsura/attune/internal/repo/feedbackaudit"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	llmauditrepo "github.com/Phixsura/attune/internal/repo/llmaudit"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/auditlog"
	gdprsvc "github.com/Phixsura/attune/internal/service/gdpr"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_GDPRExportDeleteLifecycle(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "gdpr-io", "GDPR IO")
	svc := newImmediateService(pool)

	subjectKey := "alice@example.com"
	subjectHash := "hash-alice"
	feedbackID1 := insertFeedback(t, ctx, pool, tenantID, "ext_api:alice@example.com", subjectKey, subjectKey, subjectHash, "Payment failed")
	feedbackID2 := insertFeedback(t, ctx, pool, tenantID, "legacy-alice", subjectKey, "Alice", subjectHash, "App crashes")
	otherFeedbackID := insertFeedback(t, ctx, pool, tenantID, "ext_api:bob@example.com", "bob@example.com", "Bob", "hash-bob", "Other subject stays")
	tagID := createTagAndAssign(t, ctx, pool, tenantID, feedbackID1)
	writeFeedbackAudit(t, ctx, pool, tenantID, feedbackID1)
	writeLLMAudit(t, ctx, pool, tenantID, feedbackID2)
	replyIDs := insertReplyDraftWorkflow(t, ctx, pool, tenantID, feedbackID1)

	bundle, err := svc.Export(ctx, tenantID, subjectKey, actor())
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if bundle.Counts.FeedbackCount != 2 || bundle.Counts.TagAssignmentCount != 1 ||
		bundle.Counts.FeedbackAuditCount != 1 || bundle.Counts.LLMAuditCount != 1 ||
		bundle.Counts.ReplyDraftCount != 1 || bundle.Counts.ReplyDraftRevisionCount != 1 ||
		bundle.Counts.ReplyDraftEventCount != 1 || bundle.Counts.ReplyDeliveryAttemptCount != 1 {
		t.Fatalf("unexpected export counts: %+v", bundle.Counts)
	}
	files := unzipFiles(t, bundle.Data)
	assertManifest(t, files["manifest.json"], tenantID, subjectKey, 2)
	assertJSONLCount(t, files["feedback.jsonl"], 2)
	assertJSONLCount(t, files["feedback_tags.jsonl"], 1)
	assertJSONLCount(t, files["feedback_audit_log.jsonl"], 1)
	assertJSONLCount(t, files["llm_audit.jsonl"], 1)
	assertJSONLCount(t, files["reply_drafts.jsonl"], 1)
	assertJSONLCount(t, files["reply_draft_revisions.jsonl"], 1)
	assertJSONLCount(t, files["reply_draft_events.jsonl"], 1)
	assertJSONLCount(t, files["reply_delivery_attempts.jsonl"], 1)
	assertFeedbackRowsIncludeOnlySubject(t, files["feedback.jsonl"], subjectKey, []int64{feedbackID1, feedbackID2})
	assertTagRow(t, files["feedback_tags.jsonl"], feedbackID1, tagID)
	assertAuditLogActionCount(t, ctx, pool, tenantID, "gdpr.export", 1)

	result, err := svc.Delete(ctx, tenantID, subjectKey, actor())
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if result.Counts != bundle.Counts {
		t.Fatalf("delete counts %+v do not match export counts %+v", result.Counts, bundle.Counts)
	}
	assertFeedbackDeleted(t, ctx, pool, []int64{feedbackID1, feedbackID2})
	assertFeedbackPresent(t, ctx, pool, otherFeedbackID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM feedback_tag_assignments WHERE feedback_id = $1`, 0, feedbackID1)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM feedback_audit_log WHERE feedback_id = $1`, 0, feedbackID1)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM llm_audit WHERE feedback_id = $1`, 0, feedbackID2)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM reply_delivery_attempts WHERE id = $1`, 0, replyIDs.attemptID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM reply_draft_events WHERE id = $1`, 0, replyIDs.eventID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM reply_draft_revisions WHERE id = $1`, 0, replyIDs.revisionID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM reply_drafts WHERE id = $1`, 0, replyIDs.draftID)
	assertAuditLogActionCount(t, ctx, pool, tenantID, "gdpr.delete", 1)
}

func TestPG_GDPRExportDeleteSupportsLegacyUnbackfilledRows(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "gdpr-legacy", "GDPR Legacy")
	svc := newImmediateService(pool)

	legacyFeedbackID := insertFeedback(t, ctx, pool, tenantID, "ext_api:legacy@example.com", "", "", "", "Legacy row")
	writeLLMAudit(t, ctx, pool, tenantID, legacyFeedbackID)
	keyOnlyFeedbackID := insertFeedback(t, ctx, pool, tenantID, "ext_00000000-0000-0000-0000-000000000123", "", "", "", "Legacy row without source user")

	bundle, err := svc.Export(ctx, tenantID, "legacy@example.com", actor())
	if err != nil {
		t.Fatalf("Export legacy row: %v", err)
	}
	if bundle.Counts.FeedbackCount != 1 || bundle.Counts.LLMAuditCount != 1 {
		t.Fatalf("unexpected legacy export counts: %+v", bundle.Counts)
	}
	files := unzipFiles(t, bundle.Data)
	assertJSONLCount(t, files["feedback.jsonl"], 1)
	assertFeedbackRowsIncludeOnlySubject(t, files["feedback.jsonl"], "legacy@example.com", []int64{legacyFeedbackID})

	if _, err := svc.Delete(ctx, tenantID, "legacy@example.com", actor()); err != nil {
		t.Fatalf("Delete legacy row: %v", err)
	}
	assertFeedbackDeleted(t, ctx, pool, []int64{legacyFeedbackID})

	keyOnlySubjectKey := "ext_00000000-0000-0000-0000-000000000123"
	keyOnlyBundle, err := svc.Export(ctx, tenantID, keyOnlySubjectKey, actor())
	if err != nil {
		t.Fatalf("Export legacy key-only row: %v", err)
	}
	if keyOnlyBundle.Counts.FeedbackCount != 1 {
		t.Fatalf("unexpected legacy key-only export counts: %+v", keyOnlyBundle.Counts)
	}
	keyOnlyFiles := unzipFiles(t, keyOnlyBundle.Data)
	assertJSONLCount(t, keyOnlyFiles["feedback.jsonl"], 1)
	assertFeedbackRowsIncludeOnlySubject(t, keyOnlyFiles["feedback.jsonl"], keyOnlySubjectKey, []int64{keyOnlyFeedbackID})

	if _, err := svc.Delete(ctx, tenantID, keyOnlySubjectKey, actor()); err != nil {
		t.Fatalf("Delete legacy key-only row: %v", err)
	}
	assertFeedbackDeleted(t, ctx, pool, []int64{keyOnlyFeedbackID})
}

func TestPG_GDPRDeleteSubjectIsolationAndNotFound(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "gdpr-scope", "GDPR Scope")
	svc := newImmediateService(pool)

	targetID := insertFeedback(t, ctx, pool, tenantID, "ext_api:scope@example.com", "scope@example.com", "scope@example.com", "hash-scope", "Target")
	otherID := insertFeedback(t, ctx, pool, tenantID, "ext_api:other@example.com", "other@example.com", "other@example.com", "hash-other", "Other")

	if _, err := svc.Delete(ctx, tenantID, "scope@example.com", actor()); err != nil {
		t.Fatalf("Delete target subject: %v", err)
	}
	assertFeedbackDeleted(t, ctx, pool, []int64{targetID})
	assertFeedbackPresent(t, ctx, pool, otherID)

	_, err := svc.Delete(ctx, tenantID, "missing@example.com", actor())
	if !errors.Is(err, gdprsvc.ErrSubjectNotFound) {
		t.Fatalf("Delete missing err = %v, want %v", err, gdprsvc.ErrSubjectNotFound)
	}
}

func TestPG_GDPRDeleteRequestLifecycle(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "gdpr-scheduled", "GDPR Scheduled")
	svc := newService(pool)
	repo := gdprrepo.New(pool)

	subjectKey := "scheduled@example.com"
	subjectHash := "hash-scheduled"
	feedbackID := insertFeedback(t, ctx, pool, tenantID, "ext_api:scheduled@example.com", subjectKey, subjectKey, subjectHash, "Needs deletion")
	writeLLMAudit(t, ctx, pool, tenantID, feedbackID)

	result, err := svc.Delete(ctx, tenantID, subjectKey, actor())
	if err != nil {
		t.Fatalf("Delete schedule: %v", err)
	}
	if result.Status != gdprrepo.RequestStatusScheduled {
		t.Fatalf("Delete schedule status = %q", result.Status)
	}
	if result.ExecuteAfter == nil {
		t.Fatal("expected execute_after to be set")
	}
	assertFeedbackPresent(t, ctx, pool, feedbackID)
	assertAuditLogActionCount(t, ctx, pool, tenantID, "gdpr.delete.requested", 1)

	req, err := repo.ClaimNextDeleteRequest(ctx, result.ExecuteAfter.Add(time.Second))
	if err != nil {
		t.Fatalf("ClaimNextDeleteRequest: %v", err)
	}
	if req == nil || req.ID != result.RequestID {
		t.Fatalf("unexpected claimed request: %#v", req)
	}
	execResult, err := repo.ExecuteDeleteRequest(ctx, req.ID)
	if err != nil {
		t.Fatalf("ExecuteDeleteRequest: %v", err)
	}
	if err := repo.CompleteDeleteRequest(ctx, req.ID, execResult.Counts); err != nil {
		t.Fatalf("CompleteDeleteRequest: %v", err)
	}
	if err := svc.RecordDeleteCompletion(ctx, tenantID, subjectKey, actor(), execResult); err != nil {
		t.Fatalf("RecordDeleteCompletion: %v", err)
	}

	assertFeedbackDeleted(t, ctx, pool, []int64{feedbackID})
	assertAuditLogActionCount(t, ctx, pool, tenantID, "gdpr.delete", 1)
}

func TestPG_GDPRExportRevokeLifecycle(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "gdpr-export-revoke", "GDPR Export Revoke")
	svc := newService(pool)
	repo := gdprrepo.New(pool)

	subjectKey := "revoke@example.com"
	insertFeedback(t, ctx, pool, tenantID, "ext_api:revoke@example.com", subjectKey, subjectKey, "hash-revoke", "Needs export revoke")

	started, err := svc.StartExport(ctx, tenantID, subjectKey, actor())
	if err != nil {
		t.Fatalf("StartExport: %v", err)
	}
	job, err := repo.ClaimNextExportJob(ctx)
	if err != nil {
		t.Fatalf("ClaimNextExportJob: %v", err)
	}
	if job == nil || job.ID != started.GetJobId() {
		t.Fatalf("unexpected claimed export job: %#v", job)
	}
	data, err := repo.Export(ctx, tenantID, subjectKey)
	if err != nil {
		t.Fatalf("Export data: %v", err)
	}
	expiresAt := time.Now().UTC().Add(time.Hour)
	if err := repo.CompleteExportJob(ctx, job.ID, data.SubjectDisplay, "gdpr-export.zip", []byte("zip"), data.Counts, expiresAt); err != nil {
		t.Fatalf("CompleteExportJob: %v", err)
	}
	if _, err := repo.MarkExportJobDownloaded(ctx, tenantID, job.ID); err != nil {
		t.Fatalf("MarkExportJobDownloaded: %v", err)
	}

	resp, err := svc.RevokeExport(ctx, tenantID, job.ID, actor())
	if err != nil {
		t.Fatalf("RevokeExport: %v", err)
	}
	if resp.GetStatus() != attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_REVOKED {
		t.Fatalf("unexpected export revoke status: %v", resp.GetStatus())
	}
	if _, err := repo.MarkExportJobDownloaded(ctx, tenantID, job.ID); !errors.Is(err, gdprrepo.ErrExportJobNotDownloadable) {
		t.Fatalf("download after revoke err = %v, want %v", err, gdprrepo.ErrExportJobNotDownloadable)
	}
	requests, err := repo.ListRequests(ctx, gdprrepo.ListRequestFilter{TenantID: tenantID, RequestType: "export"})
	if err != nil {
		t.Fatalf("ListRequests: %v", err)
	}
	if len(requests.Items) != 1 {
		t.Fatalf("expected exactly one export request, got %d", len(requests.Items))
	}
	if requests.Items[0].Status != gdprrepo.RequestStatusRevoked {
		t.Fatalf("request status = %q", requests.Items[0].Status)
	}
	if requests.Items[0].RevokedAt == nil {
		t.Fatal("expected revoked_at to be populated")
	}
	assertAuditLogActionCount(t, ctx, pool, tenantID, "gdpr.export.revoked", 1)
}

func newService(pool *pgxpool.Pool) *gdprsvc.Service {
	return gdprsvc.New(gdprrepo.New(pool), auditlog.New(auditlogrepo.New(pool)))
}

type immediateRepo struct {
	base *gdprrepo.Repo
}

func (r *immediateRepo) Export(ctx context.Context, tenantID, subjectKey string) (*gdprrepo.ExportData, error) {
	return r.base.Export(ctx, tenantID, subjectKey)
}

func (r *immediateRepo) Delete(ctx context.Context, tenantID, subjectKey string) (*gdprrepo.DeleteResult, error) {
	return r.base.Delete(ctx, tenantID, subjectKey)
}

func newImmediateService(pool *pgxpool.Pool) *gdprsvc.Service {
	return gdprsvc.New(&immediateRepo{base: gdprrepo.New(pool)}, auditlog.New(auditlogrepo.New(pool)))
}

func actor() auditlog.Actor {
	return auditlog.Actor{
		Type:  "admin",
		ID:    "admin-1",
		Email: "admin@example.com",
		IP:    "127.0.0.1",
	}
}

func createTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug, name string) string {
	t.Helper()
	tenantID, err := tenant.NewTenant(pool).Create(ctx, slug, name)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return tenantID
}

func insertFeedback(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID, userID, subjectKey, subjectDisplay, subjectHash, content string,
) int64 {
	t.Helper()
	var feedbackID int64
	err := pool.QueryRow(
		ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, subject_key, subject_display, subject_hash, source, content)
		VALUES ($1, $2, $3, $4, $5, 'api', $6)
		RETURNING id`,
		tenantID, userID, subjectKey, subjectDisplay, subjectHash, content,
	).Scan(&feedbackID)
	if err != nil {
		t.Fatalf("insert feedback: %v", err)
	}
	return feedbackID
}

func createTagAndAssign(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, feedbackID int64) uuid.UUID {
	t.Helper()
	tagRepo := feedbacktag.New(pool)
	tag, err := tagRepo.Create(ctx, feedbacktag.Tag{
		TenantID:  tenantID,
		Name:      "vip",
		Color:     "#2563eb",
		CreatedBy: "admin-1",
	})
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	added, err := feedbacktagassignment.New(pool).Add(ctx, tenantID, feedbackID, tag.ID, "admin-1")
	if err != nil {
		t.Fatalf("assign tag: %v", err)
	}
	if !added {
		t.Fatal("expected tag assignment to be added")
	}
	return tag.ID
}

func writeFeedbackAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, feedbackID int64) {
	t.Helper()
	oldValue := "pending"
	newValue := "done"
	err := feedbackaudit.New(pool).Write(ctx, feedbackaudit.Entry{
		TenantID:   tenantID,
		FeedbackID: feedbackID,
		EntityType: "feedback",
		FieldName:  "workflow_state",
		OldValue:   &oldValue,
		NewValue:   &newValue,
		Comment:    "completed",
		ChangedBy:  "admin-1",
	})
	if err != nil {
		t.Fatalf("write feedback audit: %v", err)
	}
}

func writeLLMAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, feedbackID int64) {
	t.Helper()
	err := llmauditrepo.New(pool).Insert(ctx, llmauditrepo.Row{
		TenantID:         tenantID,
		FeedbackID:       feedbackID,
		InboundTraceID:   "trace-" + uuid.NewString(),
		OtelTraceID:      "otel-" + uuid.NewString(),
		ModelID:          "gpt-4o-mini",
		Purpose:          "enrich",
		PromptTokens:     42,
		CompletionTokens: 7,
		CostUSD:          0.001,
		Status:           "ok",
		LatencyMS:        11,
	})
	if err != nil {
		t.Fatalf("write llm audit: %v", err)
	}
}

type replyDraftWorkflowIDs struct {
	draftID    uuid.UUID
	revisionID uuid.UUID
	eventID    uuid.UUID
	attemptID  uuid.UUID
}

func insertReplyDraftWorkflow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	feedbackID int64,
) replyDraftWorkflowIDs {
	t.Helper()
	hookID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO reply_send_hooks (
			id, tenant_id, name, url_ciphertext, url_key_id, url_fingerprint,
			url_host, secret_ciphertext, secret_key_id, created_by, updated_by
		) VALUES ($1, $2, 'GDPR reply hook', $3, 'test-key', 'sha256:gdpr-hook',
			'hooks.example.com', $4, 'test-key', 'admin-1', 'admin-1')`,
		hookID, tenantID, []byte("https://hooks.example.com/reply"), []byte("secret"),
	); err != nil {
		t.Fatalf("insert reply send hook: %v", err)
	}

	ids := replyDraftWorkflowIDs{
		draftID:    uuid.New(),
		revisionID: uuid.New(),
		eventID:    uuid.New(),
		attemptID:  uuid.New(),
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO reply_drafts (
			id, tenant_id, feedback_id, cycle_no, status, active_revision_id,
			approved_revision_id, approved_hook_id, approved_hook_fingerprint,
			source_fingerprint, source_meta, generated_at, generated_by,
			approved_at, approved_by, revision
		) VALUES ($1, $2, $3, 1, 'approved', $4, $4, $5, 'sha256:gdpr-hook',
			'source-fingerprint', '{"source":"gdpr-test"}'::jsonb,
			NOW(), 'system', NOW(), 'admin-1', 3)`,
		ids.draftID, tenantID, feedbackID, ids.revisionID, hookID,
	); err != nil {
		t.Fatalf("insert reply draft: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO reply_draft_revisions (
			id, draft_id, tenant_id, feedback_id, cycle_no, revision_no,
			origin, content, content_sha256, source_fingerprint, created_by
		) VALUES ($1, $2, $3, $4, 1, 1, 'human', 'Reviewed subject reply',
			decode('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'hex'),
			'source-fingerprint', 'admin-1')`,
		ids.revisionID, ids.draftID, tenantID, feedbackID,
	); err != nil {
		t.Fatalf("insert reply draft revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO reply_draft_events (
			id, draft_id, tenant_id, feedback_id, revision_id, hook_id,
			event_type, actor_type, actor_id, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, 'approve', 'admin', 'admin-1',
			'{"reason":"gdpr integration"}'::jsonb)`,
		ids.eventID, ids.draftID, tenantID, feedbackID, ids.revisionID, hookID,
	); err != nil {
		t.Fatalf("insert reply draft event: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO reply_delivery_attempts (
			id, tenant_id, draft_id, feedback_id, hook_id, revision_id,
			event_type, idempotency_key, status, http_status, attempts,
			request_fingerprint, response_meta, requested_by_type, requested_by,
			completed_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'reply.send', 'reply_send_gdpr_1',
			'accepted', 202, 1, 'delivery-fingerprint', '{"accepted":true}'::jsonb,
			'admin', 'admin-1', NOW())`,
		ids.attemptID, tenantID, ids.draftID, feedbackID, hookID, ids.revisionID,
	); err != nil {
		t.Fatalf("insert reply delivery attempt: %v", err)
	}
	return ids
}

func unzipFiles(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", f.Name, err)
		}
		payload := new(bytes.Buffer)
		if _, err := payload.ReadFrom(rc); err != nil {
			_ = rc.Close()
			t.Fatalf("read zip entry %s: %v", f.Name, err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("close zip entry %s: %v", f.Name, err)
		}
		files[f.Name] = payload.Bytes()
	}
	return files
}

func assertManifest(t *testing.T, payload []byte, tenantID, subjectKey string, feedbackCount int) {
	t.Helper()
	var manifest struct {
		TenantID      string `json:"tenant_id"`
		SubjectKey    string `json:"subject_key"`
		SchemaVersion string `json:"schema_version"`
		Counts        struct {
			Feedback int `json:"feedback"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(payload, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if manifest.TenantID != tenantID || manifest.SubjectKey != subjectKey || manifest.SchemaVersion != "gdpr-export-v1" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.Counts.Feedback != feedbackCount {
		t.Fatalf("manifest feedback count = %d want %d", manifest.Counts.Feedback, feedbackCount)
	}
}

func assertJSONLCount(t *testing.T, payload []byte, want int) {
	t.Helper()
	lines := splitJSONL(payload)
	if len(lines) != want {
		t.Fatalf("jsonl rows = %d want %d payload=%s", len(lines), want, string(payload))
	}
}

func assertFeedbackRowsIncludeOnlySubject(t *testing.T, payload []byte, subjectKey string, wantIDs []int64) {
	t.Helper()
	lines := splitJSONL(payload)
	gotIDs := make(map[int64]struct{}, len(lines))
	for _, line := range lines {
		var row struct {
			ID         int64  `json:"id"`
			SubjectKey string `json:"subject_key"`
			UserID     string `json:"user_id"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("unmarshal feedback row: %v", err)
		}
		gotIDs[row.ID] = struct{}{}
		if row.SubjectKey != "" && row.SubjectKey != subjectKey {
			t.Fatalf("unexpected subject_key %q in row %+v", row.SubjectKey, row)
		}
	}
	for _, wantID := range wantIDs {
		if _, ok := gotIDs[wantID]; !ok {
			t.Fatalf("missing feedback id %d in export rows", wantID)
		}
	}
}

func assertTagRow(t *testing.T, payload []byte, feedbackID int64, tagID uuid.UUID) {
	t.Helper()
	lines := splitJSONL(payload)
	if len(lines) != 1 {
		t.Fatalf("tag rows = %d want 1", len(lines))
	}
	var row struct {
		FeedbackID int64     `json:"feedback_id"`
		TagID      uuid.UUID `json:"tag_id"`
	}
	if err := json.Unmarshal(lines[0], &row); err != nil {
		t.Fatalf("unmarshal tag row: %v", err)
	}
	if row.FeedbackID != feedbackID || row.TagID != tagID {
		t.Fatalf("unexpected tag row: %+v", row)
	}
}

func splitJSONL(payload []byte) [][]byte {
	lines := bytes.Split(bytes.TrimSpace(payload), []byte{'\n'})
	if len(lines) == 1 && len(lines[0]) == 0 {
		return nil
	}
	return lines
}

func assertAuditLogActionCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, action string, want int) {
	t.Helper()
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`, want, tenantID, action)
}

func assertFeedbackDeleted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, feedbackIDs []int64) {
	t.Helper()
	for _, feedbackID := range feedbackIDs {
		assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM user_feedback WHERE id = $1`, 0, feedbackID)
	}
}

func assertFeedbackPresent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, feedbackID int64) {
	t.Helper()
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM user_feedback WHERE id = $1`, 1, feedbackID)
}

func assertTableCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, query, args...).Scan(&got); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if got != want {
		t.Fatalf("count for %q = %d want %d", query, got, want)
	}
}
