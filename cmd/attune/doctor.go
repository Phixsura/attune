package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/preflight"
	"github.com/Phixsura/attune/internal/preflight/checks"
)

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	format := fs.String("format", "text", "Output format: text or json")
	warnExit := fs.Bool("warn-exit", false, "Exit code 1 on any warning (default: only on failure)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()

	// Load config (tolerate failure — the config:parse check reports it).
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		logext.Warnf(ctx, "[doctor] config load failed (checks will report),err:%s", cfgErr.Error())
	}

	// Open DB pool (tolerate failure — connectivity check reports it).
	pool := openDoctorPool(ctx, cfg)

	// Inject migration count so the migration check knows the expected total.
	checks.SetMigrationTotal(database.MigrationCount())

	env := ptrext.Of(preflight.Environment{
		Cfg:  cfg,
		Pool: pool,
	})

	report := preflight.RunAll(ctx, env)

	if pool != nil {
		pool.Close()
	}

	switch ptrext.Indirect(format) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
	default:
		preflight.FormatText(os.Stdout, report)
	}

	if report.Status == preflight.StatusFail {
		os.Exit(1)
	}
	if ptrext.Indirect(warnExit) && report.Status == preflight.StatusWarn {
		os.Exit(1)
	}
	return nil
}

func openDoctorPool(ctx context.Context, cfg *config.Config) *pgxpool.Pool {
	if cfg == nil {
		return nil
	}
	url := cfg.DatabaseURL
	if url == "" {
		return nil
	}
	poolCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := database.NewPool(poolCtx, url)
	if err != nil {
		logext.Warnf(ctx, "[doctor] db pool open failed (checks will report),err:%s", err.Error())
		return nil
	}
	return pool
}
