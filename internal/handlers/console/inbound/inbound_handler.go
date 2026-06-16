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
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/email"
	"github.com/Phixsura/attune/internal/inbound/adapter/webhook"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// Channel names — aliased from the adapter packages so a typo in this
// package fails compilation, AND the literal lives in exactly one place
// per channel (#66 review M7).
const (
	channelWebhook = webhook.Channel
	channelEmail   = email.Channel
)

// rotator is the subset of webhook.RotateSecret the handler depends on,
// kept as an interface so tests can stub it without standing up Postgres.
type rotator func(ctx context.Context, pool *pgxpool.Pool, secrets inbound.SecretStore, sourceID string) ([]byte, time.Time, error)

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
	audit      auditRecorder
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
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

func (h *Handler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
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

func (h *Handler) recordAudit(
	ctx context.Context,
	authType, actorID, tenantID, action, targetID, summary string,
	req *http.Request,
	before, after any,
) error {
	if h.audit == nil {
		return nil
	}
	if authType == "" {
		authType = "admin"
	}
	return h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   tenantID,
		Actor:      auditlogsvc.ActorFromRequest(authType, actorID, req),
		Action:     action,
		TargetType: "inbound_source",
		TargetID:   targetID,
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}
