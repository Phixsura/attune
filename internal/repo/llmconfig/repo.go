// SPDX-License-Identifier: Apache-2.0

package llmconfig

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var (
	ErrChannelNotFound       = errors.New("llmconfig: channel not found")
	ErrChannelConflict       = errors.New("llmconfig: channel conflict")
	ErrRouteNotFound         = errors.New("llmconfig: route not found")
	ErrNoCandidates          = errors.New("llmconfig: no enabled channel candidates")
	ErrSecretKeyUnavailable  = errors.New("llmconfig: encrypted secrets require unavailable Tink key ids")
	ErrSecretKeyUndetectable = errors.New("llmconfig: encrypted secrets use an unknown or legacy envelope")
	ErrSecretKeyMismatch     = errors.New("llmconfig: encrypted secret key id metadata mismatch")
	ErrSecretKeyFingerprint  = errors.New("llmconfig: secret key registry fingerprint mismatch")
	ErrSecretKeyRetired      = errors.New("llmconfig: secret key is retired")
)

type Repo struct {
	pool *pgxpool.Pool
}

type keyRegistryQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

func (r *Repo) SyncKeyRegistry(ctx context.Context, rows []KeyRegistryRow) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin secret key registry sync: %w", err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('attune.secret_key_registry'))`); err != nil {
		return fmt.Errorf("lock secret key registry: %w", err)
	}
	for _, row := range rows {
		if err := validateKeyRegistryRow(ctx, tx, row); err != nil {
			return err
		}
	}
	primary := primaryKeyID(rows)
	if primary != "" {
		if _, err := tx.Exec(
			ctx,
			`UPDATE secret_key_registry SET primary_key = FALSE WHERE primary_key AND key_id <> $1`,
			primary,
		); err != nil {
			return fmt.Errorf("clear previous primary secret key: %w", err)
		}
	}
	for _, row := range rows {
		if err := upsertKeyRegistryRow(ctx, tx, row); err != nil {
			return err
		}
	}
	finished = true
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit secret key registry sync: %w", err)
	}
	return nil
}

func validateKeyRegistryRow(ctx context.Context, q keyRegistryQuerier, row KeyRegistryRow) error {
	var existingFingerprint string
	var existingVersion int
	var retired bool
	err := q.QueryRow(
		ctx, `
		SELECT fingerprint_sha256, fingerprint_version, retired_at IS NOT NULL
		  FROM secret_key_registry
		 WHERE key_id = $1`,
		row.KeyID,
	).Scan(&existingFingerprint, &existingVersion, &retired)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read secret key registry row: %w", err)
	}
	if retired && row.Primary {
		return fmt.Errorf("%w: key_id=%s", ErrSecretKeyRetired, row.KeyID)
	}
	if existingFingerprint == "" || row.FingerprintSHA256 == "" {
		return nil
	}
	if existingVersion == row.FingerprintVersion && existingFingerprint != row.FingerprintSHA256 {
		return fmt.Errorf("%w: key_id=%s", ErrSecretKeyFingerprint, row.KeyID)
	}
	return nil
}

func (r *Repo) ValidateSecretKeyReferences(ctx context.Context, keyIDs []string) error {
	refs, err := r.listSecretKeyReferences(ctx)
	if err != nil {
		return err
	}
	available := make(map[string]struct{}, len(keyIDs))
	for _, keyID := range keyIDs {
		available[keyID] = struct{}{}
	}
	var missing []string
	var undetectable []string
	for _, ref := range refs {
		if ref.KeyID == "" {
			undetectable = append(undetectable, ref.Location+" row="+ref.RowID)
			continue
		}
		if _, ok := available[ref.KeyID]; !ok {
			missing = append(missing, ref.Location+" row="+ref.RowID+" key_id="+ref.KeyID)
		}
	}
	if len(undetectable) > 0 {
		return fmt.Errorf("%w: %s", ErrSecretKeyUndetectable, strings.Join(undetectable, "; "))
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrSecretKeyUnavailable, strings.Join(missing, "; "))
	}
	return nil
}

func upsertKeyRegistryRow(ctx context.Context, q keyRegistryQuerier, row KeyRegistryRow) error {
	_, err := q.Exec(
		ctx, `
		INSERT INTO secret_key_registry
		 (key_id, primary_key, status, type_url, output_prefix_type, key_material_type,
		  fingerprint_sha256, fingerprint_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (key_id) DO UPDATE SET
		 primary_key = CASE
		   WHEN secret_key_registry.retired_at IS NULL THEN EXCLUDED.primary_key
		   ELSE FALSE
		 END,
		 status = CASE
		   WHEN secret_key_registry.retired_at IS NULL THEN EXCLUDED.status
		   ELSE secret_key_registry.status
		 END,
		 type_url = EXCLUDED.type_url,
		 output_prefix_type = EXCLUDED.output_prefix_type,
		 key_material_type = EXCLUDED.key_material_type,
		 fingerprint_sha256 = EXCLUDED.fingerprint_sha256,
		 fingerprint_version = EXCLUDED.fingerprint_version,
		 last_seen_at = NOW()`,
		row.KeyID, row.Primary, normalizeKeyStatus(row.Status), row.TypeURL,
		row.OutputPrefixType, row.KeyMaterialType, row.FingerprintSHA256,
		row.FingerprintVersion,
	)
	if err != nil {
		return fmt.Errorf("upsert secret key registry: %w", err)
	}
	return nil
}

func primaryKeyID(rows []KeyRegistryRow) string {
	for _, row := range rows {
		if row.Primary {
			return row.KeyID
		}
	}
	return ""
}

func normalizeKeyStatus(status string) string {
	switch status {
	case "ENABLED", "DISABLED", "DESTROYED":
		return status
	default:
		return "UNKNOWN"
	}
}

func (r *Repo) listSecretKeyReferences(ctx context.Context) ([]SecretKeyReference, error) {
	return r.listLLMSecretKeyReferences(ctx)
}

func (r *Repo) listLLMSecretKeyReferences(ctx context.Context) ([]SecretKeyReference, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, credential_key_id, credential_ciphertext
		  FROM llm_channels
		 WHERE credential_ciphertext IS NOT NULL
		   AND octet_length(credential_ciphertext) > 0`)
	if err != nil {
		return nil, fmt.Errorf("list llm credential key references: %w", err)
	}
	defer rows.Close()
	var refs []SecretKeyReference
	for rows.Next() {
		var ref SecretKeyReference
		var storedKeyID string
		var ciphertext []byte
		ref.Location = "llm_channels.credential_ciphertext"
		if err := rows.Scan(&ref.RowID, &storedKeyID, &ciphertext); err != nil {
			return nil, err
		}
		detectedKeyID := tinkKeyIDFromCiphertext(ciphertext)
		if storedKeyID != "" && detectedKeyID != "" && storedKeyID != detectedKeyID {
			return nil, fmt.Errorf("%w: %s row=%s credential_key_id=%s ciphertext_key_id=%s",
				ErrSecretKeyMismatch, ref.Location, ref.RowID, storedKeyID, detectedKeyID)
		}
		ref.KeyID = detectedKeyID
		if ref.KeyID == "" {
			ref.KeyID = storedKeyID
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (r *Repo) ListInboundSecretConfigs(ctx context.Context) ([]InboundConfigRef, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, channel, config
		  FROM inbound_sources
		 WHERE config IS NOT NULL
		   AND octet_length(config) > 0`)
	if err != nil {
		return nil, fmt.Errorf("list inbound source configs: %w", err)
	}
	defer rows.Close()
	var refs []InboundConfigRef
	for rows.Next() {
		var ref InboundConfigRef
		if err := rows.Scan(&ref.ID, &ref.Channel, &ref.Config); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func tinkKeyIDFromCiphertext(ciphertext []byte) string {
	if len(ciphertext) < 5 || ciphertext[0] != 0x01 {
		return ""
	}
	return fmt.Sprintf("%d", binary.BigEndian.Uint32(ciphertext[1:5]))
}

func mapNoRows(err error, mapped error, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return mapped
	}
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}
