// SPDX-License-Identifier: Apache-2.0

// Package slo provides Service Level Objective tracking and automation.
package slo

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// SLI represents a Service Level Indicator type.
type SLI string

const (
	SLIAvailability SLI = "availability"
	SLILatency      SLI = "latency"
	SLIThroughput   SLI = "throughput"
	SLIErrorRate    SLI = "error_rate"
)

// Objective represents a Service Level Objective.
type Objective struct {
	Name        string
	SLI         SLI
	Target      float64       // Target value (e.g., 0.999 for 99.9%)
	Window      time.Duration // Measurement window
	Description string
}

// Tracker tracks SLO compliance.
type Tracker struct {
	mu sync.RWMutex

	objective Objective

	// Counters
	goodEvents  atomic.Int64
	totalEvents atomic.Int64

	// Latency tracking
	latencySum   atomic.Int64
	latencyCount atomic.Int64
	latencyBad   atomic.Int64 // Events exceeding threshold

	// Window management
	windowStart time.Time
	history     []WindowResult

	// Error budget
	errorBudget float64 // Remaining error budget (1.0 = 100%)

	// Callbacks
	onBudgetExhausted func()
	onBudgetRestored  func()

	nowFunc func() time.Time
}

// WindowResult contains results for a measurement window.
type WindowResult struct {
	Start       time.Time
	End         time.Time
	SLI         float64
	Target      float64
	Met         bool
	GoodEvents  int64
	TotalEvents int64
}

// NewTracker creates a new SLO tracker.
func NewTracker(obj Objective) *Tracker {
	return ptrext.Of(Tracker{
		objective:   obj,
		windowStart: time.Now(),
		errorBudget: 1.0,
		nowFunc:     time.Now,
	})
}

// RecordGood records a good event (request succeeded within SLO).
func (t *Tracker) RecordGood() {
	t.goodEvents.Add(1)
	t.totalEvents.Add(1)
	t.maybeRotateWindow()
}

// RecordBad records a bad event (request failed or exceeded SLO).
func (t *Tracker) RecordBad() {
	t.totalEvents.Add(1)
	t.maybeRotateWindow()
}

// RecordLatency records a latency measurement.
// threshold is the SLO latency target.
func (t *Tracker) RecordLatency(latency time.Duration, threshold time.Duration) {
	t.latencySum.Add(int64(latency))
	t.latencyCount.Add(1)
	t.totalEvents.Add(1)

	if latency <= threshold {
		t.goodEvents.Add(1)
	} else {
		t.latencyBad.Add(1)
	}

	t.maybeRotateWindow()
}

// CurrentSLI returns the current SLI value.
func (t *Tracker) CurrentSLI() float64 {
	total := t.totalEvents.Load()
	if total == 0 {
		return 1.0 // No data = 100% SLI
	}
	good := t.goodEvents.Load()
	return float64(good) / float64(total)
}

// ErrorBudgetRemaining returns the remaining error budget (0.0-1.0).
func (t *Tracker) ErrorBudgetRemaining() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.errorBudget
}

// IsMeetingSLO returns whether the SLO is currently being met.
func (t *Tracker) IsMeetingSLO() bool {
	return t.CurrentSLI() >= t.objective.Target
}

// OnBudgetExhausted sets a callback for when error budget is exhausted.
func (t *Tracker) OnBudgetExhausted(fn func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onBudgetExhausted = fn
}

// OnBudgetRestored sets a callback for when error budget is restored.
func (t *Tracker) OnBudgetRestored(fn func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onBudgetRestored = fn
}

// Stats returns current SLO statistics.
func (t *Tracker) Stats() Stats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	total := t.totalEvents.Load()
	good := t.goodEvents.Load()
	currentSLI := float64(1.0)
	if total > 0 {
		currentSLI = float64(good) / float64(total)
	}

	// Calculate error budget consumed
	allowedErrors := (1.0 - t.objective.Target) * float64(total)
	actualErrors := float64(total - good)
	budgetConsumed := float64(0)
	if allowedErrors > 0 {
		budgetConsumed = actualErrors / allowedErrors
	}

	return Stats{
		Objective:       t.objective,
		CurrentSLI:      currentSLI,
		Target:          t.objective.Target,
		MeetingSLO:      currentSLI >= t.objective.Target,
		GoodEvents:      good,
		TotalEvents:     total,
		ErrorBudget:     t.errorBudget,
		BudgetConsumed:  budgetConsumed,
		WindowStart:     t.windowStart,
		WindowsComplete: len(t.history),
	}
}

// Stats contains SLO statistics.
type Stats struct {
	Objective       Objective
	CurrentSLI      float64
	Target          float64
	MeetingSLO      bool
	GoodEvents      int64
	TotalEvents     int64
	ErrorBudget     float64
	BudgetConsumed  float64
	WindowStart     time.Time
	WindowsComplete int
}

func (t *Tracker) maybeRotateWindow() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.nowFunc()
	if now.Sub(t.windowStart) < t.objective.Window {
		return
	}

	// Calculate window result
	total := t.totalEvents.Load()
	good := t.goodEvents.Load()
	sli := float64(1.0)
	if total > 0 {
		sli = float64(good) / float64(total)
	}

	result := WindowResult{
		Start:       t.windowStart,
		End:         now,
		SLI:         sli,
		Target:      t.objective.Target,
		Met:         sli >= t.objective.Target,
		GoodEvents:  good,
		TotalEvents: total,
	}

	t.history = append(t.history, result)
	if len(t.history) > 100 {
		t.history = t.history[1:]
	}

	// Update error budget
	wasExhausted := t.errorBudget <= 0
	t.updateErrorBudget(result)
	isExhausted := t.errorBudget <= 0

	// Fire callbacks
	if !wasExhausted && isExhausted && t.onBudgetExhausted != nil {
		go t.onBudgetExhausted()
	}
	if wasExhausted && !isExhausted && t.onBudgetRestored != nil {
		go t.onBudgetRestored()
	}

	// Reset for new window
	t.windowStart = now
	t.goodEvents.Store(0)
	t.totalEvents.Store(0)
	t.latencySum.Store(0)
	t.latencyCount.Store(0)
	t.latencyBad.Store(0)
}

func (t *Tracker) updateErrorBudget(result WindowResult) {
	// Error budget is based on how much of the allowed error rate was consumed
	allowedErrorRate := 1.0 - t.objective.Target
	actualErrorRate := 1.0 - result.SLI

	if allowedErrorRate <= 0 {
		t.errorBudget = 0
		return
	}

	// Consume budget based on actual vs allowed errors
	consumption := actualErrorRate / allowedErrorRate
	t.errorBudget -= consumption

	// Clamp to [0, 1]
	if t.errorBudget < 0 {
		t.errorBudget = 0
	}
	if t.errorBudget > 1 {
		t.errorBudget = 1
	}
}

// Policy defines actions based on error budget state.
type Policy struct {
	// MinBudget is the minimum error budget before triggering actions
	MinBudget float64

	// Actions when budget is low
	OnLowBudget func()

	// Actions when budget is exhausted
	OnExhausted func()

	// Actions when SLO is violated
	OnViolation func()
}

// Manager manages multiple SLO trackers.
type Manager struct {
	mu       sync.RWMutex
	trackers map[string]*Tracker
	policies map[string]Policy
}

// NewManager creates a new SLO manager.
func NewManager() *Manager {
	return ptrext.Of(Manager{
		trackers: make(map[string]*Tracker),
		policies: make(map[string]Policy),
	})
}

// Register registers an SLO tracker.
func (m *Manager) Register(name string, tracker *Tracker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackers[name] = tracker
}

// SetPolicy sets a policy for an SLO.
func (m *Manager) SetPolicy(name string, policy Policy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies[name] = policy
}

// Get returns a tracker by name.
func (m *Manager) Get(name string) (*Tracker, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.trackers[name]
	return t, ok
}

// Evaluate evaluates all SLOs and triggers policies.
func (m *Manager) Evaluate(ctx context.Context) map[string]string {
	m.mu.RLock()
	trackers := make(map[string]*Tracker, len(m.trackers))
	policies := make(map[string]Policy, len(m.policies))
	for k, v := range m.trackers {
		trackers[k] = v
	}
	for k, v := range m.policies {
		policies[k] = v
	}
	m.mu.RUnlock()

	actions := make(map[string]string)

	for name, tracker := range trackers {
		policy, hasPolicy := policies[name]
		stats := tracker.Stats()

		if !stats.MeetingSLO {
			actions[name] = "SLO violated"
			if hasPolicy && policy.OnViolation != nil {
				go policy.OnViolation()
			}
		}

		if stats.ErrorBudget <= 0 {
			if hasPolicy && policy.OnExhausted != nil {
				go policy.OnExhausted()
			}
		} else if hasPolicy && stats.ErrorBudget < policy.MinBudget {
			if policy.OnLowBudget != nil {
				go policy.OnLowBudget()
			}
		}
	}

	return actions
}

// AllStats returns stats for all tracked SLOs.
func (m *Manager) AllStats() map[string]Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]Stats, len(m.trackers))
	for name, tracker := range m.trackers {
		result[name] = tracker.Stats()
	}
	return result
}
