// SPDX-License-Identifier: Apache-2.0

package llmconfig

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
)

type fakeRepo struct {
	created          llmrepo.Channel
	updated          llmrepo.Channel
	existing         *llmrepo.Channel
	abilities        []llmrepo.Ability
	hasEligibleRoute bool
	tenantExists     bool
	testStatus       string
	testError        string
	updateCredential bool
	validatedKeyIDs  []string
}

func (r *fakeRepo) SyncKeyRegistry(context.Context, []llmrepo.KeyRegistryRow) error { return nil }
func (r *fakeRepo) ValidateSecretKeyReferences(_ context.Context, keyIDs []string) error {
	r.validatedKeyIDs = append([]string(nil), keyIDs...)
	return nil
}

func (r *fakeRepo) ListInboundSecretConfigs(context.Context) ([]llmrepo.InboundConfigRef, error) {
	return nil, nil
}
func (r *fakeRepo) ListChannels(context.Context) ([]llmrepo.Channel, error) { return nil, nil }
func (r *fakeRepo) GetChannel(context.Context, uuid.UUID) (*llmrepo.Channel, error) {
	if r.existing != nil {
		ch := ptrext.Indirect(r.existing)
		return ptrext.Of(ch), nil
	}
	return nil, llmrepo.ErrChannelNotFound
}

func (r *fakeRepo) CreateChannel(_ context.Context, ch llmrepo.Channel) (*llmrepo.Channel, error) {
	if ch.ID == uuid.Nil {
		ch.ID = uuid.New()
	}
	r.created = ch
	return ptrext.Of(ch), nil
}

func (r *fakeRepo) UpdateChannel(_ context.Context, ch llmrepo.Channel, updateCredential bool) (*llmrepo.Channel, error) {
	r.updated = ch
	r.updateCredential = updateCredential
	return ptrext.Of(ch), nil
}

func (r *fakeRepo) UpdateChannelTestResult(
	_ context.Context,
	id uuid.UUID,
	status string,
	lastError string,
) (*llmrepo.Channel, error) {
	r.testStatus = status
	r.testError = lastError
	if r.existing == nil {
		return nil, llmrepo.ErrChannelNotFound
	}
	ch := ptrext.Indirect(r.existing)
	ch.ID = id
	ch.LastTestStatus = status
	ch.LastError = lastError
	return ptrext.Of(ch), nil
}
func (r *fakeRepo) DeleteChannel(context.Context, uuid.UUID) error { return nil }
func (r *fakeRepo) ListAbilities(context.Context, uuid.UUID) ([]llmrepo.Ability, error) {
	return r.abilities, nil
}

func (r *fakeRepo) UpsertAbility(context.Context, llmrepo.Ability) (*llmrepo.Ability, error) {
	return nil, nil
}
func (r *fakeRepo) DeleteAbility(context.Context, uuid.UUID, string) error { return nil }
func (r *fakeRepo) HasEligibleAbility(context.Context, string) (bool, error) {
	return r.hasEligibleRoute, nil
}
func (r *fakeRepo) TenantExists(context.Context, string) (bool, error)  { return r.tenantExists, nil }
func (r *fakeRepo) ListRoutes(context.Context) ([]llmrepo.Route, error) { return nil, nil }
func (r *fakeRepo) UpsertRoute(context.Context, llmrepo.Route) (*llmrepo.Route, error) {
	return nil, nil
}
func (r *fakeRepo) DeleteRoute(context.Context, string, string) error { return nil }

type fakeStore struct {
	gotPlaintext []byte
	gotAAD       []byte
	decryptAAD   []byte
	decryptErr   error
}

func (s *fakeStore) EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error) {
	s.gotPlaintext = append([]byte(nil), plaintext...)
	s.gotAAD = append([]byte(nil), aad...)
	return secretstore.EncryptedValue{KeyID: "123", Ciphertext: []byte("ciphertext")}, nil
}

func (s *fakeStore) DecryptValue(_ secretstore.EncryptedValue, aad []byte) ([]byte, error) {
	s.decryptAAD = append([]byte(nil), aad...)
	if s.decryptErr != nil {
		return nil, s.decryptErr
	}
	return []byte("sk-test"), nil
}

func (s *fakeStore) Keys() []secretstore.KeyMetadata {
	return []secretstore.KeyMetadata{{KeyID: "123", Primary: true, Status: "ENABLED"}}
}

type fakeBackend struct {
	err error
}

func (b fakeBackend) Complete(context.Context, llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	if b.err != nil {
		return llmclient.CompletionResponse{}, b.err
	}
	return llmclient.CompletionResponse{
		Text:  "attune-ok",
		Usage: llmclient.Usage{InputTokens: 3, OutputTokens: 2},
	}, nil
}

func (b fakeBackend) Close() error { return nil }

func TestCreateChannelEncryptsWriteOnlyAPIKey(t *testing.T) {
	repo := ptrext.Of(fakeRepo{})
	store := ptrext.Of(fakeStore{})
	svc := NewService(repo, store)
	apiKey := "sk-test"
	row, err := svc.CreateChannel(context.Background(), ChannelInput{
		Name:           "primary",
		Protocol:       llmrepo.ProtocolOpenAICompat,
		BaseURL:        "https://api.openai.com",
		AuthMode:       llmrepo.AuthModeBearer,
		APIKey:         ptrext.Of(apiKey),
		Status:         llmrepo.ChannelStatusEnabled,
		Weight:         intPtr(1),
		TimeoutSeconds: intPtr(60),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(store.gotPlaintext, []byte("sk-test")) {
		t.Fatalf("plaintext = %q", store.gotPlaintext)
	}
	wantAAD := channelCredentialAAD(row.ID)
	if !bytes.Equal(store.gotAAD, wantAAD) {
		t.Fatalf("aad = %q; want %q", store.gotAAD, wantAAD)
	}
	if string(repo.created.CredentialCiphertext) != "ciphertext" {
		t.Fatalf("ciphertext not persisted: %+v", repo.created)
	}
}

func TestCreateChannelNoAuthDoesNotRequireAPIKey(t *testing.T) {
	repo := ptrext.Of(fakeRepo{})
	svc := NewService(repo, ptrext.Of(fakeStore{}))
	row, err := svc.CreateChannel(context.Background(), ChannelInput{
		Name:           "local",
		Protocol:       llmrepo.ProtocolOpenAICompat,
		BaseURL:        "http://localhost:11434",
		AuthMode:       llmrepo.AuthModeNone,
		Status:         llmrepo.ChannelStatusEnabled,
		Weight:         intPtr(1),
		TimeoutSeconds: intPtr(60),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(row.CredentialCiphertext) != 0 || row.CredentialKeyID != "" {
		t.Fatalf("no-auth channel should not persist credential: %+v", row)
	}
}

func TestCreateChannelRejectsPublicHTTPBaseURL(t *testing.T) {
	svc := NewService(ptrext.Of(fakeRepo{}), ptrext.Of(fakeStore{}))
	apiKey := "sk-test"
	_, err := svc.CreateChannel(context.Background(), ChannelInput{
		Name:           "public-http",
		Protocol:       llmrepo.ProtocolOpenAICompat,
		BaseURL:        "http://api.example.com",
		AuthMode:       llmrepo.AuthModeBearer,
		APIKey:         ptrext.Of(apiKey),
		Status:         llmrepo.ChannelStatusEnabled,
		Weight:         intPtr(1),
		TimeoutSeconds: intPtr(60),
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v; want validation", err)
	}
}

func TestUpdateChannelPreservesCredentialWhenAPIKeyOmitted(t *testing.T) {
	channelID := uuid.New()
	repo := ptrext.Of(fakeRepo{
		existing: ptrext.Of(llmrepo.Channel{
			ID:                   channelID,
			Name:                 "primary",
			Protocol:             llmrepo.ProtocolOpenAICompat,
			BaseURL:              "https://api.openai.com",
			AuthMode:             llmrepo.AuthModeBearer,
			CredentialKeyID:      "123",
			CredentialCiphertext: []byte("ciphertext"),
			Status:               llmrepo.ChannelStatusEnabled,
			Weight:               1,
			TimeoutSeconds:       60,
		}),
	})
	svc := NewService(repo, ptrext.Of(fakeStore{}))

	row, err := svc.UpdateChannel(context.Background(), channelID, ChannelInput{
		Name:           "renamed",
		Protocol:       llmrepo.ProtocolOpenAICompat,
		BaseURL:        "https://api.openai.com",
		AuthMode:       llmrepo.AuthModeBearer,
		Status:         llmrepo.ChannelStatusEnabled,
		Weight:         intPtr(1),
		TimeoutSeconds: intPtr(60),
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.updateCredential {
		t.Fatal("UpdateChannel should not rewrite credential when api_key is omitted")
	}
	if row.CredentialKeyID != "123" || string(row.CredentialCiphertext) != "ciphertext" {
		t.Fatalf("credential = key %q ciphertext %q; want preserved", row.CredentialKeyID, row.CredentialCiphertext)
	}
}

func TestSyncKeyRegistryValidatesLocalKeys(t *testing.T) {
	repo := ptrext.Of(fakeRepo{})
	svc := NewService(repo, ptrext.Of(fakeStore{}))
	if err := svc.SyncKeyRegistry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.validatedKeyIDs) != 1 || repo.validatedKeyIDs[0] != "123" {
		t.Fatalf("validated key ids = %#v; want [123]", repo.validatedKeyIDs)
	}
}

func TestCheckInboundCiphertextPrefersKnownTinkKeyOverLegacyHeader(t *testing.T) {
	svc := NewService(ptrext.Of(fakeRepo{}), ptrext.Of(fakeStore{}))
	validation := ptrext.Of(secretReferenceValidation{})
	ciphertext := append([]byte{0x01, 0x00, 0x00, 0x00, 0x7b}, make([]byte, 30)...)
	ok, err := svc.checkInboundCiphertext(
		ciphertext,
		map[string]struct{}{"123": {}},
		"inbound_sources.config.webhook.secret_current_encrypted",
		"row-1",
		validation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("known Tink key should be decryptable, not treated as legacy")
	}
	if len(validation.missing) != 0 || len(validation.undetectable) != 0 {
		t.Fatalf("validation = %+v; want no findings", validation)
	}
}

func TestCheckInboundCiphertextRejectsKnownKeyThatCannotDecrypt(t *testing.T) {
	svc := NewService(ptrext.Of(fakeRepo{}), ptrext.Of(fakeStore{
		decryptErr: errors.New("bad tag"),
	}))
	validation := ptrext.Of(secretReferenceValidation{})
	ciphertext := append([]byte{0x01, 0x00, 0x00, 0x00, 0x7b}, make([]byte, 30)...)
	ok, err := svc.checkInboundCiphertext(
		ciphertext,
		map[string]struct{}{"123": {}},
		"inbound_sources.config.webhook.secret_current_encrypted",
		"row-1",
		validation,
	)
	if err == nil || !strings.Contains(err.Error(), "decrypt inbound secret") {
		t.Fatalf("err = %v; want decrypt error", err)
	}
	if ok {
		t.Fatal("ok = true; want decrypt failure")
	}
}

func TestTestChannelUsesEnabledAbilityAndPersistsSuccess(t *testing.T) {
	channelID := uuid.New()
	repo := ptrext.Of(fakeRepo{
		existing: ptrext.Of(llmrepo.Channel{
			ID:                   channelID,
			Name:                 "primary",
			Protocol:             llmrepo.ProtocolOpenAICompat,
			BaseURL:              "http://localhost:11434",
			AuthMode:             llmrepo.AuthModeBearer,
			CredentialKeyID:      "123",
			CredentialCiphertext: []byte("ciphertext"),
			TimeoutSeconds:       1,
		}),
		abilities: []llmrepo.Ability{
			{ProviderModel: "low", Enabled: true, Priority: 0},
			{ProviderModel: "high", Enabled: true, Priority: 10},
		},
	})
	store := ptrext.Of(fakeStore{})
	var gotModel string
	svc := newService(repo, store, func(_, _, _ string) (llmclient.LLMClient, error) {
		return captureBackend{setModel: func(model string) { gotModel = model }}, nil
	})

	result, err := svc.TestChannel(context.Background(), channelID, TestChannelInput{})
	if err != nil {
		t.Fatal(err)
	}
	if gotModel != "high" || result.ProviderModel != "high" {
		t.Fatalf("provider model = got %q result %q; want high", gotModel, result.ProviderModel)
	}
	if repo.testStatus != "ok" || repo.testError != "" {
		t.Fatalf("test status = %q err=%q; want ok", repo.testStatus, repo.testError)
	}
	if !bytes.Equal(store.decryptAAD, channelCredentialAAD(channelID)) {
		t.Fatalf("decrypt aad = %q; want %q", store.decryptAAD, channelCredentialAAD(channelID))
	}
}

func TestTestChannelPersistsFailure(t *testing.T) {
	channelID := uuid.New()
	repo := ptrext.Of(fakeRepo{
		existing: ptrext.Of(llmrepo.Channel{
			ID:             channelID,
			Name:           "primary",
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 1,
		}),
		abilities: []llmrepo.Ability{{ProviderModel: "m", Enabled: true}},
	})
	svc := newService(repo, ptrext.Of(fakeStore{}), func(_, _, _ string) (llmclient.LLMClient, error) {
		return fakeBackend{err: errors.New("upstream unavailable")}, nil
	})

	_, err := svc.TestChannel(context.Background(), channelID, TestChannelInput{})
	if err == nil {
		t.Fatal("expected test failure")
	}
	if repo.testStatus != "failed" || repo.testError != "provider request failed" {
		t.Fatalf("test status = %q err=%q", repo.testStatus, repo.testError)
	}
}

func TestListChannelModelsUsesStoredCredential(t *testing.T) {
	channelID := uuid.New()
	repo := ptrext.Of(fakeRepo{
		existing: ptrext.Of(llmrepo.Channel{
			ID:                   channelID,
			Protocol:             llmrepo.ProtocolOpenAICompat,
			BaseURL:              "https://example.test",
			AuthMode:             llmrepo.AuthModeBearer,
			CredentialKeyID:      "123",
			CredentialCiphertext: []byte("ciphertext"),
			TimeoutSeconds:       1,
		}),
	})
	store := ptrext.Of(fakeStore{})
	svc := NewService(repo, store)
	var gotProtocol, gotBaseURL, gotAPIKey string
	svc.modelLister = func(
		_ context.Context,
		protocol string,
		baseURL string,
		apiKey string,
	) ([]ProviderModel, error) {
		gotProtocol = protocol
		gotBaseURL = baseURL
		gotAPIKey = apiKey
		return []ProviderModel{{ID: "gpt-4o-mini"}}, nil
	}

	models, err := svc.ListChannelModels(context.Background(), channelID)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-4o-mini" {
		t.Fatalf("models = %#v", models)
	}
	if gotProtocol != llmrepo.ProtocolOpenAICompat || gotBaseURL != "https://example.test" {
		t.Fatalf("lister got protocol=%q baseURL=%q", gotProtocol, gotBaseURL)
	}
	if gotAPIKey != "sk-test" {
		t.Fatalf("api key = %q; want decrypted key", gotAPIKey)
	}
	if !bytes.Equal(store.decryptAAD, channelCredentialAAD(channelID)) {
		t.Fatalf("decrypt aad = %q; want %q", store.decryptAAD, channelCredentialAAD(channelID))
	}
}

func TestUpsertRouteRequiresEligibleAbilityWhenEnabled(t *testing.T) {
	svc := NewService(ptrext.Of(fakeRepo{}), ptrext.Of(fakeStore{}))
	_, err := svc.UpsertRoute(context.Background(), RouteInput{
		Purpose:      "enrich",
		LogicalModel: "enrich-default",
		Enabled:      true,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v; want validation", err)
	}
}

func TestUpsertRouteAllowsEligibleAbility(t *testing.T) {
	repo := ptrext.Of(fakeRepo{hasEligibleRoute: true})
	svc := NewService(repo, ptrext.Of(fakeStore{}))
	_, err := svc.UpsertRoute(context.Background(), RouteInput{
		Purpose:      "enrich",
		LogicalModel: "enrich-default",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpsertTenantRouteRequiresActiveTenantEvenWhenDisabled(t *testing.T) {
	repo := ptrext.Of(fakeRepo{hasEligibleRoute: true})
	svc := NewService(repo, ptrext.Of(fakeStore{}))
	_, err := svc.UpsertRoute(context.Background(), RouteInput{
		TenantID:     "missing-tenant",
		Purpose:      "enrich",
		LogicalModel: "enrich-default",
		Enabled:      false,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v; want validation", err)
	}
}

type captureBackend struct {
	setModel func(string)
}

func (b captureBackend) Complete(_ context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	b.setModel(req.Model)
	return fakeBackend{}.Complete(context.Background(), req)
}

func (b captureBackend) Close() error { return nil }

func intPtr(v int) *int {
	return ptrext.Of(v)
}
