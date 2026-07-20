// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRunListLimitBounds(t *testing.T) {
	t.Parallel()

	if boundedRunListLimit(0) != defaultLimit || boundedRunListLimit(201) != defaultLimit {
		t.Fatalf("boundedRunListLimit should default outside the accepted range")
	}
	if boundedRunListLimit(1) != 1 || boundedRunListLimit(200) != 200 {
		t.Fatalf("boundedRunListLimit should keep accepted limits")
	}
}

func TestRunFilterHelpers(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	query, args := appendRunUUIDFilter("SELECT 1 WHERE tenant_id = $1", []any{"tenant"}, "connection_id", ptrext.Of(runID))
	if !strings.Contains(query, "connection_id = $2") || !reflect.DeepEqual(args, []any{"tenant", runID}) {
		t.Fatalf("appendRunUUIDFilter query=%q args=%v", query, args)
	}
	query, args = appendRunStatusFilter(query, args, " failed ")
	if !strings.Contains(query, "status = $3") || args[2] != "failed" {
		t.Fatalf("appendRunStatusFilter query=%q args=%v", query, args)
	}
	query, args = appendRunStatusFilter(query, args, "   ")
	if strings.Contains(query, "$4") || len(args) != 3 {
		t.Fatalf("blank status should not add a filter: query=%q args=%v", query, args)
	}
}

func TestRepoListRunAndEventQueries(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repo := ptrext.Of(Repo{})
	query, args, err := repo.listRunsQuery(context.Background(), ListRunsFilter{
		TenantID:     "tenant",
		ConnectionID: ptrext.Of(runID),
		Status:       "queued",
	}, 25)
	if err != nil {
		t.Fatalf("listRunsQuery returned error: %v", err)
	}
	if !strings.Contains(query, "connection_id = $2") ||
		!strings.Contains(query, "status = $3") ||
		!strings.Contains(query, "LIMIT $4") ||
		len(args) != 4 ||
		args[3] != 26 {
		t.Fatalf("listRunsQuery query=%q args=%v", query, args)
	}

	query, args, err = repo.listEventsQuery(context.Background(), ListEventsFilter{
		TenantID:     "tenant",
		ConnectionID: ptrext.Of(runID),
		Status:       "received",
	}, 10)
	if err != nil {
		t.Fatalf("listEventsQuery returned error: %v", err)
	}
	if !strings.Contains(query, "connection_id = $2") ||
		!strings.Contains(query, "status = $3") ||
		!strings.Contains(query, "LIMIT $4") ||
		args[3] != 11 {
		t.Fatalf("listEventsQuery query=%q args=%v", query, args)
	}
}

func TestRepoNormalizeHelpers(t *testing.T) {
	t.Parallel()

	if normalizeStreamKey("") != StreamDefault {
		t.Fatalf("empty stream key should normalize to default")
	}
	if normalizeStreamKey(" custom ") != "custom" {
		t.Fatalf("stream key should be trimmed")
	}
	if got := normalizeStreamKey(strings.Repeat("x", 250)); len(got) != 200 {
		t.Fatalf("stream key length = %d, want 200", len(got))
	}
	if normalizeLocalObjectID(" local ", "EXT-1") != "local" {
		t.Fatalf("local object id should win when present")
	}
	if normalizeLocalObjectID("", " EXT-1 ") != "external:EXT-1" {
		t.Fatalf("external key fallback not normalized")
	}

	if string(normalizeJSONObjectBytes(nil)) != "{}" {
		t.Fatalf("nil JSON should normalize to object")
	}
	if string(normalizeJSONObjectBytes([]byte("not-json"))) != "{}" {
		t.Fatalf("invalid JSON should normalize to object")
	}
	if got := string(normalizeJSONObjectBytes([]byte(` {"b":2,"a":1} `))); got != `{"a":1,"b":2}` {
		t.Fatalf("normalized JSON = %q", got)
	}
	if got := string(normalizeCursorAfter([]byte(`{"page":1}`), nil)); got != `{"page":1}` {
		t.Fatalf("empty cursor after should reuse cursor before, got %q", got)
	}

	hint := pushRunHintFromMetadata([]byte(`{"local_object_id":" cr-1 ","external_key":" ISS-1 ","source":" customer_request_issue_create "}`))
	if hint.LocalObjectID != "cr-1" || hint.ExternalKey != "ISS-1" || hint.Source != "customer_request_issue_create" {
		t.Fatalf("push run hint = %+v; want trimmed selector", hint)
	}
	if bad := pushRunHintFromMetadata([]byte(`not-json`)); bad != (pushRunHint{}) {
		t.Fatalf("bad push run hint = %+v; want empty selector", bad)
	}
}

func TestCustomerRequestIssueRunMetadataHelpers(t *testing.T) {
	t.Parallel()

	requestID := uuid.MustParse("99999999-9999-9999-9999-999999999999")

	createMetadata := customerRequestIssueCreateRunMetadata(requestID)
	if got := pushRunHintFromMetadata(createMetadata); got.LocalObjectID != requestID.String() ||
		got.ExternalKey != "" ||
		got.Source != "customer_request_issue_create" {
		t.Fatalf("create metadata hint = %+v", got)
	}

	pullMetadata := customerRequestIssuePullRunMetadata(requestID, "  ISSUE-228  ")
	if got := pushRunHintFromMetadata(pullMetadata); got.LocalObjectID != requestID.String() ||
		got.ExternalKey != "ISSUE-228" ||
		got.Source != "customer_request_issue_link" {
		t.Fatalf("pull metadata hint = %+v", got)
	}
}

func TestCustomerRequestIssueRunLinkChecks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tenantID := "tenant-1"
	requestID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	errBoom := errors.New("boom")

	t.Run("require external link", func(t *testing.T) {
		t.Parallel()

		err := requireCustomerRequestIssueExternalLink(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{values: []any{true}}},
		}), tenantID, requestID, mappingID, " ISSUE-228 ")
		if err != nil {
			t.Fatalf("linked request returned error: %v", err)
		}
		if err := requireCustomerRequestIssueExternalLink(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{values: []any{false}}},
		}), tenantID, requestID, mappingID, "ISSUE-404"); !errors.Is(err, ErrLocalObjectNotFound) {
			t.Fatalf("missing link error = %v; want ErrLocalObjectNotFound", err)
		}
		if err := requireCustomerRequestIssueExternalLink(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{err: errBoom}},
		}), tenantID, requestID, mappingID, "ISSUE-500"); err == nil {
			t.Fatal("link check query error returned nil")
		}
	})

	t.Run("reject existing links", func(t *testing.T) {
		t.Parallel()

		err := rejectExistingCustomerRequestIssueLink(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{values: []any{false, false}}},
		}), tenantID, requestID, mappingID)
		if err != nil {
			t.Fatalf("unlinked request returned error: %v", err)
		}
		if err := rejectExistingCustomerRequestIssueLink(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{values: []any{true, false}}},
		}), tenantID, requestID, mappingID); !errors.Is(err, ErrConflict) {
			t.Fatalf("issue link error = %v; want ErrConflict", err)
		}
		if err := rejectExistingCustomerRequestIssueLink(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{values: []any{false, true}}},
		}), tenantID, requestID, mappingID); !errors.Is(err, ErrConflict) {
			t.Fatalf("object link error = %v; want ErrConflict", err)
		}
		if err := rejectExistingCustomerRequestIssueLink(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{err: errBoom}},
		}), tenantID, requestID, mappingID); err == nil {
			t.Fatal("existing link query error returned nil")
		}
	})

	t.Run("reject concurrent create runs", func(t *testing.T) {
		t.Parallel()

		err := rejectConcurrentCustomerRequestIssueCreateRun(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{values: []any{false}}},
		}), tenantID, requestID, mappingID)
		if err != nil {
			t.Fatalf("request without concurrent run returned error: %v", err)
		}
		if err := rejectConcurrentCustomerRequestIssueCreateRun(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{values: []any{true}}},
		}), tenantID, requestID, mappingID); !errors.Is(err, ErrConflict) {
			t.Fatalf("concurrent run error = %v; want ErrConflict", err)
		}
		if err := rejectConcurrentCustomerRequestIssueCreateRun(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{err: errBoom}},
		}), tenantID, requestID, mappingID); err == nil {
			t.Fatal("concurrent run query error returned nil")
		}
	})
}

func TestCustomerRequestIssueRunExistingRunHelpers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 7, 8, 2, 3, 4, 0, time.UTC)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	requestID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	runID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	row := issueRunFakeRow(runID, tenantID, connectionID, mappingID, now, DirectionPush)
	run, err := existingCustomerRequestIssueCreateRun(ctx, ptrext.Of(fakeTx{rows: []fakeRow{row}}), tenantID, requestID, mappingID)
	if err != nil {
		t.Fatalf("existing create run returned error: %v", err)
	}
	if run.ID != runID || run.Direction != DirectionPush || run.Trigger != TriggerManual {
		t.Fatalf("existing create run = %+v", run)
	}

	row = issueRunFakeRow(runID, tenantID, connectionID, mappingID, now, DirectionPull)
	run, err = existingCustomerRequestIssuePullRun(ctx, ptrext.Of(fakeTx{rows: []fakeRow{row}}), tenantID, requestID, mappingID, " ISSUE-228 ")
	if err != nil {
		t.Fatalf("existing pull run returned error: %v", err)
	}
	if run.ID != runID || run.Direction != DirectionPull || run.Trigger != TriggerManual {
		t.Fatalf("existing pull run = %+v", run)
	}

	errBoom := errors.New("boom")
	if _, err := existingCustomerRequestIssueCreateRun(ctx, ptrext.Of(fakeTx{
		rows: []fakeRow{{err: errBoom}},
	}), tenantID, requestID, mappingID); !errors.Is(err, errBoom) {
		t.Fatalf("existing create run error = %v; want boom", err)
	}
	if _, err := existingCustomerRequestIssuePullRun(ctx, ptrext.Of(fakeTx{
		rows: []fakeRow{{err: errBoom}},
	}), tenantID, requestID, mappingID, "ISSUE-228"); !errors.Is(err, errBoom) {
		t.Fatalf("existing pull run error = %v; want boom", err)
	}
}

func TestRunInputMetadataFromEventIncludesWebhookHints(t *testing.T) {
	t.Parallel()

	eventID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	raw := runInputMetadataFromEvent(SyncEvent{
		ID:              eventID,
		ExternalEventID: "delivery-1",
		NormalizedPayload: []byte(`{
			"event_type": "issue_comment",
			"action": "deleted",
			"repository": {
				"full_name": "acme/app",
				"html_url": "https://github.com/acme/app"
			},
			"issue": {
				"number": 42,
				"html_url": "https://github.com/acme/app/issues/42"
			},
			"comment": {
				"id": 777
			}
		}`),
	})
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("unmarshal run input metadata: %v", err)
	}
	wantStrings := map[string]string{
		"external_sync_event_id": eventID.String(),
		"provider_event_id":      "delivery-1",
		"event_type":             "issue_comment",
		"action":                 "deleted",
		"repository_full_name":   "acme/app",
		"repository_url":         "https://github.com/acme/app",
		"issue_url":              "https://github.com/acme/app/issues/42",
	}
	for key, want := range wantStrings {
		if got[key] != want {
			t.Fatalf("metadata[%s] = %#v; want %q in %s", key, got[key], want, raw)
		}
	}
	if got["issue_number"] != float64(42) || got["comment_id"] != float64(777) {
		t.Fatalf("metadata numeric hints = issue:%#v comment:%#v; want 42/777 in %s",
			got["issue_number"], got["comment_id"], raw)
	}
}

func TestRepoPayloadDigestAndStringHelpers(t *testing.T) {
	t.Parallel()

	digest := payloadDigest([]byte("payload"))
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		t.Fatalf("payloadDigest = %q", digest)
	}
	if payloadString([]byte(`{"title":"  Issue title  "}`), "summary", "title") != "Issue title" {
		t.Fatalf("payloadString did not trim selected string")
	}
	if payloadString([]byte("bad"), "title") != "" {
		t.Fatalf("payloadString should ignore invalid JSON")
	}
	if payloadString([]byte(`{"name":42}`), "title") != "" {
		t.Fatalf("payloadString should ignore missing string keys")
	}
}

func TestRepoConflictAndValueHelpers(t *testing.T) {
	t.Parallel()

	if issueProvider("github") != "github" || issueProvider("unknown") != "other" {
		t.Fatalf("issueProvider should preserve known providers and collapse unknowns")
	}
	if conflictMessage("link_mismatch") == conflictMessage("version_mismatch") {
		t.Fatalf("conflict messages should distinguish link mismatches")
	}
	if boolToInt(true) != 1 || boolToInt(false) != 0 {
		t.Fatalf("boolToInt returned unexpected values")
	}
	if parseExternalVersionTime("not-time") != nil {
		t.Fatalf("invalid external version should not parse")
	}
	if ts := parseExternalVersionTime("2026-07-08T01:02:03Z"); ts == nil || ts.UTC().Year() != 2026 {
		t.Fatalf("valid external version timestamp did not parse: %v", ts)
	}
	if truncate("  abcdef  ", 3) != "abc" {
		t.Fatalf("truncate should trim before cutting")
	}
	if string(marshalJSONObject(map[string]any{"bad": func() {}})) != "{}" {
		t.Fatalf("marshalJSONObject should fall back to empty object on unsupported values")
	}
}

func TestCustomerRequestPushPayloadHelpers(t *testing.T) {
	t.Parallel()

	if customerRequestIssueTitle("CR-7", " Login fails ") != "CR-7 Login fails" {
		t.Fatalf("title should include display id and trimmed title")
	}
	if customerRequestIssueTitle("CR-7", "") != "CR-7" {
		t.Fatalf("title without request title should use display id")
	}
	if customerRequestIssueTitle("", "Only title") != "Only title" {
		t.Fatalf("title without display id should use the title")
	}
	body := customerRequestIssueBody("CR-7", "", "planned", "high")
	if !strings.Contains(body, "No description provided.") ||
		!strings.Contains(body, "| Status | `planned` |") ||
		!strings.Contains(body, "| Priority | `high` |") {
		t.Fatalf("customer request body missing fallback fields: %q", body)
	}

	payload, err := customerRequestIssuePayload("cr-1", "CR-1", "Sync bug", "Body", "open", "high", "ISS-1")
	if err != nil {
		t.Fatalf("customerRequestIssuePayload returned error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("payload JSON invalid: %v", err)
	}
	if decoded["external_key"] != "ISS-1" || decoded["title"] != "CR-1 Sync bug" {
		t.Fatalf("payload = %+v", decoded)
	}

	records := pushResultsByLocal([]PushResult{
		{LocalObjectID: " cr-1 ", ExternalKey: "ISS-1"},
		{LocalObjectID: "   ", ExternalKey: "ISS-blank"},
	})
	if len(records) != 1 || records["cr-1"].ExternalKey != "ISS-1" {
		t.Fatalf("pushResultsByLocal = %+v", records)
	}
}

func TestRepoConnectionMappingRunEventScanners(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 8, 2, 3, 4, 0, time.UTC)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	runID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	conn, err := scanConnection(fakeRow{values: []any{
		connectionID, tenantID, "github", "GitHub", true, ConnectionStatusActive,
		"token", "https://api.github.com", []byte(`{"repo":"acme/app"}`),
		[]string{"issues"},
		"credential-key", []byte("credential"), stringPtr("webhook-key"), []byte("webhook"),
		timePtr(now), timePtr(now), TestStatusOK, "", "admin", "admin", now, now,
	}})
	if err != nil {
		t.Fatalf("scanConnection returned error: %v", err)
	}
	if conn.WebhookSecretKeyID != "webhook-key" || conn.LastTestedAt == nil {
		t.Fatalf("connection nullable fields = %+v", conn)
	}

	mapping, err := scanMapping(fakeRow{values: []any{
		mappingID, tenantID, connectionID, "customer_request", "issue", DirectionPull,
		[]byte(`{"title":"title"}`), []byte(`{}`), "manual", "mark_stale", true, 3, now, now,
	}})
	if err != nil {
		t.Fatalf("scanMapping returned error: %v", err)
	}
	if mapping.MappingVersion != 3 || string(mapping.FieldMapping) == "" {
		t.Fatalf("mapping = %+v", mapping)
	}

	run, err := scanRun(fakeRow{values: []any{
		runID, tenantID, connectionID, ptrext.Of(mappingID), DirectionPull, TriggerManual, RunStatusRunning,
		timePtr(now), stringPtr("worker-1"), 2, now.Add(time.Minute), timePtr(now),
		timePtr(now.Add(time.Minute)), []byte(`{"before":1}`), []byte(`{"after":1}`),
		[]byte(`{"issue_number":228}`), 5, 4, 1, 1, "rate_limited", "slow down", "admin", now, now,
	}})
	if err != nil {
		t.Fatalf("scanRun returned error: %v", err)
	}
	if run.MappingID == nil || run.ClaimedBy != "worker-1" || run.RecordsSeen != 5 || string(run.InputMetadata) == "" {
		t.Fatalf("run = %+v", run)
	}

	event, err := scanEvent(fakeRow{values: []any{
		uuid.MustParse("44444444-4444-4444-4444-444444444444"), tenantID, connectionID, ptrext.Of(mappingID),
		"github", "issues", "delivery-1", "github:issues:delivery-1",
		EventSignatureVerified, EventStatusReceived, "sha256:abc", []byte(`{"action":"opened"}`),
		now, timePtr(now.Add(time.Minute)), "admin", ptrext.Of(runID), "", now, now,
	}})
	if err != nil {
		t.Fatalf("scanEvent returned error: %v", err)
	}
	if event.RunID == nil || event.ReplayedBy != "admin" {
		t.Fatalf("event = %+v", event)
	}
}

func TestRepoFailureRetryScanners(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 8, 2, 3, 4, 0, time.UTC)
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	runID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	attempt, err := scanAttempt(fakeRow{values: []any{
		int64(9), runID, 2, now, timePtr(now.Add(time.Second)), "retryable_error",
		429, "gh-req", timePtr(now.Add(time.Minute)), "rate_limited", "secondary limit",
	}})
	if err != nil || attempt.AttemptNumber != 2 || attempt.RetryAfter == nil {
		t.Fatalf("attempt=%+v err=%v", attempt, err)
	}

	failureID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	failure, err := scanFailure(fakeRow{values: []any{
		failureID, tenantID, runID, mappingID, "pull", "cr-1", "ISS-1", "validation",
		"missing field", "sha256:def", "refetch", []byte(`{"title":"bug"}`), true,
		timePtr(now), "admin", now,
	}})
	if err != nil || !failure.Retryable || failure.ResolvedAt == nil {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}

	seed, err := scanFailureRetrySeed(fakeRow{values: []any{runID, mappingID, connectionID, DirectionPush}})
	if err != nil || seed.direction != DirectionPush {
		t.Fatalf("seed=%+v err=%v", seed, err)
	}
}

func TestRepoConflictTimelineAndLinkScanners(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 8, 2, 3, 4, 0, time.UTC)
	tenantID := "tenant-1"
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	runID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	conflict, err := scanConflict(fakeRow{values: []any{
		uuid.MustParse("66666666-6666-6666-6666-666666666666"), tenantID, mappingID,
		"cr-1", "ISS-1", "version_mismatch", "resolved", []byte(`{"local":1}`),
		[]byte(`{"external":1}`), "external_wins", timePtr(now), "admin", now, now,
	}})
	if err != nil || conflict.ResolvedAt == nil || conflict.Status != "resolved" {
		t.Fatalf("conflict=%+v err=%v", conflict, err)
	}

	timeline, err := scanTimelineEntry(fakeRow{values: []any{
		"failure", now, ptrext.Of(runID), "open", "pull", "cr-1", "ISS-1", "validation: bad", []byte(`{"retryable":true}`),
	}})
	if err != nil || timeline.RunID == nil || timeline.Kind != "failure" {
		t.Fatalf("timeline=%+v err=%v", timeline, err)
	}

	link, err := scanObjectLink(fakeRow{values: []any{
		uuid.MustParse("77777777-7777-7777-7777-777777777777"), "cr-1", "ISS-1",
		"https://github.com/acme/app/issues/1", "v1", SyncStatePending, false,
	}})
	if err != nil || link.SyncState != SyncStatePending {
		t.Fatalf("link=%+v err=%v", link, err)
	}

	scanErr := errors.New("scan failed")
	if _, err := scanRun(fakeRow{err: scanErr}); !errors.Is(err, scanErr) {
		t.Fatalf("scanRun error=%v, want scanErr", err)
	}
}

func TestScanPushCandidateBuildsPayload(t *testing.T) {
	t.Parallel()

	if _, err := scanPushCandidate(fakeRow{err: errors.New("scan failed")}); err == nil {
		t.Fatal("scanPushCandidate scan error returned nil")
	}
	updatedAt := time.Date(2026, 7, 8, 4, 0, 0, 0, time.UTC)
	record, err := scanPushCandidate(fakeRow{values: []any{
		"cr-1", "CR-1", "Login fails", "Users cannot login", "planned", "high",
		updatedAt, " ISS-9 ", " 2026-07-08T04:00:00Z ",
	}})
	if err != nil {
		t.Fatalf("scanPushCandidate returned error: %v", err)
	}
	if record.ExternalKey != "ISS-9" || record.LocalVersion != updatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("record = %+v", record)
	}
	if payloadString(record.Payload, "title") != "CR-1 Login fails" {
		t.Fatalf("payload title = %q", payloadString(record.Payload, "title"))
	}
}

func TestApplyPullRecordBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 7, 8, 4, 0, 0, 0, time.UTC)
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	input := ApplyPullInput{
		TenantID:  "tenant-1",
		RunID:     uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		MappingID: mappingID,
		Provider:  "github",
	}
	mapping := Mapping{
		ID:                 mappingID,
		LocalObjectType:    "ticket",
		ExternalObjectType: "issue",
	}

	t.Run("missing external key records a validation failure", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1")}})
		outcome, err := applyPullRecord(ctx, tx, input, mapping, PullRecord{Payload: []byte(`{"title":"bug"}`)})
		if err != nil {
			t.Fatalf("applyPullRecord returned error: %v", err)
		}
		if outcome.failed != 1 || tx.execIdx != 1 {
			t.Fatalf("outcome=%+v execs=%d", outcome, tx.execIdx)
		}
	})

	t.Run("invalid local customer request id records a validation failure", func(t *testing.T) {
		t.Parallel()

		customerMapping := mapping
		customerMapping.LocalObjectType = "customer_request"
		tx := ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1")}})
		outcome, err := applyPullRecord(ctx, tx, input, customerMapping, PullRecord{
			LocalObjectID: "not-a-uuid",
			ExternalKey:   "ISS-1",
			Payload:       []byte(`{"title":"bug"}`),
		})
		if err != nil {
			t.Fatalf("applyPullRecord returned error: %v", err)
		}
		if outcome.failed != 1 || tx.execIdx != 1 {
			t.Fatalf("outcome=%+v execs=%d", outcome, tx.execIdx)
		}
	})

	t.Run("deleted record without an existing link is a no-op", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{rows: []fakeRow{{err: pgx.ErrNoRows}}})
		outcome, err := applyPullRecord(ctx, tx, input, mapping, PullRecord{
			ExternalKey: "ISS-1",
			Deleted:     true,
			Payload:     []byte(`{"title":"bug"}`),
		})
		if err != nil {
			t.Fatalf("applyPullRecord returned error: %v", err)
		}
		if outcome != (pullApplyOutcome{}) {
			t.Fatalf("outcome=%+v, want zero", outcome)
		}
	})

	t.Run("pending link with a mismatched version creates a conflict", func(t *testing.T) {
		t.Parallel()

		linkID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
		tx := ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{linkID, "cr-1", "ISS-1", "https://example.test/1", "v1", SyncStatePending, false}},
			{values: []any{1}},
		}})
		outcome, err := applyPullRecord(ctx, tx, input, mapping, PullRecord{
			ExternalKey:     "ISS-1",
			ExternalURL:     "https://example.test/1",
			ExternalVersion: "v2",
			Payload:         []byte(`{"title":"bug"}`),
		})
		if err != nil {
			t.Fatalf("applyPullRecord returned error: %v", err)
		}
		if outcome.conflicts != 1 {
			t.Fatalf("outcome=%+v, want one conflict", outcome)
		}
	})

	t.Run("unchanged existing link does not count as changed", func(t *testing.T) {
		t.Parallel()

		linkID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
		tx := ptrext.Of(fakeTx{
			rows:  []fakeRow{{values: []any{linkID, "cr-1", "ISS-1", "https://example.test/1", "v1", SyncStateSynced, false}}},
			execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
		})
		outcome, err := applyPullRecord(ctx, tx, input, mapping, PullRecord{
			ExternalKey:       "ISS-1",
			ExternalURL:       "https://example.test/1",
			ExternalVersion:   "v1",
			ExternalUpdatedAt: ptrext.Of(now),
			Payload:           []byte(`{"title":"bug"}`),
		})
		if err != nil {
			t.Fatalf("applyPullRecord returned error: %v", err)
		}
		if outcome.changed != 0 || tx.execIdx != 1 {
			t.Fatalf("outcome=%+v execs=%d", outcome, tx.execIdx)
		}
	})

	t.Run("local tombstone skips existing external link", func(t *testing.T) {
		t.Parallel()

		linkID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
		tx := ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{linkID, "cr-1", "ISS-1", "https://example.test/1", "v1", SyncStateDeleted, true}},
		}})
		outcome, err := applyPullRecord(ctx, tx, input, mapping, PullRecord{
			ExternalKey:       "ISS-1",
			ExternalURL:       "https://example.test/1",
			ExternalVersion:   "v2",
			ExternalUpdatedAt: ptrext.Of(now),
			Payload:           []byte(`{"title":"bug"}`),
		})
		if err != nil {
			t.Fatalf("applyPullRecord returned error: %v", err)
		}
		if outcome != (pullApplyOutcome{}) || tx.execIdx != 0 {
			t.Fatalf("outcome=%+v execs=%d, want skipped tombstone", outcome, tx.execIdx)
		}
	})

	t.Run("new external link is inserted", func(t *testing.T) {
		t.Parallel()

		linkID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
		tx := ptrext.Of(fakeTx{rows: []fakeRow{
			{err: pgx.ErrNoRows},
			{err: pgx.ErrNoRows},
			{values: []any{linkID}},
		}})
		outcome, err := applyPullRecord(ctx, tx, input, mapping, PullRecord{
			ExternalKey:       "ISS-2",
			ExternalURL:       "https://example.test/2",
			ExternalVersion:   "v2",
			ExternalUpdatedAt: ptrext.Of(now),
			Payload:           []byte(`{"title":"bug"}`),
		})
		if err != nil {
			t.Fatalf("applyPullRecord returned error: %v", err)
		}
		if outcome.changed != 1 || tx.rowIdx != 3 {
			t.Fatalf("outcome=%+v rows=%d", outcome, tx.rowIdx)
		}
	})
}

func TestApplyPushRecordBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 7, 8, 4, 0, 0, 0, time.UTC)
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	requestID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	input := ApplyPushInput{
		TenantID: "tenant-1",
		RunID:    uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Provider: "github",
	}
	mapping := Mapping{
		ID:                 mappingID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
	}
	record := PushRecord{
		LocalObjectID:  requestID.String(),
		LocalVersion:   now.Format(time.RFC3339Nano),
		LocalUpdatedAt: now,
		Payload:        []byte(`{"title":"CR-1 Login fails","status":"planned"}`),
	}

	t.Run("provider error records a retryable failure", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1")}})
		outcome, err := applyPushRecord(ctx, tx, input, mapping, record, PushResult{
			LocalObjectID: record.LocalObjectID,
			ErrorKind:     "provider_unavailable",
			ErrorMessage:  "temporary outage",
			Retryable:     true,
		})
		if err != nil {
			t.Fatalf("applyPushRecord returned error: %v", err)
		}
		if outcome.failed != 1 || tx.execIdx != 1 {
			t.Fatalf("outcome=%+v execs=%d", outcome, tx.execIdx)
		}
	})

	t.Run("missing external key records a validation failure", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1")}})
		outcome, err := applyPushRecord(ctx, tx, input, mapping, record, PushResult{
			LocalObjectID: record.LocalObjectID,
		})
		if err != nil {
			t.Fatalf("applyPushRecord returned error: %v", err)
		}
		if outcome.failed != 1 || tx.execIdx != 1 {
			t.Fatalf("outcome=%+v execs=%d", outcome, tx.execIdx)
		}
	})

	t.Run("invalid local id records a validation failure", func(t *testing.T) {
		t.Parallel()

		badRecord := record
		badRecord.LocalObjectID = "not-a-uuid"
		tx := ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1")}})
		outcome, err := applyPushRecord(ctx, tx, input, mapping, badRecord, PushResult{
			LocalObjectID: badRecord.LocalObjectID,
			ExternalKey:   "ISS-1",
		})
		if err != nil {
			t.Fatalf("applyPushRecord returned error: %v", err)
		}
		if outcome.failed != 1 || tx.execIdx != 1 {
			t.Fatalf("outcome=%+v execs=%d", outcome, tx.execIdx)
		}
	})

	t.Run("external link owned by another object creates a conflict", func(t *testing.T) {
		t.Parallel()

		linkID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
		tx := ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{true}},
			{values: []any{linkID, "other-local", "ISS-1", "https://example.test/1", "v1", SyncStateSynced, false}},
			{values: []any{1}},
		}})
		outcome, err := applyPushRecord(ctx, tx, input, mapping, record, PushResult{
			LocalObjectID:   record.LocalObjectID,
			ExternalKey:     "ISS-1",
			ExternalURL:     "https://example.test/1",
			ExternalVersion: "v1",
		})
		if err != nil {
			t.Fatalf("applyPushRecord returned error: %v", err)
		}
		if outcome.conflicts != 1 {
			t.Fatalf("outcome=%+v, want one conflict", outcome)
		}
	})

	t.Run("successful push inserts a link and issue reference", func(t *testing.T) {
		t.Parallel()

		linkID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
		tx := ptrext.Of(fakeTx{
			rows: []fakeRow{
				{values: []any{true}},
				{err: pgx.ErrNoRows},
				{err: pgx.ErrNoRows},
				{values: []any{linkID}},
			},
			execs: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1")},
		})
		outcome, err := applyPushRecord(ctx, tx, input, mapping, record, PushResult{
			LocalObjectID:   record.LocalObjectID,
			ExternalKey:     "ISS-2",
			ExternalURL:     "https://example.test/2",
			ExternalVersion: now.Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatalf("applyPushRecord returned error: %v", err)
		}
		if outcome.changed != 1 || tx.rowIdx != 4 || tx.execIdx != 1 {
			t.Fatalf("outcome=%+v rows=%d execs=%d", outcome, tx.rowIdx, tx.execIdx)
		}
	})
}

func TestRepoLinkValidationHelpers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	requestID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	input := ApplyPullInput{
		TenantID:  "tenant-1",
		RunID:     uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		MappingID: mappingID,
		Provider:  "github",
	}
	mapping := Mapping{
		ID:                 mappingID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
	}
	record := PullRecord{
		LocalObjectID: requestID.String(),
		ExternalKey:   "ISS-1",
		ExternalURL:   "https://example.test/1",
		Payload:       []byte(`{"title":"Bug"}`),
	}
	payload := normalizePayloadObject(record.Payload)

	t.Run("valid local object reference passes", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{rows: []fakeRow{{values: []any{true}}}})
		failed, err := validateLocalObjectReference(ctx, tx, input, mapping, record, payload)
		if failed || err != nil {
			t.Fatalf("failed=%t err=%v, want success", failed, err)
		}
	})

	t.Run("missing local object records a failure", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{
			rows:  []fakeRow{{values: []any{false}}},
			execs: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1")},
		})
		failed, err := validateLocalObjectReference(ctx, tx, input, mapping, record, payload)
		if !failed || err != nil || tx.execIdx != 1 {
			t.Fatalf("failed=%t err=%v execs=%d, want recorded failure", failed, err, tx.execIdx)
		}
	})

	t.Run("non customer request mappings skip local validation", func(t *testing.T) {
		t.Parallel()

		otherMapping := mapping
		otherMapping.LocalObjectType = "ticket"
		failed, err := validateLocalObjectReference(ctx, ptrext.Of(fakeTx{}), input, otherMapping, record, payload)
		if failed || err != nil {
			t.Fatalf("failed=%t err=%v, want skipped validation", failed, err)
		}
	})

	t.Run("find links maps no rows and success", func(t *testing.T) {
		t.Parallel()

		linkID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
		tx := ptrext.Of(fakeTx{rows: []fakeRow{
			{err: pgx.ErrNoRows},
			{values: []any{linkID, requestID.String(), "ISS-1", "https://example.test/1", "v1", SyncStateSynced, false}},
		}})
		link, err := findLinkByExternal(ctx, tx, input.TenantID, mapping.ID, mapping.ExternalObjectType, "ISS-1")
		if link != nil || err != nil {
			t.Fatalf("external link=%+v err=%v, want nil no-row result", link, err)
		}
		link, err = findLinkByLocal(ctx, tx, input.TenantID, mapping.ID, mapping.LocalObjectType, requestID.String())
		if err != nil || link == nil || link.ID != linkID {
			t.Fatalf("local link=%+v err=%v, want loaded link", link, err)
		}
	})
}

func TestRepoLinkMutationHelpers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 7, 8, 5, 0, 0, 0, time.UTC)
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	requestID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	linkID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	pullInput := ApplyPullInput{
		TenantID:  "tenant-1",
		RunID:     uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		MappingID: mappingID,
		Provider:  "github",
		StreamKey: StreamDefault,
	}
	pushInput := ApplyPushInput{
		TenantID: "tenant-1",
		RunID:    pullInput.RunID,
		Provider: "github",
	}
	mapping := Mapping{
		ID:                 mappingID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
	}
	pullRecord := PullRecord{
		LocalObjectID:     requestID.String(),
		ExternalKey:       "ISS-1",
		ExternalURL:       "https://example.test/1",
		ExternalVersion:   now.Format(time.RFC3339Nano),
		ExternalUpdatedAt: ptrext.Of(now),
		Payload:           []byte(`{"title":"Bug","state":"closed","assignee":"octo"}`),
	}
	pushRecord := PushRecord{
		LocalObjectID:  requestID.String(),
		LocalVersion:   now.Format(time.RFC3339Nano),
		LocalUpdatedAt: now,
		Payload:        []byte(`{"title":"Bug","status":"open"}`),
	}
	pushResult := PushResult{
		LocalObjectID:   requestID.String(),
		ExternalKey:     "ISS-1",
		ExternalURL:     "https://example.test/1",
		ExternalVersion: now.Format(time.RFC3339Nano),
	}

	t.Run("tombstone marks object link and issue link stale", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{
			rows:  []fakeRow{{values: []any{linkID, requestID.String()}}},
			execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
		})
		outcome, err := tombstoneExternalLink(ctx, tx, pullInput, mapping, pullRecord, requestID.String())
		if err != nil || outcome.changed != 1 || tx.rowIdx != 1 || tx.execIdx != 1 {
			t.Fatalf("outcome=%+v err=%v rows=%d execs=%d", outcome, err, tx.rowIdx, tx.execIdx)
		}
	})

	t.Run("customer request issue link skips unsupported shapes", func(t *testing.T) {
		t.Parallel()

		otherMapping := mapping
		otherMapping.ExternalObjectType = "ticket"
		if err := upsertCustomerRequestIssueLink(ctx, ptrext.Of(fakeTx{}), pullInput, otherMapping, pullRecord, requestID.String(), linkID, pullRecord.Payload); err != nil {
			t.Fatalf("unsupported mapping returned error: %v", err)
		}
		noURL := pullRecord
		noURL.ExternalURL = ""
		if err := upsertCustomerRequestIssueLink(ctx, ptrext.Of(fakeTx{}), pullInput, mapping, noURL, requestID.String(), linkID, noURL.Payload); err != nil {
			t.Fatalf("empty URL returned error: %v", err)
		}
		if err := markCustomerRequestIssueLinkStale(ctx, ptrext.Of(fakeTx{}), pullInput, otherMapping, pullRecord, requestID.String(), linkID); err != nil {
			t.Fatalf("unsupported stale mapping returned error: %v", err)
		}
	})

	t.Run("customer request issue link upsert and cursor write execute", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{execs: []pgconn.CommandTag{
			pgxconnTag("INSERT 0 1"),
			pgxconnTag("UPDATE 1"),
		}})
		if err := upsertCustomerRequestIssueLink(ctx, tx, pullInput, mapping, pullRecord, requestID.String(), linkID, pullRecord.Payload); err != nil {
			t.Fatalf("upsert issue link returned error: %v", err)
		}
		if len(tx.execArgs) == 0 || len(tx.execArgs[0]) != 11 ||
			tx.execArgs[0][8] != true || tx.execArgs[0][9] != "closed" || tx.execArgs[0][10] != "octo" {
			t.Fatalf("issue link update args = %#v; want pull external fields", tx.execArgs)
		}
		if err := upsertCursor(ctx, tx, ApplyPullInput{
			TenantID:    pullInput.TenantID,
			RunID:       pullInput.RunID,
			MappingID:   mappingID,
			StreamKey:   StreamDefault,
			CursorAfter: []byte(`{"next":"2"}`),
		}, ptrext.Of(now)); err != nil {
			t.Fatalf("upsertCursor returned error: %v", err)
		}
		if tx.execIdx != 2 {
			t.Fatalf("exec count = %d, want 2", tx.execIdx)
		}
	})

	t.Run("update external link records issue link and change state", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{execs: []pgconn.CommandTag{
			pgxconnTag("UPDATE 1"),
			pgxconnTag("INSERT 0 1"),
		}})
		changed, err := updateExternalLink(ctx, tx, pullInput, mapping, objectLinkRow{
			ID:              linkID,
			LocalObjectID:   requestID.String(),
			ExternalKey:     "ISS-1",
			ExternalURL:     "https://example.test/old",
			ExternalVersion: "old",
			SyncState:       SyncStatePending,
		}, pullRecord, pullRecord.Payload)
		if err != nil || changed != 1 || tx.execIdx != 2 {
			t.Fatalf("changed=%d err=%v execs=%d", changed, err, tx.execIdx)
		}
	})

	t.Run("push external link detects unchanged local link", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgxconnTag("UPDATE 1")}})
		gotID, changed, err := upsertPushExternalLink(ctx, tx, pushInput, mapping, pushRecord, pushResult, ptrext.Of(objectLinkRow{
			ID:              linkID,
			LocalObjectID:   requestID.String(),
			ExternalKey:     "ISS-1",
			ExternalURL:     pushResult.ExternalURL,
			ExternalVersion: pushResult.ExternalVersion,
			SyncState:       SyncStateSynced,
		}))
		if err != nil || gotID != linkID || changed != 0 || tx.execIdx != 1 {
			t.Fatalf("id=%s changed=%d err=%v execs=%d", gotID, changed, err, tx.execIdx)
		}
	})

	t.Run("push issue link skips empty URL and upserts populated URL", func(t *testing.T) {
		t.Parallel()

		if err := upsertCustomerRequestIssueLinkFromPush(ctx, ptrext.Of(fakeTx{}), pushInput, pushRecord, PushResult{
			LocalObjectID: pushRecord.LocalObjectID,
			ExternalKey:   "ISS-1",
		}, requestID, linkID); err != nil {
			t.Fatalf("empty URL returned error: %v", err)
		}
		tx := ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgxconnTag("INSERT 0 1")}})
		if err := upsertCustomerRequestIssueLinkFromPush(ctx, tx, pushInput, pushRecord, pushResult, requestID, linkID); err != nil {
			t.Fatalf("upsert from push returned error: %v", err)
		}
		if len(tx.execArgs) == 0 || len(tx.execArgs[0]) != 11 || tx.execArgs[0][8] != false {
			t.Fatalf("push issue link update args = %#v; want external fields untouched", tx.execArgs)
		}
		if tx.execIdx != 1 {
			t.Fatalf("exec count = %d, want 1", tx.execIdx)
		}
	})
}

func TestRepoIssueExternalProjectionHelpers(t *testing.T) {
	t.Parallel()

	if got := issueExternalStatusCategory([]byte(`{"state":"open"}`)); got != "open" {
		t.Fatalf("open status category = %q", got)
	}
	if got := issueExternalStatusCategory([]byte(`{"state":"closed"}`)); got != "closed" {
		t.Fatalf("closed status category = %q", got)
	}
	if got := issueExternalStatusCategory([]byte(`{"state":"reopened"}`)); got != "unknown" {
		t.Fatalf("unknown status category = %q", got)
	}
	if got := issueExternalAssignee([]byte(`{"assignee":" octo "}`)); got != "octo" {
		t.Fatalf("single assignee = %q", got)
	}
	if got := issueExternalAssignee([]byte(`{"assignees":[" octo ","hubot",""]}`)); got != "octo, hubot" {
		t.Fatalf("multi assignee = %q", got)
	}
}

func TestRepoApplyHelperErrorBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	errBoom := errors.New("boom")
	now := time.Date(2026, 7, 8, 5, 0, 0, 0, time.UTC)
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	requestID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	runID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	linkID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	pullInput := ApplyPullInput{
		TenantID:  "tenant-1",
		RunID:     runID,
		MappingID: mappingID,
		Provider:  "github",
		StreamKey: StreamDefault,
	}
	pushInput := ApplyPushInput{
		TenantID: "tenant-1",
		RunID:    runID,
		Provider: "github",
	}
	mapping := Mapping{
		ID:                 mappingID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
	}
	pullRecord := PullRecord{
		LocalObjectID:     requestID.String(),
		ExternalKey:       "ISS-1",
		ExternalURL:       "https://example.test/1",
		ExternalVersion:   now.Format(time.RFC3339Nano),
		ExternalUpdatedAt: ptrext.Of(now),
		Payload:           []byte(`{"title":"Bug","state":"open"}`),
	}
	pushRecord := PushRecord{
		LocalObjectID:  requestID.String(),
		LocalVersion:   now.Format(time.RFC3339Nano),
		LocalUpdatedAt: now,
		Payload:        []byte(`{"title":"Bug","status":"open"}`),
	}
	pushResult := PushResult{
		LocalObjectID:   requestID.String(),
		ExternalKey:     "ISS-1",
		ExternalURL:     "https://example.test/1",
		ExternalVersion: now.Format(time.RFC3339Nano),
	}

	t.Run("push local object lookup error", func(t *testing.T) {
		t.Parallel()

		_, _, err := validatePushLocalObject(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: errBoom}}}), pushInput, mapping, pushRecord, pushResult)
		if err == nil {
			t.Fatal("validatePushLocalObject returned nil error")
		}
	})

	t.Run("push local validation failures are recorded", func(t *testing.T) {
		t.Parallel()

		missingLocal := pushRecord
		missingLocal.LocalObjectID = ""
		_, failed, err := validatePushLocalObject(ctx, ptrext.Of(fakeTx{execs: []pgconn.CommandTag{pgxconnTag("INSERT 0 1")}}), pushInput, mapping, missingLocal, pushResult)
		if !failed || err != nil {
			t.Fatalf("missing local failed=%t err=%v, want recorded failure", failed, err)
		}
		_, failed, err = validatePushLocalObject(ctx, ptrext.Of(fakeTx{
			rows:  []fakeRow{{values: []any{false}}},
			execs: []pgconn.CommandTag{pgxconnTag("INSERT 0 1")},
		}), pushInput, mapping, pushRecord, pushResult)
		if !failed || err != nil {
			t.Fatalf("missing request failed=%t err=%v, want recorded failure", failed, err)
		}
	})

	t.Run("push external link lookup error", func(t *testing.T) {
		t.Parallel()

		_, err := applyPushRecord(ctx, ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{true}},
			{err: errBoom},
		}}), pushInput, mapping, pushRecord, pushResult)
		if err == nil {
			t.Fatal("applyPushRecord external lookup error returned nil")
		}
	})

	t.Run("push result against local tombstone records failure", func(t *testing.T) {
		t.Parallel()

		outcome, err := applyPushRecord(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{
				{values: []any{true}},
				{values: []any{linkID, requestID.String(), "ISS-1", "https://example.test/1", "v1", SyncStateDeleted, true}},
			},
			execs: []pgconn.CommandTag{pgxconnTag("INSERT 0 1")},
		}), pushInput, mapping, pushRecord, pushResult)
		if err != nil || outcome.failed != 1 {
			t.Fatalf("outcome=%+v err=%v, want local tombstone failure", outcome, err)
		}
	})

	t.Run("push downstream errors propagate from record apply", func(t *testing.T) {
		t.Parallel()

		if _, err := applyPushRecord(ctx, ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{true}},
			{err: pgx.ErrNoRows},
			{err: errBoom},
		}}), pushInput, mapping, pushRecord, pushResult); err == nil {
			t.Fatal("applyPushRecord local lookup error returned nil")
		}
		if _, err := applyPushRecord(ctx, ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{true}},
			{err: pgx.ErrNoRows},
			{err: pgx.ErrNoRows},
			{err: errBoom},
		}}), pushInput, mapping, pushRecord, pushResult); err == nil {
			t.Fatal("applyPushRecord upsert link error returned nil")
		}
		if _, err := applyPushRecord(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{
				{values: []any{true}},
				{err: pgx.ErrNoRows},
				{err: pgx.ErrNoRows},
				{values: []any{linkID}},
			},
			execErrs: []error{errBoom},
		}), pushInput, mapping, pushRecord, pushResult); err == nil {
			t.Fatal("applyPushRecord issue link error returned nil")
		}
	})

	t.Run("push local link mismatch creates conflict", func(t *testing.T) {
		t.Parallel()

		outcome, err := applyPushRecord(ctx, ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{true}},
			{err: pgx.ErrNoRows},
			{values: []any{linkID, requestID.String(), "ISS-2", "https://example.test/2", "v1", SyncStateSynced, false}},
			{values: []any{1}},
		}}), pushInput, mapping, pushRecord, pushResult)
		if err != nil || outcome.conflicts != 1 {
			t.Fatalf("outcome=%+v err=%v, want one conflict", outcome, err)
		}
	})

	t.Run("push mutation errors propagate", func(t *testing.T) {
		t.Parallel()

		if _, _, err := upsertPushExternalLink(ctx, ptrext.Of(fakeTx{execErrs: []error{errBoom}}), pushInput, mapping, pushRecord, pushResult, ptrext.Of(objectLinkRow{ID: linkID})); err == nil {
			t.Fatal("upsertPushExternalLink update error returned nil")
		}
		if _, _, err := upsertPushExternalLink(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: errBoom}}}), pushInput, mapping, pushRecord, pushResult, nil); err == nil {
			t.Fatal("upsertPushExternalLink insert error returned nil")
		}
		if err := upsertCustomerRequestIssueLinkFromPush(ctx, ptrext.Of(fakeTx{execErrs: []error{errBoom}}), pushInput, pushRecord, pushResult, requestID, linkID); err == nil {
			t.Fatal("upsertCustomerRequestIssueLinkFromPush error returned nil")
		}
		if _, err := createPushConflict(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: errBoom}}}), pushInput, mapping, pushRecord, pushResult, "link_mismatch"); err == nil {
			t.Fatal("createPushConflict error returned nil")
		}
		if err := insertPushRecordFailure(ctx, ptrext.Of(fakeTx{execErrs: []error{errBoom}}), pushInput, mapping.ID, pushRecord, PushResult{}, "validation", "", false); err == nil {
			t.Fatal("insertPushRecordFailure error returned nil")
		}
	})

	t.Run("pull lookup and mutation errors propagate", func(t *testing.T) {
		t.Parallel()

		if _, err := validateLocalObjectReference(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: errBoom}}}), pullInput, mapping, pullRecord, pullRecord.Payload); err == nil {
			t.Fatal("validateLocalObjectReference error returned nil")
		}
		if _, err := findLinkByExternal(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: errBoom}}}), pullInput.TenantID, mapping.ID, mapping.ExternalObjectType, pullRecord.ExternalKey); err == nil {
			t.Fatal("findLinkByExternal error returned nil")
		}
		if _, err := findLinkByLocal(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: errBoom}}}), pullInput.TenantID, mapping.ID, mapping.LocalObjectType, pullRecord.LocalObjectID); err == nil {
			t.Fatal("findLinkByLocal error returned nil")
		}
		if _, err := insertExternalLink(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: errBoom}}}), pullInput, mapping, pullRecord, pullRecord.LocalObjectID, pullRecord.Payload); err == nil {
			t.Fatal("insertExternalLink error returned nil")
		}
		if _, err := createConflict(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: errBoom}}}), pullInput, mapping, objectLinkRow{ID: linkID, LocalObjectID: pullRecord.LocalObjectID, ExternalKey: pullRecord.ExternalKey}, pullRecord, "version_mismatch", pullRecord.Payload); err == nil {
			t.Fatal("createConflict error returned nil")
		}
		if err := insertRecordFailure(ctx, ptrext.Of(fakeTx{execErrs: []error{errBoom}}), pullInput, mapping.ID, pullRecord, "validation", "bad", pullRecord.Payload, false); err == nil {
			t.Fatal("insertRecordFailure error returned nil")
		}
		if err := upsertCursor(ctx, ptrext.Of(fakeTx{execErrs: []error{errBoom}}), pullInput, ptrext.Of(now)); err == nil {
			t.Fatal("upsertCursor error returned nil")
		}
	})

	t.Run("pull link edge cases propagate", func(t *testing.T) {
		t.Parallel()

		if _, err := updateExternalLink(ctx, ptrext.Of(fakeTx{execErrs: []error{errBoom}}), pullInput, mapping, objectLinkRow{ID: linkID}, pullRecord, pullRecord.Payload); err == nil {
			t.Fatal("updateExternalLink update error returned nil")
		}
		if _, err := updateExternalLink(ctx, ptrext.Of(fakeTx{
			execs:    []pgconn.CommandTag{pgxconnTag("UPDATE 1")},
			execErrs: []error{nil, errBoom},
		}), pullInput, mapping, objectLinkRow{ID: linkID, LocalObjectID: requestID.String()}, pullRecord, pullRecord.Payload); err == nil {
			t.Fatal("updateExternalLink issue link error returned nil")
		}
		if _, err := tombstoneExternalLink(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{err: errBoom}}}), pullInput, mapping, pullRecord, pullRecord.LocalObjectID); err == nil {
			t.Fatal("tombstoneExternalLink row error returned nil")
		}
		if _, err := tombstoneExternalLink(ctx, ptrext.Of(fakeTx{
			rows:     []fakeRow{{values: []any{linkID, requestID.String()}}},
			execErrs: []error{errBoom},
		}), pullInput, mapping, pullRecord, pullRecord.LocalObjectID); err == nil {
			t.Fatal("tombstoneExternalLink stale issue link error returned nil")
		}
		if err := upsertCustomerRequestIssueLink(ctx, ptrext.Of(fakeTx{execErrs: []error{errBoom}}), pullInput, mapping, pullRecord, requestID.String(), linkID, pullRecord.Payload); err == nil {
			t.Fatal("upsertCustomerRequestIssueLink error returned nil")
		}
		if err := upsertCustomerRequestIssueLink(ctx, ptrext.Of(fakeTx{}), pullInput, mapping, pullRecord, "not-a-uuid", linkID, pullRecord.Payload); err != nil {
			t.Fatalf("invalid issue link local id error = %v; want nil skip", err)
		}
		if err := markCustomerRequestIssueLinkStale(ctx, ptrext.Of(fakeTx{execErrs: []error{errBoom}}), pullInput, mapping, pullRecord, requestID.String(), linkID); err == nil {
			t.Fatal("markCustomerRequestIssueLinkStale error returned nil")
		}
		if err := markCustomerRequestIssueLinkStale(ctx, ptrext.Of(fakeTx{}), pullInput, mapping, pullRecord, "not-a-uuid", linkID); err != nil {
			t.Fatalf("invalid stale local id error = %v; want nil skip", err)
		}
	})

	t.Run("pull record local link branches", func(t *testing.T) {
		t.Parallel()

		if _, err := applyPullRecord(ctx, ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{true}},
			{err: errBoom},
		}}), pullInput, mapping, pullRecord); err == nil {
			t.Fatal("applyPullRecord external lookup error returned nil")
		}
		if _, err := applyPullRecord(ctx, ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{true}},
			{err: pgx.ErrNoRows},
			{err: errBoom},
		}}), pullInput, mapping, pullRecord); err == nil {
			t.Fatal("applyPullRecord local lookup error returned nil")
		}
		if _, err := applyPullRecord(ctx, ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{true}},
			{err: pgx.ErrNoRows},
			{err: pgx.ErrNoRows},
			{err: errBoom},
		}}), pullInput, mapping, pullRecord); err == nil {
			t.Fatal("applyPullRecord insert link error returned nil")
		}
		if _, err := applyPullRecord(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{
				{values: []any{true}},
				{err: pgx.ErrNoRows},
				{err: pgx.ErrNoRows},
				{values: []any{linkID}},
			},
			execErrs: []error{errBoom},
		}), pullInput, mapping, pullRecord); err == nil {
			t.Fatal("applyPullRecord issue link error returned nil")
		}
		outcome, err := applyPullRecord(ctx, ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{true}},
			{err: pgx.ErrNoRows},
			{values: []any{linkID, pullRecord.LocalObjectID, "ISS-2", "https://example.test/2", "v1", SyncStateSynced, false}},
			{values: []any{1}},
		}}), pullInput, mapping, pullRecord)
		if err != nil || outcome.conflicts != 1 {
			t.Fatalf("outcome=%+v err=%v, want local link conflict", outcome, err)
		}
		outcome, err = applyPullRecord(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{
				{values: []any{true}},
				{err: pgx.ErrNoRows},
				{values: []any{linkID, pullRecord.LocalObjectID, pullRecord.ExternalKey, "https://example.test/old", "v1", SyncStatePending, false}},
			},
			execs: []pgconn.CommandTag{pgxconnTag("UPDATE 1"), pgxconnTag("INSERT 0 1")},
		}), pullInput, mapping, pullRecord)
		if err != nil || outcome.changed != 1 {
			t.Fatalf("outcome=%+v err=%v, want local link update", outcome, err)
		}
	})

	if parseExternalVersionTime("") != nil {
		t.Fatal("empty external version should not parse")
	}
}

func TestRepoMustListHelpersReturnNilOnQueryErrors(t *testing.T) {
	t.Parallel()

	repo := newCanceledPoolRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	if attempts := mustListAttempts(ctx, repo.pool, runID); attempts != nil {
		t.Fatalf("mustListAttempts = %#v; want nil on query error", attempts)
	}
	if failures := mustListFailures(ctx, repo.pool, "tenant-1", runID); failures != nil {
		t.Fatalf("mustListFailures = %#v; want nil on query error", failures)
	}
	if conflicts := mustListConflicts(ctx, repo.pool, "tenant-1", nil); conflicts != nil {
		t.Fatalf("mustListConflicts nil mapping = %#v; want nil", conflicts)
	}
	if conflicts := mustListConflicts(ctx, repo.pool, "tenant-1", ptrext.Of(mappingID)); conflicts != nil {
		t.Fatalf("mustListConflicts = %#v; want nil on query error", conflicts)
	}
}

func TestRepoMethodsReturnErrorWhenContextCanceled(t *testing.T) {
	t.Parallel()

	repo := newCanceledPoolRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tenantID := "tenant-1"
	connectionID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	mappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	runID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	eventID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	failureID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	conflictID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	now := time.Date(2026, 7, 8, 6, 0, 0, 0, time.UTC)
	mappingIDPtr := mappingID

	checks := []struct {
		name string
		run  func() error
	}{
		{name: "ListConnections", run: func() error {
			_, err := repo.ListConnections(ctx, tenantID)
			return err
		}},
		{name: "GetConnection", run: func() error {
			_, err := repo.GetConnection(ctx, tenantID, connectionID)
			return err
		}},
		{name: "CreateConnection", run: func() error {
			_, err := repo.CreateConnection(ctx, Connection{
				ID:                   connectionID,
				TenantID:             tenantID,
				Provider:             "github",
				Name:                 "GitHub",
				Enabled:              true,
				Status:               ConnectionStatusActive,
				AuthType:             "token",
				ProviderConfig:       []byte(`{}`),
				CredentialKeyID:      "kid",
				CredentialCiphertext: []byte("ciphertext"),
				CreatedBy:            "admin",
				UpdatedBy:            "admin",
			})
			return err
		}},
		{name: "UpdateConnection", run: func() error {
			_, err := repo.UpdateConnection(ctx, Connection{
				ID:             connectionID,
				TenantID:       tenantID,
				Provider:       "github",
				Name:           "GitHub",
				Enabled:        true,
				Status:         ConnectionStatusActive,
				AuthType:       "token",
				ProviderConfig: []byte(`{}`),
				UpdatedBy:      "admin",
			}, false, false)
			return err
		}},
		{name: "DeleteConnection", run: func() error {
			return repo.DeleteConnection(ctx, tenantID, connectionID, "admin")
		}},
		{name: "UpdateConnectionTestResult", run: func() error {
			_, err := repo.UpdateConnectionTestResult(ctx, tenantID, connectionID, false, "failed")
			return err
		}},
		{name: "ResumeConnection", run: func() error {
			_, err := repo.ResumeConnection(ctx, tenantID, connectionID, "admin")
			return err
		}},
		{name: "ListMappings", run: func() error {
			_, err := repo.ListMappings(ctx, tenantID, connectionID)
			return err
		}},
		{name: "GetMapping", run: func() error {
			_, err := repo.GetMapping(ctx, tenantID, mappingID)
			return err
		}},
		{name: "ResolveRunMapping", run: func() error {
			_, err := repo.ResolveRunMapping(ctx, tenantID, connectionID, ptrext.Of(mappingIDPtr))
			return err
		}},
		{name: "UpdateMapping", run: func() error {
			_, err := repo.UpdateMapping(ctx, Mapping{
				ID:                 mappingID,
				TenantID:           tenantID,
				ConnectionID:       connectionID,
				LocalObjectType:    "customer_request",
				ExternalObjectType: "issue",
				Direction:          DirectionPull,
				FieldMapping:       []byte(`{}`),
				StatusMapping:      []byte(`{}`),
				ConflictPolicy:     "manual",
				TombstonePolicy:    "mark_stale",
				Enabled:            true,
			})
			return err
		}},
		{name: "ResetCursor", run: func() error {
			_, err := repo.ResetCursor(ctx, tenantID, mappingID, "admin")
			return err
		}},
		{name: "EnqueueBackfill", run: func() error {
			_, err := repo.EnqueueBackfill(ctx, tenantID, mappingID, "admin", true)
			return err
		}},
		{name: "InsertRun", run: func() error {
			_, err := repo.InsertRun(ctx, SyncRun{
				ID:           runID,
				TenantID:     tenantID,
				ConnectionID: connectionID,
				MappingID:    ptrext.Of(mappingIDPtr),
				Direction:    DirectionPull,
				Trigger:      TriggerManual,
				ActorID:      "admin",
			})
			return err
		}},
		{name: "ListRuns", run: func() error {
			_, err := repo.ListRuns(ctx, ListRunsFilter{TenantID: tenantID, Limit: 10})
			return err
		}},
		{name: "RecordEvent", run: func() error {
			_, err := repo.RecordEvent(ctx, SyncEvent{
				ID:                eventID,
				TenantID:          tenantID,
				ConnectionID:      connectionID,
				Provider:          "github",
				EventType:         "issues",
				DedupeKey:         "github:issues:delivery-1",
				SignatureStatus:   EventSignatureVerified,
				Status:            EventStatusReceived,
				PayloadDigest:     "sha256:abc",
				NormalizedPayload: []byte(`{"action":"opened"}`),
				ReceivedAt:        now,
			})
			return err
		}},
		{name: "ListEvents", run: func() error {
			_, err := repo.ListEvents(ctx, ListEventsFilter{TenantID: tenantID, Limit: 10})
			return err
		}},
		{name: "GetEvent", run: func() error {
			_, err := repo.GetEvent(ctx, tenantID, eventID)
			return err
		}},
		{name: "ReplayEvent", run: func() error {
			_, _, err := repo.ReplayEvent(ctx, tenantID, eventID, "admin", mappingID, DirectionPull)
			return err
		}},
		{name: "GetRunDetail", run: func() error {
			_, err := repo.GetRunDetail(ctx, tenantID, runID)
			return err
		}},
		{name: "RecordTimeline", run: func() error {
			_, err := repo.RecordTimeline(ctx, RecordTimelineFilter{TenantID: tenantID, MappingID: mappingID, LocalObjectID: "cr-1"})
			return err
		}},
		{name: "PrepareRunCursor", run: func() error {
			_, err := repo.PrepareRunCursor(ctx, runID, "worker", tenantID, mappingID, StreamDefault)
			return err
		}},
		{name: "ApplyPullResult", run: func() error {
			_, err := repo.ApplyPullResult(ctx, ApplyPullInput{
				TenantID:     tenantID,
				RunID:        runID,
				ConnectionID: connectionID,
				MappingID:    mappingID,
				Provider:     "github",
				StreamKey:    StreamDefault,
				CursorBefore: []byte(`{}`),
				CursorAfter:  []byte(`{}`),
			})
			return err
		}},
		{name: "PreparePushRecords", run: func() error {
			_, err := repo.PreparePushRecords(ctx, runID, "worker", tenantID, mappingID, "github", 10)
			return err
		}},
		{name: "ApplyPushResult", run: func() error {
			_, err := repo.ApplyPushResult(ctx, ApplyPushInput{
				TenantID:     tenantID,
				RunID:        runID,
				ConnectionID: connectionID,
				MappingID:    mappingID,
				Provider:     "github",
			})
			return err
		}},
		{name: "RecordAttempt", run: func() error {
			return repo.RecordAttempt(ctx, AttemptInput{
				RunID:         runID,
				AttemptNumber: 1,
				StartedAt:     now,
				Result:        "failed",
				ErrorKind:     "provider_unavailable",
				ErrorMessage:  "context canceled",
			})
		}},
		{name: "ClaimBatch", run: func() error {
			_, err := repo.ClaimBatch(ctx, 10, "worker")
			return err
		}},
		{name: "RefreshRunClaim", run: func() error {
			_, err := repo.RefreshRunClaim(ctx, runID, "worker")
			return err
		}},
		{name: "MarkRunSucceeded", run: func() error {
			_, err := repo.MarkRunSucceeded(ctx, runID, "worker")
			return err
		}},
		{name: "MarkRunFailed", run: func() error {
			_, err := repo.MarkRunFailed(ctx, runID, "worker", "provider_unavailable", "context canceled", time.Second, false)
			return err
		}},
		{name: "QuarantineDegradedConnection", run: func() error {
			_, err := repo.QuarantineDegradedConnection(ctx, tenantID, connectionID, "failed")
			return err
		}},
		{name: "RetryRun", run: func() error {
			_, err := repo.RetryRun(ctx, tenantID, runID)
			return err
		}},
		{name: "RetryFailure", run: func() error {
			_, err := repo.RetryFailure(ctx, tenantID, failureID, "admin")
			return err
		}},
		{name: "ResolveConflict", run: func() error {
			_, err := repo.ResolveConflict(ctx, tenantID, conflictID, "local_wins", "admin")
			return err
		}},
		{name: "ResolveConflicts", run: func() error {
			_, err := repo.ResolveConflicts(ctx, tenantID, []uuid.UUID{conflictID}, "local_wins", "admin")
			return err
		}},
		{name: "Health", run: func() error {
			_, err := repo.Health(ctx, tenantID)
			return err
		}},
		{name: "MetricSnapshot", run: func() error {
			_, err := repo.MetricSnapshot(ctx)
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			t.Parallel()
			if err := check.run(); err == nil {
				t.Fatalf("%s returned nil error; want canceled context error", check.name)
			}
		})
	}
}

type fakeTx struct {
	rows     []fakeRow
	execs    []pgconn.CommandTag
	execErrs []error
	execArgs [][]any
	rowIdx   int
	execIdx  int
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

func (tx *fakeTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	tx.execArgs = append(tx.execArgs, args)
	if tx.execIdx < len(tx.execErrs) && tx.execErrs[tx.execIdx] != nil {
		err := tx.execErrs[tx.execIdx]
		tx.execIdx++
		return pgconn.CommandTag{}, err
	}
	if tx.execIdx >= len(tx.execs) {
		tx.execIdx++
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	tag := tx.execs[tx.execIdx]
	tx.execIdx++
	return tag, nil
}

func (tx *fakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call in fakeTx")
}

func (tx *fakeTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.rowIdx >= len(tx.rows) {
		tx.rowIdx++
		return fakeRow{err: errors.New("unexpected QueryRow call in fakeTx")}
	}
	row := tx.rows[tx.rowIdx]
	tx.rowIdx++
	return row
}

func (tx *fakeTx) Conn() *pgx.Conn { return nil }

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("fake row value count does not match scan destination count")
	}
	for i := range dest {
		if err := assignScanValue(dest[i], r.values[i]); err != nil {
			return err
		}
	}
	return nil
}

func assignScanValue(dest, value any) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Pointer || dv.IsNil() {
		return errors.New("scan destination must be a non-nil pointer")
	}
	target := dv.Elem()
	if value == nil {
		target.SetZero()
		return nil
	}
	source := reflect.ValueOf(value)
	if source.Type().AssignableTo(target.Type()) {
		target.Set(source)
		return nil
	}
	if source.Type().ConvertibleTo(target.Type()) {
		target.Set(source.Convert(target.Type()))
		return nil
	}
	return errors.New("scan value is not assignable to destination")
}

func stringPtr(value string) *string {
	return ptrext.Of(value)
}

func timePtr(value time.Time) *time.Time {
	return ptrext.Of(value)
}

func pgxconnTag(value string) pgconn.CommandTag {
	return pgconn.NewCommandTag(value)
}

func issueRunFakeRow(
	runID uuid.UUID,
	tenantID string,
	connectionID uuid.UUID,
	mappingID uuid.UUID,
	now time.Time,
	direction string,
) fakeRow {
	return fakeRow{values: []any{
		runID, tenantID, connectionID, ptrext.Of(mappingID), direction, TriggerManual, RunStatusQueued,
		nil, nil, 0, nil, nil, nil, []byte(`{}`), []byte(`{}`),
		[]byte(`{"local_object_id":"99999999-9999-9999-9999-999999999999"}`), 0, 0, 0, 0, "", "", "admin", now, now,
	}}
}

func newCanceledPoolRepo(t *testing.T) *Repo {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("parse pgx config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return New(pool)
}
