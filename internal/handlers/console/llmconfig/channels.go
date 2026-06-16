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

func (h *Handler) ListChannels(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.ListLLMChannelsRequest,
) (dispatcher.Result[*attunev1.ListLLMChannelsResponse], error) {
	rows, err := h.svc.ListChannels(ctx)
	if err != nil {
		return dispatcher.Fail[*attunev1.ListLLMChannelsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list llm channels")
	}
	items := make([]*attunev1.LLMChannel, 0, len(rows))
	for _, row := range rows {
		items = append(items, toChannelProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListLLMChannelsResponse{Items: items}))
}

func (h *Handler) CreateChannel(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateLLMChannelRequest,
) (dispatcher.Result[*attunev1.LLMChannel], error) {
	row, err := h.svc.CreateChannel(ctx, llmsvc.ChannelInput{
		Name:           req.GetName(),
		Protocol:       req.GetProtocol(),
		BaseURL:        req.GetBaseUrl(),
		AuthMode:       req.GetAuthMode(),
		APIKey:         req.ApiKey,
		Status:         req.GetStatus(),
		Priority:       int(req.GetPriority()),
		Weight:         optionalInt32(req.Weight),
		TimeoutSeconds: optionalInt32(req.TimeoutSeconds),
	})
	if err != nil {
		return channelWriteError[*attunev1.LLMChannel](err, "failed to create llm channel")
	}
	if err := h.recordAudit(ctx, "llm_channel.create", "llm_channel", row.ID.String(),
		llmChannelSummary("Created LLM channel", ptrext.Indirect(row)), nil, channelAuditSnapshot(ptrext.Indirect(row))); err != nil {
		return dispatcher.Fail[*attunev1.LLMChannel](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(toChannelProto(ptrext.Indirect(row)))
}

func (h *Handler) GetChannel(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetLLMChannelRequest,
) (dispatcher.Result[*attunev1.LLMChannel], error) {
	id, err := parseID(req.GetId())
	if err != nil {
		return dispatcher.Result[*attunev1.LLMChannel]{}, err
	}
	row, err := h.svc.GetChannel(ctx, id)
	if err != nil {
		if errors.Is(err, llmrepo.ErrChannelNotFound) {
			return dispatcher.Fail[*attunev1.LLMChannel](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "llm channel not found")
		}
		return dispatcher.Fail[*attunev1.LLMChannel](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load llm channel")
	}
	return dispatcher.OK(toChannelProto(ptrext.Indirect(row)))
}

func (h *Handler) UpdateChannel(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateLLMChannelRequest,
) (dispatcher.Result[*attunev1.LLMChannel], error) {
	id, err := parseID(req.GetId())
	if err != nil {
		return dispatcher.Result[*attunev1.LLMChannel]{}, err
	}
	before, beforeErr := h.svc.GetChannel(ctx, id)
	if beforeErr != nil && !errors.Is(beforeErr, llmrepo.ErrChannelNotFound) {
		return dispatcher.Fail[*attunev1.LLMChannel](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load llm channel")
	}
	row, err := h.svc.UpdateChannel(ctx, id, llmsvc.ChannelInput{
		Name:           req.GetName(),
		Protocol:       req.GetProtocol(),
		BaseURL:        req.GetBaseUrl(),
		AuthMode:       req.GetAuthMode(),
		APIKey:         req.ApiKey,
		Status:         req.GetStatus(),
		Priority:       int(req.GetPriority()),
		Weight:         optionalInt32(req.Weight),
		TimeoutSeconds: optionalInt32(req.TimeoutSeconds),
	})
	if err != nil {
		return channelWriteError[*attunev1.LLMChannel](err, "failed to update llm channel")
	}
	var beforeSnapshot any
	if before != nil {
		beforeSnapshot = channelAuditSnapshot(ptrext.Indirect(before))
	}
	if err := h.recordAudit(ctx, "llm_channel.update", "llm_channel", row.ID.String(),
		llmChannelSummary("Updated LLM channel", ptrext.Indirect(row)), beforeSnapshot, channelAuditSnapshot(ptrext.Indirect(row))); err != nil {
		return dispatcher.Fail[*attunev1.LLMChannel](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(toChannelProto(ptrext.Indirect(row)))
}

func (h *Handler) TestChannel(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.TestLLMChannelRequest,
) (dispatcher.Result[*attunev1.TestLLMChannelResponse], error) {
	id, err := parseID(req.GetId())
	if err != nil {
		return dispatcher.Result[*attunev1.TestLLMChannelResponse]{}, err
	}
	row, err := h.svc.TestChannel(ctx, id, llmsvc.TestChannelInput{
		ProviderModel: req.GetProviderModel(),
		Prompt:        req.GetPrompt(),
	})
	if err != nil {
		if auditErr := h.recordAudit(ctx, "llm_channel.test", "llm_channel", id.String(), "Tested LLM channel", nil,
			channelTestAuditSnapshot(id.String(), req.GetProviderModel(), false, err)); auditErr != nil {
			return dispatcher.Fail[*attunev1.TestLLMChannelResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
		}
		return channelTestError(err)
	}
	after := channelTestAuditSnapshot(id.String(), row.ProviderModel, true, nil)
	after["input_tokens"] = row.InputTokens
	after["output_tokens"] = row.OutputTokens
	after["latency_ms"] = row.LatencyMS
	if err := h.recordAudit(ctx, "llm_channel.test", "llm_channel", id.String(), "Tested LLM channel", nil, after); err != nil {
		return dispatcher.Fail[*attunev1.TestLLMChannelResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(toChannelTestProto(ptrext.Indirect(row)))
}

func (h *Handler) ListChannelModels(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListLLMChannelModelsRequest,
) (dispatcher.Result[*attunev1.ListLLMChannelModelsResponse], error) {
	id, err := parseID(req.GetChannelId())
	if err != nil {
		return dispatcher.Result[*attunev1.ListLLMChannelModelsResponse]{}, err
	}
	rows, err := h.svc.ListChannelModels(ctx, id)
	if err != nil {
		return channelModelsError(err)
	}
	items := make([]*attunev1.LLMProviderModel, 0, len(rows))
	for _, row := range rows {
		items = append(items, toProviderModelProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListLLMChannelModelsResponse{Items: items}))
}

func (h *Handler) DeleteChannel(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DeleteLLMChannelRequest,
) (dispatcher.Result[*attunev1.DeleteLLMChannelResponse], error) {
	id, err := parseID(req.GetId())
	if err != nil {
		return dispatcher.Result[*attunev1.DeleteLLMChannelResponse]{}, err
	}
	before, beforeErr := h.svc.GetChannel(ctx, id)
	if beforeErr != nil && !errors.Is(beforeErr, llmrepo.ErrChannelNotFound) {
		return dispatcher.Fail[*attunev1.DeleteLLMChannelResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load llm channel")
	}
	if err := h.svc.DeleteChannel(ctx, id); err != nil {
		if errors.Is(err, llmrepo.ErrChannelNotFound) {
			return dispatcher.Fail[*attunev1.DeleteLLMChannelResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "llm channel not found")
		}
		return dispatcher.Fail[*attunev1.DeleteLLMChannelResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to delete llm channel")
	}
	var beforeSnapshot any
	summary := "Deleted LLM channel"
	if before != nil {
		beforeSnapshot = channelAuditSnapshot(ptrext.Indirect(before))
		summary = llmChannelSummary(summary, ptrext.Indirect(before))
	}
	if err := h.recordAudit(ctx, "llm_channel.delete", "llm_channel", id.String(), summary, beforeSnapshot, nil); err != nil {
		return dispatcher.Fail[*attunev1.DeleteLLMChannelResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(ptrext.Of(attunev1.DeleteLLMChannelResponse{}))
}

func channelModelsError(err error) (dispatcher.Result[*attunev1.ListLLMChannelModelsResponse], error) {
	if errors.Is(err, llmrepo.ErrChannelNotFound) {
		return dispatcher.Fail[*attunev1.ListLLMChannelModelsResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "llm channel not found")
	}
	if isValidation(err) {
		return dispatcher.Fail[*attunev1.ListLLMChannelModelsResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	return dispatcher.Fail[*attunev1.ListLLMChannelModelsResponse](
		http.StatusBadGateway, attunev1.ErrorCode_BAD_GATEWAY, "failed to list provider models")
}

func channelTestError(err error) (dispatcher.Result[*attunev1.TestLLMChannelResponse], error) {
	if errors.Is(err, llmrepo.ErrChannelNotFound) {
		return dispatcher.Fail[*attunev1.TestLLMChannelResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "llm channel not found")
	}
	if isValidation(err) {
		return dispatcher.Fail[*attunev1.TestLLMChannelResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	return dispatcher.Fail[*attunev1.TestLLMChannelResponse](
		http.StatusBadGateway, attunev1.ErrorCode_BAD_GATEWAY, "llm channel test failed")
}

func channelWriteError[Resp protoMessage](err error, message string) (dispatcher.Result[Resp], error) {
	if errors.Is(err, llmrepo.ErrChannelNotFound) {
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "llm channel not found")
	}
	if errors.Is(err, llmrepo.ErrChannelConflict) {
		return dispatcher.Fail[Resp](http.StatusConflict, attunev1.ErrorCode_CONFLICT, "llm channel name already exists")
	}
	if errors.Is(err, llmsvc.ErrNoAPIKey) {
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	if isValidation(err) {
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, message)
}
