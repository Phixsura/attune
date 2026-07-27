// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/handlers/console/auth"
)

func TestVerifyOrDummy_RealHashWrongPassword(t *testing.T) {
	t.Parallel()
	h, err := auth.HashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if auth.VerifyOrDummy(h, "wrong-password") {
		t.Error("wrong password should not verify")
	}
}

func TestVerifyOrDummy_RealHashRightPassword(t *testing.T) {
	t.Parallel()
	h, err := auth.HashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyOrDummy(h, "correct-password") {
		t.Error("correct password should verify")
	}
}

func TestVerifyOrDummy_EmptyHashRunsDummy(t *testing.T) {
	t.Parallel()
	if auth.VerifyOrDummy("", "anything") {
		t.Error("empty hash should always return false")
	}
}

func TestHashPassword_Success(t *testing.T) {
	t.Parallel()
	h, err := auth.HashPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	if h == "" {
		t.Error("hash should not be empty")
	}
	if !auth.VerifyOrDummy(h, "test-password") {
		t.Error("hash should verify against original password")
	}
}

// TestVerifyOrDummy_TimingEquality checks that the dummy-hash code path
// is similar in wall-clock cost to the real-hash path. Avoid this test
// in -race because bcrypt is CPU-bound and -race adds overhead unevenly.
func TestVerifyOrDummy_TimingEquality(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in -short mode")
	}
	if raceEnabled {
		t.Skip("skipping timing test under -race")
	}
	h, _ := auth.HashPassword("known-password")
	const iters = 6
	auth.VerifyOrDummy(h, "wrong")
	auth.VerifyOrDummy("", "wrong")
	var realTime time.Duration
	var dummyTime time.Duration
	for i := 0; i < iters; i++ {
		start := time.Now()
		auth.VerifyOrDummy(h, "wrong")
		realTime += time.Since(start)
		start = time.Now()
		auth.VerifyOrDummy("", "wrong")
		dummyTime += time.Since(start)
	}
	ratio := float64(realTime) / float64(dummyTime)
	if ratio < 0.25 || ratio > 4.0 {
		t.Errorf("timing ratio real/dummy = %.2f; want same order of magnitude (between 0.25 and 4.0)", ratio)
	}
}
