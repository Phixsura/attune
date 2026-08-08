// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/survey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/survey"
)

func TestBindListSurveyResponsesRecoveryFilters(t *testing.T) {
	t.Parallel()

	ownerID := "22222222-2222-2222-2222-222222222222"
	req := ptrext.Of(attunev1.ListSurveyResponsesRequest{})
	httpReq := httptest.NewRequest(
		http.MethodGet,
		"/surveys/responses?recovery_sla_status=SURVEY_RECOVERY_SLA_STATUS_OVERDUE&recovery_blocker_reason=owner_missing&review_severity=SURVEY_LOW_SCORE_SEVERITY_CRITICAL&owner_member_id="+ownerID+"&account_key=+acct:acme+&limit=9",
		http.NoBody,
	)

	if err := BindListSurveyResponses(httpReq, req); err != nil {
		t.Fatalf("BindListSurveyResponses() error = %v", err)
	}
	if req.GetRecoverySlaStatus() != attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_OVERDUE {
		t.Fatalf("RecoverySlaStatus = %v, want overdue", req.GetRecoverySlaStatus())
	}
	if req.GetRecoveryBlockerReason() != repo.RecoveryBlockerOwner {
		t.Fatalf("RecoveryBlockerReason = %q, want %q", req.GetRecoveryBlockerReason(), repo.RecoveryBlockerOwner)
	}
	if req.GetReviewSeverity() != attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_CRITICAL {
		t.Fatalf("ReviewSeverity = %v, want critical", req.GetReviewSeverity())
	}
	if req.GetOwnerMemberId() != ownerID {
		t.Fatalf("OwnerMemberId = %q, want %q", req.GetOwnerMemberId(), ownerID)
	}
	if req.GetAccountKey() != "acct:acme" {
		t.Fatalf("AccountKey = %q, want acct:acme", req.GetAccountKey())
	}

	filter, err := responseFilter("tenant-1", req)
	if err != nil {
		t.Fatalf("responseFilter() error = %v", err)
	}
	if filter.RecoverySLAStatus != repo.RecoverySLAOverdue {
		t.Fatalf("RecoverySLAStatus = %q, want %q", filter.RecoverySLAStatus, repo.RecoverySLAOverdue)
	}
	if filter.RecoveryBlockerReason != repo.RecoveryBlockerOwner {
		t.Fatalf("RecoveryBlockerReason = %q, want %q", filter.RecoveryBlockerReason, repo.RecoveryBlockerOwner)
	}
	if filter.ReviewSeverity != repo.SeverityCritical {
		t.Fatalf("ReviewSeverity = %q, want %q", filter.ReviewSeverity, repo.SeverityCritical)
	}
	if filter.OwnerMemberID == nil || filter.OwnerMemberID.String() != ownerID {
		t.Fatalf("OwnerMemberID = %v, want %s", filter.OwnerMemberID, ownerID)
	}
	if filter.AccountKey != "acct:acme" {
		t.Fatalf("AccountKey = %q, want acct:acme", filter.AccountKey)
	}
}

func TestBindListNPSCampaignRunsCursor(t *testing.T) {
	t.Parallel()

	req := ptrext.Of(attunev1.ListNpsCampaignRunsRequest{})
	httpReq := httptest.NewRequest(
		http.MethodGet,
		"/surveys/campaigns/campaign-1/nps-runs?limit=12&before_sequence=24",
		http.NoBody,
	)
	if err := BindListNPSCampaignRuns(httpReq, req); err != nil {
		t.Fatalf("BindListNPSCampaignRuns() error = %v", err)
	}
	if req.GetLimit() != 12 || req.GetBeforeSequence() != 24 {
		t.Fatalf("request = %+v, want limit 12 and cursor 24", req)
	}

	for _, target := range []string{
		"/surveys/campaigns/campaign-1/nps-runs?before_sequence=0",
		"/surveys/campaigns/campaign-1/nps-runs?before_sequence=-1",
		"/surveys/campaigns/campaign-1/nps-runs?before_sequence=not-a-sequence",
	} {
		invalid := ptrext.Of(attunev1.ListNpsCampaignRunsRequest{})
		if err := BindListNPSCampaignRuns(httptest.NewRequest(http.MethodGet, target, http.NoBody), invalid); err == nil {
			t.Fatalf("BindListNPSCampaignRuns(%q) error = nil, want invalid cursor", target)
		}
	}
}

func TestBindListSurveyResponsesAcceptsShortRecoverySLAStatus(t *testing.T) {
	t.Parallel()

	req := ptrext.Of(attunev1.ListSurveyResponsesRequest{})
	httpReq := httptest.NewRequest(
		http.MethodGet,
		"/surveys/responses?recovery_sla_status=due_soon",
		http.NoBody,
	)

	if err := BindListSurveyResponses(httpReq, req); err != nil {
		t.Fatalf("BindListSurveyResponses() error = %v", err)
	}
	if req.GetRecoverySlaStatus() != attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_DUE_SOON {
		t.Fatalf("RecoverySlaStatus = %v, want due soon", req.GetRecoverySlaStatus())
	}
}

func TestBindListSurveyResponsesRejectsInvalidRecoveryFilters(t *testing.T) {
	t.Parallel()

	for _, target := range []string{
		"/surveys/responses?recovery_sla_status=soonish",
		"/surveys/responses?recovery_blocker_reason=missing_everything",
		"/surveys/responses?review_severity=hair_on_fire",
	} {
		req := ptrext.Of(attunev1.ListSurveyResponsesRequest{})
		httpReq := httptest.NewRequest(http.MethodGet, target, http.NoBody)
		if err := BindListSurveyResponses(httpReq, req); err == nil {
			t.Fatalf("BindListSurveyResponses(%q) error = nil, want error", target)
		}
	}
}

func TestResponseFilterRejectsInvalidOwnerMemberID(t *testing.T) {
	t.Parallel()

	req := ptrext.Of(attunev1.ListSurveyResponsesRequest{
		OwnerMemberId: ptrext.Of("not-a-uuid"),
	})
	if _, err := responseFilter("tenant-1", req); err == nil {
		t.Fatal("responseFilter() error = nil, want invalid owner member id")
	}
}

func TestParseUUIDsRejectsInvalidSurveyResponseID(t *testing.T) {
	t.Parallel()

	if _, err := parseUUIDs([]string{"11111111-1111-1111-1111-111111111111", "not-a-uuid"}); err == nil {
		t.Fatal("parseUUIDs() error = nil, want invalid id")
	}
}

func TestHandlerRecordProviderEvent(t *testing.T) {
	t.Parallel()

	invitationID := uuid.New()
	campaignID := uuid.New()
	contactID := uuid.New()
	payload, err := structpb.NewStruct(map[string]any{"event_id": "evt-1"})
	if err != nil {
		t.Fatalf("NewStruct() error = %v", err)
	}
	fake := ptrext.Of(fakeSurveyService{
		invitation: repo.Invitation{
			ID:                invitationID,
			CampaignID:        campaignID,
			ContactID:         ptrext.Of(contactID),
			DeliveryStatus:    repo.DeliveryBounced,
			ResponseStatus:    repo.ResponseOpened,
			SuppressionStatus: repo.SuppressionSuppressed,
			SuppressionReason: "provider_bounce",
			CreatedAt:         time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC),
			UpdatedAt:         time.Date(2026, 7, 30, 10, 1, 0, 0, time.UTC),
		},
	})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)

	result, err := h.RecordProviderEvent(surveyHandlerContext(), ptrext.Of(attunev1.RecordSurveyProviderEventRequest{
		InvitationId:      ptrext.Of(invitationID.String()),
		Provider:          "postmark",
		ProviderEventType: "bounce",
		ProviderMessageId: "msg-1",
		ProviderEventKey:  "evt-1",
		Payload:           payload,
		OccurredAt:        ptrext.Of("2026-07-30T10:00:00Z"),
	}))
	if err != nil {
		t.Fatalf("RecordProviderEvent() error = %v", err)
	}
	requireProviderEventResponse(t, result, invitationID)
	requireProviderEventInput(t, fake.providerInput, invitationID)
	requireProviderEventAudit(t, audit.events)
}

func requireProviderEventResponse(
	t *testing.T,
	result dispatcher.Result[*attunev1.SurveyInvitation],
	invitationID uuid.UUID,
) {
	t.Helper()
	if result.Status != http.StatusOK || result.Body.GetId() != invitationID.String() {
		t.Fatalf("RecordProviderEvent() = status %d body %#v", result.Status, result.Body)
	}
}

func requireProviderEventInput(t *testing.T, input svc.ProviderEventInput, invitationID uuid.UUID) {
	t.Helper()
	if input.TenantID != "tenant-1" ||
		input.ProviderMessageID != "msg-1" ||
		input.ProviderEventKey != "evt-1" {
		t.Fatalf("provider input = %+v", input)
	}
	if input.InvitationID == nil || ptrext.Indirect(input.InvitationID) != invitationID {
		t.Fatalf("provider invitation id = %v", input.InvitationID)
	}
	if input.Payload["event_id"] != "evt-1" {
		t.Fatalf("provider payload = %+v", input.Payload)
	}
	if !input.OccurredAt.Equal(time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("occurred_at = %s", input.OccurredAt)
	}
}

func requireProviderEventAudit(t *testing.T, events []auditlogsvc.Event) {
	t.Helper()
	if len(events) != 1 || events[0].Action != "survey.provider_event_record" {
		t.Fatalf("audit events = %+v", events)
	}
	after, ok := events[0].After.(map[string]any)
	if !ok {
		t.Fatalf("audit after = %#v, want map", events[0].After)
	}
	wantAfter := map[string]any{
		"ok":                          true,
		"provider":                    "postmark",
		"provider_event_type":         repo.ProviderEventBounced,
		"delivery_status":             repo.DeliveryBounced,
		"response_status":             repo.ResponseOpened,
		"suppression_status":          repo.SuppressionSuppressed,
		"suppression_reason":          "provider_bounce",
		"contact_suppressed":          true,
		"payload_present":             true,
		"provider_message_id_present": true,
		"provider_event_key_present":  true,
	}
	for key, want := range wantAfter {
		if got := after[key]; got != want {
			t.Fatalf("audit after[%s] = %#v, want %#v in %#v", key, got, want, after)
		}
	}
	for _, forbidden := range []string{"payload", "provider_message_id", "provider_event_key"} {
		if _, ok := after[forbidden]; ok {
			t.Fatalf("audit after leaked %s: %#v", forbidden, after)
		}
	}
}

func TestHandlerRecordProviderEventRejectsMalformedFields(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeSurveyService{}))
	if _, err := h.RecordProviderEvent(surveyHandlerContext(), ptrext.Of(attunev1.RecordSurveyProviderEventRequest{
		InvitationId: ptrext.Of("not-a-uuid"),
	})); err == nil {
		t.Fatal("RecordProviderEvent() invalid id error = nil")
	}
	if _, err := h.RecordProviderEvent(surveyHandlerContext(), ptrext.Of(attunev1.RecordSurveyProviderEventRequest{
		OccurredAt: ptrext.Of("not-a-time"),
	})); err == nil {
		t.Fatal("RecordProviderEvent() invalid occurred_at error = nil")
	}
}

func TestHandlerRetryInvitationDelivery(t *testing.T) {
	t.Parallel()

	invitationID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	campaignID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	fake := ptrext.Of(fakeSurveyService{
		invitation: repo.Invitation{
			ID:                     invitationID,
			TenantID:               "tenant-1",
			CampaignID:             campaignID,
			CampaignContentVersion: 1,
			DeliveryStatus:         repo.DeliveryPending,
			ResponseStatus:         repo.ResponseNotStarted,
			SuppressionStatus:      repo.SuppressionNotSuppressed,
			CreatedAt:              time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC),
			UpdatedAt:              time.Date(2026, 7, 30, 10, 1, 0, 0, time.UTC),
		},
	})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)

	result, err := h.RetryInvitationDelivery(surveyHandlerContext(), ptrext.Of(attunev1.RetrySurveyInvitationDeliveryRequest{
		Id: invitationID.String(),
	}))
	if err != nil {
		t.Fatalf("RetryInvitationDelivery() error = %v", err)
	}
	if result.Status != http.StatusOK || result.Body.GetId() != invitationID.String() {
		t.Fatalf("RetryInvitationDelivery() = status %d body %#v", result.Status, result.Body)
	}
	if fake.retryTenantID != "tenant-1" || fake.retryInvitationID != invitationID || fake.retryActorID != "user-1" {
		t.Fatalf("retry input tenant=%q id=%s actor=%q", fake.retryTenantID, fake.retryInvitationID, fake.retryActorID)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "survey.invitation_delivery_retry" {
		t.Fatalf("audit events = %+v", audit.events)
	}
}

func TestHandlerRetryInvitationDeliveryRejectsMalformedID(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeSurveyService{}))
	if _, err := h.RetryInvitationDelivery(surveyHandlerContext(), ptrext.Of(attunev1.RetrySurveyInvitationDeliveryRequest{
		Id: "not-a-uuid",
	})); err == nil {
		t.Fatal("RetryInvitationDelivery() invalid id error = nil")
	}
}

func TestHandlerPreviewRecipients(t *testing.T) {
	t.Parallel()

	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	requestID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	contactID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	contextStruct, err := structpb.NewStruct(map[string]any{"workflow_category": "closed"})
	if err != nil {
		t.Fatalf("NewStruct() error = %v", err)
	}
	fake := ptrext.Of(fakeSurveyService{
		preview: svc.RecipientPreviewResult{
			CampaignID:     campaignID,
			TriggerMatched: true,
			SampleIncluded: true,
			MatchedCount:   1,
			EligibleCount:  1,
			DeliveryReady:  true,
			Recipients: []svc.RecipientPreview{{
				SourceType:     repo.TriggerWorkflowTransition,
				SourceID:       "42",
				RequestID:      ptrext.Of(requestID),
				ContactID:      ptrext.Of(contactID),
				Channel:        "email",
				DisplayName:    "Ada Lovelace",
				SubjectDisplay: "Ada",
				Eligible:       true,
				LastActivityAt: ptrext.Of(time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)),
			}},
		},
	})
	h := NewHandler(fake)

	result, err := h.PreviewRecipients(surveyHandlerContext(), ptrext.Of(attunev1.PreviewSurveyRecipientsRequest{
		CampaignId: campaignID.String(),
		SourceType: "feedback",
		SourceId:   "42",
		RequestId:  ptrext.Of(requestID.String()),
		Context:    contextStruct,
		Limit:      5,
	}))
	if err != nil {
		t.Fatalf("PreviewRecipients() error = %v", err)
	}
	requirePreviewHandlerResponse(t, result, contactID)
	requirePreviewHandlerInput(t, fake.previewInput, campaignID, requestID)
}

func requirePreviewHandlerResponse(
	t *testing.T,
	result dispatcher.Result[*attunev1.PreviewSurveyRecipientsResponse],
	contactID uuid.UUID,
) {
	t.Helper()
	if result.Status != http.StatusOK || result.Body.GetEligibleCount() != 1 {
		t.Fatalf("PreviewRecipients() = status %d body %#v", result.Status, result.Body)
	}
	if !result.Body.GetDeliveryReady() || result.Body.GetDeliveryBlocker() != "" {
		t.Fatalf("PreviewRecipients() delivery = %t/%q, want ready", result.Body.GetDeliveryReady(), result.Body.GetDeliveryBlocker())
	}
	got := result.Body.GetRecipients()[0]
	if got.GetContactId() != contactID.String() || !got.GetEligible() || got.GetLastActivityAt() == "" {
		t.Fatalf("preview recipient = %#v", got)
	}
}

func requirePreviewHandlerInput(
	t *testing.T,
	input svc.RecipientPreviewInput,
	campaignID uuid.UUID,
	requestID uuid.UUID,
) {
	t.Helper()
	if input.TenantID != "tenant-1" || input.CampaignID != campaignID || input.SourceID != "42" || input.Limit != 5 {
		t.Fatalf("preview input = %+v", input)
	}
	if input.RequestID == nil || ptrext.Indirect(input.RequestID) != requestID {
		t.Fatalf("preview request id = %v", input.RequestID)
	}
	if input.Context["workflow_category"] != "closed" {
		t.Fatalf("preview context = %+v", input.Context)
	}
}

func TestHandlerPreviewRecipientsRejectsMalformedIDs(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeSurveyService{}))
	if _, err := h.PreviewRecipients(surveyHandlerContext(), ptrext.Of(attunev1.PreviewSurveyRecipientsRequest{
		CampaignId: "not-a-uuid",
	})); err == nil {
		t.Fatal("PreviewRecipients() invalid campaign id error = nil")
	}
	if _, err := h.PreviewRecipients(surveyHandlerContext(), ptrext.Of(attunev1.PreviewSurveyRecipientsRequest{
		CampaignId: uuid.NewString(),
		RequestId:  ptrext.Of("not-a-uuid"),
	})); err == nil {
		t.Fatal("PreviewRecipients() invalid request id error = nil")
	}
}

func TestHandlerSendTestEmail(t *testing.T) {
	t.Parallel()

	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	sentAt := time.Date(2026, 7, 30, 12, 34, 0, 0, time.UTC)
	fake := ptrext.Of(fakeSurveyService{
		testEmailResult: svc.TestEmailResult{
			OK:       true,
			Provider: "postmark",
			SentAt:   sentAt,
		},
	})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)

	result, err := h.SendTestEmail(surveyHandlerContext(), ptrext.Of(attunev1.SendSurveyTestEmailRequest{
		CampaignId: campaignID.String(),
		ToEmail:    "operator@example.test",
	}))
	if err != nil {
		t.Fatalf("SendTestEmail() error = %v", err)
	}
	requireSendTestEmailResponse(t, result)
	requireSendTestEmailInput(t, fake.testEmailInput, campaignID)
	requireSendTestEmailAudit(t, audit.events, campaignID)
}

func requireSendTestEmailResponse(
	t *testing.T,
	result dispatcher.Result[*attunev1.SendSurveyTestEmailResponse],
) {
	t.Helper()
	if result.Status != http.StatusOK || !result.Body.GetOk() {
		t.Fatalf("SendTestEmail() = status %d body %#v", result.Status, result.Body)
	}
	if result.Body.GetProvider() != "postmark" || result.Body.GetSentAt() != "2026-07-30T12:34:00Z" {
		t.Fatalf("SendTestEmail() body = %#v", result.Body)
	}
}

func requireSendTestEmailInput(t *testing.T, input svc.TestEmailInput, campaignID uuid.UUID) {
	t.Helper()
	if input.TenantID != "tenant-1" || input.CampaignID != campaignID {
		t.Fatalf("test email input tenant/campaign = %+v", input)
	}
	if input.ToEmail != "operator@example.test" || input.ActorID != "user-1" {
		t.Fatalf("test email input recipient/actor = %+v", input)
	}
}

func requireSendTestEmailAudit(t *testing.T, events []auditlogsvc.Event, campaignID uuid.UUID) {
	t.Helper()
	if len(events) != 1 || events[0].Action != "survey.test_email_send" || events[0].TargetID != campaignID.String() {
		t.Fatalf("audit events = %+v", events)
	}
	after, ok := events[0].After.(map[string]any)
	if !ok {
		t.Fatalf("audit after = %#v, want map", events[0].After)
	}
	requireSendTestEmailAuditAfter(t, after)
}

func requireSendTestEmailAuditAfter(t *testing.T, after map[string]any) {
	t.Helper()
	if after["provider"] != "postmark" || after["sent_at"] != "2026-07-30T12:34:00Z" {
		t.Fatalf("audit delivery evidence = %+v", after)
	}
	if after["ok"] != true || after["test_only"] != true || after["invitation_persisted"] != false {
		t.Fatalf("audit safety evidence = %+v", after)
	}
	if _, exists := after["to_email"]; exists {
		t.Fatalf("audit after leaked to_email: %+v", after)
	}
}

func TestHandlerSendTestEmailRejectsMalformedCampaignID(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeSurveyService{}))
	if _, err := h.SendTestEmail(surveyHandlerContext(), ptrext.Of(attunev1.SendSurveyTestEmailRequest{
		CampaignId: "not-a-uuid",
		ToEmail:    "operator@example.test",
	})); err == nil {
		t.Fatal("SendTestEmail() invalid campaign id error = nil")
	}
}

func TestHandlerSendTestEmailAuditsFailureWithoutRecipientLeak(t *testing.T) {
	t.Parallel()

	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	fake := ptrext.Of(fakeSurveyService{err: svc.ErrDisabled})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)

	_, err := h.SendTestEmail(surveyHandlerContext(), ptrext.Of(attunev1.SendSurveyTestEmailRequest{
		CampaignId: campaignID.String(),
		ToEmail:    "operator@example.test",
	}))
	if err == nil {
		t.Fatal("SendTestEmail() error = nil, want service error")
	}
	requireSendTestEmailFailureAudit(t, audit.events, campaignID, "disabled")
}

func requireSendTestEmailFailureAudit(
	t *testing.T,
	events []auditlogsvc.Event,
	campaignID uuid.UUID,
	errorCode string,
) {
	t.Helper()
	if len(events) != 1 || events[0].Action != "survey.test_email_send" || events[0].TargetID != campaignID.String() {
		t.Fatalf("audit events = %+v", events)
	}
	after, ok := events[0].After.(map[string]any)
	if !ok {
		t.Fatalf("audit after = %#v, want map", events[0].After)
	}
	if after["ok"] != false || after["error_code"] != errorCode {
		t.Fatalf("audit failure evidence = %+v", after)
	}
	if after["test_only"] != true || after["invitation_persisted"] != false {
		t.Fatalf("audit safety evidence = %+v", after)
	}
	if _, exists := after["to_email"]; exists {
		t.Fatalf("audit after leaked to_email: %+v", after)
	}
}

func TestHandlerCampaignHealth(t *testing.T) {
	t.Parallel()

	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	fake := ptrext.Of(fakeSurveyService{
		health: svc.CampaignHealth{
			CampaignID:     campaignID,
			Status:         svc.CampaignHealthBlocked,
			ReadinessScore: 65,
			Funnel: svc.CampaignHealthFunnel{
				InvitationCount:            12,
				PendingCount:               2,
				DelayedCount:               1,
				DeliveredCount:             7,
				StartedCount:               5,
				CompletedCount:             3,
				SuppressedCount:            2,
				OverdueLowScoreReviewCount: 1,
				StartRate:                  5.0 / 12.0,
				CompletionRate:             3.0 / 5.0,
				ResponseRate:               0.25,
			},
			Checks: []svc.CampaignHealthCheck{{
				ID:                "delivery-readiness",
				Status:            svc.CampaignHealthCheckFail,
				Title:             "Delivery path is blocked",
				Summary:           "The campaign cannot safely deliver survey invitations.",
				RecommendedAction: "Resolve the delivery blocker.",
				Evidence:          "blocker=email_sender_not_configured",
			}},
			SuppressionReasons: []repo.SuppressionReasonBucket{{
				Reason: "contact_cooldown",
				Count:  2,
			}},
			GeneratedAt: time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC),
		},
	})
	h := NewHandler(fake)

	result, err := h.CampaignHealth(surveyHandlerContext(), ptrext.Of(attunev1.GetSurveyCampaignHealthRequest{
		CampaignId: campaignID.String(),
	}))
	if err != nil {
		t.Fatalf("CampaignHealth() error = %v", err)
	}
	if result.Status != http.StatusOK || result.Body.GetCampaignId() != campaignID.String() {
		t.Fatalf("CampaignHealth() = status %d body %#v", result.Status, result.Body)
	}
	if fake.healthTenantID != "tenant-1" || fake.healthCampaignID != campaignID {
		t.Fatalf("health input tenant=%q campaign=%s", fake.healthTenantID, fake.healthCampaignID)
	}
	if result.Body.GetStatus() != attunev1.SurveyCampaignHealthStatus_SURVEY_CAMPAIGN_HEALTH_STATUS_BLOCKED ||
		result.Body.GetReadinessScore() != 65 {
		t.Fatalf("health status/score = %v/%d", result.Body.GetStatus(), result.Body.GetReadinessScore())
	}
	requireHandlerCampaignHealthFunnel(t, result.Body.GetFunnel())
	if got := result.Body.GetChecks()[0]; got.GetStatus() != attunev1.SurveyCampaignHealthCheckStatus_SURVEY_CAMPAIGN_HEALTH_CHECK_STATUS_FAIL ||
		got.GetEvidence() != "blocker=email_sender_not_configured" {
		t.Fatalf("health check = %#v", got)
	}
	if result.Body.GetSuppressionReasonDistribution()[0].GetReason() != "contact_cooldown" ||
		result.Body.GetGeneratedAt() != "2026-07-30T13:00:00Z" {
		t.Fatalf("health metadata = %#v", result.Body)
	}
}

func requireHandlerCampaignHealthFunnel(t *testing.T, funnel *attunev1.SurveyCampaignHealthFunnel) {
	t.Helper()
	if funnel.GetPendingCount() != 2 || funnel.GetStartedCount() != 5 {
		t.Fatalf("health funnel counts = %#v", funnel)
	}
	if funnel.GetStartRate() != 5.0/12.0 || funnel.GetCompletionRate() != 3.0/5.0 {
		t.Fatalf("health funnel conversion rates = %#v", funnel)
	}
	if funnel.GetResponseRate() != 0.25 {
		t.Fatalf("health funnel response rate = %#v", funnel)
	}
}

func TestHandlerCampaignHealthRejectsMalformedCampaignID(t *testing.T) {
	t.Parallel()

	h := NewHandler(ptrext.Of(fakeSurveyService{}))
	if _, err := h.CampaignHealth(surveyHandlerContext(), ptrext.Of(attunev1.GetSurveyCampaignHealthRequest{
		CampaignId: "not-a-uuid",
	})); err == nil {
		t.Fatal("CampaignHealth() invalid id error = nil")
	}
}

func TestHandlerCampaignLifecycleEndpoints(t *testing.T) {
	t.Parallel()

	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	invitationID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	requestID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	fake := ptrext.Of(fakeSurveyService{
		campaigns:  []repo.Campaign{surveyHandlerCampaignFixture(campaignID, now)},
		campaign:   surveyHandlerCampaignFixture(campaignID, now),
		invitation: surveyHandlerInvitationFixture(invitationID, campaignID, ptrext.Of(requestID), now),
	})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)

	requireCampaignLifecycleReads(t, h, fake, campaignID)
	requireCampaignLifecycleWrites(t, h, fake, campaignID, requestID)
	if len(audit.events) != 4 {
		t.Fatalf("audit events = %d, want four write events", len(audit.events))
	}
}

func TestHandlerNPSCampaignEndpoints(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	cohortID := uuid.New()
	ownerID := uuid.New()
	scheduledRunID := uuid.New()
	runID := uuid.New()
	requestKey := uuid.New()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	fake := ptrext.Of(fakeSurveyService{
		campaign: repo.Campaign{ID: campaignID, TenantID: "tenant-1", SurveyType: repo.TypeNPS},
		npsRuns: []repo.NPSCampaignRun{{
			ID: scheduledRunID, TenantID: "tenant-1", CampaignID: campaignID, Sequence: 1,
			Status: repo.NPSRunScheduled, ScheduledAt: now, CreatedAt: now, UpdatedAt: now,
		}, {
			ID: runID, TenantID: "tenant-1", CampaignID: campaignID, Sequence: 2,
			Status: repo.NPSRunClosed, ScheduledAt: now, InvitationCount: 12, DeliveredCount: 11,
			StartedCount: 6, CompletedCount: 4, ResponseRate: 1.0 / 2.0, CompletionRate: 2.0 / 3.0,
			CompletedResponseRate: 1.0 / 3.0, MinimumCompletedResponses: 4, MinimumResponseRatePercent: 10,
			MeasurementReadiness: repo.NPSMeasurementQualified, NPS: 25, DetractorCount: 1, PassiveCount: 1,
			PromoterCount: 2, CreatedAt: now, UpdatedAt: now,
		}},
		npsScheduleCreated: true,
		npsCancelRun: repo.NPSCampaignRun{
			ID:          scheduledRunID,
			TenantID:    "tenant-1",
			CampaignID:  campaignID,
			Sequence:    1,
			Status:      repo.NPSRunCancelled,
			ScheduledAt: now,
			CancelledAt: ptrext.Of(now.Add(time.Minute)),
			CreatedAt:   now,
			UpdatedAt:   now.Add(time.Minute),
		},
		npsCancelChanged: true,
		npsPreflight: svc.NPSCampaignPreflight{
			CampaignID:     campaignID,
			EvaluatedCount: 42,
			EligibleCount:  31,
			ExcludedCount:  11,
			ExclusionReasons: []repo.SuppressionReasonBucket{
				{Reason: "contact_missing", Count: 4},
				{Reason: "contact_unavailable", Count: 3},
				{Reason: "contact_cooldown", Count: 4},
			},
			PlannedInvitationCount:                               24,
			MaximumRunRecipients:                                 40,
			MinimumCompletedResponses:                            30,
			RecurrenceSamplingPercent:                            25,
			SamplePlanningTargetExceedsRecipientCap:              true,
			PlannedInvitationCountBelowMinimumCompletedResponses: true,
			DeliveryReady:                                        true,
			GeneratedAt:                                          now,
		},
	})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)

	requireNPSCampaignCreate(t, h, fake, cohortID, ownerID)
	requireNPSRunScheduleAndList(t, h, fake, campaignID, requestKey, now)
	requireNPSRunCancellation(t, h, fake, campaignID, scheduledRunID)
	requireNPSPreflight(t, h, fake, campaignID)
	requireNPSRunAudit(t, audit, scheduledRunID)
}

func TestHandlerNPSRunScheduleReplayDoesNotDuplicateAudit(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	runID := uuid.New()
	fake := ptrext.Of(fakeSurveyService{
		npsRuns: []repo.NPSCampaignRun{{
			ID: runID, TenantID: "tenant-1", CampaignID: campaignID, Status: repo.NPSRunScheduled,
		}},
	})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)

	result, err := h.ScheduleNPSCampaignRun(surveyHandlerContext(), ptrext.Of(attunev1.ScheduleNpsCampaignRunRequest{
		CampaignId: campaignID.String(), ClientRequestKey: uuid.New().String(),
	}))
	if err != nil || result.Status != http.StatusOK || result.Body.GetId() != runID.String() {
		t.Fatalf("ScheduleNPSCampaignRun() replay = status %d body %#v err=%v", result.Status, result.Body, err)
	}
	if len(audit.events) != 0 {
		t.Fatalf("replayed schedule wrote audit events: %#v", audit.events)
	}
}

func TestHandlerNPSRunCancellationReplayDoesNotDuplicateAudit(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	runID := uuid.New()
	now := time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC)
	fake := ptrext.Of(fakeSurveyService{
		npsCancelRun: repo.NPSCampaignRun{
			ID:          runID,
			TenantID:    "tenant-1",
			CampaignID:  campaignID,
			Status:      repo.NPSRunCancelled,
			ScheduledAt: now.Add(-time.Hour),
			CancelledAt: ptrext.Of(now),
			CancelledBy: "user-1",
		},
		npsCancelChanged: false,
	})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)

	result, err := h.CancelNPSCampaignRun(surveyHandlerContext(), ptrext.Of(attunev1.CancelNpsCampaignRunRequest{
		CampaignId: campaignID.String(), RunId: runID.String(),
	}))
	if err != nil || result.Status != http.StatusOK || result.Body.GetId() != runID.String() {
		t.Fatalf("CancelNPSCampaignRun() replay = status %d body %#v err=%v", result.Status, result.Body, err)
	}
	if len(audit.events) != 0 {
		t.Fatalf("replayed cancellation wrote audit events: %#v", audit.events)
	}
}

func TestHandlerNPSCampaignEndpointsMapNotFound(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	runID := uuid.New()
	h := NewHandler(ptrext.Of(fakeSurveyService{err: svc.ErrNotFound}))

	_, err := h.ScheduleNPSCampaignRun(surveyHandlerContext(), ptrext.Of(attunev1.ScheduleNpsCampaignRunRequest{
		CampaignId: campaignID.String(), ClientRequestKey: uuid.New().String(),
	}))
	requireNPSNotFound(t, err)

	_, err = h.CancelNPSCampaignRun(surveyHandlerContext(), ptrext.Of(attunev1.CancelNpsCampaignRunRequest{
		CampaignId: campaignID.String(), RunId: runID.String(),
	}))
	requireNPSNotFound(t, err)

	_, err = h.NPSCampaignPreflight(surveyHandlerContext(), ptrext.Of(attunev1.GetNpsCampaignPreflightRequest{
		CampaignId: campaignID.String(),
	}))
	requireNPSNotFound(t, err)
}

func requireNPSNotFound(t *testing.T, err error) {
	t.Helper()
	var got *dispatcher.Error
	if !errors.As(err, &got) || got.Status != http.StatusNotFound || got.Code != attunev1.ErrorCode_NOT_FOUND {
		t.Fatalf("NPS endpoint error = %#v, want dispatcher 404 NOT_FOUND", err)
	}
}

func requireNPSCampaignCreate(t *testing.T, h *Handler, fake *fakeSurveyService, cohortID, ownerID uuid.UUID) {
	t.Helper()
	created, err := h.CreateCampaign(surveyHandlerContext(), ptrext.Of(attunev1.CreateSurveyCampaignRequest{
		Name:             "Relationship NPS",
		SurveyType:       attunev1.SurveyType_SURVEY_TYPE_NPS,
		Status:           attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ACTIVE,
		TriggerEvent:     attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_SCHEDULED_RUN,
		DistributionMode: attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL,
		DedupePolicy:     attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_RUN,
		Locale:           "en",
		NpsSettings: ptrext.Of(attunev1.NpsCampaignSettings{
			CohortId:                   cohortID.String(),
			DetractorOwnerMemberId:     ownerID.String(),
			CollectionDays:             14,
			MaximumRunRecipients:       500,
			MinimumCompletedResponses:  30,
			MinimumResponseRatePercent: 10,
		}),
	}))
	if err != nil || created.Status != http.StatusCreated || fake.campaignInput.NPSSettings == nil {
		t.Fatalf("CreateCampaign() = status %d input %+v err=%v", created.Status, fake.campaignInput, err)
	}
	if got := fake.campaignInput.NPSSettings; got.CohortID != cohortID || got.DetractorOwnerMemberID != ownerID ||
		got.MinimumCompletedResponses != 30 || got.MinimumResponseRatePercent != 10 {
		t.Fatalf("NPS settings = %+v", got)
	}
	if fake.campaignInput.MinDaysBetweenContact != nil {
		t.Fatalf("NPS cooldown default must remain unset at the handler boundary: %d", ptrext.Indirect(fake.campaignInput.MinDaysBetweenContact))
	}
}

func requireNPSRunScheduleAndList(t *testing.T, h *Handler, fake *fakeSurveyService, campaignID, requestKey uuid.UUID, now time.Time) {
	t.Helper()
	scheduled, err := h.ScheduleNPSCampaignRun(surveyHandlerContext(), ptrext.Of(attunev1.ScheduleNpsCampaignRunRequest{
		CampaignId:       campaignID.String(),
		ClientRequestKey: requestKey.String(),
		ScheduledAt:      ptrext.Of(now.Format(time.RFC3339)),
	}))
	if err != nil || scheduled.Status != http.StatusCreated || fake.npsScheduleInput.ClientRequestKey != requestKey {
		t.Fatalf("ScheduleNPSCampaignRun() = status %d input %+v err=%v", scheduled.Status, fake.npsScheduleInput, err)
	}

	runs, err := h.ListNPSCampaignRuns(surveyHandlerContext(), ptrext.Of(attunev1.ListNpsCampaignRunsRequest{
		CampaignId: campaignID.String(), Limit: 5,
	}))
	if err != nil || runs.Status != http.StatusOK || len(runs.Body.GetRuns()) != 2 ||
		runs.Body.GetRuns()[1].GetStartedCount() != 6 || runs.Body.GetRuns()[1].GetCompletedCount() != 4 ||
		runs.Body.GetRuns()[1].GetNps() != 25 || legacyNPSResponseRate(runs.Body.GetRuns()[1]) != 1.0/2.0 ||
		runs.Body.GetRuns()[1].GetCompletionRate() != 2.0/3.0 ||
		runs.Body.GetRuns()[1].GetCompletedResponseRate() != 1.0/3.0 ||
		runs.Body.GetRuns()[1].GetMeasurementReadiness() != attunev1.NpsMeasurementReadiness_NPS_MEASUREMENT_READINESS_QUALIFIED {
		t.Fatalf("ListNPSCampaignRuns() = status %d body %#v err=%v", runs.Status, runs.Body, err)
	}
}

func legacyNPSResponseRate(run *attunev1.NpsCampaignRun) float64 {
	field := run.ProtoReflect().Descriptor().Fields().ByNumber(protoreflect.FieldNumber(18))
	return run.ProtoReflect().Get(field).Float()
}

func TestHandlerListNPSCampaignRunsReturnsHistoryCursor(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	fake := ptrext.Of(fakeSurveyService{
		npsRunPage: repo.NPSCampaignRunPage{
			Runs:               []repo.NPSCampaignRun{{ID: uuid.New(), CampaignID: campaignID, Sequence: 24}},
			NextBeforeSequence: 24,
		},
	})
	h := NewHandler(fake)

	result, err := h.ListNPSCampaignRuns(surveyHandlerContext(), ptrext.Of(attunev1.ListNpsCampaignRunsRequest{
		CampaignId:     campaignID.String(),
		Limit:          12,
		BeforeSequence: ptrext.Of(int32(48)),
	}))
	if err != nil || result.Status != http.StatusOK || result.Body.GetNextBeforeSequence() != 24 {
		t.Fatalf("ListNPSCampaignRuns() = status %d body %#v err=%v", result.Status, result.Body, err)
	}
	if fake.npsRunPageLimit != 12 || fake.npsRunPageBeforeSequence != 48 {
		t.Fatalf("page input = limit %d cursor %d, want 12 and 48", fake.npsRunPageLimit, fake.npsRunPageBeforeSequence)
	}
}

func TestHandlerListNPSCampaignRunEvidenceExportsReturnsDownloadPath(t *testing.T) {
	t.Parallel()
	campaignID := uuid.New()
	runID := uuid.New()
	exportID := uuid.New()
	fake := ptrext.Of(fakeSurveyService{
		npsEvidenceExports: []repo.NPSCampaignRunEvidenceExportSummary{{
			ID:             exportID,
			CampaignID:     campaignID,
			RunID:          runID,
			ReportVersion:  "1",
			GeneratedAt:    time.Date(2026, 8, 8, 1, 2, 3, 0, time.UTC),
			ArtifactSHA256: "sha256:abc",
			CreatedByType:  "admin",
		}},
	})
	h := NewHandler(fake)

	result, err := h.ListNPSCampaignRunEvidenceExports(surveyHandlerContext(), ptrext.Of(attunev1.ListNpsCampaignRunEvidenceExportsRequest{
		CampaignId: campaignID.String(),
		RunId:      runID.String(),
		Limit:      12,
	}))
	if err != nil || result.Status != http.StatusOK || len(result.Body.GetExports()) != 1 {
		t.Fatalf("ListNPSCampaignRunEvidenceExports() = status %d body %#v err=%v", result.Status, result.Body, err)
	}
	item := result.Body.GetExports()[0]
	if item.GetId() != exportID.String() || item.GetReportVersion() != "1" ||
		!strings.HasSuffix(item.GetDownloadPath(), "/"+exportID.String()+".csv") {
		t.Fatalf("evidence export = %#v", item)
	}
}

func requireNPSRunCancellation(t *testing.T, h *Handler, fake *fakeSurveyService, campaignID, runID uuid.UUID) {
	t.Helper()
	result, err := h.CancelNPSCampaignRun(surveyHandlerContext(), ptrext.Of(attunev1.CancelNpsCampaignRunRequest{
		CampaignId: campaignID.String(),
		RunId:      runID.String(),
	}))
	if err != nil || result.Status != http.StatusOK ||
		result.Body.GetStatus() != attunev1.NpsCampaignRunStatus_NPS_CAMPAIGN_RUN_STATUS_CANCELLED ||
		result.Body.GetCancelledAt() == "" {
		t.Fatalf("CancelNPSCampaignRun() = status %d body %#v err=%v", result.Status, result.Body, err)
	}
	if fake.npsCancelInput.TenantID != "tenant-1" || fake.npsCancelInput.CampaignID != campaignID ||
		fake.npsCancelInput.RunID != runID || fake.npsCancelInput.ActorID != "user-1" {
		t.Fatalf("CancelNPSCampaignRun input = %+v", fake.npsCancelInput)
	}
}

func requireNPSPreflight(t *testing.T, h *Handler, fake *fakeSurveyService, campaignID uuid.UUID) {
	t.Helper()
	preflight, err := h.NPSCampaignPreflight(surveyHandlerContext(), ptrext.Of(attunev1.GetNpsCampaignPreflightRequest{
		CampaignId: campaignID.String(),
	}))
	if err != nil || preflight.Status != http.StatusOK || preflight.Body.GetPlannedInvitationCount() != 24 ||
		preflight.Body.GetMinimumCompletedResponses() != 30 ||
		preflight.Body.GetRecurrenceSamplingPercent() != 25 ||
		!preflight.Body.GetPlannedInvitationCountBelowMinimumCompletedResponses() ||
		!preflight.Body.GetSamplePlanningTargetExceedsRecipientCap() ||
		preflight.Body.GetDeliveryBlocker() != "" || fake.npsPreflightTenantID != "tenant-1" ||
		fake.npsPreflightCampaignID != campaignID {
		t.Fatalf("NPSCampaignPreflight() = status %d body %#v input %q/%s err=%v", preflight.Status, preflight.Body, fake.npsPreflightTenantID, fake.npsPreflightCampaignID, err)
	}
	requireNPSPreflightExclusionReasons(t, preflight.Body)
}

func requireNPSPreflightExclusionReasons(t *testing.T, preflight *attunev1.NpsCampaignPreflight) {
	t.Helper()

	got := preflight.GetExclusionReasonDistribution()
	if len(got) != 3 ||
		got[0].GetReason() != "contact_missing" || got[0].GetCount() != 4 ||
		got[1].GetReason() != "contact_unavailable" || got[1].GetCount() != 3 ||
		got[2].GetReason() != "contact_cooldown" || got[2].GetCount() != 4 {
		t.Fatalf("NPSCampaignPreflight exclusion reason distribution = %#v", got)
	}
}

func requireNPSRunAudit(t *testing.T, audit *fakeSurveyAudit, scheduledRunID uuid.UUID) {
	t.Helper()
	if len(audit.events) != 3 || audit.events[1].Action != "survey.nps_run_schedule" ||
		audit.events[1].TargetID != scheduledRunID.String() || audit.events[2].Action != "survey.nps_run_cancel" ||
		audit.events[2].TargetID != scheduledRunID.String() {
		t.Fatalf("audit events = %+v, want NPS run schedule and cancellation audits", audit.events)
	}
}

func requireCampaignLifecycleReads(t *testing.T, h *Handler, fake *fakeSurveyService, campaignID uuid.UUID) {
	t.Helper()
	list, err := h.ListCampaigns(surveyHandlerContext(), ptrext.Of(attunev1.ListSurveyCampaignsRequest{
		Status: attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ACTIVE,
		Limit:  5,
	}))
	if err != nil || list.Status != http.StatusOK || list.Body.GetCampaigns()[0].GetId() != campaignID.String() {
		t.Fatalf("ListCampaigns() = status %d body %#v err=%v", list.Status, list.Body, err)
	}
	if fake.campaignInput.TenantID != "" {
		t.Fatalf("list unexpectedly mutated campaign input: %+v", fake.campaignInput)
	}
}

func requireCampaignLifecycleWrites(t *testing.T, h *Handler, fake *fakeSurveyService, campaignID uuid.UUID, requestID uuid.UUID) {
	t.Helper()
	triggerFilter := mustStruct(t, map[string]any{"status": "closed"})
	content := mustStruct(t, map[string]any{"question": "How was support?"})
	created, err := h.CreateCampaign(surveyHandlerContext(), ptrext.Of(attunev1.CreateSurveyCampaignRequest{
		Name: "CSAT", SurveyType: attunev1.SurveyType_SURVEY_TYPE_CSAT,
		Status:           attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ACTIVE,
		TriggerEvent:     attunev1.SurveyTriggerEvent_SURVEY_TRIGGER_EVENT_REQUEST_RESOLVED,
		DistributionMode: attunev1.SurveyDistributionMode_SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL,
		DedupePolicy:     attunev1.SurveyDedupePolicy_SURVEY_DEDUPE_POLICY_ONE_PER_RESOLUTION,
		TriggerFilter:    triggerFilter, Content: content, Locale: "en", LowScoreThreshold: ptrext.Of(int32(3)),
	}))
	if err != nil || created.Status != http.StatusCreated || fake.campaignInput.ActorID != "user-1" {
		t.Fatalf("CreateCampaign() = status %d input %+v err=%v", created.Status, fake.campaignInput, err)
	}
	updated, err := h.UpdateCampaign(surveyHandlerContext(), ptrext.Of(attunev1.UpdateSurveyCampaignRequest{
		Id: campaignID.String(), Name: ptrext.Of("CSAT v2"), Content: content, Locale: ptrext.Of("en-US"),
		Status: attunev1.SurveyCampaignStatus_SURVEY_CAMPAIGN_STATUS_ACTIVE,
	}))
	if err != nil || updated.Status != http.StatusOK || !fake.campaignInput.ContentSet {
		t.Fatalf("UpdateCampaign() = status %d input %+v err=%v", updated.Status, fake.campaignInput, err)
	}
	archived, err := h.ArchiveCampaign(surveyHandlerContext(), ptrext.Of(attunev1.ArchiveSurveyCampaignRequest{Id: campaignID.String()}))
	if err != nil || archived.Status != http.StatusOK || fake.archiveCampaignID != campaignID {
		t.Fatalf("ArchiveCampaign() = status %d fake %+v err=%v", archived.Status, fake, err)
	}
	link, err := h.CreateHostedLink(surveyHandlerContext(), ptrext.Of(attunev1.CreateSurveyHostedLinkRequest{
		CampaignId: campaignID.String(), SourceType: "request", SourceId: "CR-1",
		RequestId: ptrext.Of(requestID.String()), Context: triggerFilter,
	}))
	if err != nil || link.Status != http.StatusCreated || fake.hostedInput.RequestID == nil {
		t.Fatalf("CreateHostedLink() = status %d input %+v err=%v", link.Status, fake.hostedInput, err)
	}
}

func TestHandlerSurveyReadEndpoints(t *testing.T) {
	t.Parallel()

	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	invitationID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	responseID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	ownerID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	fake := ptrext.Of(fakeSurveyService{
		invitations: []repo.Invitation{surveyHandlerInvitationFixture(invitationID, campaignID, nil, now)},
		responses:   []repo.Response{surveyHandlerResponseFixture(responseID, campaignID, invitationID, ownerID, now)},
		analytics:   surveyHandlerAnalyticsFixture(campaignID, ownerID, now),
		trend:       []repo.AnalyticsTrendBucket{{Date: "2026-08-02", InvitationCount: 10, StartedCount: 6, CompletedCount: 4}},
		segments:    []repo.AnalyticsSegment{surveyHandlerSegmentFixture(campaignID)},
		insights:    []svc.AnalyticsInsight{surveyHandlerInsightFixture()},
	})
	h := NewHandler(fake)

	requireSurveyListReadEndpoints(t, h, fake, campaignID, ownerID)
	requireSurveyAnalyticsReadEndpoints(t, h, fake, campaignID)
}

func requireSurveyListReadEndpoints(t *testing.T, h *Handler, fake *fakeSurveyService, campaignID uuid.UUID, ownerID uuid.UUID) {
	t.Helper()
	invitations, err := h.ListInvitations(surveyHandlerContext(), ptrext.Of(attunev1.ListSurveyInvitationsRequest{
		CampaignId: ptrext.Of(campaignID.String()), ResponseStatus: attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_NOT_STARTED,
		SuppressionStatus: attunev1.SurveySuppressionStatus_SURVEY_SUPPRESSION_STATUS_NOT_SUPPRESSED, Limit: 7,
	}))
	if err != nil || invitations.Body.GetInvitations()[0].GetCampaignId() != campaignID.String() {
		t.Fatalf("ListInvitations() = %#v, %v", invitations.Body, err)
	}
	from := "2026-08-01T00:00:00Z"
	to := "2026-08-03T00:00:00Z"
	responses, err := h.ListResponses(surveyHandlerContext(), ptrext.Of(attunev1.ListSurveyResponsesRequest{
		CampaignId: ptrext.Of(campaignID.String()), LowScoreOnly: ptrext.Of(true), From: ptrext.Of(from), To: ptrext.Of(to),
		RecoverySlaStatus:     ptrext.Of(attunev1.SurveyRecoverySlaStatus_SURVEY_RECOVERY_SLA_STATUS_OVERDUE),
		RecoveryBlockerReason: ptrext.Of(repo.RecoveryBlockerOverdue), ReviewSeverity: ptrext.Of(attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_HIGH),
		OwnerMemberId: ptrext.Of(ownerID.String()), AccountKey: ptrext.Of(" acct:acme "), Limit: 9,
	}))
	if err != nil || !responses.Body.GetResponses()[0].GetLowScore() || fake.responseFilter.AccountKey != "acct:acme" {
		t.Fatalf("ListResponses() = %#v filter %+v err=%v", responses.Body, fake.responseFilter, err)
	}
}

func requireSurveyAnalyticsReadEndpoints(t *testing.T, h *Handler, fake *fakeSurveyService, campaignID uuid.UUID) {
	t.Helper()
	from := ptrext.Of("2026-08-01T00:00:00Z")
	to := ptrext.Of("2026-08-03T00:00:00Z")
	runID := uuid.New()
	analytics, err := h.Analytics(surveyHandlerContext(), ptrext.Of(attunev1.GetSurveyAnalyticsRequest{CampaignId: ptrext.Of(campaignID.String()), From: from, To: to, RunId: ptrext.Of(runID.String())}))
	if err != nil {
		t.Fatalf("Analytics() = %#v, %v", analytics.Body, err)
	}
	if analytics.Body.GetOwnerRecoveryLoads()[0].GetOpenCount() != 2 || analytics.Body.GetStartedCount() != 6 || analytics.Body.GetCompletionRate() != 2.0/3.0 {
		t.Fatalf("Analytics() = %#v", analytics.Body)
	}
	if fake.analyticsFilter.RunID == nil || ptrext.Indirect(fake.analyticsFilter.RunID) != runID {
		t.Fatalf("Analytics() filter = %#v", fake.analyticsFilter)
	}
	trend, err := h.AnalyticsTrend(surveyHandlerContext(), ptrext.Of(attunev1.GetSurveyAnalyticsTrendRequest{CampaignId: ptrext.Of(campaignID.String()), From: from, To: to, RunId: ptrext.Of(runID.String())}))
	if err != nil {
		t.Fatalf("AnalyticsTrend() = %#v, %v", trend.Body, err)
	}
	if trend.Body.GetBuckets()[0].GetDate() != "2026-08-02" || trend.Body.GetBuckets()[0].GetStartedCount() != 6 {
		t.Fatalf("AnalyticsTrend() = %#v", trend.Body)
	}
	if fake.trendFilter.RunID == nil || ptrext.Indirect(fake.trendFilter.RunID) != runID {
		t.Fatalf("AnalyticsTrend() filter = %#v", fake.trendFilter)
	}
	segments, err := h.AnalyticsSegments(surveyHandlerContext(), ptrext.Of(attunev1.GetSurveyAnalyticsSegmentsRequest{Dimension: attunev1.SurveyAnalyticsSegmentDimension_SURVEY_ANALYTICS_SEGMENT_DIMENSION_CAMPAIGN, Limit: 3}))
	if err != nil {
		t.Fatalf("AnalyticsSegments() = %#v filter %+v err=%v", segments.Body, fake.segmentFilter, err)
	}
	requireSurveyAnalyticsSegmentsRead(t, segments.Body.GetSegments(), fake.segmentFilter, campaignID)
	insights, err := h.AnalyticsInsights(surveyHandlerContext(), ptrext.Of(attunev1.GetSurveyAnalyticsInsightsRequest{Limit: 4}))
	if err != nil {
		t.Fatalf("AnalyticsInsights() = %#v filter %+v err=%v", insights.Body, fake.insightFilter, err)
	}
	requireSurveyAnalyticsInsightsRead(t, insights.Body.GetInsights(), fake.insightFilter)
}

func requireSurveyAnalyticsSegmentsRead(t *testing.T, segments []*attunev1.SurveyAnalyticsSegment, filter repo.AnalyticsSegmentFilter, campaignID uuid.UUID) {
	t.Helper()
	if len(segments) != 1 || segments[0].GetCampaignId() != campaignID.String() {
		t.Fatalf("AnalyticsSegments() = %#v", segments)
	}
	if filter.Limit != 3 {
		t.Fatalf("AnalyticsSegments() filter = %+v", filter)
	}
}

func requireSurveyAnalyticsInsightsRead(t *testing.T, insights []*attunev1.SurveyAnalyticsInsight, filter svc.AnalyticsInsightFilter) {
	t.Helper()
	if len(insights) != 1 || insights[0].GetSeverity() != attunev1.SurveyAnalyticsInsightSeverity_SURVEY_ANALYTICS_INSIGHT_SEVERITY_WARNING {
		t.Fatalf("AnalyticsInsights() = %#v", insights)
	}
	if filter.Limit != 4 {
		t.Fatalf("AnalyticsInsights() filter = %+v", filter)
	}
}

func TestHandlerSurveyRecoveryWriteEndpoints(t *testing.T) {
	t.Parallel()

	responseID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ownerID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	review := surveyHandlerReviewFixture(responseID, campaignID, ownerID, now)
	fake := ptrext.Of(fakeSurveyService{
		review: review, reviews: []repo.LowScoreReview{review},
		assignment: surveyHandlerAssignmentFixture(review, ownerID, now),
		escalation: surveyHandlerEscalationFixture(review, now),
	})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)

	requireSurveyReviewUpdateEndpoints(t, h, fake, responseID, ownerID)
	requireSurveyRecoveryAutomationEndpoints(t, h, fake, responseID, ownerID)
	if len(audit.events) != 4 {
		t.Fatalf("audit events = %d, want four recovery write events", len(audit.events))
	}
}

func requireSurveyReviewUpdateEndpoints(t *testing.T, h *Handler, fake *fakeSurveyService, responseID uuid.UUID, ownerID uuid.UUID) {
	t.Helper()
	dueAt := "2026-08-03T10:00:00Z"
	updated, err := h.UpdateLowScoreReview(surveyHandlerContext(), ptrext.Of(attunev1.UpdateSurveyLowScoreReviewRequest{
		ResponseId: responseID.String(), Status: attunev1.SurveyLowScoreReviewStatus_SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW,
		Severity: attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_HIGH, OwnerMemberId: ptrext.Of(ownerID.String()),
		RootCause: ptrext.Of("billing"), ActionTaken: ptrext.Of("credited"), CustomerContacted: ptrext.Of(true), DueAt: ptrext.Of(dueAt),
	}))
	if err != nil || updated.Body.GetOwnerMemberId() != ownerID.String() || !fake.reviewInput.OwnerMemberIDSet {
		t.Fatalf("UpdateLowScoreReview() = %#v input %+v err=%v", updated.Body, fake.reviewInput, err)
	}
	batched, err := h.BatchUpdateLowScoreReviews(surveyHandlerContext(), ptrext.Of(attunev1.BatchUpdateSurveyLowScoreReviewsRequest{
		ResponseIds: []string{responseID.String()}, Severity: attunev1.SurveyLowScoreSeverity_SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
		OwnerMemberId: ptrext.Of(ownerID.String()), DueAt: ptrext.Of(dueAt),
	}))
	if err != nil || len(batched.Body.GetReviews()) != 1 || !fake.batchInput.DueAtSet {
		t.Fatalf("BatchUpdateLowScoreReviews() = %#v input %+v err=%v", batched.Body, fake.batchInput, err)
	}
}

func requireSurveyRecoveryAutomationEndpoints(t *testing.T, h *Handler, fake *fakeSurveyService, responseID uuid.UUID, ownerID uuid.UUID) {
	t.Helper()
	assigned, err := h.AssignLowScoreReviews(surveyHandlerContext(), ptrext.Of(attunev1.AssignSurveyLowScoreReviewsRequest{
		ResponseIds: []string{responseID.String()}, CandidateOwnerMemberIds: []string{ownerID.String()}, DueInHours: ptrext.Of(int32(24)),
	}))
	if err != nil || len(assigned.Body.GetDecisions()) != 1 || fake.assignmentInput.DueInHours != 24 {
		t.Fatalf("AssignLowScoreReviews() = %#v input %+v err=%v", assigned.Body, fake.assignmentInput, err)
	}
	escalated, err := h.EscalateLowScoreReviews(surveyHandlerContext(), ptrext.Of(attunev1.EscalateSurveyLowScoreReviewsRequest{
		ResponseIds: []string{responseID.String()}, DueInHours: ptrext.Of(int32(4)), Note: ptrext.Of("urgent"),
	}))
	if err != nil || len(escalated.Body.GetDecisions()) != 1 || fake.escalationInput.Note != "urgent" {
		t.Fatalf("EscalateLowScoreReviews() = %#v input %+v err=%v", escalated.Body, fake.escalationInput, err)
	}
}

func surveyHandlerCampaignFixture(id uuid.UUID, now time.Time) repo.Campaign {
	return repo.Campaign{
		ID: id, TenantID: "tenant-1", Name: "Post-resolution CSAT", SurveyType: repo.TypeCSAT,
		Status: repo.StatusActive, TriggerEvent: repo.TriggerRequestResolved, DistributionMode: repo.DistributionContactEmail,
		DedupePolicy: repo.DedupeOnePerResolution, TriggerFilter: map[string]any{"status": "closed"},
		Content: map[string]any{"question": "How was support?"}, Locale: "en", ContentVersion: 2,
		SamplingPercent: 100, MinDaysBetweenContact: 30, ExpiresAfterDays: 14, MaxDailyInvitations: 100,
		LowScoreThreshold: 3, RequireRecentCustomerActivity: true, RecentActivityDays: 90,
		SuppressAutoResolved: true, CreatedBy: "admin-1", UpdatedBy: "admin-1", CreatedAt: now, UpdatedAt: now,
	}
}

func surveyHandlerInvitationFixture(id uuid.UUID, campaignID uuid.UUID, requestID *uuid.UUID, now time.Time) repo.Invitation {
	return repo.Invitation{
		ID: id, TenantID: "tenant-1", CampaignID: campaignID, CampaignContentVersion: 2, SourceType: "request",
		SourceID: "CR-1", RequestID: requestID, DistributionMode: repo.DistributionContactEmail,
		DeliveryStatus: repo.DeliveryPending, ResponseStatus: repo.ResponseNotStarted,
		SuppressionStatus: repo.SuppressionNotSuppressed, DeliverySecret: []byte("secret"),
		PublicURL: "https://example.test/surveys/token", CreatedAt: now, UpdatedAt: now,
	}
}

func surveyHandlerResponseFixture(id uuid.UUID, campaignID uuid.UUID, invitationID uuid.UUID, ownerID uuid.UUID, now time.Time) repo.Response {
	review := surveyHandlerReviewFixture(id, campaignID, ownerID, now)
	return repo.Response{
		ID: id, TenantID: "tenant-1", CampaignID: campaignID, InvitationID: invitationID, SourceType: "request",
		SourceID: "CR-1", Score: 2, Comment: "Still painful", Locale: "en", SubmittedAt: now,
		Account: repo.AccountContext{AccountKey: "acct:acme", AccountDisplay: "Acme Corp", Source: "response_metadata"},
		Review:  ptrext.Of(review),
	}
}

func surveyHandlerReviewFixture(responseID uuid.UUID, campaignID uuid.UUID, ownerID uuid.UUID, now time.Time) repo.LowScoreReview {
	return repo.LowScoreReview{
		ResponseID: responseID, TenantID: "tenant-1", CampaignID: campaignID, Status: repo.ReviewOpen,
		Severity: repo.SeverityHigh, OwnerMemberID: ptrext.Of(ownerID), RootCause: "billing",
		ActionTaken: "credited", CustomerContacted: true, DueAt: ptrext.Of(now.Add(24 * time.Hour)),
		UpdatedBy: "admin-1", CreatedAt: now, UpdatedAt: now, RecoveryNotificationStatus: repo.RecoveryNotificationPending,
	}
}

func surveyHandlerAnalyticsFixture(campaignID uuid.UUID, ownerID uuid.UUID, now time.Time) repo.Analytics {
	return repo.Analytics{
		CampaignID: ptrext.Of(campaignID), InvitationCount: 10, DeliveredCount: 8, StartedCount: 6, CompletedCount: 4,
		LowScoreCount: 2, AverageScore: 3.5, ResponseRate: 0.4, StartRate: 0.6, CompletionRate: 2.0 / 3.0, PositiveScoreCount: 2,
		PositiveScoreRate: 0.5, ScoreDistribution: []repo.ScoreBucket{{Score: 1, Count: 2}},
		SuppressionReasons: []repo.SuppressionReasonBucket{{Reason: "contact_cooldown", Count: 1}},
		OwnerRecoveryLoads: []repo.RecoveryOwnerLoad{{OwnerMemberID: ownerID, OpenCount: 2, OldestOpenDueAt: ptrext.Of(now), WorkloadScore: 76}},
	}
}

func surveyHandlerSegmentFixture(campaignID uuid.UUID) repo.AnalyticsSegment {
	return repo.AnalyticsSegment{
		Dimension: repo.SegmentCampaign, Key: "campaign-1", Label: "Post-resolution CSAT",
		CampaignID: ptrext.Of(campaignID), InvitationCount: 10, CompletedCount: 4,
		LowScoreRate: 0.5, AttentionScore: 9,
	}
}

func surveyHandlerInsightFixture() svc.AnalyticsInsight {
	return svc.AnalyticsInsight{
		ID: "low-score-spike", Severity: svc.InsightSeverityWarning, Title: "Low-score spike",
		Summary: "Low-score responses increased.", Metric: "low_score_rate", Value: 0.5,
		Threshold: 0.25, SegmentDimension: repo.SegmentCampaign, RecommendedAction: "Review recovery queue.", Rank: 1,
	}
}

func surveyHandlerAssignmentFixture(review repo.LowScoreReview, ownerID uuid.UUID, now time.Time) svc.AssignmentResult {
	return svc.AssignmentResult{
		Reviews: []repo.LowScoreReview{review},
		Decisions: []svc.AssignmentDecision{{
			ResponseID: review.ResponseID, OwnerMemberID: ownerID, DueAt: now.Add(24 * time.Hour),
			Severity: repo.SeverityHigh, Escalated: true, Reason: "balanced_workload", WorkloadScoreAfter: 76,
		}},
	}
}

func surveyHandlerEscalationFixture(review repo.LowScoreReview, now time.Time) svc.EscalationResult {
	return svc.EscalationResult{
		Reviews: []repo.LowScoreReview{review},
		Decisions: []svc.EscalationDecision{{
			ResponseID: review.ResponseID, PreviousSeverity: repo.SeverityHigh, Severity: repo.SeverityCritical,
			DueAt: now.Add(4 * time.Hour), OwnerMissing: false, DueAtChanged: true, Reason: repo.RecoveryBlockerOverdue,
			ActionTaken: "Escalated recovery.",
		}},
	}
}

func mustStruct(t *testing.T, values map[string]any) *structpb.Struct {
	t.Helper()
	out, err := structpb.NewStruct(values)
	if err != nil {
		t.Fatalf("NewStruct() error = %v", err)
	}
	return out
}

type fakeSurveyService struct {
	campaigns                   []repo.Campaign
	campaign                    repo.Campaign
	campaignInput               svc.CampaignInput
	npsRuns                     []repo.NPSCampaignRun
	npsRunPage                  repo.NPSCampaignRunPage
	npsRunPageLimit             int
	npsRunPageBeforeSequence    int
	npsEvidence                 svc.NPSCampaignRunEvidence
	npsEvidenceExport           repo.NPSCampaignRunEvidenceExport
	npsEvidenceExportReplayed   bool
	npsEvidenceExportCalls      int
	npsEvidenceExportRequestKey uuid.UUID
	npsEvidenceExports          []repo.NPSCampaignRunEvidenceExportSummary
	npsPreflight                svc.NPSCampaignPreflight
	npsPreflightTenantID        string
	npsPreflightCampaignID      uuid.UUID
	npsScheduleInput            svc.ScheduleNPSCampaignRunInput
	npsScheduleCreated          bool
	npsCancelInput              svc.CancelNPSCampaignRunInput
	npsCancelRun                repo.NPSCampaignRun
	npsCancelChanged            bool
	archiveTenantID             string
	archiveCampaignID           uuid.UUID
	archiveActorID              string
	invitations                 []repo.Invitation
	invitation                  repo.Invitation
	invitationFilter            repo.InvitationFilter
	hostedInput                 svc.HostedLinkInput
	responses                   []repo.Response
	responseFilter              repo.ResponseFilter
	analytics                   repo.Analytics
	analyticsFilter             repo.AnalyticsFilter
	trend                       []repo.AnalyticsTrendBucket
	trendFilter                 repo.AnalyticsFilter
	segments                    []repo.AnalyticsSegment
	segmentFilter               repo.AnalyticsSegmentFilter
	insights                    []svc.AnalyticsInsight
	insightFilter               svc.AnalyticsInsightFilter
	review                      repo.LowScoreReview
	reviews                     []repo.LowScoreReview
	reviewInput                 svc.ReviewInput
	batchInput                  svc.BatchReviewInput
	assignment                  svc.AssignmentResult
	assignmentInput             svc.AssignmentInput
	escalation                  svc.EscalationResult
	escalationInput             svc.EscalationInput
	preview                     svc.RecipientPreviewResult
	previewInput                svc.RecipientPreviewInput
	health                      svc.CampaignHealth
	healthTenantID              string
	healthCampaignID            uuid.UUID
	testEmailResult             svc.TestEmailResult
	testEmailInput              svc.TestEmailInput
	providerInput               svc.ProviderEventInput
	retryTenantID               string
	retryInvitationID           uuid.UUID
	retryActorID                string
	err                         error
}

func (f *fakeSurveyService) ListCampaigns(context.Context, string, string, int) ([]repo.Campaign, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.campaigns, nil
}

func (f *fakeSurveyService) CreateCampaign(_ context.Context, in svc.CampaignInput) (repo.Campaign, error) {
	f.campaignInput = in
	if f.err != nil {
		return repo.Campaign{}, f.err
	}
	return f.campaign, nil
}

func (f *fakeSurveyService) UpdateCampaign(_ context.Context, in svc.CampaignInput) (repo.Campaign, error) {
	f.campaignInput = in
	if f.err != nil {
		return repo.Campaign{}, f.err
	}
	return f.campaign, nil
}

func (f *fakeSurveyService) ScheduleNPSCampaignRun(_ context.Context, in svc.ScheduleNPSCampaignRunInput) (repo.NPSCampaignRun, bool, error) {
	f.npsScheduleInput = in
	if f.err != nil {
		return repo.NPSCampaignRun{}, false, f.err
	}
	if len(f.npsRuns) > 0 {
		return f.npsRuns[0], f.npsScheduleCreated, nil
	}
	return repo.NPSCampaignRun{}, f.npsScheduleCreated, nil
}

func (f *fakeSurveyService) CancelNPSCampaignRun(_ context.Context, in svc.CancelNPSCampaignRunInput) (repo.NPSCampaignRun, bool, error) {
	f.npsCancelInput = in
	if f.err != nil {
		return repo.NPSCampaignRun{}, false, f.err
	}
	return f.npsCancelRun, f.npsCancelChanged, nil
}

func (f *fakeSurveyService) ListNPSCampaignRuns(_ context.Context, _ string, _ uuid.UUID, _ int) ([]repo.NPSCampaignRun, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.npsRuns, nil
}

func (f *fakeSurveyService) ListNPSCampaignRunPage(_ context.Context, _ string, _ uuid.UUID, limit int, beforeSequence int) (repo.NPSCampaignRunPage, error) {
	f.npsRunPageLimit = limit
	f.npsRunPageBeforeSequence = beforeSequence
	if f.err != nil {
		return repo.NPSCampaignRunPage{}, f.err
	}
	if len(f.npsRunPage.Runs) > 0 || f.npsRunPage.NextBeforeSequence > 0 {
		return f.npsRunPage, nil
	}
	return repo.NPSCampaignRunPage{Runs: f.npsRuns}, nil
}

func (f *fakeSurveyService) NPSCampaignRunEvidence(_ context.Context, _ string, _ uuid.UUID, _ uuid.UUID) (svc.NPSCampaignRunEvidence, error) {
	if f.err != nil {
		return svc.NPSCampaignRunEvidence{}, f.err
	}
	return f.npsEvidence, nil
}

func (f *fakeSurveyService) CreateNPSCampaignRunEvidenceExport(_ context.Context, _ string, _ uuid.UUID, _ uuid.UUID, _, _ string) (repo.NPSCampaignRunEvidenceExport, error) {
	if f.err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, f.err
	}
	return f.npsEvidenceExport, nil
}

func (f *fakeSurveyService) CreateNPSCampaignRunEvidenceExportWithRequestKey(_ context.Context, _ string, _ uuid.UUID, _ uuid.UUID, requestKey uuid.UUID, _, _ string) (repo.NPSCampaignRunEvidenceExport, bool, error) {
	if f.err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, false, f.err
	}
	f.npsEvidenceExportCalls++
	f.npsEvidenceExportRequestKey = requestKey
	return f.npsEvidenceExport, f.npsEvidenceExportReplayed, nil
}

func (f *fakeSurveyService) ListNPSCampaignRunEvidenceExports(_ context.Context, _ string, _ uuid.UUID, _ uuid.UUID, _ int) ([]repo.NPSCampaignRunEvidenceExportSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.npsEvidenceExports, nil
}

func (f *fakeSurveyService) DownloadNPSCampaignRunEvidenceExport(_ context.Context, _ string, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID) (repo.NPSCampaignRunEvidenceExport, error) {
	if f.err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, f.err
	}
	return f.npsEvidenceExport, nil
}

func (f *fakeSurveyService) NPSCampaignPreflight(
	_ context.Context,
	tenantID string,
	campaignID uuid.UUID,
) (svc.NPSCampaignPreflight, error) {
	f.npsPreflightTenantID = tenantID
	f.npsPreflightCampaignID = campaignID
	if f.err != nil {
		return svc.NPSCampaignPreflight{}, f.err
	}
	return f.npsPreflight, nil
}

func (f *fakeSurveyService) ArchiveCampaign(_ context.Context, tenantID string, id uuid.UUID, actorID string) (repo.Campaign, error) {
	f.archiveTenantID = tenantID
	f.archiveCampaignID = id
	f.archiveActorID = actorID
	if f.err != nil {
		return repo.Campaign{}, f.err
	}
	return f.campaign, nil
}

func (f *fakeSurveyService) CreateHostedLink(_ context.Context, in svc.HostedLinkInput) (repo.Invitation, error) {
	f.hostedInput = in
	if f.err != nil {
		return repo.Invitation{}, f.err
	}
	return f.invitation, nil
}

func (f *fakeSurveyService) PreviewRecipients(_ context.Context, in svc.RecipientPreviewInput) (svc.RecipientPreviewResult, error) {
	f.previewInput = in
	if f.err != nil {
		return svc.RecipientPreviewResult{}, f.err
	}
	return f.preview, nil
}

func (f *fakeSurveyService) SendTestEmail(_ context.Context, in svc.TestEmailInput) (svc.TestEmailResult, error) {
	f.testEmailInput = in
	if f.err != nil {
		return svc.TestEmailResult{}, f.err
	}
	return f.testEmailResult, nil
}

func (f *fakeSurveyService) CampaignHealth(
	_ context.Context,
	tenantID string,
	campaignID uuid.UUID,
) (svc.CampaignHealth, error) {
	f.healthTenantID = tenantID
	f.healthCampaignID = campaignID
	if f.err != nil {
		return svc.CampaignHealth{}, f.err
	}
	return f.health, nil
}

func (f *fakeSurveyService) RetryInvitationDelivery(
	_ context.Context,
	tenantID string,
	id uuid.UUID,
	actorID string,
) (repo.Invitation, error) {
	f.retryTenantID = tenantID
	f.retryInvitationID = id
	f.retryActorID = actorID
	if f.err != nil {
		return repo.Invitation{}, f.err
	}
	return f.invitation, nil
}

func (f *fakeSurveyService) RecordProviderEvent(_ context.Context, in svc.ProviderEventInput) (repo.Invitation, error) {
	f.providerInput = in
	if f.err != nil {
		return repo.Invitation{}, f.err
	}
	return f.invitation, nil
}

func (f *fakeSurveyService) ListInvitations(_ context.Context, filter repo.InvitationFilter) ([]repo.Invitation, error) {
	f.invitationFilter = filter
	if f.err != nil {
		return nil, f.err
	}
	return f.invitations, nil
}

func (f *fakeSurveyService) ListResponses(_ context.Context, filter repo.ResponseFilter) ([]repo.Response, error) {
	f.responseFilter = filter
	if f.err != nil {
		return nil, f.err
	}
	return f.responses, nil
}

func (f *fakeSurveyService) Analytics(_ context.Context, filter repo.AnalyticsFilter) (repo.Analytics, error) {
	f.analyticsFilter = filter
	if f.err != nil {
		return repo.Analytics{}, f.err
	}
	return f.analytics, nil
}

func (f *fakeSurveyService) AnalyticsTrend(_ context.Context, filter repo.AnalyticsFilter) ([]repo.AnalyticsTrendBucket, error) {
	f.trendFilter = filter
	if f.err != nil {
		return nil, f.err
	}
	return f.trend, nil
}

func (f *fakeSurveyService) AnalyticsSegments(_ context.Context, filter repo.AnalyticsSegmentFilter) ([]repo.AnalyticsSegment, error) {
	f.segmentFilter = filter
	if f.err != nil {
		return nil, f.err
	}
	return f.segments, nil
}

func (f *fakeSurveyService) AnalyticsInsights(_ context.Context, filter svc.AnalyticsInsightFilter) ([]svc.AnalyticsInsight, error) {
	f.insightFilter = filter
	if f.err != nil {
		return nil, f.err
	}
	return f.insights, nil
}

func (f *fakeSurveyService) UpdateLowScoreReview(_ context.Context, in svc.ReviewInput) (repo.LowScoreReview, error) {
	f.reviewInput = in
	if f.err != nil {
		return repo.LowScoreReview{}, f.err
	}
	return f.review, nil
}

func (f *fakeSurveyService) BatchUpdateLowScoreReviews(_ context.Context, in svc.BatchReviewInput) ([]repo.LowScoreReview, error) {
	f.batchInput = in
	if f.err != nil {
		return nil, f.err
	}
	return f.reviews, nil
}

func (f *fakeSurveyService) AssignLowScoreReviews(_ context.Context, in svc.AssignmentInput) (svc.AssignmentResult, error) {
	f.assignmentInput = in
	if f.err != nil {
		return svc.AssignmentResult{}, f.err
	}
	return f.assignment, nil
}

func (f *fakeSurveyService) EscalateLowScoreReviews(_ context.Context, in svc.EscalationInput) (svc.EscalationResult, error) {
	f.escalationInput = in
	if f.err != nil {
		return svc.EscalationResult{}, f.err
	}
	return f.escalation, nil
}

type fakeSurveyAudit struct {
	events []auditlogsvc.Event
}

func (f *fakeSurveyAudit) Record(_ context.Context, event auditlogsvc.Event) error {
	if event.Action == "" {
		return errors.New("missing audit action")
	}
	f.events = append(f.events, event)
	return nil
}

func surveyHandlerContext() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "user-1",
		}),
	})
}
