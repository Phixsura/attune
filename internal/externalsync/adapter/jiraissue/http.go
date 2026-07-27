// SPDX-License-Identifier: Apache-2.0

package jiraissue

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	core "github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	userAgent    = "attune/1.0"
	maxErrorBody = 64 * 1024
)

type providerError struct {
	kind       string
	message    string
	status     int
	retryable  bool
	requestID  string
	retryAfter *time.Time
}

func (e providerError) Error() string {
	return e.message
}

func (p *Provider) request(ctx context.Context, cfg settings, method, rawURL string, payload []byte) ([]byte, http.Header, error) {
	req, err := buildRequest(ctx, cfg, method, rawURL, payload)
	if err != nil {
		return nil, nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return nil, resp.Header, fmt.Errorf("read jira response: %w", err)
	}
	requestID := firstHeader(resp.Header, "X-Request-Id", "X-Atlassian-Request-Id", "Atlassian-Request-Id", "X-Arequestid")
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return body, resp.Header, jiraHTTPError(resp.StatusCode, rawURL, body, requestID, resp.Header.Get("Retry-After"))
	}
	return body, resp.Header, nil
}

func buildRequest(ctx context.Context, cfg settings, method, rawURL string, payload []byte) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Basic "+basicAuth(cfg.email, cfg.token))
	req.Header.Set("User-Agent", userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	return req, nil
}

func basicAuth(email, token string) string {
	raw := strings.TrimSpace(email) + ":" + strings.TrimSpace(token)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func jiraHTTPError(status int, rawURL string, body []byte, requestID, retryAfter string) error {
	kind, retryable := classifyHTTPStatus(status, body)
	message := jiraErrorMessage(status, rawURL, body)
	return ptrext.Of(providerError{
		kind:       kind,
		message:    message,
		status:     status,
		retryable:  retryable,
		requestID:  requestID,
		retryAfter: retryAfterTime(retryAfter),
	})
}

func retryAfterTime(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		return ptrext.Of(time.Now().UTC().Add(time.Duration(seconds) * time.Second))
	}
	parsed, err := http.ParseTime(raw)
	if err != nil {
		return nil
	}
	return ptrext.Of(parsed.UTC())
}

func validationError(format string, args ...any) error {
	return ptrext.Of(providerError{
		kind:      "validation",
		message:   fmt.Sprintf(format, args...),
		retryable: false,
	})
}

func classifyHTTPStatus(status int, body []byte) (string, bool) {
	msg := strings.ToLower(extractJiraMessage(body))
	switch {
	case status == http.StatusUnauthorized:
		return "auth_failed", false
	case status == http.StatusForbidden && strings.Contains(msg, "rate limit"):
		return "rate_limited", true
	case status == http.StatusForbidden:
		return "auth_failed", false
	case status == http.StatusNotFound:
		return "not_found", false
	case status == http.StatusTooManyRequests:
		return "rate_limited", true
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return "validation", false
	case status == http.StatusConflict:
		return "conflict", false
	case status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout:
		return "provider_unavailable", true
	case status >= http.StatusInternalServerError:
		return "provider_unavailable", true
	default:
		return "provider_error", false
	}
}

func jiraErrorMessage(status int, rawURL string, body []byte) string {
	statusText := http.StatusText(status)
	if statusText == "" {
		statusText = "provider error"
	}
	detail := extractJiraMessage(body)
	if detail == "" {
		detail = statusText
	}
	return fmt.Sprintf("jira api %d at %s: %s", status, nethardening.RedactURL(rawURL), truncate(detail, 300))
}

func extractJiraMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	msg := struct {
		Message       string            `json:"message"`
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}{}
	if err := json.Unmarshal(body, &msg); err == nil { // ptrext:allow unmarshal-out-param
		parts := make([]string, 0, 1+len(msg.ErrorMessages)+len(msg.Errors))
		if s := strings.TrimSpace(msg.Message); s != "" {
			parts = append(parts, s)
		}
		for _, s := range msg.ErrorMessages {
			if s = strings.TrimSpace(s); s != "" {
				parts = append(parts, s)
			}
		}
		for key, value := range msg.Errors {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" && value == "" {
				continue
			}
			if key == "" {
				parts = append(parts, value)
				continue
			}
			if value == "" {
				parts = append(parts, key)
				continue
			}
			parts = append(parts, key+": "+value)
		}
		return strings.TrimSpace(strings.Join(parts, "; "))
	}
	return strings.TrimSpace(string(body))
}

func classifyError(err error) core.SyncError {
	if err == nil {
		return core.SyncError{}
	}
	providerErr := (*providerError)(nil)
	if errors.As(err, &providerErr) { // ptrext:allow errors.As out-param
		return core.SyncError{
			Kind:              providerErr.kind,
			Message:           providerErr.message,
			HTTPStatus:        providerErr.status,
			ProviderRequestID: providerErr.requestID,
			RetryAfter:        providerErr.retryAfter,
			Retryable:         providerErr.retryable,
		}
	}
	blocked := (*nethardening.BlockedError)(nil)
	if errors.As(err, &blocked) { // ptrext:allow errors.As out-param
		return core.SyncError{Kind: "validation", Message: blocked.Error(), Retryable: false}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return core.SyncError{Kind: "provider_unavailable", Message: err.Error(), Retryable: true}
	}
	urlErr := (*url.Error)(nil)
	if errors.As(err, &urlErr) { // ptrext:allow errors.As out-param
		return core.SyncError{
			Kind:      "provider_unavailable",
			Message:   nethardening.RedactURLIn(err.Error(), urlErr.URL),
			Retryable: true,
		}
	}
	return core.SyncError{Kind: "provider_error", Message: err.Error(), Retryable: true}
}

func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit]
}
