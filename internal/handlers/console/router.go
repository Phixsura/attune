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
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/apikey"
	consoleauditlog "github.com/Phixsura/attune/internal/handlers/console/auditlog"
	"github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/clusters"
	"github.com/Phixsura/attune/internal/handlers/console/digestsubscription"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	consoleenrichmentruntime "github.com/Phixsura/attune/internal/handlers/console/enrichmentruntime"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	"github.com/Phixsura/attune/internal/handlers/console/feedbackjob"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
	consoleguardpolicy "github.com/Phixsura/attune/internal/handlers/console/guardpolicy"
	consoleinbound "github.com/Phixsura/attune/internal/handlers/console/inbound"
	"github.com/Phixsura/attune/internal/handlers/console/internal/rbac"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consolellmconfig "github.com/Phixsura/attune/internal/handlers/console/llmconfig"
	"github.com/Phixsura/attune/internal/handlers/console/me"
	"github.com/Phixsura/attune/internal/handlers/console/member"
	"github.com/Phixsura/attune/internal/handlers/console/notifytarget"
	consoleoidc "github.com/Phixsura/attune/internal/handlers/console/oidc"
	consoleoutbox "github.com/Phixsura/attune/internal/handlers/console/outbox"
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
	NewAuditLogHandler           = consoleauditlog.NewHandler
	NewChangePasswordHandler     = auth.NewChangePasswordHandler
	NewMeHandler                 = me.NewMeHandler
	NewAPIKeysHandler            = apikey.NewAPIKeysHandler
	NewNotifyTargetsHandler      = notifytarget.NewNotifyTargetsHandler
	NewFeedbackHandler           = feedback.NewFeedbackHandler
	NewBatchHandler              = feedback.NewBatchHandler
	NewSearchHandler             = feedback.NewSearchHandler
	NewFeedbackJobHandler        = feedbackjob.NewHandler
	NewGDPRHandler               = consolegdpr.NewHandler
	NewUsageHandler              = usage.NewUsageHandler
	NewEnrichConfigHandler       = enrichconfig.NewHandler
	NewEnrichmentRuntimeHandler  = consoleenrichmentruntime.NewHandler
	NewGuardPolicyHandler        = consoleguardpolicy.NewHandler
	NewInboundHandler            = consoleinbound.NewHandler
	NewLLMConfigHandler          = consolellmconfig.NewHandler
	NewClustersHandler           = clusters.NewClustersHandler
	NewDigestSubscriptionHandler = digestsubscription.NewHandler
	NewOutboxHandler             = consoleoutbox.NewHandler
	NewTagHandler                = consoletag.NewHandler
	NewTagAssignmentHandler      = consoletagassignment.NewHandler
	NewWorkflowHandler           = consoleworkflow.NewHandler
	NewOIDCHandler               = consoleoidc.NewHandler
	NewMemberHandler             = member.NewHandler
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
	auditLog           *consoleauditlog.Handler
	apiKeys            *apikey.APIKeysHandler
	notifyTargets      *notifytarget.NotifyTargetsHandler
	feedback           *feedback.FeedbackHandler
	feedbackBatch      *feedback.BatchHandler
	feedbackSearch     *feedback.SearchHandler
	feedbackJob        *feedbackjob.Handler
	gdpr               *consolegdpr.Handler
	usage              *usage.UsageHandler
	enrichConfig       *enrichconfig.Handler
	enrichmentRuntime  *consoleenrichmentruntime.Handler
	guardPolicies      *consoleguardpolicy.Handler
	inbound            *consoleinbound.Handler
	llmConfig          *consolellmconfig.Handler
	clusters           *clusters.ClustersHandler
	digestSubscription *digestsubscription.Handler
	outbox             *consoleoutbox.Handler
	tags               *consoletag.Handler
	tagAssignments     *consoletagassignment.Handler
	workflow           *consoleworkflow.Handler
	oidc               *consoleoidc.Handler
	members            *member.Handler
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
	auditLog *consoleauditlog.Handler,
	apiKeys *apikey.APIKeysHandler,
	notifyTargets *notifytarget.NotifyTargetsHandler,
	feedback *feedback.FeedbackHandler,
	feedbackBatch *feedback.BatchHandler,
	feedbackSearch *feedback.SearchHandler,
	feedbackJob *feedbackjob.Handler,
	gdprHandler *consolegdpr.Handler,
	usage *usage.UsageHandler,
	enrichConfig *enrichconfig.Handler,
	enrichmentRuntime *consoleenrichmentruntime.Handler,
	guardPolicies *consoleguardpolicy.Handler,
	inbound *consoleinbound.Handler,
	llmConfig *consolellmconfig.Handler,
	clustersH *clusters.ClustersHandler,
	digestSubscription *digestsubscription.Handler,
	tags *consoletag.Handler,
	tagAssignments *consoletagassignment.Handler,
	workflow *consoleworkflow.Handler,
	oidc *consoleoidc.Handler,
	membersHandler *member.Handler,
	admins adminReader,
	membersRepo *tenantmember.Repo,
) *Router {
	var rbacMW *rbac.Middleware
	if membersRepo != nil {
		rbacMW = rbac.NewMiddleware(membersRepo)
	}
	return ptrext.Of(Router{
		signer:             signer,
		login:              authH,
		changePassword:     changePassword,
		me:                 me,
		auditLog:           auditLog,
		apiKeys:            apiKeys,
		notifyTargets:      notifyTargets,
		feedback:           feedback,
		feedbackBatch:      feedbackBatch,
		feedbackSearch:     feedbackSearch,
		feedbackJob:        feedbackJob,
		gdpr:               gdprHandler,
		usage:              usage,
		enrichConfig:       enrichConfig,
		enrichmentRuntime:  enrichmentRuntime,
		guardPolicies:      guardPolicies,
		inbound:            inbound,
		llmConfig:          llmConfig,
		clusters:           clustersH,
		digestSubscription: digestSubscription,
		tags:               tags,
		tagAssignments:     tagAssignments,
		workflow:           workflow,
		oidc:               oidc,
		members:            membersHandler,
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
	r.mountAuditLog(m)
	r.mountGDPR(m)
	r.mountNotifyTargets(m)
	r.mountOutbox(m)
	r.mountDigestSubscription(m)
	r.mountFeedback(m)
	m.Group(func(u chi.Router) {
		u.Use(r.requireViewer) // Usage stats visible to all roles
		u.Get("/usage", dispatcher.Bind(
			"console.UsageHandler.Get",
			dispatcher.Empty(func() *attunev1.GetUsageRequest { return ptrext.Of(attunev1.GetUsageRequest{}) }),
			r.usage.Get,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetUsageRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		u.Get("/llm-usage", dispatcher.Bind(
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
	})
	r.mountEnrichConfig(m)
	r.mountEnrichmentRuntime(m)
	r.mountGuardPolicies(m)
	r.mountInbound(m)
	r.mountJobs(m)
	r.mountLLMConfig(m)
	r.mountClusters(m)
	r.mountTags(m)
	r.mountWorkflow(m)
	r.mountMembers(m)
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
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.useRBACForRequest(req) {
			r.rbac.RequireAdmin()(next).ServeHTTP(w, req)
			return
		}
		r.requireAdminLegacy(next).ServeHTTP(w, req)
	})
}

func (r *Router) requireAdminStrict(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.useRBACForRequest(req) {
			r.rbac.RequireAdminStrict()(next).ServeHTTP(w, req)
			return
		}
		r.requireAdminLegacy(next).ServeHTTP(w, req)
	})
}

func (r *Router) requireViewer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.useRBACForRequest(req) {
			r.rbac.RequireViewer()(next).ServeHTTP(w, req)
			return
		}
		if r.admins != nil {
			r.requireAdminLegacy(next).ServeHTTP(w, req)
			return
		}
		// Legacy fallback: all authenticated users pass (viewer is baseline)
		next.ServeHTTP(w, req)
	})
}

func (r *Router) useRBACForRequest(req *http.Request) bool {
	if r.rbac == nil {
		return false
	}
	auth := session.FromContext(req.Context())
	if auth == nil {
		return false
	}
	return auth.TenantID != "" && auth.UserType != ""
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
		next.ServeHTTP(w, req.WithContext(rbac.WithRole(req.Context(), domain.RoleAdmin)))
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
		k.Use(r.requireAdminStrict)
		r.mountAPIKeyCoreRoutes(k)
		r.mountAPIKeyAdvancedRoutes(k)
		r.mountAPIKeyPerKeyRoutes(k)
	})
	r.mountAPIKeyRelatedResources(m)
}

func (r *Router) mountAPIKeyCoreRoutes(k chi.Router) {
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
	k.Get("/scopes", dispatcher.Bind(
		"console.APIKeysHandler.ListScopes",
		dispatcher.Empty(func() *attunev1.ListScopesRequest { return ptrext.Of(attunev1.ListScopesRequest{}) }),
		r.apiKeys.ListScopes,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListScopesRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Get("/presets", dispatcher.Bind(
		"console.APIKeysHandler.ListScopePresets",
		dispatcher.Empty(func() *attunev1.ListScopePresetsRequest { return ptrext.Of(attunev1.ListScopePresetsRequest{}) }),
		r.apiKeys.ListScopePresets,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListScopePresetsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Get("/expiring", dispatcher.Bind(
		"console.APIKeysHandler.ListExpiring",
		dispatcher.Query(
			func() *attunev1.ListExpiringApiKeysRequest { return ptrext.Of(attunev1.ListExpiringApiKeysRequest{}) },
			func(r *http.Request, req *attunev1.ListExpiringApiKeysRequest) error {
				if w := r.URL.Query().Get("within"); w != "" {
					req.Within = ptrext.Of(w)
				}
				return nil
			},
		),
		r.apiKeys.ListExpiring,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListExpiringApiKeysRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Get("/event-subscriptions", dispatcher.Bind(
		"console.APIKeysHandler.ListEventSubscriptions",
		dispatcher.Empty(func() *attunev1.ListEventSubscriptionsRequest {
			return ptrext.Of(attunev1.ListEventSubscriptionsRequest{})
		}),
		r.apiKeys.ListEventSubscriptions,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListEventSubscriptionsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Post("/event-subscriptions", dispatcher.Bind(
		"console.APIKeysHandler.CreateEventSubscription",
		dispatcher.JSON(func() *attunev1.CreateEventSubscriptionRequest {
			return ptrext.Of(attunev1.CreateEventSubscriptionRequest{})
		}),
		r.apiKeys.CreateEventSubscription,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateEventSubscriptionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Get("/leaks", dispatcher.Bind(
		"console.APIKeysHandler.ListLeakDetections",
		dispatcher.Empty(func() *attunev1.ListLeakDetectionsRequest { return ptrext.Of(attunev1.ListLeakDetectionsRequest{}) }),
		r.apiKeys.ListLeakDetections,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListLeakDetectionsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountAPIKeyAdvancedRoutes(k chi.Router) {
	k.Post("/{id}/rotate", dispatcher.Bind(
		"console.APIKeysHandler.Rotate",
		dispatcher.Combine(
			func() *attunev1.RotateApiKeyRequest { return ptrext.Of(attunev1.RotateApiKeyRequest{}) },
			dispatcher.JSONBody[*attunev1.RotateApiKeyRequest],
			dispatcher.Param("id", func(req *attunev1.RotateApiKeyRequest, id string) { req.Id = id }),
		),
		r.apiKeys.Rotate,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RotateApiKeyRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Get("/{id}/logs", dispatcher.Bind(
		"console.APIKeysHandler.ListLogs",
		dispatcher.Query(
			func() *attunev1.ListApiKeyLogsRequest { return ptrext.Of(attunev1.ListApiKeyLogsRequest{}) },
			func(r *http.Request, req *attunev1.ListApiKeyLogsRequest) error {
				req.Id = chi.URLParam(r, "id")
				if lim := r.URL.Query().Get("limit"); lim != "" {
					var limit int32
					if _, err := fmt.Sscanf(lim, "%d", &limit); err == nil {
						req.Limit = ptrext.Of(limit)
					}
				}
				return nil
			},
		),
		r.apiKeys.ListLogs,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListApiKeyLogsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Patch("/{id}/environment", dispatcher.Bind(
		"console.APIKeysHandler.UpdateEnvironment",
		dispatcher.Combine(
			func() *attunev1.UpdateApiKeyEnvironmentRequest {
				return ptrext.Of(attunev1.UpdateApiKeyEnvironmentRequest{})
			},
			dispatcher.JSONBody[*attunev1.UpdateApiKeyEnvironmentRequest],
			dispatcher.Param("id", func(req *attunev1.UpdateApiKeyEnvironmentRequest, id string) { req.Id = id }),
		),
		r.apiKeys.UpdateEnvironment,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateApiKeyEnvironmentRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	// Policy
	k.Get("/policy", dispatcher.Bind(
		"console.APIKeysHandler.GetPolicy",
		dispatcher.Empty(func() *attunev1.GetPolicyRequest { return ptrext.Of(attunev1.GetPolicyRequest{}) }),
		r.apiKeys.GetPolicy,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetPolicyRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Put("/policy", dispatcher.Bind(
		"console.APIKeysHandler.UpdatePolicy",
		dispatcher.JSON(func() *attunev1.UpdatePolicyRequest { return ptrext.Of(attunev1.UpdatePolicyRequest{}) }),
		r.apiKeys.UpdatePolicy,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdatePolicyRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	// Approvals
	k.Get("/approvals", dispatcher.Bind(
		"console.APIKeysHandler.ListApprovalRequests",
		dispatcher.Query(
			func() *attunev1.ListApprovalRequestsRequest { return ptrext.Of(attunev1.ListApprovalRequestsRequest{}) },
			func(r *http.Request, req *attunev1.ListApprovalRequestsRequest) error {
				if s := r.URL.Query().Get("status"); s != "" {
					req.Status = ptrext.Of(s)
				}
				return nil
			},
		),
		r.apiKeys.ListApprovalRequests,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListApprovalRequestsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Post("/approvals", dispatcher.Bind(
		"console.APIKeysHandler.CreateApprovalRequest",
		dispatcher.JSON(func() *attunev1.CreateApprovalRequestRequest {
			return ptrext.Of(attunev1.CreateApprovalRequestRequest{})
		}),
		r.apiKeys.CreateApprovalRequest,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateApprovalRequestRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Post("/approvals/{id}/review", dispatcher.Bind(
		"console.APIKeysHandler.ReviewApproval",
		dispatcher.Combine(
			func() *attunev1.ReviewApprovalRequest { return ptrext.Of(attunev1.ReviewApprovalRequest{}) },
			dispatcher.JSONBody[*attunev1.ReviewApprovalRequest],
			dispatcher.Param("id", func(req *attunev1.ReviewApprovalRequest, id string) { req.Id = id }),
		),
		r.apiKeys.ReviewApproval,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ReviewApprovalRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	// Analytics
	k.Get("/analytics", dispatcher.Bind(
		"console.APIKeysHandler.GetTenantAnalytics",
		dispatcher.Query(
			func() *attunev1.GetTenantAnalyticsRequest { return ptrext.Of(attunev1.GetTenantAnalyticsRequest{}) },
			func(r *http.Request, req *attunev1.GetTenantAnalyticsRequest) error {
				if s := r.URL.Query().Get("start"); s != "" {
					req.Start = ptrext.Of(s)
				}
				if e := r.URL.Query().Get("end"); e != "" {
					req.End = ptrext.Of(e)
				}
				return nil
			},
		),
		r.apiKeys.GetTenantAnalytics,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetTenantAnalyticsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountAPIKeyPerKeyRoutes(k chi.Router) {
	r.mountAPIKeyPerKeyBasicRoutes(k)
	r.mountAPIKeyPerKeySecurityRoutes(k)
}

func (r *Router) mountAPIKeyPerKeyBasicRoutes(k chi.Router) {
	k.Get("/{id}/tags", dispatcher.Bind(
		"console.APIKeysHandler.GetKeyTags",
		dispatcher.Path(
			func() *attunev1.GetKeyTagsRequest { return ptrext.Of(attunev1.GetKeyTagsRequest{}) },
			dispatcher.Param("id", func(req *attunev1.GetKeyTagsRequest, id string) { req.Id = id }),
		),
		r.apiKeys.GetKeyTags,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetKeyTagsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Put("/{id}/tags", dispatcher.Bind(
		"console.APIKeysHandler.SetKeyTags",
		dispatcher.Combine(
			func() *attunev1.SetKeyTagsRequest { return ptrext.Of(attunev1.SetKeyTagsRequest{}) },
			dispatcher.JSONBody[*attunev1.SetKeyTagsRequest],
			dispatcher.Param("id", func(req *attunev1.SetKeyTagsRequest, id string) { req.Id = id }),
		),
		r.apiKeys.SetKeyTags,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SetKeyTagsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Put("/{id}/budget", dispatcher.Bind(
		"console.APIKeysHandler.SetKeyBudget",
		dispatcher.Combine(
			func() *attunev1.SetKeyBudgetRequest { return ptrext.Of(attunev1.SetKeyBudgetRequest{}) },
			dispatcher.JSONBody[*attunev1.SetKeyBudgetRequest],
			dispatcher.Param("id", func(req *attunev1.SetKeyBudgetRequest, id string) { req.Id = id }),
		),
		r.apiKeys.SetKeyBudget,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SetKeyBudgetRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Post("/{id}/temp-token", dispatcher.Bind(
		"console.APIKeysHandler.CreateTempToken",
		dispatcher.Combine(
			func() *attunev1.CreateTempTokenRequest { return ptrext.Of(attunev1.CreateTempTokenRequest{}) },
			dispatcher.JSONBody[*attunev1.CreateTempTokenRequest],
			dispatcher.Param("id", func(req *attunev1.CreateTempTokenRequest, id string) { req.Id = id }),
		),
		r.apiKeys.CreateTempToken,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateTempTokenRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Post("/{id}/project", dispatcher.Bind(
		"console.APIKeysHandler.BindKeyToProject",
		dispatcher.Combine(
			func() *attunev1.BindKeyToProjectRequest { return ptrext.Of(attunev1.BindKeyToProjectRequest{}) },
			dispatcher.JSONBody[*attunev1.BindKeyToProjectRequest],
			dispatcher.Param("id", func(req *attunev1.BindKeyToProjectRequest, id string) { req.Id = id }),
		),
		r.apiKeys.BindKeyToProject,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.BindKeyToProjectRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Get("/{id}/analytics", dispatcher.Bind(
		"console.APIKeysHandler.GetKeyAnalytics",
		dispatcher.Query(
			func() *attunev1.GetKeyAnalyticsRequest { return ptrext.Of(attunev1.GetKeyAnalyticsRequest{}) },
			func(r *http.Request, req *attunev1.GetKeyAnalyticsRequest) error {
				req.Id = chi.URLParam(r, "id")
				if s := r.URL.Query().Get("start"); s != "" {
					req.Start = ptrext.Of(s)
				}
				if e := r.URL.Query().Get("end"); e != "" {
					req.End = ptrext.Of(e)
				}
				return nil
			},
		),
		r.apiKeys.GetKeyAnalytics,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetKeyAnalyticsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountAPIKeyPerKeySecurityRoutes(k chi.Router) {
	k.Get("/{id}/rotation-schedule", dispatcher.Bind(
		"console.APIKeysHandler.GetRotationSchedule",
		dispatcher.Path(
			func() *attunev1.GetRotationScheduleRequest { return ptrext.Of(attunev1.GetRotationScheduleRequest{}) },
			dispatcher.Param("id", func(req *attunev1.GetRotationScheduleRequest, id string) { req.Id = id }),
		),
		r.apiKeys.GetRotationSchedule,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetRotationScheduleRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Post("/{id}/rotation-schedule", dispatcher.Bind(
		"console.APIKeysHandler.CreateRotationSchedule",
		dispatcher.Combine(
			func() *attunev1.CreateRotationScheduleRequest {
				return ptrext.Of(attunev1.CreateRotationScheduleRequest{})
			},
			dispatcher.JSONBody[*attunev1.CreateRotationScheduleRequest],
			dispatcher.Param("id", func(req *attunev1.CreateRotationScheduleRequest, id string) { req.Id = id }),
		),
		r.apiKeys.CreateRotationSchedule,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateRotationScheduleRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Get("/{id}/unused-scopes", dispatcher.Bind(
		"console.APIKeysHandler.GetUnusedScopes",
		dispatcher.Path(
			func() *attunev1.GetUnusedScopesRequest { return ptrext.Of(attunev1.GetUnusedScopesRequest{}) },
			dispatcher.Param("id", func(req *attunev1.GetUnusedScopesRequest, id string) { req.Id = id }),
		),
		r.apiKeys.GetUnusedScopes,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetUnusedScopesRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Get("/{id}/signing-keys", dispatcher.Bind(
		"console.APIKeysHandler.ListSigningKeys",
		dispatcher.Path(
			func() *attunev1.ListSigningKeysRequest { return ptrext.Of(attunev1.ListSigningKeysRequest{}) },
			dispatcher.Param("id", func(req *attunev1.ListSigningKeysRequest, id string) { req.Id = id }),
		),
		r.apiKeys.ListSigningKeys,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListSigningKeysRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Post("/{id}/signing-keys", dispatcher.Bind(
		"console.APIKeysHandler.CreateSigningKey",
		dispatcher.Combine(
			func() *attunev1.CreateSigningKeyRequest { return ptrext.Of(attunev1.CreateSigningKeyRequest{}) },
			dispatcher.JSONBody[*attunev1.CreateSigningKeyRequest],
			dispatcher.Param("id", func(req *attunev1.CreateSigningKeyRequest, id string) { req.Id = id }),
		),
		r.apiKeys.CreateSigningKey,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateSigningKeyRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	k.Get("/{id}/health", dispatcher.Bind(
		"console.APIKeysHandler.GetKeyHealth",
		dispatcher.Path(
			func() *attunev1.GetKeyHealthRequest { return ptrext.Of(attunev1.GetKeyHealthRequest{}) },
			dispatcher.Param("id", func(req *attunev1.GetKeyHealthRequest, id string) { req.Id = id }),
		),
		r.apiKeys.GetKeyHealth,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetKeyHealthRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountAPIKeyRelatedResources(m chi.Router) {
	m.Route("/service-accounts", func(s chi.Router) {
		s.Use(r.requireAdminStrict)
		s.Get("/", dispatcher.Bind(
			"console.APIKeysHandler.ListServiceAccounts",
			dispatcher.Empty(func() *attunev1.ListServiceAccountsRequest { return ptrext.Of(attunev1.ListServiceAccountsRequest{}) }),
			r.apiKeys.ListServiceAccounts,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListServiceAccountsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		s.Post("/", dispatcher.Bind(
			"console.APIKeysHandler.CreateServiceAccount",
			dispatcher.JSON(func() *attunev1.CreateServiceAccountRequest { return ptrext.Of(attunev1.CreateServiceAccountRequest{}) }),
			r.apiKeys.CreateServiceAccount,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateServiceAccountRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})

	m.Route("/projects", func(p chi.Router) {
		p.Use(r.requireAdminStrict)
		p.Get("/", dispatcher.Bind(
			"console.APIKeysHandler.ListProjects",
			dispatcher.Empty(func() *attunev1.ListProjectsRequest { return ptrext.Of(attunev1.ListProjectsRequest{}) }),
			r.apiKeys.ListProjects,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListProjectsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		p.Post("/", dispatcher.Bind(
			"console.APIKeysHandler.CreateProject",
			dispatcher.JSON(func() *attunev1.CreateProjectRequest { return ptrext.Of(attunev1.CreateProjectRequest{}) }),
			r.apiKeys.CreateProject,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateProjectRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})

	m.Route("/oauth2/clients", func(o chi.Router) {
		o.Use(r.requireAdminStrict)
		o.Get("/", dispatcher.Bind(
			"console.APIKeysHandler.ListOAuth2Clients",
			dispatcher.Empty(func() *attunev1.ListOAuth2ClientsRequest { return ptrext.Of(attunev1.ListOAuth2ClientsRequest{}) }),
			r.apiKeys.ListOAuth2Clients,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListOAuth2ClientsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		o.Post("/", dispatcher.Bind(
			"console.APIKeysHandler.CreateOAuth2Client",
			dispatcher.JSON(func() *attunev1.CreateOAuth2ClientRequest { return ptrext.Of(attunev1.CreateOAuth2ClientRequest{}) }),
			r.apiKeys.CreateOAuth2Client,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateOAuth2ClientRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})

	m.Route("/secret-managers", func(s chi.Router) {
		s.Use(r.requireAdminStrict)
		s.Get("/", dispatcher.Bind(
			"console.APIKeysHandler.ListSecretManagers",
			dispatcher.Empty(func() *attunev1.ListSecretManagersRequest { return ptrext.Of(attunev1.ListSecretManagersRequest{}) }),
			r.apiKeys.ListSecretManagers,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListSecretManagersRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		s.Post("/", dispatcher.Bind(
			"console.APIKeysHandler.CreateSecretManager",
			dispatcher.JSON(func() *attunev1.CreateSecretManagerRequest { return ptrext.Of(attunev1.CreateSecretManagerRequest{}) }),
			r.apiKeys.CreateSecretManager,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateSecretManagerRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})

	m.Route("/managed-identities", func(mi chi.Router) {
		mi.Use(r.requireAdminStrict)
		mi.Get("/", dispatcher.Bind(
			"console.APIKeysHandler.ListManagedIdentities",
			dispatcher.Empty(func() *attunev1.ListManagedIdentitiesRequest {
				return ptrext.Of(attunev1.ListManagedIdentitiesRequest{})
			}),
			r.apiKeys.ListManagedIdentities,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListManagedIdentitiesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		mi.Post("/", dispatcher.Bind(
			"console.APIKeysHandler.CreateManagedIdentity",
			dispatcher.JSON(func() *attunev1.CreateManagedIdentityRequest {
				return ptrext.Of(attunev1.CreateManagedIdentityRequest{})
			}),
			r.apiKeys.CreateManagedIdentity,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateManagedIdentityRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})

	m.Route("/siem-integrations", func(si chi.Router) {
		si.Use(r.requireAdminStrict)
		si.Get("/", dispatcher.Bind(
			"console.APIKeysHandler.ListSIEMIntegrations",
			dispatcher.Empty(func() *attunev1.ListSIEMIntegrationsRequest { return ptrext.Of(attunev1.ListSIEMIntegrationsRequest{}) }),
			r.apiKeys.ListSIEMIntegrations,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListSIEMIntegrationsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		si.Post("/", dispatcher.Bind(
			"console.APIKeysHandler.CreateSIEMIntegration",
			dispatcher.JSON(func() *attunev1.CreateSIEMIntegrationRequest {
				return ptrext.Of(attunev1.CreateSIEMIntegrationRequest{})
			}),
			r.apiKeys.CreateSIEMIntegration,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateSIEMIntegrationRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})

	m.Route("/ai-agents", func(ai chi.Router) {
		ai.Use(r.requireAdminStrict)
		ai.Get("/", dispatcher.Bind(
			"console.APIKeysHandler.ListAIAgentConfigs",
			dispatcher.Empty(func() *attunev1.ListAIAgentConfigsRequest { return ptrext.Of(attunev1.ListAIAgentConfigsRequest{}) }),
			r.apiKeys.ListAIAgentConfigs,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListAIAgentConfigsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		ai.Post("/", dispatcher.Bind(
			"console.APIKeysHandler.CreateAIAgentConfig",
			dispatcher.JSON(func() *attunev1.CreateAIAgentConfigRequest { return ptrext.Of(attunev1.CreateAIAgentConfigRequest{}) }),
			r.apiKeys.CreateAIAgentConfig,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateAIAgentConfigRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountAuditLog(m chi.Router) {
	if r.auditLog == nil {
		return
	}
	m.Route("/audit-log", func(a chi.Router) {
		a.Use(r.requireAdminStrict)
		a.Get("/", dispatcher.Bind(
			"console.auditlog.List",
			dispatcher.Query(
				func() *attunev1.ListAuditLogRequest { return ptrext.Of(attunev1.ListAuditLogRequest{}) },
				consoleauditlog.BindListRequest,
			),
			r.auditLog.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListAuditLogRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		a.Get("/export.csv", r.auditLog.ExportCSV)
	})
}

func (r *Router) mountGDPR(m chi.Router) {
	if r.gdpr == nil {
		return
	}
	m.Route("/gdpr", func(g chi.Router) {
		g.Use(r.requireAdminStrict)
		g.Get("/requests", dispatcher.Bind(
			"console.gdpr.ListRequests",
			dispatcher.Combine(
				func() *attunev1.ListGdprRequestsRequest { return ptrext.Of(attunev1.ListGdprRequestsRequest{}) },
				consolegdpr.BindListRequests,
			),
			r.gdpr.ListRequests,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListGdprRequestsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Get("/operations", dispatcher.Bind(
			"console.gdpr.GetOperations",
			dispatcher.Empty(func() *attunev1.GetGdprOperationsRequest {
				return ptrext.Of(attunev1.GetGdprOperationsRequest{})
			}),
			r.gdpr.GetOperations,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetGdprOperationsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Post("/step-up/verify", dispatcher.Bind(
			"console.gdpr.VerifyStepUp",
			dispatcher.JSON(func() *attunev1.VerifyGdprStepUpRequest {
				return ptrext.Of(attunev1.VerifyGdprStepUpRequest{})
			}),
			r.gdpr.VerifyStepUp,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.VerifyGdprStepUpRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Post("/requests/{request_id}/cancel", dispatcher.Bind(
			"console.gdpr.CancelRequest",
			dispatcher.Empty(func() *attunev1.CancelGdprRequestRequest {
				return ptrext.Of(attunev1.CancelGdprRequestRequest{})
			}),
			r.gdpr.CancelRequest,
			dispatcher.WithBinders(
				dispatcher.Param("request_id", func(req *attunev1.CancelGdprRequestRequest, id string) {
					req.RequestId = id
				}),
			),
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CancelGdprRequestRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Post("/export", dispatcher.Bind(
			"console.gdpr.Export",
			dispatcher.JSON(func() *attunev1.ExportGdprSubjectRequest {
				return ptrext.Of(attunev1.ExportGdprSubjectRequest{})
			}),
			r.gdpr.Export,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ExportGdprSubjectRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Get("/exports/{job_id}", dispatcher.Bind(
			"console.gdpr.GetExport",
			dispatcher.Empty(func() *attunev1.GetGdprExportRequest { return ptrext.Of(attunev1.GetGdprExportRequest{}) }),
			r.gdpr.GetExport,
			dispatcher.WithBinders(
				dispatcher.Param("job_id", func(req *attunev1.GetGdprExportRequest, id string) {
					req.JobId = id
				}),
			),
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetGdprExportRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Get("/exports/{job_id}/download", dispatcher.Bind(
			"console.gdpr.DownloadExport",
			dispatcher.Empty(func() *attunev1.DownloadGdprExportRequest {
				return ptrext.Of(attunev1.DownloadGdprExportRequest{})
			}),
			r.gdpr.DownloadExport,
			dispatcher.WithBinders(
				dispatcher.Param("job_id", func(req *attunev1.DownloadGdprExportRequest, id string) {
					req.JobId = id
				}),
			),
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DownloadGdprExportRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Post("/exports/{job_id}/revoke", dispatcher.Bind(
			"console.gdpr.RevokeExport",
			dispatcher.Empty(func() *attunev1.RevokeGdprExportRequest {
				return ptrext.Of(attunev1.RevokeGdprExportRequest{})
			}),
			r.gdpr.RevokeExport,
			dispatcher.WithBinders(
				dispatcher.Param("job_id", func(req *attunev1.RevokeGdprExportRequest, id string) {
					req.JobId = id
				}),
			),
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RevokeGdprExportRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		g.Post("/delete", dispatcher.Bind(
			"console.gdpr.Delete",
			dispatcher.JSON(func() *attunev1.DeleteGdprSubjectRequest {
				return ptrext.Of(attunev1.DeleteGdprSubjectRequest{})
			}),
			r.gdpr.Delete,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteGdprSubjectRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountNotifyTargets(m chi.Router) {
	m.Route("/notify-targets", func(n chi.Router) {
		n.Use(r.requireAdmin) // Notify targets are admin-only
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
	m.Group(func(d chi.Router) {
		d.Use(r.requireAdmin) // Digest subscription is admin-only
		d.Get("/digest-subscription", dispatcher.Bind(
			"console.DigestSubscriptionHandler.Get",
			dispatcher.Empty(func() *attunev1.GetDigestSubscriptionRequest {
				return ptrext.Of(attunev1.GetDigestSubscriptionRequest{})
			}),
			r.digestSubscription.Get,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetDigestSubscriptionRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		d.Put("/digest-subscription", dispatcher.Bind(
			"console.DigestSubscriptionHandler.Upsert",
			dispatcher.JSON(func() *attunev1.UpsertDigestSubscriptionRequest {
				return ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{})
			}),
			r.digestSubscription.Upsert,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpsertDigestSubscriptionRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		d.Delete("/digest-subscription", dispatcher.Bind(
			"console.DigestSubscriptionHandler.Delete",
			dispatcher.Empty(func() *attunev1.DeleteDigestSubscriptionRequest {
				return ptrext.Of(attunev1.DeleteDigestSubscriptionRequest{})
			}),
			r.digestSubscription.Delete,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteDigestSubscriptionRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountFeedback(m chi.Router) {
	m.Route("/feedback", func(f chi.Router) {
		f.Use(r.requireViewer) // Feedback visible to all roles; write ops check policy in handlers
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
		e.Use(r.requireAdmin) // AI classification config is admin-only
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

func (r *Router) mountEnrichmentRuntime(m chi.Router) {
	if r.enrichmentRuntime == nil {
		return
	}
	adminOnly := r.requireAdmin
	m.Route("/enrichment-runtime", func(e chi.Router) {
		e.Use(adminOnly)
		e.Get("/", dispatcher.Bind(
			"console.EnrichmentRuntimeHandler.Get",
			dispatcher.Empty(func() *attunev1.GetEnrichmentRuntimeRequest { return ptrext.Of(attunev1.GetEnrichmentRuntimeRequest{}) }),
			r.enrichmentRuntime.Get,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetEnrichmentRuntimeRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.Put("/", dispatcher.Bind(
			"console.EnrichmentRuntimeHandler.Update",
			dispatcher.JSON(func() *attunev1.UpdateEnrichmentRuntimeRequest {
				return ptrext.Of(attunev1.UpdateEnrichmentRuntimeRequest{})
			}),
			r.enrichmentRuntime.Update,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateEnrichmentRuntimeRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.Post("/reset", dispatcher.Bind(
			"console.EnrichmentRuntimeHandler.Reset",
			dispatcher.JSON(func() *attunev1.ResetEnrichmentRuntimeRequest {
				return ptrext.Of(attunev1.ResetEnrichmentRuntimeRequest{})
			}),
			r.enrichmentRuntime.Reset,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ResetEnrichmentRuntimeRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.Post("/rollback", dispatcher.Bind(
			"console.EnrichmentRuntimeHandler.Rollback",
			dispatcher.JSON(func() *attunev1.RollbackEnrichmentRuntimeRequest {
				return ptrext.Of(attunev1.RollbackEnrichmentRuntimeRequest{})
			}),
			r.enrichmentRuntime.Rollback,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RollbackEnrichmentRuntimeRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
	m.With(adminOnly).Post("/enrichment-runtime:reset", dispatcher.Bind(
		"console.EnrichmentRuntimeHandler.ResetLegacy",
		dispatcher.JSON(func() *attunev1.ResetEnrichmentRuntimeRequest {
			return ptrext.Of(attunev1.ResetEnrichmentRuntimeRequest{})
		}),
		r.enrichmentRuntime.Reset,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ResetEnrichmentRuntimeRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.With(adminOnly).Post("/enrichment-runtime:rollback", dispatcher.Bind(
		"console.EnrichmentRuntimeHandler.RollbackLegacy",
		dispatcher.JSON(func() *attunev1.RollbackEnrichmentRuntimeRequest {
			return ptrext.Of(attunev1.RollbackEnrichmentRuntimeRequest{})
		}),
		r.enrichmentRuntime.Rollback,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RollbackEnrichmentRuntimeRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountGuardPolicies(m chi.Router) {
	m.Route("/guard-policies", func(g chi.Router) {
		g.Use(r.requireAdmin) // Guard policies are admin-only
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
		s.Use(r.requireAdmin) // Inbound sources are admin-only
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
		c.Use(r.requireViewer) // Clusters are read-only, visible to all roles
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

// SetOutboxHandler injects the notify dead-queue handler (#33). Optional, so
// callers of NewRouter that don't wire it (tests) simply don't expose /outbox.
func (r *Router) SetOutboxHandler(h *consoleoutbox.Handler) {
	r.outbox = h
}

func (r *Router) mountOutbox(m chi.Router) {
	if r.outbox == nil {
		return
	}
	m.Route("/outbox", func(o chi.Router) {
		o.Use(r.requireAdmin) // dead-queue inspection + manual retry is admin-only
		o.Get("/deliveries", dispatcher.Bind(
			"console.OutboxHandler.List",
			dispatcher.Query(
				func() *attunev1.ListDeliveriesRequest { return ptrext.Of(attunev1.ListDeliveriesRequest{}) },
				bindListDeliveriesRequest,
			),
			r.outbox.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListDeliveriesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		o.Post("/{id}/retry", dispatcher.Bind(
			"console.OutboxHandler.Retry",
			dispatcher.Combine(
				func() *attunev1.RetryDeliveryRequest { return ptrext.Of(attunev1.RetryDeliveryRequest{}) },
				dispatcher.ParamInt64("id", func(req *attunev1.RetryDeliveryRequest, id int64) {
					req.Id = id
				}, "invalid delivery id"),
			),
			r.outbox.Retry,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RetryDeliveryRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

// bindListDeliveriesRequest fills the list request from query params:
// ?status=dead&status=failed&limit=50&before_id=123
func bindListDeliveriesRequest(r *http.Request, req *attunev1.ListDeliveriesRequest) error {
	q := r.URL.Query()
	req.Status = q["status"]
	if v := q.Get("limit"); v != "" {
		// ParseInt with bitSize=32 rejects values that don't fit int32, so the
		// conversion below can't overflow (the repo clamps the value anyway).
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid limit")
		}
		req.Limit = int32(n)
	}
	if v := q.Get("before_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid before_id")
		}
		req.BeforeId = n
	}
	return nil
}

func (r *Router) mountTags(m chi.Router) {
	if r.tags == nil {
		return
	}
	m.Route("/tags", func(t chi.Router) {
		t.Use(r.requireAdmin) // Tags config is admin-only
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
		j.Use(r.requireAdmin) // Jobs are admin-only
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

func (r *Router) mountMembers(m chi.Router) {
	if r.members == nil {
		return
	}
	m.Route("/members", func(mb chi.Router) {
		// GET is viewer+ (all authenticated users can see member list)
		mb.With(r.requireViewer).Get("/", dispatcher.Bind(
			"console.MemberHandler.List",
			dispatcher.Empty(func() *attunev1.ListMembersRequest {
				return ptrext.Of(attunev1.ListMembersRequest{})
			}),
			r.members.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListMembersRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		// Mutations require admin (strict = bypass cache)
		mb.With(r.requireAdminStrict).Post("/", dispatcher.Bind(
			"console.MemberHandler.Invite",
			dispatcher.JSON(func() *attunev1.InviteMemberRequest {
				return ptrext.Of(attunev1.InviteMemberRequest{})
			}),
			r.members.Invite,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.InviteMemberRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		mb.With(r.requireAdminStrict).Patch("/{id}", dispatcher.Bind(
			"console.MemberHandler.UpdateRole",
			dispatcher.Combine(
				func() *attunev1.UpdateMemberRoleRequest {
					return ptrext.Of(attunev1.UpdateMemberRoleRequest{})
				},
				dispatcher.JSONBody[*attunev1.UpdateMemberRoleRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateMemberRoleRequest, id string) {
					req.Id = id
				}),
			),
			r.members.UpdateRole,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateMemberRoleRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		mb.With(r.requireAdminStrict).Delete("/{id}", dispatcher.Bind(
			"console.MemberHandler.Remove",
			dispatcher.Path(
				func() *attunev1.RemoveMemberRequest {
					return ptrext.Of(attunev1.RemoveMemberRequest{})
				},
				dispatcher.Param("id", func(req *attunev1.RemoveMemberRequest, id string) {
					req.Id = id
				}),
			),
			r.members.Remove,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RemoveMemberRequest) (*session.AuthCtx, error) {
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
