// SPDX-License-Identifier: Apache-2.0

package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkSlidingLimiter_Allow(b *testing.B) {
	lim := NewMemorySlidingLimiter()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = lim.Allow(ctx, "tenant-1", 1000, time.Minute)
	}
}

func BenchmarkSlidingLimiter_Allow_Parallel(b *testing.B) {
	lim := NewMemorySlidingLimiter()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("tenant-%d", i%100)
			_, _, _ = lim.Allow(ctx, key, 1000, time.Minute)
			i++
		}
	})
}

func BenchmarkTokenBucketLimiter_AllowWithInfo(b *testing.B) {
	lim := NewMemoryTokenBucketLimiter()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = lim.AllowWithInfo(ctx, "client-1", 60, 10)
	}
}

func BenchmarkTokenBucketLimiter_AllowWithInfo_Parallel(b *testing.B) {
	lim := NewMemoryTokenBucketLimiter()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("client-%d", i%100)
			_, _, _ = lim.AllowWithInfo(ctx, key, 60, 10)
			i++
		}
	})
}

func BenchmarkConcurrencyLimiter_Acquire(b *testing.B) {
	lim := NewMemoryConcurrencyLimiter()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		release, _, _ := lim.Acquire(ctx, "worker-1", 10)
		if release != nil {
			release()
		}
	}
}
