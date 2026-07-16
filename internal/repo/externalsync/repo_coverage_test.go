// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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

func expectExternalSyncRepoError(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
