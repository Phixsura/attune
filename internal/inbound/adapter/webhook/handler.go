// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/respond"
)

const (
	channelName = "webhook"

	// maxBodyBytes — same 64 KiB cap as /v1/feedback/ingest. We bound
	// unauthenticated work cheaply (the cap runs before HMAC verify).
	maxBodyBytes = 64 * 1024

	hdrTimestamp = "X-Attune-Timestamp"
	hdrSignature = "X-Attune-Signature"
)

// ingestUnmarshal — protojson decoder configured to tolerate unknown
// fields (mirrors /v1/feedback/ingest decoder for behavioural parity).
var ingestUnmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}

// handle serves POST /v1/inbound/webhook/{tenant-slug}/{source-slug}.
// Spec: docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md
// §Webhook adapter.
func (a *adapter) handle(w http.ResponseWriter, r *http.Request) {
	const where = "inbound.webhook.handle"
	ctx := r.Context()

	tenantSlug := chi.URLParam(r, "tenant-slug")
	sourceSlug := chi.URLParam(r, "source-slug")
	ts := r.Header.Get(hdrTimestamp)
	sig := r.Header.Get(hdrSignature)

	// Read body BEFORE branching on the source lookup so timing does
	// not leak source existence. MaxBytesReader caps unauthenticated
	// work at maxBodyBytes.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		a.deps.Metrics.Total(channelName, tenantSlug, sourceSlug, "validate_err")
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "body too large or unreadable")
		return
	}

	src, srcErr := a.deps.Sources.GetBySlugs(ctx, tenantSlug, channelName, sourceSlug)
	if srcErr != nil || !src.Enabled {
		// Enumeration resistance: verify against stub secret so the
		// response time + status match a real auth failure.
		_ = verifyHMACAgainstStub(a.stubSecret, ts, body, sig)
		a.deps.Metrics.Total(channelName, tenantSlug, sourceSlug, "auth_err")
		respond.Error(ctx, w, http.StatusUnauthorized, "unauthorized", "signature or timestamp invalid")
		return
	}

	current, previous, prevExpired, err := parseConfig(src.Config, a.deps.Secrets)
	if err != nil {
		a.deps.Logger.Errorf(ctx, "[%s] parseConfig failed,err:%+v", where, err.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "internal error")
		return
	}

	// Decrypt current secret only — decrypt previous lazily on
	// fallback so the common path (valid signature against current)
	// stays a single decrypt round.
	curSecret, err := a.deps.Secrets.Decrypt(current)
	if err != nil {
		a.deps.Logger.Errorf(ctx, "[%s] decrypt current failed,err:%+v", where, err.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "internal error")
		return
	}

	ok := verifyHMAC(curSecret, ts, body, sig)
	if !ok && len(previous) > 0 && !prevExpired {
		prevSecret, decErr := a.deps.Secrets.Decrypt(previous)
		if decErr == nil {
			ok = verifyHMAC(prevSecret, ts, body, sig)
		}
	}
	if !ok {
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "auth_err")
		respond.Error(ctx, w, http.StatusUnauthorized, "unauthorized", "signature or timestamp invalid")
		return
	}

	var req attunev1.IngestRequest
	if err := ingestUnmarshal.Unmarshal(body, &req); err != nil {
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	meta := map[string]any{
		"inbound_source_id":   src.ID,
		"inbound_source_name": src.Name,
	}
	if m := req.GetSourceMeta(); m != nil {
		for k, v := range m.AsMap() {
			if _, reserved := meta[k]; !reserved {
				meta[k] = v
			}
		}
	}

	in := domain.IngestInput{
		Source:     channelName,
		Content:    req.GetContent(),
		SourceUser: req.GetSourceUser(),
		PageURL:    req.GetPageUrl(),
		SourceMeta: meta,
	}

	id, err := a.deps.Ingest.Ingest(ctx, src.TenantID, uuid.Nil, in)
	if err != nil {
		// We do not surface internal-vs-validation distinctions to the
		// caller here; the inbound code paths simply log + map onto
		// validate_err / internal_err.
		status, msg := http.StatusBadRequest, err.Error()
		result := "validate_err"
		if errors.Is(err, errInternal) {
			status, msg, result = http.StatusInternalServerError, "internal error", "internal_err"
		}
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, result)
		respond.Error(ctx, w, status, "ingest", msg)
		return
	}

	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "ok")
	if err := a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
		LastEventAt: ptrext.Of(nowFn()),
		LastUID:     src.State.LastUID,
	}); err != nil {
		a.deps.Logger.Warnf(ctx, "[%s] UpdateState failed,err:%+v", where, err.Error())
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,source_id:%s,feedback_id:%d",
		where, src.TenantID, src.ID, id)
	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.IngestResponse{
		Id:               id,
		EnrichmentStatus: "pending",
	}))
}

// errInternal — sentinel for distinguishing 500 vs 400 if upstream
// IngestPort wraps an internal error. We never construct this here; we
// just compare via errors.Is so the framework's caller can opt in.
var errInternal = errors.New("inbound.webhook: internal")
