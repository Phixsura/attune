package attune

import (
	"context"
	"net/http"
	"net/url"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// Re-exported MCP client governance wire types (generated). MCP client
// governance needs a key with the `mcpclient:admin` scope.
type (
	MCPClient                            = attunev1.MCPClient
	MCPSession                           = attunev1.MCPSession
	MCPRefreshGrant                      = attunev1.MCPRefreshGrant
	MCPClientToolPolicy                  = attunev1.MCPClientToolPolicy
	MCPConnectionProfile                 = attunev1.MCPConnectionProfile
	MCPToolPolicyInput                   = attunev1.MCPToolPolicyInput
	ListMCPClientsResponse               = attunev1.ListMCPClientsResponse
	CreateMCPClientRequest               = attunev1.CreateMCPClientRequest
	CreateMCPClientResponse              = attunev1.CreateMCPClientResponse
	GetMCPClientResponse                 = attunev1.GetMCPClientResponse
	RevokeMCPClientResponse              = attunev1.RevokeMCPClientResponse
	UpdateMCPClientRequest               = attunev1.UpdateMCPClientRequest
	UpdateMCPClientResponse              = attunev1.UpdateMCPClientResponse
	ReplaceMCPClientToolPoliciesRequest  = attunev1.ReplaceMCPClientToolPoliciesRequest
	ReplaceMCPClientToolPoliciesResponse = attunev1.ReplaceMCPClientToolPoliciesResponse
	RevokeMCPSessionResponse             = attunev1.RevokeMCPSessionResponse
	RevokeMCPRefreshGrantResponse        = attunev1.RevokeMCPRefreshGrantResponse
)

// ListMCPClients returns MCP OAuth clients for the caller's tenant.
func (c *Client) ListMCPClients(ctx context.Context) (*ListMCPClientsResponse, error) {
	var out attunev1.ListMCPClientsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/mcp/clients", nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateMCPClient creates one MCP OAuth client.
func (c *Client) CreateMCPClient(ctx context.Context, req *CreateMCPClientRequest) (*CreateMCPClientResponse, error) {
	if err := requireRequest(req, "mcp client request must not be nil"); err != nil {
		return nil, err
	}
	name, err := normalizeMCPClientName(req.GetName())
	if err != nil {
		return nil, err
	}
	if err := validateMCPRedirectURIs(req.GetRedirectUris()); err != nil {
		return nil, err
	}
	scopes, err := normalizeMCPScopes(req.GetScopes())
	if err != nil {
		return nil, err
	}
	req.Name = name
	req.RedirectUris = append([]string(nil), req.GetRedirectUris()...)
	req.Scopes = scopes
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}
	var out attunev1.CreateMCPClientResponse
	if err := c.do(ctx, http.MethodPost, "/v1/mcp/clients", payload, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMCPClient returns one MCP OAuth client by id.
func (c *Client) GetMCPClient(ctx context.Context, id string) (*GetMCPClientResponse, error) {
	if !validUUID(id) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "mcp client id is invalid"}
	}
	var out attunev1.GetMCPClientResponse
	if err := c.do(ctx, http.MethodGet, "/v1/mcp/clients/"+url.PathEscape(id), nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeMCPClient revokes one MCP OAuth client by id.
func (c *Client) RevokeMCPClient(ctx context.Context, id string) (*RevokeMCPClientResponse, error) {
	if !validUUID(id) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "mcp client id is invalid"}
	}
	var out attunev1.RevokeMCPClientResponse
	if err := c.do(ctx, http.MethodDelete, "/v1/mcp/clients/"+url.PathEscape(id), nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateMCPClient updates one MCP OAuth client.
func (c *Client) UpdateMCPClient(ctx context.Context, req *UpdateMCPClientRequest) (*UpdateMCPClientResponse, error) {
	if err := requireRequest(req, "mcp client request must not be nil"); err != nil {
		return nil, err
	}
	if !validUUID(req.GetId()) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "mcp client id is invalid"}
	}
	if req.ToolPolicyMode == nil && req.RateLimitRpm == nil && req.RateLimitBurst == nil {
		return nil, &AttuneError{
			Code:    CodeBadRequest,
			Message: "mcp update request must include at least one of toolPolicyMode, rateLimitRpm, or rateLimitBurst",
		}
	}
	toolPolicyMode, err := normalizeOptionalMCPToolPolicyMode(req.ToolPolicyMode)
	if err != nil {
		return nil, err
	}
	if err := validateOptionalPositiveMCPInt32(req.RateLimitRpm, "mcp rateLimitRpm must be a positive integer"); err != nil {
		return nil, err
	}
	if err := validateOptionalPositiveMCPInt32(req.RateLimitBurst, "mcp rateLimitBurst must be a positive integer"); err != nil {
		return nil, err
	}
	req.ToolPolicyMode = toolPolicyMode
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}
	var out attunev1.UpdateMCPClientResponse
	if err := c.do(ctx, http.MethodPatch, "/v1/mcp/clients/"+url.PathEscape(req.GetId()), payload, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceMCPClientToolPolicies replaces one client's explicit tool-policy overrides.
func (c *Client) ReplaceMCPClientToolPolicies(ctx context.Context, req *ReplaceMCPClientToolPoliciesRequest) (*ReplaceMCPClientToolPoliciesResponse, error) {
	if err := requireRequest(req, "mcp tool policy request must not be nil"); err != nil {
		return nil, err
	}
	if !validUUID(req.GetId()) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "mcp client id is invalid"}
	}
	if req.Policies == nil {
		return nil, &AttuneError{
			Code:    CodeBadRequest,
			Message: "mcp policies must be provided; use an empty slice to clear all policies",
		}
	}
	policies, err := normalizeMCPToolPolicies(req.GetPolicies())
	if err != nil {
		return nil, err
	}
	req.Policies = policies
	payload, err := protojsonMarshal.Marshal(req)
	if err != nil {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "invalid request body", cause: err}
	}
	var out attunev1.ReplaceMCPClientToolPoliciesResponse
	if err := c.do(ctx, http.MethodPut, "/v1/mcp/clients/"+url.PathEscape(req.GetId())+"/tool-policies", payload, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeMCPRefreshGrant revokes one refresh grant by id.
func (c *Client) RevokeMCPRefreshGrant(ctx context.Context, clientID, grantID string) (*RevokeMCPRefreshGrantResponse, error) {
	if !validUUID(clientID) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "mcp client id is invalid"}
	}
	if !validUUID(grantID) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "mcp refresh grant id is invalid"}
	}
	var out attunev1.RevokeMCPRefreshGrantResponse
	if err := c.do(ctx, http.MethodDelete, "/v1/mcp/clients/"+url.PathEscape(clientID)+"/grants/"+url.PathEscape(grantID), nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeMCPSession revokes one MCP session by id.
func (c *Client) RevokeMCPSession(ctx context.Context, clientID, sessionID string) (*RevokeMCPSessionResponse, error) {
	if !validUUID(clientID) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "mcp client id is invalid"}
	}
	if !validUUID(sessionID) {
		return nil, &AttuneError{Code: CodeBadRequest, Message: "mcp session id is invalid"}
	}
	var out attunev1.RevokeMCPSessionResponse
	if err := c.do(ctx, http.MethodDelete, "/v1/mcp/clients/"+url.PathEscape(clientID)+"/sessions/"+url.PathEscape(sessionID), nil, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}
