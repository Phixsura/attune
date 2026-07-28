// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-mock-fixtures

package cohortsync

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/cohortsync"
	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/cohortsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/cohortsync"
)

// ---------------------------------------------------------------------------
// stub service
// ---------------------------------------------------------------------------

type stubService struct {
	sources     []repo.Source
	source      *repo.Source
	cohorts     []repo.Cohort
	cohort      *repo.Cohort
	members     []repo.Membership
	events      []repo.SyncEvent
	runs        []repo.SyncRun
	runsResult  repo.ListRunsResult
	syncResult  *svc.SyncRunResult
	health      svc.HealthSummary
	checkResult cohortsync.CheckResult
	err         error
	testErr     error
}

func (s *stubService) CreateSource(_ context.Context, _ svc.CreateSourceInput) (*repo.Source, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.source, nil
}

func (s *stubService) UpdateSource(_ context.Context, _ svc.UpdateSourceInput) (*repo.Source, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.source, nil
}

func (s *stubService) TestSource(_ context.Context, _ string, _ uuid.UUID, _ auditlogsvc.Actor) (cohortsync.CheckResult, error) {
	return s.checkResult, s.testErr
}

func (s *stubService) GetSource(_ context.Context, _ string, _ uuid.UUID) (*repo.Source, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.source, nil
}

func (s *stubService) ListSources(_ context.Context, _ string) ([]repo.Source, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.sources, nil
}

func (s *stubService) DeleteSource(_ context.Context, _ string, _ uuid.UUID, _ svc.Actor, _ auditlogsvc.Actor) error {
	return s.err
}

func (s *stubService) GetCohort(_ context.Context, _ string, _ uuid.UUID) (*repo.Cohort, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.cohort, nil
}

func (s *stubService) ListAllCohorts(_ context.Context, _ string) ([]repo.Cohort, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.cohorts, nil
}

func (s *stubService) ListCohorts(_ context.Context, _ string, _ uuid.UUID) ([]repo.Cohort, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.cohorts, nil
}

func (s *stubService) UpdateCohort(_ context.Context, _ svc.UpdateCohortInput) (*repo.Cohort, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.cohort, nil
}

func (s *stubService) ListMembers(_ context.Context, _ string, _ uuid.UUID, _ int) ([]repo.Membership, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.members, nil
}

func (s *stubService) ListEvents(_ context.Context, _ string, _ uuid.UUID, _ int) ([]repo.SyncEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.events, nil
}

func (s *stubService) SyncNow(_ context.Context, _ string, _ uuid.UUID, _ svc.Actor, _ auditlogsvc.Actor) (*svc.SyncRunResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.syncResult, nil
}

func (s *stubService) ListRuns(_ context.Context, _ string, _ uuid.UUID, _ int) ([]repo.SyncRun, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.runs, nil
}

func (s *stubService) ListRunsPaginated(_ context.Context, _ string, _ uuid.UUID, _ int, _ string) (repo.ListRunsResult, error) {
	if s.err != nil {
		return repo.ListRunsResult{}, s.err
	}
	return s.runsResult, nil
}

func (s *stubService) Health(_ context.Context, _ string) (svc.HealthSummary, error) {
	if s.err != nil {
		return svc.HealthSummary{}, s.err
	}
	return s.health, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func testCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    &session.AuthCtx{TenantID: "t1", UserID: "u1", UserType: "admin"},
	}
}

func assertDispatcherErr(t *testing.T, err error, wantStatus int, wantCode attunev1.ErrorCode) {
	t.Helper()
	var de *dispatcher.Error
	if !errors.As(err, &de) {
		t.Fatalf("want *dispatcher.Error, got %v", err)
	}
	if de.Status != wantStatus {
		t.Errorf("status = %d, want %d (msg=%q)", de.Status, wantStatus, de.Message)
	}
	if de.Code != wantCode {
		t.Errorf("code = %v, want %v", de.Code, wantCode)
	}
}

var refTime = time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// buildWebhookURLs
// ---------------------------------------------------------------------------

func TestBuildWebhookURLs_Amplitude(t *testing.T) {
	sid := uuid.New()
	s := repo.Source{ID: sid, TenantID: "t1", Provider: "amplitude"}
	urls := buildWebhookURLs(s, "https://attune.example.com")

	if len(urls) != 3 {
		t.Fatalf("amplitude webhook URL count = %d, want 3", len(urls))
	}
	prefix := "https://attune.example.com/v1/cohort-sync/amplitude/t1/" + sid.String()
	want := []string{prefix + "/create", prefix + "/add", prefix + "/remove"}
	for i, u := range urls {
		if u != want[i] {
			t.Errorf("urls[%d] = %q, want %q", i, u, want[i])
		}
	}
}

func TestBuildWebhookURLs_Mixpanel(t *testing.T) {
	sid := uuid.New()
	s := repo.Source{ID: sid, TenantID: "t1", Provider: "mixpanel"}
	urls := buildWebhookURLs(s, "https://attune.example.com")

	if len(urls) != 1 {
		t.Fatalf("mixpanel webhook URL count = %d, want 1", len(urls))
	}
	want := "https://attune.example.com/v1/cohort-sync/mixpanel/t1/" + sid.String()
	if urls[0] != want {
		t.Errorf("urls[0] = %q, want %q", urls[0], want)
	}
}

func TestBuildWebhookURLs_UnknownProvider(t *testing.T) {
	sid := uuid.New()
	s := repo.Source{ID: sid, TenantID: "t1", Provider: "custom"}
	urls := buildWebhookURLs(s, "https://base.io")

	if len(urls) != 1 {
		t.Fatalf("unknown-provider webhook URL count = %d, want 1", len(urls))
	}
	want := "https://base.io/v1/cohort-sync/custom/t1/" + sid.String()
	if urls[0] != want {
		t.Errorf("urls[0] = %q, want %q", urls[0], want)
	}
}

// ---------------------------------------------------------------------------
// sourceToProto
// ---------------------------------------------------------------------------

func TestSourceToProto_AllFields(t *testing.T) {
	sid := uuid.New()
	syncTime := refTime
	testedTime := refTime.Add(time.Hour)
	lastTestOK := true
	s := repo.Source{
		ID:           sid,
		TenantID:     "t1",
		Provider:     "amplitude",
		Name:         "My Source",
		AuthType:     "api_key",
		BaseURL:      "https://amplitude.com",
		Enabled:      true,
		Status:       "active",
		LastError:    "none",
		LastSyncAt:   &syncTime,
		LastTestedAt: &testedTime,
		LastTestOK:   &lastTestOK,
		CreatedAt:    refTime,
		UpdatedAt:    refTime,
	}
	h := NewHandler(nil, "https://base.io")
	pb := h.sourceToProto(s)

	if pb.Id != sid.String() {
		t.Errorf("Id = %q, want %q", pb.Id, sid.String())
	}
	if pb.Provider != "amplitude" {
		t.Errorf("Provider = %q, want amplitude", pb.Provider)
	}
	if pb.Name != "My Source" {
		t.Errorf("Name = %q, want My Source", pb.Name)
	}
	if pb.AuthType != "api_key" {
		t.Errorf("AuthType = %q, want api_key", pb.AuthType)
	}
	if pb.BaseUrl != "https://amplitude.com" {
		t.Errorf("BaseUrl = %q, want https://amplitude.com", pb.BaseUrl)
	}
	if !pb.Enabled {
		t.Error("Enabled = false, want true")
	}
	if pb.Status != "active" {
		t.Errorf("Status = %q, want active", pb.Status)
	}
	if pb.LastError != "none" {
		t.Errorf("LastError = %q, want none", pb.LastError)
	}
	if pb.LastSyncAt == nil {
		t.Fatal("LastSyncAt = nil, want non-nil")
	}
	if pb.LastTestedAt == nil {
		t.Fatal("LastTestedAt = nil, want non-nil")
	}
	if pb.LastTestOk == nil || !*pb.LastTestOk {
		t.Error("LastTestOk should be true")
	}
	if len(pb.WebhookUrls) != 3 {
		t.Errorf("WebhookUrls count = %d, want 3 (amplitude)", len(pb.WebhookUrls))
	}
	if pb.WebhookUrl != pb.WebhookUrls[0] {
		t.Errorf("WebhookUrl = %q, should equal first of WebhookUrls %q", pb.WebhookUrl, pb.WebhookUrls[0])
	}
}

func TestSourceToProto_NilOptionalFields(t *testing.T) {
	s := repo.Source{
		ID:        uuid.New(),
		TenantID:  "t1",
		Provider:  "mixpanel",
		Name:      "Source 2",
		AuthType:  "api_key",
		Enabled:   false,
		CreatedAt: refTime,
		UpdatedAt: refTime,
	}
	h := NewHandler(nil, "https://base.io")
	pb := h.sourceToProto(s)

	if pb.LastSyncAt != nil {
		t.Errorf("LastSyncAt = %v, want nil", pb.LastSyncAt)
	}
	if pb.LastTestedAt != nil {
		t.Errorf("LastTestedAt = %v, want nil", pb.LastTestedAt)
	}
	if pb.LastTestOk != nil {
		t.Errorf("LastTestOk = %v, want nil", pb.LastTestOk)
	}
}

// ---------------------------------------------------------------------------
// cohortToProto
// ---------------------------------------------------------------------------

func TestCohortToProto_WithSourceMap(t *testing.T) {
	cid := uuid.New()
	sid := uuid.New()
	syncedAt := refTime
	c := repo.Cohort{
		ID:               cid,
		CohortSourceID:   sid,
		ExternalCohortID: "ext-123",
		Name:             "Power Users",
		Description:      "Active in last 7d",
		StaleTTLDays:     30,
		MemberCount:      42,
		Enabled:          true,
		LastSyncedAt:     &syncedAt,
		LastError:        "",
		CreatedAt:        refTime,
		UpdatedAt:        refTime,
	}
	sm := map[uuid.UUID]sourceInfo{
		sid: {Name: "Amplitude Prod", Provider: "amplitude"},
	}
	pb := cohortToProto(c, sm)

	if pb.Id != cid.String() {
		t.Errorf("Id = %q, want %q", pb.Id, cid.String())
	}
	if pb.CohortSourceId != sid.String() {
		t.Errorf("CohortSourceId = %q, want %q", pb.CohortSourceId, sid.String())
	}
	if pb.ExternalCohortId != "ext-123" {
		t.Errorf("ExternalCohortId = %q, want ext-123", pb.ExternalCohortId)
	}
	if pb.Name != "Power Users" {
		t.Errorf("Name = %q, want Power Users", pb.Name)
	}
	if pb.SourceName != "Amplitude Prod" {
		t.Errorf("SourceName = %q, want Amplitude Prod", pb.SourceName)
	}
	if pb.SourceProvider != "amplitude" {
		t.Errorf("SourceProvider = %q, want amplitude", pb.SourceProvider)
	}
	if pb.StaleTtlDays != 30 {
		t.Errorf("StaleTtlDays = %d, want 30", pb.StaleTtlDays)
	}
	if pb.MemberCount != 42 {
		t.Errorf("MemberCount = %d, want 42", pb.MemberCount)
	}
	if pb.LastSyncedAt == nil {
		t.Error("LastSyncedAt = nil, want non-nil")
	}
}

func TestCohortToProto_NilSourceMap(t *testing.T) {
	c := repo.Cohort{
		ID:             uuid.New(),
		CohortSourceID: uuid.New(),
		Name:           "Test Cohort",
		CreatedAt:      refTime,
		UpdatedAt:      refTime,
	}
	pb := cohortToProto(c, nil)

	if pb.SourceName != "" {
		t.Errorf("SourceName = %q, want empty with nil map", pb.SourceName)
	}
	if pb.SourceProvider != "" {
		t.Errorf("SourceProvider = %q, want empty with nil map", pb.SourceProvider)
	}
	if pb.LastSyncedAt != nil {
		t.Errorf("LastSyncedAt = %v, want nil", pb.LastSyncedAt)
	}
}

// ---------------------------------------------------------------------------
// memberToProto
// ---------------------------------------------------------------------------

func TestMemberToProto(t *testing.T) {
	mid := uuid.New()
	m := repo.Membership{
		ID:             mid,
		ExternalUserID: "ext-u-1",
		Email:          "user@example.com",
		DisplayName:    "Test User",
		JoinedAt:       refTime,
		LastSeenAt:     refTime.Add(24 * time.Hour),
	}
	pb := memberToProto(m)

	if pb.Id != mid.String() {
		t.Errorf("Id = %q, want %q", pb.Id, mid.String())
	}
	if pb.ExternalUserId != "ext-u-1" {
		t.Errorf("ExternalUserId = %q, want ext-u-1", pb.ExternalUserId)
	}
	if pb.Email != "user@example.com" {
		t.Errorf("Email = %q, want user@example.com", pb.Email)
	}
	if pb.DisplayName != "Test User" {
		t.Errorf("DisplayName = %q, want Test User", pb.DisplayName)
	}
	if pb.JoinedAt == nil {
		t.Error("JoinedAt = nil, want non-nil")
	}
	if pb.LastSeenAt == nil {
		t.Error("LastSeenAt = nil, want non-nil")
	}
}

// ---------------------------------------------------------------------------
// eventToProto
// ---------------------------------------------------------------------------

func TestEventToProto_WithRunID(t *testing.T) {
	eid := uuid.New()
	rid := uuid.New()
	e := repo.SyncEvent{
		ID:             eid,
		CohortSourceID: uuid.New(),
		Provider:       "amplitude",
		EventType:      "add",
		Status:         "processed",
		MembersCount:   5,
		FailureReason:  "",
		RunID:          &rid,
		ReceivedAt:     refTime,
		CreatedAt:      refTime,
	}
	pb := eventToProto(e)

	if pb.Id != eid.String() {
		t.Errorf("Id = %q, want %q", pb.Id, eid.String())
	}
	if pb.Provider != "amplitude" {
		t.Errorf("Provider = %q, want amplitude", pb.Provider)
	}
	if pb.EventType != "add" {
		t.Errorf("EventType = %q, want add", pb.EventType)
	}
	if pb.Status != "processed" {
		t.Errorf("Status = %q, want processed", pb.Status)
	}
	if pb.MembersCount != 5 {
		t.Errorf("MembersCount = %d, want 5", pb.MembersCount)
	}
	if pb.RunId == nil {
		t.Fatal("RunId = nil, want non-nil")
	}
	if *pb.RunId != rid.String() {
		t.Errorf("RunId = %q, want %q", *pb.RunId, rid.String())
	}
}

func TestEventToProto_NilRunID(t *testing.T) {
	e := repo.SyncEvent{
		ID:             uuid.New(),
		CohortSourceID: uuid.New(),
		Provider:       "mixpanel",
		EventType:      "members",
		Status:         "failed",
		FailureReason:  "timeout",
		ReceivedAt:     refTime,
		CreatedAt:      refTime,
	}
	pb := eventToProto(e)

	if pb.RunId != nil {
		t.Errorf("RunId = %v, want nil", pb.RunId)
	}
	if pb.FailureReason != "timeout" {
		t.Errorf("FailureReason = %q, want timeout", pb.FailureReason)
	}
}

// ---------------------------------------------------------------------------
// runToProto
// ---------------------------------------------------------------------------

func TestRunToProto_WithFinishedAt(t *testing.T) {
	rid := uuid.New()
	cohortID := uuid.New()
	finAt := refTime.Add(5 * time.Minute)
	r := repo.SyncRun{
		ID:             rid,
		CohortID:       cohortID,
		Trigger:        "webhook",
		Status:         "completed",
		MembersAdded:   10,
		MembersRemoved: 3,
		MembersTotal:   42,
		ErrorMessage:   "",
		StartedAt:      refTime,
		FinishedAt:     &finAt,
		CreatedAt:      refTime,
	}
	pb := runToProto(r)

	if pb.Id != rid.String() {
		t.Errorf("Id = %q, want %q", pb.Id, rid.String())
	}
	if pb.CohortId != cohortID.String() {
		t.Errorf("CohortId = %q, want %q", pb.CohortId, cohortID.String())
	}
	if pb.Trigger != "webhook" {
		t.Errorf("Trigger = %q, want webhook", pb.Trigger)
	}
	if pb.Status != "completed" {
		t.Errorf("Status = %q, want completed", pb.Status)
	}
	if pb.MembersAdded != 10 {
		t.Errorf("MembersAdded = %d, want 10", pb.MembersAdded)
	}
	if pb.MembersRemoved != 3 {
		t.Errorf("MembersRemoved = %d, want 3", pb.MembersRemoved)
	}
	if pb.MembersTotal != 42 {
		t.Errorf("MembersTotal = %d, want 42", pb.MembersTotal)
	}
	if pb.FinishedAt == nil {
		t.Fatal("FinishedAt = nil, want non-nil")
	}
}

func TestRunToProto_NilFinishedAt(t *testing.T) {
	r := repo.SyncRun{
		ID:        uuid.New(),
		CohortID:  uuid.New(),
		Trigger:   "manual",
		Status:    "running",
		StartedAt: refTime,
		CreatedAt: refTime,
	}
	pb := runToProto(r)

	if pb.FinishedAt != nil {
		t.Errorf("FinishedAt = %v, want nil", pb.FinishedAt)
	}
	if pb.Status != "running" {
		t.Errorf("Status = %q, want running", pb.Status)
	}
}

// ---------------------------------------------------------------------------
// mapError
// ---------------------------------------------------------------------------

func TestMapError_Validation(t *testing.T) {
	err := fmt.Errorf("name empty: %w", svc.ErrValidation)
	_, gotErr := mapError[*attunev1.CohortSource](context.Background(), "Test", err)
	assertDispatcherErr(t, gotErr, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
}

func TestMapError_SourceNotFound(t *testing.T) {
	_, gotErr := mapError[*attunev1.CohortSource](context.Background(), "Test", repo.ErrSourceNotFound)
	assertDispatcherErr(t, gotErr, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
}

func TestMapError_CohortNotFound(t *testing.T) {
	_, gotErr := mapError[*attunev1.Cohort](context.Background(), "Test", repo.ErrCohortNotFound)
	assertDispatcherErr(t, gotErr, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
}

func TestMapError_Conflict(t *testing.T) {
	_, gotErr := mapError[*attunev1.CohortSource](context.Background(), "Test", repo.ErrConflict)
	assertDispatcherErr(t, gotErr, http.StatusConflict, attunev1.ErrorCode_CONFLICT)
}

func TestMapError_Unavailable(t *testing.T) {
	err := fmt.Errorf("%w: fakeprov", cohortsync.ErrProviderUnavailable)
	_, gotErr := mapError[*attunev1.CohortSource](context.Background(), "Test", err)
	assertDispatcherErr(t, gotErr, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
}

func TestMapError_Internal(t *testing.T) {
	_, gotErr := mapError[*attunev1.CohortSource](context.Background(), "Test", errors.New("db down"))
	assertDispatcherErr(t, gotErr, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
}

// ---------------------------------------------------------------------------
// intPtrFromInt32
// ---------------------------------------------------------------------------

func TestIntPtrFromInt32_Nil(t *testing.T) {
	if got := intPtrFromInt32(nil); got != nil {
		t.Errorf("intPtrFromInt32(nil) = %v, want nil", got)
	}
}

func TestIntPtrFromInt32_Value(t *testing.T) {
	v := int32(42)
	got := intPtrFromInt32(&v)
	if got == nil {
		t.Fatal("intPtrFromInt32(&42) = nil, want non-nil")
	}
	if *got != 42 {
		t.Errorf("intPtrFromInt32(&42) = %d, want 42", *got)
	}
}

// ---------------------------------------------------------------------------
// buildSourceMap
// ---------------------------------------------------------------------------

func TestBuildSourceMap(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	sources := []repo.Source{
		{ID: id1, Name: "Source A", Provider: "amplitude"},
		{ID: id2, Name: "Source B", Provider: "mixpanel"},
	}
	sm := buildSourceMap(sources)
	if len(sm) != 2 {
		t.Fatalf("len(sourceMap) = %d, want 2", len(sm))
	}
	if sm[id1].Name != "Source A" || sm[id1].Provider != "amplitude" {
		t.Errorf("sourceMap[id1] = %+v, want {Source A, amplitude}", sm[id1])
	}
	if sm[id2].Name != "Source B" || sm[id2].Provider != "mixpanel" {
		t.Errorf("sourceMap[id2] = %+v, want {Source B, mixpanel}", sm[id2])
	}
}

// ---------------------------------------------------------------------------
// Handler-level: ListSources
// ---------------------------------------------------------------------------

func TestListSources_Success(t *testing.T) {
	stub := &stubService{
		sources: []repo.Source{
			{ID: uuid.New(), TenantID: "t1", Provider: "amplitude", Name: "S1", CreatedAt: refTime, UpdatedAt: refTime},
		},
	}
	h := NewHandler(stub, "https://base.io")
	res, err := h.ListSources(testCtx(), &attunev1.ListCohortSourcesRequest{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(res.Body.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(res.Body.Sources))
	}
	if res.Body.Sources[0].Name != "S1" {
		t.Errorf("Sources[0].Name = %q, want S1", res.Body.Sources[0].Name)
	}
}

func TestListSources_ServiceError(t *testing.T) {
	stub := &stubService{err: errors.New("db down")}
	h := NewHandler(stub, "https://base.io")
	_, err := h.ListSources(testCtx(), &attunev1.ListCohortSourcesRequest{})
	assertDispatcherErr(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
}

// ---------------------------------------------------------------------------
// Handler-level: GetSource
// ---------------------------------------------------------------------------

func TestGetSource_BadID(t *testing.T) {
	h := NewHandler(&stubService{}, "https://base.io")
	_, err := h.GetSource(testCtx(), &attunev1.GetCohortSourceRequest{Id: "not-a-uuid"})
	assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
}

func TestGetSource_NotFound(t *testing.T) {
	stub := &stubService{err: repo.ErrSourceNotFound}
	h := NewHandler(stub, "https://base.io")
	_, err := h.GetSource(testCtx(), &attunev1.GetCohortSourceRequest{Id: uuid.New().String()})
	assertDispatcherErr(t, err, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
}

func TestGetSource_Success(t *testing.T) {
	sid := uuid.New()
	stub := &stubService{
		source: &repo.Source{ID: sid, TenantID: "t1", Provider: "amplitude", Name: "S1", CreatedAt: refTime, UpdatedAt: refTime},
	}
	h := NewHandler(stub, "https://base.io")
	res, err := h.GetSource(testCtx(), &attunev1.GetCohortSourceRequest{Id: sid.String()})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Body.Id != sid.String() {
		t.Errorf("Id = %q, want %q", res.Body.Id, sid.String())
	}
}

// ---------------------------------------------------------------------------
// Handler-level: DeleteSource
// ---------------------------------------------------------------------------

func TestDeleteSource_BadID(t *testing.T) {
	h := NewHandler(&stubService{}, "https://base.io")
	_, err := h.DeleteSource(testCtx(), &attunev1.DeleteCohortSourceRequest{Id: "bad"})
	assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
}

func TestDeleteSource_NotFound(t *testing.T) {
	stub := &stubService{err: repo.ErrSourceNotFound}
	h := NewHandler(stub, "https://base.io")
	_, err := h.DeleteSource(testCtx(), &attunev1.DeleteCohortSourceRequest{Id: uuid.New().String()})
	assertDispatcherErr(t, err, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
}

// ---------------------------------------------------------------------------
// Handler-level: SyncCohort
// ---------------------------------------------------------------------------

func TestSyncCohort_BadID(t *testing.T) {
	h := NewHandler(&stubService{}, "https://base.io")
	_, err := h.SyncCohort(testCtx(), &attunev1.SyncCohortRequest{Id: "invalid"})
	assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
}

func TestSyncCohort_Success(t *testing.T) {
	rid := uuid.New()
	stub := &stubService{
		syncResult: &svc.SyncRunResult{
			Run: repo.SyncRun{
				ID:        rid,
				CohortID:  uuid.New(),
				Trigger:   "manual",
				Status:    "completed",
				StartedAt: refTime,
				CreatedAt: refTime,
			},
		},
	}
	h := NewHandler(stub, "https://base.io")
	res, err := h.SyncCohort(testCtx(), &attunev1.SyncCohortRequest{Id: uuid.New().String()})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Body.Run == nil {
		t.Fatal("Run = nil, want non-nil")
	}
	if res.Body.Run.Id != rid.String() {
		t.Errorf("Run.Id = %q, want %q", res.Body.Run.Id, rid.String())
	}
}

// ---------------------------------------------------------------------------
// Handler-level: Health
// ---------------------------------------------------------------------------

func TestHealth_Success(t *testing.T) {
	lastSync := refTime
	stub := &stubService{
		health: svc.HealthSummary{
			SourceCount:        3,
			ActiveSources:      2,
			ErrorSources:       1,
			DisabledSources:    0,
			CohortCount:        5,
			TotalActiveMembers: 100,
			LastSyncAt:         &lastSync,
			SyncsLast24h:       7,
		},
	}
	h := NewHandler(stub, "https://base.io")
	res, err := h.Health(testCtx(), &attunev1.GetCohortSyncHealthRequest{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Body.SourceCount != 3 {
		t.Errorf("SourceCount = %d, want 3", res.Body.SourceCount)
	}
	if res.Body.ActiveSources != 2 {
		t.Errorf("ActiveSources = %d, want 2", res.Body.ActiveSources)
	}
	if res.Body.TotalActiveMembers != 100 {
		t.Errorf("TotalActiveMembers = %d, want 100", res.Body.TotalActiveMembers)
	}
	if res.Body.LastSyncAt == nil {
		t.Error("LastSyncAt = nil, want non-nil")
	}
	if res.Body.SyncsLast_24H != 7 {
		t.Errorf("SyncsLast24H = %d, want 7", res.Body.SyncsLast_24H)
	}
}

func TestHealth_NilLastSyncAt(t *testing.T) {
	stub := &stubService{health: svc.HealthSummary{}}
	h := NewHandler(stub, "https://base.io")
	res, err := h.Health(testCtx(), &attunev1.GetCohortSyncHealthRequest{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Body.LastSyncAt != nil {
		t.Errorf("LastSyncAt = %v, want nil", res.Body.LastSyncAt)
	}
}

// ---------------------------------------------------------------------------
// Handler-level: ListSyncRuns pagination
// ---------------------------------------------------------------------------

func TestListSyncRuns_DefaultLimit(t *testing.T) {
	rid := uuid.New()
	stub := &stubService{
		runsResult: repo.ListRunsResult{
			Runs:       []repo.SyncRun{{ID: rid, CohortID: uuid.New(), StartedAt: refTime, CreatedAt: refTime}},
			NextCursor: "abc",
		},
	}
	h := NewHandler(stub, "https://base.io")
	res, err := h.ListSyncRuns(testCtx(), &attunev1.ListCohortSyncRunsRequest{CohortId: uuid.New().String()})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(res.Body.Runs) != 1 {
		t.Fatalf("len(Runs) = %d, want 1", len(res.Body.Runs))
	}
	if res.Body.NextCursor == nil || *res.Body.NextCursor != "abc" {
		t.Errorf("NextCursor = %v, want abc", res.Body.NextCursor)
	}
}

func TestListSyncRuns_NoCursor(t *testing.T) {
	stub := &stubService{runsResult: repo.ListRunsResult{}}
	h := NewHandler(stub, "https://base.io")
	res, err := h.ListSyncRuns(testCtx(), &attunev1.ListCohortSyncRunsRequest{CohortId: uuid.New().String()})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Body.NextCursor != nil {
		t.Errorf("NextCursor = %v, want nil", res.Body.NextCursor)
	}
}

// ---------------------------------------------------------------------------
// Handler-level: TestSource
// ---------------------------------------------------------------------------

func TestTestSource_BadID(t *testing.T) {
	h := NewHandler(&stubService{}, "https://base.io")
	_, err := h.TestSource(testCtx(), &attunev1.TestCohortSourceRequest{Id: "bad"})
	assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
}

func TestTestSource_Success(t *testing.T) {
	stub := &stubService{
		checkResult: cohortsync.CheckResult{OK: true},
	}
	h := NewHandler(stub, "https://base.io")
	res, err := h.TestSource(testCtx(), &attunev1.TestCohortSourceRequest{Id: uuid.New().String()})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Body.Ok {
		t.Error("Ok = false, want true")
	}
}

func TestTestSource_TestRanButFailed(t *testing.T) {
	stub := &stubService{
		checkResult: cohortsync.CheckResult{OK: false, Error: "invalid key"},
		testErr:     errors.New("test connectivity failed"),
	}
	h := NewHandler(stub, "https://base.io")
	res, err := h.TestSource(testCtx(), &attunev1.TestCohortSourceRequest{Id: uuid.New().String()})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Body.Ok {
		t.Error("Ok = true, want false")
	}
	if res.Body.Error != "invalid key" {
		t.Errorf("Error = %q, want invalid key", res.Body.Error)
	}
}

func TestTestSource_CouldNotRun(t *testing.T) {
	stub := &stubService{
		checkResult: cohortsync.CheckResult{},
		testErr:     repo.ErrSourceNotFound,
	}
	h := NewHandler(stub, "https://base.io")
	_, err := h.TestSource(testCtx(), &attunev1.TestCohortSourceRequest{Id: uuid.New().String()})
	assertDispatcherErr(t, err, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
}

// ---------------------------------------------------------------------------
// Handler-level: ListCohorts
// ---------------------------------------------------------------------------

func TestListCohorts_BadSourceID(t *testing.T) {
	stub := &stubService{sources: []repo.Source{}}
	h := NewHandler(stub, "https://base.io")
	_, err := h.ListCohorts(testCtx(), &attunev1.ListCohortsRequest{SourceId: ptrext.Of("bad-uuid")})
	assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
}

// ---------------------------------------------------------------------------
// Handler-level: UpdateCohort
// ---------------------------------------------------------------------------

func TestUpdateCohort_BadID(t *testing.T) {
	h := NewHandler(&stubService{}, "https://base.io")
	_, err := h.UpdateCohort(testCtx(), &attunev1.UpdateCohortRequest{Id: "invalid"})
	assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
}

// ---------------------------------------------------------------------------
// Handler-level: ListMembers
// ---------------------------------------------------------------------------

func TestListMembers_BadCohortID(t *testing.T) {
	h := NewHandler(&stubService{}, "https://base.io")
	_, err := h.ListMembers(testCtx(), &attunev1.ListCohortMembersRequest{CohortId: "nope"})
	assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
}

// ---------------------------------------------------------------------------
// Handler-level: ListEvents
// ---------------------------------------------------------------------------

func TestListEvents_BadSourceID(t *testing.T) {
	h := NewHandler(&stubService{}, "https://base.io")
	_, err := h.ListEvents(testCtx(), &attunev1.ListCohortSyncEventsRequest{SourceId: "nope"})
	assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
}
