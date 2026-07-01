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

// ProviderShape names the provider wire-contract profile an adapter must emit.
type ProviderShape string

const (
	ProviderShapeRawWebhook  ProviderShape = "raw-webhook"
	ProviderShapeSlack       ProviderShape = "slack"
	ProviderShapeDiscord     ProviderShape = "discord"
	ProviderShapeLark        ProviderShape = "lark"
	ProviderShapeGitHubIssue ProviderShape = "github-issue"
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
	ProviderShape ProviderShape
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
	ProviderShape ProviderShape
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
		ProviderShape: tc.ProviderShape,
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
		ProviderShape: tc.ProviderShape,
		Capabilities:  tc.Capabilities,
		ResponseCases: tc.ResponseCases,
		ForbiddenBody: tc.ForbiddenBody,
	})
}

type renderCase struct {
	Target        outbound.Target
	Golden        string
	ProviderShape ProviderShape
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
	assertProviderShape(t, req, tc.Target, tc.ProviderShape)
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

func assertProviderShape(t *testing.T, req *http.Request, target outbound.Target, shape ProviderShape) {
	t.Helper()
	if shape == "" {
		return
	}
	body := requestJSONBody(t, req)
	switch shape {
	case ProviderShapeRawWebhook:
		assertRawWebhookShape(t, body)
	case ProviderShapeSlack:
		assertSlackShape(t, body)
	case ProviderShapeDiscord:
		assertDiscordShape(t, body)
	case ProviderShapeLark:
		assertLarkShape(t, body, target)
	case ProviderShapeGitHubIssue:
		assertGitHubIssueShape(t, body)
	default:
		t.Fatalf("unknown provider shape %q", shape)
	}
}

func requestJSONBody(t *testing.T, req *http.Request) map[string]any {
	t.Helper()
	rawBody, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read provider body: %v", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(rawBody))

	var body map[string]any
	if err := json.Unmarshal(rawBody, &body); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("provider body must be a JSON object: %v\nbody=%s", err, rawBody)
	}
	return body
}

func assertRawWebhookShape(t *testing.T, body map[string]any) {
	t.Helper()
	if stringValue(body, "event_type") == "" {
		t.Fatalf("raw webhook body missing event_type: %#v", body)
	}
	if stringValue(body, "tenant_id") == "" {
		t.Fatalf("raw webhook body missing tenant_id: %#v", body)
	}
}

func assertSlackShape(t *testing.T, body map[string]any) {
	t.Helper()
	blocks, ok := body["blocks"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("Slack body must include non-empty blocks: %#v", body)
	}
	for i, block := range blocks {
		m, ok := block.(map[string]any)
		if !ok || stringValue(m, "type") == "" {
			t.Fatalf("Slack block %d missing type: %#v", i, block)
		}
	}
}

func assertDiscordShape(t *testing.T, body map[string]any) {
	t.Helper()
	embeds, ok := body["embeds"].([]any)
	if !ok || len(embeds) == 0 {
		t.Fatalf("Discord body must include non-empty embeds: %#v", body)
	}
	allowed, ok := body["allowed_mentions"].(map[string]any)
	if !ok {
		t.Fatalf("Discord body missing allowed_mentions: %#v", body)
	}
	parse, ok := allowed["parse"].([]any)
	if !ok || len(parse) != 0 {
		t.Fatalf("Discord allowed_mentions.parse = %#v, want empty array", allowed["parse"])
	}
}

func assertLarkShape(t *testing.T, body map[string]any, target outbound.Target) {
	t.Helper()
	if got := stringValue(body, "msg_type"); got != "interactive" {
		t.Fatalf("Lark msg_type = %q, want interactive", got)
	}
	if target.Secret != "" {
		if stringValue(body, "timestamp") == "" || stringValue(body, "sign") == "" {
			t.Fatalf("signed Lark webhook must include timestamp and sign: %#v", body)
		}
	}
	card, ok := body["card"].(map[string]any)
	if !ok {
		t.Fatalf("Lark body missing card: %#v", body)
	}
	header, ok := card["header"].(map[string]any)
	if !ok {
		t.Fatalf("Lark card missing header: %#v", card)
	}
	title, ok := header["title"].(map[string]any)
	if !ok || stringValue(title, "content") == "" {
		t.Fatalf("Lark header.title missing text content: %#v", header)
	}
	elements, ok := card["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("Lark card must include elements: %#v", card)
	}
	assertLarkNoteContentStrings(t, elements)
}

func assertLarkNoteContentStrings(t *testing.T, elements []any) {
	t.Helper()
	for _, element := range elements {
		m, ok := element.(map[string]any)
		if !ok {
			continue
		}
		if stringValue(m, "tag") != "note" {
			continue
		}
		children, ok := m["elements"].([]any)
		if !ok || len(children) == 0 {
			t.Fatalf("Lark note must include child elements: %#v", m)
		}
		for _, child := range children {
			childMap, ok := child.(map[string]any)
			if !ok {
				t.Fatalf("Lark note child must be an object: %#v", child)
			}
			if _, ok := childMap["content"].(string); !ok {
				t.Fatalf("Lark note child content encoded as %T, want string", childMap["content"])
			}
		}
	}
}

func assertGitHubIssueShape(t *testing.T, body map[string]any) {
	t.Helper()
	if stringValue(body, "title") == "" {
		t.Fatalf("GitHub issue body missing title: %#v", body)
	}
	if stringValue(body, "body") == "" {
		t.Fatalf("GitHub issue body missing body: %#v", body)
	}
	labels, ok := body["labels"].([]any)
	if !ok || len(labels) == 0 {
		t.Fatalf("GitHub issue body must include labels: %#v", body)
	}
	for _, label := range labels {
		s, ok := label.(string)
		if !ok || s == "" {
			t.Fatalf("GitHub issue label must be a non-empty string: %#v", label)
		}
		if len([]rune(s)) > 50 {
			t.Fatalf("GitHub issue label %q exceeds provider limit", s)
		}
		if strings.TrimSpace(s) != s || strings.ContainsAny(s, "\n\r\t@") {
			t.Fatalf("GitHub issue label %q contains unsafe whitespace or mention marker", s)
		}
	}
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
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
