// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

const maxListLimit = 200

func EmailHash(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

func DestinationHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func URLHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

func mapWriteError(err error) error {
	if pgxutil.IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func jsonObject(value map[string]any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeObject(raw []byte) (map[string]any, error) {
	out := map[string]any{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil { // ptrext:allow unmarshal-out-param
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func scanSettings(row pgx.Row) (*Settings, error) {
	var s Settings
	var enabledRaw []byte
	var policyRaw []byte
	err := row.Scan(
		&s.TenantID,
		&s.EmailEnabled,
		&s.WebhookEnabled,
		&enabledRaw,
		&policyRaw,
		&s.DefaultConsentMode,
		&s.RequirePublicUpdateForStatus,
		&s.MaxRecipientsWithoutConfirm,
		&s.TenantHourlySendLimit,
		&s.ContactDailySendLimit,
		&s.UpdatedBy,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		return nil, mapNotFound(err)
	}
	enabled, err := decodeObject(enabledRaw)
	if err != nil {
		return nil, err
	}
	policy, err := decodeObject(policyRaw)
	if err != nil {
		return nil, err
	}
	s.EnabledEventTypes = enabled
	s.StatusPolicy = policy
	return ptrext.Of(s), nil
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}
