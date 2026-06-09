// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package notifytarget

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

func TestHTTPDispatchSmoke(t *testing.T) {
	t.Parallel()

	t.Run("list", func(t *testing.T) {
		id := uuid.New()
		h := NewNotifyTargetsHandler(&fakeNotifyRepo{
			listRows: []notifytarget.NotifyTarget{{
				ID:              id,
				TenantID:        dispatchtest.TenantID,
				DestinationType: notifytarget.DestRawWebhook,
				Audience:        notifytarget.AudienceAll,
				URL:             "https://example.com/hook",
				TimeoutSeconds:  10,
				CreatedAt:       time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
			}},
		})
		handler := dispatcher.Bind(
			"console.NotifyTargetsHandler.List",
			dispatcher.Empty(func() *attunev1.ListNotifyTargetsRequest { return &attunev1.ListNotifyTargetsRequest{} }),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListNotifyTargetsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/notify-targets", ""))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		items := body["items"].([]any)
		require.Len(t, items, 1)
		require.Equal(t, id.String(), items[0].(map[string]any)["id"])
	})

	t.Run("create", func(t *testing.T) {
		id := uuid.New()
		createdAt := time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC)
		repo := &fakeNotifyRepo{insertID: id, insertTime: createdAt}
		h := NewNotifyTargetsHandler(repo)
		handler := dispatcher.Bind(
			"console.NotifyTargetsHandler.Create",
			dispatcher.JSON(func() *attunev1.CreateNotifyTargetRequest { return &attunev1.CreateNotifyTargetRequest{} }),
			h.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateNotifyTargetRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/notify-targets",
			`{"destinationType":"raw-webhook","audience":"all","url":"http://127.0.0.1/hook","timeoutSeconds":1}`,
		))

		require.Equal(t, http.StatusCreated, w.Code)
		require.NotNil(t, repo.insertTarget)
		require.Equal(t, dispatchtest.TenantID, repo.insertTarget.TenantID)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, id.String(), body["id"])
		require.Equal(t, "raw-webhook", body["destinationType"])
	})

	t.Run("patch", func(t *testing.T) {
		id := uuid.New()
		repo := &fakeNotifyRepo{
			getRow: &notifytarget.NotifyTarget{
				ID:              id,
				TenantID:        dispatchtest.TenantID,
				DestinationType: notifytarget.DestRawWebhook,
				Audience:        notifytarget.AudiencePool,
				URL:             "https://example.com/old",
				TimeoutSeconds:  10,
				CreatedAt:       time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
			},
		}
		h := NewNotifyTargetsHandler(repo)
		handler := dispatcher.Bind(
			"console.NotifyTargetsHandler.Patch",
			dispatcher.Combine(
				func() *attunev1.UpdateNotifyTargetRequest { return &attunev1.UpdateNotifyTargetRequest{} },
				dispatcher.JSONBody[*attunev1.UpdateNotifyTargetRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateNotifyTargetRequest, id string) { req.Id = id }),
			),
			h.Patch,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateNotifyTargetRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPatch,
			"/fb/v1/console/notify-targets/"+id.String(),
			`{"url":"https://example.com/new","disabled":true}`,
			dispatchtest.Param{Name: "id", Value: id.String()},
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, id, repo.updateID)
		require.True(t, repo.updateCalledWith.Disabled)
		require.Equal(t, "https://example.com/new", repo.updateCalledWith.URL)
	})

	t.Run("delete", func(t *testing.T) {
		id := uuid.New()
		repo := &fakeNotifyRepo{}
		h := NewNotifyTargetsHandler(repo)
		handler := dispatcher.Bind(
			"console.NotifyTargetsHandler.Delete",
			dispatcher.Path(
				func() *attunev1.DeleteNotifyTargetRequest { return &attunev1.DeleteNotifyTargetRequest{} },
				dispatcher.Param("id", func(req *attunev1.DeleteNotifyTargetRequest, id string) { req.Id = id }),
			),
			h.Delete,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteNotifyTargetRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodDelete,
			"/fb/v1/console/notify-targets/"+id.String(),
			"",
			dispatchtest.Param{Name: "id", Value: id.String()},
		))

		require.Equal(t, http.StatusNoContent, w.Code)
		require.Equal(t, dispatchtest.TenantID, repo.deleteTenant)
		require.Equal(t, id, repo.deleteID)
	})

	t.Run("test send", func(t *testing.T) {
		targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(targetServer.Close)

		id := uuid.New()
		repo := &fakeNotifyRepo{
			getRow: &notifytarget.NotifyTarget{
				ID:              id,
				TenantID:        dispatchtest.TenantID,
				DestinationType: notifytarget.DestRawWebhook,
				Audience:        notifytarget.AudienceAll,
				URL:             targetServer.URL,
				TimeoutSeconds:  1,
				CreatedAt:       time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
			},
		}
		h := NewNotifyTargetsHandler(repo)
		handler := dispatcher.Bind(
			"console.NotifyTargetsHandler.Test",
			dispatcher.Path(
				func() *attunev1.TestNotifyTargetRequest { return &attunev1.TestNotifyTargetRequest{} },
				dispatcher.Param("id", func(req *attunev1.TestNotifyTargetRequest, id string) { req.Id = id }),
			),
			h.Test,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TestNotifyTargetRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/notify-targets/"+id.String()+"/test",
			"",
			dispatchtest.Param{Name: "id", Value: id.String()},
		))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, true, body["ok"])
		require.Equal(t, float64(http.StatusNoContent), body["statusCode"])
	})
}
