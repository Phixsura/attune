package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wanmuchengchuan/listen/internal/domain"
)

// fakeNotifier records every Push call so tests can assert fanout
// reached all members regardless of intermediate failures.
type fakeNotifier struct {
	poolCalls  atomic.Int32
	radarCalls atomic.Int32
	poolErr    error
	radarErr   error
}

func (f *fakeNotifier) PushPool(_ context.Context, _ domain.Snapshot) error {
	f.poolCalls.Add(1)
	return f.poolErr
}

func (f *fakeNotifier) PushRadar(_ context.Context, _ domain.Snapshot) error {
	f.radarCalls.Add(1)
	return f.radarErr
}

func TestMultiNotifier_NoMembers(t *testing.T) {
	m := NewMultiNotifier()
	if err := m.PushPool(context.Background(), domain.Snapshot{}); err != nil {
		t.Fatalf("empty Multi PushPool: want nil, got %v", err)
	}
	if err := m.PushRadar(context.Background(), domain.Snapshot{}); err != nil {
		t.Fatalf("empty Multi PushRadar: want nil, got %v", err)
	}
}

func TestMultiNotifier_NilMembersSkipped(t *testing.T) {
	a := &fakeNotifier{}
	m := NewMultiNotifier(nil, a, nil)
	_ = m.PushPool(context.Background(), domain.Snapshot{})
	if a.poolCalls.Load() != 1 {
		t.Fatalf("a should be called once despite nil siblings, got %d", a.poolCalls.Load())
	}
}

func TestMultiNotifier_AllSuccess(t *testing.T) {
	a, b := &fakeNotifier{}, &fakeNotifier{}
	m := NewMultiNotifier(a, b)
	if err := m.PushPool(context.Background(), domain.Snapshot{}); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if a.poolCalls.Load() != 1 || b.poolCalls.Load() != 1 {
		t.Fatalf("want both members called once; a=%d b=%d",
			a.poolCalls.Load(), b.poolCalls.Load())
	}
}

func TestMultiNotifier_PartialFailureContinues(t *testing.T) {
	// Critical invariant: one failure must not skip the next member.
	failA := &fakeNotifier{poolErr: errors.New("a is down")}
	okB := &fakeNotifier{}
	m := NewMultiNotifier(failA, okB)
	err := m.PushPool(context.Background(), domain.Snapshot{})
	if err == nil {
		t.Fatalf("want joined err, got nil")
	}
	if !strings.Contains(err.Error(), "a is down") {
		t.Fatalf("err should mention failing member, got %v", err)
	}
	if okB.poolCalls.Load() != 1 {
		t.Fatalf("okB must be called despite failA failing, got %d",
			okB.poolCalls.Load())
	}
}

func TestMultiNotifier_RadarHonoredSeparately(t *testing.T) {
	// PushRadar must use radar callbacks, not pool. (Easy regression
	// trap with copy-paste fanout code.)
	a := &fakeNotifier{}
	m := NewMultiNotifier(a)
	_ = m.PushRadar(context.Background(), domain.Snapshot{})
	if a.radarCalls.Load() != 1 {
		t.Fatalf("radar count: want 1, got %d", a.radarCalls.Load())
	}
	if a.poolCalls.Load() != 0 {
		t.Fatalf("pool count: want 0 (radar only), got %d", a.poolCalls.Load())
	}
}

func TestMultiNotifier_BothMembersFailReturnsJoined(t *testing.T) {
	a := &fakeNotifier{poolErr: errors.New("a-err")}
	b := &fakeNotifier{poolErr: errors.New("b-err")}
	m := NewMultiNotifier(a, b)
	err := m.PushPool(context.Background(), domain.Snapshot{})
	if err == nil {
		t.Fatalf("want err, got nil")
	}
	if !strings.Contains(err.Error(), "a-err") || !strings.Contains(err.Error(), "b-err") {
		t.Fatalf("joined err should contain both, got %v", err)
	}
}
