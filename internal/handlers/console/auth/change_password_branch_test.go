// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
)

type fakePasswordAdminStore struct {
	row       admin.Admin
	getErr    error
	updateErr error

	updateID   string
	updateHash string
}

func (f *fakePasswordAdminStore) GetByID(context.Context, string) (admin.Admin, error) {
	if f.getErr != nil {
		return admin.Admin{}, f.getErr
	}
	return f.row, nil
}

func (f *fakePasswordAdminStore) UpdatePasswordHash(_ context.Context, id, newHash string) error {
	f.updateID = id
	f.updateHash = newHash
	return f.updateErr
}

func TestChangePasswordHandlerBranches(t *testing.T) {
	validHash, err := HashPassword("current-secret-value")
	require.NoError(t, err)

	tests := []struct {
		name       string
		store      *fakePasswordAdminStore
		body       string
		wantStatus int
		wantCode   string
		wantClear  bool
		wantUpdate bool
	}{
		{
			name:       "admin row missing",
			store:      ptrext.Of(fakePasswordAdminStore{getErr: admin.ErrNotFound}),
			body:       changePasswordBody("current-secret-value", "new-secret-value"),
			wantStatus: http.StatusForbidden,
			wantCode:   "FORBIDDEN",
		},
		{
			name:       "lookup error",
			store:      ptrext.Of(fakePasswordAdminStore{getErr: errors.New("database unavailable")}),
			body:       changePasswordBody("current-secret-value", "new-secret-value"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL",
		},
		{
			name: "wrong current password",
			store: ptrext.Of(fakePasswordAdminStore{row: admin.Admin{
				ID:           "admin-1",
				PasswordHash: validHash,
			}}),
			body:       changePasswordBody("wrong-secret-value", "new-secret-value"),
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
		},
		{
			name: "admin deleted before update",
			store: ptrext.Of(fakePasswordAdminStore{
				row: admin.Admin{
					ID:           "admin-1",
					PasswordHash: validHash,
				},
				updateErr: admin.ErrNotFound,
			}),
			body:       changePasswordBody("current-secret-value", "new-secret-value"),
			wantStatus: http.StatusUnauthorized,
			wantCode:   "USER_GONE",
			wantClear:  true,
			wantUpdate: true,
		},
		{
			name: "update error",
			store: ptrext.Of(fakePasswordAdminStore{
				row: admin.Admin{
					ID:           "admin-1",
					PasswordHash: validHash,
				},
				updateErr: errors.New("write failed"),
			}),
			body:       changePasswordBody("current-secret-value", "new-secret-value"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL",
			wantUpdate: true,
		},
		{
			name: "success",
			store: ptrext.Of(fakePasswordAdminStore{row: admin.Admin{
				ID:           "admin-1",
				PasswordHash: validHash,
			}}),
			body:       changePasswordBody("current-secret-value", "new-secret-value"),
			wantStatus: http.StatusOK,
			wantUpdate: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveChangePasswordWithStore(t, tc.store, tc.body)

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			if tc.wantCode != "" {
				require.Equal(t, tc.wantCode, responseCode(t, rec.Body.Bytes()))
			}
			if tc.wantUpdate {
				require.Equal(t, "admin-1", tc.store.updateID)
				require.True(t, VerifyOrDummy(tc.store.updateHash, "new-secret-value"))
			} else {
				require.Empty(t, tc.store.updateHash)
			}
			require.Equal(t, tc.wantClear, hasClearedSessionCookie(rec.Result().Cookies()))
		})
	}
}

func serveChangePasswordWithStore(
	t *testing.T,
	store *fakePasswordAdminStore,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	signer, err := session.NewSigner(strings.Repeat("s", 32))
	require.NoError(t, err)
	h := ptrext.Of(ChangePasswordHandler{admins: store, signer: signer})
	rec := httptest.NewRecorder()
	handler := dispatcher.Bind(
		"test.ChangePasswordBranches",
		dispatcher.Combine(
			func() *attunev1.ChangePasswordRequest {
				return ptrext.Of(attunev1.ChangePasswordRequest{})
			},
			dispatcher.JSONBody[*attunev1.ChangePasswordRequest],
			h.ValidateRequest,
		),
		h.ChangePassword,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ChangePasswordRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/fb/v1/console/me/change-password",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	ctx := session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "admin-1"}))
	handler(rec, req.WithContext(ctx))
	return rec
}

func changePasswordBody(current, next string) string {
	return `{"currentPassword":"` + current + `","newPassword":"` + next + `"}`
}

func responseCode(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope), string(body))
	return envelope.Code
}

func hasClearedSessionCookie(cookies []*http.Cookie) bool {
	for _, cookie := range cookies {
		if cookie.Name == session.SessionCookieName && cookie.MaxAge < 0 {
			return true
		}
	}
	return false
}
