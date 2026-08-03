// SPDX-License-Identifier: Apache-2.0

package surveywebhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
	svc "github.com/Phixsura/attune/internal/service/survey"
)

func TestRecordAcceptsSignedProviderEvent(t *testing.T) {
	t.Parallel()

	service := ptrext.Of(fakeService{})
	handler := NewHandler(service)
	senderID := uuid.New()
	body := `{"provider_event_type":"bounce","provider_message_id":"message-1"}`
	req := routeRequest(httptest.NewRequest(http.MethodPost, "/tenant-1/"+senderID.String(), strings.NewReader(body)), map[string]string{
		"tenant_id": "tenant-1",
		"sender_id": senderID.String(),
	})
	req.Header.Set(svc.ProviderWebhookTimestampHeader, "2026-07-30T12:00:00Z")
	req.Header.Set(svc.ProviderWebhookSignatureSHA256, "sha256=abc")
	rec := httptest.NewRecorder()

	handler.Record(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s; want 202", rec.Code, rec.Body.String())
	}
	if service.input.TenantID != "tenant-1" || service.input.SenderID != senderID ||
		service.input.Timestamp != "2026-07-30T12:00:00Z" || service.input.Signature != "sha256=abc" ||
		string(service.input.RawBody) != body {
		t.Fatalf("service input = %#v; want signed provider event", service.input)
	}
}

func TestRoutesDispatchesProviderEvent(t *testing.T) {
	t.Parallel()

	service := ptrext.Of(fakeService{})
	handler := NewHandler(service)
	senderID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/tenant-1/"+senderID.String(), strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s; want 202", rec.Code, rec.Body.String())
	}
	if service.input.TenantID != "tenant-1" || service.input.SenderID != senderID {
		t.Fatalf("service input = %#v; want route params", service.input)
	}
}

func TestRecordRejectsMalformedSenderAndOversizedBodies(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		senderID   string
		body       string
		wantStatus int
	}{
		{name: "bad sender", senderID: "bad", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "too large", senderID: uuid.NewString(), body: strings.Repeat("x", maxProviderEventBodyBytes+1), wantStatus: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := NewHandler(ptrext.Of(fakeService{}))
			req := routeRequest(httptest.NewRequest(http.MethodPost, "/tenant-1/"+tt.senderID, strings.NewReader(tt.body)), map[string]string{
				"tenant_id": "tenant-1",
				"sender_id": tt.senderID,
			})
			rec := httptest.NewRecorder()

			handler.Record(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d body = %s; want %d", rec.Code, rec.Body.String(), tt.wantStatus)
			}
		})
	}
}

func TestRecordMapsServiceErrors(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "signature", err: svc.ErrWebhookSignature, wantStatus: http.StatusUnauthorized},
		{name: "validation", err: svc.ErrValidation, wantStatus: http.StatusBadRequest},
		{name: "disabled", err: svc.ErrDisabled, wantStatus: http.StatusForbidden},
		{name: "not found", err: svc.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "internal", err: errors.New("database down"), wantStatus: http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := NewHandler(ptrext.Of(fakeService{err: tt.err}))
			senderID := uuid.NewString()
			req := routeRequest(httptest.NewRequest(http.MethodPost, "/tenant-1/"+senderID, strings.NewReader(`{}`)), map[string]string{
				"tenant_id": "tenant-1",
				"sender_id": senderID,
			})
			rec := httptest.NewRecorder()

			handler.Record(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d body = %s; want %d", rec.Code, rec.Body.String(), tt.wantStatus)
			}
		})
	}
}

type fakeService struct {
	input svc.SignedProviderEventInput
	err   error
}

func (s *fakeService) RecordSignedProviderEvent(_ context.Context, in svc.SignedProviderEventInput) (repo.Invitation, error) {
	s.input = in
	if s.err != nil {
		return repo.Invitation{}, s.err
	}
	return repo.Invitation{ID: uuid.New()}, nil
}

func routeRequest(req *http.Request, params map[string]string) *http.Request {
	ctx := chi.NewRouteContext()
	for key, value := range params {
		ctx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
}
