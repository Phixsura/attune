// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	surveyrepo "github.com/Phixsura/attune/internal/repo/survey"
	surveysvc "github.com/Phixsura/attune/internal/service/survey"
)

const surveyPageFormLimitBytes = 64 * 1024

var surveyPageTemplate = template.Must(template.New("survey-page").Parse(`<!doctype html>
<html lang="{{.Locale}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex,nofollow">
  <meta name="color-scheme" content="light">
  <title>{{.Title}} | Attune survey</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f8;
      --panel: #ffffff;
      --text: #13151a;
      --muted: #5c6470;
      --border: #d9dde3;
      --accent: #0f766e;
      --accent-strong: #115e59;
      --accent-soft: #e7f4f2;
      --danger: #b42318;
      --success: #166534;
      --shadow: 0 18px 54px -42px rgba(19, 21, 26, 0.42);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: var(--bg);
      color: var(--text);
    }
    .shell {
      display: grid;
      gap: 20px;
      max-width: 840px;
      margin: 0 auto;
      padding: 32px 20px 48px;
    }
    .hero {
      display: grid;
      gap: 14px;
      padding: 30px 28px;
      border: 1px solid var(--border);
      border-radius: 8px;
      background: var(--panel);
      box-shadow: var(--shadow);
    }
    .eyebrow {
      margin: 0;
      font-size: 12px;
      font-weight: 700;
      letter-spacing: 0;
      text-transform: uppercase;
      color: var(--accent-strong);
    }
    h1 {
      margin: 0;
      font-size: clamp(2rem, 5vw, 3.4rem);
      line-height: 1.04;
      letter-spacing: 0;
    }
    .lede {
      margin: 0;
      max-width: 62ch;
      color: var(--muted);
      font-size: 1rem;
      line-height: 1.7;
    }
    .meta {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
      color: var(--muted);
      font-size: 0.92rem;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 6px 10px;
      border-radius: 8px;
      background: var(--accent-soft);
      color: var(--accent-strong);
      font-size: 12px;
      font-weight: 700;
    }
    .card {
      border: 1px solid var(--border);
      border-radius: 8px;
      background: var(--panel);
      box-shadow: var(--shadow);
      overflow: hidden;
    }
    .card-body {
      display: grid;
      gap: 18px;
      padding: 26px 24px;
    }
    .notice {
      padding: 14px 16px;
      border-radius: 8px;
      border: 1px solid rgba(20, 83, 45, 0.18);
      background: rgba(240, 253, 244, 0.85);
      color: var(--success);
      font-size: 0.95rem;
      line-height: 1.6;
    }
    .notice[data-kind="error"] {
      border-color: rgba(180, 35, 24, 0.18);
      background: rgba(254, 242, 242, 0.9);
      color: var(--danger);
    }
    form {
      display: grid;
      gap: 20px;
    }
    fieldset {
      margin: 0;
      padding: 0;
      border: 0;
      min-inline-size: 0;
    }
    legend {
      margin-bottom: 12px;
      font-size: 1.08rem;
      font-weight: 750;
    }
    .score-grid {
      display: grid;
      gap: 10px;
      grid-template-columns: repeat({{.ScoreColumnCount}}, minmax(0, 1fr));
    }
    .score-option {
      position: relative;
      min-width: 0;
    }
    .score-option input {
      position: absolute;
      inset: 0;
      width: 100%;
      height: 100%;
      opacity: 0;
      cursor: pointer;
    }
    .score-option span {
      display: grid;
      min-height: 58px;
      place-items: center;
      border: 1px solid var(--border);
      border-radius: 8px;
      background: #fff;
      color: var(--text);
      font-size: 1.16rem;
      font-weight: 800;
    }
    .score-option input:focus + span,
    .score-option input:checked + span {
      border-color: rgba(15, 118, 110, 0.42);
      background: rgba(15, 118, 110, 0.1);
      color: var(--accent-strong);
      outline: 2px solid rgba(15, 118, 110, 0.22);
      outline-offset: 2px;
    }
    .scale-labels {
      display: flex;
      justify-content: space-between;
      gap: 14px;
      margin-top: 10px;
      color: var(--muted);
      font-size: 0.88rem;
      line-height: 1.5;
    }
    .field {
      display: grid;
      gap: 8px;
    }
    .field label {
      font-size: 0.95rem;
      font-weight: 650;
    }
    textarea, button {
      font: inherit;
    }
    textarea {
      width: 100%;
      min-height: 150px;
      resize: vertical;
      padding: 13px 14px;
      border-radius: 8px;
      border: 1px solid var(--border);
      background: #fff;
      color: var(--text);
      box-shadow: inset 0 1px 0 rgba(255,255,255,0.7);
    }
    textarea:focus, button:focus {
      outline: 2px solid rgba(15, 118, 110, 0.28);
      outline-offset: 2px;
    }
    .footer {
      display: flex;
      flex-wrap: wrap;
      gap: 14px;
      align-items: center;
      justify-content: space-between;
    }
    .trap {
      position: absolute;
      left: -10000px;
      width: 1px;
      height: 1px;
      overflow: hidden;
    }
    .button {
      appearance: none;
      border: 0;
      border-radius: 8px;
      padding: 14px 20px;
      background: var(--accent);
      color: white;
      cursor: pointer;
      font-weight: 750;
      box-shadow: 0 14px 30px -18px rgba(15, 118, 110, 0.72);
    }
    .hint {
      color: var(--muted);
      font-size: 0.9rem;
      line-height: 1.6;
    }
    .privacy {
      margin: 0;
      color: var(--muted);
      font-size: 0.86rem;
      line-height: 1.6;
    }
    .privacy a {
      color: var(--accent-strong);
      text-decoration: underline;
      text-underline-offset: 3px;
    }
    @media (max-width: 760px) {
      .shell { padding-inline: 16px; }
      .hero, .card-body { padding-inline: 18px; }
      .score-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .footer { align-items: stretch; }
      .button { width: 100%; }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="hero">
      <p class="eyebrow">{{.SurveyTypeLabel}}</p>
      <h1>{{.Title}}</h1>
      {{if .Intro}}<p class="lede">{{.Intro}}</p>{{end}}
      <div class="meta">
        <span class="pill">{{.ScaleLabel}}</span>
        {{if .ExpiresAtLabel}}<span class="pill">Open until {{.ExpiresAtLabel}}</span>{{end}}
      </div>
    </section>

    <section class="card">
      <div class="card-body">
        {{if .Message}}<div class="notice" data-kind="{{.MessageKind}}" role="status">{{.Message}}</div>{{end}}

        {{if .CanSubmit}}
        <form method="post" action="{{.SubmitURL}}">
          <input type="hidden" name="locale" value="{{.Locale}}">
          <div class="trap" aria-hidden="true">
            <label for="survey-company-website">Company website</label>
            <input id="survey-company-website" type="text" name="company_website" tabindex="-1" autocomplete="off">
          </div>
          <fieldset>
            <legend>{{.Question}}</legend>
            <div class="score-grid">
              {{range .ScoreOptions}}
              <label class="score-option">
                <input type="radio" name="score" value="{{.Value}}" required aria-label="Score {{.Value}}" {{if .Checked}}checked{{end}}>
                <span>{{.Value}}</span>
              </label>
              {{end}}
            </div>
            <div class="scale-labels">
              <span>{{.ScaleLowLabel}}</span>
              <span>{{.ScaleHighLabel}}</span>
            </div>
          </fieldset>

          <div class="field">
            <label for="survey-comment">{{.CommentPrompt}}</label>
            <textarea id="survey-comment" name="comment" maxlength="5000"></textarea>
          </div>

          <div class="footer">
            <p class="hint">Your response is tied to this invitation link.</p>
            <button class="button" type="submit">Submit feedback</button>
          </div>
        </form>
        {{else if .ThankYou}}
        <p class="hint">{{.ThankYou}}</p>
        {{end}}
        {{if .UnsubscribeURL}}
        <p class="privacy"><a href="{{.UnsubscribeURL}}" rel="nofollow">Unsubscribe from future survey emails</a></p>
        {{end}}
      </div>
    </section>
  </main>
</body>
</html>`))

type surveyPageData struct {
	Title            string
	Intro            string
	Question         string
	CommentPrompt    string
	ThankYou         string
	Locale           string
	SubmitURL        string
	SurveyTypeLabel  string
	ScaleLabel       string
	ScaleLowLabel    string
	ScaleHighLabel   string
	ScoreColumnCount int
	ScoreOptions     []surveyPageScoreOption
	ExpiresAtLabel   string
	UnsubscribeURL   string
	CanSubmit        bool
	Message          string
	MessageKind      string
}

type surveyPageScoreOption struct {
	Value   int
	Checked bool
}

type surveyPageSubmission struct {
	Score   int
	Comment string
	Locale  string
}

func (h *Handler) SurveyPage(w http.ResponseWriter, r *http.Request) {
	setSurveyPageHeaders(w)
	if h.surveys == nil {
		renderSurveyPageError(w, http.StatusNotImplemented, "Survey is not configured.")
		return
	}
	data, status, err := h.surveyPageData(r.Context(), surveyTokenFromRequest(r), surveyScoreQuery(r))
	if err != nil {
		renderSurveyPageError(w, status, surveyPageErrorMessage(err))
		return
	}
	if err := surveyPageExecuteTemplate(w, http.StatusOK, data); err != nil {
		http.Error(w, "survey render failed", http.StatusInternalServerError)
	}
}

func (h *Handler) SubmitSurveyPageResponse(w http.ResponseWriter, r *http.Request) {
	setSurveyPageHeaders(w)
	if h.surveys == nil {
		renderSurveyPageError(w, http.StatusNotImplemented, "Survey is not configured.")
		return
	}
	token := surveyTokenFromRequest(r)
	data, status, err := h.surveyPageData(r.Context(), token, 0)
	if err != nil {
		renderSurveyPageError(w, status, surveyPageErrorMessage(err))
		return
	}
	if !data.CanSubmit {
		renderSurveyPageMessage(w, http.StatusOK, data, "success", "This survey has already been submitted.")
		return
	}
	submission, ok := surveyPageSubmissionFromRequest(w, r, data)
	if !ok {
		return
	}
	data = surveyPageDataWithSelectedScore(data, submission.Score)
	_, lowScore, thankYou, err := h.surveys.SubmitPublicResponse(r.Context(), surveysvc.PublicSubmitInput{
		Token:         token,
		Score:         submission.Score,
		Comment:       submission.Comment,
		Locale:        submission.Locale,
		UserAgentHash: surveysvc.HashValue(userAgentFromRequest(r)),
		IPHash:        surveysvc.HashValue(r.RemoteAddr),
	})
	if err != nil {
		h.renderSurveyPageSubmitError(r.Context(), w, token, data, err)
		return
	}
	renderSurveyPageSuccess(w, data, lowScore, thankYou)
}

func surveyPageSubmissionFromRequest(
	w http.ResponseWriter,
	r *http.Request,
	data surveyPageData,
) (surveyPageSubmission, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, surveyPageFormLimitBytes)
	if err := r.ParseForm(); err != nil {
		renderSurveyPageMessage(w, http.StatusBadRequest, data, "error", "The survey response could not be read.")
		return surveyPageSubmission{}, false
	}
	if surveyPageHoneypotFilled(r) {
		renderSurveyPageSuccess(w, data, false, data.ThankYou)
		return surveyPageSubmission{}, false
	}
	score, message, ok := surveyPageScoreFromRequest(r, data)
	if !ok {
		renderSurveyPageMessage(w, http.StatusBadRequest, data, "error", message)
		return surveyPageSubmission{}, false
	}
	return surveyPageSubmission{
		Score:   score,
		Comment: r.FormValue("comment"),
		Locale:  r.FormValue("locale"),
	}, true
}

func surveyPageScoreFromRequest(r *http.Request, data surveyPageData) (int, string, bool) {
	score, err := strconv.Atoi(strings.TrimSpace(r.FormValue("score")))
	if err != nil {
		return 0, "Choose a score before submitting.", false
	}
	if !surveyPageScoreAllowed(data.ScoreOptions, score) {
		return 0, "Choose a score from the survey scale.", false
	}
	return score, "", true
}

func renderSurveyPageSuccess(w http.ResponseWriter, data surveyPageData, lowScore bool, thankYou string) {
	data.CanSubmit = false
	data.ThankYou = strings.TrimSpace(thankYou)
	if data.ThankYou == "" {
		data.ThankYou = "Thanks for your feedback."
	}
	data.Message = data.ThankYou
	if lowScore {
		data.Message = data.ThankYou + " Your response has been flagged for review."
	}
	data.MessageKind = "success"
	if err := surveyPageExecuteTemplate(w, http.StatusOK, data); err != nil {
		http.Error(w, "survey render failed", http.StatusInternalServerError)
	}
}

func surveyPageHoneypotFilled(r *http.Request) bool {
	return strings.TrimSpace(r.FormValue("company_website")) != ""
}

func (h *Handler) renderSurveyPageSubmitError(
	ctx context.Context,
	w http.ResponseWriter,
	token string,
	data surveyPageData,
	err error,
) {
	if errors.Is(err, surveysvc.ErrConflict) {
		if refreshed, _, loadErr := h.surveyPageData(ctx, token, 0); loadErr == nil {
			data = refreshed
		}
		renderSurveyPageMessage(w, http.StatusOK, data, "success", "This survey has already been submitted.")
		return
	}
	renderSurveyPageMessage(w, surveyPageStatus(err), data, "error", surveyPageErrorMessage(err))
}

func renderSurveyPageMessage(w http.ResponseWriter, status int, data surveyPageData, kind string, message string) {
	data.Message = message
	data.MessageKind = kind
	if kind == "success" {
		data.CanSubmit = false
	}
	if err := surveyPageExecuteTemplate(w, status, data); err != nil {
		http.Error(w, "survey render failed", http.StatusInternalServerError)
	}
}

func (h *Handler) surveyPageData(ctx context.Context, token string, selectedScore int) (surveyPageData, int, error) {
	result, err := h.surveys.GetPublicSurvey(ctx, token)
	if err != nil {
		return surveyPageData{}, surveyPageStatus(err), err
	}
	return surveyPageDataFromPublicSurvey(token, result, selectedScore), http.StatusOK, nil
}

func surveyPageDataFromPublicSurvey(token string, result surveyrepo.PublicSurvey, selectedScore int) surveyPageData {
	minScore, maxScore := surveysvc.ScoreRange(result.Campaign.SurveyType)
	options := make([]surveyPageScoreOption, 0, maxScore-minScore+1)
	for score := minScore; score <= maxScore; score++ {
		options = append(options, surveyPageScoreOption{Value: score, Checked: score == selectedScore})
	}
	title := publicSurveyText(result.Campaign.Content, "title")
	if strings.TrimSpace(title) == "" {
		title = "Resolution feedback"
	}
	intro := publicSurveyText(result.Campaign.Content, "intro")
	question := publicSurveyText(result.Campaign.Content, "question")
	if strings.TrimSpace(question) == "" {
		question = "How was your experience?"
	}
	commentPrompt := publicSurveyText(result.Campaign.Content, "comment_prompt")
	if strings.TrimSpace(commentPrompt) == "" {
		commentPrompt = "What could we improve?"
	}
	thankYou := publicSurveyText(result.Campaign.Content, "thank_you")
	if strings.TrimSpace(thankYou) == "" {
		thankYou = "Thanks for your feedback."
	}
	responseStatus := result.Invitation.ResponseStatus
	if result.Response != nil {
		responseStatus = surveyrepo.ResponseCompleted
	}
	data := surveyPageData{
		Title:            title,
		Intro:            intro,
		Question:         question,
		CommentPrompt:    commentPrompt,
		ThankYou:         thankYou,
		Locale:           strings.TrimSpace(result.Campaign.Locale),
		SubmitURL:        "/surveys/" + url.PathEscape(token) + "/responses",
		SurveyTypeLabel:  surveyPageTypeLabel(result.Campaign.SurveyType),
		ScaleLabel:       strconv.Itoa(minScore) + "-" + strconv.Itoa(maxScore),
		ScaleLowLabel:    surveyPageLowLabel(result.Campaign.SurveyType),
		ScaleHighLabel:   surveyPageHighLabel(result.Campaign.SurveyType),
		ScoreColumnCount: maxScore - minScore + 1,
		ScoreOptions:     options,
		ExpiresAtLabel:   surveyPageDateLabel(result.Invitation.ExpiresAt),
		UnsubscribeURL:   strings.TrimSpace(result.UnsubscribeURL),
		CanSubmit:        responseStatus != surveyrepo.ResponseCompleted,
	}
	if strings.TrimSpace(data.Locale) == "" {
		data.Locale = "en"
	}
	if !data.CanSubmit {
		data.Message = "This survey has already been submitted."
		data.MessageKind = "success"
	}
	return data
}

func setSurveyPageHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", publicRequestCacheControl)
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=()")
	w.Header().Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"style-src 'unsafe-inline'",
		"img-src 'self' data:",
	}, "; "))
}

func surveyPageExecuteTemplate(w http.ResponseWriter, status int, data surveyPageData) error {
	buf := ptrext.Of(bytes.Buffer{})
	if err := surveyPageTemplate.Execute(buf, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
	return nil
}

func renderSurveyPageError(w http.ResponseWriter, status int, message string) {
	data := surveyPageData{
		Title:            "Survey unavailable",
		Locale:           "en",
		SurveyTypeLabel:  "Feedback survey",
		ScaleLabel:       "Private link",
		ScoreColumnCount: 1,
		Message:          message,
		MessageKind:      "error",
	}
	if err := surveyPageExecuteTemplate(w, status, data); err != nil {
		http.Error(w, "survey render failed", http.StatusInternalServerError)
	}
}

func surveyTokenFromRequest(r *http.Request) string {
	return strings.TrimSpace(chi.URLParam(r, "token"))
}

func surveyScoreQuery(r *http.Request) int {
	if r == nil {
		return 0
	}
	score, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("score")))
	if err != nil {
		return 0
	}
	return score
}

func surveyPageStatus(err error) int {
	switch {
	case errors.Is(err, surveysvc.ErrValidation):
		return http.StatusBadRequest
	case errors.Is(err, surveysvc.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, surveysvc.ErrExpired):
		return http.StatusGone
	case errors.Is(err, surveysvc.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, surveysvc.ErrDisabled):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func surveyPageErrorMessage(err error) string {
	switch {
	case errors.Is(err, surveysvc.ErrValidation):
		return "This survey link is invalid."
	case errors.Is(err, surveysvc.ErrConflict):
		return "This survey has already been submitted."
	case errors.Is(err, surveysvc.ErrNotFound), errors.Is(err, surveysvc.ErrExpired), errors.Is(err, surveysvc.ErrDisabled):
		return "This survey is no longer available."
	default:
		return "Survey is unavailable."
	}
}

func surveyPageTypeLabel(value string) string {
	if value == surveyrepo.TypeCES {
		return "Effort survey"
	}
	return "Satisfaction survey"
}

func surveyPageLowLabel(value string) string {
	if value == surveyrepo.TypeCES {
		return "Very difficult"
	}
	return "Very dissatisfied"
}

func surveyPageHighLabel(value string) string {
	if value == surveyrepo.TypeCES {
		return "Very easy"
	}
	return "Very satisfied"
}

func surveyPageDateLabel(t *time.Time) string {
	if t == nil {
		return ""
	}
	return ptrext.Indirect(t).UTC().Format("Jan 2, 2006")
}

func surveyPageScoreAllowed(options []surveyPageScoreOption, score int) bool {
	for _, option := range options {
		if option.Value == score {
			return true
		}
	}
	return false
}

func surveyPageDataWithSelectedScore(data surveyPageData, score int) surveyPageData {
	for idx := range data.ScoreOptions {
		data.ScoreOptions[idx].Checked = data.ScoreOptions[idx].Value == score
	}
	return data
}
