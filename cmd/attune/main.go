// attune is the public-facing ingest service for AI-classified user
// feedback (operationally still deployed as "feedback-api"). Subcommands:
//
//	attune server # run the HTTP server (default)
//	attune keys issue --tenant <slug> # mint a new external API key
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
	"os"
	"time"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/observability"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/apikey"

	// #66 inbound adapters self-register via init(). Blank-import is the
	// only legal site per the inbound-boundary depguard rule; cmd/attune
	// owns this entrypoint so the framework registry is populated before
	// inbound.Manager.StartAll runs in server.go.
	_ "github.com/Phixsura/attune/internal/inbound/adapter/email"
	_ "github.com/Phixsura/attune/internal/inbound/adapter/webhook"
)

// subcommands routes each CLI verb to its handler. `server` ignores its args
// (it reads config + env); the rest receive args[1:]. A dispatch table keeps
// main() a thin router instead of a long switch.
var subcommands = map[string]func([]string) error{
	"server": func([]string) error { return runServer() },
	"keys":   runKeys,
	"tenant": runTenant,
	"eval":   runEval,
	"outbox": runOutbox,
	"digest": runDigest,
}

func main() {
	// Install the slog default (JSON in prod, Text under ENV=dev) wrapped in
	// TraceIDHandler so every log record carries trace_id/span_id. CLAUDE.md
	// §7 routes all log calls through internal/pkg/logext on top of this.
	// Rationale: docs/observability-trace-design.md.
	observability.InstallDefaultLogger()

	ctx := context.Background()
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"server"}
	}
	switch args[0] {
	case "-h", "--help", "help":
		printUsage()
		return
	}
	handler, ok := subcommands[args[0]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", args[0])
		printUsage()
		os.Exit(2)
	}
	if err := handler(args[1:]); err != nil {
		logext.Errorf(ctx, "%s subcommand failed,err:%+v", args[0], err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `attune

Usage:
 attune server Run the HTTP server (default)
 attune tenant create --slug <s> [--name <n>] Create a new tenant
 attune keys issue --tenant <slug> [--label <s>] Mint an API key
 attune eval --mode <m> [--tenant <slug>] ... AI accuracy report (--tenant required for export-for-human / score-human)
 attune outbox prune --older-than <dur> Mark stale pending rows dead
 attune digest run --tenant <slug> Send weekly digest now (smoke)
`)
}

// ── keys ──────────────────────────────────────────────────────────────────

func runKeys(args []string) error {
	if len(args) == 0 || args[0] != "issue" {
		return fmt.Errorf("usage: attune keys issue --tenant <slug> [--label <s>]")
	}
	fs := flag.NewFlagSet("keys issue", flag.ContinueOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug (required)")
	label := fs.String("label", "", "human-readable label for this key")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if ptrext.Indirect(tenantSlug) == "" {
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

	tenantID, err := tenant.NewTenant(pool).ResolveSlug(ctx, ptrext.Indirect(tenantSlug))
	if errors.Is(err, tenant.ErrTenantNotFound) {
		return fmt.Errorf("tenant slug %q not found or inactive", ptrext.Indirect(tenantSlug))
	}
	if err != nil {
		return err
	}

	svc := apikey.NewAPIKeys(apikeyrepo.NewAPIKey(pool))
	raw, keyID, err := svc.Issue(ctx, tenantID, ptrext.Indirect(label))
	if err != nil {
		return err
	}
	fmt.Printf(`API key issued for tenant %s (%s):

 key: %s
 label: %s
 id: %s

Store this key now — it is not recoverable.
`, ptrext.Indirect(tenantSlug), tenantID, raw, ptrext.Indirect(label), keyID)
	return nil
}
