// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrRotationInGraceWindow — RotateSecret refuses a second rotation
// while previous_expires_at is still in the future (24h grace from the
// first rotate). Per spec §Webhook adapter / Rotate behavior: this
// prevents a double-rotate from stranding clients mid-roll.
var ErrRotationInGraceWindow = errors.New("webhook: rotation in 24h grace window")

// SecretLen — 32 bytes (256 bits) of randomness per rotation.
const SecretLen = 32

// GraceWindow — how long the previous secret remains accepted after a
// rotation. 24h is the Hookdeck / Stripe convention.
const GraceWindow = 24 * time.Hour

// RotateSecret rotates a webhook source's HMAC secret. Returns the new
// plaintext secret once (response body); never persisted unencrypted.
// nextEligible is when the next rotation may run (only meaningful when
// the call succeeds; the grace window starts now).
//
// The whole operation is one DB transaction:
//
//  1. SELECT FOR UPDATE the row (locks against concurrent rotations).
//  2. Decode the current config; if previous_expires_at is in the
//     future, abort with ErrRotationInGraceWindow.
//  3. Generate a fresh secret, encrypt with the supplied SecretStore.
//  4. Promote current → previous, install the new current, stamp
//     previous_expires_at = now + GraceWindow, UPDATE the row.
//  5. COMMIT.
func RotateSecret(
	ctx context.Context,
	pool *pgxpool.Pool,
	secrets inbound.SecretStore,
	sourceID string,
) (newSecret []byte, nextEligible time.Time, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("rotate: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rawConfig []byte
	err = tx.QueryRow(
		ctx,
		`SELECT config FROM inbound_sources WHERE id = $1 FOR UPDATE`,
		sourceID,
	).Scan(&rawConfig)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, fmt.Errorf("rotate: source not found")
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("rotate: select: %w", err)
	}

	decoded, err := secrets.Decrypt(rawConfig)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("rotate: decrypt config: %w", err)
	}
	var cfg webhookConfig
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		return nil, time.Time{}, fmt.Errorf("rotate: unmarshal config: %w", err)
	}
	if cfg.PreviousExpiresAt != nil && cfg.PreviousExpiresAt.After(time.Now()) {
		return nil, ptrext.Indirect(cfg.PreviousExpiresAt), ErrRotationInGraceWindow
	}

	newSecret = make([]byte, SecretLen)
	if _, err := rand.Read(newSecret); err != nil {
		return nil, time.Time{}, fmt.Errorf("rotate: rand: %w", err)
	}
	newEncrypted, err := secrets.Encrypt(newSecret)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("rotate: encrypt new: %w", err)
	}

	expires := time.Now().Add(GraceWindow)
	cfg.SecretPreviousEncrypted = cfg.SecretCurrentEncrypted
	cfg.SecretCurrentEncrypted = newEncrypted
	cfg.PreviousExpiresAt = ptrext.Of(expires)
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.HMACAlgo == "" {
		cfg.HMACAlgo = "sha256"
	}

	updated, err := json.Marshal(cfg)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("rotate: marshal config: %w", err)
	}
	updatedEnvelope, err := secrets.Encrypt(updated)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("rotate: encrypt config: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE inbound_sources SET config = $1, updated_at = now() WHERE id = $2`,
		updatedEnvelope, sourceID,
	); err != nil {
		return nil, time.Time{}, fmt.Errorf("rotate: update: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, time.Time{}, fmt.Errorf("rotate: commit: %w", err)
	}
	return newSecret, expires, nil
}
