// SPDX-License-Identifier: Apache-2.0

package timeout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMiddleware_NoTimeout(t *testing.T) {
	t.Parallel()
	handler := Middleware(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestMiddleware_ContextCancelled(t *testing.T) {
	t.Parallel()
	handler := Middleware(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			// Context was cancelled
		case <-time.After(time.Second):
			t.Fatal("timeout not enforced")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestMiddleware_ChecksContextDeadline(t *testing.T) {
	t.Parallel()
	var ctxDeadline time.Time

	handler := Middleware(100 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		require.True(t, ok)
		ctxDeadline = deadline
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, time.Now().Before(ctxDeadline))
}

func TestMiddlewareWithResponse_NoTimeout(t *testing.T) {
	t.Parallel()
	handler := MiddlewareWithResponse(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", rec.Body.String())
}

func TestMiddlewareWithResponse_Timeout(t *testing.T) {
	t.Parallel()
	handler := MiddlewareWithResponse(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "timeout")
}

func TestSyncMutex(t *testing.T) {
	t.Parallel()
	var m syncMutex

	m.Lock()
	locked := make(chan struct{})
	go func() {
		m.Lock()
		close(locked)
		m.Unlock()
	}()

	select {
	case <-locked:
		t.Fatal("mutex should be locked")
	case <-time.After(10 * time.Millisecond):
	}

	m.Unlock()

	select {
	case <-locked:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("goroutine should have acquired lock")
	}
}

func TestTimeoutWriter_SingleWrite(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	tw := &timeoutWriter{ResponseWriter: rec} // ptrext:allow test wrapper

	tw.WriteHeader(http.StatusOK)
	tw.WriteHeader(http.StatusBadRequest) // Should be ignored

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestContext_Cancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.Error(t, ctx.Err())
}
