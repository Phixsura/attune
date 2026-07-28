// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-mock-fixtures

package cohortsync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/cohortsync"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	repo "github.com/Phixsura/attune/internal/repo/cohortsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// ---------- mocks ----------

type mockStore struct {
	keyID string
}

func (m mockStore) EncryptValue(plaintext, _ []byte) (secretstore.EncryptedValue, error) {
	return secretstore.EncryptedValue{KeyID: m.keyID, Ciphertext: plaintext}, nil
}

func (m mockStore) DecryptValue(value secretstore.EncryptedValue, _ []byte) ([]byte, error) {
	return value.Ciphertext, nil
}

type mockRepo struct {
	sources      map[uuid.UUID]*repo.Source
	cohorts      map[uuid.UUID]*repo.Cohort
	runs         map[uuid.UUID]*repo.SyncRun
	events       []repo.SyncEvent
	members      []repo.Membership
	memberAdded  int
	hasRunning   bool
	recentRuns   int
	testResultOK *bool
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		sources: make(map[uuid.UUID]*repo.Source),
		cohorts: make(map[uuid.UUID]*repo.Cohort),
		runs:    make(map[uuid.UUID]*repo.SyncRun),
	}
}

func (m *mockRepo) CreateSource(_ context.Context, in repo.Source) (*repo.Source, error) {
	row := in
	m.sources[row.ID] = &row
	return &row, nil
}

func (m *mockRepo) GetSource(_ context.Context, _ string, id uuid.UUID) (*repo.Source, error) {
	if s, ok := m.sources[id]; ok {
		return s, nil
	}
	return nil, repo.ErrSourceNotFound
}

func (m *mockRepo) ListSources(_ context.Context, _ string) ([]repo.Source, error) {
	out := make([]repo.Source, 0, len(m.sources))
	for _, s := range m.sources {
		out = append(out, *s)
	}
	return out, nil
}

func (m *mockRepo) UpdateSource(_ context.Context, in repo.Source) (*repo.Source, error) {
	row := in
	m.sources[row.ID] = &row
	return &row, nil
}

func (m *mockRepo) DeleteSource(_ context.Context, _ string, id uuid.UUID) error {
	if _, ok := m.sources[id]; !ok {
		return repo.ErrSourceNotFound
	}
	delete(m.sources, id)
	return nil
}

func (m *mockRepo) UpdateSourceSyncStatus(_ context.Context, _ string, _ uuid.UUID, _ string) error {
	return nil
}

func (m *mockRepo) UpsertCohort(_ context.Context, in repo.Cohort) (*repo.Cohort, error) {
	row := in
	m.cohorts[row.ID] = &row
	return &row, nil
}

func (m *mockRepo) GetCohort(_ context.Context, _ string, id uuid.UUID) (*repo.Cohort, error) {
	if c, ok := m.cohorts[id]; ok {
		return c, nil
	}
	return nil, repo.ErrCohortNotFound
}

func (m *mockRepo) GetCohortByExternalID(_ context.Context, _ string, _ uuid.UUID, extID string) (*repo.Cohort, error) {
	for _, c := range m.cohorts {
		if c.ExternalCohortID == extID {
			return c, nil
		}
	}
	return nil, repo.ErrCohortNotFound
}

func (m *mockRepo) ListCohorts(_ context.Context, _ string, sourceID uuid.UUID) ([]repo.Cohort, error) {
	var out []repo.Cohort
	for _, c := range m.cohorts {
		if c.CohortSourceID == sourceID {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (m *mockRepo) ListAllCohorts(_ context.Context, _ string) ([]repo.Cohort, error) {
	out := make([]repo.Cohort, 0, len(m.cohorts))
	for _, c := range m.cohorts {
		out = append(out, *c)
	}
	return out, nil
}

func (m *mockRepo) UpdateCohort(_ context.Context, in repo.Cohort) (*repo.Cohort, error) {
	row := in
	m.cohorts[row.ID] = &row
	return &row, nil
}

func (m *mockRepo) UpdateCohortSyncResult(_ context.Context, _ string, _ uuid.UUID, _ int, _ string) error {
	return nil
}

func (m *mockRepo) UpsertMemberships(_ context.Context, _ string, _ uuid.UUID, members []repo.MembershipUpsert) (int, error) {
	m.memberAdded += len(members)
	return len(members), nil
}

func (m *mockRepo) MarkDeparted(_ context.Context, _ string, _ uuid.UUID, _ int, _ time.Time) (int64, error) {
	return 0, nil
}

func (m *mockRepo) MarkMembersDeparted(_ context.Context, _ string, _ uuid.UUID, _ int, ids []string) (int64, error) {
	return int64(len(ids)), nil
}

func (m *mockRepo) CleanExpired(_ context.Context) (int64, error) { return 0, nil }

func (m *mockRepo) RecoverStaleRuns(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

func (m *mockRepo) CountActiveMembers(_ context.Context, _ string, _ uuid.UUID) (int, error) {
	return m.memberAdded, nil
}

func (m *mockRepo) InsertRun(_ context.Context, run repo.SyncRun) (*repo.SyncRun, error) {
	row := run
	m.runs[row.ID] = &row
	return &row, nil
}

func (m *mockRepo) InsertExclusiveRun(_ context.Context, run repo.SyncRun) (*repo.SyncRun, error) {
	if m.hasRunning {
		return nil, repo.ErrConflict
	}
	row := run
	m.runs[row.ID] = &row
	return &row, nil
}

func (m *mockRepo) ApplyMembershipDelta(_ context.Context, in repo.ApplyInput) (repo.ApplyResult, error) {
	if m.hasRunning && in.IsSnapshot {
		return repo.ApplyResult{}, repo.ErrConflict
	}
	m.memberAdded += len(in.Members)
	return repo.ApplyResult{
		MembersAdded: len(in.Members),
		Removed:      int64(len(in.RemoveIDs)),
		MemberCount:  m.memberAdded,
	}, nil
}

func (m *mockRepo) FinishRun(_ context.Context, id uuid.UUID, status string, added, removed, total int, msg string) error {
	if r, ok := m.runs[id]; ok {
		r.Status = status
		r.MembersAdded = added
		r.MembersRemoved = removed
		r.MembersTotal = total
		r.ErrorMessage = msg
		return nil
	}
	return repo.ErrRunNotFound
}

func (m *mockRepo) ListRuns(_ context.Context, _ string, _ uuid.UUID, _ int) ([]repo.SyncRun, error) {
	return nil, nil
}

func (m *mockRepo) ListRunsPaginated(_ context.Context, _ string, cohortID uuid.UUID, limit int, cursor string) (repo.ListRunsResult, error) {
	var runs []repo.SyncRun
	for _, r := range m.runs {
		if r.CohortID == cohortID {
			runs = append(runs, *r)
		}
	}
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	next := ""
	if cursor != "" {
		next = "next-cursor"
	}
	return repo.ListRunsResult{Runs: runs, NextCursor: next}, nil
}

func (m *mockRepo) UpdateTestResult(_ context.Context, _ string, _ uuid.UUID, ok bool) error {
	m.testResultOK = &ok // ptrext:allow test-mock-capture
	return nil
}

func (m *mockRepo) HasRunningRun(_ context.Context, _ string, _ uuid.UUID) (bool, error) {
	return m.hasRunning, nil
}

func (m *mockRepo) HasRunningRunForSource(_ context.Context, _ string, _ uuid.UUID) (bool, error) {
	return m.hasRunning, nil
}

func (m *mockRepo) CountRecentRuns(_ context.Context, _ string, _ time.Duration) (int, error) {
	return m.recentRuns, nil
}

func (m *mockRepo) RecordEvent(_ context.Context, in repo.SyncEvent) (*repo.SyncEvent, error) {
	row := in
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	return &row, nil
}

func (m *mockRepo) UpdateEventStatus(_ context.Context, _ uuid.UUID, _ string, _ *uuid.UUID, _ string) error {
	return nil
}

func (m *mockRepo) ListEvents(_ context.Context, _ string, sourceID uuid.UUID, limit int) ([]repo.SyncEvent, error) {
	var out []repo.SyncEvent
	for _, e := range m.events {
		if e.CohortSourceID == sourceID {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *mockRepo) ListMembers(_ context.Context, _ string, cohortID uuid.UUID, limit int) ([]repo.Membership, error) {
	var out []repo.Membership
	for _, mb := range m.members {
		if mb.CohortID == cohortID {
			out = append(out, mb)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- tests ----------

func TestCreateSource_EncryptsCredential(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "key-1"})

	src, err := svc.CreateSource(context.Background(), CreateSourceInput{
		TenantID:   "t1",
		Provider:   "amplitude",
		Name:       "My Amplitude",
		AuthType:   "api_key",
		Credential: "secret-cred",
		Enabled:    true,
		Actor:      Actor{ID: "admin-1"},
		AuditActor: auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("CreateSource failed: %v", err)
	}
	if src.CredentialKeyID != "key-1" {
		t.Errorf("CredentialKeyID = %q, want key-1", src.CredentialKeyID)
	}
	if len(src.CredentialCiphertext) == 0 {
		t.Error("CredentialCiphertext is empty")
	}
}

func TestCreateSource_RejectsInvalidProvider(t *testing.T) {
	svc := New(newMockRepo(), mockStore{keyID: "k"})
	_, err := svc.CreateSource(context.Background(), CreateSourceInput{
		TenantID:   "t1",
		Provider:   "has space",
		Name:       "X",
		AuthType:   "api_key",
		Credential: "c",
		Actor:      Actor{ID: "a"},
	})
	if err == nil {
		t.Fatal("expected validation error for invalid provider token")
	}
}

func TestCreateSource_RejectsEmptyCredential(t *testing.T) {
	svc := New(newMockRepo(), mockStore{keyID: "k"})
	_, err := svc.CreateSource(context.Background(), CreateSourceInput{
		TenantID: "t1",
		Provider: "amplitude",
		Name:     "X",
		AuthType: "api_key",
		Actor:    Actor{ID: "a"},
	})
	if err == nil {
		t.Fatal("expected validation error for empty credential")
	}
}

func TestApplyDelta_AddsMembers(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	// Seed source
	sourceID := uuid.New()
	mr.sources[sourceID] = &repo.Source{
		ID: sourceID, TenantID: "t1", Provider: "amplitude", Enabled: true,
	}

	result, err := svc.ApplyDelta(context.Background(), "t1", sourceID, cohortsync.SyncPayload{
		Provider:         "amplitude",
		ExternalCohortID: "cohort-1",
		CohortName:       "Power Users",
		Deltas: []cohortsync.MemberDelta{
			{ExternalUserID: "u1", Email: "u1@test.com", Action: "add"},
			{ExternalUserID: "u2", Email: "u2@test.com", Action: "add"},
		},
	})
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}
	if result.Added != 2 {
		t.Errorf("Added = %d, want 2", result.Added)
	}
	if result.Cohort.Name != "Power Users" {
		t.Errorf("Cohort.Name = %q, want Power Users", result.Cohort.Name)
	}
}

func TestApplyDelta_SkipsDisabledSource(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	sourceID := uuid.New()
	mr.sources[sourceID] = &repo.Source{
		ID: sourceID, TenantID: "t1", Provider: "amplitude", Enabled: false,
	}

	result, err := svc.ApplyDelta(context.Background(), "t1", sourceID, cohortsync.SyncPayload{
		ExternalCohortID: "cohort-1",
		CohortName:       "C",
		Deltas:           []cohortsync.MemberDelta{{ExternalUserID: "u1", Action: "add"}},
	})
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}
	// Run should be recorded as skipped
	for _, run := range mr.runs {
		if run.Status != "skipped" {
			t.Errorf("expected skipped run, got status=%q", run.Status)
		}
	}
	_ = result
}

func TestApplyDelta_RemoveMembers(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	sourceID := uuid.New()
	mr.sources[sourceID] = &repo.Source{
		ID: sourceID, TenantID: "t1", Provider: "amplitude", Enabled: true,
	}

	// First add members.
	_, err := svc.ApplyDelta(context.Background(), "t1", sourceID, cohortsync.SyncPayload{
		ExternalCohortID: "cohort-1",
		CohortName:       "C",
		Deltas: []cohortsync.MemberDelta{
			{ExternalUserID: "u1", Action: "add"},
			{ExternalUserID: "u2", Action: "add"},
		},
	})
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	// Then remove one member.
	result, err := svc.ApplyDelta(context.Background(), "t1", sourceID, cohortsync.SyncPayload{
		ExternalCohortID: "cohort-1",
		CohortName:       "C",
		Deltas: []cohortsync.MemberDelta{
			{ExternalUserID: "u1", Action: "remove"},
		},
	})
	if err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("Removed = %d, want 1", result.Removed)
	}
}

func TestApplyFullSnapshot(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	sourceID := uuid.New()
	mr.sources[sourceID] = &repo.Source{
		ID: sourceID, TenantID: "t1", Provider: "mixpanel", Enabled: true,
	}

	result, err := svc.ApplyFullSnapshot(context.Background(), "t1", sourceID, cohortsync.SyncPayload{
		ExternalCohortID: "enterprise",
		CohortName:       "Enterprise",
		IsFullSnapshot:   true,
		Deltas: []cohortsync.MemberDelta{
			{ExternalUserID: "u1", Action: "add"},
			{ExternalUserID: "u2", Action: "add"},
		},
	}, "webhook")
	if err != nil {
		t.Fatalf("ApplyFullSnapshot failed: %v", err)
	}
	if result.Added != 2 {
		t.Errorf("Added = %d, want 2", result.Added)
	}
	if result.Cohort.Name != "Enterprise" {
		t.Errorf("Cohort.Name = %q, want Enterprise", result.Cohort.Name)
	}
}

func TestApplyFullSnapshot_SkipsDisabledCohort(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	sourceID := uuid.New()
	mr.sources[sourceID] = &repo.Source{
		ID: sourceID, TenantID: "t1", Provider: "mixpanel", Enabled: true,
	}

	// Pre-seed a disabled cohort.
	cohortID := uuid.New()
	mr.cohorts[cohortID] = &repo.Cohort{
		ID: cohortID, TenantID: "t1", CohortSourceID: sourceID,
		ExternalCohortID: "disabled-cohort", Name: "Disabled", Enabled: false,
	}

	result, err := svc.ApplyFullSnapshot(context.Background(), "t1", sourceID, cohortsync.SyncPayload{
		ExternalCohortID: "disabled-cohort",
		CohortName:       "Disabled",
		IsFullSnapshot:   true,
		Deltas:           []cohortsync.MemberDelta{{ExternalUserID: "u1", Action: "add"}},
	}, "webhook")
	if err != nil {
		t.Fatalf("ApplyFullSnapshot failed: %v", err)
	}
	// Should be skipped.
	for _, run := range mr.runs {
		if run.Status != "skipped" {
			t.Errorf("expected skipped run, got status=%q", run.Status)
		}
	}
	_ = result
}

func TestApplyFullSnapshot_RejectsRunning(t *testing.T) {
	mr := newMockRepo()
	mr.hasRunning = true
	svc := New(mr, mockStore{keyID: "k"})

	sourceID := uuid.New()
	mr.sources[sourceID] = &repo.Source{
		ID: sourceID, TenantID: "t1", Provider: "mixpanel", Enabled: true,
	}

	_, err := svc.ApplyFullSnapshot(context.Background(), "t1", sourceID, cohortsync.SyncPayload{
		ExternalCohortID: "c1",
		CohortName:       "C",
		IsFullSnapshot:   true,
		Deltas:           []cohortsync.MemberDelta{{ExternalUserID: "u1", Action: "add"}},
	}, "webhook")
	if err == nil {
		t.Fatal("expected conflict error for running sync")
	}
}

func TestSplitDeltas(t *testing.T) {
	deltas := []cohortsync.MemberDelta{
		{ExternalUserID: "u1", Action: "add"},
		{ExternalUserID: "u2", Action: "remove"},
		{ExternalUserID: "u3", Action: "add"},
		{ExternalUserID: "u4", Action: "remove"},
	}
	adds, removes := splitDeltas(deltas)
	if len(adds) != 2 {
		t.Errorf("adds = %d, want 2", len(adds))
	}
	if len(removes) != 2 {
		t.Errorf("removes = %d, want 2", len(removes))
	}
}

func TestValidateSourceShape(t *testing.T) {
	if err := validateSourceShape("amplitude", "Name", "api_key"); err != nil {
		t.Errorf("valid input rejected: %v", err)
	}
	if err := validateSourceShape("INVALID", "Name", "api_key"); err == nil {
		t.Error("invalid provider accepted")
	}
	if err := validateSourceShape("amplitude", "", "api_key"); err == nil {
		t.Error("empty name accepted")
	}
	if err := validateSourceShape("amplitude", "Name", "oauth"); err == nil {
		t.Error("invalid auth_type accepted")
	}
}

func TestNormalizeJSONObject(t *testing.T) {
	out, err := normalizeJSONObject("")
	if err != nil || out != "{}" {
		t.Errorf("empty → %q, %v", out, err)
	}
	out, err = normalizeJSONObject(`{"key":"val"}`)
	if err != nil || out != `{"key":"val"}` {
		t.Errorf("valid → %q, %v", out, err)
	}
	_, err = normalizeJSONObject(`[1,2,3]`)
	if err == nil {
		t.Error("array accepted as object")
	}
	_, err = normalizeJSONObject(`not json`)
	if err == nil {
		t.Error("invalid json accepted")
	}
}

// ---------- new tests ----------

// mockProvider implements cohortsync.Provider for TestSource tests.
type mockProvider struct {
	checkResult cohortsync.CheckResult
	checkErr    error
	pullPayload cohortsync.SyncPayload
	pullErr     error
}

func (p *mockProvider) Provider() string { return "mockprov" }
func (p *mockProvider) Check(_ context.Context, _ cohortsync.Connection) (cohortsync.CheckResult, error) {
	return p.checkResult, p.checkErr
}

func (p *mockProvider) ParseWebhook(_ []byte, _ map[string]string, _ []byte) (cohortsync.SyncPayload, error) {
	return cohortsync.SyncPayload{}, nil
}

func (p *mockProvider) PullCohort(_ context.Context, _ cohortsync.Connection, _ string) (cohortsync.SyncPayload, error) {
	return p.pullPayload, p.pullErr
}

// mockAudit records audit events for assertion.
type mockAudit struct {
	events []auditlogsvc.Event
}

func (a *mockAudit) Record(_ context.Context, e auditlogsvc.Event) error {
	a.events = append(a.events, e)
	return nil
}

// --- GetSource ---

func TestGetSource_Found(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{ID: id, TenantID: "t1", Provider: "amplitude", Name: "S1"}

	src, err := svc.GetSource(context.Background(), "t1", id)
	if err != nil {
		t.Fatalf("GetSource failed: %v", err)
	}
	if src.Name != "S1" {
		t.Errorf("Name = %q, want S1", src.Name)
	}
}

func TestGetSource_NotFound(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	_, err := svc.GetSource(context.Background(), "t1", uuid.New())
	if !errors.Is(err, repo.ErrSourceNotFound) {
		t.Fatalf("expected ErrSourceNotFound, got %v", err)
	}
}

// --- GetCohort ---

func TestGetCohort_Found(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.cohorts[id] = &repo.Cohort{ID: id, TenantID: "t1", Name: "Beta Users"}

	c, err := svc.GetCohort(context.Background(), "t1", id)
	if err != nil {
		t.Fatalf("GetCohort failed: %v", err)
	}
	if c.Name != "Beta Users" {
		t.Errorf("Name = %q, want Beta Users", c.Name)
	}
}

func TestGetCohort_NotFound(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	_, err := svc.GetCohort(context.Background(), "t1", uuid.New())
	if !errors.Is(err, repo.ErrCohortNotFound) {
		t.Fatalf("expected ErrCohortNotFound, got %v", err)
	}
}

// --- ListMembers ---

func TestListMembers_ReturnsCohortMembers(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	cohortID := uuid.New()
	otherID := uuid.New()
	mr.members = []repo.Membership{
		{ID: uuid.New(), CohortID: cohortID, ExternalUserID: "u1"},
		{ID: uuid.New(), CohortID: cohortID, ExternalUserID: "u2"},
		{ID: uuid.New(), CohortID: otherID, ExternalUserID: "u3"},
	}

	members, err := svc.ListMembers(context.Background(), "t1", cohortID, 100)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("len(members) = %d, want 2", len(members))
	}
}

func TestListMembers_Empty(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	members, err := svc.ListMembers(context.Background(), "t1", uuid.New(), 100)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("expected empty members, got %d", len(members))
	}
}

func TestListMembers_RespectsLimit(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	cohortID := uuid.New()
	for i := 0; i < 5; i++ {
		mr.members = append(mr.members, repo.Membership{
			ID: uuid.New(), CohortID: cohortID, ExternalUserID: "u",
		})
	}

	members, err := svc.ListMembers(context.Background(), "t1", cohortID, 3)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(members) != 3 {
		t.Errorf("len(members) = %d, want 3", len(members))
	}
}

// --- ListEvents ---

func TestListEvents_ReturnsSourceEvents(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	sourceID := uuid.New()
	otherID := uuid.New()
	mr.events = []repo.SyncEvent{
		{ID: uuid.New(), CohortSourceID: sourceID, EventType: "cohort_delta"},
		{ID: uuid.New(), CohortSourceID: sourceID, EventType: "cohort_members"},
		{ID: uuid.New(), CohortSourceID: otherID, EventType: "cohort_delta"},
	}

	events, err := svc.ListEvents(context.Background(), "t1", sourceID, 100)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("len(events) = %d, want 2", len(events))
	}
}

func TestListEvents_Empty(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	events, err := svc.ListEvents(context.Background(), "t1", uuid.New(), 100)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty events, got %d", len(events))
	}
}

// --- ListRunsPaginated ---

func TestListRunsPaginated_ReturnsCohortRuns(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	cohortID := uuid.New()
	otherCohortID := uuid.New()
	r1 := uuid.New()
	r2 := uuid.New()
	r3 := uuid.New()
	mr.runs[r1] = &repo.SyncRun{ID: r1, CohortID: cohortID, Status: "succeeded"}
	mr.runs[r2] = &repo.SyncRun{ID: r2, CohortID: cohortID, Status: "failed"}
	mr.runs[r3] = &repo.SyncRun{ID: r3, CohortID: otherCohortID, Status: "succeeded"}

	result, err := svc.ListRunsPaginated(context.Background(), "t1", cohortID, 100, "")
	if err != nil {
		t.Fatalf("ListRunsPaginated failed: %v", err)
	}
	if len(result.Runs) != 2 {
		t.Errorf("len(Runs) = %d, want 2", len(result.Runs))
	}
}

func TestListRunsPaginated_WithCursor(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	cohortID := uuid.New()
	r1 := uuid.New()
	mr.runs[r1] = &repo.SyncRun{ID: r1, CohortID: cohortID, Status: "succeeded"}

	result, err := svc.ListRunsPaginated(context.Background(), "t1", cohortID, 100, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("ListRunsPaginated failed: %v", err)
	}
	if result.NextCursor == "" {
		t.Error("expected non-empty NextCursor when cursor was provided")
	}
}

// --- Health ---

func TestHealth_SourceCounting(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	s1 := uuid.New()
	s2 := uuid.New()
	s3 := uuid.New()
	mr.sources[s1] = &repo.Source{ID: s1, TenantID: "t1", Status: "active", Enabled: true}
	mr.sources[s2] = &repo.Source{ID: s2, TenantID: "t1", Status: "error", Enabled: true}
	mr.sources[s3] = &repo.Source{ID: s3, TenantID: "t1", Status: "disabled", Enabled: false}

	h, err := svc.Health(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if h.SourceCount != 3 {
		t.Errorf("SourceCount = %d, want 3", h.SourceCount)
	}
	if h.ActiveSources != 1 {
		t.Errorf("ActiveSources = %d, want 1", h.ActiveSources)
	}
	if h.ErrorSources != 1 {
		t.Errorf("ErrorSources = %d, want 1", h.ErrorSources)
	}
	if h.DisabledSources != 1 {
		t.Errorf("DisabledSources = %d, want 1", h.DisabledSources)
	}
}

func TestHealth_LastSyncAtPicksLatest(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	s1 := uuid.New()
	s2 := uuid.New()
	mr.sources[s1] = &repo.Source{ID: s1, TenantID: "t1", Status: "active", LastSyncAt: &early} // ptrext:allow test-fixture
	mr.sources[s2] = &repo.Source{ID: s2, TenantID: "t1", Status: "active", LastSyncAt: &late}  // ptrext:allow test-fixture

	h, err := svc.Health(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if h.LastSyncAt == nil {
		t.Fatal("LastSyncAt is nil")
	}
	if !h.LastSyncAt.Equal(late) {
		t.Errorf("LastSyncAt = %v, want %v", h.LastSyncAt, late)
	}
}

func TestHealth_NilLastSyncAt(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	s1 := uuid.New()
	mr.sources[s1] = &repo.Source{ID: s1, TenantID: "t1", Status: "active"}

	h, err := svc.Health(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if h.LastSyncAt != nil {
		t.Errorf("expected nil LastSyncAt, got %v", h.LastSyncAt)
	}
}

func TestHealth_CohortMemberCounting(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	c1 := uuid.New()
	c2 := uuid.New()
	mr.cohorts[c1] = &repo.Cohort{ID: c1, TenantID: "t1", MemberCount: 50}
	mr.cohorts[c2] = &repo.Cohort{ID: c2, TenantID: "t1", MemberCount: 30}

	h, err := svc.Health(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if h.CohortCount != 2 {
		t.Errorf("CohortCount = %d, want 2", h.CohortCount)
	}
	if h.TotalActiveMembers != 80 {
		t.Errorf("TotalActiveMembers = %d, want 80", h.TotalActiveMembers)
	}
}

func TestHealth_RecentRuns(t *testing.T) {
	mr := newMockRepo()
	mr.recentRuns = 7
	svc := New(mr, mockStore{keyID: "k"})

	h, err := svc.Health(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if h.SyncsLast24h != 7 {
		t.Errorf("SyncsLast24h = %d, want 7", h.SyncsLast24h)
	}
}

// --- DeleteSource ---

func TestDeleteSource_Success(t *testing.T) {
	mr := newMockRepo()
	audit := &mockAudit{}
	svc := New(mr, mockStore{keyID: "k"})
	svc.SetAuditLogger(audit)

	id := uuid.New()
	mr.sources[id] = &repo.Source{ID: id, TenantID: "t1", Provider: "amplitude", Name: "S"}

	err := svc.DeleteSource(context.Background(), "t1", id,
		Actor{Type: "admin", ID: "admin-1"},
		auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("DeleteSource failed: %v", err)
	}
	if _, exists := mr.sources[id]; exists {
		t.Error("source still exists after delete")
	}
	if len(audit.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(audit.events))
	}
	if audit.events[0].Action != "cohort_source.delete" {
		t.Errorf("audit action = %q, want cohort_source.delete", audit.events[0].Action)
	}
}

func TestDeleteSource_NotFound(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	err := svc.DeleteSource(context.Background(), "t1", uuid.New(),
		Actor{ID: "a"}, auditlogsvc.Actor{Type: "admin", ID: "a"})
	if !errors.Is(err, repo.ErrSourceNotFound) {
		t.Fatalf("expected ErrSourceNotFound, got %v", err)
	}
}

func TestDeleteSource_BlockedByRunningRun(t *testing.T) {
	mr := newMockRepo()
	mr.hasRunning = true
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{ID: id, TenantID: "t1", Provider: "amplitude", Name: "S"}

	err := svc.DeleteSource(context.Background(), "t1", id,
		Actor{ID: "a"}, auditlogsvc.Actor{Type: "admin", ID: "a"})
	if !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	// Source should not be deleted.
	if _, exists := mr.sources[id]; !exists {
		t.Error("source was deleted despite running run")
	}
}

// --- UpdateSource ---

func TestUpdateSource_NameUpdate(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "amplitude",
		Name: "Old Name", Enabled: true, Status: "active",
	}

	newName := "New Name"
	updated, err := svc.UpdateSource(context.Background(), UpdateSourceInput{
		TenantID:   "t1",
		ID:         id,
		Name:       &newName, // ptrext:allow test-fixture
		Actor:      Actor{ID: "admin-1"},
		AuditActor: auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("UpdateSource failed: %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("Name = %q, want New Name", updated.Name)
	}
}

func TestUpdateSource_EmptyNameRejected(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "amplitude",
		Name: "Old", Enabled: true, Status: "active",
	}

	empty := ""
	_, err := svc.UpdateSource(context.Background(), UpdateSourceInput{
		TenantID: "t1",
		ID:       id,
		Name:     &empty, // ptrext:allow test-fixture
		Actor:    Actor{ID: "a"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestUpdateSource_CredentialReEncryption(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "key-2"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "amplitude",
		Name: "S", Enabled: true, Status: "active",
		CredentialKeyID: "key-1", CredentialCiphertext: []byte("old-cred"),
	}

	newCred := "new-secret"
	updated, err := svc.UpdateSource(context.Background(), UpdateSourceInput{
		TenantID:   "t1",
		ID:         id,
		Credential: &newCred, // ptrext:allow test-fixture
		Actor:      Actor{ID: "admin-1"},
		AuditActor: auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("UpdateSource failed: %v", err)
	}
	if updated.CredentialKeyID != "key-2" {
		t.Errorf("CredentialKeyID = %q, want key-2", updated.CredentialKeyID)
	}
	if string(updated.CredentialCiphertext) != "new-secret" {
		t.Errorf("CredentialCiphertext = %q, want new-secret", string(updated.CredentialCiphertext))
	}
}

func TestUpdateSource_EmptyCredentialRejected(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "amplitude",
		Name: "S", Enabled: true, Status: "active",
	}

	empty := ""
	_, err := svc.UpdateSource(context.Background(), UpdateSourceInput{
		TenantID:   "t1",
		ID:         id,
		Credential: &empty, // ptrext:allow test-fixture
		Actor:      Actor{ID: "a"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestUpdateSource_EnabledTogglesStatus(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "amplitude",
		Name: "S", Enabled: true, Status: "active",
	}

	disabled := false
	updated, err := svc.UpdateSource(context.Background(), UpdateSourceInput{
		TenantID: "t1",
		ID:       id,
		Enabled:  &disabled, // ptrext:allow test-fixture
		Actor:    Actor{ID: "a"},
	})
	if err != nil {
		t.Fatalf("UpdateSource failed: %v", err)
	}
	if updated.Enabled {
		t.Error("expected Enabled=false")
	}
	if updated.Status != "disabled" {
		t.Errorf("Status = %q, want disabled", updated.Status)
	}
}

func TestUpdateSource_NotFound(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	name := "X"
	_, err := svc.UpdateSource(context.Background(), UpdateSourceInput{
		TenantID: "t1",
		ID:       uuid.New(),
		Name:     &name, // ptrext:allow test-fixture
		Actor:    Actor{ID: "a"},
	})
	if !errors.Is(err, repo.ErrSourceNotFound) {
		t.Fatalf("expected ErrSourceNotFound, got %v", err)
	}
}

func TestUpdateSource_PullCredentialClear(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "amplitude",
		Name: "S", Enabled: true, Status: "active",
		PullCredentialKeyID: "old-key", PullCredentialCiphertext: []byte("old"),
	}

	empty := ""
	updated, err := svc.UpdateSource(context.Background(), UpdateSourceInput{
		TenantID:       "t1",
		ID:             id,
		PullCredential: &empty, // ptrext:allow test-fixture
		Actor:          Actor{ID: "a"},
	})
	if err != nil {
		t.Fatalf("UpdateSource failed: %v", err)
	}
	if updated.PullCredentialKeyID != "" {
		t.Errorf("PullCredentialKeyID = %q, want empty", updated.PullCredentialKeyID)
	}
	if len(updated.PullCredentialCiphertext) != 0 {
		t.Error("PullCredentialCiphertext should be cleared")
	}
}

func TestUpdateSource_ProviderConfigJSON(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "amplitude",
		Name: "S", Enabled: true, Status: "active",
		ProviderConfig: []byte("{}"),
	}

	invalidJSON := "[1,2]"
	_, err := svc.UpdateSource(context.Background(), UpdateSourceInput{
		TenantID:       "t1",
		ID:             id,
		ProviderConfig: &invalidJSON, // ptrext:allow test-fixture
		Actor:          Actor{ID: "a"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for non-object JSON, got %v", err)
	}
}

// --- TestSource ---

func TestTestSource_ProviderNotRegistered(t *testing.T) {
	cohortsync.ResetForTest()
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "nonexistent",
		Name: "S", PullCredentialKeyID: "k", PullCredentialCiphertext: []byte("c"),
	}

	result, err := svc.TestSource(context.Background(), "t1", id, auditlogsvc.Actor{Type: "admin", ID: "a"})
	if err == nil {
		t.Fatal("expected error for unregistered provider")
	}
	if !cohortsync.IsUnavailableError(err) {
		t.Errorf("expected UnavailableError, got %v", err)
	}
	if result.OK {
		t.Error("result.OK should be false for unregistered provider")
	}
}

func TestTestSource_CheckOK(t *testing.T) {
	cohortsync.ResetForTest()
	prov := &mockProvider{checkResult: cohortsync.CheckResult{OK: true}}
	cohortsync.Register("mockprov", "Mock Provider", func() cohortsync.Provider { return prov })
	defer cohortsync.ResetForTest()

	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "mockprov",
		Name: "S", PullCredentialKeyID: "k", PullCredentialCiphertext: []byte("cred"),
	}

	result, err := svc.TestSource(context.Background(), "t1", id, auditlogsvc.Actor{Type: "admin", ID: "a"})
	if err != nil {
		t.Fatalf("TestSource failed: %v", err)
	}
	if !result.OK {
		t.Error("expected result.OK = true")
	}
	if mr.testResultOK == nil || !*mr.testResultOK { // ptrext:allow test-assertion
		t.Error("expected UpdateTestResult called with ok=true")
	}
}

func TestTestSource_CheckError(t *testing.T) {
	cohortsync.ResetForTest()
	prov := &mockProvider{
		checkResult: cohortsync.CheckResult{OK: false, Error: "auth failed"},
		checkErr:    errors.New("auth failed"),
	}
	cohortsync.Register("mockprov", "Mock Provider", func() cohortsync.Provider { return prov })
	defer cohortsync.ResetForTest()

	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "mockprov",
		Name: "S", PullCredentialKeyID: "k", PullCredentialCiphertext: []byte("cred"),
	}

	result, err := svc.TestSource(context.Background(), "t1", id, auditlogsvc.Actor{Type: "admin", ID: "a"})
	if err == nil {
		t.Fatal("expected error from Check")
	}
	if result.OK {
		t.Error("expected result.OK = false")
	}
	if result.Error != "auth failed" {
		t.Errorf("result.Error = %q, want auth failed", result.Error)
	}
}

func TestTestSource_NoPullCredential(t *testing.T) {
	cohortsync.ResetForTest()
	prov := &mockProvider{checkResult: cohortsync.CheckResult{OK: true}}
	cohortsync.Register("mockprov", "Mock Provider", func() cohortsync.Provider { return prov })
	defer cohortsync.ResetForTest()

	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "mockprov", Name: "S",
		// No PullCredentialKeyID set
	}

	result, err := svc.TestSource(context.Background(), "t1", id, auditlogsvc.Actor{Type: "admin", ID: "a"})
	if err != nil {
		t.Fatalf("TestSource failed: %v (should return result, not error)", err)
	}
	if result.OK {
		t.Error("expected result.OK = false when pull credential missing")
	}
	if result.Error != "pull credential not configured" {
		t.Errorf("result.Error = %q, want 'pull credential not configured'", result.Error)
	}
}

func TestTestSource_SourceNotFound(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	_, err := svc.TestSource(context.Background(), "t1", uuid.New(), auditlogsvc.Actor{Type: "admin", ID: "a"})
	if !errors.Is(err, repo.ErrSourceNotFound) {
		t.Fatalf("expected ErrSourceNotFound, got %v", err)
	}
}

// --- applyScalarFields ---

func TestApplyScalarFields_NameTrimmed(t *testing.T) {
	next := &repo.Source{Name: "old", Enabled: true, Status: "active"} // ptrext:allow test-fixture
	name := "  New Name  "
	applyScalarFields(UpdateSourceInput{Name: &name}, next) // ptrext:allow test-fixture
	if next.Name != "New Name" {
		t.Errorf("Name = %q, want New Name", next.Name)
	}
}

func TestApplyScalarFields_EnabledChangesStatus(t *testing.T) {
	next := &repo.Source{Name: "S", Enabled: true, Status: "active"} // ptrext:allow test-fixture
	disabled := false
	applyScalarFields(UpdateSourceInput{Enabled: &disabled}, next) // ptrext:allow test-fixture
	if next.Enabled {
		t.Error("expected Enabled=false")
	}
	if next.Status != "disabled" {
		t.Errorf("Status = %q, want disabled", next.Status)
	}

	enabled := true
	applyScalarFields(UpdateSourceInput{Enabled: &enabled}, next) // ptrext:allow test-fixture
	if !next.Enabled {
		t.Error("expected Enabled=true")
	}
	if next.Status != "active" {
		t.Errorf("Status = %q, want active", next.Status)
	}
}

func TestApplyScalarFields_NilFieldsUnchanged(t *testing.T) {
	next := &repo.Source{Name: "Original", Enabled: true, Status: "active"} // ptrext:allow test-fixture
	applyScalarFields(UpdateSourceInput{}, next)
	if next.Name != "Original" {
		t.Errorf("Name changed unexpectedly to %q", next.Name)
	}
	if !next.Enabled {
		t.Error("Enabled changed unexpectedly")
	}
}

// --- applySourceConfig ---

func TestApplySourceConfig_ValidJSON(t *testing.T) {
	svc := New(newMockRepo(), mockStore{keyID: "k"})
	next := &repo.Source{Name: "S", ProviderConfig: []byte("{}")} // ptrext:allow test-fixture
	cfg := `{"key":"val"}`
	err := svc.applySourceConfig(UpdateSourceInput{ProviderConfig: &cfg}, next) // ptrext:allow test-fixture
	if err != nil {
		t.Fatalf("applySourceConfig failed: %v", err)
	}
	if string(next.ProviderConfig) != `{"key":"val"}` {
		t.Errorf("ProviderConfig = %q", string(next.ProviderConfig))
	}
}

func TestApplySourceConfig_InvalidJSON(t *testing.T) {
	svc := New(newMockRepo(), mockStore{keyID: "k"})
	next := &repo.Source{Name: "S", ProviderConfig: []byte("{}")} // ptrext:allow test-fixture
	bad := "not json"
	err := svc.applySourceConfig(UpdateSourceInput{ProviderConfig: &bad}, next) // ptrext:allow test-fixture
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestApplySourceConfig_BaseURLClear(t *testing.T) {
	svc := New(newMockRepo(), mockStore{keyID: "k"})
	next := &repo.Source{Name: "S", BaseURL: "https://old.example.com"} // ptrext:allow test-fixture
	empty := ""
	err := svc.applySourceConfig(UpdateSourceInput{BaseURL: &empty}, next) // ptrext:allow test-fixture
	if err != nil {
		t.Fatalf("applySourceConfig failed: %v", err)
	}
	if next.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty", next.BaseURL)
	}
}

// --- applySourceCredentials ---

func TestApplySourceCredentials_EncryptsNewCredential(t *testing.T) {
	svc := New(newMockRepo(), mockStore{keyID: "key-new"})
	next := &repo.Source{ // ptrext:allow test-fixture
		ID: uuid.New(), TenantID: "t1", Provider: "amplitude",
		Name: "S", CredentialKeyID: "key-old", CredentialCiphertext: []byte("old"),
	}
	cred := "new-secret"
	err := svc.applySourceCredentials(UpdateSourceInput{Credential: &cred}, next) // ptrext:allow test-fixture
	if err != nil {
		t.Fatalf("applySourceCredentials failed: %v", err)
	}
	if next.CredentialKeyID != "key-new" {
		t.Errorf("CredentialKeyID = %q, want key-new", next.CredentialKeyID)
	}
	if string(next.CredentialCiphertext) != "new-secret" {
		t.Errorf("CredentialCiphertext = %q, want new-secret", string(next.CredentialCiphertext))
	}
}

func TestApplySourceCredentials_EmptyCredentialRejected(t *testing.T) {
	svc := New(newMockRepo(), mockStore{keyID: "k"})
	next := &repo.Source{ID: uuid.New(), TenantID: "t1", Provider: "amplitude", Name: "S"} // ptrext:allow test-fixture
	empty := "   "
	err := svc.applySourceCredentials(UpdateSourceInput{Credential: &empty}, next) // ptrext:allow test-fixture
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for blank credential, got %v", err)
	}
}

func TestApplySourceCredentials_PullCredentialSet(t *testing.T) {
	svc := New(newMockRepo(), mockStore{keyID: "pk"})
	next := &repo.Source{ID: uuid.New(), TenantID: "t1", Provider: "amplitude", Name: "S"} // ptrext:allow test-fixture
	pc := "api_key:secret"
	err := svc.applySourceCredentials(UpdateSourceInput{PullCredential: &pc}, next) // ptrext:allow test-fixture
	if err != nil {
		t.Fatalf("applySourceCredentials failed: %v", err)
	}
	if next.PullCredentialKeyID != "pk" {
		t.Errorf("PullCredentialKeyID = %q, want pk", next.PullCredentialKeyID)
	}
	if string(next.PullCredentialCiphertext) != "api_key:secret" {
		t.Errorf("PullCredentialCiphertext = %q", string(next.PullCredentialCiphertext))
	}
}

func TestApplySourceCredentials_PullCredentialCleared(t *testing.T) {
	svc := New(newMockRepo(), mockStore{keyID: "k"})
	next := &repo.Source{ // ptrext:allow test-fixture
		ID: uuid.New(), TenantID: "t1", Provider: "amplitude", Name: "S",
		PullCredentialKeyID: "old", PullCredentialCiphertext: []byte("old-val"),
	}
	empty := ""
	err := svc.applySourceCredentials(UpdateSourceInput{PullCredential: &empty}, next) // ptrext:allow test-fixture
	if err != nil {
		t.Fatalf("applySourceCredentials failed: %v", err)
	}
	if next.PullCredentialKeyID != "" {
		t.Errorf("PullCredentialKeyID = %q, want empty", next.PullCredentialKeyID)
	}
	if len(next.PullCredentialCiphertext) != 0 {
		t.Error("PullCredentialCiphertext should be nil")
	}
}

// --- Audit recording ---

func TestUpdateSource_RecordsAudit(t *testing.T) {
	mr := newMockRepo()
	audit := &mockAudit{}
	svc := New(mr, mockStore{keyID: "k"})
	svc.SetAuditLogger(audit)

	id := uuid.New()
	mr.sources[id] = &repo.Source{
		ID: id, TenantID: "t1", Provider: "amplitude",
		Name: "Old", Enabled: true, Status: "active",
	}

	newName := "New"
	_, err := svc.UpdateSource(context.Background(), UpdateSourceInput{
		TenantID:   "t1",
		ID:         id,
		Name:       &newName, // ptrext:allow test-fixture
		Actor:      Actor{ID: "admin-1"},
		AuditActor: auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("UpdateSource failed: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(audit.events))
	}
	if audit.events[0].Action != "cohort_source.update" {
		t.Errorf("audit action = %q, want cohort_source.update", audit.events[0].Action)
	}
}

// --- SyncNow_PreCheckBlocks (existing but adding variant) ---

func TestSyncNow_SourceNotFound(t *testing.T) {
	mr := newMockRepo()
	svc := New(mr, mockStore{keyID: "k"})

	cohortID := uuid.New()
	mr.cohorts[cohortID] = &repo.Cohort{
		ID: cohortID, TenantID: "t1", CohortSourceID: uuid.New(),
	}

	_, err := svc.SyncNow(context.Background(), "t1", cohortID,
		Actor{ID: "a"}, auditlogsvc.Actor{Type: "admin", ID: "a"})
	if !errors.Is(err, repo.ErrSourceNotFound) {
		t.Fatalf("expected ErrSourceNotFound, got %v", err)
	}
}
