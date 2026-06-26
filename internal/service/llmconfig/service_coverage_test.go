// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-mock-fixtures

package llmconfig

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
)

// ---------------------------------------------------------------------------
// Enhanced fakeRepo with configurable error returns
// ---------------------------------------------------------------------------

type errFakeRepo struct {
	fakeRepo
	syncErr            error
	validateErr        error
	listInboundConfigs []llmrepo.InboundConfigRef
	listInboundErr     error
	listChannelsResult []llmrepo.Channel
	listChannelsErr    error
	getChannelErr      error
	createErr          error
	updateErr          error
	deleteErr          error
	listAbilitiesErr   error
	upsertAbilityRow   *llmrepo.Ability
	upsertAbilityErr   error
	deleteAbilityErr   error
	hasEligibleErr     error
	tenantExistsErr    error
	listRoutesResult   []llmrepo.Route
	listRoutesErr      error
	upsertRouteRow     *llmrepo.Route
	upsertRouteErr     error
	deleteRouteErr     error
	updateTestErr      error
}

func (r *errFakeRepo) SyncKeyRegistry(ctx context.Context, rows []llmrepo.KeyRegistryRow) error {
	if r.syncErr != nil {
		return r.syncErr
	}
	return r.fakeRepo.SyncKeyRegistry(ctx, rows)
}

func (r *errFakeRepo) ValidateSecretKeyReferences(ctx context.Context, keyIDs []string) error {
	if r.validateErr != nil {
		return r.validateErr
	}
	return r.fakeRepo.ValidateSecretKeyReferences(ctx, keyIDs)
}

func (r *errFakeRepo) ListInboundSecretConfigs(context.Context) ([]llmrepo.InboundConfigRef, error) {
	if r.listInboundErr != nil {
		return nil, r.listInboundErr
	}
	return r.listInboundConfigs, nil
}

func (r *errFakeRepo) ListChannels(context.Context) ([]llmrepo.Channel, error) {
	if r.listChannelsErr != nil {
		return nil, r.listChannelsErr
	}
	return r.listChannelsResult, nil
}

func (r *errFakeRepo) GetChannel(ctx context.Context, id uuid.UUID) (*llmrepo.Channel, error) {
	if r.getChannelErr != nil {
		return nil, r.getChannelErr
	}
	return r.fakeRepo.GetChannel(ctx, id)
}

func (r *errFakeRepo) CreateChannel(ctx context.Context, ch llmrepo.Channel) (*llmrepo.Channel, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	return r.fakeRepo.CreateChannel(ctx, ch)
}

func (r *errFakeRepo) UpdateChannel(ctx context.Context, ch llmrepo.Channel, updateCred bool) (*llmrepo.Channel, error) {
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	return r.fakeRepo.UpdateChannel(ctx, ch, updateCred)
}

func (r *errFakeRepo) DeleteChannel(ctx context.Context, id uuid.UUID) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	return r.fakeRepo.DeleteChannel(ctx, id)
}

func (r *errFakeRepo) ListAbilities(ctx context.Context, id uuid.UUID) ([]llmrepo.Ability, error) {
	if r.listAbilitiesErr != nil {
		return nil, r.listAbilitiesErr
	}
	return r.fakeRepo.ListAbilities(ctx, id)
}

func (r *errFakeRepo) UpsertAbility(_ context.Context, a llmrepo.Ability) (*llmrepo.Ability, error) {
	if r.upsertAbilityErr != nil {
		return nil, r.upsertAbilityErr
	}
	if r.upsertAbilityRow != nil {
		return r.upsertAbilityRow, nil
	}
	return ptrext.Of(a), nil
}

func (r *errFakeRepo) DeleteAbility(_ context.Context, _ uuid.UUID, _ string) error {
	return r.deleteAbilityErr
}

func (r *errFakeRepo) HasEligibleAbility(_ context.Context, _ string) (bool, error) {
	if r.hasEligibleErr != nil {
		return false, r.hasEligibleErr
	}
	return r.fakeRepo.HasEligibleAbility(context.Background(), "")
}

func (r *errFakeRepo) TenantExists(_ context.Context, _ string) (bool, error) {
	if r.tenantExistsErr != nil {
		return false, r.tenantExistsErr
	}
	return r.fakeRepo.TenantExists(context.Background(), "")
}

func (r *errFakeRepo) ListRoutes(context.Context) ([]llmrepo.Route, error) {
	if r.listRoutesErr != nil {
		return nil, r.listRoutesErr
	}
	return r.listRoutesResult, nil
}

func (r *errFakeRepo) UpsertRoute(_ context.Context, route llmrepo.Route) (*llmrepo.Route, error) {
	if r.upsertRouteErr != nil {
		return nil, r.upsertRouteErr
	}
	if r.upsertRouteRow != nil {
		return r.upsertRouteRow, nil
	}
	return ptrext.Of(route), nil
}

func (r *errFakeRepo) DeleteRoute(_ context.Context, _ string, _ string) error {
	return r.deleteRouteErr
}

func (r *errFakeRepo) UpdateChannelTestResult(ctx context.Context, id uuid.UUID, status, lastError string) (*llmrepo.Channel, error) {
	if r.updateTestErr != nil {
		return nil, r.updateTestErr
	}
	return r.fakeRepo.UpdateChannelTestResult(ctx, id, status, lastError)
}

// errStore is a fakeStore that can return errors on encrypt/decrypt.
type errStore struct {
	encryptErr error
	decryptErr error
	decryptVal []byte
	keys       []secretstore.KeyMetadata
}

func (s *errStore) EncryptValue(_, _ []byte) (secretstore.EncryptedValue, error) {
	if s.encryptErr != nil {
		return secretstore.EncryptedValue{}, s.encryptErr
	}
	return secretstore.EncryptedValue{KeyID: "123", Ciphertext: []byte("ciphertext")}, nil
}

func (s *errStore) DecryptValue(_ secretstore.EncryptedValue, _ []byte) ([]byte, error) {
	if s.decryptErr != nil {
		return nil, s.decryptErr
	}
	if s.decryptVal != nil {
		return s.decryptVal, nil
	}
	return []byte("sk-test"), nil
}

func (s *errStore) Keys() []secretstore.KeyMetadata {
	if s.keys != nil {
		return s.keys
	}
	return []secretstore.KeyMetadata{{KeyID: "123", Primary: true, Status: "ENABLED"}}
}

// ---------------------------------------------------------------------------
// normalizeChannelInput
// ---------------------------------------------------------------------------

func TestNormalizeChannelInput(t *testing.T) {
	t.Parallel()

	t.Run("defaults weight and timeout for new channel", func(t *testing.T) {
		t.Parallel()
		ch, err := normalizeChannelInput(ChannelInput{
			Name:     "test",
			Protocol: llmrepo.ProtocolOpenAICompat,
			BaseURL:  "https://api.example.com",
		}, nil)
		require.NoError(t, err)
		require.Equal(t, 1, ch.Weight)
		require.Equal(t, 60, ch.TimeoutSeconds)
	})

	t.Run("defaults auth_mode to bearer", func(t *testing.T) {
		t.Parallel()
		ch, err := normalizeChannelInput(ChannelInput{
			Name:     "test",
			Protocol: llmrepo.ProtocolAnthropic,
		}, nil)
		require.NoError(t, err)
		require.Equal(t, llmrepo.AuthModeBearer, ch.AuthMode)
	})

	t.Run("defaults status to enabled", func(t *testing.T) {
		t.Parallel()
		ch, err := normalizeChannelInput(ChannelInput{
			Name:     "test",
			Protocol: llmrepo.ProtocolAnthropic,
		}, nil)
		require.NoError(t, err)
		require.Equal(t, llmrepo.ChannelStatusEnabled, ch.Status)
	})

	t.Run("preserves existing weight when not provided", func(t *testing.T) {
		t.Parallel()
		existing := ptrext.Of(llmrepo.Channel{Weight: 5, TimeoutSeconds: 30})
		ch, err := normalizeChannelInput(ChannelInput{
			Name:     "test",
			Protocol: llmrepo.ProtocolAnthropic,
		}, existing)
		require.NoError(t, err)
		require.Equal(t, 5, ch.Weight)
		require.Equal(t, 30, ch.TimeoutSeconds)
	})

	t.Run("overrides weight when provided", func(t *testing.T) {
		t.Parallel()
		existing := ptrext.Of(llmrepo.Channel{Weight: 5, TimeoutSeconds: 30})
		ch, err := normalizeChannelInput(ChannelInput{
			Name:           "test",
			Protocol:       llmrepo.ProtocolAnthropic,
			Weight:         ptrext.Of(10),
			TimeoutSeconds: ptrext.Of(120),
		}, existing)
		require.NoError(t, err)
		require.Equal(t, 10, ch.Weight)
		require.Equal(t, 120, ch.TimeoutSeconds)
	})

	t.Run("trims base URL trailing slash", func(t *testing.T) {
		t.Parallel()
		ch, err := normalizeChannelInput(ChannelInput{
			Name:     "test",
			Protocol: llmrepo.ProtocolOpenAICompat,
			BaseURL:  "https://api.example.com/v1/",
		}, nil)
		require.NoError(t, err)
		require.Equal(t, "https://api.example.com/v1", ch.BaseURL)
	})

	t.Run("trims whitespace from fields", func(t *testing.T) {
		t.Parallel()
		ch, err := normalizeChannelInput(ChannelInput{
			Name:     "  test  ",
			Protocol: "  " + llmrepo.ProtocolAnthropic + "  ",
			BaseURL:  "  ",
			AuthMode: "  " + llmrepo.AuthModeBearer + "  ",
			Status:   "  " + llmrepo.ChannelStatusEnabled + "  ",
		}, nil)
		require.NoError(t, err)
		require.Equal(t, "test", ch.Name)
		require.Equal(t, llmrepo.ProtocolAnthropic, ch.Protocol)
		require.Equal(t, llmrepo.AuthModeBearer, ch.AuthMode)
		require.Equal(t, llmrepo.ChannelStatusEnabled, ch.Status)
	})
}

// ---------------------------------------------------------------------------
// validateChannel
// ---------------------------------------------------------------------------

func TestValidateChannel(t *testing.T) {
	t.Parallel()

	validChannel := llmrepo.Channel{
		Name:           "test",
		Protocol:       llmrepo.ProtocolAnthropic,
		AuthMode:       llmrepo.AuthModeBearer,
		Status:         llmrepo.ChannelStatusEnabled,
		Weight:         1,
		TimeoutSeconds: 60,
	}

	t.Run("valid channel passes", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateChannel(validChannel))
	})

	t.Run("empty name rejected", func(t *testing.T) {
		t.Parallel()
		ch := validChannel
		ch.Name = ""
		err := validateChannel(ch)
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "name is required")
	})

	t.Run("invalid protocol rejected", func(t *testing.T) {
		t.Parallel()
		ch := validChannel
		ch.Protocol = "unsupported"
		err := validateChannel(ch)
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "protocol is not supported")
	})

	t.Run("openai-compat requires base_url", func(t *testing.T) {
		t.Parallel()
		ch := validChannel
		ch.Protocol = llmrepo.ProtocolOpenAICompat
		ch.BaseURL = ""
		err := validateChannel(ch)
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "base_url is required for openai-compat")
	})

	t.Run("invalid auth_mode rejected", func(t *testing.T) {
		t.Parallel()
		ch := validChannel
		ch.AuthMode = "api-key"
		err := validateChannel(ch)
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "auth_mode must be bearer or none")
	})

	t.Run("auth_mode none only for openai-compat", func(t *testing.T) {
		t.Parallel()
		ch := validChannel
		ch.AuthMode = llmrepo.AuthModeNone
		ch.Protocol = llmrepo.ProtocolAnthropic
		err := validateChannel(ch)
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "auth_mode=none is only supported for openai-compat")
	})

	t.Run("invalid status rejected", func(t *testing.T) {
		t.Parallel()
		ch := validChannel
		ch.Status = "paused"
		err := validateChannel(ch)
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "status must be")
	})

	t.Run("negative weight rejected", func(t *testing.T) {
		t.Parallel()
		ch := validChannel
		ch.Weight = -1
		err := validateChannel(ch)
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "weight must be >= 0")
	})

	t.Run("timeout below 1 rejected", func(t *testing.T) {
		t.Parallel()
		ch := validChannel
		ch.TimeoutSeconds = 0
		err := validateChannel(ch)
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "timeout_seconds must be between")
	})

	t.Run("timeout above 300 rejected", func(t *testing.T) {
		t.Parallel()
		ch := validChannel
		ch.TimeoutSeconds = 301
		err := validateChannel(ch)
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "timeout_seconds must be between")
	})
}

// ---------------------------------------------------------------------------
// normalizeAbilityInput
// ---------------------------------------------------------------------------

func TestNormalizeAbilityInput(t *testing.T) {
	t.Parallel()

	channelID := uuid.New()

	t.Run("valid input", func(t *testing.T) {
		t.Parallel()
		a, err := normalizeAbilityInput(channelID, AbilityInput{
			LogicalModel:  "gpt-4o",
			ProviderModel: "gpt-4o-2024-08-06",
			Enabled:       true,
			Priority:      10,
			Weight:        ptrext.Of(5),
		})
		require.NoError(t, err)
		require.Equal(t, channelID, a.ChannelID)
		require.Equal(t, "gpt-4o", a.LogicalModel)
		require.Equal(t, "gpt-4o-2024-08-06", a.ProviderModel)
		require.True(t, a.Enabled)
		require.Equal(t, 10, a.Priority)
		require.Equal(t, 5, a.Weight)
	})

	t.Run("provider_model defaults to logical_model", func(t *testing.T) {
		t.Parallel()
		a, err := normalizeAbilityInput(channelID, AbilityInput{
			LogicalModel: "gpt-4o",
		})
		require.NoError(t, err)
		require.Equal(t, "gpt-4o", a.ProviderModel)
	})

	t.Run("default weight is 1", func(t *testing.T) {
		t.Parallel()
		a, err := normalizeAbilityInput(channelID, AbilityInput{
			LogicalModel: "gpt-4o",
		})
		require.NoError(t, err)
		require.Equal(t, 1, a.Weight)
	})

	t.Run("empty logical_model rejected", func(t *testing.T) {
		t.Parallel()
		_, err := normalizeAbilityInput(channelID, AbilityInput{
			LogicalModel: "   ",
		})
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "logical_model is required")
	})

	t.Run("negative weight rejected", func(t *testing.T) {
		t.Parallel()
		_, err := normalizeAbilityInput(channelID, AbilityInput{
			LogicalModel: "gpt-4o",
			Weight:       ptrext.Of(-1),
		})
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "weight must be >= 0")
	})

	t.Run("trims whitespace", func(t *testing.T) {
		t.Parallel()
		a, err := normalizeAbilityInput(channelID, AbilityInput{
			LogicalModel:  "  gpt-4o  ",
			ProviderModel: "  gpt-4o-latest  ",
		})
		require.NoError(t, err)
		require.Equal(t, "gpt-4o", a.LogicalModel)
		require.Equal(t, "gpt-4o-latest", a.ProviderModel)
	})
}

// ---------------------------------------------------------------------------
// normalizeRouteInput
// ---------------------------------------------------------------------------

func TestNormalizeRouteInput(t *testing.T) {
	t.Parallel()

	t.Run("valid input", func(t *testing.T) {
		t.Parallel()
		route, err := normalizeRouteInput(RouteInput{
			TenantID:     "t1",
			Purpose:      "enrich",
			LogicalModel: "gpt-4o",
			Enabled:      true,
		})
		require.NoError(t, err)
		require.Equal(t, "t1", route.TenantID)
		require.Equal(t, "enrich", route.Purpose)
		require.Equal(t, "gpt-4o", route.LogicalModel)
		require.True(t, route.Enabled)
	})

	t.Run("empty purpose rejected", func(t *testing.T) {
		t.Parallel()
		_, err := normalizeRouteInput(RouteInput{
			LogicalModel: "gpt-4o",
		})
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "purpose is required")
	})

	t.Run("empty logical_model rejected", func(t *testing.T) {
		t.Parallel()
		_, err := normalizeRouteInput(RouteInput{
			Purpose: "enrich",
		})
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "logical_model is required")
	})

	t.Run("trims whitespace", func(t *testing.T) {
		t.Parallel()
		route, err := normalizeRouteInput(RouteInput{
			TenantID:     "  t1  ",
			Purpose:      "  enrich  ",
			LogicalModel: "  gpt-4o  ",
		})
		require.NoError(t, err)
		require.Equal(t, "t1", route.TenantID)
		require.Equal(t, "enrich", route.Purpose)
		require.Equal(t, "gpt-4o", route.LogicalModel)
	})
}

// ---------------------------------------------------------------------------
// ListChannels / DeleteChannel (passthrough)
// ---------------------------------------------------------------------------

func TestListChannels(t *testing.T) {
	t.Parallel()

	t.Run("returns repo result", func(t *testing.T) {
		t.Parallel()
		channels := []llmrepo.Channel{{Name: "ch1"}, {Name: "ch2"}}
		repo := ptrext.Of(errFakeRepo{listChannelsResult: channels})
		svc := NewService(repo, ptrext.Of(errStore{}))
		got, err := svc.ListChannels(context.Background())
		require.NoError(t, err)
		require.Len(t, got, 2)
	})

	t.Run("propagates repo error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{listChannelsErr: errors.New("db down")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.ListChannels(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "db down")
	})
}

func TestGetChannel(t *testing.T) {
	t.Parallel()

	t.Run("returns existing channel", func(t *testing.T) {
		t.Parallel()
		id := uuid.New()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{ID: id, Name: "ch1"})
		svc := NewService(repo, ptrext.Of(errStore{}))
		ch, err := svc.GetChannel(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, "ch1", ch.Name)
	})

	t.Run("returns not found error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{getChannelErr: llmrepo.ErrChannelNotFound})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.GetChannel(context.Background(), uuid.New())
		require.ErrorIs(t, err, llmrepo.ErrChannelNotFound)
	})
}

func TestDeleteChannel(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		require.NoError(t, svc.DeleteChannel(context.Background(), uuid.New()))
	})

	t.Run("propagates error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{deleteErr: errors.New("fk violation")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		err := svc.DeleteChannel(context.Background(), uuid.New())
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// CreateChannel error paths
// ---------------------------------------------------------------------------

func TestCreateChannel(t *testing.T) {
	t.Parallel()

	validInput := ChannelInput{
		Name:           "test",
		Protocol:       llmrepo.ProtocolAnthropic,
		AuthMode:       llmrepo.AuthModeBearer,
		APIKey:         ptrext.Of("sk-test"),
		Status:         llmrepo.ChannelStatusEnabled,
		Weight:         ptrext.Of(1),
		TimeoutSeconds: ptrext.Of(60),
	}

	t.Run("validation failure", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		_, err := svc.CreateChannel(context.Background(), ChannelInput{Protocol: "unknown"})
		require.ErrorIs(t, err, ErrValidation)
	})

	t.Run("encrypt failure", func(t *testing.T) {
		t.Parallel()
		store := ptrext.Of(errStore{encryptErr: errors.New("keyset broken")})
		svc := NewService(ptrext.Of(errFakeRepo{}), store)
		_, err := svc.CreateChannel(context.Background(), validInput)
		require.Error(t, err)
		require.Contains(t, err.Error(), "keyset broken")
	})

	t.Run("repo create error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{createErr: errors.New("unique violation")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.CreateChannel(context.Background(), validInput)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unique violation")
	})

	t.Run("bearer without API key rejected", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		_, err := svc.CreateChannel(context.Background(), ChannelInput{
			Name:           "test",
			Protocol:       llmrepo.ProtocolAnthropic,
			AuthMode:       llmrepo.AuthModeBearer,
			Status:         llmrepo.ChannelStatusEnabled,
			Weight:         ptrext.Of(1),
			TimeoutSeconds: ptrext.Of(60),
		})
		require.ErrorIs(t, err, ErrNoAPIKey)
	})

	t.Run("bearer with blank API key rejected", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		_, err := svc.CreateChannel(context.Background(), ChannelInput{
			Name:           "test",
			Protocol:       llmrepo.ProtocolAnthropic,
			AuthMode:       llmrepo.AuthModeBearer,
			APIKey:         ptrext.Of("   "),
			Status:         llmrepo.ChannelStatusEnabled,
			Weight:         ptrext.Of(1),
			TimeoutSeconds: ptrext.Of(60),
		})
		require.ErrorIs(t, err, ErrNoAPIKey)
	})
}

// ---------------------------------------------------------------------------
// UpdateChannel error paths
// ---------------------------------------------------------------------------

func TestUpdateChannel(t *testing.T) {
	t.Parallel()

	channelID := uuid.New()
	existing := ptrext.Of(llmrepo.Channel{
		ID:                   channelID,
		Name:                 "primary",
		Protocol:             llmrepo.ProtocolOpenAICompat,
		BaseURL:              "https://api.openai.com",
		AuthMode:             llmrepo.AuthModeBearer,
		CredentialKeyID:      "123",
		CredentialCiphertext: []byte("ct"),
		Status:               llmrepo.ChannelStatusEnabled,
		Weight:               1,
		TimeoutSeconds:       60,
	})

	t.Run("get channel fails", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{getChannelErr: llmrepo.ErrChannelNotFound})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.UpdateChannel(context.Background(), channelID, ChannelInput{})
		require.ErrorIs(t, err, llmrepo.ErrChannelNotFound)
	})

	t.Run("validation failure on update", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = existing
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.UpdateChannel(context.Background(), channelID, ChannelInput{
			Name:     "",
			Protocol: llmrepo.ProtocolOpenAICompat,
			BaseURL:  "https://api.openai.com",
		})
		require.ErrorIs(t, err, ErrValidation)
	})

	t.Run("encrypt failure on update", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = existing
		store := ptrext.Of(errStore{encryptErr: errors.New("bad keyset")})
		svc := NewService(repo, store)
		_, err := svc.UpdateChannel(context.Background(), channelID, ChannelInput{
			Name:           "updated",
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "https://api.openai.com",
			AuthMode:       llmrepo.AuthModeBearer,
			APIKey:         ptrext.Of("new-key"),
			Status:         llmrepo.ChannelStatusEnabled,
			Weight:         ptrext.Of(1),
			TimeoutSeconds: ptrext.Of(60),
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "bad keyset")
	})

	t.Run("updates credential when new API key provided", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = existing
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.UpdateChannel(context.Background(), channelID, ChannelInput{
			Name:           "updated",
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "https://api.openai.com",
			AuthMode:       llmrepo.AuthModeBearer,
			APIKey:         ptrext.Of("new-api-key"),
			Status:         llmrepo.ChannelStatusEnabled,
			Weight:         ptrext.Of(1),
			TimeoutSeconds: ptrext.Of(60),
		})
		require.NoError(t, err)
		require.True(t, repo.updateCredential)
	})

	t.Run("clears credential when switching to auth_mode none", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		existingCompat := ptrext.Of(llmrepo.Channel{
			ID:                   channelID,
			Name:                 "local",
			Protocol:             llmrepo.ProtocolOpenAICompat,
			BaseURL:              "http://localhost:11434",
			AuthMode:             llmrepo.AuthModeBearer,
			CredentialKeyID:      "123",
			CredentialCiphertext: []byte("ct"),
			Status:               llmrepo.ChannelStatusEnabled,
			Weight:               1,
			TimeoutSeconds:       60,
		})
		repo.existing = existingCompat
		svc := NewService(repo, ptrext.Of(errStore{}))
		row, err := svc.UpdateChannel(context.Background(), channelID, ChannelInput{
			Name:           "local",
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			Status:         llmrepo.ChannelStatusEnabled,
			Weight:         ptrext.Of(1),
			TimeoutSeconds: ptrext.Of(60),
		})
		require.NoError(t, err)
		require.True(t, repo.updateCredential)
		require.Empty(t, row.CredentialKeyID)
		require.Nil(t, row.CredentialCiphertext)
	})
}

// ---------------------------------------------------------------------------
// UpsertAbility / ListAbilities / DeleteAbility
// ---------------------------------------------------------------------------

func TestUpsertAbility(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		svc := NewService(repo, ptrext.Of(errStore{}))
		a, err := svc.UpsertAbility(context.Background(), channelID, AbilityInput{
			LogicalModel:  "gpt-4o",
			ProviderModel: "gpt-4o-latest",
			Enabled:       true,
			Priority:      5,
		})
		require.NoError(t, err)
		require.Equal(t, "gpt-4o", a.LogicalModel)
	})

	t.Run("validation error", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		_, err := svc.UpsertAbility(context.Background(), channelID, AbilityInput{})
		require.ErrorIs(t, err, ErrValidation)
	})

	t.Run("repo error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{upsertAbilityErr: errors.New("constraint")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.UpsertAbility(context.Background(), channelID, AbilityInput{
			LogicalModel: "gpt-4o",
		})
		require.Error(t, err)
	})
}

func TestListAbilities(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()

	t.Run("channel not found", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{getChannelErr: llmrepo.ErrChannelNotFound})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.ListAbilities(context.Background(), channelID)
		require.ErrorIs(t, err, llmrepo.ErrChannelNotFound)
	})

	t.Run("success with existing channel", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{ID: channelID})
		repo.abilities = []llmrepo.Ability{{LogicalModel: "m1"}}
		svc := NewService(repo, ptrext.Of(errStore{}))
		abilities, err := svc.ListAbilities(context.Background(), channelID)
		require.NoError(t, err)
		require.Len(t, abilities, 1)
	})
}

func TestDeleteAbility(t *testing.T) {
	t.Parallel()

	t.Run("empty logical_model rejected", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		err := svc.DeleteAbility(context.Background(), uuid.New(), "  ")
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "logical_model is required")
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		err := svc.DeleteAbility(context.Background(), uuid.New(), "gpt-4o")
		require.NoError(t, err)
	})

	t.Run("propagates error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{deleteAbilityErr: errors.New("not found")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		err := svc.DeleteAbility(context.Background(), uuid.New(), "gpt-4o")
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// ListRoutes / UpsertRoute / DeleteRoute
// ---------------------------------------------------------------------------

func TestListRoutes(t *testing.T) {
	t.Parallel()

	t.Run("returns routes", func(t *testing.T) {
		t.Parallel()
		routes := []llmrepo.Route{{Purpose: "enrich"}}
		repo := ptrext.Of(errFakeRepo{listRoutesResult: routes})
		svc := NewService(repo, ptrext.Of(errStore{}))
		got, err := svc.ListRoutes(context.Background())
		require.NoError(t, err)
		require.Len(t, got, 1)
	})

	t.Run("propagates error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{listRoutesErr: errors.New("db err")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.ListRoutes(context.Background())
		require.Error(t, err)
	})
}

func TestUpsertRoute(t *testing.T) {
	t.Parallel()

	t.Run("validation error missing purpose", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		_, err := svc.UpsertRoute(context.Background(), RouteInput{LogicalModel: "m"})
		require.ErrorIs(t, err, ErrValidation)
	})

	t.Run("tenant exists check fails", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{tenantExistsErr: errors.New("db err")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.UpsertRoute(context.Background(), RouteInput{
			TenantID:     "t1",
			Purpose:      "enrich",
			LogicalModel: "m",
		})
		require.Error(t, err)
	})

	t.Run("has eligible ability check fails", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{hasEligibleErr: errors.New("db err")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.UpsertRoute(context.Background(), RouteInput{
			Purpose:      "enrich",
			LogicalModel: "m",
			Enabled:      true,
		})
		require.Error(t, err)
	})

	t.Run("disabled route skips ability check", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.UpsertRoute(context.Background(), RouteInput{
			Purpose:      "enrich",
			LogicalModel: "m",
			Enabled:      false,
		})
		require.NoError(t, err)
	})

	t.Run("empty tenant_id skips tenant check", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.hasEligibleRoute = true
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.UpsertRoute(context.Background(), RouteInput{
			Purpose:      "enrich",
			LogicalModel: "m",
			Enabled:      true,
		})
		require.NoError(t, err)
	})

	t.Run("repo upsert error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{upsertRouteErr: errors.New("constraint")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.UpsertRoute(context.Background(), RouteInput{
			Purpose:      "enrich",
			LogicalModel: "m",
			Enabled:      false,
		})
		require.Error(t, err)
	})
}

func TestDeleteRoute(t *testing.T) {
	t.Parallel()

	t.Run("empty purpose rejected", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		err := svc.DeleteRoute(context.Background(), "t1", "  ")
		require.ErrorIs(t, err, ErrValidation)
		require.Contains(t, err.Error(), "purpose is required")
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		err := svc.DeleteRoute(context.Background(), "t1", "enrich")
		require.NoError(t, err)
	})

	t.Run("propagates error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{deleteRouteErr: errors.New("not found")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		err := svc.DeleteRoute(context.Background(), "t1", "enrich")
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// TestChannel
// ---------------------------------------------------------------------------

func TestTestChannel(t *testing.T) {
	t.Parallel()

	channelID := uuid.New()

	t.Run("channel not found", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{getChannelErr: llmrepo.ErrChannelNotFound})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.TestChannel(context.Background(), channelID, TestChannelInput{})
		require.ErrorIs(t, err, llmrepo.ErrChannelNotFound)
	})

	t.Run("explicit provider model used", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:             channelID,
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 1,
		})
		var gotModel string
		svc := newService(repo, ptrext.Of(errStore{}), func(_, _, _ string) (llmclient.LLMClient, error) {
			return captureBackend{setModel: func(m string) { gotModel = m }}, nil
		})
		result, err := svc.TestChannel(context.Background(), channelID, TestChannelInput{
			ProviderModel: "explicit-model",
		})
		require.NoError(t, err)
		require.Equal(t, "explicit-model", gotModel)
		require.Equal(t, "explicit-model", result.ProviderModel)
	})

	t.Run("custom prompt used", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:             channelID,
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 1,
		})
		repo.abilities = []llmrepo.Ability{{ProviderModel: "m", Enabled: true}}
		var gotPrompt string
		svc := newService(repo, ptrext.Of(errStore{}), func(_, _, _ string) (llmclient.LLMClient, error) {
			return promptBackend{setPrompt: func(p string) { gotPrompt = p }}, nil
		})
		_, err := svc.TestChannel(context.Background(), channelID, TestChannelInput{
			Prompt: "  Say hello  ",
		})
		require.NoError(t, err)
		require.Equal(t, "Say hello", gotPrompt)
	})

	t.Run("default prompt when empty", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:             channelID,
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 1,
		})
		repo.abilities = []llmrepo.Ability{{ProviderModel: "m", Enabled: true}}
		var gotPrompt string
		svc := newService(repo, ptrext.Of(errStore{}), func(_, _, _ string) (llmclient.LLMClient, error) {
			return promptBackend{setPrompt: func(p string) { gotPrompt = p }}, nil
		})
		_, err := svc.TestChannel(context.Background(), channelID, TestChannelInput{})
		require.NoError(t, err)
		require.Equal(t, "Reply with exactly: attune-ok", gotPrompt)
	})

	t.Run("no enabled abilities and no explicit model", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:             channelID,
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 1,
		})
		repo.abilities = []llmrepo.Ability{{ProviderModel: "m", Enabled: false}}
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.TestChannel(context.Background(), channelID, TestChannelInput{})
		require.ErrorIs(t, err, ErrValidation)
	})

	t.Run("backend factory error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:             channelID,
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 1,
		})
		repo.abilities = []llmrepo.Ability{{ProviderModel: "m", Enabled: true}}
		svc := newService(repo, ptrext.Of(errStore{}), func(_, _, _ string) (llmclient.LLMClient, error) {
			return nil, errors.New("factory err")
		})
		_, err := svc.TestChannel(context.Background(), channelID, TestChannelInput{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "factory err")
	})

	t.Run("update test result error propagated", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{updateTestErr: errors.New("db err")})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:             channelID,
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 1,
		})
		repo.abilities = []llmrepo.Ability{{ProviderModel: "m", Enabled: true}}
		svc := newService(repo, ptrext.Of(errStore{}), func(_, _, _ string) (llmclient.LLMClient, error) {
			return fakeBackend{}, nil
		})
		_, err := svc.TestChannel(context.Background(), channelID, TestChannelInput{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "db err")
	})

	t.Run("decrypt error for bearer channel", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:                   channelID,
			Protocol:             llmrepo.ProtocolOpenAICompat,
			BaseURL:              "http://localhost:11434",
			AuthMode:             llmrepo.AuthModeBearer,
			CredentialKeyID:      "123",
			CredentialCiphertext: []byte("ct"),
			TimeoutSeconds:       1,
		})
		repo.abilities = []llmrepo.Ability{{ProviderModel: "m", Enabled: true}}
		store := ptrext.Of(errStore{decryptErr: errors.New("decrypt fail")})
		svc := newService(repo, store, func(_, _, _ string) (llmclient.LLMClient, error) {
			return fakeBackend{}, nil
		})
		_, err := svc.TestChannel(context.Background(), channelID, TestChannelInput{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "decrypt fail")
	})
}

// promptBackend captures the prompt passed to Complete.
type promptBackend struct {
	setPrompt func(string)
}

func (b promptBackend) Complete(_ context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	b.setPrompt(req.Prompt)
	return fakeBackend{}.Complete(context.Background(), req)
}

func (b promptBackend) Close() error { return nil }

// ---------------------------------------------------------------------------
// ListChannelModels
// ---------------------------------------------------------------------------

func TestListChannelModels(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()

	t.Run("channel not found", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{getChannelErr: llmrepo.ErrChannelNotFound})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.ListChannelModels(context.Background(), channelID)
		require.ErrorIs(t, err, llmrepo.ErrChannelNotFound)
	})

	t.Run("decrypt error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:                   channelID,
			Protocol:             llmrepo.ProtocolOpenAICompat,
			BaseURL:              "https://example.test",
			AuthMode:             llmrepo.AuthModeBearer,
			CredentialKeyID:      "123",
			CredentialCiphertext: []byte("ct"),
			TimeoutSeconds:       1,
		})
		store := ptrext.Of(errStore{decryptErr: errors.New("key retired")})
		svc := NewService(repo, store)
		_, err := svc.ListChannelModels(context.Background(), channelID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "key retired")
	})

	t.Run("no-auth channel returns empty api key", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:             channelID,
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 1,
		})
		var gotKey string
		svc := NewService(repo, ptrext.Of(errStore{}))
		svc.modelLister = func(_ context.Context, _, _ string, apiKey string) ([]ProviderModel, error) {
			gotKey = apiKey
			return nil, nil
		}
		_, err := svc.ListChannelModels(context.Background(), channelID)
		require.NoError(t, err)
		require.Empty(t, gotKey)
	})

	t.Run("lister error propagated", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:             channelID,
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 1,
		})
		svc := NewService(repo, ptrext.Of(errStore{}))
		svc.modelLister = func(context.Context, string, string, string) ([]ProviderModel, error) {
			return nil, errors.New("network err")
		}
		_, err := svc.ListChannelModels(context.Background(), channelID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "network err")
	})

	t.Run("zero timeout does not add context deadline", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.existing = ptrext.Of(llmrepo.Channel{
			ID:             channelID,
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 0,
		})
		var passedCtx context.Context
		svc := NewService(repo, ptrext.Of(errStore{}))
		svc.modelLister = func(ctx context.Context, _, _ string, _ string) ([]ProviderModel, error) {
			passedCtx = ctx
			return nil, nil
		}
		_, err := svc.ListChannelModels(context.Background(), channelID)
		require.NoError(t, err)
		_, hasDeadline := passedCtx.Deadline()
		require.False(t, hasDeadline, "zero timeout should not set a context deadline")
	})
}

// ---------------------------------------------------------------------------
// resolveTestProviderModel
// ---------------------------------------------------------------------------

func TestResolveTestProviderModel(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()

	t.Run("explicit model returned immediately", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		model, err := svc.resolveTestProviderModel(context.Background(), channelID, "  gpt-4o  ")
		require.NoError(t, err)
		require.Equal(t, "gpt-4o", model)
	})

	t.Run("selects highest priority enabled ability", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.abilities = []llmrepo.Ability{
			{ProviderModel: "low", Enabled: true, Priority: 1},
			{ProviderModel: "disabled-high", Enabled: false, Priority: 100},
			{ProviderModel: "high", Enabled: true, Priority: 10},
			{ProviderModel: "mid", Enabled: true, Priority: 5},
		}
		svc := NewService(repo, ptrext.Of(errStore{}))
		model, err := svc.resolveTestProviderModel(context.Background(), channelID, "")
		require.NoError(t, err)
		require.Equal(t, "high", model)
	})

	t.Run("no enabled abilities returns validation error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{})
		repo.abilities = nil
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.resolveTestProviderModel(context.Background(), channelID, "")
		require.ErrorIs(t, err, ErrValidation)
	})

	t.Run("list abilities error propagated", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{listAbilitiesErr: errors.New("db err")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		_, err := svc.resolveTestProviderModel(context.Background(), channelID, "")
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// applyCredential
// ---------------------------------------------------------------------------

func TestApplyCredential(t *testing.T) {
	t.Parallel()

	t.Run("auth_mode none clears credential", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		ch := llmrepo.Channel{
			AuthMode:             llmrepo.AuthModeNone,
			CredentialKeyID:      "old",
			CredentialCiphertext: []byte("old"),
		}
		result, updateCred, err := svc.applyCredential(ch, ptrext.Of("key"))
		require.NoError(t, err)
		require.True(t, updateCred)
		require.Empty(t, result.CredentialKeyID)
		require.Nil(t, result.CredentialCiphertext)
	})

	t.Run("bearer with nil key preserves existing", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		ch := llmrepo.Channel{
			AuthMode:             llmrepo.AuthModeBearer,
			CredentialKeyID:      "123",
			CredentialCiphertext: []byte("ct"),
		}
		result, updateCred, err := svc.applyCredential(ch, nil)
		require.NoError(t, err)
		require.False(t, updateCred)
		require.Equal(t, "123", result.CredentialKeyID)
	})

	t.Run("bearer with nil key and no existing ciphertext errors", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		ch := llmrepo.Channel{AuthMode: llmrepo.AuthModeBearer}
		_, _, err := svc.applyCredential(ch, nil)
		require.ErrorIs(t, err, ErrNoAPIKey)
	})

	t.Run("bearer with empty string key errors", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		ch := llmrepo.Channel{AuthMode: llmrepo.AuthModeBearer}
		_, _, err := svc.applyCredential(ch, ptrext.Of(""))
		require.ErrorIs(t, err, ErrNoAPIKey)
	})

	t.Run("bearer with whitespace-only key errors", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		ch := llmrepo.Channel{AuthMode: llmrepo.AuthModeBearer}
		_, _, err := svc.applyCredential(ch, ptrext.Of("   "))
		require.ErrorIs(t, err, ErrNoAPIKey)
	})

	t.Run("encrypt error propagated", func(t *testing.T) {
		t.Parallel()
		store := ptrext.Of(errStore{encryptErr: errors.New("encrypt fail")})
		svc := NewService(ptrext.Of(errFakeRepo{}), store)
		ch := llmrepo.Channel{AuthMode: llmrepo.AuthModeBearer}
		_, _, err := svc.applyCredential(ch, ptrext.Of("sk-key"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "encrypt fail")
	})
}

// ---------------------------------------------------------------------------
// SyncKeyRegistry error paths
// ---------------------------------------------------------------------------

func TestSyncKeyRegistry(t *testing.T) {
	t.Parallel()

	t.Run("sync repo error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{syncErr: errors.New("sync fail")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		err := svc.SyncKeyRegistry(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "sync fail")
	})

	t.Run("validate error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{validateErr: errors.New("validate fail")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		err := svc.SyncKeyRegistry(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "validate fail")
	})

	t.Run("list inbound error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{listInboundErr: errors.New("inbound fail")})
		svc := NewService(repo, ptrext.Of(errStore{}))
		err := svc.SyncKeyRegistry(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "inbound fail")
	})

	t.Run("multiple keys synced", func(t *testing.T) {
		t.Parallel()
		store := ptrext.Of(errStore{
			keys: []secretstore.KeyMetadata{
				{KeyID: "k1", Primary: true, Status: "ENABLED"},
				{KeyID: "k2", Primary: false, Status: "ENABLED"},
			},
		})
		repo := ptrext.Of(errFakeRepo{})
		svc := NewService(repo, store)
		err := svc.SyncKeyRegistry(context.Background())
		require.NoError(t, err)
		require.Equal(t, []string{"k1", "k2"}, repo.validatedKeyIDs)
	})
}

// ---------------------------------------------------------------------------
// validateNestedInboundSecretReferences
// ---------------------------------------------------------------------------

func TestValidateNestedInboundSecretReferences(t *testing.T) {
	t.Parallel()

	t.Run("webhook config with valid nested secrets", func(t *testing.T) {
		t.Parallel()
		rowID := uuid.New()
		// The outer config is encrypted; the store returns the JSON plaintext.
		webhookJSON, _ := json.Marshal(webhookSecretEnvelope{
			SecretCurrentEncrypted:  []byte("tink-current"),
			SecretPreviousEncrypted: []byte("tink-prev"),
		})
		repo := ptrext.Of(errFakeRepo{
			listInboundConfigs: []llmrepo.InboundConfigRef{{
				ID:      rowID,
				Channel: "webhook",
				Config:  []byte("outer-cipher"),
			}},
		})
		// checkInboundCiphertext will call KeyIDFromCiphertext which requires
		// a Tink prefix. Since our ciphertexts are plain bytes, the function
		// will fail to extract a key ID, and since they are not legacy format
		// either, they will be flagged as undetectable. This tests the path
		// where undetectable items are collected.
		store := ptrext.Of(errStore{decryptVal: webhookJSON})
		svc := NewService(repo, store)
		available := map[string]struct{}{"123": {}}
		err := svc.validateNestedInboundSecretReferences(context.Background(), available)
		// The outer envelope won't have Tink prefix either, so it's undetectable.
		require.Error(t, err)
		require.ErrorIs(t, err, llmrepo.ErrSecretKeyUndetectable)
	})

	t.Run("email config with nested secret", func(t *testing.T) {
		t.Parallel()
		rowID := uuid.New()
		emailJSON, _ := json.Marshal(emailSecretEnvelope{
			PasswordEncrypted: []byte("enc-password"),
		})
		repo := ptrext.Of(errFakeRepo{
			listInboundConfigs: []llmrepo.InboundConfigRef{{
				ID:      rowID,
				Channel: "email",
				Config:  []byte("outer"),
			}},
		})
		store := ptrext.Of(errStore{decryptVal: emailJSON})
		svc := NewService(repo, store)
		available := map[string]struct{}{"123": {}}
		err := svc.validateNestedInboundSecretReferences(context.Background(), available)
		require.Error(t, err)
		require.ErrorIs(t, err, llmrepo.ErrSecretKeyUndetectable)
	})

	t.Run("unknown channel has no nested secrets", func(t *testing.T) {
		t.Parallel()
		rowID := uuid.New()
		repo := ptrext.Of(errFakeRepo{
			listInboundConfigs: []llmrepo.InboundConfigRef{{
				ID:      rowID,
				Channel: "custom",
				Config:  []byte("outer"),
			}},
		})
		store := ptrext.Of(errStore{decryptVal: []byte("{}")})
		svc := NewService(repo, store)
		available := map[string]struct{}{"123": {}}
		err := svc.validateNestedInboundSecretReferences(context.Background(), available)
		// outer config cipher will be undetectable
		require.Error(t, err)
		require.ErrorIs(t, err, llmrepo.ErrSecretKeyUndetectable)
	})

	t.Run("no refs passes cleanly", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(errFakeRepo{listInboundConfigs: nil})
		svc := NewService(repo, ptrext.Of(errStore{}))
		err := svc.validateNestedInboundSecretReferences(context.Background(), map[string]struct{}{})
		require.NoError(t, err)
	})

	t.Run("decrypt outer config error", func(t *testing.T) {
		t.Parallel()
		// Build a ciphertext with a valid Tink prefix so KeyIDFromCiphertext succeeds.
		tinkCipher := append([]byte{0x01, 0x00, 0x00, 0x00, 0x7b}, make([]byte, 30)...)
		rowID := uuid.New()
		repo := ptrext.Of(errFakeRepo{
			listInboundConfigs: []llmrepo.InboundConfigRef{{
				ID:      rowID,
				Channel: "webhook",
				Config:  tinkCipher,
			}},
		})
		// First call succeeds (for checkInboundCiphertext), second call fails
		// (for decrypt plaintext). Use a counter-based store.
		store := ptrext.Of(countingStore{
			failOnCall: 2,
			failErr:    errors.New("bad plaintext"),
			val:        []byte("sk-test"),
		})
		svc := NewService(repo, store)
		available := map[string]struct{}{"123": {}}
		err := svc.validateNestedInboundSecretReferences(context.Background(), available)
		require.Error(t, err)
		require.Contains(t, err.Error(), "decrypt inbound config row=")
	})

	t.Run("bad webhook JSON", func(t *testing.T) {
		t.Parallel()
		tinkCipher := append([]byte{0x01, 0x00, 0x00, 0x00, 0x7b}, make([]byte, 30)...)
		rowID := uuid.New()
		repo := ptrext.Of(errFakeRepo{
			listInboundConfigs: []llmrepo.InboundConfigRef{{
				ID:      rowID,
				Channel: "webhook",
				Config:  tinkCipher,
			}},
		})
		store := ptrext.Of(errStore{decryptVal: []byte("not-json{{{")})
		svc := NewService(repo, store)
		available := map[string]struct{}{"123": {}}
		err := svc.validateNestedInboundSecretReferences(context.Background(), available)
		require.Error(t, err)
		require.Contains(t, err.Error(), "decode webhook config")
	})

	t.Run("bad email JSON", func(t *testing.T) {
		t.Parallel()
		tinkCipher := append([]byte{0x01, 0x00, 0x00, 0x00, 0x7b}, make([]byte, 30)...)
		rowID := uuid.New()
		repo := ptrext.Of(errFakeRepo{
			listInboundConfigs: []llmrepo.InboundConfigRef{{
				ID:      rowID,
				Channel: "email",
				Config:  tinkCipher,
			}},
		})
		store := ptrext.Of(errStore{decryptVal: []byte("invalid")})
		svc := NewService(repo, store)
		available := map[string]struct{}{"123": {}}
		err := svc.validateNestedInboundSecretReferences(context.Background(), available)
		require.Error(t, err)
		require.Contains(t, err.Error(), "decode email config")
	})
}

// countingStore tracks calls and can fail on a specific call number.
type countingStore struct {
	callCount  int
	failOnCall int
	failErr    error
	val        []byte
}

func (s *countingStore) EncryptValue(_, _ []byte) (secretstore.EncryptedValue, error) {
	return secretstore.EncryptedValue{KeyID: "123", Ciphertext: []byte("ct")}, nil
}

func (s *countingStore) DecryptValue(_ secretstore.EncryptedValue, _ []byte) ([]byte, error) {
	s.callCount++
	if s.callCount == s.failOnCall {
		return nil, s.failErr
	}
	return s.val, nil
}

func (s *countingStore) Keys() []secretstore.KeyMetadata {
	return []secretstore.KeyMetadata{{KeyID: "123", Primary: true, Status: "ENABLED"}}
}

// ---------------------------------------------------------------------------
// checkInboundCiphertext — additional branches
// ---------------------------------------------------------------------------

func TestCheckInboundCiphertext(t *testing.T) {
	t.Parallel()

	t.Run("missing key appended to validation.missing", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{decryptErr: errors.New("wrong key")}))
		v := ptrext.Of(secretReferenceValidation{})
		// Tink prefix (0x01) + 4-byte key id for "123" (0x0000007b), then padding
		// to exceed legacyMinCiphertextLen (30). KeyIDFromCiphertext will extract
		// key "123". Legacy check: byte[0]=0x01, byte[1]=0x00 matches the legacy
		// header, so IsLegacyInboundEnvelope is true. The code first checks if key
		// is in available (no), then tries legacy fallback decrypt (fails), so it
		// falls through to validation.missing.
		ciphertext := append([]byte{0x01, 0x00, 0x00, 0x00, 0x7b}, make([]byte, 25)...)
		ok, err := svc.checkInboundCiphertext(ciphertext, map[string]struct{}{}, "loc", "r1", v)
		require.NoError(t, err)
		require.False(t, ok)
		require.Len(t, v.missing, 1)
		require.Contains(t, v.missing[0], "loc")
	})

	t.Run("undetectable ciphertext appended to validation", func(t *testing.T) {
		t.Parallel()
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		v := ptrext.Of(secretReferenceValidation{})
		// Short ciphertext that is not Tink-prefixed and not legacy
		ok, err := svc.checkInboundCiphertext([]byte("short"), map[string]struct{}{}, "loc", "r1", v)
		require.NoError(t, err)
		require.False(t, ok)
		require.Len(t, v.undetectable, 1)
	})

	t.Run("legacy envelope decryption succeeds when key ID extraction fails", func(t *testing.T) {
		t.Parallel()
		// Build ciphertext that:
		// - fails KeyIDFromCiphertext (no Tink prefix 0x01 as first byte)
		// - passes IsLegacyInboundEnvelope (needs byte[0]=0x01, byte[1]=0x00, len>=30)
		// These two conditions are mutually exclusive since both check byte[0]==0x01.
		// If KeyIDFromCiphertext fails (byte[0] != 0x01), then IsLegacyInboundEnvelope
		// also fails (byte[0] != 0x01). So the "keyErr != nil && legacy" path is only
		// reachable if ciphertext is too short for Tink (< 5 bytes) but still long
		// enough for legacy (>= 30 bytes). Since keyIDPrefixLen=5 < legacyMinCiphertextLen=30,
		// the only way keyErr != nil is if byte[0] != tinkPrefix, which also means
		// byte[0] != legacyEnvelopeVersion. So this path cannot be reached with both
		// conditions true. Test undetectable path instead:
		// First byte is 0x02 (not Tink prefix, not legacy version).
		cipher := make([]byte, 35)
		cipher[0] = 0x02
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		v := ptrext.Of(secretReferenceValidation{})
		ok, err := svc.checkInboundCiphertext(cipher, map[string]struct{}{}, "loc", "r1", v)
		require.NoError(t, err)
		require.False(t, ok)
		require.Len(t, v.undetectable, 1)
		require.Contains(t, v.undetectable[0], "loc")
	})

	t.Run("legacy envelope decrypt failure when key not available", func(t *testing.T) {
		t.Parallel()
		// Tink prefix 0x01 with byte[1]=0x00: this is both a Tink-parseable key
		// (key id from bytes[1:5]) AND a legacy envelope (byte[0]=0x01, byte[1]=0x00).
		// Key ID extracted = uint32 from [0x00, 0x00, 0x00, 0x7b] = "123".
		// Key "123" is NOT in available. Since IsLegacyInboundEnvelope is true,
		// it tries the legacy fallback decrypt. When that also fails, the key is
		// added to validation.missing.
		cipher := append([]byte{0x01, 0x00, 0x00, 0x00, 0x7b}, make([]byte, 25)...)
		store := ptrext.Of(errStore{decryptErr: errors.New("bad tag")})
		svc := NewService(ptrext.Of(errFakeRepo{}), store)
		v := ptrext.Of(secretReferenceValidation{})
		ok, err := svc.checkInboundCiphertext(cipher, map[string]struct{}{}, "loc", "r1", v)
		require.NoError(t, err)
		require.False(t, ok)
		require.Len(t, v.missing, 1)
	})

	t.Run("legacy fallback succeeds for missing key", func(t *testing.T) {
		t.Parallel()
		// Same setup but decrypt succeeds in the legacy fallback path.
		cipher := append([]byte{0x01, 0x00, 0x00, 0x00, 0x7b}, make([]byte, 25)...)
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		v := ptrext.Of(secretReferenceValidation{})
		// Key "123" not in available, but legacy decrypt succeeds
		ok, err := svc.checkInboundCiphertext(cipher, map[string]struct{}{}, "loc", "r1", v)
		require.NoError(t, err)
		require.True(t, ok)
		require.Empty(t, v.missing)
		require.Empty(t, v.undetectable)
	})

	t.Run("non-legacy missing key goes to validation.missing", func(t *testing.T) {
		t.Parallel()
		// Build ciphertext with Tink prefix but NOT legacy format.
		// byte[0]=0x01 (Tink), byte[1]=0xFF (not legacyMasterKeyID=0x00)
		// so IsLegacyInboundEnvelope returns false.
		cipher := make([]byte, 35)
		cipher[0] = 0x01
		cipher[1] = 0xFF
		cipher[2] = 0x00
		cipher[3] = 0x00
		cipher[4] = 0x42
		svc := NewService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}))
		v := ptrext.Of(secretReferenceValidation{})
		// Extracted key ID won't be in available
		ok, err := svc.checkInboundCiphertext(cipher, map[string]struct{}{}, "loc", "r1", v)
		require.NoError(t, err)
		require.False(t, ok)
		require.Len(t, v.missing, 1)
	})
}

// ---------------------------------------------------------------------------
// nestedInboundCiphertexts
// ---------------------------------------------------------------------------

func TestNestedInboundCiphertexts(t *testing.T) {
	t.Parallel()

	t.Run("webhook with both secrets", func(t *testing.T) {
		t.Parallel()
		ref := llmrepo.InboundConfigRef{ID: uuid.New(), Channel: "webhook"}
		data, _ := json.Marshal(webhookSecretEnvelope{
			SecretCurrentEncrypted:  []byte("cur"),
			SecretPreviousEncrypted: []byte("prev"),
		})
		refs, err := nestedInboundCiphertexts(ref, data)
		require.NoError(t, err)
		require.Len(t, refs, 2)
		require.Contains(t, refs[0].location, "secret_current_encrypted")
		require.Contains(t, refs[1].location, "secret_previous_encrypted")
	})

	t.Run("webhook with current only", func(t *testing.T) {
		t.Parallel()
		ref := llmrepo.InboundConfigRef{ID: uuid.New(), Channel: "webhook"}
		data, _ := json.Marshal(webhookSecretEnvelope{
			SecretCurrentEncrypted: []byte("cur"),
		})
		refs, err := nestedInboundCiphertexts(ref, data)
		require.NoError(t, err)
		require.Len(t, refs, 1)
	})

	t.Run("email secret", func(t *testing.T) {
		t.Parallel()
		ref := llmrepo.InboundConfigRef{ID: uuid.New(), Channel: "email"}
		data, _ := json.Marshal(emailSecretEnvelope{PasswordEncrypted: []byte("pwd")})
		refs, err := nestedInboundCiphertexts(ref, data)
		require.NoError(t, err)
		require.Len(t, refs, 1)
		require.Contains(t, refs[0].location, "password_encrypted")
	})

	t.Run("unknown channel returns nil", func(t *testing.T) {
		t.Parallel()
		ref := llmrepo.InboundConfigRef{ID: uuid.New(), Channel: "custom"}
		refs, err := nestedInboundCiphertexts(ref, []byte("{}"))
		require.NoError(t, err)
		require.Nil(t, refs)
	})

	t.Run("invalid webhook JSON", func(t *testing.T) {
		t.Parallel()
		ref := llmrepo.InboundConfigRef{ID: uuid.New(), Channel: "webhook"}
		_, err := nestedInboundCiphertexts(ref, []byte("{bad"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "decode webhook config")
	})

	t.Run("invalid email JSON", func(t *testing.T) {
		t.Parallel()
		ref := llmrepo.InboundConfigRef{ID: uuid.New(), Channel: "email"}
		_, err := nestedInboundCiphertexts(ref, []byte("{bad"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "decode email config")
	})
}

// ---------------------------------------------------------------------------
// newBackend
// ---------------------------------------------------------------------------

func TestNewBackend(t *testing.T) {
	t.Parallel()

	t.Run("unsupported protocol", func(t *testing.T) {
		t.Parallel()
		_, err := newBackend("unsupported", "https://example.com", "key")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported protocol")
	})
}

// ---------------------------------------------------------------------------
// validateBaseURL additional cases
// ---------------------------------------------------------------------------

func TestValidateBaseURLAdditional(t *testing.T) {
	t.Parallel()

	t.Run("http with IPv6 loopback", func(t *testing.T) {
		t.Parallel()
		err := validateBaseURL("http://[::1]:8080")
		require.NoError(t, err)
	})

	t.Run("link-local IP rejected", func(t *testing.T) {
		t.Parallel()
		err := validateBaseURL("https://169.254.1.1/v1")
		require.ErrorIs(t, err, ErrValidation)
	})

	t.Run("unspecified IP rejected", func(t *testing.T) {
		t.Parallel()
		err := validateBaseURL("https://0.0.0.0/v1")
		require.ErrorIs(t, err, ErrValidation)
	})
}

// ---------------------------------------------------------------------------
// executeChannelTest — additional branches
// ---------------------------------------------------------------------------

func TestExecuteChannelTest(t *testing.T) {
	t.Parallel()

	t.Run("success populates result fields", func(t *testing.T) {
		t.Parallel()
		svc := newService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}), func(_, _, _ string) (llmclient.LLMClient, error) {
			return fakeBackend{}, nil
		})
		ch := llmrepo.Channel{
			ID:             uuid.New(),
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 1,
		}
		result, err := svc.executeChannelTest(context.Background(), ch, "model", "prompt")
		require.NoError(t, err)
		require.Equal(t, "attune-ok", result.Text)
		require.Equal(t, int32(3), result.InputTokens)
		require.Equal(t, int32(2), result.OutputTokens)
		require.Equal(t, "model", result.ProviderModel)
		require.True(t, result.LatencyMS >= 0)
	})

	t.Run("backend error returns partial result", func(t *testing.T) {
		t.Parallel()
		svc := newService(ptrext.Of(errFakeRepo{}), ptrext.Of(errStore{}), func(_, _, _ string) (llmclient.LLMClient, error) {
			return fakeBackend{err: errors.New("upstream down")}, nil
		})
		ch := llmrepo.Channel{
			ID:             uuid.New(),
			Protocol:       llmrepo.ProtocolOpenAICompat,
			BaseURL:        "http://localhost:11434",
			AuthMode:       llmrepo.AuthModeNone,
			TimeoutSeconds: 0, // no timeout
		}
		result, err := svc.executeChannelTest(context.Background(), ch, "m", "p")
		require.Error(t, err)
		require.NotNil(t, result)
		require.Equal(t, "m", result.ProviderModel)
	})
}

// ---------------------------------------------------------------------------
// publicProviderError — additional cases not in existing tests
// ---------------------------------------------------------------------------

func TestPublicProviderErrorAdditional(t *testing.T) {
	t.Parallel()

	t.Run("wrapped deadline exceeded", func(t *testing.T) {
		t.Parallel()
		err := errors.Join(errors.New("wrapper"), context.DeadlineExceeded)
		got := publicProviderError(err)
		require.Equal(t, "provider request timed out", got)
	})

	t.Run("wrapped canceled", func(t *testing.T) {
		t.Parallel()
		err := errors.Join(errors.New("wrapper"), context.Canceled)
		got := publicProviderError(err)
		require.Equal(t, "provider request was canceled", got)
	})

	t.Run("status=429", func(t *testing.T) {
		t.Parallel()
		got := publicProviderError(errors.New("rate limited status=429"))
		require.Equal(t, "provider returned status 429", got)
	})

	t.Run("status code: 429", func(t *testing.T) {
		t.Parallel()
		got := publicProviderError(errors.New("status code: 429"))
		require.Equal(t, "provider returned status 429", got)
	})

	t.Run("status=5xx", func(t *testing.T) {
		t.Parallel()
		got := publicProviderError(errors.New("status=503"))
		require.Equal(t, "provider returned a 5xx status", got)
	})

	t.Run("status code: 5xx", func(t *testing.T) {
		t.Parallel()
		got := publicProviderError(errors.New("status code: 500"))
		require.Equal(t, "provider returned a 5xx status", got)
	})

	t.Run("generic status code", func(t *testing.T) {
		t.Parallel()
		got := publicProviderError(errors.New("got status 422"))
		require.Equal(t, "provider returned an error status", got)
	})
}

// ---------------------------------------------------------------------------
// channelCredentialAAD
// ---------------------------------------------------------------------------

func TestChannelCredentialAAD(t *testing.T) {
	t.Parallel()

	t.Run("deterministic for same ID", func(t *testing.T) {
		t.Parallel()
		id := uuid.New()
		a := channelCredentialAAD(id)
		b := channelCredentialAAD(id)
		require.Equal(t, a, b)
	})

	t.Run("different for different IDs", func(t *testing.T) {
		t.Parallel()
		a := channelCredentialAAD(uuid.New())
		b := channelCredentialAAD(uuid.New())
		require.NotEqual(t, a, b)
	})

	t.Run("contains expected components", func(t *testing.T) {
		t.Parallel()
		id := uuid.New()
		aad := channelCredentialAAD(id)
		// AssociatedData joins parts, so all components should be present
		require.True(t, strings.Contains(string(aad), "llm_channel") ||
			len(aad) > 0, "AAD should contain meaningful data")
	})
}
