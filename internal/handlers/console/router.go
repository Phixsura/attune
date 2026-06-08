// Package console is the root that wires every console handler
// subpackage (apikey, feedback, me, notifytarget, oauth, usage) into a
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
	"github.com/go-chi/chi/v5"

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
//	 POST /install/login → auth.Handler.Login (#66 Plan T11)
//	 POST /install/logout → auth.Handler.Logout
//
//	session-required (RequireSession middleware):
//	 GET /me → me.Handler.Me
//	 POST /logout → me.Handler.Logout
//	 POST /me/change-password → auth.ChangePasswordHandler.ChangePassword
//	 GET /api-keys → apikey.Handler.List
//	 POST /api-keys → apikey.Handler.Create
//	 DELETE /api-keys/{id} → apikey.Handler.Revoke
//	 GET /notify-targets → notifytarget.Handler.List
//	 POST /notify-targets → notifytarget.Handler.Create
//	 PATCH /notify-targets/{id} → notifytarget.Handler.Patch
//	 DELETE /notify-targets/{id} → notifytarget.Handler.Delete
//	 POST /notify-targets/{id}/test → notifytarget.Handler.Test
//	 GET /feedback → feedback.Handler.List
//	 GET /feedback/stats → feedback.Handler.Stats
//	 GET /feedback/{id} → feedback.Handler.Get
//	 GET /usage → usage.Handler.ServeHTTP
//	 GET /enrich-config → enrichconfig.Handler.Get
//	 PUT /enrich-config → enrichconfig.Handler.Update
//	 POST /enrich-config/preview → enrichconfig.Handler.Preview
//	 GET /inbound/sources → inbound.Handler.List
//	 POST /inbound/sources → inbound.Handler.Create
//	 GET /inbound/sources/{id} → inbound.Handler.Get
//	 POST /inbound/sources/{id}/rotate-secret → inbound.Handler.Rotate
//	 POST /inbound/sources/{id}/pause → inbound.Handler.Pause
//	 POST /inbound/sources/{id}/resume → inbound.Handler.Resume
//	 DELETE /inbound/sources/{id} → inbound.Handler.Delete
//	 POST /inbound/sources/test-connection → inbound.Handler.TestConnection
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

func (r *Router) Mount() chi.Router {
	mux := chi.NewRouter()

	mux.Route("/install", func(m chi.Router) {
		// Local-admin password login (#66 Plan T11) — replaces the
		// deleted external-OAuth flow (#66 Plan T17).
		m.Post("/login", r.auth.Login)
		m.Post("/logout", r.auth.Logout)
	})

	mux.Group(func(m chi.Router) {
		m.Use(r.signer.RequireSession)
		m.Get("/me", r.me.Me)
		m.Post("/logout", r.me.Logout)
		if r.changePassword != nil {
			m.Post("/me/change-password", r.changePassword.ChangePassword)
		}

		m.Route("/api-keys", func(k chi.Router) {
			k.Get("/", r.apiKeys.List)
			k.Post("/", r.apiKeys.Create)
			k.Delete("/{id}", r.apiKeys.Revoke)
		})

		m.Route("/notify-targets", func(n chi.Router) {
			n.Get("/", r.notifyTargets.List)
			n.Post("/", r.notifyTargets.Create)
			n.Patch("/{id}", r.notifyTargets.Patch)
			n.Delete("/{id}", r.notifyTargets.Delete)
			n.Post("/{id}/test", r.notifyTargets.Test)
		})

		m.Route("/feedback", func(f chi.Router) {
			f.Get("/", r.feedback.List)
			// /stats must come BEFORE /{id} — chi matches literally
			// first but order in source keeps the intent clear.
			f.Get("/stats", r.feedback.Stats)
			f.Get("/{id}", r.feedback.Get)
		})

		m.Get("/usage", r.usage.ServeHTTP)

		m.Route("/enrich-config", func(e chi.Router) {
			e.Get("/", r.enrichConfig.Get)
			e.Put("/", r.enrichConfig.Update)
			e.Post("/preview", r.enrichConfig.Preview)
		})

		if r.inbound != nil {
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
	})

	return mux
}
