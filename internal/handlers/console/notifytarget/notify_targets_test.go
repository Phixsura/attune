// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package notifytarget

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

func notifyRequestContext() *dispatcher.RequestContext[*session.AuthCtx] {
	return &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    &session.AuthCtx{TenantID: "tenant-1"},
	}
}

func TestListNotifyTargetsMapsRows(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC)
	failedAt := created.Add(time.Minute)
	fake := &fakeNotifyRepo{
		listRows: []notifytarget.NotifyTarget{{
			ID:              id,
			DestinationType: notifytarget.DestRawWebhook,
			Audience:        notifytarget.AudienceAll,
			URL:             "https://example.com/hook",
			TimeoutSeconds:  15,
			Disabled:        true,
			CreatedAt:       created,
			LastFailureAt:   &failedAt,
			LastError:       "boom",
		}},
	}
	h := NewNotifyTargetsHandler(fake)

	result, err := h.List(notifyRequestContext(), &attunev1.ListNotifyTargetsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, "tenant-1", fake.listTenant)
	require.Len(t, result.Body.GetItems(), 1)
	got := result.Body.GetItems()[0]
	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, notifytarget.DestRawWebhook, got.GetDestinationType())
	require.Equal(t, notifytarget.AudienceAll, got.GetAudience())
	require.Equal(t, "https://example.com/hook", got.GetUrl())
	require.Equal(t, int32(15), got.GetTimeoutSeconds())
	require.True(t, got.GetDisabled())
	require.Equal(t, created.Format(time.RFC3339), got.GetCreatedAt())
	require.Equal(t, failedAt.Format(time.RFC3339), got.GetLastFailureAt())
	require.Equal(t, "boom", got.GetLastError())
}

func TestListNotifyTargetsServiceError(t *testing.T) {
	t.Parallel()

	h := NewNotifyTargetsHandler(&fakeNotifyRepo{listErr: errors.New("db down")})

	_, err := h.List(notifyRequestContext(), &attunev1.ListNotifyTargetsRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

func TestCreateNotifyTargetNormalizesAndInserts(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC)
	fake := &fakeNotifyRepo{insertID: id, insertTime: created}
	h := NewNotifyTargetsHandler(fake)

	result, err := h.Create(notifyRequestContext(), &attunev1.CreateNotifyTargetRequest{
		DestinationType: " raw-webhook ",
		Url:             " https://example.com/hook ",
		TimeoutSeconds:  0,
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.NotNil(t, fake.insertTarget)
	require.Equal(t, "tenant-1", fake.insertTarget.TenantID)
	require.Equal(t, notifytarget.DestRawWebhook, fake.insertTarget.DestinationType)
	require.Equal(t, notifytarget.AudienceAll, fake.insertTarget.Audience)
	require.Equal(t, "https://example.com/hook", fake.insertTarget.URL)
	require.Equal(t, 10, fake.insertTarget.TimeoutSeconds)
	require.Equal(t, id.String(), result.Body.GetId())
	require.Equal(t, created.Format(time.RFC3339), result.Body.GetCreatedAt())
}

func TestCreateSlackNotifyTargetNormalizesAndInserts(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	fake := &fakeNotifyRepo{insertID: id, insertTime: created}
	h := NewNotifyTargetsHandler(fake)

	result, err := h.Create(notifyRequestContext(), &attunev1.CreateNotifyTargetRequest{
		DestinationType: " slack ",
		Url:             " https://hooks.slack.com/services/T00/B00/xxx ",
		TimeoutSeconds:  0,
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.NotNil(t, fake.insertTarget)
	require.Equal(t, "tenant-1", fake.insertTarget.TenantID)
	require.Equal(t, notifytarget.DestSlack, fake.insertTarget.DestinationType)
	require.Equal(t, notifytarget.AudienceAll, fake.insertTarget.Audience)
	require.Equal(t, "https://hooks.slack.com/services/T00/B00/xxx", fake.insertTarget.URL)
	require.Equal(t, 10, fake.insertTarget.TimeoutSeconds)
	require.Equal(t, id.String(), result.Body.GetId())
	require.Equal(t, "slack", result.Body.GetDestinationType())
}

func TestCreateNotifyTargetValidationAndConflict(t *testing.T) {
	t.Parallel()

	h := NewNotifyTargetsHandler(&fakeNotifyRepo{})
	_, err := h.Create(notifyRequestContext(), &attunev1.CreateNotifyTargetRequest{
		DestinationType: notifytarget.DestRawWebhook,
		Url:             "http://example.com/hook",
	})
	var validation *dispatcher.Error
	require.ErrorAs(t, err, &validation)
	require.Equal(t, http.StatusBadRequest, validation.Status)
	require.Equal(t, attunev1.ErrorCode_VALIDATION, validation.Code)

	h = NewNotifyTargetsHandler(&fakeNotifyRepo{insertErr: notifytarget.ErrNotifyTargetConflict})
	_, err = h.Create(notifyRequestContext(), &attunev1.CreateNotifyTargetRequest{
		DestinationType: notifytarget.DestRawWebhook,
		Url:             "https://example.com/hook",
	})
	var conflict *dispatcher.Error
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, http.StatusConflict, conflict.Status)
	require.Equal(t, attunev1.ErrorCode_CONFLICT, conflict.Code)
}

func TestValidateNotifyCreateRejectsEmbeddedCredentials(t *testing.T) {
	t.Parallel()

	err := validateNotifyCreate(&createNotifyRequest{
		DestinationType: notifytarget.DestRawWebhook,
		URL:             "https://user:pass@example.com/hook",
	})

	require.EqualError(t, err, "url must not embed credentials")
}

func TestSanitizeNotifyTargetURLDropsCredentialsAndQuery(t *testing.T) {
	t.Parallel()

	got := sanitizeNotifyTargetURL("https://user:pass@example.com/hook?token=secret#frag")

	require.Equal(t, "https://example.com/hook", got)
}

func TestValidateNotifyCreateAcceptsSlackAndLark(t *testing.T) {
	t.Parallel()
	for _, dest := range []string{
		notifytarget.DestSlack,
		notifytarget.DestLark,
		notifytarget.DestGitHubIssue,
	} {
		err := validateNotifyCreate(&createNotifyRequest{
			DestinationType: dest,
			URL:             "https://hooks.example.com/x",
		})
		require.NoError(t, err, "dest=%s should be accepted", dest)
	}
}

func TestValidateNotifyCreateRejectsLegacyAndUnknownTypes(t *testing.T) {
	t.Parallel()
	for _, dest := range []string{"slack-bot", "email", "unknown", ""} {
		err := validateNotifyCreate(&createNotifyRequest{
			DestinationType: dest,
			URL:             "https://hooks.example.com/x",
		})
		require.Error(t, err, "dest=%q should be rejected", dest)
	}
}

func TestAuditNotifyTargetSnapshot_WithSecret(t *testing.T) {
	t.Parallel()
	target := notifytarget.NotifyTarget{
		DestinationType: notifytarget.DestRawWebhook,
		URL:             "https://example.com/hook",
		Secret:          "some-secret-value",
	}
	snap := auditNotifyTargetSnapshot(target)
	require.Equal(t, true, snap["has_secret"])
	_, hasURL := snap["url"]
	require.True(t, hasURL, "targets with secret should have sanitized url")
	_, hasHash := snap["url_hash"]
	require.False(t, hasHash, "targets with secret should not have url_hash")
}

// A token-in-URL destination (Discord/Slack/Lark) must NEVER have its URL path
// written to the audit log, even when the operator also supplies an optional
// secret. The Discord webhook token lives in the URL path, so logging the
// "sanitized" URL (which keeps the path) would leak the credential.
func TestAuditNotifyTargetSnapshot_DiscordWithSecretStillHashesURL(t *testing.T) {
	t.Parallel()
	target := notifytarget.NotifyTarget{
		DestinationType: notifytarget.DestDiscord,
		URL:             "https://discord.com/api/webhooks/123456789/SUPER_SECRET_TOKEN",
		Secret:          "operator-supplied-extra-secret",
	}
	snap := auditNotifyTargetSnapshot(target)

	if _, hasURL := snap["url"]; hasURL {
		t.Errorf("token-in-URL dest must not record a path-bearing url, got %v", snap["url"])
	}
	hash, hasHash := snap["url_hash"]
	if !hasHash {
		t.Fatalf("token-in-URL dest must record url_hash even with a secret")
	}
	if hashStr, _ := hash.(string); strings.Contains(hashStr, "SUPER_SECRET_TOKEN") {
		t.Errorf("url_hash must not contain the raw token: %v", hashStr)
	}
}

func TestAuditNotifyTargetSnapshot_WithoutSecret(t *testing.T) {
	t.Parallel()
	target := notifytarget.NotifyTarget{
		DestinationType: notifytarget.DestSlack,
		URL:             "https://hooks.slack.com/services/T00/B00/xxx",
	}
	snap := auditNotifyTargetSnapshot(target)
	_, hasSecret := snap["has_secret"]
	require.False(t, hasSecret, "targets without secret should not have has_secret")
	hash, hasHash := snap["url_hash"]
	require.True(t, hasHash, "targets without secret should have url_hash")
	hashStr, ok := hash.(string)
	require.True(t, ok)
	require.True(t, len(hashStr) > 10, "url_hash should be a sha256 string")
	require.Contains(t, hashStr, "sha256:")
}

func TestNotifyTargetSummary_EmptyType(t *testing.T) {
	t.Parallel()
	got := notifyTargetSummary("created", notifytarget.NotifyTarget{})
	require.Equal(t, "created", got)
}

func TestNotifyTargetSummary_WithType(t *testing.T) {
	t.Parallel()
	got := notifyTargetSummary("deleted", notifytarget.NotifyTarget{DestinationType: "slack"})
	require.Equal(t, "deleted (slack)", got)
}

func TestErrMessage_Nil(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", errMessage(nil))
}

func TestErrMessage_WithError(t *testing.T) {
	t.Parallel()
	require.Equal(t, "something broke", errMessage(errors.New("something broke")))
}

func TestSanitizeNotifyTargetURL_Empty(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", sanitizeNotifyTargetURL(""))
}

func TestSanitizeNotifyTargetURL_InvalidURL(t *testing.T) {
	t.Parallel()
	got := sanitizeNotifyTargetURL("://broken")
	require.Equal(t, "://broken", got)
}
