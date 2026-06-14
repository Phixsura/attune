// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package feedbackjob

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/feedbackbatch"
)

type fakeJobService struct {
	jobs       []*attunev1.JobStatusResponse
	nextCursor string
	statusJob  *attunev1.JobStatusResponse
	statusErr  error
	cancelResp *attunev1.CancelJobResponse
	cancelErr  error

	// Capture calls
	getJobID      string
	getTenantID   string
	cancelJobID   string
	listCursor    string
	listTenantID  string
	listStatusPtr *string
	listLimit     int
}

func (s *fakeJobService) GetJobStatus(ctx context.Context, tenantID, jobID string) (*attunev1.JobStatusResponse, error) {
	s.getTenantID = tenantID
	s.getJobID = jobID
	return s.statusJob, s.statusErr
}

func (s *fakeJobService) ListJobs(ctx context.Context, tenantID string, status *string, limit int, cursor string) ([]*attunev1.JobStatusResponse, string, error) {
	s.listTenantID = tenantID
	s.listStatusPtr = status
	s.listLimit = limit
	s.listCursor = cursor
	return s.jobs, s.nextCursor, nil
}

func (s *fakeJobService) CancelJob(ctx context.Context, tenantID, jobID string) (*attunev1.CancelJobResponse, error) {
	s.cancelJobID = jobID
	return s.cancelResp, s.cancelErr
}

func TestGetStatus(t *testing.T) {
	t.Parallel()

	t.Run("existing job", func(t *testing.T) {
		svc := &fakeJobService{
			statusJob: ptrext.Of(attunev1.JobStatusResponse{
				JobId:    "job-123",
				Status:   attunev1.JobStatus_JOB_STATUS_RUNNING,
				Total:    100,
				Progress: 50,
			}),
		}
		h := NewHandler(svc)
		handler := dispatcher.Bind(
			"console.feedbackjob.Handler.GetStatus",
			dispatcher.Path(
				func() *attunev1.GetJobStatusRequest { return &attunev1.GetJobStatusRequest{} },
				dispatcher.Param("job_id", func(req *attunev1.GetJobStatusRequest, id string) { req.JobId = id }),
			),
			h.GetStatus,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetJobStatusRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/jobs/job-123",
			"",
			dispatchtest.Param{Name: "job_id", Value: "job-123"},
		))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "job-123", body["jobId"])
		require.Equal(t, "JOB_STATUS_RUNNING", body["status"])
		require.Equal(t, float64(100), body["total"])
		require.Equal(t, float64(50), body["progress"])
		require.Equal(t, "job-123", svc.getJobID)
		require.Equal(t, dispatchtest.TenantID, svc.getTenantID)
	})

	t.Run("not found", func(t *testing.T) {
		svc := &fakeJobService{
			statusErr: feedbackbatch.ErrJobNotFound,
		}
		h := NewHandler(svc)
		handler := dispatcher.Bind(
			"console.feedbackjob.Handler.GetStatus",
			dispatcher.Path(
				func() *attunev1.GetJobStatusRequest { return &attunev1.GetJobStatusRequest{} },
				dispatcher.Param("job_id", func(req *attunev1.GetJobStatusRequest, id string) { req.JobId = id }),
			),
			h.GetStatus,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetJobStatusRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/jobs/nonexistent",
			"",
			dispatchtest.Param{Name: "job_id", Value: "nonexistent"},
		))

		require.Equal(t, http.StatusNotFound, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "JOB_NOT_FOUND", body["code"])
	})
}

func TestList(t *testing.T) {
	t.Parallel()

	t.Run("list jobs", func(t *testing.T) {
		svc := &fakeJobService{
			jobs: []*attunev1.JobStatusResponse{
				{
					JobId:  "job-1",
					Status: attunev1.JobStatus_JOB_STATUS_COMPLETED,
					Total:  50,
				},
				{
					JobId:  "job-2",
					Status: attunev1.JobStatus_JOB_STATUS_RUNNING,
					Total:  100,
				},
			},
		}
		h := NewHandler(svc)
		handler := dispatcher.Bind(
			"console.feedbackjob.Handler.List",
			dispatcher.Query(
				func() *attunev1.ListJobsRequest { return &attunev1.ListJobsRequest{} },
				BindListRequest,
			),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListJobsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/jobs",
			"",
		))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		jobs := body["jobs"].([]any)
		require.Len(t, jobs, 2)
	})

	t.Run("list with status filter", func(t *testing.T) {
		svc := &fakeJobService{
			jobs: []*attunev1.JobStatusResponse{
				{
					JobId:  "job-1",
					Status: attunev1.JobStatus_JOB_STATUS_RUNNING,
				},
			},
		}
		h := NewHandler(svc)
		handler := dispatcher.Bind(
			"console.feedbackjob.Handler.List",
			dispatcher.Query(
				func() *attunev1.ListJobsRequest { return &attunev1.ListJobsRequest{} },
				BindListRequest,
			),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListJobsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/jobs?status=running",
			"",
		))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		jobs := body["jobs"].([]any)
		require.Len(t, jobs, 1)
	})

	t.Run("list with cursor pagination", func(t *testing.T) {
		svc := &fakeJobService{
			jobs: []*attunev1.JobStatusResponse{
				{
					JobId:  "job-3",
					Status: attunev1.JobStatus_JOB_STATUS_COMPLETED,
				},
				{
					JobId:  "job-4",
					Status: attunev1.JobStatus_JOB_STATUS_COMPLETED,
				},
			},
			nextCursor: "job-4",
		}
		h := NewHandler(svc)
		handler := dispatcher.Bind(
			"console.feedbackjob.Handler.List",
			dispatcher.Query(
				func() *attunev1.ListJobsRequest { return &attunev1.ListJobsRequest{} },
				BindListRequest,
			),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListJobsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/jobs?cursor=job-2&limit=2",
			"",
		))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		jobs := body["jobs"].([]any)
		require.Len(t, jobs, 2)
		require.Equal(t, "job-4", body["nextCursor"])
		require.Equal(t, "job-2", svc.listCursor)
		require.Equal(t, 2, svc.listLimit)
	})

	t.Run("list without next cursor when no more pages", func(t *testing.T) {
		svc := &fakeJobService{
			jobs: []*attunev1.JobStatusResponse{
				{
					JobId:  "job-5",
					Status: attunev1.JobStatus_JOB_STATUS_COMPLETED,
				},
			},
			nextCursor: "", // No more pages
		}
		h := NewHandler(svc)
		handler := dispatcher.Bind(
			"console.feedbackjob.Handler.List",
			dispatcher.Query(
				func() *attunev1.ListJobsRequest { return &attunev1.ListJobsRequest{} },
				BindListRequest,
			),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListJobsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/jobs?cursor=job-4",
			"",
		))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		jobs := body["jobs"].([]any)
		require.Len(t, jobs, 1)
		require.Nil(t, body["nextCursor"], "should not have nextCursor when no more pages")
	})
}

func TestCancel(t *testing.T) {
	t.Parallel()

	t.Run("cancel queued job", func(t *testing.T) {
		svc := &fakeJobService{
			cancelResp: ptrext.Of(attunev1.CancelJobResponse{
				Status:  attunev1.JobStatus_JOB_STATUS_CANCELLED,
				Message: "Job cancelled successfully",
			}),
		}
		h := NewHandler(svc)
		handler := dispatcher.Bind(
			"console.feedbackjob.Handler.Cancel",
			dispatcher.Path(
				func() *attunev1.CancelJobRequest { return &attunev1.CancelJobRequest{} },
				dispatcher.Param("job_id", func(req *attunev1.CancelJobRequest, id string) { req.JobId = id }),
			),
			h.Cancel,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CancelJobRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/jobs/job-123/cancel",
			"",
			dispatchtest.Param{Name: "job_id", Value: "job-123"},
		))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "JOB_STATUS_CANCELLED", body["status"])
		require.Equal(t, "job-123", svc.cancelJobID)
	})

	t.Run("cancel completed job (409)", func(t *testing.T) {
		svc := &fakeJobService{
			cancelErr: feedbackbatch.ErrJobNotCancellable,
		}
		h := NewHandler(svc)
		handler := dispatcher.Bind(
			"console.feedbackjob.Handler.Cancel",
			dispatcher.Path(
				func() *attunev1.CancelJobRequest { return &attunev1.CancelJobRequest{} },
				dispatcher.Param("job_id", func(req *attunev1.CancelJobRequest, id string) { req.JobId = id }),
			),
			h.Cancel,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CancelJobRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/jobs/job-123/cancel",
			"",
			dispatchtest.Param{Name: "job_id", Value: "job-123"},
		))

		require.Equal(t, http.StatusConflict, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "JOB_ALREADY_CANCELLED", body["code"])
	})

	t.Run("cancel non-existing job (404)", func(t *testing.T) {
		svc := &fakeJobService{
			cancelErr: feedbackbatch.ErrJobNotFound,
		}
		h := NewHandler(svc)
		handler := dispatcher.Bind(
			"console.feedbackjob.Handler.Cancel",
			dispatcher.Path(
				func() *attunev1.CancelJobRequest { return &attunev1.CancelJobRequest{} },
				dispatcher.Param("job_id", func(req *attunev1.CancelJobRequest, id string) { req.JobId = id }),
			),
			h.Cancel,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.CancelJobRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/jobs/nonexistent/cancel",
			"",
			dispatchtest.Param{Name: "job_id", Value: "nonexistent"},
		))

		require.Equal(t, http.StatusNotFound, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "JOB_NOT_FOUND", body["code"])
	})
}
