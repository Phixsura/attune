// pgvector.go — pgvector extension availability and version check.
package database

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/logext"
)

// ErrPgvectorNotInstalled signals the pgvector extension is missing.
var ErrPgvectorNotInstalled = errors.New("pgvector extension not installed")

// ErrPgvectorVersionTooOld signals pgvector < 0.5.0 (no HNSW support).
var ErrPgvectorVersionTooOld = errors.New("pgvector version too old")

// CheckPgvector verifies pgvector is installed and >= 0.5.0 (required for HNSW).
// Returns nil if pgvector is not installed but clustering is not enabled for any tenant.
// Returns an error if clustering is enabled but pgvector is missing or too old.
func CheckPgvector(ctx context.Context, pool *pgxpool.Pool) error {
	const where = "database.CheckPgvector"

	var clusteringEnabled bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM tenants WHERE clustering_enabled = TRUE)
	`).Scan(&clusteringEnabled)
	if err != nil {
		return fmt.Errorf("check clustering enabled: %w", err)
	}

	var version string
	err = pool.QueryRow(ctx, `
		SELECT extversion FROM pg_extension WHERE extname = 'vector'
	`).Scan(&version)

	if errors.Is(err, pgx.ErrNoRows) {
		if clusteringEnabled {
			logext.Errorf(ctx, "[%s] pgvector not installed but clustering enabled", where)
			return fmt.Errorf("%w: run 'CREATE EXTENSION vector' or see https://github.com/pgvector/pgvector", ErrPgvectorNotInstalled)
		}
		logext.Infof(ctx, "[%s] pgvector not installed (clustering disabled, OK)", where)
		return nil
	}
	if err != nil {
		return fmt.Errorf("check pgvector: %w", err)
	}

	if err := checkPgvectorVersion(version); err != nil {
		logext.Errorf(ctx, "[%s] pgvector %s: %v", where, version, err)
		return err
	}

	logext.Infof(ctx, "[%s] pgvector %s OK", where, version)
	return nil
}

func checkPgvectorVersion(version string) error {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return nil
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return nil
	}
	if major == 0 && minor < 5 {
		return fmt.Errorf("%w: found %s, need >= 0.5.0 for HNSW indexes", ErrPgvectorVersionTooOld, version)
	}
	return nil
}
