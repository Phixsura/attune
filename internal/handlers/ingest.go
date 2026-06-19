// Package handlers wires HTTP routes for attune.
package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/ingest"
)

// ingestRower is the business-layer dependency the handler needs: validate +
// persist + fire enrichment. Declared here as a consumer-side interface so the
// handler can be unit-tested without a DB; *service.Ingestor satisfies it.
type ingestRower interface {
	IngestRow(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error)
}

// IngestHandler serves POST /v1/feedback/ingest.
type IngestHandler struct {
	ingestor ingestRower
}

func NewIngestHandler(ingestor ingestRower) *IngestHandler {
	return ptrext.Of(IngestHandler{ingestor: ingestor})
}

func (h *IngestHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/ingest", dispatcher.Bind(
		"handlers.IngestHandler.Ingest",
		dispatcher.Custom(func() *attunev1.IngestRequest { return ptrext.Of(attunev1.IngestRequest{}) }, BindIngestRequest),
		h.Ingest,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.IngestRequest) (*apikey.AuthCtx, error) {
			return apikey.FromContext(r.Context()), nil
		}),
	))
	return r
}

// ingestUnmarshal mirrors encoding/json's lenient decoding: protojson rejects
// unknown fields by default, so DiscardUnknown keeps clients that send extra
// fields working (proposal 2026-06-06-protobuf-idl-contract, Decision 6).
var ingestUnmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}

func BindIngestRequest(r *http.Request, req *attunev1.IngestRequest) error {
	tenantID, ok := apikey.TenantIDFromContext(r.Context())
	if !ok {
		tenantID = "unknown"
	}
	const maxBody = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		metrics.IngestTotal.WithLabelValues(tenantID, "unknown", "validate_err").Inc()
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid json body")
	}
	if len(body) > maxBody {
		// Mirror the dispatcher's standard decode path (bind.go), which returns
		// 413 BODY_TOO_LARGE for an over-limit body — not 400.
		metrics.IngestTotal.WithLabelValues(tenantID, "unknown", "validate_err").Inc()
		return dispatcher.NewError(http.StatusRequestEntityTooLarge, attunev1.ErrorCode_BODY_TOO_LARGE, "request body too large")
	}
	if err := ingestUnmarshal.Unmarshal(body, req); err != nil {
		metrics.IngestTotal.WithLabelValues(tenantID, "unknown", "validate_err").Inc()
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid json body")
	}
	return nil
}

func (h *IngestHandler) Ingest(ctx *dispatcher.RequestContext[*apikey.AuthCtx], req *attunev1.IngestRequest) (dispatcher.Result[*attunev1.IngestResponse], error) {
	const where = "handlers.IngestHandler.Ingest"
	tenantID := ctx.Auth.TenantID
	keyID := ctx.Auth.KeyID

	in := domain.IngestInput{
		Content:    req.GetContent(),
		Source:     req.GetSource(),
		SourceUser: req.GetSourceUser(),
		PageURL:    req.GetPageUrl(),
		// Optional client-supplied dedup token (header, not body) so a retry of
		// an at-least-once delivery cannot create a duplicate row.
		IdempotencyKey: ctx.Request().Header.Get("Idempotency-Key"),
	}
	if m := req.GetSourceMeta(); m != nil {
		in.SourceMeta = m.AsMap()
	}
	if in.Source == "" {
		in.Source = "api"
	}

	id, err := h.ingestor.IngestRow(ctx, tenantID, keyID, in)
	if err != nil {
		return h.ingestError(ctx, tenantID, in.Source, err)
	}

	metrics.IngestTotal.WithLabelValues(tenantID, in.Source, "ok").Inc()
	logext.Infof(ctx, "[%s] ingest accepted,inbound_trace_id:%s,tenant_id:%s,feedback_id:%d,source:%s",
		where, trace.FromContext(ctx), tenantID, id, in.Source)
	return dispatcher.OK(ptrext.Of(attunev1.IngestResponse{
		Id:               id,
		EnrichmentStatus: "pending",
	}))
}

// ingestError maps an IngestRow failure to the right HTTP status + error code.
// Idempotency outcomes are 409s; everything else is a 400 validation error.
func (h *IngestHandler) ingestError(
	ctx *dispatcher.RequestContext[*apikey.AuthCtx], tenantID, source string, err error,
) (dispatcher.Result[*attunev1.IngestResponse], error) {
	switch {
	case errors.Is(err, ingest.ErrIdempotencyConflict):
		metrics.IngestTotal.WithLabelValues(tenantID, boundedSource(source), "conflict").Inc()
		return dispatcher.Fail[*attunev1.IngestResponse](
			http.StatusConflict, attunev1.ErrorCode_IDEMPOTENCY_CONFLICT, err.Error())
	default:
		metrics.IngestTotal.WithLabelValues(tenantID, boundedSource(source), "validate_err").Inc()
		return dispatcher.Fail[*attunev1.IngestResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
}

// boundedSource keeps attune_ingest_total's `source` label bounded to known
// sources. On the validate_err path we'd otherwise record the raw client value,
// an unbounded-cardinality vector (proposal #6). Mirrors how the JSON-decode and
// auth paths record "unknown".
func boundedSource(s string) string {
	if domain.ValidSources[s] {
		return s
	}
	return "invalid"
}
