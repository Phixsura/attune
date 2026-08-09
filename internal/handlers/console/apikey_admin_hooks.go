package console

// apikey_admin_hooks.go — /v1/hooks mounts for the automation webhook
// subscription surface (#234). Scope hooks:manage, RequireExplicitScope:
// legacy unscoped keys do NOT get this surface implicitly.

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consolewebhooksub "github.com/Phixsura/attune/internal/handlers/console/webhooksub"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func mountAPIKeyHooks(g chi.Router, hooks *consolewebhooksub.Handler, idem func(http.Handler) http.Handler) {
	g.Route("/hooks", func(hr chi.Router) {
		hr.With(apikey.RequireExplicitScope(domain.ScopeHooksManage), idem).Post("/", dispatcher.Bind(
			"apikey.WebhookSubHandler.Create",
			dispatcher.JSON(func() *attunev1.CreateWebhookSubscriptionRequest {
				return ptrext.Of(attunev1.CreateWebhookSubscriptionRequest{})
			}),
			hooks.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateWebhookSubscriptionRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		hr.With(apikey.RequireExplicitScope(domain.ScopeHooksManage)).Get("/", dispatcher.Bind(
			"apikey.WebhookSubHandler.List",
			dispatcher.Empty(func() *attunev1.ListWebhookSubscriptionsRequest {
				return ptrext.Of(attunev1.ListWebhookSubscriptionsRequest{})
			}),
			hooks.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListWebhookSubscriptionsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		hr.With(apikey.RequireExplicitScope(domain.ScopeHooksManage)).Delete("/{id}", dispatcher.Bind(
			"apikey.WebhookSubHandler.Delete",
			dispatcher.Path(
				func() *attunev1.DeleteWebhookSubscriptionRequest {
					return ptrext.Of(attunev1.DeleteWebhookSubscriptionRequest{})
				},
				dispatcher.Param("id", func(req *attunev1.DeleteWebhookSubscriptionRequest, id string) { req.Id = id }),
			),
			hooks.Delete,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteWebhookSubscriptionRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		hr.With(apikey.RequireExplicitScope(domain.ScopeHooksManage)).Get("/samples/{event_type}", dispatcher.Bind(
			"apikey.WebhookSubHandler.Samples",
			dispatcher.Path(
				func() *attunev1.ListWebhookSamplesRequest {
					return ptrext.Of(attunev1.ListWebhookSamplesRequest{})
				},
				dispatcher.Param("event_type", func(req *attunev1.ListWebhookSamplesRequest, v string) { req.EventType = v }),
			),
			hooks.Samples,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListWebhookSamplesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}
