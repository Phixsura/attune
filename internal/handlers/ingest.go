// Package handlers wires HTTP routes for attune.
package handlers

import (
	"context"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/respond"
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
	r.Post("/ingest", h.Ingest)
	return r
}

// ingestUnmarshal mirrors encoding/json's lenient decoding: protojson rejects
// unknown fields by default, so DiscardUnknown keeps clients that send extra
// fields working (proposal 2026-06-06-protobuf-idl-contract, Decision 6).
var ingestUnmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}

func (h *IngestHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	const where = "handlers.IngestHandler.Ingest"
	ctx := r.Context()
	tenantID, ok := apikey.TenantIDFromContext(ctx)
	if !ok {
		metrics.IngestTotal.WithLabelValues("unknown", "unknown", "auth_err").Inc()
		respond.Error(ctx, w, http.StatusUnauthorized, "unauthorized", "no tenant in context")
		return
	}
	keyID, _ := apikey.KeyIDFromContext(ctx)

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		metrics.IngestTotal.WithLabelValues(tenantID, "unknown", "validate_err").Inc()
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}
	var req attunev1.IngestRequest
	if err := ingestUnmarshal.Unmarshal(body, &req); err != nil {
		metrics.IngestTotal.WithLabelValues(tenantID, "unknown", "validate_err").Inc()
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	in := domain.IngestInput{
		Content:    req.GetContent(),
		Source:     req.GetSource(),
		SourceUser: req.GetSourceUser(),
		PageURL:    req.GetPageUrl(),
	}
	if m := req.GetSourceMeta(); m != nil {
		in.SourceMeta = m.AsMap()
	}
	if in.Source == "" {
		in.Source = "api"
	}

	id, err := h.ingestor.IngestRow(ctx, tenantID, keyID, in)
	if err != nil {
		// IngestRow currently only returns Validate()-style errors; map
		// uniformly to 400 + validate_err. If a future internal-error
		// path needs distinguishing, wire it through a typed error then.
		metrics.IngestTotal.WithLabelValues(tenantID, boundedSource(in.Source), "validate_err").Inc()
		respond.Error(ctx, w, http.StatusBadRequest, "validation", err.Error())
		return
	}

	metrics.IngestTotal.WithLabelValues(tenantID, in.Source, "ok").Inc()
	logext.Infof(ctx, "[%s] ingest accepted,inbound_trace_id:%s,tenant_id:%s,feedback_id:%d,source:%s",
		where, trace.FromContext(ctx), tenantID, id, in.Source)
	respond.Proto(w, http.StatusOK, ptrext.Of(attunev1.IngestResponse{
		Id:               id,
		EnrichmentStatus: "pending",
	}))
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
