// SPDX-License-Identifier: Apache-2.0

package intercomclient

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestParseRateLimitReset(t *testing.T) {
	fixed := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	orig := nowFn
	nowFn = func() time.Time { return fixed }
	t.Cleanup(func() { nowFn = orig })

	mk := func(v string) http.Header {
		h := http.Header{}
		if v != "" {
			h.Set("X-RateLimit-Reset", v)
		}
		return h
	}

	// Missing header → 10s fallback (Intercom's window size).
	if got := parseRateLimitReset(mk("")); got != 10*time.Second {
		t.Errorf("missing header → %s, want 10s", got)
	}
	// Garbage → fallback.
	if got := parseRateLimitReset(mk("soon")); got != 10*time.Second {
		t.Errorf("garbage → %s, want 10s", got)
	}
	// Normal: 7 seconds out.
	reset := fixed.Add(7 * time.Second).Unix()
	if got := parseRateLimitReset(mk(strconv.FormatInt(reset, 10))); got != 7*time.Second {
		t.Errorf("7s reset → %s", got)
	}
	// Past reset → clamped to 1s minimum.
	past := fixed.Add(-time.Minute).Unix()
	if got := parseRateLimitReset(mk(strconv.FormatInt(past, 10))); got != time.Second {
		t.Errorf("past reset → %s, want 1s", got)
	}
	// Far future → clamped to 15m.
	far := fixed.Add(2 * time.Hour).Unix()
	if got := parseRateLimitReset(mk(strconv.FormatInt(far, 10))); got != 15*time.Minute {
		t.Errorf("far reset → %s, want 15m", got)
	}
	// Zero / negative → fallback.
	if got := parseRateLimitReset(mk("0")); got != 10*time.Second {
		t.Errorf("zero → %s, want 10s", got)
	}
}

func TestAPIError_PermanentMatrix(t *testing.T) {
	permanent := []APIError{
		{Status: 401, Code: "unauthorized"},
		{Status: 403, Code: "api_plan_restricted"},
		{Status: 403, Code: "forbidden"},
		{Status: 401, Code: "token_revoked"},
		{Status: 401, Code: "token_expired"},
		{Status: 401, Code: "something-else"}, // status-based fallback
	}
	for _, e := range permanent {
		if !e.Permanent() {
			t.Errorf("%+v should be permanent", e)
		}
	}
	transient := []APIError{
		{Status: 500, Code: "server_error"},
		{Status: 404, Code: "not_found"},
		{Status: 429, Code: "rate_limit"},
	}
	for _, e := range transient {
		if e.Permanent() {
			t.Errorf("%+v should NOT be permanent", e)
		}
	}
}

func TestRateLimitError_Message(t *testing.T) {
	e := RateLimitError{Method: "/conversations/search", RetryAfter: 9 * time.Second}
	if e.Error() == "" {
		t.Error("empty message")
	}
}
