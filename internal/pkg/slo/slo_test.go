// SPDX-License-Identifier: Apache-2.0

package slo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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

func TestTracker_CurrentSLI_NoData(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "test",
		Target: 0.99,
		Window: time.Hour,
	})

	require.Equal(t, float64(1.0), tracker.CurrentSLI())
}

func TestTracker_IsMeetingSLO(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "test",
		Target: 0.99,
		Window: time.Hour,
	})

	// 100% success
	for i := 0; i < 100; i++ {
		tracker.RecordGood()
	}
	require.True(t, tracker.IsMeetingSLO())

	// Add failures to drop below target
	for i := 0; i < 10; i++ {
		tracker.RecordBad()
	}
	// 100/110 = 90.9%, below 99%
	require.False(t, tracker.IsMeetingSLO())
}

func TestTracker_ErrorBudget(t *testing.T) {
	t.Parallel()
	tracker := NewTracker(Objective{
		Name:   "test",
		Target: 0.99,
		Window: time.Millisecond, // Short window for testing
	})

	// Initially full budget
	require.Equal(t, float64(1.0), tracker.ErrorBudgetRemaining())
}

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

func TestManager_Register(t *testing.T) {
	t.Parallel()
	m := NewManager()
	tracker := NewTracker(Objective{Name: "test"})

	m.Register("test", tracker)

	got, ok := m.Get("test")
	require.True(t, ok)
	require.Equal(t, tracker, got)
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

	// Policy should be stored but not triggered yet
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

func TestObjective_SLITypes(t *testing.T) {
	t.Parallel()
	require.Equal(t, SLI("availability"), SLIAvailability)
	require.Equal(t, SLI("latency"), SLILatency)
	require.Equal(t, SLI("throughput"), SLIThroughput)
	require.Equal(t, SLI("error_rate"), SLIErrorRate)
}
