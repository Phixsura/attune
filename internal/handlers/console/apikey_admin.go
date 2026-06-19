// API-key-authenticated admin routes. The tag and workflow config handlers were
// built for the console (session auth); this exposes them under the API-key
// surface so the published SDKs can manage tags/workflow, gated by the
// tags:* / workflow:* scopes. Same handlers, adapted auth source (#36).
package console

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consoletag "github.com/Phixsura/attune/internal/handlers/console/tag"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	feedbacktagrepo "github.com/Phixsura/attune/internal/repo/feedbacktag"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// apikeyToSession adapts the API-key auth context to the session.AuthCtx the
// console handlers consume, so the same handlers serve API-key callers. The
// actor is recorded as "apikey:<keyID>" for audit. apikey.RequireScope has
// already run by the time a handler is reached, so FromContext is non-nil.
func apikeyToSession(r *http.Request) (*session.AuthCtx, error) {
	ak := apikey.FromContext(r.Context())
	if ak == nil {
		return nil, dispatcher.NewError(http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "missing api key")
	}
	return ptrext.Of(session.AuthCtx{
		TenantID: ak.TenantID,
		UserID:   "apikey:" + ak.KeyID.String(),
		UserType: "admin",
	}), nil
}

// MountAPIKeyAdminRoutes mounts the tag/workflow config endpoints under the
// API-key surface (scope-gated), reusing the console handlers. It builds its own
// handler instances from the pool so it does not depend on the console router.
func MountAPIKeyAdminRoutes(r chi.Router, pool *pgxpool.Pool, apiKeys apikey.Verifier, trustedProxyHops int) {
	audit := auditlogsvc.New(auditlogrepo.New(pool))

	tags := consoletag.NewHandler(feedbacktagrepo.New(pool))
	tags.SetAuditLogger(audit)

	r.Group(func(g chi.Router) {
		g.Use(apikey.MiddlewareWithProxies(apiKeys, trustedProxyHops))
		mountAPIKeyTags(g, tags)
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
				return apikeyToSession(r)
			}),
		))
		t.With(apikey.RequireScope(domain.ScopeTagsWrite)).Post("/", dispatcher.Bind(
			"apikey.TagHandler.Create",
			dispatcher.JSON(func() *attunev1.CreateTagRequest { return ptrext.Of(attunev1.CreateTagRequest{}) }),
			tags.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateTagRequest) (*session.AuthCtx, error) {
				return apikeyToSession(r)
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
				return apikeyToSession(r)
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
				return apikeyToSession(r)
			}),
		))
	})
}
