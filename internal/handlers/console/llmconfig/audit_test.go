package llmconfig

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	llmsvc "github.com/Phixsura/attune/internal/service/llmconfig"
)

type fakeAuditRecorder struct {
	events []auditlogsvc.Event
}

func (f *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestSetAuditLoggerStoresRecorder(t *testing.T) {
	t.Parallel()

	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{})

	h.SetAuditLogger(audit)

	require.Same(t, audit, h.audit)
}

func TestNewHandlerStoresService(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeService{})
	h := NewHandler(svc)

	require.NotNil(t, h)
	require.Same(t, svc, h.svc)
}

type fakeService struct {
	channel   llmrepo.Channel
	ability   llmrepo.Ability
	route     llmrepo.Route
	abilities []llmrepo.Ability
	routes    []llmrepo.Route
	testErr   error
}

func (f *fakeService) ListChannels(context.Context) ([]llmrepo.Channel, error) { return nil, nil }
func (f *fakeService) GetChannel(context.Context, uuid.UUID) (*llmrepo.Channel, error) {
	return ptrext.Of(f.channel), nil
}

func (f *fakeService) CreateChannel(context.Context, llmsvc.ChannelInput) (*llmrepo.Channel, error) {
	return ptrext.Of(f.channel), nil
}

func (f *fakeService) UpdateChannel(context.Context, uuid.UUID, llmsvc.ChannelInput) (*llmrepo.Channel, error) {
	return ptrext.Of(f.channel), nil
}

func (f *fakeService) TestChannel(context.Context, uuid.UUID, llmsvc.TestChannelInput) (*llmsvc.ChannelTestResult, error) {
	if f.testErr != nil {
		return nil, f.testErr
	}
	return ptrext.Of(llmsvc.ChannelTestResult{ProviderModel: "gpt-4.1", InputTokens: 10, OutputTokens: 5, LatencyMS: 25, Channel: f.channel}), nil
}

func (f *fakeService) ListChannelModels(context.Context, uuid.UUID) ([]llmsvc.ProviderModel, error) {
	return nil, nil
}

func (f *fakeService) DeleteChannel(context.Context, uuid.UUID) error {
	return nil
}

func (f *fakeService) ListAbilities(context.Context, uuid.UUID) ([]llmrepo.Ability, error) {
	return f.abilities, nil
}

func (f *fakeService) UpsertAbility(context.Context, uuid.UUID, llmsvc.AbilityInput) (*llmrepo.Ability, error) {
	return ptrext.Of(f.ability), nil
}

func (f *fakeService) DeleteAbility(context.Context, uuid.UUID, string) error {
	return nil
}

func (f *fakeService) ListRoutes(context.Context) ([]llmrepo.Route, error) {
	return f.routes, nil
}

func (f *fakeService) UpsertRoute(context.Context, llmsvc.RouteInput) (*llmrepo.Route, error) {
	return ptrext.Of(f.route), nil
}

func (f *fakeService) DeleteRoute(context.Context, string, string) error {
	return nil
}

func TestChannelCreateRecordsAudit(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	svc := ptrext.Of(fakeService{channel: llmrepo.Channel{
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
	}})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{svc: svc, audit: audit})

	_, err := h.CreateChannel(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.CreateLLMChannelRequest{Name: "primary", Protocol: llmrepo.ProtocolOpenAICompat, BaseUrl: "https://example.com", AuthMode: llmrepo.AuthModeBearer, Status: llmrepo.ChannelStatusEnabled}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "llm_channel.create", audit.events[0].Action)
}

func TestAbilityUpsertRecordsAudit(t *testing.T) {
	t.Parallel()

	channelID := uuid.New()
	abilityID := uuid.New()
	svc := ptrext.Of(fakeService{
		abilities: []llmrepo.Ability{{ID: abilityID, ChannelID: channelID, LogicalModel: "gpt-4.1", ProviderModel: "old-model"}},
		ability:   llmrepo.Ability{ID: abilityID, ChannelID: channelID, LogicalModel: "gpt-4.1", ProviderModel: "new-model", Enabled: true},
	})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{svc: svc, audit: audit})

	_, err := h.UpsertAbility(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.UpsertLLMChannelAbilityRequest{ChannelId: channelID.String(), LogicalModel: "gpt-4.1", ProviderModel: "new-model"}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "llm_ability.upsert", audit.events[0].Action)
	require.NotNil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}

func TestRouteDeleteRecordsAudit(t *testing.T) {
	t.Parallel()

	routeID := uuid.New()
	svc := ptrext.Of(fakeService{
		routes: []llmrepo.Route{{ID: routeID, TenantID: "tenant-1", Purpose: "triage", LogicalModel: "gpt-4.1", Enabled: true}},
	})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{svc: svc, audit: audit})

	_, err := h.DeleteRoute(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.DeleteLLMRouteRequest{TenantId: "tenant-1", Purpose: "triage"}))

	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "llm_route.delete", audit.events[0].Action)
	require.NotNil(t, audit.events[0].Before)
	require.Nil(t, audit.events[0].After)
}

func TestChannelTestFailureRecordsAudit(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	channelID := uuid.New()
	svc := ptrext.Of(fakeService{
		channel: llmrepo.Channel{
			ID:             channelID,
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
		},
		testErr: errors.New("upstream timeout"),
	})
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{svc: svc, audit: audit})

	_, err := h.TestChannel(ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	}), ptrext.Of(attunev1.TestLLMChannelRequest{
		Id:            channelID.String(),
		ProviderModel: "gpt-4.1",
		Prompt:        "ping",
	}))

	require.Error(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "llm_channel.test", audit.events[0].Action)
	after := audit.events[0].After.(map[string]any)
	require.Equal(t, false, after["ok"])
	require.Equal(t, "provider_failed", after["failure_reason"])
}

func TestLlmTargetID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       string
		fallback []string
		want     string
	}{
		{"id present", "abc-123", nil, "abc-123"},
		{"id with spaces trimmed", "  def-456  ", nil, "def-456"},
		{"empty id with one fallback", "", []string{"fallback"}, "fallback"},
		{"empty id with two fallbacks", "", []string{"first", "second"}, "first:second"},
		{"whitespace id uses fallback", "   ", []string{"fb"}, "fb"},
		{"empty id no fallback", "", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := llmTargetID(tt.id, tt.fallback...)
			if got != tt.want {
				t.Errorf("llmTargetID(%q, %v) = %q, want %q", tt.id, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestLlmChannelSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix string
		row    llmrepo.Channel
		want   string
	}{
		{"with name", "Created", llmrepo.Channel{Name: "OpenAI Primary"}, "Created (OpenAI Primary)"},
		{"empty name", "Created", llmrepo.Channel{Name: ""}, "Created"},
		{"prefix only", "Deleted", llmrepo.Channel{}, "Deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := llmChannelSummary(tt.prefix, tt.row)
			if got != tt.want {
				t.Errorf("llmChannelSummary(%q, %+v) = %q, want %q", tt.prefix, tt.row, got, tt.want)
			}
		})
	}
}

func TestLlmAbilitySummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix string
		row    llmrepo.Ability
		want   string
	}{
		{"with logical model", "Created", llmrepo.Ability{LogicalModel: "gpt-4"}, "Created (gpt-4)"},
		{"empty logical model", "Created", llmrepo.Ability{LogicalModel: ""}, "Created"},
		{"prefix only", "Deleted", llmrepo.Ability{}, "Deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := llmAbilitySummary(tt.prefix, tt.row)
			if got != tt.want {
				t.Errorf("llmAbilitySummary(%q, %+v) = %q, want %q", tt.prefix, tt.row, got, tt.want)
			}
		})
	}
}

func TestLlmRouteSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix string
		row    llmrepo.Route
		want   string
	}{
		{"with purpose", "Created", llmrepo.Route{Purpose: "enrich"}, "Created (enrich)"},
		{"empty purpose", "Created", llmrepo.Route{Purpose: ""}, "Created"},
		{"prefix only", "Deleted", llmrepo.Route{}, "Deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := llmRouteSummary(tt.prefix, tt.row)
			if got != tt.want {
				t.Errorf("llmRouteSummary(%q, %+v) = %q, want %q", tt.prefix, tt.row, got, tt.want)
			}
		})
	}
}

func TestChannelTestAuditSnapshot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		id               string
		providerModel    string
		ok               bool
		err              error
		wantFailure      string
		wantFailureExist bool
	}{
		{"success", "ch-1", "openai/gpt-4", true, nil, "", false},
		{"failure with error", "ch-2", "anthropic/claude", false, errors.New("connection refused"), "provider_failed", true},
		{"failure no error", "ch-3", "azure/gpt-4", false, nil, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := channelTestAuditSnapshot(tt.id, tt.providerModel, tt.ok, tt.err)
			if got["channel_id"] != tt.id {
				t.Errorf("channel_id = %v, want %v", got["channel_id"], tt.id)
			}
			if got["provider_model"] != tt.providerModel {
				t.Errorf("provider_model = %v, want %v", got["provider_model"], tt.providerModel)
			}
			if got["ok"] != tt.ok {
				t.Errorf("ok = %v, want %v", got["ok"], tt.ok)
			}
			_, exists := got["failure_reason"]
			if exists != tt.wantFailureExist {
				t.Errorf("failure_reason exists = %v, want %v", exists, tt.wantFailureExist)
			}
			if tt.wantFailureExist && got["failure_reason"] != tt.wantFailure {
				t.Errorf("failure_reason = %v, want %v", got["failure_reason"], tt.wantFailure)
			}
		})
	}
}
