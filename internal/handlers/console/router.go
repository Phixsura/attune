// Package console is the root that wires every console handler
// subpackage (auth, apikey, feedback, inbound, me, notifytarget, usage) into a
// single chi.Router mounted by attune under /fb/v1/console.
//
// Shared helpers live under handlers/console/internal/session: Signer, cookies,
// RequireSession middleware, AuthCtx + FromContext.
//
// Handler subpackages import dispatcher + session. This package (`console`)
// imports the handler subpackages + session for the middleware. No cycles:
// subpackages do not import this root, and session does not import any handler.
package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/apikey"
	"github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/clusters"
	"github.com/Phixsura/attune/internal/handlers/console/digestsubscription"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	"github.com/Phixsura/attune/internal/handlers/console/feedbackjob"
	consoleguardpolicy "github.com/Phixsura/attune/internal/handlers/console/guardpolicy"
	consoleinbound "github.com/Phixsura/attune/internal/handlers/console/inbound"
	"github.com/Phixsura/attune/internal/handlers/console/internal/rbac"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consolellmconfig "github.com/Phixsura/attune/internal/handlers/console/llmconfig"
	"github.com/Phixsura/attune/internal/handlers/console/me"
	"github.com/Phixsura/attune/internal/handlers/console/notifytarget"
	consoleoidc "github.com/Phixsura/attune/internal/handlers/console/oidc"
	consoletag "github.com/Phixsura/attune/internal/handlers/console/tag"
	consoletagassignment "github.com/Phixsura/attune/internal/handlers/console/tagassignment"
	"github.com/Phixsura/attune/internal/handlers/console/usage"
	consoleworkflow "github.com/Phixsura/attune/internal/handlers/console/workflow"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
)

// Re-exports so cmd/attune can keep a single `console.X` surface even
// after the per-feature split. Lets the bootstrap (setup.go) stay close
// to the previous shape without learning every new package path.
type (
	Signer          = session.Signer
	BootstrapConfig = auth.BootstrapConfig
)

// Constructor re-exports so cmd/attune/setup.go can keep building
// handlers via `console.NewXHandler(...)` after the split.
var (
	NewSigner                    = session.NewSigner
	NewAuthHandler               = auth.NewHandler
	NewChangePasswordHandler     = auth.NewChangePasswordHandler
	NewMeHandler                 = me.NewMeHandler
	NewAPIKeysHandler            = apikey.NewAPIKeysHandler
	NewNotifyTargetsHandler      = notifytarget.NewNotifyTargetsHandler
	NewFeedbackHandler           = feedback.NewFeedbackHandler
	NewBatchHandler              = feedback.NewBatchHandler
	NewSearchHandler             = feedback.NewSearchHandler
	NewFeedbackJobHandler        = feedbackjob.NewHandler
	NewUsageHandler              = usage.NewUsageHandler
	NewEnrichConfigHandler       = enrichconfig.NewHandler
	NewGuardPolicyHandler        = consoleguardpolicy.NewHandler
	NewInboundHandler            = consoleinbound.NewHandler
	NewLLMConfigHandler          = consolellmconfig.NewHandler
	NewClustersHandler           = clusters.NewClustersHandler
	NewDigestSubscriptionHandler = digestsubscription.NewHandler
	NewTagHandler                = consoletag.NewHandler
	NewTagAssignmentHandler      = consoletagassignment.NewHandler
	NewWorkflowHandler           = consoleworkflow.NewHandler
	NewOIDCHandler               = consoleoidc.NewHandler
	BootstrapAdmin               = auth.BootstrapAdmin
)

// Router wires every console endpoint into a single chi.Router.
//
// Endpoint inventory:
//
//	public (no session required):
//	 POST /install/login -> dispatcher.Bind(auth.Handler.Login)
//
//	session-required (RequireSession middleware):
//	 GET /me -> dispatcher.Bind(me.Handler.Me)
//	 POST /logout -> dispatcher.Bind(me.Handler.Logout)
//	 POST /me/change-password -> dispatcher.Bind(auth.ChangePasswordHandler.ChangePassword)
//	 GET /api-keys -> dispatcher.Bind(apikey.Handler.List)
//	 POST /api-keys -> dispatcher.Bind(apikey.Handler.Create)
//	 DELETE /api-keys/{id} -> dispatcher.Bind(apikey.Handler.Revoke)
//	 GET /notify-targets -> dispatcher.Bind(notifytarget.Handler.List)
//	 POST /notify-targets -> dispatcher.Bind(notifytarget.Handler.Create)
//	 PATCH /notify-targets/{id} -> dispatcher.Bind(notifytarget.Handler.Patch)
//	 DELETE /notify-targets/{id} -> dispatcher.Bind(notifytarget.Handler.Delete)
//	 POST /notify-targets/{id}/test -> dispatcher.Bind(notifytarget.Handler.Test)
//	 GET /feedback -> dispatcher.Bind(feedback.Handler.List)
//	 GET /feedback/stats -> dispatcher.Bind(feedback.Handler.Stats)
//	 POST /feedback/search -> dispatcher.Bind(feedback.SearchHandler.Search)
//	 GET /feedback/{id} -> dispatcher.Bind(feedback.Handler.Get)
//	 GET /usage -> dispatcher.Bind(usage.Handler.Get)
//	 GET /llm-usage -> dispatcher.Bind(usage.Handler.GetLLMUsage)
//	 GET /enrich-config -> dispatcher.Bind(enrichconfig.Handler.Get)
//	 PUT /enrich-config -> dispatcher.Bind(enrichconfig.Handler.Update)
//	 POST /enrich-config/preview -> dispatcher.Bind(enrichconfig.Handler.Preview)
//	 GET /guard-policies -> dispatcher.Bind(guardpolicy.Handler.List)
//	 POST /guard-policies -> dispatcher.Bind(guardpolicy.Handler.Create)
//	 PUT /guard-policies -> dispatcher.Bind(guardpolicy.Handler.Update)
//	 PATCH /guard-policies/{id} -> dispatcher.Bind(guardpolicy.Handler.Patch)
//	 DELETE /guard-policies/{id} -> dispatcher.Bind(guardpolicy.Handler.Delete)
//	 POST /guard-policies/effective -> dispatcher.Bind(guardpolicy.Handler.Resolve)
//	 GET /inbound/sources -> dispatcher.Bind(inbound.Handler.List)
//	 POST /inbound/sources -> dispatcher.Bind(inbound.Handler.Create)
//	 GET /inbound/sources/{id} -> dispatcher.Bind(inbound.Handler.Get)
//	 POST /inbound/sources/{id}/rotate-secret -> dispatcher.Bind(inbound.Handler.Rotate)
//	 POST /inbound/sources/{id}/pause -> dispatcher.Bind(inbound.Handler.Pause)
//	 POST /inbound/sources/{id}/resume -> dispatcher.Bind(inbound.Handler.Resume)
//	 DELETE /inbound/sources/{id} -> dispatcher.Bind(inbound.Handler.Delete)
//	 POST /inbound/sources/test-connection -> dispatcher.Bind(inbound.Handler.TestConnection)
//	 POST /llm/channels/{id}/test -> dispatcher.Bind(llmconfig.Handler.TestChannel)
//	 GET /llm/channels/{channel_id}/models -> dispatcher.Bind(llmconfig.Handler.ListChannelModels)
//	 GET /clusters -> dispatcher.Bind(clusters.Handler.List)
//	 GET /clusters/{cluster_id}/members -> dispatcher.Bind(clusters.Handler.GetMembers)
type Router struct {
	signer             *session.Signer
	login              *auth.Handler
	changePassword     *auth.ChangePasswordHandler
	me                 *me.MeHandler
	apiKeys            *apikey.APIKeysHandler
	notifyTargets      *notifytarget.NotifyTargetsHandler
	feedback           *feedback.FeedbackHandler
	feedbackBatch      *feedback.BatchHandler
	feedbackSearch     *feedback.SearchHandler
	feedbackJob        *feedbackjob.Handler
	usage              *usage.UsageHandler
	enrichConfig       *enrichconfig.Handler
	guardPolicies      *consoleguardpolicy.Handler
	inbound            *consoleinbound.Handler
	llmConfig          *consolellmconfig.Handler
	clusters           *clusters.ClustersHandler
	digestSubscription *digestsubscription.Handler
	tags               *consoletag.Handler
	tagAssignments     *consoletagassignment.Handler
	workflow           *consoleworkflow.Handler
	oidc               *consoleoidc.Handler
	admins             adminReader
	rbac               *rbac.Middleware
}

type adminReader interface {
	GetByID(ctx context.Context, id string) (admin.Admin, error)
}

func NewRouter(
	signer *session.Signer,
	authH *auth.Handler,
	changePassword *auth.ChangePasswordHandler,
	me *me.MeHandler,
	apiKeys *apikey.APIKeysHandler,
	notifyTargets *notifytarget.NotifyTargetsHandler,
	feedback *feedback.FeedbackHandler,
	feedbackBatch *feedback.BatchHandler,
	feedbackSearch *feedback.SearchHandler,
	feedbackJob *feedbackjob.Handler,
	usage *usage.UsageHandler,
	enrichConfig *enrichconfig.Handler,
	guardPolicies *consoleguardpolicy.Handler,
	inbound *consoleinbound.Handler,
	llmConfig *consolellmconfig.Handler,
	clustersH *clusters.ClustersHandler,
	digestSubscription *digestsubscription.Handler,
	tags *consoletag.Handler,
	tagAssignments *consoletagassignment.Handler,
	workflow *consoleworkflow.Handler,
	oidc *consoleoidc.Handler,
	admins adminReader,
	members *tenantmember.Repo,
) *Router {
	var rbacMW *rbac.Middleware
	if members != nil {
		rbacMW = rbac.NewMiddleware(members)
	}
	return ptrext.Of(Router{
		signer:             signer,
		login:              authH,
		changePassword:     changePassword,
		me:                 me,
		apiKeys:            apiKeys,
		notifyTargets:      notifyTargets,
		feedback:           feedback,
		feedbackBatch:      feedbackBatch,
		feedbackSearch:     feedbackSearch,
		feedbackJob:        feedbackJob,
		usage:              usage,
		enrichConfig:       enrichConfig,
		guardPolicies:      guardPolicies,
		inbound:            inbound,
		llmConfig:          llmConfig,
		clusters:           clustersH,
		digestSubscription: digestSubscription,
		tags:               tags,
		tagAssignments:     tagAssignments,
		workflow:           workflow,
		oidc:               oidc,
		admins:             admins,
		rbac:               rbacMW,
	})
}

func (r *Router) Mount() chi.Router {
	mux := chi.NewRouter()

	mux.Post("/install/login", dispatcher.Bind(
		"console.auth.Handler.Login",
		dispatcher.Combine(
			func() *attunev1.LoginRequest { return ptrext.Of(attunev1.LoginRequest{}) },
			r.login.RequireLoginOrigin,
			dispatcher.JSONBody[*attunev1.LoginRequest],
			r.login.ValidateRequest,
		),
		r.login.Login,
		dispatcher.WithAuth(func(_ *http.Request, _ *attunev1.LoginRequest) (struct{}, error) {
			return struct{}{}, nil
		}),
	))

	// OIDC SSO endpoints (public, no session required)
	r.mountOIDC(mux)

	// /auth/providers returns available auth methods (public)
	mux.Get("/auth/providers", r.authProviders)

	mux.Group(func(m chi.Router) {
		m.Use(r.signer.RequireSession)
		if r.login != nil {
			m.Use(r.login.ScopeAdminSession)
		}
		r.mountSession(m)
	})

	return mux
}

func (r *Router) mountSession(m chi.Router) {
	m.Get("/me", dispatcher.Bind(
		"console.MeHandler.Me",
		dispatcher.Empty(func() *attunev1.GetMeRequest { return ptrext.Of(attunev1.GetMeRequest{}) }),
		r.me.Me,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetMeRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.Post("/logout", dispatcher.Bind(
		"console.MeHandler.Logout",
		dispatcher.Empty(func() *attunev1.LogoutRequest { return ptrext.Of(attunev1.LogoutRequest{}) }),
		r.me.Logout,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.LogoutRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	if r.changePassword != nil {
		m.Post("/me/change-password", dispatcher.Bind(
			"console.auth.ChangePasswordHandler.ChangePassword",
			dispatcher.Combine(
				func() *attunev1.ChangePasswordRequest { return ptrext.Of(attunev1.ChangePasswordRequest{}) },
				dispatcher.JSONBody[*attunev1.ChangePasswordRequest],
				r.changePassword.ValidateRequest,
			),
			r.changePassword.ChangePassword,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ChangePasswordRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	}
	r.mountAPIKeys(m)
	r.mountNotifyTargets(m)
	r.mountDigestSubscription(m)
	r.mountFeedback(m)
	m.Get("/usage", dispatcher.Bind(
		"console.UsageHandler.Get",
		dispatcher.Empty(func() *attunev1.GetUsageRequest { return ptrext.Of(attunev1.GetUsageRequest{}) }),
		r.usage.Get,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetUsageRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.Get("/llm-usage", dispatcher.Bind(
		"console.UsageHandler.GetLLMUsage",
		dispatcher.Query(
			func() *attunev1.GetLLMUsageRequest { return ptrext.Of(attunev1.GetLLMUsageRequest{}) },
			usage.BindLLMUsageRequest,
		),
		r.usage.GetLLMUsage,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetLLMUsageRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	r.mountEnrichConfig(m)
	r.mountGuardPolicies(m)
	r.mountInbound(m)
	r.mountJobs(m)
	r.mountLLMConfig(m)
	r.mountClusters(m)
	r.mountTags(m)
	r.mountWorkflow(m)
}

func (r *Router) mountLLMConfig(m chi.Router) {
	if r.llmConfig == nil {
		return
	}
	m.Route("/llm", func(l chi.Router) {
		l.Use(r.requireAdminStrict) // Bypass cache for sensitive LLM config
		l.Get("/channels", dispatcher.Bind(
			"console.llmconfig.ListChannels",
			dispatcher.Empty(func() *attunev1.ListLLMChannelsRequest { return ptrext.Of(attunev1.ListLLMChannelsRequest{}) }),
			r.llmConfig.ListChannels,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListLLMChannelsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		l.Post("/channels", dispatcher.Bind(
			"console.llmconfig.CreateChannel",
			dispatcher.JSON(func() *attunev1.CreateLLMChannelRequest { return ptrext.Of(attunev1.CreateLLMChannelRequest{}) }),
			r.llmConfig.CreateChannel,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateLLMChannelRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		l.Get("/channels/{id}", dispatcher.Bind(
			"console.llmconfig.GetChannel",
			dispatcher.Path(
				func() *attunev1.GetLLMChannelRequest { return ptrext.Of(attunev1.GetLLMChannelRequest{}) },
				dispatcher.Param("id", func(req *attunev1.GetLLMChannelRequest, id string) { req.Id = id }),
			),
			r.llmConfig.GetChannel,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetLLMChannelRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		l.Patch("/channels/{id}", dispatcher.Bind(
			"console.llmconfig.UpdateChannel",
			dispatcher.Combine(
				func() *attunev1.UpdateLLMChannelRequest { return ptrext.Of(attunev1.UpdateLLMChannelRequest{}) },
				dispatcher.JSONBody[*attunev1.UpdateLLMChannelRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateLLMChannelRequest, id string) { req.Id = id }),
			),
			r.llmConfig.UpdateChannel,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateLLMChannelRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		l.Delete("/channels/{id}", dispatcher.Bind(
			"console.llmconfig.DeleteChannel",
			dispatcher.Path(
				func() *attunev1.DeleteLLMChannelRequest { return ptrext.Of(attunev1.DeleteLLMChannelRequest{}) },
				dispatcher.Param("id", func(req *attunev1.DeleteLLMChannelRequest, id string) { req.Id = id }),
			),
			r.llmConfig.DeleteChannel,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteLLMChannelRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		l.Post("/channels/{id}/test", dispatcher.Bind(
			"console.llmconfig.TestChannel",
			dispatcher.Combine(
				func() *attunev1.TestLLMChannelRequest { return ptrext.Of(attunev1.TestLLMChannelRequest{}) },
				dispatcher.JSONBody[*attunev1.TestLLMChannelRequest],
				dispatcher.Param("id", func(req *attunev1.TestLLMChannelRequest, id string) { req.Id = id }),
			),
			r.llmConfig.TestChannel,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TestLLMChannelRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		l.Get("/channels/{channel_id}/models", dispatcher.Bind(
			"console.llmconfig.ListChannelModels",
			dispatcher.Path(
				func() *attunev1.ListLLMChannelModelsRequest {
					return ptrext.Of(attunev1.ListLLMChannelModelsRequest{})
				},
				dispatcher.Param("channel_id", func(req *attunev1.ListLLMChannelModelsRequest, id string) {
					req.ChannelId = id
				}),
			),
			r.llmConfig.ListChannelModels,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListLLMChannelModelsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		r.mountLLMAbilities(l)
		r.mountLLMRoutes(l)
	})
}

func (r *Router) requireAdmin(next http.Handler) http.Handler {
	// Use RBAC middleware if available (tenant_members table exists)
	if r.rbac != nil {
		return r.rbac.RequireAdmin()(next)
	}
	// Fallback to legacy admin table check
	return r.requireAdminLegacy(next)
}

func (r *Router) requireAdminStrict(next http.Handler) http.Handler {
	// Use RBAC strict middleware if available (bypasses cache)
	if r.rbac != nil {
		return r.rbac.RequireAdminStrict()(next)
	}
	return r.requireAdminLegacy(next)
}

func (r *Router) requireAdminLegacy(next http.Handler) http.Handler {
	const where = "console.Router.requireAdminLegacy"
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authCtx := session.FromContext(req.Context())
		if r.admins == nil {
			logext.Warnf(req.Context(), "[%s] reject: admin repo not configured,user_id:%s", where, authCtx.UserID)
			dispatcher.Reject(req.Context(), w, http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "admin session required")
			return
		}
		adminRow, err := r.admins.GetByID(req.Context(), authCtx.UserID)
		if err != nil {
			if errors.Is(err, admin.ErrNotFound) {
				logext.Warnf(req.Context(), "[%s] reject: non-admin session,user_id:%s", where, authCtx.UserID)
				dispatcher.Reject(req.Context(), w, http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "admin session required")
				return
			}
			logext.Errorf(req.Context(), "[%s] admin lookup failed,user_id:%s,err:%+v",
				where, authCtx.UserID, err.Error())
			dispatcher.Reject(req.Context(), w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to verify admin session")
			return
		}
		if adminRow.Role != "admin" {
			logext.Warnf(req.Context(), "[%s] reject: non-admin role,user_id:%s,role:%s",
				where, authCtx.UserID, adminRow.Role)
			dispatcher.Reject(req.Context(), w, http.StatusForbidden, attunev1.ErrorCode_FORBIDDEN, "admin session required")
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (r *Router) mountLLMAbilities(l chi.Router) {
	l.Get("/channels/{channel_id}/abilities", dispatcher.Bind(
		"console.llmconfig.ListAbilities",
		dispatcher.Path(
			func() *attunev1.ListLLMChannelAbilitiesRequest {
				return ptrext.Of(attunev1.ListLLMChannelAbilitiesRequest{})
			},
			dispatcher.Param("channel_id", func(req *attunev1.ListLLMChannelAbilitiesRequest, id string) {
				req.ChannelId = id
			}),
		),
		r.llmConfig.ListAbilities,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListLLMChannelAbilitiesRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	l.Put("/channels/{channel_id}/abilities", dispatcher.Bind(
		"console.llmconfig.UpsertAbility",
		dispatcher.Combine(
			func() *attunev1.UpsertLLMChannelAbilityRequest {
				return ptrext.Of(attunev1.UpsertLLMChannelAbilityRequest{})
			},
			dispatcher.JSONBody[*attunev1.UpsertLLMChannelAbilityRequest],
			dispatcher.Param("channel_id", func(req *attunev1.UpsertLLMChannelAbilityRequest, id string) {
				req.ChannelId = id
			}),
		),
		r.llmConfig.UpsertAbility,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpsertLLMChannelAbilityRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	l.Post("/channels/{channel_id}/abilities/delete", dispatcher.Bind(
		"console.llmconfig.DeleteAbility",
		dispatcher.Combine(
			func() *attunev1.DeleteLLMChannelAbilityRequest {
				return ptrext.Of(attunev1.DeleteLLMChannelAbilityRequest{})
			},
			dispatcher.JSONBody[*attunev1.DeleteLLMChannelAbilityRequest],
			dispatcher.Param("channel_id", func(req *attunev1.DeleteLLMChannelAbilityRequest, id string) {
				req.ChannelId = id
			}),
		),
		r.llmConfig.DeleteAbility,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteLLMChannelAbilityRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountLLMRoutes(l chi.Router) {
	l.Get("/routes", dispatcher.Bind(
		"console.llmconfig.ListRoutes",
		dispatcher.Empty(func() *attunev1.ListLLMRoutesRequest { return ptrext.Of(attunev1.ListLLMRoutesRequest{}) }),
		r.llmConfig.ListRoutes,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListLLMRoutesRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	l.Put("/routes", dispatcher.Bind(
		"console.llmconfig.UpsertRoute",
		dispatcher.JSON(func() *attunev1.UpsertLLMRouteRequest { return ptrext.Of(attunev1.UpsertLLMRouteRequest{}) }),
		r.llmConfig.UpsertRoute,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpsertLLMRouteRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	l.Post("/routes/delete", dispatcher.Bind(
		"console.llmconfig.DeleteRoute",
		dispatcher.JSON(func() *attunev1.DeleteLLMRouteRequest { return ptrext.Of(attunev1.DeleteLLMRouteRequest{}) }),
		r.llmConfig.DeleteRoute,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteLLMRouteRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountAPIKeys(m chi.Router) {
	m.Route("/api-keys", func(k chi.Router) {
		k.Get("/", dispatcher.Bind(
			"console.APIKeysHandler.List",
			dispatcher.Empty(func() *attunev1.ListApiKeysRequest { return ptrext.Of(attunev1.ListApiKeysRequest{}) }),
			r.apiKeys.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListApiKeysRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		k.Post("/", dispatcher.Bind(
			"console.APIKeysHandler.Create",
			dispatcher.JSON(func() *attunev1.CreateApiKeyRequest { return ptrext.Of(attunev1.CreateApiKeyRequest{}) }),
			r.apiKeys.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateApiKeyRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		k.Delete("/{id}", dispatcher.Bind(
			"console.APIKeysHandler.Revoke",
			dispatcher.Path(
				func() *attunev1.DeleteApiKeyRequest { return ptrext.Of(attunev1.DeleteApiKeyRequest{}) },
				dispatcher.Param("id", func(req *attunev1.DeleteApiKeyRequest, id string) {
					req.Id = id
				}),
			),
			r.apiKeys.Revoke,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteApiKeyRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountNotifyTargets(m chi.Router) {
	m.Route("/notify-targets", func(n chi.Router) {
		n.Get("/", dispatcher.Bind(
			"console.NotifyTargetsHandler.List",
			dispatcher.Empty(func() *attunev1.ListNotifyTargetsRequest { return ptrext.Of(attunev1.ListNotifyTargetsRequest{}) }),
			r.notifyTargets.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListNotifyTargetsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		n.Post("/", dispatcher.Bind(
			"console.NotifyTargetsHandler.Create",
			dispatcher.JSON(func() *attunev1.CreateNotifyTargetRequest { return ptrext.Of(attunev1.CreateNotifyTargetRequest{}) }),
			r.notifyTargets.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateNotifyTargetRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		n.Patch("/{id}", dispatcher.Bind(
			"console.NotifyTargetsHandler.Patch",
			dispatcher.Combine(
				func() *attunev1.UpdateNotifyTargetRequest { return ptrext.Of(attunev1.UpdateNotifyTargetRequest{}) },
				dispatcher.JSONBody[*attunev1.UpdateNotifyTargetRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateNotifyTargetRequest, id string) {
					req.Id = id
				}),
			),
			r.notifyTargets.Patch,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateNotifyTargetRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		n.Delete("/{id}", dispatcher.Bind(
			"console.NotifyTargetsHandler.Delete",
			dispatcher.Path(
				func() *attunev1.DeleteNotifyTargetRequest { return ptrext.Of(attunev1.DeleteNotifyTargetRequest{}) },
				dispatcher.Param("id", func(req *attunev1.DeleteNotifyTargetRequest, id string) {
					req.Id = id
				}),
			),
			r.notifyTargets.Delete,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteNotifyTargetRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		n.Post("/{id}/test", dispatcher.Bind(
			"console.NotifyTargetsHandler.Test",
			dispatcher.Path(
				func() *attunev1.TestNotifyTargetRequest { return ptrext.Of(attunev1.TestNotifyTargetRequest{}) },
				dispatcher.Param("id", func(req *attunev1.TestNotifyTargetRequest, id string) {
					req.Id = id
				}),
			),
			r.notifyTargets.Test,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TestNotifyTargetRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

// mountDigestSubscription mounts the per-tenant daily digest config (#27). It is
// a singleton resource (one subscription per tenant), so get / upsert / delete
// with no id path param.
func (r *Router) mountDigestSubscription(m chi.Router) {
	m.Get("/digest-subscription", dispatcher.Bind(
		"console.DigestSubscriptionHandler.Get",
		dispatcher.Empty(func() *attunev1.GetDigestSubscriptionRequest {
			return ptrext.Of(attunev1.GetDigestSubscriptionRequest{})
		}),
		r.digestSubscription.Get,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetDigestSubscriptionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.Put("/digest-subscription", dispatcher.Bind(
		"console.DigestSubscriptionHandler.Upsert",
		dispatcher.JSON(func() *attunev1.UpsertDigestSubscriptionRequest {
			return ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{})
		}),
		r.digestSubscription.Upsert,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpsertDigestSubscriptionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.Delete("/digest-subscription", dispatcher.Bind(
		"console.DigestSubscriptionHandler.Delete",
		dispatcher.Empty(func() *attunev1.DeleteDigestSubscriptionRequest {
			return ptrext.Of(attunev1.DeleteDigestSubscriptionRequest{})
		}),
		r.digestSubscription.Delete,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteDigestSubscriptionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountFeedback(m chi.Router) {
	m.Route("/feedback", func(f chi.Router) {
		f.Get("/", dispatcher.Bind(
			"console.FeedbackHandler.List",
			dispatcher.Query(func() *attunev1.ListFeedbackRequest { return ptrext.Of(attunev1.ListFeedbackRequest{}) }, feedback.BindListRequest),
			r.feedback.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListFeedbackRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		// /stats and /batch/tags must come BEFORE /{id}; source order keeps the intent clear.
		f.Get("/stats", dispatcher.Bind(
			"console.FeedbackHandler.Stats",
			dispatcher.Empty(func() *attunev1.GetFeedbackStatsRequest { return ptrext.Of(attunev1.GetFeedbackStatsRequest{}) }),
			r.feedback.Stats,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackStatsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		if r.tagAssignments != nil {
			f.Post("/batch/tags", dispatcher.Bind(
				"console.TagAssignmentHandler.BatchUpdate",
				dispatcher.JSON(func() *attunev1.BatchUpdateFeedbackTagsRequest {
					return ptrext.Of(attunev1.BatchUpdateFeedbackTagsRequest{})
				}),
				r.tagAssignments.BatchUpdate,
				dispatcher.WithAuth(func(r *http.Request, _ *attunev1.BatchUpdateFeedbackTagsRequest) (*session.AuthCtx, error) {
					return session.FromContext(r.Context()), nil
				}),
			))
		}
		if r.feedback != nil {
			f.Post("/transition/batch", dispatcher.Bind(
				"console.FeedbackHandler.BatchTransitionState",
				dispatcher.JSON(func() *attunev1.BatchTransitionFeedbackRequest {
					return ptrext.Of(attunev1.BatchTransitionFeedbackRequest{})
				}),
				r.feedback.BatchTransitionState,
				dispatcher.WithAuth(func(r *http.Request, _ *attunev1.BatchTransitionFeedbackRequest) (*session.AuthCtx, error) {
					return session.FromContext(r.Context()), nil
				}),
			))
		}
		r.mountFeedbackBatchRoutes(f)
		f.Get("/{id}", dispatcher.Bind(
			"console.FeedbackHandler.Get",
			dispatcher.Path(
				func() *attunev1.GetFeedbackRequest { return ptrext.Of(attunev1.GetFeedbackRequest{}) },
				dispatcher.ParamInt64("id", func(req *attunev1.GetFeedbackRequest, id int64) {
					req.Id = id
				}, "id must be an integer"),
			),
			r.feedback.Get,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		f.Post("/{id}/reply-draft/regenerate", dispatcher.Bind(
			"console.FeedbackHandler.Regenerate",
			dispatcher.Path(
				func() *attunev1.RegenerateReplyDraftRequest { return ptrext.Of(attunev1.RegenerateReplyDraftRequest{}) },
				dispatcher.ParamInt64("id", func(req *attunev1.RegenerateReplyDraftRequest, id int64) {
					req.Id = id
				}, "id must be an integer"),
			),
			r.feedback.Regenerate,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RegenerateReplyDraftRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		if r.tagAssignments != nil {
			f.Post("/{id}/tags", dispatcher.Bind(
				"console.TagAssignmentHandler.Add",
				dispatcher.Combine(
					func() *attunev1.AddFeedbackTagRequest { return ptrext.Of(attunev1.AddFeedbackTagRequest{}) },
					dispatcher.JSONBody[*attunev1.AddFeedbackTagRequest],
					dispatcher.ParamInt64("id", func(req *attunev1.AddFeedbackTagRequest, id int64) {
						req.FeedbackId = id
					}, "id must be an integer"),
				),
				r.tagAssignments.Add,
				dispatcher.WithAuth(func(r *http.Request, _ *attunev1.AddFeedbackTagRequest) (*session.AuthCtx, error) {
					return session.FromContext(r.Context()), nil
				}),
			))
			f.Delete("/{id}/tags/{tag_id}", dispatcher.Bind(
				"console.TagAssignmentHandler.Remove",
				dispatcher.Path(
					func() *attunev1.RemoveFeedbackTagRequest { return ptrext.Of(attunev1.RemoveFeedbackTagRequest{}) },
					dispatcher.ParamInt64("id", func(req *attunev1.RemoveFeedbackTagRequest, id int64) {
						req.FeedbackId = id
					}, "id must be an integer"),
					dispatcher.Param("tag_id", func(req *attunev1.RemoveFeedbackTagRequest, id string) {
						req.TagId = id
					}),
				),
				r.tagAssignments.Remove,
				dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RemoveFeedbackTagRequest) (*session.AuthCtx, error) {
					return session.FromContext(r.Context()), nil
				}),
			))
		}
		f.Post("/{id}/transition", dispatcher.Bind(
			"console.FeedbackHandler.TransitionState",
			dispatcher.Combine(
				func() *attunev1.TransitionFeedbackRequest {
					return ptrext.Of(attunev1.TransitionFeedbackRequest{})
				},
				dispatcher.JSONBody[*attunev1.TransitionFeedbackRequest],
				dispatcher.ParamInt64("id", func(req *attunev1.TransitionFeedbackRequest, id int64) {
					req.FeedbackId = id
				}, "id must be an integer"),
			),
			r.feedback.TransitionState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TransitionFeedbackRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		f.Get("/{id}/audit", dispatcher.Bind(
			"console.FeedbackHandler.ListAudit",
			dispatcher.Combine(
				func() *attunev1.ListAuditRequest { return ptrext.Of(attunev1.ListAuditRequest{}) },
				dispatcher.ParamInt64("id", func(req *attunev1.ListAuditRequest, id int64) {
					req.FeedbackId = id
				}, "id must be an integer"),
				func(r *http.Request, req *attunev1.ListAuditRequest) error {
					q := r.URL.Query()
					if c := q.Get("cursor"); c != "" {
						req.Cursor = ptrext.Of(c)
					}
					if l := q.Get("limit"); l != "" {
						v, err := strconv.ParseInt(l, 10, 32)
						if err != nil {
							return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "limit must be an integer")
						}
						req.Limit = ptrext.Of(int32(v))
					}
					return nil
				},
			),
			r.feedback.ListAudit,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListAuditRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountFeedbackBatchRoutes(f chi.Router) {
	if r.feedbackBatch != nil {
		f.Post("/batch", dispatcher.Bind(
			"console.BatchHandler.Execute",
			dispatcher.JSON(func() *attunev1.BatchFeedbackRequest {
				return ptrext.Of(attunev1.BatchFeedbackRequest{})
			}),
			r.feedbackBatch.Execute,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.BatchFeedbackRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	}
	if r.feedbackSearch != nil {
		f.Post("/search", dispatcher.Bind(
			"console.SearchHandler.Search",
			dispatcher.JSON(func() *attunev1.SemanticSearchRequest {
				return ptrext.Of(attunev1.SemanticSearchRequest{})
			}),
			r.feedbackSearch.Search,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SemanticSearchRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	}
}

func (r *Router) mountEnrichConfig(m chi.Router) {
	m.Route("/enrich-config", func(e chi.Router) {
		e.Get("/", dispatcher.Bind(
			"console.EnrichConfigHandler.Get",
			dispatcher.Empty(func() *attunev1.GetEnrichConfigRequest { return ptrext.Of(attunev1.GetEnrichConfigRequest{}) }),
			r.enrichConfig.Get,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetEnrichConfigRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.Put("/", dispatcher.Bind(
			"console.EnrichConfigHandler.Update",
			dispatcher.JSON(func() *attunev1.UpdateEnrichConfigRequest { return ptrext.Of(attunev1.UpdateEnrichConfigRequest{}) }),
			r.enrichConfig.Update,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateEnrichConfigRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.Post("/preview", dispatcher.Bind(
			"console.EnrichConfigHandler.Preview",
			dispatcher.JSON(func() *attunev1.PreviewEnrichPromptRequest { return ptrext.Of(attunev1.PreviewEnrichPromptRequest{}) }),
			r.enrichConfig.Preview,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PreviewEnrichPromptRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountGuardPolicies(m chi.Router) {
	m.Route("/guard-policies", func(g chi.Router) {
		g.Get("/", dispatcher.Bind(
			"console.GuardPolicyHandler.List",
			dispatcher.Empty(func() *attunev1.ListGuardPoliciesRequest {
				return ptrext.Of(attunev1.ListGuardPoliciesRequest{})
			}),
			r.guardPolicies.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListGuardPoliciesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Post("/", dispatcher.Bind(
			"console.GuardPolicyHandler.Create",
			dispatcher.JSON(func() *attunev1.CreateGuardPolicyRequest {
				return ptrext.Of(attunev1.CreateGuardPolicyRequest{})
			}),
			r.guardPolicies.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateGuardPolicyRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Put("/", dispatcher.Bind(
			"console.GuardPolicyHandler.Update",
			dispatcher.JSON(func() *attunev1.UpdateGuardPoliciesRequest {
				return ptrext.Of(attunev1.UpdateGuardPoliciesRequest{})
			}),
			r.guardPolicies.Update,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateGuardPoliciesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Patch("/{id}", dispatcher.Bind(
			"console.GuardPolicyHandler.Patch",
			dispatcher.Combine(
				func() *attunev1.PatchGuardPolicyRequest {
					return ptrext.Of(attunev1.PatchGuardPolicyRequest{})
				},
				dispatcher.JSONBody[*attunev1.PatchGuardPolicyRequest],
				dispatcher.Param("id", func(req *attunev1.PatchGuardPolicyRequest, id string) {
					req.Id = id
				}),
			),
			r.guardPolicies.Patch,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PatchGuardPolicyRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Delete("/{id}", dispatcher.Bind(
			"console.GuardPolicyHandler.Delete",
			dispatcher.Path(
				func() *attunev1.DeleteGuardPolicyRequest {
					return ptrext.Of(attunev1.DeleteGuardPolicyRequest{})
				},
				dispatcher.Param("id", func(req *attunev1.DeleteGuardPolicyRequest, id string) {
					req.Id = id
				}),
			),
			r.guardPolicies.Delete,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteGuardPolicyRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Post("/effective", dispatcher.Bind(
			"console.GuardPolicyHandler.Resolve",
			dispatcher.JSON(func() *attunev1.ResolveGuardPolicyRequest {
				return ptrext.Of(attunev1.ResolveGuardPolicyRequest{})
			}),
			r.guardPolicies.Resolve,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ResolveGuardPolicyRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountInbound(m chi.Router) {
	if r.inbound == nil {
		return
	}
	m.Route("/inbound/sources", func(s chi.Router) {
		s.Get("/", dispatcher.Bind(
			"console.inbound.List",
			dispatcher.Empty(func() *attunev1.ListInboundSourcesRequest { return ptrext.Of(attunev1.ListInboundSourcesRequest{}) }),
			r.inbound.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListInboundSourcesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		s.Post("/", dispatcher.Bind(
			"console.inbound.Create",
			dispatcher.JSON(func() *attunev1.CreateInboundSourceRequest { return ptrext.Of(attunev1.CreateInboundSourceRequest{}) }),
			r.inbound.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateInboundSourceRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		s.Post("/test-connection", dispatcher.Bind(
			"console.inbound.TestConnection",
			dispatcher.JSON(func() *attunev1.TestInboundConnectionRequest {
				return ptrext.Of(attunev1.TestInboundConnectionRequest{})
			}),
			r.inbound.TestConnection,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TestInboundConnectionRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		s.Get("/{id}", dispatcher.Bind(
			"console.inbound.Get",
			dispatcher.Path(
				func() *attunev1.GetInboundSourceRequest { return ptrext.Of(attunev1.GetInboundSourceRequest{}) },
				dispatcher.Param("id", func(req *attunev1.GetInboundSourceRequest, id string) { req.Id = id }),
			),
			r.inbound.Get,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetInboundSourceRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		s.Delete("/{id}", dispatcher.Bind(
			"console.inbound.Delete",
			dispatcher.Path(
				func() *attunev1.DeleteInboundSourceRequest { return ptrext.Of(attunev1.DeleteInboundSourceRequest{}) },
				dispatcher.Param("id", func(req *attunev1.DeleteInboundSourceRequest, id string) { req.Id = id }),
			),
			r.inbound.Delete,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteInboundSourceRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		s.Post("/{id}/rotate-secret", dispatcher.Bind(
			"console.inbound.Rotate",
			dispatcher.Path(
				func() *attunev1.RotateInboundSourceSecretRequest {
					return ptrext.Of(attunev1.RotateInboundSourceSecretRequest{})
				},
				dispatcher.Param("id", func(req *attunev1.RotateInboundSourceSecretRequest, id string) { req.Id = id }),
			),
			r.inbound.Rotate,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RotateInboundSourceSecretRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		s.Post("/{id}/pause", dispatcher.Bind(
			"console.inbound.Pause",
			dispatcher.Path(
				func() *attunev1.PauseInboundSourceRequest { return ptrext.Of(attunev1.PauseInboundSourceRequest{}) },
				dispatcher.Param("id", func(req *attunev1.PauseInboundSourceRequest, id string) { req.Id = id }),
			),
			r.inbound.Pause,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PauseInboundSourceRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		s.Post("/{id}/resume", dispatcher.Bind(
			"console.inbound.Resume",
			dispatcher.Path(
				func() *attunev1.ResumeInboundSourceRequest { return ptrext.Of(attunev1.ResumeInboundSourceRequest{}) },
				dispatcher.Param("id", func(req *attunev1.ResumeInboundSourceRequest, id string) { req.Id = id }),
			),
			r.inbound.Resume,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ResumeInboundSourceRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountClusters(m chi.Router) {
	if r.clusters == nil {
		return
	}
	m.Route("/clusters", func(c chi.Router) {
		c.Get("/", dispatcher.Bind(
			"console.ClustersHandler.List",
			dispatcher.Query(
				func() *attunev1.ListClustersRequest { return ptrext.Of(attunev1.ListClustersRequest{}) },
				clusters.BindListRequest,
			),
			r.clusters.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListClustersRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		c.Get("/{cluster_id}/members", dispatcher.Bind(
			"console.ClustersHandler.GetMembers",
			dispatcher.Combine(
				func() *attunev1.GetClusterMembersRequest { return ptrext.Of(attunev1.GetClusterMembersRequest{}) },
				dispatcher.Param("cluster_id", func(req *attunev1.GetClusterMembersRequest, id string) {
					req.ClusterId = id
				}),
				clusters.BindGetMembersQuery,
			),
			r.clusters.GetMembers,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetClusterMembersRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountTags(m chi.Router) {
	if r.tags == nil {
		return
	}
	m.Route("/tags", func(t chi.Router) {
		t.Get("/", dispatcher.Bind(
			"console.TagHandler.List",
			dispatcher.Query(
				func() *attunev1.ListTagsRequest { return ptrext.Of(attunev1.ListTagsRequest{}) },
				func(r *http.Request, req *attunev1.ListTagsRequest) error {
					if v := r.URL.Query().Get("include_archived"); v == "true" || v == "1" {
						req.IncludeArchived = true
					}
					return nil
				},
			),
			r.tags.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListTagsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		t.Post("/", dispatcher.Bind(
			"console.TagHandler.Create",
			dispatcher.JSON(func() *attunev1.CreateTagRequest { return ptrext.Of(attunev1.CreateTagRequest{}) }),
			r.tags.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		t.Patch("/{id}", dispatcher.Bind(
			"console.TagHandler.Update",
			dispatcher.Combine(
				func() *attunev1.UpdateTagRequest { return ptrext.Of(attunev1.UpdateTagRequest{}) },
				dispatcher.JSONBody[*attunev1.UpdateTagRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateTagRequest, id string) {
					req.Id = id
				}),
			),
			r.tags.Update,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		t.Delete("/{id}", dispatcher.Bind(
			"console.TagHandler.Archive",
			dispatcher.Path(
				func() *attunev1.ArchiveTagRequest { return ptrext.Of(attunev1.ArchiveTagRequest{}) },
				dispatcher.Param("id", func(req *attunev1.ArchiveTagRequest, id string) {
					req.Id = id
				}),
			),
			r.tags.Archive,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ArchiveTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountJobs(m chi.Router) {
	if r.feedbackJob == nil {
		return
	}
	m.Route("/jobs", func(j chi.Router) {
		j.Get("/", dispatcher.Bind(
			"console.feedbackjob.Handler.List",
			dispatcher.Query(
				func() *attunev1.ListJobsRequest { return ptrext.Of(attunev1.ListJobsRequest{}) },
				feedbackjob.BindListRequest,
			),
			r.feedbackJob.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListJobsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		j.Get("/{job_id}", dispatcher.Bind(
			"console.feedbackjob.Handler.GetStatus",
			dispatcher.Path(
				func() *attunev1.GetJobStatusRequest { return ptrext.Of(attunev1.GetJobStatusRequest{}) },
				dispatcher.Param("job_id", func(req *attunev1.GetJobStatusRequest, id string) {
					req.JobId = id
				}),
			),
			r.feedbackJob.GetStatus,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetJobStatusRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		j.Post("/{job_id}/cancel", dispatcher.Bind(
			"console.feedbackjob.Handler.Cancel",
			dispatcher.Path(
				func() *attunev1.CancelJobRequest { return ptrext.Of(attunev1.CancelJobRequest{}) },
				dispatcher.Param("job_id", func(req *attunev1.CancelJobRequest, id string) {
					req.JobId = id
				}),
			),
			r.feedbackJob.Cancel,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CancelJobRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountWorkflow(m chi.Router) {
	if r.workflow == nil {
		return
	}
	m.Route("/workflow", func(w chi.Router) {
		w.Use(r.requireAdmin)
		w.Get("/states", dispatcher.Bind(
			"console.WorkflowHandler.ListStates",
			dispatcher.Query(
				func() *attunev1.ListStatesRequest { return ptrext.Of(attunev1.ListStatesRequest{}) },
				func(r *http.Request, req *attunev1.ListStatesRequest) error {
					if v := r.URL.Query().Get("include_archived"); v == "true" || v == "1" {
						req.IncludeArchived = true
					}
					return nil
				},
			),
			r.workflow.ListStates,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListStatesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.Post("/states", dispatcher.Bind(
			"console.WorkflowHandler.CreateState",
			dispatcher.JSON(func() *attunev1.CreateStateRequest { return ptrext.Of(attunev1.CreateStateRequest{}) }),
			r.workflow.CreateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateStateRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.Patch("/states/{id}", dispatcher.Bind(
			"console.WorkflowHandler.UpdateState",
			dispatcher.Combine(
				func() *attunev1.UpdateStateRequest { return ptrext.Of(attunev1.UpdateStateRequest{}) },
				dispatcher.JSONBody[*attunev1.UpdateStateRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateStateRequest, id string) { req.Id = id }),
			),
			r.workflow.UpdateState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateStateRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.Delete("/states/{id}", dispatcher.Bind(
			"console.WorkflowHandler.ArchiveState",
			dispatcher.Path(
				func() *attunev1.ArchiveStateRequest { return ptrext.Of(attunev1.ArchiveStateRequest{}) },
				dispatcher.Param("id", func(req *attunev1.ArchiveStateRequest, id string) { req.Id = id }),
			),
			r.workflow.ArchiveState,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ArchiveStateRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.Get("/transitions", dispatcher.Bind(
			"console.WorkflowHandler.ListTransitions",
			dispatcher.Empty(func() *attunev1.ListTransitionsRequest {
				return ptrext.Of(attunev1.ListTransitionsRequest{})
			}),
			r.workflow.ListTransitions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListTransitionsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.Put("/transitions", dispatcher.Bind(
			"console.WorkflowHandler.ReplaceTransitions",
			dispatcher.JSON(func() *attunev1.ReplaceTransitionsRequest {
				return ptrext.Of(attunev1.ReplaceTransitionsRequest{})
			}),
			r.workflow.ReplaceTransitions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ReplaceTransitionsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		w.Post("/seed", dispatcher.Bind(
			"console.WorkflowHandler.SeedDefaults",
			dispatcher.Empty(func() *attunev1.SeedDefaultsRequest {
				return ptrext.Of(attunev1.SeedDefaultsRequest{})
			}),
			r.workflow.SeedDefaults,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SeedDefaultsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountOIDC(mux chi.Router) {
	if r.oidc == nil {
		return
	}
	mux.Route("/auth/oidc", func(o chi.Router) {
		o.Get("/start", r.oidc.Start)
		o.Get("/callback", r.oidc.Callback)
		o.Get("/health", r.oidc.Health)
	})
}

// authProviders returns available authentication methods for the login UI.
func (r *Router) authProviders(w http.ResponseWriter, req *http.Request) {
	type provider struct {
		Type string `json:"type"`
		Name string `json:"name,omitempty"`
	}
	type response struct {
		Providers []provider `json:"providers"`
		OIDCOnly  bool       `json:"oidc_only,omitempty"`
	}

	resp := response{Providers: []provider{}}

	// Admin login is always available unless oidc_only is set
	oidcOnly := false
	if r.oidc != nil {
		oidcOnly = r.oidc.OIDCOnly()
		resp.Providers = append(resp.Providers, provider{
			Type: "oidc",
			Name: r.oidc.ProviderName(),
		})
	}

	if !oidcOnly {
		resp.Providers = append(resp.Providers, provider{Type: "password"})
	}
	resp.OIDCOnly = oidcOnly

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
