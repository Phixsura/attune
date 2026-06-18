// pool.go — pgxpool helper that wires in the OTel pgx tracer for
// every connection.
//
// Use it instead of pgxpool.New(...):
//
//	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
//
// Each SQL query then opens a child span — db.statement, db.operation,
// and duration land in the trace backend so the DB shows up as a real
// downstream node in service topology views.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/logext"
)

// NewPool constructs a pgxpool with the otelpgx tracer attached.
// Used by every attune subcommand and the long-running server.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	const where = "database.NewPool"
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		logext.Errorf(ctx, "[%s] parse url failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()

	// Hardening (#64): bound the pool and fail slow connects/queries so a single
	// stuck query can't pin a connection or run unbounded past the HTTP timeout.
	// URL params (pool_max_conns / connect_timeout / statement_timeout) win — these
	// are only applied when the caller didn't already set them.
	if cfg.MaxConns == 0 {
		cfg.MaxConns = 20
	}
	if cfg.ConnConfig.ConnectTimeout == 0 {
		cfg.ConnConfig.ConnectTimeout = 10 * time.Second
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	if _, ok := cfg.ConnConfig.RuntimeParams["statement_timeout"]; !ok {
		// 30s default. Migrations today run well under this; a future long
		// backfill should `SET LOCAL statement_timeout = 0` within its own tx.
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		logext.Errorf(ctx, "[%s] new pool failed,err:%+v", where, err.Error())
		return nil, err
	}
	return pool, nil
}
