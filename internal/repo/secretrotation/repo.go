// SPDX-License-Identifier: Apache-2.0

// Package secretrotation owns SQL for DB-wide runtime-secret rotation.
package secretrotation

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

var ErrSecretKeyNotFound = errors.New("secretrotation: secret key registry row not found")

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

type LLMCredentialRow struct {
	ID         uuid.UUID
	KeyID      string
	Ciphertext []byte
}

type InboundConfigRow struct {
	ID      uuid.UUID
	Channel string
	Config  []byte
}

type TxQueries struct {
	tx pgx.Tx
}

type Queries interface {
	CheckKeyExists(ctx context.Context, keyID string) error
	ListLLMCredentialsForUpdate(ctx context.Context) ([]LLMCredentialRow, error)
	UpdateLLMCredential(ctx context.Context, id uuid.UUID, keyID string, ciphertext []byte) error
	ListInboundConfigsForUpdate(ctx context.Context) ([]InboundConfigRow, error)
	UpdateInboundConfig(ctx context.Context, id uuid.UUID, config []byte) error
	RetireKey(ctx context.Context, keyID string) error
}

func (r *Repo) WithLockedSecrets(
	ctx context.Context,
	commit bool,
	fn func(context.Context, Queries) error,
) error {
	return secretlock.WithTx(ctx, r.pool, commit, func(ctx context.Context, tx secretlock.Tx) error {
		return fn(ctx, ptrext.Of(TxQueries{tx: tx}))
	})
}

func (q *TxQueries) CheckKeyExists(ctx context.Context, keyID string) error {
	var exists int
	err := q.tx.QueryRow(ctx,
		`SELECT 1 FROM secret_key_registry WHERE key_id = $1 FOR UPDATE`,
		keyID,
	).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSecretKeyNotFound
	}
	if err != nil {
		return fmt.Errorf("check secret key: %w", err)
	}
	return nil
}

func (q *TxQueries) ListLLMCredentialsForUpdate(ctx context.Context) ([]LLMCredentialRow, error) {
	rows, err := q.tx.Query(ctx, `
		SELECT id, credential_key_id, credential_ciphertext
		  FROM llm_channels
		 WHERE credential_ciphertext IS NOT NULL
		   AND octet_length(credential_ciphertext) > 0
		 FOR UPDATE`)
	if err != nil {
		return nil, fmt.Errorf("list llm credentials for rotation: %w", err)
	}
	defer rows.Close()
	var out []LLMCredentialRow
	for rows.Next() {
		var row LLMCredentialRow
		if err := rows.Scan(&row.ID, &row.KeyID, &row.Ciphertext); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (q *TxQueries) UpdateLLMCredential(
	ctx context.Context,
	id uuid.UUID,
	keyID string,
	ciphertext []byte,
) error {
	tag, err := q.tx.Exec(ctx, `
		UPDATE llm_channels
		   SET credential_key_id = $2,
		       credential_ciphertext = $3,
		       updated_at = NOW()
		 WHERE id = $1`,
		id, keyID, ciphertext,
	)
	return checkOne(tag, err, "update llm credential")
}

func (q *TxQueries) ListInboundConfigsForUpdate(ctx context.Context) ([]InboundConfigRow, error) {
	rows, err := q.tx.Query(ctx, `
		SELECT id, channel, config
		  FROM inbound_sources
		 WHERE config IS NOT NULL
		   AND octet_length(config) > 0
		 FOR UPDATE`)
	if err != nil {
		return nil, fmt.Errorf("list inbound configs for rotation: %w", err)
	}
	defer rows.Close()
	var out []InboundConfigRow
	for rows.Next() {
		var row InboundConfigRow
		if err := rows.Scan(&row.ID, &row.Channel, &row.Config); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (q *TxQueries) UpdateInboundConfig(ctx context.Context, id uuid.UUID, config []byte) error {
	tag, err := q.tx.Exec(ctx, `
		UPDATE inbound_sources
		   SET config = $2,
		       updated_at = NOW()
		 WHERE id = $1`,
		id, config,
	)
	return checkOne(tag, err, "update inbound source config")
}

func (q *TxQueries) RetireKey(ctx context.Context, keyID string) error {
	tag, err := q.tx.Exec(ctx, `
		UPDATE secret_key_registry
		   SET status = 'DISABLED',
		       retired_at = NOW(),
		       primary_key = FALSE,
		       last_seen_at = NOW()
		 WHERE key_id = $1`,
		keyID,
	)
	if err := checkOne(tag, err, "retire secret key"); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSecretKeyNotFound
		}
		return err
	}
	return nil
}

func checkOne(tag pgconn.CommandTag, err error, op string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
