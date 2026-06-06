// Package handlers wires HTTP routes for attune.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/trace"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
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
	return &IngestHandler{ingestor: ingestor}
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
	ctx := r.Context()
	tenantID, ok := apikey.TenantIDFromContext(ctx)
	if !ok {
		metrics.IngestTotal.WithLabelValues("unknown", "unknown", "auth_err").Inc()
		writeError(ctx, w, http.StatusUnauthorized, "unauthorized", "no tenant in context")
		return
	}
	keyID, _ := apikey.KeyIDFromContext(ctx)

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		metrics.IngestTotal.WithLabelValues(tenantID, "unknown", "validate_err").Inc()
		writeError(ctx, w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}
	var req attunev1.IngestRequest
	if err := ingestUnmarshal.Unmarshal(body, &req); err != nil {
		metrics.IngestTotal.WithLabelValues(tenantID, "unknown", "validate_err").Inc()
		writeError(ctx, w, http.StatusBadRequest, "bad_request", "invalid json body")
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
		// validate() errors are user-facing 400s; everything else is 500.
		status, code, message, result := http.StatusBadRequest, "validation", err.Error(), "validate_err"
		if errors.Is(err, errInternal) {
			status, code, message, result = http.StatusInternalServerError, "internal", "internal error", "internal_err"
		}
		metrics.IngestTotal.WithLabelValues(tenantID, boundedSource(in.Source), result).Inc()
		writeError(ctx, w, status, code, message)
		return
	}

	metrics.IngestTotal.WithLabelValues(tenantID, in.Source, "ok").Inc()
	slog.InfoContext(ctx, "ingest accepted",
		"inbound_trace_id", trace.FromContext(ctx),
		"tenant_id", tenantID,
		"feedback_id", id,
		"source", in.Source)
	writeJSONProto(w, http.StatusOK, &attunev1.IngestResponse{
		Id:               id,
		EnrichmentStatus: "pending",
	})
}

// errInternal is a sentinel for IngestRow internal failures we want to map to 500.
// (Currently not produced; reserved for future per-error mapping.)
var errInternal = errors.New("internal")

// writeJSON encodes an arbitrary body as JSON. Still used by the Lark adapter
// handler (lark.go), which is intentionally NOT on the proto contract (#66).
func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// writeJSONProto encodes a proto message as protoJSON — the wire contract for
// the migrated endpoints (#19).
func writeJSONProto(w http.ResponseWriter, code int, m proto.Message) {
	b, err := protojson.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(b)
}

// writeError emits the unified ErrorResponse {code, message, requestId} used by
// every proto-migrated endpoint (#19). request_id comes from the chi RequestID
// middleware (X-Request-ID) for support / log correlation.
func writeError(ctx context.Context, w http.ResponseWriter, status int, code, message string) {
	writeJSONProto(w, status, &attunev1.ErrorResponse{
		Code:      code,
		Message:   message,
		RequestId: middleware.GetReqID(ctx),
	})
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
