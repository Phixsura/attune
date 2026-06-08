// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/handlers/console/auth"
)

func TestVerifyOrDummy_RealHashWrongPassword(t *testing.T) {
	h, err := auth.HashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if auth.VerifyOrDummy(h, "wrong-password") {
		t.Error("wrong password should not verify")
	}
}

func TestVerifyOrDummy_RealHashRightPassword(t *testing.T) {
	h, err := auth.HashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyOrDummy(h, "correct-password") {
		t.Error("correct password should verify")
	}
}

func TestVerifyOrDummy_EmptyHashRunsDummy(t *testing.T) {
	if auth.VerifyOrDummy("", "anything") {
		t.Error("empty hash should always return false")
	}
}

// TestVerifyOrDummy_TimingEquality checks that the dummy-hash code path
// is similar in wall-clock cost to the real-hash path. Avoid this test
// in -race because bcrypt is CPU-bound and -race adds overhead unevenly.
func TestVerifyOrDummy_TimingEquality(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in -short mode")
	}
	h, _ := auth.HashPassword("known-password")
	const iters = 20
	start := time.Now()
	for i := 0; i < iters; i++ {
		auth.VerifyOrDummy(h, "wrong")
	}
	realTime := time.Since(start)
	start = time.Now()
	for i := 0; i < iters; i++ {
		auth.VerifyOrDummy("", "wrong")
	}
	dummyTime := time.Since(start)
	ratio := float64(realTime) / float64(dummyTime)
	if ratio < 0.5 || ratio > 2.0 {
		t.Errorf("timing ratio real/dummy = %.2f; want ~1.0 (between 0.5 and 2.0)", ratio)
	}
}
