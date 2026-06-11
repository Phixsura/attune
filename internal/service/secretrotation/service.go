// SPDX-License-Identifier: Apache-2.0

// Package secretrotation re-encrypts runtime secrets with the current Tink
// primary key and verifies old-key retirement.
package secretrotation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	rotationrepo "github.com/Phixsura/attune/internal/repo/secretrotation"
)

var (
	ErrKeyStillReferenced = errors.New("secretrotation: key still has encrypted DB references")
	ErrPrimaryKeyRetire   = errors.New("secretrotation: cannot retire the active primary key")
)

type Repo interface {
	WithLockedSecrets(ctx context.Context, commit bool, fn func(context.Context, rotationrepo.Queries) error) error
}

type Store interface {
	PrimaryKeyID() string
	EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error)
	DecryptValue(value secretstore.EncryptedValue, aad []byte) ([]byte, error)
	Keys() []secretstore.KeyMetadata
}

type Service struct {
	repo  Repo
	store Store
}

func New(repo Repo, store Store) *Service {
	return ptrext.Of(Service{repo: repo, store: store})
}

type ReencryptOptions struct {
	Apply     bool   `json:"apply"`
	FromKeyID string `json:"from_key_id,omitempty"`
}

type RetireOptions struct {
	Apply bool `json:"apply"`
}

type Report struct {
	Apply          bool           `json:"apply"`
	PrimaryKeyID   string         `json:"primary_key_id"`
	FromKeyID      string         `json:"from_key_id,omitempty"`
	RetiredKeyID   string         `json:"retired_key_id,omitempty"`
	RowsScanned    int            `json:"rows_scanned"`
	RowsUpdated    int            `json:"rows_updated"`
	ValuesScanned  int            `json:"values_scanned"`
	ValuesCurrent  int            `json:"values_current"`
	ValuesFiltered int            `json:"values_filtered"`
	ValuesRotated  int            `json:"values_rotated"`
	Items          []ReportItem   `json:"items"`
	Warnings       []ReportNotice `json:"warnings,omitempty"`
}

type ReportItem struct {
	Location string `json:"location"`
	RowID    string `json:"row_id"`
	OldKeyID string `json:"old_key_id"`
	NewKeyID string `json:"new_key_id,omitempty"`
	Action   string `json:"action"`
}

type ReportNotice struct {
	Location string `json:"location"`
	RowID    string `json:"row_id"`
	Message  string `json:"message"`
}

type webhookConfigEnvelope struct {
	Version                 int     `json:"version"`
	SecretCurrentEncrypted  []byte  `json:"secret_current_encrypted"`
	SecretPreviousEncrypted []byte  `json:"secret_previous_encrypted,omitempty"`
	PreviousExpiresAt       *string `json:"previous_expires_at,omitempty"`
	HMACAlgo                string  `json:"hmac_algo"`
}

type emailConfigEnvelope struct {
	Version           int    `json:"version"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	TLS               bool   `json:"tls"`
	Username          string `json:"username"`
	PasswordEncrypted []byte `json:"password_encrypted"`
	Folder            string `json:"folder"`
	StartFrom         string `json:"start_from"`
	AfterIngest       string `json:"after_ingest"`
}

func (s *Service) Reencrypt(ctx context.Context, opt ReencryptOptions) (*Report, error) {
	opt.FromKeyID = strings.TrimSpace(opt.FromKeyID)
	report := s.newReport(opt)
	err := s.repo.WithLockedSecrets(ctx, opt.Apply, func(ctx context.Context, q rotationrepo.Queries) error {
		if err := s.rotateLLMCredentials(ctx, q, opt, report); err != nil {
			return err
		}
		return s.rotateInboundConfigs(ctx, q, opt, report)
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}

func (s *Service) RetireKey(ctx context.Context, keyID string, opt RetireOptions) (*Report, error) {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return nil, errors.New("secretrotation: key id is required")
	}
	if keyID == s.store.PrimaryKeyID() {
		return nil, ErrPrimaryKeyRetire
	}
	report := s.newReport(ReencryptOptions{Apply: opt.Apply, FromKeyID: keyID})
	report.RetiredKeyID = keyID
	err := s.repo.WithLockedSecrets(ctx, opt.Apply, func(ctx context.Context, q rotationrepo.Queries) error {
		if err := q.CheckKeyExists(ctx, keyID); err != nil {
			return err
		}
		if err := s.rotateLLMCredentials(ctx, q, ReencryptOptions{FromKeyID: keyID}, report); err != nil {
			return err
		}
		if err := s.rotateInboundConfigs(ctx, q, ReencryptOptions{FromKeyID: keyID}, report); err != nil {
			return err
		}
		if report.ValuesRotated > 0 {
			return fmt.Errorf("%w: key_id=%s references=%d", ErrKeyStillReferenced, keyID, report.ValuesRotated)
		}
		if opt.Apply {
			return q.RetireKey(ctx, keyID)
		}
		report.Items = append(report.Items, ReportItem{
			Location: "secret_key_registry",
			RowID:    keyID,
			OldKeyID: keyID,
			Action:   "would_retire",
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if opt.Apply {
		report.Items = append(report.Items, ReportItem{
			Location: "secret_key_registry",
			RowID:    keyID,
			OldKeyID: keyID,
			Action:   "retired",
		})
	}
	return report, nil
}

func (s *Service) newReport(opt ReencryptOptions) *Report {
	return ptrext.Of(Report{
		Apply:        opt.Apply,
		PrimaryKeyID: s.store.PrimaryKeyID(),
		FromKeyID:    strings.TrimSpace(opt.FromKeyID),
		Items:        []ReportItem{},
	})
}

func (s *Service) rotateLLMCredentials(
	ctx context.Context,
	q rotationrepo.Queries,
	opt ReencryptOptions,
	report *Report,
) error {
	rows, err := q.ListLLMCredentialsForUpdate(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		report.RowsScanned++
		newValue, changed, err := s.rotateValue(
			row.Ciphertext,
			row.KeyID,
			secretstore.AssociatedData("llm_channel", row.ID.String(), "api_key"),
			"llm_channels.credential_ciphertext",
			row.ID.String(),
			opt,
			report,
		)
		if err != nil {
			return err
		}
		if changed && opt.Apply {
			if err := q.UpdateLLMCredential(ctx, row.ID, newValue.KeyID, newValue.Ciphertext); err != nil {
				return err
			}
			report.RowsUpdated++
		}
	}
	return nil
}

func (s *Service) rotateInboundConfigs(
	ctx context.Context,
	q rotationrepo.Queries,
	opt ReencryptOptions,
	report *Report,
) error {
	rows, err := q.ListInboundConfigsForUpdate(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		report.RowsScanned++
		plaintext, err := s.decryptValue(row.Config, "", nil)
		if err != nil {
			return fmt.Errorf("decrypt inbound config row=%s: %w", row.ID, err)
		}
		nextPlaintext, nestedChanged, err := s.rotateInboundNested(row, plaintext, opt, report)
		if err != nil {
			return err
		}
		newOuter, outerChanged, err := s.rotateValue(
			row.Config,
			"",
			nil,
			"inbound_sources.config",
			row.ID.String(),
			opt,
			report,
		)
		if err != nil {
			return err
		}
		if nestedChanged {
			if !outerChanged {
				report.Items = append(report.Items, ReportItem{
					Location: "inbound_sources.config",
					RowID:    row.ID.String(),
					OldKeyID: s.store.PrimaryKeyID(),
					NewKeyID: s.store.PrimaryKeyID(),
					Action:   actionName(opt.Apply, "container_rewritten"),
				})
			}
			newOuter, err = s.store.EncryptValue(nextPlaintext, nil)
			if err != nil {
				return fmt.Errorf("encrypt inbound config row=%s: %w", row.ID, err)
			}
			outerChanged = true
		}
		if outerChanged && opt.Apply {
			if err := q.UpdateInboundConfig(ctx, row.ID, newOuter.Ciphertext); err != nil {
				return err
			}
			report.RowsUpdated++
		}
	}
	return nil
}

func (s *Service) rotateInboundNested(
	row rotationrepo.InboundConfigRow,
	plaintext []byte,
	opt ReencryptOptions,
	report *Report,
) ([]byte, bool, error) {
	switch row.Channel {
	case "webhook":
		return s.rotateWebhookConfig(row.ID, plaintext, opt, report)
	case "email":
		return s.rotateEmailConfig(row.ID, plaintext, opt, report)
	default:
		report.Warnings = append(report.Warnings, ReportNotice{
			Location: "inbound_sources.config",
			RowID:    row.ID.String(),
			Message:  "unknown inbound channel; only outer config was inspected",
		})
		return plaintext, false, nil
	}
}

func (s *Service) rotateWebhookConfig(
	rowID uuid.UUID,
	plaintext []byte,
	opt ReencryptOptions,
	report *Report,
) ([]byte, bool, error) {
	var cfg webhookConfigEnvelope
	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		return nil, false, fmt.Errorf("decode webhook config row=%s: %w", rowID, err)
	}
	changed := false
	current, ok, err := s.rotateValue(
		cfg.SecretCurrentEncrypted,
		"",
		nil,
		"inbound_sources.config.webhook.secret_current_encrypted",
		rowID.String(),
		opt,
		report,
	)
	if err != nil {
		return nil, false, err
	}
	if ok {
		cfg.SecretCurrentEncrypted = current.Ciphertext
		changed = true
	}
	if len(cfg.SecretPreviousEncrypted) > 0 {
		previous, ok, err := s.rotateValue(
			cfg.SecretPreviousEncrypted,
			"",
			nil,
			"inbound_sources.config.webhook.secret_previous_encrypted",
			rowID.String(),
			opt,
			report,
		)
		if err != nil {
			return nil, false, err
		}
		if ok {
			cfg.SecretPreviousEncrypted = previous.Ciphertext
			changed = true
		}
	}
	if !changed {
		return plaintext, false, nil
	}
	next, err := json.Marshal(cfg)
	if err != nil {
		return nil, false, fmt.Errorf("encode webhook config row=%s: %w", rowID, err)
	}
	return next, true, nil
}

func (s *Service) rotateEmailConfig(
	rowID uuid.UUID,
	plaintext []byte,
	opt ReencryptOptions,
	report *Report,
) ([]byte, bool, error) {
	var cfg emailConfigEnvelope
	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		return nil, false, fmt.Errorf("decode email config row=%s: %w", rowID, err)
	}
	password, changed, err := s.rotateValue(
		cfg.PasswordEncrypted,
		"",
		nil,
		"inbound_sources.config.email.password_encrypted",
		rowID.String(),
		opt,
		report,
	)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return plaintext, false, nil
	}
	cfg.PasswordEncrypted = password.Ciphertext
	next, err := json.Marshal(cfg)
	if err != nil {
		return nil, false, fmt.Errorf("encode email config row=%s: %w", rowID, err)
	}
	return next, true, nil
}

func (s *Service) rotateValue(
	ciphertext []byte,
	keyID string,
	aad []byte,
	location string,
	rowID string,
	opt ReencryptOptions,
	report *Report,
) (secretstore.EncryptedValue, bool, error) {
	oldKeyID, err := s.detectedKeyID(ciphertext, keyID)
	if err != nil {
		return secretstore.EncryptedValue{}, false, fmt.Errorf("%s row=%s: %w", location, rowID, err)
	}
	report.ValuesScanned++
	fromKeyID := strings.TrimSpace(opt.FromKeyID)
	if fromKeyID != "" && oldKeyID != fromKeyID {
		report.ValuesFiltered++
		return secretstore.EncryptedValue{KeyID: oldKeyID, Ciphertext: ciphertext}, false, nil
	}
	if oldKeyID == s.store.PrimaryKeyID() {
		report.ValuesCurrent++
		return secretstore.EncryptedValue{KeyID: oldKeyID, Ciphertext: ciphertext}, false, nil
	}
	plaintext, err := s.decryptValue(ciphertext, oldKeyID, aad)
	if err != nil {
		return secretstore.EncryptedValue{}, false, fmt.Errorf("%s row=%s: %w", location, rowID, err)
	}
	next, err := s.store.EncryptValue(plaintext, aad)
	if err != nil {
		return secretstore.EncryptedValue{}, false, fmt.Errorf("%s row=%s encrypt: %w", location, rowID, err)
	}
	report.ValuesRotated++
	report.Items = append(report.Items, ReportItem{
		Location: location,
		RowID:    rowID,
		OldKeyID: oldKeyID,
		NewKeyID: next.KeyID,
		Action:   actionName(opt.Apply, "rewritten"),
	})
	return next, true, nil
}

func (s *Service) decryptValue(ciphertext []byte, keyID string, aad []byte) ([]byte, error) {
	if keyID == "" {
		detected, err := s.detectedKeyID(ciphertext, "")
		if err != nil {
			return nil, err
		}
		keyID = detected
	}
	return s.store.DecryptValue(secretstore.EncryptedValue{
		KeyID:      keyID,
		Ciphertext: ciphertext,
	}, aad)
}

func (s *Service) detectedKeyID(ciphertext []byte, fallback string) (string, error) {
	storedKeyID := strings.TrimSpace(fallback)
	legacyShaped := secretstore.IsLegacyInboundEnvelope(ciphertext)
	ciphertextKeyID, err := secretstore.KeyIDFromCiphertext(ciphertext)
	if err == nil {
		if storedKeyID != "" && storedKeyID != ciphertextKeyID {
			return "", fmt.Errorf("stored key_id %s does not match ciphertext key_id %s", storedKeyID, ciphertextKeyID)
		}
		if storedKeyID != "" || !legacyShaped || s.hasLocalKey(ciphertextKeyID) {
			return ciphertextKeyID, nil
		}
	}
	if storedKeyID != "" {
		return "", err
	}
	if legacyShaped {
		return secretstore.LegacyInboundKeyID, nil
	}
	return "", err
}

func (s *Service) hasLocalKey(keyID string) bool {
	if s == nil || s.store == nil {
		return false
	}
	for _, key := range s.store.Keys() {
		if key.KeyID == keyID {
			return true
		}
	}
	return false
}

func actionName(apply bool, applied string) string {
	if apply {
		return applied
	}
	if strings.HasPrefix(applied, "container") {
		return "would_" + applied
	}
	return "would_rewrite"
}
