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
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/handlers/console/apikey"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/handlers/console/me"
	"github.com/Phixsura/attune/internal/handlers/console/notifytarget"
	"github.com/Phixsura/attune/internal/handlers/console/oauth"
	"github.com/Phixsura/attune/internal/handlers/console/usage"
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
	NewSigner               = session.NewSigner
	NewOAuthHandler         = oauth.NewOAuthHandler
	NewDevLoginHandler      = oauth.NewDevLoginHandler
	NewMeHandler            = me.NewMeHandler
	NewAPIKeysHandler       = apikey.NewAPIKeysHandler
	NewNotifyTargetsHandler = notifytarget.NewNotifyTargetsHandler
	NewFeedbackHandler      = feedback.NewFeedbackHandler
	NewUsageHandler         = usage.NewUsageHandler
	NewEnrichConfigHandler  = enrichconfig.NewHandler
)

// Router wires every console endpoint into a single chi.Router.
//
// Endpoint inventory:
//
//	public (no session required):
//	 GET /install/start → oauth.Handler.Start
//	 GET /install/callback → oauth.Handler.Callback
//	 GET /install/dev-login → DevLogin (optional, gated)
//
//	session-required (RequireSession middleware):
//	 GET /me → me.Handler.Me
//	 POST /logout → me.Handler.Logout
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
type Router struct {
	signer        *session.Signer
	oauth         *oauth.OAuthHandler
	me            *me.MeHandler
	apiKeys       *apikey.APIKeysHandler
	notifyTargets *notifytarget.NotifyTargetsHandler
	feedback      *feedback.FeedbackHandler
	usage         *usage.UsageHandler
	enrichConfig  *enrichconfig.Handler
	devLogin      http.Handler // nil when ConsoleDevLogin is off
}

func NewRouter(
	signer *session.Signer,
	oauth *oauth.OAuthHandler,
	me *me.MeHandler,
	apiKeys *apikey.APIKeysHandler,
	notifyTargets *notifytarget.NotifyTargetsHandler,
	feedback *feedback.FeedbackHandler,
	usage *usage.UsageHandler,
	enrichConfig *enrichconfig.Handler,
	devLogin http.Handler,
) *Router {
	return &Router{
		signer:        signer,
		oauth:         oauth,
		me:            me,
		apiKeys:       apiKeys,
		notifyTargets: notifyTargets,
		feedback:      feedback,
		usage:         usage,
		enrichConfig:  enrichConfig,
		devLogin:      devLogin,
	}
}

func (r *Router) Mount() chi.Router {
	mux := chi.NewRouter()

	mux.Route("/install", func(m chi.Router) {
		m.Get("/start", r.oauth.Start)
		m.Get("/callback", r.oauth.Callback)
		if r.devLogin != nil {
			m.Method(http.MethodGet, "/dev-login", r.devLogin)
		}
	})

	mux.Group(func(m chi.Router) {
		m.Use(r.signer.RequireSession)
		m.Get("/me", r.me.Me)
		m.Post("/logout", r.me.Logout)

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
	})

	return mux
}
