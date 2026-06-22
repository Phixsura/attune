// SPDX-License-Identifier: Apache-2.0

package embedding

import (
	"testing"
	"time"
)

func TestNewWorker(t *testing.T) {
	t.Parallel()

	w := NewWorker(nil, nil, nil, nil)
	if w == nil {
		t.Fatal("NewWorker returned nil")
	}
	if w.pollInterval != 5*time.Second {
		t.Errorf("pollInterval = %v, want 5s", w.pollInterval)
	}
	if w.staleDuration != 5*time.Minute {
		t.Errorf("staleDuration = %v, want 5m", w.staleDuration)
	}
	if w.maxAttempts != 5 {
		t.Errorf("maxAttempts = %d, want 5", w.maxAttempts)
	}
	if w.dimensions != 256 {
		t.Errorf("dimensions = %d, want 256", w.dimensions)
	}
	if w.threshold != 0.85 {
		t.Errorf("threshold = %f, want 0.85", w.threshold)
	}
	if w.recencyDays != 30 {
		t.Errorf("recencyDays = %d, want 30", w.recencyDays)
	}
}

func TestWorker_Configure(t *testing.T) {
	t.Parallel()

	t.Run("updates all values", func(t *testing.T) {
		w := NewWorker(nil, nil, nil, nil)
		w.Configure(10*time.Second, 512, 3, 0.90)

		if w.pollInterval != 10*time.Second {
			t.Errorf("pollInterval = %v, want 10s", w.pollInterval)
		}
		if w.dimensions != 512 {
			t.Errorf("dimensions = %d, want 512", w.dimensions)
		}
		if w.maxAttempts != 3 {
			t.Errorf("maxAttempts = %d, want 3", w.maxAttempts)
		}
		if w.threshold != 0.90 {
			t.Errorf("threshold = %f, want 0.90", w.threshold)
		}
	})

	t.Run("zero values keep defaults", func(t *testing.T) {
		w := NewWorker(nil, nil, nil, nil)
		origPoll := w.pollInterval
		origDims := w.dimensions
		origAttempts := w.maxAttempts
		origThreshold := w.threshold

		w.Configure(0, 0, 0, 0)

		if w.pollInterval != origPoll {
			t.Errorf("pollInterval changed from %v to %v", origPoll, w.pollInterval)
		}
		if w.dimensions != origDims {
			t.Errorf("dimensions changed from %d to %d", origDims, w.dimensions)
		}
		if w.maxAttempts != origAttempts {
			t.Errorf("maxAttempts changed from %d to %d", origAttempts, w.maxAttempts)
		}
		if w.threshold != origThreshold {
			t.Errorf("threshold changed from %f to %f", origThreshold, w.threshold)
		}
	})

	t.Run("negative values keep defaults", func(t *testing.T) {
		w := NewWorker(nil, nil, nil, nil)
		origPoll := w.pollInterval
		origDims := w.dimensions
		origAttempts := w.maxAttempts
		origThreshold := w.threshold

		w.Configure(-1*time.Second, -512, -3, -0.5)

		if w.pollInterval != origPoll {
			t.Errorf("pollInterval changed from %v to %v", origPoll, w.pollInterval)
		}
		if w.dimensions != origDims {
			t.Errorf("dimensions changed from %d to %d", origDims, w.dimensions)
		}
		if w.maxAttempts != origAttempts {
			t.Errorf("maxAttempts changed from %d to %d", origAttempts, w.maxAttempts)
		}
		if w.threshold != origThreshold {
			t.Errorf("threshold changed from %f to %f", origThreshold, w.threshold)
		}
	})

	t.Run("partial updates", func(t *testing.T) {
		w := NewWorker(nil, nil, nil, nil)
		origDims := w.dimensions
		origThreshold := w.threshold

		w.Configure(15*time.Second, 0, 7, 0)

		if w.pollInterval != 15*time.Second {
			t.Errorf("pollInterval = %v, want 15s", w.pollInterval)
		}
		if w.dimensions != origDims {
			t.Errorf("dimensions should not change, got %d", w.dimensions)
		}
		if w.maxAttempts != 7 {
			t.Errorf("maxAttempts = %d, want 7", w.maxAttempts)
		}
		if w.threshold != origThreshold {
			t.Errorf("threshold should not change, got %f", w.threshold)
		}
	})
}
