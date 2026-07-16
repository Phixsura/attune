// ptrext:file-allow request notification repo coverage tests use pgxpool config and pointer filters.
// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRepoDatabaseMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	id := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")
	requestID := uuid.MustParse("bbbbbbbb-1111-2222-3333-cccccccccccc")

	expectErr(t, "Begin", func() error {
		tx, err := r.Begin(ctx)
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
		return err
	})
	expectErr(t, "GetSettings", func() error {
		_, err := r.GetSettings(ctx, "tenant-1")
		return err
	})
	expectErr(t, "UpsertSettings", func() error {
		_, err := r.UpsertSettings(ctx, Settings{
			TenantID:                     "tenant-1",
			EmailEnabled:                 true,
			EnabledEventTypes:            map[string]any{EventTypeShipped: true},
			StatusPolicy:                 map[string]any{"shipped": true},
			DefaultConsentMode:           "explicit_opt_in",
			RequirePublicUpdateForStatus: true,
			MaxRecipientsWithoutConfirm:  100,
			TenantHourlySendLimit:        1000,
			ContactDailySendLimit:        10,
		})
		return err
	})
	expectErr(t, "UpsertContact", func() error {
		_, err := r.UpsertContact(ctx, Contact{
			TenantID:     "tenant-1",
			EmailHash:    EmailHash("jane@example.test"),
			EmailPayload: []byte("jane@example.test"),
			ConsentState: ConsentOptedIn,
		})
		return err
	})
	expectErr(t, "GetContact", func() error {
		_, err := r.GetContact(ctx, "tenant-1", id)
		return err
	})
	expectErr(t, "SuppressContact", func() error {
		_, err := r.SuppressContact(ctx, "tenant-1", id, "manual")
		return err
	})
	expectErr(t, "SuppressContactByEmailHash", func() error {
		_, err := r.SuppressContactByEmailHash(ctx, "tenant-1", EmailHash("jane@example.test"), "provider_bounce", "bounce")
		return err
	})
	expectErr(t, "UpsertRequestSubscription", func() error {
		_, err := r.UpsertRequestSubscription(ctx, Subscription{
			TenantID:  "tenant-1",
			RequestID: requestID,
			ContactID: id,
			Source:    SourceFollower,
			CreatedBy: "user-1",
		})
		return err
	})
	expectErr(t, "ListSubscribers", func() error {
		_, err := r.ListSubscribers(ctx, "tenant-1", requestID)
		return err
	})
	expectErr(t, "EligibleRequestRecipients", func() error {
		_, err := r.EligibleRequestRecipients(ctx, "tenant-1", requestID)
		return err
	})
}

func TestRepoDeliveryEventTargetAndSenderMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	id := uuid.MustParse("cccccccc-1111-2222-3333-dddddddddddd")
	requestID := uuid.MustParse("dddddddd-1111-2222-3333-eeeeeeeeeeee")

	expectErr(t, "InsertDelivery", func() error {
		_, err := r.InsertDelivery(ctx, DeliveryInput{
			TenantID:        "tenant-1",
			EventID:         id,
			Channel:         ChannelEmail,
			DestinationHash: DestinationHash("jane@example.test"),
			Payload:         map[string]any{"version": "1"},
		})
		return err
	})
	expectErr(t, "CountTenantEmailDeliveriesSince", func() error {
		_, err := r.CountTenantEmailDeliveriesSince(ctx, "tenant-1", time.Now().Add(-time.Hour))
		return err
	})
	expectErr(t, "CountContactEmailDeliveriesSince", func() error {
		_, err := r.CountContactEmailDeliveriesSince(ctx, "tenant-1", id, time.Now().Add(-24*time.Hour))
		return err
	})
	expectErr(t, "ClaimDeliveries", func() error {
		_, err := r.ClaimDeliveries(ctx, 10, "worker-1")
		return err
	})
	expectErr(t, "MarkDeliveryDelivered", func() error {
		_, err := r.MarkDeliveryDelivered(ctx, 1, "worker-1")
		return err
	})
	expectErr(t, "MarkDeliveryFailed", func() error {
		_, err := r.MarkDeliveryFailed(ctx, 1, "worker-1", "failed", "http_5xx", 500, time.Second)
		return err
	})
	expectErr(t, "MarkDeliveryDead", func() error {
		_, err := r.MarkDeliveryDead(ctx, 1, "worker-1", "dead", "terminal", 400)
		return err
	})
	expectErr(t, "RetryDelivery", func() error {
		_, err := r.RetryDelivery(ctx, "tenant-1", 1, "user-1")
		return err
	})
	expectErr(t, "ListDeliveries", func() error {
		_, err := r.ListDeliveries(ctx, ListDeliveryFilter{
			TenantID:  "tenant-1",
			RequestID: ptrext.Of(requestID),
			Statuses:  []string{DeliveryStatusFailed},
			Limit:     10,
		})
		return err
	})
	expectErr(t, "ResolvePublicRequest", func() error {
		_, err := r.ResolvePublicRequest(ctx, "acme", "csv-export")
		return err
	})
	expectErr(t, "ResolveTenantIDBySlug", func() error {
		_, err := r.ResolveTenantIDBySlug(ctx, "acme")
		return err
	})
	expectErr(t, "GetRequestSummary", func() error {
		_, err := r.GetRequestSummary(ctx, "tenant-1", requestID)
		return err
	})
	expectErr(t, "GetEventContext", func() error {
		_, err := r.GetEventContext(ctx, id)
		return err
	})
	expectErr(t, "ClaimEvents", func() error {
		_, err := r.ClaimEvents(ctx, 10, "worker-1")
		return err
	})
	expectErr(t, "MarkEventResolved", func() error {
		return r.MarkEventResolved(ctx, id, "worker-1", map[string]any{"email": 1})
	})
	expectErr(t, "MarkEventFailed", func() error {
		return r.MarkEventFailed(ctx, id, "worker-1", "failed", time.Second)
	})
}

func TestRepoConfigurationMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	id := uuid.MustParse("eeeeeeee-1111-2222-3333-ffffffffffff")

	expectErr(t, "ListWebhookTargets", func() error {
		_, err := r.ListWebhookTargets(ctx, "tenant-1")
		return err
	})
	expectErr(t, "ListActiveWebhookTargets", func() error {
		_, err := r.ListActiveWebhookTargets(ctx, "tenant-1")
		return err
	})
	expectErr(t, "GetWebhookTarget", func() error {
		_, err := r.GetWebhookTarget(ctx, "tenant-1", id)
		return err
	})
	expectErr(t, "CreateWebhookTarget", func() error {
		_, err := r.CreateWebhookTarget(ctx, WebhookTarget{
			TenantID:      "tenant-1",
			Name:          "CRM",
			URLPayload:    []byte("https://hooks.example.test/notify"),
			URLHost:       "hooks.example.test",
			SecretPayload: []byte("secret"),
			EventMask:     map[string]any{EventTypeShipped: true},
			CreatedBy:     "user-1",
		})
		return err
	})
	expectErr(t, "UpdateWebhookTarget", func() error {
		_, err := r.UpdateWebhookTarget(ctx, WebhookTarget{
			ID:         id,
			TenantID:   "tenant-1",
			Name:       "CRM",
			URLPayload: []byte("https://hooks.example.test/notify"),
			URLHost:    "hooks.example.test",
			EventMask:  map[string]any{EventTypeStatusChanged: true},
			Status:     "active",
		})
		return err
	})
	expectErr(t, "DeleteWebhookTarget", func() error {
		return r.DeleteWebhookTarget(ctx, "tenant-1", id)
	})
	expectErr(t, "MarkWebhookTargetTested", func() error {
		_, err := r.MarkWebhookTargetTested(ctx, "tenant-1", id, false)
		return err
	})
	expectErr(t, "UpsertSender", func() error {
		_, err := r.UpsertSender(ctx, Sender{
			TenantID:         "tenant-1",
			FromName:         "Attune",
			FromEmailHash:    EmailHash("notify@example.test"),
			FromEmailPayload: []byte("notify@example.test"),
			Domain:           "example.test",
			Provider:         "email",
			ProviderConfig:   []byte(`{"url":"https://mail.example.test/send"}`),
			CreatedBy:        "user-1",
		})
		return err
	})
	expectErr(t, "VerifySender", func() error {
		_, err := r.VerifySender(ctx, "tenant-1", id)
		return err
	})
	expectErr(t, "ActiveSender", func() error {
		_, err := r.ActiveSender(ctx, "tenant-1")
		return err
	})
	expectErr(t, "CreateUnsubscribeToken", func() error {
		return r.CreateUnsubscribeToken(ctx, "tenant-1", id, ptrext.Of(id), SubscriptionScopeRequest, "hash", time.Now().Add(time.Hour))
	})
	expectErr(t, "UseUnsubscribeToken", func() error {
		_, err := r.UseUnsubscribeToken(ctx, "tenant-1", "hash", "browser")
		return err
	})
	expectErr(t, "ConfirmContactToken", func() error {
		_, err := r.ConfirmContactToken(ctx, "tenant-1", "hash", "browser")
		return err
	})
}

func newUnreachableRepo(t *testing.T) *Repo {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 50 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return New(pool)
}

func expectErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
