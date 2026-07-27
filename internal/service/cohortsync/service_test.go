// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-mock-fixtures

package cohortsync

import (
	"context"
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
	sources     map[uuid.UUID]*repo.Source
	cohorts     map[uuid.UUID]*repo.Cohort
	runs        map[uuid.UUID]*repo.SyncRun
	memberAdded int
	hasRunning  bool
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

func (m *mockRepo) ListCohorts(_ context.Context, _ string, _ uuid.UUID) ([]repo.Cohort, error) {
	return nil, nil
}

func (m *mockRepo) ListAllCohorts(_ context.Context, _ string) ([]repo.Cohort, error) {
	return nil, nil
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

func (m *mockRepo) CountActiveMembers(_ context.Context, _ string, _ uuid.UUID) (int, error) {
	return m.memberAdded, nil
}

func (m *mockRepo) InsertRun(_ context.Context, run repo.SyncRun) (*repo.SyncRun, error) {
	row := run
	m.runs[row.ID] = &row
	return &row, nil
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

func (m *mockRepo) HasRunningRun(_ context.Context, _ string, _ uuid.UUID) (bool, error) {
	return m.hasRunning, nil
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
