// SPDX-License-Identifier: Apache-2.0

// Package chaos provides chaos engineering primitives for fault injection.
// Use these in testing and controlled environments to verify system resilience.
package chaos

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrInjectedFault is returned when a fault is injected.
var ErrInjectedFault = errors.New("chaos: injected fault")

// ErrCircuitOpen is returned when the chaos circuit breaker is open.
var ErrCircuitOpen = errors.New("chaos: circuit open")

// FaultType represents types of faults that can be injected.
type FaultType string

const (
	FaultLatency            FaultType = "latency"
	FaultError              FaultType = "error"
	FaultPanic              FaultType = "panic"
	FaultTimeout            FaultType = "timeout"
	FaultPartition          FaultType = "partition"
	FaultCorruption         FaultType = "corruption"
	FaultResourceExhaustion FaultType = "resource_exhaustion"
)

// FaultConfig configures a fault injection.
type FaultConfig struct {
	Type        FaultType
	Probability float64       // 0.0-1.0, chance of fault occurring
	Duration    time.Duration // For latency faults
	ErrorMsg    string        // For error faults
	Enabled     bool
}

// Engine manages fault injection rules.
type Engine struct {
	mu      sync.RWMutex
	faults  map[string]FaultConfig
	enabled atomic.Bool
	rng     *rand.Rand
}

// NewEngine creates a new chaos engine.
func NewEngine() *Engine {
	return ptrext.Of(Engine{
		faults: make(map[string]FaultConfig),
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // chaos doesn't need crypto rand
	})
}

// Enable enables the chaos engine globally.
func (e *Engine) Enable() {
	e.enabled.Store(true)
}

// Disable disables the chaos engine globally.
func (e *Engine) Disable() {
	e.enabled.Store(false)
}

// IsEnabled returns whether the chaos engine is enabled.
func (e *Engine) IsEnabled() bool {
	return e.enabled.Load()
}

// RegisterFault registers a fault injection rule.
func (e *Engine) RegisterFault(name string, cfg FaultConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.faults[name] = cfg
}

// UnregisterFault removes a fault injection rule.
func (e *Engine) UnregisterFault(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.faults, name)
}

// GetFault returns a fault configuration by name.
func (e *Engine) GetFault(name string) (FaultConfig, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	cfg, ok := e.faults[name]
	return cfg, ok
}

// ListFaults returns all registered faults.
func (e *Engine) ListFaults() map[string]FaultConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[string]FaultConfig, len(e.faults))
	for k, v := range e.faults {
		result[k] = v
	}
	return result
}

// MaybeInject checks if a fault should be injected and applies it.
// Returns nil if no fault is injected.
func (e *Engine) MaybeInject(ctx context.Context, name string) error {
	if !e.enabled.Load() {
		return nil
	}

	e.mu.RLock()
	cfg, ok := e.faults[name]
	e.mu.RUnlock()

	if !ok || !cfg.Enabled {
		return nil
	}

	// Check probability
	e.mu.Lock()
	shouldInject := e.rng.Float64() < cfg.Probability
	e.mu.Unlock()

	if !shouldInject {
		return nil
	}

	return e.injectFault(ctx, cfg)
}

func (e *Engine) injectFault(ctx context.Context, cfg FaultConfig) error {
	switch cfg.Type {
	case FaultLatency:
		select {
		case <-time.After(cfg.Duration):
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil

	case FaultError:
		if cfg.ErrorMsg != "" {
			return errors.New(cfg.ErrorMsg)
		}
		return ErrInjectedFault

	case FaultPanic:
		panic("chaos: injected panic")

	case FaultTimeout:
		<-ctx.Done()
		return context.DeadlineExceeded

	case FaultPartition:
		return ErrCircuitOpen

	default:
		return ErrInjectedFault
	}
}

// Middleware returns an HTTP middleware that injects faults.
func (e *Engine) Middleware(faultName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := e.MaybeInject(r.Context(), faultName); err != nil {
				if errors.Is(err, ErrCircuitOpen) {
					http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Experiment represents a chaos experiment.
type Experiment struct {
	Name        string
	Description string
	Faults      []FaultConfig
	Duration    time.Duration
	Hypothesis  string
	Rollback    func()
}

// Runner executes chaos experiments.
type Runner struct {
	engine *Engine
}

// NewRunner creates a new experiment runner.
func NewRunner(engine *Engine) *Runner {
	return ptrext.Of(Runner{engine: engine})
}

// Run executes an experiment for its configured duration.
func (r *Runner) Run(ctx context.Context, exp Experiment) error {
	// Register all faults
	for i, f := range exp.Faults {
		name := exp.Name + "_" + string(f.Type) + "_" + string(rune('0'+i))
		r.engine.RegisterFault(name, f)
		defer r.engine.UnregisterFault(name)
	}

	// Run for duration
	select {
	case <-time.After(exp.Duration):
	case <-ctx.Done():
		if exp.Rollback != nil {
			exp.Rollback()
		}
		return ctx.Err()
	}

	return nil
}

// Global is the default chaos engine (disabled by default).
var Global = NewEngine()
