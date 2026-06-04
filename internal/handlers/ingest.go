// Package handlers wires HTTP routes for listen.
package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/listen/internal/domain"
	"github.com/Phixsura/listen/internal/infra/apikey"
	"github.com/Phixsura/listen/internal/infra/metrics"
	"github.com/Phixsura/listen/internal/infra/trace"
	"github.com/Phixsura/listen/internal/service"
)

// IngestHandler serves POST /v1/feedback/ingest.
type IngestHandler struct {
	ingestor *service.Ingestor
}

func NewIngestHandler(ingestor *service.Ingestor) *IngestHandler {
	return &IngestHandler{ingestor: ingestor}
}

func (h *IngestHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/ingest", h.Ingest)
	return r
}

func (h *IngestHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID, ok := apikey.TenantIDFromContext(r.Context())
	if !ok {
		metrics.IngestTotal.WithLabelValues("unknown", "unknown", "auth_err").Inc()
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no tenant in context"})
		return
	}
	keyID, _ := apikey.KeyIDFromContext(r.Context())

	var in domain.IngestInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&in); err != nil {
		metrics.IngestTotal.WithLabelValues(tenantID, "unknown", "validate_err").Inc()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	if in.Source == "" {
		in.Source = "api"
	}

	id, err := h.ingestor.IngestRow(r.Context(), tenantID, keyID, in)
	if err != nil {
		var msg string
		var code int
		var result string
		// validate() errors are user-facing 400s; everything else is 500.
		switch {
		case errors.Is(err, errInternal):
			code, msg, result = http.StatusInternalServerError, "internal error", "internal_err"
		default:
			code, msg, result = http.StatusBadRequest, err.Error(), "validate_err"
		}
		metrics.IngestTotal.WithLabelValues(tenantID, in.Source, result).Inc()
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	metrics.IngestTotal.WithLabelValues(tenantID, in.Source, "ok").Inc()
	slog.InfoContext(ctx, "ingest accepted",
		"inbound_trace_id", trace.FromContext(r.Context()),
		"tenant_id", tenantID,
		"feedback_id", id,
		"source", in.Source)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":               id,
		"enrichmentStatus": "pending",
	})
}

// errInternal is a sentinel for IngestRow internal failures we want to map to 500.
// (Currently not produced; reserved for future per-error mapping.)
var errInternal = errors.New("internal")

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
