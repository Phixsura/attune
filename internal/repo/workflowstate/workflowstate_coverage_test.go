// SPDX-License-Identifier: Apache-2.0

package workflowstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func TestRepoTxHelpersReturnWrappedErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	r := New(nil)
	tx := failingWorkflowTx{err: errors.New("tx failed")}
	state := WorkflowState{
		TenantID:    "tenant-1",
		Name:        "triage",
		DisplayName: domain.I18nString{"en": "Triage"},
		Color:       "#3366ff",
		Category:    "active",
	}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "CheckTransitionTx", call: func() error {
			_, err := r.CheckTransitionTx(ctx, tx, "tenant-1", "aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb", "cccccccc-1111-2222-3333-dddddddddddd")
			return err
		}},
		{name: "UpsertStateReturningID", call: func() error {
			_, err := r.UpsertStateReturningID(ctx, tx, state)
			return err
		}},
		{name: "InsertTransitionIgnoreConflict", call: func() error {
			return r.InsertTransitionIgnoreConflict(ctx, tx, "tenant-1", "aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb", "cccccccc-1111-2222-3333-dddddddddddd")
		}},
		{name: "GetCurrentStateForUpdate", call: func() error {
			_, err := r.GetCurrentStateForUpdate(ctx, tx, "tenant-1", 42)
			return err
		}},
		{name: "SetFeedbackState", call: func() error {
			return r.SetFeedbackState(ctx, tx, 42, "aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")
		}},
	} {
		expectWorkflowStateErr(t, tc.name, tc.call)
	}
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

type failingWorkflowTx struct {
	err error
}

func (f failingWorkflowTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, f.err
}

func (f failingWorkflowTx) Commit(context.Context) error {
	return f.err
}

func (f failingWorkflowTx) Rollback(context.Context) error {
	return nil
}

func (f failingWorkflowTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, f.err
}

func (f failingWorkflowTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (f failingWorkflowTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (f failingWorkflowTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, f.err
}

func (f failingWorkflowTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.err
}

func (f failingWorkflowTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, f.err
}

func (f failingWorkflowTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return failingWorkflowRow(f)
}

func (f failingWorkflowTx) Conn() *pgx.Conn {
	return nil
}

type failingWorkflowRow struct {
	err error
}

func (r failingWorkflowRow) Scan(...any) error {
	return r.err
}

func expectWorkflowStateErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
