// SPDX-License-Identifier: Apache-2.0

// Package email is the request-notification email delivery adapter. It posts a
// normalized email payload to the tenant-configured provider endpoint.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const channelID = "email"

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
		Metadata: map[string]any{
			"request":   env.Request,
			"update":    env.Update,
			"recipient": env.Recipient,
		},
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

func notificationHTML(env *outbound.NotificationEnvelope) string {
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
