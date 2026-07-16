// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// ---------- mock SecretStore for parseConfig tests ----------

type mockSecrets struct {
	decryptFn func([]byte) ([]byte, error)
	encryptFn func([]byte) ([]byte, error)
}

func (m mockSecrets) Decrypt(b []byte) ([]byte, error) { return m.decryptFn(b) }
func (m mockSecrets) Encrypt(b []byte) ([]byte, error) { return m.encryptFn(b) }

// passthrough returns a SecretStore where Encrypt/Decrypt are identity.
func passthrough() inbound.SecretStore {
	return mockSecrets{
		decryptFn: func(b []byte) ([]byte, error) { return b, nil },
		encryptFn: func(b []byte) ([]byte, error) { return b, nil },
	}
}

func TestAdapterShutdownTimeoutIsImmediate(t *testing.T) {
	t.Parallel()

	timeoutProvider, ok := NewAdapter().(interface{ ShutdownTimeout() time.Duration })
	if !ok {
		t.Fatal("NewAdapter() does not expose ShutdownTimeout")
	}
	if got := timeoutProvider.ShutdownTimeout(); got != 0 {
		t.Fatalf("ShutdownTimeout() = %s, want immediate shutdown", got)
	}
}

// ---------- parseConfig tests ----------

func TestParseConfig_DecryptError(t *testing.T) {
	t.Parallel()
	secrets := mockSecrets{
		decryptFn: func([]byte) ([]byte, error) { return nil, errors.New("decrypt boom") },
		encryptFn: func([]byte) ([]byte, error) { return nil, nil },
	}
	_, _, prevExpired, err := parseConfig([]byte("cipher"), secrets)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt boom")
	assert.True(t, prevExpired)
}

func TestParseConfig_UnmarshalError(t *testing.T) {
	t.Parallel()
	secrets := passthrough()
	_, _, prevExpired, err := parseConfig([]byte("not-json{{{"), secrets)
	require.Error(t, err)
	assert.True(t, prevExpired)
}

func TestParseConfig_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	cfg, _ := json.Marshal(Config{
		Version:                99,
		SecretCurrentEncrypted: []byte("cur"),
		HMACAlgo:               "sha256",
	})
	_, _, prevExpired, err := parseConfig(cfg, passthrough())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported config version")
	assert.True(t, prevExpired)
}

func TestParseConfig_UnsupportedHMACAlgo(t *testing.T) {
	t.Parallel()
	cfg, _ := json.Marshal(Config{
		Version:                ConfigVersion,
		SecretCurrentEncrypted: []byte("cur"),
		HMACAlgo:               "sha512",
	})
	_, _, prevExpired, err := parseConfig(cfg, passthrough())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported hmac algo")
	assert.True(t, prevExpired)
}

func TestParseConfig_EmptyHMACAlgo_DefaultsSHA256(t *testing.T) {
	t.Parallel()
	cfg, _ := json.Marshal(Config{
		Version:                ConfigVersion,
		SecretCurrentEncrypted: []byte("cur"),
		HMACAlgo:               "",
	})
	current, previous, prevExpired, err := parseConfig(cfg, passthrough())
	require.NoError(t, err)
	assert.Equal(t, []byte("cur"), current)
	assert.Nil(t, previous)
	assert.True(t, prevExpired, "no previous secret means prevExpired=true")
}

func TestParseConfig_NoPreviousSecret(t *testing.T) {
	t.Parallel()
	cfg, _ := json.Marshal(Config{
		Version:                ConfigVersion,
		SecretCurrentEncrypted: []byte("my-current"),
		HMACAlgo:               "sha256",
	})
	current, previous, prevExpired, err := parseConfig(cfg, passthrough())
	require.NoError(t, err)
	assert.Equal(t, []byte("my-current"), current)
	assert.Nil(t, previous)
	assert.True(t, prevExpired, "absent previous always counts as expired")
}

func TestParseConfig_PreviousSecretFutureExpiry(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(1 * time.Hour)
	cfg, _ := json.Marshal(Config{
		Version:                 ConfigVersion,
		SecretCurrentEncrypted:  []byte("cur"),
		SecretPreviousEncrypted: []byte("prev"),
		PreviousExpiresAt:       ptrext.Of(future),
		HMACAlgo:                "sha256",
	})
	current, previous, prevExpired, err := parseConfig(cfg, passthrough())
	require.NoError(t, err)
	assert.Equal(t, []byte("cur"), current)
	assert.Equal(t, []byte("prev"), previous)
	assert.False(t, prevExpired, "future expiry means previous is NOT expired")
}

func TestParseConfig_PreviousSecretPastExpiry(t *testing.T) {
	t.Parallel()
	past := time.Now().Add(-1 * time.Hour)
	cfg, _ := json.Marshal(Config{
		Version:                 ConfigVersion,
		SecretCurrentEncrypted:  []byte("cur"),
		SecretPreviousEncrypted: []byte("prev"),
		PreviousExpiresAt:       ptrext.Of(past),
		HMACAlgo:                "sha256",
	})
	current, previous, prevExpired, err := parseConfig(cfg, passthrough())
	require.NoError(t, err)
	assert.Equal(t, []byte("cur"), current)
	assert.Equal(t, []byte("prev"), previous)
	assert.True(t, prevExpired, "past expiry means previous IS expired")
}

func TestParseConfig_PreviousSecretNilExpiresAt(t *testing.T) {
	t.Parallel()
	cfg, _ := json.Marshal(Config{
		Version:                 ConfigVersion,
		SecretCurrentEncrypted:  []byte("cur"),
		SecretPreviousEncrypted: []byte("prev"),
		PreviousExpiresAt:       nil,
		HMACAlgo:                "sha256",
	})
	current, previous, prevExpired, err := parseConfig(cfg, passthrough())
	require.NoError(t, err)
	assert.Equal(t, []byte("cur"), current)
	assert.Equal(t, []byte("prev"), previous)
	assert.True(t, prevExpired, "nil PreviousExpiresAt means expired")
}

// ---------- authenticate tests ----------

// buildSourceWithSecret builds an inbound.Source whose Config is a
// double-encrypted webhook config envelope using FakeSecrets.
func buildSourceWithSecret(
	t *testing.T,
	secrets inboundtest.FakeSecrets,
	plainSecret []byte,
	prev []byte,
	prevExpires *time.Time,
) inbound.Source {
	t.Helper()
	encCur, err := secrets.Encrypt(plainSecret)
	require.NoError(t, err)

	cfgMap := map[string]any{
		"version":                  1,
		"secret_current_encrypted": encCur,
		"hmac_algo":                "sha256",
	}
	if prev != nil {
		encPrev, encErr := secrets.Encrypt(prev)
		require.NoError(t, encErr)
		cfgMap["secret_previous_encrypted"] = encPrev
		if prevExpires != nil {
			cfgMap["previous_expires_at"] = prevExpires.Format(time.RFC3339Nano)
		}
	}

	inner, err := json.Marshal(cfgMap)
	require.NoError(t, err)
	enc, err := secrets.Encrypt(inner)
	require.NoError(t, err)

	return inbound.Source{
		ID:       "src-test",
		TenantID: "tenant-test",
		Channel:  channelName,
		Slug:     "test-src",
		Name:     "Test Source",
		Enabled:  true,
		Config:   enc,
	}
}

func newTestAdapter(
	t *testing.T,
	src inbound.Source,
	secrets inbound.SecretStore,
) (*adapter, *inboundtest.FakeMetrics) {
	t.Helper()
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	sources := inboundtest.NewFakeSources()
	sources.Put("test-tenant", src)
	a := &adapter{ // ptrext:allow inbound-handle-identity
		deps: inbound.Deps{
			Mux:     ptrext.Of(inboundtest.FakeMux{}),
			Ingest:  inbound.IngestFunc((&inboundtest.FakeIngest{}).Ingest), // ptrext:allow mutex-identity
			Sources: sources,
			Secrets: secrets,
			Metrics: metrics,
		},
	}
	return a, metrics
}

func TestAuthenticate_ValidCurrentSignature(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	plainSecret := []byte("test-secret-32-bytes-padding-zzz")
	src := buildSourceWithSecret(t, secrets, plainSecret, nil, nil)
	a, _ := newTestAdapter(t, src, secrets)

	body := []byte(`{"content":"hi"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := "sha256=" + computeSig(plainSecret, ts, body)

	_, _, _, ok := a.authenticate(context.Background(), src, ts, body, sig)
	assert.True(t, ok)
}

func TestAuthenticate_InvalidSignature(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	plainSecret := []byte("real-secret-pad-to-32-bytesXXXXX")
	src := buildSourceWithSecret(t, secrets, plainSecret, nil, nil)
	a, metrics := newTestAdapter(t, src, secrets)

	body := []byte(`{"content":"hi"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := "sha256=" + computeSig([]byte("wrong-secret-pad-to-32-bytesXXX"), ts, body)

	status, _, msg, ok := a.authenticate(context.Background(), src, ts, body, sig)
	assert.False(t, ok)
	assert.Equal(t, 401, status)
	assert.Contains(t, msg, "signature or timestamp invalid")
	require.NotEmpty(t, metrics.Totals)
	assert.Contains(t, metrics.Totals[0], "auth_err")
}

func TestAuthenticate_ParseConfigError(t *testing.T) {
	t.Parallel()
	// Give the source a config that will fail to decrypt at the
	// parseConfig level (too short for FakeSecrets.Decrypt).
	src := inbound.Source{
		ID:       "src-bad",
		TenantID: "tenant-bad",
		Channel:  channelName,
		Slug:     "bad",
		Name:     "Bad Config",
		Enabled:  true,
		Config:   []byte("x"),
	}
	secrets := inboundtest.FakeSecrets{}
	a, metrics := newTestAdapter(t, src, secrets)

	status, _, msg, ok := a.authenticate(
		context.Background(), src, "123", []byte("body"), "sha256=abc",
	)
	assert.False(t, ok)
	assert.Equal(t, 500, status)
	assert.Contains(t, msg, "internal error")
	require.NotEmpty(t, metrics.Totals)
	assert.Contains(t, metrics.Totals[0], "internal_err")
}

func TestAuthenticate_DecryptCurrentSecretError(t *testing.T) {
	t.Parallel()
	// Build a config whose outer envelope decrypts fine but the inner
	// secret_current_encrypted is too short for FakeSecrets.Decrypt.
	inner, _ := json.Marshal(Config{
		Version:                ConfigVersion,
		SecretCurrentEncrypted: []byte("x"), // too short
		HMACAlgo:               "sha256",
	})
	secrets := inboundtest.FakeSecrets{}
	enc, err := secrets.Encrypt(inner)
	require.NoError(t, err)

	src := inbound.Source{
		ID: "src-dec", TenantID: "tenant-dec", Channel: channelName,
		Slug: "dec", Name: "Decrypt Fail", Enabled: true, Config: enc,
	}
	a, metrics := newTestAdapter(t, src, secrets)

	status, _, msg, ok := a.authenticate(
		context.Background(), src, "123", []byte("body"), "sha256=abc",
	)
	assert.False(t, ok)
	assert.Equal(t, 500, status)
	assert.Contains(t, msg, "internal error")
	require.NotEmpty(t, metrics.Totals)
	assert.Contains(t, metrics.Totals[0], "internal_err")
}

func TestAuthenticate_FallbackToPreviousSecret(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	currentSecret := []byte("current-secret-32-bytes-paddingX")
	previousSecret := []byte("previous-secret-32-bytes-paddngX")
	future := time.Now().Add(1 * time.Hour)
	src := buildSourceWithSecret(
		t, secrets, currentSecret, previousSecret, ptrext.Of(future),
	)
	a, _ := newTestAdapter(t, src, secrets)

	body := []byte(`{"content":"fallback"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := "sha256=" + computeSig(previousSecret, ts, body)

	_, _, _, ok := a.authenticate(context.Background(), src, ts, body, sig)
	assert.True(t, ok, "should accept previous secret within grace window")
}

func TestAuthenticate_PreviousSecretExpired_NoFallback(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	currentSecret := []byte("current-secret-32-bytes-paddingX")
	previousSecret := []byte("previous-secret-32-bytes-paddngX")
	past := time.Now().Add(-1 * time.Hour)
	src := buildSourceWithSecret(
		t, secrets, currentSecret, previousSecret, ptrext.Of(past),
	)
	a, _ := newTestAdapter(t, src, secrets)

	body := []byte(`{"content":"expired"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := "sha256=" + computeSig(previousSecret, ts, body)

	_, _, _, ok := a.authenticate(context.Background(), src, ts, body, sig)
	assert.False(t, ok, "expired previous secret must not be accepted")
}

// ---------- bindRequest tests ----------

func makeSignedRequest(
	t *testing.T,
	body []byte,
	secret []byte,
	tenantSlug, sourceSlug string,
) *http.Request {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := "sha256=" + computeSig(secret, ts, body)
	r := httptest.NewRequest(
		http.MethodPost,
		"/webhook/"+tenantSlug+"/"+sourceSlug,
		bytes.NewReader(body),
	)
	r.Header.Set(hdrTimestamp, ts)
	r.Header.Set(hdrSignature, sig)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant-slug", tenantSlug)
	rctx.URLParams.Add("source-slug", sourceSlug)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	return r
}

func TestBindRequest_OversizeBody(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	plainSecret := []byte("test-secret-32-bytes-padding-zzz")
	src := buildSourceWithSecret(t, secrets, plainSecret, nil, nil)
	a, metrics := newTestAdapter(t, src, secrets)

	bigBody := bytes.Repeat([]byte("A"), maxBodyBytes+1)
	r := makeSignedRequest(t, bigBody, plainSecret, "test-tenant", "test-src")

	req := ptrext.Of(attunev1.IngestRequest{})
	_, err := a.bindRequest(r, req)
	require.Error(t, err)
	require.NotEmpty(t, metrics.Totals)
	assert.Contains(t, metrics.Totals[0], "validate_err")
}

func TestBindRequest_UnknownSource(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	plainSecret := []byte("test-secret-32-bytes-padding-zzz")
	src := buildSourceWithSecret(t, secrets, plainSecret, nil, nil)
	a, metrics := newTestAdapter(t, src, secrets)
	_ = src // source is stored under test-tenant, but request uses no-tenant

	body := []byte(`{"content":"hello"}`)
	r := makeSignedRequest(t, body, plainSecret, "no-tenant", "no-src")

	req := ptrext.Of(attunev1.IngestRequest{})
	_, err := a.bindRequest(r, req)
	require.Error(t, err)
	require.NotEmpty(t, metrics.Totals)
	assert.Contains(t, metrics.Totals[0], "auth_err")
}

func TestBindRequest_ReadBodyError(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	plainSecret := []byte("test-secret-32-bytes-padding-zzz")
	src := buildSourceWithSecret(t, secrets, plainSecret, nil, nil)
	a, metrics := newTestAdapter(t, src, secrets)

	r := httptest.NewRequest(http.MethodPost, "/webhook/test-tenant/test-src", errReader{})
	r.Header.Set(hdrTimestamp, "123")
	r.Header.Set(hdrSignature, "sha256=abc")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant-slug", "test-tenant")
	rctx.URLParams.Add("source-slug", "test-src")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	req := ptrext.Of(attunev1.IngestRequest{})
	_, err := a.bindRequest(r, req)
	require.Error(t, err)
	require.NotEmpty(t, metrics.Totals)
	assert.Contains(t, metrics.Totals[0], "validate_err")
}

// errReader is an io.Reader that always returns an error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read error") }

func TestBindRequest_DisabledSource(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	plainSecret := []byte("test-secret-32-bytes-padding-zzz")
	src := buildSourceWithSecret(t, secrets, plainSecret, nil, nil)
	src.Enabled = false

	sources := inboundtest.NewFakeSources()
	sources.Put("test-tenant", src)
	metrics := ptrext.Of(inboundtest.FakeMetrics{})
	a := &adapter{ // ptrext:allow inbound-handle-identity
		deps: inbound.Deps{
			Mux:     ptrext.Of(inboundtest.FakeMux{}),
			Ingest:  inbound.IngestFunc((&inboundtest.FakeIngest{}).Ingest), // ptrext:allow mutex-identity
			Sources: sources,
			Secrets: secrets,
			Metrics: metrics,
		},
	}

	body := []byte(`{"content":"hello"}`)
	r := makeSignedRequest(t, body, plainSecret, "test-tenant", "test-src")

	req := ptrext.Of(attunev1.IngestRequest{})
	_, err := a.bindRequest(r, req)
	require.Error(t, err)
	require.NotEmpty(t, metrics.Totals)
	assert.Contains(t, metrics.Totals[0], "auth_err")
}

func TestBindRequest_InvalidJSONBody(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	plainSecret := []byte("test-secret-32-bytes-padding-zzz")
	src := buildSourceWithSecret(t, secrets, plainSecret, nil, nil)
	a, metrics := newTestAdapter(t, src, secrets)

	// Valid auth but body is not valid protojson.
	body := []byte(`not valid json at all!!!`)
	r := makeSignedRequest(t, body, plainSecret, "test-tenant", "test-src")

	req := ptrext.Of(attunev1.IngestRequest{})
	_, err := a.bindRequest(r, req)
	require.Error(t, err)
	require.NotEmpty(t, metrics.Totals)
	assert.Contains(t, metrics.Totals[0], "validate_err")
}

func TestBindRequest_ValidRequest(t *testing.T) {
	t.Parallel()
	secrets := inboundtest.FakeSecrets{}
	plainSecret := []byte("test-secret-32-bytes-padding-zzz")
	src := buildSourceWithSecret(t, secrets, plainSecret, nil, nil)
	a, _ := newTestAdapter(t, src, secrets)

	body := []byte(`{"content":"hello world"}`)
	r := makeSignedRequest(t, body, plainSecret, "test-tenant", "test-src")

	req := ptrext.Of(attunev1.IngestRequest{})
	auth, err := a.bindRequest(r, req)
	require.NoError(t, err)
	assert.Equal(t, "src-test", auth.Source.ID)
	assert.Equal(t, "tenant-test", auth.Source.TenantID)
}

// ---------- readCappedBody tests ----------

func TestReadCappedBody_ExactLimit(t *testing.T) {
	t.Parallel()
	data := strings.Repeat("x", 100)
	got, err := readCappedBody(strings.NewReader(data), 100)
	require.NoError(t, err)
	assert.Equal(t, 100, len(got))
}

func TestReadCappedBody_OverLimit(t *testing.T) {
	t.Parallel()
	data := strings.Repeat("x", 101)
	_, err := readCappedBody(strings.NewReader(data), 100)
	require.Error(t, err)
}

func TestReadCappedBody_Empty(t *testing.T) {
	t.Parallel()
	got, err := readCappedBody(strings.NewReader(""), 100)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestReadCappedBody_ReadError(t *testing.T) {
	t.Parallel()
	_, err := readCappedBody(errReader{}, 100)
	require.Error(t, err)
}

// ---------- ProcessStubSecret tests ----------

func TestProcessStubSecret_ReturnsNonEmpty(t *testing.T) {
	t.Parallel()
	s := ProcessStubSecret()
	assert.NotEmpty(t, s)
	assert.Equal(t, 32, len(s), "stub secret should be 32 bytes")
}

func TestProcessStubSecret_Idempotent(t *testing.T) {
	t.Parallel()
	a := ProcessStubSecret()
	b := ProcessStubSecret()
	assert.Equal(t, a, b, "sync.Once should return the same slice")
}

func TestVerifyHMACAgainstStub_AlwaysFalseWithRealStub(t *testing.T) {
	t.Parallel()
	stub := ProcessStubSecret()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte("test")
	sig := "sha256=" + computeSig(stub, ts, body)
	// Even with a valid signature, the stub verify must return false.
	result := verifyHMACAgainstStub(stub, ts, body, sig)
	assert.False(t, result, "verifyHMACAgainstStub must always return false")
}

// Compile-time interface checks.
var _ io.Reader = errReader{}
