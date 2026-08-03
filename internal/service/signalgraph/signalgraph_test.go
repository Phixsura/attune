// ptrext:file-allow test fakes implement pgx and audit interfaces.
package signalgraph

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	repo "github.com/Phixsura/attune/internal/repo/signalgraph"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestMergeIdentityReviewNormalizesAndAudits(t *testing.T) {
	t.Parallel()

	subjectID := uuid.New()
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	graphRepo := &fakeSignalGraphRepo{
		result: repo.MergeIdentityReviewResult{
			Subject: repo.Subject{
				ID:                   subjectID,
				TenantID:             "tenant-1",
				DisplayName:          "Ada Lovelace",
				PrimaryIdentityKind:  "email",
				PrimaryIdentityValue: "Ada@Example.COM",
				Status:               "active",
				IdentityCount:        1,
				EvidenceCount:        2,
				CreatedAt:            now,
				UpdatedAt:            now,
			},
			EvidenceCount:  2,
			CreatedSubject: true,
		},
	}
	auditRepo := &fakeAuditRepo{}
	svc := New(graphRepo, auditlogsvc.New(auditRepo))

	got, err := svc.MergeIdentityReview(context.Background(), MergeIdentityReviewInput{
		TenantID:      " tenant-1 ",
		IdentityKind:  " Email ",
		IdentityValue: " Ada@Example.COM ",
		FeedbackIDs:   []int64{202, 201, 0, 201},
		Note:          " reviewed by support ",
		Actor: auditlogsvc.Actor{
			Type:      "oidc",
			ID:        "user-1",
			IP:        "203.0.113.9",
			UserAgent: "Playwright",
		},
	})

	require.NoError(t, err)
	require.Equal(t, subjectID, got.Subject.ID)
	require.True(t, got.CreatedSubject)
	require.True(t, graphRepo.tx.committed)
	require.Equal(t, repo.MergeIdentityReviewInput{
		TenantID:                "tenant-1",
		ActorID:                 "user-1",
		IdentityKind:            "email",
		IdentityValue:           "Ada@Example.COM",
		IdentityValueNormalized: "ada@example.com",
		FeedbackIDs:             []int64{201, 202},
		Note:                    "reviewed by support",
	}, graphRepo.input)
	require.Len(t, auditRepo.entries, 1)
	entry := auditRepo.entries[0]
	require.Equal(t, "signal_subject.merge", entry.Action)
	require.Equal(t, "signal_subject", entry.TargetType)
	require.Equal(t, subjectID.String(), entry.TargetID)
	require.Equal(t, "oidc", entry.ActorType)
	require.Equal(t, "203.0.113.9", entry.ActorIP)
	require.NotContains(t, string(entry.AfterJSON), "Ada@Example.COM")
	require.NotContains(t, strings.ToLower(string(entry.AfterJSON)), "ada@example.com")

	var after map[string]any
	require.NoError(t, json.Unmarshal(entry.AfterJSON, &after))
	require.Equal(t, "email", after["identity_kind"])
	require.Equal(t, true, after["created_subject"])
	require.Equal(t, float64(2), after["feedback_count"])
	require.NotEmpty(t, after["identity_value_hash"])
}

func TestMergeIdentityReviewRejectsWeakEvidence(t *testing.T) {
	t.Parallel()

	graphRepo := &fakeSignalGraphRepo{}
	svc := New(graphRepo, nil)

	_, err := svc.MergeIdentityReview(context.Background(), MergeIdentityReviewInput{
		TenantID:      "tenant-1",
		IdentityKind:  "email",
		IdentityValue: "ada@example.com",
		FeedbackIDs:   []int64{201},
		Actor:         auditlogsvc.Actor{ID: "user-1"},
	})

	require.ErrorIs(t, err, ErrValidation)
	require.False(t, graphRepo.beginCalled)
}

func TestMergeIdentityReviewPropagatesEvidenceNotFound(t *testing.T) {
	t.Parallel()

	graphRepo := &fakeSignalGraphRepo{err: repo.ErrFeedbackNotFound}
	svc := New(graphRepo, nil)

	_, err := svc.MergeIdentityReview(context.Background(), MergeIdentityReviewInput{
		TenantID:      "tenant-1",
		IdentityKind:  "email",
		IdentityValue: "ada@example.com",
		FeedbackIDs:   []int64{201, 202},
		Actor:         auditlogsvc.Actor{ID: "user-1"},
	})

	require.ErrorIs(t, err, ErrFeedbackNotFound)
	require.False(t, graphRepo.tx.committed)
}

func TestSplitIdentityReviewNormalizesAndAudits(t *testing.T) {
	t.Parallel()

	subjectID := uuid.New()
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	graphRepo := &fakeSignalGraphRepo{
		splitResult: repo.SplitIdentityReviewResult{
			Subject: repo.Subject{
				ID:                   subjectID,
				TenantID:             "tenant-1",
				DisplayName:          "Ada Lovelace",
				PrimaryIdentityKind:  "",
				PrimaryIdentityValue: "",
				Status:               "active",
				IdentityCount:        0,
				EvidenceCount:        0,
				CreatedAt:            now,
				UpdatedAt:            now,
			},
			EvidenceCount: 2,
		},
	}
	auditRepo := &fakeAuditRepo{}
	svc := New(graphRepo, auditlogsvc.New(auditRepo))

	got, err := svc.SplitIdentityReview(context.Background(), SplitIdentityReviewInput{
		TenantID:      " tenant-1 ",
		SubjectID:     subjectID.String(),
		IdentityKind:  " Email ",
		IdentityValue: " Ada@Example.COM ",
		Note:          " wrong person ",
		Actor: auditlogsvc.Actor{
			Type:      "oidc",
			ID:        "user-1",
			IP:        "203.0.113.10",
			UserAgent: "Playwright",
		},
	})

	require.NoError(t, err)
	require.Equal(t, subjectID, got.Subject.ID)
	require.True(t, graphRepo.tx.committed)
	require.Equal(t, repo.SplitIdentityReviewInput{
		TenantID:                "tenant-1",
		ActorID:                 "user-1",
		SubjectID:               subjectID,
		IdentityKind:            "email",
		IdentityValue:           "Ada@Example.COM",
		IdentityValueNormalized: "ada@example.com",
		Note:                    "wrong person",
	}, graphRepo.splitInput)
	require.Len(t, auditRepo.entries, 1)
	entry := auditRepo.entries[0]
	require.Equal(t, "signal_subject.split", entry.Action)
	require.Equal(t, subjectID.String(), entry.TargetID)
	require.Equal(t, "oidc", entry.ActorType)
	require.Equal(t, "203.0.113.10", entry.ActorIP)
	require.NotContains(t, string(entry.AfterJSON), "Ada@Example.COM")
	require.NotContains(t, strings.ToLower(string(entry.AfterJSON)), "ada@example.com")

	var after map[string]any
	require.NoError(t, json.Unmarshal(entry.AfterJSON, &after))
	require.Equal(t, "email", after["identity_kind"])
	require.Equal(t, float64(2), after["evidence_count"])
	require.NotEmpty(t, after["identity_value_hash"])
}

func TestSplitIdentityReviewRejectsBadSubject(t *testing.T) {
	t.Parallel()

	graphRepo := &fakeSignalGraphRepo{}
	svc := New(graphRepo, nil)

	_, err := svc.SplitIdentityReview(context.Background(), SplitIdentityReviewInput{
		TenantID:      "tenant-1",
		SubjectID:     "not-a-uuid",
		IdentityKind:  "email",
		IdentityValue: "ada@example.com",
		Actor:         auditlogsvc.Actor{ID: "user-1"},
	})

	require.ErrorIs(t, err, ErrValidation)
	require.False(t, graphRepo.beginCalled)
}

func TestSplitIdentityReviewPropagatesIdentityNotFound(t *testing.T) {
	t.Parallel()

	graphRepo := &fakeSignalGraphRepo{splitErr: repo.ErrIdentityNotFound}
	svc := New(graphRepo, nil)

	_, err := svc.SplitIdentityReview(context.Background(), SplitIdentityReviewInput{
		TenantID:      "tenant-1",
		SubjectID:     uuid.New().String(),
		IdentityKind:  "email",
		IdentityValue: "ada@example.com",
		Actor:         auditlogsvc.Actor{ID: "user-1"},
	})

	require.ErrorIs(t, err, ErrIdentityNotFound)
	require.False(t, graphRepo.tx.committed)
}

func TestSubjectRosterValidatesTenantAndDelegates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC)
	subjectID := uuid.New()
	graphRepo := &fakeSignalGraphRepo{
		roster: repo.SubjectRoster{
			ActiveSubjectCount:  1,
			ActiveIdentityCount: 2,
			EvidenceCount:       4,
			Subjects: []repo.Subject{{
				ID:                   subjectID,
				TenantID:             "tenant-1",
				DisplayName:          "Ada Lovelace",
				PrimaryIdentityKind:  "email",
				PrimaryIdentityValue: "ada@example.com",
				Status:               "active",
				IdentityCount:        2,
				EvidenceCount:        4,
				CreatedAt:            now,
				UpdatedAt:            now,
			}},
		},
	}
	svc := New(graphRepo, nil)

	got, err := svc.SubjectRoster(context.Background(), " tenant-1 ", 6)

	require.NoError(t, err)
	require.Equal(t, 1, got.ActiveSubjectCount)
	require.Equal(t, subjectID, got.Subjects[0].ID)
	require.Equal(t, "tenant-1", graphRepo.rosterTenant)
	require.Equal(t, 6, graphRepo.rosterLimit)
}

func TestSubjectRosterRejectsBlankTenant(t *testing.T) {
	t.Parallel()

	graphRepo := &fakeSignalGraphRepo{}
	svc := New(graphRepo, nil)

	_, err := svc.SubjectRoster(context.Background(), " ", 6)

	require.ErrorIs(t, err, ErrValidation)
	require.Empty(t, graphRepo.rosterTenant)
}

func TestSubjectDetailValidatesSubjectAndDelegates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	subjectID := uuid.New()
	eventID := uuid.New()
	graphRepo := &fakeSignalGraphRepo{
		detail: repo.SubjectDetail{
			Subject: repo.Subject{
				ID:                   subjectID,
				TenantID:             "tenant-1",
				DisplayName:          "Ada Lovelace",
				PrimaryIdentityKind:  "email",
				PrimaryIdentityValue: "ada@example.com",
				Status:               "active",
				IdentityCount:        1,
				EvidenceCount:        2,
				CreatedAt:            now,
				UpdatedAt:            now,
			},
			Events: []repo.SubjectEvent{{
				ID:            eventID,
				Action:        "review_merge",
				IdentityKind:  "email",
				IdentityValue: "ada@example.com",
				EvidenceCount: 2,
				FeedbackIDs:   []int64{201, 202},
				CreatedBy:     "user-1",
				CreatedAt:     now,
			}},
		},
	}
	svc := New(graphRepo, nil)

	got, err := svc.SubjectDetail(context.Background(), " tenant-1 ", subjectID.String(), 20)

	require.NoError(t, err)
	require.Equal(t, subjectID, got.Subject.ID)
	require.Equal(t, eventID, got.Events[0].ID)
	require.Equal(t, "tenant-1", graphRepo.detailTenant)
	require.Equal(t, subjectID, graphRepo.detailSubjectID)
	require.Equal(t, 20, graphRepo.detailLimit)
}

func TestSubjectDetailRejectsBadSubject(t *testing.T) {
	t.Parallel()

	graphRepo := &fakeSignalGraphRepo{}
	svc := New(graphRepo, nil)

	_, err := svc.SubjectDetail(context.Background(), "tenant-1", "bad-subject", 20)

	require.ErrorIs(t, err, ErrValidation)
	require.Empty(t, graphRepo.detailTenant)
}

type fakeSignalGraphRepo struct {
	beginCalled     bool
	input           repo.MergeIdentityReviewInput
	result          repo.MergeIdentityReviewResult
	err             error
	splitInput      repo.SplitIdentityReviewInput
	splitResult     repo.SplitIdentityReviewResult
	splitErr        error
	recent          []repo.RecentMerge
	recentErr       error
	roster          repo.SubjectRoster
	rosterErr       error
	rosterTenant    string
	rosterLimit     int
	detail          repo.SubjectDetail
	detailErr       error
	detailTenant    string
	detailSubjectID uuid.UUID
	detailLimit     int
	tx              *fakeTx
}

func (f *fakeSignalGraphRepo) Begin(context.Context) (pgx.Tx, error) {
	f.beginCalled = true
	f.tx = &fakeTx{}
	return f.tx, nil
}

func (f *fakeSignalGraphRepo) MergeIdentityReviewTx(
	_ context.Context,
	_ pgx.Tx,
	in repo.MergeIdentityReviewInput,
) (repo.MergeIdentityReviewResult, error) {
	f.input = in
	return f.result, f.err
}

func (f *fakeSignalGraphRepo) SplitIdentityReviewTx(
	_ context.Context,
	_ pgx.Tx,
	in repo.SplitIdentityReviewInput,
) (repo.SplitIdentityReviewResult, error) {
	f.splitInput = in
	return f.splitResult, f.splitErr
}

func (f *fakeSignalGraphRepo) ListRecentMerges(_ context.Context, _ string, _ int) ([]repo.RecentMerge, error) {
	return f.recent, f.recentErr
}

func (f *fakeSignalGraphRepo) ListSubjectRoster(
	_ context.Context,
	tenantID string,
	limit int,
) (repo.SubjectRoster, error) {
	f.rosterTenant = tenantID
	f.rosterLimit = limit
	return f.roster, f.rosterErr
}

func (f *fakeSignalGraphRepo) SubjectDetail(
	_ context.Context,
	tenantID string,
	subjectID uuid.UUID,
	eventLimit int,
) (repo.SubjectDetail, error) {
	f.detailTenant = tenantID
	f.detailSubjectID = subjectID
	f.detailLimit = eventLimit
	return f.detail, f.detailErr
}

type fakeAuditRepo struct {
	entries []auditlogrepo.Entry
}

func (f *fakeAuditRepo) Insert(context.Context, auditlogrepo.Entry) error {
	return errors.New("unexpected non-transactional audit insert")
}

func (f *fakeAuditRepo) InsertTx(_ context.Context, _ pgx.Tx, entry auditlogrepo.Entry) error {
	f.entries = append(f.entries, entry)
	return nil
}

func (f *fakeAuditRepo) List(context.Context, auditlogrepo.ListFilter) (auditlogrepo.ListResult, error) {
	return auditlogrepo.ListResult{}, nil
}

func (f *fakeAuditRepo) PruneBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

type fakeTx struct {
	committed bool
	rolled    bool
}

func (f *fakeTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }

func (f *fakeTx) Commit(context.Context) error {
	f.committed = true
	return nil
}

func (f *fakeTx) Rollback(context.Context) error {
	f.rolled = true
	return nil
}

func (f *fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (f *fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

func (f *fakeTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }

func (f *fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (f *fakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func (f *fakeTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func (f *fakeTx) Conn() *pgx.Conn { return nil }
