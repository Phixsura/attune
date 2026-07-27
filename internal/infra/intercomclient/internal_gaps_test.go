// SPDX-License-Identifier: Apache-2.0

// internal_gaps_test.go covers the HTTP helper error legs that are not
// reachable through the public API surface: encode failures, request
// construction failures, and mid-body read failures.
package intercomclient

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func newInternalClient(t *testing.T, handler http.Handler) *httpClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	SetAPIBaseURL(srv.URL)
	t.Cleanup(func() {
		srv.Close()
		SetAPIBaseURL("")
		SetEgressPolicy(nethardening.Policy{})
	})
	return New("us", "tok").(*httpClient)
}

func TestPostJSON_EncodeError(t *testing.T) {
	c := newInternalClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	// A channel cannot be JSON-marshalled.
	err := c.postJSON(context.Background(), "/x", make(chan int), ptrext.Of(struct{}{}))
	if err == nil || !strings.Contains(err.Error(), "encode") {
		t.Errorf("postJSON encode error = %v, want encode failure", err)
	}
}

func TestDoJSON_BadMethodRequestError(t *testing.T) {
	c := newInternalClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	// A method with a space fails http.NewRequestWithContext.
	err := c.doJSON(context.Background(), "BAD METHOD", "/x", nil, ptrext.Of(struct{}{}))
	if err == nil {
		t.Error("expected request construction error")
	}
}

func TestDoJSON_BodyReadError(t *testing.T) {
	// Declare a long body, write a fragment, then sever the connection:
	// io.ReadAll fails mid-body.
	c := newInternalClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetLinger(0) // RST instead of FIN — read errors immediately
		}
		_ = conn.Close()
	}))
	err := c.doJSON(context.Background(), http.MethodGet, "/x", nil, ptrext.Of(struct{}{}))
	if err == nil {
		t.Error("expected mid-body read error")
	}
}
