package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/admin"
)

func TestRequireAdminRejectsNonAdminRole(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "user-1", Role: "viewer"}}})
	called := false
	req := httptest.NewRequest(http.MethodGet, "/llm/channels", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "user-1"})))
	w := httptest.NewRecorder()

	router.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdminAllowsAdminRole(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "user-1", Role: "admin"}}})
	called := false
	req := httptest.NewRequest(http.MethodGet, "/llm/channels", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "user-1"})))
	w := httptest.NewRecorder()

	router.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

type roleAdminReader struct {
	row admin.Admin
	err error
}

func (r roleAdminReader) GetByID(context.Context, string) (admin.Admin, error) {
	if r.err != nil {
		return admin.Admin{}, r.err
	}
	return r.row, nil
}
