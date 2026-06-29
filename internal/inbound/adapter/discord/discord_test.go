// SPDX-License-Identifier: Apache-2.0

package discord

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	assert.Equal(t, "discord", a.Channel())
}

// testKeyPair generates a deterministic Ed25519 key pair for tests.
func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	pk := ed25519.NewKeyFromSeed(seed)
	return pk.Public().(ed25519.PublicKey), pk
}

func signRequest(t *testing.T, body []byte, ts string, privKey ed25519.PrivateKey) string {
	t.Helper()
	msg := []byte(ts)
	msg = append(msg, body...)
	sig := ed25519.Sign(privKey, msg)
	return hex.EncodeToString(sig)
}

func mustEncrypt(fk inboundtest.FakeSecrets, data []byte) []byte {
	enc, err := fk.Encrypt(data)
	if err != nil {
		panic(err)
	}
	return enc
}

func newTestDeps(t *testing.T) (inbound.Deps, *inboundtest.FakeIngest, *inboundtest.FakeSources, *inboundtest.FakeMetrics, ed25519.PrivateKey) {
	t.Helper()
	fi := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	fs := inboundtest.NewFakeSources()
	fm := ptrext.Of(inboundtest.FakeMetrics{})
	var fk inboundtest.FakeSecrets

	pubKey, privKey := testKeyPair(t)
	pubKeyHex := hex.EncodeToString(pubKey)

	configJSON, _ := json.Marshal(Config{
		Version:            configVersion,
		PublicKeyEncrypted: mustEncrypt(fk, []byte(pubKeyHex)),
	})
	envelope := mustEncrypt(fk, configJSON)

	fs.Put("test-tenant", inbound.Source{
		ID:       "src-1",
		TenantID: "tenant-1",
		Channel:  "discord",
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
	return deps, fi, fs, fm, privKey
}

func serveDiscord(t *testing.T, a *adapter, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Method(http.MethodPost, "/discord/{tenant-slug}/{source-slug}", http.HandlerFunc(a.handleHTTP))

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

func TestPingVerification(t *testing.T) {
	t.Parallel()
	deps, _, _, _, privKey := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(discordPayload{Type: interactionPing})

	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := signRequest(t, payload, ts, privKey)

	rr := serveDiscord(t, a, "/discord/test-tenant/my-source", payload, map[string]string{
		hdrTimestamp: ts,
		hdrSignature: sig,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]int
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp)) // ptrext:allow unmarshal-out-param
	assert.Equal(t, 1, resp["type"])
}

func TestApplicationCommand(t *testing.T) {
	t.Parallel()
	deps, fi, _, fm, privKey := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	cmdData, _ := json.Marshal(commandData{
		Name: "feedback",
		Options: []commandOption{
			{Name: "content", Type: 3, Value: "The export feature is broken"},
		},
	})

	payload, _ := json.Marshal(discordPayload{
		Type:      interactionApplicationCommand,
		Data:      cmdData,
		GuildID:   "G12345",
		ChannelID: "C12345",
		Member:    ptrext.Of(guildMember{User: discordUser{ID: "U12345", Username: "testuser"}}),
		ID:        "INT-001",
	})

	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := signRequest(t, payload, ts, privKey)

	rr := serveDiscord(t, a, "/discord/test-tenant/my-source", payload, map[string]string{
		hdrTimestamp: ts,
		hdrSignature: sig,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, fi.Calls, 1)

	call := fi.Calls[0]
	assert.Equal(t, "tenant-1", call.TenantID)
	assert.Equal(t, "discord", call.In.Source)
	assert.Equal(t, "The export feature is broken", call.In.Content)
	assert.Equal(t, "testuser", call.In.SourceUser)
	assert.Equal(t, "src-1", call.In.SourceMeta["inbound_source_id"])
	assert.Equal(t, "G12345", call.In.SourceMeta["discord_guild_id"])
	assert.Equal(t, "C12345", call.In.SourceMeta["discord_channel_id"])
	assert.Equal(t, "U12345", call.In.SourceMeta["discord_user_id"])

	assert.True(t, hasTotal(fm, "discord", "tenant-1", "my-source", "ok"))
}

func TestMessageComponent(t *testing.T) {
	t.Parallel()
	deps, fi, _, _, privKey := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(discordPayload{
		Type:      interactionMessageComponent,
		GuildID:   "G12345",
		ChannelID: "C12345",
		User:      ptrext.Of(discordUser{ID: "U99", Username: "dmuser"}),
		Message:   ptrext.Of(msgObject{ID: "msg-1", Content: "Button clicked feedback"}),
		ID:        "INT-002",
	})

	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := signRequest(t, payload, ts, privKey)

	rr := serveDiscord(t, a, "/discord/test-tenant/my-source", payload, map[string]string{
		hdrTimestamp: ts,
		hdrSignature: sig,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, fi.Calls, 1)
	assert.Equal(t, "Button clicked feedback", fi.Calls[0].In.Content)
	assert.Equal(t, "dmuser", fi.Calls[0].In.SourceUser)
}

func TestInvalidSignature(t *testing.T) {
	t.Parallel()
	deps, fi, _, fm, _ := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(discordPayload{Type: interactionPing})

	rr := serveDiscord(t, a, "/discord/test-tenant/my-source", payload, map[string]string{
		hdrTimestamp: fmt.Sprintf("%d", time.Now().Unix()),
		hdrSignature: "00" + hex.EncodeToString(make([]byte, ed25519.SignatureSize-1)),
	})

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Empty(t, fi.Calls)
	assert.True(t, hasTotal(fm, "discord", "tenant-1", "my-source", "auth_err"))
}

func TestUnknownSource(t *testing.T) {
	t.Parallel()
	deps, fi, _, _, _ := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload := []byte(`{"type":1}`)

	rr := serveDiscord(t, a, "/discord/unknown-tenant/unknown-source", payload, map[string]string{
		hdrTimestamp: fmt.Sprintf("%d", time.Now().Unix()),
		hdrSignature: hex.EncodeToString(make([]byte, ed25519.SignatureSize)),
	})

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Empty(t, fi.Calls)
}

func TestVerifyDiscordSignature(t *testing.T) {
	t.Parallel()
	pubKey, privKey := testKeyPair(t)
	body := []byte(`{"test":"data"}`)
	ts := fmt.Sprintf("%d", time.Now().Unix())

	msg := []byte(ts)
	msg = append(msg, body...)
	sig := hex.EncodeToString(ed25519.Sign(privKey, msg))

	assert.True(t, verifyDiscordSignature(pubKey, sig, ts, body))
	assert.False(t, verifyDiscordSignature(pubKey, hex.EncodeToString(make([]byte, 64)), ts, body))
	assert.False(t, verifyDiscordSignature(nil, sig, ts, body))
	assert.False(t, verifyDiscordSignature(pubKey, "", ts, body))
	assert.False(t, verifyDiscordSignature(pubKey, sig, "", body))
	assert.False(t, verifyDiscordSignature(pubKey, "invalid-hex", ts, body))
}

func TestExtractContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    discordPayload
		want string
	}{
		{
			"message content",
			discordPayload{Message: ptrext.Of(msgObject{Content: "hello"})},
			"hello",
		},
		{
			"command with content option",
			discordPayload{
				Type: interactionApplicationCommand,
				Data: mustMarshal(commandData{Name: "feedback", Options: []commandOption{{Name: "content", Value: "bug report"}}}),
			},
			"bug report",
		},
		{
			"command fallback",
			discordPayload{
				Type: interactionApplicationCommand,
				Data: mustMarshal(commandData{Name: "submit"}),
			},
			"/submit command invoked",
		},
		{
			"empty",
			discordPayload{},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extractContent(tt.p))
		})
	}
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestParseConfig(t *testing.T) {
	t.Parallel()
	var fk inboundtest.FakeSecrets

	configJSON, _ := json.Marshal(Config{
		Version:            configVersion,
		PublicKeyEncrypted: mustEncrypt(fk, []byte("deadbeef")),
	})
	envelope := mustEncrypt(fk, configJSON)

	cfg, err := parseConfig(envelope, fk)
	require.NoError(t, err)
	assert.Equal(t, configVersion, cfg.Version)
	assert.Equal(t, []byte("deadbeef"), cfg.publicKey)
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
		Version:            99,
		PublicKeyEncrypted: mustEncrypt(fk, []byte("x")),
	})
	envelope := mustEncrypt(fk, configJSON)

	_, err := parseConfig(envelope, fk)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported version")
}

func TestEmptyContentIgnored(t *testing.T) {
	t.Parallel()
	deps, fi, _, _, privKey := newTestDeps(t)
	a := ptrext.Of(adapter{deps: deps})

	payload, _ := json.Marshal(discordPayload{
		Type:      interactionApplicationCommand,
		GuildID:   "G1",
		ChannelID: "C1",
		Data:      mustMarshal(commandData{}),
		ID:        "INT-003",
	})

	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := signRequest(t, payload, ts, privKey)

	rr := serveDiscord(t, a, "/discord/test-tenant/my-source", payload, map[string]string{
		hdrTimestamp: ts,
		hdrSignature: sig,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, fi.Calls)
}

var _ inbound.ShutdownTimeouter = (*adapter)(nil)

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
	assert.Equal(t, "/discord/{tenant-slug}/{source-slug}", mux.Routes[0].Pattern)
}
