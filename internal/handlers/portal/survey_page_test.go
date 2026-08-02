// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	surveyrepo "github.com/Phixsura/attune/internal/repo/survey"
	surveysvc "github.com/Phixsura/attune/internal/service/survey"
)

func TestSurveyPageRendersSurveyForm(t *testing.T) {
	t.Parallel()

	service := ptrext.Of(fakeSurveyService{publicSurvey: surveyPageSurveyFixture(nil)})
	handler := NewHandler(nil, nil, testVisitorSecrets())
	handler.SetSurveyService(service)

	rec := httptest.NewRecorder()
	req := requestWithSurveyToken(http.MethodGet, "/surveys/token-1", "token-1", nil)
	handler.SurveyPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	requireSurveyPageSecurityHeaders(t, rec)
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("Content-Type = %q, want html", got)
	}

	body := rec.Body.String()
	for _, want := range []string{
		`<html lang="en">`,
		"<title>Resolution &lt;feedback&gt; | Attune survey</title>",
		"Satisfaction survey",
		"Your feedback helps us improve.",
		"How satisfied are you?",
		`action="/surveys/token-1/responses"`,
		`name="company_website"`,
		`name="score" value="1"`,
		`name="score" value="5"`,
		"Very dissatisfied",
		"Very satisfied",
		"Tell us more",
		"Submit feedback",
		`href="https://example.test/v1/portal/acme/unsubscribe?token=unsubscribe-token" rel="nofollow"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
	if service.gotToken != "token-1" {
		t.Fatalf("GetPublicSurvey token = %q, want token-1", service.gotToken)
	}
}

func requireSurveyPageSecurityHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Cache-Control"); got != publicRequestCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicRequestCacheControl)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Fatalf("X-Robots-Tag = %q, want noindex, nofollow", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Permissions-Policy"); !strings.Contains(got, "geolocation=()") {
		t.Fatalf("Permissions-Policy = %q, want geolocation disabled", got)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "form-action 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("Content-Security-Policy = %q, missing %q", csp, want)
		}
	}
}

func TestSurveyPagePreselectsScoreQueryWithoutSubmitting(t *testing.T) {
	t.Parallel()

	service := ptrext.Of(fakeSurveyService{publicSurvey: surveyPageSurveyFixture(nil)})
	handler := NewHandler(nil, nil, testVisitorSecrets())
	handler.SetSurveyService(service)

	rec := httptest.NewRecorder()
	req := requestWithSurveyToken(http.MethodGet, "/surveys/token-1?score=3", "token-1", nil)
	handler.SurveyPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if service.gotSubmit.Token != "" {
		t.Fatalf("SubmitPublicResponse was called on GET: %#v", service.gotSubmit)
	}
	if body := rec.Body.String(); !strings.Contains(body, `value="3" required aria-label="Score 3" checked`) {
		t.Fatalf("body missing selected score: %s", body)
	}
}

func TestSurveyPageRendersCompletedState(t *testing.T) {
	t.Parallel()

	response := surveyrepo.Response{ID: uuid.New(), Score: 5}
	handler := NewHandler(nil, nil, testVisitorSecrets())
	handler.SetSurveyService(ptrext.Of(fakeSurveyService{
		publicSurvey: surveyPageSurveyFixture(ptrext.Of(response)),
	}))

	rec := httptest.NewRecorder()
	req := requestWithSurveyToken(http.MethodGet, "/surveys/token-1", "token-1", nil)
	handler.SurveyPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "This survey has already been submitted.") {
		t.Fatalf("body missing completed message: %s", body)
	}
	if strings.Contains(body, "Submit feedback") {
		t.Fatalf("body rendered submit button for completed survey: %s", body)
	}
}

func TestSubmitSurveyPageResponseSubmitsForm(t *testing.T) {
	t.Parallel()

	responseID := uuid.New()
	service := ptrext.Of(fakeSurveyService{
		publicSurvey: surveyPageSurveyFixture(nil),
		submitResponse: surveyrepo.Response{
			ID: responseID,
		},
		submitLowScore: true,
		submitThankYou: "Thanks for your feedback.",
	})
	handler := NewHandler(nil, nil, testVisitorSecrets())
	handler.SetSurveyService(service)

	form := url.Values{}
	form.Set("score", "2")
	form.Set("comment", "The fix was useful but hard to find.")
	form.Set("locale", "en-US")
	rec := httptest.NewRecorder()
	req := requestWithSurveyToken(http.MethodPost, "/surveys/token-1/responses", "token-1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "survey-page-test")
	req.RemoteAddr = "203.0.113.15:49152"
	handler.SubmitSurveyPageResponse(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	got := service.gotSubmit
	if got.Token != "token-1" || got.Score != 2 || got.Comment != "The fix was useful but hard to find." || got.Locale != "en-US" {
		t.Fatalf("submit input = %#v", got)
	}
	if got.UserAgentHash == "" || got.IPHash == "" {
		t.Fatalf("submit hashes = user agent %q ip %q", got.UserAgentHash, got.IPHash)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Your response has been flagged for review.") {
		t.Fatalf("body missing low-score review message: %s", body)
	}
	if strings.Contains(body, "Submit feedback") {
		t.Fatalf("body rendered submit button after success: %s", body)
	}
}

func TestSubmitSurveyPageResponseDropsHoneypotSubmission(t *testing.T) {
	t.Parallel()

	service := ptrext.Of(fakeSurveyService{publicSurvey: surveyPageSurveyFixture(nil)})
	handler := NewHandler(nil, nil, testVisitorSecrets())
	handler.SetSurveyService(service)

	form := url.Values{}
	form.Set("score", "5")
	form.Set("comment", "Looks fine.")
	form.Set("locale", "en-US")
	form.Set("company_website", "https://bot.example")
	rec := httptest.NewRecorder()
	req := requestWithSurveyToken(http.MethodPost, "/surveys/token-1/responses", "token-1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.SubmitSurveyPageResponse(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if service.gotSubmit.Token != "" {
		t.Fatalf("SubmitPublicResponse was called for honeypot submission: %#v", service.gotSubmit)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Thanks for your feedback.") ||
		!strings.Contains(body, `role="status"`) ||
		strings.Contains(body, "Submit feedback") {
		t.Fatalf("body = %s", body)
	}
}

func TestSubmitSurveyPageResponseRejectsMissingScore(t *testing.T) {
	t.Parallel()

	service := ptrext.Of(fakeSurveyService{publicSurvey: surveyPageSurveyFixture(nil)})
	handler := NewHandler(nil, nil, testVisitorSecrets())
	handler.SetSurveyService(service)

	form := url.Values{}
	form.Set("comment", "No score")
	rec := httptest.NewRecorder()
	req := requestWithSurveyToken(http.MethodPost, "/surveys/token-1/responses", "token-1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.SubmitSurveyPageResponse(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if service.gotSubmit.Token != "" {
		t.Fatalf("SubmitPublicResponse was called: %#v", service.gotSubmit)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Choose a score before submitting.") {
		t.Fatalf("body missing validation message: %s", body)
	}
}

func TestSubmitSurveyPageResponseRejectsOutOfRangeScore(t *testing.T) {
	t.Parallel()

	service := ptrext.Of(fakeSurveyService{publicSurvey: surveyPageSurveyFixture(nil)})
	handler := NewHandler(nil, nil, testVisitorSecrets())
	handler.SetSurveyService(service)

	form := url.Values{}
	form.Set("score", "9")
	rec := httptest.NewRecorder()
	req := requestWithSurveyToken(http.MethodPost, "/surveys/token-1/responses", "token-1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.SubmitSurveyPageResponse(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if service.gotSubmit.Token != "" {
		t.Fatalf("SubmitPublicResponse was called: %#v", service.gotSubmit)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Choose a score from the survey scale.") {
		t.Fatalf("body missing validation message: %s", body)
	}
}

func TestSurveyPageReturnsUnavailableForExpiredSurvey(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil, testVisitorSecrets())
	handler.SetSurveyService(ptrext.Of(fakeSurveyService{publicErr: surveysvc.ErrExpired}))

	rec := httptest.NewRecorder()
	req := requestWithSurveyToken(http.MethodGet, "/surveys/token-1", "token-1", nil)
	handler.SurveyPage(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusGone, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "This survey is no longer available.") {
		t.Fatalf("body missing unavailable message: %s", body)
	}
}

func requestWithSurveyToken(method, target, token string, body io.Reader) *http.Request {
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, body)
	}
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("token", token)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func surveyPageSurveyFixture(response *surveyrepo.Response) surveyrepo.PublicSurvey {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	return surveyrepo.PublicSurvey{
		Campaign: surveyrepo.Campaign{
			ID:         uuid.New(),
			TenantID:   "tenant-1",
			SurveyType: surveyrepo.TypeCSAT,
			Status:     surveyrepo.StatusActive,
			Content: map[string]any{
				"title":          "Resolution <feedback>",
				"intro":          "Your feedback helps us improve.",
				"question":       "How satisfied are you?",
				"comment_prompt": "Tell us more",
				"thank_you":      "Thanks for your feedback.",
			},
			Locale:            "en",
			LowScoreThreshold: 3,
		},
		Invitation: surveyrepo.Invitation{
			ID:                uuid.New(),
			TenantID:          "tenant-1",
			CampaignID:        uuid.New(),
			ResponseStatus:    surveyrepo.ResponseNotStarted,
			SuppressionStatus: surveyrepo.SuppressionNotSuppressed,
			ExpiresAt:         ptrext.Of(now.Add(24 * time.Hour)),
		},
		Response:       response,
		UnsubscribeURL: "https://example.test/v1/portal/acme/unsubscribe?token=unsubscribe-token",
	}
}

type fakeSurveyService struct {
	publicSurvey   surveyrepo.PublicSurvey
	publicErr      error
	submitResponse surveyrepo.Response
	submitLowScore bool
	submitThankYou string
	submitErr      error
	gotToken       string
	gotSubmit      surveysvc.PublicSubmitInput
}

func (f *fakeSurveyService) GetPublicSurvey(_ context.Context, token string) (surveyrepo.PublicSurvey, error) {
	f.gotToken = token
	return f.publicSurvey, f.publicErr
}

func (f *fakeSurveyService) SubmitPublicResponse(_ context.Context, in surveysvc.PublicSubmitInput) (surveyrepo.Response, bool, string, error) {
	f.gotSubmit = in
	if f.submitErr != nil {
		return surveyrepo.Response{}, false, "", f.submitErr
	}
	if f.submitResponse.ID == uuid.Nil {
		f.submitResponse.ID = uuid.New()
	}
	thankYou := strings.TrimSpace(f.submitThankYou)
	if thankYou == "" {
		thankYou = "Thanks for your feedback."
	}
	return f.submitResponse, f.submitLowScore, thankYou, nil
}

var _ surveyService = ptrext.Of(fakeSurveyService{})
