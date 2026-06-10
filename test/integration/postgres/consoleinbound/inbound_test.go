//go:build integration

package consoleinbound_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_Delete_RemovesInboundSource(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	insertTenant(t, ctx, pool)
	id := insertSource(t, ctx, pool, true)
	repo := inboundsource.NewRepo(pool)
	mux, signer := newConsoleRouter(t, pool)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedRequest(t, signer, http.MethodDelete, "/inbound/sources/"+id))
	if w.Code != http.StatusOK {
		t.Fatalf("Delete status: got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := repo.Get(ctx, id); !errors.Is(err, inboundsource.ErrNotFound) {
		t.Fatalf("Get deleted source: got %v want %v", err, inboundsource.ErrNotFound)
	}
}

func TestPG_Delete_RaceLostReturns404(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	insertTenant(t, ctx, pool)
	id := insertSource(t, ctx, pool, true)
	installDeleteSuppressingTrigger(t, ctx, pool)
	mux, signer := newConsoleRouter(t, pool)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedRequest(t, signer, http.MethodDelete, "/inbound/sources/"+id))
	if w.Code != http.StatusNotFound {
		t.Fatalf("Delete race status: got %d want 404 body=%s", w.Code, w.Body.String())
	}
}

func newConsoleRouter(t *testing.T, pool *pgxpool.Pool) (http.Handler, *console.Signer) {
	t.Helper()
	signer, err := console.NewSigner("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	inbound := console.NewInboundHandler(
		inboundsource.NewRepo(pool),
		pool,
		inboundtest.FakeSecrets{},
		"https://attune.example.com",
	)
	return console.NewRouter(
		signer,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		inbound,
	).Mount(), signer
}

func authedRequest(t *testing.T, signer *console.Signer, method, urlStr string) *http.Request {
	t.Helper()
	const userID = "user-1"
	r := httptest.NewRequest(method, urlStr, nil)
	r.Header.Set("X-CSRF-Token", signer.CSRFToken(userID))
	w := httptest.NewRecorder()
	if err := signer.IssueSessionCookie(cookieSinkRecorder{ResponseRecorder: w}, "tenant-1", userID); err != nil {
		t.Fatalf("IssueSessionCookie: %v", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("IssueSessionCookie did not set a cookie")
	}
	r.AddCookie(cookies[0])
	return r
}

type cookieSinkRecorder struct {
	*httptest.ResponseRecorder
}

func (c cookieSinkRecorder) SetCookie(cookie *http.Cookie) {
	http.SetCookie(c.ResponseRecorder, cookie)
}

func insertTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO tenants (id, slug, name)
		VALUES ('tenant-1', 'inbound-delete', 'Inbound Delete')`,
	); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
}

func insertSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, enabled bool) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO inbound_sources (id, tenant_id, channel, name, slug, config, enabled)
		VALUES ($1, 'tenant-1', 'webhook', 'Webhook Delete', 'webhook-delete', $2, $3)`,
		id, []byte("{}"), enabled,
	); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	return id
}

func installDeleteSuppressingTrigger(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION suppress_inbound_source_delete()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			RETURN NULL;
		END
		$$;
		CREATE TRIGGER suppress_inbound_source_delete
		BEFORE DELETE ON inbound_sources
		FOR EACH ROW EXECUTE FUNCTION suppress_inbound_source_delete();`,
	); err != nil {
		t.Fatalf("install delete-suppressing trigger: %v", err)
	}
}
