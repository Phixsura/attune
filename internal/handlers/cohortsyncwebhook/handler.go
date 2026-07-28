// SPDX-License-Identifier: Apache-2.0

// Package cohortsyncwebhook exposes webhook receivers for cohort sync
// providers (Amplitude list-based, Mixpanel custom webhook).
package cohortsyncwebhook

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/cohortsync"
	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/cohortsync"
	svc "github.com/Phixsura/attune/internal/service/cohortsync"
)

const maxCohortWebhookBodyBytes = 32 << 20 // 32 MB

type service interface {
	GetSource(ctx context.Context, tenantID string, id uuid.UUID) (*repo.Source, error)
	DecryptCredential(source repo.Source) ([]byte, error)
	ApplyDelta(ctx context.Context, tenantID string, sourceID uuid.UUID, payload cohortsync.SyncPayload) (*svc.SyncRunResult, error)
	ApplyFullSnapshot(ctx context.Context, tenantID string, sourceID uuid.UUID, payload cohortsync.SyncPayload, trigger string) (*svc.SyncRunResult, error)
	RecordEvent(ctx context.Context, in repo.SyncEvent) (*repo.SyncEvent, error)
	UpdateEventStatus(ctx context.Context, id uuid.UUID, status string, runID *uuid.UUID, failureReason string) error
}

// Handler is the cohort sync webhook receiver.
type Handler struct {
	service service
}

// NewHandler builds a cohort sync webhook handler.
func NewHandler(service service) *Handler {
	return ptrext.Of(Handler{service: service})
}

// Routes returns the chi router for cohort sync webhooks.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/amplitude/{tenant_id}/{source_id}/create", h.amplitude)
	r.Post("/amplitude/{tenant_id}/{source_id}/add", h.amplitude)
	r.Post("/amplitude/{tenant_id}/{source_id}/remove", h.amplitude)
	r.Post("/mixpanel/{tenant_id}/{source_id}", h.mixpanel)
	return r
}

func (h *Handler) amplitude(w http.ResponseWriter, r *http.Request) {
	const where = "handlers.CohortSyncWebhookHandler.Amplitude"
	ctx := r.Context()

	tenantID, sourceID, ok := parsePath(ctx, w, r)
	if !ok {
		return
	}

	// Authenticate BEFORE reading the body to prevent unauthenticated
	// callers from forcing 32MB allocations (DoS mitigation).
	source, credential, ok := h.authenticateSource(ctx, w, r, where, tenantID, sourceID, "amplitude")
	if !ok {
		return
	}
	_ = credential // Amplitude uses basic auth; verification happens in authenticateSource

	body, ok := readBody(ctx, w, r, where)
	if !ok {
		return
	}

	provider, providerOK := cohortsync.Lookup("amplitude")
	if !providerOK {
		rejectInternal(ctx, w, where, "amplitude adapter not registered")
		return
	}

	// Pass operation from URL path suffix as a header hint.
	operation := lastPathSegment(r.URL.Path)
	headers := map[string]string{"x-operation": operation}

	payload, err := provider.ParseWebhook(body, headers, nil)
	if err != nil {
		logext.Warnf(ctx, "[%s] parse failed,err:%s", where, err.Error())
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
		return
	}

	h.applyPayload(ctx, w, where, tenantID, source.ID, payload, body)
}

func (h *Handler) mixpanel(w http.ResponseWriter, r *http.Request) {
	const where = "handlers.CohortSyncWebhookHandler.Mixpanel"
	ctx := r.Context()

	tenantID, sourceID, ok := parsePath(ctx, w, r)
	if !ok {
		return
	}

	source, _, ok := h.authenticateSource(ctx, w, r, where, tenantID, sourceID, "mixpanel")
	if !ok {
		return
	}

	body, ok := readBody(ctx, w, r, where)
	if !ok {
		return
	}

	provider, providerOK := cohortsync.Lookup("mixpanel")
	if !providerOK {
		rejectInternal(ctx, w, where, "mixpanel adapter not registered")
		return
	}

	payload, err := provider.ParseWebhook(body, nil, nil)
	if err != nil {
		logext.Warnf(ctx, "[%s] parse failed,err:%s", where, err.Error())
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
		return
	}

	h.applyPayload(ctx, w, where, tenantID, source.ID, payload, body)
}

func (h *Handler) applyPayload(ctx context.Context, w http.ResponseWriter, where, tenantID string, sourceID uuid.UUID, payload cohortsync.SyncPayload, body []byte) {
	// Dedup: record the event before processing. If the dedupe_key already
	// exists, return 200 OK without re-processing (Amplitude retries are safe).
	dedupeKey := payload.Provider + ":" + payload.ExternalCohortID + ":" + repo.EventPayloadDigest(body)
	event, err := h.service.RecordEvent(ctx, repo.SyncEvent{
		TenantID:       tenantID,
		CohortSourceID: sourceID,
		Provider:       payload.Provider,
		EventType:      eventType(payload),
		DedupeKey:      dedupeKey,
		Status:         "received",
		PayloadDigest:  repo.EventPayloadDigest(body),
		MembersCount:   len(payload.Deltas),
	})
	if errors.Is(err, repo.ErrDuplicateEvent) {
		metrics.CohortSyncWebhookRequestsTotal.WithLabelValues(payload.Provider, "duplicate").Inc()
		w.WriteHeader(http.StatusOK) // idempotent: already processed
		logext.Infof(ctx, "[%s] duplicate event,tenant_id:%s,dedupe_key:%s", where, tenantID, dedupeKey)
		return
	}
	if err != nil {
		rejectInternal(ctx, w, where, err.Error())
		return
	}

	var applyErr error
	var result *svc.SyncRunResult
	if payload.IsFullSnapshot {
		result, applyErr = h.service.ApplyFullSnapshot(ctx, tenantID, sourceID, payload, "webhook")
	} else {
		result, applyErr = h.service.ApplyDelta(ctx, tenantID, sourceID, payload)
	}
	if applyErr != nil {
		metrics.CohortSyncWebhookRequestsTotal.WithLabelValues(payload.Provider, "error").Inc()
		if statusErr := h.service.UpdateEventStatus(ctx, event.ID, "failed", nil, applyErr.Error()); statusErr != nil {
			logext.Warnf(ctx, "[%s] event status update failed,event_id:%s,err:%s", where, event.ID.String(), statusErr.Error())
		}
		h.reject(ctx, w, where, applyErr)
		return
	}

	var runID *uuid.UUID
	if result != nil {
		runID = ptrext.Of(result.Run.ID)
	}
	if statusErr := h.service.UpdateEventStatus(ctx, event.ID, "processed", runID, ""); statusErr != nil {
		logext.Warnf(ctx, "[%s] event status update failed,event_id:%s,err:%s", where, event.ID.String(), statusErr.Error())
	}
	metrics.CohortSyncWebhookRequestsTotal.WithLabelValues(payload.Provider, "ok").Inc()
	w.WriteHeader(http.StatusOK)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,source_id:%s,cohort:%s,deltas:%d",
		where, tenantID, sourceID.String(), payload.ExternalCohortID, len(payload.Deltas))
}

func (h *Handler) authenticateSource(ctx context.Context, w http.ResponseWriter, r *http.Request, where, tenantID string, sourceID uuid.UUID, expectedProvider string) (*repo.Source, []byte, bool) {
	source, err := h.service.GetSource(ctx, tenantID, sourceID)
	if err != nil {
		if errors.Is(err, repo.ErrSourceNotFound) {
			dispatcher.Reject(ctx, w, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "cohort source not found")
			return nil, nil, false
		}
		rejectInternal(ctx, w, where, err.Error())
		return nil, nil, false
	}
	if source.Provider != expectedProvider {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "provider mismatch")
		return nil, nil, false
	}
	// Early exit for disabled sources: accept the webhook (200 OK) without
	// reading the body or processing, to avoid wasting resources on paused
	// integrations. The provider sees success and does not retry.
	if !source.Enabled {
		logext.Infof(ctx, "[%s] source disabled,tenant_id:%s,source_id:%s", where, tenantID, sourceID.String())
		w.WriteHeader(http.StatusOK)
		return nil, nil, false
	}

	credential, err := h.service.DecryptCredential(ptrext.Indirect(source))
	if err != nil {
		rejectInternal(ctx, w, where, "credential decrypt failed")
		return nil, nil, false
	}

	// Verify basic auth: the api key is the username, password is empty.
	// Use constant-time comparison to prevent timing attacks.
	username, _, _ := r.BasicAuth()
	if username == "" || subtle.ConstantTimeCompare([]byte(username), credential) != 1 {
		logext.Warnf(ctx, "[%s] auth failed,tenant_id:%s,source_id:%s", where, tenantID, sourceID.String())
		dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "authentication failed")
		return nil, nil, false
	}

	return source, credential, true
}

func parsePath(ctx context.Context, w http.ResponseWriter, r *http.Request) (string, uuid.UUID, bool) {
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "tenant_id is required")
		return "", uuid.Nil, false
	}
	sourceIDStr := chi.URLParam(r, "source_id")
	sourceID, err := uuid.Parse(sourceIDStr)
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid source id")
		return "", uuid.Nil, false
	}
	return tenantID, sourceID, true
}

func readBody(ctx context.Context, w http.ResponseWriter, r *http.Request, where string) ([]byte, bool) {
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCohortWebhookBodyBytes+1))
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "failed to read body")
		return nil, false
	}
	if len(body) > maxCohortWebhookBodyBytes {
		logext.Warnf(ctx, "[%s] body too large: %d bytes", where, len(body))
		dispatcher.Reject(ctx, w, http.StatusRequestEntityTooLarge, attunev1.ErrorCode_BODY_TOO_LARGE, "body exceeds 32MB limit")
		return nil, false
	}
	return body, true
}

func eventType(payload cohortsync.SyncPayload) string {
	if payload.IsFullSnapshot {
		return "full_snapshot"
	}
	return "incremental"
}

func lastPathSegment(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func (h *Handler) reject(ctx context.Context, w http.ResponseWriter, where string, err error) {
	switch {
	case errors.Is(err, svc.ErrValidation):
		logext.Warnf(ctx, "[%s] validation failed,err:%s", where, err.Error())
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	case errors.Is(err, repo.ErrSourceNotFound), errors.Is(err, repo.ErrCohortNotFound):
		dispatcher.Reject(ctx, w, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, err.Error())
	case errors.Is(err, repo.ErrConflict):
		dispatcher.Reject(ctx, w, http.StatusConflict, attunev1.ErrorCode_CONFLICT, err.Error())
	default:
		rejectInternal(ctx, w, where, err.Error())
	}
}

func rejectInternal(ctx context.Context, w http.ResponseWriter, where, msg string) {
	logext.Errorf(ctx, "[%s] internal error: %s", where, msg)
	dispatcher.Reject(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "cohort sync webhook failed")
}
