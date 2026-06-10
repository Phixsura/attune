// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package guardpolicy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	serviceguard "github.com/Phixsura/attune/internal/service/guardpolicy"
)

func TestHTTPDispatchSmoke(t *testing.T) {
	t.Parallel()

	t.Run("list", func(t *testing.T) {
		svc := &fakeService{list: []llmguard.Policy{{
			ID: "sys-1", Name: "System privacy", Kind: llmguard.KindDefault, Enabled: true,
			Rules: []llmguard.Rule{{
				Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"email"}, Action: llmguard.ActionRedact,
			}},
		}}}
		h := &Handler{svc: svc}
		handler := dispatcher.Bind(
			"console.GuardPolicyHandler.List",
			dispatcher.Empty(func() *attunev1.ListGuardPoliciesRequest { return &attunev1.ListGuardPoliciesRequest{} }),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListGuardPoliciesRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/guard-policies", ""))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, svc.listTenant)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		items := body["items"].([]any)
		first := items[0].(map[string]any)
		require.Equal(t, "system", first["scope"])
	})

	t.Run("update", func(t *testing.T) {
		svc := &fakeService{}
		h := &Handler{svc: svc}
		handler := dispatcher.Bind(
			"console.GuardPolicyHandler.Update",
			dispatcher.JSON(func() *attunev1.UpdateGuardPoliciesRequest { return &attunev1.UpdateGuardPoliciesRequest{} }),
			h.Update,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateGuardPoliciesRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPut,
			"/fb/v1/console/guard-policies",
			`{"policies":[{"name":"Source override","kind":"override","enabled":true,"target":{"channels":["email"]},"rules":[{"guard":"pii","stage":"llm_input","entities":["email"],"action":"off"}]}]}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, svc.replaceTenant)
		require.Equal(t, dispatchtest.UserID, svc.actor)
		require.Equal(t, "email", svc.replaced[0].Target.Channels[0])
	})

	t.Run("create", func(t *testing.T) {
		svc := &fakeService{}
		h := &Handler{svc: svc}
		handler := dispatcher.Bind(
			"console.GuardPolicyHandler.Create",
			dispatcher.JSON(func() *attunev1.CreateGuardPolicyRequest { return &attunev1.CreateGuardPolicyRequest{} }),
			h.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateGuardPolicyRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/guard-policies",
			`{"policy":{"name":"API redaction","kind":"default","enabled":true,"target":{"channels":["api"]},"rules":[{"guard":"pii","stage":"llm_input","entities":["email"],"action":"redact"}]}}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, svc.createTenant)
		require.Equal(t, dispatchtest.UserID, svc.actor)
		require.Equal(t, "api", svc.created.Target.Channels[0])
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "tenant", body["scope"])
	})

	t.Run("patch", func(t *testing.T) {
		const id = "7b3685c9-0798-4f68-901b-46ea8cc15da2"
		svc := &fakeService{}
		h := &Handler{svc: svc}
		handler := dispatcher.Bind(
			"console.GuardPolicyHandler.Patch",
			dispatcher.Combine(
				func() *attunev1.PatchGuardPolicyRequest { return &attunev1.PatchGuardPolicyRequest{} },
				dispatcher.JSONBody[*attunev1.PatchGuardPolicyRequest],
				dispatcher.Param("id", func(req *attunev1.PatchGuardPolicyRequest, id string) {
					req.Id = id
				}),
			),
			h.Patch,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PatchGuardPolicyRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPatch,
			"/fb/v1/console/guard-policies/"+id,
			`{"policy":{"name":"Email off","kind":"override","enabled":true,"target":{"channels":["email"]},"rules":[{"guard":"pii","stage":"llm_input","entities":["email"],"action":"off"}]}}`,
			dispatchtest.Param{Name: "id", Value: id},
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, id, svc.updateID)
		require.Equal(t, "Email off", svc.updated.Name)
	})

	t.Run("delete", func(t *testing.T) {
		const id = "7b3685c9-0798-4f68-901b-46ea8cc15da2"
		svc := &fakeService{}
		h := &Handler{svc: svc}
		handler := dispatcher.Bind(
			"console.GuardPolicyHandler.Delete",
			dispatcher.Path(
				func() *attunev1.DeleteGuardPolicyRequest { return &attunev1.DeleteGuardPolicyRequest{} },
				dispatcher.Param("id", func(req *attunev1.DeleteGuardPolicyRequest, id string) {
					req.Id = id
				}),
			),
			h.Delete,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteGuardPolicyRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodDelete,
			"/fb/v1/console/guard-policies/"+id,
			"",
			dispatchtest.Param{Name: "id", Value: id},
		))

		require.Equal(t, http.StatusNoContent, w.Code)
		require.Equal(t, id, svc.deleteID)
		require.Equal(t, dispatchtest.TenantID, svc.deleteTenant)
	})

	t.Run("effective", func(t *testing.T) {
		svc := &fakeService{plan: llmguard.Plan{Rules: []llmguard.Rule{{
			Guard: "pii", Stage: llmguard.StageLLMInput, Entities: []string{"credit_card"}, Action: llmguard.ActionBlock,
		}}}}
		h := &Handler{svc: svc}
		handler := dispatcher.Bind(
			"console.GuardPolicyHandler.Resolve",
			dispatcher.JSON(func() *attunev1.ResolveGuardPolicyRequest { return &attunev1.ResolveGuardPolicyRequest{} }),
			h.Resolve,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ResolveGuardPolicyRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/guard-policies/effective",
			`{"channel":"email","sourceTags":["regulated"],"purpose":"enrich"}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "email", svc.meta.Channel)
		require.Equal(t, "regulated", svc.meta.SourceTags[0])
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		rules := body["rules"].([]any)
		rule := rules[0].(map[string]any)
		require.Equal(t, "block", rule["action"])
	})
}

func TestUpdateMapsValidationErrors(t *testing.T) {
	t.Parallel()
	h := &Handler{svc: &fakeService{replaceErr: serviceguard.ErrActionInvalid}}
	handler := dispatcher.Bind(
		"console.GuardPolicyHandler.Update",
		dispatcher.JSON(func() *attunev1.UpdateGuardPoliciesRequest { return &attunev1.UpdateGuardPoliciesRequest{} }),
		h.Update,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateGuardPoliciesRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(
		http.MethodPut,
		"/fb/v1/console/guard-policies",
		`{"policies":[{"name":"bad","kind":"default","enabled":true}]}`,
	))

	require.Equal(t, http.StatusBadRequest, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "VALIDATION", body["code"])
}

func TestListMapsStoreErrors(t *testing.T) {
	t.Parallel()
	h := &Handler{svc: &fakeService{listErr: errors.New("db down")}}
	handler := dispatcher.Bind(
		"console.GuardPolicyHandler.List",
		dispatcher.Empty(func() *attunev1.ListGuardPoliciesRequest { return &attunev1.ListGuardPoliciesRequest{} }),
		h.List,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListGuardPoliciesRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)

	w := httptest.NewRecorder()
	handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/guard-policies", ""))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "INTERNAL", body["code"])
}

func TestListRejectsMissingTenant(t *testing.T) {
	t.Parallel()
	h := &Handler{svc: &fakeService{}}
	_, err := h.List(
		&dispatcher.RequestContext[*session.AuthCtx]{
			Context: context.Background(),
			Auth:    &session.AuthCtx{UserID: "admin-1"},
		},
		&attunev1.ListGuardPoliciesRequest{},
	)
	var derr *dispatcher.Error
	if !errors.As(err, &derr) {
		t.Fatalf("err = %v, want dispatcher error", err)
	}
	require.Equal(t, http.StatusBadRequest, derr.Status)
	require.Equal(t, attunev1.ErrorCode_MISSING_TENANT, derr.Code)
}

type fakeService struct {
	list          []llmguard.Policy
	plan          llmguard.Plan
	listErr       error
	replaceErr    error
	createErr     error
	updateErr     error
	deleteErr     error
	resolveErr    error
	listTenant    string
	replaceTenant string
	createTenant  string
	updateTenant  string
	deleteTenant  string
	updateID      string
	deleteID      string
	actor         string
	replaced      []llmguard.Policy
	created       llmguard.Policy
	updated       llmguard.Policy
	meta          llmclient.GuardMetadata
}

func (f *fakeService) List(_ context.Context, tenantID string) ([]llmguard.Policy, error) {
	f.listTenant = tenantID
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeService) ReplaceTenantPolicies(
	_ context.Context, tenantID, actor string, policies []llmguard.Policy,
) ([]llmguard.Policy, error) {
	f.replaceTenant = tenantID
	f.actor = actor
	f.replaced = policies
	if f.replaceErr != nil {
		return nil, f.replaceErr
	}
	return policies, nil
}

func (f *fakeService) CreatePolicy(
	_ context.Context, tenantID, actor string, policy llmguard.Policy,
) (llmguard.Policy, error) {
	f.createTenant = tenantID
	f.actor = actor
	f.created = policy
	if f.createErr != nil {
		return llmguard.Policy{}, f.createErr
	}
	policy.ID = "7b3685c9-0798-4f68-901b-46ea8cc15da2"
	policy.TenantID = tenantID
	return policy, nil
}

func (f *fakeService) UpdatePolicy(
	_ context.Context, tenantID, actor, id string, policy llmguard.Policy,
) (llmguard.Policy, error) {
	f.updateTenant = tenantID
	f.actor = actor
	f.updateID = id
	f.updated = policy
	if f.updateErr != nil {
		return llmguard.Policy{}, f.updateErr
	}
	policy.ID = id
	policy.TenantID = tenantID
	return policy, nil
}

func (f *fakeService) DeletePolicy(_ context.Context, tenantID, id string) error {
	f.deleteTenant = tenantID
	f.deleteID = id
	return f.deleteErr
}

func (f *fakeService) Resolve(_ context.Context, meta llmclient.GuardMetadata) (llmguard.Plan, error) {
	f.meta = meta
	if f.resolveErr != nil {
		return llmguard.Plan{}, f.resolveErr
	}
	return f.plan, nil
}
