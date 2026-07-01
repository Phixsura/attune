// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test log capture and io helpers

package outboundtest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext/logexttest"
)

// Capabilities declares which conformance profiles apply to an adapter.
type Capabilities struct {
	URLIsCredential          bool
	HasActiveMentions        bool
	RequiresAuthHeader       bool
	AllowsHTTP201            bool
	AllowsHTTP204            bool
	RequiresProviderCodeZero bool
	PreservesRawCustomerBody bool
}

// EventCase configures one EventChannel conformance run.
type EventCase struct {
	Channel       outbound.EventChannel
	Target        outbound.Target
	Envelope      *outbound.Envelope
	Golden        string
	Capabilities  Capabilities
	ResponseCases []ResponseCase
	ForbiddenBody []string
}

// DigestCase configures one DigestChannel conformance run.
type DigestCase struct {
	Channel       outbound.DigestChannel
	Target        outbound.Target
	View          any
	Golden        string
	Capabilities  Capabilities
	ResponseCases []ResponseCase
	ForbiddenBody []string
}

// TestEventChannel runs the shared event adapter contract.
func TestEventChannel(t *testing.T, tc EventCase) {
	t.Helper()
	if tc.Channel == nil {
		t.Fatal("Channel must not be nil")
	}
	if tc.Channel.ID() == "" {
		t.Fatal("Channel.ID returned empty string")
	}
	if tc.Envelope == nil {
		tc.Envelope = CanonicalEvent()
	}
	rendered, err := tc.Channel.RenderEvent(tc.Envelope, tc.Target)
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	testRendered(t, rendered, renderCase{
		Target:        tc.Target,
		Golden:        tc.Golden,
		Capabilities:  tc.Capabilities,
		ResponseCases: tc.ResponseCases,
		ForbiddenBody: tc.ForbiddenBody,
	})
}

// TestDigestChannel runs the shared digest adapter contract.
func TestDigestChannel(t *testing.T, tc DigestCase) {
	t.Helper()
	if tc.Channel == nil {
		t.Fatal("Channel must not be nil")
	}
	if tc.Channel.ID() == "" {
		t.Fatal("Channel.ID returned empty string")
	}
	if tc.View == nil {
		tc.View = CanonicalDigest()
	}
	rendered, err := tc.Channel.RenderDigest(tc.View, tc.Target)
	if err != nil {
		t.Fatalf("RenderDigest: %v", err)
	}
	testRendered(t, rendered, renderCase{
		Target:        tc.Target,
		Golden:        tc.Golden,
		Capabilities:  tc.Capabilities,
		ResponseCases: tc.ResponseCases,
		ForbiddenBody: tc.ForbiddenBody,
	})
}

type renderCase struct {
	Target        outbound.Target
	Golden        string
	Capabilities  Capabilities
	ResponseCases []ResponseCase
	ForbiddenBody []string
}

func testRendered(t *testing.T, rendered outbound.Rendered, tc renderCase) {
	t.Helper()
	if rendered.Build == nil {
		t.Fatal("Rendered.Build must not be nil")
	}
	if rendered.Check == nil {
		t.Fatal("Rendered.Check must not be nil")
	}

	req, logs := buildWithCapturedLogs(t, rendered.Build)
	if req.Method != http.MethodPost {
		t.Fatalf("request method = %s, want POST", req.Method)
	}
	if req.Header.Get("Content-Type") == "" {
		t.Fatal("Content-Type must be set")
	}
	if req.Header.Get("User-Agent") == "" {
		t.Fatal("User-Agent must be set")
	}
	if tc.Capabilities.RequiresAuthHeader && req.Header.Get("Authorization") == "" {
		t.Fatal("Authorization header must be set")
	}

	snap := snapshotRequest(t, req)
	assertNoSensitiveMarkers(t, "logs", logs, tc.Target, tc.Capabilities, tc.ForbiddenBody)
	assertNoSensitiveMarkers(t, "snapshot", snap, tc.Target, tc.Capabilities, nil)
	assertForbiddenBody(t, snap, tc.ForbiddenBody)
	assertMentionDefense(t, snap, tc.Capabilities, tc.ForbiddenBody)
	checkGolden(t, tc.Golden, snap)
	if len(tc.ResponseCases) > 0 {
		TestResponseChecker(t, rendered.Check, tc.ResponseCases)
	}
}

func buildWithCapturedLogs(
	t *testing.T, build func(context.Context) (*http.Request, error),
) (*http.Request, string) {
	t.Helper()
	var req *http.Request
	var err error
	logs := logexttest.CaptureText(t, func() {
		req, err = build(context.Background())
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return req, logs
}

func snapshotRequest(t *testing.T, req *http.Request) string {
	t.Helper()
	rawBody, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(rawBody))

	body := normalizeJSON(t, rawBody)
	headers := map[string]string{}
	for key, values := range req.Header {
		if len(values) == 0 {
			continue
		}
		headers[key] = normalizeHeader(key, values[0])
	}
	out := map[string]any{
		"method":  req.Method,
		"url":     normalizeURL(req.URL),
		"headers": headers,
		"body":    body,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("marshal request snapshot: %v", err)
	}
	return string(b) + "\n"
}

func normalizeHeader(key, value string) string {
	switch strings.ToLower(key) {
	case "authorization":
		return "<authorization>"
	case "x-attune-signature":
		return "<signature>"
	default:
		return value
	}
}

func normalizeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func normalizeJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil { // ptrext:allow unmarshal-out-param
		return string(raw)
	}
	return normalizeValue(v)
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range val {
			out[key] = normalizeDynamic(key, child)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = normalizeValue(child)
		}
		return out
	default:
		return v
	}
}

func normalizeDynamic(key string, v any) any {
	switch strings.ToLower(key) {
	case "sign":
		if s, ok := v.(string); ok && s != "" {
			return "<signature>"
		}
	case "timestamp":
		if s, ok := v.(string); ok && s != "" {
			return "<timestamp>"
		}
	}
	return normalizeValue(v)
}

func checkGolden(t *testing.T, relPath, got string) {
	t.Helper()
	if relPath == "" {
		return
	}
	path := filepath.Clean(relPath)
	if os.Getenv("ATTUNE_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("update golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("snapshot drift for %s\n--- want\n%s\n--- got\n%s", path, string(want), got)
	}
}

func assertNoSensitiveMarkers(
	t *testing.T,
	where string,
	value string,
	target outbound.Target,
	caps Capabilities,
	extra []string,
) {
	t.Helper()
	for _, forbidden := range sensitiveMarkers(target, caps) {
		if strings.Contains(value, forbidden) {
			t.Fatalf("%s leaked sensitive marker %q in %q", where, forbidden, value)
		}
	}
	for _, forbidden := range extra {
		if strings.Contains(value, forbidden) {
			t.Fatalf("%s leaked forbidden marker %q in %q", where, forbidden, value)
		}
	}
}

func sensitiveMarkers(target outbound.Target, caps Capabilities) []string {
	var out []string
	if target.Secret != "" {
		out = append(out, target.Secret)
	}
	if caps.URLIsCredential {
		out = append(out, URLTokenMarker)
		if u, err := url.Parse(target.URL); err == nil {
			out = append(out, strings.Trim(u.Path, "/"))
		}
	}
	out = append(out, "Bearer "+target.Secret)
	return compactStrings(out)
}

func assertForbiddenBody(t *testing.T, value string, forbidden []string) {
	t.Helper()
	for _, marker := range forbidden {
		if strings.Contains(value, marker) {
			t.Fatalf("request body retained forbidden marker %q in %q", marker, value)
		}
	}
}

func assertMentionDefense(
	t *testing.T,
	snapshot string,
	caps Capabilities,
	forbiddenBody []string,
) {
	t.Helper()
	if !caps.HasActiveMentions {
		return
	}
	if len(forbiddenBody) > 0 {
		return
	}
	if strings.Contains(snapshot, `"allowed_mentions"`) && strings.Contains(snapshot, `"parse": []`) {
		return
	}
	t.Fatal("active-mention adapter must forbid dangerous body tokens or disable mentions in the provider payload")
}

func compactStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
