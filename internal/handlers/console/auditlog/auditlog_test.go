package auditlog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	auditlogviewsvc "github.com/Phixsura/attune/internal/service/auditlogview"
)

type fakeAuditLogService struct {
	filter     auditlogsvc.ListFilter
	nextCursor string
	rows       []auditlogrepo.Entry
}

type fakeSavedViewService struct {
	listViews []auditlogviewsvc.View
	saveInput auditlogviewsvc.SaveInput
	saveView  *auditlogviewsvc.View
	deleteID  string
	deleteBy  string
}

func (f *fakeAuditLogService) List(_ context.Context, filter auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
	f.filter = filter
	return auditlogrepo.ListResult{Items: f.rows, NextCursor: f.nextCursor}, nil
}

func (f *fakeSavedViewService) List(_ context.Context, _, _ string) ([]auditlogviewsvc.View, error) {
	return f.listViews, nil
}

func (f *fakeSavedViewService) Save(_ context.Context, _, _ string, input auditlogviewsvc.SaveInput) (*auditlogviewsvc.View, error) {
	f.saveInput = input
	if f.saveView != nil {
		return f.saveView, nil
	}
	return ptrext.Of(auditlogviewsvc.View{
		ID:   "view-1",
		Name: input.Name,
		State: auditlogviewsvc.State{
			Actions: input.State.Actions,
		},
	}), nil
}

func (f *fakeSavedViewService) Delete(_ context.Context, _, _, id, updatedBy string) error {
	f.deleteID = id
	f.deleteBy = updatedBy
	return nil
}

func TestBindListRequestAcceptsSnakeAndCamelCase(t *testing.T) {
	t.Parallel()

	req := ptrext.Of(attunev1.ListAuditLogRequest{})
	err := BindListRequest(httptest.NewRequest(http.MethodGet,
		"/fb/v1/console/audit-log?actor_type=snake&actorId=camel-id&targetType=channel&target_id=snake-target&action=member.invite&action=member.remove", nil), req)
	require.NoError(t, err)
	require.Equal(t, "snake", req.GetActorType())
	require.Equal(t, "camel-id", req.GetActorId())
	require.Equal(t, "channel", req.GetTargetType())
	require.Equal(t, "snake-target", req.GetTargetId())
	require.Equal(t, []string{"member.invite", "member.remove"}, req.GetActions())
}

func TestBindListRequestRejectsOutOfRangeLimit(t *testing.T) {
	t.Parallel()

	req := ptrext.Of(attunev1.ListAuditLogRequest{})
	err := BindListRequest(httptest.NewRequest(http.MethodGet,
		"/fb/v1/console/audit-log?limit=2147483648", nil), req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "limit must be a positive integer")
}

func TestBindListRequestRejectsNonPositiveLimit(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"0", "-10"} {
		req := ptrext.Of(attunev1.ListAuditLogRequest{})
		err := BindListRequest(httptest.NewRequest(http.MethodGet,
			"/fb/v1/console/audit-log?limit="+raw, nil), req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "limit must be a positive integer")
	}
}

func TestExportCSVUsesUnboundedFilterAndSecurityHeaders(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeAuditLogService{
		rows: []auditlogrepo.Entry{{
			ID:         42,
			ActorType:  "admin",
			ActorID:    "user-1",
			Action:     "api_key.create",
			TargetType: "api_key",
			TargetID:   "key-1",
			Summary:    "Created API key",
			CreatedAt:  time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		}},
	})
	handler := NewHandler(svc)
	req := httptest.NewRequest(http.MethodGet,
		"/fb/v1/console/audit-log/export.csv?actorId=user-1", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{TenantID: "tenant-1"})))
	rec := httptest.NewRecorder()

	handler.ExportCSV(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, svc.filter.Unbounded)
	require.Equal(t, "user-1", svc.filter.ActorID)
	require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", rec.Header().Get("Pragma"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "audit-log-")
	require.Contains(t, rec.Body.String(), "api_key.create")
}

func TestExportCSVBuildsMultiActionAndDateFilter(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeAuditLogService{})
	handler := NewHandler(svc)
	req := httptest.NewRequest(
		http.MethodGet,
		"/fb/v1/console/audit-log/export.csv?action=member.invite&action=member.remove&from=2026-06-16T00:00:00Z&to=2026-06-17T00:00:00Z",
		nil,
	)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{TenantID: "tenant-1"})))
	rec := httptest.NewRecorder()

	handler.ExportCSV(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, svc.filter.Unbounded)
	require.Equal(t, []string{"member.invite", "member.remove"}, svc.filter.Actions)
	require.NotNil(t, svc.filter.From)
	require.NotNil(t, svc.filter.To)
	require.Equal(t, time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC), svc.filter.From.UTC())
	require.Equal(t, time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC), svc.filter.To.UTC())
}

func TestListBuildsFilterFromCamelCaseRequest(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeAuditLogService{})
	handler := NewHandler(svc)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1"}),
	})
	req := ptrext.Of(attunev1.ListAuditLogRequest{
		Action:     "member.remove",
		ActorId:    "user-7",
		TargetType: "member",
	})

	_, err := handler.List(ctx, req)

	require.NoError(t, err)
	require.Equal(t, "tenant-1", svc.filter.TenantID)
	require.Equal(t, []string{"member.remove"}, svc.filter.Actions)
	require.Equal(t, "user-7", svc.filter.ActorID)
	require.Equal(t, "member", svc.filter.TargetType)
	require.False(t, svc.filter.Unbounded)
}

func TestListIncludesNextCursor(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeAuditLogService{
		nextCursor: "1718539200000000000:42",
	})
	handler := NewHandler(svc)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1"}),
	})

	res, err := handler.List(ctx, ptrext.Of(attunev1.ListAuditLogRequest{}))

	require.NoError(t, err)
	require.Equal(t, "1718539200000000000:42", res.Body.GetNextCursor())
}

func TestBindListRequestBindsCursor(t *testing.T) {
	t.Parallel()

	req := ptrext.Of(attunev1.ListAuditLogRequest{})
	err := BindListRequest(httptest.NewRequest(http.MethodGet,
		"/fb/v1/console/audit-log?cursor=1718539200000000000:42", nil), req)
	require.NoError(t, err)
	require.Equal(t, "1718539200000000000:42", req.GetCursor())
}

func TestBuildFilterIncludesAllActions(t *testing.T) {
	t.Parallel()

	filter, err := buildFilter("tenant-1", ptrext.Of(attunev1.ListAuditLogRequest{
		Action:  "member.invite",
		Actions: []string{"member.remove", "member.invite"},
	}))

	require.NoError(t, err)
	require.Equal(t, []string{"member.remove", "member.invite"}, filter.Actions)
}

func TestExportCSVQuotesJSONColumns(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeAuditLogService{
		rows: []auditlogrepo.Entry{{
			ID:             1,
			ActorType:      "admin",
			ActorID:        "user-1",
			ActorUserAgent: "curl/8.0",
			Action:         "notify_target.update",
			TargetType:     "notify_target",
			TargetID:       "target-1",
			Summary:        "Updated notify target",
			BeforeJSON:     []byte(`{"url":"https://example.com/hook"}`),
			AfterJSON:      []byte(`{"url":"https://example.com/next"}`),
			CreatedAt:      time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		}},
	})
	handler := NewHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/fb/v1/console/audit-log/export.csv", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{TenantID: "tenant-1"})))
	rec := httptest.NewRecorder()

	handler.ExportCSV(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "actor_user_agent")
	require.Contains(t, rec.Body.String(), "curl/8.0")
	require.True(t, strings.Contains(rec.Body.String(), `"{""url"":""https://example.com/hook""}"`))
}

func TestToProto_MapsAllFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 15, 10, 30, 0, 0, time.UTC)
	entry := auditlogrepo.Entry{
		ID:             1,
		ActorType:      "admin",
		ActorID:        "user-42",
		ActorEmail:     "admin@example.com",
		ActorIP:        "10.0.0.1",
		ActorUserAgent: "curl/8.0",
		Action:         "created",
		TargetType:     "notify_target",
		TargetID:       "target-1",
		Summary:        "created webhook",
		BeforeJSON:     []byte(`{"url":"old"}`),
		AfterJSON:      []byte(`{"url":"new"}`),
		CreatedAt:      now,
	}
	out := toProto(entry)
	require.Equal(t, int64(1), out.GetId())
	require.Equal(t, "admin", out.GetActorType())
	require.Equal(t, "user-42", out.GetActorId())
	require.Equal(t, "admin@example.com", out.GetActorEmail())
	require.Equal(t, "10.0.0.1", out.GetActorIp())
	require.Equal(t, "curl/8.0", out.GetActorUserAgent())
	require.Equal(t, "created", out.GetAction())
	require.Equal(t, "notify_target", out.GetTargetType())
	require.Equal(t, "target-1", out.GetTargetId())
	require.Equal(t, "created webhook", out.GetSummary())
	require.Equal(t, `{"url":"old"}`, out.GetBeforeJson())
	require.Equal(t, `{"url":"new"}`, out.GetAfterJson())
	require.Contains(t, out.GetCreatedAt(), "2026-06-15")
}

func TestListSavedViewsMapsSavedViews(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 10, 30, 0, 0, time.UTC)
	handler := NewHandler(ptrext.Of(fakeAuditLogService{}))
	handler.SetSavedViewService(ptrext.Of(fakeSavedViewService{
		listViews: []auditlogviewsvc.View{{
			ID:        "view-1",
			Name:      "Investigation",
			State:     auditlogviewsvc.State{Actions: []string{"member.remove"}, LocalQuery: "playwright"},
			CreatedAt: now.Add(-time.Hour),
			UpdatedAt: now,
		}},
	}))
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	})

	res, err := handler.ListSavedViews(ctx, ptrext.Of(attunev1.ListSavedAuditLogViewsRequest{}))
	require.NoError(t, err)
	require.Len(t, res.Body.GetItems(), 1)
	got := res.Body.GetItems()[0]
	require.Equal(t, "view-1", got.GetId())
	require.Equal(t, "Investigation", got.GetName())
	require.Equal(t, []string{"member.remove"}, got.GetState().GetActions())
	require.Equal(t, "playwright", got.GetState().GetLocalQuery())
}

func TestCreateSavedViewDelegatesToService(t *testing.T) {
	t.Parallel()

	savedSvc := ptrext.Of(fakeSavedViewService{})
	handler := NewHandler(ptrext.Of(fakeAuditLogService{}))
	handler.SetSavedViewService(savedSvc)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	})

	res, err := handler.CreateSavedView(ctx, ptrext.Of(attunev1.CreateSavedAuditLogViewRequest{
		Name: "Investigation",
		State: ptrext.Of(attunev1.AuditLogViewState{
			Actions:    []string{"member.remove"},
			LocalQuery: "playwright",
		}),
	}))
	require.NoError(t, err)
	require.Equal(t, "Investigation", savedSvc.saveInput.Name)
	require.Equal(t, []string{"member.remove"}, savedSvc.saveInput.State.Actions)
	require.Equal(t, "user-1", savedSvc.saveInput.UpdatedBy)
	require.Equal(t, "view-1", res.Body.GetView().GetId())
}

func TestUpdateSavedViewDelegatesToService(t *testing.T) {
	t.Parallel()

	savedSvc := ptrext.Of(fakeSavedViewService{})
	handler := NewHandler(ptrext.Of(fakeAuditLogService{}))
	handler.SetSavedViewService(savedSvc)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	})

	res, err := handler.UpdateSavedView(ctx, ptrext.Of(attunev1.UpdateSavedAuditLogViewRequest{
		Id:   "view-1",
		Name: "Investigation",
		State: ptrext.Of(attunev1.AuditLogViewState{
			TargetType: "notify_target",
			TargetId:   "target-1",
		}),
	}))

	require.NoError(t, err)
	require.Equal(t, "view-1", savedSvc.saveInput.ID)
	require.Equal(t, "Investigation", savedSvc.saveInput.Name)
	require.Equal(t, "notify_target", savedSvc.saveInput.State.TargetType)
	require.Equal(t, "target-1", savedSvc.saveInput.State.TargetID)
	require.Equal(t, "view-1", res.Body.GetView().GetId())
}

func TestDeleteSavedViewDelegatesToService(t *testing.T) {
	t.Parallel()

	savedSvc := ptrext.Of(fakeSavedViewService{})
	handler := NewHandler(ptrext.Of(fakeAuditLogService{}))
	handler.SetSavedViewService(savedSvc)
	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	})

	_, err := handler.DeleteSavedView(ctx, ptrext.Of(attunev1.DeleteSavedAuditLogViewRequest{Id: "view-1"}))
	require.NoError(t, err)
	require.Equal(t, "view-1", savedSvc.deleteID)
	require.Equal(t, "user-1", savedSvc.deleteBy)
}

func TestCollectQueryValues_MultipleCommaValues(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/?action=a,b&action=c", nil)
	got := collectQueryValues(req.URL.Query(), "action")
	require.ElementsMatch(t, []string{"a", "b", "c"}, got)
}

func TestCollectQueryValues_EmptyKey(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	got := collectQueryValues(req.URL.Query(), "action")
	require.Empty(t, got)
}

func TestBuildFilter_ValidDates(t *testing.T) {
	t.Parallel()
	req := ptrext.Of(attunev1.ListAuditLogRequest{
		From: "2026-01-01T00:00:00Z",
		To:   "2026-12-31T23:59:59Z",
	})
	f, err := buildFilter("t1", req)
	require.NoError(t, err)
	require.NotNil(t, f.From)
	require.NotNil(t, f.To)
}

func TestBuildFilter_InvalidFrom(t *testing.T) {
	t.Parallel()
	req := ptrext.Of(attunev1.ListAuditLogRequest{
		From: "not-a-date",
	})
	_, err := buildFilter("t1", req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "from must be RFC3339")
}

func TestBuildFilter_InvalidTo(t *testing.T) {
	t.Parallel()
	req := ptrext.Of(attunev1.ListAuditLogRequest{
		To: "not-a-date",
	})
	_, err := buildFilter("t1", req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "to must be RFC3339")
}

func TestBuildFilter_InvalidCursor(t *testing.T) {
	t.Parallel()
	req := ptrext.Of(attunev1.ListAuditLogRequest{
		Cursor: ptrext.Of("bad-cursor"),
	})
	_, err := buildFilter("t1", req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cursor must be audit pagination token")
}

func TestFirstQueryValue_ReturnsFirst(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/?foo=bar&baz=qux", nil)
	got := firstQueryValue(req.URL.Query(), "missing", "foo")
	require.Equal(t, "bar", got)
}

func TestFirstQueryValue_AllEmpty(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	got := firstQueryValue(req.URL.Query(), "a", "b")
	require.Equal(t, "", got)
}
