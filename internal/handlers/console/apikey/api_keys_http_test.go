// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package apikey

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

func TestHTTPDispatchSmoke(t *testing.T) {
	t.Parallel()

	t.Run("list", func(t *testing.T) {
		id := uuid.New()
		h := &APIKeysHandler{svc: &fakeAPIKeysService{
			listRows: []apikeyrepo.APIKeyListRow{{
				ID:        id,
				KeyPrefix: "fbk_live_123",
				Label:     "Primary",
				IsActive:  true,
				CreatedAt: time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
			}},
		}}
		handler := dispatcher.Bind(
			"console.APIKeysHandler.List",
			dispatchtest.Auth,
			dispatcher.Empty(func() *attunev1.ListApiKeysRequest { return &attunev1.ListApiKeysRequest{} }),
			h.List,
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/api-keys", ""))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		items := body["items"].([]any)
		require.Len(t, items, 1)
		item := items[0].(map[string]any)
		require.Equal(t, id.String(), item["id"])
		require.Equal(t, "Primary", item["label"])
	})

	t.Run("create", func(t *testing.T) {
		id := uuid.New()
		h := &APIKeysHandler{svc: &fakeAPIKeysService{
			issueRaw: "fbk_live_secret",
			issueID:  id,
			listRows: []apikeyrepo.APIKeyListRow{{
				ID:        id,
				KeyPrefix: "fbk_live_sec",
				Label:     "Created",
				IsActive:  true,
				CreatedAt: time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
			}},
		}}
		handler := dispatcher.Bind(
			"console.APIKeysHandler.Create",
			dispatchtest.Auth,
			dispatcher.JSON(func() *attunev1.CreateApiKeyRequest { return &attunev1.CreateApiKeyRequest{} }),
			h.Create,
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/api-keys", `{"label":"Created"}`))

		require.Equal(t, http.StatusCreated, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "fbk_live_secret", body["secret"])
		key := body["key"].(map[string]any)
		require.Equal(t, id.String(), key["id"])
	})

	t.Run("revoke", func(t *testing.T) {
		id := uuid.New()
		svc := &fakeAPIKeysService{}
		h := &APIKeysHandler{svc: svc}
		handler := dispatcher.Bind(
			"console.APIKeysHandler.Revoke",
			dispatchtest.Auth,
			dispatcher.Path(
				func() *attunev1.DeleteApiKeyRequest { return &attunev1.DeleteApiKeyRequest{} },
				dispatcher.Param("id", func(req *attunev1.DeleteApiKeyRequest, id string) { req.Id = id }),
			),
			h.Revoke,
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodDelete,
			"/fb/v1/console/api-keys/"+id.String(),
			"",
			dispatchtest.Param{Name: "id", Value: id.String()},
		))

		require.Equal(t, http.StatusNoContent, w.Code)
		require.Equal(t, id, svc.revokeID)
		require.Equal(t, dispatchtest.TenantID, svc.revokeTenant)
	})
}
