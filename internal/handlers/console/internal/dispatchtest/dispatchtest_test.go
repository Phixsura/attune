package dispatchtest

import (
	"net/http"
	"strings"
	"testing"
)

func TestRequestInstallsAuthAndRouteParams(t *testing.T) {
	t.Parallel()

	req := Request(http.MethodPost, "/things/123", `{"ok":true}`, Param{Name: "id", Value: "123"})
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", req.Method)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	auth := Auth(req.Context())
	if auth.TenantID != TenantID || auth.UserID != UserID {
		t.Fatalf("auth = %+v, want tenant/user defaults", auth)
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	got, err := DecodeJSON(strings.NewReader(`{"ok":true,"n":2}`))
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v, want nil", err)
	}
	if got["ok"] != true || got["n"] != float64(2) {
		t.Fatalf("DecodeJSON() = %+v", got)
	}

	if _, err := DecodeJSON(strings.NewReader(`{`)); err == nil {
		t.Fatal("DecodeJSON(invalid) error = nil, want error")
	}
}
