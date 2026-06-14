//go:build integration

package llmconfig_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/inbound/adapter/webhook"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/repo/admin"
	llmconfigrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	llmconfigsvc "github.com/Phixsura/attune/internal/service/llmconfig"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_SecretKeyRegistryRejectsFingerprintMismatch(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := llmconfigrepo.New(pool)
	ctx := context.Background()

	row := llmconfigrepo.KeyRegistryRow{
		KeyID:              "123",
		Primary:            true,
		Status:             "ENABLED",
		TypeURL:            "type.googleapis.com/google.crypto.tink.AesGcmKey",
		OutputPrefixType:   "TINK",
		KeyMaterialType:    "SYMMETRIC",
		FingerprintSHA256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		FingerprintVersion: 1,
	}
	if err := repo.SyncKeyRegistry(ctx, []llmconfigrepo.KeyRegistryRow{row}); err != nil {
		t.Fatalf("initial SyncKeyRegistry: %v", err)
	}

	row.FingerprintSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	err := repo.SyncKeyRegistry(ctx, []llmconfigrepo.KeyRegistryRow{row})
	if !errors.Is(err, llmconfigrepo.ErrSecretKeyFingerprint) {
		t.Fatalf("SyncKeyRegistry err = %v; want %v", err, llmconfigrepo.ErrSecretKeyFingerprint)
	}
}

func TestPG_ChannelCRUDPersistsEncryptedKeyAndZeroWeights(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repo := llmconfigrepo.New(pool)
	store := newTinkStore(t)
	svc := llmconfigsvc.NewService(repo, store)
	if err := svc.SyncKeyRegistry(ctx); err != nil {
		t.Fatalf("SyncKeyRegistry: %v", err)
	}

	apiKey := "sk-prod-like"
	channel, err := svc.CreateChannel(ctx, llmconfigsvc.ChannelInput{
		Name:           "prod-openai",
		Protocol:       llmconfigrepo.ProtocolOpenAICompat,
		BaseURL:        "https://api.openai.com",
		AuthMode:       llmconfigrepo.AuthModeBearer,
		APIKey:         &apiKey,
		Status:         llmconfigrepo.ChannelStatusEnabled,
		Priority:       100,
		Weight:         intPtr(0),
		TimeoutSeconds: intPtr(45),
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	assertChannelSecret(t, store, channel, apiKey)
	if channel.Weight != 0 || channel.TimeoutSeconds != 45 {
		t.Fatalf("channel weight/timeout = %d/%d; want 0/45", channel.Weight, channel.TimeoutSeconds)
	}

	updated, err := svc.UpdateChannel(ctx, channel.ID, llmconfigsvc.ChannelInput{
		Name:     "prod-openai-renamed",
		Protocol: llmconfigrepo.ProtocolOpenAICompat,
		BaseURL:  "https://api.openai.com",
		AuthMode: llmconfigrepo.AuthModeBearer,
		Status:   llmconfigrepo.ChannelStatusEnabled,
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}
	if updated.Weight != 0 || updated.TimeoutSeconds != 45 {
		t.Fatalf("updated weight/timeout = %d/%d; want preserved 0/45", updated.Weight, updated.TimeoutSeconds)
	}

	ability, err := svc.UpsertAbility(ctx, channel.ID, llmconfigsvc.AbilityInput{
		LogicalModel:  "enrich-default",
		ProviderModel: "gpt-4o-mini",
		Enabled:       true,
		Priority:      100,
		Weight:        intPtr(0),
	})
	if err != nil {
		t.Fatalf("UpsertAbility: %v", err)
	}
	if ability.Weight != 0 {
		t.Fatalf("ability weight = %d; want 0", ability.Weight)
	}
	if _, err := svc.UpsertRoute(ctx, llmconfigsvc.RouteInput{
		Purpose:      "enrich",
		LogicalModel: "enrich-default",
		Enabled:      true,
	}); err != nil {
		t.Fatalf("UpsertRoute: %v", err)
	}
	candidates, err := repo.ResolveCandidates(ctx, "", "enrich")
	if err != nil {
		t.Fatalf("ResolveCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Ability.Weight != 0 || candidates[0].Channel.Weight != 0 {
		t.Fatalf("candidate weights = %+v; want one zero-weight candidate", candidates)
	}
}

func TestPG_DisabledTenantRouteBlocksGlobalFallback(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repo := llmconfigrepo.New(pool)
	svc := llmconfigsvc.NewService(repo, newTinkStore(t))
	channel, err := svc.CreateChannel(ctx, llmconfigsvc.ChannelInput{
		Name:           "local-openai",
		Protocol:       llmconfigrepo.ProtocolOpenAICompat,
		BaseURL:        "http://localhost:11434",
		AuthMode:       llmconfigrepo.AuthModeNone,
		Status:         llmconfigrepo.ChannelStatusEnabled,
		Priority:       100,
		Weight:         intPtr(1),
		TimeoutSeconds: intPtr(45),
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if _, err := svc.UpsertAbility(ctx, channel.ID, llmconfigsvc.AbilityInput{
		LogicalModel:  "enrich-default",
		ProviderModel: "gpt-4o-mini",
		Enabled:       true,
		Priority:      100,
		Weight:        intPtr(1),
	}); err != nil {
		t.Fatalf("UpsertAbility: %v", err)
	}
	if _, err := svc.UpsertRoute(ctx, llmconfigsvc.RouteInput{
		Purpose:      "enrich",
		LogicalModel: "enrich-default",
		Enabled:      true,
	}); err != nil {
		t.Fatalf("UpsertRoute global: %v", err)
	}
	tenantID := "tenant-route-disabled"
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`,
		tenantID, "tenant-route-disabled", "Tenant Route Disabled",
	); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := svc.UpsertRoute(ctx, llmconfigsvc.RouteInput{
		TenantID:     tenantID,
		Purpose:      "enrich",
		LogicalModel: "enrich-default",
		Enabled:      false,
	}); err != nil {
		t.Fatalf("UpsertRoute disabled tenant override: %v", err)
	}
	if _, err := repo.ResolveCandidates(ctx, tenantID, "enrich"); !errors.Is(err, llmconfigrepo.ErrNoCandidates) {
		t.Fatalf("disabled tenant route ResolveCandidates err = %v; want %v", err, llmconfigrepo.ErrNoCandidates)
	}
	candidates, err := repo.ResolveCandidates(ctx, "", "enrich")
	if err != nil {
		t.Fatalf("ResolveCandidates global after tenant override: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Route.TenantID != "" {
		t.Fatalf("global route candidates after tenant override = %+v; want global candidate", candidates)
	}
}

func TestPG_LocalNoAuthChannelStoresNoCredential(t *testing.T) {
	ctx := context.Background()
	svc := llmconfigsvc.NewService(llmconfigrepo.New(testdb.NewPool(t)), newTinkStore(t))
	channel, err := svc.CreateChannel(ctx, llmconfigsvc.ChannelInput{
		Name:           "local",
		Protocol:       llmconfigrepo.ProtocolOpenAICompat,
		BaseURL:        "http://localhost:11434",
		AuthMode:       llmconfigrepo.AuthModeNone,
		Status:         llmconfigrepo.ChannelStatusEnabled,
		Weight:         intPtr(1),
		TimeoutSeconds: intPtr(10),
	})
	if err != nil {
		t.Fatalf("CreateChannel local: %v", err)
	}
	if channel.CredentialKeyID != "" || len(channel.CredentialCiphertext) != 0 {
		t.Fatalf("local no-auth credential = key_id %q len %d; want empty", channel.CredentialKeyID, len(channel.CredentialCiphertext))
	}
}

func TestPG_ConsoleLLMConfigAPIUsesRealDBAndWriteOnlyKeys(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	store := newTinkStore(t)
	svc := llmconfigsvc.NewService(llmconfigrepo.New(pool), store)
	if err := svc.SyncKeyRegistry(ctx); err != nil {
		t.Fatalf("SyncKeyRegistry: %v", err)
	}
	mux, signer := newConsoleRouter(t, pool, svc)

	create := postJSON(t, mux, signer, http.MethodPost, "/llm/channels", `{
		"name":"console-openai",
		"protocol":"openai-compat",
		"baseUrl":"https://api.openai.com",
		"authMode":"bearer",
		"apiKey":"sk-console",
		"status":"enabled",
		"priority":100,
		"weight":0,
		"timeoutSeconds":30
	}`)
	if create.Code != http.StatusOK {
		t.Fatalf("create channel status=%d body=%s", create.Code, create.Body.String())
	}
	body := decodeObject(t, create)
	channelID := stringField(t, body, "id")
	if _, ok := body["apiKey"]; ok {
		t.Fatalf("response leaked apiKey: %s", create.Body.String())
	}
	if body["hasApiKey"] != true || body["weight"] != float64(0) {
		t.Fatalf("create response = %#v; want hasApiKey true weight 0", body)
	}

	ability := postJSON(t, mux, signer, http.MethodPut, "/llm/channels/"+channelID+"/abilities", `{
		"logicalModel":"enrich-default",
		"providerModel":"gpt-4o-mini",
		"enabled":true,
		"priority":100,
		"weight":0
	}`)
	if ability.Code != http.StatusOK {
		t.Fatalf("upsert ability status=%d body=%s", ability.Code, ability.Body.String())
	}
	if got := decodeObject(t, ability)["weight"]; got != float64(0) {
		t.Fatalf("ability weight = %#v; want 0", got)
	}

	route := postJSON(t, mux, signer, http.MethodPut, "/llm/routes", `{
		"purpose":"enrich",
		"logicalModel":"enrich-default",
		"enabled":true
	}`)
	if route.Code != http.StatusOK {
		t.Fatalf("upsert route status=%d body=%s", route.Code, route.Body.String())
	}
}

func TestPG_SyncKeyRegistryRejectsLLMCredentialKeyIDMismatch(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	oldRaw, combinedRaw, _ := rotationKeysets(t)
	oldStore := mustTinkStoreFromJSON(t, oldRaw)
	combinedStore := mustTinkStoreFromJSON(t, combinedRaw)
	channelID := uuid.New()
	value, err := oldStore.EncryptValue(
		[]byte("sk-old"),
		secretstore.AssociatedData("llm_channel", channelID.String(), "api_key"),
	)
	if err != nil {
		t.Fatalf("encrypt old llm credential: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO llm_channels
		 (id, name, protocol, base_url, auth_mode, credential_key_id,
		  credential_ciphertext, status, priority, weight, timeout_seconds)
		VALUES ($1, 'mismatched key id', 'openai-compat', 'http://localhost:11434',
		        'bearer', $2, $3, 'enabled', 0, 1, 60)`,
		channelID, combinedStore.PrimaryKeyID(), value.Ciphertext,
	); err != nil {
		t.Fatalf("insert mismatched llm channel: %v", err)
	}

	err = llmconfigsvc.NewService(llmconfigrepo.New(pool), combinedStore).SyncKeyRegistry(ctx)
	if !errors.Is(err, llmconfigrepo.ErrSecretKeyMismatch) {
		t.Fatalf("SyncKeyRegistry err = %v; want %v", err, llmconfigrepo.ErrSecretKeyMismatch)
	}
}

func TestPG_SyncKeyRegistryRejectsNestedInboundSecretWithMissingKey(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	oldRaw, combinedRaw, oldKeyID := rotationKeysets(t)
	oldStore := mustTinkStoreFromJSON(t, oldRaw)
	combinedStore := mustTinkStoreFromJSON(t, combinedRaw)
	newOnlyRaw, err := secretstore.DeleteKeyFromKeysetJSON(combinedRaw, oldKeyID)
	if err != nil {
		t.Fatalf("DeleteKeyFromKeysetJSON: %v", err)
	}
	newOnlyStore := mustTinkStoreFromJSON(t, newOnlyRaw)

	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`,
		"tenant-nested-key-check", "nested-key-check", "Nested Key Check",
	); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	current, err := oldStore.EncryptValue([]byte("current-secret"), nil)
	if err != nil {
		t.Fatalf("encrypt nested webhook secret: %v", err)
	}
	raw, err := json.Marshal(webhook.Config{
		Version:                webhook.ConfigVersion,
		SecretCurrentEncrypted: current.Ciphertext,
		HMACAlgo:               "sha256",
	})
	if err != nil {
		t.Fatalf("marshal webhook config: %v", err)
	}
	outer, err := combinedStore.EncryptValue(raw, nil)
	if err != nil {
		t.Fatalf("encrypt outer webhook config: %v", err)
	}
	sourceID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO inbound_sources (id, tenant_id, channel, name, slug, config, enabled)
		VALUES ($1, 'tenant-nested-key-check', 'webhook', 'nested key check', 'nested-key-check', $2, TRUE)`,
		sourceID, outer.Ciphertext,
	); err != nil {
		t.Fatalf("insert inbound source: %v", err)
	}

	err = llmconfigsvc.NewService(llmconfigrepo.New(pool), newOnlyStore).SyncKeyRegistry(ctx)
	if !errors.Is(err, llmconfigrepo.ErrSecretKeyUnavailable) {
		t.Fatalf("SyncKeyRegistry err = %v; want %v", err, llmconfigrepo.ErrSecretKeyUnavailable)
	}
	if !strings.Contains(err.Error(), "secret_current_encrypted") {
		t.Fatalf("SyncKeyRegistry err = %v; want nested webhook location", err)
	}
}

func newConsoleRouter(
	t *testing.T,
	_ *pgxpool.Pool,
	svc *llmconfigsvc.Service,
) (http.Handler, *console.Signer) {
	t.Helper()
	signer, err := console.NewSigner("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	router := console.NewRouter(
		signer,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		console.NewLLMConfigHandler(svc),
		nil,
		nil, // digestSubscription
		fakeAdminReader{},
	)
	return router.Mount(), signer
}

type fakeAdminReader struct{}

func (fakeAdminReader) GetByID(context.Context, string) (admin.Admin, error) {
	return admin.Admin{ID: "user-1", Role: "admin"}, nil
}

func postJSON(
	t *testing.T,
	mux http.Handler,
	signer *console.Signer,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := authedRequest(t, signer, method, path, body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func authedRequest(
	t *testing.T,
	signer *console.Signer,
	method string,
	path string,
	body string,
) *http.Request {
	t.Helper()
	const userID = "user-1"
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", signer.CSRFToken(userID))
	w := httptest.NewRecorder()
	if err := signer.IssueSessionCookie(cookieSinkRecorder{ResponseRecorder: w}, "tenant-1", userID); err != nil {
		t.Fatalf("IssueSessionCookie: %v", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("IssueSessionCookie did not set a cookie")
	}
	req.AddCookie(cookies[0])
	return req
}

type cookieSinkRecorder struct {
	*httptest.ResponseRecorder
}

func (c cookieSinkRecorder) SetCookie(cookie *http.Cookie) {
	http.SetCookie(c.ResponseRecorder, cookie)
}

func decodeObject(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode json body %q: %v", w.Body.String(), err)
	}
	return out
}

func stringField(t *testing.T, body map[string]any, field string) string {
	t.Helper()
	value, ok := body[field].(string)
	if !ok || value == "" {
		t.Fatalf("body[%s] = %#v; want non-empty string", field, body[field])
	}
	return value
}

func newTinkStore(t *testing.T) *secretstore.TinkStore {
	t.Helper()
	raw, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatalf("GenerateAES256GCMKeysetJSON: %v", err)
	}
	return mustTinkStoreFromJSON(t, raw)
}

func mustTinkStoreFromJSON(t *testing.T, raw string) *secretstore.TinkStore {
	t.Helper()
	store, err := secretstore.NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatalf("NewTinkStoreFromJSON: %v", err)
	}
	return store
}

func rotationKeysets(t *testing.T) (oldRaw string, combinedRaw string, oldKeyID string) {
	t.Helper()
	oldRaw, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatalf("GenerateAES256GCMKeysetJSON: %v", err)
	}
	oldStore := mustTinkStoreFromJSON(t, oldRaw)
	combinedRaw, _, err = secretstore.AddAES256GCMKeyToKeysetJSON(oldRaw, true)
	if err != nil {
		t.Fatalf("AddAES256GCMKeyToKeysetJSON: %v", err)
	}
	return oldRaw, combinedRaw, oldStore.PrimaryKeyID()
}

func assertChannelSecret(
	t *testing.T,
	store *secretstore.TinkStore,
	channel *llmconfigrepo.Channel,
	want string,
) {
	t.Helper()
	plaintext, err := store.DecryptValue(secretstore.EncryptedValue{
		KeyID:      channel.CredentialKeyID,
		Ciphertext: channel.CredentialCiphertext,
	}, secretstore.AssociatedData("llm_channel", channel.ID.String(), "api_key"))
	if err != nil {
		t.Fatalf("DecryptValue: %v", err)
	}
	if string(plaintext) != want {
		t.Fatalf("plaintext = %q; want %q", plaintext, want)
	}
}

func intPtr(v int) *int {
	return &v
}
