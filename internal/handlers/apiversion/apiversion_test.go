// SPDX-License-Identifier: Apache-2.0

package apiversion

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestMiddleware_DefaultsToCurrentVersion(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/feedback/ingest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, CurrentVersion, rec.Header().Get(HeaderName))
	require.True(t, varyContains(rec.Header(), HeaderName))
}

func TestMiddleware_AllowsSupportedDeprecatedVersion(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.SupportedVersions = []string{"2026-06-15", CurrentVersion}
	cfg.Deprecations = map[string]DeprecationNotice{
		"2026-06-15": {
			Deprecation: "@1751328000",
			Sunset:      "Thu, 31 Jul 2026 00:00:00 GMT",
		},
	}
	called := false
	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Vary", "Accept-Encoding")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tags", nil)
	req.Header.Set(HeaderName, "2026-06-15")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "2026-06-15", rec.Header().Get(HeaderName))
	require.Equal(t, "@1751328000", rec.Header().Get(DeprecationHeader))
	require.Equal(t, "Thu, 31 Jul 2026 00:00:00 GMT", rec.Header().Get(SunsetHeader))
	require.True(t, varyContains(rec.Header(), HeaderName))
	require.True(t, varyContains(rec.Header(), "Accept-Encoding"))
}

func TestMiddleware_RejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler must not run on unsupported versions")
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/feedback/ingest", nil)
	req.Header.Set(HeaderName, "2025-01-01")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CurrentVersion, rec.Header().Get(HeaderName))
	require.True(t, varyContains(rec.Header(), HeaderName))

	body := decodeBody(t, rec)
	require.Equal(t, "BAD_REQUEST", body["code"])
	require.Contains(t, body["message"], `unsupported X-Attune-Api-Version "2025-01-01"`)
	require.Contains(t, body["message"], CurrentVersion)
}

func TestMiddleware_RejectsMultipleVersionValues(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler must not run on ambiguous versions")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tags", nil)
	req.Header.Add(HeaderName, CurrentVersion)
	req.Header.Add(HeaderName, "2026-06-15")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeBody(t, rec)
	require.Equal(t, "BAD_REQUEST", body["code"])
	require.Contains(t, body["message"], "must appear at most once")
}

func TestMiddleware_RejectsEmptyVersionValue(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler must not run on empty versions")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tags", nil)
	req.Header.Set(HeaderName, "   ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeBody(t, rec)
	require.Equal(t, "BAD_REQUEST", body["code"])
	require.Contains(t, body["message"], "must not be empty")
}

func TestMiddleware_RejectsCommaSeparatedVersionValue(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler must not run on ambiguous versions")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tags", nil)
	req.Header.Set(HeaderName, CurrentVersion+", 2026-06-15")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeBody(t, rec)
	require.Equal(t, "BAD_REQUEST", body["code"])
	require.Contains(t, body["message"], "must contain exactly one version")
}

func TestMiddleware_NormalizesEmptyAndDuplicateConfigVersions(t *testing.T) {
	t.Parallel()

	handler := Middleware(Config{
		SupportedVersions: []string{"", CurrentVersion, " 2026-06-15 ", "2026-06-15"},
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tags", nil)
	req.Header.Set(HeaderName, "2026-06-15")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "2026-06-15", rec.Header().Get(HeaderName))
	require.Empty(t, rec.Header().Get(DeprecationHeader))
	require.Empty(t, rec.Header().Get(SunsetHeader))
}

func TestMiddleware_ClearsStaleDeprecationHeadersForCurrentVersion(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(DeprecationHeader, "stale")
		w.Header().Set(SunsetHeader, "stale")
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tags", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, rec.Header().Get(DeprecationHeader))
	require.Empty(t, rec.Header().Get(SunsetHeader))
}

func TestMiddleware_PreservesFlusher(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "wrapped writer must preserve http.Flusher")
		_, err := w.Write([]byte("chunk"))
		require.NoError(t, err)
		flusher.Flush()
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/audit-log/export.csv", nil)
	rec := newFlushRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, rec.flushed)
	require.Equal(t, CurrentVersion, rec.Header().Get(HeaderName))
	require.True(t, varyContains(rec.Header(), HeaderName))
}

func TestMiddleware_PreservesReaderFrom(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := io.Copy(w, plainReader{r: strings.NewReader("chunk")})
		require.NoError(t, err)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/gdpr/exports/job-1/download", nil)
	rec := newReaderFromRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, rec.readFromCalled)
	require.Equal(t, "chunk", rec.Body.String())
	require.Equal(t, CurrentVersion, rec.Header().Get(HeaderName))
	require.True(t, varyContains(rec.Header(), HeaderName))
}

func TestMiddleware_FallsBackWhenReaderFromIsUnsupported(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		readerFrom, ok := w.(io.ReaderFrom)
		require.True(t, ok, "wrapped writer must expose io.ReaderFrom fallback")
		n, err := readerFrom.ReadFrom(plainReader{r: strings.NewReader("fallback")})
		require.NoError(t, err)
		require.EqualValues(t, len("fallback"), n)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/gdpr/exports/job-1/download", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, "fallback", rec.Body.String())
	require.Equal(t, CurrentVersion, rec.Header().Get(HeaderName))
	require.True(t, varyContains(rec.Header(), HeaderName))
}

func TestMiddleware_PreservesHijacker(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("hijacked")
	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		require.True(t, ok, "wrapped writer must preserve http.Hijacker")
		_, _, err := hijacker.Hijack()
		require.ErrorIs(t, err, wantErr)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/mcp/clients", nil)
	rec := newHijackRecorder(wantErr)
	handler.ServeHTTP(rec, req)
}

func TestMiddleware_PreservesPusher(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pusher, ok := w.(http.Pusher)
		require.True(t, ok, "wrapped writer must preserve http.Pusher")
		err := pusher.Push("/assets/app.js", ptrext.Of(http.PushOptions{
			Header: http.Header{"Accept": []string{"application/javascript"}},
		}))
		require.NoError(t, err)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tags", nil)
	rec := newPushRecorder(nil)
	handler.ServeHTTP(rec, req)

	require.Equal(t, "/assets/app.js", rec.target)
	require.Equal(t, "application/javascript", rec.opts.Header.Get("Accept"))
	require.Equal(t, CurrentVersion, rec.Header().Get(HeaderName))
}

func TestMiddleware_PusherReturnsUnsupportedWhenUnderlyingWriterCannotPush(t *testing.T) {
	t.Parallel()

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pusher, ok := w.(http.Pusher)
		require.True(t, ok, "contract writer exposes http.Pusher")
		err := pusher.Push("/assets/app.js", nil)
		require.ErrorIs(t, err, http.ErrNotSupported)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tags", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestMiddleware_UnwrapReturnsUnderlyingWriter(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		require.True(t, ok)
		require.Same(t, rec, unwrapper.Unwrap())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tags", nil)
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, CurrentVersion, rec.Header().Get(HeaderName))
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func varyContains(h http.Header, want string) bool {
	for _, value := range h.Values("Vary") {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), want) {
				return true
			}
		}
	}
	return false
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func newFlushRecorder() *flushRecorder {
	return ptrext.Of(flushRecorder{ResponseRecorder: httptest.NewRecorder()})
}

func (r *flushRecorder) Flush() {
	r.flushed = true
}

type readerFromRecorder struct {
	*httptest.ResponseRecorder
	readFromCalled bool
}

func newReaderFromRecorder() *readerFromRecorder {
	return ptrext.Of(readerFromRecorder{ResponseRecorder: httptest.NewRecorder()})
}

func (r *readerFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.readFromCalled = true
	return io.Copy(r.Body, src)
}

type hijackRecorder struct {
	*httptest.ResponseRecorder
	err error
}

func newHijackRecorder(err error) *hijackRecorder {
	return ptrext.Of(hijackRecorder{ResponseRecorder: httptest.NewRecorder(), err: err})
}

func (r *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, r.err
}

type pushRecorder struct {
	*httptest.ResponseRecorder
	target string
	opts   *http.PushOptions
	err    error
}

func newPushRecorder(err error) *pushRecorder {
	return ptrext.Of(pushRecorder{ResponseRecorder: httptest.NewRecorder(), err: err})
}

func (r *pushRecorder) Push(target string, opts *http.PushOptions) error {
	r.target = target
	r.opts = opts
	return r.err
}

type plainReader struct {
	r *strings.Reader
}

func (r plainReader) Read(p []byte) (int, error) {
	return r.r.Read(p)
}
