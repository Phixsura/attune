// SPDX-License-Identifier: Apache-2.0

package console

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/idempotency"
)

func TestAPIKeyIdempotencyCanonicalizationAndHashing(t *testing.T) {
	t.Parallel()

	empty, err := canonicalizeAPIKeyIdempotencyBody("application/json", []byte("   "))
	require.NoError(t, err)
	require.Nil(t, empty)

	raw, err := canonicalizeAPIKeyIdempotencyBody("text/plain", []byte("  hello  "))
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), raw)

	canonical, err := canonicalizeAPIKeyIdempotencyBody("application/json; charset=utf-8", []byte(`{"b":2,"a":1}`))
	require.NoError(t, err)
	require.Equal(t, []byte(`{"a":1,"b":2}`), canonical)

	_, err = canonicalizeAPIKeyIdempotencyBody("application/json", []byte(`{`))
	require.Error(t, err)

	h1, err := hashAPIKeyIdempotencyRequest(http.MethodPost, "/v1/x", "application/json", []byte(`{"a":1,"b":2}`))
	require.NoError(t, err)
	h2, err := hashAPIKeyIdempotencyRequest(http.MethodPost, "/v1/x", "application/json", []byte(`{"b":2,"a":1}`))
	require.NoError(t, err)
	require.Equal(t, h1, h2)
	h3, err := hashAPIKeyIdempotencyRequest(http.MethodPatch, "/v1/x", "application/json", []byte(`{"a":1,"b":2}`))
	require.NoError(t, err)
	require.NotEqual(t, h1, h3)
}

func TestAPIKeyIdempotencyStatusAndCachedResponseHelpers(t *testing.T) {
	t.Parallel()

	require.False(t, shouldCacheAPIKeyIdempotentStatus(0))
	require.False(t, shouldCacheAPIKeyIdempotentStatus(http.StatusRequestTimeout))
	require.False(t, shouldCacheAPIKeyIdempotentStatus(http.StatusTooManyRequests))
	require.False(t, shouldCacheAPIKeyIdempotentStatus(http.StatusInternalServerError))
	require.True(t, shouldCacheAPIKeyIdempotentStatus(http.StatusCreated))

	rec := httptest.NewRecorder()
	writeCachedAPIKeyIdempotentResponse(rec, ptrext.Of(idempotency.Key{
		Status:       idempotency.StatusCompleted,
		ResponseCode: http.StatusAccepted,
		ResponseBody: []byte(`{"ok":true}`),
	}))
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"ok":true}`, rec.Body.String())

	rec = httptest.NewRecorder()
	rec.Header().Set("Content-Type", "text/plain")
	writeCachedAPIKeyIdempotentResponse(rec, ptrext.Of(idempotency.Key{ResponseCode: http.StatusNoContent}))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
}

func TestBufferedResponseWriterCapturesFirstStatusAndBody(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	w := newBufferedResponseWriter(rec)
	w.Header().Set("X-Test", "yes")
	w.WriteHeader(http.StatusCreated)
	w.WriteHeader(http.StatusAccepted)
	n, err := w.Write([]byte(`{"ok":true}`))
	require.NoError(t, err)
	require.Equal(t, len(`{"ok":true}`), n)
	require.Equal(t, http.StatusCreated, w.status)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "yes", rec.Header().Get("X-Test"))
	require.JSONEq(t, `{"ok":true}`, w.body.String())
}

func TestWithAPIKeyIdempotencyBypassAndValidation(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/v1/test", strings.NewReader(`{"ok":true}`))
	rec := httptest.NewRecorder()
	called := false
	withAPIKeyIdempotency(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	})).ServeHTTP(rec, req)
	require.True(t, called)
	require.Equal(t, http.StatusCreated, rec.Code)

	store := ptrext.Of(fakeAPIKeyIdempotencyStore{})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/test", strings.NewReader(`{"ok":true}`))
	req.Header.Set("Idempotency-Key", "bad")
	withAPIKeyIdempotency(store)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next should not be called")
	})).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	rec = httptest.NewRecorder()
	req = apiKeyIdempotencyRequest(`{"ok":true}`, false)
	withAPIKeyIdempotency(store)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next should not be called")
	})).ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWithAPIKeyIdempotencyCachedAndPendingStates(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		key  *idempotency.Key
		want int
		body string
	}{
		{name: "completed", key: ptrext.Of(idempotency.Key{Status: idempotency.StatusCompleted, ResponseCode: http.StatusOK, ResponseBody: []byte(`{"cached":true}`)}), want: http.StatusOK, body: "cached"},
		{name: "pending", key: ptrext.Of(idempotency.Key{Status: idempotency.StatusPending}), want: http.StatusConflict, body: "REQUEST_IN_PROGRESS"},
		{name: "invalid", key: ptrext.Of(idempotency.Key{Status: idempotency.StatusFailed}), want: http.StatusInternalServerError, body: "INTERNAL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := ptrext.Of(fakeAPIKeyIdempotencyStore{key: tc.key, acquired: false})
			rec := httptest.NewRecorder()
			withAPIKeyIdempotency(store)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("next should not be called")
			})).ServeHTTP(rec, apiKeyIdempotencyRequest(`{"ok":true}`, true))
			require.Equal(t, tc.want, rec.Code)
			require.Contains(t, rec.Body.String(), tc.body)
		})
	}
}

func TestAcquireAPIKeyIdempotencyRetriesExpiredKey(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(fakeAPIKeyIdempotencyStore{
		acquireErrs: []error{idempotency.ErrExpired, nil},
		key:         ptrext.Of(idempotency.Key{Status: idempotency.StatusPending}),
		acquired:    true,
	})
	req := apiKeyIdempotencyRequest(`{"ok":true}`, true)
	key, acquired, err := acquireAPIKeyIdempotency(req, store, "tenant-1", "idem_key1", []byte("hash"))
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, key)
	require.Equal(t, 2, store.acquireCalls)
	require.Equal(t, 1, store.deleteCalls)
}

func TestWithAPIKeyIdempotencyAcquireErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "hash mismatch", err: idempotency.ErrHashMismatch, want: http.StatusConflict},
		{name: "generic", err: errors.New("db down"), want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := ptrext.Of(fakeAPIKeyIdempotencyStore{acquireErrs: []error{tc.err}})
			rec := httptest.NewRecorder()
			withAPIKeyIdempotency(store)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("next should not be called")
			})).ServeHTTP(rec, apiKeyIdempotencyRequest(`{"ok":true}`, true))
			require.Equal(t, tc.want, rec.Code)
		})
	}
}

func TestWithAPIKeyIdempotencyCompletesOrFailsAfterHandler(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(fakeAPIKeyIdempotencyStore{
		key:      ptrext.Of(idempotency.Key{Status: idempotency.StatusPending}),
		acquired: true,
	})
	rec := httptest.NewRecorder()
	withAPIKeyIdempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true}`))
	})).ServeHTTP(rec, apiKeyIdempotencyRequest(`{"ok":true}`, true))
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, 1, store.completeCalls)
	require.Equal(t, 0, store.failCalls)
	require.Equal(t, []byte(`{"created":true}`), store.completedBody)

	store = ptrext.Of(fakeAPIKeyIdempotencyStore{
		key:      ptrext.Of(idempotency.Key{Status: idempotency.StatusPending}),
		acquired: true,
	})
	rec = httptest.NewRecorder()
	withAPIKeyIdempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})).ServeHTTP(rec, apiKeyIdempotencyRequest(`{"ok":true}`, true))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, 0, store.completeCalls)
	require.Equal(t, 1, store.failCalls)
}

func apiKeyIdempotencyRequest(body string, withAuth bool) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/test", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "idem_key1")
	req.Header.Set("Content-Type", "application/json")
	if withAuth {
		ctx := apikey.WithAuthForTest(req.Context(), "tenant-1", "11111111-1111-4111-8111-111111111111", nil)
		req = req.WithContext(ctx)
	}
	return req
}

type fakeAPIKeyIdempotencyStore struct {
	key         *idempotency.Key
	acquired    bool
	acquireErrs []error

	acquireCalls int
	deleteCalls  int

	completeCalls int
	completedBody []byte
	completeErr   error

	failCalls int
	failErr   error
}

func (f *fakeAPIKeyIdempotencyStore) Acquire(context.Context, string, string, []byte, time.Duration) (*idempotency.Key, bool, error) {
	f.acquireCalls++
	if len(f.acquireErrs) >= f.acquireCalls {
		if err := f.acquireErrs[f.acquireCalls-1]; err != nil {
			return nil, false, err
		}
	}
	return f.key, f.acquired, nil
}

func (f *fakeAPIKeyIdempotencyStore) Complete(_ context.Context, _ string, _ string, _ int, body []byte) error {
	f.completeCalls++
	f.completedBody = append(f.completedBody[:0], body...)
	return f.completeErr
}

func (f *fakeAPIKeyIdempotencyStore) Fail(context.Context, string, string) error {
	f.failCalls++
	return f.failErr
}

func (f *fakeAPIKeyIdempotencyStore) Get(context.Context, string, string) (*idempotency.Key, error) {
	return f.key, nil
}

func (f *fakeAPIKeyIdempotencyStore) DeleteExpired(context.Context, string, string) (bool, error) {
	f.deleteCalls++
	return true, nil
}

func (f *fakeAPIKeyIdempotencyStore) CleanupExpired(context.Context) (int64, error) {
	return 0, nil
}
