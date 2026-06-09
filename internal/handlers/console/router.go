// Package console is the root that wires every console handler
// subpackage (auth, apikey, feedback, inbound, me, notifytarget, usage) into a
// single chi.Router mounted by attune under /fb/v1/console.
//
// Shared helpers live under handlers/console/internal/:
// - internal/respond — response/decode helpers (Proto, Error, Decode,
// ErrBodyTooLarge)
// - internal/session — Signer, cookies, RequireSession middleware,
// AuthCtx + FromContext
//
// Each handler subpackage imports respond + session. This package
// (`console`) imports the handler subpackages + session for the
// middleware. No cycles: subpackages do not import this root, and
// neither internal/respond nor internal/session import any handler.
package console

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/apikey"
	"github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	consoleinbound "github.com/Phixsura/attune/internal/handlers/console/inbound"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/handlers/console/me"
	"github.com/Phixsura/attune/internal/handlers/console/notifytarget"
	"github.com/Phixsura/attune/internal/handlers/console/usage"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Re-exports so cmd/attune can keep a single `console.X` surface even
// after the per-feature split. Lets the bootstrap (setup.go) stay close
// to the previous shape without learning every new package path.
type (
	Signer = session.Signer
)

// Constructor re-exports so cmd/attune/setup.go can keep building
// handlers via `console.NewXHandler(...)` after the split.
var (
	NewSigner                = session.NewSigner
	NewAuthHandler           = auth.NewHandler
	NewChangePasswordHandler = auth.NewChangePasswordHandler
	NewMeHandler             = me.NewMeHandler
	NewAPIKeysHandler        = apikey.NewAPIKeysHandler
	NewNotifyTargetsHandler  = notifytarget.NewNotifyTargetsHandler
	NewFeedbackHandler       = feedback.NewFeedbackHandler
	NewUsageHandler          = usage.NewUsageHandler
	NewEnrichConfigHandler   = enrichconfig.NewHandler
	NewInboundHandler        = consoleinbound.NewHandler
	BootstrapAdmin           = auth.BootstrapAdmin
)

// Router wires every console endpoint into a single chi.Router.
//
// Endpoint inventory:
//
//	public (no session required):
//	 POST /install/login -> auth.Handler.Login
//
//	session-required (RequireSession middleware):
//	 GET /me -> dispatcher.Bind(me.Handler.Me)
//	 POST /logout -> dispatcher.Bind(me.Handler.Logout)
//	 POST /me/change-password -> auth.ChangePasswordHandler.ChangePassword
//	 GET /api-keys -> dispatcher.Bind(apikey.Handler.List)
//	 POST /api-keys -> dispatcher.Bind(apikey.Handler.Create)
//	 DELETE /api-keys/{id} -> dispatcher.Bind(apikey.Handler.Revoke)
//	 GET /notify-targets -> dispatcher.Bind(notifytarget.Handler.List)
//	 POST /notify-targets -> dispatcher.Bind(notifytarget.Handler.Create)
//	 PATCH /notify-targets/{id} -> dispatcher.Bind(notifytarget.Handler.Patch)
//	 DELETE /notify-targets/{id} -> dispatcher.Bind(notifytarget.Handler.Delete)
//	 POST /notify-targets/{id}/test -> dispatcher.Bind(notifytarget.Handler.Test)
//	 GET /feedback -> dispatcher.Bind(feedback.Handler.List)
//	 GET /feedback/stats -> dispatcher.Bind(feedback.Handler.Stats)
//	 GET /feedback/{id} -> dispatcher.Bind(feedback.Handler.Get)
//	 GET /usage -> dispatcher.Bind(usage.Handler.Get)
//	 GET /enrich-config -> dispatcher.Bind(enrichconfig.Handler.Get)
//	 PUT /enrich-config -> dispatcher.Bind(enrichconfig.Handler.Update)
//	 POST /enrich-config/preview -> dispatcher.Bind(enrichconfig.Handler.Preview)
//	 GET /inbound/sources -> inbound.Handler.List
//	 POST /inbound/sources -> inbound.Handler.Create
//	 GET /inbound/sources/{id} -> inbound.Handler.Get
//	 POST /inbound/sources/{id}/rotate-secret -> inbound.Handler.Rotate
//	 POST /inbound/sources/{id}/pause -> inbound.Handler.Pause
//	 POST /inbound/sources/{id}/resume -> inbound.Handler.Resume
//	 DELETE /inbound/sources/{id} -> inbound.Handler.Delete
//	 POST /inbound/sources/test-connection -> inbound.Handler.TestConnection
type Router struct {
	signer         *session.Signer
	auth           *auth.Handler
	changePassword *auth.ChangePasswordHandler
	me             *me.MeHandler
	apiKeys        *apikey.APIKeysHandler
	notifyTargets  *notifytarget.NotifyTargetsHandler
	feedback       *feedback.FeedbackHandler
	usage          *usage.UsageHandler
	enrichConfig   *enrichconfig.Handler
	inbound        *consoleinbound.Handler
}

func NewRouter(
	signer *session.Signer,
	authH *auth.Handler,
	changePassword *auth.ChangePasswordHandler,
	me *me.MeHandler,
	apiKeys *apikey.APIKeysHandler,
	notifyTargets *notifytarget.NotifyTargetsHandler,
	feedback *feedback.FeedbackHandler,
	usage *usage.UsageHandler,
	enrichConfig *enrichconfig.Handler,
	inbound *consoleinbound.Handler,
) *Router {
	return ptrext.Of(Router{
		signer:         signer,
		auth:           authH,
		changePassword: changePassword,
		me:             me,
		apiKeys:        apiKeys,
		notifyTargets:  notifyTargets,
		feedback:       feedback,
		usage:          usage,
		enrichConfig:   enrichConfig,
		inbound:        inbound,
	})
}

func consoleSession(ctx context.Context) *session.AuthCtx {
	return session.FromContext(ctx)
}

func (r *Router) Mount() chi.Router {
	mux := chi.NewRouter()

	mux.Post("/install/login", r.auth.Login)

	mux.Group(func(m chi.Router) {
		m.Use(r.signer.RequireSession)
		r.mountSession(m)
	})

	return mux
}

func (r *Router) mountSession(m chi.Router) {
	m.Get("/me", dispatcher.Bind(
		"console.MeHandler.Me",
		consoleSession,
		dispatcher.Empty(func() *attunev1.GetMeRequest { return ptrext.Of(attunev1.GetMeRequest{}) }),
		r.me.Me,
	))
	m.Post("/logout", dispatcher.Bind(
		"console.MeHandler.Logout",
		consoleSession,
		dispatcher.Empty(func() *attunev1.LogoutRequest { return ptrext.Of(attunev1.LogoutRequest{}) }),
		r.me.Logout,
	))
	if r.changePassword != nil {
		m.Post("/me/change-password", r.changePassword.ChangePassword)
	}
	r.mountAPIKeys(m)
	r.mountNotifyTargets(m)
	r.mountFeedback(m)
	m.Get("/usage", dispatcher.Bind(
		"console.UsageHandler.Get",
		consoleSession,
		dispatcher.Empty(func() *attunev1.GetUsageRequest { return ptrext.Of(attunev1.GetUsageRequest{}) }),
		r.usage.Get,
	))
	r.mountEnrichConfig(m)
	r.mountInbound(m)
}

func (r *Router) mountAPIKeys(m chi.Router) {
	m.Route("/api-keys", func(k chi.Router) {
		k.Get("/", dispatcher.Bind(
			"console.APIKeysHandler.List",
			consoleSession,
			dispatcher.Empty(func() *attunev1.ListApiKeysRequest { return ptrext.Of(attunev1.ListApiKeysRequest{}) }),
			r.apiKeys.List,
		))
		k.Post("/", dispatcher.Bind(
			"console.APIKeysHandler.Create",
			consoleSession,
			dispatcher.JSON(func() *attunev1.CreateApiKeyRequest { return ptrext.Of(attunev1.CreateApiKeyRequest{}) }),
			r.apiKeys.Create,
		))
		k.Delete("/{id}", dispatcher.Bind(
			"console.APIKeysHandler.Revoke",
			consoleSession,
			dispatcher.Path(
				func() *attunev1.DeleteApiKeyRequest { return ptrext.Of(attunev1.DeleteApiKeyRequest{}) },
				dispatcher.Param("id", func(req *attunev1.DeleteApiKeyRequest, id string) {
					req.Id = id
				}),
			),
			r.apiKeys.Revoke,
		))
	})
}

func (r *Router) mountNotifyTargets(m chi.Router) {
	m.Route("/notify-targets", func(n chi.Router) {
		n.Get("/", dispatcher.Bind(
			"console.NotifyTargetsHandler.List",
			consoleSession,
			dispatcher.Empty(func() *attunev1.ListNotifyTargetsRequest { return ptrext.Of(attunev1.ListNotifyTargetsRequest{}) }),
			r.notifyTargets.List,
		))
		n.Post("/", dispatcher.Bind(
			"console.NotifyTargetsHandler.Create",
			consoleSession,
			dispatcher.JSON(func() *attunev1.CreateNotifyTargetRequest { return ptrext.Of(attunev1.CreateNotifyTargetRequest{}) }),
			r.notifyTargets.Create,
		))
		n.Patch("/{id}", dispatcher.Bind(
			"console.NotifyTargetsHandler.Patch",
			consoleSession,
			dispatcher.Combine(
				func() *attunev1.UpdateNotifyTargetRequest { return ptrext.Of(attunev1.UpdateNotifyTargetRequest{}) },
				dispatcher.JSONBody[*attunev1.UpdateNotifyTargetRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateNotifyTargetRequest, id string) {
					req.Id = id
				}),
			),
			r.notifyTargets.Patch,
		))
		n.Delete("/{id}", dispatcher.Bind(
			"console.NotifyTargetsHandler.Delete",
			consoleSession,
			dispatcher.Path(
				func() *attunev1.DeleteNotifyTargetRequest { return ptrext.Of(attunev1.DeleteNotifyTargetRequest{}) },
				dispatcher.Param("id", func(req *attunev1.DeleteNotifyTargetRequest, id string) {
					req.Id = id
				}),
			),
			r.notifyTargets.Delete,
		))
		n.Post("/{id}/test", dispatcher.Bind(
			"console.NotifyTargetsHandler.Test",
			consoleSession,
			dispatcher.Path(
				func() *attunev1.TestNotifyTargetRequest { return ptrext.Of(attunev1.TestNotifyTargetRequest{}) },
				dispatcher.Param("id", func(req *attunev1.TestNotifyTargetRequest, id string) {
					req.Id = id
				}),
			),
			r.notifyTargets.Test,
		))
	})
}

func (r *Router) mountFeedback(m chi.Router) {
	m.Route("/feedback", func(f chi.Router) {
		f.Get("/", dispatcher.Bind(
			"console.FeedbackHandler.List",
			consoleSession,
			dispatcher.Query(func() *attunev1.ListFeedbackRequest { return ptrext.Of(attunev1.ListFeedbackRequest{}) }, feedback.BindListRequest),
			r.feedback.List,
		))
		// /stats must come BEFORE /{id}; source order keeps the intent clear.
		f.Get("/stats", dispatcher.Bind(
			"console.FeedbackHandler.Stats",
			consoleSession,
			dispatcher.Empty(func() *attunev1.GetFeedbackStatsRequest { return ptrext.Of(attunev1.GetFeedbackStatsRequest{}) }),
			r.feedback.Stats,
		))
		f.Get("/{id}", dispatcher.Bind(
			"console.FeedbackHandler.Get",
			consoleSession,
			dispatcher.Path(
				func() *attunev1.GetFeedbackRequest { return ptrext.Of(attunev1.GetFeedbackRequest{}) },
				dispatcher.ParamInt64("id", func(req *attunev1.GetFeedbackRequest, id int64) {
					req.Id = id
				}, "id must be an integer"),
			),
			r.feedback.Get,
		))
	})
}

func (r *Router) mountEnrichConfig(m chi.Router) {
	m.Route("/enrich-config", func(e chi.Router) {
		e.Get("/", dispatcher.Bind(
			"console.EnrichConfigHandler.Get",
			consoleSession,
			dispatcher.Empty(func() *attunev1.GetEnrichConfigRequest { return ptrext.Of(attunev1.GetEnrichConfigRequest{}) }),
			r.enrichConfig.Get,
		))
		e.Put("/", dispatcher.Bind(
			"console.EnrichConfigHandler.Update",
			consoleSession,
			dispatcher.JSON(func() *attunev1.UpdateEnrichConfigRequest { return ptrext.Of(attunev1.UpdateEnrichConfigRequest{}) }),
			r.enrichConfig.Update,
		))
		e.Post("/preview", dispatcher.Bind(
			"console.EnrichConfigHandler.Preview",
			consoleSession,
			dispatcher.JSON(func() *attunev1.PreviewEnrichPromptRequest { return ptrext.Of(attunev1.PreviewEnrichPromptRequest{}) }),
			r.enrichConfig.Preview,
		))
	})
}

func (r *Router) mountInbound(m chi.Router) {
	if r.inbound == nil {
		return
	}
	m.Route("/inbound/sources", func(s chi.Router) {
		s.Get("/", r.inbound.List)
		s.Post("/", r.inbound.Create)
		s.Post("/test-connection", r.inbound.TestConnection)
		s.Get("/{id}", r.inbound.Get)
		s.Delete("/{id}", r.inbound.Delete)
		s.Post("/{id}/rotate-secret", r.inbound.Rotate)
		s.Post("/{id}/pause", r.inbound.Pause)
		s.Post("/{id}/resume", r.inbound.Resume)
	})
}
