// SPDX-License-Identifier: Apache-2.0

package externalsyncwebhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/externalsync"
	svc "github.com/Phixsura/attune/internal/service/externalsync"
)

func TestGitHubAcceptsSignedDelivery(t *testing.T) {
	service := ptrext.Of(fakeService{})
	handler := NewHandler(service)
	connectionID := uuid.New()
	body := []byte(`{"zen":"Approachable is better than simple."}`)
	req := routeRequest(httptest.NewRequest(http.MethodPost, "/github/tenant-1/"+connectionID.String(), bytes.NewReader(body)), map[string]string{
		"tenant_id":     "tenant-1",
		"connection_id": connectionID.String(),
	})
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", testGitHubSignature([]byte("secret"), body))
	rec := httptest.NewRecorder()

	handler.GitHub(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s; want 202", rec.Code, rec.Body.String())
	}
	if service.input.TenantID != "tenant-1" || service.input.ConnectionID != connectionID ||
		service.input.EventType != "ping" || service.input.DeliveryID != "delivery-1" ||
		string(service.input.Body) != string(body) {
		t.Fatalf("service input = %#v; want routed GitHub delivery", service.input)
	}
}

func TestRoutesDispatchesGitHubWebhook(t *testing.T) {
	service := ptrext.Of(fakeService{})
	handler := NewHandler(service)
	connectionID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/github/tenant-1/"+connectionID.String(), strings.NewReader(`{}`))
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s; want 202", rec.Code, rec.Body.String())
	}
	if service.input.TenantID != "tenant-1" || service.input.ConnectionID != connectionID ||
		service.input.EventType != "ping" {
		t.Fatalf("service input = %#v; want routed request", service.input)
	}
}

func TestGitHubRejectsMalformedConnectionID(t *testing.T) {
	handler := NewHandler(ptrext.Of(fakeService{}))
	req := routeRequest(httptest.NewRequest(http.MethodPost, "/github/tenant-1/bad", strings.NewReader(`{}`)), map[string]string{
		"tenant_id":     "tenant-1",
		"connection_id": "bad",
	})
	rec := httptest.NewRecorder()

	handler.GitHub(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s; want 400", rec.Code, rec.Body.String())
	}
}

func TestGitHubRejectsUnreadableAndOversizedBodies(t *testing.T) {
	tests := []struct {
		name       string
		body       ioReadCloser
		wantStatus int
	}{
		{name: "read error", body: failingBody{}, wantStatus: http.StatusBadRequest},
		{name: "too large", body: sizedBody{Reader: strings.NewReader(strings.Repeat("x", maxWebhookBodyBytes+1))}, wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(ptrext.Of(fakeService{}))
			connectionID := uuid.New()
			req := routeRequest(httptest.NewRequest(http.MethodPost, "/github/tenant-1/"+connectionID.String(), tt.body), map[string]string{
				"tenant_id":     "tenant-1",
				"connection_id": connectionID.String(),
			})
			rec := httptest.NewRecorder()

			handler.GitHub(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d body = %s; want %d", rec.Code, rec.Body.String(), tt.wantStatus)
			}
		})
	}
}

func TestGitHubSignatureFailureMapsUnauthorized(t *testing.T) {
	service := ptrext.Of(fakeService{err: svc.ErrWebhookSignature})
	handler := NewHandler(service)
	connectionID := uuid.New()
	req := routeRequest(httptest.NewRequest(http.MethodPost, "/github/tenant-1/"+connectionID.String(), bytes.NewReader([]byte(`{}`))), map[string]string{
		"tenant_id":     "tenant-1",
		"connection_id": connectionID.String(),
	})
	rec := httptest.NewRecorder()

	handler.GitHub(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s; want 401", rec.Code, rec.Body.String())
	}
}

func TestGitHubServiceErrorsMapToResponses(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "validation", err: svc.ErrValidation, wantStatus: http.StatusBadRequest},
		{name: "missing connection", err: repo.ErrConnectionNotFound, wantStatus: http.StatusNotFound},
		{name: "internal", err: errors.New("database down"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := ptrext.Of(fakeService{err: tt.err})
			handler := NewHandler(service)
			connectionID := uuid.New()
			req := routeRequest(httptest.NewRequest(http.MethodPost, "/github/tenant-1/"+connectionID.String(), strings.NewReader(`{}`)), map[string]string{
				"tenant_id":     "tenant-1",
				"connection_id": connectionID.String(),
			})
			rec := httptest.NewRecorder()

			handler.GitHub(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d body = %s; want %d", rec.Code, rec.Body.String(), tt.wantStatus)
			}
		})
	}
}

func TestJiraAcceptsSignedDelivery(t *testing.T) {
	service := ptrext.Of(fakeService{})
	handler := NewHandler(service)
	connectionID := uuid.New()
	body := []byte(`{"webhookEvent":"jira:issue_updated","timestamp":1710000000000,"issue":{"id":"10001","key":"ABC-1","fields":{"summary":"Sync me","status":{"name":"In Progress"}}},"changelog":{"id":"200","items":[{"field":"status","fromString":"To Do","toString":"In Progress"}]},"user":{"accountId":"acc-1","displayName":"Alice"}}`)
	req := routeRequest(httptest.NewRequest(http.MethodPost, "/jira/tenant-1/"+connectionID.String(), bytes.NewReader(body)), map[string]string{
		"tenant_id":     "tenant-1",
		"connection_id": connectionID.String(),
	})
	req.Header.Set("X-Hub-Signature", testGitHubSignature([]byte("secret"), body))
	rec := httptest.NewRecorder()

	handler.Jira(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s; want 202", rec.Code, rec.Body.String())
	}
	if service.jiraInput.TenantID != "tenant-1" || service.jiraInput.ConnectionID != connectionID ||
		service.jiraInput.Signature == "" || string(service.jiraInput.Body) != string(body) {
		t.Fatalf("service input = %#v; want routed Jira delivery", service.jiraInput)
	}
}

func TestRoutesDispatchesJiraWebhook(t *testing.T) {
	service := ptrext.Of(fakeService{})
	handler := NewHandler(service)
	connectionID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/jira/tenant-1/"+connectionID.String(), strings.NewReader(`{}`))
	req.Header.Set("X-Hub-Signature", testGitHubSignature([]byte("secret"), []byte(`{}`)))
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s; want 202", rec.Code, rec.Body.String())
	}
	if service.jiraInput.TenantID != "tenant-1" || service.jiraInput.ConnectionID != connectionID {
		t.Fatalf("service input = %#v; want routed Jira request", service.jiraInput)
	}
}

func TestJiraSignatureFailureMapsUnauthorized(t *testing.T) {
	service := ptrext.Of(fakeService{err: svc.ErrWebhookSignature})
	handler := NewHandler(service)
	connectionID := uuid.New()
	req := routeRequest(httptest.NewRequest(http.MethodPost, "/jira/tenant-1/"+connectionID.String(), bytes.NewReader([]byte(`{}`))), map[string]string{
		"tenant_id":     "tenant-1",
		"connection_id": connectionID.String(),
	})
	req.Header.Set("X-Hub-Signature", testGitHubSignature([]byte("secret"), []byte(`{}`)))
	rec := httptest.NewRecorder()

	handler.Jira(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s; want 401", rec.Code, rec.Body.String())
	}
}

type fakeService struct {
	input     svc.GitHubWebhookInput
	jiraInput svc.JiraWebhookInput
	err       error
}

func (s *fakeService) RecordGitHubWebhook(_ context.Context, in svc.GitHubWebhookInput) (*repo.SyncEvent, error) {
	s.input = in
	if s.err != nil {
		return nil, s.err
	}
	return ptrext.Of(repo.SyncEvent{}), nil
}

func (s *fakeService) RecordJiraWebhook(_ context.Context, in svc.JiraWebhookInput) (*repo.SyncEvent, error) {
	s.jiraInput = in
	if s.err != nil {
		return nil, s.err
	}
	return ptrext.Of(repo.SyncEvent{}), nil
}

func routeRequest(req *http.Request, params map[string]string) *http.Request {
	ctx := chi.NewRouteContext()
	for key, value := range params {
		ctx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
}

func testGitHubSignature(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type ioReadCloser interface {
	Read([]byte) (int, error)
	Close() error
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (failingBody) Close() error {
	return nil
}

type sizedBody struct {
	*strings.Reader
}

func (sizedBody) Close() error {
	return nil
}
