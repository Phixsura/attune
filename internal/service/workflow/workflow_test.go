// ptrext:file-allow test fixtures build fake stores and pointer-heavy setup
package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedbackaudit"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeStateStore struct {
	states       map[string]*workflowstate.WorkflowState // keyed by id
	transitions  []workflowstate.Transition
	edges        []workflowstate.TransitionEdge
	activeFB     int64  // CountActiveFeedback return value
	activeDefCnt int64  // CountActiveDefaults return value
	archived     string // last stateID passed to ArchiveWithTransitions

	checkResult bool // CheckTransitionTx return value
	currentID   *string

	upsertIDs map[string]string // name -> returned ID

	// Error injection
	getErr           error
	countFBErr       error
	countDefaultsErr error
	archiveErr       error
	checkTxErr       error
	currentErr       error
	setFBStateErr    error
	upsertErr        error
	insertTrErr      error
}

func newFakeStateStore() *fakeStateStore {
	return &fakeStateStore{
		states:    make(map[string]*workflowstate.WorkflowState),
		upsertIDs: make(map[string]string),
	}
}

func (f *fakeStateStore) List(_ context.Context, _ string, _ bool) ([]workflowstate.WorkflowState, error) {
	var out []workflowstate.WorkflowState
	for _, s := range f.states {
		out = append(out, *s)
	}
	return out, nil
}

func (f *fakeStateStore) GetByTenantAndID(_ context.Context, _, id string) (*workflowstate.WorkflowState, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	s, ok := f.states[id]
	if !ok {
		return nil, workflowstate.ErrNotFound
	}
	return s, nil
}

func (f *fakeStateStore) Create(_ context.Context, s workflowstate.WorkflowState) (*workflowstate.WorkflowState, error) {
	f.states[s.ID] = &s
	return &s, nil
}

func (f *fakeStateStore) Update(_ context.Context, s workflowstate.WorkflowState) (*workflowstate.WorkflowState, error) {
	f.states[s.ID] = &s
	return &s, nil
}

func (f *fakeStateStore) Archive(_ context.Context, _, id string) error {
	delete(f.states, id)
	return nil
}

func (f *fakeStateStore) DeleteTransitionsForState(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeStateStore) CountActiveFeedback(_ context.Context, _, _ string) (int64, error) {
	return f.activeFB, f.countFBErr
}

func (f *fakeStateStore) CountActiveDefaults(_ context.Context, _ string) (int64, error) {
	return f.activeDefCnt, f.countDefaultsErr
}

func (f *fakeStateStore) CheckTransition(_ context.Context, _, _, _ string) (bool, error) {
	return f.checkResult, nil
}

func (f *fakeStateStore) CheckTransitionTx(_ context.Context, _ pgx.Tx, _, _, _ string) (bool, error) {
	return f.checkResult, f.checkTxErr
}

func (f *fakeStateStore) AllowedNext(_ context.Context, _, _ string) ([]workflowstate.WorkflowState, error) {
	return nil, nil
}

func (f *fakeStateStore) ListTransitions(_ context.Context, _ string) ([]workflowstate.Transition, error) {
	return f.transitions, nil
}

func (f *fakeStateStore) ReplaceTransitions(_ context.Context, _ string, edges []workflowstate.TransitionEdge) ([]workflowstate.Transition, error) {
	f.edges = edges
	return nil, nil
}

func (f *fakeStateStore) UpsertStateReturningID(_ context.Context, _ pgx.Tx, s workflowstate.WorkflowState) (string, error) {
	if f.upsertErr != nil {
		return "", f.upsertErr
	}
	id, ok := f.upsertIDs[s.Name]
	if !ok {
		id = "gen-" + s.Name
		f.upsertIDs[s.Name] = id
	}
	return id, nil
}

func (f *fakeStateStore) InsertTransitionIgnoreConflict(_ context.Context, _ pgx.Tx, _, _, _ string) error {
	return f.insertTrErr
}

func (f *fakeStateStore) GetCurrentStateForUpdate(_ context.Context, _ pgx.Tx, _ string, _ int64) (*string, error) {
	return f.currentID, f.currentErr
}

func (f *fakeStateStore) ArchiveWithTransitions(_ context.Context, _, stateID string) error {
	if f.archiveErr != nil {
		return f.archiveErr
	}
	f.archived = stateID
	return nil
}

func (f *fakeStateStore) SetFeedbackState(_ context.Context, _ pgx.Tx, _ int64, _ string) error {
	return f.setFBStateErr
}

type fakeAuditWriter struct {
	entries []feedbackaudit.Entry
	err     error
}

func (f *fakeAuditWriter) WriteTx(_ context.Context, _ pgx.Tx, e feedbackaudit.Entry) error {
	if f.err != nil {
		return f.err
	}
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeAuditWriter) List(_ context.Context, _ string, _ int64, _ string, _ int) ([]feedbackaudit.Entry, string, error) {
	return f.entries, "", nil
}

// ---------------------------------------------------------------------------
// fakeTx / fakeTxBeginner — mock pgx.Tx and TxBeginner for unit testing
// ---------------------------------------------------------------------------

type fakeTx struct {
	commitErr error
}

func (f *fakeTx) Begin(ctx context.Context) (pgx.Tx, error)                  { return f, nil }
func (f *fakeTx) Commit(_ context.Context) error                             { return f.commitErr }
func (f *fakeTx) Rollback(_ context.Context) error                           { return nil }
func (f *fakeTx) Conn() *pgx.Conn                                            { return nil }
func (f *fakeTx) LargeObjects() pgx.LargeObjects                             { return pgx.LargeObjects{} }
func (f *fakeTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (f *fakeTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (f *fakeTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (f *fakeTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f *fakeTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) { return nil, nil }
func (f *fakeTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row        { return nil }

type fakeTxBeginner struct {
	tx       *fakeTx
	beginErr error
}

func (f *fakeTxBeginner) Begin(_ context.Context) (pgx.Tx, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.tx, nil
}

func (f *fakeTxBeginner) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.tx, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeState(id, name, category string, isDefault bool) *workflowstate.WorkflowState {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	return &workflowstate.WorkflowState{
		ID:        id,
		TenantID:  "t-1",
		Name:      name,
		Color:     "#3b82f6",
		Category:  category,
		Position:  0,
		IsDefault: isDefault,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// newServiceNoPool creates a Service with a nil pool. Only test paths that
// do not call pool.Begin / pool.BeginTx (ArchiveState, early-exit
// validations in Transition).
func newServiceNoPool(states StateStore, audits AuditWriter) *Service {
	return &Service{states: states, audits: audits, pool: nil}
}

// ---------------------------------------------------------------------------
// ArchiveState tests (no pool dependency — fully unit-testable)
// ---------------------------------------------------------------------------

func TestArchiveState_Success(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-1"] = makeState("s-1", "Review", "open", false)
	store.activeFB = 0
	svc := newServiceNoPool(store, &fakeAuditWriter{})

	if err := svc.ArchiveState(context.Background(), "t-1", "s-1"); err != nil {
		t.Fatalf("ArchiveState: %v", err)
	}
	if store.archived != "s-1" {
		t.Fatalf("archived = %q, want s-1", store.archived)
	}
}

func TestArchiveState_NotFound(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	svc := newServiceNoPool(store, &fakeAuditWriter{})

	err := svc.ArchiveState(context.Background(), "t-1", "missing")
	if !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("err = %v, want ErrStateNotFound", err)
	}
}

func TestArchiveState_AlreadyArchived(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	st := makeState("s-1", "Done", "closed", false)
	now := time.Now()
	st.ArchivedAt = ptrext.Of(now)
	store.states["s-1"] = st

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	err := svc.ArchiveState(context.Background(), "t-1", "s-1")
	if !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("err = %v, want ErrStateNotFound (already archived)", err)
	}
}

func TestArchiveState_StateInUse(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-1"] = makeState("s-1", "Active", "active", false)
	store.activeFB = 3

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	err := svc.ArchiveState(context.Background(), "t-1", "s-1")
	if !errors.Is(err, ErrStateInUse) {
		t.Fatalf("err = %v, want ErrStateInUse", err)
	}
}

func TestArchiveState_LastDefault(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-1"] = makeState("s-1", "Triage", "open", true)
	store.activeFB = 0
	store.activeDefCnt = 1 // only one default left

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	err := svc.ArchiveState(context.Background(), "t-1", "s-1")
	if !errors.Is(err, ErrLastDefault) {
		t.Fatalf("err = %v, want ErrLastDefault", err)
	}
}

func TestArchiveState_DefaultWithMultipleDefaults(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-1"] = makeState("s-1", "Triage", "open", true)
	store.activeFB = 0
	store.activeDefCnt = 2 // two defaults, safe to archive one

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	if err := svc.ArchiveState(context.Background(), "t-1", "s-1"); err != nil {
		t.Fatalf("ArchiveState: %v", err)
	}
	if store.archived != "s-1" {
		t.Fatalf("archived = %q, want s-1", store.archived)
	}
}

func TestArchiveState_CountFeedbackError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-1"] = makeState("s-1", "Active", "active", false)
	store.countFBErr = errors.New("db down")

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	err := svc.ArchiveState(context.Background(), "t-1", "s-1")
	if err == nil || err.Error() != "db down" {
		t.Fatalf("err = %v, want db down", err)
	}
}

func TestArchiveState_CountDefaultsError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-1"] = makeState("s-1", "Triage", "open", true)
	store.activeFB = 0
	store.countDefaultsErr = errors.New("db down")

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	err := svc.ArchiveState(context.Background(), "t-1", "s-1")
	if err == nil || err.Error() != "db down" {
		t.Fatalf("err = %v, want db down", err)
	}
}

func TestArchiveState_GetByTenantError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.getErr = errors.New("unexpected")

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	err := svc.ArchiveState(context.Background(), "t-1", "s-1")
	if err == nil || err.Error() != "unexpected" {
		t.Fatalf("err = %v, want unexpected", err)
	}
}

func TestArchiveState_ArchiveWithTransitionsError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-1"] = makeState("s-1", "Review", "open", false)
	store.activeFB = 0
	store.archiveErr = errors.New("archive failed")

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	err := svc.ArchiveState(context.Background(), "t-1", "s-1")
	if err == nil || err.Error() != "archive failed" {
		t.Fatalf("err = %v, want archive failed", err)
	}
}

// ---------------------------------------------------------------------------
// Transition – early validation tests (before pool.Begin is called)
// ---------------------------------------------------------------------------

func TestTransition_TargetStateNotFound(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	svc := newServiceNoPool(store, &fakeAuditWriter{})

	_, err := svc.Transition(context.Background(), "t-1", 1, "missing", "user-1", "")
	if !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("err = %v, want ErrStateNotFound", err)
	}
}

func TestTransition_TargetStateArchived(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	st := makeState("s-to", "Done", "closed", false)
	now := time.Now()
	st.ArchivedAt = ptrext.Of(now)
	store.states["s-to"] = st

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "user-1", "")
	if !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("err = %v, want ErrStateNotFound (archived target)", err)
	}
}

func TestTransition_GetByTenantError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.getErr = errors.New("db error")

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-1", "user-1", "")
	if err == nil || err.Error() != "db error" {
		t.Fatalf("err = %v, want db error", err)
	}
}

// ---------------------------------------------------------------------------
// BatchTransition – error classification tests (uses early-exit paths only)
// ---------------------------------------------------------------------------

func TestBatchTransition_AllFail_StateNotFound(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	// No states registered → all transitions fail with ErrStateNotFound
	svc := newServiceNoPool(store, &fakeAuditWriter{})

	result, err := svc.BatchTransition(context.Background(), "t-1",
		[]int64{1, 2, 3}, "missing", "user-1", "test")
	if err != nil {
		t.Fatalf("BatchTransition: %v", err)
	}
	if result.Succeeded != 0 {
		t.Fatalf("succeeded = %d, want 0", result.Succeeded)
	}
	if len(result.Failed) != 3 {
		t.Fatalf("failed = %d, want 3", len(result.Failed))
	}
	for _, f := range result.Failed {
		if f.Code != "WORKFLOW_STATE_NOT_FOUND" {
			t.Fatalf("code = %q, want WORKFLOW_STATE_NOT_FOUND", f.Code)
		}
		if f.Message != "target state not found" {
			t.Fatalf("message = %q", f.Message)
		}
	}
}

func TestBatchTransition_AllFail_ArchivedTarget(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	st := makeState("s-to", "Done", "closed", false)
	now := time.Now()
	st.ArchivedAt = ptrext.Of(now)
	store.states["s-to"] = st

	svc := newServiceNoPool(store, &fakeAuditWriter{})
	result, err := svc.BatchTransition(context.Background(), "t-1",
		[]int64{10, 20}, "s-to", "user-1", "")
	if err != nil {
		t.Fatalf("BatchTransition: %v", err)
	}
	if result.Succeeded != 0 {
		t.Fatalf("succeeded = %d, want 0", result.Succeeded)
	}
	if len(result.Failed) != 2 {
		t.Fatalf("failed = %d, want 2", len(result.Failed))
	}
	for _, f := range result.Failed {
		if f.Code != "WORKFLOW_STATE_NOT_FOUND" {
			t.Fatalf("code = %q, want WORKFLOW_STATE_NOT_FOUND", f.Code)
		}
	}
}

func TestBatchTransition_Empty(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	svc := newServiceNoPool(store, &fakeAuditWriter{})

	result, err := svc.BatchTransition(context.Background(), "t-1",
		[]int64{}, "s-1", "user-1", "")
	if err != nil {
		t.Fatalf("BatchTransition: %v", err)
	}
	if result.Succeeded != 0 || len(result.Failed) != 0 {
		t.Fatalf("result = %+v, want empty", result)
	}
}

func TestBatchTransition_FeedbackIDsPreserved(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	svc := newServiceNoPool(store, &fakeAuditWriter{})

	result, err := svc.BatchTransition(context.Background(), "t-1",
		[]int64{42, 99}, "missing", "user-1", "")
	if err != nil {
		t.Fatalf("BatchTransition: %v", err)
	}
	if result.Failed[0].FeedbackID != 42 {
		t.Fatalf("failed[0].FeedbackID = %d, want 42", result.Failed[0].FeedbackID)
	}
	if result.Failed[1].FeedbackID != 99 {
		t.Fatalf("failed[1].FeedbackID = %d, want 99", result.Failed[1].FeedbackID)
	}
}

func TestBatchTransition_InternalErrorCode(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.getErr = errors.New("unexpected db failure")
	svc := newServiceNoPool(store, &fakeAuditWriter{})

	result, err := svc.BatchTransition(context.Background(), "t-1",
		[]int64{1}, "s-1", "user-1", "")
	if err != nil {
		t.Fatalf("BatchTransition: %v", err)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("failed = %d, want 1", len(result.Failed))
	}
	if result.Failed[0].Code != "INTERNAL" {
		t.Fatalf("code = %q, want INTERNAL", result.Failed[0].Code)
	}
}

// ---------------------------------------------------------------------------
// NewService
// ---------------------------------------------------------------------------

func TestNewService(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	audits := &fakeAuditWriter{}

	svc := NewService(store, audits, nil)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.states != store {
		t.Fatal("states not wired")
	}
	if svc.audits != audits {
		t.Fatal("audits not wired")
	}
}

// ---------------------------------------------------------------------------
// Exported error sentinel identity
// ---------------------------------------------------------------------------

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"ErrInvalidTransition", ErrInvalidTransition, "transition not allowed"},
		{"ErrStateNotFound", ErrStateNotFound, "workflow state not found"},
		{"ErrNoWorkflowState", ErrNoWorkflowState, "feedback has no workflow state"},
		{"ErrLastDefault", ErrLastDefault, "cannot remove last default state"},
		{"ErrStateInUse", ErrStateInUse, "state has active feedback"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Error() != tc.want {
				t.Fatalf("Error() = %q, want %q", tc.err.Error(), tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Transition – full transaction path tests (with fakeTxBeginner)
// ---------------------------------------------------------------------------

func TestTransition_Success(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-from"] = makeState("s-from", "Triage", "open", true)
	store.states["s-to"] = makeState("s-to", "In Progress", "active", false)
	store.currentID = ptrext.Of("s-from")
	store.checkResult = true
	audits := &fakeAuditWriter{}
	tx := &fakeTx{}
	pool := &fakeTxBeginner{tx: tx}

	svc := NewService(store, audits, pool)
	result, err := svc.Transition(context.Background(), "t-1", 42, "s-to", "admin", "moving forward")
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if result.FeedbackID != 42 {
		t.Fatalf("FeedbackID = %d, want 42", result.FeedbackID)
	}
	if result.FromState.ID != "s-from" {
		t.Fatalf("FromState.ID = %q, want s-from", result.FromState.ID)
	}
	if result.ToState.ID != "s-to" {
		t.Fatalf("ToState.ID = %q, want s-to", result.ToState.ID)
	}
	if len(audits.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(audits.entries))
	}
	e := audits.entries[0]
	if e.FeedbackID != 42 || e.ChangedBy != "admin" || e.Comment != "moving forward" {
		t.Fatalf("audit entry = %+v", e)
	}
}

func TestTransition_BeginError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	pool := &fakeTxBeginner{beginErr: errors.New("pool exhausted")}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "u", "")
	if err == nil {
		t.Fatal("expected error from Begin")
	}
	if !errors.Is(err, pool.beginErr) {
		t.Fatalf("err = %v, want pool exhausted (wrapped)", err)
	}
}

func TestTransition_CurrentStateNil(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	store.currentID = nil
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "u", "")
	if !errors.Is(err, ErrNoWorkflowState) {
		t.Fatalf("err = %v, want ErrNoWorkflowState", err)
	}
}

func TestTransition_CurrentStateNotFound(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	store.currentErr = workflowstate.ErrNotFound
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "u", "")
	if !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("err = %v, want ErrStateNotFound", err)
	}
}

func TestTransition_CurrentStateError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	store.currentErr = errors.New("lock timeout")
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "u", "")
	if err == nil || err.Error() != "lock timeout" {
		t.Fatalf("err = %v, want lock timeout", err)
	}
}

func TestTransition_FromStateLookupFails(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	store.currentID = ptrext.Of("nonexistent")
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "u", "")
	if !errors.Is(err, workflowstate.ErrNotFound) {
		t.Fatalf("err = %v, want workflowstate.ErrNotFound (from state missing)", err)
	}
}

func TestTransition_NotAllowed(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-from"] = makeState("s-from", "Triage", "open", true)
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	store.currentID = ptrext.Of("s-from")
	store.checkResult = false
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "u", "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestTransition_CheckTransitionTxError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-from"] = makeState("s-from", "Triage", "open", true)
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	store.currentID = ptrext.Of("s-from")
	store.checkTxErr = errors.New("check failed")
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "u", "")
	if err == nil || err.Error() != "check failed" {
		t.Fatalf("err = %v, want check failed", err)
	}
}

func TestTransition_SetFeedbackStateError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-from"] = makeState("s-from", "Triage", "open", true)
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	store.currentID = ptrext.Of("s-from")
	store.checkResult = true
	store.setFBStateErr = errors.New("set state failed")
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "u", "")
	if err == nil || err.Error() != "set state failed" {
		t.Fatalf("err = %v, want set state failed", err)
	}
}

func TestTransition_AuditWriteError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-from"] = makeState("s-from", "Triage", "open", true)
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	store.currentID = ptrext.Of("s-from")
	store.checkResult = true
	audits := &fakeAuditWriter{err: errors.New("audit write failed")}
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, audits, pool)
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "u", "")
	if err == nil || err.Error() != "audit write failed" {
		t.Fatalf("err = %v, want audit write failed", err)
	}
}

func TestTransition_CommitError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-from"] = makeState("s-from", "Triage", "open", true)
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	store.currentID = ptrext.Of("s-from")
	store.checkResult = true
	tx := &fakeTx{commitErr: errors.New("commit failed")}
	pool := &fakeTxBeginner{tx: tx}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	_, err := svc.Transition(context.Background(), "t-1", 1, "s-to", "u", "")
	if err == nil {
		t.Fatal("expected commit error")
	}
	if !errors.Is(err, tx.commitErr) {
		t.Fatalf("err = %v, want commit failed (wrapped)", err)
	}
}

// ---------------------------------------------------------------------------
// BatchTransition – full path with mock pool
// ---------------------------------------------------------------------------

func TestBatchTransition_MixedResults(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.states["s-from"] = makeState("s-from", "Triage", "open", true)
	store.states["s-to"] = makeState("s-to", "Done", "closed", false)
	store.currentID = ptrext.Of("s-from")
	store.checkResult = true
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	result, err := svc.BatchTransition(context.Background(), "t-1",
		[]int64{1, 2}, "s-to", "admin", "batch")
	if err != nil {
		t.Fatalf("BatchTransition: %v", err)
	}
	if result.Succeeded != 2 {
		t.Fatalf("succeeded = %d, want 2", result.Succeeded)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("failed = %d, want 0", len(result.Failed))
	}
}

// ---------------------------------------------------------------------------
// SeedDefaults tests (with fakeTxBeginner)
// ---------------------------------------------------------------------------

func TestSeedDefaults_Success(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	tx := &fakeTx{}
	pool := &fakeTxBeginner{tx: tx}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	err := svc.SeedDefaults(context.Background(), "t-1")
	if err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	if len(store.upsertIDs) != 5 {
		t.Fatalf("upserted states = %d, want 5", len(store.upsertIDs))
	}
}

func TestSeedDefaults_BeginTxError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	pool := &fakeTxBeginner{beginErr: errors.New("serializable failed")}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	err := svc.SeedDefaults(context.Background(), "t-1")
	if err == nil {
		t.Fatal("expected begin error")
	}
	if !errors.Is(err, pool.beginErr) {
		t.Fatalf("err = %v, want serializable failed (wrapped)", err)
	}
}

func TestSeedDefaults_UpsertError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.upsertErr = errors.New("upsert boom")
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	err := svc.SeedDefaults(context.Background(), "t-1")
	if err == nil {
		t.Fatal("expected upsert error")
	}
	if !errors.Is(err, store.upsertErr) {
		t.Fatalf("err = %v, want upsert boom (wrapped)", err)
	}
}

func TestSeedDefaults_InsertTransitionError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	store.insertTrErr = errors.New("transition insert fail")
	pool := &fakeTxBeginner{tx: &fakeTx{}}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	err := svc.SeedDefaults(context.Background(), "t-1")
	if err == nil {
		t.Fatal("expected insert transition error")
	}
	if !errors.Is(err, store.insertTrErr) {
		t.Fatalf("err = %v, want transition insert fail (wrapped)", err)
	}
}

func TestSeedDefaults_CommitError(t *testing.T) {
	t.Parallel()
	store := newFakeStateStore()
	tx := &fakeTx{commitErr: errors.New("commit seed failed")}
	pool := &fakeTxBeginner{tx: tx}

	svc := NewService(store, &fakeAuditWriter{}, pool)
	err := svc.SeedDefaults(context.Background(), "t-1")
	if err == nil {
		t.Fatal("expected commit error")
	}
	if !errors.Is(err, tx.commitErr) {
		t.Fatalf("err = %v, want commit seed failed (wrapped)", err)
	}
}
