// SPDX-License-Identifier: Apache-2.0

package publicvisibility

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, value := range r.values {
		target := reflect.ValueOf(dest[i]).Elem()
		if value == nil {
			target.Set(reflect.Zero(target.Type()))
			continue
		}
		source := reflect.ValueOf(value)
		if source.Type().AssignableTo(target.Type()) {
			target.Set(source)
			continue
		}
		target.Set(source.Convert(target.Type()))
	}
	return nil
}

type fakeQueryer struct {
	row  pgx.Row
	sql  string
	args []any
}

func (q *fakeQueryer) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.sql = sql
	q.args = args
	return q.row
}

type fakeRows struct {
	rows   []fakeRow
	index  int
	err    error
	closed bool
}

func (r *fakeRows) Close() {
	r.closed = true
}

func (r *fakeRows) Err() error {
	return r.err
}

func (r *fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *fakeRows) Next() bool {
	if r.index >= len(r.rows) {
		r.closed = true
		return false
	}
	r.index++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	return r.rows[r.index-1].Scan(dest...)
}

func (r *fakeRows) Values() ([]any, error) {
	if r.index == 0 {
		return nil, errors.New("next was not called")
	}
	return r.rows[r.index-1].values, nil
}

func (r *fakeRows) RawValues() [][]byte {
	return nil
}

func (r *fakeRows) Conn() *pgx.Conn {
	return nil
}

func TestColumnHelpers(t *testing.T) {
	t.Parallel()

	repository := New(nil)
	if repository == nil {
		t.Fatal("New() = nil, want repository")
	}
	if !strings.Contains(policyColumns(), "portal_access_mode") {
		t.Fatalf("policyColumns() = %q, want policy columns", policyColumns())
	}
	if !strings.Contains(subjectColumns(), "submitted_by_fingerprint") {
		t.Fatalf("subjectColumns() = %q, want subject columns", subjectColumns())
	}
	if !strings.Contains(profileColumns(), "included_in_roadmap") {
		t.Fatalf("profileColumns() = %q, want profile columns", profileColumns())
	}
	if !strings.Contains(prefixedPolicyColumns("pol"), "pol.tenant_id") {
		t.Fatalf("prefixedPolicyColumns() = %q, want alias prefix", prefixedPolicyColumns("pol"))
	}
	if !strings.Contains(prefixedSubjectColumns("pms"), "pms.subject_id") {
		t.Fatalf("prefixedSubjectColumns() = %q, want alias prefix", prefixedSubjectColumns("pms"))
	}
	if !strings.Contains(prefixedProfileColumns("prp"), "prp.public_slug") {
		t.Fatalf("prefixedProfileColumns() = %q, want alias prefix", prefixedProfileColumns("prp"))
	}
}

func TestPublicBoardSearchTerms(t *testing.T) {
	t.Parallel()

	terms := publicBoardSearchTerms("Pricing API pricing 24/7 bug")
	if want := []string{"pricing", "api", "bug"}; !reflect.DeepEqual(terms, want) {
		t.Fatalf("publicBoardSearchTerms() = %#v, want %#v", terms, want)
	}
}

func TestPublicBoardSimilarityClauseWithoutTermsReturnsFalse(t *testing.T) {
	t.Parallel()

	clause, args := publicBoardSimilarityClause("AI", []any{"tenant-a"})
	if !strings.Contains(clause, "FALSE") || len(args) != 1 {
		t.Fatalf("publicBoardSimilarityClause() = %q args=%#v, want false guard", clause, args)
	}
}

func TestPublicBoardContainsAndSearchClauses(t *testing.T) {
	t.Parallel()

	args := []any{"tenant-a"}
	clause, args := publicBoardContainsClause("prp.public_state", " planned ", args)
	if !strings.Contains(clause, "prp.public_state ILIKE $2") || args[1] != "%planned%" {
		t.Fatalf("publicBoardContainsClause() = %q args=%#v, want contains clause", clause, args)
	}
	clause, args = publicBoardContainsClause("prp.public_state", " ", args)
	if clause != "" || len(args) != 2 {
		t.Fatalf("publicBoardContainsClause(empty) = %q args=%#v, want no-op", clause, args)
	}

	clause, args = publicBoardSearchClause(" billing ", args)
	if !strings.Contains(clause, "public_title ILIKE $3") || args[2] != "%billing%" {
		t.Fatalf("publicBoardSearchClause() = %q args=%#v, want search clause", clause, args)
	}
	clause, args = publicBoardSearchClause("", args)
	if clause != "" || len(args) != 3 {
		t.Fatalf("publicBoardSearchClause(empty) = %q args=%#v, want no-op", clause, args)
	}
}

func TestPublicBoardSimilarityAndExcludeClauses(t *testing.T) {
	t.Parallel()

	args := []any{"tenant-a"}
	clause, args := publicBoardSimilarityClause("API latency and timeout", args)
	if !strings.Contains(clause, "public_summary ILIKE $2") || !strings.Contains(clause, "public_summary ILIKE $5") {
		t.Fatalf("publicBoardSimilarityClause() = %q args=%#v, want term clauses", clause, args)
	}

	clause, args = publicBoardExcludeClause(" old-slug ", args)
	if !strings.Contains(clause, "prp.public_slug <> $6") || args[5] != "old-slug" {
		t.Fatalf("publicBoardExcludeClause() = %q args=%#v, want exclude clause", clause, args)
	}
	clause, args = publicBoardExcludeClause(" ", args)
	if clause != "" || len(args) != 6 {
		t.Fatalf("publicBoardExcludeClause(empty) = %q args=%#v, want no-op", clause, args)
	}
}

func TestPublicBoardViewerVoteAndOrderingClauses(t *testing.T) {
	t.Parallel()

	args := []any{"tenant-a"}
	clause, args := publicBoardViewerVoteClause(true, " viewer-1 ", args)
	if !strings.Contains(clause, "vv.subject_key = $2") || args[1] != "viewer-1" {
		t.Fatalf("publicBoardViewerVoteClause() = %q args=%#v, want vote clause", clause, args)
	}
	clause, args = publicBoardViewerVoteClause(false, "viewer-1", args)
	if clause != "" || len(args) != 2 {
		t.Fatalf("publicBoardViewerVoteClause(disabled) = %q args=%#v, want no-op", clause, args)
	}
	clause, args = publicBoardViewerVoteClause(true, " ", args)
	if !strings.Contains(clause, "FALSE") || len(args) != 2 {
		t.Fatalf("publicBoardViewerVoteClause(no viewer) = %q args=%#v, want false guard", clause, args)
	}

	if got := normalizePublicBoardSort(" Latest "); got != "recent" {
		t.Fatalf("normalizePublicBoardSort(latest) = %q, want recent", got)
	}
	if got := normalizePublicBoardSort("votes"); got != "top" {
		t.Fatalf("normalizePublicBoardSort(votes) = %q, want top", got)
	}
	if got := publicBoardOrderByClause("recent", true); !strings.Contains(got, "roadmap_order") || !strings.Contains(got, "updated_at DESC") {
		t.Fatalf("publicBoardOrderByClause(roadmap recent) = %q, want roadmap recent ordering", got)
	}
	if got := publicBoardOrderByClause("top", false); strings.Contains(got, "roadmap_order") || !strings.Contains(got, "vote_count") {
		t.Fatalf("publicBoardOrderByClause(top) = %q, want vote ordering without roadmap prefix", got)
	}
}

func TestScanPolicyHelper(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	policy, err := scanPolicy(fakeRow{values: policyValues(now)})
	if err != nil {
		t.Fatalf("scanPolicy() error = %v", err)
	}
	if policy.TenantID != "tenant-a" || policy.PortalAccessMode != AccessModePublic {
		t.Fatalf("scanPolicy() = %#v, want policy", policy)
	}
	if policy.PortalSubmissionForm.Headline != "Share feedback" || len(policy.PortalSubmissionForm.Fields) != 1 {
		t.Fatalf("scanPolicy() portal form = %#v, want normalized portal form", policy.PortalSubmissionForm)
	}
	if len(policy.RoadmapStatusMappings) != 5 || policy.RoadmapStatusMappings[0].Status != "open" {
		t.Fatalf("scanPolicy() roadmap mappings = %#v, want normalized roadmap defaults", policy.RoadmapStatusMappings)
	}
}

func TestScanSubjectHelper(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	subjectID := uuid.New()
	subject, err := scanSubject(fakeRow{values: subjectValues(subjectID, now)})
	if err != nil {
		t.Fatalf("scanSubject() error = %v", err)
	}
	if subject.ID != subjectID || subject.State != ModerationStatePending {
		t.Fatalf("scanSubject() = %#v, want subject", subject)
	}
}

func TestScanProfileHelper(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	profile, err := scanProfile(fakeRow{values: profileValues(requestID, now)})
	if err != nil {
		t.Fatalf("scanProfile() error = %v", err)
	}
	if profile.RequestID != requestID || profile.PublicSlug != "pricing-api" {
		t.Fatalf("scanProfile() = %#v, want profile", profile)
	}
	if profile.RoadmapOrder != 2 || !profile.RoadmapVisible {
		t.Fatalf("scanProfile() roadmap fields = %#v, want derived roadmap metadata", profile)
	}
}

func TestScanHelpersPropagateErrors(t *testing.T) {
	t.Parallel()

	scanErr := errors.New("scan failed")
	if _, err := scanPolicy(fakeRow{err: scanErr}); !errors.Is(err, scanErr) {
		t.Fatalf("scanPolicy() error = %v, want %v", err, scanErr)
	}
	if _, err := scanSubject(fakeRow{err: scanErr}); !errors.Is(err, scanErr) {
		t.Fatalf("scanSubject() error = %v, want %v", err, scanErr)
	}
	if _, err := scanProfile(fakeRow{err: scanErr}); !errors.Is(err, scanErr) {
		t.Fatalf("scanProfile() error = %v, want %v", err, scanErr)
	}
}

func TestLoadHelpersMapNoRowsAndUseExpectedQueries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	query := ptrext.Of(fakeQueryer{row: fakeRow{values: policyValues(now)}})
	policy, err := loadPolicy(context.Background(), query, "tenant-a")
	if err != nil {
		t.Fatalf("loadPolicy() error = %v", err)
	}
	if policy.TenantID != "tenant-a" || query.args[0] != "tenant-a" {
		t.Fatalf("loadPolicy() = %#v args=%#v, want tenant lookup", policy, query.args)
	}

	subjectID := uuid.New()
	query = ptrext.Of(fakeQueryer{row: fakeRow{values: subjectValues(subjectID, now)}})
	subject, err := loadSubject(context.Background(), query, "tenant-a", subjectID, true)
	if err != nil {
		t.Fatalf("loadSubject() error = %v", err)
	}
	if subject.ID != subjectID || !strings.Contains(query.sql, "FOR UPDATE") {
		t.Fatalf("loadSubject() = %#v sql=%q, want locked subject lookup", subject, query.sql)
	}

	query = ptrext.Of(fakeQueryer{row: fakeRow{err: pgx.ErrNoRows}})
	if _, err := loadPolicy(context.Background(), query, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("loadPolicy() error = %v, want %v", err, ErrNotFound)
	}
	if _, err := loadSubject(context.Background(), query, "missing", uuid.New(), false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("loadSubject() error = %v, want %v", err, ErrNotFound)
	}
}

func TestScanSubjects(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	rows := ptrext.Of(fakeRows{rows: []fakeRow{
		{values: subjectValues(uuid.New(), now)},
		{values: subjectValues(uuid.New(), now.Add(time.Minute))},
	}})
	subjects, err := scanSubjects(rows)
	if err != nil {
		t.Fatalf("scanSubjects() error = %v", err)
	}
	if len(subjects) != 2 || !rows.closed {
		t.Fatalf("scanSubjects() = %#v closed=%v, want two closed rows", subjects, rows.closed)
	}

	rows = ptrext.Of(fakeRows{err: errors.New("read failed")})
	if _, err := scanSubjects(rows); err == nil {
		t.Fatal("scanSubjects() error = nil, want rows error")
	}

	rows = ptrext.Of(fakeRows{rows: []fakeRow{{err: errors.New("scan failed")}}})
	if _, err := scanSubjects(rows); err == nil {
		t.Fatal("scanSubjects() error = nil, want row scan error")
	}
}

func TestScanPublicRequestListCandidates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	row := append(profileValues(requestID, now), subjectValues(uuid.New(), now)...)
	row = append(row, int64(4), true, int64(2), requestID)
	rows := ptrext.Of(fakeRows{rows: []fakeRow{{values: row}}})

	items, err := scanPublicRequestListCandidates(rows)
	if err != nil {
		t.Fatalf("scanPublicRequestListCandidates() error = %v", err)
	}
	if len(items) != 1 || !rows.closed {
		t.Fatalf("scanPublicRequestListCandidates() = %#v closed=%v, want one closed row", items, rows.closed)
	}
	if items[0].Profile.RequestID != requestID || items[0].VoteCount != 4 ||
		!items[0].ViewerHasVoted || items[0].CommentCount != 2 || items[0].SubmitterDisplay != "Ada" {
		t.Fatalf("scanPublicRequestListCandidates() = %#v, want public request list candidate", items[0])
	}

	rows = ptrext.Of(fakeRows{err: errors.New("read failed")})
	if _, err := scanPublicRequestListCandidates(rows); err == nil {
		t.Fatal("scanPublicRequestListCandidates() error = nil, want rows error")
	}

	rows = ptrext.Of(fakeRows{rows: []fakeRow{{err: errors.New("scan failed")}}})
	if _, err := scanPublicRequestListCandidates(rows); err == nil {
		t.Fatal("scanPublicRequestListCandidates() error = nil, want row scan error")
	}
}

func TestPolicyAndSubjectTxMethodsUseTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := Repo{}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	subjectID := uuid.New()
	tx := ptrext.Of(fakeTx{
		rows: []pgx.Row{
			fakeRow{values: policyValues(now)},
			fakeRow{values: subjectValues(subjectID, now)},
			fakeRow{values: subjectValues(subjectID, now)},
			fakeRow{values: subjectValues(subjectID, now)},
		},
	})

	policy, err := repository.UpsertPolicyTx(ctx, tx, Policy{
		TenantID:              "tenant-a",
		PortalAccessMode:      AccessModePublic,
		SearchIndexingEnabled: true,
		RequestsEnabled:       true,
		CommentsEnabled:       true,
		RoadmapEnabled:        true,
		SubmissionWriteMode:   WriteModeIdentified,
		CommentWriteMode:      WriteModeDisabled,
		VoteWriteMode:         WriteModeAnonymous,
		DefaultRequestState:   ModerationStateApproved,
		DefaultCommentState:   ModerationStatePending,
		SubmitterIdentityMode: IdentityModeDisplayName,
		ShowVoteCount:         true,
		ShowSubmitterDisplay:  true,
		PortalSubmissionForm:  PortalSubmissionForm{Headline: "Share feedback"},
		UpdatedBy:             "admin-1",
	})
	if err != nil {
		t.Fatalf("UpsertPolicyTx() error = %v", err)
	}
	if policy.TenantID != "tenant-a" || len(tx.args[0]) != 20 {
		t.Fatalf("UpsertPolicyTx() = %#v args=%#v, want policy insert args", policy, tx.args[0])
	}

	subject, err := repository.GetSubjectForUpdateTx(ctx, tx, "tenant-a", subjectID)
	if err != nil {
		t.Fatalf("GetSubjectForUpdateTx() error = %v", err)
	}
	if subject.ID != subjectID || !strings.Contains(tx.queries[1], "FOR UPDATE") {
		t.Fatalf("GetSubjectForUpdateTx() = %#v sql=%q, want locked subject", subject, tx.queries[1])
	}

	reviewedAt := now.Add(time.Hour)
	subject, err = repository.UpdateSubjectStateTx(
		ctx,
		tx,
		"tenant-a",
		subjectID,
		ModerationStateApproved,
		"valid",
		"looks good",
		"reviewer-1",
		reviewedAt,
	)
	if err != nil {
		t.Fatalf("UpdateSubjectStateTx() error = %v", err)
	}
	if subject.ID != subjectID || tx.args[2][5] != "reviewer-1" || tx.args[2][6] != reviewedAt {
		t.Fatalf("UpdateSubjectStateTx() = %#v args=%#v, want review metadata", subject, tx.args[2])
	}

	subject, err = repository.CreateModerationSubjectTx(ctx, tx, ModerationSubject{
		TenantID:               "tenant-a",
		Surface:                SurfaceRequest,
		SubjectID:              "request-profile-1",
		State:                  ModerationStatePending,
		SubmittedByDisplay:     "Ada",
		SubmittedByFingerprint: "fingerprint-1",
	})
	if err != nil {
		t.Fatalf("CreateModerationSubjectTx() error = %v", err)
	}
	if subject.ID != subjectID || tx.args[3][5] != "fingerprint-1" {
		t.Fatalf("CreateModerationSubjectTx() = %#v args=%#v, want moderation subject", subject, tx.args[3])
	}
}

func TestPolicyAndSubjectTxMethodsMapErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := Repo{}
	subjectID := uuid.New()
	const constraint = "public_visibility_policy_tenant_id_fkey"
	pgErr := ptrext.Of(pgconn.PgError{Code: "23503", ConstraintName: constraint})

	if _, err := repository.UpsertPolicyTx(ctx, ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{err: pgErr}}}), Policy{
		TenantID: "tenant-a",
	}); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), constraint) {
		t.Fatalf("UpsertPolicyTx(error) = %v, want invalid input with constraint", err)
	}
	if _, err := repository.GetSubjectForUpdateTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{err: pgx.ErrNoRows}}}),
		"tenant-a",
		subjectID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSubjectForUpdateTx(missing) = %v, want %v", err, ErrNotFound)
	}
	if _, err := repository.UpdateSubjectStateTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{err: pgx.ErrNoRows}}}),
		"tenant-a",
		subjectID,
		ModerationStateRejected,
		"spam",
		"bad",
		"reviewer-1",
		time.Now(),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateSubjectStateTx(missing) = %v, want %v", err, ErrNotFound)
	}
	if _, err := repository.CreateModerationSubjectTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{err: pgErr}}}),
		ModerationSubject{TenantID: "tenant-a"},
	); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), constraint) {
		t.Fatalf("CreateModerationSubjectTx(error) = %v, want invalid input with constraint", err)
	}
}

func TestUpsertRequestPublicationTxUsesTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := Repo{}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	subjectID := uuid.New()
	tx := ptrext.Of(fakeTx{
		rows: []pgx.Row{
			fakeRow{values: []any{true}},
			fakeRow{values: profileValues(requestID, now)},
			fakeRow{values: subjectValues(subjectID, now)},
		},
	})

	publication, err := repository.UpsertRequestPublicationTx(
		ctx,
		tx,
		RequestProfile{
			TenantID:          "tenant-a",
			RequestID:         requestID,
			PublicSlug:        "pricing-api",
			PublicTitle:       "Pricing API",
			PublicSummary:     "Summary",
			PublicState:       "planned",
			RoadmapColumn:     "next",
			IncludedInPortal:  true,
			IncludedInRoadmap: true,
			UpdatedBy:         "admin-1",
		},
		ModerationStatePending,
		"Ada",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("UpsertRequestPublicationTx() error = %v", err)
	}
	if publication.Profile.RequestID != requestID || publication.Moderation.ID != subjectID || len(tx.queries) != 3 {
		t.Fatalf("UpsertRequestPublicationTx() = %#v queries=%d, want publication", publication, len(tx.queries))
	}
}

func TestUpsertRequestPublicationTxMapsFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := Repo{}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	pgErr := ptrext.Of(pgconn.PgError{Code: "23505", ConstraintName: "public_request_profiles_public_slug_key"})
	profile := RequestProfile{TenantID: "tenant-a", RequestID: requestID, PublicSlug: "pricing-api"}

	if _, err := repository.UpsertRequestPublicationTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{values: []any{false}}}}),
		profile,
		ModerationStatePending,
		"Ada",
		"fingerprint-1",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpsertRequestPublicationTx(missing request) = %v, want %v", err, ErrNotFound)
	}
	if _, err := repository.UpsertRequestPublicationTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{err: errors.New("exists failed")}}}),
		profile,
		ModerationStatePending,
		"Ada",
		"fingerprint-1",
	); err == nil || !strings.Contains(err.Error(), "exists failed") {
		t.Fatalf("UpsertRequestPublicationTx(exists error) = %v, want raw error", err)
	}
	if _, err := repository.UpsertRequestPublicationTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{
			fakeRow{values: []any{true}},
			fakeRow{err: pgErr},
		}}),
		profile,
		ModerationStatePending,
		"Ada",
		"fingerprint-1",
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpsertRequestPublicationTx(profile error) = %v, want %v", err, ErrInvalidInput)
	}
	if _, err := repository.UpsertRequestPublicationTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{
			fakeRow{values: []any{true}},
			fakeRow{values: profileValues(requestID, now)},
			fakeRow{err: pgErr},
		}}),
		profile,
		ModerationStatePending,
		"Ada",
		"fingerprint-1",
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpsertRequestPublicationTx(subject error) = %v, want %v", err, ErrInvalidInput)
	}
}

func TestPublicRequestVoteTxMethods(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := Repo{}
	requestID := uuid.New()
	pgErr := ptrext.Of(pgconn.PgError{Code: "23503", ConstraintName: "customer_request_votes_request_id_fkey"})

	if err := repository.AddPublicRequestVoteTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{values: []any{uuid.New()}}}}),
		"tenant-a",
		requestID,
		"portal:user-1",
		"hash-1",
		"Ada",
		"portal",
	); err != nil {
		t.Fatalf("AddPublicRequestVoteTx() error = %v", err)
	}
	if err := repository.AddPublicRequestVoteTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{err: pgx.ErrNoRows}}}),
		"tenant-a",
		requestID,
		"portal:user-1",
		"hash-1",
		"Ada",
		"portal",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddPublicRequestVoteTx(missing) = %v, want %v", err, ErrNotFound)
	}
	if err := repository.AddPublicRequestVoteTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{err: pgErr}}}),
		"tenant-a",
		requestID,
		"portal:user-1",
		"hash-1",
		"Ada",
		"portal",
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("AddPublicRequestVoteTx(error) = %v, want %v", err, ErrInvalidInput)
	}

	tx := ptrext.Of(fakeTx{})
	if err := repository.RemovePublicRequestVoteTx(ctx, tx, "tenant-a", requestID, "portal:user-1"); err != nil {
		t.Fatalf("RemovePublicRequestVoteTx() error = %v", err)
	}
	if tx.execs != 1 {
		t.Fatalf("RemovePublicRequestVoteTx() execs = %d, want 1", tx.execs)
	}
	if err := repository.RemovePublicRequestVoteTx(
		ctx,
		ptrext.Of(fakeTx{execErr: errors.New("delete failed")}),
		"tenant-a",
		requestID,
		"portal:user-1",
	); err == nil || !strings.Contains(err.Error(), "remove public vote") {
		t.Fatalf("RemovePublicRequestVoteTx(error) = %v, want wrapped delete error", err)
	}
}

func TestPublicRequestCommentTxMethods(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := Repo{}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	commentID := uuid.New()
	pgErr := ptrext.Of(pgconn.PgError{Code: "23503", ConstraintName: "customer_request_votes_request_id_fkey"})

	comment, err := repository.AddPublicRequestCommentTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{values: []any{commentID, "hello", "Ada", now}}}}),
		"tenant-a",
		requestID,
		"portal:user-1",
		"hash-1",
		"Ada",
		"hello",
		"portal",
	)
	if err != nil {
		t.Fatalf("AddPublicRequestCommentTx() error = %v", err)
	}
	if comment.ID != commentID || comment.Body != "hello" || comment.SubmittedByDisplay != "Ada" {
		t.Fatalf("AddPublicRequestCommentTx() = %#v, want comment", comment)
	}
	if _, err := repository.AddPublicRequestCommentTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{err: pgx.ErrNoRows}}}),
		"tenant-a",
		requestID,
		"portal:user-1",
		"hash-1",
		"Ada",
		"hello",
		"portal",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddPublicRequestCommentTx(missing) = %v, want %v", err, ErrNotFound)
	}
	if _, err := repository.AddPublicRequestCommentTx(
		ctx,
		ptrext.Of(fakeTx{rows: []pgx.Row{fakeRow{err: pgErr}}}),
		"tenant-a",
		requestID,
		"portal:user-1",
		"hash-1",
		"Ada",
		"hello",
		"portal",
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("AddPublicRequestCommentTx(error) = %v, want %v", err, ErrInvalidInput)
	}
}

func TestPaginationAndWriteErrorHelpers(t *testing.T) {
	t.Parallel()

	if boundedLimit(0) != 50 || boundedLimit(101) != 100 || boundedLimit(20) != 20 {
		t.Fatalf("boundedLimit() returned unexpected values")
	}
	if offset, err := parseCursor(" 7 "); err != nil || offset != 7 {
		t.Fatalf("parseCursor() = %d, %v, want offset", offset, err)
	}
	if offset, err := parseCursor(" "); err != nil || offset != 0 {
		t.Fatalf("parseCursor(empty) = %d, %v, want zero", offset, err)
	}
	if _, err := parseCursor("-1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("parseCursor(-1) error = %v, want %v", err, ErrInvalidInput)
	}
	if _, err := parseCursor("bad"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("parseCursor(bad) error = %v, want %v", err, ErrInvalidInput)
	}

	err := mapWriteError(ptrext.Of(pgconn.PgError{Code: "23505", ConstraintName: "public_subject_key"}))
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "public_subject_key") {
		t.Fatalf("mapWriteError() error = %v, want invalid input with constraint", err)
	}
	base := errors.New("plain error")
	if !errors.Is(mapWriteError(base), base) {
		t.Fatalf("mapWriteError() did not preserve plain error")
	}
}

func policyValues(now time.Time) []any {
	return []any{
		"tenant-a", AccessModePublic, true, true, true, true, false,
		WriteModeIdentified, WriteModeDisabled, WriteModeAnonymous,
		ModerationStateApproved, ModerationStatePending, IdentityModeDisplayName,
		true, false, true, false, nil, portalSubmissionFormJSON(), "admin-1", now, now.Add(time.Minute),
	}
}

func portalSubmissionFormJSON() []byte {
	return []byte(`{
		"headline":"Share feedback",
		"description":"Tell us what is broken, missing, or worth improving.",
		"acknowledgement":"Thanks. We will review your submission.",
		"submit_button_label":"Submit feedback",
		"show_page_url":true,
		"fields":[
			{
				"key":"severity",
				"label":"Severity",
				"kind":"select",
				"required":true,
				"options":["low","medium","high"],
				"placeholder":"Choose a severity"
			}
		]
	}`)
}

func subjectValues(id uuid.UUID, now time.Time) []any {
	return []any{
		id, "tenant-a", SurfaceRequest, "request-profile-1", ModerationStatePending,
		"", "", "Ada", "fingerprint-1", "", ptrext.Of(now),
		now, now.Add(time.Minute),
	}
}

func profileValues(requestID uuid.UUID, now time.Time) []any {
	return []any{
		uuid.New(), "tenant-a", requestID, "pricing-api", "Pricing API", "Summary",
		"planned", "next", 2, true, true, false, ptrext.Of(now), "admin-1",
		now, now.Add(time.Minute),
	}
}

type fakeTx struct {
	rows    []pgx.Row
	row     int
	queries []string
	args    [][]any
	execs   int
	execErr error
}

func (tx *fakeTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *fakeTx) Commit(context.Context) error          { return nil }
func (tx *fakeTx) Rollback(context.Context) error        { return nil }
func (tx *fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *fakeTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }

func (tx *fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *fakeTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.execs++
	tx.queries = append(tx.queries, sql)
	tx.args = append(tx.args, args)
	if tx.execErr != nil {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}

func (tx *fakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call in fakeTx")
}

func (tx *fakeTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.queries = append(tx.queries, sql)
	tx.args = append(tx.args, args)
	if tx.row >= len(tx.rows) {
		return fakeRow{err: errors.New("unexpected QueryRow call in fakeTx")}
	}
	row := tx.rows[tx.row]
	tx.row++
	return row
}
func (tx *fakeTx) Conn() *pgx.Conn { return nil }
