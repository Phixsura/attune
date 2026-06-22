// SPDX-License-Identifier: Apache-2.0

package outbox

import (
	"strings"
	"testing"
	"time"
)

func TestNewOutboxWorker(t *testing.T) {
	t.Parallel()

	w := NewOutboxWorker(nil, nil, nil)
	if w == nil {
		t.Fatal("NewOutboxWorker returned nil")
	}
	if !strings.HasPrefix(w.owner, "outbox-") {
		t.Errorf("owner = %q, want prefix 'outbox-'", w.owner)
	}
	if w.pollInterval != 5*time.Second {
		t.Errorf("pollInterval = %v, want 5s", w.pollInterval)
	}
	if w.batchSize != 10 {
		t.Errorf("batchSize = %d, want 10", w.batchSize)
	}
	if w.maxAttempts != 5 {
		t.Errorf("maxAttempts = %d, want 5", w.maxAttempts)
	}
}

func TestOutboxWorker_Configure(t *testing.T) {
	t.Parallel()

	t.Run("updates all values", func(t *testing.T) {
		w := NewOutboxWorker(nil, nil, nil)
		w.Configure(10*time.Second, 20, 3)
		if w.pollInterval != 10*time.Second {
			t.Errorf("pollInterval = %v, want 10s", w.pollInterval)
		}
		if w.batchSize != 20 {
			t.Errorf("batchSize = %d, want 20", w.batchSize)
		}
		if w.maxAttempts != 3 {
			t.Errorf("maxAttempts = %d, want 3", w.maxAttempts)
		}
	})

	t.Run("zero values keep defaults", func(t *testing.T) {
		w := NewOutboxWorker(nil, nil, nil)
		origPoll := w.pollInterval
		origBatch := w.batchSize
		origAttempts := w.maxAttempts

		w.Configure(0, 0, 0)

		if w.pollInterval != origPoll {
			t.Errorf("pollInterval changed from %v to %v", origPoll, w.pollInterval)
		}
		if w.batchSize != origBatch {
			t.Errorf("batchSize changed from %d to %d", origBatch, w.batchSize)
		}
		if w.maxAttempts != origAttempts {
			t.Errorf("maxAttempts changed from %d to %d", origAttempts, w.maxAttempts)
		}
	})

	t.Run("negative values keep defaults", func(t *testing.T) {
		w := NewOutboxWorker(nil, nil, nil)
		origPoll := w.pollInterval
		origBatch := w.batchSize
		origAttempts := w.maxAttempts

		w.Configure(-1*time.Second, -5, -3)

		if w.pollInterval != origPoll {
			t.Errorf("pollInterval changed from %v to %v", origPoll, w.pollInterval)
		}
		if w.batchSize != origBatch {
			t.Errorf("batchSize changed from %d to %d", origBatch, w.batchSize)
		}
		if w.maxAttempts != origAttempts {
			t.Errorf("maxAttempts changed from %d to %d", origAttempts, w.maxAttempts)
		}
	})

	t.Run("partial updates", func(t *testing.T) {
		w := NewOutboxWorker(nil, nil, nil)
		origBatch := w.batchSize

		w.Configure(15*time.Second, 0, 7)

		if w.pollInterval != 15*time.Second {
			t.Errorf("pollInterval = %v, want 15s", w.pollInterval)
		}
		if w.batchSize != origBatch {
			t.Errorf("batchSize should not change, got %d", w.batchSize)
		}
		if w.maxAttempts != 7 {
			t.Errorf("maxAttempts = %d, want 7", w.maxAttempts)
		}
	})
}

func TestOutboxWorker_OwnerUniqueness(t *testing.T) {
	t.Parallel()

	w1 := NewOutboxWorker(nil, nil, nil)
	w2 := NewOutboxWorker(nil, nil, nil)

	if w1.owner == w2.owner {
		t.Error("two workers should have unique owners")
	}
}
