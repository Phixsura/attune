// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package enrichconfig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/enrich"
)

type fakeConfigService struct {
	view enrich.View

	getErr    error // when set, Get fails with it
	updateErr error // when set, Update fails with it

	updateTenant string
	updateView   enrich.View

	previewTenant string
	previewSample string
	previewOut    string
}

func (f *fakeConfigService) Get(_ context.Context, _ string) (enrich.View, error) {
	if f.getErr != nil {
		return enrich.View{}, f.getErr
	}
	return f.view, nil
}

func (f *fakeConfigService) Update(_ context.Context, tenantID string, v enrich.View) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updateTenant = tenantID
	f.updateView = v
	f.view = v
	return nil
}

func (f *fakeConfigService) Preview(_ context.Context, tenantID, sampleContent string) (string, error) {
	f.previewTenant = tenantID
	f.previewSample = sampleContent
	return f.previewOut, nil
}

func TestHTTPDispatchSmoke(t *testing.T) {
	t.Parallel()

	t.Run("get", func(t *testing.T) {
		h := &Handler{svc: &fakeConfigService{
			view: enrich.View{
				PromptTemplate: ptrext.Of("custom prompt"),
				Dimensions: domain.DimensionSet{{
					Name: "severity",
					Kind: domain.DimSingle,
				}},
			},
		}}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.Get",
			dispatcher.Empty(func() *attunev1.GetEnrichConfigRequest { return &attunev1.GetEnrichConfigRequest{} }),
			h.Get,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetEnrichConfigRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/enrich-config", ""))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		config := body["config"].(map[string]any)
		require.Equal(t, "custom prompt", config["promptTemplate"])
		require.Len(t, config["dimensions"].([]any), 1)
	})

	t.Run("update", func(t *testing.T) {
		svc := &fakeConfigService{view: enrich.View{PromptTemplate: ptrext.Of("saved prompt")}}
		h := &Handler{svc: svc}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.Update",
			dispatcher.JSON(func() *attunev1.UpdateEnrichConfigRequest { return &attunev1.UpdateEnrichConfigRequest{} }),
			h.Update,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateEnrichConfigRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPut,
			"/fb/v1/console/enrich-config",
			`{"promptTemplate":"  saved prompt  ","dimensions":[{"name":"severity","displayName":{"entries":{"default":"Severity"}},"kind":"single","taxonomy":[{"value":"critical","displayName":{"entries":{"default":"Critical"}}}]}]}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, svc.updateTenant)
		require.Equal(t, "saved prompt", *svc.updateView.PromptTemplate)
		require.Len(t, svc.updateView.Dimensions, 1)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.NotNil(t, body["config"])
	})

	t.Run("preview", func(t *testing.T) {
		svc := &fakeConfigService{previewOut: "rendered prompt"}
		h := &Handler{svc: svc}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.Preview",
			dispatcher.JSON(func() *attunev1.PreviewEnrichPromptRequest { return &attunev1.PreviewEnrichPromptRequest{} }),
			h.Preview,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PreviewEnrichPromptRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/enrich-config/preview",
			`{"sampleContent":"  hello  "}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, svc.previewTenant)
		require.Equal(t, "hello", svc.previewSample)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "rendered prompt", body["renderedPrompt"])
	})
}
