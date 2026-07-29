package console

// apikey_admin_requests.go — /v1/requests mounts for the customer-request
// automation surface (#234). Scopes requests:read / requests:write via
// RequireExplicitScope: legacy unscoped keys do NOT get this surface.

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	consolecustomerrequest "github.com/Phixsura/attune/internal/handlers/console/customerrequest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consoletagassignment "github.com/Phixsura/attune/internal/handlers/console/tagassignment"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func mountAPIKeyRequests(g chi.Router, requests *consolecustomerrequest.Handler, idem func(http.Handler) http.Handler) {
	g.Route("/requests", func(rr chi.Router) {
		rr.With(apikey.RequireExplicitScope(domain.ScopeRequestsRead)).Get("/", dispatcher.Bind(
			"apikey.CustomerRequestHandler.ListAutomation",
			dispatcher.Query(
				func() *attunev1.ListRequestsAutomationRequest {
					return ptrext.Of(attunev1.ListRequestsAutomationRequest{})
				},
				bindListRequestsQuery,
			),
			requests.ListAutomation,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListRequestsAutomationRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		rr.With(apikey.RequireExplicitScope(domain.ScopeRequestsWrite), idem).Post("/", dispatcher.Bind(
			"apikey.CustomerRequestHandler.CreateAutomation",
			dispatcher.JSON(func() *attunev1.CreateRequestAutomationRequest {
				return ptrext.Of(attunev1.CreateRequestAutomationRequest{})
			}),
			requests.CreateAutomation,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CreateRequestAutomationRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		rr.With(apikey.RequireExplicitScope(domain.ScopeRequestsWrite)).Patch("/{id}", dispatcher.Bind(
			"apikey.CustomerRequestHandler.UpdateAutomation",
			dispatcher.Combine(
				func() *attunev1.UpdateRequestAutomationRequest {
					return ptrext.Of(attunev1.UpdateRequestAutomationRequest{})
				},
				dispatcher.JSONBody[*attunev1.UpdateRequestAutomationRequest],
				dispatcher.Param("id", func(req *attunev1.UpdateRequestAutomationRequest, id string) { req.Id = id }),
			),
			requests.UpdateAutomation,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateRequestAutomationRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		rr.With(apikey.RequireExplicitScope(domain.ScopeRequestsWrite), idem).Post("/{id}/notes", dispatcher.Bind(
			"apikey.CustomerRequestHandler.AddNoteAutomation",
			dispatcher.Combine(
				func() *attunev1.AddRequestNoteAutomationRequest {
					return ptrext.Of(attunev1.AddRequestNoteAutomationRequest{})
				},
				dispatcher.JSONBody[*attunev1.AddRequestNoteAutomationRequest],
				dispatcher.Param("id", func(req *attunev1.AddRequestNoteAutomationRequest, id string) { req.Id = id }),
			),
			requests.AddNoteAutomation,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.AddRequestNoteAutomationRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}

// bindListRequestsQuery maps the /v1/requests query params onto the proto.
func bindListRequestsQuery(r *http.Request, req *attunev1.ListRequestsAutomationRequest) error {
	q := r.URL.Query()
	req.Q = q.Get("q")
	for _, s := range q["status"] {
		if v, ok := attunev1.CustomerRequestStatus_value[s]; ok {
			req.Status = append(req.Status, attunev1.CustomerRequestStatus(v))
		}
	}
	for _, p := range q["priority"] {
		if v, ok := attunev1.CustomerRequestPriority_value[p]; ok {
			req.Priority = append(req.Priority, attunev1.CustomerRequestPriority(v))
		}
	}
	if v := q.Get("cursor"); v != "" {
		req.Cursor = ptrext.Of(v)
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil && n > 0 {
			req.Limit = ptrext.Of(int32(n))
		}
	}
	return nil
}

// mountAPIKeyTagAssignments exposes tag assignment (POST/DELETE
// /v1/feedback/{id}/tags[/{tag_id}]) over tags:write — the console has the
// same routes session-authenticated; Zapier's "add tag" action needs them
// on the API-key surface.
func mountAPIKeyTagAssignments(g chi.Router, assignments *consoletagassignment.Handler) {
	g.Route("/feedback/{id}/tags", func(tr chi.Router) {
		tr.With(apikey.RequireScope(domain.ScopeTagsWrite)).Post("/", dispatcher.Bind(
			"apikey.TagAssignmentHandler.Add",
			dispatcher.Combine(
				func() *attunev1.AddFeedbackTagRequest { return ptrext.Of(attunev1.AddFeedbackTagRequest{}) },
				dispatcher.JSONBody[*attunev1.AddFeedbackTagRequest],
				dispatcher.ParamInt64("id", func(req *attunev1.AddFeedbackTagRequest, id int64) {
					req.FeedbackId = id
				}, "id must be an integer"),
			),
			assignments.Add,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.AddFeedbackTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
		tr.With(apikey.RequireScope(domain.ScopeTagsWrite)).Delete("/{tag_id}", dispatcher.Bind(
			"apikey.TagAssignmentHandler.Remove",
			dispatcher.Path(
				func() *attunev1.RemoveFeedbackTagRequest { return ptrext.Of(attunev1.RemoveFeedbackTagRequest{}) },
				dispatcher.ParamInt64("id", func(req *attunev1.RemoveFeedbackTagRequest, id int64) {
					req.FeedbackId = id
				}, "id must be an integer"),
				dispatcher.Param("tag_id", func(req *attunev1.RemoveFeedbackTagRequest, id string) {
					req.TagId = id
				}),
			),
			assignments.Remove,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RemoveFeedbackTagRequest) (*session.AuthCtx, error) {
				return session.FromContext(r.Context()), nil
			}),
		))
	})
}
