// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/webhook"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
)

// fakeSourceRepo — in-memory inboundsource.Repo stand-in.
type fakeSourceRepo struct {
	getSrc        inbound.Source
	getErr        error
	setEnabledID  string
	setEnabledOn  bool
	setEnabledErr error
}

func (f *fakeSourceRepo) List(_ context.Context, _ string) ([]inbound.Source, error) {
	return nil, nil
}

func (f *fakeSourceRepo) Get(_ context.Context, _ string) (inbound.Source, error) {
	return f.getSrc, f.getErr
}

func (f *fakeSourceRepo) SetEnabled(_ context.Context, id string, enabled bool, _ string) error {
	f.setEnabledID = id
	f.setEnabledOn = enabled
	return f.setEnabledErr
}

// GetBySlugs + UpdateState fill out the inbound.SourceStore surface —
// the handler doesn't exercise these paths in unit tests but the
// interface is the framework's, so we satisfy it for the type-check.
func (f *fakeSourceRepo) GetBySlugs(_ context.Context, _, _, _ string) (inbound.Source, error) {
	return f.getSrc, f.getErr
}

func (f *fakeSourceRepo) UpdateState(_ context.Context, _ string, _ inbound.SourceState) error {
	return nil
}

// newTestHandler — wires a Handler against fakes. Delete uses h.pool
// directly (real pgx); tests that exercise Delete pass nil pool +
// expect 500 "pool not configured" or are deferred to the integration
// suite (review O10, #66).
func newTestHandler(repo *fakeSourceRepo, _ *struct{}) *Handler {
	return ptrext.Of(Handler{
		sources:    repo,
		secrets:    inboundtest.FakeSecrets{},
		baseURL:    "https://attune.example.com",
		rotate:     stubRotate(nil, time.Time{}),
		testConn:   stubProbe(nil),
		tenantSlug: func(context.Context, string) (string, error) { return "tenant-x", nil },
	})
}

func stubRotate(err error, next time.Time) rotator {
	return func(context.Context, *pgxpool.Pool, inbound.SecretStore, string) ([]byte, time.Time, error) {
		if err != nil {
			return nil, next, err
		}
		return []byte("freshly-rotated-secret-bytes!!"), next, nil
	}
}

func stubProbe(err error) testConnFn {
	return func(context.Context, testConnInputs) error { return err }
}

func authedRequest(method, urlStr, body, id string) *http.Request {
	r := httptest.NewRequest(method, urlStr, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	if id != "" {
		rctx.URLParams.Add("id", id)
	}
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	ctx = session.WithAuthCtx(ctx, ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
	}))
	return r.WithContext(ctx)
}

func serveGet(h *Handler, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler := dispatcher.Bind(
		"test.inbound.Get",
		dispatcher.Path(
			func() *attunev1.GetInboundSourceRequest { return ptrext.Of(attunev1.GetInboundSourceRequest{}) },
			dispatcher.Param("id", func(req *attunev1.GetInboundSourceRequest, id string) { req.Id = id }),
		),
		h.Get,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetInboundSourceRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	)
	handler(w, authedRequest(http.MethodGet, "/x", "", id))
	return w
}

func servePause(h *Handler, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler := dispatcher.Bind(
		"test.inbound.Pause",
		dispatcher.Path(
			func() *attunev1.PauseInboundSourceRequest { return ptrext.Of(attunev1.PauseInboundSourceRequest{}) },
			dispatcher.Param("id", func(req *attunev1.PauseInboundSourceRequest, id string) { req.Id = id }),
		),
		h.Pause,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PauseInboundSourceRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	)
	handler(w, authedRequest(http.MethodPost, "/x", "", id))
	return w
}

func serveResume(h *Handler, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler := dispatcher.Bind(
		"test.inbound.Resume",
		dispatcher.Path(
			func() *attunev1.ResumeInboundSourceRequest { return ptrext.Of(attunev1.ResumeInboundSourceRequest{}) },
			dispatcher.Param("id", func(req *attunev1.ResumeInboundSourceRequest, id string) { req.Id = id }),
		),
		h.Resume,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ResumeInboundSourceRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	)
	handler(w, authedRequest(http.MethodPost, "/x", "", id))
	return w
}

func serveDelete(h *Handler, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler := dispatcher.Bind(
		"test.inbound.Delete",
		dispatcher.Path(
			func() *attunev1.DeleteInboundSourceRequest { return ptrext.Of(attunev1.DeleteInboundSourceRequest{}) },
			dispatcher.Param("id", func(req *attunev1.DeleteInboundSourceRequest, id string) { req.Id = id }),
		),
		h.Delete,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteInboundSourceRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	)
	handler(w, authedRequest(http.MethodDelete, "/x", "", id))
	return w
}

func serveRotate(h *Handler, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler := dispatcher.Bind(
		"test.inbound.Rotate",
		dispatcher.Path(
			func() *attunev1.RotateInboundSourceSecretRequest {
				return ptrext.Of(attunev1.RotateInboundSourceSecretRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.RotateInboundSourceSecretRequest, id string) { req.Id = id }),
		),
		h.Rotate,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RotateInboundSourceSecretRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	)
	handler(w, authedRequest(http.MethodPost, "/x", "", id))
	return w
}

func serveTestConnection(h *Handler, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler := dispatcher.Bind(
		"test.inbound.TestConnection",
		dispatcher.JSON(func() *attunev1.TestInboundConnectionRequest {
			return ptrext.Of(attunev1.TestInboundConnectionRequest{})
		}),
		h.TestConnection,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TestInboundConnectionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	)
	handler(w, authedRequest(http.MethodPost, "/x", body, ""))
	return w
}

func serveCreate(h *Handler, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler := dispatcher.Bind(
		"test.inbound.Create",
		dispatcher.JSON(func() *attunev1.CreateInboundSourceRequest { return ptrext.Of(attunev1.CreateInboundSourceRequest{}) }),
		h.Create,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateInboundSourceRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	)
	handler(w, authedRequest(http.MethodPost, "/x", body, ""))
	return w
}

// --- pure helpers ----------------------------------------------------

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"My Webhook", "my-webhook"},
		{"Customer Support // Inbox", "customer-support-inbox"},
		{"   double-space   bar  ", "double-space-bar"},
		{"!!!", ""},
		{"", ""},
		{"ALLCAPS123", "allcaps123"},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestValidateEmailCreateConfig_HappyPath(t *testing.T) {
	cfg := ptrext.Of(attunev1.EmailCreateConfig{
		Host:     "imap.example.com",
		Port:     993,
		Tls:      true,
		Username: "ops@example.com",
		Password: "secret-pw",
		Folder:   "Custom",
	})
	if err := validateEmailCreateConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestValidateEmailCreateConfig_Validation(t *testing.T) {
	bad := []struct {
		name string
		cfg  *attunev1.EmailCreateConfig
		want string
	}{
		{
			"empty host",
			ptrext.Of(attunev1.EmailCreateConfig{Port: 993, Tls: true, Username: "u", Password: "p"}),
			"host",
		},
		{
			"port 0",
			ptrext.Of(attunev1.EmailCreateConfig{Host: "h", Port: 0, Tls: true, Username: "u", Password: "p"}),
			"port",
		},
		{
			"port too big",
			ptrext.Of(attunev1.EmailCreateConfig{Host: "h", Port: 70000, Tls: true, Username: "u", Password: "p"}),
			"port",
		},
		{
			"tls false rejected (review H2, #66)",
			ptrext.Of(attunev1.EmailCreateConfig{Host: "h", Port: 993, Tls: false, Username: "u", Password: "p"}),
			"tls",
		},
		{
			"no username",
			ptrext.Of(attunev1.EmailCreateConfig{Host: "h", Port: 993, Tls: true, Password: "p"}),
			"username",
		},
		{
			"no password",
			ptrext.Of(attunev1.EmailCreateConfig{Host: "h", Port: 993, Tls: true, Username: "u"}),
			"password",
		},
		{
			"bad start_from",
			ptrext.Of(attunev1.EmailCreateConfig{
				Host: "h", Port: 993, Tls: true, Username: "u", Password: "p", StartFrom: "junk",
			}),
			"start_from",
		},
	}
	for _, c := range bad {
		err := validateEmailCreateConfig(c.cfg)
		if err == nil {
			t.Errorf("%s: expected error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want err containing %q, got %q", c.name, c.want, err.Error())
		}
	}
}

// --- endpoint tests --------------------------------------------------

func TestGet_404(t *testing.T) {
	repo := ptrext.Of(fakeSourceRepo{getErr: inboundsource.ErrNotFound})
	h := newTestHandler(repo, nil)
	w := serveGet(h, uuid.NewString())
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestGet_CrossTenant(t *testing.T) {
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{getSrc: inbound.Source{
		ID: id, TenantID: "tenant-OTHER", Channel: "webhook", Name: "n", Slug: "n", Enabled: true,
	}})
	h := newTestHandler(repo, nil)
	w := serveGet(h, id)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant must 404, got %d", w.Code)
	}
}

func TestGet_HappyPath(t *testing.T) {
	id := uuid.NewString()
	ts := time.Now()
	repo := ptrext.Of(fakeSourceRepo{getSrc: inbound.Source{
		ID:       id,
		TenantID: "tenant-1",
		Channel:  "webhook",
		Name:     "Public form",
		Slug:     "public-form",
		Enabled:  true,
		State:    inbound.SourceState{LastEventAt: ptrext.Of(ts)},
	}})
	h := newTestHandler(repo, nil)
	w := serveGet(h, id)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var out attunev1.InboundSource
	if err := protojson.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("proto decode: %v\nbody: %s", err, w.Body.String())
	}
	if out.GetId() != id || out.GetChannel() != "webhook" {
		t.Fatalf("unexpected detail: %+v", out.String())
	}
	if out.LastEventAt == nil {
		t.Fatal("LastEventAt should be set")
	}
}

func TestPause_FlipsEnabled(t *testing.T) {
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{getSrc: inbound.Source{
		ID: id, TenantID: "tenant-1", Channel: "webhook", Enabled: true,
	}})
	h := newTestHandler(repo, nil)
	w := servePause(h, id)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.setEnabledID != id || repo.setEnabledOn {
		t.Fatalf("SetEnabled not invoked with (id, false); got id=%q enabled=%v",
			repo.setEnabledID, repo.setEnabledOn)
	}
}

func TestResume_FlipsEnabled(t *testing.T) {
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{getSrc: inbound.Source{
		ID: id, TenantID: "tenant-1", Channel: "webhook", Enabled: false,
	}})
	h := newTestHandler(repo, nil)
	w := serveResume(h, id)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !repo.setEnabledOn {
		t.Fatalf("Resume should enable; got enabled=%v", repo.setEnabledOn)
	}
}

func TestDelete_404OnMissing(t *testing.T) {
	repo := ptrext.Of(fakeSourceRepo{getErr: inboundsource.ErrNotFound})
	h := newTestHandler(repo, nil)
	w := serveDelete(h, uuid.NewString())
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

// TestDelete_HappyPath and TestDelete_RaceLost moved to the testdb
// integration suite — Delete now drives pgxpool.Pool directly (no exec
// seam, review O10 #66) so the happy / race branches are exercised
// against a real Postgres instead of a fake.

func TestDelete_NoPoolReturns500(t *testing.T) {
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{getSrc: inbound.Source{
		ID: id, TenantID: "tenant-1", Channel: "webhook", Enabled: true,
	}})
	h := newTestHandler(repo, nil) // pool stays nil
	w := serveDelete(h, id)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil-pool Delete must 500; got %d", w.Code)
	}
}

func TestRotate_RejectsEmailChannel(t *testing.T) {
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{getSrc: inbound.Source{
		ID: id, TenantID: "tenant-1", Channel: "email",
	}})
	h := newTestHandler(repo, nil)
	w := serveRotate(h, id)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for email rotate, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRotate_GraceWindow_Returns409(t *testing.T) {
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{getSrc: inbound.Source{
		ID: id, TenantID: "tenant-1", Channel: "webhook",
	}})
	next := time.Now().Add(2 * time.Hour)
	h := newTestHandler(repo, nil)
	h.rotate = stubRotate(webhook.ErrRotationInGraceWindow, next)
	w := serveRotate(h, id)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", w.Code, w.Body.String())
	}
	// Conflict body uses the standard error envelope, not the success
	// proto — the error code identifies the grace-window case.
	if !strings.Contains(w.Body.String(), "ROTATION_IN_GRACE_WINDOW") {
		t.Fatalf("want ROTATION_IN_GRACE_WINDOW error code; got %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "nextEligibleAt") {
		t.Fatalf("error envelope should not include ad-hoc top-level fields; got %s", w.Body.String())
	}
}

func TestRotate_HappyPath(t *testing.T) {
	id := uuid.NewString()
	repo := ptrext.Of(fakeSourceRepo{getSrc: inbound.Source{
		ID: id, TenantID: "tenant-1", Channel: "webhook",
	}})
	h := newTestHandler(repo, nil)
	w := serveRotate(h, id)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var out attunev1.RotateInboundSourceSecretResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("proto decode: %v\nbody: %s", err, w.Body.String())
	}
	if out.GetSecretHex() == "" {
		t.Fatal("missing secret in response")
	}
	if _, err := hex.DecodeString(out.GetSecretHex()); err != nil {
		t.Fatalf("secret not hex: %v", err)
	}
	if out.GetNextEligibleAt() == "" {
		t.Fatal("missing next_eligible_at")
	}
}

func TestTestConnection_EmailHappyPath(t *testing.T) {
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	// tls:true is mandatory post-H2 (review fix #66); plain IMAP is disallowed.
	body := `{"channel":"email","emailConfig":{"host":"h","port":993,"tls":true,"username":"u","password":"p"}}`
	w := serveTestConnection(h, body)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var out attunev1.TestInboundConnectionResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("proto decode: %v\nbody: %s", err, w.Body.String())
	}
	if !out.GetOk() {
		t.Fatalf("expected ok=true, got %+v", out.String())
	}
}

func TestTestConnection_DialFailure_Surfaces(t *testing.T) {
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	h.testConn = stubProbe(errors.New("connection refused"))
	// tls:true is mandatory post-H2 (review fix #66); plain IMAP is disallowed.
	body := `{"channel":"email","emailConfig":{"host":"h","port":993,"tls":true,"username":"u","password":"p"}}`
	w := serveTestConnection(h, body)
	if w.Code != http.StatusOK {
		t.Fatalf("test-connection MUST never 500; got %d", w.Code)
	}
	var out attunev1.TestInboundConnectionResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("proto decode: %v\nbody: %s", err, w.Body.String())
	}
	if out.GetOk() {
		t.Fatalf("expected ok=false; got %+v", out.String())
	}
	if !strings.Contains(out.GetError(), "connection refused") {
		t.Fatalf("expected dial error; got %q", out.GetError())
	}
}

func TestTestConnection_RejectsWebhookChannel(t *testing.T) {
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	body := `{"channel":"webhook"}`
	w := serveTestConnection(h, body)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (never-500 contract), got %d", w.Code)
	}
	var out attunev1.TestInboundConnectionResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("proto decode: %v", err)
	}
	if out.GetOk() {
		t.Fatalf("want ok=false; got %+v", out.String())
	}
}

func TestTestConnection_RejectsBadJSON(t *testing.T) {
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveTestConnection(h, `not-json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"BAD_REQUEST"`) {
		t.Fatalf("want BAD_REQUEST envelope, got %s", w.Body.String())
	}
}

func TestCreate_RejectsUnknownChannel(t *testing.T) {
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveCreate(h, `{"channel":"bogus","name":"X"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreate_RejectsBadJSON(t *testing.T) {
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveCreate(h, `{not-json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestCreate_RejectsEmptyName(t *testing.T) {
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveCreate(h, `{"channel":"webhook","name":"   "}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestCreate_RejectsNonAlphanumericName(t *testing.T) {
	repo := ptrext.Of(fakeSourceRepo{})
	h := newTestHandler(repo, nil)
	w := serveCreate(h, `{"channel":"webhook","name":"!!!"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBuildCurlExample_HasURL(t *testing.T) {
	got := buildCurlExample("https://example.com/v1/inbound/webhook/t/s")
	if !strings.Contains(got, "https://example.com/v1/inbound/webhook/t/s") {
		t.Fatalf("URL not embedded in curl example: %q", got)
	}
}
