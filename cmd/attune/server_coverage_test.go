// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestServeUntilStoppedReturnsListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer ln.Close()

	srv := ptrext.Of(http.Server{
		Addr:    ln.Addr().String(),
		Handler: http.NewServeMux(),
	})
	err = serveUntilStopped(context.Background(), srv, newDrainAwareReadiness(nil), 0, time.Second)
	if err == nil {
		t.Fatal("serveUntilStopped() error = nil, want listen error")
	}
	if !strings.Contains(err.Error(), "address already in use") && !strings.Contains(err.Error(), "bind") {
		t.Fatalf("serveUntilStopped() error = %v, want bind/listen failure", err)
	}
}

func TestServeUntilStoppedDrainsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := ptrext.Of(http.Server{
		Addr:    "127.0.0.1:0",
		Handler: http.NewServeMux(),
	})
	ready := newDrainAwareReadiness(ptrext.Of(fakeChecker{}))

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	if err := serveUntilStopped(ctx, srv, ready, 0, time.Second); err != nil {
		t.Fatalf("serveUntilStopped() error = %v, want nil", err)
	}
	if err := ready.Ping(context.Background()); !errors.Is(err, errServerDraining) {
		t.Fatalf("ready.Ping() error = %v, want errServerDraining", err)
	}
}

func TestRunQueueDepthRefresherAppliesInitialDepthAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var queries atomic.Int32
	var applied atomic.Bool

	runQueueDepthRefresher(
		ctx,
		"unit",
		func(context.Context) (map[string]int64, error) {
			queries.Add(1)
			cancel()
			return map[string]int64{"tenant-1": 3}, nil
		},
		func(depths map[string]int64) {
			if depths["tenant-1"] != 3 {
				t.Fatalf("depths[tenant-1] = %d, want 3", depths["tenant-1"])
			}
			applied.Store(true)
		},
	)

	if queries.Load() != 1 {
		t.Fatalf("queries = %d, want 1", queries.Load())
	}
	if !applied.Load() {
		t.Fatal("apply was not called")
	}
}

func TestRunQueueDepthRefresherSkipsApplyOnQueryError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var applied atomic.Bool

	runQueueDepthRefresher(
		ctx,
		"unit",
		func(context.Context) (map[string]int64, error) {
			cancel()
			return nil, errors.New("queue depth failed")
		},
		func(map[string]int64) {
			applied.Store(true)
		},
	)

	if applied.Load() {
		t.Fatal("apply should not be called after a query error")
	}
}

func TestSetupTracingNoopCanShutdown(t *testing.T) {
	shutdown, err := setupTracing(context.Background(), ptrext.Of(config.Config{}))
	if err != nil {
		t.Fatalf("setupTracing() error = %v, want nil", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("tracer shutdown error = %v, want nil", err)
	}
}

func TestShutdownTracingLogsShutdownError(t *testing.T) {
	shutdownTracing(func(context.Context) error {
		return errors.New("flush failed")
	})
}

func TestSetupDatabaseRejectsInvalidURL(t *testing.T) {
	_, err := setupDatabase(context.Background(), ptrext.Of(config.Config{DatabaseURL: "://invalid"}))
	if err == nil {
		t.Fatal("setupDatabase() error = nil, want invalid URL error")
	}
}

func TestBackgroundPrunersReturnForInvalidInputs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runAuditPruner(ctx, nil, nil, time.Hour, time.Hour)
	runIdempotencyKeyPruner(ctx, nil, nil, time.Hour, time.Hour)
	runMCPPruner(ctx, nil, time.Hour, time.Hour)
	runMCPPruner(ctx, newUnreachableServerPool(t), 0, time.Hour)
}

func TestBackgroundPrunersReturnOnAdvisoryLockErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pool := newUnreachableServerPool(t)

	runAuditPruner(ctx, pool, auditlogsvc.New(nil), time.Hour, time.Hour)
	runIdempotencyKeyPruner(ctx, pool, feedback.NewFeedback(pool), time.Hour, time.Hour)
	runMCPPruner(ctx, pool, time.Hour, time.Hour)
}

func TestPruneIdempotencyKeysOnceLogsRepoError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pruneIdempotencyKeysOnce(ctx, feedback.NewFeedback(newUnreachableServerPool(t)), time.Hour)
}

func TestPruneMCPOnceLogsRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pool := newUnreachableServerPool(t)

	pruneMCPOnce(ctx, mcprepo.NewCodes(pool), mcprepo.NewTokens(pool), mcprepo.NewSessions(pool), time.Hour)
}

func TestRetryDatabasePingImmediateAndSkippedRetryLogBranches(t *testing.T) {
	errBoom := errors.New("postgres down")
	if err := retryDatabasePing(context.Background(), 0, time.Second, func(context.Context) error {
		return errBoom
	}); !errors.Is(err, errBoom) {
		t.Fatalf("retryDatabasePing() error = %v, want %v", err, errBoom)
	}
	logDatabasePingRetry(context.Background(), 2, time.Second, time.Second, errBoom)
}

func newUnreachableServerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
