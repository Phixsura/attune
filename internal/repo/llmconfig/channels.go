// SPDX-License-Identifier: Apache-2.0

package llmconfig

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

func (r *Repo) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, protocol, base_url, auth_mode, credential_key_id,
		       credential_ciphertext, status, priority, weight, timeout_seconds,
		       created_at, updated_at, last_tested_at, last_test_status, last_error
		  FROM llm_channels
		 ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list llm channels: %w", err)
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		row, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repo) GetChannel(ctx context.Context, id uuid.UUID) (*Channel, error) {
	row, err := scanChannel(r.pool.QueryRow(ctx, `
		SELECT id, name, protocol, base_url, auth_mode, credential_key_id,
		       credential_ciphertext, status, priority, weight, timeout_seconds,
		       created_at, updated_at, last_tested_at, last_test_status, last_error
		  FROM llm_channels
		 WHERE id = $1`, id))
	if err := mapNoRows(err, ErrChannelNotFound, "get llm channel"); err != nil {
		return nil, err
	}
	return ptrext.Of(row), nil
}

func (r *Repo) CreateChannel(ctx context.Context, ch Channel) (*Channel, error) {
	if ch.ID == uuid.Nil {
		ch.ID = uuid.New()
	}
	var row Channel
	err := secretlock.WithTx(ctx, r.pool, true, func(ctx context.Context, tx secretlock.Tx) error {
		if err := secretlock.EnsureWritableKey(ctx, tx, ch.CredentialKeyID); err != nil {
			return err
		}
		var scanErr error
		row, scanErr = scanChannel(tx.QueryRow(ctx, `
			INSERT INTO llm_channels
			 (id, name, protocol, base_url, auth_mode, credential_key_id, credential_ciphertext,
			  status, priority, weight, timeout_seconds)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id, name, protocol, base_url, auth_mode, credential_key_id,
			          credential_ciphertext, status, priority, weight, timeout_seconds,
			          created_at, updated_at, last_tested_at, last_test_status, last_error`,
			ch.ID, ch.Name, ch.Protocol, ch.BaseURL, ch.AuthMode, ch.CredentialKeyID,
			ch.CredentialCiphertext, ch.Status, ch.Priority, ch.Weight, ch.TimeoutSeconds,
		))
		return scanErr
	})
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: name already exists", ErrChannelConflict)
		}
		return nil, fmt.Errorf("create llm channel: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) UpdateChannel(ctx context.Context, ch Channel, updateCredential bool) (*Channel, error) {
	var row Channel
	err := secretlock.WithTx(ctx, r.pool, true, func(ctx context.Context, tx secretlock.Tx) error {
		if updateCredential {
			if err := secretlock.EnsureWritableKey(ctx, tx, ch.CredentialKeyID); err != nil {
				return err
			}
		}
		var scanErr error
		row, scanErr = scanChannel(tx.QueryRow(ctx, `
			UPDATE llm_channels
			   SET name = $2,
			       protocol = $3,
			       base_url = $4,
			       auth_mode = $5,
			       credential_key_id = CASE WHEN $12 THEN $6 ELSE credential_key_id END,
			       credential_ciphertext = CASE WHEN $12 THEN $7 ELSE credential_ciphertext END,
			       status = $8,
			       priority = $9,
			       weight = $10,
			       timeout_seconds = $11,
			       updated_at = NOW()
			 WHERE id = $1
			RETURNING id, name, protocol, base_url, auth_mode, credential_key_id,
			          credential_ciphertext, status, priority, weight, timeout_seconds,
			          created_at, updated_at, last_tested_at, last_test_status, last_error`,
			ch.ID, ch.Name, ch.Protocol, ch.BaseURL, ch.AuthMode, ch.CredentialKeyID,
			ch.CredentialCiphertext, ch.Status, ch.Priority, ch.Weight, ch.TimeoutSeconds,
			updateCredential,
		))
		return scanErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChannelNotFound
	}
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: name already exists", ErrChannelConflict)
		}
		return nil, fmt.Errorf("update llm channel: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) UpdateChannelTestResult(
	ctx context.Context,
	id uuid.UUID,
	status string,
	lastError string,
) (*Channel, error) {
	row, err := scanChannel(r.pool.QueryRow(ctx, `
		UPDATE llm_channels
		   SET last_tested_at = NOW(),
		       last_test_status = $2,
		       last_error = $3,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, name, protocol, base_url, auth_mode, credential_key_id,
		          credential_ciphertext, status, priority, weight, timeout_seconds,
		          created_at, updated_at, last_tested_at, last_test_status, last_error`,
		id, status, lastError,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update llm channel test result: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) DeleteChannel(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM llm_channels WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete llm channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChannelNotFound
	}
	return nil
}

type channelScanner interface {
	Scan(dest ...any) error
}

func scanChannel(row channelScanner) (Channel, error) {
	var ch Channel
	err := row.Scan(
		&ch.ID, &ch.Name, &ch.Protocol, &ch.BaseURL, &ch.AuthMode,
		&ch.CredentialKeyID, &ch.CredentialCiphertext, &ch.Status,
		&ch.Priority, &ch.Weight, &ch.TimeoutSeconds, &ch.CreatedAt,
		&ch.UpdatedAt, &ch.LastTestedAt, &ch.LastTestStatus, &ch.LastError,
	)
	return ch, err
}
