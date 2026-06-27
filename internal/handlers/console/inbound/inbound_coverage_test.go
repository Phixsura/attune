// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// ---------- extended fakes (different names to avoid collision) ----------

// coverageFakeSourceRepo extends the scenario space beyond the existing
// fakeSourceRepo: it allows per-call List results plus a separate Get
// result for the post-toggle reload path.
type coverageFakeSourceRepo struct {
	listSrcs      []inbound.Source
	listErr       error
	getSrc        inbound.Source
	getErr        error
	reloadSrc     inbound.Source // returned by Get on the second call
	reloadErr     error
	reloadCalled  bool
	setEnabledID  string
	setEnabledOn  bool
	setEnabledErr error
}

func (f *coverageFakeSourceRepo) List(_ context.Context, _ string) ([]inbound.Source, error) {
	return f.listSrcs, f.listErr
}

func (f *coverageFakeSourceRepo) Get(_ context.Context, _ string) (inbound.Source, error) {
	if f.reloadCalled {
		return f.reloadSrc, f.reloadErr
	}
	return f.getSrc, f.getErr
}

func (f *coverageFakeSourceRepo) SetEnabled(_ context.Context, id string, enabled bool, _ string) error {
	f.setEnabledID = id
	f.setEnabledOn = enabled
	f.reloadCalled = true
	return f.setEnabledErr
}

func (f *coverageFakeSourceRepo) GetBySlugs(_ context.Context, _, _, _ string) (inbound.Source, error) {
	return f.getSrc, f.getErr
}

func (f *coverageFakeSourceRepo) UpdateState(_ context.Context, _ string, _ inbound.SourceState) error {
	return nil
}

// failingAuditRecorder always returns an error on Record.
type failingAuditRecorder struct {
	err error
}

func (f *failingAuditRecorder) Record(_ context.Context, _ auditlogsvc.Event) error {
	return f.err
}

// --- helpers for direct handler calls ---

func directCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	})
}

// ====================== pure function tests ============================

func TestRowToProto_FullSource(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 15, 10, 30, 0, 0, time.UTC)
	evtAt := now.Add(-2 * time.Hour)
	src := inbound.Source{
		ID:        "id-1",
		TenantID:  "t-1",
		Channel:   "webhook",
		Name:      "My Source",
		Slug:      "my-source",
		Enabled:   true,
		State:     inbound.SourceState{LastEventAt: ptrext.Of(evtAt), LastUID: 42, LastError: "oops"},
		CreatedAt: now,
		UpdatedAt: now.Add(time.Minute),
	}
	out := rowToProto(src)
	require.Equal(t, "id-1", out.GetId())
	require.Equal(t, "t-1", out.GetTenantId())
	require.Equal(t, "webhook", out.GetChannel())
	require.Equal(t, "My Source", out.GetName())
	require.Equal(t, "my-source", out.GetSlug())
	require.True(t, out.GetEnabled())
	require.Equal(t, int64(42), out.GetLastUid())
	require.Equal(t, "oops", out.GetLastError())
	require.NotNil(t, out.LastEventAt)
	require.Contains(t, ptrext.Indirect(out.LastEventAt), "2026-06-15")
	require.Contains(t, out.GetCreatedAt(), "2026-06-15")
	require.Contains(t, out.GetUpdatedAt(), "2026-06-15")
}

func TestRowToProto_ZeroTimestamps(t *testing.T) {
	t.Parallel()
	src := inbound.Source{
		ID:       "id-3",
		TenantID: "t-3",
		Channel:  "webhook",
		Name:     "Zero",
		Slug:     "zero",
	}
	out := rowToProto(src)
	// Zero time should still format to a valid RFC3339 string.
	require.NotEmpty(t, out.GetCreatedAt())
	require.NotEmpty(t, out.GetUpdatedAt())
}

func TestIsUUID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid UUID", uuid.NewString(), true},
		{"another valid UUID", "550e8400-e29b-41d4-a716-446655440000", true},
		{"invalid string", "not-a-uuid", false},
		{"empty string", "", false},
		{"almost UUID", "550e8400-e29b-41d4-a716", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, isUUID(tc.input))
		})
	}
}

func TestValidateEmailConnConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     *attunev1.EmailConnConfig
		wantErr string
	}{
		{
			name: "happy path",
			cfg: ptrext.Of(attunev1.EmailConnConfig{
				Host: "imap.example.com", Port: 993, Tls: true,
				Username: "user", Password: "pass",
			}),
		},
		{
			name: "tls false",
			cfg: ptrext.Of(attunev1.EmailConnConfig{
				Host: "h", Port: 993, Tls: false,
				Username: "u", Password: "p",
			}),
			wantErr: "tls",
		},
		{
			name: "empty host",
			cfg: ptrext.Of(attunev1.EmailConnConfig{
				Host: "", Port: 993, Tls: true,
				Username: "u", Password: "p",
			}),
			wantErr: "host",
		},
		{
			name: "port zero",
			cfg: ptrext.Of(attunev1.EmailConnConfig{
				Host: "h", Port: 0, Tls: true,
				Username: "u", Password: "p",
			}),
			wantErr: "port",
		},
		{
			name: "port too large",
			cfg: ptrext.Of(attunev1.EmailConnConfig{
				Host: "h", Port: 65536, Tls: true,
				Username: "u", Password: "p",
			}),
			wantErr: "port",
		},
		{
			name: "empty username",
			cfg: ptrext.Of(attunev1.EmailConnConfig{
				Host: "h", Port: 993, Tls: true,
				Username: "", Password: "p",
			}),
			wantErr: "username",
		},
		{
			name: "empty password",
			cfg: ptrext.Of(attunev1.EmailConnConfig{
				Host: "h", Port: 993, Tls: true,
				Username: "u", Password: "",
			}),
			wantErr: "password",
		},
		{
			name: "host with spaces trimmed",
			cfg: ptrext.Of(attunev1.EmailConnConfig{
				Host: "  imap.example.com  ", Port: 993, Tls: true,
				Username: "u", Password: "p",
			}),
		},
		{
			name: "boundary port 1",
			cfg: ptrext.Of(attunev1.EmailConnConfig{
				Host: "h", Port: 1, Tls: true,
				Username: "u", Password: "p",
			}),
		},
		{
			name: "boundary port 65535",
			cfg: ptrext.Of(attunev1.EmailConnConfig{
				Host: "h", Port: 65535, Tls: true,
				Username: "u", Password: "p",
			}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := validateEmailConnConfig(tc.cfg)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, out.Host)
			}
		})
	}
}

func TestValidateEmailConnConfig_NormalisesFields(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.EmailConnConfig{
		Host:     "  mail.example.com  ",
		Port:     993,
		Tls:      true,
		Username: "  ops@example.com  ",
		Password: "secret",
		Folder:   "  Archive  ",
	})
	out, err := validateEmailConnConfig(cfg)
	require.NoError(t, err)
	require.Equal(t, "mail.example.com", out.Host)
	require.Equal(t, "ops@example.com", out.Username)
	require.Equal(t, "Archive", out.Folder)
	require.True(t, out.TLS)
	require.Equal(t, 993, out.Port)
}

func TestBuildCurlExample_SpecialChars(t *testing.T) {
	t.Parallel()
	url := `https://example.com/v1/inbound/webhook/t/my source`
	got := buildCurlExample(url)
	require.Contains(t, got, "curl -X POST")
	require.Contains(t, got, "X-Attune-Signature")
	require.Contains(t, got, "X-Attune-Timestamp")
	require.Contains(t, got, "Content-Type: application/json")
}

func TestBuildCurlExample_EmptyURL(t *testing.T) {
	t.Parallel()
	got := buildCurlExample("")
	require.Contains(t, got, "curl -X POST")
}

// =================== recordAudit tests ==================================

func TestRecordAudit_NilAudit_NoOp(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{}) // audit is nil
	err := h.recordAudit(
		context.Background(),
		"admin", "user-1", "tenant-1", "test.action", "target-1", "summary",
		nil, nil, nil,
	)
	require.NoError(t, err)
}

func TestRecordAudit_EmptyAuthType_DefaultsToAdmin(t *testing.T) {
	t.Parallel()
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{audit: audit})
	err := h.recordAudit(
		context.Background(),
		"", "user-1", "tenant-1", "test.action", "target-1", "summary",
		nil, nil, nil,
	)
	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "admin", audit.events[0].Actor.Type)
}

func TestRecordAudit_NonEmptyAuthType_Preserved(t *testing.T) {
	t.Parallel()
	audit := ptrext.Of(fakeAuditRecorder{})
	h := ptrext.Of(Handler{audit: audit})
	err := h.recordAudit(
		context.Background(),
		"oidc", "user-2", "tenant-2", "test.action", "target-2", "summary",
		nil, nil, nil,
	)
	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	require.Equal(t, "oidc", audit.events[0].Actor.Type)
}

// =================== handler endpoint tests via HTTP dispatch ============

func TestPause_SourceNotFound(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{getErr: inboundsource.ErrNotFound})
	h := newTestHandler(repo, nil)
	w := servePause(h, uuid.NewString())
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestPause_SetEnabledFails(t *testing.T) {
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

func TestResume_SourceNotFound(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{getErr: inboundsource.ErrNotFound})
	h := newTestHandler(repo, nil)
	w := serveResume(h, uuid.NewString())
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRotate_InternalError(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{getSrc: inbound.Source{
		ID: id, TenantID: "tenant-1", Channel: "webhook",
	}})
	h := newTestHandler(repo, nil)
	h.rotate = stubRotate(errors.New("kaboom"), time.Time{})
	w := serveRotate(h, id)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTestConnection_MissingEmailConfig(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	body := `{"channel":"email"}`
	w := serveTestConnection(h, body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "email_config is required")
}

func TestTestConnection_ValidationError_PortRange(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	body := `{"channel":"email","emailConfig":{"host":"h","port":99999,"tls":true,"username":"u","password":"p"}}`
	w := serveTestConnection(h, body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "port")
}

func TestTestConnection_ValidationError_TLSFalse(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	body := `{"channel":"email","emailConfig":{"host":"h","port":993,"tls":false,"username":"u","password":"p"}}`
	w := serveTestConnection(h, body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "tls")
}

// =================== direct handler method calls ========================

func TestGet_BadID_DirectCall(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	_, err := h.Get(directCtx(), ptrext.Of(attunev1.GetInboundSourceRequest{Id: "not-a-uuid"}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusBadRequest, de.Status)
}

func TestGet_InternalError_DirectCall(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{getErr: errors.New("db connection lost")})
	h := newTestHandler(repo, nil)
	_, err := h.Get(directCtx(), ptrext.Of(attunev1.GetInboundSourceRequest{Id: uuid.NewString()}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestPause_AuditFailure_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(coverageFakeSourceRepo{
		getSrc:    inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook", Enabled: true},
		reloadSrc: inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook", Enabled: false},
	})
	h := ptrext.Of(Handler{
		sources:  repo,
		secrets:  inboundtest.FakeSecrets{},
		baseURL:  "https://attune.example.com",
		audit:    ptrext.Of(failingAuditRecorder{err: errors.New("audit db down")}),
		rotate:   stubRotate(nil, time.Time{}),
		testConn: stubProbe(nil),
	})
	_, err := h.Pause(directCtx(), ptrext.Of(attunev1.PauseInboundSourceRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestResume_ReloadFails_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(coverageFakeSourceRepo{
		getSrc:    inbound.Source{ID: id, TenantID: "tenant-1", Channel: "webhook", Enabled: false},
		reloadErr: errors.New("reload failed"),
	})
	h := ptrext.Of(Handler{
		sources:  repo,
		secrets:  inboundtest.FakeSecrets{},
		baseURL:  "https://attune.example.com",
		rotate:   stubRotate(nil, time.Time{}),
		testConn: stubProbe(nil),
	})
	_, err := h.Resume(directCtx(), ptrext.Of(attunev1.ResumeInboundSourceRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestRotate_AuditFailure_DirectCall(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{getSrc: inbound.Source{
		ID: id, TenantID: "tenant-1", Channel: "webhook",
	}})
	h := ptrext.Of(Handler{
		sources:  repo,
		secrets:  inboundtest.FakeSecrets{},
		baseURL:  "https://attune.example.com",
		rotate:   stubRotate(nil, time.Now().Add(24*time.Hour)),
		testConn: stubProbe(nil),
		audit:    ptrext.Of(failingAuditRecorder{err: errors.New("audit db down")}),
	})
	_, err := h.Rotate(directCtx(), ptrext.Of(attunev1.RotateInboundSourceSecretRequest{Id: id}))
	require.Error(t, err)
	var de *dispatcher.Error
	require.True(t, errors.As(err, &de))
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestTestConnection_AuditFailure_OnSuccess_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		testConn: stubProbe(nil),
		audit:    ptrext.Of(failingAuditRecorder{err: errors.New("audit db down")}),
	})
	_, err := h.TestConnection(directCtx(), ptrext.Of(attunev1.TestInboundConnectionRequest{
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

func TestTestConnection_AuditFailure_OnProbeError_DirectCall(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{
		testConn: stubProbe(errors.New("connection refused")),
		audit:    ptrext.Of(failingAuditRecorder{err: errors.New("audit db down")}),
	})
	_, err := h.TestConnection(directCtx(), ptrext.Of(attunev1.TestInboundConnectionRequest{
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

func TestGet_Via_ServeGet_BadUUID(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveGet(h, "not-a-uuid")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRotate_SourceNotFound(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{getErr: inboundsource.ErrNotFound})
	h := newTestHandler(repo, nil)
	w := serveRotate(h, uuid.NewString())
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDelete_SourceNotFound_BadUUID(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveDelete(h, "not-a-uuid")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSetAuditLogger(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	require.Nil(t, h.audit)
	audit := ptrext.Of(fakeAuditRecorder{})
	h.SetAuditLogger(audit)
	require.NotNil(t, h.audit)
}

func TestResume_SetEnabledFails(t *testing.T) {
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

func TestPause_BadUUID(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := servePause(h, "not-a-uuid")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestResume_BadUUID(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveResume(h, "not-a-uuid")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRotate_BadUUID(t *testing.T) {
	t.Parallel()
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveRotate(h, "not-a-uuid")
	require.Equal(t, http.StatusBadRequest, w.Code)
}
