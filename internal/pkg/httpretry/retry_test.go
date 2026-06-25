// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDo_Success(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := Do(context.Background(), http.DefaultClient, req, DefaultConfig())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

func TestDo_RetryOn503(t *testing.T) {
	t.Parallel()
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 { // ptrext:allow atomic out-param
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseDelay = 10 * time.Millisecond
	cfg.MaxDelay = 50 * time.Millisecond

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := Do(context.Background(), http.DefaultClient, req, cfg)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(3), atomic.LoadInt32(&attempts)) // ptrext:allow atomic out-param
	resp.Body.Close()
}

func TestDo_NoRetryOn400(t *testing.T) {
	t.Parallel()
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1) // ptrext:allow atomic out-param
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := Do(context.Background(), http.DefaultClient, req, DefaultConfig())

	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, int32(1), atomic.LoadInt32(&attempts)) // ptrext:allow atomic out-param
	resp.Body.Close()
}

func TestDo_ContextCancel(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := Do(ctx, http.DefaultClient, req, DefaultConfig())

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestDefaultRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status   int
		expected bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusNotFound, false},
		{http.StatusTooManyRequests, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{http.StatusBadGateway, true},
	}

	for _, tt := range tests {
		resp := &http.Response{StatusCode: tt.status} // ptrext:allow test helper
		result := DefaultRetryable(resp, nil)
		require.Equal(t, tt.expected, result, "status %d", tt.status)
	}
}
