// SPDX-License-Identifier: Apache-2.0

package llmconfig

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	llmsvc "github.com/Phixsura/attune/internal/service/llmconfig"
)

type service interface {
	ListChannels(ctx context.Context) ([]llmrepo.Channel, error)
	GetChannel(ctx context.Context, id uuid.UUID) (*llmrepo.Channel, error)
	CreateChannel(ctx context.Context, in llmsvc.ChannelInput) (*llmrepo.Channel, error)
	UpdateChannel(ctx context.Context, id uuid.UUID, in llmsvc.ChannelInput) (*llmrepo.Channel, error)
	TestChannel(ctx context.Context, id uuid.UUID, in llmsvc.TestChannelInput) (*llmsvc.ChannelTestResult, error)
	ListChannelModels(ctx context.Context, id uuid.UUID) ([]llmsvc.ProviderModel, error)
	DeleteChannel(ctx context.Context, id uuid.UUID) error
	ListAbilities(ctx context.Context, channelID uuid.UUID) ([]llmrepo.Ability, error)
	UpsertAbility(ctx context.Context, channelID uuid.UUID, in llmsvc.AbilityInput) (*llmrepo.Ability, error)
	DeleteAbility(ctx context.Context, channelID uuid.UUID, logicalModel string) error
	ListRoutes(ctx context.Context) ([]llmrepo.Route, error)
	UpsertRoute(ctx context.Context, in llmsvc.RouteInput) (*llmrepo.Route, error)
	DeleteRoute(ctx context.Context, tenantID, purpose string) error
}

type Handler struct {
	svc service
}

type protoMessage interface {
	proto.Message
}

func NewHandler(svc service) *Handler {
	return ptrext.Of(Handler{svc: svc})
}

func toChannelProto(row llmrepo.Channel) *attunev1.LLMChannel {
	out := ptrext.Of(attunev1.LLMChannel{
		Id:              row.ID.String(),
		Name:            row.Name,
		Protocol:        row.Protocol,
		BaseUrl:         row.BaseURL,
		AuthMode:        row.AuthMode,
		HasApiKey:       len(row.CredentialCiphertext) > 0,
		CredentialKeyId: row.CredentialKeyID,
		Status:          row.Status,
		Priority:        int32(row.Priority),
		Weight:          int32(row.Weight),
		TimeoutSeconds:  int32(row.TimeoutSeconds),
		CreatedAt:       row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.UTC().Format(time.RFC3339),
		LastTestStatus:  row.LastTestStatus,
		LastError:       row.LastError,
	})
	if row.LastTestedAt != nil {
		out.LastTestedAt = ptrext.Of(row.LastTestedAt.UTC().Format(time.RFC3339))
	}
	return out
}

func toChannelTestProto(row llmsvc.ChannelTestResult) *attunev1.TestLLMChannelResponse {
	return ptrext.Of(attunev1.TestLLMChannelResponse{
		Ok:            true,
		ProviderModel: row.ProviderModel,
		Text:          row.Text,
		InputTokens:   row.InputTokens,
		OutputTokens:  row.OutputTokens,
		LatencyMs:     row.LatencyMS,
		Channel:       toChannelProto(row.Channel),
	})
}

func toProviderModelProto(row llmsvc.ProviderModel) *attunev1.LLMProviderModel {
	return ptrext.Of(attunev1.LLMProviderModel{
		Id:          row.ID,
		DisplayName: row.DisplayName,
		OwnedBy:     row.OwnedBy,
	})
}

func toAbilityProto(row llmrepo.Ability) *attunev1.LLMChannelAbility {
	return ptrext.Of(attunev1.LLMChannelAbility{
		Id:            row.ID.String(),
		ChannelId:     row.ChannelID.String(),
		LogicalModel:  row.LogicalModel,
		ProviderModel: row.ProviderModel,
		Enabled:       row.Enabled,
		Priority:      int32(row.Priority),
		Weight:        int32(row.Weight),
		CreatedAt:     row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     row.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func toRouteProto(row llmrepo.Route) *attunev1.LLMRoute {
	return ptrext.Of(attunev1.LLMRoute{
		Id:           row.ID.String(),
		TenantId:     row.TenantID,
		Purpose:      row.Purpose,
		LogicalModel: row.LogicalModel,
		Enabled:      row.Enabled,
		CreatedAt:    row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    row.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func parseID(id string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id must be a UUID")
	}
	return parsed, nil
}

func isValidation(err error) bool {
	return errors.Is(err, llmsvc.ErrValidation) || errors.Is(err, llmsvc.ErrNoAPIKey)
}
