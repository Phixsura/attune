package feedbackassignment

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbackaudit"
	"github.com/Phixsura/attune/internal/repo/systemsettings"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestRecommendBatchRulesAndFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	store := ptrext.Of(recommendationStore{candidates: map[int64]feedbackrepo.AssignmentCandidate{
		1: recommendationCandidate(1, now.Add(-2*time.Hour), candidateOpts{
			urgent:            true,
			workflowCategory:  "open",
			hasStableIdentity: true,
		}),
		2: recommendationCandidate(2, now.Add(-6*time.Hour), candidateOpts{
			enrichmentStatus:   "failed",
			enrichmentAttempts: terminalRecommendationAttempts,
			workflowCategory:   "open",
			hasStableIdentity:  true,
		}),
		3: recommendationCandidate(3, now.Add(-6*time.Hour), candidateOpts{
			workflowCategory:  "closed",
			hasStableIdentity: true,
		}),
	}})
	svc := New(store, nil, ptrext.Of(recommendationTx{}))

	got, err := svc.RecommendBatch(context.Background(), RecommendationInput{
		TenantID:    "tenant-1",
		FeedbackIDs: []int64{1, 2, 3, 99},
		Now:         now,
	})
	if err != nil {
		t.Fatalf("RecommendBatch() error = %v", err)
	}
	if got.TotalMatched != 4 || len(got.Recommendations) != 2 || len(got.Failed) != 2 {
		t.Fatalf("RecommendBatch() = %#v, want 2 recommendations and 2 failures", got)
	}
	if got.Recommendations[0].RuleKey != "urgent_open" {
		t.Fatalf("first rule = %s, want urgent_open", got.Recommendations[0].RuleKey)
	}
	if got.Recommendations[1].RuleKey != "terminal_failures" {
		t.Fatalf("second rule = %s, want terminal_failures", got.Recommendations[1].RuleKey)
	}
	if got.Failed[0].Code != "NO_RULE" || got.Failed[1].Code != "NOT_FOUND" {
		t.Fatalf("failure codes = %#v, want NO_RULE then NOT_FOUND", got.Failed)
	}
}

func TestApplyRecommendationsWritesPolicyNoteAndSkipsSatisfied(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	createdAt := now.Add(-24 * time.Hour)
	recommendedDue := createdAt.Add(72 * time.Hour)
	store := ptrext.Of(recommendationStore{candidates: map[int64]feedbackrepo.AssignmentCandidate{
		1: recommendationCandidate(1, createdAt, candidateOpts{
			workflowCategory:  "open",
			hasStableIdentity: true,
		}),
		2: recommendationCandidate(2, createdAt, candidateOpts{
			workflowCategory:  "open",
			hasStableIdentity: true,
			currentDueAt:      ptrext.Of(recommendedDue.Add(-time.Hour)),
		}),
	}})
	audits := ptrext.Of(recommendationAuditWriter{})
	svc := New(store, audits, ptrext.Of(recommendationTx{}))

	got, err := svc.ApplyRecommendations(context.Background(), ApplyRecommendationInput{
		TenantID:    "tenant-1",
		FeedbackIDs: []int64{1, 2},
		ActorID:     "operator-1",
		Now:         now,
	})
	if err != nil {
		t.Fatalf("ApplyRecommendations() error = %v", err)
	}
	if got.Succeeded != 1 || got.Skipped != 1 || len(got.Applied) != 1 {
		t.Fatalf("ApplyRecommendations() = %#v, want one write and one skip", got)
	}
	assigned := store.assignments[1]
	if assigned.SLADueAt == nil || !assigned.SLADueAt.Equal(recommendedDue) {
		t.Fatalf("assigned SLA = %v, want %v", assigned.SLADueAt, recommendedDue)
	}
	if assigned.Note != "Assignment policy: Untriaged intake (triage_dri, 72h)." {
		t.Fatalf("assigned note = %q", assigned.Note)
	}
	if len(audits.entries) == 0 {
		t.Fatal("expected assignment audit entries")
	}
}

func TestAssignmentPolicyUpdatesRecommendationsAndDefaultOwner(t *testing.T) {
	t.Parallel()

	fixture := newAssignmentPolicyTestFixture(t)
	policyV1 := updateAssignmentPolicyForTest(t, fixture.svc, "admin-1", "enterprise lane", 12, fixture.ownerID)
	assertPolicyV1ForTest(t, policyV1, fixture.policyStore, fixture.policyAudits)
	assertPolicyRecommendationForTest(t, fixture.svc, fixture.now, fixture.ownerID)
	assertPolicyApplyForTest(t, fixture.svc, fixture.store, fixture.now, fixture.ownerID)
	assertPolicyDryRunForTest(t, fixture.svc, fixture.now, fixture.ownerID)
	policyV2 := updateAssignmentPolicyForTest(t, fixture.svc, "admin-2", "tighten urgent SLA", 8, fixture.ownerID)
	assertPolicyRevisionStateForTest(t, fixture.svc, policyV2, []int{2, 1})
	assertPolicyRestoreForTest(t, fixture.svc, fixture.policyAudits)
}

type assignmentPolicyTestFixture struct {
	now          time.Time
	ownerID      string
	store        *recommendationStore
	policyStore  *recommendationPolicyStore
	policyAudits *recommendationPolicyAuditRepo
	svc          *Service
}

func newAssignmentPolicyTestFixture(t *testing.T) assignmentPolicyTestFixture {
	t.Helper()
	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	store := ptrext.Of(recommendationStore{candidates: map[int64]feedbackrepo.AssignmentCandidate{
		1: recommendationCandidate(1, now.Add(-2*time.Hour), candidateOpts{
			urgent:            true,
			workflowCategory:  "open",
			hasStableIdentity: true,
		}),
	}})
	policyStore := ptrext.Of(recommendationPolicyStore{values: map[string]string{}})
	policyAudits := ptrext.Of(recommendationPolicyAuditRepo{})
	svc := New(store, ptrext.Of(recommendationAuditWriter{}), ptrext.Of(recommendationTx{}))
	svc.SetPolicyStore(policyStore)
	svc.SetAuditLogger(auditlogsvc.New(policyAudits))
	return assignmentPolicyTestFixture{
		now:          now,
		ownerID:      "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		store:        store,
		policyStore:  policyStore,
		policyAudits: policyAudits,
		svc:          svc,
	}
}

func updateAssignmentPolicyForTest(
	t *testing.T,
	svc *Service,
	actorID string,
	note string,
	slaHours int,
	ownerID string,
) Policy {
	t.Helper()
	policy, err := svc.UpdatePolicy(context.Background(), UpdatePolicyInput{
		TenantID: "tenant-1",
		Actor:    auditlogsvc.Actor{Type: "user", ID: actorID},
		Note:     note,
		Rules:    []PolicyRule{urgentPolicyRuleForTest(ownerID, slaHours)},
	})
	if err != nil {
		t.Fatalf("UpdatePolicy(%s) error = %v", actorID, err)
	}
	return policy
}

func urgentPolicyRuleForTest(ownerID string, slaHours int) PolicyRule {
	return PolicyRule{
		RuleKey:              "urgent_open",
		OwnerLane:            "enterprise_triage",
		SLAHours:             slaHours,
		DefaultOwnerMemberID: ptrext.Of(ownerID),
		Enabled:              true,
	}
}

func assertPolicyV1ForTest(
	t *testing.T,
	policy Policy,
	policyStore *recommendationPolicyStore,
	policyAudits *recommendationPolicyAuditRepo,
) {
	t.Helper()
	if policy.Version != 1 || policy.UpdatedBy != "admin-1" || policy.Note != "enterprise lane" {
		t.Fatalf("policy metadata = %#v, want version 1 updated by admin-1", policy)
	}
	if len(policyAudits.entries) != 1 || policyAudits.entries[0].Action != "feedback_assignment.policy_update" {
		t.Fatalf("policy audit entries = %#v, want feedback_assignment.policy_update", policyAudits.entries)
	}
	if !policyStore.setTxCalled {
		t.Fatal("UpdatePolicy did not use transactional settings write")
	}
}

func assertPolicyRecommendationForTest(t *testing.T, svc *Service, now time.Time, ownerID string) {
	t.Helper()
	recs, err := svc.RecommendBatch(context.Background(), RecommendationInput{
		TenantID:    "tenant-1",
		FeedbackIDs: []int64{1},
		Now:         now,
	})
	if err != nil {
		t.Fatalf("RecommendBatch() error = %v", err)
	}
	rec := recs.Recommendations[0]
	if rec.OwnerLane != "enterprise_triage" || rec.SLAHours != 12 || ptrext.Indirect(rec.RecommendedOwnerMemberID) != ownerID {
		t.Fatalf("recommendation = %#v, want configured owner lane, SLA, and owner", rec)
	}
}

func assertPolicyApplyForTest(
	t *testing.T,
	svc *Service,
	store *recommendationStore,
	now time.Time,
	ownerID string,
) {
	t.Helper()
	applied, err := svc.ApplyRecommendations(context.Background(), ApplyRecommendationInput{
		TenantID:    "tenant-1",
		FeedbackIDs: []int64{1},
		ActorID:     "operator-1",
		Now:         now,
	})
	if err != nil {
		t.Fatalf("ApplyRecommendations() error = %v", err)
	}
	assertAppliedPolicyAssignmentForTest(t, applied, store.assignments[1], now, ownerID)
}

func assertAppliedPolicyAssignmentForTest(
	t *testing.T,
	applied ApplyRecommendationResult,
	assignment feedbackrepo.Assignment,
	now time.Time,
	ownerID string,
) {
	t.Helper()
	if applied.Succeeded != 1 {
		t.Fatalf("ApplyRecommendations().Succeeded = %d, want 1", applied.Succeeded)
	}
	if ptrext.Indirect(assignment.OwnerMemberID) != ownerID {
		t.Fatalf("assigned owner = %q, want policy owner %q", ptrext.Indirect(assignment.OwnerMemberID), ownerID)
	}
	if assignment.SLADueAt == nil || !assignment.SLADueAt.Equal(now.Add(10*time.Hour)) {
		t.Fatalf("assigned SLA = %v, want created_at + 12h", assignment.SLADueAt)
	}
	if assignment.Note != "Assignment policy: Urgent open feedback (enterprise_triage, 12h)." {
		t.Fatalf("assignment note = %q", assignment.Note)
	}
}

func assertPolicyDryRunForTest(t *testing.T, svc *Service, now time.Time, ownerID string) {
	t.Helper()
	dryRun, err := svc.DryRunPolicy(context.Background(), DryRunPolicyInput{
		TenantID:    "tenant-1",
		FeedbackIDs: []int64{1},
		Now:         now,
		Rules:       []PolicyRule{urgentPolicyRuleForTest(ownerID, 8)},
	})
	if err != nil {
		t.Fatalf("DryRunPolicy() error = %v", err)
	}
	if dryRun.Changed != 1 || len(dryRun.Impacts) != 1 {
		t.Fatalf("DryRunPolicy() = %#v, want one changed impact", dryRun)
	}
	impact := dryRun.Impacts[0]
	if impact.CurrentSLAHours != 12 || impact.DraftSLAHours != 8 || !impact.Changed {
		t.Fatalf("dry-run impact = %#v, want 12h -> 8h change", impact)
	}
}

func assertPolicyRevisionStateForTest(t *testing.T, svc *Service, policy Policy, versions []int) {
	t.Helper()
	if policy.Version != versions[0] {
		t.Fatalf("policy.Version = %d, want %d", policy.Version, versions[0])
	}
	revisions, err := svc.ListPolicyRevisions(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ListPolicyRevisions() error = %v", err)
	}
	for i, version := range versions {
		if revisions[i].Version != version {
			t.Fatalf("revisions = %#v, want version %d at index %d", revisions, version, i)
		}
	}
}

func assertPolicyRestoreForTest(t *testing.T, svc *Service, policyAudits *recommendationPolicyAuditRepo) {
	t.Helper()
	restored, err := svc.RestorePolicy(context.Background(), RestorePolicyInput{
		TenantID: "tenant-1",
		Version:  1,
		Actor:    auditlogsvc.Actor{Type: "user", ID: "admin-3"},
	})
	if err != nil {
		t.Fatalf("RestorePolicy() error = %v", err)
	}
	if restored.Version != 3 || restored.Rules[0].SLAHours != 12 {
		t.Fatalf("restored policy = %#v, want new version 3 with v1 SLA", restored)
	}
	if got := policyAudits.entries[len(policyAudits.entries)-1].Action; got != "feedback_assignment.policy_restore" {
		t.Fatalf("last policy audit action = %s, want feedback_assignment.policy_restore", got)
	}
}

type candidateOpts struct {
	urgent             bool
	enrichmentStatus   string
	enrichmentAttempts int
	workflowCategory   string
	hasStableIdentity  bool
	currentDueAt       *time.Time
}

func recommendationCandidate(
	id int64,
	createdAt time.Time,
	opts candidateOpts,
) feedbackrepo.AssignmentCandidate {
	status := opts.enrichmentStatus
	if status == "" {
		status = "done"
	}
	return feedbackrepo.AssignmentCandidate{
		ID:                 id,
		CreatedAt:          createdAt,
		Source:             "api",
		Type:               "bug",
		IsUrgent:           opts.urgent,
		EnrichmentStatus:   status,
		EnrichmentAttempts: opts.enrichmentAttempts,
		WorkflowCategory:   opts.workflowCategory,
		HasStableIdentity:  opts.hasStableIdentity,
		Assignment: feedbackrepo.Assignment{
			FeedbackID: id,
			SLADueAt:   opts.currentDueAt,
		},
	}
}

type recommendationStore struct {
	candidates   map[int64]feedbackrepo.AssignmentCandidate
	assignments  map[int64]feedbackrepo.Assignment
	invalidOwner string
}

func (s *recommendationStore) AssignmentCandidates(
	_ context.Context,
	_ string,
	ids []int64,
) ([]feedbackrepo.AssignmentCandidate, error) {
	out := make([]feedbackrepo.AssignmentCandidate, 0, len(ids))
	for _, id := range ids {
		candidate, ok := s.candidates[id]
		if ok {
			out = append(out, candidate)
		}
	}
	return out, nil
}

func (s *recommendationStore) AssignmentForUpdate(
	_ context.Context,
	_ pgx.Tx,
	_ string,
	feedbackID int64,
) (feedbackrepo.Assignment, error) {
	if s.assignments != nil {
		if assignment, ok := s.assignments[feedbackID]; ok {
			return assignment, nil
		}
	}
	candidate, ok := s.candidates[feedbackID]
	if !ok {
		return feedbackrepo.Assignment{}, feedbackrepo.ErrFeedbackNotFound
	}
	return candidate.Assignment, nil
}

func (s *recommendationStore) AssignFeedbackTx(
	_ context.Context,
	_ pgx.Tx,
	_ string,
	feedbackID int64,
	input feedbackrepo.AssignmentInput,
) (feedbackrepo.Assignment, error) {
	before, err := s.AssignmentForUpdate(context.Background(), nil, "", feedbackID)
	if err != nil {
		return feedbackrepo.Assignment{}, err
	}
	after := before
	if input.OwnerMemberIDSet {
		after.OwnerMemberID = input.OwnerMemberID
	}
	if input.SLADueAtSet {
		after.SLADueAt = input.SLADueAt
	}
	after.Note = input.Note
	after.AssignedBy = input.ActorID
	if s.assignments == nil {
		s.assignments = make(map[int64]feedbackrepo.Assignment)
	}
	s.assignments[feedbackID] = after
	return after, nil
}

func (s *recommendationStore) ValidateAssignmentOwner(
	_ context.Context,
	_ string,
	ownerMemberID string,
) error {
	if ownerMemberID == s.invalidOwner {
		return feedbackrepo.ErrAssignmentOwnerNotFound
	}
	return nil
}

type recommendationAuditWriter struct {
	entries []feedbackaudit.Entry
}

func (w *recommendationAuditWriter) WriteTx(_ context.Context, _ pgx.Tx, e feedbackaudit.Entry) error {
	w.entries = append(w.entries, e)
	return nil
}

type recommendationPolicyStore struct {
	values      map[string]string
	updatedBy   string
	setTxCalled bool
}

func (s *recommendationPolicyStore) Get(_ context.Context, _ string, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", systemsettings.ErrNotFound
	}
	return value, nil
}

func (s *recommendationPolicyStore) Set(_ context.Context, _ string, key string, value string, updatedBy string) error {
	s.values[key] = value
	s.updatedBy = updatedBy
	return nil
}

func (s *recommendationPolicyStore) SetTx(_ context.Context, _ pgx.Tx, _ string, key string, value string, updatedBy string) error {
	s.setTxCalled = true
	s.values[key] = value
	s.updatedBy = updatedBy
	return nil
}

type recommendationPolicyAuditRepo struct {
	entries []auditlogrepo.Entry
}

func (r *recommendationPolicyAuditRepo) Insert(_ context.Context, entry auditlogrepo.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func (r *recommendationPolicyAuditRepo) InsertTx(_ context.Context, _ pgx.Tx, entry auditlogrepo.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func (r *recommendationPolicyAuditRepo) List(
	context.Context,
	auditlogrepo.ListFilter,
) (auditlogrepo.ListResult, error) {
	return auditlogrepo.ListResult{}, nil
}

func (r *recommendationPolicyAuditRepo) PruneBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

type recommendationTx struct{}

func (tx *recommendationTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *recommendationTx) Commit(context.Context) error          { return nil }
func (tx *recommendationTx) Rollback(context.Context) error        { return nil }
func (tx *recommendationTx) Conn() *pgx.Conn                       { return nil }
func (tx *recommendationTx) LargeObjects() pgx.LargeObjects        { return pgx.LargeObjects{} }
func (tx *recommendationTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (tx *recommendationTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (tx *recommendationTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *recommendationTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (tx *recommendationTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (tx *recommendationTx) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }
