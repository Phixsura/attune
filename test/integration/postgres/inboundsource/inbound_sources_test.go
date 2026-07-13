//go:build integration

package inboundsource_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_InboundSourceRepoListLookupAndState(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := insertTenant(t, ctx, pool)
	webhookID := insertSource(t, ctx, pool, sourceRow{
		tenantID: tenantID,
		channel:  "webhook",
		name:     "Webhook A",
		slug:     "webhook-a",
		enabled:  true,
	})
	insertSource(t, ctx, pool, sourceRow{
		tenantID: tenantID,
		channel:  "webhook",
		name:     "Webhook Paused",
		slug:     "webhook-paused",
		enabled:  false,
	})
	insertSource(t, ctx, pool, sourceRow{
		tenantID: tenantID,
		channel:  "email",
		name:     "Email A",
		slug:     "email-a",
		enabled:  true,
	})
	repo := inboundsource.NewRepo(pool)
	assertListAndLookup(t, ctx, repo, webhookID)
	assertStateAndEnabled(t, ctx, repo, webhookID)
}

func TestPG_InboundSourceRepoUpdateConfig(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := insertTenant(t, ctx, pool)
	sourceID := insertSource(t, ctx, pool, sourceRow{
		tenantID: tenantID,
		channel:  "slack",
		name:     "Slack Feed",
		slug:     "slack-feed",
		enabled:  true,
	})

	repo := inboundsource.NewRepo(pool)
	want := []byte(`{"thread_cache":[{"root_ts":"1700000000.000100"}]}`)
	if err := repo.UpdateConfig(ctx, sourceID, want); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	got, err := repo.Get(ctx, sourceID)
	if err != nil {
		t.Fatalf("Get after UpdateConfig: %v", err)
	}
	if string(got.Config) != string(want) {
		t.Fatalf("Config = %s, want %s", got.Config, want)
	}
}

func assertListAndLookup(t *testing.T, ctx context.Context, repo *inboundsource.Repo, webhookID string) {
	t.Helper()
	webhooks, err := repo.List(ctx, "webhook")
	if err != nil {
		t.Fatalf("List webhook: %v", err)
	}
	if len(webhooks) != 1 || webhooks[0].ID != webhookID {
		t.Fatalf("enabled webhook list mismatch: %#v", webhooks)
	}

	bySlug, err := repo.GetBySlugs(ctx, "inbound-io", "webhook", "webhook-a")
	if err != nil {
		t.Fatalf("GetBySlugs: %v", err)
	}
	if bySlug.ID != webhookID || string(bySlug.Config) != "{}" {
		t.Fatalf("GetBySlugs row mismatch: %#v", bySlug)
	}
	if _, err := repo.GetBySlugs(ctx, "inbound-io", "webhook", "missing"); !errors.Is(err, inboundsource.ErrNotFound) {
		t.Fatalf("GetBySlugs missing: got %v want %v", err, inboundsource.ErrNotFound)
	}
}

func assertStateAndEnabled(t *testing.T, ctx context.Context, repo *inboundsource.Repo, webhookID string) {
	t.Helper()
	lastEventAt := time.Now().UTC().Truncate(time.Microsecond)
	state := inbound.SourceState{LastEventAt: ptrext.Of(lastEventAt), LastUID: 42, LastError: "boom"}
	if err := repo.UpdateState(ctx, webhookID, state); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	got, err := repo.Get(ctx, webhookID)
	if err != nil {
		t.Fatalf("Get after UpdateState: %v", err)
	}
	if got.State.LastUID != 42 || got.State.LastError != "boom" || got.State.LastEventAt == nil {
		t.Fatalf("state not stored: %#v", got.State)
	}

	if err := repo.SetEnabled(ctx, webhookID, false, "paused"); err != nil {
		t.Fatalf("SetEnabled false: %v", err)
	}
	disabled, _ := repo.Get(ctx, webhookID)
	if disabled.Enabled || disabled.State.LastError != "paused" {
		t.Fatalf("disabled state: enabled=%v err=%q", disabled.Enabled, disabled.State.LastError)
	}
	if err := repo.SetEnabled(ctx, webhookID, true, ""); err != nil {
		t.Fatalf("SetEnabled true: %v", err)
	}
	enabled, _ := repo.Get(ctx, webhookID)
	if !enabled.Enabled || enabled.State.LastError != "" {
		t.Fatalf("enabled state: enabled=%v err=%q", enabled.Enabled, enabled.State.LastError)
	}
}

type sourceRow struct {
	tenantID string
	channel  string
	name     string
	slug     string
	enabled  bool
}

func insertTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var tenantID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name)
		VALUES ('inbound-io', 'Inbound IO')
		RETURNING id`,
	).Scan(&tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return tenantID
}

func insertSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, row sourceRow) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(ctx, `
		INSERT INTO inbound_sources (id, tenant_id, channel, name, slug, config, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, row.tenantID, row.channel, row.name, row.slug, []byte("{}"), row.enabled,
	)
	if err != nil {
		t.Fatalf("insert source %s/%s: %v", row.channel, row.slug, err)
	}
	return id
}
