// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/externalsync"
)

type JiraWebhookInput struct {
	TenantID     string
	ConnectionID uuid.UUID
	Signature    string
	Body         []byte
	ReceivedAt   time.Time
}

type jiraWebhookPayload struct {
	EventType       string
	ExternalEventID string
	JSON            string
}

func (s *Service) RecordJiraWebhook(ctx context.Context, in JiraWebhookInput) (*repo.SyncEvent, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Signature = strings.TrimSpace(in.Signature)
	if in.TenantID == "" {
		return nil, normalizeWebhookValidationError("tenant_id is required")
	}
	if in.ConnectionID == uuid.Nil {
		return nil, normalizeWebhookValidationError("connection_id is required")
	}
	conn, err := s.repo.GetConnection(ctx, in.TenantID, in.ConnectionID)
	if err != nil {
		return nil, err
	}
	if conn.Provider != "jira" {
		return nil, normalizeWebhookValidationError("connection provider must be jira")
	}
	secret, err := s.decryptWebhookSecret(ptrext.Indirect(conn))
	if err != nil {
		return nil, err
	}
	payload := normalizeJiraWebhookPayload(in.Body)
	signatureStatus := repo.EventSignatureVerified
	failureReason := ""
	if !verifyJiraSignature(in.Signature, secret, in.Body) {
		signatureStatus = repo.EventSignatureFailed
		failureReason = "jira webhook signature verification failed"
	}
	event, err := s.RecordEvent(ctx, RecordEventInput{
		TenantID:              in.TenantID,
		ConnectionID:          in.ConnectionID,
		EventType:             payload.EventType,
		ExternalEventID:       payload.ExternalEventID,
		SignatureStatus:       signatureStatus,
		PayloadDigest:         eventPayloadDigest(in.Body),
		NormalizedPayloadJSON: payload.JSON,
		FailureReason:         failureReason,
		ReceivedAt:            in.ReceivedAt,
	})
	if err != nil {
		return nil, err
	}
	if signatureStatus == repo.EventSignatureFailed {
		return event, ErrWebhookSignature
	}
	return event, nil
}

func verifyJiraSignature(header string, secret, body []byte) bool {
	return verifyGitHubSignatureSHA256(header, secret, body)
}

func normalizeJiraWebhookPayload(body []byte) jiraWebhookPayload {
	out := map[string]any{
		"provider": "jira",
	}
	eventType := "jira:webhook"
	externalEventID := ""
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil { // ptrext:allow unmarshal-out-param
		out["parse_error"] = "invalid_json"
		return jiraWebhookPayload{
			EventType:       eventType,
			ExternalEventID: externalEventID,
			JSON:            mustMarshalJSONObject(out),
		}
	}
	if raw := strings.TrimSpace(jsonString(payload["webhookEvent"])); raw != "" {
		eventType = raw
	}
	if raw := strings.TrimSpace(jsonString(payload["event"])); raw != "" && eventType == "jira:webhook" {
		eventType = raw
	}
	out["webhook_event"] = eventType
	if ts := payload["timestamp"]; ts != nil {
		out["timestamp"] = ts
	}
	if issue, ok := jsonObject(payload["issue"]); ok {
		out["issue"] = normalizeJiraWebhookIssue(issue)
		externalEventID = jiraWebhookExternalEventID(eventType, issue, payload)
	}
	if changelog, ok := jsonObject(payload["changelog"]); ok {
		out["changelog"] = normalizeJiraWebhookChangelog(changelog)
		if externalEventID == "" {
			externalEventID = jiraWebhookChangeEventID(eventType, payload, changelog)
		}
	}
	if comment, ok := jsonObject(payload["comment"]); ok {
		out["comment"] = normalizeJiraWebhookComment(comment)
		if externalEventID == "" {
			externalEventID = jiraWebhookCommentEventID(eventType, payload, comment)
		}
	}
	if user, ok := jsonObject(payload["user"]); ok {
		out["user"] = pickJSONFields(user, "accountId", "displayName", "emailAddress", "active", "accountType", "timeZone")
	}
	if eventType == "" {
		eventType = "jira:webhook"
	}
	return jiraWebhookPayload{
		EventType:       eventType,
		ExternalEventID: externalEventID,
		JSON:            mustMarshalJSONObject(out),
	}
}

func jiraWebhookExternalEventID(eventType string, issue map[string]any, payload map[string]any) string {
	parts := []string{strings.TrimSpace(eventType)}
	if id := jsonString(issue["id"]); id != "" {
		parts = append(parts, "issue:"+id)
	} else if key := jsonString(issue["key"]); key != "" {
		parts = append(parts, "issue:"+key)
	}
	if changelog, ok := jsonObject(payload["changelog"]); ok {
		if id := jsonString(changelog["id"]); id != "" {
			parts = append(parts, "change:"+id)
		}
	}
	if comment, ok := jsonObject(payload["comment"]); ok {
		if id := jsonString(comment["id"]); id != "" {
			parts = append(parts, "comment:"+id)
		}
	}
	if len(parts) == 1 {
		return ""
	}
	return strings.Join(parts, ":")
}

func jiraWebhookChangeEventID(eventType string, payload map[string]any, changelog map[string]any) string {
	parts := []string{strings.TrimSpace(eventType)}
	if issue, ok := jsonObject(payload["issue"]); ok {
		if id := jsonString(issue["id"]); id != "" {
			parts = append(parts, "issue:"+id)
		}
	}
	if id := jsonString(changelog["id"]); id != "" {
		parts = append(parts, "change:"+id)
	}
	if len(parts) == 1 {
		return ""
	}
	return strings.Join(parts, ":")
}

func jiraWebhookCommentEventID(eventType string, payload map[string]any, comment map[string]any) string {
	parts := []string{strings.TrimSpace(eventType)}
	if issue, ok := jsonObject(payload["issue"]); ok {
		if id := jsonString(issue["id"]); id != "" {
			parts = append(parts, "issue:"+id)
		}
	}
	if id := jsonString(comment["id"]); id != "" {
		parts = append(parts, "comment:"+id)
	}
	if len(parts) == 1 {
		return ""
	}
	return strings.Join(parts, ":")
}

func normalizeJiraWebhookIssue(issue map[string]any) map[string]any {
	out := pickJSONFields(issue, "id", "key", "self")
	if fields, ok := jsonObject(issue["fields"]); ok {
		out["fields"] = pickJSONFields(fields, "summary", "status", "project", "issuetype", "labels", "updated", "created", "resolutiondate")
		if assignee, ok := jsonObject(fields["assignee"]); ok {
			out["assignee"] = pickJSONFields(assignee, "accountId", "displayName", "emailAddress", "active", "accountType")
		}
		if reporter, ok := jsonObject(fields["reporter"]); ok {
			out["reporter"] = pickJSONFields(reporter, "accountId", "displayName", "emailAddress", "active", "accountType")
		}
	}
	return out
}

func normalizeJiraWebhookChangelog(changelog map[string]any) map[string]any {
	out := pickJSONFields(changelog, "id", "created")
	if items, ok := changelog["items"].([]any); ok {
		normalized := make([]any, 0, len(items))
		for _, item := range items {
			if row, ok := jsonObject(item); ok {
				normalized = append(normalized, pickJSONFields(row, "field", "fieldtype", "from", "fromString", "to", "toString"))
			}
		}
		out["items"] = normalized
	}
	return out
}

func normalizeJiraWebhookComment(comment map[string]any) map[string]any {
	out := pickJSONFields(comment, "id", "created", "updated")
	if body, ok := comment["body"]; ok {
		out["body"] = body
	}
	if author, ok := jsonObject(comment["author"]); ok {
		out["author"] = pickJSONFields(author, "accountId", "displayName", "emailAddress", "active", "accountType")
	}
	return out
}

func jsonString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func normalizeWebhookValidationError(message string) error {
	return fmt.Errorf("%w: %s", ErrValidation, strings.TrimSpace(message))
}
