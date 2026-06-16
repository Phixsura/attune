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
)

type fakeAuditLogService struct {
	filter     auditlogsvc.ListFilter
	nextCursor string
	rows       []auditlogrepo.Entry
}

func (f *fakeAuditLogService) List(_ context.Context, filter auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
	f.filter = filter
	return auditlogrepo.ListResult{Items: f.rows, NextCursor: f.nextCursor}, nil
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
