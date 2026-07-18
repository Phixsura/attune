// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"errors"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/requestnotification"
)

func withRandomRead(t *testing.T, read func([]byte) (int, error)) {
	t.Helper()
	previous := randomRead
	randomRead = read
	t.Cleanup(func() {
		randomRead = previous
	})
}

func TestNormalizeNotificationChannelsDefaultsAndDeduplicates(t *testing.T) {
	got := normalizeNotificationChannels([]string{
		repo.ChannelWebhook,
		"unknown",
		repo.ChannelEmail,
		repo.ChannelWebhook,
	})
	want := []string{repo.ChannelWebhook, repo.ChannelEmail}
	if len(got) != len(want) {
		t.Fatalf("channels = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("channels = %#v, want %#v", got, want)
		}
	}

	defaults := normalizeNotificationChannels(nil)
	if len(defaults) != 2 || defaults[0] != repo.ChannelEmail || defaults[1] != repo.ChannelWebhook {
		t.Fatalf("default channels = %#v", defaults)
	}
}

func TestEventChannelRequestedUsesRecipientSnapshot(t *testing.T) {
	if !eventChannelRequested(repo.Event{}, repo.ChannelEmail) {
		t.Fatalf("missing snapshot should allow email")
	}
	event := repo.Event{RecipientSnapshot: map[string]any{
		"channels": []any{repo.ChannelWebhook},
	}}
	if eventChannelRequested(event, repo.ChannelEmail) {
		t.Fatalf("email was allowed by webhook-only snapshot")
	}
	if !eventChannelRequested(event, repo.ChannelWebhook) {
		t.Fatalf("webhook was denied by webhook-only snapshot")
	}
}

func TestRequestNotificationHelpers(t *testing.T) {
	if got := redactedEmail("customer@example.test"); got != "c***@example.test" {
		t.Fatalf("redactedEmail() = %q", got)
	}
	if got := emailDomain("Sender@Example.TEST"); got != "example.test" {
		t.Fatalf("emailDomain() = %q", got)
	}
	if err := validateOutboundURL("https://hooks.example.test/notify"); err != nil {
		t.Fatalf("validateOutboundURL(https) error = %v", err)
	}
	if err := validateOutboundURL("http://127.0.0.1:8080/notify"); err != nil {
		t.Fatalf("validateOutboundURL(loopback) error = %v", err)
	}
	if err := validateOutboundURL("http://hooks.example.test/notify"); err == nil {
		t.Fatalf("validateOutboundURL(non-https) error = nil")
	}
	if !errors.Is(mapRepoError(repo.ErrNotFound), ErrNotFound) {
		t.Fatalf("mapRepoError(not found) did not map to service not found")
	}
	if got := tokenHash("token"); got == "" || got == tokenHash("other") {
		t.Fatalf("tokenHash() = %q, want stable non-empty distinct digest", got)
	}
	token, err := newToken()
	if err != nil {
		t.Fatalf("newToken() error = %v", err)
	}
	if token == "" {
		t.Fatalf("newToken() = empty string")
	}
	if got, err := ptrext.Of(Service{}).decryptString(nil); err != nil || got != "" {
		t.Fatalf("decryptString(nil) = %q, %v; want empty nil-error", got, err)
	}
}

func TestNewTokenReturnsRandomSourceError(t *testing.T) {
	readErr := errors.New("entropy unavailable")
	withRandomRead(t, func([]byte) (int, error) {
		return 0, readErr
	})
	if _, err := newToken(); !errors.Is(err, readErr) {
		t.Fatalf("newToken() error = %v, want random source error", err)
	}
}

func TestEventMaskAndRetryDelay(t *testing.T) {
	if !eventAllowed(nil, repo.EventTypeShipped) {
		t.Fatalf("nil mask should allow events")
	}
	if eventAllowed(map[string]any{repo.EventTypeShipped: false}, repo.EventTypeShipped) {
		t.Fatalf("false mask value should deny event")
	}
	if !eventAllowed(map[string]any{repo.EventTypeShipped: true}, repo.EventTypeShipped) {
		t.Fatalf("true mask value should allow event")
	}
	if !statusAllowed(nil, "shipped") {
		t.Fatalf("nil status policy should allow status")
	}
	if statusAllowed(map[string]any{"shipped": false}, "shipped") {
		t.Fatalf("false status policy should deny status")
	}
	if !statusAllowed(map[string]any{"shipped": true}, " shipped ") {
		t.Fatalf("true status policy should allow trimmed status")
	}
	if statusAllowed(map[string]any{"shipped": "yes"}, "shipped") {
		t.Fatalf("non-bool status policy value should deny status")
	}
	if statusAllowed(map[string]any{"planned": true}, "shipped") {
		t.Fatalf("missing status policy value should deny status")
	}
	blocked := notificationPolicyBlockReason(repo.Settings{
		EnabledEventTypes: map[string]any{repo.EventTypeShipped: false},
	}, repo.EventTypeShipped, "shipped")
	if blocked != "event_type_disabled" {
		t.Fatalf("notificationPolicyBlockReason(event disabled) = %q", blocked)
	}
	blocked = notificationPolicyBlockReason(repo.Settings{
		StatusPolicy: map[string]any{"shipped": false},
	}, repo.EventTypeShipped, "shipped")
	if blocked != "status_policy_disabled" {
		t.Fatalf("notificationPolicyBlockReason(status disabled) = %q", blocked)
	}
	if got := retryDelay(-1); got != 30*time.Second {
		t.Fatalf("retryDelay(-1) = %s", got)
	}
	if got := retryDelay(99); got != time.Hour {
		t.Fatalf("retryDelay(99) = %s", got)
	}
}
