// SPDX-License-Identifier: Apache-2.0

package breakglass

import (
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// LockoutConfig configures the IP lockout behavior.
type LockoutConfig struct {
	// MaxAttempts before lockout (default: 5)
	MaxAttempts int
	// BaseLockoutDuration is the initial lockout duration (default: 1 minute)
	BaseLockoutDuration time.Duration
	// MaxLockoutDuration caps exponential backoff (default: 1 hour)
	MaxLockoutDuration time.Duration
	// AttemptWindow is how long failed attempts are remembered (default: 15 minutes)
	AttemptWindow time.Duration
}

// DefaultLockoutConfig returns production-safe defaults.
func DefaultLockoutConfig() LockoutConfig {
	return LockoutConfig{
		MaxAttempts:         5,
		BaseLockoutDuration: 1 * time.Minute,
		MaxLockoutDuration:  1 * time.Hour,
		AttemptWindow:       15 * time.Minute,
	}
}

// ipState tracks attempts and lockout for one IP.
type ipState struct {
	attempts     int
	firstAttempt time.Time
	lockedUntil  time.Time
	lockoutCount int // how many times this IP has been locked out (for exponential backoff)
}

// LockoutTracker tracks failed login attempts and enforces IP lockouts
// with exponential backoff.
type LockoutTracker struct {
	mu     sync.RWMutex
	config LockoutConfig
	ips    map[string]*ipState
}

// NewLockoutTracker creates a new lockout tracker.
func NewLockoutTracker(cfg LockoutConfig) *LockoutTracker {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.BaseLockoutDuration <= 0 {
		cfg.BaseLockoutDuration = 1 * time.Minute
	}
	if cfg.MaxLockoutDuration <= 0 {
		cfg.MaxLockoutDuration = 1 * time.Hour
	}
	if cfg.AttemptWindow <= 0 {
		cfg.AttemptWindow = 15 * time.Minute
	}

	return ptrext.Of(LockoutTracker{
		config: cfg,
		ips:    make(map[string]*ipState),
	})
}

// LockoutStatus describes the current lockout state for an IP.
type LockoutStatus struct {
	Locked        bool
	LockedUntil   time.Time
	Attempts      int
	RemainingTime time.Duration
}

// Check returns the lockout status for an IP without modifying state.
func (t *LockoutTracker) Check(ip string) LockoutStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state, ok := t.ips[ip]
	if !ok {
		return LockoutStatus{}
	}

	now := time.Now()
	if !state.lockedUntil.IsZero() && now.Before(state.lockedUntil) {
		return LockoutStatus{
			Locked:        true,
			LockedUntil:   state.lockedUntil,
			Attempts:      state.attempts,
			RemainingTime: state.lockedUntil.Sub(now),
		}
	}

	return LockoutStatus{
		Attempts: state.attempts,
	}
}

// IsLocked returns true if the IP is currently locked out.
func (t *LockoutTracker) IsLocked(ip string) bool {
	return t.Check(ip).Locked
}

// RecordFailure records a failed attempt and returns true if the IP is now locked.
func (t *LockoutTracker) RecordFailure(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	state, ok := t.ips[ip]
	if !ok {
		state = ptrext.Of(ipState{
			firstAttempt: now,
		})
		t.ips[ip] = state
	}

	// If currently locked, extend the lockout (they're trying while locked)
	if !state.lockedUntil.IsZero() && now.Before(state.lockedUntil) {
		return true
	}

	// Reset if the attempt window has passed
	if now.Sub(state.firstAttempt) > t.config.AttemptWindow {
		state.attempts = 0
		state.firstAttempt = now
	}

	state.attempts++

	// Check if we should lock out
	if state.attempts >= t.config.MaxAttempts {
		state.lockoutCount++
		lockoutDuration := t.calculateLockoutDuration(state.lockoutCount)
		state.lockedUntil = now.Add(lockoutDuration)
		return true
	}

	return false
}

// RecordSuccess clears the failed attempt counter for an IP.
func (t *LockoutTracker) RecordSuccess(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.ips[ip]
	if !ok {
		return
	}

	// Clear attempts but keep lockoutCount for exponential backoff history
	state.attempts = 0
	state.lockedUntil = time.Time{}
}

// Unlock manually unlocks an IP (admin action).
func (t *LockoutTracker) Unlock(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.ips, ip)
}

// ListLocked returns all currently locked IPs with their status.
func (t *LockoutTracker) ListLocked() map[string]LockoutStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	result := make(map[string]LockoutStatus)

	for ip, state := range t.ips {
		if !state.lockedUntil.IsZero() && now.Before(state.lockedUntil) {
			result[ip] = LockoutStatus{
				Locked:        true,
				LockedUntil:   state.lockedUntil,
				Attempts:      state.attempts,
				RemainingTime: state.lockedUntil.Sub(now),
			}
		}
	}

	return result
}

// Cleanup removes expired entries to prevent memory leaks.
// Call periodically (e.g., every hour).
func (t *LockoutTracker) Cleanup() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-2 * t.config.AttemptWindow)
	removed := 0

	for ip, state := range t.ips {
		// Remove if: not locked AND no recent attempts
		isLocked := !state.lockedUntil.IsZero() && now.Before(state.lockedUntil)
		isStale := state.firstAttempt.Before(cutoff) && state.lockedUntil.Before(now)

		if !isLocked && isStale {
			delete(t.ips, ip)
			removed++
		}
	}

	return removed
}

// calculateLockoutDuration returns the lockout duration with exponential backoff.
// lockoutCount=1: base, lockoutCount=2: 2x base, lockoutCount=3: 4x base, etc.
func (t *LockoutTracker) calculateLockoutDuration(lockoutCount int) time.Duration {
	if lockoutCount <= 0 {
		lockoutCount = 1
	}

	// Exponential: base * 2^(count-1)
	multiplier := 1 << (lockoutCount - 1) // 1, 2, 4, 8, 16, ...
	duration := t.config.BaseLockoutDuration * time.Duration(multiplier)

	if duration > t.config.MaxLockoutDuration {
		return t.config.MaxLockoutDuration
	}
	return duration
}
