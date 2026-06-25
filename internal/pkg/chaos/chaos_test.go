// SPDX-License-Identifier: Apache-2.0

package chaos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEngine_EnableDisable(t *testing.T) {
	t.Parallel()
	e := NewEngine()

	require.False(t, e.IsEnabled())

	e.Enable()
	require.True(t, e.IsEnabled())

	e.Disable()
	require.False(t, e.IsEnabled())
}

func TestEngine_RegisterFault(t *testing.T) {
	t.Parallel()
	e := NewEngine()

	cfg := FaultConfig{
		Type:        FaultError,
		Probability: 1.0,
		Enabled:     true,
	}
	e.RegisterFault("test", cfg)

	got, ok := e.GetFault("test")
	require.True(t, ok)
	require.Equal(t, FaultError, got.Type)
}

func TestEngine_UnregisterFault(t *testing.T) {
	t.Parallel()
	e := NewEngine()

	e.RegisterFault("test", FaultConfig{Type: FaultError, Enabled: true})
	e.UnregisterFault("test")

	_, ok := e.GetFault("test")
	require.False(t, ok)
}

func TestEngine_ListFaults(t *testing.T) {
	t.Parallel()
	e := NewEngine()

	e.RegisterFault("a", FaultConfig{Type: FaultError, Enabled: true})
	e.RegisterFault("b", FaultConfig{Type: FaultLatency, Enabled: true})

	faults := e.ListFaults()
	require.Len(t, faults, 2)
}

func TestEngine_MaybeInject_Disabled(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.RegisterFault("test", FaultConfig{Type: FaultError, Probability: 1.0, Enabled: true})

	// Engine is disabled, so no fault should be injected
	err := e.MaybeInject(context.Background(), "test")
	require.NoError(t, err)
}

func TestEngine_MaybeInject_Error(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	e.RegisterFault("test", FaultConfig{
		Type:        FaultError,
		Probability: 1.0,
		Enabled:     true,
	})

	err := e.MaybeInject(context.Background(), "test")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInjectedFault)
}

func TestEngine_MaybeInject_Latency(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	e.RegisterFault("test", FaultConfig{
		Type:        FaultLatency,
		Probability: 1.0,
		Duration:    10 * time.Millisecond,
		Enabled:     true,
	})

	start := time.Now()
	err := e.MaybeInject(context.Background(), "test")
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.GreaterOrEqual(t, elapsed, 10*time.Millisecond)
}

func TestEngine_MaybeInject_Probability(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	e.RegisterFault("test", FaultConfig{
		Type:        FaultError,
		Probability: 0.0, // Never inject
		Enabled:     true,
	})

	err := e.MaybeInject(context.Background(), "test")
	require.NoError(t, err)
}

func TestEngine_MaybeInject_NotEnabled(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	e.RegisterFault("test", FaultConfig{
		Type:        FaultError,
		Probability: 1.0,
		Enabled:     false, // Fault disabled
	})

	err := e.MaybeInject(context.Background(), "test")
	require.NoError(t, err)
}

func TestEngine_Middleware(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	e.RegisterFault("http", FaultConfig{
		Type:        FaultPartition,
		Probability: 1.0,
		Enabled:     true,
	})

	handler := e.Middleware("http")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestGlobal(t *testing.T) {
	// Not parallel - uses global
	require.NotNil(t, Global)
	require.False(t, Global.IsEnabled())
}
