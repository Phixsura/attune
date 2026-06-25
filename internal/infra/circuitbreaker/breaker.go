// SPDX-License-Identifier: Apache-2.0

// Package circuitbreaker provides a simple circuit breaker for external calls.
// It prevents cascading failures by fast-failing when an upstream is unhealthy.
package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// State represents the circuit breaker state.
type State int

const (
	StateClosed   State = iota // Normal operation, requests pass through
	StateOpen                  // Failing fast, requests rejected
	StateHalfOpen              // Testing if upstream recovered
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// ErrCircuitOpen is returned when the circuit is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// Config configures a circuit breaker.
type Config struct {
	Name string // For metrics labeling

	// FailureThreshold is the number of consecutive failures to open the circuit.
	FailureThreshold int

	// SuccessThreshold is the number of consecutive successes in half-open
	// state to close the circuit.
	SuccessThreshold int

	// OpenDuration is how long to stay open before transitioning to half-open.
	OpenDuration time.Duration

	// HalfOpenMaxConcurrent is max requests allowed through in half-open state.
	HalfOpenMaxConcurrent int
}

// DefaultConfig returns sensible defaults for most external services.
func DefaultConfig(name string) Config {
	return Config{
		Name:                  name,
		FailureThreshold:      5,
		SuccessThreshold:      2,
		OpenDuration:          30 * time.Second,
		HalfOpenMaxConcurrent: 1,
	}
}

// Breaker is a circuit breaker for external service calls.
type Breaker struct {
	cfg Config

	mu              sync.RWMutex
	state           State
	failures        int
	successes       int
	lastFailureTime time.Time
	halfOpenCount   int
}

// New creates a new circuit breaker.
func New(cfg Config) *Breaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.OpenDuration <= 0 {
		cfg.OpenDuration = 30 * time.Second
	}
	if cfg.HalfOpenMaxConcurrent <= 0 {
		cfg.HalfOpenMaxConcurrent = 1
	}
	return ptrext.Of(Breaker{cfg: cfg, state: StateClosed})
}

// Allow checks if a request should be allowed through.
// Returns a done func to call with the result, or ErrCircuitOpen.
func (b *Breaker) Allow() (done func(success bool), err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return b.recordResult, nil

	case StateOpen:
		if time.Since(b.lastFailureTime) >= b.cfg.OpenDuration {
			b.transitionTo(StateHalfOpen)
			b.halfOpenCount = 1
			return b.recordResult, nil
		}
		metrics.CircuitBreakerRejected.WithLabelValues(b.cfg.Name).Inc()
		return nil, ErrCircuitOpen

	case StateHalfOpen:
		if b.halfOpenCount >= b.cfg.HalfOpenMaxConcurrent {
			metrics.CircuitBreakerRejected.WithLabelValues(b.cfg.Name).Inc()
			return nil, ErrCircuitOpen
		}
		b.halfOpenCount++
		return b.recordResult, nil
	}

	return nil, ErrCircuitOpen
}

// Execute runs f through the circuit breaker.
func (b *Breaker) Execute(ctx context.Context, f func(context.Context) error) error {
	done, err := b.Allow()
	if err != nil {
		return err
	}

	err = f(ctx)
	done(err == nil)
	return err
}

func (b *Breaker) recordResult(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if success {
		b.onSuccess()
	} else {
		b.onFailure()
	}
}

func (b *Breaker) onSuccess() {
	metrics.CircuitBreakerResults.WithLabelValues(b.cfg.Name, "success").Inc()

	switch b.state {
	case StateClosed:
		b.failures = 0

	case StateHalfOpen:
		b.successes++
		b.halfOpenCount--
		if b.successes >= b.cfg.SuccessThreshold {
			b.transitionTo(StateClosed)
		}
	}
}

func (b *Breaker) onFailure() {
	b.lastFailureTime = time.Now()
	metrics.CircuitBreakerResults.WithLabelValues(b.cfg.Name, "failure").Inc()

	switch b.state {
	case StateClosed:
		b.failures++
		if b.failures >= b.cfg.FailureThreshold {
			b.transitionTo(StateOpen)
		}

	case StateHalfOpen:
		b.halfOpenCount--
		b.transitionTo(StateOpen)
	}
}

func (b *Breaker) transitionTo(newState State) {
	if b.state == newState {
		return
	}
	metrics.CircuitBreakerTransitions.WithLabelValues(b.cfg.Name, b.state.String(), newState.String()).Inc()
	b.state = newState
	b.failures = 0
	b.successes = 0
}

// State returns the current circuit state.
func (b *Breaker) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// Reset forces the circuit to closed state.
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.transitionTo(StateClosed)
}
