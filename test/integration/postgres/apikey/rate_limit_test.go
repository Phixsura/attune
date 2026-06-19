//go:build integration

package apikey_test

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/tenant"
	serviceapikey "github.com/Phixsura/attune/internal/service/apikey"
	"github.com/Phixsura/attune/internal/testdb"
)

// A key's rate_limit_rpm must round-trip DB -> service so the api-key middleware
// can populate AuthCtx.RateLimitRPM for the per-key limiter. (The limiter's
// enforcement is unit-tested in internal/infra/ratelimit.)
func TestPG_APIKeyRateLimitRPMSurfaced(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "rpm", "RPM")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	svc := serviceapikey.NewAPIKeys(apikey.NewAPIKey(pool))

	// Key WITH a per-key rpm.
	rawLimited, _, err := svc.IssueFullWithScopes(ctx, serviceapikey.IssueParams{
		TenantID: tenantID, Label: "limited", RateLimitRPM: ptrext.Of(42),
	}, domain.AllScopes)
	if err != nil {
		t.Fatalf("issue limited: %v", err)
	}
	if _, _, _, rpm, err := svc.LookupWithScopesAndIP(ctx, rawLimited, ""); err != nil || rpm == nil || *rpm != 42 {
		t.Fatalf("limited key: rpm=%v err=%v, want 42", rpm, err)
	}

	// Key WITHOUT an rpm → nil (no per-key limit).
	rawPlain, _, err := svc.Issue(ctx, tenantID, "plain")
	if err != nil {
		t.Fatalf("issue plain: %v", err)
	}
	if _, _, _, rpm, err := svc.LookupWithScopesAndIP(ctx, rawPlain, ""); err != nil || rpm != nil {
		t.Fatalf("plain key: rpm=%v err=%v, want nil", rpm, err)
	}
}
