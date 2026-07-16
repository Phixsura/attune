// SPDX-License-Identifier: Apache-2.0

package authmode

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/preflight"
	"github.com/Phixsura/attune/internal/repo/systemsettings"
)

func TestServiceMethodsReturnSettingsErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	svc := NewService(
		newUnreachableSettingsRepo(t),
		ptrext.Of(mockPreflightRunner{}),
		ptrext.Of(preflight.Environment{}),
		func(context.Context, string) (int, error) { return 1, nil },
	)

	if _, err := svc.GetMode(ctx, "tenant-1"); err == nil {
		t.Fatalf("GetMode() error = nil, want settings error")
	}
	if _, err := svc.Cutover(ctx, CutoverInput{TenantID: "tenant-1", UpdatedBy: "admin-1"}); err == nil {
		t.Fatalf("Cutover() error = nil, want settings error")
	}
	if err := svc.Fallback(ctx, "tenant-1", "admin-1"); err == nil {
		t.Fatalf("Fallback() error = nil, want settings error")
	}
	allowed, err := svc.IsLocalLoginAllowed(ctx, "tenant-1")
	if err == nil || !allowed {
		t.Fatalf("IsLocalLoginAllowed() = (%t, %v), want true with settings error", allowed, err)
	}
}

func TestServiceCheckBreakglassReady(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("nil counter skips", func(t *testing.T) {
		t.Parallel()
		got := NewService(nil, nil, nil, nil).checkBreakglassReady(ctx, "tenant-1")
		if got.Status != preflight.StatusSkipped {
			t.Fatalf("Status = %s, want skipped", got.Status)
		}
	})

	t.Run("counter error fails", func(t *testing.T) {
		t.Parallel()
		svc := NewService(nil, nil, nil, func(context.Context, string) (int, error) {
			return 0, errors.New("counter failed")
		})
		got := svc.checkBreakglassReady(ctx, "tenant-1")
		if got.Status != preflight.StatusFail || !strings.Contains(got.Message, "counter failed") {
			t.Fatalf("result = %#v, want failed counter message", got)
		}
	})

	t.Run("zero tokens fails", func(t *testing.T) {
		t.Parallel()
		svc := NewService(nil, nil, nil, func(context.Context, string) (int, error) {
			return 0, nil
		})
		got := svc.checkBreakglassReady(ctx, "tenant-1")
		if got.Status != preflight.StatusFail || got.Remediation == "" {
			t.Fatalf("result = %#v, want failed result with remediation", got)
		}
	})

	t.Run("positive tokens pass", func(t *testing.T) {
		t.Parallel()
		svc := NewService(nil, nil, nil, func(context.Context, string) (int, error) {
			return 2, nil
		})
		got := svc.checkBreakglassReady(ctx, "tenant-1")
		if got.Status != preflight.StatusPass || !strings.Contains(got.Message, "2 valid") {
			t.Fatalf("result = %#v, want pass message", got)
		}
	})
}

func newUnreachableSettingsRepo(t *testing.T) *systemsettings.Repo {
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
	return systemsettings.NewRepo(pool)
}
