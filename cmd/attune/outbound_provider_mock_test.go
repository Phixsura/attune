// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/outbound/outboundtest"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

func TestOutboundProviderMocks_TestSendMatrix(t *testing.T) {
	notify.SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})

	cases := []struct {
		name      string
		destType  string
		path      string
		secret    string
		signature string
		response  outboundtest.ProviderResponse
		assert    func(t *testing.T, req outboundtest.ProviderRequest)
	}{
		{
			name:      "raw_webhook_signed_json",
			destType:  notifytarget.DestRawWebhook,
			path:      "/webhook/raw/" + outboundtest.URLTokenMarker,
			secret:    outboundtest.SecretMarker,
			signature: outbound.SignatureVersionContentHash,
			response:  outboundtest.ProviderResponse{Status: http.StatusNoContent},
			assert: func(t *testing.T, req outboundtest.ProviderRequest) {
				t.Helper()
				assertPostJSON(t, req)
				if req.Header.Get("X-Attune-Signature") == "" {
					t.Error("X-Attune-Signature must be set")
				}
				if !strings.Contains(req.BodyString(), "Test Notification") {
					t.Error("raw webhook body should contain the test notification")
				}
			},
		},
		{
			name:     "slack_block_kit",
			destType: notifytarget.DestSlack,
			path:     "/services/T000/B000/" + outboundtest.URLTokenMarker,
			response: outboundtest.ProviderResponse{Status: http.StatusOK, Body: "ok"},
			assert: func(t *testing.T, req outboundtest.ProviderRequest) {
				t.Helper()
				assertPostJSON(t, req)
				msg := ptrext.Of(struct {
					Blocks []map[string]any `json:"blocks"`
				}{})
				decodeProviderJSON(t, req, msg)
				if len(msg.Blocks) < 3 {
					t.Fatalf("blocks = %d, want at least 3", len(msg.Blocks))
				}
				if strings.Contains(req.BodyString(), outboundtest.URLTokenMarker) {
					t.Error("Slack URL token leaked into request body")
				}
			},
		},
		{
			name:     "lark_card_success_body",
			destType: notifytarget.DestLark,
			path:     "/open-apis/bot/v2/hook/" + outboundtest.URLTokenMarker,
			secret:   outboundtest.SecretMarker,
			response: outboundtest.ProviderResponse{
				Status: http.StatusOK,
				Body:   `{"StatusCode":0,"StatusMessage":"success"}`,
			},
			assert: func(t *testing.T, req outboundtest.ProviderRequest) {
				t.Helper()
				assertPostJSON(t, req)
				msg := ptrext.Of(struct {
					MsgType   string         `json:"msg_type"`
					Card      map[string]any `json:"card"`
					Timestamp string         `json:"timestamp"`
					Sign      string         `json:"sign"`
				}{})
				decodeProviderJSON(t, req, msg)
				if msg.MsgType != "interactive" {
					t.Fatalf("msg_type = %q, want interactive", msg.MsgType)
				}
				if len(msg.Card) == 0 {
					t.Fatal("lark card must be present")
				}
				if msg.Timestamp == "" || msg.Sign == "" {
					t.Fatal("lark signed webhook must include timestamp and sign")
				}
			},
		},
		{
			name:     "discord_embed_no_mentions",
			destType: notifytarget.DestDiscord,
			path:     "/api/webhooks/123/" + outboundtest.URLTokenMarker,
			response: outboundtest.ProviderResponse{Status: http.StatusNoContent},
			assert: func(t *testing.T, req outboundtest.ProviderRequest) {
				t.Helper()
				assertPostJSON(t, req)
				msg := ptrext.Of(struct {
					Embeds          []map[string]any `json:"embeds"`
					AllowedMentions struct {
						Parse []string `json:"parse"`
					} `json:"allowed_mentions"`
				}{})
				decodeProviderJSON(t, req, msg)
				if len(msg.Embeds) == 0 {
					t.Fatal("discord embeds must be present")
				}
				if msg.AllowedMentions.Parse == nil || len(msg.AllowedMentions.Parse) != 0 {
					t.Fatalf("allowed_mentions.parse = %v, want empty list", msg.AllowedMentions.Parse)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := outboundtest.NewProvider(t, outboundtest.ProviderScenario{
				Name:      tc.name,
				Responses: []outboundtest.ProviderResponse{tc.response},
				Assert:    tc.assert,
			})
			target := notifytarget.NotifyTarget{
				ID:               uuid.New(),
				TenantID:         "provider-mock-tenant",
				DestinationType:  tc.destType,
				Audience:         notifytarget.AudienceAll,
				URL:              provider.URL(tc.path),
				Secret:           tc.secret,
				SignatureVersion: tc.signature,
				TimeoutSeconds:   5,
			}

			result := notify.TestSend(t.Context(), target)
			if !result.OK {
				t.Fatalf("TestSend failed: ok=%v status=%d err=%v",
					result.OK, result.StatusCode, result.Err)
			}
			if provider.CallCount() != 1 {
				t.Fatalf("provider calls = %d, want 1", provider.CallCount())
			}
		})
	}
}

func TestOutboundProviderMocks_TransportRetryAndTerminal(t *testing.T) {
	notify.SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})

	cases := []struct {
		name      string
		responses []outboundtest.ProviderResponse
		wantErr   bool
		wantTerm  bool
		wantCalls int
	}{
		{
			name: "retryable_then_success",
			responses: []outboundtest.ProviderResponse{
				{Status: http.StatusInternalServerError, Body: "temporary upstream failure"},
				{Status: http.StatusNoContent},
			},
			wantCalls: 2,
		},
		{
			name: "terminal_4xx_stops",
			responses: []outboundtest.ProviderResponse{
				{Status: http.StatusForbidden, Body: "invalid signature"},
				{Status: http.StatusNoContent},
			},
			wantErr:   true,
			wantTerm:  true,
			wantCalls: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := outboundtest.NewProvider(t, outboundtest.ProviderScenario{
				Name:      tc.name,
				Responses: tc.responses,
				Assert: func(t *testing.T, req outboundtest.ProviderRequest) {
					t.Helper()
					assertPostJSON(t, req)
				},
			})
			ch := outbound.LookupEvent(notifytarget.DestRawWebhook)
			if ch == nil {
				t.Fatal("raw-webhook adapter not registered")
			}
			rendered, err := ch.RenderEvent(outboundtest.TestSendEvent(), outbound.Target{
				ID:               "provider-mock-target",
				TenantID:         "provider-mock-tenant",
				URL:              provider.URL("/transport/raw/" + outboundtest.URLTokenMarker),
				Secret:           outboundtest.SecretMarker,
				SignatureVersion: outbound.SignatureVersionContentHash,
				DestinationType:  notifytarget.DestRawWebhook,
			})
			if err != nil {
				t.Fatalf("RenderEvent: %v", err)
			}

			transport := notify.NewTransport(nil, notify.RetryPolicy{MaxAttempts: 2})
			err = transport.Send(
				t.Context(),
				"provider-mock-raw-webhook",
				rendered.Build,
				bridgeOutboundCheck(rendered.Check),
			)
			if tc.wantErr && err == nil {
				t.Fatal("got nil error, want failure")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Send: %v", err)
			}
			if tc.wantTerm && !errors.Is(err, notify.ErrTerminal) {
				t.Fatalf("got %v, want notify.ErrTerminal", err)
			}
			if provider.CallCount() != tc.wantCalls {
				t.Fatalf("provider calls = %d, want %d", provider.CallCount(), tc.wantCalls)
			}
		})
	}
}

func assertPostJSON(t *testing.T, req outboundtest.ProviderRequest) {
	t.Helper()
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", req.Method)
	}
	if !strings.HasPrefix(req.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", req.Header.Get("Content-Type"))
	}
	if req.Header.Get("User-Agent") == "" {
		t.Fatal("User-Agent must be set")
	}
}

func decodeProviderJSON(t *testing.T, req outboundtest.ProviderRequest, dst any) {
	t.Helper()
	if err := json.Unmarshal(req.Body, dst); err != nil {
		t.Fatalf("unmarshal provider request body: %v\nbody: %s", err, req.BodyString())
	}
}

func bridgeOutboundCheck(check outbound.ResponseChecker) notify.ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		err := check(ctx, status, body)
		if err == nil {
			return nil
		}
		if errors.Is(err, outbound.ErrTerminal) {
			return fmt.Errorf("%w: %w", notify.ErrTerminal, err)
		}
		return err
	}
}
