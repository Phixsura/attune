//go:build integration

package gdpr_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
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
	surveyIDs := insertSurveySubjectRows(t, ctx, pool, tenantID, feedbackID1, otherFeedbackID, subjectHash)

	bundle, err := svc.Export(ctx, tenantID, subjectKey, actor())
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	expectedCounts := gdprLifecycleCounts()
	assertCounts(t, bundle.Counts, expectedCounts)
	files := unzipFiles(t, bundle.Data)
	assertManifest(t, files["manifest.json"], tenantID, subjectKey, 2)
	assertLifecycleJSONLCounts(t, files)
	assertFeedbackRowsIncludeOnlySubject(t, files["feedback.jsonl"], subjectKey, []int64{feedbackID1, feedbackID2})
	assertTagRow(t, files["feedback_tags.jsonl"], feedbackID1, tagID)
	assertSurveyResponseComment(t, files["survey_responses.jsonl"], surveyIDs.responseID, "Survey comment for Alice")
	assertSurveyProviderEventKey(t, files["survey_provider_events.jsonl"], surveyIDs.providerEventID, "gdpr-provider-event-1")
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
	assertLifecycleRowsDeleted(t, ctx, pool, feedbackID1, feedbackID2, replyIDs, surveyIDs)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM survey_responses WHERE id = $1`, 1, surveyIDs.otherResponseID)
	assertAuditLogActionCount(t, ctx, pool, tenantID, "gdpr.delete", 1)
}

func gdprLifecycleCounts() gdprrepo.Counts {
	return gdprrepo.Counts{
		FeedbackCount:                   2,
		TagAssignmentCount:              1,
		FeedbackAuditCount:              1,
		LLMAuditCount:                   1,
		ReplyDraftCount:                 1,
		ReplyDraftRevisionCount:         1,
		ReplyDraftEventCount:            1,
		ReplyDeliveryAttemptCount:       1,
		SurveyInvitationCount:           1,
		SurveyResponseCount:             1,
		SurveyLowScoreReviewCount:       1,
		SurveyProviderEventCount:        1,
		SurveyRecoveryNotificationCount: 1,
	}
}

func assertCounts(t *testing.T, got gdprrepo.Counts, want gdprrepo.Counts) {
	t.Helper()
	if got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
}

func assertLifecycleJSONLCounts(t *testing.T, files map[string][]byte) {
	t.Helper()
	for name, want := range map[string]int{
		"feedback.jsonl":                      2,
		"feedback_tags.jsonl":                 1,
		"feedback_audit_log.jsonl":            1,
		"llm_audit.jsonl":                     1,
		"reply_drafts.jsonl":                  1,
		"reply_draft_revisions.jsonl":         1,
		"reply_draft_events.jsonl":            1,
		"reply_delivery_attempts.jsonl":       1,
		"survey_invitations.jsonl":            1,
		"survey_responses.jsonl":              1,
		"survey_low_score_reviews.jsonl":      1,
		"survey_provider_events.jsonl":        1,
		"survey_recovery_notifications.jsonl": 1,
	} {
		assertJSONLCount(t, files[name], want)
	}
}

func assertLifecycleRowsDeleted(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	feedbackID1 int64,
	feedbackID2 int64,
	replyIDs replyDraftWorkflowIDs,
	surveyIDs surveySubjectIDs,
) {
	t.Helper()
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM feedback_tag_assignments WHERE feedback_id = $1`, 0, feedbackID1)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM feedback_audit_log WHERE feedback_id = $1`, 0, feedbackID1)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM llm_audit WHERE feedback_id = $1`, 0, feedbackID2)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM reply_delivery_attempts WHERE id = $1`, 0, replyIDs.attemptID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM reply_draft_events WHERE id = $1`, 0, replyIDs.eventID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM reply_draft_revisions WHERE id = $1`, 0, replyIDs.revisionID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM reply_drafts WHERE id = $1`, 0, replyIDs.draftID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM survey_recovery_notifications WHERE id = $1`, 0, surveyIDs.recoveryNotificationID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM survey_low_score_reviews WHERE response_id = $1`, 0, surveyIDs.responseID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM survey_provider_events WHERE id = $1`, 0, surveyIDs.providerEventID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM survey_responses WHERE id = $1`, 0, surveyIDs.responseID)
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM survey_invitations WHERE id = $1`, 0, surveyIDs.invitationID)
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

// TestPG_GDPRDeleteAnonymizesCustomerRequestIdentity: customer-request
// links and votes carry the subject's email/name (via manual linking
// and promote-time auto-attribution) but have no FK to user_feedback —
// erasure must scrub them in place while keeping request aggregates.
func TestPG_GDPRDeleteAnonymizesCustomerRequestIdentity(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "gdpr-crlinks", "GDPR CR Links")
	svc := newImmediateService(pool)

	subjectKey := "carol@example.com"
	insertFeedback(t, ctx, pool, tenantID, "ext_api:carol@example.com", subjectKey, "Carol", "hash-carol", "Broken export")

	requestID := insertCustomerRequest(t, ctx, pool, tenantID, "Export failures")
	otherRequestID := insertCustomerRequest(t, ctx, pool, tenantID, "Unrelated request")
	// Subject's link + vote on one request; a second raw link on the
	// same request/account exercises the pre-scrub dedup collapse.
	insertCustomerLink(t, ctx, pool, tenantID, requestID, subjectKey, "Carol", "acme")
	insertCustomerLink(t, ctx, pool, tenantID, otherRequestID, subjectKey, "Carol", "acme")
	insertCustomerVote(t, ctx, pool, tenantID, requestID, subjectKey, "Carol")
	// Another subject's rows must be untouched.
	insertCustomerLink(t, ctx, pool, tenantID, requestID, "dave@example.com", "Dave", "globex")

	result, err := svc.Delete(ctx, tenantID, subjectKey, actor())
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if result.Counts.CustomerLinkCount != 2 {
		t.Errorf("CustomerLinkCount = %d, want 2", result.Counts.CustomerLinkCount)
	}
	if result.Counts.VoteCount != 1 {
		t.Errorf("VoteCount = %d, want 1", result.Counts.VoteCount)
	}

	// The subject's identity is gone from both tables...
	assertTableCount(t, ctx, pool,
		`SELECT COUNT(*) FROM customer_request_customer_links WHERE tenant_id = $1 AND subject_key = $2`,
		0, tenantID, subjectKey)
	assertTableCount(t, ctx, pool,
		`SELECT COUNT(*) FROM customer_request_votes WHERE tenant_id = $1 AND subject_key = $2`,
		0, tenantID, subjectKey)
	var display string
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(string_agg(subject_display, ','), '') FROM customer_request_customer_links
		 WHERE tenant_id = $1 AND subject_display <> '' AND subject_display <> 'Dave'`,
		tenantID).Scan(&display); err != nil {
		t.Fatalf("query displays: %v", err)
	}
	if display != "" {
		t.Errorf("subject_display not scrubbed: %q", display)
	}
	// ...but the aggregates survive: the request keeps its anonymized
	// customer rows and the vote row.
	assertTableCount(t, ctx, pool,
		`SELECT COUNT(*) FROM customer_request_customer_links WHERE tenant_id = $1 AND request_id = $2`,
		2, tenantID, requestID)
	assertTableCount(t, ctx, pool,
		`SELECT COUNT(*) FROM customer_request_votes WHERE tenant_id = $1 AND request_id = $2`,
		1, tenantID, requestID)
	// The untouched subject stays raw.
	assertTableCount(t, ctx, pool,
		`SELECT COUNT(*) FROM customer_request_customer_links WHERE tenant_id = $1 AND subject_key = 'dave@example.com'`,
		1, tenantID)

	// Idempotent: a second subject with the SAME anonymized shape on the
	// same request/account must collapse instead of violating the
	// unique constraint.
	insertFeedback(t, ctx, pool, tenantID, "ext_api:erin@example.com", "erin@example.com", "Erin", "hash-erin", "More exports")
	insertCustomerLink(t, ctx, pool, tenantID, requestID, "erin@example.com", "Erin", "acme")
	if _, err := svc.Delete(ctx, tenantID, "erin@example.com", actor()); err != nil {
		t.Fatalf("Delete second subject: %v", err)
	}
	assertTableCount(t, ctx, pool,
		`SELECT COUNT(*) FROM customer_request_customer_links WHERE tenant_id = $1 AND subject_key = 'erin@example.com'`,
		0, tenantID)
}

func insertCustomerRequest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, title string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		WITH next AS (
			SELECT COALESCE(MAX(display_number), 0) + 1 AS n
			FROM customer_requests WHERE tenant_id = $1
		)
		INSERT INTO customer_requests (tenant_id, display_number, display_id, title, created_by, updated_by)
		SELECT $1, n, 'CR-' || n, $2, 'admin-1', 'admin-1' FROM next
		RETURNING id`,
		tenantID, title,
	).Scan(&id); err != nil {
		t.Fatalf("insert customer request: %v", err)
	}
	return id
}

func insertCustomerLink(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, requestID, subjectKey, subjectDisplay, accountKey string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO customer_request_customer_links (tenant_id, request_id, subject_key, subject_display, account_key, created_by)
		VALUES ($1, $2, $3, $4, $5, 'admin-1')`,
		tenantID, requestID, subjectKey, subjectDisplay, accountKey,
	); err != nil {
		t.Fatalf("insert customer link: %v", err)
	}
}

func insertCustomerVote(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, requestID, subjectKey, subjectDisplay string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO customer_request_votes (tenant_id, request_id, subject_key, subject_display, created_by)
		VALUES ($1, $2, $3, $4, 'admin-1')`,
		tenantID, requestID, subjectKey, subjectDisplay,
	); err != nil {
		t.Fatalf("insert customer vote: %v", err)
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
	insertOutbox(t, ctx, pool, tenantID, feedbackID)

	result, err := svc.Delete(ctx, tenantID, subjectKey, actor())
	if err != nil {
		t.Fatalf("Delete schedule: %v", err)
	}
	if result.Status != gdprrepo.RequestStatusScheduled {
		t.Fatalf("Delete schedule status = %q", result.Status)
	}
	if result.Counts.OutboxCount != 1 {
		t.Fatalf("Delete schedule outbox count = %d, want 1", result.Counts.OutboxCount)
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

	requests, err := repo.ListRequests(ctx, gdprrepo.ListRequestFilter{TenantID: tenantID, RequestType: "delete"})
	if err != nil {
		t.Fatalf("ListRequests: %v", err)
	}
	if len(requests.Items) != 1 {
		t.Fatalf("expected exactly one delete request, got %d", len(requests.Items))
	}
	if requests.Items[0].Counts.OutboxCount != 1 {
		t.Fatalf("request history outbox count = %d, want 1", requests.Items[0].Counts.OutboxCount)
	}

	assertFeedbackDeleted(t, ctx, pool, []int64{feedbackID})
	assertTableCount(t, ctx, pool, `SELECT COUNT(*) FROM notify_outbox WHERE feedback_id = $1`, 0, feedbackID)
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

type surveySubjectIDs struct {
	invitationID           uuid.UUID
	responseID             uuid.UUID
	providerEventID        int64
	recoveryNotificationID uuid.UUID
	otherResponseID        uuid.UUID
}

func insertSurveySubjectRows(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	feedbackID int64,
	otherFeedbackID int64,
	subjectHash string,
) surveySubjectIDs {
	t.Helper()
	campaignID := insertSurveyCampaign(t, ctx, pool, tenantID)
	ids := surveySubjectIDs{
		invitationID:           uuid.New(),
		responseID:             uuid.New(),
		recoveryNotificationID: uuid.New(),
		otherResponseID:        uuid.New(),
	}

	insertSurveyInvitation(t, ctx, pool, tenantID, campaignID, surveyInvitationSeed{
		id:                ids.invitationID,
		dedupeKey:         "gdpr-target",
		sourceType:        "reply_sent",
		sourceID:          "delivery-attempt-1",
		tokenChar:         "a",
		recipientSnapshot: map[string]any{"feedback_id": feedbackID, "subject_hash": subjectHash},
	})
	if err := insertSurveyResponse(ctx, pool, tenantID, campaignID, ids.invitationID, ids.responseID, feedbackID, "Survey comment for Alice"); err != nil {
		t.Fatalf("insert survey response: %v", err)
	}
	insertSurveyLowScoreReview(t, ctx, pool, tenantID, campaignID, ids.responseID)
	ids.providerEventID = insertSurveyProviderEvent(t, ctx, pool, tenantID, ids.invitationID)
	insertSurveyRecoveryNotification(t, ctx, pool, tenantID, ids.responseID, ids.recoveryNotificationID)

	otherInvitationID := uuid.New()
	insertSurveyInvitation(t, ctx, pool, tenantID, campaignID, surveyInvitationSeed{
		id:                otherInvitationID,
		dedupeKey:         "gdpr-other",
		sourceType:        "workflow_transition",
		sourceID:          strconv.FormatInt(otherFeedbackID, 10),
		tokenChar:         "b",
		recipientSnapshot: map[string]any{"feedback_id": otherFeedbackID},
	})
	if err := insertSurveyResponse(ctx, pool, tenantID, campaignID, otherInvitationID, ids.otherResponseID, otherFeedbackID, "Other subject comment"); err != nil {
		t.Fatalf("insert other survey response: %v", err)
	}
	return ids
}

func insertSurveyCampaign(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) uuid.UUID {
	t.Helper()
	campaignID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO survey_campaigns (
			id, tenant_id, name, survey_type, status, trigger_event,
			distribution_mode, content, created_by, updated_by
		) VALUES (
			$1, $2, 'GDPR survey', 'csat', 'active', 'manual_link',
			'source_link', '{"title":"Resolution feedback"}'::jsonb,
			'admin-1', 'admin-1'
		)`,
		campaignID, tenantID,
	); err != nil {
		t.Fatalf("insert survey campaign: %v", err)
	}
	return campaignID
}

type surveyInvitationSeed struct {
	id                uuid.UUID
	dedupeKey         string
	sourceType        string
	sourceID          string
	tokenChar         string
	recipientSnapshot map[string]any
}

func insertSurveyInvitation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	campaignID uuid.UUID,
	seed surveyInvitationSeed,
) {
	t.Helper()
	snapshot, err := json.Marshal(seed.recipientSnapshot)
	if err != nil {
		t.Fatalf("marshal survey invitation snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO survey_invitations (
			id, tenant_id, campaign_id, campaign_content_version,
			campaign_snapshot, dedupe_key, source_type, source_id,
			distribution_mode, token_hash, delivery_status, response_status,
			suppression_status, recipient_snapshot, created_by
		) VALUES (
			$1, $2, $3, 1, '{"name":"GDPR survey"}'::jsonb,
			$4, $5, $6, 'source_link', repeat($7::text, 64), 'not_applicable',
			'completed', 'not_suppressed', $8::jsonb, 'admin-1'
		)`,
		seed.id, tenantID, campaignID, seed.dedupeKey, seed.sourceType,
		seed.sourceID, seed.tokenChar, string(snapshot),
	); err != nil {
		t.Fatalf("insert survey invitation: %v", err)
	}
}

func insertSurveyLowScoreReview(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	campaignID uuid.UUID,
	responseID uuid.UUID,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO survey_low_score_reviews (
			response_id, tenant_id, campaign_id, status, severity,
			root_cause, action_taken, customer_contacted, updated_by
		) VALUES (
			$1, $2, $3, 'open', 'high',
			'unclear_resolution', 'Followed up with Alice', true, 'admin-1'
		)`,
		responseID, tenantID, campaignID,
	); err != nil {
		t.Fatalf("insert survey low-score review: %v", err)
	}
}

func insertSurveyProviderEvent(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	invitationID uuid.UUID,
) int64 {
	t.Helper()
	var providerEventID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO survey_provider_events (
			tenant_id, invitation_id, provider, provider_event_type,
			provider_message_id, provider_event_key, payload
		) VALUES (
			$1, $2, 'gdpr-email', 'delivered', 'msg-1', 'gdpr-provider-event-1',
			'{"diagnostic":"delivered"}'::jsonb
		)
		RETURNING id`,
		tenantID, invitationID,
	).Scan(&providerEventID); err != nil {
		t.Fatalf("insert survey provider event: %v", err)
	}
	return providerEventID
}

func insertSurveyRecoveryNotification(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	responseID uuid.UUID,
	recoveryNotificationID uuid.UUID,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO survey_recovery_notifications (
			id, tenant_id, response_id, status, reason, destination_hash, payload
		) VALUES (
			$1, $2, $3, 'pending', 'overdue_sla', 'sha256:owner',
			'{"comment":"Survey comment for Alice"}'::jsonb
		)`,
		recoveryNotificationID, tenantID, responseID,
	); err != nil {
		t.Fatalf("insert survey recovery notification: %v", err)
	}
}

func insertSurveyResponse(
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	campaignID uuid.UUID,
	invitationID uuid.UUID,
	responseID uuid.UUID,
	feedbackID int64,
	comment string,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO survey_responses (
			id, tenant_id, campaign_id, invitation_id, source_type, source_id,
			score, comment, locale, metadata, user_agent_hash, ip_hash
			) VALUES (
				$1, $2, $3, $4, 'reply_sent', $5::bigint::text, 2, $6, 'en',
			'{"surface":"gdpr-test"}'::jsonb, 'sha256:ua', 'sha256:ip'
		)`,
		responseID, tenantID, campaignID, invitationID, feedbackID, comment,
	)
	return err
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

func assertSurveyResponseComment(t *testing.T, payload []byte, responseID uuid.UUID, comment string) {
	t.Helper()
	lines := splitJSONL(payload)
	if len(lines) != 1 {
		t.Fatalf("survey response rows = %d want 1", len(lines))
	}
	var row struct {
		ID      uuid.UUID `json:"id"`
		Comment string    `json:"comment"`
	}
	if err := json.Unmarshal(lines[0], &row); err != nil {
		t.Fatalf("unmarshal survey response row: %v", err)
	}
	if row.ID != responseID || row.Comment != comment {
		t.Fatalf("unexpected survey response row: %+v", row)
	}
}

func assertSurveyProviderEventKey(t *testing.T, payload []byte, eventID int64, eventKey string) {
	t.Helper()
	lines := splitJSONL(payload)
	if len(lines) != 1 {
		t.Fatalf("survey provider event rows = %d want 1", len(lines))
	}
	var row struct {
		ID               int64  `json:"id"`
		ProviderEventKey string `json:"provider_event_key"`
	}
	if err := json.Unmarshal(lines[0], &row); err != nil {
		t.Fatalf("unmarshal survey provider event row: %v", err)
	}
	if row.ID != eventID || row.ProviderEventKey != eventKey {
		t.Fatalf("unexpected survey provider event row: %+v", row)
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
