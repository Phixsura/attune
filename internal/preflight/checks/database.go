package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/preflight"
)

func init() {
	preflight.Register(preflight.Check{
		Name:     "database:connectivity",
		Category: preflight.CategoryDatabase,
		Run:      checkDBConnectivity,
	})
	preflight.Register(preflight.Check{
		Name:     "database:pgvector",
		Category: preflight.CategoryDatabase,
		Run:      checkPgvector,
	})
}

func checkDBConnectivity(ctx context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "database:connectivity",
		Category: preflight.CategoryDatabase,
	}
	if env.Pool == nil {
		r.Status = preflight.StatusFail
		r.Message = "Database pool not available"
		r.Remediation = "Check database.url in config. Ensure PostgreSQL is running and reachable."
		return r
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := env.Pool.Ping(pingCtx); err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Database ping failed"
		r.Remediation = "Check database.url in config. Ensure PostgreSQL is running and reachable."
		return r
	}
	r.Status = preflight.StatusPass
	r.Message = fmt.Sprintf("Connected (%s)", time.Since(start).Truncate(time.Millisecond))
	return r
}

func checkPgvector(ctx context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "database:pgvector",
		Category: preflight.CategoryDatabase,
	}
	if env.Pool == nil {
		r.Status = preflight.StatusFail
		r.Message = "Database pool not available"
		r.Remediation = "Fix database connectivity first."
		return r
	}
	if err := database.CheckPgvector(ctx, env.Pool); err != nil {
		r.Status = preflight.StatusFail
		r.Message = "pgvector check failed"
		r.Remediation = "Install the pgvector extension: CREATE EXTENSION IF NOT EXISTS vector; Requires pgvector >= 0.5.0 for HNSW index support."
		return r
	}
	r.Status = preflight.StatusPass
	r.Message = "pgvector available"
	return r
}
