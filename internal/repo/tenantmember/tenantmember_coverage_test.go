// SPDX-License-Identifier: Apache-2.0

package tenantmember

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	member := Member{
		TenantID:   "tenant-1",
		MemberType: "invite",
		UserID:     "invite-1",
		Role:       domain.RoleViewer,
		RoleSource: "manual",
		InvitedAt:  time.Now(),
	}

	expectRepoErr(t, "GetByUser", func() error {
		_, err := r.GetByUser(ctx, "tenant-1", "", "admin-1")
		return err
	})
	expectRepoErr(t, "GetByID", func() error {
		_, err := r.GetByID(ctx, "member-1")
		return err
	})
	expectRepoErr(t, "GetRole", func() error {
		_, err := r.GetRole(ctx, "tenant-1", "", "admin-1")
		return err
	})
	expectRepoErr(t, "List", func() error {
		_, err := r.List(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "CountAdmins", func() error {
		_, err := r.CountAdmins(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "Create", func() error {
		_, err := r.Create(ctx, member)
		return err
	})
	expectRepoErr(t, "UpdateRole", func() error {
		return r.UpdateRole(ctx, "member-1", domain.RoleMember, "admin-1")
	})
	expectRepoErr(t, "Remove", func() error {
		return r.Remove(ctx, "member-1")
	})
}

func TestRepoEnsureAndInviteMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)

	expectRepoErr(t, "EnsureOIDCMember", func() error {
		_, err := r.EnsureOIDCMember(ctx, "tenant-1", "user-1", domain.RoleMember)
		return err
	})
	expectRepoErr(t, "EnsureAdminMember", func() error {
		_, err := r.EnsureAdminMember(ctx, "tenant-1", "admin-1")
		return err
	})
	expectRepoErr(t, "ExistsByEmail", func() error {
		_, err := r.ExistsByEmail(ctx, "tenant-1", "admin@example.test")
		return err
	})
	expectRepoErr(t, "GetPendingInviteByEmail", func() error {
		_, err := r.GetPendingInviteByEmail(ctx, "tenant-1", "invite@example.test")
		return err
	})
	expectRepoErr(t, "AcceptInvite", func() error {
		_, err := r.AcceptInvite(ctx, "invite-1", "oidc_user", "user-1")
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
