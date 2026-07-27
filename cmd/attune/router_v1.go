// SPDX-License-Identifier: Apache-2.0

package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers"
	"github.com/Phixsura/attune/internal/handlers/cohortsyncwebhook"
	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/handlers/externalsyncwebhook"
	"github.com/Phixsura/attune/internal/handlers/portal"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	externalsyncrepo "github.com/Phixsura/attune/internal/repo/externalsync"
	apikeysvc "github.com/Phixsura/attune/internal/service/apikey"
	cohortsyncservice "github.com/Phixsura/attune/internal/service/cohortsync"
	externalsyncsvc "github.com/Phixsura/attune/internal/service/externalsync"
)

func mountV1Routes(
	r chi.Router,
	cfg *config.Config,
	pool *pgxpool.Pool,
	ingestHandler *handlers.IngestHandler,
	apiKeys *apikeysvc.APIKeys,
	inboundMux chi.Router,
	inboundSecrets *secretstore.TinkStore,
	cohortSyncSvc *cohortsyncservice.Service,
	versionMW func(http.Handler) http.Handler,
	rateLimiter *ratelimit.Limiter,
	perKeyRateLimiter *ratelimit.PerKeyLimiter,
	portalHandler *portal.Handler,
	portalLimiter middlewareProvider,
	portalWriteLimiter middlewareProvider,
	adminRepo *admin.Repo,
) {
	r.Route("/v1", func(r chi.Router) {
		mountV1AdapterRoutes(r, pool, inboundMux, inboundSecrets, cohortSyncSvc)
		mountV1PortalRoutes(r, portalHandler, versionMW, portalLimiter, portalWriteLimiter)
		mountV1ApiKeyRoutes(r, cfg, pool, ingestHandler, apiKeys, versionMW, rateLimiter, perKeyRateLimiter, adminRepo)
	})
}

func mountV1AdapterRoutes(r chi.Router, pool *pgxpool.Pool, inboundMux chi.Router, inboundSecrets *secretstore.TinkStore, cohortSyncSvc *cohortsyncservice.Service) {
	if inboundMux != nil {
		r.Mount("/inbound", inboundMux)
	}
	if inboundSecrets != nil {
		webhooks := externalsyncwebhook.NewHandler(externalsyncsvc.New(externalsyncrepo.New(pool), inboundSecrets))
		r.Mount("/external-sync/webhooks", webhooks.Routes())
	}
	if cohortSyncSvc != nil {
		cohortWebhooks := cohortsyncwebhook.NewHandler(cohortSyncSvc)
		r.Mount("/cohort-sync", cohortWebhooks.Routes())
	}
}

func mountV1PortalRoutes(
	r chi.Router,
	portalHandler *portal.Handler,
	versionMW func(http.Handler) http.Handler,
	portalLimiter middlewareProvider,
	portalWriteLimiter middlewareProvider,
) {
	r.Group(func(r chi.Router) {
		r.Use(versionMW)
		r.Use(portal.NoStore)
		r.Use(portalLimiter.Middleware)
		mountPortalReadRoutes(r, portalHandler)
	})
	r.Group(func(r chi.Router) {
		r.Use(versionMW)
		r.Use(portal.NoStore)
		r.Use(portalWriteLimiter.Middleware)
		mountPortalWriteRoutes(r, portalHandler)
	})
}

func mountPortalReadRoutes(r chi.Router, portalHandler *portal.Handler) {
	r.Get("/portal/{tenant_slug}/submission-config", dispatcher.Bind(
		"portal.Handler.GetPublicSubmissionConfig",
		dispatcher.Path(
			func() *attunev1.GetPublicSubmissionConfigRequest {
				return ptrext.Of(attunev1.GetPublicSubmissionConfigRequest{})
			},
			dispatcher.Param("tenant_slug", func(req *attunev1.GetPublicSubmissionConfigRequest, slug string) {
				req.TenantSlug = slug
			}),
		),
		portalHandler.GetPublicSubmissionConfig,
		dispatcher.WithAuth(okAuth[attunev1.GetPublicSubmissionConfigRequest]),
	))
	r.Get("/portal/{tenant_slug}/requests", dispatcher.Bind(
		"portal.Handler.ListPublicCustomerRequests",
		dispatcher.Query(
			func() *attunev1.ListPublicCustomerRequestsRequest {
				return ptrext.Of(attunev1.ListPublicCustomerRequestsRequest{})
			},
			dispatcher.Param("tenant_slug", func(req *attunev1.ListPublicCustomerRequestsRequest, slug string) {
				req.TenantSlug = slug
			}),
			portal.BindListCustomerRequests,
		),
		portalHandler.ListPublicCustomerRequests,
		dispatcher.WithAuth(okAuth[attunev1.ListPublicCustomerRequestsRequest]),
	))
	r.Get("/portal/{tenant_slug}/requests/{public_slug}", dispatcher.Bind(
		"portal.Handler.GetPublicCustomerRequest",
		dispatcher.Path(
			func() *attunev1.GetPublicCustomerRequestRequest {
				return ptrext.Of(attunev1.GetPublicCustomerRequestRequest{})
			},
			dispatcher.Param("tenant_slug", func(req *attunev1.GetPublicCustomerRequestRequest, slug string) {
				req.TenantSlug = slug
			}),
			dispatcher.Param("public_slug", func(req *attunev1.GetPublicCustomerRequestRequest, slug string) {
				req.PublicSlug = slug
			}),
		),
		portalHandler.GetPublicCustomerRequest,
		dispatcher.WithAuth(okAuth[attunev1.GetPublicCustomerRequestRequest]),
	))
	r.Get("/portal/{tenant_slug}/roadmap", dispatcher.Bind(
		"portal.Handler.ListPublicRoadmap",
		dispatcher.Query(
			func() *attunev1.ListPublicRoadmapRequest {
				return ptrext.Of(attunev1.ListPublicRoadmapRequest{})
			},
			dispatcher.Param("tenant_slug", func(req *attunev1.ListPublicRoadmapRequest, slug string) {
				req.TenantSlug = slug
			}),
			portal.BindListRoadmap,
		),
		portalHandler.ListPublicRoadmap,
		dispatcher.WithAuth(okAuth[attunev1.ListPublicRoadmapRequest]),
	))
}

func mountPortalWriteRoutes(
	r chi.Router,
	portalHandler *portal.Handler,
) {
	r.Post("/portal/{tenant_slug}/requests/{public_slug}/votes", dispatcher.Bind(
		"portal.Handler.VotePublicCustomerRequest",
		dispatcher.Custom(
			func() *attunev1.VotePublicCustomerRequest {
				return ptrext.Of(attunev1.VotePublicCustomerRequest{})
			},
			portal.BindVotePublicCustomerRequest,
		),
		portalHandler.VotePublicCustomerRequest,
		dispatcher.WithAuth(okAuth[attunev1.VotePublicCustomerRequest]),
	))
	r.Post("/portal/{tenant_slug}/requests/{public_slug}/subscribe", dispatcher.Bind(
		"portal.Handler.SubscribePublicCustomerRequest",
		dispatcher.Custom(
			func() *attunev1.SubscribePublicCustomerRequestRequest {
				return ptrext.Of(attunev1.SubscribePublicCustomerRequestRequest{})
			},
			portal.BindSubscribePublicCustomerRequest,
		),
		portalHandler.SubscribePublicCustomerRequest,
		dispatcher.WithAuth(okAuth[attunev1.SubscribePublicCustomerRequestRequest]),
	))
	r.Delete("/portal/{tenant_slug}/requests/{public_slug}/votes", dispatcher.Bind(
		"portal.Handler.UnvotePublicCustomerRequest",
		dispatcher.Path(
			func() *attunev1.UnvotePublicCustomerRequest {
				return ptrext.Of(attunev1.UnvotePublicCustomerRequest{})
			},
			dispatcher.Param("tenant_slug", func(req *attunev1.UnvotePublicCustomerRequest, slug string) {
				req.TenantSlug = slug
			}),
			dispatcher.Param("public_slug", func(req *attunev1.UnvotePublicCustomerRequest, slug string) {
				req.PublicSlug = slug
			}),
		),
		portalHandler.UnvotePublicCustomerRequest,
		dispatcher.WithAuth(okAuth[attunev1.UnvotePublicCustomerRequest]),
	))
	r.Post("/portal/{tenant_slug}/requests/{public_slug}/comments", dispatcher.Bind(
		"portal.Handler.CreatePublicCustomerComment",
		dispatcher.Path(
			func() *attunev1.CreatePublicCustomerCommentRequest {
				return ptrext.Of(attunev1.CreatePublicCustomerCommentRequest{})
			},
			dispatcher.JSONBody[*attunev1.CreatePublicCustomerCommentRequest],
			dispatcher.Param("tenant_slug", func(req *attunev1.CreatePublicCustomerCommentRequest, slug string) {
				req.TenantSlug = slug
			}),
			dispatcher.Param("public_slug", func(req *attunev1.CreatePublicCustomerCommentRequest, slug string) {
				req.PublicSlug = slug
			}),
		),
		portalHandler.CreatePublicCustomerComment,
		dispatcher.WithAuth(okAuth[attunev1.CreatePublicCustomerCommentRequest]),
	))
	r.Post("/portal/{tenant_slug}/submissions", dispatcher.Bind(
		"portal.Handler.CreatePublicSubmission",
		dispatcher.Custom(
			func() *attunev1.CreatePublicSubmissionRequest {
				return ptrext.Of(attunev1.CreatePublicSubmissionRequest{})
			},
			portal.BindCreatePublicSubmissionRequest,
		),
		portalHandler.CreatePublicSubmission,
		dispatcher.WithAuth(okAuth[attunev1.CreatePublicSubmissionRequest]),
	))
	r.Post("/portal/{tenant_slug}/unsubscribe", dispatcher.Bind(
		"portal.Handler.UnsubscribePublicCustomerRequest",
		dispatcher.Custom(
			func() *attunev1.UnsubscribePublicCustomerRequestRequest {
				return ptrext.Of(attunev1.UnsubscribePublicCustomerRequestRequest{})
			},
			portal.BindUnsubscribePublicCustomerRequest,
		),
		portalHandler.UnsubscribePublicCustomerRequest,
		dispatcher.WithAuth(okAuth[attunev1.UnsubscribePublicCustomerRequestRequest]),
	))
	r.Post("/portal/{tenant_slug}/notification-contact/confirm", dispatcher.Bind(
		"portal.Handler.ConfirmPublicNotificationContact",
		dispatcher.Custom(
			func() *attunev1.ConfirmPublicNotificationContactRequest {
				return ptrext.Of(attunev1.ConfirmPublicNotificationContactRequest{})
			},
			portal.BindConfirmPublicNotificationContact,
		),
		portalHandler.ConfirmPublicNotificationContact,
		dispatcher.WithAuth(okAuth[attunev1.ConfirmPublicNotificationContactRequest]),
	))
}

func mountV1ApiKeyRoutes(
	r chi.Router,
	cfg *config.Config,
	pool *pgxpool.Pool,
	ingestHandler *handlers.IngestHandler,
	apiKeys *apikeysvc.APIKeys,
	versionMW func(http.Handler) http.Handler,
	rateLimiter *ratelimit.Limiter,
	perKeyRateLimiter *ratelimit.PerKeyLimiter,
	adminRepo *admin.Repo,
) {
	r.Group(func(r chi.Router) {
		// Auth verify endpoint - requires valid API key but no specific scope.
		// Rate-limited to prevent brute-force attacks.
		r.Group(func(r chi.Router) {
			r.Use(versionMW)
			r.Use(apikey.MiddlewareWithProxies(apiKeys, cfg.Security.TrustedProxyHops))
			r.Use(rateLimiter.Middleware)
			authVerify := handlers.NewAuthVerifyHandler(apikeyrepo.NewAPIKey(pool))
			r.Get("/auth/verify", dispatcher.Bind(
				"handlers.AuthVerifyHandler.Verify",
				dispatcher.Custom(func() *attunev1.VerifyApiKeyRequest { return ptrext.Of(attunev1.VerifyApiKeyRequest{}) }, nil),
				authVerify.Verify,
				dispatcher.WithAuth(apiKeyAuth),
			))
		})

		r.Group(func(r chi.Router) {
			if mw := publicIngestCORS(cfg); mw != nil {
				r.Use(mw)
			}
			// Browser-safe ingest needs CORS headers even on version errors, so
			// its order is CORS -> version contract -> auth.
			r.Use(versionMW)
			r.Use(apikey.MiddlewareWithProxies(apiKeys, cfg.Security.TrustedProxyHops))
			r.Use(apikey.RequireScope(domain.ScopeIngestWrite))
			r.Use(rateLimiter.Middleware)       // per-tenant
			r.Use(perKeyRateLimiter.Middleware) // per-key (key's own rate_limit_rpm)
			r.Mount("/feedback", ingestHandler.Routes())
		})

		// Selected management routes over the API-key surface (scope-gated),
		// reusing the console handlers — lets the SDKs manage admin resources
		// without cloning business logic (#36, #168).
		r.Group(func(r chi.Router) {
			r.Use(versionMW)
			console.MountAPIKeyAdminRoutes(r, pool, apiKeys, cfg.Security.TrustedProxyHops, perKeyRateLimiter, console.APIKeyAdminRouteOptions{
				GDPRStepUpTTL:         cfg.GDPRStepUpTTL,
				GDPRExportTTL:         cfg.GDPRExportTTL,
				GDPRDeleteGraceWindow: cfg.GDPRDeleteGraceWindow,
				AuditRetentionDays:    cfg.Audit.RetentionDays,
				AuditPruneInterval:    cfg.AuditPruneInterval,
				MCPPublicBaseURL:      cfg.MCPPublicBaseURL,
				MCPOAuthIssuer:        cfg.MCP.OAuth.Issuer,
				GDPRAdmins:            adminRepo,
			})
		})
	})
}

func okAuth[Req any](_ *http.Request, _ *Req) (struct{}, error) {
	return struct{}{}, nil
}

func apiKeyAuth(r *http.Request, _ *attunev1.VerifyApiKeyRequest) (*apikey.AuthCtx, error) {
	return apikey.FromContext(r.Context()), nil
}
