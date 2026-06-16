// SPDX-License-Identifier: Apache-2.0

package llmconfig

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	llmsvc "github.com/Phixsura/attune/internal/service/llmconfig"
)

func (h *Handler) ListAbilities(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListLLMChannelAbilitiesRequest,
) (dispatcher.Result[*attunev1.ListLLMChannelAbilitiesResponse], error) {
	channelID, err := parseID(req.GetChannelId())
	if err != nil {
		return dispatcher.Result[*attunev1.ListLLMChannelAbilitiesResponse]{}, err
	}
	rows, err := h.svc.ListAbilities(ctx, channelID)
	if err != nil {
		if errors.Is(err, llmrepo.ErrChannelNotFound) {
			return dispatcher.Fail[*attunev1.ListLLMChannelAbilitiesResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "llm channel not found")
		}
		return dispatcher.Fail[*attunev1.ListLLMChannelAbilitiesResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list llm abilities")
	}
	items := make([]*attunev1.LLMChannelAbility, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAbilityProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListLLMChannelAbilitiesResponse{Items: items}))
}

func (h *Handler) UpsertAbility(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpsertLLMChannelAbilityRequest,
) (dispatcher.Result[*attunev1.LLMChannelAbility], error) {
	channelID, err := parseID(req.GetChannelId())
	if err != nil {
		return dispatcher.Result[*attunev1.LLMChannelAbility]{}, err
	}
	before := h.findAbility(ctx, channelID, req.GetLogicalModel())
	row, err := h.svc.UpsertAbility(ctx, channelID, llmsvc.AbilityInput{
		LogicalModel:  req.GetLogicalModel(),
		ProviderModel: req.GetProviderModel(),
		Enabled:       optionalBoolDefault(req.Enabled, true),
		Priority:      int(req.GetPriority()),
		Weight:        optionalInt32(req.Weight),
	})
	if err != nil {
		return abilityWriteError[*attunev1.LLMChannelAbility](err, "failed to upsert llm ability")
	}
	var beforeSnapshot any
	if before != nil {
		beforeValue := ptrext.Indirect(before)
		beforeSnapshot = abilityAuditSnapshot(beforeValue)
	}
	rowValue := ptrext.Indirect(row)
	if err := h.recordAudit(ctx, "llm_ability.upsert", "llm_ability",
		llmTargetID(rowValue.ID.String(), channelID.String(), rowValue.LogicalModel),
		llmAbilitySummary("Upserted LLM ability", rowValue), beforeSnapshot, abilityAuditSnapshot(rowValue)); err != nil {
		return dispatcher.Fail[*attunev1.LLMChannelAbility](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(toAbilityProto(rowValue))
}

func (h *Handler) DeleteAbility(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DeleteLLMChannelAbilityRequest,
) (dispatcher.Result[*attunev1.DeleteLLMChannelAbilityResponse], error) {
	channelID, err := parseID(req.GetChannelId())
	if err != nil {
		return dispatcher.Result[*attunev1.DeleteLLMChannelAbilityResponse]{}, err
	}
	before := h.findAbility(ctx, channelID, req.GetLogicalModel())
	if err := h.svc.DeleteAbility(ctx, channelID, req.GetLogicalModel()); err != nil {
		if errors.Is(err, llmrepo.ErrAbilityNotFound) {
			return dispatcher.Fail[*attunev1.DeleteLLMChannelAbilityResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "llm ability not found")
		}
		if isValidation(err) {
			return dispatcher.Fail[*attunev1.DeleteLLMChannelAbilityResponse](
				http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
		}
		return dispatcher.Fail[*attunev1.DeleteLLMChannelAbilityResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to delete llm ability")
	}
	var beforeSnapshot any
	summary := "Deleted LLM ability"
	if before != nil {
		beforeValue := ptrext.Indirect(before)
		beforeSnapshot = abilityAuditSnapshot(beforeValue)
		summary = llmAbilitySummary(summary, beforeValue)
	}
	if err := h.recordAudit(ctx, "llm_ability.delete", "llm_ability",
		llmTargetID("", channelID.String(), req.GetLogicalModel()), summary, beforeSnapshot, nil); err != nil {
		return dispatcher.Fail[*attunev1.DeleteLLMChannelAbilityResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(ptrext.Of(attunev1.DeleteLLMChannelAbilityResponse{}))
}

func abilityWriteError[Resp protoMessage](err error, message string) (dispatcher.Result[Resp], error) {
	if errors.Is(err, llmrepo.ErrChannelNotFound) {
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "llm channel not found")
	}
	if isValidation(err) {
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, message)
}

func optionalBoolDefault(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return ptrext.Indirect(v)
}

func optionalInt32(v *int32) *int {
	if v == nil {
		return nil
	}
	return ptrext.Of(int(ptrext.Indirect(v)))
}
