// router.go builds the HTTP router for `attune server`: the OTel root span +
// X-Trace-Id middleware, the standard chi middleware chain, health/metrics, the
// /v1 API surface, and the optional Console mount. Split out of
// server.go to keep each file under the 300-line cap (CLAUDE.md §1).
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riandyrn/otelchi"
	"go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers"
	"github.com/Phixsura/attune/internal/handlers/apiversion"
	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/handlers/externalsyncwebhook"
	"github.com/Phixsura/attune/internal/handlers/mcp"
	"github.com/Phixsura/attune/internal/handlers/portal"
	"github.com/Phixsura/attune/internal/handlers/security"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	mcpoauth "github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/policy"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	externalsyncrepo "github.com/Phixsura/attune/internal/repo/externalsync"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbackaudit"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	inboundsourcerepo "github.com/Phixsura/attune/internal/repo/inboundsource"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	publicvisibilityrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	apikeysvc "github.com/Phixsura/attune/internal/service/apikey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	enrichruntimesvc "github.com/Phixsura/attune/internal/service/enrichruntime"
	externalsyncsvc "github.com/Phixsura/attune/internal/service/externalsync"
	"github.com/Phixsura/attune/internal/service/ingest"
	publicvisibilitysvc "github.com/Phixsura/attune/internal/service/publicvisibility"
	workflowsvc "github.com/Phixsura/attune/internal/service/workflow"
)

// buildRouter wires the chi router: OTel root span + X-Trace-Id, the standard
// middleware chain, /healthz, /readyz, /metrics, the /v1 API (api-key /
// rate-limited feedback ingest + inbound adapter mux), and — when
// CONSOLE_SESSION_KEY is set — the Console under /fb/v1/console.
func buildRouter(
	ctx context.Context,
	cfg *config.Config,
	ingestHandler *handlers.IngestHandler,
	apiKeys *apikeysvc.APIKeys,
	pool *pgxpool.Pool,
	ready readinessChecker,
	llm llmclient.LLMClient,
	inboundMux chi.Router,
	inboundSecrets *secretstore.TinkStore,
	inboundSources *inboundsourcerepo.Repo,
	adminRepo *admin.Repo,
	enrichRuntime *enrichruntimesvc.Service,
	ingestor *ingest.Ingestor,
	sources domain.SourceSet,
) (chi.Router, error) {
	const where = "main.buildRouter"
	r := chi.NewRouter()
	// otelchi opens the root span — continued from a client-supplied
	// traceparent when present, generated from our readable trace_id
	// format otherwise. It must run first. The /health prefix filter
	// covers /healthz plus any leftover /health probes (now 404) so
	// liveness checks don't flood the trace backend.
	r.Use(otelchi.Middleware("attune", otelchi.WithFilter(func(r *http.Request) bool {
		return !strings.HasPrefix(r.URL.Path, "/health") && !strings.HasPrefix(r.URL.Path, "/metrics")
	})))
	// X-Trace-Id response header — optional debug aid for clients; the
	// API contract does not require it.
	r.Use(traceIDResponseHeader)
	r.Use(middleware.RequestID) // chi's own X-Request-ID — kept alongside X-Trace-Id for back-compat
	// NOTE: chi's middleware.RealIP is intentionally NOT used. It unconditionally
	// rewrites r.RemoteAddr from X-Forwarded-For / X-Real-IP, which a client on a
	// direct connection can forge — defeating the API-key IP allowlist. Client-IP
	// resolution is done by nethardening.ClientIP honoring security.trusted_proxy_hops
	// (see apikey.MiddlewareWithProxies), so RemoteAddr must stay the true peer.
	r.Use(middleware.Recoverer)
	r.Use(security.Headers)       // X-Frame-Options, X-Content-Type-Options, etc.
	r.Use(middleware.Compress(5)) // gzip responses > 500 bytes
	r.Use(middleware.Timeout(305 * time.Second))
	mountHealth(r, ready)
	// Prometheus scrape endpoint. Restrict to internal CIDR via nginx
	// in production — no auth at the Go level.
	r.Handle("/metrics", metrics.Handler())
	rateLimiter := buildRateLimiter(cfg)
	perKeyRateLimiter := buildPerKeyRateLimiter(cfg)
	versionMW := apiversion.Middleware(apiversion.DefaultConfig())
	portalLimiter := newPortalAnonymousLimiter(cfg.RateLimitPerMinute, cfg.RateLimitBurst, cfg.RateLimitDisabled, cfg.Security.TrustedProxyHops)
	portalHandler := portal.NewHandler(publicvisibilitysvc.New(publicvisibilityrepo.New(pool), nil))

	r.Route("/v1", func(r chi.Router) {
		// Inbound adapter mux. Adapters have already registered their
		// routes onto inboundMux during Manager.StartAll(ctx). Mounting
		// it here exposes them under /v1/inbound/<channel>/...
		if inboundMux != nil {
			r.Mount("/inbound", inboundMux)
		}
		if inboundSecrets != nil {
			webhooks := externalsyncwebhook.NewHandler(externalsyncsvc.New(externalsyncrepo.New(pool), inboundSecrets))
			r.Mount("/external-sync/webhooks", webhooks.Routes())
		}
		r.Group(func(r chi.Router) {
			r.Use(versionMW)
			r.Use(portal.NoStore)
			r.Use(portalLimiter.Middleware)
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
				dispatcher.WithAuth(func(_ *http.Request, _ *attunev1.ListPublicCustomerRequestsRequest) (struct{}, error) {
					return struct{}{}, nil
				}),
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
				dispatcher.WithAuth(func(_ *http.Request, _ *attunev1.GetPublicCustomerRequestRequest) (struct{}, error) {
					return struct{}{}, nil
				}),
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
				dispatcher.WithAuth(func(_ *http.Request, _ *attunev1.ListPublicRoadmapRequest) (struct{}, error) {
					return struct{}{}, nil
				}),
			))
		})

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
					dispatcher.WithAuth(func(r *http.Request, _ *attunev1.VerifyApiKeyRequest) (*apikey.AuthCtx, error) {
						return apikey.FromContext(r.Context()), nil
					}),
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
	})

	// Console UI. Mounted under /fb/v1/console; the reverse
	// proxy forwards external traffic here. Disabled gracefully
	// when ConsoleSessionKey is empty (single-process dev defaults).
	if cfg.ConsoleSessionKey != "" {
		consoleRouter, err := buildConsoleRouter(cfg, pool, inboundSecrets, inboundSources, adminRepo, llm, enrichRuntime, sources)
		if err != nil {
			return nil, fmt.Errorf("build console: %w", err)
		}
		r.Route("/fb/v1/console", func(r chi.Router) {
			r.Mount("/", consoleRouter)
		})
		mountConsoleStatic(ctx, r)
		logext.Infof(ctx, "[%s] console enabled,base_url:%s", where, cfg.ConsoleBaseURL)
	} else {
		logext.Infof(ctx, "[%s] console disabled (no CONSOLE_SESSION_KEY)", where)
	}

	// MCP Server. Mounted under /mcp when enabled. Provides OAuth 2.1
	// Authorization Server and JSON-RPC 2.0 tool surface for AI agents (#93).
	if cfg.MCPEnabled {
		mcpHandler, err := buildMCPHandler(ctx, cfg, pool, ingestor)
		if err != nil {
			return nil, fmt.Errorf("build mcp: %w", err)
		}
		if err := mcpHandler.MountWellKnownRoutes(r); err != nil {
			return nil, fmt.Errorf("mount mcp discovery: %w", err)
		}
		r.Mount("/mcp", mcpHandler.Routes())
		logext.Infof(ctx, "[%s] mcp server enabled", where)
	} else {
		logext.Infof(ctx, "[%s] mcp server disabled", where)
	}

	return r, nil
}

func buildMCPHandler(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, ingestor *ingest.Ingestor) (*mcp.Handler, error) {
	const where = "main.buildMCPHandler"

	jwtSecret := strings.TrimSpace(cfg.MCP.OAuth.JWTSecret)
	if len(jwtSecret) < 32 {
		return nil, fmt.Errorf("mcp.oauth.jwt_secret must be at least 32 bytes")
	}

	publicBaseURL := cfg.MCPPublicBaseURL
	issuer := cfg.MCP.OAuth.Issuer
	if issuer == "" {
		issuer = publicBaseURL + "/mcp/oauth"
	}

	mcpCfg := mcp.Config{
		PublicBaseURL:      publicBaseURL,
		JWTSecret:          []byte(jwtSecret),
		JWTIssuer:          issuer,
		RateLimitPerMinute: cfg.MCPRateLimitPerMinute,
		RateLimitBurst:     cfg.MCPRateLimitBurst,
		AccessTokenTTL:     cfg.MCPAccessTokenTTL,
		RefreshTokenTTL:    cfg.MCPRefreshTokenTTL,
	}

	clientsRepo := mcprepo.NewClients(pool)
	codesRepo := mcprepo.NewCodes(pool)
	tokensRepo := mcprepo.NewTokens(pool)
	sessionsRepo := mcprepo.NewSessions(pool)
	toolPoliciesRepo := mcprepo.NewToolPolicies(pool)

	stores := mcp.Stores{
		Clients:          newMCPClientStore(clientsRepo),
		Codes:            newMCPCodeStore(codesRepo),
		Tokens:           newMCPTokenStore(tokensRepo),
		Sessions:         newMCPSessionStore(sessionsRepo),
		ToolPolicies:     newMCPToolPolicyStore(toolPoliciesRepo),
		ClientValidator:  clientsRepo,
		SessionValidator: sessionsRepo,
		SessionActivity:  sessionsRepo,
	}

	feedbackRepo := feedback.NewFeedback(pool)
	workflowRepo := workflowstate.New(pool)
	tagRepo := feedbacktag.New(pool)
	tagAssignRepo := feedbacktagassignment.New(pool)
	auditRepo := feedbackaudit.New(pool)
	workflowService := workflowsvc.NewService(workflowRepo, auditRepo, pool)
	auditLogSvc := auditlogsvc.New(auditlogrepo.New(pool))

	deps := ptrext.Of(tools.Deps{
		Feedback:        feedbackRepo,
		FeedbackWriter:  feedbackRepo,
		WorkflowState:   workflowRepo,
		WorkflowTransit: newMCPWorkflowAdapter(workflowService),
		Tag:             tagRepo,
		TagAssign:       tagAssignRepo,
		Ingestor:        newMCPIngestorAdapter(ingestor),
		Audit:           newMCPAuditAdapter(auditLogSvc),
	})

	logext.Infof(ctx, "[%s] built,issuer:%s", where, issuer)
	return mcp.NewHandler(mcpCfg, stores, deps), nil
}

type mcpClientStoreAdapter struct {
	repo *mcprepo.ClientsRepo
}

func newMCPClientStore(repo *mcprepo.ClientsRepo) *mcpClientStoreAdapter {
	return ptrext.Of(mcpClientStoreAdapter{repo: repo})
}

func (a *mcpClientStoreAdapter) GetByID(ctx context.Context, id uuid.UUID) (*mcpoauth.Client, error) {
	c, err := a.repo.GetActiveByID(ctx, id)
	if err != nil {
		return nil, mcpoauth.ErrInvalidClient
	}
	return ptrext.Of(mcpoauth.Client{
		ID:             c.ID,
		TenantID:       c.TenantID,
		Name:           c.Name,
		RedirectURIs:   c.RedirectURIs,
		Scopes:         c.Scopes,
		ToolPolicyMode: c.ToolPolicyMode,
		RateLimitRPM:   c.RateLimitRPM,
		RateLimitBurst: c.RateLimitBurst,
		CreatedAt:      c.CreatedAt,
	}), nil
}

func (a *mcpClientStoreAdapter) ValidateRedirectURI(ctx context.Context, clientID uuid.UUID, uri string) (bool, error) {
	c, err := a.repo.GetActiveByID(ctx, clientID)
	if err != nil {
		return false, nil
	}
	for _, allowed := range c.RedirectURIs {
		if allowed == uri {
			return true, nil
		}
	}
	return false, nil
}

type mcpCodeStoreAdapter struct {
	repo *mcprepo.CodesRepo
}

func newMCPCodeStore(repo *mcprepo.CodesRepo) *mcpCodeStoreAdapter {
	return ptrext.Of(mcpCodeStoreAdapter{repo: repo})
}

func (a *mcpCodeStoreAdapter) Create(ctx context.Context, code *mcpoauth.AuthCode) error {
	_, err := a.repo.Create(ctx, mcprepo.CreateCodeParams{
		Code:          code.Code,
		ClientID:      code.ClientID,
		RedirectURI:   code.RedirectURI,
		Scopes:        code.Scopes,
		CodeChallenge: code.CodeChallenge,
		UserID:        code.TenantID,
		ExpiresAt:     code.ExpiresAt,
	})
	return err
}

func (a *mcpCodeStoreAdapter) Consume(ctx context.Context, code string) (*mcpoauth.AuthCode, error) {
	c, err := a.repo.Consume(ctx, code)
	if err != nil {
		return nil, mcpoauth.ErrInvalidCode
	}
	return ptrext.Of(mcpoauth.AuthCode{
		Code:                c.Code,
		ClientID:            c.ClientID,
		TenantID:            c.UserID,
		RedirectURI:         c.RedirectURI,
		Scopes:              c.Scopes,
		CodeChallenge:       c.CodeChallenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           c.ExpiresAt,
		CreatedAt:           c.CreatedAt,
	}), nil
}

type mcpTokenStoreAdapter struct {
	repo *mcprepo.TokensRepo
}

func newMCPTokenStore(repo *mcprepo.TokensRepo) *mcpTokenStoreAdapter {
	return ptrext.Of(mcpTokenStoreAdapter{repo: repo})
}

func (a *mcpTokenStoreAdapter) Create(ctx context.Context, token *mcpoauth.RefreshToken) error {
	_, err := a.repo.CreateWithHash(ctx, mcprepo.CreateWithHashParams{
		TokenHash: token.TokenHash,
		ClientID:  token.ClientID,
		SessionID: token.SessionID,
		Scopes:    token.Scopes,
		UserID:    token.TenantID,
		ExpiresAt: token.ExpiresAt,
	})
	return err
}

func (a *mcpTokenStoreAdapter) GetByHash(ctx context.Context, hash string) (*mcpoauth.RefreshToken, error) {
	t, err := a.repo.GetByHash(ctx, hash)
	if err != nil {
		return nil, mcpoauth.ErrInvalidRefreshToken
	}
	return ptrext.Of(mcpoauth.RefreshToken{
		ID:        t.ID,
		TokenHash: t.TokenHash,
		ClientID:  t.ClientID,
		TenantID:  t.UserID,
		SessionID: t.SessionID,
		Scopes:    t.Scopes,
		ExpiresAt: t.ExpiresAt,
		CreatedAt: t.CreatedAt,
	}), nil
}

func (a *mcpTokenStoreAdapter) Revoke(ctx context.Context, id uuid.UUID) error {
	return a.repo.Revoke(ctx, id)
}

func (a *mcpTokenStoreAdapter) Consume(ctx context.Context, hash string) (*mcpoauth.RefreshToken, error) {
	t, err := a.repo.Consume(ctx, hash)
	if err != nil {
		return nil, mcpoauth.ErrInvalidRefreshToken
	}
	return ptrext.Of(mcpoauth.RefreshToken{
		ID:        t.ID,
		TokenHash: t.TokenHash,
		ClientID:  t.ClientID,
		TenantID:  t.UserID,
		SessionID: t.SessionID,
		Scopes:    t.Scopes,
		ExpiresAt: t.ExpiresAt,
		CreatedAt: t.CreatedAt,
	}), nil
}

func (a *mcpTokenStoreAdapter) RotateToken(ctx context.Context, oldHash, newHash string, newExpiresAt time.Time) (*mcpoauth.RefreshToken, *mcpoauth.RefreshToken, error) {
	old, newToken, err := a.repo.RotateToken(ctx, mcprepo.RotateTokenParams{
		OldTokenHash: oldHash,
		NewTokenHash: newHash,
		NewExpiresAt: newExpiresAt,
	})
	if err != nil {
		return nil, nil, mcpoauth.ErrInvalidRefreshToken
	}
	return ptrext.Of(mcpoauth.RefreshToken{
			ID:        old.ID,
			TokenHash: old.TokenHash,
			ClientID:  old.ClientID,
			TenantID:  old.UserID,
			SessionID: old.SessionID,
			Scopes:    old.Scopes,
			ExpiresAt: old.ExpiresAt,
			CreatedAt: old.CreatedAt,
		}), ptrext.Of(mcpoauth.RefreshToken{
			ID:        newToken.ID,
			TokenHash: newToken.TokenHash,
			ClientID:  newToken.ClientID,
			TenantID:  newToken.UserID,
			SessionID: newToken.SessionID,
			Scopes:    newToken.Scopes,
			ExpiresAt: newToken.ExpiresAt,
			CreatedAt: newToken.CreatedAt,
		}), nil
}

type mcpSessionStoreAdapter struct {
	repo *mcprepo.SessionsRepo
}

func newMCPSessionStore(repo *mcprepo.SessionsRepo) *mcpSessionStoreAdapter {
	return ptrext.Of(mcpSessionStoreAdapter{repo: repo})
}

func (a *mcpSessionStoreAdapter) Create(ctx context.Context, session *mcpoauth.Session) error {
	created, err := a.repo.Create(ctx, mcprepo.CreateSessionParams{
		ClientID: session.ClientID,
		TenantID: session.TenantID,
		Scopes:   session.Scopes,
	})
	if err != nil {
		return err
	}
	session.ID = created.ID
	session.CreatedAt = created.CreatedAt
	session.LastUsed = created.LastActiveAt
	return nil
}

func (a *mcpSessionStoreAdapter) Touch(ctx context.Context, id uuid.UUID) error {
	return a.repo.Touch(ctx, id)
}

func (a *mcpSessionStoreAdapter) IsActive(ctx context.Context, id uuid.UUID) (bool, error) {
	return a.repo.IsActive(ctx, id)
}

type mcpToolPolicyStoreAdapter struct {
	repo *mcprepo.ToolPoliciesRepo
}

func newMCPToolPolicyStore(repo *mcprepo.ToolPoliciesRepo) *mcpToolPolicyStoreAdapter {
	return ptrext.Of(mcpToolPolicyStoreAdapter{repo: repo})
}

func (a *mcpToolPolicyStoreAdapter) GetByClientAndTool(ctx context.Context, clientID uuid.UUID, toolName string) (*policy.ToolPolicy, error) {
	p, err := a.repo.GetByClientAndTool(ctx, clientID, toolName)
	if errors.Is(err, mcprepo.ErrToolPolicyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(policy.ToolPolicy{
		Effect:         p.Effect,
		RateLimitRPM:   p.RateLimitRPM,
		RateLimitBurst: p.RateLimitBurst,
	}), nil
}

type mcpWorkflowAdapter struct {
	svc *workflowsvc.Service
}

func newMCPWorkflowAdapter(svc *workflowsvc.Service) *mcpWorkflowAdapter {
	return ptrext.Of(mcpWorkflowAdapter{svc: svc})
}

func (a *mcpWorkflowAdapter) Transition(ctx context.Context, tenantID string, feedbackID int64, toStateID, byUser, comment string) error {
	_, err := a.svc.Transition(ctx, tenantID, feedbackID, toStateID, byUser, comment)
	return err
}

type mcpIngestorAdapter struct {
	ingestor *ingest.Ingestor
}

func newMCPIngestorAdapter(i *ingest.Ingestor) *mcpIngestorAdapter {
	return ptrext.Of(mcpIngestorAdapter{ingestor: i})
}

func (a *mcpIngestorAdapter) Ingest(ctx context.Context, tenantID, userID string, in domain.IngestInput) (int64, error) {
	return a.ingestor.IngestRow(ctx, tenantID, uuid.Nil, in)
}

type mcpAuditAdapter struct {
	svc *auditlogsvc.Service
}

func newMCPAuditAdapter(svc *auditlogsvc.Service) *mcpAuditAdapter {
	return ptrext.Of(mcpAuditAdapter{svc: svc})
}

func (a *mcpAuditAdapter) Record(ctx context.Context, event tools.AuditEvent) error {
	return a.svc.Record(ctx, auditlogsvc.Event{
		TenantID: event.TenantID,
		Actor: auditlogsvc.Actor{
			Type:      "mcp",
			ID:        event.Actor,
			IP:        event.ActorIP,
			UserAgent: event.UserAgent,
		},
		Action:     event.Action,
		TargetType: event.TargetType,
		TargetID:   event.TargetID,
		Summary:    event.Summary,
		Before:     event.Before,
		After:      event.After,
	})
}

func mountConsoleStatic(ctx context.Context, r chi.Router) {
	const where = "main.mountConsoleStatic"
	dir, ok := consoleStaticDir()
	if !ok {
		logext.Warnf(ctx, "[%s] skipped: console dist not found", where)
		return
	}
	handler := consoleSPAHandler(dir)
	r.Get("/console", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/console/", http.StatusMovedPermanently)
	})
	r.Handle("/console/*", handler)
	logext.Infof(ctx, "[%s] mounted,dir:%s", where, dir)
}

func consoleStaticDir() (string, bool) {
	for _, dir := range []string{"/app/console", "console/dist"} {
		info, err := os.Stat(dir)
		if err == nil && info.IsDir() {
			return dir, true
		}
	}
	return "", false
}

func consoleSPAHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rel := strings.TrimPrefix(req.URL.Path, "/console/")
		clean := filepath.Clean(rel)
		if clean == "." ||
			filepath.IsAbs(clean) ||
			strings.HasPrefix(clean, "..") ||
			strings.Contains(clean, string(os.PathSeparator)+".."+string(os.PathSeparator)) ||
			strings.HasSuffix(clean, string(os.PathSeparator)+"..") ||
			strings.Contains(clean, "\\") {
			serveConsoleIndex(w, req, dir)
			return
		}

		baseAbs, err := filepath.Abs(dir)
		if err != nil {
			serveConsoleIndex(w, req, dir)
			return
		}
		targetAbs, err := filepath.Abs(filepath.Join(dir, clean))
		if err != nil {
			serveConsoleIndex(w, req, dir)
			return
		}
		if targetAbs != baseAbs && !strings.HasPrefix(targetAbs, baseAbs+string(os.PathSeparator)) {
			serveConsoleIndex(w, req, dir)
			return
		}

		if info, err := os.Stat(targetAbs); err == nil && !info.IsDir() {
			http.StripPrefix("/console/", files).ServeHTTP(w, req)
			return
		}
		serveConsoleIndex(w, req, dir)
	})
}

func serveConsoleIndex(w http.ResponseWriter, req *http.Request, dir string) {
	http.ServeFile(w, req, filepath.Join(dir, "index.html"))
}

// mountHealth registers the liveness probe at /healthz — the Google/Kubernetes
// convention, where the trailing "z" keeps a health route from colliding with a
// real application path. (The pre-0.2 /health route was removed; see CHANGELOG.)
// The otelchi /health-prefix filter in buildRouter keeps /healthz out of traces.
type readinessChecker interface {
	Ping(context.Context) error
}

var errServerDraining = errors.New("server draining")

type drainAwareReadiness struct {
	base     readinessChecker
	draining atomic.Bool
}

func newDrainAwareReadiness(base readinessChecker) *drainAwareReadiness {
	return ptrext.Of(drainAwareReadiness{base: base})
}

func (r *drainAwareReadiness) BeginDrain() {
	r.draining.Store(true)
}

func (r *drainAwareReadiness) Ping(ctx context.Context) error {
	if r == nil || r.draining.Load() {
		return errServerDraining
	}
	if r.base == nil {
		return errServerDraining
	}
	return r.base.Ping(ctx)
}

func mountHealth(r chi.Router, ready readinessChecker) {
	r.Get("/healthz", dispatcher.HealthzHandler())
	r.Get("/readyz", readyzHandler(ready))
	r.Get("/startupz", startupzHandler(ready))
}

// startupzHandler is like readyzHandler but with a longer timeout for slow
// startups (migration, warming). Kubernetes startup probes use this.
func startupzHandler(ready readinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if ready == nil {
			writeNotReady(req.Context(), w)
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
		defer cancel()
		if err := ready.Ping(ctx); err != nil {
			writeNotReady(req.Context(), w)
			return
		}
		dispatcher.WriteText(w, http.StatusOK, "ok")
	}
}

func readyzHandler(ready readinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if ready == nil {
			writeNotReady(req.Context(), w)
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := ready.Ping(ctx); err != nil {
			writeNotReady(req.Context(), w)
			return
		}
		dispatcher.WriteText(w, http.StatusOK, "ok")
	}
}

func writeNotReady(ctx context.Context, w http.ResponseWriter) {
	dispatcher.Reject(ctx, w, http.StatusServiceUnavailable, attunev1.ErrorCode_INTERNAL, "not ready")
}

// traceIDResponseHeader writes the active OTel trace_id into the
// X-Trace-Id response header so clients can correlate from their side.
// Must run AFTER otelchi — otherwise SpanContext is not yet populated.
func traceIDResponseHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if span.SpanContext().IsValid() {
			w.Header().Set("X-Trace-Id", span.SpanContext().TraceID().String())
		}
		next.ServeHTTP(w, r)
	})
}
