// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
)

type loginAdminStore struct {
	row      admin.Admin
	emailErr error
	resetErr error
	incErr   error

	getEmails    []string
	resetIDs     []string
	incrementIDs []string
}

func (s *loginAdminStore) GetByEmail(_ context.Context, email string) (admin.Admin, error) {
	s.getEmails = append(s.getEmails, email)
	if s.emailErr != nil {
		return admin.Admin{}, s.emailErr
	}
	return s.row, nil
}

func (s *loginAdminStore) GetByID(_ context.Context, id string) (admin.Admin, error) {
	if s.row.ID == id {
		return s.row, nil
	}
	return admin.Admin{}, admin.ErrNotFound
}

func (s *loginAdminStore) IncrementFailedAttempts(_ context.Context, id string) error {
	s.incrementIDs = append(s.incrementIDs, id)
	return s.incErr
}

func (s *loginAdminStore) ResetFailedAttempts(_ context.Context, id string) error {
	s.resetIDs = append(s.resetIDs, id)
	return s.resetErr
}

type authenticateCase struct {
	name           string
	store          *loginAdminStore
	password       string
	wantStatus     int
	wantCode       attunev1.ErrorCode
	wantAdminID    string
	wantResetIDs   []string
	wantIncIDs     []string
	wantEmailCalls []string
}

func TestHandlerAuthenticateRejectBranches(t *testing.T) {
	validHash, err := HashPassword("correct-password")
	require.NoError(t, err)
	lockedFuture := time.Now().Add(time.Hour)

	tests := []authenticateCase{
		{
			name: "unknown email is unauthorized",
			store: ptrext.Of(loginAdminStore{
				emailErr: admin.ErrNotFound,
			}),
			password:       "correct-password",
			wantStatus:     http.StatusUnauthorized,
			wantCode:       attunev1.ErrorCode_UNAUTHORIZED,
			wantEmailCalls: []string{"admin@example.test"},
		},
		{
			name: "email lookup error is internal",
			store: ptrext.Of(loginAdminStore{
				emailErr: errors.New("database unavailable"),
			}),
			password:       "correct-password",
			wantStatus:     http.StatusInternalServerError,
			wantCode:       attunev1.ErrorCode_INTERNAL,
			wantEmailCalls: []string{"admin@example.test"},
		},
		{
			name: "active lock is locked",
			store: ptrext.Of(loginAdminStore{row: admin.Admin{
				ID:           "admin-1",
				PasswordHash: validHash,
				LockedUntil:  ptrext.Of(lockedFuture),
			}}),
			password:       "correct-password",
			wantStatus:     http.StatusLocked,
			wantCode:       attunev1.ErrorCode_LOCKED,
			wantEmailCalls: []string{"admin@example.test"},
		},
		{
			name: "wrong password increments attempts",
			store: ptrext.Of(loginAdminStore{row: admin.Admin{
				ID:           "admin-1",
				PasswordHash: validHash,
			}}),
			password:       "wrong-password",
			wantStatus:     http.StatusUnauthorized,
			wantCode:       attunev1.ErrorCode_UNAUTHORIZED,
			wantIncIDs:     []string{"admin-1"},
			wantEmailCalls: []string{"admin@example.test"},
		},
		{
			name: "increment error still returns unauthorized",
			store: ptrext.Of(loginAdminStore{
				row: admin.Admin{
					ID:           "admin-1",
					PasswordHash: validHash,
				},
				incErr: errors.New("increment failed"),
			}),
			password:       "wrong-password",
			wantStatus:     http.StatusUnauthorized,
			wantCode:       attunev1.ErrorCode_UNAUTHORIZED,
			wantIncIDs:     []string{"admin-1"},
			wantEmailCalls: []string{"admin@example.test"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runAuthenticateCase(t, tc)
		})
	}
}

func TestHandlerAuthenticateSuccessBranches(t *testing.T) {
	validHash, err := HashPassword("correct-password")
	require.NoError(t, err)
	lockedPast := time.Now().Add(-time.Hour)

	tests := []authenticateCase{
		{
			name: "expired lock resets before success",
			store: ptrext.Of(loginAdminStore{row: admin.Admin{
				ID:             "admin-1",
				PasswordHash:   validHash,
				FailedAttempts: 5,
				LockedUntil:    ptrext.Of(lockedPast),
			}}),
			password:       "correct-password",
			wantAdminID:    "admin-1",
			wantResetIDs:   []string{"admin-1", "admin-1"},
			wantEmailCalls: []string{"admin@example.test"},
		},
		{
			name: "success resets attempts",
			store: ptrext.Of(loginAdminStore{row: admin.Admin{
				ID:           "admin-1",
				PasswordHash: validHash,
			}}),
			password:       "correct-password",
			wantAdminID:    "admin-1",
			wantResetIDs:   []string{"admin-1"},
			wantEmailCalls: []string{"admin@example.test"},
		},
		{
			name: "reset error still authenticates",
			store: ptrext.Of(loginAdminStore{
				row: admin.Admin{
					ID:           "admin-1",
					PasswordHash: validHash,
				},
				resetErr: errors.New("reset failed"),
			}),
			password:       "correct-password",
			wantAdminID:    "admin-1",
			wantResetIDs:   []string{"admin-1"},
			wantEmailCalls: []string{"admin@example.test"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runAuthenticateCase(t, tc)
		})
	}
}

func runAuthenticateCase(t *testing.T, tc authenticateCase) {
	t.Helper()
	h := ptrext.Of(Handler{admins: tc.store})
	req := ptrext.Of(attunev1.LoginRequest{
		Email:    "admin@example.test",
		Password: tc.password,
	})

	got, err := h.authenticate(context.Background(), req)

	if tc.wantStatus != 0 {
		require.Error(t, err)
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, tc.wantStatus, de.Status)
		require.Equal(t, tc.wantCode, de.Code)
		require.Empty(t, got.ID)
	} else {
		require.NoError(t, err)
		require.Equal(t, tc.wantAdminID, got.ID)
	}
	require.Equal(t, tc.wantEmailCalls, tc.store.getEmails)
	require.Equal(t, tc.wantResetIDs, tc.store.resetIDs)
	require.Equal(t, tc.wantIncIDs, tc.store.incrementIDs)
}

func TestNewChangePasswordHandlerKeepsNonNilStore(t *testing.T) {
	signer, err := session.NewSigner(strings.Repeat("c", 32))
	require.NoError(t, err)

	h := NewChangePasswordHandler(admin.NewRepo(nil), signer)

	require.NotNil(t, h)
	require.NotNil(t, h.admins)
	require.Same(t, signer, h.signer)
}

func TestHandlerLoginIssuesSessionAndRedirects(t *testing.T) {
	validHash, err := HashPassword("correct-password")
	require.NoError(t, err)

	tests := []struct {
		name         string
		redirectURI  string
		wantRedirect string
		wantTenantID string
	}{
		{
			name:         "safe redirect is preserved",
			redirectURI:  "/console/settings",
			wantRedirect: "/console/settings",
			wantTenantID: "tenant-1",
		},
		{
			name:         "unsafe redirect falls back to console",
			redirectURI:  "https://evil.example.test/console/settings",
			wantRedirect: "/console/",
			wantTenantID: "tenant-1",
		},
		{
			name:         "tenantless login keeps empty tenant scope",
			redirectURI:  "",
			wantRedirect: "/console/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := ptrext.Of(loginAdminStore{row: admin.Admin{
				ID:           "admin-1",
				PasswordHash: validHash,
			}})
			memberSync := ptrext.Of(fakeAdminMembershipStore{})
			h := newLoginTestHandler(t, store, memberSync, tc.wantTenantID)
			rec := serveLoginWithHandler(t, h, loginBody(tc.redirectURI))

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, tc.wantRedirect, loginRedirect(t, rec.Body.Bytes()))
			require.Equal(t, []string{"admin-1"}, store.resetIDs)
			assertLoginSessionCookie(t, h.signer, rec.Result().Cookies(), tc.wantTenantID, "admin-1")
			if tc.wantTenantID == "" {
				require.Equal(t, 0, memberSync.calls)
			} else {
				require.Equal(t, 1, memberSync.calls)
				require.Equal(t, tc.wantTenantID, memberSync.tenantID)
				require.Equal(t, "admin-1", memberSync.userID)
			}
		})
	}
}

func newLoginTestHandler(
	t *testing.T,
	store *loginAdminStore,
	memberSync *fakeAdminMembershipStore,
	tenantID string,
) *Handler {
	t.Helper()
	signer, err := session.NewSigner(strings.Repeat("l", 32))
	require.NoError(t, err)
	h := ptrext.Of(Handler{
		signer:  signer,
		admins:  store,
		members: memberSync,
		baseURL: "https://console.example.test",
	})
	if tenantID != "" {
		h.tenants = ptrext.Of(fakeTenantScopeResolver{id: tenantID})
	}
	return h
}

func serveLoginWithHandler(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler := dispatcher.Bind(
		"test.LoginBranches",
		dispatcher.Combine(
			func() *attunev1.LoginRequest {
				return ptrext.Of(attunev1.LoginRequest{})
			},
			dispatcher.JSONBody[*attunev1.LoginRequest],
			h.ValidateRequest,
		),
		h.Login,
		dispatcher.WithAuth(func(_ *http.Request, _ *attunev1.LoginRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
		dispatcher.WithBefore[struct{}](func(ctx *dispatcher.RequestContext[struct{}], req *attunev1.LoginRequest) error {
			if ctx.Request() == nil {
				return nil
			}
			return h.RequireLoginOrigin(ctx.Request(), req)
		}),
	)
	req := httptest.NewRequest(http.MethodPost, "/install/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://console.example.test")
	handler(rec, req)
	return rec
}

func loginBody(redirectURI string) string {
	return `{"email":"admin@example.test","password":"correct-password","redirectUri":"` + redirectURI + `"}`
}

func loginRedirect(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Redirect string `json:"redirect"`
	}
	require.NoError(t, json.Unmarshal(body, &resp), string(body))
	return resp.Redirect
}

func assertLoginSessionCookie(
	t *testing.T,
	signer *session.Signer,
	cookies []*http.Cookie,
	wantTenantID string,
	wantUserID string,
) {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name != session.SessionCookieName {
			continue
		}
		payload, err := signer.VerifySession(cookie.Value)
		require.NoError(t, err)
		require.Equal(t, wantTenantID, payload.TenantID)
		require.Equal(t, wantUserID, payload.UserID)
		require.True(t, cookie.HttpOnly)
		require.True(t, cookie.Secure)
		return
	}
	require.Fail(t, "session cookie was not issued")
}
