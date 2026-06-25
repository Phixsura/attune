// SPDX-License-Identifier: Apache-2.0

package ratelimitheaders

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHeaders_Write(t *testing.T) {
	t.Parallel()
	h := Headers{
		Limit:     100,
		Remaining: 95,
		Reset:     time.Unix(1704067200, 0),
	}

	rec := httptest.NewRecorder()
	h.Write(rec)

	require.Equal(t, "100", rec.Header().Get("X-RateLimit-Limit"))
	require.Equal(t, "95", rec.Header().Get("X-RateLimit-Remaining"))
	require.Equal(t, "1704067200", rec.Header().Get("X-RateLimit-Reset"))

	// New standard headers
	require.Equal(t, "100", rec.Header().Get("RateLimit-Limit"))
	require.Equal(t, "95", rec.Header().Get("RateLimit-Remaining"))
}

func TestHeaders_Write_WithRetryAfter(t *testing.T) {
	t.Parallel()
	h := Headers{
		Limit:      100,
		Remaining:  0,
		Reset:      time.Unix(1704067200, 0),
		RetryAfter: 60,
	}

	rec := httptest.NewRecorder()
	h.Write(rec)

	require.Equal(t, "60", rec.Header().Get("Retry-After"))
}

func TestHeaders_Write_NoRetryAfter(t *testing.T) {
	t.Parallel()
	h := Headers{
		Limit:     100,
		Remaining: 50,
		Reset:     time.Unix(1704067200, 0),
	}

	rec := httptest.NewRecorder()
	h.Write(rec)

	require.Empty(t, rec.Header().Get("Retry-After"))
}

type mockContext struct {
	val any
}

func (m mockContext) Value(key any) any {
	if _, ok := key.(ContextKey); ok {
		return m.val
	}
	return nil
}

func TestFromContext_Found(t *testing.T) {
	t.Parallel()
	expected := &Headers{Limit: 100} // ptrext:allow test value
	ctx := mockContext{val: expected}

	result := FromContext(ctx)
	require.Equal(t, expected, result)
}

func TestFromContext_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	result := FromContext(ctx)
	require.Nil(t, result)
}

func TestFromContext_WrongType(t *testing.T) {
	t.Parallel()
	ctx := mockContext{val: "not headers"}
	result := FromContext(ctx)
	require.Nil(t, result)
}
