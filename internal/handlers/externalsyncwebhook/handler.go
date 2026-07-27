// SPDX-License-Identifier: Apache-2.0

// Package externalsyncwebhook exposes signed provider webhook receivers for
// external sync connections.
package externalsyncwebhook

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/externalsync"
	svc "github.com/Phixsura/attune/internal/service/externalsync"
)

const maxWebhookBodyBytes = 1 << 20

type service interface {
	RecordGitHubWebhook(ctx context.Context, in svc.GitHubWebhookInput) (*repo.SyncEvent, error)
	RecordJiraWebhook(ctx context.Context, in svc.JiraWebhookInput) (*repo.SyncEvent, error)
}

type Handler struct {
	service service
}

func NewHandler(service service) *Handler {
	return ptrext.Of(Handler{service: service})
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/github/{tenant_id}/{connection_id}", h.GitHub)
	r.Post("/jira/{tenant_id}/{connection_id}", h.Jira)
	return r
}

func (h *Handler) GitHub(w http.ResponseWriter, r *http.Request) {
	const where = "handlers.ExternalSyncWebhookHandler.GitHub"
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	connectionID, err := uuid.Parse(chi.URLParam(r, "connection_id"))
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid connection id")
		return
	}
	body, err := readWebhookBody(r)
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "failed to read webhook body")
		return
	}
	if len(body) > maxWebhookBodyBytes {
		dispatcher.Reject(ctx, w, http.StatusRequestEntityTooLarge, attunev1.ErrorCode_BODY_TOO_LARGE, "webhook body exceeds the size limit")
		return
	}
	_, err = h.service.RecordGitHubWebhook(ctx, svc.GitHubWebhookInput{
		TenantID:        tenantID,
		ConnectionID:    connectionID,
		EventType:       r.Header.Get("X-GitHub-Event"),
		DeliveryID:      r.Header.Get("X-GitHub-Delivery"),
		SignatureSHA256: r.Header.Get("X-Hub-Signature-256"),
		Body:            body,
	})
	if err != nil {
		h.reject(ctx, w, where, "github", err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,connection_id:%s", where, tenantID, connectionID.String())
}

func (h *Handler) Jira(w http.ResponseWriter, r *http.Request) {
	const where = "handlers.ExternalSyncWebhookHandler.Jira"
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	connectionID, err := uuid.Parse(chi.URLParam(r, "connection_id"))
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid connection id")
		return
	}
	body, err := readWebhookBody(r)
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "failed to read webhook body")
		return
	}
	if len(body) > maxWebhookBodyBytes {
		dispatcher.Reject(ctx, w, http.StatusRequestEntityTooLarge, attunev1.ErrorCode_BODY_TOO_LARGE, "webhook body exceeds the size limit")
		return
	}
	_, err = h.service.RecordJiraWebhook(ctx, svc.JiraWebhookInput{
		TenantID:     tenantID,
		ConnectionID: connectionID,
		Signature:    r.Header.Get("X-Hub-Signature"),
		Body:         body,
	})
	if err != nil {
		h.reject(ctx, w, where, "jira", err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,connection_id:%s", where, tenantID, connectionID.String())
}

func readWebhookBody(r *http.Request) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	return io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes+1))
}

func (h *Handler) reject(ctx context.Context, w http.ResponseWriter, where, provider string, err error) {
	switch {
	case errors.Is(err, svc.ErrWebhookSignature):
		logext.Warnf(ctx, "[%s] reject: %s signature failed", where, provider)
		dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, provider+" webhook signature verification failed")
	case errors.Is(err, svc.ErrValidation):
		logext.Warnf(ctx, "[%s] reject: validation failed,err:%s", where, err.Error())
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	case errors.Is(err, repo.ErrConnectionNotFound):
		dispatcher.Reject(ctx, w, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "external sync connection not found")
	default:
		logext.Errorf(ctx, "[%s] failed,err:%+v", where, err.Error())
		dispatcher.Reject(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "external sync webhook failed")
	}
}
