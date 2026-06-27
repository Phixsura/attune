// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow flag-variable dereferences in CLI commands

package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/repo/breakglass"
	"github.com/Phixsura/attune/internal/repo/tenant"
	breakglasssvc "github.com/Phixsura/attune/internal/service/breakglass"
)

func runAuth(args []string) error {
	if len(args) == 0 {
		return authUsage()
	}
	switch args[0] {
	case "breakglass":
		return runBreakglass(args[1:])
	default:
		return authUsage()
	}
}

func authUsage() error {
	return fmt.Errorf("usage: attune auth breakglass {issue|list|revoke}")
}

func runBreakglass(args []string) error {
	if len(args) == 0 {
		return breakglassUsage()
	}
	switch args[0] {
	case "issue":
		return runBreakglassIssue(args[1:])
	case "list":
		return runBreakglassList(args[1:])
	case "revoke":
		return runBreakglassRevoke(args[1:])
	default:
		return breakglassUsage()
	}
}

func breakglassUsage() error {
	return fmt.Errorf(`usage: attune auth breakglass <command>

Commands:
  issue   Issue a new break-glass token
  list    List all break-glass tokens
  revoke  Revoke a break-glass token

Examples:
  attune auth breakglass issue --admin admin@example.com --ttl 30m
  attune auth breakglass list --tenant acme
  attune auth breakglass revoke --id <token-id> --tenant acme`)
}

func runBreakglassIssue(args []string) error {
	fs := flag.NewFlagSet("auth breakglass issue", flag.ContinueOnError)
	adminEmail := fs.String("admin", "", "Admin email for the token (required)")
	ttlStr := fs.String("ttl", "30m", "Token TTL (e.g., 30m, 1h, 24h)")
	tenantSlug := fs.String("tenant", "", "Tenant slug (uses first tenant if not specified)")
	issuedBy := fs.String("issued-by", "cli", "Who is issuing this token")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *adminEmail == "" {
		return fmt.Errorf("--admin is required")
	}

	ttl, err := time.ParseDuration(*ttlStr)
	if err != nil {
		return fmt.Errorf("invalid TTL: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tenantRepo := tenant.NewTenant(pool)
	bgRepo := breakglass.NewRepo(pool)
	svc := breakglasssvc.NewService(bgRepo, breakglasssvc.DefaultConfig())

	tenantID, err := resolveTenantID(ctx, tenantRepo, *tenantSlug)
	if err != nil {
		return err
	}

	result, err := svc.Issue(ctx, breakglasssvc.IssueInput{
		TenantID:   tenantID,
		AdminEmail: *adminEmail,
		TTL:        ttl,
		IssuedBy:   *issuedBy,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Break-glass token issued for %s\n", *adminEmail)
	fmt.Printf("Expires: %s\n", result.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("Token: %s (SAVE THIS - shown only once)\n", result.RawToken)
	fmt.Printf("Token ID: %s\n", result.Token.ID)
	return nil
}

func runBreakglassList(args []string) error {
	fs := flag.NewFlagSet("auth breakglass list", flag.ContinueOnError)
	tenantSlug := fs.String("tenant", "", "Tenant slug (uses first tenant if not specified)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tenantRepo := tenant.NewTenant(pool)
	bgRepo := breakglass.NewRepo(pool)
	svc := breakglasssvc.NewService(bgRepo, breakglasssvc.DefaultConfig())

	tenantID, err := resolveTenantID(ctx, tenantRepo, *tenantSlug)
	if err != nil {
		return err
	}

	tokens, err := svc.List(ctx, tenantID)
	if err != nil {
		return err
	}

	if len(tokens) == 0 {
		fmt.Println("No break-glass tokens found.")
		return nil
	}

	now := time.Now()
	fmt.Printf("%-36s  %-30s  %-20s  %-10s  %-20s\n", "ID", "Admin", "Expires", "Status", "Issued By")
	fmt.Printf("%s\n", "----------------------------------------------------------------------------------------------------------------------------")
	for _, t := range tokens {
		status := "valid"
		if t.IsUsed() {
			status = "used"
		} else if t.IsRevoked() {
			status = "revoked"
		} else if t.IsExpired(now) {
			status = "expired"
		}
		fmt.Printf("%-36s  %-30s  %-20s  %-10s  %-20s\n",
			t.ID,
			truncate(t.AdminEmail, 30),
			t.ExpiresAt.Format(time.RFC3339),
			status,
			truncate(t.IssuedBy, 20),
		)
	}
	return nil
}

func runBreakglassRevoke(args []string) error {
	fs := flag.NewFlagSet("auth breakglass revoke", flag.ContinueOnError)
	tokenID := fs.String("id", "", "Token ID to revoke (required)")
	tenantSlug := fs.String("tenant", "", "Tenant slug (uses first tenant if not specified)")
	revokedBy := fs.String("revoked-by", "cli", "Who is revoking this token")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tokenID == "" {
		return fmt.Errorf("--id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tenantRepo := tenant.NewTenant(pool)
	bgRepo := breakglass.NewRepo(pool)
	svc := breakglasssvc.NewService(bgRepo, breakglasssvc.DefaultConfig())

	tenantID, err := resolveTenantID(ctx, tenantRepo, *tenantSlug)
	if err != nil {
		return err
	}

	if err := svc.Revoke(ctx, tenantID, *tokenID, *revokedBy); err != nil {
		return err
	}

	fmt.Printf("Token %s revoked.\n", *tokenID)
	return nil
}

func resolveTenantID(ctx context.Context, repo *tenant.TenantRepo, slug string) (string, error) {
	if slug != "" {
		id, err := repo.ResolveSlug(ctx, slug)
		if err != nil {
			return "", fmt.Errorf("tenant %q not found: %w", slug, err)
		}
		return id, nil
	}
	id, err := repo.FirstActiveID(ctx)
	if err != nil {
		return "", fmt.Errorf("no tenant found: %w", err)
	}
	return id, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
