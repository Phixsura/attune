// SPDX-License-Identifier: Apache-2.0

package ratelimit

import (
	"context"
	"testing"
	"time"
)

func FuzzSlidingLimiter(f *testing.F) {
	f.Add("tenant-1", 100, int64(60))
	f.Add("", 0, int64(0))
	f.Add("tenant-with-long-name-12345", 1000000, int64(3600))

	lim := NewMemorySlidingLimiter()
	ctx := context.Background()

	f.Fuzz(func(t *testing.T, key string, limit int, windowSecs int64) {
		if windowSecs <= 0 {
			windowSecs = 1
		}
		if windowSecs > 3600 {
			windowSecs = 3600
		}
		window := time.Duration(windowSecs) * time.Second

		_, _, err := lim.Allow(ctx, key, limit, window)
		if err != nil {
			t.Errorf("Allow returned error: %v", err)
		}
	})
}

func FuzzTokenBucketLimiter(f *testing.F) {
	f.Add("client-1", 60, 10)
	f.Add("", 0, 0)
	f.Add("client-long-name", 1000000, 100000)

	lim := NewMemoryTokenBucketLimiter()
	ctx := context.Background()

	f.Fuzz(func(t *testing.T, key string, rpm int, burst int) {
		_, _, err := lim.AllowWithInfo(ctx, key, rpm, burst)
		if err != nil {
			t.Errorf("AllowWithInfo returned error: %v", err)
		}
	})
}
