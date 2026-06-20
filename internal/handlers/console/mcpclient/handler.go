// SPDX-License-Identifier: Apache-2.0

// Package mcpclient provides console handlers for MCP OAuth client management.
// Endpoints are mounted under /fb/v1/console/mcp/clients and allow admins to
// register, list, and revoke MCP OAuth clients for AI agent access.
package mcpclient

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

// Handler implements MCP OAuth client management endpoints.
type Handler struct {
	clients *mcprepo.ClientsRepo
	audit   auditRecorder
}

// NewHandler creates a new MCP client handler.
func NewHandler(clients *mcprepo.ClientsRepo) *Handler {
	return ptrext.Of(Handler{clients: clients})
}

// SetAuditLogger configures the audit logger for tracking client operations.
func (h *Handler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

// ListRequest is the request shape for listing MCP clients.
type ListRequest struct{}

// ListResponse is the response shape for listing MCP clients.
type ListResponse struct {
	Clients []ClientDTO `json:"clients"`
}

// ClientDTO is the wire representation of an MCP OAuth client.
type ClientDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
	Scopes       []string `json:"scopes"`
	CreatedAt    string   `json:"created_at"`
	CreatedBy    string   `json:"created_by"`
	RevokedAt    *string  `json:"revoked_at,omitempty"`
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
	if err := validateScopes(req.Scopes); err != nil {
		return nil, err
	}
	if len(req.RedirectURIs) == 0 {
		return nil, ptrext.Of(ValidationError{Field: "redirect_uris", Message: "at least one redirect URI is required"})
	}

	client, err := h.clients.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     auth.TenantID,
		Name:         req.Name,
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

	client, err := h.clients.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if client.TenantID != auth.TenantID {
		return nil, mcprepo.ErrClientNotFound
	}

	if err := h.clients.Revoke(ctx, id); err != nil {
		return nil, err
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
		ID:           c.ID.String(),
		Name:         c.Name,
		RedirectURIs: c.RedirectURIs,
		Scopes:       c.Scopes,
		CreatedAt:    c.CreatedAt.UTC().Format(time.RFC3339),
		CreatedBy:    c.CreatedBy,
	}
	if c.RevokedAt != nil {
		s := c.RevokedAt.UTC().Format(time.RFC3339)
		dto.RevokedAt = ptrext.Of(s)
	}
	return dto
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

// ValidationError represents a validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
