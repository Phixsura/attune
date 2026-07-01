// SPDX-License-Identifier: Apache-2.0

package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrTerminal signals the response is a final failure — the adapter
// considers retrying pointless (e.g. 403 invalid_token). The service
// layer's wrapCheck translates this into notify.ErrTerminal at the
// bridge point (depguard forbids outbound → notify).
var ErrTerminal = errors.New("terminal failure")

// CheckWebhook is the standard response checker for raw-webhook style
// destinations: 2xx ok, 408/429 retryable, other 4xx terminal, 5xx retryable.
// Adapters that need different semantics (e.g., GitHub 201) provide their own.
func CheckWebhook(label string) ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		switch {
		case status >= 200 && status < 300:
			return nil
		case status == 408 || status == 429:
			return fmt.Errorf("%s retryable status=%d", label, status)
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: %s status=%d", ErrTerminal, label, status)
		default:
			return fmt.Errorf("%s status=%d", label, status)
		}
	}
}

// CheckGitHub handles GitHub's response semantics: 201 ok (created);
// rate-limit 403 responses are retryable, other 4xx responses are terminal.
func CheckGitHub(label string) ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		switch {
		case status == 200 || status == 201:
			return nil
		case status == 403 && githubRateLimited(body):
			return fmt.Errorf("%s rate limited status=403 body=%s", label, truncateBody(body, 200))
		case status == 408 || status == 429:
			return fmt.Errorf("%s retryable status=%d", label, status)
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: %s status=%d body=%s", ErrTerminal, label, status, truncateBody(body, 200))
		default:
			return fmt.Errorf("%s status=%d", label, status)
		}
	}
}

func githubRateLimited(body []byte) bool {
	msg := strings.ToLower(githubResponseMessage(body))
	return strings.Contains(msg, "rate limit") || strings.Contains(msg, "rate-limit")
}

func githubResponseMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != "" {
		return payload.Message
	}
	return string(body)
}

func truncateBody(body []byte, n int) string {
	s := string(body)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
