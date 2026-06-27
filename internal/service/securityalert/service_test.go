// SPDX-License-Identifier: Apache-2.0

package securityalert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewService_EmptyURL(t *testing.T) {
	svc := NewService("")
	if svc == nil {
		t.Fatal("NewService should not return nil")
	}

	// Should not panic, just log
	svc.Send(context.Background(), Alert{
		Type:    AlertBreakGlassUsed,
		Summary: "test",
	})
}

func TestService_Send_WebhookCalled(t *testing.T) {
	var called atomic.Bool
	var mu sync.Mutex
	var receivedAlert Alert

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}
		if ua := r.Header.Get("User-Agent"); ua != "attune-security-alert/1.0" {
			t.Errorf("expected User-Agent attune-security-alert/1.0, got %s", ua)
		}

		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		if err := json.Unmarshal(body, &receivedAlert); err != nil {
			t.Errorf("failed to unmarshal alert: %v", err)
		}
		mu.Unlock()

		called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	svc := NewService(server.URL)

	alert := Alert{
		Type:     AlertBreakGlassUsed,
		TenantID: "tenant-1",
		Actor:    "admin@example.com",
		Summary:  "test alert",
		Details:  map[string]string{"key": "value"},
	}

	svc.Send(context.Background(), alert)

	// Wait for async send
	time.Sleep(100 * time.Millisecond)

	if !called.Load() {
		t.Error("webhook was not called")
	}
	mu.Lock()
	defer mu.Unlock()
	if receivedAlert.Type != AlertBreakGlassUsed {
		t.Errorf("Type = %s, want %s", receivedAlert.Type, AlertBreakGlassUsed)
	}
	if receivedAlert.TenantID != "tenant-1" {
		t.Errorf("TenantID = %s, want tenant-1", receivedAlert.TenantID)
	}
	if receivedAlert.Actor != "admin@example.com" {
		t.Errorf("Actor = %s, want admin@example.com", receivedAlert.Actor)
	}
}

func TestBreakGlassUsedAlert(t *testing.T) {
	alert := BreakGlassUsedAlert("tenant-1", "admin@example.com", "token-123", "192.168.1.100", "Mozilla/5.0")

	if alert.Type != AlertBreakGlassUsed {
		t.Errorf("Type = %s, want %s", alert.Type, AlertBreakGlassUsed)
	}
	if alert.TenantID != "tenant-1" {
		t.Errorf("TenantID = %s, want tenant-1", alert.TenantID)
	}
	if alert.Actor != "admin@example.com" {
		t.Errorf("Actor = %s, want admin@example.com", alert.Actor)
	}
	if alert.Details["token_id"] != "token-123" {
		t.Errorf("Details[token_id] = %s, want token-123", alert.Details["token_id"])
	}
	if alert.Details["client_ip"] != "192.168.1.100" {
		t.Errorf("Details[client_ip] = %s, want 192.168.1.100", alert.Details["client_ip"])
	}
}

func TestBreakGlassIssuedAlert(t *testing.T) {
	alert := BreakGlassIssuedAlert("tenant-1", "issuer@example.com", "admin@example.com", "token-456", 30*time.Minute)

	if alert.Type != AlertBreakGlassIssued {
		t.Errorf("Type = %s, want %s", alert.Type, AlertBreakGlassIssued)
	}
	if alert.Actor != "issuer@example.com" {
		t.Errorf("Actor = %s, want issuer@example.com", alert.Actor)
	}
	if alert.Details["for_admin"] != "admin@example.com" {
		t.Errorf("Details[for_admin] = %s, want admin@example.com", alert.Details["for_admin"])
	}
	if alert.Details["token_id"] != "token-456" {
		t.Errorf("Details[token_id] = %s, want token-456", alert.Details["token_id"])
	}
}

func TestSSOCutoverAlert(t *testing.T) {
	alert := SSOCutoverAlert("tenant-1", "admin@example.com", "hybrid", "sso_only")

	if alert.Type != AlertSSOCutover {
		t.Errorf("Type = %s, want %s", alert.Type, AlertSSOCutover)
	}
	if alert.Details["old_mode"] != "hybrid" {
		t.Errorf("Details[old_mode] = %s, want hybrid", alert.Details["old_mode"])
	}
	if alert.Details["new_mode"] != "sso_only" {
		t.Errorf("Details[new_mode] = %s, want sso_only", alert.Details["new_mode"])
	}
}

func TestService_Send_WebhookErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	svc := NewService(server.URL)

	// Should not panic on 5xx response
	svc.Send(context.Background(), Alert{
		Type:    AlertBreakGlassUsed,
		Summary: "test",
	})

	// Wait for async send
	time.Sleep(100 * time.Millisecond)
}

func TestService_Send_SetsTimestamp(t *testing.T) {
	var mu sync.Mutex
	var receivedAlert Alert

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(body, &receivedAlert)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	svc := NewService(server.URL)

	// Send with zero timestamp
	svc.Send(context.Background(), Alert{
		Type:    AlertBreakGlassUsed,
		Summary: "test",
	})

	// Wait for async send
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if receivedAlert.Timestamp.IsZero() {
		t.Error("Timestamp should be set automatically")
	}
}

func TestService_Send_InvalidURL(t *testing.T) {
	svc := NewService("http://invalid.local.test:99999")

	// Should not panic on connection error
	svc.Send(context.Background(), Alert{
		Type:    AlertBreakGlassUsed,
		Summary: "test",
	})

	// Wait for async send attempt
	time.Sleep(100 * time.Millisecond)
}

func TestAlertType_Constants(t *testing.T) {
	if AlertBreakGlassUsed != "breakglass.used" {
		t.Errorf("AlertBreakGlassUsed = %s, want breakglass.used", AlertBreakGlassUsed)
	}
	if AlertBreakGlassIssued != "breakglass.issued" {
		t.Errorf("AlertBreakGlassIssued = %s, want breakglass.issued", AlertBreakGlassIssued)
	}
	if AlertSSOCutover != "sso.cutover" {
		t.Errorf("AlertSSOCutover = %s, want sso.cutover", AlertSSOCutover)
	}
}
