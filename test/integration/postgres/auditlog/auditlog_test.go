//go:build integration

package auditlog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/handlers/console/member"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_AuditLogRejectsUpdateDeleteTruncateAndUnknownAction(t *testing.T) {
	t.Parallel()

	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := insertTenant(t, ctx, pool, "audit-immutable")
	insertAuditRow(t, ctx, pool, auditRow{
		tenantID:   tenantID,
		actorType:  "admin",
		actorID:    "user-1",
		action:     "member.invite",
		targetType: "member",
		targetID:   "member-1",
		createdAt:  time.Now().UTC(),
	})

	_, err := pool.Exec(ctx, `UPDATE audit_log SET summary = 'tampered'`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "append-only")

	_, err = pool.Exec(ctx, `DELETE FROM audit_log`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retention prune")

	_, err = pool.Exec(ctx, `TRUNCATE audit_log`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "append-only")

	_, err = pool.Exec(ctx, `
		INSERT INTO audit_log (
			tenant_id, actor_type, actor_id, actor_email, actor_ip, actor_user_agent,
			action, target_type, target_id, summary
		) VALUES ($1, 'admin', 'user-1', '', '', '', 'unknown.action', 'member', 'member-2', 'bad action')
	`, tenantID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "chk_audit_action_value")
}

func TestPG_AuditLogListFiltersByActionActorAndDateRange(t *testing.T) {
	t.Parallel()

	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := insertTenant(t, ctx, pool, "audit-filters")
	memberRepo := tenantmember.NewRepo(pool)
	insertAdminMember(t, ctx, memberRepo, tenantID, "user-1", "owner@example.com")

	base := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	insertAuditRow(t, ctx, pool, auditRow{
		tenantID:   tenantID,
		actorType:  "admin",
		actorID:    "user-1",
		action:     "member.invite",
		targetType: "member",
		targetID:   "member-a",
		createdAt:  base.Add(-2 * time.Hour),
	})
	insertAuditRow(t, ctx, pool, auditRow{
		tenantID:   tenantID,
		actorType:  "admin",
		actorID:    "user-2",
		action:     "api_key.create",
		targetType: "api_key",
		targetID:   "key-a",
		createdAt:  base.Add(-time.Hour),
	})
	insertAuditRow(t, ctx, pool, auditRow{
		tenantID:   tenantID,
		actorType:  "admin",
		actorID:    "user-1",
		action:     "member.invite",
		targetType: "member",
		targetID:   "member-b",
		createdAt:  base,
	})

	mux, signer := newAuditRouter(t, pool)

	t.Run("action", func(t *testing.T) {
		rec := getJSON(t, mux, signer, tenantID, "user-1", "/audit-log?action=member.invite")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		items := decodeItems(t, rec)
		require.Len(t, items, 2)
		for _, item := range items {
			require.Equal(t, "member.invite", item["action"])
		}
	})

	t.Run("action-multi", func(t *testing.T) {
		rec := getJSON(t, mux, signer, tenantID, "user-1", "/audit-log?action=member.invite&action=api_key.create")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		items := decodeItems(t, rec)
		require.Len(t, items, 3)
	})

	t.Run("actor", func(t *testing.T) {
		rec := getJSON(t, mux, signer, tenantID, "user-1", "/audit-log?actorId=user-2")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		items := decodeItems(t, rec)
		require.Len(t, items, 1)
		require.Equal(t, "user-2", items[0]["actorId"])
		require.Equal(t, "api_key.create", items[0]["action"])
	})

	t.Run("date-range", func(t *testing.T) {
		from := base.Add(-90 * time.Minute).Format(time.RFC3339)
		to := base.Add(-30 * time.Minute).Format(time.RFC3339)
		rec := getJSON(t, mux, signer, tenantID, "user-1", "/audit-log?from="+from+"&to="+to)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		items := decodeItems(t, rec)
		require.Len(t, items, 1)
		require.Equal(t, "api_key.create", items[0]["action"])
	})

	t.Run("cursor-pagination", func(t *testing.T) {
		rec := getJSON(t, mux, signer, tenantID, "user-1", "/audit-log?limit=2")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		items := decodeItems(t, rec)
		require.Len(t, items, 2)

		nextCursor := decodeNextCursor(t, rec)
		require.NotEmpty(t, nextCursor)

		rec = getJSON(t, mux, signer, tenantID, "user-1", "/audit-log?limit=2&cursor="+nextCursor)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		items = decodeItems(t, rec)
		require.Len(t, items, 1)
		require.Equal(t, "member-a", items[0]["targetId"])
	})
}

func TestPG_AuditLogPruneRemovesRowsPastRetention(t *testing.T) {
	t.Parallel()

	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := insertTenant(t, ctx, pool, "audit-prune")
	svc := auditlogsvc.New(auditlogrepo.New(pool))

	insertAuditRow(t, ctx, pool, auditRow{
		tenantID:   tenantID,
		actorType:  "admin",
		actorID:    "user-1",
		action:     "member.invite",
		targetType: "member",
		targetID:   "old-member",
		createdAt:  time.Now().UTC().Add(-370 * 24 * time.Hour),
	})
	insertAuditRow(t, ctx, pool, auditRow{
		tenantID:   tenantID,
		actorType:  "admin",
		actorID:    "user-1",
		action:     "member.invite",
		targetType: "member",
		targetID:   "recent-member",
		createdAt:  time.Now().UTC().Add(-7 * 24 * time.Hour),
	})

	rows, err := svc.Prune(ctx, 365*24*time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	listed, err := auditlogrepo.New(pool).List(ctx, auditlogrepo.ListFilter{
		TenantID:  tenantID,
		Unbounded: true,
	})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, "recent-member", listed.Items[0].TargetID)
}

func TestPG_InviteMemberWritesAuditRowWithBeforeAfter(t *testing.T) {
	t.Parallel()

	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := insertTenant(t, ctx, pool, "audit-member-invite")
	memberRepo := tenantmember.NewRepo(pool)
	insertAdminMember(t, ctx, memberRepo, tenantID, "user-1", "owner@example.com")

	mux, signer := newMemberRouter(t, pool)
	req := authedJSONRequest(t, signer, http.MethodPost, "/members/", `{"email":"new@example.com","role":"member"}`, tenantID, "user-1")
	req.Header.Set("User-Agent", "audit-e2e-test")
	req.RemoteAddr = "203.0.113.10:4321"
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rows, err := auditlogrepo.New(pool).List(ctx, auditlogrepo.ListFilter{
		TenantID:  tenantID,
		Actions:   []string{"member.invite"},
		Unbounded: true,
	})
	require.NoError(t, err)
	require.Len(t, rows.Items, 1)

	row := rows.Items[0]
	require.Equal(t, "admin", row.ActorType)
	require.Equal(t, "user-1", row.ActorID)
	require.Equal(t, "203.0.113.10", row.ActorIP)
	require.Equal(t, "audit-e2e-test", row.ActorUserAgent)
	require.Equal(t, "member", row.TargetType)
	require.Empty(t, row.BeforeJSON)

	var after map[string]any
	require.NoError(t, json.Unmarshal(row.AfterJSON, &after))
	require.Equal(t, "new@example.com", after["email"])
	require.Equal(t, "member", after["role"])
	require.Equal(t, "invite", after["member_type"])
	require.Equal(t, row.TargetID, after["id"])
}

type auditRow struct {
	tenantID   string
	actorType  string
	actorID    string
	action     string
	targetType string
	targetID   string
	createdAt  time.Time
}

func newAuditRouter(t *testing.T, pool *pgxpool.Pool) (http.Handler, *console.Signer) {
	t.Helper()
	signer, err := console.NewSigner("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)

	router := console.NewRouter(
		signer,
		nil,
		nil,
		nil,
		console.NewAuditLogHandler(auditlogsvc.New(auditlogrepo.New(pool))),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		tenantmember.NewRepo(pool),
	)
	return router.Mount(), signer
}

func newMemberRouter(t *testing.T, pool *pgxpool.Pool) (http.Handler, *console.Signer) {
	t.Helper()
	signer, err := console.NewSigner("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)

	memberRepo := tenantmember.NewRepo(pool)
	memberHandler := member.NewHandler(memberRepo)
	memberHandler.SetAuditLogger(auditlogsvc.New(auditlogrepo.New(pool)))

	router := console.NewRouter(
		signer,
		nil,
		nil,
		nil,
		console.NewAuditLogHandler(auditlogsvc.New(auditlogrepo.New(pool))),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		memberHandler,
		nil,
		memberRepo,
	)
	return router.Mount(), signer
}

func insertTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug string) string {
	t.Helper()

	const tenantID = "tenant-1"
	_, err := pool.Exec(ctx, `
		INSERT INTO tenants (id, slug, name)
		VALUES ($1, $2, $3)
	`, tenantID, slug, "Audit Log Test")
	require.NoError(t, err)
	return tenantID
}

func insertAdminMember(t *testing.T, ctx context.Context, repo *tenantmember.Repo, tenantID, userID, email string) tenantmember.Member {
	t.Helper()

	now := time.Now().UTC()
	return mustCreateMember(t, repo, ctx, tenantmember.Member{
		TenantID:   tenantID,
		MemberType: "admin",
		UserID:     userID,
		Email:      ptrext.Of(email),
		Role:       domain.RoleAdmin,
		RoleSource: "bootstrap",
		InvitedAt:  now,
		AcceptedAt: ptrext.Of(now),
	})
}

func mustCreateMember(t *testing.T, repo *tenantmember.Repo, ctx context.Context, m tenantmember.Member) tenantmember.Member {
	t.Helper()

	created, err := repo.Create(ctx, m)
	require.NoError(t, err)
	return created
}

func insertAuditRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, row auditRow) int64 {
	t.Helper()

	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO audit_log (
			tenant_id, actor_type, actor_id, actor_email, actor_ip, actor_user_agent,
			action, target_type, target_id, summary, created_at
		) VALUES ($1, $2, $3, '', '', '', $4, $5, $6, 'integration fixture', $7)
		RETURNING id
	`, row.tenantID, row.actorType, row.actorID, row.action, row.targetType, row.targetID, row.createdAt).Scan(&id)
	require.NoError(t, err)
	return id
}

func getJSON(t *testing.T, mux http.Handler, signer *console.Signer, tenantID, userID, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := authedJSONRequest(t, signer, http.MethodGet, path, "", tenantID, userID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func authedJSONRequest(t *testing.T, signer *console.Signer, method, path, body, tenantID, userID string) *http.Request {
	t.Helper()

	var reader *bytes.Buffer
	if body == "" {
		reader = bytes.NewBuffer(nil)
	} else {
		reader = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-CSRF-Token", signer.CSRFToken(userID))

	w := httptest.NewRecorder()
	err := signer.IssueSessionCookieWithType(cookieSinkRecorder{ResponseRecorder: w}, tenantID, userID, "admin")
	require.NoError(t, err)

	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies)
	req.AddCookie(cookies[0])
	return req
}

type cookieSinkRecorder struct {
	*httptest.ResponseRecorder
}

func (c cookieSinkRecorder) SetCookie(cookie *http.Cookie) {
	http.SetCookie(c.ResponseRecorder, cookie)
}

func decodeItems(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()

	var body map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)

	rawItems, ok := body["items"].([]any)
	require.True(t, ok, "response body missing items: %s", rec.Body.String())

	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		item, ok := raw.(map[string]any)
		require.True(t, ok, "item has unexpected type: %T", raw)
		items = append(items, item)
	}
	return items
}

func decodeNextCursor(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)

	nextCursor, _ := body["nextCursor"].(string)
	return nextCursor
}
