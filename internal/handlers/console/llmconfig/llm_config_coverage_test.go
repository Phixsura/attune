// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test fixtures use raw pointer comparisons.
package llmconfig

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	llmsvc "github.com/Phixsura/attune/internal/service/llmconfig"
)

// configurableFakeService wraps configurable error returns per method,
// enabling error-path testing of handler methods.
type configurableFakeService struct {
	channels      []llmrepo.Channel
	channel       llmrepo.Channel
	ability       llmrepo.Ability
	route         llmrepo.Route
	abilities     []llmrepo.Ability
	routes        []llmrepo.Route
	models        []llmsvc.ProviderModel
	testResult    *llmsvc.ChannelTestResult
	listChErr     error
	getChErr      error
	createChErr   error
	updateChErr   error
	testChErr     error
	listModelsErr error
	deleteChErr   error
	listAbErr     error
	upsertAbErr   error
	deleteAbErr   error
	listRtErr     error
	upsertRtErr   error
	deleteRtErr   error
}

func (f *configurableFakeService) ListChannels(context.Context) ([]llmrepo.Channel, error) {
	return f.channels, f.listChErr
}

func (f *configurableFakeService) GetChannel(context.Context, uuid.UUID) (*llmrepo.Channel, error) {
	if f.getChErr != nil {
		return nil, f.getChErr
	}
	return ptrext.Of(f.channel), nil
}

func (f *configurableFakeService) CreateChannel(context.Context, llmsvc.ChannelInput) (*llmrepo.Channel, error) {
	if f.createChErr != nil {
		return nil, f.createChErr
	}
	return ptrext.Of(f.channel), nil
}

func (f *configurableFakeService) UpdateChannel(context.Context, uuid.UUID, llmsvc.ChannelInput) (*llmrepo.Channel, error) {
	if f.updateChErr != nil {
		return nil, f.updateChErr
	}
	return ptrext.Of(f.channel), nil
}

func (f *configurableFakeService) TestChannel(context.Context, uuid.UUID, llmsvc.TestChannelInput) (*llmsvc.ChannelTestResult, error) {
	if f.testChErr != nil {
		return nil, f.testChErr
	}
	if f.testResult != nil {
		return f.testResult, nil
	}
	return ptrext.Of(llmsvc.ChannelTestResult{
		ProviderModel: "gpt-4.1",
		InputTokens:   10,
		OutputTokens:  5,
		LatencyMS:     25,
		Channel:       f.channel,
	}), nil
}

func (f *configurableFakeService) ListChannelModels(context.Context, uuid.UUID) ([]llmsvc.ProviderModel, error) {
	if f.listModelsErr != nil {
		return nil, f.listModelsErr
	}
	return f.models, nil
}

func (f *configurableFakeService) DeleteChannel(context.Context, uuid.UUID) error {
	return f.deleteChErr
}

func (f *configurableFakeService) ListAbilities(context.Context, uuid.UUID) ([]llmrepo.Ability, error) {
	if f.listAbErr != nil {
		return nil, f.listAbErr
	}
	return f.abilities, nil
}

func (f *configurableFakeService) UpsertAbility(context.Context, uuid.UUID, llmsvc.AbilityInput) (*llmrepo.Ability, error) {
	if f.upsertAbErr != nil {
		return nil, f.upsertAbErr
	}
	return ptrext.Of(f.ability), nil
}

func (f *configurableFakeService) DeleteAbility(context.Context, uuid.UUID, string) error {
	return f.deleteAbErr
}

func (f *configurableFakeService) ListRoutes(context.Context) ([]llmrepo.Route, error) {
	if f.listRtErr != nil {
		return nil, f.listRtErr
	}
	return f.routes, nil
}

func (f *configurableFakeService) UpsertRoute(context.Context, llmsvc.RouteInput) (*llmrepo.Route, error) {
	if f.upsertRtErr != nil {
		return nil, f.upsertRtErr
	}
	return ptrext.Of(f.route), nil
}

func (f *configurableFakeService) DeleteRoute(context.Context, string, string) error {
	return f.deleteRtErr
}

func testCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	})
}

func testChannel() llmrepo.Channel {
	now := time.Now().UTC()
	return llmrepo.Channel{
		ID:             uuid.New(),
		Name:           "primary",
		Protocol:       llmrepo.ProtocolOpenAICompat,
		BaseURL:        "https://example.com",
		AuthMode:       llmrepo.AuthModeBearer,
		Status:         llmrepo.ChannelStatusEnabled,
		Priority:       10,
		Weight:         20,
		TimeoutSeconds: 30,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// ---------------------------------------------------------------------------
// Proto conversion functions
// ---------------------------------------------------------------------------

func TestToChannelProto(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	tested := time.Date(2025, 6, 2, 8, 0, 0, 0, time.UTC)
	chID := uuid.New()

	t.Run("all fields populated with LastTestedAt", func(t *testing.T) {
		t.Parallel()
		row := llmrepo.Channel{
			ID:                   chID,
			Name:                 "primary",
			Protocol:             llmrepo.ProtocolOpenAICompat,
			BaseURL:              "https://api.example.com",
			AuthMode:             llmrepo.AuthModeBearer,
			CredentialCiphertext: []byte("encrypted"),
			CredentialKeyID:      "key-1",
			Status:               llmrepo.ChannelStatusEnabled,
			Priority:             10,
			Weight:               20,
			TimeoutSeconds:       30,
			CreatedAt:            now,
			UpdatedAt:            now,
			LastTestedAt:         ptrext.Of(tested),
			LastTestStatus:       "pass",
			LastError:            "",
		}
		out := toChannelProto(row)
		require.Equal(t, chID.String(), out.Id)
		require.Equal(t, "primary", out.Name)
		require.Equal(t, llmrepo.ProtocolOpenAICompat, out.Protocol)
		require.Equal(t, "https://api.example.com", out.BaseUrl)
		require.Equal(t, llmrepo.AuthModeBearer, out.AuthMode)
		require.True(t, out.HasApiKey)
		require.Equal(t, "key-1", out.CredentialKeyId)
		require.Equal(t, llmrepo.ChannelStatusEnabled, out.Status)
		require.Equal(t, int32(10), out.Priority)
		require.Equal(t, int32(20), out.Weight)
		require.Equal(t, int32(30), out.TimeoutSeconds)
		require.Equal(t, now.Format(time.RFC3339), out.CreatedAt)
		require.Equal(t, now.Format(time.RFC3339), out.UpdatedAt)
		require.NotNil(t, out.LastTestedAt)
		require.Equal(t, tested.Format(time.RFC3339), ptrext.Indirect(out.LastTestedAt))
		require.Equal(t, "pass", out.LastTestStatus)
		require.Equal(t, "", out.LastError)
	})

	t.Run("nil LastTestedAt and no credential", func(t *testing.T) {
		t.Parallel()
		row := llmrepo.Channel{
			ID:                   chID,
			Name:                 "secondary",
			Protocol:             llmrepo.ProtocolAnthropic,
			BaseURL:              "https://api2.example.com",
			AuthMode:             llmrepo.AuthModeNone,
			CredentialCiphertext: nil,
			Status:               llmrepo.ChannelStatusDisabled,
			CreatedAt:            now,
			UpdatedAt:            now,
			LastTestedAt:         nil,
		}
		out := toChannelProto(row)
		require.False(t, out.HasApiKey)
		require.Nil(t, out.LastTestedAt)
		require.Equal(t, llmrepo.AuthModeNone, out.AuthMode)
	})
}

func TestToChannelTestProto(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	ch := llmrepo.Channel{
		ID:        uuid.New(),
		Name:      "test-ch",
		Protocol:  llmrepo.ProtocolOpenAICompat,
		CreatedAt: now,
		UpdatedAt: now,
	}
	result := llmsvc.ChannelTestResult{
		Channel:       ch,
		ProviderModel: "gpt-4.1",
		Text:          "hello world",
		InputTokens:   42,
		OutputTokens:  17,
		LatencyMS:     123,
	}
	out := toChannelTestProto(result)
	require.True(t, out.Ok)
	require.Equal(t, "gpt-4.1", out.ProviderModel)
	require.Equal(t, "hello world", out.Text)
	require.Equal(t, int32(42), out.InputTokens)
	require.Equal(t, int32(17), out.OutputTokens)
	require.Equal(t, int64(123), out.LatencyMs)
	require.NotNil(t, out.Channel)
	require.Equal(t, ch.ID.String(), out.Channel.Id)
}

func TestToProviderModelProto(t *testing.T) {
	t.Parallel()

	m := llmsvc.ProviderModel{
		ID:          "gpt-4.1",
		DisplayName: "GPT-4.1",
		OwnedBy:     "openai",
	}
	out := toProviderModelProto(m)
	require.Equal(t, "gpt-4.1", out.Id)
	require.Equal(t, "GPT-4.1", out.DisplayName)
	require.Equal(t, "openai", out.OwnedBy)
}

func TestToAbilityProto(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	abID := uuid.New()
	chID := uuid.New()
	row := llmrepo.Ability{
		ID:            abID,
		ChannelID:     chID,
		LogicalModel:  "gpt-4.1",
		ProviderModel: "gpt-4.1-turbo",
		Enabled:       true,
		Priority:      5,
		Weight:        10,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	out := toAbilityProto(row)
	require.Equal(t, abID.String(), out.Id)
	require.Equal(t, chID.String(), out.ChannelId)
	require.Equal(t, "gpt-4.1", out.LogicalModel)
	require.Equal(t, "gpt-4.1-turbo", out.ProviderModel)
	require.True(t, out.Enabled)
	require.Equal(t, int32(5), out.Priority)
	require.Equal(t, int32(10), out.Weight)
	require.Equal(t, now.Format(time.RFC3339), out.CreatedAt)
	require.Equal(t, now.Format(time.RFC3339), out.UpdatedAt)
}

func TestToRouteProto(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	rtID := uuid.New()
	row := llmrepo.Route{
		ID:           rtID,
		TenantID:     "tenant-1",
		Purpose:      "enrich",
		LogicalModel: "gpt-4.1",
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	out := toRouteProto(row)
	require.Equal(t, rtID.String(), out.Id)
	require.Equal(t, "tenant-1", out.TenantId)
	require.Equal(t, "enrich", out.Purpose)
	require.Equal(t, "gpt-4.1", out.LogicalModel)
	require.True(t, out.Enabled)
	require.Equal(t, now.Format(time.RFC3339), out.CreatedAt)
	require.Equal(t, now.Format(time.RFC3339), out.UpdatedAt)
}

// ---------------------------------------------------------------------------
// parseID
// ---------------------------------------------------------------------------

func TestParseID(t *testing.T) {
	t.Parallel()

	t.Run("valid UUID", func(t *testing.T) {
		t.Parallel()
		id := uuid.New()
		got, err := parseID(id.String())
		require.NoError(t, err)
		require.Equal(t, id, got)
	})

	t.Run("invalid UUID", func(t *testing.T) {
		t.Parallel()
		_, err := parseID("not-a-uuid")
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("empty string", func(t *testing.T) {
		t.Parallel()
		_, err := parseID("")
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})
}

// ---------------------------------------------------------------------------
// isValidation
// ---------------------------------------------------------------------------

func TestIsValidation(t *testing.T) {
	t.Parallel()

	t.Run("ErrValidation", func(t *testing.T) {
		t.Parallel()
		require.True(t, isValidation(llmsvc.ErrValidation))
	})

	t.Run("wrapped ErrValidation", func(t *testing.T) {
		t.Parallel()
		require.True(t, isValidation(fmt.Errorf("wrap: %w", llmsvc.ErrValidation)))
	})

	t.Run("ErrNoAPIKey", func(t *testing.T) {
		t.Parallel()
		require.True(t, isValidation(llmsvc.ErrNoAPIKey))
	})

	t.Run("generic error", func(t *testing.T) {
		t.Parallel()
		require.False(t, isValidation(errors.New("something else")))
	})

	t.Run("nil error", func(t *testing.T) {
		t.Parallel()
		require.False(t, isValidation(nil))
	})
}

// ---------------------------------------------------------------------------
// Error mapper functions
// ---------------------------------------------------------------------------

func TestChannelWriteError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   attunev1.ErrorCode
	}{
		{"channel not found", llmrepo.ErrChannelNotFound, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND},
		{"channel conflict", llmrepo.ErrChannelConflict, http.StatusConflict, attunev1.ErrorCode_CONFLICT},
		{"no api key", llmsvc.ErrNoAPIKey, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION},
		{"validation", fmt.Errorf("%w: bad field", llmsvc.ErrValidation), http.StatusBadRequest, attunev1.ErrorCode_VALIDATION},
		{"generic", errors.New("boom"), http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := channelWriteError[*attunev1.LLMChannel](tt.err, "write failed")
			require.Error(t, err)
			var de *dispatcher.Error
			require.True(t, errors.As(err, &de))
			require.Equal(t, tt.wantStatus, de.Status)
			require.Equal(t, tt.wantCode, de.Code)
		})
	}
}

func TestChannelModelsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   attunev1.ErrorCode
	}{
		{"channel not found", llmrepo.ErrChannelNotFound, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND},
		{"validation", fmt.Errorf("%w: missing key", llmsvc.ErrValidation), http.StatusBadRequest, attunev1.ErrorCode_VALIDATION},
		{"no api key", llmsvc.ErrNoAPIKey, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION},
		{"generic", errors.New("upstream error"), http.StatusBadGateway, attunev1.ErrorCode_BAD_GATEWAY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := channelModelsError(tt.err)
			require.Error(t, err)
			var de *dispatcher.Error
			require.True(t, errors.As(err, &de))
			require.Equal(t, tt.wantStatus, de.Status)
			require.Equal(t, tt.wantCode, de.Code)
		})
	}
}

func TestChannelTestError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   attunev1.ErrorCode
	}{
		{"channel not found", llmrepo.ErrChannelNotFound, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND},
		{"validation", fmt.Errorf("%w: invalid", llmsvc.ErrValidation), http.StatusBadRequest, attunev1.ErrorCode_VALIDATION},
		{"no api key", llmsvc.ErrNoAPIKey, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION},
		{"generic", errors.New("provider timeout"), http.StatusBadGateway, attunev1.ErrorCode_BAD_GATEWAY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := channelTestError(tt.err)
			require.Error(t, err)
			var de *dispatcher.Error
			require.True(t, errors.As(err, &de))
			require.Equal(t, tt.wantStatus, de.Status)
			require.Equal(t, tt.wantCode, de.Code)
		})
	}
}

func TestAbilityWriteError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   attunev1.ErrorCode
	}{
		{"channel not found", llmrepo.ErrChannelNotFound, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND},
		{"validation", fmt.Errorf("%w: bad model", llmsvc.ErrValidation), http.StatusBadRequest, attunev1.ErrorCode_VALIDATION},
		{"generic", errors.New("db error"), http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := abilityWriteError[*attunev1.LLMChannelAbility](tt.err, "upsert failed")
			require.Error(t, err)
			var de *dispatcher.Error
			require.True(t, errors.As(err, &de))
			require.Equal(t, tt.wantStatus, de.Status)
			require.Equal(t, tt.wantCode, de.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// Snapshot functions
// ---------------------------------------------------------------------------

func TestChannelAuditSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("populated channel", func(t *testing.T) {
		t.Parallel()
		now := time.Now().UTC()
		tested := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
		chID := uuid.New()
		row := llmrepo.Channel{
			ID:                   chID,
			Name:                 "primary",
			Protocol:             llmrepo.ProtocolOpenAICompat,
			BaseURL:              "https://api.example.com",
			AuthMode:             llmrepo.AuthModeBearer,
			CredentialCiphertext: []byte("enc"),
			CredentialKeyID:      "key-1",
			Status:               llmrepo.ChannelStatusEnabled,
			Priority:             10,
			Weight:               20,
			TimeoutSeconds:       30,
			LastTestStatus:       "pass",
			LastTestedAt:         ptrext.Of(tested),
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		snap := channelAuditSnapshot(row)
		require.Equal(t, chID.String(), snap["id"])
		require.Equal(t, "primary", snap["name"])
		require.Equal(t, true, snap["has_api_key"])
		require.Equal(t, "key-1", snap["credential_key_id"])
		require.Equal(t, true, snap["last_tested_present"])
	})

	t.Run("zero-value channel", func(t *testing.T) {
		t.Parallel()
		snap := channelAuditSnapshot(llmrepo.Channel{})
		require.Equal(t, false, snap["has_api_key"])
		require.Equal(t, false, snap["last_tested_present"])
		require.Equal(t, uuid.Nil.String(), snap["id"])
	})
}

func TestAbilityAuditSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("populated ability", func(t *testing.T) {
		t.Parallel()
		abID := uuid.New()
		chID := uuid.New()
		row := llmrepo.Ability{
			ID:            abID,
			ChannelID:     chID,
			LogicalModel:  "gpt-4.1",
			ProviderModel: "gpt-4.1-turbo",
			Enabled:       true,
			Priority:      5,
			Weight:        10,
		}
		snap := abilityAuditSnapshot(row)
		require.Equal(t, abID.String(), snap["id"])
		require.Equal(t, chID.String(), snap["channel_id"])
		require.Equal(t, "gpt-4.1", snap["logical_model"])
		require.Equal(t, "gpt-4.1-turbo", snap["provider_model"])
		require.Equal(t, true, snap["enabled"])
		require.Equal(t, 5, snap["priority"])
		require.Equal(t, 10, snap["weight"])
	})

	t.Run("zero-value ability", func(t *testing.T) {
		t.Parallel()
		snap := abilityAuditSnapshot(llmrepo.Ability{})
		require.Equal(t, uuid.Nil.String(), snap["id"])
		require.Equal(t, false, snap["enabled"])
	})
}

func TestRouteAuditSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("populated route", func(t *testing.T) {
		t.Parallel()
		rtID := uuid.New()
		row := llmrepo.Route{
			ID:           rtID,
			TenantID:     "tenant-1",
			Purpose:      "enrich",
			LogicalModel: "gpt-4.1",
			Enabled:      true,
		}
		snap := routeAuditSnapshot(row)
		require.Equal(t, rtID.String(), snap["id"])
		require.Equal(t, "tenant-1", snap["tenant_id"])
		require.Equal(t, "enrich", snap["purpose"])
		require.Equal(t, "gpt-4.1", snap["logical_model"])
		require.Equal(t, true, snap["enabled"])
	})

	t.Run("zero-value route", func(t *testing.T) {
		t.Parallel()
		snap := routeAuditSnapshot(llmrepo.Route{})
		require.Equal(t, uuid.Nil.String(), snap["id"])
		require.Equal(t, false, snap["enabled"])
	})
}

// ---------------------------------------------------------------------------
// Handler method tests
// ---------------------------------------------------------------------------

func TestListChannels(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		now := time.Now().UTC()
		ch1 := llmrepo.Channel{ID: uuid.New(), Name: "ch1", CreatedAt: now, UpdatedAt: now}
		ch2 := llmrepo.Channel{ID: uuid.New(), Name: "ch2", CreatedAt: now, UpdatedAt: now}
		svc := ptrext.Of(configurableFakeService{channels: []llmrepo.Channel{ch1, ch2}})
		h := ptrext.Of(Handler{svc: svc})

		res, err := h.ListChannels(testCtx(), ptrext.Of(attunev1.ListLLMChannelsRequest{}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.Len(t, res.Body.Items, 2)
		require.Equal(t, ch1.ID.String(), res.Body.Items[0].Id)
		require.Equal(t, ch2.ID.String(), res.Body.Items[1].Id)
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{})
		h := ptrext.Of(Handler{svc: svc})

		res, err := h.ListChannels(testCtx(), ptrext.Of(attunev1.ListLLMChannelsRequest{}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.Empty(t, res.Body.Items)
	})

	t.Run("service error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{listChErr: errors.New("db down")})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.ListChannels(testCtx(), ptrext.Of(attunev1.ListLLMChannelsRequest{}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})
}

func TestGetChannel(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		ch := testChannel()
		svc := ptrext.Of(configurableFakeService{channel: ch})
		h := ptrext.Of(Handler{svc: svc})

		res, err := h.GetChannel(testCtx(), ptrext.Of(attunev1.GetLLMChannelRequest{Id: ch.ID.String()}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.Equal(t, ch.ID.String(), res.Body.Id)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{getChErr: llmrepo.ErrChannelNotFound})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.GetChannel(testCtx(), ptrext.Of(attunev1.GetLLMChannelRequest{Id: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusNotFound, de.Status)
	})

	t.Run("internal error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{getChErr: errors.New("db crash")})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.GetChannel(testCtx(), ptrext.Of(attunev1.GetLLMChannelRequest{Id: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.GetChannel(testCtx(), ptrext.Of(attunev1.GetLLMChannelRequest{Id: "bad"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})
}

func TestListChannelModels(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		models := []llmsvc.ProviderModel{
			{ID: "gpt-4.1", DisplayName: "GPT-4.1", OwnedBy: "openai"},
			{ID: "gpt-4.1-mini", DisplayName: "GPT-4.1 Mini", OwnedBy: "openai"},
		}
		svc := ptrext.Of(configurableFakeService{models: models})
		h := ptrext.Of(Handler{svc: svc})

		res, err := h.ListChannelModels(testCtx(), ptrext.Of(attunev1.ListLLMChannelModelsRequest{ChannelId: uuid.New().String()}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.Len(t, res.Body.Items, 2)
		require.Equal(t, "gpt-4.1", res.Body.Items[0].Id)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{listModelsErr: llmrepo.ErrChannelNotFound})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.ListChannelModels(testCtx(), ptrext.Of(attunev1.ListLLMChannelModelsRequest{ChannelId: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusNotFound, de.Status)
	})

	t.Run("validation error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{listModelsErr: fmt.Errorf("%w: no key", llmsvc.ErrValidation)})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.ListChannelModels(testCtx(), ptrext.Of(attunev1.ListLLMChannelModelsRequest{ChannelId: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("generic error (bad gateway)", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{listModelsErr: errors.New("provider unreachable")})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.ListChannelModels(testCtx(), ptrext.Of(attunev1.ListLLMChannelModelsRequest{ChannelId: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadGateway, de.Status)
	})

	t.Run("invalid channel id", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.ListChannelModels(testCtx(), ptrext.Of(attunev1.ListLLMChannelModelsRequest{ChannelId: "bad-id"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})
}

func TestListRoutes(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		now := time.Now().UTC()
		r1 := llmrepo.Route{ID: uuid.New(), TenantID: "t1", Purpose: "enrich", CreatedAt: now, UpdatedAt: now}
		r2 := llmrepo.Route{ID: uuid.New(), TenantID: "t2", Purpose: "triage", CreatedAt: now, UpdatedAt: now}
		svc := ptrext.Of(configurableFakeService{routes: []llmrepo.Route{r1, r2}})
		h := ptrext.Of(Handler{svc: svc})

		res, err := h.ListRoutes(testCtx(), ptrext.Of(attunev1.ListLLMRoutesRequest{}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.Len(t, res.Body.Items, 2)
	})

	t.Run("service error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{listRtErr: errors.New("db")})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.ListRoutes(testCtx(), ptrext.Of(attunev1.ListLLMRoutesRequest{}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})
}

func TestListAbilities(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		now := time.Now().UTC()
		chID := uuid.New()
		ab := llmrepo.Ability{ID: uuid.New(), ChannelID: chID, LogicalModel: "gpt-4.1", CreatedAt: now, UpdatedAt: now}
		svc := ptrext.Of(configurableFakeService{abilities: []llmrepo.Ability{ab}})
		h := ptrext.Of(Handler{svc: svc})

		res, err := h.ListAbilities(testCtx(), ptrext.Of(attunev1.ListLLMChannelAbilitiesRequest{ChannelId: chID.String()}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.Len(t, res.Body.Items, 1)
		require.Equal(t, ab.ID.String(), res.Body.Items[0].Id)
	})

	t.Run("channel not found", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{listAbErr: llmrepo.ErrChannelNotFound})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.ListAbilities(testCtx(), ptrext.Of(attunev1.ListLLMChannelAbilitiesRequest{ChannelId: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusNotFound, de.Status)
	})

	t.Run("internal error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{listAbErr: errors.New("db")})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.ListAbilities(testCtx(), ptrext.Of(attunev1.ListLLMChannelAbilitiesRequest{ChannelId: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})

	t.Run("invalid channel id", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.ListAbilities(testCtx(), ptrext.Of(attunev1.ListLLMChannelAbilitiesRequest{ChannelId: "bad"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})
}

func TestDeleteChannel(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		ch := testChannel()
		audit := ptrext.Of(fakeAuditRecorder{})
		svc := ptrext.Of(configurableFakeService{channel: ch})
		h := ptrext.Of(Handler{svc: svc, audit: audit})

		res, err := h.DeleteChannel(testCtx(), ptrext.Of(attunev1.DeleteLLMChannelRequest{Id: ch.ID.String()}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{deleteChErr: llmrepo.ErrChannelNotFound})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.DeleteChannel(testCtx(), ptrext.Of(attunev1.DeleteLLMChannelRequest{Id: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusNotFound, de.Status)
	})

	t.Run("internal error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{deleteChErr: errors.New("db")})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.DeleteChannel(testCtx(), ptrext.Of(attunev1.DeleteLLMChannelRequest{Id: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.DeleteChannel(testCtx(), ptrext.Of(attunev1.DeleteLLMChannelRequest{Id: "bad"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})
}

func TestDeleteAbility(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		chID := uuid.New()
		audit := ptrext.Of(fakeAuditRecorder{})
		svc := ptrext.Of(configurableFakeService{})
		h := ptrext.Of(Handler{svc: svc, audit: audit})

		res, err := h.DeleteAbility(testCtx(), ptrext.Of(attunev1.DeleteLLMChannelAbilityRequest{ChannelId: chID.String(), LogicalModel: "gpt-4.1"}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{deleteAbErr: llmrepo.ErrAbilityNotFound})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.DeleteAbility(testCtx(), ptrext.Of(attunev1.DeleteLLMChannelAbilityRequest{ChannelId: uuid.New().String(), LogicalModel: "m"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusNotFound, de.Status)
	})

	t.Run("validation error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{deleteAbErr: fmt.Errorf("%w: bad", llmsvc.ErrValidation)})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.DeleteAbility(testCtx(), ptrext.Of(attunev1.DeleteLLMChannelAbilityRequest{ChannelId: uuid.New().String(), LogicalModel: "m"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("internal error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{deleteAbErr: errors.New("db")})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.DeleteAbility(testCtx(), ptrext.Of(attunev1.DeleteLLMChannelAbilityRequest{ChannelId: uuid.New().String(), LogicalModel: "m"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})

	t.Run("invalid channel id", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.DeleteAbility(testCtx(), ptrext.Of(attunev1.DeleteLLMChannelAbilityRequest{ChannelId: "bad", LogicalModel: "m"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})
}

func TestUpsertRoute(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		now := time.Now().UTC()
		rt := llmrepo.Route{ID: uuid.New(), TenantID: "t1", Purpose: "enrich", LogicalModel: "gpt-4.1", Enabled: true, CreatedAt: now, UpdatedAt: now}
		audit := ptrext.Of(fakeAuditRecorder{})
		svc := ptrext.Of(configurableFakeService{route: rt})
		h := ptrext.Of(Handler{svc: svc, audit: audit})

		res, err := h.UpsertRoute(testCtx(), ptrext.Of(attunev1.UpsertLLMRouteRequest{TenantId: "t1", Purpose: "enrich", LogicalModel: "gpt-4.1"}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.Equal(t, rt.ID.String(), res.Body.Id)
	})

	t.Run("validation error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{upsertRtErr: fmt.Errorf("%w: missing purpose", llmsvc.ErrValidation)})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.UpsertRoute(testCtx(), ptrext.Of(attunev1.UpsertLLMRouteRequest{}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("internal error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{upsertRtErr: errors.New("db")})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.UpsertRoute(testCtx(), ptrext.Of(attunev1.UpsertLLMRouteRequest{}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})
}

func TestDeleteRoute(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		audit := ptrext.Of(fakeAuditRecorder{})
		svc := ptrext.Of(configurableFakeService{})
		h := ptrext.Of(Handler{svc: svc, audit: audit})

		res, err := h.DeleteRoute(testCtx(), ptrext.Of(attunev1.DeleteLLMRouteRequest{TenantId: "t1", Purpose: "enrich"}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{deleteRtErr: llmrepo.ErrRouteNotFound})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.DeleteRoute(testCtx(), ptrext.Of(attunev1.DeleteLLMRouteRequest{TenantId: "t1", Purpose: "x"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusNotFound, de.Status)
	})

	t.Run("validation error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{deleteRtErr: fmt.Errorf("%w: bad", llmsvc.ErrValidation)})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.DeleteRoute(testCtx(), ptrext.Of(attunev1.DeleteLLMRouteRequest{TenantId: "t1", Purpose: "x"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("internal error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{deleteRtErr: errors.New("db")})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.DeleteRoute(testCtx(), ptrext.Of(attunev1.DeleteLLMRouteRequest{TenantId: "t1", Purpose: "x"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})
}

func TestUpdateChannel(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		ch := testChannel()
		audit := ptrext.Of(fakeAuditRecorder{})
		svc := ptrext.Of(configurableFakeService{channel: ch})
		h := ptrext.Of(Handler{svc: svc, audit: audit})

		res, err := h.UpdateChannel(testCtx(), ptrext.Of(attunev1.UpdateLLMChannelRequest{
			Id:       ch.ID.String(),
			Name:     "updated",
			Protocol: llmrepo.ProtocolOpenAICompat,
			BaseUrl:  "https://new.example.com",
			AuthMode: llmrepo.AuthModeBearer,
			Status:   llmrepo.ChannelStatusEnabled,
		}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.Equal(t, ch.ID.String(), res.Body.Id)
		require.Len(t, audit.events, 1)
		require.Equal(t, "llm_channel.update", audit.events[0].Action)
	})

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.UpdateChannel(testCtx(), ptrext.Of(attunev1.UpdateLLMChannelRequest{Id: "bad"}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("get channel error before update", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{getChErr: errors.New("db")})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.UpdateChannel(testCtx(), ptrext.Of(attunev1.UpdateLLMChannelRequest{Id: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})

	t.Run("update service error", func(t *testing.T) {
		t.Parallel()
		svc := ptrext.Of(configurableFakeService{
			getChErr:    llmrepo.ErrChannelNotFound, // channel not found is tolerated for before-snapshot
			updateChErr: llmrepo.ErrChannelConflict,
		})
		h := ptrext.Of(Handler{svc: svc})

		_, err := h.UpdateChannel(testCtx(), ptrext.Of(attunev1.UpdateLLMChannelRequest{Id: uuid.New().String()}))
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusConflict, de.Status)
	})
}

func TestTestChannelSuccess(t *testing.T) {
	t.Parallel()

	ch := testChannel()
	audit := ptrext.Of(fakeAuditRecorder{})
	svc := ptrext.Of(configurableFakeService{channel: ch})
	h := ptrext.Of(Handler{svc: svc, audit: audit})

	res, err := h.TestChannel(testCtx(), ptrext.Of(attunev1.TestLLMChannelRequest{
		Id:            ch.ID.String(),
		ProviderModel: "gpt-4.1",
		Prompt:        "hello",
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.Status)
	require.True(t, res.Body.Ok)
	require.Equal(t, "gpt-4.1", res.Body.ProviderModel)
	require.Len(t, audit.events, 1)
	after := audit.events[0].After.(map[string]any)
	require.Equal(t, true, after["ok"])
	require.Equal(t, int32(10), after["input_tokens"])
}

func TestTestChannelInvalidID(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(configurableFakeService{})
	h := ptrext.Of(Handler{svc: svc})

	_, err := h.TestChannel(testCtx(), ptrext.Of(attunev1.TestLLMChannelRequest{Id: "bad"}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
}

// ---------------------------------------------------------------------------
// DeleteChannel with pre-load error edge case
// ---------------------------------------------------------------------------

func TestDeleteChannelPreLoadError(t *testing.T) {
	t.Parallel()

	// When GetChannel returns a non-ErrChannelNotFound error before Delete,
	// DeleteChannel returns internal error.
	svc := ptrext.Of(configurableFakeService{getChErr: errors.New("db timeout")})
	h := ptrext.Of(Handler{svc: svc})

	_, err := h.DeleteChannel(testCtx(), ptrext.Of(attunev1.DeleteLLMChannelRequest{Id: uuid.New().String()}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}
