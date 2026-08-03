// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	surveyrepo "github.com/Phixsura/attune/internal/repo/survey"
	surveysvc "github.com/Phixsura/attune/internal/service/survey"
)

func BindGetPublicSurvey(r *http.Request, req *attunev1.GetPublicSurveyRequest) error {
	req.Token = strings.TrimSpace(chi.URLParam(r, "token"))
	return nil
}

func BindSubmitPublicSurveyResponse(r *http.Request, req *attunev1.SubmitPublicSurveyResponseRequest) error {
	if err := dispatcher.JSONBody(r, req); err != nil {
		return err
	}
	req.Token = strings.TrimSpace(chi.URLParam(r, "token"))
	return nil
}

func setSurveyHeaders(ctx *dispatcher.RequestContext[struct{}]) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	ctx.SetHeader("X-Robots-Tag", "noindex, nofollow")
	ctx.SetHeader("Referrer-Policy", "no-referrer")
}

func publicSurveyToProto(result surveyrepo.PublicSurvey) *attunev1.PublicSurvey {
	minScore, maxScore := surveysvc.ScoreRange(result.Campaign.SurveyType)
	responseStatus := result.Invitation.ResponseStatus
	if result.Response != nil {
		responseStatus = surveyrepo.ResponseCompleted
	}
	return ptrext.Of(attunev1.PublicSurvey{
		CampaignId:     result.Campaign.ID.String(),
		InvitationId:   result.Invitation.ID.String(),
		SurveyType:     publicSurveyTypeToProto(result.Campaign.SurveyType),
		Title:          publicSurveyText(result.Campaign.Content, "title"),
		Intro:          publicSurveyText(result.Campaign.Content, "intro"),
		Question:       publicSurveyText(result.Campaign.Content, "question"),
		CommentPrompt:  publicSurveyText(result.Campaign.Content, "comment_prompt"),
		ThankYou:       publicSurveyText(result.Campaign.Content, "thank_you"),
		Locale:         result.Campaign.Locale,
		MinScore:       int32(minScore),
		MaxScore:       int32(maxScore),
		ExpiresAt:      optionalPortalTimeString(result.Invitation.ExpiresAt),
		ResponseStatus: publicResponseStatusToProto(responseStatus),
		UnsubscribeUrl: optionalPortalString(result.UnsubscribeURL),
	})
}

func publicSurveyText(content map[string]any, key string) string {
	value, ok := content[key].(string)
	if !ok {
		return ""
	}
	return value
}

func publicSurveyTypeToProto(value string) attunev1.SurveyType {
	switch value {
	case surveyrepo.TypeCSAT:
		return attunev1.SurveyType_SURVEY_TYPE_CSAT
	case surveyrepo.TypeCES:
		return attunev1.SurveyType_SURVEY_TYPE_CES
	default:
		return attunev1.SurveyType_SURVEY_TYPE_UNSPECIFIED
	}
}

func publicResponseStatusToProto(value string) attunev1.SurveyResponseStatus {
	switch value {
	case surveyrepo.ResponseNotStarted:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_NOT_STARTED
	case surveyrepo.ResponseOpened:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_OPENED
	case surveyrepo.ResponseStarted:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_STARTED
	case surveyrepo.ResponseCompleted:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_COMPLETED
	case surveyrepo.ResponseExpired:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_EXPIRED
	default:
		return attunev1.SurveyResponseStatus_SURVEY_RESPONSE_STATUS_UNSPECIFIED
	}
}

func optionalPortalTimeString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return ptrext.Of(ptrext.Indirect(t).UTC().Format(time.RFC3339Nano))
}

func optionalPortalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return ptrext.Of(value)
}

func portalSurveyError[Resp proto.Message](err error) (dispatcher.Result[Resp], error) {
	switch {
	case errors.Is(err, surveysvc.ErrValidation):
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid survey")
	case errors.Is(err, surveysvc.ErrNotFound):
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "survey not found")
	case errors.Is(err, surveysvc.ErrExpired):
		return dispatcher.Fail[Resp](http.StatusGone, attunev1.ErrorCode_NOT_FOUND, "survey not found")
	case errors.Is(err, surveysvc.ErrConflict):
		return dispatcher.Fail[Resp](http.StatusConflict, attunev1.ErrorCode_CONFLICT, "survey already submitted")
	case errors.Is(err, surveysvc.ErrDisabled):
		return dispatcher.Fail[Resp](http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "survey unavailable")
	default:
		return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "survey failed")
	}
}
