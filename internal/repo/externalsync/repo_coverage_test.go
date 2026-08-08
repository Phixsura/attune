// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestExternalSyncRepoConnectionMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableExternalSyncRepo(t)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000001")

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "ListConnections", call: func() error {
			_, err := r.ListConnections(ctx, tenantID)
			return err
		}},
		{name: "GetConnection", call: func() error {
			_, err := r.GetConnection(ctx, tenantID, connectionID)
			return err
		}},
		{name: "CreateConnection", call: func() error {
			_, err := r.CreateConnection(ctx, testExternalSyncConnection(tenantID, connectionID))
			return err
		}},
		{name: "UpdateConnection", call: func() error {
			_, err := r.UpdateConnection(ctx, testExternalSyncConnection(tenantID, connectionID), true, true)
			return err
		}},
		{name: "DeleteConnection", call: func() error {
			return r.DeleteConnection(ctx, tenantID, connectionID, "user-1")
		}},
		{name: "UpdateConnectionTestResult", call: func() error {
			_, err := r.UpdateConnectionTestResult(ctx, tenantID, connectionID, false, "rate limited")
			return err
		}},
		{name: "ResumeConnection", call: func() error {
			_, err := r.ResumeConnection(ctx, tenantID, connectionID, "user-1")
			return err
		}},
	} {
		expectExternalSyncRepoError(t, tc.name, tc.call)
	}
}

func TestExternalSyncRepoMappingMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableExternalSyncRepo(t)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000002")
	mappingID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000003")

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "ListMappings", call: func() error {
			_, err := r.ListMappings(ctx, tenantID, uuid.Nil)
			return err
		}},
		{name: "GetMapping", call: func() error {
			_, err := r.GetMapping(ctx, tenantID, mappingID)
			return err
		}},
		{name: "ResolveRunMappingExplicit", call: func() error {
			_, err := r.ResolveRunMapping(ctx, tenantID, connectionID, ptrext.Of(mappingID))
			return err
		}},
		{name: "ResolveRunMappingDefault", call: func() error {
			_, err := r.ResolveRunMapping(ctx, tenantID, connectionID, nil)
			return err
		}},
		{name: "loadMapping", call: func() error {
			_, err := r.loadMapping(ctx, tenantID, mappingID)
			return err
		}},
		{name: "UpdateMapping", call: func() error {
			_, err := r.UpdateMapping(ctx, Mapping{
				ID: mappingID, TenantID: tenantID, Direction: DirectionPull,
				FieldMapping: []byte(`{}`), StatusMapping: []byte(`{}`), Enabled: true,
			})
			return err
		}},
		{name: "ResetCursor", call: func() error {
			_, err := r.ResetCursor(ctx, tenantID, mappingID, "user-1")
			return err
		}},
		{name: "EnqueueBackfill", call: func() error {
			_, err := r.EnqueueBackfill(ctx, tenantID, mappingID, "user-1", true)
			return err
		}},
	} {
		expectExternalSyncRepoError(t, tc.name, tc.call)
	}
}

func TestExternalSyncRepoRunAndEventMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableExternalSyncRepo(t)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000004")
	mappingID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000005")
	runID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000006")
	eventID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000007")

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "InsertRun", call: func() error {
			_, err := r.InsertRun(ctx, SyncRun{
				ID: runID, TenantID: tenantID, ConnectionID: connectionID,
				MappingID: ptrext.Of(mappingID), Direction: DirectionPull, Trigger: TriggerManual, ActorID: "user-1",
			})
			return err
		}},
		{name: "CreateCustomerRequestIssueRun", call: func() error {
			_, err := r.CreateCustomerRequestIssueRun(ctx, CustomerRequestIssueCreateRunInput{
				TenantID:  tenantID,
				RequestID: uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000011"),
				ActorID:   "user-1",
			})
			return err
		}},
		{name: "ListRuns", call: func() error {
			_, err := r.ListRuns(ctx, ListRunsFilter{TenantID: tenantID, BeforeID: ptrext.Of(runID), Limit: 25})
			return err
		}},
		{name: "RecordEvent", call: func() error {
			_, err := r.RecordEvent(ctx, SyncEvent{
				ID: eventID, TenantID: tenantID, ConnectionID: connectionID, MappingID: ptrext.Of(mappingID),
				Provider: "github", EventType: "issues", DedupeKey: "github:issues:1",
				SignatureStatus: EventSignatureVerified, Status: EventStatusReceived, NormalizedPayload: []byte(`{"ok":true}`),
				ReceivedAt: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
			})
			return err
		}},
		{name: "ListEvents", call: func() error {
			_, err := r.ListEvents(ctx, ListEventsFilter{TenantID: tenantID, BeforeID: ptrext.Of(eventID), Limit: 25})
			return err
		}},
		{name: "GetEvent", call: func() error {
			_, err := r.GetEvent(ctx, tenantID, eventID)
			return err
		}},
		{name: "getEventByDedupe", call: func() error {
			_, err := r.getEventByDedupe(ctx, tenantID, connectionID, "github:issues:1")
			return err
		}},
		{name: "ReplayEvent", call: func() error {
			_, _, err := r.ReplayEvent(ctx, tenantID, eventID, "user-1", mappingID, DirectionPull)
			return err
		}},
		{name: "GetRunDetail", call: func() error {
			_, err := r.GetRunDetail(ctx, tenantID, runID)
			return err
		}},
		{name: "RecordTimeline", call: func() error {
			_, err := r.RecordTimeline(ctx, RecordTimelineFilter{TenantID: tenantID, MappingID: mappingID, Limit: 25})
			return err
		}},
	} {
		expectExternalSyncRepoError(t, tc.name, tc.call)
	}
}

func TestExternalSyncRepoApplyAndWorkerMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableExternalSyncRepo(t)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000008")
	mappingID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000009")
	runID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000010")

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "PrepareRunCursor", call: func() error {
			_, err := r.PrepareRunCursor(ctx, runID, "worker-1", tenantID, mappingID, "")
			return err
		}},
		{name: "ApplyPullResult", call: func() error {
			_, err := r.ApplyPullResult(ctx, ApplyPullInput{TenantID: tenantID, RunID: runID, ConnectionID: connectionID, MappingID: mappingID})
			return err
		}},
		{name: "PreparePushRecords", call: func() error {
			_, err := r.PreparePushRecords(ctx, runID, "worker-1", tenantID, mappingID, "github", 25)
			return err
		}},
		{name: "ApplyPushResult", call: func() error {
			_, err := r.ApplyPushResult(ctx, ApplyPushInput{TenantID: tenantID, RunID: runID, ConnectionID: connectionID, MappingID: mappingID})
			return err
		}},
		{name: "RecordAttempt", call: func() error {
			return r.RecordAttempt(ctx, AttemptInput{RunID: runID, Result: RunStatusFailed, ErrorKind: "rate_limited"})
		}},
		{name: "ClaimBatch", call: func() error {
			_, err := r.ClaimBatch(ctx, 10, "worker-1")
			return err
		}},
		{name: "RefreshRunClaim", call: func() error {
			_, err := r.RefreshRunClaim(ctx, runID, "worker-1")
			return err
		}},
		{name: "MarkRunSucceeded", call: func() error {
			_, err := r.MarkRunSucceeded(ctx, runID, "worker-1")
			return err
		}},
		{name: "MarkRunFailed", call: func() error {
			_, err := r.MarkRunFailed(ctx, runID, "worker-1", "rate_limited", "slow down", time.Minute, false)
			return err
		}},
	} {
		expectExternalSyncRepoError(t, tc.name, tc.call)
	}
}

func TestExternalSyncRepoAdminAndHealthMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableExternalSyncRepo(t)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000011")
	runID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000012")
	failureID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000013")
	conflictID := uuid.MustParse("bbbbbbbb-1000-4000-8000-000000000014")

	empty, err := r.ResolveConflicts(ctx, tenantID, nil, "resolved", "user-1")
	if err != nil || len(empty.Conflicts) != 0 {
		t.Fatalf("ResolveConflicts(empty) = %+v, %v; want empty result", empty, err)
	}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "QuarantineDegradedConnection", call: func() error {
			_, err := r.QuarantineDegradedConnection(ctx, tenantID, connectionID, "three failed runs")
			return err
		}},
		{name: "RetryRun", call: func() error {
			_, err := r.RetryRun(ctx, tenantID, runID)
			return err
		}},
		{name: "RetryFailure", call: func() error {
			_, err := r.RetryFailure(ctx, tenantID, failureID, "user-1")
			return err
		}},
		{name: "ResolveConflict", call: func() error {
			_, err := r.ResolveConflict(ctx, tenantID, conflictID, "resolved", "user-1")
			return err
		}},
		{name: "ResolveConflicts", call: func() error {
			_, err := r.ResolveConflicts(ctx, tenantID, []uuid.UUID{conflictID}, "ignored", "user-1")
			return err
		}},
		{name: "Health", call: func() error {
			_, err := r.Health(ctx, tenantID)
			return err
		}},
		{name: "MetricSnapshot", call: func() error {
			_, err := r.MetricSnapshot(ctx)
			return err
		}},
	} {
		expectExternalSyncRepoError(t, tc.name, tc.call)
	}
}

func TestExternalSyncRepoConnectionSuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000001")
	tx := ptrext.Of(fakeTx{
		queryRows: []fakeRows{{rows: []fakeRow{fakeExternalConnectionRow(tenantID, connectionID, now)}}},
		rows: []fakeRow{
			fakeExternalConnectionRow(tenantID, connectionID, now),
			fakeExternalConnectionRow(tenantID, connectionID, now),
			fakeExternalConnectionRow(tenantID, connectionID, now),
		},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
	})
	r := ptrext.Of(Repo{pool: tx})

	connections, err := r.ListConnections(ctx, tenantID)
	if err != nil || len(connections) != 1 || connections[0].ID != connectionID {
		t.Fatalf("ListConnections() = %+v, %v", connections, err)
	}
	connection, err := r.GetConnection(ctx, tenantID, connectionID)
	if err != nil || connection.Provider != "github" {
		t.Fatalf("GetConnection() = %+v, %v", connection, err)
	}
	if err := r.DeleteConnection(ctx, tenantID, connectionID, "user-1"); err != nil {
		t.Fatalf("DeleteConnection() error = %v", err)
	}
	tested, err := r.UpdateConnectionTestResult(ctx, tenantID, connectionID, true, "ok")
	if err != nil || tested.LastTestStatus != TestStatusOK {
		t.Fatalf("UpdateConnectionTestResult() = %+v, %v", tested, err)
	}
	resumed, err := r.ResumeConnection(ctx, tenantID, connectionID, "user-1")
	if err != nil || !resumed.Enabled {
		t.Fatalf("ResumeConnection() = %+v, %v", resumed, err)
	}
}

func TestExternalSyncRepoMappingQueueAndRunSuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000002")
	mappingID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000003")
	runID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000004")
	tx := ptrext.Of(fakeTx{
		queryRows: []fakeRows{{rows: []fakeRow{fakeMappingRow(mappingID, tenantID, connectionID, DirectionPull, now)}}},
		rows: []fakeRow{
			fakeMappingRow(mappingID, tenantID, connectionID, DirectionPull, now),
			fakeMappingRow(mappingID, tenantID, connectionID, DirectionPull, now),
			fakeMappingRow(mappingID, tenantID, connectionID, DirectionPull, now),
			fakeMappingRow(mappingID, tenantID, connectionID, DirectionPush, now),
			fakeMappingRow(mappingID, tenantID, connectionID, DirectionPull, now),
			fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerManual, RunStatusQueued, now),
			fakeMappingRow(mappingID, tenantID, connectionID, DirectionPull, now),
			fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerBackfill, RunStatusQueued, now),
			fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerManual, RunStatusQueued, now),
		},
	})
	r := ptrext.Of(Repo{pool: tx})

	mappings, err := r.ListMappings(ctx, tenantID, connectionID)
	requireExternalSyncCondition(t, "ListMappings", err, len(mappings) == 1, mappings)
	got, err := r.GetMapping(ctx, tenantID, mappingID)
	requireExternalSyncCondition(t, "GetMapping", err, got.ID == mappingID, got)
	got, err = r.ResolveRunMapping(ctx, tenantID, connectionID, ptrext.Of(mappingID))
	requireExternalSyncCondition(t, "ResolveRunMapping(explicit)", err, got.ID == mappingID, got)
	got, err = r.ResolveRunMapping(ctx, tenantID, connectionID, nil)
	requireExternalSyncCondition(t, "ResolveRunMapping(default)", err, got.ID == mappingID, got)
	got, err = r.UpdateMapping(ctx, Mapping{ID: mappingID, TenantID: tenantID, Direction: DirectionPull, FieldMapping: []byte(`{}`), StatusMapping: []byte(`{}`), Enabled: true})
	requireExternalSyncCondition(t, "UpdateMapping", err, got.Direction == DirectionPush, got)
	reset, err := r.ResetCursor(ctx, tenantID, mappingID, "user-1")
	requireExternalSyncCondition(t, "ResetCursor", err, reset.Run.ID == runID, reset)
	backfill, err := r.EnqueueBackfill(ctx, tenantID, mappingID, "user-1", true)
	requireExternalSyncCondition(t, "EnqueueBackfill", err, backfill.Mapping.ID == mappingID, backfill)
	run, err := r.InsertRun(ctx, SyncRun{ID: runID, TenantID: tenantID, ConnectionID: connectionID, MappingID: ptrext.Of(mappingID), Direction: DirectionPull, Trigger: TriggerManual})
	requireExternalSyncCondition(t, "InsertRun", err, run.ID == runID, run)
}

func TestExternalSyncRepoCustomerRequestIssueRunSuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000005")
	mappingID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000006")
	requestID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000007")
	runID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000008")
	tx := ptrext.Of(fakeTx{
		queryRows: []fakeRows{{rows: []fakeRow{fakeMappingRow(mappingID, tenantID, connectionID, DirectionBidirectional, now)}}},
		rows: []fakeRow{
			{values: []any{requestID}},
			{values: []any{false, false}},
			{values: []any{false}},
			{err: pgx.ErrNoRows},
			fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPush, TriggerManual, RunStatusQueued, now),
			fakeMappingRow(mappingID, tenantID, connectionID, DirectionPull, now),
			{values: []any{requestID}},
			{values: []any{true}},
			{err: pgx.ErrNoRows},
			fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerManual, RunStatusQueued, now),
		},
	})
	r := ptrext.Of(Repo{pool: tx})

	createRun, err := r.CreateCustomerRequestIssueRun(ctx, CustomerRequestIssueCreateRunInput{
		TenantID: tenantID, RequestID: requestID, ActorID: "user-1",
	})
	if err != nil || createRun.Mapping.ID != mappingID || createRun.Run.Direction != DirectionPush {
		t.Fatalf("CreateCustomerRequestIssueRun() = %+v, %v", createRun, err)
	}
	pullRun, err := r.CreateCustomerRequestIssuePullRun(ctx, CustomerRequestIssuePullRunInput{
		TenantID: tenantID, RequestID: requestID, ConnectionID: connectionID, MappingID: mappingID,
		ExternalKey: " Acme/attune#235 ", ActorID: "user-1",
	})
	if err != nil || pullRun.Mapping.ID != mappingID || pullRun.Run.Direction != DirectionPull {
		t.Fatalf("CreateCustomerRequestIssuePullRun() = %+v, %v", pullRun, err)
	}
}

func TestExternalSyncRepoProviderInstallationReadSuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 10, 30, 0, 0, time.UTC)
	tenantID := "tenant-1"
	installationID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000009")
	resourceID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000010")
	tx := ptrext.Of(fakeTx{
		queryRows: []fakeRows{
			{rows: []fakeRow{fakeProviderInstallationRow(tenantID, installationID, now)}},
			{rows: []fakeRow{fakeProviderInstallationResourceRow(tenantID, installationID, resourceID, now)}},
		},
		rows: []fakeRow{
			fakeProviderInstallationRow(tenantID, installationID, now),
			fakeProviderInstallationRow(tenantID, installationID, now),
			fakeProviderInstallationRow(tenantID, installationID, now),
		},
	})
	r := ptrext.Of(Repo{pool: tx})

	if got, err := r.ListProviderInstallations(ctx, tenantID); err != nil || len(got) != 1 {
		t.Fatalf("ListProviderInstallations() = %+v, %v", got, err)
	}
	if got, err := r.GetProviderInstallation(ctx, tenantID, installationID); err != nil || got.ID != installationID {
		t.Fatalf("GetProviderInstallation() = %+v, %v", got, err)
	}
	if got, err := r.UpdateProviderInstallationQualification(ctx, tenantID, installationID, "qualified", "ok", []byte(`{"full_app":true}`), "user-1"); err != nil || got.QualificationStatus != "qualified" {
		t.Fatalf("UpdateProviderInstallationQualification() = %+v, %v", got, err)
	}
	resources, err := r.ListProviderInstallationResources(ctx, tenantID, installationID)
	if err != nil || len(resources) != 1 || resources[0].ResourceKey != "acme/attune" {
		t.Fatalf("ListProviderInstallationResources() = %+v, %v", resources, err)
	}
}

func TestProviderInstallationResourceHelpers(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000011")
	ids := normalizedUUIDArray([]uuid.UUID{uuid.Nil, id, id})
	if len(ids) != 2 || ids[1] != id {
		t.Fatalf("normalizedUUIDArray() = %v", ids)
	}
	if got := normalizedUUIDArray(nil); len(got) != 0 {
		t.Fatalf("normalizedUUIDArray(nil) = %v, want empty slice", got)
	}
}

func TestExternalSyncRepoRunEventSuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 11, 0, 0, 0, time.UTC)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000013")
	mappingID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000014")
	runID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000015")
	nextRunID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000016")
	eventID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000017")
	nextEventID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000018")
	tx := ptrext.Of(fakeTx{
		queryRows: []fakeRows{
			{rows: []fakeRow{
				fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerManual, RunStatusQueued, now),
				fakeRunRow(nextRunID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerManual, RunStatusQueued, now),
			}},
			{rows: []fakeRow{
				fakeEventRow(eventID, tenantID, connectionID, ptrext.Of(mappingID), EventStatusReceived, now),
				fakeEventRow(nextEventID, tenantID, connectionID, ptrext.Of(mappingID), EventStatusReceived, now),
			}},
			{rows: []fakeRow{fakeTimelineRow(runID, now)}},
			{rows: []fakeRow{fakeAttemptRow(runID, now)}},
			{rows: []fakeRow{fakeFailureRow(tenantID, runID, mappingID, now)}},
			{rows: []fakeRow{fakeConflictRow(tenantID, mappingID, now)}},
		},
		rows: []fakeRow{
			fakeEventRow(eventID, tenantID, connectionID, ptrext.Of(mappingID), EventStatusReceived, now),
			fakeEventRow(eventID, tenantID, connectionID, ptrext.Of(mappingID), EventStatusReceived, now),
			fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerManual, RunStatusQueued, now),
		},
	})
	r := ptrext.Of(Repo{pool: tx})

	runs, err := r.ListRuns(ctx, ListRunsFilter{TenantID: tenantID, ConnectionID: ptrext.Of(connectionID), MappingID: ptrext.Of(mappingID), Status: RunStatusQueued, Limit: 1})
	requireExternalSyncCondition(t, "ListRuns", err, len(runs.Runs) == 1 && runs.NextBeforeID != "", runs)
	event, err := r.RecordEvent(ctx, SyncEvent{ID: eventID, TenantID: tenantID, ConnectionID: connectionID, MappingID: ptrext.Of(mappingID), Provider: "github", EventType: "issues", DedupeKey: "dedupe-1", SignatureStatus: EventSignatureVerified, Status: EventStatusReceived, NormalizedPayload: []byte(`{"action":"opened"}`), ReceivedAt: now})
	requireExternalSyncCondition(t, "RecordEvent", err, event.ID == eventID, event)
	events, err := r.ListEvents(ctx, ListEventsFilter{TenantID: tenantID, ConnectionID: ptrext.Of(connectionID), Status: EventStatusReceived, Limit: 1})
	requireExternalSyncCondition(t, "ListEvents", err, len(events.Events) == 1 && events.NextBeforeID != "", events)
	event, err = r.GetEvent(ctx, tenantID, eventID)
	requireExternalSyncCondition(t, "GetEvent", err, event.Status == EventStatusReceived, event)
	entries, err := r.RecordTimeline(ctx, RecordTimelineFilter{TenantID: tenantID, MappingID: mappingID, Limit: 25})
	requireExternalSyncCondition(t, "RecordTimeline", err, len(entries) == 1, entries)
	detail, err := r.GetRunDetail(ctx, tenantID, runID)
	requireExternalSyncCondition(t, "GetRunDetail", err, len(detail.Attempts) == 1 && len(detail.Failures) == 1 && len(detail.Conflicts) == 1, detail)
}

func TestExternalSyncRepoEventReplaySuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 11, 30, 0, 0, time.UTC)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000019")
	mappingID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000020")
	eventID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000021")
	runID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000022")

	replayTx := ptrext.Of(fakeTx{rows: []fakeRow{
		fakeEventRow(eventID, tenantID, connectionID, ptrext.Of(mappingID), EventStatusReceived, now),
		fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerWebhook, RunStatusQueued, now),
		fakeEventRowWithRun(eventID, tenantID, connectionID, mappingID, runID, now),
	}})
	r := ptrext.Of(Repo{pool: replayTx})
	event, run, err := r.ReplayEvent(ctx, tenantID, eventID, "user-1", mappingID, DirectionPull)
	if err != nil || event.Status != EventStatusReplayed || run.Trigger != TriggerWebhook {
		t.Fatalf("ReplayEvent() = %+v, %+v, %v", event, run, err)
	}

	enqueueTx := ptrext.Of(fakeTx{rows: []fakeRow{
		fakeEventRow(eventID, tenantID, connectionID, ptrext.Of(mappingID), EventStatusReceived, now),
		fakeMappingRow(mappingID, tenantID, connectionID, DirectionBidirectional, now),
		fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerWebhook, RunStatusQueued, now),
		fakeEventRowWithRun(eventID, tenantID, connectionID, mappingID, runID, now),
	}})
	r = ptrext.Of(Repo{pool: enqueueTx})
	event, run, err = r.EnqueueEventRun(ctx, tenantID, eventID, "user-1")
	if err != nil || event.Status != EventStatusReplayed || run.Direction != DirectionPull {
		t.Fatalf("EnqueueEventRun() = %+v, %+v, %v", event, run, err)
	}
}

func TestExternalSyncRepoApplySuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000023")
	mappingID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000024")
	runID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000025")
	requestID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000026")
	tx := ptrext.Of(fakeTx{
		rows: []fakeRow{
			fakeMappingRow(mappingID, tenantID, connectionID, DirectionPull, now),
			fakeMappingRow(mappingID, tenantID, connectionID, DirectionPush, now),
			{values: []any{true, []byte(`{"local_object_id":"` + requestID.String() + `","source":"customer_request_issue_create"}`)}},
			fakeMappingRow(mappingID, tenantID, connectionID, DirectionPush, now),
		},
		queryRows: []fakeRows{{rows: []fakeRow{fakePushCandidateRow(requestID, now)}}},
	})
	r := ptrext.Of(Repo{pool: tx})

	pullStats, err := r.ApplyPullResult(ctx, ApplyPullInput{TenantID: tenantID, RunID: runID, ConnectionID: connectionID, MappingID: mappingID, Provider: "github"})
	if err != nil || pullStats != (ApplyStats{}) {
		t.Fatalf("ApplyPullResult() = %+v, %v", pullStats, err)
	}
	pushStats, err := r.ApplyPushResult(ctx, ApplyPushInput{TenantID: tenantID, RunID: runID, ConnectionID: connectionID, MappingID: mappingID, Provider: "github"})
	if err != nil || pushStats != (ApplyStats{}) {
		t.Fatalf("ApplyPushResult() = %+v, %v", pushStats, err)
	}
	records, err := r.PreparePushRecords(ctx, runID, "worker-1", tenantID, mappingID, "github", 10)
	if err != nil || len(records) != 1 || records[0].LocalObjectID != requestID.String() {
		t.Fatalf("PreparePushRecords() = %+v, %v", records, err)
	}
}

func TestExternalSyncRepoWorkerAndAdminSuccessPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 12, 30, 0, 0, time.UTC)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000027")
	mappingID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000028")
	runID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000029")
	tx := ptrext.Of(fakeTx{
		rows: []fakeRow{
			{values: []any{[]byte(`{"page":2}`)}},
			fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerRetry, RunStatusQueued, now),
			fakeConflictRow(tenantID, mappingID, now),
			fakeHealthRow(now),
		},
		queryRows: []fakeRows{
			{rows: []fakeRow{fakeRunRow(runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerManual, RunStatusRunning, now)}},
			{rows: []fakeRow{fakeConflictRow(tenantID, mappingID, now)}},
			{rows: []fakeRow{fakeMetricPointRow()}},
		},
	})
	r := ptrext.Of(Repo{pool: tx})

	cursor, err := r.PrepareRunCursor(ctx, runID, "worker-1", tenantID, mappingID, StreamDefault)
	requireExternalSyncCondition(t, "PrepareRunCursor", err, string(cursor) == `{"page":2}`, string(cursor))
	requireExternalSyncNoError(t, "RecordAttempt", r.RecordAttempt(ctx, AttemptInput{RunID: runID, Result: RunStatusSucceeded, HTTPStatus: 200, ProviderRequestID: "request-1"}))
	runs, err := r.ClaimBatch(ctx, 1, "worker-1")
	requireExternalSyncCondition(t, "ClaimBatch", err, len(runs) == 1 && runs[0].Status == RunStatusRunning, runs)
	expectAffectedRows(t, "RefreshRunClaim", func() (int64, error) { return r.RefreshRunClaim(ctx, runID, "worker-1") })
	expectAffectedRows(t, "MarkRunSucceeded", func() (int64, error) { return r.MarkRunSucceeded(ctx, runID, "worker-1") })
	expectAffectedRows(t, "MarkRunFailed", func() (int64, error) {
		return r.MarkRunFailed(ctx, runID, "worker-1", "provider_unavailable", "slow", time.Minute, false)
	})
	expectAffectedRows(t, "QuarantineDegradedConnection", func() (int64, error) {
		return r.QuarantineDegradedConnection(ctx, tenantID, connectionID, "recent failures")
	})
	run, err := r.RetryRun(ctx, tenantID, runID)
	requireExternalSyncCondition(t, "RetryRun", err, run.Trigger == TriggerRetry, run)
	conflict, err := r.ResolveConflict(ctx, tenantID, mappingID, "provider", "user-1")
	requireExternalSyncCondition(t, "ResolveConflict", err, conflict.Status != "open", conflict)
	result, err := r.ResolveConflicts(ctx, tenantID, []uuid.UUID{mappingID}, "ignored", "user-1")
	requireExternalSyncCondition(t, "ResolveConflicts", err, len(result.Conflicts) == 1, result)
	health, err := r.Health(ctx, tenantID)
	requireExternalSyncCondition(t, "Health", err, health.EnabledConnections == 2, health)
	snapshot, err := r.MetricSnapshot(ctx)
	requireExternalSyncCondition(t, "MetricSnapshot", err, len(snapshot.Points) == 1, snapshot)
}

func newUnreachableExternalSyncRepo(t *testing.T) *Repo {
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

func testExternalSyncConnection(tenantID string, id uuid.UUID) Connection {
	return Connection{
		ID: id, TenantID: tenantID, Provider: "github", Name: "GitHub",
		Enabled: true, Status: ConnectionStatusActive, AuthType: "token",
		BaseURL: "https://api.github.com", ProviderConfig: []byte(`{"repo":"acme/app"}`),
		Scopes: []string{"issues"}, CredentialKeyID: "credential-key",
		CredentialCiphertext: []byte("credential"), WebhookSecretKeyID: "webhook-key",
		WebhookSecretCiphertext: []byte("webhook"), CreatedBy: "user-1", UpdatedBy: "user-1",
	}
}

func fakeExternalConnectionRow(tenantID string, id uuid.UUID, now time.Time) fakeRow {
	providerInstallationID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000012")
	return fakeRow{values: []any{
		id, tenantID, "github", "GitHub", true, ConnectionStatusActive,
		"token", "https://api.github.com", []byte(`{"repo":"acme/attune"}`),
		[]string{"issues"},
		"credential-key", []byte("credential"),
		stringPtr("webhook-key"), []byte("webhook"), timePtr(now), timePtr(now),
		TestStatusOK, "", "user-1", "user-1", ptrext.Of(providerInstallationID), now, now,
	}}
}

func fakeProviderInstallationRow(tenantID string, id uuid.UUID, now time.Time) fakeRow {
	return fakeRow{values: []any{
		id, tenantID, "github", "Acme GitHub App", InstallationKindGitHubApp,
		InstallationStatusActive, "12345", "acme", "42", "https://github.com/acme",
		"https://api.github.com", []byte(`{"issues":"write"}`), []byte(`{"full_app":true}`),
		ResourceSelectionSelected, "qualified", timePtr(now), "", "user-1", "user-1", now, now,
	}}
}

func fakeProviderInstallationResourceRow(tenantID string, installationID, id uuid.UUID, now time.Time) fakeRow {
	return fakeRow{values: []any{
		id, tenantID, installationID, "github", ResourceTypeRepository, "98765",
		"acme/attune", "acme/attune", "https://github.com/acme/attune",
		true, ResourceStatusActive, []byte(`{"issues":"write"}`), timePtr(now), now, now,
	}}
}

func fakeEventRowWithRun(id uuid.UUID, tenantID string, connectionID uuid.UUID, mappingID, runID uuid.UUID, now time.Time) fakeRow {
	return fakeRow{values: []any{
		id, tenantID, connectionID, ptrext.Of(mappingID), "github", "issue_comment", "delivery-1",
		"dedupe-1", EventSignatureVerified, EventStatusReplayed, payloadDigest([]byte("payload")),
		[]byte(`{"action":"created"}`), now, timePtr(now), "user-1", ptrext.Of(runID), "", now, now,
	}}
}

func fakeTimelineRow(runID uuid.UUID, now time.Time) fakeRow {
	return fakeRow{values: []any{
		"run", now, ptrext.Of(runID), RunStatusQueued, DirectionPull, "", "",
		"manual pull run queued", []byte(`{"attempts":1}`),
	}}
}

func fakeAttemptRow(runID uuid.UUID, now time.Time) fakeRow {
	return fakeRow{values: []any{
		int64(1), runID, 1, now, timePtr(now), RunStatusSucceeded, 200,
		"provider-request-1", nil, "", "",
	}}
}

func fakeFailureRow(tenantID string, runID, mappingID uuid.UUID, now time.Time) fakeRow {
	failureID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000030")
	return fakeRow{values: []any{
		failureID, tenantID, runID, mappingID, DirectionPull, "request-1", "ISS-235",
		"validation", "missing title", payloadDigest([]byte("payload")), "manual",
		[]byte(`{"title":""}`), false, nil, "", now,
	}}
}

func fakeConflictRow(tenantID string, mappingID uuid.UUID, now time.Time) fakeRow {
	conflictID := uuid.MustParse("bbbbbbbb-2000-4000-8000-000000000031")
	return fakeRow{values: []any{
		conflictID, tenantID, mappingID, "request-1", "ISS-235", "field",
		"resolved", []byte(`{"status":"open"}`), []byte(`{"status":"closed"}`),
		"provider", timePtr(now), "user-1", now, now,
	}}
}

func fakePushCandidateRow(requestID uuid.UUID, now time.Time) fakeRow {
	return fakeRow{values: []any{
		requestID.String(), "CR-235", "Sync issue", "Customer cannot qualify provider",
		"planned", "high", now, "ISS-235", "v1",
	}}
}

func fakeHealthRow(now time.Time) fakeRow {
	return fakeRow{values: []any{
		2, 1, 0, 1, 1, 0, 1, timePtr(now), 1, 1, 0, 0, 1, timePtr(now), 1, 0,
	}}
}

func fakeMetricPointRow() fakeRow {
	return fakeRow{values: []any{"github", "issue", 1, 42.5}}
}

func expectAffectedRows(t *testing.T, name string, call func() (int64, error)) {
	t.Helper()
	affected, err := call()
	if err != nil || affected != 1 {
		t.Fatalf("%s() = %d, %v; want one affected row", name, affected, err)
	}
}

func requireExternalSyncCondition(t *testing.T, name string, err error, ok bool, got any) {
	t.Helper()
	if err != nil || !ok {
		t.Fatalf("%s() = %+v, %v", name, got, err)
	}
}

func requireExternalSyncNoError(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s() error = %v", name, err)
	}
}

func expectExternalSyncRepoError(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
