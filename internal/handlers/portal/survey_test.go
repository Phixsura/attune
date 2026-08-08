// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	surveyrepo "github.com/Phixsura/attune/internal/repo/survey"
)

func TestPublicSurveyTypeToProto(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  attunev1.SurveyType
	}{
		{name: "CSAT", input: surveyrepo.TypeCSAT, want: attunev1.SurveyType_SURVEY_TYPE_CSAT},
		{name: "CES", input: surveyrepo.TypeCES, want: attunev1.SurveyType_SURVEY_TYPE_CES},
		{name: "NPS", input: surveyrepo.TypeNPS, want: attunev1.SurveyType_SURVEY_TYPE_NPS},
		{name: "unknown", input: "unknown", want: attunev1.SurveyType_SURVEY_TYPE_UNSPECIFIED},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := publicSurveyTypeToProto(tt.input); got != tt.want {
				t.Fatalf("publicSurveyTypeToProto(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestPublicSurveyToProtoCollectsFollowUpConsentOnlyForNPS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		surveyType string
		want       bool
	}{
		{name: "NPS", surveyType: surveyrepo.TypeNPS, want: true},
		{name: "CSAT", surveyType: surveyrepo.TypeCSAT, want: false},
		{name: "CES", surveyType: surveyrepo.TypeCES, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := publicSurveyToProto(surveyrepo.PublicSurvey{
				Campaign:   surveyrepo.Campaign{ID: uuid.New(), SurveyType: tt.surveyType},
				Invitation: surveyrepo.Invitation{ID: uuid.New()},
			})
			if got.CollectsFollowUpConsent != tt.want {
				t.Fatalf("CollectsFollowUpConsent = %t, want %t", got.CollectsFollowUpConsent, tt.want)
			}
		})
	}
}

func TestBindSubmitPublicSurveyResponseRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	payload := `{"comment":"` + strings.Repeat("x", publicSurveySubmissionBodyLimitBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/surveys/token/responses", strings.NewReader(payload))
	err := BindSubmitPublicSurveyResponse(req, ptrext.Of(attunev1.SubmitPublicSurveyResponseRequest{}))
	if !errors.Is(err, dispatcher.ErrBodyTooLarge) {
		t.Fatalf("BindSubmitPublicSurveyResponse() error = %v, want ErrBodyTooLarge", err)
	}
}

func TestBindSubmitPublicSurveyResponsePreservesFollowUpConsent(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/v1/surveys/token/responses", strings.NewReader(`{"followUpConsent":true}`))
	input := ptrext.Of(attunev1.SubmitPublicSurveyResponseRequest{})
	if err := BindSubmitPublicSurveyResponse(req, input); err != nil {
		t.Fatalf("BindSubmitPublicSurveyResponse() error = %v", err)
	}
	if input.FollowUpConsent == nil || !ptrext.Indirect(input.FollowUpConsent) {
		t.Fatalf("FollowUpConsent = %#v, want true", input.FollowUpConsent)
	}
}
