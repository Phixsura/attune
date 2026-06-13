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
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/apikey"
	"github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/clusters"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	consoleguardpolicy "github.com/Phixsura/attune/internal/handlers/console/guardpolicy"
	consoleinbound "github.com/Phixsura/attune/internal/handlers/console/inbound"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consolellmconfig "github.com/Phixsura/attune/internal/handlers/console/llmconfig"
	"github.com/Phixsura/attune/internal/handlers/console/me"
	"github.com/Phixsura/attune/internal/handlers/console/notifytarget"
	"github.com/Phixsura/attune/internal/handlers/console/usage"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
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
	NewSigner                = session.NewSigner
	NewAuthHandler           = auth.NewHandler
	NewChangePasswordHandler = auth.NewChangePasswordHandler
	NewMeHandler             = me.NewMeHandler
	NewAPIKeysHandler        = apikey.NewAPIKeysHandler
	NewNotifyTargetsHandler  = notifytarget.NewNotifyTargetsHandler
	NewFeedbackHandler       = feedback.NewFeedbackHandler
	NewUsageHandler          = usage.NewUsageHandler
	NewEnrichConfigHandler   = enrichconfig.NewHandler
	NewGuardPolicyHandler    = consoleguardpolicy.NewHandler
	NewInboundHandler        = consoleinbound.NewHandler
	NewLLMConfigHandler      = consolellmconfig.NewHandler
	NewClustersHandler       = clusters.NewClustersHandler
	BootstrapAdmin           = auth.BootstrapAdmin
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
	signer         *session.Signer
	login          *auth.Handler
	changePassword *auth.ChangePasswordHandler
	me             *me.MeHandler
	apiKeys        *apikey.APIKeysHandler
	notifyTargets  *notifytarget.NotifyTargetsHandler
	feedback       *feedback.FeedbackHandler
	usage          *usage.UsageHandler
	enrichConfig   *enrichconfig.Handler
	guardPolicies  *consoleguardpolicy.Handler
	inbound        *consoleinbound.Handler
	llmConfig      *consolellmconfig.Handler
	clusters       *clusters.ClustersHandler
	admins         adminReader
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
	usage *usage.UsageHandler,
	enrichConfig *enrichconfig.Handler,
	guardPolicies *consoleguardpolicy.Handler,
	inbound *consoleinbound.Handler,
	llmConfig *consolellmconfig.Handler,
	clustersH *clusters.ClustersHandler,
	admins adminReader,
) *Router {
	return ptrext.Of(Router{
		signer:         signer,
		login:          authH,
		changePassword: changePassword,
		me:             me,
		apiKeys:        apiKeys,
		notifyTargets:  notifyTargets,
		feedback:       feedback,
		usage:          usage,
		enrichConfig:   enrichConfig,
		guardPolicies:  guardPolicies,
		inbound:        inbound,
		llmConfig:      llmConfig,
		clusters:       clustersH,
		admins:         admins,
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
	r.mountLLMConfig(m)
	r.mountClusters(m)
}

func (r *Router) mountLLMConfig(m chi.Router) {
	if r.llmConfig == nil {
		return
	}
	m.Route("/llm", func(l chi.Router) {
		l.Use(r.requireAdmin)
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
	const where = "console.Router.requireAdmin"
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
		// /stats must come BEFORE /{id}; source order keeps the intent clear.
		f.Get("/stats", dispatcher.Bind(
			"console.FeedbackHandler.Stats",
			dispatcher.Empty(func() *attunev1.GetFeedbackStatsRequest { return ptrext.Of(attunev1.GetFeedbackStatsRequest{}) }),
			r.feedback.Stats,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackStatsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
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
	})
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
