// SPDX-License-Identifier: Apache-2.0

package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// GetResponse is the detail shape for one MCP client governance view.
type GetResponse struct {
	Client        ClientDTO          `json:"client"`
	Sessions      []SessionDTO       `json:"sessions"`
	RefreshGrants []RefreshGrantDTO  `json:"refresh_grants"`
	Tools         []ClientToolPolicy `json:"tools"`
	Connection    *ConnectionProfile `json:"connection,omitempty"`
}

// SessionDTO is the wire representation of one MCP session.
type SessionDTO struct {
	ID            string   `json:"id"`
	Scopes        []string `json:"scopes"`
	LastToolName  string   `json:"last_tool_name"`
	LastDecision  string   `json:"last_decision"`
	LastIP        string   `json:"last_ip"`
	LastUserAgent string   `json:"last_user_agent"`
	LastActiveAt  string   `json:"last_active_at"`
	CreatedAt     string   `json:"created_at"`
	ClosedReason  string   `json:"closed_reason"`
	ClosedBy      string   `json:"closed_by"`
	ClosedAt      *string  `json:"closed_at,omitempty"`
}

// RefreshGrantDTO is the safe wire representation of an active refresh grant.
type RefreshGrantDTO struct {
	ID        string   `json:"id"`
	SessionID string   `json:"session_id"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
	CreatedAt string   `json:"created_at"`
}

// ClientToolAlias describes one compatibility alias for a tool.
type ClientToolAlias struct {
	Name        string `json:"name"`
	Deprecated  bool   `json:"deprecated"`
	Replacement string `json:"replacement"`
}

// ClientToolProvenance describes the delivery metadata for a distributable extension.
type ClientToolProvenance struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
}

// ClientToolPolicy describes one catalogued tool and the client's current effective policy.
type ClientToolPolicy struct {
	Name             string                `json:"name"`
	Kind             string                `json:"kind"`
	Owner            string                `json:"owner"`
	EnabledByDefault bool                  `json:"enabled_by_default"`
	Deprecated       bool                  `json:"deprecated"`
	Replacement      string                `json:"replacement"`
	Aliases          []ClientToolAlias     `json:"aliases,omitempty"`
	Provenance       *ClientToolProvenance `json:"provenance,omitempty"`
	RequiredScope    string                `json:"required_scope"`
	Risk             string                `json:"risk"`
	DataClass        string                `json:"data_class"`
	ReadOnlyHint     bool                  `json:"read_only_hint"`
	DestructiveHint  bool                  `json:"destructive_hint"`
	OpenWorldHint    bool                  `json:"open_world_hint"`
	DefaultRPM       int                   `json:"default_rpm"`
	DefaultBurst     int                   `json:"default_burst"`
	ScopeGranted     bool                  `json:"scope_granted"`
	EffectiveAllowed bool                  `json:"effective_allowed"`
	Effect           string                `json:"effect"`
	RateLimitRPM     *int                  `json:"rate_limit_rpm,omitempty"`
	RateLimitBurst   *int                  `json:"rate_limit_burst,omitempty"`
}

// UpdateRequest updates mutable client governance settings.
type UpdateRequest struct {
	ToolPolicyMode *string `json:"tool_policy_mode"`
	RateLimitRPM   *int    `json:"rate_limit_rpm"`
	RateLimitBurst *int    `json:"rate_limit_burst"`

	toolPolicyModeSet bool
	rateLimitRPMSet   bool
	rateLimitBurstSet bool
}

func (r *UpdateRequest) UnmarshalJSON(data []byte) error {
	type updateRequestJSON struct {
		ToolPolicyMode *string `json:"tool_policy_mode"`
		RateLimitRPM   *int    `json:"rate_limit_rpm"`
		RateLimitBurst *int    `json:"rate_limit_burst"`
	}

	var raw updateRequestJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	r.ToolPolicyMode = raw.ToolPolicyMode
	r.RateLimitRPM = raw.RateLimitRPM
	r.RateLimitBurst = raw.RateLimitBurst
	_, r.toolPolicyModeSet = fields["tool_policy_mode"]
	_, r.rateLimitRPMSet = fields["rate_limit_rpm"]
	_, r.rateLimitBurstSet = fields["rate_limit_burst"]
	return nil
}

func (r *UpdateRequest) hasAnyField() bool {
	return r.hasToolPolicyMode() || r.hasRateLimitRPM() || r.hasRateLimitBurst()
}

func (r *UpdateRequest) hasToolPolicyMode() bool {
	return r.toolPolicyModeSet || r.ToolPolicyMode != nil
}

func (r *UpdateRequest) hasRateLimitRPM() bool {
	return r.rateLimitRPMSet || r.RateLimitRPM != nil
}

func (r *UpdateRequest) hasRateLimitBurst() bool {
	return r.rateLimitBurstSet || r.RateLimitBurst != nil
}

// UpdateResponse returns the updated client.
type UpdateResponse struct {
	Client ClientDTO `json:"client"`
}

// ReplaceToolPoliciesRequest atomically replaces explicit tool policy overrides.
type ReplaceToolPoliciesRequest struct {
	Policies []ToolPolicyInput `json:"policies"`
}

// ToolPolicyInput is one explicit override row in the admin API.
type ToolPolicyInput struct {
	ToolName       string `json:"tool_name"`
	Effect         string `json:"effect"`
	RateLimitRPM   *int   `json:"rate_limit_rpm"`
	RateLimitBurst *int   `json:"rate_limit_burst"`
}

// ReplaceToolPoliciesResponse returns the rebuilt effective tool table.
type ReplaceToolPoliciesResponse struct {
	Tools []ClientToolPolicy `json:"tools"`
}

// RevokeSessionResponse is empty on success.
type RevokeSessionResponse struct{}

// RevokeRefreshGrantResponse is empty on success.
type RevokeRefreshGrantResponse struct{}

// Get returns one client with sessions, active grants, and effective tool policies.
func (h *Handler) Get(ctx context.Context, auth *session.AuthCtx, id string) (*GetResponse, error) {
	clientID, err := uuid.Parse(id)
	if err != nil {
		return nil, ptrext.Of(ValidationError{Field: "id", Message: "invalid client ID"})
	}

	client, err := h.loadClientForTenant(ctx, auth.TenantID, clientID)
	if err != nil {
		return nil, err
	}

	var sessions []mcprepo.Session
	if h.sessions != nil {
		sessions, err = h.sessions.ListByClient(ctx, client.ID)
		if err != nil {
			return nil, err
		}
	}

	var grants []mcprepo.RefreshToken
	if h.tokens != nil {
		grants, err = h.tokens.ListByClient(ctx, client.ID)
		if err != nil {
			return nil, err
		}
	}

	toolPolicies, err := h.listToolPolicies(ctx, client.ID)
	if err != nil {
		return nil, err
	}

	return ptrext.Of(GetResponse{
		Client:        toDTO(ptrext.Indirect(client)),
		Sessions:      toSessionDTOs(sessions),
		RefreshGrants: toGrantDTOs(grants),
		Tools:         buildClientToolPolicies(ptrext.Indirect(client), toolPolicies),
		Connection:    h.connection,
	}), nil
}

// Update changes one client's governance defaults.
func (h *Handler) Update(ctx context.Context, auth *session.AuthCtx, id string, req *UpdateRequest, r *http.Request) (*UpdateResponse, error) {
	clientID, err := uuid.Parse(id)
	if err != nil {
		return nil, ptrext.Of(ValidationError{Field: "id", Message: "invalid client ID"})
	}
	if req == nil {
		return nil, ptrext.Of(ValidationError{Field: "body", Message: "request body is required"})
	}
	if !req.hasAnyField() {
		return nil, ptrext.Of(ValidationError{
			Field:   "body",
			Message: "at least one of tool_policy_mode, rate_limit_rpm, or rate_limit_burst is required",
		})
	}
	if err := validateOptionalToolPolicyMode(req.ToolPolicyMode, req.hasToolPolicyMode()); err != nil {
		return nil, err
	}
	if err := validateOptionalPositiveInt("rate_limit_rpm", req.RateLimitRPM); err != nil {
		return nil, err
	}
	if err := validateOptionalPositiveInt("rate_limit_burst", req.RateLimitBurst); err != nil {
		return nil, err
	}

	before, err := h.loadClientForTenant(ctx, auth.TenantID, clientID)
	if err != nil {
		return nil, err
	}
	if err := ensureClientMutable(before); err != nil {
		return nil, err
	}

	toolPolicyMode := before.ToolPolicyMode
	if req.hasToolPolicyMode() {
		toolPolicyMode = strings.TrimSpace(ptrext.IndirectOr(req.ToolPolicyMode, ""))
	}
	rateLimitRPM := before.RateLimitRPM
	if req.hasRateLimitRPM() {
		rateLimitRPM = req.RateLimitRPM
	}
	rateLimitBurst := before.RateLimitBurst
	if req.hasRateLimitBurst() {
		rateLimitBurst = req.RateLimitBurst
	}

	updated, err := h.clients.UpdateGovernance(ctx, mcprepo.UpdateClientGovernanceParams{
		ID:             clientID,
		ToolPolicyMode: toolPolicyMode,
		RateLimitRPM:   rateLimitRPM,
		RateLimitBurst: rateLimitBurst,
	})
	if err != nil {
		return nil, err
	}

	if h.audit != nil {
		h.audit.Record(ctx, auditlogsvc.Event{ //nolint:errcheck
			TenantID:   auth.TenantID,
			Actor:      auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, r),
			Action:     domain.AuditActionMCPClientUpdate,
			TargetType: "mcp_client",
			TargetID:   updated.ID.String(),
			Summary:    "Updated MCP client governance: " + updated.Name,
			Before:     toDTO(ptrext.Indirect(before)),
			After:      toDTO(ptrext.Indirect(updated)),
		})
	}

	return ptrext.Of(UpdateResponse{Client: toDTO(ptrext.Indirect(updated))}), nil
}

// ReplaceToolPolicies atomically replaces the explicit overrides for one client.
func (h *Handler) ReplaceToolPolicies(ctx context.Context, auth *session.AuthCtx, id string, req *ReplaceToolPoliciesRequest, r *http.Request) (*ReplaceToolPoliciesResponse, error) {
	clientID, err := uuid.Parse(id)
	if err != nil {
		return nil, ptrext.Of(ValidationError{Field: "id", Message: "invalid client ID"})
	}
	client, err := h.loadClientForTenant(ctx, auth.TenantID, clientID)
	if err != nil {
		return nil, err
	}
	if err := ensureClientMutable(client); err != nil {
		return nil, err
	}
	if req.Policies == nil {
		return nil, ptrext.Of(ValidationError{
			Field:   "policies",
			Message: "field is required; use [] to clear all tool policies",
		})
	}

	if err := validateToolPolicyInputs(client.Scopes, req.Policies); err != nil {
		return nil, err
	}

	before, err := h.listToolPolicies(ctx, client.ID)
	if err != nil {
		return nil, err
	}

	replacements := make([]mcprepo.UpsertToolPolicyParams, 0, len(req.Policies))
	for _, p := range req.Policies {
		meta, _ := tools.LookupTool(strings.TrimSpace(p.ToolName))
		replacements = append(replacements, mcprepo.UpsertToolPolicyParams{
			ClientID:       client.ID,
			ToolName:       meta.Name,
			Effect:         strings.TrimSpace(p.Effect),
			RateLimitRPM:   p.RateLimitRPM,
			RateLimitBurst: p.RateLimitBurst,
		})
	}

	if h.toolPolicies == nil {
		return nil, ptrext.Of(ValidationError{Field: "policies", Message: "tool policy store not configured"})
	}
	if err := h.toolPolicies.ReplaceByClient(ctx, client.ID, replacements); err != nil {
		return nil, err
	}

	after, err := h.listToolPolicies(ctx, client.ID)
	if err != nil {
		return nil, err
	}

	if h.audit != nil {
		h.audit.Record(ctx, auditlogsvc.Event{ //nolint:errcheck
			TenantID:   auth.TenantID,
			Actor:      auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, r),
			Action:     domain.AuditActionMCPClientToolPolicies,
			TargetType: "mcp_client",
			TargetID:   client.ID.String(),
			Summary:    "Updated MCP client tool policies: " + client.Name,
			Before:     buildClientToolPolicies(ptrext.Indirect(client), before),
			After:      buildClientToolPolicies(ptrext.Indirect(client), after),
		})
	}

	return ptrext.Of(ReplaceToolPoliciesResponse{
		Tools: buildClientToolPolicies(ptrext.Indirect(client), after),
	}), nil
}

// RevokeSession terminates one active session and revokes its grants.
func (h *Handler) RevokeSession(ctx context.Context, auth *session.AuthCtx, clientIDValue, sessionIDValue string, r *http.Request) (*RevokeSessionResponse, error) {
	clientID, err := uuid.Parse(clientIDValue)
	if err != nil {
		return nil, ptrext.Of(ValidationError{Field: "id", Message: "invalid client ID"})
	}
	sessionID, err := uuid.Parse(sessionIDValue)
	if err != nil {
		return nil, ptrext.Of(ValidationError{Field: "session_id", Message: "invalid session ID"})
	}

	client, err := h.loadClientForTenant(ctx, auth.TenantID, clientID)
	if err != nil {
		return nil, err
	}
	if h.sessions == nil {
		return nil, ptrext.Of(ValidationError{Field: "session_id", Message: "session store not configured"})
	}

	s, err := h.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if s.ClientID != client.ID {
		return nil, mcprepo.ErrSessionNotFound
	}

	before := toSessionDTO(ptrext.Indirect(s))

	if h.tokens != nil {
		_, _ = h.tokens.RevokeBySession(ctx, sessionID)
	}
	if err := h.sessions.CloseWithReason(ctx, sessionID, "admin_revoked", auth.UserID); err != nil {
		return nil, err
	}

	if h.audit != nil {
		after := before
		now := time.Now().UTC().Format(time.RFC3339)
		after.ClosedReason = "admin_revoked"
		after.ClosedBy = auth.UserID
		after.ClosedAt = ptrext.Of(now)
		h.audit.Record(ctx, auditlogsvc.Event{ //nolint:errcheck
			TenantID:   auth.TenantID,
			Actor:      auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, r),
			Action:     domain.AuditActionMCPSessionRevoke,
			TargetType: "mcp_session",
			TargetID:   sessionID.String(),
			Summary:    "Revoked MCP session for client: " + client.Name,
			Before:     before,
			After:      after,
		})
	}

	return ptrext.Of(RevokeSessionResponse{}), nil
}

// RevokeRefreshGrant revokes one active refresh grant without closing the session.
func (h *Handler) RevokeRefreshGrant(ctx context.Context, auth *session.AuthCtx, clientIDValue, grantIDValue string, r *http.Request) (*RevokeRefreshGrantResponse, error) {
	clientID, err := uuid.Parse(clientIDValue)
	if err != nil {
		return nil, ptrext.Of(ValidationError{Field: "id", Message: "invalid client ID"})
	}
	grantID, err := uuid.Parse(grantIDValue)
	if err != nil {
		return nil, ptrext.Of(ValidationError{Field: "grant_id", Message: "invalid grant ID"})
	}

	client, err := h.loadClientForTenant(ctx, auth.TenantID, clientID)
	if err != nil {
		return nil, err
	}
	if h.tokens == nil {
		return nil, ptrext.Of(ValidationError{Field: "grant_id", Message: "token store not configured"})
	}

	grants, err := h.tokens.ListByClient(ctx, client.ID)
	if err != nil {
		return nil, err
	}

	var target *mcprepo.RefreshToken
	for i := range grants {
		if grants[i].ID == grantID {
			target = &grants[i]
			break
		}
	}
	if target == nil {
		return nil, mcprepo.ErrRefreshTokenNotFound
	}

	before := toGrantDTO(ptrext.Indirect(target))
	if err := h.tokens.Revoke(ctx, grantID); err != nil {
		return nil, err
	}

	if h.audit != nil {
		h.audit.Record(ctx, auditlogsvc.Event{ //nolint:errcheck
			TenantID:   auth.TenantID,
			Actor:      auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, r),
			Action:     domain.AuditActionMCPRefreshGrantRevoke,
			TargetType: "mcp_refresh_grant",
			TargetID:   grantID.String(),
			Summary:    "Revoked MCP refresh grant for client: " + client.Name,
			Before:     before,
		})
	}

	return ptrext.Of(RevokeRefreshGrantResponse{}), nil
}

// ServeGet handles GET /mcp/clients/{id}.
func (h *Handler) ServeGet(w http.ResponseWriter, r *http.Request) {
	auth := session.FromContext(r.Context())

	resp, err := h.Get(r.Context(), auth, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ServeUpdate handles PATCH /mcp/clients/{id}.
func (h *Handler) ServeUpdate(w http.ResponseWriter, r *http.Request) {
	auth := session.FromContext(r.Context())

	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ptrext.Of(ValidationError{Field: "body", Message: "invalid JSON"}))
		return
	}

	resp, err := h.Update(r.Context(), auth, chi.URLParam(r, "id"), ptrext.Of(req), r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ServeReplaceToolPolicies handles PUT /mcp/clients/{id}/tool-policies.
func (h *Handler) ServeReplaceToolPolicies(w http.ResponseWriter, r *http.Request) {
	auth := session.FromContext(r.Context())

	var req ReplaceToolPoliciesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ptrext.Of(ValidationError{Field: "body", Message: "invalid JSON"}))
		return
	}

	resp, err := h.ReplaceToolPolicies(r.Context(), auth, chi.URLParam(r, "id"), ptrext.Of(req), r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ServeRevokeSession handles DELETE /mcp/clients/{id}/sessions/{session_id}.
func (h *Handler) ServeRevokeSession(w http.ResponseWriter, r *http.Request) {
	auth := session.FromContext(r.Context())

	_, err := h.RevokeSession(r.Context(), auth, chi.URLParam(r, "id"), chi.URLParam(r, "session_id"), r)
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ServeRevokeRefreshGrant handles DELETE /mcp/clients/{id}/grants/{grant_id}.
func (h *Handler) ServeRevokeRefreshGrant(w http.ResponseWriter, r *http.Request) {
	auth := session.FromContext(r.Context())

	_, err := h.RevokeRefreshGrant(r.Context(), auth, chi.URLParam(r, "id"), chi.URLParam(r, "grant_id"), r)
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listToolPolicies(ctx context.Context, clientID uuid.UUID) ([]mcprepo.ToolPolicy, error) {
	if h.toolPolicies == nil {
		return nil, nil
	}
	return h.toolPolicies.ListByClient(ctx, clientID)
}

func toSessionDTOs(in []mcprepo.Session) []SessionDTO {
	out := make([]SessionDTO, 0, len(in))
	for _, s := range in {
		out = append(out, toSessionDTO(s))
	}
	return out
}

func toSessionDTO(s mcprepo.Session) SessionDTO {
	dto := SessionDTO{
		ID:            s.ID.String(),
		Scopes:        s.Scopes,
		LastToolName:  s.LastToolName,
		LastDecision:  s.LastDecision,
		LastIP:        s.LastIP,
		LastUserAgent: s.LastUserAgent,
		LastActiveAt:  s.LastActiveAt.UTC().Format(time.RFC3339),
		CreatedAt:     s.CreatedAt.UTC().Format(time.RFC3339),
		ClosedReason:  s.ClosedReason,
		ClosedBy:      s.ClosedBy,
	}
	if s.ClosedAt != nil {
		closedAt := s.ClosedAt.UTC().Format(time.RFC3339)
		dto.ClosedAt = ptrext.Of(closedAt)
	}
	return dto
}

func toGrantDTOs(in []mcprepo.RefreshToken) []RefreshGrantDTO {
	out := make([]RefreshGrantDTO, 0, len(in))
	for _, g := range in {
		out = append(out, toGrantDTO(g))
	}
	return out
}

func toGrantDTO(g mcprepo.RefreshToken) RefreshGrantDTO {
	return RefreshGrantDTO{
		ID:        g.ID.String(),
		SessionID: g.SessionID.String(),
		Scopes:    g.Scopes,
		ExpiresAt: g.ExpiresAt.UTC().Format(time.RFC3339),
		CreatedAt: g.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func buildClientToolPolicies(client mcprepo.Client, explicit []mcprepo.ToolPolicy) []ClientToolPolicy {
	overrides := make(map[string]mcprepo.ToolPolicy, len(explicit))
	for _, p := range explicit {
		overrides[p.ToolName] = p
	}

	catalog := tools.ListTools()
	out := make([]ClientToolPolicy, 0, len(catalog))
	for _, meta := range catalog {
		override, hasOverride := overrides[meta.Name]
		var overridePtr *mcprepo.ToolPolicy
		if hasOverride {
			overridePtr = ptrext.Of(override)
		}
		out = append(out, buildClientToolPolicy(client, meta, overridePtr, hasOverride))
	}
	return out
}

func buildClientToolPolicy(client mcprepo.Client, meta tools.ToolMeta, override *mcprepo.ToolPolicy, hasOverride bool) ClientToolPolicy {
	scopeGranted := scopeCovers(client.Scopes, meta.RequiredScope)
	effectiveAllowed := false
	overrideEffect := ""
	if override != nil {
		overrideEffect = override.Effect
	}
	if meta.EnabledByDefault && scopeGranted {
		effectiveAllowed = toolEffectiveAllowed(client.ToolPolicyMode, hasOverride, overrideEffect)
	}

	dto := ClientToolPolicy{
		Name:             meta.Name,
		Kind:             string(meta.Kind),
		Owner:            meta.Owner,
		EnabledByDefault: meta.EnabledByDefault,
		Deprecated:       meta.Deprecated,
		Replacement:      meta.Replacement,
		Aliases:          toClientToolAliases(meta.Aliases),
		Provenance:       toClientToolProvenance(meta.Provenance),
		RequiredScope:    meta.RequiredScope,
		Risk:             string(meta.Risk),
		DataClass:        string(meta.DataClass),
		ReadOnlyHint:     meta.ReadOnlyHint,
		DestructiveHint:  meta.DestructiveHint,
		OpenWorldHint:    meta.OpenWorldHint,
		DefaultRPM:       meta.DefaultRPM,
		DefaultBurst:     meta.DefaultBurst,
		ScopeGranted:     scopeGranted,
		EffectiveAllowed: effectiveAllowed,
	}
	if override != nil {
		dto.Effect = override.Effect
		dto.RateLimitRPM = override.RateLimitRPM
		dto.RateLimitBurst = override.RateLimitBurst
	}
	return dto
}

func validateToolPolicyMode(mode string) error {
	switch strings.TrimSpace(mode) {
	case domain.MCPToolPolicyModeLegacyAllowAll, domain.MCPToolPolicyModeAllowList:
		return nil
	default:
		return ptrext.Of(ValidationError{Field: "tool_policy_mode", Message: "invalid tool policy mode"})
	}
}

func validateOptionalToolPolicyMode(mode *string, set bool) error {
	if !set {
		return nil
	}
	if mode == nil {
		return ptrext.Of(ValidationError{Field: "tool_policy_mode", Message: "invalid tool policy mode"})
	}
	return validateToolPolicyMode(ptrext.Indirect(mode))
}

func validateOptionalPositiveInt(field string, v *int) error {
	if ptrext.IndirectOr(v, 0) <= 0 && v != nil {
		return ptrext.Of(ValidationError{Field: field, Message: "must be greater than zero"})
	}
	return nil
}

func validateToolPolicyInputs(clientScopes []string, policies []ToolPolicyInput) error {
	seen := make(map[string]struct{}, len(policies))
	for _, p := range policies {
		name := strings.TrimSpace(p.ToolName)
		if name == "" {
			return ptrext.Of(ValidationError{Field: "policies", Message: "tool_name is required"})
		}

		meta, ok := tools.LookupTool(name)
		if !ok {
			return ptrext.Of(ValidationError{Field: "policies", Message: "unknown tool: " + name})
		}
		canonicalName := meta.Name
		if _, dup := seen[canonicalName]; dup {
			return ptrext.Of(ValidationError{Field: "policies", Message: "duplicate tool_name: " + canonicalName})
		}
		seen[canonicalName] = struct{}{}

		if !scopeCovers(clientScopes, meta.RequiredScope) {
			return ptrext.Of(ValidationError{Field: "policies", Message: "tool scope not granted: " + canonicalName})
		}
		if !meta.EnabledByDefault {
			return ptrext.Of(ValidationError{Field: "policies", Message: "tool is disabled by catalog: " + canonicalName})
		}

		effect := strings.TrimSpace(p.Effect)
		if effect != domain.MCPToolPolicyEffectAllow && effect != domain.MCPToolPolicyEffectDeny {
			return ptrext.Of(ValidationError{Field: "policies", Message: "invalid effect for tool: " + canonicalName})
		}
		if err := validateOptionalPositiveInt("policies.rate_limit_rpm", p.RateLimitRPM); err != nil {
			return err
		}
		if err := validateOptionalPositiveInt("policies.rate_limit_burst", p.RateLimitBurst); err != nil {
			return err
		}
	}
	return nil
}

func toClientToolAliases(in []tools.ToolAlias) []ClientToolAlias {
	if len(in) == 0 {
		return nil
	}
	out := make([]ClientToolAlias, 0, len(in))
	for _, alias := range in {
		out = append(out, ClientToolAlias{
			Name:        alias.Name,
			Deprecated:  alias.Deprecated,
			Replacement: alias.Replacement,
		})
	}
	return out
}

func toClientToolProvenance(in *tools.ExtensionProvenance) *ClientToolProvenance {
	if in == nil {
		return nil
	}
	return ptrext.Of(ClientToolProvenance{
		Kind:      in.Kind,
		Reference: in.Reference,
	})
}

func scopeCovers(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
		if scope == domain.MCPScopeWrite && required == domain.MCPScopeRead {
			return true
		}
	}
	return false
}

func toolEffectiveAllowed(mode string, hasOverride bool, effect string) bool {
	if effect == domain.MCPToolPolicyEffectDeny {
		return false
	}
	if strings.TrimSpace(mode) == domain.MCPToolPolicyModeAllowList {
		return hasOverride && effect == domain.MCPToolPolicyEffectAllow
	}
	return true
}
