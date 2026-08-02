// SPDX-License-Identifier: Apache-2.0

// Package email is the request-notification email delivery adapter. It posts a
// normalized email payload to the tenant-configured provider endpoint.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	channelID                         = "email"
	maxSurveyScoreLinks               = 20
	surveyInvitationEventType         = "survey.invitation"
	surveyRecoveryEscalationEventType = "survey.recovery_escalation"
)

func init() {
	outbound.Register(ptrext.Of(channel{}))
}

type channel struct{}

func (c *channel) ID() string { return channelID }

func (c *channel) RenderNotification(env *outbound.NotificationEnvelope, dst outbound.Target) (outbound.Rendered, error) {
	messageHeaders := unsubscribeHeaders(headerUnsubscribeURL(env))
	body, err := json.Marshal(emailPayload{
		Version:            env.Version,
		EventID:            env.EventID,
		EventType:          env.EventType,
		TenantID:           env.TenantID,
		FromName:           configString(dst.Config, "from_name"),
		FromEmail:          configString(dst.Config, "from_email"),
		ReplyTo:            configString(dst.Config, "reply_to"),
		ToEmail:            configString(dst.Config, "to_email"),
		Subject:            notificationSubject(env),
		TextBody:           notificationText(env),
		HTMLBody:           notificationHTML(env),
		UnsubscribeURL:     env.UnsubscribeURL,
		ListUnsubscribeURL: env.ListUnsubscribeURL,
		Headers:            messageHeaders,
		Metadata:           notificationMetadata(env),
	})
	if err != nil {
		return outbound.Rendered{}, fmt.Errorf("marshal email notification: %w", err)
	}
	label := fmt.Sprintf("request-notification-email-%s", dst.TenantID)
	return outbound.Rendered{
		Build: func(ctx context.Context) (*http.Request, error) {
			if strings.TrimSpace(dst.URL) == "" {
				return nil, fmt.Errorf("%w: email provider url is empty", outbound.ErrTerminal)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dst.URL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			if dst.Secret != "" {
				req.Header.Set("Authorization", "Bearer "+dst.Secret)
			}
			if env.DeliveryID != "" {
				req.Header.Set("X-Attune-Delivery-Id", env.DeliveryID)
			}
			req.Header.Set("User-Agent", "attune/1.0")
			logext.Infof(ctx, "[outbound.email] request notification req,label:%s,url:%s,body_bytes:%d",
				label, nethardening.RedactURL(dst.URL), len(body))
			return req, nil
		},
		Check: outbound.CheckWebhook(label),
	}, nil
}

type emailPayload struct {
	Version            string         `json:"version"`
	EventID            string         `json:"event_id"`
	EventType          string         `json:"event_type"`
	TenantID           string         `json:"tenant_id"`
	FromName           string         `json:"from_name"`
	FromEmail          string         `json:"from_email"`
	ReplyTo            string         `json:"reply_to,omitempty"`
	ToEmail            string         `json:"to_email"`
	Subject            string         `json:"subject"`
	TextBody           string         `json:"text_body"`
	HTMLBody           string         `json:"html_body"`
	UnsubscribeURL     string         `json:"unsubscribe_url,omitempty"`
	ListUnsubscribeURL string         `json:"list_unsubscribe_url,omitempty"`
	Headers            []emailHeader  `json:"headers,omitempty"`
	Metadata           map[string]any `json:"metadata"`
}

type emailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func configString(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	value, _ := config[key].(string)
	return strings.TrimSpace(value)
}

func notificationSubject(env *outbound.NotificationEnvelope) string {
	if env.EventType == surveyInvitationEventType {
		if title := mapString(env.Survey, "title"); title != "" {
			return title
		}
		return "Resolution feedback"
	}
	if env.EventType == surveyRecoveryEscalationEventType {
		if campaign := mapString(env.Survey, "campaign_name"); campaign != "" {
			return "Low-score recovery: " + campaign
		}
		return "Low-score recovery needs attention"
	}
	title := mapString(env.Request, "title")
	updateTitle := mapString(env.Update, "title")
	switch {
	case updateTitle != "":
		return updateTitle
	case title != "" && env.EventType == "request.shipped":
		return "Shipped: " + title
	case title != "":
		return "Update: " + title
	default:
		return "Request update"
	}
}

func notificationText(env *outbound.NotificationEnvelope) string {
	if env.EventType == surveyInvitationEventType {
		return surveyNotificationText(env)
	}
	if env.EventType == surveyRecoveryEscalationEventType {
		return surveyRecoveryNotificationText(env)
	}
	var b strings.Builder
	if title := mapString(env.Request, "title"); title != "" {
		b.WriteString(title)
		b.WriteString("\n\n")
	}
	if body := mapString(env.Update, "body"); body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	if state := mapString(env.Request, "state"); state != "" {
		b.WriteString("Status: ")
		b.WriteString(state)
		b.WriteString("\n")
	}
	if env.UnsubscribeURL != "" {
		b.WriteString("\nUnsubscribe: ")
		b.WriteString(env.UnsubscribeURL)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func surveyNotificationText(env *outbound.NotificationEnvelope) string {
	var b strings.Builder
	if title := mapString(env.Survey, "title"); title != "" {
		b.WriteString(title)
		b.WriteString("\n\n")
	}
	if intro := mapString(env.Survey, "intro"); intro != "" {
		b.WriteString(intro)
		b.WriteString("\n\n")
	}
	if question := mapString(env.Survey, "question"); question != "" {
		b.WriteString(question)
		b.WriteString("\n\n")
	}
	if requestTitle := mapString(env.Survey, "request_title"); requestTitle != "" {
		b.WriteString("Request: ")
		b.WriteString(requestTitle)
		b.WriteString("\n")
	}
	if publicURL := mapString(env.Survey, "public_url"); publicURL != "" {
		if links := surveyScoreLinks(env.Survey); len(links) > 0 {
			b.WriteString("\nChoose a score:\n")
			for _, link := range links {
				b.WriteString(strconv.Itoa(link.Score))
				b.WriteString(": ")
				b.WriteString(link.URL)
				b.WriteString("\n")
			}
		}
		b.WriteString("\nShare feedback: ")
		b.WriteString(publicURL)
		b.WriteString("\n")
	}
	if expiresAt := mapString(env.Survey, "expires_at"); expiresAt != "" {
		b.WriteString("Expires: ")
		b.WriteString(expiresAt)
		b.WriteString("\n")
	}
	if env.UnsubscribeURL != "" {
		b.WriteString("\nUnsubscribe: ")
		b.WriteString(env.UnsubscribeURL)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func notificationHTML(env *outbound.NotificationEnvelope) string {
	if env.EventType == surveyInvitationEventType {
		return surveyNotificationHTML(env)
	}
	if env.EventType == surveyRecoveryEscalationEventType {
		return surveyRecoveryNotificationHTML(env)
	}
	text := notificationText(env)
	if text == "" {
		return ""
	}
	escaped := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\n", "<br>",
	).Replace(text)
	return "<p>" + escaped + "</p>"
}

func surveyRecoveryNotificationText(env *outbound.NotificationEnvelope) string {
	var b strings.Builder
	b.WriteString("Low-score recovery needs attention\n\n")
	if campaign := mapString(env.Survey, "campaign_name"); campaign != "" {
		b.WriteString("Campaign: ")
		b.WriteString(campaign)
		b.WriteString("\n")
	}
	if score := mapInt(env.Survey, "score"); score > 0 {
		b.WriteString("Score: ")
		b.WriteString(strconv.Itoa(score))
		b.WriteString("\n")
	}
	if severity := mapString(env.Survey, "severity"); severity != "" {
		b.WriteString("Severity: ")
		b.WriteString(severity)
		b.WriteString("\n")
	}
	if reason := mapString(env.Survey, "reason"); reason != "" {
		b.WriteString("Reason: ")
		b.WriteString(reason)
		b.WriteString("\n")
	}
	if dueAt := mapString(env.Survey, "due_at"); dueAt != "" {
		b.WriteString("Due: ")
		b.WriteString(dueAt)
		b.WriteString("\n")
	}
	if sourceType := mapString(env.Survey, "source_type"); sourceType != "" {
		b.WriteString("Source: ")
		b.WriteString(sourceType)
		if sourceID := mapString(env.Survey, "source_id"); sourceID != "" {
			b.WriteString(" / ")
			b.WriteString(sourceID)
		}
		b.WriteString("\n")
	}
	if comment := mapString(env.Survey, "comment"); comment != "" {
		b.WriteString("\nCustomer comment:\n")
		b.WriteString(comment)
		b.WriteString("\n")
	}
	if consoleURL := mapString(env.Survey, "console_url"); consoleURL != "" {
		b.WriteString("\nOpen Attune: ")
		b.WriteString(consoleURL)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func surveyRecoveryNotificationHTML(env *outbound.NotificationEnvelope) string {
	if env == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<div style="font-family:Inter,Arial,sans-serif;color:#13151a;line-height:1.55">`)
	b.WriteString(`<h1 style="font-size:22px;line-height:1.2;margin:0 0 12px">Low-score recovery needs attention</h1>`)
	if campaign := mapString(env.Survey, "campaign_name"); campaign != "" {
		b.WriteString(`<p style="margin:0 0 16px;color:#4b5563">Campaign: `)
		b.WriteString(html.EscapeString(campaign))
		b.WriteString(`</p>`)
	}
	b.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" style="margin:0 0 18px;border-collapse:collapse">`)
	b.WriteString(recoveryRowHTML("Score", recoveryScore(env)))
	b.WriteString(recoveryRowHTML("Severity", mapString(env.Survey, "severity")))
	b.WriteString(recoveryRowHTML("Reason", mapString(env.Survey, "reason")))
	b.WriteString(recoveryRowHTML("Due", mapString(env.Survey, "due_at")))
	b.WriteString(recoveryRowHTML("Source", recoverySource(env)))
	b.WriteString(`</table>`)
	if comment := mapString(env.Survey, "comment"); comment != "" {
		b.WriteString(`<p style="margin:0 0 8px;font-weight:700">Customer comment</p>`)
		b.WriteString(`<p style="margin:0 0 18px;color:#374151">`)
		b.WriteString(html.EscapeString(comment))
		b.WriteString(`</p>`)
	}
	if consoleURL := mapString(env.Survey, "console_url"); consoleURL != "" {
		b.WriteString(`<p style="margin:0"><a href="`)
		b.WriteString(html.EscapeString(consoleURL))
		b.WriteString(`" style="display:inline-block;padding:10px 14px;border-radius:8px;background:#0f766e;color:white;text-decoration:none;font-weight:700">Open Attune</a></p>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func recoveryScore(env *outbound.NotificationEnvelope) string {
	if score := mapInt(env.Survey, "score"); score > 0 {
		return strconv.Itoa(score)
	}
	return ""
}

func recoverySource(env *outbound.NotificationEnvelope) string {
	sourceType := mapString(env.Survey, "source_type")
	sourceID := mapString(env.Survey, "source_id")
	if sourceType == "" {
		return sourceID
	}
	if sourceID == "" {
		return sourceType
	}
	return sourceType + " / " + sourceID
}

func recoveryRowHTML(label string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<tr><td style="padding:3px 18px 3px 0;color:#6b7280">`)
	b.WriteString(html.EscapeString(label))
	b.WriteString(`</td><td style="padding:3px 0;font-weight:700">`)
	b.WriteString(html.EscapeString(value))
	b.WriteString(`</td></tr>`)
	return b.String()
}

func surveyNotificationHTML(env *outbound.NotificationEnvelope) string {
	if env == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<div style="font-family:Inter,Arial,sans-serif;color:#13151a;line-height:1.55">`)
	if title := mapString(env.Survey, "title"); title != "" {
		b.WriteString(`<h1 style="font-size:22px;line-height:1.2;margin:0 0 12px">`)
		b.WriteString(html.EscapeString(title))
		b.WriteString(`</h1>`)
	}
	if intro := mapString(env.Survey, "intro"); intro != "" {
		b.WriteString(`<p style="margin:0 0 16px;color:#4b5563">`)
		b.WriteString(html.EscapeString(intro))
		b.WriteString(`</p>`)
	}
	if question := mapString(env.Survey, "question"); question != "" {
		b.WriteString(`<p style="margin:0 0 14px;font-weight:700">`)
		b.WriteString(html.EscapeString(question))
		b.WriteString(`</p>`)
	}
	if requestTitle := mapString(env.Survey, "request_title"); requestTitle != "" {
		b.WriteString(`<p style="margin:0 0 16px;color:#4b5563">Request: `)
		b.WriteString(html.EscapeString(requestTitle))
		b.WriteString(`</p>`)
	}
	if links := surveyScoreLinks(env.Survey); len(links) > 0 {
		b.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" style="margin:0 0 18px"><tr>`)
		for _, link := range links {
			b.WriteString(`<td style="padding-right:8px"><a href="`)
			b.WriteString(html.EscapeString(link.URL))
			b.WriteString(`" style="display:inline-block;min-width:34px;padding:9px 11px;border:1px solid #0f766e;border-radius:10px;color:#115e59;text-align:center;text-decoration:none;font-weight:700">`)
			b.WriteString(strconv.Itoa(link.Score))
			b.WriteString(`</a></td>`)
		}
		b.WriteString(`</tr></table>`)
	}
	if publicURL := mapString(env.Survey, "public_url"); publicURL != "" {
		b.WriteString(`<p style="margin:0 0 12px"><a href="`)
		b.WriteString(html.EscapeString(publicURL))
		b.WriteString(`" style="color:#115e59;font-weight:700">Share feedback</a></p>`)
	}
	if expiresAt := mapString(env.Survey, "expires_at"); expiresAt != "" {
		b.WriteString(`<p style="margin:0;color:#6b7280;font-size:13px">Expires: `)
		b.WriteString(html.EscapeString(expiresAt))
		b.WriteString(`</p>`)
	}
	if env.UnsubscribeURL != "" {
		b.WriteString(`<p style="margin:16px 0 0;color:#6b7280;font-size:13px"><a href="`)
		b.WriteString(html.EscapeString(env.UnsubscribeURL))
		b.WriteString(`" style="color:#6b7280;text-decoration:underline">Unsubscribe</a></p>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

type surveyScoreLink struct {
	Score int
	URL   string
}

func surveyScoreLinks(values map[string]any) []surveyScoreLink {
	publicURL := mapString(values, "public_url")
	if publicURL == "" {
		return nil
	}
	minScore, maxScore := surveyScoreRange(values)
	if minScore <= 0 || maxScore < minScore {
		return nil
	}
	linkCount := int64(maxScore) - int64(minScore) + 1
	if linkCount <= 0 || linkCount > maxSurveyScoreLinks {
		return nil
	}
	links := make([]surveyScoreLink, 0, int(linkCount))
	for offset := int64(0); offset < linkCount; offset++ {
		score := minScore + int(offset)
		links = append(links, surveyScoreLink{
			Score: score,
			URL:   surveyScoreURL(publicURL, score),
		})
	}
	return links
}

func surveyScoreRange(values map[string]any) (int, int) {
	minScore := mapInt(values, "score_min")
	maxScore := mapInt(values, "score_max")
	if minScore > 0 && maxScore >= minScore {
		return minScore, maxScore
	}
	if mapString(values, "survey_type") == "ces" {
		return 1, 7
	}
	return 1, 5
}

func surveyScoreURL(raw string, score int) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	query := parsed.Query()
	query.Set("score", strconv.Itoa(score))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func notificationMetadata(env *outbound.NotificationEnvelope) map[string]any {
	metadata := map[string]any{}
	addMetadata(metadata, "request", env.Request)
	addMetadata(metadata, "survey", env.Survey)
	addMetadata(metadata, "update", env.Update)
	addMetadata(metadata, "recipient", env.Recipient)
	return metadata
}

func addMetadata(metadata map[string]any, key string, value map[string]any) {
	if len(value) > 0 {
		metadata[key] = value
	}
}

func unsubscribeHeaders(unsubscribeURL string) []emailHeader {
	unsubscribeURL = strings.TrimSpace(unsubscribeURL)
	if unsubscribeURL == "" {
		return nil
	}
	return []emailHeader{
		{Name: "List-Unsubscribe", Value: "<" + unsubscribeURL + ">"},
		{Name: "List-Unsubscribe-Post", Value: "List-Unsubscribe=One-Click"},
	}
}

func headerUnsubscribeURL(env *outbound.NotificationEnvelope) string {
	if env == nil {
		return ""
	}
	if value := strings.TrimSpace(env.ListUnsubscribeURL); value != "" {
		return value
	}
	return strings.TrimSpace(env.UnsubscribeURL)
}

func mapString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func mapInt(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		number, err := value.Int64()
		if err == nil {
			return int(number)
		}
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return number
		}
	}
	return 0
}
