// SPDX-License-Identifier: Apache-2.0

//go:build integration

package auditevidence_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	aerepo "github.com/Phixsura/attune/internal/repo/auditevidence"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/auditevidence"
	"github.com/Phixsura/attune/internal/service/auditlog"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_EvidenceExportFullLifecycle(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "evidence-lifecycle", "Evidence Lifecycle")

	seedAuditEntries(t, ctx, pool, tenantID, 5)

	repo := aerepo.New(pool)
	auditSvc := auditlog.New(auditlogrepo.New(pool))
	svc := auditevidence.New(repo, auditSvc, auditSvc)

	result := startAndAssertQueued(t, ctx, svc, tenantID)
	assertAuditLogActionCount(t, ctx, pool, tenantID, "audit_evidence.create", 1)

	assertJobStatus(t, ctx, svc, tenantID, result.JobID, aerepo.JobQueued)

	owner := "worker-test-1"
	claimed := claimAndAssert(t, ctx, repo, owner, result.JobID)

	n, err := repo.HeartbeatJob(ctx, claimed.ID, owner)
	if err != nil {
		t.Fatalf("HeartbeatJob: %v", err)
	}
	if n != 1 {
		t.Fatalf("heartbeat affected %d rows, want 1", n)
	}

	archiveData := []byte("fake-archive-data")
	completeJob(t, ctx, repo, claimed.ID, owner, archiveData, 5)
	assertJobStatus(t, ctx, svc, tenantID, result.JobID, aerepo.JobCompleted)

	job, err := svc.GetJob(ctx, tenantID, result.JobID)
	if err != nil {
		t.Fatalf("GetJob after complete: %v", err)
	}
	if job.TotalEvents != 5 {
		t.Fatalf("total_events = %d, want 5", job.TotalEvents)
	}

	bundle, err := svc.Download(ctx, tenantID, result.JobID, auditlog.Actor{
		Type: "admin", ID: "admin-1",
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !bytes.Equal(bundle.Data, archiveData) {
		t.Fatalf("download data mismatch")
	}
	assertAuditLogActionCount(t, ctx, pool, tenantID, "audit_evidence.download", 1)

	job, err = svc.GetJob(ctx, tenantID, result.JobID)
	if err != nil {
		t.Fatalf("GetJob after download: %v", err)
	}
	if job.DownloadedAt == nil {
		t.Fatal("expected downloaded_at to be set")
	}
}

func startAndAssertQueued(t *testing.T, ctx context.Context, svc *auditevidence.Service, tenantID string) *auditevidence.StartExportResult {
	t.Helper()
	result, err := svc.StartExport(ctx, tenantID, "admin-1", auditevidence.ExportFilter{})
	if err != nil {
		t.Fatalf("StartExport: %v", err)
	}
	if result.Status != "queued" {
		t.Fatalf("expected queued, got %s", result.Status)
	}
	return result
}

func claimAndAssert(t *testing.T, ctx context.Context, repo *aerepo.Repo, owner, wantID string) *aerepo.ExportJob {
	t.Helper()
	claimed, err := repo.ClaimNextJob(ctx, owner)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if claimed == nil || claimed.ID != wantID {
		t.Fatalf("unexpected claimed job: %+v", claimed)
	}
	if claimed.Status != aerepo.JobRunning {
		t.Fatalf("claimed status = %s, want running", claimed.Status)
	}
	return claimed
}

func completeJob(t *testing.T, ctx context.Context, repo *aerepo.Repo, jobID, owner string, data []byte, events int) {
	t.Helper()
	expiresAt := time.Now().UTC().Add(time.Hour)
	n, err := repo.CompleteJob(ctx, jobID, owner, "evidence.zip", data, events, expiresAt)
	if err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}
	if n != 1 {
		t.Fatalf("complete affected %d rows, want 1", n)
	}
}

func assertJobStatus(t *testing.T, ctx context.Context, svc *auditevidence.Service, tenantID, jobID string, want aerepo.JobStatus) {
	t.Helper()
	job, err := svc.GetJob(ctx, tenantID, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.Status != want {
		t.Fatalf("job status = %s, want %s", job.Status, want)
	}
}

func TestPG_EvidenceExportExpiry(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "evidence-expiry", "Evidence Expiry")

	repo := aerepo.New(pool)
	auditSvc := auditlog.New(auditlogrepo.New(pool))
	svc := auditevidence.New(repo, auditSvc, auditSvc)

	result, err := svc.StartExport(ctx, tenantID, "admin-1", auditevidence.ExportFilter{})
	if err != nil {
		t.Fatalf("StartExport: %v", err)
	}

	owner := "worker-test-2"
	claimed, err := repo.ClaimNextJob(ctx, owner)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}

	pastExpiry := time.Now().UTC().Add(-time.Hour)
	if _, err := repo.CompleteJob(ctx, claimed.ID, owner, "e.zip", []byte("data"), 0, pastExpiry); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}

	expired, err := repo.ExpireJobs(ctx, time.Now())
	if err != nil {
		t.Fatalf("ExpireJobs: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired %d jobs, want 1", expired)
	}

	job, err := svc.GetJob(ctx, tenantID, result.JobID)
	if err != nil {
		t.Fatalf("GetJob after expire: %v", err)
	}
	if job.Status != aerepo.JobExpired {
		t.Fatalf("expected expired, got %s", job.Status)
	}
	if job.Archive != nil {
		t.Fatal("expected archive to be cleared on expiry")
	}

	_, err = svc.Download(ctx, tenantID, result.JobID, auditlog.Actor{Type: "admin", ID: "admin-1"})
	if !errors.Is(err, auditevidence.ErrJobNotDownloadable) {
		t.Fatalf("download expired: got %v, want ErrJobNotDownloadable", err)
	}
}

func TestPG_EvidenceExportFailAndRequeueStale(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "evidence-fail", "Evidence Fail")

	repo := aerepo.New(pool)
	auditSvc := auditlog.New(auditlogrepo.New(pool))
	svc := auditevidence.New(repo, auditSvc, auditSvc)

	result, err := svc.StartExport(ctx, tenantID, "admin-1", auditevidence.ExportFilter{})
	if err != nil {
		t.Fatalf("StartExport: %v", err)
	}

	owner := "worker-test-3"
	claimed, err := repo.ClaimNextJob(ctx, owner)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}

	if _, err := repo.FailJob(ctx, claimed.ID, owner, "test error"); err != nil {
		t.Fatalf("FailJob: %v", err)
	}

	job, err := svc.GetJob(ctx, tenantID, result.JobID)
	if err != nil {
		t.Fatalf("GetJob after fail: %v", err)
	}
	if job.Status != aerepo.JobFailed {
		t.Fatalf("expected failed, got %s", job.Status)
	}
	if job.Error != "test error" {
		t.Fatalf("error = %q, want %q", job.Error, "test error")
	}
}

func TestPG_EvidenceExportRequeueStaleJobs(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "evidence-stale", "Evidence Stale")

	repo := aerepo.New(pool)
	auditSvc := auditlog.New(auditlogrepo.New(pool))
	svc := auditevidence.New(repo, auditSvc, auditSvc)

	if _, err := svc.StartExport(ctx, tenantID, "admin-1", auditevidence.ExportFilter{}); err != nil {
		t.Fatalf("StartExport: %v", err)
	}

	owner := "worker-stale"
	claimed, err := repo.ClaimNextJob(ctx, owner)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}

	_, err = pool.Exec(ctx, `
		UPDATE audit_evidence_export
		SET last_heartbeat = NOW() - INTERVAL '10 minutes'
		WHERE id = $1`, claimed.ID)
	if err != nil {
		t.Fatalf("backdate heartbeat: %v", err)
	}

	requeued, err := repo.RequeueStaleJobs(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RequeueStaleJobs: %v", err)
	}
	if requeued != 1 {
		t.Fatalf("requeued %d, want 1", requeued)
	}

	newOwner := "worker-reclaim"
	reclaimed, err := repo.ClaimNextJob(ctx, newOwner)
	if err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	if reclaimed == nil || reclaimed.ID != claimed.ID {
		t.Fatalf("expected re-claimed job %s, got %+v", claimed.ID, reclaimed)
	}
	if reclaimed.ClaimedBy != newOwner {
		t.Fatalf("claimed_by = %q, want %q", reclaimed.ClaimedBy, newOwner)
	}
}

func TestPG_EvidenceExportFencingPreventsCompleteByStalledWorker(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "evidence-fence", "Evidence Fence")

	repo := aerepo.New(pool)
	auditSvc := auditlog.New(auditlogrepo.New(pool))
	svc := auditevidence.New(repo, auditSvc, auditSvc)

	if _, err := svc.StartExport(ctx, tenantID, "admin-1", auditevidence.ExportFilter{}); err != nil {
		t.Fatalf("StartExport: %v", err)
	}

	oldOwner := "worker-old"
	claimed, err := repo.ClaimNextJob(ctx, oldOwner)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}

	_, err = pool.Exec(ctx, `
		UPDATE audit_evidence_export
		SET last_heartbeat = NOW() - INTERVAL '10 minutes'
		WHERE id = $1`, claimed.ID)
	if err != nil {
		t.Fatalf("backdate heartbeat: %v", err)
	}
	if _, err := repo.RequeueStaleJobs(ctx, 5*time.Minute); err != nil {
		t.Fatalf("requeue: %v", err)
	}

	newOwner := "worker-new"
	reclaimed, err := repo.ClaimNextJob(ctx, newOwner)
	if err != nil || reclaimed == nil {
		t.Fatalf("re-claim: %v", err)
	}

	n, err := repo.CompleteJob(ctx, claimed.ID, oldOwner, "stale.zip", []byte("stale"), 0, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("old worker CompleteJob: %v", err)
	}
	if n != 0 {
		t.Fatal("old worker should not be able to complete re-claimed job")
	}

	n, err = repo.CompleteJob(ctx, reclaimed.ID, newOwner, "fresh.zip", []byte("fresh"), 3, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("new worker CompleteJob: %v", err)
	}
	if n != 1 {
		t.Fatal("new worker should complete the job")
	}
}

func TestPG_EvidenceExportWorkerProducesVerifiableArchive(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := createTenant(t, ctx, pool, "evidence-archive", "Evidence Archive")

	seedAuditEntries(t, ctx, pool, tenantID, 3)

	repo := aerepo.New(pool)
	auditlogRepo := auditlogrepo.New(pool)
	auditSvc := auditlog.New(auditlogRepo)

	svc := auditevidence.New(repo, auditSvc, auditSvc)
	worker := auditevidence.NewWorker(repo, auditSvc, auditSvc)

	if _, err := svc.StartExport(ctx, tenantID, "admin-1", auditevidence.ExportFilter{}); err != nil {
		t.Fatalf("StartExport: %v", err)
	}

	if err := worker.ProcessNextForTest(ctx); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}

	archiveData := fetchCompletedArchive(t, ctx, pool, tenantID)
	zr := openZip(t, archiveData)
	assertZipContains(t, zr, "manifest.json", "events.jsonl", "events.csv")

	m := readManifest(t, zr)
	assertManifestFields(t, m, tenantID, "admin-1", 3)
}

type evidenceManifest struct {
	Format    string `json:"format"`
	ExportID  string `json:"export_id"`
	TenantID  string `json:"tenant_id"`
	CreatedBy struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"created_by"`
	Stats struct {
		TotalEvents  int            `json:"total_events"`
		ActionCounts map[string]int `json:"action_counts"`
	} `json:"stats"`
	Files []struct {
		Name   string `json:"name"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
	Integrity struct {
		Algorithm  string `json:"algorithm"`
		ChainHash  string `json:"chain_hash"`
		EventCount int    `json:"event_count"`
	} `json:"integrity"`
}

func fetchCompletedArchive(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) []byte {
	t.Helper()
	var status string
	var data []byte
	err := pool.QueryRow(ctx, `
		SELECT status, archive FROM audit_evidence_export
		WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT 1`, tenantID,
	).Scan(&status, &data)
	if err != nil {
		t.Fatalf("query completed job: %v", err)
	}
	if status != "completed" {
		t.Fatalf("job status = %q, want completed", status)
	}
	if len(data) == 0 {
		t.Fatal("archive data is empty")
	}
	return data
}

func openZip(t *testing.T, data []byte) *zip.Reader {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	return zr
}

func assertZipContains(t *testing.T, zr *zip.Reader, names ...string) {
	t.Helper()
	have := make(map[string]bool)
	for _, f := range zr.File {
		have[f.Name] = true
	}
	for _, name := range names {
		if !have[name] {
			t.Errorf("missing zip entry: %s", name)
		}
	}
}

func readManifest(t *testing.T, zr *zip.Reader) evidenceManifest {
	t.Helper()
	f := findZipFile(zr, "manifest.json")
	if f == nil {
		t.Fatal("manifest.json not found")
	}
	rc, err := f.Open()
	if err != nil {
		t.Fatalf("open manifest: %v", err)
	}
	defer rc.Close()
	var m evidenceManifest
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return m
}

func assertManifestFields(t *testing.T, m evidenceManifest, tenantID, createdByID string, minEvents int) {
	t.Helper()
	if m.Format != "attune-audit-evidence-v1" {
		t.Errorf("format = %s", m.Format)
	}
	if m.TenantID != tenantID {
		t.Errorf("tenant_id = %s, want %s", m.TenantID, tenantID)
	}
	if m.CreatedBy.ID != createdByID {
		t.Errorf("created_by.id = %s", m.CreatedBy.ID)
	}
	if m.Stats.TotalEvents < minEvents {
		t.Errorf("total_events = %d, want >= %d", m.Stats.TotalEvents, minEvents)
	}
	if len(m.Files) != 2 {
		t.Errorf("files count = %d, want 2", len(m.Files))
	}
	if m.Integrity.Algorithm != "SHA-256" {
		t.Errorf("integrity.algorithm = %s", m.Integrity.Algorithm)
	}
	if m.Integrity.ChainHash == "" {
		t.Error("integrity.chain_hash is empty")
	}
}

func TestPG_EvidenceExportTenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantA := createTenant(t, ctx, pool, "evidence-iso-a", "Tenant A")
	tenantB := createTenant(t, ctx, pool, "evidence-iso-b", "Tenant B")

	repo := aerepo.New(pool)
	auditSvc := auditlog.New(auditlogrepo.New(pool))
	svc := auditevidence.New(repo, auditSvc, auditSvc)

	resultA, err := svc.StartExport(ctx, tenantA, "admin-a", auditevidence.ExportFilter{})
	if err != nil {
		t.Fatalf("StartExport A: %v", err)
	}

	_, err = svc.GetJob(ctx, tenantB, resultA.JobID)
	if !errors.Is(err, auditevidence.ErrJobNotFound) {
		t.Fatalf("cross-tenant GetJob: got %v, want ErrJobNotFound", err)
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

func seedAuditEntries(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := pool.Exec(ctx, `
			INSERT INTO audit_log (
				tenant_id, actor_type, actor_id, actor_email, actor_ip, actor_user_agent,
				action, target_type, target_id, summary
			) VALUES ($1, 'admin', 'user-1', 'admin@test.com', '127.0.0.1', 'test',
				'api_key.create', 'api_key', 'key-1', 'test action')`,
			tenantID,
		)
		if err != nil {
			t.Fatalf("seed audit entry %d: %v", i, err)
		}
	}
}

func assertAuditLogActionCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, action string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
		tenantID, action,
	).Scan(&got); err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	if got != want {
		t.Fatalf("audit_log action %q count = %d, want %d", action, got, want)
	}
}

func findZipFile(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}
