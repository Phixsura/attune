// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/externalsync"
)

func TestRecordJiraWebhookVerifiesSignatureAndStoresNormalizedPayload(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = jiraWebhookTestConnection(connectionID, "tenant-1", "jira", true)

	secret := []byte("webhook-secret-123")
	store := ptrext.Of(fakeSecretStore{decryptPlaintext: secret})
	service := New(repository, store)
	body := []byte(`{"webhookEvent":"jira:issue_updated","timestamp":1710000000000,"issue":{"id":"10001","key":"ACME-1","fields":{"summary":"Sync me","status":{"name":"In Progress","statusCategory":{"key":"indeterminate","name":"In Progress"}},"labels":["bug"]}},"changelog":{"id":"200","items":[{"field":"status","fromString":"To Do","toString":"In Progress"}]},"comment":{"id":"300","created":"2026-07-18T00:00:00.000+0000","updated":"2026-07-18T00:10:00.000+0000","body":"Looks good","author":{"accountId":"acc-1","displayName":"Alice","emailAddress":"alice@example.com"}},"user":{"accountId":"acc-1","displayName":"Alice","emailAddress":"alice@example.com"}}`)
	receivedAt := time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC)

	event, err := service.RecordJiraWebhook(context.Background(), JiraWebhookInput{
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		Signature:    githubSignature(secret, body),
		Body:         body,
		ReceivedAt:   receivedAt,
	})
	if err != nil {
		t.Fatalf("RecordJiraWebhook returned error: %v", err)
	}

	assertRecordedJiraWebhookEvent(t, event, receivedAt)
	assertRecordedJiraWebhookPayloadDigest(t, event.PayloadDigest, body)
	assertRecordedJiraWebhookAAD(t, store.decryptAAD, connectionID)
	assertRecordedJiraWebhookNormalizedPayload(t, event.NormalizedPayload)
}

func TestRecordJiraWebhookRecordsFailedSignature(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = jiraWebhookTestConnection(connectionID, "tenant-1", "jira", true)
	service := New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("webhook-secret-123")}))
	body := []byte(`{"webhookEvent":"jira:issue_created","issue":{"id":"10002","key":"ACME-2"}}`)

	event, err := service.RecordJiraWebhook(context.Background(), JiraWebhookInput{
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		Signature:    "sha256=bad",
		Body:         body,
		ReceivedAt:   time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC),
	})
	if !errors.Is(err, ErrWebhookSignature) {
		t.Fatalf("RecordJiraWebhook error = %v; want ErrWebhookSignature", err)
	}
	if event == nil || event.Provider != "jira" || event.EventType != "jira:issue_created" ||
		event.ExternalEventID != "jira:issue_created:issue:10002" ||
		event.SignatureStatus != repo.EventSignatureFailed ||
		event.Status != repo.EventStatusFailed ||
		event.FailureReason != "jira webhook signature verification failed" {
		t.Fatalf("event = %#v; want failed jira webhook event", event)
	}
	if event.PayloadDigest != eventPayloadDigest(body) {
		t.Fatalf("payload digest = %q; want raw body digest %q", event.PayloadDigest, eventPayloadDigest(body))
	}
}

func TestRecordJiraWebhookValidationBranches(t *testing.T) {
	body := []byte(`{"webhookEvent":"jira:issue_created"}`)
	secret := []byte("webhook-secret-123")
	connectionID := uuid.New()

	t.Run("missing tenant", func(t *testing.T) {
		repository := newFakeRepo()
		repository.connections[connectionID] = jiraWebhookTestConnection(connectionID, "tenant-1", "jira", true)
		service := New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: secret}))

		_, err := service.RecordJiraWebhook(context.Background(), JiraWebhookInput{
			ConnectionID: connectionID,
			Signature:    githubSignature(secret, body),
			Body:         body,
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("RecordJiraWebhook error = %v; want ErrValidation", err)
		}
	})

	t.Run("missing connection", func(t *testing.T) {
		repository := newFakeRepo()
		repository.connections[connectionID] = jiraWebhookTestConnection(connectionID, "tenant-1", "jira", true)
		service := New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: secret}))

		_, err := service.RecordJiraWebhook(context.Background(), JiraWebhookInput{
			TenantID:  "tenant-1",
			Signature: githubSignature(secret, body),
			Body:      body,
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("RecordJiraWebhook error = %v; want ErrValidation", err)
		}
	})

	t.Run("wrong provider", func(t *testing.T) {
		repository := newFakeRepo()
		repository.connections[connectionID] = jiraWebhookTestConnection(connectionID, "tenant-1", "github", true)
		service := New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: secret}))

		_, err := service.RecordJiraWebhook(context.Background(), JiraWebhookInput{
			TenantID:     "tenant-1",
			ConnectionID: connectionID,
			Signature:    githubSignature(secret, body),
			Body:         body,
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("RecordJiraWebhook error = %v; want ErrValidation", err)
		}
	})

	t.Run("missing webhook secret", func(t *testing.T) {
		repository := newFakeRepo()
		repository.connections[connectionID] = jiraWebhookTestConnection(connectionID, "tenant-1", "jira", false)
		service := New(repository, ptrext.Of(fakeSecretStore{}))

		_, err := service.RecordJiraWebhook(context.Background(), JiraWebhookInput{
			TenantID:     "tenant-1",
			ConnectionID: connectionID,
			Signature:    githubSignature(secret, body),
			Body:         body,
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("RecordJiraWebhook error = %v; want ErrValidation", err)
		}
	})
}

func TestNormalizeJiraWebhookPayloadHelpers(t *testing.T) {
	invalid := normalizeJiraWebhookPayload([]byte(`{`))
	if invalid.EventType != "jira:webhook" || invalid.ExternalEventID != "" ||
		!strings.Contains(invalid.JSON, `"parse_error":"invalid_json"`) {
		t.Fatalf("invalid payload = %#v; want parse error envelope", invalid)
	}

	payload := normalizeJiraWebhookPayload([]byte(`{"event":"jira:issue_updated","comment":{"id":"300","created":"2026-07-18T00:00:00.000+0000","updated":"2026-07-18T00:10:00.000+0000","body":"Looks good","author":{"accountId":"acc-1","displayName":"Alice","emailAddress":"alice@example.com","secret":"drop"}},"user":{"accountId":"acc-1","displayName":"Alice","emailAddress":"alice@example.com","secret":"drop"}}`))
	if payload.EventType != "jira:issue_updated" || payload.ExternalEventID != "jira:issue_updated:comment:300" {
		t.Fatalf("payload = %#v; want comment-derived event id", payload)
	}
	if !strings.Contains(payload.JSON, `"provider":"jira"`) ||
		!strings.Contains(payload.JSON, `"comment":`) ||
		!strings.Contains(payload.JSON, `"user":`) ||
		strings.Contains(payload.JSON, "secret") ||
		strings.Contains(payload.JSON, "Looks good") ||
		strings.Contains(payload.JSON, "alice@example.com") ||
		strings.Contains(payload.JSON, "emailAddress") {
		t.Fatalf("normalized JSON = %s; want compact safe payload", payload.JSON)
	}
}

func assertRecordedJiraWebhookEvent(t *testing.T, event *repo.SyncEvent, receivedAt time.Time) {
	t.Helper()
	if event == nil {
		t.Fatal("event = nil")
	}
	if got, want := event.Provider, "jira"; got != want {
		t.Fatalf("event.Provider = %q; want %q", got, want)
	}
	if got, want := event.EventType, "jira:issue_updated"; got != want {
		t.Fatalf("event.EventType = %q; want %q", got, want)
	}
	if got, want := event.ExternalEventID, "jira:issue_updated:issue:10001:change:200:comment:300"; got != want {
		t.Fatalf("event.ExternalEventID = %q; want %q", got, want)
	}
	if got, want := event.SignatureStatus, repo.EventSignatureVerified; got != want {
		t.Fatalf("event.SignatureStatus = %v; want %v", got, want)
	}
	if got, want := event.Status, repo.EventStatusReceived; got != want {
		t.Fatalf("event.Status = %v; want %v", got, want)
	}
	if !event.ReceivedAt.Equal(receivedAt) {
		t.Fatalf("event.ReceivedAt = %v; want %v", event.ReceivedAt, receivedAt)
	}
}

func assertRecordedJiraWebhookPayloadDigest(t *testing.T, got string, body []byte) {
	t.Helper()
	if want := eventPayloadDigest(body); got != want {
		t.Fatalf("payload digest = %q; want raw body digest %q", got, want)
	}
}

func assertRecordedJiraWebhookAAD(t *testing.T, got []byte, connectionID uuid.UUID) {
	t.Helper()
	want := connectionWebhookSecretAAD("tenant-1", connectionID, "jira")
	if string(got) != string(want) {
		t.Fatalf("decrypt AAD = %q; want webhook-scoped AAD %q", string(got), string(want))
	}
}

func assertRecordedJiraWebhookNormalizedPayload(t *testing.T, raw []byte) {
	t.Helper()
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatalf("decode normalized payload: %v", err)
	}
	if got, want := normalized["provider"], "jira"; got != want {
		t.Fatalf("normalized payload provider = %v; want %q", got, want)
	}
	if got, want := normalized["webhook_event"], "jira:issue_updated"; got != want {
		t.Fatalf("normalized payload event = %v; want %q", got, want)
	}
	if _, ok := normalized["timestamp"]; !ok {
		t.Fatalf("normalized payload = %#v; want timestamp", normalized)
	}
	assertRecordedJiraWebhookObjectField(t, normalized, "issue", "key", "ACME-1")
	assertRecordedJiraWebhookObjectField(t, normalized, "changelog", "id", "200")
	assertRecordedJiraWebhookObjectField(t, normalized, "comment", "id", "300")
	assertRecordedJiraWebhookObjectField(t, normalized, "user", "displayName", "Alice")
	assertRecordedJiraWebhookSensitiveFieldsRedacted(t, raw, normalized)
}

func assertRecordedJiraWebhookObjectField(t *testing.T, normalized map[string]any, fieldName, key, want string) {
	t.Helper()
	obj, ok := normalized[fieldName].(map[string]any)
	if !ok || obj[key] != want {
		t.Fatalf("normalized %s = %#v; want %s=%s", fieldName, normalized[fieldName], key, want)
	}
}

func assertRecordedJiraWebhookSensitiveFieldsRedacted(t *testing.T, raw []byte, normalized map[string]any) {
	t.Helper()
	if strings.Contains(string(raw), "Looks good") ||
		strings.Contains(string(raw), "alice@example.com") ||
		strings.Contains(string(raw), "emailAddress") {
		t.Fatalf("normalized payload = %s; want body and email redacted", string(raw))
	}
	comment, ok := normalized["comment"].(map[string]any)
	if !ok {
		t.Fatalf("normalized comment = %#v; want object", normalized["comment"])
	}
	if _, ok := comment["body"]; ok || comment["body_present"] != true || comment["body_digest"] == "" {
		t.Fatalf("normalized comment = %#v; want body digest metadata without body", comment)
	}
}

func jiraWebhookTestConnection(id uuid.UUID, tenantID, provider string, withSecret bool) repo.Connection {
	conn := serviceTestConnection(id, tenantID, provider)
	if withSecret {
		conn.WebhookSecretKeyID = "kid-1"
		conn.WebhookSecretCiphertext = []byte("ciphertext")
	}
	return conn
}
