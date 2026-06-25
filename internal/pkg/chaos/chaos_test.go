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

// ============================================================================
// Edge Cases
// ============================================================================

func TestEngine_MaybeInject_UnknownFault(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()

	// Inject with unknown fault name
	err := e.MaybeInject(context.Background(), "nonexistent")
	require.NoError(t, err, "unknown fault should not error")
}

func TestEngine_MaybeInject_ContextCancelled(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	e.RegisterFault("latency", FaultConfig{
		Type:        FaultLatency,
		Probability: 1.0,
		Duration:    5 * time.Second,
		Enabled:     true,
	})

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := e.MaybeInject(ctx, "latency")
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, elapsed, time.Second, "should return early on context cancel")
}

func TestEngine_MaybeInject_Timeout(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	e.RegisterFault("timeout", FaultConfig{
		Type:        FaultTimeout,
		Probability: 1.0,
		Enabled:     true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := e.MaybeInject(ctx, "timeout")
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestEngine_MaybeInject_CustomErrorMessage(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	e.RegisterFault("custom-error", FaultConfig{
		Type:        FaultError,
		Probability: 1.0,
		ErrorMsg:    "custom error message",
		Enabled:     true,
	})

	err := e.MaybeInject(context.Background(), "custom-error")
	require.Error(t, err)
	require.Equal(t, "custom error message", err.Error())
}

func TestEngine_ConcurrentRegisterUnregister(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()

	done := make(chan bool)

	// Concurrent registrations
	go func() {
		for i := 0; i < 100; i++ {
			e.RegisterFault("fault-a", FaultConfig{Type: FaultError, Enabled: true})
		}
		done <- true
	}()

	// Concurrent unregistrations
	go func() {
		for i := 0; i < 100; i++ {
			e.UnregisterFault("fault-a")
		}
		done <- true
	}()

	// Concurrent gets
	go func() {
		for i := 0; i < 100; i++ {
			e.GetFault("fault-a")
		}
		done <- true
	}()

	// Concurrent injects
	go func() {
		for i := 0; i < 100; i++ {
			_ = e.MaybeInject(context.Background(), "fault-a")
		}
		done <- true
	}()

	for i := 0; i < 4; i++ {
		<-done
	}
}

func TestEngine_OverwriteFault(t *testing.T) {
	t.Parallel()
	e := NewEngine()

	e.RegisterFault("test", FaultConfig{Type: FaultError, Probability: 0.5, Enabled: true})
	e.RegisterFault("test", FaultConfig{Type: FaultLatency, Probability: 1.0, Enabled: false})

	cfg, ok := e.GetFault("test")
	require.True(t, ok)
	require.Equal(t, FaultLatency, cfg.Type)
	require.Equal(t, 1.0, cfg.Probability)
	require.False(t, cfg.Enabled)
}

func TestEngine_Middleware_NoFault(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()

	handler := e.Middleware("nonexistent")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "should pass through when no fault registered")
}

func TestEngine_Middleware_Error(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	e.RegisterFault("http-error", FaultConfig{
		Type:        FaultError,
		Probability: 1.0,
		ErrorMsg:    "injected error",
		Enabled:     true,
	})

	handler := e.Middleware("http-error")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestExperiment_Run(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	runner := NewRunner(e)

	exp := Experiment{
		Name:        "test-experiment",
		Description: "Test experiment",
		Faults: []FaultConfig{
			{Type: FaultError, Probability: 0.5, Enabled: true},
		},
		Duration: 100 * time.Millisecond,
	}

	err := runner.Run(context.Background(), exp)
	require.NoError(t, err)
}

func TestExperiment_Run_ContextCancel(t *testing.T) {
	t.Parallel()
	e := NewEngine()
	e.Enable()
	runner := NewRunner(e)

	rollbackCalled := false
	exp := Experiment{
		Name:        "test-experiment",
		Description: "Test experiment",
		Faults: []FaultConfig{
			{Type: FaultError, Probability: 0.5, Enabled: true},
		},
		Duration: 5 * time.Second,
		Rollback: func() { rollbackCalled = true },
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := runner.Run(ctx, exp)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, rollbackCalled, "rollback should be called on cancel")
}

func TestFaultTypes(t *testing.T) {
	t.Parallel()
	require.Equal(t, FaultType("latency"), FaultLatency)
	require.Equal(t, FaultType("error"), FaultError)
	require.Equal(t, FaultType("panic"), FaultPanic)
	require.Equal(t, FaultType("timeout"), FaultTimeout)
	require.Equal(t, FaultType("partition"), FaultPartition)
	require.Equal(t, FaultType("corruption"), FaultCorruption)
	require.Equal(t, FaultType("resource_exhaustion"), FaultResourceExhaustion)
}

func TestEngine_EnableDisableConcurrent(t *testing.T) {
	t.Parallel()
	e := NewEngine()

	done := make(chan bool)

	go func() {
		for i := 0; i < 1000; i++ {
			e.Enable()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			e.Disable()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			_ = e.IsEnabled()
		}
		done <- true
	}()

	for i := 0; i < 3; i++ {
		<-done
	}
}
