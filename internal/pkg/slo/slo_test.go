// SPDX-License-Identifier: Apache-2.0

package slo

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Tracker — basic recording
// ---------------------------------------------------------------------------

func TestTracker_RecordGood(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "test",
		SLI:    SLIAvailability,
		Target: 0.99,
		Window: time.Hour,
	})

	tracker.RecordGood()
	tracker.RecordGood()

	stats := tracker.Stats()
	require.Equal(t, int64(2), stats.GoodEvents)
	require.Equal(t, int64(2), stats.TotalEvents)
	require.Equal(t, float64(1.0), stats.CurrentSLI)
}

func TestTracker_RecordBad(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "test",
		SLI:    SLIAvailability,
		Target: 0.99,
		Window: time.Hour,
	})

	tracker.RecordGood()
	tracker.RecordBad()

	stats := tracker.Stats()
	require.Equal(t, int64(1), stats.GoodEvents)
	require.Equal(t, int64(2), stats.TotalEvents)
	require.Equal(t, float64(0.5), stats.CurrentSLI)
}

func TestTracker_RecordLatency(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "test",
		SLI:    SLILatency,
		Target: 0.95,
		Window: time.Hour,
	})

	threshold := 100 * time.Millisecond

	// Within threshold
	tracker.RecordLatency(50*time.Millisecond, threshold)
	// Exceeds threshold
	tracker.RecordLatency(200*time.Millisecond, threshold)

	stats := tracker.Stats()
	require.Equal(t, int64(1), stats.GoodEvents)
	require.Equal(t, int64(2), stats.TotalEvents)
	require.Equal(t, float64(0.5), stats.CurrentSLI)
}

func TestTracker_RecordLatency_ExactThreshold(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "latency-exact",
		SLI:    SLILatency,
		Target: 0.95,
		Window: time.Hour,
	})

	threshold := 100 * time.Millisecond

	// Exactly at threshold counts as good (<= threshold).
	tracker.RecordLatency(threshold, threshold)

	require.Equal(t, float64(1.0), tracker.CurrentSLI())
	stats := tracker.Stats()
	require.Equal(t, int64(1), stats.GoodEvents)
	require.Equal(t, int64(1), stats.TotalEvents)
}

func TestTracker_RecordLatency_AllBad(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "latency-all-bad",
		SLI:    SLILatency,
		Target: 0.95,
		Window: time.Hour,
	})

	threshold := 50 * time.Millisecond
	tracker.RecordLatency(100*time.Millisecond, threshold)
	tracker.RecordLatency(200*time.Millisecond, threshold)
	tracker.RecordLatency(300*time.Millisecond, threshold)

	require.InDelta(t, 0.0, tracker.CurrentSLI(), 1e-9)
	require.False(t, tracker.IsMeetingSLO())
}

// ---------------------------------------------------------------------------
// CurrentSLI
// ---------------------------------------------------------------------------

func TestTracker_CurrentSLI_NoData(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "test",
		Target: 0.99,
		Window: time.Hour,
	})

	require.Equal(t, float64(1.0), tracker.CurrentSLI())
}

func TestTracker_CurrentSLI_MixedEvents(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "mixed",
		Target: 0.90,
		Window: time.Hour,
	})

	for i := 0; i < 9; i++ {
		tracker.RecordGood()
	}
	tracker.RecordBad()

	// 9/10 = 0.9
	require.InDelta(t, 0.9, tracker.CurrentSLI(), 1e-9)
}

// ---------------------------------------------------------------------------
// IsMeetingSLO
// ---------------------------------------------------------------------------

func TestTracker_IsMeetingSLO(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "test",
		Target: 0.99,
		Window: time.Hour,
	})

	for i := 0; i < 100; i++ {
		tracker.RecordGood()
	}
	require.True(t, tracker.IsMeetingSLO())

	for i := 0; i < 10; i++ {
		tracker.RecordBad()
	}
	// 100/110 = 90.9%, below 99%
	require.False(t, tracker.IsMeetingSLO())
}

func TestTracker_IsMeetingSLO_ExactTarget(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "exact-target",
		Target: 0.50,
		Window: time.Hour,
	})

	tracker.RecordGood()
	tracker.RecordBad()

	// 1/2 = 0.5, exactly at target (>= satisfied).
	require.True(t, tracker.IsMeetingSLO())
}

func TestTracker_IsMeetingSLO_NoData(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "no-data",
		Target: 0.999,
		Window: time.Hour,
	})

	// No data -> SLI = 1.0 -> meeting SLO.
	require.True(t, tracker.IsMeetingSLO())
}

// ---------------------------------------------------------------------------
// ErrorBudgetRemaining
// ---------------------------------------------------------------------------

func TestTracker_ErrorBudget_Initial(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "test",
		Target: 0.99,
		Window: time.Millisecond,
	})

	require.Equal(t, float64(1.0), tracker.ErrorBudgetRemaining())
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

func TestTracker_Stats(t *testing.T) {
	t.Parallel()
	obj := Objective{
		Name:   "test",
		SLI:    SLIAvailability,
		Target: 0.99,
		Window: time.Hour,
	}
	tracker := NewTracker(obj)

	tracker.RecordGood()
	tracker.RecordBad()

	stats := tracker.Stats()
	require.Equal(t, obj.Name, stats.Objective.Name)
	require.Equal(t, float64(0.99), stats.Target)
	require.Equal(t, int64(1), stats.GoodEvents)
	require.Equal(t, int64(2), stats.TotalEvents)
}

func TestTracker_Stats_BudgetConsumed_NoAllowedErrors(t *testing.T) {
	t.Parallel()
	// Target 1.0 -> allowedErrors = 0 -> budgetConsumed stays 0.
	tracker := NewTracker(Objective{
		Name:   "perfect",
		Target: 1.0,
		Window: time.Hour,
	})

	tracker.RecordGood()
	tracker.RecordGood()

	stats := tracker.Stats()
	require.InDelta(t, 0.0, stats.BudgetConsumed, 1e-9)
}

func TestTracker_Stats_BudgetConsumed_Positive(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "budget-test",
		Target: 0.90,
		Window: time.Hour,
	})

	// 9 good, 1 bad -> SLI = 0.9, allowedErrors = (0.1)*10 = 1, actualErrors = 1.
	for i := 0; i < 9; i++ {
		tracker.RecordGood()
	}
	tracker.RecordBad()

	stats := tracker.Stats()
	// budgetConsumed = actualErrors / allowedErrors = 1/1 = 1.0
	require.InDelta(t, 1.0, stats.BudgetConsumed, 1e-9)
}

func TestTracker_Stats_BudgetConsumed_Partial(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "partial-budget",
		Target: 0.90,
		Window: time.Hour,
	})

	// 19 good, 1 bad -> SLI = 0.95, allowedErrors = (0.1)*20 = 2, actualErrors = 1.
	for i := 0; i < 19; i++ {
		tracker.RecordGood()
	}
	tracker.RecordBad()

	stats := tracker.Stats()
	// budgetConsumed = 1/2 = 0.5
	require.InDelta(t, 0.5, stats.BudgetConsumed, 1e-9)
}

func TestTracker_Stats_MeetingSLO(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "stats-meeting",
		Target: 0.50,
		Window: time.Hour,
	})

	tracker.RecordGood()
	stats := tracker.Stats()
	require.True(t, stats.MeetingSLO)

	tracker.RecordBad()
	tracker.RecordBad()
	stats = tracker.Stats()
	// 1/3 ~ 0.33 < 0.5
	require.False(t, stats.MeetingSLO)
}

func TestTracker_Stats_NoData(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "stats-no-data",
		Target: 0.99,
		Window: time.Hour,
	})

	stats := tracker.Stats()
	require.InDelta(t, 1.0, stats.CurrentSLI, 1e-9)
	require.True(t, stats.MeetingSLO)
	require.Equal(t, int64(0), stats.GoodEvents)
	require.Equal(t, int64(0), stats.TotalEvents)
	require.Equal(t, 0, stats.WindowsComplete)
}

// ---------------------------------------------------------------------------
// Window rotation
// ---------------------------------------------------------------------------

func newTrackerWithClock(obj Objective, now func() time.Time) *Tracker {
	tr := NewTracker(obj)
	tr.nowFunc = now
	tr.windowStart = now()
	return tr
}

func TestTracker_WindowRotation_Basic(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "rotate",
		Target: 0.99,
		Window: time.Hour,
	}, nowFn)

	// Record events within the window.
	tracker.RecordGood()
	tracker.RecordGood()
	tracker.RecordBad()

	// Before rotation: counters intact.
	require.Equal(t, int64(2), tracker.goodEvents.Load())
	require.Equal(t, int64(3), tracker.totalEvents.Load())

	// Advance past the window.
	advanceClock(2 * time.Hour)

	// Trigger rotation by recording another event.
	tracker.RecordGood()

	// After rotation: counters reset to 0 (the triggering event is
	// included in the old window because Add runs before maybeRotateWindow).
	require.Equal(t, int64(0), tracker.goodEvents.Load())
	require.Equal(t, int64(0), tracker.totalEvents.Load())

	// History should have one completed window.
	tracker.mu.RLock()
	require.Len(t, tracker.history, 1)
	result := tracker.history[0]
	tracker.mu.RUnlock()

	// The triggering RecordGood is included: 3 good out of 4 total.
	require.InDelta(t, 3.0/4.0, result.SLI, 1e-9)
	require.Equal(t, int64(3), result.GoodEvents)
	require.Equal(t, int64(4), result.TotalEvents)
	require.False(t, result.Met) // 75% < 99%
}

func TestTracker_WindowRotation_CountersReset(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "reset-check",
		Target: 0.95,
		Window: time.Minute,
	}, nowFn)

	// Record latency events.
	tracker.RecordLatency(10*time.Millisecond, 50*time.Millisecond)
	tracker.RecordLatency(100*time.Millisecond, 50*time.Millisecond)

	require.Equal(t, int64(1), tracker.latencyBad.Load())
	require.NotZero(t, tracker.latencySum.Load())
	require.NotZero(t, tracker.latencyCount.Load())

	// Rotate.
	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	// All latency counters should be zeroed.
	require.Equal(t, int64(0), tracker.latencyBad.Load())
	require.Equal(t, int64(0), tracker.latencySum.Load())
	require.Equal(t, int64(0), tracker.latencyCount.Load())
}

func TestTracker_WindowRotation_NoRotationBeforeWindowExpires(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "no-rotate",
		Target: 0.99,
		Window: time.Hour,
	}, nowFn)

	tracker.RecordGood()
	tracker.RecordBad()

	// Advance less than the window.
	advanceClock(30 * time.Minute)
	tracker.RecordGood()

	// No rotation: counters accumulate.
	require.Equal(t, int64(2), tracker.goodEvents.Load())
	require.Equal(t, int64(3), tracker.totalEvents.Load())

	tracker.mu.RLock()
	require.Len(t, tracker.history, 0)
	tracker.mu.RUnlock()
}

func TestTracker_WindowRotation_HistoryCap(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "history-cap",
		Target: 0.99,
		Window: time.Second,
	}, nowFn)

	// Create 105 windows: history should be capped at 100.
	for i := 0; i < 105; i++ {
		tracker.RecordGood()
		advanceClock(2 * time.Second)
	}
	// One more event to flush the final window.
	tracker.RecordGood()
	advanceClock(2 * time.Second)
	tracker.RecordGood() // triggers rotation

	tracker.mu.RLock()
	histLen := len(tracker.history)
	tracker.mu.RUnlock()

	require.LessOrEqual(t, histLen, 100)
}

func TestTracker_WindowRotation_WindowStartUpdated(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "window-start",
		Target: 0.99,
		Window: time.Minute,
	}, nowFn)

	initialStart := tracker.windowStart
	tracker.RecordGood()

	advanceClock(2 * time.Minute)
	tracker.RecordGood() // triggers rotation

	tracker.mu.RLock()
	newStart := tracker.windowStart
	tracker.mu.RUnlock()

	require.True(t, newStart.After(initialStart))
}

func TestTracker_WindowRotation_SLI100Percent(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "perfect-window",
		Target: 0.99,
		Window: time.Minute,
	}, nowFn)

	for i := 0; i < 10; i++ {
		tracker.RecordGood()
	}

	advanceClock(2 * time.Minute)
	tracker.RecordGood() // trigger rotation

	tracker.mu.RLock()
	require.Len(t, tracker.history, 1)
	require.InDelta(t, 1.0, tracker.history[0].SLI, 1e-9)
	require.True(t, tracker.history[0].Met)
	tracker.mu.RUnlock()
}

// ---------------------------------------------------------------------------
// Error budget via window rotation
// ---------------------------------------------------------------------------

func TestTracker_ErrorBudget_ConsumedAfterRotation(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "budget-consume",
		Target: 0.90,
		Window: time.Minute,
	}, nowFn)

	// 9 good, 1 bad recorded; the triggering RecordGood adds 1 more good
	// before maybeRotateWindow runs. Window contains 10 good, 11 total.
	// SLI = 10/11, allowedErrorRate = 0.1, actualErrorRate = 1/11.
	// consumption = (1/11) / 0.1 = 10/11.
	// errorBudget = 1.0 - 10/11 = 1/11.
	for i := 0; i < 9; i++ {
		tracker.RecordGood()
	}
	tracker.RecordBad()

	advanceClock(2 * time.Minute)
	tracker.RecordGood() // trigger rotation

	budget := tracker.ErrorBudgetRemaining()
	require.InDelta(t, 1.0/11.0, budget, 1e-9)
}

func TestTracker_ErrorBudget_PartialConsumption(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "budget-partial",
		Target: 0.90,
		Window: time.Minute,
	}, nowFn)

	// 19 good, 1 bad recorded; the triggering RecordGood adds 1 more good.
	// Window contains 20 good, 21 total.
	// SLI = 20/21, allowedErrorRate = 0.1, actualErrorRate = 1/21.
	// consumption = (1/21) / 0.1 = 10/21.
	// errorBudget = 1.0 - 10/21 = 11/21.
	for i := 0; i < 19; i++ {
		tracker.RecordGood()
	}
	tracker.RecordBad()

	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	budget := tracker.ErrorBudgetRemaining()
	require.InDelta(t, 11.0/21.0, budget, 1e-9)
}

func TestTracker_ErrorBudget_ClampToZero(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "budget-clamp-zero",
		Target: 0.99,
		Window: time.Minute,
	}, nowFn)

	// All bad events -> SLI = 0, actualErrorRate = 1.0
	// allowedErrorRate = 0.01, consumption = 1.0/0.01 = 100
	// errorBudget = 1.0 - 100 = -99 -> clamped to 0
	for i := 0; i < 10; i++ {
		tracker.RecordBad()
	}

	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	budget := tracker.ErrorBudgetRemaining()
	require.InDelta(t, 0.0, budget, 1e-9)
}

func TestTracker_ErrorBudget_Target100Percent(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	// Target = 1.0 -> allowedErrorRate = 0 -> budget goes to 0 immediately.
	tracker := newTrackerWithClock(Objective{
		Name:   "budget-100",
		Target: 1.0,
		Window: time.Minute,
	}, nowFn)

	tracker.RecordGood()

	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	budget := tracker.ErrorBudgetRemaining()
	require.InDelta(t, 0.0, budget, 1e-9)
}

func TestTracker_ErrorBudget_PerfectWindow_NoConsumption(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "budget-perfect",
		Target: 0.90,
		Window: time.Minute,
	}, nowFn)

	// All good -> SLI = 1.0 -> actualErrorRate = 0 -> consumption = 0.
	for i := 0; i < 10; i++ {
		tracker.RecordGood()
	}

	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	budget := tracker.ErrorBudgetRemaining()
	require.InDelta(t, 1.0, budget, 1e-9)
}

// ---------------------------------------------------------------------------
// Callbacks on budget transitions
// ---------------------------------------------------------------------------

func TestTracker_Callback_OnBudgetExhausted(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "cb-exhausted",
		Target: 0.99,
		Window: time.Minute,
	}, nowFn)

	var exhaustedCalled atomic.Bool
	tracker.OnBudgetExhausted(func() {
		exhaustedCalled.Store(true)
	})

	// All bad will exhaust the budget.
	for i := 0; i < 10; i++ {
		tracker.RecordBad()
	}

	advanceClock(2 * time.Minute)
	tracker.RecordGood() // trigger rotation

	// Callback runs in a goroutine; wait for it.
	require.Eventually(t, func() bool {
		return exhaustedCalled.Load()
	}, time.Second, 10*time.Millisecond)
}

func TestTracker_Callback_OnBudgetRestored(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "cb-restored",
		Target: 0.90,
		Window: time.Minute,
	}, nowFn)

	var restoredCalled atomic.Bool
	tracker.OnBudgetRestored(func() {
		restoredCalled.Store(true)
	})
	tracker.OnBudgetExhausted(func() {})

	// First window: exhaust the budget.
	for i := 0; i < 10; i++ {
		tracker.RecordBad()
	}
	advanceClock(2 * time.Minute)
	tracker.RecordGood() // triggers rotation, budget goes to 0

	require.InDelta(t, 0.0, tracker.ErrorBudgetRemaining(), 1e-9)

	// updateErrorBudget only subtracts consumption; a perfect follow-up
	// window (consumption=0) leaves errorBudget at 0. The restored
	// callback only fires when wasExhausted && !isExhausted, which
	// requires the budget to go above 0. Since the code never increases
	// errorBudget, the restored callback does not fire.
	for i := 0; i < 10; i++ {
		tracker.RecordGood()
	}
	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	// Budget remains 0; restored callback should NOT have fired.
	require.InDelta(t, 0.0, tracker.ErrorBudgetRemaining(), 1e-9)
	require.False(t, restoredCalled.Load())
}

func TestTracker_Callback_NotCalledWhenNotSet(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "cb-nil",
		Target: 0.99,
		Window: time.Minute,
	}, nowFn)

	// No callbacks set: rotation should not panic.
	for i := 0; i < 10; i++ {
		tracker.RecordBad()
	}
	advanceClock(2 * time.Minute)

	require.NotPanics(t, func() {
		tracker.RecordGood()
	})
}

// ---------------------------------------------------------------------------
// Stats windowsComplete after rotation
// ---------------------------------------------------------------------------

func TestTracker_Stats_WindowsComplete(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "windows-count",
		Target: 0.99,
		Window: time.Minute,
	}, nowFn)

	require.Equal(t, 0, tracker.Stats().WindowsComplete)

	// Rotate 3 times.
	for w := 0; w < 3; w++ {
		tracker.RecordGood()
		advanceClock(2 * time.Minute)
	}
	tracker.RecordGood() // trigger final rotation

	require.Equal(t, 3, tracker.Stats().WindowsComplete)
}

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

func TestManager_Register(t *testing.T) {
	t.Parallel()
	m := NewManager()
	tracker := NewTracker(Objective{Name: "test"})

	m.Register("test", tracker)

	got, ok := m.Get("test")
	require.True(t, ok)
	require.Equal(t, tracker, got)
}

func TestManager_Get_NotFound(t *testing.T) {
	t.Parallel()
	m := NewManager()

	got, ok := m.Get("nonexistent")
	require.False(t, ok)
	require.Nil(t, got)
}

func TestManager_SetPolicy(t *testing.T) {
	t.Parallel()
	m := NewManager()
	tracker := NewTracker(Objective{Name: "test"})
	m.Register("test", tracker)

	called := false
	m.SetPolicy("test", Policy{
		MinBudget:   0.5,
		OnLowBudget: func() { called = true },
	})

	// Policy stored but not triggered yet.
	require.False(t, called)
}

func TestManager_AllStats(t *testing.T) {
	t.Parallel()
	m := NewManager()
	m.Register("a", NewTracker(Objective{Name: "a"}))
	m.Register("b", NewTracker(Objective{Name: "b"}))

	stats := m.AllStats()
	require.Len(t, stats, 2)
	require.Contains(t, stats, "a")
	require.Contains(t, stats, "b")
}

func TestManager_AllStats_Empty(t *testing.T) {
	t.Parallel()
	m := NewManager()
	stats := m.AllStats()
	require.Empty(t, stats)
}

// ---------------------------------------------------------------------------
// Manager.Evaluate
// ---------------------------------------------------------------------------

func TestManager_Evaluate_NoTrackers(t *testing.T) {
	t.Parallel()
	m := NewManager()
	actions := m.Evaluate(context.Background())
	require.Empty(t, actions)
}

func TestManager_Evaluate_MeetingSLO(t *testing.T) {
	t.Parallel()
	m := NewManager()
	tracker := NewTracker(Objective{
		Name:   "healthy",
		Target: 0.90,
		Window: time.Hour,
	})
	for i := 0; i < 100; i++ {
		tracker.RecordGood()
	}
	m.Register("healthy", tracker)

	actions := m.Evaluate(context.Background())
	assert.NotContains(t, actions, "healthy")
}

func TestManager_Evaluate_SLOViolated(t *testing.T) {
	t.Parallel()
	m := NewManager()
	tracker := NewTracker(Objective{
		Name:   "violated",
		Target: 0.99,
		Window: time.Hour,
	})
	for i := 0; i < 50; i++ {
		tracker.RecordGood()
	}
	for i := 0; i < 50; i++ {
		tracker.RecordBad()
	}
	m.Register("violated", tracker)

	actions := m.Evaluate(context.Background())
	require.Contains(t, actions, "violated")
	require.Equal(t, "SLO violated", actions["violated"])
}

func TestManager_Evaluate_OnViolationCallback(t *testing.T) {
	t.Parallel()
	m := NewManager()
	tracker := NewTracker(Objective{
		Name:   "cb-violation",
		Target: 0.99,
		Window: time.Hour,
	})
	for i := 0; i < 50; i++ {
		tracker.RecordBad()
	}
	m.Register("cb-violation", tracker)

	var violationCalled atomic.Bool
	m.SetPolicy("cb-violation", Policy{
		OnViolation: func() {
			violationCalled.Store(true)
		},
	})

	m.Evaluate(context.Background())

	require.Eventually(t, func() bool {
		return violationCalled.Load()
	}, time.Second, 10*time.Millisecond)
}

func TestManager_Evaluate_OnExhaustedCallback(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	m := NewManager()
	tracker := newTrackerWithClock(Objective{
		Name:   "cb-mgr-exhausted",
		Target: 0.99,
		Window: time.Minute,
	}, nowFn)

	// Exhaust the budget via rotation.
	for i := 0; i < 10; i++ {
		tracker.RecordBad()
	}
	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	m.Register("cb-mgr-exhausted", tracker)

	var exhaustedCalled atomic.Bool
	m.SetPolicy("cb-mgr-exhausted", Policy{
		OnExhausted: func() {
			exhaustedCalled.Store(true)
		},
	})

	m.Evaluate(context.Background())

	require.Eventually(t, func() bool {
		return exhaustedCalled.Load()
	}, time.Second, 10*time.Millisecond)
}

func TestManager_Evaluate_OnLowBudgetCallback(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	m := NewManager()
	tracker := newTrackerWithClock(Objective{
		Name:   "cb-low-budget",
		Target: 0.90,
		Window: time.Minute,
	}, nowFn)

	// 19 good + 1 bad + triggering good = 20 good, 21 total.
	// consumption = 10/21, budget = 11/21 ~ 0.524.
	for i := 0; i < 19; i++ {
		tracker.RecordGood()
	}
	tracker.RecordBad()
	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	m.Register("cb-low-budget", tracker)

	var lowBudgetCalled atomic.Bool
	m.SetPolicy("cb-low-budget", Policy{
		MinBudget: 0.6, // budget is ~0.524, below this threshold
		OnLowBudget: func() {
			lowBudgetCalled.Store(true)
		},
	})

	m.Evaluate(context.Background())

	require.Eventually(t, func() bool {
		return lowBudgetCalled.Load()
	}, time.Second, 10*time.Millisecond)
}

func TestManager_Evaluate_LowBudget_NotTriggered_WhenAboveThreshold(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	m := NewManager()
	tracker := newTrackerWithClock(Objective{
		Name:   "above-threshold",
		Target: 0.90,
		Window: time.Minute,
	}, nowFn)

	// All good -> budget stays at 1.0.
	for i := 0; i < 10; i++ {
		tracker.RecordGood()
	}
	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	m.Register("above-threshold", tracker)

	var lowBudgetCalled atomic.Bool
	m.SetPolicy("above-threshold", Policy{
		MinBudget: 0.5,
		OnLowBudget: func() {
			lowBudgetCalled.Store(true)
		},
	})

	m.Evaluate(context.Background())

	// Give callbacks a moment; it should NOT have fired.
	time.Sleep(50 * time.Millisecond)
	require.False(t, lowBudgetCalled.Load())
}

func TestManager_Evaluate_NoPolicyForTracker(t *testing.T) {
	t.Parallel()
	m := NewManager()
	tracker := NewTracker(Objective{
		Name:   "no-policy",
		Target: 0.99,
		Window: time.Hour,
	})
	for i := 0; i < 50; i++ {
		tracker.RecordBad()
	}
	m.Register("no-policy", tracker)

	// No policy set: should not panic and should still return action.
	actions := m.Evaluate(context.Background())
	require.Equal(t, "SLO violated", actions["no-policy"])
}

func TestManager_Evaluate_MultipleTrackers(t *testing.T) {
	t.Parallel()
	m := NewManager()

	healthy := NewTracker(Objective{Name: "healthy", Target: 0.50, Window: time.Hour})
	healthy.RecordGood()

	sick := NewTracker(Objective{Name: "sick", Target: 0.99, Window: time.Hour})
	for i := 0; i < 50; i++ {
		sick.RecordBad()
	}

	m.Register("healthy", healthy)
	m.Register("sick", sick)

	actions := m.Evaluate(context.Background())
	require.NotContains(t, actions, "healthy")
	require.Contains(t, actions, "sick")
}

// ---------------------------------------------------------------------------
// SLI type constants
// ---------------------------------------------------------------------------

func TestObjective_SLITypes(t *testing.T) {
	t.Parallel()
	require.Equal(t, SLI("availability"), SLIAvailability)
	require.Equal(t, SLI("latency"), SLILatency)
	require.Equal(t, SLI("throughput"), SLIThroughput)
	require.Equal(t, SLI("error_rate"), SLIErrorRate)
}

// ---------------------------------------------------------------------------
// Concurrency safety
// ---------------------------------------------------------------------------

func TestTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	tracker := NewTracker(Objective{
		Name:   "concurrent",
		Target: 0.95,
		Window: time.Hour,
	})

	var wg sync.WaitGroup
	const goroutines = 10
	const opsPerGoroutine = 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				tracker.RecordGood()
				tracker.RecordBad()
				tracker.RecordLatency(50*time.Millisecond, 100*time.Millisecond)
				_ = tracker.CurrentSLI()
				_ = tracker.IsMeetingSLO()
				_ = tracker.ErrorBudgetRemaining()
				_ = tracker.Stats()
			}
		}()
	}
	wg.Wait()

	stats := tracker.Stats()
	// Each goroutine does 3 total events per iteration (good + bad + latency).
	expectedTotal := int64(goroutines * opsPerGoroutine * 3)
	require.Equal(t, expectedTotal, stats.TotalEvents)
}

func TestManager_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	m := NewManager()

	var wg sync.WaitGroup
	const goroutines = 10

	for g := 0; g < goroutines; g++ {
		name := "tracker-" + string(rune('a'+g))
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr := NewTracker(Objective{Name: name, Target: 0.95, Window: time.Hour})
			m.Register(name, tr)
			m.Get(name)
			m.AllStats()
			m.Evaluate(context.Background())
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestTracker_ZeroWindowDuration(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}

	// A zero-duration window means every event triggers rotation.
	tracker := newTrackerWithClock(Objective{
		Name:   "zero-window",
		Target: 0.99,
		Window: 0,
	}, nowFn)

	// Should not panic.
	require.NotPanics(t, func() {
		tracker.RecordGood()
		tracker.RecordBad()
	})
}

func TestTracker_AllBadEvents(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "all-bad",
		Target: 0.99,
		Window: time.Hour,
	})

	for i := 0; i < 100; i++ {
		tracker.RecordBad()
	}

	require.InDelta(t, 0.0, tracker.CurrentSLI(), 1e-9)
	require.False(t, tracker.IsMeetingSLO())

	stats := tracker.Stats()
	require.Equal(t, int64(0), stats.GoodEvents)
	require.Equal(t, int64(100), stats.TotalEvents)
}

func TestTracker_OverwriteCallbacks(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "overwrite-cb",
		Target: 0.99,
		Window: time.Hour,
	})

	tracker.OnBudgetExhausted(func() {})
	tracker.OnBudgetExhausted(func() {})

	// Only the second callback should be stored; no panic.
	tracker.mu.RLock()
	cb := tracker.onBudgetExhausted
	tracker.mu.RUnlock()

	require.NotNil(t, cb)
}

func TestTracker_WindowResult_Fields(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "window-fields",
		Target: 0.80,
		Window: 10 * time.Minute,
	}, nowFn)

	for i := 0; i < 8; i++ {
		tracker.RecordGood()
	}
	for i := 0; i < 2; i++ {
		tracker.RecordBad()
	}

	advanceClock(15 * time.Minute)
	tracker.RecordGood()

	tracker.mu.RLock()
	require.Len(t, tracker.history, 1)
	wr := tracker.history[0]
	tracker.mu.RUnlock()

	require.Equal(t, time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), wr.Start)
	require.Equal(t, time.Date(2025, 6, 15, 10, 15, 0, 0, time.UTC), wr.End)
	// The triggering RecordGood is included: 9 good out of 11 total.
	require.InDelta(t, 9.0/11.0, wr.SLI, 1e-9)
	require.Equal(t, 0.80, wr.Target)
	require.True(t, wr.Met) // 9/11 ~ 0.818 >= 0.80
	require.Equal(t, int64(9), wr.GoodEvents)
	require.Equal(t, int64(11), wr.TotalEvents)
}

func TestTracker_MultipleRotations_BudgetDecreases(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "multi-rotate",
		Target: 0.90,
		Window: time.Minute,
	}, nowFn)

	// Window 1: 19 good + 1 bad + triggering good = 20 good, 21 total.
	// consumption = (1/21) / 0.1 = 10/21. budget = 11/21.
	for i := 0; i < 19; i++ {
		tracker.RecordGood()
	}
	tracker.RecordBad()
	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	budget1 := tracker.ErrorBudgetRemaining()
	require.InDelta(t, 11.0/21.0, budget1, 1e-9)

	// Window 2: 18 good + 1 bad + triggering good = 19 good, 20 total.
	// SLI = 19/20, actualErrorRate = 1/20.
	// consumption = (1/20) / 0.1 = 0.5.
	// budget = 11/21 - 0.5 = (22 - 21) / 42 = 1/42.
	for i := 0; i < 18; i++ {
		tracker.RecordGood()
	}
	tracker.RecordBad()
	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	budget2 := tracker.ErrorBudgetRemaining()
	require.InDelta(t, 1.0/42.0, budget2, 1e-9)
	require.True(t, budget2 <= budget1)
}

func TestTracker_Objective_Description(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:        "with-desc",
		SLI:         SLIAvailability,
		Target:      0.999,
		Window:      24 * time.Hour,
		Description: "API availability over 24h",
	})

	stats := tracker.Stats()
	require.Equal(t, "API availability over 24h", stats.Objective.Description)
	require.Equal(t, 24*time.Hour, stats.Objective.Window)
}

func TestManager_Register_Overwrite(t *testing.T) {
	t.Parallel()
	m := NewManager()

	tracker1 := NewTracker(Objective{Name: "first"})
	tracker2 := NewTracker(Objective{Name: "second"})

	m.Register("slot", tracker1)
	m.Register("slot", tracker2)

	got, ok := m.Get("slot")
	require.True(t, ok)
	require.Equal(t, tracker2, got)
}

func TestTracker_Stats_ErrorBudget_Persisted(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advanceClock := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(d)
	}

	tracker := newTrackerWithClock(Objective{
		Name:   "stats-budget",
		Target: 0.90,
		Window: time.Minute,
	}, nowFn)

	// Partially consume budget: 19 good + 1 bad + triggering good.
	// Window: 20 good, 21 total. Budget = 11/21.
	for i := 0; i < 19; i++ {
		tracker.RecordGood()
	}
	tracker.RecordBad()
	advanceClock(2 * time.Minute)
	tracker.RecordGood()

	stats := tracker.Stats()
	require.InDelta(t, 11.0/21.0, stats.ErrorBudget, 1e-9)
}

func TestTracker_Stats_WindowStart_Tracked(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "window-start-check",
		Target: 0.99,
		Window: time.Hour,
	})

	stats := tracker.Stats()
	require.False(t, stats.WindowStart.IsZero())
}
