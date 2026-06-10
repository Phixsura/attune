// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Channel is the registered channel name for this adapter. Exported so
// the console handler (and other layers that need to refer to the
// channel by string) reuse a single definition (#66 review M7).
const Channel = "webhook"

const (
	channelName = Channel

	// maxBodyBytes — same 64 KiB cap as /v1/feedback/ingest. We bound
	// unauthenticated work cheaply (the cap runs before HMAC verify).
	maxBodyBytes = 64 * 1024

	hdrTimestamp = "X-Attune-Timestamp"
	hdrSignature = "X-Attune-Signature"

	// unknownLabel is the fixed Prometheus label value used on every
	// unauthenticated metric emission so an attacker spamming random
	// URL slugs cannot pump cardinality. Real tenant / source slugs
	// only enter label vectors after the source row is loaded.
	unknownLabel = "unknown"
)

// ingestUnmarshal — protojson decoder configured to tolerate unknown
// fields (mirrors /v1/feedback/ingest decoder for behavioural parity).
var ingestUnmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}

type webhookAuth struct {
	Source inbound.Source
	Start  time.Time
}

// bindRequest authenticates and decodes POST /v1/inbound/webhook/{tenant-slug}/{source-slug}.
// Spec: docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md
// §Webhook adapter.
func (a *adapter) bindRequest(r *http.Request, req *attunev1.IngestRequest) (webhookAuth, error) {
	ctx := r.Context()
	start := nowFn()

	tenantSlug := chi.URLParam(r, "tenant-slug")
	sourceSlug := chi.URLParam(r, "source-slug")
	ts := r.Header.Get(hdrTimestamp)
	sig := r.Header.Get(hdrSignature)

	// Read body BEFORE branching on the source lookup so timing does
	// not leak source existence. readCappedBody caps unauthenticated
	// work at maxBodyBytes.
	//
	// Metric labels for unauthenticated paths use the fixed "unknown"
	// sentinels — accepting the URL-supplied slugs into Prometheus
	// label vectors lets any client pump label cardinality to OOM
	// (review B4, #66). Authenticated paths (post GetBySlugs success
	// + src.Enabled) use the real tenant_id / slug below.
	body, err := readCappedBody(r.Body, maxBodyBytes)
	if err != nil {
		a.deps.Metrics.Total(channelName, unknownLabel, unknownLabel, "validate_err")
		if errors.Is(err, dispatcher.ErrBodyTooLarge) {
			return webhookAuth{}, err
		}
		return webhookAuth{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "body too large or unreadable")
	}

	src, srcErr := a.deps.Sources.GetBySlugs(ctx, tenantSlug, channelName, sourceSlug)
	if srcErr != nil || !src.Enabled {
		// Enumeration resistance per spec §Webhook adapter: verify
		// against the per-process stub secret so the response shape +
		// status match a real auth failure. (The spec asks for one
		// HMAC verify, not full-shape equivalence to the authenticated
		// path — we stay literal to keep the surface small.)
		_ = verifyHMACAgainstStub(ProcessStubSecret(), ts, body, sig)
		a.deps.Metrics.Total(channelName, unknownLabel, unknownLabel, "auth_err")
		return webhookAuth{}, dispatcher.NewError(http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "signature or timestamp invalid")
	}

	if status, code, msg, ok := a.authenticate(ctx, src, ts, body, sig); !ok {
		return webhookAuth{}, dispatcher.NewError(status, code, msg)
	}

	if err := ingestUnmarshal.Unmarshal(body, req); err != nil {
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		return webhookAuth{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid json body")
	}
	return webhookAuth{Source: src, Start: start}, nil
}

func (a *adapter) handle(ctx *dispatcher.RequestContext[webhookAuth], req *attunev1.IngestRequest) (dispatcher.Result[*attunev1.IngestResponse], error) {
	const where = "inbound.webhook.handle"
	auth := ctx.Auth
	src := auth.Source
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
		// service.Ingestor.IngestRow only returns Validate()-style
		// errors; map to 400 + validate_err uniformly.
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		return dispatcher.Fail[*attunev1.IngestResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}

	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "ok")
	a.deps.Metrics.Latency(channelName, src.TenantID, src.Slug, time.Since(auth.Start).Seconds())
	// Touch the per-source state gauge so an ops dashboard filtering by
	// {state="enabled"} reflects active sources — webhook is push-mode,
	// so we mark the source as enabled on every successful delivery.
	a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", true)
	if err := a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
		LastEventAt: ptrext.Of(nowFn()),
		LastUID:     src.State.LastUID,
	}); err != nil {
		logext.Warnf(ctx, "[%s] UpdateState failed,err:%+v", where, err.Error())
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,source_id:%s,feedback_id:%d",
		where, src.TenantID, src.ID, id)
	return dispatcher.OK(ptrext.Of(attunev1.IngestResponse{
		Id:               id,
		EnrichmentStatus: "pending",
	}))
}

func readCappedBody(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, dispatcher.ErrBodyTooLarge
	}
	return body, nil
}

// authenticate decodes the source's secret envelope and verifies the
// HMAC against current — falling back to the previous secret while it
// remains inside its rotation grace window. Returns (status, code,
// msg, ok); on ok=true the caller proceeds to ingest. Pulled out of
// handle() so the request flow stays inside the §1 CCN threshold.
func (a *adapter) authenticate(
	ctx context.Context,
	src inbound.Source,
	ts string, body []byte, sig string,
) (status int, code attunev1.ErrorCode, msg string, ok bool) {
	const where = "inbound.webhook.authenticate"
	current, previous, prevExpired, err := parseConfig(src.Config, a.deps.Secrets)
	if err != nil {
		logext.Errorf(ctx, "[%s] parseConfig failed,err:%+v", where, err.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		return http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "internal error", false
	}

	// Decrypt current secret only — decrypt previous lazily on
	// fallback so the common path (valid signature against current)
	// stays a single decrypt round.
	curSecret, err := a.deps.Secrets.Decrypt(current)
	if err != nil {
		logext.Errorf(ctx, "[%s] decrypt current failed,err:%+v", where, err.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		return http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "internal error", false
	}

	valid := verifyHMAC(curSecret, ts, body, sig)
	if !valid && len(previous) > 0 && !prevExpired {
		if prevSecret, decErr := a.deps.Secrets.Decrypt(previous); decErr == nil {
			valid = verifyHMAC(prevSecret, ts, body, sig)
		}
	}
	if !valid {
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "auth_err")
		return http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "signature or timestamp invalid", false
	}
	return 0, attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED, "", true
}
