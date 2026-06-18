// ptrext:file-allow proto request/response conversions use raw address-of for optional fields.
package apikey

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

// GetPolicy returns the org-level API key policy.
func (h *APIKeysHandler) GetPolicy(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.GetPolicyRequest) (dispatcher.Result[*attunev1.GetPolicyResponse], error) {
	const where = "handlers.console.apikey.GetPolicy"
	auth := ctx.Auth

	policy, err := h.svc.GetPolicy(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] get policy failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetPolicyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get policy")
	}

	return dispatcher.OK(ptrext.Of(attunev1.GetPolicyResponse{
		Policy: toProtoPolicy(policy),
	}))
}

// UpdatePolicy updates the org-level API key policy.
func (h *APIKeysHandler) UpdatePolicy(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdatePolicyRequest) (dispatcher.Result[*attunev1.UpdatePolicyResponse], error) {
	const where = "handlers.console.apikey.UpdatePolicy"
	auth := ctx.Auth

	p := req.GetPolicy()
	if p == nil {
		return dispatcher.Fail[*attunev1.UpdatePolicyResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "policy is required")
	}

	policy := apikeyrepo.Policy{
		TenantID:               auth.TenantID,
		RequireExpiry:          p.GetRequireExpiry(),
		RequireIPAllowlist:     p.GetRequireIpAllowlist(),
		RequireDescription:     p.GetRequireDescription(),
		AllowedEnvironments:    p.GetAllowedEnvironments(),
		RequireMFAForCreate:    p.GetRequireMfaForCreate(),
		RequireApprovalForProd: p.GetRequireApprovalForProd(),
	}
	if p.MaxExpiryDays != nil {
		v := int(ptrext.Indirect(p.MaxExpiryDays))
		policy.MaxExpiryDays = &v
	}
	if p.MaxKeysPerServiceAccount != nil {
		v := int(ptrext.Indirect(p.MaxKeysPerServiceAccount))
		policy.MaxKeysPerServiceAccount = &v
	}
	if p.AutoRevokeUnusedDays != nil {
		v := int(ptrext.Indirect(p.AutoRevokeUnusedDays))
		policy.AutoRevokeUnusedDays = &v
	}

	if err := h.svc.UpsertPolicy(ctx, policy); err != nil {
		logext.Errorf(ctx, "[%s] upsert policy failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.UpdatePolicyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to update policy")
	}

	return dispatcher.OK(ptrext.Of(attunev1.UpdatePolicyResponse{
		Policy: toProtoPolicy(&policy),
	}))
}

// ListProjects returns all projects for the tenant.
func (h *APIKeysHandler) ListProjects(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListProjectsRequest) (dispatcher.Result[*attunev1.ListProjectsResponse], error) {
	const where = "handlers.console.apikey.ListProjects"
	auth := ctx.Auth

	projects, err := h.svc.ListProjects(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list projects failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListProjectsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list projects")
	}

	items := make([]*attunev1.Project, 0, len(projects))
	for _, p := range projects {
		items = append(items, ptrext.Of(attunev1.Project{
			Id:          p.ID.String(),
			Name:        p.Name,
			Description: ptrext.Of(p.Description),
			IsActive:    p.IsActive,
			CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:   p.UpdatedAt.UTC().Format(time.RFC3339),
		}))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListProjectsResponse{Items: items}))
}

// CreateProject creates a new project.
func (h *APIKeysHandler) CreateProject(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateProjectRequest) (dispatcher.Result[*attunev1.CreateProjectResponse], error) {
	const where = "handlers.console.apikey.CreateProject"
	auth := ctx.Auth

	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return dispatcher.Fail[*attunev1.CreateProjectResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_LABEL, "name is required")
	}

	description := strings.TrimSpace(ptrext.Indirect(req.Description))

	project, err := h.svc.CreateProject(ctx, auth.TenantID, name, description)
	if err != nil {
		logext.Errorf(ctx, "[%s] create project failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateProjectResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create project")
	}

	return dispatcher.Created(ptrext.Of(attunev1.CreateProjectResponse{
		Project: ptrext.Of(attunev1.Project{
			Id:          project.ID.String(),
			Name:        project.Name,
			Description: ptrext.Of(project.Description),
			IsActive:    project.IsActive,
			CreatedAt:   project.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:   project.UpdatedAt.UTC().Format(time.RFC3339),
		}),
	}))
}

// BindKeyToProject binds an API key to a project.
func (h *APIKeysHandler) BindKeyToProject(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.BindKeyToProjectRequest) (dispatcher.Result[*attunev1.BindKeyToProjectResponse], error) {
	const where = "handlers.console.apikey.BindKeyToProject"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.BindKeyToProjectResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	projectID, err := uuid.Parse(req.GetProjectId())
	if err != nil {
		return dispatcher.Fail[*attunev1.BindKeyToProjectResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid project id")
	}

	if err := h.svc.BindKeyToProject(ctx, auth.TenantID, keyID, projectID); err != nil {
		if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
			return dispatcher.Fail[*attunev1.BindKeyToProjectResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "key not found")
		}
		logext.Errorf(ctx, "[%s] bind key to project failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.BindKeyToProjectResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to bind key to project")
	}

	row, _ := h.svc.GetByID(ctx, auth.TenantID, keyID)
	scopes, _ := h.svc.GetScopes(ctx, keyID)

	return dispatcher.OK(ptrext.Of(attunev1.BindKeyToProjectResponse{
		Key: toProtoAPIKeyFromRow(row, scopes),
	}))
}

// GetKeyTags returns tags for an API key.
func (h *APIKeysHandler) GetKeyTags(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetKeyTagsRequest) (dispatcher.Result[*attunev1.GetKeyTagsResponse], error) {
	const where = "handlers.console.apikey.GetKeyTags"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.GetKeyTagsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	tags, err := h.svc.GetKeyTags(ctx, auth.TenantID, keyID)
	if err != nil {
		logext.Errorf(ctx, "[%s] get key tags failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.GetKeyTagsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get tags")
	}

	items := make([]*attunev1.ApiKeyTag, 0, len(tags))
	for _, t := range tags {
		items = append(items, ptrext.Of(attunev1.ApiKeyTag{
			Key:   t.Key,
			Value: t.Value,
		}))
	}

	return dispatcher.OK(ptrext.Of(attunev1.GetKeyTagsResponse{Tags: items}))
}

// SetKeyTags sets tags for an API key.
func (h *APIKeysHandler) SetKeyTags(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.SetKeyTagsRequest) (dispatcher.Result[*attunev1.SetKeyTagsResponse], error) {
	const where = "handlers.console.apikey.SetKeyTags"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SetKeyTagsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	tags := make([]apikeyrepo.Tag, 0, len(req.GetTags()))
	for _, t := range req.GetTags() {
		tags = append(tags, apikeyrepo.Tag{
			Key:   strings.TrimSpace(t.GetKey()),
			Value: strings.TrimSpace(t.GetValue()),
		})
	}

	if err := h.svc.SetKeyTags(ctx, auth.TenantID, keyID, tags); err != nil {
		logext.Errorf(ctx, "[%s] set key tags failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.SetKeyTagsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to set tags")
	}

	items := make([]*attunev1.ApiKeyTag, 0, len(tags))
	for _, t := range tags {
		items = append(items, ptrext.Of(attunev1.ApiKeyTag{
			Key:   t.Key,
			Value: t.Value,
		}))
	}

	return dispatcher.OK(ptrext.Of(attunev1.SetKeyTagsResponse{Tags: items}))
}

// SetKeyBudget sets budget for an API key.
func (h *APIKeysHandler) SetKeyBudget(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.SetKeyBudgetRequest) (dispatcher.Result[*attunev1.SetKeyBudgetResponse], error) {
	const where = "handlers.console.apikey.SetKeyBudget"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SetKeyBudgetResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	budgetStr := strings.TrimSpace(req.GetBudgetLimitUsd())
	if budgetStr == "" {
		return dispatcher.Fail[*attunev1.SetKeyBudgetResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "budget_limit_usd is required")
	}

	budget, err := strconv.ParseFloat(budgetStr, 64)
	if err != nil || budget < 0 {
		return dispatcher.Fail[*attunev1.SetKeyBudgetResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid budget_limit_usd")
	}

	overageAction := strings.TrimSpace(ptrext.Indirect(req.OverageAction))
	if overageAction == "" {
		overageAction = "alert"
	}

	if err := h.svc.SetKeyBudget(ctx, auth.TenantID, keyID, budget, overageAction); err != nil {
		if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
			return dispatcher.Fail[*attunev1.SetKeyBudgetResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "key not found")
		}
		logext.Errorf(ctx, "[%s] set key budget failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.SetKeyBudgetResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to set budget")
	}

	row, _ := h.svc.GetByID(ctx, auth.TenantID, keyID)
	scopes, _ := h.svc.GetScopes(ctx, keyID)

	return dispatcher.OK(ptrext.Of(attunev1.SetKeyBudgetResponse{
		Key: toProtoAPIKeyFromRow(row, scopes),
	}))
}

// CreateTempToken creates a temporary token from a parent key.
func (h *APIKeysHandler) CreateTempToken(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateTempTokenRequest) (dispatcher.Result[*attunev1.CreateTempTokenResponse], error) {
	const where = "handlers.console.apikey.CreateTempToken"
	auth := ctx.Auth

	parentKeyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateTempTokenResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	expiresIn := strings.TrimSpace(req.GetExpiresIn())
	if expiresIn == "" {
		expiresIn = "1h"
	}
	expiresInDur, err := parseGracePeriod(expiresIn)
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateTempTokenResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid expires_in")
	}

	var maxUses *int
	if req.MaxUses != nil {
		v := int(ptrext.Indirect(req.MaxUses))
		maxUses = &v
	}

	purpose := strings.TrimSpace(ptrext.Indirect(req.Purpose))
	if purpose == "" {
		purpose = "one_time"
	}

	token, err := h.svc.CreateTempToken(ctx, auth.TenantID, parentKeyID, expiresInDur, maxUses, purpose)
	if err != nil {
		if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
			return dispatcher.Fail[*attunev1.CreateTempTokenResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "parent key not found")
		}
		logext.Errorf(ctx, "[%s] create temp token failed,tenant_id:%s,parent_key_id:%s,err:%+v", where, auth.TenantID, parentKeyID, err.Error())
		return dispatcher.Fail[*attunev1.CreateTempTokenResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create temp token")
	}

	resp := ptrext.Of(attunev1.CreateTempTokenResponse{
		Token:       token.Token,
		TokenPrefix: token.TokenPrefix,
		ExpiresAt:   token.ExpiresAt.UTC().Format(time.RFC3339),
	})
	if maxUses != nil {
		resp.MaxUses = ptrext.Of(int32(*maxUses))
	}

	return dispatcher.Created(resp)
}

// ListApprovalRequests returns approval requests.
func (h *APIKeysHandler) ListApprovalRequests(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListApprovalRequestsRequest) (dispatcher.Result[*attunev1.ListApprovalRequestsResponse], error) {
	const where = "handlers.console.apikey.ListApprovalRequests"
	auth := ctx.Auth

	status := strings.TrimSpace(ptrext.Indirect(req.Status))

	requests, err := h.svc.ListApprovalRequests(ctx, auth.TenantID, status)
	if err != nil {
		logext.Errorf(ctx, "[%s] list approval requests failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListApprovalRequestsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list approval requests")
	}

	items := make([]*attunev1.ApprovalRequest, 0, len(requests))
	for _, r := range requests {
		items = append(items, toProtoApprovalRequest(&r))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListApprovalRequestsResponse{Items: items}))
}

// CreateApprovalRequest creates a new approval request.
func (h *APIKeysHandler) CreateApprovalRequest(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateApprovalRequestRequest) (dispatcher.Result[*attunev1.CreateApprovalRequestResponse], error) {
	const where = "handlers.console.apikey.CreateApprovalRequest"
	auth := ctx.Auth

	label := strings.TrimSpace(req.GetKeyLabel())
	if label == "" {
		return dispatcher.Fail[*attunev1.CreateApprovalRequestResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_LABEL, "key_label is required")
	}

	params := apikeyrepo.ApprovalRequest{
		TenantID:             auth.TenantID,
		RequesterID:          auth.UserID,
		RequesterType:        "admin",
		KeyLabel:             label,
		KeyDescription:       ptrext.Indirect(req.KeyDescription),
		RequestedScopes:      req.GetRequestedScopes(),
		RequestedEnvironment: ptrext.Indirect(req.RequestedEnvironment),
		Justification:        ptrext.Indirect(req.Justification),
		Status:               "pending",
	}
	if req.RequestedExpiryDays != nil {
		v := int(ptrext.Indirect(req.RequestedExpiryDays))
		params.RequestedExpiryDays = &v
	}

	created, err := h.svc.CreateApprovalRequest(ctx, params)
	if err != nil {
		logext.Errorf(ctx, "[%s] create approval request failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateApprovalRequestResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create approval request")
	}

	return dispatcher.Created(ptrext.Of(attunev1.CreateApprovalRequestResponse{
		Request: toProtoApprovalRequest(created),
	}))
}

// ReviewApproval reviews an approval request.
func (h *APIKeysHandler) ReviewApproval(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ReviewApprovalRequest) (dispatcher.Result[*attunev1.ReviewApprovalResponse], error) {
	const where = "handlers.console.apikey.ReviewApproval"
	auth := ctx.Auth

	requestID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ReviewApprovalResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid request id")
	}

	reviewed, err := h.svc.ReviewApproval(ctx, auth.TenantID, requestID, auth.UserID, req.GetApprove(), ptrext.Indirect(req.Notes))
	if err != nil {
		logext.Errorf(ctx, "[%s] review approval failed,tenant_id:%s,request_id:%s,err:%+v", where, auth.TenantID, requestID, err.Error())
		return dispatcher.Fail[*attunev1.ReviewApprovalResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to review approval")
	}

	return dispatcher.OK(ptrext.Of(attunev1.ReviewApprovalResponse{
		Request: toProtoApprovalRequest(reviewed),
	}))
}

// ListOAuth2Clients returns OAuth2 clients.
func (h *APIKeysHandler) ListOAuth2Clients(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListOAuth2ClientsRequest) (dispatcher.Result[*attunev1.ListOAuth2ClientsResponse], error) {
	const where = "handlers.console.apikey.ListOAuth2Clients"
	auth := ctx.Auth

	clients, err := h.svc.ListOAuth2Clients(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list oauth2 clients failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListOAuth2ClientsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list oauth2 clients")
	}

	items := make([]*attunev1.OAuth2Client, 0, len(clients))
	for _, c := range clients {
		items = append(items, ptrext.Of(attunev1.OAuth2Client{
			Id:            c.ID.String(),
			ClientId:      c.ClientID,
			Name:          c.Name,
			Description:   ptrext.Of(c.Description),
			RedirectUris:  c.RedirectURIs,
			AllowedScopes: c.AllowedScopes,
			IsActive:      c.IsActive,
			CreatedAt:     c.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:     c.UpdatedAt.UTC().Format(time.RFC3339),
		}))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListOAuth2ClientsResponse{Items: items}))
}

// CreateOAuth2Client creates a new OAuth2 client.
func (h *APIKeysHandler) CreateOAuth2Client(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateOAuth2ClientRequest) (dispatcher.Result[*attunev1.CreateOAuth2ClientResponse], error) {
	const where = "handlers.console.apikey.CreateOAuth2Client"
	auth := ctx.Auth

	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return dispatcher.Fail[*attunev1.CreateOAuth2ClientResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_LABEL, "name is required")
	}

	client, secret, err := h.svc.CreateOAuth2Client(ctx, auth.TenantID, name, ptrext.Indirect(req.Description), req.GetRedirectUris(), req.GetAllowedScopes())
	if err != nil {
		logext.Errorf(ctx, "[%s] create oauth2 client failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateOAuth2ClientResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create oauth2 client")
	}

	return dispatcher.Created(ptrext.Of(attunev1.CreateOAuth2ClientResponse{
		Client: ptrext.Of(attunev1.OAuth2Client{
			Id:            client.ID.String(),
			ClientId:      client.ClientID,
			Name:          client.Name,
			Description:   ptrext.Of(client.Description),
			RedirectUris:  client.RedirectURIs,
			AllowedScopes: client.AllowedScopes,
			IsActive:      client.IsActive,
			CreatedAt:     client.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:     client.UpdatedAt.UTC().Format(time.RFC3339),
		}),
		ClientSecret: secret,
	}))
}

// GetKeyAnalytics returns analytics for an API key.
func (h *APIKeysHandler) GetKeyAnalytics(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetKeyAnalyticsRequest) (dispatcher.Result[*attunev1.GetKeyAnalyticsResponse], error) {
	const where = "handlers.console.apikey.GetKeyAnalytics"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.GetKeyAnalyticsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	start := time.Now().Add(-24 * time.Hour)
	if s := ptrext.Indirect(req.Start); s != "" {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			start = parsed
		}
	}

	end := time.Now()
	if e := ptrext.Indirect(req.End); e != "" {
		if parsed, err := time.Parse(time.RFC3339, e); err == nil {
			end = parsed
		}
	}

	analytics, err := h.svc.GetKeyAnalytics(ctx, auth.TenantID, keyID, start, end)
	if err != nil {
		logext.Errorf(ctx, "[%s] get key analytics failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.GetKeyAnalyticsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get analytics")
	}

	items := make([]*attunev1.ApiKeyAnalytics, 0, len(analytics))
	for _, a := range analytics {
		items = append(items, toProtoAnalytics(&a))
	}

	return dispatcher.OK(ptrext.Of(attunev1.GetKeyAnalyticsResponse{Items: items}))
}

// GetTenantAnalytics returns analytics for all keys in the tenant.
func (h *APIKeysHandler) GetTenantAnalytics(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetTenantAnalyticsRequest) (dispatcher.Result[*attunev1.GetTenantAnalyticsResponse], error) {
	const where = "handlers.console.apikey.GetTenantAnalytics"
	auth := ctx.Auth

	start := time.Now().Add(-24 * time.Hour)
	if s := ptrext.Indirect(req.Start); s != "" {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			start = parsed
		}
	}

	end := time.Now()
	if e := ptrext.Indirect(req.End); e != "" {
		if parsed, err := time.Parse(time.RFC3339, e); err == nil {
			end = parsed
		}
	}

	analytics, err := h.svc.GetTenantAnalytics(ctx, auth.TenantID, start, end)
	if err != nil {
		logext.Errorf(ctx, "[%s] get tenant analytics failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetTenantAnalyticsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get analytics")
	}

	items := make([]*attunev1.ApiKeyAnalytics, 0, len(analytics))
	for _, a := range analytics {
		items = append(items, toProtoAnalytics(&a))
	}

	return dispatcher.OK(ptrext.Of(attunev1.GetTenantAnalyticsResponse{Items: items}))
}

// ListSecretManagers returns secret manager configs.
func (h *APIKeysHandler) ListSecretManagers(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListSecretManagersRequest) (dispatcher.Result[*attunev1.ListSecretManagersResponse], error) {
	const where = "handlers.console.apikey.ListSecretManagers"
	auth := ctx.Auth

	configs, err := h.svc.ListSecretManagers(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list secret managers failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListSecretManagersResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list secret managers")
	}

	items := make([]*attunev1.SecretManagerConfig, 0, len(configs))
	for _, c := range configs {
		item := ptrext.Of(attunev1.SecretManagerConfig{
			Id:          c.ID.String(),
			ManagerType: string(c.ManagerType),
			Name:        c.Name,
			IsActive:    c.IsActive,
			CreatedAt:   c.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:   c.UpdatedAt.UTC().Format(time.RFC3339),
		})
		if c.LastSyncAt != nil {
			item.LastSyncAt = ptrext.Of(c.LastSyncAt.UTC().Format(time.RFC3339))
		}
		if c.LastSyncStatus != "" {
			item.LastSyncStatus = ptrext.Of(c.LastSyncStatus)
		}
		items = append(items, item)
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListSecretManagersResponse{Items: items}))
}

// CreateSecretManager creates a new secret manager config.
func (h *APIKeysHandler) CreateSecretManager(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateSecretManagerRequest) (dispatcher.Result[*attunev1.CreateSecretManagerResponse], error) {
	const where = "handlers.console.apikey.CreateSecretManager"
	auth := ctx.Auth

	managerType := strings.TrimSpace(req.GetManagerType())
	if managerType == "" {
		return dispatcher.Fail[*attunev1.CreateSecretManagerResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "manager_type is required")
	}

	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return dispatcher.Fail[*attunev1.CreateSecretManagerResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_LABEL, "name is required")
	}

	config, err := h.svc.CreateSecretManager(ctx, auth.TenantID, managerType, name, req.GetConfig())
	if err != nil {
		logext.Errorf(ctx, "[%s] create secret manager failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateSecretManagerResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create secret manager")
	}

	return dispatcher.Created(ptrext.Of(attunev1.CreateSecretManagerResponse{
		Config: ptrext.Of(attunev1.SecretManagerConfig{
			Id:          config.ID.String(),
			ManagerType: string(config.ManagerType),
			Name:        config.Name,
			IsActive:    config.IsActive,
			CreatedAt:   config.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:   config.UpdatedAt.UTC().Format(time.RFC3339),
		}),
	}))
}

func toProtoPolicy(p *apikeyrepo.Policy) *attunev1.ApiKeyPolicy {
	if p == nil {
		return nil
	}
	policy := ptrext.Of(attunev1.ApiKeyPolicy{
		RequireExpiry:          p.RequireExpiry,
		RequireIpAllowlist:     p.RequireIPAllowlist,
		RequireDescription:     p.RequireDescription,
		AllowedEnvironments:    p.AllowedEnvironments,
		RequireMfaForCreate:    p.RequireMFAForCreate,
		RequireApprovalForProd: p.RequireApprovalForProd,
	})
	if p.MaxExpiryDays != nil {
		policy.MaxExpiryDays = ptrext.Of(int32(*p.MaxExpiryDays))
	}
	if p.MaxKeysPerServiceAccount != nil {
		policy.MaxKeysPerServiceAccount = ptrext.Of(int32(*p.MaxKeysPerServiceAccount))
	}
	if p.AutoRevokeUnusedDays != nil {
		policy.AutoRevokeUnusedDays = ptrext.Of(int32(*p.AutoRevokeUnusedDays))
	}
	return policy
}

func toProtoApprovalRequest(r *apikeyrepo.ApprovalRequest) *attunev1.ApprovalRequest {
	if r == nil {
		return nil
	}
	req := ptrext.Of(attunev1.ApprovalRequest{
		Id:                   r.ID.String(),
		RequesterId:          r.RequesterID,
		RequesterType:        r.RequesterType,
		KeyLabel:             r.KeyLabel,
		KeyDescription:       ptrext.Of(r.KeyDescription),
		RequestedScopes:      r.RequestedScopes,
		RequestedEnvironment: ptrext.Of(r.RequestedEnvironment),
		Justification:        ptrext.Of(r.Justification),
		Status:               string(r.Status),
		CreatedAt:            r.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:            r.ExpiresAt.UTC().Format(time.RFC3339),
	})
	if r.RequestedExpiryDays != nil {
		req.RequestedExpiryDays = ptrext.Of(int32(*r.RequestedExpiryDays))
	}
	if r.ReviewerID != "" {
		req.ReviewerId = ptrext.Of(r.ReviewerID)
	}
	if r.ReviewerNotes != "" {
		req.ReviewerNotes = ptrext.Of(r.ReviewerNotes)
	}
	if r.ReviewedAt != nil {
		req.ReviewedAt = ptrext.Of(r.ReviewedAt.UTC().Format(time.RFC3339))
	}
	return req
}

func toProtoAnalytics(a *apikeyrepo.AnalyticsHourly) *attunev1.ApiKeyAnalytics {
	if a == nil {
		return nil
	}
	avgLatency := int64(0)
	if a.RequestCount > 0 {
		avgLatency = a.TotalLatencyMs / a.RequestCount
	}
	return ptrext.Of(attunev1.ApiKeyAnalytics{
		Hour:         a.Hour.UTC().Format(time.RFC3339),
		RequestCount: a.RequestCount,
		ErrorCount:   a.ErrorCount,
		AvgLatencyMs: avgLatency,
		Status_2Xx:   int32(a.Status2xx),
		Status_4Xx:   int32(a.Status4xx),
		Status_5Xx:   int32(a.Status5xx),
	})
}
