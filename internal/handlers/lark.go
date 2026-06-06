package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/lark"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/ingest"
)

// LarkHandler serves POST /v1/lark/event for Lark/Feishu event
// subscriptions. Protocol details (signature, envelope shape) live in
// internal/infra/lark; this handler is just routing + dispatch.
//
// When the signing secret is empty the handler responds 404 to avoid
// leaking implementation details to probing clients.
type LarkHandler struct {
	enabled           bool
	signingSecret     string
	verificationToken string
	defaultTenant     string // TEXT tenants.id
	ingestor          *ingest.Ingestor
}

// NewLarkHandler constructs the handler. If signingSecret is empty all
// requests get 404. defaultTenantSlug is resolved at startup via the
// tenant repo; pass an empty slug to keep the handler disabled.
func NewLarkHandler(
	ctx context.Context,
	tenants *tenant.TenantRepo,
	ingestor *ingest.Ingestor,
	signingSecret, verificationToken, defaultTenantSlug string,
) (*LarkHandler, error) {
	h := &LarkHandler{
		signingSecret:     signingSecret,
		verificationToken: verificationToken,
		ingestor:          ingestor,
	}
	if signingSecret == "" {
		return h, nil
	}
	tid, err := tenants.ResolveSlug(ctx, defaultTenantSlug)
	if errors.Is(err, tenant.ErrTenantNotFound) {
		return nil, fmt.Errorf("lark: tenant slug %q not found or inactive", defaultTenantSlug)
	}
	if err != nil {
		return nil, fmt.Errorf("lark: resolve tenant: %w", err)
	}
	h.defaultTenant = tid
	h.enabled = true
	return h, nil
}

func (h *LarkHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/event", h.Event)
	return r
}

func (h *LarkHandler) Event(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.enabled {
		http.Error(w, "lark integration disabled", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if err := lark.VerifySignature(
		h.signingSecret,
		r.Header.Get("X-Lark-Request-Timestamp"),
		r.Header.Get("X-Lark-Request-Nonce"),
		r.Header.Get("X-Lark-Signature"),
		body,
	); err != nil {
		slog.WarnContext(ctx, "lark signature check failed",
			"err", err, "event_id", r.Header.Get("X-Lark-Request-Id"))
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	ev, err := lark.Decode(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	switch ev.Kind {
	case lark.EventURLVerification:
		h.handleURLVerification(w, ev)
	case lark.EventMessageReceive:
		h.handleMessage(r.Context(), w, ev)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
}

func (h *LarkHandler) handleURLVerification(w http.ResponseWriter, ev lark.Event) {
	ctx := context.Background()
	if h.verificationToken != "" && ev.Token != h.verificationToken {
		slog.WarnContext(ctx, "lark url_verification token mismatch")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "verification token mismatch"})
		return
	}
	slog.InfoContext(ctx, "lark url_verification ok")
	writeJSON(w, http.StatusOK, map[string]string{"challenge": ev.Challenge})
}

func (h *LarkHandler) handleMessage(ctx context.Context, w http.ResponseWriter, ev lark.Event) {
	if ev.Text == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored-empty"})
		return
	}
	in := domain.IngestInput{
		Content:    ev.Text,
		Source:     "lark-group",
		SourceUser: ev.SenderOpenID,
		SourceMeta: map[string]any{
			"chat_id":    ev.ChatID,
			"message_id": ev.MessageID,
			"app_id":     ev.AppID,
			"event_id":   ev.EventID,
		},
	}
	// keyID is uuid.Nil for Lark-sourced rows; user_id becomes
	// "ext_<nil-uuid>:<open_id>".
	if _, err := h.ingestor.IngestRow(ctx, h.defaultTenant, uuid.Nil, in); err != nil {
		slog.WarnContext(ctx, "lark ingest failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
