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

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/logext"
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
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		logext.Errorf(ctx, "[%s] new pool failed,err:%+v", where, err.Error())
		return nil, err
	}
	return pool, nil
}
