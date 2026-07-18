// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func edgeRepo(pool repoFakePool) *Repo {
	return ptrext.Of(Repo{pool: ptrext.Of(pool)})
}

func TestHelperEdgeBranches(t *testing.T) {
	if URLHost("%zz") != "" {
		t.Fatalf("URLHost(invalid) should return empty host")
	}
	if !errors.Is(mapWriteError(ptrext.Of(pgconn.PgError{Code: "23505"})), ErrConflict) {
		t.Fatalf("mapWriteError(unique) did not map to conflict")
	}
	if _, err := jsonObject(map[string]any{"bad": func() {}}); err == nil {
		t.Fatalf("jsonObject(unsupported value) error = nil")
	}
	decoded, err := decodeObject([]byte("null"))
	if err != nil {
		t.Fatalf("decodeObject(null) error = %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decodeObject(null) = %#v, want empty map", decoded)
	}
	decoded, err = decodeObject(nil)
	if err != nil || len(decoded) != 0 {
		t.Fatalf("decodeObject(nil) = %#v, %v; want empty map", decoded, err)
	}
	if err := validateUnsubscribeTokenShape("custom", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("validateUnsubscribeTokenShape(custom) = %v, want invalid input", err)
	}
	if _, err := scanSettings(scanRow{
		"tenant-1", true, true, []byte(`{}`), []byte("{"), "disabled", true,
		100, 1000, 10, "", time.Now(), time.Now(),
	}); err == nil {
		t.Fatalf("scanSettings(invalid policy json) error = nil")
	}
}

func TestRepoPreflightJSONErrors(t *testing.T) {
	ctx := context.Background()
	r := Repo{}
	badMap := map[string]any{"bad": func() {}}
	if _, err := r.InsertDelivery(ctx, DeliveryInput{Payload: badMap}); err == nil {
		t.Fatalf("InsertDelivery(bad payload) error = nil")
	}
	if _, err := r.UpsertSettings(ctx, Settings{EnabledEventTypes: badMap}); err == nil {
		t.Fatalf("UpsertSettings(bad enabled events) error = nil")
	}
	if _, err := r.UpsertSettings(ctx, Settings{StatusPolicy: badMap}); err == nil {
		t.Fatalf("UpsertSettings(bad status policy) error = nil")
	}
	if _, err := r.CreateWebhookTarget(ctx, WebhookTarget{EventMask: badMap}); err == nil {
		t.Fatalf("CreateWebhookTarget(bad event mask) error = nil")
	}
	if _, err := r.UpdateWebhookTarget(ctx, WebhookTarget{EventMask: badMap}); err == nil {
		t.Fatalf("UpdateWebhookTarget(bad event mask) error = nil")
	}
	if err := r.MarkEventResolved(ctx, uuid.New(), "worker-1", badMap); err == nil {
		t.Fatalf("MarkEventResolved(bad snapshot) error = nil")
	}
}

func TestScanCollectionErrorBranches(t *testing.T) {
	boom := errors.New("boom")
	if _, err := scanWebhookTargets(ptrext.Of(scanRows{err: boom})); !errors.Is(err, boom) {
		t.Fatalf("scanWebhookTargets(rows err) = %v, want boom", err)
	}
	if _, err := scanDeliveries(ptrext.Of(scanRows{err: boom})); !errors.Is(err, boom) {
		t.Fatalf("scanDeliveries(rows err) = %v, want boom", err)
	}
	if _, err := scanEvents(ptrext.Of(scanRows{err: boom})); !errors.Is(err, boom) {
		t.Fatalf("scanEvents(rows err) = %v, want boom", err)
	}
	if _, err := scanWebhookTargets(ptrext.Of(scanRows{rows: []scanRow{{}}})); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("scanWebhookTargets(scan err) = %v, want invalid input", err)
	}
	if _, err := scanDeliveries(ptrext.Of(scanRows{rows: []scanRow{{}}})); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("scanDeliveries(scan err) = %v, want invalid input", err)
	}
	if _, err := scanEvents(ptrext.Of(scanRows{rows: []scanRow{{}}})); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("scanEvents(scan err) = %v, want invalid input", err)
	}
	if _, err := scanEvent(errScanRow{err: pgx.ErrNoRows}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("scanEvent(no rows) = %v, want not found", err)
	}
	if _, err := scanWebhookTarget(scanRow{
		uuid.New(), "tenant-1", "CRM", []byte("url"), "hooks.example.test", []byte("secret"),
		"v1", []byte("{"), true, "active",
		(*time.Time)(nil), (*time.Time)(nil), "user-1", time.Now(), time.Now(),
	}); err == nil {
		t.Fatalf("scanWebhookTarget(invalid event mask) error = nil")
	}
}

func TestUnsubscribeHelperErrorBranches(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	requestID := uuid.New()
	if normalizeUnsubscribeScope(UnsubscribeScopeRequest) != UnsubscribeScopeRequest {
		t.Fatalf("normalizeUnsubscribeScope(request) mismatch")
	}
	if normalizeUnsubscribeScope(SubscriptionScopeTenantUpdates) != UnsubscribeScopeTenant {
		t.Fatalf("normalizeUnsubscribeScope(tenant_updates) mismatch")
	}
	if err := markUnsubscribeTokenUsed(ctx, ptrext.Of(eventTx{execErr: boom}), uuid.New(), "ua"); !errors.Is(err, boom) {
		t.Fatalf("markUnsubscribeTokenUsed(exec err) = %v, want boom", err)
	}
	if _, err := unsubscribeRequestSubscriptions(ctx, ptrext.Of(eventTx{}), UnsubscribeToken{
		Scope: UnsubscribeScopeRequest,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsubscribeRequestSubscriptions(invalid token) = %v, want invalid input", err)
	}
	if _, err := unsubscribeTenantSubscriptions(ctx, ptrext.Of(eventTx{}), UnsubscribeToken{
		RequestID: ptrext.Of(requestID),
		Scope:     UnsubscribeScopeTenant,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsubscribeTenantSubscriptions(invalid token) = %v, want invalid input", err)
	}
	if _, err := lockPreferenceToken(ctx, ptrext.Of(eventTx{
		rows: []pgx.Row{errScanRow{err: pgx.ErrNoRows}},
	}), "tenant-1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lockPreferenceToken(no rows) = %v, want not found", err)
	}
}

func TestRepoFakePoolSettingsBranches(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	settingsRow := scanRow{"tenant-1", true, false, []byte(`{"request.shipped":true}`), []byte(`{}`), "explicit_opt_in", true, 10, 20, 3, "user-1", now, now}

	settings, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{settingsRow}}).GetSettings(ctx, "tenant-1")
	if err != nil || !settings.EmailEnabled {
		t.Fatalf("GetSettings() = %+v, %v", settings, err)
	}
	settings, err = edgeRepo(repoFakePool{rowQueue: []pgx.Row{errScanRow{err: pgx.ErrNoRows}}}).GetSettings(ctx, "tenant-1")
	if err != nil || settings.TenantID != "tenant-1" || settings.DefaultConsentMode != "disabled" {
		t.Fatalf("GetSettings(default) = %+v, %v", settings, err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{settingsRow}}).UpsertSettings(ctx, Settings{TenantID: "tenant-1"}); err != nil {
		t.Fatalf("UpsertSettings() error = %v", err)
	}
}

func TestRepoFakePoolWebhookTargetBranches(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	ctx := context.Background()
	targetRow := scanRow{id, "tenant-1", "CRM", []byte("url"), "hooks.example.test", []byte("secret"), "v1", []byte(`{}`), true, "active", ptrext.Of(now), ptrext.Of(now), "user-1", now, now}

	targets, err := edgeRepo(repoFakePool{rowsQueue: []pgx.Rows{ptrext.Of(scanRows{rows: []scanRow{targetRow}})}}).ListWebhookTargets(ctx, "tenant-1")
	if err != nil || len(targets) != 1 {
		t.Fatalf("ListWebhookTargets() = %+v, %v", targets, err)
	}
	targets, err = edgeRepo(repoFakePool{rowsQueue: []pgx.Rows{ptrext.Of(scanRows{rows: []scanRow{targetRow}})}}).ListActiveWebhookTargets(ctx, "tenant-1")
	if err != nil || len(targets) != 1 {
		t.Fatalf("ListActiveWebhookTargets() = %+v, %v", targets, err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{targetRow}}).CreateWebhookTarget(ctx, WebhookTarget{EventMask: map[string]any{}}); err != nil {
		t.Fatalf("CreateWebhookTarget() error = %v", err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{targetRow}}).UpdateWebhookTarget(ctx, WebhookTarget{EventMask: map[string]any{}}); err != nil {
		t.Fatalf("UpdateWebhookTarget() error = %v", err)
	}
	if err := edgeRepo(repoFakePool{execTags: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 1")}}).DeleteWebhookTarget(ctx, "tenant-1", id); err != nil {
		t.Fatalf("DeleteWebhookTarget() error = %v", err)
	}
	if err := edgeRepo(repoFakePool{execTags: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 0")}}).DeleteWebhookTarget(ctx, "tenant-1", id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteWebhookTarget(not found) = %v, want not found", err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{targetRow}}).MarkWebhookTargetTested(ctx, "tenant-1", id, true); err != nil {
		t.Fatalf("MarkWebhookTargetTested() error = %v", err)
	}
}

func TestRepoFakePoolSenderBranches(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	ctx := context.Background()
	senderRow := scanRow{id, "tenant-1", "Attune", "from-hash", []byte("from"), "", []byte(nil), "example.test", "verified", "verified", "verified", "email", []byte(`{}`), "active", ptrext.Of(now), "user-1", now, now}

	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{senderRow}}).UpsertSender(ctx, Sender{}); err != nil {
		t.Fatalf("UpsertSender() error = %v", err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{senderRow}}).LatestSender(ctx, "tenant-1"); err != nil {
		t.Fatalf("LatestSender() error = %v", err)
	}
}

func TestRepoFakePoolContactBranches(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	requestID := uuid.New()
	ctx := context.Background()
	contactRow := scanRow{id, "tenant-1", "subject", "hash", "Jane", "Example", "email-hash", []byte("email"), ptrext.Of(now), ConsentOptedIn, "portal", "v1", "consent", ptrext.Of(now), "en", "UTC", (*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), "", now, now}
	subRow := scanRow{uuid.New(), "tenant-1", pgtype.UUID{Bytes: requestID, Valid: true}, id, SubscriptionScopeRequest, SourceVoter, SubscriptionStatusActive, (*time.Time)(nil), now, now}
	subscriberRow := scanRow{id, "Jane", "Example", []byte("email"), ConsentOptedIn, SubscriptionStatusActive, []string{SourceVoter}, ptrext.Of(now), (*time.Time)(nil)}

	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{contactRow}}).UpsertContact(ctx, Contact{}); err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{subRow}}).UpsertRequestSubscription(ctx, Subscription{}); err != nil {
		t.Fatalf("UpsertRequestSubscription() error = %v", err)
	}
	subscribers, err := edgeRepo(repoFakePool{rowsQueue: []pgx.Rows{ptrext.Of(scanRows{rows: []scanRow{subscriberRow}})}}).ListSubscribers(ctx, "tenant-1", requestID)
	if err != nil || len(subscribers) != 1 {
		t.Fatalf("ListSubscribers() = %+v, %v", subscribers, err)
	}
	subscribers, err = edgeRepo(repoFakePool{rowsQueue: []pgx.Rows{ptrext.Of(scanRows{rows: []scanRow{subscriberRow}})}}).EligibleRequestRecipients(ctx, "tenant-1", requestID)
	if err != nil || len(subscribers) != 1 {
		t.Fatalf("EligibleRequestRecipients() = %+v, %v", subscribers, err)
	}
}

func TestRepoFakePoolDeliveryBranches(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	eventID := uuid.New()
	ctx := context.Background()
	deliveryRow := scanRow{int64(42), "tenant-1", eventID, ptrext.Of(uuid.New()), ptrext.Of(id), (*uuid.UUID)(nil), ChannelEmail, "sha256:abc", []byte(`{"ok":true}`), []byte("secret"), DeliveryStatusFailed, 2, "transient", 503, "failed", "", "trace-1", now, (*time.Time)(nil), ptrext.Of(now), ptrext.Of(now), "user-1", 1}

	deliveryID, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{scanRow{int64(42)}}}).InsertDelivery(ctx, DeliveryInput{Payload: map[string]any{}})
	if err != nil || deliveryID != 42 {
		t.Fatalf("InsertDelivery() = %d, %v", deliveryID, err)
	}
	deliveryID, err = edgeRepo(repoFakePool{rowQueue: []pgx.Row{errScanRow{err: pgx.ErrNoRows}}}).InsertDelivery(ctx, DeliveryInput{Payload: map[string]any{}})
	if err != nil || deliveryID != 0 {
		t.Fatalf("InsertDelivery(conflict) = %d, %v", deliveryID, err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{scanRow{3}}}).CountTenantEmailDeliveriesSince(ctx, "tenant-1", now); err != nil {
		t.Fatalf("CountTenantEmailDeliveriesSince() error = %v", err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{scanRow{4}}}).CountContactEmailDeliveriesSince(ctx, "tenant-1", id, now); err != nil {
		t.Fatalf("CountContactEmailDeliveriesSince() error = %v", err)
	}
	deliveries, err := edgeRepo(repoFakePool{rowsQueue: []pgx.Rows{ptrext.Of(scanRows{rows: []scanRow{deliveryRow}})}}).ClaimDeliveries(ctx, 1, "worker-1")
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("ClaimDeliveries() = %+v, %v", deliveries, err)
	}
	deliveries, err = edgeRepo(repoFakePool{rowsQueue: []pgx.Rows{ptrext.Of(scanRows{rows: []scanRow{deliveryRow}})}}).ListDeliveries(ctx, ListDeliveryFilter{TenantID: "tenant-1", Limit: 1})
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("ListDeliveries() = %+v, %v", deliveries, err)
	}
}

func TestRepoFakePoolChangelogListBranches(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()
	postID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	olderPostID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	requestID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	result, err := edgeRepo(repoFakePool{
		rowQueue: []pgx.Row{
			scanRow{false, true, true},
		},
		rowsQueue: []pgx.Rows{
			ptrext.Of(scanRows{rows: []scanRow{
				{postID, uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), "Release notes: CSV export", "We shipped CSV export.\n\nCustomers can export their data from the requests page.", "changelog_post", now},
				{olderPostID, uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), "Release notes: Search", "Search is live.", "changelog_post", now.Add(-time.Hour)},
			}}),
			ptrext.Of(scanRows{rows: []scanRow{
				{postID, requestID, "csv-export", "CSV export", "Customers can export their data from the requests page.", "shipped", "done"},
			}}),
		},
	}).ListChangelogPosts(ctx, "tenant-1", 1, "0")
	assertChangelogListPage(t, result, err, postID)
	assertChangelogListPageRequests(t, result)

	request, err := edgeRepo(repoFakePool{
		rowQueue: []pgx.Row{
			scanRow{requestID, "csv-export", "CSV export", "Customers can export their data from the requests page.", "shipped", "done"},
		},
	}).GetChangelogRequest(ctx, "tenant-1", requestID)
	assertChangelogRequestResult(t, request, err, requestID)

	assertChangelogPostsDisabled(ctx, t, "tenant-1", repoFakePool{rowQueue: []pgx.Row{scanRow{true, false, false}}})
	assertChangelogRequestNotFound(ctx, t, "tenant-1", requestID, repoFakePool{rowQueue: []pgx.Row{errScanRow{err: pgx.ErrNoRows}}})

	emptyResult, err := edgeRepo(repoFakePool{
		rowQueue: []pgx.Row{
			scanRow{true, true, false},
		},
		rowsQueue: []pgx.Rows{
			ptrext.Of(scanRows{}),
		},
	}).ListChangelogPosts(ctx, "tenant-1", 10, "")
	assertChangelogEmptyResult(t, emptyResult, err)
	assertChangelogInvalidCursor(ctx, t, "tenant-1", repoFakePool{rowQueue: []pgx.Row{scanRow{true, true, false}}})
}

func TestRepoFakePoolChangelogRequestBranches(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	request, err := edgeRepo(repoFakePool{
		rowQueue: []pgx.Row{
			scanRow{requestID, "csv-export", "CSV export", "Customers can export their data from the requests page.", "shipped", "done"},
		},
	}).GetChangelogRequest(ctx, "tenant-1", requestID)
	if err != nil {
		t.Fatalf("GetChangelogRequest() error = %v", err)
	}
	if request.ID != requestID || request.PublicTitle != "CSV export" || request.RoadmapColumn != "done" {
		t.Fatalf("GetChangelogRequest() = %+v, want public-safe request data", request)
	}

	if _, err := edgeRepo(repoFakePool{
		rowQueue: []pgx.Row{errScanRow{err: pgx.ErrNoRows}},
	}).GetChangelogRequest(ctx, "tenant-1", requestID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetChangelogRequest(missing) = %v, want not found", err)
	}
}

func assertChangelogListPage(t *testing.T, result ChangelogListResult, err error, postID uuid.UUID) {
	t.Helper()
	if err != nil {
		t.Fatalf("ListChangelogPosts() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != postID || result.NextCursor != "1" || !result.NoIndex || !result.HidePublicTimestamps {
		t.Fatalf("ListChangelogPosts() = %+v, want first page with hidden timestamps", result)
	}
}

func assertChangelogListPageRequests(t *testing.T, result ChangelogListResult) {
	t.Helper()
	if len(result.Items[0].Requests) != 1 || result.Items[0].Requests[0].PublicSlug != "csv-export" {
		t.Fatalf("ListChangelogPosts() requests = %+v, want linked public request", result.Items[0].Requests)
	}
}

func assertChangelogRequestResult(t *testing.T, request ChangelogRequest, err error, requestID uuid.UUID) {
	t.Helper()
	if err != nil {
		t.Fatalf("GetChangelogRequest() error = %v", err)
	}
	if request.ID != requestID || request.PublicTitle != "CSV export" || request.RoadmapColumn != "done" {
		t.Fatalf("GetChangelogRequest() = %+v, want public-safe request data", request)
	}
}

func assertChangelogPostsDisabled(ctx context.Context, t *testing.T, tenantID string, pool repoFakePool) {
	t.Helper()
	if _, err := edgeRepo(pool).ListChangelogPosts(ctx, tenantID, 10, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListChangelogPosts(disabled) = %v, want not found", err)
	}
}

func assertChangelogRequestNotFound(ctx context.Context, t *testing.T, tenantID string, requestID uuid.UUID, pool repoFakePool) {
	t.Helper()
	if _, err := edgeRepo(pool).GetChangelogRequest(ctx, tenantID, requestID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetChangelogRequest(missing) = %v, want not found", err)
	}
}

func assertChangelogEmptyResult(t *testing.T, result ChangelogListResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("ListChangelogPosts(empty) error = %v", err)
	}
	if len(result.Items) != 0 || result.NextCursor != "" || result.NoIndex || result.HidePublicTimestamps {
		t.Fatalf("ListChangelogPosts(empty) = %+v, want empty public-indexed result", result)
	}
}

func assertChangelogInvalidCursor(ctx context.Context, t *testing.T, tenantID string, pool repoFakePool) {
	t.Helper()
	if _, err := edgeRepo(pool).ListChangelogPosts(ctx, tenantID, 10, "bad-cursor"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListChangelogPosts(invalid cursor) = %v, want invalid input", err)
	}
}

func TestRepoFakePoolEventAndMarkBranches(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	requestID := uuid.New()
	updateID := uuid.New()
	ctx := context.Background()
	eventRow := scanRow{id, "tenant-1", ptrext.Of(requestID), ptrext.Of(updateID), (*uuid.UUID)(nil), EventTypeShipped, "public_broadcast", "dedupe", "planned", "shipped", "user", "user-1", EventStatusPending, 1, []byte(`{"channels":["email"]}`), now}

	ref, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{scanRow{"tenant-1", requestID, "csv-export", "CSV export", "published"}}}).ResolvePublicRequest(ctx, "acme", "csv-export")
	if err != nil || ref.RequestID != requestID {
		t.Fatalf("ResolvePublicRequest() = %+v, %v", ref, err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{scanRow{"tenant-1"}}}).ResolveTenantIDBySlug(ctx, "acme"); err != nil {
		t.Fatalf("ResolveTenantIDBySlug() error = %v", err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{scanRow{requestID, "7", "Title", "Body", "planned"}}}).GetRequestSummary(ctx, "tenant-1", requestID); err != nil {
		t.Fatalf("GetRequestSummary() error = %v", err)
	}
	if _, err := edgeRepo(repoFakePool{rowQueue: []pgx.Row{scanRow{"acme", requestID, "7", "Title", "Body", "planned", updateID, "Update", "Text", "status_change"}}}).GetEventContext(ctx, id); err != nil {
		t.Fatalf("GetEventContext() error = %v", err)
	}
	events, err := edgeRepo(repoFakePool{rowsQueue: []pgx.Rows{ptrext.Of(scanRows{rows: []scanRow{eventRow}})}}).ClaimEvents(ctx, 1, "worker-1")
	if err != nil || len(events) != 1 {
		t.Fatalf("ClaimEvents() = %+v, %v", events, err)
	}
	for _, call := range []struct {
		name        string
		notFoundErr bool
		fn          func(*Repo) error
	}{
		{"MarkDeliveryDelivered", false, func(r *Repo) error { _, err := r.MarkDeliveryDelivered(ctx, 1, "worker-1"); return err }},
		{"MarkDeliveryFailed", false, func(r *Repo) error {
			_, err := r.MarkDeliveryFailed(ctx, 1, "worker-1", "failed", "kind", 500, time.Second)
			return err
		}},
		{"MarkDeliveryDead", false, func(r *Repo) error { _, err := r.MarkDeliveryDead(ctx, 1, "worker-1", "dead", "kind", 400); return err }},
		{"MarkEventResolved", true, func(r *Repo) error { return r.MarkEventResolved(ctx, id, "worker-1", map[string]any{}) }},
		{"MarkEventFailed", true, func(r *Repo) error { return r.MarkEventFailed(ctx, id, "worker-1", "failed", time.Second) }},
	} {
		if err := call.fn(edgeRepo(repoFakePool{execTags: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")}})); err != nil {
			t.Fatalf("%s() success error = %v", call.name, err)
		}
		err := call.fn(edgeRepo(repoFakePool{execTags: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 0")}}))
		if call.notFoundErr && !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s() not found = %v, want not found", call.name, err)
		}
		if !call.notFoundErr && err != nil {
			t.Fatalf("%s() zero rows error = %v", call.name, err)
		}
	}
}

func TestRepoFakePoolUnsubscribeTokenBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	tokenID := uuid.New()
	contactID := uuid.New()
	requestID := uuid.New()
	subID := uuid.New()
	tokenRow := scanRow{
		tokenID,
		"tenant-1",
		contactID,
		ptrext.Of(requestID),
		UnsubscribeScopeRequest,
		ptrext.Of(now.Add(time.Hour)),
		(*time.Time)(nil),
		now,
	}
	subRow := scanRow{
		subID,
		"tenant-1",
		pgtype.UUID{Bytes: requestID, Valid: true},
		contactID,
		SubscriptionScopeRequest,
		SourceVoter,
		SubscriptionStatusActive,
		ptrext.Of(now),
		now,
		now,
	}
	repoWithTx := func(tx *eventTx) *Repo {
		return edgeRepo(repoFakePool{beginTx: tx})
	}

	if err := edgeRepo(repoFakePool{}).CreateUnsubscribeToken(ctx, "tenant-1", contactID, ptrext.Of(requestID), UnsubscribeScopeRequest, "hash", now); err != nil {
		t.Fatalf("CreateUnsubscribeToken() error = %v", err)
	}
	if err := edgeRepo(repoFakePool{}).CreateUnsubscribeToken(ctx, "tenant-1", contactID, nil, UnsubscribeScopeRequest, "hash", now); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateUnsubscribeToken(invalid shape) = %v, want invalid input", err)
	}
	if err := edgeRepo(repoFakePool{execErr: errors.New("exec failed")}).CreateUnsubscribeToken(ctx, "tenant-1", contactID, ptrext.Of(requestID), UnsubscribeScopeRequest, "hash", now); err == nil {
		t.Fatalf("CreateUnsubscribeToken(exec err) error = nil")
	}
	if _, err := repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{tokenRow, subRow}})).UseUnsubscribeToken(ctx, "tenant-1", "hash", "ua"); err != nil {
		t.Fatalf("UseUnsubscribeToken() error = %v", err)
	}
	if _, err := edgeRepo(repoFakePool{beginErr: errors.New("begin failed")}).UseUnsubscribeToken(ctx, "tenant-1", "hash", "ua"); err == nil {
		t.Fatalf("UseUnsubscribeToken(begin err) error = nil")
	}
	_, err := repoWithTx(ptrext.Of(eventTx{
		rows: []pgx.Row{errScanRow{err: pgx.ErrNoRows}},
	})).UseUnsubscribeToken(ctx, "tenant-1", "hash", "ua")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UseUnsubscribeToken(lock err) = %v, want not found", err)
	}
	used := now
	_, err = repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{scanRow{
		tokenID, "tenant-1", contactID, ptrext.Of(requestID),
		UnsubscribeScopeRequest, ptrext.Of(now.Add(time.Hour)), ptrext.Of(used), now,
	}}})).UseUnsubscribeToken(ctx, "tenant-1", "hash", "ua")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UseUnsubscribeToken(used) = %v, want not found", err)
	}
	_, err = repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{scanRow{
		tokenID, "tenant-1", contactID, ptrext.Of(requestID),
		UnsubscribeScopeRequest, ptrext.Of(now.Add(-time.Hour)), (*time.Time)(nil), now,
	}}})).UseUnsubscribeToken(ctx, "tenant-1", "hash", "ua")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UseUnsubscribeToken(expired) = %v, want not found", err)
	}
	if _, err := repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{tokenRow}, execErr: errors.New("mark failed")})).UseUnsubscribeToken(ctx, "tenant-1", "hash", "ua"); err == nil {
		t.Fatalf("UseUnsubscribeToken(mark err) error = nil")
	}
	if _, err := repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{scanRow{
		tokenID, "tenant-1", contactID, ptrext.Of(requestID),
		"custom", ptrext.Of(now.Add(time.Hour)), (*time.Time)(nil), now,
	}}})).UseUnsubscribeToken(ctx, "tenant-1", "hash", "ua"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UseUnsubscribeToken(custom scope) = %v, want invalid input", err)
	}
	if _, err := repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{tokenRow, subRow}, commitErr: errors.New("commit failed")})).UseUnsubscribeToken(ctx, "tenant-1", "hash", "ua"); err == nil {
		t.Fatalf("UseUnsubscribeToken(commit err) error = nil")
	}
}

func TestRepoFakePoolConfirmContactTokenBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	tokenID := uuid.New()
	contactID := uuid.New()
	tokenRow := scanRow{
		tokenID, "tenant-1", contactID, (*uuid.UUID)(nil),
		UnsubscribeScopeTenant, ptrext.Of(now.Add(time.Hour)),
		(*time.Time)(nil), now,
	}
	contactRow := scanRow{
		contactID, "tenant-1", "subject", "hash", "Jane", "Example",
		"email-hash", []byte("email"), ptrext.Of(now), ConsentOptedIn,
		"portal", "v1", "consent", ptrext.Of(now), "en", "UTC",
		(*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), "", now, now,
	}
	repoWithTx := func(tx *eventTx) *Repo {
		return edgeRepo(repoFakePool{beginTx: tx})
	}

	if _, err := repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{tokenRow, contactRow}})).ConfirmContactToken(ctx, "tenant-1", "hash", "ua"); err != nil {
		t.Fatalf("ConfirmContactToken() error = %v", err)
	}
	if _, err := edgeRepo(repoFakePool{beginErr: errors.New("begin failed")}).ConfirmContactToken(ctx, "tenant-1", "hash", "ua"); err == nil {
		t.Fatalf("ConfirmContactToken(begin err) error = nil")
	}
	_, err := repoWithTx(ptrext.Of(eventTx{
		rows: []pgx.Row{errScanRow{err: pgx.ErrNoRows}},
	})).ConfirmContactToken(ctx, "tenant-1", "hash", "ua")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfirmContactToken(lock err) = %v, want not found", err)
	}
	used := now
	_, err = repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{scanRow{
		tokenID, "tenant-1", contactID, (*uuid.UUID)(nil),
		UnsubscribeScopeTenant, ptrext.Of(now.Add(time.Hour)), ptrext.Of(used), now,
	}}})).ConfirmContactToken(ctx, "tenant-1", "hash", "ua")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfirmContactToken(used) = %v, want not found", err)
	}
	_, err = repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{scanRow{
		tokenID, "tenant-1", contactID, (*uuid.UUID)(nil),
		UnsubscribeScopeTenant, ptrext.Of(now.Add(-time.Hour)), (*time.Time)(nil), now,
	}}})).ConfirmContactToken(ctx, "tenant-1", "hash", "ua")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfirmContactToken(expired) = %v, want not found", err)
	}
	if _, err := repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{tokenRow}, execErr: errors.New("mark failed")})).ConfirmContactToken(ctx, "tenant-1", "hash", "ua"); err == nil {
		t.Fatalf("ConfirmContactToken(mark err) error = nil")
	}
	if _, err := repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{tokenRow, errScanRow{err: errors.New("contact failed")}}})).ConfirmContactToken(ctx, "tenant-1", "hash", "ua"); err == nil {
		t.Fatalf("ConfirmContactToken(contact scan err) error = nil")
	}
	if _, err := repoWithTx(ptrext.Of(eventTx{rows: []pgx.Row{tokenRow, contactRow}, commitErr: errors.New("commit failed")})).ConfirmContactToken(ctx, "tenant-1", "hash", "ua"); err == nil {
		t.Fatalf("ConfirmContactToken(commit err) error = nil")
	}
}

func TestRepoFakePoolRemainingErrorBranches(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.New()
	threadID := uuid.New()
	updateID := uuid.New()
	r := Repo{}
	if _, err := edgeRepo(repoFakePool{
		rowsQueue: []pgx.Rows{ptrext.Of(scanRows{rows: []scanRow{{}}})},
	}).ListSubscribers(ctx, "tenant-1", requestID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListSubscribers(scan err) = %v, want invalid input", err)
	}
	if _, err := edgeRepo(repoFakePool{
		rowsQueue: []pgx.Rows{ptrext.Of(scanRows{rows: []scanRow{{}}})},
	}).EligibleRequestRecipients(ctx, "tenant-1", requestID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("EligibleRequestRecipients(scan err) = %v, want invalid input", err)
	}
	if _, err := r.CreatePublicUpdateEventTx(ctx, ptrext.Of(eventTx{
		rows: []pgx.Row{scanRow{threadID}, errScanRow{err: errors.New("post failed")}},
	}), PublicUpdateInput{}); err == nil {
		t.Fatalf("CreatePublicUpdateEventTx(post err) error = nil")
	}
	if _, err := r.CreatePublicUpdateEventTx(ctx, ptrext.Of(eventTx{
		rows:    []pgx.Row{scanRow{threadID}, scanRow{updateID}},
		execErr: errors.New("link failed"),
	}), PublicUpdateInput{}); err == nil {
		t.Fatalf("CreatePublicUpdateEventTx(link err) error = nil")
	}
	if _, err := r.CreatePublicUpdateEventTx(ctx, ptrext.Of(eventTx{
		rows: []pgx.Row{scanRow{threadID}, scanRow{updateID}, errScanRow{err: errors.New("event failed")}},
	}), PublicUpdateInput{}); err == nil {
		t.Fatalf("CreatePublicUpdateEventTx(event err) error = nil")
	}
}

type repoFakePool struct {
	rowQueue  []pgx.Row
	rowsQueue []pgx.Rows
	execTags  []pgconn.CommandTag
	beginTx   pgx.Tx
	beginErr  error
	execErr   error
	queryErr  error
}

func (p *repoFakePool) Begin(context.Context) (pgx.Tx, error) {
	if p.beginErr != nil {
		return nil, p.beginErr
	}
	if p.beginTx != nil {
		return p.beginTx, nil
	}
	return ptrext.Of(eventTx{}), nil
}

func (p *repoFakePool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	if p.execErr != nil {
		return pgconn.CommandTag{}, p.execErr
	}
	if len(p.execTags) == 0 {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	tag := p.execTags[0]
	p.execTags = p.execTags[1:]
	return tag, nil
}

func (p *repoFakePool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if p.queryErr != nil {
		return nil, p.queryErr
	}
	if len(p.rowsQueue) == 0 {
		return ptrext.Of(scanRows{}), nil
	}
	rows := p.rowsQueue[0]
	p.rowsQueue = p.rowsQueue[1:]
	return rows, nil
}

func (p *repoFakePool) QueryRow(context.Context, string, ...any) pgx.Row {
	if len(p.rowQueue) == 0 {
		return errScanRow{err: errors.New("unexpected QueryRow")}
	}
	row := p.rowQueue[0]
	p.rowQueue = p.rowQueue[1:]
	return row
}
