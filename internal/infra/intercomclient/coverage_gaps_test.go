// SPDX-License-Identifier: Apache-2.0

// coverage_gaps_test.go closes unit-coverage gaps on the client's error
// legs: per-endpoint API failures, decode failures, encode failures, and
// the error-string formatting variants.
package intercomclient_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/infra/intercomclient"
)

// errorHandler responds with a fixed status/body to every request.
func errorHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	})
}

func TestEndpointErrorLegs(t *testing.T) {
	client := newTestClient(t, errorHandler(http.StatusBadGateway, `{"type":"error.list","errors":[{"code":"server_error"}]}`))
	ctx := context.Background()

	if _, err := client.SearchConversations(ctx, 0, 100, ""); err == nil {
		t.Error("SearchConversations: expected error")
	}
	if _, err := client.GetConversation(ctx, "42"); err == nil {
		t.Error("GetConversation: expected error")
	}
	if _, err := client.SearchContacts(ctx, []string{"c1"}); err == nil {
		t.Error("SearchContacts: expected error")
	}
	if _, err := client.ListAdmins(ctx); err == nil {
		t.Error("ListAdmins: expected error")
	}
	if _, err := client.GetCompany(ctx, "co-1"); err == nil {
		t.Error("GetCompany: expected error")
	}
}

func TestListAdmins_Success(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admins" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"type":"admin.list","admins":[{"type":"admin","id":"7","name":"Sam","email":"sam@acme.com"}]}`)
	}))
	admins, err := client.ListAdmins(context.Background())
	if err != nil {
		t.Fatalf("ListAdmins() error = %v", err)
	}
	if len(admins) != 1 || admins[0].Name != "Sam" {
		t.Errorf("admins = %+v", admins)
	}
}

func TestDoJSON_DecodeError(t *testing.T) {
	client := newTestClient(t, errorHandler(http.StatusOK, `{"admins": not-json`))
	_, err := client.ListAdmins(context.Background())
	var de intercomclient.DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("error = %v, want DecodeError", err)
	}
	if de.Truncated {
		t.Error("small body must not be flagged truncated")
	}
	if de.Unwrap() == nil {
		t.Error("DecodeError must expose the unmarshal cause")
	}
}

func TestDoJSON_TransportError(t *testing.T) {
	// Point at a server, then close it: Do() fails at the dial.
	client := newTestClient(t, errorHandler(http.StatusOK, `{}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ListAdmins(ctx); err == nil {
		t.Error("expected transport error on cancelled context")
	}
}

func TestExtractErrorCode_RawBodyFallbackTruncated(t *testing.T) {
	// Non-error.list body longer than 200 bytes: raw fallback is trimmed.
	longMsg := strings.Repeat("x", 300)
	client := newTestClient(t, errorHandler(http.StatusBadRequest, longMsg))
	_, err := client.ListAdmins(context.Background())
	var ae intercomclient.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("error = %v, want APIError", err)
	}
	if len(ae.Code) != 200 {
		t.Errorf("fallback code length = %d, want 200", len(ae.Code))
	}
}

func TestValidateHost_InvalidURL(t *testing.T) {
	// Control-character URLs fail url.Parse.
	if err := intercomclient.ValidateHost("http://bad\x7f.example"); err == nil {
		t.Error("expected parse error for control-character host")
	}
}

func TestAPIError_ErrorVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  intercomclient.APIError
		want string
	}{
		{"no code", intercomclient.APIError{Method: "/x"}, "intercom /x failed"},
		{"code and status", intercomclient.APIError{Method: "/x", Code: "unauthorized", Status: 401}, "intercom /x: unauthorized status=401"},
		{"code only", intercomclient.APIError{Method: "/x", Code: "unauthorized"}, "intercom /x: unauthorized"},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("%s: Error() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestDecodeError_ErrorVariants(t *testing.T) {
	t.Parallel()
	cause := errors.New("unexpected token")
	plain := intercomclient.DecodeError{Method: "/x", Err: cause}
	if got := plain.Error(); !strings.Contains(got, "decode: unexpected token") || strings.Contains(got, "truncated") {
		t.Errorf("plain Error() = %q", got)
	}
	trunc := intercomclient.DecodeError{Method: "/x", Truncated: true, Err: cause}
	if got := trunc.Error(); !strings.Contains(got, "truncated at size cap") {
		t.Errorf("truncated Error() = %q", got)
	}
}
