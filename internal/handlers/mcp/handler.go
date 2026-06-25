// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/policy"
	"github.com/Phixsura/attune/internal/mcp/server"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// SessionActivityTracker records tool-level last-seen context for an MCP session.
type SessionActivityTracker interface {
	RecordActivity(ctx context.Context, id uuid.UUID, toolName, decision, ip, userAgent string) error
}

// Handler holds the MCP server components.
type Handler struct {
	signer        *oauth.JWTSigner
	authServer    *oauth.AuthServer
	discovery     *oauth.DiscoveryHandler
	dispatcher    *jsonrpc.Dispatcher
	evaluator     *policy.Evaluator
	auth          *server.AuthMiddleware
	audit         tools.AuditRecorder
	sessions      SessionActivityTracker
	rateLimiter   *ratelimit.MemoryTokenBucketLimiter
	oauthLimiter  *ratelimit.MemorySlidingLimiter
	rpmLimit      int
	burstLimit    int
	oauthRPMLimit int
}

// Config holds MCP handler configuration.
type Config struct {
	PublicBaseURL      string
	JWTSecret          []byte
	JWTIssuer          string
	RateLimitPerMinute int
	RateLimitBurst     int
	TrustedProxyHops   int
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
}

// Stores holds the OAuth storage dependencies.
type Stores struct {
	Clients          oauth.ClientStore
	Codes            oauth.CodeStore
	Tokens           oauth.TokenStore
	Sessions         oauth.SessionStore
	ToolPolicies     policy.ToolPolicyReader
	ClientValidator  server.ClientValidator
	SessionValidator server.SessionValidator
	SessionActivity  SessionActivityTracker
}

// NewHandler creates a new MCP handler.
func NewHandler(cfg Config, stores Stores, deps *tools.Deps) *Handler {
	signer := oauth.NewJWTSigner(cfg.JWTSecret, cfg.JWTIssuer)
	discovery := oauth.NewDiscoveryHandler(cfg.PublicBaseURL, cfg.JWTIssuer)
	auth := server.NewAuthMiddleware(signer, stores.ClientValidator, stores.SessionValidator, discovery.ProtectedResourceMetadataURL())

	authServer := oauth.NewAuthServer(
		stores.Clients,
		stores.Codes,
		stores.Tokens,
		stores.Sessions,
		signer,
		oauth.AuthServerConfig{
			BaseURL:    cfg.PublicBaseURL,
			AccessTTL:  cfg.AccessTokenTTL,
			RefreshTTL: cfg.RefreshTokenTTL,
		},
	)

	d := jsonrpc.NewDispatcher()
	tools.RegisterReadTools(d, deps)
	tools.RegisterWriteTools(d, deps)
	tools.RegisterIngestTools(d, deps)

	rpmLimit := cfg.RateLimitPerMinute
	if rpmLimit <= 0 {
		rpmLimit = 60
	}
	burstLimit := cfg.RateLimitBurst
	if burstLimit <= 0 {
		burstLimit = 10
	}

	// OAuth endpoints get stricter rate limiting (10 RPM per IP by default)
	// to prevent brute force attacks on authorization codes and refresh tokens
	oauthRPMLimit := 10

	var audit tools.AuditRecorder
	if deps != nil {
		audit = deps.Audit
	}

	return ptrext.Of(Handler{
		signer:        signer,
		authServer:    authServer,
		discovery:     discovery,
		dispatcher:    d,
		evaluator:     policy.NewEvaluator(stores.Clients, stores.ToolPolicies),
		auth:          auth,
		audit:         audit,
		sessions:      stores.SessionActivity,
		rateLimiter:   ratelimit.NewMemoryTokenBucketLimiter(),
		oauthLimiter:  ratelimit.NewMemorySlidingLimiter(),
		rpmLimit:      rpmLimit,
		burstLimit:    burstLimit,
		oauthRPMLimit: oauthRPMLimit,
	})
}

// Routes returns the chi router for MCP endpoints.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	// Security headers for all MCP endpoints
	r.Use(securityHeadersMiddleware)

	// Legacy metadata paths remain mounted under /mcp for compatibility with
	// existing manual setups while the root well-known routes provide the
	// standards-compliant RFC 8414 / RFC 9728 locations.
	r.Get("/.well-known/oauth-protected-resource", h.discovery.ServeProtectedResourceMetadata)

	r.Route("/oauth", func(r chi.Router) {
		r.Use(h.oauthRateLimitMiddleware)
		r.Get("/.well-known/oauth-authorization-server", h.discovery.ServeAuthorizationServerMetadata)
		r.Get("/.well-known/openid-configuration", h.discovery.ServeOpenIDConfiguration)
		r.Get("/authorize", h.authServer.ServeAuthorize)
		r.Post("/token", h.authServer.ServeToken)
	})

	r.Group(func(r chi.Router) {
		r.Use(h.auth.Wrap)
		r.Use(h.governanceMiddleware)
		r.Post("/v1", jsonrpc.NewHandler(h.dispatcher).ServeHTTP)
	})

	return r
}

// MountWellKnownRoutes registers the RFC-compliant well-known metadata routes.
func (h *Handler) MountWellKnownRoutes(r chi.Router) error {
	protectedPath := h.discovery.ProtectedResourceMetadataPath()
	authPath := h.discovery.AuthorizationServerMetadataPath()
	openIDPath := h.discovery.OpenIDConfigurationPath()

	if protectedPath == "" || authPath == "" || openIDPath == "" {
		return fmt.Errorf("mcp discovery routes not configured")
	}

	r.Get(protectedPath, h.discovery.ServeProtectedResourceMetadata)
	r.Get(authPath, h.discovery.ServeAuthorizationServerMetadata)
	r.Get(openIDPath, h.discovery.ServeOpenIDConfiguration)

	return nil
}

// securityHeadersMiddleware adds security headers per industry best practices.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent clickjacking attacks
		w.Header().Set("X-Frame-Options", "DENY")
		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Control referrer information leakage
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) governanceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, body, ok := decodeJSONRPCRequest(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		claims := server.ClaimsFromContext(r.Context())
		if claims == nil || h.evaluator == nil {
			h.forwardWithBody(next, w, r, body)
			return
		}

		tool, found := tools.LookupTool(req.Method)
		if !found {
			h.forwardWithBody(next, w, r, body)
			return
		}

		decision, err := h.evaluator.Evaluate(r.Context(), claims, tool)
		if err != nil {
			logext.Errorf(r.Context(), "[mcp.governanceMiddleware] policy evaluate failed,client_id:%s,tool:%s,err:%v", claims.ClientID, req.Method, err)
			writeJSONRPCError(w, http.StatusInternalServerError, jsonrpc.CodeInternalError, req.ID, "internal error")
			return
		}

		ip := getClientIP(r)
		userAgent := r.UserAgent()

		if h.handleDeniedDecision(r.Context(), w, req.ID, claims, tool, decision, ip, userAgent) {
			return
		}

		if h.enforceClientRateLimit(r.Context(), w, req.ID, claims, decision, ip, userAgent) {
			return
		}

		if h.enforceToolRateLimit(r.Context(), w, req.ID, claims, decision, ip, userAgent) {
			return
		}

		h.forwardWithBody(next, w, r, body)
		h.recordActivity(r.Context(), claims.SessionID, tool.Name, policy.DecisionAllowed, ip, userAgent)
	})
}

func (h *Handler) forwardWithBody(next http.Handler, w http.ResponseWriter, r *http.Request, body []byte) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	next.ServeHTTP(w, r)
}

func (h *Handler) handleDeniedDecision(
	ctx context.Context,
	w http.ResponseWriter,
	requestID any,
	claims *oauth.AccessTokenClaims,
	tool tools.ToolMeta,
	decision *policy.Decision,
	ip, userAgent string,
) bool {
	if decision.Allowed {
		return false
	}
	if decision.Reason == policy.DecisionDeniedScope {
		w.Header().Set("WWW-Authenticate", server.BearerChallenge(h.discovery.ProtectedResourceMetadataURL(),
			server.BearerChallengeParam("error", "insufficient_scope"),
			server.BearerChallengeParam("scope", decision.Tool.RequiredScope),
		))
	}
	h.recordDecisionAudit(ctx, decision, ip, userAgent)
	h.recordActivity(ctx, claims.SessionID, tool.Name, decision.Reason, ip, userAgent)
	writeJSONRPCError(w, http.StatusForbidden, jsonrpc.CodeInvalidRequest, requestID, decisionMessage(decision))
	return true
}

func (h *Handler) enforceClientRateLimit(
	ctx context.Context,
	w http.ResponseWriter,
	requestID any,
	claims *oauth.AccessTokenClaims,
	decision *policy.Decision,
	ip, userAgent string,
) bool {
	clientLimit := effectivePositiveInt(decision.ClientRateRPM, h.rpmLimit)
	clientBurst := effectivePositiveInt(decision.ClientRateBurst, h.burstLimit)
	if clientLimit <= 0 {
		return false
	}

	return h.enforceRateLimit(
		ctx,
		w,
		requestID,
		claims.SessionID,
		decision,
		ip,
		userAgent,
		"client:"+claims.ClientID.String(),
		"client",
		clientLimit,
		clientBurst,
	)
}

func (h *Handler) enforceToolRateLimit(
	ctx context.Context,
	w http.ResponseWriter,
	requestID any,
	claims *oauth.AccessTokenClaims,
	decision *policy.Decision,
	ip, userAgent string,
) bool {
	toolLimit := effectivePositiveInt(decision.ToolRateRPM, decision.Tool.DefaultRPM)
	toolBurst := effectivePositiveInt(decision.ToolRateBurst, decision.Tool.DefaultBurst)
	if toolLimit <= 0 {
		return false
	}

	return h.enforceRateLimit(
		ctx,
		w,
		requestID,
		claims.SessionID,
		decision,
		ip,
		userAgent,
		"client:"+claims.ClientID.String()+":tool:"+decision.ToolName,
		"tool",
		toolLimit,
		toolBurst,
	)
}

func (h *Handler) enforceRateLimit(
	ctx context.Context,
	w http.ResponseWriter,
	requestID any,
	sessionID uuid.UUID,
	decision *policy.Decision,
	ip, userAgent, key, limitType string,
	limit, burst int,
) bool {
	allowed, info, _ := h.rateLimiter.AllowWithInfo(ctx, key, limit, burst)
	setRateLimitHeaders(w, info)
	if allowed {
		return false
	}

	logext.Warnf(
		ctx,
		"[mcp.governanceMiddleware] %s rate limit exceeded,client_id:%s,tool:%s,limit:%d,burst:%d",
		limitType,
		decision.ClientID,
		decision.ToolName,
		limit,
		burst,
	)
	w.Header().Set("Retry-After", formatRetryAfter(info.Reset))
	decision.Reason = policy.DecisionDeniedRateLimited
	h.recordDecisionAudit(ctx, decision, ip, userAgent)
	h.recordActivity(ctx, sessionID, decision.ToolName, policy.DecisionDeniedRateLimited, ip, userAgent)
	writeJSONRPCError(w, http.StatusTooManyRequests, jsonrpc.CodeInvalidRequest, requestID, "rate limit exceeded")
	return true
}

func effectivePositiveInt(value *int, fallback int) int {
	candidate := ptrext.IndirectOr(value, 0)
	if candidate > 0 {
		return candidate
	}
	return fallback
}

func (h *Handler) oauthRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use client IP as rate limit key for OAuth endpoints
		// This prevents brute force attacks on authorization codes and refresh tokens
		ip := getClientIP(r)
		allowed, info, _ := h.oauthLimiter.AllowWithInfo(r.Context(), ip, h.oauthRPMLimit, time.Minute)
		setRateLimitHeaders(w, info)
		if !allowed {
			logext.Warnf(r.Context(), "[mcp.oauthRateLimitMiddleware] rate limit exceeded,ip:%s,limit:%d,path:%s", ip, h.oauthRPMLimit, r.URL.Path)
			w.Header().Set("Retry-After", formatRetryAfter(info.Reset))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setRateLimitHeaders(w http.ResponseWriter, info ratelimit.RateLimitInfo) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(info.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(info.Remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(info.Reset).Unix(), 10))
}

func getClientIP(r *http.Request) string { return nethardening.ClientIPDefault(r) }

func formatRetryAfter(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}

func decodeJSONRPCRequest(r *http.Request) (*jsonrpc.Request, []byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		logext.Warnf(r.Context(), "[mcp.decodeJSONRPCRequest] read body failed: %v", err)
		return nil, nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var req jsonrpc.Request
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, body, false
	}
	return ptrext.Of(req), body, true
}

func writeJSONRPCError(w http.ResponseWriter, status, code int, id any, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(id, code, message, nil))
}

func decisionMessage(decision *policy.Decision) string {
	switch decision.Reason {
	case policy.DecisionDeniedScope:
		return "insufficient scope"
	case policy.DecisionDeniedPolicy:
		return "tool not allowed for client"
	case policy.DecisionDeniedClient:
		return "client not authorized"
	case policy.DecisionDeniedRateLimited:
		return "rate limit exceeded"
	default:
		return "request denied"
	}
}

func (h *Handler) recordActivity(ctx context.Context, sessionID uuid.UUID, toolName, decision, ip, userAgent string) {
	if h.sessions == nil {
		return
	}
	if err := h.sessions.RecordActivity(ctx, sessionID, toolName, decision, ip, userAgent); err != nil {
		logext.Warnf(ctx, "[mcp.recordActivity] failed,session_id:%s,tool:%s,err:%v", sessionID, toolName, err)
	}
}

func (h *Handler) recordDecisionAudit(ctx context.Context, decision *policy.Decision, ip, userAgent string) {
	if h.audit == nil {
		return
	}
	action := domain.AuditActionMCPToolAuthorizeDenied
	summary := "Denied MCP tool call"
	if decision.Reason == policy.DecisionDeniedRateLimited {
		action = domain.AuditActionMCPToolRateLimited
		summary = "Rate limited MCP tool call"
	}
	if err := h.audit.Record(ctx, tools.AuditEvent{
		TenantID:   server.TenantIDFromContext(ctx),
		Actor:      tools.MCPPrincipal(ctx),
		ActorIP:    ip,
		UserAgent:  userAgent,
		Action:     action,
		TargetType: "mcp_tool",
		TargetID:   decision.ToolName,
		Summary:    summary + ": " + decision.ToolName,
		After: map[string]any{
			"client_id":          decision.ClientID.String(),
			"session_id":         decision.SessionID.String(),
			"tool_name":          decision.ToolName,
			"tool_risk":          decision.Tool.Risk,
			"policy_mode":        decision.ClientPolicyMode,
			"policy_effect":      decision.PolicyEffect,
			"required_scope":     decision.Tool.RequiredScope,
			"decision_reason":    decision.Reason,
			"tool_default_rpm":   decision.Tool.DefaultRPM,
			"tool_default_burst": decision.Tool.DefaultBurst,
		},
	}); err != nil {
		logext.Warnf(ctx, "[mcp.recordDecisionAudit] failed,tool:%s,reason:%s,err:%v", decision.ToolName, decision.Reason, err)
	}
}

// Signer returns the JWT signer for issuing tokens.
func (h *Handler) Signer() *oauth.JWTSigner {
	return h.signer
}
