// SPDX-License-Identifier: Apache-2.0

// Package llmconfig implements write-only managed LLM channel operations.
package llmconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
)

var (
	ErrValidation = errors.New("llmconfig: validation failed")
	ErrNoAPIKey   = errors.New("llmconfig: api_key is required for bearer auth")
)

type Repo interface {
	SyncKeyRegistry(ctx context.Context, rows []llmrepo.KeyRegistryRow) error
	ValidateSecretKeyReferences(ctx context.Context, keyIDs []string) error
	ListInboundSecretConfigs(ctx context.Context) ([]llmrepo.InboundConfigRef, error)
	ListChannels(ctx context.Context) ([]llmrepo.Channel, error)
	GetChannel(ctx context.Context, id uuid.UUID) (*llmrepo.Channel, error)
	CreateChannel(ctx context.Context, ch llmrepo.Channel) (*llmrepo.Channel, error)
	UpdateChannel(ctx context.Context, ch llmrepo.Channel, updateCredential bool) (*llmrepo.Channel, error)
	UpdateChannelTestResult(ctx context.Context, id uuid.UUID, status string, lastError string) (*llmrepo.Channel, error)
	DeleteChannel(ctx context.Context, id uuid.UUID) error
	ListAbilities(ctx context.Context, channelID uuid.UUID) ([]llmrepo.Ability, error)
	UpsertAbility(ctx context.Context, a llmrepo.Ability) (*llmrepo.Ability, error)
	DeleteAbility(ctx context.Context, channelID uuid.UUID, logicalModel string) error
	HasEligibleAbility(ctx context.Context, logicalModel string) (bool, error)
	TenantExists(ctx context.Context, tenantID string) (bool, error)
	ListRoutes(ctx context.Context) ([]llmrepo.Route, error)
	UpsertRoute(ctx context.Context, route llmrepo.Route) (*llmrepo.Route, error)
	DeleteRoute(ctx context.Context, tenantID, purpose string) error
}

type Store interface {
	EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error)
	DecryptValue(value secretstore.EncryptedValue, aad []byte) ([]byte, error)
	Keys() []secretstore.KeyMetadata
}

type Service struct {
	repo           Repo
	store          Store
	backendFactory backendFactory
	modelLister    modelLister
}

type ChannelInput struct {
	Name           string
	Protocol       string
	BaseURL        string
	AuthMode       string
	APIKey         *string
	Status         string
	Priority       int
	Weight         *int
	TimeoutSeconds *int
}

type AbilityInput struct {
	LogicalModel  string
	ProviderModel string
	Enabled       bool
	Priority      int
	Weight        *int
}

type RouteInput struct {
	TenantID     string
	Purpose      string
	LogicalModel string
	Enabled      bool
}

type TestChannelInput struct {
	ProviderModel string
	Prompt        string
}

type ChannelTestResult struct {
	Channel       llmrepo.Channel
	ProviderModel string
	Text          string
	InputTokens   int32
	OutputTokens  int32
	LatencyMS     int64
}

type ProviderModel = llmclient.ModelInfo

type (
	backendFactory func(protocol, baseURL, apiKey string) (llmclient.LLMClient, error)
	modelLister    func(ctx context.Context, protocol, baseURL, apiKey string) ([]ProviderModel, error)
)

func NewService(repo Repo, store Store) *Service {
	return newService(repo, store, newBackend)
}

func newService(repo Repo, store Store, factory backendFactory) *Service {
	return ptrext.Of(Service{
		repo:           repo,
		store:          store,
		backendFactory: factory,
		modelLister:    llmclient.ListProviderModels,
	})
}

func (s *Service) SyncKeyRegistry(ctx context.Context) error {
	keys := s.store.Keys()
	rows := make([]llmrepo.KeyRegistryRow, 0, len(keys))
	keyIDs := make([]string, 0, len(keys))
	available := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keyIDs = append(keyIDs, key.KeyID)
		available[key.KeyID] = struct{}{}
		rows = append(rows, llmrepo.KeyRegistryRow{
			KeyID:              key.KeyID,
			Primary:            key.Primary,
			Status:             key.Status,
			TypeURL:            key.TypeURL,
			OutputPrefixType:   key.OutputPrefixType,
			KeyMaterialType:    key.KeyMaterialType,
			FingerprintSHA256:  key.FingerprintSHA256,
			FingerprintVersion: key.FingerprintVersion,
		})
	}
	if err := s.repo.SyncKeyRegistry(ctx, rows); err != nil {
		return err
	}
	if err := s.repo.ValidateSecretKeyReferences(ctx, keyIDs); err != nil {
		return err
	}
	return s.validateNestedInboundSecretReferences(ctx, available)
}

func (s *Service) validateNestedInboundSecretReferences(
	ctx context.Context,
	available map[string]struct{},
) error {
	refs, err := s.repo.ListInboundSecretConfigs(ctx)
	if err != nil {
		return err
	}
	validation := ptrext.Of(secretReferenceValidation{})
	for _, ref := range refs {
		canDecrypt, err := s.checkInboundCiphertext(
			ref.Config,
			available,
			"inbound_sources.config",
			ref.ID.String(),
			validation,
		)
		if err != nil {
			return err
		}
		if !canDecrypt {
			continue
		}
		plaintext, err := s.store.DecryptValue(secretstore.EncryptedValue{
			Ciphertext: ref.Config,
		}, nil)
		if err != nil {
			return fmt.Errorf("decrypt inbound config row=%s: %w", ref.ID, err)
		}
		nested, err := nestedInboundCiphertexts(ref, plaintext)
		if err != nil {
			return err
		}
		for _, item := range nested {
			if _, err := s.checkInboundCiphertext(
				item.ciphertext,
				available,
				item.location,
				item.rowID,
				validation,
			); err != nil {
				return err
			}
		}
	}
	if len(validation.undetectable) > 0 {
		return fmt.Errorf("%w: %s", llmrepo.ErrSecretKeyUndetectable, strings.Join(validation.undetectable, "; "))
	}
	if len(validation.missing) > 0 {
		return fmt.Errorf("%w: %s", llmrepo.ErrSecretKeyUnavailable, strings.Join(validation.missing, "; "))
	}
	return nil
}

type secretReferenceValidation struct {
	missing      []string
	undetectable []string
}

func (s *Service) checkInboundCiphertext(
	ciphertext []byte,
	available map[string]struct{},
	location string,
	rowID string,
	validation *secretReferenceValidation,
) (bool, error) {
	keyID, keyErr := secretstore.KeyIDFromCiphertext(ciphertext)
	if keyErr == nil {
		if _, ok := available[keyID]; ok {
			if _, decryptErr := s.store.DecryptValue(secretstore.EncryptedValue{
				KeyID:      keyID,
				Ciphertext: ciphertext,
			}, nil); decryptErr != nil {
				return false, fmt.Errorf("decrypt inbound secret %s row=%s key_id=%s: %w",
					location, rowID, keyID, decryptErr)
			}
			return true, nil
		}
		if secretstore.IsLegacyInboundEnvelope(ciphertext) {
			if _, decryptErr := s.store.DecryptValue(secretstore.EncryptedValue{
				Ciphertext: ciphertext,
			}, nil); decryptErr == nil {
				return true, nil
			}
		}
		validation.missing = append(validation.missing, location+" row="+rowID+" key_id="+keyID)
		return false, nil
	}
	if secretstore.IsLegacyInboundEnvelope(ciphertext) {
		if _, decryptErr := s.store.DecryptValue(secretstore.EncryptedValue{
			Ciphertext: ciphertext,
		}, nil); decryptErr != nil {
			return false, fmt.Errorf("decrypt legacy inbound secret %s row=%s: %w",
				location, rowID, decryptErr)
		}
		return true, nil
	}
	validation.undetectable = append(validation.undetectable, location+" row="+rowID)
	return false, nil
}

type nestedCiphertextRef struct {
	location   string
	rowID      string
	ciphertext []byte
}

type webhookSecretEnvelope struct {
	SecretCurrentEncrypted  []byte `json:"secret_current_encrypted"`
	SecretPreviousEncrypted []byte `json:"secret_previous_encrypted,omitempty"`
}

type emailSecretEnvelope struct {
	PasswordEncrypted []byte `json:"password_encrypted"`
}

func nestedInboundCiphertexts(
	ref llmrepo.InboundConfigRef,
	plaintext []byte,
) ([]nestedCiphertextRef, error) {
	switch ref.Channel {
	case "webhook":
		return webhookCiphertexts(ref.ID, plaintext)
	case "email":
		return emailCiphertexts(ref.ID, plaintext)
	default:
		return nil, nil
	}
}

func webhookCiphertexts(rowID uuid.UUID, plaintext []byte) ([]nestedCiphertextRef, error) {
	var cfg webhookSecretEnvelope
	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		return nil, fmt.Errorf("decode webhook config row=%s: %w", rowID, err)
	}
	refs := []nestedCiphertextRef{{
		location:   "inbound_sources.config.webhook.secret_current_encrypted",
		rowID:      rowID.String(),
		ciphertext: cfg.SecretCurrentEncrypted,
	}}
	if len(cfg.SecretPreviousEncrypted) > 0 {
		refs = append(refs, nestedCiphertextRef{
			location:   "inbound_sources.config.webhook.secret_previous_encrypted",
			rowID:      rowID.String(),
			ciphertext: cfg.SecretPreviousEncrypted,
		})
	}
	return refs, nil
}

func emailCiphertexts(rowID uuid.UUID, plaintext []byte) ([]nestedCiphertextRef, error) {
	var cfg emailSecretEnvelope
	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		return nil, fmt.Errorf("decode email config row=%s: %w", rowID, err)
	}
	return []nestedCiphertextRef{{
		location:   "inbound_sources.config.email.password_encrypted",
		rowID:      rowID.String(),
		ciphertext: cfg.PasswordEncrypted,
	}}, nil
}

func (s *Service) ListChannels(ctx context.Context) ([]llmrepo.Channel, error) {
	return s.repo.ListChannels(ctx)
}

func (s *Service) GetChannel(ctx context.Context, id uuid.UUID) (*llmrepo.Channel, error) {
	return s.repo.GetChannel(ctx, id)
}

func (s *Service) CreateChannel(ctx context.Context, in ChannelInput) (*llmrepo.Channel, error) {
	ch, err := normalizeChannelInput(in, nil)
	if err != nil {
		return nil, err
	}
	ch.ID = uuid.New()
	ch, _, err = s.applyCredential(ch, in.APIKey)
	if err != nil {
		return nil, err
	}
	return s.repo.CreateChannel(ctx, ch)
}

func (s *Service) UpdateChannel(ctx context.Context, id uuid.UUID, in ChannelInput) (*llmrepo.Channel, error) {
	existing, err := s.repo.GetChannel(ctx, id)
	if err != nil {
		return nil, err
	}
	ch, err := normalizeChannelInput(in, existing)
	if err != nil {
		return nil, err
	}
	ch.ID = id
	ch.CredentialKeyID = existing.CredentialKeyID
	ch.CredentialCiphertext = existing.CredentialCiphertext
	ch, updateCredential, err := s.applyCredential(ch, in.APIKey)
	if err != nil {
		return nil, err
	}
	return s.repo.UpdateChannel(ctx, ch, updateCredential)
}

func (s *Service) DeleteChannel(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteChannel(ctx, id)
}

func (s *Service) TestChannel(
	ctx context.Context,
	id uuid.UUID,
	in TestChannelInput,
) (*ChannelTestResult, error) {
	ch, err := s.repo.GetChannel(ctx, id)
	if err != nil {
		return nil, err
	}
	model, err := s.resolveTestProviderModel(ctx, id, in.ProviderModel)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		prompt = "Reply with exactly: attune-ok"
	}
	result, err := s.executeChannelTest(ctx, ptrext.Indirect(ch), model, prompt)
	if result == nil {
		result = ptrext.Of(ChannelTestResult{Channel: ptrext.Indirect(ch), ProviderModel: model})
	}
	status := "ok"
	lastError := ""
	if err != nil {
		status = "failed"
		lastError = publicProviderError(err)
	}
	updated, updateErr := s.repo.UpdateChannelTestResult(ctx, id, status, lastError)
	if updateErr != nil {
		return nil, updateErr
	}
	result.Channel = ptrext.Indirect(updated)
	if err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) ListChannelModels(ctx context.Context, id uuid.UUID) ([]ProviderModel, error) {
	ch, err := s.repo.GetChannel(ctx, id)
	if err != nil {
		return nil, err
	}
	apiKey, err := s.apiKeyForChannel(ptrext.Indirect(ch))
	if err != nil {
		return nil, err
	}
	modelCtx := ctx
	cancel := func() {}
	if ch.TimeoutSeconds > 0 {
		modelCtx, cancel = context.WithTimeout(ctx, time.Duration(ch.TimeoutSeconds)*time.Second)
	}
	defer cancel()
	lister := s.modelLister
	if lister == nil {
		lister = llmclient.ListProviderModels
	}
	return lister(modelCtx, ch.Protocol, ch.BaseURL, apiKey)
}

func (s *Service) UpsertAbility(
	ctx context.Context,
	channelID uuid.UUID,
	in AbilityInput,
) (*llmrepo.Ability, error) {
	a, err := normalizeAbilityInput(channelID, in)
	if err != nil {
		return nil, err
	}
	return s.repo.UpsertAbility(ctx, a)
}

func (s *Service) ListAbilities(ctx context.Context, channelID uuid.UUID) ([]llmrepo.Ability, error) {
	if _, err := s.repo.GetChannel(ctx, channelID); err != nil {
		return nil, err
	}
	return s.repo.ListAbilities(ctx, channelID)
}

func (s *Service) DeleteAbility(ctx context.Context, channelID uuid.UUID, logicalModel string) error {
	trimmed := strings.TrimSpace(logicalModel)
	if trimmed == "" {
		return validation("logical_model is required")
	}
	return s.repo.DeleteAbility(ctx, channelID, trimmed)
}

func (s *Service) ListRoutes(ctx context.Context) ([]llmrepo.Route, error) {
	return s.repo.ListRoutes(ctx)
}

func (s *Service) UpsertRoute(ctx context.Context, in RouteInput) (*llmrepo.Route, error) {
	route, err := normalizeRouteInput(in)
	if err != nil {
		return nil, err
	}
	if route.TenantID != "" {
		ok, err := s.repo.TenantExists(ctx, route.TenantID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, validation("tenant_id does not reference an active tenant")
		}
	}
	if route.Enabled {
		ok, err := s.repo.HasEligibleAbility(ctx, route.LogicalModel)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, validation("logical_model has no eligible enabled ability")
		}
	}
	return s.repo.UpsertRoute(ctx, route)
}

func (s *Service) DeleteRoute(ctx context.Context, tenantID, purpose string) error {
	trimmedPurpose := strings.TrimSpace(purpose)
	if trimmedPurpose == "" {
		return validation("purpose is required")
	}
	return s.repo.DeleteRoute(ctx, strings.TrimSpace(tenantID), trimmedPurpose)
}

func (s *Service) applyCredential(
	ch llmrepo.Channel,
	apiKey *string,
) (llmrepo.Channel, bool, error) {
	if ch.AuthMode == llmrepo.AuthModeNone {
		ch.CredentialKeyID = ""
		ch.CredentialCiphertext = nil
		return ch, true, nil
	}
	if apiKey == nil {
		if len(ch.CredentialCiphertext) == 0 {
			return llmrepo.Channel{}, false, ErrNoAPIKey
		}
		return ch, false, nil
	}
	trimmed := strings.TrimSpace(ptrext.Indirect(apiKey))
	if trimmed == "" {
		return llmrepo.Channel{}, false, ErrNoAPIKey
	}
	value, err := s.store.EncryptValue([]byte(trimmed), channelCredentialAAD(ch.ID))
	if err != nil {
		return llmrepo.Channel{}, false, err
	}
	ch.CredentialKeyID = value.KeyID
	ch.CredentialCiphertext = value.Ciphertext
	return ch, true, nil
}

func channelCredentialAAD(id uuid.UUID) []byte {
	return secretstore.AssociatedData("llm_channel", id.String(), "api_key")
}

func (s *Service) resolveTestProviderModel(ctx context.Context, id uuid.UUID, providerModel string) (string, error) {
	trimmed := strings.TrimSpace(providerModel)
	if trimmed != "" {
		return trimmed, nil
	}
	abilities, err := s.repo.ListAbilities(ctx, id)
	if err != nil {
		return "", err
	}
	var selected *llmrepo.Ability
	for _, ability := range abilities {
		if !ability.Enabled {
			continue
		}
		if selected == nil || ability.Priority > selected.Priority {
			selected = ptrext.Of(ability)
		}
	}
	if selected == nil {
		return "", validation("provider_model is required when the channel has no enabled abilities")
	}
	return selected.ProviderModel, nil
}

func (s *Service) executeChannelTest(
	ctx context.Context,
	ch llmrepo.Channel,
	model string,
	prompt string,
) (*ChannelTestResult, error) {
	apiKey, err := s.apiKeyForChannel(ch)
	if err != nil {
		return nil, err
	}
	factory := s.backendFactory
	if factory == nil {
		factory = newBackend
	}
	backend, err := factory(ch.Protocol, ch.BaseURL, apiKey)
	if err != nil {
		return nil, err
	}
	defer backend.Close()
	testCtx := ctx
	cancel := func() {}
	if ch.TimeoutSeconds > 0 {
		testCtx, cancel = context.WithTimeout(ctx, time.Duration(ch.TimeoutSeconds)*time.Second)
	}
	defer cancel()
	started := time.Now()
	resp, err := backend.Complete(testCtx, llmclient.CompletionRequest{
		Model:       model,
		Prompt:      prompt,
		Temperature: 0,
		MaxTokens:   32,
		UserID:      "attune-llm-channel-test",
	})
	result := ptrext.Of(ChannelTestResult{
		Channel:       ch,
		ProviderModel: model,
		LatencyMS:     time.Since(started).Milliseconds(),
	})
	if err != nil {
		return result, err
	}
	result.Text = resp.Text
	result.InputTokens = resp.Usage.InputTokens
	result.OutputTokens = resp.Usage.OutputTokens
	return result, nil
}

func (s *Service) apiKeyForChannel(ch llmrepo.Channel) (string, error) {
	if ch.AuthMode == llmrepo.AuthModeNone {
		return "", nil
	}
	plaintext, err := s.store.DecryptValue(secretstore.EncryptedValue{
		KeyID:      ch.CredentialKeyID,
		Ciphertext: ch.CredentialCiphertext,
	}, channelCredentialAAD(ch.ID))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func newBackend(protocol, baseURL, apiKey string) (llmclient.LLMClient, error) {
	switch protocol {
	case llmrepo.ProtocolOpenAIResponses:
		return llmclient.NewOpenAIResponses(baseURL, apiKey)
	case llmrepo.ProtocolAnthropic:
		return llmclient.NewAnthropic(baseURL, apiKey)
	case llmrepo.ProtocolGemini:
		return llmclient.NewGemini(baseURL, apiKey)
	case llmrepo.ProtocolOpenAICompat:
		return llmclient.NewOpenAICompat(baseURL, apiKey)
	default:
		return nil, fmt.Errorf("llmconfig: unsupported protocol %q", protocol)
	}
}

func publicProviderError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "provider request timed out"
	case errors.Is(err, context.Canceled):
		return "provider request was canceled"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "status 401") || strings.Contains(msg, "status=401") ||
		strings.Contains(msg, "status code: 401") {
		return "provider returned status 401"
	}
	if strings.Contains(msg, "status 429") || strings.Contains(msg, "status=429") ||
		strings.Contains(msg, "status code: 429") {
		return "provider returned status 429"
	}
	if strings.Contains(msg, "status 5") || strings.Contains(msg, "status=5") ||
		strings.Contains(msg, "status code: 5") {
		return "provider returned a 5xx status"
	}
	if strings.Contains(msg, "status") {
		return "provider returned an error status"
	}
	return "provider request failed"
}

func normalizeChannelInput(in ChannelInput, existing *llmrepo.Channel) (llmrepo.Channel, error) {
	ch := llmrepo.Channel{}
	if existing != nil {
		ch = ptrext.Indirect(existing)
	}
	ch.Name = strings.TrimSpace(in.Name)
	ch.Protocol = strings.TrimSpace(in.Protocol)
	ch.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	ch.AuthMode = strings.TrimSpace(in.AuthMode)
	ch.Status = strings.TrimSpace(in.Status)
	ch.Priority = in.Priority
	if in.Weight != nil {
		ch.Weight = ptrext.Indirect(in.Weight)
	} else if existing == nil {
		ch.Weight = 1
	}
	if in.TimeoutSeconds != nil {
		ch.TimeoutSeconds = ptrext.Indirect(in.TimeoutSeconds)
	} else if existing == nil {
		ch.TimeoutSeconds = 60
	}
	if ch.AuthMode == "" {
		ch.AuthMode = llmrepo.AuthModeBearer
	}
	if ch.Status == "" {
		ch.Status = llmrepo.ChannelStatusEnabled
	}
	return ch, validateChannel(ch)
}

func validateChannel(ch llmrepo.Channel) error {
	if ch.Name == "" {
		return validation("name is required")
	}
	if !validProtocol(ch.Protocol) {
		return validation("protocol is not supported")
	}
	if ch.Protocol == llmrepo.ProtocolOpenAICompat && ch.BaseURL == "" {
		return validation("base_url is required for openai-compat")
	}
	if err := validateBaseURL(ch.BaseURL); err != nil {
		return err
	}
	if ch.AuthMode != llmrepo.AuthModeBearer && ch.AuthMode != llmrepo.AuthModeNone {
		return validation("auth_mode must be bearer or none")
	}
	if ch.AuthMode == llmrepo.AuthModeNone && ch.Protocol != llmrepo.ProtocolOpenAICompat {
		return validation("auth_mode=none is only supported for openai-compat")
	}
	if !validStatus(ch.Status) {
		return validation("status must be enabled, disabled, or draining")
	}
	if ch.Weight < 0 {
		return validation("weight must be >= 0")
	}
	if ch.TimeoutSeconds < 1 || ch.TimeoutSeconds > 300 {
		return validation("timeout_seconds must be between 1 and 300")
	}
	return nil
}

func validateBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return validation("base_url must be an absolute URL")
	}
	if parsed.User != nil {
		return validation("base_url must not include userinfo")
	}
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(parsed.Hostname()) {
			return nil
		}
		return validation("base_url must use https; http is allowed only for localhost or loopback")
	default:
		return validation("base_url must use http or https")
	}
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func normalizeAbilityInput(channelID uuid.UUID, in AbilityInput) (llmrepo.Ability, error) {
	a := llmrepo.Ability{
		ChannelID:     channelID,
		LogicalModel:  strings.TrimSpace(in.LogicalModel),
		ProviderModel: strings.TrimSpace(in.ProviderModel),
		Enabled:       in.Enabled,
		Priority:      in.Priority,
		Weight:        1,
	}
	if in.Weight != nil {
		a.Weight = ptrext.Indirect(in.Weight)
	}
	if a.ProviderModel == "" {
		a.ProviderModel = a.LogicalModel
	}
	if a.LogicalModel == "" {
		return llmrepo.Ability{}, validation("logical_model is required")
	}
	if a.ProviderModel == "" {
		return llmrepo.Ability{}, validation("provider_model is required")
	}
	if a.Weight < 0 {
		return llmrepo.Ability{}, validation("weight must be >= 0")
	}
	return a, nil
}

func normalizeRouteInput(in RouteInput) (llmrepo.Route, error) {
	route := llmrepo.Route{
		TenantID:     strings.TrimSpace(in.TenantID),
		Purpose:      strings.TrimSpace(in.Purpose),
		LogicalModel: strings.TrimSpace(in.LogicalModel),
		Enabled:      in.Enabled,
	}
	if route.Purpose == "" {
		return llmrepo.Route{}, validation("purpose is required")
	}
	if route.LogicalModel == "" {
		return llmrepo.Route{}, validation("logical_model is required")
	}
	return route, nil
}

func validation(message string) error {
	return fmt.Errorf("%w: %s", ErrValidation, message)
}

func validProtocol(protocol string) bool {
	switch protocol {
	case llmrepo.ProtocolOpenAICompat, llmrepo.ProtocolOpenAIResponses,
		llmrepo.ProtocolAnthropic, llmrepo.ProtocolGemini:
		return true
	default:
		return false
	}
}

func validStatus(status string) bool {
	switch status {
	case llmrepo.ChannelStatusEnabled, llmrepo.ChannelStatusDisabled, llmrepo.ChannelStatusDraining:
		return true
	default:
		return false
	}
}
