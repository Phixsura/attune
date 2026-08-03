// SPDX-License-Identifier: Apache-2.0

// Package survey serves console endpoints for post-resolution CSAT and CES
// surveys.
package survey

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/survey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/survey"
)

type Handler struct {
	service service
	audit   auditRecorder
}

type service interface {
	ListCampaigns(ctx context.Context, tenantID string, status string, limit int) ([]repo.Campaign, error)
	CreateCampaign(ctx context.Context, in svc.CampaignInput) (repo.Campaign, error)
	UpdateCampaign(ctx context.Context, in svc.CampaignInput) (repo.Campaign, error)
	ArchiveCampaign(ctx context.Context, tenantID string, id uuid.UUID, actorID string) (repo.Campaign, error)
	CreateHostedLink(ctx context.Context, in svc.HostedLinkInput) (repo.Invitation, error)
	PreviewRecipients(ctx context.Context, in svc.RecipientPreviewInput) (svc.RecipientPreviewResult, error)
	SendTestEmail(ctx context.Context, in svc.TestEmailInput) (svc.TestEmailResult, error)
	CampaignHealth(ctx context.Context, tenantID string, campaignID uuid.UUID) (svc.CampaignHealth, error)
	RetryInvitationDelivery(ctx context.Context, tenantID string, id uuid.UUID, actorID string) (repo.Invitation, error)
	RecordProviderEvent(ctx context.Context, in svc.ProviderEventInput) (repo.Invitation, error)
	ListInvitations(ctx context.Context, filter repo.InvitationFilter) ([]repo.Invitation, error)
	ListResponses(ctx context.Context, filter repo.ResponseFilter) ([]repo.Response, error)
	Analytics(ctx context.Context, filter repo.AnalyticsFilter) (repo.Analytics, error)
	AnalyticsTrend(ctx context.Context, filter repo.AnalyticsFilter) ([]repo.AnalyticsTrendBucket, error)
	AnalyticsSegments(ctx context.Context, filter repo.AnalyticsSegmentFilter) ([]repo.AnalyticsSegment, error)
	AnalyticsInsights(ctx context.Context, filter svc.AnalyticsInsightFilter) ([]svc.AnalyticsInsight, error)
	UpdateLowScoreReview(ctx context.Context, in svc.ReviewInput) (repo.LowScoreReview, error)
	BatchUpdateLowScoreReviews(ctx context.Context, in svc.BatchReviewInput) ([]repo.LowScoreReview, error)
	AssignLowScoreReviews(ctx context.Context, in svc.AssignmentInput) (svc.AssignmentResult, error)
	EscalateLowScoreReviews(ctx context.Context, in svc.EscalationInput) (svc.EscalationResult, error)
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

func NewHandler(service service) *Handler {
	return ptrext.Of(Handler{service: service})
}

func (h *Handler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

func BindListSurveyCampaigns(r *http.Request, req *attunev1.ListSurveyCampaignsRequest) error {
	q := r.URL.Query()
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		status, ok := campaignStatusFromQuery(raw)
		if !ok {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey campaign status")
		}
		req.Status = status
	}
	return bindLimit(q.Get("limit"), func(limit int32) { req.Limit = limit })
}

func BindListSurveyInvitations(r *http.Request, req *attunev1.ListSurveyInvitationsRequest) error {
	q := r.URL.Query()
	if raw := strings.TrimSpace(q.Get("campaign_id")); raw != "" {
		req.CampaignId = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("response_status")); raw != "" {
		status, ok := responseStatusFromQuery(raw)
		if !ok {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey response status")
		}
		req.ResponseStatus = status
	}
	if raw := strings.TrimSpace(q.Get("suppression_status")); raw != "" {
		status, ok := suppressionStatusFromQuery(raw)
		if !ok {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey suppression status")
		}
		req.SuppressionStatus = status
	}
	return bindLimit(q.Get("limit"), func(limit int32) { req.Limit = limit })
}

func BindListSurveyResponses(r *http.Request, req *attunev1.ListSurveyResponsesRequest) error {
	q := r.URL.Query()
	if raw := strings.TrimSpace(q.Get("campaign_id")); raw != "" {
		req.CampaignId = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("low_score_only")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid low_score_only")
		}
		req.LowScoreOnly = ptrext.Of(value)
	}
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		req.From = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		req.To = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("recovery_sla_status")); raw != "" {
		status, ok := recoverySLAStatusFromQuery(raw)
		if !ok {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey recovery SLA status")
		}
		req.RecoverySlaStatus = ptrext.Of(status)
	}
	if raw := strings.TrimSpace(q.Get("recovery_blocker_reason")); raw != "" {
		blocker, ok := recoveryBlockerReasonFromQuery(raw)
		if !ok {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey recovery blocker reason")
		}
		req.RecoveryBlockerReason = ptrext.Of(blocker)
	}
	if raw := strings.TrimSpace(q.Get("review_severity")); raw != "" {
		severity, ok := severityFromQuery(raw)
		if !ok {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey low-score review severity")
		}
		req.ReviewSeverity = ptrext.Of(severity)
	}
	if raw := strings.TrimSpace(q.Get("owner_member_id")); raw != "" {
		req.OwnerMemberId = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("account_key")); raw != "" {
		req.AccountKey = ptrext.Of(raw)
	}
	return bindLimit(q.Get("limit"), func(limit int32) { req.Limit = limit })
}

func BindSurveyAnalytics(r *http.Request, req *attunev1.GetSurveyAnalyticsRequest) error {
	q := r.URL.Query()
	if raw := strings.TrimSpace(q.Get("campaign_id")); raw != "" {
		req.CampaignId = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		req.From = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		req.To = ptrext.Of(raw)
	}
	return nil
}

func BindSurveyAnalyticsTrend(r *http.Request, req *attunev1.GetSurveyAnalyticsTrendRequest) error {
	q := r.URL.Query()
	if raw := strings.TrimSpace(q.Get("campaign_id")); raw != "" {
		req.CampaignId = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		req.From = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		req.To = ptrext.Of(raw)
	}
	return nil
}

func BindSurveyAnalyticsSegments(r *http.Request, req *attunev1.GetSurveyAnalyticsSegmentsRequest) error {
	q := r.URL.Query()
	if raw := strings.TrimSpace(q.Get("campaign_id")); raw != "" {
		req.CampaignId = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		req.From = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		req.To = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("dimension")); raw != "" {
		dimension, ok := analyticsSegmentDimensionFromQuery(raw)
		if !ok {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey analytics segment dimension")
		}
		req.Dimension = dimension
	}
	return bindLimit(q.Get("limit"), func(limit int32) { req.Limit = limit })
}

func BindSurveyAnalyticsInsights(r *http.Request, req *attunev1.GetSurveyAnalyticsInsightsRequest) error {
	q := r.URL.Query()
	if raw := strings.TrimSpace(q.Get("campaign_id")); raw != "" {
		req.CampaignId = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		req.From = ptrext.Of(raw)
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		req.To = ptrext.Of(raw)
	}
	return bindLimit(q.Get("limit"), func(limit int32) { req.Limit = limit })
}

func (h *Handler) ListCampaigns(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListSurveyCampaignsRequest,
) (dispatcher.Result[*attunev1.ListSurveyCampaignsResponse], error) {
	items, err := h.service.ListCampaigns(ctx, ctx.Auth.TenantID, campaignStatusToRepo(req.GetStatus()), int(req.GetLimit()))
	if err != nil {
		return consoleError[*attunev1.ListSurveyCampaignsResponse](err, "survey campaigns failed")
	}
	out := ptrext.Of(attunev1.ListSurveyCampaignsResponse{
		Campaigns: make([]*attunev1.SurveyCampaign, 0, len(items)),
	})
	for _, item := range items {
		out.Campaigns = append(out.Campaigns, campaignToProto(item))
	}
	return dispatcher.OK(out)
}

func (h *Handler) CreateCampaign(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateSurveyCampaignRequest,
) (dispatcher.Result[*attunev1.SurveyCampaign], error) {
	item, err := h.service.CreateCampaign(ctx, svc.CampaignInput{
		TenantID:                      ctx.Auth.TenantID,
		Name:                          ptrext.Of(req.GetName()),
		SurveyType:                    surveyTypeToRepo(req.GetSurveyType()),
		Status:                        campaignStatusToRepo(req.GetStatus()),
		TriggerEvent:                  triggerEventToRepo(req.GetTriggerEvent()),
		DistributionMode:              distributionModeToRepo(req.GetDistributionMode()),
		DedupePolicy:                  dedupePolicyToRepo(req.GetDedupePolicy()),
		TriggerFilter:                 structMap(req.GetTriggerFilter()),
		Content:                       structMap(req.GetContent()),
		Locale:                        ptrext.Of(req.GetLocale()),
		SamplingPercent:               req.SamplingPercent,
		MinDaysBetweenContact:         int32PtrToInt(req.MinDaysBetweenContact),
		ExpiresAfterDays:              int32PtrToInt(req.ExpiresAfterDays),
		MaxDailyInvitations:           int32PtrToInt(req.MaxDailyInvitations),
		LowScoreThreshold:             int32PtrToInt(req.LowScoreThreshold),
		RequireRecentCustomerActivity: req.RequireRecentCustomerActivity,
		RecentActivityDays:            int32PtrToInt(req.RecentActivityDays),
		SuppressAutoResolved:          req.SuppressAutoResolved,
		ActorID:                       ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.SurveyCampaign](err, "survey campaign failed")
	}
	_ = h.record(ctx, "survey.campaign_create", "survey_campaign", item.ID.String(), "Created survey campaign")
	return dispatcher.Created(campaignToProto(item))
}

func (h *Handler) UpdateCampaign(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateSurveyCampaignRequest,
) (dispatcher.Result[*attunev1.SurveyCampaign], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyCampaign](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey campaign id")
	}
	item, err := h.service.UpdateCampaign(ctx, svc.CampaignInput{
		TenantID:                      ctx.Auth.TenantID,
		ID:                            id,
		Name:                          req.Name,
		Status:                        campaignStatusToRepo(req.GetStatus()),
		TriggerEvent:                  triggerEventToRepo(req.GetTriggerEvent()),
		DistributionMode:              distributionModeToRepo(req.GetDistributionMode()),
		DedupePolicy:                  dedupePolicyToRepo(req.GetDedupePolicy()),
		TriggerFilter:                 structMap(req.GetTriggerFilter()),
		TriggerFilterSet:              req.GetTriggerFilter() != nil,
		Content:                       structMap(req.GetContent()),
		ContentSet:                    req.GetContent() != nil,
		Locale:                        req.Locale,
		SamplingPercent:               req.SamplingPercent,
		MinDaysBetweenContact:         int32PtrToInt(req.MinDaysBetweenContact),
		ExpiresAfterDays:              int32PtrToInt(req.ExpiresAfterDays),
		MaxDailyInvitations:           int32PtrToInt(req.MaxDailyInvitations),
		LowScoreThreshold:             int32PtrToInt(req.LowScoreThreshold),
		RequireRecentCustomerActivity: req.RequireRecentCustomerActivity,
		RecentActivityDays:            int32PtrToInt(req.RecentActivityDays),
		SuppressAutoResolved:          req.SuppressAutoResolved,
		ActorID:                       ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.SurveyCampaign](err, "survey campaign failed")
	}
	_ = h.record(ctx, "survey.campaign_update", "survey_campaign", item.ID.String(), "Updated survey campaign")
	return dispatcher.OK(campaignToProto(item))
}

func (h *Handler) ArchiveCampaign(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ArchiveSurveyCampaignRequest,
) (dispatcher.Result[*attunev1.SurveyCampaign], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyCampaign](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey campaign id")
	}
	item, err := h.service.ArchiveCampaign(ctx, ctx.Auth.TenantID, id, ctx.Auth.UserID)
	if err != nil {
		return consoleError[*attunev1.SurveyCampaign](err, "survey campaign failed")
	}
	_ = h.record(ctx, "survey.campaign_archive", "survey_campaign", item.ID.String(), "Archived survey campaign")
	return dispatcher.OK(campaignToProto(item))
}

func (h *Handler) CreateHostedLink(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateSurveyHostedLinkRequest,
) (dispatcher.Result[*attunev1.SurveyInvitation], error) {
	campaignID, err := parseUUID(req.GetCampaignId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyInvitation](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey campaign id")
	}
	requestID, err := optionalUUID(req.GetRequestId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyInvitation](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid request id")
	}
	item, err := h.service.CreateHostedLink(ctx, svc.HostedLinkInput{
		TenantID:   ctx.Auth.TenantID,
		CampaignID: campaignID,
		SourceType: req.GetSourceType(),
		SourceID:   req.GetSourceId(),
		RequestID:  requestID,
		Context:    structMap(req.GetContext()),
		ActorID:    ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.SurveyInvitation](err, "survey hosted link failed")
	}
	_ = h.record(ctx, "survey.hosted_link_create", "survey_invitation", item.ID.String(), "Created survey hosted link")
	return dispatcher.Created(invitationToProto(item))
}

func (h *Handler) PreviewRecipients(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.PreviewSurveyRecipientsRequest,
) (dispatcher.Result[*attunev1.PreviewSurveyRecipientsResponse], error) {
	campaignID, err := parseUUID(req.GetCampaignId())
	if err != nil {
		return dispatcher.Fail[*attunev1.PreviewSurveyRecipientsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey campaign id")
	}
	requestID, err := optionalUUID(req.GetRequestId())
	if err != nil {
		return dispatcher.Fail[*attunev1.PreviewSurveyRecipientsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid request id")
	}
	item, err := h.service.PreviewRecipients(ctx, svc.RecipientPreviewInput{
		TenantID:   ctx.Auth.TenantID,
		CampaignID: campaignID,
		SourceType: req.GetSourceType(),
		SourceID:   req.GetSourceId(),
		RequestID:  requestID,
		Context:    structMap(req.GetContext()),
		Limit:      int(req.GetLimit()),
	})
	if err != nil {
		return consoleError[*attunev1.PreviewSurveyRecipientsResponse](err, "survey recipient preview failed")
	}
	return dispatcher.OK(recipientPreviewResultToProto(item))
}

func (h *Handler) SendTestEmail(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.SendSurveyTestEmailRequest,
) (dispatcher.Result[*attunev1.SendSurveyTestEmailResponse], error) {
	campaignID, err := parseUUID(req.GetCampaignId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SendSurveyTestEmailResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey campaign id")
	}
	result, err := h.service.SendTestEmail(ctx, svc.TestEmailInput{
		TenantID:   ctx.Auth.TenantID,
		CampaignID: campaignID,
		ToEmail:    req.GetToEmail(),
		ActorID:    ctx.Auth.UserID,
	})
	if err != nil {
		_ = h.recordAfter(ctx, "survey.test_email_send", "survey_campaign", campaignID.String(), "Survey test email failed", surveyTestEmailFailureAudit(err))
		return consoleError[*attunev1.SendSurveyTestEmailResponse](err, "survey test email failed")
	}
	_ = h.recordAfter(ctx, "survey.test_email_send", "survey_campaign", campaignID.String(), "Sent survey test email", surveyTestEmailSuccessAudit(result))
	return dispatcher.OK(ptrext.Of(attunev1.SendSurveyTestEmailResponse{
		Ok:       result.OK,
		Provider: result.Provider,
		SentAt:   timeString(result.SentAt),
	}))
}

func (h *Handler) CampaignHealth(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetSurveyCampaignHealthRequest,
) (dispatcher.Result[*attunev1.SurveyCampaignHealth], error) {
	campaignID, err := parseUUID(req.GetCampaignId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyCampaignHealth](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey campaign id")
	}
	item, err := h.service.CampaignHealth(ctx, ctx.Auth.TenantID, campaignID)
	if err != nil {
		return consoleError[*attunev1.SurveyCampaignHealth](err, "survey campaign health failed")
	}
	return dispatcher.OK(campaignHealthToProto(item))
}

func surveyTestEmailSuccessAudit(result svc.TestEmailResult) map[string]any {
	return map[string]any{
		"ok":                   result.OK,
		"provider":             result.Provider,
		"sent_at":              timeString(result.SentAt),
		"test_only":            true,
		"invitation_persisted": false,
	}
}

func surveyTestEmailFailureAudit(err error) map[string]any {
	return map[string]any{
		"ok":                   false,
		"error_code":           surveyAuditErrorCode(err),
		"test_only":            true,
		"invitation_persisted": false,
	}
}

func surveyAuditErrorCode(err error) string {
	switch {
	case errors.Is(err, svc.ErrValidation):
		return "validation"
	case errors.Is(err, svc.ErrNotFound):
		return "not_found"
	case errors.Is(err, svc.ErrConflict):
		return "conflict"
	case errors.Is(err, svc.ErrDisabled):
		return "disabled"
	default:
		return "internal"
	}
}

func (h *Handler) RecordProviderEvent(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RecordSurveyProviderEventRequest,
) (dispatcher.Result[*attunev1.SurveyInvitation], error) {
	invitationID, err := optionalUUID(req.GetInvitationId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyInvitation](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey invitation id")
	}
	occurredAt, err := optionalTime(req.GetOccurredAt())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyInvitation](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid occurred_at")
	}
	var occurred time.Time
	if occurredAt != nil {
		occurred = ptrext.Indirect(occurredAt)
	}
	item, err := h.service.RecordProviderEvent(ctx, svc.ProviderEventInput{
		TenantID:          ctx.Auth.TenantID,
		InvitationID:      invitationID,
		Provider:          req.GetProvider(),
		ProviderEventType: req.GetProviderEventType(),
		ProviderMessageID: req.GetProviderMessageId(),
		ProviderEventKey:  req.GetProviderEventKey(),
		Payload:           structMap(req.GetPayload()),
		OccurredAt:        occurred,
	})
	if err != nil {
		return consoleError[*attunev1.SurveyInvitation](err, "survey provider event failed")
	}
	_ = h.recordAfter(ctx, "survey.provider_event_record", "survey_invitation", item.ID.String(), "Recorded survey provider event", surveyProviderEventAudit(item, req))
	return dispatcher.OK(invitationToProto(item))
}

func surveyProviderEventAudit(item repo.Invitation, req *attunev1.RecordSurveyProviderEventRequest) map[string]any {
	eventType := surveyProviderEventTypeAudit(req.GetProviderEventType())
	return map[string]any{
		"ok":                          true,
		"provider":                    strings.TrimSpace(req.GetProvider()),
		"provider_event_type":         eventType,
		"delivery_status":             item.DeliveryStatus,
		"response_status":             item.ResponseStatus,
		"suppression_status":          item.SuppressionStatus,
		"suppression_reason":          item.SuppressionReason,
		"contact_suppressed":          providerEventSuppressesContactAudit(eventType) && item.ContactID != nil,
		"payload_present":             req.GetPayload() != nil,
		"provider_message_id_present": strings.TrimSpace(req.GetProviderMessageId()) != "",
		"provider_event_key_present":  strings.TrimSpace(req.GetProviderEventKey()) != "",
	}
}

func surveyProviderEventTypeAudit(raw string) string {
	value := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "-", "_"))
	switch value {
	case repo.ProviderEventAccepted, "accept":
		return repo.ProviderEventAccepted
	case repo.ProviderEventDelivered, "delivery":
		return repo.ProviderEventDelivered
	case repo.ProviderEventBounced, "bounce":
		return repo.ProviderEventBounced
	case repo.ProviderEventComplained, "complaint":
		return repo.ProviderEventComplained
	case repo.ProviderEventRejected, "reject":
		return repo.ProviderEventRejected
	case repo.ProviderEventTemporarilyDelayed, "temporary_delayed", "delayed", "deferred":
		return repo.ProviderEventTemporarilyDelayed
	case repo.ProviderEventOpened, "open":
		return repo.ProviderEventOpened
	default:
		return ""
	}
}

func providerEventSuppressesContactAudit(eventType string) bool {
	return eventType == repo.ProviderEventBounced || eventType == repo.ProviderEventComplained
}

func (h *Handler) RetryInvitationDelivery(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RetrySurveyInvitationDeliveryRequest,
) (dispatcher.Result[*attunev1.SurveyInvitation], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyInvitation](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid survey invitation id")
	}
	item, err := h.service.RetryInvitationDelivery(ctx, ctx.Auth.TenantID, id, ctx.Auth.UserID)
	if err != nil {
		return consoleError[*attunev1.SurveyInvitation](err, "survey invitation delivery retry failed")
	}
	_ = h.record(ctx, "survey.invitation_delivery_retry", "survey_invitation", item.ID.String(), "Retried survey invitation delivery")
	return dispatcher.OK(invitationToProto(item))
}

func (h *Handler) ListInvitations(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListSurveyInvitationsRequest,
) (dispatcher.Result[*attunev1.ListSurveyInvitationsResponse], error) {
	campaignID, err := optionalUUID(req.GetCampaignId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListSurveyInvitationsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey campaign id")
	}
	items, err := h.service.ListInvitations(ctx, repo.InvitationFilter{
		TenantID:          ctx.Auth.TenantID,
		CampaignID:        campaignID,
		ResponseStatus:    responseStatusToRepo(req.GetResponseStatus()),
		SuppressionStatus: suppressionStatusToRepo(req.GetSuppressionStatus()),
		Limit:             int(req.GetLimit()),
	})
	if err != nil {
		return consoleError[*attunev1.ListSurveyInvitationsResponse](err, "survey invitations failed")
	}
	out := ptrext.Of(attunev1.ListSurveyInvitationsResponse{
		Invitations: make([]*attunev1.SurveyInvitation, 0, len(items)),
	})
	for _, item := range items {
		out.Invitations = append(out.Invitations, invitationToProto(item))
	}
	return dispatcher.OK(out)
}

func (h *Handler) ListResponses(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListSurveyResponsesRequest,
) (dispatcher.Result[*attunev1.ListSurveyResponsesResponse], error) {
	filter, err := responseFilter(ctx.Auth.TenantID, req)
	if err != nil {
		return dispatcher.Fail[*attunev1.ListSurveyResponsesResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey response filter")
	}
	items, err := h.service.ListResponses(ctx, filter)
	if err != nil {
		return consoleError[*attunev1.ListSurveyResponsesResponse](err, "survey responses failed")
	}
	out := ptrext.Of(attunev1.ListSurveyResponsesResponse{
		Responses: make([]*attunev1.SurveyResponse, 0, len(items)),
	})
	for _, item := range items {
		out.Responses = append(out.Responses, responseToProto(item))
	}
	return dispatcher.OK(out)
}

func (h *Handler) Analytics(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetSurveyAnalyticsRequest,
) (dispatcher.Result[*attunev1.SurveyAnalytics], error) {
	filter, err := analyticsFilter(ctx.Auth.TenantID, req)
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyAnalytics](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey analytics filter")
	}
	item, err := h.service.Analytics(ctx, filter)
	if err != nil {
		return consoleError[*attunev1.SurveyAnalytics](err, "survey analytics failed")
	}
	return dispatcher.OK(analyticsToProto(item))
}

func (h *Handler) AnalyticsTrend(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetSurveyAnalyticsTrendRequest,
) (dispatcher.Result[*attunev1.GetSurveyAnalyticsTrendResponse], error) {
	filter, err := analyticsTrendFilter(ctx.Auth.TenantID, req)
	if err != nil {
		return dispatcher.Fail[*attunev1.GetSurveyAnalyticsTrendResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey analytics trend filter")
	}
	items, err := h.service.AnalyticsTrend(ctx, filter)
	if err != nil {
		return consoleError[*attunev1.GetSurveyAnalyticsTrendResponse](err, "survey analytics trend failed")
	}
	return dispatcher.OK(analyticsTrendToProto(items))
}

func (h *Handler) AnalyticsSegments(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetSurveyAnalyticsSegmentsRequest,
) (dispatcher.Result[*attunev1.GetSurveyAnalyticsSegmentsResponse], error) {
	filter, err := analyticsSegmentFilter(ctx.Auth.TenantID, req)
	if err != nil {
		return dispatcher.Fail[*attunev1.GetSurveyAnalyticsSegmentsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey analytics segment filter")
	}
	items, err := h.service.AnalyticsSegments(ctx, filter)
	if err != nil {
		return consoleError[*attunev1.GetSurveyAnalyticsSegmentsResponse](err, "survey analytics segments failed")
	}
	return dispatcher.OK(analyticsSegmentsToProto(items))
}

func (h *Handler) AnalyticsInsights(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetSurveyAnalyticsInsightsRequest,
) (dispatcher.Result[*attunev1.GetSurveyAnalyticsInsightsResponse], error) {
	filter, err := analyticsInsightFilter(ctx.Auth.TenantID, req)
	if err != nil {
		return dispatcher.Fail[*attunev1.GetSurveyAnalyticsInsightsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey analytics insight filter")
	}
	items, err := h.service.AnalyticsInsights(ctx, filter)
	if err != nil {
		return consoleError[*attunev1.GetSurveyAnalyticsInsightsResponse](err, "survey analytics insights failed")
	}
	return dispatcher.OK(analyticsInsightsToProto(items))
}

func (h *Handler) UpdateLowScoreReview(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateSurveyLowScoreReviewRequest,
) (dispatcher.Result[*attunev1.SurveyLowScoreReview], error) {
	responseID, err := parseUUID(req.GetResponseId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyLowScoreReview](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey response id")
	}
	ownerID, err := optionalUUID(req.GetOwnerMemberId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyLowScoreReview](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid owner member id")
	}
	dueAt, err := optionalTime(req.GetDueAt())
	if err != nil {
		return dispatcher.Fail[*attunev1.SurveyLowScoreReview](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid due_at")
	}
	item, err := h.service.UpdateLowScoreReview(ctx, svc.ReviewInput{
		TenantID:          ctx.Auth.TenantID,
		ResponseID:        responseID,
		Status:            reviewStatusToRepo(req.GetStatus()),
		Severity:          severityToRepo(req.GetSeverity()),
		OwnerMemberID:     ownerID,
		OwnerMemberIDSet:  req.OwnerMemberId != nil,
		RootCause:         req.RootCause,
		ActionTaken:       req.ActionTaken,
		CustomerContacted: req.CustomerContacted,
		DueAt:             dueAt,
		DueAtSet:          req.DueAt != nil,
		ActorID:           ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.SurveyLowScoreReview](err, "survey low score review failed")
	}
	_ = h.record(ctx, "survey.low_score_review_update", "survey_low_score_review", item.ResponseID.String(), "Updated survey low score review")
	return dispatcher.OK(reviewToProto(item))
}

func (h *Handler) BatchUpdateLowScoreReviews(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.BatchUpdateSurveyLowScoreReviewsRequest,
) (dispatcher.Result[*attunev1.BatchUpdateSurveyLowScoreReviewsResponse], error) {
	responseIDs, err := parseUUIDs(req.GetResponseIds())
	if err != nil {
		return dispatcher.Fail[*attunev1.BatchUpdateSurveyLowScoreReviewsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey response id")
	}
	ownerID, err := optionalUUID(req.GetOwnerMemberId())
	if err != nil {
		return dispatcher.Fail[*attunev1.BatchUpdateSurveyLowScoreReviewsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid owner member id")
	}
	dueAt, err := optionalTime(req.GetDueAt())
	if err != nil {
		return dispatcher.Fail[*attunev1.BatchUpdateSurveyLowScoreReviewsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid due_at")
	}
	items, err := h.service.BatchUpdateLowScoreReviews(ctx, svc.BatchReviewInput{
		TenantID:          ctx.Auth.TenantID,
		ResponseIDs:       responseIDs,
		Status:            reviewStatusToRepo(req.GetStatus()),
		Severity:          severityToRepo(req.GetSeverity()),
		OwnerMemberID:     ownerID,
		OwnerMemberIDSet:  req.OwnerMemberId != nil,
		RootCause:         req.RootCause,
		ActionTaken:       req.ActionTaken,
		CustomerContacted: req.CustomerContacted,
		DueAt:             dueAt,
		DueAtSet:          req.DueAt != nil,
		ActorID:           ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.BatchUpdateSurveyLowScoreReviewsResponse](err, "survey low score review batch failed")
	}
	out := ptrext.Of(attunev1.BatchUpdateSurveyLowScoreReviewsResponse{
		Reviews: make([]*attunev1.SurveyLowScoreReview, 0, len(items)),
	})
	for _, item := range items {
		out.Reviews = append(out.Reviews, reviewToProto(item))
	}
	_ = h.record(ctx, "survey.low_score_review_batch_update", "survey_low_score_review", ctx.Auth.TenantID, "Updated survey low score reviews")
	return dispatcher.OK(out)
}

func (h *Handler) AssignLowScoreReviews(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.AssignSurveyLowScoreReviewsRequest,
) (dispatcher.Result[*attunev1.AssignSurveyLowScoreReviewsResponse], error) {
	responseIDs, err := parseUUIDs(req.GetResponseIds())
	if err != nil {
		return dispatcher.Fail[*attunev1.AssignSurveyLowScoreReviewsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey response id")
	}
	ownerIDs, err := parseUUIDs(req.GetCandidateOwnerMemberIds())
	if err != nil {
		return dispatcher.Fail[*attunev1.AssignSurveyLowScoreReviewsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid owner member id")
	}
	result, err := h.service.AssignLowScoreReviews(ctx, svc.AssignmentInput{
		TenantID:                ctx.Auth.TenantID,
		ResponseIDs:             responseIDs,
		CandidateOwnerMemberIDs: ownerIDs,
		DueInHours:              int(req.GetDueInHours()),
		ActorID:                 ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.AssignSurveyLowScoreReviewsResponse](err, "survey low score review assignment failed")
	}
	_ = h.record(ctx, "survey.low_score_review_assign", "survey_low_score_review", ctx.Auth.TenantID, "Assigned survey low score reviews")
	return dispatcher.OK(assignmentResultToProto(result))
}

func (h *Handler) EscalateLowScoreReviews(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.EscalateSurveyLowScoreReviewsRequest,
) (dispatcher.Result[*attunev1.EscalateSurveyLowScoreReviewsResponse], error) {
	responseIDs, err := parseUUIDs(req.GetResponseIds())
	if err != nil {
		return dispatcher.Fail[*attunev1.EscalateSurveyLowScoreReviewsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid survey response id")
	}
	result, err := h.service.EscalateLowScoreReviews(ctx, svc.EscalationInput{
		TenantID:    ctx.Auth.TenantID,
		ResponseIDs: responseIDs,
		DueInHours:  int(req.GetDueInHours()),
		Note:        req.GetNote(),
		ActorID:     ctx.Auth.UserID,
	})
	if err != nil {
		return consoleError[*attunev1.EscalateSurveyLowScoreReviewsResponse](err, "survey low score review escalation failed")
	}
	_ = h.record(ctx, "survey.low_score_review_escalate", "survey_low_score_review", ctx.Auth.TenantID, "Escalated survey low score reviews")
	return dispatcher.OK(escalationResultToProto(result))
}

func parseUUIDs(raw []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		id, err := parseUUID(value)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func responseFilter(tenantID string, req *attunev1.ListSurveyResponsesRequest) (repo.ResponseFilter, error) {
	campaignID, err := optionalUUID(req.GetCampaignId())
	if err != nil {
		return repo.ResponseFilter{}, err
	}
	from, err := optionalTime(req.GetFrom())
	if err != nil {
		return repo.ResponseFilter{}, err
	}
	to, err := optionalTime(req.GetTo())
	if err != nil {
		return repo.ResponseFilter{}, err
	}
	recoverySLAStatus := recoverySLAStatusToRepo(req.GetRecoverySlaStatus())
	if req.GetRecoverySlaStatus() != attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_UNSPECIFIED &&
		recoverySLAStatus == "" {
		return repo.ResponseFilter{}, errors.New("invalid recovery SLA status")
	}
	recoveryBlockerReason := ""
	if raw := strings.TrimSpace(req.GetRecoveryBlockerReason()); raw != "" {
		var ok bool
		recoveryBlockerReason, ok = recoveryBlockerReasonFromQuery(raw)
		if !ok {
			return repo.ResponseFilter{}, errors.New("invalid recovery blocker reason")
		}
	}
	reviewSeverity := severityToRepo(req.GetReviewSeverity())
	if req.GetReviewSeverity() != attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_UNSPECIFIED &&
		reviewSeverity == "" {
		return repo.ResponseFilter{}, errors.New("invalid review severity")
	}
	ownerMemberID, err := optionalUUID(req.GetOwnerMemberId())
	if err != nil {
		return repo.ResponseFilter{}, err
	}
	return repo.ResponseFilter{
		TenantID:              tenantID,
		CampaignID:            campaignID,
		LowScoreOnly:          req.LowScoreOnly,
		SubmittedFrom:         from,
		SubmittedTo:           to,
		RecoverySLAStatus:     recoverySLAStatus,
		RecoveryBlockerReason: recoveryBlockerReason,
		ReviewSeverity:        reviewSeverity,
		OwnerMemberID:         ownerMemberID,
		AccountKey:            strings.TrimSpace(req.GetAccountKey()),
		Limit:                 int(req.GetLimit()),
	}, nil
}

func analyticsFilter(tenantID string, req *attunev1.GetSurveyAnalyticsRequest) (repo.AnalyticsFilter, error) {
	campaignID, err := optionalUUID(req.GetCampaignId())
	if err != nil {
		return repo.AnalyticsFilter{}, err
	}
	from, err := optionalTime(req.GetFrom())
	if err != nil {
		return repo.AnalyticsFilter{}, err
	}
	to, err := optionalTime(req.GetTo())
	if err != nil {
		return repo.AnalyticsFilter{}, err
	}
	return repo.AnalyticsFilter{
		TenantID:   tenantID,
		CampaignID: campaignID,
		From:       from,
		To:         to,
	}, nil
}

func analyticsTrendFilter(tenantID string, req *attunev1.GetSurveyAnalyticsTrendRequest) (repo.AnalyticsFilter, error) {
	campaignID, err := optionalUUID(req.GetCampaignId())
	if err != nil {
		return repo.AnalyticsFilter{}, err
	}
	from, err := optionalTime(req.GetFrom())
	if err != nil {
		return repo.AnalyticsFilter{}, err
	}
	to, err := optionalTime(req.GetTo())
	if err != nil {
		return repo.AnalyticsFilter{}, err
	}
	return repo.AnalyticsFilter{
		TenantID:   tenantID,
		CampaignID: campaignID,
		From:       from,
		To:         to,
	}, nil
}

func analyticsSegmentFilter(tenantID string, req *attunev1.GetSurveyAnalyticsSegmentsRequest) (repo.AnalyticsSegmentFilter, error) {
	campaignID, err := optionalUUID(req.GetCampaignId())
	if err != nil {
		return repo.AnalyticsSegmentFilter{}, err
	}
	from, err := optionalTime(req.GetFrom())
	if err != nil {
		return repo.AnalyticsSegmentFilter{}, err
	}
	to, err := optionalTime(req.GetTo())
	if err != nil {
		return repo.AnalyticsSegmentFilter{}, err
	}
	return repo.AnalyticsSegmentFilter{
		TenantID:   tenantID,
		CampaignID: campaignID,
		From:       from,
		To:         to,
		Dimension:  analyticsSegmentDimensionToRepo(req.GetDimension()),
		Limit:      int(req.GetLimit()),
	}, nil
}

func analyticsInsightFilter(tenantID string, req *attunev1.GetSurveyAnalyticsInsightsRequest) (svc.AnalyticsInsightFilter, error) {
	campaignID, err := optionalUUID(req.GetCampaignId())
	if err != nil {
		return svc.AnalyticsInsightFilter{}, err
	}
	from, err := optionalTime(req.GetFrom())
	if err != nil {
		return svc.AnalyticsInsightFilter{}, err
	}
	to, err := optionalTime(req.GetTo())
	if err != nil {
		return svc.AnalyticsInsightFilter{}, err
	}
	return svc.AnalyticsInsightFilter{
		TenantID:   tenantID,
		CampaignID: campaignID,
		From:       from,
		To:         to,
		Limit:      int(req.GetLimit()),
	}, nil
}

func (h *Handler) record(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	action string,
	targetType string,
	targetID string,
	summary string,
) error {
	return h.recordChange(ctx, action, targetType, targetID, summary, nil, nil)
}

func (h *Handler) recordAfter(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	action string,
	targetType string,
	targetID string,
	summary string,
	after any,
) error {
	return h.recordChange(ctx, action, targetType, targetID, summary, nil, after)
}

func (h *Handler) recordChange(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	action string,
	targetType string,
	targetID string,
	summary string,
	before any,
	after any,
) error {
	if h.audit == nil {
		return nil
	}
	return h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   ctx.Auth.TenantID,
		Actor:      auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}

func consoleError[Resp proto.Message](err error, message string) (dispatcher.Result[Resp], error) {
	switch {
	case errors.Is(err, svc.ErrValidation):
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, message)
	case errors.Is(err, svc.ErrNotFound):
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, message)
	case errors.Is(err, svc.ErrConflict):
		return dispatcher.Fail[Resp](http.StatusConflict, attunev1.ErrorCode_CONFLICT, message)
	case errors.Is(err, svc.ErrDisabled):
		return dispatcher.Fail[Resp](http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, message)
	default:
		return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, message)
	}
}

func bindLimit(raw string, set func(int32)) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	limit, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid survey list limit")
	}
	set(int32(limit))
	return nil
}

func int32PtrToInt(value *int32) *int {
	if value == nil {
		return nil
	}
	return ptrext.Of(int(ptrext.Indirect(value)))
}

func structMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

func mapStruct(values map[string]any) *structpb.Struct {
	if values == nil {
		values = map[string]any{}
	}
	out, err := structpb.NewStruct(values)
	if err != nil {
		out, _ = structpb.NewStruct(map[string]any{})
	}
	return out
}

func parseUUID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func optionalUUID(raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := parseUUID(raw)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(id), nil
}

func optionalTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(t), nil
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func optionalTimeString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return ptrext.Of(timeString(ptrext.Indirect(t)))
}
