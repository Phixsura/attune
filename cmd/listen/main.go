// listen is the public-facing ingest service for AI-classified user
// feedback (operationally still deployed as "feedback-api"). Subcommands:
//
//	listen server                     # run the HTTP server (default)
//	listen keys issue --tenant <slug> # mint a new external API key
//
// main.go is the CLI entrypoint: it installs the slog handler and dispatches
// subcommands. The `server` bootstrap lives in server.go; tenant/eval/outbox/
// digest live in their own sibling files (all package main).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/wanmuchengchuan/listen/internal/infra/config"
	"github.com/wanmuchengchuan/listen/internal/infra/database"
	"github.com/wanmuchengchuan/listen/internal/repo"
	"github.com/wanmuchengchuan/listen/internal/service"
	"github.com/wanmuchengchuan/listen/internal/observability"
)

func main() {
	// slog handler:默认 JSON(prod 安全默认,SLS 字段索引必需)。
	// 本地开发显式设 ENV=dev 才用 text(docker logs 可读)。
	// 详见 docs/observability-trace-design.md
	var inner slog.Handler
	if os.Getenv("ENV") == "dev" {
		inner = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		inner = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	slog.SetDefault(slog.New(observability.NewTraceIDHandler(inner)))

	ctx := context.Background()
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"server"}
	}
	switch args[0] {
	case "server":
		if err := runServer(); err != nil {
			slog.ErrorContext(ctx, "server exited", "err", err)
			os.Exit(1)
		}
	case "keys":
		if err := runKeys(args[1:]); err != nil {
			slog.ErrorContext(ctx, "keys subcommand failed", "err", err)
			os.Exit(1)
		}
	case "tenant":
		if err := runTenant(args[1:]); err != nil {
			slog.ErrorContext(ctx, "tenant subcommand failed", "err", err)
			os.Exit(1)
		}
	case "eval":
		if err := runEval(args[1:]); err != nil {
			slog.ErrorContext(ctx, "eval subcommand failed", "err", err)
			os.Exit(1)
		}
	case "outbox":
		if err := runOutbox(args[1:]); err != nil {
			slog.ErrorContext(ctx, "outbox subcommand failed", "err", err)
			os.Exit(1)
		}
	case "digest":
		if err := runDigest(args[1:]); err != nil {
			slog.ErrorContext(ctx, "digest subcommand failed", "err", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", args[0])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `listen

Usage:
  listen server                                       Run the HTTP server (default)
  listen tenant create --slug <s> [--name <n>]        Create a new tenant
  listen keys issue --tenant <slug> [--label <s>]     Mint an API key
  listen eval --mode <m> [--since <date>] ...         AI accuracy report
  listen outbox prune --older-than <dur>              Mark stale pending rows dead
  listen digest run --tenant <slug>                   Send weekly digest now (smoke)
`)
}

// ── keys ──────────────────────────────────────────────────────────────────

func runKeys(args []string) error {
	if len(args) == 0 || args[0] != "issue" {
		return fmt.Errorf("usage: listen keys issue --tenant <slug> [--label <s>]")
	}
	fs := flag.NewFlagSet("keys issue", flag.ContinueOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug (required)")
	label := fs.String("label", "", "human-readable label for this key")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *tenantSlug == "" {
		return fmt.Errorf("--tenant is required")
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

	tenantID, err := repo.NewTenant(pool).ResolveSlug(ctx, *tenantSlug)
	if errors.Is(err, repo.ErrTenantNotFound) {
		return fmt.Errorf("tenant slug %q not found or inactive", *tenantSlug)
	}
	if err != nil {
		return err
	}

	svc := service.NewAPIKeys(repo.NewAPIKey(pool))
	raw, keyID, err := svc.Issue(ctx, tenantID, *label)
	if err != nil {
		return err
	}
	fmt.Printf(`API key issued for tenant %s (%s):

  key:    %s
  label:  %s
  id:     %s

Store this key now — it is not recoverable.
`, *tenantSlug, tenantID, raw, *label, keyID)
	return nil
}
