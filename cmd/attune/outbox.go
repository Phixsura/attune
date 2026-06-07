package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

// runOutbox dispatches `attune outbox <subcmd> [flags]`.
//
//	attune outbox prune --older-than 1h Mark every pending/failed row
//	 older than 1h as 'dead'.
//	 Reads DB URL from FEEDBACK_API_*
//	 env vars / config.yaml exactly
//	 like `server`.
//
// Designed for one-shot cleanup after the pgx-encode-bug era left
// orphan rows in pending state (commit 2054e71 fixed the bug; legacy
// rows need this command to clear). Idempotent — runs to completion
// on an empty backlog.
func runOutbox(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: attune outbox prune --older-than <duration>")
	}
	switch args[0] {
	case "prune":
		return runOutboxPrune(args[1:])
	default:
		return fmt.Errorf("unknown outbox subcommand %q (try: prune)", args[0])
	}
}

func runOutboxPrune(args []string) error {
	fs := flag.NewFlagSet("outbox prune", flag.ContinueOnError)
	olderThan := fs.Duration("older-than", time.Hour,
		"only prune rows created before NOW - <duration> (e.g. 1h, 24h)")
	reason := fs.String("reason", "stale: pre-fix pgx encode bug era",
		"dead_reason text written for traceability")
	if err := fs.Parse(args); err != nil {
		return err
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

	before := time.Now().Add(-ptrext.Indirect(olderThan))
	n, err := outboxrepo.NewOutbox(pool).PruneStalePending(ctx, before, ptrext.Indirect(reason))
	if err != nil {
		return err
	}
	fmt.Printf("pruned %d outbox rows (older than %s, reason=%q)\n", n, ptrext.Indirect(olderThan), ptrext.Indirect(reason))
	return nil
}
