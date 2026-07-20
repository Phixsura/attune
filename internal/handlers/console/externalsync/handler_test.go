// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	externalsynccore "github.com/Phixsura/attune/internal/externalsync"
	_ "github.com/Phixsura/attune/internal/externalsync/adapter/githubissue"
	_ "github.com/Phixsura/attune/internal/externalsync/adapter/jiraissue"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/externalsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/externalsync"
)

func TestCreateConnectionDefaultsEnabledWhenRequestOmitsField(t *testing.T) {
	tests := []struct {
		name        string
		enabled     *bool
		wantEnabled bool
	}{
		{name: "omitted", enabled: nil, wantEnabled: true},
		{name: "explicit false", enabled: ptrext.Of(false), wantEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := ptrext.Of(fakeHandlerService{})
			handler := ptrext.Of(Handler{service: fake})

			result, err := handler.CreateConnection(handlerTestContext(), ptrext.Of(attunev1.CreateExternalConnectionRequest{
				Provider:   "github",
				Name:       "GitHub",
				AuthType:   "token",
				Credential: "secret",
				Enabled:    tt.enabled,
			}))
			if err != nil {
				t.Fatalf("CreateConnection returned error: %v", err)
			}
			if fake.createInput.Enabled != tt.wantEnabled {
				t.Fatalf("service Enabled = %t; want %t", fake.createInput.Enabled, tt.wantEnabled)
			}
			if result.Body.GetEnabled() != tt.wantEnabled {
				t.Fatalf("response Enabled = %t; want %t", result.Body.GetEnabled(), tt.wantEnabled)
			}
		})
	}
}

func TestRunDirectionFromProtoLeavesUnspecifiedForMappingDefault(t *testing.T) {
	if got := runDirectionFromProto(attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_UNSPECIFIED); got != "" {
		t.Fatalf("unspecified run direction = %q; want mapping default", got)
	}
	if got := runDirectionFromProto(attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_PUSH); got != repo.DirectionPush {
		t.Fatalf("push run direction = %q; want push", got)
	}
}

func TestListProvidersReturnsRegisteredEntries(t *testing.T) {
	handler := ptrext.Of(Handler{})

	result, err := handler.ListProviders(handlerTestContext(), ptrext.Of(attunev1.ListExternalSyncProvidersRequest{}))
	if err != nil {
		t.Fatalf("ListProviders returned error: %v", err)
	}

	got := result.Body.GetProviders()
	if len(got) != 2 {
		t.Fatalf("providers len = %d; want 2", len(got))
	}
	if got[0].GetProvider() != "github" || got[0].GetDisplay() != "GitHub" {
		t.Fatalf("providers[0] = %#v; want github/GitHub", got[0])
	}
	if got[1].GetProvider() != "jira" || got[1].GetDisplay() != "Jira" {
		t.Fatalf("providers[1] = %#v; want jira/Jira", got[1])
	}
}

func TestMappingDirectionFromProtoRequiresExplicitDirection(t *testing.T) {
	if got := mappingDirectionFromProto(attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_UNSPECIFIED); got != "" {
		t.Fatalf("unspecified mapping direction = %q; want validation path", got)
	}
	if got := mappingDirectionFromProto(attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL); got != repo.DirectionBidirectional {
		t.Fatalf("bidirectional mapping direction = %q; want bidirectional", got)
	}
}

func TestDirectionFromProtoRejectsUnknownEnumValue(t *testing.T) {
	got := runDirectionFromProto(attunev1.ExternalSyncDirection(99))
	if got == "" || got == repo.DirectionPull || got == repo.DirectionPush || got == repo.DirectionBidirectional {
		t.Fatalf("unknown run direction = %q; want invalid validation token", got)
	}
}

func TestBindListRunsRequestAcceptsFilters(t *testing.T) {
	req := ptrext.Of(attunev1.ListExternalSyncRunsRequest{})
	httpReq := httptest.NewRequest(http.MethodGet,
		"/external-sync/runs?connectionId=conn-1&mapping_id=map-1&status=failed&beforeId=run-1&limit=25", nil)

	if err := BindListRunsRequest(httpReq, req); err != nil {
		t.Fatalf("BindListRunsRequest returned error: %v", err)
	}
	if req.GetConnectionId() != "conn-1" || req.GetMappingId() != "map-1" ||
		req.GetStatus() != "failed" || req.GetBeforeId() != "run-1" || req.GetLimit() != 25 {
		t.Fatalf("bound request = %#v; want all filters", req)
	}
}

func TestBindListEventsRequestAcceptsFilters(t *testing.T) {
	req := ptrext.Of(attunev1.ListExternalSyncEventsRequest{})
	httpReq := httptest.NewRequest(http.MethodGet,
		"/external-sync/events?connection_id=conn-1&status=received&beforeId=event-1&limit=25", nil)

	if err := BindListEventsRequest(httpReq, req); err != nil {
		t.Fatalf("BindListEventsRequest returned error: %v", err)
	}
	if req.GetConnectionId() != "conn-1" || req.GetStatus() != "received" ||
		req.GetBeforeId() != "event-1" || req.GetLimit() != 25 {
		t.Fatalf("bound request = %#v; want all filters", req)
	}
}

func TestHealthReturnsProviderBreakdown(t *testing.T) {
	retryAfter := time.Date(2026, 7, 8, 3, 4, 5, 0, time.UTC)
	fake := ptrext.Of(fakeHandlerService{
		health: repo.Health{
			EnabledConnections:      2,
			DisabledConnections:     1,
			ThrottledRuns:           3,
			UnauthorizedRuns:        4,
			ProviderUnavailableRuns: 5,
			DelayedRetryRuns:        6,
			NewestRetryAfter:        ptrext.Of(retryAfter),
			DegradedConnections:     7,
			QuarantinedConnections:  8,
		},
	})
	handler := ptrext.Of(Handler{service: fake})

	result, err := handler.Health(handlerTestContext(), ptrext.Of(attunev1.GetExternalSyncHealthRequest{}))
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if result.Body.GetEnabledConnections() != 2 ||
		result.Body.GetDisabledConnections() != 1 ||
		result.Body.GetThrottledRuns() != 3 ||
		result.Body.GetUnauthorizedRuns() != 4 ||
		result.Body.GetProviderUnavailableRuns() != 5 ||
		result.Body.GetDelayedRetryRuns() != 6 ||
		result.Body.GetNewestRetryAfter() != "2026-07-08T03:04:05Z" ||
		result.Body.GetDegradedConnections() != 7 ||
		result.Body.GetQuarantinedConnections() != 8 {
		t.Fatalf("response = %#v; want provider breakdown fields", result.Body)
	}
}

func TestHealthPropagatesContextCancellation(t *testing.T) {
	handler := ptrext.Of(Handler{service: ptrext.Of(fakeHandlerService{healthErr: context.Canceled})})

	_, err := handler.Health(handlerTestContext(), ptrext.Of(attunev1.GetExternalSyncHealthRequest{}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Health error = %v; want context.Canceled", err)
	}
}

func TestResumeConnectionReturnsActiveConnection(t *testing.T) {
	connectionID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{})
	handler := ptrext.Of(Handler{service: fake})

	result, err := handler.ResumeConnection(handlerTestContext(), ptrext.Of(attunev1.ResumeExternalConnectionRequest{
		Id: connectionID.String(),
	}))
	if err != nil {
		t.Fatalf("ResumeConnection returned error: %v", err)
	}
	if fake.resumeInput.ID != connectionID || fake.resumeInput.Actor.ID == "" {
		t.Fatalf("resume input = %#v; want connection id and actor", fake.resumeInput)
	}
	if !result.Body.GetEnabled() || result.Body.GetStatus() != repo.ConnectionStatusActive {
		t.Fatalf("response = %#v; want active enabled connection", result.Body)
	}
}

func TestQualifyConnectionReturnsChecks(t *testing.T) {
	connectionID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{
		qualifyResult: svc.QualificationResult{
			ConnectionID: connectionID,
			Ready:        true,
			Checks: []svc.QualificationCheck{{
				Name:       "provider_check",
				Status:     svc.QualificationStatusOK,
				Summary:    "Provider check succeeded",
				DetailJSON: `{"latency_ms":12}`,
			}},
		},
	})
	handler := ptrext.Of(Handler{service: fake})

	result, err := handler.QualifyConnection(handlerTestContext(), ptrext.Of(attunev1.QualifyExternalConnectionRequest{
		Id: connectionID.String(),
	}))
	if err != nil {
		t.Fatalf("QualifyConnection returned error: %v", err)
	}
	if fake.qualifyID != connectionID {
		t.Fatalf("qualify id = %s; want %s", fake.qualifyID, connectionID)
	}
	if !result.Body.GetReady() || len(result.Body.GetChecks()) != 1 ||
		result.Body.GetChecks()[0].GetStatus() != attunev1.ExternalSyncQualificationCheckStatus_EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_OK {
		t.Fatalf("qualification response = %#v; want one ok check", result.Body)
	}
}

func TestBatchResolveConflictsReturnsRows(t *testing.T) {
	conflictID := uuid.New()
	otherID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{
		batchResolveRows: []repo.ConflictRow{{
			ID:         conflictID,
			TenantID:   "tenant-1",
			Status:     "resolved",
			Resolution: "external_wins",
		}},
	})
	handler := ptrext.Of(Handler{service: fake})

	result, err := handler.BatchResolveConflicts(handlerTestContext(), ptrext.Of(attunev1.BatchResolveExternalSyncConflictsRequest{
		Ids:        []string{conflictID.String(), otherID.String()},
		Resolution: attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS,
	}))
	if err != nil {
		t.Fatalf("BatchResolveConflicts returned error: %v", err)
	}
	if !reflect.DeepEqual(fake.batchResolveInput.IDs, []uuid.UUID{conflictID, otherID}) ||
		fake.batchResolveInput.Resolution != "external_wins" {
		t.Fatalf("batch input = %#v; want parsed ids and resolution", fake.batchResolveInput)
	}
	if result.Body.GetResolvedCount() != 1 || len(result.Body.GetConflicts()) != 1 {
		t.Fatalf("batch response = %#v; want one resolved conflict", result.Body)
	}
}

func TestListRunsInputRejectsInvalidBeforeID(t *testing.T) {
	_, err := listRunsInput("tenant-1", ptrext.Of(attunev1.ListExternalSyncRunsRequest{
		BeforeId: "not-a-uuid",
	}))
	if err == nil {
		t.Fatal("listRunsInput returned nil error; want invalid before id")
	}
}

func TestErrorMappingReturnsDispatcherErrors(t *testing.T) {
	if NewHandler(nil) == nil {
		t.Fatal("NewHandler returned nil")
	}
	ctx := handlerTestContext()

	if _, err := mapError[*attunev1.ExternalSyncRun](ctx, "test", svc.ErrValidation); err == nil {
		t.Fatal("validation mapError returned nil error; want dispatcher error")
	}
	if _, err := mapError[*attunev1.ExternalSyncRun](ctx, "test", repo.ErrRunNotFound); err == nil {
		t.Fatal("not found mapError returned nil error; want dispatcher error")
	}
	if _, err := mapError[*attunev1.ExternalSyncRun](ctx, "test", repo.ErrConflict); err == nil {
		t.Fatal("conflict mapError returned nil error; want dispatcher error")
	}
	if _, err := badID[*attunev1.ExternalSyncRun]("invalid run id"); err == nil {
		t.Fatal("badID returned nil error; want dispatcher error")
	}
	if _, err := internalError[*attunev1.ExternalSyncRun](context.Background(), "test", context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("internalError cancellation = %v; want context.Canceled", err)
	}
	if _, err := mapError[*attunev1.ExternalSyncRun](ctx, "test", svc.ErrProviderUnavailable); err == nil {
		t.Fatal("provider unavailable mapError returned nil error; want dispatcher error")
	}
	if _, err := mapError[*attunev1.ExternalSyncRun](ctx, "test", errors.New("boom")); err == nil {
		t.Fatal("unknown mapError returned nil error; want dispatcher error")
	}
	if _, err := internalError[*attunev1.ExternalSyncRun](ctx, "test", errors.New("boom")); err == nil {
		t.Fatal("internalError returned nil error; want dispatcher error")
	}
}

func TestRunEnumFallbackMappings(t *testing.T) {
	statuses := map[string]attunev1.ExternalSyncRunStatus{
		repo.RunStatusSucceeded: attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED,
		repo.RunStatusFailed:    attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_FAILED,
		repo.RunStatusDead:      attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_DEAD,
		"cancelled":             attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_CANCELLED,
	}
	for status, want := range statuses {
		if got := statusToProto(status); got != want {
			t.Fatalf("statusToProto(%q) = %v; want %v", status, got, want)
		}
	}
	triggers := map[string]attunev1.ExternalSyncRunTrigger{
		"schedule":         attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_SCHEDULE,
		repo.TriggerRetry:  attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_RETRY,
		repo.TriggerSystem: attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_SYSTEM,
	}
	for trigger, want := range triggers {
		if got := triggerToProto(trigger); got != want {
			t.Fatalf("triggerToProto(%q) = %v; want %v", trigger, got, want)
		}
	}
	if directionFromProto(attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_UNSPECIFIED) != invalidProtoDirection {
		t.Fatal("directionFromProto unspecified should return validation token")
	}
	if directionFromProto(attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_PULL) != repo.DirectionPull {
		t.Fatal("directionFromProto pull should map to repo pull")
	}
}

func TestEventEnumFallbackMappings(t *testing.T) {
	if eventSignatureToProto(repo.EventSignatureVerified) != attunev1.ExternalSyncEventSignatureStatus_EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED ||
		eventSignatureToProto(repo.EventSignatureFailed) != attunev1.ExternalSyncEventSignatureStatus_EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_FAILED ||
		eventSignatureToProto(repo.EventSignatureNotRequired) != attunev1.ExternalSyncEventSignatureStatus_EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_NOT_REQUIRED ||
		eventSignatureToProto("unknown") != attunev1.ExternalSyncEventSignatureStatus_EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_UNSPECIFIED {
		t.Fatal("eventSignatureToProto did not map all statuses")
	}
	if eventStatusToProto(repo.EventStatusReceived) != attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_RECEIVED ||
		eventStatusToProto(repo.EventStatusReplayed) != attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_REPLAYED ||
		eventStatusToProto(repo.EventStatusIgnored) != attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_IGNORED ||
		eventStatusToProto(repo.EventStatusFailed) != attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_FAILED ||
		eventStatusToProto("unknown") != attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_UNSPECIFIED {
		t.Fatalf("eventStatusToProto did not map all statuses")
	}
}

func TestResolutionEnumFallbackMappings(t *testing.T) {
	if resolutionFromProto(attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_LOCAL_WINS) != "local_wins" ||
		resolutionFromProto(attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS) != "external_wins" ||
		resolutionFromProto(attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_MANUAL_MERGE) != "manual_merge" ||
		resolutionFromProto(attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_IGNORED) != "ignored" ||
		resolutionFromProto(attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_UNSPECIFIED) != "" {
		t.Fatalf("resolutionFromProto did not map all resolutions")
	}
}

func TestQualificationEnumFallbackMappings(t *testing.T) {
	if qualificationStatusToProto(svc.QualificationStatusOK) != attunev1.ExternalSyncQualificationCheckStatus_EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_OK ||
		qualificationStatusToProto(svc.QualificationStatusWarning) != attunev1.ExternalSyncQualificationCheckStatus_EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_WARNING ||
		qualificationStatusToProto(svc.QualificationStatusFailed) != attunev1.ExternalSyncQualificationCheckStatus_EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_FAILED ||
		qualificationStatusToProto("unknown") != attunev1.ExternalSyncQualificationCheckStatus_EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_UNSPECIFIED {
		t.Fatalf("qualificationStatusToProto did not map all statuses")
	}
}

func TestHandlerRejectsInvalidIDs(t *testing.T) {
	handler := ptrext.Of(Handler{service: ptrext.Of(fakeHandlerService{})})
	ctx := handlerTestContext()
	if _, err := handler.UpdateConnection(ctx, ptrext.Of(attunev1.UpdateExternalConnectionRequest{Id: "bad"})); err == nil {
		t.Fatal("UpdateConnection invalid id returned nil error")
	}
	if _, err := handler.DeleteConnection(ctx, ptrext.Of(attunev1.DeleteExternalConnectionRequest{Id: "bad"})); err == nil {
		t.Fatal("DeleteConnection invalid id returned nil error")
	}
	if _, err := handler.TestConnection(ctx, ptrext.Of(attunev1.TestExternalConnectionRequest{Id: "bad"})); err == nil {
		t.Fatal("TestConnection invalid id returned nil error")
	}
	if _, err := handler.ResumeConnection(ctx, ptrext.Of(attunev1.ResumeExternalConnectionRequest{Id: "bad"})); err == nil {
		t.Fatal("ResumeConnection invalid id returned nil error")
	}
	if _, err := handler.QualifyConnection(ctx, ptrext.Of(attunev1.QualifyExternalConnectionRequest{Id: "bad"})); err == nil {
		t.Fatal("QualifyConnection invalid id returned nil error")
	}
	if _, err := handler.DiscoverConnectionSchema(ctx, ptrext.Of(attunev1.DiscoverExternalConnectionSchemaRequest{Id: "bad"})); err == nil {
		t.Fatal("DiscoverConnectionSchema invalid id returned nil error")
	}
	if _, err := handler.ListMappings(ctx, ptrext.Of(attunev1.ListExternalObjectMappingsRequest{ConnectionId: "bad"})); err == nil {
		t.Fatal("ListMappings invalid connection id returned nil error")
	}
	if _, err := handler.UpdateMapping(ctx, ptrext.Of(attunev1.UpdateExternalObjectMappingRequest{Id: "bad"})); err == nil {
		t.Fatal("UpdateMapping invalid id returned nil error")
	}
}

func TestHandlerRejectsInvalidRunEventAndConflictIDs(t *testing.T) {
	handler := ptrext.Of(Handler{service: ptrext.Of(fakeHandlerService{})})
	ctx := handlerTestContext()
	if _, err := handler.PreviewMapping(ctx, ptrext.Of(attunev1.PreviewExternalObjectMappingRequest{Id: "bad"})); err == nil {
		t.Fatal("PreviewMapping invalid id returned nil error")
	}
	if _, err := handler.ResetCursor(ctx, ptrext.Of(attunev1.ResetExternalSyncCursorRequest{Id: "bad"})); err == nil {
		t.Fatal("ResetCursor invalid id returned nil error")
	}
	if _, err := handler.RequestBackfill(ctx, ptrext.Of(attunev1.RequestExternalSyncBackfillRequest{Id: "bad"})); err == nil {
		t.Fatal("RequestBackfill invalid id returned nil error")
	}
	if _, err := handler.RequestRun(ctx, ptrext.Of(attunev1.RequestExternalSyncRunRequest{ConnectionId: "bad"})); err == nil {
		t.Fatal("RequestRun invalid connection id returned nil error")
	}
	if _, err := handler.RequestRun(ctx, ptrext.Of(attunev1.RequestExternalSyncRunRequest{ConnectionId: uuid.NewString(), MappingId: "bad"})); err == nil {
		t.Fatal("RequestRun invalid mapping id returned nil error")
	}
	if _, err := handler.GetRun(ctx, ptrext.Of(attunev1.GetExternalSyncRunRequest{Id: "bad"})); err == nil {
		t.Fatal("GetRun invalid id returned nil error")
	}
	if _, err := handler.RecordTimeline(ctx, ptrext.Of(attunev1.GetExternalSyncRecordTimelineRequest{MappingId: "bad"})); err == nil {
		t.Fatal("RecordTimeline invalid mapping id returned nil error")
	}
	if _, err := handler.RetryRun(ctx, ptrext.Of(attunev1.RetryExternalSyncRunRequest{Id: "bad"})); err == nil {
		t.Fatal("RetryRun invalid id returned nil error")
	}
}

func TestHandlerRejectsInvalidFailureConflictAndEventIDs(t *testing.T) {
	handler := ptrext.Of(Handler{service: ptrext.Of(fakeHandlerService{})})
	ctx := handlerTestContext()
	if _, err := handler.RetryFailure(ctx, ptrext.Of(attunev1.RetryExternalSyncFailureRequest{Id: "bad"})); err == nil {
		t.Fatal("RetryFailure invalid id returned nil error")
	}
	if _, err := handler.ResolveConflict(ctx, ptrext.Of(attunev1.ResolveExternalSyncConflictRequest{Id: "bad"})); err == nil {
		t.Fatal("ResolveConflict invalid id returned nil error")
	}
	if _, err := handler.BatchResolveConflicts(ctx, ptrext.Of(attunev1.BatchResolveExternalSyncConflictsRequest{Ids: []string{"bad"}})); err == nil {
		t.Fatal("BatchResolveConflicts invalid id returned nil error")
	}
	if _, err := handler.ListEvents(ctx, ptrext.Of(attunev1.ListExternalSyncEventsRequest{ConnectionId: "bad"})); err == nil {
		t.Fatal("ListEvents invalid connection id returned nil error")
	}
	if _, err := handler.GetEvent(ctx, ptrext.Of(attunev1.GetExternalSyncEventRequest{Id: "bad"})); err == nil {
		t.Fatal("GetEvent invalid id returned nil error")
	}
	if _, err := handler.ReplayEvent(ctx, ptrext.Of(attunev1.ReplayExternalSyncEventRequest{Id: "bad"})); err == nil {
		t.Fatal("ReplayEvent invalid id returned nil error")
	}
}

func TestHandlerConnectionServiceErrorsMapThroughEndpoints(t *testing.T) {
	fake := ptrext.Of(fakeHandlerService{
		listConnectionsErr: errors.New("list failed"),
		createErr:          svc.ErrValidation,
		listMappingsErr:    errors.New("mappings failed"),
		testErr:            errors.New("probe failed"),
		resumeErr:          svc.ErrValidation,
		qualifyErr:         svc.ErrValidation,
		discoverErr:        svc.ErrValidation,
		previewErr:         svc.ErrValidation,
		listRunsErr:        svc.ErrValidation,
		recordTimelineErr:  svc.ErrValidation,
		batchResolveErr:    svc.ErrValidation,
		listEventsErr:      svc.ErrValidation,
	})
	handler := ptrext.Of(Handler{service: fake})
	ctx := handlerTestContext()
	id := uuid.NewString()
	if _, err := handler.ListConnections(ctx, ptrext.Of(attunev1.ListExternalConnectionsRequest{})); err == nil {
		t.Fatal("ListConnections service error returned nil")
	}
	if _, err := handler.CreateConnection(ctx, ptrext.Of(attunev1.CreateExternalConnectionRequest{})); err == nil {
		t.Fatal("CreateConnection service error returned nil")
	}
	if _, err := handler.ListMappings(ctx, ptrext.Of(attunev1.ListExternalObjectMappingsRequest{})); err == nil {
		t.Fatal("ListMappings service error returned nil")
	}
	if _, err := handler.TestConnection(ctx, ptrext.Of(attunev1.TestExternalConnectionRequest{Id: id})); err == nil {
		t.Fatal("TestConnection service error returned nil")
	}
	if _, err := handler.ResumeConnection(ctx, ptrext.Of(attunev1.ResumeExternalConnectionRequest{Id: id})); err == nil {
		t.Fatal("ResumeConnection service error returned nil")
	}
	if _, err := handler.QualifyConnection(ctx, ptrext.Of(attunev1.QualifyExternalConnectionRequest{Id: id})); err == nil {
		t.Fatal("QualifyConnection service error returned nil")
	}
	if _, err := handler.DiscoverConnectionSchema(ctx, ptrext.Of(attunev1.DiscoverExternalConnectionSchemaRequest{Id: id})); err == nil {
		t.Fatal("DiscoverConnectionSchema service error returned nil")
	}
}

func TestHandlerMappingAndRunServiceErrorsMapThroughEndpoints(t *testing.T) {
	fake := ptrext.Of(fakeHandlerService{
		previewErr:        svc.ErrValidation,
		listRunsErr:       svc.ErrValidation,
		recordTimelineErr: svc.ErrValidation,
	})
	handler := ptrext.Of(Handler{service: fake})
	ctx := handlerTestContext()
	id := uuid.NewString()

	if _, err := handler.UpdateMapping(ctx, ptrext.Of(attunev1.UpdateExternalObjectMappingRequest{Id: id})); err == nil {
		t.Fatal("UpdateMapping service error returned nil")
	}
	if _, err := handler.PreviewMapping(ctx, ptrext.Of(attunev1.PreviewExternalObjectMappingRequest{Id: id})); err == nil {
		t.Fatal("PreviewMapping service error returned nil")
	}
	if _, err := handler.ResetCursor(ctx, ptrext.Of(attunev1.ResetExternalSyncCursorRequest{Id: id})); err == nil {
		t.Fatal("ResetCursor service error returned nil")
	}
	if _, err := handler.RequestBackfill(ctx, ptrext.Of(attunev1.RequestExternalSyncBackfillRequest{Id: id})); err == nil {
		t.Fatal("RequestBackfill service error returned nil")
	}
	if _, err := handler.RequestRun(ctx, ptrext.Of(attunev1.RequestExternalSyncRunRequest{ConnectionId: id})); err == nil {
		t.Fatal("RequestRun service error returned nil")
	}
	if _, err := handler.ListRuns(ctx, ptrext.Of(attunev1.ListExternalSyncRunsRequest{})); err == nil {
		t.Fatal("ListRuns service error returned nil")
	}
	if _, err := handler.RecordTimeline(ctx, ptrext.Of(attunev1.GetExternalSyncRecordTimelineRequest{MappingId: uuid.NewString()})); err == nil {
		t.Fatal("RecordTimeline service error returned nil")
	}
	if _, err := handler.RetryRun(ctx, ptrext.Of(attunev1.RetryExternalSyncRunRequest{Id: id})); err == nil {
		t.Fatal("RetryRun service error returned nil")
	}
}

func TestHandlerFailureConflictAndEventServiceErrorsMapThroughEndpoints(t *testing.T) {
	fake := ptrext.Of(fakeHandlerService{
		batchResolveErr: svc.ErrValidation,
		listEventsErr:   svc.ErrValidation,
	})
	handler := ptrext.Of(Handler{service: fake})
	ctx := handlerTestContext()
	id := uuid.NewString()

	if _, err := handler.RetryFailure(ctx, ptrext.Of(attunev1.RetryExternalSyncFailureRequest{Id: id})); err == nil {
		t.Fatal("RetryFailure service error returned nil")
	}
	if _, err := handler.ResolveConflict(ctx, ptrext.Of(attunev1.ResolveExternalSyncConflictRequest{Id: id})); err == nil {
		t.Fatal("ResolveConflict service error returned nil")
	}
	if _, err := handler.BatchResolveConflicts(ctx, ptrext.Of(attunev1.BatchResolveExternalSyncConflictsRequest{Ids: []string{id}})); err == nil {
		t.Fatal("BatchResolveConflicts service error returned nil")
	}
	if _, err := handler.ListEvents(ctx, ptrext.Of(attunev1.ListExternalSyncEventsRequest{})); err == nil {
		t.Fatal("ListEvents service error returned nil")
	}
	if _, err := handler.ReplayEvent(ctx, ptrext.Of(attunev1.ReplayExternalSyncEventRequest{Id: id})); err == nil {
		t.Fatal("ReplayEvent service error returned nil")
	}
}

func TestHandlerListBinderRejectsBadFilters(t *testing.T) {
	handler := ptrext.Of(Handler{service: ptrext.Of(fakeHandlerService{})})
	ctx := handlerTestContext()
	if _, err := handler.ListRuns(ctx, ptrext.Of(attunev1.ListExternalSyncRunsRequest{MappingId: "bad"})); err == nil {
		t.Fatal("ListRuns invalid mapping id returned nil error")
	}
	if _, err := handler.ListRuns(ctx, ptrext.Of(attunev1.ListExternalSyncRunsRequest{BeforeId: "bad"})); err == nil {
		t.Fatal("ListRuns invalid before id returned nil error")
	}
	if _, err := handler.ListEvents(ctx, ptrext.Of(attunev1.ListExternalSyncEventsRequest{BeforeId: "bad"})); err == nil {
		t.Fatal("ListEvents invalid before id returned nil error")
	}
	if err := BindListRunsRequest(httptest.NewRequest(http.MethodGet, "/runs?limit=0", nil), ptrext.Of(attunev1.ListExternalSyncRunsRequest{})); err == nil {
		t.Fatal("BindListRunsRequest invalid limit returned nil error")
	}
	if err := BindListEventsRequest(httptest.NewRequest(http.MethodGet, "/events?limit=nan", nil), ptrext.Of(attunev1.ListExternalSyncEventsRequest{})); err == nil {
		t.Fatal("BindListEventsRequest invalid limit returned nil error")
	}
	if got := queryValue(httptest.NewRequest(http.MethodGet, "/events", nil), "missing"); got != "" {
		t.Fatalf("queryValue missing = %q; want empty", got)
	}
}

func TestHandlerNotFoundAndProviderProbeErrors(t *testing.T) {
	handler := ptrext.Of(Handler{service: ptrext.Of(fakeHandlerService{
		testResult: externalsynccore.CheckResult{OK: false, Error: "provider missing"},
		testErr:    svc.ErrProviderUnavailable,
	})})
	ctx := handlerTestContext()
	if result, err := handler.TestConnection(ctx, ptrext.Of(attunev1.TestExternalConnectionRequest{Id: uuid.NewString()})); err != nil || result.Body.GetOk() {
		t.Fatalf("TestConnection provider unavailable = %+v err=%v; want OK response with failed body", result, err)
	}
	if _, err := handler.UpdateConnection(ctx, ptrext.Of(attunev1.UpdateExternalConnectionRequest{Id: uuid.NewString()})); err == nil {
		t.Fatal("UpdateConnection not found returned nil error")
	}
	if _, err := handler.GetRun(ctx, ptrext.Of(attunev1.GetExternalSyncRunRequest{Id: uuid.NewString()})); err == nil {
		t.Fatal("GetRun not found returned nil error")
	}
	if _, err := handler.GetEvent(ctx, ptrext.Of(attunev1.GetExternalSyncEventRequest{Id: uuid.NewString()})); err == nil {
		t.Fatal("GetEvent not found returned nil error")
	}
}

func TestReplayEventReturnsAcceptedRun(t *testing.T) {
	eventID := uuid.New()
	runID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{
		replayEvent: ptrext.Of(repo.SyncEvent{
			ID:              eventID,
			TenantID:        "tenant-1",
			ConnectionID:    uuid.New(),
			Provider:        "github",
			EventType:       "issues",
			DedupeKey:       "github:issues:delivery-1",
			SignatureStatus: repo.EventSignatureVerified,
			Status:          repo.EventStatusReplayed,
			PayloadDigest:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			RunID:           ptrext.Of(runID),
			CreatedAt:       time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC),
			UpdatedAt:       time.Date(2026, 7, 8, 1, 2, 4, 0, time.UTC),
		}),
		replayRun: ptrext.Of(repo.SyncRun{
			ID:        runID,
			TenantID:  "tenant-1",
			Direction: repo.DirectionPull,
			Trigger:   repo.TriggerWebhook,
			Status:    repo.RunStatusQueued,
			CreatedAt: time.Date(2026, 7, 8, 1, 2, 5, 0, time.UTC),
			UpdatedAt: time.Date(2026, 7, 8, 1, 2, 5, 0, time.UTC),
		}),
	})
	handler := ptrext.Of(Handler{service: fake})

	result, err := handler.ReplayEvent(handlerTestContext(), ptrext.Of(attunev1.ReplayExternalSyncEventRequest{
		Id: eventID.String(),
	}))
	if err != nil {
		t.Fatalf("ReplayEvent returned error: %v", err)
	}
	if result.Status != http.StatusAccepted {
		t.Fatalf("status = %d; want accepted", result.Status)
	}
	if result.Body.GetEvent().GetStatus() != attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_REPLAYED ||
		result.Body.GetRun().GetTrigger() != attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_WEBHOOK {
		t.Fatalf("response = %#v; want replayed event and webhook run", result.Body)
	}
}

func TestResetCursorReturnsAcceptedRun(t *testing.T) {
	mappingID := uuid.New()
	runID := uuid.New()
	connectionID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{
		resetCursorResult: ptrext.Of(repo.ResetCursorResult{
			Mapping: repo.Mapping{
				ID:                 mappingID,
				TenantID:           "tenant-1",
				ConnectionID:       connectionID,
				LocalObjectType:    "customer_request",
				ExternalObjectType: "issue",
				Direction:          repo.DirectionPull,
				Enabled:            true,
			},
			Run: repo.SyncRun{
				ID:           runID,
				TenantID:     "tenant-1",
				ConnectionID: connectionID,
				MappingID:    ptrext.Of(mappingID),
				Direction:    repo.DirectionPull,
				Trigger:      repo.TriggerManual,
				Status:       repo.RunStatusQueued,
				CreatedAt:    time.Date(2026, 7, 8, 1, 2, 5, 0, time.UTC),
				UpdatedAt:    time.Date(2026, 7, 8, 1, 2, 5, 0, time.UTC),
			},
		}),
	})
	handler := ptrext.Of(Handler{service: fake})

	result, err := handler.ResetCursor(handlerTestContext(), ptrext.Of(attunev1.ResetExternalSyncCursorRequest{
		Id: mappingID.String(),
	}))
	if err != nil {
		t.Fatalf("ResetCursor returned error: %v", err)
	}
	if result.Status != http.StatusAccepted {
		t.Fatalf("status = %d; want accepted", result.Status)
	}
	if fake.resetCursorInput.ID != mappingID || fake.resetCursorInput.TenantID != "tenant-1" ||
		fake.resetCursorInput.Actor.ID != "admin-1" {
		t.Fatalf("service input = %#v; want tenant, mapping, actor", fake.resetCursorInput)
	}
	if result.Body.GetMapping().GetId() != mappingID.String() ||
		result.Body.GetRun().GetTrigger() != attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_MANUAL {
		t.Fatalf("response = %#v; want mapping and manual run", result.Body)
	}
}

func TestRequestBackfillReturnsAcceptedRun(t *testing.T) {
	mappingID := uuid.New()
	runID := uuid.New()
	connectionID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{
		backfillResult: ptrext.Of(repo.BackfillResult{
			Mapping: repo.Mapping{
				ID:                 mappingID,
				TenantID:           "tenant-1",
				ConnectionID:       connectionID,
				LocalObjectType:    "customer_request",
				ExternalObjectType: "issue",
				Direction:          repo.DirectionPull,
				Enabled:            true,
			},
			Run: repo.SyncRun{
				ID:           runID,
				TenantID:     "tenant-1",
				ConnectionID: connectionID,
				MappingID:    ptrext.Of(mappingID),
				Direction:    repo.DirectionPull,
				Trigger:      repo.TriggerBackfill,
				Status:       repo.RunStatusQueued,
				CreatedAt:    time.Date(2026, 7, 8, 1, 2, 5, 0, time.UTC),
				UpdatedAt:    time.Date(2026, 7, 8, 1, 2, 5, 0, time.UTC),
			},
		}),
	})
	handler := ptrext.Of(Handler{service: fake})

	result, err := handler.RequestBackfill(handlerTestContext(), ptrext.Of(attunev1.RequestExternalSyncBackfillRequest{
		Id:          mappingID.String(),
		ResetCursor: true,
	}))
	if err != nil {
		t.Fatalf("RequestBackfill returned error: %v", err)
	}
	if result.Status != http.StatusAccepted {
		t.Fatalf("status = %d; want accepted", result.Status)
	}
	if fake.backfillInput.ID != mappingID || !fake.backfillInput.ResetCursor ||
		fake.backfillInput.Actor.ID != "admin-1" {
		t.Fatalf("service input = %#v; want mapping, reset flag, actor", fake.backfillInput)
	}
	if result.Body.GetMapping().GetId() != mappingID.String() ||
		result.Body.GetRun().GetTrigger() != attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_BACKFILL {
		t.Fatalf("response = %#v; want mapping and backfill run", result.Body)
	}
}

func TestDiscoverConnectionSchemaReturnsProviderSchemas(t *testing.T) {
	connectionID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{
		schemas: []externalsynccore.ObjectSchema{
			{
				Type:           "issue",
				Fields:         []string{"title", "state"},
				RequiredFields: []string{"title"},
				WritableFields: []string{"title", "state"},
			},
		},
	})
	handler := ptrext.Of(Handler{service: fake})

	result, err := handler.DiscoverConnectionSchema(handlerTestContext(), ptrext.Of(attunev1.DiscoverExternalConnectionSchemaRequest{
		Id: connectionID.String(),
	}))
	if err != nil {
		t.Fatalf("DiscoverConnectionSchema returned error: %v", err)
	}
	if fake.discoverID != connectionID {
		t.Fatalf("service id = %s; want %s", fake.discoverID, connectionID)
	}
	if len(result.Body.GetSchemas()) != 1 ||
		result.Body.GetSchemas()[0].GetType() != "issue" ||
		!reflect.DeepEqual(result.Body.GetSchemas()[0].GetFields(), []string{"title", "state"}) ||
		!reflect.DeepEqual(result.Body.GetSchemas()[0].GetRequiredFields(), []string{"title"}) ||
		!reflect.DeepEqual(result.Body.GetSchemas()[0].GetWritableFields(), []string{"title", "state"}) {
		t.Fatalf("schemas = %#v; want issue schema", result.Body.GetSchemas())
	}
}

func TestPreviewMappingReturnsDiagnostics(t *testing.T) {
	mappingID := uuid.New()
	fieldMappingJSON := `{"title":"title"}`
	statusMappingJSON := `{"closed":"done"}`
	fake := ptrext.Of(fakeHandlerService{
		previewResult: svc.MappingPreview{
			Schema: externalsynccore.ObjectSchema{
				Type:           "issue",
				Fields:         []string{"title", "state"},
				RequiredFields: []string{"title"},
				WritableFields: []string{"title", "state"},
			},
			Errors:   []string{"missing required field mapping target owner"},
			Warnings: []string{"field mapping target number is read-only"},
		},
	})
	handler := ptrext.Of(Handler{service: fake})

	result, err := handler.PreviewMapping(handlerTestContext(), ptrext.Of(attunev1.PreviewExternalObjectMappingRequest{
		Id:                mappingID.String(),
		FieldMappingJson:  ptrext.Of(fieldMappingJSON),
		StatusMappingJson: ptrext.Of(statusMappingJSON),
	}))
	if err != nil {
		t.Fatalf("PreviewMapping returned error: %v", err)
	}
	if fake.previewInput.ID != mappingID ||
		fake.previewInput.FieldMappingJSON == nil ||
		ptrext.Indirect(fake.previewInput.FieldMappingJSON) != fieldMappingJSON ||
		fake.previewInput.StatusMappingJSON == nil ||
		ptrext.Indirect(fake.previewInput.StatusMappingJSON) != statusMappingJSON {
		t.Fatalf("service input = %#v; want mapping and draft JSON", fake.previewInput)
	}
	if result.Body.GetSchema().GetType() != "issue" ||
		!reflect.DeepEqual(result.Body.GetErrors(), []string{"missing required field mapping target owner"}) ||
		!reflect.DeepEqual(result.Body.GetWarnings(), []string{"field mapping target number is read-only"}) {
		t.Fatalf("response = %#v; want schema diagnostics", result.Body)
	}
}

func TestRecordTimelineReturnsEntries(t *testing.T) {
	mappingID := uuid.New()
	runID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{
		timelineRows: []repo.RecordTimelineEntry{
			{
				Kind:          "failure",
				OccurredAt:    time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC),
				RunID:         ptrext.Of(runID),
				Status:        "open",
				Operation:     repo.DirectionPull,
				LocalObjectID: "cr-1",
				ExternalKey:   "#123",
				Summary:       "validation: title missing",
				Detail:        []byte(`{"retryable":true}`),
			},
		},
	})
	handler := ptrext.Of(Handler{service: fake})

	result, err := handler.RecordTimeline(handlerTestContext(), ptrext.Of(attunev1.GetExternalSyncRecordTimelineRequest{
		MappingId:     mappingID.String(),
		LocalObjectId: "cr-1",
		Limit:         5,
	}))
	if err != nil {
		t.Fatalf("RecordTimeline returned error: %v", err)
	}
	if fake.timelineInput.MappingID != mappingID ||
		fake.timelineInput.LocalObjectID != "cr-1" ||
		fake.timelineInput.Limit != 5 {
		t.Fatalf("service input = %#v; want record selector", fake.timelineInput)
	}
	if len(result.Body.GetEntries()) != 1 ||
		result.Body.GetEntries()[0].GetKind() != "failure" ||
		result.Body.GetEntries()[0].GetRunId() != runID.String() ||
		result.Body.GetEntries()[0].GetDetailJson() != `{"retryable":true}` {
		t.Fatalf("entries = %#v; want converted timeline row", result.Body.GetEntries())
	}
}

func TestHandlerConnectionListAndUpdateEndpointsReturnRows(t *testing.T) {
	now := time.Date(2026, 7, 8, 6, 7, 8, 0, time.UTC)
	connectionID := uuid.New()
	fake := newConnectionEndpointFakeService(now, connectionID)
	handler := ptrext.Of(Handler{service: fake})

	listed, err := handler.ListConnections(handlerTestContext(), ptrext.Of(attunev1.ListExternalConnectionsRequest{}))
	if err != nil {
		t.Fatalf("ListConnections returned error: %v", err)
	}
	if len(listed.Body.GetConnections()) != 1 {
		t.Fatalf("listed connections = %#v; want one connection", listed.Body.GetConnections())
	}
	if !listed.Body.GetConnections()[0].GetWebhookSecretConfigured() {
		t.Fatalf("listed connections = %#v; want webhook-enabled connection", listed.Body.GetConnections())
	}

	enabled := false
	updated, err := handler.UpdateConnection(handlerTestContext(), ptrext.Of(attunev1.UpdateExternalConnectionRequest{
		Id:                 connectionID.String(),
		Name:               ptrext.Of("GitHub Issues"),
		Enabled:            ptrext.Of(enabled),
		Credential:         ptrext.Of("new-token"),
		WebhookSecret:      ptrext.Of("new-webhook"),
		BaseUrl:            ptrext.Of("https://api.github.com"),
		ProviderConfigJson: ptrext.Of(`{"repo":"acme/app"}`),
		Scopes:             []string{"issues", "metadata"},
	}))
	if err != nil {
		t.Fatalf("UpdateConnection returned error: %v", err)
	}
	if fake.updateInput.ID != connectionID {
		t.Fatalf("update id = %s; want %s", fake.updateInput.ID, connectionID)
	}
	if fake.updateInput.Name == nil || ptrext.Indirect(fake.updateInput.Name) != "GitHub Issues" {
		t.Fatalf("update name = %#v; want GitHub Issues", fake.updateInput.Name)
	}
	if fake.updateInput.Enabled == nil || ptrext.Indirect(fake.updateInput.Enabled) {
		t.Fatalf("update enabled = %#v; want explicit false", fake.updateInput.Enabled)
	}
	if updated.Body.GetStatus() != repo.ConnectionStatusDisabled {
		t.Fatalf("updated status = %q; want disabled", updated.Body.GetStatus())
	}
}

func TestHandlerConnectionDeleteAndProbeEndpointsReturnRows(t *testing.T) {
	now := time.Date(2026, 7, 8, 6, 7, 8, 0, time.UTC)
	connectionID := uuid.New()
	fake := newConnectionEndpointFakeService(now, connectionID)
	handler := ptrext.Of(Handler{service: fake})

	if _, err := handler.DeleteConnection(handlerTestContext(), ptrext.Of(attunev1.DeleteExternalConnectionRequest{Id: connectionID.String()})); err != nil {
		t.Fatalf("DeleteConnection returned error: %v", err)
	}
	if fake.deletedID != connectionID {
		t.Fatalf("deleted id = %s; want %s", fake.deletedID, connectionID)
	}

	tested, err := handler.TestConnection(handlerTestContext(), ptrext.Of(attunev1.TestExternalConnectionRequest{Id: connectionID.String()}))
	if err != nil {
		t.Fatalf("TestConnection returned error: %v", err)
	}
	if fake.testID != connectionID {
		t.Fatalf("test id = %s; want %s", fake.testID, connectionID)
	}
	if !tested.Body.GetOk() {
		t.Fatalf("test response = %#v; want ok", tested.Body)
	}
	if tested.Body.GetLatencyMs() != 25 {
		t.Fatalf("latency = %d; want 25", tested.Body.GetLatencyMs())
	}
}

func newConnectionEndpointFakeService(now time.Time, connectionID uuid.UUID) *fakeHandlerService {
	return ptrext.Of(fakeHandlerService{
		listConnectionsRows: []repo.Connection{{
			ID:                      connectionID,
			TenantID:                "tenant-1",
			Provider:                "github",
			Name:                    "GitHub",
			Enabled:                 true,
			Status:                  repo.ConnectionStatusActive,
			AuthType:                "token",
			BaseURL:                 "https://api.github.com",
			ProviderConfig:          []byte(`{"repo":"acme/app"}`),
			Scopes:                  []string{"issues"},
			WebhookSecretKeyID:      "webhook-key",
			WebhookSecretCiphertext: []byte("ciphertext"),
			LastTestedAt:            ptrext.Of(now),
			LastTestStatus:          repo.TestStatusOK,
			CreatedBy:               "admin-1",
			UpdatedBy:               "admin-1",
			CreatedAt:               now,
			UpdatedAt:               now,
		}},
		updateConnection: ptrext.Of(repo.Connection{
			ID:        connectionID,
			TenantID:  "tenant-1",
			Provider:  "github",
			Name:      "GitHub Issues",
			Enabled:   false,
			Status:    repo.ConnectionStatusDisabled,
			AuthType:  "token",
			CreatedAt: now,
			UpdatedAt: now,
		}),
		testResult: externalsynccore.CheckResult{OK: true, Latency: 25 * time.Millisecond, RequestID: "gh-req-1"},
	})
}

func TestHandlerMappingEndpointsReturnRows(t *testing.T) {
	now := time.Date(2026, 7, 8, 6, 7, 8, 0, time.UTC)
	connectionID := uuid.New()
	mappingID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{
		mappingRows: []repo.Mapping{{
			ID:                 mappingID,
			TenantID:           "tenant-1",
			ConnectionID:       connectionID,
			LocalObjectType:    "customer_request",
			ExternalObjectType: "issue",
			Direction:          repo.DirectionBidirectional,
			FieldMapping:       []byte(`{"title":"title"}`),
			StatusMapping:      []byte(`{"closed":"done"}`),
			ConflictPolicy:     "manual",
			TombstonePolicy:    "mark_stale",
			Enabled:            true,
			MappingVersion:     2,
			CreatedAt:          now,
			UpdatedAt:          now,
		}},
		updateMapping: ptrext.Of(repo.Mapping{
			ID:                 mappingID,
			TenantID:           "tenant-1",
			ConnectionID:       connectionID,
			LocalObjectType:    "customer_request",
			ExternalObjectType: "issue",
			Direction:          repo.DirectionPush,
			FieldMapping:       []byte(`{"title":"summary"}`),
			StatusMapping:      []byte(`{}`),
			ConflictPolicy:     "external_wins",
			TombstonePolicy:    "ignore",
			Enabled:            false,
			MappingVersion:     3,
			CreatedAt:          now,
			UpdatedAt:          now,
		}),
	})
	handler := ptrext.Of(Handler{service: fake})

	mappings, err := handler.ListMappings(handlerTestContext(), ptrext.Of(attunev1.ListExternalObjectMappingsRequest{
		ConnectionId: connectionID.String(),
	}))
	if err != nil {
		t.Fatalf("ListMappings returned error: %v", err)
	}
	if fake.listMappingsConnectionID != connectionID {
		t.Fatalf("list mappings connection id = %s; want %s", fake.listMappingsConnectionID, connectionID)
	}
	if len(mappings.Body.GetMappings()) != 1 {
		t.Fatalf("mappings = %#v; want one mapping", mappings.Body.GetMappings())
	}
	if mappings.Body.GetMappings()[0].GetDirection() != attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL {
		t.Fatalf("mapping direction = %v; want bidirectional", mappings.Body.GetMappings()[0].GetDirection())
	}

	updatedMapping, err := handler.UpdateMapping(handlerTestContext(), ptrext.Of(attunev1.UpdateExternalObjectMappingRequest{
		Id:                mappingID.String(),
		Direction:         attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_PUSH,
		FieldMappingJson:  `{"title":"summary"}`,
		StatusMappingJson: `{}`,
		ConflictPolicy:    "external_wins",
		TombstonePolicy:   "ignore",
		Enabled:           ptrext.Of(false),
	}))
	if err != nil {
		t.Fatalf("UpdateMapping returned error: %v", err)
	}
	if fake.updateMappingInput.ID != mappingID {
		t.Fatalf("update mapping id = %s; want %s", fake.updateMappingInput.ID, mappingID)
	}
	if fake.updateMappingInput.Direction != repo.DirectionPush {
		t.Fatalf("update direction = %q; want push", fake.updateMappingInput.Direction)
	}
	if fake.updateMappingInput.Enabled == nil || ptrext.Indirect(fake.updateMappingInput.Enabled) {
		t.Fatalf("update enabled = %#v; want explicit false", fake.updateMappingInput.Enabled)
	}
	if updatedMapping.Body.GetMappingVersion() != 3 {
		t.Fatalf("mapping version = %d; want 3", updatedMapping.Body.GetMappingVersion())
	}
}

func TestHandlerRequestAndListRunEndpointsReturnRows(t *testing.T) {
	now := time.Date(2026, 7, 8, 6, 7, 8, 0, time.UTC)
	ids := newHandlerRunEndpointIDs()
	fake := newRunEndpointFakeService(now, ids)
	handler := ptrext.Of(Handler{service: fake})

	assertHandlerRequestRunEndpoint(t, handler, fake, ids)
	assertHandlerListRunsEndpoint(t, handler, fake, ids)
}

func assertHandlerRequestRunEndpoint(
	t *testing.T,
	handler *Handler,
	fake *fakeHandlerService,
	ids handlerRunEndpointIDs,
) {
	t.Helper()

	requested, err := handler.RequestRun(handlerTestContext(), ptrext.Of(attunev1.RequestExternalSyncRunRequest{
		ConnectionId:  ids.connectionID.String(),
		MappingId:     ids.mappingID.String(),
		Direction:     attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_PUSH,
		LocalObjectId: "cr-1",
		ExternalKey:   "42",
	}))
	if err != nil {
		t.Fatalf("RequestRun returned error: %v", err)
	}
	if requested.Status != http.StatusAccepted {
		t.Fatalf("request status = %d; want accepted", requested.Status)
	}
	if fake.requestRunInput.ConnectionID != ids.connectionID {
		t.Fatalf("request connection id = %s; want %s", fake.requestRunInput.ConnectionID, ids.connectionID)
	}
	if fake.requestRunInput.MappingID == nil || ptrext.Indirect(fake.requestRunInput.MappingID) != ids.mappingID {
		t.Fatalf("request mapping id = %#v; want %s", fake.requestRunInput.MappingID, ids.mappingID)
	}
	if fake.requestRunInput.LocalObjectID != "cr-1" || fake.requestRunInput.ExternalKey != "42" {
		t.Fatalf("request selector = %q/%q; want local/external selector", fake.requestRunInput.LocalObjectID, fake.requestRunInput.ExternalKey)
	}
	if requested.Body.GetDirection() != attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_PUSH {
		t.Fatalf("request direction = %v; want push", requested.Body.GetDirection())
	}
	if requested.Body.GetInputMetadataJson() != `{"local_object_id":"cr-1"}` {
		t.Fatalf("request input metadata = %s; want response metadata", requested.Body.GetInputMetadataJson())
	}
}

func assertHandlerListRunsEndpoint(
	t *testing.T,
	handler *Handler,
	fake *fakeHandlerService,
	ids handlerRunEndpointIDs,
) {
	t.Helper()

	runs, err := handler.ListRuns(handlerTestContext(), ptrext.Of(attunev1.ListExternalSyncRunsRequest{
		ConnectionId: ids.connectionID.String(),
		MappingId:    ids.mappingID.String(),
		Status:       repo.RunStatusRunning,
		Limit:        10,
	}))
	if err != nil {
		t.Fatalf("ListRuns returned error: %v", err)
	}
	if fake.listRunsInput.ConnectionID == nil || ptrext.Indirect(fake.listRunsInput.ConnectionID) != ids.connectionID {
		t.Fatalf("list connection id = %#v; want %s", fake.listRunsInput.ConnectionID, ids.connectionID)
	}
	if len(runs.Body.GetRuns()) != 1 {
		t.Fatalf("runs = %#v; want one run", runs.Body.GetRuns())
	}
	if !runs.Body.GetRuns()[0].GetInFlight() {
		t.Fatalf("run = %#v; want in-flight", runs.Body.GetRuns()[0])
	}
	if runs.Body.GetNextBeforeId() != "next-run" {
		t.Fatalf("next before id = %q; want next-run", runs.Body.GetNextBeforeId())
	}
}

func TestHandlerGetAndRetryRunEndpointsReturnRows(t *testing.T) {
	now := time.Date(2026, 7, 8, 6, 7, 8, 0, time.UTC)
	ids := newHandlerRunEndpointIDs()
	fake := newRunEndpointFakeService(now, ids)
	handler := ptrext.Of(Handler{service: fake})

	detail, err := handler.GetRun(handlerTestContext(), ptrext.Of(attunev1.GetExternalSyncRunRequest{Id: ids.runID.String()}))
	if err != nil {
		t.Fatalf("GetRun returned error: %v", err)
	}
	if fake.runDetailID != ids.runID {
		t.Fatalf("run detail id = %s; want %s", fake.runDetailID, ids.runID)
	}
	if len(detail.Body.GetAttempts()) != 1 {
		t.Fatalf("attempts = %#v; want one attempt", detail.Body.GetAttempts())
	}
	if len(detail.Body.GetFailures()) != 1 {
		t.Fatalf("failures = %#v; want one failure", detail.Body.GetFailures())
	}
	if len(detail.Body.GetConflicts()) != 1 {
		t.Fatalf("conflicts = %#v; want one conflict", detail.Body.GetConflicts())
	}

	retriedRun, err := handler.RetryRun(handlerTestContext(), ptrext.Of(attunev1.RetryExternalSyncRunRequest{Id: ids.retryRunID.String()}))
	if err != nil {
		t.Fatalf("RetryRun returned error: %v", err)
	}
	if retriedRun.Status != http.StatusAccepted {
		t.Fatalf("retry status = %d; want accepted", retriedRun.Status)
	}
	if fake.retryRunID != ids.retryRunID {
		t.Fatalf("retry run id = %s; want %s", fake.retryRunID, ids.retryRunID)
	}
}

func TestHandlerFailureAndConflictEndpointsReturnRows(t *testing.T) {
	now := time.Date(2026, 7, 8, 6, 7, 8, 0, time.UTC)
	mappingID := uuid.New()
	failureID := uuid.New()
	conflictID := uuid.New()
	retryRunID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{
		retryFailure: ptrext.Of(repo.RecordFailure{
			ID:                failureID,
			TenantID:          "tenant-1",
			RunID:             retryRunID,
			MappingID:         mappingID,
			Operation:         repo.DirectionPush,
			LocalObjectID:     "cr-2",
			ExternalKey:       "ISS-2",
			FailureKind:       "provider",
			Message:           "retry queued",
			PayloadDigest:     "sha256:def",
			RetryMode:         "replay",
			NormalizedPayload: []byte(`{"title":"Retry"}`),
			Retryable:         true,
			ResolvedAt:        ptrext.Of(now),
			ResolvedBy:        "admin-1",
			CreatedAt:         now,
		}),
		resolveConflict: ptrext.Of(repo.ConflictRow{
			ID:               conflictID,
			TenantID:         "tenant-1",
			MappingID:        mappingID,
			LocalObjectID:    "cr-1",
			ExternalKey:      "ISS-1",
			ConflictKind:     "version_mismatch",
			Status:           "resolved",
			LocalSnapshot:    []byte(`{"local":1}`),
			ExternalSnapshot: []byte(`{"external":1}`),
			Resolution:       "local_wins",
			ResolvedAt:       ptrext.Of(now),
			ResolvedBy:       "admin-1",
			CreatedAt:        now,
			UpdatedAt:        now,
		}),
	})
	handler := ptrext.Of(Handler{service: fake})

	retriedFailure, err := handler.RetryFailure(handlerTestContext(), ptrext.Of(attunev1.RetryExternalSyncFailureRequest{Id: failureID.String()}))
	if err != nil {
		t.Fatalf("RetryFailure returned error: %v", err)
	}
	if fake.retryFailureID != failureID {
		t.Fatalf("retry failure id = %s; want %s", fake.retryFailureID, failureID)
	}
	if retriedFailure.Body.GetRetryMode() != "replay" {
		t.Fatalf("retry mode = %q; want replay", retriedFailure.Body.GetRetryMode())
	}
	if !retriedFailure.Body.GetRetryable() {
		t.Fatalf("retry failure = %#v; want retryable", retriedFailure.Body)
	}

	resolved, err := handler.ResolveConflict(handlerTestContext(), ptrext.Of(attunev1.ResolveExternalSyncConflictRequest{
		Id:         conflictID.String(),
		Resolution: attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_LOCAL_WINS,
	}))
	if err != nil {
		t.Fatalf("ResolveConflict returned error: %v", err)
	}
	if fake.resolveConflictID != conflictID {
		t.Fatalf("resolve conflict id = %s; want %s", fake.resolveConflictID, conflictID)
	}
	if fake.resolveResolution != "local_wins" {
		t.Fatalf("resolve resolution = %q; want local_wins", fake.resolveResolution)
	}
	if resolved.Body.GetResolution() != "local_wins" {
		t.Fatalf("resolved conflict = %#v; want local_wins", resolved.Body)
	}
}

func TestHandlerEventEndpointsReturnRows(t *testing.T) {
	now := time.Date(2026, 7, 8, 6, 7, 8, 0, time.UTC)
	connectionID := uuid.New()
	mappingID := uuid.New()
	eventID := uuid.New()
	eventRunID := uuid.New()
	fake := ptrext.Of(fakeHandlerService{
		listEventsResult: repo.ListEventsResult{
			Events: []repo.SyncEvent{{
				ID:                eventID,
				TenantID:          "tenant-1",
				ConnectionID:      connectionID,
				MappingID:         ptrext.Of(mappingID),
				Provider:          "github",
				EventType:         "issues",
				ExternalEventID:   "delivery-1",
				DedupeKey:         "github:issues:delivery-1",
				SignatureStatus:   repo.EventSignatureFailed,
				Status:            repo.EventStatusFailed,
				PayloadDigest:     "sha256:evt",
				NormalizedPayload: []byte(`{"action":"opened"}`),
				ReceivedAt:        now,
				RunID:             ptrext.Of(eventRunID),
				FailureReason:     "bad signature",
				CreatedAt:         now,
				UpdatedAt:         now,
			}},
			NextBeforeID: "next-event",
		},
		getEvent: ptrext.Of(repo.SyncEvent{
			ID:                eventID,
			TenantID:          "tenant-1",
			ConnectionID:      connectionID,
			MappingID:         ptrext.Of(mappingID),
			Provider:          "github",
			EventType:         "issues",
			DedupeKey:         "github:issues:delivery-1",
			SignatureStatus:   repo.EventSignatureNotRequired,
			Status:            repo.EventStatusIgnored,
			PayloadDigest:     "sha256:evt",
			NormalizedPayload: []byte(`{"action":"ping"}`),
			ReceivedAt:        now,
			CreatedAt:         now,
			UpdatedAt:         now,
		}),
	})
	handler := ptrext.Of(Handler{service: fake})

	events, err := handler.ListEvents(handlerTestContext(), ptrext.Of(attunev1.ListExternalSyncEventsRequest{
		ConnectionId: connectionID.String(),
		Status:       repo.EventStatusFailed,
		Limit:        5,
	}))
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if fake.listEventsInput.ConnectionID == nil || ptrext.Indirect(fake.listEventsInput.ConnectionID) != connectionID {
		t.Fatalf("list events connection id = %#v; want %s", fake.listEventsInput.ConnectionID, connectionID)
	}
	if len(events.Body.GetEvents()) != 1 {
		t.Fatalf("events = %#v; want one event", events.Body.GetEvents())
	}
	if events.Body.GetEvents()[0].GetSignatureStatus() != attunev1.ExternalSyncEventSignatureStatus_EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_FAILED {
		t.Fatalf("signature status = %v; want failed", events.Body.GetEvents()[0].GetSignatureStatus())
	}
	if events.Body.GetNextBeforeId() != "next-event" {
		t.Fatalf("next before id = %q; want next-event", events.Body.GetNextBeforeId())
	}

	event, err := handler.GetEvent(handlerTestContext(), ptrext.Of(attunev1.GetExternalSyncEventRequest{Id: eventID.String()}))
	if err != nil {
		t.Fatalf("GetEvent returned error: %v", err)
	}
	if fake.getEventID != eventID {
		t.Fatalf("get event id = %s; want %s", fake.getEventID, eventID)
	}
	if event.Body.GetStatus() != attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_IGNORED {
		t.Fatalf("event status = %v; want ignored", event.Body.GetStatus())
	}
	if event.Body.GetSignatureStatus() != attunev1.ExternalSyncEventSignatureStatus_EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_NOT_REQUIRED {
		t.Fatalf("signature status = %v; want not required", event.Body.GetSignatureStatus())
	}
}

type handlerRunEndpointIDs struct {
	connectionID uuid.UUID
	mappingID    uuid.UUID
	runID        uuid.UUID
	failureID    uuid.UUID
	conflictID   uuid.UUID
	nextRunID    uuid.UUID
	retryRunID   uuid.UUID
}

func newHandlerRunEndpointIDs() handlerRunEndpointIDs {
	return handlerRunEndpointIDs{
		connectionID: uuid.New(),
		mappingID:    uuid.New(),
		runID:        uuid.New(),
		failureID:    uuid.New(),
		conflictID:   uuid.New(),
		nextRunID:    uuid.New(),
		retryRunID:   uuid.New(),
	}
}

func newRunEndpointFakeService(now time.Time, ids handlerRunEndpointIDs) *fakeHandlerService {
	return ptrext.Of(fakeHandlerService{
		requestRun: ptrext.Of(repo.SyncRun{
			ID:            ids.runID,
			TenantID:      "tenant-1",
			ConnectionID:  ids.connectionID,
			MappingID:     ptrext.Of(ids.mappingID),
			Direction:     repo.DirectionPush,
			Trigger:       repo.TriggerManual,
			Status:        repo.RunStatusQueued,
			ActorID:       "admin-1",
			InputMetadata: []byte(`{"local_object_id":"cr-1"}`),
			CreatedAt:     now,
			UpdatedAt:     now,
		}),
		listRunsResult: repo.ListRunsResult{
			Runs: []repo.SyncRun{{
				ID:           ids.nextRunID,
				TenantID:     "tenant-1",
				ConnectionID: ids.connectionID,
				MappingID:    ptrext.Of(ids.mappingID),
				Direction:    repo.DirectionPull,
				Trigger:      repo.TriggerRetry,
				Status:       repo.RunStatusRunning,
				ClaimedAt:    ptrext.Of(now),
				Attempts:     2,
				CreatedAt:    now,
				UpdatedAt:    now,
			}},
			NextBeforeID: "next-run",
		},
		runDetail: ptrext.Of(repo.RunDetail{
			Run: repo.SyncRun{
				ID:           ids.runID,
				TenantID:     "tenant-1",
				ConnectionID: ids.connectionID,
				MappingID:    ptrext.Of(ids.mappingID),
				Direction:    repo.DirectionPull,
				Trigger:      repo.TriggerWebhook,
				Status:       repo.RunStatusPartial,
				CreatedAt:    now,
				UpdatedAt:    now,
			},
			Attempts: []repo.SyncAttempt{{
				ID:                7,
				RunID:             ids.runID,
				AttemptNumber:     2,
				StartedAt:         now,
				FinishedAt:        ptrext.Of(now.Add(time.Second)),
				Result:            "retryable_error",
				HTTPStatus:        http.StatusTooManyRequests,
				ProviderRequestID: "gh-req-2",
				RetryAfter:        ptrext.Of(now.Add(time.Minute)),
				ErrorKind:         "rate_limited",
				ErrorMessage:      "secondary limit",
			}},
			Failures: []repo.RecordFailure{{
				ID:                ids.failureID,
				TenantID:          "tenant-1",
				RunID:             ids.runID,
				MappingID:         ids.mappingID,
				Operation:         repo.DirectionPull,
				LocalObjectID:     "cr-1",
				ExternalKey:       "ISS-1",
				FailureKind:       "validation",
				Message:           "missing title",
				PayloadDigest:     "sha256:abc",
				RetryMode:         "refetch",
				NormalizedPayload: []byte(`{"title":""}`),
				Retryable:         true,
				CreatedAt:         now,
			}},
			Conflicts: []repo.ConflictRow{{
				ID:               ids.conflictID,
				TenantID:         "tenant-1",
				MappingID:        ids.mappingID,
				LocalObjectID:    "cr-1",
				ExternalKey:      "ISS-1",
				ConflictKind:     "version_mismatch",
				Status:           "open",
				LocalSnapshot:    []byte(`{"local":1}`),
				ExternalSnapshot: []byte(`{"external":1}`),
				CreatedAt:        now,
				UpdatedAt:        now,
			}},
		}),
		retryRun: ptrext.Of(repo.SyncRun{
			ID:           ids.retryRunID,
			TenantID:     "tenant-1",
			ConnectionID: ids.connectionID,
			MappingID:    ptrext.Of(ids.mappingID),
			Direction:    repo.DirectionPull,
			Trigger:      repo.TriggerRetry,
			Status:       repo.RunStatusQueued,
			CreatedAt:    now,
			UpdatedAt:    now,
		}),
	})
}

func handlerTestContext() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "admin-1",
			UserType: "admin",
		}),
	})
}

type fakeHandlerService struct {
	listConnectionsErr       error
	createErr                error
	listMappingsErr          error
	listRunsErr              error
	recordTimelineErr        error
	listEventsErr            error
	listConnectionsRows      []repo.Connection
	createInput              svc.CreateConnectionInput
	updateInput              svc.UpdateConnectionInput
	updateConnection         *repo.Connection
	deletedID                uuid.UUID
	testID                   uuid.UUID
	testResult               externalsynccore.CheckResult
	testErr                  error
	resumeInput              svc.ResumeConnectionInput
	resumeErr                error
	qualifyID                uuid.UUID
	qualifyResult            svc.QualificationResult
	qualifyErr               error
	discoverID               uuid.UUID
	schemas                  []externalsynccore.ObjectSchema
	discoverErr              error
	listMappingsConnectionID uuid.UUID
	mappingRows              []repo.Mapping
	updateMappingInput       svc.UpdateMappingInput
	updateMapping            *repo.Mapping
	previewInput             svc.PreviewMappingInput
	previewResult            svc.MappingPreview
	previewErr               error
	resetCursorInput         svc.ResetCursorInput
	resetCursorResult        *repo.ResetCursorResult
	backfillInput            svc.BackfillInput
	backfillResult           *repo.BackfillResult
	requestRunInput          svc.RequestRunInput
	requestRun               *repo.SyncRun
	listRunsInput            svc.ListRunsInput
	listRunsResult           repo.ListRunsResult
	runDetailID              uuid.UUID
	runDetail                *repo.RunDetail
	timelineInput            svc.RecordTimelineInput
	timelineRows             []repo.RecordTimelineEntry
	retryRunID               uuid.UUID
	retryRun                 *repo.SyncRun
	retryFailureID           uuid.UUID
	retryFailure             *repo.RecordFailure
	resolveConflictID        uuid.UUID
	resolveResolution        string
	resolveConflict          *repo.ConflictRow
	batchResolveInput        svc.BatchResolveConflictsInput
	batchResolveRows         []repo.ConflictRow
	batchResolveErr          error
	listEventsInput          svc.ListEventsInput
	listEventsResult         repo.ListEventsResult
	getEventID               uuid.UUID
	getEvent                 *repo.SyncEvent
	replayEvent              *repo.SyncEvent
	replayRun                *repo.SyncRun
	health                   repo.Health
	healthErr                error
}

func (s *fakeHandlerService) ListConnections(context.Context, string) ([]repo.Connection, error) {
	if s.listConnectionsErr != nil {
		return nil, s.listConnectionsErr
	}
	return s.listConnectionsRows, nil
}

func (s *fakeHandlerService) CreateConnection(_ context.Context, in svc.CreateConnectionInput) (*repo.Connection, error) {
	s.createInput = in
	if s.createErr != nil {
		return nil, s.createErr
	}
	now := time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC)
	status := repo.ConnectionStatusDisabled
	if in.Enabled {
		status = repo.ConnectionStatusActive
	}
	return ptrext.Of(repo.Connection{
		ID:             uuid.New(),
		TenantID:       in.TenantID,
		Provider:       in.Provider,
		Name:           in.Name,
		Enabled:        in.Enabled,
		Status:         status,
		AuthType:       in.AuthType,
		BaseURL:        in.BaseURL,
		ProviderConfig: []byte(in.ProviderConfigJSON),
		Scopes:         in.Scopes,
		CreatedBy:      in.Actor.ID,
		UpdatedBy:      in.Actor.ID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}), nil
}

func (s *fakeHandlerService) UpdateConnection(_ context.Context, in svc.UpdateConnectionInput) (*repo.Connection, error) {
	s.updateInput = in
	if s.updateConnection == nil {
		return nil, repo.ErrConnectionNotFound
	}
	return s.updateConnection, nil
}

func (s *fakeHandlerService) DeleteConnection(_ context.Context, _ string, id uuid.UUID, _ svc.Actor, _ auditlogsvc.Actor) error {
	s.deletedID = id
	if id == uuid.Nil {
		return repo.ErrConnectionNotFound
	}
	return nil
}

func (s *fakeHandlerService) TestConnection(_ context.Context, _ string, id uuid.UUID, _ auditlogsvc.Actor) (externalsynccore.CheckResult, error) {
	s.testID = id
	return s.testResult, s.testErr
}

func (s *fakeHandlerService) ResumeConnection(_ context.Context, in svc.ResumeConnectionInput) (*repo.Connection, error) {
	s.resumeInput = in
	if s.resumeErr != nil {
		return nil, s.resumeErr
	}
	now := time.Date(2026, 7, 8, 2, 3, 4, 0, time.UTC)
	return ptrext.Of(repo.Connection{
		ID:        in.ID,
		TenantID:  in.TenantID,
		Provider:  "github",
		Name:      "GitHub",
		Enabled:   true,
		Status:    repo.ConnectionStatusActive,
		AuthType:  "token",
		UpdatedBy: in.Actor.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}), nil
}

func (s *fakeHandlerService) QualifyConnection(_ context.Context, _ string, id uuid.UUID, _ auditlogsvc.Actor) (svc.QualificationResult, error) {
	s.qualifyID = id
	if s.qualifyErr != nil {
		return svc.QualificationResult{}, s.qualifyErr
	}
	return s.qualifyResult, nil
}

func (s *fakeHandlerService) DiscoverConnectionSchema(_ context.Context, _ string, id uuid.UUID) ([]externalsynccore.ObjectSchema, error) {
	s.discoverID = id
	if s.discoverErr != nil {
		return nil, s.discoverErr
	}
	return s.schemas, nil
}

func (s *fakeHandlerService) ListMappings(_ context.Context, _ string, connectionID uuid.UUID) ([]repo.Mapping, error) {
	s.listMappingsConnectionID = connectionID
	if s.listMappingsErr != nil {
		return nil, s.listMappingsErr
	}
	return s.mappingRows, nil
}

func (s *fakeHandlerService) UpdateMapping(_ context.Context, in svc.UpdateMappingInput) (*repo.Mapping, error) {
	s.updateMappingInput = in
	if s.updateMapping == nil {
		return nil, repo.ErrMappingNotFound
	}
	return s.updateMapping, nil
}

func (s *fakeHandlerService) PreviewMapping(_ context.Context, in svc.PreviewMappingInput) (svc.MappingPreview, error) {
	s.previewInput = in
	if s.previewErr != nil {
		return svc.MappingPreview{}, s.previewErr
	}
	return s.previewResult, nil
}

func (s *fakeHandlerService) ResetCursor(_ context.Context, in svc.ResetCursorInput) (*repo.ResetCursorResult, error) {
	s.resetCursorInput = in
	if s.resetCursorResult == nil {
		return nil, repo.ErrMappingNotFound
	}
	return s.resetCursorResult, nil
}

func (s *fakeHandlerService) RequestBackfill(_ context.Context, in svc.BackfillInput) (*repo.BackfillResult, error) {
	s.backfillInput = in
	if s.backfillResult == nil {
		return nil, repo.ErrMappingNotFound
	}
	return s.backfillResult, nil
}

func (s *fakeHandlerService) RequestRun(_ context.Context, in svc.RequestRunInput) (*repo.SyncRun, error) {
	s.requestRunInput = in
	if s.requestRun == nil {
		return nil, repo.ErrRunNotFound
	}
	return s.requestRun, nil
}

func (s *fakeHandlerService) ListRuns(_ context.Context, in svc.ListRunsInput) (repo.ListRunsResult, error) {
	s.listRunsInput = in
	if s.listRunsErr != nil {
		return repo.ListRunsResult{}, s.listRunsErr
	}
	return s.listRunsResult, nil
}

func (s *fakeHandlerService) GetRunDetail(_ context.Context, _ string, id uuid.UUID) (*repo.RunDetail, error) {
	s.runDetailID = id
	if s.runDetail == nil {
		return nil, repo.ErrRunNotFound
	}
	return s.runDetail, nil
}

func (s *fakeHandlerService) RecordTimeline(_ context.Context, in svc.RecordTimelineInput) ([]repo.RecordTimelineEntry, error) {
	s.timelineInput = in
	if s.recordTimelineErr != nil {
		return nil, s.recordTimelineErr
	}
	return s.timelineRows, nil
}

func (s *fakeHandlerService) RetryRun(_ context.Context, _ string, id uuid.UUID, _ svc.Actor, _ auditlogsvc.Actor) (*repo.SyncRun, error) {
	s.retryRunID = id
	if s.retryRun == nil {
		return nil, repo.ErrRunNotFound
	}
	return s.retryRun, nil
}

func (s *fakeHandlerService) RetryFailure(_ context.Context, _ string, id uuid.UUID, _ svc.Actor, _ auditlogsvc.Actor) (*repo.RecordFailure, error) {
	s.retryFailureID = id
	if s.retryFailure == nil {
		return nil, repo.ErrFailureNotFound
	}
	return s.retryFailure, nil
}

func (s *fakeHandlerService) ResolveConflict(_ context.Context, _ string, id uuid.UUID, resolution string, _ svc.Actor, _ auditlogsvc.Actor) (*repo.ConflictRow, error) {
	s.resolveConflictID = id
	s.resolveResolution = resolution
	if s.resolveConflict == nil {
		return nil, repo.ErrConflictNotFound
	}
	return s.resolveConflict, nil
}

func (s *fakeHandlerService) BatchResolveConflicts(_ context.Context, in svc.BatchResolveConflictsInput) (repo.BatchResolveConflictsResult, error) {
	s.batchResolveInput = in
	if s.batchResolveErr != nil {
		return repo.BatchResolveConflictsResult{}, s.batchResolveErr
	}
	return repo.BatchResolveConflictsResult{Conflicts: s.batchResolveRows}, nil
}

func (s *fakeHandlerService) ListEvents(_ context.Context, in svc.ListEventsInput) (repo.ListEventsResult, error) {
	s.listEventsInput = in
	if s.listEventsErr != nil {
		return repo.ListEventsResult{}, s.listEventsErr
	}
	return s.listEventsResult, nil
}

func (s *fakeHandlerService) GetEvent(_ context.Context, _ string, id uuid.UUID) (*repo.SyncEvent, error) {
	s.getEventID = id
	if s.getEvent == nil {
		return nil, repo.ErrEventNotFound
	}
	return s.getEvent, nil
}

func (s *fakeHandlerService) ReplayEvent(context.Context, string, uuid.UUID, svc.Actor, auditlogsvc.Actor) (*repo.SyncEvent, *repo.SyncRun, error) {
	if s.replayEvent == nil || s.replayRun == nil {
		return nil, nil, repo.ErrEventNotFound
	}
	return s.replayEvent, s.replayRun, nil
}

func (s *fakeHandlerService) Health(context.Context, string) (repo.Health, error) {
	return s.health, s.healthErr
}
