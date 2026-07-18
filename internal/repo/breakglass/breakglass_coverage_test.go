// SPDX-License-Identifier: Apache-2.0

package breakglass

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	expiresAt := time.Now().Add(time.Hour)

	expectRepoErr(t, "Issue", func() error {
		_, err := r.Issue(ctx, NewToken{
			TenantID:   "tenant-1",
			AdminEmail: "admin@example.test",
			TokenHash:  "hash",
			ExpiresAt:  expiresAt,
			IssuedBy:   "admin-1",
			AllowedIPs: []string{"127.0.0.1/32"},
		})
		return err
	})
	expectRepoErr(t, "GetByID", func() error {
		_, err := r.GetByID(ctx, "tenant-1", "token-1")
		return err
	})
	expectRepoErr(t, "ListValidForAdmin", func() error {
		_, err := r.ListValidForAdmin(ctx, "tenant-1", "admin@example.test")
		return err
	})
	expectRepoErr(t, "ListAll", func() error {
		_, err := r.ListAll(ctx, "tenant-1", 0)
		return err
	})
	expectRepoErr(t, "MarkUsed", func() error {
		return r.MarkUsed(ctx, "tenant-1", "token-1", "127.0.0.1")
	})
	expectRepoErr(t, "Revoke", func() error {
		return r.Revoke(ctx, "tenant-1", "token-1", "admin-1")
	})
	expectRepoErr(t, "RevokeAll", func() error {
		_, err := r.RevokeAll(ctx, "tenant-1", "admin-1")
		return err
	})
	expectRepoErr(t, "CountValid", func() error {
		_, err := r.CountValid(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "Cleanup", func() error {
		_, err := r.Cleanup(ctx, expiresAt)
		return err
	})
	expectRepoErr(t, "Approve", func() error {
		return r.Approve(ctx, "tenant-1", "token-1", "admin-2")
	})
	expectRepoErr(t, "ListPendingApproval", func() error {
		_, err := r.ListPendingApproval(ctx, "tenant-1")
		return err
	})
}

func TestRecoveryCodeRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := NewRecoveryCodeRepo(newUnreachableRepo(t).pool)

	expectRepoErr(t, "RecoveryCodeRepo.Create", func() error {
		_, err := r.Create(ctx, NewRecoveryCode{
			TenantID:  "tenant-1",
			CodeHash:  "hash",
			Label:     "Code 1",
			CreatedBy: "admin-1",
		})
		return err
	})
	expectRepoErr(t, "RecoveryCodeRepo.ListValid", func() error {
		_, err := r.ListValid(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "RecoveryCodeRepo.ListAll", func() error {
		_, err := r.ListAll(ctx, "tenant-1", 0)
		return err
	})
	expectRepoErr(t, "RecoveryCodeRepo.MarkUsed", func() error {
		return r.MarkUsed(ctx, "tenant-1", "code-1", "127.0.0.1")
	})
	expectRepoErr(t, "RecoveryCodeRepo.Revoke", func() error {
		return r.Revoke(ctx, "tenant-1", "code-1", "admin-1")
	})
	expectRepoErr(t, "RecoveryCodeRepo.RevokeAll", func() error {
		_, err := r.RevokeAll(ctx, "tenant-1", "admin-1")
		return err
	})
	expectRepoErr(t, "RecoveryCodeRepo.CountValid", func() error {
		_, err := r.CountValid(ctx, "tenant-1")
		return err
	})
}

func TestExpiryWarningRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := NewExpiryWarningRepo(newUnreachableRepo(t).pool)

	expectRepoErr(t, "ExpiryWarningRepo.HasSent", func() error {
		_, err := r.HasSent(ctx, "token-1", ExpiryWarning24h)
		return err
	})
	expectRepoErr(t, "ExpiryWarningRepo.MarkSent", func() error {
		return r.MarkSent(ctx, "token-1", ExpiryWarning1h)
	})
	expectRepoErr(t, "ExpiryWarningRepo.Cleanup", func() error {
		_, err := r.Cleanup(ctx)
		return err
	})
}

func newUnreachableRepo(t *testing.T) *Repo {
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
	return NewRepo(pool)
}

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
