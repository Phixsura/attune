// API-key-authenticated admin routes. The tag and workflow config handlers were
// built for the console (session auth); this exposes them under the API-key
// surface so the published SDKs can manage tags/workflow, gated by the
// tags:* / workflow:* scopes. Same handlers, adapted auth source (#36).
package console

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	consoleauditlog "github.com/Phixsura/attune/internal/handlers/console/auditlog"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consolemcpclient "github.com/Phixsura/attune/internal/handlers/console/mcpclient"
	consoleoutbox "github.com/Phixsura/attune/internal/handlers/console/outbox"
	consoletag "github.com/Phixsura/attune/internal/handlers/console/tag"
	consoleworkflow "github.com/Phixsura/attune/internal/handlers/console/workflow"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	feedbackauditrepo "github.com/Phixsura/attune/internal/repo/feedbackaudit"
	feedbacktagrepo "github.com/Phixsura/attune/internal/repo/feedbacktag"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	workflowstaterepo "github.com/Phixsura/attune/internal/repo/workflowstate"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	gdprsvc "github.com/Phixsura/attune/internal/service/gdpr"
	workflowsvc "github.com/Phixsura/attune/internal/service/workflow"
)

type gdprAdminReader interface {
	GetByID(ctx context.Context, id string) (admin.Admin, error)
}

// APIKeyAdminRouteOptions carries the extra dependencies needed to expose the
// public API-key management surface beyond tags/workflow.
type APIKeyAdminRouteOptions struct {
	GDPRStepUpTTL         time.Duration
	GDPRExportTTL         time.Duration
	GDPRDeleteGraceWindow time.Duration
	AuditRetentionDays    int
	AuditPruneInterval    time.Duration
	MCPPublicBaseURL      string
	MCPOAuthIssuer        string
	GDPRAdmins            gdprAdminReader
}

// apikeyToSession adapts the API-key auth context to the session.AuthCtx the
// console handlers consume, so the same handlers serve API-key callers. The
// actor is recorded as "apikey:<keyID>" for audit. GDPR machine calls are also
// treated as already step-upped because possession of a valid server-side key is
// the machine-auth equivalent of the console's recent-password check.
//
// apikey.RequireScope has
// already run by the time a handler is reached, so the auth context is present;
// FromContextSafe is used (rather than the panicking FromContext) so a handler
// ever reached off the middleware fails closed with 401 instead of panicking.
func apikeyToSession(r *http.Request) (*session.AuthCtx, error) {
	ak, ok := apikey.FromContextSafe(r.Context())
	if !ok {
		return nil, dispatcher.NewError(http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "missing api key")
	}
	now := time.Now().UTC()
	return ptrext.Of(session.AuthCtx{
		TenantID: ak.TenantID,
		UserID:   "apikey:" + ak.KeyID.String(),
		UserType: "api_key",
		IssuedAt: now,
		StepUpAt: ptrext.Of(now),
	}), nil
}

func withAPIKeySession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, err := apikeyToSession(r)
		if err != nil {
			var derr *dispatcher.Error
			if errors.As(err, &derr) {
				dispatcher.Reject(r.Context(), w, derr.Status, derr.Code, derr.Message)
				return
			}
			dispatcher.Reject(r.Context(), w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to adapt api key session")
			return
		}
		next.ServeHTTP(w, r.WithContext(session.WithAuthCtx(r.Context(), auth)))
	})
}

// MountAPIKeyAdminRoutes mounts the tag/workflow config endpoints under the
// API-key surface (scope-gated), reusing the console handlers. It builds its own
// handler instances from the pool so it does not depend on the console router.
//
// perKeyLimiter applies the key's own rate_limit_rpm here too: that cap is the
// key's overall request budget, not an ingest-only limit, so these admin writes
// must count against it (else a capped key could mutate tags/workflow without
// bound). Mounted after the api-key middleware so AuthCtx is on the context.
//
// The per-tenant ingest Limiter is deliberately NOT mounted here: it is an
// ingest-sized bucket "guarding the ingest API", and sharing it would couple two
// unrelated budgets (admin writes eating the ingest rate and vice-versa). A key
// with no rate_limit_rpm is therefore unthrottled on this low-volume admin
// surface — the same posture as a console-session admin on the identical
// handlers. Operators cap machine keys by setting rate_limit_rpm.
func MountAPIKeyAdminRoutes(r chi.Router, pool *pgxpool.Pool, apiKeys apikey.Verifier, trustedProxyHops int, perKeyLimiter *ratelimit.PerKeyLimiter, opts APIKeyAdminRouteOptions) {
	audit := auditlogsvc.New(auditlogrepo.New(pool))

	tags := consoletag.NewHandler(feedbacktagrepo.New(pool))
	tags.SetAuditLogger(audit)

	wfStateRepo := workflowstaterepo.New(pool)
	wf := consoleworkflow.NewHandler(wfStateRepo, workflowsvc.NewService(wfStateRepo, feedbackauditrepo.New(pool), pool))
	wf.SetAuditLogger(audit)

	gdpr := consolegdpr.NewHandler(
		gdprsvc.New(
			gdprrepo.New(pool),
			audit,
			gdprsvc.WithAuditRetention(opts.AuditRetentionDays, opts.AuditPruneInterval),
			gdprsvc.WithExportTTL(opts.GDPRExportTTL),
			gdprsvc.WithStepUpTTL(opts.GDPRStepUpTTL),
			gdprsvc.WithDeleteGraceWindow(opts.GDPRDeleteGraceWindow),
		),
		nil,
		opts.GDPRAdmins,
		opts.GDPRStepUpTTL,
	)

	outbox := consoleoutbox.NewHandler(outboxrepo.NewOutbox(pool))
	outbox.SetAuditLogger(audit)

	mcpClients := consolemcpclient.NewHandler(
		mcprepo.NewClients(pool),
		mcprepo.NewTokens(pool),
		mcprepo.NewSessions(pool),
		mcprepo.NewToolPolicies(pool),
	)
	mcpClients.SetAuditLogger(audit)
	mcpClients.SetConnectionProfile(opts.MCPPublicBaseURL, opts.MCPOAuthIssuer)

	r.Group(func(g chi.Router) {
		g.Use(apikey.MiddlewareWithProxies(apiKeys, trustedProxyHops))
		g.Use(perKeyLimiter.Middleware)
		g.Use(withAPIKeySession)
		mountAPIKeyTags(g, tags)
		mountAPIKeyWorkflow(g, wf)
		mountAPIKeyAuditLog(g, consoleauditlog.NewHandler(audit))
		mountAPIKeyGDPR(g, gdpr)
		mountAPIKeyOutbox(g, outbox)
		mountAPIKeyMCPClients(g, mcpClients)
	})
}

func mountAPIKeyTags(g chi.Router, tags *consoletag.Handler) {
	g.Route("/tags", func(t chi.Router) {
		t.With(apikey.RequireScope(domain.ScopeTagsRead)).Get("/", dispatcher.Bind(
			"apikey.TagHandler.List",
			dispatcher.Query(
				func() *attunev1.ListTagsRequest { return ptrext.Of(attunev1.ListTagsRequest{}) },
				func(r *http.Request, req *attunev1.ListTagsRequest) error {
					if v := r.URL.Query().Get("include_archived"); v == "true" || v == "1" {
						req.IncludeArchived = true
					}
					return nil
				},
			),
			tags.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListTagsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		t.With(apikey.RequireScope(domain.ScopeTagsWrite)).Post("/", dispatcher.Bind(
			"apikey.TagHandler.Create",
			dispatcher.JSON(func() *attunev1.CreateTagRequest { return ptrext.Of(attunev1.CreateTagRequest{}) }),
			tags.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		t.With(apikey.RequireScope(domain.ScopeTagsWrite)).Patch("/{id}", dispatcher.Bind(
			"apikey.TagHandler.Update",
			dispatcher.Combine(
				func() *attunev1.UpdateTagRequest { return ptrext.Of(attunev1.UpdateTagRequest{}) },
				dispatcher.JSONBody[*attunev1.UpdateTagRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateTagRequest, id string) { req.Id = id }),
			),
			tags.Update,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		t.With(apikey.RequireScope(domain.ScopeTagsWrite)).Delete("/{id}", dispatcher.Bind(
			"apikey.TagHandler.Archive",
			dispatcher.Path(
				func() *attunev1.ArchiveTagRequest { return ptrext.Of(attunev1.ArchiveTagRequest{}) },
				dispatcher.Param("id", func(req *attunev1.ArchiveTagRequest, id string) { req.Id = id }),
			),
			tags.Archive,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ArchiveTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func mountAPIKeyWorkflow(g chi.Router, wf *consoleworkflow.Handler) {
	g.Route("/workflow", func(w chi.Router) {
		w.With(apikey.RequireScope(domain.ScopeWorkflowRead)).Get("/states", dispatcher.Bind(
			"apikey.WorkflowHandler.ListStates",
			dispatcher.Query(
				func() *attunev1.ListStatesRequest { return ptrext.Of(attunev1.ListStatesRequest{}) },
				func(r *http.Request, req *attunev1.ListStatesRequest) error {
					if v := r.URL.Query().Get("include_archived"); v == "true" || v == "1" {
						req.IncludeArchived = true
					}
					return nil
				},
			),
			wf.ListStates,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListStatesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.With(apikey.RequireScope(domain.ScopeWorkflowWrite)).Post("/states", dispatcher.Bind(
			"apikey.WorkflowHandler.CreateState",
			dispatcher.JSON(func() *attunev1.CreateStateRequest { return ptrext.Of(attunev1.CreateStateRequest{}) }),
			wf.CreateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateStateRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.With(apikey.RequireScope(domain.ScopeWorkflowWrite)).Patch("/states/{id}", dispatcher.Bind(
			"apikey.WorkflowHandler.UpdateState",
			dispatcher.Combine(
				func() *attunev1.UpdateStateRequest { return ptrext.Of(attunev1.UpdateStateRequest{}) },
				dispatcher.JSONBody[*attunev1.UpdateStateRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateStateRequest, id string) { req.Id = id }),
			),
			wf.UpdateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateStateRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.With(apikey.RequireScope(domain.ScopeWorkflowWrite)).Delete("/states/{id}", dispatcher.Bind(
			"apikey.WorkflowHandler.ArchiveState",
			dispatcher.Path(
				func() *attunev1.ArchiveStateRequest { return ptrext.Of(attunev1.ArchiveStateRequest{}) },
				dispatcher.Param("id", func(req *attunev1.ArchiveStateRequest, id string) { req.Id = id }),
			),
			wf.ArchiveState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ArchiveStateRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.With(apikey.RequireScope(domain.ScopeWorkflowRead)).Get("/transitions", dispatcher.Bind(
			"apikey.WorkflowHandler.ListTransitions",
			dispatcher.Empty(func() *attunev1.ListTransitionsRequest { return ptrext.Of(attunev1.ListTransitionsRequest{}) }),
			wf.ListTransitions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListTransitionsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.With(apikey.RequireScope(domain.ScopeWorkflowWrite)).Put("/transitions", dispatcher.Bind(
			"apikey.WorkflowHandler.ReplaceTransitions",
			dispatcher.JSON(func() *attunev1.ReplaceTransitionsRequest { return ptrext.Of(attunev1.ReplaceTransitionsRequest{}) }),
			wf.ReplaceTransitions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ReplaceTransitionsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.With(apikey.RequireScope(domain.ScopeWorkflowWrite)).Post("/seed", dispatcher.Bind(
			"apikey.WorkflowHandler.SeedDefaults",
			dispatcher.Empty(func() *attunev1.SeedDefaultsRequest { return ptrext.Of(attunev1.SeedDefaultsRequest{}) }),
			wf.SeedDefaults,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SeedDefaultsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}
