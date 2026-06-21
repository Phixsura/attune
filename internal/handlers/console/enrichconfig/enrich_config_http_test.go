// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package enrichconfig

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/enrich"
)

type fakeConfigService struct {
	view enrich.View

	getErr      error // when set, Get fails with it
	updateErr   error // when set, Update fails with it
	activateErr error // when set, ActivatePromptVersion fails with it

	updateTenant string
	updateView   enrich.View

	activateTenant  string
	activateVersion string

	previewTenant string
	previewInput  enrich.PreviewInput
	previewOut    enrich.PreviewResult

	listTenant string
	listFilter enrich.PromptVersionListFilter
	listResult enrich.PromptVersionListResult
	listErr    error
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
	// Mirror the real ConfigService.Update, which runs domain validation before
	// persisting. Without this the fake would mask handler bugs that push
	// invalid taxonomy past the handler's own checks and rely on Update to
	// reject them (the promote endpoint's whitespace/dup/display-name edges).
	if err := v.Dimensions.Validate(); err != nil {
		return err
	}
	f.updateTenant = tenantID
	f.updateView = v
	f.view = v
	return nil
}

func (f *fakeConfigService) Preview(_ context.Context, tenantID string, in enrich.PreviewInput) (enrich.PreviewResult, error) {
	f.previewTenant = tenantID
	f.previewInput = in
	return f.previewOut, nil
}

func (f *fakeConfigService) ActivatePromptVersion(_ context.Context, tenantID string, versionID string) error {
	if f.activateErr != nil {
		return f.activateErr
	}
	f.activateTenant = tenantID
	f.activateVersion = versionID
	return nil
}

func (f *fakeConfigService) ListPromptVersions(_ context.Context, tenantID string, filter enrich.PromptVersionListFilter) (enrich.PromptVersionListResult, error) {
	if f.listErr != nil {
		return enrich.PromptVersionListResult{}, f.listErr
	}
	f.listTenant = tenantID
	f.listFilter = filter
	return f.listResult, nil
}

func TestHTTPDispatchSmoke(t *testing.T) {
	t.Parallel()

	t.Run("get", func(t *testing.T) {
		h := &Handler{svc: &fakeConfigService{
			view: enrich.View{
				PromptTemplate:        ptrext.Of("custom prompt"),
				DefaultPromptTemplate: "default prompt",
				Dimensions: domain.DimensionSet{{
					Name: "severity",
					Kind: domain.DimSingle,
				}},
				PromptPolicy: enrich.PromptPolicyMetadata{PolicyID: "enrich.default", PromptVersion: "enrich.default@1"},
				PromptVersions: []enrich.PromptVersionSummary{{
					ID:            "11111111-1111-1111-1111-111111111111",
					PromptVersion: "enrich.default@1",
					PolicyID:      "enrich.default",
					PolicyVersion: "1",
					Mode:          "default",
					PromptSource:  "built_in",
					IsActive:      true,
					DimensionsN:   1,
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
		require.Equal(t, "default prompt", config["defaultPromptTemplate"])
		require.Len(t, config["dimensions"].([]any), 1)
		policy := config["promptPolicy"].(map[string]any)
		require.Equal(t, "enrich.default", policy["policyId"])
		versions := config["promptVersions"].([]any)
		require.Len(t, versions, 1)
		require.Equal(t, "enrich.default@1", versions[0].(map[string]any)["promptVersion"])
	})

	t.Run("list versions", func(t *testing.T) {
		fake := &fakeConfigService{
			listResult: enrich.PromptVersionListResult{
				Items: []enrich.PromptVersionSummary{{
					ID:            "22222222-2222-2222-2222-222222222222",
					PromptVersion: "enrich.default@1",
					PolicyID:      "enrich.default",
					PolicyVersion: "1",
					Mode:          "default",
					PromptSource:  "built_in",
					IsActive:      true,
					DimensionsN:   1,
				}},
				NextCursor: "22222222-2222-2222-2222-222222222222",
			},
		}
		h := &Handler{svc: fake}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.ListPromptVersions",
			dispatcher.Query(
				func() *attunev1.ListEnrichPromptVersionsRequest {
					return &attunev1.ListEnrichPromptVersionsRequest{}
				},
				BindListPromptVersionsRequest,
			),
			h.ListPromptVersions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListEnrichPromptVersionsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/enrich-config/versions?limit=20&q=default", ""))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "tenant-1", fake.listTenant)
		require.Equal(t, 20, fake.listFilter.Limit)
		require.Equal(t, "default", fake.listFilter.Query)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "22222222-2222-2222-2222-222222222222", body["nextCursor"])
		require.Len(t, body["versions"], 1)
	})

	t.Run("list versions rejects invalid cursor", func(t *testing.T) {
		h := &Handler{svc: &fakeConfigService{}}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.ListPromptVersions",
			dispatcher.Query(
				func() *attunev1.ListEnrichPromptVersionsRequest {
					return &attunev1.ListEnrichPromptVersionsRequest{}
				},
				BindListPromptVersionsRequest,
			),
			h.ListPromptVersions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListEnrichPromptVersionsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/enrich-config/versions?cursor=not-a-version-id", ""))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("list versions rejects invalid limit", func(t *testing.T) {
		h := &Handler{svc: &fakeConfigService{}}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.ListPromptVersions",
			dispatcher.Query(
				func() *attunev1.ListEnrichPromptVersionsRequest {
					return &attunev1.ListEnrichPromptVersionsRequest{}
				},
				BindListPromptVersionsRequest,
			),
			h.ListPromptVersions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListEnrichPromptVersionsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/enrich-config/versions?limit=51", ""))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("list versions handles service error", func(t *testing.T) {
		h := &Handler{svc: &fakeConfigService{listErr: errors.New("boom")}}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.ListPromptVersions",
			dispatcher.Query(
				func() *attunev1.ListEnrichPromptVersionsRequest {
					return &attunev1.ListEnrichPromptVersionsRequest{}
				},
				BindListPromptVersionsRequest,
			),
			h.ListPromptVersions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListEnrichPromptVersionsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/enrich-config/versions", ""))

		require.Equal(t, http.StatusInternalServerError, w.Code)
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

	t.Run("update policy config preserves default display fields when omitted", func(t *testing.T) {
		svc := &fakeConfigService{}
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
			`{"dimensions":[],"policyConfig":{"tone":"neutral"}}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "neutral", svc.updateView.PolicyConfig.Tone)
		require.True(t, svc.updateView.PolicyConfig.DisplayFieldsRequired)
	})

	t.Run("update policy config accepts explicit disabled display fields", func(t *testing.T) {
		svc := &fakeConfigService{}
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
			`{"dimensions":[],"policyConfig":{"tone":"neutral","displayFieldsRequired":false}}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.False(t, svc.updateView.PolicyConfig.DisplayFieldsRequired)
	})

	t.Run("preview", func(t *testing.T) {
		svc := &fakeConfigService{previewOut: enrich.PreviewResult{
			RenderedPrompt: "rendered prompt",
			PromptPolicy:   enrich.PromptPolicyMetadata{PolicyID: "enrich.default", PromptVersion: "enrich.default@1"},
		}}
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
			`{"sampleContent":"  hello  ","promptTemplate":"draft {{content}}","useDraftConfig":true,"dimensions":[{"name":"severity","displayName":{"entries":{"default":"Severity"}},"kind":"single"}]}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, svc.previewTenant)
		require.Equal(t, "hello", svc.previewInput.SampleContent)
		require.True(t, svc.previewInput.HasDraftConfig)
		require.Equal(t, "draft {{content}}", *svc.previewInput.PromptTemplate)
		require.Len(t, svc.previewInput.Dimensions, 1)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "rendered prompt", body["renderedPrompt"])
		policy := body["promptPolicy"].(map[string]any)
		require.Equal(t, "enrich.default", policy["policyId"])
	})

	t.Run("activate version", func(t *testing.T) {
		svc := &fakeConfigService{view: enrich.View{
			PromptPolicy: enrich.PromptPolicyMetadata{PolicyID: "enrich.default", PromptVersion: "enrich.default@1"},
			PromptVersions: []enrich.PromptVersionSummary{{
				ID:            "11111111-1111-1111-1111-111111111111",
				PromptVersion: "enrich.default@1",
				PolicyID:      "enrich.default",
				PolicyVersion: "1",
				Mode:          "default",
				PromptSource:  "built_in",
				IsActive:      true,
			}},
		}}
		h := &Handler{svc: svc}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.ActivatePromptVersion",
			dispatcher.Custom(
				func() *attunev1.ActivateEnrichPromptVersionRequest {
					return &attunev1.ActivateEnrichPromptVersionRequest{}
				},
				func(r *http.Request, req *attunev1.ActivateEnrichPromptVersionRequest) error {
					req.VersionId = "11111111-1111-1111-1111-111111111111"
					return nil
				},
			),
			h.ActivatePromptVersion,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ActivateEnrichPromptVersionRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/enrich-config/versions/11111111-1111-1111-1111-111111111111:activate",
			`{}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, svc.activateTenant)
		require.Equal(t, "11111111-1111-1111-1111-111111111111", svc.activateVersion)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.NotNil(t, body["config"])
	})

	t.Run("activate version rejects empty id", func(t *testing.T) {
		h := &Handler{svc: &fakeConfigService{}}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.ActivatePromptVersion",
			dispatcher.Custom(
				func() *attunev1.ActivateEnrichPromptVersionRequest {
					return &attunev1.ActivateEnrichPromptVersionRequest{}
				},
				func(_ *http.Request, _ *attunev1.ActivateEnrichPromptVersionRequest) error {
					return nil
				},
			),
			h.ActivatePromptVersion,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ActivateEnrichPromptVersionRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/enrich-config/versions/:activate", `{}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("activate version maps not found", func(t *testing.T) {
		h := &Handler{svc: &fakeConfigService{
			view:        enrich.View{},
			activateErr: tenant.ErrTenantNotFound,
		}}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.ActivatePromptVersion",
			dispatcher.Custom(
				func() *attunev1.ActivateEnrichPromptVersionRequest {
					return &attunev1.ActivateEnrichPromptVersionRequest{}
				},
				func(_ *http.Request, req *attunev1.ActivateEnrichPromptVersionRequest) error {
					req.VersionId = "missing-version"
					return nil
				},
			),
			h.ActivatePromptVersion,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ActivateEnrichPromptVersionRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/enrich-config/versions/missing-version:activate", `{}`))

		require.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("activate version handles unknown service error", func(t *testing.T) {
		h := &Handler{svc: &fakeConfigService{
			view:        enrich.View{},
			activateErr: errors.New("activate failed"),
		}}
		handler := dispatcher.Bind(
			"console.EnrichConfigHandler.ActivatePromptVersion",
			dispatcher.Custom(
				func() *attunev1.ActivateEnrichPromptVersionRequest {
					return &attunev1.ActivateEnrichPromptVersionRequest{}
				},
				func(_ *http.Request, req *attunev1.ActivateEnrichPromptVersionRequest) error {
					req.VersionId = "broken-version"
					return nil
				},
			),
			h.ActivatePromptVersion,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ActivateEnrichPromptVersionRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/fb/v1/console/enrich-config/versions/broken-version:activate", `{}`))

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestPromptPolicyProtoConversions(t *testing.T) {
	t.Parallel()

	require.Nil(t, promptVariablesToProto(nil))
	require.Nil(t, promptOutputsToProto(nil))
	require.Nil(t, promptWarningsToProto(nil))
	require.Nil(t, promptVersionsToProto(nil))

	vars := promptVariablesToProto([]enrich.PromptVariable{{
		Name:     "content",
		Required: true,
		Meaning:  "Raw feedback.",
	}})
	require.Len(t, vars, 1)
	require.Equal(t, "content", vars[0].GetName())
	require.True(t, vars[0].GetRequired())

	outputs := promptOutputsToProto([]enrich.PromptOutput{{
		Name:     "display_title",
		Required: true,
		Language: "display",
	}})
	require.Len(t, outputs, 1)
	require.Equal(t, "display", outputs[0].GetLanguage())

	warnings := promptWarningsToProto([]enrich.PromptWarning{{
		Code:     "missing_dimensions",
		Severity: "warning",
		Message:  "Template omits dimensions.",
	}})
	require.Len(t, warnings, 1)
	require.Equal(t, "missing_dimensions", warnings[0].GetCode())

	created := time.Date(2026, 6, 21, 10, 11, 12, 13, time.UTC)
	template := "custom {{content}}"
	versions := promptVersionsToProto([]enrich.PromptVersionSummary{{
		ID:                "version-1",
		PromptVersion:     "enrich.custom@sha256:abc",
		PromptFingerprint: "sha256:prompt",
		SchemaFingerprint: "sha256:schema",
		PolicyID:          "enrich.custom",
		PolicyVersion:     "sha256:abc",
		Mode:              "legacy_custom_override",
		PromptSource:      "custom_template",
		CreatedAt:         created,
		IsActive:          true,
		HasTemplate:       true,
		PromptTemplate:    &template,
		Dimensions: domain.DimensionSet{{
			Name:        "severity",
			DisplayName: domain.I18nString{"default": "Severity"},
			Kind:        domain.DimSingle,
			Taxonomy: []domain.Taxonomy{{
				Value:       "critical",
				DisplayName: domain.I18nString{"default": "Critical"},
			}},
		}},
		PolicyConfig: domain.EnrichPromptPolicyConfig{
			OutputLanguagePolicy:  domain.PromptOutputDisplayOnly,
			TitleMaxChars:         40,
			RationaleMaxChars:     70,
			DisplayFieldsRequired: true,
			Tone:                  "concise",
		},
		DimensionsN: 1,
		Warnings:    []string{"missing_dimensions"},
	}})
	require.Len(t, versions, 1)
	require.Equal(t, "version-1", versions[0].GetId())
	require.Equal(t, "2026-06-21T10:11:12.000000013Z", versions[0].GetCreatedAt())
	require.Equal(t, template, versions[0].GetPromptTemplate())
	require.Len(t, versions[0].GetDimensions(), 1)
	require.Equal(t, "display_only", versions[0].GetPolicyConfig().GetOutputLanguagePolicy())
}
