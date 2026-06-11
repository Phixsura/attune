// SPDX-License-Identifier: Apache-2.0

// Package secretlock coordinates DB writes that persist encrypted runtime
// secrets with DB-wide key rotation.
package secretlock

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sqlLock = `SELECT pg_advisory_xact_lock(hashtext('attune.secret_key_registry'))`

type Tx = pgx.Tx

var ErrWritableKeyUnavailable = errors.New("secretlock: writable secret key unavailable")

func LockTx(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, sqlLock); err != nil {
		return fmt.Errorf("lock secret key registry: %w", err)
	}
	return nil
}

func WithTx(ctx context.Context, pool *pgxpool.Pool, commit bool, fn func(context.Context, Tx) error) error {
	if pool == nil {
		return errors.New("secretlock: pool not configured")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin secret lock tx: %w", err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := LockTx(ctx, tx); err != nil {
		return err
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if !commit {
		finished = true
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			return fmt.Errorf("rollback secret lock dry run: %w", err)
		}
		return nil
	}
	finished = true
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit secret lock tx: %w", err)
	}
	return nil
}

func EnsureWritableKey(ctx context.Context, tx pgx.Tx, keyID string) error {
	if keyID == "" {
		return nil
	}
	var status string
	var retired bool
	err := tx.QueryRow(ctx, `
		SELECT status, retired_at IS NOT NULL
		  FROM secret_key_registry
		 WHERE key_id = $1
		 FOR UPDATE`,
		keyID,
	).Scan(&status, &retired)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: key_id=%s is not registered", ErrWritableKeyUnavailable, keyID)
	}
	if err != nil {
		return fmt.Errorf("read writable secret key: %w", err)
	}
	if retired || status != "ENABLED" {
		return fmt.Errorf("%w: key_id=%s status=%s retired=%t", ErrWritableKeyUnavailable, keyID, status, retired)
	}
	return nil
}
