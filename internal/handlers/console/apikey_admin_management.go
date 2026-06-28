package console

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	consoleauditevidence "github.com/Phixsura/attune/internal/handlers/console/auditevidence"
	consoleauditlog "github.com/Phixsura/attune/internal/handlers/console/auditlog"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consoleoutbox "github.com/Phixsura/attune/internal/handlers/console/outbox"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func mountAPIKeyAuditLog(
	g chi.Router,
	audit *consoleauditlog.Handler,
	evidence *consoleauditevidence.Handler,
	idem func(http.Handler) http.Handler,
) {
	g.Route("/audit-log", func(a chi.Router) {
		a.With(apikey.RequireExplicitScope(domain.ScopeAuditRead)).Get("/", dispatcher.Bind(
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
		a.With(apikey.RequireExplicitScope(domain.ScopeAuditRead)).Get("/export.csv", audit.ExportCSV)
		a.Route("/evidence", func(e chi.Router) {
			e.With(apikey.RequireExplicitScope(domain.ScopeAuditRead), idem).Post("/", dispatcher.Bind(
				"apikey.auditlog.evidence.Create",
				dispatcher.JSON(func() *attunev1.CreateAuditEvidenceExportRequest {
					return ptrext.Of(attunev1.CreateAuditEvidenceExportRequest{})
				}),
				evidence.Create,
				dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateAuditEvidenceExportRequest) (*session.AuthCtx, error) {
					return session.FromContext(r.Context()), nil
				}),
			))
			e.With(apikey.RequireExplicitScope(domain.ScopeAuditRead)).Get("/{job_id}", dispatcher.Bind(
				"apikey.auditlog.evidence.Get",
				dispatcher.Empty(func() *attunev1.GetAuditEvidenceExportRequest {
					return ptrext.Of(attunev1.GetAuditEvidenceExportRequest{})
				}),
				evidence.Get,
				dispatcher.WithBinders(
					dispatcher.Param("job_id", func(req *attunev1.GetAuditEvidenceExportRequest, id string) {
						req.JobId = id
					}),
				),
				dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetAuditEvidenceExportRequest) (*session.AuthCtx, error) {
					return session.FromContext(r.Context()), nil
				}),
			))
			e.With(apikey.RequireExplicitScope(domain.ScopeAuditRead)).Get("/{job_id}/download", dispatcher.Bind(
				"apikey.auditlog.evidence.Download",
				dispatcher.Empty(func() *attunev1.DownloadAuditEvidenceExportRequest {
					return ptrext.Of(attunev1.DownloadAuditEvidenceExportRequest{})
				}),
				evidence.Download,
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
	})
}

func mountAPIKeyGDPR(g chi.Router, gdpr *consolegdpr.Handler, idem func(http.Handler) http.Handler) {
	g.Route("/gdpr", func(gr chi.Router) {
		gr.With(apikey.RequireExplicitScope(domain.ScopeGDPRRead)).Get("/requests", dispatcher.Bind(
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
		gr.With(apikey.RequireExplicitScope(domain.ScopeGDPRRead)).Get("/operations", dispatcher.Bind(
			"apikey.gdpr.GetOperations",
			dispatcher.Empty(func() *attunev1.GetGdprOperationsRequest {
				return ptrext.Of(attunev1.GetGdprOperationsRequest{})
			}),
			gdpr.GetOperations,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetGdprOperationsRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		gr.With(apikey.RequireExplicitScope(domain.ScopeGDPRDelete), idem).Post("/requests/{request_id}/cancel", dispatcher.Bind(
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
		gr.With(apikey.RequireExplicitScope(domain.ScopeGDPRExport), idem).Post("/export", dispatcher.Bind(
			"apikey.gdpr.Export",
			dispatcher.JSON(func() *attunev1.ExportGdprSubjectRequest {
				return ptrext.Of(attunev1.ExportGdprSubjectRequest{})
			}),
			gdpr.Export,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ExportGdprSubjectRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		gr.With(apikey.RequireExplicitScope(domain.ScopeGDPRRead)).Get("/exports/{job_id}", dispatcher.Bind(
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
		gr.With(apikey.RequireExplicitScope(domain.ScopeGDPRExport)).Get("/exports/{job_id}/download", dispatcher.Bind(
			"apikey.gdpr.DownloadExport",
			dispatcher.Empty(func() *attunev1.DownloadGdprExportRequest {
				return ptrext.Of(attunev1.DownloadGdprExportRequest{})
			}),
			gdpr.DownloadExport,
			dispatcher.WithBinders(
				dispatcher.Param("job_id", func(req *attunev1.DownloadGdprExportRequest, id string) {
					req.JobId = id
				}),
			),
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.DownloadGdprExportRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		gr.With(apikey.RequireExplicitScope(domain.ScopeGDPRExport), idem).Post("/exports/{job_id}/revoke", dispatcher.Bind(
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
		gr.With(apikey.RequireExplicitScope(domain.ScopeGDPRDelete), idem).Post("/delete", dispatcher.Bind(
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

func mountAPIKeyOutbox(g chi.Router, outbox *consoleoutbox.Handler, idem func(http.Handler) http.Handler) {
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
		o.With(apikey.RequireExplicitScope(domain.ScopeNotifyWrite), idem).Post("/{id}/retry", dispatcher.Bind(
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
