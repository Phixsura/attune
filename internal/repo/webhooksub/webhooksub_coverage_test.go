package webhooksub

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Unit tier: every repo method surfaces pool errors instead of swallowing
// them (mirrors notifytarget's coverage pattern; happy paths live in
// test/integration/postgres/webhooksub).
func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	id := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")
	sub := Subscription{
		TenantID:   "tenant-1",
		TargetURL:  "https://hooks.example.test/zap",
		Secret:     "0123456789abcdef",
		EventTypes: []string{"feedback.created"},
	}

	expectRepoErr(t, "Insert", func() error {
		_, err := r.Insert(ctx, sub)
		return err
	})
	expectRepoErr(t, "GetByID", func() error {
		_, err := r.GetByID(ctx, "tenant-1", id)
		return err
	})
	expectRepoErr(t, "GetByIDAny", func() error {
		_, err := r.GetByIDAny(ctx, id)
		return err
	})
	expectRepoErr(t, "ListByTenant", func() error {
		_, err := r.ListByTenant(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "ListActiveByTenantEvent", func() error {
		_, err := r.ListActiveByTenantEvent(ctx, "tenant-1", "feedback.created")
		return err
	})
	expectRepoErr(t, "Delete", func() error {
		_, err := r.Delete(ctx, "tenant-1", id)
		return err
	})
	expectRepoErr(t, "Disable", func() error {
		return r.Disable(ctx, id, ReasonGone)
	})
	expectRepoErr(t, "CountByTenant", func() error {
		_, err := r.CountByTenant(ctx, "tenant-1")
		return err
	})
}

func TestInsertDefaultsConsumer(t *testing.T) {
	t.Parallel()
	// The consumer default is applied Go-side before the INSERT; verify the
	// constant wiring without a database.
	if ConsumerGeneric != "generic" || ConsumerZapier != "zapier" {
		t.Fatal("consumer constants must match the chk_webhook_sub_consumer CHECK")
	}
	if StatusActive != "active" || StatusDisabled != "disabled" {
		t.Fatal("status constants must match the chk_webhook_sub_status CHECK")
	}
	if ReasonGone != "gone" {
		t.Fatal("ReasonGone drives the 410 contract; renaming it breaks ops queries")
	}
}

func newUnreachableRepo(t *testing.T) *Repo {
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

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Errorf("%s: want error from unreachable pool, got nil", name)
	}
}
