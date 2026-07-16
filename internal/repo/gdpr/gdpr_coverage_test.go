// SPDX-License-Identifier: Apache-2.0

package gdpr

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepoSubjectMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableGDPRRepo(t)

	expectGDPRErr(t, "Export", func() error {
		_, err := r.Export(ctx, "tenant-1", "subject-1")
		return err
	})
	expectGDPRErr(t, "Delete", func() error {
		_, err := r.Delete(ctx, "tenant-1", "subject-1")
		return err
	})
	expectGDPRErr(t, "ExecuteDeleteRequest", func() error {
		_, err := r.ExecuteDeleteRequest(ctx, "request-1")
		return err
	})
	expectGDPRErr(t, "queryJSONLines", func() error {
		_, err := r.queryJSONLines(ctx, `SELECT '{}'::jsonb`)
		return err
	})
}

func TestRepoRequestMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableGDPRRepo(t)
	now := time.Now().UTC()
	counts := Counts{FeedbackCount: 1, TagAssignmentCount: 2, FeedbackAuditCount: 3, LLMAuditCount: 4, OutboxCount: 5}

	expectGDPRErr(t, "ListRequests", func() error {
		_, err := r.ListRequests(ctx, ListRequestFilter{
			TenantID:    "tenant-1",
			Limit:       250,
			RequestType: string(RequestTypeDelete),
		})
		return err
	})
	expectGDPRErr(t, "GetOperationsSummary", func() error {
		_, err := r.GetOperationsSummary(ctx, "tenant-1")
		return err
	})
	expectGDPRErr(t, "CreateDeleteRequest", func() error {
		_, err := r.CreateDeleteRequest(ctx, "tenant-1", "subject-1", "hash-1", "admin", "user-1", now)
		return err
	})
	expectGDPRErr(t, "CancelDeleteRequest", func() error {
		_, err := r.CancelDeleteRequest(ctx, "tenant-1", "request-1")
		return err
	})
	expectGDPRErr(t, "ClaimNextDeleteRequest", func() error {
		_, err := r.ClaimNextDeleteRequest(ctx, now)
		return err
	})
	expectGDPRErr(t, "CompleteDeleteRequest", func() error {
		return r.CompleteDeleteRequest(ctx, "request-1", counts)
	})
	expectGDPRErr(t, "FailDeleteRequest", func() error {
		return r.FailDeleteRequest(ctx, "request-1", "failed")
	})
	if _, err := r.ListRequests(ctx, ListRequestFilter{TenantID: "tenant-1", Cursor: "bad"}); err == nil {
		t.Fatalf("ListRequests(invalid cursor) error = nil")
	}
}

func TestRepoExportJobMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableGDPRRepo(t)
	expiresAt := time.Now().UTC().Add(time.Hour)
	counts := Counts{FeedbackCount: 1, TagAssignmentCount: 2, FeedbackAuditCount: 3, LLMAuditCount: 4}

	expectGDPRErr(t, "CreateExportJob", func() error {
		_, err := r.CreateExportJob(ctx, "tenant-1", "subject-1", "hash-1", "admin", "user-1")
		return err
	})
	expectGDPRErr(t, "GetExportJob", func() error {
		_, err := r.GetExportJob(ctx, "tenant-1", "job-1")
		return err
	})
	expectGDPRErr(t, "ClaimNextExportJob", func() error {
		_, err := r.ClaimNextExportJob(ctx)
		return err
	})
	expectGDPRErr(t, "ClaimNextExportJobWithOwner", func() error {
		_, err := r.ClaimNextExportJobWithOwner(ctx, "worker-1")
		return err
	})
	expectGDPRErr(t, "HeartbeatExportJob", func() error {
		return r.HeartbeatExportJob(ctx, "job-1")
	})
	expectGDPRErr(t, "HeartbeatExportJobWithOwner", func() error {
		_, err := r.HeartbeatExportJobWithOwner(ctx, "job-1", "worker-1")
		return err
	})
	expectGDPRErr(t, "CompleteExportJob", func() error {
		return r.CompleteExportJob(ctx, "job-1", "Subject One", "export.zip", []byte("zip"), counts, expiresAt)
	})
	expectGDPRErr(t, "CompleteExportJobWithOwner", func() error {
		_, err := r.CompleteExportJobWithOwner(ctx, "job-1", "worker-1", "Subject One", "export.zip", []byte("zip"), counts, expiresAt)
		return err
	})
	expectGDPRErr(t, "FailExportJob", func() error {
		return r.FailExportJob(ctx, "job-1", "failed")
	})
	expectGDPRErr(t, "FailExportJobWithOwner", func() error {
		_, err := r.FailExportJobWithOwner(ctx, "job-1", "worker-1", "failed")
		return err
	})
	expectGDPRErr(t, "ExpireReadyExportJobs", func() error {
		_, err := r.ExpireReadyExportJobs(ctx, expiresAt)
		return err
	})
	expectGDPRErr(t, "MarkExportJobDownloaded", func() error {
		_, err := r.MarkExportJobDownloaded(ctx, "tenant-1", "job-1")
		return err
	})
	expectGDPRErr(t, "RevokeExportJob", func() error {
		_, err := r.RevokeExportJob(ctx, "tenant-1", "job-1")
		return err
	})
}

func newUnreachableGDPRRepo(t *testing.T) *Repo {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return New(pool)
}

func expectGDPRErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
