// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestAdapterContract(t *testing.T) {
	inboundtest.TestAdapterContract(t, NewAdapter)
}

func TestChannel(t *testing.T) {
	t.Parallel()
	a := NewAdapter()
	assert.Equal(t, "slack", a.Channel())
}

const testSigningSecret = "test-signing-secret-1234567890"

func signRequest(t *testing.T, body []byte, ts time.Time) (string, string) {
	t.Helper()
	timestamp := strconv.FormatInt(ts.Unix(), 10)
	sigBase := fmt.Sprintf("v0:%s:%s", timestamp, body)
	mac := hmac.New(sha256.New, []byte(testSigningSecret))
	mac.Write([]byte(sigBase))
	sig := fmt.Sprintf("v0=%s", hex.EncodeToString(mac.Sum(nil)))
	return timestamp, sig
}

func mustEncrypt(fk inboundtest.FakeSecrets, data []byte) []byte {
	enc, err := fk.Encrypt(data)
	if err != nil {
		panic(err)
	}
	return enc
}

func newTestDeps(t *testing.T) (inbound.Deps, *inboundtest.FakeIngest, *inboundtest.FakeSources, *inboundtest.FakeMetrics) {
	t.Helper()
	fi := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	fs := inboundtest.NewFakeSources()
	fm := ptrext.Of(inboundtest.FakeMetrics{})
	var fk inboundtest.FakeSecrets

	configJSON, _ := json.Marshal(Config{
		Version:              configVersion,
		SigningSecretEncrypt: mustEncrypt(fk, []byte(testSigningSecret)),
	})
	envelope := mustEncrypt(fk, configJSON)

	fs.Put("test-tenant", inbound.Source{
		ID:       "src-1",
		TenantID: "tenant-1",
		Channel:  "slack",
		Name:     "My Source",
		Slug:     "my-source",
		Config:   envelope,
		Enabled:  true,
	})

	deps := inbound.Deps{
		Ingest:  inbound.IngestFunc(fi.Ingest),
		Sources: fs,
		Secrets: fk,
		Metrics: fm,
	}
	return deps, fi, fs, fm
}

func serveSlack(t *testing.T, a *adapter, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Method(http.MethodPost, "/slack/{tenant-slug}/{source-slug}", http.HandlerFunc(a.handleHTTP))

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func hasTotal(fm *inboundtest.FakeMetrics, channel, tenant, source, result string) bool {
	want := channel + "|" + tenant + "|" + source + "|" + result
	for _, t := range fm.Totals {
		if t == want {
			return true
		}
	}
	return false
}

func TestURLVerification(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(slackPayload{
		Type:      "url_verification",
		Challenge: "test-challenge-token",
	})

	ts, sig := signRequest(t, payload, time.Now())

	rr := serveSlack(t, a, "/slack/test-tenant/my-source", payload, map[string]string{
		hdrSlackRequestTS: ts,
		hdrSlackSignature: sig,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "test-challenge-token", resp["challenge"])
}

func TestEventCallback_Message(t *testing.T) {
	t.Parallel()
	deps, fi, _, fm := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(slackPayload{
		Type:   "event_callback",
		TeamID: "T12345",
		Event: slackEvent{
			Type:    "message",
			User:    "U12345",
			Text:    "This feature is broken",
			Channel: "C12345",
			TS:      "1234567890.123456",
		},
		EventID: "Ev12345",
	})

	ts, sig := signRequest(t, payload, time.Now())

	rr := serveSlack(t, a, "/slack/test-tenant/my-source", payload, map[string]string{
		hdrSlackRequestTS: ts,
		hdrSlackSignature: sig,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, fi.Calls, 1)

	call := fi.Calls[0]
	assert.Equal(t, "tenant-1", call.TenantID)
	assert.Equal(t, "slack", call.In.Source)
	assert.Equal(t, "This feature is broken", call.In.Content)
	assert.Equal(t, "U12345", call.In.SourceUser)
	assert.Equal(t, "src-1", call.In.SourceMeta["inbound_source_id"])
	assert.Equal(t, "T12345", call.In.SourceMeta["slack_team_id"])
	assert.Equal(t, "C12345", call.In.SourceMeta["slack_channel"])

	assert.True(t, hasTotal(fm, "slack", "tenant-1", "my-source", "ok"))
}

func TestEventCallback_AppMention(t *testing.T) {
	t.Parallel()
	deps, fi, _, _ := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(slackPayload{
		Type: "event_callback",
		Event: slackEvent{
			Type: "app_mention",
			User: "U99",
			Text: "Please fix the login page",
		},
		EventID: "Ev99",
	})

	ts, sig := signRequest(t, payload, time.Now())
	rr := serveSlack(t, a, "/slack/test-tenant/my-source", payload, map[string]string{
		hdrSlackRequestTS: ts,
		hdrSlackSignature: sig,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, fi.Calls, 1)
	assert.Equal(t, "Please fix the login page", fi.Calls[0].In.Content)
}

func TestEventCallback_ReactionAdded(t *testing.T) {
	t.Parallel()
	deps, fi, _, _ := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(slackPayload{
		Type: "event_callback",
		Event: slackEvent{
			Type:     "reaction_added",
			User:     "U99",
			Reaction: "thumbsup",
		},
		EventID: "Ev99",
	})

	ts, sig := signRequest(t, payload, time.Now())
	rr := serveSlack(t, a, "/slack/test-tenant/my-source", payload, map[string]string{
		hdrSlackRequestTS: ts,
		hdrSlackSignature: sig,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, fi.Calls, 1)
	assert.Contains(t, fi.Calls[0].In.Content, ":thumbsup:")
}

func TestBotMessagesIgnored(t *testing.T) {
	t.Parallel()
	deps, fi, _, _ := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(slackPayload{
		Type: "event_callback",
		Event: slackEvent{
			Type:  "message",
			Text:  "I am a bot",
			BotID: "B12345",
		},
		EventID: "Ev99",
	})

	ts, sig := signRequest(t, payload, time.Now())
	rr := serveSlack(t, a, "/slack/test-tenant/my-source", payload, map[string]string{
		hdrSlackRequestTS: ts,
		hdrSlackSignature: sig,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, fi.Calls)
}

func TestInvalidSignature(t *testing.T) {
	t.Parallel()
	deps, fi, _, fm := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(slackPayload{
		Type: "event_callback",
		Event: slackEvent{
			Type: "message",
			Text: "should not ingest",
		},
	})

	rr := serveSlack(t, a, "/slack/test-tenant/my-source", payload, map[string]string{
		hdrSlackRequestTS: strconv.FormatInt(time.Now().Unix(), 10),
		hdrSlackSignature: "v0=invalid",
	})

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Empty(t, fi.Calls)
	assert.True(t, hasTotal(fm, "slack", "tenant-1", "my-source", "auth_err"))
}

func TestExpiredTimestamp(t *testing.T) {
	t.Parallel()
	deps, fi, _, _ := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(slackPayload{
		Type:  "event_callback",
		Event: slackEvent{Type: "message", Text: "old"},
	})

	oldTime := time.Now().Add(-10 * time.Minute)
	ts, sig := signRequest(t, payload, oldTime)

	rr := serveSlack(t, a, "/slack/test-tenant/my-source", payload, map[string]string{
		hdrSlackRequestTS: ts,
		hdrSlackSignature: sig,
	})

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Empty(t, fi.Calls)
}

func TestUnknownSource(t *testing.T) {
	t.Parallel()
	deps, fi, _, _ := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload := []byte(`{"type":"event_callback"}`)

	rr := serveSlack(t, a, "/slack/unknown-tenant/unknown-source", payload, map[string]string{
		hdrSlackRequestTS: strconv.FormatInt(time.Now().Unix(), 10),
		hdrSlackSignature: "v0=anything",
	})

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Empty(t, fi.Calls)
}

func TestVerifySlackSignature(t *testing.T) {
	t.Parallel()
	secret := []byte("secret123")
	body := []byte(`{"test":"data"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	sigBase := fmt.Sprintf("v0:%s:%s", ts, body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sigBase))
	sig := fmt.Sprintf("v0=%s", hex.EncodeToString(mac.Sum(nil)))

	assert.True(t, verifySlackSignature(secret, ts, body, sig))
	assert.False(t, verifySlackSignature(secret, ts, body, "v0=wrong"))
	assert.False(t, verifySlackSignature(nil, ts, body, sig))
	assert.False(t, verifySlackSignature(secret, "", body, sig))
	assert.False(t, verifySlackSignature(secret, ts, body, ""))
}

func TestExtractContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ev   slackEvent
		want string
	}{
		{"message", slackEvent{Type: "message", Text: "hello"}, "hello"},
		{"app_mention", slackEvent{Type: "app_mention", Text: "mention"}, "mention"},
		{"reaction", slackEvent{Type: "reaction_added", Reaction: "fire"}, ":fire: reaction on message"},
		{"empty reaction", slackEvent{Type: "reaction_added"}, ""},
		{"unknown with text", slackEvent{Type: "channel_created", Text: "info"}, "info"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extractContent(tt.ev))
		})
	}
}

func TestParseConfig(t *testing.T) {
	t.Parallel()
	var fk inboundtest.FakeSecrets

	configJSON, _ := json.Marshal(Config{
		Version:              configVersion,
		SigningSecretEncrypt: mustEncrypt(fk, []byte("my-secret")),
	})
	envelope := mustEncrypt(fk, configJSON)

	cfg, err := parseConfig(envelope, fk)
	require.NoError(t, err)
	assert.Equal(t, configVersion, cfg.Version)
	assert.Equal(t, []byte("my-secret"), cfg.signingSecret)
}

func TestParseConfig_Empty(t *testing.T) {
	t.Parallel()
	var fk inboundtest.FakeSecrets
	_, err := parseConfig(nil, fk)
	require.Error(t, err)
}

func TestParseConfig_BadVersion(t *testing.T) {
	t.Parallel()
	var fk inboundtest.FakeSecrets

	configJSON, _ := json.Marshal(Config{
		Version:              99,
		SigningSecretEncrypt: mustEncrypt(fk, []byte("x")),
	})
	envelope := mustEncrypt(fk, configJSON)

	_, err := parseConfig(envelope, fk)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported version")
}

func TestEmptyTextIgnored(t *testing.T) {
	t.Parallel()
	deps, fi, _, _ := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(slackPayload{
		Type: "event_callback",
		Event: slackEvent{
			Type: "message",
			User: "U99",
			Text: "",
		},
		EventID: "Ev99",
	})

	ts, sig := signRequest(t, payload, time.Now())
	rr := serveSlack(t, a, "/slack/test-tenant/my-source", payload, map[string]string{
		hdrSlackRequestTS: ts,
		hdrSlackSignature: sig,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, fi.Calls)
}

// Compile-time check that adapter implements ShutdownTimeouter.
var _ inbound.ShutdownTimeouter = (*adapter)(nil)

// contextCancellation: covered by the inboundtest conformance suite.
func TestShutdownTimeout(t *testing.T) {
	t.Parallel()
	a := ptrext.Of(adapter{})
	assert.Equal(t, time.Duration(0), a.ShutdownTimeout())
}

func TestStartMountsRoute(t *testing.T) {
	t.Parallel()
	a := NewAdapter()
	mux := ptrext.Of(inboundtest.FakeMux{})
	deps := inbound.Deps{
		Mux:     mux,
		Ingest:  inbound.IngestFunc(ptrext.Of(inboundtest.FakeIngest{}).Ingest),
		Sources: inboundtest.NewFakeSources(),
		Secrets: inboundtest.FakeSecrets{},
		Metrics: ptrext.Of(inboundtest.FakeMetrics{}),
	}

	err := a.Start(context.Background(), deps)
	require.NoError(t, err)
	require.Len(t, mux.Routes, 1)
	assert.Equal(t, "POST", mux.Routes[0].Method)
	assert.Equal(t, "/slack/{tenant-slug}/{source-slug}", mux.Routes[0].Pattern)
}
