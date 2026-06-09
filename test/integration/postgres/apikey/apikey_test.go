//go:build integration

package apikey_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/tenant"
	serviceapikey "github.com/Phixsura/attune/internal/service/apikey"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_APIKeyIssueLookupRevoke(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "api-key-io", "API Key IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	svc := serviceapikey.NewAPIKeys(apikey.NewAPIKey(pool))

	raw, keyID, err := svc.Issue(ctx, tenantID, "smoke")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	gotTenant, gotKey, err := svc.Lookup(ctx, raw)
	if err != nil {
		t.Fatalf("Lookup issued key: %v", err)
	}
	if gotTenant != tenantID || gotKey != keyID {
		t.Fatalf("lookup mismatch: tenant=%q key=%s", gotTenant, gotKey)
	}

	rows, err := svc.List(ctx, tenantID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != keyID || !rows[0].IsActive {
		t.Fatalf("unexpected key list: %#v", rows)
	}

	if err := svc.Revoke(ctx, tenantID, keyID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, _, err := svc.Lookup(ctx, raw); !errors.Is(err, domain.ErrInvalidAPIKey) {
		t.Fatalf("Lookup revoked key: got %v want %v", err, domain.ErrInvalidAPIKey)
	}
}
