// SPDX-License-Identifier: Apache-2.0

// Package securityalert sends real-time security notifications for critical
// events like break-glass token usage (#158).
package securityalert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// AlertType categorizes the security event.
type AlertType string

const (
	// AlertBreakGlassUsed — a break-glass token was used for emergency login.
	AlertBreakGlassUsed AlertType = "breakglass.used"
	// AlertBreakGlassIssued — a break-glass token was issued.
	AlertBreakGlassIssued AlertType = "breakglass.issued"
	// AlertSSOCutover — SSO mode was changed (cutover or fallback).
	AlertSSOCutover AlertType = "sso.cutover"
)

// Alert represents a security event to be sent.
type Alert struct {
	Type      AlertType         `json:"type"`
	Timestamp time.Time         `json:"timestamp"`
	TenantID  string            `json:"tenant_id"`
	Actor     string            `json:"actor"`
	Summary   string            `json:"summary"`
	Details   map[string]string `json:"details,omitempty"`
}

// Service handles security alert dispatch.
type Service struct {
	webhookURL string
	httpClient *http.Client
}

// NewService creates a security alert service.
// If webhookURL is empty, alerts are logged but not sent.
func NewService(webhookURL string) *Service {
	return ptrext.Of(Service{
		webhookURL: webhookURL,
		httpClient: ptrext.Of(http.Client{
			Timeout:   10 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		}),
	})
}

// Send dispatches a security alert. Errors are logged but not returned
// to avoid blocking the critical path.
func (s *Service) Send(ctx context.Context, alert Alert) {
	const where = "securityalert.Service.Send"

	if alert.Timestamp.IsZero() {
		alert.Timestamp = time.Now().UTC()
	}

	logext.Warnf(ctx, "[%s] SECURITY ALERT: type=%s,actor=%s,summary=%s",
		where, alert.Type, alert.Actor, alert.Summary)

	if s.webhookURL == "" {
		return
	}

	go s.sendWebhook(ctx, alert)
}

func (s *Service) sendWebhook(ctx context.Context, alert Alert) {
	const where = "securityalert.Service.sendWebhook"

	payload, err := json.Marshal(alert)
	if err != nil {
		logext.Errorf(ctx, "[%s] marshal failed,err:%s", where, err.Error())
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(payload))
	if err != nil {
		logext.Errorf(ctx, "[%s] request creation failed,err:%s", where, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "attune-security-alert/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		logext.Errorf(ctx, "[%s] send failed,url:%s,err:%s", where, s.webhookURL, err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		logext.Errorf(ctx, "[%s] webhook error,status:%d,body:%s", where, resp.StatusCode, string(body))
		return
	}

	logext.Infof(ctx, "[%s] alert sent,type:%s,status:%d", where, alert.Type, resp.StatusCode)
}

// BreakGlassUsedAlert creates an alert for break-glass token usage.
func BreakGlassUsedAlert(tenantID, adminEmail, tokenID, clientIP, userAgent string) Alert {
	return Alert{
		Type:     AlertBreakGlassUsed,
		TenantID: tenantID,
		Actor:    adminEmail,
		Summary:  fmt.Sprintf("Break-glass emergency login by %s from %s", adminEmail, clientIP),
		Details: map[string]string{
			"token_id":   tokenID,
			"client_ip":  clientIP,
			"user_agent": userAgent,
		},
	}
}

// BreakGlassIssuedAlert creates an alert for break-glass token issuance.
func BreakGlassIssuedAlert(tenantID, issuedBy, forAdmin, tokenID string, ttl time.Duration) Alert {
	return Alert{
		Type:     AlertBreakGlassIssued,
		TenantID: tenantID,
		Actor:    issuedBy,
		Summary:  fmt.Sprintf("Break-glass token issued by %s for %s (TTL: %s)", issuedBy, forAdmin, ttl.String()),
		Details: map[string]string{
			"token_id":  tokenID,
			"for_admin": forAdmin,
			"ttl":       ttl.String(),
		},
	}
}

// SSOCutoverAlert creates an alert for SSO mode change.
func SSOCutoverAlert(tenantID, actor, oldMode, newMode string) Alert {
	return Alert{
		Type:     AlertSSOCutover,
		TenantID: tenantID,
		Actor:    actor,
		Summary:  fmt.Sprintf("SSO mode changed by %s: %s -> %s", actor, oldMode, newMode),
		Details: map[string]string{
			"old_mode": oldMode,
			"new_mode": newMode,
		},
	}
}
