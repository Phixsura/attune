// SPDX-License-Identifier: Apache-2.0

// Package surveywebhook exposes signed provider webhook receivers for survey
// email delivery events.
package surveywebhook

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
	repo "github.com/Phixsura/attune/internal/repo/survey"
	svc "github.com/Phixsura/attune/internal/service/survey"
)

const maxProviderEventBodyBytes = 1 << 20

type service interface {
	RecordSignedProviderEvent(ctx context.Context, in svc.SignedProviderEventInput) (repo.Invitation, error)
}

type Handler struct {
	service service
}

func NewHandler(service service) *Handler {
	return ptrext.Of(Handler{service: service})
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/{tenant_id}/{sender_id}", h.Record)
	return r
}

func (h *Handler) Record(w http.ResponseWriter, r *http.Request) {
	const where = "handlers.SurveyProviderWebhookHandler.Record"
	ctx := r.Context()
	senderID, err := uuid.Parse(chi.URLParam(r, "sender_id"))
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid sender id")
		return
	}
	body, err := readProviderEventBody(r)
	if err != nil {
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "failed to read provider event body")
		return
	}
	if len(body) > maxProviderEventBodyBytes {
		dispatcher.Reject(ctx, w, http.StatusRequestEntityTooLarge, attunev1.ErrorCode_BODY_TOO_LARGE, "provider event body exceeds the size limit")
		return
	}
	if h.service == nil {
		dispatcher.Reject(ctx, w, http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "survey provider webhooks not configured")
		return
	}
	_, err = h.service.RecordSignedProviderEvent(ctx, svc.SignedProviderEventInput{
		TenantID:  chi.URLParam(r, "tenant_id"),
		SenderID:  senderID,
		Timestamp: r.Header.Get(svc.ProviderWebhookTimestampHeader),
		Signature: r.Header.Get(svc.ProviderWebhookSignatureSHA256),
		RawBody:   body,
	})
	if err != nil {
		h.reject(ctx, w, where, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,sender_id:%s", where, chi.URLParam(r, "tenant_id"), senderID.String())
}

func readProviderEventBody(r *http.Request) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	return io.ReadAll(io.LimitReader(r.Body, maxProviderEventBodyBytes+1))
}

func (h *Handler) reject(ctx context.Context, w http.ResponseWriter, where string, err error) {
	switch {
	case errors.Is(err, svc.ErrWebhookSignature):
		logext.Warnf(ctx, "[%s] reject: survey provider signature failed", where)
		dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "survey provider webhook signature verification failed")
	case errors.Is(err, svc.ErrValidation):
		logext.Warnf(ctx, "[%s] reject: validation failed", where)
		dispatcher.Reject(ctx, w, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid survey provider event")
	case errors.Is(err, svc.ErrDisabled):
		logext.Warnf(ctx, "[%s] reject: webhook secret missing", where)
		dispatcher.Reject(ctx, w, http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "survey provider webhook unavailable")
	case errors.Is(err, svc.ErrNotFound):
		dispatcher.Reject(ctx, w, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "survey provider webhook not found")
	default:
		logext.Errorf(ctx, "[%s] failed,err:%+v", where, err.Error())
		dispatcher.Reject(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "survey provider webhook failed")
	}
}
