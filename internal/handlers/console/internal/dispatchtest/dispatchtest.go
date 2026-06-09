package dispatchtest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	TenantID = "tenant-1"
	UserID   = "user-1"
)

type Param struct {
	Name  string
	Value string
}

func Auth(ctx context.Context) *session.AuthCtx {
	return session.FromContext(ctx)
}

func Request(method, target, body string, params ...Param) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	ctx := session.WithAuthCtx(r.Context(), ptrext.Of(session.AuthCtx{
		TenantID: TenantID,
		UserID:   UserID,
	}))
	if len(params) > 0 {
		rctx := chi.NewRouteContext()
		for _, p := range params {
			rctx.URLParams.Add(p.Name, p.Value)
		}
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	}
	return r.WithContext(ctx)
}

func DecodeJSON(r io.Reader) (map[string]any, error) {
	var out map[string]any
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
