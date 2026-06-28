package console

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consolemcpclient "github.com/Phixsura/attune/internal/handlers/console/mcpclient"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
)

func mountAPIKeyMCPClients(g chi.Router, clients *consolemcpclient.Handler, idem func(http.Handler) http.Handler) {
	g.Route("/mcp/clients", func(c chi.Router) {
		c.Use(apikey.RequireExplicitScope(domain.ScopeMCPClientAdmin))
		c.Get("/", dispatcher.Bind(
			"apikey.mcpclient.List",
			dispatcher.Empty(func() *attunev1.ListMCPClientsRequest {
				return ptrext.Of(attunev1.ListMCPClientsRequest{})
			}),
			func(
				ctx *dispatcher.RequestContext[*session.AuthCtx],
				_ *attunev1.ListMCPClientsRequest,
			) (dispatcher.Result[*attunev1.ListMCPClientsResponse], error) {
				resp, err := clients.List(ctx, ctx.Auth, ptrext.Of(consolemcpclient.ListRequest{}))
				if err != nil {
					return dispatcher.Result[*attunev1.ListMCPClientsResponse]{}, mapAPIKeyMCPClientError(err)
				}
				return dispatcher.OK(ptrext.Of(attunev1.ListMCPClientsResponse{
					Clients: mcpClientListProto(resp.Clients),
				}))
			},
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListMCPClientsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		c.With(idem).Post("/", dispatcher.Bind(
			"apikey.mcpclient.Create",
			dispatcher.JSON(func() *attunev1.CreateMCPClientRequest {
				return ptrext.Of(attunev1.CreateMCPClientRequest{})
			}),
			func(
				ctx *dispatcher.RequestContext[*session.AuthCtx],
				req *attunev1.CreateMCPClientRequest,
			) (dispatcher.Result[*attunev1.CreateMCPClientResponse], error) {
				resp, err := clients.Create(ctx, ctx.Auth, ptrext.Of(consolemcpclient.CreateRequest{
					Name:         req.GetName(),
					RedirectURIs: append([]string(nil), req.GetRedirectUris()...),
					Scopes:       append([]string(nil), req.GetScopes()...),
				}), ctx.Request())
				if err != nil {
					return dispatcher.Result[*attunev1.CreateMCPClientResponse]{}, mapAPIKeyMCPClientError(err)
				}
				return dispatcher.Created(ptrext.Of(attunev1.CreateMCPClientResponse{
					Client: mcpClientProto(resp.Client),
				}))
			},
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateMCPClientRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		c.Get("/{id}", dispatcher.Bind(
			"apikey.mcpclient.Get",
			dispatcher.Path(
				func() *attunev1.GetMCPClientRequest { return ptrext.Of(attunev1.GetMCPClientRequest{}) },
				dispatcher.Param("id", func(req *attunev1.GetMCPClientRequest, id string) { req.Id = id }),
			),
			func(
				ctx *dispatcher.RequestContext[*session.AuthCtx],
				req *attunev1.GetMCPClientRequest,
			) (dispatcher.Result[*attunev1.GetMCPClientResponse], error) {
				resp, err := clients.Get(ctx, ctx.Auth, req.GetId())
				if err != nil {
					return dispatcher.Result[*attunev1.GetMCPClientResponse]{}, mapAPIKeyMCPClientError(err)
				}
				return dispatcher.OK(ptrext.Of(attunev1.GetMCPClientResponse{
					Client:        mcpClientProto(resp.Client),
					Sessions:      mcpSessionListProto(resp.Sessions),
					RefreshGrants: mcpRefreshGrantListProto(resp.RefreshGrants),
					Tools:         mcpToolPolicyListProto(resp.Tools),
					Connection:    mcpConnectionProfileProto(resp.Connection),
				}))
			},
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetMCPClientRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		c.Delete("/{id}", dispatcher.Bind(
			"apikey.mcpclient.Revoke",
			dispatcher.Path(
				func() *attunev1.RevokeMCPClientRequest { return ptrext.Of(attunev1.RevokeMCPClientRequest{}) },
				dispatcher.Param("id", func(req *attunev1.RevokeMCPClientRequest, id string) { req.Id = id }),
			),
			func(
				ctx *dispatcher.RequestContext[*session.AuthCtx],
				req *attunev1.RevokeMCPClientRequest,
			) (dispatcher.Result[*attunev1.RevokeMCPClientResponse], error) {
				_, err := clients.Revoke(ctx, ctx.Auth, ptrext.Of(consolemcpclient.RevokeRequest{
					ID: req.GetId(),
				}), ctx.Request())
				if err != nil {
					return dispatcher.Result[*attunev1.RevokeMCPClientResponse]{}, mapAPIKeyMCPClientError(err)
				}
				return dispatcher.NoContent[*attunev1.RevokeMCPClientResponse]()
			},
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RevokeMCPClientRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		c.Patch("/{id}", dispatcher.Bind(
			"apikey.mcpclient.Update",
			dispatcher.Combine(
				func() *attunev1.UpdateMCPClientRequest { return ptrext.Of(attunev1.UpdateMCPClientRequest{}) },
				dispatcher.JSONBody[*attunev1.UpdateMCPClientRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateMCPClientRequest, id string) { req.Id = id }),
			),
			func(
				ctx *dispatcher.RequestContext[*session.AuthCtx],
				req *attunev1.UpdateMCPClientRequest,
			) (dispatcher.Result[*attunev1.UpdateMCPClientResponse], error) {
				resp, err := clients.Update(ctx, ctx.Auth, req.GetId(), ptrext.Of(consolemcpclient.UpdateRequest{
					ToolPolicyMode: req.ToolPolicyMode,
					RateLimitRPM:   int32ToInt(req.RateLimitRpm),
					RateLimitBurst: int32ToInt(req.RateLimitBurst),
				}), ctx.Request())
				if err != nil {
					return dispatcher.Result[*attunev1.UpdateMCPClientResponse]{}, mapAPIKeyMCPClientError(err)
				}
				return dispatcher.OK(ptrext.Of(attunev1.UpdateMCPClientResponse{
					Client: mcpClientProto(resp.Client),
				}))
			},
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateMCPClientRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		c.Put("/{id}/tool-policies", dispatcher.Bind(
			"apikey.mcpclient.ReplaceToolPolicies",
			dispatcher.Combine(
				func() *attunev1.ReplaceMCPClientToolPoliciesRequest {
					return ptrext.Of(attunev1.ReplaceMCPClientToolPoliciesRequest{})
				},
				dispatcher.JSONBody[*attunev1.ReplaceMCPClientToolPoliciesRequest],
				dispatcher.Param("id", func(req *attunev1.ReplaceMCPClientToolPoliciesRequest, id string) { req.Id = id }),
			),
			func(
				ctx *dispatcher.RequestContext[*session.AuthCtx],
				req *attunev1.ReplaceMCPClientToolPoliciesRequest,
			) (dispatcher.Result[*attunev1.ReplaceMCPClientToolPoliciesResponse], error) {
				resp, err := clients.ReplaceToolPolicies(ctx, ctx.Auth, req.GetId(), ptrext.Of(consolemcpclient.ReplaceToolPoliciesRequest{
					Policies: mcpToolPolicyInputListHandler(req.GetPolicies()),
				}), ctx.Request())
				if err != nil {
					return dispatcher.Result[*attunev1.ReplaceMCPClientToolPoliciesResponse]{}, mapAPIKeyMCPClientError(err)
				}
				return dispatcher.OK(ptrext.Of(attunev1.ReplaceMCPClientToolPoliciesResponse{
					Tools: mcpToolPolicyListProto(resp.Tools),
				}))
			},
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ReplaceMCPClientToolPoliciesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		c.Delete("/{id}/grants/{grant_id}", dispatcher.Bind(
			"apikey.mcpclient.RevokeRefreshGrant",
			dispatcher.Path(
				func() *attunev1.RevokeMCPRefreshGrantRequest {
					return ptrext.Of(attunev1.RevokeMCPRefreshGrantRequest{})
				},
				dispatcher.Param("id", func(req *attunev1.RevokeMCPRefreshGrantRequest, id string) { req.Id = id }),
				dispatcher.Param("grant_id", func(req *attunev1.RevokeMCPRefreshGrantRequest, id string) {
					req.GrantId = id
				}),
			),
			func(
				ctx *dispatcher.RequestContext[*session.AuthCtx],
				req *attunev1.RevokeMCPRefreshGrantRequest,
			) (dispatcher.Result[*attunev1.RevokeMCPRefreshGrantResponse], error) {
				_, err := clients.RevokeRefreshGrant(ctx, ctx.Auth, req.GetId(), req.GetGrantId(), ctx.Request())
				if err != nil {
					return dispatcher.Result[*attunev1.RevokeMCPRefreshGrantResponse]{}, mapAPIKeyMCPClientError(err)
				}
				return dispatcher.NoContent[*attunev1.RevokeMCPRefreshGrantResponse]()
			},
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RevokeMCPRefreshGrantRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		c.Delete("/{id}/sessions/{session_id}", dispatcher.Bind(
			"apikey.mcpclient.RevokeSession",
			dispatcher.Path(
				func() *attunev1.RevokeMCPSessionRequest { return ptrext.Of(attunev1.RevokeMCPSessionRequest{}) },
				dispatcher.Param("id", func(req *attunev1.RevokeMCPSessionRequest, id string) { req.Id = id }),
				dispatcher.Param("session_id", func(req *attunev1.RevokeMCPSessionRequest, id string) {
					req.SessionId = id
				}),
			),
			func(
				ctx *dispatcher.RequestContext[*session.AuthCtx],
				req *attunev1.RevokeMCPSessionRequest,
			) (dispatcher.Result[*attunev1.RevokeMCPSessionResponse], error) {
				_, err := clients.RevokeSession(ctx, ctx.Auth, req.GetId(), req.GetSessionId(), ctx.Request())
				if err != nil {
					return dispatcher.Result[*attunev1.RevokeMCPSessionResponse]{}, mapAPIKeyMCPClientError(err)
				}
				return dispatcher.NoContent[*attunev1.RevokeMCPSessionResponse]()
			},
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RevokeMCPSessionRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func mapAPIKeyMCPClientError(err error) error {
	if err == nil {
		return nil
	}
	if dispatcher.IsRequestContextError(err) {
		return err
	}
	var validationErr *consolemcpclient.ValidationError
	switch {
	case errors.As(err, &validationErr):
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, validationErr.Error())
	case errors.Is(err, mcprepo.ErrClientNameConflict):
		return dispatcher.NewError(http.StatusConflict, attunev1.ErrorCode_CONFLICT, "client name already exists")
	case errors.Is(err, mcprepo.ErrInvalidInput):
		return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "client field is invalid")
	case errors.Is(err, mcprepo.ErrClientNotFound):
		return dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "client not found")
	case errors.Is(err, mcprepo.ErrSessionNotFound):
		return dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "session not found")
	case errors.Is(err, mcprepo.ErrRefreshTokenNotFound):
		return dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "refresh grant not found")
	default:
		return dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to process MCP client request")
	}
}

func mcpClientListProto(in []consolemcpclient.ClientDTO) []*attunev1.MCPClient {
	out := make([]*attunev1.MCPClient, 0, len(in))
	for _, item := range in {
		out = append(out, mcpClientProto(item))
	}
	return out
}

func mcpClientProto(in consolemcpclient.ClientDTO) *attunev1.MCPClient {
	out := ptrext.Of(attunev1.MCPClient{
		Id:             in.ID,
		Name:           in.Name,
		RedirectUris:   append([]string(nil), in.RedirectURIs...),
		Scopes:         append([]string(nil), in.Scopes...),
		ToolPolicyMode: in.ToolPolicyMode,
		CreatedAt:      in.CreatedAt,
		CreatedBy:      in.CreatedBy,
	})
	if in.RateLimitRPM != nil {
		out.RateLimitRpm = ptrext.Of(int32(ptrext.Indirect(in.RateLimitRPM)))
	}
	if in.RateLimitBurst != nil {
		out.RateLimitBurst = ptrext.Of(int32(ptrext.Indirect(in.RateLimitBurst)))
	}
	if in.RevokedAt != nil {
		out.RevokedAt = ptrext.Of(ptrext.Indirect(in.RevokedAt))
	}
	return out
}

func mcpSessionListProto(in []consolemcpclient.SessionDTO) []*attunev1.MCPSession {
	out := make([]*attunev1.MCPSession, 0, len(in))
	for _, item := range in {
		resp := ptrext.Of(attunev1.MCPSession{
			Id:            item.ID,
			Scopes:        append([]string(nil), item.Scopes...),
			LastToolName:  item.LastToolName,
			LastDecision:  item.LastDecision,
			LastIp:        item.LastIP,
			LastUserAgent: item.LastUserAgent,
			LastActiveAt:  item.LastActiveAt,
			CreatedAt:     item.CreatedAt,
			ClosedReason:  item.ClosedReason,
			ClosedBy:      item.ClosedBy,
		})
		if item.ClosedAt != nil {
			resp.ClosedAt = ptrext.Of(ptrext.Indirect(item.ClosedAt))
		}
		out = append(out, resp)
	}
	return out
}

func mcpRefreshGrantListProto(in []consolemcpclient.RefreshGrantDTO) []*attunev1.MCPRefreshGrant {
	out := make([]*attunev1.MCPRefreshGrant, 0, len(in))
	for _, item := range in {
		out = append(out, ptrext.Of(attunev1.MCPRefreshGrant{
			Id:        item.ID,
			SessionId: item.SessionID,
			Scopes:    append([]string(nil), item.Scopes...),
			ExpiresAt: item.ExpiresAt,
			CreatedAt: item.CreatedAt,
		}))
	}
	return out
}

func mcpToolPolicyListProto(in []consolemcpclient.ClientToolPolicy) []*attunev1.MCPClientToolPolicy {
	out := make([]*attunev1.MCPClientToolPolicy, 0, len(in))
	for _, item := range in {
		resp := ptrext.Of(attunev1.MCPClientToolPolicy{
			Name:             item.Name,
			RequiredScope:    item.RequiredScope,
			Risk:             item.Risk,
			DataClass:        item.DataClass,
			ReadOnlyHint:     item.ReadOnlyHint,
			DestructiveHint:  item.DestructiveHint,
			OpenWorldHint:    item.OpenWorldHint,
			DefaultRpm:       int32(item.DefaultRPM),
			DefaultBurst:     int32(item.DefaultBurst),
			ScopeGranted:     item.ScopeGranted,
			EffectiveAllowed: item.EffectiveAllowed,
			Effect:           item.Effect,
		})
		if item.RateLimitRPM != nil {
			resp.RateLimitRpm = ptrext.Of(int32(ptrext.Indirect(item.RateLimitRPM)))
		}
		if item.RateLimitBurst != nil {
			resp.RateLimitBurst = ptrext.Of(int32(ptrext.Indirect(item.RateLimitBurst)))
		}
		out = append(out, resp)
	}
	return out
}

func mcpConnectionProfileProto(in *consolemcpclient.ConnectionProfile) *attunev1.MCPConnectionProfile {
	if in == nil {
		return nil
	}
	return ptrext.Of(attunev1.MCPConnectionProfile{
		ServerUrl:                            in.ServerURL,
		ResourceUrl:                          in.ResourceURL,
		OauthIssuer:                          in.OAuthIssuer,
		AuthorizationEndpoint:                in.AuthorizationEndpoint,
		TokenEndpoint:                        in.TokenEndpoint,
		ProtectedResourceMetadataUrl:         in.ProtectedResourceMetadataURL,
		AuthorizationServerMetadataUrl:       in.AuthorizationServerMetadataURL,
		OpenidConfigurationUrl:               in.OpenIDConfigurationURL,
		LegacyProtectedResourceMetadataUrl:   in.LegacyProtectedResourceMetadataURL,
		LegacyAuthorizationServerMetadataUrl: in.LegacyAuthorizationServerMetadataURL,
		LegacyOpenidConfigurationUrl:         in.LegacyOpenIDConfigurationURL,
	})
}

func mcpToolPolicyInputListHandler(in []*attunev1.MCPToolPolicyInput) []consolemcpclient.ToolPolicyInput {
	if in == nil {
		return nil
	}
	out := make([]consolemcpclient.ToolPolicyInput, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		out = append(out, consolemcpclient.ToolPolicyInput{
			ToolName:       item.GetToolName(),
			Effect:         item.GetEffect(),
			RateLimitRPM:   int32ToInt(item.RateLimitRpm),
			RateLimitBurst: int32ToInt(item.RateLimitBurst),
		})
	}
	return out
}

func int32ToInt(in *int32) *int {
	if in == nil {
		return nil
	}
	return ptrext.Of(int(ptrext.Indirect(in)))
}
