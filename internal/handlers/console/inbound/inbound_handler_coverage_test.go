// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/email"
	"github.com/Phixsura/attune/internal/inbound/adapter/webhook"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// ---------- test-only fakes (unique names to avoid collisions) ----------

// covSourceRepo supports per-call Get results for the post-toggle and
// post-create reload paths, plus configurable List and SetEnabled
// behaviour.
type covSourceRepo struct {
	listSrcs      []inbound.Source
	listErr       error
	getSrc        inbound.Source
	getErr        error
	getCalls      int
	reloadSrc     inbound.Source
	reloadErr     error
	setEnabledID  string
	setEnabledOn  bool
	setEnabledErr error
}

func (f *covSourceRepo) List(_ context.Context, _ string) ([]inbound.Source, error) {
	return f.listSrcs, f.listErr
}

func (f *covSourceRepo) Get(_ context.Context, _ string) (inbound.Source, error) {
	f.getCalls++
	if f.getCalls > 1 && f.reloadSrc.ID != "" {
		return f.reloadSrc, f.reloadErr
	}
	return f.getSrc, f.getErr
}

func (f *covSourceRepo) SetEnabled(_ context.Context, id string, enabled bool, _ string) error {
	f.setEnabledID = id
	f.setEnabledOn = enabled
	return f.setEnabledErr
}

func (f *covSourceRepo) GetBySlugs(_ context.Context, _, _, _ string) (inbound.Source, error) {
	return f.getSrc, f.getErr
}

func (f *covSourceRepo) UpdateState(_ context.Context, _ string, _ inbound.SourceState) error {
	return nil
}

// covAuditRecorder records events and optionally returns an error.
type covAuditRecorder struct {
	events []auditlogsvc.Event
	err    error
}

func (f *covAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.events = append(f.events, event)
	return f.err
}

// covFailingAuditRecorder always returns an error on Record.
type covFailingAuditRecorder struct{ err error }

func (f *covFailingAuditRecorder) Record(_ context.Context, _ auditlogsvc.Event) error {
	return f.err
}

// covFailingSecrets always fails on Encrypt.
type covFailingSecrets struct{ encryptErr error }

func (f covFailingSecrets) Encrypt(_ []byte) ([]byte, error) {
	return nil, f.encryptErr
}

func (f covFailingSecrets) Decrypt(_ []byte) ([]byte, error) {
	return nil, errors.New("covFailingSecrets: not implemented")
}

// covSecondCallFailSecrets succeeds on the first Encrypt, fails on the second.
type covSecondCallFailSecrets struct {
	callCount int
	err       error
}

func (f *covSecondCallFailSecrets) Encrypt(b []byte) ([]byte, error) {
	f.callCount++
	if f.callCount >= 2 {
		return nil, f.err
	}
	return inboundtest.FakeSecrets{}.Encrypt(b)
}

func (f *covSecondCallFailSecrets) Decrypt(b []byte) ([]byte, error) {
	return inboundtest.FakeSecrets{}.Decrypt(b)
}

// covFakeSQLStateErr implements the sqlState interface for unique constraint tests.
type covFakeSQLStateErr struct{ state string }

func (e covFakeSQLStateErr) Error() string    { return "sql: " + e.state }
func (e covFakeSQLStateErr) SQLState() string { return e.state }

// covDirectCtx builds a dispatcher.RequestContext for direct handler calls.
func covDirectCtx(tenantID string) *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: tenantID, UserID: "user-cov"}),
	})
}

// covHandler constructs a Handler with injectable fakes.
func covHandler(repo inbound.SourceStore, audit auditRecorder) *Handler {
	return ptrext.Of(Handler{
		sources:    repo,
		secrets:    inboundtest.FakeSecrets{},
		baseURL:    "https://attune.example.com",
		rotate:     stubRotate(nil, time.Now().Add(24*time.Hour)),
		testConn:   stubProbe(nil),
		tenantSlug: func(_ context.Context, _ string) (string, error) { return "tenant-x", nil },
		audit:      audit,
	})
}

// ====================== encryptEmailConfig =================================

func TestEncryptEmailConfig_HappyPath(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: inboundtest.FakeSecrets{}})
	cfg := ptrext.Of(attunev1.EmailCreateConfig{
		Host:     "imap.example.com",
		Port:     993,
		Tls:      true,
		Username: "ops@example.com",
		Password: "supersecret",
		Folder:   "Archive",
	})
	envelope, err := h.encryptEmailConfig(cfg)
	require.NoError(t, err)
	require.NotEmpty(t, envelope)

	decOuter, err := inboundtest.FakeSecrets{}.Decrypt(envelope)
	require.NoError(t, err)

	var inner email.Config
	require.NoError(t, json.Unmarshal(decOuter, &inner))
	require.Equal(t, email.ConfigVersion, inner.Version)
	require.Equal(t, "imap.example.com", inner.Host)
	require.Equal(t, 993, inner.Port)
	require.True(t, inner.TLS)
	require.Equal(t, "ops@example.com", inner.Username)
	require.Equal(t, "Archive", inner.Folder)
	require.NotEmpty(t, inner.PasswordEncrypted)

	decPW, err := inboundtest.FakeSecrets{}.Decrypt(inner.PasswordEncrypted)
	require.NoError(t, err)
	require.Equal(t, "supersecret", string(decPW))
}

func TestEncryptEmailConfig_DefaultFolder(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: inboundtest.FakeSecrets{}})
	cfg := ptrext.Of(attunev1.EmailCreateConfig{
		Host: "h", Port: 993, Tls: true, Username: "u", Password: "pw",
	})
	envelope, err := h.encryptEmailConfig(cfg)
	require.NoError(t, err)
	decOuter, err := inboundtest.FakeSecrets{}.Decrypt(envelope)
	require.NoError(t, err)
	var inner email.Config
	require.NoError(t, json.Unmarshal(decOuter, &inner))
	require.Equal(t, "INBOX", inner.Folder)
}

func TestEncryptEmailConfig_DefaultStartFrom(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: inboundtest.FakeSecrets{}})
	cfg := ptrext.Of(attunev1.EmailCreateConfig{
		Host: "h", Port: 993, Tls: true, Username: "u", Password: "pw",
	})
	envelope, err := h.encryptEmailConfig(cfg)
	require.NoError(t, err)
	decOuter, err := inboundtest.FakeSecrets{}.Decrypt(envelope)
	require.NoError(t, err)
	var inner email.Config
	require.NoError(t, json.Unmarshal(decOuter, &inner))
	require.Equal(t, "now", inner.StartFrom)
}

func TestEncryptEmailConfig_DefaultAfterIngest(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: inboundtest.FakeSecrets{}})
	cfg := ptrext.Of(attunev1.EmailCreateConfig{
		Host: "h", Port: 993, Tls: true, Username: "u", Password: "pw",
	})
	envelope, err := h.encryptEmailConfig(cfg)
	require.NoError(t, err)
	decOuter, err := inboundtest.FakeSecrets{}.Decrypt(envelope)
	require.NoError(t, err)
	var inner email.Config
	require.NoError(t, json.Unmarshal(decOuter, &inner))
	require.Equal(t, "mark_seen", inner.AfterIngest)
}

func TestEncryptEmailConfig_CustomValues(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: inboundtest.FakeSecrets{}})
	cfg := ptrext.Of(attunev1.EmailCreateConfig{
		Host: "mail.test.io", Port: 143, Tls: true,
		Username: "user@test.io", Password: "my-pw",
		Folder: "Sent", StartFrom: "all_unseen", AfterIngest: "delete",
	})
	envelope, err := h.encryptEmailConfig(cfg)
	require.NoError(t, err)
	decOuter, err := inboundtest.FakeSecrets{}.Decrypt(envelope)
	require.NoError(t, err)
	var inner email.Config
	require.NoError(t, json.Unmarshal(decOuter, &inner))
	require.Equal(t, "Sent", inner.Folder)
	require.Equal(t, "all_unseen", inner.StartFrom)
	require.Equal(t, "delete", inner.AfterIngest)
}

func TestEncryptEmailConfig_PasswordEncryptFails(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: covFailingSecrets{encryptErr: errors.New("encrypt boom")}})
	cfg := ptrext.Of(attunev1.EmailCreateConfig{
		Host: "h", Port: 993, Tls: true, Username: "u", Password: "p",
	})
	_, err := h.encryptEmailConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "encrypt password")
}

func TestEncryptEmailConfig_EnvelopeEncryptFails(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{secrets: ptrext.Of(covSecondCallFailSecrets{err: errors.New("envelope boom")})})
	cfg := ptrext.Of(attunev1.EmailCreateConfig{
		Host: "h", Port: 993, Tls: true, Username: "u", Password: "p",
	})
	_, err := h.encryptEmailConfig(cfg)
	require.Error(t, err)
}

// ====================== Create email validation paths ======================

func TestCreateEmail_MissingConfig_DirectCall(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := covHandler(repo, nil)
	_, err := h.Create(covDirectCtx("tenant-1"), ptrext.Of(attunev1.CreateInboundSourceRequest{
		Channel: "email",
		Name:    "My Email Source",
	}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "email_config is required")
}

func TestCreateEmail_BadHost_DirectCall(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := covHandler(repo, nil)
	_, err := h.Create(covDirectCtx("tenant-1"), ptrext.Of(attunev1.CreateInboundSourceRequest{
		Channel: "email",
		Name:    "My Email Source",
		EmailConfig: ptrext.Of(attunev1.EmailCreateConfig{
			Host: "", Port: 993, Tls: true,
		}),
	}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "host")
}

func TestCreateEmail_TLSFalse_DirectCall(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := covHandler(repo, nil)
	_, err := h.Create(covDirectCtx("tenant-1"), ptrext.Of(attunev1.CreateInboundSourceRequest{
		Channel: "email",
		Name:    "My Email Source",
		EmailConfig: ptrext.Of(attunev1.EmailCreateConfig{
			Host: "imap.example.com", Port: 993, Tls: false,
			Username: "u", Password: "p",
		}),
	}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "tls")
}

func TestCreateEmail_MissingPassword_DirectCall(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := covHandler(repo, nil)
	_, err := h.Create(covDirectCtx("tenant-1"), ptrext.Of(attunev1.CreateInboundSourceRequest{
		Channel: "email",
		Name:    "My Email Source",
		EmailConfig: ptrext.Of(attunev1.EmailCreateConfig{
			Host: "imap.example.com", Port: 993, Tls: true,
			Username: "u", Password: "",
		}),
	}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "password")
}

func TestCreateEmail_NoPool_DirectCall(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := covHandler(repo, nil) // pool is nil
	_, err := h.Create(covDirectCtx("tenant-1"), ptrext.Of(attunev1.CreateInboundSourceRequest{
		Channel: "email",
		Name:    "My Email Source",
		EmailConfig: ptrext.Of(attunev1.EmailCreateConfig{
			Host: "imap.example.com", Port: 993, Tls: true,
			Username: "u", Password: "p",
		}),
	}))
	// secretlock.WithTx returns error when pool is nil.
	require.Error(t, err)
}

// ====================== Create channel normalisation =======================

func TestCreate_ChannelCaseInsensitive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		channel string
	}{
		{"uppercase WEBHOOK", "WEBHOOK"},
		{"mixed Webhook", "Webhook"},
		{"spaces", "  webhook  "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := ptrext.Of(covSourceRepo{})
			h := covHandler(repo, nil)
			_, err := h.Create(covDirectCtx("tenant-1"), ptrext.Of(attunev1.CreateInboundSourceRequest{
				Channel: tc.channel,
				Name:    "Test",
			}))
			// Should not be a "channel must be" error; the nil pool
			// causes a different error downstream.
			require.Error(t, err)
			var de *dispatcher.Error
			if errors.As(err, &de) {
				require.NotContains(t, de.Message, "channel must be")
			}
		})
	}
}

func TestCreate_ChannelEmailUpperCase(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := covHandler(repo, nil)
	_, err := h.Create(covDirectCtx("tenant-1"), ptrext.Of(attunev1.CreateInboundSourceRequest{
		Channel: "EMAIL",
		Name:    "Test Email",
	}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "email_config is required")
}

// ====================== Create via HTTP ====================================

func TestCreateWebhook_NoPool_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveCreate(h, `{"channel":"webhook","name":"My Hook"}`)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateEmail_NoPool_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	body := `{"channel":"email","name":"My Email","emailConfig":{"host":"h","port":993,"tls":true,"username":"u","password":"p"}}`
	w := serveCreate(h, body)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateEmail_MissingConfig_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveCreate(h, `{"channel":"email","name":"My Email"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "email_config is required")
}

func TestCreateEmail_ValidationFails_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	body := `{"channel":"email","name":"My Email","emailConfig":{"host":"h","port":0,"tls":true,"username":"u","password":"p"}}`
	w := serveCreate(h, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "port")
}

// ====================== Create name boundary ===============================

func TestCreate_NameExactly200Chars(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := covHandler(repo, nil)
	name := strings.Repeat("a", 200)
	_, err := h.Create(covDirectCtx("tenant-1"), ptrext.Of(attunev1.CreateInboundSourceRequest{
		Channel: "webhook",
		Name:    name,
	}))
	if err != nil {
		var de *dispatcher.Error
		if errors.As(err, &de) {
			require.NotContains(t, de.Message, "name")
		}
	}
}

func TestCreate_NameExactly201Chars(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := covHandler(repo, nil)
	name := strings.Repeat("a", 201)
	_, err := h.Create(covDirectCtx("tenant-1"), ptrext.Of(attunev1.CreateInboundSourceRequest{
		Channel: "webhook",
		Name:    name,
	}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
}

func TestCreate_NameNonAlphanumericSlugEmpty(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := covHandler(repo, nil)
	_, err := h.Create(covDirectCtx("tenant-1"), ptrext.Of(attunev1.CreateInboundSourceRequest{
		Channel: "webhook",
		Name:    "!!!",
	}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
	require.Contains(t, de.Message, "alphanumeric")
}

// ====================== rowToProto =========================================

func TestRowToProto_DisabledSource(t *testing.T) {
	t.Parallel()
	src := inbound.Source{
		ID: "id-5", TenantID: "t-1", Channel: "email",
		Name: "Disabled", Slug: "disabled", Enabled: false,
		State:     inbound.SourceState{LastError: "paused by admin"},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	out := rowToProto(src)
	require.False(t, out.GetEnabled())
	require.Equal(t, "paused by admin", out.GetLastError())
	require.Nil(t, out.LastEventAt)
}

// ====================== isUUID =============================================

// ====================== isUniqueViolation ==================================

func TestIsUniqueViolation_PlainError(t *testing.T) {
	t.Parallel()
	require.False(t, isUniqueViolation(errors.New("plain")))
}

// ====================== insertErr ==========================================

func TestInsertErr_UniqueViolation(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	err := h.insertErr(context.Background(), "test", "t-1", covFakeSQLStateErr{state: "23505"})
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusConflict, de.Status)
	require.Contains(t, de.Message, "same name")
}

func TestInsertErr_GenericDBError(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	err := h.insertErr(context.Background(), "test", "t-1", errors.New("connection reset"))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestInsertErr_WrappedUniqueViolation(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	inner := covFakeSQLStateErr{state: "23505"}
	wrapped := errors.Join(errors.New("outer"), inner)
	err := h.insertErr(context.Background(), "test", "t-1", wrapped)
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusConflict, de.Status)
}

// ====================== tenantSlugFromPool =================================

// ====================== SetAuditLogger =====================================

// ====================== recordAudit ========================================

func TestRecordAudit_WithHTTPRequest(t *testing.T) {
	t.Parallel()
	audit := ptrext.Of(covAuditRecorder{})
	h := ptrext.Of(Handler{audit: audit})
	req, _ := http.NewRequest(http.MethodPost, "/test", nil)
	err := h.recordAudit(
		context.Background(),
		"admin", "user-1", "tenant-1", "test.action", "target-1", "summary",
		req, nil, nil,
	)
	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "inbound_source", audit.events[0].TargetType)
}

func TestRecordAudit_ErrorPropagated(t *testing.T) {
	t.Parallel()
	audit := ptrext.Of(covAuditRecorder{err: errors.New("audit db down")})
	h := ptrext.Of(Handler{audit: audit})
	err := h.recordAudit(
		context.Background(),
		"admin", "user-1", "tenant-1", "test.action", "target-1", "summary",
		nil, nil, nil,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "audit db down")
}

func TestRecordAudit_BeforeAndAfterPayloads(t *testing.T) {
	t.Parallel()
	audit := ptrext.Of(covAuditRecorder{})
	h := ptrext.Of(Handler{audit: audit})
	before := map[string]any{"enabled": true}
	after := map[string]any{"enabled": false}
	err := h.recordAudit(
		context.Background(),
		"admin", "user-1", "tenant-1", "test.action", "target-1", "summary",
		nil, before, after,
	)
	require.NoError(t, err)
	require.NotNil(t, audit.events[0].Before)
	require.NotNil(t, audit.events[0].After)
}

// ====================== getOwnedSource =====================================

func TestGetOwnedSource_Success(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	now := time.Now().UTC()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Name: "Hook", Slug: "hook", Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		},
	})
	h := ptrext.Of(Handler{sources: repo})
	auth := ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"})
	src, err := h.getOwnedSource(context.Background(), auth, id, "test")
	require.NoError(t, err)
	require.Equal(t, id, src.ID)
}

func TestGetOwnedSource_BadUUID(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := ptrext.Of(Handler{sources: repo})
	auth := ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"})
	_, err := h.getOwnedSource(context.Background(), auth, "not-a-uuid", "test")
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
}

func TestGetOwnedSource_NotFound(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{getErr: inboundsource.ErrNotFound})
	h := ptrext.Of(Handler{sources: repo})
	auth := ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"})
	_, err := h.getOwnedSource(context.Background(), auth, uuid.NewString(), "test")
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusNotFound, de.Status)
}

func TestGetOwnedSource_InternalError(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{getErr: errors.New("connection lost")})
	h := ptrext.Of(Handler{sources: repo})
	auth := ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"})
	_, err := h.getOwnedSource(context.Background(), auth, uuid.NewString(), "test")
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestGetOwnedSource_CrossTenant(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{ID: id, TenantID: "other-tenant", Channel: "webhook"},
	})
	h := ptrext.Of(Handler{sources: repo})
	auth := ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"})
	_, err := h.getOwnedSource(context.Background(), auth, id, "test")
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusNotFound, de.Status)
}

// ====================== Delete — direct-call paths =========================

func TestDelete_BadUUID_DirectCall(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{})
	h := covHandler(repo, nil)
	_, err := h.Delete(covDirectCtx("tenant-1"), ptrext.Of(attunev1.DeleteInboundSourceRequest{Id: "not-uuid"}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
}

func TestDelete_NotFound_DirectCall(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{getErr: inboundsource.ErrNotFound})
	h := covHandler(repo, nil)
	_, err := h.Delete(covDirectCtx("tenant-1"), ptrext.Of(attunev1.DeleteInboundSourceRequest{Id: uuid.NewString()}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusNotFound, de.Status)
}

func TestDelete_CrossTenant_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{ID: id, TenantID: "other-tenant", Channel: "webhook"},
	})
	h := covHandler(repo, nil)
	_, err := h.Delete(covDirectCtx("tenant-1"), ptrext.Of(attunev1.DeleteInboundSourceRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusNotFound, de.Status)
}

func TestDelete_NilPool_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook"},
	})
	h := covHandler(repo, nil) // pool is nil
	_, err := h.Delete(covDirectCtx("tenant-1"), ptrext.Of(attunev1.DeleteInboundSourceRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
	require.Contains(t, de.Message, "pool not configured")
}

// ====================== Get — direct-call paths ============================

func TestGet_OwnedByTenant_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	now := time.Now().UTC()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Name: "My Hook", Slug: "my-hook", Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		},
	})
	h := covHandler(repo, nil)
	result, err := h.Get(covDirectCtx("tenant-1"), ptrext.Of(attunev1.GetInboundSourceRequest{Id: id}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, id, result.Body.GetId())
}

// ====================== Pause/Resume — direct-call paths ===================

func TestPause_Success_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	now := time.Now().UTC()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Name: "Hook", Slug: "hook", Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		},
		reloadSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Name: "Hook", Slug: "hook", Enabled: false,
			CreatedAt: now, UpdatedAt: now,
		},
	})
	h := covHandler(repo, nil)
	result, err := h.Pause(covDirectCtx("tenant-1"), ptrext.Of(attunev1.PauseInboundSourceRequest{Id: id}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.False(t, result.Body.GetEnabled())
}

func TestPause_NotFound_DirectCall(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(covSourceRepo{getErr: inboundsource.ErrNotFound})
	h := covHandler(repo, nil)
	_, err := h.Pause(covDirectCtx("tenant-1"), ptrext.Of(attunev1.PauseInboundSourceRequest{Id: uuid.NewString()}))
	require.Error(t, err)
}

func TestPause_SetEnabledFails_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	now := time.Now().UTC()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Enabled: true, CreatedAt: now, UpdatedAt: now,
		},
		setEnabledErr: errors.New("db error"),
	})
	h := covHandler(repo, nil)
	_, err := h.Pause(covDirectCtx("tenant-1"), ptrext.Of(attunev1.PauseInboundSourceRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestPause_ReloadFails_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	now := time.Now().UTC()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Enabled: true, CreatedAt: now, UpdatedAt: now,
		},
		reloadSrc: inbound.Source{ID: id}, // triggers reload path
		reloadErr: errors.New("reload failed"),
	})
	h := covHandler(repo, nil)
	_, err := h.Pause(covDirectCtx("tenant-1"), ptrext.Of(attunev1.PauseInboundSourceRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestPause_AuditFails_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	now := time.Now().UTC()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Enabled: true, CreatedAt: now, UpdatedAt: now,
		},
		reloadSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Enabled: false, CreatedAt: now, UpdatedAt: now,
		},
	})
	h := covHandler(repo, ptrext.Of(covFailingAuditRecorder{err: errors.New("audit db down")}))
	_, err := h.Pause(covDirectCtx("tenant-1"), ptrext.Of(attunev1.PauseInboundSourceRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestPause_RecordsAudit_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	now := time.Now().UTC()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Enabled: true, CreatedAt: now, UpdatedAt: now,
		},
		reloadSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Enabled: false, CreatedAt: now, UpdatedAt: now,
		},
	})
	audit := ptrext.Of(covAuditRecorder{})
	h := covHandler(repo, audit)
	result, err := h.Pause(covDirectCtx("tenant-1"), ptrext.Of(attunev1.PauseInboundSourceRequest{Id: id}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, audit.events, 1)
	require.Equal(t, "inbound_source.pause", audit.events[0].Action)
}

func TestResume_Success_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	now := time.Now().UTC()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Enabled: false, CreatedAt: now, UpdatedAt: now,
		},
		reloadSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Enabled: true, CreatedAt: now, UpdatedAt: now,
		},
	})
	h := covHandler(repo, nil)
	result, err := h.Resume(covDirectCtx("tenant-1"), ptrext.Of(attunev1.ResumeInboundSourceRequest{Id: id}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.True(t, result.Body.GetEnabled())
}

func TestResume_RecordsAudit_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	now := time.Now().UTC()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Enabled: false, CreatedAt: now, UpdatedAt: now,
		},
		reloadSrc: inbound.Source{
			ID: id, TenantID: "tenant-1", Channel: "webhook",
			Enabled: true, CreatedAt: now, UpdatedAt: now,
		},
	})
	audit := ptrext.Of(covAuditRecorder{})
	h := covHandler(repo, audit)
	result, err := h.Resume(covDirectCtx("tenant-1"), ptrext.Of(attunev1.ResumeInboundSourceRequest{Id: id}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, audit.events, 1)
	require.Equal(t, "inbound_source.resume", audit.events[0].Action)
}

// ====================== Rotate — direct-call paths =========================

func TestRotate_Success_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	nextEligible := time.Now().Add(24 * time.Hour)
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook"},
	})
	h := ptrext.Of(Handler{
		sources:  repo,
		secrets:  inboundtest.FakeSecrets{},
		baseURL:  "https://attune.example.com",
		rotate:   stubRotate(nil, nextEligible),
		testConn: stubProbe(nil),
	})
	result, err := h.Rotate(covDirectCtx("tenant-1"), ptrext.Of(attunev1.RotateInboundSourceSecretRequest{Id: id}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.NotEmpty(t, result.Body.GetSecretHex())
	require.NotEmpty(t, result.Body.GetNextEligibleAt())
}

func TestRotate_NotWebhook_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{ID: id, TenantID: "tenant-1", Channel: "email"},
	})
	h := covHandler(repo, nil)
	_, err := h.Rotate(covDirectCtx("tenant-1"), ptrext.Of(attunev1.RotateInboundSourceSecretRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
}

func TestRotate_GraceWindow_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	nextEligible := time.Now().Add(2 * time.Hour)
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook"},
	})
	h := ptrext.Of(Handler{
		sources: repo,
		secrets: inboundtest.FakeSecrets{},
		baseURL: "https://attune.example.com",
		rotate: func(_ context.Context, _ *pgxpool.Pool, _ inbound.SecretStore, _ string) ([]byte, time.Time, error) {
			return nil, nextEligible, webhook.ErrRotationInGraceWindow
		},
		testConn: stubProbe(nil),
	})
	_, err := h.Rotate(covDirectCtx("tenant-1"), ptrext.Of(attunev1.RotateInboundSourceSecretRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusConflict, de.Status)
}

func TestRotate_InternalError_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook"},
	})
	h := ptrext.Of(Handler{
		sources:  repo,
		secrets:  inboundtest.FakeSecrets{},
		baseURL:  "https://attune.example.com",
		rotate:   stubRotate(errors.New("internal failure"), time.Time{}),
		testConn: stubProbe(nil),
	})
	_, err := h.Rotate(covDirectCtx("tenant-1"), ptrext.Of(attunev1.RotateInboundSourceSecretRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestRotate_RecordsAudit_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	nextEligible := time.Now().Add(24 * time.Hour)
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook"},
	})
	audit := ptrext.Of(covAuditRecorder{})
	h := ptrext.Of(Handler{
		sources:  repo,
		secrets:  inboundtest.FakeSecrets{},
		baseURL:  "https://attune.example.com",
		rotate:   stubRotate(nil, nextEligible),
		testConn: stubProbe(nil),
		audit:    audit,
	})
	result, err := h.Rotate(covDirectCtx("tenant-1"), ptrext.Of(attunev1.RotateInboundSourceSecretRequest{Id: id}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, audit.events, 1)
	require.Equal(t, "inbound_source.rotate_secret", audit.events[0].Action)
}

func TestRotate_CrossTenant_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(covSourceRepo{
		getSrc: inbound.Source{ID: id, TenantID: "other-tenant", Channel: "webhook"},
	})
	h := covHandler(repo, nil)
	_, err := h.Rotate(covDirectCtx("tenant-1"), ptrext.Of(attunev1.RotateInboundSourceSecretRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusNotFound, de.Status)
}

// ====================== TestConnection — direct-call paths =================

func TestTestConn_NonEmailChannel_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{testConn: stubProbe(nil)})
	result, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "webhook",
	}))
	require.NoError(t, err)
	require.False(t, result.Body.GetOk())
	require.Contains(t, result.Body.GetError(), "email")
}

func TestTestConn_EmptyChannel_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{testConn: stubProbe(nil)})
	result, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "",
	}))
	require.NoError(t, err)
	require.False(t, result.Body.GetOk())
}

func TestTestConn_NilEmailConfig_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{testConn: stubProbe(nil)})
	result, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "email",
	}))
	require.NoError(t, err)
	require.False(t, result.Body.GetOk())
	require.Contains(t, result.Body.GetError(), "email_config is required")
}

func TestTestConn_ValidationError_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{testConn: stubProbe(nil)})
	result, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "email",
		EmailConfig: ptrext.Of(attunev1.EmailConnConfig{
			Host: "h", Port: 993, Tls: false, Username: "u", Password: "p",
		}),
	}))
	require.NoError(t, err)
	require.False(t, result.Body.GetOk())
	require.Contains(t, result.Body.GetError(), "tls")
}

func TestTestConn_Success_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{testConn: stubProbe(nil)})
	result, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "email",
		EmailConfig: ptrext.Of(attunev1.EmailConnConfig{
			Host: "imap.example.com", Port: 993, Tls: true,
			Username: "u", Password: "p",
		}),
	}))
	require.NoError(t, err)
	require.True(t, result.Body.GetOk())
	require.NotNil(t, result.Body.LatencyMs)
}

func TestTestConn_ProbeFails_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{testConn: stubProbe(errors.New("connection refused"))})
	result, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "email",
		EmailConfig: ptrext.Of(attunev1.EmailConnConfig{
			Host: "imap.example.com", Port: 993, Tls: true,
			Username: "u", Password: "p",
		}),
	}))
	require.NoError(t, err)
	require.False(t, result.Body.GetOk())
	require.Contains(t, result.Body.GetError(), "connection refused")
}

func TestTestConn_ChannelCaseInsensitive_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{testConn: stubProbe(nil)})
	result, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "  EMAIL  ",
		EmailConfig: ptrext.Of(attunev1.EmailConnConfig{
			Host: "h", Port: 993, Tls: true, Username: "u", Password: "p",
		}),
	}))
	require.NoError(t, err)
	require.True(t, result.Body.GetOk())
}

func TestTestConn_ProbeSuccess_RecordsAudit_DirectCall(t *testing.T) {
	t.Parallel()
	audit := ptrext.Of(covAuditRecorder{})
	h := ptrext.Of(Handler{
		testConn: stubProbe(nil),
		audit:    audit,
	})
	result, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "email",
		EmailConfig: ptrext.Of(attunev1.EmailConnConfig{
			Host: "imap.example.com", Port: 993, Tls: true,
			Username: "u", Password: "p",
		}),
	}))
	require.NoError(t, err)
	require.True(t, result.Body.GetOk())
	require.Len(t, audit.events, 1)
	require.Equal(t, "inbound_source.test_connection", audit.events[0].Action)
}

func TestTestConn_ProbeFail_RecordsAudit_DirectCall(t *testing.T) {
	t.Parallel()
	audit := ptrext.Of(covAuditRecorder{})
	h := ptrext.Of(Handler{
		testConn: stubProbe(errors.New("timeout")),
		audit:    audit,
	})
	result, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "email",
		EmailConfig: ptrext.Of(attunev1.EmailConnConfig{
			Host: "imap.example.com", Port: 993, Tls: true,
			Username: "u", Password: "p",
		}),
	}))
	require.NoError(t, err)
	require.False(t, result.Body.GetOk())
	require.Len(t, audit.events, 1)
}

func TestTestConn_AuditFailure_OnSuccess_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		testConn: stubProbe(nil),
		audit:    ptrext.Of(covFailingAuditRecorder{err: errors.New("audit db down")}),
	})
	_, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "email",
		EmailConfig: ptrext.Of(attunev1.EmailConnConfig{
			Host: "imap.example.com", Port: 993, Tls: true,
			Username: "u", Password: "p",
		}),
	}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestTestConn_AuditFailure_OnProbeError_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		testConn: stubProbe(errors.New("connection refused")),
		audit:    ptrext.Of(covFailingAuditRecorder{err: errors.New("audit db down")}),
	})
	_, err := h.TestConnection(covDirectCtx("tenant-1"), ptrext.Of(attunev1.TestInboundConnectionRequest{
		Channel: "email",
		EmailConfig: ptrext.Of(attunev1.EmailConnConfig{
			Host: "imap.example.com", Port: 993, Tls: true,
			Username: "u", Password: "p",
		}),
	}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

// ====================== validateEmailConnConfig ============================

func TestValidateEmailConnConfig_HappyPath(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.EmailConnConfig{
		Host: "imap.example.com", Port: 993, Tls: true,
		Username: "user", Password: "pass",
	})
	out, err := validateEmailConnConfig(cfg)
	require.NoError(t, err)
	require.Equal(t, "imap.example.com", out.Host)
	require.Equal(t, 993, out.Port)
}

func TestValidateEmailConnConfig_TLSFalse(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.EmailConnConfig{
		Host: "h", Port: 993, Tls: false, Username: "u", Password: "p",
	})
	_, err := validateEmailConnConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tls")
}

func TestValidateEmailConnConfig_EmptyHost(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.EmailConnConfig{
		Host: "", Port: 993, Tls: true, Username: "u", Password: "p",
	})
	_, err := validateEmailConnConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "host")
}

func TestValidateEmailConnConfig_PortZero(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.EmailConnConfig{
		Host: "h", Port: 0, Tls: true, Username: "u", Password: "p",
	})
	_, err := validateEmailConnConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "port")
}

func TestValidateEmailConnConfig_PortTooLarge(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.EmailConnConfig{
		Host: "h", Port: 65536, Tls: true, Username: "u", Password: "p",
	})
	_, err := validateEmailConnConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "port")
}

func TestValidateEmailConnConfig_EmptyUsername(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.EmailConnConfig{
		Host: "h", Port: 993, Tls: true, Username: "", Password: "p",
	})
	_, err := validateEmailConnConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "username")
}

func TestValidateEmailConnConfig_EmptyPassword(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.EmailConnConfig{
		Host: "h", Port: 993, Tls: true, Username: "u", Password: "",
	})
	_, err := validateEmailConnConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "password")
}

func TestValidateEmailConnConfig_BoundaryPorts(t *testing.T) {
	t.Parallel()
	for _, port := range []int32{1, 65535} {
		cfg := ptrext.Of(attunev1.EmailConnConfig{
			Host: "h", Port: port, Tls: true, Username: "u", Password: "p",
		})
		_, err := validateEmailConnConfig(cfg)
		require.NoError(t, err, "port %d should be accepted", port)
	}
}

// ====================== validateEmailCreateConfig extras ===================

// ====================== slugify edge cases =================================

// ====================== buildCurlExample ===================================

func TestBuildCurlExample_Headers(t *testing.T) {
	t.Parallel()
	got := buildCurlExample("https://example.com/v1/inbound/webhook/t/s")
	require.Contains(t, got, "X-Attune-Timestamp")
	require.Contains(t, got, "X-Attune-Signature")
	require.Contains(t, got, "Content-Type: application/json")
	require.Contains(t, got, "-X POST")
}

// ====================== channel constants ==================================

// ====================== listAllForTenant — nil pool ========================

func TestListAllForTenant_NilPool(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	_, err := h.listAllForTenant(context.Background(), "tenant-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "pool not configured")
}

// ====================== via HTTP helpers (additional) ======================

func TestPause_BadUUID_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := servePause(h, "not-a-uuid")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestResume_BadUUID_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveResume(h, "not-a-uuid")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRotate_BadUUID_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveRotate(h, "not-a-uuid")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDelete_BadUUID_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveDelete(h, "not-a-uuid")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPause_SourceNotFound_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{getErr: inboundsource.ErrNotFound})
	h := newTestHandler(repo, nil)
	w := servePause(h, uuid.NewString())
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestResume_SourceNotFound_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{getErr: inboundsource.ErrNotFound})
	h := newTestHandler(repo, nil)
	w := serveResume(h, uuid.NewString())
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRotate_SourceNotFound_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{getErr: inboundsource.ErrNotFound})
	h := newTestHandler(repo, nil)
	w := serveRotate(h, uuid.NewString())
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestPause_SetEnabledFails_ViaHTTP(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{
		getSrc:        inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook", Enabled: true},
		setEnabledErr: errors.New("db error"),
	})
	h := newTestHandler(repo, nil)
	w := servePause(h, id)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestResume_SetEnabledFails_ViaHTTP(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{
		getSrc:        inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook", Enabled: false},
		setEnabledErr: errors.New("db error"),
	})
	h := newTestHandler(repo, nil)
	w := serveResume(h, id)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRotate_InternalError_ViaHTTP(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{
		getSrc: inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook"},
	})
	h := newTestHandler(repo, nil)
	h.rotate = stubRotate(errors.New("kaboom"), time.Time{})
	w := serveRotate(h, id)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTestConnection_MissingEmailConfig_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveTestConnection(h, `{"channel":"email"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "email_config is required")
}

func TestTestConnection_ValidationError_TLSFalse_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	body := `{"channel":"email","emailConfig":{"host":"h","port":993,"tls":false,"username":"u","password":"p"}}`
	w := serveTestConnection(h, body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "tls")
}

func TestTestConnection_ValidationError_PortRange_ViaHTTP(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	body := `{"channel":"email","emailConfig":{"host":"h","port":99999,"tls":true,"username":"u","password":"p"}}`
	w := serveTestConnection(h, body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "port")
}
