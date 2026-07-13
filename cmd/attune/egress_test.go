// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/inbound/adapter/slack"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	replydraftsvc "github.com/Phixsura/attune/internal/service/replydraft"
)

func allowLoopbackEgressForTest(t *testing.T) {
	t.Helper()
	notify.SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})
	t.Cleanup(func() {
		notify.SetEgressPolicy(nethardening.Policy{})
	})
}

func TestApplyRuntimeHardeningSetsSlackAPIBaseURL(t *testing.T) {
	t.Cleanup(func() {
		notify.SetEgressPolicy(nethardening.Policy{})
		externalsync.SetEgressPolicy(nethardening.Policy{})
		llmclient.SetEgressPolicy(nethardening.Policy{})
		replydraftsvc.SetEgressPolicy(nethardening.Policy{})
		nethardening.SetTrustedProxyHops(0)
		slack.SetAPIBaseURL("")
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth.test" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-test" {
			t.Fatalf("Authorization = %q, want Bearer xoxb-test", got)
		}
		_, _ = w.Write([]byte(`{"ok":true,"team_id":"T123","team":"Acme","url":"https://acme.slack.com/"}`))
	}))
	defer server.Close()

	cfg := ptrext.Of(config.Config{
		SlackAPIBaseURL: server.URL,
		Security: config.SecurityConfig{
			AllowLoopbackEgress: true,
			AllowPrivateEgress:  true,
			TrustedProxyHops:    2,
		},
	})
	applyRuntimeHardening(cfg)

	info, err := slack.AuthTest(context.Background(), "xoxb-test")
	if err != nil {
		t.Fatalf("AuthTest: %v", err)
	}
	if info.TeamID != "T123" {
		t.Fatalf("TeamID = %q, want T123", info.TeamID)
	}
}
