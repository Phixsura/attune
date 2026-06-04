package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/Phixsura/listen/internal/infra/config"
	"github.com/Phixsura/listen/internal/infra/database"
	"github.com/Phixsura/listen/internal/repo"
)

// runTenant dispatches `listen tenant <verb>` subcommands. Wave 1.2
// only exposes `create`; Wave 2 control plane will add `list / activate /
// deactivate / set-lark-key` once the OAuth flow lands.
func runTenant(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: listen tenant create --slug <s> [--name <n>]")
	}
	switch args[0] {
	case "create":
		return runTenantCreate(args[1:])
	default:
		return fmt.Errorf("unknown tenant subcommand %q (have: create)", args[0])
	}
}

// runTenantCreate is the explicit "seed a new tenant" path for ops.
// Picked over an SQL seed file so the operation goes through repo's
// validation + slug uniqueness check, and shows up clearly in audit
// logs as a deliberate action.
func runTenantCreate(args []string) error {
	fs := flag.NewFlagSet("tenant create", flag.ContinueOnError)
	slug := fs.String("slug", "", "business-readable slug (required, unique)")
	name := fs.String("name", "", "display name (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slug == "" {
		return fmt.Errorf("--slug is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()

	tenants := repo.NewTenant(pool)
	id, err := tenants.Create(ctx, *slug, *name)
	if errors.Is(err, repo.ErrTenantSlugTaken) {
		return err
	}
	if err != nil {
		return err
	}
	fmt.Printf(`Tenant created:

  slug: %s
  name: %s
  id:   %s

Next:
  listen keys issue --tenant %s [--label <label>]
`, *slug, *name, id, *slug)
	return nil
}
