// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestEmailHashNormalizesCaseAndWhitespace(t *testing.T) {
	left := EmailHash("  Customer@Example.COM ")
	right := EmailHash("customer@example.com")
	if left == "" {
		t.Fatalf("EmailHash() returned empty string")
	}
	if left != right {
		t.Fatalf("EmailHash() = %q, want %q", left, right)
	}
}

func TestDestinationHashPreservesSchemeAndAddsPrefix(t *testing.T) {
	hash := DestinationHash(" https://hooks.example.test/attune ")
	if hash == "" {
		t.Fatalf("DestinationHash() returned empty string")
	}
	if len(hash) != len("sha256:")+64 {
		t.Fatalf("DestinationHash() length = %d", len(hash))
	}
	if hash[:len("sha256:")] != "sha256:" {
		t.Fatalf("DestinationHash() = %q, want sha256 prefix", hash)
	}
}

func TestURLHostReturnsLowercaseHost(t *testing.T) {
	got := URLHost("https://Hooks.EXAMPLE.test:8443/attune")
	if got != "hooks.example.test:8443" {
		t.Fatalf("URLHost() = %q", got)
	}
}

func TestJSONObjectsDecodeAndDefaultEmptyMaps(t *testing.T) {
	raw, err := jsonObject(nil)
	if err != nil {
		t.Fatalf("jsonObject(nil) error = %v", err)
	}
	decoded, err := decodeObject(raw)
	if err != nil {
		t.Fatalf("decodeObject() error = %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded = %#v, want empty map", decoded)
	}

	raw, err = jsonObject(map[string]any{"enabled": true, "count": float64(2)})
	if err != nil {
		t.Fatalf("jsonObject(map) error = %v", err)
	}
	decoded, err = decodeObject(raw)
	if err != nil {
		t.Fatalf("decodeObject(map) error = %v", err)
	}
	if decoded["enabled"] != true || decoded["count"] != float64(2) {
		t.Fatalf("decoded = %#v", decoded)
	}
	if _, err := decodeObject([]byte("{")); err == nil {
		t.Fatalf("decodeObject(invalid) error = nil")
	}
}

func TestDefaultSettingsLimitAndErrorMapping(t *testing.T) {
	settings := DefaultSettings("tenant-1")
	if settings.TenantID != "tenant-1" || settings.DefaultConsentMode != "disabled" ||
		settings.MaxRecipientsWithoutConfirm != 100 || !settings.RequirePublicUpdateForStatus {
		t.Fatalf("DefaultSettings() = %+v", settings)
	}
	if boundedLimit(-1) != 50 || boundedLimit(0) != 50 || boundedLimit(201) != maxListLimit || boundedLimit(25) != 25 {
		t.Fatalf("boundedLimit() did not clamp as expected")
	}
	if !errors.Is(mapNotFound(pgx.ErrNoRows), ErrNotFound) {
		t.Fatalf("mapNotFound(ErrNoRows) did not map to ErrNotFound")
	}
	boom := errors.New("boom")
	if !errors.Is(mapNotFound(boom), boom) {
		t.Fatalf("mapNotFound(non-not-found) changed the error")
	}
}

func TestScanSettingsRow(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	settings, err := scanSettings(scanRow{
		"tenant-1", true, true, []byte(`{"request.shipped":true}`),
		[]byte(`{"shipped":true}`), "explicit_opt_in", false,
		25, 250, 3, "user-1", now, now,
	})
	if err != nil {
		t.Fatalf("scanSettings() error = %v", err)
	}
	if settings == nil || settings.EnabledEventTypes[EventTypeShipped] != true ||
		settings.MaxRecipientsWithoutConfirm != 25 {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestScanContactSubscriptionAndSubscriberRows(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	requestID := uuid.New()
	contact, err := scanContact(scanRow{
		id, "tenant-1", "subject", "subject-hash", "Jane", "Example Co",
		"email-hash", []byte("email-payload"), ptrext.Of(now), ConsentOptedIn,
		"portal", "v1", "consent", ptrext.Of(now), "en", "UTC",
		(*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), "", now, now,
	})
	if err != nil {
		t.Fatalf("scanContact() error = %v", err)
	}
	if contact.ID != id || contact.DisplayName != "Jane" || contact.ConsentState != ConsentOptedIn {
		t.Fatalf("contact = %+v", contact)
	}

	sub, err := scanSubscription(scanRow{
		uuid.New(),
		"tenant-1",
		pgtype.UUID{
			Bytes: requestID,
			Valid: true,
		},
		id,
		SubscriptionScopeRequest,
		SourceVoter,
		SubscriptionStatusActive,
		(*time.Time)(nil),
		now,
		now,
	})
	if err != nil {
		t.Fatalf("scanSubscription() error = %v", err)
	}
	if sub.RequestID != requestID || sub.ContactID != id || sub.Source != SourceVoter {
		t.Fatalf("subscription = %+v", sub)
	}

	tenantSub, err := scanSubscription(scanRow{
		uuid.New(),
		"tenant-1",
		pgtype.UUID{},
		id,
		SubscriptionScopeTenantUpdates,
		SourceManual,
		"unsubscribed",
		ptrext.Of(now),
		now,
		now,
	})
	if err != nil {
		t.Fatalf("scanSubscription(tenant scope) error = %v", err)
	}
	if tenantSub.RequestID != uuid.Nil || tenantSub.Scope != SubscriptionScopeTenantUpdates {
		t.Fatalf("tenant subscription = %+v", tenantSub)
	}

	subscriber, err := scanSubscriber(scanRow{
		id, "Jane", "Example Co", []byte("email-payload"), ConsentOptedIn,
		SubscriptionStatusActive,
		[]string{SourceVoter},
		ptrext.Of(now),
		(*time.Time)(nil),
	})
	if err != nil {
		t.Fatalf("scanSubscriber() error = %v", err)
	}
	if subscriber.ContactID != id || len(subscriber.Sources) != 1 {
		t.Fatalf("subscriber = %+v", subscriber)
	}
}

func TestScanSenderAndWebhookTargetRows(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	id := uuid.New()

	sender, err := scanSender(scanRow{
		id, "tenant-1", "Attune", "from-hash", []byte("from"),
		"reply-hash", []byte("reply"), "example.test", "verified", "verified",
		"verified", "email", []byte("{}"), "active", ptrext.Of(now), "user-1", now, now,
	})
	if err != nil {
		t.Fatalf("scanSender() error = %v", err)
	}
	if sender.ID != id || sender.Domain != "example.test" || sender.Status != "active" {
		t.Fatalf("sender = %+v", sender)
	}

	target, err := scanWebhookTarget(scanRow{
		id, "tenant-1", "CRM", []byte("url"), "hooks.example.test", []byte("secret"),
		"v1", []byte(`{"request.shipped":true}`), true, "active",
		ptrext.Of(now), ptrext.Of(now), "user-1", now, now,
	})
	if err != nil {
		t.Fatalf("scanWebhookTarget() error = %v", err)
	}
	if target.ID != id || target.EventMask[EventTypeShipped] != true || !target.IncludeRecipientIdentity {
		t.Fatalf("target = %+v", target)
	}
}

func TestScanDeliveryAndEventRows(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	requestID := uuid.New()
	updateID := uuid.New()

	delivery, err := scanDelivery(scanRow{
		int64(42), "tenant-1", uuid.New(), (*uuid.UUID)(nil), ptrext.Of(id),
		(*uuid.UUID)(nil), ChannelEmail, "sha256:abc", []byte(`{"event_id":"event-1"}`),
		[]byte("secret"), DeliveryStatusFailed, 2, "transient", 503,
		"temporary", "", "trace-1", now, (*time.Time)(nil), ptrext.Of(now),
		ptrext.Of(now), "user-1", 1,
	})
	if err != nil {
		t.Fatalf("scanDelivery() error = %v", err)
	}
	if delivery.ID != 42 || delivery.Payload["event_id"] != "event-1" || delivery.HTTPStatus != 503 {
		t.Fatalf("delivery = %+v", delivery)
	}

	event, err := scanEvent(scanRow{
		uuid.New(), "tenant-1", ptrext.Of(requestID), ptrext.Of(updateID), (*uuid.UUID)(nil),
		EventTypeShipped, "public_broadcast", "dedupe", "planned", "shipped",
		"user", "user-1", EventStatusPending, 1, []byte(`{"channels":["email"]}`), now,
	})
	if err != nil {
		t.Fatalf("scanEvent() error = %v", err)
	}
	if event.EventType != EventTypeShipped || event.RecipientSnapshot["channels"] == nil {
		t.Fatalf("event = %+v", event)
	}
}

func TestScanRowsMapErrors(t *testing.T) {
	if _, err := scanSettings(scanRow{
		"tenant-1", true, true, []byte("{"), []byte(`{}`), "disabled", true,
		100, 1000, 10, "", time.Now(), time.Now(),
	}); err == nil {
		t.Fatalf("scanSettings(invalid json) error = nil")
	}
	if _, err := scanContact(errScanRow{err: pgx.ErrNoRows}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("scanContact(no rows) error = %v", err)
	}
	if _, err := scanDelivery(scanRow{
		int64(42), "tenant-1", uuid.New(), (*uuid.UUID)(nil), (*uuid.UUID)(nil),
		(*uuid.UUID)(nil), ChannelEmail, "sha256:abc", []byte("{"),
		[]byte("secret"), DeliveryStatusFailed, 2, "transient", 503,
		"temporary", "", "trace-1", time.Now(), (*time.Time)(nil), (*time.Time)(nil),
		(*time.Time)(nil), "", 0,
	}); err == nil {
		t.Fatalf("scanDelivery(invalid json) error = nil")
	}
	if _, err := scanEvent(scanRow{
		uuid.New(), "tenant-1", (*uuid.UUID)(nil), (*uuid.UUID)(nil), (*uuid.UUID)(nil),
		EventTypeShipped, "public_broadcast", "dedupe", "", "",
		"user", "user-1", EventStatusPending, 1, []byte("{"), time.Now(),
	}); err == nil {
		t.Fatalf("scanEvent(invalid json) error = nil")
	}
}

func TestSmallHelperMappings(t *testing.T) {
	if got := normalizeStatuses([]string{" failed ", "", "dead"}); len(got) != 2 || got[0] != "failed" || got[1] != "dead" {
		t.Fatalf("normalizeStatuses() = %#v", got)
	}
	if nullInt(0) != nil || nullInt(503) != 503 {
		t.Fatalf("nullInt() did not map zero/non-zero as expected")
	}
	if nullableBytes(nil) != nil || nullableBytes([]byte("x")) == nil {
		t.Fatalf("nullableBytes() did not map empty/non-empty as expected")
	}
	if defaultSignatureVersion("") != "v1-content-sha256" || defaultSignatureVersion("v2") != "v2" {
		t.Fatalf("defaultSignatureVersion() mismatch")
	}
	if actorID(" ") != "system" || actorID(" user-1 ") != "user-1" ||
		actorType("") != "system" || actorType(" user ") != "user" {
		t.Fatalf("actor helpers did not normalize")
	}
	if contentHash("a", "b") == contentHash("ab") {
		t.Fatalf("contentHash should delimit parts")
	}
}

func TestScanRowCollections(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	eventID := uuid.New()
	updateID := uuid.New()
	requestID := uuid.New()

	targets, err := scanWebhookTargets(ptrext.Of(scanRows{rows: []scanRow{{
		id, "tenant-1", "CRM", []byte("url"), "hooks.example.test", []byte("secret"),
		"v1", []byte(`{"request.shipped":true}`), true, "active",
		ptrext.Of(now), ptrext.Of(now), "user-1", now, now,
	}}}))
	if err != nil || len(targets) != 1 {
		t.Fatalf("scanWebhookTargets() = %+v, %v", targets, err)
	}

	deliveries, err := scanDeliveries(ptrext.Of(scanRows{rows: []scanRow{{
		int64(42), "tenant-1", eventID, (*uuid.UUID)(nil), ptrext.Of(id),
		(*uuid.UUID)(nil), ChannelEmail, "sha256:abc", []byte(`{"event_id":"event-1"}`),
		[]byte("secret"), DeliveryStatusFailed, 2, "transient", 503,
		"temporary", "", "trace-1", now, (*time.Time)(nil), ptrext.Of(now),
		ptrext.Of(now), "user-1", 1,
	}}}))
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("scanDeliveries() = %+v, %v", deliveries, err)
	}

	events, err := scanEvents(ptrext.Of(scanRows{rows: []scanRow{{
		eventID, "tenant-1", ptrext.Of(requestID), ptrext.Of(updateID), (*uuid.UUID)(nil),
		EventTypeShipped, "public_broadcast", "dedupe", "planned", "shipped",
		"user", "user-1", EventStatusPending, 1, []byte(`{"channels":["email"]}`), now,
	}}}))
	if err != nil || len(events) != 1 {
		t.Fatalf("scanEvents() = %+v, %v", events, err)
	}
}

func TestCreatePublicUpdateEventTxBuildsUpdateChain(t *testing.T) {
	ctx := context.Background()
	threadID := uuid.New()
	updateID := uuid.New()
	eventID := uuid.New()
	requestID := uuid.New()
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(eventTx{
		rows: []pgx.Row{
			scanRow{threadID},
			scanRow{updateID},
			scanRow{
				eventID, "tenant-1", ptrext.Of(requestID), ptrext.Of(updateID), (*uuid.UUID)(nil),
				EventTypeStatusChanged, "public_broadcast", "public-update:" + updateID.String(),
				"planned", "shipped", "system", "system", EventStatusPending, 0,
				[]byte(`{"channels":["email","webhook"]}`), now,
			},
		},
	})
	r := Repo{}
	event, err := r.CreatePublicUpdateEventTx(ctx, tx, PublicUpdateInput{
		TenantID:  "tenant-1",
		RequestID: requestID,
		Title:     "Shipped",
		Body:      "CSV export is live.",
		OldStatus: "planned",
		NewStatus: "shipped",
		Channels:  []string{ChannelEmail, ChannelWebhook},
		Notify:    true,
	})
	if err != nil {
		t.Fatalf("CreatePublicUpdateEventTx() error = %v", err)
	}
	if event.ID != eventID || event.EventType != EventTypeStatusChanged ||
		event.DedupeKey != "public-update:"+updateID.String() {
		t.Fatalf("event = %+v", event)
	}
	if tx.rowIdx != 3 || tx.execIdx != 1 {
		t.Fatalf("tx row/exec calls = %d/%d, want 3/1", tx.rowIdx, tx.execIdx)
	}
	if _, err := r.CreatePublicUpdateEventTx(ctx, ptrext.Of(eventTx{rows: []pgx.Row{errScanRow{err: errors.New("boom")}}}), PublicUpdateInput{}); err == nil {
		t.Fatalf("CreatePublicUpdateEventTx(query error) error = nil")
	}
}

func TestUnsubscribeTokenHelpersUseRequestAndTenantScopes(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	tokenID := uuid.New()
	contactID := uuid.New()
	requestID := uuid.New()
	subID := uuid.New()

	tx := ptrext.Of(eventTx{
		rows: []pgx.Row{
			scanRow{
				tokenID, "tenant-1", contactID, ptrext.Of(requestID),
				SubscriptionScopeRequest, ptrext.Of(now.Add(time.Hour)),
				(*time.Time)(nil), now,
			},
			scanRow{
				subID, "tenant-1",
				pgtype.UUID{Bytes: requestID, Valid: true},
				contactID, SubscriptionScopeRequest, SourceVoter,
				SubscriptionStatusActive, ptrext.Of(now), now, now,
			},
		},
	})
	token, err := lockUnsubscribeToken(ctx, tx, "tenant-1", "hash")
	if err != nil {
		t.Fatalf("lockUnsubscribeToken() error = %v", err)
	}
	if token.ID != tokenID || ptrext.Indirect(token.RequestID) != requestID {
		t.Fatalf("token = %+v", token)
	}
	if err := markUnsubscribeTokenUsed(ctx, tx, token.ID, "browser"); err != nil {
		t.Fatalf("markUnsubscribeTokenUsed() error = %v", err)
	}
	sub, err := unsubscribeSubscriptions(ctx, tx, token)
	if err != nil {
		t.Fatalf("unsubscribeSubscriptions(request) error = %v", err)
	}
	if sub.ID != subID || sub.RequestID != requestID || tx.execIdx != 1 {
		t.Fatalf("subscription = %+v execIdx=%d", sub, tx.execIdx)
	}

	tenantTx := ptrext.Of(eventTx{rows: []pgx.Row{scanRow{
		uuid.New(), "tenant-1",
		pgtype.UUID{},
		contactID,
		SubscriptionScopeTenantUpdates, SourceManual,
		"unsubscribed", ptrext.Of(now), now, now,
	}}})
	tenantSub, err := unsubscribeSubscriptions(ctx, tenantTx, UnsubscribeToken{
		ID:        uuid.New(),
		TenantID:  "tenant-1",
		ContactID: contactID,
		Scope:     SubscriptionScopeTenantUpdates,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("unsubscribeSubscriptions(tenant) error = %v", err)
	}
	if tenantSub.RequestID != uuid.Nil || tenantSub.Scope != SubscriptionScopeTenantUpdates {
		t.Fatalf("tenant subscription = %+v", tenantSub)
	}
}

func TestPreferenceTokenAndUnsubscribeValidationBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	contactID := uuid.New()
	requestID := uuid.New()
	tx := ptrext.Of(eventTx{rows: []pgx.Row{scanRow{
		uuid.New(), "tenant-1", contactID, (*uuid.UUID)(nil),
		SubscriptionScopeTenantUpdates, ptrext.Of(now.Add(time.Hour)),
		(*time.Time)(nil), now,
	}}})
	token, err := lockPreferenceToken(ctx, tx, "tenant-1", "hash")
	if err != nil {
		t.Fatalf("lockPreferenceToken() error = %v", err)
	}
	if token.ContactID != contactID || token.RequestID != nil {
		t.Fatalf("preference token = %+v", token)
	}
	if got := normalizeUnsubscribeScope(" "); got != SubscriptionScopeRequest {
		t.Fatalf("normalizeUnsubscribeScope(blank) = %q", got)
	}
	if got := normalizeUnsubscribeScope("custom"); got != "custom" {
		t.Fatalf("normalizeUnsubscribeScope(custom) = %q", got)
	}
	if err := validateUnsubscribeTokenShape(SubscriptionScopeRequest, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("validate request nil error = %v, want invalid input", err)
	}
	zero := uuid.Nil
	if err := validateUnsubscribeTokenShape(SubscriptionScopeRequest, ptrext.Of(zero)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("validate request zero error = %v, want invalid input", err)
	}
	if err := validateUnsubscribeTokenShape(SubscriptionScopeTenantUpdates, ptrext.Of(requestID)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("validate tenant with request error = %v, want invalid input", err)
	}
	if _, err := unsubscribeSubscriptions(ctx, ptrext.Of(eventTx{}), UnsubscribeToken{Scope: "custom"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsubscribeSubscriptions(custom) error = %v, want invalid input", err)
	}
	if _, err := lockUnsubscribeToken(ctx, ptrext.Of(eventTx{rows: []pgx.Row{errScanRow{err: pgx.ErrNoRows}}}), "tenant-1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lockUnsubscribeToken(no rows) error = %v, want not found", err)
	}
}

type scanRow []any

func (r scanRow) Scan(dest ...any) error {
	if len(dest) != len(r) {
		return ErrInvalidInput
	}
	for i, value := range r {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return ErrInvalidInput
		}
		elem := target.Elem()
		if value == nil {
			elem.Set(reflect.Zero(elem.Type()))
			continue
		}
		elem.Set(reflect.ValueOf(value))
	}
	return nil
}

type errScanRow struct {
	err error
}

func (r errScanRow) Scan(...any) error {
	return r.err
}

type scanRows struct {
	rows    []scanRow
	current scanRow
	idx     int
	err     error
}

func (r *scanRows) Close() {}

func (r *scanRows) Err() error { return r.err }

func (r *scanRows) CommandTag() pgconn.CommandTag { return pgconn.NewCommandTag("SELECT 1") }

func (r *scanRows) FieldDescriptions() []pgconn.FieldDescription { return nil }

func (r *scanRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.current = r.rows[r.idx]
	r.idx++
	return true
}

func (r *scanRows) Scan(dest ...any) error { return r.current.Scan(dest...) }

func (r *scanRows) Values() ([]any, error) { return r.current, nil }

func (r *scanRows) RawValues() [][]byte { return nil }

func (r *scanRows) Conn() *pgx.Conn { return nil }

type eventTx struct {
	rows    []pgx.Row
	rowIdx  int
	execIdx int
	execErr error
}

func (tx *eventTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }

func (tx *eventTx) Commit(context.Context) error { return nil }

func (tx *eventTx) Rollback(context.Context) error { return nil }

func (tx *eventTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (tx *eventTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

func (tx *eventTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }

func (tx *eventTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *eventTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.execIdx++
	if tx.execErr != nil {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *eventTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func (tx *eventTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.rowIdx >= len(tx.rows) {
		return errScanRow{err: errors.New("unexpected QueryRow")}
	}
	row := tx.rows[tx.rowIdx]
	tx.rowIdx++
	return row
}
func (tx *eventTx) Conn() *pgx.Conn { return nil }
