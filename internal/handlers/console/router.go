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
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/apikey"
	consoleauditevidence "github.com/Phixsura/attune/internal/handlers/console/auditevidence"
	consoleauditlog "github.com/Phixsura/attune/internal/handlers/console/auditlog"
	"github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/clusters"
	consolecustomerrequest "github.com/Phixsura/attune/internal/handlers/console/customerrequest"
	"github.com/Phixsura/attune/internal/handlers/console/digestsubscription"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	consoleenrichmentruntime "github.com/Phixsura/attune/internal/handlers/console/enrichmentruntime"
	consoleexternalsync "github.com/Phixsura/attune/internal/handlers/console/externalsync"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	"github.com/Phixsura/attune/internal/handlers/console/feedbackjob"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
	consoleguardpolicy "github.com/Phixsura/attune/internal/handlers/console/guardpolicy"
	consoleinbound "github.com/Phixsura/attune/internal/handlers/console/inbound"
	"github.com/Phixsura/attune/internal/handlers/console/internal/rbac"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consolellmconfig "github.com/Phixsura/attune/internal/handlers/console/llmconfig"
	consolemcpclient "github.com/Phixsura/attune/internal/handlers/console/mcpclient"
	"github.com/Phixsura/attune/internal/handlers/console/me"
	"github.com/Phixsura/attune/internal/handlers/console/member"
	"github.com/Phixsura/attune/internal/handlers/console/notifytarget"
	consoleoidc "github.com/Phixsura/attune/internal/handlers/console/oidc"
	consoleoutbox "github.com/Phixsura/attune/internal/handlers/console/outbox"
	consolepublicvisibility "github.com/Phixsura/attune/internal/handlers/console/publicvisibility"
	consoletag "github.com/Phixsura/attune/internal/handlers/console/tag"
	consoletagassignment "github.com/Phixsura/attune/internal/handlers/console/tagassignment"
	"github.com/Phixsura/attune/internal/handlers/console/usage"
	consoleworkflow "github.com/Phixsura/attune/internal/handlers/console/workflow"
	"github.com/Phixsura/attune/internal/pkg/httputil"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
	breakglasssvc "github.com/Phixsura/attune/internal/service/breakglass"
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
	NewAuditEvidenceHandler      = consoleauditevidence.NewHandler
	NewChangePasswordHandler     = auth.NewChangePasswordHandler
	NewBreakGlassHandler         = auth.NewBreakGlassHandler
	NewBreakGlassAPIHandler      = auth.NewBreakGlassAPIHandler
	NewSSOCutoverHandler         = auth.NewSSOCutoverHandler
	NewMeHandler                 = me.NewMeHandler
	NewAPIKeysHandler            = apikey.NewAPIKeysHandler
	NewNotifyTargetsHandler      = notifytarget.NewNotifyTargetsHandler
	NewFeedbackHandler           = feedback.NewFeedbackHandler
	NewBatchHandler              = feedback.NewBatchHandler
	NewSearchHandler             = feedback.NewSearchHandler
	NewQualityActionHandler      = feedback.NewQualityActionHandler
	NewFeedbackJobHandler        = feedbackjob.NewHandler
	NewGDPRHandler               = consolegdpr.NewHandler
	NewUsageHandler              = usage.NewUsageHandler
	NewEnrichConfigHandler       = enrichconfig.NewHandler
	NewEnrichmentRuntimeHandler  = consoleenrichmentruntime.NewHandler
	NewExternalSyncHandler       = consoleexternalsync.NewHandler
	NewGuardPolicyHandler        = consoleguardpolicy.NewHandler
	NewInboundHandler            = consoleinbound.NewHandler
	NewLLMConfigHandler          = consolellmconfig.NewHandler
	NewClustersHandler           = clusters.NewClustersHandler
	NewCustomerRequestHandler    = consolecustomerrequest.NewHandler
	NewPublicVisibilityHandler   = consolepublicvisibility.NewHandler
	NewDigestSubscriptionHandler = digestsubscription.NewHandler
	NewOutboxHandler             = consoleoutbox.NewHandler
	NewTagHandler                = consoletag.NewHandler
	NewTagAssignmentHandler      = consoletagassignment.NewHandler
	NewWorkflowHandler           = consoleworkflow.NewHandler
	NewOIDCHandler               = consoleoidc.NewHandler
	NewMemberHandler             = member.NewHandler
	NewMCPClientHandler          = consolemcpclient.NewHandler
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
//	 GET /feedback/terminal-failures -> dispatcher.Bind(feedback.Handler.GetTerminalFailureWorkbench)
//	 GET /feedback/stats -> dispatcher.Bind(feedback.Handler.Stats)
//	 POST /feedback/search -> dispatcher.Bind(feedback.SearchHandler.Search)
//	 GET /feedback/search/quality -> dispatcher.Bind(feedback.SearchHandler.GetSearchQuality)
//	 POST /feedback/search/events -> dispatcher.Bind(feedback.SearchHandler.RecordSearchEvent)
//	 GET /quality-actions -> dispatcher.Bind(feedback.QualityActionHandler.ListQualityActions)
//	 POST /quality-actions/update -> dispatcher.Bind(feedback.QualityActionHandler.UpdateQualityAction)
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
//	 POST /inbound/sources/slack/discover -> dispatcher.Bind(inbound.Handler.DiscoverSlackChannels)
//	 POST /llm/channels/{id}/test -> dispatcher.Bind(llmconfig.Handler.TestChannel)
//	 GET /llm/channels/{channel_id}/models -> dispatcher.Bind(llmconfig.Handler.ListChannelModels)
//	 GET /clusters -> dispatcher.Bind(clusters.Handler.List)
//	 GET /clusters/{cluster_id}/members -> dispatcher.Bind(clusters.Handler.GetMembers)
type Router struct {
	signer             *session.Signer
	login              *auth.Handler
	changePassword     *auth.ChangePasswordHandler
	breakglass         *auth.BreakGlassHandler
	breakglassAPI      *auth.BreakGlassAPIHandler
	ssoCutover         *auth.SSOCutoverHandler
	me                 *me.MeHandler
	auditLog           *consoleauditlog.Handler
	apiKeys            *apikey.APIKeysHandler
	notifyTargets      *notifytarget.NotifyTargetsHandler
	feedback           *feedback.FeedbackHandler
	feedbackBatch      *feedback.BatchHandler
	feedbackSearch     *feedback.SearchHandler
	qualityActions     *feedback.QualityActionHandler
	customerRequests   *consolecustomerrequest.Handler
	publicVisibility   *consolepublicvisibility.Handler
	feedbackJob        *feedbackjob.Handler
	gdpr               *consolegdpr.Handler
	usage              *usage.UsageHandler
	enrichConfig       *enrichconfig.Handler
	enrichmentRuntime  *consoleenrichmentruntime.Handler
	externalSync       *consoleexternalsync.Handler
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
	mcpClients         *consolemcpclient.Handler
	auditEvidence      *consoleauditevidence.Handler
	preflight          http.Handler
	recovery           http.Handler
	releaseInfo        http.Handler
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

	// Break-glass login (public, no session required)
	r.mountBreakGlass(mux)

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
	r.mountMCPClients(m)
	r.mountPreflight(m)
	r.mountSSOCutover(m)
	r.mountDigestSubscription(m)
	r.mountCustomerRequests(m)
	r.mountPublicVisibility(m)
	r.mountFeedback(m)
	r.mountReplySendHook(m)
	m.Group(func(u chi.Router) {
		u.Use(r.requireViewer) // Usage stats visible to all roles
		u.Get("/classification-quality", dispatcher.Bind(
			"console.FeedbackHandler.GetClassificationQuality",
			dispatcher.Query(
				func() *attunev1.GetClassificationQualityRequest {
					return ptrext.Of(attunev1.GetClassificationQualityRequest{})
				},
				feedback.BindClassificationQualityRequest,
			),
			r.feedback.GetClassificationQuality,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetClassificationQualityRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		u.Get("/classification-quality/samples", dispatcher.Bind(
			"console.FeedbackHandler.GetClassificationQualitySamples",
			dispatcher.Query(
				func() *attunev1.GetClassificationQualitySamplesRequest {
					return ptrext.Of(attunev1.GetClassificationQualitySamplesRequest{})
				},
				feedback.BindClassificationQualitySamplesRequest,
			),
			r.feedback.GetClassificationQualitySamples,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetClassificationQualitySamplesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
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
		r.mountQualityActions(u)
	})
	r.mountEnrichConfig(m)
	r.mountEnrichmentRuntime(m)
	r.mountExternalSync(m)
	r.mountGuardPolicies(m)
	r.mountInbound(m)
	r.mountJobs(m)
	r.mountLLMConfig(m)
	r.mountClusters(m)
	r.mountTags(m)
	r.mountWorkflow(m)
	r.mountMembers(m)
}

func (r *Router) SetQualityActionHandler(h *feedback.QualityActionHandler) {
	r.qualityActions = h
}

func (r *Router) SetCustomerRequestHandler(h *consolecustomerrequest.Handler) {
	r.customerRequests = h
}

func (r *Router) SetPublicVisibilityHandler(h *consolepublicvisibility.Handler) {
	r.publicVisibility = h
}

func (r *Router) mountPublicVisibility(m chi.Router) {
	if r.publicVisibility == nil {
		return
	}
	m.Route("/public-visibility", func(pv chi.Router) {
		pv.With(r.requireDelegatedAdmin).Get("/policy", dispatcher.Bind(
			"console.PublicVisibilityHandler.GetPolicy",
			dispatcher.Empty(func() *attunev1.GetPublicVisibilityPolicyRequest {
				return ptrext.Of(attunev1.GetPublicVisibilityPolicyRequest{})
			}),
			r.publicVisibility.GetPolicy,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetPublicVisibilityPolicyRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		pv.With(r.requireDelegatedAdminStrict).Put("/policy", dispatcher.Bind(
			"console.PublicVisibilityHandler.UpdatePolicy",
			dispatcher.JSON(func() *attunev1.UpdatePublicVisibilityPolicyRequest {
				return ptrext.Of(attunev1.UpdatePublicVisibilityPolicyRequest{})
			}),
			r.publicVisibility.UpdatePolicy,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdatePublicVisibilityPolicyRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		pv.With(r.requireMember).Get("/moderation", dispatcher.Bind(
			"console.PublicVisibilityHandler.ListModeration",
			dispatcher.Query(
				func() *attunev1.ListModerationSubjectsRequest {
					return ptrext.Of(attunev1.ListModerationSubjectsRequest{})
				},
				consolepublicvisibility.BindListRequest,
			),
			r.publicVisibility.ListModeration,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListModerationSubjectsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		pv.With(r.requireMember).Get("/requests/{request_id}/profile", dispatcher.Bind(
			"console.PublicVisibilityHandler.GetRequestProfile",
			dispatcher.Combine(
				func() *attunev1.GetPublicRequestProfileRequest {
					return ptrext.Of(attunev1.GetPublicRequestProfileRequest{})
				},
				dispatcher.Param("request_id", func(req *attunev1.GetPublicRequestProfileRequest, id string) {
					req.RequestId = id
				}),
			),
			r.publicVisibility.GetRequestProfile,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetPublicRequestProfileRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		pv.With(r.requireDelegatedAdminStrict).Put("/requests/{request_id}/profile", dispatcher.Bind(
			"console.PublicVisibilityHandler.UpsertRequestProfile",
			dispatcher.Combine(
				func() *attunev1.UpsertPublicRequestProfileRequest {
					return ptrext.Of(attunev1.UpsertPublicRequestProfileRequest{})
				},
				dispatcher.JSONBody[*attunev1.UpsertPublicRequestProfileRequest],
				dispatcher.Param("request_id", func(req *attunev1.UpsertPublicRequestProfileRequest, id string) {
					req.RequestId = id
				}),
			),
			r.publicVisibility.UpsertRequestProfile,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpsertPublicRequestProfileRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		pv.With(r.requireMember).Post("/moderation/{id}:approve", dispatcher.Bind(
			"console.PublicVisibilityHandler.Approve",
			dispatcher.Combine(
				func() *attunev1.ApproveModerationSubjectRequest {
					return ptrext.Of(attunev1.ApproveModerationSubjectRequest{})
				},
				dispatcher.JSONBody[*attunev1.ApproveModerationSubjectRequest],
				dispatcher.Param("id", func(req *attunev1.ApproveModerationSubjectRequest, id string) {
					req.Id = id
				}),
			),
			r.publicVisibility.Approve,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ApproveModerationSubjectRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		pv.With(r.requireMember).Post("/moderation/{id}:reject", dispatcher.Bind(
			"console.PublicVisibilityHandler.Reject",
			dispatcher.Combine(
				func() *attunev1.RejectModerationSubjectRequest {
					return ptrext.Of(attunev1.RejectModerationSubjectRequest{})
				},
				dispatcher.JSONBody[*attunev1.RejectModerationSubjectRequest],
				dispatcher.Param("id", func(req *attunev1.RejectModerationSubjectRequest, id string) {
					req.Id = id
				}),
			),
			r.publicVisibility.Reject,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RejectModerationSubjectRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		pv.With(r.requireDelegatedAdminStrict).Post("/moderation/{id}:hide", dispatcher.Bind(
			"console.PublicVisibilityHandler.Hide",
			dispatcher.Combine(
				func() *attunev1.HideModerationSubjectRequest {
					return ptrext.Of(attunev1.HideModerationSubjectRequest{})
				},
				dispatcher.JSONBody[*attunev1.HideModerationSubjectRequest],
				dispatcher.Param("id", func(req *attunev1.HideModerationSubjectRequest, id string) {
					req.Id = id
				}),
			),
			r.publicVisibility.Hide,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.HideModerationSubjectRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		pv.With(r.requireDelegatedAdminStrict).Post("/moderation/{id}:mark-spam", dispatcher.Bind(
			"console.PublicVisibilityHandler.MarkSpam",
			dispatcher.Combine(
				func() *attunev1.MarkModerationSubjectSpamRequest {
					return ptrext.Of(attunev1.MarkModerationSubjectSpamRequest{})
				},
				dispatcher.JSONBody[*attunev1.MarkModerationSubjectSpamRequest],
				dispatcher.Param("id", func(req *attunev1.MarkModerationSubjectSpamRequest, id string) {
					req.Id = id
				}),
			),
			r.publicVisibility.MarkSpam,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.MarkModerationSubjectSpamRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		pv.With(r.requireDelegatedAdminStrict).Post("/moderation/{id}:restore", dispatcher.Bind(
			"console.PublicVisibilityHandler.Restore",
			dispatcher.Combine(
				func() *attunev1.RestoreModerationSubjectRequest {
					return ptrext.Of(attunev1.RestoreModerationSubjectRequest{})
				},
				dispatcher.JSONBody[*attunev1.RestoreModerationSubjectRequest],
				dispatcher.Param("id", func(req *attunev1.RestoreModerationSubjectRequest, id string) {
					req.Id = id
				}),
			),
			r.publicVisibility.Restore,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RestoreModerationSubjectRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountCustomerRequests(m chi.Router) {
	if r.customerRequests == nil {
		return
	}
	m.Route("/customer-requests", func(cr chi.Router) {
		cr.Use(r.requireViewer)
		r.mountCustomerRequestReads(cr)
		r.mountCustomerRequestBaseMutations(cr)
		r.mountCustomerRequestFeedback(cr)
		r.mountCustomerRequestCustomers(cr)
		r.mountCustomerRequestVotes(cr)
		r.mountCustomerRequestNotes(cr)
		r.mountCustomerRequestIssues(cr)
		r.mountCustomerRequestMerge(cr)
	})
	r.mountCustomerRequestPromotion(m)
}

func (r *Router) mountCustomerRequestReads(cr chi.Router) {
	cr.Get("/", dispatcher.Bind(
		"console.CustomerRequestHandler.List",
		dispatcher.Query(
			func() *attunev1.ListCustomerRequestsRequest {
				return ptrext.Of(attunev1.ListCustomerRequestsRequest{})
			},
			consolecustomerrequest.BindListRequest,
		),
		r.customerRequests.List,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListCustomerRequestsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.Get("/scoring-settings", dispatcher.Bind(
		"console.CustomerRequestHandler.GetScoringSettings",
		dispatcher.Path(func() *attunev1.GetCustomerRequestScoringSettingsRequest {
			return ptrext.Of(attunev1.GetCustomerRequestScoringSettingsRequest{})
		}),
		r.customerRequests.GetScoringSettings,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetCustomerRequestScoringSettingsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.With(r.requireDelegatedAdminStrict).Put("/scoring-settings", dispatcher.Bind(
		"console.CustomerRequestHandler.UpdateScoringSettings",
		dispatcher.JSON(func() *attunev1.UpdateCustomerRequestScoringSettingsRequest {
			return ptrext.Of(attunev1.UpdateCustomerRequestScoringSettingsRequest{})
		}),
		r.customerRequests.UpdateScoringSettings,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateCustomerRequestScoringSettingsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.Get("/saved-views", dispatcher.Bind(
		"console.CustomerRequestHandler.ListSavedViews",
		dispatcher.Empty(func() *attunev1.ListCustomerRequestSavedViewsRequest {
			return ptrext.Of(attunev1.ListCustomerRequestSavedViewsRequest{})
		}),
		r.customerRequests.ListSavedViews,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListCustomerRequestSavedViewsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.Post("/saved-views", dispatcher.Bind(
		"console.CustomerRequestHandler.CreateSavedView",
		dispatcher.JSON(func() *attunev1.CreateCustomerRequestSavedViewRequest {
			return ptrext.Of(attunev1.CreateCustomerRequestSavedViewRequest{})
		}),
		r.customerRequests.CreateSavedView,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateCustomerRequestSavedViewRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.Put("/saved-views/{view_id}", dispatcher.Bind(
		"console.CustomerRequestHandler.UpdateSavedView",
		dispatcher.Combine(
			func() *attunev1.UpdateCustomerRequestSavedViewRequest {
				return ptrext.Of(attunev1.UpdateCustomerRequestSavedViewRequest{})
			},
			dispatcher.JSONBody[*attunev1.UpdateCustomerRequestSavedViewRequest],
			dispatcher.Param("view_id", func(req *attunev1.UpdateCustomerRequestSavedViewRequest, id string) {
				req.Id = id
			}),
		),
		r.customerRequests.UpdateSavedView,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateCustomerRequestSavedViewRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.Delete("/saved-views/{view_id}", dispatcher.Bind(
		"console.CustomerRequestHandler.DeleteSavedView",
		dispatcher.Path(
			func() *attunev1.DeleteCustomerRequestSavedViewRequest {
				return ptrext.Of(attunev1.DeleteCustomerRequestSavedViewRequest{})
			},
			dispatcher.Param("view_id", func(req *attunev1.DeleteCustomerRequestSavedViewRequest, id string) {
				req.Id = id
			}),
		),
		r.customerRequests.DeleteSavedView,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteCustomerRequestSavedViewRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.Get("/{id}", dispatcher.Bind(
		"console.CustomerRequestHandler.Get",
		dispatcher.Path(
			func() *attunev1.GetCustomerRequestRequest {
				return ptrext.Of(attunev1.GetCustomerRequestRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.GetCustomerRequestRequest, id string) {
				req.Id = id
			}),
		),
		r.customerRequests.Get,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetCustomerRequestRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountCustomerRequestBaseMutations(cr chi.Router) {
	cr.With(r.requireMember).Post("/", dispatcher.Bind(
		"console.CustomerRequestHandler.Create",
		dispatcher.JSON(func() *attunev1.CreateCustomerRequestRequest {
			return ptrext.Of(attunev1.CreateCustomerRequestRequest{})
		}),
		r.customerRequests.Create,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateCustomerRequestRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.With(r.requireMember).Patch("/{id}", dispatcher.Bind(
		"console.CustomerRequestHandler.Update",
		dispatcher.Combine(
			func() *attunev1.UpdateCustomerRequestRequest {
				return ptrext.Of(attunev1.UpdateCustomerRequestRequest{})
			},
			dispatcher.JSONBody[*attunev1.UpdateCustomerRequestRequest],
			dispatcher.Param("id", func(req *attunev1.UpdateCustomerRequestRequest, id string) {
				req.Id = id
			}),
		),
		r.customerRequests.Update,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateCustomerRequestRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountCustomerRequestFeedback(cr chi.Router) {
	cr.With(r.requireMember).Post("/{id}/feedback", dispatcher.Bind(
		"console.CustomerRequestHandler.LinkFeedback",
		dispatcher.Combine(
			func() *attunev1.LinkFeedbackToCustomerRequestRequest {
				return ptrext.Of(attunev1.LinkFeedbackToCustomerRequestRequest{})
			},
			dispatcher.JSONBody[*attunev1.LinkFeedbackToCustomerRequestRequest],
			dispatcher.Param("id", func(req *attunev1.LinkFeedbackToCustomerRequestRequest, id string) {
				req.Id = id
			}),
		),
		r.customerRequests.LinkFeedback,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.LinkFeedbackToCustomerRequestRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.With(r.requireMember).Delete("/{id}/feedback/{feedback_id}", dispatcher.Bind(
		"console.CustomerRequestHandler.UnlinkFeedback",
		dispatcher.Path(
			func() *attunev1.UnlinkFeedbackFromCustomerRequestRequest {
				return ptrext.Of(attunev1.UnlinkFeedbackFromCustomerRequestRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.UnlinkFeedbackFromCustomerRequestRequest, id string) {
				req.Id = id
			}),
			dispatcher.ParamInt64("feedback_id", func(req *attunev1.UnlinkFeedbackFromCustomerRequestRequest, id int64) {
				req.FeedbackId = id
			}, "feedback_id must be an integer"),
		),
		r.customerRequests.UnlinkFeedback,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UnlinkFeedbackFromCustomerRequestRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountCustomerRequestCustomers(cr chi.Router) {
	cr.With(r.requireMember).Post("/{id}/customers", dispatcher.Bind(
		"console.CustomerRequestHandler.LinkCustomer",
		dispatcher.Combine(
			func() *attunev1.LinkCustomerToCustomerRequestRequest {
				return ptrext.Of(attunev1.LinkCustomerToCustomerRequestRequest{})
			},
			dispatcher.JSONBody[*attunev1.LinkCustomerToCustomerRequestRequest],
			dispatcher.Param("id", func(req *attunev1.LinkCustomerToCustomerRequestRequest, id string) {
				req.Id = id
			}),
		),
		r.customerRequests.LinkCustomer,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.LinkCustomerToCustomerRequestRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.With(r.requireMember).Delete("/{id}/customers/{customer_link_id}", dispatcher.Bind(
		"console.CustomerRequestHandler.UnlinkCustomer",
		dispatcher.Path(
			func() *attunev1.UnlinkCustomerFromCustomerRequestRequest {
				return ptrext.Of(attunev1.UnlinkCustomerFromCustomerRequestRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.UnlinkCustomerFromCustomerRequestRequest, id string) {
				req.Id = id
			}),
			dispatcher.Param("customer_link_id", func(req *attunev1.UnlinkCustomerFromCustomerRequestRequest, id string) {
				req.CustomerLinkId = id
			}),
		),
		r.customerRequests.UnlinkCustomer,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UnlinkCustomerFromCustomerRequestRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountCustomerRequestVotes(cr chi.Router) {
	cr.With(r.requireMember).Post("/{id}/votes", dispatcher.Bind(
		"console.CustomerRequestHandler.AddVote",
		dispatcher.Combine(
			func() *attunev1.AddCustomerRequestVoteRequest {
				return ptrext.Of(attunev1.AddCustomerRequestVoteRequest{})
			},
			dispatcher.JSONBody[*attunev1.AddCustomerRequestVoteRequest],
			dispatcher.Param("id", func(req *attunev1.AddCustomerRequestVoteRequest, id string) {
				req.Id = id
			}),
		),
		r.customerRequests.AddVote,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.AddCustomerRequestVoteRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.With(r.requireMember).Delete("/{id}/votes/{vote_id}", dispatcher.Bind(
		"console.CustomerRequestHandler.RemoveVote",
		dispatcher.Path(
			func() *attunev1.RemoveCustomerRequestVoteRequest {
				return ptrext.Of(attunev1.RemoveCustomerRequestVoteRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.RemoveCustomerRequestVoteRequest, id string) {
				req.Id = id
			}),
			dispatcher.Param("vote_id", func(req *attunev1.RemoveCustomerRequestVoteRequest, id string) {
				req.VoteId = id
			}),
		),
		r.customerRequests.RemoveVote,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RemoveCustomerRequestVoteRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountCustomerRequestNotes(cr chi.Router) {
	cr.With(r.requireMember).Post("/{id}/notes", dispatcher.Bind(
		"console.CustomerRequestHandler.AddNote",
		dispatcher.Combine(
			func() *attunev1.AddCustomerRequestNoteRequest {
				return ptrext.Of(attunev1.AddCustomerRequestNoteRequest{})
			},
			dispatcher.JSONBody[*attunev1.AddCustomerRequestNoteRequest],
			dispatcher.Param("id", func(req *attunev1.AddCustomerRequestNoteRequest, id string) {
				req.Id = id
			}),
		),
		r.customerRequests.AddNote,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.AddCustomerRequestNoteRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.With(r.requireMember).Delete("/{id}/notes/{note_id}", dispatcher.Bind(
		"console.CustomerRequestHandler.DeleteNote",
		dispatcher.Path(
			func() *attunev1.DeleteCustomerRequestNoteRequest {
				return ptrext.Of(attunev1.DeleteCustomerRequestNoteRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.DeleteCustomerRequestNoteRequest, id string) {
				req.Id = id
			}),
			dispatcher.Param("note_id", func(req *attunev1.DeleteCustomerRequestNoteRequest, id string) {
				req.NoteId = id
			}),
		),
		r.customerRequests.DeleteNote,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteCustomerRequestNoteRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountCustomerRequestIssues(cr chi.Router) {
	cr.With(r.requireMember).Post("/{id}/issue-links", dispatcher.Bind(
		"console.CustomerRequestHandler.LinkIssue",
		dispatcher.Combine(
			func() *attunev1.LinkCustomerRequestIssueRequest {
				return ptrext.Of(attunev1.LinkCustomerRequestIssueRequest{})
			},
			dispatcher.JSONBody[*attunev1.LinkCustomerRequestIssueRequest],
			dispatcher.Param("id", func(req *attunev1.LinkCustomerRequestIssueRequest, id string) {
				req.Id = id
			}),
		),
		r.customerRequests.LinkIssue,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.LinkCustomerRequestIssueRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.With(r.requireMember).Delete("/{id}/issue-links/{issue_link_id}", dispatcher.Bind(
		"console.CustomerRequestHandler.UnlinkIssue",
		dispatcher.Path(
			func() *attunev1.UnlinkCustomerRequestIssueRequest {
				return ptrext.Of(attunev1.UnlinkCustomerRequestIssueRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.UnlinkCustomerRequestIssueRequest, id string) {
				req.Id = id
			}),
			dispatcher.Param("issue_link_id", func(req *attunev1.UnlinkCustomerRequestIssueRequest, id string) {
				req.IssueLinkId = id
			}),
		),
		r.customerRequests.UnlinkIssue,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UnlinkCustomerRequestIssueRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	cr.With(r.requireMember).Post("/{id}/issue-links/{issue_link_id}:record-sync", dispatcher.Bind(
		"console.CustomerRequestHandler.RecordIssueSync",
		dispatcher.Combine(
			func() *attunev1.RecordCustomerRequestIssueSyncRequest {
				return ptrext.Of(attunev1.RecordCustomerRequestIssueSyncRequest{})
			},
			dispatcher.JSONBody[*attunev1.RecordCustomerRequestIssueSyncRequest],
			dispatcher.Param("id", func(req *attunev1.RecordCustomerRequestIssueSyncRequest, id string) {
				req.Id = id
			}),
			dispatcher.Param("issue_link_id", func(req *attunev1.RecordCustomerRequestIssueSyncRequest, id string) {
				req.IssueLinkId = id
			}),
		),
		r.customerRequests.RecordIssueSync,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RecordCustomerRequestIssueSyncRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountCustomerRequestMerge(cr chi.Router) {
	cr.With(r.requireDelegatedAdminStrict).Post("/{source_id}:merge", dispatcher.Bind(
		"console.CustomerRequestHandler.Merge",
		dispatcher.Combine(
			func() *attunev1.MergeCustomerRequestsRequest {
				return ptrext.Of(attunev1.MergeCustomerRequestsRequest{})
			},
			dispatcher.JSONBody[*attunev1.MergeCustomerRequestsRequest],
			dispatcher.Param("source_id", func(req *attunev1.MergeCustomerRequestsRequest, id string) {
				req.SourceId = id
			}),
		),
		r.customerRequests.Merge,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.MergeCustomerRequestsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountCustomerRequestPromotion(m chi.Router) {
	m.With(r.requireMember).Post("/customer-requests:promote-feedback", dispatcher.Bind(
		"console.CustomerRequestHandler.PromoteFeedback",
		dispatcher.JSON(func() *attunev1.PromoteFeedbackToCustomerRequestRequest {
			return ptrext.Of(attunev1.PromoteFeedbackToCustomerRequestRequest{})
		}),
		r.customerRequests.PromoteFeedback,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PromoteFeedbackToCustomerRequestRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountQualityActions(m chi.Router) {
	if r.qualityActions == nil {
		return
	}
	m.Get("/quality-actions", dispatcher.Bind(
		"console.QualityActionHandler.ListQualityActions",
		dispatcher.Query(
			func() *attunev1.ListQualityActionsRequest {
				return ptrext.Of(attunev1.ListQualityActionsRequest{})
			},
			feedback.BindListQualityActionsRequest,
		),
		r.qualityActions.ListQualityActions,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListQualityActionsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.Post("/quality-actions/update", dispatcher.Bind(
		"console.QualityActionHandler.UpdateQualityAction",
		dispatcher.JSON(func() *attunev1.UpdateQualityActionRequest {
			return ptrext.Of(attunev1.UpdateQualityActionRequest{})
		}),
		r.qualityActions.UpdateQualityAction,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateQualityActionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
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

func (r *Router) requireDelegatedAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.useRBACForRequest(req) {
			r.rbac.RequireDelegatedAdmin()(next).ServeHTTP(w, req)
			return
		}
		r.requireAdminLegacy(next).ServeHTTP(w, req)
	})
}

func (r *Router) requireDelegatedAdminStrict(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.useRBACForRequest(req) {
			r.rbac.RequireDelegatedAdminStrict()(next).ServeHTTP(w, req)
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

func (r *Router) requireMember(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.useRBACForRequest(req) {
			r.rbac.RequireMember()(next).ServeHTTP(w, req)
			return
		}
		if r.admins != nil {
			r.requireAdminLegacy(next).ServeHTTP(w, req)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (r *Router) useRBACForRequest(req *http.Request) bool {
	if r.rbac == nil {
		return false
	}
	auth := session.OptionalFromContext(req.Context())
	if auth == nil {
		return false
	}
	return auth.TenantID != "" && auth.UserType != ""
}

func (r *Router) requireAdminLegacy(next http.Handler) http.Handler {
	const where = "console.Router.requireAdminLegacy"
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authCtx := session.OptionalFromContext(req.Context())
		if authCtx == nil {
			logext.Warnf(req.Context(), "[%s] reject: missing auth context", where)
			dispatcher.Reject(req.Context(), w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "session required")
			return
		}
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
			if errors.Is(err, context.Canceled) {
				logext.Warnf(req.Context(), "[%s] canceled,user_id:%s", where, authCtx.UserID)
				dispatcher.Reject(req.Context(), w, dispatcher.StatusClientClosedRequest, attunev1.ErrorCode_CLIENT_CANCELED, "client canceled request")
				return
			}
			if errors.Is(err, context.DeadlineExceeded) {
				logext.Warnf(req.Context(), "[%s] deadline exceeded,user_id:%s", where, authCtx.UserID)
				dispatcher.Reject(req.Context(), w, http.StatusGatewayTimeout, attunev1.ErrorCode_DEADLINE_EXCEEDED, "request deadline exceeded")
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
		s.Patch("/{id}", dispatcher.Bind(
			"console.APIKeysHandler.UpdateServiceAccount",
			dispatcher.Path(
				func() *attunev1.UpdateServiceAccountRequest { return ptrext.Of(attunev1.UpdateServiceAccountRequest{}) },
				dispatcher.JSONBody[*attunev1.UpdateServiceAccountRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateServiceAccountRequest, id string) { req.Id = id }),
			),
			r.apiKeys.UpdateServiceAccount,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateServiceAccountRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		s.Delete("/{id}", dispatcher.Bind(
			"console.APIKeysHandler.DeleteServiceAccount",
			dispatcher.Path(
				func() *attunev1.DeleteServiceAccountRequest { return ptrext.Of(attunev1.DeleteServiceAccountRequest{}) },
				dispatcher.Param("id", func(req *attunev1.DeleteServiceAccountRequest, id string) { req.Id = id }),
			),
			r.apiKeys.DeleteServiceAccount,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteServiceAccountRequest) (*session.AuthCtx, error) {
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
		a.Use(r.requireDelegatedAdminStrict)
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
		a.Route("/views", func(v chi.Router) {
			v.Get("/", dispatcher.Bind(
				"console.auditlog.ListSavedViews",
				dispatcher.Empty(func() *attunev1.ListSavedAuditLogViewsRequest {
					return ptrext.Of(attunev1.ListSavedAuditLogViewsRequest{})
				}),
				r.auditLog.ListSavedViews,
				dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListSavedAuditLogViewsRequest) (*session.AuthCtx, error) {
					return session.FromContext(r.Context()), nil
				}),
			))
			v.Post("/", dispatcher.Bind(
				"console.auditlog.CreateSavedView",
				dispatcher.JSON(func() *attunev1.CreateSavedAuditLogViewRequest {
					return ptrext.Of(attunev1.CreateSavedAuditLogViewRequest{})
				}),
				r.auditLog.CreateSavedView,
				dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateSavedAuditLogViewRequest) (*session.AuthCtx, error) {
					return session.FromContext(r.Context()), nil
				}),
			))
			v.Put("/{id}", dispatcher.Bind(
				"console.auditlog.UpdateSavedView",
				dispatcher.Combine(
					func() *attunev1.UpdateSavedAuditLogViewRequest {
						return ptrext.Of(attunev1.UpdateSavedAuditLogViewRequest{})
					},
					dispatcher.JSONBody[*attunev1.UpdateSavedAuditLogViewRequest],
					dispatcher.Param("id", func(req *attunev1.UpdateSavedAuditLogViewRequest, id string) {
						req.Id = id
					}),
				),
				r.auditLog.UpdateSavedView,
				dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateSavedAuditLogViewRequest) (*session.AuthCtx, error) {
					return session.FromContext(r.Context()), nil
				}),
			))
			v.Delete("/{id}", dispatcher.Bind(
				"console.auditlog.DeleteSavedView",
				dispatcher.Path(
					func() *attunev1.DeleteSavedAuditLogViewRequest {
						return ptrext.Of(attunev1.DeleteSavedAuditLogViewRequest{})
					},
					dispatcher.Param("id", func(req *attunev1.DeleteSavedAuditLogViewRequest, id string) {
						req.Id = id
					}),
				),
				r.auditLog.DeleteSavedView,
				dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteSavedAuditLogViewRequest) (*session.AuthCtx, error) {
					return session.FromContext(r.Context()), nil
				}),
			))
		})
		r.mountAuditEvidence(a)
	})
}

func (r *Router) SetAuditEvidenceHandler(h *consoleauditevidence.Handler) {
	r.auditEvidence = h
}

func (r *Router) mountAuditEvidence(a chi.Router) {
	if r.auditEvidence == nil {
		return
	}
	a.Route("/evidence", func(e chi.Router) {
		e.Post("/", dispatcher.Bind(
			"console.auditevidence.Create",
			dispatcher.JSON(func() *attunev1.CreateAuditEvidenceExportRequest {
				return ptrext.Of(attunev1.CreateAuditEvidenceExportRequest{})
			}),
			r.auditEvidence.Create,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateAuditEvidenceExportRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.Get("/{job_id}", dispatcher.Bind(
			"console.auditevidence.Get",
			dispatcher.Empty(func() *attunev1.GetAuditEvidenceExportRequest {
				return ptrext.Of(attunev1.GetAuditEvidenceExportRequest{})
			}),
			r.auditEvidence.Get,
			dispatcher.WithBinders(
				dispatcher.Param("job_id", func(req *attunev1.GetAuditEvidenceExportRequest, id string) {
					req.JobId = id
				}),
			),
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetAuditEvidenceExportRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.Get("/{job_id}/download", dispatcher.Bind(
			"console.auditevidence.Download",
			dispatcher.Empty(func() *attunev1.DownloadAuditEvidenceExportRequest {
				return ptrext.Of(attunev1.DownloadAuditEvidenceExportRequest{})
			}),
			r.auditEvidence.Download,
			dispatcher.WithBinders(
				dispatcher.Param("job_id", func(req *attunev1.DownloadAuditEvidenceExportRequest, id string) {
					req.JobId = id
				}),
			),
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DownloadAuditEvidenceExportRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountGDPR(m chi.Router) {
	if r.gdpr == nil {
		return
	}
	m.Route("/gdpr", func(g chi.Router) {
		g.Use(r.requireDelegatedAdminStrict)
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
		n.Use(r.requireDelegatedAdmin) // Notify targets are operational settings
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
		d.Use(r.requireDelegatedAdmin) // Digest subscription is operational settings
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
		// /terminal-failures, /stats and /batch/tags must come BEFORE /{id}; source order keeps the intent clear.
		f.Get("/terminal-failures", dispatcher.Bind(
			"console.FeedbackHandler.GetTerminalFailureWorkbench",
			dispatcher.Empty(func() *attunev1.GetTerminalFailureWorkbenchRequest {
				return ptrext.Of(attunev1.GetTerminalFailureWorkbenchRequest{})
			}),
			r.feedback.GetTerminalFailureWorkbench,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetTerminalFailureWorkbenchRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
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
		r.mountFeedbackReplyDraftRoutes(f)
		f.Post("/{id}/retry-enrichment", dispatcher.Bind(
			"console.FeedbackHandler.RetryEnrichment",
			dispatcher.Path(
				func() *attunev1.RetryEnrichmentRequest { return ptrext.Of(attunev1.RetryEnrichmentRequest{}) },
				dispatcher.ParamInt64("id", func(req *attunev1.RetryEnrichmentRequest, id int64) {
					req.Id = id
				}, "id must be an integer"),
			),
			r.feedback.RetryEnrichment,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RetryEnrichmentRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		r.mountFeedbackTagRoutes(f)
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

func (r *Router) mountFeedbackReplyDraftRoutes(f chi.Router) {
	f.With(r.requireMember).Post("/{id}/reply-draft/regenerate", dispatcher.Bind(
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
	f.With(r.requireMember).Post("/{id}/reply-draft/edit", dispatcher.Bind(
		"console.FeedbackHandler.UpdateReplyDraft",
		dispatcher.Combine(
			func() *attunev1.UpdateReplyDraftRequest {
				return ptrext.Of(attunev1.UpdateReplyDraftRequest{})
			},
			dispatcher.JSONBody[*attunev1.UpdateReplyDraftRequest],
			dispatcher.ParamInt64("id", func(req *attunev1.UpdateReplyDraftRequest, id int64) {
				req.Id = id
			}, "id must be an integer"),
		),
		r.feedback.UpdateReplyDraft,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateReplyDraftRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	f.With(r.requireMember).Post("/{id}/reply-draft/approve", dispatcher.Bind(
		"console.FeedbackHandler.ApproveReplyDraft",
		dispatcher.Combine(
			func() *attunev1.ApproveReplyDraftRequest {
				return ptrext.Of(attunev1.ApproveReplyDraftRequest{})
			},
			optionalJSONBody[*attunev1.ApproveReplyDraftRequest], // ptrext:allow proto request type parameter
			dispatcher.ParamInt64("id", func(req *attunev1.ApproveReplyDraftRequest, id int64) {
				req.Id = id
			}, "id must be an integer"),
		),
		r.feedback.ApproveReplyDraft,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ApproveReplyDraftRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	f.With(r.requireMember).Post("/{id}/reply-draft/reject", dispatcher.Bind(
		"console.FeedbackHandler.RejectReplyDraft",
		dispatcher.Combine(
			func() *attunev1.RejectReplyDraftRequest {
				return ptrext.Of(attunev1.RejectReplyDraftRequest{})
			},
			optionalJSONBody[*attunev1.RejectReplyDraftRequest], // ptrext:allow proto request type parameter
			dispatcher.ParamInt64("id", func(req *attunev1.RejectReplyDraftRequest, id int64) {
				req.Id = id
			}, "id must be an integer"),
		),
		r.feedback.RejectReplyDraft,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RejectReplyDraftRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	f.With(r.requireMember).Post("/{id}/reply-draft/send", dispatcher.Bind(
		"console.FeedbackHandler.SendReplyDraft",
		dispatcher.Combine(
			func() *attunev1.SendReplyDraftRequest {
				return ptrext.Of(attunev1.SendReplyDraftRequest{})
			},
			optionalJSONBody[*attunev1.SendReplyDraftRequest], // ptrext:allow proto request type parameter
			dispatcher.ParamInt64("id", func(req *attunev1.SendReplyDraftRequest, id int64) {
				req.Id = id
			}, "id must be an integer"),
		),
		r.feedback.SendReplyDraft,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SendReplyDraftRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountReplySendHook(m chi.Router) {
	if r.feedback == nil {
		return
	}
	m.With(r.requireAdminStrict).Get("/reply-send-hook", dispatcher.Bind(
		"console.FeedbackHandler.GetReplySendHook",
		dispatcher.Empty(func() *attunev1.GetReplySendHookRequest {
			return ptrext.Of(attunev1.GetReplySendHookRequest{})
		}),
		r.feedback.GetReplySendHook,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetReplySendHookRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.With(r.requireAdminStrict).Put("/reply-send-hook", dispatcher.Bind(
		"console.FeedbackHandler.UpsertReplySendHook",
		dispatcher.JSON(func() *attunev1.UpsertReplySendHookRequest {
			return ptrext.Of(attunev1.UpsertReplySendHookRequest{})
		}),
		r.feedback.UpsertReplySendHook,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpsertReplySendHookRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.With(r.requireAdminStrict).Get("/reply-send-hook/health", dispatcher.Bind(
		"console.FeedbackHandler.GetReplySendHookHealth",
		dispatcher.Empty(func() *attunev1.GetReplySendHookHealthRequest {
			return ptrext.Of(attunev1.GetReplySendHookHealthRequest{})
		}),
		r.feedback.GetReplySendHookHealth,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetReplySendHookHealthRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.With(r.requireAdminStrict).Get("/reply-send-hook/deliveries", dispatcher.Bind(
		"console.FeedbackHandler.ListReplySendHookDeliveries",
		dispatcher.Query(
			func() *attunev1.ListReplySendHookDeliveriesRequest {
				return ptrext.Of(attunev1.ListReplySendHookDeliveriesRequest{})
			},
			func(r *http.Request, req *attunev1.ListReplySendHookDeliveriesRequest) error {
				if lim := r.URL.Query().Get("limit"); lim != "" {
					parsed, err := strconv.ParseInt(lim, 10, 32)
					if err != nil {
						return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "limit must be an integer")
					}
					req.Limit = ptrext.Of(int32(parsed))
				}
				return nil
			},
		),
		r.feedback.ListReplySendHookDeliveries,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListReplySendHookDeliveriesRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.With(r.requireAdminStrict).Post("/reply-send-hook/test", dispatcher.Bind(
		"console.FeedbackHandler.TestReplySendHook",
		dispatcher.JSON(func() *attunev1.TestReplySendHookRequest {
			return ptrext.Of(attunev1.TestReplySendHookRequest{})
		}),
		r.feedback.TestReplySendHook,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TestReplySendHookRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.With(r.requireAdminStrict).Post("/reply-send-hook/deliveries/{id}/redeliver", dispatcher.Bind(
		"console.FeedbackHandler.RedeliverReplySendHookDelivery",
		dispatcher.Path(
			func() *attunev1.RedeliverReplySendHookDeliveryRequest {
				return ptrext.Of(attunev1.RedeliverReplySendHookDeliveryRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.RedeliverReplySendHookDeliveryRequest, id string) {
				req.Id = id
			}),
		),
		r.feedback.RedeliverReplySendHookDelivery,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RedeliverReplySendHookDeliveryRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.With(r.requireAdminStrict).Delete("/reply-send-hook", dispatcher.Bind(
		"console.FeedbackHandler.DisableReplySendHook",
		dispatcher.Empty(func() *attunev1.DisableReplySendHookRequest {
			return ptrext.Of(attunev1.DisableReplySendHookRequest{})
		}),
		r.feedback.DisableReplySendHook,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DisableReplySendHookRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func optionalJSONBody[Req proto.Message](r *http.Request, req Req) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	if err := dispatcher.JSONBody[Req](r, req); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
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
		f.Get("/search/quality", dispatcher.Bind(
			"console.SearchHandler.GetSearchQuality",
			dispatcher.Query(
				func() *attunev1.GetSearchQualityRequest {
					return ptrext.Of(attunev1.GetSearchQualityRequest{})
				},
				feedback.BindSearchQualityRequest,
			),
			r.feedbackSearch.GetSearchQuality,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetSearchQualityRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
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
		f.Post("/search/events", dispatcher.Bind(
			"console.SearchHandler.RecordSearchEvent",
			dispatcher.JSON(func() *attunev1.RecordSearchEventRequest {
				return ptrext.Of(attunev1.RecordSearchEventRequest{})
			}),
			r.feedbackSearch.RecordSearchEvent,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RecordSearchEventRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	}
}

func (r *Router) mountFeedbackTagRoutes(f chi.Router) {
	if r.tagAssignments == nil {
		return
	}
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

func (r *Router) mountEnrichConfig(m chi.Router) {
	m.Route("/enrich-config", func(e chi.Router) {
		e.With(r.requireViewer).Get("/", dispatcher.Bind(
			"console.EnrichConfigHandler.Get",
			dispatcher.Empty(func() *attunev1.GetEnrichConfigRequest { return ptrext.Of(attunev1.GetEnrichConfigRequest{}) }),
			r.enrichConfig.Get,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetEnrichConfigRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.With(r.requireDelegatedAdminStrict).Put("/", dispatcher.Bind(
			"console.EnrichConfigHandler.Update",
			dispatcher.JSON(func() *attunev1.UpdateEnrichConfigRequest { return ptrext.Of(attunev1.UpdateEnrichConfigRequest{}) }),
			r.enrichConfig.Update,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateEnrichConfigRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.With(r.requireDelegatedAdminStrict).Post("/preview", dispatcher.Bind(
			"console.EnrichConfigHandler.Preview",
			dispatcher.JSON(func() *attunev1.PreviewEnrichPromptRequest { return ptrext.Of(attunev1.PreviewEnrichPromptRequest{}) }),
			r.enrichConfig.Preview,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PreviewEnrichPromptRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.With(r.requireDelegatedAdminStrict).Post("/eval-suggestions:analyze", dispatcher.Bind(
			"console.EnrichConfigHandler.GetEvalSuggestions",
			dispatcher.JSON(func() *attunev1.GetEvalSuggestionsRequest { return ptrext.Of(attunev1.GetEvalSuggestionsRequest{}) }),
			r.enrichConfig.GetEvalSuggestions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetEvalSuggestionsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.With(r.requireDelegatedAdminStrict).Post("/promote", dispatcher.Bind(
			"console.EnrichConfigHandler.PromoteSuggestedValue",
			dispatcher.JSON(func() *attunev1.PromoteSuggestedValueRequest {
				return ptrext.Of(attunev1.PromoteSuggestedValueRequest{})
			}),
			r.enrichConfig.PromoteSuggestedValue,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PromoteSuggestedValueRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.With(r.requireDelegatedAdminStrict).Post("/versions/{version_id}:activate", dispatcher.Bind(
			"console.EnrichConfigHandler.ActivatePromptVersion",
			dispatcher.Path(
				func() *attunev1.ActivateEnrichPromptVersionRequest {
					return ptrext.Of(attunev1.ActivateEnrichPromptVersionRequest{})
				},
				dispatcher.Param("version_id", func(req *attunev1.ActivateEnrichPromptVersionRequest, id string) {
					req.VersionId = id
				}),
			),
			r.enrichConfig.ActivatePromptVersion,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ActivateEnrichPromptVersionRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		e.With(r.requireViewer).Get("/versions", dispatcher.Bind(
			"console.EnrichConfigHandler.ListPromptVersions",
			dispatcher.Query(
				func() *attunev1.ListEnrichPromptVersionsRequest {
					return ptrext.Of(attunev1.ListEnrichPromptVersionsRequest{})
				},
				enrichconfig.BindListPromptVersionsRequest,
			),
			r.enrichConfig.ListPromptVersions,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListEnrichPromptVersionsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountEnrichmentRuntime(m chi.Router) {
	if r.enrichmentRuntime == nil {
		return
	}
	m.Route("/enrichment-runtime", func(e chi.Router) {
		e.Use(r.requireDelegatedAdminStrict)
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
	m.With(r.requireDelegatedAdminStrict).Post("/enrichment-runtime:reset", dispatcher.Bind(
		"console.EnrichmentRuntimeHandler.ResetLegacy",
		dispatcher.JSON(func() *attunev1.ResetEnrichmentRuntimeRequest {
			return ptrext.Of(attunev1.ResetEnrichmentRuntimeRequest{})
		}),
		r.enrichmentRuntime.Reset,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ResetEnrichmentRuntimeRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	m.With(r.requireDelegatedAdminStrict).Post("/enrichment-runtime:rollback", dispatcher.Bind(
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
		s.Use(r.requireDelegatedAdmin) // Inbound sources are operational settings
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
		s.Post("/slack/discover", dispatcher.Bind(
			"console.inbound.DiscoverSlackChannels",
			dispatcher.JSON(func() *attunev1.DiscoverSlackChannelsRequest {
				return ptrext.Of(attunev1.DiscoverSlackChannelsRequest{})
			}),
			r.inbound.DiscoverSlackChannels,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DiscoverSlackChannelsRequest) (*session.AuthCtx, error) {
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

func (r *Router) SetExternalSyncHandler(h *consoleexternalsync.Handler) {
	r.externalSync = h
}

func (r *Router) mountExternalSync(m chi.Router) {
	if r.externalSync == nil {
		return
	}
	m.Route("/external-sync", func(es chi.Router) {
		es.Use(r.requireDelegatedAdmin)
		r.mountExternalSyncConnectionRoutes(es)
		r.mountExternalSyncMappingRoutes(es)
		r.mountExternalSyncRunRoutes(es)
		r.mountExternalSyncEventRoutes(es)
		es.Get("/health", dispatcher.Bind(
			"console.ExternalSyncHandler.Health",
			dispatcher.Empty(func() *attunev1.GetExternalSyncHealthRequest {
				return ptrext.Of(attunev1.GetExternalSyncHealthRequest{})
			}),
			r.externalSync.Health,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetExternalSyncHealthRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func (r *Router) mountExternalSyncConnectionRoutes(es chi.Router) {
	es.Get("/connections", dispatcher.Bind(
		"console.ExternalSyncHandler.ListConnections",
		dispatcher.Empty(func() *attunev1.ListExternalConnectionsRequest {
			return ptrext.Of(attunev1.ListExternalConnectionsRequest{})
		}),
		r.externalSync.ListConnections,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListExternalConnectionsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/connections", dispatcher.Bind(
		"console.ExternalSyncHandler.CreateConnection",
		dispatcher.JSON(func() *attunev1.CreateExternalConnectionRequest {
			return ptrext.Of(attunev1.CreateExternalConnectionRequest{})
		}),
		r.externalSync.CreateConnection,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateExternalConnectionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Patch("/connections/{id}", dispatcher.Bind(
		"console.ExternalSyncHandler.UpdateConnection",
		dispatcher.Combine(
			func() *attunev1.UpdateExternalConnectionRequest {
				return ptrext.Of(attunev1.UpdateExternalConnectionRequest{})
			},
			dispatcher.JSONBody[*attunev1.UpdateExternalConnectionRequest],
			dispatcher.Param("id", func(req *attunev1.UpdateExternalConnectionRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.UpdateConnection,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateExternalConnectionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Delete("/connections/{id}", dispatcher.Bind(
		"console.ExternalSyncHandler.DeleteConnection",
		dispatcher.Path(
			func() *attunev1.DeleteExternalConnectionRequest {
				return ptrext.Of(attunev1.DeleteExternalConnectionRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.DeleteExternalConnectionRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.DeleteConnection,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteExternalConnectionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/connections/{id}:test", dispatcher.Bind(
		"console.ExternalSyncHandler.TestConnection",
		dispatcher.Path(
			func() *attunev1.TestExternalConnectionRequest {
				return ptrext.Of(attunev1.TestExternalConnectionRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.TestExternalConnectionRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.TestConnection,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.TestExternalConnectionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/connections/{id}:resume", dispatcher.Bind(
		"console.ExternalSyncHandler.ResumeConnection",
		dispatcher.Path(
			func() *attunev1.ResumeExternalConnectionRequest {
				return ptrext.Of(attunev1.ResumeExternalConnectionRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.ResumeExternalConnectionRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.ResumeConnection,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ResumeExternalConnectionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/connections/{id}:qualify", dispatcher.Bind(
		"console.ExternalSyncHandler.QualifyConnection",
		dispatcher.Path(
			func() *attunev1.QualifyExternalConnectionRequest {
				return ptrext.Of(attunev1.QualifyExternalConnectionRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.QualifyExternalConnectionRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.QualifyConnection,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.QualifyExternalConnectionRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.Get("/connections/{id}/schema", dispatcher.Bind(
		"console.ExternalSyncHandler.DiscoverConnectionSchema",
		dispatcher.Path(
			func() *attunev1.DiscoverExternalConnectionSchemaRequest {
				return ptrext.Of(attunev1.DiscoverExternalConnectionSchemaRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.DiscoverExternalConnectionSchemaRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.DiscoverConnectionSchema,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DiscoverExternalConnectionSchemaRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountExternalSyncMappingRoutes(es chi.Router) {
	es.Get("/mappings", dispatcher.Bind(
		"console.ExternalSyncHandler.ListMappings",
		dispatcher.Query(
			func() *attunev1.ListExternalObjectMappingsRequest {
				return ptrext.Of(attunev1.ListExternalObjectMappingsRequest{})
			},
			func(r *http.Request, req *attunev1.ListExternalObjectMappingsRequest) error {
				req.ConnectionId = r.URL.Query().Get("connection_id")
				return nil
			},
		),
		r.externalSync.ListMappings,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListExternalObjectMappingsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Put("/mappings/{id}", dispatcher.Bind(
		"console.ExternalSyncHandler.UpdateMapping",
		dispatcher.Combine(
			func() *attunev1.UpdateExternalObjectMappingRequest {
				return ptrext.Of(attunev1.UpdateExternalObjectMappingRequest{})
			},
			dispatcher.JSONBody[*attunev1.UpdateExternalObjectMappingRequest],
			dispatcher.Param("id", func(req *attunev1.UpdateExternalObjectMappingRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.UpdateMapping,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateExternalObjectMappingRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.Post("/mappings/{id}:preview", dispatcher.Bind(
		"console.ExternalSyncHandler.PreviewMapping",
		dispatcher.Combine(
			func() *attunev1.PreviewExternalObjectMappingRequest {
				return ptrext.Of(attunev1.PreviewExternalObjectMappingRequest{})
			},
			dispatcher.JSONBody[*attunev1.PreviewExternalObjectMappingRequest],
			dispatcher.Param("id", func(req *attunev1.PreviewExternalObjectMappingRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.PreviewMapping,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.PreviewExternalObjectMappingRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/mappings/{id}:reset-cursor", dispatcher.Bind(
		"console.ExternalSyncHandler.ResetCursor",
		dispatcher.Path(
			func() *attunev1.ResetExternalSyncCursorRequest {
				return ptrext.Of(attunev1.ResetExternalSyncCursorRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.ResetExternalSyncCursorRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.ResetCursor,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ResetExternalSyncCursorRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/mappings/{id}:backfill", dispatcher.Bind(
		"console.ExternalSyncHandler.RequestBackfill",
		dispatcher.Combine(
			func() *attunev1.RequestExternalSyncBackfillRequest {
				return ptrext.Of(attunev1.RequestExternalSyncBackfillRequest{})
			},
			dispatcher.JSONBody[*attunev1.RequestExternalSyncBackfillRequest],
			dispatcher.Param("id", func(req *attunev1.RequestExternalSyncBackfillRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.RequestBackfill,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RequestExternalSyncBackfillRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountExternalSyncRunRoutes(es chi.Router) {
	es.With(r.requireDelegatedAdminStrict).Post("/runs", dispatcher.Bind(
		"console.ExternalSyncHandler.RequestRun",
		dispatcher.JSON(func() *attunev1.RequestExternalSyncRunRequest {
			return ptrext.Of(attunev1.RequestExternalSyncRunRequest{})
		}),
		r.externalSync.RequestRun,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RequestExternalSyncRunRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.Get("/runs", dispatcher.Bind(
		"console.ExternalSyncHandler.ListRuns",
		dispatcher.Query(
			func() *attunev1.ListExternalSyncRunsRequest {
				return ptrext.Of(attunev1.ListExternalSyncRunsRequest{})
			},
			consoleexternalsync.BindListRunsRequest,
		),
		r.externalSync.ListRuns,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListExternalSyncRunsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.Get("/runs/{id}", dispatcher.Bind(
		"console.ExternalSyncHandler.GetRun",
		dispatcher.Path(
			func() *attunev1.GetExternalSyncRunRequest {
				return ptrext.Of(attunev1.GetExternalSyncRunRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.GetExternalSyncRunRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.GetRun,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetExternalSyncRunRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.Post("/records:timeline", dispatcher.Bind(
		"console.ExternalSyncHandler.RecordTimeline",
		dispatcher.JSON(func() *attunev1.GetExternalSyncRecordTimelineRequest {
			return ptrext.Of(attunev1.GetExternalSyncRecordTimelineRequest{})
		}),
		r.externalSync.RecordTimeline,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetExternalSyncRecordTimelineRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/runs/{id}:retry", dispatcher.Bind(
		"console.ExternalSyncHandler.RetryRun",
		dispatcher.Path(
			func() *attunev1.RetryExternalSyncRunRequest {
				return ptrext.Of(attunev1.RetryExternalSyncRunRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.RetryExternalSyncRunRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.RetryRun,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RetryExternalSyncRunRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/failures/{id}:retry", dispatcher.Bind(
		"console.ExternalSyncHandler.RetryFailure",
		dispatcher.Path(
			func() *attunev1.RetryExternalSyncFailureRequest {
				return ptrext.Of(attunev1.RetryExternalSyncFailureRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.RetryExternalSyncFailureRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.RetryFailure,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RetryExternalSyncFailureRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/conflicts/{id}:resolve", dispatcher.Bind(
		"console.ExternalSyncHandler.ResolveConflict",
		dispatcher.Combine(
			func() *attunev1.ResolveExternalSyncConflictRequest {
				return ptrext.Of(attunev1.ResolveExternalSyncConflictRequest{})
			},
			dispatcher.JSONBody[*attunev1.ResolveExternalSyncConflictRequest],
			dispatcher.Param("id", func(req *attunev1.ResolveExternalSyncConflictRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.ResolveConflict,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ResolveExternalSyncConflictRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/conflicts:batch-resolve", dispatcher.Bind(
		"console.ExternalSyncHandler.BatchResolveConflicts",
		dispatcher.JSON(func() *attunev1.BatchResolveExternalSyncConflictsRequest {
			return ptrext.Of(attunev1.BatchResolveExternalSyncConflictsRequest{})
		}),
		r.externalSync.BatchResolveConflicts,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.BatchResolveExternalSyncConflictsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
}

func (r *Router) mountExternalSyncEventRoutes(es chi.Router) {
	es.Get("/events", dispatcher.Bind(
		"console.ExternalSyncHandler.ListEvents",
		dispatcher.Query(
			func() *attunev1.ListExternalSyncEventsRequest {
				return ptrext.Of(attunev1.ListExternalSyncEventsRequest{})
			},
			consoleexternalsync.BindListEventsRequest,
		),
		r.externalSync.ListEvents,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListExternalSyncEventsRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.Get("/events/{id}", dispatcher.Bind(
		"console.ExternalSyncHandler.GetEvent",
		dispatcher.Path(
			func() *attunev1.GetExternalSyncEventRequest {
				return ptrext.Of(attunev1.GetExternalSyncEventRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.GetExternalSyncEventRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.GetEvent,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetExternalSyncEventRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
	es.With(r.requireDelegatedAdminStrict).Post("/events/{id}:replay", dispatcher.Bind(
		"console.ExternalSyncHandler.ReplayEvent",
		dispatcher.Path(
			func() *attunev1.ReplayExternalSyncEventRequest {
				return ptrext.Of(attunev1.ReplayExternalSyncEventRequest{})
			},
			dispatcher.Param("id", func(req *attunev1.ReplayExternalSyncEventRequest, id string) {
				req.Id = id
			}),
		),
		r.externalSync.ReplayEvent,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ReplayExternalSyncEventRequest) (*session.AuthCtx, error) {
			return session.FromContext(r.Context()), nil
		}),
	))
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
				consoleoutbox.BindListRequest,
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

func bindListDeliveriesRequest(r *http.Request, req *attunev1.ListDeliveriesRequest) error {
	return consoleoutbox.BindListRequest(r, req)
}

// SetMCPClientHandler injects the MCP OAuth client handler (#93). Optional, so
// callers of NewRouter that don't wire it (tests) simply don't expose /mcp/clients.
func (r *Router) SetMCPClientHandler(h *consolemcpclient.Handler) {
	r.mcpClients = h
}

func (r *Router) mountMCPClients(m chi.Router) {
	if r.mcpClients == nil {
		return
	}
	m.Route("/mcp/clients", func(c chi.Router) {
		c.Use(r.requireAdmin)
		c.Get("/", r.mcpClients.ServeList)
		c.Post("/", r.mcpClients.ServeCreate)
		c.Get("/{id}", r.mcpClients.ServeGet)
		c.Delete("/{id}", r.mcpClients.ServeRevoke)
		c.Patch("/{id}", r.mcpClients.ServeUpdate)
		c.Put("/{id}/tool-policies", r.mcpClients.ServeReplaceToolPolicies)
		c.Delete("/{id}/grants/{grant_id}", r.mcpClients.ServeRevokeRefreshGrant)
		c.Delete("/{id}/sessions/{session_id}", r.mcpClients.ServeRevokeSession)
	})
}

func (r *Router) mountTags(m chi.Router) {
	if r.tags == nil {
		return
	}
	m.Route("/tags", func(t chi.Router) {
		t.Use(r.requireDelegatedAdmin) // Tags config is operational settings
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
		w.Use(r.requireDelegatedAdmin)
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

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// SetPreflightHandler sets the system preflight handler for production
// readiness checks (#149).
func (r *Router) SetPreflightHandler(h http.Handler) {
	r.preflight = h
}

// SetRecoveryHandler sets the restore-drill recovery handler.
func (r *Router) SetRecoveryHandler(h http.Handler) {
	r.recovery = h
}

// SetReleaseInfoHandler sets the system release metadata handler for the
// Reliability page.
func (r *Router) SetReleaseInfoHandler(h http.Handler) {
	r.releaseInfo = h
}

// SetBreakGlassHandler sets the break-glass login handler (#158).
func (r *Router) SetBreakGlassHandler(h *auth.BreakGlassHandler) {
	r.breakglass = h
}

// SetBreakGlassAPIHandler sets the break-glass API handler (#158).
func (r *Router) SetBreakGlassAPIHandler(h *auth.BreakGlassAPIHandler) {
	r.breakglassAPI = h
}

// SetSSOCutoverHandler sets the SSO cutover handler (#158).
func (r *Router) SetSSOCutoverHandler(h *auth.SSOCutoverHandler) {
	r.ssoCutover = h
}

// mountPreflight mounts the admin-only system readiness, recovery, and release routes.
func (r *Router) mountPreflight(m chi.Router) {
	if r.preflight == nil && r.recovery == nil && r.releaseInfo == nil {
		return
	}
	m.Route("/system", func(s chi.Router) {
		s.Use(r.requireAdmin)
		if r.preflight != nil {
			s.Get("/preflight", r.preflight.ServeHTTP)
		}
		if r.recovery != nil {
			s.Get("/recovery", r.recovery.ServeHTTP)
		}
		if r.releaseInfo != nil {
			s.Get("/release", r.releaseInfo.ServeHTTP)
		}
	})
}

// mountBreakGlass mounts the break-glass login endpoint (public, no session).
func (r *Router) mountBreakGlass(mux chi.Router) {
	if r.breakglass == nil {
		return
	}
	mux.Get("/auth/breakglass", r.breakglass.Login)
}

// mountSSOCutover mounts the SSO cutover and break-glass API endpoints (admin-only).
func (r *Router) mountSSOCutover(m chi.Router) {
	m.Route("/auth", func(a chi.Router) {
		a.Use(r.requireAdmin)
		if r.ssoCutover != nil {
			a.Route("/sso", func(s chi.Router) {
				s.Get("/mode", r.ssoCutover.GetAuthMode)
				s.Post("/cutover", r.ssoCutover.Cutover)
				s.Post("/fallback", r.ssoCutover.Fallback)
			})
		}
		if r.breakglassAPI != nil {
			a.Route("/breakglass", func(b chi.Router) {
				b.Get("/tokens", r.breakglassAPI.List)
				b.Post("/issue", r.breakglassAPI.Issue)
				b.Post("/tokens/revoke-all", r.breakglassAPI.RevokeAll)
				b.Post("/tokens/{id}/revoke", r.breakglassAPI.Revoke)
				var lockout *breakglasssvc.LockoutTracker
				if r.breakglass != nil {
					lockout = r.breakglass.Lockout()
				}
				b.Get("/lockouts", func(w http.ResponseWriter, req *http.Request) {
					r.breakglassAPI.ListLockouts(w, req, lockout)
				})
				b.Post("/lockouts/{ip}/unlock", func(w http.ResponseWriter, req *http.Request) {
					r.breakglassAPI.UnlockIP(w, req, lockout)
				})
			})
		}
	})
}
