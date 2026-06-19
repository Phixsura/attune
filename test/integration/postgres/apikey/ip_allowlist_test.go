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

// A key with an IP allowlist must fail CLOSED: an in-range IP is accepted, an
// out-of-range IP is rejected, and crucially an EMPTY/unresolvable client IP is
// rejected too (not silently bypassed).
func TestPG_APIKeyIPAllowlistFailsClosed(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "ip-allowlist", "IP Allowlist")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	svc := serviceapikey.NewAPIKeys(apikey.NewAPIKey(pool))

	raw, _, err := svc.IssueFullWithScopes(ctx, serviceapikey.IssueParams{
		TenantID:     tenantID,
		Label:        "cidr-restricted",
		AllowedCIDRs: []string{"10.0.0.0/8"},
	}, domain.AllScopes)
	if err != nil {
		t.Fatalf("IssueFullWithScopes: %v", err)
	}

	cases := []struct {
		name     string
		clientIP string
		wantErr  bool
	}{
		{"in-range IP accepted", "10.1.2.3", false},
		{"out-of-range IP rejected", "203.0.113.9", true},
		{"empty IP rejected (fail closed)", "", true},
		{"unparseable IP rejected", "not-an-ip", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, _, _, err := svc.LookupWithScopesAndIP(ctx, raw, c.clientIP)
			if c.wantErr {
				if !errors.Is(err, domain.ErrIPNotAllowed) {
					t.Fatalf("clientIP=%q: err = %v, want ErrIPNotAllowed", c.clientIP, err)
				}
			} else if err != nil {
				t.Fatalf("clientIP=%q: unexpected err = %v", c.clientIP, err)
			}
		})
	}
}
