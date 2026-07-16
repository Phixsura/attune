// SPDX-License-Identifier: Apache-2.0

package workflowstate

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
	r := newUnreachableWorkflowStateRepo(t)
	state := WorkflowState{
		ID:          "aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb",
		TenantID:    "tenant-1",
		Name:        "triage",
		DisplayName: domain.I18nString{"en": "Triage"},
		Color:       "#3366ff",
		Category:    "active",
		Position:    10,
		IsDefault:   true,
	}

	expectWorkflowStateErr(t, "List", func() error {
		_, err := r.List(ctx, "tenant-1", false)
		return err
	})
	expectWorkflowStateErr(t, "List include archived", func() error {
		_, err := r.List(ctx, "tenant-1", true)
		return err
	})
	expectWorkflowStateErr(t, "Get", func() error {
		_, err := r.Get(ctx, state.ID)
		return err
	})
	expectWorkflowStateErr(t, "GetByTenantAndID", func() error {
		_, err := r.GetByTenantAndID(ctx, "tenant-1", state.ID)
		return err
	})
	expectWorkflowStateErr(t, "Create", func() error {
		_, err := r.Create(ctx, state)
		return err
	})
	expectWorkflowStateErr(t, "Update", func() error {
		_, err := r.Update(ctx, state)
		return err
	})
	expectWorkflowStateErr(t, "Archive", func() error {
		return r.Archive(ctx, "tenant-1", state.ID)
	})
	expectWorkflowStateErr(t, "DeleteTransitionsForState", func() error {
		return r.DeleteTransitionsForState(ctx, "tenant-1", state.ID)
	})
	expectWorkflowStateErr(t, "CountActiveFeedback", func() error {
		_, err := r.CountActiveFeedback(ctx, "tenant-1", state.ID)
		return err
	})
	expectWorkflowStateErr(t, "CountActiveDefaults", func() error {
		_, err := r.CountActiveDefaults(ctx, "tenant-1")
		return err
	})
	expectWorkflowStateErr(t, "CheckTransition", func() error {
		_, err := r.CheckTransition(ctx, "tenant-1", state.ID, "cccccccc-1111-2222-3333-dddddddddddd")
		return err
	})
	expectWorkflowStateErr(t, "ArchiveWithTransitions", func() error {
		return r.ArchiveWithTransitions(ctx, "tenant-1", state.ID)
	})
	expectWorkflowStateErr(t, "AllowedNext", func() error {
		_, err := r.AllowedNext(ctx, "tenant-1", state.ID)
		return err
	})
	expectWorkflowStateErr(t, "ListTransitions", func() error {
		_, err := r.ListTransitions(ctx, "tenant-1")
		return err
	})
	expectWorkflowStateErr(t, "ReplaceTransitions", func() error {
		_, err := r.ReplaceTransitions(ctx, "tenant-1", []TransitionEdge{{
			FromStateID: state.ID,
			ToStateID:   "cccccccc-1111-2222-3333-dddddddddddd",
		}})
		return err
	})
}

func newUnreachableWorkflowStateRepo(t *testing.T) *Repo {
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
	return New(pool)
}

func expectWorkflowStateErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
