// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound/adapter/email"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// createEmail handles the email-channel branch of Create. Validates the
// EmailCreateConfig, encrypts the IMAP password, wraps in an envelope,
// and inserts.
func (h *Handler) createEmail(ctx context.Context, auth *session.AuthCtx, req *attunev1.CreateInboundSourceRequest, name, slug string) (dispatcher.Result[*attunev1.CreateInboundSourceResponse], error) {
	const where = "console.inbound.createEmail"
	cfg := req.GetEmailConfig()
	if cfg == nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "email_config is required for channel=email")
	}
	if err := validateEmailCreateConfig(cfg); err != nil {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	envelope, err := h.encryptEmailConfig(cfg)
	if err != nil {
		logext.Errorf(ctx, "[%s] encrypt failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to encrypt config")
	}
	id := uuid.NewString()
	if err := h.insertRow(ctx, id, auth.TenantID, channelEmail, name, slug, envelope); err != nil {
		return dispatcher.Result[*attunev1.CreateInboundSourceResponse]{}, h.insertErr(ctx, where, auth.TenantID, err)
	}
	stored, err := h.sources.Get(ctx, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] post-insert reload failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "row created but reload failed")
	}
	resp := ptrext.Of(attunev1.CreateInboundSourceResponse{
		Source: rowToProto(stored),
	})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,slug:%s",
		where, auth.TenantID, id, slug)
	return dispatcher.Created(resp)
}

// encryptEmailConfig — encrypts the IMAP password first (inner
// ciphertext lives inside the typed config blob), then encrypts the
// JSON-encoded config as the outer envelope.
func (h *Handler) encryptEmailConfig(cfg *attunev1.EmailCreateConfig) ([]byte, error) {
	encPW, err := h.secrets.Encrypt([]byte(cfg.GetPassword()))
	if err != nil {
		return nil, fmt.Errorf("encrypt password: %w", err)
	}
	folder := cfg.GetFolder()
	if folder == "" {
		folder = "INBOX"
	}
	startFrom := cfg.GetStartFrom()
	if startFrom == "" {
		startFrom = "now"
	}
	afterIngest := cfg.GetAfterIngest()
	if afterIngest == "" {
		afterIngest = "mark_seen"
	}
	inner := email.Config{
		Version:           email.ConfigVersion,
		Host:              cfg.GetHost(),
		Port:              int(cfg.GetPort()),
		TLS:               cfg.GetTls(),
		Username:          cfg.GetUsername(),
		PasswordEncrypted: encPW,
		Folder:            folder,
		StartFrom:         startFrom,
		AfterIngest:       afterIngest,
	}
	raw, err := json.Marshal(inner)
	if err != nil {
		return nil, fmt.Errorf("marshal email config: %w", err)
	}
	return h.secrets.Encrypt(raw)
}

// validateEmailCreateConfig — strict validation of the proto-decoded
// EmailCreateConfig before it touches encryption / insertion. Defaults
// for folder/start_from/after_ingest are applied in encryptEmailConfig
// rather than here so this stays a pure validator.
func validateEmailCreateConfig(cfg *attunev1.EmailCreateConfig) error {
	if strings.TrimSpace(cfg.GetHost()) == "" {
		return errors.New("email_config.host must not be empty")
	}
	if cfg.GetPort() < 1 || cfg.GetPort() > 65535 {
		return errors.New("email_config.port must be 1..65535")
	}
	if !cfg.GetTls() {
		// TLS-only (review H2, #66 — confirmed during Phase-4 audit).
		// Plain IMAP would expose the LOGIN literal (username +
		// password) over the wire; the runtime dialIMAP already
		// enforces TLS unconditionally, but rejecting at create time
		// stops a stale `tls:false` row from sitting in the table.
		return errors.New("email_config.tls must be true (plain IMAP is disallowed)")
	}
	if strings.TrimSpace(cfg.GetUsername()) == "" {
		return errors.New("email_config.username must not be empty")
	}
	if cfg.GetPassword() == "" {
		return errors.New("email_config.password must not be empty")
	}
	switch cfg.GetStartFrom() {
	case "", "now", "all_unseen":
		// ok
	default:
		return errors.New("email_config.start_from must be 'now' or 'all_unseen'")
	}
	return nil
}
