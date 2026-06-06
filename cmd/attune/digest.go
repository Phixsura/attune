package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/outbox"
)

// runDigest dispatches `attune digest <subcmd>`.
//
//	attune digest run --tenant <slug>   Send one digest right now to
//	                                     the tenant's first lark-bot.
//	                                     Used for prod smoke + manual
//	                                     catch-up when scheduler missed.
//
// Bypasses the cutoff (last_digest_sent_at < cutoff) — always sends.
// Updates last_digest_sent_at on success exactly like the scheduler,
// so it counts as the week's official send.
func runDigest(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: attune digest run --tenant <slug>")
	}
	switch args[0] {
	case "run":
		return runDigestSend(args[1:])
	default:
		return fmt.Errorf("unknown digest subcommand %q (try: run)", args[0])
	}
}

func runDigestSend(args []string) error {
	fs := flag.NewFlagSet("digest run", flag.ContinueOnError)
	slug := fs.String("tenant", "", "tenant slug to send digest to (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slug == "" {
		return errors.New("--tenant is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()

	tenants := tenant.NewTenant(pool)
	tenantID, err := tenants.ResolveSlug(ctx, *slug)
	if err != nil {
		return fmt.Errorf("resolve tenant: %w", err)
	}
	t, err := tenants.GetByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("get tenant: %w", err)
	}

	svc := outbox.NewDigestService(
		tenants,
		feedback.NewFeedback(pool),
		notifytarget.NewNotifyTarget(pool),
	)
	if err := svc.SendForTenant(ctx, tenantID, t.Slug, t.Name); err != nil {
		return err
	}
	fmt.Printf("digest dispatched (tenant=%s name=%q)\n", t.Slug, t.Name)
	return nil
}
