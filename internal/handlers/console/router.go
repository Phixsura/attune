package console

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Router wires every console endpoint into a single chi.Router that
// the attune main mounts under /fb/v1/console.
//
// Endpoint inventory (matches openapi.yaml):
//
//	public (no session required):
//	  GET    /install/start                 → OAuth.Start
//	  GET    /install/callback              → OAuth.Callback
//	  GET    /install/dev-login   (gated)   → DevLogin (HTTP test backdoor)
//
//	session-required (RequireSession middleware):
//	  GET    /me                            → Me.Me
//	  POST   /logout                        → Me.Logout
//	  GET    /api-keys                      → APIKeys.List
//	  POST   /api-keys                      → APIKeys.Create
//	  DELETE /api-keys/{id}                 → APIKeys.Revoke
//
// Wave 2.x next: feedback / notify-targets / usage handlers attach
// under RequireSession in this same Mount() body.
type Router struct {
	signer        *Signer
	oauth         *OAuthHandler
	me            *MeHandler
	apiKeys       *APIKeysHandler
	notifyTargets *NotifyTargetsHandler
	feedback      *FeedbackHandler
	usage         *UsageHandler
	devLogin      http.Handler // nil when ConsoleDevLogin is off
}

func NewRouter(
	signer *Signer,
	oauth *OAuthHandler,
	me *MeHandler,
	apiKeys *APIKeysHandler,
	notifyTargets *NotifyTargetsHandler,
	feedback *FeedbackHandler,
	usage *UsageHandler,
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
	})

	return mux
}
