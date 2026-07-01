// SPDX-License-Identifier: Apache-2.0

package outboundtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
)

func TestConformanceRunnersExerciseGoldenAndResponseProfiles(t *testing.T) {
	t.Setenv("ATTUNE_UPDATE_GOLDEN", "1")
	channel := stubChannel{}
	target := stubTarget()

	TestEventChannel(t, EventCase{
		Channel:      channel,
		Target:       target,
		Golden:       filepath.Join(t.TempDir(), "event.json"),
		Capabilities: stubCapabilities(),
		ResponseCases: []ResponseCase{
			{Name: "ok", Status: http.StatusOK, WantOK: true},
			{Name: "retry", Status: http.StatusTooManyRequests},
			{Name: "terminal", Status: http.StatusForbidden, WantTerminal: true},
		},
	})

	TestDigestChannel(t, DigestCase{
		Channel:      channel,
		Target:       target,
		Golden:       filepath.Join(t.TempDir(), "digest.json"),
		Capabilities: stubCapabilities(),
		ResponseCases: []ResponseCase{
			{Name: "ok", Status: http.StatusNoContent, WantOK: true},
		},
	})
}

func TestCheckGoldenReadPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	got := "{\"ok\":true}\n"
	t.Setenv("ATTUNE_UPDATE_GOLDEN", "1")
	checkGolden(t, path, got)

	t.Setenv("ATTUNE_UPDATE_GOLDEN", "")
	checkGolden(t, path, got)
}

func TestNormalizeHelpers(t *testing.T) {
	if got := normalizeHeader("Authorization", "Bearer secret"); got != "<authorization>" {
		t.Fatalf("normalizeHeader authorization = %q", got)
	}
	if got := normalizeHeader("X-Attune-Signature", "sig"); got != "<signature>" {
		t.Fatalf("normalizeHeader signature = %q", got)
	}
	if got := normalizeJSON(t, []byte("plain text")); got != "plain text" {
		t.Fatalf("normalizeJSON plain text = %v", got)
	}

	value := normalizeValue(map[string]any{
		"timestamp": "2026-07-01T00:00:00Z",
		"sign":      "abc",
		"nested":    []any{map[string]any{"timestamp": "later"}},
	}).(map[string]any)
	if value["timestamp"] != "<timestamp>" || value["sign"] != "<signature>" {
		t.Fatalf("dynamic fields not normalized: %#v", value)
	}
}

func TestFixturesExposeCanonicalShapes(t *testing.T) {
	if !strings.Contains(MentionAttackText(), "@channel") {
		t.Fatal("mention payload should include chat attack syntax")
	}
	if len(ChatMentionForbiddenBody()) == 0 {
		t.Fatal("chat forbidden body markers should not be empty")
	}

	event := CanonicalEvent()
	if event.EventType != "feedback.enriched" {
		t.Fatalf("canonical event type = %q", event.EventType)
	}
	enriched, _ := event.Feedback["enriched"].(map[string]any)
	if enriched["title"] == "" {
		t.Fatalf("canonical event missing enriched title: %#v", event.Feedback)
	}
	if TestSendEvent().EventType != "test" {
		t.Fatal("TestSendEvent must use the console test event type")
	}
	if CanonicalDigest()["result"] == nil {
		t.Fatal("CanonicalDigest must include result")
	}
	if UnknownDigest()["unexpected"] == nil {
		t.Fatal("UnknownDigest must include fallback content")
	}
}

func TestResponseCheckerHelperAndProfiles(t *testing.T) {
	check := func(ctx context.Context, status int, body []byte) error {
		switch status {
		case http.StatusOK:
			return nil
		case http.StatusBadRequest:
			return fmt.Errorf("%w: bad request", outbound.ErrTerminal)
		default:
			return errors.New("retry body")
		}
	}
	TestResponseChecker(t, check, []ResponseCase{
		{Name: "ok", Status: http.StatusOK, WantOK: true},
		{Name: "terminal", Status: http.StatusBadRequest, WantTerminal: true, WantContains: []string{"bad request"}},
		{Name: "retry", Status: http.StatusTooManyRequests, WantContains: []string{"retry"}, WantNotContains: []string{"secret"}},
	})

	if len(GenericWebhookResponses()) == 0 ||
		len(GitHubIssueResponses()) == 0 ||
		len(LarkWebhookResponses()) == 0 {
		t.Fatal("response profiles must not be empty")
	}
	if len(ChatWebhookResponses(true)) <= len(ChatWebhookResponses(false)) {
		t.Fatal("204-capable chat profile should include an extra success case")
	}
}

type stubChannel struct{}

func (stubChannel) ID() string { return "stub" }

func (stubChannel) RenderEvent(env *outbound.Envelope, dst outbound.Target) (outbound.Rendered, error) {
	return stubRendered(dst.URL), nil
}

func (stubChannel) RenderDigest(view any, dst outbound.Target) (outbound.Rendered, error) {
	return stubRendered(dst.URL), nil
}

func stubRendered(url string) outbound.Rendered {
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			body := []byte(`{"timestamp":"2026-07-01T00:00:00Z","sign":"sig","allowed_mentions":{"parse":[]}}`)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+SecretMarker)
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("User-Agent", "attune/test")
			return req, nil
		},
		Check: func(ctx context.Context, status int, body []byte) error {
			switch {
			case status >= 200 && status < 300:
				return nil
			case status == http.StatusForbidden:
				return fmt.Errorf("%w: forbidden", outbound.ErrTerminal)
			default:
				return errors.New("retry")
			}
		},
	}
}

func stubTarget() outbound.Target {
	return outbound.Target{
		ID:              "target-stub",
		TenantID:        "tenant-conformance",
		URL:             "https://hooks.example.com/services/" + URLTokenMarker,
		Secret:          SecretMarker,
		DestinationType: "stub",
	}
}

func stubCapabilities() Capabilities {
	return Capabilities{
		URLIsCredential:    true,
		HasActiveMentions:  true,
		RequiresAuthHeader: true,
	}
}
