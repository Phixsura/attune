// SPDX-License-Identifier: Apache-2.0

// Package mcpclient provides console handlers for MCP OAuth client management.
// Endpoints are mounted under /fb/v1/console/mcp/clients and allow admins to
// register, list, and revoke MCP OAuth clients for AI agent access.
package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

type clientStore interface {
	ListByTenant(ctx context.Context, tenantID string) ([]mcprepo.Client, error)
	Create(ctx context.Context, p mcprepo.CreateClientParams) (*mcprepo.Client, error)
	GetByID(ctx context.Context, id uuid.UUID) (*mcprepo.Client, error)
	Revoke(ctx context.Context, id uuid.UUID) error
	UpdateGovernance(ctx context.Context, p mcprepo.UpdateClientGovernanceParams) (*mcprepo.Client, error)
}

type tokenStore interface {
	Revoke(ctx context.Context, id uuid.UUID) error
	RevokeByClient(ctx context.Context, clientID uuid.UUID) (int64, error)
	RevokeBySession(ctx context.Context, sessionID uuid.UUID) (int64, error)
	ListByClient(ctx context.Context, clientID uuid.UUID) ([]mcprepo.RefreshToken, error)
}

type sessionStore interface {
	CloseByClient(ctx context.Context, clientID uuid.UUID) (int64, error)
	CloseWithReason(ctx context.Context, id uuid.UUID, reason, closedBy string) error
	GetByID(ctx context.Context, id uuid.UUID) (*mcprepo.Session, error)
	ListByClient(ctx context.Context, clientID uuid.UUID) ([]mcprepo.Session, error)
}

type toolPolicyStore interface {
	ListByClient(ctx context.Context, clientID uuid.UUID) ([]mcprepo.ToolPolicy, error)
	ReplaceByClient(ctx context.Context, clientID uuid.UUID, policies []mcprepo.UpsertToolPolicyParams) error
}

// Handler implements MCP OAuth client management endpoints.
type Handler struct {
	clients      clientStore
	tokens       tokenStore
	sessions     sessionStore
	toolPolicies toolPolicyStore
	audit        auditRecorder
	connection   *ConnectionProfile
}

// NewHandler creates a new MCP client handler.
func NewHandler(clients clientStore, tokens tokenStore, sessions sessionStore, toolPolicies toolPolicyStore) *Handler {
	return ptrext.Of(Handler{
		clients:      clients,
		tokens:       tokens,
		sessions:     sessions,
		toolPolicies: toolPolicies,
	})
}

// SetAuditLogger configures the audit logger for tracking client operations.
func (h *Handler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

// SetConnectionProfile configures the public MCP connection details exposed to Console admins.
func (h *Handler) SetConnectionProfile(publicBaseURL, issuer string) {
	h.connection = newConnectionProfile(publicBaseURL, issuer)
}

// ListRequest is the request shape for listing MCP clients.
type ListRequest struct{}

// ListResponse is the response shape for listing MCP clients.
type ListResponse struct {
	Clients []ClientDTO `json:"clients"`
}

// ClientDTO is the wire representation of an MCP OAuth client.
type ClientDTO struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	RedirectURIs   []string `json:"redirect_uris"`
	Scopes         []string `json:"scopes"`
	ToolPolicyMode string   `json:"tool_policy_mode"`
	RateLimitRPM   *int     `json:"rate_limit_rpm,omitempty"`
	RateLimitBurst *int     `json:"rate_limit_burst,omitempty"`
	CreatedAt      string   `json:"created_at"`
	CreatedBy      string   `json:"created_by"`
	RevokedAt      *string  `json:"revoked_at,omitempty"`
}

// List returns all MCP OAuth clients for the tenant.
func (h *Handler) List(ctx context.Context, auth *session.AuthCtx, _ *ListRequest) (*ListResponse, error) {
	clients, err := h.clients.ListByTenant(ctx, auth.TenantID)
	if err != nil {
		return nil, err
	}
	out := make([]ClientDTO, 0, len(clients))
	for _, c := range clients {
		out = append(out, toDTO(c))
	}
	return ptrext.Of(ListResponse{Clients: out}), nil
}

// CreateRequest is the request shape for creating an MCP client.
type CreateRequest struct {
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
	Scopes       []string `json:"scopes"`
}

// CreateResponse is the response shape for creating an MCP client.
type CreateResponse struct {
	Client ClientDTO `json:"client"`
}

// Create registers a new MCP OAuth client.
func (h *Handler) Create(ctx context.Context, auth *session.AuthCtx, req *CreateRequest, r *http.Request) (*CreateResponse, error) {
	name := strings.TrimSpace(req.Name)
	if err := validateClientName(name); err != nil {
		return nil, err
	}
	if err := validateScopes(req.Scopes); err != nil {
		return nil, err
	}
	if len(req.RedirectURIs) == 0 {
		return nil, ptrext.Of(ValidationError{Field: "redirect_uris", Message: "at least one redirect URI is required"})
	}
	if err := validateRedirectURIs(req.RedirectURIs); err != nil {
		return nil, err
	}

	client, err := h.clients.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     auth.TenantID,
		Name:         name,
		RedirectURIs: req.RedirectURIs,
		Scopes:       req.Scopes,
		CreatedBy:    auth.UserID,
	})
	if err != nil {
		return nil, err
	}

	if h.audit != nil {
		h.audit.Record(ctx, auditlogsvc.Event{ //nolint:errcheck
			TenantID:   auth.TenantID,
			Actor:      auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, r),
			Action:     domain.AuditActionMCPClientCreate,
			TargetType: "mcp_client",
			TargetID:   client.ID.String(),
			Summary:    "Created MCP OAuth client: " + client.Name,
			After:      toDTO(ptrext.Indirect(client)),
		})
	}

	return ptrext.Of(CreateResponse{Client: toDTO(ptrext.Indirect(client))}), nil
}

// RevokeRequest is the request shape for revoking an MCP client.
type RevokeRequest struct {
	ID string `json:"id"`
}

// RevokeResponse is the response shape for revoking an MCP client.
type RevokeResponse struct{}

// Revoke marks an MCP OAuth client as revoked.
func (h *Handler) Revoke(ctx context.Context, auth *session.AuthCtx, req *RevokeRequest, r *http.Request) (*RevokeResponse, error) {
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return nil, ptrext.Of(ValidationError{Field: "id", Message: "invalid client ID"})
	}

	client, err := h.loadClientForTenant(ctx, auth.TenantID, id)
	if err != nil {
		return nil, err
	}

	if err := h.clients.Revoke(ctx, id); err != nil {
		return nil, err
	}

	// Revoke all refresh tokens and close all sessions for this client
	if h.tokens != nil {
		_, _ = h.tokens.RevokeByClient(ctx, id)
	}
	if h.sessions != nil {
		_, _ = h.sessions.CloseByClient(ctx, id)
	}

	if h.audit != nil {
		h.audit.Record(ctx, auditlogsvc.Event{ //nolint:errcheck
			TenantID:   auth.TenantID,
			Actor:      auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, r),
			Action:     domain.AuditActionMCPClientRevoke,
			TargetType: "mcp_client",
			TargetID:   client.ID.String(),
			Summary:    "Revoked MCP OAuth client: " + client.Name,
			Before:     toDTO(ptrext.Indirect(client)),
		})
	}

	return ptrext.Of(RevokeResponse{}), nil
}

func toDTO(c mcprepo.Client) ClientDTO {
	dto := ClientDTO{
		ID:             c.ID.String(),
		Name:           c.Name,
		RedirectURIs:   c.RedirectURIs,
		Scopes:         c.Scopes,
		ToolPolicyMode: c.ToolPolicyMode,
		RateLimitRPM:   c.RateLimitRPM,
		RateLimitBurst: c.RateLimitBurst,
		CreatedAt:      c.CreatedAt.UTC().Format(time.RFC3339),
		CreatedBy:      c.CreatedBy,
	}
	if c.RevokedAt != nil {
		s := c.RevokedAt.UTC().Format(time.RFC3339)
		dto.RevokedAt = ptrext.Of(s)
	}
	return dto
}

func (h *Handler) loadClientForTenant(ctx context.Context, tenantID string, id uuid.UUID) (*mcprepo.Client, error) {
	client, err := h.clients.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if client.TenantID != tenantID {
		return nil, mcprepo.ErrClientNotFound
	}
	return client, nil
}

func ensureClientMutable(client *mcprepo.Client) error {
	if client != nil && client.RevokedAt != nil {
		return ptrext.Of(ValidationError{Field: "id", Message: "client is revoked"})
	}
	return nil
}

func validateScopes(scopes []string) error {
	valid := map[string]bool{
		domain.MCPScopeRead:   true,
		domain.MCPScopeWrite:  true,
		domain.MCPScopeIngest: true,
	}
	for _, s := range scopes {
		if !valid[s] {
			return ptrext.Of(ValidationError{Field: "scopes", Message: "invalid scope: " + s})
		}
	}
	return nil
}

func validateClientName(name string) error {
	if name == "" {
		return ptrext.Of(ValidationError{Field: "name", Message: "name is required"})
	}
	if utf8.RuneCountInString(name) > 128 {
		return ptrext.Of(ValidationError{Field: "name", Message: "name exceeds 128 characters"})
	}
	return nil
}

func validateRedirectURIs(uris []string) error {
	for _, uri := range uris {
		u, err := url.Parse(uri)
		if err != nil {
			return ptrext.Of(ValidationError{Field: "redirect_uris", Message: "invalid URL: " + uri})
		}
		if u.Fragment != "" {
			return ptrext.Of(ValidationError{Field: "redirect_uris", Message: "fragment not allowed in redirect URI"})
		}
		if u.Scheme == "javascript" || u.Scheme == "data" || u.Scheme == "file" || u.Scheme == "vbscript" {
			return ptrext.Of(ValidationError{Field: "redirect_uris", Message: "dangerous URI scheme not allowed"})
		}
		if u.Scheme == "" || u.Host == "" || u.Opaque != "" {
			return ptrext.Of(ValidationError{
				Field:   "redirect_uris",
				Message: "redirect URI must be an absolute URI with a host",
			})
		}
		if u.Scheme != "https" && !isLoopbackURI(u) {
			return ptrext.Of(ValidationError{Field: "redirect_uris", Message: "redirect URI must use HTTPS (except for localhost)"})
		}
	}
	return nil
}

func isLoopbackURI(u *url.URL) bool {
	return nethardening.IsLoopbackHost(u.Hostname())
}

// ValidationError represents a validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// ServeList handles GET /mcp/clients.
func (h *Handler) ServeList(w http.ResponseWriter, r *http.Request) {
	auth := session.FromContext(r.Context())
	resp, err := h.List(r.Context(), auth, ptrext.Of(ListRequest{}))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ServeCreate handles POST /mcp/clients.
func (h *Handler) ServeCreate(w http.ResponseWriter, r *http.Request) {
	auth := session.FromContext(r.Context())

	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ptrext.Of(ValidationError{Field: "body", Message: "invalid JSON"}))
		return
	}

	resp, err := h.Create(r.Context(), auth, ptrext.Of(req), r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// ServeRevoke handles DELETE /mcp/clients/{id}.
func (h *Handler) ServeRevoke(w http.ResponseWriter, r *http.Request) {
	auth := session.FromContext(r.Context())

	id := chi.URLParam(r, "id")
	req := ptrext.Of(RevokeRequest{ID: id})

	_, err := h.Revoke(r.Context(), auth, req, r)
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, err error) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		writeErrorJSON(w, http.StatusBadRequest, "validation_error", ve.Error())
		return
	}
	if errors.Is(err, mcprepo.ErrClientNameConflict) {
		writeErrorJSON(w, http.StatusConflict, "conflict", "client name already exists")
		return
	}
	if errors.Is(err, mcprepo.ErrInvalidInput) {
		writeErrorJSON(w, http.StatusBadRequest, "validation_error", "client field is invalid")
		return
	}
	if errors.Is(err, mcprepo.ErrClientNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "client not found")
		return
	}
	if errors.Is(err, mcprepo.ErrSessionNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	if errors.Is(err, mcprepo.ErrRefreshTokenNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "refresh grant not found")
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal error")
}

func writeErrorJSON(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"code":    code,
		"message": message,
		"error":   message,
	})
}
