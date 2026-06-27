// SPDX-License-Identifier: Apache-2.0

package breakglass

import (
	"testing"
	"time"
)

func TestLockoutTracker_NoLockoutInitially(t *testing.T) {
	tracker := NewLockoutTracker(DefaultLockoutConfig())

	status := tracker.Check("192.168.1.1")
	if status.Locked {
		t.Error("new IP should not be locked")
	}
	if status.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0", status.Attempts)
	}
}

func TestLockoutTracker_LockoutAfterMaxAttempts(t *testing.T) {
	cfg := LockoutConfig{
		MaxAttempts:         3,
		BaseLockoutDuration: 1 * time.Minute,
		MaxLockoutDuration:  1 * time.Hour,
		AttemptWindow:       15 * time.Minute,
	}
	tracker := NewLockoutTracker(cfg)
	ip := "192.168.1.1"

	// First two failures should not lock
	if tracker.RecordFailure(ip) {
		t.Error("should not be locked after 1 failure")
	}
	if tracker.RecordFailure(ip) {
		t.Error("should not be locked after 2 failures")
	}

	// Third failure should lock
	if !tracker.RecordFailure(ip) {
		t.Error("should be locked after 3 failures")
	}

	status := tracker.Check(ip)
	if !status.Locked {
		t.Error("IP should be locked")
	}
	if status.RemainingTime <= 0 {
		t.Error("remaining time should be positive")
	}
}

func TestLockoutTracker_ExponentialBackoff(t *testing.T) {
	cfg := LockoutConfig{
		MaxAttempts:         2,
		BaseLockoutDuration: 1 * time.Minute,
		MaxLockoutDuration:  1 * time.Hour,
		AttemptWindow:       15 * time.Minute,
	}
	tracker := NewLockoutTracker(cfg)

	// First lockout: 1 minute
	d1 := tracker.calculateLockoutDuration(1)
	if d1 != 1*time.Minute {
		t.Errorf("first lockout = %v, want 1m", d1)
	}

	// Second lockout: 2 minutes
	d2 := tracker.calculateLockoutDuration(2)
	if d2 != 2*time.Minute {
		t.Errorf("second lockout = %v, want 2m", d2)
	}

	// Third lockout: 4 minutes
	d3 := tracker.calculateLockoutDuration(3)
	if d3 != 4*time.Minute {
		t.Errorf("third lockout = %v, want 4m", d3)
	}

	// Should cap at max
	d10 := tracker.calculateLockoutDuration(10)
	if d10 != 1*time.Hour {
		t.Errorf("tenth lockout = %v, want 1h (max)", d10)
	}
}

func TestLockoutTracker_Unlock(t *testing.T) {
	cfg := LockoutConfig{
		MaxAttempts:         2,
		BaseLockoutDuration: 1 * time.Hour,
		MaxLockoutDuration:  1 * time.Hour,
		AttemptWindow:       15 * time.Minute,
	}
	tracker := NewLockoutTracker(cfg)
	ip := "192.168.1.1"

	// Lock the IP
	tracker.RecordFailure(ip)
	tracker.RecordFailure(ip)

	if !tracker.IsLocked(ip) {
		t.Error("IP should be locked")
	}

	// Unlock
	tracker.Unlock(ip)

	if tracker.IsLocked(ip) {
		t.Error("IP should not be locked after unlock")
	}
}

func TestLockoutTracker_RecordSuccess(t *testing.T) {
	cfg := LockoutConfig{
		MaxAttempts:         3,
		BaseLockoutDuration: 1 * time.Minute,
		MaxLockoutDuration:  1 * time.Hour,
		AttemptWindow:       15 * time.Minute,
	}
	tracker := NewLockoutTracker(cfg)
	ip := "192.168.1.1"

	// Record 2 failures
	tracker.RecordFailure(ip)
	tracker.RecordFailure(ip)

	status := tracker.Check(ip)
	if status.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", status.Attempts)
	}

	// Success clears attempts
	tracker.RecordSuccess(ip)

	status = tracker.Check(ip)
	if status.Attempts != 0 {
		t.Errorf("Attempts after success = %d, want 0", status.Attempts)
	}
}

func TestLockoutTracker_ListLocked(t *testing.T) {
	cfg := LockoutConfig{
		MaxAttempts:         2,
		BaseLockoutDuration: 1 * time.Hour,
		MaxLockoutDuration:  1 * time.Hour,
		AttemptWindow:       15 * time.Minute,
	}
	tracker := NewLockoutTracker(cfg)

	// Lock two IPs
	tracker.RecordFailure("192.168.1.1")
	tracker.RecordFailure("192.168.1.1")
	tracker.RecordFailure("192.168.1.2")
	tracker.RecordFailure("192.168.1.2")

	// One IP not locked
	tracker.RecordFailure("192.168.1.3")

	locked := tracker.ListLocked()
	if len(locked) != 2 {
		t.Errorf("ListLocked() returned %d IPs, want 2", len(locked))
	}
	if _, ok := locked["192.168.1.1"]; !ok {
		t.Error("192.168.1.1 should be in locked list")
	}
	if _, ok := locked["192.168.1.2"]; !ok {
		t.Error("192.168.1.2 should be in locked list")
	}
	if _, ok := locked["192.168.1.3"]; ok {
		t.Error("192.168.1.3 should not be in locked list")
	}
}

func TestLockoutTracker_DefaultConfig(t *testing.T) {
	cfg := DefaultLockoutConfig()

	if cfg.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", cfg.MaxAttempts)
	}
	if cfg.BaseLockoutDuration != 1*time.Minute {
		t.Errorf("BaseLockoutDuration = %v, want 1m", cfg.BaseLockoutDuration)
	}
	if cfg.MaxLockoutDuration != 1*time.Hour {
		t.Errorf("MaxLockoutDuration = %v, want 1h", cfg.MaxLockoutDuration)
	}
	if cfg.AttemptWindow != 15*time.Minute {
		t.Errorf("AttemptWindow = %v, want 15m", cfg.AttemptWindow)
	}
}

func TestLockoutTracker_Cleanup(t *testing.T) {
	cfg := LockoutConfig{
		MaxAttempts:         2,
		BaseLockoutDuration: 1 * time.Millisecond,
		MaxLockoutDuration:  1 * time.Millisecond,
		AttemptWindow:       1 * time.Millisecond,
	}
	tracker := NewLockoutTracker(cfg)

	// Add some entries
	tracker.RecordFailure("192.168.1.1")
	tracker.RecordFailure("192.168.1.2")

	// Wait for them to become stale
	time.Sleep(10 * time.Millisecond)

	// Cleanup should remove them
	removed := tracker.Cleanup()
	if removed != 2 {
		t.Errorf("Cleanup removed %d entries, want 2", removed)
	}

	// Check they're gone
	if tracker.Check("192.168.1.1").Attempts != 0 {
		t.Error("192.168.1.1 should be cleaned up")
	}
}
