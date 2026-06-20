// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/server"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Handler holds the MCP server components.
type Handler struct {
	signer        *oauth.JWTSigner
	authServer    *oauth.AuthServer
	discovery     *oauth.DiscoveryHandler
	dispatcher    *jsonrpc.Dispatcher
	auth          *server.AuthMiddleware
	rateLimiter   *ratelimit.MemorySlidingLimiter
	oauthLimiter  *ratelimit.MemorySlidingLimiter
	rpmLimit      int
	oauthRPMLimit int
}

// Config holds MCP handler configuration.
type Config struct {
	BaseURL            string
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
	ClientValidator  server.ClientValidator
	SessionValidator server.SessionValidator
}

// NewHandler creates a new MCP handler.
func NewHandler(cfg Config, stores Stores, deps *tools.Deps) *Handler {
	signer := oauth.NewJWTSigner(cfg.JWTSecret, cfg.JWTIssuer)
	discovery := oauth.NewDiscoveryHandler(cfg.BaseURL)
	auth := server.NewAuthMiddleware(signer, stores.ClientValidator, stores.SessionValidator)

	authServer := oauth.NewAuthServer(
		stores.Clients,
		stores.Codes,
		stores.Tokens,
		stores.Sessions,
		signer,
		oauth.AuthServerConfig{
			BaseURL:    cfg.BaseURL,
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

	// OAuth endpoints get stricter rate limiting (10 RPM per IP by default)
	// to prevent brute force attacks on authorization codes and refresh tokens
	oauthRPMLimit := 10

	return ptrext.Of(Handler{
		signer:        signer,
		authServer:    authServer,
		discovery:     discovery,
		dispatcher:    d,
		auth:          auth,
		rateLimiter:   ratelimit.NewMemorySlidingLimiter(),
		oauthLimiter:  ratelimit.NewMemorySlidingLimiter(),
		rpmLimit:      rpmLimit,
		oauthRPMLimit: oauthRPMLimit,
	})
}

// Routes returns the chi router for MCP endpoints.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	// Security headers for all MCP endpoints
	r.Use(securityHeadersMiddleware)

	r.Get("/.well-known/oauth-protected-resource", h.discovery.ServeHTTP)

	r.Route("/oauth", func(r chi.Router) {
		r.Use(h.oauthRateLimitMiddleware)
		r.Get("/authorize", h.authServer.ServeAuthorize)
		r.Post("/token", h.authServer.ServeToken)
	})

	r.Group(func(r chi.Router) {
		r.Use(h.auth.Wrap)
		r.Use(h.rateLimitMiddleware)
		r.Post("/v1", jsonrpc.NewHandler(h.dispatcher).ServeHTTP)
	})

	return r
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

func (h *Handler) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := server.ClaimsFromContext(r.Context())
		if claims == nil {
			next.ServeHTTP(w, r)
			return
		}

		key := claims.ClientID.String()
		allowed, info, _ := h.rateLimiter.AllowWithInfo(r.Context(), key, h.rpmLimit, time.Minute)
		setRateLimitHeaders(w, info)
		if !allowed {
			logext.Warnf(r.Context(), "[mcp.rateLimitMiddleware] rate limit exceeded,client_id:%s,limit:%d", key, h.rpmLimit)
			w.Header().Set("Retry-After", formatRetryAfter(info.Reset))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
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

func getClientIP(r *http.Request) string {
	// Use RemoteAddr as the source IP
	// Note: In production behind a reverse proxy, X-Forwarded-For should be handled
	// by the trusted proxy configuration, not here
	ip := r.RemoteAddr
	if colonIdx := len(ip) - 1; colonIdx > 0 {
		for i := colonIdx; i >= 0; i-- {
			if ip[i] == ':' {
				return ip[:i]
			}
		}
	}
	return ip
}

func formatRetryAfter(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}

// Signer returns the JWT signer for issuing tokens.
func (h *Handler) Signer() *oauth.JWTSigner {
	return h.signer
}
