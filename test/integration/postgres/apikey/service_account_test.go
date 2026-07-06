//go:build integration

package apikey_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/tenant"
	serviceapikey "github.com/Phixsura/attune/internal/service/apikey"
	"github.com/Phixsura/attune/internal/testdb"
)

type serviceAccountToggleScenario struct {
	pool             *pgxpool.Pool
	tenantID         string
	svc              *serviceapikey.APIKeys
	raw              string
	keyID            uuid.UUID
	serviceAccountID uuid.UUID
}

func newServiceAccountToggleScenario(t *testing.T) serviceAccountToggleScenario {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "service-account-toggle", "Service Account Toggle")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	svc := serviceapikey.NewAPIKeys(apikey.NewAPIKey(pool))

	raw, keyID, err := svc.Issue(ctx, tenantID, "ci-key")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	serviceAccountID, err := svc.CreateServiceAccount(ctx, tenantID, "ci-bot", "CI/CD automation")
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	if err := svc.LinkKeyToServiceAccount(ctx, tenantID, keyID, serviceAccountID); err != nil {
		t.Fatalf("LinkKeyToServiceAccount: %v", err)
	}

	return serviceAccountToggleScenario{
		pool:             pool,
		tenantID:         tenantID,
		svc:              svc,
		raw:              raw,
		keyID:            keyID,
		serviceAccountID: serviceAccountID,
	}
}

func TestPG_ServiceAccountDisableBlocksLinkedKeys(t *testing.T) {
	scenario := newServiceAccountToggleScenario(t)

	disabled, err := scenario.svc.UpdateServiceAccount(context.Background(), scenario.tenantID, scenario.serviceAccountID, false)
	if err != nil {
		t.Fatalf("UpdateServiceAccount disable: %v", err)
	}
	if disabled.IsActive {
		t.Fatalf("UpdateServiceAccount disable returned active row: %#v", disabled)
	}

	if gotTenant, gotKey, err := scenario.svc.Lookup(context.Background(), scenario.raw); !errors.Is(err, domain.ErrInvalidAPIKey) {
		t.Fatalf("Lookup disabled linked key: tenant=%q key=%s err=%v, want %v", gotTenant, gotKey, err, domain.ErrInvalidAPIKey)
	}
}

func TestPG_ServiceAccountEnableRestoresLinkedKeys(t *testing.T) {
	scenario := newServiceAccountToggleScenario(t)
	ctx := context.Background()

	if _, err := scenario.svc.UpdateServiceAccount(ctx, scenario.tenantID, scenario.serviceAccountID, false); err != nil {
		t.Fatalf("UpdateServiceAccount disable: %v", err)
	}
	enabled, err := scenario.svc.UpdateServiceAccount(ctx, scenario.tenantID, scenario.serviceAccountID, true)
	if err != nil {
		t.Fatalf("UpdateServiceAccount enable: %v", err)
	}
	if !enabled.IsActive {
		t.Fatalf("UpdateServiceAccount enable returned inactive row: %#v", enabled)
	}

	if gotTenant, gotKey, err := scenario.svc.Lookup(ctx, scenario.raw); err != nil || gotTenant != scenario.tenantID || gotKey != scenario.keyID {
		t.Fatalf("Lookup linked key after enable: tenant=%q key=%s err=%v", gotTenant, gotKey, err)
	}
}

func TestPG_ServiceAccountDeleteDetachesLinkedKeys(t *testing.T) {
	scenario := newServiceAccountToggleScenario(t)
	ctx := context.Background()

	deleted, err := scenario.svc.DeleteServiceAccount(ctx, scenario.tenantID, scenario.serviceAccountID)
	if err != nil {
		t.Fatalf("DeleteServiceAccount: %v", err)
	}
	if deleted.ID != scenario.serviceAccountID {
		t.Fatalf("DeleteServiceAccount returned wrong row: got=%s want=%s", deleted.ID, scenario.serviceAccountID)
	}

	if gotTenant, gotKey, err := scenario.svc.Lookup(ctx, scenario.raw); err != nil || gotTenant != scenario.tenantID || gotKey != scenario.keyID {
		t.Fatalf("Lookup linked key after delete: tenant=%q key=%s err=%v", gotTenant, gotKey, err)
	}
}

func TestPG_ServiceAccountToggleAfterDeleteReturnsNotFound(t *testing.T) {
	scenario := newServiceAccountToggleScenario(t)
	ctx := context.Background()

	if _, err := scenario.svc.DeleteServiceAccount(ctx, scenario.tenantID, scenario.serviceAccountID); err != nil {
		t.Fatalf("DeleteServiceAccount: %v", err)
	}
	if _, err := scenario.svc.UpdateServiceAccount(ctx, scenario.tenantID, scenario.serviceAccountID, false); !errors.Is(err, apikey.ErrServiceAccountNotFound) {
		t.Fatalf("UpdateServiceAccount after delete: got %v want %v", err, apikey.ErrServiceAccountNotFound)
	}
}

func TestPG_ServiceAccountLinkRejectsCrossTenantBinding(t *testing.T) {
	scenario := newServiceAccountToggleScenario(t)
	ctx := context.Background()

	otherTenantID, err := tenant.NewTenant(scenario.pool).Create(ctx, "service-account-toggle-cross", "Service Account Toggle Cross")
	if err != nil {
		t.Fatalf("create other tenant: %v", err)
	}
	otherServiceAccountID, err := scenario.svc.CreateServiceAccount(ctx, otherTenantID, "ops-bot", "cross-tenant automation")
	if err != nil {
		t.Fatalf("CreateServiceAccount other tenant: %v", err)
	}

	if err := scenario.svc.LinkKeyToServiceAccount(ctx, scenario.tenantID, scenario.keyID, otherServiceAccountID); !errors.Is(err, apikey.ErrServiceAccountNotFound) {
		t.Fatalf("LinkKeyToServiceAccount cross-tenant: got %v want %v", err, apikey.ErrServiceAccountNotFound)
	}

	var linkedServiceAccountID uuid.UUID
	if err := scenario.pool.QueryRow(
		ctx,
		`SELECT service_account_id FROM external_api_keys WHERE id = $1`,
		scenario.keyID,
	).Scan(&linkedServiceAccountID); err != nil {
		t.Fatalf("read service_account_id: %v", err)
	}
	if linkedServiceAccountID != scenario.serviceAccountID {
		t.Fatalf("LinkKeyToServiceAccount changed binding: got=%s want=%s", linkedServiceAccountID, scenario.serviceAccountID)
	}
}

func TestPG_ServiceAccountLookupRejectsCrossTenantBinding(t *testing.T) {
	scenario := newServiceAccountToggleScenario(t)
	ctx := context.Background()

	otherTenantID, err := tenant.NewTenant(scenario.pool).Create(ctx, "service-account-lookup-cross", "Service Account Lookup Cross")
	if err != nil {
		t.Fatalf("create other tenant: %v", err)
	}
	otherServiceAccountID, err := scenario.svc.CreateServiceAccount(ctx, otherTenantID, "ops-bot", "cross-tenant automation")
	if err != nil {
		t.Fatalf("CreateServiceAccount other tenant: %v", err)
	}

	if _, err := scenario.pool.Exec(
		ctx,
		`UPDATE external_api_keys SET service_account_id = $1 WHERE id = $2`,
		otherServiceAccountID,
		scenario.keyID,
	); err != nil {
		t.Fatalf("seed cross-tenant binding: %v", err)
	}

	if gotTenant, gotKey, err := scenario.svc.Lookup(ctx, scenario.raw); !errors.Is(err, domain.ErrInvalidAPIKey) {
		t.Fatalf("Lookup cross-tenant binding: tenant=%q key=%s err=%v, want %v", gotTenant, gotKey, err, domain.ErrInvalidAPIKey)
	}
}
