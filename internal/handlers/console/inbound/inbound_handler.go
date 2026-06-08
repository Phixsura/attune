// SPDX-License-Identifier: Apache-2.0

// Package inbound is the console handler subpackage for inbound source
// management (#66 Plan T15). It exposes /fb/v1/console/inbound/sources
// endpoints: list, create, detail, rotate secret, pause/resume, delete,
// and an IMAP-only test-connection probe.
//
// All request / response shapes are attune.v1 proto messages
// (CLAUDE.md §11) — the InboundSourceService RPCs in
// proto/attune/v1/inbound_source.proto are the source of truth; the
// REST surface is the google.api.http transcoding of those RPCs and
// regenerates on every `make proto`.
//
// Layering note (CLAUDE.md §5 + the inbound-boundary depguard rule):
// this is the one handler subpackage allowed to import an inbound
// adapter directly. It calls webhook.RotateSecret as the operator-side
// wiring point for the dual-secret rotation primitive, analogous to how
// cmd/attune blank-imports adapters for Start. The depguard rule has an
// explicit exception for this path.
package inbound

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/email"
	"github.com/Phixsura/attune/internal/inbound/adapter/webhook"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
)

// Channel names — aliased from the adapter packages so a typo in this
// file fails compilation, AND the literal lives in exactly one place
// per channel (#66 review M7).
const (
	channelWebhook = webhook.Channel
	channelEmail   = email.Channel

	// secretLen is 32 bytes (256 bits) of randomness per webhook
	// source — same length the adapter uses on rotate.
	secretLen = 32

	// testConnTimeout caps the IMAP dial+login+select+logout probe so
	// the operator UI stays responsive even when the remote host is
	// silently dropping packets.
	testConnTimeout = 8 * time.Second
)

// slugSanitize is the regex used to derive a URL-safe slug from the
// human-facing source name. Anything outside [a-z0-9-] gets folded to
// "-"; multiple "-" collapse to one, then we trim. The slug ends up in
// the public webhook URL so it's lower-case ASCII only.
var slugSanitize = regexp.MustCompile(`[^a-z0-9]+`)

// rotator is the subset of webhook.RotateSecret the handler depends on,
// kept as an interface so tests can stub it without standing up Postgres.
type rotator func(ctx context.Context, pool *pgxpool.Pool, secrets inbound.SecretStore, sourceID string) ([]byte, time.Time, error)

// testConnFn is the seam used by TestConnection for the IMAP probe.
// Production uses imapDialAndProbe; tests inject a fake that returns
// nil/err without opening a TCP socket.
type testConnFn func(ctx context.Context, cfg testConnInputs) error

// tenantLookup resolves a tenant's slug from its id — needed to build
// the public webhook URL the create response surfaces. Production uses
// the tenants table; tests stub.
type tenantLookup func(ctx context.Context, tenantID string) (string, error)

// Handler implements the /fb/v1/console/inbound/sources surface.
// `sources` types as the framework's own `inbound.SourceStore` so the
// handler and the adapter framework share one interface (#66 review
// M-5; the prior 3-method `sourceRepo` was a strict subset).
type Handler struct {
	sources    inbound.SourceStore
	pool       *pgxpool.Pool
	secrets    inbound.SecretStore
	baseURL    string
	rotate     rotator
	testConn   testConnFn
	tenantSlug tenantLookup
}

// NewHandler wires production dependencies — pool for direct DELETE,
// secrets for envelope encryption, baseURL for the public webhook URL
// echoed back on create. tenantSlug is resolved on-demand from the
// pool; callers do not need to pass it explicitly.
func NewHandler(sources *inboundsource.Repo, p *pgxpool.Pool, secrets inbound.SecretStore, baseURL string) *Handler {
	return ptrext.Of(Handler{
		sources:    sources,
		pool:       p,
		secrets:    secrets,
		baseURL:    strings.TrimRight(baseURL, "/"),
		rotate:     webhook.RotateSecret,
		testConn:   imapDialAndProbe,
		tenantSlug: tenantSlugFromPool(p),
	})
}

// rowToProto projects an inbound.Source into the wire-shape
// attunev1.InboundSource (CLAUDE.md §11).
func rowToProto(s inbound.Source) *attunev1.InboundSource {
	out := ptrext.Of(attunev1.InboundSource{
		Id:        s.ID,
		TenantId:  s.TenantID,
		Channel:   s.Channel,
		Name:      s.Name,
		Slug:      s.Slug,
		Enabled:   s.Enabled,
		LastUid:   s.State.LastUID,
		LastError: s.State.LastError,
		CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if s.State.LastEventAt != nil {
		out.LastEventAt = ptrext.Of(s.State.LastEventAt.UTC().Format(time.RFC3339))
	}
	return out
}

// List handles GET /fb/v1/console/inbound/sources.
//
// SourceStore.List filters on (channel, enabled=TRUE); the operator UI
// needs to see paused rows too, so we query the table directly. The
// alternative — extending SourceStore with a per-tenant "list all" —
// would break the adapter contract just to feed the console.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	const where = "console.inbound.List"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)

	rows, err := h.listAllForTenant(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to list inbound sources")
		return
	}
	items := make([]*attunev1.InboundSource, 0, len(rows))
	for _, row := range rows {
		items = append(items, rowToProto(row))
	}
	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.ListInboundSourcesResponse{Items: items}))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
}

// listAllForTenant — direct query that ignores the enabled flag (so the
// UI can show paused rows). Kept inside the handler to avoid widening
// the SourceStore contract for a console-only need.
func (h *Handler) listAllForTenant(ctx context.Context, tenantID string) ([]inbound.Source, error) {
	if h.pool == nil {
		return nil, errors.New("inbound: pool not configured")
	}
	rows, err := h.pool.Query(
		ctx,
		`SELECT id, tenant_id, channel, name, slug, enabled,
		        last_event_at, last_uid, last_error, created_at, updated_at
		   FROM inbound_sources
		  WHERE tenant_id = $1
		  ORDER BY channel ASC, name ASC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("inbound: query: %w", err)
	}
	defer rows.Close()
	var out []inbound.Source
	for rows.Next() {
		var s inbound.Source
		var lastEventAt *time.Time
		var lastError *string
		if err := rows.Scan(&s.ID, &s.TenantID, &s.Channel, &s.Name, &s.Slug,
			&s.Enabled, &lastEventAt, &s.State.LastUID, &lastError,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("inbound: scan: %w", err)
		}
		s.State.LastEventAt = lastEventAt
		s.State.LastError = ptrext.Indirect(lastError)
		out = append(out, s)
	}
	return out, rows.Err()
}

// Get handles GET /fb/v1/console/inbound/sources/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	const where = "console.inbound.Get"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	id := chi.URLParam(r, "id")
	if !isUUID(id) {
		logext.Warnf(ctx, "[%s] reject: bad id,tenant_id:%s", where, auth.TenantID)
		respond.Error(ctx, w, http.StatusBadRequest, "bad_id", "id is not a UUID")
		return
	}
	src, err := h.sources.Get(ctx, id)
	if err != nil {
		if errors.Is(err, inboundsource.ErrNotFound) {
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "inbound source not found")
			return
		}
		logext.Errorf(ctx, "[%s] sources.Get failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to load inbound source")
		return
	}
	if src.TenantID != auth.TenantID {
		// Same response shape as not-found to avoid leaking other tenants' IDs.
		respond.Error(ctx, w, http.StatusNotFound, "not_found", "inbound source not found")
		return
	}
	respond.Proto(w, http.StatusOK, rowToProto(src))
}

// Create handles POST /fb/v1/console/inbound/sources.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	const where = "console.inbound.Create"
	ctx := r.Context()
	auth := session.FromContext(ctx)

	var req attunev1.CreateInboundSourceRequest
	if err := respond.Decode(r.Body, &req); err != nil {
		if errors.Is(err, respond.ErrBodyTooLarge) {
			respond.Error(ctx, w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds 1 MiB")
			return
		}
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "request body is not valid JSON")
		return
	}
	channel := strings.TrimSpace(strings.ToLower(req.GetChannel()))
	name := strings.TrimSpace(req.GetName())
	if channel != channelWebhook && channel != channelEmail {
		respond.Error(ctx, w, http.StatusBadRequest, "validation", "channel must be webhook or email")
		return
	}
	if name == "" || len(name) > 200 {
		respond.Error(ctx, w, http.StatusBadRequest, "validation", "name must be non-empty and ≤ 200 characters")
		return
	}
	slug := slugify(name)
	if slug == "" {
		respond.Error(ctx, w, http.StatusBadRequest, "validation", "name must contain at least one alphanumeric character")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,channel:%s,name:%s,slug:%s",
		where, auth.TenantID, channel, name, slug)

	switch channel {
	case channelWebhook:
		h.createWebhook(ctx, w, auth, name, slug)
	case channelEmail:
		h.createEmail(ctx, w, auth, &req, name, slug) // ptrext:allow proto-decoded-value
	}
}

// createWebhook handles the webhook-channel branch of Create. Generates
// a fresh 32-byte secret, encrypts both the secret and the wrapping
// webhookConfig JSON via the SecretStore, then inserts the row. The raw
// secret is returned once in the response — never persisted plaintext.
func (h *Handler) createWebhook(ctx context.Context, w http.ResponseWriter, auth *session.AuthCtx, name, slug string) {
	const where = "console.inbound.createWebhook"
	id := uuid.NewString()

	rawSecret := make([]byte, secretLen)
	if _, err := rand.Read(rawSecret); err != nil {
		logext.Errorf(ctx, "[%s] rand failed,err:%+v", where, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to generate secret")
		return
	}
	encSecret, err := h.secrets.Encrypt(rawSecret)
	if err != nil {
		logext.Errorf(ctx, "[%s] encrypt secret failed,err:%+v", where, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to encrypt secret")
		return
	}
	cfg := webhook.Config{
		Version:                webhook.ConfigVersion,
		SecretCurrentEncrypted: encSecret,
		HMACAlgo:               "sha256",
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		logext.Errorf(ctx, "[%s] marshal cfg failed,err:%+v", where, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to encode config")
		return
	}
	envelope, err := h.secrets.Encrypt(cfgBytes)
	if err != nil {
		logext.Errorf(ctx, "[%s] encrypt cfg failed,err:%+v", where, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to encrypt config")
		return
	}
	if err := h.insertRow(ctx, id, auth.TenantID, channelWebhook, name, slug, envelope); err != nil {
		h.handleInsertErr(ctx, w, where, auth.TenantID, err)
		return
	}

	stored, err := h.sources.Get(ctx, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] post-insert reload failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "row created but reload failed")
		return
	}
	resp := ptrext.Of(attunev1.CreateInboundSourceResponse{Source: rowToProto(stored)})

	tenantSlug, err := h.tenantSlug(ctx, auth.TenantID)
	if err != nil {
		// The row landed; surface a soft warning but still return the
		// secret so the operator can finish setup. The URL field will
		// be empty — they can derive it from docs.
		logext.Warnf(ctx, "[%s] tenantSlug lookup failed,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
		tenantSlug = ""
	}
	reveal := ptrext.Of(attunev1.WebhookSecretReveal{
		SecretHex: hex.EncodeToString(rawSecret),
	})
	if tenantSlug != "" && h.baseURL != "" {
		reveal.Url = fmt.Sprintf("%s/v1/inbound/webhook/%s/%s", h.baseURL, tenantSlug, slug)
		reveal.CurlExample = buildCurlExample(reveal.Url)
	}
	resp.WebhookSecretReveal = reveal
	respond.Proto(w, http.StatusCreated, resp)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,slug:%s",
		where, auth.TenantID, id, slug)
}

// createEmail handles the email-channel branch of Create. Validates the
// EmailCreateConfig, encrypts the IMAP password, wraps in an envelope,
// and inserts.
func (h *Handler) createEmail(ctx context.Context, w http.ResponseWriter, auth *session.AuthCtx, req *attunev1.CreateInboundSourceRequest, name, slug string) {
	const where = "console.inbound.createEmail"
	cfg := req.GetEmailConfig()
	if cfg == nil {
		respond.Error(ctx, w, http.StatusBadRequest, "validation", "email_config is required for channel=email")
		return
	}
	if err := validateEmailCreateConfig(cfg); err != nil {
		respond.Error(ctx, w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	envelope, err := h.encryptEmailConfig(cfg)
	if err != nil {
		logext.Errorf(ctx, "[%s] encrypt failed,err:%+v", where, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to encrypt config")
		return
	}
	id := uuid.NewString()
	if err := h.insertRow(ctx, id, auth.TenantID, channelEmail, name, slug, envelope); err != nil {
		h.handleInsertErr(ctx, w, where, auth.TenantID, err)
		return
	}
	stored, err := h.sources.Get(ctx, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] post-insert reload failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "row created but reload failed")
		return
	}
	respond.Proto(w, http.StatusCreated, ptrext.Of(attunev1.CreateInboundSourceResponse{
		Source: rowToProto(stored),
	}))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,slug:%s",
		where, auth.TenantID, id, slug)
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

// insertRow — direct INSERT against inbound_sources. The repo layer
// exposes List/Get/UpdateState/SetEnabled (the adapter contract); a
// create path lives only in the console handler, so we own the SQL
// here. The caller has already validated channel/name/slug.
func (h *Handler) insertRow(ctx context.Context, id, tenantID, channel, name, slug string, envelope []byte) error {
	if h.pool == nil {
		return errors.New("inbound: pool not configured")
	}
	_, err := h.pool.Exec(
		ctx,
		`INSERT INTO inbound_sources (id, tenant_id, channel, name, slug, config, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, TRUE)`,
		id, tenantID, channel, name, slug, envelope,
	)
	return err
}

// handleInsertErr maps DB errors at insert time. The unique constraint
// on (tenant_id, channel, slug) is the only operator-actionable case;
// everything else is internal.
func (h *Handler) handleInsertErr(ctx context.Context, w http.ResponseWriter, where, tenantID string, err error) {
	if isUniqueViolation(err) {
		logext.Warnf(ctx, "[%s] reject: slug conflict,tenant_id:%s", where, tenantID)
		respond.Error(ctx, w, http.StatusConflict, "conflict",
			"another inbound source with the same name already exists; pick a different name")
		return
	}
	logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
	respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to create inbound source")
}

// Rotate handles POST /fb/v1/console/inbound/sources/{id}/rotate-secret.
// Calls into webhook.RotateSecret (the one place where the handler
// layer touches an adapter directly — documented exception in the
// inbound-boundary depguard rule).
func (h *Handler) Rotate(w http.ResponseWriter, r *http.Request) {
	const where = "console.inbound.Rotate"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	id := chi.URLParam(r, "id")
	if !isUUID(id) {
		respond.Error(ctx, w, http.StatusBadRequest, "bad_id", "id is not a UUID")
		return
	}
	src, err := h.sources.Get(ctx, id)
	if err != nil {
		if errors.Is(err, inboundsource.ErrNotFound) {
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "inbound source not found")
			return
		}
		logext.Errorf(ctx, "[%s] sources.Get failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to load inbound source")
		return
	}
	if src.TenantID != auth.TenantID {
		respond.Error(ctx, w, http.StatusNotFound, "not_found", "inbound source not found")
		return
	}
	if src.Channel != channelWebhook {
		respond.Error(ctx, w, http.StatusBadRequest, "unsupported",
			"rotate-secret only applies to webhook sources")
		return
	}

	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
	newSecret, nextEligible, err := h.rotate(ctx, h.pool, h.secrets, id)
	if err != nil {
		if errors.Is(err, webhook.ErrRotationInGraceWindow) {
			// Spec §Webhook adapter / Rotate behavior requires
			// `next_eligible_at` as a top-level field next to the
			// usual envelope (G5 fix, #66).
			nextStr := nextEligible.UTC().Format(time.RFC3339)
			respond.ErrorWithExtra(ctx, w, http.StatusConflict,
				"rotation_in_grace_window",
				"rotation refused while previous secret is still inside its 24h grace window",
				map[string]any{"nextEligibleAt": nextStr})
			logext.Warnf(ctx, "[%s] reject: grace window,tenant_id:%s,id:%s,next:%s",
				where, auth.TenantID, id, nextStr)
			return
		}
		logext.Errorf(ctx, "[%s] rotate failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to rotate secret")
		return
	}
	// Reuse the `nextEligible` that the rotator already computed (and
	// persisted into the on-disk envelope). Recomputing `time.Now() +
	// GraceWindow` here drifted by microseconds vs the DB value (#66
	// review H-3); the response field and the actual grace boundary
	// now agree to the nanosecond.
	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.RotateInboundSourceSecretResponse{
		SecretHex:      hex.EncodeToString(newSecret),
		NextEligibleAt: nextEligible.UTC().Format(time.RFC3339),
	}))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
}

// Pause / Resume share SetEnabled; reason is fixed on pause for
// operator-trail visibility (it lands in inbound_sources.last_error).
func (h *Handler) Pause(w http.ResponseWriter, r *http.Request)  { h.setEnabled(w, r, false) }
func (h *Handler) Resume(w http.ResponseWriter, r *http.Request) { h.setEnabled(w, r, true) }

func (h *Handler) setEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	const where = "console.inbound.setEnabled"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	id := chi.URLParam(r, "id")
	if !isUUID(id) {
		respond.Error(ctx, w, http.StatusBadRequest, "bad_id", "id is not a UUID")
		return
	}
	src, err := h.sources.Get(ctx, id)
	if err != nil {
		if errors.Is(err, inboundsource.ErrNotFound) {
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "inbound source not found")
			return
		}
		logext.Errorf(ctx, "[%s] sources.Get failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to load inbound source")
		return
	}
	if src.TenantID != auth.TenantID {
		respond.Error(ctx, w, http.StatusNotFound, "not_found", "inbound source not found")
		return
	}
	reason := ""
	if !enabled {
		reason = "paused by admin"
	}
	if err := h.sources.SetEnabled(ctx, id, enabled, reason); err != nil {
		logext.Errorf(ctx, "[%s] SetEnabled failed,tenant_id:%s,id:%s,enabled:%v,err:%+v",
			where, auth.TenantID, id, enabled, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to update enabled flag")
		return
	}
	// Reload so the response carries the freshly-mutated state.
	updated, err := h.sources.Get(ctx, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] post-toggle reload failed,id:%s,err:%+v",
			where, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "toggle ok but reload failed")
		return
	}
	respond.Proto(w, http.StatusOK, rowToProto(updated))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,enabled:%v",
		where, auth.TenantID, id, enabled)
}

// Delete handles DELETE /fb/v1/console/inbound/sources/{id}. The
// SourceStore interface deliberately stops at SetEnabled (the adapters
// only ever need to toggle), so we issue the DELETE inline here.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	const where = "console.inbound.Delete"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	id := chi.URLParam(r, "id")
	if !isUUID(id) {
		respond.Error(ctx, w, http.StatusBadRequest, "bad_id", "id is not a UUID")
		return
	}
	// Look up first to enforce tenant ownership in a single round-trip
	// before issuing the destructive SQL.
	src, err := h.sources.Get(ctx, id)
	if err != nil {
		if errors.Is(err, inboundsource.ErrNotFound) {
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "inbound source not found")
			return
		}
		logext.Errorf(ctx, "[%s] sources.Get failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to load inbound source")
		return
	}
	if src.TenantID != auth.TenantID {
		respond.Error(ctx, w, http.StatusNotFound, "not_found", "inbound source not found")
		return
	}

	if h.pool == nil {
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "inbound: pool not configured")
		return
	}
	tag, err := h.pool.Exec(ctx, `DELETE FROM inbound_sources WHERE id = $1`, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] delete failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to delete inbound source")
		return
	}
	if tag.RowsAffected() == 0 {
		// Race: deleted between Get and Exec. Treat as 404 — the
		// admin's intent is reached either way.
		respond.Error(ctx, w, http.StatusNotFound, "not_found", "inbound source not found")
		return
	}
	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.DeleteInboundSourceResponse{}))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
}

// TestConnection handles POST /fb/v1/console/inbound/sources/test-connection.
// Email-only IMAP probe: dial + login + select(folder) + logout.
// Response: TestInboundConnectionResponse{ok=true} or {ok=false,error=...}
// — never 500; even an internal failure surfaces as ok:false so the
// wizard UI can render the result without special-case branching.
func (h *Handler) TestConnection(w http.ResponseWriter, r *http.Request) {
	const where = "console.inbound.TestConnection"
	ctx := r.Context()
	auth := session.FromContext(ctx)

	var req attunev1.TestInboundConnectionRequest
	if err := respond.Decode(r.Body, &req); err != nil {
		respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of("invalid request body"),
		}))
		return
	}
	if strings.TrimSpace(strings.ToLower(req.GetChannel())) != channelEmail {
		respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of("test-connection only supports the email channel"),
		}))
		return
	}
	cfg := req.GetEmailConfig()
	if cfg == nil {
		respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of("email_config is required"),
		}))
		return
	}
	inputs, err := validateEmailConnConfig(cfg)
	if err != nil {
		respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of(err.Error()),
		}))
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, testConnTimeout)
	defer cancel()
	start := time.Now()
	if err := h.testConn(probeCtx, inputs); err != nil {
		logext.Warnf(ctx, "[%s] probe failed,tenant_id:%s,host:%s,err:%s",
			where, auth.TenantID, inputs.Host, err.Error())
		respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.TestInboundConnectionResponse{
			Ok:    false,
			Error: ptrext.Of(err.Error()),
		}))
		return
	}
	latency := time.Since(start).Milliseconds()
	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.TestInboundConnectionResponse{
		Ok:        true,
		LatencyMs: ptrext.Of(latency),
	}))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,host:%s,latency_ms:%d",
		where, auth.TenantID, inputs.Host, latency)
}

// testConnInputs — narrower variant of EmailCreateConfig used only by
// the test-connection probe. Keeps password in plaintext since the IMAP
// client needs it for the LOGIN command and the value never lands in
// the DB.
type testConnInputs struct {
	Host     string
	Port     int
	TLS      bool
	Username string
	Password string
	Folder   string
}

// imapDialAndProbe — production IMAP probe. TLS-only (review H2, #66):
// the cfg.TLS=false escape hatch is gone here just as it is in the
// inbound/adapter/email runtime — loopback reverse proxies that
// terminate TLS can front a plain-IMAP server if the operator truly
// needs one. After dial, runs LOGIN / SELECT / LOGOUT. Returns any
// error verbatim — the handler wraps it into the proto response.
func imapDialAndProbe(_ context.Context, cfg testConnInputs) error {
	if err := email.ValidateOutboundHost(cfg.Host); err != nil {
		// SSRF guard (#66 review M-3) — surfaces the validate-shape
		// error verbatim. The handler wraps it into the proto response;
		// operators see the blocked-host detail.
		return err
	}
	addr := cfg.Host + ":" + strconv.Itoa(cfg.Port)
	opt := ptrext.Of(imapclient.Options{})
	cli, err := imapclient.DialTLS(addr, opt)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = cli.Logout() }()
	if err := cli.Login(cfg.Username, cfg.Password).Wait(); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	folder := cfg.Folder
	if folder == "" {
		folder = "INBOX"
	}
	if _, err := cli.Select(folder, ptrext.Of(imap.SelectOptions{})).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", folder, err)
	}
	return nil
}

// --- internal helpers below this line --------------------------------

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

// validateEmailConnConfig — narrower variant for the test-connection
// probe. Returns a normalised testConnInputs on success. Like the
// create-source path it requires tls=true (review H2, #66).
func validateEmailConnConfig(cfg *attunev1.EmailConnConfig) (testConnInputs, error) {
	if !cfg.GetTls() {
		return testConnInputs{}, errors.New("tls must be true (plain IMAP is disallowed)")
	}
	out := testConnInputs{
		Host:     strings.TrimSpace(cfg.GetHost()),
		Port:     int(cfg.GetPort()),
		TLS:      true,
		Username: strings.TrimSpace(cfg.GetUsername()),
		Password: cfg.GetPassword(),
		Folder:   strings.TrimSpace(cfg.GetFolder()),
	}
	if out.Host == "" {
		return out, errors.New("email_config.host must not be empty")
	}
	if out.Port < 1 || out.Port > 65535 {
		return out, errors.New("email_config.port must be 1..65535")
	}
	if out.Username == "" {
		return out, errors.New("email_config.username must not be empty")
	}
	if out.Password == "" {
		return out, errors.New("email_config.password must not be empty")
	}
	return out, nil
}

// slugify — keep only [a-z0-9], collapse runs of separators to '-'.
// Empty input → empty output (caller treats as a validation error).
func slugify(in string) string {
	lower := strings.ToLower(strings.TrimSpace(in))
	replaced := slugSanitize.ReplaceAllString(lower, "-")
	return strings.Trim(replaced, "-")
}

// buildCurlExample — short curl one-liner the operator can paste. Body
// is an inline IngestRequest stub matching the proto JSON shape; the
// signature here is dummy (the operator computes the real HMAC client-
// side; the example is just to show the call shape).
func buildCurlExample(targetURL string) string {
	return fmt.Sprintf(`curl -X POST %q -H 'Content-Type: application/json' `+
		`-H 'X-Attune-Timestamp: <unix-seconds>' -H 'X-Attune-Signature: <hex-hmac>' `+
		`-d '{"content":"hello from webhook","source_user":"alice@example.com"}'`, targetURL)
}

// isUUID — quick UUID-shape gate before issuing DB ops.
func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// isUniqueViolation — pgx unique-constraint check. Used at insert time
// so duplicate (tenant_id, channel, slug) returns 409 not 500.
func isUniqueViolation(err error) bool {
	type sqlState interface{ SQLState() string }
	var st sqlState
	return errors.As(err, &st) && st.SQLState() == "23505"
}

// tenantSlugFromPool — production tenant slug resolver. Returns the
// tenant's `slug` column from the tenants table. Empty pool → returns
// a stub that errors (constructor calls this with the live pool so
// production never hits the stub path).
func tenantSlugFromPool(p *pgxpool.Pool) tenantLookup {
	return func(ctx context.Context, tenantID string) (string, error) {
		if p == nil {
			return "", errors.New("inbound: pool not configured")
		}
		var slug string
		err := p.QueryRow(ctx,
			`SELECT slug FROM tenants WHERE id = $1`, tenantID).Scan(&slug)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("tenant not found")
		}
		return slug, err
	}
}
