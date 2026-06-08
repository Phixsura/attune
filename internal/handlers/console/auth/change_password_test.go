// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// fakeChangePasswordRepo isn't viable: the real Repo wraps *pgxpool.Pool
// directly. The change-password handler tests skip the persistence
// branch via dependency narrowing — the route file boundary is exercised
// in handler_test.go style here, with the http surface only. The
// stored-row + db-row update behaviour is covered by the existing
// admins_test.go integration tests against pgx.
//
// What we *do* test at unit level:
//   - request validation (missing fields, weak password, same as current)
//   - tenant-session rejection (403)
//   - unauthenticated body shapes do not panic
//
// The "happy path" (verify + bcrypt + UpdatePasswordHash) requires a
// pgxpool fixture and lives in the integration suite.

func authedRequest(t *testing.T, tenantID, userID string, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost,
		"/fb/v1/console/me/change-password",
		strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	ctx := session.WithAuthCtx(r.Context(), ptrext.Of(session.AuthCtx{
		TenantID: tenantID,
		UserID:   userID,
	}))
	return r.WithContext(ctx)
}

func decodeEnvelope(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode body: %v; raw=%q", err, body)
	}
	return m
}

// Signer is constructed with a 32-byte dev key so the
// ClearSessionCookie branch in error paths is exercised end-to-end
// without hitting any nil pointers. We never read the cookie back; the
// test only cares about the response status + envelope shape.
func newTestHandler(t *testing.T) *auth.ChangePasswordHandler {
	t.Helper()
	signer, err := session.NewSigner(strings.Repeat("a", 32))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	// admins=nil: every test path we exercise rejects the request
	// before touching the repo. The persistence branch is integration-
	// covered.
	return auth.NewChangePasswordHandler(nil, signer)
}

func TestChangePassword_RejectsTenantSession(t *testing.T) {
	h := newTestHandler(t)
	r := authedRequest(t, "tenant-1", "user-1", `{"currentPassword":"old","newPassword":"newpass123456"}`)
	w := httptest.NewRecorder()
	h.ChangePassword(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d want 403; body=%s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	if env["code"] != "forbidden" {
		t.Fatalf("code: got %v want forbidden", env["code"])
	}
}

func TestChangePassword_RejectsMissingBody(t *testing.T) {
	h := newTestHandler(t)
	r := authedRequest(t, "", "admin-1", `not-json`)
	w := httptest.NewRecorder()
	h.ChangePassword(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestChangePassword_RejectsMissingFields(t *testing.T) {
	h := newTestHandler(t)
	cases := []struct {
		name, body string
	}{
		{"no current", `{"newPassword":"newpass123456"}`},
		{"no new", `{"currentPassword":"old"}`},
		{"both empty", `{"currentPassword":"","newPassword":""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := authedRequest(t, "", "admin-1", tc.body)
			w := httptest.NewRecorder()
			h.ChangePassword(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestChangePassword_RejectsShortNewPassword(t *testing.T) {
	h := newTestHandler(t)
	r := authedRequest(t, "", "admin-1", `{"currentPassword":"old","newPassword":"short"}`)
	w := httptest.NewRecorder()
	h.ChangePassword(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400; body=%s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	if env["code"] != "weak_password" {
		t.Fatalf("code: got %v want weak_password", env["code"])
	}
}

func TestChangePassword_RejectsSamePassword(t *testing.T) {
	h := newTestHandler(t)
	r := authedRequest(t, "", "admin-1",
		`{"currentPassword":"sameSecretValue!","newPassword":"sameSecretValue!"}`)
	w := httptest.NewRecorder()
	h.ChangePassword(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400; body=%s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	if env["code"] != "same_password" {
		t.Fatalf("code: got %v want same_password", env["code"])
	}
}

// Sanity check: decoding context-derived AuthCtx doesn't panic on a
// non-empty TenantID branch (covered above) and an empty TenantID
// branch with valid body lands on the repo-call branch — which we
// don't have a fake for at unit level. Reaching that branch with a
// nil repo would panic; the validation rejects first.
func TestChangePassword_ValidationOrder(t *testing.T) {
	h := newTestHandler(t)
	// New password is OK length, but admin lookup would happen — we
	// shortcut by sending a body that fails at the field-required gate.
	r := authedRequest(t, "", "admin-1", `{}`)
	w := httptest.NewRecorder()
	h.ChangePassword(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400; body=%s", w.Code, w.Body.String())
	}
}
