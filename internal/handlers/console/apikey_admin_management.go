package console

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	consoleauditlog "github.com/Phixsura/attune/internal/handlers/console/auditlog"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consoleoutbox "github.com/Phixsura/attune/internal/handlers/console/outbox"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func mountAPIKeyAuditLog(g chi.Router, audit *consoleauditlog.Handler) {
	g.Route("/audit-log", func(a chi.Router) {
		a.Use(apikey.RequireExplicitScope(domain.ScopeAuditRead))
		a.Get("/", dispatcher.Bind(
			"apikey.auditlog.List",
			dispatcher.Query(
				func() *attunev1.ListAuditLogRequest { return ptrext.Of(attunev1.ListAuditLogRequest{}) },
				consoleauditlog.BindListRequest,
			),
			audit.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListAuditLogRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func mountAPIKeyGDPR(g chi.Router, gdpr *consolegdpr.Handler) {
	g.Route("/gdpr", func(gr chi.Router) {
		gr.Use(apikey.RequireExplicitScope(domain.ScopeGDPRAdmin))
		gr.Get("/requests", dispatcher.Bind(
			"apikey.gdpr.ListRequests",
			dispatcher.Combine(
				func() *attunev1.ListGdprRequestsRequest { return ptrext.Of(attunev1.ListGdprRequestsRequest{}) },
				consolegdpr.BindListRequests,
			),
			gdpr.ListRequests,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListGdprRequestsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		gr.Get("/operations", dispatcher.Bind(
			"apikey.gdpr.GetOperations",
			dispatcher.Empty(func() *attunev1.GetGdprOperationsRequest {
				return ptrext.Of(attunev1.GetGdprOperationsRequest{})
			}),
			gdpr.GetOperations,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetGdprOperationsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		gr.Post("/requests/{request_id}/cancel", dispatcher.Bind(
			"apikey.gdpr.CancelRequest",
			dispatcher.Empty(func() *attunev1.CancelGdprRequestRequest {
				return ptrext.Of(attunev1.CancelGdprRequestRequest{})
			}),
			gdpr.CancelRequest,
			dispatcher.WithBinders(
				dispatcher.Param("request_id", func(req *attunev1.CancelGdprRequestRequest, id string) {
					req.RequestId = id
				}),
			),
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CancelGdprRequestRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		gr.Post("/export", dispatcher.Bind(
			"apikey.gdpr.Export",
			dispatcher.JSON(func() *attunev1.ExportGdprSubjectRequest {
				return ptrext.Of(attunev1.ExportGdprSubjectRequest{})
			}),
			gdpr.Export,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ExportGdprSubjectRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		gr.Get("/exports/{job_id}", dispatcher.Bind(
			"apikey.gdpr.GetExport",
			dispatcher.Empty(func() *attunev1.GetGdprExportRequest { return ptrext.Of(attunev1.GetGdprExportRequest{}) }),
			gdpr.GetExport,
			dispatcher.WithBinders(
				dispatcher.Param("job_id", func(req *attunev1.GetGdprExportRequest, id string) {
					req.JobId = id
				}),
			),
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetGdprExportRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		gr.Post("/exports/{job_id}/revoke", dispatcher.Bind(
			"apikey.gdpr.RevokeExport",
			dispatcher.Empty(func() *attunev1.RevokeGdprExportRequest {
				return ptrext.Of(attunev1.RevokeGdprExportRequest{})
			}),
			gdpr.RevokeExport,
			dispatcher.WithBinders(
				dispatcher.Param("job_id", func(req *attunev1.RevokeGdprExportRequest, id string) {
					req.JobId = id
				}),
			),
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RevokeGdprExportRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		gr.Post("/delete", dispatcher.Bind(
			"apikey.gdpr.Delete",
			dispatcher.JSON(func() *attunev1.DeleteGdprSubjectRequest {
				return ptrext.Of(attunev1.DeleteGdprSubjectRequest{})
			}),
			gdpr.Delete,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DeleteGdprSubjectRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

func mountAPIKeyOutbox(g chi.Router, outbox *consoleoutbox.Handler) {
	g.Route("/outbox", func(o chi.Router) {
		o.With(apikey.RequireExplicitScope(domain.ScopeNotifyRead)).Get("/deliveries", dispatcher.Bind(
			"apikey.outbox.List",
			dispatcher.Query(
				func() *attunev1.ListDeliveriesRequest { return ptrext.Of(attunev1.ListDeliveriesRequest{}) },
				consoleoutbox.BindListRequest,
			),
			outbox.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListDeliveriesRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		o.With(apikey.RequireExplicitScope(domain.ScopeNotifyWrite)).Post("/{id}/retry", dispatcher.Bind(
			"apikey.outbox.Retry",
			dispatcher.Combine(
				func() *attunev1.RetryDeliveryRequest { return ptrext.Of(attunev1.RetryDeliveryRequest{}) },
				dispatcher.ParamInt64("id", func(req *attunev1.RetryDeliveryRequest, id int64) {
					req.Id = id
				}, "invalid delivery id"),
			),
			outbox.Retry,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RetryDeliveryRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}
